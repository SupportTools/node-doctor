package main

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// TestLoadConfiguration tests configuration loading with various scenarios
func TestLoadConfiguration(t *testing.T) {
	tests := []struct {
		name           string
		configContent  string
		configExists   bool
		expectError    bool
		expectedNode   string
		setEnv         map[string]string
		validateConfig func(*types.NodeDoctorConfig) error
	}{
		{
			name:         "default config when file doesn't exist",
			configExists: false,
			expectError:  false,
			expectedNode: "test-node-from-env",
			setEnv:       map[string]string{"NODE_NAME": "test-node-from-env"},
		},
		{
			name: "valid YAML config file",
			configContent: `apiVersion: node-doctor.io/v1alpha1
kind: NodeDoctorConfig
metadata:
  name: test-config
settings:
  nodeName: test-node
  logLevel: info
  logFormat: json
monitors:
  - name: test-monitor
    type: noop
    enabled: true
exporters:
  kubernetes:
    enabled: true
  http:
    enabled: true
  prometheus:
    enabled: true
remediation:
  enabled: false`,
			configExists: true,
			expectError:  false,
			expectedNode: "test-node",
		},
		{
			name: "valid JSON config file",
			configContent: `{
  "apiVersion": "node-doctor.io/v1alpha1",
  "kind": "NodeDoctorConfig",
  "metadata": {
    "name": "test-config"
  },
  "settings": {
    "nodeName": "json-test-node",
    "logLevel": "debug",
    "logFormat": "text"
  },
  "monitors": [
    {
      "name": "json-monitor",
      "type": "noop",
      "enabled": true
    }
  ],
  "exporters": {
    "kubernetes": {"enabled": true},
    "http": {"enabled": true},
    "prometheus": {"enabled": true}
  },
  "remediation": {
    "enabled": false
  }
}`,
			configExists: true,
			expectError:  false,
			expectedNode: "json-test-node",
		},
		{
			name: "invalid YAML config",
			configContent: `apiVersion: node-doctor.io/v1alpha1
kind: NodeDoctorConfig
metadata:
  name: ""  # Invalid: empty name
settings:
  nodeName: test-node`,
			configExists: true,
			expectError:  true,
		},
		{
			name: "missing required fields",
			configContent: `apiVersion: node-doctor.io/v1alpha1
kind: NodeDoctorConfig
# Missing metadata section`,
			configExists: true,
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup environment variables
			originalEnvVars := make(map[string]string)
			for key, value := range tt.setEnv {
				originalEnvVars[key] = os.Getenv(key)
				os.Setenv(key, value)
			}
			defer func() {
				for key, original := range originalEnvVars {
					if original == "" {
						os.Unsetenv(key)
					} else {
						os.Setenv(key, original)
					}
				}
			}()

			// Setup test environment
			tempDir := t.TempDir()
			originalConfigPath := *configPath
			testConfigPath := filepath.Join(tempDir, "config.yaml")
			*configPath = testConfigPath
			defer func() { *configPath = originalConfigPath }()

			// Create config file if needed
			if tt.configExists {
				if err := os.WriteFile(testConfigPath, []byte(tt.configContent), 0644); err != nil {
					t.Fatalf("Failed to write test config: %v", err)
				}
			}

			// Test configuration loading
			config, err := loadConfiguration()

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			// Validate results
			if config == nil {
				t.Errorf("Config is nil")
				return
			}

			if tt.expectedNode != "" && config.Settings.NodeName != tt.expectedNode {
				t.Errorf("Expected node name %q, got %q", tt.expectedNode, config.Settings.NodeName)
			}

			// Run custom validation if provided
			if tt.validateConfig != nil {
				if err := tt.validateConfig(config); err != nil {
					t.Errorf("Custom validation failed: %v", err)
				}
			}
		})
	}
}

// TestApplyFlagOverrides tests command-line flag overrides
func TestApplyFlagOverrides(t *testing.T) {
	tests := []struct {
		name            string
		originalConfig  *types.NodeDoctorConfig
		setFlags        func()
		expectedChanges map[string]interface{}
	}{
		{
			name: "node name override",
			originalConfig: &types.NodeDoctorConfig{
				Settings: types.GlobalSettings{
					NodeName:  "original-node",
					LogLevel:  "info",
					LogFormat: "json",
				},
			},
			setFlags: func() {
				*nodeName = "override-node"
				*logLevel = ""
				*logFormat = ""
				*kubeconfig = ""
				*dryRun = false
			},
			expectedChanges: map[string]interface{}{
				"nodeName": "override-node",
			},
		},
		{
			name: "multiple overrides",
			originalConfig: &types.NodeDoctorConfig{
				Settings: types.GlobalSettings{
					NodeName:   "original-node",
					LogLevel:   "info",
					LogFormat:  "json",
					Kubeconfig: "/original/path",
				},
				Remediation: types.RemediationConfig{
					DryRun: false,
				},
			},
			setFlags: func() {
				*nodeName = "new-node"
				*logLevel = "debug"
				*logFormat = "text"
				*kubeconfig = "/new/path"
				*dryRun = true
			},
			expectedChanges: map[string]interface{}{
				"nodeName":   "new-node",
				"logLevel":   "debug",
				"logFormat":  "text",
				"kubeconfig": "/new/path",
				"dryRunMode": true,
				"dryRunRem":  true,
			},
		},
		{
			name: "no overrides",
			originalConfig: &types.NodeDoctorConfig{
				Settings: types.GlobalSettings{
					NodeName:  "original-node",
					LogLevel:  "info",
					LogFormat: "json",
				},
			},
			setFlags: func() {
				*nodeName = ""
				*logLevel = ""
				*logFormat = ""
				*kubeconfig = ""
				*dryRun = false
			},
			expectedChanges: map[string]interface{}{
				"nodeName": "original-node", // Should remain unchanged
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set flags for this test
			tt.setFlags()

			// Apply overrides
			applyFlagOverrides(tt.originalConfig)

			// Verify changes
			for key, expected := range tt.expectedChanges {
				var actual interface{}
				switch key {
				case "nodeName":
					actual = tt.originalConfig.Settings.NodeName
				case "logLevel":
					actual = tt.originalConfig.Settings.LogLevel
				case "logFormat":
					actual = tt.originalConfig.Settings.LogFormat
				case "kubeconfig":
					actual = tt.originalConfig.Settings.Kubeconfig
				case "dryRunMode":
					actual = tt.originalConfig.Settings.DryRunMode
				case "dryRunRem":
					actual = tt.originalConfig.Remediation.DryRun
				default:
					t.Errorf("Unknown test key: %s", key)
					continue
				}

				if actual != expected {
					t.Errorf("Expected %s=%v, got %v", key, expected, actual)
				}
			}
		})
	}
}

// TestSetupLogging tests logging configuration
func TestSetupLogging(t *testing.T) {
	tests := []struct {
		name     string
		settings types.GlobalSettings
		validate func(t *testing.T)
	}{
		{
			name: "stdout json logging",
			settings: types.GlobalSettings{
				LogLevel:  "info",
				LogFormat: "json",
				LogOutput: "stdout",
			},
			validate: func(t *testing.T) {
				// Verify flags are set correctly for JSON format
				if log.Flags() != 0 {
					t.Errorf("Expected log flags to be 0 for JSON format, got %d", log.Flags())
				}
			},
		},
		{
			name: "stderr text logging",
			settings: types.GlobalSettings{
				LogLevel:  "debug",
				LogFormat: "text",
				LogOutput: "stderr",
			},
			validate: func(t *testing.T) {
				// Verify flags include timestamp and microseconds for text format
				expectedFlags := log.LstdFlags | log.Lmicroseconds
				if log.Flags() != expectedFlags {
					t.Errorf("Expected log flags %d for text format, got %d", expectedFlags, log.Flags())
				}
			},
		},
		{
			name: "file logging",
			settings: types.GlobalSettings{
				LogLevel:  "warn",
				LogFormat: "text",
				LogOutput: "file",
				LogFile:   filepath.Join(t.TempDir(), "test.log"),
			},
			validate: func(t *testing.T) {
				// Just verify setup doesn't crash - file output is harder to test
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture original log settings
			originalFlags := log.Flags()
			originalOutput := log.Writer()
			defer func() {
				log.SetFlags(originalFlags)
				log.SetOutput(originalOutput)
			}()

			// Setup logging
			setupLogging(tt.settings)

			// Run validation
			tt.validate(t)
		})
	}
}

// TestCreateMonitors tests monitor creation
func TestCreateMonitors(t *testing.T) {
	tests := []struct {
		name           string
		monitorConfigs []types.MonitorConfig
		expectError    bool
		expectedCount  int
	}{
		{
			name: "valid noop monitor",
			monitorConfigs: []types.MonitorConfig{
				{
					Name:    "test-noop",
					Type:    "noop",
					Enabled: true,
					Config: map[string]interface{}{
						"testMessage": "Test monitor active",
					},
				},
			},
			expectError:   false,
			expectedCount: 1,
		},
		{
			name: "disabled monitor",
			monitorConfigs: []types.MonitorConfig{
				{
					Name:    "disabled-monitor",
					Type:    "noop",
					Enabled: false,
				},
			},
			expectError:   true, // No monitors created
			expectedCount: 0,
		},
		{
			name: "invalid monitor type",
			monitorConfigs: []types.MonitorConfig{
				{
					Name:    "invalid-monitor",
					Type:    "nonexistent-type",
					Enabled: true,
				},
			},
			expectError:   true,
			expectedCount: 0,
		},
		{
			name: "multiple monitors",
			monitorConfigs: []types.MonitorConfig{
				{
					Name:    "monitor1",
					Type:    "noop",
					Enabled: true,
				},
				{
					Name:    "monitor2",
					Type:    "noop",
					Enabled: true,
				},
			},
			expectError:   false,
			expectedCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Apply defaults to monitor configs
			for i := range tt.monitorConfigs {
				if err := tt.monitorConfigs[i].ApplyDefaults(); err != nil {
					t.Fatalf("Failed to apply defaults to monitor config %d: %v", i, err)
				}
			}

			config := &types.NodeDoctorConfig{
				Monitors: tt.monitorConfigs,
			}

			ctx := context.Background()
			monitors, err := createMonitors(ctx, config)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if len(monitors) != tt.expectedCount {
				t.Errorf("Expected %d monitors, got %d", tt.expectedCount, len(monitors))
			}
		})
	}
}

// TestCreateExporters tests exporter creation (stub implementation)
func TestCreateExporters(t *testing.T) {
	tests := []struct {
		name             string
		exporterConfigs  types.ExporterConfigs
		expectedCount    int
		expectKubernetes bool
		expectHTTP       bool
		expectPrometheus bool
	}{
		{
			name: "all exporters enabled",
			exporterConfigs: types.ExporterConfigs{
				Kubernetes: &types.KubernetesExporterConfig{Enabled: true},
				HTTP:       &types.HTTPExporterConfig{Enabled: true},
				Prometheus: &types.PrometheusExporterConfig{Enabled: true},
			},
			expectedCount:    1, // Only noop exporter for now
			expectKubernetes: true,
			expectHTTP:       true,
			expectPrometheus: true,
		},
		{
			name: "no exporters enabled",
			exporterConfigs: types.ExporterConfigs{
				Kubernetes: &types.KubernetesExporterConfig{Enabled: false},
				HTTP:       &types.HTTPExporterConfig{Enabled: false},
				Prometheus: &types.PrometheusExporterConfig{Enabled: false},
			},
			expectedCount: 1, // Still returns noop exporter
		},
		{
			name: "partial exporters enabled",
			exporterConfigs: types.ExporterConfigs{
				Kubernetes: &types.KubernetesExporterConfig{Enabled: true},
				HTTP:       &types.HTTPExporterConfig{Enabled: false},
				Prometheus: &types.PrometheusExporterConfig{Enabled: true},
			},
			expectedCount:    1, // Only noop exporter for now
			expectKubernetes: true,
			expectPrometheus: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &types.NodeDoctorConfig{
				Exporters: tt.exporterConfigs,
			}

			// Capture log output to verify logging behavior
			var logOutput strings.Builder
			originalOutput := log.Writer()
			log.SetOutput(&logOutput)
			defer log.SetOutput(originalOutput)

			exporters := createExporters(config)

			if len(exporters) != tt.expectedCount {
				t.Errorf("Expected %d exporters, got %d", tt.expectedCount, len(exporters))
			}

			// Verify log messages for what would be created
			logContent := logOutput.String()
			if tt.expectKubernetes && !strings.Contains(logContent, "Kubernetes exporter would be enabled") {
				t.Errorf("Expected Kubernetes exporter log message")
			}
			if tt.expectHTTP && !strings.Contains(logContent, "HTTP exporter would be enabled") {
				t.Errorf("Expected HTTP exporter log message")
			}
			if tt.expectPrometheus && !strings.Contains(logContent, "Prometheus exporter would be enabled") {
				t.Errorf("Expected Prometheus exporter log message")
			}
		})
	}
}

// TestNoopExporter tests the stub exporter implementation
func TestNoopExporter(t *testing.T) {
	exporter := &noopExporter{}
	ctx := context.Background()

	// Test ExportStatus
	status := &types.Status{
		Source:     "test-monitor",
		Events:     []types.Event{{Severity: types.EventInfo, Reason: "test", Message: "test event"}},
		Conditions: []types.Condition{{Type: "TestCondition", Status: types.ConditionTrue}},
		Timestamp:  time.Now(),
	}

	err := exporter.ExportStatus(ctx, status)
	if err != nil {
		t.Errorf("ExportStatus returned error: %v", err)
	}

	// Test ExportProblem
	problem := &types.Problem{
		Type:       "test-problem",
		Resource:   "test-resource",
		Severity:   types.ProblemWarning,
		Message:    "test problem message",
		DetectedAt: time.Now(),
		Metadata:   make(map[string]string),
	}

	err = exporter.ExportProblem(ctx, problem)
	if err != nil {
		t.Errorf("ExportProblem returned error: %v", err)
	}
}

// TestPrintVersion tests version information printing
func TestPrintVersion(t *testing.T) {
	// Capture stdout
	originalStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Call printVersion
	printVersion()

	// Restore stdout and get output
	w.Close()
	os.Stdout = originalStdout
	output, _ := io.ReadAll(r)
	outputStr := string(output)

	// Verify required fields are present
	if !strings.Contains(outputStr, "node-doctor") {
		t.Errorf("Version output missing program name")
	}
	if !strings.Contains(outputStr, "Git Commit:") {
		t.Errorf("Version output missing git commit")
	}
	if !strings.Contains(outputStr, "Built:") {
		t.Errorf("Version output missing build time")
	}
	if !strings.Contains(outputStr, "Go Version:") {
		t.Errorf("Version output missing Go version")
	}
	if !strings.Contains(outputStr, "OS/Arch:") {
		t.Errorf("Version output missing OS/Arch")
	}
}

// TestConfigurationPrecedence tests that CLI flags override configuration file values
func TestConfigurationPrecedence(t *testing.T) {
	// Create temporary config file
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "test-config.yaml")
	configContent := `apiVersion: node-doctor.io/v1alpha1
kind: NodeDoctorConfig
metadata:
  name: test-config
settings:
  nodeName: config-file-node
  logLevel: warn
  logFormat: json
monitors:
  - name: test-monitor
    type: noop
    enabled: true
exporters:
  kubernetes:
    enabled: true
remediation:
  enabled: false`

	if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	// Save original flag values
	originalConfigPath := *configPath
	originalNodeName := *nodeName
	originalLogLevel := *logLevel
	defer func() {
		*configPath = originalConfigPath
		*nodeName = originalNodeName
		*logLevel = originalLogLevel
	}()

	// Set test values
	*configPath = configFile
	*nodeName = "cli-override-node"
	*logLevel = "debug"

	// Load configuration
	config, err := loadConfiguration()
	if err != nil {
		t.Fatalf("Failed to load configuration: %v", err)
	}

	// Verify CLI overrides took effect
	if config.Settings.NodeName != "cli-override-node" {
		t.Errorf("Expected node name to be overridden to 'cli-override-node', got %q", config.Settings.NodeName)
	}
	if config.Settings.LogLevel != "debug" {
		t.Errorf("Expected log level to be overridden to 'debug', got %q", config.Settings.LogLevel)
	}
	// LogFormat should remain from config file since no CLI override
	if config.Settings.LogFormat != "json" {
		t.Errorf("Expected log format to remain 'json' from config file, got %q", config.Settings.LogFormat)
	}
}

// TestGracefulShutdown is a basic test for shutdown behavior
// Note: Full integration testing would require more complex setup
func TestGracefulShutdown(t *testing.T) {
	// This is a simplified test - a full integration test would require
	// setting up the entire pipeline and testing signal handling

	// Test that the noopExporter handles context cancellation gracefully
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	exporter := &noopExporter{}

	// These should complete quickly even with cancelled context
	status := &types.Status{Source: "test", Timestamp: time.Now()}
	problem := &types.Problem{Type: "test", Resource: "test", Severity: types.ProblemInfo, Message: "test", DetectedAt: time.Now()}

	if err := exporter.ExportStatus(ctx, status); err != nil {
		t.Errorf("ExportStatus failed with cancelled context: %v", err)
	}

	if err := exporter.ExportProblem(ctx, problem); err != nil {
		t.Errorf("ExportProblem failed with cancelled context: %v", err)
	}
}
