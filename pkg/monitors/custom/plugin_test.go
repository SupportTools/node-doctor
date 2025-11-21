package custom

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// mockPluginExecutor is a mock implementation of PluginExecutor for testing
type mockPluginExecutor struct {
	stdout   string
	stderr   string
	exitCode int
	err      error
	delay    time.Duration

	// Track execution calls with mutex for thread safety
	mu           sync.Mutex
	executeCalls []mockExecuteCall
}

type mockExecuteCall struct {
	pluginPath string
	args       []string
	env        map[string]string
}

func (m *mockPluginExecutor) Execute(ctx context.Context, pluginPath string, args []string, env map[string]string) (stdout, stderr string, exitCode int, err error) {
	// Record the call with mutex protection
	m.mu.Lock()
	m.executeCalls = append(m.executeCalls, mockExecuteCall{
		pluginPath: pluginPath,
		args:       args,
		env:        env,
	})
	m.mu.Unlock()

	// Simulate delay if configured
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return "", "", -1, ctx.Err()
		}
	}

	return m.stdout, m.stderr, m.exitCode, m.err
}

// Test helper to create a plugin monitor for testing
func createTestPluginMonitor(t *testing.T, configMap map[string]interface{}, mockExecutor PluginExecutor) *PluginMonitor {
	t.Helper()

	monitorConfig := types.MonitorConfig{
		Name:     "test-plugin-monitor",
		Type:     "custom-plugin",
		Interval: 10 * time.Second,
		Timeout:  5 * time.Second,
		Config:   configMap,
	}

	// Parse config
	pluginConfig, err := parsePluginConfig(configMap)
	if err != nil {
		t.Fatalf("parsePluginConfig() error: %v", err)
	}

	pluginConfig.applyDefaults()

	ctx := context.Background()
	monitorInterface, err := NewPluginMonitorWithExecutor(ctx, monitorConfig, pluginConfig, mockExecutor)
	if err != nil {
		t.Fatalf("createTestPluginMonitor() error: %v", err)
	}

	return monitorInterface.(*PluginMonitor)
}

// Configuration Tests

func TestParsePluginConfig_Valid(t *testing.T) {
	config, err := parsePluginConfig(map[string]interface{}{
		"pluginPath":       "/opt/plugins/check.sh",
		"args":             []interface{}{"--threshold", "90"},
		"outputFormat":     "json",
		"failureThreshold": 5,
		"apiTimeout":       "15s",
		"env": map[string]interface{}{
			"CUSTOM_VAR": "value",
		},
	})

	if err != nil {
		t.Fatalf("parsePluginConfig() error: %v", err)
	}

	if config.PluginPath != "/opt/plugins/check.sh" {
		t.Errorf("PluginPath = %s, want /opt/plugins/check.sh", config.PluginPath)
	}

	if len(config.Args) != 2 || config.Args[0] != "--threshold" || config.Args[1] != "90" {
		t.Errorf("Args = %v, want [--threshold 90]", config.Args)
	}

	if config.OutputFormat != OutputFormatJSON {
		t.Errorf("OutputFormat = %s, want json", config.OutputFormat)
	}

	if config.FailureThreshold != 5 {
		t.Errorf("FailureThreshold = %d, want 5", config.FailureThreshold)
	}

	if config.APITimeout != 15*time.Second {
		t.Errorf("APITimeout = %v, want 15s", config.APITimeout)
	}

	if config.Env["CUSTOM_VAR"] != "value" {
		t.Errorf("Env[CUSTOM_VAR] = %s, want value", config.Env["CUSTOM_VAR"])
	}
}

func TestParsePluginConfig_MinimalValid(t *testing.T) {
	config, err := parsePluginConfig(map[string]interface{}{
		"pluginPath": "/opt/plugins/check.sh",
	})

	if err != nil {
		t.Fatalf("parsePluginConfig() error: %v", err)
	}

	if config.PluginPath != "/opt/plugins/check.sh" {
		t.Errorf("PluginPath = %s, want /opt/plugins/check.sh", config.PluginPath)
	}

	// Apply defaults
	config.applyDefaults()

	if config.OutputFormat != defaultOutputFormat {
		t.Errorf("OutputFormat = %s, want %s", config.OutputFormat, defaultOutputFormat)
	}

	if config.FailureThreshold != defaultFailureThreshold {
		t.Errorf("FailureThreshold = %d, want %d", config.FailureThreshold, defaultFailureThreshold)
	}

	if config.APITimeout != defaultAPITimeout {
		t.Errorf("APITimeout = %v, want %v", config.APITimeout, defaultAPITimeout)
	}
}

func TestParsePluginConfig_MissingPluginPath(t *testing.T) {
	_, err := parsePluginConfig(map[string]interface{}{
		"outputFormat": "json",
	})

	if err == nil {
		t.Fatal("parsePluginConfig() expected error for missing pluginPath, got nil")
	}

	if !strings.Contains(err.Error(), "pluginPath is required") {
		t.Errorf("error message = %v, want 'pluginPath is required'", err)
	}
}

func TestParsePluginConfig_InvalidOutputFormat(t *testing.T) {
	_, err := parsePluginConfig(map[string]interface{}{
		"pluginPath":   "/opt/plugins/check.sh",
		"outputFormat": "invalid",
	})

	if err == nil {
		t.Fatal("parsePluginConfig() expected error for invalid outputFormat, got nil")
	}

	if !strings.Contains(err.Error(), "outputFormat must be") {
		t.Errorf("error message = %v, want outputFormat validation error", err)
	}
}

func TestParsePluginConfig_InvalidFailureThreshold(t *testing.T) {
	_, err := parsePluginConfig(map[string]interface{}{
		"pluginPath":       "/opt/plugins/check.sh",
		"failureThreshold": 0,
	})

	if err == nil {
		t.Fatal("parsePluginConfig() expected error for invalid failureThreshold, got nil")
	}

	if !strings.Contains(err.Error(), "failureThreshold must be at least 1") {
		t.Errorf("error message = %v, want failureThreshold validation error", err)
	}
}

func TestParsePluginConfig_InvalidAPITimeout(t *testing.T) {
	_, err := parsePluginConfig(map[string]interface{}{
		"pluginPath": "/opt/plugins/check.sh",
		"apiTimeout": -5,
	})

	if err == nil {
		t.Fatal("parsePluginConfig() expected error for invalid apiTimeout, got nil")
	}

	if !strings.Contains(err.Error(), "apiTimeout must be positive") {
		t.Errorf("error message = %v, want apiTimeout validation error", err)
	}
}

func TestParsePluginConfig_NumericTimeout(t *testing.T) {
	config, err := parsePluginConfig(map[string]interface{}{
		"pluginPath": "/opt/plugins/check.sh",
		"apiTimeout": 20, // Numeric timeout (seconds)
	})

	if err != nil {
		t.Fatalf("parsePluginConfig() error: %v", err)
	}

	if config.APITimeout != 20*time.Second {
		t.Errorf("APITimeout = %v, want 20s", config.APITimeout)
	}
}

func TestParsePluginConfig_TypeValidation(t *testing.T) {
	tests := []struct {
		name      string
		configMap map[string]interface{}
		wantError string
	}{
		{
			name: "pluginPath not string",
			configMap: map[string]interface{}{
				"pluginPath": 123,
			},
			wantError: "pluginPath must be a string",
		},
		{
			name: "args not array",
			configMap: map[string]interface{}{
				"pluginPath": "/opt/plugins/check.sh",
				"args":       "not-an-array",
			},
			wantError: "args must be an array",
		},
		{
			name: "outputFormat not string",
			configMap: map[string]interface{}{
				"pluginPath":   "/opt/plugins/check.sh",
				"outputFormat": 123,
			},
			wantError: "outputFormat must be a string",
		},
		{
			name: "env not map",
			configMap: map[string]interface{}{
				"pluginPath": "/opt/plugins/check.sh",
				"env":        "not-a-map",
			},
			wantError: "env must be a map",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parsePluginConfig(tt.configMap)
			if err == nil {
				t.Fatal("parsePluginConfig() expected error, got nil")
			}

			if !strings.Contains(err.Error(), tt.wantError) {
				t.Errorf("error = %v, want error containing %q", err, tt.wantError)
			}
		})
	}
}

func TestValidatePluginConfig_PathTraversal(t *testing.T) {
	config := types.MonitorConfig{
		Name:     "test",
		Type:     "custom-plugin",
		Interval: 10 * time.Second,
		Timeout:  5 * time.Second,
		Config: map[string]interface{}{
			"pluginPath": "/opt/../etc/passwd",
		},
	}

	err := ValidatePluginConfig(config)

	if err == nil {
		t.Fatal("ValidatePluginConfig() expected error for path traversal, got nil")
	}

	if !strings.Contains(err.Error(), "path traversal") {
		t.Errorf("error = %v, want path traversal error", err)
	}
}

func TestValidatePluginConfig_RelativePath(t *testing.T) {
	config := types.MonitorConfig{
		Name:     "test",
		Type:     "custom-plugin",
		Interval: 10 * time.Second,
		Timeout:  5 * time.Second,
		Config: map[string]interface{}{
			"pluginPath": "relative/path/check.sh",
		},
	}

	err := ValidatePluginConfig(config)

	if err == nil {
		t.Fatal("ValidatePluginConfig() expected error for relative path, got nil")
	}

	if !strings.Contains(err.Error(), "must be absolute") {
		t.Errorf("error = %v, want absolute path error", err)
	}
}

// Output Parser Tests

func TestOutputParser_JSON_Valid(t *testing.T) {
	parser := NewOutputParser(OutputFormatJSON)

	stdout := `{"status": "healthy", "message": "All systems operational", "metrics": {"cpu": 45.2}}`
	result, err := parser.Parse(stdout, "", 0)

	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if result.Status != PluginStatusHealthy {
		t.Errorf("Status = %s, want healthy", result.Status)
	}

	if result.Message != "All systems operational" {
		t.Errorf("Message = %s, want 'All systems operational'", result.Message)
	}

	if result.Metrics["cpu"] != 45.2 {
		t.Errorf("Metrics[cpu] = %v, want 45.2", result.Metrics["cpu"])
	}
}

func TestOutputParser_JSON_Invalid(t *testing.T) {
	parser := NewOutputParser(OutputFormatJSON)

	stdout := `{invalid json`
	result, err := parser.Parse(stdout, "error details", 1)

	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	// Should fall back to exit code interpretation
	if result.Status != PluginStatusWarning {
		t.Errorf("Status = %s, want warning (from exit code 1)", result.Status)
	}
}

func TestOutputParser_JSON_EmptyOutput(t *testing.T) {
	parser := NewOutputParser(OutputFormatJSON)

	result, err := parser.Parse("", "check failed", 2)

	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if result.Status != PluginStatusCritical {
		t.Errorf("Status = %s, want critical (from exit code 2)", result.Status)
	}

	if result.Message != "check failed" {
		t.Errorf("Message = %s, want 'check failed'", result.Message)
	}
}

func TestOutputParser_Simple_OKPrefix(t *testing.T) {
	parser := NewOutputParser(OutputFormatSimple)

	result, err := parser.Parse("OK: Everything is fine", "", 0)

	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if result.Status != PluginStatusHealthy {
		t.Errorf("Status = %s, want healthy", result.Status)
	}

	if result.Message != "Everything is fine" {
		t.Errorf("Message = %s, want 'Everything is fine'", result.Message)
	}
}

func TestOutputParser_Simple_ErrorPrefix(t *testing.T) {
	parser := NewOutputParser(OutputFormatSimple)

	result, err := parser.Parse("ERROR: Disk full", "", 2)

	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if result.Status != PluginStatusCritical {
		t.Errorf("Status = %s, want critical", result.Status)
	}

	if result.Message != "Disk full" {
		t.Errorf("Message = %s, want 'Disk full'", result.Message)
	}
}

func TestOutputParser_Simple_WarningPrefix(t *testing.T) {
	parser := NewOutputParser(OutputFormatSimple)

	result, err := parser.Parse("WARNING: High memory usage", "", 1)

	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if result.Status != PluginStatusWarning {
		t.Errorf("Status = %s, want warning", result.Status)
	}

	if result.Message != "High memory usage" {
		t.Errorf("Message = %s, want 'High memory usage'", result.Message)
	}
}

func TestStatusFromExitCode(t *testing.T) {
	tests := []struct {
		exitCode int
		want     PluginStatus
	}{
		{0, PluginStatusHealthy},
		{1, PluginStatusWarning},
		{2, PluginStatusCritical},
		{3, PluginStatusUnknown},
		{127, PluginStatusUnknown},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("exit_code_%d", tt.exitCode), func(t *testing.T) {
			got := statusFromExitCode(tt.exitCode)
			if got != tt.want {
				t.Errorf("statusFromExitCode(%d) = %s, want %s", tt.exitCode, got, tt.want)
			}
		})
	}
}

func TestNormalizeStatus(t *testing.T) {
	tests := []struct {
		input string
		want  PluginStatus
	}{
		{"healthy", PluginStatusHealthy},
		{"HEALTHY", PluginStatusHealthy},
		{"ok", PluginStatusHealthy},
		{"OK", PluginStatusHealthy},
		{"pass", PluginStatusHealthy},
		{"warning", PluginStatusWarning},
		{"warn", PluginStatusWarning},
		{"critical", PluginStatusCritical},
		{"error", PluginStatusCritical},
		{"fail", PluginStatusCritical},
		{"unknown", PluginStatusUnknown},
		{"invalid", PluginStatusUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeStatus(tt.input)
			if got != tt.want {
				t.Errorf("normalizeStatus(%q) = %s, want %s", tt.input, got, tt.want)
			}
		})
	}
}

// Monitor Tests

func TestPluginMonitor_HealthyCheck(t *testing.T) {
	mockExec := &mockPluginExecutor{
		stdout:   `{"status": "healthy", "message": "OK"}`,
		stderr:   "",
		exitCode: 0,
		err:      nil,
	}

	monitor := createTestPluginMonitor(t, map[string]interface{}{
		"pluginPath":   "/opt/check.sh",
		"outputFormat": "json",
	}, mockExec)

	ctx := context.Background()
	status, err := monitor.checkPlugin(ctx)

	if err != nil {
		t.Fatalf("checkPlugin() error: %v", err)
	}

	// Check for healthy condition
	foundHealthy := false
	for _, cond := range status.Conditions {
		if cond.Type == "PluginHealthy" && cond.Status == types.ConditionTrue {
			foundHealthy = true
			if !strings.Contains(cond.Message, "OK") {
				t.Errorf("Condition message = %s, want to contain 'OK'", cond.Message)
			}
			break
		}
	}

	if !foundHealthy {
		t.Error("Expected PluginHealthy condition with Status=True, not found")
	}
}

func TestPluginMonitor_WarningCheck(t *testing.T) {
	mockExec := &mockPluginExecutor{
		stdout:   `{"status": "warning", "message": "High load"}`,
		stderr:   "",
		exitCode: 1,
		err:      nil,
	}

	monitor := createTestPluginMonitor(t, map[string]interface{}{
		"pluginPath":   "/opt/check.sh",
		"outputFormat": "json",
	}, mockExec)

	ctx := context.Background()
	status, err := monitor.checkPlugin(ctx)

	if err != nil {
		t.Fatalf("checkPlugin() error: %v", err)
	}

	// Check for warning condition
	foundWarning := false
	for _, cond := range status.Conditions {
		if cond.Type == "PluginWarning" && cond.Status == types.ConditionTrue {
			foundWarning = true
			if !strings.Contains(cond.Message, "warning") {
				t.Errorf("Condition message = %s, want to contain 'warning'", cond.Message)
			}
			break
		}
	}

	if !foundWarning {
		t.Error("Expected PluginWarning condition with Status=True, not found")
	}

	if len(status.Events) == 0 {
		t.Error("Expected warning event, got none")
	}
}

func TestPluginMonitor_CriticalCheck(t *testing.T) {
	mockExec := &mockPluginExecutor{
		stdout:   `{"status": "critical", "message": "System down"}`,
		stderr:   "",
		exitCode: 2,
		err:      nil,
	}

	monitor := createTestPluginMonitor(t, map[string]interface{}{
		"pluginPath":       "/opt/check.sh",
		"outputFormat":     "json",
		"failureThreshold": 1, // Fail immediately
	}, mockExec)

	ctx := context.Background()
	status, err := monitor.checkPlugin(ctx)

	if err != nil {
		t.Fatalf("checkPlugin() error: %v", err)
	}

	// Check for unhealthy condition
	foundUnhealthy := false
	for _, cond := range status.Conditions {
		if cond.Type == "PluginUnhealthy" && cond.Status == types.ConditionTrue {
			foundUnhealthy = true
			if !strings.Contains(cond.Message, "critical") {
				t.Errorf("Condition message = %s, want to contain 'critical'", cond.Message)
			}
			break
		}
	}

	if !foundUnhealthy {
		t.Error("Expected PluginUnhealthy condition with Status=True, not found")
	}
}

func TestPluginMonitor_FailureThreshold(t *testing.T) {
	mockExec := &mockPluginExecutor{
		stdout:   `{"status": "critical", "message": "Failed"}`,
		stderr:   "",
		exitCode: 2,
		err:      nil,
	}

	monitor := createTestPluginMonitor(t, map[string]interface{}{
		"pluginPath":       "/opt/check.sh",
		"outputFormat":     "json",
		"failureThreshold": 3,
	}, mockExec)

	ctx := context.Background()

	// First failure - should not be unhealthy yet
	status, _ := monitor.checkPlugin(ctx)
	foundUnhealthy := false
	for _, cond := range status.Conditions {
		if cond.Type == "PluginUnhealthy" && cond.Status == types.ConditionTrue {
			foundUnhealthy = true
			break
		}
	}
	if foundUnhealthy {
		t.Error("First failure: Found PluginUnhealthy condition, want none (within threshold)")
	}

	// Second failure - should not be unhealthy yet
	status, _ = monitor.checkPlugin(ctx)
	foundUnhealthy = false
	for _, cond := range status.Conditions {
		if cond.Type == "PluginUnhealthy" && cond.Status == types.ConditionTrue {
			foundUnhealthy = true
			break
		}
	}
	if foundUnhealthy {
		t.Error("Second failure: Found PluginUnhealthy condition, want none (within threshold)")
	}

	// Third failure - should now be unhealthy
	status, _ = monitor.checkPlugin(ctx)
	foundUnhealthy = false
	for _, cond := range status.Conditions {
		if cond.Type == "PluginUnhealthy" && cond.Status == types.ConditionTrue {
			foundUnhealthy = true
			break
		}
	}
	if !foundUnhealthy {
		t.Error("Third failure: Expected PluginUnhealthy condition with Status=True, not found")
	}
}

func TestPluginMonitor_Recovery(t *testing.T) {
	mockExec := &mockPluginExecutor{
		stdout:   `{"status": "critical", "message": "Failed"}`,
		stderr:   "",
		exitCode: 2,
		err:      nil,
	}

	monitor := createTestPluginMonitor(t, map[string]interface{}{
		"pluginPath":       "/opt/check.sh",
		"outputFormat":     "json",
		"failureThreshold": 1,
	}, mockExec)

	ctx := context.Background()

	// First check - critical (unhealthy)
	status, _ := monitor.checkPlugin(ctx)
	foundUnhealthy := false
	for _, cond := range status.Conditions {
		if cond.Type == "PluginUnhealthy" && cond.Status == types.ConditionTrue {
			foundUnhealthy = true
			break
		}
	}
	if !foundUnhealthy {
		t.Error("Critical check: Expected PluginUnhealthy condition, not found")
	}

	// Change mock to return healthy
	mockExec.stdout = `{"status": "healthy", "message": "Recovered"}`
	mockExec.exitCode = 0

	// Second check - should recover
	status, _ = monitor.checkPlugin(ctx)
	foundHealthy := false
	for _, cond := range status.Conditions {
		if cond.Type == "PluginHealthy" && cond.Status == types.ConditionTrue {
			foundHealthy = true
			break
		}
	}
	if !foundHealthy {
		t.Error("Recovery check: Expected PluginHealthy condition, not found")
	}

	// Check for recovery event
	foundRecoveryEvent := false
	for _, event := range status.Events {
		if event.Reason == "PluginRecovered" {
			foundRecoveryEvent = true
			break
		}
	}
	if !foundRecoveryEvent {
		t.Error("Expected PluginRecovered event, not found")
	}

	// Check that unhealthy condition is cleared
	for _, cond := range status.Conditions {
		if cond.Type == "PluginUnhealthy" && cond.Status != types.ConditionFalse {
			t.Errorf("PluginUnhealthy condition should be False, got %s", cond.Status)
		}
	}
}

func TestPluginMonitor_ExecutionError(t *testing.T) {
	mockExec := &mockPluginExecutor{
		stdout:   "",
		stderr:   "command not found",
		exitCode: -1,
		err:      fmt.Errorf("exec: file not found"),
	}

	monitor := createTestPluginMonitor(t, map[string]interface{}{
		"pluginPath":       "/opt/check.sh",
		"outputFormat":     "json",
		"failureThreshold": 1,
	}, mockExec)

	ctx := context.Background()
	status, err := monitor.checkPlugin(ctx)

	if err != nil {
		t.Fatalf("checkPlugin() error: %v", err)
	}

	// Check for unhealthy condition
	foundUnhealthy := false
	for _, cond := range status.Conditions {
		if cond.Type == "PluginUnhealthy" && cond.Status == types.ConditionTrue {
			foundUnhealthy = true
			break
		}
	}
	if !foundUnhealthy {
		t.Error("Execution error: Expected PluginUnhealthy condition, not found")
	}

	if len(status.Events) == 0 {
		t.Error("Expected failure event, got none")
	}
}

func TestPluginMonitor_EnvironmentVariables(t *testing.T) {
	mockExec := &mockPluginExecutor{
		stdout:   "OK",
		stderr:   "",
		exitCode: 0,
		err:      nil,
	}

	monitor := createTestPluginMonitor(t, map[string]interface{}{
		"pluginPath":   "/opt/check.sh",
		"outputFormat": "simple",
		"env": map[string]interface{}{
			"CUSTOM_VAR": "test-value",
		},
	}, mockExec)

	ctx := context.Background()
	_, _ = monitor.checkPlugin(ctx)

	// Verify environment was passed
	if len(mockExec.executeCalls) != 1 {
		t.Fatalf("Expected 1 execute call, got %d", len(mockExec.executeCalls))
	}

	env := mockExec.executeCalls[0].env
	if env["CUSTOM_VAR"] != "test-value" {
		t.Errorf("env[CUSTOM_VAR] = %s, want test-value", env["CUSTOM_VAR"])
	}
}

func TestPluginMonitor_Arguments(t *testing.T) {
	mockExec := &mockPluginExecutor{
		stdout:   "OK",
		stderr:   "",
		exitCode: 0,
		err:      nil,
	}

	monitor := createTestPluginMonitor(t, map[string]interface{}{
		"pluginPath":   "/opt/check.sh",
		"args":         []interface{}{"--threshold", "90", "--verbose"},
		"outputFormat": "simple",
	}, mockExec)

	ctx := context.Background()
	_, _ = monitor.checkPlugin(ctx)

	// Verify arguments were passed
	if len(mockExec.executeCalls) != 1 {
		t.Fatalf("Expected 1 execute call, got %d", len(mockExec.executeCalls))
	}

	args := mockExec.executeCalls[0].args
	expectedArgs := []string{"--threshold", "90", "--verbose"}
	if len(args) != len(expectedArgs) {
		t.Errorf("args length = %d, want %d", len(args), len(expectedArgs))
	}

	for i, arg := range args {
		if arg != expectedArgs[i] {
			t.Errorf("args[%d] = %s, want %s", i, arg, expectedArgs[i])
		}
	}
}

func TestPluginMonitor_ConcurrentChecks(t *testing.T) {
	mockExec := &mockPluginExecutor{
		stdout:   "OK",
		stderr:   "",
		exitCode: 0,
		err:      nil,
		delay:    50 * time.Millisecond,
	}

	monitor := createTestPluginMonitor(t, map[string]interface{}{
		"pluginPath":   "/opt/check.sh",
		"outputFormat": "simple",
	}, mockExec)

	ctx := context.Background()

	// Run multiple checks concurrently
	done := make(chan bool, 3)
	for i := 0; i < 3; i++ {
		go func() {
			_, err := monitor.checkPlugin(ctx)
			if err != nil {
				t.Errorf("checkPlugin() error: %v", err)
			}
			done <- true
		}()
	}

	// Wait for all to complete
	for i := 0; i < 3; i++ {
		<-done
	}

	// All checks should have executed
	if len(mockExec.executeCalls) != 3 {
		t.Errorf("Expected 3 execute calls, got %d", len(mockExec.executeCalls))
	}
}

func TestPluginMonitor_ContextCancellation(t *testing.T) {
	mockExec := &mockPluginExecutor{
		stdout:   "OK",
		stderr:   "",
		exitCode: 0,
		err:      nil,
		delay:    1 * time.Second, // Long delay
	}

	monitor := createTestPluginMonitor(t, map[string]interface{}{
		"pluginPath":   "/opt/check.sh",
		"outputFormat": "simple",
	}, mockExec)

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel context after short delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	// This should be cancelled
	_, _ = monitor.checkPlugin(ctx)

	// Execution should have been attempted and cancelled
	if len(mockExec.executeCalls) != 1 {
		t.Errorf("Expected 1 execute call, got %d", len(mockExec.executeCalls))
	}
}
