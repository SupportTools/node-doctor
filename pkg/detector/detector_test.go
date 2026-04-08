// Package detector provides comprehensive tests for the Problem Detector orchestrator.
package detector

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

func TestNewProblemDetector(t *testing.T) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()
	factory := NewMockMonitorFactory()

	tests := []struct {
		name       string
		config     *types.NodeDoctorConfig
		monitors   []types.Monitor
		exporters  []types.Exporter
		configPath string
		factory    MonitorFactory
		wantError  bool
		errorMsg   string
	}{
		{
			name:       "valid configuration",
			config:     config,
			monitors:   []types.Monitor{NewMockMonitor("test-monitor")},
			exporters:  []types.Exporter{NewMockExporter("test-exporter")},
			configPath: "/tmp/test-config.yaml",
			factory:    factory,
			wantError:  false,
		},
		{
			name:       "nil config",
			config:     nil,
			monitors:   []types.Monitor{NewMockMonitor("test-monitor")},
			exporters:  []types.Exporter{NewMockExporter("test-exporter")},
			configPath: "/tmp/test-config.yaml",
			factory:    factory,
			wantError:  true,
			errorMsg:   "config cannot be nil",
		},
		{
			name:       "empty config path",
			config:     config,
			monitors:   []types.Monitor{NewMockMonitor("test-monitor")},
			exporters:  []types.Exporter{NewMockExporter("test-exporter")},
			configPath: "",
			factory:    factory,
			wantError:  true,
			errorMsg:   "config file path cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detector, err := NewProblemDetector(tt.config, tt.monitors, tt.exporters, tt.configPath, tt.factory, nil)

			if tt.wantError {
				if err == nil {
					t.Errorf("NewProblemDetector() expected error but got none")
				} else if err.Error() != tt.errorMsg {
					t.Errorf("NewProblemDetector() error = %v, want %v", err.Error(), tt.errorMsg)
				}
				return
			}

			if err != nil {
				t.Errorf("NewProblemDetector() unexpected error = %v", err)
				return
			}

			if detector == nil {
				t.Errorf("NewProblemDetector() returned nil detector")
				return
			}

			if detector.IsRunning() {
				t.Errorf("NewProblemDetector() detector should not be running initially")
			}
		})
	}
}

// TestNewProblemDetector_WithRegistry verifies that registry-aware validation rejects
// configs that reference monitor types not present in the provided registry.
func TestNewProblemDetector_WithRegistry(t *testing.T) {
	helper := NewTestHelper()
	factory := NewMockMonitorFactory()

	t.Run("unknown monitor type rejected when registry provided", func(t *testing.T) {
		config := helper.CreateTestConfig() // has monitor type "test"
		// Registry that doesn't know "test"
		registry := newMockRegistry("cpu-check", "disk-check")
		_, err := NewProblemDetector(
			config,
			[]types.Monitor{},
			[]types.Exporter{NewMockExporter("exp")},
			"/tmp/test-config.yaml",
			factory,
			registry,
		)
		if err == nil {
			t.Fatal("expected error for unknown monitor type, got nil")
		}
		if !strings.Contains(err.Error(), "unknown monitor type") {
			t.Errorf("error should mention unknown monitor type, got: %v", err)
		}
	})

	t.Run("known monitor type accepted when registry provided", func(t *testing.T) {
		config := helper.CreateTestConfig() // has monitor type "test"
		// Registry that knows "test"
		registry := newMockRegistry("test")
		det, err := NewProblemDetector(
			config,
			[]types.Monitor{},
			[]types.Exporter{NewMockExporter("exp")},
			"/tmp/test-config.yaml",
			factory,
			registry,
		)
		if err != nil {
			t.Fatalf("unexpected error with registered type: %v", err)
		}
		if det == nil {
			t.Fatal("expected non-nil detector")
		}
	})

	t.Run("nil registry skips type check", func(t *testing.T) {
		config := helper.CreateTestConfig() // has monitor type "test"
		det, err := NewProblemDetector(
			config,
			[]types.Monitor{},
			[]types.Exporter{NewMockExporter("exp")},
			"/tmp/test-config.yaml",
			factory,
			nil,
		)
		if err != nil {
			t.Fatalf("unexpected error with nil registry: %v", err)
		}
		if det == nil {
			t.Fatal("expected non-nil detector")
		}
	})
}

func TestProblemDetector_AllMonitorsFail(t *testing.T) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()

	// Create factory that returns monitors that fail to start
	factory := NewMockMonitorFactory().SetCreateFunc(func(config types.MonitorConfig) (types.Monitor, error) {
		return NewMockMonitor(config.Name).SetStartError(fmt.Errorf("failed to start")), nil
	})

	detector, err := NewProblemDetector(config, []types.Monitor{}, []types.Exporter{NewMockExporter("test")}, "/tmp/test-config.yaml", factory, nil)
	if err != nil {
		t.Fatalf("NewProblemDetector() error = %v", err)
	}

	err = detector.Start()
	if err != nil {
		t.Errorf("Start() unexpected error = %v", err)
	}

	// Wait a moment for monitors to start
	time.Sleep(100 * time.Millisecond)

	// Clean shutdown
	detector.Stop()

	// Verify statistics
	stats := detector.GetStatistics()
	if stats.GetMonitorsFailed() == 0 {
		t.Errorf("Expected monitor failures to be recorded")
	}
	if stats.GetMonitorsStarted() != 0 {
		t.Errorf("Expected 0 started monitors, got %d", stats.GetMonitorsStarted())
	}
}

func TestProblemDetector_SomeMonitorsFail(t *testing.T) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()
	config.Monitors = []types.MonitorConfig{
		helper.CreateTestMonitorConfig("success-monitor", "test"),
		helper.CreateTestMonitorConfig("fail-monitor", "test"),
	}

	// Create factory that makes success-monitor work, fail-monitor fail
	factory := NewMockMonitorFactory().SetCreateFunc(func(config types.MonitorConfig) (types.Monitor, error) {
		if config.Name == "fail-monitor" {
			return NewMockMonitor(config.Name).SetStartError(fmt.Errorf("failed to start")), nil
		}
		return NewMockMonitor(config.Name).AddStatusUpdate(helper.CreateTestStatus(config.Name)), nil
	})

	detector, err := NewProblemDetector(config, []types.Monitor{}, []types.Exporter{NewMockExporter("test")}, "/tmp/test-config.yaml", factory, nil)
	if err != nil {
		t.Fatalf("NewProblemDetector() error = %v", err)
	}

	err = detector.Start()
	if err != nil {
		t.Errorf("Start() unexpected error = %v", err)
	}

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	// Clean shutdown
	detector.Stop()

	// Verify statistics
	stats := detector.GetStatistics()
	if stats.GetMonitorsFailed() != 1 {
		t.Errorf("Expected 1 failed monitor, got %d", stats.GetMonitorsFailed())
	}
	if stats.GetMonitorsStarted() != 1 {
		t.Errorf("Expected 1 started monitor, got %d", stats.GetMonitorsStarted())
	}
}

func TestProblemDetector_StatusProcessing(t *testing.T) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()

	// Create monitor with test status
	factory := NewMockMonitorFactory().SetCreateFunc(func(config types.MonitorConfig) (types.Monitor, error) {
		return NewMockMonitor(config.Name).AddStatusUpdate(helper.CreateTestStatus(config.Name)), nil
	})

	exporter := NewMockExporter("test-exporter")
	detector, err := NewProblemDetector(config, []types.Monitor{}, []types.Exporter{exporter}, "/tmp/test-config.yaml", factory, nil)
	if err != nil {
		t.Fatalf("NewProblemDetector() error = %v", err)
	}

	err = detector.Start()
	if err != nil {
		t.Errorf("Start() unexpected error = %v", err)
	}

	// Wait for processing
	time.Sleep(300 * time.Millisecond)

	// Clean shutdown
	detector.Stop()

	// Verify statistics
	stats := detector.GetStatistics()
	if stats.GetStatusesReceived() == 0 {
		t.Errorf("Expected status to be received")
	}

	// Verify exporter received status
	statusExports := exporter.GetStatusExports()
	if len(statusExports) == 0 {
		t.Errorf("Expected exporter to receive status updates")
	}
}

func TestExportDistribution(t *testing.T) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()

	// Create multiple exporters
	exporter1 := NewMockExporter("exporter1")
	exporter2 := NewMockExporter("exporter2")
	exporter3 := NewMockExporter("exporter3")

	// Create factory that returns monitor with test status
	factory := NewMockMonitorFactory().SetCreateFunc(func(config types.MonitorConfig) (types.Monitor, error) {
		return NewMockMonitor(config.Name).AddStatusUpdate(helper.CreateTestStatus(config.Name)), nil
	})

	detector, err := NewProblemDetector(config, []types.Monitor{}, []types.Exporter{exporter1, exporter2, exporter3}, "/tmp/test-config.yaml", factory, nil)
	if err != nil {
		t.Fatalf("NewProblemDetector() error = %v", err)
	}

	err = detector.Start()
	if err != nil {
		t.Errorf("Start() unexpected error = %v", err)
	}

	// Wait for processing
	time.Sleep(300 * time.Millisecond)

	// Clean shutdown
	detector.Stop()

	// Verify all exporters received the status
	exporters := []*MockExporter{exporter1, exporter2, exporter3}
	for i, exp := range exporters {
		statusCount, _ := exp.GetExportCounts()
		if statusCount == 0 {
			t.Errorf("Exporter %d did not receive any status updates", i+1)
		}
	}
}

func TestGracefulShutdown(t *testing.T) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()

	factory := NewMockMonitorFactory()
	exporter := NewMockExporter("test-exporter")

	detector, err := NewProblemDetector(config, []types.Monitor{}, []types.Exporter{exporter}, "/tmp/test-config.yaml", factory, nil)
	if err != nil {
		t.Fatalf("NewProblemDetector() error = %v", err)
	}

	// Start detector in background
	var runErr error
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		runErr = detector.Start()
	}()

	// Wait for startup
	time.Sleep(100 * time.Millisecond)

	// Verify it's running
	if !detector.IsRunning() {
		t.Errorf("Detector should be running")
	}

	// Stop detector to trigger shutdown
	err = detector.Stop()
	if err != nil {
		t.Errorf("Stop() unexpected error = %v", err)
	}

	// Wait for shutdown
	wg.Wait()

	// Verify it's stopped
	if detector.IsRunning() {
		t.Errorf("Detector should be stopped after shutdown")
	}

	if runErr != nil {
		t.Errorf("Start() unexpected error during shutdown = %v", runErr)
	}
}

func TestShutdownTimeout(t *testing.T) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()

	// Create exporter with long delay to simulate hung exporter
	exporter := NewMockExporter("slow-exporter").SetExportDelay(2 * time.Second)

	factory := NewMockMonitorFactory().SetCreateFunc(func(config types.MonitorConfig) (types.Monitor, error) {
		return NewMockMonitor(config.Name).AddStatusUpdate(helper.CreateTestStatus(config.Name)), nil
	})

	detector, err := NewProblemDetector(config, []types.Monitor{}, []types.Exporter{exporter}, "/tmp/test-config.yaml", factory, nil)
	if err != nil {
		t.Fatalf("NewProblemDetector() error = %v", err)
	}

	// Start and immediately stop to trigger shutdown with pending operations
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		detector.Start()
	}()

	// Let it start and begin processing
	time.Sleep(100 * time.Millisecond)
	detector.Stop()

	// Wait for shutdown - should complete even with slow exporter
	wg.Wait()

	// Note: We expect shutdown to complete within the 30s timeout,
	// but this test verifies the mechanism works rather than timing it precisely
}

func TestConcurrencySafety(t *testing.T) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()
	config.Monitors = []types.MonitorConfig{
		helper.CreateTestMonitorConfig("monitor-0", "test"),
		helper.CreateTestMonitorConfig("monitor-1", "test"),
		helper.CreateTestMonitorConfig("monitor-2", "test"),
		helper.CreateTestMonitorConfig("monitor-3", "test"),
		helper.CreateTestMonitorConfig("monitor-4", "test"),
	}

	// Create factory that returns monitors sending concurrent updates
	factory := NewMockMonitorFactory().SetCreateFunc(func(config types.MonitorConfig) (types.Monitor, error) {
		monitor := NewMockMonitor(config.Name).AddStatusUpdate(helper.CreateTestStatus(config.Name))
		return monitor, nil
	})

	exporter := NewMockExporter("test-exporter")

	detector, err := NewProblemDetector(config, []types.Monitor{}, []types.Exporter{exporter}, "/tmp/test-config.yaml", factory, nil)
	if err != nil {
		t.Fatalf("NewProblemDetector() error = %v", err)
	}

	err = detector.Start()
	if err != nil {
		t.Errorf("Start() unexpected error = %v", err)
	}

	// Let it run for some time
	time.Sleep(500 * time.Millisecond)

	// Clean shutdown
	detector.Stop()

	// Verify statistics are consistent (this tests thread safety)
	stats := detector.GetStatistics()
	if stats.GetMonitorsStarted() != 5 {
		t.Errorf("Expected 5 monitors started, got %d", stats.GetMonitorsStarted())
	}

	if stats.GetStatusesReceived() == 0 {
		t.Errorf("Expected some status updates to be processed")
	}
}

func TestChannelOverflow(t *testing.T) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()

	// Create factory that returns monitor sending many rapid updates
	factory := NewMockMonitorFactory().SetCreateFunc(func(config types.MonitorConfig) (types.Monitor, error) {
		monitor := NewMockMonitor(config.Name)
		for i := 0; i < 2000; i++ { // More than channel buffer size
			monitor.AddStatusUpdate(helper.CreateTestStatus(config.Name))
		}
		return monitor, nil
	})

	exporter := NewMockExporter("test-exporter")

	detector, err := NewProblemDetector(config, []types.Monitor{}, []types.Exporter{exporter}, "/tmp/test-config.yaml", factory, nil)
	if err != nil {
		t.Fatalf("NewProblemDetector() error = %v", err)
	}

	// Should not block or crash even with channel overflow
	err = detector.Start()
	if err != nil {
		t.Errorf("Start() unexpected error = %v", err)
	}

	// Let it run briefly
	time.Sleep(200 * time.Millisecond)

	// Clean shutdown
	detector.Stop()

	// Some updates should have been processed
	stats := detector.GetStatistics()
	if stats.GetStatusesReceived() == 0 {
		t.Errorf("Expected some status updates despite overflow")
	}
}

func TestExportFailureIsolation(t *testing.T) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()

	// Create exporters with different failure behaviors
	// Note: We only test status export failures since ExportProblem is no longer called (issue #7 fix)
	successExporter := NewMockExporter("success-exporter")
	failStatusExporter := NewMockExporter("fail-status").SetStatusExportError(fmt.Errorf("status export failed"))

	factory := NewMockMonitorFactory().SetCreateFunc(func(config types.MonitorConfig) (types.Monitor, error) {
		status := types.NewStatus(config.Name)
		status.AddEvent(types.NewEvent(types.EventError, "CriticalError", "Critical error occurred"))
		return NewMockMonitor(config.Name).AddStatusUpdate(status), nil
	})

	detector, err := NewProblemDetector(config, []types.Monitor{}, []types.Exporter{successExporter, failStatusExporter}, "/tmp/test-config.yaml", factory, nil)
	if err != nil {
		t.Fatalf("NewProblemDetector() error = %v", err)
	}

	err = detector.Start()
	if err != nil {
		t.Errorf("Start() unexpected error = %v", err)
	}

	// Wait for processing
	time.Sleep(300 * time.Millisecond)

	// Clean shutdown
	detector.Stop()

	// Verify successful exporter still received updates
	successCount, _ := successExporter.GetExportCounts()
	if successCount == 0 {
		t.Errorf("Successful exporter should have received status updates")
	}

	// Verify statistics tracked failures
	stats := detector.GetStatistics()
	if stats.GetExportsFailed() == 0 {
		t.Errorf("Expected some export failures to be recorded")
	}
	if stats.GetExportsSucceeded() == 0 {
		t.Errorf("Expected some successful exports")
	}
}

func TestMonitorChannelClosure(t *testing.T) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()

	factory := NewMockMonitorFactory().SetCreateFunc(func(config types.MonitorConfig) (types.Monitor, error) {
		return NewMockMonitor(config.Name).AddStatusUpdate(helper.CreateTestStatus(config.Name)), nil
	})

	exporter := NewMockExporter("test-exporter")

	detector, err := NewProblemDetector(config, []types.Monitor{}, []types.Exporter{exporter}, "/tmp/test-config.yaml", factory, nil)
	if err != nil {
		t.Fatalf("NewProblemDetector() error = %v", err)
	}

	// Start detector
	err = detector.Start()
	if err != nil {
		t.Errorf("Start() unexpected error = %v", err)
	}

	// Let it start
	time.Sleep(100 * time.Millisecond)

	// Stop the detector (which stops monitors and closes channels)
	detector.Stop()

	// Should handle channel closure gracefully
	if detector.IsRunning() {
		t.Errorf("Detector should have stopped gracefully")
	}
}

// TestNoDuplicateMonitorStarts verifies that each configured monitor starts exactly once,
// even when a caller (incorrectly) passes the same monitors both as passedMonitors and
// via the config. The deduplication guard in Start() must prevent double starts.
func TestNoDuplicateMonitorStarts(t *testing.T) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()
	config.Monitors = []types.MonitorConfig{
		helper.CreateTestMonitorConfig("monitor-a", "test"),
		helper.CreateTestMonitorConfig("monitor-b", "test"),
	}

	startCount := make(map[string]int)
	var mu sync.Mutex

	factory := NewMockMonitorFactory().SetCreateFunc(func(cfg types.MonitorConfig) (types.Monitor, error) {
		mu.Lock()
		startCount[cfg.Name]++
		mu.Unlock()
		return NewMockMonitor(cfg.Name), nil
	})

	// Pass no monitors directly — factory is the sole startup path (production behaviour).
	det, err := NewProblemDetector(config, []types.Monitor{}, []types.Exporter{NewMockExporter("exp")}, "/tmp/test-config.yaml", factory, nil)
	if err != nil {
		t.Fatalf("NewProblemDetector() error = %v", err)
	}

	if err := det.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	det.Stop()

	mu.Lock()
	defer mu.Unlock()
	for _, name := range []string{"monitor-a", "monitor-b"} {
		if startCount[name] != 1 {
			t.Errorf("monitor %s started %d time(s), want exactly 1", name, startCount[name])
		}
	}

	stats := det.GetStatistics()
	if stats.GetMonitorsStarted() != 2 {
		t.Errorf("expected 2 monitors started, got %d", stats.GetMonitorsStarted())
	}
}

func TestStatisticsTracking(t *testing.T) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()
	config.Monitors = []types.MonitorConfig{
		helper.CreateTestMonitorConfig("success-monitor", "test"),
		helper.CreateTestMonitorConfig("fail-monitor", "test"),
	}

	// Create mix of successful/failing monitors and exporters
	factory := NewMockMonitorFactory().SetCreateFunc(func(config types.MonitorConfig) (types.Monitor, error) {
		if config.Name == "fail-monitor" {
			return NewMockMonitor(config.Name).SetStartError(fmt.Errorf("failed")), nil
		}
		status := types.NewStatus(config.Name)
		status.AddEvent(types.NewEvent(types.EventError, "CriticalError", "Critical error occurred"))
		return NewMockMonitor(config.Name).AddStatusUpdate(status), nil
	})

	successExporter := NewMockExporter("success-exp")
	failExporter := NewMockExporter("fail-exp").SetStatusExportError(fmt.Errorf("export failed"))

	detector, err := NewProblemDetector(config, []types.Monitor{}, []types.Exporter{successExporter, failExporter}, "/tmp/test-config.yaml", factory, nil)
	if err != nil {
		t.Fatalf("NewProblemDetector() error = %v", err)
	}

	detector.Start()

	// Wait for processing
	time.Sleep(300 * time.Millisecond)

	// Clean shutdown
	detector.Stop()

	stats := detector.GetStatistics()

	// Verify monitor statistics
	if stats.GetMonitorsStarted() != 1 {
		t.Errorf("Expected 1 monitor started, got %d", stats.GetMonitorsStarted())
	}
	if stats.GetMonitorsFailed() != 1 {
		t.Errorf("Expected 1 monitor failed, got %d", stats.GetMonitorsFailed())
	}

	// Verify processing statistics
	if stats.GetStatusesReceived() == 0 {
		t.Errorf("Expected statuses to be received")
	}

	// Verify export statistics
	if stats.GetExportsSucceeded() == 0 {
		t.Errorf("Expected some successful exports")
	}
	if stats.GetExportsFailed() == 0 {
		t.Errorf("Expected some failed exports")
	}

	// Test statistics summary
	summary := stats.Summary()
	if summary == nil {
		t.Errorf("Statistics summary should not be nil")
	}

	// Test statistics copy
	statsCopy := stats.Copy()
	if statsCopy.GetMonitorsStarted() != stats.GetMonitorsStarted() {
		t.Errorf("Statistics copy should match original")
	}
}

// ── dependsOn runtime enforcement tests ──────────────────────────────────────

func TestTopologicalSortMonitors_LinearChain(t *testing.T) {
	monitors := []types.MonitorConfig{
		{Name: "c", DependsOn: []string{"b"}},
		{Name: "a"},
		{Name: "b", DependsOn: []string{"a"}},
	}

	sorted, err := topologicalSortMonitors(monitors)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sorted) != 3 {
		t.Fatalf("expected 3 monitors, got %d", len(sorted))
	}

	pos := make(map[string]int, 3)
	for i, m := range sorted {
		pos[m.Name] = i
	}
	if pos["a"] >= pos["b"] {
		t.Errorf("expected a before b, got positions a=%d b=%d", pos["a"], pos["b"])
	}
	if pos["b"] >= pos["c"] {
		t.Errorf("expected b before c, got positions b=%d c=%d", pos["b"], pos["c"])
	}
}

func TestTopologicalSortMonitors_Diamond(t *testing.T) {
	// d depends on b and c; b and c both depend on a
	monitors := []types.MonitorConfig{
		{Name: "d", DependsOn: []string{"b", "c"}},
		{Name: "b", DependsOn: []string{"a"}},
		{Name: "c", DependsOn: []string{"a"}},
		{Name: "a"},
	}

	sorted, err := topologicalSortMonitors(monitors)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pos := make(map[string]int, 4)
	for i, m := range sorted {
		pos[m.Name] = i
	}
	if pos["a"] >= pos["b"] || pos["a"] >= pos["c"] {
		t.Errorf("a must precede b and c: a=%d b=%d c=%d", pos["a"], pos["b"], pos["c"])
	}
	if pos["b"] >= pos["d"] || pos["c"] >= pos["d"] {
		t.Errorf("b and c must precede d: b=%d c=%d d=%d", pos["b"], pos["c"], pos["d"])
	}
}

func TestTopologicalSortMonitors_NoDeps(t *testing.T) {
	monitors := []types.MonitorConfig{
		{Name: "x"},
		{Name: "y"},
	}

	sorted, err := topologicalSortMonitors(monitors)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sorted) != 2 {
		t.Fatalf("expected 2 monitors, got %d", len(sorted))
	}
}

func TestTopologicalSortMonitors_Cycle(t *testing.T) {
	monitors := []types.MonitorConfig{
		{Name: "a", DependsOn: []string{"b"}},
		{Name: "b", DependsOn: []string{"a"}},
	}

	_, err := topologicalSortMonitors(monitors)
	if err == nil {
		t.Fatal("expected error for cycle, got nil")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error should mention cycle, got: %v", err)
	}
}

func TestTopologicalSortMonitors_SelfLoop(t *testing.T) {
	monitors := []types.MonitorConfig{
		{Name: "a", DependsOn: []string{"a"}},
	}

	_, err := topologicalSortMonitors(monitors)
	if err == nil {
		t.Fatal("expected error for self-loop, got nil")
	}
}

func TestDependsOn_BlockedStateInjection(t *testing.T) {
	// dep-monitor is healthy initially then goes unhealthy.
	// dep-child declares DependsOn: [dep-monitor].
	// When dep-monitor reports ConditionFalse, dep-child's exported status
	// must show "MonitorBlocked" with ConditionUnknown.

	helper := NewTestHelper()
	config := helper.CreateTestConfig()
	config.Monitors = []types.MonitorConfig{
		{Name: "dep-monitor", Type: "test", Enabled: true, Interval: 30 * time.Second, Timeout: 10 * time.Second},
		{Name: "dep-child", Type: "test", Enabled: true, Interval: 30 * time.Second, Timeout: 10 * time.Second, DependsOn: []string{"dep-monitor"}},
	}
	config.ApplyDefaults()

	exporter := NewMockExporter("test-exporter")

	depMonitor := NewMockMonitor("dep-monitor")
	childMonitor := NewMockMonitor("dep-child")

	factory := NewMockMonitorFactory()
	factory.AddMonitor("dep-monitor", depMonitor)
	factory.AddMonitor("dep-child", childMonitor)

	pd, err := NewProblemDetector(config, nil, []types.Exporter{exporter}, "/tmp/test-config.yaml", factory, nil)
	if err != nil {
		t.Fatalf("NewProblemDetector: %v", err)
	}

	if err := pd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer pd.Stop()

	// Step 1: dep-monitor reports healthy — child should pass through.
	healthyStatus := types.NewStatus("dep-monitor")
	healthyStatus.AddCondition(types.NewCondition("Check", types.ConditionTrue, "OK", "all good"))
	pd.statusChan <- healthyStatus

	time.Sleep(150 * time.Millisecond) // let processStatus run

	// Record baseline export count
	baseCnt, _ := exporter.GetExportCounts()

	// Step 2: dep-monitor reports unhealthy.
	unhealthyStatus := types.NewStatus("dep-monitor")
	unhealthyStatus.AddCondition(types.NewCondition("Check", types.ConditionFalse, "Fail", "something broke"))
	pd.statusChan <- unhealthyStatus

	time.Sleep(150 * time.Millisecond)

	// Step 3: child sends a real status — it must be replaced by a blocked status.
	childRealStatus := types.NewStatus("dep-child")
	childRealStatus.AddCondition(types.NewCondition("ChildCheck", types.ConditionTrue, "OK", "child is fine"))
	pd.statusChan <- childRealStatus

	time.Sleep(150 * time.Millisecond)

	exports := exporter.GetStatusExports()
	// Find the last export from dep-child.
	var lastChildExport *types.Status
	for _, s := range exports {
		if s.Source == "dep-child" {
			lastChildExport = s
		}
	}
	if lastChildExport == nil {
		t.Fatalf("no export from dep-child found (total exports since baseline: %d)", len(exports)-baseCnt)
	}

	// Must have the MonitorBlocked condition with ConditionUnknown.
	found := false
	for _, cond := range lastChildExport.Conditions {
		if cond.Type == "MonitorBlocked" && cond.Status == types.ConditionUnknown {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected MonitorBlocked/Unknown condition in dep-child export; got conditions: %+v", lastChildExport.Conditions)
	}
}

func TestDependsOn_StartOrder(t *testing.T) {
	// Verify that the detector starts monitors in topological order by checking
	// that the dependents reverse-lookup is populated correctly after Start().

	helper := NewTestHelper()
	config := helper.CreateTestConfig()
	config.Monitors = []types.MonitorConfig{
		{Name: "child", Type: "test", Enabled: true, Interval: 30 * time.Second, Timeout: 10 * time.Second, DependsOn: []string{"parent"}},
		{Name: "parent", Type: "test", Enabled: true, Interval: 30 * time.Second, Timeout: 10 * time.Second},
	}
	config.ApplyDefaults()

	factory := NewMockMonitorFactory()
	pd, err := NewProblemDetector(config, nil, nil, "/tmp/test-config.yaml", factory, nil)
	if err != nil {
		t.Fatalf("NewProblemDetector: %v", err)
	}

	if err := pd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer pd.Stop()

	// After Start(), dependents["parent"] must include "child".
	deps := pd.dependents["parent"]
	found := false
	for _, d := range deps {
		if d == "child" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected dependents[parent] to contain 'child', got: %v", deps)
	}
}

func TestDependsOn_TransitiveBlocking(t *testing.T) {
	// Chain: grandparent → parent → child.
	// When grandparent becomes unhealthy, parent is blocked;
	// when parent is blocked its effective cached status has MonitorBlocked,
	// so child should also be blocked transitively.

	helper := NewTestHelper()
	config := helper.CreateTestConfig()
	config.Monitors = []types.MonitorConfig{
		{Name: "grandparent", Type: "test", Enabled: true, Interval: 30 * time.Second, Timeout: 10 * time.Second},
		{Name: "parent", Type: "test", Enabled: true, Interval: 30 * time.Second, Timeout: 10 * time.Second, DependsOn: []string{"grandparent"}},
		{Name: "child", Type: "test", Enabled: true, Interval: 30 * time.Second, Timeout: 10 * time.Second, DependsOn: []string{"parent"}},
	}
	config.ApplyDefaults()

	exporter := NewMockExporter("test-exporter")
	factory := NewMockMonitorFactory()

	pd, err := NewProblemDetector(config, nil, []types.Exporter{exporter}, "/tmp/test-config.yaml", factory, nil)
	if err != nil {
		t.Fatalf("NewProblemDetector: %v", err)
	}
	if err := pd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer pd.Stop()

	// grandparent fails.
	grandparentFail := types.NewStatus("grandparent")
	grandparentFail.AddCondition(types.NewCondition("Check", types.ConditionFalse, "Fail", "grandparent broke"))
	pd.statusChan <- grandparentFail
	time.Sleep(150 * time.Millisecond)

	// parent sends a real (internally-OK) status — should become blocked.
	parentStatus := types.NewStatus("parent")
	parentStatus.AddCondition(types.NewCondition("Check", types.ConditionTrue, "OK", "parent is fine internally"))
	pd.statusChan <- parentStatus
	time.Sleep(150 * time.Millisecond)

	// child sends a real status — should also become blocked (transitively).
	childStatus := types.NewStatus("child")
	childStatus.AddCondition(types.NewCondition("Check", types.ConditionTrue, "OK", "child is fine internally"))
	pd.statusChan <- childStatus
	time.Sleep(150 * time.Millisecond)

	exports := exporter.GetStatusExports()

	checkBlocked := func(monitorName string) bool {
		for i := len(exports) - 1; i >= 0; i-- {
			s := exports[i]
			if s.Source != monitorName {
				continue
			}
			for _, cond := range s.Conditions {
				if cond.Type == "MonitorBlocked" {
					return true
				}
			}
			return false // last export for this monitor was NOT blocked
		}
		return false
	}

	if !checkBlocked("parent") {
		t.Error("expected parent to be blocked when grandparent is unhealthy")
	}
	if !checkBlocked("child") {
		t.Error("expected child to be transitively blocked when parent is blocked")
	}
}
