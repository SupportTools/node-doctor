// Package network provides network health monitoring capabilities.
package network

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/supporttools/node-doctor/pkg/monitors"
	"github.com/supporttools/node-doctor/pkg/types"
)

const (
	// procNetRoute is the path to the Linux routing table
	procNetRoute = "/proc/net/route"

	// Default configuration values
	defaultPingCount             = 3
	defaultPingTimeout           = 1 * time.Second
	defaultLatencyThreshold      = 100 * time.Millisecond
	defaultFailureCountThreshold = 3
	defaultAutoDetectGateway     = true
)

// GatewayMonitorConfig holds the configuration for the gateway monitor.
type GatewayMonitorConfig struct {
	// PingCount is the number of pings to send per check cycle.
	PingCount int
	// PingTimeout is the timeout for each individual ping.
	PingTimeout time.Duration
	// LatencyThreshold is the threshold above which latency is considered high.
	LatencyThreshold time.Duration
	// AutoDetectGateway enables automatic gateway detection from route table.
	AutoDetectGateway bool
	// ManualGateway allows manual specification of gateway IP (overrides auto-detection).
	ManualGateway string
	// FailureCountThreshold is the number of consecutive failures before reporting NetworkUnreachable.
	FailureCountThreshold int
}

// GatewayMonitor monitors the default gateway's reachability and latency.
type GatewayMonitor struct {
	name         string
	config       *GatewayMonitorConfig
	pinger       Pinger
	mu           sync.Mutex
	failureCount int
	lastGateway  string // Track gateway IP for change detection

	*monitors.BaseMonitor
}

// init registers the gateway monitor with the monitor registry.
func init() {
	monitors.Register(monitors.MonitorInfo{
		Type:        "network-gateway-check",
		Factory:     NewGatewayMonitor,
		Validator:   ValidateGatewayConfig,
		Description: "Monitors default gateway reachability and latency using ICMP ping",
	})
}

// NewGatewayMonitor creates a new gateway monitor instance.
func NewGatewayMonitor(ctx context.Context, config types.MonitorConfig) (types.Monitor, error) {
	// Parse gateway-specific configuration
	gatewayConfig, err := parseGatewayConfig(config.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse gateway config: %w", err)
	}

	// Create base monitor for lifecycle management
	baseMonitor, err := monitors.NewBaseMonitor(config.Name, config.Interval, config.Timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to create base monitor: %w", err)
	}

	// Create gateway monitor
	monitor := &GatewayMonitor{
		name:        config.Name,
		config:      gatewayConfig,
		pinger:      newDefaultPinger(),
		BaseMonitor: baseMonitor,
	}

	// Set the check function
	if err := baseMonitor.SetCheckFunc(monitor.checkGateway); err != nil {
		return nil, fmt.Errorf("failed to set check function: %w", err)
	}

	return monitor, nil
}

// parseGatewayConfig parses the gateway monitor configuration from a map.
func parseGatewayConfig(configMap map[string]interface{}) (*GatewayMonitorConfig, error) {
	config := &GatewayMonitorConfig{
		PingCount:             defaultPingCount,
		PingTimeout:           defaultPingTimeout,
		LatencyThreshold:      defaultLatencyThreshold,
		AutoDetectGateway:     defaultAutoDetectGateway,
		ManualGateway:         "",
		FailureCountThreshold: defaultFailureCountThreshold,
	}

	if configMap == nil {
		return config, nil
	}

	// Parse ping count
	if v, ok := configMap["pingCount"]; ok {
		switch val := v.(type) {
		case int:
			config.PingCount = val
		case float64:
			config.PingCount = int(val)
		default:
			return nil, fmt.Errorf("pingCount must be an integer, got %T", v)
		}
		if config.PingCount < 1 {
			return nil, fmt.Errorf("pingCount must be at least 1, got %d", config.PingCount)
		}
	}

	// Parse ping timeout
	if v, ok := configMap["pingTimeout"]; ok {
		timeout, err := parseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid pingTimeout: %w", err)
		}
		config.PingTimeout = timeout
	}

	// Parse latency threshold
	if v, ok := configMap["latencyThreshold"]; ok {
		threshold, err := parseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid latencyThreshold: %w", err)
		}
		config.LatencyThreshold = threshold
	}

	// Parse auto-detect gateway
	if v, ok := configMap["autoDetectGateway"]; ok {
		boolVal, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("autoDetectGateway must be a boolean, got %T", v)
		}
		config.AutoDetectGateway = boolVal
	}

	// Parse manual gateway
	if v, ok := configMap["manualGateway"]; ok {
		strVal, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("manualGateway must be a string, got %T", v)
		}
		config.ManualGateway = strVal
		// Validate IP format if provided
		if strVal != "" && net.ParseIP(strVal) == nil {
			return nil, fmt.Errorf("manualGateway must be a valid IP address, got %s", strVal)
		}
	}

	// Parse failure count threshold
	if v, ok := configMap["failureCountThreshold"]; ok {
		switch val := v.(type) {
		case int:
			config.FailureCountThreshold = val
		case float64:
			config.FailureCountThreshold = int(val)
		default:
			return nil, fmt.Errorf("failureCountThreshold must be an integer, got %T", v)
		}
		if config.FailureCountThreshold < 1 {
			return nil, fmt.Errorf("failureCountThreshold must be at least 1, got %d", config.FailureCountThreshold)
		}
	}

	return config, nil
}

// parseDuration parses a duration from various input types.
func parseDuration(v interface{}) (time.Duration, error) {
	switch val := v.(type) {
	case string:
		return time.ParseDuration(val)
	case int:
		return time.Duration(val) * time.Second, nil
	case float64:
		return time.Duration(val * float64(time.Second)), nil
	default:
		return 0, fmt.Errorf("invalid duration type: %T", v)
	}
}

// ValidateGatewayConfig validates the gateway monitor configuration.
func ValidateGatewayConfig(config types.MonitorConfig) error {
	_, err := parseGatewayConfig(config.Config)
	return err
}

// checkGateway performs a gateway health check.
func (m *GatewayMonitor) checkGateway(ctx context.Context) (*types.Status, error) {
	status := types.NewStatus(m.name)

	// Determine gateway IP
	gatewayIP, err := m.getGatewayIP()
	if err != nil {
		m.updateFailureTracking(false, status)
		status.AddEvent(types.NewEvent(
			types.EventError,
			"GatewayDetectionFailed",
			fmt.Sprintf("Failed to detect gateway: %v", err),
		))
		return status, nil
	}

	// Track gateway changes
	m.trackGatewayChange(gatewayIP)

	// Ping the gateway
	results, err := m.pinger.Ping(ctx, gatewayIP, m.config.PingCount, m.config.PingTimeout)
	if err != nil {
		m.updateFailureTracking(false, status)
		status.AddEvent(types.NewEvent(
			types.EventError,
			"GatewayPingError",
			fmt.Sprintf("Failed to ping gateway %s: %v", gatewayIP, err),
		))
		return status, nil
	}

	// Analyze ping results
	successCount := 0
	var totalRTT time.Duration
	var maxRTT time.Duration

	for _, result := range results {
		if result.Success {
			successCount++
			totalRTT += result.RTT
			if result.RTT > maxRTT {
				maxRTT = result.RTT
			}
		}
	}

	// Determine overall success (majority of pings must succeed)
	pingSucceeded := successCount > len(results)/2

	if !pingSucceeded {
		m.updateFailureTracking(false, status)
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"GatewayUnreachable",
			fmt.Sprintf("Gateway %s is unreachable (%d/%d pings failed)", gatewayIP, len(results)-successCount, len(results)),
		))
		return status, nil
	}

	// Success - reset failure counter
	m.updateFailureTracking(true, status)

	// Calculate average latency
	avgLatency := totalRTT / time.Duration(successCount)

	// Check for high latency
	if avgLatency > m.config.LatencyThreshold {
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"HighGatewayLatency",
			fmt.Sprintf("Gateway %s latency is high: avg=%v, max=%v (threshold=%v)",
				gatewayIP, avgLatency, maxRTT, m.config.LatencyThreshold),
		))
	}

	// Add informational event with metrics
	status.AddEvent(types.NewEvent(
		types.EventInfo,
		"GatewayHealthy",
		fmt.Sprintf("Gateway %s is reachable: %d/%d pings successful, avg latency=%v",
			gatewayIP, successCount, len(results), avgLatency),
	))

	return status, nil
}

// getGatewayIP determines the gateway IP to ping.
func (m *GatewayMonitor) getGatewayIP() (string, error) {
	// Use manual gateway if configured
	if m.config.ManualGateway != "" {
		return m.config.ManualGateway, nil
	}

	// Auto-detect gateway if enabled
	if m.config.AutoDetectGateway {
		return detectDefaultGateway()
	}

	return "", fmt.Errorf("no gateway configured and auto-detection is disabled")
}

// detectDefaultGateway detects the default gateway from /proc/net/route.
func detectDefaultGateway() (string, error) {
	file, err := os.Open(procNetRoute)
	if err != nil {
		return "", fmt.Errorf("failed to open %s: %w", procNetRoute, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	// Skip header line
	if !scanner.Scan() {
		return "", fmt.Errorf("route table is empty")
	}

	// Parse route entries
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)

		// Route table format: Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT
		// We need at least 8 fields
		if len(fields) < 8 {
			continue
		}

		destination := fields[1]
		gateway := fields[2]

		// Default route has destination 00000000
		if destination == "00000000" && gateway != "00000000" {
			// Parse gateway hex string to IP
			gatewayIP, err := hexToIP(gateway)
			if err != nil {
				return "", fmt.Errorf("failed to parse gateway hex %s: %w", gateway, err)
			}
			return gatewayIP, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("error reading route table: %w", err)
	}

	return "", fmt.Errorf("no default gateway found in route table")
}

// hexToIP converts a hex string (little-endian) to an IP address string.
// Example: "0100007F" -> "127.0.0.1"
func hexToIP(hexStr string) (string, error) {
	if len(hexStr) != 8 {
		return "", fmt.Errorf("invalid hex IP length: %d (expected 8)", len(hexStr))
	}

	// Parse hex string to uint32 (little-endian)
	val, err := strconv.ParseUint(hexStr, 16, 32)
	if err != nil {
		return "", fmt.Errorf("failed to parse hex: %w", err)
	}

	// Convert to IP address (little-endian byte order)
	ip := net.IPv4(
		byte(val&0xFF),
		byte((val>>8)&0xFF),
		byte((val>>16)&0xFF),
		byte((val>>24)&0xFF),
	)

	return ip.String(), nil
}

// trackGatewayChange logs when the gateway IP changes.
func (m *GatewayMonitor) trackGatewayChange(newGateway string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Note: Gateway changes are tracked but not currently logged
	// The state change is preserved for potential future metrics/events
	m.lastGateway = newGateway
}

// updateFailureTracking updates the failure counter and adds NetworkUnreachable condition if needed.
func (m *GatewayMonitor) updateFailureTracking(success bool, status *types.Status) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !success {
		m.failureCount++
	} else {
		// Success - clear failure counter
		if m.failureCount > 0 {
			// Add recovery event
			status.AddEvent(types.NewEvent(
				types.EventInfo,
				"GatewayRecovered",
				fmt.Sprintf("Gateway connectivity restored after %d consecutive failures", m.failureCount),
			))
		}
		m.failureCount = 0
	}

	// Report or clear NetworkUnreachable condition based on failure count
	if m.failureCount >= m.config.FailureCountThreshold {
		// Report NetworkUnreachable condition
		status.AddCondition(types.NewCondition(
			"NetworkUnreachable",
			types.ConditionTrue,
			"GatewayUnreachable",
			fmt.Sprintf("Default gateway has been unreachable for %d consecutive checks", m.failureCount),
		))
	} else if m.failureCount == 0 {
		// Clear NetworkUnreachable condition when recovered
		status.AddCondition(types.NewCondition(
			"NetworkUnreachable",
			types.ConditionFalse,
			"GatewayReachable",
			"Default gateway is reachable",
		))
	}
}
