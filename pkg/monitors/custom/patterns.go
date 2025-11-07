package custom

// DefaultLogPatterns provides sensible default patterns for common system issues
var DefaultLogPatterns = []LogPatternConfig{
	{
		Name:        "oom-killer",
		Regex:       "(Out of memory|Killed process \\d+|oom-killer|OOM killer)",
		Severity:    "error",
		Description: "Out of memory condition detected - kernel OOM killer activated",
		Source:      "both",
	},
	{
		Name:        "disk-io-error",
		Regex:       "(I/O error|Buffer I/O error|EXT4-fs.*error|XFS.*error|sd[a-z]+.*error|SCSI error|Medium Error|critical medium error)",
		Severity:    "error",
		Description: "Disk I/O error detected - possible hardware failure",
		Source:      "kmsg",
	},
	{
		Name:        "network-timeout",
		Regex:       "(NETDEV WATCHDOG.*transmit queue.*timed out|eth\\d+.*link down|connection timed out|No route to host|Network is unreachable)",
		Severity:    "warning",
		Description: "Network timeout or connectivity issue detected",
		Source:      "both",
	},
	{
		Name:        "kernel-panic",
		Regex:       "(kernel panic|Oops|BUG: unable to handle|general protection fault)",
		Severity:    "error",
		Description: "Kernel panic or critical system error detected",
		Source:      "kmsg",
	},
	{
		Name:        "memory-corruption",
		Regex:       "(bad page state|page allocation failure|segfault at)",
		Severity:    "error",
		Description: "Memory corruption or allocation failure detected",
		Source:      "kmsg",
	},
	{
		Name:        "device-failure",
		Regex:       "(USB disconnect|device descriptor read error|Failed to connect to device)",
		Severity:    "warning",
		Description: "Device connection or descriptor error",
		Source:      "kmsg",
	},
	{
		Name:        "filesystem-readonly",
		Regex:       "(Remounting filesystem read-only|EXT4-fs.*remounted.*read-only|XFS.*forcing shutdown)",
		Severity:    "error",
		Description: "Filesystem remounted read-only due to errors",
		Source:      "kmsg",
	},
	{
		Name:        "kubelet-error",
		Regex:       "(kubelet.*Failed|kubelet.*Error|kubelet.*failed to|PLEG is not healthy)",
		Severity:    "error",
		Description: "Kubelet error detected in systemd journal",
		Source:      "journal",
	},
	{
		Name:        "containerd-error",
		Regex:       "(containerd.*error|containerd.*failed|failed to start container)",
		Severity:    "error",
		Description: "Containerd error detected in systemd journal",
		Source:      "journal",
	},
	{
		Name:        "docker-error",
		Regex:       "(dockerd.*error|docker.*failed|Failed to start docker)",
		Severity:    "error",
		Description: "Docker daemon error detected in systemd journal",
		Source:      "journal",
	},
}

// GetDefaultPatterns returns a copy of the default patterns
func GetDefaultPatterns() []LogPatternConfig {
	patterns := make([]LogPatternConfig, len(DefaultLogPatterns))
	copy(patterns, DefaultLogPatterns)
	return patterns
}

// MergeWithDefaults merges user patterns with defaults, with user patterns taking precedence
func MergeWithDefaults(userPatterns []LogPatternConfig, useDefaults bool) []LogPatternConfig {
	if !useDefaults {
		return userPatterns
	}

	// Create a map of user pattern names for quick lookup
	userPatternMap := make(map[string]bool)
	for _, pattern := range userPatterns {
		userPatternMap[pattern.Name] = true
	}

	// Start with user patterns
	merged := make([]LogPatternConfig, 0, len(userPatterns)+len(DefaultLogPatterns))
	merged = append(merged, userPatterns...)

	// Add default patterns that aren't overridden by user
	for _, defaultPattern := range DefaultLogPatterns {
		if !userPatternMap[defaultPattern.Name] {
			merged = append(merged, defaultPattern)
		}
	}

	return merged
}
