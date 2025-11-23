package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

func TestNewHTTPExporter(t *testing.T) {
	config := &types.HTTPExporterConfig{
		Enabled:   true,
		Workers:   2,
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
				Retry: &types.RetryConfig{
					MaxAttempts: 3,
					BaseDelay:   1 * time.Second,
					MaxDelay:    30 * time.Second,
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

	if exporter == nil {
		t.Fatal("Expected non-nil exporter")
	}

	// Test configuration access
	exporterConfig := exporter.GetConfiguration()
	if exporterConfig["workers"] != 2 {
		t.Errorf("Expected 2 workers, got %v", exporterConfig["workers"])
	}
	if exporterConfig["queueSize"] != 10 {
		t.Errorf("Expected queue size 10, got %v", exporterConfig["queueSize"])
	}
}

func TestNewHTTPExporterValidation(t *testing.T) {
	tests := []struct {
		name     string
		config   *types.HTTPExporterConfig
		settings *types.GlobalSettings
		wantErr  bool
	}{
		{
			name:     "nil config",
			config:   nil,
			settings: &types.GlobalSettings{NodeName: "test-node"},
			wantErr:  true,
		},
		{
			name:     "nil settings",
			config:   &types.HTTPExporterConfig{Enabled: true},
			settings: nil,
			wantErr:  true,
		},
		{
			name:     "disabled exporter",
			config:   &types.HTTPExporterConfig{Enabled: false},
			settings: &types.GlobalSettings{NodeName: "test-node"},
			wantErr:  true,
		},
		{
			name: "empty node name",
			config: &types.HTTPExporterConfig{
				Enabled: true,
				Timeout: 30 * time.Second,
				Retry: types.RetryConfig{
					MaxAttempts: 2,
					BaseDelay:   100 * time.Millisecond,
					MaxDelay:    1 * time.Second,
				},
				Webhooks: []types.WebhookEndpoint{
					{
						Name:    "test",
						URL:     "https://example.com",
						Timeout: 30 * time.Second,
						Auth:    types.AuthConfig{Type: "none"},
						Retry: &types.RetryConfig{
							MaxAttempts: 1,
							BaseDelay:   1 * time.Second,
							MaxDelay:    30 * time.Second,
						},
						SendStatus: true,
					},
				},
			},
			settings: &types.GlobalSettings{NodeName: ""},
			wantErr:  true,
		},
		{
			name: "no webhooks",
			config: &types.HTTPExporterConfig{
				Enabled:  true,
				Timeout:  30 * time.Second,
				Webhooks: []types.WebhookEndpoint{},
				Retry: types.RetryConfig{
					MaxAttempts: 2,
					BaseDelay:   100 * time.Millisecond,
					MaxDelay:    1 * time.Second,
				},
			},
			settings: &types.GlobalSettings{NodeName: "test-node"},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewHTTPExporter(tt.config, tt.settings)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewHTTPExporter() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHTTPExporterLifecycle(t *testing.T) {
	config := &types.HTTPExporterConfig{
		Enabled:   true,
		Workers:   1,
		QueueSize: 5,
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
				Retry: &types.RetryConfig{
					MaxAttempts: 1,
					BaseDelay:   1 * time.Second,
					MaxDelay:    30 * time.Second,
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

	// Test start
	ctx := context.Background()
	err = exporter.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start exporter: %v", err)
	}

	// Check health status
	health := exporter.GetHealthStatus()
	if health["started"] != true {
		t.Error("Expected exporter to be started")
	}
	if health["workerCount"] != 1 {
		t.Errorf("Expected 1 worker, got %v", health["workerCount"])
	}

	// Test double start (should fail)
	err = exporter.Start(ctx)
	if err == nil {
		t.Error("Expected error when starting already started exporter")
	}

	// Test stop
	err = exporter.Stop()
	if err != nil {
		t.Errorf("Failed to stop exporter: %v", err)
	}

	// Test double stop (should not fail)
	err = exporter.Stop()
	if err != nil {
		t.Errorf("Expected no error when stopping already stopped exporter: %v", err)
	}
}

func TestHTTPExporterExportStatus(t *testing.T) {
	// Create test server
	received := make(chan WebhookRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req WebhookRequest
		json.NewDecoder(r.Body).Decode(&req)
		received <- req

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(WebhookResponse{Success: true})
	}))
	defer server.Close()

	config := &types.HTTPExporterConfig{
		Enabled:   true,
		Workers:   1,
		QueueSize: 5,
		Timeout:   30 * time.Second,
		Retry: types.RetryConfig{
			MaxAttempts: 2,
			BaseDelay:   100 * time.Millisecond,
			MaxDelay:    1 * time.Second,
		},
		Webhooks: []types.WebhookEndpoint{
			{
				Name:    "test-webhook",
				URL:     server.URL,
				Timeout: 30 * time.Second,
				Auth: types.AuthConfig{
					Type: "none",
				},
				Retry: &types.RetryConfig{
					MaxAttempts: 1,
					BaseDelay:   1 * time.Second,
					MaxDelay:    30 * time.Second,
				},
				SendStatus:   true,
				SendProblems: false,
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

	ctx := context.Background()
	err = exporter.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start exporter: %v", err)
	}
	defer exporter.Stop()

	// Create and export status
	status := &types.Status{
		Source:     "test-monitor",
		Timestamp:  time.Now(),
		Events:     []types.Event{},
		Conditions: []types.Condition{},
	}

	err = exporter.ExportStatus(ctx, status)
	if err != nil {
		t.Fatalf("Failed to export status: %v", err)
	}

	// Wait for webhook request
	select {
	case req := <-received:
		if req.Type != "status" {
			t.Errorf("Expected type 'status', got %s", req.Type)
		}
		if req.NodeName != "test-node" {
			t.Errorf("Expected nodeName 'test-node', got %s", req.NodeName)
		}
		if req.Status == nil {
			t.Error("Expected status data")
		}
		if req.Problem != nil {
			t.Error("Expected no problem data")
		}
	case <-time.After(5 * time.Second):
		t.Error("Timeout waiting for webhook request")
	}
}

func TestHTTPExporterExportProblem(t *testing.T) {
	// Create test server
	received := make(chan WebhookRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req WebhookRequest
		json.NewDecoder(r.Body).Decode(&req)
		received <- req

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(WebhookResponse{Success: true})
	}))
	defer server.Close()

	config := &types.HTTPExporterConfig{
		Enabled:   true,
		Workers:   1,
		QueueSize: 5,
		Timeout:   30 * time.Second,
		Retry: types.RetryConfig{
			MaxAttempts: 2,
			BaseDelay:   100 * time.Millisecond,
			MaxDelay:    1 * time.Second,
		},
		Webhooks: []types.WebhookEndpoint{
			{
				Name:    "test-webhook",
				URL:     server.URL,
				Timeout: 30 * time.Second,
				Auth: types.AuthConfig{
					Type: "none",
				},
				Retry: &types.RetryConfig{
					MaxAttempts: 1,
					BaseDelay:   1 * time.Second,
					MaxDelay:    30 * time.Second,
				},
				SendStatus:   false,
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

	ctx := context.Background()
	err = exporter.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start exporter: %v", err)
	}
	defer exporter.Stop()

	// Create and export problem
	problem := &types.Problem{
		Type:       "test-problem",
		Resource:   "test-resource",
		Severity:   types.ProblemCritical,
		Message:    "Test problem message",
		DetectedAt: time.Now(),
		Metadata:   make(map[string]string),
	}

	err = exporter.ExportProblem(ctx, problem)
	if err != nil {
		t.Fatalf("Failed to export problem: %v", err)
	}

	// Wait for webhook request
	select {
	case req := <-received:
		if req.Type != "problem" {
			t.Errorf("Expected type 'problem', got %s", req.Type)
		}
		if req.NodeName != "test-node" {
			t.Errorf("Expected nodeName 'test-node', got %s", req.NodeName)
		}
		if req.Problem == nil {
			t.Error("Expected problem data")
		}
		if req.Status != nil {
			t.Error("Expected no status data")
		}
	case <-time.After(5 * time.Second):
		t.Error("Timeout waiting for webhook request")
	}
}

func TestHTTPExporterValidation(t *testing.T) {
	config := &types.HTTPExporterConfig{
		Enabled:   true,
		Workers:   1,
		QueueSize: 5,
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
				Retry: &types.RetryConfig{
					MaxAttempts: 1,
					BaseDelay:   1 * time.Second,
					MaxDelay:    30 * time.Second,
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

	ctx := context.Background()
	err = exporter.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start exporter: %v", err)
	}
	defer exporter.Stop()

	// Test nil status
	err = exporter.ExportStatus(ctx, nil)
	if err == nil {
		t.Error("Expected error for nil status")
	}

	// Test nil problem
	err = exporter.ExportProblem(ctx, nil)
	if err == nil {
		t.Error("Expected error for nil problem")
	}

	// Test invalid status
	invalidStatus := &types.Status{
		// Missing required fields
	}
	err = exporter.ExportStatus(ctx, invalidStatus)
	if err == nil {
		t.Error("Expected error for invalid status")
	}

	// Test invalid problem
	invalidProblem := &types.Problem{
		// Missing required fields
	}
	err = exporter.ExportProblem(ctx, invalidProblem)
	if err == nil {
		t.Error("Expected error for invalid problem")
	}
}

func TestHTTPExporterNotStarted(t *testing.T) {
	config := &types.HTTPExporterConfig{
		Enabled:   true,
		Workers:   1,
		QueueSize: 5,
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
				Auth:    types.AuthConfig{Type: "none"},
				Retry: &types.RetryConfig{
					MaxAttempts: 1,
					BaseDelay:   1 * time.Second,
					MaxDelay:    30 * time.Second,
				},
				SendStatus: true,
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

	// Try to export without starting
	status := &types.Status{
		Source:     "test-monitor",
		Timestamp:  time.Now(),
		Events:     []types.Event{},
		Conditions: []types.Condition{},
	}

	err = exporter.ExportStatus(context.Background(), status)
	if err == nil {
		t.Error("Expected error when exporting before starting")
	}
}

func TestHTTPExporterStats(t *testing.T) {
	config := &types.HTTPExporterConfig{
		Enabled:   true,
		Workers:   1,
		QueueSize: 5,
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
				Auth:    types.AuthConfig{Type: "none"},
				Retry: &types.RetryConfig{
					MaxAttempts: 1,
					BaseDelay:   1 * time.Second,
					MaxDelay:    30 * time.Second,
				},
				SendStatus: true,
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
	if stats.GetTotalExports() != 0 {
		t.Errorf("Expected 0 initial exports, got %d", stats.GetTotalExports())
	}

	startTime := stats.StartTime
	if startTime.IsZero() {
		t.Error("Expected start time to be set")
	}

	uptime := stats.GetUptime()
	if uptime <= 0 {
		t.Error("Expected positive uptime")
	}

	// Test rate calculations with zero values
	if stats.GetSuccessRate() != 0.0 {
		t.Errorf("Expected 0%% success rate with no exports, got %.1f%%", stats.GetSuccessRate())
	}
}

func TestHTTPExporterHealthStatus(t *testing.T) {
	config := &types.HTTPExporterConfig{
		Enabled:   true,
		Workers:   2,
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
				Auth:    types.AuthConfig{Type: "none"},
				Retry: &types.RetryConfig{
					MaxAttempts: 1,
					BaseDelay:   1 * time.Second,
					MaxDelay:    30 * time.Second,
				},
				SendStatus: true,
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

	// Test before starting
	health := exporter.GetHealthStatus()
	if health["started"] != false {
		t.Error("Expected exporter to not be started initially")
	}

	// Start and test again
	ctx := context.Background()
	err = exporter.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start exporter: %v", err)
	}
	defer exporter.Stop()

	health = exporter.GetHealthStatus()
	if health["started"] != true {
		t.Error("Expected exporter to be started")
	}
	if health["workerCount"] != 2 {
		t.Errorf("Expected 2 workers, got %v", health["workerCount"])
	}
	if health["queueCapacity"] != 10 {
		t.Errorf("Expected queue capacity 10, got %v", health["queueCapacity"])
	}
	if health["webhookCount"] != 1 {
		t.Errorf("Expected 1 webhook, got %v", health["webhookCount"])
	}

	// Check webhook health
	webhookHealth := health["webhookHealth"].(map[string]interface{})
	if len(webhookHealth) != 1 {
		t.Errorf("Expected 1 webhook health entry, got %d", len(webhookHealth))
	}
}

func TestHTTPExporterMultipleWebhooks(t *testing.T) {
	// Create two test servers
	server1Requests := make(chan WebhookRequest, 1)
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req WebhookRequest
		json.NewDecoder(r.Body).Decode(&req)
		server1Requests <- req
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(WebhookResponse{Success: true})
	}))
	defer server1.Close()

	server2Requests := make(chan WebhookRequest, 1)
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req WebhookRequest
		json.NewDecoder(r.Body).Decode(&req)
		server2Requests <- req
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(WebhookResponse{Success: true})
	}))
	defer server2.Close()

	config := &types.HTTPExporterConfig{
		Enabled:   true,
		Workers:   2,
		QueueSize: 10,
		Timeout:   30 * time.Second,
		Retry: types.RetryConfig{
			MaxAttempts: 2,
			BaseDelay:   100 * time.Millisecond,
			MaxDelay:    1 * time.Second,
		},
		Webhooks: []types.WebhookEndpoint{
			{
				Name:         "webhook1",
				URL:          server1.URL,
				Timeout:      30 * time.Second,
				Auth:         types.AuthConfig{Type: "none"},
				Retry:        &types.RetryConfig{MaxAttempts: 1, BaseDelay: 1 * time.Second, MaxDelay: 30 * time.Second},
				SendStatus:   true,
				SendProblems: false,
			},
			{
				Name:         "webhook2",
				URL:          server2.URL,
				Timeout:      30 * time.Second,
				Auth:         types.AuthConfig{Type: "none"},
				Retry:        &types.RetryConfig{MaxAttempts: 1, BaseDelay: 1 * time.Second, MaxDelay: 30 * time.Second},
				SendStatus:   true,
				SendProblems: true,
			},
		},
	}

	settings := &types.GlobalSettings{NodeName: "test-node"}

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

	// Export status - should go to both webhooks
	status := &types.Status{
		Source:     "test-monitor",
		Timestamp:  time.Now(),
		Events:     []types.Event{},
		Conditions: []types.Condition{},
	}

	err = exporter.ExportStatus(ctx, status)
	if err != nil {
		t.Fatalf("Failed to export status: %v", err)
	}

	// Both servers should receive the status
	select {
	case <-server1Requests:
		// Good
	case <-time.After(3 * time.Second):
		t.Error("Server1 didn't receive status request")
	}

	select {
	case <-server2Requests:
		// Good
	case <-time.After(3 * time.Second):
		t.Error("Server2 didn't receive status request")
	}

	// Export problem - should only go to webhook2
	problem := &types.Problem{
		Type:       "test-problem",
		Resource:   "test-resource",
		Severity:   types.ProblemCritical,
		Message:    "Test problem",
		DetectedAt: time.Now(),
		Metadata:   make(map[string]string),
	}

	err = exporter.ExportProblem(ctx, problem)
	if err != nil {
		t.Fatalf("Failed to export problem: %v", err)
	}

	// Only server2 should receive the problem
	select {
	case <-server2Requests:
		// Good
	case <-time.After(3 * time.Second):
		t.Error("Server2 didn't receive problem request")
	}

	// Server1 should not receive the problem (it has SendProblems: false)
	select {
	case <-server1Requests:
		t.Error("Server1 unexpectedly received problem request")
	case <-time.After(1 * time.Second):
		// Good - timeout means no request was received
	}
}

// TestHTTPExporter_IsReloadable tests the IsReloadable method.
func TestHTTPExporter_IsReloadable(t *testing.T) {
	config := &types.HTTPExporterConfig{
		Enabled:   true,
		Workers:   1,
		QueueSize: 5,
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
				Auth:    types.AuthConfig{Type: "none"},
				Retry: &types.RetryConfig{
					MaxAttempts: 1,
					BaseDelay:   1 * time.Second,
					MaxDelay:    30 * time.Second,
				},
				SendStatus: true,
			},
		},
	}

	settings := &types.GlobalSettings{NodeName: "test-node"}

	exporter, err := NewHTTPExporter(config, settings)
	if err != nil {
		t.Fatalf("Failed to create HTTP exporter: %v", err)
	}

	// HTTP exporter should always be reloadable
	if !exporter.IsReloadable() {
		t.Error("Expected IsReloadable() to return true")
	}
}

// TestHTTPExporter_Reload tests the Reload method with various scenarios.
func TestHTTPExporter_Reload(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(WebhookResponse{Success: true})
	}))
	defer server.Close()

	baseConfig := &types.HTTPExporterConfig{
		Enabled:   true,
		Workers:   1,
		QueueSize: 5,
		Timeout:   30 * time.Second,
		Retry: types.RetryConfig{
			MaxAttempts: 2,
			BaseDelay:   100 * time.Millisecond,
			MaxDelay:    1 * time.Second,
		},
		Webhooks: []types.WebhookEndpoint{
			{
				Name:    "test-webhook",
				URL:     server.URL,
				Timeout: 30 * time.Second,
				Auth:    types.AuthConfig{Type: "none"},
				Retry: &types.RetryConfig{
					MaxAttempts: 1,
					BaseDelay:   1 * time.Second,
					MaxDelay:    30 * time.Second,
				},
				SendStatus: true,
			},
		},
	}

	settings := &types.GlobalSettings{NodeName: "test-node"}

	tests := []struct {
		name        string
		newConfig   interface{}
		expectError bool
		startFirst  bool
	}{
		{
			name:        "wrong config type",
			newConfig:   "invalid-config",
			expectError: true,
			startFirst:  false,
		},
		{
			name:        "nil config",
			newConfig:   (*types.HTTPExporterConfig)(nil),
			expectError: true,
			startFirst:  false,
		},
		{
			name: "empty webhooks",
			newConfig: &types.HTTPExporterConfig{
				Enabled:   true,
				Workers:   1,
				QueueSize: 5,
				Timeout:   30 * time.Second,
				Retry: types.RetryConfig{
					MaxAttempts: 2,
					BaseDelay:   100 * time.Millisecond,
					MaxDelay:    1 * time.Second,
				},
				Webhooks: []types.WebhookEndpoint{},
			},
			expectError: true,
			startFirst:  false,
		},
		{
			name: "valid config without worker pool recreation",
			newConfig: &types.HTTPExporterConfig{
				Enabled:   true,
				Workers:   1,                // Same as initial
				QueueSize: 5,                // Same as initial
				Timeout:   30 * time.Second, // Same as initial
				Retry: types.RetryConfig{
					MaxAttempts: 2,
					BaseDelay:   100 * time.Millisecond,
					MaxDelay:    1 * time.Second,
				},
				Webhooks: []types.WebhookEndpoint{
					{
						Name:    "updated-webhook",
						URL:     server.URL,
						Timeout: 30 * time.Second,
						Auth:    types.AuthConfig{Type: "none"},
						Retry: &types.RetryConfig{
							MaxAttempts: 1,
							BaseDelay:   1 * time.Second,
							MaxDelay:    30 * time.Second,
						},
						SendStatus: true,
					},
				},
			},
			expectError: false,
			startFirst:  true,
		},
		{
			name: "valid config with worker count change",
			newConfig: &types.HTTPExporterConfig{
				Enabled:   true,
				Workers:   2, // Changed from 1 to 2
				QueueSize: 5,
				Timeout:   30 * time.Second,
				Retry: types.RetryConfig{
					MaxAttempts: 2,
					BaseDelay:   100 * time.Millisecond,
					MaxDelay:    1 * time.Second,
				},
				Webhooks: []types.WebhookEndpoint{
					{
						Name:    "test-webhook",
						URL:     server.URL,
						Timeout: 30 * time.Second,
						Auth:    types.AuthConfig{Type: "none"},
						Retry: &types.RetryConfig{
							MaxAttempts: 1,
							BaseDelay:   1 * time.Second,
							MaxDelay:    30 * time.Second,
						},
						SendStatus: true,
					},
				},
			},
			expectError: false,
			startFirst:  true,
		},
		{
			name: "valid config with queue size change",
			newConfig: &types.HTTPExporterConfig{
				Enabled:   true,
				Workers:   1,
				QueueSize: 10, // Changed from 5 to 10
				Timeout:   30 * time.Second,
				Retry: types.RetryConfig{
					MaxAttempts: 2,
					BaseDelay:   100 * time.Millisecond,
					MaxDelay:    1 * time.Second,
				},
				Webhooks: []types.WebhookEndpoint{
					{
						Name:    "test-webhook",
						URL:     server.URL,
						Timeout: 30 * time.Second,
						Auth:    types.AuthConfig{Type: "none"},
						Retry: &types.RetryConfig{
							MaxAttempts: 1,
							BaseDelay:   1 * time.Second,
							MaxDelay:    30 * time.Second,
						},
						SendStatus: true,
					},
				},
			},
			expectError: false,
			startFirst:  true,
		},
		{
			name: "valid config with timeout change",
			newConfig: &types.HTTPExporterConfig{
				Enabled:   true,
				Workers:   1,
				QueueSize: 5,
				Timeout:   60 * time.Second, // Changed from 30s to 60s
				Retry: types.RetryConfig{
					MaxAttempts: 2,
					BaseDelay:   100 * time.Millisecond,
					MaxDelay:    1 * time.Second,
				},
				Webhooks: []types.WebhookEndpoint{
					{
						Name:    "test-webhook",
						URL:     server.URL,
						Timeout: 30 * time.Second,
						Auth:    types.AuthConfig{Type: "none"},
						Retry: &types.RetryConfig{
							MaxAttempts: 1,
							BaseDelay:   1 * time.Second,
							MaxDelay:    30 * time.Second,
						},
						SendStatus: true,
					},
				},
			},
			expectError: false,
			startFirst:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create new exporter for each test
			exporter, err := NewHTTPExporter(baseConfig, settings)
			if err != nil {
				t.Fatalf("Failed to create HTTP exporter: %v", err)
			}
			defer exporter.Stop()

			if tt.startFirst {
				ctx := context.Background()
				if err := exporter.Start(ctx); err != nil {
					t.Fatalf("Failed to start exporter: %v", err)
				}
			}

			err = exporter.Reload(tt.newConfig)
			if (err != nil) != tt.expectError {
				t.Errorf("Reload() error = %v, expectError = %v", err, tt.expectError)
			}
		})
	}
}

// TestHTTPExporter_needsWorkerPoolRecreation tests the needsWorkerPoolRecreation method.
func TestHTTPExporter_needsWorkerPoolRecreation(t *testing.T) {
	config := &types.HTTPExporterConfig{
		Enabled:   true,
		Workers:   2,
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
				Auth:    types.AuthConfig{Type: "none"},
				Retry: &types.RetryConfig{
					MaxAttempts: 1,
					BaseDelay:   1 * time.Second,
					MaxDelay:    30 * time.Second,
				},
				SendStatus: true,
			},
		},
	}

	settings := &types.GlobalSettings{NodeName: "test-node"}

	exporter, err := NewHTTPExporter(config, settings)
	if err != nil {
		t.Fatalf("Failed to create HTTP exporter: %v", err)
	}

	tests := []struct {
		name      string
		oldConfig *types.HTTPExporterConfig
		newConfig *types.HTTPExporterConfig
		expected  bool
	}{
		{
			name:      "nil old config",
			oldConfig: nil,
			newConfig: config,
			expected:  true,
		},
		{
			name:      "same config",
			oldConfig: config,
			newConfig: config,
			expected:  false,
		},
		{
			name:      "worker count changed",
			oldConfig: config,
			newConfig: &types.HTTPExporterConfig{
				Workers:   4,
				QueueSize: 10,
				Timeout:   30 * time.Second,
			},
			expected: true,
		},
		{
			name:      "queue size changed",
			oldConfig: config,
			newConfig: &types.HTTPExporterConfig{
				Workers:   2,
				QueueSize: 20,
				Timeout:   30 * time.Second,
			},
			expected: true,
		},
		{
			name:      "timeout changed",
			oldConfig: config,
			newConfig: &types.HTTPExporterConfig{
				Workers:   2,
				QueueSize: 10,
				Timeout:   60 * time.Second,
			},
			expected: true,
		},
		{
			name:      "no relevant changes",
			oldConfig: config,
			newConfig: &types.HTTPExporterConfig{
				Workers:   2,
				QueueSize: 10,
				Timeout:   30 * time.Second,
				Enabled:   false, // This change doesn't affect worker pool
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := exporter.needsWorkerPoolRecreation(tt.oldConfig, tt.newConfig)
			if got != tt.expected {
				t.Errorf("needsWorkerPoolRecreation() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

// TestHTTPExporter_performHealthCheck tests the performHealthCheck method.
func TestHTTPExporter_performHealthCheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(WebhookResponse{Success: true})
	}))
	defer server.Close()

	config := &types.HTTPExporterConfig{
		Enabled:   true,
		Workers:   1,
		QueueSize: 5,
		Timeout:   30 * time.Second,
		Retry: types.RetryConfig{
			MaxAttempts: 2,
			BaseDelay:   100 * time.Millisecond,
			MaxDelay:    1 * time.Second,
		},
		Webhooks: []types.WebhookEndpoint{
			{
				Name:    "healthy-webhook",
				URL:     server.URL,
				Timeout: 30 * time.Second,
				Auth:    types.AuthConfig{Type: "none"},
				Retry: &types.RetryConfig{
					MaxAttempts: 1,
					BaseDelay:   1 * time.Second,
					MaxDelay:    30 * time.Second,
				},
				SendStatus: true,
			},
		},
	}

	settings := &types.GlobalSettings{NodeName: "test-node"}

	exporter, err := NewHTTPExporter(config, settings)
	if err != nil {
		t.Fatalf("Failed to create HTTP exporter: %v", err)
	}

	ctx := context.Background()
	if err := exporter.Start(ctx); err != nil {
		t.Fatalf("Failed to start exporter: %v", err)
	}
	defer exporter.Stop()

	// Export a status to create webhook stats
	status := &types.Status{
		Source:    "test",
		Timestamp: time.Now(),
	}
	_ = exporter.ExportStatus(ctx, status)

	// Wait for export to complete
	time.Sleep(200 * time.Millisecond)

	// Perform health check - should not panic and should work with stats
	exporter.performHealthCheck()

	// Verify health status
	health := exporter.GetHealthStatus()
	if health["started"] != true {
		t.Error("Expected exporter to be started after health check")
	}
}

// TestHTTPExporter_min tests the min helper function.
func TestHTTPExporter_min(t *testing.T) {
	tests := []struct {
		a, b, expected int
	}{
		{1, 2, 1},
		{2, 1, 1},
		{5, 5, 5},
		{0, 10, 0},
		{-1, 1, -1},
		{-5, -2, -5},
	}

	for _, tt := range tests {
		got := min(tt.a, tt.b)
		if got != tt.expected {
			t.Errorf("min(%d, %d) = %d, expected %d", tt.a, tt.b, got, tt.expected)
		}
	}
}
