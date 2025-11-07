package custom

import (
	"testing"
	"time"
)

// TestValidateRegexSafety_NestedQuantifiers tests detection of dangerous nested quantifier patterns
func TestValidateRegexSafety_NestedQuantifiers(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		wantErr bool
		errMsg  string
	}{
		// Basic nested quantifiers
		{
			name:    "nested star-plus (.*)+",
			pattern: "(.*)+",
			wantErr: true,
			errMsg:  "nested star-plus",
		},
		{
			name:    "nested plus-plus (.+)+",
			pattern: "(.+)+",
			wantErr: true,
			errMsg:  "nested plus-plus",
		},
		{
			name:    "nested star-star (.*)*",
			pattern: "(.*)*",
			wantErr: true,
			errMsg:  "nested star-star",
		},
		{
			name:    "nested plus-star (.+)*",
			pattern: "(.+)*",
			wantErr: true,
			errMsg:  "nested plus-star",
		},
		// Complex nested quantifiers
		{
			name:    "nested plus quantifiers (a+)+",
			pattern: "(a+)+",
			wantErr: true,
			errMsg:  "nested plus quantifiers",
		},
		{
			name:    "nested star-plus quantifiers (a*)+",
			pattern: "(a*)+",
			wantErr: true,
			errMsg:  "nested star-plus quantifiers",
		},
		{
			name:    "nested plus-star quantifiers (a+)*",
			pattern: "(a+)*",
			wantErr: true,
			errMsg:  "nested plus-star quantifiers",
		},
		{
			name:    "nested star quantifiers (a*)*",
			pattern: "(a*)*",
			wantErr: true,
			errMsg:  "nested star quantifiers",
		},
		{
			name:    "nested optional-plus (a?)+",
			pattern: "(a?)+",
			wantErr: true,
			errMsg:  "nested optional-plus",
		},
		{
			name:    "nested optional-star (a?)*",
			pattern: "(a?)*",
			wantErr: true,
			errMsg:  "nested optional-star",
		},
		// Complex patterns with nested quantifiers
		{
			name:    "complex nested pattern (\\w+)+",
			pattern: `(\w+)+`,
			wantErr: true,
			errMsg:  "nested plus quantifiers",
		},
		{
			name:    "complex nested pattern ([a-z]+)*",
			pattern: `([a-z]+)*`,
			wantErr: true,
			errMsg:  "nested character class plus-star",
		},
		// Safe patterns
		{
			name:    "safe simple pattern",
			pattern: "error.*occurred",
			wantErr: false,
		},
		{
			name:    "safe alternation",
			pattern: "(error|warning|info)",
			wantErr: false,
		},
		{
			name:    "safe quantified group",
			pattern: "(error: \\d+)+",
			wantErr: false,
		},
		{
			name:    "safe character class",
			pattern: "[a-z]+",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRegexSafety(tt.pattern)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateRegexSafety() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if err == nil || !contains(err.Error(), tt.errMsg) {
					t.Errorf("validateRegexSafety() error = %v, want error containing %q", err, tt.errMsg)
				}
			}
		})
	}
}

// TestValidateRegexSafety_QuantifiedAdjacency tests detection of quantified adjacency patterns
func TestValidateRegexSafety_QuantifiedAdjacency(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		wantErr bool
	}{
		// Dangerous quantified adjacencies
		{
			name:    "adjacent wildcards .*.*",
			pattern: ".*.*",
			wantErr: true,
		},
		{
			name:    "adjacent plus wildcards .+.+",
			pattern: ".+.+",
			wantErr: true,
		},
		{
			name:    "adjacent digits \\d+\\d+",
			pattern: `\d+\d+`,
			wantErr: true,
		},
		{
			name:    "adjacent word chars \\w+\\w+",
			pattern: `\w+\w+`,
			wantErr: true,
		},
		{
			name:    "adjacent whitespace \\s+\\s+",
			pattern: `\s+\s+`,
			wantErr: true,
		},
		{
			name:    "adjacent character classes [a-z]+[a-z]+",
			pattern: `[a-z]+[a-z]+`,
			wantErr: true,
		},
		{
			name:    "adjacent star digits \\d*\\d*",
			pattern: `\d*\d*`,
			wantErr: true,
		},
		// Safe patterns (separated by other characters)
		{
			name:    "safe digits separated \\d+abc\\d+",
			pattern: `\d+abc\d+`,
			wantErr: false,
		},
		{
			name:    "safe wildcards separated .*abc.*",
			pattern: ".*abc.*",
			wantErr: false,
		},
		{
			name:    "safe word chars separated \\w+ \\w+",
			pattern: `\w+ \w+`,
			wantErr: false,
		},
		{
			name:    "safe character classes separated [a-z]+123[a-z]+",
			pattern: `[a-z]+123[a-z]+`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRegexSafety(tt.pattern)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateRegexSafety() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidateRegexSafety_RepetitionDepth tests detection of excessive repetition nesting
func TestValidateRegexSafety_RepetitionDepth(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		wantErr bool
	}{
		// Excessive repetition depth (> 3)
		{
			name:    "depth 4: ((((a)+)+)+)+",
			pattern: "((((a)+)+)+)+",
			wantErr: true,
		},
		{
			name:    "depth 5: (((((a)+)+)+)+)+",
			pattern: "(((((a)+)+)+)+)+",
			wantErr: true,
		},
		// Acceptable repetition depth (<= 3)
		{
			name:    "depth 1: (a)+",
			pattern: "(a)+",
			wantErr: false,
		},
		{
			name:    "depth 2: ((a)+)+",
			pattern: "((a)+)+",
			wantErr: false,
		},
		{
			name:    "depth 3: (((a)+)+)+",
			pattern: "(((a)+)+)+",
			wantErr: false,
		},
		{
			name:    "depth 0: abc",
			pattern: "abc",
			wantErr: false,
		},
		// Real-world patterns
		{
			name:    "safe real-world pattern",
			pattern: `(error|warning): \d+`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRegexSafety(tt.pattern)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateRegexSafety() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidateRegexSafety_ExponentialAlternation tests detection of exponential alternation growth
func TestValidateRegexSafety_ExponentialAlternation(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		wantErr bool
	}{
		// Dangerous: too many alternations (> 5)
		{
			name:    "7 branches under quantifier",
			pattern: "(a|b|c|d|e|f|g)+",
			wantErr: true,
		},
		{
			name:    "10 branches under quantifier",
			pattern: "(one|two|three|four|five|six|seven|eight|nine|ten)*",
			wantErr: true,
		},
		// Safe: <= 5 branches
		{
			name:    "2 branches",
			pattern: "(error|warning)+",
			wantErr: false,
		},
		{
			name:    "5 branches",
			pattern: "(a|b|c|d|e)+",
			wantErr: false,
		},
		{
			name:    "3 branches",
			pattern: "(foo|bar|baz)*",
			wantErr: false,
		},
		// Safe: many branches but no quantifier
		{
			name:    "10 branches no quantifier",
			pattern: "(a|b|c|d|e|f|g|h|i|j)",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRegexSafety(tt.pattern)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateRegexSafety() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidateRegexSafety_PatternLength tests pattern length limits
func TestValidateRegexSafety_PatternLength(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		wantErr bool
	}{
		{
			name:    "pattern at max length (1000)",
			pattern: generatePattern(1000),
			wantErr: false,
		},
		{
			name:    "pattern exceeds max length (1001)",
			pattern: generatePattern(1001),
			wantErr: true,
		},
		{
			name:    "short pattern",
			pattern: "error",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRegexSafety(tt.pattern)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateRegexSafety() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidateRegexSafety_RealWorldPatterns tests actual patterns from the codebase
func TestValidateRegexSafety_RealWorldPatterns(t *testing.T) {
	// These are real patterns from the default patterns (should all be safe)
	realWorldPatterns := []struct {
		name    string
		pattern string
	}{
		{
			name:    "oom-killer pattern",
			pattern: `(Out of memory|Killed process \d+|oom-killer|OOM killer)`,
		},
		{
			name:    "disk-io-error pattern",
			pattern: `(I/O error|Buffer I/O error|EXT4-fs.*error|XFS.*error|sd[a-z]+.*error|SCSI error|Medium Error|critical medium error)`,
		},
		{
			name:    "kernel-panic pattern",
			pattern: `(Kernel panic|BUG: unable to handle|general protection fault|Oops:)`,
		},
		{
			name:    "memory-corruption pattern",
			pattern: `(Memory corruption|slab error|Bad page state|Corrupted low memory)`,
		},
		{
			name:    "filesystem-corruption pattern",
			pattern: `(Filesystem corruption|ext4_lookup|XFS.*corruption|corrupted size vs\. prev_size)`,
		},
		{
			name:    "network-timeout pattern",
			pattern: `(NETDEV WATCHDOG.*transmit queue.*timed out|network timeout|connection timed out)`,
		},
		{
			name:    "hardware-error pattern",
			pattern: `(Hardware Error|Machine Check Exception|CPU.*error|MCE:)`,
		},
		{
			name:    "driver-error pattern",
			pattern: `(driver.*error|.*driver.*failed|device.*failed to initialize)`,
		},
		{
			name:    "systemd-failed pattern",
			pattern: `(systemd.*failed|Failed to start|Service.*failed)`,
		},
		{
			name:    "container-error pattern",
			pattern: `(containerd.*error|docker.*error|runc.*error|container.*failed)`,
		},
	}

	for _, tt := range realWorldPatterns {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRegexSafety(tt.pattern)
			if err != nil {
				t.Errorf("Real-world pattern %q should be safe but got error: %v", tt.pattern, err)
			}
		})
	}
}

// TestValidateRegexSafety_MaliciousPatterns tests known malicious ReDoS patterns
func TestValidateRegexSafety_MaliciousPatterns(t *testing.T) {
	maliciousPatterns := []struct {
		name    string
		pattern string
	}{
		{
			name:    "classic ReDoS (a+)+",
			pattern: "(a+)+",
		},
		{
			name:    "classic ReDoS (a*)*",
			pattern: "(a*)*",
		},
		{
			name:    "email ReDoS",
			pattern: `([a-zA-Z0-9]+)*@([a-zA-Z0-9]+)*`,
		},
		{
			name:    "wildcard explosion",
			pattern: ".*.*.*",
		},
		{
			name:    "digit explosion",
			pattern: `\d+\d+\d+`,
		},
		{
			name:    "word char explosion",
			pattern: `\w+\w+`,
		},
	}

	for _, tt := range maliciousPatterns {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRegexSafety(tt.pattern)
			if err == nil {
				t.Errorf("Malicious pattern %q should be rejected but was accepted", tt.pattern)
			}
		})
	}
}

// TestCalculateRepetitionDepth tests the repetition depth calculation function
func TestCalculateRepetitionDepth(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		wantDepth int
	}{
		{
			name:      "depth 0: no quantifiers",
			pattern:   "abc",
			wantDepth: 0,
		},
		{
			name:      "depth 0: single quantifier not in group",
			pattern:   "a+",
			wantDepth: 0,
		},
		{
			name:      "depth 1: single quantified group",
			pattern:   "(abc)+",
			wantDepth: 1,
		},
		{
			name:      "depth 2: nested quantified groups",
			pattern:   "((a)+)+",
			wantDepth: 2,
		},
		{
			name:      "depth 3: triple nested quantified groups",
			pattern:   "(((a)+)+)+",
			wantDepth: 3,
		},
		{
			name:      "depth 4: quad nested quantified groups",
			pattern:   "((((a)+)+)+)+",
			wantDepth: 4,
		},
		{
			name:      "depth 1: quantifier inside group doesn't count",
			pattern:   `(error: \d+)+`,
			wantDepth: 1, // Only the outer group is quantified
		},
		{
			name:      "depth 0: alternation with wildcards",
			pattern:   `(driver.*error|.*driver.*failed)`,
			wantDepth: 0, // Group not quantified
		},
		{
			name:      "depth 1: quantified alternation with wildcards",
			pattern:   `(driver.*error|.*driver.*failed)+`,
			wantDepth: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateRepetitionDepth(tt.pattern)
			if got != tt.wantDepth {
				t.Errorf("calculateRepetitionDepth(%q) = %d, want %d", tt.pattern, got, tt.wantDepth)
			}
		})
	}
}

// TestHasQuantifiedAdjacency tests the quantified adjacency detection function
func TestHasQuantifiedAdjacency(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    bool
	}{
		// Positive cases (has adjacency)
		{
			name:    ".*.*",
			pattern: ".*.*",
			want:    true,
		},
		{
			name:    ".+.+",
			pattern: ".+.+",
			want:    true,
		},
		{
			name:    `\d+\d+`,
			pattern: `\d+\d+`,
			want:    true,
		},
		{
			name:    `\w+\w+`,
			pattern: `\w+\w+`,
			want:    true,
		},
		// Negative cases (no adjacency)
		{
			name:    "separated wildcards",
			pattern: ".*abc.*",
			want:    false,
		},
		{
			name:    "separated digits",
			pattern: `\d+:\d+`,
			want:    false,
		},
		{
			name:    "simple pattern",
			pattern: "error",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasQuantifiedAdjacency(tt.pattern)
			if got != tt.want {
				t.Errorf("hasQuantifiedAdjacency(%q) = %v, want %v", tt.pattern, got, tt.want)
			}
		})
	}
}

// TestCompileWithTimeout tests the regex compilation timeout mechanism
func TestCompileWithTimeout(t *testing.T) {
	tests := []struct {
		name        string
		pattern     string
		timeout     time.Duration
		wantErr     bool
		errContains string
	}{
		{
			name:    "simple pattern compiles quickly",
			pattern: "error|warning|info",
			timeout: 100 * time.Millisecond,
			wantErr: false,
		},
		{
			name:    "complex but valid pattern",
			pattern: `(Out of memory|Killed process \d+|oom-killer|OOM killer)`,
			timeout: 100 * time.Millisecond,
			wantErr: false,
		},
		{
			name:        "invalid regex syntax",
			pattern:     "[invalid(",
			timeout:     100 * time.Millisecond,
			wantErr:     true,
			errContains: "error parsing regexp",
		},
		{
			name:    "very long but simple pattern",
			pattern: generatePattern(500),
			timeout: 100 * time.Millisecond,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiled, err := compileWithTimeout(tt.pattern, tt.timeout)

			if tt.wantErr {
				if err == nil {
					t.Errorf("compileWithTimeout() expected error but got none")
					return
				}
				if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("compileWithTimeout() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("compileWithTimeout() unexpected error = %v", err)
				return
			}

			if compiled == nil {
				t.Errorf("compileWithTimeout() returned nil compiled regex without error")
			}
		})
	}
}

// TestCompileWithTimeout_Integration tests timeout integration with applyDefaults
func TestCompileWithTimeout_Integration(t *testing.T) {
	tests := []struct {
		name    string
		config  *LogPatternMonitorConfig
		wantErr bool
	}{
		{
			name: "all patterns compile successfully",
			config: &LogPatternMonitorConfig{
				Patterns: []LogPatternConfig{
					{
						Name:   "test1",
						Regex:  "error",
						Source: "kmsg",
					},
					{
						Name:   "test2",
						Regex:  "(warning|info)",
						Source: "journal",
					},
				},
				CheckKmsg:    true,
				CheckJournal: true,
				JournalUnits: []string{"kubelet.service"},
			},
			wantErr: false,
		},
		{
			name: "invalid pattern fails with clear error",
			config: &LogPatternMonitorConfig{
				Patterns: []LogPatternConfig{
					{
						Name:   "invalid",
						Regex:  "[unclosed",
						Source: "kmsg",
					},
				},
				CheckKmsg: true,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.applyDefaults()
			if (err != nil) != tt.wantErr {
				t.Errorf("applyDefaults() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// Helper functions

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func generatePattern(length int) string {
	pattern := ""
	for i := 0; i < length; i++ {
		pattern += "a"
	}
	return pattern
}
