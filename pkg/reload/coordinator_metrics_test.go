package reload

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// fakeReloadRecorder captures invocations of a ReloadMetricsRecorder.
type fakeReloadRecorder struct {
	mu        sync.Mutex
	calls     int
	lastOK    bool
	lastDur   time.Duration
	successes int
	failures  int
}

func (f *fakeReloadRecorder) record(success bool, duration time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastOK = success
	f.lastDur = duration
	if success {
		f.successes++
	} else {
		f.failures++
	}
}

// TestPerformReload_RecorderSuccess verifies the metrics recorder is invoked
// exactly once with success=true and a non-negative duration on a good reload
// that applies changes.
func TestPerformReload_RecorderSuccess(t *testing.T) {
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
    type: kubernetes-kubelet-check
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
		Metadata:   types.ConfigMetadata{Name: "test-config"},
		Settings:   types.GlobalSettings{NodeName: "test-node"},
		Exporters: types.ExporterConfigs{
			Kubernetes: &types.KubernetesExporterConfig{Enabled: true, Namespace: "default"},
		},
		Remediation: types.RemediationConfig{Enabled: false},
	}

	callback := func(ctx context.Context, newConfig *types.NodeDoctorConfig, diff *ConfigDiff) error {
		return nil
	}
	emitter := func(severity types.EventSeverity, reason, message string) {}

	coordinator := NewReloadCoordinator(configPath, config, callback, emitter)

	rec := &fakeReloadRecorder{}
	coordinator.SetMetricsRecorder(rec.record)

	if err := coordinator.TriggerReload(context.Background()); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if rec.calls != 1 {
		t.Fatalf("expected recorder to be invoked exactly once, got %d", rec.calls)
	}
	if !rec.lastOK {
		t.Errorf("expected success=true, got false")
	}
	if rec.lastDur < 0 {
		t.Errorf("expected non-negative duration, got %v", rec.lastDur)
	}
	if rec.successes != 1 || rec.failures != 0 {
		t.Errorf("expected 1 success / 0 failures, got %d/%d", rec.successes, rec.failures)
	}
}

// TestPerformReload_RecorderFailure verifies the metrics recorder is invoked
// exactly once with success=false when the reload fails (validation failure).
func TestPerformReload_RecorderFailure(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	// Config that loads but fails validation (empty monitor name/type, no exporters).
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
		Metadata:   types.ConfigMetadata{Name: "test-config"},
		Settings:   types.GlobalSettings{NodeName: "test-node"},
	}

	callbackCalled := false
	callback := func(ctx context.Context, newConfig *types.NodeDoctorConfig, diff *ConfigDiff) error {
		callbackCalled = true
		return nil
	}
	emitter := func(severity types.EventSeverity, reason, message string) {}

	coordinator := NewReloadCoordinator(configPath, config, callback, emitter)

	rec := &fakeReloadRecorder{}
	coordinator.SetMetricsRecorder(rec.record)

	if err := coordinator.TriggerReload(context.Background()); err == nil {
		t.Fatal("expected reload to fail validation")
	}
	if callbackCalled {
		t.Error("callback should not run on a failed reload")
	}

	if rec.calls != 1 {
		t.Fatalf("expected recorder to be invoked exactly once, got %d", rec.calls)
	}
	if rec.lastOK {
		t.Errorf("expected success=false, got true")
	}
	if rec.lastDur < 0 {
		t.Errorf("expected non-negative duration, got %v", rec.lastDur)
	}
	if rec.successes != 0 || rec.failures != 1 {
		t.Errorf("expected 0 success / 1 failure, got %d/%d", rec.successes, rec.failures)
	}
}

// TestPerformReload_NilRecorder ensures reloads are nil-safe when no recorder is set.
func TestPerformReload_NilRecorder(t *testing.T) {
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
    type: kubernetes-kubelet-check
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
		Metadata:   types.ConfigMetadata{Name: "test-config"},
		Settings:   types.GlobalSettings{NodeName: "test-node"},
		Exporters: types.ExporterConfigs{
			Kubernetes: &types.KubernetesExporterConfig{Enabled: true, Namespace: "default"},
		},
	}

	callback := func(ctx context.Context, newConfig *types.NodeDoctorConfig, diff *ConfigDiff) error {
		return nil
	}
	emitter := func(severity types.EventSeverity, reason, message string) {}

	coordinator := NewReloadCoordinator(configPath, config, callback, emitter)
	// No recorder set; must not panic.
	if err := coordinator.TriggerReload(context.Background()); err != nil {
		t.Fatalf("unexpected error with nil recorder: %v", err)
	}
}
