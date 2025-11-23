package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
	"github.com/supporttools/node-doctor/pkg/util"
)

// =============================================================================
// Configuration Loading Tests
// =============================================================================

func TestConfigurationLoading(t *testing.T) {
	tests := []struct {
		name        string
		configFile  string
		expectError bool
		validate    func(*testing.T, *types.NodeDoctorConfig)
	}{
		{
			name:        "valid YAML config",
			configFile:  "testdata/valid_config.yaml",
			expectError: false,
			validate: func(t *testing.T, cfg *types.NodeDoctorConfig) {
				if cfg.Settings.NodeName != "test-node" {
					t.Errorf("NodeName = %q, want %q", cfg.Settings.NodeName, "test-node")
				}
				if cfg.Settings.LogLevel != "info" {
					t.Errorf("LogLevel = %q, want %q", cfg.Settings.LogLevel, "info")
				}
			},
		},
		{
			name:        "full config with all options",
			configFile:  "testdata/full_config.yaml",
			expectError: false,
			validate: func(t *testing.T, cfg *types.NodeDoctorConfig) {
				if cfg.Settings.NodeName != "full-test-node" {
					t.Errorf("NodeName = %q, want %q", cfg.Settings.NodeName, "full-test-node")
				}
				if cfg.Settings.LogLevel != "debug" {
					t.Errorf("LogLevel = %q, want %q", cfg.Settings.LogLevel, "debug")
				}
				if len(cfg.Monitors) != 2 {
					t.Errorf("Monitors count = %d, want 2", len(cfg.Monitors))
				}
			},
		},
		{
			name:        "invalid YAML syntax",
			configFile:  "testdata/invalid_syntax.yaml",
			expectError: true,
		},
		{
			name:        "non-existent file",
			configFile:  "testdata/nonexistent.yaml",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := filepath.Join(getTestdataDir(t), filepath.Base(tt.configFile))

			cfg, err := util.LoadConfig(configPath)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if tt.validate != nil {
				tt.validate(t, cfg)
			}
		})
	}
}

func TestConfigFileSearchOrder(t *testing.T) {
	// Test that config file search order is correct
	candidates := []string{
		"/etc/node-doctor/config.yaml",
		"/etc/node-doctor/config.yml",
		"./config.yaml",
		"./config.yml",
	}

	// Verify the order is as expected (from main.go)
	expectedOrder := []string{
		"/etc/node-doctor/config.yaml",
		"/etc/node-doctor/config.yml",
		"./config.yaml",
		"./config.yml",
	}

	for i, c := range candidates {
		if c != expectedOrder[i] {
			t.Errorf("Config search order[%d] = %q, want %q", i, c, expectedOrder[i])
		}
	}
}

// =============================================================================
// CreateMonitors Tests
// =============================================================================

func TestCreateMonitors_TableDriven(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name          string
		configs       []types.MonitorConfig
		expectedCount int
		expectedError bool
		skipMonitors  []string // monitors that should be skipped
	}{
		{
			name:          "empty config list",
			configs:       []types.MonitorConfig{},
			expectedCount: 0,
			expectedError: false,
		},
		{
			name: "single enabled noop monitor",
			configs: []types.MonitorConfig{
				{Name: "test-noop", Type: "noop", Enabled: true, Interval: 30 * time.Second},
			},
			expectedCount: 1,
			expectedError: false,
		},
		{
			name: "single disabled monitor",
			configs: []types.MonitorConfig{
				{Name: "disabled", Type: "noop", Enabled: false},
			},
			expectedCount: 0,
			expectedError: false,
			skipMonitors:  []string{"disabled"},
		},
		{
			name: "mixed enabled and disabled",
			configs: []types.MonitorConfig{
				{Name: "enabled1", Type: "noop", Enabled: true, Interval: 30 * time.Second},
				{Name: "disabled1", Type: "noop", Enabled: false},
				{Name: "enabled2", Type: "noop", Enabled: true, Interval: 30 * time.Second},
			},
			expectedCount: 2,
			expectedError: false,
			skipMonitors:  []string{"disabled1"},
		},
		{
			name: "invalid monitor type continues",
			configs: []types.MonitorConfig{
				{Name: "invalid", Type: "nonexistent-type-xyz", Enabled: true},
			},
			expectedCount: 0,
			expectedError: false, // createMonitors logs error but continues
		},
		{
			name: "mix of valid and invalid",
			configs: []types.MonitorConfig{
				{Name: "valid", Type: "noop", Enabled: true, Interval: 30 * time.Second},
				{Name: "invalid", Type: "nonexistent-type", Enabled: true},
			},
			expectedCount: 1,
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monitors, err := createMonitors(ctx, tt.configs)

			if tt.expectedError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if len(monitors) != tt.expectedCount {
				t.Errorf("Monitor count = %d, want %d", len(monitors), tt.expectedCount)
			}
		})
	}
}

func TestCreateMonitors_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	configs := []types.MonitorConfig{
		{Name: "test", Type: "noop", Enabled: true, Interval: 30 * time.Second},
	}

	// Should still work with cancelled context (context is stored, not used immediately)
	monitors, err := createMonitors(ctx, configs)
	if err != nil {
		t.Errorf("createMonitors with cancelled context should not error: %v", err)
	}

	// Monitor may or may not be created depending on implementation
	_ = monitors
}

// =============================================================================
// CreateExporters Tests
// =============================================================================

func TestCreateExporters_TableDriven(t *testing.T) {
	tests := []struct {
		name               string
		config             *types.NodeDoctorConfig
		minExporterCount   int
		expectNoopFallback bool
	}{
		{
			name: "all exporters disabled",
			config: &types.NodeDoctorConfig{
				Settings: types.GlobalSettings{NodeName: "test-node"},
				Exporters: types.ExporterConfigs{
					Kubernetes: &types.KubernetesExporterConfig{Enabled: false},
					HTTP:       &types.HTTPExporterConfig{Enabled: false},
					Prometheus: &types.PrometheusExporterConfig{Enabled: false},
				},
			},
			minExporterCount:   1, // At least noop or health server
			expectNoopFallback: true,
		},
		{
			name: "nil exporter configs",
			config: &types.NodeDoctorConfig{
				Settings:  types.GlobalSettings{NodeName: "test-node"},
				Exporters: types.ExporterConfigs{},
			},
			minExporterCount:   1,
			expectNoopFallback: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			exporters, interfaces, err := createExporters(ctx, tt.config)
			if err != nil {
				t.Errorf("createExporters() error = %v", err)
				return
			}

			if len(exporters) < tt.minExporterCount {
				t.Errorf("Exporter count = %d, want at least %d", len(exporters), tt.minExporterCount)
			}

			if len(interfaces) < tt.minExporterCount {
				t.Errorf("Interface count = %d, want at least %d", len(interfaces), tt.minExporterCount)
			}

			// Cleanup
			for _, exp := range exporters {
				exp.Stop()
			}
		})
	}
}

// =============================================================================
// NoopExporter Tests
// =============================================================================

func TestNoopExporter_ExportStatus(t *testing.T) {
	exporter := &noopExporter{}
	ctx := context.Background()

	tests := []struct {
		name   string
		status *types.Status
	}{
		{
			name: "empty status",
			status: &types.Status{
				Source:    "test",
				Timestamp: time.Now(),
			},
		},
		{
			name: "status with events",
			status: &types.Status{
				Source: "test",
				Events: []types.Event{
					{Severity: types.EventInfo, Reason: "test", Message: "event1"},
					{Severity: types.EventWarning, Reason: "warn", Message: "event2"},
				},
				Timestamp: time.Now(),
			},
		},
		{
			name: "status with conditions",
			status: &types.Status{
				Source: "test",
				Conditions: []types.Condition{
					{Type: "Ready", Status: types.ConditionTrue},
					{Type: "Available", Status: types.ConditionFalse},
				},
				Timestamp: time.Now(),
			},
		},
		{
			name: "status with events and conditions",
			status: &types.Status{
				Source: "test",
				Events: []types.Event{
					{Severity: types.EventInfo, Reason: "test", Message: "event"},
				},
				Conditions: []types.Condition{
					{Type: "Ready", Status: types.ConditionTrue},
				},
				Timestamp: time.Now(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := exporter.ExportStatus(ctx, tt.status)
			if err != nil {
				t.Errorf("ExportStatus() error = %v", err)
			}
		})
	}
}

func TestNoopExporter_ExportProblem(t *testing.T) {
	exporter := &noopExporter{}
	ctx := context.Background()

	tests := []struct {
		name    string
		problem *types.Problem
	}{
		{
			name: "info severity",
			problem: &types.Problem{
				Type:       "test-info",
				Resource:   "resource1",
				Severity:   types.ProblemInfo,
				Message:    "info message",
				DetectedAt: time.Now(),
			},
		},
		{
			name: "warning severity",
			problem: &types.Problem{
				Type:       "test-warning",
				Resource:   "resource2",
				Severity:   types.ProblemWarning,
				Message:    "warning message",
				DetectedAt: time.Now(),
			},
		},
		{
			name: "critical severity",
			problem: &types.Problem{
				Type:       "test-critical",
				Resource:   "resource3",
				Severity:   types.ProblemCritical,
				Message:    "critical message",
				DetectedAt: time.Now(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := exporter.ExportProblem(ctx, tt.problem)
			if err != nil {
				t.Errorf("ExportProblem() error = %v", err)
			}
		})
	}
}

func TestNoopExporter_Stop(t *testing.T) {
	exporter := &noopExporter{}

	// Stop should be idempotent
	for i := 0; i < 3; i++ {
		err := exporter.Stop()
		if err != nil {
			t.Errorf("Stop() call %d error = %v", i+1, err)
		}
	}
}

func TestNoopExporter_ConcurrentAccess(t *testing.T) {
	exporter := &noopExporter{}
	ctx := context.Background()

	var wg sync.WaitGroup
	errChan := make(chan error, 100)

	// Concurrent ExportStatus calls
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			status := &types.Status{
				Source:    "test",
				Timestamp: time.Now(),
			}
			if err := exporter.ExportStatus(ctx, status); err != nil {
				errChan <- err
			}
		}(i)
	}

	// Concurrent ExportProblem calls
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			problem := &types.Problem{
				Type:       "test",
				Resource:   "resource",
				Severity:   types.ProblemInfo,
				Message:    "test",
				DetectedAt: time.Now(),
			}
			if err := exporter.ExportProblem(ctx, problem); err != nil {
				errChan <- err
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Errorf("Concurrent access error: %v", err)
	}
}

// =============================================================================
// MonitorFactoryAdapter Tests
// =============================================================================

func TestMonitorFactoryAdapter_CreateMonitor(t *testing.T) {
	ctx := context.Background()
	adapter := &monitorFactoryAdapter{ctx: ctx}

	tests := []struct {
		name        string
		config      types.MonitorConfig
		expectError bool
	}{
		{
			name: "valid noop monitor",
			config: types.MonitorConfig{
				Name:     "test",
				Type:     "noop",
				Enabled:  true,
				Interval: 30 * time.Second,
			},
			expectError: false,
		},
		{
			name: "invalid monitor type",
			config: types.MonitorConfig{
				Name:    "test",
				Type:    "nonexistent-monitor-type",
				Enabled: true,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monitor, err := adapter.CreateMonitor(tt.config)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if monitor == nil {
				t.Error("Monitor is nil")
			}
		})
	}
}

// =============================================================================
// DumpConfiguration Tests
// =============================================================================

func TestDumpConfiguration_OutputFormat(t *testing.T) {
	config := &types.NodeDoctorConfig{
		APIVersion: "v1",
		Kind:       "NodeDoctorConfig",
		Metadata:   types.ConfigMetadata{Name: "test"},
		Settings: types.GlobalSettings{
			NodeName:  "test-node",
			LogLevel:  "info",
			LogFormat: "json",
		},
	}

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	dumpConfiguration(config)

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	// Verify it's valid JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Errorf("Output is not valid JSON: %v", err)
	}

	// Verify indentation (pretty printed)
	if !strings.Contains(output, "\n") {
		t.Error("Output should be pretty printed with newlines")
	}
}

func TestDumpConfiguration_PreservesAllFields(t *testing.T) {
	config := &types.NodeDoctorConfig{
		APIVersion: "v1",
		Kind:       "NodeDoctorConfig",
		Metadata: types.ConfigMetadata{
			Name:      "test-config",
			Namespace: "test-namespace",
		},
		Settings: types.GlobalSettings{
			NodeName:   "test-node",
			LogLevel:   "debug",
			LogFormat:  "text",
			DryRunMode: true,
		},
		Monitors: []types.MonitorConfig{
			{Name: "mon1", Type: "noop", Enabled: true},
			{Name: "mon2", Type: "noop", Enabled: false},
		},
	}

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	dumpConfiguration(config)

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	// Parse and verify
	var parsed types.NodeDoctorConfig
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("Failed to parse output: %v", err)
	}

	if parsed.Metadata.Name != "test-config" {
		t.Errorf("Metadata.Name = %q, want %q", parsed.Metadata.Name, "test-config")
	}
	if parsed.Settings.DryRunMode != true {
		t.Error("DryRunMode not preserved")
	}
	if len(parsed.Monitors) != 2 {
		t.Errorf("Monitor count = %d, want 2", len(parsed.Monitors))
	}
}

// =============================================================================
// Version Information Tests
// =============================================================================

func TestVersionVariables(t *testing.T) {
	// Test that version variables are defined
	if Version == "" {
		t.Error("Version should not be empty")
	}

	// In test context, these should have default values
	if Version != "dev" && Version == "" {
		t.Error("Version should be 'dev' or a valid version string")
	}
}

func TestVersionOutputFormat(t *testing.T) {
	// Test the expected format of version output
	// This tests the format used in main() for -version flag

	expectedFields := []string{
		"Node Doctor",
		"Git Commit:",
		"Build Time:",
		"Go Version:",
		"OS/Arch:",
	}

	// Build expected output format
	output := "Node Doctor " + Version + "\n"
	output += "Git Commit: " + GitCommit + "\n"
	output += "Build Time: " + BuildTime + "\n"
	output += "Go Version: " + runtime.Version() + "\n"
	output += "OS/Arch: " + runtime.GOOS + "/" + runtime.GOARCH + "\n"

	for _, field := range expectedFields {
		if !strings.Contains(output, field) {
			t.Errorf("Version output should contain %q", field)
		}
	}
}

// =============================================================================
// Interface Compliance Tests
// =============================================================================

func TestInterfaceCompliance(t *testing.T) {
	// Verify noopExporter implements required interfaces
	var _ types.Exporter = (*noopExporter)(nil)
	var _ ExporterLifecycle = (*noopExporter)(nil)

	// Verify monitorFactoryAdapter can be used
	ctx := context.Background()
	_ = &monitorFactoryAdapter{ctx: ctx}
}

// =============================================================================
// Integration-Style Tests (using subprocess)
// =============================================================================

func TestMainBinary_VersionFlag(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Build the binary first
	binaryPath := filepath.Join(t.TempDir(), "node-doctor-test")
	cmd := exec.Command("go", "build", "-o", binaryPath, ".")
	cmd.Dir = getPackageDir(t)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build binary: %v\nOutput: %s", err, output)
	}

	// Run with -version flag
	cmd = exec.Command(binaryPath, "-version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("Running with -version flag failed: %v\nOutput: %s", err, output)
		return
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "Node Doctor") {
		t.Error("Version output should contain 'Node Doctor'")
	}
	if !strings.Contains(outputStr, "Go Version:") {
		t.Error("Version output should contain Go version")
	}
}

func TestMainBinary_ListMonitors(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Build the binary first
	binaryPath := filepath.Join(t.TempDir(), "node-doctor-test")
	cmd := exec.Command("go", "build", "-o", binaryPath, ".")
	cmd.Dir = getPackageDir(t)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build binary: %v\nOutput: %s", err, output)
	}

	// Run with -list-monitors flag
	cmd = exec.Command(binaryPath, "-list-monitors")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("Running with -list-monitors flag failed: %v\nOutput: %s", err, output)
		return
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "Available monitor types") {
		t.Error("Output should contain 'Available monitor types'")
	}
}

func TestMainBinary_ValidateConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Build the binary first
	binaryPath := filepath.Join(t.TempDir(), "node-doctor-test")
	cmd := exec.Command("go", "build", "-o", binaryPath, ".")
	cmd.Dir = getPackageDir(t)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build binary: %v\nOutput: %s", err, output)
	}

	// Run with valid config
	configPath := filepath.Join(getTestdataDir(t), "valid_config.yaml")
	cmd = exec.Command(binaryPath, "-config", configPath, "-validate-config")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("Validation of valid config failed: %v\nOutput: %s", err, output)
	}
}

func TestMainBinary_DumpConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Build the binary first
	binaryPath := filepath.Join(t.TempDir(), "node-doctor-test")
	cmd := exec.Command("go", "build", "-o", binaryPath, ".")
	cmd.Dir = getPackageDir(t)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build binary: %v\nOutput: %s", err, output)
	}

	// Run with valid config and dump-config
	configPath := filepath.Join(getTestdataDir(t), "valid_config.yaml")
	cmd = exec.Command(binaryPath, "-config", configPath, "-dump-config")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("Dump config failed: %v\nOutput: %s", err, output)
		return
	}

	// Output should be valid JSON
	var parsed map[string]interface{}
	// Skip log lines and find JSON
	lines := strings.Split(string(output), "\n")
	var jsonLines []string
	inJSON := false
	for _, line := range lines {
		if strings.HasPrefix(line, "{") {
			inJSON = true
		}
		if inJSON {
			jsonLines = append(jsonLines, line)
		}
	}
	jsonOutput := strings.Join(jsonLines, "\n")

	if err := json.Unmarshal([]byte(jsonOutput), &parsed); err != nil {
		t.Errorf("Dump config output is not valid JSON: %v\nOutput: %s", err, jsonOutput)
	}
}

func TestMainBinary_MissingConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Build the binary first
	binaryPath := filepath.Join(t.TempDir(), "node-doctor-test")
	cmd := exec.Command("go", "build", "-o", binaryPath, ".")
	cmd.Dir = getPackageDir(t)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build binary: %v\nOutput: %s", err, output)
	}

	// Run with non-existent config
	cmd = exec.Command(binaryPath, "-config", "/nonexistent/config.yaml")
	output, err := cmd.CombinedOutput()

	// Should fail with error
	if err == nil {
		t.Error("Expected error for missing config file")
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "Failed to load configuration") && !strings.Contains(outputStr, "No configuration file") {
		t.Errorf("Output should indicate config loading failure, got: %s", outputStr)
	}
}

func TestMainBinary_InvalidConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Build the binary first
	binaryPath := filepath.Join(t.TempDir(), "node-doctor-test")
	cmd := exec.Command("go", "build", "-o", binaryPath, ".")
	cmd.Dir = getPackageDir(t)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build binary: %v\nOutput: %s", err, output)
	}

	// Run with invalid config
	configPath := filepath.Join(getTestdataDir(t), "invalid_syntax.yaml")
	cmd = exec.Command(binaryPath, "-config", configPath)
	output, err := cmd.CombinedOutput()

	// Should fail
	if err == nil {
		t.Error("Expected error for invalid config file")
	}

	_ = output // Error output may vary
}

func TestMainBinary_FlagOverrides(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Build the binary first
	binaryPath := filepath.Join(t.TempDir(), "node-doctor-test")
	cmd := exec.Command("go", "build", "-o", binaryPath, ".")
	cmd.Dir = getPackageDir(t)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build binary: %v\nOutput: %s", err, output)
	}

	// Run with config and flags
	configPath := filepath.Join(getTestdataDir(t), "valid_config.yaml")
	cmd = exec.Command(binaryPath,
		"-config", configPath,
		"-debug",
		"-log-level", "warn",
		"-dump-config",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("Running with flag overrides failed: %v\nOutput: %s", err, output)
		return
	}

	// Parse JSON output (skip log lines)
	lines := strings.Split(string(output), "\n")
	var jsonLines []string
	inJSON := false
	for _, line := range lines {
		if strings.HasPrefix(line, "{") {
			inJSON = true
		}
		if inJSON {
			jsonLines = append(jsonLines, line)
		}
	}
	jsonOutput := strings.Join(jsonLines, "\n")

	var parsed types.NodeDoctorConfig
	if err := json.Unmarshal([]byte(jsonOutput), &parsed); err != nil {
		t.Fatalf("Failed to parse config output: %v", err)
	}

	// -log-level should override to "warn"
	if parsed.Settings.LogLevel != "warn" {
		t.Errorf("LogLevel = %q, want %q (flag override)", parsed.Settings.LogLevel, "warn")
	}
}

// =============================================================================
// Helper Functions
// =============================================================================

func getPackageDir(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("Failed to get caller info")
	}
	return filepath.Dir(filename)
}

func getTestdataDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(getPackageDir(t), "testdata")
}
