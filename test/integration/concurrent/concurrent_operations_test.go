package concurrent

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/detector"
	"github.com/supporttools/node-doctor/pkg/remediators"
	"github.com/supporttools/node-doctor/pkg/types"
	"github.com/supporttools/node-doctor/test"
)

// TestConcurrentMonitorStatusUpdates tests multiple monitors sending statuses concurrently
func TestConcurrentMonitorStatusUpdates(t *testing.T) {
	ctx, cancel := test.TestContext(t, 60*time.Second)
	defer cancel()

	const numMonitors = 10
	const statusesPerMonitor = 50

	// Create multiple monitors
	monitors := make([]types.Monitor, numMonitors)
	mockMonitors := make([]*mockMonitor, numMonitors)
	for i := 0; i < numMonitors; i++ {
		m := newMockMonitor(fmt.Sprintf("monitor-%d", i))
		monitors[i] = m
		mockMonitors[i] = m
	}

	// Create mock exporter
	mockExporter := newMockExporter()

	// Create configuration
	config := &types.NodeDoctorConfig{
		APIVersion: "node-doctor.io/v1alpha1",
		Kind:       "NodeDoctorConfig",
		Metadata: types.ConfigMetadata{
			Name: "concurrent-test",
		},
		Settings: types.GlobalSettings{
			NodeName: "concurrent-test-node",
		},
	}

	// Create detector (no reload in tests)
	pd, err := detector.NewProblemDetector(config, monitors, []types.Exporter{mockExporter}, "", nil)
	test.AssertNoError(t, err)

	// Start detector
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		pd.Run()
	}()

	// Give detector time to start
	time.Sleep(200 * time.Millisecond)

	// Send statuses concurrently from all monitors
	var sendWg sync.WaitGroup
	for i, m := range mockMonitors {
		sendWg.Add(1)
		go func(monitor *mockMonitor, id int) {
			defer sendWg.Done()
			for j := 0; j < statusesPerMonitor; j++ {
				status := &types.Status{
					Source:    monitor.name,
					Timestamp: time.Now(),
					Events: []types.Event{
						{
							Severity:  types.EventInfo,
							Timestamp: time.Now(),
							Reason:    fmt.Sprintf("Update%d", j),
							Message:   fmt.Sprintf("Status update %d from %s", j, monitor.name),
						},
					},
					Conditions: []types.Condition{},
				}
				monitor.SendStatus(status)
				time.Sleep(5 * time.Millisecond) // Small delay between sends
			}
		}(m, i)
	}

	sendWg.Wait()

	// Wait for all statuses to be processed
	time.Sleep(2 * time.Second)

	// Verify all statuses were received
	statuses := mockExporter.GetStatuses()
	expectedCount := numMonitors * statusesPerMonitor
	test.AssertTrue(t, len(statuses) >= int(float64(expectedCount)*0.95), // Allow 5% tolerance
		"Expected approximately %d statuses, got %d", expectedCount, len(statuses))

	// Verify statuses from all monitors were received
	monitorCounts := make(map[string]int)
	for _, status := range statuses {
		monitorCounts[status.Source]++
	}

	for i := 0; i < numMonitors; i++ {
		monitorName := fmt.Sprintf("monitor-%d", i)
		count := monitorCounts[monitorName]
		test.AssertTrue(t, count >= int(float64(statusesPerMonitor)*0.9), // Allow 10% tolerance
			"Monitor %s: expected ~%d statuses, got %d", monitorName, statusesPerMonitor, count)
	}

	cancel()
	wg.Wait()
}

// TestConcurrentRemediationWithDifferentResources tests concurrent remediation on different resources
func TestConcurrentRemediationWithDifferentResources(t *testing.T) {
	ctx := context.Background()

	// Create mock remediator (use 1ms cooldown to avoid validation error)
	mockRemediator := newThreadSafeRemediator("concurrent-remediator", 1*time.Millisecond)

	// Create registry (0 = no rate limit, 100 = history size)
	registry := remediators.NewRegistry(0, 100)
	registry.Register(remediators.RemediatorInfo{
		Type: "concurrent-problem",
		Factory: func() (types.Remediator, error) {
			return mockRemediator, nil
		},
		Description: "Concurrent test remediator",
	})

	// Launch concurrent remediation attempts on different resources
	const numGoroutines = 100
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			problem := types.NewProblem(
				"concurrent-problem",
				fmt.Sprintf("resource-%d", id),
				types.ProblemCritical,
				fmt.Sprintf("Concurrent problem %d", id),
			)
			if err := registry.Remediate(ctx, "concurrent-problem", *problem); err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Verify no errors occurred
	errorCount := 0
	for err := range errors {
		t.Logf("Remediation error: %v", err)
		errorCount++
	}

	test.AssertEqual(t, 0, errorCount, "Expected no errors from concurrent remediations")
	test.AssertEqual(t, numGoroutines, mockRemediator.GetAttemptCount(), "All remediations should succeed")
}

// TestConcurrentRemediationSameResource tests concurrent attempts on the same resource
func TestConcurrentRemediationSameResource(t *testing.T) {
	ctx := context.Background()

	mockRemediator := newThreadSafeRemediator("same-resource-test", 100*time.Millisecond)

	registry := remediators.NewRegistry(0, 100)
	registry.Register(remediators.RemediatorInfo{
		Type: "same-resource-problem",
		Factory: func() (types.Remediator, error) {
			return mockRemediator, nil
		},
		Description: "Same resource test remediator",
	})

	// Launch concurrent remediation attempts on the SAME resource
	const numGoroutines = 20
	var wg sync.WaitGroup
	successCount := 0
	failureCount := 0
	var mu sync.Mutex

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			problem := types.NewProblem(
				"same-resource-problem",
				"shared-resource", // Same resource for all
				types.ProblemCritical,
				"Concurrent problem on shared resource",
			)
			err := registry.Remediate(ctx, "same-resource-problem", *problem)
			mu.Lock()
			if err == nil {
				successCount++
			} else {
				failureCount++
			}
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	// Only first attempt should succeed, others blocked by cooldown or in-progress
	test.AssertTrue(t, successCount >= 1, "At least one remediation should succeed")
	test.AssertTrue(t, failureCount >= 1, "Some remediations should fail due to cooldown")
	t.Logf("Success: %d, Failures: %d", successCount, failureCount)
}

// TestConcurrentExporterOperations tests concurrent exports to multiple exporters
func TestConcurrentExporterOperations(t *testing.T) {
	ctx, cancel := test.TestContext(t, 30*time.Second)
	defer cancel()

	// Create multiple exporters
	const numExporters = 5
	exporters := make([]types.Exporter, numExporters)
	mockExporters := make([]*mockExporter, numExporters)

	for i := 0; i < numExporters; i++ {
		e := newMockExporter()
		exporters[i] = e
		mockExporters[i] = e
	}

	// Create monitor
	mockMonitor := newMockMonitor("test-monitor")

	config := &types.NodeDoctorConfig{
		APIVersion: "node-doctor.io/v1alpha1",
		Kind:       "NodeDoctorConfig",
		Metadata: types.ConfigMetadata{
			Name: "multi-exporter-test",
		},
		Settings: types.GlobalSettings{
			NodeName: "test-node",
		},
	}

	// Create detector with multiple exporters (no reload in tests)
	pd, err := detector.NewProblemDetector(config, []types.Monitor{mockMonitor}, exporters, "", nil)
	test.AssertNoError(t, err)

	// Start detector
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		pd.Run()
	}()

	time.Sleep(100 * time.Millisecond)

	// Send multiple statuses
	const numStatuses = 20
	for i := 0; i < numStatuses; i++ {
		status := &types.Status{
			Source:    "test-monitor",
			Timestamp: time.Now(),
			Events: []types.Event{
				{
					Severity:  types.EventInfo,
					Timestamp: time.Now(),
					Reason:    fmt.Sprintf("Update%d", i),
					Message:   fmt.Sprintf("Status update %d", i),
				},
			},
			Conditions: []types.Condition{},
		}
		mockMonitor.SendStatus(status)
		time.Sleep(20 * time.Millisecond)
	}

	// Wait for processing
	time.Sleep(1 * time.Second)

	// Verify all exporters received all statuses
	for i, exporter := range mockExporters {
		statuses := exporter.GetStatuses()
		test.AssertEqual(t, numStatuses, len(statuses),
			"Exporter %d should receive %d statuses", i, numStatuses)
	}

	cancel()
	wg.Wait()
}

// TestRaceConditionDetection tests for data races under concurrent load
func TestRaceConditionDetection(t *testing.T) {
	ctx, cancel := test.TestContext(t, 30*time.Second)
	defer cancel()

	// This test is designed to catch race conditions when run with -race flag

	// Create components
	monitor1 := newMockMonitor("monitor-1")
	monitor2 := newMockMonitor("monitor-2")
	exporter := newMockExporter()

	config := &types.NodeDoctorConfig{
		APIVersion: "node-doctor.io/v1alpha1",
		Kind:       "NodeDoctorConfig",
		Metadata: types.ConfigMetadata{
			Name: "race-test",
		},
		Settings: types.GlobalSettings{
			NodeName: "race-test-node",
		},
	}

	pd, err := detector.NewProblemDetector(
		config,
		[]types.Monitor{monitor1, monitor2},
		[]types.Exporter{exporter},
		"",  // no config file in tests
		nil, // no monitor factory in tests
	)
	test.AssertNoError(t, err)

	// Start detector
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		pd.Run()
	}()

	time.Sleep(100 * time.Millisecond)

	// Concurrent read/write operations
	var opsWg sync.WaitGroup

	// Monitor 1: continuous status sends
	opsWg.Add(1)
	go func() {
		defer opsWg.Done()
		for i := 0; i < 100; i++ {
			monitor1.SendStatus(&types.Status{
				Source:     "monitor-1",
				Timestamp:  time.Now(),
				Events:     []types.Event{},
				Conditions: []types.Condition{},
			})
			time.Sleep(10 * time.Millisecond)
		}
	}()

	// Monitor 2: continuous status sends
	opsWg.Add(1)
	go func() {
		defer opsWg.Done()
		for i := 0; i < 100; i++ {
			monitor2.SendStatus(&types.Status{
				Source:     "monitor-2",
				Timestamp:  time.Now(),
				Events:     []types.Event{},
				Conditions: []types.Condition{},
			})
			time.Sleep(10 * time.Millisecond)
		}
	}()

	// Concurrent exporter reads
	for i := 0; i < 10; i++ {
		opsWg.Add(1)
		go func() {
			defer opsWg.Done()
			for j := 0; j < 50; j++ {
				_ = exporter.GetStatuses()
				_ = exporter.GetProblems()
				time.Sleep(5 * time.Millisecond)
			}
		}()
	}

	opsWg.Wait()
	cancel()
	wg.Wait()

	// If we get here without race detector warnings, test passes
	t.Log("No race conditions detected")
}

// mockMonitor is a thread-safe mock Monitor implementation
type mockMonitor struct {
	name     string
	statusCh chan *types.Status
	started  bool
	mu       sync.Mutex
}

func newMockMonitor(name string) *mockMonitor {
	return &mockMonitor{
		name:     name,
		statusCh: make(chan *types.Status, 1000), // Large buffer for concurrent tests
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
		select {
		case m.statusCh <- status:
			// Sent successfully
		default:
			// Channel full, drop status
		}
	}
}

// mockExporter is a thread-safe mock Exporter implementation
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

// threadSafeRemediator is a thread-safe mock remediator
type threadSafeRemediator struct {
	*remediators.BaseRemediator
	mu           sync.Mutex
	attemptCount int
}

func newThreadSafeRemediator(name string, cooldown time.Duration) *threadSafeRemediator {
	r := &threadSafeRemediator{
		attemptCount: 0,
	}

	base, _ := remediators.NewBaseRemediator(name, cooldown)
	base.SetRemediateFunc(r.remediate)

	r.BaseRemediator = base
	return r
}

func (r *threadSafeRemediator) remediate(ctx context.Context, problem types.Problem) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attemptCount++
	// Simulate some work
	time.Sleep(10 * time.Millisecond)
	return nil
}

func (r *threadSafeRemediator) GetAttemptCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.attemptCount
}
