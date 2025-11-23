package detector

import (
	"sync"
	"testing"
	"time"
)

// TestStatistics_GetProblemsDeduplicated tests the GetProblemsDeduplicated getter.
func TestStatistics_GetProblemsDeduplicated(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*Statistics)
		expected int64
	}{
		{
			name: "zero dedup problems",
			setup: func(s *Statistics) {
				// No setup needed, default is zero
			},
			expected: 0,
		},
		{
			name: "single dedup problem",
			setup: func(s *Statistics) {
				s.AddProblemsDeduplicated(1)
			},
			expected: 1,
		},
		{
			name: "multiple dedup problems",
			setup: func(s *Statistics) {
				s.AddProblemsDeduplicated(5)
				s.AddProblemsDeduplicated(10)
			},
			expected: 15,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats := NewStatistics()
			if tt.setup != nil {
				tt.setup(stats)
			}

			got := stats.GetProblemsDeduplicated()
			if got != tt.expected {
				t.Errorf("GetProblemsDeduplicated() = %d, want %d", got, tt.expected)
			}
		})
	}
}

// TestStatistics_GetStartTime tests the GetStartTime getter.
func TestStatistics_GetStartTime(t *testing.T) {
	// Create statistics and check start time is recent
	before := time.Now()
	stats := NewStatistics()
	after := time.Now()

	startTime := stats.GetStartTime()

	// Start time should be between before and after timestamps
	if startTime.Before(before) || startTime.After(after) {
		t.Errorf("GetStartTime() = %v, expected between %v and %v", startTime, before, after)
	}
}

// TestStatistics_GetUptime tests the GetUptime getter.
func TestStatistics_GetUptime(t *testing.T) {
	stats := NewStatistics()

	// Wait a small amount of time
	time.Sleep(100 * time.Millisecond)

	uptime := stats.GetUptime()

	// Uptime should be at least 100ms
	if uptime < 100*time.Millisecond {
		t.Errorf("GetUptime() = %v, expected >= 100ms", uptime)
	}

	// Uptime should be reasonable (less than 1 second for this test)
	if uptime > 1*time.Second {
		t.Errorf("GetUptime() = %v, expected < 1s", uptime)
	}
}

// TestStatistics_ConcurrentAccess tests thread-safety of all getter methods.
func TestStatistics_ConcurrentAccess(t *testing.T) {
	stats := NewStatistics()
	const goroutines = 10
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 2) // readers and writers

	// Start writer goroutines
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				stats.AddProblemsDeduplicated(1)
				stats.IncrementMonitorsStarted()
				stats.IncrementStatusesReceived()
			}
		}()
	}

	// Start reader goroutines (testing all getters including uncovered ones)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = stats.GetProblemsDeduplicated()
				_ = stats.GetStartTime()
				_ = stats.GetUptime()
				_ = stats.GetMonitorsStarted()
				_ = stats.GetStatusesReceived()
			}
		}()
	}

	wg.Wait()

	// Verify final counts are correct
	expectedDedup := int64(goroutines * iterations)
	if got := stats.GetProblemsDeduplicated(); got != expectedDedup {
		t.Errorf("After concurrent access, GetProblemsDeduplicated() = %d, want %d", got, expectedDedup)
	}
}

// TestStatistics_GettersAfterReset tests that getters return correct values after reset.
func TestStatistics_GettersAfterReset(t *testing.T) {
	stats := NewStatistics()

	// Add some data
	stats.AddProblemsDeduplicated(10)
	stats.IncrementMonitorsStarted()
	stats.IncrementStatusesReceived()

	// Record start time before reset
	oldStartTime := stats.GetStartTime()

	// Wait briefly to ensure time difference
	time.Sleep(10 * time.Millisecond)

	// Reset statistics
	stats.Reset()

	// Verify all counters are zero
	if got := stats.GetProblemsDeduplicated(); got != 0 {
		t.Errorf("After Reset(), GetProblemsDeduplicated() = %d, want 0", got)
	}

	if got := stats.GetMonitorsStarted(); got != 0 {
		t.Errorf("After Reset(), GetMonitorsStarted() = %d, want 0", got)
	}

	if got := stats.GetStatusesReceived(); got != 0 {
		t.Errorf("After Reset(), GetStatusesReceived() = %d, want 0", got)
	}

	// Verify start time was updated
	newStartTime := stats.GetStartTime()
	if !newStartTime.After(oldStartTime) {
		t.Errorf("After Reset(), GetStartTime() = %v, expected to be after %v", newStartTime, oldStartTime)
	}

	// Verify uptime is very small (close to zero)
	uptime := stats.GetUptime()
	if uptime > 100*time.Millisecond {
		t.Errorf("After Reset(), GetUptime() = %v, expected < 100ms", uptime)
	}
}

// TestStatistics_Copy tests the Copy method returns accurate snapshot.
func TestStatistics_Copy(t *testing.T) {
	stats := NewStatistics()

	// Add some data
	stats.AddProblemsDeduplicated(42)
	stats.IncrementMonitorsStarted()
	stats.AddProblemsDetected(10)

	// Create copy
	snapshot := stats.Copy()

	// Verify snapshot has correct values
	if got := snapshot.GetProblemsDeduplicated(); got != 42 {
		t.Errorf("Copy().GetProblemsDeduplicated() = %d, want 42", got)
	}

	if got := snapshot.GetMonitorsStarted(); got != 1 {
		t.Errorf("Copy().GetMonitorsStarted() = %d, want 1", got)
	}

	if got := snapshot.GetProblemsDetected(); got != 10 {
		t.Errorf("Copy().GetProblemsDetected() = %d, want 10", got)
	}

	// Verify start times match
	if !snapshot.GetStartTime().Equal(stats.GetStartTime()) {
		t.Errorf("Copy().GetStartTime() != original.GetStartTime()")
	}

	// Modify original - snapshot should be unchanged
	stats.AddProblemsDeduplicated(100)

	if got := snapshot.GetProblemsDeduplicated(); got != 42 {
		t.Errorf("After modifying original, Copy().GetProblemsDeduplicated() = %d, want 42 (unchanged)", got)
	}
}

// TestStatistics_GetTotalExports tests the GetTotalExports method.
func TestStatistics_GetTotalExports(t *testing.T) {
	tests := []struct {
		name      string
		succeeded int
		failed    int
		expected  int64
	}{
		{
			name:      "no exports",
			succeeded: 0,
			failed:    0,
			expected:  0,
		},
		{
			name:      "only succeeded",
			succeeded: 10,
			failed:    0,
			expected:  10,
		},
		{
			name:      "only failed",
			succeeded: 0,
			failed:    5,
			expected:  5,
		},
		{
			name:      "both succeeded and failed",
			succeeded: 10,
			failed:    3,
			expected:  13,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats := NewStatistics()
			for i := 0; i < tt.succeeded; i++ {
				stats.IncrementExportsSucceeded()
			}
			for i := 0; i < tt.failed; i++ {
				stats.IncrementExportsFailed()
			}

			got := stats.GetTotalExports()
			if got != tt.expected {
				t.Errorf("GetTotalExports() = %d, want %d", got, tt.expected)
			}
		})
	}
}

// TestStatistics_GetExportSuccessRate tests the GetExportSuccessRate method.
func TestStatistics_GetExportSuccessRate(t *testing.T) {
	tests := []struct {
		name      string
		succeeded int
		failed    int
		expected  float64
	}{
		{
			name:      "no exports returns zero",
			succeeded: 0,
			failed:    0,
			expected:  0.0,
		},
		{
			name:      "100% success rate",
			succeeded: 10,
			failed:    0,
			expected:  100.0,
		},
		{
			name:      "0% success rate",
			succeeded: 0,
			failed:    10,
			expected:  0.0,
		},
		{
			name:      "50% success rate",
			succeeded: 5,
			failed:    5,
			expected:  50.0,
		},
		{
			name:      "75% success rate",
			succeeded: 75,
			failed:    25,
			expected:  75.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats := NewStatistics()
			for i := 0; i < tt.succeeded; i++ {
				stats.IncrementExportsSucceeded()
			}
			for i := 0; i < tt.failed; i++ {
				stats.IncrementExportsFailed()
			}

			got := stats.GetExportSuccessRate()
			if got != tt.expected {
				t.Errorf("GetExportSuccessRate() = %f, want %f", got, tt.expected)
			}
		})
	}
}

// TestStatistics_GetDeduplicationRate tests the GetDeduplicationRate method.
func TestStatistics_GetDeduplicationRate(t *testing.T) {
	tests := []struct {
		name         string
		detected     int
		deduplicated int
		expected     float64
	}{
		{
			name:         "no problems returns zero",
			detected:     0,
			deduplicated: 0,
			expected:     0.0,
		},
		{
			name:         "100% unique (no duplicates)",
			detected:     10,
			deduplicated: 10,
			expected:     100.0,
		},
		{
			name:         "50% unique",
			detected:     10,
			deduplicated: 5,
			expected:     50.0,
		},
		{
			name:         "all duplicates",
			detected:     10,
			deduplicated: 0,
			expected:     0.0,
		},
		{
			name:         "80% unique",
			detected:     100,
			deduplicated: 80,
			expected:     80.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats := NewStatistics()
			stats.AddProblemsDetected(tt.detected)
			stats.AddProblemsDeduplicated(tt.deduplicated)

			got := stats.GetDeduplicationRate()
			if got != tt.expected {
				t.Errorf("GetDeduplicationRate() = %f, want %f", got, tt.expected)
			}
		})
	}
}

// TestStatistics_Summary tests the Summary method.
func TestStatistics_Summary(t *testing.T) {
	stats := NewStatistics()

	// Add various data
	stats.IncrementMonitorsStarted()
	stats.IncrementMonitorsStarted()
	stats.IncrementMonitorsFailed()
	stats.IncrementStatusesReceived()
	stats.AddProblemsDetected(10)
	stats.AddProblemsDeduplicated(8)
	stats.IncrementExportsSucceeded()
	stats.IncrementExportsSucceeded()
	stats.IncrementExportsFailed()

	summary := stats.Summary()

	// Verify summary contains expected keys
	expectedKeys := []string{
		"uptime",
		"monitors_started",
		"monitors_failed",
		"statuses_received",
		"problems_detected",
		"problems_deduplicated",
		"exports_succeeded",
		"exports_failed",
		"total_exports",
		"export_success_rate_pct",
		"deduplication_rate_pct",
	}

	for _, key := range expectedKeys {
		if _, ok := summary[key]; !ok {
			t.Errorf("Summary() missing key %q", key)
		}
	}

	// Verify specific values
	if summary["monitors_started"] != int64(2) {
		t.Errorf("Summary()[monitors_started] = %v, want 2", summary["monitors_started"])
	}
	if summary["monitors_failed"] != int64(1) {
		t.Errorf("Summary()[monitors_failed] = %v, want 1", summary["monitors_failed"])
	}
	if summary["total_exports"] != int64(3) {
		t.Errorf("Summary()[total_exports] = %v, want 3", summary["total_exports"])
	}

	// Verify export success rate (2/3 = 66.67%)
	rate := summary["export_success_rate_pct"].(float64)
	expectedRate := float64(2) / float64(3) * 100.0
	if rate != expectedRate {
		t.Errorf("Summary()[export_success_rate_pct] = %v, want %v", rate, expectedRate)
	}

	// Verify deduplication rate (8/10 = 80%)
	dedupRate := summary["deduplication_rate_pct"].(float64)
	if dedupRate != 80.0 {
		t.Errorf("Summary()[deduplication_rate_pct] = %v, want 80.0", dedupRate)
	}
}

// TestStatistics_ExportMetrics tests export-related metrics together.
func TestStatistics_ExportMetrics(t *testing.T) {
	stats := NewStatistics()

	// Verify initial state
	if got := stats.GetExportsSucceeded(); got != 0 {
		t.Errorf("Initial GetExportsSucceeded() = %d, want 0", got)
	}
	if got := stats.GetExportsFailed(); got != 0 {
		t.Errorf("Initial GetExportsFailed() = %d, want 0", got)
	}

	// Add exports
	for i := 0; i < 7; i++ {
		stats.IncrementExportsSucceeded()
	}
	for i := 0; i < 3; i++ {
		stats.IncrementExportsFailed()
	}

	// Verify all export metrics
	if got := stats.GetExportsSucceeded(); got != 7 {
		t.Errorf("GetExportsSucceeded() = %d, want 7", got)
	}
	if got := stats.GetExportsFailed(); got != 3 {
		t.Errorf("GetExportsFailed() = %d, want 3", got)
	}
	if got := stats.GetTotalExports(); got != 10 {
		t.Errorf("GetTotalExports() = %d, want 10", got)
	}
	if got := stats.GetExportSuccessRate(); got != 70.0 {
		t.Errorf("GetExportSuccessRate() = %f, want 70.0", got)
	}
}
