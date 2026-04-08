// Package network provides network-related health monitoring capabilities.
// It includes DNS resolution monitoring for cluster and external domains,
// latency measurement, and nameserver verification.
package network

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/supporttools/node-doctor/pkg/monitors"
	"github.com/supporttools/node-doctor/pkg/types"
)

// DNSQuery represents a custom DNS query configuration.
type DNSQuery struct {
	Domain             string
	RecordType         string // Currently only "A" is supported
	TestEachNameserver bool   // Test this domain against each nameserver individually
	ConsistencyCheck   bool   // Enable consistency checking with multiple rapid queries
}

// NameserverDomainStatus tracks the status of DNS resolution for a specific
// nameserver and domain combination.
//
// Fields are read and written exclusively while holding DNSMonitor.mu. Any future
// access from a goroutine not holding that lock would require adding a per-struct mutex.
type NameserverDomainStatus struct {
	Nameserver   string
	Domain       string
	FailureCount int
	LastSuccess  time.Time
	LastLatency  time.Duration
}

// CheckResult represents a single DNS check outcome for success rate tracking.
type CheckResult struct {
	Timestamp time.Time
	Success   bool
	Latency   time.Duration
}

// RingBuffer implements a fixed-size circular buffer for tracking check results.
// It provides O(1) insertions and O(n) success rate calculation.
type RingBuffer struct {
	buffer     []*CheckResult
	size       int
	writeIndex int
	count      int // Number of valid entries (up to size)
}

// NewRingBuffer creates a new ring buffer with specified capacity.
func NewRingBuffer(size int) *RingBuffer {
	if size <= 0 {
		size = 10 // Default minimum size
	}
	return &RingBuffer{
		buffer: make([]*CheckResult, size),
		size:   size,
	}
}

// Add appends a result to the buffer, overwriting the oldest entry if full.
func (rb *RingBuffer) Add(result *CheckResult) {
	rb.buffer[rb.writeIndex] = result
	rb.writeIndex = (rb.writeIndex + 1) % rb.size
	if rb.count < rb.size {
		rb.count++
	}
}

// GetSuccessRate calculates the success percentage in the buffer.
// Returns 0.0 if no entries exist.
func (rb *RingBuffer) GetSuccessRate() float64 {
	if rb.count == 0 {
		return 0.0
	}

	successCount := 0
	for i := 0; i < rb.count; i++ {
		if rb.buffer[i] != nil && rb.buffer[i].Success {
			successCount++
		}
	}

	return float64(successCount) / float64(rb.count)
}

// GetFailureRate calculates the failure percentage in the buffer.
func (rb *RingBuffer) GetFailureRate() float64 {
	return 1.0 - rb.GetSuccessRate()
}

// Count returns the number of valid entries in the buffer.
func (rb *RingBuffer) Count() int {
	return rb.count
}

// Size returns the capacity of the buffer.
func (rb *RingBuffer) Size() int {
	return rb.size
}

// SuccessRateConfig holds configuration for success rate tracking.
type SuccessRateConfig struct {
	Enabled              bool    `json:"enabled"`
	WindowSize           int     `json:"windowSize"`           // Number of checks to track (default: 10)
	FailureRateThreshold float64 `json:"failureRateThreshold"` // Alert if failure rate exceeds this (default: 0.3 = 30%)
	MinSamplesRequired   int     `json:"minSamplesRequired"`   // Minimum samples before alerting (default: 5)
}

// ConsistencyCheckConfig holds configuration for DNS consistency checking.
// Consistency checking performs multiple rapid queries to detect intermittent DNS issues.
type ConsistencyCheckConfig struct {
	Enabled                bool          `json:"enabled"`
	QueriesPerCheck        int           `json:"queriesPerCheck"`        // Number of rapid queries (default: 5)
	IntervalBetweenQueries time.Duration `json:"intervalBetweenQueries"` // Delay between queries (default: 200ms)
}

// DNSErrorType represents the classification of DNS errors for better diagnostics.
type DNSErrorType string

const (
	// DNSErrorNone indicates no error occurred; the DNS check was successful.
	DNSErrorNone DNSErrorType = "None"

	// DNSErrorTimeout indicates the DNS query timed out.
	// Suggests: Network issues, server overload, or firewall blocking.
	DNSErrorTimeout DNSErrorType = "Timeout"

	// DNSErrorNXDOMAIN indicates the domain does not exist.
	// Suggests: Typo in domain, missing DNS record, or DNS zone not configured.
	DNSErrorNXDOMAIN DNSErrorType = "NXDOMAIN"

	// DNSErrorSERVFAIL indicates the server failed to complete the query.
	// Suggests: Upstream DNS server error or DNSSEC validation failure.
	DNSErrorSERVFAIL DNSErrorType = "SERVFAIL"

	// DNSErrorRefused indicates the connection to the DNS server was refused.
	// Suggests: DNS server down, wrong port, or firewall blocking.
	DNSErrorRefused DNSErrorType = "Refused"

	// DNSErrorTemporary indicates a temporary/transient DNS failure.
	// Suggests: Retry may succeed.
	DNSErrorTemporary DNSErrorType = "Temporary"

	// DNSErrorUnknown indicates an unclassified DNS error.
	DNSErrorUnknown DNSErrorType = "Unknown"
)

// classifyDNSError analyzes a DNS error and returns its classification.
// This helps identify root causes: NXDOMAIN suggests misconfiguration,
// while Timeout suggests infrastructure issues.
func classifyDNSError(err error) DNSErrorType {
	if err == nil {
		return DNSErrorNone
	}

	// Check for net.DNSError type first (most specific information)
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		if dnsErr.IsNotFound {
			return DNSErrorNXDOMAIN
		}
		if dnsErr.IsTemporary {
			return DNSErrorTemporary
		}
	}

	// Fall back to string matching for error messages
	errStr := err.Error()
	switch {
	case strings.Contains(errStr, "i/o timeout"),
		strings.Contains(errStr, "context deadline exceeded"):
		return DNSErrorTimeout
	case strings.Contains(errStr, "no such host"):
		return DNSErrorNXDOMAIN
	case strings.Contains(errStr, "server misbehaving"):
		return DNSErrorSERVFAIL
	case strings.Contains(errStr, "connection refused"):
		return DNSErrorRefused
	default:
		return DNSErrorUnknown
	}
}

// NameserverHealthScoringConfig configures the composite health scoring system for DNS nameservers.
// Each check cycle produces a 0-100 score per nameserver from four weighted components.
type NameserverHealthScoringConfig struct {
	Enabled bool `json:"enabled"`

	// DegradedThreshold is the score below which a nameserver is considered degraded (default: 70).
	DegradedThreshold float64 `json:"degradedThreshold"`

	// UnhealthyThreshold is the score below which a nameserver is considered unhealthy (default: 40).
	UnhealthyThreshold float64 `json:"unhealthyThreshold"`

	// Weights for each scoring component. Must sum to 1.0.
	SuccessRateWeight    float64 `json:"successRateWeight"`    // default: 0.40
	LatencyWeight        float64 `json:"latencyWeight"`        // default: 0.25
	ErrorDiversityWeight float64 `json:"errorDiversityWeight"` // default: 0.15
	ConsistencyWeight    float64 `json:"consistencyWeight"`    // default: 0.20

	// WindowSize is the number of checks to retain per nameserver (default: 20).
	WindowSize int `json:"windowSize"`

	// LatencyBaseline is the expected healthy p95 latency (default: 50ms).
	// Scores 100 at or below this value.
	LatencyBaseline time.Duration `json:"latencyBaseline"`

	// LatencyMax is the latency at which the latency score hits 0 (default: 2s).
	LatencyMax time.Duration `json:"latencyMax"`
}

// NameserverCheckResult is a single DNS check result recorded in a nameserver's stats window.
type NameserverCheckResult struct {
	Timestamp time.Time
	Success   bool
	Latency   time.Duration
	ErrType   DNSErrorType
}

// NameserverStats maintains a sliding window of check results for a single nameserver.
// It is used to compute composite health scores each cycle.
type NameserverStats struct {
	mu      sync.Mutex
	results []*NameserverCheckResult
	wIdx    int // next write index
	count   int // valid entries (up to len(results))
}

func newNameserverStats(windowSize int) *NameserverStats {
	if windowSize <= 0 {
		windowSize = 20
	}
	return &NameserverStats{
		results: make([]*NameserverCheckResult, windowSize),
	}
}

// add records a new check result into the sliding window.
func (ns *NameserverStats) add(r *NameserverCheckResult) {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	ns.results[ns.wIdx] = r
	ns.wIdx = (ns.wIdx + 1) % len(ns.results)
	if ns.count < len(ns.results) {
		ns.count++
	}
}

// Len returns the number of valid entries currently in the sliding window.
// It is safe to call concurrently.
func (ns *NameserverStats) Len() int {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	return ns.count
}

// snapshot returns a copy of the valid results in the sliding window, safe for
// concurrent access. The returned slice always has length ns.count.
//
// # Slot-ordering behavior
//
// The backing array is a ring buffer. Results are stored in physical slot order,
// not necessarily in chronological (insertion) order:
//
//   - Before wrap-around (fewer entries added than window capacity):
//     Slots 0..count-1 are in chronological order (oldest at index 0, most
//     recent at index count-1). snapshot() returns them oldest-first.
//
//   - After wrap-around (window is full and wIdx has looped back to 0):
//     The oldest surviving entry lives at slot wIdx, not slot 0. snapshot()
//     still copies slots 0..count-1, so the returned slice is in slot order,
//     NOT chronological order. Example with capacity=3:
//
//       add(r0) → slots: [r0, _, _]
//       add(r1) → slots: [r0, r1, _]
//       add(r2) → slots: [r0, r1, r2]   snapshot → [r0, r1, r2]  (ordered)
//       add(r3) → slots: [r3, r1, r2]   snapshot → [r3, r1, r2]  (slot order!)
//       add(r4) → slots: [r3, r4, r2]   snapshot → [r3, r4, r2]  (slot order!)
//
// # Callers must not assume chronological order
//
// All current callers (computeHealthScore, success-rate trackers) compute
// aggregate statistics (counts, sums, averages) over the window, so slot order
// vs. chronological order does not affect their results.
//
// Any future caller that needs chronological ordering must reconstruct it:
//
//	raw := ns.snapshot()
//	start := 0
//	if ns.Len() == cap(ns.results) {
//	    start = (ns.wIdx) % cap(ns.results)  // requires holding ns.mu — use carefully
//	}
//	// rotate raw[start:] ++ raw[:start] for oldest-first order.
//
// Because wIdx is unexported and not exposed, callers outside this package that
// need ordered results should sort by NameserverCheckResult.Timestamp instead.
func (ns *NameserverStats) snapshot() []*NameserverCheckResult {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	out := make([]*NameserverCheckResult, ns.count)
	copy(out, ns.results[:ns.count])
	return out
}

// computeHealthScore calculates a composite 0-100 health score from the stats window.
// Requires at least minSamples valid entries; returns "insufficient_data" status otherwise.
func computeHealthScore(ns *NameserverStats, cfg *NameserverHealthScoringConfig, nameserver string) types.NameserverHealthScore {
	const minSamples = 3

	results := ns.snapshot()
	score := types.NameserverHealthScore{
		Nameserver:  nameserver,
		SampleCount: len(results),
	}

	if len(results) < minSamples {
		score.Status = "insufficient_data"
		return score
	}

	// --- Success Rate Score (0-100) ---
	successes := 0
	var successLatencies []float64
	errorSeverityTotal := 0
	for _, r := range results {
		if r.Success {
			successes++
			successLatencies = append(successLatencies, float64(r.Latency))
		} else {
			errorSeverityTotal += errorSeverity(r.ErrType)
		}
	}
	successRate := float64(successes) / float64(len(results))
	score.SuccessScore = successRate * 100

	// --- Latency Score (0-100) based on p95 of successful queries ---
	// If no successful checks exist, score 0 (unknown latency is treated as worst-case).
	score.LatencyScore = 0.0
	if len(successLatencies) >= 2 {
		p95 := percentile95(successLatencies)
		baseline := float64(cfg.LatencyBaseline)
		latMax := float64(cfg.LatencyMax)
		if p95 <= baseline {
			score.LatencyScore = 100
		} else if p95 >= latMax {
			score.LatencyScore = 0
		} else {
			score.LatencyScore = 100 * (latMax - p95) / (latMax - baseline)
		}
	}

	// --- Error Diversity Score (0-100) ---
	// Higher severity errors reduce score more. Max severity per check = 3 (Timeout).
	maxSeverity := len(results) * 3
	if maxSeverity > 0 {
		score.ErrorScore = (1.0 - float64(errorSeverityTotal)/float64(maxSeverity)) * 100
	} else {
		score.ErrorScore = 100
	}
	if score.ErrorScore < 0 {
		score.ErrorScore = 0
	}

	// --- Consistency Score (0-100) based on coefficient of variation of latencies ---
	// If no successful checks, score 0. If 1-2 points, score 100 (can't measure variance yet).
	score.ConsistencyScore = 0.0
	if len(successLatencies) == 1 || len(successLatencies) == 2 {
		score.ConsistencyScore = 100.0 // insufficient data for variance, assume consistent
	}
	if len(successLatencies) >= 3 {
		mean := mean64(successLatencies)
		if mean > 0 {
			stddev := stddev64(successLatencies, mean)
			cv := stddev / mean
			// cv in [0, 0.1] → 100; cv >= 2.0 → 0; linear in between
			const cvMin, cvMax = 0.1, 2.0
			if cv <= cvMin {
				score.ConsistencyScore = 100
			} else if cv >= cvMax {
				score.ConsistencyScore = 0
			} else {
				score.ConsistencyScore = 100 * (cvMax - cv) / (cvMax - cvMin)
			}
		}
	}

	// --- Composite Score ---
	score.Score = score.SuccessScore*cfg.SuccessRateWeight +
		score.LatencyScore*cfg.LatencyWeight +
		score.ErrorScore*cfg.ErrorDiversityWeight +
		score.ConsistencyScore*cfg.ConsistencyWeight

	switch {
	case score.Score < cfg.UnhealthyThreshold:
		score.Status = "unhealthy"
	case score.Score < cfg.DegradedThreshold:
		score.Status = "degraded"
	default:
		score.Status = "healthy"
	}

	return score
}

// errorSeverity maps a DNS error type to a severity weight used in the error diversity score.
// Higher values represent more impactful error types.
func errorSeverity(e DNSErrorType) int {
	switch e {
	case DNSErrorNone:
		return 0
	case DNSErrorTimeout:
		return 3
	case DNSErrorSERVFAIL, DNSErrorRefused, DNSErrorNXDOMAIN:
		return 2
	default: // Temporary, Unknown
		return 1
	}
}

// percentile95 returns the 95th percentile of a float64 slice using linear interpolation.
// Input need not be sorted; a copy is made internally.
func percentile95(data []float64) float64 {
	if len(data) == 0 {
		return 0
	}
	sorted := make([]float64, len(data))
	copy(sorted, data)
	sortFloat64s(sorted)
	idx := 0.95 * float64(len(sorted)-1)
	lo := int(idx)
	hi := lo + 1
	if hi >= len(sorted) {
		return sorted[lo]
	}
	frac := idx - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}

// sortFloat64s sorts in ascending order (insertion sort; n is small).
func sortFloat64s(a []float64) {
	for i := 1; i < len(a); i++ {
		key := a[i]
		j := i - 1
		for j >= 0 && a[j] > key {
			a[j+1] = a[j]
			j--
		}
		a[j+1] = key
	}
}

func mean64(a []float64) float64 {
	if len(a) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range a {
		sum += v
	}
	return sum / float64(len(a))
}

func stddev64(a []float64, mean float64) float64 {
	if len(a) < 2 {
		return 0
	}
	variance := 0.0
	for _, v := range a {
		d := v - mean
		variance += d * d
	}
	variance /= float64(len(a) - 1)
	if variance <= 0 {
		return 0
	}
	return math.Sqrt(variance)
}

// DNSMonitorConfig holds the configuration for the DNS monitor.
type DNSMonitorConfig struct {
	// ClusterDomains are Kubernetes cluster internal domains to test
	ClusterDomains []string `json:"clusterDomains"`

	// ExternalDomains are external domains to test for internet connectivity
	ExternalDomains []string `json:"externalDomains"`

	// CustomQueries are additional custom DNS queries to perform
	CustomQueries []DNSQuery `json:"customQueries"`

	// LatencyThreshold is the maximum acceptable DNS query latency
	LatencyThreshold time.Duration `json:"latencyThreshold"`

	// NameserverCheckEnabled enables checking nameserver reachability
	NameserverCheckEnabled bool `json:"nameserverCheckEnabled"`

	// ResolverPath is the path to the resolver configuration file
	ResolverPath string `json:"resolverPath"`

	// FailureCountThreshold is the number of consecutive failures before reporting NetworkUnreachable
	FailureCountThreshold int `json:"failureCountThreshold"`

	// SuccessRateTracking configures sliding window success rate tracking
	SuccessRateTracking *SuccessRateConfig `json:"successRateTracking"`

	// ConsistencyChecking configures DNS consistency verification via multiple rapid queries
	ConsistencyChecking *ConsistencyCheckConfig `json:"consistencyChecking"`

	// HealthScoring configures the per-nameserver composite health scoring system.
	HealthScoring *NameserverHealthScoringConfig `json:"healthScoring"`

	// Correlation configures cross-domain and cross-nameserver failure pattern detection.
	Correlation *CorrelationConfig `json:"correlation"`

	// PredictiveAlerting configures early-warning breach prediction using linear regression.
	PredictiveAlerting *PredictiveAlertConfig `json:"predictiveAlerting"`
}

// DNSMonitor monitors DNS resolution health.
type DNSMonitor struct {
	name     string
	config   *DNSMonitorConfig
	resolver Resolver

	// Failure tracking for NetworkUnreachable condition
	mu                   sync.Mutex
	clusterFailureCount  int
	externalFailureCount int

	// Per-nameserver domain failure tracking (key: "nameserver:domain")
	nameserverDomainStatus map[string]*NameserverDomainStatus

	// Success rate tracking with sliding window
	clusterSuccessTracker  *RingBuffer
	externalSuccessTracker *RingBuffer

	// Latency metrics collected during each check cycle (for Prometheus export)
	latencyMetrics []types.DNSLatency

	// Per-nameserver sliding-window stats for composite health scoring.
	// Keyed by nameserver IP string. Populated by checkNameservers when HealthScoring is enabled.
	nameserverStats map[string]*NameserverStats

	// correlationEngine analyses failure patterns across nameservers and domains.
	// Nil when correlation is disabled.
	correlationEngine *CorrelationEngine

	// BaseMonitor for lifecycle management
	*monitors.BaseMonitor
}

// init registers the DNS monitor with the monitor registry.
func init() {
	monitors.MustRegister(monitors.MonitorInfo{
		Type:        "network-dns-check",
		Factory:     NewDNSMonitor,
		Validator:   ValidateDNSConfig,
		Description: "Monitors DNS resolution for cluster and external domains",
		DefaultConfig: &types.MonitorConfig{
			Name:           "dns-health",
			Type:           "network-dns-check",
			Enabled:        true,
			IntervalString: "30s",
			TimeoutString:  "10s",
			Config: map[string]interface{}{
				"clusterDomains":         []interface{}{"kubernetes.default.svc.cluster.local"},
				"externalDomains":        []interface{}{"google.com", "cloudflare.com"},
				"latencyThreshold":       "1s",
				"checkNameservers":       true,
				"failureCountThreshold":  3,
				"enableNameserverChecks": true,
			},
		},
	})
}

// NewDNSMonitor creates a new DNS monitor instance.
func NewDNSMonitor(ctx context.Context, config types.MonitorConfig) (types.Monitor, error) {
	// Parse DNS-specific configuration
	dnsConfig, err := parseDNSConfig(config.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse DNS config: %w", err)
	}

	// Apply defaults
	if err := dnsConfig.applyDefaults(); err != nil {
		return nil, fmt.Errorf("failed to apply defaults: %w", err)
	}

	// Create base monitor
	baseMonitor, err := monitors.NewBaseMonitor(config.Name, config.Interval, config.Timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to create base monitor: %w", err)
	}

	// Create DNS monitor
	monitor := &DNSMonitor{
		name:                   config.Name,
		config:                 dnsConfig,
		resolver:               newDefaultResolver(),
		nameserverDomainStatus: make(map[string]*NameserverDomainStatus),
		clusterSuccessTracker:  NewRingBuffer(dnsConfig.SuccessRateTracking.WindowSize),
		externalSuccessTracker: NewRingBuffer(dnsConfig.SuccessRateTracking.WindowSize),
		nameserverStats:        make(map[string]*NameserverStats),
		BaseMonitor:            baseMonitor,
	}

	// Initialise the correlation engine if enabled.
	if dnsConfig.Correlation != nil && dnsConfig.Correlation.Enabled {
		monitor.correlationEngine = newCorrelationEngine(dnsConfig.Correlation)
	}

	// Set the check function
	if err := baseMonitor.SetCheckFunc(monitor.checkDNS); err != nil {
		return nil, fmt.Errorf("failed to set check function: %w", err)
	}

	return monitor, nil
}

// ValidateDNSConfig validates the DNS monitor configuration.
func ValidateDNSConfig(config types.MonitorConfig) error {
	if config.Name == "" {
		return fmt.Errorf("monitor name is required")
	}

	if config.Type != "network-dns-check" {
		return fmt.Errorf("invalid monitor type: expected network-dns-check, got %s", config.Type)
	}

	// Parse and validate DNS-specific config
	dnsConfig, err := parseDNSConfig(config.Config)
	if err != nil {
		return fmt.Errorf("failed to parse DNS config: %w", err)
	}

	// Apply defaults for validation
	if err := dnsConfig.applyDefaults(); err != nil {
		return fmt.Errorf("failed to apply defaults: %w", err)
	}

	// Validate latency threshold
	if dnsConfig.LatencyThreshold <= 0 {
		return fmt.Errorf("latencyThreshold must be positive, got %v", dnsConfig.LatencyThreshold)
	}

	// Validate failure count threshold
	if dnsConfig.FailureCountThreshold < 1 {
		return fmt.Errorf("failureCountThreshold must be at least 1, got %d", dnsConfig.FailureCountThreshold)
	}

	// Validate success rate tracking config if enabled
	if dnsConfig.SuccessRateTracking != nil && dnsConfig.SuccessRateTracking.Enabled {
		srt := dnsConfig.SuccessRateTracking

		if srt.WindowSize <= 0 || srt.WindowSize > 10000 {
			return fmt.Errorf("successRateTracking.windowSize must be between 1 and 10000, got %d", srt.WindowSize)
		}

		if srt.MinSamplesRequired <= 0 {
			return fmt.Errorf("successRateTracking.minSamplesRequired must be at least 1, got %d", srt.MinSamplesRequired)
		}

		if srt.MinSamplesRequired > srt.WindowSize {
			return fmt.Errorf("successRateTracking.minSamplesRequired (%d) cannot exceed windowSize (%d)", srt.MinSamplesRequired, srt.WindowSize)
		}

		if srt.FailureRateThreshold <= 0 || srt.FailureRateThreshold > 1.0 {
			return fmt.Errorf("successRateTracking.failureRateThreshold must be between 0 and 1.0, got %.2f", srt.FailureRateThreshold)
		}
	}

	// Validate consistency checking config if enabled
	if dnsConfig.ConsistencyChecking != nil && dnsConfig.ConsistencyChecking.Enabled {
		cc := dnsConfig.ConsistencyChecking

		if cc.QueriesPerCheck < 2 || cc.QueriesPerCheck > 20 {
			return fmt.Errorf("consistencyChecking.queriesPerCheck must be between 2 and 20, got %d", cc.QueriesPerCheck)
		}

		if cc.IntervalBetweenQueries < 10*time.Millisecond || cc.IntervalBetweenQueries > 5*time.Second {
			return fmt.Errorf("consistencyChecking.intervalBetweenQueries must be between 10ms and 5s, got %v", cc.IntervalBetweenQueries)
		}
	}

	// Validate health scoring config if enabled
	if dnsConfig.HealthScoring != nil && dnsConfig.HealthScoring.Enabled {
		hs := dnsConfig.HealthScoring

		// Threshold ordering: unhealthy must be strictly less than degraded
		if hs.UnhealthyThreshold >= hs.DegradedThreshold {
			return fmt.Errorf("healthScoring.unhealthyThreshold (%g) must be less than degradedThreshold (%g)",
				hs.UnhealthyThreshold, hs.DegradedThreshold)
		}
		if hs.UnhealthyThreshold < 0 || hs.DegradedThreshold > 100 {
			return fmt.Errorf("healthScoring thresholds must be in range [0, 100]")
		}

		// Weights must sum to 1.0 (within floating-point tolerance)
		weightSum := hs.SuccessRateWeight + hs.LatencyWeight + hs.ErrorDiversityWeight + hs.ConsistencyWeight
		if weightSum < 0.99 || weightSum > 1.01 {
			return fmt.Errorf("healthScoring weights must sum to 1.0 (got %.4f: successRate=%.2f latency=%.2f errorDiversity=%.2f consistency=%.2f)",
				weightSum, hs.SuccessRateWeight, hs.LatencyWeight, hs.ErrorDiversityWeight, hs.ConsistencyWeight)
		}

		// WindowSize bounds
		if hs.WindowSize < 3 || hs.WindowSize > 1000 {
			return fmt.Errorf("healthScoring.windowSize must be between 3 and 1000, got %d", hs.WindowSize)
		}
	}

	// Validate that at least one type of DNS check is configured
	if len(dnsConfig.ClusterDomains) == 0 && len(dnsConfig.ExternalDomains) == 0 && len(dnsConfig.CustomQueries) == 0 {
		return fmt.Errorf("at least one of clusterDomains, externalDomains, or customQueries must be configured")
	}

	return nil
}

// parseDNSConfig parses the DNS monitor configuration from a map.
//
//nolint:gocyclo // Config parsing functions have inherently high complexity due to many fields
func parseDNSConfig(configMap map[string]interface{}) (*DNSMonitorConfig, error) {
	if configMap == nil {
		return &DNSMonitorConfig{}, nil
	}

	config := &DNSMonitorConfig{}

	// Parse ClusterDomains
	if val, ok := configMap["clusterDomains"]; ok {
		switch v := val.(type) {
		case []interface{}:
			config.ClusterDomains = make([]string, len(v))
			for i, domain := range v {
				if str, ok := domain.(string); ok {
					config.ClusterDomains[i] = str
				} else {
					return nil, fmt.Errorf("clusterDomains[%d] must be a string", i)
				}
			}
		case []string:
			config.ClusterDomains = v
		default:
			return nil, fmt.Errorf("clusterDomains must be a string array")
		}
	}

	// Parse ExternalDomains
	if val, ok := configMap["externalDomains"]; ok {
		switch v := val.(type) {
		case []interface{}:
			config.ExternalDomains = make([]string, len(v))
			for i, domain := range v {
				if str, ok := domain.(string); ok {
					config.ExternalDomains[i] = str
				} else {
					return nil, fmt.Errorf("externalDomains[%d] must be a string", i)
				}
			}
		case []string:
			config.ExternalDomains = v
		default:
			return nil, fmt.Errorf("externalDomains must be a string array")
		}
	}

	// Parse CustomQueries
	if val, ok := configMap["customQueries"]; ok {
		queriesInterface, ok := val.([]interface{})
		if !ok {
			return nil, fmt.Errorf("customQueries must be an array")
		}
		config.CustomQueries = make([]DNSQuery, len(queriesInterface))
		for i, queryInterface := range queriesInterface {
			queryMap, ok := queryInterface.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("customQueries[%d] must be an object", i)
			}

			domain, ok := queryMap["domain"].(string)
			if !ok {
				return nil, fmt.Errorf("customQueries[%d].domain must be a string", i)
			}

			recordType := "A" // Default
			if rt, ok := queryMap["recordType"].(string); ok {
				recordType = rt
			}

			testEachNameserver := false
			if tns, ok := queryMap["testEachNameserver"].(bool); ok {
				testEachNameserver = tns
			}

			consistencyCheck := false
			if cc, ok := queryMap["consistencyCheck"].(bool); ok {
				consistencyCheck = cc
			}

			config.CustomQueries[i] = DNSQuery{
				Domain:             domain,
				RecordType:         recordType,
				TestEachNameserver: testEachNameserver,
				ConsistencyCheck:   consistencyCheck,
			}
		}
	}

	// Parse LatencyThreshold
	if val, ok := configMap["latencyThreshold"]; ok {
		switch v := val.(type) {
		case string:
			duration, err := time.ParseDuration(v)
			if err != nil {
				return nil, fmt.Errorf("invalid latencyThreshold duration: %w", err)
			}
			config.LatencyThreshold = duration
		case float64:
			// Treat as seconds
			config.LatencyThreshold = time.Duration(v * float64(time.Second))
		case int:
			// Treat as seconds
			config.LatencyThreshold = time.Duration(v) * time.Second
		default:
			return nil, fmt.Errorf("latencyThreshold must be a duration string or number")
		}
	}

	// Parse NameserverCheckEnabled
	if val, ok := configMap["nameserverCheckEnabled"]; ok {
		enabled, ok := val.(bool)
		if !ok {
			return nil, fmt.Errorf("nameserverCheckEnabled must be a boolean")
		}
		config.NameserverCheckEnabled = enabled
	}

	// Parse ResolverPath
	if val, ok := configMap["resolverPath"]; ok {
		path, ok := val.(string)
		if !ok {
			return nil, fmt.Errorf("resolverPath must be a string")
		}
		config.ResolverPath = path
	}

	// Parse FailureCountThreshold
	if val, ok := configMap["failureCountThreshold"]; ok {
		switch v := val.(type) {
		case float64:
			config.FailureCountThreshold = int(v)
		case int:
			config.FailureCountThreshold = v
		default:
			return nil, fmt.Errorf("failureCountThreshold must be an integer")
		}
	}

	// Parse SuccessRateTracking
	if val, ok := configMap["successRateTracking"]; ok {
		srtMap, ok := val.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("successRateTracking must be an object")
		}

		srt := &SuccessRateConfig{}

		// Parse enabled
		if enabled, ok := srtMap["enabled"].(bool); ok {
			srt.Enabled = enabled
		}

		// Parse windowSize
		if ws, ok := srtMap["windowSize"]; ok {
			switch v := ws.(type) {
			case float64:
				srt.WindowSize = int(v)
			case int:
				srt.WindowSize = v
			default:
				return nil, fmt.Errorf("successRateTracking.windowSize must be an integer")
			}
		}

		// Parse failureRateThreshold (accepts percentage 0-100 or decimal 0.0-1.0)
		if frt, ok := srtMap["failureRateThreshold"]; ok {
			switch v := frt.(type) {
			case float64:
				// If value > 1, treat as percentage and convert to decimal
				if v > 1.0 {
					srt.FailureRateThreshold = v / 100.0
				} else {
					srt.FailureRateThreshold = v
				}
				if srt.FailureRateThreshold < 0 || srt.FailureRateThreshold > 1.0 {
					return nil, fmt.Errorf("successRateTracking.failureRateThreshold must be between 0 and 100 (or 0.0 and 1.0)")
				}
			case int:
				// Treat as percentage
				if v < 0 || v > 100 {
					return nil, fmt.Errorf("successRateTracking.failureRateThreshold must be between 0 and 100 (percentage), got %d", v)
				}
				srt.FailureRateThreshold = float64(v) / 100.0
			default:
				return nil, fmt.Errorf("successRateTracking.failureRateThreshold must be a number")
			}
		}

		// Parse minSamplesRequired
		if msr, ok := srtMap["minSamplesRequired"]; ok {
			switch v := msr.(type) {
			case float64:
				srt.MinSamplesRequired = int(v)
			case int:
				srt.MinSamplesRequired = v
			default:
				return nil, fmt.Errorf("successRateTracking.minSamplesRequired must be an integer")
			}
		}

		config.SuccessRateTracking = srt
	}

	// Parse ConsistencyChecking
	if val, ok := configMap["consistencyChecking"]; ok {
		ccMap, ok := val.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("consistencyChecking must be an object")
		}

		cc := &ConsistencyCheckConfig{}

		// Parse enabled
		if enabled, ok := ccMap["enabled"].(bool); ok {
			cc.Enabled = enabled
		}

		// Parse queriesPerCheck
		if qpc, ok := ccMap["queriesPerCheck"]; ok {
			switch v := qpc.(type) {
			case float64:
				cc.QueriesPerCheck = int(v)
			case int:
				cc.QueriesPerCheck = v
			default:
				return nil, fmt.Errorf("consistencyChecking.queriesPerCheck must be an integer")
			}
		}

		// Parse intervalBetweenQueries
		if ibq, ok := ccMap["intervalBetweenQueries"]; ok {
			switch v := ibq.(type) {
			case string:
				duration, err := time.ParseDuration(v)
				if err != nil {
					return nil, fmt.Errorf("invalid consistencyChecking.intervalBetweenQueries duration: %w", err)
				}
				cc.IntervalBetweenQueries = duration
			case float64:
				// Treat as milliseconds
				cc.IntervalBetweenQueries = time.Duration(v) * time.Millisecond
			case int:
				// Treat as milliseconds
				cc.IntervalBetweenQueries = time.Duration(v) * time.Millisecond
			default:
				return nil, fmt.Errorf("consistencyChecking.intervalBetweenQueries must be a duration string or number")
			}
		}

		config.ConsistencyChecking = cc
	}

	// Parse HealthScoring
	if val, ok := configMap["healthScoring"]; ok {
		hsMap, ok := val.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("healthScoring must be an object")
		}

		hs := &NameserverHealthScoringConfig{}

		if enabled, ok := hsMap["enabled"].(bool); ok {
			hs.Enabled = enabled
		}

		if v, ok := hsMap["degradedThreshold"].(float64); ok {
			hs.DegradedThreshold = v
		}
		if v, ok := hsMap["unhealthyThreshold"].(float64); ok {
			hs.UnhealthyThreshold = v
		}
		if v, ok := hsMap["successRateWeight"].(float64); ok {
			hs.SuccessRateWeight = v
		}
		if v, ok := hsMap["latencyWeight"].(float64); ok {
			hs.LatencyWeight = v
		}
		if v, ok := hsMap["errorDiversityWeight"].(float64); ok {
			hs.ErrorDiversityWeight = v
		}
		if v, ok := hsMap["consistencyWeight"].(float64); ok {
			hs.ConsistencyWeight = v
		}
		if v, ok := hsMap["windowSize"]; ok {
			switch n := v.(type) {
			case float64:
				hs.WindowSize = int(n)
			case int:
				hs.WindowSize = n
			}
		}
		if v, ok := hsMap["latencyBaseline"].(string); ok {
			d, err := time.ParseDuration(v)
			if err != nil {
				return nil, fmt.Errorf("invalid healthScoring.latencyBaseline: %w", err)
			}
			hs.LatencyBaseline = d
		}
		if v, ok := hsMap["latencyMax"].(string); ok {
			d, err := time.ParseDuration(v)
			if err != nil {
				return nil, fmt.Errorf("invalid healthScoring.latencyMax: %w", err)
			}
			hs.LatencyMax = d
		}

		config.HealthScoring = hs
	}

	// Parse PredictiveAlerting
	if val, ok := configMap["predictiveAlerting"]; ok {
		paMap, ok := val.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("predictiveAlerting must be an object")
		}
		pa := &PredictiveAlertConfig{}
		if enabled, ok := paMap["enabled"].(bool); ok {
			pa.Enabled = enabled
		}
		if v, ok := paMap["predictionWindow"].(string); ok {
			d, err := time.ParseDuration(v)
			if err != nil {
				return nil, fmt.Errorf("invalid predictiveAlerting.predictionWindow: %w", err)
			}
			pa.PredictionWindow = d
		}
		if v, ok := paMap["warningLeadTime"].(string); ok {
			d, err := time.ParseDuration(v)
			if err != nil {
				return nil, fmt.Errorf("invalid predictiveAlerting.warningLeadTime: %w", err)
			}
			pa.WarningLeadTime = d
		}
		if v, ok := paMap["minDataPoints"]; ok {
			switch n := v.(type) {
			case float64:
				pa.MinDataPoints = int(n)
			case int:
				pa.MinDataPoints = n
			}
		}
		if v, ok := paMap["confidenceThreshold"].(float64); ok {
			pa.ConfidenceThreshold = v
		}
		config.PredictiveAlerting = pa
	}

	// Parse Correlation
	if val, ok := configMap["correlation"]; ok {
		corrMap, ok := val.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("correlation must be an object")
		}
		corr := &CorrelationConfig{}
		if enabled, ok := corrMap["enabled"].(bool); ok {
			corr.Enabled = enabled
		}
		if v, ok := corrMap["minConfidence"].(float64); ok {
			corr.MinConfidence = v
		}
		if v, ok := corrMap["windowMinutes"]; ok {
			switch n := v.(type) {
			case float64:
				corr.WindowMinutes = int(n)
			case int:
				corr.WindowMinutes = n
			}
		}
		if v, ok := corrMap["nameserverFailureThreshold"].(float64); ok {
			corr.NameserverFailureThreshold = v
		}
		if v, ok := corrMap["minNameserversForDomainCorrelation"]; ok {
			switch n := v.(type) {
			case float64:
				corr.MinNameserversForDomainCorrelation = int(n)
			case int:
				corr.MinNameserversForDomainCorrelation = n
			}
		}
		config.Correlation = corr
	}

	return config, nil
}

// applyDefaults applies default values to the DNS monitor configuration.
func (c *DNSMonitorConfig) applyDefaults() error {
	// Default cluster domains - only apply if not explicitly set (nil vs empty slice)
	if c.ClusterDomains == nil {
		c.ClusterDomains = []string{"kubernetes.default.svc.cluster.local"}
	}

	// Default external domains - only apply if not explicitly set (nil vs empty slice)
	if c.ExternalDomains == nil {
		c.ExternalDomains = []string{"google.com", "cloudflare.com"}
	}

	// Default latency threshold
	if c.LatencyThreshold == 0 {
		c.LatencyThreshold = 1 * time.Second
	}

	// Default nameserver check enabled
	if c.ResolverPath == "" {
		c.ResolverPath = "/etc/resolv.conf"
	}

	// Default failure count threshold
	if c.FailureCountThreshold == 0 {
		c.FailureCountThreshold = 3
	}

	// Default success rate tracking config
	if c.SuccessRateTracking == nil {
		c.SuccessRateTracking = &SuccessRateConfig{
			Enabled:              false, // Disabled by default for backward compatibility
			WindowSize:           10,
			FailureRateThreshold: 0.3, // 30% failure rate threshold
			MinSamplesRequired:   5,
		}
	} else {
		// Apply defaults for fields not explicitly set
		if c.SuccessRateTracking.WindowSize <= 0 {
			c.SuccessRateTracking.WindowSize = 10
		}
		if c.SuccessRateTracking.FailureRateThreshold <= 0 {
			c.SuccessRateTracking.FailureRateThreshold = 0.3
		}
		if c.SuccessRateTracking.MinSamplesRequired <= 0 {
			c.SuccessRateTracking.MinSamplesRequired = 5
		}
	}

	// Default consistency checking config
	if c.ConsistencyChecking == nil {
		c.ConsistencyChecking = &ConsistencyCheckConfig{
			Enabled:                false, // Disabled by default for backward compatibility
			QueriesPerCheck:        5,
			IntervalBetweenQueries: 200 * time.Millisecond,
		}
	} else {
		// Apply defaults for fields not explicitly set
		if c.ConsistencyChecking.QueriesPerCheck <= 0 {
			c.ConsistencyChecking.QueriesPerCheck = 5
		}
		if c.ConsistencyChecking.IntervalBetweenQueries <= 0 {
			c.ConsistencyChecking.IntervalBetweenQueries = 200 * time.Millisecond
		}
	}

	// Default health scoring config
	if c.HealthScoring == nil {
		c.HealthScoring = &NameserverHealthScoringConfig{
			Enabled:              false, // Disabled by default for backward compatibility
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
	} else {
		if c.HealthScoring.DegradedThreshold <= 0 {
			c.HealthScoring.DegradedThreshold = 70
		}
		if c.HealthScoring.UnhealthyThreshold <= 0 {
			c.HealthScoring.UnhealthyThreshold = 40
		}
		if c.HealthScoring.SuccessRateWeight == 0 && c.HealthScoring.LatencyWeight == 0 &&
			c.HealthScoring.ErrorDiversityWeight == 0 && c.HealthScoring.ConsistencyWeight == 0 {
			c.HealthScoring.SuccessRateWeight = 0.40
			c.HealthScoring.LatencyWeight = 0.25
			c.HealthScoring.ErrorDiversityWeight = 0.15
			c.HealthScoring.ConsistencyWeight = 0.20
		}
		if c.HealthScoring.WindowSize <= 0 {
			c.HealthScoring.WindowSize = 20
		}
		if c.HealthScoring.LatencyBaseline <= 0 {
			c.HealthScoring.LatencyBaseline = 50 * time.Millisecond
		}
		if c.HealthScoring.LatencyMax <= 0 {
			c.HealthScoring.LatencyMax = 2 * time.Second
		}
	}

	// Default predictive alerting config — disabled by default for backward compatibility.
	if c.PredictiveAlerting == nil {
		c.PredictiveAlerting = &PredictiveAlertConfig{
			Enabled:             false,
			PredictionWindow:    30 * time.Minute,
			WarningLeadTime:     15 * time.Minute,
			MinDataPoints:       10,
			ConfidenceThreshold: 0.8,
		}
	} else {
		if c.PredictiveAlerting.PredictionWindow <= 0 {
			c.PredictiveAlerting.PredictionWindow = 30 * time.Minute
		}
		if c.PredictiveAlerting.WarningLeadTime <= 0 {
			c.PredictiveAlerting.WarningLeadTime = 15 * time.Minute
		}
		if c.PredictiveAlerting.MinDataPoints <= 0 {
			c.PredictiveAlerting.MinDataPoints = 10
		}
		if c.PredictiveAlerting.ConfidenceThreshold <= 0 {
			c.PredictiveAlerting.ConfidenceThreshold = 0.8
		}
	}

	// Default correlation config — disabled by default for backward compatibility.
	if c.Correlation == nil {
		c.Correlation = &CorrelationConfig{Enabled: false}
	} else {
		if c.Correlation.MinConfidence <= 0 {
			c.Correlation.MinConfidence = 0.7
		}
		if c.Correlation.WindowMinutes <= 0 {
			c.Correlation.WindowMinutes = 5
		}
		if c.Correlation.NameserverFailureThreshold <= 0 {
			c.Correlation.NameserverFailureThreshold = 0.5
		}
		if c.Correlation.MinNameserversForDomainCorrelation <= 0 {
			c.Correlation.MinNameserversForDomainCorrelation = 2
		}
	}

	return nil
}

// healthScoringEnabled returns true when health scoring config is non-nil and enabled.
// Guards against tests that construct DNSMonitor directly without applyDefaults.
func (m *DNSMonitor) healthScoringEnabled() bool {
	return m.config.HealthScoring != nil && m.config.HealthScoring.Enabled
}

// correlationEnabled returns true when correlation analysis is configured and enabled.
func (m *DNSMonitor) correlationEnabled() bool {
	return m.config.Correlation != nil && m.config.Correlation.Enabled && m.correlationEngine != nil
}

// predictiveAlertingEnabled returns true when predictive alerting is configured and enabled.
func (m *DNSMonitor) predictiveAlertingEnabled() bool {
	return m.config.PredictiveAlerting != nil && m.config.PredictiveAlerting.Enabled
}

// checkDNS performs the DNS health check.
func (m *DNSMonitor) checkDNS(ctx context.Context) (*types.Status, error) {
	status := types.NewStatus(m.name)

	// Clear latency metrics for this check cycle
	m.mu.Lock()
	m.latencyMetrics = nil
	m.mu.Unlock()

	// Check cluster DNS
	clusterOK := m.checkClusterDNS(ctx, status)

	// Check external DNS
	externalOK := m.checkExternalDNS(ctx, status)

	// Check custom queries
	m.checkCustomQueries(ctx, status)

	// Check nameservers (also feeds health scoring stats when enabled)
	if m.config.NameserverCheckEnabled || m.healthScoringEnabled() {
		m.checkNameservers(ctx, status)
	}

	// Update failure counters and conditions
	m.updateFailureTracking(clusterOK, externalOK, status)

	// Compute per-nameserver health scores, predictive alerts, and attach to status
	// metadata; run correlation analysis while holding the lock so we get a consistent
	// snapshot of all ring-buffer and stats state.
	m.mu.Lock()
	latencyMetrics := &types.LatencyMetrics{
		DNS: m.latencyMetrics,
	}
	if m.healthScoringEnabled() {
		latencyMetrics.NameserverHealthScores = m.computeNameserverHealthScores(status)
	}
	if m.predictiveAlertingEnabled() {
		m.computePredictiveAlerts(status, latencyMetrics)
	}
	if len(latencyMetrics.DNS) > 0 || len(latencyMetrics.NameserverHealthScores) > 0 ||
		len(latencyMetrics.DNSPredictiveAlerts) > 0 {
		status.SetLatencyMetrics(latencyMetrics)
	}
	if m.correlationEnabled() {
		m.runCorrelationAnalysis(status)
	}
	m.mu.Unlock()

	return status, nil
}

// recordDNSLatency records a DNS latency measurement for Prometheus export.
func (m *DNSMonitor) recordDNSLatency(domain, domainType, dnsServer, recordType string, latency time.Duration, success bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.latencyMetrics = append(m.latencyMetrics, types.DNSLatency{
		DNSServer:  dnsServer,
		Domain:     domain,
		RecordType: recordType,
		DomainType: strings.ToLower(domainType),
		LatencyMs:  float64(latency.Microseconds()) / 1000.0,
		Success:    success,
	})
}

// checkClusterDNS checks cluster DNS resolution.
func (m *DNSMonitor) checkClusterDNS(ctx context.Context, status *types.Status) bool {
	return m.checkDNSDomains(ctx, status, m.config.ClusterDomains, "Cluster")
}

// checkExternalDNS checks external DNS resolution.
func (m *DNSMonitor) checkExternalDNS(ctx context.Context, status *types.Status) bool {
	return m.checkDNSDomains(ctx, status, m.config.ExternalDomains, "External")
}

// checkDNSDomains is a helper function that checks DNS resolution for a list of domains.
// The domainType parameter is used to construct event names (e.g., "Cluster" or "External").
func (m *DNSMonitor) checkDNSDomains(ctx context.Context, status *types.Status, domains []string, domainType string) bool {
	allSuccess := true

	for _, domain := range domains {
		start := time.Now()
		addrs, err := m.resolver.LookupHost(ctx, domain)
		latency := time.Since(start)

		if err != nil {
			allSuccess = false
			errType := classifyDNSError(err)
			status.AddEvent(types.NewEvent(
				types.EventError,
				domainType+"DNSResolutionFailed",
				fmt.Sprintf("[%s] Failed to resolve %s domain %s: %v", errType, strings.ToLower(domainType), domain, err),
			))
			// Record failed DNS query latency
			m.recordDNSLatency(domain, domainType, "system", "A", latency, false)
			continue
		}

		if len(addrs) == 0 {
			allSuccess = false
			status.AddEvent(types.NewEvent(
				types.EventWarning,
				domainType+"DNSNoRecords",
				fmt.Sprintf("No A records found for %s domain %s", strings.ToLower(domainType), domain),
			))
			// Record as failed since no records found
			m.recordDNSLatency(domain, domainType, "system", "A", latency, false)
			continue
		}

		// Record successful DNS query latency
		m.recordDNSLatency(domain, domainType, "system", "A", latency, true)

		// Check latency
		if latency > m.config.LatencyThreshold {
			status.AddEvent(types.NewEvent(
				types.EventWarning,
				"High"+domainType+"DNSLatency",
				fmt.Sprintf("%s DNS resolution for %s took %v (threshold: %v)", domainType, domain, latency, m.config.LatencyThreshold),
			))
		}
	}

	return allSuccess
}

// checkCustomQueries checks custom DNS queries.
func (m *DNSMonitor) checkCustomQueries(ctx context.Context, status *types.Status) {
	for _, query := range m.config.CustomQueries {
		// Currently only A record queries are supported
		if query.RecordType != "A" && query.RecordType != "" {
			status.AddEvent(types.NewEvent(
				types.EventWarning,
				"UnsupportedQueryType",
				fmt.Sprintf("Query type %s is not supported for domain %s (only A records supported)", query.RecordType, query.Domain),
			))
			continue
		}

		// Use per-nameserver testing if enabled
		if query.TestEachNameserver {
			m.checkDomainAgainstNameservers(ctx, status, query.Domain)
			continue
		}

		// Use consistency checking if enabled (both per-query and global)
		if query.ConsistencyCheck && m.config.ConsistencyChecking != nil && m.config.ConsistencyChecking.Enabled {
			m.checkDomainConsistency(ctx, status, query.Domain)
			continue
		}

		// Standard resolution using system resolver
		start := time.Now()
		addrs, err := m.resolver.LookupHost(ctx, query.Domain)
		latency := time.Since(start)

		recordType := query.RecordType
		if recordType == "" {
			recordType = "A"
		}

		if err != nil {
			errType := classifyDNSError(err)
			status.AddEvent(types.NewEvent(
				types.EventError,
				"CustomDNSQueryFailed",
				fmt.Sprintf("[%s] Failed to resolve custom domain %s: %v", errType, query.Domain, err),
			))
			// Record failed custom DNS query latency
			m.recordDNSLatency(query.Domain, "custom", "system", recordType, latency, false)
			continue
		}

		if len(addrs) == 0 {
			status.AddEvent(types.NewEvent(
				types.EventWarning,
				"CustomDNSNoRecords",
				fmt.Sprintf("No A records found for custom domain %s", query.Domain),
			))
			// Record as failed since no records found
			m.recordDNSLatency(query.Domain, "custom", "system", recordType, latency, false)
			continue
		}

		// Record successful custom DNS query latency
		m.recordDNSLatency(query.Domain, "custom", "system", recordType, latency, true)

		// Check latency
		if latency > m.config.LatencyThreshold {
			status.AddEvent(types.NewEvent(
				types.EventWarning,
				"HighCustomDNSLatency",
				fmt.Sprintf("Custom DNS resolution for %s took %v (threshold: %v)", query.Domain, latency, m.config.LatencyThreshold),
			))
		}
	}
}

// checkDomainConsistency performs multiple rapid DNS queries to detect intermittent failures.
// It reports DNSResolutionIntermittent if some queries fail or return different results,
// and DNSResolutionConsistent if all queries succeed with consistent results.
func (m *DNSMonitor) checkDomainConsistency(ctx context.Context, status *types.Status, domain string) {
	cc := m.config.ConsistencyChecking
	queriesCount := cc.QueriesPerCheck
	interval := cc.IntervalBetweenQueries

	// Track results
	type queryResult struct {
		success bool
		addrs   []string
		err     error
		latency time.Duration
	}
	results := make([]queryResult, queriesCount)

	// Perform rapid queries with configured interval
	for i := 0; i < queriesCount; i++ {
		if i > 0 {
			// Wait between queries (not before first query)
			select {
			case <-ctx.Done():
				status.AddEvent(types.NewEvent(
					types.EventWarning,
					"ConsistencyCheckCancelled",
					fmt.Sprintf("Consistency check for %s cancelled after %d/%d queries: %v", domain, i, queriesCount, ctx.Err()),
				))
				return
			case <-time.After(interval):
			}
		}

		start := time.Now()
		addrs, err := m.resolver.LookupHost(ctx, domain)
		results[i] = queryResult{
			success: err == nil && len(addrs) > 0,
			addrs:   addrs,
			err:     err,
			latency: time.Since(start),
		}
	}

	// Analyze results
	successCount := 0
	failureCount := 0
	var totalLatency time.Duration
	allAddrs := make(map[string]int) // Track which IPs were returned and how often
	var firstError error

	for _, r := range results {
		totalLatency += r.latency
		if r.success {
			successCount++
			for _, addr := range r.addrs {
				allAddrs[addr]++
			}
		} else {
			failureCount++
			if firstError == nil {
				firstError = r.err
			}
		}
	}

	// Calculate average latency (guard against division by zero)
	var avgLatency time.Duration
	if queriesCount > 0 {
		avgLatency = totalLatency / time.Duration(queriesCount)
	}

	// Determine consistency of IP addresses
	// IP results are consistent if every successful query returned exactly the same set of IPs
	ipConsistent := true
	if successCount > 1 {
		// Build the first successful query's IP set as reference
		var referenceSet map[string]bool
		for _, r := range results {
			if r.success {
				referenceSet = make(map[string]bool, len(r.addrs))
				for _, addr := range r.addrs {
					referenceSet[addr] = true
				}
				break
			}
		}

		// Compare all other successful queries against the reference set
		for _, r := range results {
			if !r.success {
				continue
			}
			// Check if this result has the same IPs as reference
			if len(r.addrs) != len(referenceSet) {
				ipConsistent = false
				break
			}
			for _, addr := range r.addrs {
				if !referenceSet[addr] {
					ipConsistent = false
					break
				}
			}
			if !ipConsistent {
				break
			}
		}
	}

	// Generate events and conditions based on results
	if failureCount == queriesCount {
		// All queries failed
		errType := DNSErrorUnknown
		if firstError != nil {
			errType = classifyDNSError(firstError)
		}
		status.AddEvent(types.NewEvent(
			types.EventError,
			"ConsistencyCheckAllFailed",
			fmt.Sprintf("[%s] All %d consistency check queries failed for %s: %v", errType, queriesCount, domain, firstError),
		))
		status.AddCondition(types.NewCondition(
			"DNSResolutionDown",
			types.ConditionTrue,
			"AllConsistencyQueriesFailed",
			fmt.Sprintf("Domain %s: all %d rapid queries failed", domain, queriesCount),
		))
	} else if failureCount > 0 {
		// Some queries failed - intermittent issue detected
		errType := DNSErrorUnknown
		if firstError != nil {
			errType = classifyDNSError(firstError)
		}
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"ConsistencyCheckIntermittent",
			fmt.Sprintf("[%s] Intermittent DNS resolution for %s: %d/%d queries succeeded (avg latency: %v)", errType, domain, successCount, queriesCount, avgLatency),
		))
		status.AddCondition(types.NewCondition(
			"DNSResolutionIntermittent",
			types.ConditionTrue,
			"IntermittentConsistencyFailures",
			fmt.Sprintf("Domain %s: %d/%d queries succeeded (%.1f%% success rate)", domain, successCount, queriesCount, float64(successCount)/float64(queriesCount)*100),
		))
	} else if !ipConsistent {
		// All queries succeeded but returned different IPs
		ipList := make([]string, 0, len(allAddrs))
		for ip := range allAddrs {
			ipList = append(ipList, ip)
		}
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"ConsistencyCheckIPVariation",
			fmt.Sprintf("DNS resolution for %s returned varying IPs across %d queries: %s (avg latency: %v)", domain, queriesCount, strings.Join(ipList, ", "), avgLatency),
		))
		status.AddCondition(types.NewCondition(
			"DNSResolutionInconsistent",
			types.ConditionTrue,
			"InconsistentIPAddresses",
			fmt.Sprintf("Domain %s: %d unique IPs returned across %d queries", domain, len(allAddrs), queriesCount),
		))
	} else {
		// All queries succeeded with consistent results
		ipList := make([]string, 0, len(allAddrs))
		for ip := range allAddrs {
			ipList = append(ipList, ip)
		}
		status.AddCondition(types.NewCondition(
			"DNSResolutionConsistent",
			types.ConditionTrue,
			"ConsistentResolution",
			fmt.Sprintf("Domain %s: all %d queries succeeded with consistent IPs %s (avg latency: %v)", domain, queriesCount, strings.Join(ipList, ", "), avgLatency),
		))
	}

	// Check average latency
	if avgLatency > m.config.LatencyThreshold {
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"ConsistencyCheckHighLatency",
			fmt.Sprintf("Average DNS latency for %s across %d queries: %v (threshold: %v)", domain, queriesCount, avgLatency, m.config.LatencyThreshold),
		))
	}
}

// maxNameserverDomainEntries is the maximum number of entries in the nameserverDomainStatus map
// to prevent unbounded memory growth.
const maxNameserverDomainEntries = 1000

// checkDomainAgainstNameservers tests a domain against each configured nameserver individually.
// This enables detection of intermittent DNS failures caused by specific nameserver issues.
func (m *DNSMonitor) checkDomainAgainstNameservers(ctx context.Context, status *types.Status, domain string) {
	nameservers, err := m.parseResolverConfig()
	if err != nil {
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"ResolverConfigParseError",
			fmt.Sprintf("Failed to parse resolver config for per-nameserver testing: %v", err),
		))
		return
	}

	if len(nameservers) == 0 {
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"NoNameserversConfigured",
			fmt.Sprintf("No nameservers found for per-nameserver testing of %s", domain),
		))
		return
	}

	// Collect results from each nameserver (outside of lock to avoid holding mutex during I/O)
	type nsResult struct {
		nameserver string
		err        error
		latency    time.Duration
	}
	results := make([]nsResult, 0, len(nameservers))

	for _, ns := range nameservers {
		// Create a custom resolver for this specific nameserver
		resolver := m.createNameserverResolver(ns)

		// Perform the lookup with timeout (no lock held during network I/O)
		queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		start := time.Now()
		_, lookupErr := resolver.LookupHost(queryCtx, domain)
		latency := time.Since(start)
		cancel()

		results = append(results, nsResult{
			nameserver: ns,
			err:        lookupErr,
			latency:    latency,
		})
	}

	// Now process results with lock held (only for map access)
	successCount := 0
	failedServers := []string{}
	totalServers := len(nameservers)

	m.mu.Lock()
	// Clean up old entries if map is at or above limit
	if len(m.nameserverDomainStatus) >= maxNameserverDomainEntries {
		m.cleanupOldNameserverStatus()
	}

	for _, result := range results {
		key := fmt.Sprintf("%s:%s", result.nameserver, domain)
		nsStatus := m.getOrCreateNameserverStatus(key, result.nameserver, domain)

		if result.err != nil {
			nsStatus.FailureCount++
			failedServers = append(failedServers, result.nameserver)
		} else {
			successCount++
			nsStatus.FailureCount = 0
			nsStatus.LastSuccess = time.Now()
			nsStatus.LastLatency = result.latency
		}
	}
	m.mu.Unlock()

	// Generate events outside of lock
	for _, result := range results {
		if result.err != nil {
			errType := classifyDNSError(result.err)
			status.AddEvent(types.NewEvent(
				types.EventWarning,
				"NameserverDomainResolutionFailed",
				fmt.Sprintf("[%s] Nameserver %s failed to resolve %s: %v", errType, result.nameserver, domain, result.err),
			))
		} else if result.latency > m.config.LatencyThreshold {
			status.AddEvent(types.NewEvent(
				types.EventWarning,
				"NameserverDomainLatencyHigh",
				fmt.Sprintf("Nameserver %s resolved %s in %v (threshold: %v)", result.nameserver, domain, result.latency, m.config.LatencyThreshold),
			))
		}
	}

	// Report aggregate condition based on results
	if successCount == 0 && totalServers > 0 {
		// All nameservers failed
		status.AddCondition(types.NewCondition(
			"CustomDNSDown",
			types.ConditionTrue,
			"AllNameserversFailed",
			fmt.Sprintf("Domain %s: all %d nameservers failed to resolve", domain, totalServers),
		))
	} else if successCount > 0 && len(failedServers) > 0 {
		// Partial failure - some nameservers working, some not
		status.AddCondition(types.NewCondition(
			"DNSResolutionDegraded",
			types.ConditionTrue,
			"PartialNameserverFailure",
			fmt.Sprintf("Domain %s: %d/%d nameservers responding (failed: %s)",
				domain, successCount, totalServers, strings.Join(failedServers, ", ")),
		))
	} else if successCount == totalServers {
		// All nameservers succeeded
		status.AddCondition(types.NewCondition(
			"CustomDNSHealthy",
			types.ConditionTrue,
			"AllNameserversHealthy",
			fmt.Sprintf("Domain %s: all %d nameservers responding", domain, totalServers),
		))
	}
}

// cleanupOldNameserverStatus removes stale entries from the nameserverDomainStatus map.
// Caller must hold m.mu lock.
func (m *DNSMonitor) cleanupOldNameserverStatus() {
	// Remove entries that:
	// 1. Have NEVER succeeded (IsZero), OR
	// 2. Haven't succeeded recently (before cutoff)
	cutoff := time.Now().Add(-1 * time.Hour)
	for key, status := range m.nameserverDomainStatus {
		if status.LastSuccess.IsZero() || status.LastSuccess.Before(cutoff) {
			delete(m.nameserverDomainStatus, key)
		}
	}
	// If still too large after cleanup, clear and rebuild
	if len(m.nameserverDomainStatus) > maxNameserverDomainEntries/2 {
		m.nameserverDomainStatus = make(map[string]*NameserverDomainStatus)
	}
}

// createNameserverResolver creates a resolver that uses a specific nameserver.
// Supports both IPv4 and IPv6 nameservers.
func (m *DNSMonitor) createNameserverResolver(nameserver string) *net.Resolver {
	// Use net.JoinHostPort to properly handle IPv6 addresses (e.g., [2001:4860:4860::8888]:53)
	addr := net.JoinHostPort(nameserver, "53")
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{Timeout: 2 * time.Second}
			return d.DialContext(ctx, "udp", addr)
		},
	}
}

// getOrCreateNameserverStatus retrieves or creates a NameserverDomainStatus for tracking.
// Caller must hold m.mu lock.
func (m *DNSMonitor) getOrCreateNameserverStatus(key, nameserver, domain string) *NameserverDomainStatus {
	if status, exists := m.nameserverDomainStatus[key]; exists {
		return status
	}
	status := &NameserverDomainStatus{
		Nameserver: nameserver,
		Domain:     domain,
	}
	m.nameserverDomainStatus[key] = status
	return status
}

// checkNameservers verifies nameserver reachability and records results for health scoring.
func (m *DNSMonitor) checkNameservers(ctx context.Context, status *types.Status) {
	nameservers, err := m.parseResolverConfig()
	if err != nil {
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"ResolverConfigParseError",
			fmt.Sprintf("Failed to parse resolver config: %v", err),
		))
		return
	}

	if len(nameservers) == 0 {
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"NoNameserversConfigured",
			"No nameservers found in resolver configuration",
		))
		return
	}

	// For each nameserver, try a simple DNS query to verify reachability
	for _, ns := range nameservers {
		// We'll use google.com as a test domain (widely available and reliable)
		testDomain := "google.com"

		// Reuse createNameserverResolver for proper IPv4/IPv6 handling
		resolver := m.createNameserverResolver(ns)

		// Try to resolve using this nameserver
		queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		start := time.Now()
		_, lookupErr := resolver.LookupHost(queryCtx, testDomain)
		latency := time.Since(start)
		cancel()

		if lookupErr != nil {
			if m.config.NameserverCheckEnabled {
				status.AddEvent(types.NewEvent(
					types.EventWarning,
					"NameserverUnreachable",
					fmt.Sprintf("Nameserver %s is unreachable: %v", ns, lookupErr),
				))
			}
		}

		// Feed result into health scoring stats (if enabled)
		if m.healthScoringEnabled() {
			m.recordNameserverCheck(ns, lookupErr == nil, latency, classifyDNSError(lookupErr))
		}
	}
}

// recordNameserverCheck records a single check result for a nameserver into its stats window.
// This is called under the main mutex-free path; NameserverStats has its own lock.
func (m *DNSMonitor) recordNameserverCheck(nameserver string, success bool, latency time.Duration, errType DNSErrorType) {
	m.mu.Lock()
	stats, ok := m.nameserverStats[nameserver]
	if !ok {
		windowSize := 20
		if m.config.HealthScoring != nil && m.config.HealthScoring.WindowSize > 0 {
			windowSize = m.config.HealthScoring.WindowSize
		}
		stats = newNameserverStats(windowSize)
		m.nameserverStats[nameserver] = stats
	}
	m.mu.Unlock()

	stats.add(&NameserverCheckResult{
		Timestamp: time.Now(),
		Success:   success,
		Latency:   latency,
		ErrType:   errType,
	})
}

// runCorrelationAnalysis runs the correlation engine against the current in-memory
// snapshots and adds status conditions for any patterns that meet the confidence threshold.
// Caller must hold m.mu.
func (m *DNSMonitor) runCorrelationAnalysis(status *types.Status) {
	results := m.correlationEngine.Analyze(m.nameserverStats, m.nameserverDomainStatus)
	if len(results) == 0 {
		return
	}

	// Group results by type and emit one condition per type with a combined message.
	type group struct {
		causes   []string
		evidence []string
	}
	groups := make(map[string]*group)
	for _, r := range results {
		g := groups[r.Type]
		if g == nil {
			g = &group{}
			groups[r.Type] = g
		}
		g.causes = append(g.causes, r.RootCause)
		g.evidence = append(g.evidence, r.Evidence...)
	}

	conditionType := map[string]string{
		"nameserver":     "DNSNameserverCorrelation",
		"domain_pattern": "DNSDomainPatternCorrelation",
		"temporal":       "DNSTemporalCorrelation",
	}
	for typ, g := range groups {
		cType, ok := conditionType[typ]
		if !ok {
			cType = "DNSCorrelation"
		}
		msg := strings.Join(g.causes, "; ")
		status.AddCondition(types.NewCondition(
			cType,
			types.ConditionTrue,
			"CorrelatedDNSFailure",
			msg,
		))
	}
}

// computeNameserverHealthScores computes and returns health scores for all tracked nameservers.
// It also adds conditions to status for any degraded or unhealthy nameservers.
// Caller must hold m.mu.
func (m *DNSMonitor) computeNameserverHealthScores(status *types.Status) []types.NameserverHealthScore {
	if len(m.nameserverStats) == 0 {
		return nil
	}

	cfg := m.config.HealthScoring
	scores := make([]types.NameserverHealthScore, 0, len(m.nameserverStats))
	var unhealthyList, degradedList []string

	for ns, stats := range m.nameserverStats {
		score := computeHealthScore(stats, cfg, ns)
		scores = append(scores, score)

		switch score.Status {
		case "unhealthy":
			unhealthyList = append(unhealthyList, fmt.Sprintf("%s(%.0f)", ns, score.Score))
		case "degraded":
			degradedList = append(degradedList, fmt.Sprintf("%s(%.0f)", ns, score.Score))
		}
	}

	if len(unhealthyList) > 0 {
		status.AddCondition(types.NewCondition(
			"DNSNameserverUnhealthy",
			types.ConditionTrue,
			"LowHealthScore",
			fmt.Sprintf("Unhealthy nameservers (score < %.0f): %s",
				cfg.UnhealthyThreshold, strings.Join(unhealthyList, ", ")),
		))
	}

	if len(degradedList) > 0 {
		status.AddCondition(types.NewCondition(
			"DNSNameserverDegraded",
			types.ConditionTrue,
			"DegradedHealthScore",
			fmt.Sprintf("Degraded nameservers (score < %.0f): %s",
				cfg.DegradedThreshold, strings.Join(degradedList, ", ")),
		))
	}

	return scores
}

// computePredictiveAlerts runs linear-regression analysis on the cluster and external
// success-rate ring buffers, adds DNSPredictedDegradation conditions to status when a
// breach is predicted within WarningLeadTime, and appends the results to latencyMetrics
// for Prometheus export.
// Caller must hold m.mu.
func (m *DNSMonitor) computePredictiveAlerts(status *types.Status, latencyMetrics *types.LatencyMetrics) {
	cfg := m.config.PredictiveAlerting
	threshold := m.config.SuccessRateTracking.FailureRateThreshold

	clusterResult := analyzeRingBuffer(m.clusterSuccessTracker, threshold, cfg)
	externalResult := analyzeRingBuffer(m.externalSuccessTracker, threshold, cfg)

	// Add DNSPredictedDegradation conditions for breaches within lead time.
	if clusterResult.WillBreach && clusterResult.WithinLeadTime {
		status.AddCondition(types.NewCondition(
			"DNSPredictedDegradation",
			types.ConditionTrue,
			"ClusterDNSBreachPredicted",
			buildPredictiveConditionMessage("Cluster", clusterResult),
		))
	}
	if externalResult.WillBreach && externalResult.WithinLeadTime {
		status.AddCondition(types.NewCondition(
			"DNSPredictedDegradation",
			types.ConditionTrue,
			"ExternalDNSBreachPredicted",
			buildPredictiveConditionMessage("External", externalResult),
		))
	}

	// Always append predictive metrics when a prediction was computed, so Prometheus
	// can track confidence and time-to-breach even without an active alert.
	if clusterResult.HasPrediction {
		latencyMetrics.DNSPredictiveAlerts = append(latencyMetrics.DNSPredictiveAlerts,
			toDNSPredictiveAlert("cluster", clusterResult))
	}
	if externalResult.HasPrediction {
		latencyMetrics.DNSPredictiveAlerts = append(latencyMetrics.DNSPredictiveAlerts,
			toDNSPredictiveAlert("external", externalResult))
	}
}

// parseResolverConfig parses /etc/resolv.conf to extract nameservers.
func (m *DNSMonitor) parseResolverConfig() ([]string, error) {
	file, err := os.Open(m.config.ResolverPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open resolver config: %w", err)
	}
	defer file.Close()

	var nameservers []string
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Look for nameserver lines
		if strings.HasPrefix(line, "nameserver") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				nameservers = append(nameservers, fields[1])
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading resolver config: %w", err)
	}

	return nameservers, nil
}

// updateFailureTracking updates failure counters and sets conditions based on failure state.
func (m *DNSMonitor) updateFailureTracking(clusterOK, externalOK bool, status *types.Status) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Update cluster failure count
	if !clusterOK {
		m.clusterFailureCount++
	} else {
		m.clusterFailureCount = 0
	}

	// Update external failure count
	if !externalOK {
		m.externalFailureCount++
	} else {
		m.externalFailureCount = 0
	}

	// Track success rate in sliding window
	m.clusterSuccessTracker.Add(&CheckResult{
		Timestamp: time.Now(),
		Success:   clusterOK,
	})
	m.externalSuccessTracker.Add(&CheckResult{
		Timestamp: time.Now(),
		Success:   externalOK,
	})

	// Report ClusterDNSDown condition if cluster DNS failures exceed threshold
	if m.clusterFailureCount >= m.config.FailureCountThreshold {
		status.AddCondition(types.NewCondition(
			"ClusterDNSDown",
			types.ConditionTrue,
			"RepeatedClusterDNSFailures",
			fmt.Sprintf("Cluster DNS has failed %d consecutive times (threshold: %d)",
				m.clusterFailureCount, m.config.FailureCountThreshold),
		))
	} else if clusterOK && m.clusterFailureCount == 0 {
		// Report healthy condition when back to normal
		status.AddCondition(types.NewCondition(
			"ClusterDNSHealthy",
			types.ConditionTrue,
			"ClusterDNSResolved",
			"Cluster DNS resolution is healthy",
		))
	}

	// Report NetworkUnreachable condition if external DNS failures exceed threshold
	if m.externalFailureCount >= m.config.FailureCountThreshold {
		status.AddCondition(types.NewCondition(
			"NetworkUnreachable",
			types.ConditionTrue,
			"RepeatedExternalDNSFailures",
			fmt.Sprintf("External DNS has failed %d consecutive times (threshold: %d)",
				m.externalFailureCount, m.config.FailureCountThreshold),
		))
	} else if externalOK && m.externalFailureCount == 0 {
		// Report healthy condition when back to normal
		status.AddCondition(types.NewCondition(
			"NetworkReachable",
			types.ConditionTrue,
			"ExternalDNSResolved",
			"External DNS resolution is healthy",
		))
	}

	// Check success rate thresholds if enabled
	if m.config.SuccessRateTracking.Enabled {
		m.checkSuccessRateConditions(status)
	}
}

// checkSuccessRateConditions evaluates success rates and adds conditions when thresholds are exceeded.
// Caller must hold m.mu lock.
func (m *DNSMonitor) checkSuccessRateConditions(status *types.Status) {
	srtConfig := m.config.SuccessRateTracking

	// Check cluster DNS success rate
	if m.clusterSuccessTracker.Count() >= srtConfig.MinSamplesRequired {
		clusterFailureRate := m.clusterSuccessTracker.GetFailureRate()

		if clusterFailureRate >= srtConfig.FailureRateThreshold {
			status.AddCondition(types.NewCondition(
				"ClusterDNSDegraded",
				types.ConditionTrue,
				"HighClusterDNSFailureRate",
				fmt.Sprintf("Cluster DNS failure rate %.1f%% exceeds threshold %.1f%% (window: %d samples)",
					clusterFailureRate*100, srtConfig.FailureRateThreshold*100, m.clusterSuccessTracker.Count()),
			))
		} else if clusterFailureRate > 0 {
			// Some failures but below threshold - intermittent
			status.AddCondition(types.NewCondition(
				"ClusterDNSIntermittent",
				types.ConditionTrue,
				"IntermittentClusterDNSFailures",
				fmt.Sprintf("Cluster DNS has intermittent failures: %.1f%% failure rate (threshold: %.1f%%)",
					clusterFailureRate*100, srtConfig.FailureRateThreshold*100),
			))
		}
	}

	// Check external DNS success rate
	if m.externalSuccessTracker.Count() >= srtConfig.MinSamplesRequired {
		externalFailureRate := m.externalSuccessTracker.GetFailureRate()

		if externalFailureRate >= srtConfig.FailureRateThreshold {
			status.AddCondition(types.NewCondition(
				"ExternalDNSDegraded",
				types.ConditionTrue,
				"HighExternalDNSFailureRate",
				fmt.Sprintf("External DNS failure rate %.1f%% exceeds threshold %.1f%% (window: %d samples)",
					externalFailureRate*100, srtConfig.FailureRateThreshold*100, m.externalSuccessTracker.Count()),
			))
		} else if externalFailureRate > 0 {
			// Some failures but below threshold - intermittent
			status.AddCondition(types.NewCondition(
				"ExternalDNSIntermittent",
				types.ConditionTrue,
				"IntermittentExternalDNSFailures",
				fmt.Sprintf("External DNS has intermittent failures: %.1f%% failure rate (threshold: %.1f%%)",
					externalFailureRate*100, srtConfig.FailureRateThreshold*100),
			))
		}
	}
}
