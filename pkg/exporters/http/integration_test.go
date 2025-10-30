package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// TestHTTPExporterIntegration tests the complete HTTP exporter workflow
func TestHTTPExporterIntegration(t *testing.T) {
	// Create a test server that tracks requests
	var mu sync.Mutex
	var requests []WebhookRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req WebhookRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("Failed to decode webhook request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		mu.Lock()
		requests = append(requests, req)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(WebhookResponse{
			Success: true,
			Message: "Request processed successfully",
		})
	}))
	defer server.Close()

	// Create HTTP exporter configuration
	config := &types.HTTPExporterConfig{
		Enabled:   true,
		Workers:   3,
		QueueSize: 20,
		Timeout:   10 * time.Second,
		Headers: map[string]string{
			"X-Service": "node-doctor",
		},
		Retry: types.RetryConfig{
			MaxAttempts: 2,
			BaseDelay:   100 * time.Millisecond,
			MaxDelay:    1 * time.Second,
		},
		Webhooks: []types.WebhookEndpoint{
			{
				Name:    "primary-webhook",
				URL:     server.URL + "/webhook",
				Timeout: 5 * time.Second,
				Auth: types.AuthConfig{
					Type: "none",
				},
				Headers: map[string]string{
					"X-Webhook": "primary",
				},
				SendStatus:   true,
				SendProblems: true,
				Retry: &types.RetryConfig{
					MaxAttempts: 3,
					BaseDelay:   50 * time.Millisecond,
					MaxDelay:    500 * time.Millisecond,
				},
			},
		},
	}

	settings := &types.GlobalSettings{
		NodeName: "test-integration-node",
	}

	// Create and start exporter
	exporter, err := NewHTTPExporter(config, settings)
	if err != nil {
		t.Fatalf("Failed to create HTTP exporter: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = exporter.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start HTTP exporter: %v", err)
	}
	defer exporter.Stop()

	// Export multiple status updates
	for i := 0; i < 5; i++ {
		status := &types.Status{
			Source:    fmt.Sprintf("monitor-%d", i),
			Timestamp: time.Now(),
			Events: []types.Event{
				{
					Severity:  types.EventInfo,
					Timestamp: time.Now(),
					Reason:    fmt.Sprintf("StatusUpdate%d", i),
					Message:   fmt.Sprintf("Status update %d", i),
				},
			},
			Conditions: []types.Condition{
				{
					Type:       "TestCondition",
					Status:     types.ConditionTrue,
					Transition: time.Now(),
					Reason:     "Testing",
					Message:    fmt.Sprintf("Test condition %d", i),
				},
			},
		}

		err = exporter.ExportStatus(ctx, status)
		if err != nil {
			t.Errorf("Failed to export status %d: %v", i, err)
		}
	}

	// Export multiple problems
	for i := 0; i < 3; i++ {
		problem := &types.Problem{
			Type:       "TestProblem",
			Resource:   fmt.Sprintf("resource-%d", i),
			Severity:   types.ProblemWarning,
			Message:    fmt.Sprintf("Test problem %d", i),
			DetectedAt: time.Now(),
			Metadata: map[string]string{
				"iteration": fmt.Sprintf("%d", i),
				"test":      "integration",
			},
		}

		err = exporter.ExportProblem(ctx, problem)
		if err != nil {
			t.Errorf("Failed to export problem %d: %v", i, err)
		}
	}

	// Wait for all requests to be processed
	time.Sleep(2 * time.Second)

	// Verify all requests were received
	mu.Lock()
	defer mu.Unlock()

	if len(requests) != 8 { // 5 status + 3 problems
		t.Errorf("Expected 8 webhook requests, got %d", len(requests))
	}

	// Verify request types
	statusCount := 0
	problemCount := 0
	for _, req := range requests {
		switch req.Type {
		case "status":
			statusCount++
			if req.Status == nil {
				t.Error("Status request missing status data")
			}
			if req.Problem != nil {
				t.Error("Status request should not have problem data")
			}
		case "problem":
			problemCount++
			if req.Problem == nil {
				t.Error("Problem request missing problem data")
			}
			if req.Status != nil {
				t.Error("Problem request should not have status data")
			}
		default:
			t.Errorf("Unknown request type: %s", req.Type)
		}

		// Verify common fields
		if req.NodeName != "test-integration-node" {
			t.Errorf("Expected nodeName 'test-integration-node', got %s", req.NodeName)
		}
		if req.Timestamp.IsZero() {
			t.Error("Request timestamp is zero")
		}
		if req.Metadata == nil {
			t.Error("Request metadata is nil")
		}
	}

	if statusCount != 5 {
		t.Errorf("Expected 5 status requests, got %d", statusCount)
	}
	if problemCount != 3 {
		t.Errorf("Expected 3 problem requests, got %d", problemCount)
	}

	// Check exporter statistics
	stats := exporter.GetStats()
	if stats.StatusExportsTotal != 5 {
		t.Errorf("Expected 5 status exports in stats, got %d", stats.StatusExportsTotal)
	}
	if stats.ProblemExportsTotal != 3 {
		t.Errorf("Expected 3 problem exports in stats, got %d", stats.ProblemExportsTotal)
	}
	if stats.GetTotalExports() != 8 {
		t.Errorf("Expected 8 total exports in stats, got %d", stats.GetTotalExports())
	}

	// Verify webhook-specific stats
	if len(stats.WebhookStats) != 1 {
		t.Errorf("Expected 1 webhook in stats, got %d", len(stats.WebhookStats))
	}

	webhookStats := stats.WebhookStats["primary-webhook"]
	if webhookStats.RequestsTotal != 8 {
		t.Errorf("Expected 8 webhook requests in stats, got %d", webhookStats.RequestsTotal)
	}
	if webhookStats.RequestsSuccess != 8 {
		t.Errorf("Expected 8 successful webhook requests in stats, got %d", webhookStats.RequestsSuccess)
	}
}

// TestHTTPExporterRetryLogic tests the retry mechanism with a flaky server
func TestHTTPExporterRetryLogic(t *testing.T) {
	var mu sync.Mutex
	var attempts []int
	attemptCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attemptCount++
		currentAttempt := attemptCount
		attempts = append(attempts, currentAttempt)
		mu.Unlock()

		// Fail first two attempts, succeed on third
		if currentAttempt <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Server temporarily unavailable"))
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(WebhookResponse{Success: true})
	}))
	defer server.Close()

	config := &types.HTTPExporterConfig{
		Enabled:   true,
		Workers:   1,
		QueueSize: 5,
		Timeout:   10 * time.Second,
		Retry: types.RetryConfig{
			MaxAttempts: 2,
			BaseDelay:   50 * time.Millisecond,
			MaxDelay:    500 * time.Millisecond,
		},
		Webhooks: []types.WebhookEndpoint{
			{
				Name:    "retry-webhook",
				URL:     server.URL,
				Timeout: 5 * time.Second,
				Auth:    types.AuthConfig{Type: "none"},
				Retry: &types.RetryConfig{
					MaxAttempts: 4,
					BaseDelay:   10 * time.Millisecond,
					MaxDelay:    100 * time.Millisecond,
				},
				SendStatus: true,
			},
		},
	}

	settings := &types.GlobalSettings{NodeName: "retry-test-node"}

	exporter, err := NewHTTPExporter(config, settings)
	if err != nil {
		t.Fatalf("Failed to create HTTP exporter: %v", err)
	}

	ctx := context.Background()
	err = exporter.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start exporter: %v", err)
	}
	defer exporter.Stop()

	// Export a status that will trigger retries
	status := &types.Status{
		Source:     "retry-test-monitor",
		Timestamp:  time.Now(),
		Events:     []types.Event{},
		Conditions: []types.Condition{},
	}

	err = exporter.ExportStatus(ctx, status)
	if err != nil {
		t.Fatalf("Failed to export status: %v", err)
	}

	// Wait for retries to complete
	time.Sleep(2 * time.Second)

	mu.Lock()
	defer mu.Unlock()

	// Should have 3 attempts (2 failures + 1 success)
	if len(attempts) != 3 {
		t.Errorf("Expected 3 attempts, got %d: %v", len(attempts), attempts)
	}

	// Check final stats
	stats := exporter.GetStats()
	if stats.StatusExportsSuccess != 1 {
		t.Errorf("Expected 1 successful status export, got %d", stats.StatusExportsSuccess)
	}
	if stats.StatusExportsFailed != 0 {
		t.Errorf("Expected 0 failed status exports, got %d", stats.StatusExportsFailed)
	}
}

// TestHTTPExporterConcurrentLoad tests the exporter under concurrent load
func TestHTTPExporterConcurrentLoad(t *testing.T) {
	var mu sync.Mutex
	var receivedRequests int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedRequests++
		mu.Unlock()

		// Simulate some processing time
		time.Sleep(10 * time.Millisecond)

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(WebhookResponse{Success: true})
	}))
	defer server.Close()

	config := &types.HTTPExporterConfig{
		Enabled:   true,
		Workers:   5, // Multiple workers for concurrent processing
		QueueSize: 100,
		Timeout:   10 * time.Second,
		Retry: types.RetryConfig{
			MaxAttempts: 2,
			BaseDelay:   50 * time.Millisecond,
			MaxDelay:    500 * time.Millisecond,
		},
		Webhooks: []types.WebhookEndpoint{
			{
				Name:         "load-test-webhook",
				URL:          server.URL,
				Timeout:      5 * time.Second,
				Auth:         types.AuthConfig{Type: "none"},
				Retry:        &types.RetryConfig{MaxAttempts: 1, BaseDelay: 10 * time.Millisecond, MaxDelay: 100 * time.Millisecond},
				SendStatus:   true,
				SendProblems: true,
			},
		},
	}

	settings := &types.GlobalSettings{NodeName: "load-test-node"}

	exporter, err := NewHTTPExporter(config, settings)
	if err != nil {
		t.Fatalf("Failed to create HTTP exporter: %v", err)
	}

	ctx := context.Background()
	err = exporter.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start exporter: %v", err)
	}
	defer exporter.Stop()

	// Launch concurrent exports
	const numExports = 50
	var wg sync.WaitGroup
	errors := make(chan error, numExports)

	// Export statuses concurrently
	for i := 0; i < numExports/2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			status := &types.Status{
				Source:     fmt.Sprintf("concurrent-monitor-%d", id),
				Timestamp:  time.Now(),
				Events:     []types.Event{},
				Conditions: []types.Condition{},
			}
			if err := exporter.ExportStatus(ctx, status); err != nil {
				errors <- err
			}
		}(i)
	}

	// Export problems concurrently
	for i := 0; i < numExports/2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			problem := &types.Problem{
				Type:       "ConcurrentProblem",
				Resource:   fmt.Sprintf("resource-%d", id),
				Severity:   types.ProblemWarning,
				Message:    fmt.Sprintf("Concurrent problem %d", id),
				DetectedAt: time.Now(),
				Metadata:   map[string]string{"id": fmt.Sprintf("%d", id)},
			}
			if err := exporter.ExportProblem(ctx, problem); err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("Export error: %v", err)
	}

	// Wait for processing to complete
	time.Sleep(3 * time.Second)

	// Verify all requests were received
	mu.Lock()
	finalCount := receivedRequests
	mu.Unlock()

	if finalCount != numExports {
		t.Errorf("Expected %d requests to be received, got %d", numExports, finalCount)
	}

	// Check stats
	stats := exporter.GetStats()
	totalExports := stats.GetTotalExports()
	if totalExports != numExports {
		t.Errorf("Expected %d total exports in stats, got %d", numExports, totalExports)
	}

	successRate := stats.GetSuccessRate()
	if successRate < 100.0 {
		t.Errorf("Expected 100%% success rate, got %.1f%%", successRate)
	}
}

// TestHTTPExporterGracefulShutdown tests that the exporter shuts down gracefully
func TestHTTPExporterGracefulShutdown(t *testing.T) {
	var mu sync.Mutex
	var processedRequests int
	var inProgressRequests int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		inProgressRequests++
		mu.Unlock()

		// Simulate processing time
		time.Sleep(300 * time.Millisecond)

		mu.Lock()
		inProgressRequests--
		processedRequests++
		mu.Unlock()

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(WebhookResponse{Success: true})
	}))
	// Don't defer server.Close() here - we need the server to stay alive during graceful shutdown

	config := &types.HTTPExporterConfig{
		Enabled:   true,
		Workers:   2,
		QueueSize: 10,
		Timeout:   15 * time.Second,
		Retry: types.RetryConfig{
			MaxAttempts: 2,
			BaseDelay:   50 * time.Millisecond,
			MaxDelay:    500 * time.Millisecond,
		},
		Webhooks: []types.WebhookEndpoint{
			{
				Name:         "shutdown-test-webhook",
				URL:          server.URL,
				Timeout:      10 * time.Second,
				Auth:         types.AuthConfig{Type: "none"},
				Retry:        &types.RetryConfig{MaxAttempts: 1, BaseDelay: 10 * time.Millisecond, MaxDelay: 100 * time.Millisecond},
				SendStatus:   true,
			},
		},
	}

	settings := &types.GlobalSettings{NodeName: "shutdown-test-node"}

	exporter, err := NewHTTPExporter(config, settings)
	if err != nil {
		t.Fatalf("Failed to create HTTP exporter: %v", err)
	}

	ctx := context.Background()
	err = exporter.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start exporter: %v", err)
	}

	// Submit exactly 2 exports (matching number of workers for predictable timing)
	for i := 0; i < 2; i++ {
		status := &types.Status{
			Source:     fmt.Sprintf("shutdown-monitor-%d", i),
			Timestamp:  time.Now(),
			Events:     []types.Event{},
			Conditions: []types.Condition{},
		}
		err = exporter.ExportStatus(ctx, status)
		if err != nil {
			t.Errorf("Failed to export status %d: %v", i, err)
		}
	}

	// Give some time for exports to start processing
	time.Sleep(50 * time.Millisecond)

	// Stop the exporter (should wait for in-progress requests)
	stopStart := time.Now()
	err = exporter.Stop()
	stopDuration := time.Since(stopStart)

	// Now it's safe to close the server
	server.Close()

	if err != nil {
		t.Errorf("Failed to stop exporter: %v", err)
	}

	// Should have taken some time to complete in-progress requests
	// With 2 requests, 2 workers, 300ms each: expect ~300ms total (1 round)
	if stopDuration < 200*time.Millisecond {
		t.Errorf("Stop completed too quickly (%v), may not have waited for in-progress requests", stopDuration)
	}
	if stopDuration > 2*time.Second {
		t.Errorf("Stop took too long (%v), may not be shutting down gracefully", stopDuration)
	}

	mu.Lock()
	final := processedRequests
	stillInProgress := inProgressRequests
	mu.Unlock()

	if stillInProgress > 0 {
		t.Errorf("Still have %d requests in progress after stop", stillInProgress)
	}

	// Should have processed all 2 requests
	if final != 2 {
		t.Errorf("Expected exactly 2 processed requests, got %d", final)
	}

	t.Logf("Graceful shutdown completed in %v, processed %d requests", stopDuration, final)
}