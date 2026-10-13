package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/supporttools/node-doctor/pkg/remediators"
	"github.com/supporttools/node-doctor/pkg/types"
)

func startupRegistry(t *testing.T, configYAML string) (*remediators.RemediatorRegistry, func(fail bool) error) {
	t.Helper()
	config := &types.NodeDoctorConfig{}
	if err := yaml.Unmarshal([]byte(configYAML), config); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	if err := config.ApplyDefaults(); err != nil {
		t.Fatalf("apply defaults: %v", err)
	}

	registry, err := newRemediatorRegistry(config)
	if err != nil {
		t.Fatalf("newRemediatorRegistry: %v", err)
	}

	base, err := remediators.NewBaseRemediator("fake", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := base.SetMaxAttempts(100); err != nil {
		t.Fatal(err)
	}
	shouldFail := true
	if err := base.SetRemediateFunc(func(context.Context, types.Problem) error {
		if shouldFail {
			return errors.New("remediation failed")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	registry.Register(remediators.RemediatorInfo{
		Type:    "fake",
		Factory: func() (types.Remediator, error) { return base, nil },
	})

	problem := types.Problem{Type: "test", Resource: "test", Severity: types.ProblemCritical, DetectedAt: time.Now()}
	return registry, func(fail bool) error {
		shouldFail = fail
		base.ClearCooldown(problem)
		return registry.Remediate(context.Background(), "fake", problem)
	}
}

func TestStartupAppliesConfiguredCircuitBreaker(t *testing.T) {
	registry, remediate := startupRegistry(t, `
remediation:
  enabled: true
  maxRemediationsPerHour: 1000
  maxRemediationsPerMinute: 1000
  circuitBreaker:
    enabled: true
    threshold: 2
    timeout: 50ms
    successThreshold: 1
`)

	_ = remediate(true)
	_ = remediate(true)
	if got := registry.GetCircuitState(); got != remediators.CircuitOpen {
		t.Fatalf("circuit state after 2 failures with threshold 2 = %v, want Open", got)
	}

	time.Sleep(100 * time.Millisecond)
	if err := remediate(false); err != nil {
		t.Fatalf("remediation after the configured 50ms timeout was blocked: %v", err)
	}
	if got := registry.GetCircuitState(); got != remediators.CircuitClosed {
		t.Errorf("circuit state after 1 success with successThreshold 1 = %v, want Closed", got)
	}
}

func TestStartupCircuitBreakerDisabled(t *testing.T) {
	registry, remediate := startupRegistry(t, `
remediation:
  enabled: true
  maxRemediationsPerHour: 1000
  maxRemediationsPerMinute: 1000
  circuitBreaker:
    enabled: false
`)

	for i := 0; i < 20; i++ {
		_ = remediate(true)
	}
	if got := registry.GetCircuitState(); got != remediators.CircuitClosed {
		t.Errorf("disabled circuit breaker opened after 20 failures: %v", got)
	}
}

func TestStartupCircuitBreakerAbsentUsesDefaults(t *testing.T) {
	registry, remediate := startupRegistry(t, `
remediation:
  enabled: true
  maxRemediationsPerHour: 1000
  maxRemediationsPerMinute: 1000
`)

	for i := 0; i < remediators.DefaultCircuitBreakerConfig.Threshold; i++ {
		_ = remediate(true)
	}
	if got := registry.GetCircuitState(); got != remediators.CircuitOpen {
		t.Errorf("circuit state after the default threshold of failures = %v, want Open", got)
	}
}
