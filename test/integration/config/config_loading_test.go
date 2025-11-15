package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
	"github.com/supporttools/node-doctor/pkg/util"
	"github.com/supporttools/node-doctor/test"
)

// TestLoadConfigYAML tests loading configuration from YAML files
func TestLoadConfigYAML(t *testing.T) {
	yamlConfig := `
apiVersion: node-doctor.io/v1alpha1
kind: NodeDoctorConfig
metadata:
  name: test-config
settings:
  nodeName: test-node
monitors:
  - name: kubelet-health
    type: kubernetes-kubelet-check
    enabled: true
    interval: 30s
    timeout: 10s
    config:
      healthzURL: http://127.0.0.1:10248/healthz
exporters:
  kubernetes:
    enabled: true
    updateInterval: 60s
  prometheus:
    enabled: true
    port: 9090
  http:
    enabled: false
remediation:
  enabled: true
  dryRun: false
`

	// Create temp file
	tmpDir := test.TempDir(t)
	configPath := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configPath, []byte(yamlConfig), 0644)
	test.AssertNoError(t, err, "Failed to write config file")

	// Load config
	config, err := util.LoadConfig(configPath)
	test.AssertNoError(t, err, "Failed to load YAML config")

	// Verify basic fields
	test.AssertEqual(t, "node-doctor.io/v1alpha1", config.APIVersion, "APIVersion mismatch")
	test.AssertEqual(t, "NodeDoctorConfig", config.Kind, "Kind mismatch")
	test.AssertEqual(t, "test-config", config.Metadata.Name, "Metadata.Name mismatch")
	test.AssertEqual(t, "test-node", config.Settings.NodeName, "Settings.NodeName mismatch")

	// Verify monitors
	test.AssertEqual(t, 1, len(config.Monitors), "Expected 1 monitor")
	test.AssertEqual(t, "kubelet-health", config.Monitors[0].Name, "Monitor name mismatch")
	test.AssertEqual(t, "kubernetes-kubelet-check", config.Monitors[0].Type, "Monitor type mismatch")
	test.AssertEqual(t, true, config.Monitors[0].Enabled, "Monitor should be enabled")
	test.AssertEqual(t, 30*time.Second, config.Monitors[0].Interval, "Monitor interval mismatch")
	test.AssertEqual(t, 10*time.Second, config.Monitors[0].Timeout, "Monitor timeout mismatch")

	// Verify exporters
	test.AssertTrue(t, config.Exporters.Kubernetes.Enabled, "Kubernetes exporter should be enabled")
	test.AssertTrue(t, config.Exporters.Prometheus.Enabled, "Prometheus exporter should be enabled")
	test.AssertFalse(t, config.Exporters.HTTP.Enabled, "HTTP exporter should be disabled")

	// Verify remediation
	test.AssertTrue(t, config.Remediation.Enabled, "Remediation should be enabled")
	test.AssertFalse(t, config.Remediation.DryRun, "DryRun should be false")
}

// TestLoadConfigJSON tests loading configuration from JSON files
func TestLoadConfigJSON(t *testing.T) {
	jsonConfig := `{
  "apiVersion": "node-doctor.io/v1alpha1",
  "kind": "NodeDoctorConfig",
  "metadata": {
    "name": "json-test-config"
  },
  "settings": {
    "nodeName": "json-test-node"
  },
  "monitors": [
    {
      "name": "memory-monitor",
      "type": "system-memory-check",
      "enabled": true,
      "interval": "60s",
      "timeout": "15s",
      "config": {
        "thresholdPercent": 90
      }
    }
  ],
  "exporters": {
    "kubernetes": {
      "enabled": true
    },
    "prometheus": {
      "enabled": true,
      "port": 9091
    },
    "http": {
      "enabled": false
    }
  },
  "remediation": {
    "enabled": false
  }
}`

	tmpDir := test.TempDir(t)
	configPath := filepath.Join(tmpDir, "config.json")
	err := os.WriteFile(configPath, []byte(jsonConfig), 0644)
	test.AssertNoError(t, err, "Failed to write JSON config file")

	// Load config
	config, err := util.LoadConfig(configPath)
	test.AssertNoError(t, err, "Failed to load JSON config")

	// Verify fields
	test.AssertEqual(t, "json-test-config", config.Metadata.Name, "Metadata.Name mismatch")
	test.AssertEqual(t, "json-test-node", config.Settings.NodeName, "Settings.NodeName mismatch")
	test.AssertEqual(t, 1, len(config.Monitors), "Expected 1 monitor")
	test.AssertEqual(t, "memory-monitor", config.Monitors[0].Name, "Monitor name mismatch")
	test.AssertEqual(t, "system-memory-check", config.Monitors[0].Type, "Monitor type mismatch")
	test.AssertFalse(t, config.Remediation.Enabled, "Remediation should be disabled")
}

// TestEnvironmentVariableSubstitution tests env var substitution in config
func TestEnvironmentVariableSubstitution(t *testing.T) {
	// Set test environment variables
	os.Setenv("TEST_NODE_NAME", "env-test-node")
	os.Setenv("TEST_PORT", "9999")
	defer os.Unsetenv("TEST_NODE_NAME")
	defer os.Unsetenv("TEST_PORT")

	yamlConfig := `
apiVersion: node-doctor.io/v1alpha1
kind: NodeDoctorConfig
metadata:
  name: env-test
settings:
  nodeName: ${TEST_NODE_NAME}
monitors:
  - name: test-monitor
    type: system-cpu-check
    enabled: true
exporters:
  prometheus:
    enabled: true
    port: ${TEST_PORT}
remediation:
  enabled: false
`

	tmpDir := test.TempDir(t)
	configPath := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configPath, []byte(yamlConfig), 0644)
	test.AssertNoError(t, err, "Failed to write config file")

	// Load config
	config, err := util.LoadConfig(configPath)
	test.AssertNoError(t, err, "Failed to load config with env vars")

	// Verify environment variables were substituted
	test.AssertEqual(t, "env-test-node", config.Settings.NodeName, "NodeName env var not substituted")
	test.AssertEqual(t, 9999, config.Exporters.Prometheus.Port, "Port env var not substituted")
}

// TestConfigValidation tests configuration validation
func TestConfigValidation(t *testing.T) {
	// Test 1: Valid config should pass validation
	validConfig := `
apiVersion: node-doctor.io/v1alpha1
kind: NodeDoctorConfig
metadata:
  name: valid-config
settings:
  nodeName: test-node
monitors:
  - name: test-monitor
    type: system-cpu-check
    enabled: true
    interval: 30s
    timeout: 10s
exporters:
  kubernetes:
    enabled: true
remediation:
  enabled: false
`

	tmpDir := test.TempDir(t)
	validPath := filepath.Join(tmpDir, "valid.yaml")
	err := os.WriteFile(validPath, []byte(validConfig), 0644)
	test.AssertNoError(t, err)

	_, err = util.LoadConfig(validPath)
	test.AssertNoError(t, err, "Valid config should pass validation")

	// Test 2: Config with missing required fields should fail
	invalidConfig := `
apiVersion: node-doctor.io/v1alpha1
kind: NodeDoctorConfig
metadata:
  name: invalid-config
monitors:
  - name: test-monitor
    type: system-cpu-check
    enabled: true
    interval: 0s
    timeout: 10s
`

	invalidPath := filepath.Join(tmpDir, "invalid.yaml")
	err = os.WriteFile(invalidPath, []byte(invalidConfig), 0644)
	test.AssertNoError(t, err)

	_, err = util.LoadConfig(invalidPath)
	test.AssertError(t, err, "Invalid config should fail validation")

	// Test 3: Config with timeout >= interval should fail
	invalidTimeoutConfig := `
apiVersion: node-doctor.io/v1alpha1
kind: NodeDoctorConfig
metadata:
  name: invalid-timeout
settings:
  nodeName: test-node
monitors:
  - name: test-monitor
    type: system-cpu-check
    enabled: true
    interval: 30s
    timeout: 35s
exporters:
  kubernetes:
    enabled: true
`

	invalidTimeoutPath := filepath.Join(tmpDir, "invalid-timeout.yaml")
	err = os.WriteFile(invalidTimeoutPath, []byte(invalidTimeoutConfig), 0644)
	test.AssertNoError(t, err)

	_, err = util.LoadConfig(invalidTimeoutPath)
	test.AssertError(t, err, "Config with timeout >= interval should fail")
}

// TestDefaultConfig tests the default configuration
func TestDefaultConfig(t *testing.T) {
	config, err := util.DefaultConfig()
	test.AssertNoError(t, err, "Failed to create default config")

	// Verify default values
	test.AssertEqual(t, "node-doctor.io/v1alpha1", config.APIVersion, "Default APIVersion")
	test.AssertEqual(t, "NodeDoctorConfig", config.Kind, "Default Kind")
	test.AssertEqual(t, "default", config.Metadata.Name, "Default metadata name")

	// Verify default monitors
	test.AssertTrue(t, len(config.Monitors) > 0, "Default config should have monitors")

	// Verify default exporters
	test.AssertTrue(t, config.Exporters.Kubernetes.Enabled, "Kubernetes exporter should be enabled by default")
	test.AssertTrue(t, config.Exporters.Prometheus.Enabled, "Prometheus exporter should be enabled by default")
	test.AssertFalse(t, config.Exporters.HTTP.Enabled, "HTTP exporter should be disabled by default")

	// Verify remediation is disabled by default
	test.AssertFalse(t, config.Remediation.Enabled, "Remediation should be disabled by default")
}

// TestLoadConfigOrDefault tests fallback to default config
func TestLoadConfigOrDefault(t *testing.T) {
	tmpDir := test.TempDir(t)
	nonExistentPath := filepath.Join(tmpDir, "does-not-exist.yaml")

	// Should return default config when file doesn't exist
	config, err := util.LoadConfigOrDefault(nonExistentPath)
	test.AssertNoError(t, err, "LoadConfigOrDefault should succeed with default config")
	test.AssertEqual(t, "default", config.Metadata.Name, "Should load default config")
}

// TestSaveAndLoadConfig tests round-trip config saving and loading
func TestSaveAndLoadConfig(t *testing.T) {
	// Create a test config
	originalConfig := &types.NodeDoctorConfig{
		APIVersion: "node-doctor.io/v1alpha1",
		Kind:       "NodeDoctorConfig",
		Metadata: types.ConfigMetadata{
			Name: "save-test",
		},
		Settings: types.GlobalSettings{
			NodeName: "save-test-node",
		},
		Monitors: []types.MonitorConfig{
			{
				Name:     "test-monitor",
				Type:     "system-cpu-check",
				Enabled:  true,
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
			},
		},
		Exporters: types.ExporterConfigs{
			Kubernetes: &types.KubernetesExporterConfig{
				Enabled: true,
			},
			Prometheus: &types.PrometheusExporterConfig{
				Enabled: true,
				Port:    9090,
			},
			HTTP: &types.HTTPExporterConfig{
				Enabled: false,
			},
		},
		Remediation: types.RemediationConfig{
			Enabled: false,
		},
	}

	tmpDir := test.TempDir(t)

	// Test YAML save/load
	yamlPath := filepath.Join(tmpDir, "save-test.yaml")
	err := util.SaveConfig(originalConfig, yamlPath)
	test.AssertNoError(t, err, "Failed to save YAML config")

	loadedYAML, err := util.LoadConfig(yamlPath)
	test.AssertNoError(t, err, "Failed to load saved YAML config")
	test.AssertEqual(t, "save-test", loadedYAML.Metadata.Name, "YAML round-trip: name mismatch")
	test.AssertEqual(t, "save-test-node", loadedYAML.Settings.NodeName, "YAML round-trip: nodeName mismatch")

	// Test JSON save/load
	jsonPath := filepath.Join(tmpDir, "save-test.json")
	err = util.SaveConfig(originalConfig, jsonPath)
	test.AssertNoError(t, err, "Failed to save JSON config")

	loadedJSON, err := util.LoadConfig(jsonPath)
	test.AssertNoError(t, err, "Failed to load saved JSON config")
	test.AssertEqual(t, "save-test", loadedJSON.Metadata.Name, "JSON round-trip: name mismatch")
	test.AssertEqual(t, "save-test-node", loadedJSON.Settings.NodeName, "JSON round-trip: nodeName mismatch")
}

// TestComplexConfiguration tests a complex, realistic configuration
func TestComplexConfiguration(t *testing.T) {
	complexConfig := `
apiVersion: node-doctor.io/v1alpha1
kind: NodeDoctorConfig
metadata:
  name: complex-config
  labels:
    environment: production
    team: platform
settings:
  nodeName: prod-node-01
monitors:
  - name: kubelet-health
    type: kubernetes-kubelet-check
    enabled: true
    interval: 30s
    timeout: 10s
    config:
      healthzURL: http://127.0.0.1:10248/healthz
  - name: memory-pressure
    type: system-memory-check
    enabled: true
    interval: 60s
    timeout: 15s
    config:
      thresholdPercent: 90
  - name: disk-pressure
    type: system-disk-check
    enabled: true
    interval: 120s
    timeout: 20s
    config:
      paths:
        - /var
        - /var/lib/docker
      thresholdPercent: 85
exporters:
  kubernetes:
    enabled: true
    updateInterval: 60s
  prometheus:
    enabled: true
    port: 9090
    path: /metrics
  http:
    enabled: true
    workers: 3
    queueSize: 100
    timeout: 10s
    webhooks:
      - name: alertmanager
        url: http://alertmanager:9093/api/v1/alerts
        timeout: 5s
        sendStatus: false
        sendProblems: true
remediation:
  enabled: true
  dryRun: false
  maxRemediationsPerHour: 20
  circuitBreaker:
    threshold: 5
    timeout: 5m
    successThreshold: 2
`

	tmpDir := test.TempDir(t)
	configPath := filepath.Join(tmpDir, "complex.yaml")
	err := os.WriteFile(configPath, []byte(complexConfig), 0644)
	test.AssertNoError(t, err)

	// Load complex config
	config, err := util.LoadConfig(configPath)
	test.AssertNoError(t, err, "Failed to load complex config")

	// Verify structure
	test.AssertEqual(t, "complex-config", config.Metadata.Name, "Config name")
	test.AssertEqual(t, 3, len(config.Monitors), "Expected 3 monitors")
	test.AssertTrue(t, config.Exporters.HTTP.Enabled, "HTTP exporter should be enabled")
	test.AssertEqual(t, 3, config.Exporters.HTTP.Workers, "HTTP workers")
	test.AssertEqual(t, 1, len(config.Exporters.HTTP.Webhooks), "Expected 1 webhook")
	test.AssertTrue(t, config.Remediation.Enabled, "Remediation should be enabled")

	// Verify circuit breaker config
	test.AssertEqual(t, 5, config.Remediation.CircuitBreaker.Threshold, "Circuit breaker threshold")
	test.AssertEqual(t, 2, config.Remediation.CircuitBreaker.SuccessThreshold, "Circuit breaker success threshold")

	// Verify rate limit
	test.AssertEqual(t, 20, config.Remediation.MaxRemediationsPerHour, "Rate limit maxPerHour")
}
