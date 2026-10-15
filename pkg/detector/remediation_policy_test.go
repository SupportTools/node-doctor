package detector

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// These tests pin the remediation dispatch policy used for the kubelet monitor
// (cluster-services-831): restart the unit that owns kubelet at most once per
// incident, only when KubeletUnhealthy is True, only when the disk write probe
// passed recently, and never twice within the cooldown.

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func kubeletPolicyConfig() types.MonitorConfig {
	return types.MonitorConfig{
		Name:     "kubelet-health",
		Type:     "test",
		Enabled:  true,
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Remediation: &types.MonitorRemediationConfig{
			Enabled:                true,
			Strategy:               "systemd-restart",
			Service:                "rke2-agent",
			RequireServiceEnabled:  true,
			TriggerConditions:      []string{"KubeletUnhealthy"},
			MaxAttemptsPerIncident: 1,
			Cooldown:               30 * time.Minute,
			Preconditions: []types.RemediationPrecondition{{
				Type:    "ReadonlyFilesystem",
				Status:  "False",
				Monitor: "disk-health",
				MaxAge:  3 * time.Minute,
			}},
		},
	}
}

// policyDetector builds a started detector whose clock is under test control.
// Statuses are fed synchronously through processStatus so assertions are exact.
func policyDetector(t *testing.T, monCfg types.MonitorConfig, exec RemediationExecutor) (*ProblemDetector, *fakeClock) {
	t.Helper()
	pd, _ := buildDetectorWithRemediation(t, monCfg, exec)
	clock := &fakeClock{t: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)}
	pd.nowFn = clock.Now
	return pd, clock
}

func statusAt(source string, at time.Time, conds ...types.Condition) *types.Status {
	s := types.NewStatus(source)
	s.Timestamp = at
	for _, c := range conds {
		s.AddCondition(c)
	}
	return s
}

// kubeletHealthy is exactly what the kubelet monitor emits on a healthy check.
func kubeletHealthy(at time.Time) *types.Status {
	return statusAt("kubelet-health", at,
		types.NewCondition("KubeletHealthy", types.ConditionTrue, "HealthCheckPassed", "ok"),
		types.NewCondition("KubeletDown", types.ConditionFalse, "KubeletUp", "ok"),
		types.NewCondition("KubeletUnhealthy", types.ConditionFalse, "HealthCheckPassed", "ok"),
	)
}

// kubeletFailing is what the kubelet monitor emits once failureThreshold is reached.
func kubeletFailing(at time.Time) *types.Status {
	return statusAt("kubelet-health", at,
		types.NewCondition("KubeletUnhealthy", types.ConditionTrue, "RepeatedHealthCheckFailures", "kubelet down"),
	)
}

func writeProbe(at time.Time, readOnly bool) *types.Status {
	st, reason := types.ConditionFalse, "WriteProbeOK"
	if readOnly {
		st, reason = types.ConditionTrue, "WriteProbeFailed"
	}
	return statusAt("disk-health", at, types.NewCondition("ReadonlyFilesystem", st, reason, ""))
}

// The legacy rule (any False condition triggers) fires on a HEALTHY kubelet,
// because the kubelet monitor reports KubeletDown=False and KubeletUnhealthy=False
// on every passing check. This is why the kubelet monitor must use TriggerConditions.
func TestRemediationPolicy_LegacyRuleWouldRestartHealthyKubelet(t *testing.T) {
	exec := NewMockRemediationExecutor()
	cfg := makeRemediationConfig("kubelet-health", "systemd-restart")
	cfg.Remediation.Service = "rke2-agent"
	pd, clock := policyDetector(t, cfg, exec)

	pd.processStatus(kubeletHealthy(clock.Now()))

	if exec.CallCount() == 0 {
		t.Fatal("expected the legacy any-False rule to dispatch on a healthy kubelet status; " +
			"if this changed, revisit why TriggerConditions exists")
	}
}

func TestRemediationPolicy_HealthyKubeletNeverRemediates(t *testing.T) {
	exec := NewMockRemediationExecutor()
	pd, clock := policyDetector(t, kubeletPolicyConfig(), exec)

	pd.processStatus(writeProbe(clock.Now(), false))
	for i := 0; i < 20; i++ {
		clock.Advance(30 * time.Second)
		pd.processStatus(writeProbe(clock.Now(), false))
		pd.processStatus(kubeletHealthy(clock.Now()))
	}
	if exec.CallCount() != 0 {
		t.Fatalf("healthy kubelet dispatched %d remediation(s), want 0", exec.CallCount())
	}
}

func TestRemediationPolicy_OneRestartPerIncident(t *testing.T) {
	exec := NewMockRemediationExecutor()
	pd, clock := policyDetector(t, kubeletPolicyConfig(), exec)

	pd.processStatus(writeProbe(clock.Now(), false))
	for i := 0; i < 10; i++ {
		pd.processStatus(kubeletFailing(clock.Now()))
		clock.Advance(30 * time.Second)
		pd.processStatus(writeProbe(clock.Now(), false))
	}

	if got := exec.CallCount(); got != 1 {
		t.Fatalf("got %d dispatches in one incident, want exactly 1", got)
	}
	call := exec.Calls()[0]
	if call.RemediatorType != "systemd-restart" {
		t.Errorf("strategy = %q, want systemd-restart", call.RemediatorType)
	}
	if call.Problem.Resource != "KubeletUnhealthy" {
		t.Errorf("problem resource = %q, want KubeletUnhealthy", call.Problem.Resource)
	}
	if call.Problem.Metadata["service"] != "rke2-agent" {
		t.Errorf("metadata service = %q, want rke2-agent", call.Problem.Metadata["service"])
	}
	if call.Problem.Metadata["requireEnabled"] != "true" {
		t.Errorf("metadata requireEnabled = %q, want true", call.Problem.Metadata["requireEnabled"])
	}
}

// A restart that fails still uses up the incident's single attempt.
func TestRemediationPolicy_FailedRestartIsNotRetried(t *testing.T) {
	exec := NewMockRemediationExecutor()
	exec.SetError(errors.New("systemctl restart failed"))
	pd, clock := policyDetector(t, kubeletPolicyConfig(), exec)

	pd.processStatus(writeProbe(clock.Now(), false))
	for i := 0; i < 5; i++ {
		pd.processStatus(kubeletFailing(clock.Now()))
		clock.Advance(time.Minute) // well past any per-minute limit, still one incident
		pd.processStatus(writeProbe(clock.Now(), false))
	}
	if got := exec.CallCount(); got != 1 {
		t.Fatalf("got %d dispatches after a failed restart, want 1 (no retry within the incident)", got)
	}
}

func TestRemediationPolicy_NoRestartOnReadOnlyDisk(t *testing.T) {
	exec := NewMockRemediationExecutor()
	pd, clock := policyDetector(t, kubeletPolicyConfig(), exec)

	for i := 0; i < 5; i++ {
		pd.processStatus(writeProbe(clock.Now(), true))
		pd.processStatus(kubeletFailing(clock.Now()))
		clock.Advance(30 * time.Second)
	}
	if exec.CallCount() != 0 {
		t.Fatalf("restart dispatched while the write probe failed (%d calls)", exec.CallCount())
	}

	// A withheld dispatch does not use up the attempt: once the probe passes, the
	// same incident gets its one restart.
	pd.processStatus(writeProbe(clock.Now(), false))
	pd.processStatus(kubeletFailing(clock.Now()))
	if exec.CallCount() != 1 {
		t.Fatalf("got %d dispatches after the write probe recovered, want 1", exec.CallCount())
	}
}

func TestRemediationPolicy_NoRestartWithoutFreshWriteProbe(t *testing.T) {
	t.Run("never observed", func(t *testing.T) {
		exec := NewMockRemediationExecutor()
		pd, clock := policyDetector(t, kubeletPolicyConfig(), exec)
		pd.processStatus(kubeletFailing(clock.Now()))
		if exec.CallCount() != 0 {
			t.Fatalf("restart dispatched with no write probe result (%d calls)", exec.CallCount())
		}
	})
	t.Run("stale", func(t *testing.T) {
		exec := NewMockRemediationExecutor()
		pd, clock := policyDetector(t, kubeletPolicyConfig(), exec)
		pd.processStatus(writeProbe(clock.Now(), false))
		clock.Advance(4 * time.Minute) // maxAge is 3m: a hung probe stops reporting
		pd.processStatus(kubeletFailing(clock.Now()))
		if exec.CallCount() != 0 {
			t.Fatalf("restart dispatched on a 4m-old write probe result (%d calls)", exec.CallCount())
		}
	})
	t.Run("from another monitor", func(t *testing.T) {
		exec := NewMockRemediationExecutor()
		pd, clock := policyDetector(t, kubeletPolicyConfig(), exec)
		pd.processStatus(statusAt("some-other-monitor", clock.Now(),
			types.NewCondition("ReadonlyFilesystem", types.ConditionFalse, "x", "")))
		pd.processStatus(kubeletFailing(clock.Now()))
		if exec.CallCount() != 0 {
			t.Fatalf("precondition scoped to disk-health matched another monitor (%d calls)", exec.CallCount())
		}
	})
}

func TestRemediationPolicy_NewIncidentRespectsCooldown(t *testing.T) {
	exec := NewMockRemediationExecutor()
	pd, clock := policyDetector(t, kubeletPolicyConfig(), exec)

	// Incident 1: one restart, then kubelet recovers.
	pd.processStatus(writeProbe(clock.Now(), false))
	pd.processStatus(kubeletFailing(clock.Now()))
	clock.Advance(time.Minute)
	pd.processStatus(kubeletHealthy(clock.Now()))

	// Incident 2 starts 10m after the restart: inside the 30m cooldown.
	clock.Advance(9 * time.Minute)
	pd.processStatus(writeProbe(clock.Now(), false))
	pd.processStatus(kubeletFailing(clock.Now()))
	if exec.CallCount() != 1 {
		t.Fatalf("got %d dispatches, want 1: the second incident is inside the cooldown", exec.CallCount())
	}

	// Still failing after the cooldown has passed: the incident has not used its
	// attempt yet, so it now gets one.
	clock.Advance(21 * time.Minute)
	pd.processStatus(writeProbe(clock.Now(), false))
	pd.processStatus(kubeletFailing(clock.Now()))
	if exec.CallCount() != 2 {
		t.Fatalf("got %d dispatches, want 2 once the cooldown expired", exec.CallCount())
	}

	// And only one for that incident.
	clock.Advance(time.Hour)
	pd.processStatus(writeProbe(clock.Now(), false))
	pd.processStatus(kubeletFailing(clock.Now()))
	if exec.CallCount() != 2 {
		t.Fatalf("got %d dispatches, want 2: incident 2 already used its attempt", exec.CallCount())
	}
}

// Recovery ends the incident: the next incident (outside the cooldown) gets one restart.
func TestRemediationPolicy_RecoveryResetsIncident(t *testing.T) {
	exec := NewMockRemediationExecutor()
	pd, clock := policyDetector(t, kubeletPolicyConfig(), exec)

	for incident := 1; incident <= 3; incident++ {
		pd.processStatus(writeProbe(clock.Now(), false))
		pd.processStatus(kubeletFailing(clock.Now()))
		pd.processStatus(kubeletFailing(clock.Now()))
		clock.Advance(time.Minute)
		pd.processStatus(kubeletHealthy(clock.Now()))
		clock.Advance(time.Hour)
		if exec.CallCount() != incident {
			t.Fatalf("after incident %d: got %d dispatches, want %d", incident, exec.CallCount(), incident)
		}
	}
}
