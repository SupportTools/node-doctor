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

	// Verify all expected patterns are present (17 patterns total)
	expectedPatterns := map[string]bool{
		"oom-killer":                true,
		"disk-io-error":             true,
		"network-timeout":           true,
		"kernel-panic":              true,
		"memory-corruption":         true,
		"device-failure":            true,
		"filesystem-readonly":       true,
		"kubelet-error":             true,
		"containerd-error":          true,
		"docker-error":              true,
		"cpu-thermal-throttle":      true,
		"nfs-server-timeout":        true,
		"nfs-stale-filehandle":      true,
		"numa-balancing-failure":    true,
		"hardware-mce-error":        true,
		"edac-uncorrectable-error":  true,
		"edac-correctable-error":    true,
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

	// Verify modifying the returned slice doesn't affect the original
	originalLen := len(DefaultLogPatterns)
	patterns[0].Name = "modified-pattern"
	if DefaultLogPatterns[0].Name == "modified-pattern" {
		t.Error("Modifying returned slice affected DefaultLogPatterns")
	}
	if len(DefaultLogPatterns) != originalLen {
		t.Error("DefaultLogPatterns length changed after GetDefaultPatterns() call")
	}
}

// TestMergeWithDefaults tests the MergeWithDefaults function.
func TestMergeWithDefaults(t *testing.T) {
	tests := []struct {
		name         string
		userPatterns []LogPatternConfig
		useDefaults  bool
		wantCount    int
		checkNames   []string
	}{
		{
			name:         "use defaults only",
			userPatterns: []LogPatternConfig{},
			useDefaults:  true,
			wantCount:    len(DefaultLogPatterns),
			checkNames:   []string{"oom-killer", "disk-io-error"},
		},
		{
			name: "user patterns only",
			userPatterns: []LogPatternConfig{
				{Name: "custom-pattern-1", Regex: "test1"},
				{Name: "custom-pattern-2", Regex: "test2"},
			},
			useDefaults: false,
			wantCount:   2,
			checkNames:  []string{"custom-pattern-1", "custom-pattern-2"},
		},
		{
			name: "merge user and defaults",
			userPatterns: []LogPatternConfig{
				{Name: "custom-pattern-1", Regex: "test1"},
			},
			useDefaults: true,
			wantCount:   len(DefaultLogPatterns) + 1,
			checkNames:  []string{"custom-pattern-1", "oom-killer"},
		},
		{
			name: "user pattern overrides default",
			userPatterns: []LogPatternConfig{
				{Name: "oom-killer", Regex: "custom-oom-regex"},
			},
			useDefaults: true,
			wantCount:   len(DefaultLogPatterns),
			checkNames:  []string{"oom-killer"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			merged := MergeWithDefaults(tt.userPatterns, tt.useDefaults)

			if len(merged) != tt.wantCount {
				t.Errorf("MergeWithDefaults() returned %d patterns, want %d", len(merged), tt.wantCount)
			}

			// Check that expected names are present
			foundNames := make(map[string]LogPatternConfig)
			for _, pattern := range merged {
				foundNames[pattern.Name] = pattern
			}

			for _, name := range tt.checkNames {
				if _, found := foundNames[name]; !found {
					t.Errorf("MergeWithDefaults() missing expected pattern: %s", name)
				}
			}

			// For override test, verify user pattern took precedence
			if tt.name == "user pattern overrides default" {
				if pattern, found := foundNames["oom-killer"]; found {
					if pattern.Regex != "custom-oom-regex" {
						t.Errorf("User pattern did not override default, got regex: %s", pattern.Regex)
					}
				}
			}
		})
	}
}

// TestPatternAccuracy tests that patterns match expected log lines and don't match benign ones
func TestPatternAccuracy(t *testing.T) {
	tests := []struct {
		patternName     string
		shouldMatch     []string
		shouldNotMatch  []string
	}{
		{
			patternName: "cpu-thermal-throttle",
			shouldMatch: []string{
				"mce: CPU0: Package temperature/speed high, cpu clock throttled",
				"thermal_sys: Throttling enabled on Processor 0",
				"CPU0 is throttled",
			},
			shouldNotMatch: []string{
				"cpufreq: Setting CPU frequency to 1200 MHz",
				"mce: CPU0: Thermal monitoring enabled (TM1)",
				"CPU frequency scaling active",
			},
		},
		{
			patternName: "nfs-server-timeout",
			shouldMatch: []string{
				"nfs: server myserver not responding, timed out",
				"nfs: Timeout waiting for server response",
				"nfs: RPC call timeout detected",
			},
			shouldNotMatch: []string{
				"nfs: Server myserver OK",
				"nfs: Successfully mounted /mnt/nfs",
			},
		},
		{
			patternName: "nfs-stale-filehandle",
			shouldMatch: []string{
				"nfs: Stale file handle for /mnt/nfs/path",
				"Clearing 0x00100000 (NFS_STALE_INODE) inode",
				"NFS_STALE error detected",
			},
			shouldNotMatch: []string{
				"nfs: Successfully accessed file handle",
				"nfs: File handle cache hit",
			},
		},
		{
			patternName: "numa-balancing-failure",
			shouldMatch: []string{
				"numa: NUMA balancing failed: cannot migrate page",
				"numa: memory pressure high, consider page migration",
				"numa: balancing: process 12345 has high remote memory access",
				"numa: balancing disabled due to high CPU overhead",
			},
			shouldNotMatch: []string{
				"numa: page migration: 4096 pages migrated from node 0 to node 1",
				"numa: automatic page migration: 16777216 bytes migrated",
			},
		},
		{
			patternName: "hardware-mce-error",
			shouldMatch: []string{
				"mce: [Hardware Error]: Machine Check from unknown source",
				"mce: [Hardware Error]: CPU0: L3 Cache Error",
				"mce: [Hardware Error]: Package temperature above threshold",
			},
			shouldNotMatch: []string{
				"mce: CPU0: Thermal monitoring enabled (TM1)",
				"mce: Machine check events logged",
			},
		},
		{
			patternName: "edac-uncorrectable-error",
			shouldMatch: []string{
				"EDAC sbridge MC0: UE page 0x7f45a0",
				"EDAC: Uncorrectable error (UE)",
				"EDAC MC0: HANDLING MCE MEMORY ERROR - FATAL",
				"EDAC: Memory page is poisoned",
			},
			shouldNotMatch: []string{
				"EDAC sbridge MC0: CE page 0x7f45a0, offset 0x123",
				"EDAC: Single-bit error corrected",
			},
		},
		{
			patternName: "edac-correctable-error",
			shouldMatch: []string{
				"EDAC sbridge MC0: CE page 0x7f45a0, offset 0x123",
				"EDAC: Single-bit error (EDAC)",
				"EDAC: correctable error detected",
				"EDAC: corrected memory error",
			},
			shouldNotMatch: []string{
				"EDAC sbridge MC0: UE page 0x7f45a0",
				"EDAC: Uncorrectable error (UE)",
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
			patternName: "kubelet-error",
			shouldMatch: []string{
				"kubelet: Failed to start container runtime",
				"kubelet: failed to start pod",
				"kubelet: crash detected in main loop",
				"kubelet: PLEG is not healthy",
				"kubelet: eviction manager failed",
			},
			shouldNotMatch: []string{
				"kubelet: Successfully started pod",
				"kubelet: Container running",
			},
		},
		{
			patternName: "containerd-error",
			shouldMatch: []string{
				"containerd: failed to start runtime",
				"containerd: crash in event handler",
				"containerd: panic in goroutine",
			},
			shouldNotMatch: []string{
				"containerd: warning: slow operation",
				"containerd: info: container started",
			},
		},
	}

	// Build pattern map for quick lookup
	patternMap := make(map[string]LogPatternConfig)
	for _, p := range DefaultLogPatterns {
		patternMap[p.Name] = p
	}

	for _, tt := range tests {
		t.Run(tt.patternName, func(t *testing.T) {
			pattern, found := patternMap[tt.patternName]
			if !found {
				t.Fatalf("Pattern %s not found in DefaultLogPatterns", tt.patternName)
			}

			// Test positive matches
			for _, logLine := range tt.shouldMatch {
				matched, err := regexp.MatchString(pattern.Regex, logLine)
				if err != nil {
					t.Fatalf("Invalid regex for pattern %s: %v", tt.patternName, err)
				}
				if !matched {
					t.Errorf("Pattern %s should match log line: %q", tt.patternName, logLine)
				}
			}

			// Test negative matches (should NOT match benign lines)
			for _, logLine := range tt.shouldNotMatch {
				matched, err := regexp.MatchString(pattern.Regex, logLine)
				if err != nil {
					t.Fatalf("Invalid regex for pattern %s: %v", tt.patternName, err)
				}
				if matched {
					t.Errorf("Pattern %s should NOT match log line: %q", tt.patternName, logLine)
				}
			}
		})
	}
}
