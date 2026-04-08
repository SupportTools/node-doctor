package network

import (
	"testing"
	"time"
)

// makeNSStats is a test helper that builds a NameserverStats with the given success pattern.
// true = success, false = failure. Results are timestamped at equal intervals within
// the supplied window (most recent is time.Now()).
func makeNSStats(pattern []bool, window time.Duration) *NameserverStats {
	ns := newNameserverStats(len(pattern))
	if len(pattern) == 0 {
		return ns
	}
	interval := window / time.Duration(len(pattern))
	now := time.Now()
	for i, ok := range pattern {
		ts := now.Add(-window + time.Duration(i+1)*interval)
		ns.add(&NameserverCheckResult{
			Timestamp: ts,
			Success:   ok,
			Latency:   5 * time.Millisecond,
			ErrType:   map[bool]DNSErrorType{true: DNSErrorNone, false: DNSErrorTimeout}[ok],
		})
	}
	return ns
}

func defaultCorrelationConfig() *CorrelationConfig {
	return &CorrelationConfig{
		Enabled:                            true,
		MinConfidence:                      0.7,
		WindowMinutes:                      5,
		NameserverFailureThreshold:         0.5,
		MinNameserversForDomainCorrelation: 2,
	}
}

// --- detectNameserverCorrelation ---

func TestDetectNameserverCorrelation_FailingNameserver(t *testing.T) {
	engine := newCorrelationEngine(defaultCorrelationConfig())
	stats := map[string]*NameserverStats{
		"10.0.0.1": makeNSStats([]bool{false, false, false, false, false, false, false, false, false, false}, 10*time.Minute),
		"10.0.0.2": makeNSStats([]bool{true, true, true, true, true, true, true, true, true, true}, 10*time.Minute),
	}

	results := engine.detectNameserverCorrelation(stats)

	if len(results) != 1 {
		t.Fatalf("expected 1 correlation result, got %d", len(results))
	}
	r := results[0]
	if r.Type != "nameserver" {
		t.Errorf("expected type 'nameserver', got %q", r.Type)
	}
	if r.AffectedItems[0] != "10.0.0.1" {
		t.Errorf("expected affected nameserver 10.0.0.1, got %v", r.AffectedItems)
	}
	if r.Confidence < 0.7 {
		t.Errorf("expected confidence >= 0.7, got %.2f", r.Confidence)
	}
}

func TestDetectNameserverCorrelation_HealthyNameserver(t *testing.T) {
	engine := newCorrelationEngine(defaultCorrelationConfig())
	stats := map[string]*NameserverStats{
		"10.0.0.1": makeNSStats([]bool{true, true, true, true, true, false, true, true, true, true}, 10*time.Minute),
	}

	results := engine.detectNameserverCorrelation(stats)

	if len(results) != 0 {
		t.Errorf("expected no results for mostly-healthy nameserver, got %d", len(results))
	}
}

func TestDetectNameserverCorrelation_InsufficientData(t *testing.T) {
	engine := newCorrelationEngine(defaultCorrelationConfig())
	stats := map[string]*NameserverStats{
		"10.0.0.1": makeNSStats([]bool{false, false}, 5*time.Minute), // only 2 samples
	}

	results := engine.detectNameserverCorrelation(stats)

	if len(results) != 0 {
		t.Errorf("expected no results for insufficient data, got %d", len(results))
	}
}

func TestDetectNameserverCorrelation_EmptyStats(t *testing.T) {
	engine := newCorrelationEngine(defaultCorrelationConfig())
	results := engine.detectNameserverCorrelation(nil)
	if results != nil {
		t.Errorf("expected nil for empty stats, got %v", results)
	}
}

// --- detectDomainPatternCorrelation ---

func makeNSDomainStatus(nameserver, domain string, failures int, lastSuccess time.Time) *NameserverDomainStatus {
	return &NameserverDomainStatus{
		Nameserver:   nameserver,
		Domain:       domain,
		FailureCount: failures,
		LastSuccess:  lastSuccess,
	}
}

func TestDetectDomainPatternCorrelation_DomainFailingAcrossNameservers(t *testing.T) {
	engine := newCorrelationEngine(defaultCorrelationConfig())

	// Both nameservers failing for "bad-domain.com" with no recent success
	statusMap := map[string]*NameserverDomainStatus{
		"10.0.0.1:bad-domain.com": makeNSDomainStatus("10.0.0.1", "bad-domain.com", 5, time.Time{}),
		"10.0.0.2:bad-domain.com": makeNSDomainStatus("10.0.0.2", "bad-domain.com", 3, time.Time{}),
		"10.0.0.1:good-domain.com": makeNSDomainStatus("10.0.0.1", "good-domain.com", 0, time.Now()),
		"10.0.0.2:good-domain.com": makeNSDomainStatus("10.0.0.2", "good-domain.com", 0, time.Now()),
	}

	results := engine.detectDomainPatternCorrelation(statusMap)

	domainResults := filterByType(results, "domain_pattern")
	if len(domainResults) == 0 {
		t.Fatal("expected at least one domain_pattern correlation")
	}
	found := false
	for _, r := range domainResults {
		for _, item := range r.AffectedItems {
			if item == "bad-domain.com" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected bad-domain.com in affected items, got %v", domainResults)
	}
}

func TestDetectDomainPatternCorrelation_SingleNameserverFailure(t *testing.T) {
	engine := newCorrelationEngine(defaultCorrelationConfig())

	// Only one nameserver failing — below minNameserversForDomainCorrelation (2)
	statusMap := map[string]*NameserverDomainStatus{
		"10.0.0.1:bad-domain.com": makeNSDomainStatus("10.0.0.1", "bad-domain.com", 5, time.Time{}),
		"10.0.0.2:bad-domain.com": makeNSDomainStatus("10.0.0.2", "bad-domain.com", 0, time.Now()),
	}

	results := engine.detectDomainPatternCorrelation(statusMap)
	domainResults := filterByType(results, "domain_pattern")
	if len(domainResults) != 0 {
		t.Errorf("expected no results when only one nameserver is failing, got %v", domainResults)
	}
}

func TestDetectDomainPatternCorrelation_TLDPattern(t *testing.T) {
	engine := newCorrelationEngine(defaultCorrelationConfig())

	// Two cluster-local domains both failing across two nameservers — TLD correlation
	statusMap := map[string]*NameserverDomainStatus{
		"10.0.0.1:svc-a.cluster.local": makeNSDomainStatus("10.0.0.1", "svc-a.cluster.local", 3, time.Time{}),
		"10.0.0.2:svc-a.cluster.local": makeNSDomainStatus("10.0.0.2", "svc-a.cluster.local", 2, time.Time{}),
		"10.0.0.1:svc-b.cluster.local": makeNSDomainStatus("10.0.0.1", "svc-b.cluster.local", 3, time.Time{}),
		"10.0.0.2:svc-b.cluster.local": makeNSDomainStatus("10.0.0.2", "svc-b.cluster.local", 2, time.Time{}),
	}

	results := engine.detectDomainPatternCorrelation(statusMap)
	tldResults := filterByType(results, "domain_pattern")

	foundTLD := false
	for _, r := range tldResults {
		if len(r.AffectedItems) > 1 {
			foundTLD = true
		}
	}
	if !foundTLD {
		t.Error("expected TLD-level correlation for .cluster.local domains but none was detected")
	}
}

func TestDetectDomainPatternCorrelation_EmptyMap(t *testing.T) {
	engine := newCorrelationEngine(defaultCorrelationConfig())
	results := engine.detectDomainPatternCorrelation(nil)
	if results != nil {
		t.Errorf("expected nil for empty map, got %v", results)
	}
}

// --- detectTemporalCorrelation ---

func TestDetectTemporalCorrelation_CoordinatedFailures(t *testing.T) {
	engine := newCorrelationEngine(defaultCorrelationConfig())

	// Both nameservers failing in the recent window
	recentPattern := []bool{false, false, false, false, false}
	stats := map[string]*NameserverStats{
		"10.0.0.1": makeNSStats(recentPattern, 2*time.Minute),
		"10.0.0.2": makeNSStats(recentPattern, 2*time.Minute),
		"10.0.0.3": makeNSStats(recentPattern, 2*time.Minute),
	}

	results := engine.detectTemporalCorrelation(stats)

	if len(results) == 0 {
		t.Fatal("expected temporal correlation result for coordinated failures")
	}
	r := results[0]
	if r.Type != "temporal" {
		t.Errorf("expected type 'temporal', got %q", r.Type)
	}
	if len(r.AffectedItems) < 2 {
		t.Errorf("expected at least 2 affected nameservers, got %v", r.AffectedItems)
	}
}

func TestDetectTemporalCorrelation_SingleNameserver(t *testing.T) {
	engine := newCorrelationEngine(defaultCorrelationConfig())
	stats := map[string]*NameserverStats{
		"10.0.0.1": makeNSStats([]bool{false, false, false}, 2*time.Minute),
	}
	results := engine.detectTemporalCorrelation(stats)
	if len(results) != 0 {
		t.Errorf("expected no temporal correlation with single nameserver, got %v", results)
	}
}

func TestDetectTemporalCorrelation_NilStats(t *testing.T) {
	engine := newCorrelationEngine(defaultCorrelationConfig())
	results := engine.detectTemporalCorrelation(nil)
	if results != nil {
		t.Errorf("expected nil for nil stats, got %v", results)
	}
}

// --- Analyze (confidence filter) ---

func TestAnalyze_FiltersLowConfidence(t *testing.T) {
	cfg := defaultCorrelationConfig()
	cfg.MinConfidence = 0.95 // Very high threshold

	engine := newCorrelationEngine(cfg)

	// 60% failure rate → confidence ≈ 0.60*sampleFactor < 0.95
	stats := map[string]*NameserverStats{
		"10.0.0.1": makeNSStats([]bool{false, false, false, true, true, true}, 10*time.Minute),
	}

	results := engine.Analyze(stats, nil)
	// Should be filtered out by MinConfidence
	for _, r := range results {
		if r.Type == "nameserver" && r.Confidence < cfg.MinConfidence {
			t.Errorf("result with confidence %.2f should have been filtered (min %.2f)", r.Confidence, cfg.MinConfidence)
		}
	}
}

// --- extractTLD ---

func TestExtractTLD(t *testing.T) {
	tests := []struct {
		domain string
		want   string
	}{
		{"kubernetes.default.svc.cluster.local", ".svc.cluster.local"},
		{"kube-dns.kube-system.svc.cluster.local", ".svc.cluster.local"},
		{"example.cluster.local", ".cluster.local"},
		{"google.com", ".com"},
		{"api.example.org", ".org"},
		{"single", "single"},
	}

	for _, tc := range tests {
		got := extractTLD(tc.domain)
		if got != tc.want {
			t.Errorf("extractTLD(%q) = %q, want %q", tc.domain, got, tc.want)
		}
	}
}

// TestAnalyze_BothDataSourcesPopulated verifies that Analyze applies the confidence
// filter uniformly across results from all three detectors when both nameserverStats
// and nameserverDomainStatus are populated (the production call path).
func TestAnalyze_BothDataSourcesPopulated(t *testing.T) {
	engine := newCorrelationEngine(defaultCorrelationConfig())

	// Two nameservers: 10.0.0.1 is failing, 10.0.0.2 is healthy.
	stats := map[string]*NameserverStats{
		"10.0.0.1": makeNSStats([]bool{false, false, false, false, false, false, false, false, false, false}, 10*time.Minute),
		"10.0.0.2": makeNSStats([]bool{true, true, true, true, true, true, true, true, true, true}, 10*time.Minute),
	}

	// Domain failing across both nameservers (domain-pattern detector input).
	statusMap := map[string]*NameserverDomainStatus{
		"10.0.0.1:broken.example.com": makeNSDomainStatus("10.0.0.1", "broken.example.com", 5, time.Time{}),
		"10.0.0.2:broken.example.com": makeNSDomainStatus("10.0.0.2", "broken.example.com", 3, time.Time{}),
	}

	results := engine.Analyze(stats, statusMap)

	// At minimum we expect a nameserver correlation for 10.0.0.1.
	if len(results) == 0 {
		t.Fatal("expected at least one correlation result with both data sources populated")
	}

	// All returned results must meet MinConfidence.
	for _, r := range results {
		if r.Confidence < engine.cfg.MinConfidence {
			t.Errorf("result type=%q confidence=%.2f is below MinConfidence=%.2f",
				r.Type, r.Confidence, engine.cfg.MinConfidence)
		}
	}

	// Verify at least a nameserver-type result is present.
	found := false
	for _, r := range results {
		if r.Type == "nameserver" {
			found = true
		}
	}
	if !found {
		t.Error("expected nameserver-type correlation result")
	}
}

// --- helpers ---

func filterByType(results []CorrelationResult, typ string) []CorrelationResult {
	var out []CorrelationResult
	for _, r := range results {
		if r.Type == typ {
			out = append(out, r)
		}
	}
	return out
}
