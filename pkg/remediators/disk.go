package remediators

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/supporttools/node-doctor/pkg/types"
)

// DiskOperation defines the type of disk cleanup operation to perform.
type DiskOperation string

const (
	// DiskCleanJournalLogs cleans old systemd journal logs
	DiskCleanJournalLogs DiskOperation = "clean-journal-logs"

	// DiskCleanDockerImages removes unused Docker images
	DiskCleanDockerImages DiskOperation = "clean-docker-images"

	// DiskCleanTmp removes old files from /tmp
	DiskCleanTmp DiskOperation = "clean-tmp"

	// DiskCleanContainerLayers removes unused container layers (docker system prune)
	DiskCleanContainerLayers DiskOperation = "clean-container-layers"
)

// DiskConfig contains configuration for the disk remediator.
type DiskConfig struct {
	// Operation specifies the disk cleanup action to perform
	Operation DiskOperation

	// JournalVacuumSize is the target size for journal logs (e.g., "500M", "1G")
	// Only used for CleanJournalLogs operation
	JournalVacuumSize string

	// TmpFileAge is the age in days for files to be deleted from /tmp (default: 7)
	// Only used for CleanTmp operation
	TmpFileAge int

	// MinFreeSpaceGB is the minimum free space in GB before cleanup is allowed
	// Set to 0 to disable this check
	MinFreeSpaceGB float64

	// TargetPath is the path to check for free space (default: "/")
	TargetPath string

	// VerifyAfter when true, verifies disk space was reclaimed
	VerifyAfter bool

	// DryRun when true, only simulates the action without executing it
	DryRun bool
}

// DiskRemediator remediates disk space problems by performing cleanup operations.
// It supports cleaning journal logs, Docker images, /tmp files, and container layers.
type DiskRemediator struct {
	*BaseRemediator
	config DiskConfig

	// diskExecutor allows mocking disk commands for testing
	diskExecutor DiskExecutor
}

// DiskExecutor defines the interface for executing disk cleanup commands.
// This allows for mocking in tests.
type DiskExecutor interface {
	// ExecuteCommand executes a command with the given arguments
	ExecuteCommand(ctx context.Context, name string, args ...string) (string, error)

	// GetDiskUsage returns disk usage information for a path (used GB, available GB, total GB)
	GetDiskUsage(ctx context.Context, path string) (used, available, total float64, err error)
}

// defaultDiskExecutor is the default implementation that actually calls disk commands.
type defaultDiskExecutor struct{}

// ExecuteCommand executes a command and returns the output.
func (e *defaultDiskExecutor) ExecuteCommand(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

// GetDiskUsage returns disk usage information using df command.
func (e *defaultDiskExecutor) GetDiskUsage(ctx context.Context, path string) (used, available, total float64, err error) {
	// Use df -BG to get sizes in gigabytes
	output, execErr := e.ExecuteCommand(ctx, "df", "-BG", path)
	if execErr != nil {
		return 0, 0, 0, fmt.Errorf("failed to get disk usage: %w", execErr)
	}

	// Parse df output
	// Example: "Filesystem     1G-blocks  Used Available Use% Mounted on"
	//          "/dev/sda1           50G   30G      18G  63% /"
	lines := strings.Split(output, "\n")
	if len(lines) < 2 {
		return 0, 0, 0, fmt.Errorf("unexpected df output format")
	}

	fields := strings.Fields(lines[1])
	if len(fields) < 4 {
		return 0, 0, 0, fmt.Errorf("unexpected df output fields")
	}

	// Parse sizes (remove 'G' suffix and convert to float)
	totalStr := strings.TrimSuffix(fields[1], "G")
	usedStr := strings.TrimSuffix(fields[2], "G")
	availableStr := strings.TrimSuffix(fields[3], "G")

	total, err = strconv.ParseFloat(totalStr, 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to parse total: %w", err)
	}

	used, err = strconv.ParseFloat(usedStr, 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to parse used: %w", err)
	}

	available, err = strconv.ParseFloat(availableStr, 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to parse available: %w", err)
	}

	return used, available, total, nil
}

// NewDiskRemediator creates a new disk remediator with the given configuration.
func NewDiskRemediator(config DiskConfig) (*DiskRemediator, error) {
	// Validate configuration
	if err := validateDiskConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid disk config: %w", err)
	}

	// Create base remediator with medium cooldown (5 minutes for disk operations)
	base, err := NewBaseRemediator(
		fmt.Sprintf("disk-%s", config.Operation),
		CooldownMedium,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create base remediator: %w", err)
	}

	remediator := &DiskRemediator{
		BaseRemediator: base,
		config:         config,
		diskExecutor:   &defaultDiskExecutor{},
	}

	// Set the remediation function
	if err := base.SetRemediateFunc(remediator.remediate); err != nil {
		return nil, fmt.Errorf("failed to set remediate function: %w", err)
	}

	return remediator, nil
}

// validateDiskConfig validates the disk remediator configuration.
func validateDiskConfig(config *DiskConfig) error {
	// Validate operation
	switch config.Operation {
	case DiskCleanJournalLogs, DiskCleanDockerImages, DiskCleanTmp, DiskCleanContainerLayers:
		// Valid operation
	default:
		return fmt.Errorf("invalid operation: %s", config.Operation)
	}

	// Set defaults
	if config.TargetPath == "" {
		config.TargetPath = "/"
	}

	if config.Operation == DiskCleanJournalLogs && config.JournalVacuumSize == "" {
		config.JournalVacuumSize = "500M"
	}

	if config.Operation == DiskCleanTmp && config.TmpFileAge == 0 {
		config.TmpFileAge = 7 // Default: 7 days
	}

	// Validate operation-specific parameters for security
	if err := validateDiskOperation(string(config.Operation), config); err != nil {
		return fmt.Errorf("operation validation failed: %w", err)
	}

	return nil
}

// remediate performs the actual disk cleanup remediation.
func (r *DiskRemediator) remediate(ctx context.Context, problem types.Problem) error {
	// Check minimum free space threshold before cleanup
	if r.config.MinFreeSpaceGB > 0 {
		if err := r.checkMinFreeSpace(ctx); err != nil {
			return fmt.Errorf("pre-cleanup disk space check failed: %w", err)
		}
	}

	// Get disk usage before cleanup
	usedBefore, availableBefore, _, err := r.diskExecutor.GetDiskUsage(ctx, r.config.TargetPath)
	if err != nil {
		r.logWarnf("Failed to get disk usage before cleanup: %v", err)
	} else {
		r.logInfof("Disk usage before cleanup: %.2fG used, %.2fG available", usedBefore, availableBefore)
	}

	// Dry-run mode
	if r.config.DryRun {
		r.logInfof("DRY-RUN: Would execute disk cleanup: %s", r.config.Operation)
		return nil
	}

	// Execute the disk cleanup operation
	if err := r.executeOperation(ctx); err != nil {
		return fmt.Errorf("failed to execute %s: %w", r.config.Operation, err)
	}

	r.logInfof("Successfully executed disk cleanup: %s", r.config.Operation)

	// Verify disk space was reclaimed if configured
	if r.config.VerifyAfter {
		if err := r.verifyDiskSpace(ctx, usedBefore, availableBefore); err != nil {
			r.logWarnf("Disk space verification warning: %v", err)
			// Don't fail the remediation if verification fails, just log warning
		}
	}

	return nil
}

// checkMinFreeSpace checks if there's enough free space before cleanup.
func (r *DiskRemediator) checkMinFreeSpace(ctx context.Context) error {
	_, available, _, err := r.diskExecutor.GetDiskUsage(ctx, r.config.TargetPath)
	if err != nil {
		return fmt.Errorf("failed to check free space: %w", err)
	}

	if available < r.config.MinFreeSpaceGB {
		return fmt.Errorf("insufficient free space: %.2fG available, minimum required: %.2fG", available, r.config.MinFreeSpaceGB)
	}

	r.logInfof("Free space check passed: %.2fG available (minimum: %.2fG)", available, r.config.MinFreeSpaceGB)
	return nil
}

// verifyDiskSpace verifies that disk space was reclaimed after cleanup.
func (r *DiskRemediator) verifyDiskSpace(ctx context.Context, usedBefore, availableBefore float64) error {
	usedAfter, availableAfter, _, err := r.diskExecutor.GetDiskUsage(ctx, r.config.TargetPath)
	if err != nil {
		return fmt.Errorf("failed to get disk usage after cleanup: %w", err)
	}

	spaceReclaimed := usedBefore - usedAfter
	r.logInfof("Disk usage after cleanup: %.2fG used, %.2fG available (reclaimed: %.2fG)", usedAfter, availableAfter, spaceReclaimed)

	if spaceReclaimed < 0.01 { // Less than 10MB reclaimed
		return fmt.Errorf("minimal disk space reclaimed: %.2fG", spaceReclaimed)
	}

	return nil
}

// executeOperation executes the configured disk cleanup operation.
func (r *DiskRemediator) executeOperation(ctx context.Context) error {
	switch r.config.Operation {
	case DiskCleanJournalLogs:
		return r.cleanJournalLogs(ctx)
	case DiskCleanDockerImages:
		return r.cleanDockerImages(ctx)
	case DiskCleanTmp:
		return r.cleanTmp(ctx)
	case DiskCleanContainerLayers:
		return r.cleanContainerLayers(ctx)
	default:
		return fmt.Errorf("unknown operation: %s", r.config.Operation)
	}
}

// cleanJournalLogs cleans old systemd journal logs.
func (r *DiskRemediator) cleanJournalLogs(ctx context.Context) error {
	r.logInfof("Cleaning journal logs (vacuum size: %s)", r.config.JournalVacuumSize)

	output, err := r.diskExecutor.ExecuteCommand(ctx, "journalctl", "--vacuum-size="+r.config.JournalVacuumSize)
	if err != nil {
		return fmt.Errorf("journalctl vacuum failed: %w (output: %s)", err, output)
	}

	r.logInfof("Journal logs cleaned: %s", output)
	return nil
}

// cleanDockerImages removes unused Docker images.
func (r *DiskRemediator) cleanDockerImages(ctx context.Context) error {
	r.logInfof("Cleaning unused Docker images")

	// Use -a to remove all unused images, -f to force without confirmation
	output, err := r.diskExecutor.ExecuteCommand(ctx, "docker", "image", "prune", "-a", "-f")
	if err != nil {
		return fmt.Errorf("docker image prune failed: %w (output: %s)", err, output)
	}

	r.logInfof("Docker images cleaned: %s", output)
	return nil
}

// cleanTmp removes old files from /tmp.
func (r *DiskRemediator) cleanTmp(ctx context.Context) error {
	r.logInfof("Cleaning /tmp files older than %d days", r.config.TmpFileAge)

	// Use find to locate and delete old files
	// -mindepth 1 to avoid deleting /tmp itself
	// -mtime +N to find files modified more than N days ago
	// -type f to only delete files, not directories
	// -delete to remove the files
	ageStr := fmt.Sprintf("+%d", r.config.TmpFileAge)
	output, err := r.diskExecutor.ExecuteCommand(ctx, "find", "/tmp", "-mindepth", "1", "-type", "f", "-mtime", ageStr, "-delete")
	if err != nil {
		return fmt.Errorf("tmp cleanup failed: %w (output: %s)", err, output)
	}

	r.logInfof("/tmp cleaned successfully")
	return nil
}

// cleanContainerLayers removes unused container layers.
func (r *DiskRemediator) cleanContainerLayers(ctx context.Context) error {
	r.logInfof("Cleaning unused container layers")

	// Use docker system prune to clean up unused container layers, networks, and build cache
	// -a removes all unused images, not just dangling ones
	// -f force without confirmation
	// --volumes also removes unused volumes
	output, err := r.diskExecutor.ExecuteCommand(ctx, "docker", "system", "prune", "-a", "-f", "--volumes")
	if err != nil {
		return fmt.Errorf("docker system prune failed: %w (output: %s)", err, output)
	}

	r.logInfof("Container layers cleaned: %s", output)
	return nil
}

// SetDiskExecutor sets a custom disk executor (useful for testing).
func (r *DiskRemediator) SetDiskExecutor(executor DiskExecutor) {
	r.diskExecutor = executor
}

// logInfof logs an informational message if a logger is configured.
func (r *DiskRemediator) logInfof(format string, args ...interface{}) {
	if r.logger != nil {
		r.logger.Infof("[disk-%s] "+format, append([]interface{}{r.config.Operation}, args...)...)
	}
}

// logWarnf logs a warning message if a logger is configured.
func (r *DiskRemediator) logWarnf(format string, args ...interface{}) {
	if r.logger != nil {
		r.logger.Warnf("[disk-%s] "+format, append([]interface{}{r.config.Operation}, args...)...)
	}
}
