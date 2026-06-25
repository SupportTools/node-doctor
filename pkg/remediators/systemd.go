package remediators

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// SystemdOperation defines the type of systemd operation to perform.
type SystemdOperation string

const (
	// SystemdRestart restarts the service
	SystemdRestart SystemdOperation = "restart"

	// SystemdStop stops the service
	SystemdStop SystemdOperation = "stop"

	// SystemdStart starts the service
	SystemdStart SystemdOperation = "start"

	// SystemdReload reloads the service configuration
	SystemdReload SystemdOperation = "reload"
)

// SystemdConfig contains configuration for the systemd remediator.
type SystemdConfig struct {
	// Operation specifies the systemd action to perform (restart, stop, start, reload)
	Operation SystemdOperation

	// ServiceName is the name of the systemd service (e.g., "kubelet", "docker", "containerd")
	ServiceName string

	// VerifyStatus when true, verifies service is active after remediation
	VerifyStatus bool

	// VerifyTimeout is the maximum time to wait for service to become active after remediation
	VerifyTimeout time.Duration

	// DryRun when true, only simulates the action without executing it
	DryRun bool
}

// SystemdRemediator remediates problems by performing systemd service operations.
// It supports restarting, stopping, starting, and reloading systemd services like kubelet, docker, and containerd.
type SystemdRemediator struct {
	*BaseRemediator
	config SystemdConfig

	// systemdExecutor allows mocking systemd commands for testing
	systemdExecutor SystemdExecutor
}

// SystemdExecutor defines the interface for executing systemd commands.
// This allows for mocking in tests.
type SystemdExecutor interface {
	// ExecuteSystemctl executes a systemctl command with the given arguments
	ExecuteSystemctl(ctx context.Context, args ...string) (string, error)

	// IsActive checks if a service is currently active
	IsActive(ctx context.Context, serviceName string) (bool, error)
}

// defaultSystemdExecutor is the default implementation that actually calls systemctl.
type defaultSystemdExecutor struct{}

// ExecuteSystemctl executes a systemctl command and returns the output.
func (e *defaultSystemdExecutor) ExecuteSystemctl(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "systemctl", args...)
	output, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

// IsActive checks if a service is active using systemctl is-active.
func (e *defaultSystemdExecutor) IsActive(ctx context.Context, serviceName string) (bool, error) {
	output, err := e.ExecuteSystemctl(ctx, "is-active", serviceName)

	// systemctl is-active returns:
	// - "active" with exit code 0 if service is running
	// - Other states (inactive, failed, etc.) with non-zero exit code
	if err != nil {
		// Check for known states
		switch output {
		case "inactive":
			return false, nil // Service is stopped (not an error)
		case "failed":
			return false, nil // Service has failed (not an error in checking)
		case "activating", "deactivating", "reloading":
			return false, fmt.Errorf("service in transitional state: %s", output)
		default:
			return false, fmt.Errorf("systemctl is-active failed: %w (status: %s)", err, output)
		}
	}

	return output == "active", nil
}

// NewSystemdRemediator creates a new systemd remediator with the given configuration.
func NewSystemdRemediator(config SystemdConfig) (*SystemdRemediator, error) {
	// Validate configuration
	if err := validateSystemdConfig(config); err != nil {
		return nil, fmt.Errorf("invalid systemd config: %w", err)
	}

	// Create base remediator with medium cooldown (5 minutes default for systemd services).
	// When ServiceName is empty the remediator is a metadata-driven singleton (the
	// service is resolved per-call from Problem.Metadata["service"]); use a stable
	// name so the base remediator is still uniquely identifiable.
	serviceLabel := config.ServiceName
	if serviceLabel == "" {
		serviceLabel = "dynamic"
	}
	base, err := NewBaseRemediator(
		fmt.Sprintf("systemd-%s-%s", config.Operation, serviceLabel),
		CooldownMedium,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create base remediator: %w", err)
	}

	remediator := &SystemdRemediator{
		BaseRemediator:  base,
		config:          config,
		systemdExecutor: &defaultSystemdExecutor{},
	}

	// Set the remediation function
	if err := base.SetRemediateFunc(remediator.remediate); err != nil {
		return nil, fmt.Errorf("failed to set remediate function: %w", err)
	}

	return remediator, nil
}

// validateSystemdConfig validates the systemd remediator configuration.
//
// ServiceName is intentionally NOT required here: a systemd remediator may be
// registered as a metadata-driven singleton whose target service is supplied
// per-call via Problem.Metadata["service"]. When neither config.ServiceName nor
// the per-call metadata is set, remediate() fails with a clear error at
// dispatch time rather than at construction time.
func validateSystemdConfig(config SystemdConfig) error {
	// Validate operation
	switch config.Operation {
	case SystemdRestart, SystemdStop, SystemdStart, SystemdReload:
		// Valid operation
	default:
		return fmt.Errorf("invalid operation: %s (must be restart, stop, start, or reload)", config.Operation)
	}

	// Set default verify timeout if not specified
	if config.VerifyStatus && config.VerifyTimeout == 0 {
		config.VerifyTimeout = 30 * time.Second
	}

	return nil
}

// remediate performs the actual systemd service remediation.
//
// The target service name is resolved per-call: when the Problem carries a
// "service" metadata key (set by the detector from the strategy's
// MonitorRemediationConfig.Service), that value is used; otherwise the
// remediator falls back to its construction-time config.ServiceName. This lets
// a single registered systemd-restart remediator act on whatever service the
// triggering monitor declared (kubelet, containerd, etc.) without registering
// one remediator per service.
func (r *SystemdRemediator) remediate(ctx context.Context, problem types.Problem) error {
	serviceName := r.resolveServiceName(problem)
	if serviceName == "" {
		return fmt.Errorf("no systemd service specified (neither problem metadata %q nor config.ServiceName set)", metadataKeyService)
	}

	// Dry-run mode
	if r.config.DryRun {
		r.logInfof("DRY-RUN: Would execute systemctl %s %s", r.config.Operation, serviceName)
		return nil
	}

	// Check service status before remediation
	wasActive, err := r.systemdExecutor.IsActive(ctx, serviceName)
	if err != nil {
		r.logWarnf("Failed to check service status before remediation: %v", err)
	} else {
		r.logInfof("Service %s status before remediation: active=%v", serviceName, wasActive)
	}

	// Execute the systemd operation
	if err := r.executeOperation(ctx, serviceName); err != nil {
		return fmt.Errorf("failed to execute %s on %s: %w", r.config.Operation, serviceName, err)
	}

	r.logInfof("Successfully executed systemctl %s %s", r.config.Operation, serviceName)

	// Verify service status after remediation if configured
	if r.config.VerifyStatus {
		if err := r.verifyServiceStatus(ctx, serviceName); err != nil {
			return fmt.Errorf("service verification failed after %s: %w", r.config.Operation, err)
		}
	}

	return nil
}

// resolveServiceName returns the systemd service to act on for this remediation.
// It prefers the per-call "service" metadata key (threaded from the strategy's
// MonitorRemediationConfig.Service) and falls back to the construction-time
// config.ServiceName when the metadata is absent.
func (r *SystemdRemediator) resolveServiceName(problem types.Problem) string {
	if problem.Metadata != nil {
		if svc := problem.Metadata[metadataKeyService]; svc != "" {
			return svc
		}
	}
	return r.config.ServiceName
}

// executeOperation executes the configured systemd operation against serviceName.
func (r *SystemdRemediator) executeOperation(ctx context.Context, serviceName string) error {
	switch r.config.Operation {
	case SystemdRestart:
		return r.restart(ctx, serviceName)
	case SystemdStop:
		return r.stop(ctx, serviceName)
	case SystemdStart:
		return r.start(ctx, serviceName)
	case SystemdReload:
		return r.reload(ctx, serviceName)
	default:
		return fmt.Errorf("unknown operation: %s", r.config.Operation)
	}
}

// restart restarts the systemd service.
func (r *SystemdRemediator) restart(ctx context.Context, serviceName string) error {
	r.logInfof("Restarting service: %s", serviceName)
	output, err := r.systemdExecutor.ExecuteSystemctl(ctx, "restart", serviceName)
	if err != nil {
		return fmt.Errorf("systemctl restart failed: %w (output: %s)", err, output)
	}
	return nil
}

// stop stops the systemd service.
func (r *SystemdRemediator) stop(ctx context.Context, serviceName string) error {
	r.logInfof("Stopping service: %s", serviceName)
	output, err := r.systemdExecutor.ExecuteSystemctl(ctx, "stop", serviceName)
	if err != nil {
		return fmt.Errorf("systemctl stop failed: %w (output: %s)", err, output)
	}
	return nil
}

// start starts the systemd service.
func (r *SystemdRemediator) start(ctx context.Context, serviceName string) error {
	r.logInfof("Starting service: %s", serviceName)
	output, err := r.systemdExecutor.ExecuteSystemctl(ctx, "start", serviceName)
	if err != nil {
		return fmt.Errorf("systemctl start failed: %w (output: %s)", err, output)
	}
	return nil
}

// reload reloads the systemd service configuration.
func (r *SystemdRemediator) reload(ctx context.Context, serviceName string) error {
	r.logInfof("Reloading service: %s", serviceName)
	output, err := r.systemdExecutor.ExecuteSystemctl(ctx, "reload", serviceName)
	if err != nil {
		return fmt.Errorf("systemctl reload failed: %w (output: %s)", err, output)
	}
	return nil
}

// verifyServiceStatus verifies that the service is active after remediation.
func (r *SystemdRemediator) verifyServiceStatus(ctx context.Context, serviceName string) error {
	// Create a context with timeout for verification
	verifyCtx, cancel := context.WithTimeout(ctx, r.config.VerifyTimeout)
	defer cancel()

	// Poll the service status until it's active or timeout
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-verifyCtx.Done():
			return fmt.Errorf("timeout waiting for service to become active after %v", r.config.VerifyTimeout)

		case <-ticker.C:
			isActive, err := r.systemdExecutor.IsActive(verifyCtx, serviceName)
			if err != nil {
				r.logWarnf("Error checking service status during verification: %v", err)
				continue
			}

			if isActive {
				r.logInfof("Service %s is now active", serviceName)
				return nil
			}

			r.logInfof("Waiting for service %s to become active...", serviceName)
		}
	}
}

// SetSystemdExecutor sets a custom systemd executor (useful for testing).
func (r *SystemdRemediator) SetSystemdExecutor(executor SystemdExecutor) {
	r.systemdExecutor = executor
}

// logInfof logs an informational message if a logger is configured.
func (r *SystemdRemediator) logInfof(format string, args ...interface{}) {
	if r.logger != nil {
		r.logger.Infof("[systemd-%s] "+format, append([]interface{}{r.config.ServiceName}, args...)...)
	}
}

// logWarnf logs a warning message if a logger is configured.
func (r *SystemdRemediator) logWarnf(format string, args ...interface{}) {
	if r.logger != nil {
		r.logger.Warnf("[systemd-%s] "+format, append([]interface{}{r.config.ServiceName}, args...)...)
	}
}
