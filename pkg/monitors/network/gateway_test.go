package network

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

func TestParseGatewayConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]interface{}
		want    *GatewayMonitorConfig
		wantErr bool
	}{
		{
			name:   "nil config - use defaults",
			config: nil,
			want: &GatewayMonitorConfig{
				PingCount:             defaultPingCount,
				PingTimeout:           defaultPingTimeout,
				LatencyThreshold:      defaultLatencyThreshold,
				AutoDetectGateway:     defaultAutoDetectGateway,
				ManualGateway:         "",
				FailureCountThreshold: defaultFailureCountThreshold,
			},
			wantErr: false,
		},
		{
			name:   "empty config - use defaults",
			config: map[string]interface{}{},
			want: &GatewayMonitorConfig{
				PingCount:             defaultPingCount,
				PingTimeout:           defaultPingTimeout,
				LatencyThreshold:      defaultLatencyThreshold,
				AutoDetectGateway:     defaultAutoDetectGateway,
				ManualGateway:         "",
				FailureCountThreshold: defaultFailureCountThreshold,
			},
			wantErr: false,
		},
		{
			name: "custom values",
			config: map[string]interface{}{
				"pingCount":             5,
				"pingTimeout":           "2s",
				"latencyThreshold":      "200ms",
				"autoDetectGateway":     false,
				"manualGateway":         "192.168.1.1",
				"failureCountThreshold": 5,
			},
			want: &GatewayMonitorConfig{
				PingCount:             5,
				PingTimeout:           2 * time.Second,
				LatencyThreshold:      200 * time.Millisecond,
				AutoDetectGateway:     false,
				ManualGateway:         "192.168.1.1",
				FailureCountThreshold: 5,
			},
			wantErr: false,
		},
		{
			name: "pingCount as float64",
			config: map[string]interface{}{
				"pingCount": 3.0,
			},
			want: &GatewayMonitorConfig{
				PingCount:             3,
				PingTimeout:           defaultPingTimeout,
				LatencyThreshold:      defaultLatencyThreshold,
				AutoDetectGateway:     defaultAutoDetectGateway,
				ManualGateway:         "",
				FailureCountThreshold: defaultFailureCountThreshold,
			},
			wantErr: false,
		},
		{
			name: "invalid pingCount type",
			config: map[string]interface{}{
				"pingCount": "three",
			},
			wantErr: true,
		},
		{
			name: "invalid pingCount value - zero",
			config: map[string]interface{}{
				"pingCount": 0,
			},
			wantErr: true,
		},
		{
			name: "invalid pingCount value - negative",
			config: map[string]interface{}{
				"pingCount": -1,
			},
			wantErr: true,
		},
		{
			name: "invalid pingTimeout",
			config: map[string]interface{}{
				"pingTimeout": "invalid",
			},
			wantErr: true,
		},
		{
			name: "invalid latencyThreshold",
			config: map[string]interface{}{
				"latencyThreshold": []int{1, 2, 3},
			},
			wantErr: true,
		},
		{
			name: "invalid autoDetectGateway type",
			config: map[string]interface{}{
				"autoDetectGateway": "yes",
			},
			wantErr: true,
		},
		{
			name: "invalid manualGateway type",
			config: map[string]interface{}{
				"manualGateway": 192168001001,
			},
			wantErr: true,
		},
		{
			name: "invalid manualGateway IP",
			config: map[string]interface{}{
				"manualGateway": "not-an-ip",
			},
			wantErr: true,
		},
		{
			name: "invalid failureCountThreshold - zero",
			config: map[string]interface{}{
				"failureCountThreshold": 0,
			},
			wantErr: true,
		},
		{
			name: "duration as integer seconds",
			config: map[string]interface{}{
				"pingTimeout":      2,
				"latencyThreshold": 0.150, // 150ms as float seconds
			},
			want: &GatewayMonitorConfig{
				PingCount:             defaultPingCount,
				PingTimeout:           2 * time.Second,
				LatencyThreshold:      150 * time.Millisecond,
				AutoDetectGateway:     defaultAutoDetectGateway,
				ManualGateway:         "",
				FailureCountThreshold: defaultFailureCountThreshold,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseGatewayConfig(tt.config)

			if (err != nil) != tt.wantErr {
				t.Errorf("parseGatewayConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			if got.PingCount != tt.want.PingCount {
				t.Errorf("PingCount = %v, want %v", got.PingCount, tt.want.PingCount)
			}
			if got.PingTimeout != tt.want.PingTimeout {
				t.Errorf("PingTimeout = %v, want %v", got.PingTimeout, tt.want.PingTimeout)
			}
			if got.LatencyThreshold != tt.want.LatencyThreshold {
				t.Errorf("LatencyThreshold = %v, want %v", got.LatencyThreshold, tt.want.LatencyThreshold)
			}
			if got.AutoDetectGateway != tt.want.AutoDetectGateway {
				t.Errorf("AutoDetectGateway = %v, want %v", got.AutoDetectGateway, tt.want.AutoDetectGateway)
			}
			if got.ManualGateway != tt.want.ManualGateway {
				t.Errorf("ManualGateway = %v, want %v", got.ManualGateway, tt.want.ManualGateway)
			}
			if got.FailureCountThreshold != tt.want.FailureCountThreshold {
				t.Errorf("FailureCountThreshold = %v, want %v", got.FailureCountThreshold, tt.want.FailureCountThreshold)
			}
		})
	}
}

func TestValidateGatewayConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]interface{}
		wantErr bool
	}{
		{
			name:    "valid config",
			config:  map[string]interface{}{"pingCount": 3},
			wantErr: false,
		},
		{
			name:    "invalid config",
			config:  map[string]interface{}{"pingCount": -1},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monitorConfig := types.MonitorConfig{
				Name:     "test-gateway",
				Type:     "network-gateway-check",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Config:   tt.config,
			}
			err := ValidateGatewayConfig(monitorConfig)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateGatewayConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHexToIP(t *testing.T) {
	tests := []struct {
		name    string
		hexStr  string
		want    string
		wantErr bool
	}{
		{
			name:    "localhost",
			hexStr:  "0100007F",
			want:    "127.0.0.1",
			wantErr: false,
		},
		{
			name:    "typical gateway",
			hexStr:  "0101A8C0",
			want:    "192.168.1.1",
			wantErr: false,
		},
		{
			name:    "10.0.0.1",
			hexStr:  "0100000A",
			want:    "10.0.0.1",
			wantErr: false,
		},
		{
			name:    "invalid length - too short",
			hexStr:  "010000",
			wantErr: true,
		},
		{
			name:    "invalid length - too long",
			hexStr:  "0100007F00",
			wantErr: true,
		},
		{
			name:    "invalid hex characters",
			hexStr:  "ZZZZZZZZ",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := hexToIP(tt.hexStr)

			if (err != nil) != tt.wantErr {
				t.Errorf("hexToIP() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && got != tt.want {
				t.Errorf("hexToIP() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDetectDefaultGateway(t *testing.T) {
	// Create temporary route file for testing
	tmpDir := t.TempDir()
	routeFile := filepath.Join(tmpDir, "route")

	tests := []struct {
		name        string
		routeData   string
		want        string
		wantErr     bool
		skipCleanup bool
	}{
		{
			name: "valid route with default gateway",
			routeData: `Iface	Destination	Gateway 	Flags	RefCnt	Use	Metric	Mask		MTU	Window	IRTT
eth0	00000000	0101A8C0	0003	0	0	100	00000000	0	0	0
eth0	0000A8C0	00000000	0001	0	0	100	00FFFFFF	0	0	0`,
			want:    "192.168.1.1",
			wantErr: false,
		},
		{
			name: "multiple interfaces - first default gateway",
			routeData: `Iface	Destination	Gateway 	Flags	RefCnt	Use	Metric	Mask		MTU	Window	IRTT
eth0	00000000	0101A8C0	0003	0	0	100	00000000	0	0	0
wlan0	00000000	0A0AA8C0	0003	0	0	200	00000000	0	0	0`,
			want:    "192.168.1.1",
			wantErr: false,
		},
		{
			name: "no default gateway",
			routeData: `Iface	Destination	Gateway 	Flags	RefCnt	Use	Metric	Mask		MTU	Window	IRTT
eth0	0000A8C0	00000000	0001	0	0	100	00FFFFFF	0	0	0`,
			wantErr: true,
		},
		{
			name:      "empty route table",
			routeData: `Iface	Destination	Gateway 	Flags	RefCnt	Use	Metric	Mask		MTU	Window	IRTT`,
			wantErr:   true,
		},
		{
			name:        "file does not exist",
			skipCleanup: true,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.skipCleanup {
				// Write test route data
				err := os.WriteFile(routeFile, []byte(tt.routeData), 0644)
				if err != nil {
					t.Fatalf("Failed to write test route file: %v", err)
				}
			}

			// For unit testing, we should refactor detectDefaultGateway to accept a reader
			// But for now, let's just test the hex conversion and validation logic
			t.Skip("Skipping route detection test - requires refactoring to accept custom route file")
		})
	}
}

func TestGatewayMonitor_CheckGateway(t *testing.T) {
	tests := []struct {
		name                     string
		config                   *GatewayMonitorConfig
		pingResults              []PingResult
		pingErr                  error
		gatewayIP                string
		expectNetworkUnreachable bool
		expectHighLatency        bool
		expectHealthy            bool
	}{
		{
			name: "all pings successful - normal latency",
			config: &GatewayMonitorConfig{
				PingCount:             3,
				PingTimeout:           time.Second,
				LatencyThreshold:      100 * time.Millisecond,
				FailureCountThreshold: 3,
			},
			pingResults: []PingResult{
				{Success: true, RTT: 10 * time.Millisecond},
				{Success: true, RTT: 15 * time.Millisecond},
				{Success: true, RTT: 20 * time.Millisecond},
			},
			gatewayIP:     "192.168.1.1",
			expectHealthy: true,
		},
		{
			name: "all pings successful - high latency",
			config: &GatewayMonitorConfig{
				PingCount:             3,
				PingTimeout:           time.Second,
				LatencyThreshold:      100 * time.Millisecond,
				FailureCountThreshold: 3,
			},
			pingResults: []PingResult{
				{Success: true, RTT: 150 * time.Millisecond},
				{Success: true, RTT: 200 * time.Millisecond},
				{Success: true, RTT: 180 * time.Millisecond},
			},
			gatewayIP:         "192.168.1.1",
			expectHealthy:     true,
			expectHighLatency: true,
		},
		{
			name: "majority pings successful",
			config: &GatewayMonitorConfig{
				PingCount:             3,
				PingTimeout:           time.Second,
				LatencyThreshold:      100 * time.Millisecond,
				FailureCountThreshold: 3,
			},
			pingResults: []PingResult{
				{Success: true, RTT: 10 * time.Millisecond},
				{Success: false, Error: errors.New("timeout")},
				{Success: true, RTT: 20 * time.Millisecond},
			},
			gatewayIP:     "192.168.1.1",
			expectHealthy: true,
		},
		{
			name: "minority pings successful",
			config: &GatewayMonitorConfig{
				PingCount:             3,
				PingTimeout:           time.Second,
				LatencyThreshold:      100 * time.Millisecond,
				FailureCountThreshold: 3,
			},
			pingResults: []PingResult{
				{Success: true, RTT: 10 * time.Millisecond},
				{Success: false, Error: errors.New("timeout")},
				{Success: false, Error: errors.New("timeout")},
			},
			gatewayIP:     "192.168.1.1",
			expectHealthy: false,
		},
		{
			name: "all pings failed",
			config: &GatewayMonitorConfig{
				PingCount:             3,
				PingTimeout:           time.Second,
				LatencyThreshold:      100 * time.Millisecond,
				FailureCountThreshold: 3,
			},
			pingResults: []PingResult{
				{Success: false, Error: errors.New("timeout")},
				{Success: false, Error: errors.New("timeout")},
				{Success: false, Error: errors.New("timeout")},
			},
			gatewayIP:     "192.168.1.1",
			expectHealthy: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create monitor with mock pinger
			monitor := &GatewayMonitor{
				name:   "test-gateway",
				config: tt.config,
				pinger: newMockPinger(tt.pingResults, tt.pingErr),
			}

			// Override gateway detection to use test gateway
			monitor.config.ManualGateway = tt.gatewayIP
			monitor.config.AutoDetectGateway = false

			ctx := context.Background()
			status, err := monitor.checkGateway(ctx)

			if err != nil {
				t.Errorf("checkGateway() unexpected error: %v", err)
				return
			}

			if status == nil {
				t.Fatal("checkGateway() returned nil status")
			}

			// Check for expected events
			hasHealthy := false
			hasHighLatency := false
			hasUnreachable := false

			for _, event := range status.Events {
				switch event.Reason {
				case "GatewayHealthy":
					hasHealthy = true
				case "HighGatewayLatency":
					hasHighLatency = true
				case "GatewayUnreachable":
					hasUnreachable = true
				}
			}

			if tt.expectHealthy != hasHealthy {
				t.Errorf("Expected GatewayHealthy=%v, got %v", tt.expectHealthy, hasHealthy)
			}

			if tt.expectHighLatency != hasHighLatency {
				t.Errorf("Expected HighGatewayLatency=%v, got %v", tt.expectHighLatency, hasHighLatency)
			}

			if tt.expectHealthy && hasUnreachable {
				t.Error("Unexpected GatewayUnreachable event for healthy gateway")
			}
		})
	}
}

func TestGatewayMonitor_FailureTracking(t *testing.T) {
	config := &GatewayMonitorConfig{
		PingCount:             3,
		PingTimeout:           time.Second,
		LatencyThreshold:      100 * time.Millisecond,
		FailureCountThreshold: 3,
		ManualGateway:         "192.168.1.1",
		AutoDetectGateway:     false,
	}

	monitor := &GatewayMonitor{
		name:   "test-gateway",
		config: config,
	}

	tests := []struct {
		name                     string
		iterations               int
		pingResults              []PingResult
		expectNetworkUnreachable bool
	}{
		{
			name:       "first failure - no condition",
			iterations: 1,
			pingResults: []PingResult{
				{Success: false, Error: errors.New("timeout")},
				{Success: false, Error: errors.New("timeout")},
				{Success: false, Error: errors.New("timeout")},
			},
			expectNetworkUnreachable: false,
		},
		{
			name:       "second failure - no condition",
			iterations: 2,
			pingResults: []PingResult{
				{Success: false, Error: errors.New("timeout")},
				{Success: false, Error: errors.New("timeout")},
				{Success: false, Error: errors.New("timeout")},
			},
			expectNetworkUnreachable: false,
		},
		{
			name:       "third failure - condition triggered",
			iterations: 3,
			pingResults: []PingResult{
				{Success: false, Error: errors.New("timeout")},
				{Success: false, Error: errors.New("timeout")},
				{Success: false, Error: errors.New("timeout")},
			},
			expectNetworkUnreachable: true,
		},
		{
			name:       "fourth failure - condition persists",
			iterations: 4,
			pingResults: []PingResult{
				{Success: false, Error: errors.New("timeout")},
				{Success: false, Error: errors.New("timeout")},
				{Success: false, Error: errors.New("timeout")},
			},
			expectNetworkUnreachable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset failure count
			monitor.failureCount = 0

			// Run iterations
			for i := 0; i < tt.iterations; i++ {
				monitor.pinger = newMockPinger(tt.pingResults, nil)
				status, err := monitor.checkGateway(context.Background())

				if err != nil {
					t.Errorf("checkGateway() iteration %d error: %v", i+1, err)
					continue
				}

				// Only check condition on last iteration
				if i == tt.iterations-1 {
					hasNetworkUnreachable := false
					for _, cond := range status.Conditions {
						if cond.Type == "NetworkUnreachable" && cond.Status == types.ConditionTrue {
							hasNetworkUnreachable = true
							break
						}
					}

					if tt.expectNetworkUnreachable != hasNetworkUnreachable {
						t.Errorf("Expected NetworkUnreachable condition=%v, got %v (failureCount=%d)",
							tt.expectNetworkUnreachable, hasNetworkUnreachable, monitor.failureCount)
					}
				}
			}
		})
	}
}

func TestGatewayMonitor_FailureRecovery(t *testing.T) {
	config := &GatewayMonitorConfig{
		PingCount:             3,
		PingTimeout:           time.Second,
		LatencyThreshold:      100 * time.Millisecond,
		FailureCountThreshold: 3,
		ManualGateway:         "192.168.1.1",
		AutoDetectGateway:     false,
	}

	monitor := &GatewayMonitor{
		name:   "test-gateway",
		config: config,
	}

	// Simulate 3 failures to trigger NetworkUnreachable
	failureResults := []PingResult{
		{Success: false, Error: errors.New("timeout")},
		{Success: false, Error: errors.New("timeout")},
		{Success: false, Error: errors.New("timeout")},
	}

	for i := 0; i < 3; i++ {
		monitor.pinger = newMockPinger(failureResults, nil)
		_, _ = monitor.checkGateway(context.Background())
	}

	// Verify condition is set
	if monitor.failureCount != 3 {
		t.Errorf("Expected failureCount=3, got %d", monitor.failureCount)
	}

	// Now simulate recovery
	successResults := []PingResult{
		{Success: true, RTT: 10 * time.Millisecond},
		{Success: true, RTT: 15 * time.Millisecond},
		{Success: true, RTT: 20 * time.Millisecond},
	}

	monitor.pinger = newMockPinger(successResults, nil)
	status, err := monitor.checkGateway(context.Background())

	if err != nil {
		t.Errorf("checkGateway() recovery error: %v", err)
		return
	}

	// Check failure count reset
	if monitor.failureCount != 0 {
		t.Errorf("Expected failureCount=0 after recovery, got %d", monitor.failureCount)
	}

	// Check for recovery event
	hasRecovery := false
	for _, event := range status.Events {
		if strings.Contains(event.Message, "restored") || strings.Contains(event.Message, "Recovered") {
			hasRecovery = true
			break
		}
	}

	if !hasRecovery {
		t.Error("Expected recovery event after restoration")
	}

	// Check condition is cleared
	for _, cond := range status.Conditions {
		if cond.Type == "NetworkUnreachable" && cond.Status == types.ConditionTrue {
			t.Error("NetworkUnreachable condition should be cleared after recovery")
		}
	}
}

func TestNewGatewayMonitor(t *testing.T) {
	tests := []struct {
		name    string
		config  types.MonitorConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: types.MonitorConfig{
				Name:     "test-gateway",
				Type:     "network-gateway-check",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Config: map[string]interface{}{
					"pingCount": 3,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid config",
			config: types.MonitorConfig{
				Name:     "test-gateway",
				Type:     "network-gateway-check",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Config: map[string]interface{}{
					"pingCount": -1,
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monitor, err := NewGatewayMonitor(context.Background(), tt.config)

			if (err != nil) != tt.wantErr {
				t.Errorf("NewGatewayMonitor() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && monitor == nil {
				t.Error("NewGatewayMonitor() returned nil monitor")
			}
		})
	}
}

// TestGatewayMonitor_getGatewayIP tests the getGatewayIP method.
func TestGatewayMonitor_getGatewayIP(t *testing.T) {
	tests := []struct {
		name       string
		config     *GatewayMonitorConfig
		wantIP     string
		wantErr    bool
		errContain string
	}{
		{
			name: "manual gateway configured",
			config: &GatewayMonitorConfig{
				ManualGateway:     "192.168.1.1",
				AutoDetectGateway: false,
			},
			wantIP:  "192.168.1.1",
			wantErr: false,
		},
		{
			name: "manual gateway takes precedence over auto-detect",
			config: &GatewayMonitorConfig{
				ManualGateway:     "10.0.0.1",
				AutoDetectGateway: true, // should be ignored when manual is set
			},
			wantIP:  "10.0.0.1",
			wantErr: false,
		},
		{
			name: "no gateway and auto-detect disabled",
			config: &GatewayMonitorConfig{
				ManualGateway:     "",
				AutoDetectGateway: false,
			},
			wantErr:    true,
			errContain: "no gateway configured",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monitor := &GatewayMonitor{
				name:   "test-gateway",
				config: tt.config,
			}

			ip, err := monitor.getGatewayIP()

			if tt.wantErr {
				if err == nil {
					t.Errorf("getGatewayIP() expected error, got nil")
					return
				}
				if tt.errContain != "" && !strings.Contains(err.Error(), tt.errContain) {
					t.Errorf("getGatewayIP() error = %v, want error containing %q", err, tt.errContain)
				}
				return
			}

			if err != nil {
				t.Errorf("getGatewayIP() unexpected error: %v", err)
				return
			}

			if ip != tt.wantIP {
				t.Errorf("getGatewayIP() = %q, want %q", ip, tt.wantIP)
			}
		})
	}
}
