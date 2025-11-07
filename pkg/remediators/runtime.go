package remediators

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/supporttools/node-doctor/pkg/types"
)

// RuntimeType defines the type of container runtime.
type RuntimeType string

const (
	// RuntimeDocker represents Docker container runtime
	RuntimeDocker RuntimeType = "docker"

	// RuntimeContainerd represents containerd container runtime
	RuntimeContainerd RuntimeType = "containerd"

	// RuntimeCRIO represents CRI-O container runtime
	RuntimeCRIO RuntimeType = "crio"

	// RuntimeAuto automatically detects the runtime type
	RuntimeAuto RuntimeType = "auto"
)

// RuntimeOperation defines the type of runtime operation to perform.
type RuntimeOperation string

const (
	// RuntimeRestartDaemon restarts the runtime daemon via systemd
	RuntimeRestartDaemon RuntimeOperation = "restart-daemon"

	// RuntimeCleanContainers cleans up stopped containers
	RuntimeCleanContainers RuntimeOperation = "clean-containers"

	// RuntimePruneVolumes prunes dangling volumes
	RuntimePruneVolumes RuntimeOperation = "prune-volumes"
)

// RuntimeConfig contains configuration for the runtime remediator.
type RuntimeConfig struct {
	// Operation specifies the runtime action to perform
	Operation RuntimeOperation

	// RuntimeType specifies which runtime to target (docker, containerd, crio, auto)
	// If set to "auto", the remediator will detect the runtime automatically
	RuntimeType RuntimeType

	// VerifyAfter when true, verifies the operation succeeded
	VerifyAfter bool

	// DryRun when true, only simulates the action without executing it
	DryRun bool
}

// RuntimeRemediator remediates container runtime problems.
// It supports Docker, containerd, and CRI-O runtimes with auto-detection.
type RuntimeRemediator struct {
	*BaseRemediator
	config RuntimeConfig

	// runtimeExecutor allows mocking runtime commands for testing
	runtimeExecutor RuntimeExecutor

	// detectedRuntime caches the detected runtime type
	detectedRuntime RuntimeType
}

// RuntimeExecutor defines the interface for executing runtime commands.
// This allows for mocking in tests.
type RuntimeExecutor interface {
	// ExecuteCommand executes a command with the given arguments
	ExecuteCommand(ctx context.Context, name string, args ...string) (string, error)

	// IsRuntimeAvailable checks if a runtime is available on the system
	IsRuntimeAvailable(ctx context.Context, runtime RuntimeType) (bool, error)

	// GetSystemdServiceName returns the systemd service name for a runtime
	GetSystemdServiceName(runtime RuntimeType) string
}

// defaultRuntimeExecutor is the default implementation that actually calls runtime commands.
type defaultRuntimeExecutor struct{}

// ExecuteCommand executes a command and returns the output.
func (e *defaultRuntimeExecutor) ExecuteCommand(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

// IsRuntimeAvailable checks if a runtime is available by trying to run a version command.
func (e *defaultRuntimeExecutor) IsRuntimeAvailable(ctx context.Context, runtime RuntimeType) (bool, error) {
	var cmd string
	var args []string

	switch runtime {
	case RuntimeDocker:
		cmd = "docker"
		args = []string{"version", "--format", "{{.Server.Version}}"}
	case RuntimeContainerd:
		cmd = "ctr"
		args = []string{"version"}
	case RuntimeCRIO:
		cmd = "crictl"
		args = []string{"version"}
	default:
		return false, fmt.Errorf("unknown runtime type: %s", runtime)
	}

	_, err := e.ExecuteCommand(ctx, cmd, args...)
	if err != nil {
		// Command failed, runtime not available
		return false, nil
	}

	return true, nil
}

// GetSystemdServiceName returns the systemd service name for a runtime.
func (e *defaultRuntimeExecutor) GetSystemdServiceName(runtime RuntimeType) string {
	switch runtime {
	case RuntimeDocker:
		return "docker"
	case RuntimeContainerd:
		return "containerd"
	case RuntimeCRIO:
		return "crio"
	default:
		return ""
	}
}

// NewRuntimeRemediator creates a new runtime remediator with the given configuration.
func NewRuntimeRemediator(config RuntimeConfig) (*RuntimeRemediator, error) {
	// Validate configuration
	if err := validateRuntimeConfig(config); err != nil {
		return nil, fmt.Errorf("invalid runtime config: %w", err)
	}

	// Create base remediator with medium cooldown (5 minutes for runtime operations)
	base, err := NewBaseRemediator(
		fmt.Sprintf("runtime-%s", config.Operation),
		CooldownMedium,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create base remediator: %w", err)
	}

	remediator := &RuntimeRemediator{
		BaseRemediator:  base,
		config:          config,
		runtimeExecutor: &defaultRuntimeExecutor{},
	}

	// Set the remediation function
	if err := base.SetRemediateFunc(remediator.remediate); err != nil {
		return nil, fmt.Errorf("failed to set remediate function: %w", err)
	}

	return remediator, nil
}

// validateRuntimeConfig validates the runtime remediator configuration.
func validateRuntimeConfig(config RuntimeConfig) error {
	// Validate operation
	switch config.Operation {
	case RuntimeRestartDaemon, RuntimeCleanContainers, RuntimePruneVolumes:
		// Valid operation
	default:
		return fmt.Errorf("invalid operation: %s", config.Operation)
	}

	// Validate runtime type
	switch config.RuntimeType {
	case RuntimeDocker, RuntimeContainerd, RuntimeCRIO, RuntimeAuto, "":
		// Valid runtime type
		if config.RuntimeType == "" {
			config.RuntimeType = RuntimeAuto // Default to auto-detection
		}
	default:
		return fmt.Errorf("invalid runtime type: %s", config.RuntimeType)
	}

	return nil
}

// remediate performs the actual container runtime remediation.
func (r *RuntimeRemediator) remediate(ctx context.Context, problem types.Problem) error {
	// Detect runtime if needed
	if r.config.RuntimeType == RuntimeAuto {
		if r.detectedRuntime == "" {
			runtime, err := r.detectRuntime(ctx)
			if err != nil {
				return fmt.Errorf("failed to detect runtime: %w", err)
			}
			r.detectedRuntime = runtime
			r.config.RuntimeType = runtime
			r.logInfof("Detected container runtime: %s", runtime)
		}
	} else {
		// Runtime explicitly specified, just cache it if not already cached
		if r.detectedRuntime == "" {
			r.detectedRuntime = r.config.RuntimeType
		}
	}

	// Dry-run mode
	if r.config.DryRun {
		r.logInfof("DRY-RUN: Would execute runtime operation: %s on %s", r.config.Operation, r.config.RuntimeType)
		return nil
	}

	// Execute the runtime operation
	if err := r.executeOperation(ctx); err != nil {
		return fmt.Errorf("failed to execute %s on %s: %w", r.config.Operation, r.config.RuntimeType, err)
	}

	r.logInfof("Successfully executed runtime operation: %s on %s", r.config.Operation, r.config.RuntimeType)

	// Verify operation success if configured
	if r.config.VerifyAfter {
		if err := r.verifyOperation(ctx); err != nil {
			r.logWarnf("Operation verification warning: %v", err)
			// Don't fail the remediation if verification fails, just log warning
		}
	}

	return nil
}

// detectRuntime automatically detects which container runtime is available.
func (r *RuntimeRemediator) detectRuntime(ctx context.Context) (RuntimeType, error) {
	// Try to detect in order of preference: Docker, containerd, CRI-O
	runtimes := []RuntimeType{RuntimeDocker, RuntimeContainerd, RuntimeCRIO}

	for _, runtime := range runtimes {
		available, err := r.runtimeExecutor.IsRuntimeAvailable(ctx, runtime)
		if err != nil {
			r.logWarnf("Error checking %s availability: %v", runtime, err)
			continue
		}

		if available {
			return runtime, nil
		}
	}

	return "", fmt.Errorf("no supported container runtime detected (tried: docker, containerd, crio)")
}

// executeOperation executes the configured runtime operation.
func (r *RuntimeRemediator) executeOperation(ctx context.Context) error {
	switch r.config.Operation {
	case RuntimeRestartDaemon:
		return r.restartDaemon(ctx)
	case RuntimeCleanContainers:
		return r.cleanContainers(ctx)
	case RuntimePruneVolumes:
		return r.pruneVolumes(ctx)
	default:
		return fmt.Errorf("unknown operation: %s", r.config.Operation)
	}
}

// restartDaemon restarts the runtime daemon via systemd.
func (r *RuntimeRemediator) restartDaemon(ctx context.Context) error {
	serviceName := r.runtimeExecutor.GetSystemdServiceName(r.config.RuntimeType)
	if serviceName == "" {
		return fmt.Errorf("unknown systemd service for runtime: %s", r.config.RuntimeType)
	}

	r.logInfof("Restarting runtime daemon: %s", serviceName)

	output, err := r.runtimeExecutor.ExecuteCommand(ctx, "systemctl", "restart", serviceName)
	if err != nil {
		return fmt.Errorf("failed to restart %s daemon: %w (output: %s)", serviceName, err, output)
	}

	r.logInfof("Runtime daemon %s restarted successfully", serviceName)
	return nil
}

// cleanContainers cleans up stopped containers.
func (r *RuntimeRemediator) cleanContainers(ctx context.Context) error {
	r.logInfof("Cleaning up stopped containers for %s", r.config.RuntimeType)

	var cmd string
	var args []string

	switch r.config.RuntimeType {
	case RuntimeDocker:
		cmd = "docker"
		args = []string{"container", "prune", "-f"}
	case RuntimeContainerd:
		// containerd uses ctr for container management
		cmd = "ctr"
		args = []string{"containers", "rm", "$(ctr containers list -q)"}
	case RuntimeCRIO:
		// CRI-O uses crictl for container management
		cmd = "crictl"
		args = []string{"rmp", "-a"} // Remove all stopped containers
	default:
		return fmt.Errorf("unsupported runtime for container cleanup: %s", r.config.RuntimeType)
	}

	output, err := r.runtimeExecutor.ExecuteCommand(ctx, cmd, args...)
	if err != nil {
		// For some runtimes, error might occur if there are no containers to remove
		// Log as warning but don't fail
		r.logWarnf("Container cleanup completed with warnings: %v (output: %s)", err, output)
		return nil
	}

	r.logInfof("Stopped containers cleaned: %s", output)
	return nil
}

// pruneVolumes prunes dangling volumes.
func (r *RuntimeRemediator) pruneVolumes(ctx context.Context) error {
	r.logInfof("Pruning dangling volumes for %s", r.config.RuntimeType)

	// Volume pruning is primarily a Docker feature
	// containerd and CRI-O handle volumes differently
	if r.config.RuntimeType != RuntimeDocker {
		r.logWarnf("Volume pruning is only supported for Docker runtime, skipping for %s", r.config.RuntimeType)
		return nil
	}

	output, err := r.runtimeExecutor.ExecuteCommand(ctx, "docker", "volume", "prune", "-f")
	if err != nil {
		return fmt.Errorf("failed to prune volumes: %w (output: %s)", err, output)
	}

	r.logInfof("Dangling volumes pruned: %s", output)
	return nil
}

// verifyOperation verifies that the runtime operation succeeded.
func (r *RuntimeRemediator) verifyOperation(ctx context.Context) error {
	switch r.config.Operation {
	case RuntimeRestartDaemon:
		return r.verifyDaemonRunning(ctx)
	case RuntimeCleanContainers:
		// Container cleanup verification is not critical
		r.logInfof("Container cleanup verification skipped")
		return nil
	case RuntimePruneVolumes:
		// Volume prune verification is not critical
		r.logInfof("Volume prune verification skipped")
		return nil
	default:
		return fmt.Errorf("unknown operation for verification: %s", r.config.Operation)
	}
}

// verifyDaemonRunning verifies that the runtime daemon is running after restart.
func (r *RuntimeRemediator) verifyDaemonRunning(ctx context.Context) error {
	serviceName := r.runtimeExecutor.GetSystemdServiceName(r.config.RuntimeType)
	if serviceName == "" {
		return fmt.Errorf("unknown systemd service for runtime: %s", r.config.RuntimeType)
	}

	r.logInfof("Verifying %s daemon is running", serviceName)

	output, err := r.runtimeExecutor.ExecuteCommand(ctx, "systemctl", "is-active", serviceName)
	if err != nil {
		return fmt.Errorf("daemon verification failed: %w (status: %s)", err, output)
	}

	if output != "active" {
		return fmt.Errorf("daemon is not active: %s", output)
	}

	r.logInfof("Runtime daemon %s is active", serviceName)
	return nil
}

// SetRuntimeExecutor sets a custom runtime executor (useful for testing).
func (r *RuntimeRemediator) SetRuntimeExecutor(executor RuntimeExecutor) {
	r.runtimeExecutor = executor
}

// GetDetectedRuntime returns the detected runtime type (useful for testing).
func (r *RuntimeRemediator) GetDetectedRuntime() RuntimeType {
	return r.detectedRuntime
}

// logInfof logs an informational message if a logger is configured.
func (r *RuntimeRemediator) logInfof(format string, args ...interface{}) {
	if r.logger != nil {
		r.logger.Infof("[runtime-%s] "+format, append([]interface{}{r.config.Operation}, args...)...)
	}
}

// logWarnf logs a warning message if a logger is configured.
func (r *RuntimeRemediator) logWarnf(format string, args ...interface{}) {
	if r.logger != nil {
		r.logger.Warnf("[runtime-%s] "+format, append([]interface{}{r.config.Operation}, args...)...)
	}
}
