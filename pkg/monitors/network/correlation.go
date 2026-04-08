// Package network provides network-related health monitoring capabilities.
package network

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// CorrelationConfig configures cross-domain and cross-nameserver correlation analysis
// for the DNS monitor. When enabled, the monitor runs correlation detectors each cycle
// and emits status conditions for identified failure patterns.
type CorrelationConfig struct {
	// Enabled controls whether correlation analysis runs each check cycle.
	Enabled bool `json:"enabled"`

	// MinConfidence is the minimum confidence threshold (0.0–1.0) a correlation must
	// reach before it is reported as a status condition. Defaults to 0.7.
	MinConfidence float64 `json:"minConfidence"`

	// WindowMinutes defines the look-back window (in minutes) used by the temporal
	// correlation detector. Defaults to 5.
	WindowMinutes int `json:"windowMinutes"`

	// NameserverFailureThreshold is the success-rate (0.0–1.0) below which a nameserver
	// is considered "failing" for nameserver-level correlation purposes. Defaults to 0.5.
	NameserverFailureThreshold float64 `json:"nameserverFailureThreshold"`

	// MinNameserversForDomainCorrelation is the minimum number of nameservers that must
	// be failing for a domain before a domain-pattern correlation is reported. Defaults to 2.
	MinNameserversForDomainCorrelation int `json:"minNameserversForDomainCorrelation"`
}

// CorrelationResult represents a detected cross-domain or cross-nameserver failure pattern.
type CorrelationResult struct {
	// Type classifies the correlation: "nameserver", "domain_pattern", or "temporal".
	Type string

	// Confidence is a 0.0–1.0 score indicating certainty of the correlation hypothesis.
	Confidence float64

	// AffectedItems lists the nameserver addresses or domain names involved.
	AffectedItems []string

	// RootCause is a human-readable root-cause hypothesis.
	RootCause string

	// Evidence lists supporting data points (e.g., failure rates, affected domains).
	Evidence []string
}

// CorrelationEngine analyzes DNS nameserver and domain failure data to detect correlated
// failure patterns and produce actionable root-cause hypotheses.
//
// The engine is stateless: it accepts point-in-time snapshots of the monitor's internal
// tracking maps on each call to Analyze, making it both thread-safe (when called under
// the monitor's lock) and straightforward to unit-test.
type CorrelationEngine struct {
	cfg *CorrelationConfig
}

// newCorrelationEngine creates a CorrelationEngine with the supplied config.
func newCorrelationEngine(cfg *CorrelationConfig) *CorrelationEngine {
	return &CorrelationEngine{cfg: cfg}
}

// Analyze runs all correlation detectors against the supplied snapshot data and returns
// results that meet the configured minimum confidence threshold.
//
// nameserverStats is the per-nameserver sliding-window stats map; may be nil when health
// scoring is disabled (only domain-pattern detection will run).
// nameserverDomainStatus tracks per-(nameserver, domain) failure counts.
func (e *CorrelationEngine) Analyze(
	nameserverStats map[string]*NameserverStats,
	nameserverDomainStatus map[string]*NameserverDomainStatus,
) []CorrelationResult {
	var raw []CorrelationResult
	raw = append(raw, e.detectNameserverCorrelation(nameserverStats)...)
	raw = append(raw, e.detectDomainPatternCorrelation(nameserverDomainStatus)...)
	raw = append(raw, e.detectTemporalCorrelation(nameserverStats)...)

	// Filter to results that meet the confidence threshold.
	out := raw[:0]
	for _, r := range raw {
		if r.Confidence >= e.cfg.MinConfidence {
			out = append(out, r)
		}
	}
	return out
}

// detectNameserverCorrelation identifies nameservers whose recent success rate falls below
// NameserverFailureThreshold. A low rate on a single nameserver suggests that nameserver
// is the root cause, not the domain or the network in general.
func (e *CorrelationEngine) detectNameserverCorrelation(stats map[string]*NameserverStats) []CorrelationResult {
	if len(stats) == 0 {
		return nil
	}

	threshold := e.cfg.NameserverFailureThreshold
	var results []CorrelationResult

	for ns, s := range stats {
		snap := s.snapshot()
		if len(snap) < 3 {
			continue // Insufficient data for a meaningful signal.
		}

		successes := 0
		for _, r := range snap {
			if r.Success {
				successes++
			}
		}
		rate := float64(successes) / float64(len(snap))
		if rate >= threshold {
			continue
		}

		// Confidence scales linearly from 0 (at threshold) to 1 (at 0% success),
		// then is attenuated by sample completeness (full weight at 10+ samples).
		conf := (threshold - rate) / threshold
		conf *= math.Min(1.0, float64(len(snap))/10.0)
		conf = roundConf(conf)

		results = append(results, CorrelationResult{
			Type:          "nameserver",
			Confidence:    conf,
			AffectedItems: []string{ns},
			RootCause:     fmt.Sprintf("Nameserver %s is failing (%.0f%% success rate)", ns, rate*100),
			Evidence: []string{
				fmt.Sprintf("%.0f%% success rate (%d/%d checks)", rate*100, successes, len(snap)),
				fmt.Sprintf("Failure threshold: %.0f%%", threshold*100),
			},
		})
	}
	return results
}

// detectDomainPatternCorrelation identifies domains (and TLD-level patterns) that are
// failing across multiple nameservers, suggesting the problem lies with the domain
// configuration or upstream authoritative server rather than with any specific nameserver.
func (e *CorrelationEngine) detectDomainPatternCorrelation(statusMap map[string]*NameserverDomainStatus) []CorrelationResult {
	if len(statusMap) == 0 {
		return nil
	}

	type domainData struct {
		total     int
		failing   int
		failingNS []string
	}
	byDomain := make(map[string]*domainData)
	// 10-minute "recent success" window: distinct from CorrelationConfig.WindowMinutes (which
	// controls temporal correlation look-back). This window defines how stale a LastSuccess
	// timestamp must be before a nameserver is counted as "failing" for a given domain.
	cutoff := time.Now().Add(-10 * time.Minute)

	for _, s := range statusMap {
		if _, ok := byDomain[s.Domain]; !ok {
			byDomain[s.Domain] = &domainData{}
		}
		dd := byDomain[s.Domain]
		dd.total++
		// A nameserver is "failing" for a domain when failure count > 0 and
		// no recent success has been recorded.
		recentSuccess := !s.LastSuccess.IsZero() && s.LastSuccess.After(cutoff)
		if s.FailureCount > 0 && !recentSuccess {
			dd.failing++
			dd.failingNS = append(dd.failingNS, s.Nameserver)
		}
	}

	minNS := e.cfg.MinNameserversForDomainCorrelation
	var results []CorrelationResult

	// Per-domain correlations.
	for domain, dd := range byDomain {
		if dd.total < minNS || dd.failing < minNS {
			continue
		}
		conf := roundConf(float64(dd.failing) / float64(dd.total))
		results = append(results, CorrelationResult{
			Type:          "domain_pattern",
			Confidence:    conf,
			AffectedItems: []string{domain},
			RootCause:     fmt.Sprintf("Domain %s is failing on %d of %d nameservers", domain, dd.failing, dd.total),
			Evidence:      []string{fmt.Sprintf("Failing nameservers: %s", strings.Join(dd.failingNS, ", "))},
		})
	}

	// TLD-level pattern: group domains by TLD suffix, detect shared degradation.
	type tldData struct {
		domains []string
		failing int
	}
	byTLD := make(map[string]*tldData)
	for domain, dd := range byDomain {
		tld := extractTLD(domain)
		if _, ok := byTLD[tld]; !ok {
			byTLD[tld] = &tldData{}
		}
		td := byTLD[tld]
		td.domains = append(td.domains, domain)
		if dd.failing > 0 {
			td.failing++
		}
	}
	for tld, td := range byTLD {
		if len(td.domains) < 2 || td.failing < 2 {
			continue
		}
		conf := roundConf(float64(td.failing) / float64(len(td.domains)))
		if conf < e.cfg.MinConfidence {
			continue
		}
		results = append(results, CorrelationResult{
			Type:          "domain_pattern",
			Confidence:    conf,
			AffectedItems: td.domains,
			RootCause:     fmt.Sprintf("Multiple %s domains are failing (%d of %d)", tld, td.failing, len(td.domains)),
			Evidence:      []string{fmt.Sprintf("Affected domains: %s", strings.Join(td.domains, ", "))},
		})
	}

	return results
}

// detectTemporalCorrelation identifies when two or more nameservers begin failing
// within the same time window, suggesting a coordinated infrastructure event rather
// than independent nameserver issues.
func (e *CorrelationEngine) detectTemporalCorrelation(stats map[string]*NameserverStats) []CorrelationResult {
	if len(stats) < 2 {
		return nil // Temporal correlation requires at least two nameservers.
	}

	window := time.Duration(e.cfg.WindowMinutes) * time.Minute
	cutoff := time.Now().Add(-window)

	type recentResult struct {
		nameserver  string
		recentFails int
		recentTotal int
	}
	var failing []recentResult

	for ns, s := range stats {
		snap := s.snapshot()
		recentFails, recentTotal := 0, 0
		for _, r := range snap {
			if r.Timestamp.After(cutoff) {
				recentTotal++
				if !r.Success {
					recentFails++
				}
			}
		}
		if recentTotal < 2 || recentFails == 0 {
			continue
		}
		// Only count nameservers with a meaningful recent failure rate (≥30%).
		if float64(recentFails)/float64(recentTotal) >= 0.3 {
			failing = append(failing, recentResult{ns, recentFails, recentTotal})
		}
	}

	if len(failing) < 2 {
		return nil
	}

	conf := roundConf(float64(len(failing)) / float64(len(stats)))
	var affected []string
	var evidence []string
	for _, f := range failing {
		affected = append(affected, f.nameserver)
		evidence = append(evidence, fmt.Sprintf("%s: %d/%d recent checks failed",
			f.nameserver, f.recentFails, f.recentTotal))
	}

	return []CorrelationResult{{
		Type:       "temporal",
		Confidence: conf,
		AffectedItems: affected,
		RootCause: fmt.Sprintf("%d nameservers showing correlated failures in the last %d minutes",
			len(failing), e.cfg.WindowMinutes),
		Evidence: evidence,
	}}
}

// extractTLD returns a recognisable suffix from a domain name.
// Kubernetes internal domains (*.cluster.local) are returned as their full suffix.
// Standard domains return just the TLD (e.g., ".com").
func extractTLD(domain string) string {
	for _, suffix := range []string{".svc.cluster.local", ".cluster.local"} {
		if strings.HasSuffix(domain, suffix) {
			return suffix
		}
	}
	parts := strings.Split(domain, ".")
	if len(parts) >= 2 {
		return "." + parts[len(parts)-1]
	}
	return domain
}

// roundConf rounds a confidence value to two decimal places.
func roundConf(v float64) float64 {
	return math.Round(v*100) / 100
}
