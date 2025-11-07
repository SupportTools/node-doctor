// Package detector provides comprehensive tests for the Problem Detector orchestrator.
package detector

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

func TestNewProblemDetector(t *testing.T) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()

	tests := []struct {
		name      string
		config    *types.NodeDoctorConfig
		monitors  []types.Monitor
		exporters []types.Exporter
		wantError bool
		errorMsg  string
	}{
		{
			name:      "valid configuration",
			config:    config,
			monitors:  []types.Monitor{NewMockMonitor("test-monitor")},
			exporters: []types.Exporter{NewMockExporter("test-exporter")},
			wantError: false,
		},
		{
			name:      "nil config",
			config:    nil,
			monitors:  []types.Monitor{NewMockMonitor("test-monitor")},
			exporters: []types.Exporter{NewMockExporter("test-exporter")},
			wantError: true,
			errorMsg:  "config cannot be nil",
		},
		{
			name:      "no monitors",
			config:    config,
			monitors:  []types.Monitor{},
			exporters: []types.Exporter{NewMockExporter("test-exporter")},
			wantError: true,
			errorMsg:  "at least one monitor is required",
		},
		{
			name:      "no exporters",
			config:    config,
			monitors:  []types.Monitor{NewMockMonitor("test-monitor")},
			exporters: []types.Exporter{},
			wantError: true,
			errorMsg:  "at least one exporter is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detector, err := NewProblemDetector(tt.config, tt.monitors, tt.exporters)

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

func TestProblemDetector_NoMonitors(t *testing.T) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()

	// Create detector with monitor that fails to start
	failingMonitor := NewMockMonitor("failing-monitor").SetStartError(fmt.Errorf("start failed"))
	exporter := NewMockExporter("test-exporter")

	detector, err := NewProblemDetector(config, []types.Monitor{failingMonitor}, []types.Exporter{exporter})
	if err != nil {
		t.Fatalf("NewProblemDetector() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = detector.Run(ctx)
	if err == nil {
		t.Errorf("Run() expected error when no monitors start successfully")
	}
}

func TestProblemDetector_AllMonitorsFail(t *testing.T) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()

	// Create multiple monitors that all fail
	monitor1 := NewMockMonitor("monitor1").SetStartError(fmt.Errorf("failed to start"))
	monitor2 := NewMockMonitor("monitor2").SetStartError(fmt.Errorf("failed to start"))
	exporter := NewMockExporter("test-exporter")

	detector, err := NewProblemDetector(config, []types.Monitor{monitor1, monitor2}, []types.Exporter{exporter})
	if err != nil {
		t.Fatalf("NewProblemDetector() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = detector.Run(ctx)
	if err == nil {
		t.Errorf("Run() expected error when all monitors fail to start")
	}

	// Verify statistics
	stats := detector.GetStatistics()
	if stats.GetMonitorsFailed() != 2 {
		t.Errorf("Expected 2 failed monitors, got %d", stats.GetMonitorsFailed())
	}
	if stats.GetMonitorsStarted() != 0 {
		t.Errorf("Expected 0 started monitors, got %d", stats.GetMonitorsStarted())
	}
}

func TestProblemDetector_SomeMonitorsFail(t *testing.T) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()

	// Create mix of successful and failing monitors
	successMonitor := NewMockMonitor("success-monitor").AddStatusUpdate(helper.CreateTestStatus("success-monitor"))
	failMonitor := NewMockMonitor("fail-monitor").SetStartError(fmt.Errorf("failed to start"))
	exporter := NewMockExporter("test-exporter")

	detector, err := NewProblemDetector(config, []types.Monitor{successMonitor, failMonitor}, []types.Exporter{exporter})
	if err != nil {
		t.Fatalf("NewProblemDetector() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err = detector.Run(ctx)
	if err != nil {
		t.Errorf("Run() unexpected error = %v", err)
	}

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
	monitor := NewMockMonitor("test-monitor").AddStatusUpdate(helper.CreateTestStatus("test-monitor"))
	exporter := NewMockExporter("test-exporter")

	detector, err := NewProblemDetector(config, []types.Monitor{monitor}, []types.Exporter{exporter})
	if err != nil {
		t.Fatalf("NewProblemDetector() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err = detector.Run(ctx)
	if err != nil {
		t.Errorf("Run() unexpected error = %v", err)
	}

	// Wait a bit for processing
	time.Sleep(200 * time.Millisecond)

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
	detector, _ := NewProblemDetector(config, []types.Monitor{NewMockMonitor("test")}, []types.Exporter{NewMockExporter("test")})

	tests := []struct {
		name               string
		status             *types.Status
		expectedCount      int
		expectedTypes      []string
		expectedSeverities []types.ProblemSeverity
	}{
		{
			name:               "error event",
			status:             helper.CreateErrorStatus("test-source"),
			expectedCount:      2, // 1 error event + 1 false condition
			expectedTypes:      []string{"event-CriticalError", "condition-SystemHealth"},
			expectedSeverities: []types.ProblemSeverity{types.ProblemCritical, types.ProblemCritical},
		},
		{
			name:               "warning event",
			status:             helper.CreateWarningStatus("test-source"),
			expectedCount:      1,
			expectedTypes:      []string{"event-PerformanceWarning"},
			expectedSeverities: []types.ProblemSeverity{types.ProblemWarning},
		},
		{
			name:          "healthy status",
			status:        helper.CreateHealthyStatus("test-source"),
			expectedCount: 0, // Info events and true conditions are ignored
		},
		{
			name:          "empty status",
			status:        helper.CreateEmptyStatus("test-source"),
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			problems := detector.statusToProblems(tt.status)

			if len(problems) != tt.expectedCount {
				t.Errorf("statusToProblems() problem count = %d, want %d", len(problems), tt.expectedCount)
			}

			for i, problem := range problems {
				if i < len(tt.expectedTypes) && problem.Type != tt.expectedTypes[i] {
					t.Errorf("statusToProblems() problem[%d] type = %s, want %s", i, problem.Type, tt.expectedTypes[i])
				}
				if i < len(tt.expectedSeverities) && problem.Severity != tt.expectedSeverities[i] {
					t.Errorf("statusToProblems() problem[%d] severity = %s, want %s", i, problem.Severity, tt.expectedSeverities[i])
				}
			}
		})
	}
}

func TestStatusToProblems_Conditions(t *testing.T) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()
	detector, _ := NewProblemDetector(config, []types.Monitor{NewMockMonitor("test")}, []types.Exporter{NewMockExporter("test")})

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
	detector, _ := NewProblemDetector(config, []types.Monitor{NewMockMonitor("test")}, []types.Exporter{NewMockExporter("test")})

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

	monitor := NewMockMonitor("test-monitor").AddStatusUpdate(helper.CreateErrorStatus("test-monitor"))

	detector, err := NewProblemDetector(config, []types.Monitor{monitor}, []types.Exporter{exporter1, exporter2, exporter3})
	if err != nil {
		t.Fatalf("NewProblemDetector() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err = detector.Run(ctx)
	if err != nil {
		t.Errorf("Run() unexpected error = %v", err)
	}

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	// Verify all exporters received the status
	exporters := []*MockExporter{exporter1, exporter2, exporter3}
	for i, exp := range exporters {
		statusCount := exp.GetStatusExportCount()
		if statusCount == 0 {
			t.Errorf("Exporter %d did not receive any status updates", i+1)
		}

		problemCount := exp.GetProblemExportCount()
		if problemCount == 0 {
			t.Errorf("Exporter %d did not receive any problems", i+1)
		}
	}
}

func TestGracefulShutdown(t *testing.T) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()

	monitor := NewMockMonitor("test-monitor")
	exporter := NewMockExporter("test-exporter")

	detector, err := NewProblemDetector(config, []types.Monitor{monitor}, []types.Exporter{exporter})
	if err != nil {
		t.Fatalf("NewProblemDetector() error = %v", err)
	}

	// Start detector in background
	ctx, cancel := context.WithCancel(context.Background())
	var runErr error
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		runErr = detector.Run(ctx)
	}()

	// Wait for startup
	time.Sleep(100 * time.Millisecond)

	// Verify it's running
	if !detector.IsRunning() {
		t.Errorf("Detector should be running")
	}

	// Cancel context to trigger shutdown
	cancel()

	// Wait for shutdown
	wg.Wait()

	// Verify it's stopped
	if detector.IsRunning() {
		t.Errorf("Detector should be stopped after shutdown")
	}

	// Verify monitor was stopped
	if !monitor.IsStopped() {
		t.Errorf("Monitor should be stopped after detector shutdown")
	}

	if runErr != nil {
		t.Errorf("Run() unexpected error during shutdown = %v", runErr)
	}
}

func TestShutdownTimeout(t *testing.T) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()

	// Create exporter with long delay to simulate hung exporter
	exporter := NewMockExporter("slow-exporter").SetExportDelay(2 * time.Second)
	monitor := NewMockMonitor("test-monitor").AddStatusUpdate(helper.CreateTestStatus("test-monitor"))

	detector, err := NewProblemDetector(config, []types.Monitor{monitor}, []types.Exporter{exporter})
	if err != nil {
		t.Fatalf("NewProblemDetector() error = %v", err)
	}

	// Start and immediately cancel to trigger shutdown with pending operations
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		detector.Run(ctx)
	}()

	// Let it start and begin processing
	time.Sleep(100 * time.Millisecond)
	cancel()

	// Wait for shutdown - should complete even with slow exporter
	wg.Wait()

	// Note: We expect shutdown to complete within the 30s timeout,
	// but this test verifies the mechanism works rather than timing it precisely
}

func TestConcurrencySafety(t *testing.T) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()

	// Create multiple monitors sending concurrent updates
	var monitors []types.Monitor
	for i := 0; i < 5; i++ {
		monitor := NewMockMonitor(fmt.Sprintf("monitor-%d", i)).
			SetSendInterval(10 * time.Millisecond).
			AddStatusUpdate(helper.CreateTestStatus(fmt.Sprintf("monitor-%d", i)))
		monitors = append(monitors, monitor)
	}

	exporter := NewMockExporter("test-exporter")

	detector, err := NewProblemDetector(config, monitors, []types.Exporter{exporter})
	if err != nil {
		t.Fatalf("NewProblemDetector() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err = detector.Run(ctx)
	if err != nil {
		t.Errorf("Run() unexpected error = %v", err)
	}

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

	// Create monitor that sends many rapid updates
	monitor := NewMockMonitor("rapid-monitor").SetSendInterval(1 * time.Millisecond)
	for i := 0; i < 2000; i++ { // More than channel buffer size
		monitor.AddStatusUpdate(helper.CreateTestStatus("rapid-monitor"))
	}

	exporter := NewMockExporter("test-exporter")

	detector, err := NewProblemDetector(config, []types.Monitor{monitor}, []types.Exporter{exporter})
	if err != nil {
		t.Fatalf("NewProblemDetector() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Should not block or crash even with channel overflow
	err = detector.Run(ctx)
	if err != nil {
		t.Errorf("Run() unexpected error = %v", err)
	}

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

	monitor := NewMockMonitor("test-monitor").AddStatusUpdate(helper.CreateErrorStatus("test-monitor"))

	detector, err := NewProblemDetector(config, []types.Monitor{monitor}, []types.Exporter{successExporter, failStatusExporter, failProblemExporter})
	if err != nil {
		t.Fatalf("NewProblemDetector() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err = detector.Run(ctx)
	if err != nil {
		t.Errorf("Run() unexpected error = %v", err)
	}

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	// Verify successful exporter still received updates
	if successExporter.GetStatusExportCount() == 0 {
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

	monitor := NewMockMonitor("test-monitor").AddStatusUpdate(helper.CreateTestStatus("test-monitor"))
	exporter := NewMockExporter("test-exporter")

	detector, err := NewProblemDetector(config, []types.Monitor{monitor}, []types.Exporter{exporter})
	if err != nil {
		t.Fatalf("NewProblemDetector() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Start detector
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		detector.Run(ctx)
	}()

	// Let it start
	time.Sleep(100 * time.Millisecond)

	// Stop the monitor (closes channel)
	monitor.Stop()

	// Wait for completion
	wg.Wait()

	// Should handle channel closure gracefully
	if detector.IsRunning() {
		t.Errorf("Detector should have stopped gracefully")
	}
}

func TestStatisticsTracking(t *testing.T) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()

	// Create mix of successful/failing monitors and exporters
	successMonitor := NewMockMonitor("success").AddStatusUpdate(helper.CreateErrorStatus("success"))
	failMonitor := NewMockMonitor("fail").SetStartError(fmt.Errorf("failed"))

	successExporter := NewMockExporter("success-exp")
	failExporter := NewMockExporter("fail-exp").SetProblemExportError(fmt.Errorf("export failed"))

	detector, err := NewProblemDetector(config, []types.Monitor{successMonitor, failMonitor}, []types.Exporter{successExporter, failExporter})
	if err != nil {
		t.Fatalf("NewProblemDetector() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	detector.Run(ctx)

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

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
