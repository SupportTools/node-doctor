package network

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/supporttools/node-doctor/pkg/monitors"
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

// TestDeriveClusterDomainFromResolvConf is the table-driven contract for deriving the
// cluster domain from an INJECTED resolv.conf path (never the real /etc/resolv.conf),
// covering the standard domain, a custom cluster-domain, malformed/missing files, and an
// empty search list. Cases that cannot be derived must fall back to cluster.local rather
// than probing a bogus name.
func TestDeriveClusterDomainFromResolvConf(t *testing.T) {
	tests := []struct {
		name string
		// resolv is the resolv.conf content; when writeFile is false the path is left
		// nonexistent to exercise the unreadable-file path.
		resolv    string
		writeFile bool
		want      string
	}{
		{
			name:      "standard cluster.local search list",
			resolv:    "search default.svc.cluster.local svc.cluster.local cluster.local\nnameserver 10.43.0.10\noptions ndots:5\n",
			writeFile: true,
			want:      "kubernetes.default.svc.cluster.local",
		},
		{
			name:      "custom cluster domain (RKE2 cluster-domain, the incident case)",
			resolv:    "search default.svc.a1-ops-prd.local svc.a1-ops-prd.local a1-ops-prd.local\nnameserver 10.43.0.10\noptions ndots:5\n",
			writeFile: true,
			want:      "kubernetes.default.svc.a1-ops-prd.local",
		},
		{
			name:      "missing resolv.conf falls back to cluster.local",
			writeFile: false,
			want:      "kubernetes.default.svc.cluster.local",
		},
		{
			name:      "malformed resolv.conf falls back to cluster.local",
			resolv:    "this is not a resolv.conf\n\x00\x01garbage!!\nsearchsomething\n",
			writeFile: true,
			want:      "kubernetes.default.svc.cluster.local",
		},
		{
			name:      "empty search list falls back to cluster.local",
			resolv:    "search\nnameserver 10.43.0.10\n",
			writeFile: true,
			want:      "kubernetes.default.svc.cluster.local",
		},
		{
			name:      "no search line at all falls back to cluster.local",
			resolv:    "nameserver 1.1.1.1\noptions ndots:5\n",
			writeFile: true,
			want:      "kubernetes.default.svc.cluster.local",
		},
		{
			name:      "non-Kubernetes search list falls back to cluster.local",
			resolv:    "search corp.example.com example.com\nnameserver 1.1.1.1\n",
			writeFile: true,
			want:      "kubernetes.default.svc.cluster.local",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "resolv.conf")
			if tt.writeFile {
				if err := os.WriteFile(path, []byte(tt.resolv), 0o644); err != nil {
					t.Fatalf("write resolv.conf: %v", err)
				}
			}

			// The path is injected, so this test never touches the host's real
			// /etc/resolv.conf and is safe to run anywhere.
			c := &DNSMonitorConfig{ResolverPath: path}
			if err := c.applyDefaults(); err != nil {
				t.Fatalf("applyDefaults: %v", err)
			}

			if len(c.ClusterDomains) != 1 || c.ClusterDomains[0] != tt.want {
				t.Errorf("derived ClusterDomains = %v, want [%q]", c.ClusterDomains, tt.want)
			}
		})
	}
}

// TestExplicitClusterDomainsBeatDerivation pins that operator-supplied config always wins
// over runtime derivation, in both directions: an explicit non-empty list is used verbatim
// even when the resolver would derive something else, and an explicit empty list keeps the
// cluster-DNS check disabled.
func TestExplicitClusterDomainsBeatDerivation(t *testing.T) {
	// A resolver that WOULD derive kubernetes.default.svc.derived.zone.
	resolv := writeResolv(t, "search default.svc.derived.zone svc.derived.zone derived.zone\nnameserver 10.96.0.10\n")

	tests := []struct {
		name       string
		configured []string
		want       []string
	}{
		{
			name:       "explicit single domain wins over derivation",
			configured: []string{"kubernetes.default.svc.operator.choice"},
			want:       []string{"kubernetes.default.svc.operator.choice"},
		},
		{
			name:       "explicit multiple domains win over derivation",
			configured: []string{"kubernetes.default.svc.one.zone", "kube-dns.kube-system.svc.two.zone"},
			want:       []string{"kubernetes.default.svc.one.zone", "kube-dns.kube-system.svc.two.zone"},
		},
		{
			name:       "explicit empty list keeps the cluster check disabled",
			configured: []string{},
			want:       []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &DNSMonitorConfig{ResolverPath: resolv, ClusterDomains: tt.configured}
			if err := c.applyDefaults(); err != nil {
				t.Fatalf("applyDefaults: %v", err)
			}

			if len(c.ClusterDomains) != len(tt.want) {
				t.Fatalf("ClusterDomains = %v, want %v", c.ClusterDomains, tt.want)
			}
			for i, want := range tt.want {
				if c.ClusterDomains[i] != want {
					t.Errorf("ClusterDomains[%d] = %q, want %q", i, c.ClusterDomains[i], want)
				}
			}
		})
	}
}

// TestRegistryDefaultConfigDoesNotPinClusterDomain guards the auto-default path. The
// registry DefaultConfig map is injected verbatim by ApplyDefaultMonitors for any config
// that omits network-dns-check, and anything set there counts as EXPLICIT config that
// beats derivation. A hardcoded "kubernetes.default.svc.cluster.local" in that map is the
// exact footgun that NXDOMAINs fleet-wide on a custom cluster-domain, so the key must stay
// absent and let applyDefaults derive it.
func TestRegistryDefaultConfigDoesNotPinClusterDomain(t *testing.T) {
	info := monitors.GetMonitorInfo("network-dns-check")
	if info == nil || info.DefaultConfig == nil {
		t.Fatal("network-dns-check has no registered DefaultConfig")
	}

	if val, present := info.DefaultConfig.Config["clusterDomains"]; present {
		t.Errorf("registry DefaultConfig pins clusterDomains=%v; it must be absent so the "+
			"cluster domain is derived from the resolver search list at runtime", val)
	}

	// The auto-default must still be a valid config (validation requires at least one of
	// clusterDomains/externalDomains/customQueries) — externalDomains carries it.
	if err := ValidateDNSConfig(*info.DefaultConfig); err != nil {
		t.Errorf("registry DefaultConfig fails validation: %v", err)
	}

	// End-to-end: parsing the registry default leaves ClusterDomains nil, so applyDefaults
	// derives the real domain from the injected resolver rather than assuming cluster.local.
	parsed, err := parseDNSConfig(info.DefaultConfig.Config)
	if err != nil {
		t.Fatalf("parseDNSConfig(DefaultConfig): %v", err)
	}
	if parsed.ClusterDomains != nil {
		t.Fatalf("parsed ClusterDomains = %v, want nil so derivation applies", parsed.ClusterDomains)
	}

	parsed.ResolverPath = writeResolv(t, "search default.svc.a1-ops-prd.local svc.a1-ops-prd.local a1-ops-prd.local\nnameserver 10.43.0.10\n")
	if err := parsed.applyDefaults(); err != nil {
		t.Fatalf("applyDefaults: %v", err)
	}
	want := "kubernetes.default.svc.a1-ops-prd.local"
	if len(parsed.ClusterDomains) != 1 || parsed.ClusterDomains[0] != want {
		t.Errorf("auto-defaulted ClusterDomains = %v, want [%q]", parsed.ClusterDomains, want)
	}
}
