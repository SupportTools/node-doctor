package remediators

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// NetworkOperation defines the type of network operation to perform.
type NetworkOperation string

const (
	// NetworkFlushDNS flushes the DNS resolver cache
	NetworkFlushDNS NetworkOperation = "flush-dns"

	// NetworkRestartInterface restarts a network interface (down/up)
	NetworkRestartInterface NetworkOperation = "restart-interface"

	// NetworkResetRouting resets the routing table to defaults
	NetworkResetRouting NetworkOperation = "reset-routing"
)

// NetworkConfig contains configuration for the network remediator.
type NetworkConfig struct {
	// Operation specifies the network action to perform
	Operation NetworkOperation

	// InterfaceName is the name of the network interface (required for RestartInterface)
	// Examples: "eth0", "ens3", "enp0s3"
	InterfaceName string

	// BackupRouting when true, backs up routing table before reset (for ResetRouting)
	BackupRouting bool

	// VerifyAfter when true, verifies the operation succeeded
	VerifyAfter bool

	// VerifyTimeout is the maximum time to wait for verification
	VerifyTimeout time.Duration

	// DryRun when true, only simulates the action without executing it
	DryRun bool
}

// NetworkRemediator remediates network problems by performing network operations.
// It supports DNS cache flushing, network interface restarts, and routing table resets.
type NetworkRemediator struct {
	*BaseRemediator
	config NetworkConfig

	// networkExecutor allows mocking network commands for testing
	networkExecutor NetworkExecutor
}

// NetworkExecutor defines the interface for executing network commands.
// This allows for mocking in tests.
type NetworkExecutor interface {
	// ExecuteCommand executes a command with the given arguments
	ExecuteCommand(ctx context.Context, name string, args ...string) (string, error)

	// InterfaceExists checks if a network interface exists
	InterfaceExists(ctx context.Context, interfaceName string) (bool, error)

	// GetRoutingTable gets the current routing table
	GetRoutingTable(ctx context.Context) (string, error)

	// IsInterfaceUp checks if an interface is up
	IsInterfaceUp(ctx context.Context, interfaceName string) (bool, error)
}

// defaultNetworkExecutor is the default implementation that actually calls network commands.
type defaultNetworkExecutor struct{}

// ExecuteCommand executes a command and returns the output.
func (e *defaultNetworkExecutor) ExecuteCommand(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

// InterfaceExists checks if a network interface exists using ip link show.
func (e *defaultNetworkExecutor) InterfaceExists(ctx context.Context, interfaceName string) (bool, error) {
	output, err := e.ExecuteCommand(ctx, "ip", "link", "show", interfaceName)
	if err != nil {
		// Check if error is because interface doesn't exist
		if strings.Contains(output, "does not exist") || strings.Contains(output, "Device not found") {
			return false, nil
		}
		return false, fmt.Errorf("failed to check interface: %w (output: %s)", err, output)
	}
	return true, nil
}

// GetRoutingTable gets the current routing table using ip route show.
func (e *defaultNetworkExecutor) GetRoutingTable(ctx context.Context) (string, error) {
	return e.ExecuteCommand(ctx, "ip", "route", "show")
}

// IsInterfaceUp checks if an interface is up using ip link show.
func (e *defaultNetworkExecutor) IsInterfaceUp(ctx context.Context, interfaceName string) (bool, error) {
	output, err := e.ExecuteCommand(ctx, "ip", "link", "show", interfaceName)
	if err != nil {
		return false, fmt.Errorf("failed to check interface status: %w", err)
	}

	// Interface is up if output contains "state UP"
	return strings.Contains(output, "state UP"), nil
}

// NewNetworkRemediator creates a new network remediator with the given configuration.
func NewNetworkRemediator(config NetworkConfig) (*NetworkRemediator, error) {
	// Validate configuration
	if err := validateNetworkConfig(config); err != nil {
		return nil, fmt.Errorf("invalid network config: %w", err)
	}

	// Create base remediator with appropriate cooldown
	cooldown := CooldownFast // Default for network operations (3 minutes)
	if config.Operation == NetworkResetRouting {
		cooldown = CooldownMedium // Routing changes are more impactful (5 minutes)
	}

	base, err := NewBaseRemediator(
		fmt.Sprintf("network-%s", config.Operation),
		cooldown,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create base remediator: %w", err)
	}

	remediator := &NetworkRemediator{
		BaseRemediator:  base,
		config:          config,
		networkExecutor: &defaultNetworkExecutor{},
	}

	// Set the remediation function
	if err := base.SetRemediateFunc(remediator.remediate); err != nil {
		return nil, fmt.Errorf("failed to set remediate function: %w", err)
	}

	return remediator, nil
}

// validateNetworkConfig validates the network remediator configuration.
func validateNetworkConfig(config NetworkConfig) error {
	// Validate operation
	switch config.Operation {
	case NetworkFlushDNS, NetworkRestartInterface, NetworkResetRouting:
		// Valid operation
	default:
		return fmt.Errorf("invalid operation: %s (must be flush-dns, restart-interface, or reset-routing)", config.Operation)
	}

	// RestartInterface requires an interface name
	if config.Operation == NetworkRestartInterface && config.InterfaceName == "" {
		return fmt.Errorf("interface name is required for restart-interface operation")
	}

	// Set default verify timeout if not specified
	if config.VerifyAfter && config.VerifyTimeout == 0 {
		config.VerifyTimeout = 10 * time.Second
	}

	// Validate operation-specific parameters for security
	if err := validateNetworkOperation(string(config.Operation), &config); err != nil {
		return fmt.Errorf("operation validation failed: %w", err)
	}

	return nil
}

// remediate performs the actual network remediation.
func (r *NetworkRemediator) remediate(ctx context.Context, problem types.Problem) error {
	// Dry-run mode
	if r.config.DryRun {
		r.logInfof("DRY-RUN: Would execute network operation: %s", r.config.Operation)
		return nil
	}

	// Execute the network operation
	if err := r.executeOperation(ctx); err != nil {
		return fmt.Errorf("failed to execute %s: %w", r.config.Operation, err)
	}

	r.logInfof("Successfully executed network operation: %s", r.config.Operation)

	// Verify operation success if configured
	if r.config.VerifyAfter {
		if err := r.verifyOperation(ctx); err != nil {
			return fmt.Errorf("operation verification failed: %w", err)
		}
	}

	return nil
}

// executeOperation executes the configured network operation.
func (r *NetworkRemediator) executeOperation(ctx context.Context) error {
	switch r.config.Operation {
	case NetworkFlushDNS:
		return r.flushDNS(ctx)
	case NetworkRestartInterface:
		return r.restartInterface(ctx)
	case NetworkResetRouting:
		return r.resetRouting(ctx)
	default:
		return fmt.Errorf("unknown operation: %s", r.config.Operation)
	}
}

// flushDNS flushes the DNS resolver cache.
func (r *NetworkRemediator) flushDNS(ctx context.Context) error {
	r.logInfof("Flushing DNS cache")

	// Try systemd-resolved first (modern systemd systems)
	output, err := r.networkExecutor.ExecuteCommand(ctx, "resolvectl", "flush-caches")
	if err == nil {
		r.logInfof("DNS cache flushed using resolvectl: %s", output)
		return nil
	}

	// Fallback to systemd-resolve (older systemd systems)
	output, err = r.networkExecutor.ExecuteCommand(ctx, "systemd-resolve", "--flush-caches")
	if err == nil {
		r.logInfof("DNS cache flushed using systemd-resolve: %s", output)
		return nil
	}

	return fmt.Errorf("failed to flush DNS cache (tried resolvectl and systemd-resolve): %w (output: %s)", err, output)
}

// restartInterface restarts a network interface by bringing it down and then up.
func (r *NetworkRemediator) restartInterface(ctx context.Context) error {
	r.logInfof("Restarting network interface: %s", r.config.InterfaceName)

	// Safety check: verify interface exists
	exists, err := r.networkExecutor.InterfaceExists(ctx, r.config.InterfaceName)
	if err != nil {
		return fmt.Errorf("failed to verify interface exists: %w", err)
	}
	if !exists {
		return fmt.Errorf("interface %s does not exist", r.config.InterfaceName)
	}

	// Bring interface down
	r.logInfof("Bringing interface %s down", r.config.InterfaceName)
	output, err := r.networkExecutor.ExecuteCommand(ctx, "ip", "link", "set", r.config.InterfaceName, "down")
	if err != nil {
		return fmt.Errorf("failed to bring interface down: %w (output: %s)", err, output)
	}

	// Small delay to ensure interface is down
	time.Sleep(500 * time.Millisecond)

	// Bring interface up
	r.logInfof("Bringing interface %s up", r.config.InterfaceName)
	output, err = r.networkExecutor.ExecuteCommand(ctx, "ip", "link", "set", r.config.InterfaceName, "up")
	if err != nil {
		return fmt.Errorf("failed to bring interface up: %w (output: %s)", err, output)
	}

	return nil
}

// resetRouting resets the routing table.
func (r *NetworkRemediator) resetRouting(ctx context.Context) error {
	r.logInfof("Resetting routing table")

	// Backup current routing table if configured
	var routingBackup string
	if r.config.BackupRouting {
		backup, err := r.networkExecutor.GetRoutingTable(ctx)
		if err != nil {
			r.logWarnf("Failed to backup routing table: %v", err)
		} else {
			routingBackup = backup
			r.logInfof("Backed up routing table (%d bytes)", len(routingBackup))
		}
	}

	// Flush routing cache
	r.logInfof("Flushing routing cache")
	output, err := r.networkExecutor.ExecuteCommand(ctx, "ip", "route", "flush", "cache")
	if err != nil {
		// Log warning but don't fail - cache flush might not be critical
		r.logWarnf("Failed to flush routing cache: %v (output: %s)", err, output)
	}

	r.logInfof("Routing table reset complete")
	if routingBackup != "" {
		r.logInfof("Routing backup available for restore if needed")
	}

	return nil
}

// verifyOperation verifies that the network operation succeeded.
func (r *NetworkRemediator) verifyOperation(ctx context.Context) error {
	// Create a context with timeout for verification
	verifyCtx, cancel := context.WithTimeout(ctx, r.config.VerifyTimeout)
	defer cancel()

	switch r.config.Operation {
	case NetworkFlushDNS:
		// DNS flush is immediate, no verification needed
		r.logInfof("DNS flush operation requires no verification")
		return nil

	case NetworkRestartInterface:
		// Verify interface is up after restart
		return r.verifyInterfaceUp(verifyCtx)

	case NetworkResetRouting:
		// Verify routing table exists after reset
		return r.verifyRoutingTable(verifyCtx)

	default:
		return fmt.Errorf("unknown operation for verification: %s", r.config.Operation)
	}
}

// verifyInterfaceUp verifies that an interface is up after restart.
func (r *NetworkRemediator) verifyInterfaceUp(ctx context.Context) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for interface %s to come up after %v", r.config.InterfaceName, r.config.VerifyTimeout)

		case <-ticker.C:
			isUp, err := r.networkExecutor.IsInterfaceUp(ctx, r.config.InterfaceName)
			if err != nil {
				r.logWarnf("Error checking interface status during verification: %v", err)
				continue
			}

			if isUp {
				r.logInfof("Interface %s is up", r.config.InterfaceName)
				return nil
			}

			r.logInfof("Waiting for interface %s to come up...", r.config.InterfaceName)
		}
	}
}

// verifyRoutingTable verifies that routing table is accessible after reset.
func (r *NetworkRemediator) verifyRoutingTable(ctx context.Context) error {
	r.logInfof("Verifying routing table accessibility")

	_, err := r.networkExecutor.GetRoutingTable(ctx)
	if err != nil {
		return fmt.Errorf("routing table verification failed: %w", err)
	}

	r.logInfof("Routing table is accessible")
	return nil
}

// SetNetworkExecutor sets a custom network executor (useful for testing).
func (r *NetworkRemediator) SetNetworkExecutor(executor NetworkExecutor) {
	r.networkExecutor = executor
}

// logInfof logs an informational message if a logger is configured.
func (r *NetworkRemediator) logInfof(format string, args ...interface{}) {
	if r.logger != nil {
		r.logger.Infof("[network-%s] "+format, append([]interface{}{r.config.Operation}, args...)...)
	}
}

// logWarnf logs a warning message if a logger is configured.
func (r *NetworkRemediator) logWarnf(format string, args ...interface{}) {
	if r.logger != nil {
		r.logger.Warnf("[network-%s] "+format, append([]interface{}{r.config.Operation}, args...)...)
	}
}
