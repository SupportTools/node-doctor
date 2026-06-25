package remediators

import (
	"testing"

	"k8s.io/client-go/kubernetes/fake"
)

func hasType(types []string, want string) bool {
	for _, t := range types {
		if t == want {
			return true
		}
	}
	return false
}

func TestRegisterClusterRemediators_RegistersWhenClientPresent(t *testing.T) {
	registry := NewRegistry(10, 100)
	client := fake.NewSimpleClientset()

	RegisterClusterRemediators(registry, nil, client, testNodeName, "node-doctor-xyz", "monitoring")

	got := registry.GetRegisteredTypes()
	if !hasType(got, StrategyNodeReboot) {
		t.Fatalf("expected %q registered, got %v", StrategyNodeReboot, got)
	}
	if !hasType(got, StrategyPodDelete) {
		t.Fatalf("expected %q registered, got %v", StrategyPodDelete, got)
	}

	// Factories must build successfully.
	if _, err := registry.getOrCreateRemediator(StrategyNodeReboot); err != nil {
		t.Fatalf("create node-reboot: %v", err)
	}
	if _, err := registry.getOrCreateRemediator(StrategyPodDelete); err != nil {
		t.Fatalf("create pod-delete: %v", err)
	}
}

func TestRegisterClusterRemediators_NilClientRegistersNothing(t *testing.T) {
	registry := NewRegistry(10, 100)

	RegisterClusterRemediators(registry, nil, nil, testNodeName, "", "")

	got := registry.GetRegisteredTypes()
	if hasType(got, StrategyNodeReboot) || hasType(got, StrategyPodDelete) {
		t.Fatalf("expected NO destructive remediators with nil client, got %v", got)
	}
}

func TestRegisterClusterRemediators_EmptyNodeNameRegistersNothing(t *testing.T) {
	registry := NewRegistry(10, 100)
	client := fake.NewSimpleClientset()

	RegisterClusterRemediators(registry, nil, client, "", "", "")

	got := registry.GetRegisteredTypes()
	if hasType(got, StrategyNodeReboot) || hasType(got, StrategyPodDelete) {
		t.Fatalf("expected NO destructive remediators with empty node name, got %v", got)
	}
}

func TestRegisterClusterRemediators_DryRunPropagates(t *testing.T) {
	registry := NewRegistry(10, 100)
	registry.SetDryRun(true)
	client := fake.NewSimpleClientset()

	RegisterClusterRemediators(registry, nil, client, testNodeName, "", "")

	rem, err := registry.getOrCreateRemediator(StrategyNodeReboot)
	if err != nil {
		t.Fatalf("create node-reboot: %v", err)
	}
	nr, ok := rem.(*NodeRebootRemediator)
	if !ok {
		t.Fatalf("unexpected type %T", rem)
	}
	if !nr.config.DryRun {
		t.Fatalf("expected node-reboot remediator to inherit dry-run=true from registry")
	}
}
