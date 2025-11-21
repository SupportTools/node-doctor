// Package detector provides comprehensive tests for the Problem Detector orchestrator.
package detector

import (
	"fmt"
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
		name         string
		config       *types.NodeDoctorConfig
		monitors     []types.Monitor
		exporters    []types.Exporter
		configPath   string
		factory      MonitorFactory
		wantError    bool
		errorMsg     string
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
			detector, err := NewProblemDetector(tt.config, tt.monitors, tt.exporters, tt.configPath, tt.factory)

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

func TestProblemDetector_AllMonitorsFail(t *testing.T) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()

	// Create factory that returns monitors that fail to start
	factory := NewMockMonitorFactory().SetCreateFunc(func(config types.MonitorConfig) (types.Monitor, error) {
		return NewMockMonitor(config.Name).SetStartError(fmt.Errorf("failed to start")), nil
	})

	detector, err := NewProblemDetector(config, []types.Monitor{}, []types.Exporter{NewMockExporter("test")}, "/tmp/test-config.yaml", factory)
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

	detector, err := NewProblemDetector(config, []types.Monitor{}, []types.Exporter{NewMockExporter("test")}, "/tmp/test-config.yaml", factory)
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
	detector, err := NewProblemDetector(config, []types.Monitor{}, []types.Exporter{exporter}, "/tmp/test-config.yaml", factory)
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

func TestStatusToProblems_Events(t *testing.T) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()
	factory := NewMockMonitorFactory()
	detector, _ := NewProblemDetector(config, []types.Monitor{}, []types.Exporter{NewMockExporter("test")}, "/tmp/test-config.yaml", factory)

	tests := []struct {
		name               string
		status             *types.Status
		expectedCount      int
		containsTypes      []string  // Check for presence, not order
		containsSeverities []types.ProblemSeverity
	}{
		{
			name: "error event",
			status: func() *types.Status {
				status := types.NewStatus("test-source")
				status.AddEvent(types.NewEvent(types.EventError, "CriticalError", "Critical error occurred"))
				status.AddCondition(types.NewCondition("SystemHealth", types.ConditionFalse, "HealthCheck", "System not healthy"))
				return status
			}(),
			expectedCount:      2, // 1 error event + 1 false condition
			containsTypes:      []string{"event-CriticalError", "condition-SystemHealth"},
			containsSeverities: []types.ProblemSeverity{types.ProblemCritical, types.ProblemWarning},
		},
		{
			name: "warning event",
			status: func() *types.Status {
				status := types.NewStatus("test-source")
				status.AddEvent(types.NewEvent(types.EventWarning, "PerformanceWarning", "Warning occurred"))
				return status
			}(),
			expectedCount:      1,
			containsTypes:      []string{"event-PerformanceWarning"},
			containsSeverities: []types.ProblemSeverity{types.ProblemWarning},
		},
		{
			name: "healthy status",
			status: func() *types.Status {
				status := types.NewStatus("test-source")
				status.AddCondition(types.NewCondition("Ready", types.ConditionTrue, "Ready", "Ready"))
				return status
			}(),
			expectedCount: 0, // Info events and true conditions are ignored
		},
		{
			name:          "empty status",
			status:        types.NewStatus("test-source"),
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			problems := detector.statusToProblems(tt.status)

			if len(problems) != tt.expectedCount {
				t.Errorf("statusToProblems() problem count = %d, want %d", len(problems), tt.expectedCount)
			}

			// Check that all expected types are present
			if tt.expectedCount > 0 {
				problemTypes := make(map[string]bool)
				problemSeverities := make(map[types.ProblemSeverity]bool)

				for _, problem := range problems {
					problemTypes[problem.Type] = true
					problemSeverities[problem.Severity] = true
				}

				for _, expectedType := range tt.containsTypes {
					if !problemTypes[expectedType] {
						t.Errorf("statusToProblems() missing expected problem type: %s", expectedType)
					}
				}

				for _, expectedSeverity := range tt.containsSeverities {
					if !problemSeverities[expectedSeverity] {
						t.Errorf("statusToProblems() missing expected problem severity: %s", expectedSeverity)
					}
				}
			}
		})
	}
}

func TestStatusToProblems_Conditions(t *testing.T) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()
	factory := NewMockMonitorFactory()
	detector, _ := NewProblemDetector(config, []types.Monitor{}, []types.Exporter{NewMockExporter("test")}, "/tmp/test-config.yaml", factory)

	status := types.NewStatus("test-source")
	status.AddCondition(types.NewCondition("DiskPressure", types.ConditionFalse, "DiskFull", "Disk is full"))
	status.AddCondition(types.NewCondition("NetworkReady", types.ConditionTrue, "NetworkOK", "Network is ready"))
	status.AddCondition(types.NewCondition("UnknownCondition", types.ConditionUnknown, "Unknown", "Status unknown"))

	problems := detector.statusToProblems(status)

	// Should only create problem for False condition
	if len(problems) != 1 {
		t.Errorf("statusToProblems() problem count = %d, want 1", len(problems))
	}

	if problems[0].Type != "condition-DiskPressure" {
		t.Errorf("statusToProblems() problem type = %s, want condition-DiskPressure", problems[0].Type)
	}
}

func TestDeduplicateProblems(t *testing.T) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()
	factory := NewMockMonitorFactory()
	detector, _ := NewProblemDetector(config, []types.Monitor{}, []types.Exporter{NewMockExporter("test")}, "/tmp/test-config.yaml", factory)

	// First set of problems
	problem1 := types.NewProblem("disk-full", "node1", types.ProblemCritical, "Disk is full")
	problem2 := types.NewProblem("memory-pressure", "node1", types.ProblemWarning, "Memory pressure detected")

	newProblems := detector.deduplicateProblems([]*types.Problem{problem1, problem2})
	if len(newProblems) != 2 {
		t.Errorf("First deduplication expected 2 new problems, got %d", len(newProblems))
	}

	// Same problems again (should be deduplicated)
	problem1Dup := types.NewProblem("disk-full", "node1", types.ProblemCritical, "Disk is full")
	newProblems = detector.deduplicateProblems([]*types.Problem{problem1Dup})
	if len(newProblems) != 0 {
		t.Errorf("Second deduplication expected 0 new problems, got %d", len(newProblems))
	}

	// Same type/resource but different severity (should be reported)
	problem1Updated := types.NewProblem("disk-full", "node1", types.ProblemWarning, "Disk pressure reduced")
	newProblems = detector.deduplicateProblems([]*types.Problem{problem1Updated})
	if len(newProblems) != 1 {
		t.Errorf("Third deduplication expected 1 new problem, got %d", len(newProblems))
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

	detector, err := NewProblemDetector(config, []types.Monitor{}, []types.Exporter{exporter1, exporter2, exporter3}, "/tmp/test-config.yaml", factory)
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

	detector, err := NewProblemDetector(config, []types.Monitor{}, []types.Exporter{exporter}, "/tmp/test-config.yaml", factory)
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

	detector, err := NewProblemDetector(config, []types.Monitor{}, []types.Exporter{exporter}, "/tmp/test-config.yaml", factory)
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

	detector, err := NewProblemDetector(config, []types.Monitor{}, []types.Exporter{exporter}, "/tmp/test-config.yaml", factory)
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

	detector, err := NewProblemDetector(config, []types.Monitor{}, []types.Exporter{exporter}, "/tmp/test-config.yaml", factory)
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
	successExporter := NewMockExporter("success-exporter")
	failStatusExporter := NewMockExporter("fail-status").SetStatusExportError(fmt.Errorf("status export failed"))
	failProblemExporter := NewMockExporter("fail-problem").SetProblemExportError(fmt.Errorf("problem export failed"))

	factory := NewMockMonitorFactory().SetCreateFunc(func(config types.MonitorConfig) (types.Monitor, error) {
		status := types.NewStatus(config.Name)
		status.AddEvent(types.NewEvent(types.EventError, "CriticalError", "Critical error occurred"))
		return NewMockMonitor(config.Name).AddStatusUpdate(status), nil
	})

	detector, err := NewProblemDetector(config, []types.Monitor{}, []types.Exporter{successExporter, failStatusExporter, failProblemExporter}, "/tmp/test-config.yaml", factory)
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

	detector, err := NewProblemDetector(config, []types.Monitor{}, []types.Exporter{exporter}, "/tmp/test-config.yaml", factory)
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
	failExporter := NewMockExporter("fail-exp").SetProblemExportError(fmt.Errorf("export failed"))

	detector, err := NewProblemDetector(config, []types.Monitor{}, []types.Exporter{successExporter, failExporter}, "/tmp/test-config.yaml", factory)
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
	if stats.GetProblemsDetected() == 0 {
		t.Errorf("Expected problems to be detected")
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