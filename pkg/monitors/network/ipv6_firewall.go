// Package network provides network health monitoring capabilities.
package network

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/supporttools/node-doctor/pkg/monitors"
	"github.com/supporttools/node-doctor/pkg/types"
)

// Backend identifiers for the IPv6 firewall monitor.
const (
	// ipv6FirewallBackendAuto selects nft when the nft binary is present,
	// otherwise falls back to ip6tables.
	ipv6FirewallBackendAuto = "auto"
	// ipv6FirewallBackendIP6Tables forces the legacy ip6tables backend.
	ipv6FirewallBackendIP6Tables = "ip6tables"
	// ipv6FirewallBackendNFT forces the nftables backend.
	ipv6FirewallBackendNFT = "nft"
)

const (
	// Default configuration values for the IPv6 firewall sanity monitor.
	defaultIPv6FirewallExpectEnabled = true
	defaultIPv6FirewallBackend       = ipv6FirewallBackendAuto

	// ip6tablesBinary / nftBinary are the firewall tools this monitor reads.
	ip6tablesBinary = "ip6tables"
	nftBinary       = "nft"

	// conditionIPv6FirewallBlackhole is the condition reported by this monitor.
	conditionIPv6FirewallBlackhole = "IPv6FirewallBlackhole"

	// ipv6FilterChains are the built-in filter-table chains whose default
	// policy this monitor inspects for an obvious black-hole.
	chainInput   = "INPUT"
	chainForward = "FORWARD"
	chainOutput  = "OUTPUT"
)

// ipv6FilterChains is the set of built-in filter-table chains checked for a
// DROP/REJECT default policy with no ACCEPT rules.
var ipv6FilterChains = []string{chainInput, chainForward, chainOutput}

// CommandExecutor abstracts read-only command execution so tests can inject
// canned ip6tables / nft output. This mirrors the executor pattern used by the
// custom log-pattern monitor and the network remediator.
type CommandExecutor interface {
	// LookPath reports whether the named binary is resolvable in PATH.
	LookPath(name string) (string, error)
	// Run executes name with args and returns combined output. It is only ever
	// invoked with read-only listing verbs by this monitor.
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// defaultCommandExecutor implements CommandExecutor using os/exec.
type defaultCommandExecutor struct{}

func (e *defaultCommandExecutor) LookPath(name string) (string, error) {
	return exec.LookPath(name)
}

func (e *defaultCommandExecutor) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

// IPv6FirewallConfig holds configuration for the IPv6 firewall sanity monitor.
type IPv6FirewallConfig struct {
	// ExpectIPv6Enabled controls severity. When true, an obviously black-holed
	// IPv6 firewall (default DROP with no ACCEPT rules) is treated as a problem
	// (condition True, warning events). When false, the same observation is
	// recorded informationally and the condition is reported False.
	ExpectIPv6Enabled bool
	// Backend forces a firewall backend: "auto" (default), "ip6tables", or
	// "nft". In auto mode the monitor prefers nft when the nft binary is present
	// and falls back to ip6tables.
	Backend string
}

// IPv6FirewallMonitor performs read-only sanity checks of the IPv6 firewall.
//
// DETECTION ONLY: this monitor never adds, deletes, or modifies firewall rules.
// It issues only read-only listing commands (`nft list ruleset`,
// `ip6tables -S`) and reports findings; it applies no remediation.
//
// The heuristic is intentionally conservative to avoid false positives: it only
// flags a node when every built-in filter chain (INPUT/FORWARD/OUTPUT) has a
// default policy of DROP (or REJECT) and the ruleset contains no ACCEPT rule at
// all — i.e. IPv6 traffic is effectively black-holed. It does not attempt to
// validate rule correctness.
type IPv6FirewallMonitor struct {
	name     string
	config   *IPv6FirewallConfig
	executor CommandExecutor

	*monitors.BaseMonitor
}

// init registers the IPv6 firewall sanity monitor with the registry.
func init() {
	monitors.MustRegister(monitors.MonitorInfo{
		Type:        "network-ipv6-firewall",
		Factory:     NewIPv6FirewallMonitor,
		Validator:   ValidateIPv6FirewallConfig,
		Description: "Detection-only sanity monitor for the IPv6 firewall (ip6tables/nft); reads ruleset state but never modifies rules",
		DefaultConfig: &types.MonitorConfig{
			Name:           "ipv6-firewall-check",
			Type:           "network-ipv6-firewall",
			Enabled:        true,
			IntervalString: "60s",
			TimeoutString:  "5s",
			Config: map[string]any{
				"expectIPv6Enabled": true,
				"backend":           ipv6FirewallBackendAuto,
			},
		},
	})
}

// NewIPv6FirewallMonitor creates a new IPv6 firewall sanity monitor instance.
func NewIPv6FirewallMonitor(ctx context.Context, config types.MonitorConfig) (types.Monitor, error) {
	cfg, err := parseIPv6FirewallConfig(config.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ipv6 firewall config: %w", err)
	}

	baseMonitor, err := monitors.NewBaseMonitor(config.Name, config.Interval, config.Timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to create base monitor: %w", err)
	}

	monitor := &IPv6FirewallMonitor{
		name:        config.Name,
		config:      cfg,
		executor:    &defaultCommandExecutor{},
		BaseMonitor: baseMonitor,
	}

	if err := baseMonitor.SetCheckFunc(monitor.checkIPv6Firewall); err != nil {
		return nil, fmt.Errorf("failed to set check function: %w", err)
	}

	return monitor, nil
}

// SetCommandExecutor overrides the command executor (used in tests to inject
// canned ip6tables / nft output).
func (m *IPv6FirewallMonitor) SetCommandExecutor(executor CommandExecutor) {
	m.executor = executor
}

// parseIPv6FirewallConfig parses configuration from a generic map.
func parseIPv6FirewallConfig(configMap map[string]any) (*IPv6FirewallConfig, error) {
	config := &IPv6FirewallConfig{
		ExpectIPv6Enabled: defaultIPv6FirewallExpectEnabled,
		Backend:           defaultIPv6FirewallBackend,
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

	if v, ok := configMap["backend"]; ok {
		strVal, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("backend must be a string, got %T", v)
		}
		switch strVal {
		case ipv6FirewallBackendAuto, ipv6FirewallBackendIP6Tables, ipv6FirewallBackendNFT:
			config.Backend = strVal
		default:
			return nil, fmt.Errorf("backend must be one of %q, %q, or %q, got %q",
				ipv6FirewallBackendAuto, ipv6FirewallBackendIP6Tables, ipv6FirewallBackendNFT, strVal)
		}
	}

	return config, nil
}

// ValidateIPv6FirewallConfig validates the IPv6 firewall monitor configuration.
func ValidateIPv6FirewallConfig(config types.MonitorConfig) error {
	_, err := parseIPv6FirewallConfig(config.Config)
	return err
}

// checkIPv6Firewall performs the IPv6 firewall sanity check.
func (m *IPv6FirewallMonitor) checkIPv6Firewall(ctx context.Context) (*types.Status, error) {
	status := types.NewStatus(m.name)

	backend := m.resolveBackend()
	if backend == "" {
		// Neither tool is present. The node may legitimately lack a firewall
		// tool; report as a warning, not an error, and leave the condition
		// False (we cannot confirm a problem).
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"IPv6FirewallToolNotFound",
			fmt.Sprintf("Neither %q nor %q was found in PATH; cannot assess the IPv6 firewall. "+
				"This may be expected on a node without a host firewall.", nftBinary, ip6tablesBinary),
		))
		m.recordBlackholeAbsent(status, "IPv6FirewallToolUnavailable",
			"No IPv6 firewall tool available; cannot confirm an IPv6 firewall black-hole")
		return status, nil
	}

	if backend == ipv6FirewallBackendNFT {
		m.checkNFT(ctx, status)
		return status, nil
	}
	m.checkIP6Tables(ctx, status)
	return status, nil
}

// resolveBackend determines which firewall backend to read. In auto mode it
// prefers nft when present and falls back to ip6tables. A forced backend is
// returned even if its binary is missing so the missing-binary path can report
// it explicitly.
func (m *IPv6FirewallMonitor) resolveBackend() string {
	switch m.config.Backend {
	case ipv6FirewallBackendNFT:
		return ipv6FirewallBackendNFT
	case ipv6FirewallBackendIP6Tables:
		return ipv6FirewallBackendIP6Tables
	default: // auto
		if _, err := m.executor.LookPath(nftBinary); err == nil {
			return ipv6FirewallBackendNFT
		}
		if _, err := m.executor.LookPath(ip6tablesBinary); err == nil {
			return ipv6FirewallBackendIP6Tables
		}
		return ""
	}
}

// checkNFT reads the nft ruleset (`nft list ruleset`) and evaluates it.
func (m *IPv6FirewallMonitor) checkNFT(ctx context.Context, status *types.Status) {
	if _, err := m.executor.LookPath(nftBinary); err != nil {
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"IPv6FirewallToolNotFound",
			fmt.Sprintf("%q not found in PATH; cannot assess the IPv6 firewall via nft. "+
				"This may be expected on a node without nftables.", nftBinary),
		))
		m.recordBlackholeAbsent(status, "IPv6FirewallToolUnavailable",
			"nft is not available; cannot confirm an IPv6 firewall black-hole")
		return
	}

	// Read-only: `nft list ruleset` only lists the current ruleset.
	out, err := m.executor.Run(ctx, nftBinary, "list", "ruleset")
	if err != nil {
		m.recordReadError(status, nftBinary, err)
		return
	}

	blackholed, chains := evaluateNFTRuleset(string(out))
	m.recordBlackholeFinding(status, ipv6FirewallBackendNFT, blackholed, chains)
}

// checkIP6Tables reads the ip6tables filter table (`ip6tables -S`) and
// evaluates it.
func (m *IPv6FirewallMonitor) checkIP6Tables(ctx context.Context, status *types.Status) {
	if _, err := m.executor.LookPath(ip6tablesBinary); err != nil {
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"IPv6FirewallToolNotFound",
			fmt.Sprintf("%q not found in PATH; cannot assess the IPv6 firewall via ip6tables. "+
				"This may be expected on a node without ip6tables.", ip6tablesBinary),
		))
		m.recordBlackholeAbsent(status, "IPv6FirewallToolUnavailable",
			"ip6tables is not available; cannot confirm an IPv6 firewall black-hole")
		return
	}

	// Read-only: `ip6tables -S` only prints (saves) the current rules.
	out, err := m.executor.Run(ctx, ip6tablesBinary, "-S")
	if err != nil {
		m.recordReadError(status, ip6tablesBinary, err)
		return
	}

	blackholed, chains := evaluateIP6TablesRuleset(string(out))
	m.recordBlackholeFinding(status, ipv6FirewallBackendIP6Tables, blackholed, chains)
}

// recordReadError records a warning + False condition when the ruleset command
// fails (e.g. permission denied). We cannot confirm a problem, so the condition
// is reported False.
func (m *IPv6FirewallMonitor) recordReadError(status *types.Status, tool string, err error) {
	status.AddEvent(types.NewEvent(
		types.EventWarning,
		"IPv6FirewallReadError",
		fmt.Sprintf("Failed to read the IPv6 firewall ruleset via %q: %v. "+
			"This may indicate missing privileges (CAP_NET_ADMIN). "+
			"This monitor is detection-only and does not modify rules.", tool, err),
	))
	m.recordBlackholeAbsent(status, "IPv6FirewallRulesetUnreadable",
		fmt.Sprintf("IPv6 firewall ruleset could not be read via %q; cannot confirm a black-hole", tool))
}

// recordBlackholeFinding records the black-hole condition based on the
// evaluation result and the ExpectIPv6Enabled gate.
func (m *IPv6FirewallMonitor) recordBlackholeFinding(status *types.Status, backend string, blackholed bool, droppedChains []string) {
	if !blackholed {
		status.AddCondition(types.NewCondition(
			conditionIPv6FirewallBlackhole,
			types.ConditionFalse,
			"IPv6FirewallHealthy",
			fmt.Sprintf("IPv6 firewall (%s backend) is not black-holing traffic", backend),
		))
		status.AddEvent(types.NewEvent(
			types.EventInfo,
			"IPv6FirewallHealthy",
			fmt.Sprintf("IPv6 firewall (%s backend) sanity check passed", backend),
		))
		return
	}

	finding := fmt.Sprintf("default policy DROP/REJECT with no ACCEPT rules on chains %s",
		strings.Join(droppedChains, ", "))

	if m.config.ExpectIPv6Enabled {
		status.AddCondition(types.NewCondition(
			conditionIPv6FirewallBlackhole,
			types.ConditionTrue,
			"IPv6FirewallBlackhole",
			fmt.Sprintf("IPv6 firewall (%s backend) appears to black-hole IPv6 traffic: %s", backend, finding),
		))
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"IPv6FirewallBlackhole",
			fmt.Sprintf("IPv6 firewall (%s backend) appears to black-hole IPv6 traffic: %s. "+
				"If this cluster expects IPv6 connectivity, IPv6 pod networking may be broken. "+
				"This monitor is detection-only and does not modify firewall rules.", backend, finding),
		))
		return
	}

	status.AddCondition(types.NewCondition(
		conditionIPv6FirewallBlackhole,
		types.ConditionFalse,
		"IPv6FirewallBlackholeNotExpected",
		fmt.Sprintf("IPv6 firewall (%s backend) black-holes IPv6 traffic (%s); expectIPv6Enabled=false so no action required",
			backend, finding),
	))
	status.AddEvent(types.NewEvent(
		types.EventInfo,
		"IPv6FirewallBlackholeNotExpected",
		fmt.Sprintf("IPv6 firewall (%s backend) black-holes IPv6 traffic (%s); expectIPv6Enabled=false so no action required",
			backend, finding),
	))
}

// recordBlackholeAbsent records the black-hole condition as False with the
// supplied reason/message. Used when the monitor cannot confirm a problem
// (tool missing, ruleset unreadable).
func (m *IPv6FirewallMonitor) recordBlackholeAbsent(status *types.Status, reason, message string) {
	status.AddCondition(types.NewCondition(
		conditionIPv6FirewallBlackhole,
		types.ConditionFalse,
		reason,
		message,
	))
}

// evaluateNFTRuleset applies the black-hole heuristic to `nft list ruleset`
// output. It returns true (with the offending chain names) only when every
// inet/ip6 base chain of type filter with hook input/forward/output has a
// "policy drop" (or reject) and the ruleset contains no "accept" verdict.
//
// The heuristic is conservative: presence of any accept rule anywhere clears
// the finding, and chains are matched on hook name so this works for the common
// `table inet filter` and `table ip6 filter` layouts.
func evaluateNFTRuleset(ruleset string) (blackholed bool, droppedChains []string) {
	hooksSeen := map[string]bool{}
	hooksDropped := map[string]bool{}
	hasAccept := false

	var (
		inChain      bool
		chainHook    string
		chainDropped bool
	)

	flush := func() {
		if inChain && chainHook != "" {
			hooksSeen[chainHook] = true
			if chainDropped {
				hooksDropped[chainHook] = true
			}
		}
		inChain = false
		chainHook = ""
		chainDropped = false
	}

	for _, raw := range strings.Split(ruleset, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}

		// A new chain block begins with "chain <name> {".
		if strings.HasPrefix(line, "chain ") && strings.HasSuffix(line, "{") {
			flush()
			inChain = true
			continue
		}
		if line == "}" {
			flush()
			continue
		}

		// Any accept verdict (rule or policy) clears the black-hole finding.
		if strings.Contains(line, "accept") {
			hasAccept = true
		}

		if !inChain {
			continue
		}

		// Base chain declaration: "type filter hook input priority 0; policy drop;"
		if strings.Contains(line, "hook ") {
			for _, hook := range []string{"input", "forward", "output"} {
				if strings.Contains(line, "hook "+hook) {
					chainHook = hook
				}
			}
		}
		if strings.Contains(line, "policy drop") || strings.Contains(line, "policy reject") {
			chainDropped = true
		}
	}
	flush()

	return blackholeFromChainMap(hooksSeen, hooksDropped, hasAccept, []string{"input", "forward", "output"})
}

// evaluateIP6TablesRuleset applies the black-hole heuristic to `ip6tables -S`
// output (the filter table). It returns true only when the default policy for
// INPUT, FORWARD and OUTPUT is all DROP/REJECT and no "-A <chain> ... -j ACCEPT"
// rule exists.
func evaluateIP6TablesRuleset(ruleset string) (blackholed bool, droppedChains []string) {
	policy := map[string]string{}
	hasAccept := false

	for _, raw := range strings.Split(ruleset, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}

		fields := strings.Fields(line)

		// Policy line: "-P INPUT DROP"
		if len(fields) >= 3 && fields[0] == "-P" {
			policy[fields[1]] = strings.ToUpper(fields[2])
			continue
		}

		// Append rule: "-A INPUT ... -j ACCEPT"
		if len(fields) >= 2 && fields[0] == "-A" {
			if strings.Contains(line, "-j ACCEPT") || strings.Contains(line, "--jump ACCEPT") {
				hasAccept = true
			}
		}
	}

	seen := map[string]bool{}
	dropped := map[string]bool{}
	for _, chain := range ipv6FilterChains {
		if pol, ok := policy[chain]; ok {
			seen[chain] = true
			if pol == "DROP" || pol == "REJECT" {
				dropped[chain] = true
			}
		}
	}

	return blackholeFromChainMap(seen, dropped, hasAccept, ipv6FilterChains)
}

// blackholeFromChainMap returns the conservative black-hole verdict: true only
// when at least one of the target chains was observed, every observed target
// chain has a DROP/REJECT policy, all target chains were observed, and the
// ruleset contains no ACCEPT verdict. droppedChains lists the offending chains
// in canonical order.
func blackholeFromChainMap(seen, dropped map[string]bool, hasAccept bool, order []string) (bool, []string) {
	if hasAccept {
		return false, nil
	}

	var droppedChains []string
	allSeenAndDropped := true
	for _, chain := range order {
		if !seen[chain] {
			allSeenAndDropped = false
			continue
		}
		if dropped[chain] {
			droppedChains = append(droppedChains, chain)
		} else {
			allSeenAndDropped = false
		}
	}

	// Require every target chain to be present and dropping; a partially
	// observed ruleset is treated as inconclusive to avoid false positives.
	if !allSeenAndDropped || len(droppedChains) != len(order) {
		return false, nil
	}

	return true, droppedChains
}
