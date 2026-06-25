package remediators

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/controller"
	"github.com/supporttools/node-doctor/pkg/types"
)

// newTestCoordinationConfig builds a RemediationCoordinationConfig pointed at the
// given controller URL with short timeouts/retries so the unreachable cases fail
// fast rather than blocking the test suite.
func newTestCoordinationConfig(controllerURL string, fallback bool) *types.RemediationCoordinationConfig {
	return &types.RemediationCoordinationConfig{
		Enabled:               true,
		ControllerURL:         controllerURL,
		LeaseTimeout:          5 * time.Minute,
		RequestTimeout:        500 * time.Millisecond,
		FallbackOnUnreachable: fallback,
		MaxRetries:            1,
		RetryInterval:         10 * time.Millisecond,
	}
}

// TestRequestLease_ReachableGrants verifies that when the controller is reachable
// and approves the lease, RequestLease returns Approved=true with no error.
func TestRequestLease_ReachableGrants(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Respond with an approved lease, mirroring the controller's
		// LeaseResponse JSON shape (see pkg/controller/types.go).
		resp := controller.LeaseResponse{
			LeaseID:   "lease-123",
			Approved:  true,
			ExpiresAt: time.Now().Add(5 * time.Minute),
			Message:   "granted",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	lc, err := NewLeaseClient(newTestCoordinationConfig(server.URL, false), "test-node")
	if err != nil {
		t.Fatalf("NewLeaseClient returned error: %v", err)
	}

	resp, err := lc.RequestLease(context.Background(), "reboot", "test reason")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if resp == nil {
		t.Fatal("expected a lease response, got nil")
	}
	if !resp.Approved {
		t.Errorf("expected Approved=true, got Approved=false (message: %q)", resp.Message)
	}
}

// TestRequestLease_ReachableDenies verifies that when the controller is reachable
// but denies the lease (concurrency limit, HTTP 429), RequestLease returns the
// denial with Approved=false and no transport error.
func TestRequestLease_ReachableDenies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := controller.LeaseResponse{
			Approved: false,
			Message:  "denied: too many concurrent remediations",
		}
		w.Header().Set("Content-Type", "application/json")
		// 429 is mapped by sendLeaseRequest to a denied (non-error) response.
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	lc, err := NewLeaseClient(newTestCoordinationConfig(server.URL, false), "test-node")
	if err != nil {
		t.Fatalf("NewLeaseClient returned error: %v", err)
	}

	resp, err := lc.RequestLease(context.Background(), "reboot", "test reason")
	if err != nil {
		t.Fatalf("expected no transport error on denial, got: %v", err)
	}
	if resp == nil {
		t.Fatal("expected a lease response, got nil")
	}
	if resp.Approved {
		t.Error("expected Approved=false (denied), got Approved=true")
	}
}

// TestRequestLease_UnreachableFallbackTrue verifies the fallback path.
//
// STORM RISK WARNING: With FallbackOnUnreachable=true, an unreachable controller
// causes RequestLease to return a FAKE approval (Approved=true) WITHOUT any real
// lease coordination. This path is what ALLOWS A CLUSTER-WIDE REMEDIATION STORM:
// during a controller outage, EVERY DaemonSet node self-approves its own lease
// simultaneously, so ALL nodes can remediate (e.g. reboot/drain) at the same time.
// The lease coordination exists precisely to serialize remediations and prevent
// this thundering-herd behavior; enabling fallback trades that safety for
// availability. This test documents and pins that intentional (dangerous) behavior.
func TestRequestLease_UnreachableFallbackTrue(t *testing.T) {
	// Create a server then immediately close it so the URL is valid but
	// every request fails to connect (controller is unreachable).
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	unreachableURL := server.URL
	server.Close()

	lc, err := NewLeaseClient(newTestCoordinationConfig(unreachableURL, true), "test-node")
	if err != nil {
		t.Fatalf("NewLeaseClient returned error: %v", err)
	}

	resp, err := lc.RequestLease(context.Background(), "reboot", "test reason")
	if err != nil {
		t.Fatalf("expected no error with fallback enabled, got: %v", err)
	}
	if resp == nil {
		t.Fatal("expected a fallback lease response, got nil")
	}
	// FAKE approval: no controller ever granted this lease.
	if !resp.Approved {
		t.Error("expected fallback Approved=true, got Approved=false")
	}
	if resp.Message == "" {
		t.Error("expected a fallback message describing the unreachable controller")
	}
}

// TestRequestLease_UnreachableFallbackFalse verifies the safe (default) path:
// with FallbackOnUnreachable=false, an unreachable controller causes RequestLease
// to return an ERROR and NO approval, blocking remediation rather than risking a
// cluster-wide storm.
func TestRequestLease_UnreachableFallbackFalse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	unreachableURL := server.URL
	server.Close()

	lc, err := NewLeaseClient(newTestCoordinationConfig(unreachableURL, false), "test-node")
	if err != nil {
		t.Fatalf("NewLeaseClient returned error: %v", err)
	}

	resp, err := lc.RequestLease(context.Background(), "reboot", "test reason")
	if err == nil {
		t.Fatal("expected an error when controller is unreachable and fallback is disabled, got nil")
	}
	if resp != nil {
		t.Errorf("expected no lease response on error, got: %+v", resp)
	}
}
