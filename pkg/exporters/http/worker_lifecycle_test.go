package http

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// TestWorkerPool_IsStarted tests the IsStarted method
func TestWorkerPool_IsStarted(t *testing.T) {
	config := &types.HTTPExporterConfig{
		Enabled:   true,
		Workers:   1,
		QueueSize: 10,
		Timeout:   30 * time.Second,
		Retry: types.RetryConfig{
			MaxAttempts: 2,
			BaseDelay:   100 * time.Millisecond,
			MaxDelay:    1 * time.Second,
		},
		Webhooks: []types.WebhookEndpoint{
			{
				Name:    "test-webhook",
				URL:     "https://example.com/webhook",
				Timeout: 30 * time.Second,
				Auth: types.AuthConfig{
					Type: "none",
				},
				SendStatus:   true,
				SendProblems: true,
			},
		},
	}

	settings := &types.GlobalSettings{
		NodeName: "test-node",
	}

	exporter, err := NewHTTPExporter(config, settings)
	if err != nil {
		t.Fatalf("Failed to create HTTP exporter: %v", err)
	}

	// Check IsStarted before starting
	if exporter.workerPool.IsStarted() {
		t.Error("IsStarted() should return false before Start()")
	}

	// Start the exporter
	ctx := context.Background()
	if err := exporter.Start(ctx); err != nil {
		t.Fatalf("Failed to start exporter: %v", err)
	}

	// Check IsStarted after starting
	if !exporter.workerPool.IsStarted() {
		t.Error("IsStarted() should return true after Start()")
	}

	// Stop the exporter
	if err := exporter.Stop(); err != nil {
		t.Fatalf("Failed to stop exporter: %v", err)
	}

	// Check IsStarted after stopping
	if exporter.workerPool.IsStarted() {
		t.Error("IsStarted() should return false after Stop()")
	}
}

// TestWorkerPool_RecordFailure tests the recordFailure method
func TestWorkerPool_RecordFailure(t *testing.T) {
	config := &types.HTTPExporterConfig{
		Enabled:   true,
		Workers:   1,
		QueueSize: 10,
		Timeout:   30 * time.Second,
		Retry: types.RetryConfig{
			MaxAttempts: 2,
			BaseDelay:   100 * time.Millisecond,
			MaxDelay:    1 * time.Second,
		},
		Webhooks: []types.WebhookEndpoint{
			{
				Name:    "test-webhook",
				URL:     "https://example.com/webhook",
				Timeout: 30 * time.Second,
				Auth: types.AuthConfig{
					Type: "none",
				},
				SendStatus:   true,
				SendProblems: true,
			},
		},
	}

	settings := &types.GlobalSettings{
		NodeName: "test-node",
	}

	exporter, err := NewHTTPExporter(config, settings)
	if err != nil {
		t.Fatalf("Failed to create HTTP exporter: %v", err)
	}

	// Get initial stats
	stats := exporter.GetStats()
	initialStatusFailures := stats.StatusExportsFailed
	initialProblemFailures := stats.ProblemExportsFailed

	// Record a status failure
	testErr := errors.New("test error")
	exporter.workerPool.recordFailure("status", "test-webhook", 100*time.Millisecond, testErr)

	// Check stats were updated
	stats = exporter.GetStats()
	statusFailures := stats.StatusExportsFailed
	if statusFailures != initialStatusFailures+1 {
		t.Errorf("recordFailure(status) did not increment status failures: got %d, want %d", statusFailures, initialStatusFailures+1)
	}

	// Record a problem failure
	exporter.workerPool.recordFailure("problem", "test-webhook", 100*time.Millisecond, testErr)

	// Check stats were updated
	stats = exporter.GetStats()
	problemFailures := stats.ProblemExportsFailed
	if problemFailures != initialProblemFailures+1 {
		t.Errorf("recordFailure(problem) did not increment problem failures: got %d, want %d", problemFailures, initialProblemFailures+1)
	}
}

// TestWorkerPool_RecordFailure_InvalidType tests recordFailure with invalid request type
func TestWorkerPool_RecordFailure_InvalidType(t *testing.T) {
	config := &types.HTTPExporterConfig{
		Enabled:   true,
		Workers:   1,
		QueueSize: 10,
		Timeout:   30 * time.Second,
		Retry: types.RetryConfig{
			MaxAttempts: 2,
			BaseDelay:   100 * time.Millisecond,
			MaxDelay:    1 * time.Second,
		},
		Webhooks: []types.WebhookEndpoint{
			{
				Name:    "test-webhook",
				URL:     "https://example.com/webhook",
				Timeout: 30 * time.Second,
				Auth: types.AuthConfig{
					Type: "none",
				},
				SendStatus:   true,
				SendProblems: true,
			},
		},
	}

	settings := &types.GlobalSettings{
		NodeName: "test-node",
	}

	exporter, err := NewHTTPExporter(config, settings)
	if err != nil {
		t.Fatalf("Failed to create HTTP exporter: %v", err)
	}

	// Get initial stats
	stats := exporter.GetStats()
	initialStatusFailures := stats.StatusExportsFailed
	initialProblemFailures := stats.ProblemExportsFailed

	// Record failure with invalid type (should not panic or crash)
	testErr := errors.New("test error")
	exporter.workerPool.recordFailure("invalid-type", "test-webhook", 100*time.Millisecond, testErr)

	// Verify stats didn't change (invalid type is ignored)
	stats = exporter.GetStats()
	statusFailures := stats.StatusExportsFailed
	problemFailures := stats.ProblemExportsFailed

	if statusFailures != initialStatusFailures {
		t.Errorf("recordFailure(invalid-type) incorrectly incremented status failures")
	}
	if problemFailures != initialProblemFailures {
		t.Errorf("recordFailure(invalid-type) incorrectly incremented problem failures")
	}
}
