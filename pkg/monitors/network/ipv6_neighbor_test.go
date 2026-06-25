package network

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

func TestParseIPv6NeighborConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]any
		want    *IPv6NeighborConfig
		wantErr bool
	}{
		{
			name:   "nil config - use defaults",
			config: nil,
			want: &IPv6NeighborConfig{
				ExpectIPv6Enabled:    defaultIPv6NeighborExpectEnabled,
				CheckPerInterface:    defaultIPv6NeighborCheckPerIface,
				RequireGlobalAddress: defaultIPv6NeighborRequireGlobal,
				ProcPath:             defaultIPv6NeighborProcPath,
				SkipInterfaces:       defaultIPv6NeighborSkipInterfaces,
			},
		},
		{
			name:   "empty config - use defaults",
			config: map[string]any{},
			want: &IPv6NeighborConfig{
				ExpectIPv6Enabled:    defaultIPv6NeighborExpectEnabled,
				CheckPerInterface:    defaultIPv6NeighborCheckPerIface,
				RequireGlobalAddress: defaultIPv6NeighborRequireGlobal,
				ProcPath:             defaultIPv6NeighborProcPath,
				SkipInterfaces:       defaultIPv6NeighborSkipInterfaces,
			},
		},
		{
			name: "custom values",
			config: map[string]any{
				"expectIPv6Enabled":    false,
				"checkPerInterface":    false,
				"requireGlobalAddress": true,
				"procPath":             "/host/proc",
			},
			want: &IPv6NeighborConfig{
				ExpectIPv6Enabled:    false,
				CheckPerInterface:    false,
				RequireGlobalAddress: true,
				ProcPath:             "/host/proc",
				SkipInterfaces:       defaultIPv6NeighborSkipInterfaces,
			},
		},
		{
			name: "interfaces and skipInterfaces",
			config: map[string]any{
				"interfaces":     []any{"eth0", "eth1"},
				"skipInterfaces": []string{"lo"},
			},
			want: &IPv6NeighborConfig{
				ExpectIPv6Enabled:    defaultIPv6NeighborExpectEnabled,
				CheckPerInterface:    defaultIPv6NeighborCheckPerIface,
				RequireGlobalAddress: defaultIPv6NeighborRequireGlobal,
				ProcPath:             defaultIPv6NeighborProcPath,
				Interfaces:           []string{"eth0", "eth1"},
				SkipInterfaces:       []string{"lo"},
			},
		},
		{name: "invalid expectIPv6Enabled", config: map[string]any{"expectIPv6Enabled": "yes"}, wantErr: true},
		{name: "invalid checkPerInterface", config: map[string]any{"checkPerInterface": 1}, wantErr: true},
		{name: "invalid requireGlobalAddress", config: map[string]any{"requireGlobalAddress": "no"}, wantErr: true},
		{name: "invalid procPath", config: map[string]any{"procPath": 123}, wantErr: true},
		{name: "invalid interfaces type", config: map[string]any{"interfaces": "eth0"}, wantErr: true},
		{name: "invalid interfaces element", config: map[string]any{"interfaces": []any{123}}, wantErr: true},
		{name: "invalid skipInterfaces element", config: map[string]any{"skipInterfaces": []any{true}}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseIPv6NeighborConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseIPv6NeighborConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if got.ExpectIPv6Enabled != tt.want.ExpectIPv6Enabled {
				t.Errorf("ExpectIPv6Enabled = %v, want %v", got.ExpectIPv6Enabled, tt.want.ExpectIPv6Enabled)
			}
			if got.CheckPerInterface != tt.want.CheckPerInterface {
				t.Errorf("CheckPerInterface = %v, want %v", got.CheckPerInterface, tt.want.CheckPerInterface)
			}
			if got.RequireGlobalAddress != tt.want.RequireGlobalAddress {
				t.Errorf("RequireGlobalAddress = %v, want %v", got.RequireGlobalAddress, tt.want.RequireGlobalAddress)
			}
			if got.ProcPath != tt.want.ProcPath {
				t.Errorf("ProcPath = %v, want %v", got.ProcPath, tt.want.ProcPath)
			}
			if !equalStringSlice(got.Interfaces, tt.want.Interfaces) {
				t.Errorf("Interfaces = %v, want %v", got.Interfaces, tt.want.Interfaces)
			}
			if !equalStringSlice(got.SkipInterfaces, tt.want.SkipInterfaces) {
				t.Errorf("SkipInterfaces = %v, want %v", got.SkipInterfaces, tt.want.SkipInterfaces)
			}
		})
	}
}

func TestValidateIPv6NeighborConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]any
		wantErr bool
	}{
		{name: "valid config", config: map[string]any{"expectIPv6Enabled": true}, wantErr: false},
		{name: "invalid config", config: map[string]any{"requireGlobalAddress": "yes"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := types.MonitorConfig{
				Name:     "test-ipv6-neighbor",
				Type:     "network-ipv6-neighbor",
				Interval: 60 * time.Second,
				Timeout:  5 * time.Second,
				Config:   tt.config,
			}
			if err := ValidateIPv6NeighborConfig(cfg); (err != nil) != tt.wantErr {
				t.Errorf("ValidateIPv6NeighborConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewIPv6NeighborMonitor(t *testing.T) {
	tests := []struct {
		name    string
		config  types.MonitorConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: types.MonitorConfig{
				Name:     "test-ipv6-neighbor",
				Type:     "network-ipv6-neighbor",
				Interval: 60 * time.Second,
				Timeout:  5 * time.Second,
				Config:   map[string]any{"expectIPv6Enabled": true},
			},
			wantErr: false,
		},
		{
			name: "invalid config - bad type",
			config: types.MonitorConfig{
				Name:     "test-ipv6-neighbor",
				Type:     "network-ipv6-neighbor",
				Interval: 60 * time.Second,
				Timeout:  5 * time.Second,
				Config:   map[string]any{"expectIPv6Enabled": "invalid"},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monitor, err := NewIPv6NeighborMonitor(context.Background(), tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewIPv6NeighborMonitor() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && monitor == nil {
				t.Error("NewIPv6NeighborMonitor() returned nil monitor")
			}
		})
	}
}

// mockIfInet6Addr describes one address to write into a mock if_inet6 file.
type mockIfInet6Addr struct {
	addr      string // 32 hex chars; defaults to a filler if empty
	ifindex   string // hex, defaults to "01"
	prefixlen string // hex, defaults to "40"
	scope     string // hex scope, e.g. "20" (link-local) or "00" (global)
	flags     string // hex, defaults to "80"
	dev       string // device name
}

// writeMockNeighborProcFS builds a mock proc tree:
//   - <proc>/net/if_inet6 from the supplied addresses (skipped if addrs is nil)
//   - <proc>/sys/net/ipv6/conf/<iface>/{accept_ra,autoconf} from raConf
//
// raConf maps interface name -> {accept_ra, autoconf} string values; an empty
// value skips that file.
func writeMockNeighborProcFS(t *testing.T, addrs []mockIfInet6Addr, writeIfInet6 bool, raConf map[string][2]string) string {
	t.Helper()
	procDir := t.TempDir()

	if writeIfInet6 {
		netDir := filepath.Join(procDir, "net")
		if err := os.MkdirAll(netDir, 0755); err != nil {
			t.Fatalf("mkdir net: %v", err)
		}
		var b []byte
		for _, a := range addrs {
			addr := a.addr
			if addr == "" {
				addr = "fe800000000000000000000000000001"
			}
			ifindex := a.ifindex
			if ifindex == "" {
				ifindex = "01"
			}
			prefixlen := a.prefixlen
			if prefixlen == "" {
				prefixlen = "40"
			}
			flags := a.flags
			if flags == "" {
				flags = "80"
			}
			line := addr + " " + ifindex + " " + prefixlen + " " + a.scope + " " + flags + " " + a.dev + "\n"
			b = append(b, []byte(line)...)
		}
		if err := os.WriteFile(filepath.Join(netDir, "if_inet6"), b, 0644); err != nil {
			t.Fatalf("write if_inet6: %v", err)
		}
	}

	for iface, vals := range raConf {
		dir := filepath.Join(procDir, "sys", "net", "ipv6", "conf", iface)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir conf/%s: %v", iface, err)
		}
		if vals[0] != "" {
			if err := os.WriteFile(filepath.Join(dir, "accept_ra"), []byte(vals[0]+"\n"), 0644); err != nil {
				t.Fatalf("write accept_ra: %v", err)
			}
		}
		if vals[1] != "" {
			if err := os.WriteFile(filepath.Join(dir, "autoconf"), []byte(vals[1]+"\n"), 0644); err != nil {
				t.Fatalf("write autoconf: %v", err)
			}
		}
	}

	return procDir
}

// findNeighborCondition returns the named condition, or nil if absent.
func findNeighborCondition(status *types.Status, condType string) *types.Condition {
	for i := range status.Conditions {
		if status.Conditions[i].Type == condType {
			return &status.Conditions[i]
		}
	}
	return nil
}

func TestCheckIPv6Neighbor_Healthy(t *testing.T) {
	addrs := []mockIfInet6Addr{
		{scope: "20", dev: "eth0"}, // link-local
		{addr: "20010db8000000000000000000000001", scope: "00", dev: "eth0"}, // global
	}
	raConf := map[string][2]string{"eth0": {"1", "1"}}
	procDir := writeMockNeighborProcFS(t, addrs, true, raConf)

	m := &IPv6NeighborMonitor{
		name: "test",
		config: &IPv6NeighborConfig{
			ExpectIPv6Enabled:    true,
			CheckPerInterface:    true,
			RequireGlobalAddress: true,
			SkipInterfaces:       defaultIPv6NeighborSkipInterfaces,
			ProcPath:             procDir,
		},
	}

	status, err := m.checkIPv6Neighbor(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, ct := range []string{conditionIPv6LinkLocalMissing, conditionIPv6GlobalMissing, conditionIPv6RADisabled} {
		cond := findNeighborCondition(status, ct)
		if cond == nil {
			t.Fatalf("missing condition %s", ct)
		}
		if cond.Status != types.ConditionFalse {
			t.Errorf("condition %s = %s, want False", ct, cond.Status)
		}
	}
}

func TestCheckIPv6Neighbor_LinkLocalMissing(t *testing.T) {
	addrs := []mockIfInet6Addr{
		{addr: "20010db8000000000000000000000001", scope: "00", dev: "eth0"}, // global only
	}
	procDir := writeMockNeighborProcFS(t, addrs, true, map[string][2]string{"eth0": {"1", "1"}})

	m := &IPv6NeighborMonitor{
		name:   "test",
		config: &IPv6NeighborConfig{ExpectIPv6Enabled: true, CheckPerInterface: true, SkipInterfaces: defaultIPv6NeighborSkipInterfaces, ProcPath: procDir},
	}

	status, err := m.checkIPv6Neighbor(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cond := findNeighborCondition(status, conditionIPv6LinkLocalMissing)
	if cond == nil || cond.Status != types.ConditionTrue {
		t.Fatalf("expected IPv6LinkLocalMissing=True, got %+v", cond)
	}
	if !hasEventReason(status, "IPv6LinkLocalMissing") {
		t.Error("expected IPv6LinkLocalMissing event")
	}
}

func TestCheckIPv6Neighbor_GlobalMissingRequired(t *testing.T) {
	addrs := []mockIfInet6Addr{{scope: "20", dev: "eth0"}} // link-local only
	procDir := writeMockNeighborProcFS(t, addrs, true, map[string][2]string{"eth0": {"1", "1"}})

	m := &IPv6NeighborMonitor{
		name: "test",
		config: &IPv6NeighborConfig{
			ExpectIPv6Enabled: true, CheckPerInterface: true, RequireGlobalAddress: true,
			SkipInterfaces: defaultIPv6NeighborSkipInterfaces, ProcPath: procDir,
		},
	}

	status, err := m.checkIPv6Neighbor(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cond := findNeighborCondition(status, conditionIPv6GlobalMissing)
	if cond == nil || cond.Status != types.ConditionTrue {
		t.Fatalf("expected IPv6GlobalAddressMissing=True, got %+v", cond)
	}
	if !hasEventReason(status, "IPv6GlobalAddressMissing") {
		t.Error("expected IPv6GlobalAddressMissing event")
	}
	// Link-local present, so that condition stays False.
	if ll := findNeighborCondition(status, conditionIPv6LinkLocalMissing); ll == nil || ll.Status != types.ConditionFalse {
		t.Errorf("expected IPv6LinkLocalMissing=False, got %+v", ll)
	}
}

func TestCheckIPv6Neighbor_GlobalMissingNotRequiredSuppressed(t *testing.T) {
	addrs := []mockIfInet6Addr{{scope: "20", dev: "eth0"}} // link-local only
	procDir := writeMockNeighborProcFS(t, addrs, true, map[string][2]string{"eth0": {"1", "1"}})

	m := &IPv6NeighborMonitor{
		name: "test",
		config: &IPv6NeighborConfig{
			ExpectIPv6Enabled: true, CheckPerInterface: true, RequireGlobalAddress: false,
			SkipInterfaces: defaultIPv6NeighborSkipInterfaces, ProcPath: procDir,
		},
	}

	status, err := m.checkIPv6Neighbor(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cond := findNeighborCondition(status, conditionIPv6GlobalMissing)
	if cond == nil || cond.Status != types.ConditionFalse {
		t.Fatalf("expected IPv6GlobalAddressMissing=False (not required), got %+v", cond)
	}
	if hasEventReason(status, "IPv6GlobalAddressMissing") {
		t.Error("did not expect IPv6GlobalAddressMissing event when requireGlobalAddress=false")
	}
}

func TestCheckIPv6Neighbor_AcceptRADisabled(t *testing.T) {
	addrs := []mockIfInet6Addr{
		{scope: "20", dev: "eth0"},
		{addr: "20010db8000000000000000000000001", scope: "00", dev: "eth0"},
	}
	// accept_ra=0 on eth0 with autoconf enabled.
	procDir := writeMockNeighborProcFS(t, addrs, true, map[string][2]string{"eth0": {"0", "1"}})

	m := &IPv6NeighborMonitor{
		name:   "test",
		config: &IPv6NeighborConfig{ExpectIPv6Enabled: true, CheckPerInterface: true, SkipInterfaces: defaultIPv6NeighborSkipInterfaces, ProcPath: procDir},
	}

	status, err := m.checkIPv6Neighbor(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cond := findNeighborCondition(status, conditionIPv6RADisabled)
	if cond == nil || cond.Status != types.ConditionTrue {
		t.Fatalf("expected IPv6RouterAdvertisementDisabled=True, got %+v", cond)
	}
	if !hasEventReason(status, "IPv6RouterAdvertisementDisabled") {
		t.Error("expected IPv6RouterAdvertisementDisabled event")
	}
}

func TestCheckIPv6Neighbor_AutoconfDisabled(t *testing.T) {
	addrs := []mockIfInet6Addr{
		{scope: "20", dev: "eth0"},
		{addr: "20010db8000000000000000000000001", scope: "00", dev: "eth0"},
	}
	// accept_ra=1 but autoconf=0.
	procDir := writeMockNeighborProcFS(t, addrs, true, map[string][2]string{"eth0": {"1", "0"}})

	m := &IPv6NeighborMonitor{
		name:   "test",
		config: &IPv6NeighborConfig{ExpectIPv6Enabled: true, CheckPerInterface: true, SkipInterfaces: defaultIPv6NeighborSkipInterfaces, ProcPath: procDir},
	}

	status, err := m.checkIPv6Neighbor(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cond := findNeighborCondition(status, conditionIPv6RADisabled)
	if cond == nil || cond.Status != types.ConditionTrue {
		t.Fatalf("expected IPv6RouterAdvertisementDisabled=True (autoconf=0), got %+v", cond)
	}
}

func TestCheckIPv6Neighbor_AcceptRADisabledExpectFalse(t *testing.T) {
	addrs := []mockIfInet6Addr{{scope: "20", dev: "eth0"}}
	procDir := writeMockNeighborProcFS(t, addrs, true, map[string][2]string{"eth0": {"0", "0"}})

	m := &IPv6NeighborMonitor{
		name:   "test",
		config: &IPv6NeighborConfig{ExpectIPv6Enabled: false, CheckPerInterface: true, SkipInterfaces: defaultIPv6NeighborSkipInterfaces, ProcPath: procDir},
	}

	status, err := m.checkIPv6Neighbor(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cond := findNeighborCondition(status, conditionIPv6RADisabled)
	if cond == nil || cond.Status != types.ConditionFalse {
		t.Fatalf("expected IPv6RouterAdvertisementDisabled=False (expectIPv6Enabled=false), got %+v", cond)
	}
	if !hasEventReason(status, "IPv6RouterAdvertisementDisabledExpected") {
		t.Error("expected IPv6RouterAdvertisementDisabledExpected info event")
	}
	if hasEventReason(status, "IPv6RouterAdvertisementDisabled") {
		t.Error("did not expect warning IPv6RouterAdvertisementDisabled when expectIPv6Enabled=false")
	}
}

func TestCheckIPv6Neighbor_SkipInterfacesRespected(t *testing.T) {
	addrs := []mockIfInet6Addr{
		{addr: "00000000000000000000000000000001", scope: "10", dev: "lo"}, // lo, skipped
		{scope: "20", dev: "eth0"},
		{addr: "20010db8000000000000000000000001", scope: "00", dev: "eth0"},
	}
	// lo has accept_ra=0 but is skipped; eth0 healthy.
	procDir := writeMockNeighborProcFS(t, addrs, true, map[string][2]string{
		"lo":   {"0", "0"},
		"eth0": {"1", "1"},
	})

	m := &IPv6NeighborMonitor{
		name:   "test",
		config: &IPv6NeighborConfig{ExpectIPv6Enabled: true, CheckPerInterface: true, SkipInterfaces: defaultIPv6NeighborSkipInterfaces, ProcPath: procDir},
	}

	status, err := m.checkIPv6Neighbor(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cond := findNeighborCondition(status, conditionIPv6RADisabled); cond == nil || cond.Status != types.ConditionFalse {
		t.Fatalf("expected IPv6RouterAdvertisementDisabled=False (lo skipped), got %+v", cond)
	}
	if cond := findNeighborCondition(status, conditionIPv6LinkLocalMissing); cond == nil || cond.Status != types.ConditionFalse {
		t.Fatalf("expected IPv6LinkLocalMissing=False (lo skipped), got %+v", cond)
	}
}

func TestCheckIPv6Neighbor_InterfacesFilter(t *testing.T) {
	addrs := []mockIfInet6Addr{
		{addr: "20010db8000000000000000000000099", scope: "00", dev: "eth0"}, // global only (would fail link-local) but filtered out
		{scope: "20", dev: "eth1"},
	}
	procDir := writeMockNeighborProcFS(t, addrs, true, map[string][2]string{
		"eth0": {"0", "0"}, // disabled but filtered out
		"eth1": {"1", "1"},
	})

	m := &IPv6NeighborMonitor{
		name: "test",
		config: &IPv6NeighborConfig{
			ExpectIPv6Enabled: true, CheckPerInterface: true,
			Interfaces:     []string{"eth1"},
			SkipInterfaces: defaultIPv6NeighborSkipInterfaces,
			ProcPath:       procDir,
		},
	}

	status, err := m.checkIPv6Neighbor(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cond := findNeighborCondition(status, conditionIPv6LinkLocalMissing); cond == nil || cond.Status != types.ConditionFalse {
		t.Fatalf("expected IPv6LinkLocalMissing=False (only eth1 checked), got %+v", cond)
	}
	if cond := findNeighborCondition(status, conditionIPv6RADisabled); cond == nil || cond.Status != types.ConditionFalse {
		t.Fatalf("expected IPv6RouterAdvertisementDisabled=False (eth0 filtered out), got %+v", cond)
	}
}

func TestCheckIPv6Neighbor_MissingIfInet6(t *testing.T) {
	// No if_inet6 written; accept_ra files present and healthy.
	procDir := writeMockNeighborProcFS(t, nil, false, map[string][2]string{"eth0": {"1", "1"}})

	m := &IPv6NeighborMonitor{
		name:   "test",
		config: &IPv6NeighborConfig{ExpectIPv6Enabled: true, CheckPerInterface: true, SkipInterfaces: defaultIPv6NeighborSkipInterfaces, ProcPath: procDir},
	}

	status, err := m.checkIPv6Neighbor(context.Background())
	if err != nil {
		t.Fatalf("unexpected error (should not hard error): %v", err)
	}

	if !hasEventReason(status, "IPv6IfInet6ReadError") {
		t.Error("expected IPv6IfInet6ReadError warning event")
	}
	for _, ev := range status.Events {
		if ev.Reason == "IPv6IfInet6ReadError" && ev.Severity != types.EventWarning {
			t.Errorf("expected Warning severity for IPv6IfInet6ReadError, got %s", ev.Severity)
		}
	}
	// Address conditions reported False (cannot confirm), RA condition healthy.
	if cond := findNeighborCondition(status, conditionIPv6LinkLocalMissing); cond == nil || cond.Status != types.ConditionFalse {
		t.Fatalf("expected IPv6LinkLocalMissing=False (unreadable), got %+v", cond)
	}
	if cond := findNeighborCondition(status, conditionIPv6RADisabled); cond == nil || cond.Status != types.ConditionFalse {
		t.Fatalf("expected IPv6RouterAdvertisementDisabled=False, got %+v", cond)
	}
}

func TestCheckIPv6Neighbor_NonexistentProcPath(t *testing.T) {
	m := &IPv6NeighborMonitor{
		name: "test",
		config: &IPv6NeighborConfig{
			ExpectIPv6Enabled: true, CheckPerInterface: true,
			SkipInterfaces: defaultIPv6NeighborSkipInterfaces,
			ProcPath:       "/nonexistent/proc",
		},
	}

	status, err := m.checkIPv6Neighbor(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !hasEventReason(status, "IPv6IfInet6ReadError") {
		t.Error("expected IPv6IfInet6ReadError event for nonexistent procPath")
	}
	// Glob over nonexistent path yields no matches -> RA condition reported
	// unreadable (False) with a warning.
	if cond := findNeighborCondition(status, conditionIPv6RADisabled); cond == nil || cond.Status != types.ConditionFalse {
		t.Fatalf("expected IPv6RouterAdvertisementDisabled=False, got %+v", cond)
	}
	if !hasEventReason(status, "IPv6AcceptRAReadError") {
		t.Error("expected IPv6AcceptRAReadError event for nonexistent procPath")
	}
}

func TestCheckIPv6Neighbor_PerInterfaceCheckDisabled(t *testing.T) {
	addrs := []mockIfInet6Addr{
		{scope: "20", dev: "eth0"},
		{addr: "20010db8000000000000000000000001", scope: "00", dev: "eth0"},
	}
	// accept_ra=0 but checkPerInterface=false means RA scan is skipped entirely.
	procDir := writeMockNeighborProcFS(t, addrs, true, map[string][2]string{"eth0": {"0", "0"}})

	m := &IPv6NeighborMonitor{
		name:   "test",
		config: &IPv6NeighborConfig{ExpectIPv6Enabled: true, CheckPerInterface: false, SkipInterfaces: defaultIPv6NeighborSkipInterfaces, ProcPath: procDir},
	}

	status, err := m.checkIPv6Neighbor(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cond := findNeighborCondition(status, conditionIPv6RADisabled)
	if cond == nil || cond.Status != types.ConditionFalse {
		t.Fatalf("expected IPv6RouterAdvertisementDisabled=False (check skipped), got %+v", cond)
	}
	if cond.Reason != "IPv6RADisabledCheckSkipped" {
		t.Errorf("expected reason IPv6RADisabledCheckSkipped, got %s", cond.Reason)
	}
}

func TestCheckIPv6Neighbor_NoInterfacesObserved(t *testing.T) {
	// if_inet6 present but only contains lo (skipped) -> no observed interfaces.
	addrs := []mockIfInet6Addr{
		{addr: "00000000000000000000000000000001", scope: "10", dev: "lo"},
	}
	procDir := writeMockNeighborProcFS(t, addrs, true, map[string][2]string{"lo": {"1", "1"}})

	m := &IPv6NeighborMonitor{
		name:   "test",
		config: &IPv6NeighborConfig{ExpectIPv6Enabled: true, CheckPerInterface: true, SkipInterfaces: defaultIPv6NeighborSkipInterfaces, ProcPath: procDir},
	}

	status, err := m.checkIPv6Neighbor(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cond := findNeighborCondition(status, conditionIPv6LinkLocalMissing)
	if cond == nil || cond.Status != types.ConditionFalse {
		t.Fatalf("expected IPv6LinkLocalMissing=False, got %+v", cond)
	}
	if cond.Reason != "IPv6NoInterfacesObserved" {
		t.Errorf("expected reason IPv6NoInterfacesObserved, got %s", cond.Reason)
	}
}

func TestCheckIPv6Neighbor_SkipInterfacesNilFallsBack(t *testing.T) {
	addrs := []mockIfInet6Addr{
		{addr: "00000000000000000000000000000001", scope: "10", dev: "lo"},
		{scope: "20", dev: "eth0"},
		{addr: "20010db8000000000000000000000001", scope: "00", dev: "eth0"},
	}
	procDir := writeMockNeighborProcFS(t, addrs, true, map[string][2]string{
		"lo":   {"0", "0"},
		"eth0": {"1", "1"},
	})

	m := &IPv6NeighborMonitor{
		name:   "test",
		config: &IPv6NeighborConfig{ExpectIPv6Enabled: true, CheckPerInterface: true, SkipInterfaces: nil, ProcPath: procDir},
	}

	status, err := m.checkIPv6Neighbor(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// lo skipped via default fallback -> conditions healthy.
	if cond := findNeighborCondition(status, conditionIPv6LinkLocalMissing); cond == nil || cond.Status != types.ConditionFalse {
		t.Fatalf("expected IPv6LinkLocalMissing=False, got %+v", cond)
	}
	if cond := findNeighborCondition(status, conditionIPv6RADisabled); cond == nil || cond.Status != types.ConditionFalse {
		t.Fatalf("expected IPv6RouterAdvertisementDisabled=False, got %+v", cond)
	}
}

func TestParseIfInet6File(t *testing.T) {
	addrs := []mockIfInet6Addr{
		{addr: "fe800000000000000000000000000abc", scope: "20", dev: "eth0"},
		{addr: "20010db8000000000000000000000001", scope: "00", dev: "eth0"},
	}
	procDir := writeMockNeighborProcFS(t, addrs, true, nil)

	got, err := parseIfInet6File(filepath.Join(procDir, "net", "if_inet6"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 addresses, got %d", len(got))
	}
	if !got[0].IsLinkLocal || got[0].IsGlobal {
		t.Errorf("addr[0] expected link-local, got %+v", got[0])
	}
	if got[1].IsLinkLocal || !got[1].IsGlobal {
		t.Errorf("addr[1] expected global, got %+v", got[1])
	}

	t.Run("0x prefixed scope", func(t *testing.T) {
		dir := t.TempDir()
		netDir := filepath.Join(dir, "net")
		if err := os.MkdirAll(netDir, 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		content := "fe800000000000000000000000000abc 02 40 0x20 80 wlan0\n" +
			"short line skipped\n" +
			"\n" +
			"badscope0000000000000000000000ff 02 40 zz 80 bad0\n"
		if err := os.WriteFile(filepath.Join(netDir, "if_inet6"), []byte(content), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
		got, err := parseIfInet6File(filepath.Join(netDir, "if_inet6"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("expected 1 valid address (others skipped), got %d", len(got))
		}
		if !got[0].IsLinkLocal || got[0].IfaceName != "wlan0" {
			t.Errorf("unexpected parsed addr: %+v", got[0])
		}
	})

	t.Run("missing file", func(t *testing.T) {
		_, err := parseIfInet6File(filepath.Join(t.TempDir(), "nope"))
		if err == nil {
			t.Error("expected error for missing file")
		}
		if !ipv6IfInet6Unreadable(err) {
			t.Error("expected ipv6IfInet6Unreadable to report true for missing file")
		}
	})
}

func TestReadSysctlInt(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    int
		wantErr bool
	}{
		{name: "zero", content: "0\n", want: 0},
		{name: "one", content: "1\n", want: 1},
		{name: "two with whitespace", content: " 2 \n", want: 2},
		{name: "non-numeric", content: "abc\n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := filepath.Join(t.TempDir(), "accept_ra")
			if err := os.WriteFile(f, []byte(tt.content), 0644); err != nil {
				t.Fatalf("write: %v", err)
			}
			got, err := readSysctlInt(f)
			if (err != nil) != tt.wantErr {
				t.Errorf("readSysctlInt() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("readSysctlInt() = %d, want %d", got, tt.want)
			}
		})
	}

	t.Run("missing file", func(t *testing.T) {
		if _, err := readSysctlInt("/nonexistent/accept_ra"); err == nil {
			t.Error("expected error for missing file")
		}
	})
}
