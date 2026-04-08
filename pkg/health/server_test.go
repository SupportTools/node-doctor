package health

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// mockRemediationHistory is a mock implementation of RemediationHistoryProvider for testing.
type mockRemediationHistory struct {
	records []map[string]interface{}
}

func (m *mockRemediationHistory) GetHistory(limit int) interface{} {
	if limit == 0 || limit >= len(m.records) {
		return m.records
	}
	return m.records[:limit]
}

func TestNewServer(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name:    "nil config",
			config:  nil,
			wantErr: true,
		},
		{
			name: "valid config with defaults",
			config: &Config{
				Enabled: true,
			},
			wantErr: false,
		},
		{
			name: "valid config with custom values",
			config: &Config{
				Enabled:      true,
				BindAddress:  "127.0.0.1",
				Port:         9090,
				ReadTimeout:  10 * time.Second,
				WriteTimeout: 20 * time.Second,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, err := NewServer(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewServer() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if server == nil {
					t.Error("NewServer() returned nil server")
				}
			}
		})
	}
}

func TestServer_SetHealthy(t *testing.T) {
	config := &Config{Enabled: true}
	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Initial state should be healthy
	if !server.healthy {
		t.Error("Server should be healthy initially")
	}

	// Set unhealthy
	server.SetHealthy(false)
	if server.healthy {
		t.Error("Server should be unhealthy after SetHealthy(false)")
	}

	// Set healthy again
	server.SetHealthy(true)
	if !server.healthy {
		t.Error("Server should be healthy after SetHealthy(true)")
	}
}

func TestServer_SetReady(t *testing.T) {
	config := &Config{Enabled: true}
	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Initial state should be not ready
	if server.ready {
		t.Error("Server should not be ready initially")
	}

	// Set ready
	server.SetReady(true)
	if !server.ready {
		t.Error("Server should be ready after SetReady(true)")
	}

	// Set not ready
	server.SetReady(false)
	if server.ready {
		t.Error("Server should not be ready after SetReady(false)")
	}
}

func TestServer_AddHealthCheck(t *testing.T) {
	config := &Config{Enabled: true}
	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Add a health check
	server.AddHealthCheck("test-check", func() error {
		return nil
	})

	if len(server.healthChecks) != 1 {
		t.Errorf("Expected 1 health check, got %d", len(server.healthChecks))
	}

	if server.healthChecks[0].Name != "test-check" {
		t.Errorf("Expected health check name 'test-check', got '%s'", server.healthChecks[0].Name)
	}
}

func TestServer_SetRemediationHistory(t *testing.T) {
	config := &Config{Enabled: true}
	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Initially should be nil
	if server.remediationHistory != nil {
		t.Error("RemediationHistory should be nil initially")
	}

	// Set provider
	mockProvider := &mockRemediationHistory{
		records: []map[string]interface{}{
			{"test": "record"},
		},
	}
	server.SetRemediationHistory(mockProvider)

	if server.remediationHistory == nil {
		t.Error("RemediationHistory should not be nil after SetRemediationHistory")
	}
}

func TestServer_handleHealthz(t *testing.T) {
	tests := []struct {
		name           string
		healthy        bool
		healthChecks   []HealthCheck
		expectedStatus int
		expectedHealth string
	}{
		{
			name:           "healthy server",
			healthy:        true,
			healthChecks:   nil,
			expectedStatus: http.StatusOK,
			expectedHealth: "ok",
		},
		{
			name:           "unhealthy server",
			healthy:        false,
			healthChecks:   nil,
			expectedStatus: http.StatusServiceUnavailable,
			expectedHealth: "unhealthy",
		},
		{
			name:    "healthy with passing health check",
			healthy: true,
			healthChecks: []HealthCheck{
				{
					Name:  "test-check",
					Check: func() error { return nil },
				},
			},
			expectedStatus: http.StatusOK,
			expectedHealth: "ok",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{Enabled: true}
			server, err := NewServer(config)
			if err != nil {
				t.Fatalf("Failed to create server: %v", err)
			}

			server.SetHealthy(tt.healthy)
			for _, hc := range tt.healthChecks {
				server.AddHealthCheck(hc.Name, hc.Check)
			}

			req := httptest.NewRequest("GET", "/healthz", nil)
			w := httptest.NewRecorder()

			server.handleHealthz(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			var response HealthResponse
			if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			if response.Status != tt.expectedHealth {
				t.Errorf("Expected health status '%s', got '%s'", tt.expectedHealth, response.Status)
			}
		})
	}
}

func TestServer_handleReady(t *testing.T) {
	tests := []struct {
		name           string
		ready          bool
		expectedStatus int
		expectedReady  bool
	}{
		{
			name:           "ready",
			ready:          true,
			expectedStatus: http.StatusOK,
			expectedReady:  true,
		},
		{
			name:           "not ready",
			ready:          false,
			expectedStatus: http.StatusServiceUnavailable,
			expectedReady:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{Enabled: true}
			server, err := NewServer(config)
			if err != nil {
				t.Fatalf("Failed to create server: %v", err)
			}

			server.SetReady(tt.ready)

			req := httptest.NewRequest("GET", "/ready", nil)
			w := httptest.NewRecorder()

			server.handleReady(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			var response ReadinessResponse
			if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			if response.Ready != tt.expectedReady {
				t.Errorf("Expected ready status %v, got %v", tt.expectedReady, response.Ready)
			}
		})
	}
}

func TestServer_handleStatus(t *testing.T) {
	config := &Config{Enabled: true}
	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	server.SetHealthy(true)
	server.SetReady(true)

	req := httptest.NewRequest("GET", "/status", nil)
	w := httptest.NewRecorder()

	server.handleStatus(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response StatusResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !response.Healthy {
		t.Error("Expected healthy status")
	}

	if !response.Ready {
		t.Error("Expected ready status")
	}

	if response.Uptime == "" {
		t.Error("Expected non-empty uptime")
	}
}

func TestServer_handleRemediationHistory_NoProvider(t *testing.T) {
	config := &Config{Enabled: true}
	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Don't set a remediation history provider

	req := httptest.NewRequest("GET", "/remediation/history", nil)
	w := httptest.NewRecorder()

	server.handleRemediationHistory(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}

	var response map[string]string
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response["error"] == "" {
		t.Error("Expected error message in response")
	}
}

func TestServer_handleRemediationHistory_WithProvider(t *testing.T) {
	config := &Config{Enabled: true}
	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Create mock history with 5 records
	mockHistory := &mockRemediationHistory{
		records: []map[string]interface{}{
			{"remediatorType": "test-1", "success": true},
			{"remediatorType": "test-2", "success": true},
			{"remediatorType": "test-3", "success": false},
			{"remediatorType": "test-4", "success": true},
			{"remediatorType": "test-5", "success": true},
		},
	}
	server.SetRemediationHistory(mockHistory)

	tests := []struct {
		name          string
		queryString   string
		expectedCount int
		expectedLimit int
	}{
		{
			name:          "no limit - default 100",
			queryString:   "",
			expectedCount: 5, // all records since we have < 100
			expectedLimit: 100,
		},
		{
			name:          "limit 2",
			queryString:   "?limit=2",
			expectedCount: 2,
			expectedLimit: 2,
		},
		{
			name:          "limit 10 (more than available)",
			queryString:   "?limit=10",
			expectedCount: 5, // only 5 records available
			expectedLimit: 10,
		},
		{
			name:          "limit 0 (all records)",
			queryString:   "?limit=0",
			expectedCount: 5,
			expectedLimit: 0,
		},
		{
			name:          "negative limit (treated as 0)",
			queryString:   "?limit=-5",
			expectedCount: 5,
			expectedLimit: 0,
		},
		{
			name:          "limit > 1000 (capped at 1000)",
			queryString:   "?limit=2000",
			expectedCount: 5, // only 5 records available
			expectedLimit: 1000,
		},
		{
			name:          "invalid limit (ignored, use default)",
			queryString:   "?limit=invalid",
			expectedCount: 5,
			expectedLimit: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/remediation/history"+tt.queryString, nil)
			w := httptest.NewRecorder()

			server.handleRemediationHistory(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
			}

			var response map[string]interface{}
			if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			// Check count
			count, ok := response["count"].(float64)
			if !ok {
				t.Fatal("count field missing or not a number")
			}
			if int(count) != tt.expectedCount {
				t.Errorf("Expected count %d, got %d", tt.expectedCount, int(count))
			}

			// Check limit
			limit, ok := response["limit"].(float64)
			if !ok {
				t.Fatal("limit field missing or not a number")
			}
			if int(limit) != tt.expectedLimit {
				t.Errorf("Expected limit %d, got %d", tt.expectedLimit, int(limit))
			}

			// Check history array
			history, ok := response["history"].([]interface{})
			if !ok {
				t.Fatal("history field missing or not an array")
			}
			if len(history) != tt.expectedCount {
				t.Errorf("Expected %d history records, got %d", tt.expectedCount, len(history))
			}
		})
	}
}

func TestServer_StartStop(t *testing.T) {
	// Grab a free port then release it for the server to bind.
	tmp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}
	freePort := tmp.Addr().(*net.TCPAddr).Port
	tmp.Close()

	config := &Config{
		Enabled:     true,
		BindAddress: "127.0.0.1",
		Port:        freePort,
	}
	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Start server
	ctx := context.Background()
	if err := server.Start(ctx); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	if !server.started {
		t.Error("Server should be marked as started")
	}

	// Try to start again (should fail)
	if err := server.Start(ctx); err == nil {
		t.Error("Expected error when starting already started server")
	}

	// Stop server
	if err := server.Stop(); err != nil {
		t.Fatalf("Failed to stop server: %v", err)
	}

	if server.started {
		t.Error("Server should not be marked as started after stop")
	}

	// Stop again (should succeed without error)
	if err := server.Stop(); err != nil {
		t.Error("Stopping already stopped server should not error")
	}
}

func TestServer_Name(t *testing.T) {
	config := &Config{Enabled: true}
	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	if server.Name() != "health-server" {
		t.Errorf("Expected name 'health-server', got '%s'", server.Name())
	}
}

func TestServer_StartBindFailure(t *testing.T) {
	// Occupy a port with a raw listener so the health server cannot bind to it.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to grab a free port: %v", err)
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port

	config := &Config{
		Enabled:     true,
		BindAddress: "127.0.0.1",
		Port:        port,
	}
	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	ctx := context.Background()
	if err := server.Start(ctx); err == nil {
		server.Stop()
		t.Fatal("Start() should fail when port is already in use")
	}

	// Server must not be marked as started after a bind failure.
	if server.started {
		t.Error("server.started should be false after a bind failure")
	}
}

// TestServer_Stop_NoDeadlockWithInFlightHandler is a regression test for the
// "Shutdown under lock" deadlock.
//
// Root cause: the original Stop() held s.mu.Lock() across the
// httpServer.Shutdown() call.  Any connection that net/http had already
// accepted — but whose handler goroutine had not yet called s.mu.RLock() —
// would block on RLock() after Stop() acquired the write lock.  Shutdown()
// would then wait for that handler to return, the handler would wait for the
// lock, and the whole thing would stall until the 5-second Shutdown context
// deadline fired.
//
// Fix: release s.mu before calling Shutdown() so handlers can still acquire
// the read lock freely.
//
// This test approximates the scenario: it parks an in-flight handler inside
// s.mu.RLock() (via a blocking health-check callback) and calls Stop()
// concurrently, asserting that Stop() returns promptly once the handler
// finishes — not after a multi-second timeout.
func TestServer_Stop_NoDeadlockWithInFlightHandler(t *testing.T) {
	t.Parallel()

	// blockEntry is closed (once) when the health-check is running — i.e.
	// the /healthz handler is holding s.mu.RLock.
	// releaseBlock is closed to let the health-check (and thus the handler)
	// return.
	blockEntry := make(chan struct{})
	releaseBlock := make(chan struct{})
	var entered sync.Once

	tmp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}
	freePort := tmp.Addr().(*net.TCPAddr).Port
	tmp.Close()

	config := &Config{
		Enabled:     true,
		BindAddress: "127.0.0.1",
		Port:        freePort,
	}
	srv, err := NewServer(config)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	// This check runs while handleHealthz holds s.mu.RLock.
	srv.AddHealthCheck("blocker", func() error {
		entered.Do(func() { close(blockEntry) })
		<-releaseBlock
		return nil
	})

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	addr := srv.httpServer.Addr

	// Fire /healthz in the background; it will park inside the health-check
	// callback while holding s.mu.RLock.
	go func() {
		resp, err := http.Get("http://" + addr + "/healthz") //nolint:noctx
		if err == nil {
			resp.Body.Close()
		}
	}()

	// Wait until the handler is truly inside the mutex.
	select {
	case <-blockEntry:
	case <-time.After(3 * time.Second):
		close(releaseBlock)
		t.Fatal("timed out waiting for handler to enter the mutex")
	}

	// Call Stop() while the in-flight handler holds s.mu.RLock.
	// Stop() will block on s.mu.Lock() until we release the handler.
	stopDone := make(chan error, 1)
	go func() { stopDone <- srv.Stop() }()

	// Give Stop() time to reach s.mu.Lock() (it will be blocked there).
	time.Sleep(30 * time.Millisecond)

	// Release the handler. With the fix: Stop() acquires Lock, releases it,
	// calls Shutdown() (lock-free) → Shutdown finds nothing in-flight →
	// returns quickly. With the bug: Stop() would hold Lock through Shutdown(),
	// blocking any handler that arrived after Lock was taken.
	close(releaseBlock)

	// Stop() must return well within 1 second.
	select {
	case err := <-stopDone:
		if err != nil {
			t.Errorf("Stop() returned unexpected error: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Stop() did not return within 1 s after handler released — likely deadlock")
	}
}
