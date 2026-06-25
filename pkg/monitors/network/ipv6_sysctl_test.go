package network

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

func TestParseIPv6SysctlConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]any
		want    *IPv6SysctlConfig
		wantErr bool
	}{
		{
			name:   "nil config - use defaults",
			config: nil,
			want: &IPv6SysctlConfig{
				ExpectIPv6Enabled: defaultIPv6ExpectEnabled,
				CheckPerInterface: defaultIPv6CheckPerInterface,
				ProcPath:          defaultIPv6SysctlProcPath,
				SkipInterfaces:    defaultIPv6SkipInterfaces,
			},
			wantErr: false,
		},
		{
			name:   "empty config - use defaults",
			config: map[string]any{},
			want: &IPv6SysctlConfig{
				ExpectIPv6Enabled: defaultIPv6ExpectEnabled,
				CheckPerInterface: defaultIPv6CheckPerInterface,
				ProcPath:          defaultIPv6SysctlProcPath,
				SkipInterfaces:    defaultIPv6SkipInterfaces,
			},
			wantErr: false,
		},
		{
			name: "custom values",
			config: map[string]any{
				"expectIPv6Enabled": false,
				"checkPerInterface": true,
				"procPath":          "/host/proc",
			},
			want: &IPv6SysctlConfig{
				ExpectIPv6Enabled: false,
				CheckPerInterface: true,
				ProcPath:          "/host/proc",
				SkipInterfaces:    defaultIPv6SkipInterfaces,
			},
			wantErr: false,
		},
		{
			name: "with interfaces list",
			config: map[string]any{
				"interfaces": []any{"eth0", "eth1"},
			},
			want: &IPv6SysctlConfig{
				ExpectIPv6Enabled: defaultIPv6ExpectEnabled,
				CheckPerInterface: defaultIPv6CheckPerInterface,
				ProcPath:          defaultIPv6SysctlProcPath,
				SkipInterfaces:    defaultIPv6SkipInterfaces,
				Interfaces:        []string{"eth0", "eth1"},
			},
			wantErr: false,
		},
		{
			name: "with interfaces list as []string",
			config: map[string]any{
				"interfaces": []string{"eth0"},
			},
			want: &IPv6SysctlConfig{
				ExpectIPv6Enabled: defaultIPv6ExpectEnabled,
				CheckPerInterface: defaultIPv6CheckPerInterface,
				ProcPath:          defaultIPv6SysctlProcPath,
				SkipInterfaces:    defaultIPv6SkipInterfaces,
				Interfaces:        []string{"eth0"},
			},
			wantErr: false,
		},
		{
			name: "skipInterfaces overrides defaults",
			config: map[string]any{
				"skipInterfaces": []any{"all", "default"},
			},
			want: &IPv6SysctlConfig{
				ExpectIPv6Enabled: defaultIPv6ExpectEnabled,
				CheckPerInterface: defaultIPv6CheckPerInterface,
				ProcPath:          defaultIPv6SysctlProcPath,
				SkipInterfaces:    []string{"all", "default"},
			},
			wantErr: false,
		},
		{
			name: "invalid expectIPv6Enabled type",
			config: map[string]any{
				"expectIPv6Enabled": "yes",
			},
			wantErr: true,
		},
		{
			name: "invalid checkPerInterface type",
			config: map[string]any{
				"checkPerInterface": 1,
			},
			wantErr: true,
		},
		{
			name: "invalid procPath type",
			config: map[string]any{
				"procPath": 123,
			},
			wantErr: true,
		},
		{
			name: "invalid interfaces type",
			config: map[string]any{
				"interfaces": "eth0",
			},
			wantErr: true,
		},
		{
			name: "invalid interfaces element type",
			config: map[string]any{
				"interfaces": []any{123},
			},
			wantErr: true,
		},
		{
			name: "invalid skipInterfaces element type",
			config: map[string]any{
				"skipInterfaces": []any{true},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseIPv6SysctlConfig(tt.config)

			if (err != nil) != tt.wantErr {
				t.Errorf("parseIPv6SysctlConfig() error = %v, wantErr %v", err, tt.wantErr)
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

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestValidateIPv6SysctlConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]any
		wantErr bool
	}{
		{
			name:    "valid config",
			config:  map[string]any{"expectIPv6Enabled": true},
			wantErr: false,
		},
		{
			name:    "invalid config",
			config:  map[string]any{"expectIPv6Enabled": "yes"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monitorConfig := types.MonitorConfig{
				Name:     "test-ipv6-sysctl",
				Type:     "network-ipv6-sysctl",
				Interval: 60 * time.Second,
				Timeout:  5 * time.Second,
				Config:   tt.config,
			}
			err := ValidateIPv6SysctlConfig(monitorConfig)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateIPv6SysctlConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewIPv6SysctlMonitor(t *testing.T) {
	tests := []struct {
		name    string
		config  types.MonitorConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: types.MonitorConfig{
				Name:     "test-ipv6-sysctl",
				Type:     "network-ipv6-sysctl",
				Interval: 60 * time.Second,
				Timeout:  5 * time.Second,
				Config: map[string]any{
					"expectIPv6Enabled": true,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid config - bad type",
			config: types.MonitorConfig{
				Name:     "test-ipv6-sysctl",
				Type:     "network-ipv6-sysctl",
				Interval: 60 * time.Second,
				Timeout:  5 * time.Second,
				Config: map[string]any{
					"expectIPv6Enabled": "invalid",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monitor, err := NewIPv6SysctlMonitor(context.Background(), tt.config)

			if (err != nil) != tt.wantErr {
				t.Errorf("NewIPv6SysctlMonitor() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && monitor == nil {
				t.Error("NewIPv6SysctlMonitor() returned nil monitor")
			}
		})
	}
}

// createMockIPv6ProcFS creates a mock /proc/sys/net/ipv6/conf directory tree for
// testing. The allValue/defaultValue strings populate all/disable_ipv6 and
// default/disable_ipv6 respectively (empty string skips the file). The
// interfaces map populates per-interface disable_ipv6 files.
func createMockIPv6ProcFS(t *testing.T, allValue, defaultValue string, interfaces map[string]string) string {
	t.Helper()

	procDir := t.TempDir()

	writeScope := func(scope, value string) {
		if value == "" {
			return
		}
		dir := filepath.Join(procDir, "sys", "net", "ipv6", "conf", scope)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create %s dir: %v", scope, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "disable_ipv6"), []byte(value+"\n"), 0644); err != nil {
			t.Fatalf("Failed to write disable_ipv6 for %s: %v", scope, err)
		}
	}

	writeScope("all", allValue)
	writeScope("default", defaultValue)

	for ifaceName, value := range interfaces {
		writeScope(ifaceName, value)
	}

	return procDir
}

// findCondition returns the IPv6SysctlMisconfigured condition, or nil if absent.
func findIPv6Condition(status *types.Status) *types.Condition {
	for i := range status.Conditions {
		if status.Conditions[i].Type == "IPv6SysctlMisconfigured" {
			return &status.Conditions[i]
		}
	}
	return nil
}

func hasEventReason(status *types.Status, reason string) bool {
	for _, event := range status.Events {
		if event.Reason == reason {
			return true
		}
	}
	return false
}

func TestCheckIPv6Sysctl_AllEnabled(t *testing.T) {
	procDir := createMockIPv6ProcFS(t, "0", "0", nil)

	monitor := &IPv6SysctlMonitor{
		name: "test-ipv6-sysctl",
		config: &IPv6SysctlConfig{
			ExpectIPv6Enabled: true,
			ProcPath:          procDir,
		},
	}

	status, err := monitor.checkIPv6Sysctl(context.Background())
	if err != nil {
		t.Fatalf("checkIPv6Sysctl() unexpected error: %v", err)
	}

	cond := findIPv6Condition(status)
	if cond == nil {
		t.Fatal("Expected IPv6SysctlMisconfigured condition, but not found")
	}
	if cond.Status != types.ConditionFalse {
		t.Errorf("Expected IPv6SysctlMisconfigured=False, got %s", cond.Status)
	}
	if !hasEventReason(status, "IPv6SysctlsHealthy") {
		t.Error("Expected IPv6SysctlsHealthy event, but not found")
	}
}

func TestCheckIPv6Sysctl_AllDisabled(t *testing.T) {
	procDir := createMockIPv6ProcFS(t, "1", "0", nil)

	monitor := &IPv6SysctlMonitor{
		name: "test-ipv6-sysctl",
		config: &IPv6SysctlConfig{
			ExpectIPv6Enabled: true,
			ProcPath:          procDir,
		},
	}

	status, err := monitor.checkIPv6Sysctl(context.Background())
	if err != nil {
		t.Fatalf("checkIPv6Sysctl() unexpected error: %v", err)
	}

	cond := findIPv6Condition(status)
	if cond == nil {
		t.Fatal("Expected IPv6SysctlMisconfigured condition, but not found")
	}
	if cond.Status != types.ConditionTrue {
		t.Errorf("Expected IPv6SysctlMisconfigured=True, got %s", cond.Status)
	}
	if cond.Reason != "DisableIPv6Set" {
		t.Errorf("Expected reason DisableIPv6Set, got %s", cond.Reason)
	}
	if !hasEventReason(status, "IPv6Disabled") {
		t.Error("Expected IPv6Disabled warning event, but not found")
	}
	for _, event := range status.Events {
		if event.Reason == "IPv6Disabled" && event.Severity != types.EventWarning {
			t.Errorf("Expected Warning severity for IPv6Disabled, got %s", event.Severity)
		}
	}
}

func TestCheckIPv6Sysctl_DefaultDisabled(t *testing.T) {
	procDir := createMockIPv6ProcFS(t, "0", "1", nil)

	monitor := &IPv6SysctlMonitor{
		name: "test-ipv6-sysctl",
		config: &IPv6SysctlConfig{
			ExpectIPv6Enabled: true,
			ProcPath:          procDir,
		},
	}

	status, err := monitor.checkIPv6Sysctl(context.Background())
	if err != nil {
		t.Fatalf("checkIPv6Sysctl() unexpected error: %v", err)
	}

	cond := findIPv6Condition(status)
	if cond == nil {
		t.Fatal("Expected IPv6SysctlMisconfigured condition, but not found")
	}
	if cond.Status != types.ConditionTrue {
		t.Errorf("Expected IPv6SysctlMisconfigured=True (default disabled), got %s", cond.Status)
	}
	if !hasEventReason(status, "IPv6Disabled") {
		t.Error("Expected IPv6Disabled warning event, but not found")
	}
}

func TestCheckIPv6Sysctl_ExpectDisabledSuppressesSeverity(t *testing.T) {
	procDir := createMockIPv6ProcFS(t, "1", "1", nil)

	monitor := &IPv6SysctlMonitor{
		name: "test-ipv6-sysctl",
		config: &IPv6SysctlConfig{
			ExpectIPv6Enabled: false,
			ProcPath:          procDir,
		},
	}

	status, err := monitor.checkIPv6Sysctl(context.Background())
	if err != nil {
		t.Fatalf("checkIPv6Sysctl() unexpected error: %v", err)
	}

	// With expectIPv6Enabled=false, disable_ipv6=1 is not a finding.
	cond := findIPv6Condition(status)
	if cond == nil {
		t.Fatal("Expected IPv6SysctlMisconfigured condition, but not found")
	}
	if cond.Status != types.ConditionFalse {
		t.Errorf("Expected IPv6SysctlMisconfigured=False (expect disabled), got %s", cond.Status)
	}
	// Should emit informational events, not warnings.
	if !hasEventReason(status, "IPv6DisabledExpected") {
		t.Error("Expected IPv6DisabledExpected info event, but not found")
	}
	if hasEventReason(status, "IPv6Disabled") {
		t.Error("Did not expect IPv6Disabled warning event when expectIPv6Enabled=false")
	}
}

func TestCheckIPv6Sysctl_PerInterfaceMix(t *testing.T) {
	interfaces := map[string]string{
		"eth0": "0",
		"eth1": "1",
		"lo":   "1", // skipped by default
	}
	procDir := createMockIPv6ProcFS(t, "0", "0", interfaces)

	monitor := &IPv6SysctlMonitor{
		name: "test-ipv6-sysctl",
		config: &IPv6SysctlConfig{
			ExpectIPv6Enabled: true,
			CheckPerInterface: true,
			SkipInterfaces:    defaultIPv6SkipInterfaces,
			ProcPath:          procDir,
		},
	}

	status, err := monitor.checkIPv6Sysctl(context.Background())
	if err != nil {
		t.Fatalf("checkIPv6Sysctl() unexpected error: %v", err)
	}

	cond := findIPv6Condition(status)
	if cond == nil {
		t.Fatal("Expected IPv6SysctlMisconfigured condition, but not found")
	}
	if cond.Status != types.ConditionTrue {
		t.Errorf("Expected IPv6SysctlMisconfigured=True (eth1 disabled), got %s", cond.Status)
	}
	if !hasEventReason(status, "InterfaceIPv6Disabled") {
		t.Error("Expected InterfaceIPv6Disabled event for eth1, but not found")
	}
}

func TestCheckIPv6Sysctl_InterfacesFilter(t *testing.T) {
	interfaces := map[string]string{
		"eth0": "1", // disabled but not in filter
		"eth1": "0", // enabled, in filter
	}
	procDir := createMockIPv6ProcFS(t, "0", "0", interfaces)

	monitor := &IPv6SysctlMonitor{
		name: "test-ipv6-sysctl",
		config: &IPv6SysctlConfig{
			ExpectIPv6Enabled: true,
			CheckPerInterface: true,
			Interfaces:        []string{"eth1"},
			SkipInterfaces:    defaultIPv6SkipInterfaces,
			ProcPath:          procDir,
		},
	}

	status, err := monitor.checkIPv6Sysctl(context.Background())
	if err != nil {
		t.Fatalf("checkIPv6Sysctl() unexpected error: %v", err)
	}

	cond := findIPv6Condition(status)
	if cond == nil {
		t.Fatal("Expected IPv6SysctlMisconfigured condition, but not found")
	}
	if cond.Status != types.ConditionFalse {
		t.Errorf("Expected IPv6SysctlMisconfigured=False (only eth1 checked, enabled), got %s", cond.Status)
	}
	if hasEventReason(status, "InterfaceIPv6Disabled") {
		t.Error("Did not expect InterfaceIPv6Disabled event when eth0 filtered out")
	}
}

func TestCheckIPv6Sysctl_SkipInterfacesRespected(t *testing.T) {
	// all/default/lo all disabled but should be skipped in per-interface scan.
	// The scoped all/default checks run separately, so set them enabled here and
	// verify lo (disabled) is skipped by the per-interface scan.
	interfaces := map[string]string{
		"lo": "1",
	}
	procDir := createMockIPv6ProcFS(t, "0", "0", interfaces)

	monitor := &IPv6SysctlMonitor{
		name: "test-ipv6-sysctl",
		config: &IPv6SysctlConfig{
			ExpectIPv6Enabled: true,
			CheckPerInterface: true,
			SkipInterfaces:    defaultIPv6SkipInterfaces,
			ProcPath:          procDir,
		},
	}

	status, err := monitor.checkIPv6Sysctl(context.Background())
	if err != nil {
		t.Fatalf("checkIPv6Sysctl() unexpected error: %v", err)
	}

	cond := findIPv6Condition(status)
	if cond == nil {
		t.Fatal("Expected IPv6SysctlMisconfigured condition, but not found")
	}
	if cond.Status != types.ConditionFalse {
		t.Errorf("Expected IPv6SysctlMisconfigured=False (lo skipped), got %s", cond.Status)
	}
	if hasEventReason(status, "InterfaceIPv6Disabled") {
		t.Error("Did not expect InterfaceIPv6Disabled event for skipped lo interface")
	}
}

func TestCheckIPv6Sysctl_SkipInterfacesNilFallsBackToDefault(t *testing.T) {
	// SkipInterfaces is nil (not set), so checkPerInterfaceDisableIPv6 must fall
	// back to defaultIPv6SkipInterfaces and skip lo.
	interfaces := map[string]string{
		"lo": "1",
	}
	procDir := createMockIPv6ProcFS(t, "0", "0", interfaces)

	monitor := &IPv6SysctlMonitor{
		name: "test-ipv6-sysctl",
		config: &IPv6SysctlConfig{
			ExpectIPv6Enabled: true,
			CheckPerInterface: true,
			SkipInterfaces:    nil,
			ProcPath:          procDir,
		},
	}

	status, err := monitor.checkIPv6Sysctl(context.Background())
	if err != nil {
		t.Fatalf("checkIPv6Sysctl() unexpected error: %v", err)
	}

	cond := findIPv6Condition(status)
	if cond == nil {
		t.Fatal("Expected IPv6SysctlMisconfigured condition, but not found")
	}
	if cond.Status != types.ConditionFalse {
		t.Errorf("Expected IPv6SysctlMisconfigured=False (lo skipped via default), got %s", cond.Status)
	}
}

func TestCheckIPv6Sysctl_PerInterfaceExpectDisabled(t *testing.T) {
	interfaces := map[string]string{
		"eth1": "1",
	}
	procDir := createMockIPv6ProcFS(t, "0", "0", interfaces)

	monitor := &IPv6SysctlMonitor{
		name: "test-ipv6-sysctl",
		config: &IPv6SysctlConfig{
			ExpectIPv6Enabled: false,
			CheckPerInterface: true,
			SkipInterfaces:    defaultIPv6SkipInterfaces,
			ProcPath:          procDir,
		},
	}

	status, err := monitor.checkIPv6Sysctl(context.Background())
	if err != nil {
		t.Fatalf("checkIPv6Sysctl() unexpected error: %v", err)
	}

	cond := findIPv6Condition(status)
	if cond == nil {
		t.Fatal("Expected IPv6SysctlMisconfigured condition, but not found")
	}
	if cond.Status != types.ConditionFalse {
		t.Errorf("Expected IPv6SysctlMisconfigured=False (expect disabled), got %s", cond.Status)
	}
	if !hasEventReason(status, "InterfaceIPv6DisabledExpected") {
		t.Error("Expected InterfaceIPv6DisabledExpected info event, but not found")
	}
}

func TestCheckIPv6Sysctl_MissingFiles(t *testing.T) {
	// procDir exists but has no disable_ipv6 files -> read errors become
	// warnings + findings (not a hard error).
	procDir := createMockIPv6ProcFS(t, "", "", nil)

	monitor := &IPv6SysctlMonitor{
		name: "test-ipv6-sysctl",
		config: &IPv6SysctlConfig{
			ExpectIPv6Enabled: true,
			ProcPath:          procDir,
		},
	}

	status, err := monitor.checkIPv6Sysctl(context.Background())
	if err != nil {
		t.Fatalf("checkIPv6Sysctl() unexpected error (should not hard error): %v", err)
	}

	if !hasEventReason(status, "IPv6SysctlReadError") {
		t.Error("Expected IPv6SysctlReadError warning event for missing files, but not found")
	}
	for _, event := range status.Events {
		if event.Reason == "IPv6SysctlReadError" && event.Severity != types.EventWarning {
			t.Errorf("Expected Warning severity for IPv6SysctlReadError, got %s", event.Severity)
		}
	}

	cond := findIPv6Condition(status)
	if cond == nil {
		t.Fatal("Expected IPv6SysctlMisconfigured condition, but not found")
	}
	if cond.Status != types.ConditionTrue {
		t.Errorf("Expected IPv6SysctlMisconfigured=True (unreadable files flagged), got %s", cond.Status)
	}
}

func TestCheckIPv6Sysctl_NonexistentProcPath(t *testing.T) {
	monitor := &IPv6SysctlMonitor{
		name: "test-ipv6-sysctl",
		config: &IPv6SysctlConfig{
			ExpectIPv6Enabled: true,
			CheckPerInterface: true,
			SkipInterfaces:    defaultIPv6SkipInterfaces,
			ProcPath:          "/nonexistent/proc",
		},
	}

	status, err := monitor.checkIPv6Sysctl(context.Background())
	if err != nil {
		t.Fatalf("checkIPv6Sysctl() unexpected error: %v", err)
	}

	if !hasEventReason(status, "IPv6SysctlReadError") {
		t.Error("Expected IPv6SysctlReadError event for nonexistent procPath, but not found")
	}

	cond := findIPv6Condition(status)
	if cond == nil {
		t.Fatal("Expected IPv6SysctlMisconfigured condition, but not found")
	}
	if cond.Status != types.ConditionTrue {
		t.Errorf("Expected IPv6SysctlMisconfigured=True for nonexistent procPath, got %s", cond.Status)
	}
}

func TestCheckIPv6Sysctl_TestdataFixture(t *testing.T) {
	procDir := filepath.Join("testdata", "proc")

	monitor := &IPv6SysctlMonitor{
		name: "test-ipv6-sysctl",
		config: &IPv6SysctlConfig{
			ExpectIPv6Enabled: true,
			ProcPath:          procDir,
		},
	}

	status, err := monitor.checkIPv6Sysctl(context.Background())
	if err != nil {
		t.Fatalf("checkIPv6Sysctl() unexpected error: %v", err)
	}

	cond := findIPv6Condition(status)
	if cond == nil {
		t.Fatal("Expected IPv6SysctlMisconfigured condition, but not found")
	}
	if cond.Status != types.ConditionFalse {
		t.Errorf("Expected IPv6SysctlMisconfigured=False from healthy fixture, got %s", cond.Status)
	}
	if !hasEventReason(status, "IPv6SysctlsHealthy") {
		t.Error("Expected IPv6SysctlsHealthy event from healthy fixture, but not found")
	}
}

func TestReadSysctlBool(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
		wantErr bool
	}{
		{name: "disabled", content: "1\n", want: true},
		{name: "enabled", content: "0\n", want: false},
		{name: "disabled no newline", content: "1", want: true},
		{name: "enabled with whitespace", content: " 0 \n", want: false},
		{name: "unexpected value", content: "2\n", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile := filepath.Join(t.TempDir(), "disable_ipv6")
			if err := os.WriteFile(tmpFile, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			got, err := readSysctlBool(tmpFile)
			if (err != nil) != tt.wantErr {
				t.Errorf("readSysctlBool() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("readSysctlBool() = %v, want %v", got, tt.want)
			}
		})
	}

	t.Run("non-existent file", func(t *testing.T) {
		_, err := readSysctlBool("/nonexistent/file")
		if err == nil {
			t.Error("Expected error for non-existent file, got nil")
		}
	})
}
