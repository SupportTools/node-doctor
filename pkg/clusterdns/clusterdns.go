// Package clusterdns derives the Kubernetes cluster-DNS probe target from resolver
// configuration and performs cluster-DNS resolution probes. It is shared by the DNS
// health monitor (pkg/monitors/network) and the overlay-test-server, which resolves
// cluster DNS from pod-network context where node-doctor's host-network context cannot.
package clusterdns

import (
	"bufio"
	"context"
	"net"
	"os"
	"strings"
	"time"
)

// ClusterProbeName returns the default in-cluster DNS probe target. It derives the
// cluster domain from the resolver's search domains and builds
// "kubernetes.default.svc.<domain>", falling back to the well-known
// "kubernetes.default.svc.cluster.local" when derivation is not possible. This prevents
// the false ClusterDNSResolutionFailed that a hardcoded cluster.local causes on clusters
// with a custom cluster domain.
func ClusterProbeName(resolverPath string) string {
	if domain, ok := DeriveClusterDomainFromResolver(resolverPath); ok {
		return "kubernetes.default.svc." + domain
	}
	return "kubernetes.default.svc.cluster.local"
}

// DeriveClusterDomainFromResolver reads resolverPath and extracts the Kubernetes
// cluster domain from its `search` line. Returns ("", false) if the file can't be read
// or no cluster domain can be identified.
func DeriveClusterDomainFromResolver(resolverPath string) (string, bool) {
	file, err := os.Open(resolverPath)
	if err != nil {
		return "", false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "search") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				return DeriveClusterDomain(fields[1:])
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", false
	}
	return "", false
}

// DeriveClusterDomain extracts the cluster domain from a list of resolver search
// domains. A Kubernetes pod's search list looks like:
//
//	<namespace>.svc.<clusterDomain>  svc.<clusterDomain>  <clusterDomain>
//
// so the cluster domain is the suffix after the "svc." label. It prefers the canonical
// "svc.<clusterDomain>" entry, then any "<ns>.svc.<clusterDomain>" entry. Returns
// ("", false) when no svc-scoped search domain is present (e.g. a non-Kubernetes
// resolver), so the caller can fall back rather than probe a bogus target.
func DeriveClusterDomain(searchDomains []string) (string, bool) {
	// Prefer the canonical middle entry: "svc.<clusterDomain>".
	for _, d := range searchDomains {
		d = strings.TrimSuffix(strings.TrimSpace(d), ".")
		if strings.HasPrefix(d, "svc.") && len(d) > len("svc.") {
			return d[len("svc."):], true
		}
	}
	// Fall back to "<namespace>.svc.<clusterDomain>".
	for _, d := range searchDomains {
		d = strings.TrimSuffix(strings.TrimSpace(d), ".")
		if _, domain, found := strings.Cut(d, ".svc."); found && domain != "" {
			return domain, true
		}
	}
	return "", false
}

// ProbeResult is the outcome of a single cluster-DNS resolution probe.
type ProbeResult struct {
	Target    string   `json:"target"`
	Resolved  bool     `json:"resolved"`
	Addresses []string `json:"addresses,omitempty"`
	LatencyMs float64  `json:"latencyMs"`
	Error     string   `json:"error,omitempty"`
}

// Probe resolves the cluster-DNS probe target (derived from resolverPath via
// ClusterProbeName) using the system resolver and reports the outcome, including
// resolution latency. It is used both by the DNS health monitor and by the
// overlay-test-server's /clusterdns endpoint, which runs in pod-network context where
// cluster DNS resolution actually works.
func Probe(ctx context.Context, resolverPath string) ProbeResult {
	target := ClusterProbeName(resolverPath)
	result := ProbeResult{Target: target}

	start := time.Now()
	addrs, err := (&net.Resolver{}).LookupHost(ctx, target)
	result.LatencyMs = float64(time.Since(start).Microseconds()) / 1000.0

	if err != nil {
		result.Resolved = false
		result.Error = err.Error()
		return result
	}

	result.Resolved = true
	result.Addresses = addrs
	return result
}
