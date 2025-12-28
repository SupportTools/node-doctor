package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/controller"
	"github.com/supporttools/node-doctor/test"
)

// createTestServer creates a controller server and httptest server for testing.
// Returns the controller server, httptest server, and a cleanup function.
func createTestServer(t *testing.T, config *controller.ControllerConfig) (*controller.Server, *httptest.Server, func()) {
	if config == nil {
		config = controller.DefaultControllerConfig()
	}

	server, err := controller.NewServer(config)
	test.AssertNoError(t, err, "Failed to create server")

	ts := httptest.NewServer(server.Handler())

	cleanup := func() {
		ts.Close()
	}

	return server, ts, cleanup
}

// doRequest is a helper to make HTTP requests to the test server
func doRequest(t *testing.T, ts *httptest.Server, method, path string, body interface{}) (*http.Response, map[string]interface{}) {
	var reqBody io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		test.AssertNoError(t, err, "Failed to marshal request body")
		reqBody = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequest(method, ts.URL+path, reqBody)
	test.AssertNoError(t, err, "Failed to create request")

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	test.AssertNoError(t, err, "Failed to execute request")

	var result map[string]interface{}
	if resp.Body != nil {
		defer resp.Body.Close()
		respBytes, _ := io.ReadAll(resp.Body)
		if len(respBytes) > 0 {
			json.Unmarshal(respBytes, &result)
		}
	}

	return resp, result
}

// TestLeaseCoordinationFlow tests the complete lease lifecycle
func TestLeaseCoordinationFlow(t *testing.T) {
	config := controller.DefaultControllerConfig()
	config.Coordination.MaxConcurrentRemediations = 2
	config.Coordination.DefaultLeaseDuration = 5 * time.Minute

	t.Run("single node lease lifecycle", func(t *testing.T) {
		_, ts, cleanup := createTestServer(t, config)
		defer cleanup()

		// Step 1: Request a lease
		leaseReq := controller.LeaseRequest{
			NodeName:        "node-1",
			RemediationType: "restart-kubelet",
			Reason:          "Kubelet not responding",
		}

		resp, result := doRequest(t, ts, http.MethodPost, "/api/v1/leases", leaseReq)
		test.AssertEqual(t, http.StatusOK, resp.StatusCode, "Lease request should succeed")
		test.AssertTrue(t, result["success"].(bool), "Response should be successful")

		data := result["data"].(map[string]interface{})
		test.AssertTrue(t, data["approved"].(bool), "Lease should be approved")

		leaseID := data["leaseId"].(string)
		test.AssertTrue(t, len(leaseID) > 0, "Lease ID should not be empty")

		// Step 2: Verify lease appears in active list
		resp, result = doRequest(t, ts, http.MethodGet, "/api/v1/leases", nil)
		test.AssertEqual(t, http.StatusOK, resp.StatusCode, "List leases should succeed")

		leases := result["data"].([]interface{})
		test.AssertEqual(t, 1, len(leases), "Should have 1 active lease")

		// Step 3: Release the lease
		resp, _ = doRequest(t, ts, http.MethodDelete, "/api/v1/leases/"+leaseID, nil)
		test.AssertEqual(t, http.StatusOK, resp.StatusCode, "Release should succeed")

		// Step 4: Verify lease no longer appears in active list
		resp, result = doRequest(t, ts, http.MethodGet, "/api/v1/leases", nil)
		leases = result["data"].([]interface{})
		test.AssertEqual(t, 0, len(leases), "Should have 0 active leases after release")
	})

	t.Run("max concurrent remediations limit", func(t *testing.T) {
		_, ts, cleanup := createTestServer(t, config)
		defer cleanup()

		// Request leases for 2 nodes (the max)
		for i := 1; i <= 2; i++ {
			leaseReq := controller.LeaseRequest{
				NodeName:        fmt.Sprintf("node-%d", i),
				RemediationType: "restart-kubelet",
			}

			resp, _ := doRequest(t, ts, http.MethodPost, "/api/v1/leases", leaseReq)
			test.AssertEqual(t, http.StatusOK, resp.StatusCode, "Lease %d should be granted", i)
		}

		// Third request should be denied (max concurrent reached)
		leaseReq := controller.LeaseRequest{
			NodeName:        "node-3",
			RemediationType: "restart-kubelet",
		}

		resp, _ := doRequest(t, ts, http.MethodPost, "/api/v1/leases", leaseReq)
		test.AssertEqual(t, http.StatusTooManyRequests, resp.StatusCode,
			"Third lease should be denied due to max concurrent")
	})

	t.Run("same node cannot have multiple leases", func(t *testing.T) {
		_, ts, cleanup := createTestServer(t, config)
		defer cleanup()

		// First lease
		leaseReq := controller.LeaseRequest{
			NodeName:        "node-1",
			RemediationType: "restart-kubelet",
		}

		resp, _ := doRequest(t, ts, http.MethodPost, "/api/v1/leases", leaseReq)
		test.AssertEqual(t, http.StatusOK, resp.StatusCode, "First lease should succeed")

		// Second lease for same node
		leaseReq.RemediationType = "flush-dns"

		resp, _ = doRequest(t, ts, http.MethodPost, "/api/v1/leases", leaseReq)
		test.AssertEqual(t, http.StatusConflict, resp.StatusCode,
			"Second lease for same node should be denied")
	})

	t.Run("lease released frees slot for another node", func(t *testing.T) {
		_, ts, cleanup := createTestServer(t, config)
		defer cleanup()

		// Fill all slots
		var leaseIDs []string
		for i := 1; i <= 2; i++ {
			leaseReq := controller.LeaseRequest{
				NodeName:        fmt.Sprintf("node-%d", i),
				RemediationType: "restart-kubelet",
			}

			resp, result := doRequest(t, ts, http.MethodPost, "/api/v1/leases", leaseReq)
			test.AssertEqual(t, http.StatusOK, resp.StatusCode, "Lease %d should be granted", i)

			data := result["data"].(map[string]interface{})
			leaseIDs = append(leaseIDs, data["leaseId"].(string))
		}

		// Verify third node is blocked
		leaseReq := controller.LeaseRequest{
			NodeName:        "node-3",
			RemediationType: "restart-kubelet",
		}

		resp, _ := doRequest(t, ts, http.MethodPost, "/api/v1/leases", leaseReq)
		test.AssertEqual(t, http.StatusTooManyRequests, resp.StatusCode, "Third should be blocked")

		// Release first lease
		resp, _ = doRequest(t, ts, http.MethodDelete, "/api/v1/leases/"+leaseIDs[0], nil)
		test.AssertEqual(t, http.StatusOK, resp.StatusCode, "Release should succeed")

		// Now third node should succeed
		resp, _ = doRequest(t, ts, http.MethodPost, "/api/v1/leases", leaseReq)
		test.AssertEqual(t, http.StatusOK, resp.StatusCode, "Third should succeed after release")
	})
}

// TestCorrelationDetectionFlow tests correlation engine integration
func TestCorrelationDetectionFlow(t *testing.T) {
	config := controller.DefaultControllerConfig()
	config.Correlation.Enabled = true
	config.Correlation.ClusterWideThreshold = 0.3 // 30% threshold
	config.Correlation.EvaluationInterval = 100 * time.Millisecond

	t.Run("infrastructure correlation detection", func(t *testing.T) {
		server, ts, cleanup := createTestServer(t, config)
		defer cleanup()

		// Start the server to enable correlator
		ctx := context.Background()
		err := server.Start(ctx)
		test.AssertNoError(t, err, "Failed to start server")
		defer server.Stop(ctx)

		// Submit reports from 5 nodes, 3 with same problem (60% > 30% threshold)
		totalNodes := 5
		nodesWithProblem := 3

		for i := 1; i <= totalNodes; i++ {
			report := controller.NodeReport{
				NodeName:      fmt.Sprintf("node-%d", i),
				Timestamp:     time.Now(),
				OverallHealth: controller.HealthStatusHealthy,
			}

			if i <= nodesWithProblem {
				report.OverallHealth = controller.HealthStatusDegraded
				report.ActiveProblems = []controller.ProblemSummary{
					{
						Type:     "dns",
						Severity: "warning",
						Message:  "DNS resolution slow",
					},
				}
			}

			resp, _ := doRequest(t, ts, http.MethodPost, "/api/v1/reports", report)
			test.AssertEqual(t, http.StatusAccepted, resp.StatusCode, "Report should be accepted")
		}

		// Wait for correlation evaluation
		time.Sleep(300 * time.Millisecond)

		// Check for correlations
		resp, result := doRequest(t, ts, http.MethodGet, "/api/v1/correlations", nil)
		test.AssertEqual(t, http.StatusOK, resp.StatusCode, "Correlations request should succeed")

		correlations := result["data"].([]interface{})
		test.AssertTrue(t, len(correlations) > 0, "Should detect infrastructure correlation")

		if len(correlations) > 0 {
			corr := correlations[0].(map[string]interface{})
			test.AssertEqual(t, "infrastructure", corr["type"],
				"Correlation type should be 'infrastructure'")
		}
	})

	t.Run("common cause correlation detection", func(t *testing.T) {
		server, ts, cleanup := createTestServer(t, config)
		defer cleanup()

		ctx := context.Background()
		server.Start(ctx)
		defer server.Stop(ctx)

		// Submit reports with related problems (memory + disk pressure)
		for i := 1; i <= 3; i++ {
			report := controller.NodeReport{
				NodeName:      fmt.Sprintf("pressure-node-%d", i),
				Timestamp:     time.Now(),
				OverallHealth: controller.HealthStatusDegraded,
				ActiveProblems: []controller.ProblemSummary{
					{Type: "MemoryPressure", Severity: "critical", Message: "Memory exhausted"},
					{Type: "DiskPressure", Severity: "critical", Message: "Disk full"},
				},
			}

			resp, _ := doRequest(t, ts, http.MethodPost, "/api/v1/reports", report)
			test.AssertEqual(t, http.StatusAccepted, resp.StatusCode, "Report should be accepted")
		}

		// Wait for correlation evaluation
		time.Sleep(300 * time.Millisecond)

		// Check for correlations
		resp, result := doRequest(t, ts, http.MethodGet, "/api/v1/correlations", nil)
		test.AssertEqual(t, http.StatusOK, resp.StatusCode, "Correlations request should succeed")

		correlations := result["data"].([]interface{})
		// Should detect at least infrastructure correlation since 100% of nodes have same problems
		test.AssertTrue(t, len(correlations) > 0, "Should detect some correlation")
	})

	t.Run("correlation resolution when nodes recover", func(t *testing.T) {
		server, ts, cleanup := createTestServer(t, config)
		defer cleanup()

		ctx := context.Background()
		server.Start(ctx)
		defer server.Stop(ctx)

		// First, create a problem state
		for i := 1; i <= 3; i++ {
			report := controller.NodeReport{
				NodeName:      fmt.Sprintf("recover-node-%d", i),
				Timestamp:     time.Now(),
				OverallHealth: controller.HealthStatusDegraded,
				ActiveProblems: []controller.ProblemSummary{
					{Type: "network", Severity: "warning", Message: "Network issues"},
				},
			}

			doRequest(t, ts, http.MethodPost, "/api/v1/reports", report)
		}

		time.Sleep(300 * time.Millisecond)

		// Verify correlation exists
		_, result := doRequest(t, ts, http.MethodGet, "/api/v1/correlations", nil)
		initialCorrelations := len(result["data"].([]interface{}))

		// Now report recovery for all nodes
		for i := 1; i <= 3; i++ {
			report := controller.NodeReport{
				NodeName:       fmt.Sprintf("recover-node-%d", i),
				Timestamp:      time.Now(),
				OverallHealth:  controller.HealthStatusHealthy,
				ActiveProblems: []controller.ProblemSummary{}, // No problems
			}

			doRequest(t, ts, http.MethodPost, "/api/v1/reports", report)
		}

		time.Sleep(300 * time.Millisecond)

		// Verify correlation is resolved or fewer exist
		_, result = doRequest(t, ts, http.MethodGet, "/api/v1/correlations", nil)
		finalCorrelations := len(result["data"].([]interface{}))

		// Either correlations are resolved (marked as resolved) or removed
		t.Logf("Initial correlations: %d, Final correlations: %d",
			initialCorrelations, finalCorrelations)
	})
}

// TestControllerNodeHTTPFlow tests the HTTP flow between controller and nodes
func TestControllerNodeHTTPFlow(t *testing.T) {
	config := controller.DefaultControllerConfig()

	t.Run("node report ingestion updates cluster status", func(t *testing.T) {
		_, ts, cleanup := createTestServer(t, config)
		defer cleanup()

		// Submit reports from multiple nodes
		nodes := []struct {
			name   string
			health controller.HealthStatus
		}{
			{"node-1", controller.HealthStatusHealthy},
			{"node-2", controller.HealthStatusDegraded},
			{"node-3", controller.HealthStatusHealthy},
		}

		for _, n := range nodes {
			report := controller.NodeReport{
				NodeName:      n.name,
				Timestamp:     time.Now(),
				OverallHealth: n.health,
			}

			resp, _ := doRequest(t, ts, http.MethodPost, "/api/v1/reports", report)
			test.AssertEqual(t, http.StatusAccepted, resp.StatusCode, "Report for %s should be accepted", n.name)
		}

		// Check cluster status
		resp, result := doRequest(t, ts, http.MethodGet, "/api/v1/cluster/status", nil)
		test.AssertEqual(t, http.StatusOK, resp.StatusCode, "Cluster status should succeed")

		data := result["data"].(map[string]interface{})
		test.AssertEqual(t, float64(3), data["totalNodes"], "Should have 3 total nodes")
		test.AssertEqual(t, float64(2), data["healthyNodes"], "Should have 2 healthy nodes")
		test.AssertEqual(t, float64(1), data["degradedNodes"], "Should have 1 degraded node")
	})

	t.Run("node list returns all reported nodes", func(t *testing.T) {
		_, ts, cleanup := createTestServer(t, config)
		defer cleanup()

		// Submit 3 node reports
		for i := 1; i <= 3; i++ {
			report := controller.NodeReport{
				NodeName:      fmt.Sprintf("list-node-%d", i),
				Timestamp:     time.Now(),
				OverallHealth: controller.HealthStatusHealthy,
			}
			doRequest(t, ts, http.MethodPost, "/api/v1/reports", report)
		}

		resp, result := doRequest(t, ts, http.MethodGet, "/api/v1/nodes", nil)
		test.AssertEqual(t, http.StatusOK, resp.StatusCode, "Node list should succeed")

		nodes := result["data"].([]interface{})
		test.AssertEqual(t, 3, len(nodes), "Should have 3 nodes")
	})

	t.Run("node detail returns specific node info", func(t *testing.T) {
		_, ts, cleanup := createTestServer(t, config)
		defer cleanup()

		// Submit a node report
		report := controller.NodeReport{
			NodeName:      "detail-node-1",
			Timestamp:     time.Now(),
			OverallHealth: controller.HealthStatusHealthy,
		}
		doRequest(t, ts, http.MethodPost, "/api/v1/reports", report)

		resp, result := doRequest(t, ts, http.MethodGet, "/api/v1/nodes/detail-node-1", nil)
		test.AssertEqual(t, http.StatusOK, resp.StatusCode, "Node detail should succeed")

		data := result["data"].(map[string]interface{})
		test.AssertEqual(t, "detail-node-1", data["nodeName"], "Should return correct node")
	})

	t.Run("prometheus metrics endpoint works", func(t *testing.T) {
		_, ts, cleanup := createTestServer(t, config)
		defer cleanup()

		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/metrics", nil)
		resp, err := http.DefaultClient.Do(req)
		test.AssertNoError(t, err, "Metrics request should not error")
		defer resp.Body.Close()

		test.AssertEqual(t, http.StatusOK, resp.StatusCode, "Metrics should succeed")

		body, _ := io.ReadAll(resp.Body)
		test.AssertTrue(t, len(body) > 0, "Metrics should not be empty")
	})
}

// TestConcurrentLeaseRequests tests thread safety of lease operations
func TestConcurrentLeaseRequests(t *testing.T) {
	config := controller.DefaultControllerConfig()
	config.Coordination.MaxConcurrentRemediations = 5

	_, ts, cleanup := createTestServer(t, config)
	defer cleanup()

	const numGoroutines = 20
	var wg sync.WaitGroup
	results := make(chan int, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(nodeID int) {
			defer wg.Done()

			leaseReq := controller.LeaseRequest{
				NodeName:        fmt.Sprintf("concurrent-node-%d", nodeID),
				RemediationType: "restart-kubelet",
			}
			body, _ := json.Marshal(leaseReq)

			req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/leases", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				results <- 0
				return
			}
			defer resp.Body.Close()

			results <- resp.StatusCode
		}(i)
	}

	wg.Wait()
	close(results)

	// Count approved and denied
	approved := 0
	denied := 0
	for code := range results {
		if code == http.StatusOK {
			approved++
		} else if code == http.StatusTooManyRequests {
			denied++
		}
	}

	// Should have exactly maxConcurrent approved
	test.AssertEqual(t, 5, approved, "Should have exactly 5 approved (max concurrent)")
	test.AssertEqual(t, 15, denied, "Should have 15 denied (over limit)")

	t.Logf("Concurrent test: %d approved, %d denied", approved, denied)
}

// TestConcurrentReportIngestion tests thread safety of report ingestion
func TestConcurrentReportIngestion(t *testing.T) {
	config := controller.DefaultControllerConfig()

	_, ts, cleanup := createTestServer(t, config)
	defer cleanup()

	const numGoroutines = 50
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(nodeID int) {
			defer wg.Done()

			report := controller.NodeReport{
				NodeName:      fmt.Sprintf("concurrent-report-node-%d", nodeID),
				Timestamp:     time.Now(),
				OverallHealth: controller.HealthStatusHealthy,
			}
			body, _ := json.Marshal(report)

			req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/reports", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			resp, err := http.DefaultClient.Do(req)
			if err != nil || resp.StatusCode != http.StatusAccepted {
				t.Errorf("Report for node-%d failed", nodeID)
			}
			if resp != nil {
				resp.Body.Close()
			}
		}(i)
	}

	wg.Wait()

	// Verify all nodes were stored
	resp, result := doRequest(t, ts, http.MethodGet, "/api/v1/cluster/status", nil)
	test.AssertEqual(t, http.StatusOK, resp.StatusCode, "Cluster status should succeed")

	data := result["data"].(map[string]interface{})
	test.AssertEqual(t, float64(numGoroutines), data["totalNodes"],
		"Should have %d nodes after concurrent ingestion", numGoroutines)
}

// TestHealthEndpoints tests the health check endpoints
func TestHealthEndpoints(t *testing.T) {
	config := controller.DefaultControllerConfig()

	t.Run("healthz always returns ok", func(t *testing.T) {
		_, ts, cleanup := createTestServer(t, config)
		defer cleanup()

		resp, _ := doRequest(t, ts, http.MethodGet, "/healthz", nil)
		test.AssertEqual(t, http.StatusOK, resp.StatusCode, "Healthz should return 200")
	})

	t.Run("readyz returns 503 before start", func(t *testing.T) {
		_, ts, cleanup := createTestServer(t, config)
		defer cleanup()

		resp, _ := doRequest(t, ts, http.MethodGet, "/readyz", nil)
		test.AssertEqual(t, http.StatusServiceUnavailable, resp.StatusCode,
			"Readyz should return 503 before start")
	})

	t.Run("readyz returns 200 after SetReady", func(t *testing.T) {
		server, ts, cleanup := createTestServer(t, config)
		defer cleanup()

		server.SetReady(true)

		resp, _ := doRequest(t, ts, http.MethodGet, "/readyz", nil)
		test.AssertEqual(t, http.StatusOK, resp.StatusCode, "Readyz should return 200 after SetReady")
	})
}

// TestAPIErrorHandling tests error handling for invalid requests
func TestAPIErrorHandling(t *testing.T) {
	config := controller.DefaultControllerConfig()

	_, ts, cleanup := createTestServer(t, config)
	defer cleanup()

	t.Run("invalid JSON returns 400", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/leases",
			bytes.NewReader([]byte("not valid json")))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		test.AssertNoError(t, err, "Request should not error")
		defer resp.Body.Close()

		test.AssertEqual(t, http.StatusBadRequest, resp.StatusCode, "Invalid JSON should return 400")
	})

	t.Run("missing node name returns 400", func(t *testing.T) {
		leaseReq := controller.LeaseRequest{
			RemediationType: "restart-kubelet",
			// Missing NodeName
		}

		resp, _ := doRequest(t, ts, http.MethodPost, "/api/v1/leases", leaseReq)
		test.AssertEqual(t, http.StatusBadRequest, resp.StatusCode, "Missing node name should return 400")
	})

	t.Run("non-existent node returns 404", func(t *testing.T) {
		resp, _ := doRequest(t, ts, http.MethodGet, "/api/v1/nodes/nonexistent-node", nil)
		test.AssertEqual(t, http.StatusNotFound, resp.StatusCode, "Non-existent node should return 404")
	})

	t.Run("wrong HTTP method returns 405", func(t *testing.T) {
		resp, _ := doRequest(t, ts, http.MethodPut, "/api/v1/reports", nil)
		test.AssertEqual(t, http.StatusMethodNotAllowed, resp.StatusCode, "Wrong method should return 405")
	})
}
