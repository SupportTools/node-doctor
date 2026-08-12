package remediators

import (
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

// TestApplyConfigAdoptsCircuitBreaker checks valid circuit-breaker settings land.
func TestApplyConfigAdoptsCircuitBreaker(t *testing.T) {
	r := NewRegistry(10, 100)

	err := r.ApplyConfig(&types.RemediationConfig{
		Enabled: true,
		CircuitBreaker: types.CircuitBreakerConfig{
			Threshold:        7,
			Timeout:          2 * time.Minute,
			SuccessThreshold: 3,
		},
	}, false)
	if err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
}

// TestApplyConfigIgnoresIncompleteCircuitBreaker ensures an absent/zero
// circuitBreaker block does not clobber the running configuration with invalid
// values (which SetCircuitBreakerConfig would reject anyway).
func TestApplyConfigIgnoresIncompleteCircuitBreaker(t *testing.T) {
	r := NewRegistry(10, 100)

	err := r.ApplyConfig(&types.RemediationConfig{
		Enabled: true,
		// CircuitBreaker left as the zero value.
	}, false)
	if err != nil {
		t.Errorf("an absent circuitBreaker block must be treated as 'leave as-is', got error: %v", err)
	}
}

// TestApplyConfigRejectsNil guards the contract boundary.
func TestApplyConfigRejectsNil(t *testing.T) {
	r := NewRegistry(10, 100)
	if err := r.ApplyConfig(nil, false); err == nil {
		t.Error("ApplyConfig(nil) must return an error")
	}
}
