package network

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// fakeFirewallExecutor is a test double for CommandExecutor. It returns canned
// LookPath results and command output so tests never exec real ip6tables/nft.
type fakeFirewallExecutor struct {
	// present maps binary name -> whether LookPath should succeed.
	present map[string]bool
	// output maps "name args..." -> canned combined output.
	output map[string]string
	// runErr maps "name args..." -> error to return from Run.
	runErr map[string]error
	// calls records every Run invocation as "name args...".
	calls []string
}

func newFakeFirewallExecutor() *fakeFirewallExecutor {
	return &fakeFirewallExecutor{
		present: map[string]bool{},
		output:  map[string]string{},
		runErr:  map[string]error{},
	}
}

func (f *fakeFirewallExecutor) LookPath(name string) (string, error) {
	if f.present[name] {
		return "/usr/sbin/" + name, nil
	}
	return "", errors.New("exec: \"" + name + "\": executable file not found in $PATH")
}

func (f *fakeFirewallExecutor) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	key := name
	if len(args) > 0 {
		key = name + " " + strings.Join(args, " ")
	}
	f.calls = append(f.calls, key)
	if err, ok := f.runErr[key]; ok {
		return nil, err
	}
	return []byte(f.output[key]), nil
}

// newTestFirewallMonitor builds a monitor with the supplied config and fake
// executor for direct check-function invocation.
func newTestFirewallMonitor(t *testing.T, cfg *IPv6FirewallConfig, exec CommandExecutor) *IPv6FirewallMonitor {
	t.Helper()
	monitor, err := NewIPv6FirewallMonitor(context.Background(), types.MonitorConfig{
		Name:     "test-ipv6-firewall",
		Type:     "network-ipv6-firewall",
		Interval: 60 * time.Second,
		Timeout:  5 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewIPv6FirewallMonitor() unexpected error: %v", err)
	}
	m := monitor.(*IPv6FirewallMonitor)
	if cfg != nil {
		m.config = cfg
	}
	if exec != nil {
		m.SetCommandExecutor(exec)
	}
	return m
}

// findFirewallCondition returns the black-hole condition or nil.
func findFirewallCondition(status *types.Status) *types.Condition {
	for i := range status.Conditions {
		if status.Conditions[i].Type == conditionIPv6FirewallBlackhole {
			return &status.Conditions[i]
		}
	}
	return nil
}

func hasEventSeverity(status *types.Status, sev types.EventSeverity) bool {
	for i := range status.Events {
		if status.Events[i].Severity == sev {
			return true
		}
	}
	return false
}

func TestParseIPv6FirewallConfigDefaults(t *testing.T) {
	cfg, err := parseIPv6FirewallConfig(nil)
	if err != nil {
		t.Fatalf("parseIPv6FirewallConfig(nil) error: %v", err)
	}
	if !cfg.ExpectIPv6Enabled {
		t.Errorf("ExpectIPv6Enabled default = false, want true")
	}
	if cfg.Backend != ipv6FirewallBackendAuto {
		t.Errorf("Backend default = %q, want %q", cfg.Backend, ipv6FirewallBackendAuto)
	}
}

func TestParseIPv6FirewallConfigValues(t *testing.T) {
	cfg, err := parseIPv6FirewallConfig(map[string]any{
		"expectIPv6Enabled": false,
		"backend":           "nft",
	})
	if err != nil {
		t.Fatalf("parseIPv6FirewallConfig() error: %v", err)
	}
	if cfg.ExpectIPv6Enabled {
		t.Errorf("ExpectIPv6Enabled = true, want false")
	}
	if cfg.Backend != ipv6FirewallBackendNFT {
		t.Errorf("Backend = %q, want %q", cfg.Backend, ipv6FirewallBackendNFT)
	}
}

func TestParseIPv6FirewallConfigInvalid(t *testing.T) {
	tests := []struct {
		name      string
		configMap map[string]any
	}{
		{"invalid backend", map[string]any{"backend": "iptables"}},
		{"backend wrong type", map[string]any{"backend": 5}},
		{"expect wrong type", map[string]any{"expectIPv6Enabled": "yes"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseIPv6FirewallConfig(tt.configMap); err == nil {
				t.Errorf("parseIPv6FirewallConfig(%v) expected error, got nil", tt.configMap)
			}
		})
	}
}

func TestValidateIPv6FirewallConfig(t *testing.T) {
	if err := ValidateIPv6FirewallConfig(types.MonitorConfig{
		Config: map[string]any{"backend": "ip6tables"},
	}); err != nil {
		t.Errorf("ValidateIPv6FirewallConfig() valid config error: %v", err)
	}
	if err := ValidateIPv6FirewallConfig(types.MonitorConfig{
		Config: map[string]any{"backend": "bogus"},
	}); err == nil {
		t.Errorf("ValidateIPv6FirewallConfig() invalid backend expected error")
	}
}

func TestNewIPv6FirewallMonitor(t *testing.T) {
	monitor, err := NewIPv6FirewallMonitor(context.Background(), types.MonitorConfig{
		Name:     "fw",
		Type:     "network-ipv6-firewall",
		Interval: 60 * time.Second,
		Timeout:  5 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewIPv6FirewallMonitor() error: %v", err)
	}
	fw, ok := monitor.(*IPv6FirewallMonitor)
	if !ok {
		t.Fatalf("NewIPv6FirewallMonitor returned wrong type")
	}
	if fw.GetName() != "fw" {
		t.Errorf("GetName() = %q, want %q", fw.GetName(), "fw")
	}
}

func TestNewIPv6FirewallMonitorInvalidConfig(t *testing.T) {
	_, err := NewIPv6FirewallMonitor(context.Background(), types.MonitorConfig{
		Name:     "fw",
		Type:     "network-ipv6-firewall",
		Interval: 60 * time.Second,
		Timeout:  5 * time.Second,
		Config:   map[string]any{"backend": "nope"},
	})
	if err == nil {
		t.Fatalf("NewIPv6FirewallMonitor() expected error for invalid backend")
	}
}

const healthyIP6TablesOutput = `-P INPUT ACCEPT
-P FORWARD ACCEPT
-P OUTPUT ACCEPT
-A INPUT -p ipv6-icmp -j ACCEPT`

const blackholeIP6TablesOutput = `-P INPUT DROP
-P FORWARD DROP
-P OUTPUT DROP`

const partialDropIP6TablesOutput = `-P INPUT DROP
-P FORWARD ACCEPT
-P OUTPUT DROP`

const healthyNFTOutput = `table inet filter {
	chain input {
		type filter hook input priority 0; policy drop;
		ct state established,related accept
	}
	chain forward {
		type filter hook forward priority 0; policy drop;
	}
	chain output {
		type filter hook output priority 0; policy accept;
	}
}`

const blackholeNFTOutput = `table inet filter {
	chain input {
		type filter hook input priority 0; policy drop;
	}
	chain forward {
		type filter hook forward priority 0; policy drop;
	}
	chain output {
		type filter hook output priority 0; policy drop;
	}
}`

func TestCheckIPv6FirewallHealthyIP6Tables(t *testing.T) {
	exec := newFakeFirewallExecutor()
	exec.present[ip6tablesBinary] = true
	exec.output["ip6tables -S"] = healthyIP6TablesOutput

	m := newTestFirewallMonitor(t, &IPv6FirewallConfig{ExpectIPv6Enabled: true, Backend: ipv6FirewallBackendIP6Tables}, exec)
	status, err := m.checkIPv6Firewall(context.Background())
	if err != nil {
		t.Fatalf("checkIPv6Firewall() error: %v", err)
	}
	cond := findFirewallCondition(status)
	if cond == nil || cond.Status != types.ConditionFalse {
		t.Fatalf("expected condition False, got %+v", cond)
	}
	// Confirm only the read-only -S verb was issued.
	for _, c := range exec.calls {
		if !strings.HasPrefix(c, "ip6tables -S") {
			t.Errorf("unexpected command issued: %q", c)
		}
	}
}

func TestCheckIPv6FirewallBlackholeIP6Tables(t *testing.T) {
	exec := newFakeFirewallExecutor()
	exec.present[ip6tablesBinary] = true
	exec.output["ip6tables -S"] = blackholeIP6TablesOutput

	m := newTestFirewallMonitor(t, &IPv6FirewallConfig{ExpectIPv6Enabled: true, Backend: ipv6FirewallBackendIP6Tables}, exec)
	status, err := m.checkIPv6Firewall(context.Background())
	if err != nil {
		t.Fatalf("checkIPv6Firewall() error: %v", err)
	}
	cond := findFirewallCondition(status)
	if cond == nil || cond.Status != types.ConditionTrue {
		t.Fatalf("expected condition True (blackhole), got %+v", cond)
	}
	if !hasEventReason(status, "IPv6FirewallBlackhole") {
		t.Errorf("expected IPv6FirewallBlackhole event")
	}
	if !hasEventSeverity(status, types.EventWarning) {
		t.Errorf("expected a warning event")
	}
}

func TestCheckIPv6FirewallBlackholeSuppressedWhenNotExpected(t *testing.T) {
	exec := newFakeFirewallExecutor()
	exec.present[ip6tablesBinary] = true
	exec.output["ip6tables -S"] = blackholeIP6TablesOutput

	m := newTestFirewallMonitor(t, &IPv6FirewallConfig{ExpectIPv6Enabled: false, Backend: ipv6FirewallBackendIP6Tables}, exec)
	status, err := m.checkIPv6Firewall(context.Background())
	if err != nil {
		t.Fatalf("checkIPv6Firewall() error: %v", err)
	}
	cond := findFirewallCondition(status)
	if cond == nil || cond.Status != types.ConditionFalse {
		t.Fatalf("expected condition False (suppressed), got %+v", cond)
	}
	if hasEventSeverity(status, types.EventWarning) {
		t.Errorf("expected no warning event when expectIPv6Enabled=false")
	}
	if !hasEventReason(status, "IPv6FirewallBlackholeNotExpected") {
		t.Errorf("expected IPv6FirewallBlackholeNotExpected event")
	}
}

func TestCheckIPv6FirewallPartialDropNotBlackhole(t *testing.T) {
	exec := newFakeFirewallExecutor()
	exec.present[ip6tablesBinary] = true
	exec.output["ip6tables -S"] = partialDropIP6TablesOutput

	m := newTestFirewallMonitor(t, &IPv6FirewallConfig{ExpectIPv6Enabled: true, Backend: ipv6FirewallBackendIP6Tables}, exec)
	status, err := m.checkIPv6Firewall(context.Background())
	if err != nil {
		t.Fatalf("checkIPv6Firewall() error: %v", err)
	}
	cond := findFirewallCondition(status)
	if cond == nil || cond.Status != types.ConditionFalse {
		t.Fatalf("expected condition False (partial drop is not a blackhole), got %+v", cond)
	}
}

func TestCheckIPv6FirewallHealthyNFT(t *testing.T) {
	exec := newFakeFirewallExecutor()
	exec.present[nftBinary] = true
	exec.output["nft list ruleset"] = healthyNFTOutput

	m := newTestFirewallMonitor(t, &IPv6FirewallConfig{ExpectIPv6Enabled: true, Backend: ipv6FirewallBackendNFT}, exec)
	status, err := m.checkIPv6Firewall(context.Background())
	if err != nil {
		t.Fatalf("checkIPv6Firewall() error: %v", err)
	}
	cond := findFirewallCondition(status)
	if cond == nil || cond.Status != types.ConditionFalse {
		t.Fatalf("expected condition False, got %+v", cond)
	}
	for _, c := range exec.calls {
		if c != "nft list ruleset" {
			t.Errorf("unexpected command issued: %q", c)
		}
	}
}

func TestCheckIPv6FirewallBlackholeNFT(t *testing.T) {
	exec := newFakeFirewallExecutor()
	exec.present[nftBinary] = true
	exec.output["nft list ruleset"] = blackholeNFTOutput

	m := newTestFirewallMonitor(t, &IPv6FirewallConfig{ExpectIPv6Enabled: true, Backend: ipv6FirewallBackendNFT}, exec)
	status, err := m.checkIPv6Firewall(context.Background())
	if err != nil {
		t.Fatalf("checkIPv6Firewall() error: %v", err)
	}
	cond := findFirewallCondition(status)
	if cond == nil || cond.Status != types.ConditionTrue {
		t.Fatalf("expected condition True (nft blackhole), got %+v", cond)
	}
	if !hasEventReason(status, "IPv6FirewallBlackhole") {
		t.Errorf("expected IPv6FirewallBlackhole event")
	}
}

func TestCheckIPv6FirewallAutoPrefersNFT(t *testing.T) {
	exec := newFakeFirewallExecutor()
	exec.present[nftBinary] = true
	exec.present[ip6tablesBinary] = true
	exec.output["nft list ruleset"] = healthyNFTOutput

	m := newTestFirewallMonitor(t, &IPv6FirewallConfig{ExpectIPv6Enabled: true, Backend: ipv6FirewallBackendAuto}, exec)
	status, err := m.checkIPv6Firewall(context.Background())
	if err != nil {
		t.Fatalf("checkIPv6Firewall() error: %v", err)
	}
	if findFirewallCondition(status) == nil {
		t.Fatalf("expected a condition")
	}
	for _, c := range exec.calls {
		if strings.HasPrefix(c, "ip6tables") {
			t.Errorf("auto mode should prefer nft, but ip6tables was invoked: %q", c)
		}
	}
}

func TestCheckIPv6FirewallAutoFallsBackToIP6Tables(t *testing.T) {
	exec := newFakeFirewallExecutor()
	exec.present[ip6tablesBinary] = true // nft absent
	exec.output["ip6tables -S"] = healthyIP6TablesOutput

	m := newTestFirewallMonitor(t, &IPv6FirewallConfig{ExpectIPv6Enabled: true, Backend: ipv6FirewallBackendAuto}, exec)
	status, err := m.checkIPv6Firewall(context.Background())
	if err != nil {
		t.Fatalf("checkIPv6Firewall() error: %v", err)
	}
	if findFirewallCondition(status) == nil {
		t.Fatalf("expected a condition")
	}
	ranIP6Tables := false
	for _, c := range exec.calls {
		if strings.HasPrefix(c, "ip6tables -S") {
			ranIP6Tables = true
		}
	}
	if !ranIP6Tables {
		t.Errorf("auto mode should fall back to ip6tables -S; calls=%v", exec.calls)
	}
}

func TestCheckIPv6FirewallToolNotFound(t *testing.T) {
	exec := newFakeFirewallExecutor() // nothing present

	m := newTestFirewallMonitor(t, &IPv6FirewallConfig{ExpectIPv6Enabled: true, Backend: ipv6FirewallBackendAuto}, exec)
	status, err := m.checkIPv6Firewall(context.Background())
	if err != nil {
		t.Fatalf("checkIPv6Firewall() should not hard error when tools absent: %v", err)
	}
	if !hasEventReason(status, "IPv6FirewallToolNotFound") {
		t.Errorf("expected IPv6FirewallToolNotFound event")
	}
	if !hasEventSeverity(status, types.EventWarning) {
		t.Errorf("expected warning severity for missing tool")
	}
	cond := findFirewallCondition(status)
	if cond == nil || cond.Status != types.ConditionFalse {
		t.Fatalf("expected condition False when tools absent, got %+v", cond)
	}
}

func TestCheckIPv6FirewallForcedBackendMissingBinary(t *testing.T) {
	exec := newFakeFirewallExecutor() // nft forced but absent

	m := newTestFirewallMonitor(t, &IPv6FirewallConfig{ExpectIPv6Enabled: true, Backend: ipv6FirewallBackendNFT}, exec)
	status, err := m.checkIPv6Firewall(context.Background())
	if err != nil {
		t.Fatalf("checkIPv6Firewall() error: %v", err)
	}
	if !hasEventReason(status, "IPv6FirewallToolNotFound") {
		t.Errorf("expected IPv6FirewallToolNotFound event for forced-but-missing nft")
	}
	cond := findFirewallCondition(status)
	if cond == nil || cond.Status != types.ConditionFalse {
		t.Fatalf("expected condition False, got %+v", cond)
	}
}

func TestCheckIPv6FirewallPermissionDenied(t *testing.T) {
	exec := newFakeFirewallExecutor()
	exec.present[ip6tablesBinary] = true
	exec.runErr["ip6tables -S"] = errors.New("Permission denied (you must be root)")

	m := newTestFirewallMonitor(t, &IPv6FirewallConfig{ExpectIPv6Enabled: true, Backend: ipv6FirewallBackendIP6Tables}, exec)
	status, err := m.checkIPv6Firewall(context.Background())
	if err != nil {
		t.Fatalf("checkIPv6Firewall() should not hard error on read failure: %v", err)
	}
	if !hasEventReason(status, "IPv6FirewallReadError") {
		t.Errorf("expected IPv6FirewallReadError event")
	}
	if !hasEventSeverity(status, types.EventWarning) {
		t.Errorf("expected warning severity for read error")
	}
	cond := findFirewallCondition(status)
	if cond == nil || cond.Status != types.ConditionFalse {
		t.Fatalf("expected condition False on read error, got %+v", cond)
	}
}

func TestCheckIPv6FirewallNFTReadError(t *testing.T) {
	exec := newFakeFirewallExecutor()
	exec.present[nftBinary] = true
	exec.runErr["nft list ruleset"] = errors.New("Operation not permitted")

	m := newTestFirewallMonitor(t, &IPv6FirewallConfig{ExpectIPv6Enabled: true, Backend: ipv6FirewallBackendNFT}, exec)
	status, err := m.checkIPv6Firewall(context.Background())
	if err != nil {
		t.Fatalf("checkIPv6Firewall() should not hard error: %v", err)
	}
	if !hasEventReason(status, "IPv6FirewallReadError") {
		t.Errorf("expected IPv6FirewallReadError event")
	}
}

func TestEvaluateIP6TablesRuleset(t *testing.T) {
	tests := []struct {
		name       string
		ruleset    string
		blackholed bool
	}{
		{"all drop no accept", blackholeIP6TablesOutput, true},
		{"has accept rule", healthyIP6TablesOutput, false},
		{"partial drop", partialDropIP6TablesOutput, false},
		{"empty ruleset", "", false},
		{"reject policy", "-P INPUT REJECT\n-P FORWARD REJECT\n-P OUTPUT REJECT", true},
		{"only input observed", "-P INPUT DROP", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := evaluateIP6TablesRuleset(tt.ruleset)
			if got != tt.blackholed {
				t.Errorf("evaluateIP6TablesRuleset() = %v, want %v", got, tt.blackholed)
			}
		})
	}
}

func TestEvaluateNFTRuleset(t *testing.T) {
	tests := []struct {
		name       string
		ruleset    string
		blackholed bool
	}{
		{"all drop no accept", blackholeNFTOutput, true},
		{"has accept somewhere", healthyNFTOutput, false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := evaluateNFTRuleset(tt.ruleset)
			if got != tt.blackholed {
				t.Errorf("evaluateNFTRuleset() = %v, want %v", got, tt.blackholed)
			}
		})
	}
}
