package network

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

func TestParseIPv6RouteConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]any
		want    *IPv6RouteConfig
		wantErr bool
	}{
		{
			name:   "nil config - use defaults",
			config: nil,
			want: &IPv6RouteConfig{
				ExpectDefaultRoute: defaultIPv6RouteExpectDefault,
				ProcPath:           defaultIPv6RouteProcPath,
			},
		},
		{
			name:   "empty config - use defaults",
			config: map[string]any{},
			want: &IPv6RouteConfig{
				ExpectDefaultRoute: defaultIPv6RouteExpectDefault,
				ProcPath:           defaultIPv6RouteProcPath,
			},
		},
		{
			name: "custom values",
			config: map[string]any{
				"expectDefaultRoute": false,
				"procPath":           "/host/proc",
			},
			want: &IPv6RouteConfig{
				ExpectDefaultRoute: false,
				ProcPath:           "/host/proc",
			},
		},
		{
			name:    "invalid expectDefaultRoute type",
			config:  map[string]any{"expectDefaultRoute": "yes"},
			wantErr: true,
		},
		{
			name:    "invalid procPath type",
			config:  map[string]any{"procPath": 123},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseIPv6RouteConfig(tt.config)

			if (err != nil) != tt.wantErr {
				t.Errorf("parseIPv6RouteConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			if got.ExpectDefaultRoute != tt.want.ExpectDefaultRoute {
				t.Errorf("ExpectDefaultRoute = %v, want %v", got.ExpectDefaultRoute, tt.want.ExpectDefaultRoute)
			}
			if got.ProcPath != tt.want.ProcPath {
				t.Errorf("ProcPath = %v, want %v", got.ProcPath, tt.want.ProcPath)
			}
		})
	}
}

func TestValidateIPv6RouteConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]any
		wantErr bool
	}{
		{
			name:    "valid config",
			config:  map[string]any{"expectDefaultRoute": true},
			wantErr: false,
		},
		{
			name:    "invalid config",
			config:  map[string]any{"expectDefaultRoute": "yes"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monitorConfig := types.MonitorConfig{
				Name:     "test-ipv6-route",
				Type:     "network-ipv6-route",
				Interval: 60 * time.Second,
				Timeout:  5 * time.Second,
				Config:   tt.config,
			}
			err := ValidateIPv6RouteConfig(monitorConfig)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateIPv6RouteConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewIPv6RouteMonitor(t *testing.T) {
	tests := []struct {
		name    string
		config  types.MonitorConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: types.MonitorConfig{
				Name:     "test-ipv6-route",
				Type:     "network-ipv6-route",
				Interval: 60 * time.Second,
				Timeout:  5 * time.Second,
				Config:   map[string]any{"expectDefaultRoute": true},
			},
			wantErr: false,
		},
		{
			name: "invalid config - bad type",
			config: types.MonitorConfig{
				Name:     "test-ipv6-route",
				Type:     "network-ipv6-route",
				Interval: 60 * time.Second,
				Timeout:  5 * time.Second,
				Config:   map[string]any{"expectDefaultRoute": "invalid"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monitor, err := NewIPv6RouteMonitor(context.Background(), tt.config)

			if (err != nil) != tt.wantErr {
				t.Errorf("NewIPv6RouteMonitor() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && monitor == nil {
				t.Error("NewIPv6RouteMonitor() returned nil monitor")
			}
		})
	}
}

// writeMockIPv6Route creates <procDir>/net/ipv6_route with the given content
// and returns the proc directory root.
func writeMockIPv6Route(t *testing.T, content string) string {
	t.Helper()

	procDir := t.TempDir()
	netDir := filepath.Join(procDir, "net")
	if err := os.MkdirAll(netDir, 0755); err != nil {
		t.Fatalf("Failed to create net dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(netDir, "ipv6_route"), []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write ipv6_route: %v", err)
	}
	return procDir
}

// findIPv6RouteCondition returns the IPv6DefaultRouteMissing condition, or nil.
func findIPv6RouteCondition(status *types.Status) *types.Condition {
	for i := range status.Conditions {
		if status.Conditions[i].Type == "IPv6DefaultRouteMissing" {
			return &status.Conditions[i]
		}
	}
	return nil
}

// ipv6RouteFixture is a route table that contains a default route (line 1) via
// fe80::1 plus several non-default/on-link routes. Mirrors the format of the
// committed testdata fixture.
const ipv6RouteWithDefault = "00000000000000000000000000000000 00 00000000000000000000000000000000 00 fe800000000000000000000000000001 00000400 00000003 00000000 00000003     eth0\n" +
	"2001000000000000000000000000abcd 80 00000000000000000000000000000000 00 00000000000000000000000000000000 00000400 00000001 00000000 00000001     eth0\n" +
	"fe800000000000000000000000000000 40 00000000000000000000000000000000 00 00000000000000000000000000000000 00000100 00000007 00000000 00000001     eth0\n"

// ipv6RouteNoDefault contains only on-link / prefix routes (no default route).
const ipv6RouteNoDefault = "2001000000000000000000000000abcd 80 00000000000000000000000000000000 00 00000000000000000000000000000000 00000400 00000001 00000000 00000001     eth0\n" +
	"fe800000000000000000000000000000 40 00000000000000000000000000000000 00 00000000000000000000000000000000 00000100 00000007 00000000 00000001     eth0\n"

func TestCheckIPv6Route_DefaultPresent(t *testing.T) {
	procDir := writeMockIPv6Route(t, ipv6RouteWithDefault)

	monitor := &IPv6RouteMonitor{
		name: "test-ipv6-route",
		config: &IPv6RouteConfig{
			ExpectDefaultRoute: true,
			ProcPath:           procDir,
		},
	}

	status, err := monitor.checkIPv6Route(context.Background())
	if err != nil {
		t.Fatalf("checkIPv6Route() unexpected error: %v", err)
	}

	cond := findIPv6RouteCondition(status)
	if cond == nil {
		t.Fatal("Expected IPv6DefaultRouteMissing condition, but not found")
	}
	if cond.Status != types.ConditionFalse {
		t.Errorf("Expected IPv6DefaultRouteMissing=False, got %s", cond.Status)
	}
	if !hasEventReason(status, "IPv6DefaultRoutePresent") {
		t.Error("Expected IPv6DefaultRoutePresent event, but not found")
	}
}

func TestCheckIPv6Route_DefaultAbsentExpected(t *testing.T) {
	procDir := writeMockIPv6Route(t, ipv6RouteNoDefault)

	monitor := &IPv6RouteMonitor{
		name: "test-ipv6-route",
		config: &IPv6RouteConfig{
			ExpectDefaultRoute: true,
			ProcPath:           procDir,
		},
	}

	status, err := monitor.checkIPv6Route(context.Background())
	if err != nil {
		t.Fatalf("checkIPv6Route() unexpected error: %v", err)
	}

	cond := findIPv6RouteCondition(status)
	if cond == nil {
		t.Fatal("Expected IPv6DefaultRouteMissing condition, but not found")
	}
	if cond.Status != types.ConditionTrue {
		t.Errorf("Expected IPv6DefaultRouteMissing=True, got %s", cond.Status)
	}
	if cond.Reason != "NoIPv6DefaultRoute" {
		t.Errorf("Expected reason NoIPv6DefaultRoute, got %s", cond.Reason)
	}
	if !hasEventReason(status, "NoIPv6DefaultRoute") {
		t.Error("Expected NoIPv6DefaultRoute event, but not found")
	}
	for _, event := range status.Events {
		if event.Reason == "NoIPv6DefaultRoute" && event.Severity != types.EventWarning {
			t.Errorf("Expected Warning severity for NoIPv6DefaultRoute, got %s", event.Severity)
		}
	}
}

func TestCheckIPv6Route_DefaultAbsentNotExpected(t *testing.T) {
	procDir := writeMockIPv6Route(t, ipv6RouteNoDefault)

	monitor := &IPv6RouteMonitor{
		name: "test-ipv6-route",
		config: &IPv6RouteConfig{
			ExpectDefaultRoute: false,
			ProcPath:           procDir,
		},
	}

	status, err := monitor.checkIPv6Route(context.Background())
	if err != nil {
		t.Fatalf("checkIPv6Route() unexpected error: %v", err)
	}

	cond := findIPv6RouteCondition(status)
	if cond == nil {
		t.Fatal("Expected IPv6DefaultRouteMissing condition, but not found")
	}
	if cond.Status != types.ConditionFalse {
		t.Errorf("Expected IPv6DefaultRouteMissing=False (not expected), got %s", cond.Status)
	}
	if !hasEventReason(status, "IPv6DefaultRouteNotExpected") {
		t.Error("Expected IPv6DefaultRouteNotExpected info event, but not found")
	}
	if hasEventReason(status, "NoIPv6DefaultRoute") {
		t.Error("Did not expect NoIPv6DefaultRoute warning event when expectDefaultRoute=false")
	}
	for _, event := range status.Events {
		if event.Reason == "IPv6DefaultRouteNotExpected" && event.Severity != types.EventInfo {
			t.Errorf("Expected Info severity for IPv6DefaultRouteNotExpected, got %s", event.Severity)
		}
	}
}

func TestCheckIPv6Route_MissingFileExpected(t *testing.T) {
	// procDir exists but has no net/ipv6_route file -> read error becomes a
	// warning, not a hard error.
	procDir := t.TempDir()

	monitor := &IPv6RouteMonitor{
		name: "test-ipv6-route",
		config: &IPv6RouteConfig{
			ExpectDefaultRoute: true,
			ProcPath:           procDir,
		},
	}

	status, err := monitor.checkIPv6Route(context.Background())
	if err != nil {
		t.Fatalf("checkIPv6Route() unexpected error (should not hard error): %v", err)
	}

	if !hasEventReason(status, "IPv6RouteReadError") {
		t.Error("Expected IPv6RouteReadError warning event for missing file, but not found")
	}
	for _, event := range status.Events {
		if event.Reason == "IPv6RouteReadError" && event.Severity != types.EventWarning {
			t.Errorf("Expected Warning severity for IPv6RouteReadError, got %s", event.Severity)
		}
	}

	cond := findIPv6RouteCondition(status)
	if cond == nil {
		t.Fatal("Expected IPv6DefaultRouteMissing condition, but not found")
	}
	if cond.Status != types.ConditionTrue {
		t.Errorf("Expected IPv6DefaultRouteMissing=True (unreadable, expected), got %s", cond.Status)
	}
	if cond.Reason != "IPv6RouteTableUnreadable" {
		t.Errorf("Expected reason IPv6RouteTableUnreadable, got %s", cond.Reason)
	}
}

func TestCheckIPv6Route_MissingFileNotExpected(t *testing.T) {
	procDir := t.TempDir()

	monitor := &IPv6RouteMonitor{
		name: "test-ipv6-route",
		config: &IPv6RouteConfig{
			ExpectDefaultRoute: false,
			ProcPath:           procDir,
		},
	}

	status, err := monitor.checkIPv6Route(context.Background())
	if err != nil {
		t.Fatalf("checkIPv6Route() unexpected error: %v", err)
	}

	// The read error is still surfaced as a warning event.
	if !hasEventReason(status, "IPv6RouteReadError") {
		t.Error("Expected IPv6RouteReadError warning event for missing file, but not found")
	}

	cond := findIPv6RouteCondition(status)
	if cond == nil {
		t.Fatal("Expected IPv6DefaultRouteMissing condition, but not found")
	}
	if cond.Status != types.ConditionFalse {
		t.Errorf("Expected IPv6DefaultRouteMissing=False (unreadable, not expected), got %s", cond.Status)
	}
}

func TestCheckIPv6Route_NonexistentProcPath(t *testing.T) {
	monitor := &IPv6RouteMonitor{
		name: "test-ipv6-route",
		config: &IPv6RouteConfig{
			ExpectDefaultRoute: true,
			ProcPath:           "/nonexistent/proc",
		},
	}

	status, err := monitor.checkIPv6Route(context.Background())
	if err != nil {
		t.Fatalf("checkIPv6Route() unexpected error: %v", err)
	}

	if !hasEventReason(status, "IPv6RouteReadError") {
		t.Error("Expected IPv6RouteReadError event for nonexistent procPath, but not found")
	}

	cond := findIPv6RouteCondition(status)
	if cond == nil {
		t.Fatal("Expected IPv6DefaultRouteMissing condition, but not found")
	}
	if cond.Status != types.ConditionTrue {
		t.Errorf("Expected IPv6DefaultRouteMissing=True for nonexistent procPath, got %s", cond.Status)
	}
}

func TestCheckIPv6Route_TestdataFixture(t *testing.T) {
	// The committed fixture testdata/proc/net/ipv6_route contains a default
	// route via fe80::1, so the monitor reports the route present.
	monitor := &IPv6RouteMonitor{
		name: "test-ipv6-route",
		config: &IPv6RouteConfig{
			ExpectDefaultRoute: true,
			ProcPath:           filepath.Join("testdata", "proc"),
		},
	}

	status, err := monitor.checkIPv6Route(context.Background())
	if err != nil {
		t.Fatalf("checkIPv6Route() unexpected error: %v", err)
	}

	cond := findIPv6RouteCondition(status)
	if cond == nil {
		t.Fatal("Expected IPv6DefaultRouteMissing condition, but not found")
	}
	if cond.Status != types.ConditionFalse {
		t.Errorf("Expected IPv6DefaultRouteMissing=False from fixture with default route, got %s", cond.Status)
	}
	if !hasEventReason(status, "IPv6DefaultRoutePresent") {
		t.Error("Expected IPv6DefaultRoutePresent event from fixture, but not found")
	}
}
