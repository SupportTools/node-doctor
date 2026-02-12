// Package network provides network health monitoring capabilities.
package network

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/supporttools/node-doctor/pkg/monitors"
	"github.com/supporttools/node-doctor/pkg/types"
)

const (
	// Default configuration values for IP forwarding monitor
	defaultCheckIPv4         = true
	defaultCheckIPv6         = true
	defaultCheckPerInterface = false
	defaultProcPath          = "/proc"

	// Proc filesystem paths (relative to ProcPath)
	ipv4ForwardPath     = "sys/net/ipv4/ip_forward"
	ipv6ForwardPath     = "sys/net/ipv6/conf/all/forwarding"
	perInterfacePattern = "sys/net/ipv4/conf/*/forwarding"
)

// IPForwardingConfig holds the configuration for the IP forwarding monitor.
type IPForwardingConfig struct {
	// CheckIPv4 enables checking /proc/sys/net/ipv4/ip_forward.
	CheckIPv4 bool
	// CheckIPv6 enables checking /proc/sys/net/ipv6/conf/all/forwarding.
	CheckIPv6 bool
	// CheckPerInterface enables checking per-interface forwarding settings.
	CheckPerInterface bool
	// Interfaces limits per-interface checks to these specific interfaces.
	// Empty means check all interfaces found via glob.
	Interfaces []string
	// ProcPath is the base path for the proc filesystem.
	// Defaults to "/proc", but can be set to "/host/proc" for containerized deployments.
	ProcPath string
}

// IPForwardingMonitor monitors IP forwarding settings required for Kubernetes networking.
type IPForwardingMonitor struct {
	name   string
	config *IPForwardingConfig

	*monitors.BaseMonitor
}

// init registers the IP forwarding monitor with the monitor registry.
func init() {
	monitors.MustRegister(monitors.MonitorInfo{
		Type:        "network-ip-forwarding",
		Factory:     NewIPForwardingMonitor,
		Validator:   ValidateIPForwardingConfig,
		Description: "Monitors IP forwarding settings required for Kubernetes networking",
		DefaultConfig: &types.MonitorConfig{
			Name:           "ip-forwarding-check",
			Type:           "network-ip-forwarding",
			Enabled:        true,
			IntervalString: "30s",
			TimeoutString:  "5s",
			Config: map[string]any{
				"checkIPv4":         true,
				"checkIPv6":         true,
				"checkPerInterface": false,
				"procPath":          "/proc",
			},
		},
	})
}

// NewIPForwardingMonitor creates a new IP forwarding monitor instance.
func NewIPForwardingMonitor(ctx context.Context, config types.MonitorConfig) (types.Monitor, error) {
	ipfConfig, err := parseIPForwardingConfig(config.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ip forwarding config: %w", err)
	}

	baseMonitor, err := monitors.NewBaseMonitor(config.Name, config.Interval, config.Timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to create base monitor: %w", err)
	}

	monitor := &IPForwardingMonitor{
		name:        config.Name,
		config:      ipfConfig,
		BaseMonitor: baseMonitor,
	}

	if err := baseMonitor.SetCheckFunc(monitor.checkIPForwarding); err != nil {
		return nil, fmt.Errorf("failed to set check function: %w", err)
	}

	return monitor, nil
}

// parseIPForwardingConfig parses the IP forwarding monitor configuration from a map.
func parseIPForwardingConfig(configMap map[string]any) (*IPForwardingConfig, error) {
	config := &IPForwardingConfig{
		CheckIPv4:         defaultCheckIPv4,
		CheckIPv6:         defaultCheckIPv6,
		CheckPerInterface: defaultCheckPerInterface,
		ProcPath:          defaultProcPath,
	}

	if configMap == nil {
		return config, nil
	}

	if v, ok := configMap["checkIPv4"]; ok {
		boolVal, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("checkIPv4 must be a boolean, got %T", v)
		}
		config.CheckIPv4 = boolVal
	}

	if v, ok := configMap["checkIPv6"]; ok {
		boolVal, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("checkIPv6 must be a boolean, got %T", v)
		}
		config.CheckIPv6 = boolVal
	}

	if v, ok := configMap["checkPerInterface"]; ok {
		boolVal, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("checkPerInterface must be a boolean, got %T", v)
		}
		config.CheckPerInterface = boolVal
	}

	if v, ok := configMap["interfaces"]; ok {
		switch val := v.(type) {
		case []any:
			for _, item := range val {
				strVal, ok := item.(string)
				if !ok {
					return nil, fmt.Errorf("interfaces must be a list of strings, got %T element", item)
				}
				config.Interfaces = append(config.Interfaces, strVal)
			}
		case []string:
			config.Interfaces = val
		default:
			return nil, fmt.Errorf("interfaces must be a list of strings, got %T", v)
		}
	}

	if v, ok := configMap["procPath"]; ok {
		strVal, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("procPath must be a string, got %T", v)
		}
		config.ProcPath = strVal
	}

	return config, nil
}

// ValidateIPForwardingConfig validates the IP forwarding monitor configuration.
func ValidateIPForwardingConfig(config types.MonitorConfig) error {
	_, err := parseIPForwardingConfig(config.Config)
	return err
}

// checkIPForwarding performs the IP forwarding health check.
func (m *IPForwardingMonitor) checkIPForwarding(ctx context.Context) (*types.Status, error) {
	status := types.NewStatus(m.name)

	var disabledSettings []string

	// Check IPv4 forwarding
	if m.config.CheckIPv4 {
		ipv4Path := filepath.Join(m.config.ProcPath, ipv4ForwardPath)
		enabled, err := readForwardingSetting(ipv4Path)
		if err != nil {
			status.AddEvent(types.NewEvent(
				types.EventError,
				"IPForwardingReadError",
				fmt.Sprintf("Failed to read IPv4 forwarding setting from %s: %v", ipv4Path, err),
			))
			disabledSettings = append(disabledSettings, "ipv4.ip_forward (unreadable)")
		} else if !enabled {
			disabledSettings = append(disabledSettings, "net.ipv4.ip_forward=0")
			status.AddEvent(types.NewEvent(
				types.EventError,
				"IPv4ForwardingDisabled",
				fmt.Sprintf("IPv4 forwarding is disabled (net.ipv4.ip_forward=0). "+
					"Kubernetes pod networking will not function. "+
					"Remediate with: sysctl -w net.ipv4.ip_forward=1"),
			))
		}
	}

	// Check IPv6 forwarding
	if m.config.CheckIPv6 {
		ipv6Path := filepath.Join(m.config.ProcPath, ipv6ForwardPath)
		enabled, err := readForwardingSetting(ipv6Path)
		if err != nil {
			// IPv6 may not be available on all systems, treat as warning
			status.AddEvent(types.NewEvent(
				types.EventWarning,
				"IPForwardingReadError",
				fmt.Sprintf("Failed to read IPv6 forwarding setting from %s: %v", ipv6Path, err),
			))
		} else if !enabled {
			disabledSettings = append(disabledSettings, "net.ipv6.conf.all.forwarding=0")
			status.AddEvent(types.NewEvent(
				types.EventWarning,
				"IPv6ForwardingDisabled",
				fmt.Sprintf("IPv6 forwarding is disabled (net.ipv6.conf.all.forwarding=0). "+
					"IPv6 pod networking may not function. "+
					"Remediate with: sysctl -w net.ipv6.conf.all.forwarding=1"),
			))
		}
	}

	// Check per-interface forwarding
	if m.config.CheckPerInterface {
		ifaceDisabled := m.checkPerInterfaceForwarding(status)
		disabledSettings = append(disabledSettings, ifaceDisabled...)
	}

	// Set condition based on results
	if len(disabledSettings) > 0 {
		status.AddCondition(types.NewCondition(
			"IPForwardingDisabled",
			types.ConditionTrue,
			"ForwardingDisabled",
			fmt.Sprintf("IP forwarding disabled: %s", strings.Join(disabledSettings, ", ")),
		))
	} else {
		status.AddCondition(types.NewCondition(
			"IPForwardingDisabled",
			types.ConditionFalse,
			"ForwardingEnabled",
			"All checked IP forwarding settings are enabled",
		))
		status.AddEvent(types.NewEvent(
			types.EventInfo,
			"IPForwardingHealthy",
			"All IP forwarding settings are correctly enabled for Kubernetes networking",
		))
	}

	return status, nil
}

// checkPerInterfaceForwarding checks per-interface IPv4 forwarding settings.
// Returns a list of disabled settings descriptions.
func (m *IPForwardingMonitor) checkPerInterfaceForwarding(status *types.Status) []string {
	var disabled []string

	pattern := filepath.Join(m.config.ProcPath, perInterfacePattern)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"IPForwardingGlobError",
			fmt.Sprintf("Failed to glob per-interface forwarding files: %v", err),
		))
		return nil
	}

	for _, match := range matches {
		// Extract interface name from path
		ifaceName := extractInterfaceName(match)

		// Skip loopback and special interfaces
		if ifaceName == "lo" || ifaceName == "all" || ifaceName == "default" || ifaceName == "" {
			continue
		}

		// If specific interfaces are configured, filter
		if len(m.config.Interfaces) > 0 && !slices.Contains(m.config.Interfaces, ifaceName) {
			continue
		}

		enabled, err := readForwardingSetting(match)
		if err != nil {
			continue // Skip unreadable interfaces
		}

		if !enabled {
			setting := fmt.Sprintf("net.ipv4.conf.%s.forwarding=0", ifaceName)
			disabled = append(disabled, setting)
			status.AddEvent(types.NewEvent(
				types.EventWarning,
				"InterfaceForwardingDisabled",
				fmt.Sprintf("IPv4 forwarding disabled on interface %s. "+
					"Remediate with: sysctl -w net.ipv4.conf.%s.forwarding=1", ifaceName, ifaceName),
			))
		}
	}

	return disabled
}

// extractInterfaceName extracts the interface name from a per-interface proc path.
// e.g., "/proc/sys/net/ipv4/conf/eth0/forwarding" -> "eth0"
func extractInterfaceName(path string) string {
	parts := strings.Split(path, string(os.PathSeparator))
	for i, part := range parts {
		if part == "conf" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// readForwardingSetting reads a forwarding sysctl file and returns whether forwarding is enabled.
func readForwardingSetting(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("failed to read %s: %w", path, err)
	}

	value := strings.TrimSpace(string(data))
	return value == "1", nil
}
