package prometheus

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
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
	port := freePort(t)
	config := &types.PrometheusExporterConfig{
		Enabled:   true,
		Port:      port,
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

	// Wait for server to be ready before making requests
	addr := fmt.Sprintf("localhost:%d", config.Port)
	if err := waitForServerReady(addr, 5*time.Second); err != nil {
		t.Fatalf("server never became ready: %v", err)
	}

	resp, err := newTestHTTPClient().Get(fmt.Sprintf("http://localhost:%d%s", config.Port, config.Path))
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
	port := freePort(t)
	config := &types.PrometheusExporterConfig{
		Enabled:   true,
		Port:      port,
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

// TestExportStatusRecordsMonitorCycle verifies that ExportStatus records the
// per-cycle self-metrics (MonitorCyclesTotal, MonitorCheckDuration,
// MonitorCycleLastTimestamp) and classifies the cycle result based on the
// presence of a ConditionFalse condition.
func TestExportStatusRecordsMonitorCycle(t *testing.T) {
	port := freePort(t)
	config := &types.PrometheusExporterConfig{
		Enabled:   true,
		Port:      port,
		Path:      "/metrics",
		Namespace: "test",
	}
	settings := &types.GlobalSettings{NodeName: "test-node"}

	exporter, err := NewPrometheusExporter(config, settings)
	if err != nil {
		t.Fatalf("failed to create exporter: %v", err)
	}

	ctx := context.Background()
	if err := exporter.Start(ctx); err != nil {
		t.Fatalf("failed to start exporter: %v", err)
	}
	defer exporter.Stop()

	// Healthy cycle (no ConditionFalse) -> result=success.
	healthy := &types.Status{
		Source:    "disk-monitor",
		Timestamp: time.Now(),
		Conditions: []types.Condition{
			{Type: "DiskHealthy", Status: types.ConditionTrue, Reason: "OK", Message: "ok", Transition: time.Now()},
		},
	}
	if err := exporter.ExportStatus(ctx, healthy); err != nil {
		t.Fatalf("failed to export healthy status: %v", err)
	}

	// Unhealthy cycle (ConditionFalse) -> result=error.
	unhealthy := &types.Status{
		Source:    "disk-monitor",
		Timestamp: time.Now(),
		Conditions: []types.Condition{
			{Type: "DiskHealthy", Status: types.ConditionFalse, Reason: "Full", Message: "disk full", Transition: time.Now()},
		},
	}
	if err := exporter.ExportStatus(ctx, unhealthy); err != nil {
		t.Fatalf("failed to export unhealthy status: %v", err)
	}

	families, err := exporter.registry.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	if got, ok := counterValue(families, "test_monitor_cycles_total", map[string]string{
		"monitor_name": "disk-monitor", "result": "success",
	}); !ok || got != 1 {
		t.Errorf("monitor_cycles_total{result=success} = %v (found=%v), want 1", got, ok)
	}
	if got, ok := counterValue(families, "test_monitor_cycles_total", map[string]string{
		"monitor_name": "disk-monitor", "result": "error",
	}); !ok || got != 1 {
		t.Errorf("monitor_cycles_total{result=error} = %v (found=%v), want 1", got, ok)
	}
	if got, ok := histogramSampleCount(families, "test_monitor_check_duration_seconds", map[string]string{
		"monitor_name": "disk-monitor",
	}); !ok || got != 2 {
		t.Errorf("monitor_check_duration_seconds sample count = %v (found=%v), want 2", got, ok)
	}
	if got, ok := gaugeValue(families, "test_monitor_cycle_last_timestamp_seconds", map[string]string{
		"monitor_name": "disk-monitor",
	}); !ok || got <= 0 {
		t.Errorf("monitor_cycle_last_timestamp_seconds = %v (found=%v), want > 0", got, ok)
	}
}

func TestExportProblem(t *testing.T) {
	port := freePort(t)
	config := &types.PrometheusExporterConfig{
		Enabled:   true,
		Port:      port,
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
	port := freePort(t)
	config := &types.PrometheusExporterConfig{
		Enabled:   true,
		Port:      port,
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

	var wg sync.WaitGroup
	errCh := make(chan error, numGoroutines*numOperations*2)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
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

	// Wait for all goroutines to finish, then collect any errors
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent operation failed: %v", err)
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

func TestIsReloadable(t *testing.T) {
	port := freePort(t)
	config := &types.PrometheusExporterConfig{
		Enabled:   true,
		Port:      port,
		Path:      "/metrics",
		Namespace: "test",
	}
	settings := &types.GlobalSettings{NodeName: "test-node"}

	exporter, err := NewPrometheusExporter(config, settings)
	if err != nil {
		t.Fatalf("failed to create exporter: %v", err)
	}

	// Test IsReloadable returns true
	if !exporter.IsReloadable() {
		t.Errorf("expected IsReloadable to return true")
	}
}

func TestReload(t *testing.T) {
	port1 := freePort(t)
	port2 := freePort(t)

	config := &types.PrometheusExporterConfig{
		Enabled:   true,
		Port:      port1,
		Path:      "/metrics",
		Namespace: "test",
		Subsystem: "sub1",
		Labels:    map[string]string{"env": "test"},
	}
	settings := &types.GlobalSettings{NodeName: "test-node"}

	exporter, err := NewPrometheusExporter(config, settings)
	if err != nil {
		t.Fatalf("failed to create exporter: %v", err)
	}

	// Use t.Cleanup for reliable resource cleanup
	t.Cleanup(func() {
		if exporter != nil {
			exporter.Stop()
		}
	})

	ctx := context.Background()
	err = exporter.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start exporter: %v", err)
	}

	tests := []struct {
		name          string
		newConfig     interface{}
		expectedError bool
		errorContains string
	}{
		{
			name:          "invalid config type",
			newConfig:     "invalid",
			expectedError: true,
			errorContains: "invalid config type",
		},
		{
			name:          "nil config",
			newConfig:     (*types.PrometheusExporterConfig)(nil),
			expectedError: true,
			errorContains: "cannot be nil",
		},
		{
			name: "invalid port in new config",
			newConfig: &types.PrometheusExporterConfig{
				Enabled: true,
				Port:    70000,
			},
			expectedError: true,
			errorContains: "invalid port",
		},
		{
			name: "same config - no restart needed",
			newConfig: &types.PrometheusExporterConfig{
				Enabled:   true,
				Port:      port1,
				Path:      "/metrics",
				Namespace: "test",
				Subsystem: "sub1",
				Labels:    map[string]string{"env": "test"},
			},
			expectedError: false,
		},
		{
			name: "different port - restart needed",
			newConfig: &types.PrometheusExporterConfig{
				Enabled:   true,
				Port:      port2,
				Path:      "/metrics",
				Namespace: "test",
				Subsystem: "sub1",
			},
			expectedError: false,
		},
		{
			name: "different path - restart needed",
			newConfig: &types.PrometheusExporterConfig{
				Enabled:   true,
				Port:      port2,
				Path:      "/custom-metrics",
				Namespace: "test",
			},
			expectedError: false,
		},
		{
			name: "different namespace - metrics recreation needed",
			newConfig: &types.PrometheusExporterConfig{
				Enabled:   true,
				Port:      port2,
				Path:      "/custom-metrics",
				Namespace: "new_namespace",
			},
			expectedError: false,
		},
		{
			name: "different subsystem - metrics recreation needed",
			newConfig: &types.PrometheusExporterConfig{
				Enabled:   true,
				Port:      port2,
				Path:      "/custom-metrics",
				Namespace: "new_namespace",
				Subsystem: "new_subsystem",
			},
			expectedError: false,
		},
		{
			name: "different labels - metrics recreation needed",
			newConfig: &types.PrometheusExporterConfig{
				Enabled:   true,
				Port:      port2,
				Path:      "/custom-metrics",
				Namespace: "new_namespace",
				Labels:    map[string]string{"env": "prod", "region": "us-east"},
			},
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := exporter.Reload(tt.newConfig)

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
			}
		})
	}
}

func TestReloadNotStarted(t *testing.T) {
	port1 := freePort(t)
	port2 := freePort(t)

	config := &types.PrometheusExporterConfig{
		Enabled:   true,
		Port:      port1,
		Path:      "/metrics",
		Namespace: "test",
	}
	settings := &types.GlobalSettings{NodeName: "test-node"}

	exporter, err := NewPrometheusExporter(config, settings)
	if err != nil {
		t.Fatalf("failed to create exporter: %v", err)
	}

	// Reload without starting - should work but not restart server
	newConfig := &types.PrometheusExporterConfig{
		Enabled:   true,
		Port:      port2,
		Path:      "/new-metrics",
		Namespace: "new_test",
	}

	err = exporter.Reload(newConfig)
	if err != nil {
		t.Errorf("unexpected error reloading not-started exporter: %v", err)
	}
}

func TestNeedsServerRestart(t *testing.T) {
	port := freePort(t)
	config := &types.PrometheusExporterConfig{
		Enabled:   true,
		Port:      port,
		Path:      "/metrics",
		Namespace: "test",
	}
	settings := &types.GlobalSettings{NodeName: "test-node"}

	exporter, err := NewPrometheusExporter(config, settings)
	if err != nil {
		t.Fatalf("failed to create exporter: %v", err)
	}

	tests := []struct {
		name        string
		oldConfig   *types.PrometheusExporterConfig
		newConfig   *types.PrometheusExporterConfig
		needRestart bool
	}{
		{
			name:      "nil old config",
			oldConfig: nil,
			newConfig: &types.PrometheusExporterConfig{
				Port: 9100,
				Path: "/metrics",
			},
			needRestart: true,
		},
		{
			name: "same config",
			oldConfig: &types.PrometheusExporterConfig{
				Port:      9100,
				Path:      "/metrics",
				Namespace: "test",
				Subsystem: "sub",
			},
			newConfig: &types.PrometheusExporterConfig{
				Port:      9100,
				Path:      "/metrics",
				Namespace: "test",
				Subsystem: "sub",
			},
			needRestart: false,
		},
		{
			name: "different port",
			oldConfig: &types.PrometheusExporterConfig{
				Port: 9100,
				Path: "/metrics",
			},
			newConfig: &types.PrometheusExporterConfig{
				Port: 9200,
				Path: "/metrics",
			},
			needRestart: true,
		},
		{
			name: "different path",
			oldConfig: &types.PrometheusExporterConfig{
				Port: 9100,
				Path: "/metrics",
			},
			newConfig: &types.PrometheusExporterConfig{
				Port: 9100,
				Path: "/custom",
			},
			needRestart: true,
		},
		{
			name: "different namespace triggers restart via metrics recreation",
			oldConfig: &types.PrometheusExporterConfig{
				Port:      9100,
				Path:      "/metrics",
				Namespace: "old",
			},
			newConfig: &types.PrometheusExporterConfig{
				Port:      9100,
				Path:      "/metrics",
				Namespace: "new",
			},
			needRestart: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := exporter.needsServerRestart(tt.oldConfig, tt.newConfig)
			if result != tt.needRestart {
				t.Errorf("expected needsServerRestart=%v, got %v", tt.needRestart, result)
			}
		})
	}
}

func TestNeedsMetricsRecreation(t *testing.T) {
	port := freePort(t)
	config := &types.PrometheusExporterConfig{
		Enabled:   true,
		Port:      port,
		Path:      "/metrics",
		Namespace: "test",
	}
	settings := &types.GlobalSettings{NodeName: "test-node"}

	exporter, err := NewPrometheusExporter(config, settings)
	if err != nil {
		t.Fatalf("failed to create exporter: %v", err)
	}

	tests := []struct {
		name         string
		oldConfig    *types.PrometheusExporterConfig
		newConfig    *types.PrometheusExporterConfig
		needRecreate bool
	}{
		{
			name:      "nil old config",
			oldConfig: nil,
			newConfig: &types.PrometheusExporterConfig{
				Namespace: "test",
			},
			needRecreate: true,
		},
		{
			name: "same namespace and subsystem",
			oldConfig: &types.PrometheusExporterConfig{
				Namespace: "test",
				Subsystem: "sub",
				Labels:    map[string]string{"env": "test"},
			},
			newConfig: &types.PrometheusExporterConfig{
				Namespace: "test",
				Subsystem: "sub",
				Labels:    map[string]string{"env": "test"},
			},
			needRecreate: false,
		},
		{
			name: "different namespace",
			oldConfig: &types.PrometheusExporterConfig{
				Namespace: "old",
			},
			newConfig: &types.PrometheusExporterConfig{
				Namespace: "new",
			},
			needRecreate: true,
		},
		{
			name: "different subsystem",
			oldConfig: &types.PrometheusExporterConfig{
				Namespace: "test",
				Subsystem: "old",
			},
			newConfig: &types.PrometheusExporterConfig{
				Namespace: "test",
				Subsystem: "new",
			},
			needRecreate: true,
		},
		{
			name: "different labels",
			oldConfig: &types.PrometheusExporterConfig{
				Namespace: "test",
				Labels:    map[string]string{"env": "dev"},
			},
			newConfig: &types.PrometheusExporterConfig{
				Namespace: "test",
				Labels:    map[string]string{"env": "prod"},
			},
			needRecreate: true,
		},
		{
			name: "different label count",
			oldConfig: &types.PrometheusExporterConfig{
				Namespace: "test",
				Labels:    map[string]string{"env": "dev"},
			},
			newConfig: &types.PrometheusExporterConfig{
				Namespace: "test",
				Labels:    map[string]string{"env": "dev", "region": "us"},
			},
			needRecreate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := exporter.needsMetricsRecreation(tt.oldConfig, tt.newConfig)
			if result != tt.needRecreate {
				t.Errorf("expected needsMetricsRecreation=%v, got %v", tt.needRecreate, result)
			}
		})
	}
}

func TestLabelsEqual(t *testing.T) {
	tests := []struct {
		name   string
		old    map[string]string
		new    map[string]string
		expect bool
	}{
		{
			name:   "both nil",
			old:    nil,
			new:    nil,
			expect: true,
		},
		{
			name:   "both empty",
			old:    map[string]string{},
			new:    map[string]string{},
			expect: true,
		},
		{
			name:   "same labels",
			old:    map[string]string{"env": "test", "region": "us"},
			new:    map[string]string{"env": "test", "region": "us"},
			expect: true,
		},
		{
			name:   "different values",
			old:    map[string]string{"env": "test"},
			new:    map[string]string{"env": "prod"},
			expect: false,
		},
		{
			name:   "different keys",
			old:    map[string]string{"env": "test"},
			new:    map[string]string{"region": "test"},
			expect: false,
		},
		{
			name:   "different lengths - old longer",
			old:    map[string]string{"env": "test", "region": "us"},
			new:    map[string]string{"env": "test"},
			expect: false,
		},
		{
			name:   "different lengths - new longer",
			old:    map[string]string{"env": "test"},
			new:    map[string]string{"env": "test", "region": "us"},
			expect: false,
		},
		{
			name:   "one nil one empty",
			old:    nil,
			new:    map[string]string{},
			expect: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := labelsEqual(tt.old, tt.new)
			if result != tt.expect {
				t.Errorf("expected labelsEqual=%v, got %v", tt.expect, result)
			}
		})
	}
}

func TestIsValidMetricName(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect bool
	}{
		{
			name:   "empty string",
			input:  "",
			expect: false,
		},
		{
			name:   "valid lowercase",
			input:  "node_doctor",
			expect: true,
		},
		{
			name:   "valid uppercase",
			input:  "NODE_DOCTOR",
			expect: true,
		},
		{
			name:   "valid mixed case",
			input:  "Node_Doctor",
			expect: true,
		},
		{
			name:   "valid with colon",
			input:  "node:doctor",
			expect: true,
		},
		{
			name:   "valid with numbers",
			input:  "node_doctor_v2",
			expect: true,
		},
		{
			name:   "starts with underscore",
			input:  "_node_doctor",
			expect: true,
		},
		{
			name:   "starts with colon",
			input:  ":node_doctor",
			expect: true,
		},
		{
			name:   "starts with number - invalid",
			input:  "1node_doctor",
			expect: false,
		},
		{
			name:   "contains hyphen - invalid",
			input:  "node-doctor",
			expect: false,
		},
		{
			name:   "contains space - invalid",
			input:  "node doctor",
			expect: false,
		},
		{
			name:   "contains special char - invalid",
			input:  "node@doctor",
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidMetricName(tt.input)
			if result != tt.expect {
				t.Errorf("isValidMetricName(%q) = %v, want %v", tt.input, result, tt.expect)
			}
		})
	}
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name          string
		config        *types.PrometheusExporterConfig
		expectedError bool
		errorContains string
	}{
		{
			name: "valid config",
			config: &types.PrometheusExporterConfig{
				Port:      9100,
				Path:      "/metrics",
				Namespace: "node_doctor",
				Subsystem: "exporter",
			},
			expectedError: false,
		},
		{
			name: "port zero - valid default",
			config: &types.PrometheusExporterConfig{
				Port: 0,
			},
			expectedError: false,
		},
		{
			name: "negative port - invalid",
			config: &types.PrometheusExporterConfig{
				Port: -1,
			},
			expectedError: true,
			errorContains: "invalid port",
		},
		{
			name: "port too high - invalid",
			config: &types.PrometheusExporterConfig{
				Port: 65536,
			},
			expectedError: true,
			errorContains: "invalid port",
		},
		{
			name: "path without leading slash - invalid",
			config: &types.PrometheusExporterConfig{
				Port: 9100,
				Path: "metrics",
			},
			expectedError: true,
			errorContains: "path must start with",
		},
		{
			name: "invalid namespace",
			config: &types.PrometheusExporterConfig{
				Port:      9100,
				Path:      "/metrics",
				Namespace: "node-doctor", // hyphen invalid
			},
			expectedError: true,
			errorContains: "invalid namespace",
		},
		{
			name: "invalid subsystem",
			config: &types.PrometheusExporterConfig{
				Port:      9100,
				Path:      "/metrics",
				Namespace: "node_doctor",
				Subsystem: "my-sub", // hyphen invalid
			},
			expectedError: true,
			errorContains: "invalid subsystem",
		},
		{
			name: "empty path - valid",
			config: &types.PrometheusExporterConfig{
				Port: 9100,
				Path: "",
			},
			expectedError: false,
		},
		{
			name: "empty namespace - valid",
			config: &types.PrometheusExporterConfig{
				Port:      9100,
				Namespace: "",
			},
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(tt.config)

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
			}
		})
	}
}

func TestShutdownServer(t *testing.T) {
	// Test nil server
	err := shutdownServer(nil, 5*time.Second)
	if err != nil {
		t.Errorf("expected nil error for nil server, got: %v", err)
	}

	// Test shutdown of valid server
	port := freePort(t)
	config := &types.PrometheusExporterConfig{
		Enabled:   true,
		Port:      port,
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

	// Wait for server to be ready using deterministic readiness check
	addr := fmt.Sprintf("localhost:%d", config.Port)
	if err := waitForServerReady(addr, 5*time.Second); err != nil {
		t.Fatalf("server never became ready: %v", err)
	}

	// Verify server is running
	resp, err := newTestHTTPClient().Get(fmt.Sprintf("http://localhost:%d/health", config.Port))
	if err != nil {
		t.Fatalf("failed to connect to server: %v", err)
	}
	resp.Body.Close()

	// Test normal shutdown — server.Shutdown is synchronous so the port
	// should be closed by the time shutdownServer returns.
	err = shutdownServer(exporter.server, 5*time.Second)
	if err != nil {
		t.Errorf("unexpected error during shutdown: %v", err)
	}

	// Poll until the port is fully closed instead of relying on a fixed sleep
	if err := waitForPortClosed(addr, 5*time.Second); err != nil {
		t.Fatalf("server port never closed after shutdown: %v", err)
	}

	client := &http.Client{Timeout: 500 * time.Millisecond}
	_, err = client.Get(fmt.Sprintf("http://localhost:%d/health", config.Port))
	if err == nil {
		t.Errorf("expected error connecting to stopped server")
	}
}

func TestExportProblemBeforeStart(t *testing.T) {
	port := freePort(t)
	config := &types.PrometheusExporterConfig{
		Enabled:   true,
		Port:      port,
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
	problem := &types.Problem{
		Type:       "DiskPressure",
		Severity:   types.ProblemWarning,
		Resource:   "/dev/sda1",
		DetectedAt: time.Now(),
		Message:    "Disk usage high",
	}

	err = exporter.ExportProblem(ctx, problem)
	if err == nil {
		t.Errorf("expected error when exporting to stopped exporter")
	}
	if !contains(err.Error(), "not started") {
		t.Errorf("expected error to contain 'not started', got: %v", err)
	}
}

func TestPrometheusExporter_StartBindFailure(t *testing.T) {
	// Occupy a port so the exporter cannot bind to it.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to grab a free port: %v", err)
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port

	config := &types.PrometheusExporterConfig{
		Enabled:   true,
		Port:      port,
		Path:      "/metrics",
		Namespace: "test",
	}
	settings := &types.GlobalSettings{NodeName: "test-node"}

	exporter, err := NewPrometheusExporter(config, settings)
	if err != nil {
		t.Fatalf("NewPrometheusExporter() error = %v", err)
	}

	ctx := context.Background()
	if err := exporter.Start(ctx); err == nil {
		exporter.Stop()
		t.Fatal("Start() should fail when port is already in use")
	}

	// Exporter must not be marked as started after a bind failure.
	if exporter.started {
		t.Error("exporter.started should be false after a bind failure")
	}
}

// TestNewPrometheusExporter_DualStackDefault verifies an empty BindAddress
// defaults to "::" (dual-stack) in the constructor.
func TestNewPrometheusExporter_DualStackDefault(t *testing.T) {
	config := &types.PrometheusExporterConfig{
		Enabled:   true,
		Port:      freePort(t),
		Namespace: "test",
	}
	settings := &types.GlobalSettings{NodeName: "test-node"}

	exporter, err := NewPrometheusExporter(config, settings)
	if err != nil {
		t.Fatalf("failed to create exporter: %v", err)
	}
	if exporter.config.BindAddress != "::" {
		t.Errorf("default BindAddress = %q, want %q", exporter.config.BindAddress, "::")
	}
}

// TestPrometheusExporter_DualStackServesRequest verifies the exporter binds with
// the default "::" (dual-stack) BindAddress and serves /metrics. The bind has an
// automatic IPv4 fallback, so this passes whether or not IPv6 is available.
func TestPrometheusExporter_DualStackServesRequest(t *testing.T) {
	port := freePort(t)
	config := &types.PrometheusExporterConfig{
		Enabled:   true,
		Port:      port,
		Path:      "/metrics",
		Namespace: "test",
		// BindAddress intentionally left empty -> defaults to "::".
	}
	settings := &types.GlobalSettings{NodeName: "test-node"}

	exporter, err := NewPrometheusExporter(config, settings)
	if err != nil {
		t.Fatalf("failed to create exporter: %v", err)
	}
	if err := exporter.Start(context.Background()); err != nil {
		t.Fatalf("failed to start exporter: %v", err)
	}
	defer func() { _ = exporter.Stop() }()

	addr := fmt.Sprintf("localhost:%d", port)
	if err := waitForServerReady(addr, 5*time.Second); err != nil {
		t.Fatalf("server never became ready: %v", err)
	}
	resp, err := newTestHTTPClient().Get(fmt.Sprintf("http://localhost:%d%s", port, config.Path))
	if err != nil {
		t.Fatalf("failed to connect to metrics server: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

// TestPrometheusExporter_ExplicitBindAddressHonored verifies an explicit
// BindAddress is used as-is and serves a request.
func TestPrometheusExporter_ExplicitBindAddressHonored(t *testing.T) {
	port := freePort(t)
	config := &types.PrometheusExporterConfig{
		Enabled:     true,
		BindAddress: "127.0.0.1",
		Port:        port,
		Path:        "/metrics",
		Namespace:   "test",
	}
	settings := &types.GlobalSettings{NodeName: "test-node"}

	exporter, err := NewPrometheusExporter(config, settings)
	if err != nil {
		t.Fatalf("failed to create exporter: %v", err)
	}
	if err := exporter.Start(context.Background()); err != nil {
		t.Fatalf("failed to start exporter: %v", err)
	}
	defer func() { _ = exporter.Stop() }()

	host, _, err := net.SplitHostPort(exporter.server.Addr)
	if err != nil {
		t.Fatalf("SplitHostPort(%q) error = %v", exporter.server.Addr, err)
	}
	if host != "127.0.0.1" {
		t.Errorf("bound host = %q, want 127.0.0.1", host)
	}

	addr := fmt.Sprintf("localhost:%d", port)
	if err := waitForServerReady(addr, 5*time.Second); err != nil {
		t.Fatalf("server never became ready: %v", err)
	}
	resp, err := newTestHTTPClient().Get(fmt.Sprintf("http://localhost:%d%s", port, config.Path))
	if err != nil {
		t.Fatalf("failed to connect to metrics server: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestIsDualStackHost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"", true},
		{"::", true},
		{"::1", true},
		{"fe80::1", true},
		{"0.0.0.0", false},
		{"127.0.0.1", false},
	}
	for _, tt := range tests {
		if got := isDualStackHost(tt.host); got != tt.want {
			t.Errorf("isDualStackHost(%q) = %v, want %v", tt.host, got, tt.want)
		}
	}
}

func TestListenWithFallback_Success(t *testing.T) {
	ln, err := listenWithFallback("127.0.0.1", 0)
	if err != nil {
		t.Fatalf("listenWithFallback() error = %v", err)
	}
	defer ln.Close()
	if ln.Addr() == nil {
		t.Fatal("listenWithFallback() returned nil Addr")
	}
}
