package controller

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewControllerMetrics(t *testing.T) {
	metrics := NewControllerMetrics()
	if metrics == nil {
		t.Fatal("NewControllerMetrics() returned nil")
	}

	if metrics.registry == nil {
		t.Fatal("metrics registry is nil")
	}
}

func TestControllerMetrics_Handler(t *testing.T) {
	metrics := NewControllerMetrics()

	// Test that handler returns valid Prometheus output
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()

	metrics.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	body, _ := io.ReadAll(w.Body)
	bodyStr := string(body)

	// Check for expected metrics
	expectedMetrics := []string{
		"node_doctor_cluster_nodes_total",
		"node_doctor_cluster_nodes_healthy",
		"node_doctor_leases_active_total",
		"node_doctor_reports_received_total",
	}

	for _, metric := range expectedMetrics {
		if !strings.Contains(bodyStr, metric) {
			t.Errorf("expected metric %s not found in output", metric)
		}
	}
}

func TestControllerMetrics_UpdateClusterMetrics(t *testing.T) {
	metrics := NewControllerMetrics()

	status := &ClusterStatus{
		TotalNodes:     10,
		HealthyNodes:   7,
		DegradedNodes:  2,
		CriticalNodes:  1,
		UnknownNodes:   0,
		ActiveProblems: 3,
	}

	metrics.UpdateClusterMetrics(status)

	// Verify metrics by reading output
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(w, req)

	body := w.Body.String()

	// Check values are present
	if !strings.Contains(body, "node_doctor_cluster_nodes_total 10") {
		t.Error("expected nodes_total to be 10")
	}
	if !strings.Contains(body, "node_doctor_cluster_nodes_healthy 7") {
		t.Error("expected nodes_healthy to be 7")
	}
	if !strings.Contains(body, "node_doctor_cluster_nodes_degraded 2") {
		t.Error("expected nodes_degraded to be 2")
	}
	if !strings.Contains(body, "node_doctor_cluster_nodes_critical 1") {
		t.Error("expected nodes_critical to be 1")
	}
}

func TestControllerMetrics_UpdateLeaseMetrics(t *testing.T) {
	metrics := NewControllerMetrics()

	metrics.UpdateLeaseMetrics(5)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "node_doctor_leases_active_total 5") {
		t.Error("expected leases_active_total to be 5")
	}
}

func TestControllerMetrics_UpdateProblemMetrics(t *testing.T) {
	metrics := NewControllerMetrics()

	problemCounts := map[string]map[string]int{
		"dns": {
			"warning":  2,
			"critical": 1,
		},
		"disk": {
			"warning": 3,
		},
	}

	metrics.UpdateProblemMetrics(problemCounts)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(w, req)

	body := w.Body.String()

	// Check problem metrics are present
	if !strings.Contains(body, `node_doctor_cluster_problem_nodes{problem_type="dns",severity="warning"} 2`) {
		t.Error("expected dns warning count to be 2")
	}
	if !strings.Contains(body, `node_doctor_cluster_problem_nodes{problem_type="dns",severity="critical"} 1`) {
		t.Error("expected dns critical count to be 1")
	}
	if !strings.Contains(body, `node_doctor_cluster_problem_active{problem_type="dns"} 1`) {
		t.Error("expected dns problem_active to be 1")
	}
}

func TestControllerMetrics_RecordLeaseGranted(t *testing.T) {
	metrics := NewControllerMetrics()

	metrics.RecordLeaseGranted("restart-kubelet")
	metrics.RecordLeaseGranted("restart-kubelet")
	metrics.RecordLeaseGranted("flush-dns")

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, `node_doctor_leases_granted_total{remediation_type="restart-kubelet"} 2`) {
		t.Error("expected restart-kubelet granted count to be 2")
	}
	if !strings.Contains(body, `node_doctor_leases_granted_total{remediation_type="flush-dns"} 1`) {
		t.Error("expected flush-dns granted count to be 1")
	}
}

func TestControllerMetrics_RecordLeaseDenied(t *testing.T) {
	metrics := NewControllerMetrics()

	metrics.RecordLeaseDenied("max_concurrent")
	metrics.RecordLeaseDenied("max_concurrent")
	metrics.RecordLeaseDenied("node_has_lease")

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, `node_doctor_leases_denied_total{reason="max_concurrent"} 2`) {
		t.Error("expected max_concurrent denied count to be 2")
	}
	if !strings.Contains(body, `node_doctor_leases_denied_total{reason="node_has_lease"} 1`) {
		t.Error("expected node_has_lease denied count to be 1")
	}
}

func TestControllerMetrics_RecordReportReceived(t *testing.T) {
	metrics := NewControllerMetrics()

	for i := 0; i < 5; i++ {
		metrics.RecordReportReceived()
	}

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "node_doctor_reports_received_total 5") {
		t.Error("expected reports_received_total to be 5")
	}
}

func TestControllerMetrics_RecordReportError(t *testing.T) {
	metrics := NewControllerMetrics()

	metrics.RecordReportError("invalid_json")
	metrics.RecordReportError("invalid_json")
	metrics.RecordReportError("missing_node_name")

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, `node_doctor_reports_errors_total{error_type="invalid_json"} 2`) {
		t.Error("expected invalid_json error count to be 2")
	}
}

func TestServer_MetricsEndpoint(t *testing.T) {
	server, _ := NewServer(nil)

	// Add some test data
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
	server.leases["lease-1"] = &Lease{
		ID:        "lease-1",
		NodeName:  "node-1",
		Status:    "active",
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	server.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()

	server.handleMetrics(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	body := w.Body.String()

	// Verify cluster metrics
	if !strings.Contains(body, "node_doctor_cluster_nodes_total 2") {
		t.Error("expected 2 total nodes")
	}
	if !strings.Contains(body, "node_doctor_cluster_nodes_healthy 1") {
		t.Error("expected 1 healthy node")
	}
	if !strings.Contains(body, "node_doctor_cluster_nodes_degraded 1") {
		t.Error("expected 1 degraded node")
	}
	if !strings.Contains(body, "node_doctor_leases_active_total 1") {
		t.Error("expected 1 active lease")
	}
}
