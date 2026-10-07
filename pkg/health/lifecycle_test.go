package health

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

func TestProbeHandlers_SetContentType(t *testing.T) {
	cases := []struct {
		path     string
		healthy  bool
		ready    bool
		wantCode int
	}{
		{"/healthz", true, true, http.StatusOK},
		{"/healthz", false, true, http.StatusServiceUnavailable},
		{"/ready", true, true, http.StatusOK},
		{"/ready", true, false, http.StatusServiceUnavailable},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s_%d", tc.path, tc.wantCode), func(t *testing.T) {
			srv, err := NewServer(&Config{Enabled: true})
			if err != nil {
				t.Fatalf("NewServer: %v", err)
			}
			srv.SetHealthy(tc.healthy)
			srv.SetReady(tc.ready)

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			if tc.path == "/healthz" {
				srv.handleHealthz(w, req)
			} else {
				srv.handleReady(w, req)
			}

			if w.Code != tc.wantCode {
				t.Errorf("status = %d, want %d", w.Code, tc.wantCode)
			}
			if ct := w.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
		})
	}
}

func TestStatus_ReportsSetVersion(t *testing.T) {
	srv, err := NewServer(&Config{Enabled: true})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.SetVersion("v1.2.3")

	w := httptest.NewRecorder()
	srv.handleStatus(w, httptest.NewRequest(http.MethodGet, "/status", nil))

	var resp StatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := resp.Metadata["version"]; got != "v1.2.3" {
		t.Fatalf("version = %q, want v1.2.3", got)
	}
}

func TestSetReadyFalse_HoldsAgainstIncomingStatus(t *testing.T) {
	srv, err := NewServer(&Config{Enabled: true})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.UpdateStatus(&types.Status{Source: "m"})
	srv.SetReady(false)
	srv.UpdateStatus(&types.Status{Source: "m"})

	w := httptest.NewRecorder()
	srv.handleReady(w, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("/ready = %d after SetReady(false), want 503 even though a status arrived", w.Code)
	}

	srv.SetReady(true)
	w = httptest.NewRecorder()
	srv.handleReady(w, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("/ready = %d after SetReady(true), want 200", w.Code)
	}
}

func TestStart_StopsWhenContextCancelled(t *testing.T) {
	srv, err := NewServer(&Config{Enabled: true, Port: 0})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	addr := srv.httpServer.Addr

	resp, err := http.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatalf("server not serving before cancel: %v", err)
	}
	_ = resp.Body.Close()

	cancel()

	deadline := time.Now().Add(3 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err != nil {
			break
		}
		_ = conn.Close()
		if time.Now().After(deadline) {
			t.Fatal("server still accepting connections 3s after context cancel")
		}
		time.Sleep(20 * time.Millisecond)
	}

	srv.mu.RLock()
	started := srv.started
	srv.mu.RUnlock()
	if started {
		t.Fatal("server still marked started after context cancel")
	}
	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop after context cancel: %v", err)
	}
}

func TestStart_DisabledIsNoop(t *testing.T) {
	srv, err := NewServer(&Config{Enabled: false, Port: 0})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if srv.httpServer != nil {
		t.Fatal("disabled server must not create an HTTP server")
	}
	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}
