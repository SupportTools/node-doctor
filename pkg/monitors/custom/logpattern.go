package custom

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/supporttools/node-doctor/pkg/monitors"
	"github.com/supporttools/node-doctor/pkg/types"
)

const (
	// Maximum buffer size for kmsg reads to prevent memory exhaustion
	maxKmsgBufferSize = 1 * 1024 * 1024 // 1MB

	// Default configuration values
	defaultKmsgPath            = "/dev/kmsg"
	defaultMaxEventsPerPattern = 10
	defaultDedupWindow         = 5 * time.Minute

	// Regex safety limits to prevent ReDoS attacks
	maxRegexLength = 1000 // Maximum allowed regex pattern length

	// Resource limits to prevent unbounded memory usage and DoS attacks
	maxConfiguredPatterns  = 60              // Maximum number of patterns (user + defaults)
	maxJournalUnits        = 20              // Maximum number of journal units to monitor
	minMaxEventsPerPattern = 1               // Minimum events per pattern per window
	maxMaxEventsPerPattern = 1000            // Maximum events per pattern per window
	minDedupWindow         = 1 * time.Second // Minimum deduplication window
	maxDedupWindow         = 1 * time.Hour   // Maximum deduplication window

	// Memory estimation constants (approximate per-item memory usage in bytes)
	memoryPerPattern     = 512 // bytes: regex + compiled state + name + desc
	memoryPerJournalUnit = 256 // bytes: unit name + timestamp tracking
	memoryPerEventRecord = 128 // bytes: map entry overhead + timestamp

	// Overall memory limit per check (conservative estimate)
	maxMemoryPerCheck = 10 * 1024 * 1024 // 10MB total limit
)

// Pre-compiled regex patterns for performance optimization
// These are used in regex safety validation and complexity analysis
var (
	// Nested quantifier patterns (for checkDangerousPatterns)
	nestedQuantifierPatterns = []*regexp.Regexp{
		regexp.MustCompile(`\(\.\*\)\+`),         // (.*)+
		regexp.MustCompile(`\(\.\+\)\+`),         // (.+)+
		regexp.MustCompile(`\(\.\*\)\*`),         // (.*)*
		regexp.MustCompile(`\(\.\+\)\*`),         // (.+)*
		regexp.MustCompile(`\([a-zA-Z]\+\)\+`),   // (a+)+
		regexp.MustCompile(`\([a-zA-Z]\*\)\+`),   // (a*)+
		regexp.MustCompile(`\([a-zA-Z]\+\)\*`),   // (a+)*
		regexp.MustCompile(`\([a-zA-Z]\*\)\*`),   // (a*)*
		regexp.MustCompile(`\([a-zA-Z]\?\)\+`),   // (a?)+
		regexp.MustCompile(`\([a-zA-Z]\?\)\*`),   // (a?)*
		regexp.MustCompile(`\(\\[wdWDs]\+\)\+`),  // (\w+)+
		regexp.MustCompile(`\(\\[wdWDs]\*\)\+`),  // (\w*)+
		regexp.MustCompile(`\(\\[wdWDs]\+\)\*`),  // (\w+)*
		regexp.MustCompile(`\(\\[wdWDs]\*\)\*`),  // (\w*)*
		regexp.MustCompile(`\(\[[^\]]+\]\+\)\+`), // ([a-z]+)+
		regexp.MustCompile(`\(\[[^\]]+\]\*\)\+`), // ([a-z]*)+
		regexp.MustCompile(`\(\[[^\]]+\]\+\)\*`), // ([a-z]+)*
		regexp.MustCompile(`\(\[[^\]]+\]\*\)\*`), // ([a-z]*)*
	}

	// Exponential alternation pattern (for counting branches)
	// Note: Overlapping alternation detection removed - RE2 doesn't support backreferences needed for that check
	exponentialAltPattern = regexp.MustCompile(`\([^)]*\|[^)]*\)[*+{]`)

	// Quantified adjacency patterns (for hasQuantifiedAdjacency)
	adjacencyPatterns = []*regexp.Regexp{
		regexp.MustCompile(`\.\*\.\*`),                 // .*.*
		regexp.MustCompile(`\.\+\.\+`),                 // .+.+
		regexp.MustCompile(`\\d\+\\d\+`),               // \d+\d+
		regexp.MustCompile(`\\d\*\\d\*`),               // \d*\d*
		regexp.MustCompile(`\\w\+\\w\+`),               // \w+\w+
		regexp.MustCompile(`\\w\*\\w\*`),               // \w*\w*
		regexp.MustCompile(`\\s\+\\s\+`),               // \s+\s+
		regexp.MustCompile(`\\s\*\\s\*`),               // \s*\s*
		regexp.MustCompile(`\[[^\]]+\]\+\[[^\]]+\]\+`), // [a-z]+[a-z]+
		regexp.MustCompile(`\[[^\]]+\]\*\[[^\]]+\]\*`), // [a-z]*[a-z]*
	}
)

// FileReader interface abstracts file system operations for testability
type FileReader interface {
	ReadFile(path string) ([]byte, error)
	Open(path string) (*os.File, error)
}

// CommandExecutor interface abstracts command execution for testability
type CommandExecutor interface {
	ExecuteCommand(ctx context.Context, command string, args []string) ([]byte, error)
}

// defaultFileReader implements FileReader using standard os package
type defaultFileReader struct{}

func (r *defaultFileReader) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (r *defaultFileReader) Open(path string) (*os.File, error) {
	return os.Open(path)
}

// defaultCommandExecutor implements CommandExecutor using os/exec
type defaultCommandExecutor struct{}

func (e *defaultCommandExecutor) ExecuteCommand(ctx context.Context, command string, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	return cmd.CombinedOutput()
}

// LogPatternConfig defines a single log pattern to match
type LogPatternConfig struct {
	Name        string `json:"name"`
	Regex       string `json:"regex"`
	Severity    string `json:"severity"` // info, warning, error
	Description string `json:"description"`
	Source      string `json:"source"` // kmsg, journal, both

	// compiled is the compiled regex pattern, set once during initialization
	// via applyDefaults() and never modified thereafter. This field is safe
	// for concurrent read access because:
	// 1. It's set once during initialization before the monitor starts
	// 2. regexp.Regexp is documented as safe for concurrent use
	// 3. No runtime modification occurs in the current implementation
	//
	// IMPORTANT: If dynamic pattern reload is added in the future, this field
	// must be protected with appropriate synchronization (e.g., sync.RWMutex)
	compiled *regexp.Regexp
}

// LogPatternMonitorConfig holds the configuration for the log pattern monitor
type LogPatternMonitorConfig struct {
	// Pattern configuration
	Patterns    []LogPatternConfig `json:"patterns"`
	UseDefaults bool               `json:"useDefaults"` // Merge with default patterns

	// Kmsg configuration
	KmsgPath  string `json:"kmsgPath,omitempty"`
	CheckKmsg bool   `json:"checkKmsg"`

	// Journal configuration
	JournalUnits []string `json:"journalUnits,omitempty"`
	CheckJournal bool     `json:"checkJournal"`

	// Rate limiting
	MaxEventsPerPattern int           `json:"maxEventsPerPattern,omitempty"`
	DedupWindow         time.Duration `json:"dedupWindow,omitempty"`

	// Initialization metrics (set during applyDefaults)
	compilationTimeMs float64 // Total regex compilation time in milliseconds
	compiledOK        int     // Number of patterns compiled successfully
	compiledFailed    int     // Number of patterns failed to compile
}

// validateRegexSafety validates regex patterns to prevent ReDoS attacks and resource exhaustion
// While Go's RE2 engine provides inherent protection against catastrophic backtracking (linear time),
// this validation prevents resource exhaustion during compilation and enforces security best practices
//
// VALIDATION VS COMPLEXITY SCORING:
// This function provides BINARY validation (pass/fail) for categorically dangerous patterns.
// CalculateRegexComplexity() provides CONTINUOUS scoring (0-100) for nuanced complexity assessment.
//
// A pattern can score 25-30 (medium complexity) but still PASS validation if it's not dangerous:
// - Validation rejects: nested quantifiers, deep nesting (>3), excessive alternation (>5 branches)
// - Scoring measures: length, depth, nesting, adjacency, alternation (weighted 0-100)
// - Example: A 500-char pattern with no nesting scores 15/100 (low) and passes validation
// - Example: A pattern with depth=3 scores 20/100 (low) and passes (threshold is depth>3)
//
// Use validateRegexSafety() for security enforcement (prevent ReDoS and resource exhaustion).
// Use CalculateRegexComplexity() for understanding and documenting pattern complexity.
func validateRegexSafety(pattern string) error {
	// Check pattern length to prevent overly complex regex
	if len(pattern) > maxRegexLength {
		return fmt.Errorf("regex pattern exceeds maximum length of %d characters", maxRegexLength)
	}

	// Check for dangerous patterns that could cause resource exhaustion
	if err := checkDangerousPatterns(pattern); err != nil {
		return err
	}

	// Check for quantified adjacencies (e.g., \d+\d+, .*.*)
	if hasQuantifiedAdjacency(pattern) {
		return fmt.Errorf("regex pattern contains quantified adjacencies (e.g., \\d+\\d+) which can cause polynomial complexity")
	}

	// Check for excessive repetition depth
	if repetitionDepth := calculateRepetitionDepth(pattern); repetitionDepth > 3 {
		return fmt.Errorf("regex pattern has excessive repetition nesting (depth %d > 3), which may cause resource exhaustion", repetitionDepth)
	}

	return nil
}

// checkDangerousPatterns detects specific dangerous regex constructs using pre-compiled patterns
func checkDangerousPatterns(pattern string) error {
	// Nested quantifiers - catastrophic backtracking patterns
	// Note: RE2 handles these safely, but they indicate poor regex design and can cause compilation overhead
	nestedQuantifierDescriptions := []struct {
		example string
		desc    string
	}{
		{"(.*)+", "nested star-plus"},
		{"(.+)+", "nested plus-plus"},
		{"(.*)*", "nested star-star"},
		{"(.+)*", "nested plus-star"},
		{"(a+)+", "nested plus quantifiers"},
		{"(a*)+", "nested star-plus quantifiers"},
		{"(a+)*", "nested plus-star quantifiers"},
		{"(a*)*", "nested star quantifiers"},
		{"(a?)+", "nested optional-plus"},
		{"(a?)*", "nested optional-star"},
		{`(\w+)+`, "nested plus quantifiers"},
		{`(\w*)+`, "nested star-plus quantifiers"},
		{`(\w+)*`, "nested plus-star quantifiers"},
		{`(\w*)*`, "nested star quantifiers"},
		{`([a-z]+)+`, "nested character class plus-plus"},
		{`([a-z]*)+`, "nested character class star-plus"},
		{`([a-z]+)*`, "nested character class plus-star"},
		{`([a-z]*)*`, "nested character class star-star"},
	}

	// Use pre-compiled patterns for faster matching
	for i, compiledPattern := range nestedQuantifierPatterns {
		if compiledPattern.MatchString(pattern) {
			desc := nestedQuantifierDescriptions[i]
			return fmt.Errorf("regex pattern contains dangerous %s pattern (e.g., %s) which can cause resource exhaustion. Consider simplifying the pattern", desc.desc, desc.example)
		}
	}

	// Exponential alternation growth - count alternations in quantified groups using pre-compiled pattern
	// Note: Overlapping alternation detection removed - RE2 doesn't support backreferences needed for that check
	if matches := exponentialAltPattern.FindAllString(pattern, -1); len(matches) > 0 {
		for _, match := range matches {
			pipeCount := 0
			for _, ch := range match {
				if ch == '|' {
					pipeCount++
				}
			}
			if pipeCount > 5 {
				return fmt.Errorf("regex pattern contains alternation group with %d branches under quantifier, which can cause exponential complexity. Consider splitting into multiple patterns", pipeCount+1)
			}
		}
	}

	return nil
}

// hasQuantifiedAdjacency detects patterns like \d+\d+, .*.*, [a-z]+[a-z]+ which cause polynomial complexity
// Uses pre-compiled patterns for performance
func hasQuantifiedAdjacency(pattern string) bool {
	// Check for adjacent quantified atoms (can cause O(n²) complexity) using pre-compiled patterns
	for _, compiledPattern := range adjacencyPatterns {
		if compiledPattern.MatchString(pattern) {
			return true
		}
	}

	return false
}

// calculateRepetitionDepth calculates the maximum nesting depth of quantified groups
// Example: ((a+)+)+ has depth 3, but (error: \d+)+ has depth 1
// This only counts nested GROUPS with quantifiers, not quantifiers within a single group
// Handles escape sequences and character classes correctly
func calculateRepetitionDepth(pattern string) int {
	maxDepth := 0
	parenDepth := 0
	quantifiedGroupDepth := 0
	inCharClass := false

	i := 0
	for i < len(pattern) {
		ch := pattern[i]

		// Handle escape sequences - skip escaped character
		if ch == '\\' && i+1 < len(pattern) {
			i += 2
			continue
		}

		// Track character class state - parens inside [...] don't count
		if ch == '[' && !inCharClass {
			inCharClass = true
			i++
			continue
		}
		if ch == ']' && inCharClass {
			inCharClass = false
			i++
			continue
		}

		// Only count parens outside of character classes
		if !inCharClass {
			if ch == '(' {
				parenDepth++
			} else if ch == ')' {
				parenDepth--
				// Check if this group is quantified
				if i+1 < len(pattern) {
					nextCh := pattern[i+1]
					if nextCh == '*' || nextCh == '+' || nextCh == '?' || nextCh == '{' {
						quantifiedGroupDepth++
						if quantifiedGroupDepth > maxDepth {
							maxDepth = quantifiedGroupDepth
						}
					}
				}
				// When we close a group, check if we're exiting a quantified group
				if parenDepth == 0 {
					quantifiedGroupDepth = 0
				}
			}
		}

		i++
	}

	return maxDepth
}

// CalculateRegexComplexity calculates a complexity score (0-100) for a regex pattern
// Scores are weighted based on multiple risk factors:
// - Pattern length (0-25 points)
// - Repetition depth (0-30 points)
// - Nested quantifiers (0-20 points)
// - Quantified adjacency (0-15 points)
// - Alternation branches (0-10 points)
//
// Score ranges:
//
//	0-30: Low complexity (safe)
//	31-60: Medium complexity (caution)
//	61-80: High complexity (review needed)
//	81-100: Very high complexity (dangerous, likely to fail validation)
//
// SCORING VS VALIDATION:
// This function provides CONTINUOUS complexity assessment (0-100 scale).
// validateRegexSafety() provides BINARY security enforcement (pass/fail).
//
// Key differences:
// - A pattern scoring 25-30 may still PASS validation (not dangerous, just moderately complex)
// - A pattern scoring 60+ will likely FAIL validation (dangerous patterns score high)
// - Scoring is for documentation and understanding; validation is for security
//
// Example: A 500-character pattern with simple structure:
// - CalculateRegexComplexity() returns 15 (low complexity)
// - validateRegexSafety() returns nil (passes - pattern is safe)
//
// Example: A pattern with nested quantifiers `(.*)+`:
// - CalculateRegexComplexity() returns 25 (low-medium complexity)
// - validateRegexSafety() returns error (fails - pattern is dangerous)
//
// This function is designed to provide gradual complexity assessment,
// complementing the binary pass/fail of validateRegexSafety()
func CalculateRegexComplexity(pattern string) int {
	score := 0

	// Factor 1: Pattern length (max 25 points)
	score += calculateLengthScore(pattern)

	// Factor 2: Repetition depth (max 30 points)
	score += calculateDepthScore(pattern)

	// Factor 3: Nested quantifiers (max 20 points)
	score += calculateNestedQuantifierScore(pattern)

	// Factor 4: Quantified adjacency (max 15 points)
	score += calculateAdjacencyScore(pattern)

	// Factor 5: Alternation branches (max 10 points)
	score += calculateAlternationScore(pattern)

	// Cap at 100
	if score > 100 {
		score = 100
	}

	return score
}

// calculateLengthScore scores pattern length on a scale of 0-25
// Uses progressive scaling:
//
//	0-100 chars: 0-5 points (linear)
//	101-500 chars: 5-15 points (linear)
//	501-1000 chars: 15-25 points (linear)
//	>1000 chars: 25 points (max)
func calculateLengthScore(pattern string) int {
	length := len(pattern)

	switch {
	case length <= 100:
		// 0-100 chars: 0-5 points (0.05 points per char)
		return int(float64(length) * 0.05)
	case length <= 500:
		// 101-500 chars: 5-15 points (0.025 points per char above 100)
		return 5 + int(float64(length-100)*0.025)
	case length <= maxRegexLength: // 1000
		// 501-1000 chars: 15-25 points (0.02 points per char above 500)
		return 15 + int(float64(length-500)*0.02)
	default:
		// Over 1000 chars: max 25 points
		return 25
	}
}

// calculateDepthScore scores repetition depth on a scale of 0-30
// Uses existing calculateRepetitionDepth() function
// Scoring:
//
//	Depth 0: 0 points
//	Depth 1: 5 points
//	Depth 2: 12 points
//	Depth 3: 20 points (max safe depth)
//	Depth 4+: 30 points (dangerous)
func calculateDepthScore(pattern string) int {
	depth := calculateRepetitionDepth(pattern)

	switch depth {
	case 0:
		return 0
	case 1:
		return 5
	case 2:
		return 12
	case 3:
		return 20
	default: // 4 or more
		return 30
	}
}

// calculateNestedQuantifierScore detects nested quantifiers and scores 0-20
// Uses patterns from checkDangerousPatterns()
// Each type of nested quantifier adds points:
//
//	Basic nested (.*)+, (.+)+: 20 points each (any match = max score)
//	Complex nested (a+)+, (\w+)+: 20 points each
//	Character class nested ([a-z]+)+: 20 points each
//
// Returns 20 if ANY nested quantifier is found, 0 otherwise
// This is binary because nested quantifiers are categorically dangerous
func calculateNestedQuantifierScore(pattern string) int {
	// Reuse detection logic from checkDangerousPatterns()
	nestedQuantifiers := []string{
		`\(\.\*\)\+`,         // (.*)+
		`\(\.\+\)\+`,         // (.+)+
		`\(\.\*\)\*`,         // (.*)*
		`\(\.\+\)\*`,         // (.+)*
		`\([a-zA-Z]\+\)\+`,   // (a+)+
		`\([a-zA-Z]\*\)\+`,   // (a*)+
		`\([a-zA-Z]\+\)\*`,   // (a+)*
		`\([a-zA-Z]\*\)\*`,   // (a*)*
		`\([a-zA-Z]\?\)\+`,   // (a?)+
		`\([a-zA-Z]\?\)\*`,   // (a?)*
		`\(\\[wdWDs]\+\)\+`,  // (\w+)+
		`\(\\[wdWDs]\*\)\+`,  // (\w*)+
		`\(\\[wdWDs]\+\)\*`,  // (\w+)*
		`\(\\[wdWDs]\*\)\*`,  // (\w*)*
		`\(\[[^\]]+\]\+\)\+`, // ([a-z]+)+
		`\(\[[^\]]+\]\*\)\+`, // ([a-z]*)+
		`\(\[[^\]]+\]\+\)\*`, // ([a-z]+)*
		`\(\[[^\]]+\]\*\)\*`, // ([a-z]*)*
	}

	for _, nq := range nestedQuantifiers {
		if matched, err := regexp.MatchString(nq, pattern); err == nil && matched {
			return 20 // Binary: found = dangerous
		}
	}

	return 0
}

// calculateAdjacencyScore detects quantified adjacency patterns and scores 0-15
// Uses hasQuantifiedAdjacency() to detect patterns like \d+\d+, .*.*
//
// Returns 15 if adjacency is found, 0 otherwise
// This is binary because adjacency causes polynomial complexity
func calculateAdjacencyScore(pattern string) int {
	if hasQuantifiedAdjacency(pattern) {
		return 15
	}
	return 0
}

// calculateAlternationScore counts alternation branches in quantified groups
// Scoring scale:
//
//	0-2 branches: 0 points
//	3 branches: 2 points
//	4 branches: 4 points
//	5 branches: 6 points (threshold of safety)
//	6 branches: 8 points
//	7+ branches: 10 points (dangerous)
func calculateAlternationScore(pattern string) int {
	altPattern := regexp.MustCompile(`\([^)]*\|[^)]*\)[*+{]`)
	matches := altPattern.FindAllString(pattern, -1)

	maxBranches := 0
	for _, match := range matches {
		pipeCount := 0
		for _, ch := range match {
			if ch == '|' {
				pipeCount++
			}
		}
		branches := pipeCount + 1
		if branches > maxBranches {
			maxBranches = branches
		}
	}

	// Score based on max branches found
	switch {
	case maxBranches <= 2:
		return 0
	case maxBranches == 3:
		return 2
	case maxBranches == 4:
		return 4
	case maxBranches == 5:
		return 6
	case maxBranches == 6:
		return 8
	default: // 7+
		return 10
	}
}

// compileWithTimeout compiles a regex pattern with a timeout to prevent resource exhaustion
// If compilation takes longer than the timeout, an error is returned
// This protects against pathological regex patterns that could cause excessive CPU usage during compilation
func compileWithTimeout(pattern string, timeout time.Duration) (*regexp.Regexp, error) {
	type compileResult struct {
		compiled *regexp.Regexp
		err      error
	}

	resultChan := make(chan compileResult, 1)

	// Run compilation in a goroutine
	go func() {
		compiled, err := regexp.Compile(pattern)
		resultChan <- compileResult{compiled: compiled, err: err}
	}()

	// Wait for result or timeout
	select {
	case result := <-resultChan:
		return result.compiled, result.err
	case <-time.After(timeout):
		return nil, fmt.Errorf("regex compilation timeout after %v - pattern may be too complex", timeout)
	}
}

// LogPatternMetrics holds observability metrics for the log pattern monitor
type LogPatternMetrics struct {
	// Pattern matching metrics
	TotalPatternsChecked int            `json:"totalPatternsChecked"` // Total patterns evaluated per check
	PatternsMatched      map[string]int `json:"patternsMatched"`      // Pattern name -> match count
	PatternsSuppressed   map[string]int `json:"patternsSuppressed"`   // Pattern name -> suppressed count (rate limited)

	// Performance metrics
	CheckDurationMs        float64            `json:"checkDurationMs"`        // Total check duration in milliseconds
	KmsgCheckDurationMs    float64            `json:"kmsgCheckDurationMs"`    // Kmsg check duration
	JournalCheckDurationMs map[string]float64 `json:"journalCheckDurationMs"` // Unit name -> check duration
	RegexCompilationTimeMs float64            `json:"regexCompilationTimeMs"` // Total regex compilation time (initialization)

	// Configuration metrics
	TotalConfiguredPatterns int            `json:"totalConfiguredPatterns"`
	PatternsBySource        map[string]int `json:"patternsBySource"` // Source -> count
	TotalJournalUnits       int            `json:"totalJournalUnits"`
	MaxEventsPerPattern     int            `json:"maxEventsPerPattern"`
	DedupWindowSeconds      float64        `json:"dedupWindowSeconds"`

	// Health metrics
	KmsgAccessible         bool `json:"kmsgAccessible"`
	JournalAccessible      bool `json:"journalAccessible"`
	PatternsCompiledOK     int  `json:"patternsCompiledOK"`
	PatternsCompiledFailed int  `json:"patternsCompiledFailed"`

	// Resource metrics
	LastCleanupTime      time.Time `json:"lastCleanupTime"`
	TrackedPatternStates int       `json:"trackedPatternStates"` // Number of patterns being tracked
}

// LogPatternMonitor monitors log patterns in kernel messages and systemd journals
type LogPatternMonitor struct {
	name   string
	config *LogPatternMonitorConfig

	// Interface abstractions for testability
	fileReader      FileReader
	commandExecutor CommandExecutor

	// Thread-safe state tracking
	mu                   sync.RWMutex
	patternEventCount    map[string]int       // pattern name -> event count
	patternLastEvent     map[string]time.Time // pattern name -> last event time
	lastJournalCheck     map[string]time.Time // unit name -> last check time
	kmsgPermissionWarned bool                 // Track if we've warned about kmsg permissions
	lastCleanup          time.Time            // Last time map cleanup was performed

	// Metrics tracking (thread-safe access via methods)
	metrics                LogPatternMetrics
	regexCompilationTimeMs float64 // Set during initialization

	// BaseMonitor for lifecycle management
	*monitors.BaseMonitor
}

// logStructured writes a structured log message with consistent format
// Format: [LogPattern] [level] monitor=<name> component=<component> action=<action> message
func (m *LogPatternMonitor) logStructured(level, component, action, message string) {
	log.Printf("[LogPattern] [%s] monitor=%s component=%s action=%s %s",
		level, m.name, component, action, message)
}

// NewLogPatternMonitor creates a new log pattern monitor
func NewLogPatternMonitor(ctx context.Context, config types.MonitorConfig) (types.Monitor, error) {
	// Parse monitor-specific configuration
	logConfig, err := parseLogPatternConfig(config.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse log pattern config: %w", err)
	}

	// Apply defaults
	if err := logConfig.applyDefaults(); err != nil {
		return nil, fmt.Errorf("failed to apply defaults: %w", err)
	}

	// Create with real file reader and command executor
	return NewLogPatternMonitorWithDependencies(
		ctx,
		config,
		logConfig,
		&defaultFileReader{},
		&defaultCommandExecutor{},
	)
}

// NewLogPatternMonitorWithDependencies creates a log pattern monitor with custom dependencies (for testing)
func NewLogPatternMonitorWithDependencies(
	ctx context.Context,
	config types.MonitorConfig,
	logConfig *LogPatternMonitorConfig,
	fileReader FileReader,
	commandExecutor CommandExecutor,
) (types.Monitor, error) {
	// Create base monitor
	baseMonitor, err := monitors.NewBaseMonitor(config.Name, config.Interval, config.Timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to create base monitor: %w", err)
	}

	// Initialize configuration metrics
	patternsBySource := make(map[string]int)
	for _, pattern := range logConfig.Patterns {
		patternsBySource[pattern.Source]++
	}

	// Create log pattern monitor with initialized metrics
	monitor := &LogPatternMonitor{
		name:                   config.Name,
		config:                 logConfig,
		fileReader:             fileReader,
		commandExecutor:        commandExecutor,
		patternEventCount:      make(map[string]int),
		patternLastEvent:       make(map[string]time.Time),
		lastJournalCheck:       make(map[string]time.Time),
		regexCompilationTimeMs: logConfig.compilationTimeMs,
		BaseMonitor:            baseMonitor,
		metrics: LogPatternMetrics{
			// Initialize configuration metrics (static)
			TotalConfiguredPatterns: len(logConfig.Patterns),
			PatternsBySource:        patternsBySource,
			TotalJournalUnits:       len(logConfig.JournalUnits),
			MaxEventsPerPattern:     logConfig.MaxEventsPerPattern,
			DedupWindowSeconds:      logConfig.DedupWindow.Seconds(),
			RegexCompilationTimeMs:  logConfig.compilationTimeMs,
			PatternsCompiledOK:      logConfig.compiledOK,
			PatternsCompiledFailed:  logConfig.compiledFailed,

			// Initialize runtime metrics (will be updated per check)
			PatternsMatched:        make(map[string]int),
			PatternsSuppressed:     make(map[string]int),
			JournalCheckDurationMs: make(map[string]float64),
		},
	}

	// Log monitor initialization
	monitor.logStructured("INFO", "initialization", "created",
		fmt.Sprintf("patterns=%d journalUnits=%d compilationTimeMs=%.2f",
			len(logConfig.Patterns), len(logConfig.JournalUnits), logConfig.compilationTimeMs))

	// Set the check function
	if err := baseMonitor.SetCheckFunc(monitor.checkLogPatterns); err != nil {
		return nil, fmt.Errorf("failed to set check function: %w", err)
	}

	return monitor, nil
}

// checkLogPatterns performs the log pattern checking
func (m *LogPatternMonitor) checkLogPatterns(ctx context.Context) (*types.Status, error) {
	checkStart := time.Now()
	status := types.NewStatus(m.GetName())

	// Reset runtime metrics for this check
	m.mu.Lock()
	m.metrics.PatternsMatched = make(map[string]int)
	m.metrics.PatternsSuppressed = make(map[string]int)
	m.metrics.JournalCheckDurationMs = make(map[string]float64)
	m.metrics.TotalPatternsChecked = 0
	m.metrics.KmsgCheckDurationMs = 0
	m.metrics.TrackedPatternStates = len(m.patternEventCount)
	m.metrics.LastCleanupTime = m.lastCleanup
	m.mu.Unlock()

	// Perform periodic cleanup of expired entries to prevent unbounded map growth
	m.cleanupExpiredEntries()

	// Check kmsg if enabled
	if m.config.CheckKmsg {
		kmsgStart := time.Now()
		m.checkKmsg(ctx, status)

		m.mu.Lock()
		m.metrics.KmsgCheckDurationMs = float64(time.Since(kmsgStart).Microseconds()) / 1000.0
		m.mu.Unlock()
	}

	// Check journal units if enabled
	if m.config.CheckJournal {
		for _, unit := range m.config.JournalUnits {
			journalStart := time.Now()
			m.checkJournalUnit(ctx, unit, status)

			m.mu.Lock()
			m.metrics.JournalCheckDurationMs[unit] = float64(time.Since(journalStart).Microseconds()) / 1000.0
			m.mu.Unlock()
		}
	}

	// Calculate total check duration
	m.mu.Lock()
	m.metrics.CheckDurationMs = float64(time.Since(checkStart).Microseconds()) / 1000.0

	// Update health metrics
	m.metrics.KmsgAccessible = !m.kmsgPermissionWarned || !m.config.CheckKmsg
	m.metrics.JournalAccessible = true // Updated per-unit in checkJournalUnit

	// Log check completion with performance metrics
	checkDurationMs := m.metrics.CheckDurationMs
	patternsChecked := m.metrics.TotalPatternsChecked
	patternsMatched := len(m.metrics.PatternsMatched)
	patternsSuppressed := len(m.metrics.PatternsSuppressed)
	m.mu.Unlock()

	// Log performance warning if check took too long
	if checkDurationMs > 100.0 {
		m.logStructured("WARN", "performance", "check_slow",
			fmt.Sprintf("durationMs=%.2f patternsChecked=%d patternsMatched=%d patternsSuppressed=%d",
				checkDurationMs, patternsChecked, patternsMatched, patternsSuppressed))
	} else {
		m.logStructured("DEBUG", "performance", "check_complete",
			fmt.Sprintf("durationMs=%.2f patternsChecked=%d patternsMatched=%d patternsSuppressed=%d",
				checkDurationMs, patternsChecked, patternsMatched, patternsSuppressed))
	}

	// Add metrics to status as metadata
	m.mu.Lock()
	status.Metadata = map[string]interface{}{
		"metrics": m.metrics,
	}
	m.mu.Unlock()

	return status, nil
}

// checkKmsg reads and checks kernel messages
func (m *LogPatternMonitor) checkKmsg(ctx context.Context, status *types.Status) {
	data, err := m.fileReader.ReadFile(m.config.KmsgPath)
	if err != nil {
		// Handle permission errors gracefully
		if os.IsPermission(err) {
			// Capture error message before acquiring lock
			errMsg := fmt.Sprintf("Cannot access %s for kernel log monitoring: %v. Continuing without kmsg monitoring.", m.config.KmsgPath, err)

			// Check and update warning flag inside lock
			m.mu.Lock()
			shouldWarn := !m.kmsgPermissionWarned
			if shouldWarn {
				m.kmsgPermissionWarned = true
			}
			m.mu.Unlock()

			// Create event outside lock to prevent blocking other goroutines
			if shouldWarn {
				m.logStructured("WARN", "kmsg", "permission_denied",
					fmt.Sprintf("path=%s error=%v", m.config.KmsgPath, err))
				status.AddEvent(types.NewEvent(
					types.EventWarning,
					"KmsgPermissionDenied",
					errMsg,
				))
			}
			return
		}

		// Other errors
		m.logStructured("ERROR", "kmsg", "read_failed",
			fmt.Sprintf("path=%s error=%v", m.config.KmsgPath, err))
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"KmsgReadError",
			fmt.Sprintf("Failed to read %s: %v", m.config.KmsgPath, err),
		))
		return
	}

	// Protect against excessive memory usage
	if len(data) > maxKmsgBufferSize {
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"KmsgBufferTooLarge",
			fmt.Sprintf("Kmsg buffer size (%d bytes) exceeds limit (%d bytes), truncating to recent entries",
				len(data), maxKmsgBufferSize),
		))
		data = data[len(data)-maxKmsgBufferSize:]
	}

	// Process each line
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		m.matchPatterns(line, "kmsg", status)
	}
}

// checkJournalUnit checks a specific systemd journal unit
func (m *LogPatternMonitor) checkJournalUnit(ctx context.Context, unit string, status *types.Status) {
	m.mu.RLock()
	lastCheck := m.lastJournalCheck[unit]
	m.mu.RUnlock()

	// Build journalctl command
	args := []string{
		"-u", unit,
		"--no-pager",
		"-o", "cat", // Message only, no metadata
	}

	// Add since parameter if we have a last check time
	if !lastCheck.IsZero() {
		args = append(args, "--since", lastCheck.Format("2006-01-02 15:04:05"))
	} else {
		// First check: get last 100 lines
		args = append(args, "-n", "100")
	}

	// Execute journalctl
	output, err := m.commandExecutor.ExecuteCommand(ctx, "journalctl", args)
	if err != nil {
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"JournalctlError",
			fmt.Sprintf("Failed to execute journalctl for unit %s: %v", unit, err),
		))
		return
	}

	// Update last check time
	m.mu.Lock()
	m.lastJournalCheck[unit] = time.Now()
	m.mu.Unlock()

	// Process each line
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		m.matchPatterns(line, "journal", status)
	}
}

// matchPatterns checks a log line against all configured patterns
func (m *LogPatternMonitor) matchPatterns(line, source string, status *types.Status) {
	for _, pattern := range m.config.Patterns {
		// Skip if pattern doesn't apply to this source
		if !m.shouldCheckPattern(&pattern, source) {
			continue
		}

		// Increment patterns checked metric
		m.mu.Lock()
		m.metrics.TotalPatternsChecked++
		m.mu.Unlock()

		// Check if pattern matches
		if pattern.compiled.MatchString(line) {
			// Check rate limiting (this will track suppressions)
			reported, suppressed := m.shouldReportEvent(pattern.Name)

			if suppressed {
				// Pattern was suppressed due to rate limiting
				m.mu.Lock()
				m.metrics.PatternsSuppressed[pattern.Name]++
				m.mu.Unlock()
				continue
			}

			if !reported {
				// First occurrence or within dedup window but under limit
				continue
			}

			// Track matched patterns
			m.mu.Lock()
			m.metrics.PatternsMatched[pattern.Name]++
			m.mu.Unlock()

			// Generate event
			severity := m.parseSeverity(pattern.Severity)
			event := types.NewEvent(
				severity,
				pattern.Name,
				fmt.Sprintf("%s (source: %s): %s", pattern.Description, source, line),
			)
			status.AddEvent(event)

			// Add condition for error-level patterns
			if severity == types.EventError {
				condition := types.NewCondition(
					pattern.Name,
					types.ConditionTrue,
					"LogPatternMatched",
					fmt.Sprintf("Pattern '%s' matched in %s logs", pattern.Name, source),
				)
				status.AddCondition(condition)
			}
		}
	}
}

// shouldCheckPattern determines if a pattern should be checked for the given source
func (m *LogPatternMonitor) shouldCheckPattern(pattern *LogPatternConfig, source string) bool {
	switch pattern.Source {
	case "kmsg":
		return source == "kmsg"
	case "journal":
		return source == "journal"
	case "both":
		return true
	default:
		return true // Default to checking all sources
	}
}

// shouldReportEvent checks rate limiting and deduplication
// Returns (reported, suppressed):
//   - reported: true if event should be reported
//   - suppressed: true if event was suppressed due to rate limiting
//
// This function is thread-safe and consolidates map operations to minimize
// lock time and reduce race condition potential
func (m *LogPatternMonitor) shouldReportEvent(patternName string) (reported bool, suppressed bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	lastEvent, exists := m.patternLastEvent[patternName]

	// Check if we need to reset (outside dedup window)
	if !exists || now.Sub(lastEvent) > m.config.DedupWindow {
		// Reset to 1 (this event counts as first in new window)
		m.patternEventCount[patternName] = 1
		m.patternLastEvent[patternName] = now
		return true, false
	}

	// Check rate limit
	count := m.patternEventCount[patternName]
	if count >= m.config.MaxEventsPerPattern {
		// Event is suppressed due to rate limiting
		return false, true
	}

	// Increment and update (single write path)
	m.patternEventCount[patternName] = count + 1
	m.patternLastEvent[patternName] = now
	return true, false
}

// parseSeverity converts string severity to event severity
func (m *LogPatternMonitor) parseSeverity(severity string) types.EventSeverity {
	switch strings.ToLower(severity) {
	case "error":
		return types.EventError
	case "warning":
		return types.EventWarning
	case "info":
		return types.EventInfo
	default:
		return types.EventWarning
	}
}

// cleanupExpiredEntries removes old entries from tracking maps to prevent unbounded growth
// This method is called periodically (every 10 minutes) to clean up entries that are
// older than 2x the dedup window, maintaining bounded memory usage while preserving
// recent pattern matching state
func (m *LogPatternMonitor) cleanupExpiredEntries() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()

	// Only cleanup every 10 minutes to minimize overhead
	if now.Sub(m.lastCleanup) < 10*time.Minute {
		return
	}

	m.lastCleanup = now

	// Keep entries accessed within last 2x dedup window
	// This provides a safety margin while preventing unbounded growth
	expiryTime := now.Add(-m.config.DedupWindow * 2)

	// Clean pattern event tracking maps
	for pattern, lastEvent := range m.patternLastEvent {
		if lastEvent.Before(expiryTime) {
			delete(m.patternEventCount, pattern)
			delete(m.patternLastEvent, pattern)
		}
	}

	// Clean journal check tracking map
	for unit, lastCheck := range m.lastJournalCheck {
		if lastCheck.Before(expiryTime) {
			delete(m.lastJournalCheck, unit)
		}
	}
}

// parseLogPatternConfig parses the log pattern monitor configuration
func parseLogPatternConfig(configMap map[string]interface{}) (*LogPatternMonitorConfig, error) {
	config := &LogPatternMonitorConfig{
		Patterns: make([]LogPatternConfig, 0),
	}

	if configMap == nil {
		return config, nil
	}

	// Parse useDefaults
	if val, exists := configMap["useDefaults"]; exists {
		if b, ok := val.(bool); ok {
			config.UseDefaults = b
		}
	}

	// Parse patterns array
	if val, exists := configMap["patterns"]; exists {
		patternsArray, ok := val.([]interface{})
		if !ok {
			return nil, fmt.Errorf("patterns must be an array, got %T", val)
		}

		for i, patternVal := range patternsArray {
			patternMap, ok := patternVal.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("pattern %d must be an object, got %T", i, patternVal)
			}

			pattern, err := parseLogPattern(patternMap)
			if err != nil {
				return nil, fmt.Errorf("failed to parse pattern %d: %w", i, err)
			}

			config.Patterns = append(config.Patterns, *pattern)
		}
	}

	// Parse kmsg configuration
	if val, exists := configMap["kmsgPath"]; exists {
		if s, ok := val.(string); ok {
			config.KmsgPath = s
		}
	}

	if val, exists := configMap["checkKmsg"]; exists {
		if b, ok := val.(bool); ok {
			config.CheckKmsg = b
		}
	}

	// Parse journal configuration
	if val, exists := configMap["journalUnits"]; exists {
		unitsArray, ok := val.([]interface{})
		if !ok {
			return nil, fmt.Errorf("journalUnits must be an array, got %T", val)
		}

		for i, unitVal := range unitsArray {
			unit, ok := unitVal.(string)
			if !ok {
				return nil, fmt.Errorf("journalUnits[%d] must be a string, got %T", i, unitVal)
			}
			config.JournalUnits = append(config.JournalUnits, unit)
		}
	}

	if val, exists := configMap["checkJournal"]; exists {
		if b, ok := val.(bool); ok {
			config.CheckJournal = b
		}
	}

	// Parse rate limiting
	if val, exists := configMap["maxEventsPerPattern"]; exists {
		switch v := val.(type) {
		case int:
			config.MaxEventsPerPattern = v
		case float64:
			config.MaxEventsPerPattern = int(v)
		}
	}

	if val, exists := configMap["dedupWindow"]; exists {
		switch v := val.(type) {
		case string:
			duration, err := time.ParseDuration(v)
			if err != nil {
				return nil, fmt.Errorf("failed to parse dedupWindow: %w", err)
			}
			config.DedupWindow = duration
		case float64:
			config.DedupWindow = time.Duration(v) * time.Second
		case int:
			config.DedupWindow = time.Duration(v) * time.Second
		}
	}

	return config, nil
}

// parseLogPattern parses a single log pattern configuration
func parseLogPattern(patternMap map[string]interface{}) (*LogPatternConfig, error) {
	pattern := &LogPatternConfig{}

	// Parse name (required)
	if val, exists := patternMap["name"]; exists {
		if s, ok := val.(string); ok {
			pattern.Name = s
		} else {
			return nil, fmt.Errorf("name must be a string, got %T", val)
		}
	} else {
		return nil, fmt.Errorf("name is required")
	}

	// Parse regex (required)
	if val, exists := patternMap["regex"]; exists {
		if s, ok := val.(string); ok {
			pattern.Regex = s
		} else {
			return nil, fmt.Errorf("regex must be a string, got %T", val)
		}
	} else {
		return nil, fmt.Errorf("regex is required")
	}

	// Parse severity (optional, default: warning)
	if val, exists := patternMap["severity"]; exists {
		if s, ok := val.(string); ok {
			pattern.Severity = s
		}
	}
	if pattern.Severity == "" {
		pattern.Severity = "warning"
	}

	// Parse description (optional)
	if val, exists := patternMap["description"]; exists {
		if s, ok := val.(string); ok {
			pattern.Description = s
		}
	}

	// Parse source (optional, default: both)
	if val, exists := patternMap["source"]; exists {
		if s, ok := val.(string); ok {
			pattern.Source = s
		}
	}
	if pattern.Source == "" {
		pattern.Source = "both"
	}

	return pattern, nil
}

// applyDefaults applies default values to optional configuration fields
func (c *LogPatternMonitorConfig) applyDefaults() error {
	// Apply path defaults
	if c.KmsgPath == "" {
		c.KmsgPath = defaultKmsgPath
	}

	// Apply rate limiting defaults
	if c.MaxEventsPerPattern == 0 {
		c.MaxEventsPerPattern = defaultMaxEventsPerPattern
	}

	if c.DedupWindow == 0 {
		c.DedupWindow = defaultDedupWindow
	}

	// Validate MaxEventsPerPattern bounds
	if c.MaxEventsPerPattern < minMaxEventsPerPattern || c.MaxEventsPerPattern > maxMaxEventsPerPattern {
		return fmt.Errorf("maxEventsPerPattern must be between %d and %d, got %d",
			minMaxEventsPerPattern, maxMaxEventsPerPattern, c.MaxEventsPerPattern)
	}

	// Validate DedupWindow bounds
	if c.DedupWindow < minDedupWindow || c.DedupWindow > maxDedupWindow {
		return fmt.Errorf("dedupWindow must be between %v and %v, got %v",
			minDedupWindow, maxDedupWindow, c.DedupWindow)
	}

	// Merge with default patterns if requested
	if c.UseDefaults {
		c.Patterns = MergeWithDefaults(c.Patterns, true)
	}

	// Validate pattern count limit
	if len(c.Patterns) > maxConfiguredPatterns {
		return fmt.Errorf("number of configured patterns (%d) exceeds maximum limit of %d. "+
			"Consider consolidating patterns or disabling default patterns with useDefaults: false",
			len(c.Patterns), maxConfiguredPatterns)
	}

	// Validate journal units limit
	if len(c.JournalUnits) > maxJournalUnits {
		return fmt.Errorf("number of journal units (%d) exceeds maximum limit of %d. "+
			"Monitor fewer units or use pattern matching to reduce unit count",
			len(c.JournalUnits), maxJournalUnits)
	}

	// Estimate total memory usage for this configuration
	estimatedMemory := c.estimateMemoryUsage()
	if estimatedMemory > maxMemoryPerCheck {
		return fmt.Errorf("estimated memory usage (%.2f MB) exceeds limit (%.2f MB). "+
			"Reduce pattern count (%d), journal units (%d), or maxEventsPerPattern (%d)",
			float64(estimatedMemory)/(1024*1024),
			float64(maxMemoryPerCheck)/(1024*1024),
			len(c.Patterns), len(c.JournalUnits), c.MaxEventsPerPattern)
	}

	// Validate and compile regex patterns, tracking compilation time
	compilationStart := time.Now()
	compiledOK := 0
	compiledFailed := 0

	for i := range c.Patterns {
		// Validate regex safety to prevent ReDoS attacks
		if err := validateRegexSafety(c.Patterns[i].Regex); err != nil {
			compiledFailed++
			return fmt.Errorf("regex safety validation failed for pattern %s: %w", c.Patterns[i].Name, err)
		}

		// Compile with timeout to prevent resource exhaustion during compilation
		compiled, err := compileWithTimeout(c.Patterns[i].Regex, 100*time.Millisecond)
		if err != nil {
			compiledFailed++
			return fmt.Errorf("regex compilation failed for pattern %s: %w", c.Patterns[i].Name, err)
		}
		c.Patterns[i].compiled = compiled
		compiledOK++

		// Validate source
		source := c.Patterns[i].Source
		if source != "kmsg" && source != "journal" && source != "both" {
			return fmt.Errorf("invalid source in pattern %s: %s (must be kmsg, journal, or both)", c.Patterns[i].Name, source)
		}
	}

	// Store compilation metrics
	c.compilationTimeMs = float64(time.Since(compilationStart).Microseconds()) / 1000.0
	c.compiledOK = compiledOK
	c.compiledFailed = compiledFailed

	return nil
}

// estimateMemoryUsage calculates approximate memory usage for this configuration
// This is a conservative estimate used for early validation to prevent OOM scenarios
func (c *LogPatternMonitorConfig) estimateMemoryUsage() int {
	memory := 0

	// Pattern storage (struct + compiled regex)
	memory += len(c.Patterns) * memoryPerPattern

	// Journal unit tracking (unit name + lastCheck timestamp)
	memory += len(c.JournalUnits) * memoryPerJournalUnit

	// Event tracking maps (worst case: all patterns firing at max rate)
	// Each pattern tracks: count (int) + lastEvent (time.Time)
	memory += len(c.Patterns) * c.MaxEventsPerPattern * memoryPerEventRecord

	// Journal check tracking (one timestamp per unit)
	memory += len(c.JournalUnits) * 32 // time.Time size

	// Kmsg buffer allocation
	memory += maxKmsgBufferSize

	return memory
}

// ValidateLogPatternConfig validates the log pattern monitor configuration
func ValidateLogPatternConfig(config types.MonitorConfig) error {
	// Parse the configuration
	logConfig, err := parseLogPatternConfig(config.Config)
	if err != nil {
		return err
	}

	// Apply defaults for validation
	if err := logConfig.applyDefaults(); err != nil {
		return err
	}

	// Validate that at least one pattern is configured
	if len(logConfig.Patterns) == 0 {
		return fmt.Errorf("at least one log pattern must be configured")
	}

	// Validate that at least one source is enabled
	if !logConfig.CheckKmsg && !logConfig.CheckJournal {
		return fmt.Errorf("at least one log source must be enabled (checkKmsg or checkJournal)")
	}

	// Validate journal units if journal checking is enabled
	if logConfig.CheckJournal && len(logConfig.JournalUnits) == 0 {
		return fmt.Errorf("journalUnits must be specified when checkJournal is enabled")
	}

	return nil
}

// init registers the log pattern monitor
func init() {
	description := `Monitors log patterns in kernel messages and systemd journals for known issues.

Observability Metrics (exposed via status.Metadata["metrics"]):

Pattern Matching Metrics:
  - totalPatternsChecked: Total patterns evaluated per check
  - patternsMatched: Map of pattern name -> match count
  - patternsSuppressed: Map of pattern name -> suppressed count (rate limited)

Performance Metrics:
  - checkDurationMs: Total check duration in milliseconds
  - kmsgCheckDurationMs: Kernel message check duration
  - journalCheckDurationMs: Map of unit name -> check duration
  - regexCompilationTimeMs: Total regex compilation time (initialization)

Configuration Metrics:
  - totalConfiguredPatterns: Number of patterns configured
  - patternsBySource: Map of source (kmsg/journal/both) -> count
  - totalJournalUnits: Number of journal units monitored
  - maxEventsPerPattern: Rate limit configuration
  - dedupWindowSeconds: Deduplication window in seconds

Health Metrics:
  - kmsgAccessible: Whether /dev/kmsg is accessible
  - journalAccessible: Whether journalctl is accessible
  - patternsCompiledOK: Number of patterns compiled successfully
  - patternsCompiledFailed: Number of patterns failed to compile

Resource Metrics:
  - lastCleanupTime: Timestamp of last map cleanup
  - trackedPatternStates: Number of patterns being tracked

Structured Logging Format:
  [LogPattern] [level] monitor=<name> component=<component> action=<action> <message>

Performance Targets:
  - Check duration: <10ms for typical configurations (10-25 patterns, 100-500 log lines)
  - Warning logged if check duration >100ms
`

	monitors.Register(monitors.MonitorInfo{
		Type:        "custom-logpattern",
		Factory:     NewLogPatternMonitor,
		Validator:   ValidateLogPatternConfig,
		Description: description,
	})
}
