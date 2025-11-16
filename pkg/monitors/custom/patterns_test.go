package custom

import (
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

	// Verify all expected patterns are present
	expectedPatterns := map[string]bool{
		"oom-killer":          true,
		"disk-io-error":       true,
		"network-timeout":     true,
		"kernel-panic":        true,
		"memory-corruption":   true,
		"device-failure":      true,
		"filesystem-readonly": true,
		"kubelet-error":       true,
		"containerd-error":    true,
		"docker-error":        true,
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
