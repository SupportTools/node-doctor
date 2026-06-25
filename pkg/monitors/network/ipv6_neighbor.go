// Package network provides network health monitoring capabilities.
package network

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/supporttools/node-doctor/pkg/monitors"
	"github.com/supporttools/node-doctor/pkg/types"
)

// NOTE (out of scope): True NDP neighbor-cache reachability (i.e. inspecting the
// kernel neighbor table for REACHABLE/STALE/FAILED states of on-link IPv6
// neighbors and the default router) requires a netlink RTM_GETNEIGH dump.
// node-doctor does not vendor a netlink library and this task explicitly forbids
// adding a new dependency or shelling out to `ip`. Reading the neighbor cache via
// /proc is not possible (there is no stable /proc representation of the IPv6
// neighbor table). This monitor therefore assesses RA/SLAAC *configuration and
// outcome* using readable /proc sources (configured addresses + accept_ra /
// autoconf sysctls) rather than live neighbor reachability. A follow-up that adds
// netlink-based NDP reachability is tracked as a separate task (#17207 follow-up).

const (
	// Default configuration values for the IPv6 neighbor / RA / SLAAC monitor.
	defaultIPv6NeighborExpectEnabled        = true
	defaultIPv6NeighborCheckPerIface        = true
	defaultIPv6NeighborRequireGlobal        = false
	defaultIPv6NeighborProcPath             = "/proc"
	ipv6IfInet6RelPath                      = "net/if_inet6"
	ipv6AcceptRARelGlob                     = "sys/net/ipv6/conf/*/accept_ra"
	ipv6ConfDirRelPath                      = "sys/net/ipv6/conf"
	ipv6NeighborAutoconfFileName            = "autoconf"
	ipv6LinkLocalScopeHex            uint64 = 0x20
)

// Condition types emitted by the IPv6 neighbor monitor.
const (
	conditionIPv6LinkLocalMissing = "IPv6LinkLocalMissing"
	conditionIPv6GlobalMissing    = "IPv6GlobalAddressMissing"
	conditionIPv6RADisabled       = "IPv6RouterAdvertisementDisabled"
)

// defaultIPv6NeighborSkipInterfaces are interfaces excluded from per-interface
// address and RA/autoconf checks. "all"/"default" are global pseudo-interfaces
// (no entries in if_inet6) and "lo" is the loopback, which carries only ::1 and
// never participates in RA/SLAAC.
var defaultIPv6NeighborSkipInterfaces = []string{"all", "default", "lo"}

// IPv6NeighborConfig holds configuration for the IPv6 neighbor / RA / SLAAC monitor.
type IPv6NeighborConfig struct {
	// ExpectIPv6Enabled controls severity. When true, missing link-local
	// addresses and disabled RA where IPv6 is expected are treated as problems.
	// When false the findings are recorded informationally only.
	ExpectIPv6Enabled bool
	// CheckPerInterface enables scanning per-interface accept_ra / autoconf
	// sysctls. Address checks (if_inet6) always run.
	CheckPerInterface bool
	// RequireGlobalAddress controls whether a non-loopback interface that has a
	// link-local address but no global/SLAAC address is flagged as a warning.
	RequireGlobalAddress bool
	// Interfaces, when non-empty, restricts checks to these interface names.
	// Empty means check every interface discovered via if_inet6 / glob.
	Interfaces []string
	// SkipInterfaces lists interface names to exclude. Defaults to
	// {"all", "default", "lo"}.
	SkipInterfaces []string
	// ProcPath is the base path for the proc filesystem. Defaults to "/proc";
	// override with "/host/proc" for containerized deployments.
	ProcPath string
}

// IPv6NeighborMonitor assesses IPv6 RA/SLAAC health from /proc. It is
// detection-only and never modifies addresses, routes, or sysctls.
type IPv6NeighborMonitor struct {
	name   string
	config *IPv6NeighborConfig

	*monitors.BaseMonitor
}

// init registers the IPv6 neighbor / RA / SLAAC monitor with the registry.
func init() {
	monitors.MustRegister(monitors.MonitorInfo{
		Type:        "network-ipv6-neighbor",
		Factory:     NewIPv6NeighborMonitor,
		Validator:   ValidateIPv6NeighborConfig,
		Description: "Detection-only monitor for IPv6 RA/SLAAC health via configured addresses and accept_ra/autoconf sysctls (does not modify state)",
		DefaultConfig: &types.MonitorConfig{
			Name:           "ipv6-neighbor-check",
			Type:           "network-ipv6-neighbor",
			Enabled:        true,
			IntervalString: "60s",
			TimeoutString:  "5s",
			Config: map[string]any{
				"expectIPv6Enabled":    true,
				"checkPerInterface":    true,
				"requireGlobalAddress": false,
				"procPath":             "/proc",
			},
		},
	})
}

// NewIPv6NeighborMonitor creates a new IPv6 neighbor / RA / SLAAC monitor instance.
func NewIPv6NeighborMonitor(ctx context.Context, config types.MonitorConfig) (types.Monitor, error) {
	cfg, err := parseIPv6NeighborConfig(config.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ipv6 neighbor config: %w", err)
	}

	baseMonitor, err := monitors.NewBaseMonitor(config.Name, config.Interval, config.Timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to create base monitor: %w", err)
	}

	monitor := &IPv6NeighborMonitor{
		name:        config.Name,
		config:      cfg,
		BaseMonitor: baseMonitor,
	}

	if err := baseMonitor.SetCheckFunc(monitor.checkIPv6Neighbor); err != nil {
		return nil, fmt.Errorf("failed to set check function: %w", err)
	}

	return monitor, nil
}

// parseIPv6NeighborConfig parses configuration from a generic map.
func parseIPv6NeighborConfig(configMap map[string]any) (*IPv6NeighborConfig, error) {
	config := &IPv6NeighborConfig{
		ExpectIPv6Enabled:    defaultIPv6NeighborExpectEnabled,
		CheckPerInterface:    defaultIPv6NeighborCheckPerIface,
		RequireGlobalAddress: defaultIPv6NeighborRequireGlobal,
		ProcPath:             defaultIPv6NeighborProcPath,
		SkipInterfaces:       append([]string(nil), defaultIPv6NeighborSkipInterfaces...),
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

	if v, ok := configMap["requireGlobalAddress"]; ok {
		boolVal, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("requireGlobalAddress must be a boolean, got %T", v)
		}
		config.RequireGlobalAddress = boolVal
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
		// Explicit override replaces the defaults so operators can opt back into
		// checking lo if desired.
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

// ValidateIPv6NeighborConfig validates the IPv6 neighbor monitor configuration.
func ValidateIPv6NeighborConfig(config types.MonitorConfig) error {
	_, err := parseIPv6NeighborConfig(config.Config)
	return err
}

// ipv6IfInet6Path returns the full path to the if_inet6 address table.
func (m *IPv6NeighborMonitor) ipv6IfInet6Path() string {
	return filepath.Join(m.config.ProcPath, ipv6IfInet6RelPath)
}

// ipv6Address describes a single IPv6 address parsed from /proc/net/if_inet6.
type ipv6Address struct {
	// IfaceName is the device name (e.g. "eth0").
	IfaceName string
	// Scope is the raw scope value from if_inet6 (0x20 = link-local, 0x00 =
	// global).
	Scope uint64
	// IsLinkLocal reports whether the scope indicates a link-local address.
	IsLinkLocal bool
	// IsGlobal reports whether the scope indicates a global address (scope 0).
	IsGlobal bool
}

// ifaceAddrSummary aggregates per-interface address presence.
type ifaceAddrSummary struct {
	hasLinkLocal bool
	hasGlobal    bool
}

// checkIPv6Neighbor performs the IPv6 RA/SLAAC health check.
func (m *IPv6NeighborMonitor) checkIPv6Neighbor(ctx context.Context) (*types.Status, error) {
	status := types.NewStatus(m.name)

	skip := m.config.SkipInterfaces
	if skip == nil {
		skip = defaultIPv6NeighborSkipInterfaces
	}

	addrs, readErr := parseIfInet6File(m.ipv6IfInet6Path())
	if readErr != nil {
		// A missing/unreadable if_inet6 means the IPv6 stack may legitimately be
		// absent (hardened or IPv4-only node). Treat as a warning, consistent
		// with the IPv6 sysctl monitor, and report the conditions as unknown
		// outcomes rather than hard-failing.
		if ipv6IfInet6Unreadable(readErr) {
			status.AddEvent(types.NewEvent(
				types.EventWarning,
				"IPv6IfInet6ReadError",
				fmt.Sprintf("Failed to read IPv6 address table from %s: %v. "+
					"The IPv6 stack may be absent on this node.", m.ipv6IfInet6Path(), readErr),
			))
			m.recordAddressTableUnreadable(status)
			// Still attempt the RA/autoconf sysctl scan below; those files may be
			// present even when if_inet6 is not.
		} else {
			status.AddEvent(types.NewEvent(
				types.EventWarning,
				"IPv6IfInet6ParseError",
				fmt.Sprintf("Failed to parse IPv6 address table from %s: %v", m.ipv6IfInet6Path(), readErr),
			))
			m.recordAddressTableUnreadable(status)
		}
	} else {
		m.evaluateAddresses(status, addrs, skip)
	}

	if m.config.CheckPerInterface {
		m.checkRouterAdvertisement(status, skip)
	} else {
		// RA scan disabled: record the condition as healthy so consumers always
		// see a definitive state.
		status.AddCondition(types.NewCondition(
			conditionIPv6RADisabled,
			types.ConditionFalse,
			"IPv6RADisabledCheckSkipped",
			"Per-interface RA/autoconf check disabled (checkPerInterface=false)",
		))
	}

	return status, nil
}

// evaluateAddresses inspects the parsed if_inet6 addresses and records the
// link-local and global address conditions.
func (m *IPv6NeighborMonitor) evaluateAddresses(status *types.Status, addrs []ipv6Address, skip []string) {
	summaries := make(map[string]*ifaceAddrSummary)
	order := make([]string, 0)

	for _, addr := range addrs {
		if slices.Contains(skip, addr.IfaceName) {
			continue
		}
		if len(m.config.Interfaces) > 0 && !slices.Contains(m.config.Interfaces, addr.IfaceName) {
			continue
		}
		s, ok := summaries[addr.IfaceName]
		if !ok {
			s = &ifaceAddrSummary{}
			summaries[addr.IfaceName] = s
			order = append(order, addr.IfaceName)
		}
		if addr.IsLinkLocal {
			s.hasLinkLocal = true
		}
		if addr.IsGlobal {
			s.hasGlobal = true
		}
	}

	var linkLocalMissing []string
	var globalMissing []string

	for _, iface := range order {
		s := summaries[iface]
		if !s.hasLinkLocal {
			linkLocalMissing = append(linkLocalMissing, iface)
			status.AddEvent(types.NewEvent(
				m.severity(),
				"IPv6LinkLocalMissing",
				fmt.Sprintf("Interface %s has no IPv6 link-local address; "+
					"RA/SLAAC and on-link neighbor discovery cannot operate without one. "+
					"This monitor is detection-only.", iface),
			))
		}
		if !s.hasGlobal {
			globalMissing = append(globalMissing, iface)
			if m.config.RequireGlobalAddress {
				status.AddEvent(types.NewEvent(
					m.severity(),
					"IPv6GlobalAddressMissing",
					fmt.Sprintf("Interface %s has no global/SLAAC IPv6 address; "+
						"the node may lack IPv6 connectivity. This monitor is detection-only.", iface),
				))
			}
		}
	}

	m.recordLinkLocalCondition(status, linkLocalMissing, len(order))
	m.recordGlobalCondition(status, globalMissing, len(order))
}

// recordLinkLocalCondition records the IPv6LinkLocalMissing condition.
func (m *IPv6NeighborMonitor) recordLinkLocalCondition(status *types.Status, missing []string, ifaceCount int) {
	if len(missing) > 0 && m.config.ExpectIPv6Enabled {
		status.AddCondition(types.NewCondition(
			conditionIPv6LinkLocalMissing,
			types.ConditionTrue,
			"IPv6LinkLocalMissing",
			fmt.Sprintf("Interfaces missing an IPv6 link-local address: %s", strings.Join(missing, ", ")),
		))
		return
	}

	if len(missing) > 0 {
		// expectIPv6Enabled=false: record but do not flag.
		status.AddCondition(types.NewCondition(
			conditionIPv6LinkLocalMissing,
			types.ConditionFalse,
			"IPv6LinkLocalMissingNotExpected",
			fmt.Sprintf("Interfaces without an IPv6 link-local address (%s); expectIPv6Enabled=false so no action required",
				strings.Join(missing, ", ")),
		))
		return
	}

	reason := "IPv6LinkLocalPresent"
	msg := "All checked interfaces have an IPv6 link-local address"
	if ifaceCount == 0 {
		reason = "IPv6NoInterfacesObserved"
		msg = "No non-skipped IPv6 interfaces observed in if_inet6"
	}
	status.AddCondition(types.NewCondition(
		conditionIPv6LinkLocalMissing,
		types.ConditionFalse,
		reason,
		msg,
	))
}

// recordGlobalCondition records the IPv6GlobalAddressMissing condition. The
// condition is only flagged True when RequireGlobalAddress is set (and IPv6 is
// expected).
func (m *IPv6NeighborMonitor) recordGlobalCondition(status *types.Status, missing []string, ifaceCount int) {
	if len(missing) > 0 && m.config.RequireGlobalAddress && m.config.ExpectIPv6Enabled {
		status.AddCondition(types.NewCondition(
			conditionIPv6GlobalMissing,
			types.ConditionTrue,
			"IPv6GlobalAddressMissing",
			fmt.Sprintf("Interfaces missing a global/SLAAC IPv6 address: %s", strings.Join(missing, ", ")),
		))
		return
	}

	if len(missing) > 0 {
		reason := "IPv6GlobalAddressMissingNotRequired"
		msg := fmt.Sprintf("Interfaces without a global/SLAAC IPv6 address (%s); requireGlobalAddress=false so no action required",
			strings.Join(missing, ", "))
		if !m.config.ExpectIPv6Enabled {
			reason = "IPv6GlobalAddressMissingNotExpected"
			msg = fmt.Sprintf("Interfaces without a global/SLAAC IPv6 address (%s); expectIPv6Enabled=false so no action required",
				strings.Join(missing, ", "))
		}
		status.AddCondition(types.NewCondition(
			conditionIPv6GlobalMissing,
			types.ConditionFalse,
			reason,
			msg,
		))
		return
	}

	reason := "IPv6GlobalAddressPresent"
	msg := "All checked interfaces have a global/SLAAC IPv6 address"
	if ifaceCount == 0 {
		reason = "IPv6NoInterfacesObserved"
		msg = "No non-skipped IPv6 interfaces observed in if_inet6"
	}
	status.AddCondition(types.NewCondition(
		conditionIPv6GlobalMissing,
		types.ConditionFalse,
		reason,
		msg,
	))
}

// recordAddressTableUnreadable records both address conditions as False (cannot
// confirm a problem) when if_inet6 could not be read.
func (m *IPv6NeighborMonitor) recordAddressTableUnreadable(status *types.Status) {
	status.AddCondition(types.NewCondition(
		conditionIPv6LinkLocalMissing,
		types.ConditionFalse,
		"IPv6AddressTableUnreadable",
		"IPv6 address table (if_inet6) is unreadable; cannot confirm link-local addresses",
	))
	status.AddCondition(types.NewCondition(
		conditionIPv6GlobalMissing,
		types.ConditionFalse,
		"IPv6AddressTableUnreadable",
		"IPv6 address table (if_inet6) is unreadable; cannot confirm global addresses",
	))
}

// checkRouterAdvertisement scans per-interface accept_ra (and the companion
// autoconf) sysctls and records the IPv6RouterAdvertisementDisabled condition.
func (m *IPv6NeighborMonitor) checkRouterAdvertisement(status *types.Status, skip []string) {
	pattern := filepath.Join(m.config.ProcPath, ipv6AcceptRARelGlob)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"IPv6AcceptRAGlobError",
			fmt.Sprintf("Failed to glob per-interface accept_ra files: %v", err),
		))
		status.AddCondition(types.NewCondition(
			conditionIPv6RADisabled,
			types.ConditionFalse,
			"IPv6AcceptRAUnreadable",
			"Per-interface accept_ra files could not be enumerated; cannot confirm RA acceptance",
		))
		return
	}

	if len(matches) == 0 {
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"IPv6AcceptRAReadError",
			fmt.Sprintf("No per-interface accept_ra sysctls found under %s; the IPv6 stack may be absent",
				filepath.Join(m.config.ProcPath, ipv6ConfDirRelPath)),
		))
		status.AddCondition(types.NewCondition(
			conditionIPv6RADisabled,
			types.ConditionFalse,
			"IPv6AcceptRAUnreadable",
			"No per-interface accept_ra sysctls found; cannot confirm RA acceptance",
		))
		return
	}

	var disabled []string

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

		raVal, err := readSysctlInt(match)
		if err != nil {
			// Per-interface files race with link teardown; skip silently.
			continue
		}

		autoconfPath := filepath.Join(filepath.Dir(match), ipv6NeighborAutoconfFileName)
		autoconfVal, autoconfErr := readSysctlInt(autoconfPath)

		raOff := raVal == 0
		autoconfOff := autoconfErr == nil && autoconfVal == 0

		if !raOff && !autoconfOff {
			continue
		}

		var parts []string
		if raOff {
			parts = append(parts, fmt.Sprintf("net.ipv6.conf.%s.accept_ra=0", ifaceName))
		}
		if autoconfOff {
			parts = append(parts, fmt.Sprintf("net.ipv6.conf.%s.autoconf=0", ifaceName))
		}
		finding := strings.Join(parts, ", ")

		if m.config.ExpectIPv6Enabled {
			disabled = append(disabled, finding)
			status.AddEvent(types.NewEvent(
				types.EventWarning,
				"IPv6RouterAdvertisementDisabled",
				fmt.Sprintf("Router advertisement / SLAAC disabled on interface %s (%s); "+
					"the interface will not auto-configure an IPv6 address from RAs. "+
					"This monitor is detection-only and does not modify sysctls.", ifaceName, finding),
			))
		} else {
			status.AddEvent(types.NewEvent(
				types.EventInfo,
				"IPv6RouterAdvertisementDisabledExpected",
				fmt.Sprintf("Router advertisement / SLAAC disabled on interface %s (%s); expectIPv6Enabled=false so no action required",
					ifaceName, finding),
			))
		}
	}

	if len(disabled) > 0 {
		status.AddCondition(types.NewCondition(
			conditionIPv6RADisabled,
			types.ConditionTrue,
			"IPv6RouterAdvertisementDisabled",
			fmt.Sprintf("RA/SLAAC disabled: %s", strings.Join(disabled, "; ")),
		))
		return
	}

	status.AddCondition(types.NewCondition(
		conditionIPv6RADisabled,
		types.ConditionFalse,
		"IPv6RouterAdvertisementEnabled",
		"All checked interfaces accept router advertisements (accept_ra/autoconf not disabled)",
	))
}

// severity returns the event severity that corresponds to ExpectIPv6Enabled:
// warnings when IPv6 is expected, informational otherwise.
func (m *IPv6NeighborMonitor) severity() types.EventSeverity {
	if m.config.ExpectIPv6Enabled {
		return types.EventWarning
	}
	return types.EventInfo
}

// parseIfInet6File reads and parses <procPath>/net/if_inet6. Each line is
// whitespace-separated:
//
//	<32-hex-addr> <ifindex_hex> <prefixlen_hex> <scope_hex> <flags_hex> <devname>
//
// Scope 0x20 = link-local, 0x00 = global. Parse errors on individual lines are
// skipped; a read/open failure is returned to the caller.
func parseIfInet6File(path string) ([]ipv6Address, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", path, err)
	}
	defer f.Close()

	var addrs []ipv6Address
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		// Need at least scope (field index 3) and devname (last field).
		if len(fields) < 6 {
			continue
		}
		scope, err := parseHexScope(fields[3])
		if err != nil {
			continue
		}
		devName := fields[len(fields)-1]
		addrs = append(addrs, ipv6Address{
			IfaceName:   devName,
			Scope:       scope,
			IsLinkLocal: scope == ipv6LinkLocalScopeHex,
			IsGlobal:    scope == 0,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}
	return addrs, nil
}

// parseHexScope parses a scope field from if_inet6, tolerating an optional "0x"
// prefix (the kernel writes a bare two-hex-digit value, e.g. "20", but we accept
// "0x20" for robustness).
func parseHexScope(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	if s == "" {
		return 0, errors.New("empty scope field")
	}
	return strconv.ParseUint(s, 16, 64)
}

// readSysctlInt reads a sysctl-style file and returns its integer value (after
// trimming whitespace). Used for accept_ra / autoconf, which take values 0/1/2.
func readSysctlInt(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("failed to read %s: %w", path, err)
	}
	val, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("failed to parse %s: %w", path, err)
	}
	return val, nil
}

// ipv6IfInet6Unreadable reports whether the error from parseIfInet6File
// indicates the file could not be opened/read (as opposed to a parse failure).
func ipv6IfInet6Unreadable(err error) bool {
	var pathErr *fs.PathError
	return errors.As(err, &pathErr)
}
