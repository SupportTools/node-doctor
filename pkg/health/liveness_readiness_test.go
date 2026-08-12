package health

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// newSocketServer starts a health server on a per-pod unix socket only (Port 0
// still binds an ephemeral TCP listener, which is harmless) and returns an HTTP
// client wired to the socket — the same transport the `-healthcheck` and
// `-healthcheck-ready` exec probes use in production.
func newSocketServer(t *testing.T) (*Server, *http.Client) {
	t.Helper()

	socket := filepath.Join(t.TempDir(), "health.sock")
	srv, err := NewServer(&Config{
		Enabled:      true,
		BindAddress:  "127.0.0.1",
		Port:         0,
		SocketPath:   socket,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })

	client := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socket)
			},
		},
	}
	return srv, client
}

func probe(t *testing.T, client *http.Client, path string) int {
	t.Helper()
	resp, err := client.Get("http://localhost" + path)
	if err != nil {
		t.Fatalf("probe %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

func probeBody(t *testing.T, client *http.Client, path string) (int, ReadinessResponse) {
	t.Helper()
	resp, err := client.Get("http://localhost" + path)
	if err != nil {
		t.Fatalf("probe %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out ReadinessResponse
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// TestLivenessIgnoresDownstreamFailures is the central #node-doctor-246 guard.
//
// node-doctor runs on exactly the nodes whose API server / DNS / network are
// broken. If a downstream failure could fail LIVENESS, the kubelet would kill
// and restart the agent forever on those nodes — losing the diagnostics at the
// precise moment they matter. Downstream failure must produce NotReady, never a
// restart.
func TestLivenessIgnoresDownstreamFailures(t *testing.T) {
	srv, client := newSocketServer(t)

	// Get the agent to a fully healthy, ready baseline.
	srv.UpdateStatus(&types.Status{Source: "test-monitor"})
	if code := probe(t, client, "/healthz"); code != http.StatusOK {
		t.Fatalf("baseline liveness = %d, want 200", code)
	}
	if code := probe(t, client, "/ready"); code != http.StatusOK {
		t.Fatalf("baseline readiness = %d, want 200", code)
	}

	// Now every downstream the agent depends on breaks, persistently.
	failDependency(srv, "exporter/kubernetes", errors.New("connection refused: apiserver unreachable"))
	failDependency(srv, "exporter/http", errors.New("webhook timeout"))

	// LIVENESS must be untouched — a restart cannot fix an unreachable apiserver.
	if code := probe(t, client, "/healthz"); code != http.StatusOK {
		t.Errorf("liveness = %d, want 200. A downstream exporter failure must NEVER trip liveness: "+
			"the kubelet would restart node-doctor in a loop on exactly the degraded nodes it exists to observe.", code)
	}

	// READINESS must reflect it.
	code, body := probeBody(t, client, "/ready")
	if code != http.StatusServiceUnavailable {
		t.Errorf("readiness = %d, want 503 when a downstream exporter is failing", code)
	}
	if body.Ready {
		t.Error("readiness body must report ready=false on downstream failure")
	}
	if body.Message == "" {
		t.Error("readiness body should name the failing dependency for operators")
	}
}

// TestReadinessRecoversWhenDependencyRecovers ensures the NotReady state is not
// sticky — the pod must return to service once the downstream heals.
func TestReadinessRecoversWhenDependencyRecovers(t *testing.T) {
	srv, client := newSocketServer(t)
	srv.UpdateStatus(&types.Status{Source: "test-monitor"})

	failDependency(srv, "exporter/kubernetes", errors.New("apiserver down"))
	if code := probe(t, client, "/ready"); code != http.StatusServiceUnavailable {
		t.Fatalf("readiness = %d, want 503 while the dependency is down", code)
	}

	srv.SetDependencyStatus("exporter/kubernetes", nil)
	if code := probe(t, client, "/ready"); code != http.StatusOK {
		t.Errorf("readiness = %d, want 200 after the dependency recovered", code)
	}
}

// TestReadinessFalseUntilFirstMonitorStatus preserves the existing contract:
// the agent is not ready until it has actually produced a monitor status.
func TestReadinessFalseUntilFirstMonitorStatus(t *testing.T) {
	srv, client := newSocketServer(t)

	if code := probe(t, client, "/ready"); code != http.StatusServiceUnavailable {
		t.Errorf("readiness = %d, want 503 before any monitor has run", code)
	}
	// But the process is alive and must not be restarted while it starts up.
	if code := probe(t, client, "/healthz"); code != http.StatusOK {
		t.Errorf("liveness = %d, want 200 during startup — a slow start must not be a restart", code)
	}

	srv.UpdateStatus(&types.Status{Source: "test-monitor"})
	if code := probe(t, client, "/ready"); code != http.StatusOK {
		t.Errorf("readiness = %d, want 200 after the first monitor status", code)
	}
}

// TestReadinessChecksAffectOnlyReadiness covers the AddReadinessCheck path.
func TestReadinessChecksAffectOnlyReadiness(t *testing.T) {
	srv, client := newSocketServer(t)
	srv.UpdateStatus(&types.Status{Source: "test-monitor"})

	failing := true
	srv.AddReadinessCheck("cluster-reachable", func() error {
		if failing {
			return errors.New("cannot reach cluster")
		}
		return nil
	})

	if code := probe(t, client, "/ready"); code != http.StatusServiceUnavailable {
		t.Errorf("readiness = %d, want 503 when a readiness check fails", code)
	}
	if code := probe(t, client, "/healthz"); code != http.StatusOK {
		t.Errorf("liveness = %d, want 200 — readiness checks must not affect liveness", code)
	}

	failing = false
	if code := probe(t, client, "/ready"); code != http.StatusOK {
		t.Errorf("readiness = %d, want 200 once the readiness check passes", code)
	}
}

// TestLivenessCanStillFailOnProcessInternalCheck confirms liveness is not
// hard-wired to 200: a genuinely wedged process must still be restartable.
func TestLivenessCanStillFailOnProcessInternalCheck(t *testing.T) {
	srv, client := newSocketServer(t)

	srv.AddHealthCheck("status-processor", func() error {
		return errors.New("status processing goroutine is wedged")
	})

	if code := probe(t, client, "/healthz"); code != http.StatusServiceUnavailable {
		t.Errorf("liveness = %d, want 503 when a process-internal check fails — "+
			"an actually-wedged process must still be restartable", code)
	}
}

// TestSetHealthyDrivesLiveness covers the explicit liveness setter.
func TestSetHealthyDrivesLiveness(t *testing.T) {
	srv, client := newSocketServer(t)

	if code := probe(t, client, "/healthz"); code != http.StatusOK {
		t.Fatalf("baseline liveness = %d, want 200", code)
	}
	srv.SetHealthy(false)
	if code := probe(t, client, "/healthz"); code != http.StatusServiceUnavailable {
		t.Errorf("liveness = %d, want 503 after SetHealthy(false)", code)
	}
}

// TestDependencyStatusIsIdempotent guards the map bookkeeping: however many
// failures pile up, ONE success must fully resolve the dependency.
func TestDependencyStatusIsIdempotent(t *testing.T) {
	srv, client := newSocketServer(t)
	srv.UpdateStatus(&types.Status{Source: "m"})

	for i := 0; i < 10; i++ {
		srv.SetDependencyStatus("exporter/kubernetes", errors.New("down"))
	}
	if code := probe(t, client, "/ready"); code != http.StatusServiceUnavailable {
		t.Fatalf("readiness = %d, want 503", code)
	}

	srv.SetDependencyStatus("exporter/kubernetes", nil)
	if code := probe(t, client, "/ready"); code != http.StatusOK {
		t.Errorf("readiness = %d, want 200 after a single successful export cleared the dependency", code)
	}
}

// TestTransientDependencyBlipDoesNotFlipReadiness pins the hysteresis.
//
// node-doctor is a DaemonSet: if one transient export error flipped every pod
// to NotReady, a blip that has already healed would stall rolling updates
// fleet-wide. A failure must be SUSTAINED before it counts.
func TestTransientDependencyBlipDoesNotFlipReadiness(t *testing.T) {
	srv, client := newSocketServer(t)
	srv.UpdateStatus(&types.Status{Source: "m"})

	// A single blip, below the threshold.
	srv.SetDependencyStatus("exporter/kubernetes", errors.New("transient timeout"))
	if code := probe(t, client, "/ready"); code != http.StatusOK {
		t.Errorf("readiness = %d, want 200: a single transient export failure must not "+
			"make the pod NotReady and stall DaemonSet rollouts fleet-wide", code)
	}

	// Recovery resets the counter, so a later blip also does not trip it.
	srv.SetDependencyStatus("exporter/kubernetes", nil)
	srv.SetDependencyStatus("exporter/kubernetes", errors.New("another blip"))
	if code := probe(t, client, "/ready"); code != http.StatusOK {
		t.Errorf("readiness = %d, want 200: a success must reset the consecutive-failure count", code)
	}

	// Sustained failure DOES trip it.
	failDependency(srv, "exporter/kubernetes", errors.New("apiserver really is down"))
	if code := probe(t, client, "/ready"); code != http.StatusServiceUnavailable {
		t.Errorf("readiness = %d, want 503 once the failure is sustained", code)
	}
}

// failDependency reports enough consecutive failures to cross the threshold.
func failDependency(srv *Server, name string, err error) {
	for i := 0; i < dependencyFailureThreshold; i++ {
		srv.SetDependencyStatus(name, err)
	}
}
