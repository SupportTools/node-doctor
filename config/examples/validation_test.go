package examples_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/supporttools/node-doctor/pkg/monitors"
	// Import monitor packages to register monitor types
	_ "github.com/supporttools/node-doctor/pkg/monitors/custom"
	_ "github.com/supporttools/node-doctor/pkg/monitors/kubernetes"
	_ "github.com/supporttools/node-doctor/pkg/monitors/network"
	_ "github.com/supporttools/node-doctor/pkg/monitors/system"
	"github.com/supporttools/node-doctor/pkg/util"
)

// TestExampleConfigs validates all example configuration files
// This ensures that:
// 1. All example configs can be loaded without errors
// 2. All configs pass validation
// 3. Default values are applied correctly
// 4. Environment variable substitution works
// 5. No circular dependencies exist
// 6. All monitor types are registered
func TestExampleConfigs(t *testing.T) {
	// Get the path to the examples directory
	examplesDir := "."

	// Set required environment variables for substitution
	os.Setenv("NODE_NAME", "test-node")
	os.Setenv("VERSION", "v0.1.0")
	os.Setenv("CLUSTER_NAME", "test-cluster")
	os.Setenv("PAGERDUTY_TOKEN", "test-token")
	os.Setenv("SLACK_WEBHOOK_PATH", "test/path")
	os.Setenv("MONITORING_USER", "test-user")
	os.Setenv("MONITORING_PASSWORD", "test-password")

	// Use the default monitor registry (where monitors are registered in init())
	registry := monitors.DefaultRegistry

	// Test cases for each example configuration
	testCases := []struct {
		name        string
		filename    string
		description string
	}{
		{
			name:        "Minimal",
			filename:    "minimal.yaml",
			description: "Bare minimum configuration",
		},
		{
			name:        "Development",
			filename:    "development.yaml",
			description: "Development/debugging configuration",
		},
		{
			name:        "Production",
			filename:    "production.yaml",
			description: "Full production configuration",
		},
		{
			name:        "CustomPlugins",
			filename:    "custom-plugins.yaml",
			description: "Custom plugins and advanced features",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Construct path to config file
			configPath := filepath.Join(examplesDir, tc.filename)

			// Load the configuration
			config, err := util.LoadConfig(configPath)
			if err != nil {
				t.Fatalf("Failed to load %s (%s): %v", tc.name, tc.description, err)
			}

			// Verify config is not nil
			if config == nil {
				t.Fatalf("Config is nil for %s", tc.name)
			}

			// Verify basic required fields
			if config.APIVersion == "" {
				t.Errorf("%s: apiVersion is empty", tc.name)
			}
			if config.Kind != "NodeDoctorConfig" {
				t.Errorf("%s: kind is %q, expected 'NodeDoctorConfig'", tc.name, config.Kind)
			}
			if config.Metadata.Name == "" {
				t.Errorf("%s: metadata.name is empty", tc.name)
			}

			// Verify node name was populated from environment
			if config.Settings.NodeName == "" {
				t.Errorf("%s: settings.nodeName is empty (env var not substituted?)", tc.name)
			}
			if config.Settings.NodeName == "${NODE_NAME}" {
				t.Errorf("%s: settings.nodeName was not substituted from environment variable", tc.name)
			}

			// Verify at least one monitor is configured
			if len(config.Monitors) == 0 {
				t.Errorf("%s: no monitors configured", tc.name)
			}

			// Verify all monitors have required fields
			monitorNames := make(map[string]bool)
			for i, monitor := range config.Monitors {
				if monitor.Name == "" {
					t.Errorf("%s: monitor %d has no name", tc.name, i)
				}
				if monitor.Type == "" {
					t.Errorf("%s: monitor %q has no type", tc.name, monitor.Name)
				}

				// Check for duplicate monitor names
				if monitorNames[monitor.Name] {
					t.Errorf("%s: duplicate monitor name %q", tc.name, monitor.Name)
				}
				monitorNames[monitor.Name] = true

				// Verify monitor type is registered
				if !registry.IsRegistered(monitor.Type) {
					t.Errorf("%s: monitor %q has unregistered type %q. Registered types: %v",
						tc.name, monitor.Name, monitor.Type, registry.GetRegisteredTypes())
				}

				// Verify intervals are set (defaults should have been applied)
				if monitor.Interval == 0 {
					t.Errorf("%s: monitor %q has zero interval", tc.name, monitor.Name)
				}
				if monitor.Timeout == 0 {
					t.Errorf("%s: monitor %q has zero timeout", tc.name, monitor.Name)
				}

				// Verify timeout < interval
				if monitor.Timeout >= monitor.Interval {
					t.Errorf("%s: monitor %q timeout (%v) >= interval (%v)",
						tc.name, monitor.Name, monitor.Timeout, monitor.Interval)
				}
			}

			// Verify at least one exporter is enabled
			hasEnabledExporter := false
			if config.Exporters.Kubernetes != nil && config.Exporters.Kubernetes.Enabled {
				hasEnabledExporter = true
			}
			if config.Exporters.HTTP != nil && config.Exporters.HTTP.Enabled {
				hasEnabledExporter = true
			}
			if config.Exporters.Prometheus != nil && config.Exporters.Prometheus.Enabled {
				hasEnabledExporter = true
			}
			if !hasEnabledExporter {
				t.Errorf("%s: no exporters enabled", tc.name)
			}

			// Verify Prometheus port if enabled
			if config.Exporters.Prometheus != nil && config.Exporters.Prometheus.Enabled {
				if config.Exporters.Prometheus.Port == 0 {
					t.Errorf("%s: Prometheus exporter enabled but port is 0", tc.name)
				}
				if config.Exporters.Prometheus.Port < 1 || config.Exporters.Prometheus.Port > 65535 {
					t.Errorf("%s: Prometheus port %d is out of valid range (1-65535)",
						tc.name, config.Exporters.Prometheus.Port)
				}
			}

			// Verify HTTP exporter webhook configuration if enabled
			if config.Exporters.HTTP != nil && config.Exporters.HTTP.Enabled {
				if len(config.Exporters.HTTP.Webhooks) == 0 {
					t.Errorf("%s: HTTP exporter enabled but no webhooks configured", tc.name)
				}
			}

			// Verify remediation configuration
			if config.Remediation.MaxRemediationsPerHour < 0 {
				t.Errorf("%s: negative maxRemediationsPerHour: %d",
					tc.name, config.Remediation.MaxRemediationsPerHour)
			}
			if config.Remediation.MaxRemediationsPerMinute < 0 {
				t.Errorf("%s: negative maxRemediationsPerMinute: %d",
					tc.name, config.Remediation.MaxRemediationsPerMinute)
			}
		})
	}
}

// TestDefaultConfig validates the default config/node-doctor.yaml
func TestDefaultConfig(t *testing.T) {
	// Set required environment variables
	os.Setenv("NODE_NAME", "test-node")
	os.Setenv("VERSION", "v0.1.0")

	// Load the default configuration
	configPath := "../node-doctor.yaml"
	config, err := util.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("Failed to load default config: %v", err)
	}

	// Verify config is not nil
	if config == nil {
		t.Fatal("Default config is nil")
	}

	// Verify API version and kind
	if config.APIVersion != "node-doctor.io/v1alpha1" {
		t.Errorf("Expected apiVersion 'node-doctor.io/v1alpha1', got %q", config.APIVersion)
	}
	if config.Kind != "NodeDoctorConfig" {
		t.Errorf("Expected kind 'NodeDoctorConfig', got %q", config.Kind)
	}

	// Verify comprehensive monitoring (should have system, kubernetes, and network monitors)
	hasSystemMonitor := false
	hasKubernetesMonitor := false
	hasNetworkMonitor := false

	for _, monitor := range config.Monitors {
		switch {
		case monitor.Type == "system-cpu" || monitor.Type == "system-memory" || monitor.Type == "system-disk":
			hasSystemMonitor = true
		case monitor.Type == "kubernetes-kubelet-check" || monitor.Type == "kubernetes-runtime-check":
			hasKubernetesMonitor = true
		case monitor.Type == "network-dns-check" || monitor.Type == "network-gateway-check":
			hasNetworkMonitor = true
		}
	}

	if !hasSystemMonitor {
		t.Error("Default config should have at least one system monitor")
	}
	if !hasKubernetesMonitor {
		t.Error("Default config should have at least one Kubernetes monitor")
	}
	if !hasNetworkMonitor {
		t.Error("Default config should have at least one network monitor")
	}

	// Verify hot reload is enabled in default config
	if !config.Reload.Enabled {
		t.Error("Hot reload should be enabled in default config")
	}
}

// TestConfigRoundTrip tests that configs can be saved and loaded
func TestConfigRoundTrip(t *testing.T) {
	// Set required environment variables
	os.Setenv("NODE_NAME", "test-node")
	os.Setenv("VERSION", "v0.1.0")

	testCases := []string{
		"minimal.yaml",
		"development.yaml",
	}

	for _, filename := range testCases {
		t.Run(filename, func(t *testing.T) {
			// Load original config
			configPath := filepath.Join(".", filename)
			original, err := util.LoadConfig(configPath)
			if err != nil {
				t.Fatalf("Failed to load config: %v", err)
			}

			// Create temp file for round-trip test
			tmpFile, err := os.CreateTemp("", "config-*.yaml")
			if err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}
			defer os.Remove(tmpFile.Name())
			tmpFile.Close()

			// Save config to temp file
			if err := util.SaveConfig(original, tmpFile.Name()); err != nil {
				t.Fatalf("Failed to save config: %v", err)
			}

			// Load config from temp file
			reloaded, err := util.LoadConfig(tmpFile.Name())
			if err != nil {
				t.Fatalf("Failed to reload config: %v", err)
			}

			// Verify basic fields match
			if original.APIVersion != reloaded.APIVersion {
				t.Errorf("APIVersion mismatch: %q != %q", original.APIVersion, reloaded.APIVersion)
			}
			if original.Kind != reloaded.Kind {
				t.Errorf("Kind mismatch: %q != %q", original.Kind, reloaded.Kind)
			}
			if original.Metadata.Name != reloaded.Metadata.Name {
				t.Errorf("Metadata.Name mismatch: %q != %q",
					original.Metadata.Name, reloaded.Metadata.Name)
			}
			if len(original.Monitors) != len(reloaded.Monitors) {
				t.Errorf("Monitor count mismatch: %d != %d",
					len(original.Monitors), len(reloaded.Monitors))
			}
		})
	}
}

// TestMonitorDependencies verifies that monitor dependencies are valid
func TestMonitorDependencies(t *testing.T) {
	// Test the custom-plugins example which has monitor dependencies
	os.Setenv("NODE_NAME", "test-node")

	configPath := filepath.Join(".", "custom-plugins.yaml")
	config, err := util.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("Failed to load custom-plugins config: %v", err)
	}

	// Build map of monitor names
	monitorExists := make(map[string]bool)
	for _, monitor := range config.Monitors {
		monitorExists[monitor.Name] = true
	}

	// Verify all dependencies reference existing monitors
	for _, monitor := range config.Monitors {
		for _, dep := range monitor.DependsOn {
			if !monitorExists[dep] {
				t.Errorf("Monitor %q depends on non-existent monitor %q",
					monitor.Name, dep)
			}
		}
	}
}

// TestRemediationStrategies verifies remediation configuration validity
func TestRemediationStrategies(t *testing.T) {
	validStrategies := map[string]bool{
		"systemd-restart": true,
		"custom-script":   true,
		"node-reboot":     true,
		"pod-delete":      true,
	}

	testConfigs := []string{
		"production.yaml",
		"custom-plugins.yaml",
	}

	for _, filename := range testConfigs {
		t.Run(filename, func(t *testing.T) {
			os.Setenv("NODE_NAME", "test-node")
			os.Setenv("PAGERDUTY_TOKEN", "test-token")
			os.Setenv("SLACK_WEBHOOK_PATH", "test/path")
			os.Setenv("MONITORING_USER", "test-user")
			os.Setenv("MONITORING_PASSWORD", "test-password")

			configPath := filepath.Join(".", filename)
			config, err := util.LoadConfig(configPath)
			if err != nil {
				t.Fatalf("Failed to load %s: %v", filename, err)
			}

			// Check each monitor's remediation strategy
			for _, monitor := range config.Monitors {
				if monitor.Remediation != nil && monitor.Remediation.Enabled {
					strategy := monitor.Remediation.Strategy
					if strategy != "" && !validStrategies[strategy] {
						t.Errorf("Monitor %q has invalid remediation strategy %q",
							monitor.Name, strategy)
					}

					// Verify strategy-specific requirements
					switch strategy {
					case "systemd-restart":
						if monitor.Remediation.Service == "" {
							t.Errorf("Monitor %q uses systemd-restart but service is empty",
								monitor.Name)
						}
					case "custom-script":
						if monitor.Remediation.ScriptPath == "" {
							t.Errorf("Monitor %q uses custom-script but scriptPath is empty",
								monitor.Name)
						}
					}

					// Verify cooldown is set
					if monitor.Remediation.Cooldown == 0 {
						t.Errorf("Monitor %q remediation has zero cooldown", monitor.Name)
					}

					// Verify max attempts is set
					if monitor.Remediation.MaxAttempts == 0 {
						t.Errorf("Monitor %q remediation has zero maxAttempts", monitor.Name)
					}
				}
			}
		})
	}
}
