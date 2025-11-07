package remediators

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// CustomConfig contains configuration for the custom script remediator.
type CustomConfig struct {
	// ScriptPath is the absolute path to the remediation script
	ScriptPath string

	// ScriptArgs are optional arguments to pass to the script
	ScriptArgs []string

	// Timeout is the maximum execution time for the script (default: 5 minutes)
	Timeout time.Duration

	// Environment contains additional environment variables to pass to the script
	// Problem metadata is automatically injected as environment variables
	Environment map[string]string

	// CaptureOutput when true, captures and logs stdout/stderr
	CaptureOutput bool

	// AllowNonZeroExit when true, doesn't treat non-zero exit codes as failures
	// Useful for scripts that use exit codes to indicate severity levels
	AllowNonZeroExit bool

	// WorkingDir is the working directory for script execution (default: script's directory)
	WorkingDir string

	// DryRun when true, only simulates the action without executing it
	DryRun bool
}

// CustomRemediator executes custom user-defined remediation scripts.
// It provides a flexible way to integrate custom remediation logic while
// maintaining safety checks and proper error handling.
type CustomRemediator struct {
	*BaseRemediator
	config CustomConfig

	// scriptExecutor allows mocking script execution for testing
	scriptExecutor ScriptExecutor
}

// ScriptExecutor defines the interface for executing custom scripts.
// This allows for mocking in tests.
type ScriptExecutor interface {
	// ExecuteScript executes a script with given arguments and environment
	ExecuteScript(ctx context.Context, scriptPath string, args []string, env map[string]string, workingDir string) (stdout, stderr string, exitCode int, err error)

	// CheckScriptSafety verifies the script exists and has proper permissions
	CheckScriptSafety(scriptPath string) error
}

// defaultScriptExecutor is the default implementation that actually executes scripts.
type defaultScriptExecutor struct{}

// ExecuteScript executes a script and returns the output.
func (e *defaultScriptExecutor) ExecuteScript(ctx context.Context, scriptPath string, args []string, env map[string]string, workingDir string) (stdout, stderr string, exitCode int, err error) {
	cmd := exec.CommandContext(ctx, scriptPath, args...)

	// Set working directory
	if workingDir != "" {
		cmd.Dir = workingDir
	}

	// Merge custom environment with system environment
	cmd.Env = os.Environ()
	for key, value := range env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, value))
	}

	// Capture stdout and stderr
	var stdoutBuf, stderrBuf strings.Builder
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	// Execute the command
	err = cmd.Run()

	stdout = strings.TrimSpace(stdoutBuf.String())
	stderr = strings.TrimSpace(stderrBuf.String())

	// Get exit code
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			// Non-exit error (e.g., script not found, permission denied)
			exitCode = -1
		}
	} else {
		exitCode = 0
	}

	return stdout, stderr, exitCode, err
}

// CheckScriptSafety verifies the script exists and has proper permissions.
func (e *defaultScriptExecutor) CheckScriptSafety(scriptPath string) error {
	// Check if script exists
	info, err := os.Stat(scriptPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("script does not exist: %s", scriptPath)
		}
		return fmt.Errorf("failed to stat script: %w", err)
	}

	// Check if it's a regular file
	if !info.Mode().IsRegular() {
		return fmt.Errorf("script is not a regular file: %s", scriptPath)
	}

	// Check if it's executable
	if info.Mode()&0111 == 0 {
		return fmt.Errorf("script is not executable: %s (mode: %s)", scriptPath, info.Mode())
	}

	return nil
}

// NewCustomRemediator creates a new custom script remediator with the given configuration.
func NewCustomRemediator(config CustomConfig) (*CustomRemediator, error) {
	// Validate configuration
	if err := validateCustomConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid custom config: %w", err)
	}

	// Create base remediator with medium cooldown (5 minutes for custom scripts)
	scriptName := filepath.Base(config.ScriptPath)
	base, err := NewBaseRemediator(
		fmt.Sprintf("custom-%s", scriptName),
		CooldownMedium,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create base remediator: %w", err)
	}

	remediator := &CustomRemediator{
		BaseRemediator: base,
		config:         config,
		scriptExecutor: &defaultScriptExecutor{},
	}

	// Set the remediation function
	if err := base.SetRemediateFunc(remediator.remediate); err != nil {
		return nil, fmt.Errorf("failed to set remediate function: %w", err)
	}

	return remediator, nil
}

// validateCustomConfig validates the custom remediator configuration.
func validateCustomConfig(config *CustomConfig) error {
	// Validate script path
	if config.ScriptPath == "" {
		return fmt.Errorf("script path is required")
	}

	// Convert to absolute path if relative
	if !filepath.IsAbs(config.ScriptPath) {
		absPath, err := filepath.Abs(config.ScriptPath)
		if err != nil {
			return fmt.Errorf("failed to resolve absolute path: %w", err)
		}
		config.ScriptPath = absPath
	}

	// Set defaults
	if config.Timeout == 0 {
		config.Timeout = 5 * time.Minute // Default: 5 minutes
	}

	// Validate timeout
	if config.Timeout < time.Second {
		return fmt.Errorf("timeout must be at least 1 second")
	}

	if config.Timeout > 30*time.Minute {
		return fmt.Errorf("timeout must not exceed 30 minutes")
	}

	// Set working directory to script's directory if not specified
	if config.WorkingDir == "" {
		config.WorkingDir = filepath.Dir(config.ScriptPath)
	}

	// Initialize environment map if nil
	if config.Environment == nil {
		config.Environment = make(map[string]string)
	}

	return nil
}

// remediate performs the actual custom script execution.
func (r *CustomRemediator) remediate(ctx context.Context, problem types.Problem) error {
	// Safety checks
	if err := r.scriptExecutor.CheckScriptSafety(r.config.ScriptPath); err != nil {
		return fmt.Errorf("script safety check failed: %w", err)
	}

	r.logInfof("Executing custom remediation script: %s", filepath.Base(r.config.ScriptPath))

	// Dry-run mode
	if r.config.DryRun {
		r.logInfof("DRY-RUN: Would execute script: %s with args: %v", r.config.ScriptPath, r.config.ScriptArgs)
		return nil
	}

	// Prepare environment variables from problem metadata
	env := r.prepareProblemEnvironment(problem)

	// Add custom environment variables
	for key, value := range r.config.Environment {
		env[key] = value
	}

	// Create context with timeout
	execCtx, cancel := context.WithTimeout(ctx, r.config.Timeout)
	defer cancel()

	// Execute the script
	stdout, stderr, exitCode, err := r.scriptExecutor.ExecuteScript(
		execCtx,
		r.config.ScriptPath,
		r.config.ScriptArgs,
		env,
		r.config.WorkingDir,
	)

	// Log output if configured
	if r.config.CaptureOutput {
		if stdout != "" {
			r.logInfof("Script stdout: %s", stdout)
		}
		if stderr != "" {
			r.logWarnf("Script stderr: %s", stderr)
		}
	}

	// Check for context cancellation/timeout
	if execCtx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("script execution timed out after %v", r.config.Timeout)
	}

	// Handle execution errors
	if err != nil {
		if r.config.AllowNonZeroExit && exitCode > 0 {
			r.logWarnf("Script exited with code %d (allowed): %v", exitCode, err)
			return nil
		}
		return fmt.Errorf("script execution failed (exit code %d): %w", exitCode, err)
	}

	r.logInfof("Script executed successfully (exit code %d)", exitCode)
	return nil
}

// prepareProblemEnvironment converts problem metadata to environment variables.
// This allows scripts to access problem details for context-aware remediation.
func (r *CustomRemediator) prepareProblemEnvironment(problem types.Problem) map[string]string {
	env := make(map[string]string)

	// Basic problem metadata
	env["PROBLEM_TYPE"] = problem.Type
	env["PROBLEM_RESOURCE"] = problem.Resource
	env["PROBLEM_MESSAGE"] = problem.Message
	env["PROBLEM_SEVERITY"] = string(problem.Severity)
	env["PROBLEM_DETECTED_AT"] = problem.DetectedAt.Format(time.RFC3339)

	// Additional metadata
	for key, value := range problem.Metadata {
		// Convert metadata key to environment variable format (uppercase, underscores)
		envKey := "PROBLEM_META_" + strings.ToUpper(strings.ReplaceAll(key, "-", "_"))
		env[envKey] = value
	}

	return env
}

// SetScriptExecutor sets a custom script executor (useful for testing).
func (r *CustomRemediator) SetScriptExecutor(executor ScriptExecutor) {
	r.scriptExecutor = executor
}

// GetScriptPath returns the configured script path (useful for testing).
func (r *CustomRemediator) GetScriptPath() string {
	return r.config.ScriptPath
}

// logInfof logs an informational message if a logger is configured.
func (r *CustomRemediator) logInfof(format string, args ...interface{}) {
	if r.logger != nil {
		scriptName := filepath.Base(r.config.ScriptPath)
		r.logger.Infof("[custom-%s] "+format, append([]interface{}{scriptName}, args...)...)
	}
}

// logWarnf logs a warning message if a logger is configured.
func (r *CustomRemediator) logWarnf(format string, args ...interface{}) {
	if r.logger != nil {
		scriptName := filepath.Base(r.config.ScriptPath)
		r.logger.Warnf("[custom-%s] "+format, append([]interface{}{scriptName}, args...)...)
	}
}
