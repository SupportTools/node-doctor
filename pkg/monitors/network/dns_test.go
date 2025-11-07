package network

import (
	"context"
	"fmt"
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
		},
		resolver: mock,
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
