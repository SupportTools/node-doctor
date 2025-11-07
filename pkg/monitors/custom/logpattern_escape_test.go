package custom

import (
	"testing"
)

// TestCalculateRepetitionDepth_EscapeSequences tests edge cases for escape sequence handling
func TestCalculateRepetitionDepth_EscapeSequences(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		wantDepth int
		note      string
	}{
		// Basic escape sequences
		{
			name:      "escaped opening paren",
			pattern:   `\(a+\)+`,
			wantDepth: 0,
			note:      "Escaped parens are literals, only outer + counts but it's not on a group",
		},
		{
			name:      "escaped closing paren",
			pattern:   `(a+\))+`,
			wantDepth: 1,
			note:      "Only one real group with quantifier",
		},
		{
			name:      "escaped backslash before paren",
			pattern:   `\\(a+)+`,
			wantDepth: 1,
			note:      `\\ is escaped backslash (literal \), so ( is real paren`,
		},
		{
			name:      "double escaped backslash before paren",
			pattern:   `\\\\(a+)+`,
			wantDepth: 1,
			note:      `\\\\ is two escaped backslashes, ( is real paren`,
		},
		{
			name:      "escaped backslash then escaped paren",
			pattern:   `\\\(a+)+`,
			wantDepth: 1,
			note:      `INVALID REGEX - but algorithm returns 1 (acceptable for malformed input)`,
		},

		// Character classes
		{
			name:      "parens inside character class",
			pattern:   `[()+]+`,
			wantDepth: 0,
			note:      "Parens inside [...] are literals, not grouping",
		},
		{
			name:      "escaped bracket before parens",
			pattern:   `\[a-z\](a+)+`,
			wantDepth: 1,
			note:      "Escaped brackets are literals, parens are real",
		},
		{
			name:      "character class with nested group",
			pattern:   `[a-z]+((b+)+)+`,
			wantDepth: 2,
			note:      "Character class is separate, nested groups are real",
		},
		{
			name:      "unclosed character class",
			pattern:   `[a-z(a+)+`,
			wantDepth: 0,
			note:      "INVALID REGEX - unclosed char class, algorithm treats rest as inside class (safer)",
		},

		// Complex escape sequences
		{
			name:      "escaped quantifier",
			pattern:   `(a)\+`,
			wantDepth: 0,
			note:      "Escaped + is literal, group not quantified",
		},
		{
			name:      "multiple escape levels",
			pattern:   `\\(\\(a+\\)+\\)+`,
			wantDepth: 2,
			note:      `\\ are literal backslashes, ( are real - creates nested quantified groups`,
		},
		{
			name:      "escaped digit class",
			pattern:   `\d+(\d+)+`,
			wantDepth: 1,
			note:      `\d is character class, parens are real`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateRepetitionDepth(tt.pattern)
			if got != tt.wantDepth {
				t.Errorf("calculateRepetitionDepth(%q) = %d, want %d\nNote: %s",
					tt.pattern, got, tt.wantDepth, tt.note)
			}
		})
	}
}

// TestValidateRegexSafety_EscapeEdgeCases tests validation with escape sequences
func TestValidateRegexSafety_EscapeEdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		wantError bool
		note      string
	}{
		{
			name:      "escaped parens should be safe",
			pattern:   `\(error\)`,
			wantError: false,
			note:      "Escaped parens are literals",
		},
		{
			name:      "real nested quantifiers should fail",
			pattern:   `((a+)+)+`,
			wantError: true,
			note:      "Depth 2 should fail",
		},
		{
			name:      "escaped backslash with nested quantifiers should fail",
			pattern:   `\\((a+)+)+`,
			wantError: true,
			note:      `\\ is literal, parens are real`,
		},
		{
			name:      "character class with parens should be safe",
			pattern:   `[()+]+`,
			wantError: false,
			note:      "Parens inside [...] are literals",
		},
		{
			name:      "escaped quantifier should be safe",
			pattern:   `(error)\+`,
			wantError: false,
			note:      `\+ is literal, group not quantified`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRegexSafety(tt.pattern)
			if (err != nil) != tt.wantError {
				t.Errorf("validateRegexSafety(%q) error = %v, wantError = %v\nNote: %s",
					tt.pattern, err, tt.wantError, tt.note)
			}
		})
	}
}
