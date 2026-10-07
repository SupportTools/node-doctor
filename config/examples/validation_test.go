package examples_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

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

	// Verify hot reload is enabled in default config. Reload.Enabled is a *bool
	// so that "absent" (nil, meaning enabled) is distinguishable from an explicit
	// false; IsEnabled() encodes that default.
	if !config.Reload.IsEnabled() {
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

// These factories build an in-cluster Kubernetes client, which unit tests cannot provide.
var needsClusterAtConstruction = map[string]bool{
	"kubernetes-apiserver-check": true,
	"kubernetes-capacity-check":  true,
	"network-cni-check":          true,
	"network-cluster-dns-pod":    true,
}

func setSubstitutionEnv(t *testing.T) {
	t.Helper()
	for k, v := range map[string]string{
		"NODE_NAME":           "test-node",
		"VERSION":             "v0.1.0",
		"CLUSTER_NAME":        "test-cluster",
		"PAGERDUTY_TOKEN":     "test-token",
		"SLACK_WEBHOOK_PATH":  "test/path",
		"MONITORING_USER":     "test-user",
		"MONITORING_PASSWORD": "test-password",
	} {
		t.Setenv(k, v)
	}
}

// assertConfigLoadsCleanly mirrors the agent's startup path: load, add default
// monitors, run registry-aware validation, then construct every enabled monitor.
// Any "unknown config keys" warning along the way fails the test.
func assertConfigLoadsCleanly(t *testing.T, path string) {
	t.Helper()

	config, err := util.LoadConfig(path)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	monitors.ApplyDefaultMonitors(config)
	if err := config.ApplyDefaults(); err != nil {
		t.Fatalf("apply defaults: %v", err)
	}

	var logs bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(prev)

	if err := config.ValidateWithRegistry(monitors.DefaultRegistry); err != nil {
		t.Fatalf("validate %s: %v", path, err)
	}
	for _, m := range config.Monitors {
		if !m.Enabled || needsClusterAtConstruction[m.Type] {
			continue
		}
		if _, err := monitors.CreateMonitor(context.Background(), m); err != nil {
			t.Errorf("create monitor %q (%s): %v", m.Name, m.Type, err)
		}
	}
	if strings.Contains(logs.String(), "unknown config keys") {
		t.Errorf("%s uses keys no monitor reads:\n%s", path, logs.String())
	}
}

func TestShippedConfigsUseKnownKeys(t *testing.T) {
	setSubstitutionEnv(t)

	var files []string
	for _, pattern := range []string{"../*.yaml", "*.yaml"} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, matches...)
	}
	if len(files) == 0 {
		t.Fatal("no config files found")
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) { assertConfigLoadsCleanly(t, f) })
	}
}

func TestHelmChartConfigUsesKnownKeys(t *testing.T) {
	helm, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("helm not on PATH")
	}
	setSubstitutionEnv(t)

	out, err := exec.Command(helm, "template", "node-doctor", "../../helm/node-doctor").Output()
	if err != nil {
		t.Fatalf("helm template: %v", err)
	}
	path := filepath.Join(t.TempDir(), "helm-config.yaml")
	if err := os.WriteFile(path, []byte(agentConfigFromManifests(t, out)), 0o644); err != nil {
		t.Fatal(err)
	}
	assertConfigLoadsCleanly(t, path)
}

func agentConfigFromManifests(t *testing.T, manifests []byte) string {
	t.Helper()
	dec := yaml.NewDecoder(bytes.NewReader(manifests))
	for {
		var doc struct {
			Kind string            `yaml:"kind"`
			Data map[string]string `yaml:"data"`
		}
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("decode helm output: %v", err)
		}
		if doc.Kind == "ConfigMap" {
			if cfg, ok := doc.Data["config.yaml"]; ok {
				return cfg
			}
		}
	}
	t.Fatal("no ConfigMap with config.yaml in helm output")
	return ""
}
