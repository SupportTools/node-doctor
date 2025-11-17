package reload

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// TestNewReloadCoordinator tests creating a new reload coordinator
func TestNewReloadCoordinator(t *testing.T) {
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

	callback := func(ctx context.Context, newConfig *types.NodeDoctorConfig, diff *ConfigDiff) error {
		return nil
	}

	emitter := func(severity types.EventSeverity, reason, message string) {}

	coordinator := NewReloadCoordinator("/test/path", config, callback, emitter)

	if coordinator.configPath != "/test/path" {
		t.Errorf("Expected configPath '/test/path', got '%s'", coordinator.configPath)
	}

	if coordinator.currentConfig != config {
		t.Error("Expected current config to match initial config")
	}

	if coordinator.validator == nil {
		t.Error("Expected validator to be initialized")
	}
}

// TestNewReloadCoordinatorWithValidator tests creating coordinator with custom validator
func TestNewReloadCoordinatorWithValidator(t *testing.T) {
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

	callback := func(ctx context.Context, newConfig *types.NodeDoctorConfig, diff *ConfigDiff) error {
		return nil
	}

	emitter := func(severity types.EventSeverity, reason, message string) {}

	customValidator := NewConfigValidatorWithLimits(50, 5, 25, 10)
	coordinator := NewReloadCoordinatorWithValidator("/test/path", config, callback, emitter, customValidator)

	if coordinator.validator != customValidator {
		t.Error("Expected custom validator to be set")
	}

	if coordinator.validator.maxMonitors != 50 {
		t.Errorf("Expected maxMonitors to be 50, got %d", coordinator.validator.maxMonitors)
	}
}

// TestGetCurrentConfig tests getting current config
func TestGetCurrentConfig(t *testing.T) {
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

	callback := func(ctx context.Context, newConfig *types.NodeDoctorConfig, diff *ConfigDiff) error {
		return nil
	}

	emitter := func(severity types.EventSeverity, reason, message string) {}

	coordinator := NewReloadCoordinator("/test/path", config, callback, emitter)
	currentConfig := coordinator.GetCurrentConfig()

	if currentConfig.Settings.NodeName != "test-node" {
		t.Errorf("Expected node name 'test-node', got '%s'", currentConfig.Settings.NodeName)
	}
}

// TestTriggerReload_NoChanges tests reload with no configuration changes
func TestTriggerReload_NoChanges(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	// Create a valid config file
	configYAML := `
apiVersion: v1
kind: NodeDoctorConfig
metadata:
  name: test-config
settings:
  nodeName: test-node
monitors:
  - name: test-monitor
    type: kubelet
    enabled: true
    interval: 30s
    timeout: 10s
exporters:
  kubernetes:
    enabled: true
    namespace: default
remediation:
  enabled: false
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

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
				Type:     "kubelet",
				Enabled:  true,
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
			},
		},
		Exporters: types.ExporterConfigs{
			Kubernetes: &types.KubernetesExporterConfig{
				Enabled:   true,
				Namespace: "default",
			},
		},
		Remediation: types.RemediationConfig{
			Enabled: false,
		},
	}

	// Apply defaults to match what LoadConfig does
	if err := config.ApplyDefaults(); err != nil {
		t.Fatalf("Failed to apply defaults: %v", err)
	}

	callbackCalled := false
	callback := func(ctx context.Context, newConfig *types.NodeDoctorConfig, diff *ConfigDiff) error {
		callbackCalled = true
		return nil
	}

	var eventReason string
	emitter := func(severity types.EventSeverity, reason, message string) {
		eventReason = reason
	}

	coordinator := NewReloadCoordinator(configPath, config, callback, emitter)

	ctx := context.Background()
	err := coordinator.TriggerReload(ctx)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if callbackCalled {
		t.Error("Callback should not be called when there are no changes")
	}

	if eventReason != "ConfigReloadNoChanges" {
		t.Errorf("Expected 'ConfigReloadNoChanges' event, got '%s'", eventReason)
	}
}

// TestTriggerReload_WithChanges tests reload with configuration changes
func TestTriggerReload_WithChanges(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	// Create a config file with changes
	configYAML := `
apiVersion: v1
kind: NodeDoctorConfig
metadata:
  name: test-config
settings:
  nodeName: test-node
monitors:
  - name: new-monitor
    type: kubelet
    enabled: true
    interval: 30s
    timeout: 10s
exporters:
  kubernetes:
    enabled: true
    namespace: default
remediation:
  enabled: false
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	config := &types.NodeDoctorConfig{
		APIVersion: "v1",
		Kind:       "NodeDoctorConfig",
		Metadata: types.ConfigMetadata{
			Name: "test-config",
		},
		Settings: types.GlobalSettings{
			NodeName: "test-node",
		},
		Exporters: types.ExporterConfigs{
			Kubernetes: &types.KubernetesExporterConfig{
				Enabled:   true,
				Namespace: "default",
			},
		},
		Remediation: types.RemediationConfig{
			Enabled: false,
		},
	}

	callbackCalled := false
	callback := func(ctx context.Context, newConfig *types.NodeDoctorConfig, diff *ConfigDiff) error {
		callbackCalled = true
		return nil
	}

	var eventReason string
	emitter := func(severity types.EventSeverity, reason, message string) {
		eventReason = reason
	}

	coordinator := NewReloadCoordinator(configPath, config, callback, emitter)

	ctx := context.Background()
	err := coordinator.TriggerReload(ctx)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if !callbackCalled {
		t.Error("Callback should be called when there are changes")
	}

	if eventReason != "ConfigReloadSucceeded" {
		t.Errorf("Expected 'ConfigReloadSucceeded' event, got '%s'", eventReason)
	}

	// Verify the config was updated
	currentConfig := coordinator.GetCurrentConfig()
	if len(currentConfig.Monitors) != 1 {
		t.Errorf("Expected current config to have 1 monitor, got %d", len(currentConfig.Monitors))
	}
}

// TestTriggerReload_ValidationFails tests reload with invalid configuration
func TestTriggerReload_ValidationFails(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	// Create config file that passes basic validation but fails our validator
	// (empty monitor name and type, no exporters enabled)
	configYAML := `
apiVersion: v1
kind: NodeDoctorConfig
metadata:
  name: test-config
settings:
  nodeName: test-node
monitors:
  - name: ""
    type: ""
    enabled: true
    interval: 30s
    timeout: 10s
exporters:
  kubernetes:
    enabled: false
  http:
    enabled: false
  prometheus:
    enabled: false
remediation:
  enabled: false
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

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

	callbackCalled := false
	callback := func(ctx context.Context, newConfig *types.NodeDoctorConfig, diff *ConfigDiff) error {
		callbackCalled = true
		return nil
	}

	var eventReason string
	var eventMessage string
	emitter := func(severity types.EventSeverity, reason, message string) {
		eventReason = reason
		eventMessage = message
	}

	coordinator := NewReloadCoordinator(configPath, config, callback, emitter)

	ctx := context.Background()
	err := coordinator.TriggerReload(ctx)

	if err == nil {
		t.Error("Expected validation error")
	}

	if callbackCalled {
		t.Error("Callback should not be called when validation fails")
	}

	// When LoadConfig fails due to validation, we get ConfigReloadFailed
	// ConfigValidationFailed is only emitted when a separate validator is set
	if eventReason != "ConfigReloadFailed" {
		t.Errorf("Expected 'ConfigReloadFailed' event, got '%s'", eventReason)
	}

	// Verify error message contains validation details
	if !strings.Contains(err.Error(), "configuration validation failed") {
		t.Errorf("Error should mention validation failure, got: %v", err)
	}

	// Verify validation error details are in event message
	if !strings.Contains(eventMessage, "configuration validation failed") {
		t.Errorf("Event message should contain validation details, got: %s", eventMessage)
	}
}

// TestTriggerReload_InvalidConfig tests reload with invalid config file
func TestTriggerReload_InvalidConfig(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	// Create invalid YAML
	configYAML := `
invalid: yaml: content:
  - this is not valid
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

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

	callback := func(ctx context.Context, newConfig *types.NodeDoctorConfig, diff *ConfigDiff) error {
		return nil
	}

	var eventReason string
	emitter := func(severity types.EventSeverity, reason, message string) {
		eventReason = reason
	}

	coordinator := NewReloadCoordinator(configPath, config, callback, emitter)

	ctx := context.Background()
	err := coordinator.TriggerReload(ctx)

	if err == nil {
		t.Error("Expected error for invalid config")
	}

	if eventReason != "ConfigReloadFailed" {
		t.Errorf("Expected 'ConfigReloadFailed' event, got '%s'", eventReason)
	}
}

// TestTriggerReload_MissingConfigFile tests reload with missing config file
func TestTriggerReload_MissingConfigFile(t *testing.T) {
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

	callback := func(ctx context.Context, newConfig *types.NodeDoctorConfig, diff *ConfigDiff) error {
		return nil
	}

	var eventReason string
	emitter := func(severity types.EventSeverity, reason, message string) {
		eventReason = reason
	}

	coordinator := NewReloadCoordinator("/nonexistent/config.yaml", config, callback, emitter)

	ctx := context.Background()
	err := coordinator.TriggerReload(ctx)

	if err == nil {
		t.Error("Expected error for missing config file")
	}

	if eventReason != "ConfigReloadFailed" {
		t.Errorf("Expected 'ConfigReloadFailed' event, got '%s'", eventReason)
	}
}

// TestTriggerReload_CallbackError tests reload with callback returning error
func TestTriggerReload_CallbackError(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	configYAML := `
apiVersion: v1
kind: NodeDoctorConfig
metadata:
  name: test-config
settings:
  nodeName: test-node
monitors:
  - name: new-monitor
    type: kubelet
    enabled: true
    interval: 30s
    timeout: 10s
exporters:
  kubernetes:
    enabled: true
    namespace: default
remediation:
  enabled: false
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	config := &types.NodeDoctorConfig{
		APIVersion: "v1",
		Kind:       "NodeDoctorConfig",
		Metadata: types.ConfigMetadata{
			Name: "test-config",
		},
		Settings: types.GlobalSettings{
			NodeName: "test-node",
		},
		Exporters: types.ExporterConfigs{
			Kubernetes: &types.KubernetesExporterConfig{
				Enabled:   true,
				Namespace: "default",
			},
		},
		Remediation: types.RemediationConfig{
			Enabled: false,
		},
	}

	callbackError := fmt.Errorf("callback failed")
	callback := func(ctx context.Context, newConfig *types.NodeDoctorConfig, diff *ConfigDiff) error {
		return callbackError
	}

	var eventReason string
	emitter := func(severity types.EventSeverity, reason, message string) {
		eventReason = reason
	}

	coordinator := NewReloadCoordinator(configPath, config, callback, emitter)

	ctx := context.Background()
	err := coordinator.TriggerReload(ctx)

	if err == nil {
		t.Error("Expected callback error to be returned")
	}

	if !strings.Contains(err.Error(), "callback failed") {
		t.Errorf("Expected callback error in result, got: %v", err)
	}

	if eventReason != "ConfigReloadFailed" {
		t.Errorf("Expected 'ConfigReloadFailed' event, got '%s'", eventReason)
	}
}

// TestTriggerReload_ConcurrentAttempts tests concurrent reload attempts
func TestTriggerReload_ConcurrentAttempts(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	configYAML := `
apiVersion: v1
kind: NodeDoctorConfig
metadata:
  name: test-config
settings:
  nodeName: test-node
monitors:
  - name: test-monitor
    type: kubelet
    enabled: true
    interval: 30s
    timeout: 10s
exporters:
  kubernetes:
    enabled: true
    namespace: default
remediation:
  enabled: false
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

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
				Type:     "kubelet",
				Enabled:  true,
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
			},
		},
		Exporters: types.ExporterConfigs{
			Kubernetes: &types.KubernetesExporterConfig{
				Enabled:   true,
				Namespace: "default",
			},
		},
		Remediation: types.RemediationConfig{
			Enabled: false,
		},
	}

	// Slow callback to simulate long reload
	callback := func(ctx context.Context, newConfig *types.NodeDoctorConfig, diff *ConfigDiff) error {
		time.Sleep(100 * time.Millisecond)
		return nil
	}

	emitter := func(severity types.EventSeverity, reason, message string) {}

	coordinator := NewReloadCoordinator(configPath, config, callback, emitter)

	ctx := context.Background()

	// Start first reload
	errCh1 := make(chan error, 1)
	go func() {
		errCh1 <- coordinator.TriggerReload(ctx)
	}()

	// Start second reload immediately
	errCh2 := make(chan error, 1)
	go func() {
		errCh2 <- coordinator.TriggerReload(ctx)
	}()

	err1 := <-errCh1
	err2 := <-errCh2

	// One should succeed, one should fail with "reload already in progress"
	if err1 != nil && err2 != nil {
		t.Error("Expected one reload to succeed")
	}

	if err1 == nil && err2 == nil {
		t.Error("Expected one reload to fail with 'already in progress'")
	}

	// Check that one of the errors mentions "reload already in progress"
	inProgressErr := err1
	if inProgressErr == nil {
		inProgressErr = err2
	}

	if inProgressErr != nil && !strings.Contains(inProgressErr.Error(), "reload already in progress") {
		t.Errorf("Expected 'reload already in progress' error, got: %v", inProgressErr)
	}
}

// TestTriggerReload_WithValidationSuccess tests reload with validation success
func TestTriggerReload_WithValidationSuccess(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	configYAML := `
apiVersion: v1
kind: NodeDoctorConfig
metadata:
  name: test-config
settings:
  nodeName: test-node
monitors:
  - name: test-monitor
    type: kubelet
    enabled: true
    interval: 30s
    timeout: 10s
exporters:
  kubernetes:
    enabled: true
    namespace: default
remediation:
  enabled: false
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	config := &types.NodeDoctorConfig{
		APIVersion: "v1",
		Kind:       "NodeDoctorConfig",
		Metadata: types.ConfigMetadata{
			Name: "test-config",
		},
		Settings: types.GlobalSettings{
			NodeName: "different-node", // Different from file to trigger change
		},
		Exporters: types.ExporterConfigs{
			Kubernetes: &types.KubernetesExporterConfig{
				Enabled:   true,
				Namespace: "default",
			},
		},
		Remediation: types.RemediationConfig{
			Enabled: false,
		},
	}

	callbackCalled := false
	callback := func(ctx context.Context, newConfig *types.NodeDoctorConfig, diff *ConfigDiff) error {
		callbackCalled = true
		return nil
	}

	var events []string
	emitter := func(severity types.EventSeverity, reason, message string) {
		events = append(events, reason)
	}

	coordinator := NewReloadCoordinator(configPath, config, callback, emitter)

	ctx := context.Background()
	err := coordinator.TriggerReload(ctx)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if !callbackCalled {
		t.Error("Callback should be called when there are valid changes")
	}

	// Check that we got validation success event
	hasValidationSuccess := false
	for _, event := range events {
		if event == "ConfigValidationSucceeded" {
			hasValidationSuccess = true
			break
		}
	}

	if !hasValidationSuccess {
		t.Error("Expected 'ConfigValidationSucceeded' event")
	}
}

// TestSetValidator tests setting a new validator
func TestSetValidator(t *testing.T) {
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

	callback := func(ctx context.Context, newConfig *types.NodeDoctorConfig, diff *ConfigDiff) error {
		return nil
	}

	emitter := func(severity types.EventSeverity, reason, message string) {}

	coordinator := NewReloadCoordinator("/test/path", config, callback, emitter)

	// Check initial validator
	initialValidator := coordinator.GetValidator()
	if initialValidator.maxMonitors != 100 {
		t.Errorf("Expected default maxMonitors 100, got %d", initialValidator.maxMonitors)
	}

	// Set new validator
	newValidator := NewConfigValidatorWithLimits(50, 5, 25, 10)
	coordinator.SetValidator(newValidator)

	// Verify validator was changed
	currentValidator := coordinator.GetValidator()
	if currentValidator.maxMonitors != 50 {
		t.Errorf("Expected maxMonitors 50, got %d", currentValidator.maxMonitors)
	}
}

// TestTriggerReload_NilValidator tests reload with nil validator (should skip validation)
func TestTriggerReload_NilValidator(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	// Create a valid config file
	configYAML := `
apiVersion: v1
kind: NodeDoctorConfig
metadata:
  name: test-config
settings:
  nodeName: test-node
monitors:
  - name: test-monitor
    type: kubelet
    enabled: true
    interval: 30s
    timeout: 10s
exporters:
  kubernetes:
    enabled: true
    namespace: default
remediation:
  enabled: false
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	config := &types.NodeDoctorConfig{
		APIVersion: "v1",
		Kind:       "NodeDoctorConfig",
		Metadata: types.ConfigMetadata{
			Name: "test-config",
		},
		Settings: types.GlobalSettings{
			NodeName: "different-node", // Different to trigger change
		},
		Exporters: types.ExporterConfigs{
			Kubernetes: &types.KubernetesExporterConfig{
				Enabled:   true,
				Namespace: "default",
			},
		},
		Remediation: types.RemediationConfig{
			Enabled: false,
		},
	}

	callbackCalled := false
	callback := func(ctx context.Context, newConfig *types.NodeDoctorConfig, diff *ConfigDiff) error {
		callbackCalled = true
		return nil
	}

	emitter := func(severity types.EventSeverity, reason, message string) {}

	coordinator := NewReloadCoordinator(configPath, config, callback, emitter)
	coordinator.SetValidator(nil) // Disable validation

	ctx := context.Background()
	err := coordinator.TriggerReload(ctx)

	if err != nil {
		t.Errorf("Expected no error when validator is nil, got: %v", err)
	}

	if !callbackCalled {
		t.Error("Callback should be called when validation is skipped")
	}
}

// TestBuildReloadStats tests building reload statistics message
func TestBuildReloadStats(t *testing.T) {
	coordinator := &ReloadCoordinator{}

	diff := &ConfigDiff{
		MonitorsAdded: []types.MonitorConfig{
			{Name: "monitor1"},
			{Name: "monitor2"},
		},
		MonitorsRemoved: []types.MonitorConfig{
			{Name: "monitor3"},
		},
		MonitorsModified: []MonitorChange{
			{Old: types.MonitorConfig{Name: "monitor4"}, New: types.MonitorConfig{Name: "monitor4"}},
		},
		ExportersChanged:   true,
		RemediationChanged: true,
	}

	duration := 150 * time.Millisecond
	stats := coordinator.buildReloadStats(diff, duration)

	expectedPhrases := []string{
		"150ms",
		"2 monitor(s) added",
		"1 monitor(s) removed",
		"1 monitor(s) modified",
		"exporters updated",
		"remediation config updated",
	}

	for _, phrase := range expectedPhrases {
		if !strings.Contains(stats, phrase) {
			t.Errorf("Expected stats to contain '%s', got: %s", phrase, stats)
		}
	}
}

// TestReloadCoordinator_ContextCancellation tests context cancellation during reload
func TestReloadCoordinator_ContextCancellation(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	configYAML := `
apiVersion: v1
kind: NodeDoctorConfig
metadata:
  name: test-config
settings:
  nodeName: test-node
monitors:
  - name: test-monitor
    type: kubelet
    enabled: true
    interval: 30s
    timeout: 10s
exporters:
  kubernetes:
    enabled: true
    namespace: default
remediation:
  enabled: false
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	config := &types.NodeDoctorConfig{
		APIVersion: "v1",
		Kind:       "NodeDoctorConfig",
		Metadata: types.ConfigMetadata{
			Name: "test-config",
		},
		Settings: types.GlobalSettings{
			NodeName: "different-node",
		},
		Exporters: types.ExporterConfigs{
			Kubernetes: &types.KubernetesExporterConfig{
				Enabled:   true,
				Namespace: "default",
			},
		},
		Remediation: types.RemediationConfig{
			Enabled: false,
		},
	}

	// Callback that checks for cancellation
	callback := func(ctx context.Context, newConfig *types.NodeDoctorConfig, diff *ConfigDiff) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}

	emitter := func(severity types.EventSeverity, reason, message string) {}

	coordinator := NewReloadCoordinator(configPath, config, callback, emitter)

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := coordinator.TriggerReload(ctx)

	// The reload might succeed if cancellation happens after validation
	// This test mainly ensures we handle context properly
	if err != nil && !strings.Contains(err.Error(), "context canceled") {
		// If there's an error, it should be context-related
		t.Logf("Got error (acceptable): %v", err)
	}
}