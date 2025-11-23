package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestMainIntegration_VersionFlag tests version flag via subprocess
func TestMainIntegration_VersionFlag(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cmd := exec.Command(os.Args[0], "-version")
	cmd.Env = append(os.Environ(), "RUN_MAIN_TEST=1")

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to run with -version: %v\nOutput: %s", err, output)
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "Node Doctor") {
		t.Errorf("Expected version output to contain 'Node Doctor', got: %s", outputStr)
	}
	if !strings.Contains(outputStr, "Git Commit:") {
		t.Errorf("Expected version output to contain 'Git Commit:', got: %s", outputStr)
	}
	if !strings.Contains(outputStr, "Build Time:") {
		t.Errorf("Expected version output to contain 'Build Time:', got: %s", outputStr)
	}
}

// TestMainIntegration_ListMonitors tests list-monitors flag via subprocess
func TestMainIntegration_ListMonitors(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cmd := exec.Command(os.Args[0], "-list-monitors")
	cmd.Env = append(os.Environ(), "RUN_MAIN_TEST=1")

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to run with -list-monitors: %v\nOutput: %s", err, output)
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "Available monitor types:") {
		t.Errorf("Expected output to contain 'Available monitor types:', got: %s", outputStr)
	}
	// Should list some monitors
	if !strings.Contains(outputStr, "system-") && !strings.Contains(outputStr, "network-") && !strings.Contains(outputStr, "kubernetes-") {
		t.Errorf("Expected output to list monitor types, got: %s", outputStr)
	}
}

// TestMainIntegration_MissingConfig tests behavior when no config file exists
func TestMainIntegration_MissingConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create temp directory with no config file
	tmpDir := t.TempDir()

	cmd := exec.Command(os.Args[0])
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(), "RUN_MAIN_TEST=1")

	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("Expected error when no config file exists, got success\nOutput: %s", output)
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "No configuration file found") {
		t.Errorf("Expected error about missing config file, got: %s", outputStr)
	}
}

// TestMainIntegration_ValidateConfig tests config validation flag
func TestMainIntegration_ValidateConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create minimal valid config
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	configContent := `---
apiVersion: node-doctor.io/v1alpha1
kind: NodeDoctorConfig
metadata:
  name: test-config
settings:
  nodeName: test-node
  logLevel: info
  logFormat: text
  logOutput: stdout
monitors:
  - name: test-monitor
    type: noop
    enabled: true
    interval: 1m
    timeout: 30s
exporters:
  kubernetes:
    enabled: false
  prometheus:
    enabled: false
  http:
    enabled: false
remediation:
  enabled: false
`
	if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cmd := exec.Command(os.Args[0], "-config", configFile, "-validate-config")
	cmd.Env = append(os.Environ(), "RUN_MAIN_TEST=1")

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to validate config: %v\nOutput: %s", err, output)
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "Configuration validation passed") {
		t.Errorf("Expected validation success message, got: %s", outputStr)
	}
}

// TestMainIntegration_DumpConfig tests dump-config flag
func TestMainIntegration_DumpConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create minimal valid config
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	configContent := `---
apiVersion: node-doctor.io/v1alpha1
kind: NodeDoctorConfig
metadata:
  name: test-config
settings:
  nodeName: test-node
  logLevel: info
  logFormat: text
  logOutput: stdout
monitors:
  - name: test-monitor
    type: noop
    enabled: true
    interval: 1m
    timeout: 30s
exporters:
  kubernetes:
    enabled: false
  prometheus:
    enabled: false
  http:
    enabled: false
remediation:
  enabled: false
`
	if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cmd := exec.Command(os.Args[0], "-config", configFile, "-dump-config")
	cmd.Env = append(os.Environ(), "RUN_MAIN_TEST=1")

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to dump config: %v\nOutput: %s", err, output)
	}

	outputStr := string(output)
	// Should output JSON
	if !strings.Contains(outputStr, `"nodeName"`) || !strings.Contains(outputStr, `"monitors"`) {
		t.Errorf("Expected JSON config dump, got: %s", outputStr)
	}
}

// TestMainIntegration_SIGTERMHandling tests graceful shutdown on SIGTERM
func TestMainIntegration_SIGTERMHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create minimal valid config
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	configContent := `---
apiVersion: node-doctor.io/v1alpha1
kind: NodeDoctorConfig
metadata:
  name: test-config
settings:
  nodeName: test-node
  logLevel: info
  logFormat: json
  logOutput: stdout
monitors:
  - name: test-monitor
    type: noop
    enabled: true
    interval: 1m
    timeout: 30s
exporters:
  kubernetes:
    enabled: false
  prometheus:
    enabled: false
  http:
    enabled: false
remediation:
  enabled: false
`
	if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, os.Args[0], "-config", configFile)
	cmd.Env = append(os.Environ(), "RUN_MAIN_TEST=1")

	// Start the process
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Give it time to start up
	time.Sleep(2 * time.Second)

	// Send SIGTERM
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("Failed to send SIGTERM: %v", err)
	}

	// Wait for graceful shutdown
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		// Process should exit cleanly
		if err != nil {
			// exit status 0 is expected, any other exit status is an error
			if exitErr, ok := err.(*exec.ExitError); ok {
				if exitErr.ExitCode() != 0 {
					t.Errorf("Expected clean exit, got exit code %d", exitErr.ExitCode())
				}
			}
		}
	case <-time.After(10 * time.Second):
		// Force kill if not stopped
		cmd.Process.Kill()
		t.Error("Process did not shut down within 10 seconds after SIGTERM")
	}
}

// TestMainIntegration_SIGINTHandling tests graceful shutdown on SIGINT
func TestMainIntegration_SIGINTHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create minimal valid config
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	configContent := `---
apiVersion: node-doctor.io/v1alpha1
kind: NodeDoctorConfig
metadata:
  name: test-config
settings:
  nodeName: test-node
  logLevel: info
  logFormat: json
  logOutput: stdout
monitors:
  - name: test-monitor
    type: noop
    enabled: true
    interval: 1m
    timeout: 30s
exporters:
  kubernetes:
    enabled: false
  prometheus:
    enabled: false
  http:
    enabled: false
remediation:
  enabled: false
`
	if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, os.Args[0], "-config", configFile)
	cmd.Env = append(os.Environ(), "RUN_MAIN_TEST=1")

	// Start the process
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Give it time to start up
	time.Sleep(2 * time.Second)

	// Send SIGINT
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("Failed to send SIGINT: %v", err)
	}

	// Wait for graceful shutdown
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		// Process should exit cleanly
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				if exitErr.ExitCode() != 0 {
					t.Errorf("Expected clean exit, got exit code %d", exitErr.ExitCode())
				}
			}
		}
	case <-time.After(10 * time.Second):
		// Force kill if not stopped
		cmd.Process.Kill()
		t.Error("Process did not shut down within 10 seconds after SIGINT")
	}
}

// TestMainIntegration_InvalidConfig tests behavior with invalid config
func TestMainIntegration_InvalidConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create invalid config (missing required fields)
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	configContent := `---
apiVersion: node-doctor.io/v1alpha1
kind: NodeDoctorConfig
# Missing required fields
`
	if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cmd := exec.Command(os.Args[0], "-config", configFile)
	cmd.Env = append(os.Environ(), "RUN_MAIN_TEST=1")

	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("Expected error with invalid config, got success\nOutput: %s", output)
	}

	outputStr := string(output)
	// Should contain error about validation failure or missing fields
	if !strings.Contains(outputStr, "validation") && !strings.Contains(outputStr, "failed") && !strings.Contains(outputStr, "required") {
		t.Errorf("Expected validation error message, got: %s", outputStr)
	}
}

// Helper function to check if we should run the actual main
func init() {
	// This allows integration tests to invoke the real main() function
	// by setting RUN_MAIN_TEST environment variable
	if os.Getenv("RUN_MAIN_TEST") == "1" {
		// Remove the test flag so main() runs normally
		os.Args = filterTestFlags(os.Args)
		// The actual main() will be called after all init() functions
	}
}

// filterTestFlags removes test-related flags from os.Args
func filterTestFlags(args []string) []string {
	var filtered []string
	skipNext := false
	for i, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		// Skip test flags
		if strings.HasPrefix(arg, "-test.") {
			// Check if it's -test.flag=value or -test.flag value
			if !strings.Contains(arg, "=") && i+1 < len(args) {
				skipNext = true
			}
			continue
		}
		filtered = append(filtered, arg)
	}
	return filtered
}

// TestMain allows proper cleanup and setup for integration tests
func TestMain(m *testing.M) {
	// If we're in integration test mode, run the real main
	if os.Getenv("RUN_MAIN_TEST") == "1" {
		main()
		return
	}

	// Otherwise run tests normally
	exitCode := m.Run()
	os.Exit(exitCode)
}
