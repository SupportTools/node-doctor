package reload

import (
	"strings"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

func TestNewConfigValidator(t *testing.T) {
	validator := NewConfigValidator()
	if validator == nil {
		t.Fatal("NewConfigValidator returned nil")
	}

	if validator.maxMonitors != 100 {
		t.Errorf("Expected maxMonitors to be 100, got %d", validator.maxMonitors)
	}
	if validator.maxRemediatorsPerMonitor != 10 {
		t.Errorf("Expected maxRemediatorsPerMonitor to be 10, got %d", validator.maxRemediatorsPerMonitor)
	}
	if validator.maxWebhooksPerExporter != 20 {
		t.Errorf("Expected maxWebhooksPerExporter to be 20, got %d", validator.maxWebhooksPerExporter)
	}
	if validator.maxDependenciesPerMonitor != 10 {
		t.Errorf("Expected maxDependenciesPerMonitor to be 10, got %d", validator.maxDependenciesPerMonitor)
	}
}

func TestNewConfigValidatorWithLimits(t *testing.T) {
	validator := NewConfigValidatorWithLimits(50, 5, 15, 8)
	if validator.maxMonitors != 50 {
		t.Errorf("Expected maxMonitors to be 50, got %d", validator.maxMonitors)
	}
	if validator.maxRemediatorsPerMonitor != 5 {
		t.Errorf("Expected maxRemediatorsPerMonitor to be 5, got %d", validator.maxRemediatorsPerMonitor)
	}
	if validator.maxWebhooksPerExporter != 15 {
		t.Errorf("Expected maxWebhooksPerExporter to be 15, got %d", validator.maxWebhooksPerExporter)
	}
	if validator.maxDependenciesPerMonitor != 8 {
		t.Errorf("Expected maxDependenciesPerMonitor to be 8, got %d", validator.maxDependenciesPerMonitor)
	}
}

func TestValidate_NilConfig(t *testing.T) {
	validator := NewConfigValidator()
	result := validator.Validate(nil)

	if result.Valid {
		t.Error("Expected validation to fail for nil config")
	}

	if len(result.Errors) == 0 {
		t.Error("Expected validation errors for nil config")
	}

	hasConfigError := false
	for _, err := range result.Errors {
		if err.Field == "config" && err.Message == "configuration is nil" {
			hasConfigError = true
			break
		}
	}
	if !hasConfigError {
		t.Error("Expected 'configuration is nil' error")
	}
}

func TestValidate_ValidConfiguration(t *testing.T) {
	config := createValidConfig()
	validator := NewConfigValidator()
	result := validator.Validate(config)

	if !result.Valid {
		t.Errorf("Expected valid configuration to pass validation, errors: %v", result.Errors)
	}

	if len(result.Errors) != 0 {
		t.Errorf("Expected no validation errors, got %d: %v", len(result.Errors), result.Errors)
	}
}

func TestValidate_MissingRequiredFields(t *testing.T) {
	tests := []struct {
		name        string
		modifyFunc  func(*types.NodeDoctorConfig)
		expectedErr string
		expectedField string
	}{
		{
			name: "missing apiVersion",
			modifyFunc: func(config *types.NodeDoctorConfig) {
				config.APIVersion = ""
			},
			expectedErr:   "apiVersion is required",
			expectedField: "apiVersion",
		},
		{
			name: "missing kind",
			modifyFunc: func(config *types.NodeDoctorConfig) {
				config.Kind = ""
			},
			expectedErr:   "kind is required",
			expectedField: "kind",
		},
		{
			name: "invalid kind",
			modifyFunc: func(config *types.NodeDoctorConfig) {
				config.Kind = "InvalidKind"
			},
			expectedErr:   "kind must be 'NodeDoctorConfig', got \"InvalidKind\"",
			expectedField: "kind",
		},
		{
			name: "missing metadata name",
			modifyFunc: func(config *types.NodeDoctorConfig) {
				config.Metadata.Name = ""
			},
			expectedErr:   "metadata.name is required",
			expectedField: "metadata.name",
		},
		{
			name: "missing node name",
			modifyFunc: func(config *types.NodeDoctorConfig) {
				config.Settings.NodeName = ""
			},
			expectedErr:   "settings.nodeName is required",
			expectedField: "settings.nodeName",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := createValidConfig()
			tt.modifyFunc(config)

			validator := NewConfigValidator()
			result := validator.Validate(config)

			if result.Valid {
				t.Error("Expected validation to fail")
			}

			hasExpectedError := false
			for _, err := range result.Errors {
				if err.Field == tt.expectedField && err.Message == tt.expectedErr {
					hasExpectedError = true
					break
				}
			}
			if !hasExpectedError {
				t.Errorf("Expected error field=%s message=%s, got errors: %v", tt.expectedField, tt.expectedErr, result.Errors)
			}
		})
	}
}

func TestValidate_DuplicateMonitorNames(t *testing.T) {
	config := createValidConfig()

	// Add monitor with duplicate name (case-insensitive)
	config.Monitors = append(config.Monitors, types.MonitorConfig{
		Name:    "Test-Monitor", // Different case than "test-monitor"
		Type:    "disk-check",
		Enabled: true,
		IntervalString: "30s",
		TimeoutString:  "10s",
	})

	// Apply defaults to parse duration strings
	for i := range config.Monitors {
		config.Monitors[i].Interval, _ = time.ParseDuration(config.Monitors[i].IntervalString)
		config.Monitors[i].Timeout, _ = time.ParseDuration(config.Monitors[i].TimeoutString)
	}

	validator := NewConfigValidator()
	result := validator.Validate(config)

	if result.Valid {
		t.Error("Expected validation to fail for duplicate monitor names")
	}

	hasDuplicateError := false
	for _, err := range result.Errors {
		if err.Field == "monitors" && strings.Contains(err.Message, "duplicate monitor names found") {
			hasDuplicateError = true
			break
		}
	}
	if !hasDuplicateError {
		t.Errorf("Expected duplicate monitor names error, got errors: %v", result.Errors)
	}
}

func TestValidate_InvalidMonitorInterval(t *testing.T) {
	tests := []struct {
		name        string
		interval    time.Duration
		expectedErr string
	}{
		{
			name:        "zero interval",
			interval:    0,
			expectedErr: "interval must be positive, got 0s",
		},
		{
			name:        "negative interval",
			interval:    -5 * time.Second,
			expectedErr: "interval must be positive, got -5s",
		},
		{
			name:        "below minimum threshold",
			interval:    500 * time.Millisecond,
			expectedErr: "interval 500ms is below minimum threshold of 1s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := createValidConfig()
			config.Monitors[0].Interval = tt.interval

			validator := NewConfigValidator()
			result := validator.Validate(config)

			if result.Valid {
				t.Error("Expected validation to fail for invalid interval")
			}

			hasExpectedError := false
			for _, err := range result.Errors {
				if err.Field == "monitors[0].interval" && err.Message == tt.expectedErr {
					hasExpectedError = true
					break
				}
			}
			if !hasExpectedError {
				t.Errorf("Expected error message %s, got errors: %v", tt.expectedErr, result.Errors)
			}
		})
	}
}

func TestValidate_InvalidMonitorTimeout(t *testing.T) {
	config := createValidConfig()
	config.Monitors[0].Timeout = 35 * time.Second // Greater than interval (30s)

	validator := NewConfigValidator()
	result := validator.Validate(config)

	if result.Valid {
		t.Error("Expected validation to fail for timeout >= interval")
	}

	hasTimeoutError := false
	for _, err := range result.Errors {
		if err.Field == "monitors[0].timeout" && err.Message == "timeout (35s) must be less than interval (30s)" {
			hasTimeoutError = true
			break
		}
	}
	if !hasTimeoutError {
		t.Errorf("Expected timeout validation error, got errors: %v", result.Errors)
	}
}

func TestValidate_InvalidExporterConfig(t *testing.T) {
	config := createValidConfig()

	// Disable all exporters
	config.Exporters.Kubernetes.Enabled = false
	config.Exporters.HTTP.Enabled = false
	config.Exporters.Prometheus.Enabled = false

	validator := NewConfigValidator()
	result := validator.Validate(config)

	if result.Valid {
		t.Error("Expected validation to fail when no exporters are enabled")
	}

	hasExporterError := false
	for _, err := range result.Errors {
		if err.Field == "exporters" && err.Message == "at least one exporter must be enabled" {
			hasExporterError = true
			break
		}
	}
	if !hasExporterError {
		t.Errorf("Expected 'at least one exporter must be enabled' error, got errors: %v", result.Errors)
	}
}

func TestValidate_InvalidRemediatorConfig(t *testing.T) {
	config := createValidConfig()

	// Add invalid remediation config
	config.Monitors[0].Remediation = &types.MonitorRemediationConfig{
		Enabled:        true,
		Strategy:       "invalid-strategy",
		MaxAttempts:    0,
		CooldownString: "5s",
		Cooldown:       5 * time.Second,
	}

	validator := NewConfigValidator()
	result := validator.Validate(config)

	if result.Valid {
		t.Error("Expected validation to fail for invalid remediation strategy")
	}

	hasStrategyError := false
	for _, err := range result.Errors {
		if err.Field == "monitors[0].remediation.strategy" &&
		   err.Message == "invalid strategy \"invalid-strategy\", must be one of: systemd-restart, custom-script, node-reboot, pod-delete" {
			hasStrategyError = true
			break
		}
	}
	if !hasStrategyError {
		t.Errorf("Expected invalid strategy error, got errors: %v", result.Errors)
	}
}

func TestValidate_MaxMonitorsExceeded(t *testing.T) {
	validator := NewConfigValidatorWithLimits(2, 10, 20, 10) // Set max monitors to 2

	config := createValidConfig()

	// Add more monitors than allowed
	config.Monitors = append(config.Monitors,
		types.MonitorConfig{
			Name:           "extra-monitor-1",
			Type:           "disk-check",
			Enabled:        true,
			Interval:       30 * time.Second,
			Timeout:        10 * time.Second,
			IntervalString: "30s",
			TimeoutString:  "10s",
		},
		types.MonitorConfig{
			Name:           "extra-monitor-2",
			Type:           "disk-check",
			Enabled:        true,
			Interval:       30 * time.Second,
			Timeout:        10 * time.Second,
			IntervalString: "30s",
			TimeoutString:  "10s",
		})

	result := validator.Validate(config)

	if result.Valid {
		t.Error("Expected validation to fail when max monitors exceeded")
	}

	hasMaxMonitorsError := false
	for _, err := range result.Errors {
		if err.Field == "monitors" && err.Message == "too many monitors defined (3), maximum allowed is 2" {
			hasMaxMonitorsError = true
			break
		}
	}
	if !hasMaxMonitorsError {
		t.Errorf("Expected max monitors exceeded error, got errors: %v", result.Errors)
	}
}

func TestValidate_CircularDependencies(t *testing.T) {
	config := createValidConfig()

	// Create circular dependency: monitor1 -> monitor2 -> monitor1
	config.Monitors = []types.MonitorConfig{
		{
			Name:           "monitor1",
			Type:           "disk-check",
			Enabled:        true,
			Interval:       30 * time.Second,
			Timeout:        10 * time.Second,
			IntervalString: "30s",
			TimeoutString:  "10s",
			DependsOn:      []string{"monitor2"},
		},
		{
			Name:           "monitor2",
			Type:           "disk-check",
			Enabled:        true,
			Interval:       30 * time.Second,
			Timeout:        10 * time.Second,
			IntervalString: "30s",
			TimeoutString:  "10s",
			DependsOn:      []string{"monitor1"},
		},
	}

	validator := NewConfigValidator()
	result := validator.Validate(config)

	if result.Valid {
		t.Error("Expected validation to fail for circular dependencies")
	}

	hasCircularDependencyError := false
	for _, err := range result.Errors {
		if err.Field == "monitors" && strings.Contains(err.Message, "circular dependency detected") {
			hasCircularDependencyError = true
			break
		}
	}
	if !hasCircularDependencyError {
		t.Errorf("Expected circular dependency error, got errors: %v", result.Errors)
	}
}

func TestValidate_NonExistentDependency(t *testing.T) {
	config := createValidConfig()
	config.Monitors[0].DependsOn = []string{"non-existent-monitor"}

	validator := NewConfigValidator()
	result := validator.Validate(config)

	if result.Valid {
		t.Error("Expected validation to fail for non-existent dependency")
	}

	hasDependencyError := false
	for _, err := range result.Errors {
		if err.Field == "monitors[0].dependsOn[0]" && err.Message == "depends on non-existent monitor \"non-existent-monitor\"" {
			hasDependencyError = true
			break
		}
	}
	if !hasDependencyError {
		t.Errorf("Expected non-existent dependency error, got errors: %v", result.Errors)
	}
}

func TestValidate_SelfDependency(t *testing.T) {
	config := createValidConfig()
	config.Monitors[0].DependsOn = []string{config.Monitors[0].Name}

	validator := NewConfigValidator()
	result := validator.Validate(config)

	if result.Valid {
		t.Error("Expected validation to fail for self dependency")
	}

	hasSelfDependencyError := false
	for _, err := range result.Errors {
		if err.Field == "monitors[0].dependsOn[0]" && err.Message == "monitor cannot depend on itself" {
			hasSelfDependencyError = true
			break
		}
	}
	if !hasSelfDependencyError {
		t.Errorf("Expected self dependency error, got errors: %v", result.Errors)
	}
}

func TestValidate_InvalidThresholdValues(t *testing.T) {
	config := createValidConfig()

	// Add invalid threshold values
	config.Monitors[0].Config = map[string]interface{}{
		"warningThreshold":  150.0, // Invalid: > 100
		"criticalThreshold": -10.0, // Invalid: < 0
	}

	validator := NewConfigValidator()
	result := validator.Validate(config)

	if result.Valid {
		t.Error("Expected validation to fail for invalid threshold values")
	}

	hasWarningError := false
	hasCriticalError := false
	for _, err := range result.Errors {
		if err.Field == "monitors[0].config.warningThreshold" && err.Message == "warningThreshold value 150.00 must be in range 0-100" {
			hasWarningError = true
		}
		if err.Field == "monitors[0].config.criticalThreshold" && err.Message == "criticalThreshold value -10.00 must be in range 0-100" {
			hasCriticalError = true
		}
	}
	if !hasWarningError {
		t.Error("Expected warning threshold error")
	}
	if !hasCriticalError {
		t.Error("Expected critical threshold error")
	}
}

func TestValidate_HTTPExporterWebhooks(t *testing.T) {
	config := createValidConfig()

	// Remove all webhooks
	config.Exporters.HTTP.Webhooks = []types.WebhookEndpoint{}

	validator := NewConfigValidator()
	result := validator.Validate(config)

	if result.Valid {
		t.Error("Expected validation to fail when HTTP exporter has no webhooks")
	}

	hasWebhookError := false
	for _, err := range result.Errors {
		if err.Field == "exporters.http.webhooks" && err.Message == "at least one webhook must be configured when HTTP exporter is enabled" {
			hasWebhookError = true
			break
		}
	}
	if !hasWebhookError {
		t.Errorf("Expected webhook configuration error, got errors: %v", result.Errors)
	}
}

func TestValidate_DuplicateWebhookNames(t *testing.T) {
	config := createValidConfig()

	// Add webhook with duplicate name
	config.Exporters.HTTP.Webhooks = append(config.Exporters.HTTP.Webhooks, types.WebhookEndpoint{
		Name:        "test-webhook", // Same as existing
		URL:         "https://example.com/webhook2",
		SendStatus:  true,
		SendProblems: true,
		Timeout:     30 * time.Second,
		Auth:        types.AuthConfig{Type: "none"},
	})

	validator := NewConfigValidator()
	result := validator.Validate(config)

	if result.Valid {
		t.Error("Expected validation to fail for duplicate webhook names")
	}

	hasDuplicateWebhookError := false
	for _, err := range result.Errors {
		if err.Field == "exporters.http.webhooks[1].name" && err.Message == "duplicate webhook name \"test-webhook\"" {
			hasDuplicateWebhookError = true
			break
		}
	}
	if !hasDuplicateWebhookError {
		t.Errorf("Expected duplicate webhook name error, got errors: %v", result.Errors)
	}
}

func TestValidate_InvalidPrometheusPort(t *testing.T) {
	config := createValidConfig()
	config.Exporters.Prometheus.Port = 70000 // Invalid port

	validator := NewConfigValidator()
	result := validator.Validate(config)

	if result.Valid {
		t.Error("Expected validation to fail for invalid Prometheus port")
	}

	hasPortError := false
	for _, err := range result.Errors {
		if err.Field == "exporters.prometheus.port" && err.Message == "port must be between 1 and 65535, got 70000" {
			hasPortError = true
			break
		}
	}
	if !hasPortError {
		t.Errorf("Expected invalid port error, got errors: %v", result.Errors)
	}
}

func TestValidate_RemediationConsistency(t *testing.T) {
	config := createValidConfig()

	// Enable global remediation but disable it on all monitors
	config.Remediation.Enabled = true
	for i := range config.Monitors {
		config.Monitors[i].Remediation = &types.MonitorRemediationConfig{
			Enabled: false, // Disabled
		}
	}

	validator := NewConfigValidator()
	result := validator.Validate(config)

	if result.Valid {
		t.Error("Expected validation to fail for inconsistent remediation configuration")
	}

	hasConsistencyError := false
	for _, err := range result.Errors {
		if err.Field == "remediation" && err.Message == "global remediation is enabled but no monitors have remediation configured" {
			hasConsistencyError = true
			break
		}
	}
	if !hasConsistencyError {
		t.Errorf("Expected remediation consistency error, got errors: %v", result.Errors)
	}
}

func TestValidateMonitorName(t *testing.T) {
	validator := NewConfigValidator()

	tests := []struct {
		name      string
		input     string
		shouldErr bool
		errMsg    string
	}{
		{
			name:  "valid name",
			input: "test-monitor",
			shouldErr: false,
		},
		{
			name:  "valid with numbers",
			input: "test-monitor-123",
			shouldErr: false,
		},
		{
			name:  "valid with dots",
			input: "test.monitor.name",
			shouldErr: false,
		},
		{
			name:      "empty name",
			input:     "",
			shouldErr: true,
			errMsg:    "monitor name cannot be empty",
		},
		{
			name:      "uppercase letters",
			input:     "Test-Monitor",
			shouldErr: true,
			errMsg:    "monitor name \"Test-Monitor\" is invalid",
		},
		{
			name:      "starts with dash",
			input:     "-test-monitor",
			shouldErr: true,
			errMsg:    "monitor name \"-test-monitor\" is invalid",
		},
		{
			name:      "ends with dash",
			input:     "test-monitor-",
			shouldErr: true,
			errMsg:    "monitor name \"test-monitor-\" is invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.validateMonitorName(tt.input)
			if tt.shouldErr {
				if err == nil {
					t.Errorf("Expected error for input %q", tt.input)
				} else if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error to contain %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for input %q: %v", tt.input, err)
				}
			}
		})
	}
}

func TestFormatValidationErrors(t *testing.T) {
	errors := []ValidationError{
		{Field: "field1", Message: "error message 1"},
		{Field: "field2", Message: "error message 2"},
	}

	result := FormatValidationErrors(errors)
	expected := "Configuration validation failed with 2 error(s):\n  - field1: error message 1\n  - field2: error message 2"

	if result != expected {
		t.Errorf("Expected:\n%s\nGot:\n%s", expected, result)
	}
}

func TestFormatValidationErrors_Empty(t *testing.T) {
	result := FormatValidationErrors([]ValidationError{})
	if result != "" {
		t.Errorf("Expected empty string for no errors, got %q", result)
	}
}

// Helper functions

func createValidConfig() *types.NodeDoctorConfig {
	config := &types.NodeDoctorConfig{
		APIVersion: "v1",
		Kind:       "NodeDoctorConfig",
		Metadata: types.ConfigMetadata{
			Name: "test-config",
		},
		Settings: types.GlobalSettings{
			NodeName:                "test-node",
			LogLevel:                "info",
			LogFormat:               "json",
			LogOutput:               "stdout",
			UpdateIntervalString:    "10s",
			ResyncIntervalString:    "60s",
			HeartbeatIntervalString: "5m",
			UpdateInterval:          10 * time.Second,
			ResyncInterval:          60 * time.Second,
			HeartbeatInterval:       5 * time.Minute,
			QPS:                     50,
			Burst:                   100,
		},
		Monitors: []types.MonitorConfig{
			{
				Name:           "test-monitor",
				Type:           "disk-check",
				Enabled:        true,
				Interval:       30 * time.Second,
				Timeout:        10 * time.Second,
				IntervalString: "30s",
				TimeoutString:  "10s",
				Config: map[string]interface{}{
					"path":               "/var",
					"warningThreshold":   80.0,
					"criticalThreshold":  90.0,
				},
			},
		},
		Exporters: types.ExporterConfigs{
			Kubernetes: &types.KubernetesExporterConfig{
				Enabled:                 true,
				UpdateIntervalString:    "10s",
				ResyncIntervalString:    "60s",
				HeartbeatIntervalString: "5m",
				UpdateInterval:          10 * time.Second,
				ResyncInterval:          60 * time.Second,
				HeartbeatInterval:       5 * time.Minute,
				Namespace:               "default",
			},
			HTTP: &types.HTTPExporterConfig{
				Enabled:       true,
				Workers:       5,
				QueueSize:     100,
				TimeoutString: "30s",
				Timeout:       30 * time.Second,
				Retry: types.RetryConfig{
					MaxAttempts:     3,
					BaseDelayString: "1s",
					BaseDelay:       1 * time.Second,
					MaxDelayString:  "30s",
					MaxDelay:        30 * time.Second,
				},
				Webhooks: []types.WebhookEndpoint{
					{
						Name:         "test-webhook",
						URL:          "https://example.com/webhook",
						SendStatus:   true,
						SendProblems: true,
						Timeout:      30 * time.Second,
						Auth:         types.AuthConfig{Type: "none"},
					},
				},
			},
			Prometheus: &types.PrometheusExporterConfig{
				Enabled:   true,
				Port:      9100,
				Path:      "/metrics",
				Namespace: "node_doctor",
			},
		},
		Remediation: types.RemediationConfig{
			Enabled:                  true,
			MaxRemediationsPerHour:   10,
			MaxRemediationsPerMinute: 2,
			CooldownPeriodString:     "5m",
			CooldownPeriod:           5 * time.Minute,
			MaxAttemptsGlobal:        3,
			HistorySize:              100,
			CircuitBreaker: types.CircuitBreakerConfig{
				Enabled:          true,
				Threshold:        5,
				TimeoutString:    "30m",
				Timeout:          30 * time.Minute,
				SuccessThreshold: 2,
			},
		},
	}

	return config
}