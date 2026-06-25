package detector

import (
	"context"
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

// pollUntil polls cond every 5 ms until it returns true or the deadline expires.
// For positive assertions (expect N>0) it returns as soon as the condition is met.
// For negative assertions (expect 0) callers should sleep briefly then check directly;
// this helper is for cases where we actively wait for a state change.
func pollUntil(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if cond() {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}

// TestEvaluateRemediation_NoRegistrySkips verifies that no remediation call is made when
// no RemediationExecutor is attached.
func TestEvaluateRemediation_NoRegistrySkips(t *testing.T) {
	monCfg := makeRemediationConfig("my-monitor", "node-reboot")
	pd, mon := buildDetectorWithRemediation(t, monCfg, nil)

	mon.AddStatusUpdate(unhealthyStatus("my-monitor", "ServiceHealthy"))
	time.Sleep(50 * time.Millisecond) // brief wait: confirm no async trigger fired

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
		APIVersion:  "v1",
		Kind:        "NodeDoctorConfig",
		Metadata:    types.ConfigMetadata{Name: "test"},
		Settings:    types.GlobalSettings{NodeName: "test-node"},
		Monitors:    []types.MonitorConfig{monCfg},
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
	time.Sleep(50 * time.Millisecond) // brief wait: confirm no async trigger fired

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
	time.Sleep(50 * time.Millisecond) // brief wait: confirm no async trigger fired

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
	if !pollUntil(t, time.Second, func() bool { return exec.CallCount() == 1 }) {
		t.Fatalf("expected 1 executor call within 1s, got %d", exec.CallCount())
	}
	call := exec.Calls()[0]
	if call.RemediatorType != "node-reboot" {
		t.Errorf("expected strategy node-reboot, got %q", call.RemediatorType)
	}
	if call.Problem.Resource != "KubeletHealthy" {
		t.Errorf("expected problem resource KubeletHealthy, got %q", call.Problem.Resource)
	}
	// The triggered-stat increment lands after the executor call returns, so poll
	// for it rather than reading immediately after the CallCount poll.
	if !pollUntil(t, time.Second, func() bool {
		s := pd.GetStatistics()
		return s.GetRemediationsTriggered() == 1
	}) {
		s := pd.GetStatistics()
		t.Errorf("expected remediationsTriggered=1, got %d", s.GetRemediationsTriggered())
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
	if !pollUntil(t, time.Second, func() bool {
		s := pd.GetStatistics()
		return s.GetRemediationsFailed() == 1
	}) {
		s := pd.GetStatistics()
		t.Fatalf("expected remediationsFailed=1 within 1s, got %d", s.GetRemediationsFailed())
	}
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
	if !pollUntil(t, time.Second, func() bool { return exec.CallCount() == 1 }) {
		t.Fatalf("expected 1 dry-run executor call within 1s, got %d", exec.CallCount())
	}
	if !exec.IsDryRun() {
		t.Error("expected executor.IsDryRun() == true")
	}
	// The triggered-stat increment lands after the executor call returns, so poll
	// for it rather than reading immediately after the CallCount poll.
	if !pollUntil(t, time.Second, func() bool {
		s := pd.GetStatistics()
		return s.GetRemediationsTriggered() == 1
	}) {
		s := pd.GetStatistics()
		t.Errorf("expected remediationsTriggered=1 for dry-run, got %d", s.GetRemediationsTriggered())
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
	time.Sleep(50 * time.Millisecond) // brief wait: confirm no async trigger fired

	if exec.CallCount() != 0 {
		t.Errorf("expected 0 calls when monitor has no remediation config, got %d", exec.CallCount())
	}
}

// TestBuildStrategyList covers the ordered strategy-list building and the
// single-strategy fallback used by evaluateRemediation.
func TestBuildStrategyList(t *testing.T) {
	tests := []struct {
		name   string
		remCfg *types.MonitorRemediationConfig
		want   []string
	}{
		{
			name:   "nil config yields nil",
			remCfg: nil,
			want:   nil,
		},
		{
			name:   "single strategy only - fallback",
			remCfg: &types.MonitorRemediationConfig{Strategy: "systemd-restart"},
			want:   []string{"systemd-restart"},
		},
		{
			name: "strategies non-empty preserves order and ignores top-level Strategy",
			remCfg: &types.MonitorRemediationConfig{
				Strategy: "node-reboot", // should be ignored when Strategies present
				Strategies: []types.MonitorRemediationConfig{
					{Strategy: "systemd-restart"},
					{Strategy: "custom-script"},
					{Strategy: "node-reboot"},
				},
			},
			want: []string{"systemd-restart", "custom-script", "node-reboot"},
		},
		{
			name: "empty strategy entries are skipped",
			remCfg: &types.MonitorRemediationConfig{
				Strategy: "pod-delete",
				Strategies: []types.MonitorRemediationConfig{
					{Strategy: ""},
					{Strategy: "systemd-restart"},
					{Strategy: ""},
				},
			},
			want: []string{"systemd-restart"},
		},
		{
			name: "all empty strategy entries fall back to single Strategy",
			remCfg: &types.MonitorRemediationConfig{
				Strategy: "pod-delete",
				Strategies: []types.MonitorRemediationConfig{
					{Strategy: ""},
				},
			},
			want: []string{"pod-delete"},
		},
		{
			name:   "no strategy at all yields nil",
			remCfg: &types.MonitorRemediationConfig{},
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildStrategyList(tt.remCfg)
			if len(got) != len(tt.want) {
				t.Fatalf("buildStrategyList() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("buildStrategyList()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestEvaluateRemediation_MultiStrategyDispatch verifies that a monitor config
// with a Strategies list dispatches each strategy in order through the executor.
func TestEvaluateRemediation_MultiStrategyDispatch(t *testing.T) {
	exec := NewMockRemediationExecutor()
	// Force all attempts to "fail" so the executor walks every strategy in order.
	exec.SetError(fmt.Errorf("simulated failure"))

	monCfg := types.MonitorConfig{
		Name:     "multi-monitor",
		Type:     "test",
		Enabled:  true,
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Remediation: &types.MonitorRemediationConfig{
			Enabled:  true,
			Strategy: "node-reboot", // ignored in favor of Strategies
			Strategies: []types.MonitorRemediationConfig{
				{Strategy: "node-reboot"},
				{Strategy: "pod-delete"},
			},
		},
	}
	pd, mon := buildDetectorWithRemediation(t, monCfg, exec)

	mon.AddStatusUpdate(unhealthyStatus("multi-monitor", "ServiceHealthy"))
	if !pollUntil(t, time.Second, func() bool { return exec.CallCount() == 2 }) {
		t.Fatalf("expected 2 ordered executor calls within 1s, got %d", exec.CallCount())
	}

	calls := exec.Calls()
	if calls[0].RemediatorType != "node-reboot" {
		t.Errorf("first strategy = %q, want node-reboot", calls[0].RemediatorType)
	}
	if calls[1].RemediatorType != "pod-delete" {
		t.Errorf("second strategy = %q, want pod-delete", calls[1].RemediatorType)
	}

	// All strategies failed → counts as a failed remediation, not triggered.
	// The failed-stat increment happens in evaluateRemediation AFTER the executor
	// calls complete, so poll for it rather than reading immediately after the
	// CallCount poll (which would race the increment).
	if !pollUntil(t, time.Second, func() bool {
		snap := pd.GetStatistics()
		return snap.GetRemediationsFailed() == 1
	}) {
		snap := pd.GetStatistics()
		t.Errorf("expected remediationsFailed=1, got %d", snap.GetRemediationsFailed())
	}
	snap := pd.GetStatistics()
	if got := snap.GetRemediationsTriggered(); got != 0 {
		t.Errorf("expected remediationsTriggered=0, got %d", got)
	}
}

// TestEvaluateRemediation_MultiStrategyFirstSuccessWins verifies that when the
// first strategy succeeds, subsequent strategies are not attempted.
func TestEvaluateRemediation_MultiStrategyFirstSuccessWins(t *testing.T) {
	exec := NewMockRemediationExecutor() // no error => first attempt succeeds

	monCfg := types.MonitorConfig{
		Name:     "multi-monitor",
		Type:     "test",
		Enabled:  true,
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Remediation: &types.MonitorRemediationConfig{
			Enabled:  true,
			Strategy: "node-reboot", // required by validation; primary strategy
			Strategies: []types.MonitorRemediationConfig{
				{Strategy: "node-reboot"},
				{Strategy: "pod-delete"},
			},
		},
	}
	pd, mon := buildDetectorWithRemediation(t, monCfg, exec)

	mon.AddStatusUpdate(unhealthyStatus("multi-monitor", "ServiceHealthy"))
	if !pollUntil(t, time.Second, func() bool { return exec.CallCount() == 1 }) {
		t.Fatalf("expected exactly 1 executor call (first-success-wins) within 1s, got %d", exec.CallCount())
	}
	// Give a brief moment to ensure no second call sneaks in.
	time.Sleep(50 * time.Millisecond)
	if exec.CallCount() != 1 {
		t.Errorf("expected 1 call after first success, got %d", exec.CallCount())
	}
	if exec.Calls()[0].RemediatorType != "node-reboot" {
		t.Errorf("first strategy = %q, want node-reboot", exec.Calls()[0].RemediatorType)
	}

	snap := pd.GetStatistics()
	if snap.GetRemediationsTriggered() != 1 {
		t.Errorf("expected remediationsTriggered=1, got %d", snap.GetRemediationsTriggered())
	}
}
