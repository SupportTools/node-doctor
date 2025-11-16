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
				tt.setup(&stats)
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
