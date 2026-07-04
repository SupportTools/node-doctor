package network

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeriveClusterDomain(t *testing.T) {
	tests := []struct {
		name   string
		search []string
		want   string
		wantOK bool
	}{
		{
			name:   "standard cluster.local search list",
			search: []string{"default.svc.cluster.local", "svc.cluster.local", "cluster.local"},
			want:   "cluster.local",
			wantOK: true,
		},
		{
			name:   "custom cluster domain (the incident case)",
			search: []string{"default.svc.k8s.example.com", "svc.k8s.example.com", "k8s.example.com"},
			want:   "k8s.example.com",
			wantOK: true,
		},
		{
			name:   "only the ns-scoped entry present (no bare svc.)",
			search: []string{"kube-system.svc.cluster.local"},
			want:   "cluster.local",
			wantOK: true,
		},
		{
			name:   "trailing dots tolerated",
			search: []string{"svc.cluster.local."},
			want:   "cluster.local",
			wantOK: true,
		},
		{
			name:   "non-kubernetes resolver -> no derivation",
			search: []string{"corp.example.com", "example.com"},
			want:   "",
			wantOK: false,
		},
		{
			name:   "empty search",
			search: nil,
			want:   "",
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := deriveClusterDomain(tt.search)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("deriveClusterDomain(%v) = (%q, %v), want (%q, %v)", tt.search, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func writeResolv(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "resolv.conf")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write resolv.conf: %v", err)
	}
	return p
}

func TestDeriveClusterDomainFromResolver(t *testing.T) {
	custom := writeResolv(t, "search default.svc.k8s.example.com svc.k8s.example.com k8s.example.com\nnameserver 10.43.0.10\noptions ndots:5\n")
	if d, ok := deriveClusterDomainFromResolver(custom); !ok || d != "k8s.example.com" {
		t.Errorf("custom domain: got (%q,%v), want (k8s.example.com,true)", d, ok)
	}

	// Missing file -> no derivation, no panic.
	if d, ok := deriveClusterDomainFromResolver(filepath.Join(t.TempDir(), "nope")); ok || d != "" {
		t.Errorf("missing file: got (%q,%v), want ('',false)", d, ok)
	}

	// No search line -> no derivation.
	noSearch := writeResolv(t, "nameserver 1.1.1.1\n")
	if d, ok := deriveClusterDomainFromResolver(noSearch); ok || d != "" {
		t.Errorf("no search line: got (%q,%v), want ('',false)", d, ok)
	}
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
