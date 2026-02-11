package network

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

func TestParseIPForwardingConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]any
		want    *IPForwardingConfig
		wantErr bool
	}{
		{
			name:   "nil config - use defaults",
			config: nil,
			want: &IPForwardingConfig{
				CheckIPv4:         defaultCheckIPv4,
				CheckIPv6:         defaultCheckIPv6,
				CheckPerInterface: defaultCheckPerInterface,
				ProcPath:          defaultProcPath,
			},
			wantErr: false,
		},
		{
			name:   "empty config - use defaults",
			config: map[string]any{},
			want: &IPForwardingConfig{
				CheckIPv4:         defaultCheckIPv4,
				CheckIPv6:         defaultCheckIPv6,
				CheckPerInterface: defaultCheckPerInterface,
				ProcPath:          defaultProcPath,
			},
			wantErr: false,
		},
		{
			name: "custom values",
			config: map[string]any{
				"checkIPv4":         false,
				"checkIPv6":         false,
				"checkPerInterface": true,
				"procPath":          "/host/proc",
			},
			want: &IPForwardingConfig{
				CheckIPv4:         false,
				CheckIPv6:         false,
				CheckPerInterface: true,
				ProcPath:          "/host/proc",
			},
			wantErr: false,
		},
		{
			name: "with interfaces list",
			config: map[string]any{
				"interfaces": []any{"eth0", "eth1"},
			},
			want: &IPForwardingConfig{
				CheckIPv4:         defaultCheckIPv4,
				CheckIPv6:         defaultCheckIPv6,
				CheckPerInterface: defaultCheckPerInterface,
				ProcPath:          defaultProcPath,
				Interfaces:        []string{"eth0", "eth1"},
			},
			wantErr: false,
		},
		{
			name: "invalid checkIPv4 type",
			config: map[string]any{
				"checkIPv4": "yes",
			},
			wantErr: true,
		},
		{
			name: "invalid checkIPv6 type",
			config: map[string]any{
				"checkIPv6": 1,
			},
			wantErr: true,
		},
		{
			name: "invalid checkPerInterface type",
			config: map[string]any{
				"checkPerInterface": "true",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseIPForwardingConfig(tt.config)

			if (err != nil) != tt.wantErr {
				t.Errorf("parseIPForwardingConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			if got.CheckIPv4 != tt.want.CheckIPv4 {
				t.Errorf("CheckIPv4 = %v, want %v", got.CheckIPv4, tt.want.CheckIPv4)
			}
			if got.CheckIPv6 != tt.want.CheckIPv6 {
				t.Errorf("CheckIPv6 = %v, want %v", got.CheckIPv6, tt.want.CheckIPv6)
			}
			if got.CheckPerInterface != tt.want.CheckPerInterface {
				t.Errorf("CheckPerInterface = %v, want %v", got.CheckPerInterface, tt.want.CheckPerInterface)
			}
			if got.ProcPath != tt.want.ProcPath {
				t.Errorf("ProcPath = %v, want %v", got.ProcPath, tt.want.ProcPath)
			}
			if len(got.Interfaces) != len(tt.want.Interfaces) {
				t.Errorf("Interfaces length = %v, want %v", len(got.Interfaces), len(tt.want.Interfaces))
			}
			for i := range got.Interfaces {
				if i < len(tt.want.Interfaces) && got.Interfaces[i] != tt.want.Interfaces[i] {
					t.Errorf("Interfaces[%d] = %v, want %v", i, got.Interfaces[i], tt.want.Interfaces[i])
				}
			}
		})
	}
}

func TestValidateIPForwardingConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]any
		wantErr bool
	}{
		{
			name:    "valid config",
			config:  map[string]any{"checkIPv4": true},
			wantErr: false,
		},
		{
			name:    "invalid config",
			config:  map[string]any{"checkIPv4": "yes"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monitorConfig := types.MonitorConfig{
				Name:     "test-ip-forwarding",
				Type:     "network-ip-forwarding",
				Interval: 30 * time.Second,
				Timeout:  5 * time.Second,
				Config:   tt.config,
			}
			err := ValidateIPForwardingConfig(monitorConfig)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateIPForwardingConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewIPForwardingMonitor(t *testing.T) {
	tests := []struct {
		name    string
		config  types.MonitorConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: types.MonitorConfig{
				Name:     "test-ip-forwarding",
				Type:     "network-ip-forwarding",
				Interval: 30 * time.Second,
				Timeout:  5 * time.Second,
				Config: map[string]any{
					"checkIPv4": true,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid config - bad type",
			config: types.MonitorConfig{
				Name:     "test-ip-forwarding",
				Type:     "network-ip-forwarding",
				Interval: 30 * time.Second,
				Timeout:  5 * time.Second,
				Config: map[string]any{
					"checkIPv4": "invalid",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monitor, err := NewIPForwardingMonitor(context.Background(), tt.config)

			if (err != nil) != tt.wantErr {
				t.Errorf("NewIPForwardingMonitor() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && monitor == nil {
				t.Error("NewIPForwardingMonitor() returned nil monitor")
			}
		})
	}
}

// createMockProcFS creates a mock /proc/sys/net directory structure for testing.
func createMockProcFS(t *testing.T, ipv4Value, ipv6Value string, interfaces map[string]string) string {
	t.Helper()

	procDir := t.TempDir()

	// Create IPv4 forwarding file
	if ipv4Value != "" {
		ipv4Dir := filepath.Join(procDir, "sys", "net", "ipv4")
		if err := os.MkdirAll(ipv4Dir, 0755); err != nil {
			t.Fatalf("Failed to create IPv4 dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(ipv4Dir, "ip_forward"), []byte(ipv4Value+"\n"), 0644); err != nil {
			t.Fatalf("Failed to write IPv4 forward file: %v", err)
		}
	}

	// Create IPv6 forwarding file
	if ipv6Value != "" {
		ipv6Dir := filepath.Join(procDir, "sys", "net", "ipv6", "conf", "all")
		if err := os.MkdirAll(ipv6Dir, 0755); err != nil {
			t.Fatalf("Failed to create IPv6 dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(ipv6Dir, "forwarding"), []byte(ipv6Value+"\n"), 0644); err != nil {
			t.Fatalf("Failed to write IPv6 forward file: %v", err)
		}
	}

	// Create per-interface forwarding files
	for ifaceName, value := range interfaces {
		ifaceDir := filepath.Join(procDir, "sys", "net", "ipv4", "conf", ifaceName)
		if err := os.MkdirAll(ifaceDir, 0755); err != nil {
			t.Fatalf("Failed to create interface dir for %s: %v", ifaceName, err)
		}
		if err := os.WriteFile(filepath.Join(ifaceDir, "forwarding"), []byte(value+"\n"), 0644); err != nil {
			t.Fatalf("Failed to write interface forwarding file for %s: %v", ifaceName, err)
		}
	}

	return procDir
}

func TestCheckIPForwarding_AllEnabled(t *testing.T) {
	procDir := createMockProcFS(t, "1", "1", nil)

	monitor := &IPForwardingMonitor{
		name: "test-ip-forwarding",
		config: &IPForwardingConfig{
			CheckIPv4: true,
			CheckIPv6: true,
			ProcPath:  procDir,
		},
	}

	status, err := monitor.checkIPForwarding(context.Background())
	if err != nil {
		t.Fatalf("checkIPForwarding() unexpected error: %v", err)
	}

	// Should have IPForwardingDisabled=False condition
	foundCondition := false
	for _, cond := range status.Conditions {
		if cond.Type == "IPForwardingDisabled" {
			foundCondition = true
			if cond.Status != types.ConditionFalse {
				t.Errorf("Expected IPForwardingDisabled=False, got %s", cond.Status)
			}
		}
	}
	if !foundCondition {
		t.Error("Expected IPForwardingDisabled condition, but not found")
	}

	// Should have healthy event
	hasHealthy := false
	for _, event := range status.Events {
		if event.Reason == "IPForwardingHealthy" {
			hasHealthy = true
		}
	}
	if !hasHealthy {
		t.Error("Expected IPForwardingHealthy event, but not found")
	}
}

func TestCheckIPForwarding_IPv4Disabled(t *testing.T) {
	procDir := createMockProcFS(t, "0", "1", nil)

	monitor := &IPForwardingMonitor{
		name: "test-ip-forwarding",
		config: &IPForwardingConfig{
			CheckIPv4: true,
			CheckIPv6: true,
			ProcPath:  procDir,
		},
	}

	status, err := monitor.checkIPForwarding(context.Background())
	if err != nil {
		t.Fatalf("checkIPForwarding() unexpected error: %v", err)
	}

	// Should have IPForwardingDisabled=True condition
	foundCondition := false
	for _, cond := range status.Conditions {
		if cond.Type == "IPForwardingDisabled" {
			foundCondition = true
			if cond.Status != types.ConditionTrue {
				t.Errorf("Expected IPForwardingDisabled=True, got %s", cond.Status)
			}
			if cond.Reason != "ForwardingDisabled" {
				t.Errorf("Expected reason ForwardingDisabled, got %s", cond.Reason)
			}
		}
	}
	if !foundCondition {
		t.Error("Expected IPForwardingDisabled condition, but not found")
	}

	// Should have IPv4ForwardingDisabled error event
	hasIPv4Error := false
	for _, event := range status.Events {
		if event.Reason == "IPv4ForwardingDisabled" {
			hasIPv4Error = true
			if event.Severity != types.EventError {
				t.Errorf("Expected Error severity for IPv4 disabled, got %s", event.Severity)
			}
		}
	}
	if !hasIPv4Error {
		t.Error("Expected IPv4ForwardingDisabled event, but not found")
	}
}

func TestCheckIPForwarding_IPv6Disabled(t *testing.T) {
	procDir := createMockProcFS(t, "1", "0", nil)

	monitor := &IPForwardingMonitor{
		name: "test-ip-forwarding",
		config: &IPForwardingConfig{
			CheckIPv4: true,
			CheckIPv6: true,
			ProcPath:  procDir,
		},
	}

	status, err := monitor.checkIPForwarding(context.Background())
	if err != nil {
		t.Fatalf("checkIPForwarding() unexpected error: %v", err)
	}

	// Should have IPForwardingDisabled=True condition
	foundCondition := false
	for _, cond := range status.Conditions {
		if cond.Type == "IPForwardingDisabled" {
			foundCondition = true
			if cond.Status != types.ConditionTrue {
				t.Errorf("Expected IPForwardingDisabled=True, got %s", cond.Status)
			}
		}
	}
	if !foundCondition {
		t.Error("Expected IPForwardingDisabled condition, but not found")
	}

	// Should have IPv6ForwardingDisabled warning event (not error)
	hasIPv6Warning := false
	for _, event := range status.Events {
		if event.Reason == "IPv6ForwardingDisabled" {
			hasIPv6Warning = true
			if event.Severity != types.EventWarning {
				t.Errorf("Expected Warning severity for IPv6 disabled, got %s", event.Severity)
			}
		}
	}
	if !hasIPv6Warning {
		t.Error("Expected IPv6ForwardingDisabled event, but not found")
	}
}

func TestCheckIPForwarding_PerInterface(t *testing.T) {
	interfaces := map[string]string{
		"eth0":    "1",
		"eth1":    "0",
		"lo":      "1",
		"all":     "1",
		"default": "1",
	}
	procDir := createMockProcFS(t, "1", "1", interfaces)

	monitor := &IPForwardingMonitor{
		name: "test-ip-forwarding",
		config: &IPForwardingConfig{
			CheckIPv4:         true,
			CheckIPv6:         true,
			CheckPerInterface: true,
			ProcPath:          procDir,
		},
	}

	status, err := monitor.checkIPForwarding(context.Background())
	if err != nil {
		t.Fatalf("checkIPForwarding() unexpected error: %v", err)
	}

	// Should have IPForwardingDisabled=True because eth1 is disabled
	foundCondition := false
	for _, cond := range status.Conditions {
		if cond.Type == "IPForwardingDisabled" {
			foundCondition = true
			if cond.Status != types.ConditionTrue {
				t.Errorf("Expected IPForwardingDisabled=True (eth1 disabled), got %s", cond.Status)
			}
		}
	}
	if !foundCondition {
		t.Error("Expected IPForwardingDisabled condition, but not found")
	}

	// Should have InterfaceForwardingDisabled event for eth1
	hasEth1Disabled := false
	for _, event := range status.Events {
		if event.Reason == "InterfaceForwardingDisabled" {
			hasEth1Disabled = true
		}
	}
	if !hasEth1Disabled {
		t.Error("Expected InterfaceForwardingDisabled event for eth1, but not found")
	}
}

func TestCheckIPForwarding_PerInterfaceFiltered(t *testing.T) {
	interfaces := map[string]string{
		"eth0": "1",
		"eth1": "0",
		"eth2": "0",
	}
	procDir := createMockProcFS(t, "1", "1", interfaces)

	monitor := &IPForwardingMonitor{
		name: "test-ip-forwarding",
		config: &IPForwardingConfig{
			CheckIPv4:         false, // skip global checks
			CheckIPv6:         false,
			CheckPerInterface: true,
			Interfaces:        []string{"eth0"}, // only check eth0
			ProcPath:          procDir,
		},
	}

	status, err := monitor.checkIPForwarding(context.Background())
	if err != nil {
		t.Fatalf("checkIPForwarding() unexpected error: %v", err)
	}

	// eth0 is enabled, eth1/eth2 are disabled but not checked because filtered
	foundCondition := false
	for _, cond := range status.Conditions {
		if cond.Type == "IPForwardingDisabled" {
			foundCondition = true
			if cond.Status != types.ConditionFalse {
				t.Errorf("Expected IPForwardingDisabled=False (only eth0 checked, which is enabled), got %s", cond.Status)
			}
		}
	}
	if !foundCondition {
		t.Error("Expected IPForwardingDisabled condition, but not found")
	}
}

func TestCheckIPForwarding_ProcNotReadable(t *testing.T) {
	// Use a non-existent proc path
	monitor := &IPForwardingMonitor{
		name: "test-ip-forwarding",
		config: &IPForwardingConfig{
			CheckIPv4: true,
			CheckIPv6: true,
			ProcPath:  "/nonexistent/proc",
		},
	}

	status, err := monitor.checkIPForwarding(context.Background())
	if err != nil {
		t.Fatalf("checkIPForwarding() unexpected error: %v", err)
	}

	// Should have read error events
	hasReadError := false
	for _, event := range status.Events {
		if event.Reason == "IPForwardingReadError" {
			hasReadError = true
		}
	}
	if !hasReadError {
		t.Error("Expected IPForwardingReadError event for unreadable proc, but not found")
	}

	// Should still have condition set (unreadable counts as disabled)
	foundCondition := false
	for _, cond := range status.Conditions {
		if cond.Type == "IPForwardingDisabled" {
			foundCondition = true
			if cond.Status != types.ConditionTrue {
				t.Errorf("Expected IPForwardingDisabled=True for unreadable proc, got %s", cond.Status)
			}
		}
	}
	if !foundCondition {
		t.Error("Expected IPForwardingDisabled condition, but not found")
	}
}

func TestCheckIPForwarding_IPv4Only(t *testing.T) {
	procDir := createMockProcFS(t, "1", "", nil) // No IPv6 file

	monitor := &IPForwardingMonitor{
		name: "test-ip-forwarding",
		config: &IPForwardingConfig{
			CheckIPv4: true,
			CheckIPv6: false, // disabled
			ProcPath:  procDir,
		},
	}

	status, err := monitor.checkIPForwarding(context.Background())
	if err != nil {
		t.Fatalf("checkIPForwarding() unexpected error: %v", err)
	}

	// Should be healthy (only IPv4 checked, which is enabled)
	foundCondition := false
	for _, cond := range status.Conditions {
		if cond.Type == "IPForwardingDisabled" {
			foundCondition = true
			if cond.Status != types.ConditionFalse {
				t.Errorf("Expected IPForwardingDisabled=False, got %s", cond.Status)
			}
		}
	}
	if !foundCondition {
		t.Error("Expected IPForwardingDisabled condition, but not found")
	}

	// Should not have any IPv6-related events
	for _, event := range status.Events {
		if event.Reason == "IPv6ForwardingDisabled" || (event.Reason == "IPForwardingReadError" && event.Severity == types.EventWarning) {
			t.Error("Should not have IPv6-related events when IPv6 checking is disabled")
		}
	}
}

func TestCheckIPForwarding_BothDisabled(t *testing.T) {
	procDir := createMockProcFS(t, "0", "0", nil)

	monitor := &IPForwardingMonitor{
		name: "test-ip-forwarding",
		config: &IPForwardingConfig{
			CheckIPv4: true,
			CheckIPv6: true,
			ProcPath:  procDir,
		},
	}

	status, err := monitor.checkIPForwarding(context.Background())
	if err != nil {
		t.Fatalf("checkIPForwarding() unexpected error: %v", err)
	}

	// Should have both events
	hasIPv4 := false
	hasIPv6 := false
	for _, event := range status.Events {
		if event.Reason == "IPv4ForwardingDisabled" {
			hasIPv4 = true
		}
		if event.Reason == "IPv6ForwardingDisabled" {
			hasIPv6 = true
		}
	}

	if !hasIPv4 {
		t.Error("Expected IPv4ForwardingDisabled event")
	}
	if !hasIPv6 {
		t.Error("Expected IPv6ForwardingDisabled event")
	}

	// Condition message should mention both
	foundCondition := false
	for _, cond := range status.Conditions {
		if cond.Type == "IPForwardingDisabled" {
			foundCondition = true
			if cond.Status != types.ConditionTrue {
				t.Errorf("Expected IPForwardingDisabled=True, got %s", cond.Status)
			}
		}
	}
	if !foundCondition {
		t.Error("Expected IPForwardingDisabled condition, but not found")
	}
}

func TestExtractInterfaceName(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "standard interface",
			path: "/proc/sys/net/ipv4/conf/eth0/forwarding",
			want: "eth0",
		},
		{
			name: "loopback",
			path: "/proc/sys/net/ipv4/conf/lo/forwarding",
			want: "lo",
		},
		{
			name: "all",
			path: "/proc/sys/net/ipv4/conf/all/forwarding",
			want: "all",
		},
		{
			name: "container mount path",
			path: "/host/proc/sys/net/ipv4/conf/cni0/forwarding",
			want: "cni0",
		},
		{
			name: "no conf in path",
			path: "/proc/sys/net/ipv4/ip_forward",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractInterfaceName(tt.path)
			if got != tt.want {
				t.Errorf("extractInterfaceName(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestReadForwardingSetting(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
		wantErr bool
	}{
		{
			name:    "enabled",
			content: "1\n",
			want:    true,
		},
		{
			name:    "disabled",
			content: "0\n",
			want:    false,
		},
		{
			name:    "enabled no newline",
			content: "1",
			want:    true,
		},
		{
			name:    "disabled no newline",
			content: "0",
			want:    false,
		},
		{
			name:    "enabled with whitespace",
			content: " 1 \n",
			want:    true,
		},
		{
			name:    "unexpected value",
			content: "2\n",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile := filepath.Join(t.TempDir(), "forwarding")
			if err := os.WriteFile(tmpFile, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			got, err := readForwardingSetting(tmpFile)
			if (err != nil) != tt.wantErr {
				t.Errorf("readForwardingSetting() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("readForwardingSetting() = %v, want %v", got, tt.want)
			}
		})
	}

	// Test non-existent file
	t.Run("non-existent file", func(t *testing.T) {
		_, err := readForwardingSetting("/nonexistent/file")
		if err == nil {
			t.Error("Expected error for non-existent file, got nil")
		}
	})
}
