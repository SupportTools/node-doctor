package controller

import (
	"bytes"
	"context"
	"encoding/json"
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
