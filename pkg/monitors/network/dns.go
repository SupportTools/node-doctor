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
	Domain     string
	RecordType string // Currently only "A" is supported
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
}

// DNSMonitor monitors DNS resolution health.
type DNSMonitor struct {
	name     string
	config   *DNSMonitorConfig
	resolver Resolver

	// Failure tracking for NetworkUnreachable condition
	mu                     sync.Mutex
	clusterFailureCount    int
	externalFailureCount   int
	nameserverFailureCount int

	// BaseMonitor for lifecycle management
	*monitors.BaseMonitor
}

// init registers the DNS monitor with the monitor registry.
func init() {
	monitors.Register(monitors.MonitorInfo{
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
		name:        config.Name,
		config:      dnsConfig,
		resolver:    newDefaultResolver(),
		BaseMonitor: baseMonitor,
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

			config.CustomQueries[i] = DNSQuery{
				Domain:     domain,
				RecordType: recordType,
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
	allSuccess := true

	for _, domain := range m.config.ClusterDomains {
		start := time.Now()
		addrs, err := m.resolver.LookupHost(ctx, domain)
		latency := time.Since(start)

		if err != nil {
			allSuccess = false
			status.AddEvent(types.NewEvent(
				types.EventError,
				"ClusterDNSResolutionFailed",
				fmt.Sprintf("Failed to resolve cluster domain %s: %v", domain, err),
			))
			continue
		}

		if len(addrs) == 0 {
			allSuccess = false
			status.AddEvent(types.NewEvent(
				types.EventWarning,
				"ClusterDNSNoRecords",
				fmt.Sprintf("No A records found for cluster domain %s", domain),
			))
			continue
		}

		// Check latency
		if latency > m.config.LatencyThreshold {
			status.AddEvent(types.NewEvent(
				types.EventWarning,
				"HighClusterDNSLatency",
				fmt.Sprintf("Cluster DNS resolution for %s took %v (threshold: %v)", domain, latency, m.config.LatencyThreshold),
			))
		}
	}

	return allSuccess
}

// checkExternalDNS checks external DNS resolution.
func (m *DNSMonitor) checkExternalDNS(ctx context.Context, status *types.Status) bool {
	allSuccess := true

	for _, domain := range m.config.ExternalDomains {
		start := time.Now()
		addrs, err := m.resolver.LookupHost(ctx, domain)
		latency := time.Since(start)

		if err != nil {
			allSuccess = false
			status.AddEvent(types.NewEvent(
				types.EventError,
				"ExternalDNSResolutionFailed",
				fmt.Sprintf("Failed to resolve external domain %s: %v", domain, err),
			))
			continue
		}

		if len(addrs) == 0 {
			allSuccess = false
			status.AddEvent(types.NewEvent(
				types.EventWarning,
				"ExternalDNSNoRecords",
				fmt.Sprintf("No A records found for external domain %s", domain),
			))
			continue
		}

		// Check latency
		if latency > m.config.LatencyThreshold {
			status.AddEvent(types.NewEvent(
				types.EventWarning,
				"HighExternalDNSLatency",
				fmt.Sprintf("External DNS resolution for %s took %v (threshold: %v)", domain, latency, m.config.LatencyThreshold),
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

		// Create a custom resolver for this specific nameserver
		// Note: This is simplified - in production you might want to use a more robust approach
		resolver := &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{Timeout: 2 * time.Second}
				return d.DialContext(ctx, "udp", ns+":53")
			},
		}

		// Try to resolve using this nameserver
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_, err := resolver.LookupHost(ctx, testDomain)
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
}
