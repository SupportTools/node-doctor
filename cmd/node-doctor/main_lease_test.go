package main

import (
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/remediators"
	"github.com/supporttools/node-doctor/pkg/types"
)

// TestWireLeaseClient covers the small wiring helper added for TaskForge #15213.
// It is a unit test of the wireLeaseClient function only — it does not start an
// HTTP server. The lease client construction validates the config inputs and
// installs itself on the registry; that handoff is what we assert here.
func TestWireLeaseClient(t *testing.T) {
	baseConfig := func() *types.NodeDoctorConfig {
		return &types.NodeDoctorConfig{
			Settings: types.GlobalSettings{
				NodeName: "test-node",
			},
		}
	}

	enabledCoord := func() *types.RemediationCoordinationConfig {
		return &types.RemediationCoordinationConfig{
			Enabled:               true,
			ControllerURL:         "http://controller.test:8080",
			LeaseTimeout:          30 * time.Second,
			RequestTimeout:        5 * time.Second,
			FallbackOnUnreachable: true,
			MaxRetries:            3,
			RetryInterval:         1 * time.Second,
		}
	}

	t.Run("nil registry is a no-op (no panic, no error)", func(t *testing.T) {
		cfg := baseConfig()
		cfg.Remediation.Coordination = enabledCoord()
		if err := wireLeaseClient(nil, cfg); err != nil {
			t.Fatalf("expected nil error for nil registry, got %v", err)
		}
	})

	t.Run("coordination nil leaves registry without lease client", func(t *testing.T) {
		registry := remediators.NewRegistry(10, 100)
		cfg := baseConfig()
		// cfg.Remediation.Coordination is nil

		if err := wireLeaseClient(registry, cfg); err != nil {
			t.Fatalf("expected nil error when coordination is nil, got %v", err)
		}
		if registry.GetLeaseClient() != nil {
			t.Fatal("registry should have no lease client when coordination is nil")
		}
	})

	t.Run("coordination disabled leaves registry without lease client", func(t *testing.T) {
		registry := remediators.NewRegistry(10, 100)
		cfg := baseConfig()
		coord := enabledCoord()
		coord.Enabled = false
		cfg.Remediation.Coordination = coord

		if err := wireLeaseClient(registry, cfg); err != nil {
			t.Fatalf("expected nil error when coordination is disabled, got %v", err)
		}
		if registry.GetLeaseClient() != nil {
			t.Fatal("registry should have no lease client when coordination is disabled")
		}
	})

	t.Run("coordination enabled wires lease client onto registry", func(t *testing.T) {
		registry := remediators.NewRegistry(10, 100)
		cfg := baseConfig()
		cfg.Remediation.Coordination = enabledCoord()

		if err := wireLeaseClient(registry, cfg); err != nil {
			t.Fatalf("expected nil error for valid coordination config, got %v", err)
		}
		if registry.GetLeaseClient() == nil {
			t.Fatal("registry should have a lease client after wiring")
		}
	})

	t.Run("missing controller URL surfaces error (caller warns and continues)", func(t *testing.T) {
		registry := remediators.NewRegistry(10, 100)
		cfg := baseConfig()
		coord := enabledCoord()
		coord.ControllerURL = ""
		cfg.Remediation.Coordination = coord

		err := wireLeaseClient(registry, cfg)
		if err == nil {
			t.Fatal("expected error when ControllerURL is empty")
		}
		if registry.GetLeaseClient() != nil {
			t.Fatal("registry should not have a lease client when construction failed")
		}
	})

	t.Run("missing node name surfaces error", func(t *testing.T) {
		registry := remediators.NewRegistry(10, 100)
		cfg := baseConfig()
		cfg.Settings.NodeName = ""
		cfg.Remediation.Coordination = enabledCoord()

		err := wireLeaseClient(registry, cfg)
		if err == nil {
			t.Fatal("expected error when NodeName is empty")
		}
		if registry.GetLeaseClient() != nil {
			t.Fatal("registry should not have a lease client when construction failed")
		}
	})
}
