// Package network provides network health monitoring capabilities.
package network

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/supporttools/node-doctor/pkg/monitors"
	"github.com/supporttools/node-doctor/pkg/types"
)

const (
	// Default configuration values for the IPv6 default-route monitor.
	defaultIPv6RouteExpectDefault = true
	defaultIPv6RouteProcPath      = "/proc"

	// ipv6RouteRelPath is the route-table path relative to the proc mount.
	// The monitor reads <procPath>/net/ipv6_route.
	ipv6RouteRelPath = "net/ipv6_route"
)

// IPv6RouteConfig holds configuration for the IPv6 default-route monitor.
type IPv6RouteConfig struct {
	// ExpectDefaultRoute controls severity. When true, the absence of an IPv6
	// default route is treated as a problem (condition True). When false, the
	// absence is recorded but not flagged.
	ExpectDefaultRoute bool
	// ProcPath is the base path for the proc filesystem. Defaults to "/proc";
	// override with "/host/proc" for containerized deployments. The monitor
	// reads <ProcPath>/net/ipv6_route.
	ProcPath string
}

// IPv6RouteMonitor checks whether an IPv6 default route is present on the node.
// This monitor is detection-only and never modifies routes.
type IPv6RouteMonitor struct {
	name   string
	config *IPv6RouteConfig

	*monitors.BaseMonitor
}

// init registers the IPv6 default-route monitor with the monitor registry.
func init() {
	monitors.MustRegister(monitors.MonitorInfo{
		Type:        "network-ipv6-route",
		Factory:     NewIPv6RouteMonitor,
		Validator:   ValidateIPv6RouteConfig,
		Description: "Detection-only monitor for the IPv6 default route (does not modify routes)",
		DefaultConfig: &types.MonitorConfig{
			Name:           "ipv6-route-check",
			Type:           "network-ipv6-route",
			Enabled:        true,
			IntervalString: "60s",
			TimeoutString:  "5s",
			Config: map[string]any{
				"expectDefaultRoute": true,
				"procPath":           "/proc",
			},
		},
	})
}

// NewIPv6RouteMonitor creates a new IPv6 default-route monitor instance.
func NewIPv6RouteMonitor(ctx context.Context, config types.MonitorConfig) (types.Monitor, error) {
	cfg, err := parseIPv6RouteConfig(config.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ipv6 route config: %w", err)
	}

	baseMonitor, err := monitors.NewBaseMonitor(config.Name, config.Interval, config.Timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to create base monitor: %w", err)
	}

	monitor := &IPv6RouteMonitor{
		name:        config.Name,
		config:      cfg,
		BaseMonitor: baseMonitor,
	}

	if err := baseMonitor.SetCheckFunc(monitor.checkIPv6Route); err != nil {
		return nil, fmt.Errorf("failed to set check function: %w", err)
	}

	return monitor, nil
}

// parseIPv6RouteConfig parses configuration from a generic map.
func parseIPv6RouteConfig(configMap map[string]any) (*IPv6RouteConfig, error) {
	config := &IPv6RouteConfig{
		ExpectDefaultRoute: defaultIPv6RouteExpectDefault,
		ProcPath:           defaultIPv6RouteProcPath,
	}

	if configMap == nil {
		return config, nil
	}

	if v, ok := configMap["expectDefaultRoute"]; ok {
		boolVal, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("expectDefaultRoute must be a boolean, got %T", v)
		}
		config.ExpectDefaultRoute = boolVal
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

// ValidateIPv6RouteConfig validates the IPv6 default-route monitor configuration.
func ValidateIPv6RouteConfig(config types.MonitorConfig) error {
	_, err := parseIPv6RouteConfig(config.Config)
	return err
}

// ipv6RoutePath returns the full path to the IPv6 route table for this monitor.
func (m *IPv6RouteMonitor) ipv6RoutePath() string {
	return filepath.Join(m.config.ProcPath, ipv6RouteRelPath)
}

// checkIPv6Route performs the IPv6 default-route health check. It reuses the
// IPv6 route-table parser from the gateway monitor
// (detectDefaultIPv6GatewayFromFile) rather than re-parsing the route table.
func (m *IPv6RouteMonitor) checkIPv6Route(ctx context.Context) (*types.Status, error) {
	status := types.NewStatus(m.name)

	path := m.ipv6RoutePath()

	gateway, err := detectDefaultIPv6GatewayFromFile(path)
	if err != nil {
		// A missing or unreadable route table means the IPv6 stack may be
		// legitimately absent (e.g. a hardened or IPv4-only node). Treat this
		// as a warning rather than a hard error, consistent with the IPv6
		// sysctl monitor.
		if isIPv6RouteUnreadable(err) {
			status.AddEvent(types.NewEvent(
				types.EventWarning,
				"IPv6RouteReadError",
				fmt.Sprintf("Failed to read IPv6 route table from %s: %v. "+
					"The IPv6 stack may be absent on this node.", path, err),
			))
			m.recordDefaultRouteAbsent(status, "IPv6RouteTableUnreadable",
				fmt.Sprintf("IPv6 route table %s is unreadable; cannot confirm an IPv6 default route", path))
			return status, nil
		}

		// The route table was readable but contains no IPv6 default route
		// (only on-link/link-scoped routes, or no routes at all).
		m.recordDefaultRouteAbsent(status, "NoIPv6DefaultRoute",
			"No IPv6 default route is present in the IPv6 route table")
		return status, nil
	}

	// A default route exists.
	status.AddCondition(types.NewCondition(
		"IPv6DefaultRouteMissing",
		types.ConditionFalse,
		"IPv6DefaultRoutePresent",
		fmt.Sprintf("IPv6 default route present via gateway %s", gateway),
	))
	status.AddEvent(types.NewEvent(
		types.EventInfo,
		"IPv6DefaultRoutePresent",
		fmt.Sprintf("IPv6 default route is present (next-hop %s)", gateway),
	))

	return status, nil
}

// recordDefaultRouteAbsent records the condition and event for an absent IPv6
// default route. Severity depends on ExpectDefaultRoute: when a default route
// is expected the condition is True (a problem); otherwise it is False and the
// absence is reported informationally.
func (m *IPv6RouteMonitor) recordDefaultRouteAbsent(status *types.Status, reason, message string) {
	if m.config.ExpectDefaultRoute {
		status.AddCondition(types.NewCondition(
			"IPv6DefaultRouteMissing",
			types.ConditionTrue,
			reason,
			message,
		))
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			reason,
			fmt.Sprintf("%s. This monitor is detection-only and does not modify routes.", message),
		))
		return
	}

	status.AddCondition(types.NewCondition(
		"IPv6DefaultRouteMissing",
		types.ConditionFalse,
		"IPv6DefaultRouteNotExpected",
		fmt.Sprintf("%s; expectDefaultRoute=false so no action required", message),
	))
	status.AddEvent(types.NewEvent(
		types.EventInfo,
		"IPv6DefaultRouteNotExpected",
		fmt.Sprintf("%s; expectDefaultRoute=false so no action required", message),
	))
}

// isIPv6RouteUnreadable reports whether the error from
// detectDefaultIPv6GatewayFromFile indicates the route table could not be read
// (as opposed to being read successfully but containing no default route). The
// parser wraps os.Open failures, so we match against fs path errors.
func isIPv6RouteUnreadable(err error) bool {
	var pathErr *fs.PathError
	return errors.As(err, &pathErr)
}
