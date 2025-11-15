// Package util provides utility functions for Node Doctor.
package util

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/supporttools/node-doctor/pkg/types"
	"gopkg.in/yaml.v3"
)

// LoadConfig loads configuration from a file (YAML or JSON).
// The file format is determined by extension (.yaml, .yml, .json).
// Environment variables are substituted, defaults are applied, and validation is performed.
func LoadConfig(path string) (*types.NodeDoctorConfig, error) {
	// Read file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	// Substitute environment variables in raw data BEFORE parsing
	// This allows env vars to work in non-string fields (e.g., port: ${PORT})
	data = []byte(os.ExpandEnv(string(data)))

	// Determine format by extension
	ext := filepath.Ext(path)

	var config types.NodeDoctorConfig

	switch ext {
	case ".yaml", ".yml":
		err = yaml.Unmarshal(data, &config)
	case ".json":
		err = json.Unmarshal(data, &config)
	default:
		// Try YAML first, then JSON
		err = yaml.Unmarshal(data, &config)
		if err != nil {
			err = json.Unmarshal(data, &config)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", path, err)
	}

	// Substitute environment variables in string fields (for dynamic map values)
	config.SubstituteEnvVars()

	// Apply defaults
	if err := config.ApplyDefaults(); err != nil {
		return nil, fmt.Errorf("failed to apply defaults: %w", err)
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return &config, nil
}

// LoadConfigOrDefault loads configuration from a file, or returns default if file doesn't exist.
func LoadConfigOrDefault(path string) (*types.NodeDoctorConfig, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return DefaultConfig()
	}
	return LoadConfig(path)
}

// DefaultConfig returns a default configuration suitable for basic monitoring.
func DefaultConfig() (*types.NodeDoctorConfig, error) {
	// Get node name from environment, fallback to hostname if not set
	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		hostname, err := os.Hostname()
		if err != nil {
			// Last resort: use a generic name
			nodeName = "node-doctor-node"
		} else {
			nodeName = hostname
		}
	}

	config := &types.NodeDoctorConfig{
		APIVersion: "node-doctor.io/v1alpha1",
		Kind:       "NodeDoctorConfig",
		Metadata: types.ConfigMetadata{
			Name: "default",
		},
		Settings: types.GlobalSettings{
			NodeName: nodeName,
		},
		Monitors: []types.MonitorConfig{
			{
				Name:    "kubelet-health",
				Type:    "kubernetes-kubelet-check",
				Enabled: true,
				Config: map[string]interface{}{
					"healthzURL": "http://127.0.0.1:10248/healthz",
				},
			},
		},
		Exporters: types.ExporterConfigs{
			Kubernetes: &types.KubernetesExporterConfig{
				Enabled: true,
			},
			HTTP: &types.HTTPExporterConfig{
				Enabled: false, // Disabled by default - requires webhook configuration
			},
			Prometheus: &types.PrometheusExporterConfig{
				Enabled: true,
			},
		},
		Remediation: types.RemediationConfig{
			Enabled: false, // Disabled by default for safety
		},
	}

	if err := config.ApplyDefaults(); err != nil {
		return nil, fmt.Errorf("failed to apply defaults: %w", err)
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("default config validation failed: %w", err)
	}

	return config, nil
}

// SaveConfig saves configuration to a file (YAML or JSON based on extension).
func SaveConfig(config *types.NodeDoctorConfig, path string) error {
	ext := filepath.Ext(path)

	var data []byte
	var err error

	switch ext {
	case ".yaml", ".yml":
		data, err = yaml.Marshal(config)
	case ".json":
		data, err = json.MarshalIndent(config, "", "  ")
	default:
		return fmt.Errorf("unsupported file extension: %s (use .yaml, .yml, or .json)", ext)
	}

	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// ValidateConfigFile validates a configuration file without loading it into memory.
func ValidateConfigFile(path string) error {
	_, err := LoadConfig(path)
	return err
}
