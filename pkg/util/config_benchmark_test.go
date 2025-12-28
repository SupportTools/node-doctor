/*
Performance Baseline Documentation - Config Benchmarks

These benchmarks measure the performance of configuration loading, parsing,
validation, and defaults application.

Performance Targets:
- BenchmarkLoadConfig_YAML (10 monitors): Target <5ms
- BenchmarkLoadConfig_JSON: Typically 20-30% faster than YAML
- BenchmarkApplyDefaults: Target <100µs per config
- BenchmarkValidate: Target <200µs per config
- BenchmarkEnvVarSubstitution: Target <50µs per substitution
- BenchmarkDefaultConfig: Target <1ms

Run benchmarks with:

	go test -bench=BenchmarkLoadConfig -benchmem ./pkg/util/
	go test -bench=Benchmark -benchmem ./pkg/util/

For detailed profiling:

	go test -bench=BenchmarkLoadConfig -cpuprofile=cpu.prof ./pkg/util/
	go tool pprof cpu.prof
*/
package util

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// BenchmarkLoadConfig_YAML measures the performance of loading and parsing
// YAML configuration files of various sizes.
// Target: <5ms for typical config (10 monitors).
func BenchmarkLoadConfig_YAML(b *testing.B) {
	// Set required environment
	os.Setenv("NODE_NAME", "bench-node")
	defer os.Unsetenv("NODE_NAME")

	benchmarks := []struct {
		name        string
		numMonitors int
	}{
		{"minimal_1_monitor", 1},
		{"small_5_monitors", 5},
		{"medium_10_monitors", 10},
		{"large_20_monitors", 20},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			content := generateYAMLConfig(bm.numMonitors)
			tmpDir := b.TempDir()
			configPath := filepath.Join(tmpDir, "config.yaml")
			if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
				b.Fatalf("Failed to write config file: %v", err)
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_, err := LoadConfig(configPath)
				if err != nil {
					b.Fatalf("LoadConfig failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkLoadConfig_JSON measures the performance of loading and parsing
// JSON configuration files.
func BenchmarkLoadConfig_JSON(b *testing.B) {
	os.Setenv("NODE_NAME", "bench-node")
	defer os.Unsetenv("NODE_NAME")

	benchmarks := []struct {
		name        string
		numMonitors int
	}{
		{"minimal_1_monitor", 1},
		{"small_5_monitors", 5},
		{"medium_10_monitors", 10},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			content := generateJSONConfig(bm.numMonitors)
			tmpDir := b.TempDir()
			configPath := filepath.Join(tmpDir, "config.json")
			if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
				b.Fatalf("Failed to write config file: %v", err)
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_, err := LoadConfig(configPath)
				if err != nil {
					b.Fatalf("LoadConfig failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkApplyDefaults measures the performance of applying default values
// to configuration structures.
// Target: <100µs per config.
func BenchmarkApplyDefaults(b *testing.B) {
	benchmarks := []struct {
		name        string
		numMonitors int
	}{
		{"1_monitor", 1},
		{"5_monitors", 5},
		{"20_monitors", 20},
		{"50_monitors", 50},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				config := createRawConfig(bm.numMonitors)
				err := config.ApplyDefaults()
				if err != nil {
					b.Fatalf("ApplyDefaults failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkValidate measures the performance of validating configuration.
// This includes field validation, range checks, and dependency detection.
// Target: <200µs per config.
func BenchmarkValidate(b *testing.B) {
	os.Setenv("NODE_NAME", "bench-node")
	defer os.Unsetenv("NODE_NAME")

	benchmarks := []struct {
		name        string
		numMonitors int
	}{
		{"1_monitor", 1},
		{"10_monitors", 10},
		{"50_monitors", 50},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			config := createValidConfig(bm.numMonitors)
			_ = config.ApplyDefaults()

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				err := config.Validate()
				if err != nil {
					b.Fatalf("Validate failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkEnvVarSubstitution measures the performance of environment
// variable substitution in configuration values.
// Target: <50µs per substitution.
func BenchmarkEnvVarSubstitution(b *testing.B) {
	// Set up many env vars
	for i := 0; i < 20; i++ {
		os.Setenv(fmt.Sprintf("BENCH_VAR_%d", i), fmt.Sprintf("value-%d", i))
	}
	defer func() {
		for i := 0; i < 20; i++ {
			os.Unsetenv(fmt.Sprintf("BENCH_VAR_%d", i))
		}
	}()

	benchmarks := []struct {
		name       string
		numEnvRefs int
	}{
		{"no_env_vars", 0},
		{"5_env_vars", 5},
		{"20_env_vars", 20},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				config := createConfigWithEnvVars(bm.numEnvRefs)
				config.SubstituteEnvVars()
			}
		})
	}
}

// BenchmarkDefaultConfig measures the performance of generating a
// default configuration from scratch.
// Target: <1ms.
func BenchmarkDefaultConfig(b *testing.B) {
	os.Setenv("NODE_NAME", "bench-node")
	defer os.Unsetenv("NODE_NAME")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := DefaultConfig()
		if err != nil {
			b.Fatalf("DefaultConfig failed: %v", err)
		}
	}
}

// BenchmarkLoadConfigOrDefault measures the performance when config file exists.
func BenchmarkLoadConfigOrDefault_Exists(b *testing.B) {
	os.Setenv("NODE_NAME", "bench-node")
	defer os.Unsetenv("NODE_NAME")

	content := generateYAMLConfig(5)
	tmpDir := b.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		b.Fatalf("Failed to write config file: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := LoadConfigOrDefault(configPath)
		if err != nil {
			b.Fatalf("LoadConfigOrDefault failed: %v", err)
		}
	}
}

// BenchmarkLoadConfigOrDefault_Missing measures the performance when config file is missing.
func BenchmarkLoadConfigOrDefault_Missing(b *testing.B) {
	os.Setenv("NODE_NAME", "bench-node")
	defer os.Unsetenv("NODE_NAME")

	configPath := "/nonexistent/path/config.yaml"

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := LoadConfigOrDefault(configPath)
		if err != nil {
			b.Fatalf("LoadConfigOrDefault failed: %v", err)
		}
	}
}

// Helper functions

// generateYAMLConfig generates a YAML config with n monitors
func generateYAMLConfig(n int) string {
	var sb strings.Builder
	sb.WriteString(`apiVersion: "node-doctor.io/v1"
kind: "NodeDoctorConfig"
metadata:
  name: "bench-config"
settings:
  nodeName: "bench-node"
  logLevel: "info"
monitors:
`)
	for i := 0; i < n; i++ {
		sb.WriteString(fmt.Sprintf(`  - name: "monitor-%d"
    type: "test"
    enabled: true
    interval: "30s"
    timeout: "10s"
`, i))
	}
	sb.WriteString(`exporters:
  kubernetes:
    enabled: true
  prometheus:
    enabled: true
    port: 9101
`)
	return sb.String()
}

// generateJSONConfig generates a JSON config with n monitors
func generateJSONConfig(n int) string {
	var sb strings.Builder
	sb.WriteString(`{
  "apiVersion": "node-doctor.io/v1",
  "kind": "NodeDoctorConfig",
  "metadata": {
    "name": "bench-config"
  },
  "settings": {
    "nodeName": "bench-node",
    "logLevel": "info"
  },
  "monitors": [
`)
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteString(",\n")
		}
		sb.WriteString(fmt.Sprintf(`    {
      "name": "monitor-%d",
      "type": "test",
      "enabled": true,
      "interval": "30s",
      "timeout": "10s"
    }`, i))
	}
	sb.WriteString(`
  ],
  "exporters": {
    "kubernetes": {
      "enabled": true
    },
    "prometheus": {
      "enabled": true,
      "port": 9101
    }
  }
}`)
	return sb.String()
}

// createRawConfig creates a config without defaults applied
func createRawConfig(numMonitors int) *types.NodeDoctorConfig {
	config := &types.NodeDoctorConfig{
		APIVersion: "node-doctor.io/v1",
		Kind:       "NodeDoctorConfig",
		Metadata: types.ConfigMetadata{
			Name: "bench-config",
		},
		Settings: types.GlobalSettings{
			NodeName: "bench-node",
		},
		Monitors: make([]types.MonitorConfig, numMonitors),
	}
	for i := 0; i < numMonitors; i++ {
		config.Monitors[i] = types.MonitorConfig{
			Name:           fmt.Sprintf("monitor-%d", i),
			Type:           "test",
			Enabled:        true,
			IntervalString: "30s",
			TimeoutString:  "10s",
		}
	}
	return config
}

// createValidConfig creates a fully configured and valid config
func createValidConfig(numMonitors int) *types.NodeDoctorConfig {
	config := &types.NodeDoctorConfig{
		APIVersion: "node-doctor.io/v1",
		Kind:       "NodeDoctorConfig",
		Metadata: types.ConfigMetadata{
			Name: "bench-config",
		},
		Settings: types.GlobalSettings{
			NodeName: "bench-node",
			LogLevel: "info",
		},
		Monitors: make([]types.MonitorConfig, numMonitors),
		Exporters: types.ExporterConfigs{
			Kubernetes: &types.KubernetesExporterConfig{
				Enabled: true,
			},
			Prometheus: &types.PrometheusExporterConfig{
				Enabled: true,
				Port:    9101,
			},
		},
	}
	for i := 0; i < numMonitors; i++ {
		config.Monitors[i] = types.MonitorConfig{
			Name:     fmt.Sprintf("monitor-%d", i),
			Type:     "test",
			Enabled:  true,
			Interval: 30 * time.Second,
			Timeout:  10 * time.Second,
		}
	}
	return config
}

// createConfigWithEnvVars creates a config with environment variable references
func createConfigWithEnvVars(numEnvRefs int) *types.NodeDoctorConfig {
	config := &types.NodeDoctorConfig{
		APIVersion: "node-doctor.io/v1",
		Kind:       "NodeDoctorConfig",
		Metadata: types.ConfigMetadata{
			Name: "bench-config",
		},
		Settings: types.GlobalSettings{
			NodeName: "bench-node",
		},
		Monitors: make([]types.MonitorConfig, numEnvRefs),
	}

	for i := 0; i < numEnvRefs; i++ {
		config.Monitors[i] = types.MonitorConfig{
			Name:     fmt.Sprintf("monitor-%d", i),
			Type:     "test",
			Enabled:  true,
			Interval: 30 * time.Second,
			Timeout:  10 * time.Second,
			Config: map[string]interface{}{
				"envVar": fmt.Sprintf("${BENCH_VAR_%d}", i),
			},
		}
	}

	return config
}
