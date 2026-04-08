// Package network provides network-related health monitoring capabilities.
package network

import (
	"fmt"
	"math"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// PredictiveAlertConfig configures the DNS predictive alerting system.
// It uses linear regression on recent check results to forecast when the success
// rate will breach the failure threshold configured in SuccessRateTracking.
type PredictiveAlertConfig struct {
	// Enabled activates predictive alerting. Disabled by default.
	Enabled bool `json:"enabled"`

	// PredictionWindow is how far into the future to look for a predicted breach.
	// Alerts are only generated when the projected breach falls within this window.
	// Default: 30 minutes.
	PredictionWindow time.Duration `json:"predictionWindow"`

	// WarningLeadTime is the amount of advance notice to give before a predicted breach.
	// If the breach is predicted to occur within this window, the DNSPredictedDegradation
	// condition is marked WithinLeadTime so callers can escalate urgency.
	// Default: 15 minutes.
	WarningLeadTime time.Duration `json:"warningLeadTime"`

	// MinDataPoints is the minimum number of ring-buffer entries required before
	// a prediction is attempted. Too few points produce unreliable regressions.
	// Default: 10.
	MinDataPoints int `json:"minDataPoints"`

	// ConfidenceThreshold is the minimum R² value (0–1) that a regression must
	// achieve before generating an alert. Low confidence means the trend is
	// too noisy to act on. Default: 0.8.
	ConfidenceThreshold float64 `json:"confidenceThreshold"`
}

// predictiveResult holds the intermediate outcome of a regression analysis.
// It is an internal type; callers convert to types.DNSPredictiveAlert for export.
type predictiveResult struct {
	HasPrediction   bool
	Confidence      float64       // R² score (0–1)
	Slope           float64       // Success-rate change per second (negative = degrading)
	Intercept       float64       // Estimated success rate at the oldest sample
	CurrentRate     float64       // Predicted success rate right now
	TimeToBreach    time.Duration // Time until the rate crosses the failure threshold
	PredictedBreach time.Time     // Wall-clock breach time
	WillBreach      bool          // Breach predicted within PredictionWindow
	WithinLeadTime  bool          // Breach predicted within WarningLeadTime
}

// getOrdered returns ring-buffer entries in chronological order (oldest first).
// Must only be called while the ring buffer's owning mutex is held.
func (rb *RingBuffer) getOrdered() []*CheckResult {
	if rb.count == 0 {
		return nil
	}
	out := make([]*CheckResult, 0, rb.count)
	if rb.count < rb.size {
		// Buffer not yet full: entries are at indices [0, count-1] in insertion order.
		// This invariant depends on NewRingBuffer initialising writeIndex to 0 (Go zero-value)
		// and Add() writing to writeIndex before incrementing — do not change Add() or
		// NewRingBuffer without updating this branch.
		for i := 0; i < rb.count; i++ {
			if rb.buffer[i] != nil {
				out = append(out, rb.buffer[i])
			}
		}
	} else {
		// Buffer is full: the oldest entry sits at writeIndex; iterate circularly.
		for i := 0; i < rb.size; i++ {
			idx := (rb.writeIndex + i) % rb.size
			if rb.buffer[idx] != nil {
				out = append(out, rb.buffer[idx])
			}
		}
	}
	return out
}

// analyzeRingBuffer performs ordinary least-squares linear regression on the
// check results stored in rb and predicts when the DNS success rate will breach
// the failure threshold.
//
//   - failureThreshold: maximum acceptable failure rate (e.g. 0.3 = 30%).
//     The success-rate breach point is (1 - failureThreshold).
//   - cfg: predictive alert configuration.
//
// Must only be called while the ring buffer's owning mutex is held.
func analyzeRingBuffer(rb *RingBuffer, failureThreshold float64, cfg *PredictiveAlertConfig) *predictiveResult {
	result := &predictiveResult{}
	if cfg == nil || !cfg.Enabled {
		return result
	}

	entries := rb.getOrdered()
	if len(entries) < cfg.MinDataPoints {
		return result
	}

	// Use the oldest entry's timestamp as the time origin (x = 0) to keep
	// x-values small and avoid floating-point precision loss.
	origin := entries[0].Timestamp

	n := float64(len(entries))
	var sumX, sumY, sumXY, sumXX float64
	for _, e := range entries {
		x := e.Timestamp.Sub(origin).Seconds()
		y := 0.0
		if e.Success {
			y = 1.0
		}
		sumX += x
		sumY += y
		sumXY += x * y
		sumXX += x * x
	}

	// OLS slope: m = (n·Σxy − Σx·Σy) / (n·Σx² − (Σx)²)
	denominator := n*sumXX - sumX*sumX
	if math.Abs(denominator) < 1e-12 {
		// All timestamps are identical; regression is undefined.
		return result
	}
	slope := (n*sumXY - sumX*sumY) / denominator
	intercept := (sumY - slope*sumX) / n

	// Compute R² to assess how well the line fits the data.
	yMean := sumY / n
	var ssTot, ssRes float64
	for _, e := range entries {
		x := e.Timestamp.Sub(origin).Seconds()
		y := 0.0
		if e.Success {
			y = 1.0
		}
		predicted := slope*x + intercept
		diff := y - yMean
		ssTot += diff * diff
		resDiff := y - predicted
		ssRes += resDiff * resDiff
	}

	var confidence float64
	if ssTot < 1e-12 {
		// All values are identical — the trend is perfectly flat (no degradation).
		// Confidence is set to 0 so no alert fires.
		confidence = 0.0
	} else {
		confidence = 1.0 - ssRes/ssTot
		if confidence < 0 {
			confidence = 0
		}
	}

	// Project the current success rate.
	xNow := time.Since(origin).Seconds()
	currentRate := math.Max(0, math.Min(1, slope*xNow+intercept))

	result.HasPrediction = true
	result.Confidence = confidence
	result.Slope = slope
	result.Intercept = intercept
	result.CurrentRate = currentRate

	// Compute the predicted breach time only when:
	//   1. The regression is sufficiently reliable (R² ≥ ConfidenceThreshold).
	//   2. The slope is negative (success rate is degrading).
	//   3. The current rate is still above the threshold (hasn't already breached).
	successRateThreshold := 1.0 - failureThreshold
	if confidence < cfg.ConfidenceThreshold || slope >= 0 || currentRate <= successRateThreshold {
		return result
	}

	// Solve: slope·x + intercept = successRateThreshold  →  x = (threshold − intercept) / slope
	xBreach := (successRateThreshold - intercept) / slope
	timeToBreach := time.Duration((xBreach - xNow) * float64(time.Second))
	if timeToBreach <= 0 {
		// Breach is already in the past relative to the regression line.
		return result
	}

	result.TimeToBreach = timeToBreach
	result.PredictedBreach = time.Now().Add(timeToBreach)
	result.WillBreach = timeToBreach <= cfg.PredictionWindow
	result.WithinLeadTime = timeToBreach <= cfg.WarningLeadTime
	return result
}

// buildPredictiveConditionMessage formats a human-readable message for the
// DNSPredictedDegradation condition.
func buildPredictiveConditionMessage(domainType string, r *predictiveResult) string {
	mins := int(r.TimeToBreach.Minutes())
	if mins < 1 {
		mins = 1
	}
	return fmt.Sprintf(
		"%s DNS success rate predicted to breach threshold in ~%d minute(s) "+
			"(predicted at %s, confidence %.0f%%)",
		domainType,
		mins,
		r.PredictedBreach.UTC().Format("15:04:05 UTC"),
		r.Confidence*100,
	)
}

// toDNSPredictiveAlert converts an internal predictiveResult to the exported
// types.DNSPredictiveAlert for Prometheus metric recording.
//
// TimeToBreach is set whenever a future breach was computed (r.TimeToBreach > 0),
// regardless of whether it falls inside PredictionWindow. This ensures the Prometheus
// confidence gauge always has a matching time-to-breach value for operators who want
// to observe long-horizon degradation trends. The exporter only writes the
// DNSPredictedBreachSeconds gauge when WillBreach=true (breach within window).
func toDNSPredictiveAlert(domainType string, r *predictiveResult) types.DNSPredictiveAlert {
	alert := types.DNSPredictiveAlert{
		DomainType:     domainType,
		Confidence:     r.Confidence,
		TimeToBreach:   -1,
		WillBreach:     r.WillBreach,
		WithinLeadTime: r.WithinLeadTime,
	}
	if r.TimeToBreach > 0 {
		// Expose the predicted breach time even when it falls outside PredictionWindow,
		// so operators can observe early-stage degradation trends via confidence + time.
		alert.TimeToBreach = r.TimeToBreach.Seconds()
		alert.PredictedBreach = r.PredictedBreach.UTC().Format(time.RFC3339)
	}
	return alert
}
