package prometheus

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

func TestNewPrometheusExporter(t *testing.T) {
	tests := []struct {
		name          string
		config        *types.PrometheusExporterConfig
		settings      *types.GlobalSettings
		expectedError bool
		errorContains string
	}{
		{
			name:          "nil config",
			config:        nil,
			settings:      &types.GlobalSettings{NodeName: "test-node"},
			expectedError: true,
			errorContains: "config cannot be nil",
		},
		{
			name:          "nil settings",
			config:        &types.PrometheusExporterConfig{Enabled: true},
			settings:      nil,
			expectedError: true,
			errorContains: "settings cannot be nil",
		},
		{
			name:          "disabled exporter",
			config:        &types.PrometheusExporterConfig{Enabled: false},
			settings:      &types.GlobalSettings{NodeName: "test-node"},
			expectedError: true,
			errorContains: "Prometheus exporter is disabled",
		},
		{
			name:          "empty node name",
			config:        &types.PrometheusExporterConfig{Enabled: true},
			settings:      &types.GlobalSettings{NodeName: ""},
			expectedError: true,
			errorContains: "node name is required",
		},
		{
			name: "invalid port",
			config: &types.PrometheusExporterConfig{
				Enabled: true,
				Port:    70000,
			},
			settings:      &types.GlobalSettings{NodeName: "test-node"},
			expectedError: true,
			errorContains: "invalid port",
		},
		{
			name: "invalid path",
			config: &types.PrometheusExporterConfig{
				Enabled: true,
				Port:    9100,
				Path:    "metrics", // missing leading slash
			},
			settings:      &types.GlobalSettings{NodeName: "test-node"},
			expectedError: true,
			errorContains: "path must start with",
		},
		{
			name: "valid configuration",
			config: &types.PrometheusExporterConfig{
				Enabled:   true,
				Port:      9101,
				Path:      "/metrics",
				Namespace: "test_namespace",
				Labels:    map[string]string{"env": "test"},
			},
			settings:      &types.GlobalSettings{NodeName: "test-node"},
			expectedError: false,
		},
		{
			name: "valid configuration with defaults",
			config: &types.PrometheusExporterConfig{
				Enabled: true,
			},
			settings:      &types.GlobalSettings{NodeName: "test-node"},
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exporter, err := NewPrometheusExporter(tt.config, tt.settings)

			if tt.expectedError {
				if err == nil {
					t.Errorf("expected error but got none")
				} else if tt.errorContains != "" && !contains(err.Error(), tt.errorContains) {
					t.Errorf("expected error to contain '%s', got: %v", tt.errorContains, err)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if exporter == nil {
				t.Errorf("expected exporter to be created")
				return
			}

			// Verify defaults were set
			if exporter.config.Port == 0 {
				t.Errorf("expected default port to be set")
			}
			if exporter.config.Path == "" {
				t.Errorf("expected default path to be set")
			}
			if exporter.config.Namespace == "" {
				t.Errorf("expected default namespace to be set")
			}
		})
	}
}

func TestPrometheusExporterLifecycle(t *testing.T) {
	config := &types.PrometheusExporterConfig{
		Enabled:   true,
		Port:      9102, // Use different port for each test
		Path:      "/metrics",
		Namespace: "test",
	}
	settings := &types.GlobalSettings{NodeName: "test-node"}

	exporter, err := NewPrometheusExporter(config, settings)
	if err != nil {
		t.Fatalf("failed to create exporter: %v", err)
	}

	ctx := context.Background()

	// Test starting
	err = exporter.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start exporter: %v", err)
	}

	// Verify server is running
	time.Sleep(200 * time.Millisecond) // Give server time to start
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d%s", config.Port, config.Path))
	if err != nil {
		t.Fatalf("failed to connect to metrics server: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Test double start (should fail)
	err = exporter.Start(ctx)
	if err == nil {
		t.Errorf("expected error when starting already started exporter")
	}

	// Test stopping
	err = exporter.Stop()
	if err != nil {
		t.Errorf("failed to stop exporter: %v", err)
	}

	// Test double stop (should not fail)
	err = exporter.Stop()
	if err != nil {
		t.Errorf("unexpected error when stopping already stopped exporter: %v", err)
	}
}

func TestExportStatus(t *testing.T) {
	config := &types.PrometheusExporterConfig{
		Enabled:   true,
		Port:      9103,
		Path:      "/metrics",
		Namespace: "test",
	}
	settings := &types.GlobalSettings{NodeName: "test-node"}

	exporter, err := NewPrometheusExporter(config, settings)
	if err != nil {
		t.Fatalf("failed to create exporter: %v", err)
	}

	ctx := context.Background()

	// Test export before starting (should fail)
	status := &types.Status{
		Source:    "test",
		Timestamp: time.Now(),
	}
	err = exporter.ExportStatus(ctx, status)
	if err == nil {
		t.Errorf("expected error when exporting to stopped exporter")
	}

	// Start exporter
	err = exporter.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start exporter: %v", err)
	}
	defer exporter.Stop()

	// Test nil status
	err = exporter.ExportStatus(ctx, nil)
	if err == nil {
		t.Errorf("expected error for nil status")
	}

	// Test valid status
	status = &types.Status{
		Source:    "test",
		Timestamp: time.Now(),
		Events: []types.Event{
			{
				Severity:  types.EventInfo,
				Timestamp: time.Now(),
				Reason:    "test-event",
				Message:   "Test event",
			},
		},
		Conditions: []types.Condition{
			{
				Type:       "Ready",
				Status:     types.ConditionTrue,
				Reason:     "NodeReady",
				Message:    "Node is ready",
				Transition: time.Now(),
			},
		},
	}

	err = exporter.ExportStatus(ctx, status)
	if err != nil {
		t.Errorf("failed to export valid status: %v", err)
	}
}

func TestExportProblem(t *testing.T) {
	config := &types.PrometheusExporterConfig{
		Enabled:   true,
		Port:      9104,
		Path:      "/metrics",
		Namespace: "test",
	}
	settings := &types.GlobalSettings{NodeName: "test-node"}

	exporter, err := NewPrometheusExporter(config, settings)
	if err != nil {
		t.Fatalf("failed to create exporter: %v", err)
	}

	ctx := context.Background()

	// Start exporter
	err = exporter.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start exporter: %v", err)
	}
	defer exporter.Stop()

	// Test nil problem
	err = exporter.ExportProblem(ctx, nil)
	if err == nil {
		t.Errorf("expected error for nil problem")
	}

	// Test valid problem
	problem := &types.Problem{
		Type:       "DiskPressure",
		Severity:   types.ProblemWarning,
		Resource:   "/dev/sda1",
		DetectedAt: time.Now(),
		Message:    "Disk usage high",
	}

	err = exporter.ExportProblem(ctx, problem)
	if err != nil {
		t.Errorf("failed to export valid problem: %v", err)
	}

	// Test another problem to check active problems tracking
	problem2 := &types.Problem{
		Type:       "MemoryPressure",
		Severity:   types.ProblemCritical,
		Resource:   "/proc/meminfo",
		DetectedAt: time.Now(),
		Message:    "Memory usage critical",
	}

	err = exporter.ExportProblem(ctx, problem2)
	if err != nil {
		t.Errorf("failed to export second problem: %v", err)
	}
}

func TestConcurrentExports(t *testing.T) {
	config := &types.PrometheusExporterConfig{
		Enabled:   true,
		Port:      9105,
		Path:      "/metrics",
		Namespace: "test",
	}
	settings := &types.GlobalSettings{NodeName: "test-node"}

	exporter, err := NewPrometheusExporter(config, settings)
	if err != nil {
		t.Fatalf("failed to create exporter: %v", err)
	}

	ctx := context.Background()
	err = exporter.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start exporter: %v", err)
	}
	defer exporter.Stop()

	// Run concurrent exports
	const numGoroutines = 10
	const numOperations = 10

	errCh := make(chan error, numGoroutines*numOperations*2)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < numOperations; j++ {
				// Export status
				status := &types.Status{
					Source:    fmt.Sprintf("test-%d", id),
					Timestamp: time.Now(),
					Events: []types.Event{
						{
							Severity:  types.EventInfo,
							Timestamp: time.Now(),
							Reason:    fmt.Sprintf("test-event-%d-%d", id, j),
							Message:   fmt.Sprintf("Test event %d-%d", id, j),
						},
					},
				}
				if err := exporter.ExportStatus(ctx, status); err != nil {
					errCh <- fmt.Errorf("status export %d-%d failed: %w", id, j, err)
				}

				// Export problem
				problem := &types.Problem{
					Type:       fmt.Sprintf("TestProblem-%d", id),
					Severity:   types.ProblemWarning,
					Resource:   fmt.Sprintf("/resource/%d", j),
					DetectedAt: time.Now(),
					Message:    fmt.Sprintf("Test problem %d-%d", id, j),
				}
				if err := exporter.ExportProblem(ctx, problem); err != nil {
					errCh <- fmt.Errorf("problem export %d-%d failed: %w", id, j, err)
				}
			}
		}(i)
	}

	// Wait for all operations and check for errors
	timeout := time.After(10 * time.Second)
	expectedOps := numGoroutines * numOperations * 2
	completedOps := 0

	for completedOps < expectedOps {
		select {
		case err := <-errCh:
			t.Errorf("concurrent operation failed: %v", err)
		case <-timeout:
			t.Fatalf("timeout waiting for concurrent operations")
		default:
			completedOps++
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// Helper function to check if a string contains another string
func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[len(s)-len(substr):] == substr ||
		len(s) > len(substr) && s[:len(substr)] == substr ||
		len(s) > len(substr) &&
			func() bool {
				for i := 0; i <= len(s)-len(substr); i++ {
					if s[i:i+len(substr)] == substr {
						return true
					}
				}
				return false
			}()
}
