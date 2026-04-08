package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewServer(t *testing.T) {
	t.Run("with nil config uses defaults", func(t *testing.T) {
		server, err := NewServer(nil)
		if err != nil {
			t.Fatalf("NewServer() error = %v", err)
		}
		if server == nil {
			t.Fatal("NewServer() returned nil")
		}
		if server.config.Server.Port != 8080 {
			t.Errorf("expected default port 8080, got %d", server.config.Server.Port)
		}
	})

	t.Run("with custom config", func(t *testing.T) {
		config := &ControllerConfig{
			Server: ServerConfig{
				BindAddress: "127.0.0.1",
				Port:        9090,
			},
		}
		server, err := NewServer(config)
		if err != nil {
			t.Fatalf("NewServer() error = %v", err)
		}
		if server.config.Server.Port != 9090 {
			t.Errorf("expected port 9090, got %d", server.config.Server.Port)
		}
	})
}

func TestServer_Healthz(t *testing.T) {
	server, _ := NewServer(nil)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	server.handleHealthz(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response["status"] != "ok" {
		t.Errorf("expected status 'ok', got %v", response["status"])
	}
}

func TestServer_Readyz(t *testing.T) {
	server, _ := NewServer(nil)

	t.Run("not ready initially", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		w := httptest.NewRecorder()

		server.handleReadyz(w, req)

		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("expected status 503, got %d", w.Code)
		}
	})

	t.Run("ready after SetReady", func(t *testing.T) {
		server.SetReady(true)

		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		w := httptest.NewRecorder()

		server.handleReadyz(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})
}

func TestServer_ReportIngestion(t *testing.T) {
	server, _ := NewServer(nil)

	t.Run("accepts valid report", func(t *testing.T) {
		report := NodeReport{
			NodeName:      "test-node",
			Timestamp:     time.Now(),
			OverallHealth: HealthStatusHealthy,
		}
		body, _ := json.Marshal(report)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/reports", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.handleReports(w, req)

		if w.Code != http.StatusAccepted {
			t.Errorf("expected status 202, got %d", w.Code)
		}

		// Verify report was stored
		server.mu.RLock()
		_, exists := server.nodeReports["test-node"]
		server.mu.RUnlock()

		if !exists {
			t.Error("report was not stored")
		}
	})

	t.Run("rejects report without node name", func(t *testing.T) {
		report := NodeReport{
			Timestamp:     time.Now(),
			OverallHealth: HealthStatusHealthy,
		}
		body, _ := json.Marshal(report)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/reports", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.handleReports(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("rejects GET method", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/reports", nil)
		w := httptest.NewRecorder()

		server.handleReports(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected status 405, got %d", w.Code)
		}
	})
}

func TestServer_ClusterStatus(t *testing.T) {
	server, _ := NewServer(nil)

	t.Run("returns empty cluster status", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/status", nil)
		w := httptest.NewRecorder()

		server.handleClusterStatus(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}

		var response APIResponse
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if !response.Success {
			t.Error("expected success=true")
		}
	})

	t.Run("reflects stored reports", func(t *testing.T) {
		// Add a report
		server.mu.Lock()
		server.nodeReports["node-1"] = &NodeReport{
			NodeName:      "node-1",
			OverallHealth: HealthStatusHealthy,
			Timestamp:     time.Now(),
		}
		server.nodeReports["node-2"] = &NodeReport{
			NodeName:      "node-2",
			OverallHealth: HealthStatusDegraded,
			Timestamp:     time.Now(),
			ActiveProblems: []ProblemSummary{
				{Type: "dns", Severity: "warning"},
			},
		}
		server.mu.Unlock()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/status", nil)
		w := httptest.NewRecorder()

		server.handleClusterStatus(w, req)

		var response APIResponse
		json.NewDecoder(w.Body).Decode(&response)

		data := response.Data.(map[string]interface{})
		if int(data["totalNodes"].(float64)) != 2 {
			t.Errorf("expected 2 nodes, got %v", data["totalNodes"])
		}
		if int(data["healthyNodes"].(float64)) != 1 {
			t.Errorf("expected 1 healthy node, got %v", data["healthyNodes"])
		}
		if int(data["degradedNodes"].(float64)) != 1 {
			t.Errorf("expected 1 degraded node, got %v", data["degradedNodes"])
		}
	})
}

func TestServer_Leases(t *testing.T) {
	config := DefaultControllerConfig()
	config.Coordination.MaxConcurrentRemediations = 2
	server, _ := NewServer(config)

	t.Run("grants lease", func(t *testing.T) {
		leaseReq := LeaseRequest{
			NodeName:        "node-1",
			RemediationType: "restart-kubelet",
		}
		body, _ := json.Marshal(leaseReq)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/leases", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.handleLeases(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}

		var response APIResponse
		json.NewDecoder(w.Body).Decode(&response)

		data := response.Data.(map[string]interface{})
		if data["approved"] != true {
			t.Error("expected lease to be approved")
		}
		if data["leaseId"] == nil {
			t.Error("expected leaseId in response")
		}
	})

	t.Run("denies second lease for same node", func(t *testing.T) {
		leaseReq := LeaseRequest{
			NodeName:        "node-1",
			RemediationType: "flush-dns",
		}
		body, _ := json.Marshal(leaseReq)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/leases", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.handleLeases(w, req)

		if w.Code != http.StatusConflict {
			t.Errorf("expected status 409, got %d", w.Code)
		}
	})

	t.Run("grants lease to different node", func(t *testing.T) {
		leaseReq := LeaseRequest{
			NodeName:        "node-2",
			RemediationType: "restart-kubelet",
		}
		body, _ := json.Marshal(leaseReq)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/leases", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.handleLeases(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})

	t.Run("denies when max concurrent reached", func(t *testing.T) {
		leaseReq := LeaseRequest{
			NodeName:        "node-3",
			RemediationType: "restart-kubelet",
		}
		body, _ := json.Marshal(leaseReq)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/leases", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.handleLeases(w, req)

		if w.Code != http.StatusTooManyRequests {
			t.Errorf("expected status 429, got %d", w.Code)
		}
	})

	t.Run("lists active leases", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/leases", nil)
		w := httptest.NewRecorder()

		server.handleLeases(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}

		var response APIResponse
		json.NewDecoder(w.Body).Decode(&response)

		leases := response.Data.([]interface{})
		if len(leases) != 2 {
			t.Errorf("expected 2 active leases, got %d", len(leases))
		}
	})
}

func TestServer_StartStop(t *testing.T) {
	config := DefaultControllerConfig()
	config.Server.Port = 0 // Use random port
	config.Server.BindAddress = "127.0.0.1"

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	ctx := context.Background()

	// Start server
	if err := server.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if !server.IsReady() {
		t.Error("expected server to be ready after Start()")
	}

	// Stop server
	if err := server.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if server.IsReady() {
		t.Error("expected server to not be ready after Stop()")
	}
}

func TestServer_NodeEndpoints(t *testing.T) {
	server, _ := NewServer(nil)

	// Add test data
	server.mu.Lock()
	server.nodeReports["test-node"] = &NodeReport{
		NodeName:      "test-node",
		NodeUID:       "uid-123",
		OverallHealth: HealthStatusHealthy,
		Timestamp:     time.Now(),
		ActiveProblems: []ProblemSummary{
			{Type: "dns", Severity: "warning", Message: "DNS slow"},
		},
	}
	server.mu.Unlock()

	t.Run("list nodes", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil)
		w := httptest.NewRecorder()

		server.handleNodes(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})

	t.Run("get node detail", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/test-node", nil)
		w := httptest.NewRecorder()

		server.handleNodeDetail(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}

		var response APIResponse
		json.NewDecoder(w.Body).Decode(&response)

		data := response.Data.(map[string]interface{})
		if data["nodeName"] != "test-node" {
			t.Errorf("expected nodeName 'test-node', got %v", data["nodeName"])
		}
	})

	t.Run("get node not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/nonexistent", nil)
		w := httptest.NewRecorder()

		server.handleNodeDetail(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("expected status 404, got %d", w.Code)
		}
	})

	t.Run("get node history", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/test-node/history", nil)
		w := httptest.NewRecorder()

		server.handleNodeDetail(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})
}

func TestDefaultControllerConfig(t *testing.T) {
	config := DefaultControllerConfig()

	if config.Server.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", config.Server.Port)
	}

	if config.Storage.Retention != 30*24*time.Hour {
		t.Errorf("expected 30 day retention, got %v", config.Storage.Retention)
	}

	if config.Coordination.MaxConcurrentRemediations != 3 {
		t.Errorf("expected max 3 concurrent remediations, got %d",
			config.Coordination.MaxConcurrentRemediations)
	}

	if config.Correlation.ClusterWideThreshold != 0.3 {
		t.Errorf("expected 30%% threshold, got %v", config.Correlation.ClusterWideThreshold)
	}
}

func TestServer_LeaseExpiration_AutoCleanup(t *testing.T) {
	config := DefaultControllerConfig()
	server, _ := NewServer(config)

	// Add an expired lease directly to the in-memory map
	expiredTime := time.Now().Add(-1 * time.Hour)
	server.mu.Lock()
	server.leases["expired-lease-1"] = &Lease{
		ID:              "expired-lease-1",
		NodeName:        "test-node",
		RemediationType: "restart-kubelet",
		GrantedAt:       expiredTime.Add(-5 * time.Minute),
		ExpiresAt:       expiredTime,
		Status:          "active",
	}
	server.leases["active-lease-1"] = &Lease{
		ID:              "active-lease-1",
		NodeName:        "other-node",
		RemediationType: "flush-dns",
		GrantedAt:       time.Now(),
		ExpiresAt:       time.Now().Add(5 * time.Minute),
		Status:          "active",
	}
	server.mu.Unlock()

	// Manually trigger cleanup
	server.cleanupExpiredLeases(context.Background())

	// Verify expired lease was marked as expired
	server.mu.RLock()
	expiredLease := server.leases["expired-lease-1"]
	activeLease := server.leases["active-lease-1"]
	server.mu.RUnlock()

	if expiredLease.Status != "expired" {
		t.Errorf("expected expired lease status to be 'expired', got %q", expiredLease.Status)
	}
	if expiredLease.CompletedAt.IsZero() {
		t.Error("expected CompletedAt to be set on expired lease")
	}
	if activeLease.Status != "active" {
		t.Errorf("expected active lease status to remain 'active', got %q", activeLease.Status)
	}
}

func TestServer_CooldownPeriod_Enforced(t *testing.T) {
	// Create a test storage for cooldown checking
	storageConfig := &StorageConfig{
		Path:      ":memory:",
		Retention: 24 * time.Hour,
	}
	storage, err := NewSQLiteStorage(storageConfig)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	if err := storage.Initialize(context.Background()); err != nil {
		t.Fatalf("Failed to initialize storage: %v", err)
	}
	defer storage.Close()

	// Create server with 5-minute cooldown
	config := DefaultControllerConfig()
	config.Coordination.CooldownPeriod = 5 * time.Minute
	server, _ := NewServer(config)
	server.storage = storage

	// Create a recently completed lease (2 minutes ago - still in cooldown)
	recentLease := &Lease{
		ID:              "recent-lease",
		NodeName:        "cooldown-test-node",
		RemediationType: "restart-kubelet",
		GrantedAt:       time.Now().Add(-7 * time.Minute),
		ExpiresAt:       time.Now().Add(-2 * time.Minute),
		Status:          "active",
	}
	storage.SaveLease(context.Background(), recentLease)
	storage.UpdateLeaseStatus(context.Background(), "recent-lease", "completed")

	// Request a new lease - should be denied due to cooldown
	leaseReq := LeaseRequest{
		NodeName:        "cooldown-test-node",
		RemediationType: "flush-dns",
	}
	body, _ := json.Marshal(leaseReq)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/leases", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleLeases(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected status 429 (cooldown active), got %d", w.Code)
	}

	var response APIResponse
	json.NewDecoder(w.Body).Decode(&response)
	data := response.Data.(map[string]interface{})
	if data["approved"] != false {
		t.Error("expected lease to be denied due to cooldown")
	}
	if data["message"] == nil || !contains(data["message"].(string), "Cooldown") {
		t.Errorf("expected cooldown message, got %v", data["message"])
	}
}

func TestServer_CooldownPeriod_Expired(t *testing.T) {
	// Create a test storage for cooldown checking
	storageConfig := &StorageConfig{
		Path:      ":memory:",
		Retention: 24 * time.Hour,
	}
	storage, err := NewSQLiteStorage(storageConfig)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	if err := storage.Initialize(context.Background()); err != nil {
		t.Fatalf("Failed to initialize storage: %v", err)
	}
	defer storage.Close()

	// Create server with 5-minute cooldown
	config := DefaultControllerConfig()
	config.Coordination.CooldownPeriod = 5 * time.Minute
	server, _ := NewServer(config)
	server.storage = storage

	// Insert a completed lease with an old completed_at directly in the database
	// This is necessary because UpdateLeaseStatus sets completed_at to time.Now()
	oldCompletedAt := time.Now().Add(-10 * time.Minute)
	_, err = storage.db.ExecContext(context.Background(), `
		INSERT INTO leases (id, node_name, remediation_type, status, granted_at, expires_at, completed_at)
		VALUES (?, ?, ?, 'completed', ?, ?, ?)`,
		"old-lease", "cooldown-expired-node", "restart-kubelet",
		time.Now().Add(-15*time.Minute), time.Now().Add(-12*time.Minute), oldCompletedAt)
	if err != nil {
		t.Fatalf("Failed to insert old lease: %v", err)
	}

	// Request a new lease - should be granted (cooldown expired)
	leaseReq := LeaseRequest{
		NodeName:        "cooldown-expired-node",
		RemediationType: "flush-dns",
	}
	body, _ := json.Marshal(leaseReq)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/leases", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleLeases(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 (cooldown expired), got %d", w.Code)
	}

	var response APIResponse
	json.NewDecoder(w.Body).Decode(&response)
	data := response.Data.(map[string]interface{})
	if data["approved"] != true {
		t.Error("expected lease to be approved after cooldown expired")
	}
}

func TestServer_CooldownPeriod_Disabled(t *testing.T) {
	// Create a test storage
	storageConfig := &StorageConfig{
		Path:      ":memory:",
		Retention: 24 * time.Hour,
	}
	storage, err := NewSQLiteStorage(storageConfig)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	if err := storage.Initialize(context.Background()); err != nil {
		t.Fatalf("Failed to initialize storage: %v", err)
	}
	defer storage.Close()

	// Create server with cooldown disabled (0)
	config := DefaultControllerConfig()
	config.Coordination.CooldownPeriod = 0
	server, _ := NewServer(config)
	server.storage = storage

	// Create a recently completed lease (1 second ago)
	recentLease := &Lease{
		ID:              "recent-lease",
		NodeName:        "cooldown-disabled-node",
		RemediationType: "restart-kubelet",
		GrantedAt:       time.Now().Add(-5 * time.Second),
		ExpiresAt:       time.Now().Add(-1 * time.Second),
		Status:          "active",
	}
	storage.SaveLease(context.Background(), recentLease)
	storage.UpdateLeaseStatus(context.Background(), "recent-lease", "completed")

	// Request a new lease - should be granted (cooldown disabled)
	leaseReq := LeaseRequest{
		NodeName:        "cooldown-disabled-node",
		RemediationType: "flush-dns",
	}
	body, _ := json.Marshal(leaseReq)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/leases", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleLeases(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 (cooldown disabled), got %d", w.Code)
	}

	var response APIResponse
	json.NewDecoder(w.Body).Decode(&response)
	data := response.Data.(map[string]interface{})
	if data["approved"] != true {
		t.Error("expected lease to be approved when cooldown is disabled")
	}
}

func TestServer_CooldownPeriod_NoStorage(t *testing.T) {
	// Create server with cooldown but no storage (should skip cooldown check)
	config := DefaultControllerConfig()
	config.Coordination.CooldownPeriod = 5 * time.Minute
	server, _ := NewServer(config)
	// storage is nil by default

	// Request a new lease - should be granted (no storage to check)
	leaseReq := LeaseRequest{
		NodeName:        "no-storage-node",
		RemediationType: "flush-dns",
	}
	body, _ := json.Marshal(leaseReq)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/leases", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleLeases(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 (no storage), got %d", w.Code)
	}

	var response APIResponse
	json.NewDecoder(w.Body).Decode(&response)
	data := response.Data.(map[string]interface{})
	if data["approved"] != true {
		t.Error("expected lease to be approved when storage is nil")
	}
}

// contains is a helper to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestServer_WithInitializedStorage validates that the server correctly uses
// storage that has been explicitly initialized via Initialize().
func TestServer_WithInitializedStorage(t *testing.T) {
	config := &StorageConfig{
		Path:      ":memory:",
		Retention: 24 * time.Hour,
	}

	storage, err := NewSQLiteStorage(config)
	if err != nil {
		t.Fatalf("NewSQLiteStorage() error = %v", err)
	}
	ctx := context.Background()
	if err := storage.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	defer storage.Close()

	server, err := NewServer(nil)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	server.SetStorage(storage)

	t.Run("node report persists to storage after initialization", func(t *testing.T) {
		report := NodeReport{
			NodeName:      "storage-test-node",
			Timestamp:     time.Now(),
			OverallHealth: HealthStatusHealthy,
		}
		body, _ := json.Marshal(report)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/reports", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.handleReports(w, req)

		if w.Code != http.StatusAccepted {
			t.Errorf("expected 202, got %d", w.Code)
		}

		// Verify the report was persisted — retrievable from storage directly.
		saved, err := storage.GetLatestNodeReport(ctx, "storage-test-node")
		if err != nil {
			t.Fatalf("GetLatestNodeReport() error = %v", err)
		}
		if saved == nil {
			t.Fatal("expected report in storage, got nil")
		}
		if saved.NodeName != "storage-test-node" {
			t.Errorf("expected NodeName 'storage-test-node', got %q", saved.NodeName)
		}
	})
}

func TestServer_StartBindFailure(t *testing.T) {
	// Occupy a port so the controller server cannot bind to it.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to grab a free port: %v", err)
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port

	config := DefaultControllerConfig()
	config.Server.BindAddress = "127.0.0.1"
	config.Server.Port = port

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	ctx := context.Background()
	if err := server.Start(ctx); err == nil {
		server.Stop(ctx)
		t.Fatal("Start() should fail when port is already in use")
	}

	// Server must not be marked as started after a bind failure.
	if server.IsReady() {
		t.Error("server should not be ready after a bind failure")
	}
}

// =====================
// Rehydration Tests
// =====================

// newServerTestStorage creates an in-memory SQLite storage for testing.
func newServerTestStorage(t *testing.T) *SQLiteStorage {
	t.Helper()
	storage, err := NewSQLiteStorage(&StorageConfig{Path: ":memory:", Retention: 24 * time.Hour})
	if err != nil {
		t.Fatalf("NewSQLiteStorage: %v", err)
	}
	if err := storage.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	t.Cleanup(func() { storage.Close() })
	return storage
}

func TestServer_RehydratesStateOnStart(t *testing.T) {
	storage := newServerTestStorage(t)
	ctx := context.Background()

	// Seed a node report into storage before the server starts.
	seedReport := &NodeReport{
		NodeName:      "rehydrated-node",
		NodeUID:       "uid-rehydrate",
		Timestamp:     time.Now().Add(-5 * time.Minute),
		OverallHealth: HealthStatusDegraded,
		ActiveProblems: []ProblemSummary{
			{Type: "disk", Severity: "warning", Message: "disk nearly full"},
		},
	}
	if err := storage.SaveNodeReport(ctx, seedReport); err != nil {
		t.Fatalf("SaveNodeReport: %v", err)
	}

	// Seed an active lease into storage.
	seedLease := &Lease{
		ID:              "rehydrate-lease-1",
		NodeName:        "rehydrated-node",
		RemediationType: "restart-kubelet",
		GrantedAt:       time.Now().Add(-2 * time.Minute),
		ExpiresAt:       time.Now().Add(3 * time.Minute),
		Status:          "active",
	}
	if err := storage.SaveLease(ctx, seedLease); err != nil {
		t.Fatalf("SaveLease: %v", err)
	}

	// Create a server, attach storage, and start it.
	config := DefaultControllerConfig()
	config.Server.BindAddress = "127.0.0.1"
	config.Server.Port = 0 // random port
	config.Correlation.Enabled = false
	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	server.SetStorage(storage)

	if err := server.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer server.Stop(ctx)

	// Verify node report was rehydrated into the in-memory map.
	server.mu.RLock()
	report, exists := server.nodeReports["rehydrated-node"]
	server.mu.RUnlock()

	if !exists {
		t.Fatal("expected 'rehydrated-node' to be in nodeReports after Start")
	}
	if report.OverallHealth != HealthStatusDegraded {
		t.Errorf("expected health %q, got %q", HealthStatusDegraded, report.OverallHealth)
	}
	if len(report.ActiveProblems) != 1 {
		t.Errorf("expected 1 active problem, got %d", len(report.ActiveProblems))
	}

	// Verify lease was rehydrated into the in-memory map.
	server.mu.RLock()
	lease, leaseExists := server.leases["rehydrate-lease-1"]
	server.mu.RUnlock()

	if !leaseExists {
		t.Fatal("expected 'rehydrate-lease-1' to be in leases after Start")
	}
	if lease.NodeName != "rehydrated-node" {
		t.Errorf("expected lease NodeName 'rehydrated-node', got %q", lease.NodeName)
	}
	if lease.Status != "active" {
		t.Errorf("expected lease Status 'active', got %q", lease.Status)
	}
}

func TestServer_ColdStart_EmptyStorage(t *testing.T) {
	storage := newServerTestStorage(t)
	ctx := context.Background()

	config := DefaultControllerConfig()
	config.Server.BindAddress = "127.0.0.1"
	config.Server.Port = 0
	config.Correlation.Enabled = false
	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	server.SetStorage(storage)

	if err := server.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer server.Stop(ctx)

	server.mu.RLock()
	reportCount := len(server.nodeReports)
	leaseCount := len(server.leases)
	server.mu.RUnlock()

	if reportCount != 0 {
		t.Errorf("expected 0 node reports on cold start, got %d", reportCount)
	}
	if leaseCount != 0 {
		t.Errorf("expected 0 leases on cold start, got %d", leaseCount)
	}
}

func TestServer_RehydratedStateVisibleViaAPI(t *testing.T) {
	storage := newServerTestStorage(t)
	ctx := context.Background()

	// Seed two nodes with different health states.
	for _, node := range []struct {
		name   string
		health HealthStatus
	}{
		{"node-alpha", HealthStatusHealthy},
		{"node-beta", HealthStatusCritical},
	} {
		if err := storage.SaveNodeReport(ctx, &NodeReport{
			NodeName:      node.name,
			Timestamp:     time.Now(),
			OverallHealth: node.health,
		}); err != nil {
			t.Fatalf("SaveNodeReport %s: %v", node.name, err)
		}
	}

	config := DefaultControllerConfig()
	config.Server.BindAddress = "127.0.0.1"
	config.Server.Port = 0
	config.Correlation.Enabled = false
	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	server.SetStorage(storage)
	if err := server.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer server.Stop(ctx)

	// Cluster status should reflect the two seeded nodes.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/status", nil)
	w := httptest.NewRecorder()
	server.handleClusterStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp APIResponse
	json.NewDecoder(w.Body).Decode(&resp)
	data := resp.Data.(map[string]interface{})

	if int(data["totalNodes"].(float64)) != 2 {
		t.Errorf("expected 2 total nodes, got %v", data["totalNodes"])
	}
	if int(data["criticalNodes"].(float64)) != 1 {
		t.Errorf("expected 1 critical node, got %v", data["criticalNodes"])
	}
	if data["overallHealth"] != string(HealthStatusCritical) {
		t.Errorf("expected overall health %q, got %v", HealthStatusCritical, data["overallHealth"])
	}
}

// rehydrateStubStorage wraps a real SQLiteStorage and lets tests inject errors
// for specific rehydration calls without implementing the full Storage interface.
type rehydrateStubStorage struct {
	*SQLiteStorage
	reportErr error // if non-nil, GetAllLatestReports returns this error
	leaseErr  error // if non-nil, GetActiveLeases returns this error
}

func (s *rehydrateStubStorage) GetAllLatestReports(ctx context.Context) (map[string]*NodeReport, error) {
	if s.reportErr != nil {
		return nil, s.reportErr
	}
	return s.SQLiteStorage.GetAllLatestReports(ctx)
}

func (s *rehydrateStubStorage) GetActiveLeases(ctx context.Context) ([]*Lease, error) {
	if s.leaseErr != nil {
		return nil, s.leaseErr
	}
	return s.SQLiteStorage.GetActiveLeases(ctx)
}

// TestServer_RehydratePartialFailure_ReportsErr verifies that a GetAllLatestReports
// error is non-fatal: the server still starts and active leases are still loaded.
func TestServer_RehydratePartialFailure_ReportsErr(t *testing.T) {
	real := newServerTestStorage(t)
	ctx := context.Background()

	// Seed an active lease so we can verify it survives the reports failure.
	seedLease := &Lease{
		ID:              "partial-fail-lease-1",
		NodeName:        "some-node",
		RemediationType: "restart-kubelet",
		GrantedAt:       time.Now().Add(-1 * time.Minute),
		ExpiresAt:       time.Now().Add(5 * time.Minute),
		Status:          "active",
	}
	if err := real.SaveLease(ctx, seedLease); err != nil {
		t.Fatalf("SaveLease: %v", err)
	}

	stub := &rehydrateStubStorage{SQLiteStorage: real, reportErr: errors.New("disk read error")}
	config := DefaultControllerConfig()
	config.Server.BindAddress = "127.0.0.1"
	config.Server.Port = 0
	config.Correlation.Enabled = false
	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	server.SetStorage(stub)

	if err := server.Start(ctx); err != nil {
		t.Fatalf("Start should succeed despite GetAllLatestReports error: %v", err)
	}
	defer server.Stop(ctx)

	server.mu.RLock()
	reportCount := len(server.nodeReports)
	_, leaseExists := server.leases["partial-fail-lease-1"]
	server.mu.RUnlock()

	if reportCount != 0 {
		t.Errorf("expected 0 node reports (reports errored), got %d", reportCount)
	}
	if !leaseExists {
		t.Error("expected lease to be rehydrated even though GetAllLatestReports failed")
	}
}

// TestServer_RehydratePartialFailure_LeasesErr verifies that a GetActiveLeases
// error is non-fatal: the server still starts and node reports are still loaded.
func TestServer_RehydratePartialFailure_LeasesErr(t *testing.T) {
	real := newServerTestStorage(t)
	ctx := context.Background()

	// Seed a node report so we can verify it survives the leases failure.
	if err := real.SaveNodeReport(ctx, &NodeReport{
		NodeName:      "partial-fail-node",
		Timestamp:     time.Now(),
		OverallHealth: HealthStatusHealthy,
	}); err != nil {
		t.Fatalf("SaveNodeReport: %v", err)
	}

	stub := &rehydrateStubStorage{SQLiteStorage: real, leaseErr: errors.New("lease table locked")}
	config := DefaultControllerConfig()
	config.Server.BindAddress = "127.0.0.1"
	config.Server.Port = 0
	config.Correlation.Enabled = false
	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	server.SetStorage(stub)

	if err := server.Start(ctx); err != nil {
		t.Fatalf("Start should succeed despite GetActiveLeases error: %v", err)
	}
	defer server.Stop(ctx)

	server.mu.RLock()
	_, reportExists := server.nodeReports["partial-fail-node"]
	leaseCount := len(server.leases)
	server.mu.RUnlock()

	if !reportExists {
		t.Error("expected node report to be rehydrated even though GetActiveLeases failed")
	}
	if leaseCount != 0 {
		t.Errorf("expected 0 leases (leases errored), got %d", leaseCount)
	}
}

// TestServer_NilStorage_RehydrationSkipped verifies that a server started without
// any storage attached starts cleanly with empty in-memory state (no panic).
func TestServer_NilStorage_RehydrationSkipped(t *testing.T) {
	ctx := context.Background()
	config := DefaultControllerConfig()
	config.Server.BindAddress = "127.0.0.1"
	config.Server.Port = 0
	config.Correlation.Enabled = false
	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	// Intentionally do NOT call SetStorage — storage remains nil.

	if err := server.Start(ctx); err != nil {
		t.Fatalf("Start with nil storage should not error: %v", err)
	}
	defer server.Stop(ctx)

	server.mu.RLock()
	reportCount := len(server.nodeReports)
	leaseCount := len(server.leases)
	server.mu.RUnlock()

	if reportCount != 0 {
		t.Errorf("expected 0 node reports with nil storage, got %d", reportCount)
	}
	if leaseCount != 0 {
		t.Errorf("expected 0 leases with nil storage, got %d", leaseCount)
	}
}

// TestServer_ExpiredLeaseNotRehydrated verifies that leases whose ExpiresAt is in the
// past are NOT loaded into the in-memory lease map on restart. Only active, non-expired
// leases should be rehydrated.
func TestServer_ExpiredLeaseNotRehydrated(t *testing.T) {
	storage := newServerTestStorage(t)
	ctx := context.Background()

	// Seed an expired lease (ExpiresAt in the past, status still "active" in DB).
	expiredLease := &Lease{
		ID:              "expired-lease-1",
		NodeName:        "stale-node",
		RemediationType: "restart-kubelet",
		GrantedAt:       time.Now().Add(-10 * time.Minute),
		ExpiresAt:       time.Now().Add(-1 * time.Minute), // already expired
		Status:          "active",
	}
	if err := storage.SaveLease(ctx, expiredLease); err != nil {
		t.Fatalf("SaveLease (expired): %v", err)
	}

	// Seed a still-active lease.
	activeLease := &Lease{
		ID:              "active-lease-1",
		NodeName:        "live-node",
		RemediationType: "drain-node",
		GrantedAt:       time.Now().Add(-1 * time.Minute),
		ExpiresAt:       time.Now().Add(9 * time.Minute),
		Status:          "active",
	}
	if err := storage.SaveLease(ctx, activeLease); err != nil {
		t.Fatalf("SaveLease (active): %v", err)
	}

	config := DefaultControllerConfig()
	config.Server.BindAddress = "127.0.0.1"
	config.Server.Port = 0
	config.Correlation.Enabled = false
	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	server.SetStorage(storage)

	if err := server.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer server.Stop(ctx)

	server.mu.RLock()
	_, expiredExists := server.leases["expired-lease-1"]
	_, activeExists := server.leases["active-lease-1"]
	leaseCount := len(server.leases)
	server.mu.RUnlock()

	if expiredExists {
		t.Error("expired lease should NOT be rehydrated into in-memory state")
	}
	if !activeExists {
		t.Error("active lease should be rehydrated into in-memory state")
	}
	if leaseCount != 1 {
		t.Errorf("expected exactly 1 rehydrated lease, got %d", leaseCount)
	}
}

// TestServer_Stop_NoDeadlockWithCleanup is a regression test for the deadlock where
// Stop() held s.mu.Lock() while calling leaseCleanupWg.Wait(), and the cleanup goroutine
// was blocked trying to acquire s.mu.Lock() inside cleanupExpiredLeases — circular wait.
func TestServer_Stop_NoDeadlockWithCleanup(t *testing.T) {
	config := DefaultControllerConfig()
	config.Server.Port = 0
	config.Server.BindAddress = "127.0.0.1"

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	ctx := context.Background()
	if err := server.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Race Stop() against a direct call to cleanupExpiredLeases to reproduce the
	// deadlock scenario: Stop holds the write lock while waiting for the goroutine
	// that is itself blocked on acquiring that same lock.
	cleanupDone := make(chan struct{})
	go func() {
		server.cleanupExpiredLeases(ctx)
		close(cleanupDone)
	}()

	stopDone := make(chan error, 1)
	go func() {
		stopDone <- server.Stop(ctx)
	}()

	timeout := time.After(5 * time.Second)
	select {
	case err := <-stopDone:
		if err != nil {
			t.Fatalf("Stop() error = %v", err)
		}
		select {
		case <-cleanupDone:
		case <-time.After(3 * time.Second):
			t.Fatal("cleanupExpiredLeases did not return after Stop() completed")
		}
	case <-timeout:
		t.Fatal("Stop() deadlocked with concurrent cleanupExpiredLeases — lock not released before Wait()")
	}
}

// TestServer_Stop_NoDeadlock_ShutdownWithInFlightHandler is a regression test for the
// deadlock where Stop() held s.mu.Lock() while calling httpServer.Shutdown(). Any
// in-flight HTTP handler that also acquires s.mu.Lock() would block waiting for the
// lock, while Shutdown() would block waiting for the handler — circular wait.
//
// The fix (Task #14454): Stop() releases s.mu before calling Shutdown(), so
// in-flight handlers can complete and Shutdown() can return.
//
// Test design: a custom handler is registered that (a) signals it is running,
// (b) waits for a gate before calling s.mu.Lock(), and (c) writes a response.
// After confirming the HTTP connection is in-flight, Stop() is called.
// After 100ms — enough time for Stop() to acquire and release its own lock and
// enter Shutdown() — the handler is allowed to acquire s.mu.Lock().
//
// With the FIXED code: Stop() has already released s.mu; the handler acquires it
// and completes; Shutdown() returns; both goroutines finish within the timeout.
// With the OLD BUGGY code: Stop() holds s.mu while Shutdown() is waiting for the
// handler; the handler is blocked on s.mu.Lock(); deadlock → timeout fires.
func TestServer_Stop_NoDeadlock_ShutdownWithInFlightHandler(t *testing.T) {
	ctx := context.Background()
	config := DefaultControllerConfig()
	config.Server.BindAddress = "127.0.0.1"
	config.Server.Port = 0
	config.Correlation.Enabled = false

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	// handlerStarted is signalled when the test handler is executing (HTTP connection
	// is active and will be tracked by httpServer.Shutdown()).
	handlerStarted := make(chan struct{}, 1)
	// handlerCanLock is closed to allow the handler to proceed to acquire s.mu.
	handlerCanLock := make(chan struct{})

	// Register a test-only route that simulates a handler needing s.mu.Lock().
	// This must be done before Start() so the route is active when the server starts.
	server.mux.HandleFunc("/test/deadlock-probe", func(w http.ResponseWriter, r *http.Request) {
		// Signal that the HTTP connection is now in-flight.
		select {
		case handlerStarted <- struct{}{}:
		default:
		}

		// Wait until the test allows us to proceed (gives Stop() time to start).
		select {
		case <-handlerCanLock:
		case <-r.Context().Done():
			return
		}

		// Acquire the server's write lock — this is what deadlocked with the old code.
		server.mu.Lock()
		defer server.mu.Unlock()

		w.WriteHeader(http.StatusOK)
	})

	if err := server.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	serverAddr := server.httpServer.Addr

	// Issue a real HTTP request so the connection is tracked by httpServer.Shutdown().
	httpDone := make(chan struct{})
	go func() {
		defer close(httpDone)
		client := &http.Client{Timeout: 15 * time.Second}
		resp, err := client.Get("http://" + serverAddr + "/test/deadlock-probe")
		if err == nil {
			resp.Body.Close()
		}
		// Ignore errors — Shutdown may close the connection before the response is sent.
	}()

	// Wait for the handler to begin executing (connection is in-flight).
	select {
	case <-handlerStarted:
	case <-time.After(5 * time.Second):
		close(handlerCanLock)
		t.Fatal("HTTP handler did not start within timeout")
	}

	// Call Stop() concurrently. It will acquire s.mu, mark the server as stopped,
	// release s.mu, and then enter httpServer.Shutdown().
	stopDone := make(chan error, 1)
	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go func() {
		stopDone <- server.Stop(stopCtx)
	}()

	// Wait long enough for Stop() to have: acquired s.mu, released it, and called
	// Shutdown(). Shutdown() will now be waiting for the in-flight connection.
	// (Stop() holds s.mu only for a few microseconds of bookkeeping.)
	time.Sleep(100 * time.Millisecond)

	// Allow the handler to proceed to s.mu.Lock(). With the FIXED code, Stop()
	// has already released s.mu, so the handler acquires it immediately and
	// completes — allowing Shutdown() to return. With the OLD BUGGY code, Stop()
	// would still hold s.mu inside Shutdown(), so the handler would deadlock.
	close(handlerCanLock)

	deadline := time.After(5 * time.Second)
	for remaining := 2; remaining > 0; {
		select {
		case err := <-stopDone:
			if err != nil {
				t.Errorf("Stop(): %v", err)
			}
			remaining--
		case <-httpDone:
			remaining--
		case <-deadline:
			t.Fatal("deadlock: Stop() or in-flight HTTP handler did not complete — " +
				"httpServer.Shutdown() may be holding s.mu while waiting for a handler that needs s.mu")
		}
	}
}

// TestServer_CorrelationsPersistedAndRecoverable verifies the persist/load cycle:
// a correlation detected before server shutdown is recovered after a restart.
func TestServer_CorrelationsPersistedAndRecoverable(t *testing.T) {
	storage := newServerTestStorage(t)
	ctx := context.Background()

	// Phase 1: start a server, inject a correlation directly into storage.
	corr := &Correlation{
		ID:            "corr-infr-aabb",
		Type:          CorrelationTypeInfrastructure,
		Severity:      "warning",
		Status:        CorrelationStatusActive,
		AffectedNodes: []string{"node-a", "node-b"},
		ProblemTypes:  []string{"DNSFailure"},
		Message:       "DNS failure on 2 nodes",
		Confidence:    0.5,
		DetectedAt:    time.Now().Add(-10 * time.Minute),
		UpdatedAt:     time.Now().Add(-10 * time.Minute),
	}
	if err := storage.SaveCorrelation(ctx, corr); err != nil {
		t.Fatalf("SaveCorrelation: %v", err)
	}

	// Phase 2: start a fresh server backed by the same storage and verify the
	// correlation is loaded on startup.
	config := DefaultControllerConfig()
	config.Server.BindAddress = "127.0.0.1"
	config.Server.Port = 0
	config.Correlation.Enabled = true
	config.Correlation.EvaluationInterval = 24 * time.Hour // disable background eval

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	server.SetStorage(storage)

	if err := server.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer server.Stop(ctx)

	active := server.correlator.GetActiveCorrelations()
	if len(active) != 1 {
		t.Fatalf("expected 1 active correlation after restart, got %d", len(active))
	}
	if active[0].ID != "corr-infr-aabb" {
		t.Errorf("expected correlation ID 'corr-infr-aabb', got %q", active[0].ID)
	}
}

func TestServer_RemediationCoordinatedEvent_MultipleLeases(t *testing.T) {
	// Verify that granting a second lease of the same remediation type triggers
	// the RecordRemediationCoordinated path without panicking.
	// The recorder is disabled so no real k8s event is emitted; this is a
	// smoke test for the wiring.
	recorder, err := NewEventRecorder(&EventRecorderConfig{
		Enabled:   false,
		Namespace: "test",
	})
	if err != nil {
		t.Fatalf("NewEventRecorder: %v", err)
	}

	config := DefaultControllerConfig()
	config.Server.BindAddress = "127.0.0.1"
	config.Server.Port = 0
	config.Coordination.Enabled = true
	config.Coordination.MaxConcurrentRemediations = 5
	config.Coordination.DefaultLeaseDuration = time.Minute

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	// Inject disabled recorder to exercise the wiring code path.
	server.eventRecorder = recorder

	ctx := context.Background()
	if err := server.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer server.Stop(ctx)

	addr := server.httpServer.Addr
	grant := func(node string) {
		body, _ := json.Marshal(map[string]interface{}{
			"node":       node,
			"remediation": "disk-cleanup",
			"reason":     "test",
		})
		resp, err := http.Post("http://"+addr+"/api/v1/leases", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST /api/v1/leases/request (node %s): %v", node, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("lease request for node %s: status %d", node, resp.StatusCode)
		}
	}

	// First lease: coordination path skipped (only 1 active lease).
	grant("node-alpha")
	// Second lease of the same type: coordinatedNodes will have 2 entries;
	// RecordRemediationCoordinated should be called without panic.
	grant("node-beta")
}

// TestServer_Start_BlockedDuringStopping is a regression test for the race window where
// Stop() releases s.mu after marking s.started=false but before leaseCleanupWg.Wait()
// completes. A concurrent Start() would see s.started=false and proceed to restart
// the server while the old one was still draining.
func TestServer_Start_BlockedDuringStopping(t *testing.T) {
	config := DefaultControllerConfig()
	config.Server.Port = 0
	config.Server.BindAddress = "127.0.0.1"

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	ctx := context.Background()
	if err := server.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Manually set stopping=true with the lock held to simulate Stop() having
	// released the lock mid-drain. This exercises the guard in Start() directly
	// without needing a real timing race.
	server.mu.Lock()
	server.started = false
	server.stopping = true
	server.mu.Unlock()

	// Restore state so we can Stop() cleanly afterward (httpServer still running).
	defer func() {
		server.mu.Lock()
		server.started = true
		server.stopping = false
		server.mu.Unlock()
		server.Stop(ctx) //nolint:errcheck
	}()

	err = server.Start(ctx)
	if err == nil {
		t.Fatal("Start() during stop should return an error, got nil")
	}
	if !strings.Contains(err.Error(), "stopping") {
		t.Errorf("Start() error should mention 'stopping', got: %v", err)
	}
}
