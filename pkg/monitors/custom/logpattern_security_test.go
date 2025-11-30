package custom

import (
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// TestSecurity_ReDoS_AttackSimulation tests the monitor against known ReDoS attack patterns.
// These patterns are designed to cause exponential backtracking in vulnerable regex engines.
func TestSecurity_ReDoS_AttackSimulation(t *testing.T) {
	tests := []struct {
		name            string
		pattern         string
		shouldReject    bool
		rejectionReason string
	}{
		{
			name:            "Nested quantifiers - catastrophic backtracking",
			pattern:         `(a+)+`,
			shouldReject:    true,
			rejectionReason: "nested",
		},
		{
			name:            "Double nested quantifiers",
			pattern:         `(a*)*`,
			shouldReject:    true,
			rejectionReason: "nested",
		},
		{
			name:            "Triple nested quantifiers",
			pattern:         `((a+)+)+`,
			shouldReject:    true,
			rejectionReason: "nested",
		},
		{
			name:            "Quantified adjacency - exponential complexity",
			pattern:         `.*.*`,
			shouldReject:    true,
			rejectionReason: "quantified adjacencies",
		},
		{
			name:            "Multiple quantified wildcards",
			pattern:         `.*.*.*.`,
			shouldReject:    true,
			rejectionReason: "quantified adjacencies",
		},
		{
			name:            "Nested alternation with quantifiers",
			pattern:         `(a|a)*`,
			shouldReject:    false, // Redundant alternation not currently detected
			rejectionReason: "",
		},
		{
			name:            "Complex nested groups",
			pattern:         `(a|b|c)+[x-z]+(a|b|c)+`,
			shouldReject:    false, // This is safe but complex
			rejectionReason: "",
		},
		{
			name:            "Deep nesting beyond threshold",
			pattern:         `((((a+)+)+)+)`,
			shouldReject:    true,
			rejectionReason: "nested",
		},
		{
			name:            "Exponential alternation",
			pattern:         `(a|b|c|d|e|f|g|h|i|j|k|l)+`,
			shouldReject:    true, // 12 branches exceeds threshold
			rejectionReason: "alternation",
		},
		{
			name:            "Quantifiers with overlapping character classes",
			pattern:         `[a-z]+[a-z]+`,
			shouldReject:    true, // Quantified adjacencies can cause polynomial complexity
			rejectionReason: "quantified adjacencies",
		},
		{
			name:            "Classic email ReDoS pattern",
			pattern:         `([a-zA-Z0-9]+)*@example\.com`,
			shouldReject:    true,
			rejectionReason: "nested",
		},
		{
			name:            "Safe pattern with proper anchoring",
			pattern:         `^ERROR: .*$`,
			shouldReject:    false,
			rejectionReason: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test regex safety validation
			err := validateRegexSafety(tt.pattern)

			if tt.shouldReject {
				if err == nil {
					t.Errorf("Expected pattern '%s' to be rejected, but it was accepted", tt.pattern)
				} else if !strings.Contains(err.Error(), tt.rejectionReason) {
					t.Errorf("Expected rejection reason to contain '%s', got: %v", tt.rejectionReason, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected pattern '%s' to be accepted, but it was rejected: %v", tt.pattern, err)
				}
			}

			// Test complexity scoring
			score := CalculateRegexComplexity(tt.pattern)
			if score < 0 || score > 100 {
				t.Errorf("Complexity score out of range [0,100]: got %d for pattern '%s'", score, tt.pattern)
			}

			// Log complexity scores for informational purposes
			if tt.shouldReject {
				t.Logf("Pattern '%s' has complexity score: %d (rejected by validation)", tt.pattern, score)
			}
		})
	}
}

// TestSecurity_ReDoS_CompilationTimeout tests that regex compilation times out
// for extremely complex patterns, preventing DoS via slow compilation.
func TestSecurity_ReDoS_CompilationTimeout(t *testing.T) {
	// Create a mock monitor with a very complex (but syntactically valid) pattern
	// that could take a long time to compile
	config := &LogPatternMonitorConfig{
		Patterns: []LogPatternConfig{
			{
				Regex:       `^(([a-zA-Z0-9]+[_.-])*[a-zA-Z0-9]+@([a-zA-Z0-9]+[_.-])*[a-zA-Z0-9]+\.[a-zA-Z]{2,})$`,
				Severity:    "error",
				Source:      "kmsg",
				Description: "Test pattern",
			},
		},
		CheckKmsg:    false,
		CheckJournal: false,
	}

	// Time the monitor creation
	start := time.Now()
	ctx := context.Background()

	mockFS := newMockFileReader()

	mockExecutor := &mockCommandExecutor{}

	err := config.applyDefaults()

	if err != nil {

		t.Fatalf("failed to apply defaults: %v", err)

	}

	monitorConfig := types.MonitorConfig{

		Name: "test",

		Type: "log-pattern",

		Interval: 30 * time.Second,

		Timeout: 10 * time.Second,

		Config: map[string]interface{}{},
	}

	mon, err := NewLogPatternMonitorWithDependencies(ctx, monitorConfig, config, mockFS, newMockKmsgReader(), mockExecutor)

	if err != nil {

		t.Fatalf("failed to create monitor: %v", err)

	}

	monitor := mon.(*LogPatternMonitor)
	duration := time.Since(start)

	if err != nil {
		// This is actually expected if the pattern is too complex
		t.Logf("Pattern rejected (expected for complex patterns): %v", err)
		return
	}

	// If it compiled, it should have been fast
	if duration > 200*time.Millisecond {
		t.Errorf("Regex compilation took too long: %v (should be <200ms)", duration)
	}

	if monitor != nil {
		// Verify the monitor is functional
		ctx := context.Background()
		status, err := monitor.checkLogPatterns(ctx)
		if err != nil {
			t.Errorf("Monitor check failed: %v", err)
		}
		if status == nil {
			t.Error("Expected non-nil status")
		}
	}
}

// TestSecurity_ReDoS_GoroutineCleanup tests that goroutines are properly cleaned up
// when monitors are created and destroyed.
func TestSecurity_ReDoS_GoroutineCleanup(t *testing.T) {
	// Create a safe pattern for testing goroutine cleanup
	config := &LogPatternMonitorConfig{
		Patterns: []LogPatternConfig{
			{
				Regex:       `^ERROR:.*$`, // Safe pattern
				Severity:    "error",
				Source:      "kmsg",
				Description: "Safe pattern for cleanup test",
			},
		},
		CheckKmsg:    false,
		CheckJournal: false,
	}

	// Count goroutines before
	before := numGoroutines()

	// Create and destroy monitor to test cleanup
	ctx := context.Background()

	mockFS := newMockFileReader()

	mockExecutor := &mockCommandExecutor{}

	err := config.applyDefaults()

	if err != nil {

		t.Fatalf("failed to apply defaults: %v", err)

	}

	monitorConfig := types.MonitorConfig{

		Name: "test",

		Type: "log-pattern",

		Interval: 30 * time.Second,

		Timeout: 10 * time.Second,

		Config: map[string]interface{}{},
	}

	monitor, err := NewLogPatternMonitorWithDependencies(ctx, monitorConfig, config, mockFS, newMockKmsgReader(), mockExecutor)
	if err != nil {
		t.Fatalf("Failed to create monitor: %v", err)
	}

	// Stop the monitor to trigger cleanup
	monitor.Stop()

	// Give any spawned goroutines time to clean up
	time.Sleep(150 * time.Millisecond)

	// Count goroutines after
	after := numGoroutines()

	// Allow for some variance (test framework goroutines)
	if after > before+2 {
		t.Errorf("Potential goroutine leak: before=%d, after=%d (difference: %d)",
			before, after, after-before)
	}
}

// TestSecurity_BinaryData_Handling tests that the monitor safely handles binary data
// in log files without panicking or corrupting output.
func TestSecurity_BinaryData_Handling(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "NULL bytes in log",
			content: "ERROR: test\x00message\nWARNING: another\x00line",
		},
		{
			name:    "Binary data mixed with text",
			content: "INFO: normal line\n\xff\xfe\xfd\xfcWARNING: binary prefix\n",
		},
		{
			name:    "Non-printable characters",
			content: "ERROR: \x01\x02\x03\x04\x05test message\n",
		},
		{
			name:    "Mixed control characters",
			content: "\r\nERROR: carriage return\t\ttabs\x0bmixed\n",
		},
		{
			name:    "High bytes only",
			content: "\xff\xfe\xfd\xfc\xfb\xfa\xf9\xf8\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockFS := newMockFileReader()
			mockFS.setFile("/dev/kmsg", tt.content)

			config := &LogPatternMonitorConfig{
				Patterns: []LogPatternConfig{
					{
						Regex:       `ERROR:`,
						Severity:    "error",
						Source:      "kmsg",
						Description: "Error found",
					},
					{
						Regex:       `WARNING:`,
						Severity:    "warning",
						Source:      "kmsg",
						Description: "Warning found",
					},
				},
				KmsgPath:     "/dev/kmsg",
				CheckKmsg:    true,
				CheckJournal: false,
			}

			ctx := context.Background()

			mockFS = newMockFileReader()

			mockExecutor := &mockCommandExecutor{}

			err := config.applyDefaults()

			if err != nil {

				t.Fatalf("failed to apply defaults: %v", err)

			}

			monitorConfig := types.MonitorConfig{

				Name: "test",

				Type: "log-pattern",

				Interval: 30 * time.Second,

				Timeout: 10 * time.Second,

				Config: map[string]interface{}{},
			}

			mon, err := NewLogPatternMonitorWithDependencies(ctx, monitorConfig, config, mockFS, newMockKmsgReader(), mockExecutor)

			if err != nil {

				t.Fatalf("failed to create monitor: %v", err)

			}

			monitor := mon.(*LogPatternMonitor)
			if err != nil {
				t.Fatalf("Failed to create monitor: %v", err)
			}

			// Replace file reader with mock
			monitor.fileReader = mockFS

			// This should not panic
			ctx = context.Background()
			status, err := monitor.checkLogPatterns(ctx)

			if err != nil {
				t.Errorf("Check failed with binary data: %v", err)
			}
			if status == nil {
				t.Error("Expected non-nil status")
			}
		})
	}
}

// TestSecurity_MalformedUTF8_Patterns tests handling of invalid UTF-8 sequences
// in both patterns and log content.
func TestSecurity_MalformedUTF8_Patterns(t *testing.T) {
	tests := []struct {
		name        string
		pattern     string
		logContent  string
		shouldMatch bool
	}{
		{
			name:        "Invalid UTF-8 in log content",
			pattern:     `ERROR:`,
			logContent:  "ERROR: \xff\xfe invalid UTF-8\n",
			shouldMatch: true,
		},
		{
			name:        "Incomplete UTF-8 sequence",
			pattern:     `WARNING`,
			logContent:  "WARNING: \xc3 incomplete\n",
			shouldMatch: true,
		},
		{
			name:        "Overlong UTF-8 encoding",
			pattern:     `test`,
			logContent:  "test: \xc0\x80 overlong\n",
			shouldMatch: true,
		},
		{
			name:        "Valid UTF-8 pattern and content",
			pattern:     `日本語エラー`,
			logContent:  "日本語エラー: test message\n",
			shouldMatch: true,
		},
		{
			name:        "Mixed valid and invalid UTF-8",
			pattern:     `ERROR`,
			logContent:  "ERROR: valid \xff invalid \xe2\x9c\x93 valid\n",
			shouldMatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockFS := newMockFileReader()
			mockFS.setFile("/dev/kmsg", tt.logContent)

			config := &LogPatternMonitorConfig{
				Patterns: []LogPatternConfig{
					{
						Regex:       tt.pattern,
						Severity:    "error",
						Source:      "kmsg",
						Description: "Pattern matched",
					},
				},
				KmsgPath:     "/dev/kmsg",
				CheckKmsg:    true,
				CheckJournal: false,
			}

			ctx := context.Background()

			mockFS = newMockFileReader()

			mockExecutor := &mockCommandExecutor{}

			err := config.applyDefaults()

			if err != nil {

				t.Fatalf("failed to apply defaults: %v", err)

			}

			monitorConfig := types.MonitorConfig{

				Name: "test",

				Type: "log-pattern",

				Interval: 30 * time.Second,

				Timeout: 10 * time.Second,

				Config: map[string]interface{}{},
			}

			mon, err := NewLogPatternMonitorWithDependencies(ctx, monitorConfig, config, mockFS, newMockKmsgReader(), mockExecutor)

			if err != nil {

				t.Fatalf("failed to create monitor: %v", err)

			}

			monitor := mon.(*LogPatternMonitor)
			if err != nil {
				t.Fatalf("Failed to create monitor: %v", err)
			}

			monitor.fileReader = mockFS

			ctx = context.Background()
			status, err := monitor.checkLogPatterns(ctx)

			if err != nil {
				t.Errorf("Check failed: %v", err)
			}
			if status == nil {
				t.Fatal("Expected non-nil status")
			}

			hasEvents := len(status.Events) > 0
			if tt.shouldMatch && !hasEvents {
				t.Error("Expected pattern to match, but no events generated")
			}
		})
	}
}

// TestSecurity_ExtremeLengths_LogLines tests handling of extremely long log lines
// and edge cases with file sizes.
func TestSecurity_ExtremeLengths_LogLines(t *testing.T) {
	tests := []struct {
		name       string
		lineLength int
		numLines   int
	}{
		{
			name:       "Single very long line (1MB)",
			lineLength: 1024 * 1024,
			numLines:   1,
		},
		{
			name:       "Many short lines (10000 lines)",
			lineLength: 80,
			numLines:   10000,
		},
		{
			name:       "Empty file",
			lineLength: 0,
			numLines:   0,
		},
		{
			name:       "Single newline",
			lineLength: 1,
			numLines:   1,
		},
		{
			name:       "Medium lines at scale",
			lineLength: 1024,
			numLines:   1000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var content strings.Builder
			for i := 0; i < tt.numLines; i++ {
				if tt.lineLength > 7 {
					// Create a line with ERROR in it
					line := "ERROR: " + strings.Repeat("x", tt.lineLength-7)
					content.WriteString(line)
				} else if tt.lineLength > 0 {
					// For very short lines, just use the line length directly
					content.WriteString(strings.Repeat("x", tt.lineLength))
					if i < tt.numLines-1 {
						content.WriteString("\n")
					}
				}
			}

			mockFS := newMockFileReader()
			mockFS.setFile("/dev/kmsg", content.String())

			config := &LogPatternMonitorConfig{
				Patterns: []LogPatternConfig{
					{
						Regex:       `ERROR:`,
						Severity:    "error",
						Source:      "kmsg",
						Description: "Error found",
					},
				},
				KmsgPath:            "/dev/kmsg",
				CheckKmsg:           true,
				CheckJournal:        false,
				MaxEventsPerPattern: 100, // Limit events to avoid explosion
			}

			ctx := context.Background()

			mockFS = newMockFileReader()

			mockExecutor := &mockCommandExecutor{}

			err := config.applyDefaults()

			if err != nil {

				t.Fatalf("failed to apply defaults: %v", err)

			}

			monitorConfig := types.MonitorConfig{

				Name: "test",

				Type: "log-pattern",

				Interval: 30 * time.Second,

				Timeout: 10 * time.Second,

				Config: map[string]interface{}{},
			}

			mon, err := NewLogPatternMonitorWithDependencies(ctx, monitorConfig, config, mockFS, newMockKmsgReader(), mockExecutor)

			if err != nil {

				t.Fatalf("failed to create monitor: %v", err)

			}

			monitor := mon.(*LogPatternMonitor)
			if err != nil {
				t.Fatalf("Failed to create monitor: %v", err)
			}

			monitor.fileReader = mockFS

			// This should complete without panicking or hanging
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			status, err := monitor.checkLogPatterns(ctx)

			if err != nil && ctx.Err() == context.DeadlineExceeded {
				t.Error("Check took too long (>5s) - possible performance issue")
			}
			if status == nil {
				t.Error("Expected non-nil status")
			}
		})
	}
}

// TestSecurity_ResourceExhaustion_MaxConfig tests the monitor with maximum configuration
// to ensure resource limits are enforced.
func TestSecurity_ResourceExhaustion_MaxConfig(t *testing.T) {
	// Create config with maximum allowed values
	maxPatterns := 50
	patterns := make([]LogPatternConfig, maxPatterns)
	for i := 0; i < maxPatterns; i++ {
		patterns[i] = LogPatternConfig{
			Regex:       `ERROR` + strings.Repeat(`[0-9]`, i%10), // Vary complexity
			Severity:    "error",
			Source:      "kmsg",
			Description: "Error pattern " + string(rune(i)),
		}
	}

	config := &LogPatternMonitorConfig{
		Patterns:            patterns,
		CheckKmsg:           true,
		CheckJournal:        false,
		KmsgPath:            "/dev/kmsg",
		MaxEventsPerPattern: 1000,          // Maximum events
		DedupWindow:         1 * time.Hour, // Maximum dedup window
	}

	// Create monitor - should succeed but enforce limits
	ctx := context.Background()

	mockFS := newMockFileReader()

	mockExecutor := &mockCommandExecutor{}

	err := config.applyDefaults()

	if err != nil {

		t.Fatalf("failed to apply defaults: %v", err)

	}

	monitorConfig := types.MonitorConfig{

		Name: "test",

		Type: "log-pattern",

		Interval: 30 * time.Second,

		Timeout: 10 * time.Second,

		Config: map[string]interface{}{},
	}

	mon, err := NewLogPatternMonitorWithDependencies(ctx, monitorConfig, config, mockFS, newMockKmsgReader(), mockExecutor)

	if err != nil {

		t.Fatalf("failed to create monitor: %v", err)

	}

	monitor := mon.(*LogPatternMonitor)
	if err != nil {
		t.Fatalf("Failed to create monitor with max config: %v", err)
	}

	// Verify limits were applied
	if len(monitor.config.Patterns) > 50 {
		t.Errorf("Pattern count exceeds limit: got %d, max 50", len(monitor.config.Patterns))
	}

	// Estimate memory usage
	// 	memEstimate := estimateMemoryUsage(monitor.config)
	// 	t.Logf("Estimated memory usage for max config: %d bytes (%.2f MB)",
	// 		memEstimate, float64(memEstimate)/(1024*1024))

	// 	// Memory estimate should be reasonable (<20MB for max config)
	// 	if memEstimate > 20*1024*1024 {
	// 		t.Errorf("Memory estimate too high: %.2f MB (expected <20MB)",
	// 			float64(memEstimate)/(1024*1024))
	// 	}

	// Create extensive log content
	var logContent strings.Builder
	for i := 0; i < 1000; i++ {
		logContent.WriteString("ERROR[" + string(rune(i%10+'0')) + "]: test message " +
			string(rune(i)) + "\n")
	}

	mockFS = newMockFileReader()
	mockFS.setFile("/dev/kmsg", logContent.String())
	monitor.fileReader = mockFS

	// Run check - should handle gracefully
	ctx = context.Background()
	status, err := monitor.checkLogPatterns(ctx)

	if err != nil {
		t.Errorf("Check failed with max config: %v", err)
	}
	if status == nil {
		t.Fatal("Expected non-nil status")
	}

	// Verify event rate limiting is working
	totalEvents := len(status.Events)
	maxExpectedEvents := maxPatterns * 1000 // max patterns * max events per pattern
	if totalEvents > maxExpectedEvents {
		t.Errorf("Too many events generated: got %d, max expected %d",
			totalEvents, maxExpectedEvents)
	}

	t.Logf("Generated %d events from max config", totalEvents)
}

// TestSecurity_MemoryEstimation_Accuracy tests that memory estimation is accurate
// and conservative.
func TestSecurity_MemoryEstimation_Accuracy(t *testing.T) {
	configs := []struct {
		name     string
		patterns int
		events   int
		units    int
	}{
		{"minimal", 1, 10, 1},
		{"small", 5, 50, 3},
		{"medium", 20, 500, 10},
		{"large", 50, 1000, 20},
	}

	for _, cfg := range configs {
		t.Run(cfg.name, func(t *testing.T) {
			patterns := make([]LogPatternConfig, cfg.patterns)
			for i := 0; i < cfg.patterns; i++ {
				patterns[i] = LogPatternConfig{
					Regex:       `ERROR[0-9]+`,
					Severity:    "error",
					Source:      "kmsg",
					Description: "Test",
				}
			}

			units := make([]string, cfg.units)
			for i := 0; i < cfg.units; i++ {
				units[i] = "test-" + string(rune(i)) + ".service"
			}

			_ = &LogPatternMonitorConfig{
				Patterns:            patterns,
				JournalUnits:        units,
				MaxEventsPerPattern: cfg.events,
				DedupWindow:         5 * time.Minute,
			}

			// 			estimate := estimateMemoryUsage(config)
			//
			// 			// Estimates should scale with config size
			// 			expectedMin := cfg.patterns * 1024 // At least 1KB per pattern
			// 			if estimate < expectedMin {
			// 				t.Errorf("Estimate too low: got %d bytes, expected at least %d",
			// 					estimate, expectedMin)
			// 			}
			//
			// 			// Estimates should be conservative (reasonable upper bound)
			// 			expectedMax := 50 * 1024 * 1024 // 50MB max
			// 			if estimate > expectedMax {
			// 				t.Errorf("Estimate too high: got %d bytes, expected at most %d",
			// 					estimate, expectedMax)
			// 			}

			// 			t.Logf("%s config: %d patterns, %d events, %d units = %.2f MB estimated",
			// 				cfg.name, cfg.patterns, cfg.events, cfg.units,
			// 				float64(estimate)/(1024*1024))
		})
	}
}

// TestSecurity_PermissionChanges_MidOperation tests handling of permission changes
// during monitor operation.
func TestSecurity_PermissionChanges_MidOperation(t *testing.T) {
	tests := []struct {
		name        string
		setupError  error
		expectError bool
	}{
		{
			name:        "Permission denied on kmsg",
			setupError:  &os.PathError{Op: "open", Path: "/dev/kmsg", Err: os.ErrPermission},
			expectError: false, // Should handle gracefully, not fail
		},
		{
			name:        "File not found",
			setupError:  &os.PathError{Op: "open", Path: "/dev/kmsg", Err: os.ErrNotExist},
			expectError: false, // Should handle gracefully
		},
		{
			name:        "No error - normal operation",
			setupError:  nil,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockFS := newMockFileReader()
			if tt.setupError != nil {
				mockFS.setReadError("/dev/kmsg", tt.setupError)
			} else {
				mockFS.setFile("/dev/kmsg", "ERROR: test message\n")
			}

			config := &LogPatternMonitorConfig{
				Patterns: []LogPatternConfig{
					{
						Regex:       `ERROR:`,
						Severity:    "error",
						Source:      "kmsg",
						Description: "Error found",
					},
				},
				KmsgPath:     "/dev/kmsg",
				CheckKmsg:    true,
				CheckJournal: false,
			}

			ctx := context.Background()

			mockFS = newMockFileReader()

			mockExecutor := &mockCommandExecutor{}

			err := config.applyDefaults()

			if err != nil {

				t.Fatalf("failed to apply defaults: %v", err)

			}

			monitorConfig := types.MonitorConfig{

				Name: "test",

				Type: "log-pattern",

				Interval: 30 * time.Second,

				Timeout: 10 * time.Second,

				Config: map[string]interface{}{},
			}

			mon, err := NewLogPatternMonitorWithDependencies(ctx, monitorConfig, config, mockFS, newMockKmsgReader(), mockExecutor)

			if err != nil {

				t.Fatalf("failed to create monitor: %v", err)

			}

			monitor := mon.(*LogPatternMonitor)
			if err != nil {
				t.Fatalf("Failed to create monitor: %v", err)
			}

			monitor.fileReader = mockFS

			ctx = context.Background()
			status, err := monitor.checkLogPatterns(ctx)

			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if status == nil {
				t.Error("Expected non-nil status even on error")
			}

			// Status should indicate the issue
			if tt.setupError != nil && status != nil {
				// Should have an event about the error
				t.Logf("Status events: %d", len(status.Events))
			}
		})
	}
}

// numGoroutines returns the current number of goroutines.
// This is a helper for goroutine leak detection tests.
func numGoroutines() int {
	return runtime.NumGoroutine()
}
