package types

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestGlobalSettingsApplyDefaults tests default application
func TestGlobalSettingsApplyDefaults(t *testing.T) {
	tests := []struct {
		name     string
		input    GlobalSettings
		expected GlobalSettings
		wantErr  bool
	}{
		{
			name:  "empty settings get all defaults",
			input: GlobalSettings{},
			expected: GlobalSettings{
				LogLevel:                DefaultLogLevel,
				LogFormat:               DefaultLogFormat,
				LogOutput:               DefaultLogOutput,
				UpdateIntervalString:    DefaultUpdateInterval,
				ResyncIntervalString:    DefaultResyncInterval,
				HeartbeatIntervalString: DefaultHeartbeatInterval,
				UpdateInterval:          10 * time.Second,
				ResyncInterval:          60 * time.Second,
				HeartbeatInterval:       5 * time.Minute,
				QPS:                     DefaultQPS,
				Burst:                   DefaultBurst,
			},
			wantErr: false,
		},
		{
			name: "partial settings preserve existing values",
			input: GlobalSettings{
				LogLevel:             "debug",
				UpdateIntervalString: "5s",
			},
			expected: GlobalSettings{
				LogLevel:                "debug",
				LogFormat:               DefaultLogFormat,
				LogOutput:               DefaultLogOutput,
				UpdateIntervalString:    "5s",
				ResyncIntervalString:    DefaultResyncInterval,
				HeartbeatIntervalString: DefaultHeartbeatInterval,
				UpdateInterval:          5 * time.Second,
				ResyncInterval:          60 * time.Second,
				HeartbeatInterval:       5 * time.Minute,
				QPS:                     DefaultQPS,
				Burst:                   DefaultBurst,
			},
			wantErr: false,
		},
		{
			name: "invalid duration string",
			input: GlobalSettings{
				UpdateIntervalString: "invalid-duration",
			},
			wantErr: true,
		},
		{
			name: "invalid resync duration",
			input: GlobalSettings{
				ResyncIntervalString: "not-a-duration",
			},
			wantErr: true,
		},
		{
			name: "invalid heartbeat duration",
			input: GlobalSettings{
				HeartbeatIntervalString: "bad-duration",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.ApplyDefaults()
			if (err != nil) != tt.wantErr {
				t.Errorf("ApplyDefaults() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				// Check all fields match
				if tt.input.LogLevel != tt.expected.LogLevel {
					t.Errorf("LogLevel = %v, want %v", tt.input.LogLevel, tt.expected.LogLevel)
				}
				if tt.input.LogFormat != tt.expected.LogFormat {
					t.Errorf("LogFormat = %v, want %v", tt.input.LogFormat, tt.expected.LogFormat)
				}
				if tt.input.UpdateInterval != tt.expected.UpdateInterval {
					t.Errorf("UpdateInterval = %v, want %v", tt.input.UpdateInterval, tt.expected.UpdateInterval)
				}
				if tt.input.QPS != tt.expected.QPS {
					t.Errorf("QPS = %v, want %v", tt.input.QPS, tt.expected.QPS)
				}
				if tt.input.Burst != tt.expected.Burst {
					t.Errorf("Burst = %v, want %v", tt.input.Burst, tt.expected.Burst)
				}
			}
		})
	}
}

// TestMonitorConfigApplyDefaults tests monitor config defaults
func TestMonitorConfigApplyDefaults(t *testing.T) {
	tests := []struct {
		name     string
		input    MonitorConfig
		expected MonitorConfig
		wantErr  bool
	}{
		{
			name:  "empty monitor gets defaults",
			input: MonitorConfig{},
			expected: MonitorConfig{
				IntervalString: DefaultMonitorInterval,
				TimeoutString:  DefaultMonitorTimeout,
				Interval:       30 * time.Second,
				Timeout:        10 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "monitor with remediation applies nested defaults",
			input: MonitorConfig{
				Remediation: &MonitorRemediationConfig{},
			},
			expected: MonitorConfig{
				IntervalString: DefaultMonitorInterval,
				TimeoutString:  DefaultMonitorTimeout,
				Interval:       30 * time.Second,
				Timeout:        10 * time.Second,
				Remediation: &MonitorRemediationConfig{
					CooldownString: DefaultCooldownPeriod,
					Cooldown:       5 * time.Minute,
					MaxAttempts:    DefaultMaxAttemptsGlobal,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid interval duration",
			input: MonitorConfig{
				IntervalString: "invalid",
			},
			wantErr: true,
		},
		{
			name: "invalid timeout duration",
			input: MonitorConfig{
				TimeoutString: "invalid",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.ApplyDefaults()
			if (err != nil) != tt.wantErr {
				t.Errorf("ApplyDefaults() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if tt.input.Interval != tt.expected.Interval {
					t.Errorf("Interval = %v, want %v", tt.input.Interval, tt.expected.Interval)
				}
				if tt.input.Timeout != tt.expected.Timeout {
					t.Errorf("Timeout = %v, want %v", tt.input.Timeout, tt.expected.Timeout)
				}
				if tt.input.Remediation != nil && tt.expected.Remediation != nil {
					if tt.input.Remediation.Cooldown != tt.expected.Remediation.Cooldown {
						t.Errorf("Remediation.Cooldown = %v, want %v", tt.input.Remediation.Cooldown, tt.expected.Remediation.Cooldown)
					}
				}
			}
		})
	}
}

// TestRemediationConfigApplyDefaults tests remediation config defaults
func TestRemediationConfigApplyDefaults(t *testing.T) {
	tests := []struct {
		name     string
		input    RemediationConfig
		expected RemediationConfig
		wantErr  bool
	}{
		{
			name:  "empty remediation gets defaults",
			input: RemediationConfig{},
			expected: RemediationConfig{
				MaxRemediationsPerHour:   DefaultMaxRemediationsPerHour,
				MaxRemediationsPerMinute: DefaultMaxRemediationsPerMinute,
				CooldownPeriodString:     DefaultCooldownPeriod,
				CooldownPeriod:           5 * time.Minute,
				MaxAttemptsGlobal:        DefaultMaxAttemptsGlobal,
				HistorySize:              DefaultHistorySize,
			},
			wantErr: false,
		},
		{
			name: "circuit breaker enabled gets defaults",
			input: RemediationConfig{
				CircuitBreaker: CircuitBreakerConfig{
					Enabled: true,
				},
			},
			expected: RemediationConfig{
				MaxRemediationsPerHour:   DefaultMaxRemediationsPerHour,
				MaxRemediationsPerMinute: DefaultMaxRemediationsPerMinute,
				CooldownPeriodString:     DefaultCooldownPeriod,
				CooldownPeriod:           5 * time.Minute,
				MaxAttemptsGlobal:        DefaultMaxAttemptsGlobal,
				HistorySize:              DefaultHistorySize,
				CircuitBreaker: CircuitBreakerConfig{
					Enabled:          true,
					Threshold:        DefaultCircuitBreakerThreshold,
					TimeoutString:    DefaultCircuitBreakerTimeout,
					Timeout:          30 * time.Minute,
					SuccessThreshold: 2,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid cooldown duration",
			input: RemediationConfig{
				CooldownPeriodString: "invalid",
			},
			wantErr: true,
		},
		{
			name: "invalid circuit breaker timeout",
			input: RemediationConfig{
				CircuitBreaker: CircuitBreakerConfig{
					Enabled:       true,
					TimeoutString: "invalid",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.ApplyDefaults()
			if (err != nil) != tt.wantErr {
				t.Errorf("ApplyDefaults() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if tt.input.MaxRemediationsPerHour != tt.expected.MaxRemediationsPerHour {
					t.Errorf("MaxRemediationsPerHour = %v, want %v", tt.input.MaxRemediationsPerHour, tt.expected.MaxRemediationsPerHour)
				}
				if tt.input.CooldownPeriod != tt.expected.CooldownPeriod {
					t.Errorf("CooldownPeriod = %v, want %v", tt.input.CooldownPeriod, tt.expected.CooldownPeriod)
				}
			}
		})
	}
}

// TestGlobalSettingsValidation tests validation logic
func TestGlobalSettingsValidation(t *testing.T) {
	tests := []struct {
		name    string
		input   GlobalSettings
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid settings",
			input: GlobalSettings{
				NodeName:          "test-node",
				LogLevel:          "info",
				LogFormat:         "json",
				LogOutput:         "stdout",
				UpdateInterval:    10 * time.Second,
				ResyncInterval:    60 * time.Second,
				HeartbeatInterval: 5 * time.Minute,
				QPS:               50,
				Burst:             100,
			},
			wantErr: false,
		},
		{
			name: "missing node name",
			input: GlobalSettings{
				LogLevel:          "info",
				UpdateInterval:    10 * time.Second,
				ResyncInterval:    60 * time.Second,
				HeartbeatInterval: 5 * time.Minute,
				QPS:               50,
				Burst:             100,
			},
			wantErr: true,
			errMsg:  "nodeName is required",
		},
		{
			name: "invalid log level",
			input: GlobalSettings{
				NodeName:          "test-node",
				LogLevel:          "invalid",
				UpdateInterval:    10 * time.Second,
				ResyncInterval:    60 * time.Second,
				HeartbeatInterval: 5 * time.Minute,
				QPS:               50,
				Burst:             100,
			},
			wantErr: true,
			errMsg:  "invalid logLevel",
		},
		{
			name: "invalid log format",
			input: GlobalSettings{
				NodeName:          "test-node",
				LogLevel:          "info",
				LogFormat:         "invalid",
				UpdateInterval:    10 * time.Second,
				ResyncInterval:    60 * time.Second,
				HeartbeatInterval: 5 * time.Minute,
				QPS:               50,
				Burst:             100,
			},
			wantErr: true,
			errMsg:  "invalid logFormat",
		},
		{
			name: "invalid log output",
			input: GlobalSettings{
				NodeName:          "test-node",
				LogLevel:          "info",
				LogFormat:         "json",
				LogOutput:         "invalid",
				UpdateInterval:    10 * time.Second,
				ResyncInterval:    60 * time.Second,
				HeartbeatInterval: 5 * time.Minute,
				QPS:               50,
				Burst:             100,
			},
			wantErr: true,
			errMsg:  "invalid logOutput",
		},
		{
			name: "file output without log file",
			input: GlobalSettings{
				NodeName:          "test-node",
				LogLevel:          "info",
				LogFormat:         "json",
				LogOutput:         "file",
				UpdateInterval:    10 * time.Second,
				ResyncInterval:    60 * time.Second,
				HeartbeatInterval: 5 * time.Minute,
				QPS:               50,
				Burst:             100,
			},
			wantErr: true,
			errMsg:  "logFile is required when logOutput is 'file'",
		},
		{
			name: "negative update interval",
			input: GlobalSettings{
				NodeName:          "test-node",
				LogLevel:          "info",
				LogFormat:         "json",
				LogOutput:         "stdout",
				UpdateInterval:    -1 * time.Second,
				ResyncInterval:    60 * time.Second,
				HeartbeatInterval: 5 * time.Minute,
				QPS:               50,
				Burst:             100,
			},
			wantErr: true,
			errMsg:  "updateInterval must be positive",
		},
		{
			name: "zero QPS",
			input: GlobalSettings{
				NodeName:          "test-node",
				LogLevel:          "info",
				LogFormat:         "json",
				LogOutput:         "stdout",
				UpdateInterval:    10 * time.Second,
				ResyncInterval:    60 * time.Second,
				HeartbeatInterval: 5 * time.Minute,
				QPS:               0,
				Burst:             100,
			},
			wantErr: true,
			errMsg:  "qps must be positive",
		},
		{
			name: "zero burst",
			input: GlobalSettings{
				NodeName:          "test-node",
				LogLevel:          "info",
				LogFormat:         "json",
				LogOutput:         "stdout",
				UpdateInterval:    10 * time.Second,
				ResyncInterval:    60 * time.Second,
				HeartbeatInterval: 5 * time.Minute,
				QPS:               50,
				Burst:             0,
			},
			wantErr: true,
			errMsg:  "burst must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, want error containing %v", err, tt.errMsg)
				}
			}
		})
	}
}

// TestMonitorConfigValidation tests monitor validation
func TestMonitorConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		input   MonitorConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid monitor",
			input: MonitorConfig{
				Name:     "test-monitor",
				Type:     "test-type",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "missing name",
			input: MonitorConfig{
				Type:     "test-type",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
			},
			wantErr: true,
			errMsg:  "name is required",
		},
		{
			name: "missing type",
			input: MonitorConfig{
				Name:     "test-monitor",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
			},
			wantErr: true,
			errMsg:  "type is required",
		},
		{
			name: "timeout >= interval",
			input: MonitorConfig{
				Name:     "test-monitor",
				Type:     "test-type",
				Interval: 10 * time.Second,
				Timeout:  10 * time.Second,
			},
			wantErr: true,
			errMsg:  "timeout",
		},
		{
			name: "negative interval",
			input: MonitorConfig{
				Name:     "test-monitor",
				Type:     "test-type",
				Interval: -1 * time.Second,
				Timeout:  10 * time.Second,
			},
			wantErr: true,
			errMsg:  "interval must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, want error containing %v", err, tt.errMsg)
				}
			}
		})
	}
}

// TestMonitorRemediationConfigValidation tests monitor remediation validation
func TestMonitorRemediationConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		input   MonitorRemediationConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "disabled remediation is valid",
			input: MonitorRemediationConfig{
				Enabled: false,
			},
			wantErr: false,
		},
		{
			name: "valid systemd-restart remediation",
			input: MonitorRemediationConfig{
				Enabled:     true,
				Strategy:    "systemd-restart",
				Service:     "kubelet",
				Cooldown:    5 * time.Minute,
				MaxAttempts: 3,
			},
			wantErr: false,
		},
		{
			name: "missing strategy when enabled",
			input: MonitorRemediationConfig{
				Enabled:     true,
				Cooldown:    5 * time.Minute,
				MaxAttempts: 3,
			},
			wantErr: true,
			errMsg:  "strategy is required when remediation is enabled",
		},
		{
			name: "invalid strategy",
			input: MonitorRemediationConfig{
				Enabled:     true,
				Strategy:    "invalid-strategy",
				Cooldown:    5 * time.Minute,
				MaxAttempts: 3,
			},
			wantErr: true,
			errMsg:  "invalid strategy",
		},
		{
			name: "systemd-restart without service",
			input: MonitorRemediationConfig{
				Enabled:     true,
				Strategy:    "systemd-restart",
				Cooldown:    5 * time.Minute,
				MaxAttempts: 3,
			},
			wantErr: true,
			errMsg:  "service is required for systemd-restart strategy",
		},
		{
			name: "custom-script without script path",
			input: MonitorRemediationConfig{
				Enabled:     true,
				Strategy:    "custom-script",
				Cooldown:    5 * time.Minute,
				MaxAttempts: 3,
			},
			wantErr: true,
			errMsg:  "scriptPath is required for custom-script strategy",
		},
		{
			name: "negative cooldown",
			input: MonitorRemediationConfig{
				Enabled:     true,
				Strategy:    "systemd-restart",
				Service:     "kubelet",
				Cooldown:    -1 * time.Minute,
				MaxAttempts: 3,
			},
			wantErr: true,
			errMsg:  "cooldown must be positive",
		},
		{
			name: "zero max attempts",
			input: MonitorRemediationConfig{
				Enabled:     true,
				Strategy:    "systemd-restart",
				Service:     "kubelet",
				Cooldown:    5 * time.Minute,
				MaxAttempts: 0,
			},
			wantErr: true,
			errMsg:  "maxAttempts must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, want error containing %v", err, tt.errMsg)
				}
			}
		})
	}
}

// TestHTTPExporterConfigValidation tests HTTP exporter validation
func TestHTTPExporterConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		input   HTTPExporterConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "disabled exporter is valid",
			input: HTTPExporterConfig{
				Enabled: false,
			},
			wantErr: false,
		},
		{
			name: "valid HTTP exporter with webhook",
			input: HTTPExporterConfig{
				Enabled:   true,
				Workers:   3,
				QueueSize: 100,
				Timeout:   30 * time.Second,
				Webhooks: []WebhookEndpoint{
					{
						Name:         "test-webhook",
						URL:          "https://example.com/webhook",
						Timeout:      10 * time.Second,
						SendStatus:   true,
						SendProblems: true,
						Auth: AuthConfig{
							Type: "none",
						},
						Retry: &RetryConfig{
							MaxAttempts: 3,
							BaseDelay:   1 * time.Second,
							MaxDelay:    10 * time.Second,
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "enabled exporter without webhooks",
			input: HTTPExporterConfig{
				Enabled:   true,
				Workers:   3,
				QueueSize: 100,
				Timeout:   30 * time.Second,
			},
			wantErr: true,
			errMsg:  "at least one webhook or controller must be configured",
		},
		{
			name: "invalid workers count",
			input: HTTPExporterConfig{
				Enabled:   true,
				Workers:   0,
				QueueSize: 100,
				Timeout:   30 * time.Second,
				Webhooks: []WebhookEndpoint{
					{URL: "https://example.com/webhook"},
				},
			},
			wantErr: true,
			errMsg:  "workers must be positive",
		},
		{
			name: "invalid queue size",
			input: HTTPExporterConfig{
				Enabled:   true,
				Workers:   3,
				QueueSize: 0,
				Timeout:   30 * time.Second,
				Webhooks: []WebhookEndpoint{
					{URL: "https://example.com/webhook"},
				},
			},
			wantErr: true,
			errMsg:  "queueSize must be positive",
		},
		{
			name: "invalid timeout",
			input: HTTPExporterConfig{
				Enabled:   true,
				Workers:   3,
				QueueSize: 100,
				Timeout:   0,
				Webhooks: []WebhookEndpoint{
					{URL: "https://example.com/webhook"},
				},
			},
			wantErr: true,
			errMsg:  "timeout must be positive",
		},
		{
			name: "webhook without URL",
			input: HTTPExporterConfig{
				Enabled:   true,
				Workers:   3,
				QueueSize: 100,
				Timeout:   30 * time.Second,
				Webhooks: []WebhookEndpoint{
					{Name: "test"},
				},
			},
			wantErr: true,
			errMsg:  "url is required",
		},
		{
			name: "webhook with invalid URL",
			input: HTTPExporterConfig{
				Enabled:   true,
				Workers:   3,
				QueueSize: 100,
				Timeout:   30 * time.Second,
				Webhooks: []WebhookEndpoint{
					{URL: "invalid-url"},
				},
			},
			wantErr: true,
			errMsg:  "url must start with http:// or https://",
		},
		{
			name: "duplicate webhook names",
			input: HTTPExporterConfig{
				Enabled:   true,
				Workers:   3,
				QueueSize: 100,
				Timeout:   30 * time.Second,
				Webhooks: []WebhookEndpoint{
					{
						Name:         "test",
						URL:          "https://example.com/webhook1",
						Timeout:      10 * time.Second,
						SendStatus:   true,
						SendProblems: true,
						Auth: AuthConfig{
							Type: "none",
						},
						Retry: &RetryConfig{
							MaxAttempts: 3,
							BaseDelay:   1 * time.Second,
							MaxDelay:    10 * time.Second,
						},
					},
					{
						Name:         "test",
						URL:          "https://example.com/webhook2",
						Timeout:      10 * time.Second,
						SendStatus:   true,
						SendProblems: true,
						Auth: AuthConfig{
							Type: "none",
						},
						Retry: &RetryConfig{
							MaxAttempts: 3,
							BaseDelay:   1 * time.Second,
							MaxDelay:    10 * time.Second,
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "duplicate webhook name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Ensure retry config has valid defaults for enabled exporters
			// (unless we're specifically testing retry validation)
			if tt.input.Enabled && tt.input.Retry.BaseDelay == 0 {
				tt.input.Retry = RetryConfig{
					MaxAttempts: 3,
					BaseDelay:   1 * time.Second,
					MaxDelay:    30 * time.Second,
				}
			}

			err := tt.input.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, want error containing %v", err, tt.errMsg)
				}
			}
		})
	}
}

// TestPrometheusExporterConfigValidation tests Prometheus exporter validation
func TestPrometheusExporterConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		input   PrometheusExporterConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "disabled exporter is valid",
			input: PrometheusExporterConfig{
				Enabled: false,
			},
			wantErr: false,
		},
		{
			name: "valid Prometheus exporter",
			input: PrometheusExporterConfig{
				Enabled:   true,
				Port:      9100,
				Path:      "/metrics",
				Namespace: "node_doctor",
			},
			wantErr: false,
		},
		{
			name: "port out of range",
			input: PrometheusExporterConfig{
				Enabled: true,
				Port:    0,
			},
			wantErr: true,
			errMsg:  "port must be in range 1-65535",
		},
		{
			name: "path not starting with /",
			input: PrometheusExporterConfig{
				Enabled: true,
				Port:    9100,
				Path:    "metrics",
			},
			wantErr: true,
			errMsg:  "path must start with '/'",
		},
		{
			name: "invalid namespace format",
			input: PrometheusExporterConfig{
				Enabled:   true,
				Port:      9100,
				Path:      "/metrics",
				Namespace: "123invalid",
			},
			wantErr: true,
			errMsg:  "namespace",
		},
		{
			name: "invalid subsystem format",
			input: PrometheusExporterConfig{
				Enabled:   true,
				Port:      9100,
				Path:      "/metrics",
				Namespace: "valid_namespace",
				Subsystem: "123invalid",
			},
			wantErr: true,
			errMsg:  "subsystem",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, want error containing %v", err, tt.errMsg)
				}
			}
		})
	}
}

// TestNodeDoctorConfigValidation tests top-level config validation
func TestNodeDoctorConfigValidation(t *testing.T) {
	validConfig := &NodeDoctorConfig{
		APIVersion: "node-doctor.io/v1alpha1",
		Kind:       "NodeDoctorConfig",
		Metadata: ConfigMetadata{
			Name: "test-config",
		},
		Settings: GlobalSettings{
			NodeName:          "test-node",
			LogLevel:          "info",
			LogFormat:         "json",
			LogOutput:         "stdout",
			UpdateInterval:    10 * time.Second,
			ResyncInterval:    60 * time.Second,
			HeartbeatInterval: 5 * time.Minute,
			QPS:               50,
			Burst:             100,
		},
		Monitors: []MonitorConfig{
			{
				Name:     "test-monitor",
				Type:     "test-type",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
			},
		},
		Remediation: RemediationConfig{
			MaxRemediationsPerHour:   10,
			MaxRemediationsPerMinute: 2,
			CooldownPeriod:           5 * time.Minute,
			MaxAttemptsGlobal:        3,
			HistorySize:              100,
		},
	}

	tests := []struct {
		name     string
		input    *NodeDoctorConfig
		wantErr  bool
		errMsg   string
		modifyFn func(*NodeDoctorConfig)
	}{
		{
			name:    "valid config",
			input:   validConfig,
			wantErr: false,
		},
		{
			name:    "missing API version",
			input:   validConfig,
			wantErr: true,
			errMsg:  "apiVersion is required",
			modifyFn: func(c *NodeDoctorConfig) {
				c.APIVersion = ""
			},
		},
		{
			name:    "missing kind",
			input:   validConfig,
			wantErr: true,
			errMsg:  "kind is required",
			modifyFn: func(c *NodeDoctorConfig) {
				c.Kind = ""
			},
		},
		{
			name:    "wrong kind",
			input:   validConfig,
			wantErr: true,
			errMsg:  "kind must be 'NodeDoctorConfig'",
			modifyFn: func(c *NodeDoctorConfig) {
				c.Kind = "WrongKind"
			},
		},
		{
			name:    "missing metadata name",
			input:   validConfig,
			wantErr: true,
			errMsg:  "metadata.name is required",
			modifyFn: func(c *NodeDoctorConfig) {
				c.Metadata.Name = ""
			},
		},
		{
			name:    "duplicate monitor names",
			input:   validConfig,
			wantErr: true,
			errMsg:  "duplicate monitor name",
			modifyFn: func(c *NodeDoctorConfig) {
				c.Monitors = append(c.Monitors, MonitorConfig{
					Name:     "test-monitor", // Same name as first monitor
					Type:     "test-type-2",
					Interval: 30 * time.Second,
					Timeout:  10 * time.Second,
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a copy of the config to avoid modifying the original
			config := &NodeDoctorConfig{}
			*config = *tt.input
			config.Settings = tt.input.Settings
			config.Metadata = tt.input.Metadata
			config.Monitors = make([]MonitorConfig, len(tt.input.Monitors))
			copy(config.Monitors, tt.input.Monitors)
			config.Remediation = tt.input.Remediation

			if tt.modifyFn != nil {
				tt.modifyFn(config)
			}

			err := config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, want error containing %v", err, tt.errMsg)
				}
			}
		})
	}
}

// TestEnvironmentVariableSubstitution tests env var expansion
func TestEnvironmentVariableSubstitution(t *testing.T) {
	// Set test environment variables
	os.Setenv("TEST_NODE_NAME", "test-node-123")
	os.Setenv("TEST_NAMESPACE", "test-namespace")
	os.Setenv("TEST_VERSION", "v1.0.0")
	defer func() {
		os.Unsetenv("TEST_NODE_NAME")
		os.Unsetenv("TEST_NAMESPACE")
		os.Unsetenv("TEST_VERSION")
	}()

	config := &NodeDoctorConfig{
		Settings: GlobalSettings{
			NodeName: "${TEST_NODE_NAME}",
		},
		Monitors: []MonitorConfig{
			{
				Config: map[string]interface{}{
					"version":   "${TEST_VERSION}",
					"namespace": "${TEST_NAMESPACE}",
					"nested": map[string]interface{}{
						"value": "${TEST_NODE_NAME}",
					},
					"array": []interface{}{
						"${TEST_VERSION}",
						"static-value",
					},
				},
				Remediation: &MonitorRemediationConfig{
					Args: []string{
						"--node=${TEST_NODE_NAME}",
						"--version=${TEST_VERSION}",
					},
				},
			},
		},
		Exporters: ExporterConfigs{
			Kubernetes: &KubernetesExporterConfig{
				Namespace: "${TEST_NAMESPACE}",
				Annotations: []AnnotationConfig{
					{
						Key:   "node-doctor.io/node",
						Value: "${TEST_NODE_NAME}",
					},
				},
			},
			Prometheus: &PrometheusExporterConfig{
				Namespace: "${TEST_NAMESPACE}",
				Labels: map[string]string{
					"version": "${TEST_VERSION}",
				},
			},
		},
	}

	config.SubstituteEnvVars()

	// Test global settings substitution
	if config.Settings.NodeName != "test-node-123" {
		t.Errorf("Settings.NodeName = %v, want test-node-123", config.Settings.NodeName)
	}

	// Test monitor config substitution
	monitor := config.Monitors[0]
	if monitor.Config["version"] != "v1.0.0" {
		t.Errorf("Monitor config version = %v, want v1.0.0", monitor.Config["version"])
	}
	if monitor.Config["namespace"] != "test-namespace" {
		t.Errorf("Monitor config namespace = %v, want test-namespace", monitor.Config["namespace"])
	}

	// Test nested map substitution
	if nested, ok := monitor.Config["nested"].(map[string]interface{}); ok {
		if nested["value"] != "test-node-123" {
			t.Errorf("Nested value = %v, want test-node-123", nested["value"])
		}
	} else {
		t.Error("Expected nested map in monitor config")
	}

	// Test array substitution
	if arr, ok := monitor.Config["array"].([]interface{}); ok {
		if arr[0] != "v1.0.0" {
			t.Errorf("Array[0] = %v, want v1.0.0", arr[0])
		}
	} else {
		t.Error("Expected array in monitor config")
	}

	// Test remediation args substitution
	if monitor.Remediation.Args[0] != "--node=test-node-123" {
		t.Errorf("Remediation args[0] = %v, want --node=test-node-123", monitor.Remediation.Args[0])
	}

	// Test kubernetes exporter substitution
	if config.Exporters.Kubernetes.Namespace != "test-namespace" {
		t.Errorf("Kubernetes exporter namespace = %v, want test-namespace", config.Exporters.Kubernetes.Namespace)
	}
	if config.Exporters.Kubernetes.Annotations[0].Value != "test-node-123" {
		t.Errorf("Kubernetes exporter annotation value = %v, want test-node-123", config.Exporters.Kubernetes.Annotations[0].Value)
	}

	// Test prometheus exporter substitution
	if config.Exporters.Prometheus.Namespace != "test-namespace" {
		t.Errorf("Prometheus exporter namespace = %v, want test-namespace", config.Exporters.Prometheus.Namespace)
	}
	if config.Exporters.Prometheus.Labels["version"] != "v1.0.0" {
		t.Errorf("Prometheus exporter label version = %v, want v1.0.0", config.Exporters.Prometheus.Labels["version"])
	}
}

// TestFeatureFlagsApplyDefaults tests feature flags defaults
func TestFeatureFlagsApplyDefaults(t *testing.T) {
	tests := []struct {
		name     string
		input    FeatureFlags
		expected FeatureFlags
	}{
		{
			name:  "empty feature flags get defaults",
			input: FeatureFlags{},
			expected: FeatureFlags{
				EnableMetrics: true,
			},
		},
		{
			name: "existing values are preserved",
			input: FeatureFlags{
				EnableProfiling: true,
				ProfilingPort:   6060,
			},
			expected: FeatureFlags{
				EnableMetrics:   true,
				EnableProfiling: true,
				ProfilingPort:   6060,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.input.ApplyDefaults()
			if !reflect.DeepEqual(tt.input, tt.expected) {
				t.Errorf("ApplyDefaults() = %+v, want %+v", tt.input, tt.expected)
			}
		})
	}
}

// TestExporterConfigApplyDefaults tests exporter config defaults
func TestExporterConfigApplyDefaults(t *testing.T) {
	t.Run("HTTPExporterConfig", func(t *testing.T) {
		config := &HTTPExporterConfig{}
		err := config.ApplyDefaults()
		if err != nil {
			t.Errorf("ApplyDefaults() error = %v", err)
		}
		if config.Workers != 5 {
			t.Errorf("Workers = %v, want 5", config.Workers)
		}
		if config.QueueSize != 100 {
			t.Errorf("QueueSize = %v, want 100", config.QueueSize)
		}
		if config.Timeout != 30*time.Second {
			t.Errorf("Timeout = %v, want 30s", config.Timeout)
		}
	})

	t.Run("PrometheusExporterConfig", func(t *testing.T) {
		config := &PrometheusExporterConfig{}
		err := config.ApplyDefaults()
		if err != nil {
			t.Errorf("ApplyDefaults() error = %v", err)
		}
		if config.Port != DefaultPrometheusPort {
			t.Errorf("Port = %v, want %v", config.Port, DefaultPrometheusPort)
		}
		if config.Path != DefaultPrometheusPath {
			t.Errorf("Path = %v, want %v", config.Path, DefaultPrometheusPath)
		}
		if config.Namespace != "node_doctor" {
			t.Errorf("Namespace = %v, want node_doctor", config.Namespace)
		}
	})

	t.Run("KubernetesExporterConfig", func(t *testing.T) {
		config := &KubernetesExporterConfig{}
		err := config.ApplyDefaults()
		if err != nil {
			t.Errorf("ApplyDefaults() error = %v", err)
		}
		if config.UpdateInterval != 10*time.Second {
			t.Errorf("UpdateInterval = %v, want 10s", config.UpdateInterval)
		}
		if config.ResyncInterval != 60*time.Second {
			t.Errorf("ResyncInterval = %v, want 60s", config.ResyncInterval)
		}
		if config.HeartbeatInterval != 5*time.Minute {
			t.Errorf("HeartbeatInterval = %v, want 5m", config.HeartbeatInterval)
		}
	})
}

// TestKubernetesExporterConfigValidation tests Kubernetes exporter validation
func TestKubernetesExporterConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		input   KubernetesExporterConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "disabled exporter is valid",
			input: KubernetesExporterConfig{
				Enabled: false,
			},
			wantErr: false,
		},
		{
			name: "valid kubernetes exporter",
			input: KubernetesExporterConfig{
				Enabled:           true,
				UpdateInterval:    10 * time.Second,
				ResyncInterval:    60 * time.Second,
				HeartbeatInterval: 5 * time.Minute,
			},
			wantErr: false,
		},
		{
			name: "negative update interval",
			input: KubernetesExporterConfig{
				Enabled:           true,
				UpdateInterval:    -1 * time.Second,
				ResyncInterval:    60 * time.Second,
				HeartbeatInterval: 5 * time.Minute,
			},
			wantErr: true,
			errMsg:  "updateInterval must be positive",
		},
		{
			name: "condition without type",
			input: KubernetesExporterConfig{
				Enabled:           true,
				UpdateInterval:    10 * time.Second,
				ResyncInterval:    60 * time.Second,
				HeartbeatInterval: 5 * time.Minute,
				Conditions: []ConditionConfig{
					{},
				},
			},
			wantErr: true,
			errMsg:  "type is required",
		},
		{
			name: "annotation without key",
			input: KubernetesExporterConfig{
				Enabled:           true,
				UpdateInterval:    10 * time.Second,
				ResyncInterval:    60 * time.Second,
				HeartbeatInterval: 5 * time.Minute,
				Annotations: []AnnotationConfig{
					{Value: "test"},
				},
			},
			wantErr: true,
			errMsg:  "key is required",
		},
		{
			name: "negative max events per minute",
			input: KubernetesExporterConfig{
				Enabled:           true,
				UpdateInterval:    10 * time.Second,
				ResyncInterval:    60 * time.Second,
				HeartbeatInterval: 5 * time.Minute,
				Events: EventConfig{
					MaxEventsPerMinute: -1,
				},
			},
			wantErr: true,
			errMsg:  "maxEventsPerMinute must be non-negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, want error containing %v", err, tt.errMsg)
				}
			}
		})
	}
}

// TestRemediationConfigValidation tests remediation config validation
func TestRemediationConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		input   RemediationConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid remediation config",
			input: RemediationConfig{
				MaxRemediationsPerHour:   10,
				MaxRemediationsPerMinute: 2,
				CooldownPeriod:           5 * time.Minute,
				MaxAttemptsGlobal:        3,
				HistorySize:              100,
			},
			wantErr: false,
		},
		{
			name: "negative max remediations per hour",
			input: RemediationConfig{
				MaxRemediationsPerHour: -1,
				CooldownPeriod:         5 * time.Minute,
				MaxAttemptsGlobal:      3,
				HistorySize:            100,
			},
			wantErr: true,
			errMsg:  "maxRemediationsPerHour must be non-negative",
		},
		{
			name: "zero cooldown period",
			input: RemediationConfig{
				MaxRemediationsPerHour:   10,
				MaxRemediationsPerMinute: 2,
				CooldownPeriod:           0,
				MaxAttemptsGlobal:        3,
				HistorySize:              100,
			},
			wantErr: true,
			errMsg:  "cooldownPeriod must be positive",
		},
		{
			name: "zero max attempts global",
			input: RemediationConfig{
				MaxRemediationsPerHour:   10,
				MaxRemediationsPerMinute: 2,
				CooldownPeriod:           5 * time.Minute,
				MaxAttemptsGlobal:        0,
				HistorySize:              100,
			},
			wantErr: true,
			errMsg:  "maxAttemptsGlobal must be positive",
		},
		{
			name: "circuit breaker with invalid threshold",
			input: RemediationConfig{
				MaxRemediationsPerHour:   10,
				MaxRemediationsPerMinute: 2,
				CooldownPeriod:           5 * time.Minute,
				MaxAttemptsGlobal:        3,
				HistorySize:              100,
				CircuitBreaker: CircuitBreakerConfig{
					Enabled:   true,
					Threshold: 0,
					Timeout:   30 * time.Minute,
				},
			},
			wantErr: true,
			errMsg:  "circuitBreaker.threshold must be positive",
		},
		{
			name: "duplicate override problem",
			input: RemediationConfig{
				MaxRemediationsPerHour:   10,
				MaxRemediationsPerMinute: 2,
				CooldownPeriod:           5 * time.Minute,
				MaxAttemptsGlobal:        3,
				HistorySize:              100,
				Overrides: []RemediationOverride{
					{Problem: "test-problem"},
					{Problem: "test-problem"},
				},
			},
			wantErr: true,
			errMsg:  "duplicate override for problem",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, want error containing %v", err, tt.errMsg)
				}
			}
		})
	}
}

// TestNodeDoctorConfigApplyDefaults tests top-level config defaults
func TestNodeDoctorConfigApplyDefaults(t *testing.T) {
	config := &NodeDoctorConfig{
		Settings: GlobalSettings{},
		Monitors: []MonitorConfig{
			{},
		},
		Exporters: ExporterConfigs{
			Kubernetes: &KubernetesExporterConfig{},
			HTTP:       &HTTPExporterConfig{},
			Prometheus: &PrometheusExporterConfig{},
		},
		Remediation: RemediationConfig{},
	}

	err := config.ApplyDefaults()
	if err != nil {
		t.Errorf("ApplyDefaults() error = %v", err)
	}

	// Check that defaults were applied to settings
	if config.Settings.LogLevel != DefaultLogLevel {
		t.Errorf("Settings.LogLevel = %v, want %v", config.Settings.LogLevel, DefaultLogLevel)
	}

	// Check that defaults were applied to monitors
	if config.Monitors[0].Interval != 30*time.Second {
		t.Errorf("Monitor[0].Interval = %v, want 30s", config.Monitors[0].Interval)
	}

	// Check that defaults were applied to exporters
	if config.Exporters.HTTP.Workers != 5 {
		t.Errorf("HTTP.Workers = %v, want 5", config.Exporters.HTTP.Workers)
	}

	// Check that defaults were applied to remediation
	if config.Remediation.MaxRemediationsPerHour != DefaultMaxRemediationsPerHour {
		t.Errorf("Remediation.MaxRemediationsPerHour = %v, want %v", config.Remediation.MaxRemediationsPerHour, DefaultMaxRemediationsPerHour)
	}

	// Check that feature flags were applied
	if !config.Features.EnableMetrics {
		t.Error("Features.EnableMetrics should be true by default")
	}
}

// TestMonitorRemediationConfigApplyDefaults tests monitor remediation defaults
func TestMonitorRemediationConfigApplyDefaults(t *testing.T) {
	config := &MonitorRemediationConfig{
		Strategies: []MonitorRemediationConfig{
			{}, // nested strategy should also get defaults
		},
	}

	err := config.ApplyDefaults()
	if err != nil {
		t.Errorf("ApplyDefaults() error = %v", err)
	}

	if config.Cooldown != 5*time.Minute {
		t.Errorf("Cooldown = %v, want 5m", config.Cooldown)
	}
	if config.MaxAttempts != DefaultMaxAttemptsGlobal {
		t.Errorf("MaxAttempts = %v, want %v", config.MaxAttempts, DefaultMaxAttemptsGlobal)
	}

	// Check nested strategy got defaults too
	if config.Strategies[0].Cooldown != 5*time.Minute {
		t.Errorf("Strategies[0].Cooldown = %v, want 5m", config.Strategies[0].Cooldown)
	}
}

// TestEnvironmentSubstitutionMissingVars tests handling of missing env vars
func TestEnvironmentSubstitutionMissingVars(t *testing.T) {
	config := &NodeDoctorConfig{
		Settings: GlobalSettings{
			NodeName: "${MISSING_VAR}",
		},
	}

	config.SubstituteEnvVars()

	// os.ExpandEnv returns empty string for undefined variables
	// This is the actual behavior of os.ExpandEnv
	if config.Settings.NodeName != "" {
		t.Errorf("Settings.NodeName = %q, want empty string", config.Settings.NodeName)
	}
}

// TestRemediationOverrideValidation tests override validation
func TestRemediationOverrideValidation(t *testing.T) {
	config := &RemediationConfig{
		MaxRemediationsPerHour:   10,
		MaxRemediationsPerMinute: 2,
		CooldownPeriod:           5 * time.Minute,
		MaxAttemptsGlobal:        3,
		HistorySize:              100,
		Overrides: []RemediationOverride{
			{
				Problem:        "test-problem",
				CooldownString: "10m",
				Cooldown:       10 * time.Minute,
				MaxAttempts:    5,
			},
		},
	}

	err := config.Validate()
	if err != nil {
		t.Errorf("Validate() error = %v", err)
	}

	// Test missing problem name
	config.Overrides[0].Problem = ""
	err = config.Validate()
	if err == nil {
		t.Error("Validate() should fail with missing problem name")
	}
	if !strings.Contains(err.Error(), "problem is required") {
		t.Errorf("Validate() error = %v, want error containing 'problem is required'", err)
	}
}

// TestYAMLMapInterfaceHandling tests handling of YAML-style interface{} maps
func TestYAMLMapInterfaceHandling(t *testing.T) {
	// Create a config with YAML-style map[interface{}]interface{}
	yamlStyleMap := map[interface{}]interface{}{
		"string_key": "value",
		"nested": map[interface{}]interface{}{
			"inner_key": "${TEST_VAR}",
		},
	}

	config := &MonitorConfig{
		Config: map[string]interface{}{
			"yaml_map": yamlStyleMap,
		},
	}

	// Set environment variable
	os.Setenv("TEST_VAR", "substituted_value")
	defer os.Unsetenv("TEST_VAR")

	config.SubstituteEnvVars()

	// The function should handle the conversion gracefully
	if yamlMap, ok := config.Config["yaml_map"].(map[string]interface{}); ok {
		if nested, ok := yamlMap["nested"].(map[string]interface{}); ok {
			if nested["inner_key"] != "substituted_value" {
				t.Errorf("Expected substituted_value, got %v", nested["inner_key"])
			}
		} else {
			t.Error("Expected nested map to be converted to map[string]interface{}")
		}
	} else {
		t.Error("Expected yaml_map to be converted to map[string]interface{}")
	}
}

// TestEdgeCasesInValidation tests edge cases in validation
func TestEdgeCasesInValidation(t *testing.T) {
	t.Run("EventConfig with duration fields", func(t *testing.T) {
		config := &KubernetesExporterConfig{
			Enabled:           true,
			UpdateInterval:    10 * time.Second,
			ResyncInterval:    60 * time.Second,
			HeartbeatInterval: 5 * time.Minute,
			Events: EventConfig{
				EventTTLString:            "1h",
				EventTTL:                  -1 * time.Hour, // negative duration
				DeduplicationWindowString: "5m",
				DeduplicationWindow:       0, // zero duration
			},
		}

		err := config.Validate()
		if err == nil {
			t.Error("Validate() should fail with negative EventTTL")
		}
	})

	t.Run("MonitorRemediationConfig with WaitTimeout", func(t *testing.T) {
		config := &MonitorRemediationConfig{
			Enabled:           true,
			Strategy:          "systemd-restart",
			Service:           "kubelet",
			Cooldown:          5 * time.Minute,
			MaxAttempts:       3,
			WaitTimeoutString: "30s",
			WaitTimeout:       -30 * time.Second, // negative timeout
		}

		err := config.Validate()
		if err == nil {
			t.Error("Validate() should fail with negative WaitTimeout")
		}
	})
}

// TestEnvVarSubstitutionInSlices tests substitution in various slice types
func TestEnvVarSubstitutionInSlices(t *testing.T) {
	os.Setenv("TEST_ARG", "test-value")
	defer os.Unsetenv("TEST_ARG")

	config := &MonitorConfig{
		Config: map[string]interface{}{
			"args": []interface{}{
				"--config=${TEST_ARG}",
				123, // non-string value
				true,
			},
			"nested_arrays": []interface{}{
				[]interface{}{
					"${TEST_ARG}",
					"static",
				},
			},
		},
	}

	config.SubstituteEnvVars()

	// Check array substitution worked
	if args, ok := config.Config["args"].([]interface{}); ok {
		if args[0] != "--config=test-value" {
			t.Errorf("args[0] = %v, want --config=test-value", args[0])
		}
		// Non-string values should be preserved
		if args[1] != 123 {
			t.Errorf("args[1] = %v, want 123", args[1])
		}
		if args[2] != true {
			t.Errorf("args[2] = %v, want true", args[2])
		}
	} else {
		t.Error("Expected args to be []interface{}")
	}

	// Check nested array substitution
	if nestedArrays, ok := config.Config["nested_arrays"].([]interface{}); ok {
		if innerArray, ok := nestedArrays[0].([]interface{}); ok {
			if innerArray[0] != "test-value" {
				t.Errorf("nested array[0] = %v, want test-value", innerArray[0])
			}
		} else {
			t.Error("Expected nested array to be []interface{}")
		}
	} else {
		t.Error("Expected nested_arrays to be []interface{}")
	}
}

// TestComplexConfigurationValidation tests a complex configuration
func TestComplexConfigurationValidation(t *testing.T) {
	config := &NodeDoctorConfig{
		APIVersion: "node-doctor.io/v1alpha1",
		Kind:       "NodeDoctorConfig",
		Metadata: ConfigMetadata{
			Name: "complex-config",
		},
		Settings: GlobalSettings{
			NodeName:          "test-node",
			LogLevel:          "debug",
			LogFormat:         "json",
			LogOutput:         "stdout",
			UpdateInterval:    5 * time.Second,
			ResyncInterval:    30 * time.Second,
			HeartbeatInterval: 2 * time.Minute,
			QPS:               100,
			Burst:             200,
		},
		Monitors: []MonitorConfig{
			{
				Name:     "kubelet-health",
				Type:     "kubernetes-kubelet-check",
				Enabled:  true,
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Remediation: &MonitorRemediationConfig{
					Enabled:     true,
					Strategy:    "systemd-restart",
					Service:     "kubelet",
					Cooldown:    5 * time.Minute,
					MaxAttempts: 3,
					Strategies: []MonitorRemediationConfig{
						{
							Strategy:       "custom-script",
							ScriptPath:     "/bin/true", // exists on most systems
							CooldownString: "10m",
							MaxAttempts:    1,
						},
					},
				},
			},
		},
		Exporters: ExporterConfigs{
			Kubernetes: &KubernetesExporterConfig{
				Enabled:           true,
				UpdateInterval:    10 * time.Second,
				ResyncInterval:    60 * time.Second,
				HeartbeatInterval: 5 * time.Minute,
				Conditions: []ConditionConfig{
					{
						Type:          "NodeHealthy",
						DefaultStatus: "True",
					},
				},
				Annotations: []AnnotationConfig{
					{
						Key:   "node-doctor.io/health",
						Value: "ok",
					},
				},
				Events: EventConfig{
					MaxEventsPerMinute: 10,
				},
			},
			HTTP: &HTTPExporterConfig{
				Enabled:   true,
				Workers:   5,
				QueueSize: 100,
				Webhooks: []WebhookEndpoint{
					{
						Name:         "test-webhook",
						URL:          "https://example.com/webhook",
						SendStatus:   true,
						SendProblems: true,
					},
				},
			},
			Prometheus: &PrometheusExporterConfig{
				Enabled:   true,
				Port:      9100,
				Path:      "/metrics",
				Namespace: "node_doctor",
			},
		},
		Remediation: RemediationConfig{
			Enabled:                  true,
			MaxRemediationsPerHour:   20,
			MaxRemediationsPerMinute: 5,
			CooldownPeriod:           3 * time.Minute,
			MaxAttemptsGlobal:        5,
			HistorySize:              200,
			CircuitBreaker: CircuitBreakerConfig{
				Enabled:          true,
				Threshold:        10,
				Timeout:          45 * time.Minute,
				SuccessThreshold: 3,
			},
			Overrides: []RemediationOverride{
				{
					Problem:                 "kubelet-unhealthy",
					Cooldown:                2 * time.Minute,
					MaxAttempts:             10,
					CircuitBreakerThreshold: 15,
				},
			},
		},
	}

	// Apply defaults first
	err := config.ApplyDefaults()
	if err != nil {
		t.Fatalf("ApplyDefaults() error = %v", err)
	}

	// Validate the complex configuration
	err = config.Validate()
	if err != nil {
		t.Errorf("Validate() error = %v", err)
	}

	// Verify nested remediation got defaults
	nestedRemediation := config.Monitors[0].Remediation.Strategies[0]
	if nestedRemediation.Cooldown != 10*time.Minute {
		t.Errorf("Nested remediation cooldown = %v, want 10m", nestedRemediation.Cooldown)
	}
	if nestedRemediation.MaxAttempts != 1 {
		t.Errorf("Nested remediation max attempts = %v, want 1", nestedRemediation.MaxAttempts)
	}
}

// TestDetectCircularDependencies tests circular dependency detection in monitors
func TestDetectCircularDependencies(t *testing.T) {
	tests := []struct {
		name     string
		monitors []MonitorConfig
		wantErr  bool
		errMsg   string
	}{
		{
			name: "no dependencies",
			monitors: []MonitorConfig{
				{Name: "monitor-a"},
				{Name: "monitor-b"},
				{Name: "monitor-c"},
			},
			wantErr: false,
		},
		{
			name: "valid linear dependency chain",
			monitors: []MonitorConfig{
				{Name: "monitor-a", DependsOn: []string{"monitor-b"}},
				{Name: "monitor-b", DependsOn: []string{"monitor-c"}},
				{Name: "monitor-c"},
			},
			wantErr: false,
		},
		{
			name: "simple circular dependency (A -> B -> A)",
			monitors: []MonitorConfig{
				{Name: "monitor-a", DependsOn: []string{"monitor-b"}},
				{Name: "monitor-b", DependsOn: []string{"monitor-a"}},
			},
			wantErr: true,
			errMsg:  "circular dependency detected",
		},
		{
			name: "complex circular dependency (A -> B -> C -> A)",
			monitors: []MonitorConfig{
				{Name: "monitor-a", DependsOn: []string{"monitor-b"}},
				{Name: "monitor-b", DependsOn: []string{"monitor-c"}},
				{Name: "monitor-c", DependsOn: []string{"monitor-a"}},
			},
			wantErr: true,
			errMsg:  "circular dependency detected",
		},
		{
			name: "self-dependency",
			monitors: []MonitorConfig{
				{Name: "monitor-a", DependsOn: []string{"monitor-a"}},
			},
			wantErr: true,
			errMsg:  "circular dependency detected",
		},
		{
			name: "dependency on non-existent monitor",
			monitors: []MonitorConfig{
				{Name: "monitor-a", DependsOn: []string{"monitor-nonexistent"}},
			},
			wantErr: true,
			errMsg:  "depends on non-existent monitor",
		},
		{
			name: "complex valid dependency tree",
			monitors: []MonitorConfig{
				{Name: "monitor-a", DependsOn: []string{"monitor-b", "monitor-c"}},
				{Name: "monitor-b", DependsOn: []string{"monitor-d"}},
				{Name: "monitor-c", DependsOn: []string{"monitor-d"}},
				{Name: "monitor-d"},
			},
			wantErr: false,
		},
		{
			name: "cycle in complex tree (A -> B -> D, C -> D -> A)",
			monitors: []MonitorConfig{
				{Name: "monitor-a", DependsOn: []string{"monitor-b"}},
				{Name: "monitor-b", DependsOn: []string{"monitor-d"}},
				{Name: "monitor-c", DependsOn: []string{"monitor-d"}},
				{Name: "monitor-d", DependsOn: []string{"monitor-a"}},
			},
			wantErr: true,
			errMsg:  "circular dependency detected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := detectCircularDependencies(tt.monitors)
			if (err != nil) != tt.wantErr {
				t.Errorf("detectCircularDependencies() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("detectCircularDependencies() error = %v, want error containing %v", err, tt.errMsg)
				}
			}
		})
	}
}

// TestValidateMonitorThresholds tests threshold range validation (0-100)
func TestValidateMonitorThresholds(t *testing.T) {
	tests := []struct {
		name        string
		monitorName string
		config      map[string]interface{}
		wantErr     bool
		errMsg      string
	}{
		{
			name:        "valid thresholds",
			monitorName: "test-monitor",
			config: map[string]interface{}{
				"warningThreshold":  80.0,
				"criticalThreshold": 95.0,
			},
			wantErr: false,
		},
		{
			name:        "threshold at boundary (0)",
			monitorName: "test-monitor",
			config: map[string]interface{}{
				"warningThreshold": 0.0,
			},
			wantErr: false,
		},
		{
			name:        "threshold at boundary (100)",
			monitorName: "test-monitor",
			config: map[string]interface{}{
				"criticalThreshold": 100.0,
			},
			wantErr: false,
		},
		{
			name:        "threshold below range",
			monitorName: "test-monitor",
			config: map[string]interface{}{
				"warningThreshold": -10.0,
			},
			wantErr: true,
			errMsg:  "must be in range 0-100",
		},
		{
			name:        "threshold above range",
			monitorName: "test-monitor",
			config: map[string]interface{}{
				"criticalThreshold": 150.0,
			},
			wantErr: true,
			errMsg:  "must be in range 0-100",
		},
		{
			name:        "integer thresholds (valid)",
			monitorName: "test-monitor",
			config: map[string]interface{}{
				"warningThreshold":  80,
				"criticalThreshold": 95,
			},
			wantErr: false,
		},
		{
			name:        "integer threshold out of range",
			monitorName: "test-monitor",
			config: map[string]interface{}{
				"warningThreshold": 200,
			},
			wantErr: true,
			errMsg:  "must be in range 0-100",
		},
		{
			name:        "CPU load factor thresholds",
			monitorName: "cpu-monitor",
			config: map[string]interface{}{
				"warningLoadFactor":  0.7,
				"criticalLoadFactor": 0.9,
			},
			wantErr: false,
		},
		{
			name:        "disk inode thresholds",
			monitorName: "disk-monitor",
			config: map[string]interface{}{
				"inodeWarning":  80.0,
				"inodeCritical": 95.0,
			},
			wantErr: false,
		},
		{
			name:        "memory swap thresholds",
			monitorName: "memory-monitor",
			config: map[string]interface{}{
				"swapWarningThreshold":  50.0,
				"swapCriticalThreshold": 80.0,
			},
			wantErr: false,
		},
		{
			name:        "non-threshold fields ignored",
			monitorName: "test-monitor",
			config: map[string]interface{}{
				"somePath":    "/var/log",
				"someCount":   1000,
				"someBoolean": true,
			},
			wantErr: false,
		},
		{
			name:        "mixed valid and invalid",
			monitorName: "test-monitor",
			config: map[string]interface{}{
				"warningThreshold":  80.0,  // valid
				"criticalThreshold": 150.0, // invalid
			},
			wantErr: true,
			errMsg:  "must be in range 0-100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMonitorThresholds(tt.monitorName, tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateMonitorThresholds() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("validateMonitorThresholds() error = %v, want error containing %v", err, tt.errMsg)
				}
			}
		})
	}
}

// TestMonitorConfigValidation_IntervalMinimums tests minimum interval validation
func TestMonitorConfigValidation_IntervalMinimums(t *testing.T) {
	tests := []struct {
		name    string
		input   MonitorConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "interval meets minimum (1s)",
			input: MonitorConfig{
				Name:     "test-monitor",
				Type:     "test-type",
				Interval: 1 * time.Second,
				Timeout:  500 * time.Millisecond,
			},
			wantErr: false,
		},
		{
			name: "interval exceeds minimum",
			input: MonitorConfig{
				Name:     "test-monitor",
				Type:     "test-type",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "interval below minimum (500ms)",
			input: MonitorConfig{
				Name:     "test-monitor",
				Type:     "test-type",
				Interval: 500 * time.Millisecond,
				Timeout:  100 * time.Millisecond,
			},
			wantErr: true,
			errMsg:  "below minimum threshold",
		},
		{
			name: "interval below minimum (100ms)",
			input: MonitorConfig{
				Name:     "test-monitor",
				Type:     "test-type",
				Interval: 100 * time.Millisecond,
				Timeout:  50 * time.Millisecond,
			},
			wantErr: true,
			errMsg:  "below minimum threshold",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, want error containing %v", err, tt.errMsg)
				}
			}
		})
	}
}

// TestGlobalSettingsValidation_HeartbeatMinimum tests heartbeat interval minimum
func TestGlobalSettingsValidation_HeartbeatMinimum(t *testing.T) {
	tests := []struct {
		name    string
		input   GlobalSettings
		wantErr bool
		errMsg  string
	}{
		{
			name: "heartbeat meets minimum (5s)",
			input: GlobalSettings{
				NodeName:          "test-node",
				LogLevel:          "info",
				LogFormat:         "json",
				LogOutput:         "stdout",
				UpdateInterval:    10 * time.Second,
				ResyncInterval:    60 * time.Second,
				HeartbeatInterval: 5 * time.Second,
				QPS:               50,
				Burst:             100,
			},
			wantErr: false,
		},
		{
			name: "heartbeat exceeds minimum",
			input: GlobalSettings{
				NodeName:          "test-node",
				LogLevel:          "info",
				LogFormat:         "json",
				LogOutput:         "stdout",
				UpdateInterval:    10 * time.Second,
				ResyncInterval:    60 * time.Second,
				HeartbeatInterval: 5 * time.Minute,
				QPS:               50,
				Burst:             100,
			},
			wantErr: false,
		},
		{
			name: "heartbeat below minimum (3s)",
			input: GlobalSettings{
				NodeName:          "test-node",
				LogLevel:          "info",
				LogFormat:         "json",
				LogOutput:         "stdout",
				UpdateInterval:    10 * time.Second,
				ResyncInterval:    60 * time.Second,
				HeartbeatInterval: 3 * time.Second,
				QPS:               50,
				Burst:             100,
			},
			wantErr: true,
			errMsg:  "below minimum threshold",
		},
		{
			name: "heartbeat below minimum (1s)",
			input: GlobalSettings{
				NodeName:          "test-node",
				LogLevel:          "info",
				LogFormat:         "json",
				LogOutput:         "stdout",
				UpdateInterval:    10 * time.Second,
				ResyncInterval:    60 * time.Second,
				HeartbeatInterval: 1 * time.Second,
				QPS:               50,
				Burst:             100,
			},
			wantErr: true,
			errMsg:  "below minimum threshold",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, want error containing %v", err, tt.errMsg)
				}
			}
		})
	}
}

// TestMonitorRemediationConfigValidation_CooldownMinimum tests cooldown minimum
func TestMonitorRemediationConfigValidation_CooldownMinimum(t *testing.T) {
	tests := []struct {
		name    string
		input   MonitorRemediationConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "cooldown meets minimum (10s)",
			input: MonitorRemediationConfig{
				Enabled:     true,
				Strategy:    "systemd-restart",
				Service:     "kubelet",
				Cooldown:    10 * time.Second,
				MaxAttempts: 3,
			},
			wantErr: false,
		},
		{
			name: "cooldown exceeds minimum",
			input: MonitorRemediationConfig{
				Enabled:     true,
				Strategy:    "systemd-restart",
				Service:     "kubelet",
				Cooldown:    5 * time.Minute,
				MaxAttempts: 3,
			},
			wantErr: false,
		},
		{
			name: "cooldown below minimum (5s)",
			input: MonitorRemediationConfig{
				Enabled:     true,
				Strategy:    "systemd-restart",
				Service:     "kubelet",
				Cooldown:    5 * time.Second,
				MaxAttempts: 3,
			},
			wantErr: true,
			errMsg:  "below minimum threshold",
		},
		{
			name: "cooldown below minimum (1s)",
			input: MonitorRemediationConfig{
				Enabled:     true,
				Strategy:    "systemd-restart",
				Service:     "kubelet",
				Cooldown:    1 * time.Second,
				MaxAttempts: 3,
			},
			wantErr: true,
			errMsg:  "below minimum threshold",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, want error containing %v", err, tt.errMsg)
				}
			}
		})
	}
}

// mockMonitorRegistry is a mock implementation of MonitorRegistryValidator for testing
type mockMonitorRegistry struct {
	registeredTypes map[string]bool
}

func (m *mockMonitorRegistry) IsRegistered(monitorType string) bool {
	return m.registeredTypes[monitorType]
}

func (m *mockMonitorRegistry) GetRegisteredTypes() []string {
	types := make([]string, 0, len(m.registeredTypes))
	for t := range m.registeredTypes {
		types = append(types, t)
	}
	return types
}

// TestValidateWithRegistry tests configuration validation with monitor registry
func TestValidateWithRegistry(t *testing.T) {
	// Create a mock registry with some registered types
	mockRegistry := &mockMonitorRegistry{
		registeredTypes: map[string]bool{
			"system-cpu-check":    true,
			"system-memory-check": true,
			"system-disk-check":   true,
		},
	}

	tests := []struct {
		name     string
		config   NodeDoctorConfig
		registry MonitorRegistryValidator
		wantErr  bool
		errMsg   string
	}{
		{
			name: "valid config with registered monitor types",
			config: NodeDoctorConfig{
				APIVersion: "v1",
				Kind:       "NodeDoctorConfig",
				Metadata:   ConfigMetadata{Name: "test-config"},
				Settings: GlobalSettings{
					NodeName:          "test-node",
					LogLevel:          "info",
					LogFormat:         "json",
					LogOutput:         "stdout",
					UpdateInterval:    10 * time.Second,
					ResyncInterval:    60 * time.Second,
					HeartbeatInterval: 5 * time.Second,
					QPS:               50,
					Burst:             100,
				},
				Monitors: []MonitorConfig{
					{
						Name:     "cpu-monitor",
						Type:     "system-cpu-check",
						Interval: 30 * time.Second,
						Timeout:  10 * time.Second,
					},
				},
				Remediation: RemediationConfig{
					CooldownPeriod:           5 * time.Minute,
					MaxAttemptsGlobal:        3,
					MaxRemediationsPerHour:   10,
					MaxRemediationsPerMinute: 2,
					HistorySize:              100,
				},
			},
			registry: mockRegistry,
			wantErr:  false,
		},
		{
			name: "unregistered monitor type",
			config: NodeDoctorConfig{
				APIVersion: "v1",
				Kind:       "NodeDoctorConfig",
				Metadata:   ConfigMetadata{Name: "test-config"},
				Settings: GlobalSettings{
					NodeName:          "test-node",
					LogLevel:          "info",
					LogFormat:         "json",
					LogOutput:         "stdout",
					UpdateInterval:    10 * time.Second,
					ResyncInterval:    60 * time.Second,
					HeartbeatInterval: 5 * time.Second,
					QPS:               50,
					Burst:             100,
				},
				Monitors: []MonitorConfig{
					{
						Name:     "unknown-monitor",
						Type:     "unknown-type",
						Interval: 30 * time.Second,
						Timeout:  10 * time.Second,
					},
				},
				Remediation: RemediationConfig{
					CooldownPeriod:           5 * time.Minute,
					MaxAttemptsGlobal:        3,
					MaxRemediationsPerHour:   10,
					MaxRemediationsPerMinute: 2,
					HistorySize:              100,
				},
			},
			registry: mockRegistry,
			wantErr:  true,
			errMsg:   "unknown monitor type",
		},
		{
			name: "circular dependency detected",
			config: NodeDoctorConfig{
				APIVersion: "v1",
				Kind:       "NodeDoctorConfig",
				Metadata:   ConfigMetadata{Name: "test-config"},
				Settings: GlobalSettings{
					NodeName:          "test-node",
					LogLevel:          "info",
					LogFormat:         "json",
					LogOutput:         "stdout",
					UpdateInterval:    10 * time.Second,
					ResyncInterval:    60 * time.Second,
					HeartbeatInterval: 5 * time.Second,
					QPS:               50,
					Burst:             100,
				},
				Monitors: []MonitorConfig{
					{
						Name:      "cpu-monitor",
						Type:      "system-cpu-check",
						Interval:  30 * time.Second,
						Timeout:   10 * time.Second,
						DependsOn: []string{"memory-monitor"},
					},
					{
						Name:      "memory-monitor",
						Type:      "system-memory-check",
						Interval:  30 * time.Second,
						Timeout:   10 * time.Second,
						DependsOn: []string{"cpu-monitor"},
					},
				},
				Remediation: RemediationConfig{
					CooldownPeriod:           5 * time.Minute,
					MaxAttemptsGlobal:        3,
					MaxRemediationsPerHour:   10,
					MaxRemediationsPerMinute: 2,
					HistorySize:              100,
				},
			},
			registry: mockRegistry,
			wantErr:  true,
			errMsg:   "circular dependency",
		},
		{
			name: "nil registry still validates structure and dependencies",
			config: NodeDoctorConfig{
				APIVersion: "v1",
				Kind:       "NodeDoctorConfig",
				Metadata:   ConfigMetadata{Name: "test-config"},
				Settings: GlobalSettings{
					NodeName:          "test-node",
					LogLevel:          "info",
					LogFormat:         "json",
					LogOutput:         "stdout",
					UpdateInterval:    10 * time.Second,
					ResyncInterval:    60 * time.Second,
					HeartbeatInterval: 5 * time.Second,
					QPS:               50,
					Burst:             100,
				},
				Monitors: []MonitorConfig{
					{
						Name:     "test-monitor",
						Type:     "any-type",
						Interval: 30 * time.Second,
						Timeout:  10 * time.Second,
					},
				},
				Remediation: RemediationConfig{
					CooldownPeriod:           5 * time.Minute,
					MaxAttemptsGlobal:        3,
					MaxRemediationsPerHour:   10,
					MaxRemediationsPerMinute: 2,
					HistorySize:              100,
				},
			},
			registry: nil,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.ValidateWithRegistry(tt.registry)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateWithRegistry() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("ValidateWithRegistry() error = %v, want error containing %v", err, tt.errMsg)
				}
			}
		})
	}
}

// TestMonitorConfigValidation_WithThresholds tests threshold validation in MonitorConfig
func TestMonitorConfigValidation_WithThresholds(t *testing.T) {
	tests := []struct {
		name    string
		input   MonitorConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid monitor with valid thresholds",
			input: MonitorConfig{
				Name:     "cpu-monitor",
				Type:     "system-cpu-check",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Config: map[string]interface{}{
					"warningThreshold":  80.0,
					"criticalThreshold": 95.0,
				},
			},
			wantErr: false,
		},
		{
			name: "monitor with threshold out of range",
			input: MonitorConfig{
				Name:     "memory-monitor",
				Type:     "system-memory-check",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Config: map[string]interface{}{
					"warningThreshold":  150.0,
					"criticalThreshold": 95.0,
				},
			},
			wantErr: true,
			errMsg:  "must be in range 0-100",
		},
		{
			name: "monitor without threshold config",
			input: MonitorConfig{
				Name:     "test-monitor",
				Type:     "test-type",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Config:   map[string]interface{}{},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, want error containing %v", err, tt.errMsg)
				}
			}
		})
	}
}
