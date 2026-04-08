package network

import (
	"math"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// defaultTrendConfig returns a TrendDetectionConfig with sensible defaults for tests.
func defaultTrendConfig() *TrendDetectionConfig {
	return &TrendDetectionConfig{
		Enabled:              true,
		WindowSize:           10,
		DegradationThreshold: -0.05,
		AnomalyZScore:        2.5,
		FlapDetection: &FlapDetectionConfig{
			Enabled:         true,
			MinOscillations: 3,
			WindowMinutes:   10,
		},
	}
}

// TestNewTrendDetector_Defaults verifies that zero-count Analyze returns a safe zero result.
func TestNewTrendDetector_Defaults(t *testing.T) {
	td := NewTrendDetector(defaultTrendConfig())
	res := td.Analyze()

	if res.Count != 0 {
		t.Errorf("expected Count=0 before any observations, got %d", res.Count)
	}
	if res.Degrading || res.Anomalous || res.Flapping {
		t.Error("expected no conditions from empty TrendDetector")
	}
}

// TestTrendDetector_WelfordMean verifies that the long-term mean converges correctly.
func TestTrendDetector_WelfordMean(t *testing.T) {
	td := NewTrendDetector(defaultTrendConfig())
	now := time.Now()

	// Observe a known distribution: 1.0, 0.8, 0.6, 0.4, 1.0, 0.8, 0.6, 0.4
	// mean = (1.0+0.8+0.6+0.4+1.0+0.8+0.6+0.4) / 8 = 5.6 / 8 = 0.7
	rates := []float64{1.0, 0.8, 0.6, 0.4, 1.0, 0.8, 0.6, 0.4}
	for i, r := range rates {
		td.Observe(r, now.Add(time.Duration(i)*time.Minute))
	}

	res := td.Analyze()
	if math.Abs(res.Mean-0.7) > 0.001 {
		t.Errorf("expected mean=0.7, got %f", res.Mean)
	}
}

// TestTrendDetector_WelfordStdDev verifies that the standard deviation is computed correctly.
func TestTrendDetector_WelfordStdDev(t *testing.T) {
	td := NewTrendDetector(defaultTrendConfig())
	now := time.Now()

	// Observe: all 1.0 — stddev should be 0.
	for i := 0; i < 5; i++ {
		td.Observe(1.0, now.Add(time.Duration(i)*time.Minute))
	}
	res := td.Analyze()
	if res.StdDev > 0.001 {
		t.Errorf("expected StdDev≈0 for constant input, got %f", res.StdDev)
	}
}

// TestTrendDetector_ZScoreAnomaly verifies that an unusually low rate triggers Anomalous.
func TestTrendDetector_ZScoreAnomaly(t *testing.T) {
	td := NewTrendDetector(defaultTrendConfig())
	now := time.Now()

	// Build stable baseline: 20 observations at ~1.0
	for i := 0; i < 20; i++ {
		td.Observe(1.0, now.Add(time.Duration(i)*time.Minute))
	}

	// Now observe a sharp drop — this should be far below mean
	td.Observe(0.0, now.Add(21*time.Minute))

	res := td.Analyze()
	if !res.Anomalous {
		t.Errorf("expected Anomalous=true after sharp drop (z-score=%.2f)", res.ZScore)
	}
	if res.ZScore >= 0 {
		t.Errorf("expected negative z-score for below-mean observation, got %.2f", res.ZScore)
	}
}

// TestTrendDetector_ZScoreNoAnomaly verifies that normal variation doesn't trigger Anomalous.
func TestTrendDetector_ZScoreNoAnomaly(t *testing.T) {
	td := NewTrendDetector(defaultTrendConfig())
	now := time.Now()

	// Observe alternating 0.9/1.0 — moderate variation, z-score should stay within 2.5
	for i := 0; i < 20; i++ {
		rate := 0.9
		if i%2 == 0 {
			rate = 1.0
		}
		td.Observe(rate, now.Add(time.Duration(i)*time.Minute))
	}

	res := td.Analyze()
	if res.Anomalous {
		t.Errorf("expected Anomalous=false for normal variation (z-score=%.2f)", res.ZScore)
	}
}

// TestTrendDetector_SlopeDegrading verifies that a declining trend sets Degrading=true.
func TestTrendDetector_SlopeDegrading(t *testing.T) {
	td := NewTrendDetector(&TrendDetectionConfig{
		Enabled:              true,
		WindowSize:           10,
		DegradationThreshold: -0.05,
		AnomalyZScore:        99, // disable anomaly to isolate slope test
	})
	now := time.Now()

	// Observe monotonically declining success rate: 1.0, 0.9, 0.8, ..., 0.1
	// Slope ≈ -0.1 per sample → well below -0.05 threshold
	for i := 0; i < 10; i++ {
		rate := 1.0 - float64(i)*0.1
		td.Observe(rate, now.Add(time.Duration(i)*time.Minute))
	}

	res := td.Analyze()
	if !res.Degrading {
		t.Errorf("expected Degrading=true for declining rate (slope=%.4f)", res.Slope)
	}
	if res.Slope >= 0 {
		t.Errorf("expected negative slope for declining rate, got %.4f", res.Slope)
	}
}

// TestTrendDetector_SlopeImproving verifies that an improving trend does not set Degrading.
func TestTrendDetector_SlopeImproving(t *testing.T) {
	td := NewTrendDetector(defaultTrendConfig())
	now := time.Now()

	// Observe monotonically increasing success rate: 0.1, 0.2, ..., 1.0
	for i := 0; i < 10; i++ {
		rate := float64(i+1) * 0.1
		td.Observe(rate, now.Add(time.Duration(i)*time.Minute))
	}

	res := td.Analyze()
	if res.Degrading {
		t.Errorf("expected Degrading=false for improving rate (slope=%.4f)", res.Slope)
	}
	if res.Slope < 0 {
		t.Errorf("expected positive slope for improving rate, got %.4f", res.Slope)
	}
}

// TestTrendDetector_FlapDetection verifies that rapid oscillations set Flapping=true.
func TestTrendDetector_FlapDetection(t *testing.T) {
	cfg := defaultTrendConfig()
	cfg.FlapDetection = &FlapDetectionConfig{
		Enabled:         true,
		MinOscillations: 3,
		WindowMinutes:   60, // wide window to include all test samples
	}
	td := NewTrendDetector(cfg)
	now := time.Now()

	// Oscillate between 1.0 (healthy) and 0.0 (unhealthy) 4 times = 4 transitions
	for i := 0; i < 8; i++ {
		rate := 1.0
		if i%2 == 1 {
			rate = 0.0
		}
		td.Observe(rate, now.Add(time.Duration(i)*time.Minute))
	}

	res := td.Analyze()
	if !res.Flapping {
		t.Errorf("expected Flapping=true after 4 oscillations (got %d), min is %d",
			res.Oscillations, cfg.FlapDetection.MinOscillations)
	}
	if res.Oscillations < cfg.FlapDetection.MinOscillations {
		t.Errorf("expected Oscillations >= %d, got %d",
			cfg.FlapDetection.MinOscillations, res.Oscillations)
	}
}

// TestTrendDetector_FlapNotTriggeredBelowThreshold verifies that insufficient oscillations
// do not set Flapping.
func TestTrendDetector_FlapNotTriggeredBelowThreshold(t *testing.T) {
	cfg := defaultTrendConfig()
	cfg.FlapDetection = &FlapDetectionConfig{
		Enabled:         true,
		MinOscillations: 5, // require 5 oscillations
		WindowMinutes:   60,
	}
	td := NewTrendDetector(cfg)
	now := time.Now()

	// Only 2 oscillations (healthy→unhealthy→healthy)
	for i, rate := range []float64{1.0, 1.0, 0.0, 1.0, 1.0} {
		td.Observe(rate, now.Add(time.Duration(i)*time.Minute))
	}

	res := td.Analyze()
	if res.Flapping {
		t.Errorf("expected Flapping=false for %d oscillations (min=%d)",
			res.Oscillations, cfg.FlapDetection.MinOscillations)
	}
}

// TestTrendDetector_FlapWindowExclusion verifies that old oscillations outside the time window
// are not counted.
func TestTrendDetector_FlapWindowExclusion(t *testing.T) {
	cfg := &TrendDetectionConfig{
		Enabled:              true,
		WindowSize:           20,
		DegradationThreshold: -0.05,
		AnomalyZScore:        99,
		FlapDetection: &FlapDetectionConfig{
			Enabled:         true,
			MinOscillations: 3,
			WindowMinutes:   1, // only 1-minute window
		},
	}
	td := NewTrendDetector(cfg)

	// Old oscillations (100 minutes ago — outside the 1-minute window)
	oldBase := time.Now().Add(-100 * time.Minute)
	for i := 0; i < 8; i++ {
		rate := 1.0
		if i%2 == 1 {
			rate = 0.0
		}
		td.Observe(rate, oldBase.Add(time.Duration(i)*time.Minute))
	}

	// Recent samples: stable (no oscillations in the window)
	recentBase := time.Now().Add(-30 * time.Second)
	td.Observe(1.0, recentBase)
	td.Observe(1.0, recentBase.Add(10*time.Second))

	res := td.Analyze()
	if res.Flapping {
		t.Errorf("expected Flapping=false when oscillations are outside the time window (osc=%d)",
			res.Oscillations)
	}
}

// TestTrendDetector_CircularBuffer verifies that the window slides correctly when full.
func TestTrendDetector_CircularBuffer(t *testing.T) {
	cfg := &TrendDetectionConfig{
		Enabled:              true,
		WindowSize:           5, // small buffer
		DegradationThreshold: -0.05,
		AnomalyZScore:        99,
	}
	td := NewTrendDetector(cfg)
	now := time.Now()

	// Fill beyond capacity: first 5 are high (1.0), next 5 are low (0.0)
	// After overflow, window should contain only the last 5 (all 0.0)
	for i := 0; i < 5; i++ {
		td.Observe(1.0, now.Add(time.Duration(i)*time.Minute))
	}
	for i := 5; i < 10; i++ {
		td.Observe(0.0, now.Add(time.Duration(i)*time.Minute))
	}

	res := td.Analyze()
	// With window of 5 all-zero samples, slope should be ~0 (flat at 0.0)
	if math.Abs(res.Slope) > 0.01 {
		t.Errorf("expected near-zero slope for flat window, got %.4f", res.Slope)
	}
	// Long-term Welford mean should be 0.5 (10 samples: 5×1.0 + 5×0.0)
	if math.Abs(res.Mean-0.5) > 0.001 {
		t.Errorf("expected Welford mean=0.5 from 10 samples, got %.4f", res.Mean)
	}
}

// TestTrendSlope_LinearIncrease verifies OLS slope calculation on a perfect linear increase.
func TestTrendSlope_LinearIncrease(t *testing.T) {
	now := time.Now()
	samples := []trendSample{
		{rate: 0.2, timestamp: now},
		{rate: 0.4, timestamp: now.Add(time.Minute)},
		{rate: 0.6, timestamp: now.Add(2 * time.Minute)},
		{rate: 0.8, timestamp: now.Add(3 * time.Minute)},
		{rate: 1.0, timestamp: now.Add(4 * time.Minute)},
	}
	slope := trendSlope(samples)
	// Expected slope: 0.2 per sample
	if math.Abs(slope-0.2) > 0.0001 {
		t.Errorf("expected slope=0.2, got %.4f", slope)
	}
}

// TestTrendSlope_LinearDecrease verifies OLS slope on a perfect linear decrease.
func TestTrendSlope_LinearDecrease(t *testing.T) {
	now := time.Now()
	samples := []trendSample{
		{rate: 1.0, timestamp: now},
		{rate: 0.8, timestamp: now.Add(time.Minute)},
		{rate: 0.6, timestamp: now.Add(2 * time.Minute)},
		{rate: 0.4, timestamp: now.Add(3 * time.Minute)},
		{rate: 0.2, timestamp: now.Add(4 * time.Minute)},
	}
	slope := trendSlope(samples)
	// Expected slope: -0.2 per sample
	if math.Abs(slope-(-0.2)) > 0.0001 {
		t.Errorf("expected slope=-0.2, got %.4f", slope)
	}
}

// TestTrendSlope_Constant verifies that constant input produces zero slope.
func TestTrendSlope_Constant(t *testing.T) {
	now := time.Now()
	samples := make([]trendSample, 5)
	for i := range samples {
		samples[i] = trendSample{rate: 0.9, timestamp: now.Add(time.Duration(i) * time.Minute)}
	}
	slope := trendSlope(samples)
	if math.Abs(slope) > 0.0001 {
		t.Errorf("expected slope=0 for constant input, got %.4f", slope)
	}
}

// TestTrendSlope_SingleSample verifies that a single-element slice returns zero slope.
func TestTrendSlope_SingleSample(t *testing.T) {
	now := time.Now()
	samples := []trendSample{{rate: 0.9, timestamp: now}}
	slope := trendSlope(samples)
	if slope != 0 {
		t.Errorf("expected slope=0 for single sample, got %.4f", slope)
	}
}

// TestAddTrendConditions_NoneWhenClean verifies that no conditions are added when trend is healthy.
func TestAddTrendConditions_NoneWhenClean(t *testing.T) {
	status := types.NewStatus("test")
	res := TrendResult{
		Count:     10,
		Mean:      0.95,
		StdDev:    0.02,
		ZScore:    0.5,
		Slope:     0.01,
		Degrading: false,
		Anomalous: false,
		Flapping:  false,
	}
	AddTrendConditions(res, "Cluster", status)

	conditions := status.Conditions
	for _, c := range conditions {
		if c.Type == "DNSDegrading" || c.Type == "DNSAnomalous" || c.Type == "DNSFlapping" {
			t.Errorf("unexpected condition %s added for healthy result", c.Type)
		}
	}
}

// TestAddTrendConditions_AllThree verifies that all three conditions are added when all flags are set.
func TestAddTrendConditions_AllThree(t *testing.T) {
	status := types.NewStatus("test")
	res := TrendResult{
		Count:        10,
		Mean:         0.7,
		StdDev:       0.1,
		ZScore:       -3.0,
		Slope:        -0.1,
		Degrading:    true,
		Anomalous:    true,
		Flapping:     true,
		Oscillations: 4,
	}
	AddTrendConditions(res, "Cluster", status)

	conditionTypes := make(map[string]bool)
	for _, c := range status.Conditions {
		conditionTypes[c.Type] = true
	}

	for _, expected := range []string{"DNSDegrading", "DNSAnomalous", "DNSFlapping"} {
		if !conditionTypes[expected] {
			t.Errorf("expected condition %s to be added", expected)
		}
	}
}

// TestAddTrendConditions_EmptyCount verifies that zero-count results produce no conditions.
func TestAddTrendConditions_EmptyCount(t *testing.T) {
	status := types.NewStatus("test")
	AddTrendConditions(TrendResult{Count: 0, Degrading: true, Anomalous: true, Flapping: true}, "Cluster", status)

	for _, c := range status.Conditions {
		if c.Type == "DNSDegrading" || c.Type == "DNSAnomalous" || c.Type == "DNSFlapping" {
			t.Errorf("expected no conditions for empty result, got %s", c.Type)
		}
	}
}

// TestTrendDetector_ConcurrentObserveAnalyze verifies thread-safety with concurrent access.
func TestTrendDetector_ConcurrentObserveAnalyze(t *testing.T) {
	td := NewTrendDetector(defaultTrendConfig())
	now := time.Now()

	done := make(chan struct{})
	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			td.Observe(0.9, now.Add(time.Duration(i)*time.Second))
		}
		close(done)
	}()

	// Concurrent readers
	for i := 0; i < 5; i++ {
		go func() {
			for {
				select {
				case <-done:
					return
				default:
					td.Analyze()
				}
			}
		}()
	}

	<-done
	// Final analysis must not panic or deadlock
	res := td.Analyze()
	if res.Count < 0 {
		t.Error("expected non-negative Count")
	}
}
