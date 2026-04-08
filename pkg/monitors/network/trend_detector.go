// Package network provides network-related health monitoring capabilities.
package network

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// TrendDetectionConfig configures statistical trend detection for DNS monitoring.
// Trend detection analyses the history of per-cycle success rates to surface gradual
// degradation, statistical anomalies, and intermittent flapping before they breach
// the fixed failure-rate threshold.
type TrendDetectionConfig struct {
	// Enabled activates trend detection. Disabled by default.
	Enabled bool `json:"enabled"`

	// WindowSize is the number of observations retained in the circular buffer for
	// slope and flap calculations. Older entries are evicted as new ones arrive.
	// The long-term Welford baseline accumulates all observations ever seen.
	// Default: 20.
	WindowSize int `json:"windowSize"`

	// DegradationThreshold is the minimum linear-regression slope (success-rate change
	// per sample) that triggers a DNSDegrading condition. Because a declining rate
	// produces a negative slope, this value should be negative (e.g. -0.05 means a
	// 5%-per-sample downward trend triggers a warning).
	// Default: -0.05.
	DegradationThreshold float64 `json:"degradationThreshold"`

	// AnomalyZScore is the absolute z-score magnitude that triggers a DNSAnomalous
	// condition. A z-score of 2.5 means the current rate is 2.5 standard deviations
	// from the long-term mean. Default: 2.5.
	AnomalyZScore float64 `json:"anomalyZScore"`

	// FlapDetection configures intermittent healthy↔unhealthy transition detection.
	FlapDetection *FlapDetectionConfig `json:"flapDetection"`
}

// FlapDetectionConfig configures intermittent DNS failure (flapping) detection.
type FlapDetectionConfig struct {
	// Enabled activates flap detection within the trend detector. Default: true when
	// TrendDetection is enabled.
	Enabled bool `json:"enabled"`

	// MinOscillations is the minimum number of healthy↔unhealthy transitions within
	// WindowMinutes that constitutes flapping. Default: 3.
	MinOscillations int `json:"minOscillations"`

	// WindowMinutes is the rolling time window in which transitions are counted.
	// Default: 10.
	WindowMinutes int `json:"windowMinutes"`
}

// trendSample is one observation stored in the trend detector's circular buffer.
type trendSample struct {
	rate      float64
	timestamp time.Time
}

// TrendDetector accumulates DNS success-rate observations and detects anomalous
// patterns using an online Welford estimator (for z-score) and linear regression
// over a bounded circular window (for slope and flap detection).
//
// TrendDetector is safe for concurrent use; all exported methods acquire td.mu.
type TrendDetector struct {
	mu  sync.Mutex
	cfg *TrendDetectionConfig

	// Circular buffer of the most recent WindowSize success-rate samples.
	buf   []trendSample
	head  int // next write index
	count int // filled entries (capped at len(buf))

	// Welford's online algorithm state — accumulates ALL observations ever seen so
	// that the baseline mean and variance represent the long-term distribution, not
	// just the current window. This lets us detect short-term anomalies relative to
	// a stable historical norm even after many cycles.
	wn    int     // total samples seen
	wmean float64 // running mean (Welford)
	wM2   float64 // running sum of squared deviations (Welford)
}

// TrendResult holds the outcome of a single Analyze call.
type TrendResult struct {
	// Count is the number of samples currently in the circular buffer.
	Count int

	// Mean is the long-term mean success rate (Welford, all observations).
	Mean float64

	// StdDev is the long-term standard deviation (Welford, all observations).
	StdDev float64

	// ZScore is how far the most recent sample sits from the long-term mean, measured
	// in standard deviations. Negative means the current rate is below average (bad).
	ZScore float64

	// Slope is the linear-regression slope of success rates over the circular window,
	// in success-rate units per sample. Negative means the rate is declining.
	Slope float64

	// Degrading is true when Slope is more negative than TrendDetectionConfig.DegradationThreshold.
	Degrading bool

	// Anomalous is true when |ZScore| exceeds TrendDetectionConfig.AnomalyZScore.
	Anomalous bool

	// Flapping is true when the number of healthy↔unhealthy transitions in the recent
	// time window reaches TrendDetectionConfig.FlapDetection.MinOscillations.
	Flapping bool

	// Oscillations is the raw count of healthy↔unhealthy transitions in the flap window.
	Oscillations int

	// DegradationThreshold is the configured slope threshold that triggered the Degrading flag.
	// Included in TrendResult so condition messages can display the configured threshold.
	DegradationThreshold float64
}

// NewTrendDetector creates a TrendDetector configured with cfg.
// cfg must be non-nil; the caller is responsible for applying defaults before calling.
func NewTrendDetector(cfg *TrendDetectionConfig) *TrendDetector {
	size := cfg.WindowSize
	if size <= 0 {
		size = 20
	}
	return &TrendDetector{
		cfg: cfg,
		buf: make([]trendSample, size),
	}
}

// Observe records a new success-rate sample (rate in [0, 1]).
// Thread-safe.
func (td *TrendDetector) Observe(rate float64, t time.Time) {
	td.mu.Lock()
	defer td.mu.Unlock()

	// Write into circular buffer.
	td.buf[td.head] = trendSample{rate: rate, timestamp: t}
	td.head = (td.head + 1) % len(td.buf)
	if td.count < len(td.buf) {
		td.count++
	}

	// Welford's online mean+variance update (Knuth/Welford algorithm).
	// This runs on all observations (not just the window) to build a stable baseline.
	td.wn++
	delta := rate - td.wmean
	td.wmean += delta / float64(td.wn)
	td.wM2 += delta * (rate - td.wmean)
}

// Analyze returns the current trend analysis based on buffered observations.
// Returns a zero TrendResult when fewer than one sample has been observed.
// Thread-safe.
func (td *TrendDetector) Analyze() TrendResult {
	td.mu.Lock()
	defer td.mu.Unlock()

	res := TrendResult{Count: td.count}
	if td.count == 0 {
		return res
	}

	// Retrieve ordered samples (oldest → newest).
	ordered := td.orderedSamples()

	// Long-term baseline from Welford's accumulator.
	res.Mean = td.wmean
	if td.wn > 1 {
		res.StdDev = math.Sqrt(td.wM2 / float64(td.wn-1))
	}

	// Z-score of the most recent sample vs the long-term distribution.
	current := ordered[len(ordered)-1].rate
	if res.StdDev > 0 {
		res.ZScore = (current - res.Mean) / res.StdDev
	}

	// Linear regression slope over the circular window.
	if td.count >= 2 {
		res.Slope = trendSlope(ordered)
	}

	// Apply configured thresholds.
	res.DegradationThreshold = td.cfg.DegradationThreshold
	res.Degrading = res.Slope < td.cfg.DegradationThreshold
	res.Anomalous = math.Abs(res.ZScore) > td.cfg.AnomalyZScore

	// Flap detection.
	if td.cfg.FlapDetection != nil && td.cfg.FlapDetection.Enabled {
		res.Oscillations, res.Flapping = td.countOscillations(ordered)
	}

	return res
}

// orderedSamples returns buf entries in chronological order (oldest first).
// Caller must hold td.mu.
func (td *TrendDetector) orderedSamples() []trendSample {
	out := make([]trendSample, td.count)
	size := len(td.buf)
	if td.count < size {
		// Buffer not yet full: entries are at indices [0, count-1].
		copy(out, td.buf[:td.count])
	} else {
		// Full buffer: oldest entry is at td.head (the next write position).
		for i := 0; i < td.count; i++ {
			out[i] = td.buf[(td.head+i)%size]
		}
	}
	return out
}

// trendSlope computes the ordinary least-squares slope of the success rates in
// samples, using the sample index (0, 1, ..., n-1) as the x-axis.
// Returns success-rate change per sample (negative → declining).
func trendSlope(samples []trendSample) float64 {
	n := float64(len(samples))
	if n < 2 {
		return 0
	}

	// OLS: slope = (n·Σxy − Σx·Σy) / (n·Σx² − (Σx)²)
	var sumX, sumY, sumXY, sumX2 float64
	for i, s := range samples {
		x := float64(i)
		y := s.rate
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}

	denom := n*sumX2 - sumX*sumX
	if denom == 0 {
		return 0
	}
	return (n*sumXY - sumX*sumY) / denom
}

// countOscillations counts healthy↔unhealthy transitions in the configured time
// window. A transition occurs when consecutive in-window samples cross the 0.5
// success-rate boundary (below = unhealthy).
// Caller must hold td.mu.
func (td *TrendDetector) countOscillations(ordered []trendSample) (int, bool) {
	cfg := td.cfg.FlapDetection
	if cfg == nil || !cfg.Enabled || len(ordered) < 2 {
		return 0, false
	}

	winMinutes := cfg.WindowMinutes
	if winMinutes <= 0 {
		winMinutes = 10
	}
	cutoff := time.Now().Add(-time.Duration(winMinutes) * time.Minute)

	const flapThreshold = 0.5 // below this success rate → unhealthy
	osc := 0
	// Seed prevHealthy from the first in-window sample.
	prevHealthy := true
	seeded := false

	for _, s := range ordered {
		if s.timestamp.Before(cutoff) {
			continue
		}
		currHealthy := s.rate >= flapThreshold
		if !seeded {
			prevHealthy = currHealthy
			seeded = true
			continue
		}
		if currHealthy != prevHealthy {
			osc++
			prevHealthy = currHealthy
		}
	}

	minOsc := cfg.MinOscillations
	if minOsc <= 0 {
		minOsc = 3
	}
	return osc, osc >= minOsc
}

// AddTrendConditions appends DNSDegrading, DNSAnomalous, and/or DNSFlapping conditions
// to status based on res. conditionSource is a human-readable label such as "Cluster"
// or "External" that appears in condition reason strings.
func AddTrendConditions(res TrendResult, conditionSource string, status *types.Status) {
	if res.Count == 0 {
		return
	}

	if res.Degrading {
		status.AddCondition(types.NewCondition(
			"DNSDegrading",
			types.ConditionTrue,
			fmt.Sprintf("%sDNSDegrading", conditionSource),
			fmt.Sprintf("%s DNS success rate is trending downward (slope=%.4f/sample, threshold=%.4f/sample)",
				conditionSource, res.Slope, res.DegradationThreshold),
		))
	}

	if res.Anomalous {
		direction := "above"
		if res.ZScore < 0 {
			direction = "below"
		}
		status.AddCondition(types.NewCondition(
			"DNSAnomalous",
			types.ConditionTrue,
			fmt.Sprintf("%sDNSAnomalous", conditionSource),
			fmt.Sprintf("%s DNS success rate is anomalous: z-score=%.2f (current %.1f%% is %s the %.1f%% long-term mean)",
				conditionSource, res.ZScore, res.ZScore*res.StdDev*100+res.Mean*100, direction, res.Mean*100),
		))
	}

	if res.Flapping {
		status.AddCondition(types.NewCondition(
			"DNSFlapping",
			types.ConditionTrue,
			fmt.Sprintf("%sDNSFlapping", conditionSource),
			fmt.Sprintf("%s DNS is flapping: %d healthy↔unhealthy transitions in the detection window",
				conditionSource, res.Oscillations),
		))
	}
}
