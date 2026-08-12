package detector

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/supporttools/node-doctor/pkg/reload"
	"github.com/supporttools/node-doctor/pkg/types"
)

var errApplyBoom = errors.New("apply failed")

// createTestConfigForReload builds a config with remediation enabled so that
// remediation-diff behaviour can be exercised.
func createTestConfigForReload() *types.NodeDoctorConfig {
	cfg := CreateTestConfigWithMonitors([]types.MonitorConfig{
		{
			Name:           "test-monitor",
			Type:           "mock",
			Enabled:        true,
			IntervalString: "30s",
			TimeoutString:  "10s",
		},
	})
	cfg.Remediation = types.RemediationConfig{
		Enabled:                true,
		DryRun:                 false,
		MaxRemediationsPerHour: 10,
	}
	_ = cfg.ApplyDefaults()
	return cfg
}

// reconfigurableExecutor is a RemediationExecutor that also implements
// ReconfigurableRemediationExecutor, recording every ApplyConfig call.
type reconfigurableExecutor struct {
	mu       sync.Mutex
	applied  []types.RemediationConfig
	dryRuns  []bool
	dryRun   bool
	applyErr error
}

func (e *reconfigurableExecutor) Remediate(_ context.Context, _ string, _ types.Problem) error {
	return nil
}
func (e *reconfigurableExecutor) RemediateWithStrategies(_ context.Context, _ []string, _ types.Problem) error {
	return nil
}
func (e *reconfigurableExecutor) IsDryRun() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.dryRun
}
func (e *reconfigurableExecutor) ApplyConfig(cfg *types.RemediationConfig, dryRunMode bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.applyErr != nil {
		return e.applyErr
	}
	e.applied = append(e.applied, *cfg)
	e.dryRuns = append(e.dryRuns, dryRunMode)
	e.dryRun = cfg.DryRun || dryRunMode
	return nil
}
func (e *reconfigurableExecutor) appliedCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.applied)
}
func (e *reconfigurableExecutor) lastApplied() types.RemediationConfig {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.applied[len(e.applied)-1]
}

// plainExecutor implements only RemediationExecutor — no in-place reconfiguration.
type plainExecutor struct{ dryRun bool }

func (e *plainExecutor) Remediate(_ context.Context, _ string, _ types.Problem) error { return nil }
func (e *plainExecutor) RemediateWithStrategies(_ context.Context, _ []string, _ types.Problem) error {
	return nil
}
func (e *plainExecutor) IsDryRun() bool { return e.dryRun }

// remediationReloadDetector builds a started detector wired to the given executor.
func remediationReloadDetector(t *testing.T, executor RemediationExecutor) (*ProblemDetector, *types.NodeDoctorConfig) {
	t.Helper()

	cfg := createTestConfigForReload()
	helper := NewReloadTestHelper(t)
	helper.Setup(t, cfg)
	t.Cleanup(func() { _ = helper.detector.Stop() })

	helper.detector.SetRemediatorRegistry(executor)
	if err := helper.detector.Start(); err != nil {
		t.Fatalf("start detector: %v", err)
	}
	return helper.detector, cfg
}

// TestRemediationConfigIsAppliedOnReload is the core #node-doctor-243 guard.
//
// diff.RemediationChanged was computed and then dropped on the floor: an
// operator who edited the ConfigMap mid-incident to flip dryRun on, or to lower
// maxRemediationsPerHour, got a "reload succeeded" event while the registry kept
// remediating under the OLD settings until the pod was restarted.
func TestRemediationConfigIsAppliedOnReload(t *testing.T) {
	executor := &reconfigurableExecutor{}
	det, cfg := remediationReloadDetector(t, executor)

	newConfig := createTestConfigForReload()
	newConfig.Remediation.Enabled = true
	newConfig.Remediation.DryRun = true // the incident kill-switch
	newConfig.Remediation.MaxRemediationsPerHour = 2

	diff := reload.ComputeConfigDiff(cfg, newConfig)
	if !diff.RemediationChanged {
		t.Fatalf("test setup: expected the remediation diff to be flagged, got %+v", diff)
	}

	if err := det.applyConfigReload(context.Background(), newConfig, diff); err != nil {
		t.Fatalf("applyConfigReload: %v", err)
	}

	if executor.appliedCount() != 1 {
		t.Fatalf("remediation config change must be applied to the running executor exactly once, got %d calls. "+
			"A ConfigMap edit to dryRun/rate limits would otherwise be silently ignored until a pod restart.",
			executor.appliedCount())
	}
	applied := executor.lastApplied()
	if !applied.DryRun {
		t.Error("the new dryRun value must reach the executor")
	}
	if applied.MaxRemediationsPerHour != 2 {
		t.Errorf("the new maxRemediationsPerHour must reach the executor, got %d", applied.MaxRemediationsPerHour)
	}
	if !executor.IsDryRun() {
		t.Error("the executor must actually be in dry-run mode after the reload")
	}
}

// TestRemediationConfigNotAppliedWhenUnchanged avoids pointless churn: an
// unrelated edit must not reconfigure the remediator.
func TestRemediationConfigNotAppliedWhenUnchanged(t *testing.T) {
	executor := &reconfigurableExecutor{}
	det, cfg := remediationReloadDetector(t, executor)

	newConfig := createTestConfigForReload()
	// Identical remediation block; only a monitor differs.
	newConfig.Monitors[0].IntervalString = "45s"
	if err := newConfig.ApplyDefaults(); err != nil {
		t.Fatal(err)
	}

	diff := reload.ComputeConfigDiff(cfg, newConfig)
	if diff.RemediationChanged {
		t.Skip("test setup: remediation unexpectedly differs")
	}

	if err := det.applyConfigReload(context.Background(), newConfig, diff); err != nil {
		t.Fatalf("applyConfigReload: %v", err)
	}
	if executor.appliedCount() != 0 {
		t.Errorf("remediation must not be reconfigured when its config did not change, got %d calls",
			executor.appliedCount())
	}
}

// TestRemediationReloadFailureIsCritical ensures a failed in-place
// reconfiguration aborts the reload rather than half-applying it.
func TestRemediationReloadFailureIsCritical(t *testing.T) {
	executor := &reconfigurableExecutor{applyErr: errApplyBoom}
	det, cfg := remediationReloadDetector(t, executor)

	newConfig := createTestConfigForReload()
	newConfig.Remediation.Enabled = true
	newConfig.Remediation.DryRun = true
	newConfig.Remediation.MaxRemediationsPerHour = 2

	diff := reload.ComputeConfigDiff(cfg, newConfig)
	err := det.applyConfigReload(context.Background(), newConfig, diff)
	if err == nil {
		t.Fatal("a failed remediation reconfiguration must fail the reload, not be swallowed")
	}
}

// TestRemediationReloadWithNonReconfigurableExecutorDoesNotPanic covers the
// honest-degradation path: an executor that cannot be reconfigured in place is
// left alone and the operator is warned (rather than the reload exploding or
// silently claiming success).
func TestRemediationReloadWithNonReconfigurableExecutorDoesNotPanic(t *testing.T) {
	det, cfg := remediationReloadDetector(t, &plainExecutor{})

	newConfig := createTestConfigForReload()
	newConfig.Remediation.Enabled = true
	newConfig.Remediation.DryRun = true
	newConfig.Remediation.MaxRemediationsPerHour = 2

	diff := reload.ComputeConfigDiff(cfg, newConfig)
	if err := det.applyConfigReload(context.Background(), newConfig, diff); err != nil {
		t.Fatalf("a non-reconfigurable executor must not fail the reload: %v", err)
	}
}
