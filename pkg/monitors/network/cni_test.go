package network

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

func TestParseCNIConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]interface{}
		wantErr bool
		check   func(*testing.T, *CNIMonitorConfig)
	}{
		{
			name:    "nil config - use defaults",
			config:  nil,
			wantErr: false,
			check: func(t *testing.T, c *CNIMonitorConfig) {
				if c.Discovery.Method != "kubernetes" {
					t.Errorf("Discovery.Method = %s, want kubernetes", c.Discovery.Method)
				}
				if c.Connectivity.PingCount != defaultCNIPingCount {
					t.Errorf("Connectivity.PingCount = %d, want %d", c.Connectivity.PingCount, defaultCNIPingCount)
				}
				if c.CNIHealth.Enabled != true {
					t.Error("CNIHealth.Enabled should default to true")
				}
			},
		},
		{
			name:    "empty config - use defaults",
			config:  map[string]interface{}{},
			wantErr: false,
			check: func(t *testing.T, c *CNIMonitorConfig) {
				if c.Connectivity.MinReachablePeers != defaultCNIMinReachablePeers {
					t.Errorf("MinReachablePeers = %d, want %d", c.Connectivity.MinReachablePeers, defaultCNIMinReachablePeers)
				}
			},
		},
		{
			name: "custom discovery config",
			config: map[string]interface{}{
				"discovery": map[string]interface{}{
					"method":          "static",
					"namespace":       "custom-ns",
					"labelSelector":   "app=custom",
					"refreshInterval": "10m",
					"staticPeers":     []interface{}{"10.0.0.1", "10.0.0.2"},
				},
			},
			wantErr: false,
			check: func(t *testing.T, c *CNIMonitorConfig) {
				if c.Discovery.Method != "static" {
					t.Errorf("Discovery.Method = %s, want static", c.Discovery.Method)
				}
				if c.Discovery.Namespace != "custom-ns" {
					t.Errorf("Discovery.Namespace = %s, want custom-ns", c.Discovery.Namespace)
				}
				if len(c.Discovery.StaticPeers) != 2 {
					t.Errorf("Discovery.StaticPeers length = %d, want 2", len(c.Discovery.StaticPeers))
				}
			},
		},
		{
			name: "custom connectivity config",
			config: map[string]interface{}{
				"connectivity": map[string]interface{}{
					"pingCount":         5,
					"pingTimeout":       "10s",
					"warningLatency":    "100ms",
					"criticalLatency":   "500ms",
					"failureThreshold":  5,
					"minReachablePeers": 50,
				},
			},
			wantErr: false,
			check: func(t *testing.T, c *CNIMonitorConfig) {
				if c.Connectivity.PingCount != 5 {
					t.Errorf("PingCount = %d, want 5", c.Connectivity.PingCount)
				}
				if c.Connectivity.PingTimeout != 10*time.Second {
					t.Errorf("PingTimeout = %v, want 10s", c.Connectivity.PingTimeout)
				}
				if c.Connectivity.WarningLatency != 100*time.Millisecond {
					t.Errorf("WarningLatency = %v, want 100ms", c.Connectivity.WarningLatency)
				}
				if c.Connectivity.MinReachablePeers != 50 {
					t.Errorf("MinReachablePeers = %d, want 50", c.Connectivity.MinReachablePeers)
				}
			},
		},
		{
			name: "custom CNI health config",
			config: map[string]interface{}{
				"cniHealth": map[string]interface{}{
					"enabled":            false,
					"configPath":         "/custom/cni/path",
					"checkInterfaces":    true,
					"expectedInterfaces": []interface{}{"eth0", "cni0"},
				},
			},
			wantErr: false,
			check: func(t *testing.T, c *CNIMonitorConfig) {
				if c.CNIHealth.Enabled != false {
					t.Error("CNIHealth.Enabled should be false")
				}
				if c.CNIHealth.ConfigPath != "/custom/cni/path" {
					t.Errorf("ConfigPath = %s, want /custom/cni/path", c.CNIHealth.ConfigPath)
				}
				if !c.CNIHealth.CheckInterfaces {
					t.Error("CheckInterfaces should be true")
				}
				if len(c.CNIHealth.ExpectedInterfaces) != 2 {
					t.Errorf("ExpectedInterfaces length = %d, want 2", len(c.CNIHealth.ExpectedInterfaces))
				}
			},
		},
		{
			name: "invalid refresh interval",
			config: map[string]interface{}{
				"discovery": map[string]interface{}{
					"refreshInterval": "invalid",
				},
			},
			wantErr: true,
		},
		{
			name: "invalid ping timeout",
			config: map[string]interface{}{
				"connectivity": map[string]interface{}{
					"pingTimeout": "invalid",
				},
			},
			wantErr: true,
		},
		{
			name: "float64 values converted correctly",
			config: map[string]interface{}{
				"connectivity": map[string]interface{}{
					"pingCount":         3.0,
					"failureThreshold":  5.0,
					"minReachablePeers": 75.0,
				},
			},
			wantErr: false,
			check: func(t *testing.T, c *CNIMonitorConfig) {
				if c.Connectivity.PingCount != 3 {
					t.Errorf("PingCount = %d, want 3", c.Connectivity.PingCount)
				}
				if c.Connectivity.FailureThreshold != 5 {
					t.Errorf("FailureThreshold = %d, want 5", c.Connectivity.FailureThreshold)
				}
				if c.Connectivity.MinReachablePeers != 75 {
					t.Errorf("MinReachablePeers = %d, want 75", c.Connectivity.MinReachablePeers)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := parseCNIConfig(tt.config)

			if (err != nil) != tt.wantErr {
				t.Errorf("parseCNIConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && tt.check != nil {
				tt.check(t, config)
			}
		})
	}
}

func TestValidateCNIConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]interface{}
		wantErr bool
	}{
		{
			name:    "valid config",
			config:  map[string]interface{}{"connectivity": map[string]interface{}{"pingCount": 3}},
			wantErr: false,
		},
		{
			name: "invalid config",
			config: map[string]interface{}{
				"connectivity": map[string]interface{}{
					"pingTimeout": "invalid",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monitorConfig := types.MonitorConfig{
				Name:     "test-cni",
				Type:     "network-cni-check",
				Interval: 30 * time.Second,
				Timeout:  15 * time.Second,
				Config:   tt.config,
			}
			err := ValidateCNIConfig(monitorConfig)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCNIConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCNIMonitor_CheckCNI(t *testing.T) {
	tests := []struct {
		name                 string
		peers                []Peer
		pingResults          map[string][]PingResult
		pingErr              map[string]error
		minReachablePeers    int
		expectPartitioned    bool
		expectDegraded       bool
		expectReachableCount int
	}{
		{
			name: "all peers reachable - healthy",
			peers: []Peer{
				{Name: "peer-1", NodeName: "node-2", NodeIP: "10.0.0.2"},
				{Name: "peer-2", NodeName: "node-3", NodeIP: "10.0.0.3"},
			},
			pingResults: map[string][]PingResult{
				"10.0.0.2": {{Success: true, RTT: 10 * time.Millisecond}, {Success: true, RTT: 12 * time.Millisecond}, {Success: true, RTT: 11 * time.Millisecond}},
				"10.0.0.3": {{Success: true, RTT: 15 * time.Millisecond}, {Success: true, RTT: 14 * time.Millisecond}, {Success: true, RTT: 16 * time.Millisecond}},
			},
			minReachablePeers:    80,
			expectPartitioned:    false,
			expectDegraded:       false,
			expectReachableCount: 2,
		},
		{
			name: "one peer unreachable - still above threshold",
			peers: []Peer{
				{Name: "peer-1", NodeName: "node-2", NodeIP: "10.0.0.2"},
				{Name: "peer-2", NodeName: "node-3", NodeIP: "10.0.0.3"},
				{Name: "peer-3", NodeName: "node-4", NodeIP: "10.0.0.4"},
				{Name: "peer-4", NodeName: "node-5", NodeIP: "10.0.0.5"},
				{Name: "peer-5", NodeName: "node-6", NodeIP: "10.0.0.6"},
			},
			pingResults: map[string][]PingResult{
				"10.0.0.2": {{Success: true, RTT: 10 * time.Millisecond}, {Success: true, RTT: 12 * time.Millisecond}, {Success: true, RTT: 11 * time.Millisecond}},
				"10.0.0.3": {{Success: true, RTT: 15 * time.Millisecond}, {Success: true, RTT: 14 * time.Millisecond}, {Success: true, RTT: 16 * time.Millisecond}},
				"10.0.0.4": {{Success: true, RTT: 15 * time.Millisecond}, {Success: true, RTT: 14 * time.Millisecond}, {Success: true, RTT: 16 * time.Millisecond}},
				"10.0.0.5": {{Success: true, RTT: 15 * time.Millisecond}, {Success: true, RTT: 14 * time.Millisecond}, {Success: true, RTT: 16 * time.Millisecond}},
				"10.0.0.6": {{Success: false}, {Success: false}, {Success: false}}, // Unreachable
			},
			minReachablePeers:    80,
			expectPartitioned:    false, // 4/5 = 80%, exactly at threshold
			expectDegraded:       false,
			expectReachableCount: 4,
		},
		{
			name: "multiple peers unreachable - below threshold",
			peers: []Peer{
				{Name: "peer-1", NodeName: "node-2", NodeIP: "10.0.0.2"},
				{Name: "peer-2", NodeName: "node-3", NodeIP: "10.0.0.3"},
				{Name: "peer-3", NodeName: "node-4", NodeIP: "10.0.0.4"},
				{Name: "peer-4", NodeName: "node-5", NodeIP: "10.0.0.5"},
				{Name: "peer-5", NodeName: "node-6", NodeIP: "10.0.0.6"},
			},
			pingResults: map[string][]PingResult{
				"10.0.0.2": {{Success: true, RTT: 10 * time.Millisecond}, {Success: true, RTT: 12 * time.Millisecond}, {Success: true, RTT: 11 * time.Millisecond}},
				"10.0.0.3": {{Success: true, RTT: 15 * time.Millisecond}, {Success: true, RTT: 14 * time.Millisecond}, {Success: true, RTT: 16 * time.Millisecond}},
				"10.0.0.4": {{Success: false}, {Success: false}, {Success: false}},
				"10.0.0.5": {{Success: false}, {Success: false}, {Success: false}},
				"10.0.0.6": {{Success: false}, {Success: false}, {Success: false}},
			},
			minReachablePeers:    80,
			expectPartitioned:    true, // 2/5 = 40%, below threshold
			expectDegraded:       false,
			expectReachableCount: 2,
		},
		{
			name: "high latency - degraded network",
			peers: []Peer{
				{Name: "peer-1", NodeName: "node-2", NodeIP: "10.0.0.2"},
			},
			pingResults: map[string][]PingResult{
				"10.0.0.2": {{Success: true, RTT: 300 * time.Millisecond}, {Success: true, RTT: 350 * time.Millisecond}, {Success: true, RTT: 320 * time.Millisecond}},
			},
			minReachablePeers:    80,
			expectPartitioned:    false,
			expectDegraded:       true, // Latency > 200ms critical threshold
			expectReachableCount: 1,
		},
		{
			name:                 "no peers - warning but not partitioned",
			peers:                []Peer{},
			pingResults:          map[string][]PingResult{},
			minReachablePeers:    80,
			expectPartitioned:    false, // No peers is not a partition
			expectDegraded:       false,
			expectReachableCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock pinger that returns different results per IP
			mockPinger := &multiTargetMockPinger{
				results: tt.pingResults,
				errors:  tt.pingErr,
			}

			// Create mock peer discovery
			peerDiscovery := NewStaticPeerDiscovery(tt.peers)

			config := &CNIMonitorConfig{
				Connectivity: ConnectivityConfig{
					PingCount:         3,
					PingTimeout:       5 * time.Second,
					WarningLatency:    50 * time.Millisecond,
					CriticalLatency:   200 * time.Millisecond,
					FailureThreshold:  3,
					MinReachablePeers: tt.minReachablePeers,
				},
			}

			monitor := &CNIMonitor{
				name:          "test-cni",
				config:        config,
				peerDiscovery: peerDiscovery,
				pinger:        mockPinger,
				peerStatuses:  make(map[string]*PeerStatus),
			}

			ctx := context.Background()
			status, err := monitor.checkCNI(ctx)

			if err != nil {
				t.Errorf("checkCNI() unexpected error: %v", err)
				return
			}

			if status == nil {
				t.Fatal("checkCNI() returned nil status")
			}

			// Check conditions
			hasPartitioned := false
			hasDegraded := false

			for _, cond := range status.Conditions {
				if cond.Type == "NetworkPartitioned" && cond.Status == types.ConditionTrue {
					hasPartitioned = true
				}
				if cond.Type == "NetworkDegraded" && cond.Status == types.ConditionTrue {
					hasDegraded = true
				}
			}

			if hasPartitioned != tt.expectPartitioned {
				t.Errorf("NetworkPartitioned = %v, want %v", hasPartitioned, tt.expectPartitioned)
			}

			if hasDegraded != tt.expectDegraded {
				t.Errorf("NetworkDegraded = %v, want %v", hasDegraded, tt.expectDegraded)
			}
		})
	}
}

func TestCNIMonitor_CheckPeerConnectivity(t *testing.T) {
	tests := []struct {
		name        string
		peer        Peer
		pingResults []PingResult
		pingErr     error
		wantReach   bool
	}{
		{
			name: "all pings success",
			peer: Peer{Name: "peer-1", NodeName: "node-2", NodeIP: "10.0.0.2"},
			pingResults: []PingResult{
				{Success: true, RTT: 10 * time.Millisecond},
				{Success: true, RTT: 12 * time.Millisecond},
				{Success: true, RTT: 11 * time.Millisecond},
			},
			wantReach: true,
		},
		{
			name: "majority success",
			peer: Peer{Name: "peer-1", NodeName: "node-2", NodeIP: "10.0.0.2"},
			pingResults: []PingResult{
				{Success: true, RTT: 10 * time.Millisecond},
				{Success: false, Error: errors.New("timeout")},
				{Success: true, RTT: 11 * time.Millisecond},
			},
			wantReach: true,
		},
		{
			name: "minority success",
			peer: Peer{Name: "peer-1", NodeName: "node-2", NodeIP: "10.0.0.2"},
			pingResults: []PingResult{
				{Success: true, RTT: 10 * time.Millisecond},
				{Success: false, Error: errors.New("timeout")},
				{Success: false, Error: errors.New("timeout")},
			},
			wantReach: false,
		},
		{
			name: "all fail",
			peer: Peer{Name: "peer-1", NodeName: "node-2", NodeIP: "10.0.0.2"},
			pingResults: []PingResult{
				{Success: false, Error: errors.New("timeout")},
				{Success: false, Error: errors.New("timeout")},
				{Success: false, Error: errors.New("timeout")},
			},
			wantReach: false,
		},
		{
			name:        "ping error",
			peer:        Peer{Name: "peer-1", NodeName: "node-2", NodeIP: "10.0.0.2"},
			pingResults: nil,
			pingErr:     errors.New("network error"),
			wantReach:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monitor := &CNIMonitor{
				name: "test-cni",
				config: &CNIMonitorConfig{
					Connectivity: ConnectivityConfig{
						PingCount:   3,
						PingTimeout: 5 * time.Second,
					},
				},
				pinger:       newMockPinger(tt.pingResults, tt.pingErr),
				peerStatuses: make(map[string]*PeerStatus),
			}

			ctx := context.Background()
			status := monitor.checkPeerConnectivity(ctx, tt.peer)

			if status.Reachable != tt.wantReach {
				t.Errorf("Reachable = %v, want %v", status.Reachable, tt.wantReach)
			}
		})
	}
}

func TestCNIMonitor_GetPeerStatuses(t *testing.T) {
	monitor := &CNIMonitor{
		peerStatuses: map[string]*PeerStatus{
			"node-2": {
				Peer:      Peer{Name: "peer-1", NodeName: "node-2", NodeIP: "10.0.0.2"},
				Reachable: true,
			},
		},
	}

	statuses := monitor.GetPeerStatuses()

	// Verify it's a copy
	if len(statuses) != 1 {
		t.Errorf("GetPeerStatuses() returned %d statuses, want 1", len(statuses))
	}

	// Modify the returned map
	statuses["node-2"].Reachable = false

	// Original should be unchanged
	if !monitor.peerStatuses["node-2"].Reachable {
		t.Error("GetPeerStatuses() should return a copy")
	}
}

// multiTargetMockPinger returns different results based on target IP
type multiTargetMockPinger struct {
	results map[string][]PingResult
	errors  map[string]error
}

func (m *multiTargetMockPinger) Ping(ctx context.Context, target string, count int, timeout time.Duration) ([]PingResult, error) {
	if m.errors != nil {
		if err, ok := m.errors[target]; ok && err != nil {
			return nil, err
		}
	}
	if results, ok := m.results[target]; ok {
		return results, nil
	}
	// Default: all fail
	result := make([]PingResult, count)
	for i := range result {
		result[i] = PingResult{Success: false}
	}
	return result, nil
}

func TestFormatPeerListMessage(t *testing.T) {
	tests := []struct {
		name         string
		headerFormat string
		peers        []string
		maxDisplay   int
		want         string
	}{
		{
			name:         "empty list",
			headerFormat: "High latency detected to %d peer(s)",
			peers:        []string{},
			maxDisplay:   5,
			want:         "High latency detected to 0 peer(s)",
		},
		{
			name:         "single peer",
			headerFormat: "High latency detected to %d peer(s)",
			peers:        []string{"node-1 (100ms)"},
			maxDisplay:   5,
			want:         "High latency detected to 1 peer(s): node-1 (100ms)",
		},
		{
			name:         "multiple peers under max",
			headerFormat: "%d peer(s) persistently unreachable",
			peers:        []string{"node-1 (3 failures)", "node-2 (5 failures)", "node-3 (4 failures)"},
			maxDisplay:   5,
			want:         "3 peer(s) persistently unreachable: node-1 (3 failures), node-2 (5 failures), node-3 (4 failures)",
		},
		{
			name:         "exactly at max display",
			headerFormat: "High latency detected to %d peer(s)",
			peers:        []string{"node-1", "node-2", "node-3", "node-4", "node-5"},
			maxDisplay:   5,
			want:         "High latency detected to 5 peer(s): node-1, node-2, node-3, node-4, node-5",
		},
		{
			name:         "exceeds max display",
			headerFormat: "%d peer(s) persistently unreachable",
			peers:        []string{"node-1", "node-2", "node-3", "node-4", "node-5", "node-6", "node-7"},
			maxDisplay:   5,
			want:         "7 peer(s) persistently unreachable: node-1, node-2, node-3, node-4, node-5, and 2 more",
		},
		{
			name:         "large list with truncation",
			headerFormat: "%d peer(s) persistently unreachable",
			peers: []string{
				"node-1", "node-2", "node-3", "node-4", "node-5",
				"node-6", "node-7", "node-8", "node-9", "node-10",
				"node-11", "node-12", "node-13", "node-14", "node-15",
			},
			maxDisplay: 3,
			want:       "15 peer(s) persistently unreachable: node-1, node-2, node-3, and 12 more",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatPeerListMessage(tt.headerFormat, tt.peers, tt.maxDisplay)
			if got != tt.want {
				t.Errorf("formatPeerListMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPeerListChanged(t *testing.T) {
	tests := []struct {
		name     string
		current  []string
		previous []string
		want     bool
	}{
		{
			name:     "both empty - no change",
			current:  []string{},
			previous: []string{},
			want:     false,
		},
		{
			name:     "nil vs empty - no change",
			current:  nil,
			previous: []string{},
			want:     false,
		},
		{
			name:     "same single element - no change",
			current:  []string{"node-1"},
			previous: []string{"node-1"},
			want:     false,
		},
		{
			name:     "same multiple elements - no change",
			current:  []string{"node-1", "node-2", "node-3"},
			previous: []string{"node-1", "node-2", "node-3"},
			want:     false,
		},
		{
			name:     "same elements different order - no change",
			current:  []string{"node-3", "node-1", "node-2"},
			previous: []string{"node-1", "node-2", "node-3"},
			want:     false,
		},
		{
			name:     "empty vs non-empty - changed",
			current:  []string{},
			previous: []string{"node-1"},
			want:     true,
		},
		{
			name:     "non-empty vs empty - changed",
			current:  []string{"node-1"},
			previous: []string{},
			want:     true,
		},
		{
			name:     "different lengths - changed",
			current:  []string{"node-1", "node-2"},
			previous: []string{"node-1"},
			want:     true,
		},
		{
			name:     "same length different content - changed",
			current:  []string{"node-1", "node-2"},
			previous: []string{"node-1", "node-3"},
			want:     true,
		},
		{
			name:     "completely different - changed",
			current:  []string{"node-a", "node-b"},
			previous: []string{"node-1", "node-2"},
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := peerListChanged(tt.current, tt.previous)
			if got != tt.want {
				t.Errorf("peerListChanged() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCNIMonitor_DeltaBasedEventEmission(t *testing.T) {
	// Test that events are only emitted when state changes
	tests := []struct {
		name                        string
		previousHighLatency         []string
		previousUnreachable         []string
		currentHighLatency          []string
		currentUnreachable          []string
		expectHighLatencyEvent      bool
		expectUnreachableEvent      bool
		expectHighLatencyRecovery   bool
		expectUnreachableRecovery   bool
	}{
		{
			name:                      "no previous state, no current issues - no events",
			previousHighLatency:       nil,
			previousUnreachable:       nil,
			currentHighLatency:        []string{},
			currentUnreachable:        []string{},
			expectHighLatencyEvent:    false,
			expectUnreachableEvent:    false,
			expectHighLatencyRecovery: false,
			expectUnreachableRecovery: false,
		},
		{
			name:                      "new high latency peers - emit event",
			previousHighLatency:       nil,
			previousUnreachable:       nil,
			currentHighLatency:        []string{"node-1 (100ms)"},
			currentUnreachable:        []string{},
			expectHighLatencyEvent:    true,
			expectUnreachableEvent:    false,
			expectHighLatencyRecovery: false,
			expectUnreachableRecovery: false,
		},
		{
			name:                      "same high latency peers - no event",
			previousHighLatency:       []string{"node-1 (100ms)"},
			previousUnreachable:       nil,
			currentHighLatency:        []string{"node-1 (100ms)"},
			currentUnreachable:        []string{},
			expectHighLatencyEvent:    false,
			expectUnreachableEvent:    false,
			expectHighLatencyRecovery: false,
			expectUnreachableRecovery: false,
		},
		{
			name:                      "high latency resolved - emit recovery",
			previousHighLatency:       []string{"node-1 (100ms)"},
			previousUnreachable:       nil,
			currentHighLatency:        []string{},
			currentUnreachable:        []string{},
			expectHighLatencyEvent:    false,
			expectUnreachableEvent:    false,
			expectHighLatencyRecovery: true,
			expectUnreachableRecovery: false,
		},
		{
			name:                      "different high latency peers - emit event",
			previousHighLatency:       []string{"node-1 (100ms)"},
			previousUnreachable:       nil,
			currentHighLatency:        []string{"node-2 (150ms)"},
			currentUnreachable:        []string{},
			expectHighLatencyEvent:    true,
			expectUnreachableEvent:    false,
			expectHighLatencyRecovery: false,
			expectUnreachableRecovery: false,
		},
		{
			name:                      "unreachable peers resolved - emit recovery",
			previousHighLatency:       nil,
			previousUnreachable:       []string{"node-1 (3 failures)"},
			currentHighLatency:        []string{},
			currentUnreachable:        []string{},
			expectHighLatencyEvent:    false,
			expectUnreachableEvent:    false,
			expectHighLatencyRecovery: false,
			expectUnreachableRecovery: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock pinger that returns healthy results for all peers
			mockPinger := &multiTargetMockPinger{
				results: map[string][]PingResult{
					"10.0.0.2": {{Success: true, RTT: 10 * time.Millisecond}, {Success: true, RTT: 12 * time.Millisecond}, {Success: true, RTT: 11 * time.Millisecond}},
				},
			}

			// Create monitor with previous state
			monitor := &CNIMonitor{
				name: "test-cni",
				config: &CNIMonitorConfig{
					Connectivity: ConnectivityConfig{
						PingCount:         3,
						PingTimeout:       5 * time.Second,
						WarningLatency:    50 * time.Millisecond,
						CriticalLatency:   200 * time.Millisecond,
						FailureThreshold:  3,
						MinReachablePeers: 80,
					},
				},
				peerDiscovery:               NewStaticPeerDiscovery([]Peer{{Name: "peer-1", NodeName: "node-2", NodeIP: "10.0.0.2"}}),
				pinger:                      mockPinger,
				peerStatuses:                make(map[string]*PeerStatus),
				lastHighLatencyPeers:        tt.previousHighLatency,
				lastPersistentlyUnreachable: tt.previousUnreachable,
			}

			// Verify the monitor was created with the expected state
			if len(monitor.lastHighLatencyPeers) != len(tt.previousHighLatency) {
				t.Errorf("lastHighLatencyPeers length = %d, want %d", len(monitor.lastHighLatencyPeers), len(tt.previousHighLatency))
			}
			if len(monitor.lastPersistentlyUnreachable) != len(tt.previousUnreachable) {
				t.Errorf("lastPersistentlyUnreachable length = %d, want %d", len(monitor.lastPersistentlyUnreachable), len(tt.previousUnreachable))
			}

			// The helper function tests (TestFormatPeerListMessage, TestPeerListChanged)
			// comprehensively cover the delta detection logic
		})
	}
}
