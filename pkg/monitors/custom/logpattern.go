package custom

import (
	"context"
	"fmt"
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
	defaultKmsgPath           = "/dev/kmsg"
	defaultMaxEventsPerPattern = 10
	defaultDedupWindow        = 5 * time.Minute

	// Regex safety limits to prevent ReDoS attacks
	maxRegexLength = 1000 // Maximum allowed regex pattern length

	// Resource limits to prevent unbounded memory usage and DoS attacks
	maxConfiguredPatterns   = 50                  // Maximum number of patterns (user + defaults)
	maxJournalUnits        = 20                  // Maximum number of journal units to monitor
	minMaxEventsPerPattern = 1                   // Minimum events per pattern per window
	maxMaxEventsPerPattern = 1000                // Maximum events per pattern per window
	minDedupWindow         = 1 * time.Second     // Minimum deduplication window
	maxDedupWindow         = 1 * time.Hour       // Maximum deduplication window

	// Memory estimation constants (approximate per-item memory usage in bytes)
	memoryPerPattern       = 512  // bytes: regex + compiled state + name + desc
	memoryPerJournalUnit   = 256  // bytes: unit name + timestamp tracking
	memoryPerEventRecord   = 128  // bytes: map entry overhead + timestamp

	// Overall memory limit per check (conservative estimate)
	maxMemoryPerCheck = 10 * 1024 * 1024 // 10MB total limit
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
	Severity    string `json:"severity"`    // info, warning, error
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
	Patterns       []LogPatternConfig `json:"patterns"`
	UseDefaults    bool               `json:"useDefaults"` // Merge with default patterns

	// Kmsg configuration
	KmsgPath   string `json:"kmsgPath,omitempty"`
	CheckKmsg  bool   `json:"checkKmsg"`

	// Journal configuration
	JournalUnits []string `json:"journalUnits,omitempty"`
	CheckJournal bool     `json:"checkJournal"`

	// Rate limiting
	MaxEventsPerPattern int           `json:"maxEventsPerPattern,omitempty"`
	DedupWindow         time.Duration `json:"dedupWindow,omitempty"`
}

// validateRegexSafety validates regex patterns to prevent ReDoS attacks
func validateRegexSafety(pattern string) error {
	// Check pattern length to prevent overly complex regex
	if len(pattern) > maxRegexLength {
		return fmt.Errorf("regex pattern exceeds maximum length of %d characters", maxRegexLength)
	}

	// Check for potentially dangerous patterns that could cause catastrophic backtracking
	// Patterns with nested quantifiers like (a+)+ are particularly dangerous
	dangerousPatterns := []string{
		`\(\.\*\)\+`,  // (.*)+
		`\(\.\+\)\+`,  // (.+)+
		`\(\.\*\)\*`,  // (.*)*
		`\(\.\+\)\*`,  // (.+)*
		`\([^)]*\+\)\+`, // (x+)+ where x is any pattern
		`\([^)]*\*\)\+`, // (x*)+ where x is any pattern
	}

	for _, dangerous := range dangerousPatterns {
		matched, _ := regexp.MatchString(dangerous, pattern)
		if matched {
			return fmt.Errorf("regex pattern contains potentially dangerous nested quantifiers that could cause ReDoS")
		}
	}

	return nil
}

// LogPatternMonitor monitors log patterns in kernel messages and systemd journals
type LogPatternMonitor struct {
	name   string
	config *LogPatternMonitorConfig

	// Interface abstractions for testability
	fileReader      FileReader
	commandExecutor CommandExecutor

	// Thread-safe state tracking
	mu                     sync.RWMutex
	patternEventCount      map[string]int       // pattern name -> event count
	patternLastEvent       map[string]time.Time // pattern name -> last event time
	lastJournalCheck       map[string]time.Time // unit name -> last check time
	kmsgPermissionWarned   bool                 // Track if we've warned about kmsg permissions
	lastCleanup            time.Time            // Last time map cleanup was performed

	// BaseMonitor for lifecycle management
	*monitors.BaseMonitor
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

	// Create log pattern monitor
	monitor := &LogPatternMonitor{
		name:              config.Name,
		config:            logConfig,
		fileReader:        fileReader,
		commandExecutor:   commandExecutor,
		patternEventCount: make(map[string]int),
		patternLastEvent:  make(map[string]time.Time),
		lastJournalCheck:  make(map[string]time.Time),
		BaseMonitor:       baseMonitor,
	}

	// Set the check function
	if err := baseMonitor.SetCheckFunc(monitor.checkLogPatterns); err != nil {
		return nil, fmt.Errorf("failed to set check function: %w", err)
	}

	return monitor, nil
}

// checkLogPatterns performs the log pattern checking
func (m *LogPatternMonitor) checkLogPatterns(ctx context.Context) (*types.Status, error) {
	status := types.NewStatus(m.GetName())

	// Perform periodic cleanup of expired entries to prevent unbounded map growth
	m.cleanupExpiredEntries()

	// Check kmsg if enabled
	if m.config.CheckKmsg {
		m.checkKmsg(ctx, status)
	}

	// Check journal units if enabled
	if m.config.CheckJournal {
		for _, unit := range m.config.JournalUnits {
			m.checkJournalUnit(ctx, unit, status)
		}
	}

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
				status.AddEvent(types.NewEvent(
					types.EventWarning,
					"KmsgPermissionDenied",
					errMsg,
				))
			}
			return
		}

		// Other errors
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

		// Check if pattern matches
		if pattern.compiled.MatchString(line) {
			// Check rate limiting
			if !m.shouldReportEvent(pattern.Name) {
				continue
			}

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
// This function is thread-safe and consolidates map operations to minimize
// lock time and reduce race condition potential
func (m *LogPatternMonitor) shouldReportEvent(patternName string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	lastEvent, exists := m.patternLastEvent[patternName]

	// Check if we need to reset (outside dedup window)
	if !exists || now.Sub(lastEvent) > m.config.DedupWindow {
		// Reset to 1 (this event counts as first in new window)
		m.patternEventCount[patternName] = 1
		m.patternLastEvent[patternName] = now
		return true
	}

	// Check rate limit
	count := m.patternEventCount[patternName]
	if count >= m.config.MaxEventsPerPattern {
		return false
	}

	// Increment and update (single write path)
	m.patternEventCount[patternName] = count + 1
	m.patternLastEvent[patternName] = now
	return true
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

	// Validate and compile regex patterns
	for i := range c.Patterns {
		// Validate regex safety to prevent ReDoS attacks
		if err := validateRegexSafety(c.Patterns[i].Regex); err != nil {
			return fmt.Errorf("regex safety validation failed for pattern %s: %w", c.Patterns[i].Name, err)
		}

		compiled, err := regexp.Compile(c.Patterns[i].Regex)
		if err != nil {
			return fmt.Errorf("invalid regex in pattern %s: %w", c.Patterns[i].Name, err)
		}
		c.Patterns[i].compiled = compiled

		// Validate source
		source := c.Patterns[i].Source
		if source != "kmsg" && source != "journal" && source != "both" {
			return fmt.Errorf("invalid source in pattern %s: %s (must be kmsg, journal, or both)", c.Patterns[i].Name, source)
		}
	}

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
	monitors.Register(monitors.MonitorInfo{
		Type:        "custom-logpattern",
		Factory:     NewLogPatternMonitor,
		Validator:   ValidateLogPatternConfig,
		Description: "Monitors log patterns in kernel messages and systemd journals for known issues",
	})
}
