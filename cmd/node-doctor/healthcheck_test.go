package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/supporttools/node-doctor/pkg/health"
)

// TestRunHealthCheck exercises the exec-probe health check against a real health
// server unix socket. This is the code path the Kubernetes exec probes invoke, and
// the reason a1pinode01-class hostPort-8080 conflicts no longer crashloop the pod.
func TestRunHealthCheck(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "health.sock")
	srv, err := health.NewServer(&health.Config{
		Enabled:    true,
		Port:       0, // ephemeral TCP, no conflict
		SocketPath: sockPath,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	// Liveness (/healthz) should be OK immediately.
	if rc := runHealthCheck(sockPath, "/healthz"); rc != 0 {
		t.Errorf("runHealthCheck(/healthz) = %d, want 0", rc)
	}

	// Readiness (/ready) is 503 (exit 1) until a status/ready flag is set.
	if rc := runHealthCheck(sockPath, "/ready"); rc != 1 {
		t.Errorf("runHealthCheck(/ready) before ready = %d, want 1", rc)
	}
	srv.SetReady(true)
	if rc := runHealthCheck(sockPath, "/ready"); rc != 0 {
		t.Errorf("runHealthCheck(/ready) after ready = %d, want 0", rc)
	}

	// A missing socket must fail (exit 1), not hang or panic.
	if rc := runHealthCheck(filepath.Join(t.TempDir(), "nope.sock"), "/healthz"); rc != 1 {
		t.Errorf("runHealthCheck(missing socket) = %d, want 1", rc)
	}
}
