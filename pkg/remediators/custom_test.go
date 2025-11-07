package remediators

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// mockScriptExecutor implements ScriptExecutor for testing.
type mockScriptExecutor struct {
	mu sync.Mutex

	// Configuration
	shouldFailExecution bool
	shouldFailSafety    bool
	exitCode            int
	stdout              string
	stderr              string
	executionDelay      time.Duration

	// State tracking
	executedScripts []string
	executedArgs    [][]string
	executedEnv     []map[string]string
	executedWorkDir []string
	safetyChecks    int
}

func (m *mockScriptExecutor) ExecuteScript(ctx context.Context, scriptPath string, args []string, env map[string]string, workingDir string) (stdout, stderr string, exitCode int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.executedScripts = append(m.executedScripts, scriptPath)
	m.executedArgs = append(m.executedArgs, args)
	m.executedEnv = append(m.executedEnv, env)
	m.executedWorkDir = append(m.executedWorkDir, workingDir)

	// Simulate execution delay
	if m.executionDelay > 0 {
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			m.mu.Lock()
			return "", "", -1, ctx.Err()
		case <-time.After(m.executionDelay):
			m.mu.Lock()
		}
	}

	if m.shouldFailExecution {
		return m.stdout, m.stderr, m.exitCode, fmt.Errorf("mock execution error")
	}

	return m.stdout, m.stderr, m.exitCode, nil
}

func (m *mockScriptExecutor) CheckScriptSafety(scriptPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.safetyChecks++

	if m.shouldFailSafety {
		return fmt.Errorf("mock safety check failed: script not executable")
	}

	return nil
}

func (m *mockScriptExecutor) getExecutedScripts() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.executedScripts...)
}

func (m *mockScriptExecutor) getLastEnv() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.executedEnv) == 0 {
		return nil
	}
	// Return a copy
	env := make(map[string]string)
	for k, v := range m.executedEnv[len(m.executedEnv)-1] {
		env[k] = v
	}
	return env
}

func (m *mockScriptExecutor) getSafetyCheckCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.safetyChecks
}

// TestNewCustomRemediator tests the constructor with various configurations.
func TestNewCustomRemediator(t *testing.T) {
	tests := []struct {
		name      string
		config    CustomConfig
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid basic config",
			config: CustomConfig{
				ScriptPath: "/usr/local/bin/remediate.sh",
			},
			wantError: false,
		},
		{
			name: "valid config with timeout",
			config: CustomConfig{
				ScriptPath: "/usr/local/bin/remediate.sh",
				Timeout:    10 * time.Second,
			},
			wantError: false,
		},
		{
			name: "valid config with args and env",
			config: CustomConfig{
				ScriptPath: "/usr/local/bin/remediate.sh",
				ScriptArgs: []string{"--fix", "--verbose"},
				Environment: map[string]string{
					"DEBUG": "true",
				},
			},
			wantError: false,
		},
		{
			name: "valid config with custom working dir",
			config: CustomConfig{
				ScriptPath: "/usr/local/bin/remediate.sh",
				WorkingDir: "/tmp",
			},
			wantError: false,
		},
		{
			name: "empty script path",
			config: CustomConfig{
				ScriptPath: "",
			},
			wantError: true,
			errorMsg:  "script path is required",
		},
		{
			name: "timeout too short",
			config: CustomConfig{
				ScriptPath: "/usr/local/bin/remediate.sh",
				Timeout:    500 * time.Millisecond,
			},
			wantError: true,
			errorMsg:  "timeout must be at least 1 second",
		},
		{
			name: "timeout too long",
			config: CustomConfig{
				ScriptPath: "/usr/local/bin/remediate.sh",
				Timeout:    31 * time.Minute,
			},
			wantError: true,
			errorMsg:  "timeout must not exceed 30 minutes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			remediator, err := NewCustomRemediator(tt.config)

			if tt.wantError {
				if err == nil {
					t.Errorf("NewCustomRemediator() expected error but got none")
				} else if tt.errorMsg != "" && err.Error() != fmt.Sprintf("invalid custom config: %s", tt.errorMsg) {
					t.Errorf("NewCustomRemediator() error = %v, want error containing %q", err, tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("NewCustomRemediator() unexpected error: %v", err)
				}
				if remediator == nil {
					t.Errorf("NewCustomRemediator() returned nil remediator")
				}
			}
		})
	}
}

// TestCustomRemediator_ExecuteScript tests successful script execution.
func TestCustomRemediator_ExecuteScript(t *testing.T) {
	config := CustomConfig{
		ScriptPath:    "/usr/local/bin/fix-disk.sh",
		ScriptArgs:    []string{"--clean", "/var/log"},
		CaptureOutput: true,
	}

	remediator, err := NewCustomRemediator(config)
	if err != nil {
		t.Fatalf("NewCustomRemediator() failed: %v", err)
	}

	mockExec := &mockScriptExecutor{
		exitCode: 0,
		stdout:   "Cleanup completed: 500MB freed",
		stderr:   "",
	}
	remediator.SetScriptExecutor(mockExec)

	problem := types.Problem{
		Type:       "disk-pressure",
		Resource:   "disk-full-001",
		Message:    "Disk usage above 90%",
		Severity:   types.ProblemCritical,
		DetectedAt: time.Now(),
		Metadata:   make(map[string]string),
	}

	ctx := context.Background()
	err = remediator.Remediate(ctx, problem)

	if err != nil {
		t.Errorf("Remediate() unexpected error: %v", err)
	}

	// Verify script was executed
	executed := mockExec.getExecutedScripts()
	if len(executed) != 1 || executed[0] != config.ScriptPath {
		t.Errorf("Script not executed correctly, got: %v, want: %s", executed, config.ScriptPath)
	}

	// Verify safety check was performed
	if mockExec.getSafetyCheckCount() != 1 {
		t.Errorf("Safety check count = %d, want 1", mockExec.getSafetyCheckCount())
	}
}

// TestCustomRemediator_ProblemEnvironment tests environment variable injection.
func TestCustomRemediator_ProblemEnvironment(t *testing.T) {
	config := CustomConfig{
		ScriptPath: "/usr/local/bin/fix-network.sh",
		Environment: map[string]string{
			"CUSTOM_VAR": "custom-value",
		},
	}

	remediator, err := NewCustomRemediator(config)
	if err != nil {
		t.Fatalf("NewCustomRemediator() failed: %v", err)
	}

	mockExec := &mockScriptExecutor{
		exitCode: 0,
	}
	remediator.SetScriptExecutor(mockExec)

	detectedTime := time.Now()
	problem := types.Problem{
		Type:       "network-unreachable",
		Resource:   "worker-1",
		Message:    "Cannot reach API server",
		Severity:   types.ProblemCritical,
		DetectedAt: detectedTime,
		Metadata: map[string]string{
			"interface": "eth0",
			"target-ip": "10.0.0.1",
		},
	}

	ctx := context.Background()
	err = remediator.Remediate(ctx, problem)

	if err != nil {
		t.Errorf("Remediate() unexpected error: %v", err)
	}

	// Verify environment variables were set correctly
	env := mockExec.getLastEnv()
	if env == nil {
		t.Fatal("No environment variables captured")
	}

	expectedVars := map[string]string{
		"PROBLEM_TYPE":           "network-unreachable",
		"PROBLEM_RESOURCE":       "worker-1",
		"PROBLEM_MESSAGE":        "Cannot reach API server",
		"PROBLEM_SEVERITY":       "Critical",
		"PROBLEM_DETECTED_AT":    detectedTime.Format(time.RFC3339),
		"PROBLEM_META_INTERFACE": "eth0",
		"PROBLEM_META_TARGET_IP": "10.0.0.1",
		"CUSTOM_VAR":             "custom-value",
	}

	for key, expectedValue := range expectedVars {
		if actualValue, ok := env[key]; !ok {
			t.Errorf("Environment variable %s not found", key)
		} else if actualValue != expectedValue {
			t.Errorf("Environment variable %s = %q, want %q", key, actualValue, expectedValue)
		}
	}
}

// TestCustomRemediator_ScriptFailure tests handling of script execution failures.
func TestCustomRemediator_ScriptFailure(t *testing.T) {
	config := CustomConfig{
		ScriptPath: "/usr/local/bin/failing-script.sh",
	}

	remediator, err := NewCustomRemediator(config)
	if err != nil {
		t.Fatalf("NewCustomRemediator() failed: %v", err)
	}

	mockExec := &mockScriptExecutor{
		shouldFailExecution: true,
		exitCode:            1,
		stderr:              "Error: operation failed",
	}
	remediator.SetScriptExecutor(mockExec)

	problem := types.Problem{
		Type:       "test-problem",
		Resource:   "test-001",
		Message:    "Test problem",
		Severity:   types.ProblemWarning,
		DetectedAt: time.Now(),
		Metadata:   make(map[string]string),
	}

	ctx := context.Background()
	err = remediator.Remediate(ctx, problem)

	if err == nil {
		t.Error("Remediate() expected error but got none")
	}
}

// TestCustomRemediator_AllowNonZeroExit tests allowing non-zero exit codes.
func TestCustomRemediator_AllowNonZeroExit(t *testing.T) {
	config := CustomConfig{
		ScriptPath:       "/usr/local/bin/severity-script.sh",
		AllowNonZeroExit: true,
	}

	remediator, err := NewCustomRemediator(config)
	if err != nil {
		t.Fatalf("NewCustomRemediator() failed: %v", err)
	}

	mockExec := &mockScriptExecutor{
		shouldFailExecution: true, // Will return error with non-zero exit
		exitCode:            2,    // Severity level
		stdout:              "Issue detected but handled",
	}
	remediator.SetScriptExecutor(mockExec)

	problem := types.Problem{
		Type:       "test-problem",
		Resource:   "test-002",
		Message:    "Test problem",
		Severity:   types.ProblemWarning,
		DetectedAt: time.Now(),
		Metadata:   make(map[string]string),
	}

	ctx := context.Background()
	err = remediator.Remediate(ctx, problem)

	// Should not fail since AllowNonZeroExit is true
	if err != nil {
		t.Errorf("Remediate() unexpected error with AllowNonZeroExit: %v", err)
	}
}

// TestCustomRemediator_SafetyCheckFailure tests script safety check failures.
func TestCustomRemediator_SafetyCheckFailure(t *testing.T) {
	config := CustomConfig{
		ScriptPath: "/usr/local/bin/unsafe-script.sh",
	}

	remediator, err := NewCustomRemediator(config)
	if err != nil {
		t.Fatalf("NewCustomRemediator() failed: %v", err)
	}

	mockExec := &mockScriptExecutor{
		shouldFailSafety: true,
	}
	remediator.SetScriptExecutor(mockExec)

	problem := types.Problem{
		Type:       "test-problem",
		Resource:   "test-003",
		Message:    "Test problem",
		Severity:   types.ProblemWarning,
		DetectedAt: time.Now(),
		Metadata:   make(map[string]string),
	}

	ctx := context.Background()
	err = remediator.Remediate(ctx, problem)

	if err == nil {
		t.Error("Remediate() expected safety check error but got none")
	}

	// Verify script was not executed
	executed := mockExec.getExecutedScripts()
	if len(executed) != 0 {
		t.Errorf("Script should not have been executed after safety check failure")
	}
}

// TestCustomRemediator_Timeout tests script execution timeout.
func TestCustomRemediator_Timeout(t *testing.T) {
	config := CustomConfig{
		ScriptPath: "/usr/local/bin/slow-script.sh",
		Timeout:    1 * time.Second,
	}

	remediator, err := NewCustomRemediator(config)
	if err != nil {
		t.Fatalf("NewCustomRemediator() failed: %v", err)
	}

	mockExec := &mockScriptExecutor{
		executionDelay: 2 * time.Second, // Longer than timeout
	}
	remediator.SetScriptExecutor(mockExec)

	problem := types.Problem{
		Type:       "test-problem",
		Resource:   "test-004",
		Message:    "Test problem",
		Severity:   types.ProblemWarning,
		DetectedAt: time.Now(),
		Metadata:   make(map[string]string),
	}

	ctx := context.Background()
	err = remediator.Remediate(ctx, problem)

	if err == nil {
		t.Error("Remediate() expected timeout error but got none")
	}

	// Verify error message mentions timeout
	if err == nil {
		t.Error("Expected timeout error but got none")
	} else {
		expectedMsg := fmt.Sprintf("remediation failed for test-problem:test-004: script execution timed out after %v", config.Timeout)
		if err.Error() != expectedMsg {
			t.Errorf("Expected timeout error message %q, got: %q", expectedMsg, err.Error())
		}
	}
}

// TestCustomRemediator_DryRun tests dry-run mode.
func TestCustomRemediator_DryRun(t *testing.T) {
	config := CustomConfig{
		ScriptPath: "/usr/local/bin/remediate.sh",
		DryRun:     true,
	}

	remediator, err := NewCustomRemediator(config)
	if err != nil {
		t.Fatalf("NewCustomRemediator() failed: %v", err)
	}

	mockExec := &mockScriptExecutor{}
	remediator.SetScriptExecutor(mockExec)

	problem := types.Problem{
		Type:       "test-problem",
		Resource:   "test-005",
		Message:    "Test problem",
		Severity:   types.ProblemWarning,
		DetectedAt: time.Now(),
		Metadata:   make(map[string]string),
	}

	ctx := context.Background()
	err = remediator.Remediate(ctx, problem)

	if err != nil {
		t.Errorf("Remediate() in dry-run unexpected error: %v", err)
	}

	// Verify script was not executed in dry-run mode
	executed := mockExec.getExecutedScripts()
	if len(executed) != 0 {
		t.Errorf("Script should not be executed in dry-run mode, but got: %v", executed)
	}

	// Verify safety check was still performed
	if mockExec.getSafetyCheckCount() != 1 {
		t.Errorf("Safety check should still run in dry-run mode")
	}
}

// TestCustomRemediator_ContextCancellation tests context cancellation handling.
func TestCustomRemediator_ContextCancellation(t *testing.T) {
	config := CustomConfig{
		ScriptPath: "/usr/local/bin/remediate.sh",
		Timeout:    5 * time.Second,
	}

	remediator, err := NewCustomRemediator(config)
	if err != nil {
		t.Fatalf("NewCustomRemediator() failed: %v", err)
	}

	mockExec := &mockScriptExecutor{
		executionDelay: 500 * time.Millisecond,
	}
	remediator.SetScriptExecutor(mockExec)

	problem := types.Problem{
		Type:       "test-problem",
		Resource:   "test-006",
		Message:    "Test problem",
		Severity:   types.ProblemWarning,
		DetectedAt: time.Now(),
		Metadata:   make(map[string]string),
	}

	// Create a context that we'll cancel immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err = remediator.Remediate(ctx, problem)

	// Should get an error due to context cancellation
	if err == nil {
		t.Error("Remediate() expected context cancellation error but got none")
	}
}

// TestCustomRemediator_Cooldown tests cooldown behavior.
func TestCustomRemediator_Cooldown(t *testing.T) {
	config := CustomConfig{
		ScriptPath: "/usr/local/bin/remediate.sh",
	}

	remediator, err := NewCustomRemediator(config)
	if err != nil {
		t.Fatalf("NewCustomRemediator() failed: %v", err)
	}

	mockExec := &mockScriptExecutor{
		exitCode: 0,
	}
	remediator.SetScriptExecutor(mockExec)

	// Override cooldown for testing
	remediator.cooldown = 100 * time.Millisecond

	problem := types.Problem{
		Type:       "test-problem",
		Resource:   "test-007",
		Message:    "Test problem",
		Severity:   types.ProblemWarning,
		DetectedAt: time.Now(),
		Metadata:   make(map[string]string),
	}

	ctx := context.Background()

	// First remediation should succeed
	err = remediator.Remediate(ctx, problem)
	if err != nil {
		t.Errorf("First remediation failed: %v", err)
	}

	// Immediate second remediation should be blocked by cooldown
	if remediator.CanRemediate(problem) {
		t.Error("CanRemediate() should return false during cooldown")
	}

	// Wait for cooldown to expire
	time.Sleep(150 * time.Millisecond)

	// Should be able to remediate again after cooldown
	if !remediator.CanRemediate(problem) {
		t.Error("CanRemediate() should return true after cooldown expires")
	}

	// Second successful remediation after cooldown
	err = remediator.Remediate(ctx, problem)
	if err != nil {
		t.Errorf("Second remediation after cooldown failed: %v", err)
	}

	// Verify script was executed twice (first time and after cooldown)
	executed := mockExec.getExecutedScripts()
	if len(executed) != 2 {
		t.Errorf("Expected 2 script executions, got %d", len(executed))
	}
}

// TestCustomRemediator_OutputCapture tests stdout/stderr capture.
func TestCustomRemediator_OutputCapture(t *testing.T) {
	config := CustomConfig{
		ScriptPath:    "/usr/local/bin/verbose-script.sh",
		CaptureOutput: true,
	}

	remediator, err := NewCustomRemediator(config)
	if err != nil {
		t.Fatalf("NewCustomRemediator() failed: %v", err)
	}

	mockExec := &mockScriptExecutor{
		exitCode: 0,
		stdout:   "Operation completed successfully",
		stderr:   "Warning: deprecated option used",
	}
	remediator.SetScriptExecutor(mockExec)

	problem := types.Problem{
		Type:       "test-problem",
		Resource:   "test-008",
		Message:    "Test problem",
		Severity:   types.ProblemWarning,
		DetectedAt: time.Now(),
		Metadata:   make(map[string]string),
	}

	ctx := context.Background()
	err = remediator.Remediate(ctx, problem)

	if err != nil {
		t.Errorf("Remediate() unexpected error: %v", err)
	}

	// Output capture is tested indirectly through logging
	// In a real scenario, logs would be captured and verified
}

// TestCustomRemediator_WorkingDirectory tests custom working directory.
func TestCustomRemediator_WorkingDirectory(t *testing.T) {
	config := CustomConfig{
		ScriptPath: "/usr/local/bin/remediate.sh",
		WorkingDir: "/tmp/remediation",
	}

	remediator, err := NewCustomRemediator(config)
	if err != nil {
		t.Fatalf("NewCustomRemediator() failed: %v", err)
	}

	mockExec := &mockScriptExecutor{
		exitCode: 0,
	}
	remediator.SetScriptExecutor(mockExec)

	problem := types.Problem{
		Type:       "test-problem",
		Resource:   "test-009",
		Message:    "Test problem",
		Severity:   types.ProblemWarning,
		DetectedAt: time.Now(),
		Metadata:   make(map[string]string),
	}

	ctx := context.Background()
	err = remediator.Remediate(ctx, problem)

	if err != nil {
		t.Errorf("Remediate() unexpected error: %v", err)
	}

	// Verify working directory was passed correctly
	m := mockExec
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.executedWorkDir) != 1 {
		t.Fatalf("Expected 1 execution, got %d", len(m.executedWorkDir))
	}

	if m.executedWorkDir[0] != config.WorkingDir {
		t.Errorf("Working directory = %s, want %s", m.executedWorkDir[0], config.WorkingDir)
	}
}
