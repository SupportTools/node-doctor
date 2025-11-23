package network

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// TestNewConnectivityMonitor tests monitor creation.
func TestNewConnectivityMonitor(t *testing.T) {
	tests := []struct {
		name    string
		config  types.MonitorConfig
		wantErr bool
	}{
		{
			name: "valid config with single endpoint",
			config: types.MonitorConfig{
				Name:     "connectivity-test",
				Type:     "network-connectivity-check",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Enabled:  true,
				Config: map[string]interface{}{
					"endpoints": []interface{}{
						map[string]interface{}{
							"url":    "https://google.com",
							"method": "HEAD",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid config with multiple endpoints",
			config: types.MonitorConfig{
				Name:     "connectivity-test",
				Type:     "network-connectivity-check",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Enabled:  true,
				Config: map[string]interface{}{
					"endpoints": []interface{}{
						map[string]interface{}{
							"url":                "https://google.com",
							"method":             "HEAD",
							"expectedStatusCode": 200,
						},
						map[string]interface{}{
							"url":    "https://kubernetes.io",
							"method": "GET",
						},
					},
					"failureThreshold": 5,
				},
			},
			wantErr: false,
		},
		{
			name: "no endpoints configured",
			config: types.MonitorConfig{
				Name:     "connectivity-test",
				Type:     "network-connectivity-check",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Enabled:  true,
				Config: map[string]interface{}{
					"endpoints": []interface{}{},
				},
			},
			wantErr: true,
		},
		{
			name: "nil config",
			config: types.MonitorConfig{
				Name:     "connectivity-test",
				Type:     "network-connectivity-check",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Enabled:  true,
				Config:   nil,
			},
			wantErr: true,
		},
		{
			name: "missing url in endpoint",
			config: types.MonitorConfig{
				Name:     "connectivity-test",
				Type:     "network-connectivity-check",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Enabled:  true,
				Config: map[string]interface{}{
					"endpoints": []interface{}{
						map[string]interface{}{
							"method": "HEAD",
						},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			monitor, err := NewConnectivityMonitor(ctx, tt.config)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if monitor == nil {
				t.Fatal("monitor should not be nil")
			}

			// Verify monitor is a ConnectivityMonitor
			connectivityMonitor, ok := monitor.(*ConnectivityMonitor)
			if !ok {
				t.Fatal("monitor should be a ConnectivityMonitor")
			}

			// Verify config was parsed correctly
			if connectivityMonitor.name != tt.config.Name {
				t.Errorf("name = %s, want %s", connectivityMonitor.name, tt.config.Name)
			}
		})
	}
}

// TestParseConnectivityConfig tests configuration parsing.
func TestParseConnectivityConfig(t *testing.T) {
	tests := []struct {
		name      string
		configMap map[string]interface{}
		wantErr   bool
		validate  func(*testing.T, *ConnectivityMonitorConfig)
	}{
		{
			name: "default values",
			configMap: map[string]interface{}{
				"endpoints": []interface{}{
					map[string]interface{}{
						"url": "https://example.com",
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, config *ConnectivityMonitorConfig) {
				if config.FailureThreshold != defaultConnectivityFailureThreshold {
					t.Errorf("failureThreshold = %d, want %d", config.FailureThreshold, defaultConnectivityFailureThreshold)
				}
				if len(config.Endpoints) != 1 {
					t.Errorf("endpoints count = %d, want 1", len(config.Endpoints))
				}
				if config.Endpoints[0].Method != defaultConnectivityMethod {
					t.Errorf("method = %s, want %s", config.Endpoints[0].Method, defaultConnectivityMethod)
				}
			},
		},
		{
			name: "custom failure threshold",
			configMap: map[string]interface{}{
				"endpoints": []interface{}{
					map[string]interface{}{
						"url": "https://example.com",
					},
				},
				"failureThreshold": 5,
			},
			wantErr: false,
			validate: func(t *testing.T, config *ConnectivityMonitorConfig) {
				if config.FailureThreshold != 5 {
					t.Errorf("failureThreshold = %d, want 5", config.FailureThreshold)
				}
			},
		},
		{
			name: "invalid failure threshold",
			configMap: map[string]interface{}{
				"endpoints": []interface{}{
					map[string]interface{}{
						"url": "https://example.com",
					},
				},
				"failureThreshold": 0,
			},
			wantErr: true,
		},
		{
			name: "endpoint with all fields",
			configMap: map[string]interface{}{
				"endpoints": []interface{}{
					map[string]interface{}{
						"name":               "test-endpoint",
						"url":                "https://example.com",
						"method":             "GET",
						"expectedStatusCode": 200,
						"timeout":            "5s",
						"followRedirects":    true,
						"headers": map[string]interface{}{
							"X-Custom": "value",
						},
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, config *ConnectivityMonitorConfig) {
				if len(config.Endpoints) != 1 {
					t.Fatalf("endpoints count = %d, want 1", len(config.Endpoints))
				}
				ep := config.Endpoints[0]
				if ep.Name != "test-endpoint" {
					t.Errorf("name = %s, want test-endpoint", ep.Name)
				}
				if ep.Method != "GET" {
					t.Errorf("method = %s, want GET", ep.Method)
				}
				if ep.ExpectedStatusCode != 200 {
					t.Errorf("expectedStatusCode = %d, want 200", ep.ExpectedStatusCode)
				}
				if ep.Timeout != 5*time.Second {
					t.Errorf("timeout = %v, want 5s", ep.Timeout)
				}
				if !ep.FollowRedirects {
					t.Error("followRedirects should be true")
				}
				if ep.Headers["X-Custom"] != "value" {
					t.Errorf("header X-Custom = %s, want value", ep.Headers["X-Custom"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := parseConnectivityConfig(tt.configMap)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.validate != nil {
				tt.validate(t, config)
			}
		})
	}
}

// TestConnectivityMonitor_AllEndpointsSucceed tests scenario where all endpoints succeed.
func TestConnectivityMonitor_AllEndpointsSucceed(t *testing.T) {
	config := &ConnectivityMonitorConfig{
		Endpoints: []EndpointConfig{
			{Name: "endpoint1", URL: "https://example.com", Method: "HEAD"},
			{Name: "endpoint2", URL: "https://example.org", Method: "HEAD"},
		},
		FailureThreshold: 3,
	}

	mockClient := &mockHTTPClient{
		result: &EndpointResult{
			Success:      true,
			StatusCode:   200,
			ResponseTime: 50 * time.Millisecond,
		},
	}

	monitor := &ConnectivityMonitor{
		name:             "test-monitor",
		config:           config,
		client:           mockClient,
		endpointFailures: make(map[string]int),
	}

	ctx := context.Background()
	status, err := monitor.checkConnectivity(ctx)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if status == nil {
		t.Fatal("status should not be nil")
	}

	// Check for success event
	foundHealthy := false
	for _, event := range status.Events {
		if event.Reason == "ConnectivityHealthy" {
			foundHealthy = true
		}
	}

	if !foundHealthy {
		t.Error("expected ConnectivityHealthy event")
	}

	// Verify no failures tracked
	if len(monitor.endpointFailures) != 0 {
		t.Errorf("expected no failures tracked, got %d", len(monitor.endpointFailures))
	}
}

// TestConnectivityMonitor_AllEndpointsFail tests scenario where all endpoints fail.
func TestConnectivityMonitor_AllEndpointsFail(t *testing.T) {
	config := &ConnectivityMonitorConfig{
		Endpoints: []EndpointConfig{
			{Name: "endpoint1", URL: "https://example.com", Method: "HEAD"},
			{Name: "endpoint2", URL: "https://example.org", Method: "HEAD"},
		},
		FailureThreshold: 3,
	}

	mockClient := &mockHTTPClient{
		result: &EndpointResult{
			Success:   false,
			Error:     fmt.Errorf("connection refused"),
			ErrorType: "Connection",
		},
	}

	monitor := &ConnectivityMonitor{
		name:             "test-monitor",
		config:           config,
		client:           mockClient,
		endpointFailures: make(map[string]int),
	}

	ctx := context.Background()
	status, err := monitor.checkConnectivity(ctx)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check for failure event
	foundFailure := false
	for _, event := range status.Events {
		if event.Reason == "ExternalConnectivityFailure" || event.Reason == "ConnectivityFailure" {
			foundFailure = true
		}
	}

	if !foundFailure {
		t.Error("expected connectivity failure event")
	}

	// Verify failures tracked
	if monitor.endpointFailures["endpoint1"] != 1 {
		t.Errorf("endpoint1 failures = %d, want 1", monitor.endpointFailures["endpoint1"])
	}
	if monitor.endpointFailures["endpoint2"] != 1 {
		t.Errorf("endpoint2 failures = %d, want 1", monitor.endpointFailures["endpoint2"])
	}
}

// TestConnectivityMonitor_PartialFailure tests scenario where some endpoints fail.
func TestConnectivityMonitor_PartialFailure(t *testing.T) {
	config := &ConnectivityMonitorConfig{
		Endpoints: []EndpointConfig{
			{Name: "endpoint1", URL: "https://example.com", Method: "HEAD"},
			{Name: "endpoint2", URL: "https://example.org", Method: "HEAD"},
		},
		FailureThreshold: 3,
	}

	callCount := 0

	monitor := &ConnectivityMonitor{
		name:   "test-monitor",
		config: config,
		client: &mockHTTPClientFunc{
			checkFunc: func(ctx context.Context, endpoint EndpointConfig) (*EndpointResult, error) {
				callCount++
				if callCount == 1 {
					// First endpoint succeeds
					return &EndpointResult{
						Success:      true,
						StatusCode:   200,
						ResponseTime: 50 * time.Millisecond,
					}, nil
				}
				// Second endpoint fails
				return &EndpointResult{
					Success:   false,
					Error:     fmt.Errorf("connection refused"),
					ErrorType: "Connection",
				}, nil
			},
		},
		endpointFailures: make(map[string]int),
	}

	ctx := context.Background()
	status, err := monitor.checkConnectivity(ctx)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check for partial connectivity event
	foundPartial := false
	for _, event := range status.Events {
		if event.Reason == "PartialConnectivity" {
			foundPartial = true
		}
	}

	if !foundPartial {
		t.Error("expected PartialConnectivity event")
	}
}

// TestConnectivityMonitor_DNSFailure tests DNS failure classification.
func TestConnectivityMonitor_DNSFailure(t *testing.T) {
	config := &ConnectivityMonitorConfig{
		Endpoints: []EndpointConfig{
			{Name: "endpoint1", URL: "https://example.com", Method: "HEAD"},
		},
		FailureThreshold: 3,
	}

	mockClient := &mockHTTPClient{
		result: &EndpointResult{
			Success:   false,
			Error:     fmt.Errorf("DNS resolution failed"),
			ErrorType: "DNS",
		},
	}

	monitor := &ConnectivityMonitor{
		name:             "test-monitor",
		config:           config,
		client:           mockClient,
		endpointFailures: make(map[string]int),
	}

	ctx := context.Background()
	status, err := monitor.checkConnectivity(ctx)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check for DNS failure event
	foundDNSFailure := false
	for _, event := range status.Events {
		if event.Reason == "EndpointDNSFailure" {
			foundDNSFailure = true
		}
	}

	if !foundDNSFailure {
		t.Error("expected EndpointDNSFailure event")
	}
}

// TestConnectivityMonitor_FailureThreshold tests consecutive failure threshold.
func TestConnectivityMonitor_FailureThreshold(t *testing.T) {
	config := &ConnectivityMonitorConfig{
		Endpoints: []EndpointConfig{
			{Name: "endpoint1", URL: "https://example.com", Method: "HEAD"},
		},
		FailureThreshold: 3,
	}

	mockClient := &mockHTTPClient{
		result: &EndpointResult{
			Success:   false,
			Error:     fmt.Errorf("connection refused"),
			ErrorType: "Connection",
		},
	}

	monitor := &ConnectivityMonitor{
		name:             "test-monitor",
		config:           config,
		client:           mockClient,
		endpointFailures: make(map[string]int),
	}

	ctx := context.Background()

	// Run check 3 times to reach threshold
	for i := 0; i < 3; i++ {
		status, err := monitor.checkConnectivity(ctx)
		if err != nil {
			t.Fatalf("check %d: unexpected error: %v", i+1, err)
		}

		// On the third check, we should see NetworkUnreachable condition
		if i == 2 {
			foundCondition := false
			for _, condition := range status.Conditions {
				if condition.Type == "NetworkUnreachable" && condition.Status == types.ConditionTrue {
					foundCondition = true
				}
			}
			if !foundCondition {
				t.Error("expected NetworkUnreachable condition on third failure")
			}
		}
	}

	// Verify failure count
	if monitor.endpointFailures["endpoint1"] != 3 {
		t.Errorf("endpoint1 failures = %d, want 3", monitor.endpointFailures["endpoint1"])
	}
}

// TestConnectivityMonitor_Recovery tests recovery after failures.
func TestConnectivityMonitor_Recovery(t *testing.T) {
	config := &ConnectivityMonitorConfig{
		Endpoints: []EndpointConfig{
			{Name: "endpoint1", URL: "https://example.com", Method: "HEAD"},
		},
		FailureThreshold: 3,
	}

	monitor := &ConnectivityMonitor{
		name:             "test-monitor",
		config:           config,
		endpointFailures: map[string]int{"endpoint1": 5}, // Pre-populate with failures
	}

	mockClient := &mockHTTPClient{
		result: &EndpointResult{
			Success:      true,
			StatusCode:   200,
			ResponseTime: 50 * time.Millisecond,
		},
	}
	monitor.client = mockClient

	ctx := context.Background()
	status, err := monitor.checkConnectivity(ctx)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check for recovery event
	foundRecovery := false
	for _, event := range status.Events {
		if event.Reason == "EndpointRecovered" {
			foundRecovery = true
		}
	}

	if !foundRecovery {
		t.Error("expected EndpointRecovered event")
	}

	// Check for condition cleared
	foundConditionFalse := false
	for _, condition := range status.Conditions {
		if condition.Type == "NetworkUnreachable" && condition.Status == types.ConditionFalse {
			foundConditionFalse = true
		}
	}

	if !foundConditionFalse {
		t.Error("expected NetworkUnreachable condition to be cleared")
	}

	// Verify failure count reset
	if monitor.endpointFailures["endpoint1"] != 0 {
		t.Errorf("endpoint1 failures = %d, want 0", monitor.endpointFailures["endpoint1"])
	}
}

// TestValidateConnectivityConfig tests configuration validation.
func TestValidateConnectivityConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  types.MonitorConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: types.MonitorConfig{
				Config: map[string]interface{}{
					"endpoints": []interface{}{
						map[string]interface{}{
							"url": "https://google.com",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "no endpoints",
			config: types.MonitorConfig{
				Config: map[string]interface{}{
					"endpoints": []interface{}{},
				},
			},
			wantErr: false, // Parsing succeeds, but NewConnectivityMonitor would fail
		},
		{
			name: "nil config",
			config: types.MonitorConfig{
				Config: nil,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConnectivityConfig(tt.config)

			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// TestConnectivityMonitor_ConcurrentChecks tests thread safety.
func TestConnectivityMonitor_ConcurrentChecks(t *testing.T) {
	config := &ConnectivityMonitorConfig{
		Endpoints: []EndpointConfig{
			{Name: "endpoint1", URL: "https://example.com", Method: "HEAD"},
		},
		FailureThreshold: 3,
	}

	mockClient := &mockHTTPClient{
		result: &EndpointResult{
			Success:      true,
			StatusCode:   200,
			ResponseTime: 50 * time.Millisecond,
		},
		delay: 10 * time.Millisecond, // Add small delay to increase chance of race
	}

	monitor := &ConnectivityMonitor{
		name:             "test-monitor",
		config:           config,
		client:           mockClient,
		endpointFailures: make(map[string]int),
	}

	ctx := context.Background()

	// Run multiple checks concurrently
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			_, err := monitor.checkConnectivity(ctx)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			done <- true
		}()
	}

	// Wait for all checks to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}

// mockHTTPClientFunc allows custom check function.
type mockHTTPClientFunc struct {
	checkFunc func(ctx context.Context, endpoint EndpointConfig) (*EndpointResult, error)
}

func (m *mockHTTPClientFunc) CheckEndpoint(ctx context.Context, endpoint EndpointConfig) (*EndpointResult, error) {
	return m.checkFunc(ctx, endpoint)
}

// TestParseEndpointConfig_EdgeCases tests edge cases in endpoint configuration parsing.
func TestParseEndpointConfig_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]interface{}
		wantErr bool
		errMsg  string
	}{
		{
			name: "url is not a string",
			config: map[string]interface{}{
				"url": 12345,
			},
			wantErr: true,
			errMsg:  "url must be a string",
		},
		{
			name: "name is not a string",
			config: map[string]interface{}{
				"url":  "https://example.com",
				"name": 12345,
			},
			wantErr: true,
			errMsg:  "name must be a string",
		},
		{
			name: "method is not a string",
			config: map[string]interface{}{
				"url":    "https://example.com",
				"method": 12345,
			},
			wantErr: true,
			errMsg:  "method must be a string",
		},
		{
			name: "invalid method",
			config: map[string]interface{}{
				"url":    "https://example.com",
				"method": "POST",
			},
			wantErr: true,
			errMsg:  "invalid HTTP method",
		},
		{
			name: "expectedStatusCode is not a number",
			config: map[string]interface{}{
				"url":                "https://example.com",
				"expectedStatusCode": "200",
			},
			wantErr: true,
			errMsg:  "expectedStatusCode must be an integer",
		},
		{
			name: "expectedStatusCode as float64",
			config: map[string]interface{}{
				"url":                "https://example.com",
				"expectedStatusCode": float64(201),
			},
			wantErr: false,
		},
		{
			name: "followRedirects is not a bool",
			config: map[string]interface{}{
				"url":             "https://example.com",
				"followRedirects": "true",
			},
			wantErr: true,
			errMsg:  "followRedirects must be a boolean",
		},
		{
			name: "valid minimal config",
			config: map[string]interface{}{
				"url": "https://example.com",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseEndpointConfig(tt.config)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
					return
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("error = %v, want error containing %q", err, tt.errMsg)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// TestNewConnectivityMonitor_EdgeCases tests edge cases in monitor creation.
func TestNewConnectivityMonitor_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		config  types.MonitorConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "too many endpoints",
			config: types.MonitorConfig{
				Name:     "test-monitor",
				Type:     "network-connectivity-check",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Enabled:  true,
				Config: map[string]interface{}{
					"endpoints": func() []interface{} {
						endpoints := make([]interface{}, 51) // exceeds maxEndpoints (50)
						for i := 0; i < 51; i++ {
							endpoints[i] = map[string]interface{}{
								"url": fmt.Sprintf("https://example%d.com", i),
							}
						}
						return endpoints
					}(),
				},
			},
			wantErr: true,
			errMsg:  "too many endpoints",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			_, err := NewConnectivityMonitor(ctx, tt.config)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
					return
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("error = %v, want error containing %q", err, tt.errMsg)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// TestIsValidHTTPMethod tests HTTP method validation.
func TestIsValidHTTPMethod(t *testing.T) {
	tests := []struct {
		method string
		valid  bool
	}{
		{"GET", true},
		{"HEAD", true},
		{"OPTIONS", true},
		{"get", true},     // lowercase
		{"head", true},    // lowercase
		{"options", true}, // lowercase
		{"Get", true},     // mixed case
		{"POST", false},
		{"PUT", false},
		{"DELETE", false},
		{"PATCH", false},
		{"", false},
		{"INVALID", false},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			got := isValidHTTPMethod(tt.method)
			if got != tt.valid {
				t.Errorf("isValidHTTPMethod(%q) = %v, want %v", tt.method, got, tt.valid)
			}
		})
	}
}

// TestValidateURL tests URL validation for connectivity endpoints.
func TestValidateURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid https URL",
			url:     "https://example.com",
			wantErr: false,
		},
		{
			name:    "valid http URL",
			url:     "http://example.com",
			wantErr: false,
		},
		{
			name:    "valid URL with path",
			url:     "https://example.com/api/v1/health",
			wantErr: false,
		},
		{
			name:    "valid URL with port",
			url:     "https://example.com:8443",
			wantErr: false,
		},
		{
			name:    "missing scheme",
			url:     "example.com",
			wantErr: true,
			errMsg:  "scheme",
		},
		{
			name:    "unsupported scheme - ftp",
			url:     "ftp://example.com",
			wantErr: true,
			errMsg:  "unsupported URL scheme",
		},
		{
			name:    "unsupported scheme - file",
			url:     "file:///etc/passwd",
			wantErr: true,
			errMsg:  "unsupported URL scheme",
		},
		{
			name:    "missing host",
			url:     "https://",
			wantErr: true,
			errMsg:  "host",
		},
		{
			name:    "empty string",
			url:     "",
			wantErr: true,
			errMsg:  "scheme",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateURL(tt.url)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
					return
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("error = %v, want error containing %q", err, tt.errMsg)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// TestParseConnectivityConfig_EdgeCases tests edge cases in connectivity config parsing.
func TestParseConnectivityConfig_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]interface{}
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil config",
			config:  nil,
			wantErr: true,
			errMsg:  "connectivity monitor requires configuration",
		},
		{
			name: "failureThreshold invalid type",
			config: map[string]interface{}{
				"failureThreshold": "not-a-number",
				"endpoints": []interface{}{
					map[string]interface{}{"url": "https://example.com"},
				},
			},
			wantErr: true,
			errMsg:  "failureThreshold must be an integer",
		},
		{
			name: "failureThreshold as float64",
			config: map[string]interface{}{
				"failureThreshold": float64(3),
				"endpoints": []interface{}{
					map[string]interface{}{"url": "https://example.com"},
				},
			},
			wantErr: false,
		},
		{
			name: "failureThreshold too low",
			config: map[string]interface{}{
				"failureThreshold": 0,
				"endpoints": []interface{}{
					map[string]interface{}{"url": "https://example.com"},
				},
			},
			wantErr: true,
			errMsg:  "failureThreshold must be at least 1",
		},
		{
			name: "endpoints not a list",
			config: map[string]interface{}{
				"endpoints": "not-a-list",
			},
			wantErr: true,
			errMsg:  "endpoints must be a list",
		},
		{
			name: "endpoint not a map",
			config: map[string]interface{}{
				"endpoints": []interface{}{"not-a-map"},
			},
			wantErr: true,
			errMsg:  "endpoint 0 must be a map",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseConnectivityConfig(tt.config)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got none")
				} else if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("error = %v, want error containing %q", err, tt.errMsg)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
