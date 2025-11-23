package remediators

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Whitelisted paths for disk cleanup operations.
// These are the only paths allowed for automated cleanup to prevent
// accidental or malicious deletion of system files.
var allowedCleanupPaths = []string{
	"/tmp",
	"/var/tmp",
}

// Regular expressions for validation.
var (
	// interfaceNameRegex validates network interface names.
	// Matches: eth0, ens3, wlan0, docker0, br-abc123, veth1234, lo
	// Max length: 15 characters (IFNAMSIZ - 1 in Linux)
	// Must start with a letter to ensure valid interface naming
	interfaceNameRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,14}$`)

	// vacuumSizeRegex validates journalctl vacuum size format.
	// Matches: 1K, 500M, 2G, etc.
	// Rejects: 0K, -1M, 1.5G, K, etc.
	vacuumSizeRegex = regexp.MustCompile(`^[1-9][0-9]*[KMG]$`)
)

// validateDiskCleanupPath validates that a path is allowed for cleanup operations.
// This prevents path traversal attacks and accidental deletion of system files.
func validateDiskCleanupPath(path string) error {
	if path == "" {
		return fmt.Errorf("cleanup path cannot be empty")
	}

	// Resolve to absolute path to prevent relative path tricks
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to resolve absolute path: %w", err)
	}

	// Clean the path to remove any .., ., or redundant separators
	cleanPath := filepath.Clean(absPath)

	// Check if path is in whitelist
	for _, allowed := range allowedCleanupPaths {
		if cleanPath == allowed {
			return nil
		}
	}

	return fmt.Errorf("path not allowed for cleanup: %s (must be one of: %v)",
		cleanPath, allowedCleanupPaths)
}

// validateInterfaceName validates network interface name format.
// This prevents command injection via malicious interface names.
func validateInterfaceName(name string) error {
	if name == "" {
		return fmt.Errorf("interface name cannot be empty")
	}

	// Check for path separators (security check)
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("interface name cannot contain path separators: %s", name)
	}

	// Check for shell metacharacters (security check)
	if strings.ContainsAny(name, ";|&$`\"'<>(){}[]* \t\n") {
		return fmt.Errorf("interface name contains invalid characters: %s", name)
	}

	// Check against regex pattern
	if !interfaceNameRegex.MatchString(name) {
		return fmt.Errorf("invalid interface name format: %s (must match %s)",
			name, interfaceNameRegex.String())
	}

	return nil
}

// validateVacuumSize validates journalctl vacuum size format.
// This prevents command injection and ensures valid size specifications.
func validateVacuumSize(size string) error {
	if size == "" {
		return fmt.Errorf("vacuum size cannot be empty")
	}

	// Check against regex pattern
	if !vacuumSizeRegex.MatchString(size) {
		return fmt.Errorf("invalid vacuum size format: %s (must match pattern: <number>[KMG], e.g., 500M, 1G)", size)
	}

	return nil
}

// validateTmpFileAge validates the age parameter for tmp file cleanup.
// This ensures the age is non-negative to prevent unexpected behavior.
func validateTmpFileAge(age int) error {
	if age < 0 {
		return fmt.Errorf("tmp file age must be non-negative, got: %d", age)
	}

	return nil
}

// validateDiskOperation validates all parameters for a disk cleanup operation.
// This is called during configuration validation to catch issues early.
func validateDiskOperation(operation string, config *DiskConfig) error {
	switch operation {
	case "clean-tmp":
		// Validate the cleanup path
		if err := validateDiskCleanupPath("/tmp"); err != nil {
			return fmt.Errorf("clean-tmp path validation failed: %w", err)
		}
		// Validate file age parameter
		if err := validateTmpFileAge(config.TmpFileAge); err != nil {
			return fmt.Errorf("clean-tmp age validation failed: %w", err)
		}

	case "clean-journal-logs":
		// Validate vacuum size format
		if err := validateVacuumSize(config.JournalVacuumSize); err != nil {
			return fmt.Errorf("clean-journal-logs size validation failed: %w", err)
		}

	case "clean-docker-images":
		// No user-controllable parameters for this operation
		// Safe to execute as-is

	case "clean-container-layers":
		// No user-controllable parameters for this operation
		// Safe to execute as-is

	default:
		return fmt.Errorf("unknown disk operation: %s", operation)
	}

	return nil
}

// validateNetworkOperation validates all parameters for a network operation.
// This is called during configuration validation to catch issues early.
func validateNetworkOperation(operation string, config *NetworkConfig) error {
	switch operation {
	case "restart-interface":
		// Validate interface name format
		if err := validateInterfaceName(config.InterfaceName); err != nil {
			return fmt.Errorf("restart-interface validation failed: %w", err)
		}

	case "flush-dns":
		// No user-controllable parameters for this operation
		// Safe to execute as-is

	case "reset-routing":
		// No user-controllable parameters for this operation
		// Safe to execute as-is

	default:
		return fmt.Errorf("unknown network operation: %s", operation)
	}

	return nil
}
