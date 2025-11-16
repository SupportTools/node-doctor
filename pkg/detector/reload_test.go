package detector

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/reload"
	"github.com/supporttools/node-doctor/pkg/types"
)

// TestInitializeReload tests the initializeReload function.
func TestInitializeReload(t *testing.T) {
	t.Run("successful initialization", func(t *testing.T) {
		// Create temp config file
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")
		if err := os.WriteFile(configPath, []byte("apiVersion: v1\nkind: NodeDoctorConfig\n"), 0644); err != nil {
			t.Fatalf("Failed to create config file: %v", err)
		}

		// Create test config
		helper := NewTestHelper()
		config := helper.CreateTestConfig()
		config.Reload = types.ReloadConfig{
			Enabled:          true,
			DebounceInterval: 1 * time.Second,
		}

		// Create problem detector
		pd, err := NewProblemDetector(
			config,
			[]types.Monitor{NewMockMonitor("test-monitor")},
			[]types.Exporter{NewMockExporter("test-exporter")},
			configPath,
			nil,
		)
		if err != nil {
			t.Fatalf("Failed to create problem detector: %v", err)
		}

		// Test initializeReload
		err = pd.initializeReload()
		if err != nil {
			t.Errorf("initializeReload() unexpected error = %v", err)
		}

		// Verify components were initialized
		if pd.configWatcher == nil {
			t.Error("initializeReload() configWatcher is nil")
		}
		if pd.reloadCoordinator == nil {
			t.Error("initializeReload() reloadCoordinator is nil")
		}
	})
}

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
					{Name: "test-monitor", Type: "test", Enabled: true},
				}

				return NewProblemDetector(
					config,
					[]types.Monitor{NewMockMonitor("test-monitor")},
					[]types.Exporter{NewMockExporter("test-exporter")},
					"",
					nil,
				)
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
					{Name: "test-monitor", Type: "test", Enabled: true},
				}

				return NewProblemDetector(
					config,
					[]types.Monitor{NewMockMonitor("test-monitor")},
					[]types.Exporter{NewMockExporter("test-exporter")},
					"",
					nil,
				)
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

// TestReplaceMonitor tests the replaceMonitor function.
func TestReplaceMonitor(t *testing.T) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()
	config.Monitors = []types.MonitorConfig{
		{Name: "monitor-1", Type: "test", Enabled: true},
		{Name: "monitor-2", Type: "test", Enabled: true},
	}

	oldMonitor1 := NewMockMonitor("monitor-1")
	oldMonitor2 := NewMockMonitor("monitor-2")

	pd, err := NewProblemDetector(
		config,
		[]types.Monitor{oldMonitor1, oldMonitor2},
		[]types.Exporter{NewMockExporter("test-exporter")},
		"",
		nil,
	)
	if err != nil {
		t.Fatalf("Failed to create problem detector: %v", err)
	}

	// Create new monitor to replace the first one
	newMonitor := NewMockMonitor("monitor-1-replaced")

	// Replace monitor-1
	pd.replaceMonitor("monitor-1", newMonitor)

	// Verify replacement
	if pd.monitors[0] != newMonitor {
		t.Error("replaceMonitor() did not replace monitor at index 0")
	}
	if pd.monitors[1] != oldMonitor2 {
		t.Error("replaceMonitor() incorrectly modified monitor at index 1")
	}

	// Test replacing non-existent monitor (should be no-op)
	pd.replaceMonitor("nonexistent", NewMockMonitor("new"))
	// No error, just silently does nothing
}

// TestGetMonitorIndex tests the getMonitorIndex function.
func TestGetMonitorIndex(t *testing.T) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()
	config.Monitors = []types.MonitorConfig{
		{Name: "monitor-1", Type: "test", Enabled: true},
		{Name: "monitor-2", Type: "test", Enabled: true},
		{Name: "monitor-3", Type: "test", Enabled: true},
	}

	pd, err := NewProblemDetector(
		config,
		[]types.Monitor{
			NewMockMonitor("monitor-1"),
			NewMockMonitor("monitor-2"),
			NewMockMonitor("monitor-3"),
		},
		[]types.Exporter{NewMockExporter("test-exporter")},
		"",
		nil,
	)
	if err != nil {
		t.Fatalf("Failed to create problem detector: %v", err)
	}

	tests := []struct {
		name     string
		monitor  string
		expected int
	}{
		{"first monitor", "monitor-1", 0},
		{"second monitor", "monitor-2", 1},
		{"third monitor", "monitor-3", 2},
		{"non-existent monitor", "nonexistent", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pd.getMonitorIndex(tt.monitor)
			if got != tt.expected {
				t.Errorf("getMonitorIndex(%q) = %d, want %d", tt.monitor, got, tt.expected)
			}
		})
	}
}

// TestEmitReloadEvent tests the emitReloadEvent function.
func TestEmitReloadEvent(t *testing.T) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()

	pd, err := NewProblemDetector(
		config,
		[]types.Monitor{NewMockMonitor("test-monitor")},
		[]types.Exporter{NewMockExporter("test-exporter")},
		"",
		nil,
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

// TestCleanupOnce tests the cleanupOnce function.
func TestCleanupOnce(t *testing.T) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()

	pd, err := NewProblemDetector(
		config,
		[]types.Monitor{NewMockMonitor("test-monitor")},
		[]types.Exporter{NewMockExporter("test-exporter")},
		"",
		nil,
	)
	if err != nil {
		t.Fatalf("Failed to create problem detector: %v", err)
	}

	// Set a short problem TTL for testing
	pd.problemTTL = 50 * time.Millisecond

	// Add some problems
	now := time.Now()
	pd.problems["fresh-problem"] = &problemEntry{
		problem: &types.Problem{
			Type:     "test-problem",
			Resource: "fresh-resource",
			Severity: types.ProblemCritical,
			Message:  "Fresh problem",
		},
		lastSeen: now, // Fresh problem
	}
	pd.problems["old-problem"] = &problemEntry{
		problem: &types.Problem{
			Type:     "test-problem",
			Resource: "old-resource",
			Severity: types.ProblemCritical,
			Message:  "Old problem",
		},
		lastSeen: now.Add(-100 * time.Millisecond), // Expired problem
	}
	pd.problems["another-old-problem"] = &problemEntry{
		problem: &types.Problem{
			Type:     "test-problem",
			Resource: "another-old-resource",
			Severity: types.ProblemCritical,
			Message:  "Another old problem",
		},
		lastSeen: now.Add(-200 * time.Millisecond), // Very expired problem
	}

	// Verify initial state
	if len(pd.problems) != 3 {
		t.Fatalf("Initial problems count = %d, want 3", len(pd.problems))
	}

	// Run cleanup
	pd.cleanupOnce()

	// Verify cleanup results
	pd.problemsMu.Lock()
	defer pd.problemsMu.Unlock()

	if len(pd.problems) != 1 {
		t.Errorf("After cleanup, problems count = %d, want 1", len(pd.problems))
	}

	if _, exists := pd.problems["fresh-problem"]; !exists {
		t.Error("cleanupOnce() removed fresh problem")
	}
	if _, exists := pd.problems["old-problem"]; exists {
		t.Error("cleanupOnce() did not remove old problem")
	}
	if _, exists := pd.problems["another-old-problem"]; exists {
		t.Error("cleanupOnce() did not remove another old problem")
	}
}

// TestApplyConfigReload tests the applyConfigReload function.
func TestApplyConfigReload(t *testing.T) {
	t.Run("reload with monitor changes", func(t *testing.T) {
		helper := NewTestHelper()
		config := helper.CreateTestConfig()
		config.Monitors = []types.MonitorConfig{
			{Name: "monitor-1", Type: "test", Enabled: true, Interval: 30 * time.Second},
			{Name: "monitor-2", Type: "test", Enabled: true, Interval: 30 * time.Second},
		}

		pd, err := NewProblemDetector(
			config,
			[]types.Monitor{
				NewMockMonitor("monitor-1"),
				NewMockMonitor("monitor-2"),
			},
			[]types.Exporter{NewMockExporter("test-exporter")},
			"",
			nil,
		)
		if err != nil {
			t.Fatalf("Failed to create problem detector: %v", err)
		}

		// Create new config with changes
		newConfig := helper.CreateTestConfig()
		newConfig.Monitors = []types.MonitorConfig{
			{Name: "monitor-2", Type: "test", Enabled: true, Interval: 60 * time.Second}, // Modified
			{Name: "monitor-3", Type: "test", Enabled: true, Interval: 30 * time.Second}, // Added
		}
		// monitor-1 is removed

		// Create config diff
		diff := &reload.ConfigDiff{
			MonitorsAdded: []types.MonitorConfig{
				{Name: "monitor-3", Type: "test", Enabled: true, Interval: 30 * time.Second},
			},
			MonitorsRemoved: []types.MonitorConfig{
				{Name: "monitor-1", Type: "test", Enabled: true, Interval: 30 * time.Second},
			},
			MonitorsModified: []reload.MonitorChange{
				{
					Old: types.MonitorConfig{Name: "monitor-2", Type: "test", Enabled: true, Interval: 30 * time.Second},
					New: types.MonitorConfig{Name: "monitor-2", Type: "test", Enabled: true, Interval: 60 * time.Second},
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

		pd, err := NewProblemDetector(
			config,
			[]types.Monitor{NewMockMonitor("test-monitor")},
			[]types.Exporter{NewMockExporter("test-exporter")},
			"",
			nil,
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
