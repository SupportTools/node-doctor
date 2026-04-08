package prometheus

import (
	"context"
	"net"
	"testing"
	"time"
)

// freePort allocates a free TCP port on the loopback interface and immediately
// releases it. There is a small TOCTOU window, but this is acceptable for tests
// and far preferable to hardcoded port numbers that can collide across test runs.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

// waitForServerReady polls until the server at addr is accepting TCP connections,
// or until timeout elapses. Use after Start() to avoid race-prone time.Sleep calls.
func waitForServerReady(addr string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
			if err == nil {
				conn.Close()
				return nil
			}
		}
	}
}

// waitForPortClosed polls until addr refuses new connections, confirming that the
// server has fully shut down. Use after Stop() instead of a fixed-duration sleep.
func waitForPortClosed(addr string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
			if err != nil {
				return nil // port is closed — server is down
			}
			conn.Close()
		}
	}
}
