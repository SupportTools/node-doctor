package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
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
