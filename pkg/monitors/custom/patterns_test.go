package custom

import (
	"regexp"
	"testing"
)

// TestGetDefaultPatterns tests the GetDefaultPatterns function.
func TestGetDefaultPatterns(t *testing.T) {
	patterns := GetDefaultPatterns()

	// Verify we get patterns back
	if len(patterns) == 0 {
		t.Error("GetDefaultPatterns() returned empty slice, expected default patterns")
	}

	// Verify it's a copy (not the same slice)
	if &patterns[0] == &DefaultLogPatterns[0] {
		t.Error("GetDefaultPatterns() returned same slice, expected a copy")
	}

	// Verify all expected patterns are present (28 patterns total: 7 original + 12 kubelet + 9 others)
	expectedPatterns := map[string]bool{
		"oom-killer":                    true,
		"disk-io-error":                 true,
		"network-timeout":               true,
		"kernel-panic":                  true,
		"memory-corruption":             true,
		"device-failure":                true,
		"filesystem-readonly":           true,
		// 12 kubelet patterns
		"kubelet-oom":                   true,
		"kubelet-pleg-unhealthy":        true,
		"kubelet-eviction":              true,
		"kubelet-disk-pressure":         true,
		"kubelet-memory-pressure":       true,
		"kubelet-image-pull-failed":     true,
		"kubelet-cni-error":             true,
		"kubelet-runtime-error":         true,
		"kubelet-certificate-rotation":  true,
		"kubelet-node-not-ready":        true,
		"kubelet-api-connection-error":  true,
		"kubelet-volume-mount-error":    true,
		// Other patterns
		"containerd-error":              true,
		"docker-error":                  true,
		"cpu-thermal-throttle":          true,
		"nfs-server-timeout":            true,
		"nfs-stale-filehandle":          true,
		"numa-balancing-failure":        true,
		"hardware-mce-error":            true,
		"edac-uncorrectable-error":      true,
		"edac-correctable-error":        true,
	}

	foundPatterns := make(map[string]bool)
	for _, pattern := range patterns {
		foundPatterns[pattern.Name] = true
	}

	for expected := range expectedPatterns {
		if !foundPatterns[expected] {
			t.Errorf("GetDefaultPatterns() missing expected pattern: %s", expected)
		}
	}

	// Verify we have the right number of patterns
	if len(patterns) != len(expectedPatterns) {
		t.Errorf("GetDefaultPatterns() returned %d patterns, expected %d", len(patterns), len(expectedPatterns))
	}
}

// TestMergeWithDefaults tests the MergeWithDefaults function.
func TestMergeWithDefaults(t *testing.T) {
	tests := []struct {
		name         string
		userPatterns []LogPatternConfig
		useDefaults  bool
		expected     int // Expected number of patterns in result
	}{
		{
			name:         "no user patterns, no defaults",
			userPatterns: []LogPatternConfig{},
			useDefaults:  false,
			expected:     0,
		},
		{
			name:         "no user patterns, with defaults",
			userPatterns: []LogPatternConfig{},
			useDefaults:  true,
			expected:     28, // All default patterns (updated count)
		},
		{
			name: "one user pattern, no defaults",
			userPatterns: []LogPatternConfig{
				{Name: "custom-pattern", Regex: "test", Severity: "info"},
			},
			useDefaults: false,
			expected:    1,
		},
		{
			name: "one user pattern, with defaults",
			userPatterns: []LogPatternConfig{
				{Name: "custom-pattern", Regex: "test", Severity: "info"},
			},
			useDefaults: true,
			expected:    29, // 1 user + 28 defaults
		},
		{
			name: "user pattern overrides default",
			userPatterns: []LogPatternConfig{
				{Name: "oom-killer", Regex: "custom-oom", Severity: "info"}, // Override default
			},
			useDefaults: true,
			expected:    28, // Still 28 total (user pattern replaces default)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MergeWithDefaults(tt.userPatterns, tt.useDefaults)

			if len(result) != tt.expected {
				t.Errorf("MergeWithDefaults() returned %d patterns, expected %d", len(result), tt.expected)
			}

			// Verify no duplicates
			names := make(map[string]bool)
			for _, pattern := range result {
				if names[pattern.Name] {
					t.Errorf("MergeWithDefaults() resulted in duplicate pattern: %s", pattern.Name)
				}
				names[pattern.Name] = true
			}
		})
	}
}

// TestPatternAccuracy tests a subset of patterns to ensure they match expected strings
func TestPatternAccuracy(t *testing.T) {
	tests := []struct {
		patternName    string
		shouldMatch    []string
		shouldNotMatch []string
	}{
		{
			patternName: "oom-killer",
			shouldMatch: []string{
				"Out of memory: Kill process 1234 (test)",
				"Killed process 5678 (chrome)",
				"oom-killer: Task in /system.slice killed as a result of limit of /system.slice",
				"OOM killer activated",
			},
			shouldNotMatch: []string{
				"Process started normally",
				"Memory allocation successful",
			},
		},
		{
			patternName: "disk-io-error",
			shouldMatch: []string{
				"Buffer I/O error on device sda1",
				"EXT4-fs error (device sda1): ext4_lookup",
				"XFS error: failed to read directory",
				"sda: I/O error, dev sda, sector 12345",
				"SCSI error : <0 0 0 0> return code = 0x8000002",
			},
			shouldNotMatch: []string{
				"Disk operation completed successfully",
				"EXT4-fs: mounted filesystem with ordered data mode",
			},
		},
		{
			patternName: "network-timeout",
			shouldMatch: []string{
				"NETDEV WATCHDOG: eth0 (e1000e): transmit queue 0 timed out",
				"eth0: link down",
				"connect() failed: connection timed out",
				"No route to host",
				"Network is unreachable",
			},
			shouldNotMatch: []string{
				"eth0: link up",
				"Network connection established",
			},
		},
		{
			patternName: "kernel-panic",
			shouldMatch: []string{
				"kernel panic - not syncing: VFS: Unable to mount root fs",
				"Oops: 0000 [#1] SMP",
				"BUG: unable to handle kernel NULL pointer dereference",
				"general protection fault: 0000 [#1] PREEMPT SMP",
			},
			shouldNotMatch: []string{
				"Kernel started successfully",
				"System boot completed",
			},
		},
		{
			patternName: "device-failure",
			shouldMatch: []string{
				"USB device descriptor read error",
				"Failed to connect to device",
				"USB device error detected",
				"USB failed to enumerate",
			},
			shouldNotMatch: []string{
				"USB disconnect, device number 2",  // Normal disconnect
				"USB device connected successfully",
			},
		},
		{
			patternName: "kubelet-oom",
			shouldMatch: []string{
				"kubelet: OOM killer activated for container",
				"kubelet: container out of memory condition detected",
				"kubelet: memory cgroup out of memory",
			},
			shouldNotMatch: []string{
				"containerd: OOM condition detected", // Not kubelet
				"kubelet: memory usage normal",
			},
		},
		{
			patternName: "kubelet-pleg-unhealthy",
			shouldMatch: []string{
				"PLEG is not healthy: pleg has yet to be successful",
				"relist operation taking too long: 5.2s",
			},
			shouldNotMatch: []string{
				"PLEG operation completed successfully",
				"relist completed in 100ms",
			},
		},
	}

	// Get default patterns and create a map
	defaultPatterns := GetDefaultPatterns()
	patternMap := make(map[string]LogPatternConfig)
	for _, pattern := range defaultPatterns {
		patternMap[pattern.Name] = pattern
	}

	for _, test := range tests {
		t.Run(test.patternName, func(t *testing.T) {
			pattern, exists := patternMap[test.patternName]
			if !exists {
				t.Fatalf("Pattern %s not found in DefaultLogPatterns", test.patternName)
			}

			// Compile the regex
			regex, err := regexp.Compile(pattern.Regex)
			if err != nil {
				t.Fatalf("Failed to compile regex for pattern %s: %v", test.patternName, err)
			}

			// Test positive matches
			for _, should := range test.shouldMatch {
				if !regex.MatchString(should) {
					t.Errorf("Pattern %s should match: %q", test.patternName, should)
				}
			}

			// Test negative matches
			for _, shouldNot := range test.shouldNotMatch {
				if regex.MatchString(shouldNot) {
					t.Errorf("Pattern %s should NOT match: %q", test.patternName, shouldNot)
				}
			}
		})
	}
}

// TestPatternValidation tests that all default patterns compile correctly
func TestPatternValidation(t *testing.T) {
	patterns := GetDefaultPatterns()

	for _, pattern := range patterns {
		t.Run(pattern.Name, func(t *testing.T) {
			// Test regex compilation
			_, err := regexp.Compile(pattern.Regex)
			if err != nil {
				t.Errorf("Pattern %s has invalid regex: %v", pattern.Name, err)
			}

			// Test required fields
			if pattern.Name == "" {
				t.Error("Pattern has empty Name")
			}
			if pattern.Regex == "" {
				t.Error("Pattern has empty Regex")
			}
			if pattern.Severity == "" {
				t.Error("Pattern has empty Severity")
			}
			if pattern.Description == "" {
				t.Error("Pattern has empty Description")
			}

			// Test valid severity values
			validSeverities := []string{"error", "warning", "info"}
			validSeverity := false
			for _, valid := range validSeverities {
				if pattern.Severity == valid {
					validSeverity = true
					break
				}
			}
			if !validSeverity {
				t.Errorf("Pattern %s has invalid severity: %s", pattern.Name, pattern.Severity)
			}

			// Test valid source values
			validSources := []string{"kmsg", "journal", "both"}
			validSource := false
			for _, valid := range validSources {
				if pattern.Source == valid {
					validSource = true
					break
				}
			}
			if !validSource {
				t.Errorf("Pattern %s has invalid source: %s", pattern.Name, pattern.Source)
			}
		})
	}
}