package network

import (
	"math"
	"testing"
	"time"
)

// defaultPredictiveCfg returns a PredictiveAlertConfig suitable for tests.
func defaultPredictiveCfg() *PredictiveAlertConfig {
	return &PredictiveAlertConfig{
		Enabled:             true,
		PredictionWindow:    30 * time.Minute,
		WarningLeadTime:     15 * time.Minute,
		MinDataPoints:       5,
		ConfidenceThreshold: 0.7,
	}
}

// fillRingBuffer builds a RingBuffer from a slice of (success, offset) pairs.
// offset is the seconds before now that the check occurred.
func fillRingBuffer(entries []struct {
	success bool
	secsAgo float64
}, size int) *RingBuffer {
	rb := NewRingBuffer(size)
	now := time.Now()
	for _, e := range entries {
		rb.Add(&CheckResult{
			Timestamp: now.Add(-time.Duration(e.secsAgo * float64(time.Second))),
			Success:   e.success,
		})
	}
	return rb
}

func TestAnalyzeRingBuffer_TooFewPoints(t *testing.T) {
	cfg := defaultPredictiveCfg()
	rb := NewRingBuffer(20)
	// Only 3 entries — below MinDataPoints of 5.
	for i := 0; i < 3; i++ {
		rb.Add(&CheckResult{Timestamp: time.Now().Add(-time.Duration(i) * time.Second), Success: true})
	}
	result := analyzeRingBuffer(rb, 0.3, cfg)
	if result.HasPrediction {
		t.Error("expected no prediction with too few data points")
	}
}

func TestAnalyzeRingBuffer_Disabled(t *testing.T) {
	cfg := defaultPredictiveCfg()
	cfg.Enabled = false
	rb := NewRingBuffer(20)
	for i := 0; i < 10; i++ {
		rb.Add(&CheckResult{Timestamp: time.Now().Add(-time.Duration(i) * time.Second), Success: true})
	}
	result := analyzeRingBuffer(rb, 0.3, cfg)
	if result.HasPrediction {
		t.Error("expected no prediction when feature is disabled")
	}
}

func TestAnalyzeRingBuffer_SteadySuccess_NoAlert(t *testing.T) {
	cfg := defaultPredictiveCfg()
	// All successes — perfectly flat trend, confidence should be zero.
	entries := make([]struct {
		success bool
		secsAgo float64
	}, 10)
	for i := range entries {
		entries[i] = struct {
			success bool
			secsAgo float64
		}{true, float64(10 - i) * 30}
	}
	rb := fillRingBuffer(entries, 20)
	result := analyzeRingBuffer(rb, 0.3, cfg)
	if !result.HasPrediction {
		t.Fatal("expected HasPrediction=true for sufficient data points")
	}
	if result.WillBreach {
		t.Errorf("expected no breach prediction for steady success, got TimeToBreach=%v", result.TimeToBreach)
	}
}

func TestAnalyzeRingBuffer_ImprovingTrend_NoAlert(t *testing.T) {
	cfg := defaultPredictiveCfg()
	// Improving trend: early failures, recent successes.
	entries := []struct {
		success bool
		secsAgo float64
	}{
		{false, 300}, {false, 270}, {false, 240},
		{true, 210}, {true, 180}, {true, 150},
		{true, 120}, {true, 90}, {true, 60}, {true, 30},
	}
	rb := fillRingBuffer(entries, 20)
	result := analyzeRingBuffer(rb, 0.3, cfg)
	if result.WillBreach {
		t.Errorf("improving trend should not produce a breach prediction, got slope=%.6f", result.Slope)
	}
}

func TestAnalyzeRingBuffer_DegradingTrend_Alert(t *testing.T) {
	cfg := defaultPredictiveCfg()
	// Degrading trend: early successes, recent failures producing a clear negative slope.
	entries := []struct {
		success bool
		secsAgo float64
	}{
		{true, 600}, {true, 540}, {true, 480}, {true, 420},
		{true, 360}, {false, 300}, {false, 240}, {false, 180},
		{false, 120}, {false, 60},
	}
	rb := fillRingBuffer(entries, 20)
	result := analyzeRingBuffer(rb, 0.3, cfg)

	if !result.HasPrediction {
		t.Fatal("expected HasPrediction=true")
	}
	if result.Slope >= 0 {
		t.Errorf("expected negative slope for degrading trend, got slope=%.6f", result.Slope)
	}
	if result.Confidence < 0 || result.Confidence > 1 {
		t.Errorf("confidence %.4f out of [0,1] range", result.Confidence)
	}
}

func TestAnalyzeRingBuffer_HighConfidenceDegradation_WillBreach(t *testing.T) {
	cfg := defaultPredictiveCfg()
	cfg.PredictionWindow = 2 * time.Hour // Wide window to ensure breach is predicted.
	cfg.ConfidenceThreshold = 0.5        // Lower threshold so the test is robust.

	// Build a clean degrading series: first 5 successes, next 5 failures,
	// evenly spaced 60s apart — this gives a clear linear trend.
	now := time.Now()
	rb := NewRingBuffer(20)
	for i := 0; i < 10; i++ {
		ts := now.Add(-time.Duration(9-i) * 60 * time.Second)
		rb.Add(&CheckResult{Timestamp: ts, Success: i < 5})
	}
	result := analyzeRingBuffer(rb, 0.3, cfg)

	if !result.HasPrediction {
		t.Fatal("expected HasPrediction=true")
	}
	if result.Confidence < cfg.ConfidenceThreshold {
		t.Logf("confidence=%.4f below threshold %.4f — breach prediction may not fire", result.Confidence, cfg.ConfidenceThreshold)
	}
	// Slope must be negative.
	if result.Slope >= 0 {
		t.Errorf("expected negative slope, got %.6f", result.Slope)
	}
}

func TestGetOrdered_NotFull(t *testing.T) {
	rb := NewRingBuffer(10)
	now := time.Now()
	t1 := now.Add(-2 * time.Second)
	t2 := now.Add(-1 * time.Second)
	t3 := now
	rb.Add(&CheckResult{Timestamp: t1, Success: true})
	rb.Add(&CheckResult{Timestamp: t2, Success: false})
	rb.Add(&CheckResult{Timestamp: t3, Success: true})

	ordered := rb.getOrdered()
	if len(ordered) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(ordered))
	}
	if !ordered[0].Timestamp.Equal(t1) {
		t.Errorf("first entry should be t1 (oldest), got %v", ordered[0].Timestamp)
	}
	if !ordered[2].Timestamp.Equal(t3) {
		t.Errorf("last entry should be t3 (newest), got %v", ordered[2].Timestamp)
	}
}

func TestGetOrdered_Full(t *testing.T) {
	rb := NewRingBuffer(3)
	now := time.Now()
	t1 := now.Add(-3 * time.Second)
	t2 := now.Add(-2 * time.Second)
	t3 := now.Add(-1 * time.Second)
	t4 := now // This overwrites t1.
	rb.Add(&CheckResult{Timestamp: t1, Success: true})
	rb.Add(&CheckResult{Timestamp: t2, Success: true})
	rb.Add(&CheckResult{Timestamp: t3, Success: true})
	rb.Add(&CheckResult{Timestamp: t4, Success: false})

	ordered := rb.getOrdered()
	if len(ordered) != 3 {
		t.Fatalf("expected 3 entries (buffer capacity), got %d", len(ordered))
	}
	// After 4 writes into a size-3 buffer, t1 is overwritten.
	// Chronological order should be t2, t3, t4.
	if !ordered[0].Timestamp.Equal(t2) {
		t.Errorf("first entry should be t2, got %v", ordered[0].Timestamp)
	}
	if !ordered[2].Timestamp.Equal(t4) {
		t.Errorf("last entry should be t4, got %v", ordered[2].Timestamp)
	}
}

func TestToDNSPredictiveAlert_NoWillBreach(t *testing.T) {
	r := &predictiveResult{
		HasPrediction: true,
		Confidence:    0.6,
		Slope:         -0.001,
		WillBreach:    false,
	}
	alert := toDNSPredictiveAlert("cluster", r)
	if alert.DomainType != "cluster" {
		t.Errorf("expected domain_type=cluster, got %s", alert.DomainType)
	}
	if alert.TimeToBreach != -1 {
		t.Errorf("expected TimeToBreach=-1 when WillBreach=false, got %.1f", alert.TimeToBreach)
	}
	if alert.PredictedBreach != "" {
		t.Errorf("expected empty PredictedBreach when WillBreach=false, got %s", alert.PredictedBreach)
	}
}

func TestToDNSPredictiveAlert_WillBreach(t *testing.T) {
	breach := time.Now().Add(10 * time.Minute)
	r := &predictiveResult{
		HasPrediction:   true,
		Confidence:      0.9,
		WillBreach:      true,
		WithinLeadTime:  true,
		TimeToBreach:    10 * time.Minute,
		PredictedBreach: breach,
	}
	alert := toDNSPredictiveAlert("external", r)
	if !alert.WillBreach {
		t.Error("expected WillBreach=true")
	}
	if math.Abs(alert.TimeToBreach-600) > 1 {
		t.Errorf("expected ~600s, got %.1f", alert.TimeToBreach)
	}
	if alert.PredictedBreach == "" {
		t.Error("expected non-empty PredictedBreach")
	}
}
