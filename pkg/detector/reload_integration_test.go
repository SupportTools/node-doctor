package detector

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/reload"
	"github.com/supporttools/node-doctor/pkg/types"
	"gopkg.in/yaml.v2"
)

// ReloadTestHelper provides utilities for integration testing config reload functionality
type ReloadTestHelper struct {
	configFile    string
	detector      *ProblemDetector
	factory       *MockMonitorFactory
	events        []types.Event
	eventsMu      sync.Mutex
	tempDir       string
	cleanupFunc   func()
}

// NewReloadTestHelper creates a new test helper for reload integration tests
func NewReloadTestHelper(t *testing.T) *ReloadTestHelper {
	tempDir, err := ioutil.TempDir("", "reload-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	configFile := filepath.Join(tempDir, "config.yaml")

	return &ReloadTestHelper{
		configFile: configFile,
		tempDir:    tempDir,
		cleanupFunc: func() {
			os.RemoveAll(tempDir)
		},
		events: make([]types.Event, 0),
	}
}

// Setup initializes the detector with the given configuration
func (h *ReloadTestHelper) Setup(t *testing.T, initialConfig *types.NodeDoctorConfig) {
	// Write initial config to file
	h.UpdateConfig(t, initialConfig)

	// Create mock factory
	h.factory = NewMockMonitorFactory()

	// Create detector
	detector, err := NewProblemDetector(
		initialConfig,
		[]types.Monitor{},
		[]types.Exporter{NewMockExporter("test-exporter")},
		h.configFile,
		h.factory,
	)
	if err != nil {
		t.Fatalf("Failed to create problem detector: %v", err)
	}

	h.detector = detector

	// Start capturing events by replacing the event emitter
	h.detector.reloadCoordinator = reload.NewReloadCoordinator(
		h.configFile,
		initialConfig,
		h.detector.handleConfigReload,
		h.captureEvent,
	)
}

// UpdateConfig writes a new configuration to the config file
func (h *ReloadTestHelper) UpdateConfig(t *testing.T, newConfig *types.NodeDoctorConfig) {
	data, err := yaml.Marshal(newConfig)
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	// Write atomically like Kubernetes ConfigMap
	tmpFile := h.configFile + ".tmp"
	if err := ioutil.WriteFile(tmpFile, data, 0644); err != nil {
		t.Fatalf("Failed to write temp config: %v", err)
	}

	if err := os.Rename(tmpFile, h.configFile); err != nil {
		t.Fatalf("Failed to rename config: %v", err)
	}
}

// TriggerReload manually triggers a configuration reload
func (h *ReloadTestHelper) TriggerReload(t *testing.T, ctx context.Context) error {
	return h.detector.reloadCoordinator.TriggerReload(ctx)
}

// WaitForReload waits for a reload to complete within the timeout
func (h *ReloadTestHelper) WaitForReload(t *testing.T, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		h.eventsMu.Lock()
		for _, event := range h.events {
			if event.Reason == "ConfigReloadSucceeded" || event.Reason == "ConfigReloadFailed" {
				h.eventsMu.Unlock()
				if event.Reason == "ConfigReloadFailed" {
					return fmt.Errorf("reload failed: %s", event.Message)
				}
				return nil
			}
		}
		h.eventsMu.Unlock()

		time.Sleep(50 * time.Millisecond)
	}

	return fmt.Errorf("reload did not complete within %v", timeout)
}

// GetMonitorNames returns the names of all currently running monitors
func (h *ReloadTestHelper) GetMonitorNames() []string {
	h.detector.mu.RLock()
	defer h.detector.mu.RUnlock()

	names := make([]string, len(h.detector.monitorHandles))
	for i, handle := range h.detector.monitorHandles {
		names[i] = handle.GetName()
	}
	return names
}

// GetEvents returns all captured reload events
func (h *ReloadTestHelper) GetEvents() []types.Event {
	h.eventsMu.Lock()
	defer h.eventsMu.Unlock()

	events := make([]types.Event, len(h.events))
	copy(events, h.events)
	return events
}

// HasEvent checks if an event with the given reason was captured
func (h *ReloadTestHelper) HasEvent(reason string) bool {
	h.eventsMu.Lock()
	defer h.eventsMu.Unlock()

	for _, event := range h.events {
		if event.Reason == reason {
			return true
		}
	}
	return false
}

// ClearEvents clears all captured events
func (h *ReloadTestHelper) ClearEvents() {
	h.eventsMu.Lock()
	defer h.eventsMu.Unlock()
	h.events = h.events[:0]
}

// Cleanup cleans up resources
func (h *ReloadTestHelper) Cleanup() {
	if h.detector != nil {
		h.detector.Stop()
	}
	if h.cleanupFunc != nil {
		h.cleanupFunc()
	}
}

// captureEvent captures reload events for testing
func (h *ReloadTestHelper) captureEvent(severity types.EventSeverity, reason, message string) {
	h.eventsMu.Lock()
	defer h.eventsMu.Unlock()

	h.events = append(h.events, types.Event{
		Severity:  severity,
		Reason:    reason,
		Message:   message,
		Timestamp: time.Now(),
	})
}

// CreateTestConfig creates a test configuration with the specified monitors
func CreateTestConfigWithMonitors(monitors []types.MonitorConfig) *types.NodeDoctorConfig {
	config := &types.NodeDoctorConfig{
		APIVersion: "v1",
		Kind:       "NodeDoctorConfig",
		Metadata: types.ConfigMetadata{
			Name: "integration-test-config",
		},
		Settings: types.GlobalSettings{
			NodeName: "test-node",
		},
		Monitors: monitors,
		Exporters: types.ExporterConfigs{
			HTTP: &types.HTTPExporterConfig{
				Enabled: true,
				Webhooks: []types.WebhookEndpoint{
					{
						Name: "default",
						URL:  "http://localhost:8080/webhook",
						SendStatus: true,
						SendProblems: true,
					},
				},
				TimeoutString: "10s",
			},
		},
		Reload: types.ReloadConfig{
			Enabled:          true,
			DebounceInterval: 100 * time.Millisecond,
		},
	}

	// Apply defaults
	config.ApplyDefaults()
	return config
}

// TestConfigReloadEndToEnd tests the complete reload workflow from file change to monitor restart
func TestConfigReloadEndToEnd(t *testing.T) {
	helper := NewReloadTestHelper(t)
	defer helper.Cleanup()

	// Setup initial config with 2 monitors
	initialMonitors := []types.MonitorConfig{
		{Name: "monitor-1", Type: "kubelet", Enabled: true, Interval: 30 * time.Second, Timeout: 10 * time.Second},
		{Name: "monitor-2", Type: "kubelet", Enabled: true, Interval: 30 * time.Second, Timeout: 10 * time.Second},
	}
	initialConfig := CreateTestConfigWithMonitors(initialMonitors)

	helper.Setup(t, initialConfig)

	// Start detector
	err := helper.detector.Start()
	if err != nil {
		t.Fatalf("Failed to start detector: %v", err)
	}

	// Wait for monitors to start
	time.Sleep(200 * time.Millisecond)

	// Verify initial state
	monitorNames := helper.GetMonitorNames()
	if len(monitorNames) != 2 {
		t.Errorf("Expected 2 monitors, got %d", len(monitorNames))
	}

	// Update config: add 1 monitor, remove 1 monitor, modify 1 monitor
	newMonitors := []types.MonitorConfig{
		{Name: "monitor-2", Type: "kubelet", Enabled: true, Interval: 60 * time.Second, Timeout: 10 * time.Second}, // Modified interval
		{Name: "monitor-3", Type: "kubelet", Enabled: true, Interval: 30 * time.Second, Timeout: 10 * time.Second}, // Added
		// monitor-1 is removed
	}
	newConfig := CreateTestConfigWithMonitors(newMonitors)

	helper.ClearEvents()
	helper.UpdateConfig(t, newConfig)

	// Trigger reload
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = helper.TriggerReload(t, ctx)
	if err != nil {
		t.Fatalf("Failed to trigger reload: %v", err)
	}

	// Wait for reload to complete
	err = helper.WaitForReload(t, 3*time.Second)
	if err != nil {
		t.Fatalf("Reload did not complete: %v", err)
	}

	// Verify final state
	finalMonitorNames := helper.GetMonitorNames()
	if len(finalMonitorNames) != 2 {
		t.Errorf("Expected 2 monitors after reload, got %d: %v", len(finalMonitorNames), finalMonitorNames)
	}

	// Check that we have the right monitors
	expectedNames := []string{"monitor-2", "monitor-3"}
	for _, expected := range expectedNames {
		found := false
		for _, actual := range finalMonitorNames {
			if actual == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected monitor %s not found in %v", expected, finalMonitorNames)
		}
	}

	// Verify events were emitted
	if !helper.HasEvent("ConfigReloadStarted") {
		t.Error("ConfigReloadStarted event not emitted")
	}
	if !helper.HasEvent("ConfigReloadSucceeded") {
		t.Error("ConfigReloadSucceeded event not emitted")
	}

	// Verify config was updated
	helper.detector.mu.RLock()
	currentConfig := helper.detector.config
	helper.detector.mu.RUnlock()

	if len(currentConfig.Monitors) != 2 {
		t.Errorf("Config not updated: expected 2 monitors, got %d", len(currentConfig.Monitors))
	}
}

// TestConfigReloadWithValidationFailure tests that invalid config is rejected and system continues with old config
func TestConfigReloadWithValidationFailure(t *testing.T) {
	helper := NewReloadTestHelper(t)
	defer helper.Cleanup()

	// Setup initial valid config
	initialMonitors := []types.MonitorConfig{
		{Name: "monitor-1", Type: "kubelet", Enabled: true, Interval: 30 * time.Second, Timeout: 10 * time.Second},
	}
	initialConfig := CreateTestConfigWithMonitors(initialMonitors)

	helper.Setup(t, initialConfig)

	err := helper.detector.Start()
	if err != nil {
		t.Fatalf("Failed to start detector: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	// Verify initial state
	if len(helper.GetMonitorNames()) != 1 {
		t.Errorf("Expected 1 monitor initially")
	}

	// Create invalid config by writing malformed YAML directly to the file
	invalidYAML := `
apiVersion: v1
kind: NodeDoctorConfig
metadata:
  name: invalid-config
monitors:
  - name: "missing-type"
    enabled: true
    interval: "30s"
  - name: "duplicate"
    type: "kubelet"
    enabled: true
    interval: "30s"
  - name: "duplicate"
    type: "kubelet"
    enabled: true
    interval: "30s"
exporters:
  http:
    enabled: true
    webhooks:
      - name: "default"
        url: "http://localhost:8080/webhook"
`

	// Write invalid config directly
	if err := ioutil.WriteFile(helper.configFile, []byte(invalidYAML), 0644); err != nil {
		t.Fatalf("Failed to write invalid config: %v", err)
	}

	helper.ClearEvents()

	// Trigger reload - should fail
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = helper.TriggerReload(t, ctx)
	if err == nil {
		t.Error("Expected reload to fail with validation error")
	}

	// Verify old config is still in use
	monitorNames := helper.GetMonitorNames()
	if len(monitorNames) != 1 || monitorNames[0] != "monitor-1" {
		t.Errorf("Old config not preserved: got monitors %v", monitorNames)
	}

	// Check that a validation failed event was captured (could be ConfigReloadFailed or ConfigValidationFailed)
	allEvents := helper.GetEvents()
	validationFailed := false
	for _, event := range allEvents {
		if event.Reason == "ConfigValidationFailed" || event.Reason == "ConfigReloadFailed" {
			validationFailed = true
			break
		}
	}
	if !validationFailed {
		t.Errorf("Neither ConfigValidationFailed nor ConfigReloadFailed event captured. Events: %v", allEvents)
	}
}

// TestConfigReloadConcurrentSafety tests that multiple concurrent reload attempts don't cause races
func TestConfigReloadConcurrentSafety(t *testing.T) {
	helper := NewReloadTestHelper(t)
	defer helper.Cleanup()

	// Setup initial config
	initialMonitors := []types.MonitorConfig{
		{Name: "monitor-1", Type: "kubelet", Enabled: true, Interval: 30 * time.Second, Timeout: 10 * time.Second},
	}
	initialConfig := CreateTestConfigWithMonitors(initialMonitors)

	helper.Setup(t, initialConfig)

	err := helper.detector.Start()
	if err != nil {
		t.Fatalf("Failed to start detector: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	// Create new valid config
	newMonitors := []types.MonitorConfig{
		{Name: "monitor-2", Type: "kubelet", Enabled: true, Interval: 30 * time.Second, Timeout: 10 * time.Second},
	}
	newConfig := CreateTestConfigWithMonitors(newMonitors)
	helper.UpdateConfig(t, newConfig)

	// Trigger 10 concurrent reload attempts
	const numGoroutines = 10
	var wg sync.WaitGroup
	errors := make([]error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			errors[index] = helper.TriggerReload(t, ctx)
		}(i)
	}

	wg.Wait()

	// Only one reload should succeed, others should fail with "already in progress"
	successCount := 0
	alreadyInProgressCount := 0

	for _, err := range errors {
		if err == nil {
			successCount++
		} else if err.Error() == "reload already in progress" {
			alreadyInProgressCount++
		} else {
			t.Errorf("Unexpected error: %v", err)
		}
	}

	if successCount != 1 {
		t.Errorf("Expected exactly 1 successful reload, got %d", successCount)
	}

	if successCount+alreadyInProgressCount != numGoroutines {
		t.Errorf("Expected all attempts to be accounted for: success=%d, in-progress=%d, total=%d",
			successCount, alreadyInProgressCount, numGoroutines)
	}

	// Final state should be consistent
	time.Sleep(100 * time.Millisecond)
	monitorNames := helper.GetMonitorNames()
	if len(monitorNames) != 1 || monitorNames[0] != "monitor-2" {
		t.Errorf("Final state inconsistent: got monitors %v", monitorNames)
	}
}

// TestConfigReloadExporterUpdates tests that exporter configuration changes are applied
func TestConfigReloadExporterUpdates(t *testing.T) {
	helper := NewReloadTestHelper(t)
	defer helper.Cleanup()

	// Setup initial config with HTTP exporter
	initialMonitors := []types.MonitorConfig{
		{Name: "monitor-1", Type: "kubelet", Enabled: true, Interval: 30 * time.Second, Timeout: 10 * time.Second},
	}
	initialConfig := CreateTestConfigWithMonitors(initialMonitors)
	initialConfig.Exporters.HTTP.Webhooks[0].URL = "http://localhost:8080/webhook1"

	// Create reloadable mock exporter
	mockExporter := NewMockReloadableExporter("http-exporter")
	helper.Setup(t, initialConfig)

	// Replace exporter with reloadable one
	helper.detector.exporters = []types.Exporter{mockExporter}

	err := helper.detector.Start()
	if err != nil {
		t.Fatalf("Failed to start detector: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	// Update exporter config - change webhook URL and mark exporters as changed
	newConfig := CreateTestConfigWithMonitors(initialMonitors)
	newConfig.Exporters.HTTP.Webhooks[0].URL = "http://localhost:8080/webhook2"

	helper.ClearEvents()
	helper.UpdateConfig(t, newConfig)

	// Trigger reload
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = helper.TriggerReload(t, ctx)
	if err != nil {
		t.Fatalf("Failed to trigger reload: %v", err)
	}

	// Wait for reload to complete
	err = helper.WaitForReload(t, 3*time.Second)
	if err != nil {
		t.Fatalf("Reload did not complete: %v", err)
	}

	// Verify exporter was reloaded
	if mockExporter.GetReloadCount() == 0 {
		t.Logf("Exporter reload count: %d", mockExporter.GetReloadCount())
		t.Logf("All events: %v", helper.GetEvents())
		t.Error("Exporter reload was not called")
	}

	if mockExporter.GetLastConfig() == nil {
		t.Error("Exporter did not receive config")
	} else if httpConfig, ok := mockExporter.GetLastConfig().(*types.HTTPExporterConfig); ok {
		if len(httpConfig.Webhooks) == 0 || httpConfig.Webhooks[0].URL != "http://localhost:8080/webhook2" {
			t.Errorf("Exporter config not updated: got %v", httpConfig.Webhooks)
		}
	} else {
		t.Error("Exporter received wrong config type")
	}
}

// TestConfigReloadMonitorLifecycle tests that monitor handles are properly managed during reload
func TestConfigReloadMonitorLifecycle(t *testing.T) {
	helper := NewReloadTestHelper(t)
	defer helper.Cleanup()

	// Setup initial config with 3 monitors
	initialMonitors := []types.MonitorConfig{
		{Name: "keep-monitor", Type: "kubelet", Enabled: true, Interval: 30 * time.Second, Timeout: 10 * time.Second},
		{Name: "modify-monitor", Type: "kubelet", Enabled: true, Interval: 30 * time.Second, Timeout: 10 * time.Second},
		{Name: "remove-monitor", Type: "kubelet", Enabled: true, Interval: 30 * time.Second, Timeout: 10 * time.Second},
	}
	initialConfig := CreateTestConfigWithMonitors(initialMonitors)

	helper.Setup(t, initialConfig)

	err := helper.detector.Start()
	if err != nil {
		t.Fatalf("Failed to start detector: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	// Verify initial state
	if len(helper.GetMonitorNames()) != 3 {
		t.Errorf("Expected 3 monitors initially")
	}

	// Update config: remove 1, modify 1, add 1
	newMonitors := []types.MonitorConfig{
		{Name: "keep-monitor", Type: "kubelet", Enabled: true, Interval: 30 * time.Second, Timeout: 10 * time.Second},
		{Name: "modify-monitor", Type: "kubelet", Enabled: true, Interval: 60 * time.Second, Timeout: 10 * time.Second}, // Modified
		{Name: "new-monitor", Type: "kubelet", Enabled: true, Interval: 30 * time.Second, Timeout: 10 * time.Second},    // Added
		// remove-monitor is removed
	}
	newConfig := CreateTestConfigWithMonitors(newMonitors)

	helper.ClearEvents()
	helper.UpdateConfig(t, newConfig)

	// Trigger reload
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = helper.TriggerReload(t, ctx)
	if err != nil {
		t.Fatalf("Failed to trigger reload: %v", err)
	}

	// Wait for reload to complete
	err = helper.WaitForReload(t, 3*time.Second)
	if err != nil {
		t.Fatalf("Reload did not complete: %v", err)
	}

	// Verify final state
	finalMonitorNames := helper.GetMonitorNames()
	if len(finalMonitorNames) != 3 {
		t.Errorf("Expected 3 monitors after reload, got %d: %v", len(finalMonitorNames), finalMonitorNames)
	}

	// Check that we have the right monitors
	expectedNames := []string{"keep-monitor", "modify-monitor", "new-monitor"}
	for _, expected := range expectedNames {
		found := false
		for _, actual := range finalMonitorNames {
			if actual == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected monitor %s not found in %v", expected, finalMonitorNames)
		}
	}

	// Ensure removed monitor is not present
	for _, name := range finalMonitorNames {
		if name == "remove-monitor" {
			t.Error("Removed monitor still present after reload")
		}
	}
}

// TestConfigReloadPartialFailureRecovery tests that critical errors during reload prevent config update
func TestConfigReloadPartialFailureRecovery(t *testing.T) {
	helper := NewReloadTestHelper(t)
	defer helper.Cleanup()

	// Setup initial config
	initialMonitors := []types.MonitorConfig{
		{Name: "working-monitor", Type: "kubelet", Enabled: true, Interval: 30 * time.Second, Timeout: 10 * time.Second},
	}
	initialConfig := CreateTestConfigWithMonitors(initialMonitors)

	helper.Setup(t, initialConfig)

	// Configure factory to fail for specific monitor type
	helper.factory.SetCreateFunc(func(config types.MonitorConfig) (types.Monitor, error) {
		if config.Name == "failing-monitor" {
			return nil, fmt.Errorf("simulated monitor creation failure")
		}
		return NewMockMonitor(config.Name), nil
	})

	err := helper.detector.Start()
	if err != nil {
		t.Fatalf("Failed to start detector: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	// Verify initial state
	if len(helper.GetMonitorNames()) != 1 {
		t.Errorf("Expected 1 monitor initially")
	}

	// Store reference to original config
	helper.detector.mu.RLock()
	originalConfig := helper.detector.config
	helper.detector.mu.RUnlock()

	// Update config to add a monitor that will fail to create
	newMonitors := []types.MonitorConfig{
		{Name: "working-monitor", Type: "kubelet", Enabled: true, Interval: 30 * time.Second, Timeout: 10 * time.Second},
		{Name: "failing-monitor", Type: "kubelet", Enabled: true, Interval: 30 * time.Second, Timeout: 10 * time.Second},
	}
	newConfig := CreateTestConfigWithMonitors(newMonitors)

	helper.ClearEvents()
	helper.UpdateConfig(t, newConfig)

	// Trigger reload
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = helper.TriggerReload(t, ctx)
	if err == nil {
		t.Error("Expected reload to fail due to critical error")
	}

	// Verify old config is still in use
	helper.detector.mu.RLock()
	currentConfig := helper.detector.config
	helper.detector.mu.RUnlock()

	if currentConfig != originalConfig {
		t.Error("Config was updated despite critical failure")
	}

	// Verify existing monitors are still running
	monitorNames := helper.GetMonitorNames()
	if len(monitorNames) != 1 || monitorNames[0] != "working-monitor" {
		t.Errorf("Existing monitors not preserved: got %v", monitorNames)
	}

	// Check for failure events
	allEvents := helper.GetEvents()
	failureEventFound := false
	for _, event := range allEvents {
		if event.Reason == "ConfigReloadFailed" || event.Reason == "ReloadFailed" {
			failureEventFound = true
			break
		}
	}
	if !failureEventFound {
		t.Errorf("No failure event captured. Events: %v", allEvents)
	}
}

// MockReloadableExporter is a mock exporter that implements ReloadableExporter interface
type MockReloadableExporter struct {
	*MockExporter
	reloadable  bool
	reloadCount int
	lastConfig  interface{}
	mu          sync.RWMutex
}

// NewMockReloadableExporter creates a new mock reloadable exporter
func NewMockReloadableExporter(name string) *MockReloadableExporter {
	return &MockReloadableExporter{
		MockExporter: NewMockExporter(name),
		reloadable:   true,
	}
}

// Reload implements ReloadableExporter interface
func (m *MockReloadableExporter) Reload(config interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.reloadCount++
	m.lastConfig = config
	return nil
}

// IsReloadable implements ReloadableExporter interface
func (m *MockReloadableExporter) IsReloadable() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.reloadable
}

// SetReloadable sets whether this exporter is reloadable
func (m *MockReloadableExporter) SetReloadable(reloadable bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reloadable = reloadable
}

// GetReloadCount returns the number of times Reload was called
func (m *MockReloadableExporter) GetReloadCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.reloadCount
}

// GetLastConfig returns the last config passed to Reload
func (m *MockReloadableExporter) GetLastConfig() interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastConfig
}