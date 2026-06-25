package controller

import (
	"context"
	"testing"
	"time"
)

// newTestCorrelator returns a Correlator with no storage/metrics/events wired,
// which is sufficient for exercising the in-memory detection and injection
// paths. minNodes controls MinNodesForCorrelation.
func newTestCorrelator(minNodes int) *Correlator {
	cfg := &CorrelationConfig{
		Enabled:                true,
		ClusterWideThreshold:   0.3,
		EvaluationInterval:     30 * time.Second,
		MinNodesForCorrelation: minNodes,
	}
	return NewCorrelator(cfg, nil, nil, nil)
}

// reportWithProblems builds a NodeReport for nodeName whose ActiveProblems are
// the supplied problem types.
func reportWithProblems(nodeName string, problemTypes ...string) *NodeReport {
	problems := make([]ProblemSummary, 0, len(problemTypes))
	for _, pt := range problemTypes {
		problems = append(problems, ProblemSummary{
			Type:       pt,
			Severity:   "warning",
			Message:    pt + " active",
			Source:     "test",
			DetectedAt: time.Now(),
			LastSeenAt: time.Now(),
		})
	}
	return &NodeReport{
		NodeName:       nodeName,
		Timestamp:      time.Now(),
		OverallHealth:  HealthStatusDegraded,
		ActiveProblems: problems,
	}
}

// findCorrelationByPattern returns the active correlation whose metadata
// "pattern" key matches name, or nil.
func findCorrelationByPattern(corrs []*Correlation, name string) *Correlation {
	for _, c := range corrs {
		if p, ok := c.Metadata["pattern"]; ok && p == name {
			return c
		}
	}
	return nil
}

// TestDetectCommonCauseCorrelation exercises the common-cause detection entry
// point for the built-in "resource-exhaustion" pattern (MemoryPressure +
// DiskPressure), both above and below the node threshold.
func TestDetectCommonCauseCorrelation(t *testing.T) {
	tests := []struct {
		name        string
		minNodes    int
		reports     []*NodeReport
		wantDetect  bool
		wantNodes   []string
		wantProblem []string
	}{
		{
			name:     "at threshold detects",
			minNodes: 2,
			reports: []*NodeReport{
				reportWithProblems("node-a", "MemoryPressure", "DiskPressure"),
				reportWithProblems("node-b", "MemoryPressure", "DiskPressure"),
			},
			wantDetect:  true,
			wantNodes:   []string{"node-a", "node-b"},
			wantProblem: []string{"MemoryPressure", "DiskPressure"},
		},
		{
			name:     "below threshold does not detect",
			minNodes: 2,
			reports: []*NodeReport{
				reportWithProblems("node-a", "MemoryPressure", "DiskPressure"),
				reportWithProblems("node-b", "MemoryPressure"), // missing DiskPressure
			},
			wantDetect: false,
		},
		{
			name:     "partial problem set on a node is not a match",
			minNodes: 1,
			reports: []*NodeReport{
				reportWithProblems("node-a", "MemoryPressure"), // only one of two
			},
			wantDetect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestCorrelator(tt.minNodes)
			for _, r := range tt.reports {
				c.UpdateNodeReport(r)
			}

			c.EvaluateNow(context.Background())

			active := c.GetActiveCorrelations()
			corr := findCorrelationByPattern(active, "resource-exhaustion")

			if tt.wantDetect {
				if corr == nil {
					t.Fatalf("expected resource-exhaustion correlation, got none (active=%d)", len(active))
				}
				if corr.Type != CorrelationTypeCommonCause {
					t.Errorf("type = %q, want %q", corr.Type, CorrelationTypeCommonCause)
				}
				if len(corr.AffectedNodes) != len(tt.wantNodes) {
					t.Errorf("affected nodes = %v, want %v", corr.AffectedNodes, tt.wantNodes)
				}
				if len(corr.ProblemTypes) != len(tt.wantProblem) {
					t.Errorf("problem types = %v, want %v", corr.ProblemTypes, tt.wantProblem)
				}
				if corr.Status != CorrelationStatusActive {
					t.Errorf("status = %q, want %q", corr.Status, CorrelationStatusActive)
				}
			} else if corr != nil {
				t.Fatalf("expected no resource-exhaustion correlation, got %+v", corr)
			}
		})
	}
}

// TestDetectInfrastructureConfidence verifies confidence and affected-node
// calculation for the infrastructure detection path. Two of two nodes share a
// problem type => ratio 1.0 => critical severity, confidence 1.0.
func TestDetectInfrastructureConfidence(t *testing.T) {
	c := newTestCorrelator(2)
	c.UpdateNodeReport(reportWithProblems("node-a", "DNSFailure"))
	c.UpdateNodeReport(reportWithProblems("node-b", "DNSFailure"))

	c.EvaluateNow(context.Background())

	active := c.GetActiveCorrelations()
	var infra *Correlation
	for _, corr := range active {
		if corr.Type == CorrelationTypeInfrastructure {
			infra = corr
			break
		}
	}
	if infra == nil {
		t.Fatalf("expected an infrastructure correlation, got none (active=%d)", len(active))
	}

	if infra.Confidence != 1.0 {
		t.Errorf("confidence = %v, want 1.0", infra.Confidence)
	}
	if infra.Severity != "critical" {
		t.Errorf("severity = %q, want critical (ratio >= 0.5)", infra.Severity)
	}
	if len(infra.AffectedNodes) != 2 {
		t.Errorf("affected nodes = %v, want 2", infra.AffectedNodes)
	}
	if r, ok := infra.Metadata["ratio"].(float64); !ok || r != 1.0 {
		t.Errorf("metadata ratio = %v, want 1.0", infra.Metadata["ratio"])
	}
}

// TestGetStatsAfterDetection verifies the summary-shaped accessor reflects the
// post-detection state.
func TestGetStatsAfterDetection(t *testing.T) {
	c := newTestCorrelator(2)
	c.UpdateNodeReport(reportWithProblems("node-a", "MemoryPressure", "DiskPressure"))
	c.UpdateNodeReport(reportWithProblems("node-b", "MemoryPressure", "DiskPressure"))

	// Before evaluation: no active correlations, 2 tracked nodes.
	pre := c.GetStats()
	if pre.TrackedNodes != 2 {
		t.Errorf("pre TrackedNodes = %d, want 2", pre.TrackedNodes)
	}
	if pre.ActiveCorrelations != 0 {
		t.Errorf("pre ActiveCorrelations = %d, want 0", pre.ActiveCorrelations)
	}

	c.EvaluateNow(context.Background())

	post := c.GetStats()
	if post.ActiveCorrelations < 1 {
		t.Errorf("post ActiveCorrelations = %d, want >= 1", post.ActiveCorrelations)
	}
	if post.TrackedNodes != 2 {
		t.Errorf("post TrackedNodes = %d, want 2", post.TrackedNodes)
	}
	if post.LastEvalTime.IsZero() {
		t.Errorf("post LastEvalTime should be set after evaluation")
	}
}

// TestInjectProblemPatternValidation covers the validation behavior of the
// injection API.
func TestInjectProblemPatternValidation(t *testing.T) {
	tests := []struct {
		name        string
		patternName string
		problems    []string
		wantErr     bool
	}{
		{
			name:        "empty name is rejected",
			patternName: "",
			problems:    []string{"A", "B"},
			wantErr:     true,
		},
		{
			name:        "whitespace name is rejected",
			patternName: "   ",
			problems:    []string{"A", "B"},
			wantErr:     true,
		},
		{
			name:        "empty problems is rejected",
			patternName: "custom",
			problems:    nil,
			wantErr:     true,
		},
		{
			name:        "valid pattern is accepted",
			patternName: "custom",
			problems:    []string{"A", "B"},
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestCorrelator(2)
			err := c.InjectProblemPattern(CorrelationTypeCommonCause, tt.problems, tt.patternName, "desc")
			if tt.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			c.mu.RLock()
			stored := len(c.injectedPatterns)
			c.mu.RUnlock()
			if tt.wantErr && stored != 0 {
				t.Errorf("rejected pattern should not be stored, found %d", stored)
			}
			if !tt.wantErr && stored != 1 {
				t.Errorf("valid pattern should be stored once, found %d", stored)
			}
		})
	}
}

// TestInjectProblemPatternIdempotentReinjection verifies re-injecting under the
// same name replaces rather than duplicates.
func TestInjectProblemPatternIdempotentReinjection(t *testing.T) {
	c := newTestCorrelator(2)

	if err := c.InjectProblemPattern(CorrelationTypeCommonCause, []string{"A", "B"}, "custom", "v1"); err != nil {
		t.Fatalf("first inject: %v", err)
	}
	if err := c.InjectProblemPattern(CorrelationTypeCommonCause, []string{"A", "C"}, "custom", "v2"); err != nil {
		t.Fatalf("second inject: %v", err)
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.injectedPatterns) != 1 {
		t.Fatalf("expected 1 stored pattern after re-injection, got %d", len(c.injectedPatterns))
	}
	got := c.injectedPatterns[0]
	if got.description != "v2" || len(got.problems) != 2 || got.problems[1] != "C" {
		t.Errorf("re-injection did not replace pattern: %+v", got)
	}
}

// TestInjectedPatternParticipatesInDetection is the core end-to-end check:
// an injected custom pattern must be detected when enough nodes match.
func TestInjectedPatternParticipatesInDetection(t *testing.T) {
	c := newTestCorrelator(2)

	customProblems := []string{"GPUOverheat", "ThermalThrottle"}
	if err := c.InjectProblemPattern(CorrelationTypeCommonCause, customProblems, "gpu-thermal", "GPU thermal event"); err != nil {
		t.Fatalf("inject: %v", err)
	}

	// Two nodes share both injected problem types => should match (>= minNodes).
	c.UpdateNodeReport(reportWithProblems("node-a", "GPUOverheat", "ThermalThrottle"))
	c.UpdateNodeReport(reportWithProblems("node-b", "GPUOverheat", "ThermalThrottle"))
	// A third node has only one of the two => must not be counted as a match.
	c.UpdateNodeReport(reportWithProblems("node-c", "GPUOverheat"))

	c.EvaluateNow(context.Background())

	active := c.GetActiveCorrelations()
	corr := findCorrelationByPattern(active, "gpu-thermal")
	if corr == nil {
		t.Fatalf("expected injected gpu-thermal correlation, got none (active=%d)", len(active))
	}
	if corr.Type != CorrelationTypeCommonCause {
		t.Errorf("type = %q, want %q", corr.Type, CorrelationTypeCommonCause)
	}
	if len(corr.AffectedNodes) != 2 {
		t.Errorf("affected nodes = %v, want exactly [node-a node-b]", corr.AffectedNodes)
	}
	for _, n := range corr.AffectedNodes {
		if n == "node-c" {
			t.Errorf("node-c (partial match) should not be in affected nodes: %v", corr.AffectedNodes)
		}
	}
	if len(corr.ProblemTypes) != 2 {
		t.Errorf("problem types = %v, want 2", corr.ProblemTypes)
	}
	// Confidence = matching nodes / total reports = 2/3.
	if corr.Confidence <= 0 || corr.Confidence > 1.0 {
		t.Errorf("confidence = %v, want in (0,1]", corr.Confidence)
	}
}

// TestInjectedPatternBelowThresholdNotDetected verifies an injected pattern
// matched by too few nodes does not produce a correlation.
func TestInjectedPatternBelowThresholdNotDetected(t *testing.T) {
	c := newTestCorrelator(3) // require 3 nodes

	if err := c.InjectProblemPattern(CorrelationTypeCommonCause, []string{"X", "Y"}, "xy-pattern", "desc"); err != nil {
		t.Fatalf("inject: %v", err)
	}

	c.UpdateNodeReport(reportWithProblems("node-a", "X", "Y"))
	c.UpdateNodeReport(reportWithProblems("node-b", "X", "Y"))
	// Only 2 match, threshold is 3.

	c.EvaluateNow(context.Background())

	if corr := findCorrelationByPattern(c.GetActiveCorrelations(), "xy-pattern"); corr != nil {
		t.Fatalf("expected no xy-pattern correlation below threshold, got %+v", corr)
	}
}

// TestInjectedPatternDoesNotShadowBuiltin ensures an injected pattern reusing a
// built-in name is deduped (the built-in remains, no duplicate detection).
func TestInjectedPatternDoesNotShadowBuiltin(t *testing.T) {
	c := newTestCorrelator(2)

	// Inject using the built-in name "resource-exhaustion" but with different
	// problems. The dedupe-by-name logic should drop the injected copy.
	if err := c.InjectProblemPattern(CorrelationTypeCommonCause, []string{"Bogus1", "Bogus2"}, "resource-exhaustion", "shadow"); err != nil {
		t.Fatalf("inject: %v", err)
	}

	// Feed the built-in problem set.
	c.UpdateNodeReport(reportWithProblems("node-a", "MemoryPressure", "DiskPressure"))
	c.UpdateNodeReport(reportWithProblems("node-b", "MemoryPressure", "DiskPressure"))

	c.EvaluateNow(context.Background())

	count := 0
	for _, corr := range c.GetActiveCorrelations() {
		if p, ok := corr.Metadata["pattern"]; ok && p == "resource-exhaustion" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 resource-exhaustion correlation (no shadowing), got %d", count)
	}
}
