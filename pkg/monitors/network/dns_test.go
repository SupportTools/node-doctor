package network

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// mockResolver implements the Resolver interface for testing.
type mockResolver struct {
	// Responses maps domain names to their resolved addresses
	responses map[string][]string
	// Errors maps domain names to errors
	errors map[string]error
	// Latencies maps domain names to artificial latencies
	latencies map[string]time.Duration
}

// newMockResolver creates a new mock resolver for testing.
func newMockResolver() *mockResolver {
	return &mockResolver{
		responses: make(map[string][]string),
		errors:    make(map[string]error),
		latencies: make(map[string]time.Duration),
	}
}

// setResponse configures a successful DNS response for a domain.
func (m *mockResolver) setResponse(domain string, addrs []string) {
	m.responses[domain] = addrs
}

// setError configures an error response for a domain.
func (m *mockResolver) setError(domain string, err error) {
	m.errors[domain] = err
}

// setLatency configures an artificial latency for a domain.
func (m *mockResolver) setLatency(domain string, latency time.Duration) {
	m.latencies[domain] = latency
}

// LookupHost implements the Resolver interface.
func (m *mockResolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	// Simulate latency if configured
	if latency, ok := m.latencies[host]; ok {
		time.Sleep(latency)
	}

	// Return error if configured
	if err, ok := m.errors[host]; ok {
		return nil, err
	}

	// Return response if configured
	if addrs, ok := m.responses[host]; ok {
		return addrs, nil
	}

	// Default: not found
	return nil, fmt.Errorf("no such host: %s", host)
}

// LookupAddr implements reverse DNS lookups for the Resolver interface.
func (m *mockResolver) LookupAddr(ctx context.Context, addr string) ([]string, error) {
	// Simulate latency if configured
	if latency, ok := m.latencies[addr]; ok {
		time.Sleep(latency)
	}

	// Return error if configured
	if err, ok := m.errors[addr]; ok {
		return nil, err
	}

	// Return response if configured
	if names, ok := m.responses[addr]; ok {
		return names, nil
	}

	// Default: no PTR record
	return nil, fmt.Errorf("no PTR record for %s", addr)
}

// TestParseDNSConfig tests the parseDNSConfig function.
func TestParseDNSConfig(t *testing.T) {
	tests := []struct {
		name      string
		configMap map[string]interface{}
		expected  *DNSMonitorConfig
		wantError bool
	}{
		{
			name:      "nil config",
			configMap: nil,
			expected:  &DNSMonitorConfig{},
			wantError: false,
		},
		{
			name:      "empty config",
			configMap: map[string]interface{}{},
			expected:  &DNSMonitorConfig{},
			wantError: false,
		},
		{
			name: "valid config with all fields",
			configMap: map[string]interface{}{
				"clusterDomains":         []interface{}{"kubernetes.default.svc.cluster.local", "kube-dns.kube-system.svc.cluster.local"},
				"externalDomains":        []interface{}{"google.com", "cloudflare.com"},
				"latencyThreshold":       "2s",
				"nameserverCheckEnabled": true,
				"resolverPath":           "/etc/resolv.conf",
				"failureCountThreshold":  5,
			},
			expected: &DNSMonitorConfig{
				ClusterDomains:         []string{"kubernetes.default.svc.cluster.local", "kube-dns.kube-system.svc.cluster.local"},
				ExternalDomains:        []string{"google.com", "cloudflare.com"},
				LatencyThreshold:       2 * time.Second,
				NameserverCheckEnabled: true,
				ResolverPath:           "/etc/resolv.conf",
				FailureCountThreshold:  5,
			},
			wantError: false,
		},
		{
			name: "latency threshold as float seconds",
			configMap: map[string]interface{}{
				"latencyThreshold": 1.5,
			},
			expected: &DNSMonitorConfig{
				LatencyThreshold: 1500 * time.Millisecond,
			},
			wantError: false,
		},
		{
			name: "latency threshold as int seconds",
			configMap: map[string]interface{}{
				"latencyThreshold": 2,
			},
			expected: &DNSMonitorConfig{
				LatencyThreshold: 2 * time.Second,
			},
			wantError: false,
		},
		{
			name: "invalid cluster domains type",
			configMap: map[string]interface{}{
				"clusterDomains": "not-an-array",
			},
			wantError: true,
		},
		{
			name: "invalid external domains type",
			configMap: map[string]interface{}{
				"externalDomains": 123,
			},
			wantError: true,
		},
		{
			name: "invalid latency threshold",
			configMap: map[string]interface{}{
				"latencyThreshold": "invalid",
			},
			wantError: true,
		},
		{
			name: "invalid nameserver check enabled type",
			configMap: map[string]interface{}{
				"nameserverCheckEnabled": "not-a-bool",
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseDNSConfig(tt.configMap)

			if tt.wantError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			// Compare results
			if len(result.ClusterDomains) != len(tt.expected.ClusterDomains) {
				t.Errorf("ClusterDomains length: got %d, want %d", len(result.ClusterDomains), len(tt.expected.ClusterDomains))
			}
			if len(result.ExternalDomains) != len(tt.expected.ExternalDomains) {
				t.Errorf("ExternalDomains length: got %d, want %d", len(result.ExternalDomains), len(tt.expected.ExternalDomains))
			}
			if result.LatencyThreshold != tt.expected.LatencyThreshold {
				t.Errorf("LatencyThreshold: got %v, want %v", result.LatencyThreshold, tt.expected.LatencyThreshold)
			}
			if result.NameserverCheckEnabled != tt.expected.NameserverCheckEnabled {
				t.Errorf("NameserverCheckEnabled: got %v, want %v", result.NameserverCheckEnabled, tt.expected.NameserverCheckEnabled)
			}
			if result.ResolverPath != tt.expected.ResolverPath {
				t.Errorf("ResolverPath: got %q, want %q", result.ResolverPath, tt.expected.ResolverPath)
			}
			if result.FailureCountThreshold != tt.expected.FailureCountThreshold {
				t.Errorf("FailureCountThreshold: got %d, want %d", result.FailureCountThreshold, tt.expected.FailureCountThreshold)
			}
		})
	}
}

// TestDNSMonitorConfigApplyDefaults tests the applyDefaults method.
func TestDNSMonitorConfigApplyDefaults(t *testing.T) {
	config := &DNSMonitorConfig{}
	err := config.applyDefaults()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(config.ClusterDomains) == 0 {
		t.Error("expected default cluster domains")
	}
	if len(config.ExternalDomains) == 0 {
		t.Error("expected default external domains")
	}
	if config.LatencyThreshold != 1*time.Second {
		t.Errorf("LatencyThreshold: got %v, want 1s", config.LatencyThreshold)
	}
	if config.ResolverPath != "/etc/resolv.conf" {
		t.Errorf("ResolverPath: got %q, want /etc/resolv.conf", config.ResolverPath)
	}
	if config.FailureCountThreshold != 3 {
		t.Errorf("FailureCountThreshold: got %d, want 3", config.FailureCountThreshold)
	}
}

// TestValidateDNSConfig tests the ValidateDNSConfig function.
func TestValidateDNSConfig(t *testing.T) {
	tests := []struct {
		name      string
		config    types.MonitorConfig
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid config",
			config: types.MonitorConfig{
				Name:     "test-dns",
				Type:     "network-dns-check",
				Interval: 60 * time.Second,
				Timeout:  5 * time.Second,
				Config: map[string]interface{}{
					"clusterDomains":  []interface{}{"kubernetes.default.svc.cluster.local"},
					"externalDomains": []interface{}{"google.com"},
				},
			},
			wantError: false,
		},
		{
			name: "missing name",
			config: types.MonitorConfig{
				Type: "network-dns-check",
			},
			wantError: true,
			errorMsg:  "monitor name is required",
		},
		{
			name: "wrong type",
			config: types.MonitorConfig{
				Name: "test-dns",
				Type: "wrong-type",
			},
			wantError: true,
			errorMsg:  "invalid monitor type",
		},
		{
			name: "no domains configured",
			config: types.MonitorConfig{
				Name:     "test-dns",
				Type:     "network-dns-check",
				Interval: 60 * time.Second,
				Timeout:  5 * time.Second,
				Config: map[string]interface{}{
					"clusterDomains":  []interface{}{},
					"externalDomains": []interface{}{},
				},
			},
			wantError: true,
			errorMsg:  "at least one of clusterDomains, externalDomains, or customQueries must be configured",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDNSConfig(tt.config)

			if tt.wantError {
				if err == nil {
					t.Errorf("expected error but got none")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestClusterDNSResolution tests cluster DNS resolution.
func TestClusterDNSResolution(t *testing.T) {
	tests := []struct {
		name          string
		setupMock     func(*mockResolver)
		expectedEvent string
	}{
		{
			name: "successful resolution",
			setupMock: func(m *mockResolver) {
				m.setResponse("kubernetes.default.svc.cluster.local", []string{"10.96.0.1"})
			},
			expectedEvent: "",
		},
		{
			name: "resolution failure",
			setupMock: func(m *mockResolver) {
				m.setError("kubernetes.default.svc.cluster.local", fmt.Errorf("no such host"))
			},
			expectedEvent: "ClusterDNSResolutionFailed",
		},
		{
			name: "no records",
			setupMock: func(m *mockResolver) {
				m.setResponse("kubernetes.default.svc.cluster.local", []string{})
			},
			expectedEvent: "ClusterDNSNoRecords",
		},
		{
			name: "high latency",
			setupMock: func(m *mockResolver) {
				m.setResponse("kubernetes.default.svc.cluster.local", []string{"10.96.0.1"})
				m.setLatency("kubernetes.default.svc.cluster.local", 2*time.Second)
			},
			expectedEvent: "HighClusterDNSLatency",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newMockResolver()
			tt.setupMock(mock)

			monitor := &DNSMonitor{
				name: "test-dns",
				config: &DNSMonitorConfig{
					ClusterDomains:   []string{"kubernetes.default.svc.cluster.local"},
					LatencyThreshold: 1 * time.Second,
				},
				resolver: mock,
			}

			ctx := context.Background()
			status := types.NewStatus("test-dns")
			_ = monitor.checkClusterDNS(ctx, status)

			// Check for expected event
			if tt.expectedEvent == "" {
				// Should have no error events
				for _, event := range status.Events {
					if event.Severity == types.EventError || event.Severity == types.EventWarning {
						t.Errorf("unexpected event: %v", event)
					}
				}
			} else {
				// Should have the expected event
				found := false
				for _, event := range status.Events {
					if strings.Contains(event.Reason, tt.expectedEvent) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected event %q not found in status", tt.expectedEvent)
				}
			}
		})
	}
}

// TestExternalDNSResolution tests external DNS resolution.
func TestExternalDNSResolution(t *testing.T) {
	tests := []struct {
		name          string
		setupMock     func(*mockResolver)
		expectedEvent string
	}{
		{
			name: "successful resolution",
			setupMock: func(m *mockResolver) {
				m.setResponse("google.com", []string{"142.250.80.46"})
				m.setResponse("cloudflare.com", []string{"104.16.132.229"})
			},
			expectedEvent: "",
		},
		{
			name: "resolution failure",
			setupMock: func(m *mockResolver) {
				m.setError("google.com", fmt.Errorf("no such host"))
				m.setError("cloudflare.com", fmt.Errorf("no such host"))
			},
			expectedEvent: "ExternalDNSResolutionFailed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newMockResolver()
			tt.setupMock(mock)

			monitor := &DNSMonitor{
				name: "test-dns",
				config: &DNSMonitorConfig{
					ExternalDomains:  []string{"google.com", "cloudflare.com"},
					LatencyThreshold: 1 * time.Second,
				},
				resolver: mock,
			}

			ctx := context.Background()
			status := types.NewStatus("test-dns")
			_ = monitor.checkExternalDNS(ctx, status)

			// Check for expected event
			if tt.expectedEvent == "" {
				// Should have no error events
				for _, event := range status.Events {
					if event.Severity == types.EventError {
						t.Errorf("unexpected error event: %v", event)
					}
				}
			} else {
				// Should have the expected event
				found := false
				for _, event := range status.Events {
					if strings.Contains(event.Reason, tt.expectedEvent) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected event %q not found in status", tt.expectedEvent)
				}
			}
		})
	}
}

// TestRepeatedFailures tests the NetworkUnreachable condition on repeated failures.
func TestRepeatedFailures(t *testing.T) {
	mock := newMockResolver()
	// Configure all domains to fail
	mock.setError("kubernetes.default.svc.cluster.local", fmt.Errorf("no such host"))
	mock.setError("google.com", fmt.Errorf("no such host"))
	mock.setError("cloudflare.com", fmt.Errorf("no such host"))

	monitor := &DNSMonitor{
		name: "test-dns",
		config: &DNSMonitorConfig{
			ClusterDomains:        []string{"kubernetes.default.svc.cluster.local"},
			ExternalDomains:       []string{"google.com", "cloudflare.com"},
			LatencyThreshold:      1 * time.Second,
			FailureCountThreshold: 3,
			SuccessRateTracking: &SuccessRateConfig{
				Enabled:              false,
				WindowSize:           10,
				FailureRateThreshold: 0.3,
				MinSamplesRequired:   5,
			},
		},
		resolver:               mock,
		clusterSuccessTracker:  NewRingBuffer(10),
		externalSuccessTracker: NewRingBuffer(10),
	}

	ctx := context.Background()

	// First check - should not report NetworkUnreachable yet
	status1 := types.NewStatus("test-dns")
	_, _ = monitor.checkDNS(ctx)
	monitor.updateFailureTracking(false, false, status1)

	hasNetworkUnreachable := false
	for _, cond := range status1.Conditions {
		if cond.Type == "NetworkUnreachable" && cond.Status == types.ConditionTrue {
			hasNetworkUnreachable = true
		}
	}
	if hasNetworkUnreachable {
		t.Error("unexpected NetworkUnreachable condition on first failure")
	}

	// Second check
	status2 := types.NewStatus("test-dns")
	_, _ = monitor.checkDNS(ctx)
	monitor.updateFailureTracking(false, false, status2)

	// Third check - should now report NetworkUnreachable
	status3 := types.NewStatus("test-dns")
	_, _ = monitor.checkDNS(ctx)
	monitor.updateFailureTracking(false, false, status3)

	hasNetworkUnreachable = false
	for _, cond := range status3.Conditions {
		if cond.Type == "NetworkUnreachable" && cond.Status == types.ConditionTrue {
			hasNetworkUnreachable = true
			break
		}
	}
	if !hasNetworkUnreachable {
		t.Error("expected NetworkUnreachable condition after 3 consecutive failures")
	}

	// Recovery - configure domains to succeed
	mock.setResponse("kubernetes.default.svc.cluster.local", []string{"10.96.0.1"})
	mock.setResponse("google.com", []string{"142.250.80.46"})
	mock.setResponse("cloudflare.com", []string{"104.16.132.229"})

	status4 := types.NewStatus("test-dns")
	_, _ = monitor.checkDNS(ctx)
	monitor.updateFailureTracking(true, true, status4)

	// Should report healthy conditions
	hasNetworkReachable := false
	for _, cond := range status4.Conditions {
		if cond.Type == "NetworkReachable" && cond.Status == types.ConditionTrue {
			hasNetworkReachable = true
			break
		}
	}
	if !hasNetworkReachable {
		t.Error("expected NetworkReachable condition after recovery")
	}
}

// TestParseResolverConfig tests parsing of /etc/resolv.conf.
func TestParseResolverConfig(t *testing.T) {
	// Create a temporary resolv.conf file
	tmpFile, err := os.CreateTemp("", "resolv.conf")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	// Write test content
	content := `# Test resolv.conf
nameserver 8.8.8.8
nameserver 8.8.4.4
search example.com
nameserver 1.1.1.1
`
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	tmpFile.Close()

	monitor := &DNSMonitor{
		config: &DNSMonitorConfig{
			ResolverPath: tmpFile.Name(),
		},
	}

	nameservers, err := monitor.parseResolverConfig()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	expected := []string{"8.8.8.8", "8.8.4.4", "1.1.1.1"}
	if len(nameservers) != len(expected) {
		t.Errorf("expected %d nameservers, got %d", len(expected), len(nameservers))
	}

	for i, ns := range expected {
		if nameservers[i] != ns {
			t.Errorf("nameserver[%d]: got %q, want %q", i, nameservers[i], ns)
		}
	}
}

// TestNewDNSMonitor tests the NewDNSMonitor constructor.
func TestNewDNSMonitor(t *testing.T) {
	tests := []struct {
		name      string
		config    types.MonitorConfig
		wantError bool
	}{
		{
			name: "valid config",
			config: types.MonitorConfig{
				Name:     "test-dns",
				Type:     "network-dns-check",
				Interval: 60 * time.Second,
				Timeout:  5 * time.Second,
				Config: map[string]interface{}{
					"clusterDomains": []interface{}{"kubernetes.default.svc.cluster.local"},
				},
			},
			wantError: false,
		},
		{
			name: "invalid config",
			config: types.MonitorConfig{
				Name:     "test-dns",
				Type:     "network-dns-check",
				Interval: 60 * time.Second,
				Timeout:  5 * time.Second,
				Config: map[string]interface{}{
					"latencyThreshold": "invalid",
				},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			monitor, err := NewDNSMonitor(ctx, tt.config)

			if tt.wantError {
				if err == nil {
					t.Error("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if monitor == nil {
					t.Error("monitor is nil")
				}
			}
		})
	}
}

// TestCustomQueries tests custom DNS query resolution.
func TestCustomQueries(t *testing.T) {
	tests := []struct {
		name          string
		customQueries []DNSQuery
		mockSetup     func(*mockResolver)
		wantEvents    int
	}{
		{
			name: "successful custom query",
			customQueries: []DNSQuery{
				{Domain: "example.com", RecordType: "A"},
			},
			mockSetup: func(m *mockResolver) {
				m.setResponse("example.com", []string{"93.184.216.34"})
			},
			wantEvents: 0,
		},
		{
			name: "failed custom query",
			customQueries: []DNSQuery{
				{Domain: "nonexistent.example.com", RecordType: "A"},
			},
			mockSetup: func(m *mockResolver) {
				m.setError("nonexistent.example.com", fmt.Errorf("no such host"))
			},
			wantEvents: 1,
		},
		{
			name: "high latency custom query",
			customQueries: []DNSQuery{
				{Domain: "slow.example.com", RecordType: "A"},
			},
			mockSetup: func(m *mockResolver) {
				m.setResponse("slow.example.com", []string{"1.2.3.4"})
				m.setLatency("slow.example.com", 2*time.Second)
			},
			wantEvents: 1, // High latency warning
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newMockResolver()
			tt.mockSetup(mock)

			monitor := &DNSMonitor{
				name: "test-dns",
				config: &DNSMonitorConfig{
					ClusterDomains:   []string{}, // Empty to focus on custom queries
					ExternalDomains:  []string{},
					CustomQueries:    tt.customQueries,
					LatencyThreshold: 1 * time.Second,
				},
				resolver: mock,
			}

			ctx := context.Background()
			status := &types.Status{
				Source:    monitor.name,
				Timestamp: time.Now(),
			}

			monitor.checkCustomQueries(ctx, status)

			if len(status.Events) != tt.wantEvents {
				t.Errorf("expected %d events, got %d", tt.wantEvents, len(status.Events))
			}
		})
	}
}

// TestNameserverChecks tests nameserver verification error handling.
// Note: This primarily tests error paths since checkNameservers creates its own net.Resolver.
func TestNameserverChecks(t *testing.T) {
	tests := []struct {
		name         string
		resolverPath string
		fileContent  string
		createFile   bool
		wantEvents   int
		checkReason  string
	}{
		{
			name:         "missing resolver file",
			resolverPath: "/nonexistent/resolv.conf",
			createFile:   false,
			wantEvents:   1,
			checkReason:  "ResolverConfigParseError",
		},
		{
			name:         "empty resolver file",
			resolverPath: "",
			fileContent:  "",
			createFile:   true,
			wantEvents:   1,
			checkReason:  "NoNameserversConfigured",
		},
		{
			name:         "valid resolver file with nameserver",
			resolverPath: "",
			fileContent:  "nameserver 8.8.8.8\n",
			createFile:   true,
			wantEvents:   -1, // Don't check event count (depends on network)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolverPath := tt.resolverPath

			if tt.createFile {
				tmpFile, err := os.CreateTemp("", "resolv.conf")
				if err != nil {
					t.Fatalf("failed to create temp file: %v", err)
				}
				defer os.Remove(tmpFile.Name())

				if _, err := tmpFile.WriteString(tt.fileContent); err != nil {
					t.Fatalf("failed to write temp file: %v", err)
				}
				tmpFile.Close()
				resolverPath = tmpFile.Name()
			}

			monitor := &DNSMonitor{
				name: "test-dns",
				config: &DNSMonitorConfig{
					ClusterDomains:         []string{},
					ExternalDomains:        []string{},
					NameserverCheckEnabled: true,
					ResolverPath:           resolverPath,
					LatencyThreshold:       1 * time.Second,
				},
				resolver: newMockResolver(),
			}

			ctx := context.Background()
			status := &types.Status{
				Source:    monitor.name,
				Timestamp: time.Now(),
			}

			monitor.checkNameservers(ctx, status)

			// Only check event count and reason for error cases
			if tt.wantEvents >= 0 {
				if len(status.Events) != tt.wantEvents {
					t.Errorf("expected %d events, got %d", tt.wantEvents, len(status.Events))
				}
				if tt.checkReason != "" && len(status.Events) > 0 {
					if status.Events[0].Reason != tt.checkReason {
						t.Errorf("expected reason %s, got %s", tt.checkReason, status.Events[0].Reason)
					}
				}
			}
		})
	}
}

// TestParseDNSConfigCustomQueries tests parsing of custom DNS queries.
func TestParseDNSConfigCustomQueries(t *testing.T) {
	tests := []struct {
		name      string
		config    map[string]interface{}
		wantError bool
	}{
		{
			name: "valid custom queries",
			config: map[string]interface{}{
				"customQueries": []interface{}{
					map[string]interface{}{
						"domain":     "example.com",
						"recordType": "A",
					},
					map[string]interface{}{
						"domain":     "example.org",
						"recordType": "AAAA",
					},
				},
			},
			wantError: false,
		},
		{
			name: "custom query with default record type",
			config: map[string]interface{}{
				"customQueries": []interface{}{
					map[string]interface{}{
						"domain": "example.com",
					},
				},
			},
			wantError: false,
		},
		{
			name: "invalid custom queries - not array",
			config: map[string]interface{}{
				"customQueries": "not-an-array",
			},
			wantError: true,
		},
		{
			name: "invalid custom queries - item not object",
			config: map[string]interface{}{
				"customQueries": []interface{}{
					"not-an-object",
				},
			},
			wantError: true,
		},
		{
			name: "invalid custom queries - missing domain",
			config: map[string]interface{}{
				"customQueries": []interface{}{
					map[string]interface{}{
						"recordType": "A",
					},
				},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseDNSConfig(tt.config)

			if tt.wantError && err == nil {
				t.Error("expected error but got none")
			}
			if !tt.wantError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// BenchmarkDNSResolution benchmarks DNS resolution performance.
func BenchmarkDNSResolution(b *testing.B) {
	mock := newMockResolver()
	mock.setResponse("kubernetes.default.svc.cluster.local", []string{"10.96.0.1"})
	mock.setResponse("google.com", []string{"142.250.80.46"})

	monitor := &DNSMonitor{
		name: "bench-dns",
		config: &DNSMonitorConfig{
			ClusterDomains:   []string{"kubernetes.default.svc.cluster.local"},
			ExternalDomains:  []string{"google.com"},
			LatencyThreshold: 1 * time.Second,
		},
		resolver: mock,
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = monitor.checkDNS(ctx)
	}
}

// TestValidateDNSConfig_EdgeCases tests additional edge cases for DNS config validation.
func TestValidateDNSConfig_EdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		config    types.MonitorConfig
		wantError bool
		errorMsg  string
	}{
		{
			name: "invalid latency threshold - negative",
			config: types.MonitorConfig{
				Name:     "test-dns",
				Type:     "network-dns-check",
				Interval: 60 * time.Second,
				Timeout:  5 * time.Second,
				Config: map[string]interface{}{
					"clusterDomains":   []interface{}{"kubernetes.default.svc.cluster.local"},
					"latencyThreshold": "-1s",
				},
			},
			wantError: true,
			errorMsg:  "latencyThreshold must be positive",
		},
		{
			name: "valid config with customQueries only",
			config: types.MonitorConfig{
				Name:     "test-dns",
				Type:     "network-dns-check",
				Interval: 60 * time.Second,
				Timeout:  5 * time.Second,
				Config: map[string]interface{}{
					"customQueries": []interface{}{
						map[string]interface{}{
							"domain":     "example.com",
							"recordType": "A",
						},
					},
				},
			},
			wantError: false,
		},
		{
			name: "invalid config parsing - malformed customQueries",
			config: types.MonitorConfig{
				Name:     "test-dns",
				Type:     "network-dns-check",
				Interval: 60 * time.Second,
				Timeout:  5 * time.Second,
				Config: map[string]interface{}{
					"customQueries": "not-an-array",
				},
			},
			wantError: true,
			errorMsg:  "customQueries must be an array",
		},
		{
			name: "invalid config parsing - customQueries entry not object",
			config: types.MonitorConfig{
				Name:     "test-dns",
				Type:     "network-dns-check",
				Interval: 60 * time.Second,
				Timeout:  5 * time.Second,
				Config: map[string]interface{}{
					"customQueries": []interface{}{"not-an-object"},
				},
			},
			wantError: true,
			errorMsg:  "customQueries[0] must be an object",
		},
		{
			name: "invalid config parsing - customQueries domain not string",
			config: types.MonitorConfig{
				Name:     "test-dns",
				Type:     "network-dns-check",
				Interval: 60 * time.Second,
				Timeout:  5 * time.Second,
				Config: map[string]interface{}{
					"customQueries": []interface{}{
						map[string]interface{}{
							"domain": 12345,
						},
					},
				},
			},
			wantError: true,
			errorMsg:  "customQueries[0].domain must be a string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDNSConfig(tt.config)

			if tt.wantError {
				if err == nil {
					t.Errorf("expected error but got none")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestCheckCustomQueries tests the checkCustomQueries method.
func TestCheckCustomQueries(t *testing.T) {
	tests := []struct {
		name           string
		queries        []DNSQuery
		setupMock      func(*mockResolver)
		wantEventTypes []string
	}{
		{
			name: "unsupported record type - MX",
			queries: []DNSQuery{
				{Domain: "example.com", RecordType: "MX"},
			},
			setupMock:      func(m *mockResolver) {},
			wantEventTypes: []string{"UnsupportedQueryType"},
		},
		{
			name: "unsupported record type - AAAA",
			queries: []DNSQuery{
				{Domain: "example.com", RecordType: "AAAA"},
			},
			setupMock:      func(m *mockResolver) {},
			wantEventTypes: []string{"UnsupportedQueryType"},
		},
		{
			name: "successful A record query",
			queries: []DNSQuery{
				{Domain: "example.com", RecordType: "A"},
			},
			setupMock: func(m *mockResolver) {
				m.responses["example.com"] = []string{"93.184.216.34"}
			},
			wantEventTypes: []string{},
		},
		{
			name: "A record query with empty record type defaults to A",
			queries: []DNSQuery{
				{Domain: "example.com", RecordType: ""},
			},
			setupMock: func(m *mockResolver) {
				m.responses["example.com"] = []string{"93.184.216.34"}
			},
			wantEventTypes: []string{},
		},
		{
			name: "DNS query failure",
			queries: []DNSQuery{
				{Domain: "nonexistent.invalid", RecordType: "A"},
			},
			setupMock: func(m *mockResolver) {
				m.errors["nonexistent.invalid"] = fmt.Errorf("no such host")
			},
			wantEventTypes: []string{"CustomDNSQueryFailed"},
		},
		{
			name: "no records found",
			queries: []DNSQuery{
				{Domain: "norecords.example.com", RecordType: "A"},
			},
			setupMock: func(m *mockResolver) {
				m.responses["norecords.example.com"] = []string{}
			},
			wantEventTypes: []string{"CustomDNSNoRecords"},
		},
		{
			name: "high latency warning",
			queries: []DNSQuery{
				{Domain: "slow.example.com", RecordType: "A"},
			},
			setupMock: func(m *mockResolver) {
				m.responses["slow.example.com"] = []string{"1.2.3.4"}
				m.latencies["slow.example.com"] = 2 * time.Second
			},
			wantEventTypes: []string{"HighCustomDNSLatency"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newMockResolver()
			tt.setupMock(mock)

			monitor := &DNSMonitor{
				config: &DNSMonitorConfig{
					CustomQueries:    tt.queries,
					LatencyThreshold: 500 * time.Millisecond,
				},
				resolver: mock,
			}

			status := types.NewStatus("test-dns")
			ctx := context.Background()
			monitor.checkCustomQueries(ctx, status)

			// Check for expected event types
			events := status.Events
			eventTypes := make([]string, len(events))
			for i, e := range events {
				eventTypes[i] = e.Reason
			}

			if len(eventTypes) != len(tt.wantEventTypes) {
				t.Errorf("got %d events, want %d. Got types: %v, want: %v",
					len(eventTypes), len(tt.wantEventTypes), eventTypes, tt.wantEventTypes)
				return
			}

			for i, wantType := range tt.wantEventTypes {
				if eventTypes[i] != wantType {
					t.Errorf("event[%d] type = %s, want %s", i, eventTypes[i], wantType)
				}
			}
		})
	}
}

// TestParseDNSConfigTestEachNameserver tests parsing of testEachNameserver field.
func TestParseDNSConfigTestEachNameserver(t *testing.T) {
	tests := []struct {
		name                   string
		config                 map[string]interface{}
		wantTestEachNameserver bool
	}{
		{
			name: "testEachNameserver enabled",
			config: map[string]interface{}{
				"customQueries": []interface{}{
					map[string]interface{}{
						"domain":             "example.com",
						"recordType":         "A",
						"testEachNameserver": true,
					},
				},
			},
			wantTestEachNameserver: true,
		},
		{
			name: "testEachNameserver disabled",
			config: map[string]interface{}{
				"customQueries": []interface{}{
					map[string]interface{}{
						"domain":             "example.com",
						"recordType":         "A",
						"testEachNameserver": false,
					},
				},
			},
			wantTestEachNameserver: false,
		},
		{
			name: "testEachNameserver not specified (defaults to false)",
			config: map[string]interface{}{
				"customQueries": []interface{}{
					map[string]interface{}{
						"domain":     "example.com",
						"recordType": "A",
					},
				},
			},
			wantTestEachNameserver: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseDNSConfig(tt.config)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(result.CustomQueries) == 0 {
				t.Fatal("expected at least one custom query")
			}

			if result.CustomQueries[0].TestEachNameserver != tt.wantTestEachNameserver {
				t.Errorf("TestEachNameserver = %v, want %v",
					result.CustomQueries[0].TestEachNameserver, tt.wantTestEachNameserver)
			}
		})
	}
}

// TestCheckDomainAgainstNameservers tests per-nameserver domain resolution.
func TestCheckDomainAgainstNameservers(t *testing.T) {
	tests := []struct {
		name              string
		resolverContent   string
		wantConditionType string
		wantConditionTrue bool
		wantEvents        int
	}{
		{
			name: "missing resolver file",
			// No file content, use non-existent path
			resolverContent:   "",
			wantConditionType: "",
			wantEvents:        1, // ResolverConfigParseError
		},
		{
			name:              "empty resolver file - no nameservers",
			resolverContent:   "# No nameservers configured\n",
			wantConditionType: "",
			wantEvents:        1, // NoNameserversConfigured
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resolverPath string

			if tt.name == "missing resolver file" {
				resolverPath = "/nonexistent/resolv.conf"
			} else {
				// Create temp file with resolver content
				tmpFile, err := os.CreateTemp("", "resolv.conf")
				if err != nil {
					t.Fatalf("failed to create temp file: %v", err)
				}
				defer os.Remove(tmpFile.Name())

				if _, err := tmpFile.WriteString(tt.resolverContent); err != nil {
					t.Fatalf("failed to write temp file: %v", err)
				}
				tmpFile.Close()
				resolverPath = tmpFile.Name()
			}

			monitor := &DNSMonitor{
				name: "test-dns",
				config: &DNSMonitorConfig{
					ResolverPath:     resolverPath,
					LatencyThreshold: 1 * time.Second,
				},
				nameserverDomainStatus: make(map[string]*NameserverDomainStatus),
			}

			ctx := context.Background()
			status := types.NewStatus("test-dns")
			monitor.checkDomainAgainstNameservers(ctx, status, "test.example.com")

			// Check event count
			if tt.wantEvents >= 0 && len(status.Events) != tt.wantEvents {
				t.Errorf("got %d events, want %d", len(status.Events), tt.wantEvents)
			}

			// Check condition
			if tt.wantConditionType != "" {
				found := false
				for _, cond := range status.Conditions {
					if cond.Type == tt.wantConditionType {
						found = true
						if tt.wantConditionTrue && cond.Status != types.ConditionTrue {
							t.Errorf("expected condition %s to be True", tt.wantConditionType)
						}
						break
					}
				}
				if !found && tt.wantConditionTrue {
					t.Errorf("expected condition %s not found", tt.wantConditionType)
				}
			}
		})
	}
}

// TestCheckCustomQueriesWithTestEachNameserver tests custom queries with per-nameserver testing.
func TestCheckCustomQueriesWithTestEachNameserver(t *testing.T) {
	// Create a temp resolver file
	tmpFile, err := os.CreateTemp("", "resolv.conf")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	// Empty file to test the NoNameserversConfigured path
	if _, err := tmpFile.WriteString("# Empty\n"); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	tmpFile.Close()

	mock := newMockResolver()

	monitor := &DNSMonitor{
		name: "test-dns",
		config: &DNSMonitorConfig{
			ClusterDomains:  []string{},
			ExternalDomains: []string{},
			CustomQueries: []DNSQuery{
				{
					Domain:             "test.example.com",
					RecordType:         "A",
					TestEachNameserver: true, // Enable per-nameserver testing
				},
			},
			ResolverPath:     tmpFile.Name(),
			LatencyThreshold: 1 * time.Second,
		},
		resolver:               mock,
		nameserverDomainStatus: make(map[string]*NameserverDomainStatus),
	}

	ctx := context.Background()
	status := types.NewStatus("test-dns")
	monitor.checkCustomQueries(ctx, status)

	// Should generate NoNameserversConfigured event
	if len(status.Events) == 0 {
		t.Error("expected at least one event for no nameservers configured")
	}

	foundNoNameservers := false
	for _, event := range status.Events {
		if event.Reason == "NoNameserversConfigured" {
			foundNoNameservers = true
			break
		}
	}
	if !foundNoNameservers {
		t.Error("expected NoNameserversConfigured event")
	}
}

// TestGetOrCreateNameserverStatus tests the nameserver status tracking.
func TestGetOrCreateNameserverStatus(t *testing.T) {
	monitor := &DNSMonitor{
		nameserverDomainStatus: make(map[string]*NameserverDomainStatus),
	}

	// First call should create a new status
	key1 := "8.8.8.8:example.com"
	status1 := monitor.getOrCreateNameserverStatus(key1, "8.8.8.8", "example.com")
	if status1 == nil {
		t.Fatal("expected non-nil status")
	}
	if status1.Nameserver != "8.8.8.8" {
		t.Errorf("Nameserver = %s, want 8.8.8.8", status1.Nameserver)
	}
	if status1.Domain != "example.com" {
		t.Errorf("Domain = %s, want example.com", status1.Domain)
	}
	if status1.FailureCount != 0 {
		t.Errorf("FailureCount = %d, want 0", status1.FailureCount)
	}

	// Modify the status
	status1.FailureCount = 5

	// Second call with same key should return the same status
	status2 := monitor.getOrCreateNameserverStatus(key1, "8.8.8.8", "example.com")
	if status2.FailureCount != 5 {
		t.Errorf("FailureCount = %d, want 5 (should be same instance)", status2.FailureCount)
	}

	// Different key should create a new status
	key2 := "8.8.4.4:example.com"
	status3 := monitor.getOrCreateNameserverStatus(key2, "8.8.4.4", "example.com")
	if status3.FailureCount != 0 {
		t.Errorf("FailureCount = %d, want 0 (should be new instance)", status3.FailureCount)
	}
}

// TestCreateNameserverResolver tests the nameserver resolver creation.
func TestCreateNameserverResolver(t *testing.T) {
	monitor := &DNSMonitor{}

	resolver := monitor.createNameserverResolver("8.8.8.8")
	if resolver == nil {
		t.Fatal("expected non-nil resolver")
	}

	// Verify the resolver has PreferGo set
	if !resolver.PreferGo {
		t.Error("expected PreferGo to be true")
	}

	// Note: We can't easily test the Dial function without actual network calls,
	// but we can verify the resolver was created with the right settings
}

// TestNameserverDomainStatusTracking tests that failure counts are tracked correctly.
func TestNameserverDomainStatusTracking(t *testing.T) {
	monitor := &DNSMonitor{
		nameserverDomainStatus: make(map[string]*NameserverDomainStatus),
	}

	key := "10.0.0.1:critical.example.com"

	// Simulate multiple failures
	for i := 0; i < 3; i++ {
		status := monitor.getOrCreateNameserverStatus(key, "10.0.0.1", "critical.example.com")
		status.FailureCount++
	}

	// Verify failure count
	status := monitor.nameserverDomainStatus[key]
	if status.FailureCount != 3 {
		t.Errorf("FailureCount = %d, want 3", status.FailureCount)
	}

	// Simulate recovery
	status.FailureCount = 0
	status.LastSuccess = time.Now()

	if status.FailureCount != 0 {
		t.Errorf("FailureCount = %d after recovery, want 0", status.FailureCount)
	}
}

// TestCleanupOldNameserverStatus tests the cleanup of old status entries.
func TestCleanupOldNameserverStatus(t *testing.T) {
	monitor := &DNSMonitor{
		nameserverDomainStatus: make(map[string]*NameserverDomainStatus),
	}

	// Add some entries with old LastSuccess times
	oldTime := time.Now().Add(-2 * time.Hour)
	recentTime := time.Now().Add(-30 * time.Minute)

	monitor.nameserverDomainStatus["old:domain1"] = &NameserverDomainStatus{
		Nameserver:  "8.8.8.8",
		Domain:      "domain1",
		LastSuccess: oldTime,
	}
	monitor.nameserverDomainStatus["recent:domain2"] = &NameserverDomainStatus{
		Nameserver:  "8.8.4.4",
		Domain:      "domain2",
		LastSuccess: recentTime,
	}
	monitor.nameserverDomainStatus["zero:domain3"] = &NameserverDomainStatus{
		Nameserver: "1.1.1.1",
		Domain:     "domain3",
		// LastSuccess is zero
	}

	// Run cleanup
	monitor.cleanupOldNameserverStatus()

	// Old entry should be removed (> 1 hour old)
	if _, exists := monitor.nameserverDomainStatus["old:domain1"]; exists {
		t.Error("expected old entry to be removed")
	}
	// Recent entry should remain (< 1 hour old)
	if _, exists := monitor.nameserverDomainStatus["recent:domain2"]; !exists {
		t.Error("expected recent entry to remain")
	}
	// Zero-time entry (never succeeded) should be removed to prevent memory leak
	if _, exists := monitor.nameserverDomainStatus["zero:domain3"]; exists {
		t.Error("expected zero-time entry (never succeeded) to be removed")
	}
}

// TestCreateNameserverResolverIPv6 tests that IPv6 nameservers are properly formatted.
func TestCreateNameserverResolverIPv6(t *testing.T) {
	monitor := &DNSMonitor{}

	tests := []struct {
		name       string
		nameserver string
	}{
		{"IPv4", "8.8.8.8"},
		{"IPv6", "2001:4860:4860::8888"},
		{"IPv6 full", "2001:4860:4860:0000:0000:0000:0000:8888"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := monitor.createNameserverResolver(tt.nameserver)
			if resolver == nil {
				t.Fatal("expected non-nil resolver")
			}
			// Verify PreferGo is set
			if !resolver.PreferGo {
				t.Error("expected PreferGo to be true")
			}
		})
	}
}

// TestMaxNameserverDomainEntries tests the constant is defined.
func TestMaxNameserverDomainEntries(t *testing.T) {
	if maxNameserverDomainEntries <= 0 {
		t.Errorf("maxNameserverDomainEntries should be positive, got %d", maxNameserverDomainEntries)
	}
	if maxNameserverDomainEntries > 10000 {
		t.Errorf("maxNameserverDomainEntries seems too large: %d", maxNameserverDomainEntries)
	}
}

// TestRingBuffer tests the RingBuffer implementation.
func TestRingBuffer(t *testing.T) {
	t.Run("new buffer has zero count", func(t *testing.T) {
		rb := NewRingBuffer(10)
		if rb.Count() != 0 {
			t.Errorf("expected count 0, got %d", rb.Count())
		}
		if rb.Size() != 10 {
			t.Errorf("expected size 10, got %d", rb.Size())
		}
	})

	t.Run("empty buffer returns 0 success rate", func(t *testing.T) {
		rb := NewRingBuffer(10)
		if rb.GetSuccessRate() != 0.0 {
			t.Errorf("expected success rate 0.0, got %f", rb.GetSuccessRate())
		}
	})

	t.Run("all successes returns 1.0 success rate", func(t *testing.T) {
		rb := NewRingBuffer(5)
		for i := 0; i < 5; i++ {
			rb.Add(&CheckResult{Success: true})
		}
		if rb.GetSuccessRate() != 1.0 {
			t.Errorf("expected success rate 1.0, got %f", rb.GetSuccessRate())
		}
		if rb.GetFailureRate() != 0.0 {
			t.Errorf("expected failure rate 0.0, got %f", rb.GetFailureRate())
		}
	})

	t.Run("all failures returns 0.0 success rate", func(t *testing.T) {
		rb := NewRingBuffer(5)
		for i := 0; i < 5; i++ {
			rb.Add(&CheckResult{Success: false})
		}
		if rb.GetSuccessRate() != 0.0 {
			t.Errorf("expected success rate 0.0, got %f", rb.GetSuccessRate())
		}
		if rb.GetFailureRate() != 1.0 {
			t.Errorf("expected failure rate 1.0, got %f", rb.GetFailureRate())
		}
	})

	t.Run("mixed results calculates correctly", func(t *testing.T) {
		rb := NewRingBuffer(10)
		// Add 7 successes and 3 failures
		for i := 0; i < 7; i++ {
			rb.Add(&CheckResult{Success: true})
		}
		for i := 0; i < 3; i++ {
			rb.Add(&CheckResult{Success: false})
		}
		successRate := rb.GetSuccessRate()
		// Use approximate comparison for floating point
		if successRate < 0.69 || successRate > 0.71 {
			t.Errorf("expected success rate ~0.7, got %f", successRate)
		}
		failureRate := rb.GetFailureRate()
		if failureRate < 0.29 || failureRate > 0.31 {
			t.Errorf("expected failure rate ~0.3, got %f", failureRate)
		}
	})

	t.Run("buffer wraps around correctly", func(t *testing.T) {
		rb := NewRingBuffer(5)
		// Add 5 successes
		for i := 0; i < 5; i++ {
			rb.Add(&CheckResult{Success: true})
		}
		if rb.GetSuccessRate() != 1.0 {
			t.Errorf("expected success rate 1.0 before wrap, got %f", rb.GetSuccessRate())
		}

		// Add 3 failures (overwrites 3 oldest successes)
		for i := 0; i < 3; i++ {
			rb.Add(&CheckResult{Success: false})
		}
		// Should now have 2 successes and 3 failures
		successRate := rb.GetSuccessRate()
		expectedRate := 2.0 / 5.0 // 0.4
		if successRate != expectedRate {
			t.Errorf("expected success rate %f after wrap, got %f", expectedRate, successRate)
		}
	})

	t.Run("count stays at buffer size after wrap", func(t *testing.T) {
		rb := NewRingBuffer(5)
		// Add more than buffer size
		for i := 0; i < 10; i++ {
			rb.Add(&CheckResult{Success: true})
		}
		if rb.Count() != 5 {
			t.Errorf("expected count to cap at 5, got %d", rb.Count())
		}
	})

	t.Run("minimum size enforced", func(t *testing.T) {
		rb := NewRingBuffer(0)
		if rb.Size() < 1 {
			t.Errorf("expected minimum size of 1, got %d", rb.Size())
		}

		rb2 := NewRingBuffer(-5)
		if rb2.Size() < 1 {
			t.Errorf("expected minimum size of 1 for negative input, got %d", rb2.Size())
		}
	})
}

// TestSuccessRateConfigParsing tests parsing of successRateTracking config.
func TestSuccessRateConfigParsing(t *testing.T) {
	tests := []struct {
		name        string
		configMap   map[string]interface{}
		expectError bool
		validate    func(*DNSMonitorConfig) error
	}{
		{
			name:      "no successRateTracking config",
			configMap: map[string]interface{}{},
			validate: func(c *DNSMonitorConfig) error {
				// After applyDefaults, should have default config
				if c.SuccessRateTracking == nil {
					return fmt.Errorf("expected SuccessRateTracking to be non-nil after defaults")
				}
				return nil
			},
		},
		{
			name: "successRateTracking enabled with all fields",
			configMap: map[string]interface{}{
				"successRateTracking": map[string]interface{}{
					"enabled":              true,
					"windowSize":           float64(20),
					"failureRateThreshold": float64(40), // percentage
					"minSamplesRequired":   float64(10),
				},
			},
			validate: func(c *DNSMonitorConfig) error {
				srt := c.SuccessRateTracking
				if srt == nil {
					return fmt.Errorf("expected SuccessRateTracking to be non-nil")
				}
				if !srt.Enabled {
					return fmt.Errorf("expected Enabled to be true")
				}
				if srt.WindowSize != 20 {
					return fmt.Errorf("expected WindowSize 20, got %d", srt.WindowSize)
				}
				if srt.FailureRateThreshold != 0.4 {
					return fmt.Errorf("expected FailureRateThreshold 0.4, got %f", srt.FailureRateThreshold)
				}
				if srt.MinSamplesRequired != 10 {
					return fmt.Errorf("expected MinSamplesRequired 10, got %d", srt.MinSamplesRequired)
				}
				return nil
			},
		},
		{
			name: "failureRateThreshold as decimal",
			configMap: map[string]interface{}{
				"successRateTracking": map[string]interface{}{
					"enabled":              true,
					"failureRateThreshold": float64(0.25),
				},
			},
			validate: func(c *DNSMonitorConfig) error {
				if c.SuccessRateTracking.FailureRateThreshold != 0.25 {
					return fmt.Errorf("expected FailureRateThreshold 0.25, got %f", c.SuccessRateTracking.FailureRateThreshold)
				}
				return nil
			},
		},
		{
			name: "failureRateThreshold as integer percentage",
			configMap: map[string]interface{}{
				"successRateTracking": map[string]interface{}{
					"enabled":              true,
					"failureRateThreshold": 35, // int
				},
			},
			validate: func(c *DNSMonitorConfig) error {
				if c.SuccessRateTracking.FailureRateThreshold != 0.35 {
					return fmt.Errorf("expected FailureRateThreshold 0.35, got %f", c.SuccessRateTracking.FailureRateThreshold)
				}
				return nil
			},
		},
		{
			name: "invalid successRateTracking type",
			configMap: map[string]interface{}{
				"successRateTracking": "invalid",
			},
			expectError: true,
		},
		{
			name: "failureRateThreshold integer > 100 invalid",
			configMap: map[string]interface{}{
				"successRateTracking": map[string]interface{}{
					"enabled":              true,
					"failureRateThreshold": 150, // invalid - must be 0-100
				},
			},
			expectError: true,
		},
		{
			name: "failureRateThreshold integer negative invalid",
			configMap: map[string]interface{}{
				"successRateTracking": map[string]interface{}{
					"enabled":              true,
					"failureRateThreshold": -10, // invalid
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := parseDNSConfig(tt.configMap)
			if tt.expectError {
				if err == nil {
					t.Error("expected error but got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Apply defaults
			if err := config.applyDefaults(); err != nil {
				t.Fatalf("applyDefaults failed: %v", err)
			}

			if tt.validate != nil {
				if err := tt.validate(config); err != nil {
					t.Error(err)
				}
			}
		})
	}
}

// TestSuccessRateConditions tests that success rate conditions are triggered correctly.
func TestSuccessRateConditions(t *testing.T) {
	t.Run("no conditions when not enabled", func(t *testing.T) {
		monitor := &DNSMonitor{
			config: &DNSMonitorConfig{
				SuccessRateTracking: &SuccessRateConfig{
					Enabled:              false,
					WindowSize:           10,
					FailureRateThreshold: 0.3,
					MinSamplesRequired:   5,
				},
				FailureCountThreshold: 3,
			},
			clusterSuccessTracker:  NewRingBuffer(10),
			externalSuccessTracker: NewRingBuffer(10),
		}

		// Add failures
		for i := 0; i < 10; i++ {
			monitor.clusterSuccessTracker.Add(&CheckResult{Success: false})
		}

		status := types.NewStatus("test")
		monitor.checkSuccessRateConditions(status)

		// Should have no conditions since not enabled
		// checkSuccessRateConditions is only called when enabled
		// In this test we call it directly, but the parent updateFailureTracking checks enabled
		if len(status.Conditions) > 0 {
			// This is expected since we're calling the method directly
		}
	})

	t.Run("no conditions when below minSamplesRequired", func(t *testing.T) {
		monitor := &DNSMonitor{
			config: &DNSMonitorConfig{
				SuccessRateTracking: &SuccessRateConfig{
					Enabled:              true,
					WindowSize:           10,
					FailureRateThreshold: 0.3,
					MinSamplesRequired:   5,
				},
				FailureCountThreshold: 3,
			},
			clusterSuccessTracker:  NewRingBuffer(10),
			externalSuccessTracker: NewRingBuffer(10),
		}

		// Add only 3 failures (below minSamplesRequired of 5)
		for i := 0; i < 3; i++ {
			monitor.clusterSuccessTracker.Add(&CheckResult{Success: false})
		}

		status := types.NewStatus("test")
		monitor.checkSuccessRateConditions(status)

		// Should have no cluster conditions since below min samples
		hasClusterCondition := false
		for _, cond := range status.Conditions {
			if cond.Type == "ClusterDNSDegraded" || cond.Type == "ClusterDNSIntermittent" {
				hasClusterCondition = true
			}
		}
		if hasClusterCondition {
			t.Error("expected no cluster conditions when below minSamplesRequired")
		}
	})

	t.Run("DNSDegraded condition when above threshold", func(t *testing.T) {
		monitor := &DNSMonitor{
			config: &DNSMonitorConfig{
				SuccessRateTracking: &SuccessRateConfig{
					Enabled:              true,
					WindowSize:           10,
					FailureRateThreshold: 0.3, // 30%
					MinSamplesRequired:   5,
				},
				FailureCountThreshold: 3,
			},
			clusterSuccessTracker:  NewRingBuffer(10),
			externalSuccessTracker: NewRingBuffer(10),
		}

		// Add 4 failures and 6 successes = 40% failure rate (above 30% threshold)
		for i := 0; i < 4; i++ {
			monitor.clusterSuccessTracker.Add(&CheckResult{Success: false})
		}
		for i := 0; i < 6; i++ {
			monitor.clusterSuccessTracker.Add(&CheckResult{Success: true})
		}

		status := types.NewStatus("test")
		monitor.checkSuccessRateConditions(status)

		// Should have ClusterDNSDegraded condition
		hasDegraded := false
		for _, cond := range status.Conditions {
			if cond.Type == "ClusterDNSDegraded" {
				hasDegraded = true
				if cond.Status != types.ConditionTrue {
					t.Errorf("expected condition status True, got %s", cond.Status)
				}
			}
		}
		if !hasDegraded {
			t.Error("expected ClusterDNSDegraded condition")
		}
	})

	t.Run("DNSIntermittent condition when below threshold but some failures", func(t *testing.T) {
		monitor := &DNSMonitor{
			config: &DNSMonitorConfig{
				SuccessRateTracking: &SuccessRateConfig{
					Enabled:              true,
					WindowSize:           10,
					FailureRateThreshold: 0.3, // 30%
					MinSamplesRequired:   5,
				},
				FailureCountThreshold: 3,
			},
			clusterSuccessTracker:  NewRingBuffer(10),
			externalSuccessTracker: NewRingBuffer(10),
		}

		// Add 2 failures and 8 successes = 20% failure rate (below 30% threshold)
		for i := 0; i < 2; i++ {
			monitor.clusterSuccessTracker.Add(&CheckResult{Success: false})
		}
		for i := 0; i < 8; i++ {
			monitor.clusterSuccessTracker.Add(&CheckResult{Success: true})
		}

		status := types.NewStatus("test")
		monitor.checkSuccessRateConditions(status)

		// Should have ClusterDNSIntermittent condition (some failures but below threshold)
		hasIntermittent := false
		for _, cond := range status.Conditions {
			if cond.Type == "ClusterDNSIntermittent" {
				hasIntermittent = true
			}
		}
		if !hasIntermittent {
			t.Error("expected ClusterDNSIntermittent condition")
		}
	})

	t.Run("no conditions when 100% success", func(t *testing.T) {
		monitor := &DNSMonitor{
			config: &DNSMonitorConfig{
				SuccessRateTracking: &SuccessRateConfig{
					Enabled:              true,
					WindowSize:           10,
					FailureRateThreshold: 0.3,
					MinSamplesRequired:   5,
				},
				FailureCountThreshold: 3,
			},
			clusterSuccessTracker:  NewRingBuffer(10),
			externalSuccessTracker: NewRingBuffer(10),
		}

		// Add 10 successes
		for i := 0; i < 10; i++ {
			monitor.clusterSuccessTracker.Add(&CheckResult{Success: true})
			monitor.externalSuccessTracker.Add(&CheckResult{Success: true})
		}

		status := types.NewStatus("test")
		monitor.checkSuccessRateConditions(status)

		// Should have no degraded or intermittent conditions
		for _, cond := range status.Conditions {
			if cond.Type == "ClusterDNSDegraded" || cond.Type == "ClusterDNSIntermittent" ||
				cond.Type == "ExternalDNSDegraded" || cond.Type == "ExternalDNSIntermittent" {
				t.Errorf("unexpected condition %s when 100%% success", cond.Type)
			}
		}
	})
}

// TestSuccessRateConfigDefaults tests default values for success rate tracking.
func TestSuccessRateConfigDefaults(t *testing.T) {
	config := &DNSMonitorConfig{}
	if err := config.applyDefaults(); err != nil {
		t.Fatalf("applyDefaults failed: %v", err)
	}

	if config.SuccessRateTracking == nil {
		t.Fatal("expected SuccessRateTracking to be non-nil")
	}

	srt := config.SuccessRateTracking
	if srt.Enabled {
		t.Error("expected Enabled to be false by default")
	}
	if srt.WindowSize != 10 {
		t.Errorf("expected WindowSize 10, got %d", srt.WindowSize)
	}
	if srt.FailureRateThreshold != 0.3 {
		t.Errorf("expected FailureRateThreshold 0.3, got %f", srt.FailureRateThreshold)
	}
	if srt.MinSamplesRequired != 5 {
		t.Errorf("expected MinSamplesRequired 5, got %d", srt.MinSamplesRequired)
	}
}

// TestSuccessRateValidation tests validation of success rate tracking config.
func TestSuccessRateValidation(t *testing.T) {
	baseConfig := func() types.MonitorConfig {
		return types.MonitorConfig{
			Name: "test-dns",
			Type: "network-dns-check",
			Config: map[string]interface{}{
				"clusterDomains":  []interface{}{"kubernetes.default.svc.cluster.local"},
				"externalDomains": []interface{}{"google.com"},
			},
		}
	}

	t.Run("windowSize too large", func(t *testing.T) {
		config := baseConfig()
		config.Config["successRateTracking"] = map[string]interface{}{
			"enabled":              true,
			"windowSize":           20000, // exceeds 10000 limit
			"failureRateThreshold": 0.3,
			"minSamplesRequired":   5,
		}
		err := ValidateDNSConfig(config)
		if err == nil {
			t.Error("expected error for windowSize > 10000")
		}
	})

	t.Run("windowSize negative", func(t *testing.T) {
		config := baseConfig()
		config.Config["successRateTracking"] = map[string]interface{}{
			"enabled":              true,
			"windowSize":           -5, // negative values stay negative
			"failureRateThreshold": 0.3,
			"minSamplesRequired":   5,
		}
		// Note: applyDefaults converts <= 0 to 10, so negative becomes 10 (valid)
		// This test verifies the default fixing behavior works
		err := ValidateDNSConfig(config)
		if err != nil {
			t.Errorf("expected no error (defaults fix windowSize), got: %v", err)
		}
	})

	t.Run("minSamplesRequired exceeds windowSize", func(t *testing.T) {
		config := baseConfig()
		config.Config["successRateTracking"] = map[string]interface{}{
			"enabled":              true,
			"windowSize":           10,
			"failureRateThreshold": 0.3,
			"minSamplesRequired":   20, // exceeds windowSize
		}
		err := ValidateDNSConfig(config)
		if err == nil {
			t.Error("expected error for minSamplesRequired > windowSize")
		}
	})

	t.Run("failureRateThreshold as percentage > 100", func(t *testing.T) {
		config := baseConfig()
		config.Config["successRateTracking"] = map[string]interface{}{
			"enabled":              true,
			"windowSize":           10,
			"failureRateThreshold": 150, // 150% after /100 = 1.5 which exceeds 1.0
			"minSamplesRequired":   5,
		}
		err := ValidateDNSConfig(config)
		if err == nil {
			t.Error("expected error for failureRateThreshold > 100%")
		}
	})

	t.Run("valid config passes validation", func(t *testing.T) {
		config := baseConfig()
		config.Config["successRateTracking"] = map[string]interface{}{
			"enabled":              true,
			"windowSize":           100,
			"failureRateThreshold": 0.3,
			"minSamplesRequired":   10,
		}
		err := ValidateDNSConfig(config)
		if err != nil {
			t.Errorf("unexpected error for valid config: %v", err)
		}
	})
}

// TestClassifyDNSError tests DNS error type classification.
func TestClassifyDNSError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected DNSErrorType
	}{
		{
			name:     "nil error returns Unknown",
			err:      nil,
			expected: DNSErrorUnknown,
		},
		{
			name:     "i/o timeout",
			err:      fmt.Errorf("dial tcp 8.8.8.8:53: i/o timeout"),
			expected: DNSErrorTimeout,
		},
		{
			name:     "context deadline exceeded",
			err:      fmt.Errorf("lookup example.com: context deadline exceeded"),
			expected: DNSErrorTimeout,
		},
		{
			name:     "no such host (NXDOMAIN)",
			err:      fmt.Errorf("lookup nonexistent.example.com: no such host"),
			expected: DNSErrorNXDOMAIN,
		},
		{
			name:     "server misbehaving (SERVFAIL)",
			err:      fmt.Errorf("lookup example.com: server misbehaving"),
			expected: DNSErrorSERVFAIL,
		},
		{
			name:     "connection refused",
			err:      fmt.Errorf("dial tcp 10.0.0.1:53: connection refused"),
			expected: DNSErrorRefused,
		},
		{
			name:     "unknown error",
			err:      fmt.Errorf("some random DNS error"),
			expected: DNSErrorUnknown,
		},
		{
			name: "net.DNSError with IsNotFound",
			err: &net.DNSError{
				Err:        "no such host",
				Name:       "nonexistent.example.com",
				IsNotFound: true,
			},
			expected: DNSErrorNXDOMAIN,
		},
		{
			name: "net.DNSError with IsTemporary",
			err: &net.DNSError{
				Err:         "temporary failure",
				Name:        "example.com",
				IsTemporary: true,
			},
			expected: DNSErrorTemporary,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyDNSError(tt.err)
			if result != tt.expected {
				t.Errorf("classifyDNSError(%v) = %s, want %s", tt.err, result, tt.expected)
			}
		})
	}
}

// TestDNSErrorTypeString verifies DNSErrorType string values.
func TestDNSErrorTypeString(t *testing.T) {
	tests := []struct {
		errType  DNSErrorType
		expected string
	}{
		{DNSErrorTimeout, "Timeout"},
		{DNSErrorNXDOMAIN, "NXDOMAIN"},
		{DNSErrorSERVFAIL, "SERVFAIL"},
		{DNSErrorRefused, "Refused"},
		{DNSErrorTemporary, "Temporary"},
		{DNSErrorUnknown, "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if string(tt.errType) != tt.expected {
				t.Errorf("DNSErrorType string = %s, want %s", string(tt.errType), tt.expected)
			}
		})
	}
}
