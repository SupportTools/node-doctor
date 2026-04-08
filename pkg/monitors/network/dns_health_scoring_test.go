package network

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// TestNameserverStatsAdd verifies the ring-buffer semantics of NameserverStats.
func TestNameserverStatsAdd(t *testing.T) {
	ns := newNameserverStats(3)

	r1 := &NameserverCheckResult{Success: true, Latency: 10 * time.Millisecond}
	r2 := &NameserverCheckResult{Success: false, Latency: 0, ErrType: DNSErrorTimeout}
	r3 := &NameserverCheckResult{Success: true, Latency: 20 * time.Millisecond}
	r4 := &NameserverCheckResult{Success: true, Latency: 15 * time.Millisecond}

	ns.add(r1)
	if ns.count != 1 {
		t.Fatalf("expected count 1, got %d", ns.count)
	}

	ns.add(r2)
	ns.add(r3)
	if ns.count != 3 {
		t.Fatalf("expected count 3, got %d", ns.count)
	}

	// Adding a 4th overwrites oldest; count stays at capacity
	ns.add(r4)
	if ns.count != 3 {
		t.Fatalf("expected count 3 after overflow, got %d", ns.count)
	}

	snap := ns.snapshot()
	if len(snap) != 3 {
		t.Fatalf("snapshot length: got %d, want 3", len(snap))
	}
}

// TestComputeHealthScore_AllSuccess verifies a perfect score when all checks pass with low latency.
func TestComputeHealthScore_AllSuccess(t *testing.T) {
	cfg := defaultHealthScoringConfig()
	ns := newNameserverStats(10)

	for range 10 {
		ns.add(&NameserverCheckResult{
			Timestamp: time.Now(),
			Success:   true,
			Latency:   20 * time.Millisecond,
		})
	}

	score := computeHealthScore(ns, cfg, "8.8.8.8")

	if score.Status != "healthy" {
		t.Errorf("expected healthy, got %s (score=%.1f)", score.Status, score.Score)
	}
	if score.Score < 95 {
		t.Errorf("expected score >= 95 for all-success low-latency, got %.1f", score.Score)
	}
	if score.SuccessScore != 100 {
		t.Errorf("SuccessScore: got %.1f, want 100", score.SuccessScore)
	}
	if score.SampleCount != 10 {
		t.Errorf("SampleCount: got %d, want 10", score.SampleCount)
	}
}

// TestComputeHealthScore_AllFailure verifies a very low score when all checks fail.
func TestComputeHealthScore_AllFailure(t *testing.T) {
	cfg := defaultHealthScoringConfig()
	ns := newNameserverStats(10)

	for range 10 {
		ns.add(&NameserverCheckResult{
			Timestamp: time.Now(),
			Success:   false,
			ErrType:   DNSErrorTimeout,
		})
	}

	score := computeHealthScore(ns, cfg, "9.9.9.9")

	if score.Status != "unhealthy" {
		t.Errorf("expected unhealthy, got %s (score=%.1f)", score.Status, score.Score)
	}
	if score.Score >= cfg.UnhealthyThreshold {
		t.Errorf("expected score < %.0f, got %.1f", cfg.UnhealthyThreshold, score.Score)
	}
	if score.SuccessScore != 0 {
		t.Errorf("SuccessScore: got %.1f, want 0 for all-failure", score.SuccessScore)
	}
}

// TestComputeHealthScore_InsufficientData checks the guard for too-few samples.
func TestComputeHealthScore_InsufficientData(t *testing.T) {
	cfg := defaultHealthScoringConfig()
	ns := newNameserverStats(20)

	// Add only 2 results (below minSamples=3)
	ns.add(&NameserverCheckResult{Success: true, Latency: 10 * time.Millisecond})
	ns.add(&NameserverCheckResult{Success: true, Latency: 12 * time.Millisecond})

	score := computeHealthScore(ns, cfg, "1.1.1.1")
	if score.Status != "insufficient_data" {
		t.Errorf("expected insufficient_data with 2 samples, got %s", score.Status)
	}
}

// TestComputeHealthScore_DegradedLatency verifies that high p95 latency degrades the score.
func TestComputeHealthScore_DegradedLatency(t *testing.T) {
	cfg := defaultHealthScoringConfig()
	ns := newNameserverStats(20)

	// All succeed but with latency at 80% of LatencyMax (1.6s)
	for range 10 {
		ns.add(&NameserverCheckResult{
			Timestamp: time.Now(),
			Success:   true,
			Latency:   1600 * time.Millisecond,
		})
	}

	score := computeHealthScore(ns, cfg, "4.4.4.4")

	if score.LatencyScore >= 50 {
		t.Errorf("expected low latency score for 1.6s latency, got %.1f", score.LatencyScore)
	}
	// Success score is still 100 (all succeeded)
	if score.SuccessScore != 100 {
		t.Errorf("SuccessScore: got %.1f, want 100 (all checks passed)", score.SuccessScore)
	}
}

// TestComputeHealthScore_ErrorDiversity verifies that error type diversity reduces the score.
func TestComputeHealthScore_ErrorDiversity(t *testing.T) {
	cfg := defaultHealthScoringConfig()

	// Score with all-timeout errors should be worse than all-temporary errors.
	nsTimeout := newNameserverStats(10)
	nsTemp := newNameserverStats(10)

	for range 5 {
		nsTimeout.add(&NameserverCheckResult{Success: true, Latency: 10 * time.Millisecond})
		nsTimeout.add(&NameserverCheckResult{Success: false, ErrType: DNSErrorTimeout})

		nsTemp.add(&NameserverCheckResult{Success: true, Latency: 10 * time.Millisecond})
		nsTemp.add(&NameserverCheckResult{Success: false, ErrType: DNSErrorTemporary})
	}

	scoreTimeout := computeHealthScore(nsTimeout, cfg, "ns1")
	scoreTemp := computeHealthScore(nsTemp, cfg, "ns2")

	if scoreTimeout.ErrorScore >= scoreTemp.ErrorScore {
		t.Errorf("timeout errors should produce lower error score than temporary errors: timeout=%.1f, temp=%.1f",
			scoreTimeout.ErrorScore, scoreTemp.ErrorScore)
	}
}

// TestComputeHealthScore_StatusThresholds verifies threshold transitions.
func TestComputeHealthScore_StatusThresholds(t *testing.T) {
	cfg := defaultHealthScoringConfig()
	// cfg.UnhealthyThreshold = 40, cfg.DegradedThreshold = 70

	// Scores are composite: success-rate weight is only 40%, so other components lift the total.
	// 60% success + perfect latency/consistency ≈ 78 (healthy).
	// 20% success + perfect latency for successes ≈ 56 (degraded).
	// 0% success → latency/consistency default to 0, total ≈ 0 (unhealthy).
	tests := []struct {
		name           string
		successRate    float64 // 0.0-1.0
		expectedStatus string
	}{
		{"perfect", 1.0, "healthy"},
		{"good", 0.9, "healthy"},
		{"60pct-success-still-healthy", 0.6, "healthy"}, // composite score ~78 — latency/consistency lift it above the degraded threshold
		{"mostly-failing", 0.2, "degraded"},
		{"all-failing", 0.0, "unhealthy"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ns := newNameserverStats(10)
			successes := int(tc.successRate * 10)
			for i := range 10 {
				if i < successes {
					ns.add(&NameserverCheckResult{Success: true, Latency: 30 * time.Millisecond})
				} else {
					ns.add(&NameserverCheckResult{Success: false, ErrType: DNSErrorTimeout, Latency: 0})
				}
			}
			score := computeHealthScore(ns, cfg, "ns")
			if score.Status != tc.expectedStatus {
				t.Errorf("successRate=%.1f: expected %s, got %s (score=%.1f)",
					tc.successRate, tc.expectedStatus, score.Status, score.Score)
			}
		})
	}
}

// TestSortFloat64s verifies our insertion-sort implementation.
func TestSortFloat64s(t *testing.T) {
	input := []float64{3.0, 1.0, 4.0, 1.5, 9.0, 2.6}
	sortFloat64s(input)
	for i := 1; i < len(input); i++ {
		if input[i] < input[i-1] {
			t.Errorf("not sorted at index %d: %v", i, input)
		}
	}
}

// TestPercentile95 verifies p95 calculation.
func TestPercentile95(t *testing.T) {
	data := []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}
	p95 := percentile95(data)
	// For 10 elements, p95 index = 0.95*9 = 8.55 → linear interp between 90 and 100
	expected := 90.0*(1-0.55) + 100.0*0.55
	if abs64(p95-expected) > 0.01 {
		t.Errorf("p95: got %.4f, want %.4f", p95, expected)
	}
}

func abs64(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// TestDNSMonitorHealthScoring_Integration tests the full path: config → check → scores in status.
func TestDNSMonitorHealthScoring_Integration(t *testing.T) {
	// Build a mock resolver that always succeeds quickly
	resolver := newMockResolver()
	for _, ns := range []string{"1.1.1.1", "8.8.8.8"} {
		resolver.setResponse(fmt.Sprintf("google.com@%s", ns), []string{"1.2.3.4"})
	}
	// Use the default resolver since checkNameservers uses createNameserverResolver internally
	resolver.setResponse("google.com", []string{"1.2.3.4"})

	// Write a minimal resolv.conf with two nameservers
	resolverFile := t.TempDir() + "/resolv.conf"
	if err := writeFile(resolverFile, "nameserver 127.0.0.1\nnameserver 127.0.0.2\n"); err != nil {
		t.Fatal(err)
	}

	monitor := &DNSMonitor{
		name: "test-dns-health-scoring",
		config: &DNSMonitorConfig{
			ClusterDomains:         []string{},
			ExternalDomains:        []string{},
			LatencyThreshold:       1 * time.Second,
			FailureCountThreshold:  3,
			ResolverPath:           resolverFile,
			NameserverCheckEnabled: false,
			SuccessRateTracking: &SuccessRateConfig{
				Enabled:    false,
				WindowSize: 10,
			},
			ConsistencyChecking: &ConsistencyCheckConfig{
				Enabled: false,
			},
			HealthScoring: &NameserverHealthScoringConfig{
				Enabled:              true,
				DegradedThreshold:    70,
				UnhealthyThreshold:   40,
				SuccessRateWeight:    0.40,
				LatencyWeight:        0.25,
				ErrorDiversityWeight: 0.15,
				ConsistencyWeight:    0.20,
				WindowSize:           20,
				LatencyBaseline:      50 * time.Millisecond,
				LatencyMax:           2 * time.Second,
			},
		},
		resolver:               resolver,
		nameserverDomainStatus: make(map[string]*NameserverDomainStatus),
		nameserverStats:        make(map[string]*NameserverStats),
		clusterSuccessTracker:  NewRingBuffer(10),
		externalSuccessTracker: NewRingBuffer(10),
	}

	ctx := context.Background()
	status := types.NewStatus(monitor.name)

	// Run enough cycles to exceed minSamples=3
	for range 5 {
		monitor.checkNameservers(ctx, status)
	}

	// Compute scores (normally called by checkDNS under mu; call directly for test)
	monitor.mu.Lock()
	scores := monitor.computeNameserverHealthScores(status)
	monitor.mu.Unlock()

	// We should have at least one score (127.0.0.1 and/or 127.0.0.2)
	if len(scores) == 0 {
		t.Fatal("expected at least one nameserver health score")
	}
	for _, s := range scores {
		if s.SampleCount == 0 {
			t.Errorf("nameserver %s has zero sample count", s.Nameserver)
		}
		t.Logf("nameserver=%s score=%.1f status=%s samples=%d", s.Nameserver, s.Score, s.Status, s.SampleCount)
	}
}

// TestHealthScoringConfigDefaults verifies applyDefaults populates HealthScoring.
func TestHealthScoringConfigDefaults(t *testing.T) {
	cfg := &DNSMonitorConfig{}
	if err := cfg.applyDefaults(); err != nil {
		t.Fatalf("applyDefaults: %v", err)
	}

	hs := cfg.HealthScoring
	if hs == nil {
		t.Fatal("HealthScoring should not be nil after applyDefaults")
	}
	if hs.Enabled {
		t.Error("HealthScoring should be disabled by default")
	}
	if hs.DegradedThreshold != 70 {
		t.Errorf("DegradedThreshold: got %.0f, want 70", hs.DegradedThreshold)
	}
	if hs.UnhealthyThreshold != 40 {
		t.Errorf("UnhealthyThreshold: got %.0f, want 40", hs.UnhealthyThreshold)
	}
	total := hs.SuccessRateWeight + hs.LatencyWeight + hs.ErrorDiversityWeight + hs.ConsistencyWeight
	if abs64(total-1.0) > 0.001 {
		t.Errorf("weights sum: got %.4f, want 1.0", total)
	}
	if hs.WindowSize != 20 {
		t.Errorf("WindowSize: got %d, want 20", hs.WindowSize)
	}
	if hs.LatencyBaseline != 50*time.Millisecond {
		t.Errorf("LatencyBaseline: got %v, want 50ms", hs.LatencyBaseline)
	}
}

// TestParseDNSConfigHealthScoring verifies healthScoring is parsed from config map.
func TestParseDNSConfigHealthScoring(t *testing.T) {
	configMap := map[string]interface{}{
		"clusterDomains": []interface{}{"kubernetes.default.svc.cluster.local"},
		"healthScoring": map[string]interface{}{
			"enabled":              true,
			"degradedThreshold":    float64(75),
			"unhealthyThreshold":   float64(50),
			"successRateWeight":    float64(0.50),
			"latencyWeight":        float64(0.20),
			"errorDiversityWeight": float64(0.15),
			"consistencyWeight":    float64(0.15),
			"windowSize":           float64(30),
			"latencyBaseline":      "100ms",
			"latencyMax":           "3s",
		},
	}

	cfg, err := parseDNSConfig(configMap)
	if err != nil {
		t.Fatalf("parseDNSConfig: %v", err)
	}

	hs := cfg.HealthScoring
	if hs == nil {
		t.Fatal("HealthScoring should not be nil")
	}
	if !hs.Enabled {
		t.Error("expected Enabled=true")
	}
	if hs.DegradedThreshold != 75 {
		t.Errorf("DegradedThreshold: got %.0f, want 75", hs.DegradedThreshold)
	}
	if hs.UnhealthyThreshold != 50 {
		t.Errorf("UnhealthyThreshold: got %.0f, want 50", hs.UnhealthyThreshold)
	}
	if hs.WindowSize != 30 {
		t.Errorf("WindowSize: got %d, want 30", hs.WindowSize)
	}
	if hs.LatencyBaseline != 100*time.Millisecond {
		t.Errorf("LatencyBaseline: got %v, want 100ms", hs.LatencyBaseline)
	}
	if hs.LatencyMax != 3*time.Second {
		t.Errorf("LatencyMax: got %v, want 3s", hs.LatencyMax)
	}
}

// TestLatencyMetricsCarriesHealthScores verifies the types.LatencyMetrics can hold scores.
func TestLatencyMetricsCarriesHealthScores(t *testing.T) {
	lm := &types.LatencyMetrics{
		NameserverHealthScores: []types.NameserverHealthScore{
			{
				Nameserver:       "8.8.8.8",
				Score:            92.5,
				SuccessScore:     100,
				LatencyScore:     90,
				ErrorScore:       100,
				ConsistencyScore: 80,
				Status:           "healthy",
				SampleCount:      20,
			},
		},
	}

	status := types.NewStatus("test")
	status.SetLatencyMetrics(lm)

	got := status.GetLatencyMetrics()
	if got == nil {
		t.Fatal("GetLatencyMetrics returned nil")
	}
	if len(got.NameserverHealthScores) != 1 {
		t.Fatalf("expected 1 score, got %d", len(got.NameserverHealthScores))
	}
	if got.NameserverHealthScores[0].Score != 92.5 {
		t.Errorf("Score: got %.1f, want 92.5", got.NameserverHealthScores[0].Score)
	}
}

// defaultHealthScoringConfig returns the standard defaults used in tests.
func defaultHealthScoringConfig() *NameserverHealthScoringConfig {
	return &NameserverHealthScoringConfig{
		Enabled:              true,
		DegradedThreshold:    70,
		UnhealthyThreshold:   40,
		SuccessRateWeight:    0.40,
		LatencyWeight:        0.25,
		ErrorDiversityWeight: 0.15,
		ConsistencyWeight:    0.20,
		WindowSize:           20,
		LatencyBaseline:      50 * time.Millisecond,
		LatencyMax:           2 * time.Second,
	}
}

// writeFile is a test helper to create a file with content.
func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}
