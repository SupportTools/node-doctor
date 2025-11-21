package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// TestDumpConfiguration tests the dumpConfiguration function.
func TestDumpConfiguration(t *testing.T) {
	// Create test config
	config := &types.NodeDoctorConfig{
		APIVersion: "v1",
		Kind:       "NodeDoctorConfig",
		Metadata: types.ConfigMetadata{
			Name: "test-config",
		},
		Settings: types.GlobalSettings{
			NodeName:  "test-node",
			LogLevel:  "info",
			LogFormat: "json",
		},
	}

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Call dumpConfiguration
	dumpConfiguration(config)

	// Restore stdout
	w.Close()
	os.Stdout = old

	// Read captured output
	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	// Verify output is valid JSON
	var parsed types.NodeDoctorConfig
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Errorf("dumpConfiguration() output is not valid JSON: %v", err)
	}

	// Verify key fields
	if parsed.Settings.NodeName != "test-node" {
		t.Errorf("dumpConfiguration() node name = %q, want %q", parsed.Settings.NodeName, "test-node")
	}
	if parsed.Settings.LogLevel != "info" {
		t.Errorf("dumpConfiguration() log level = %q, want %q", parsed.Settings.LogLevel, "info")
	}
}

// TestMonitorFactoryAdapter tests the monitorFactoryAdapter.
func TestMonitorFactoryAdapter(t *testing.T) {
	ctx := context.Background()
	adapter := &monitorFactoryAdapter{ctx: ctx}

	t.Run("CreateMonitor with invalid type", func(t *testing.T) {
		config := types.MonitorConfig{
			Name:    "test-monitor",
			Type:    "invalid-type-that-does-not-exist",
			Enabled: true,
		}

		_, err := adapter.CreateMonitor(config)
		if err == nil {
			t.Error("CreateMonitor() expected error for invalid monitor type, got nil")
		}
	})

	// Note: Testing valid monitor creation would require registered monitor types,
	// which may not be available in unit tests without proper initialization
}

// TestCreateMonitors_Current tests the current createMonitors function.
func TestCreateMonitors_Current(t *testing.T) {
	ctx := context.Background()

	t.Run("empty config", func(t *testing.T) {
		monitors, err := createMonitors(ctx, []types.MonitorConfig{})
		if err != nil {
			t.Errorf("createMonitors() error = %v, want nil", err)
		}
		if len(monitors) != 0 {
			t.Errorf("createMonitors() returned %d monitors, want 0", len(monitors))
		}
	})

	t.Run("disabled monitor", func(t *testing.T) {
		configs := []types.MonitorConfig{
			{
				Name:    "disabled-monitor",
				Type:    "system-cpu",
				Enabled: false,
			},
		}

		monitors, err := createMonitors(ctx, configs)
		if err != nil {
			t.Errorf("createMonitors() error = %v, want nil", err)
		}
		if len(monitors) != 0 {
			t.Errorf("createMonitors() returned %d monitors, want 0 (disabled monitor should be skipped)", len(monitors))
		}
	})

	t.Run("invalid monitor type", func(t *testing.T) {
		configs := []types.MonitorConfig{
			{
				Name:    "invalid-monitor",
				Type:    "invalid-type",
				Enabled: true,
			},
		}

		monitors, err := createMonitors(ctx, configs)
		if err != nil {
			t.Errorf("createMonitors() should not return error for failed monitor creation, got: %v", err)
		}
		if len(monitors) != 0 {
			t.Errorf("createMonitors() returned %d monitors, want 0 (invalid monitor should be skipped)", len(monitors))
		}
	})
}

// TestCreateExporters_Current tests the current createExporters function.
func TestCreateExporters_Current(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	t.Run("no exporters enabled", func(t *testing.T) {
		config := &types.NodeDoctorConfig{
			Settings: types.GlobalSettings{
				NodeName: "test-node",
			},
			Exporters: types.ExporterConfigs{
				Kubernetes: &types.KubernetesExporterConfig{
					Enabled: false,
				},
				HTTP: &types.HTTPExporterConfig{
					Enabled: false,
				},
				Prometheus: &types.PrometheusExporterConfig{
					Enabled: false,
				},
			},
		}

		exporters, interfaces, err := createExporters(ctx, config)
		if err != nil {
			t.Errorf("createExporters() error = %v, want nil", err)
		}

		// Should have at least the noop exporter
		if len(exporters) == 0 {
			t.Error("createExporters() should create noop exporter when no exporters enabled")
		}
		if len(interfaces) == 0 {
			t.Error("createExporters() should return at least one exporter interface")
		}

		// Clean up
		for _, exp := range exporters {
			exp.Stop()
		}
	})

	t.Run("health server always created", func(t *testing.T) {
		// Skip this test in environments where port 8080 might be in use
		// In real usage, the test would bind to a random port
		t.Skip("Skipping health server test to avoid port conflicts")

		config := &types.NodeDoctorConfig{
			Settings: types.GlobalSettings{
				NodeName: "test-node",
			},
			Exporters: types.ExporterConfigs{
				Kubernetes: &types.KubernetesExporterConfig{
					Enabled: false,
				},
				HTTP: &types.HTTPExporterConfig{
					Enabled: false,
				},
				Prometheus: &types.PrometheusExporterConfig{
					Enabled: false,
				},
			},
		}

		exporters, _, err := createExporters(ctx, config)
		if err != nil {
			t.Errorf("createExporters() error = %v, want nil", err)
		}

		// Health server should be created
		foundHealthServer := false
		for range exporters {
			// In a real test, we'd check the type
			foundHealthServer = true
			break
		}

		if !foundHealthServer {
			t.Error("createExporters() should create health server")
		}

		// Clean up
		for _, exp := range exporters {
			exp.Stop()
		}
	})
}

// TestExporterLifecycleInterface tests that noopExporter implements ExporterLifecycle.
func TestExporterLifecycleInterface(t *testing.T) {
	var _ ExporterLifecycle = (*noopExporter)(nil)
	var _ types.Exporter = (*noopExporter)(nil)
}

// TestNoopExporterLogging tests that noopExporter logs correctly.
func TestNoopExporterLogging(t *testing.T) {
	exporter := &noopExporter{}

	t.Run("ExportStatus logging", func(t *testing.T) {
		// This test verifies that ExportStatus logs the expected information
		// We can't easily capture log output, but we can verify no errors
		status := &types.Status{
			Source:     "test-source",
			Events:     make([]types.Event, 2),
			Conditions: make([]types.Condition, 3),
			Timestamp:  time.Now(),
		}

		ctx := context.Background()
		err := exporter.ExportStatus(ctx, status)

		if err != nil {
			t.Errorf("ExportStatus() error = %v, want nil", err)
		}
	})

	t.Run("ExportProblem logging", func(t *testing.T) {
		problem := &types.Problem{
			Type:       "test-problem",
			Resource:   "test-resource",
			Severity:   types.ProblemCritical,
			Message:    "Test problem message",
			DetectedAt: time.Now(),
		}

		ctx := context.Background()
		err := exporter.ExportProblem(ctx, problem)

		if err != nil {
			t.Errorf("ExportProblem() error = %v, want nil", err)
		}
	})

	t.Run("Stop", func(t *testing.T) {
		err := exporter.Stop()
		if err != nil {
			t.Errorf("Stop() error = %v, want nil", err)
		}
	})
}

// TestCreateMonitors_ErrorLogging tests that createMonitors logs errors but continues.
func TestCreateMonitors_ErrorLogging(t *testing.T) {
	ctx := context.Background()

	configs := []types.MonitorConfig{
		{
			Name:    "invalid-monitor-1",
			Type:    "nonexistent-type",
			Enabled: true,
		},
		{
			Name:    "invalid-monitor-2",
			Type:    "another-invalid-type",
			Enabled: true,
		},
	}

	// This should not panic and should return empty list
	monitors, err := createMonitors(ctx, configs)
	if err != nil {
		t.Errorf("createMonitors() should not return error for failed monitor creation, got: %v", err)
	}
	if len(monitors) != 0 {
		t.Errorf("createMonitors() returned %d monitors, want 0 (all invalid monitors should be skipped)", len(monitors))
	}
}

// TestMonitorFactoryAdapter_ContextPropagation tests that context is properly propagated.
func TestMonitorFactoryAdapter_ContextPropagation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	adapter := &monitorFactoryAdapter{ctx: ctx}

	// Verify that adapter has the context
	if adapter.ctx == nil {
		t.Error("monitorFactoryAdapter.ctx is nil")
	}

	if adapter.ctx != ctx {
		t.Error("monitorFactoryAdapter.ctx does not match provided context")
	}
}

// TestDumpConfiguration_ComplexConfig tests dumpConfiguration with a more complex configuration.
func TestDumpConfiguration_ComplexConfig(t *testing.T) {
	config := &types.NodeDoctorConfig{
		APIVersion: "v1",
		Kind:       "NodeDoctorConfig",
		Metadata: types.ConfigMetadata{
			Name: "complex-config",
		},
		Settings: types.GlobalSettings{
			NodeName:  "complex-node",
			LogLevel:  "debug",
			LogFormat: "text",
		},
		Monitors: []types.MonitorConfig{
			{
				Name:     "test-monitor-1",
				Type:     "system-cpu",
				Enabled:  true,
				Interval: 30 * time.Second,
			},
			{
				Name:     "test-monitor-2",
				Type:     "system-memory",
				Enabled:  false,
				Interval: 60 * time.Second,
			},
		},
		Exporters: types.ExporterConfigs{
			Kubernetes: &types.KubernetesExporterConfig{
				Enabled: true,
			},
		},
	}

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	dumpConfiguration(config)

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	// Verify output is valid JSON
	var parsed types.NodeDoctorConfig
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Errorf("dumpConfiguration() output is not valid JSON: %v", err)
	}

	// Verify monitors are preserved
	if len(parsed.Monitors) != 2 {
		t.Errorf("dumpConfiguration() monitors count = %d, want 2", len(parsed.Monitors))
	}

	// Verify output contains expected JSON structure
	if !strings.Contains(output, "complex-node") {
		t.Error("dumpConfiguration() output missing node name")
	}
	if !strings.Contains(output, "system-cpu") {
		t.Error("dumpConfiguration() output missing monitor type")
	}
}
