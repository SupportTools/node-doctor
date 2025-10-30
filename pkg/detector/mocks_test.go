// Package detector provides test mocks for the Problem Detector.
package detector

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// MockMonitor is a configurable mock implementation of types.Monitor for testing.
type MockMonitor struct {
	mu            sync.RWMutex
	name          string
	statusChan    chan *types.Status
	started       bool
	stopped       bool
	startError    error
	statusUpdates []*types.Status
	sendInterval  time.Duration
	stopChan      chan struct{}
	wg            sync.WaitGroup
}

// NewMockMonitor creates a new MockMonitor with the given name.
func NewMockMonitor(name string) *MockMonitor {
	return &MockMonitor{
		name:         name,
		statusChan:   make(chan *types.Status, 10),
		sendInterval: 100 * time.Millisecond, // Default send interval
		stopChan:     make(chan struct{}),
	}
}

// SetStartError configures the monitor to return an error on Start().
func (m *MockMonitor) SetStartError(err error) *MockMonitor {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startError = err
	return m
}

// SetSendInterval configures how frequently the monitor sends status updates.
func (m *MockMonitor) SetSendInterval(interval time.Duration) *MockMonitor {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sendInterval = interval
	return m
}

// AddStatusUpdate adds a status update to be sent by this monitor.
func (m *MockMonitor) AddStatusUpdate(status *types.Status) *MockMonitor {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statusUpdates = append(m.statusUpdates, status)
	return m
}

// Start implements types.Monitor interface.
func (m *MockMonitor) Start() (<-chan *types.Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.startError != nil {
		return nil, m.startError
	}

	if m.started {
		return nil, fmt.Errorf("monitor %s already started", m.name)
	}

	m.started = true
	m.stopped = false
	m.stopChan = make(chan struct{})

	// Start sending status updates in background
	m.wg.Add(1)
	go m.sendStatusUpdates()

	return m.statusChan, nil
}

// Stop implements types.Monitor interface.
func (m *MockMonitor) Stop() {
	m.mu.Lock()
	if m.stopped || !m.started {
		m.mu.Unlock()
		return
	}
	m.stopped = true
	close(m.stopChan)
	m.mu.Unlock()

	// Wait for sending goroutine to finish
	m.wg.Wait()

	m.mu.Lock()
	close(m.statusChan)
	m.mu.Unlock()
}

// sendStatusUpdates sends configured status updates at the specified interval.
func (m *MockMonitor) sendStatusUpdates() {
	defer m.wg.Done()

	m.mu.RLock()
	updates := make([]*types.Status, len(m.statusUpdates))
	copy(updates, m.statusUpdates)
	interval := m.sendInterval
	m.mu.RUnlock()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	updateIndex := 0

	for {
		select {
		case <-m.stopChan:
			return
		case <-ticker.C:
			if updateIndex < len(updates) {
				select {
				case m.statusChan <- updates[updateIndex]:
					updateIndex++
				case <-m.stopChan:
					return
				}
			}
		}
	}
}

// IsStarted returns whether the monitor has been started.
func (m *MockMonitor) IsStarted() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.started
}

// IsStopped returns whether the monitor has been stopped.
func (m *MockMonitor) IsStopped() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.stopped
}

// GetName returns the monitor's name.
func (m *MockMonitor) GetName() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.name
}

// MockExporter is a configurable mock implementation of types.Exporter for testing.
type MockExporter struct {
	mu                sync.RWMutex
	name              string
	statusExports     []*types.Status
	problemExports    []*types.Problem
	statusExportError error
	problemExportError error
	exportDelay       time.Duration
}

// NewMockExporter creates a new MockExporter with the given name.
func NewMockExporter(name string) *MockExporter {
	return &MockExporter{
		name: name,
	}
}

// SetStatusExportError configures the exporter to return an error on ExportStatus().
func (m *MockExporter) SetStatusExportError(err error) *MockExporter {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statusExportError = err
	return m
}

// SetProblemExportError configures the exporter to return an error on ExportProblem().
func (m *MockExporter) SetProblemExportError(err error) *MockExporter {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.problemExportError = err
	return m
}

// SetExportDelay configures a delay for all export operations.
func (m *MockExporter) SetExportDelay(delay time.Duration) *MockExporter {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.exportDelay = delay
	return m
}

// ExportStatus implements types.Exporter interface.
func (m *MockExporter) ExportStatus(ctx context.Context, status *types.Status) error {
	m.mu.Lock()
	err := m.statusExportError
	delay := m.exportDelay
	m.statusExports = append(m.statusExports, status)
	m.mu.Unlock()

	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return err
}

// ExportProblem implements types.Exporter interface.
func (m *MockExporter) ExportProblem(ctx context.Context, problem *types.Problem) error {
	m.mu.Lock()
	err := m.problemExportError
	delay := m.exportDelay
	m.problemExports = append(m.problemExports, problem)
	m.mu.Unlock()

	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return err
}

// GetStatusExports returns all status exports received by this exporter.
func (m *MockExporter) GetStatusExports() []*types.Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*types.Status, len(m.statusExports))
	copy(result, m.statusExports)
	return result
}

// GetProblemExports returns all problem exports received by this exporter.
func (m *MockExporter) GetProblemExports() []*types.Problem {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*types.Problem, len(m.problemExports))
	copy(result, m.problemExports)
	return result
}

// GetStatusExportCount returns the number of status exports received.
func (m *MockExporter) GetStatusExportCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.statusExports)
}

// GetProblemExportCount returns the number of problem exports received.
func (m *MockExporter) GetProblemExportCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.problemExports)
}

// Reset clears all recorded exports.
func (m *MockExporter) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statusExports = nil
	m.problemExports = nil
}

// GetName returns the exporter's name.
func (m *MockExporter) GetName() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.name
}

// TestHelper provides utility functions for creating test data.
type TestHelper struct{}

// NewTestHelper creates a new TestHelper instance.
func NewTestHelper() *TestHelper {
	return &TestHelper{}
}

// CreateTestStatus creates a status with test events and conditions.
func (h *TestHelper) CreateTestStatus(source string) *types.Status {
	status := types.NewStatus(source)

	// Add test events
	status.AddEvent(types.NewEvent(types.EventError, "TestError", "Test error message"))
	status.AddEvent(types.NewEvent(types.EventWarning, "TestWarning", "Test warning message"))
	status.AddEvent(types.NewEvent(types.EventInfo, "TestInfo", "Test info message"))

	// Add test conditions
	status.AddCondition(types.NewCondition("TestCondition", types.ConditionFalse, "TestReason", "Test condition failed"))
	status.AddCondition(types.NewCondition("HealthyCondition", types.ConditionTrue, "TestReason", "Test condition passed"))

	return status
}

// CreateErrorStatus creates a status with only error events and false conditions.
func (h *TestHelper) CreateErrorStatus(source string) *types.Status {
	status := types.NewStatus(source)
	status.AddEvent(types.NewEvent(types.EventError, "CriticalError", "Critical system failure"))
	status.AddCondition(types.NewCondition("SystemHealth", types.ConditionFalse, "SystemFailure", "System is unhealthy"))
	return status
}

// CreateWarningStatus creates a status with only warning events.
func (h *TestHelper) CreateWarningStatus(source string) *types.Status {
	status := types.NewStatus(source)
	status.AddEvent(types.NewEvent(types.EventWarning, "PerformanceWarning", "System performance degraded"))
	return status
}

// CreateHealthyStatus creates a status with only healthy conditions and info events.
func (h *TestHelper) CreateHealthyStatus(source string) *types.Status {
	status := types.NewStatus(source)
	status.AddEvent(types.NewEvent(types.EventInfo, "HealthCheck", "System is healthy"))
	status.AddCondition(types.NewCondition("SystemHealth", types.ConditionTrue, "HealthCheck", "System is operating normally"))
	return status
}

// CreateEmptyStatus creates a status with no events or conditions.
func (h *TestHelper) CreateEmptyStatus(source string) *types.Status {
	return types.NewStatus(source)
}

// CreateTestConfig creates a minimal test configuration.
func (h *TestHelper) CreateTestConfig() *types.NodeDoctorConfig {
	config := &types.NodeDoctorConfig{
		APIVersion: "v1",
		Kind:       "NodeDoctorConfig",
		Metadata: types.ConfigMetadata{
			Name: "test-config",
		},
		Settings: types.GlobalSettings{
			NodeName: "test-node",
		},
	}

	// Apply defaults to ensure valid configuration
	config.ApplyDefaults()
	return config
}