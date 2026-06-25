package kubernetes

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newLoopbackTestClient builds a defaultKubeletClient whose healthz/metrics
// URLs point at the supplied addresses, with a short timeout and no auth.
func newLoopbackTestClient(healthzURL, metricsURL string) *defaultKubeletClient {
	cfg := &KubeletMonitorConfig{
		HealthzURL:  healthzURL,
		MetricsURL:  metricsURL,
		HTTPTimeout: 2 * time.Second,
	}
	return newDefaultKubeletClient(cfg).(*defaultKubeletClient)
}

// TestLoopbackFallbackURL_Rewrite verifies the URL-rewrite helper for the
// loopback families and that non-loopback hosts are never rewritten.
func TestLoopbackFallbackURL_Rewrite(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantURL  string
		wantBool bool
	}{
		{
			name:     "ipv4 loopback rewrites to ipv6",
			in:       "http://127.0.0.1:10248/healthz",
			wantURL:  "http://[::1]:10248/healthz",
			wantBool: true,
		},
		{
			name:     "ipv4 loopback metrics with query preserved",
			in:       "http://127.0.0.1:10250/metrics?foo=bar",
			wantURL:  "http://[::1]:10250/metrics?foo=bar",
			wantBool: true,
		},
		{
			name:     "ipv6 loopback rewrites to ipv4",
			in:       "http://[::1]:10248/healthz",
			wantURL:  "http://127.0.0.1:10248/healthz",
			wantBool: true,
		},
		{
			name:     "localhost rewrites to ipv6",
			in:       "https://localhost:10250/metrics",
			wantURL:  "https://[::1]:10250/metrics",
			wantBool: true,
		},
		{
			name:     "127.0.0.0/8 loopback rewrites to ipv6",
			in:       "http://127.0.0.53:10248/healthz",
			wantURL:  "http://[::1]:10248/healthz",
			wantBool: true,
		},
		{
			name:     "no port preserved",
			in:       "http://127.0.0.1/healthz",
			wantURL:  "http://[::1]/healthz",
			wantBool: true,
		},
		{
			name:     "non-loopback host not rewritten",
			in:       "http://10.0.0.5:10248/healthz",
			wantBool: false,
		},
		{
			name:     "public hostname not rewritten",
			in:       "http://kubelet.example.com:10250/metrics",
			wantBool: false,
		},
		{
			name:     "invalid url not rewritten",
			in:       "://not a url",
			wantBool: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, gotBool := loopbackFallbackURL(tt.in)
			if gotBool != tt.wantBool {
				t.Fatalf("loopbackFallbackURL(%q) bool = %v, want %v", tt.in, gotBool, tt.wantBool)
			}
			if tt.wantBool && gotURL != tt.wantURL {
				t.Fatalf("loopbackFallbackURL(%q) = %q, want %q", tt.in, gotURL, tt.wantURL)
			}
			if !tt.wantBool && gotURL != "" {
				t.Fatalf("loopbackFallbackURL(%q) returned url %q with bool=false, want empty", tt.in, gotURL)
			}
		})
	}
}

// TestIsLoopbackHost covers host classification including host:port forms.
func TestIsLoopbackHost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"127.0.0.1", true},
		{"127.0.0.1:10248", true},
		{"127.0.0.53", true},
		{"::1", true},
		{"[::1]:10250", true},
		{"localhost", true},
		{"LOCALHOST", true},
		{"localhost:10248", true},
		{"10.0.0.5", false},
		{"10.0.0.5:10248", false},
		{"example.com", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isLoopbackHost(tt.host); got != tt.want {
			t.Errorf("isLoopbackHost(%q) = %v, want %v", tt.host, got, tt.want)
		}
	}
}

// TestIsConnectionLevelError verifies the error classification used to decide
// whether the loopback fallback should fire.
func TestIsConnectionLevelError(t *testing.T) {
	t.Run("nil is not connection-level", func(t *testing.T) {
		if isConnectionLevelError(nil) {
			t.Fatal("nil should not be connection-level")
		}
	})

	t.Run("context canceled is not connection-level", func(t *testing.T) {
		if isConnectionLevelError(context.Canceled) {
			t.Fatal("context.Canceled should not be connection-level")
		}
	})

	t.Run("deadline exceeded is not connection-level", func(t *testing.T) {
		if isConnectionLevelError(context.DeadlineExceeded) {
			t.Fatal("context.DeadlineExceeded should not be connection-level")
		}
	})

	t.Run("dial OpError is connection-level", func(t *testing.T) {
		// A real connection-refused error: dial a closed port.
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		addr := ln.Addr().String()
		_ = ln.Close() // close so the port is refused

		_, dialErr := net.Dial("tcp", addr)
		if dialErr == nil {
			t.Skip("expected dial to fail against closed port; environment reused port")
		}
		if !isConnectionLevelError(dialErr) {
			t.Fatalf("dial error %v should be connection-level", dialErr)
		}
	})

	t.Run("plain error is not connection-level", func(t *testing.T) {
		if isConnectionLevelError(errors.New("boom")) {
			t.Fatal("plain error should not be connection-level")
		}
	})
}

// TestLoopbackFallback_PrimarySucceeds ensures no fallback request is made when
// the primary loopback probe succeeds. We count requests on the server.
func TestLoopbackFallback_PrimarySucceeds(t *testing.T) {
	var count int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// httptest binds to 127.0.0.1 by default, a recognized loopback.
	c := newLoopbackTestClient(srv.URL+"/healthz", srv.URL+"/metrics")

	if err := c.CheckHealth(context.Background()); err != nil {
		t.Fatalf("CheckHealth returned error: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 request, got %d", count)
	}
}

// TestLoopbackFallback_HTTP500NoFallback ensures an HTTP error status (kubelet
// answered) does NOT trigger a fallback. The server is hit exactly once.
func TestLoopbackFallback_HTTP500NoFallback(t *testing.T) {
	var count int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newLoopbackTestClient(srv.URL+"/healthz", srv.URL+"/metrics")

	err := c.CheckHealth(context.Background())
	if err == nil {
		t.Fatal("expected CheckHealth to fail on HTTP 500")
	}
	if count != 1 {
		t.Fatalf("HTTP 500 must not trigger fallback; expected 1 request, got %d", count)
	}
}

// TestLoopbackFallback_NonLoopbackNoFallback verifies the execution helper does
// not attempt a fallback for a non-loopback host that fails to dial.
func TestLoopbackFallback_NonLoopbackNoFallback(t *testing.T) {
	// 192.0.2.0/24 is TEST-NET-1 (RFC 5737), guaranteed not routable. Use a
	// port; the dial should fail fast-ish. Keep timeout short via client.
	c := newLoopbackTestClient("http://192.0.2.1:10248/healthz", "http://192.0.2.1:10250/metrics")

	req, err := http.NewRequestWithContext(context.Background(), "GET", c.healthzURL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	_, usedFallback, doErr := c.doRequestWithLoopbackFallback(req, "healthz")
	if doErr == nil {
		t.Skip("dial unexpectedly succeeded against TEST-NET address")
	}
	if usedFallback {
		t.Fatal("non-loopback host must not trigger loopback fallback")
	}
}

// TestLoopbackFallback_IPv4FailsIPv6Succeeds binds a server on the IPv6
// loopback only, targets the IPv4 loopback (which will refuse), and verifies
// the fallback retries [::1] and succeeds. Skips if ::1 cannot be bound.
func TestLoopbackFallback_IPv4FailsIPv6Succeeds(t *testing.T) {
	ln, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("cannot bind [::1] in this environment: %v", err)
	}

	srv := &httptest.Server{
		Listener: ln,
		Config: &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})},
	}
	srv.Start()
	defer srv.Close()

	// Extract the port the IPv6 server is listening on.
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}

	// Find a 127.0.0.1 port that is NOT listening, so the primary IPv4 probe
	// gets connection-refused. Reuse the IPv6 port number on 127.0.0.1: it is
	// very likely closed there since the server bound only to [::1].
	ipv4Target := "http://" + net.JoinHostPort("127.0.0.1", port) + "/healthz"

	// Sanity: confirm nothing answers on 127.0.0.1:port. If something does,
	// skip rather than produce a misleading result.
	if conn, derr := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", port), 200*time.Millisecond); derr == nil {
		_ = conn.Close()
		t.Skip("127.0.0.1 port unexpectedly in use; cannot exercise refused-primary path")
	}

	c := newLoopbackTestClient(ipv4Target, ipv4Target)

	req, err := http.NewRequestWithContext(context.Background(), "GET", c.healthzURL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, usedFallback, doErr := c.doRequestWithLoopbackFallback(req, "healthz")
	if doErr != nil {
		t.Fatalf("expected fallback to [::1] to succeed, got error: %v", doErr)
	}
	defer resp.Body.Close()

	if !usedFallback {
		t.Fatal("expected fallback to be used (IPv4 refused, IPv6 reachable)")
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from IPv6 server, got %d", resp.StatusCode)
	}
	if !strings.Contains(resp.Request.URL.Host, "::1") {
		t.Fatalf("expected final request host to be [::1], got %q", resp.Request.URL.Host)
	}
}
