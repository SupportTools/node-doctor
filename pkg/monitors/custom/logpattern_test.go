package custom

import (
	"context"
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// TestParseLogPatternConfig tests configuration parsing
func TestParseLogPatternConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      map[string]interface{}
		expectError bool
		validate    func(*testing.T, *LogPatternMonitorConfig)
	}{
		{
			name:   "empty config",
			config: map[string]interface{}{},
			validate: func(t *testing.T, cfg *LogPatternMonitorConfig) {
				if len(cfg.Patterns) != 0 {
					t.Errorf("expected 0 patterns, got %d", len(cfg.Patterns))
				}
			},
		},
		{
			name: "valid single pattern",
			config: map[string]interface{}{
				"patterns": []interface{}{
					map[string]interface{}{
						"name":        "test-pattern",
						"regex":       "test.*error",
						"severity":    "error",
						"description": "Test error pattern",
						"source":      "kmsg",
					},
				},
			},
			validate: func(t *testing.T, cfg *LogPatternMonitorConfig) {
				if len(cfg.Patterns) != 1 {
					t.Fatalf("expected 1 pattern, got %d", len(cfg.Patterns))
				}
				p := cfg.Patterns[0]
				if p.Name != "test-pattern" {
					t.Errorf("expected name 'test-pattern', got '%s'", p.Name)
				}
				if p.Regex != "test.*error" {
					t.Errorf("expected regex 'test.*error', got '%s'", p.Regex)
				}
				if p.Severity != "error" {
					t.Errorf("expected severity 'error', got '%s'", p.Severity)
				}
			},
		},
		{
			name: "invalid patterns type",
			config: map[string]interface{}{
				"patterns": "not an array",
			},
			expectError: true,
		},
		{
			name: "missing pattern name",
			config: map[string]interface{}{
				"patterns": []interface{}{
					map[string]interface{}{
						"regex": "test",
					},
				},
			},
			expectError: true,
		},
		{
			name: "missing pattern regex",
			config: map[string]interface{}{
				"patterns": []interface{}{
					map[string]interface{}{
						"name": "test",
					},
				},
			},
			expectError: true,
		},
		{
			name: "with kmsg path",
			config: map[string]interface{}{
				"kmsgPath": "/custom/kmsg",
			},
			validate: func(t *testing.T, cfg *LogPatternMonitorConfig) {
				if cfg.KmsgPath != "/custom/kmsg" {
					t.Errorf("expected kmsgPath '/custom/kmsg', got '%s'", cfg.KmsgPath)
				}
			},
		},
		{
			name: "with journal units",
			config: map[string]interface{}{
				"journalUnits": []interface{}{"kubelet", "docker"},
			},
			validate: func(t *testing.T, cfg *LogPatternMonitorConfig) {
				if len(cfg.JournalUnits) != 2 {
					t.Fatalf("expected 2 journal units, got %d", len(cfg.JournalUnits))
				}
				if cfg.JournalUnits[0] != "kubelet" {
					t.Errorf("expected first unit 'kubelet', got '%s'", cfg.JournalUnits[0])
				}
			},
		},
		{
			name: "with rate limiting config",
			config: map[string]interface{}{
				"maxEventsPerPattern": 5,
				"dedupWindow":         "10m",
			},
			validate: func(t *testing.T, cfg *LogPatternMonitorConfig) {
				if cfg.MaxEventsPerPattern != 5 {
					t.Errorf("expected maxEventsPerPattern 5, got %d", cfg.MaxEventsPerPattern)
				}
				if cfg.DedupWindow != 10*time.Minute {
					t.Errorf("expected dedupWindow 10m, got %v", cfg.DedupWindow)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := parseLogPatternConfig(tt.config)
			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.validate != nil {
				tt.validate(t, cfg)
			}
		})
	}
}

// TestApplyDefaults tests default value application
func TestApplyDefaults(t *testing.T) {
	tests := []struct {
		name        string
		config      *LogPatternMonitorConfig
		expectError bool
		validate    func(*testing.T, *LogPatternMonitorConfig)
	}{
		{
			name:   "apply all defaults",
			config: &LogPatternMonitorConfig{},
			validate: func(t *testing.T, cfg *LogPatternMonitorConfig) {
				if cfg.KmsgPath != defaultKmsgPath {
					t.Errorf("expected default kmsgPath, got '%s'", cfg.KmsgPath)
				}
				if cfg.MaxEventsPerPattern != defaultMaxEventsPerPattern {
					t.Errorf("expected default maxEventsPerPattern %d, got %d", defaultMaxEventsPerPattern, cfg.MaxEventsPerPattern)
				}
				if cfg.DedupWindow != defaultDedupWindow {
					t.Errorf("expected default dedupWindow %v, got %v", defaultDedupWindow, cfg.DedupWindow)
				}
				// Note: CheckKmsg and CheckJournal are NOT automatically enabled
				// Users must explicitly enable at least one source
			},
		},
		{
			name: "compile regex patterns",
			config: &LogPatternMonitorConfig{
				Patterns: []LogPatternConfig{
					{Name: "test", Regex: "test.*error", Severity: "error", Source: "kmsg"},
				},
			},
			validate: func(t *testing.T, cfg *LogPatternMonitorConfig) {
				if cfg.Patterns[0].compiled == nil {
					t.Error("expected regex to be compiled")
				}
				if !cfg.Patterns[0].compiled.MatchString("test error") {
					t.Error("compiled regex should match 'test error'")
				}
			},
		},
		{
			name: "invalid regex pattern",
			config: &LogPatternMonitorConfig{
				Patterns: []LogPatternConfig{
					{Name: "test", Regex: "[invalid(regex", Severity: "error", Source: "kmsg"},
				},
			},
			expectError: true,
		},
		{
			name: "invalid source",
			config: &LogPatternMonitorConfig{
				Patterns: []LogPatternConfig{
					{Name: "test", Regex: "test", Severity: "error", Source: "invalid"},
				},
			},
			expectError: true,
		},
		{
			name: "merge with defaults",
			config: &LogPatternMonitorConfig{
				UseDefaults: true,
				Patterns: []LogPatternConfig{
					{Name: "custom", Regex: "custom.*pattern", Severity: "warning", Source: "both"},
				},
			},
			validate: func(t *testing.T, cfg *LogPatternMonitorConfig) {
				// Should have custom pattern plus defaults
				if len(cfg.Patterns) <= 1 {
					t.Errorf("expected more than 1 pattern after merge, got %d", len(cfg.Patterns))
				}
				// First should be custom
				if cfg.Patterns[0].Name != "custom" {
					t.Errorf("expected first pattern to be 'custom', got '%s'", cfg.Patterns[0].Name)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.applyDefaults()
			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.validate != nil {
				tt.validate(t, tt.config)
			}
		})
	}
}

// TestPatternMatching tests log pattern matching
func TestPatternMatching(t *testing.T) {
	tests := []struct {
		name        string
		logLine     string
		pattern     LogPatternConfig
		shouldMatch bool
	}{
		{
			name:    "OOM killer match",
			logLine: testOOMKillLog,
			pattern: LogPatternConfig{
				Name:   "oom-killer",
				Regex:  "(Out of memory|Killed process)",
				Source: "kmsg",
			},
			shouldMatch: true,
		},
		{
			name:    "disk error match",
			logLine: testDiskErrorLog,
			pattern: LogPatternConfig{
				Name:   "disk-error",
				Regex:  "EXT4-fs.*error",
				Source: "kmsg",
			},
			shouldMatch: true,
		},
		{
			name:    "network timeout match",
			logLine: testNetworkErrorLog,
			pattern: LogPatternConfig{
				Name:   "network-timeout",
				Regex:  "NETDEV WATCHDOG.*timed out",
				Source: "kmsg",
			},
			shouldMatch: true,
		},
		{
			name:    "no match",
			logLine: testNormalLog,
			pattern: LogPatternConfig{
				Name:   "error-pattern",
				Regex:  "ERROR|CRITICAL",
				Source: "kmsg",
			},
			shouldMatch: false,
		},
		{
			name:    "kubelet error match",
			logLine: testKubeletErrorLog,
			pattern: LogPatternConfig{
				Name:   "kubelet-cni-error",
				Regex:  "kubelet.*Failed",
				Source: "journal",
			},
			shouldMatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Compile the pattern
			compiled, err := regexp.Compile(tt.pattern.Regex)
			if err != nil {
				t.Fatalf("failed to compile regex: %v", err)
			}
			tt.pattern.compiled = compiled

			matches := tt.pattern.compiled.MatchString(tt.logLine)
			if matches != tt.shouldMatch {
				t.Errorf("expected match=%v, got %v for line: %s", tt.shouldMatch, matches, tt.logLine)
			}
		})
	}
}

// TestRateLimiting tests event rate limiting
func TestRateLimiting(t *testing.T) {
	config := &LogPatternMonitorConfig{
		MaxEventsPerPattern: 3,
		DedupWindow:         1 * time.Second,
	}

	monitor := &LogPatternMonitor{
		config:            config,
		patternEventCount: make(map[string]int),
		patternLastEvent:  make(map[string]time.Time),
	}

	patternName := "test-pattern"

	// First 3 events should be allowed
	for i := 0; i < 3; i++ {
		reported, suppressed := monitor.shouldReportEvent(patternName)
		if !reported || suppressed {
			t.Errorf("event %d should be allowed (reported=%v, suppressed=%v)", i+1, reported, suppressed)
		}
	}

	// 4th event should be blocked (suppressed)
	reported, suppressed := monitor.shouldReportEvent(patternName)
	if reported || !suppressed {
		t.Errorf("4th event should be blocked by rate limit (reported=%v, suppressed=%v)", reported, suppressed)
	}

	// Wait for dedup window to expire
	time.Sleep(1100 * time.Millisecond)

	// After window expires, should allow events again
	reported, suppressed = monitor.shouldReportEvent(patternName)
	if !reported || suppressed {
		t.Errorf("event should be allowed after dedup window expires (reported=%v, suppressed=%v)", reported, suppressed)
	}
}

// TestKmsgCheck tests kernel message checking
func TestKmsgCheck(t *testing.T) {
	tests := []struct {
		name           string
		kmsgContent    string
		kmsgError      error
		patterns       []LogPatternConfig
		expectEvents   int
		expectWarnings bool
	}{
		{
			name:        "successful OOM detection",
			kmsgContent: createTestKmsgContent(testOOMKillLog, testNormalLog),
			patterns: []LogPatternConfig{
				{Name: "oom-killer", Regex: "Out of memory", Severity: "error", Source: "kmsg"},
			},
			expectEvents: 1,
		},
		{
			name:        "multiple pattern matches",
			kmsgContent: createTestKmsgContent(testOOMKillLog, testDiskErrorLog, testNetworkErrorLog),
			patterns: []LogPatternConfig{
				{Name: "oom-killer", Regex: "Out of memory", Severity: "error", Source: "kmsg"},
				{Name: "disk-error", Regex: "EXT4-fs.*error", Severity: "error", Source: "kmsg"},
				{Name: "network-error", Regex: "NETDEV WATCHDOG", Severity: "warning", Source: "kmsg"},
			},
			expectEvents: 3,
		},
		{
			name:           "permission denied",
			kmsgError:      os.ErrPermission,
			patterns:       []LogPatternConfig{},
			expectEvents:   0,
			expectWarnings: true,
		},
		{
			name:        "no matches",
			kmsgContent: createTestKmsgContent(testNormalLog),
			patterns: []LogPatternConfig{
				{Name: "error-pattern", Regex: "ERROR|CRITICAL", Severity: "error", Source: "kmsg"},
			},
			expectEvents: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mocks
			mockReader := newMockFileReader()
			mockKmsg := newMockKmsgReader()
			if tt.kmsgError != nil {
				mockKmsg.setReadError("/dev/kmsg", tt.kmsgError)
			} else {
				mockKmsg.setContent("/dev/kmsg", tt.kmsgContent)
			}

			// Compile patterns
			for i := range tt.patterns {
				compiled, err := regexp.Compile(tt.patterns[i].Regex)
				if err != nil {
					t.Fatalf("failed to compile regex: %v", err)
				}
				tt.patterns[i].compiled = compiled
			}

			// Create monitor
			config := &LogPatternMonitorConfig{
				CheckKmsg:           true,
				KmsgPath:            "/dev/kmsg",
				Patterns:            tt.patterns,
				MaxEventsPerPattern: 10,
				DedupWindow:         5 * time.Minute,
			}

			monitor := &LogPatternMonitor{
				name:              "test-monitor",
				config:            config,
				fileReader:        mockReader,
				kmsgReader:        mockKmsg,
				patternEventCount: make(map[string]int),
				patternLastEvent:  make(map[string]time.Time),
				metrics: LogPatternMetrics{
					PatternsMatched:        make(map[string]int),
					PatternsSuppressed:     make(map[string]int),
					JournalCheckDurationMs: make(map[string]float64),
				},
			}

			// Run check
			status := types.NewStatus("test-monitor")
			monitor.checkKmsg(context.Background(), status)

			// Verify results
			eventCount := len(status.Events)
			if tt.expectWarnings {
				if eventCount == 0 {
					t.Error("expected warning event for permission denied")
				}
			} else {
				if eventCount != tt.expectEvents {
					t.Errorf("expected %d events, got %d", tt.expectEvents, eventCount)
					for i, event := range status.Events {
						t.Logf("Event %d: %s - %s", i, event.Reason, event.Message)
					}
				}
			}
		})
	}
}

// TestJournalCheck tests systemd journal checking
func TestJournalCheck(t *testing.T) {
	tests := []struct {
		name          string
		unit          string
		journalOutput string
		journalError  error
		patterns      []LogPatternConfig
		expectEvents  int
	}{
		{
			name:          "kubelet error detection",
			unit:          "kubelet",
			journalOutput: createTestJournalContent(testKubeletErrorLog, "normal log line"),
			patterns: []LogPatternConfig{
				{Name: "kubelet-test", Regex: "kubelet.*Failed", Severity: "error", Source: "journal"},
			},
			expectEvents: 1,
		},
		{
			name:          "multiple service errors",
			unit:          "containerd",
			journalOutput: createTestJournalContent(testContainerdLog, testDockerErrorLog),
			patterns: []LogPatternConfig{
				{Name: "containerd-error", Regex: "containerd.*error", Severity: "error", Source: "journal"},
				{Name: "docker-error", Regex: "dockerd.*Failed", Severity: "error", Source: "journal"},
			},
			expectEvents: 2,
		},
		{
			name:         "journalctl command error",
			unit:         "kubelet",
			journalError: errors.New("journalctl failed"),
			patterns:     []LogPatternConfig{},
			expectEvents: 0, // Error logged as warning, no pattern matches
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mocks
			mockExecutor := newMockCommandExecutor()
			if tt.journalError != nil {
				mockExecutor.setError("journalctl", []string{"-u", tt.unit, "--no-pager", "-o", "cat", "-n", "100"}, tt.journalError)
			} else {
				mockExecutor.setOutput("journalctl", []string{"-u", tt.unit, "--no-pager", "-o", "cat", "-n", "100"}, tt.journalOutput)
			}

			// Compile patterns
			for i := range tt.patterns {
				compiled, err := regexp.Compile(tt.patterns[i].Regex)
				if err != nil {
					t.Fatalf("failed to compile regex: %v", err)
				}
				tt.patterns[i].compiled = compiled
			}

			// Create monitor
			config := &LogPatternMonitorConfig{
				CheckJournal:        true,
				JournalUnits:        []string{tt.unit},
				Patterns:            tt.patterns,
				MaxEventsPerPattern: 10,
				DedupWindow:         5 * time.Minute,
			}

			monitor := &LogPatternMonitor{
				name:              "test-monitor",
				config:            config,
				commandExecutor:   mockExecutor,
				patternEventCount: make(map[string]int),
				patternLastEvent:  make(map[string]time.Time),
				lastJournalCheck:  make(map[string]time.Time),
				metrics: LogPatternMetrics{
					PatternsMatched:        make(map[string]int),
					PatternsSuppressed:     make(map[string]int),
					JournalCheckDurationMs: make(map[string]float64),
				},
			}

			// Run check
			status := types.NewStatus("test-monitor")
			monitor.checkJournalUnit(context.Background(), tt.unit, status)

			// Verify results
			patternEventCount := 0
			for _, event := range status.Events {
				// Count non-warning events (pattern matches)
				if event.Severity != types.EventWarning {
					patternEventCount++
				}
			}

			if patternEventCount != tt.expectEvents {
				t.Errorf("expected %d pattern match events, got %d", tt.expectEvents, patternEventCount)
				for i, event := range status.Events {
					t.Logf("Event %d: %s (%s) - %s", i, event.Reason, event.Severity, event.Message)
				}
			}
		})
	}
}

// TestLogPatternMonitorIntegration tests the full monitor lifecycle
func TestLogPatternMonitorIntegration(t *testing.T) {
	// Setup mocks
	mockReader := newMockFileReader()
	mockKmsg := newMockKmsgReader()
	mockKmsg.setContent("/dev/kmsg", createTestKmsgContent(testOOMKillLog, testDiskErrorLog))

	mockExecutor := newMockCommandExecutor()
	mockExecutor.setOutput("journalctl",
		[]string{"-u", "kubelet", "--no-pager", "-o", "cat", "-n", "100"},
		createTestJournalContent(testKubeletErrorLog))

	// Create monitor config
	config := types.MonitorConfig{
		Name:     "integration-test",
		Type:     "custom-logpattern",
		Interval: 100 * time.Millisecond,
		Timeout:  50 * time.Millisecond,
		Config: map[string]interface{}{
			"checkKmsg":    true,
			"checkJournal": true,
			"journalUnits": []interface{}{"kubelet"},
			"patterns": []interface{}{
				map[string]interface{}{
					"name":        "oom-killer",
					"regex":       "Out of memory",
					"severity":    "error",
					"description": "OOM killer activated",
					"source":      "kmsg",
				},
				map[string]interface{}{
					"name":        "kubelet-cni-error",
					"regex":       "kubelet.*Failed",
					"severity":    "error",
					"description": "Kubelet error",
					"source":      "journal",
				},
			},
		},
	}

	// Create monitor
	ctx := context.Background()
	logConfig, err := parseLogPatternConfig(config.Config)
	if err != nil {
		t.Fatalf("failed to parse config: %v", err)
	}

	if err := logConfig.applyDefaults(); err != nil {
		t.Fatalf("failed to apply defaults: %v", err)
	}

	monitor, err := NewLogPatternMonitorWithDependencies(ctx, config, logConfig, mockReader, mockKmsg, mockExecutor)
	if err != nil {
		t.Fatalf("failed to create monitor: %v", err)
	}

	// Start monitor
	statusCh, err := monitor.Start()
	if err != nil {
		t.Fatalf("failed to start monitor: %v", err)
	}

	// Wait for status
	select {
	case status := <-statusCh:
		if status == nil {
			t.Fatal("received nil status")
		}

		// Should have events from both kmsg and journal
		if len(status.Events) < 2 {
			t.Errorf("expected at least 2 events, got %d", len(status.Events))
		}

		// Verify event types
		foundOOM := false
		foundKubelet := false
		for _, event := range status.Events {
			if strings.Contains(event.Reason, "oom-killer") {
				foundOOM = true
			}
			if strings.Contains(event.Reason, "kubelet-cni-error") {
				foundKubelet = true
			}
		}

		if !foundOOM {
			t.Error("expected OOM killer event")
		}
		if !foundKubelet {
			t.Error("expected kubelet error event")
		}

	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for status")
	}

	// Stop monitor
	monitor.Stop()
}

// TestValidateLogPatternConfig tests configuration validation
func TestValidateLogPatternConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      types.MonitorConfig
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid configuration",
			config: types.MonitorConfig{
				Name:     "test",
				Type:     "custom-logpattern",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Config: map[string]interface{}{
					"checkKmsg":    true,
					"checkJournal": true,
					"journalUnits": []interface{}{"kubelet"},
					"patterns": []interface{}{
						map[string]interface{}{
							"name":  "test",
							"regex": "test",
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "no patterns configured",
			config: types.MonitorConfig{
				Name:     "test",
				Type:     "custom-logpattern",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Config: map[string]interface{}{
					"checkKmsg": true,
				},
			},
			expectError: true,
			errorMsg:    "at least one log pattern",
		},
		{
			name: "no sources enabled",
			config: types.MonitorConfig{
				Name:     "test",
				Type:     "custom-logpattern",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Config: map[string]interface{}{
					"checkKmsg":    false,
					"checkJournal": false,
					"patterns": []interface{}{
						map[string]interface{}{
							"name":  "test",
							"regex": "test",
						},
					},
				},
			},
			expectError: true,
			errorMsg:    "at least one log source",
		},
		{
			name: "journal enabled but no units",
			config: types.MonitorConfig{
				Name:     "test",
				Type:     "custom-logpattern",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Config: map[string]interface{}{
					"checkJournal": true,
					"patterns": []interface{}{
						map[string]interface{}{
							"name":  "test",
							"regex": "test",
						},
					},
				},
			},
			expectError: true,
			errorMsg:    "journalUnits must be specified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateLogPatternConfig(tt.config)
			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing '%s', got '%v'", tt.errorMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestSeverityParsing tests severity string parsing
func TestSeverityParsing(t *testing.T) {
	monitor := &LogPatternMonitor{}

	tests := []struct {
		input    string
		expected types.EventSeverity
	}{
		{"error", types.EventError},
		{"ERROR", types.EventError},
		{"warning", types.EventWarning},
		{"WARNING", types.EventWarning},
		{"info", types.EventInfo},
		{"INFO", types.EventInfo},
		{"unknown", types.EventWarning}, // Default to warning
		{"", types.EventWarning},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := monitor.parseSeverity(tt.input)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

// TestBufferSizeLimit tests protection against excessive memory usage
func TestBufferSizeLimit(t *testing.T) {
	// Create large kmsg content (> 1MB)
	largeContent := strings.Repeat("test log line\n", 100000) // ~1.5MB

	mockReader := newMockFileReader()
	mockKmsg := newMockKmsgReader()
	mockKmsg.setContent("/dev/kmsg", largeContent)

	config := &LogPatternMonitorConfig{
		CheckKmsg:           true,
		KmsgPath:            "/dev/kmsg",
		Patterns:            []LogPatternConfig{},
		MaxEventsPerPattern: 10,
		DedupWindow:         5 * time.Minute,
	}

	monitor := &LogPatternMonitor{
		name:              "test-monitor",
		config:            config,
		fileReader:        mockReader,
		kmsgReader:        mockKmsg,
		patternEventCount: make(map[string]int),
		patternLastEvent:  make(map[string]time.Time),
	}

	status := types.NewStatus("test-monitor")
	monitor.checkKmsg(context.Background(), status)

	// Should have warning about buffer size
	foundWarning := false
	for _, event := range status.Events {
		if strings.Contains(event.Reason, "KmsgBufferTooLarge") {
			foundWarning = true
			break
		}
	}

	if !foundWarning {
		t.Error("expected warning about large kmsg buffer")
	}
}

// TestApplyDefaults_ResourceLimits tests resource limit validation
func TestApplyDefaults_ResourceLimits(t *testing.T) {
	tests := []struct {
		name        string
		config      *LogPatternMonitorConfig
		expectError bool
		errorMsg    string
	}{
		{
			name: "too many patterns",
			config: &LogPatternMonitorConfig{
				Patterns: func() []LogPatternConfig {
					patterns := make([]LogPatternConfig, 121)
					for i := range patterns {
						patterns[i] = LogPatternConfig{
							Name:     "test",
							Regex:    "test",
							Source:   "kmsg",
							Severity: "error",
						}
					}
					return patterns
				}(),
				MaxEventsPerPattern: 10,
				DedupWindow:         5 * time.Minute,
			},
			expectError: true,
			errorMsg:    "exceeds maximum limit of 120",
		},
		{
			name: "too many journal units",
			config: &LogPatternMonitorConfig{
				Patterns: []LogPatternConfig{{
					Name:     "test",
					Regex:    "test",
					Source:   "journal",
					Severity: "error",
				}},
				JournalUnits:        make([]string, 21),
				MaxEventsPerPattern: 10,
				DedupWindow:         5 * time.Minute,
			},
			expectError: true,
			errorMsg:    "exceeds maximum limit of 20",
		},
		{
			name: "maxEventsPerPattern uses default when zero",
			config: &LogPatternMonitorConfig{
				Patterns: []LogPatternConfig{{
					Name:     "test",
					Regex:    "test",
					Source:   "kmsg",
					Severity: "error",
				}},
				MaxEventsPerPattern: 0, // Will be defaulted to 10
				DedupWindow:         5 * time.Minute,
			},
			expectError: false, // 0 gets defaulted to 10 which is valid
		},
		{
			name: "maxEventsPerPattern too high",
			config: &LogPatternMonitorConfig{
				Patterns: []LogPatternConfig{{
					Name:     "test",
					Regex:    "test",
					Source:   "kmsg",
					Severity: "error",
				}},
				MaxEventsPerPattern: 1001,
				DedupWindow:         5 * time.Minute,
			},
			expectError: true,
			errorMsg:    "must be between 1 and 1000",
		},
		{
			name: "dedupWindow too short",
			config: &LogPatternMonitorConfig{
				Patterns: []LogPatternConfig{{
					Name:     "test",
					Regex:    "test",
					Source:   "kmsg",
					Severity: "error",
				}},
				MaxEventsPerPattern: 10,
				DedupWindow:         500 * time.Millisecond,
			},
			expectError: true,
			errorMsg:    "must be between 1s and 1h0m0s",
		},
		{
			name: "dedupWindow too long",
			config: &LogPatternMonitorConfig{
				Patterns: []LogPatternConfig{{
					Name:     "test",
					Regex:    "test",
					Source:   "kmsg",
					Severity: "error",
				}},
				MaxEventsPerPattern: 10,
				DedupWindow:         2 * time.Hour,
			},
			expectError: true,
			errorMsg:    "must be between 1s and 1h0m0s",
		},
		{
			name: "valid maximum configuration",
			config: &LogPatternMonitorConfig{
				Patterns: func() []LogPatternConfig {
					patterns := make([]LogPatternConfig, 60)
					for i := range patterns {
						patterns[i] = LogPatternConfig{
							Name:     "test",
							Regex:    "test",
							Source:   "kmsg",
							Severity: "error",
						}
					}
					return patterns
				}(),
				JournalUnits:        make([]string, 20),
				MaxEventsPerPattern: 1000,
				DedupWindow:         1 * time.Hour,
			},
			expectError: false,
		},
		{
			name: "valid minimum configuration",
			config: &LogPatternMonitorConfig{
				Patterns: []LogPatternConfig{{
					Name:     "test",
					Regex:    "test",
					Source:   "kmsg",
					Severity: "error",
				}},
				MaxEventsPerPattern: 1,
				DedupWindow:         1 * time.Second,
			},
			expectError: false,
		},
		{
			name: "valid typical configuration",
			config: &LogPatternMonitorConfig{
				Patterns: []LogPatternConfig{
					{Name: "oom", Regex: "Out of memory", Source: "kmsg", Severity: "error"},
					{Name: "disk", Regex: "I/O error", Source: "kmsg", Severity: "error"},
					{Name: "network", Regex: "timeout", Source: "both", Severity: "warning"},
				},
				JournalUnits:        []string{"kubelet", "containerd", "docker"},
				MaxEventsPerPattern: 50,
				DedupWindow:         10 * time.Minute,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.applyDefaults()
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errorMsg)
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestEstimateMemoryUsage tests memory estimation calculations
func TestEstimateMemoryUsage(t *testing.T) {
	tests := []struct {
		name         string
		config       *LogPatternMonitorConfig
		expectWithin []int // [min, max] expected memory range in bytes
	}{
		{
			name: "minimal configuration",
			config: &LogPatternMonitorConfig{
				Patterns: []LogPatternConfig{{
					Name:     "test",
					Regex:    "test",
					Source:   "kmsg",
					Severity: "error",
				}},
				MaxEventsPerPattern: 10,
				DedupWindow:         5 * time.Minute,
			},
			expectWithin: []int{1024 * 500, 1024 * 1536}, // ~0.5-1.5MB
		},
		{
			name: "medium configuration",
			config: &LogPatternMonitorConfig{
				Patterns: []LogPatternConfig{
					{Name: "p1", Regex: "test1", Source: "kmsg", Severity: "error"},
					{Name: "p2", Regex: "test2", Source: "kmsg", Severity: "error"},
					{Name: "p3", Regex: "test3", Source: "kmsg", Severity: "error"},
					{Name: "p4", Regex: "test4", Source: "kmsg", Severity: "error"},
					{Name: "p5", Regex: "test5", Source: "kmsg", Severity: "error"},
				},
				JournalUnits:        []string{"kubelet", "containerd", "docker"},
				MaxEventsPerPattern: 100,
				DedupWindow:         10 * time.Minute,
			},
			expectWithin: []int{1024 * 1024, 1024 * 1024 * 3}, // ~1-3MB
		},
		{
			name: "large configuration approaching limits",
			config: &LogPatternMonitorConfig{
				Patterns: func() []LogPatternConfig {
					patterns := make([]LogPatternConfig, 30)
					for i := range patterns {
						patterns[i] = LogPatternConfig{
							Name:     "test",
							Regex:    "test",
							Source:   "kmsg",
							Severity: "error",
						}
					}
					return patterns
				}(),
				JournalUnits:        make([]string, 15),
				MaxEventsPerPattern: 500,
				DedupWindow:         30 * time.Minute,
			},
			expectWithin: []int{1024 * 1024 * 2, 1024 * 1024 * 10}, // ~2-10MB (should be under limit)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			memory := tt.config.estimateMemoryUsage()
			if memory < tt.expectWithin[0] || memory > tt.expectWithin[1] {
				t.Errorf("memory estimate %d bytes outside expected range [%d, %d]",
					memory, tt.expectWithin[0], tt.expectWithin[1])
			}

			// Verify it's under the maximum limit
			if memory > maxMemoryPerCheck {
				t.Errorf("memory estimate %d bytes exceeds maximum limit %d",
					memory, maxMemoryPerCheck)
			}
		})
	}
}

// TestResourceLimits_WithDefaults tests resource limits with default patterns
func TestResourceLimits_WithDefaults(t *testing.T) {
	tests := []struct {
		name         string
		userPatterns int
		useDefaults  bool
		expectError  bool
		errorMsg     string
	}{
		{
			name:         "user patterns + defaults within limit",
			userPatterns: 62, // 62 user + 58 defaults = 120 (at limit)
			useDefaults:  true,
			expectError:  false,
		},
		{
			name:         "user patterns + defaults exceeds limit",
			userPatterns: 65, // 65 user + 58 defaults = 123 > 120 (exceeds)
			useDefaults:  true,
			expectError:  true,
			errorMsg:     "exceeds maximum limit of 120",
		},
		{
			name:         "defaults only within limit",
			userPatterns: 0,
			useDefaults:  true,
			expectError:  false,
		},
		{
			name:         "user patterns only at limit",
			userPatterns: 120,
			useDefaults:  false,
			expectError:  false,
		},
		{
			name:         "user patterns only exceeds limit",
			userPatterns: 121,
			useDefaults:  false,
			expectError:  true,
			errorMsg:     "exceeds maximum limit of 120",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patterns := make([]LogPatternConfig, tt.userPatterns)
			for i := range patterns {
				patterns[i] = LogPatternConfig{
					Name:     "test",
					Regex:    "test",
					Source:   "kmsg",
					Severity: "error",
				}
			}

			config := &LogPatternMonitorConfig{
				Patterns:            patterns,
				UseDefaults:         tt.useDefaults,
				MaxEventsPerPattern: 10,
				DedupWindow:         5 * time.Minute,
			}

			err := config.applyDefaults()
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errorMsg)
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestNewLogPatternMonitor tests the factory function for creating monitors
func TestNewLogPatternMonitor(t *testing.T) {
	tests := []struct {
		name        string
		monitorName string
		config      *LogPatternMonitorConfig
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid basic config",
			monitorName: "test-monitor",
			config: &LogPatternMonitorConfig{
				Patterns: []LogPatternConfig{
					{
						Regex:       `ERROR:`,
						Severity:    "error",
						Source:      "kmsg",
						Description: "Error found",
					},
				},
				CheckKmsg:    true,
				CheckJournal: false,
			},
			expectError: false,
		},
		{
			name:        "valid with defaults enabled",
			monitorName: "test-defaults",
			config: &LogPatternMonitorConfig{
				UseDefaults:  true,
				CheckKmsg:    true,
				CheckJournal: false,
			},
			expectError: false,
		},
		{
			name:        "invalid pattern - nested quantifiers",
			monitorName: "test-invalid",
			config: &LogPatternMonitorConfig{
				Patterns: []LogPatternConfig{
					{
						Regex:       `(a+)+`, // Nested quantifiers - should fail
						Severity:    "error",
						Source:      "kmsg",
						Description: "Test",
					},
				},
				CheckKmsg: true,
			},
			expectError: true,
			errorMsg:    "nested",
		},
		{
			name:        "invalid pattern - quantified adjacency",
			monitorName: "test-adjacency",
			config: &LogPatternMonitorConfig{
				Patterns: []LogPatternConfig{
					{
						Regex:       `.*.*`, // Quantified adjacency - should fail
						Severity:    "warning",
						Source:      "kmsg",
						Description: "Test",
					},
				},
				CheckKmsg: true,
			},
			expectError: true,
			errorMsg:    "quantified adjacencies",
		},
		{
			name:        "pattern too long",
			monitorName: "test-toolong",
			config: &LogPatternMonitorConfig{
				Patterns: []LogPatternConfig{
					{
						Regex:       strings.Repeat("a", 1001), // Over 1000 char limit
						Severity:    "error",
						Source:      "kmsg",
						Description: "Test",
					},
				},
				CheckKmsg: true,
			},
			expectError: true,
			errorMsg:    "exceeds maximum length",
		},
		{
			name:        "empty config with neither kmsg nor journal",
			monitorName: "test-empty",
			config: &LogPatternMonitorConfig{
				Patterns: []LogPatternConfig{
					{
						Regex:       `test`,
						Severity:    "info",
						Source:      "kmsg",
						Description: "Test",
					},
				},
				CheckKmsg:    false,
				CheckJournal: false,
			},
			expectError: false, // Should succeed, just won't check anything
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			mockReader := newMockFileReader()
			mockExecutor := &mockCommandExecutor{}

			monitorConfig := types.MonitorConfig{
				Name:     tt.monitorName,
				Type:     "log-pattern",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Config:   map[string]interface{}{},
			}

			err := tt.config.applyDefaults()
			if tt.expectError {
				// If we expect an error, check if applyDefaults or monitor creation fails
				if err != nil {
					// applyDefaults failed as expected
					if !strings.Contains(err.Error(), tt.errorMsg) {
						t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
					}
					return
				}
				// applyDefaults succeeded, monitor creation should fail
			} else if err != nil {
				t.Fatalf("failed to apply defaults: %v", err)
			}

			monitor, err := NewLogPatternMonitorWithDependencies(ctx, monitorConfig, tt.config, mockReader, newMockKmsgReader(), mockExecutor)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errorMsg)
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
				if monitor != nil {
					t.Error("expected nil monitor on error")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if monitor == nil {
					t.Fatal("expected non-nil monitor")
				}
			}
		})
	}
}

// TestShouldCheckPattern tests the pattern source filtering logic
func TestShouldCheckPattern(t *testing.T) {
	tests := []struct {
		name           string
		patternSource  string
		checkSource    string
		expectedResult bool
	}{
		{
			name:           "kmsg pattern for kmsg source",
			patternSource:  "kmsg",
			checkSource:    "kmsg",
			expectedResult: true,
		},
		{
			name:           "kmsg pattern for journal source",
			patternSource:  "kmsg",
			checkSource:    "journal",
			expectedResult: false,
		},
		{
			name:           "journal pattern for journal source",
			patternSource:  "journal",
			checkSource:    "journal",
			expectedResult: true,
		},
		{
			name:           "journal pattern for kmsg source",
			patternSource:  "journal",
			checkSource:    "kmsg",
			expectedResult: false,
		},
		{
			name:           "both pattern for kmsg source",
			patternSource:  "both",
			checkSource:    "kmsg",
			expectedResult: true,
		},
		{
			name:           "both pattern for journal source",
			patternSource:  "both",
			checkSource:    "journal",
			expectedResult: true,
		},
		{
			name:           "default (empty) pattern for kmsg source",
			patternSource:  "",
			checkSource:    "kmsg",
			expectedResult: true, // Default is to check all sources
		},
		{
			name:           "default (empty) pattern for journal source",
			patternSource:  "",
			checkSource:    "journal",
			expectedResult: true, // Default is to check all sources
		},
		{
			name:           "custom pattern for kmsg source",
			patternSource:  "custom",
			checkSource:    "kmsg",
			expectedResult: true, // Unknown sources default to checking all
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// For edge cases with invalid sources, test shouldCheckPattern directly
			// without going through validation
			if tt.patternSource == "" || tt.patternSource == "custom" {
				// Create pattern directly
				pattern := &LogPatternConfig{
					Regex:       `test`,
					Severity:    "info",
					Source:      tt.patternSource,
					Description: "Test",
				}

				// Create a minimal monitor just to call shouldCheckPattern
				monitor := &LogPatternMonitor{
					config: &LogPatternMonitorConfig{
						Patterns: []LogPatternConfig{*pattern},
					},
				}

				result := monitor.shouldCheckPattern(pattern, tt.checkSource)
				if result != tt.expectedResult {
					t.Errorf("shouldCheckPattern(%q, %q) = %v, want %v",
						tt.patternSource, tt.checkSource, result, tt.expectedResult)
				}
				return
			}

			ctx := context.Background()
			mockReader := newMockFileReader()
			mockExecutor := &mockCommandExecutor{}

			// Create a minimal monitor
			logConfig := &LogPatternMonitorConfig{
				Patterns: []LogPatternConfig{
					{
						Regex:       `test`,
						Severity:    "info",
						Description: "Test",
						Source:      tt.patternSource,
					},
				},
				CheckKmsg:    true,
				CheckJournal: false,
			}

			err := logConfig.applyDefaults()
			if err != nil {
				t.Fatalf("failed to apply defaults: %v", err)
			}

			monitorConfig := types.MonitorConfig{
				Name:     "test",
				Type:     "log-pattern",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Config:   map[string]interface{}{},
			}

			mon, err := NewLogPatternMonitorWithDependencies(ctx, monitorConfig, logConfig, mockReader, newMockKmsgReader(), mockExecutor)
			if err != nil {
				t.Fatalf("failed to create monitor: %v", err)
			}

			monitor := mon.(*LogPatternMonitor)
			pattern := &monitor.config.Patterns[0]
			result := monitor.shouldCheckPattern(pattern, tt.checkSource)

			if result != tt.expectedResult {
				t.Errorf("shouldCheckPattern(%q, %q) = %v, want %v",
					tt.patternSource, tt.checkSource, result, tt.expectedResult)
			}
		})
	}
}

// TestCleanupExpiredEntries tests the map cleanup logic
func TestCleanupExpiredEntries(t *testing.T) {
	tests := []struct {
		name          string
		dedupWindow   time.Duration
		setupTime     time.Duration
		waitTime      time.Duration
		expectCleanup bool
	}{
		{
			name:          "cleanup not triggered - too soon",
			dedupWindow:   5 * time.Minute,
			setupTime:     0,
			waitTime:      1 * time.Minute,
			expectCleanup: false, // Less than 10 minutes
		},
		{
			name:          "cleanup triggered - after 10 minutes",
			dedupWindow:   1 * time.Second, // Short for testing
			setupTime:     0,
			waitTime:      11 * time.Minute, // Simulated time
			expectCleanup: true,
		},
		{
			name:          "entries not expired - within 2x dedup window",
			dedupWindow:   10 * time.Minute,
			setupTime:     0,
			waitTime:      15 * time.Minute, // Less than 2x dedup window
			expectCleanup: false,            // Entries not old enough
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			mockFS := newMockFileReader()
			mockKmsg := newMockKmsgReader()
			mockKmsg.setContent("/dev/kmsg", "ERROR: test\n")
			mockExecutor := &mockCommandExecutor{}

			logConfig := &LogPatternMonitorConfig{
				Patterns: []LogPatternConfig{
					{
						Regex:       `ERROR:`,
						Severity:    "error",
						Source:      "kmsg",
						Description: "Error",
					},
				},
				KmsgPath:            "/dev/kmsg",
				CheckKmsg:           true,
				CheckJournal:        false,
				MaxEventsPerPattern: 10,
				DedupWindow:         tt.dedupWindow,
			}

			err := logConfig.applyDefaults()
			if err != nil {
				t.Fatalf("failed to apply defaults: %v", err)
			}

			monitorConfig := types.MonitorConfig{
				Name:     "test-cleanup",
				Type:     "log-pattern",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Config:   map[string]interface{}{},
			}

			mon, err := NewLogPatternMonitorWithDependencies(ctx, monitorConfig, logConfig, mockFS, mockKmsg, mockExecutor)
			if err != nil {
				t.Fatalf("failed to create monitor: %v", err)
			}

			monitor := mon.(*LogPatternMonitor)

			// Run initial check to populate maps
			_, err = monitor.checkLogPatterns(ctx)
			if err != nil {
				t.Fatalf("initial check failed: %v", err)
			}

			// Verify maps are populated
			monitor.mu.Lock()
			initialCount := len(monitor.patternLastEvent)
			monitor.mu.Unlock()

			if initialCount == 0 {
				t.Fatal("expected some entries in patternLastEvent map")
			}

			// Manually adjust lastCleanup to simulate time passage
			monitor.mu.Lock()
			if tt.waitTime > 0 {
				monitor.lastCleanup = time.Now().Add(-tt.waitTime)
			}

			// Manually adjust entry timestamps to simulate age
			if tt.expectCleanup {
				// Make entries old enough to be cleaned up
				expiryTime := time.Now().Add(-tt.dedupWindow * 3)
				for pattern := range monitor.patternLastEvent {
					monitor.patternLastEvent[pattern] = expiryTime
				}
			}
			monitor.mu.Unlock()

			// Trigger cleanup
			monitor.cleanupExpiredEntries()

			// Check if maps were cleaned
			monitor.mu.Lock()
			finalCount := len(monitor.patternLastEvent)
			monitor.mu.Unlock()

			if tt.expectCleanup {
				if finalCount >= initialCount {
					t.Errorf("expected cleanup to reduce map size: initial=%d, final=%d",
						initialCount, finalCount)
				}
			} else {
				if finalCount != initialCount {
					t.Errorf("expected no cleanup: initial=%d, final=%d",
						initialCount, finalCount)
				}
			}
		})
	}
}

// TestParseLogPatternConfig_ErrorCases tests error handling in config parsing
func TestParseLogPatternConfig_ErrorCases(t *testing.T) {
	tests := []struct {
		name        string
		config      map[string]interface{}
		expectError bool
		errorMsg    string
	}{
		{
			name: "invalid patterns type - not a slice",
			config: map[string]interface{}{
				"patterns": "not-a-slice",
			},
			expectError: true,
			errorMsg:    "patterns must be an array",
		},
		{
			name: "invalid pattern item type",
			config: map[string]interface{}{
				"patterns": []interface{}{
					"not-a-map",
				},
			},
			expectError: true,
			errorMsg:    "must be an object",
		},
		{
			name: "invalid maxEventsPerPattern type",
			config: map[string]interface{}{
				"maxEventsPerPattern": "not-a-number",
			},
			expectError: false, // Silently ignored with lenient parsing
		},
		{
			name: "invalid dedupWindow type",
			config: map[string]interface{}{
				"dedupWindow": []int{1, 2, 3}, // Not a valid duration type
			},
			expectError: false, // Silently ignored with lenient parsing
		},
		{
			name: "invalid checkKmsg type",
			config: map[string]interface{}{
				"checkKmsg": 123, // Not a boolean
			},
			expectError: false, // Silently ignored with lenient parsing
		},
		{
			name: "invalid journalUnits type - not a slice",
			config: map[string]interface{}{
				"journalUnits": "not-a-slice",
			},
			expectError: true,
			errorMsg:    "journalUnits must be an array",
		},
		{
			name: "invalid journalUnits item type",
			config: map[string]interface{}{
				"journalUnits": []interface{}{123, 456}, // Not strings
			},
			expectError: true,
			errorMsg:    "must be a string",
		},
		{
			name:        "nil config",
			config:      nil,
			expectError: false, // Should return default config
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := parseLogPatternConfig(tt.config)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errorMsg)
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if cfg == nil {
					t.Error("expected non-nil config")
				}
			}
		})
	}
}
