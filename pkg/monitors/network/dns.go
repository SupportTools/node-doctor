// Package network provides network-related health monitoring capabilities.
// It includes DNS resolution monitoring for cluster and external domains,
// latency measurement, and nameserver verification.
package network

import (
	"bufio"
	"context"
	"fmt"
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
}

// NameserverDomainStatus tracks the status of DNS resolution for a specific
// nameserver and domain combination.
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
		BaseMonitor:            baseMonitor,
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

	// Validate that at least one type of DNS check is configured
	if len(dnsConfig.ClusterDomains) == 0 && len(dnsConfig.ExternalDomains) == 0 && len(dnsConfig.CustomQueries) == 0 {
		return fmt.Errorf("at least one of clusterDomains, externalDomains, or customQueries must be configured")
	}

	return nil
}

// parseDNSConfig parses the DNS monitor configuration from a map.
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

			config.CustomQueries[i] = DNSQuery{
				Domain:             domain,
				RecordType:         recordType,
				TestEachNameserver: testEachNameserver,
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

	return nil
}

// checkDNS performs the DNS health check.
func (m *DNSMonitor) checkDNS(ctx context.Context) (*types.Status, error) {
	status := types.NewStatus(m.name)

	// Check cluster DNS
	clusterOK := m.checkClusterDNS(ctx, status)

	// Check external DNS
	externalOK := m.checkExternalDNS(ctx, status)

	// Check custom queries
	m.checkCustomQueries(ctx, status)

	// Check nameservers
	if m.config.NameserverCheckEnabled {
		m.checkNameservers(ctx, status)
	}

	// Update failure counters and conditions
	m.updateFailureTracking(clusterOK, externalOK, status)

	return status, nil
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
			status.AddEvent(types.NewEvent(
				types.EventError,
				domainType+"DNSResolutionFailed",
				fmt.Sprintf("Failed to resolve %s domain %s: %v", strings.ToLower(domainType), domain, err),
			))
			continue
		}

		if len(addrs) == 0 {
			allSuccess = false
			status.AddEvent(types.NewEvent(
				types.EventWarning,
				domainType+"DNSNoRecords",
				fmt.Sprintf("No A records found for %s domain %s", strings.ToLower(domainType), domain),
			))
			continue
		}

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

		// Standard resolution using system resolver
		start := time.Now()
		addrs, err := m.resolver.LookupHost(ctx, query.Domain)
		latency := time.Since(start)

		if err != nil {
			status.AddEvent(types.NewEvent(
				types.EventError,
				"CustomDNSQueryFailed",
				fmt.Sprintf("Failed to resolve custom domain %s: %v", query.Domain, err),
			))
			continue
		}

		if len(addrs) == 0 {
			status.AddEvent(types.NewEvent(
				types.EventWarning,
				"CustomDNSNoRecords",
				fmt.Sprintf("No A records found for custom domain %s", query.Domain),
			))
			continue
		}

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
			status.AddEvent(types.NewEvent(
				types.EventWarning,
				"NameserverDomainResolutionFailed",
				fmt.Sprintf("Nameserver %s failed to resolve %s: %v", result.nameserver, domain, result.err),
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

// checkNameservers verifies nameserver reachability.
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
		_, err := resolver.LookupHost(queryCtx, testDomain)
		cancel()

		if err != nil {
			status.AddEvent(types.NewEvent(
				types.EventWarning,
				"NameserverUnreachable",
				fmt.Sprintf("Nameserver %s is unreachable: %v", ns, err),
			))
		}
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
