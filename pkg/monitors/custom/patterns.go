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
		Regex:       "(device descriptor read error|Failed to connect to device|USB.*(?:error|failed))",
		Severity:    "warning",
		Description: "Device descriptor or connection error (not normal disconnect)",
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
		Regex:       "(kubelet.*(?:Failed to|failed to start|crash|PLEG is not healthy|eviction manager.*failed))",
		Severity:    "error",
		Description: "Critical kubelet error - service may be degraded",
		Source:      "journal",
	},
	{
		Name:        "containerd-error",
		Regex:       "(containerd.*(?:failed to start|crash|panic))",
		Severity:    "error",
		Description: "Critical containerd error - service may be degraded",
		Source:      "journal",
	},
	{
		Name:        "docker-error",
		Regex:       "(dockerd.*error|docker.*failed|Failed to start docker)",
		Severity:    "error",
		Description: "Docker daemon error detected in systemd journal",
		Source:      "journal",
	},
	{
		Name:        "cpu-thermal-throttle",
		Regex:       "(Package temperature/speed (high|normal)|cpu clock throttled|thermal_sys.*Throttling|CPU\\d+.*throttled)",
		Severity:    "warning",
		Description: "CPU thermal throttling detected - possible thermal/cooling issue",
		Source:      "kmsg",
	},
	{
		Name:        "nfs-server-timeout",
		Regex:       "nfs.*(?:not responding|timed out|Timeout waiting|RPC.*timeout)",
		Severity:    "error",
		Description: "NFS server timeout - mount may hang or become unavailable",
		Source:      "kmsg",
	},
	{
		Name:        "nfs-stale-filehandle",
		Regex:       "(?:Stale file handle|NFS_STALE|Clearing.*NFS_STALE)",
		Severity:    "error",
		Description: "NFS stale file handle - filesystem inconsistency detected",
		Source:      "kmsg",
	},
	{
		Name:        "numa-balancing-failure",
		Regex:       "numa.*(?:balancing failed|memory pressure|high remote|imbalance|disabled)",
		Severity:    "warning",
		Description: "NUMA balancing issue detected - may impact memory performance",
		Source:      "kmsg",
	},
	{
		Name:        "hardware-mce-error",
		Regex:       "mce:.*\\[Hardware Error\\]",
		Severity:    "error",
		Description: "Machine Check Exception - hardware error detected",
		Source:      "kmsg",
	},
	{
		Name:        "edac-uncorrectable-error",
		Regex:       "EDAC.*(?:UE|Uncorrectable|FATAL|poisoned)",
		Severity:    "error",
		Description: "EDAC uncorrectable memory error - hardware failure",
		Source:      "kmsg",
	},
	{
		Name:        "edac-correctable-error",
		Regex:       "(?i)EDAC.*(?:\\bCE\\b|single-bit|\\bcorrectable\\b|\\bcorrected\\b)",
		Severity:    "warning",
		Description: "EDAC correctable memory error - monitor for degradation",
		Source:      "kmsg",
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
