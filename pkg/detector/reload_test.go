package detector

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/reload"
	"github.com/supporttools/node-doctor/pkg/types"
)

// TestStopMonitorByName tests the stopMonitorByName function.
func TestStopMonitorByName(t *testing.T) {
	tests := []struct {
		name          string
		monitorName   string
		setupMonitors func() (*ProblemDetector, error)
		wantError     bool
		errorMsg      string
	}{
		{
			name:        "stop existing monitor",
			monitorName: "test-monitor",
			setupMonitors: func() (*ProblemDetector, error) {
				helper := NewTestHelper()
				config := helper.CreateTestConfig()
				config.Monitors = []types.MonitorConfig{
					helper.CreateTestMonitorConfig("test-monitor", "test"),
				}
				factory := NewMockMonitorFactory()

				detector, err := NewProblemDetector(
					config,
					[]types.Monitor{},
					[]types.Exporter{NewMockExporter("test-exporter")},
					"/tmp/test-config.yaml",
					factory,
				)
				if err != nil {
					return nil, err
				}

				// Start the detector to create monitor handles
				detector.Start()
				time.Sleep(50 * time.Millisecond) // Give monitors time to start
				return detector, nil
			},
			wantError: false,
		},
		{
			name:        "stop non-existent monitor",
			monitorName: "nonexistent-monitor",
			setupMonitors: func() (*ProblemDetector, error) {
				helper := NewTestHelper()
				config := helper.CreateTestConfig()
				config.Monitors = []types.MonitorConfig{
					helper.CreateTestMonitorConfig("test-monitor", "test"),
				}
				factory := NewMockMonitorFactory()

				detector, err := NewProblemDetector(
					config,
					[]types.Monitor{},
					[]types.Exporter{NewMockExporter("test-exporter")},
					"/tmp/test-config.yaml",
					factory,
				)
				if err != nil {
					return nil, err
				}

				// Start the detector to create monitor handles
				detector.Start()
				time.Sleep(50 * time.Millisecond) // Give monitors time to start
				return detector, nil
			},
			wantError: true,
			errorMsg:  "monitor nonexistent-monitor not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pd, err := tt.setupMonitors()
			if err != nil {
				t.Fatalf("Failed to setup monitors: %v", err)
			}
			defer pd.Stop() // Clean shutdown

			err = pd.stopMonitorByName(tt.monitorName)

			if tt.wantError {
				if err == nil {
					t.Errorf("stopMonitorByName() expected error but got none")
				} else if err.Error() != tt.errorMsg {
					t.Errorf("stopMonitorByName() error = %v, want %v", err.Error(), tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("stopMonitorByName() unexpected error = %v", err)
				}
			}
		})
	}
}

// TestEmitReloadEvent tests the emitReloadEvent function.
func TestEmitReloadEvent(t *testing.T) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()
	factory := NewMockMonitorFactory()

	pd, err := NewProblemDetector(
		config,
		[]types.Monitor{},
		[]types.Exporter{NewMockExporter("test-exporter")},
		"/tmp/test-config.yaml",
		factory,
	)
	if err != nil {
		t.Fatalf("Failed to create problem detector: %v", err)
	}

	tests := []struct {
		name     string
		severity types.EventSeverity
		reason   string
		message  string
	}{
		{
			name:     "info event",
			severity: types.EventInfo,
			reason:   "ReloadStarted",
			message:  "Configuration reload initiated",
		},
		{
			name:     "warning event",
			severity: types.EventWarning,
			reason:   "ReloadWarning",
			message:  "Some monitors failed to reload",
		},
		{
			name:     "error event",
			severity: types.EventError,
			reason:   "ReloadFailed",
			message:  "Configuration reload failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Emit reload event
			pd.emitReloadEvent(tt.severity, tt.reason, tt.message)

			// Wait briefly for event to be sent to statusChan
			select {
			case status := <-pd.statusChan:
				if status.Source != "config-reload" {
					t.Errorf("emitReloadEvent() source = %q, want %q", status.Source, "config-reload")
				}
				if len(status.Events) != 1 {
					t.Fatalf("emitReloadEvent() events count = %d, want 1", len(status.Events))
				}
				event := status.Events[0]
				if event.Severity != tt.severity {
					t.Errorf("emitReloadEvent() severity = %v, want %v", event.Severity, tt.severity)
				}
				if event.Reason != tt.reason {
					t.Errorf("emitReloadEvent() reason = %q, want %q", event.Reason, tt.reason)
				}
				if event.Message != tt.message {
					t.Errorf("emitReloadEvent() message = %q, want %q", event.Message, tt.message)
				}
			case <-time.After(100 * time.Millisecond):
				t.Error("emitReloadEvent() did not send event to statusChan")
			}
		})
	}
}

// TestApplyConfigReload tests the applyConfigReload function.
func TestApplyConfigReload(t *testing.T) {
	t.Run("reload with monitor changes", func(t *testing.T) {
		helper := NewTestHelper()
		config := helper.CreateTestConfig()
		config.Monitors = []types.MonitorConfig{
			helper.CreateTestMonitorConfig("monitor-1", "test"),
			helper.CreateTestMonitorConfig("monitor-2", "test"),
		}

		factory := NewMockMonitorFactory()
		pd, err := NewProblemDetector(
			config,
			[]types.Monitor{},
			[]types.Exporter{NewMockExporter("test-exporter")},
			"/tmp/test-config.yaml",
			factory,
		)
		if err != nil {
			t.Fatalf("Failed to create problem detector: %v", err)
		}

		// Start detector to create monitor handles
		err = pd.Start()
		if err != nil {
			t.Fatalf("Failed to start detector: %v", err)
		}
		defer pd.Stop()

		time.Sleep(100 * time.Millisecond) // Wait for monitors to start

		// Create new config with changes
		newConfig := helper.CreateTestConfig()
		newConfig.Monitors = []types.MonitorConfig{
			helper.CreateTestMonitorConfig("monitor-2", "test"), // Modified (different interval)
			helper.CreateTestMonitorConfig("monitor-3", "test"), // Added
		}
		// Set different interval for monitor-2 to make it a modification
		newConfig.Monitors[0].Interval = 60 * time.Second
		// monitor-1 is removed

		// Create config diff
		diff := &reload.ConfigDiff{
			MonitorsAdded: []types.MonitorConfig{
				helper.CreateTestMonitorConfig("monitor-3", "test"),
			},
			MonitorsRemoved: []types.MonitorConfig{
				helper.CreateTestMonitorConfig("monitor-1", "test"),
			},
			MonitorsModified: []reload.MonitorChange{
				{
					Old: helper.CreateTestMonitorConfig("monitor-2", "test"),
					New: func() types.MonitorConfig {
						cfg := helper.CreateTestMonitorConfig("monitor-2", "test")
						cfg.Interval = 60 * time.Second
						return cfg
					}(),
				},
			},
		}

		// Apply config reload
		ctx := context.Background()
		err = pd.applyConfigReload(ctx, newConfig, diff)

		if err != nil {
			t.Errorf("applyConfigReload() unexpected error = %v", err)
		}

		// Verify config was updated
		pd.mu.RLock()
		if pd.config != newConfig {
			t.Error("applyConfigReload() did not update config reference")
		}
		pd.mu.RUnlock()
	})

	t.Run("reload with no changes", func(t *testing.T) {
		helper := NewTestHelper()
		config := helper.CreateTestConfig()
		factory := NewMockMonitorFactory()

		pd, err := NewProblemDetector(
			config,
			[]types.Monitor{},
			[]types.Exporter{NewMockExporter("test-exporter")},
			"/tmp/test-config.yaml",
			factory,
		)
		if err != nil {
			t.Fatalf("Failed to create problem detector: %v", err)
		}

		// Create new config with no changes
		newConfig := helper.CreateTestConfig()
		diff := &reload.ConfigDiff{} // Empty diff

		// Apply config reload
		ctx := context.Background()
		err = pd.applyConfigReload(ctx, newConfig, diff)

		if err != nil {
			t.Errorf("applyConfigReload() unexpected error = %v", err)
		}
	})

	t.Run("reload with critical failures", func(t *testing.T) {
		helper := NewTestHelper()
		config := helper.CreateTestConfig()
		config.Monitors = []types.MonitorConfig{
			helper.CreateTestMonitorConfig("monitor-1", "test"),
		}

		// Factory that returns an error for new monitors
		factory := NewMockMonitorFactory().SetCreateFunc(func(config types.MonitorConfig) (types.Monitor, error) {
			if config.Name == "new-monitor" {
				return nil, fmt.Errorf("create failed")
			}
			return NewMockMonitor(config.Name), nil
		})

		pd, err := NewProblemDetector(
			config,
			[]types.Monitor{},
			[]types.Exporter{NewMockExporter("test-exporter")},
			"/tmp/test-config.yaml",
			factory,
		)
		if err != nil {
			t.Fatalf("Failed to create problem detector: %v", err)
		}

		err = pd.Start()
		if err != nil {
			t.Fatalf("Failed to start detector: %v", err)
		}
		defer pd.Stop()

		time.Sleep(100 * time.Millisecond) // Wait for monitors to start

		// Create new config that will cause failure
		newConfig := helper.CreateTestConfig()
		newConfig.Monitors = []types.MonitorConfig{
			helper.CreateTestMonitorConfig("new-monitor", "test"), // This will fail
		}

		diff := &reload.ConfigDiff{
			MonitorsAdded: []types.MonitorConfig{
				helper.CreateTestMonitorConfig("new-monitor", "test"),
			},
		}

		// Apply config reload - should fail
		ctx := context.Background()
		err = pd.applyConfigReload(ctx, newConfig, diff)

		if err == nil {
			t.Errorf("applyConfigReload() expected error due to critical failure")
		}

		// Verify original config is still in place
		pd.mu.RLock()
		if pd.config == newConfig {
			t.Error("applyConfigReload() updated config despite critical failure")
		}
		pd.mu.RUnlock()
	})
}

// TestConfigReloadCriticalErrorHandling tests the P1 fix for error aggregation
func TestConfigReloadCriticalErrorHandling(t *testing.T) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()
	config.Monitors = []types.MonitorConfig{
		helper.CreateTestMonitorConfig("existing-monitor", "test"),
	}

	// Factory that fails for certain monitors
	factory := NewMockMonitorFactory().SetCreateFunc(func(config types.MonitorConfig) (types.Monitor, error) {
		if config.Name == "failing-monitor" {
			return nil, fmt.Errorf("monitor creation failed")
		}
		return NewMockMonitor(config.Name), nil
	})

	pd, err := NewProblemDetector(
		config,
		[]types.Monitor{},
		[]types.Exporter{NewMockExporter("test-exporter")},
		"/tmp/test-config.yaml",
		factory,
	)
	if err != nil {
		t.Fatalf("Failed to create problem detector: %v", err)
	}

	err = pd.Start()
	if err != nil {
		t.Fatalf("Failed to start detector: %v", err)
	}
	defer pd.Stop()

	time.Sleep(100 * time.Millisecond)

	originalConfig := pd.config

	// Test 1: Monitor creation failure should be critical
	t.Run("monitor creation failure is critical", func(t *testing.T) {
		newConfig := helper.CreateTestConfig()
		newConfig.Monitors = []types.MonitorConfig{
			helper.CreateTestMonitorConfig("existing-monitor", "test"),
			helper.CreateTestMonitorConfig("failing-monitor", "test"),
		}

		diff := &reload.ConfigDiff{
			MonitorsAdded: []types.MonitorConfig{
				helper.CreateTestMonitorConfig("failing-monitor", "test"),
			},
		}

		err = pd.applyConfigReload(context.Background(), newConfig, diff)

		if err == nil {
			t.Errorf("Expected critical error due to monitor creation failure")
		}

		// Config should NOT be updated due to critical error
		pd.mu.RLock()
		if pd.config != originalConfig {
			t.Error("Config was updated despite critical error")
		}
		pd.mu.RUnlock()
	})

	// Test 2: Non-critical errors should allow partial success
	t.Run("non-critical errors allow partial success", func(t *testing.T) {
		newConfig := helper.CreateTestConfig()
		newConfig.Monitors = []types.MonitorConfig{
			helper.CreateTestMonitorConfig("existing-monitor", "test"),
			helper.CreateTestMonitorConfig("good-monitor", "test"),
		}

		// Remove a non-existent monitor (non-critical error)
		diff := &reload.ConfigDiff{
			MonitorsRemoved: []types.MonitorConfig{
				helper.CreateTestMonitorConfig("non-existent-monitor", "test"),
			},
			MonitorsAdded: []types.MonitorConfig{
				helper.CreateTestMonitorConfig("good-monitor", "test"),
			},
		}

		err = pd.applyConfigReload(context.Background(), newConfig, diff)

		if err != nil {
			t.Errorf("Expected success with warnings, got error: %v", err)
		}

		// Config SHOULD be updated since only non-critical errors occurred
		pd.mu.RLock()
		if pd.config == originalConfig {
			t.Error("Config was not updated despite successful reload")
		}
		pd.mu.RUnlock()
	})
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}