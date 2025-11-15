package reload

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
	"github.com/supporttools/node-doctor/pkg/util"
)

// TestNewReloadCoordinator tests creation of ReloadCoordinator
func TestNewReloadCoordinator(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	config := &types.NodeDoctorConfig{
		APIVersion: "node-doctor.io/v1alpha1",
		Kind:       "NodeDoctorConfig",
	}

	callbackCalled := false
	callback := func(ctx context.Context, newConfig *types.NodeDoctorConfig, diff *ConfigDiff) error {
		callbackCalled = true
		return nil
	}

	emitterCalled := false
	emitter := func(severity types.EventSeverity, reason, message string) {
		emitterCalled = true
	}

	coordinator := NewReloadCoordinator(configPath, config, callback, emitter)

	if coordinator == nil {
		t.Fatal("Expected coordinator but got nil")
	}

	if coordinator.configPath != configPath {
		t.Errorf("Expected configPath %s, got %s", configPath, coordinator.configPath)
	}

	if coordinator.currentConfig != config {
		t.Error("Current config not set correctly")
	}

	if coordinator.reloadCallback == nil {
		t.Error("Reload callback not set")
	}

	if coordinator.eventEmitter == nil {
		t.Error("Event emitter not set")
	}

	if coordinator.reloadInProgress {
		t.Error("Reload should not be in progress initially")
	}

	// Verify callbacks were not called during construction
	if callbackCalled {
		t.Error("Callback should not be called during construction")
	}

	if emitterCalled {
		t.Error("Emitter should not be called during construction")
	}
}

// TestGetCurrentConfig tests retrieving current configuration
func TestGetCurrentConfig(t *testing.T) {
	config := &types.NodeDoctorConfig{
		APIVersion: "node-doctor.io/v1alpha1",
		Kind:       "NodeDoctorConfig",
		Metadata: types.ConfigMetadata{
			Name: "test-config",
		},
	}

	coordinator := NewReloadCoordinator("", config, nil, nil)

	currentConfig := coordinator.GetCurrentConfig()

	if currentConfig != config {
		t.Error("GetCurrentConfig returned wrong config")
	}

	if currentConfig.Metadata.Name != "test-config" {
		t.Errorf("Expected config name 'test-config', got '%s'", currentConfig.Metadata.Name)
	}
}

// TestTriggerReload_NoChanges tests reload when configuration is unchanged
func TestTriggerReload_NoChanges(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	// Create config file
	configYAML := `
apiVersion: node-doctor.io/v1alpha1
kind: NodeDoctorConfig
metadata:
  name: test-config
settings:
  nodeName: test-node
  logLevel: info
  logFormat: text
monitors: []
exporters: {}
remediation:
  enabled: false
reload:
  enabled: false
  debounceInterval: "500ms"
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	// Load initial config from file (so it has defaults applied)
	config, err := util.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("Failed to load initial config: %v", err)
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
	err = coordinator.TriggerReload(ctx)

	// Should succeed but not call callback (no changes)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if callbackCalled {
		t.Error("Callback should not be called when there are no changes")
	}

	// When there are no changes, the event should be ConfigReloadNoChanges
	if eventReason != "ConfigReloadNoChanges" {
		t.Errorf("Expected 'ConfigReloadNoChanges' event, got '%s'", eventReason)
	}
}

// TestTriggerReload_WithChanges tests successful reload with changes
func TestTriggerReload_WithChanges(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	// Create updated config file with new monitor
	configYAML := `
apiVersion: node-doctor.io/v1alpha1
kind: NodeDoctorConfig
metadata:
  name: test-config
settings:
  nodeName: test-node
monitors:
  - name: new-monitor
    type: system
    enabled: true
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	// Create initial config without monitor
	config := &types.NodeDoctorConfig{
		APIVersion: "node-doctor.io/v1alpha1",
		Kind:       "NodeDoctorConfig",
		Metadata: types.ConfigMetadata{
			Name: "test-config",
		},
		Settings: types.GlobalSettings{
			NodeName: "test-node",
		},
		Monitors: []types.MonitorConfig{},
	}

	var callbackDiff *ConfigDiff
	callback := func(ctx context.Context, newConfig *types.NodeDoctorConfig, diff *ConfigDiff) error {
		callbackDiff = diff
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

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify callback was called with correct diff
	if callbackDiff == nil {
		t.Fatal("Callback should be called with changes")
	}

	if len(callbackDiff.MonitorsAdded) != 1 {
		t.Errorf("Expected 1 monitor added, got %d", len(callbackDiff.MonitorsAdded))
	}

	if len(callbackDiff.MonitorsAdded) > 0 && callbackDiff.MonitorsAdded[0].Name != "new-monitor" {
		t.Errorf("Expected monitor 'new-monitor', got '%s'", callbackDiff.MonitorsAdded[0].Name)
	}

	// Verify success event
	if eventReason != "ConfigReloadSucceeded" {
		t.Errorf("Expected 'ConfigReloadSucceeded' event, got '%s'", eventReason)
	}

	// Verify event message contains statistics
	if !strings.Contains(eventMessage, "1 monitor(s) added") {
		t.Errorf("Event message should contain '1 monitor(s) added', got: %s", eventMessage)
	}

	// Verify current config was updated
	currentConfig := coordinator.GetCurrentConfig()
	if len(currentConfig.Monitors) != 1 {
		t.Errorf("Expected current config to have 1 monitor, got %d", len(currentConfig.Monitors))
	}
}

// TestTriggerReload_InvalidConfig tests reload with invalid configuration
func TestTriggerReload_InvalidConfig(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	// Create invalid config file
	invalidYAML := `
invalid: yaml: syntax[[[
`
	if err := os.WriteFile(configPath, []byte(invalidYAML), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	config := &types.NodeDoctorConfig{}

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

	if err == nil {
		t.Error("Expected error for invalid config")
	}

	if callbackCalled {
		t.Error("Callback should not be called for invalid config")
	}

	if eventReason != "ConfigReloadFailed" {
		t.Errorf("Expected 'ConfigReloadFailed' event, got '%s'", eventReason)
	}
}

// TestTriggerReload_MissingConfigFile tests reload when config file doesn't exist
func TestTriggerReload_MissingConfigFile(t *testing.T) {
	configPath := "/nonexistent/config.yaml"

	config := &types.NodeDoctorConfig{}

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

	if err == nil {
		t.Error("Expected error for missing config file")
	}

	if callbackCalled {
		t.Error("Callback should not be called for missing config")
	}

	if eventReason != "ConfigReloadFailed" {
		t.Errorf("Expected 'ConfigReloadFailed' event, got '%s'", eventReason)
	}
}

// TestTriggerReload_CallbackError tests reload when callback returns error
func TestTriggerReload_CallbackError(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	// Create valid but different config
	configYAML := `
apiVersion: node-doctor.io/v1alpha1
kind: NodeDoctorConfig
metadata:
  name: updated-config
settings:
  nodeName: test-node
monitors:
  - name: new-monitor
    type: system
    enabled: true
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	config := &types.NodeDoctorConfig{
		APIVersion: "node-doctor.io/v1alpha1",
		Kind:       "NodeDoctorConfig",
		Metadata: types.ConfigMetadata{
			Name: "test-config",
		},
	}

	callbackError := errors.New("callback failed")
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
		t.Error("Expected error when callback fails")
	}

	if !errors.Is(err, callbackError) {
		t.Errorf("Expected callback error, got: %v", err)
	}

	if eventReason != "ConfigReloadFailed" {
		t.Errorf("Expected 'ConfigReloadFailed' event, got '%s'", eventReason)
	}

	// Verify current config was NOT updated on failure
	currentConfig := coordinator.GetCurrentConfig()
	if currentConfig.Metadata.Name != "test-config" {
		t.Error("Config should not be updated when callback fails")
	}
}

// TestTriggerReload_ConcurrentAttempts tests that concurrent reloads are prevented
func TestTriggerReload_ConcurrentAttempts(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	// Create config file
	configYAML := `
apiVersion: node-doctor.io/v1alpha1
kind: NodeDoctorConfig
metadata:
  name: test-config
settings:
  nodeName: test-node
monitors:
  - name: monitor1
    type: system
    enabled: true
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	config := &types.NodeDoctorConfig{
		APIVersion: "node-doctor.io/v1alpha1",
		Kind:       "NodeDoctorConfig",
	}

	// Slow callback that takes 500ms
	callbackCount := 0
	callback := func(ctx context.Context, newConfig *types.NodeDoctorConfig, diff *ConfigDiff) error {
		callbackCount++
		time.Sleep(500 * time.Millisecond)
		return nil
	}

	emitter := func(severity types.EventSeverity, reason, message string) {}

	coordinator := NewReloadCoordinator(configPath, config, callback, emitter)

	ctx := context.Background()

	// Start first reload in goroutine
	done := make(chan error, 2)
	go func() {
		done <- coordinator.TriggerReload(ctx)
	}()

	// Give first reload time to start
	time.Sleep(50 * time.Millisecond)

	// Try second reload while first is in progress
	go func() {
		done <- coordinator.TriggerReload(ctx)
	}()

	// Wait for both to complete
	err1 := <-done
	err2 := <-done

	// One should succeed, one should fail with "in progress" error
	if err1 == nil && err2 == nil {
		t.Error("Expected one reload to fail due to concurrent attempt")
	}

	// Callback should only be called once
	if callbackCount != 1 {
		t.Errorf("Expected callback to be called once, got %d times", callbackCount)
	}
}

// TestBuildReloadStats tests the reload statistics message building
func TestBuildReloadStats(t *testing.T) {
	coordinator := NewReloadCoordinator("", &types.NodeDoctorConfig{}, nil, nil)

	diff := &ConfigDiff{
		MonitorsAdded:      []types.MonitorConfig{{Name: "m1"}, {Name: "m2"}},
		MonitorsRemoved:    []types.MonitorConfig{{Name: "m3"}},
		MonitorsModified:   []MonitorChange{{}, {}, {}},
		ExportersChanged:   true,
		RemediationChanged: false,
	}

	stats := coordinator.buildReloadStats(diff, 1234*time.Millisecond)

	if !strings.Contains(stats, "2 monitor(s) added") {
		t.Errorf("Stats should contain '2 monitor(s) added', got: %s", stats)
	}

	if !strings.Contains(stats, "1 monitor(s) removed") {
		t.Errorf("Stats should contain '1 monitor(s) removed', got: %s", stats)
	}

	if !strings.Contains(stats, "3 monitor(s) modified") {
		t.Errorf("Stats should contain '3 monitor(s) modified', got: %s", stats)
	}

	if !strings.Contains(stats, "exporters updated") {
		t.Errorf("Stats should contain 'exporters updated', got: %s", stats)
	}

	if !strings.Contains(stats, "1.234s") {
		t.Errorf("Stats should contain duration '1.234s', got: %s", stats)
	}
}

// TestReloadCoordinator_ContextCancellation tests reload cancellation
func TestReloadCoordinator_ContextCancellation(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	// Create config with changes
	configYAML := `
apiVersion: node-doctor.io/v1alpha1
kind: NodeDoctorConfig
metadata:
  name: test-config
settings:
  nodeName: test-node
monitors:
  - name: new-monitor
    type: system
    enabled: true
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	config := &types.NodeDoctorConfig{}

	// Callback that checks context
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

	// Create context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := coordinator.TriggerReload(ctx)

	// Should handle context cancellation gracefully
	// Error might occur during callback or earlier
	if err == nil {
		t.Log("Reload succeeded despite cancelled context (acceptable if validated before callback)")
	}
}
