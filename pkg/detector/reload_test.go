package detector

import (
	"context"
	"fmt"
	"slices"
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
					nil,
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
					nil,
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
			nil,
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
			nil,
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
		nil,
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

// TestApplyConfigReload_DependsOnSemantics verifies that blocked-state semantics are
// preserved across a hot-reload that adds monitors with DependsOn declarations:
//  1. Newly-added monitors are sorted topologically (dependency starts first).
//  2. pd.dependents is updated so the reverse-lookup map reflects new monitors.
//  3. A dependency cycle among newly-added monitors is rejected as a critical error.
func TestApplyConfigReload_DependsOnSemantics(t *testing.T) {
	t.Run("dependents map updated for newly-added monitor with DependsOn", func(t *testing.T) {
		helper := NewTestHelper()
		config := helper.CreateTestConfig()
		config.Monitors = []types.MonitorConfig{
			helper.CreateTestMonitorConfig("dep-monitor", "test"),
		}

		factory := NewMockMonitorFactory()
		pd, err := NewProblemDetector(
			config,
			[]types.Monitor{},
			[]types.Exporter{NewMockExporter("test-exporter")},
			"/tmp/test-config.yaml",
			factory,
			nil,
		)
		if err != nil {
			t.Fatalf("NewProblemDetector() error = %v", err)
		}
		if err := pd.Start(); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		defer pd.Stop()
		time.Sleep(50 * time.Millisecond)

		// Hot-reload adds dep-child which declares DependsOn: [dep-monitor].
		depChild := types.MonitorConfig{
			Name:      "dep-child",
			Type:      "test",
			Enabled:   true,
			Interval:  30 * time.Second,
			Timeout:   10 * time.Second,
			DependsOn: []string{"dep-monitor"},
		}
		newConfig := helper.CreateTestConfig()
		newConfig.Monitors = append(newConfig.Monitors, depChild)

		diff := &reload.ConfigDiff{
			MonitorsAdded: []types.MonitorConfig{depChild},
		}

		if err := pd.applyConfigReload(context.Background(), newConfig, diff); err != nil {
			t.Fatalf("applyConfigReload() unexpected error = %v", err)
		}

		// pd.dependents["dep-monitor"] must include "dep-child".
		dependents := pd.dependents["dep-monitor"]
		if !slices.Contains(dependents, "dep-child") {
			t.Errorf("pd.dependents[%q] = %v, want it to contain %q", "dep-monitor", dependents, "dep-child")
		}
	})

	t.Run("topological order preserved among newly-added monitors with inter-dependencies", func(t *testing.T) {
		helper := NewTestHelper()
		config := helper.CreateTestConfig()
		config.Monitors = []types.MonitorConfig{}

		var startOrder []string
		factory := NewMockMonitorFactory().SetCreateFunc(func(mc types.MonitorConfig) (types.Monitor, error) {
			startOrder = append(startOrder, mc.Name)
			return NewMockMonitor(mc.Name), nil
		})

		pd, err := NewProblemDetector(
			config,
			[]types.Monitor{},
			[]types.Exporter{NewMockExporter("test-exporter")},
			"/tmp/test-config.yaml",
			factory,
			nil,
		)
		if err != nil {
			t.Fatalf("NewProblemDetector() error = %v", err)
		}

		// Add parent and child together; child depends on parent.
		parent := types.MonitorConfig{
			Name: "parent", Type: "test", Enabled: true,
			Interval: 30 * time.Second, Timeout: 10 * time.Second,
		}
		child := types.MonitorConfig{
			Name: "child", Type: "test", Enabled: true,
			Interval: 30 * time.Second, Timeout: 10 * time.Second,
			DependsOn: []string{"parent"},
		}

		// Deliberately pass child before parent to confirm sort fixes the order.
		diff := &reload.ConfigDiff{
			MonitorsAdded: []types.MonitorConfig{child, parent},
		}
		newConfig := helper.CreateTestConfig()
		newConfig.Monitors = []types.MonitorConfig{parent, child}

		if err := pd.applyConfigReload(context.Background(), newConfig, diff); err != nil {
			t.Fatalf("applyConfigReload() unexpected error = %v", err)
		}

		if len(startOrder) != 2 {
			t.Fatalf("expected 2 monitors started, got %d (%v)", len(startOrder), startOrder)
		}
		if startOrder[0] != "parent" || startOrder[1] != "child" {
			t.Errorf("start order = %v, want [parent child]", startOrder)
		}
	})

	t.Run("dependency cycle among newly-added monitors returns critical error", func(t *testing.T) {
		helper := NewTestHelper()
		config := helper.CreateTestConfig()
		config.Monitors = []types.MonitorConfig{}

		factory := NewMockMonitorFactory()
		pd, err := NewProblemDetector(
			config,
			[]types.Monitor{},
			[]types.Exporter{NewMockExporter("test-exporter")},
			"/tmp/test-config.yaml",
			factory,
			nil,
		)
		if err != nil {
			t.Fatalf("NewProblemDetector() error = %v", err)
		}

		// a depends on b, b depends on a — cycle.
		a := types.MonitorConfig{
			Name: "cycle-a", Type: "test", Enabled: true,
			Interval: 30 * time.Second, Timeout: 10 * time.Second,
			DependsOn: []string{"cycle-b"},
		}
		b := types.MonitorConfig{
			Name: "cycle-b", Type: "test", Enabled: true,
			Interval: 30 * time.Second, Timeout: 10 * time.Second,
			DependsOn: []string{"cycle-a"},
		}

		diff := &reload.ConfigDiff{
			MonitorsAdded: []types.MonitorConfig{a, b},
		}
		newConfig := helper.CreateTestConfig()
		newConfig.Monitors = []types.MonitorConfig{a, b}

		err = pd.applyConfigReload(context.Background(), newConfig, diff)
		if err == nil {
			t.Error("applyConfigReload() expected critical error for dependency cycle, got nil")
		}
	})

	t.Run("dependents map updated for modified monitor DependsOn change", func(t *testing.T) {
		helper := NewTestHelper()
		config := helper.CreateTestConfig()
		// Start with "child" depending on "dep-a".
		depA := helper.CreateTestMonitorConfig("dep-a", "test")
		depB := helper.CreateTestMonitorConfig("dep-b", "test")
		child := types.MonitorConfig{
			Name:      "child",
			Type:      "test",
			Enabled:   true,
			Interval:  30 * time.Second,
			Timeout:   10 * time.Second,
			DependsOn: []string{"dep-a"},
		}
		config.Monitors = []types.MonitorConfig{depA, depB, child}

		factory := NewMockMonitorFactory()
		pd, err := NewProblemDetector(
			config,
			[]types.Monitor{},
			[]types.Exporter{NewMockExporter("test-exporter")},
			"/tmp/test-config.yaml",
			factory,
			nil,
		)
		if err != nil {
			t.Fatalf("NewProblemDetector() error = %v", err)
		}
		if err := pd.Start(); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		defer pd.Stop()
		time.Sleep(50 * time.Millisecond)

		// Sanity: dep-a's dependents should include child before reload.
		if !slices.Contains(pd.dependents["dep-a"], "child") {
			t.Fatalf("pre-condition: pd.dependents[dep-a] = %v, want child", pd.dependents["dep-a"])
		}

		// Hot-reload changes child's DependsOn from [dep-a] to [dep-b].
		modifiedChild := types.MonitorConfig{
			Name:      "child",
			Type:      "test",
			Enabled:   true,
			Interval:  30 * time.Second,
			Timeout:   10 * time.Second,
			DependsOn: []string{"dep-b"},
		}
		newConfig := helper.CreateTestConfig()
		newConfig.Monitors = []types.MonitorConfig{depA, depB, modifiedChild}

		diff := &reload.ConfigDiff{
			MonitorsModified: []reload.MonitorChange{
				{Old: child, New: modifiedChild},
			},
		}

		if err := pd.applyConfigReload(context.Background(), newConfig, diff); err != nil {
			t.Fatalf("applyConfigReload() unexpected error = %v", err)
		}

		// dep-a must no longer list child as a dependent.
		if slices.Contains(pd.dependents["dep-a"], "child") {
			t.Errorf("pd.dependents[dep-a] = %v, want child removed after DependsOn change", pd.dependents["dep-a"])
		}
		// dep-b must now list child as a dependent.
		if !slices.Contains(pd.dependents["dep-b"], "child") {
			t.Errorf("pd.dependents[dep-b] = %v, want child added after DependsOn change", pd.dependents["dep-b"])
		}
	})

	t.Run("dependents map cleaned up for removed monitor with DependsOn", func(t *testing.T) {
		helper := NewTestHelper()
		config := helper.CreateTestConfig()
		// Start with "child" depending on "dep-monitor".
		dep := helper.CreateTestMonitorConfig("dep-monitor", "test")
		child := types.MonitorConfig{
			Name:      "child",
			Type:      "test",
			Enabled:   true,
			Interval:  30 * time.Second,
			Timeout:   10 * time.Second,
			DependsOn: []string{"dep-monitor"},
		}
		config.Monitors = []types.MonitorConfig{dep, child}

		factory := NewMockMonitorFactory()
		pd, err := NewProblemDetector(
			config,
			[]types.Monitor{},
			[]types.Exporter{NewMockExporter("test-exporter")},
			"/tmp/test-config.yaml",
			factory,
			nil,
		)
		if err != nil {
			t.Fatalf("NewProblemDetector() error = %v", err)
		}
		if err := pd.Start(); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		defer pd.Stop()
		time.Sleep(50 * time.Millisecond)

		// Pre-condition: dep-monitor's dependents must contain child.
		if !slices.Contains(pd.dependents["dep-monitor"], "child") {
			t.Fatalf("pre-condition: pd.dependents[dep-monitor] = %v, want child", pd.dependents["dep-monitor"])
		}

		// Hot-reload removes child entirely.
		newConfig := helper.CreateTestConfig()
		newConfig.Monitors = []types.MonitorConfig{dep}

		diff := &reload.ConfigDiff{
			MonitorsRemoved: []types.MonitorConfig{child},
		}

		if err := pd.applyConfigReload(context.Background(), newConfig, diff); err != nil {
			t.Fatalf("applyConfigReload() unexpected error = %v", err)
		}

		// dep-monitor must no longer list child as a dependent.
		if slices.Contains(pd.dependents["dep-monitor"], "child") {
			t.Errorf("pd.dependents[dep-monitor] = %v, want child removed after monitor removal", pd.dependents["dep-monitor"])
		}
	})

	t.Run("dependents map cleaned when two monitors sharing a dependency are removed", func(t *testing.T) {
		helper := NewTestHelper()
		config := helper.CreateTestConfig()
		dep := helper.CreateTestMonitorConfig("dep-monitor", "test")
		child1 := types.MonitorConfig{
			Name:      "child-1",
			Type:      "test",
			Enabled:   true,
			Interval:  30 * time.Second,
			Timeout:   10 * time.Second,
			DependsOn: []string{"dep-monitor"},
		}
		child2 := types.MonitorConfig{
			Name:      "child-2",
			Type:      "test",
			Enabled:   true,
			Interval:  30 * time.Second,
			Timeout:   10 * time.Second,
			DependsOn: []string{"dep-monitor"},
		}
		config.Monitors = []types.MonitorConfig{dep, child1, child2}

		factory := NewMockMonitorFactory()
		pd, err := NewProblemDetector(
			config,
			[]types.Monitor{},
			[]types.Exporter{NewMockExporter("test-exporter")},
			"/tmp/test-config.yaml",
			factory,
			nil,
		)
		if err != nil {
			t.Fatalf("NewProblemDetector() error = %v", err)
		}
		if err := pd.Start(); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		defer pd.Stop()
		time.Sleep(50 * time.Millisecond)

		// Pre-condition: dep-monitor's dependents must contain both children.
		if !slices.Contains(pd.dependents["dep-monitor"], "child-1") {
			t.Fatalf("pre-condition: pd.dependents[dep-monitor] = %v, want child-1", pd.dependents["dep-monitor"])
		}
		if !slices.Contains(pd.dependents["dep-monitor"], "child-2") {
			t.Fatalf("pre-condition: pd.dependents[dep-monitor] = %v, want child-2", pd.dependents["dep-monitor"])
		}

		// Hot-reload removes both children in a single diff. The filter loop in
		// applyConfigReload runs twice over pd.dependents["dep-monitor"] (once per
		// removed monitor), each time producing a fresh slice — both must be purged.
		newConfig := helper.CreateTestConfig()
		newConfig.Monitors = []types.MonitorConfig{dep}

		diff := &reload.ConfigDiff{
			MonitorsRemoved: []types.MonitorConfig{child1, child2},
		}

		if err := pd.applyConfigReload(context.Background(), newConfig, diff); err != nil {
			t.Fatalf("applyConfigReload() unexpected error = %v", err)
		}

		if slices.Contains(pd.dependents["dep-monitor"], "child-1") {
			t.Errorf("pd.dependents[dep-monitor] = %v, want child-1 removed", pd.dependents["dep-monitor"])
		}
		if slices.Contains(pd.dependents["dep-monitor"], "child-2") {
			t.Errorf("pd.dependents[dep-monitor] = %v, want child-2 removed", pd.dependents["dep-monitor"])
		}
	})


	t.Run("dependents map preserves surviving child after partial removal", func(t *testing.T) {
		helper := NewTestHelper()
		config := helper.CreateTestConfig()
		dep := helper.CreateTestMonitorConfig("dep-monitor", "test")
		child1 := types.MonitorConfig{
			Name:      "child-1",
			Type:      "test",
			Enabled:   true,
			Interval:  30 * time.Second,
			Timeout:   10 * time.Second,
			DependsOn: []string{"dep-monitor"},
		}
		child2 := types.MonitorConfig{
			Name:      "child-2",
			Type:      "test",
			Enabled:   true,
			Interval:  30 * time.Second,
			Timeout:   10 * time.Second,
			DependsOn: []string{"dep-monitor"},
		}
		config.Monitors = []types.MonitorConfig{dep, child1, child2}

		factory := NewMockMonitorFactory()
		pd, err := NewProblemDetector(
			config,
			[]types.Monitor{},
			[]types.Exporter{NewMockExporter("test-exporter")},
			"/tmp/test-config.yaml",
			factory,
			nil,
		)
		if err != nil {
			t.Fatalf("NewProblemDetector() error = %v", err)
		}
		if err := pd.Start(); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		defer pd.Stop()
		time.Sleep(50 * time.Millisecond)

		// Pre-condition: dep-monitor's dependents must contain both children.
		if !slices.Contains(pd.dependents["dep-monitor"], "child-1") {
			t.Fatalf("pre-condition: pd.dependents[dep-monitor] = %v, want child-1", pd.dependents["dep-monitor"])
		}
		if !slices.Contains(pd.dependents["dep-monitor"], "child-2") {
			t.Fatalf("pre-condition: pd.dependents[dep-monitor] = %v, want child-2", pd.dependents["dep-monitor"])
		}

		// Hot-reload removes only child-2; child-1 stays in the new config.
		newConfig := helper.CreateTestConfig()
		newConfig.Monitors = []types.MonitorConfig{dep, child1}

		diff := &reload.ConfigDiff{
			MonitorsRemoved: []types.MonitorConfig{child2},
		}

		if err := pd.applyConfigReload(context.Background(), newConfig, diff); err != nil {
			t.Fatalf("applyConfigReload() unexpected error = %v", err)
		}

		// child-2 must be evicted from dep-monitor's dependents.
		if slices.Contains(pd.dependents["dep-monitor"], "child-2") {
			t.Errorf("pd.dependents[dep-monitor] = %v, want child-2 removed after partial removal", pd.dependents["dep-monitor"])
		}
		// child-1 must survive — partial removal must not evict remaining dependents.
		if !slices.Contains(pd.dependents["dep-monitor"], "child-1") {
			t.Errorf("pd.dependents[dep-monitor] = %v, want child-1 preserved after partial removal", pd.dependents["dep-monitor"])
		}
	})
	t.Run("dependents map cleaned when stopMonitorByName fails for removed monitor", func(t *testing.T) {
		helper := NewTestHelper()
		config := helper.CreateTestConfig()
		// Start with only dep-monitor running; "child" is intentionally absent
		// from the handles list so stopMonitorByName("child") will return an error.
		dep := helper.CreateTestMonitorConfig("dep-monitor", "test")
		config.Monitors = []types.MonitorConfig{dep}

		factory := NewMockMonitorFactory()
		pd, err := NewProblemDetector(
			config,
			[]types.Monitor{},
			[]types.Exporter{NewMockExporter("test-exporter")},
			"/tmp/test-config.yaml",
			factory,
			nil,
		)
		if err != nil {
			t.Fatalf("NewProblemDetector() error = %v", err)
		}
		if err := pd.Start(); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		defer pd.Stop()
		time.Sleep(50 * time.Millisecond)

		// Pre-condition: dep-monitor has no dependents yet (child was never started).
		if slices.Contains(pd.dependents["dep-monitor"], "child") {
			t.Fatalf("pre-condition: pd.dependents[dep-monitor] already contains child — unexpected state")
		}

		// Inject "child" into pd.dependents as if it had declared DependsOn:
		// ["dep-monitor"] when it was originally started.  Because we never
		// added a MonitorHandle for "child", stopMonitorByName("child") will
		// return "monitor child not found" — simulating a failed stop.
		pd.dependents["dep-monitor"] = []string{"child"}

		child := types.MonitorConfig{
			Name:      "child",
			Type:      "test",
			Enabled:   true,
			Interval:  30 * time.Second,
			Timeout:   10 * time.Second,
			DependsOn: []string{"dep-monitor"},
		}

		newConfig := helper.CreateTestConfig()
		newConfig.Monitors = []types.MonitorConfig{dep}

		diff := &reload.ConfigDiff{
			MonitorsRemoved: []types.MonitorConfig{child},
		}

		// stopMonitorByName errors for removed monitors are non-critical (only
		// logged); applyConfigReload must still succeed overall.
		if err := pd.applyConfigReload(context.Background(), newConfig, diff); err != nil {
			t.Fatalf("applyConfigReload() unexpected error = %v", err)
		}

		// pd.dependents must be cleaned up even though stopMonitorByName failed.
		if slices.Contains(pd.dependents["dep-monitor"], "child") {
			t.Errorf("pd.dependents[dep-monitor] = %v, want child removed despite stopMonitorByName failure", pd.dependents["dep-monitor"])
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
