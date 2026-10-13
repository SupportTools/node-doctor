package remediators

import (
	"context"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// TestApplyConfigAdoptsDryRun is the kill-switch guard: flipping dryRun in the
// ConfigMap during an incident must take effect on the RUNNING registry, not
// only after a pod restart (#node-doctor-243).
func TestApplyConfigAdoptsDryRun(t *testing.T) {
	r := NewRegistry(10, 100)
	r.SetDryRun(false)

	if r.IsDryRun() {
		t.Fatal("test setup: registry should start out of dry-run")
	}

	err := r.ApplyConfig(&types.RemediationConfig{
		Enabled:                true,
		DryRun:                 true,
		MaxRemediationsPerHour: 5,
	}, false)
	if err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}

	if !r.IsDryRun() {
		t.Error("flipping remediation.dryRun must take effect immediately on the running registry")
	}
}

// TestApplyConfigRespectsProcessWideDryRun ensures the -dry-run flag / global
// settings.dryRunMode cannot be cleared by a ConfigMap edit.
func TestApplyConfigRespectsProcessWideDryRun(t *testing.T) {
	r := NewRegistry(10, 100)

	err := r.ApplyConfig(&types.RemediationConfig{
		Enabled: true,
		DryRun:  false, // config says "live"
	}, true) // ...but the process is globally in dry-run
	if err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}

	if !r.IsDryRun() {
		t.Error("process-wide dry-run must win over a config that sets dryRun:false")
	}
}

// TestApplyConfigAdoptsRateLimits verifies the per-hour and per-minute caps are
// re-applied, since lowering them is the other lever operators pull mid-incident.
func TestApplyConfigAdoptsRateLimits(t *testing.T) {
	r := NewRegistry(100, 100)

	err := r.ApplyConfig(&types.RemediationConfig{
		Enabled:                  true,
		MaxRemediationsPerHour:   3,
		MaxRemediationsPerMinute: 1,
	}, false)
	if err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}

	stats := r.GetStats()
	if stats.MaxPerHour != 3 {
		t.Errorf("maxRemediationsPerHour must be adopted, got %d want 3", stats.MaxPerHour)
	}
}

// TestSetMaxRemediationsPerHour covers the new setter directly, including the
// "0 disables the check" contract inherited from the constructor.
func TestSetMaxRemediationsPerHour(t *testing.T) {
	r := NewRegistry(10, 100)

	r.SetMaxRemediationsPerHour(4)
	if got := r.GetStats().MaxPerHour; got != 4 {
		t.Errorf("MaxPerHour = %d, want 4", got)
	}

	r.SetMaxRemediationsPerHour(0)
	if got := r.GetStats().MaxPerHour; got != 0 {
		t.Errorf("MaxPerHour = %d, want 0 (disabled)", got)
	}

	// Negative values are clamped rather than corrupting the window check.
	r.SetMaxRemediationsPerHour(-5)
	if got := r.GetStats().MaxPerHour; got != 0 {
		t.Errorf("negative MaxPerHour must clamp to 0, got %d", got)
	}
}

// newFailingRegistry returns a registry with a remediator that fails its first
// 10 calls, and a func that runs n remediations against it.
func newFailingRegistry() (*RemediatorRegistry, func(n int)) {
	r := NewRegistry(100, 100)
	mock := newMockRemediator("failing", true)
	r.Register(RemediatorInfo{
		Type:    "failing",
		Factory: func() (types.Remediator, error) { return mock, nil },
	})
	problem := createTestProblem("test-type", "test-resource")
	return r, func(n int) {
		for i := 0; i < n; i++ {
			mock.ClearCooldown(problem)
			_ = r.Remediate(context.Background(), "failing", problem)
		}
	}
}

func TestApplyConfigAdoptsCircuitBreaker(t *testing.T) {
	r, fail := newFailingRegistry()

	err := r.ApplyConfig(&types.RemediationConfig{
		Enabled: true,
		CircuitBreaker: types.CircuitBreakerConfig{
			Threshold:        2,
			Timeout:          time.Hour,
			SuccessThreshold: 1,
		},
	}, false)
	if err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}

	fail(2)
	if got := r.GetCircuitState(); got != CircuitOpen {
		t.Errorf("circuit state after 2 failures with threshold 2 = %v, want Open", got)
	}
}

func TestApplyConfigAbsentCircuitBreakerUsesDefaults(t *testing.T) {
	r, fail := newFailingRegistry()
	if err := r.SetCircuitBreakerConfig(CircuitBreakerConfig{Threshold: 1, Timeout: time.Hour, SuccessThreshold: 1}); err != nil {
		t.Fatal(err)
	}

	if err := r.ApplyConfig(&types.RemediationConfig{Enabled: true}, false); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}

	fail(DefaultCircuitBreakerConfig.Threshold - 1)
	if got := r.GetCircuitState(); got != CircuitClosed {
		t.Fatalf("circuit opened before the default threshold: %v", got)
	}
	fail(1)
	if got := r.GetCircuitState(); got != CircuitOpen {
		t.Errorf("circuit state at the default threshold = %v, want Open", got)
	}
}

func TestApplyConfigCircuitBreakerDisabled(t *testing.T) {
	r, fail := newFailingRegistry()
	if err := r.SetCircuitBreakerConfig(CircuitBreakerConfig{Threshold: 1, Timeout: time.Hour, SuccessThreshold: 1}); err != nil {
		t.Fatal(err)
	}
	fail(1)
	if got := r.GetCircuitState(); got != CircuitOpen {
		t.Fatalf("test setup: circuit state = %v, want Open", got)
	}

	disabled := false
	err := r.ApplyConfig(&types.RemediationConfig{
		Enabled:        true,
		CircuitBreaker: types.CircuitBreakerConfig{Enabled: &disabled},
	}, false)
	if err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	if got := r.GetCircuitState(); got != CircuitClosed {
		t.Errorf("disabling the breaker must close an open circuit, got %v", got)
	}

	fail(9)
	if got := r.GetCircuitState(); got != CircuitClosed {
		t.Errorf("disabled breaker opened after 9 more failures: %v", got)
	}
}

// TestApplyConfigRejectsNil guards the contract boundary.
func TestApplyConfigRejectsNil(t *testing.T) {
	r := NewRegistry(10, 100)
	if err := r.ApplyConfig(nil, false); err == nil {
		t.Error("ApplyConfig(nil) must return an error")
	}
}
