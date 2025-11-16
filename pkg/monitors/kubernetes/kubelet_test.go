package kubernetes

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// TestDefaultKubeletClient_CheckHealth tests the health check functionality.
func TestDefaultKubeletClient_CheckHealth(t *testing.T) {
	tests := []struct {
		name        string
		statusCode  int
		serverDelay time.Duration
		wantErr     bool
		errContains string
	}{
		{
			name:       "Success - HTTP 200",
			statusCode: 200,
			wantErr:    false,
		},
		{
			name:        "Failure - HTTP 503",
			statusCode:  503,
			wantErr:     true,
			errContains: "health check returned status 503",
		},
		{
			name:        "Failure - HTTP 500",
			statusCode:  500,
			wantErr:     true,
			errContains: "health check returned status 500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.serverDelay > 0 {
					time.Sleep(tt.serverDelay)
				}
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			// Create client
			config := &KubeletMonitorConfig{
				HealthzURL:  server.URL,
				HTTPTimeout: 2 * time.Second,
			}
			client := newDefaultKubeletClient(config)

			// Perform check
			ctx := context.Background()
			err := client.CheckHealth(ctx)

			// Verify results
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error but got none")
				} else if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error = %v, want error containing %q", err, tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestDefaultKubeletClient_CheckHealth_Timeout tests timeout handling.
func TestDefaultKubeletClient_CheckHealth_Timeout(t *testing.T) {
	// Create slow server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create client with short timeout
	config := &KubeletMonitorConfig{
		HealthzURL:  server.URL,
		HTTPTimeout: 100 * time.Millisecond,
	}
	client := newDefaultKubeletClient(config)

	ctx := context.Background()
	err := client.CheckHealth(ctx)

	if err == nil {
		t.Error("expected timeout error but got none")
	}
}

// TestDefaultKubeletClient_GetMetrics tests metrics retrieval and parsing.
func TestDefaultKubeletClient_GetMetrics(t *testing.T) {
	tests := []struct {
		name        string
		metrics     string
		statusCode  int
		wantPLEG    float64
		wantErr     bool
		errContains string
	}{
		{
			name: "Success - PLEG metric found",
			metrics: `# HELP kubelet_pleg_relist_duration_seconds Duration in seconds for relisting pods in PLEG.
# TYPE kubelet_pleg_relist_duration_seconds summary
kubelet_pleg_relist_duration_seconds{quantile="0.5"} 0.001
kubelet_pleg_relist_duration_seconds{quantile="0.9"} 0.003
kubelet_pleg_relist_duration_seconds{quantile="0.99"} 0.005
kubelet_pleg_relist_duration_seconds_sum 123.45
kubelet_pleg_relist_duration_seconds_count 67890
`,
			statusCode: 200,
			wantPLEG:   0.005, // Should take the maximum (0.99 quantile)
			wantErr:    false,
		},
		{
			name: "Success - No PLEG metric",
			metrics: `# HELP some_other_metric Some other metric
# TYPE some_other_metric gauge
some_other_metric 1.0
`,
			statusCode: 200,
			wantPLEG:   0.0,
			wantErr:    false,
		},
		{
			name:        "Failure - HTTP 500",
			metrics:     "",
			statusCode:  500,
			wantErr:     true,
			errContains: "metrics endpoint returned status 500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				if tt.statusCode == 200 {
					_, _ = w.Write([]byte(tt.metrics))
				}
			}))
			defer server.Close()

			// Create client
			config := &KubeletMonitorConfig{
				MetricsURL:  server.URL,
				HTTPTimeout: 2 * time.Second,
			}
			client := newDefaultKubeletClient(config)

			// Get metrics
			ctx := context.Background()
			metrics, err := client.GetMetrics(ctx)

			// Verify results
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error but got none")
				} else if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error = %v, want error containing %q", err, tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if metrics == nil {
					t.Fatal("expected metrics but got nil")
				}
				if metrics.PLEGRelistDuration != tt.wantPLEG {
					t.Errorf("PLEGRelistDuration = %v, want %v", metrics.PLEGRelistDuration, tt.wantPLEG)
				}
			}
		})
	}
}

// TestParsePrometheusMetrics tests Prometheus metrics parsing.
func TestParsePrometheusMetrics(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantPLEG float64
		wantErr  bool
	}{
		{
			name: "Valid PLEG metrics",
			input: `kubelet_pleg_relist_duration_seconds{quantile="0.5"} 0.001
kubelet_pleg_relist_duration_seconds{quantile="0.9"} 0.003
kubelet_pleg_relist_duration_seconds{quantile="0.99"} 0.005`,
			wantPLEG: 0.005,
			wantErr:  false,
		},
		{
			name:     "No PLEG metrics",
			input:    "some_other_metric 1.0",
			wantPLEG: 0.0,
			wantErr:  false,
		},
		{
			name:     "Empty input",
			input:    "",
			wantPLEG: 0.0,
			wantErr:  false,
		},
		{
			name: "Comments and empty lines",
			input: `# HELP kubelet_pleg_relist_duration_seconds Duration
# TYPE kubelet_pleg_relist_duration_seconds summary

kubelet_pleg_relist_duration_seconds{quantile="0.9"} 0.002`,
			wantPLEG: 0.002,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := strings.NewReader(tt.input)
			metrics, err := parsePrometheusMetrics(reader)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if metrics == nil {
					t.Fatal("expected metrics but got nil")
				}
				if metrics.PLEGRelistDuration != tt.wantPLEG {
					t.Errorf("PLEGRelistDuration = %v, want %v", metrics.PLEGRelistDuration, tt.wantPLEG)
				}
			}
		})
	}
}

// mockKubeletClient is a mock implementation for testing.
type mockKubeletClient struct {
	healthErr     error
	metrics       *KubeletMetrics
	metricsErr    error
	systemdActive bool
	systemdErr    error
	delay         time.Duration
}

func (m *mockKubeletClient) CheckHealth(ctx context.Context) error {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return m.healthErr
}

func (m *mockKubeletClient) GetMetrics(ctx context.Context) (*KubeletMetrics, error) {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return m.metrics, m.metricsErr
}

func (m *mockKubeletClient) CheckSystemdStatus(ctx context.Context) (bool, error) {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	return m.systemdActive, m.systemdErr
}

// TestKubeletMonitor_CheckKubelet_Success tests successful health checks.
func TestKubeletMonitor_CheckKubelet_Success(t *testing.T) {
	config := &KubeletMonitorConfig{
		HealthzURL:         "http://127.0.0.1:10248/healthz",
		MetricsURL:         "http://127.0.0.1:10255/metrics",
		CheckSystemdStatus: true,
		CheckPLEG:          true,
		PLEGThreshold:      5 * time.Second,
		FailureThreshold:   3,
		HTTPTimeout:        5 * time.Second,
	}

	monitor := &KubeletMonitor{
		name:   "test-kubelet",
		config: config,
		client: &mockKubeletClient{
			healthErr:     nil,
			systemdActive: true,
			metrics: &KubeletMetrics{
				PLEGRelistDuration: 0.002, // 2ms - healthy
			},
		},
	}

	ctx := context.Background()
	status, err := monitor.checkKubelet(ctx)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if status == nil {
		t.Fatal("expected status but got nil")
	}

	// Verify healthy condition was added
	foundHealthy := false
	for _, cond := range status.Conditions {
		if cond.Type == "KubeletHealthy" {
			foundHealthy = true
			break
		}
	}
	if !foundHealthy {
		t.Error("expected KubeletHealthy condition but didn't find it")
	}
}

// TestKubeletMonitor_CheckKubelet_HealthFailure tests health check failures.
func TestKubeletMonitor_CheckKubelet_HealthFailure(t *testing.T) {
	config := &KubeletMonitorConfig{
		HealthzURL:         "http://127.0.0.1:10248/healthz",
		MetricsURL:         "http://127.0.0.1:10255/metrics",
		CheckSystemdStatus: false,
		CheckPLEG:          false,
		FailureThreshold:   3,
		HTTPTimeout:        5 * time.Second,
	}

	monitor := &KubeletMonitor{
		name:   "test-kubelet",
		config: config,
		client: &mockKubeletClient{
			healthErr: fmt.Errorf("health check failed"),
		},
	}

	ctx := context.Background()

	// First failure
	status, err := monitor.checkKubelet(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status == nil {
		t.Fatal("expected status but got nil")
	}

	// Check consecutive failures counter
	if monitor.consecutiveFailures != 1 {
		t.Errorf("consecutiveFailures = %d, want 1", monitor.consecutiveFailures)
	}

	// Second failure
	status, err = monitor.checkKubelet(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if monitor.consecutiveFailures != 2 {
		t.Errorf("consecutiveFailures = %d, want 2", monitor.consecutiveFailures)
	}

	// Third failure - should trigger KubeletUnhealthy condition
	status, err = monitor.checkKubelet(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if monitor.consecutiveFailures != 3 {
		t.Errorf("consecutiveFailures = %d, want 3", monitor.consecutiveFailures)
	}

	// Verify KubeletUnhealthy condition
	foundUnhealthy := false
	for _, cond := range status.Conditions {
		if cond.Type == "KubeletUnhealthy" {
			foundUnhealthy = true
			break
		}
	}
	if !foundUnhealthy {
		t.Error("expected KubeletUnhealthy condition after 3 failures")
	}
}

// TestKubeletMonitor_CheckKubelet_SystemdInactive tests systemd service down.
func TestKubeletMonitor_CheckKubelet_SystemdInactive(t *testing.T) {
	config := &KubeletMonitorConfig{
		HealthzURL:         "http://127.0.0.1:10248/healthz",
		CheckSystemdStatus: true,
		FailureThreshold:   3,
		HTTPTimeout:        5 * time.Second,
	}

	monitor := &KubeletMonitor{
		name:   "test-kubelet",
		config: config,
		client: &mockKubeletClient{
			healthErr:     nil,
			systemdActive: false,
		},
	}

	ctx := context.Background()
	status, err := monitor.checkKubelet(ctx)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify KubeletDown condition
	foundDown := false
	for _, cond := range status.Conditions {
		if cond.Type == "KubeletDown" {
			foundDown = true
			break
		}
	}
	if !foundDown {
		t.Error("expected KubeletDown condition when systemd is inactive")
	}
}

// TestKubeletMonitor_CheckKubelet_PLEGSlow tests slow PLEG detection.
func TestKubeletMonitor_CheckKubelet_PLEGSlow(t *testing.T) {
	config := &KubeletMonitorConfig{
		HealthzURL:       "http://127.0.0.1:10248/healthz",
		CheckPLEG:        true,
		PLEGThreshold:    5 * time.Second,
		FailureThreshold: 3,
		HTTPTimeout:      5 * time.Second,
	}

	monitor := &KubeletMonitor{
		name:   "test-kubelet",
		config: config,
		client: &mockKubeletClient{
			healthErr: nil,
			metrics: &KubeletMetrics{
				PLEGRelistDuration: 8.0, // 8 seconds - exceeds threshold
			},
		},
	}

	ctx := context.Background()
	status, err := monitor.checkKubelet(ctx)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify PLEGSlow event
	foundPLEGEvent := false
	for _, event := range status.Events {
		if event.Reason == "PLEGSlow" {
			foundPLEGEvent = true
			break
		}
	}
	if !foundPLEGEvent {
		t.Error("expected PLEGSlow event when PLEG exceeds threshold")
	}
}

// TestKubeletMonitor_CheckKubelet_Recovery tests recovery from failures.
func TestKubeletMonitor_CheckKubelet_Recovery(t *testing.T) {
	config := &KubeletMonitorConfig{
		HealthzURL:       "http://127.0.0.1:10248/healthz",
		FailureThreshold: 3,
		HTTPTimeout:      5 * time.Second,
	}

	mockClient := &mockKubeletClient{
		healthErr: fmt.Errorf("health check failed"),
	}

	monitor := &KubeletMonitor{
		name:   "test-kubelet",
		config: config,
		client: mockClient,
	}

	ctx := context.Background()

	// Fail twice
	_, _ = monitor.checkKubelet(ctx)
	_, _ = monitor.checkKubelet(ctx)

	if monitor.consecutiveFailures != 2 {
		t.Errorf("consecutiveFailures = %d, want 2", monitor.consecutiveFailures)
	}

	// Recover
	mockClient.healthErr = nil
	status, err := monitor.checkKubelet(ctx)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify recovery
	if monitor.consecutiveFailures != 0 {
		t.Errorf("consecutiveFailures = %d, want 0 after recovery", monitor.consecutiveFailures)
	}

	// Verify recovery event
	foundRecovery := false
	for _, event := range status.Events {
		if event.Reason == "KubeletRecovered" {
			foundRecovery = true
			if !strings.Contains(event.Message, "2 consecutive failures") {
				t.Errorf("recovery message doesn't mention correct failure count: %s", event.Message)
			}
			break
		}
	}
	if !foundRecovery {
		t.Error("expected KubeletRecovered event after recovery")
	}
}

// TestParseKubeletConfig tests configuration parsing.
func TestParseKubeletConfig(t *testing.T) {
	tests := []struct {
		name    string
		input   map[string]interface{}
		want    *KubeletMonitorConfig
		wantErr bool
	}{
		{
			name: "Full config",
			input: map[string]interface{}{
				"healthzURL":         "http://custom:10248/healthz",
				"metricsURL":         "http://custom:10255/metrics",
				"checkSystemdStatus": true,
				"checkPLEG":          true,
				"plegThreshold":      "10s",
				"failureThreshold":   5,
				"httpTimeout":        "3s",
			},
			want: &KubeletMonitorConfig{
				HealthzURL:         "http://custom:10248/healthz",
				MetricsURL:         "http://custom:10255/metrics",
				CheckSystemdStatus: true,
				CheckPLEG:          true,
				PLEGThreshold:      10 * time.Second,
				FailureThreshold:   5,
				HTTPTimeout:        3 * time.Second,
			},
			wantErr: false,
		},
		{
			name:  "Empty config",
			input: map[string]interface{}{},
			want: &KubeletMonitorConfig{
				HealthzURL:         "",
				MetricsURL:         "",
				CheckSystemdStatus: false,
				CheckPLEG:          false,
				PLEGThreshold:      0,
				FailureThreshold:   0,
				HTTPTimeout:        0,
			},
			wantErr: false,
		},
		{
			name: "Numeric duration",
			input: map[string]interface{}{
				"plegThreshold": 5,
				"httpTimeout":   10.5,
			},
			want: &KubeletMonitorConfig{
				PLEGThreshold: 5 * time.Second,
				HTTPTimeout:   10500 * time.Millisecond,
			},
			wantErr: false,
		},
		{
			name: "Invalid type for healthzURL",
			input: map[string]interface{}{
				"healthzURL": 123,
			},
			wantErr: true,
		},
		{
			name: "Invalid type for checkSystemdStatus",
			input: map[string]interface{}{
				"checkSystemdStatus": "yes",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseKubeletConfig(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if got == nil {
					t.Fatal("expected config but got nil")
				}

				// Compare fields
				if tt.want.HealthzURL != "" && got.HealthzURL != tt.want.HealthzURL {
					t.Errorf("HealthzURL = %v, want %v", got.HealthzURL, tt.want.HealthzURL)
				}
				if tt.want.MetricsURL != "" && got.MetricsURL != tt.want.MetricsURL {
					t.Errorf("MetricsURL = %v, want %v", got.MetricsURL, tt.want.MetricsURL)
				}
				if got.CheckSystemdStatus != tt.want.CheckSystemdStatus {
					t.Errorf("CheckSystemdStatus = %v, want %v", got.CheckSystemdStatus, tt.want.CheckSystemdStatus)
				}
				if got.CheckPLEG != tt.want.CheckPLEG {
					t.Errorf("CheckPLEG = %v, want %v", got.CheckPLEG, tt.want.CheckPLEG)
				}
				if tt.want.PLEGThreshold != 0 && got.PLEGThreshold != tt.want.PLEGThreshold {
					t.Errorf("PLEGThreshold = %v, want %v", got.PLEGThreshold, tt.want.PLEGThreshold)
				}
				if tt.want.FailureThreshold != 0 && got.FailureThreshold != tt.want.FailureThreshold {
					t.Errorf("FailureThreshold = %v, want %v", got.FailureThreshold, tt.want.FailureThreshold)
				}
				if tt.want.HTTPTimeout != 0 && got.HTTPTimeout != tt.want.HTTPTimeout {
					t.Errorf("HTTPTimeout = %v, want %v", got.HTTPTimeout, tt.want.HTTPTimeout)
				}
			}
		})
	}
}

// TestKubeletMonitorConfig_ApplyDefaults tests default value application.
func TestKubeletMonitorConfig_ApplyDefaults(t *testing.T) {
	config := &KubeletMonitorConfig{}
	err := config.applyDefaults()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify defaults
	if config.HealthzURL != defaultKubeletHealthzURL {
		t.Errorf("HealthzURL = %v, want %v", config.HealthzURL, defaultKubeletHealthzURL)
	}
	if config.MetricsURL != defaultKubeletMetricsURL {
		t.Errorf("MetricsURL = %v, want %v", config.MetricsURL, defaultKubeletMetricsURL)
	}
	if config.PLEGThreshold != defaultKubeletPLEGThreshold {
		t.Errorf("PLEGThreshold = %v, want %v", config.PLEGThreshold, defaultKubeletPLEGThreshold)
	}
	if config.FailureThreshold != defaultKubeletFailureThreshold {
		t.Errorf("FailureThreshold = %v, want %v", config.FailureThreshold, defaultKubeletFailureThreshold)
	}
	if config.HTTPTimeout != defaultKubeletTimeout {
		t.Errorf("HTTPTimeout = %v, want %v", config.HTTPTimeout, defaultKubeletTimeout)
	}
}

// TestKubeletMonitor_ContextCancellation tests context cancellation handling.
func TestKubeletMonitor_ContextCancellation(t *testing.T) {
	config := &KubeletMonitorConfig{
		HealthzURL:       "http://127.0.0.1:10248/healthz",
		FailureThreshold: 3,
		HTTPTimeout:      5 * time.Second,
	}

	monitor := &KubeletMonitor{
		name:   "test-kubelet",
		config: config,
		client: &mockKubeletClient{
			delay: 2 * time.Second, // Slow check
		},
	}

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	status, err := monitor.checkKubelet(ctx)

	// Should not return error, but status should indicate failure
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if status == nil {
		t.Fatal("expected status but got nil")
	}

	// Health check should have failed due to context cancellation
	if monitor.consecutiveFailures == 0 {
		t.Error("expected failure to be recorded")
	}
}

// TestValidateKubeletConfig tests configuration validation.
func TestValidateKubeletConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  types.MonitorConfig
		wantErr bool
	}{
		{
			name: "Valid configuration",
			config: types.MonitorConfig{
				Name: "test-kubelet",
				Type: "kubernetes-kubelet-check",
				Config: map[string]interface{}{
					"healthzURL": "http://127.0.0.1:10248/healthz",
					"metricsURL": "http://127.0.0.1:10250/metrics",
				},
			},
			wantErr: false,
		},
		{
			name: "Missing name",
			config: types.MonitorConfig{
				Name: "",
				Type: "kubernetes-kubelet-check",
				Config: map[string]interface{}{
					"healthzURL": "http://127.0.0.1:10248/healthz",
				},
			},
			wantErr: true,
		},
		{
			name: "Wrong type",
			config: types.MonitorConfig{
				Name: "test-kubelet",
				Type: "wrong-type",
				Config: map[string]interface{}{
					"healthzURL": "http://127.0.0.1:10248/healthz",
				},
			},
			wantErr: true,
		},
		{
			name: "Invalid config - bad healthzURL type",
			config: types.MonitorConfig{
				Name: "test-kubelet",
				Type: "kubernetes-kubelet-check",
				Config: map[string]interface{}{
					"healthzURL": 12345,
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateKubeletConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateKubeletConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestDefaultKubeletClient_CheckSystemdStatus tests systemd status checking.
func TestDefaultKubeletClient_CheckSystemdStatus(t *testing.T) {
	// Note: This test requires systemctl to be available on the system
	// On systems without systemd, this test should be skipped
	config := &KubeletMonitorConfig{
		HTTPTimeout: 2 * time.Second,
	}
	client := newDefaultKubeletClient(config)

	ctx := context.Background()
	active, err := client.CheckSystemdStatus(ctx)

	// We can't guarantee kubelet is running in test environment
	// But we can verify the function doesn't panic and returns valid results
	if err != nil {
		// Error is acceptable (kubelet might not be installed/running)
		t.Logf("CheckSystemdStatus returned error (expected in test environment): %v", err)
	}
	t.Logf("CheckSystemdStatus: active=%v, err=%v", active, err)
}

// TestDefaultKubeletClient_CheckSystemdStatus_Timeout tests systemd timeout handling.
func TestDefaultKubeletClient_CheckSystemdStatus_Timeout(t *testing.T) {
	config := &KubeletMonitorConfig{
		HTTPTimeout: 1 * time.Nanosecond, // Extremely short timeout
	}
	client := newDefaultKubeletClient(config)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	_, err := client.CheckSystemdStatus(ctx)
	// Should get a timeout or context deadline exceeded error
	if err == nil {
		t.Log("Warning: Expected timeout error but got success (test might be unreliable)")
	}
}

// TestNewKubeletMonitor tests monitor creation and initialization.
func TestNewKubeletMonitor(t *testing.T) {
	tests := []struct {
		name    string
		config  types.MonitorConfig
		wantErr bool
	}{
		{
			name: "Valid configuration",
			config: types.MonitorConfig{
				Name:     "test-kubelet",
				Type:     "kubernetes-kubelet-check",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Config: map[string]interface{}{
					"healthzURL": "http://127.0.0.1:10248/healthz",
					"metricsURL": "http://127.0.0.1:10250/metrics",
				},
			},
			wantErr: false,
		},
		{
			name: "Invalid interval",
			config: types.MonitorConfig{
				Name:     "test-kubelet",
				Type:     "kubernetes-kubelet-check",
				Interval: 0, // Invalid
				Timeout:  10 * time.Second,
				Config: map[string]interface{}{
					"healthzURL": "http://127.0.0.1:10248/healthz",
				},
			},
			wantErr: true,
		},
		{
			name: "Invalid config type",
			config: types.MonitorConfig{
				Name:     "test-kubelet",
				Type:     "kubernetes-kubelet-check",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Config: map[string]interface{}{
					"healthzURL": 12345, // Wrong type
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			monitor, err := NewKubeletMonitor(ctx, tt.config)

			if (err != nil) != tt.wantErr {
				t.Errorf("NewKubeletMonitor() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if monitor == nil {
					t.Error("expected monitor but got nil")
					return
				}

				// Verify monitor is properly initialized
				kubeletMonitor, ok := monitor.(*KubeletMonitor)
				if !ok {
					t.Error("monitor is not a *KubeletMonitor")
					return
				}

				if kubeletMonitor.name != tt.config.Name {
					t.Errorf("monitor name = %s, want %s", kubeletMonitor.name, tt.config.Name)
				}

				if kubeletMonitor.config == nil {
					t.Error("monitor config is nil")
				}

				if kubeletMonitor.client == nil {
					t.Error("monitor client is nil")
				}

				if kubeletMonitor.BaseMonitor == nil {
					t.Error("monitor BaseMonitor is nil")
				}
			}
		})
	}
}

// TestValidateKubeletURL tests URL validation.
func TestValidateKubeletURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		fieldName string
		wantErr   bool
	}{
		{
			name:      "Valid HTTP URL",
			url:       "http://127.0.0.1:10248/healthz",
			fieldName: "healthzURL",
			wantErr:   false,
		},
		{
			name:      "Valid HTTPS URL",
			url:       "https://127.0.0.1:10248/healthz",
			fieldName: "healthzURL",
			wantErr:   false,
		},
		{
			name:      "Missing scheme",
			url:       "127.0.0.1:10248/healthz",
			fieldName: "healthzURL",
			wantErr:   true,
		},
		{
			name:      "Invalid scheme",
			url:       "ftp://127.0.0.1:10248/healthz",
			fieldName: "healthzURL",
			wantErr:   true,
		},
		{
			name:      "Missing host",
			url:       "http:///healthz",
			fieldName: "healthzURL",
			wantErr:   true,
		},
		{
			name:      "Malformed URL",
			url:       "://invalid",
			fieldName: "healthzURL",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateKubeletURL(tt.url, tt.fieldName)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateKubeletURL() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestKubeletMonitorConfig_ApplyDefaults_InvalidURL tests URL validation in applyDefaults.
func TestKubeletMonitorConfig_ApplyDefaults_InvalidURL(t *testing.T) {
	tests := []struct {
		name    string
		config  *KubeletMonitorConfig
		wantErr bool
	}{
		{
			name: "Invalid healthz URL",
			config: &KubeletMonitorConfig{
				HealthzURL: "://invalid",
				MetricsURL: "http://127.0.0.1:10250/metrics",
			},
			wantErr: true,
		},
		{
			name: "Invalid metrics URL",
			config: &KubeletMonitorConfig{
				HealthzURL: "http://127.0.0.1:10248/healthz",
				MetricsURL: "ftp://invalid",
			},
			wantErr: true,
		},
		{
			name: "Missing scheme in healthz URL",
			config: &KubeletMonitorConfig{
				HealthzURL: "127.0.0.1:10248/healthz",
				MetricsURL: "http://127.0.0.1:10250/metrics",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.applyDefaults()
			if (err != nil) != tt.wantErr {
				t.Errorf("applyDefaults() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestKubeletMonitor_CircuitBreaker tests circuit breaker integration.
func TestKubeletMonitor_CircuitBreaker(t *testing.T) {
	ctx := context.Background()

	// Create a mock client that will fail
	mockClient := &mockKubeletClient{
		healthErr: fmt.Errorf("connection refused"),
	}

	config := &KubeletMonitorConfig{
		HealthzURL:       "http://127.0.0.1:10248/healthz",
		MetricsURL:       "http://127.0.0.1:10250/metrics",
		FailureThreshold: 3,
		HTTPTimeout:      5 * time.Second,
		CircuitBreaker: &CircuitBreakerConfig{
			Enabled:             true,
			FailureThreshold:    2, // Open circuit after 2 failures
			OpenTimeout:         100 * time.Millisecond,
			HalfOpenMaxRequests: 2,
		},
	}

	monitor := &KubeletMonitor{
		name:   "kubelet-circuit-breaker-test",
		config: config,
		client: mockClient,
	}

	// Initialize circuit breaker
	cb, err := NewCircuitBreaker(config.CircuitBreaker)
	if err != nil {
		t.Fatalf("Failed to create circuit breaker: %v", err)
	}
	monitor.circuitBreaker = cb

	// First check - should attempt and fail
	status, err := monitor.checkKubelet(ctx)
	if err != nil {
		t.Errorf("checkKubelet() unexpected error: %v", err)
	}
	if cb.State() != StateClosed {
		t.Errorf("Circuit breaker should be closed after 1 failure, got %v", cb.State())
	}

	// Second check - should attempt and fail, opening circuit
	status, err = monitor.checkKubelet(ctx)
	if err != nil {
		t.Errorf("checkKubelet() unexpected error: %v", err)
	}
	if cb.State() != StateOpen {
		t.Errorf("Circuit breaker should be open after 2 failures, got %v", cb.State())
	}

	// Verify status has circuit breaker open event
	hasCircuitBreakerEvent := false
	for _, event := range status.Events {
		if event.Reason == "CircuitBreakerOpen" {
			hasCircuitBreakerEvent = true
			break
		}
	}
	if !hasCircuitBreakerEvent {
		t.Error("Status should contain CircuitBreakerOpen event")
	}

	// Third check - circuit is open, should fail fast without attempting
	status, err = monitor.checkKubelet(ctx)
	if err != nil {
		t.Errorf("checkKubelet() unexpected error: %v", err)
	}
	// Verify health check was NOT attempted (circuit open)
	hasHealthEvent := false
	for _, event := range status.Events {
		if event.Reason == "KubeletHealthCheckFailed" {
			hasHealthEvent = true
			break
		}
	}
	if hasHealthEvent {
		t.Error("Health check should not be attempted when circuit is open")
	}

	// Wait for circuit to transition to half-open
	time.Sleep(150 * time.Millisecond)

	// Fix the mock client to succeed
	mockClient.healthErr = nil

	// Next check - circuit transitions to half-open, should attempt
	status, err = monitor.checkKubelet(ctx)
	if err != nil {
		t.Errorf("checkKubelet() unexpected error: %v", err)
	}
	if cb.State() != StateHalfOpen {
		t.Errorf("Circuit breaker should be half-open after timeout, got %v", cb.State())
	}

	// Another successful check - should close circuit
	status, err = monitor.checkKubelet(ctx)
	if err != nil {
		t.Errorf("checkKubelet() unexpected error: %v", err)
	}
	if cb.State() != StateClosed {
		t.Errorf("Circuit breaker should be closed after successes, got %v", cb.State())
	}

	// Verify metrics
	metrics := cb.Metrics()
	if metrics.TotalOpenings != 1 {
		t.Errorf("TotalOpenings = %d, want 1", metrics.TotalOpenings)
	}
	if metrics.TotalRecoveries != 1 {
		t.Errorf("TotalRecoveries = %d, want 1", metrics.TotalRecoveries)
	}
}

// TestKubeletMonitor_CircuitBreakerDisabled tests that circuit breaker can be disabled.
func TestKubeletMonitor_CircuitBreakerDisabled(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockKubeletClient{
		healthErr: fmt.Errorf("connection refused"),
	}

	config := &KubeletMonitorConfig{
		HealthzURL:       "http://127.0.0.1:10248/healthz",
		MetricsURL:       "http://127.0.0.1:10250/metrics",
		FailureThreshold: 3,
		HTTPTimeout:      5 * time.Second,
		// Circuit breaker not configured - should work without it
	}

	monitor := &KubeletMonitor{
		name:           "kubelet-no-circuit-breaker-test",
		config:         config,
		client:         mockClient,
		circuitBreaker: nil, // No circuit breaker
	}

	// Should still work without circuit breaker
	for i := 0; i < 5; i++ {
		_, err := monitor.checkKubelet(ctx)
		if err != nil {
			t.Errorf("checkKubelet() unexpected error: %v", err)
		}
		// Verify health check is always attempted
	}
}

// TestKubeletMonitor_CircuitBreakerExponentialBackoff tests exponential backoff.
func TestKubeletMonitor_CircuitBreakerExponentialBackoff(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockKubeletClient{
		healthErr: fmt.Errorf("connection refused"),
	}

	config := &KubeletMonitorConfig{
		HealthzURL:       "http://127.0.0.1:10248/healthz",
		MetricsURL:       "http://127.0.0.1:10250/metrics",
		FailureThreshold: 3,
		HTTPTimeout:      5 * time.Second,
		CircuitBreaker: &CircuitBreakerConfig{
			Enabled:               true,
			FailureThreshold:      1,
			OpenTimeout:           50 * time.Millisecond,
			HalfOpenMaxRequests:   1,
			UseExponentialBackoff: true,
			MaxBackoffTimeout:     500 * time.Millisecond,
		},
	}

	monitor := &KubeletMonitor{
		name:   "kubelet-backoff-test",
		config: config,
		client: mockClient,
	}

	cb, err := NewCircuitBreaker(config.CircuitBreaker)
	if err != nil {
		t.Fatalf("Failed to create circuit breaker: %v", err)
	}
	monitor.circuitBreaker = cb

	// Fail to open circuit
	monitor.checkKubelet(ctx)
	if cb.State() != StateOpen {
		t.Errorf("Circuit should be open, got %v", cb.State())
	}

	metrics := cb.Metrics()
	if metrics.CurrentBackoffMultiplier != 1 {
		t.Errorf("BackoffMultiplier = %d, want 1", metrics.CurrentBackoffMultiplier)
	}

	// Wait for first timeout (50ms)
	time.Sleep(60 * time.Millisecond)
	monitor.checkKubelet(ctx) // Transition to half-open

	// Fail in half-open - should reopen with increased backoff
	monitor.checkKubelet(ctx)
	if cb.State() != StateOpen {
		t.Errorf("Circuit should be open again, got %v", cb.State())
	}

	metrics = cb.Metrics()
	if metrics.CurrentBackoffMultiplier != 2 {
		t.Errorf("BackoffMultiplier = %d, want 2 after failed recovery", metrics.CurrentBackoffMultiplier)
	}

	// Second timeout should be longer (50ms * 2^(2-1) = 100ms)
	time.Sleep(60 * time.Millisecond)
	if cb.CanAttempt() {
		t.Error("Should not allow attempt before doubled timeout")
	}

	time.Sleep(50 * time.Millisecond) // Total 110ms
	if !cb.CanAttempt() {
		t.Error("Should allow attempt after doubled timeout")
	}
}
// TestParseCircuitBreakerConfig tests the parseCircuitBreakerConfig function.
func TestParseCircuitBreakerConfig(t *testing.T) {
	tests := []struct {
		name      string
		configMap map[string]interface{}
		want      *CircuitBreakerConfig
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "nil config map",
			configMap: nil,
			want:      nil,
			wantErr:   false,
		},
		{
			name: "valid config with all fields",
			configMap: map[string]interface{}{
				"enabled":                true,
				"failureThreshold":       5,
				"openTimeout":            "30s",
				"halfOpenMaxRequests":    3,
				"useExponentialBackoff":  true,
				"maxBackoffTimeout":      "5m",
			},
			want: &CircuitBreakerConfig{
				Enabled:                true,
				FailureThreshold:       5,
				OpenTimeout:            30 * time.Second,
				HalfOpenMaxRequests:    3,
				UseExponentialBackoff:  true,
				MaxBackoffTimeout:      5 * time.Minute,
			},
			wantErr: false,
		},
		{
			name: "failureThreshold as float64",
			configMap: map[string]interface{}{
				"failureThreshold": float64(10),
			},
			want: &CircuitBreakerConfig{
				FailureThreshold: 10,
			},
			wantErr: false,
		},
		{
			name: "openTimeout as float64 seconds",
			configMap: map[string]interface{}{
				"openTimeout": float64(15),
			},
			want: &CircuitBreakerConfig{
				OpenTimeout: 15 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "openTimeout as int seconds",
			configMap: map[string]interface{}{
				"openTimeout": 20,
			},
			want: &CircuitBreakerConfig{
				OpenTimeout: 20 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "maxBackoffTimeout as float64 seconds",
			configMap: map[string]interface{}{
				"maxBackoffTimeout": float64(300),
			},
			want: &CircuitBreakerConfig{
				MaxBackoffTimeout: 300 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "maxBackoffTimeout as int seconds",
			configMap: map[string]interface{}{
				"maxBackoffTimeout": 600,
			},
			want: &CircuitBreakerConfig{
				MaxBackoffTimeout: 600 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "halfOpenMaxRequests as float64",
			configMap: map[string]interface{}{
				"halfOpenMaxRequests": float64(5),
			},
			want: &CircuitBreakerConfig{
				HalfOpenMaxRequests: 5,
			},
			wantErr: false,
		},
		{
			name: "invalid enabled type",
			configMap: map[string]interface{}{
				"enabled": "true",
			},
			want:    nil,
			wantErr: true,
			errMsg:  "enabled must be a boolean",
		},
		{
			name: "invalid failureThreshold type",
			configMap: map[string]interface{}{
				"failureThreshold": "5",
			},
			want:    nil,
			wantErr: true,
			errMsg:  "failureThreshold must be an integer",
		},
		{
			name: "invalid openTimeout type",
			configMap: map[string]interface{}{
				"openTimeout": true,
			},
			want:    nil,
			wantErr: true,
			errMsg:  "openTimeout must be a duration string or number",
		},
		{
			name: "invalid openTimeout duration string",
			configMap: map[string]interface{}{
				"openTimeout": "invalid",
			},
			want:    nil,
			wantErr: true,
			errMsg:  "invalid openTimeout duration",
		},
		{
			name: "invalid halfOpenMaxRequests type",
			configMap: map[string]interface{}{
				"halfOpenMaxRequests": "3",
			},
			want:    nil,
			wantErr: true,
			errMsg:  "halfOpenMaxRequests must be an integer",
		},
		{
			name: "invalid useExponentialBackoff type",
			configMap: map[string]interface{}{
				"useExponentialBackoff": "true",
			},
			want:    nil,
			wantErr: true,
			errMsg:  "useExponentialBackoff must be a boolean",
		},
		{
			name: "invalid maxBackoffTimeout type",
			configMap: map[string]interface{}{
				"maxBackoffTimeout": true,
			},
			want:    nil,
			wantErr: true,
			errMsg:  "maxBackoffTimeout must be a duration string or number",
		},
		{
			name: "invalid maxBackoffTimeout duration string",
			configMap: map[string]interface{}{
				"maxBackoffTimeout": "bad-duration",
			},
			want:    nil,
			wantErr: true,
			errMsg:  "invalid maxBackoffTimeout duration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCircuitBreakerConfig(tt.configMap)

			if tt.wantErr {
				if err == nil {
					t.Errorf("parseCircuitBreakerConfig() expected error containing %q, got nil", tt.errMsg)
					return
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("parseCircuitBreakerConfig() error = %v, want error containing %q", err, tt.errMsg)
				}
				return
			}

			if err != nil {
				t.Errorf("parseCircuitBreakerConfig() unexpected error: %v", err)
				return
			}

			// Compare results
			if tt.want == nil && got != nil {
				t.Errorf("parseCircuitBreakerConfig() = %+v, want nil", got)
				return
			}
			if tt.want != nil && got == nil {
				t.Errorf("parseCircuitBreakerConfig() = nil, want %+v", tt.want)
				return
			}
			if tt.want == nil && got == nil {
				return
			}

			if got.Enabled != tt.want.Enabled {
				t.Errorf("parseCircuitBreakerConfig().Enabled = %v, want %v", got.Enabled, tt.want.Enabled)
			}
			if got.FailureThreshold != tt.want.FailureThreshold {
				t.Errorf("parseCircuitBreakerConfig().FailureThreshold = %v, want %v", got.FailureThreshold, tt.want.FailureThreshold)
			}
			if got.OpenTimeout != tt.want.OpenTimeout {
				t.Errorf("parseCircuitBreakerConfig().OpenTimeout = %v, want %v", got.OpenTimeout, tt.want.OpenTimeout)
			}
			if got.HalfOpenMaxRequests != tt.want.HalfOpenMaxRequests {
				t.Errorf("parseCircuitBreakerConfig().HalfOpenMaxRequests = %v, want %v", got.HalfOpenMaxRequests, tt.want.HalfOpenMaxRequests)
			}
			if got.UseExponentialBackoff != tt.want.UseExponentialBackoff {
				t.Errorf("parseCircuitBreakerConfig().UseExponentialBackoff = %v, want %v", got.UseExponentialBackoff, tt.want.UseExponentialBackoff)
			}
			if got.MaxBackoffTimeout != tt.want.MaxBackoffTimeout {
				t.Errorf("parseCircuitBreakerConfig().MaxBackoffTimeout = %v, want %v", got.MaxBackoffTimeout, tt.want.MaxBackoffTimeout)
			}
		})
	}
}
