package network

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// testServerHostPort extracts the host and port from an httptest.Server.
func testServerHostPort(t *testing.T, server *httptest.Server) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to parse test server address: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("failed to parse test server port: %v", err)
	}
	return host, port
}

func TestHTTPPinger_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`)) //nolint:errcheck
	}))
	defer server.Close()

	host, port := testServerHostPort(t, server)
	pinger := newHTTPPinger(port, "/healthz")

	ctx := context.Background()
	results, err := pinger.Ping(ctx, host, 3, 5*time.Second)

	if err != nil {
		t.Fatalf("Ping() unexpected error: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("Ping() returned %d results, want 3", len(results))
	}

	for i, r := range results {
		if !r.Success {
			t.Errorf("Ping result %d: expected success, got error: %v", i, r.Error)
		}
		if r.RTT == 0 {
			t.Errorf("Ping result %d: expected non-zero RTT", i)
		}
	}
}

func TestHTTPPinger_ServerDown(t *testing.T) {
	// Start and immediately close server to get an unreachable port
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	host, port := testServerHostPort(t, server)
	server.Close()

	pinger := newHTTPPinger(port, "/healthz")

	ctx := context.Background()
	results, err := pinger.Ping(ctx, host, 1, 2*time.Second)

	if err != nil {
		t.Fatalf("Ping() should not return top-level error, got: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("Ping() returned %d results, want 1", len(results))
	}

	if results[0].Success {
		t.Error("Ping to closed server should fail")
	}
}

func TestHTTPPinger_Non200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	host, port := testServerHostPort(t, server)
	pinger := newHTTPPinger(port, "/healthz")

	ctx := context.Background()
	results, err := pinger.Ping(ctx, host, 1, 5*time.Second)

	if err != nil {
		t.Fatalf("Ping() unexpected error: %v", err)
	}

	if results[0].Success {
		t.Error("Ping with 500 response should not be successful")
	}
}

func TestHTTPPinger_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	host, port := testServerHostPort(t, server)
	pinger := newHTTPPinger(port, "/healthz")

	ctx := context.Background()
	results, err := pinger.Ping(ctx, host, 1, 100*time.Millisecond)

	if err != nil {
		t.Fatalf("Ping() unexpected error: %v", err)
	}

	if results[0].Success {
		t.Error("Ping with timeout should fail")
	}
}

func TestHTTPPinger_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	host, port := testServerHostPort(t, server)
	pinger := newHTTPPinger(port, "/healthz")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	results, err := pinger.Ping(ctx, host, 5, 10*time.Second)

	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Errorf("expected context error, got: %v", err)
	}

	if len(results) >= 5 {
		t.Errorf("expected fewer than 5 results due to cancellation, got %d", len(results))
	}
}

func TestHTTPPinger_MultipleProbes(t *testing.T) {
	var callCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		if n == 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	host, port := testServerHostPort(t, server)
	pinger := newHTTPPinger(port, "/healthz")

	ctx := context.Background()
	results, err := pinger.Ping(ctx, host, 3, 5*time.Second)

	if err != nil {
		t.Fatalf("Ping() unexpected error: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	if !results[0].Success {
		t.Error("first probe should succeed")
	}
	if results[1].Success {
		t.Error("second probe should fail (503)")
	}
	if !results[2].Success {
		t.Error("third probe should succeed")
	}
}

func TestHTTPPinger_DefaultValues(t *testing.T) {
	pinger := newHTTPPinger(0, "").(*httpPinger)

	if pinger.port != defaultProbePort {
		t.Errorf("port = %d, want %d", pinger.port, defaultProbePort)
	}
	if pinger.path != defaultProbePath {
		t.Errorf("path = %q, want %q", pinger.path, defaultProbePath)
	}
}

func TestHTTPPinger_ImplementsPingerInterface(t *testing.T) {
	var _ Pinger = newHTTPPinger(8023, "/healthz")
}

// fakeResolver is a test Resolver returning canned LookupIP results. It records
// whether LookupIP was consulted so tests can assert IP-literal targets skip it.
type fakeResolver struct {
	ips         []net.IP
	err         error
	lookupIPHit atomic.Bool
}

func (f *fakeResolver) LookupHost(_ context.Context, _ string) ([]string, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeResolver) LookupAddr(_ context.Context, _ string) ([]string, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeResolver) LookupIP(_ context.Context, _, _ string) ([]net.IP, error) {
	f.lookupIPHit.Store(true)
	if f.err != nil {
		return nil, f.err
	}
	return f.ips, nil
}

func TestHTTPPinger_HostnameIPv4Only(t *testing.T) {
	pinger := newHTTPPinger(8023, "/healthz").(*httpPinger)
	pinger.resolver = &fakeResolver{ips: []net.IP{net.ParseIP("203.0.113.10")}}

	// Pin the dial to a dead port so the probe fails fast; we only assert family.
	pinger.client = unreachableClient()

	results, err := pinger.Ping(context.Background(), "ipv4.example.test", 1, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("Ping() unexpected error: %v", err)
	}
	if got := results[0].Family; got != FamilyIPv4 {
		t.Errorf("Family = %q, want %q", got, FamilyIPv4)
	}
}

func TestHTTPPinger_HostnameIPv6Only(t *testing.T) {
	pinger := newHTTPPinger(8023, "/healthz").(*httpPinger)
	pinger.resolver = &fakeResolver{ips: []net.IP{net.ParseIP("2001:db8::1")}}
	pinger.client = unreachableClient()

	results, err := pinger.Ping(context.Background(), "ipv6.example.test", 1, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("Ping() unexpected error: %v", err)
	}
	if got := results[0].Family; got != FamilyIPv6 {
		t.Errorf("Family = %q, want %q", got, FamilyIPv6)
	}
}

func TestHTTPPinger_HostnameDualStackPrefersIPv4(t *testing.T) {
	// httptest binds 127.0.0.1. A fake resolver maps the hostname to both an
	// IPv6 address (listed first) and the loopback IPv4 the server listens on.
	// IPv4 preference must win AND the pinned dial must reach the server.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	_, port := testServerHostPort(t, server)
	pinger := newHTTPPinger(port, "/healthz").(*httpPinger)
	pinger.resolver = &fakeResolver{ips: []net.IP{
		net.ParseIP("2001:db8::1"), // IPv6 first to prove preference, not order
		net.ParseIP("127.0.0.1"),   // the address the test server actually serves
	}}

	results, err := pinger.Ping(context.Background(), "dual.example.test", 1, 5*time.Second)
	if err != nil {
		t.Fatalf("Ping() unexpected error: %v", err)
	}
	if !results[0].Success {
		t.Fatalf("expected pinned dial to reach the server, got error: %v", results[0].Error)
	}
	if got := results[0].Family; got != FamilyIPv4 {
		t.Errorf("Family = %q, want %q (IPv4 preference)", got, FamilyIPv4)
	}
}

func TestHTTPPinger_ResolutionFailureGraceful(t *testing.T) {
	pinger := newHTTPPinger(8023, "/healthz").(*httpPinger)
	pinger.resolver = &fakeResolver{err: errors.New("no such host")}
	pinger.client = unreachableClient()

	results, err := pinger.Ping(context.Background(), "broken.example.test", 1, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("Ping() should not return a top-level error on resolution failure, got: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	// Documented fallback: resolution failure leaves family empty and the probe
	// surfaces its own (unpinned) error rather than a resolution error.
	if results[0].Success {
		t.Error("probe against unreachable fallback should not succeed")
	}
	if got := results[0].Family; got != "" {
		t.Errorf("Family = %q, want empty on resolution failure", got)
	}
}

func TestHTTPPinger_IPLiteralSkipsResolver(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target string
		family string
	}{
		{"ipv4", "127.0.0.1", FamilyIPv4},
		{"ipv6", "::1", FamilyIPv6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pinger := newHTTPPinger(8023, "/healthz").(*httpPinger)
			fake := &fakeResolver{ips: []net.IP{net.ParseIP("203.0.113.10")}}
			pinger.resolver = fake
			pinger.client = unreachableClient()

			results, err := pinger.Ping(context.Background(), tc.target, 1, 500*time.Millisecond)
			if err != nil {
				t.Fatalf("Ping() unexpected error: %v", err)
			}
			if got := results[0].Family; got != tc.family {
				t.Errorf("Family = %q, want %q", got, tc.family)
			}
			if fake.lookupIPHit.Load() {
				t.Error("resolver should NOT be consulted for IP-literal targets")
			}
		})
	}
}

// unreachableClient returns a client that dials a closed loopback port so probes
// fail fast without network access; used by family-only assertion tests.
func unreachableClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DisableKeepAlives: true,
			DialContext:       (&net.Dialer{Timeout: 200 * time.Millisecond}).DialContext,
		},
	}
}

func TestHTTPPinger_URLConstruction(t *testing.T) {
	var receivedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	host, port := testServerHostPort(t, server)
	pinger := newHTTPPinger(port, "/custom-health")

	ctx := context.Background()
	results, _ := pinger.Ping(ctx, host, 1, 5*time.Second)

	if len(results) == 0 || !results[0].Success {
		t.Fatal("probe should have succeeded")
	}

	if receivedPath != "/custom-health" {
		t.Errorf("server received path %q, want /custom-health", receivedPath)
	}
}
