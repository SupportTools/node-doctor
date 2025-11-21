package detector

import (
	"context"
	"sync"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// MockMonitor implements types.Monitor interface for testing.
type MockMonitor struct {
	mu               sync.RWMutex
	name             string
	statusChan       chan *types.Status
	running          bool
	startError       error
	stopError        error
	statusUpdates    []*types.Status
	updateIndex      int
	sendContinuously bool
	sendInterval     time.Duration
	stopChan         chan struct{}
}

// NewMockMonitor creates a new MockMonitor with default settings.
func NewMockMonitor(name string) *MockMonitor {
	return &MockMonitor{
		name:          name,
		statusChan:    make(chan *types.Status, 1000),
		sendInterval:  100 * time.Millisecond,
		stopChan:      make(chan struct{}),
		statusUpdates: []*types.Status{},
	}
}

// SetStartError configures the monitor to return an error on Start().
func (m *MockMonitor) SetStartError(err error) *MockMonitor {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startError = err
	return m
}

// SetStopError configures the monitor to return an error on Stop().
func (m *MockMonitor) SetStopError(err error) *MockMonitor {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopError = err
	return m
}

// AddStatusUpdate adds a status update to be sent by the monitor.
func (m *MockMonitor) AddStatusUpdate(status *types.Status) *MockMonitor {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statusUpdates = append(m.statusUpdates, status)
	return m
}

// SetContinuousSending configures the monitor to send status updates continuously.
func (m *MockMonitor) SetContinuousSending(enabled bool, interval time.Duration) *MockMonitor {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sendContinuously = enabled
	if interval > 0 {
		m.sendInterval = interval
	}
	return m
}

// Start implements types.Monitor interface.
func (m *MockMonitor) Start() (<-chan *types.Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.startError != nil {
		return nil, m.startError
	}

	if m.running {
		return m.statusChan, nil
	}

	m.running = true

	// Start background goroutine to send status updates
	go m.sendStatusUpdates()

	return m.statusChan, nil
}

// Stop implements types.Monitor interface.
func (m *MockMonitor) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return
	}

	m.running = false
	close(m.stopChan)
}

// GetName returns the monitor name (helper for testing).
func (m *MockMonitor) GetName() string {
	return m.name
}

// IsRunning returns whether the monitor is currently running.
func (m *MockMonitor) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running
}

// sendStatusUpdates sends configured status updates.
func (m *MockMonitor) sendStatusUpdates() {
	ticker := time.NewTicker(m.sendInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopChan:
			return
		case <-ticker.C:
			m.mu.Lock()
			if !m.running {
				m.mu.Unlock()
				return
			}

			// Send next status update if available
			if m.updateIndex < len(m.statusUpdates) {
				status := m.statusUpdates[m.updateIndex]
				select {
				case m.statusChan <- status:
					m.updateIndex++
				default:
					// Channel full, skip
				}
			}

			// If continuous sending is disabled and we've sent all updates, stop
			if !m.sendContinuously && m.updateIndex >= len(m.statusUpdates) {
				m.mu.Unlock()
				return
			}

			m.mu.Unlock()
		}
	}
}

// MockExporter implements types.Exporter interface for testing.
type MockExporter struct {
	mu                 sync.RWMutex
	name               string
	statusExports      []*types.Status
	problemExports     []*types.Problem
	statusExportError  error
	problemExportError error
	exportDelay        time.Duration
}

// NewMockExporter creates a new MockExporter.
func NewMockExporter(name string) *MockExporter {
	return &MockExporter{
		name:           name,
		statusExports:  []*types.Status{},
		problemExports: []*types.Problem{},
	}
}

// SetStatusExportError configures the exporter to return an error on ExportStatus.
func (m *MockExporter) SetStatusExportError(err error) *MockExporter {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statusExportError = err
	return m
}

// SetProblemExportError configures the exporter to return an error on ExportProblem.
func (m *MockExporter) SetProblemExportError(err error) *MockExporter {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.problemExportError = err
	return m
}

// SetExportDelay configures a delay for export operations.
func (m *MockExporter) SetExportDelay(delay time.Duration) *MockExporter {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.exportDelay = delay
	return m
}

// GetName implements types.Exporter interface.
func (m *MockExporter) GetName() string {
	return m.name
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

// ClearExports clears all recorded exports.
func (m *MockExporter) ClearExports() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statusExports = []*types.Status{}
	m.problemExports = []*types.Problem{}
}

// GetExportCounts returns the count of exports for testing.
func (m *MockExporter) GetExportCounts() (statusCount, problemCount int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.statusExports), len(m.problemExports)
}

// MockMonitorFactory is a configurable mock implementation of MonitorFactory for testing.
type MockMonitorFactory struct {
	mu         sync.RWMutex
	createFunc func(config types.MonitorConfig) (types.Monitor, error)
	monitors   map[string]*MockMonitor
}

// NewMockMonitorFactory creates a new MockMonitorFactory.
func NewMockMonitorFactory() *MockMonitorFactory {
	return &MockMonitorFactory{
		monitors: make(map[string]*MockMonitor),
	}
}

// SetCreateFunc sets a custom create function for the factory.
func (m *MockMonitorFactory) SetCreateFunc(createFunc func(config types.MonitorConfig) (types.Monitor, error)) *MockMonitorFactory {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createFunc = createFunc
	return m
}

// AddMonitor pre-configures a monitor for a specific name.
func (m *MockMonitorFactory) AddMonitor(name string, monitor *MockMonitor) *MockMonitorFactory {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.monitors[name] = monitor
	return m
}

// CreateMonitor implements MonitorFactory interface.
func (m *MockMonitorFactory) CreateMonitor(config types.MonitorConfig) (types.Monitor, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Use custom create function if set
	if m.createFunc != nil {
		return m.createFunc(config)
	}

	// Check for pre-configured monitor
	if monitor, exists := m.monitors[config.Name]; exists {
		return monitor, nil
	}

	// Return a new mock monitor by default
	return NewMockMonitor(config.Name), nil
}

// TestHelper provides utilities for creating test objects.
type TestHelper struct{}

// NewTestHelper creates a new TestHelper.
func NewTestHelper() *TestHelper {
	return &TestHelper{}
}

// CreateTestConfig creates a minimal test configuration with valid defaults.
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
		Monitors: []types.MonitorConfig{
			{
				Name:     "test-monitor",
				Type:     "test",
				Enabled:  true,
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
			},
		},
	}

	// Apply defaults to ensure valid configuration
	config.ApplyDefaults()
	return config
}

// CreateTestMonitorConfig creates a monitor config with valid defaults.
func (h *TestHelper) CreateTestMonitorConfig(name, monitorType string) types.MonitorConfig {
	return types.MonitorConfig{
		Name:     name,
		Type:     monitorType,
		Enabled:  true,
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
	}
}

// CreateTestStatus creates a status with test data.
func (h *TestHelper) CreateTestStatus(source string) *types.Status {
	status := types.NewStatus(source)
	status.AddEvent(types.NewEvent(types.EventInfo, "TestReason", "Test message"))
	status.AddCondition(types.NewCondition("TestCondition", types.ConditionTrue, "TestReason", "Test condition"))
	return status
}

// CreateTestProblem creates a problem with test data.
func (h *TestHelper) CreateTestProblem(problemType, source string, severity types.ProblemSeverity) *types.Problem {
	return types.NewProblem(problemType, source, severity, "Test problem message")
}
