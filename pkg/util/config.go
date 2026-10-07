// Package util provides utility functions for Node Doctor.
package util

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/supporttools/node-doctor/pkg/types"
	"gopkg.in/yaml.v3"
)

// LoadConfig loads configuration from a file (YAML or JSON).
// The file format is determined by extension (.yaml, .yml, .json).
// Environment variables are substituted, defaults are applied, and validation is performed.
// Unknown keys anywhere in the typed schema are rejected; monitor `config` maps stay free-form.
func LoadConfig(path string) (*types.NodeDoctorConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	// Expand before parsing so env vars also work in non-string fields (e.g. port: ${PORT}).
	data = []byte(os.ExpandEnv(string(data)))

	var config types.NodeDoctorConfig

	switch filepath.Ext(path) {
	case ".yaml", ".yml":
		err = decodeYAMLStrict(data, &config)
	case ".json":
		err = decodeJSONStrict(data, &config)
	default:
		err = decodeYAMLStrict(data, &config)
		if err != nil {
			err = decodeJSONStrict(data, &config)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", path, err)
	}

	config.SubstituteEnvVars()

	if err := config.ApplyDefaults(); err != nil {
		return nil, fmt.Errorf("failed to apply defaults: %w", err)
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return &config, nil
}

func decodeYAMLStrict(data []byte, out *types.NodeDoctorConfig) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	err := dec.Decode(out)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func decodeJSONStrict(data []byte, out *types.NodeDoctorConfig) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode(out)
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
