package network

import (
	"os"
	"path/filepath"
	"testing"
)

func writeResolv(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "resolv.conf")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write resolv.conf: %v", err)
	}
	return p
}

func TestDefaultClusterDomains(t *testing.T) {
	// Custom-domain cluster: derived target must use the real domain, NOT cluster.local.
	custom := writeResolv(t, "search default.svc.mesh.internal svc.mesh.internal mesh.internal\nnameserver 10.96.0.10\n")
	got := defaultClusterDomains(custom)
	want := "kubernetes.default.svc.mesh.internal"
	if len(got) != 1 || got[0] != want {
		t.Errorf("defaultClusterDomains(custom) = %v, want [%q]", got, want)
	}

	// Non-derivable resolver: fall back to the well-known cluster.local target.
	fallback := defaultClusterDomains(filepath.Join(t.TempDir(), "missing"))
	if len(fallback) != 1 || fallback[0] != "kubernetes.default.svc.cluster.local" {
		t.Errorf("defaultClusterDomains(fallback) = %v, want [kubernetes.default.svc.cluster.local]", fallback)
	}
}

// TestApplyDefaultsDerivesClusterDomain verifies the config wiring: a nil ClusterDomains
// gets the derived default, while an explicit empty slice (cluster DNS disabled) is left
// untouched — the property that keeps this change from re-enabling the check on a fleet
// that intentionally set clusterDomains: [].
func TestApplyDefaultsDerivesClusterDomain(t *testing.T) {
	resolv := writeResolv(t, "search default.svc.custom.zone svc.custom.zone custom.zone\nnameserver 10.96.0.10\n")

	// nil -> derived
	c := &DNSMonitorConfig{ResolverPath: resolv}
	if err := c.applyDefaults(); err != nil {
		t.Fatalf("applyDefaults: %v", err)
	}
	if len(c.ClusterDomains) != 1 || c.ClusterDomains[0] != "kubernetes.default.svc.custom.zone" {
		t.Errorf("nil ClusterDomains derived = %v, want [kubernetes.default.svc.custom.zone]", c.ClusterDomains)
	}

	// explicit empty slice -> left disabled (NOT re-enabled)
	disabled := &DNSMonitorConfig{ResolverPath: resolv, ClusterDomains: []string{}}
	if err := disabled.applyDefaults(); err != nil {
		t.Fatalf("applyDefaults: %v", err)
	}
	if len(disabled.ClusterDomains) != 0 {
		t.Errorf("explicit empty ClusterDomains should stay empty, got %v", disabled.ClusterDomains)
	}
}
