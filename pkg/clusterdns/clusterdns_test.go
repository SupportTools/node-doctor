package clusterdns

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
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
			got, ok := DeriveClusterDomain(tt.search)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("DeriveClusterDomain(%v) = (%q, %v), want (%q, %v)", tt.search, got, ok, tt.want, tt.wantOK)
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
	if d, ok := DeriveClusterDomainFromResolver(custom); !ok || d != "k8s.example.com" {
		t.Errorf("custom domain: got (%q,%v), want (k8s.example.com,true)", d, ok)
	}

	// Missing file -> no derivation, no panic.
	if d, ok := DeriveClusterDomainFromResolver(filepath.Join(t.TempDir(), "nope")); ok || d != "" {
		t.Errorf("missing file: got (%q,%v), want ('',false)", d, ok)
	}

	// No search line -> no derivation.
	noSearch := writeResolv(t, "nameserver 1.1.1.1\n")
	if d, ok := DeriveClusterDomainFromResolver(noSearch); ok || d != "" {
		t.Errorf("no search line: got (%q,%v), want ('',false)", d, ok)
	}
}

func TestClusterProbeName(t *testing.T) {
	// Custom-domain cluster: derived target must use the real domain, NOT cluster.local.
	custom := writeResolv(t, "search default.svc.mesh.internal svc.mesh.internal mesh.internal\nnameserver 10.96.0.10\n")
	got := ClusterProbeName(custom)
	want := "kubernetes.default.svc.mesh.internal"
	if got != want {
		t.Errorf("ClusterProbeName(custom) = %q, want %q", got, want)
	}

	// Non-derivable resolver: fall back to the well-known cluster.local target.
	fallback := ClusterProbeName(filepath.Join(t.TempDir(), "missing"))
	if fallback != "kubernetes.default.svc.cluster.local" {
		t.Errorf("ClusterProbeName(fallback) = %q, want kubernetes.default.svc.cluster.local", fallback)
	}
}

// TestProbe is hermetic: it does not depend on real network access succeeding, since
// the test sandbox likely has no DNS/network access at all. It only asserts on the
// deterministic field-wiring, branching on whether resolution happened to succeed.
func TestProbe(t *testing.T) {
	resolv := writeResolv(t, "search default.svc.probe.test svc.probe.test probe.test\nnameserver 10.96.0.10\n")
	wantTarget := "kubernetes.default.svc.probe.test"

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result := Probe(ctx, resolv)

	if result.Target != wantTarget {
		t.Errorf("Probe().Target = %q, want %q", result.Target, wantTarget)
	}
	if result.LatencyMs < 0 {
		t.Errorf("Probe().LatencyMs = %v, want >= 0", result.LatencyMs)
	}

	if result.Resolved {
		if len(result.Addresses) == 0 {
			t.Errorf("Probe() Resolved=true but Addresses is empty")
		}
		if result.Error != "" {
			t.Errorf("Probe() Resolved=true but Error = %q, want empty", result.Error)
		}
	} else if result.Error == "" {
		t.Errorf("Probe() Resolved=false but Error is empty")
	}
}
