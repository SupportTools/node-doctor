package detector

import (
	"fmt"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// makeRemediationConfig builds a minimal MonitorConfig with remediation enabled.
func makeRemediationConfig(name, strategy string) types.MonitorConfig {
	return types.MonitorConfig{
		Name:     name,
		Type:     "test",
		Enabled:  true,
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Remediation: &types.MonitorRemediationConfig{
			Enabled:  true,
			Strategy: strategy,
		},
	}
}

// unhealthyStatus returns a Status from the named source with one ConditionFalse condition.
func unhealthyStatus(source, condType string) *types.Status {
	s := types.NewStatus(source)
	s.AddCondition(types.NewCondition(condType, types.ConditionFalse, "TestFailure", "something broke"))
	return s
}

// healthyStatus returns a Status with one ConditionTrue condition.
func healthyStatus(source, condType string) *types.Status {
	s := types.NewStatus(source)
	s.AddCondition(types.NewCondition(condType, types.ConditionTrue, "AllGood", "everything fine"))
	return s
}

// buildDetectorWithRemediation creates a started detector with a pre-configured monitor,
// global remediation enabled, and the given executor attached (nil = no executor).
func buildDetectorWithRemediation(t *testing.T, monitorCfg types.MonitorConfig, exec RemediationExecutor) (*ProblemDetector, *MockMonitor) {
	t.Helper()

	cfg := &types.NodeDoctorConfig{
		APIVersion: "v1",
		Kind:       "NodeDoctorConfig",
		Metadata:   types.ConfigMetadata{Name: "test"},
		Settings:   types.GlobalSettings{NodeName: "test-node"},
		Monitors:   []types.MonitorConfig{monitorCfg},
		Remediation: types.RemediationConfig{
			Enabled:                true,
			MaxRemediationsPerHour: 100,
			HistorySize:            50,
		},
	}
	cfg.ApplyDefaults()

	mon := NewMockMonitor(monitorCfg.Name)
	factory := NewMockMonitorFactory().AddMonitor(monitorCfg.Name, mon)

	pd, err := NewProblemDetector(cfg, nil, []types.Exporter{NewMockExporter("e")}, "config.yaml", factory, nil)
	if err != nil {
		t.Fatalf("NewProblemDetector: %v", err)
	}

	if exec != nil {
		pd.SetRemediatorRegistry(exec)
	}

	if err := pd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	t.Cleanup(func() { pd.Stop() })
	return pd, mon
}

// TestEvaluateRemediation_NoRegistrySkips verifies that no remediation call is made when
// no RemediationExecutor is attached.
func TestEvaluateRemediation_NoRegistrySkips(t *testing.T) {
	monCfg := makeRemediationConfig("my-monitor", "node-reboot")
	pd, mon := buildDetectorWithRemediation(t, monCfg, nil)

	mon.AddStatusUpdate(unhealthyStatus("my-monitor", "ServiceHealthy"))
	time.Sleep(300 * time.Millisecond)

	snap := pd.GetStatistics()
	if snap.GetRemediationsTriggered() != 0 {
		t.Errorf("expected 0 remediations triggered, got %d", snap.GetRemediationsTriggered())
	}
}

// TestEvaluateRemediation_GlobalDisabledSkips verifies that no remediation fires when
// config.Remediation.Enabled is false.
func TestEvaluateRemediation_GlobalDisabledSkips(t *testing.T) {
	monCfg := makeRemediationConfig("my-monitor", "node-reboot")

	cfg := &types.NodeDoctorConfig{
		APIVersion: "v1",
		Kind:       "NodeDoctorConfig",
		Metadata:   types.ConfigMetadata{Name: "test"},
		Settings:   types.GlobalSettings{NodeName: "test-node"},
		Monitors:   []types.MonitorConfig{monCfg},
		Remediation: types.RemediationConfig{Enabled: false},
	}
	cfg.ApplyDefaults()

	mon := NewMockMonitor("my-monitor")
	factory := NewMockMonitorFactory().AddMonitor("my-monitor", mon)
	exec := NewMockRemediationExecutor()

	pd, err := NewProblemDetector(cfg, nil, []types.Exporter{NewMockExporter("e")}, "config.yaml", factory, nil)
	if err != nil {
		t.Fatalf("NewProblemDetector: %v", err)
	}
	pd.SetRemediatorRegistry(exec)
	if err := pd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer pd.Stop()

	mon.AddStatusUpdate(unhealthyStatus("my-monitor", "ServiceHealthy"))
	time.Sleep(300 * time.Millisecond)

	if exec.CallCount() != 0 {
		t.Errorf("expected 0 executor calls with remediation disabled, got %d", exec.CallCount())
	}
}

// TestEvaluateRemediation_HealthyConditionSkips verifies ConditionTrue does not trigger
// remediation even when everything else is configured.
func TestEvaluateRemediation_HealthyConditionSkips(t *testing.T) {
	exec := NewMockRemediationExecutor()
	monCfg := makeRemediationConfig("my-monitor", "node-reboot")
	pd, mon := buildDetectorWithRemediation(t, monCfg, exec)

	mon.AddStatusUpdate(healthyStatus("my-monitor", "ServiceHealthy"))
	time.Sleep(300 * time.Millisecond)

	if exec.CallCount() != 0 {
		t.Errorf("expected 0 remediation calls for healthy condition, got %d", exec.CallCount())
	}
	snap := pd.GetStatistics()
	if snap.GetRemediationsTriggered() != 0 {
		t.Errorf("expected 0 remediationsTriggered, got %d", snap.GetRemediationsTriggered())
	}
}

// TestEvaluateRemediation_UnhealthyConditionTriggersRemediation is the happy path:
// ConditionFalse → one Remediate call with the configured strategy and correct Problem fields.
func TestEvaluateRemediation_UnhealthyConditionTriggersRemediation(t *testing.T) {
	exec := NewMockRemediationExecutor()
	monCfg := makeRemediationConfig("my-monitor", "node-reboot")
	pd, mon := buildDetectorWithRemediation(t, monCfg, exec)

	mon.AddStatusUpdate(unhealthyStatus("my-monitor", "KubeletHealthy"))
	time.Sleep(300 * time.Millisecond)

	if exec.CallCount() != 1 {
		t.Fatalf("expected 1 executor call, got %d", exec.CallCount())
	}
	call := exec.Calls()[0]
	if call.RemediatorType != "node-reboot" {
		t.Errorf("expected strategy node-reboot, got %q", call.RemediatorType)
	}
	if call.Problem.Resource != "KubeletHealthy" {
		t.Errorf("expected problem resource KubeletHealthy, got %q", call.Problem.Resource)
	}
	snap := pd.GetStatistics()
	if snap.GetRemediationsTriggered() != 1 {
		t.Errorf("expected remediationsTriggered=1, got %d", snap.GetRemediationsTriggered())
	}
}

// TestEvaluateRemediation_FailedRemediationIncrementsFailed verifies that an executor error
// increments remediationsFailed and not remediationsTriggered.
func TestEvaluateRemediation_FailedRemediationIncrementsFailed(t *testing.T) {
	exec := NewMockRemediationExecutor()
	exec.SetError(fmt.Errorf("cooldown active"))
	monCfg := makeRemediationConfig("my-monitor", "node-reboot")
	pd, mon := buildDetectorWithRemediation(t, monCfg, exec)

	mon.AddStatusUpdate(unhealthyStatus("my-monitor", "KubeletHealthy"))
	time.Sleep(300 * time.Millisecond)

	snap := pd.GetStatistics()
	if snap.GetRemediationsFailed() != 1 {
		t.Errorf("expected remediationsFailed=1, got %d", snap.GetRemediationsFailed())
	}
	if snap.GetRemediationsTriggered() != 0 {
		t.Errorf("expected remediationsTriggered=0 on failure, got %d", snap.GetRemediationsTriggered())
	}
}

// TestEvaluateRemediation_DryRunExecutorCalled verifies the dry-run path: the executor
// is still called (so IsDryRun is surfaced), and stats count the trigger.
func TestEvaluateRemediation_DryRunExecutorCalled(t *testing.T) {
	exec := NewMockRemediationExecutor()
	exec.SetDryRun(true)
	monCfg := makeRemediationConfig("my-monitor", "node-reboot")
	pd, mon := buildDetectorWithRemediation(t, monCfg, exec)

	mon.AddStatusUpdate(unhealthyStatus("my-monitor", "DiskHealthy"))
	time.Sleep(300 * time.Millisecond)

	if exec.CallCount() != 1 {
		t.Fatalf("expected 1 dry-run executor call, got %d", exec.CallCount())
	}
	if !exec.IsDryRun() {
		t.Error("expected executor.IsDryRun() == true")
	}
	snap := pd.GetStatistics()
	if snap.GetRemediationsTriggered() != 1 {
		t.Errorf("expected remediationsTriggered=1 for dry-run, got %d", snap.GetRemediationsTriggered())
	}
}

// TestEvaluateRemediation_NoMonitorRemediationConfig verifies that a monitor with
// Remediation=nil skips evaluation silently.
func TestEvaluateRemediation_NoMonitorRemediationConfig(t *testing.T) {
	exec := NewMockRemediationExecutor()
	monCfg := types.MonitorConfig{
		Name:     "bare-monitor",
		Type:     "test",
		Enabled:  true,
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		// Remediation: nil
	}
	_, mon := buildDetectorWithRemediation(t, monCfg, exec)

	mon.AddStatusUpdate(unhealthyStatus("bare-monitor", "SomethingBroke"))
	time.Sleep(300 * time.Millisecond)

	if exec.CallCount() != 0 {
		t.Errorf("expected 0 calls when monitor has no remediation config, got %d", exec.CallCount())
	}
}
