/*
Integration Tests for Config Hot-Reload

These tests verify that configuration changes are properly detected and applied
without requiring a restart of the node-doctor process.

Tests cover:
- Adding monitors dynamically
- Removing monitors gracefully
- Modifying monitor settings
- Rejecting invalid configuration changes
- Handling concurrent/rapid changes
- Exporter configuration changes

Run tests with:

	go test -v ./test/integration/config/... -run TestConfigHotReload
	go test -v -race ./test/integration/config/... -run TestConfigHotReload
*/
package config

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/reload"
	"github.com/supporttools/node-doctor/pkg/types"
	"github.com/supporttools/node-doctor/pkg/util"
	"github.com/supporttools/node-doctor/test"
)

// testReloadCallback is a helper for capturing reload events
type testReloadCallback struct {
	mu            sync.Mutex
	reloadCount   int
	lastConfig    *types.NodeDoctorConfig
	lastDiff      *reload.ConfigDiff
	lastErr       error
	reloadCh      chan struct{}
	shouldFail    bool
	failureReason error
}

func newTestReloadCallback() *testReloadCallback {
	return &testReloadCallback{
		reloadCh: make(chan struct{}, 10),
	}
}

func (cb *testReloadCallback) Callback(ctx context.Context, newConfig *types.NodeDoctorConfig, diff *reload.ConfigDiff) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.reloadCount++
	cb.lastConfig = newConfig
	cb.lastDiff = diff

	if cb.shouldFail {
		cb.lastErr = cb.failureReason
		return cb.failureReason
	}

	// Signal that a reload occurred
	select {
	case cb.reloadCh <- struct{}{}:
	default:
	}

	return nil
}

func (cb *testReloadCallback) GetReloadCount() int {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.reloadCount
}

func (cb *testReloadCallback) GetLastDiff() *reload.ConfigDiff {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.lastDiff
}

func (cb *testReloadCallback) GetLastConfig() *types.NodeDoctorConfig {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.lastConfig
}

func (cb *testReloadCallback) SetShouldFail(fail bool, reason error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.shouldFail = fail
	cb.failureReason = reason
}

// waitForReload waits for a reload event with timeout
func (cb *testReloadCallback) waitForReload(timeout time.Duration) bool {
	select {
	case <-cb.reloadCh:
		return true
	case <-time.After(timeout):
		return false
	}
}

// noopEventEmitter is a no-op event emitter for tests
func noopEventEmitter(severity types.EventSeverity, reason, message string) {
	// Do nothing
}

// TestConfigHotReload_AddMonitor tests adding a new monitor via config change
func TestConfigHotReload_AddMonitor(t *testing.T) {
	// Set required environment (t.Setenv auto-cleans up after test)
	t.Setenv("NODE_NAME", "test-node")

	ctx, cancel := test.TestContext(t, 30*time.Second)
	defer cancel()

	// Create initial config
	tmpDir := test.TempDir(t)
	configPath := filepath.Join(tmpDir, "config.yaml")

	initialConfig := `apiVersion: "node-doctor.io/v1"
kind: "NodeDoctorConfig"
metadata:
  name: "test-config"
settings:
  nodeName: "test-node"
  logLevel: "info"
monitors:
  - name: "monitor-1"
    type: "kubelet"
    enabled: true
    interval: "30s"
    timeout: "10s"
exporters:
  kubernetes:
    enabled: true
    namespace: "test-ns"
  prometheus:
    enabled: true
    port: 9101
    path: "/metrics"
`
	test.AssertNoError(t, os.WriteFile(configPath, []byte(initialConfig), 0644), "write initial config")

	// Load initial config
	initialCfg, err := util.LoadConfig(configPath)
	test.AssertNoError(t, err, "load initial config")

	// Create reload callback
	callback := newTestReloadCallback()

	// Create reload coordinator
	coordinator := reload.NewReloadCoordinator(configPath, initialCfg, callback.Callback, noopEventEmitter)

	// Create config watcher
	watcher, err := reload.NewConfigWatcher(configPath, 100*time.Millisecond)
	test.AssertNoError(t, err, "create config watcher")

	// Start watcher
	changeCh, err := watcher.Start(ctx)
	test.AssertNoError(t, err, "start watcher")
	defer watcher.Stop()

	// Start listening for changes in background
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-changeCh:
				coordinator.TriggerReload(ctx)
			}
		}
	}()

	// Modify config to add a new monitor
	updatedConfig := `apiVersion: "node-doctor.io/v1"
kind: "NodeDoctorConfig"
metadata:
  name: "test-config"
settings:
  nodeName: "test-node"
  logLevel: "info"
monitors:
  - name: "monitor-1"
    type: "kubelet"
    enabled: true
    interval: "30s"
    timeout: "10s"
  - name: "monitor-2"
    type: "kubelet"
    enabled: true
    interval: "60s"
    timeout: "15s"
exporters:
  kubernetes:
    enabled: true
    namespace: "test-ns"
  prometheus:
    enabled: true
    port: 9101
    path: "/metrics"
`
	test.AssertNoError(t, os.WriteFile(configPath, []byte(updatedConfig), 0644), "write updated config")

	// Wait for reload
	test.Eventually(t, func() bool {
		return callback.GetReloadCount() > 0
	}, 5*time.Second, 100*time.Millisecond, "expected reload to occur")

	// Verify the diff shows the added monitor
	diff := callback.GetLastDiff()
	test.AssertTrue(t, diff != nil, "expected diff to be non-nil")
	test.AssertEqual(t, 1, len(diff.MonitorsAdded), "expected 1 monitor added")
	test.AssertEqual(t, "monitor-2", diff.MonitorsAdded[0].Name, "expected monitor-2 to be added")

	// Verify the new config has both monitors
	newConfig := callback.GetLastConfig()
	test.AssertEqual(t, 2, len(newConfig.Monitors), "expected 2 monitors in new config")
}

// TestConfigHotReload_RemoveMonitor tests removing a monitor via config change
func TestConfigHotReload_RemoveMonitor(t *testing.T) {
	t.Setenv("NODE_NAME", "test-node")

	ctx, cancel := test.TestContext(t, 30*time.Second)
	defer cancel()

	tmpDir := test.TempDir(t)
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Start with two monitors
	initialConfig := `apiVersion: "node-doctor.io/v1"
kind: "NodeDoctorConfig"
metadata:
  name: "test-config"
settings:
  nodeName: "test-node"
monitors:
  - name: "monitor-1"
    type: "kubelet"
    enabled: true
    interval: "30s"
  - name: "monitor-2"
    type: "kubelet"
    enabled: true
    interval: "60s"
exporters:
  kubernetes:
    enabled: true
    namespace: "test-ns"
  prometheus:
    enabled: true
    port: 9101
    path: "/metrics"
`
	test.AssertNoError(t, os.WriteFile(configPath, []byte(initialConfig), 0644), "write initial config")

	initialCfg, err := util.LoadConfig(configPath)
	test.AssertNoError(t, err, "load initial config")

	callback := newTestReloadCallback()
	coordinator := reload.NewReloadCoordinator(configPath, initialCfg, callback.Callback, noopEventEmitter)

	watcher, err := reload.NewConfigWatcher(configPath, 100*time.Millisecond)
	test.AssertNoError(t, err, "create watcher")

	changeCh, err := watcher.Start(ctx)
	test.AssertNoError(t, err, "start watcher")
	defer watcher.Stop()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-changeCh:
				coordinator.TriggerReload(ctx)
			}
		}
	}()

	// Remove monitor-2
	updatedConfig := `apiVersion: "node-doctor.io/v1"
kind: "NodeDoctorConfig"
metadata:
  name: "test-config"
settings:
  nodeName: "test-node"
monitors:
  - name: "monitor-1"
    type: "kubelet"
    enabled: true
    interval: "30s"
exporters:
  kubernetes:
    enabled: true
    namespace: "test-ns"
  prometheus:
    enabled: true
    port: 9101
    path: "/metrics"
`
	test.AssertNoError(t, os.WriteFile(configPath, []byte(updatedConfig), 0644), "write updated config")

	test.Eventually(t, func() bool {
		return callback.GetReloadCount() > 0
	}, 5*time.Second, 100*time.Millisecond, "expected reload to occur")

	diff := callback.GetLastDiff()
	test.AssertTrue(t, diff != nil, "expected diff to be non-nil")
	test.AssertEqual(t, 1, len(diff.MonitorsRemoved), "expected 1 monitor removed")
	test.AssertEqual(t, "monitor-2", diff.MonitorsRemoved[0].Name, "expected monitor-2 to be removed")

	newConfig := callback.GetLastConfig()
	test.AssertEqual(t, 1, len(newConfig.Monitors), "expected 1 monitor in new config")
}

// TestConfigHotReload_ModifyMonitor tests modifying a monitor's settings
func TestConfigHotReload_ModifyMonitor(t *testing.T) {
	t.Setenv("NODE_NAME", "test-node")

	ctx, cancel := test.TestContext(t, 30*time.Second)
	defer cancel()

	tmpDir := test.TempDir(t)
	configPath := filepath.Join(tmpDir, "config.yaml")

	initialConfig := `apiVersion: "node-doctor.io/v1"
kind: "NodeDoctorConfig"
metadata:
  name: "test-config"
settings:
  nodeName: "test-node"
monitors:
  - name: "monitor-1"
    type: "kubelet"
    enabled: true
    interval: "30s"
    timeout: "10s"
exporters:
  kubernetes:
    enabled: true
    namespace: "test-ns"
  prometheus:
    enabled: true
    port: 9101
    path: "/metrics"
`
	test.AssertNoError(t, os.WriteFile(configPath, []byte(initialConfig), 0644), "write initial config")

	initialCfg, err := util.LoadConfig(configPath)
	test.AssertNoError(t, err, "load initial config")

	callback := newTestReloadCallback()
	coordinator := reload.NewReloadCoordinator(configPath, initialCfg, callback.Callback, noopEventEmitter)

	watcher, err := reload.NewConfigWatcher(configPath, 100*time.Millisecond)
	test.AssertNoError(t, err, "create watcher")

	changeCh, err := watcher.Start(ctx)
	test.AssertNoError(t, err, "start watcher")
	defer watcher.Stop()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-changeCh:
				coordinator.TriggerReload(ctx)
			}
		}
	}()

	// Modify monitor-1 interval
	updatedConfig := `apiVersion: "node-doctor.io/v1"
kind: "NodeDoctorConfig"
metadata:
  name: "test-config"
settings:
  nodeName: "test-node"
monitors:
  - name: "monitor-1"
    type: "kubelet"
    enabled: true
    interval: "60s"
    timeout: "15s"
exporters:
  kubernetes:
    enabled: true
    namespace: "test-ns"
  prometheus:
    enabled: true
    port: 9101
    path: "/metrics"
`
	test.AssertNoError(t, os.WriteFile(configPath, []byte(updatedConfig), 0644), "write updated config")

	test.Eventually(t, func() bool {
		return callback.GetReloadCount() > 0
	}, 5*time.Second, 100*time.Millisecond, "expected reload to occur")

	diff := callback.GetLastDiff()
	test.AssertTrue(t, diff != nil, "expected diff to be non-nil")
	test.AssertEqual(t, 1, len(diff.MonitorsModified), "expected 1 monitor modified")
	test.AssertEqual(t, "monitor-1", diff.MonitorsModified[0].New.Name, "expected monitor-1 to be modified")

	// Verify the new interval
	newConfig := callback.GetLastConfig()
	test.AssertEqual(t, 60*time.Second, newConfig.Monitors[0].Interval, "expected new interval to be 60s")
}

// TestConfigHotReload_InvalidConfig tests that invalid configurations are rejected
func TestConfigHotReload_InvalidConfig(t *testing.T) {
	t.Setenv("NODE_NAME", "test-node")

	ctx, cancel := test.TestContext(t, 30*time.Second)
	defer cancel()

	tmpDir := test.TempDir(t)
	configPath := filepath.Join(tmpDir, "config.yaml")

	initialConfig := `apiVersion: "node-doctor.io/v1"
kind: "NodeDoctorConfig"
metadata:
  name: "test-config"
settings:
  nodeName: "test-node"
monitors:
  - name: "monitor-1"
    type: "kubelet"
    enabled: true
    interval: "30s"
exporters:
  kubernetes:
    enabled: true
    namespace: "test-ns"
  prometheus:
    enabled: true
    port: 9101
    path: "/metrics"
`
	test.AssertNoError(t, os.WriteFile(configPath, []byte(initialConfig), 0644), "write initial config")

	initialCfg, err := util.LoadConfig(configPath)
	test.AssertNoError(t, err, "load initial config")

	callback := newTestReloadCallback()
	coordinator := reload.NewReloadCoordinator(configPath, initialCfg, callback.Callback, noopEventEmitter)

	watcher, err := reload.NewConfigWatcher(configPath, 100*time.Millisecond)
	test.AssertNoError(t, err, "create watcher")

	changeCh, err := watcher.Start(ctx)
	test.AssertNoError(t, err, "start watcher")
	defer watcher.Stop()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-changeCh:
				coordinator.TriggerReload(ctx)
			}
		}
	}()

	// Write invalid YAML
	invalidConfig := `apiVersion: "node-doctor.io/v1"
kind: "NodeDoctorConfig"
metadata:
  name: "test-config"
settings:
  nodeName: "test-node"
monitors:
  - name: ""
    type: ""
    enabled: true
    interval: "1s"
`
	test.AssertNoError(t, os.WriteFile(configPath, []byte(invalidConfig), 0644), "write invalid config")

	// Wait a bit for the file change to be detected
	time.Sleep(500 * time.Millisecond)

	// Reload should have been attempted but failed during validation
	// The current config should remain unchanged
	currentConfig := coordinator.GetCurrentConfig()
	test.AssertEqual(t, 1, len(currentConfig.Monitors), "current config should be unchanged")
	test.AssertEqual(t, "monitor-1", currentConfig.Monitors[0].Name, "monitor name should be unchanged")
}

// TestConfigHotReload_ConcurrentChanges tests handling of rapid config changes
func TestConfigHotReload_ConcurrentChanges(t *testing.T) {
	t.Setenv("NODE_NAME", "test-node")

	ctx, cancel := test.TestContext(t, 30*time.Second)
	defer cancel()

	tmpDir := test.TempDir(t)
	configPath := filepath.Join(tmpDir, "config.yaml")

	initialConfig := `apiVersion: "node-doctor.io/v1"
kind: "NodeDoctorConfig"
metadata:
  name: "test-config"
settings:
  nodeName: "test-node"
monitors:
  - name: "monitor-1"
    type: "kubelet"
    enabled: true
    interval: "30s"
exporters:
  kubernetes:
    enabled: true
    namespace: "test-ns"
  prometheus:
    enabled: true
    port: 9101
    path: "/metrics"
`
	test.AssertNoError(t, os.WriteFile(configPath, []byte(initialConfig), 0644), "write initial config")

	initialCfg, err := util.LoadConfig(configPath)
	test.AssertNoError(t, err, "load initial config")

	callback := newTestReloadCallback()
	coordinator := reload.NewReloadCoordinator(configPath, initialCfg, callback.Callback, noopEventEmitter)

	watcher, err := reload.NewConfigWatcher(configPath, 50*time.Millisecond)
	test.AssertNoError(t, err, "create watcher")

	changeCh, err := watcher.Start(ctx)
	test.AssertNoError(t, err, "start watcher")
	defer watcher.Stop()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-changeCh:
				coordinator.TriggerReload(ctx)
			}
		}
	}()

	// Rapidly update config multiple times
	for i := 0; i < 5; i++ {
		config := `apiVersion: "node-doctor.io/v1"
kind: "NodeDoctorConfig"
metadata:
  name: "test-config"
settings:
  nodeName: "test-node"
monitors:
  - name: "monitor-1"
    type: "kubelet"
    enabled: true
    interval: "%ds"
exporters:
  kubernetes:
    enabled: true
    namespace: "test-ns"
  prometheus:
    enabled: true
    port: 9101
    path: "/metrics"
`
		configContent := []byte(config)
		// Replace %ds with actual interval
		interval := (i + 1) * 10
		configContent = []byte(`apiVersion: "node-doctor.io/v1"
kind: "NodeDoctorConfig"
metadata:
  name: "test-config"
settings:
  nodeName: "test-node"
monitors:
  - name: "monitor-1"
    type: "kubelet"
    enabled: true
    interval: "` + string(rune('0'+interval/10)) + `0s"
exporters:
  kubernetes:
    enabled: true
    namespace: "test-ns"
  prometheus:
    enabled: true
    port: 9101
    path: "/metrics"
`)
		test.AssertNoError(t, os.WriteFile(configPath, configContent, 0644), "write config update")
		time.Sleep(20 * time.Millisecond)
	}

	// Wait for debouncing to complete and reload to occur using Eventually
	test.Eventually(t, func() bool {
		return callback.GetReloadCount() > 0
	}, 2*time.Second, 100*time.Millisecond, "expected at least one reload")

	// Final config should reflect the last change
	currentConfig := coordinator.GetCurrentConfig()
	test.AssertEqual(t, 1, len(currentConfig.Monitors), "should have 1 monitor")
}

// TestConfigHotReload_ExporterChanges tests modifying exporter settings
func TestConfigHotReload_ExporterChanges(t *testing.T) {
	t.Setenv("NODE_NAME", "test-node")

	ctx, cancel := test.TestContext(t, 30*time.Second)
	defer cancel()

	tmpDir := test.TempDir(t)
	configPath := filepath.Join(tmpDir, "config.yaml")

	initialConfig := `apiVersion: "node-doctor.io/v1"
kind: "NodeDoctorConfig"
metadata:
  name: "test-config"
settings:
  nodeName: "test-node"
monitors:
  - name: "monitor-1"
    type: "kubelet"
    enabled: true
    interval: "30s"
exporters:
  kubernetes:
    enabled: true
    namespace: "test-ns"
  prometheus:
    enabled: true
    port: 9101
    path: "/metrics"
`
	test.AssertNoError(t, os.WriteFile(configPath, []byte(initialConfig), 0644), "write initial config")

	initialCfg, err := util.LoadConfig(configPath)
	test.AssertNoError(t, err, "load initial config")

	callback := newTestReloadCallback()
	coordinator := reload.NewReloadCoordinator(configPath, initialCfg, callback.Callback, noopEventEmitter)

	watcher, err := reload.NewConfigWatcher(configPath, 100*time.Millisecond)
	test.AssertNoError(t, err, "create watcher")

	changeCh, err := watcher.Start(ctx)
	test.AssertNoError(t, err, "start watcher")
	defer watcher.Stop()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-changeCh:
				coordinator.TriggerReload(ctx)
			}
		}
	}()

	// Modify prometheus port
	updatedConfig := `apiVersion: "node-doctor.io/v1"
kind: "NodeDoctorConfig"
metadata:
  name: "test-config"
settings:
  nodeName: "test-node"
monitors:
  - name: "monitor-1"
    type: "kubelet"
    enabled: true
    interval: "30s"
exporters:
  kubernetes:
    enabled: true
    namespace: "test-ns"
  prometheus:
    enabled: true
    port: 9102
    path: "/metrics"
`
	test.AssertNoError(t, os.WriteFile(configPath, []byte(updatedConfig), 0644), "write updated config")

	test.Eventually(t, func() bool {
		return callback.GetReloadCount() > 0
	}, 5*time.Second, 100*time.Millisecond, "expected reload to occur")

	diff := callback.GetLastDiff()
	test.AssertTrue(t, diff != nil, "expected diff to be non-nil")
	test.AssertTrue(t, diff.ExportersChanged, "expected exporters to be changed")

	// Verify the new port
	newConfig := callback.GetLastConfig()
	test.AssertEqual(t, 9102, newConfig.Exporters.Prometheus.Port, "expected new port to be 9102")
}

// TestConfigDiff_ComputeChanges tests the ConfigDiff computation
func TestConfigDiff_ComputeChanges(t *testing.T) {
	tests := []struct {
		name             string
		oldConfig        *types.NodeDoctorConfig
		newConfig        *types.NodeDoctorConfig
		expectedAdded    int
		expectedRemoved  int
		expectedModified int
		expectedChanges  bool
	}{
		{
			name: "no changes",
			oldConfig: &types.NodeDoctorConfig{
				Monitors: []types.MonitorConfig{
					{Name: "mon1", Type: "kubelet", Interval: 30 * time.Second},
				},
			},
			newConfig: &types.NodeDoctorConfig{
				Monitors: []types.MonitorConfig{
					{Name: "mon1", Type: "kubelet", Interval: 30 * time.Second},
				},
			},
			expectedAdded:    0,
			expectedRemoved:  0,
			expectedModified: 0,
			expectedChanges:  false,
		},
		{
			name: "monitor added",
			oldConfig: &types.NodeDoctorConfig{
				Monitors: []types.MonitorConfig{
					{Name: "mon1", Type: "kubelet", Interval: 30 * time.Second},
				},
			},
			newConfig: &types.NodeDoctorConfig{
				Monitors: []types.MonitorConfig{
					{Name: "mon1", Type: "kubelet", Interval: 30 * time.Second},
					{Name: "mon2", Type: "kubelet", Interval: 60 * time.Second},
				},
			},
			expectedAdded:    1,
			expectedRemoved:  0,
			expectedModified: 0,
			expectedChanges:  true,
		},
		{
			name: "monitor removed",
			oldConfig: &types.NodeDoctorConfig{
				Monitors: []types.MonitorConfig{
					{Name: "mon1", Type: "kubelet", Interval: 30 * time.Second},
					{Name: "mon2", Type: "kubelet", Interval: 60 * time.Second},
				},
			},
			newConfig: &types.NodeDoctorConfig{
				Monitors: []types.MonitorConfig{
					{Name: "mon1", Type: "kubelet", Interval: 30 * time.Second},
				},
			},
			expectedAdded:    0,
			expectedRemoved:  1,
			expectedModified: 0,
			expectedChanges:  true,
		},
		{
			name: "monitor modified",
			oldConfig: &types.NodeDoctorConfig{
				Monitors: []types.MonitorConfig{
					{Name: "mon1", Type: "kubelet", Interval: 30 * time.Second},
				},
			},
			newConfig: &types.NodeDoctorConfig{
				Monitors: []types.MonitorConfig{
					{Name: "mon1", Type: "kubelet", Interval: 60 * time.Second},
				},
			},
			expectedAdded:    0,
			expectedRemoved:  0,
			expectedModified: 1,
			expectedChanges:  true,
		},
		{
			name: "complex changes",
			oldConfig: &types.NodeDoctorConfig{
				Monitors: []types.MonitorConfig{
					{Name: "mon1", Type: "kubelet", Interval: 30 * time.Second},
					{Name: "mon2", Type: "kubelet", Interval: 60 * time.Second},
					{Name: "mon3", Type: "kubelet", Interval: 90 * time.Second},
				},
			},
			newConfig: &types.NodeDoctorConfig{
				Monitors: []types.MonitorConfig{
					{Name: "mon1", Type: "kubelet", Interval: 45 * time.Second}, // modified
					{Name: "mon3", Type: "kubelet", Interval: 90 * time.Second}, // unchanged
					{Name: "mon4", Type: "kubelet", Interval: 120 * time.Second}, // added
				},
			},
			expectedAdded:    1,
			expectedRemoved:  1,
			expectedModified: 1,
			expectedChanges:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diff := reload.ComputeConfigDiff(tt.oldConfig, tt.newConfig)

			test.AssertEqual(t, tt.expectedAdded, len(diff.MonitorsAdded), "added monitors count")
			test.AssertEqual(t, tt.expectedRemoved, len(diff.MonitorsRemoved), "removed monitors count")
			test.AssertEqual(t, tt.expectedModified, len(diff.MonitorsModified), "modified monitors count")
			test.AssertEqual(t, tt.expectedChanges, diff.HasChanges(), "has changes")
		})
	}
}

// TestConfigWatcher_FileChanges tests that the watcher detects file changes
func TestConfigWatcher_FileChanges(t *testing.T) {
	ctx, cancel := test.TestContext(t, 10*time.Second)
	defer cancel()

	tmpDir := test.TempDir(t)
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Write initial content
	test.AssertNoError(t, os.WriteFile(configPath, []byte("initial"), 0644), "write initial")

	watcher, err := reload.NewConfigWatcher(configPath, 50*time.Millisecond)
	test.AssertNoError(t, err, "create watcher")

	changeCh, err := watcher.Start(ctx)
	test.AssertNoError(t, err, "start watcher")
	defer watcher.Stop()

	// Modify the file
	test.AssertNoError(t, os.WriteFile(configPath, []byte("modified"), 0644), "write modified")

	// Wait for change notification
	select {
	case <-changeCh:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("expected file change notification")
	}
}

// TestReloadCoordinator_GetCurrentConfig tests retrieving current config
func TestReloadCoordinator_GetCurrentConfig(t *testing.T) {
	t.Setenv("NODE_NAME", "test-node")

	tmpDir := test.TempDir(t)
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `apiVersion: "node-doctor.io/v1"
kind: "NodeDoctorConfig"
metadata:
  name: "test-config"
settings:
  nodeName: "test-node"
monitors:
  - name: "monitor-1"
    type: "kubelet"
    enabled: true
    interval: "30s"
exporters:
  kubernetes:
    enabled: true
    namespace: "test-ns"
  prometheus:
    enabled: true
    port: 9101
    path: "/metrics"
`
	test.AssertNoError(t, os.WriteFile(configPath, []byte(configContent), 0644), "write config")

	cfg, err := util.LoadConfig(configPath)
	test.AssertNoError(t, err, "load config")

	callback := newTestReloadCallback()
	coordinator := reload.NewReloadCoordinator(configPath, cfg, callback.Callback, noopEventEmitter)

	currentConfig := coordinator.GetCurrentConfig()
	test.AssertTrue(t, currentConfig != nil, "current config should not be nil")
	test.AssertEqual(t, "test-config", currentConfig.Metadata.Name, "config name should match")
	test.AssertEqual(t, 1, len(currentConfig.Monitors), "should have 1 monitor")
}
