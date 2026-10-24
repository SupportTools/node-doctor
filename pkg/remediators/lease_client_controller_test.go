package remediators

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/supporttools/node-doctor/pkg/controller"
)

func newControllerBackedLeaseClient(t *testing.T, maxConcurrent int, nodeName string) (*LeaseClient, *httptest.Server) {
	t.Helper()
	config := controller.DefaultControllerConfig()
	config.Coordination.MaxConcurrentRemediations = maxConcurrent
	server, err := controller.NewServer(config)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)

	lc, err := NewLeaseClient(newTestCoordinationConfig(ts.URL, false), nodeName)
	if err != nil {
		t.Fatalf("NewLeaseClient: %v", err)
	}
	return lc, ts
}

func TestRequestLease_RealControllerGrants(t *testing.T) {
	lc, _ := newControllerBackedLeaseClient(t, 1, "node-a")

	resp, err := lc.RequestLease(context.Background(), "systemd-restart", "kubelet unhealthy")
	if err != nil {
		t.Fatalf("RequestLease: %v", err)
	}
	if !resp.Approved {
		t.Fatalf("expected the controller to grant the lease, got denied: %q", resp.Message)
	}
	if resp.LeaseID == "" {
		t.Error("expected a lease ID from the controller")
	}
	if !lc.HasActiveLease() {
		t.Error("expected the client to hold the granted lease")
	}
}

func TestRequestLease_RealControllerDeniesOverLimit(t *testing.T) {
	first, ts := newControllerBackedLeaseClient(t, 1, "node-a")
	if resp, err := first.RequestLease(context.Background(), "systemd-restart", "first"); err != nil || !resp.Approved {
		t.Fatalf("first lease should be granted, got resp=%+v err=%v", resp, err)
	}

	second, err := NewLeaseClient(newTestCoordinationConfig(ts.URL, false), "node-b")
	if err != nil {
		t.Fatalf("NewLeaseClient: %v", err)
	}
	resp, err := second.RequestLease(context.Background(), "systemd-restart", "second")
	if err != nil {
		t.Fatalf("a denial is not a transport error, got: %v", err)
	}
	if resp.Approved {
		t.Fatal("expected the controller to deny a lease over maxConcurrentRemediations")
	}
}
