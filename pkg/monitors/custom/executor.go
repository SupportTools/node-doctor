package custom

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	// Maximum output size to prevent memory exhaustion (1MB)
	maxOutputSize = 1 * 1024 * 1024

	// Default plugin execution timeout
	defaultPluginTimeout = 10 * time.Second
)

// PluginExecutor defines the interface for executing plugin binaries
type PluginExecutor interface {
	// Execute runs the plugin and returns stdout, stderr, exit code, and error
	Execute(ctx context.Context, pluginPath string, args []string, env map[string]string) (stdout, stderr string, exitCode int, err error)
}

// realPluginExecutor implements PluginExecutor using os/exec
type realPluginExecutor struct {
	timeout time.Duration
}

// NewPluginExecutor creates a new plugin executor with the specified timeout
func NewPluginExecutor(timeout time.Duration) PluginExecutor {
	if timeout <= 0 {
		timeout = defaultPluginTimeout
	}
	return &realPluginExecutor{
		timeout: timeout,
	}
}

// Execute runs the plugin binary with the given arguments and environment variables
func (e *realPluginExecutor) Execute(ctx context.Context, pluginPath string, args []string, env map[string]string) (stdout, stderr string, exitCode int, err error) {
	// Validate plugin path for security
	if err := validatePluginPath(pluginPath); err != nil {
		return "", "", -1, fmt.Errorf("invalid plugin path: %w", err)
	}

	// Check if plugin file exists and is executable
	if err := validatePluginExecutable(pluginPath); err != nil {
		return "", "", -1, fmt.Errorf("plugin validation failed: %w", err)
	}

	// Create context with timeout
	execCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	// Create command
	cmd := exec.CommandContext(execCtx, pluginPath, args...)

	// Prepare environment variables
	cmd.Env = prepareEnvironment(env)

	// Prepare output buffers with size limits
	var stdoutBuf, stderrBuf limitedBuffer
	stdoutBuf.limit = maxOutputSize
	stderrBuf.limit = maxOutputSize
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	// Execute the command
	execErr := cmd.Run()

	// Get output
	stdout = stdoutBuf.String()
	stderr = stderrBuf.String()

	// Determine exit code
	exitCode = getExitCode(execErr)

	// Handle execution errors
	if execErr != nil {
		// Check if it was a timeout
		if execCtx.Err() == context.DeadlineExceeded {
			return stdout, stderr, exitCode, fmt.Errorf("plugin execution timeout after %v", e.timeout)
		}

		// For non-zero exit codes, this is expected behavior (not an error)
		if exitCode > 0 {
			return stdout, stderr, exitCode, nil
		}

		// Other execution errors
		return stdout, stderr, exitCode, fmt.Errorf("plugin execution failed: %w", execErr)
	}

	return stdout, stderr, exitCode, nil
}

// validatePluginPath performs security validation on the plugin path
func validatePluginPath(pluginPath string) error {
	if pluginPath == "" {
		return fmt.Errorf("plugin path cannot be empty")
	}

	// Prevent path traversal attacks
	if strings.Contains(pluginPath, "..") {
		return fmt.Errorf("plugin path cannot contain '..' (path traversal)")
	}

	// Require absolute paths
	if !filepath.IsAbs(pluginPath) {
		return fmt.Errorf("plugin path must be absolute, got: %s", pluginPath)
	}

	// Clean the path to normalize it
	cleanPath := filepath.Clean(pluginPath)
	if cleanPath != pluginPath {
		return fmt.Errorf("plugin path must be clean (no . or .. components)")
	}

	return nil
}

// validatePluginExecutable checks if the plugin file exists and is executable
func validatePluginExecutable(pluginPath string) error {
	// Check if file exists
	info, err := os.Stat(pluginPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("plugin file does not exist: %s", pluginPath)
		}
		return fmt.Errorf("failed to stat plugin file: %w", err)
	}

	// Check if it's a regular file
	if !info.Mode().IsRegular() {
		return fmt.Errorf("plugin path is not a regular file: %s", pluginPath)
	}

	// Check if it's executable
	if info.Mode().Perm()&0111 == 0 {
		return fmt.Errorf("plugin file is not executable: %s (permissions: %s)", pluginPath, info.Mode().Perm())
	}

	return nil
}

// prepareEnvironment merges custom environment variables with the current environment
func prepareEnvironment(customEnv map[string]string) []string {
	// Start with current environment
	env := os.Environ()

	// Add custom environment variables
	for key, value := range customEnv {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}

	return env
}

// getExitCode extracts the exit code from an exec error
func getExitCode(err error) int {
	if err == nil {
		return 0
	}

	// Try to get exit code from ExitError
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}

	// Unknown error, return -1
	return -1
}

// limitedBuffer is a buffer that stops accepting writes after reaching a size limit
type limitedBuffer struct {
	bytes.Buffer
	limit int
}

// Write implements io.Writer with a size limit
func (b *limitedBuffer) Write(p []byte) (n int, err error) {
	if b.limit > 0 && b.Len() >= b.limit {
		// Buffer is full, discard additional data
		return len(p), nil
	}

	// Calculate how much we can write
	remaining := b.limit - b.Len()
	if remaining < len(p) {
		// Write only what fits
		n, err = b.Buffer.Write(p[:remaining])
		return len(p), err // Return full length to prevent errors
	}

	// Write all data
	return b.Buffer.Write(p)
}
