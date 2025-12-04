package workflows

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/detector"
	"github.com/supporttools/node-doctor/pkg/types"
	"github.com/supporttools/node-doctor/test"
)

// TestMonitorToDetectorToExporterFlow tests the complete workflow from monitor → detector → exporter
func TestMonitorToDetectorToExporterFlow(t *testing.T) {
	_, cancel := test.TestContext(t, 30*time.Second)
	defer cancel()

	// Create a mock monitor that sends test statuses
	mockMonitor := newMockMonitor("test-monitor")

	// Create a mock exporter that tracks received statuses and problems
	mockExporter := newMockExporter()

	// Create configuration
	config := &types.NodeDoctorConfig{
		APIVersion: "node-doctor.io/v1alpha1",
		Kind:       "NodeDoctorConfig",
		Metadata: types.ConfigMetadata{
			Name: "integration-test",
		},
		Settings: types.GlobalSettings{
			NodeName:          "test-node",
			LogLevel:          "info",
			LogFormat:         "text",
			LogOutput:         "stdout",
			UpdateInterval:    30 * time.Second,
			ResyncInterval:    5 * time.Minute,
			HeartbeatInterval: 30 * time.Second,
			QPS:               5,
			Burst:             10,
		},
		Remediation: types.RemediationConfig{
			Enabled:           false,
			CooldownPeriod:    5 * time.Minute,
			MaxAttemptsGlobal: 3,
			HistorySize:       100,
		},
	}

	// Create temp config file for test
	configPath := test.TempConfigFile(t, "integration-test")

	// Create problem detector (with config file for reload support)
	pd, err := detector.NewProblemDetector(config, []types.Monitor{mockMonitor}, []types.Exporter{mockExporter}, configPath, nil)
	test.AssertNoError(t, err, "Failed to create problem detector")

	// Start detector in background
	var detectorErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		detectorErr = pd.Run()
	}()

	// Give detector time to start
	time.Sleep(100 * time.Millisecond)

	// Test 1: Send a healthy status
	mockMonitor.SendStatus(&types.Status{
		Source:    "test-monitor",
		Timestamp: time.Now(),
		Events: []types.Event{
			{
				Severity:  types.EventInfo,
				Timestamp: time.Now(),
				Reason:    "HealthCheckPassed",
				Message:   "All checks passed",
			},
		},
		Conditions: []types.Condition{
			{
				Type:       "Healthy",
				Status:     types.ConditionTrue,
				Transition: time.Now(),
				Reason:     "AllChecksPass",
				Message:    "Monitor is healthy",
			},
		},
	})

	// Wait for status to be processed
	time.Sleep(200 * time.Millisecond)

	// Verify exporter received the status
	statuses := mockExporter.GetStatuses()
	test.AssertEqual(t, 1, len(statuses), "Expected 1 status")
	test.AssertEqual(t, "test-monitor", statuses[0].Source, "Status source mismatch")

	// Test 2: Send an unhealthy status with errors
	mockMonitor.SendStatus(&types.Status{
		Source:    "test-monitor",
		Timestamp: time.Now(),
		Events: []types.Event{
			{
				Severity:  types.EventError,
				Timestamp: time.Now(),
				Reason:    "ServiceFailed",
				Message:   "Service has failed",
			},
			{
				Severity:  types.EventWarning,
				Timestamp: time.Now(),
				Reason:    "HighLatency",
				Message:   "Latency is high",
			},
		},
		Conditions: []types.Condition{
			{
				Type:       "Ready",
				Status:     types.ConditionFalse,
				Transition: time.Now(),
				Reason:     "ServiceNotReady",
				Message:    "Service is not ready",
			},
		},
	})

	// Wait for status to be processed
	time.Sleep(200 * time.Millisecond)

	// Verify exporter received the second status
	statuses = mockExporter.GetStatuses()
	test.AssertEqual(t, 2, len(statuses), "Expected 2 statuses")

	// Verify the unhealthy status contains the expected events and conditions
	// Note: Since GitHub issue #7 fix, we only export Status objects (not Problems)
	// The Status contains all events and conditions for the exporter to process
	unhealthyStatus := statuses[1]
	test.AssertEqual(t, 2, len(unhealthyStatus.Events), "Expected 2 events in unhealthy status")
	test.AssertEqual(t, 1, len(unhealthyStatus.Conditions), "Expected 1 condition in unhealthy status")

	// Verify event details in the status
	var hasServiceFailed, hasHighLatency bool
	for _, event := range unhealthyStatus.Events {
		if event.Reason == "ServiceFailed" {
			hasServiceFailed = true
			test.AssertEqual(t, types.EventError, event.Severity, "ServiceFailed should be error severity")
		}
		if event.Reason == "HighLatency" {
			hasHighLatency = true
			test.AssertEqual(t, types.EventWarning, event.Severity, "HighLatency should be warning severity")
		}
	}
	test.AssertTrue(t, hasServiceFailed, "Missing ServiceFailed event")
	test.AssertTrue(t, hasHighLatency, "Missing HighLatency event")

	// Verify condition details in the status
	test.AssertEqual(t, "Ready", unhealthyStatus.Conditions[0].Type, "Condition type mismatch")
	test.AssertEqual(t, types.ConditionFalse, unhealthyStatus.Conditions[0].Status, "Ready condition should be False")

	// Stop detector
	cancel()
	wg.Wait()

	// Verify detector stopped without error
	if detectorErr != nil && detectorErr != context.Canceled {
		t.Errorf("Detector returned unexpected error: %v", detectorErr)
	}
}

// TestMultipleMonitorsWorkflow tests integration with multiple monitors running concurrently
func TestMultipleMonitorsWorkflow(t *testing.T) {
	_, cancel := test.TestContext(t, 30*time.Second)
	defer cancel()

	// Create multiple mock monitors
	monitor1 := newMockMonitor("monitor-1")
	monitor2 := newMockMonitor("monitor-2")
	monitor3 := newMockMonitor("monitor-3")

	// Create mock exporter
	mockExporter := newMockExporter()

	// Create configuration
	config := &types.NodeDoctorConfig{
		APIVersion: "node-doctor.io/v1alpha1",
		Kind:       "NodeDoctorConfig",
		Metadata: types.ConfigMetadata{
			Name: "multi-monitor-test",
		},
		Settings: types.GlobalSettings{
			NodeName:          "test-node",
			LogLevel:          "info",
			LogFormat:         "text",
			LogOutput:         "stdout",
			UpdateInterval:    30 * time.Second,
			ResyncInterval:    5 * time.Minute,
			HeartbeatInterval: 30 * time.Second,
			QPS:               5,
			Burst:             10,
		},
		Remediation: types.RemediationConfig{
			Enabled:           false,
			CooldownPeriod:    5 * time.Minute,
			MaxAttemptsGlobal: 3,
			HistorySize:       100,
		},
	}

	// Create temp config file for test
	configPath := test.TempConfigFile(t, "multi-monitor-test")

	// Create problem detector with multiple monitors
	pd, err := detector.NewProblemDetector(
		config,
		[]types.Monitor{monitor1, monitor2, monitor3},
		[]types.Exporter{mockExporter},
		configPath, // use temp config file for reload support
		nil,        // no monitor factory in tests
	)
	test.AssertNoError(t, err, "Failed to create problem detector")

	// Start detector
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		pd.Run()
	}()

	// Give detector time to start
	time.Sleep(100 * time.Millisecond)

	// Send statuses from all monitors concurrently
	var sendWg sync.WaitGroup
	for i, mon := range []*mockMonitor{monitor1, monitor2, monitor3} {
		sendWg.Add(1)
		go func(m *mockMonitor, idx int) {
			defer sendWg.Done()
			for j := 0; j < 5; j++ {
				m.SendStatus(&types.Status{
					Source:    m.name,
					Timestamp: time.Now(),
					Events: []types.Event{
						{
							Severity:  types.EventInfo,
							Timestamp: time.Now(),
							Reason:    "Update",
							Message:   "Status update",
						},
					},
					Conditions: []types.Condition{},
				})
				time.Sleep(10 * time.Millisecond)
			}
		}(mon, i)
	}

	sendWg.Wait()

	// Wait for all statuses to be processed
	time.Sleep(500 * time.Millisecond)

	// Verify exporter received statuses from all monitors
	statuses := mockExporter.GetStatuses()
	test.AssertEqual(t, 15, len(statuses), "Expected 15 statuses (3 monitors × 5 statuses)")

	// Verify statuses from each monitor
	monitor1Count := 0
	monitor2Count := 0
	monitor3Count := 0
	for _, status := range statuses {
		switch status.Source {
		case "monitor-1":
			monitor1Count++
		case "monitor-2":
			monitor2Count++
		case "monitor-3":
			monitor3Count++
		}
	}

	test.AssertEqual(t, 5, monitor1Count, "Expected 5 statuses from monitor-1")
	test.AssertEqual(t, 5, monitor2Count, "Expected 5 statuses from monitor-2")
	test.AssertEqual(t, 5, monitor3Count, "Expected 5 statuses from monitor-3")

	// Stop detector
	cancel()
	wg.Wait()
}

// TestStatusExportFlow tests that all statuses are exported without modification
// Note: Problem deduplication was removed in GitHub issue #7 fix - the detector
// now only exports Status objects, and any deduplication happens at the exporter level.
func TestStatusExportFlow(t *testing.T) {
	_, cancel := test.TestContext(t, 30*time.Second)
	defer cancel()

	mockMonitor := newMockMonitor("test-monitor")
	mockExporter := newMockExporter()

	config := &types.NodeDoctorConfig{
		APIVersion: "node-doctor.io/v1alpha1",
		Kind:       "NodeDoctorConfig",
		Metadata: types.ConfigMetadata{
			Name: "status-export-test",
		},
		Settings: types.GlobalSettings{
			NodeName:          "test-node",
			LogLevel:          "info",
			LogFormat:         "text",
			LogOutput:         "stdout",
			UpdateInterval:    30 * time.Second,
			ResyncInterval:    5 * time.Minute,
			HeartbeatInterval: 30 * time.Second,
			QPS:               5,
			Burst:             10,
		},
		Remediation: types.RemediationConfig{
			Enabled:           false,
			CooldownPeriod:    5 * time.Minute,
			MaxAttemptsGlobal: 3,
			HistorySize:       100,
		},
	}

	// Create temp config file for test
	configPath := test.TempConfigFile(t, "status-export-test")

	pd, err := detector.NewProblemDetector(config, []types.Monitor{mockMonitor}, []types.Exporter{mockExporter}, configPath, nil)
	test.AssertNoError(t, err)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		pd.Run()
	}()

	time.Sleep(100 * time.Millisecond)

	// Send multiple statuses with the same error event
	for i := 0; i < 3; i++ {
		mockMonitor.SendStatus(&types.Status{
			Source:    "test-monitor",
			Timestamp: time.Now(),
			Events: []types.Event{
				{
					Severity:  types.EventError,
					Timestamp: time.Now(),
					Reason:    "ServiceFailed",
					Message:   "Service has failed",
				},
			},
			Conditions: []types.Condition{},
		})
		time.Sleep(100 * time.Millisecond)
	}

	// Wait for processing
	time.Sleep(300 * time.Millisecond)

	// Verify exporter received all 3 statuses (no deduplication at detector level)
	statuses := mockExporter.GetStatuses()
	test.AssertEqual(t, 3, len(statuses), "Expected 3 statuses")

	// Verify each status contains the expected event
	for i, status := range statuses {
		test.AssertEqual(t, 1, len(status.Events), "Expected 1 event in status %d", i)
		test.AssertEqual(t, "ServiceFailed", status.Events[0].Reason, "Event reason mismatch in status %d", i)
		test.AssertEqual(t, types.EventError, status.Events[0].Severity, "Event severity mismatch in status %d", i)
	}

	// Send status with different severity
	mockMonitor.SendStatus(&types.Status{
		Source:    "test-monitor",
		Timestamp: time.Now(),
		Events: []types.Event{
			{
				Severity:  types.EventWarning, // Changed from Error to Warning
				Timestamp: time.Now(),
				Reason:    "ServiceFailed",
				Message:   "Service has failed",
			},
		},
		Conditions: []types.Condition{},
	})

	time.Sleep(200 * time.Millisecond)

	// Verify fourth status was exported
	statuses = mockExporter.GetStatuses()
	test.AssertEqual(t, 4, len(statuses), "Expected 4 statuses")
	test.AssertEqual(t, types.EventWarning, statuses[3].Events[0].Severity, "Fourth status should have warning severity")

	cancel()
	wg.Wait()
}

// mockMonitor is a mock Monitor implementation for testing
type mockMonitor struct {
	name     string
	statusCh chan *types.Status
	started  bool
	mu       sync.Mutex
}

func newMockMonitor(name string) *mockMonitor {
	return &mockMonitor{
		name:     name,
		statusCh: make(chan *types.Status, 100),
	}
}

func (m *mockMonitor) Start() (<-chan *types.Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return nil, fmt.Errorf("monitor already started")
	}
	m.started = true
	return m.statusCh, nil
}

func (m *mockMonitor) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		close(m.statusCh)
		m.started = false
	}
}

func (m *mockMonitor) SendStatus(status *types.Status) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		m.statusCh <- status
	}
}

// mockExporter is a mock Exporter implementation for testing
type mockExporter struct {
	mu       sync.RWMutex
	statuses []*types.Status
	problems []*types.Problem
}

func newMockExporter() *mockExporter {
	return &mockExporter{
		statuses: make([]*types.Status, 0),
		problems: make([]*types.Problem, 0),
	}
}

func (e *mockExporter) ExportStatus(ctx context.Context, status *types.Status) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.statuses = append(e.statuses, status)
	return nil
}

func (e *mockExporter) ExportProblem(ctx context.Context, problem *types.Problem) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.problems = append(e.problems, problem)
	return nil
}

func (e *mockExporter) GetStatuses() []*types.Status {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make([]*types.Status, len(e.statuses))
	copy(result, e.statuses)
	return result
}

func (e *mockExporter) GetProblems() []*types.Problem {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make([]*types.Problem, len(e.problems))
	copy(result, e.problems)
	return result
}

func (e *mockExporter) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.statuses = make([]*types.Status, 0)
	e.problems = make([]*types.Problem, 0)
}
