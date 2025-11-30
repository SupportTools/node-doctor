package custom

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// BenchmarkValidateRegexSafety benchmarks the full validation function
func BenchmarkValidateRegexSafety(b *testing.B) {
	benchmarks := []struct {
		name    string
		pattern string
	}{
		{
			name:    "simple pattern",
			pattern: `error.*occurred`,
		},
		{
			name:    "medium pattern with alternation",
			pattern: `(error|warning|info):.*\d+`,
		},
		{
			name:    "complex safe pattern",
			pattern: `(error|warning|critical|info):\s+.*\[code:\d+\]`,
		},
		{
			name:    "long pattern",
			pattern: strings.Repeat("(error|warning|info)", 10) + `:.*`,
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = validateRegexSafety(bm.pattern)
			}
		})
	}
}

// BenchmarkCheckDangerousPatterns benchmarks nested quantifier detection
func BenchmarkCheckDangerousPatterns(b *testing.B) {
	benchmarks := []struct {
		name    string
		pattern string
	}{
		{
			name:    "safe pattern",
			pattern: `error.*occurred`,
		},
		{
			name:    "pattern with alternation",
			pattern: `(error|warning|info)+`,
		},
		{
			name:    "complex safe pattern",
			pattern: `(error|warning):\s+[a-zA-Z0-9]+`,
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = checkDangerousPatterns(bm.pattern)
			}
		})
	}
}

// BenchmarkHasQuantifiedAdjacency benchmarks adjacency detection with pre-compiled patterns
func BenchmarkHasQuantifiedAdjacency(b *testing.B) {
	benchmarks := []struct {
		name    string
		pattern string
	}{
		{
			name:    "no adjacency",
			pattern: `error.*occurred`,
		},
		{
			name:    "with separator",
			pattern: `\d+:\d+`,
		},
		{
			name:    "complex pattern no adjacency",
			pattern: `[a-z]+:[a-z]+`,
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = hasQuantifiedAdjacency(bm.pattern)
			}
		})
	}
}

// BenchmarkCalculateRepetitionDepth benchmarks depth calculation with escape handling
func BenchmarkCalculateRepetitionDepth(b *testing.B) {
	benchmarks := []struct {
		name    string
		pattern string
	}{
		{
			name:    "no nesting",
			pattern: `error.*occurred`,
		},
		{
			name:    "depth 1",
			pattern: `(error)+`,
		},
		{
			name:    "depth 2",
			pattern: `((error)+)+`,
		},
		{
			name:    "with escapes",
			pattern: `\(error\).*\d+`,
		},
		{
			name:    "with character class",
			pattern: `[()+]*error`,
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = calculateRepetitionDepth(bm.pattern)
			}
		})
	}
}

// BenchmarkCalculateRegexComplexity benchmarks the full complexity scoring function
func BenchmarkCalculateRegexComplexity(b *testing.B) {
	benchmarks := []struct {
		name    string
		pattern string
	}{
		{
			name:    "simple pattern (5 chars)",
			pattern: `error`,
		},
		{
			name:    "medium pattern (50 chars)",
			pattern: `(error|warning|info):.*occurred.*\d{1,5}`,
		},
		{
			name:    "complex pattern (100 chars)",
			pattern: `(error|warning|critical|info|debug):\s+.*\[code:\d+\]\s+(error|warning|info):.*occurred`,
		},
		{
			name:    "large pattern (500 chars)",
			pattern: generatePattern(500),
		},
		{
			name:    "real-world OOM pattern",
			pattern: `(Out of memory|Killed process \d+|oom-killer|OOM killer)`,
		},
		{
			name:    "real-world disk error pattern",
			pattern: `(I/O error|Buffer I/O error|EXT4-fs.*error|XFS.*error|sd[a-z]+.*error|SCSI error|Medium Error|critical medium error)`,
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = CalculateRegexComplexity(bm.pattern)
			}
		})
	}
}

// BenchmarkCalculateLengthScore benchmarks length scoring component
func BenchmarkCalculateLengthScore(b *testing.B) {
	benchmarks := []struct {
		name    string
		pattern string
	}{
		{"short (50 chars)", generatePattern(50)},
		{"medium (300 chars)", generatePattern(300)},
		{"long (1000 chars)", generatePattern(1000)},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = calculateLengthScore(bm.pattern)
			}
		})
	}
}

// BenchmarkCalculateDepthScore benchmarks depth scoring component
func BenchmarkCalculateDepthScore(b *testing.B) {
	benchmarks := []struct {
		name    string
		pattern string
	}{
		{"no nesting", `error`},
		{"depth 1", `(error)+`},
		{"depth 2", `((error)+)+`},
		{"depth 3", `(((error)+)+)+`},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = calculateDepthScore(bm.pattern)
			}
		})
	}
}

// BenchmarkCalculateNestedQuantifierScore benchmarks nested quantifier scoring
func BenchmarkCalculateNestedQuantifierScore(b *testing.B) {
	benchmarks := []struct {
		name    string
		pattern string
	}{
		{"no nested", `error.*occurred`},
		{"safe nested", `(error: \d+)+`},
		{"nested star-plus", `(.*)+`},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = calculateNestedQuantifierScore(bm.pattern)
			}
		})
	}
}

// BenchmarkCalculateAdjacencyScore benchmarks adjacency scoring component
func BenchmarkCalculateAdjacencyScore(b *testing.B) {
	benchmarks := []struct {
		name    string
		pattern string
	}{
		{"no adjacency", `error.*occurred`},
		{"separated", `\d+:\d+`},
		{"adjacent wildcards", `.*.*`},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = calculateAdjacencyScore(bm.pattern)
			}
		})
	}
}

// BenchmarkCalculateAlternationScore benchmarks alternation scoring component
func BenchmarkCalculateAlternationScore(b *testing.B) {
	benchmarks := []struct {
		name    string
		pattern string
	}{
		{"no alternation", `error`},
		{"2 branches", `(error|warning)+`},
		{"7 branches", `(a|b|c|d|e|f|g)+`},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = calculateAlternationScore(bm.pattern)
			}
		})
	}
}

// BenchmarkCompileWithTimeout benchmarks the timeout mechanism
func BenchmarkCompileWithTimeout(b *testing.B) {
	pattern := `(error|warning|info):.*occurred`

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = compileWithTimeout(pattern, 100*time.Millisecond)
	}
}

// BenchmarkConcurrent_100Checks benchmarks concurrent check throughput
func BenchmarkConcurrent_100Checks(b *testing.B) {
	ctx := context.Background()
	mockFS := newMockFileReader()
	mockExecutor := &mockCommandExecutor{}

	// Create varied log content
	var logContent strings.Builder
	for i := 0; i < 100; i++ {
		logContent.WriteString("ERROR: message " + string(rune(i)) + "\n")
		logContent.WriteString("WARNING: warning " + string(rune(i)) + "\n")
	}
	mockFS.setFile("/dev/kmsg", logContent.String())

	logConfig := &LogPatternMonitorConfig{
		Patterns: []LogPatternConfig{
			{
				Regex:       `ERROR:`,
				Severity:    "error",
				Source:      "kmsg",
				Description: "Error",
			},
			{
				Regex:       `WARNING:`,
				Severity:    "warning",
				Source:      "kmsg",
				Description: "Warning",
			},
		},
		KmsgPath:            "/dev/kmsg",
		CheckKmsg:           true,
		CheckJournal:        false,
		MaxEventsPerPattern: 100,
		DedupWindow:         1 * time.Second,
	}

	err := logConfig.applyDefaults()
	if err != nil {
		b.Fatalf("Failed to apply defaults: %v", err)
	}

	monitorConfig := types.MonitorConfig{
		Name:     "bench",
		Type:     "log-pattern",
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Config:   map[string]interface{}{},
	}

	mon, err := NewLogPatternMonitorWithDependencies(ctx, monitorConfig, logConfig, mockFS, newMockKmsgReader(), mockExecutor)
	if err != nil {
		b.Fatalf("Failed to create monitor: %v", err)
	}
	monitor := mon.(*LogPatternMonitor)

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = monitor.checkLogPatterns(ctx)
		}
	})
}

// BenchmarkMemory_AllocationRate benchmarks memory allocations over repeated checks
func BenchmarkMemory_AllocationRate(b *testing.B) {
	ctx := context.Background()
	mockFS := newMockFileReader()
	mockExecutor := &mockCommandExecutor{}

	var logContent strings.Builder
	for i := 0; i < 50; i++ {
		logContent.WriteString("ERROR: test message\n")
	}
	mockFS.setFile("/dev/kmsg", logContent.String())

	logConfig := &LogPatternMonitorConfig{
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
		MaxEventsPerPattern: 50,
		DedupWindow:         1 * time.Second,
	}

	err := logConfig.applyDefaults()
	if err != nil {
		b.Fatalf("Failed to apply defaults: %v", err)
	}

	monitorConfig := types.MonitorConfig{
		Name:     "bench-mem",
		Type:     "log-pattern",
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Config:   map[string]interface{}{},
	}

	mon, err := NewLogPatternMonitorWithDependencies(ctx, monitorConfig, logConfig, mockFS, newMockKmsgReader(), mockExecutor)
	if err != nil {
		b.Fatalf("Failed to create monitor: %v", err)
	}
	monitor := mon.(*LogPatternMonitor)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = monitor.checkLogPatterns(ctx)
	}
}

// BenchmarkPatternMatching_Performance benchmarks end-to-end pattern matching performance
// to verify <10ms per check requirement
func BenchmarkPatternMatching_Performance(b *testing.B) {
	benchmarks := []struct {
		name        string
		numPatterns int
		numLogLines int
		target10ms  bool // Whether this should meet <10ms target
	}{
		{
			name:        "10 patterns, 100 lines",
			numPatterns: 10,
			numLogLines: 100,
			target10ms:  true,
		},
		{
			name:        "25 patterns, 500 lines",
			numPatterns: 25,
			numLogLines: 500,
			target10ms:  true,
		},
		{
			name:        "60 patterns, 1000 lines (max config)",
			numPatterns: 60,
			numLogLines: 1000,
			target10ms:  false, // Max config may exceed 10ms
		},
		{
			name:        "5 patterns, 50 lines (minimal)",
			numPatterns: 5,
			numLogLines: 50,
			target10ms:  true,
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			ctx := context.Background()
			mockFS := newMockFileReader()
			mockExecutor := &mockCommandExecutor{}

			// Create patterns
			patterns := make([]LogPatternConfig, bm.numPatterns)
			for i := 0; i < bm.numPatterns; i++ {
				patterns[i] = LogPatternConfig{
					Regex:       `ERROR` + string(rune(i%10)) + `:`,
					Severity:    "error",
					Source:      "kmsg",
					Description: "Error " + string(rune(i)),
				}
			}

			// Create log content
			var logContent strings.Builder
			for i := 0; i < bm.numLogLines; i++ {
				logContent.WriteString("ERROR" + string(rune(i%10)) + ": message " +
					string(rune(i)) + "\n")
			}
			mockFS.setFile("/dev/kmsg", logContent.String())

			logConfig := &LogPatternMonitorConfig{
				Patterns:            patterns,
				KmsgPath:            "/dev/kmsg",
				CheckKmsg:           true,
				CheckJournal:        false,
				MaxEventsPerPattern: 100,
				DedupWindow:         1 * time.Second,
			}

			err := logConfig.applyDefaults()
			if err != nil {
				b.Fatalf("Failed to apply defaults: %v", err)
			}

			monitorConfig := types.MonitorConfig{
				Name:     "bench-perf",
				Type:     "log-pattern",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Config:   map[string]interface{}{},
			}

			mon, err := NewLogPatternMonitorWithDependencies(ctx, monitorConfig, logConfig, mockFS, newMockKmsgReader(), mockExecutor)
			if err != nil {
				b.Fatalf("Failed to create monitor: %v", err)
			}
			monitor := mon.(*LogPatternMonitor)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_, _ = monitor.checkLogPatterns(ctx)
			}

			// Report if target was met
			nsPerOp := b.Elapsed().Nanoseconds() / int64(b.N)
			msPerOp := float64(nsPerOp) / 1000000.0

			if bm.target10ms && msPerOp > 10.0 {
				b.Logf("WARNING: Exceeds 10ms target: %.2fms/op", msPerOp)
			} else {
				b.Logf("Performance: %.2fms/op (target: %v)", msPerOp, bm.target10ms)
			}
		})
	}
}
