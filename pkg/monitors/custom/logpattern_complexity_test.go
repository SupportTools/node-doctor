package custom

import (
	"testing"
)

// TestCalculateLengthScore tests the pattern length scoring function
func TestCalculateLengthScore(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		wantScore int
		wantRange string // For documentation
	}{
		{
			name:      "empty pattern",
			pattern:   "",
			wantScore: 0,
			wantRange: "0 chars",
		},
		{
			name:      "short pattern (50 chars)",
			pattern:   generatePattern(50),
			wantScore: 2, // 50 * 0.05 = 2.5 -> 2
			wantRange: "0-100 chars",
		},
		{
			name:      "100 char boundary",
			pattern:   generatePattern(100),
			wantScore: 5,
			wantRange: "0-100 chars",
		},
		{
			name:      "medium pattern (300 chars)",
			pattern:   generatePattern(300),
			wantScore: 10, // 5 + (200 * 0.025) = 10
			wantRange: "101-500 chars",
		},
		{
			name:      "500 char boundary",
			pattern:   generatePattern(500),
			wantScore: 15,
			wantRange: "101-500 chars",
		},
		{
			name:      "large pattern (750 chars)",
			pattern:   generatePattern(750),
			wantScore: 20, // 15 + (250 * 0.02) = 20
			wantRange: "501-1000 chars",
		},
		{
			name:      "at max length (1000 chars)",
			pattern:   generatePattern(1000),
			wantScore: 25,
			wantRange: "501-1000 chars",
		},
		{
			name:      "over max length (1500 chars)",
			pattern:   generatePattern(1500),
			wantScore: 25,
			wantRange: ">1000 chars",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateLengthScore(tt.pattern)
			if got != tt.wantScore {
				t.Errorf("calculateLengthScore() = %d, want %d (range: %s)",
					got, tt.wantScore, tt.wantRange)
			}
		})
	}
}

// TestCalculateDepthScore tests the repetition depth scoring function
func TestCalculateDepthScore(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		wantScore int
	}{
		{
			name:      "no quantifiers",
			pattern:   "abc",
			wantScore: 0,
		},
		{
			name:      "depth 1",
			pattern:   "(a)+",
			wantScore: 5,
		},
		{
			name:      "depth 2",
			pattern:   "((a)+)+",
			wantScore: 12,
		},
		{
			name:      "depth 3 (max safe)",
			pattern:   "(((a)+)+)+",
			wantScore: 20,
		},
		{
			name:      "depth 4 (dangerous)",
			pattern:   "((((a)+)+)+)+",
			wantScore: 30,
		},
		{
			name:      "depth 5 (very dangerous)",
			pattern:   "(((((a)+)+)+)+)+",
			wantScore: 30,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateDepthScore(tt.pattern)
			if got != tt.wantScore {
				t.Errorf("calculateDepthScore() = %d, want %d", got, tt.wantScore)
			}
		})
	}
}

// TestCalculateNestedQuantifierScore tests nested quantifier detection scoring
func TestCalculateNestedQuantifierScore(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		wantScore int
	}{
		{
			name:      "no nested quantifiers",
			pattern:   "error.*occurred",
			wantScore: 0,
		},
		{
			name:      "safe quantified group",
			pattern:   "(error: \\d+)+",
			wantScore: 0,
		},
		{
			name:      "nested star-plus",
			pattern:   "(.*)+",
			wantScore: 20,
		},
		{
			name:      "nested plus-plus",
			pattern:   "(.+)+",
			wantScore: 20,
		},
		{
			name:      "nested character class",
			pattern:   "([a-z]+)+",
			wantScore: 20,
		},
		{
			name:      "nested word chars",
			pattern:   `(\w+)+`,
			wantScore: 20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateNestedQuantifierScore(tt.pattern)
			if got != tt.wantScore {
				t.Errorf("calculateNestedQuantifierScore() = %d, want %d", got, tt.wantScore)
			}
		})
	}
}

// TestCalculateAdjacencyScore tests quantified adjacency detection scoring
func TestCalculateAdjacencyScore(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		wantScore int
	}{
		{
			name:      "no adjacency",
			pattern:   "error.*occurred",
			wantScore: 0,
		},
		{
			name:      "separated quantifiers",
			pattern:   "\\d+:\\d+",
			wantScore: 0,
		},
		{
			name:      "adjacent wildcards",
			pattern:   ".*.*",
			wantScore: 15,
		},
		{
			name:      "adjacent digits",
			pattern:   "\\d+\\d+",
			wantScore: 15,
		},
		{
			name:      "adjacent word chars",
			pattern:   "\\w+\\w+",
			wantScore: 15,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateAdjacencyScore(tt.pattern)
			if got != tt.wantScore {
				t.Errorf("calculateAdjacencyScore() = %d, want %d", got, tt.wantScore)
			}
		})
	}
}

// TestCalculateAlternationScore tests alternation branch counting and scoring
func TestCalculateAlternationScore(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		wantScore int
	}{
		{
			name:      "no alternation",
			pattern:   "error",
			wantScore: 0,
		},
		{
			name:      "2 branches (safe)",
			pattern:   "(error|warning)+",
			wantScore: 0,
		},
		{
			name:      "3 branches",
			pattern:   "(a|b|c)+",
			wantScore: 2,
		},
		{
			name:      "4 branches",
			pattern:   "(a|b|c|d)+",
			wantScore: 4,
		},
		{
			name:      "5 branches (threshold)",
			pattern:   "(a|b|c|d|e)+",
			wantScore: 6,
		},
		{
			name:      "6 branches",
			pattern:   "(a|b|c|d|e|f)+",
			wantScore: 8,
		},
		{
			name:      "7+ branches (dangerous)",
			pattern:   "(a|b|c|d|e|f|g)+",
			wantScore: 10,
		},
		{
			name:      "10 branches",
			pattern:   "(one|two|three|four|five|six|seven|eight|nine|ten)*",
			wantScore: 10,
		},
		{
			name:      "many branches without quantifier (safe)",
			pattern:   "(a|b|c|d|e|f|g|h|i|j)",
			wantScore: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateAlternationScore(tt.pattern)
			if got != tt.wantScore {
				t.Errorf("calculateAlternationScore() = %d, want %d", got, tt.wantScore)
			}
		})
	}
}

// TestCalculateRegexComplexity tests the main complexity scoring function
func TestCalculateRegexComplexity(t *testing.T) {
	tests := []struct {
		name         string
		pattern      string
		wantScoreMin int
		wantScoreMax int
		wantRange    string
		breakdown    string // For documentation
	}{
		// Low complexity (0-30)
		{
			name:         "simple literal",
			pattern:      "error",
			wantScoreMin: 0,
			wantScoreMax: 0,
			wantRange:    "low",
			breakdown:    "length:0 + depth:0 + nested:0 + adjacency:0 + alternation:0",
		},
		{
			name:         "simple pattern",
			pattern:      "error.*occurred",
			wantScoreMin: 0,
			wantScoreMax: 1,
			wantRange:    "low",
			breakdown:    "length:0 + depth:0 + nested:0 + adjacency:0 + alternation:0",
		},
		{
			name:         "safe alternation",
			pattern:      "(error|warning)",
			wantScoreMin: 0,
			wantScoreMax: 1,
			wantRange:    "low",
			breakdown:    "length:0 + depth:0 + nested:0 + adjacency:0 + alternation:0",
		},
		{
			name:         "safe quantified group",
			pattern:      "(error: \\d+)+",
			wantScoreMin: 5,
			wantScoreMax: 5,
			wantRange:    "low",
			breakdown:    "length:0 + depth:5 + nested:0 + adjacency:0 + alternation:0",
		},

		// Medium complexity (31-60)
		{
			name:         "medium with depth 2",
			pattern:      "((error)+)+",
			wantScoreMin: 12,
			wantScoreMax: 12,
			wantRange:    "low",
			breakdown:    "length:0 + depth:12 + nested:0 + adjacency:0 + alternation:0",
		},
		{
			name:         "long pattern (500 chars)",
			pattern:      generatePattern(500),
			wantScoreMin: 15,
			wantScoreMax: 15,
			wantRange:    "low",
			breakdown:    "length:15 + depth:0 + nested:0 + adjacency:0 + alternation:0",
		},
		{
			name:         "depth 3 + length",
			pattern:      "(((a)+)+)+" + generatePattern(100),
			wantScoreMin: 25,
			wantScoreMax: 25,
			wantRange:    "low",
			breakdown:    "length:5 + depth:20 + nested:0 + adjacency:0 + alternation:0",
		},

		// Medium complexity (31-60)
		{
			name:         "depth 4 (dangerous)",
			pattern:      "((((a)+)+)+)+",
			wantScoreMin: 30,
			wantScoreMax: 30,
			wantRange:    "low",
			breakdown:    "length:0 + depth:30 + nested:0 + adjacency:0 + alternation:0",
		},
		{
			name:         "quantified adjacency",
			pattern:      ".*.*",
			wantScoreMin: 15,
			wantScoreMax: 15,
			wantRange:    "low",
			breakdown:    "length:0 + depth:0 + nested:0 + adjacency:15 + alternation:0",
		},
		{
			name:         "nested quantifiers",
			pattern:      "(.*)+",
			wantScoreMin: 25,
			wantScoreMax: 25,
			wantRange:    "low",
			breakdown:    "length:0 + depth:5 + nested:20 + adjacency:0 + alternation:0",
		},
		{
			name:         "multiple factors",
			pattern:      generatePattern(500) + "((a)+)+",
			wantScoreMin: 27,
			wantScoreMax: 27,
			wantRange:    "low",
			breakdown:    "length:15 + depth:12 + nested:0 + adjacency:0 + alternation:0",
		},

		// Very high complexity (81-100)
		{
			name:         "nested + adjacency",
			pattern:      "(.*)+.*.*",
			wantScoreMin: 40,
			wantScoreMax: 40,
			wantRange:    "medium",
			breakdown:    "length:0 + depth:5 + nested:20 + adjacency:15 + alternation:0",
		},
		{
			name:         "all factors combined",
			pattern:      generatePattern(1000) + "((((a)+)+)+)+" + "(.*)+.*.*" + "(a|b|c|d|e|f|g)+",
			wantScoreMin: 100,
			wantScoreMax: 100,
			wantRange:    "very high",
			breakdown:    "length:25 + depth:30 + nested:20 + adjacency:15 + alternation:10",
		},

		// Real-world patterns (from defaults)
		{
			name:         "oom-killer pattern",
			pattern:      `(Out of memory|Killed process \d+|oom-killer|OOM killer)`,
			wantScoreMin: 0,
			wantScoreMax: 5,
			wantRange:    "low",
			breakdown:    "length:0-3 + depth:0 + nested:0 + adjacency:0 + alternation:0",
		},
		{
			name:         "disk-io-error pattern",
			pattern:      `(I/O error|Buffer I/O error|EXT4-fs.*error|XFS.*error|sd[a-z]+.*error|SCSI error|Medium Error|critical medium error)`,
			wantScoreMin: 0,
			wantScoreMax: 10,
			wantRange:    "low",
			breakdown:    "length:0-6 + depth:0 + nested:0 + adjacency:0 + alternation:8-10",
		},
		{
			name:         "driver-error pattern",
			pattern:      `(driver.*error|.*driver.*failed|device.*failed to initialize)`,
			wantScoreMin: 0,
			wantScoreMax: 5,
			wantRange:    "low",
			breakdown:    "length:0-3 + depth:0 + nested:0 + adjacency:0 + alternation:0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateRegexComplexity(tt.pattern)

			// Check if score is within expected range
			if got < tt.wantScoreMin || got > tt.wantScoreMax {
				t.Errorf("CalculateRegexComplexity() = %d, want range [%d-%d] (category: %s)\nBreakdown: %s",
					got, tt.wantScoreMin, tt.wantScoreMax, tt.wantRange, tt.breakdown)
			}

			// Verify range classification
			var gotRange string
			switch {
			case got <= 30:
				gotRange = "low"
			case got <= 60:
				gotRange = "medium"
			case got <= 80:
				gotRange = "high"
			default:
				gotRange = "very high"
			}

			if gotRange != tt.wantRange {
				t.Errorf("Score %d classified as %q, expected %q", got, gotRange, tt.wantRange)
			}
		})
	}
}

// TestComplexityCorrelation verifies that complexity scores correlate with validation failures
func TestComplexityCorrelation(t *testing.T) {
	tests := []struct {
		name           string
		pattern        string
		shouldFailSafe bool // Should fail validateRegexSafety?
		minComplexity  int  // Minimum expected complexity score
	}{
		{
			name:           "safe pattern should have low score",
			pattern:        "error.*occurred",
			shouldFailSafe: false,
			minComplexity:  0,
		},
		{
			name:           "nested quantifiers fail and score high",
			pattern:        "(.*)+",
			shouldFailSafe: true,
			minComplexity:  20,
		},
		{
			name:           "quantified adjacency fails and scores high",
			pattern:        ".*.*",
			shouldFailSafe: true,
			minComplexity:  15,
		},
		{
			name:           "depth 4 fails and scores high",
			pattern:        "((((a)+)+)+)+",
			shouldFailSafe: true,
			minComplexity:  30,
		},
		{
			name:           "excessive alternation fails and scores high",
			pattern:        "(a|b|c|d|e|f|g)+",
			shouldFailSafe: true,
			minComplexity:  10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			complexity := CalculateRegexComplexity(tt.pattern)
			err := validateRegexSafety(tt.pattern)

			if (err != nil) != tt.shouldFailSafe {
				t.Errorf("validateRegexSafety() error = %v, shouldFailSafe = %v", err, tt.shouldFailSafe)
			}

			if complexity < tt.minComplexity {
				t.Errorf("CalculateRegexComplexity() = %d, want >= %d for pattern that %s",
					complexity, tt.minComplexity,
					map[bool]string{true: "fails validation", false: "passes validation"}[tt.shouldFailSafe])
			}

			// Patterns that fail validation should generally have higher scores
			if tt.shouldFailSafe && complexity < 10 {
				t.Errorf("Pattern that fails validation has low complexity score %d", complexity)
			}
		})
	}
}
