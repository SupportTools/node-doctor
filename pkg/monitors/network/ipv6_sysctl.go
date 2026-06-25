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
	// Default configuration values for IPv6 sysctl monitor.
	defaultIPv6ExpectEnabled       = true
	defaultIPv6CheckPerInterface   = false
	defaultIPv6SysctlProcPath      = "/proc"
	ipv6AllDisableSysctlPath       = "sys/net/ipv6/conf/all/disable_ipv6"
	ipv6DefaultDisableSysctlPath   = "sys/net/ipv6/conf/default/disable_ipv6"
	ipv6PerIfaceDisableGlobPattern = "sys/net/ipv6/conf/*/disable_ipv6"
)

// defaultIPv6SkipInterfaces are interfaces that are excluded from per-interface
// disable_ipv6 checks. "all" and "default" are the global pseudo-interfaces and
// are checked separately; "lo" is the loopback and intentionally has IPv6
// disabled on some hardened images.
var defaultIPv6SkipInterfaces = []string{"all", "default", "lo"}

// IPv6SysctlConfig holds configuration for the IPv6 sysctl monitor.
type IPv6SysctlConfig struct {
	// ExpectIPv6Enabled controls severity. When true, disable_ipv6=1 is treated
	// as a misconfiguration (warning). When false, the value is recorded but
	// not flagged.
	ExpectIPv6Enabled bool
	// CheckPerInterface enables scanning per-interface disable_ipv6 settings.
	CheckPerInterface bool
	// Interfaces, when non-empty, restricts per-interface checks to these
	// interface names. Empty means check every interface discovered via glob.
	Interfaces []string
	// SkipInterfaces lists interface names to exclude from per-interface
	// checks. Defaults to {"all", "default", "lo"}.
	SkipInterfaces []string
	// ProcPath is the base path for the proc filesystem. Defaults to "/proc";
	// override with "/host/proc" for containerized deployments.
	ProcPath string
}

// IPv6SysctlMonitor monitors IPv6 sysctls relevant to Kubernetes networking.
// This monitor is detection-only and does not modify any sysctls.
type IPv6SysctlMonitor struct {
	name   string
	config *IPv6SysctlConfig

	*monitors.BaseMonitor
}

// init registers the IPv6 sysctl monitor with the monitor registry.
func init() {
	monitors.MustRegister(monitors.MonitorInfo{
		Type:        "network-ipv6-sysctl",
		Factory:     NewIPv6SysctlMonitor,
		Validator:   ValidateIPv6SysctlConfig,
		Description: "Detection-only monitor for IPv6 disable_ipv6 sysctls (does not modify settings)",
		DefaultConfig: &types.MonitorConfig{
			Name:           "ipv6-sysctl-check",
			Type:           "network-ipv6-sysctl",
			Enabled:        true,
			IntervalString: "60s",
			TimeoutString:  "5s",
			Config: map[string]any{
				"expectIPv6Enabled": true,
				"checkPerInterface": false,
				"procPath":          "/proc",
			},
		},
	})
}

// NewIPv6SysctlMonitor creates a new IPv6 sysctl monitor instance.
func NewIPv6SysctlMonitor(ctx context.Context, config types.MonitorConfig) (types.Monitor, error) {
	cfg, err := parseIPv6SysctlConfig(config.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ipv6 sysctl config: %w", err)
	}

	baseMonitor, err := monitors.NewBaseMonitor(config.Name, config.Interval, config.Timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to create base monitor: %w", err)
	}

	monitor := &IPv6SysctlMonitor{
		name:        config.Name,
		config:      cfg,
		BaseMonitor: baseMonitor,
	}

	if err := baseMonitor.SetCheckFunc(monitor.checkIPv6Sysctl); err != nil {
		return nil, fmt.Errorf("failed to set check function: %w", err)
	}

	return monitor, nil
}

// parseIPv6SysctlConfig parses configuration from a generic map.
func parseIPv6SysctlConfig(configMap map[string]any) (*IPv6SysctlConfig, error) {
	config := &IPv6SysctlConfig{
		ExpectIPv6Enabled: defaultIPv6ExpectEnabled,
		CheckPerInterface: defaultIPv6CheckPerInterface,
		ProcPath:          defaultIPv6SysctlProcPath,
		SkipInterfaces:    append([]string(nil), defaultIPv6SkipInterfaces...),
	}

	if configMap == nil {
		return config, nil
	}

	if v, ok := configMap["expectIPv6Enabled"]; ok {
		boolVal, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("expectIPv6Enabled must be a boolean, got %T", v)
		}
		config.ExpectIPv6Enabled = boolVal
	}

	if v, ok := configMap["checkPerInterface"]; ok {
		boolVal, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("checkPerInterface must be a boolean, got %T", v)
		}
		config.CheckPerInterface = boolVal
	}

	if v, ok := configMap["interfaces"]; ok {
		ifaces, err := parseStringList(v, "interfaces")
		if err != nil {
			return nil, err
		}
		config.Interfaces = ifaces
	}

	if v, ok := configMap["skipInterfaces"]; ok {
		ifaces, err := parseStringList(v, "skipInterfaces")
		if err != nil {
			return nil, err
		}
		// Explicit override replaces the defaults so operators can opt back
		// into checking lo if desired.
		config.SkipInterfaces = ifaces
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

// parseStringList accepts either []string or []any (where each element is a
// string) from a config map. The fieldName is used for error messages.
func parseStringList(v any, fieldName string) ([]string, error) {
	switch val := v.(type) {
	case []string:
		return val, nil
	case []any:
		out := make([]string, 0, len(val))
		for _, item := range val {
			strVal, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s must be a list of strings, got %T element", fieldName, item)
			}
			out = append(out, strVal)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%s must be a list of strings, got %T", fieldName, v)
	}
}

// ValidateIPv6SysctlConfig validates the IPv6 sysctl monitor configuration.
func ValidateIPv6SysctlConfig(config types.MonitorConfig) error {
	_, err := parseIPv6SysctlConfig(config.Config)
	return err
}

// checkIPv6Sysctl performs the IPv6 sysctl health check.
func (m *IPv6SysctlMonitor) checkIPv6Sysctl(ctx context.Context) (*types.Status, error) {
	status := types.NewStatus(m.name)

	var findings []string

	allPath := filepath.Join(m.config.ProcPath, ipv6AllDisableSysctlPath)
	defaultPath := filepath.Join(m.config.ProcPath, ipv6DefaultDisableSysctlPath)

	m.checkScopedDisableIPv6(status, "all", allPath, &findings)
	m.checkScopedDisableIPv6(status, "default", defaultPath, &findings)

	if m.config.CheckPerInterface {
		ifaceFindings := m.checkPerInterfaceDisableIPv6(status)
		findings = append(findings, ifaceFindings...)
	}

	if len(findings) > 0 {
		status.AddCondition(types.NewCondition(
			"IPv6SysctlMisconfigured",
			types.ConditionTrue,
			"DisableIPv6Set",
			fmt.Sprintf("IPv6 sysctls flagged: %s", strings.Join(findings, ", ")),
		))
	} else {
		status.AddCondition(types.NewCondition(
			"IPv6SysctlMisconfigured",
			types.ConditionFalse,
			"IPv6SysctlsHealthy",
			"All checked IPv6 disable_ipv6 sysctls match expectations",
		))
		status.AddEvent(types.NewEvent(
			types.EventInfo,
			"IPv6SysctlsHealthy",
			"IPv6 disable_ipv6 sysctls are configured as expected",
		))
	}

	return status, nil
}

// checkScopedDisableIPv6 reads a single all/default disable_ipv6 file. Read
// errors are reported as warnings (the IPv6 stack may legitimately be absent
// on hardened kernels), and findings are appended to the supplied slice when
// the value is set and ExpectIPv6Enabled is true.
func (m *IPv6SysctlMonitor) checkScopedDisableIPv6(status *types.Status, scope, path string, findings *[]string) {
	disabled, err := readSysctlBool(path)
	if err != nil {
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"IPv6SysctlReadError",
			fmt.Sprintf("Failed to read net.ipv6.conf.%s.disable_ipv6 from %s: %v", scope, path, err),
		))
		*findings = append(*findings, fmt.Sprintf("net.ipv6.conf.%s.disable_ipv6 (unreadable)", scope))
		return
	}

	if !disabled {
		return
	}

	setting := fmt.Sprintf("net.ipv6.conf.%s.disable_ipv6=1", scope)
	if m.config.ExpectIPv6Enabled {
		*findings = append(*findings, setting)
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"IPv6Disabled",
			fmt.Sprintf("IPv6 is disabled (%s) on scope %q. "+
				"If this cluster expects IPv6 connectivity, this monitor would block "+
				"IPv6 pod networking. This monitor is detection-only and does not modify "+
				"sysctls. To enable: sysctl -w %s", setting, scope, strings.Replace(setting, "=1", "=0", 1)),
		))
	} else {
		status.AddEvent(types.NewEvent(
			types.EventInfo,
			"IPv6DisabledExpected",
			fmt.Sprintf("IPv6 disabled on scope %q (%s); expectIPv6Enabled=false so no action required", scope, setting),
		))
	}
}

// checkPerInterfaceDisableIPv6 globs per-interface disable_ipv6 sysctls and
// returns descriptions of interfaces with disable_ipv6=1 (when expected to be
// enabled).
func (m *IPv6SysctlMonitor) checkPerInterfaceDisableIPv6(status *types.Status) []string {
	var disabled []string

	pattern := filepath.Join(m.config.ProcPath, ipv6PerIfaceDisableGlobPattern)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"IPv6SysctlGlobError",
			fmt.Sprintf("Failed to glob per-interface disable_ipv6 files: %v", err),
		))
		return nil
	}

	skip := m.config.SkipInterfaces
	if skip == nil {
		skip = defaultIPv6SkipInterfaces
	}

	for _, match := range matches {
		ifaceName := extractInterfaceName(match)
		if ifaceName == "" {
			continue
		}
		if slices.Contains(skip, ifaceName) {
			continue
		}
		if len(m.config.Interfaces) > 0 && !slices.Contains(m.config.Interfaces, ifaceName) {
			continue
		}

		isDisabled, err := readSysctlBool(match)
		if err != nil {
			// Skip unreadable interfaces silently — per-interface files race
			// with link teardown and noisy errors are not actionable.
			continue
		}

		if !isDisabled {
			continue
		}

		setting := fmt.Sprintf("net.ipv6.conf.%s.disable_ipv6=1", ifaceName)
		if m.config.ExpectIPv6Enabled {
			disabled = append(disabled, setting)
			status.AddEvent(types.NewEvent(
				types.EventWarning,
				"InterfaceIPv6Disabled",
				fmt.Sprintf("IPv6 disabled on interface %s (%s). Detection-only — no sysctl change applied.",
					ifaceName, setting),
			))
		} else {
			status.AddEvent(types.NewEvent(
				types.EventInfo,
				"InterfaceIPv6DisabledExpected",
				fmt.Sprintf("IPv6 disabled on interface %s (%s); expectIPv6Enabled=false so no action required",
					ifaceName, setting),
			))
		}
	}

	return disabled
}

// readSysctlBool reads a sysctl-style file and returns true when its content
// (trimmed of whitespace) is "1". Any other value is treated as false. Errors
// are propagated.
func readSysctlBool(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("failed to read %s: %w", path, err)
	}
	return strings.TrimSpace(string(data)) == "1", nil
}
