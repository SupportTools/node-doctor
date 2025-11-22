package http

import (
	"errors"
	"testing"
	"time"
)

// TestStats_RecordDroppedRequest tests the RecordDroppedRequest method.
func TestStats_RecordDroppedRequest(t *testing.T) {
	stats := NewStats()

	// Initially, dropped count should be 0
	snapshot := stats.GetSnapshot()
	if snapshot.RequestsDropped != 0 {
		t.Errorf("Initial RequestsDropped = %d, want 0", snapshot.RequestsDropped)
	}

	// Record dropped requests
	stats.RecordDroppedRequest()
	stats.RecordDroppedRequest()
	stats.RecordDroppedRequest()

	snapshot = stats.GetSnapshot()
	if snapshot.RequestsDropped != 3 {
		t.Errorf("After 3 drops, RequestsDropped = %d, want 3", snapshot.RequestsDropped)
	}
}

// TestStats_RecordRetryAttempt tests the RecordRetryAttempt method.
func TestStats_RecordRetryAttempt(t *testing.T) {
	stats := NewStats()

	// Initialize webhook stats
	stats.GetWebhookStats("test-webhook")
	stats.GetWebhookStats("other-webhook")

	// Record retry attempts
	stats.RecordRetryAttempt("test-webhook")
	stats.RecordRetryAttempt("test-webhook")
	stats.RecordRetryAttempt("other-webhook")

	// Verify retry counts
	snapshot := stats.GetSnapshot()

	testWebhook, ok := snapshot.WebhookStats["test-webhook"]
	if !ok {
		t.Fatal("Expected test-webhook stats to exist")
	}
	if testWebhook.RetryAttempts != 2 {
		t.Errorf("test-webhook RetryAttempts = %d, want 2", testWebhook.RetryAttempts)
	}

	otherWebhook, ok := snapshot.WebhookStats["other-webhook"]
	if !ok {
		t.Fatal("Expected other-webhook stats to exist")
	}
	if otherWebhook.RetryAttempts != 1 {
		t.Errorf("other-webhook RetryAttempts = %d, want 1", otherWebhook.RetryAttempts)
	}

	// Record retry for non-existent webhook (should not panic)
	stats.RecordRetryAttempt("non-existent")
}

// TestStatsSnapshot_GetTotalFailures tests the GetTotalFailures method.
func TestStatsSnapshot_GetTotalFailures(t *testing.T) {
	tests := []struct {
		name          string
		statusFailed  int64
		problemFailed int64
		expected      int64
	}{
		{
			name:          "no failures",
			statusFailed:  0,
			problemFailed: 0,
			expected:      0,
		},
		{
			name:          "only status failures",
			statusFailed:  5,
			problemFailed: 0,
			expected:      5,
		},
		{
			name:          "only problem failures",
			statusFailed:  0,
			problemFailed: 3,
			expected:      3,
		},
		{
			name:          "both failures",
			statusFailed:  7,
			problemFailed: 4,
			expected:      11,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := StatsSnapshot{
				StatusExportsFailed:  tt.statusFailed,
				ProblemExportsFailed: tt.problemFailed,
			}

			got := snapshot.GetTotalFailures()
			if got != tt.expected {
				t.Errorf("GetTotalFailures() = %d, want %d", got, tt.expected)
			}
		})
	}
}

// TestStatsSnapshot_GetStatusSuccessRate tests the GetStatusSuccessRate method.
func TestStatsSnapshot_GetStatusSuccessRate(t *testing.T) {
	tests := []struct {
		name     string
		total    int64
		success  int64
		expected float64
	}{
		{
			name:     "no exports",
			total:    0,
			success:  0,
			expected: 0.0,
		},
		{
			name:     "100% success",
			total:    10,
			success:  10,
			expected: 100.0,
		},
		{
			name:     "50% success",
			total:    10,
			success:  5,
			expected: 50.0,
		},
		{
			name:     "0% success",
			total:    10,
			success:  0,
			expected: 0.0,
		},
		{
			name:     "75% success",
			total:    100,
			success:  75,
			expected: 75.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := StatsSnapshot{
				StatusExportsTotal:   tt.total,
				StatusExportsSuccess: tt.success,
			}

			got := snapshot.GetStatusSuccessRate()
			if got != tt.expected {
				t.Errorf("GetStatusSuccessRate() = %f, want %f", got, tt.expected)
			}
		})
	}
}

// TestStatsSnapshot_GetProblemSuccessRate tests the GetProblemSuccessRate method.
func TestStatsSnapshot_GetProblemSuccessRate(t *testing.T) {
	tests := []struct {
		name     string
		total    int64
		success  int64
		expected float64
	}{
		{
			name:     "no exports",
			total:    0,
			success:  0,
			expected: 0.0,
		},
		{
			name:     "100% success",
			total:    20,
			success:  20,
			expected: 100.0,
		},
		{
			name:     "60% success",
			total:    10,
			success:  6,
			expected: 60.0,
		},
		{
			name:     "0% success",
			total:    5,
			success:  0,
			expected: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := StatsSnapshot{
				ProblemExportsTotal:   tt.total,
				ProblemExportsSuccess: tt.success,
			}

			got := snapshot.GetProblemSuccessRate()
			if got != tt.expected {
				t.Errorf("GetProblemSuccessRate() = %f, want %f", got, tt.expected)
			}
		})
	}
}

// TestWebhookStatsSnapshot_GetSuccessRate tests the WebhookStatsSnapshot GetSuccessRate method.
func TestWebhookStatsSnapshot_GetSuccessRate(t *testing.T) {
	tests := []struct {
		name     string
		total    int64
		success  int64
		expected float64
	}{
		{
			name:     "no requests",
			total:    0,
			success:  0,
			expected: 0.0,
		},
		{
			name:     "100% success",
			total:    10,
			success:  10,
			expected: 100.0,
		},
		{
			name:     "80% success",
			total:    100,
			success:  80,
			expected: 80.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := WebhookStatsSnapshot{
				RequestsTotal:   tt.total,
				RequestsSuccess: tt.success,
			}

			got := snapshot.GetSuccessRate()
			if got != tt.expected {
				t.Errorf("GetSuccessRate() = %f, want %f", got, tt.expected)
			}
		})
	}
}

// TestWebhookStatsSnapshot_IsHealthy tests the WebhookStatsSnapshot IsHealthy method.
func TestWebhookStatsSnapshot_IsHealthy(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		snapshot WebhookStatsSnapshot
		expected bool
	}{
		{
			name: "no requests - healthy by default",
			snapshot: WebhookStatsSnapshot{
				RequestsTotal:   0,
				RequestsSuccess: 0,
			},
			expected: true,
		},
		{
			name: "high success rate with recent success",
			snapshot: WebhookStatsSnapshot{
				RequestsTotal:   100,
				RequestsSuccess: 95,
				LastSuccessTime: now,
			},
			expected: true,
		},
		{
			name: "exactly 90% success rate with recent success",
			snapshot: WebhookStatsSnapshot{
				RequestsTotal:   100,
				RequestsSuccess: 90,
				LastSuccessTime: now,
			},
			expected: true,
		},
		{
			name: "success rate below 90%",
			snapshot: WebhookStatsSnapshot{
				RequestsTotal:   100,
				RequestsSuccess: 89,
				LastSuccessTime: now,
			},
			expected: false,
		},
		{
			name: "high success rate but no recent success",
			snapshot: WebhookStatsSnapshot{
				RequestsTotal:   100,
				RequestsSuccess: 95,
				LastSuccessTime: now.Add(-10 * time.Minute), // More than 5 minutes ago
			},
			expected: false,
		},
		{
			name: "high success rate with success just under 5 minutes ago",
			snapshot: WebhookStatsSnapshot{
				RequestsTotal:   100,
				RequestsSuccess: 95,
				LastSuccessTime: now.Add(-4*time.Minute - 59*time.Second),
			},
			expected: true,
		},
		{
			name: "low success rate with no recent success",
			snapshot: WebhookStatsSnapshot{
				RequestsTotal:   100,
				RequestsSuccess: 50,
				LastSuccessTime: now.Add(-10 * time.Minute),
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.snapshot.IsHealthy()
			if got != tt.expected {
				t.Errorf("IsHealthy() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestWebhookStats_recordRequest tests the recordRequest method.
func TestWebhookStats_recordRequest(t *testing.T) {
	tests := []struct {
		name         string
		success      bool
		responseTime time.Duration
		err          error
	}{
		{
			name:         "successful request with response time",
			success:      true,
			responseTime: 100 * time.Millisecond,
			err:          nil,
		},
		{
			name:         "failed request with error",
			success:      false,
			responseTime: 50 * time.Millisecond,
			err:          errors.New("connection refused"),
		},
		{
			name:         "successful request with zero response time",
			success:      true,
			responseTime: 0,
			err:          nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ws := NewWebhookStats("test-webhook")

			// Record the request
			ws.recordRequest(tt.success, tt.responseTime, tt.err)

			snapshot := ws.getSnapshot()

			// Verify request was counted
			if snapshot.RequestsTotal != 1 {
				t.Errorf("RequestsTotal = %d, want 1", snapshot.RequestsTotal)
			}

			if tt.success {
				if snapshot.RequestsSuccess != 1 {
					t.Errorf("RequestsSuccess = %d, want 1", snapshot.RequestsSuccess)
				}
				if snapshot.RequestsFailed != 0 {
					t.Errorf("RequestsFailed = %d, want 0", snapshot.RequestsFailed)
				}
			} else {
				if snapshot.RequestsFailed != 1 {
					t.Errorf("RequestsFailed = %d, want 1", snapshot.RequestsFailed)
				}
				if snapshot.RequestsSuccess != 0 {
					t.Errorf("RequestsSuccess = %d, want 0", snapshot.RequestsSuccess)
				}
				if snapshot.LastError == nil {
					t.Error("Expected LastError to be set")
				}
			}
		})
	}
}

// TestWebhookStats_recordRequest_ResponseTimeAverage tests response time averaging.
func TestWebhookStats_recordRequest_ResponseTimeAverage(t *testing.T) {
	ws := NewWebhookStats("test-webhook")

	// Record multiple requests with different response times
	ws.recordRequest(true, 100*time.Millisecond, nil)
	ws.recordRequest(true, 200*time.Millisecond, nil)
	ws.recordRequest(true, 300*time.Millisecond, nil)

	snapshot := ws.getSnapshot()

	// Average should be (100+200+300)/3 = 200ms
	expectedAvg := 200 * time.Millisecond
	if snapshot.AvgResponseTime != expectedAvg {
		t.Errorf("AvgResponseTime = %v, want %v", snapshot.AvgResponseTime, expectedAvg)
	}
}

// TestStats_RecordStatusExport tests the RecordStatusExport method with failures.
func TestStats_RecordStatusExport_Failures(t *testing.T) {
	stats := NewStats()
	stats.GetWebhookStats("test-webhook")

	// Record a failed status export
	stats.RecordStatusExport(false, "test-webhook", 50*time.Millisecond, errors.New("timeout"))

	snapshot := stats.GetSnapshot()

	if snapshot.StatusExportsTotal != 1 {
		t.Errorf("StatusExportsTotal = %d, want 1", snapshot.StatusExportsTotal)
	}
	if snapshot.StatusExportsFailed != 1 {
		t.Errorf("StatusExportsFailed = %d, want 1", snapshot.StatusExportsFailed)
	}
	if snapshot.StatusExportsSuccess != 0 {
		t.Errorf("StatusExportsSuccess = %d, want 0", snapshot.StatusExportsSuccess)
	}
	if snapshot.LastError == nil {
		t.Error("Expected LastError to be set")
	}
}

// TestStats_RecordProblemExport tests the RecordProblemExport method with failures.
func TestStats_RecordProblemExport_Failures(t *testing.T) {
	stats := NewStats()
	stats.GetWebhookStats("test-webhook")

	// Record a failed problem export
	stats.RecordProblemExport(false, "test-webhook", 50*time.Millisecond, errors.New("connection refused"))

	snapshot := stats.GetSnapshot()

	if snapshot.ProblemExportsTotal != 1 {
		t.Errorf("ProblemExportsTotal = %d, want 1", snapshot.ProblemExportsTotal)
	}
	if snapshot.ProblemExportsFailed != 1 {
		t.Errorf("ProblemExportsFailed = %d, want 1", snapshot.ProblemExportsFailed)
	}
	if snapshot.ProblemExportsSuccess != 0 {
		t.Errorf("ProblemExportsSuccess = %d, want 0", snapshot.ProblemExportsSuccess)
	}
}

// TestStats_ConcurrentAccess tests thread-safety of Stats methods.
func TestStats_ConcurrentAccess(t *testing.T) {
	stats := NewStats()
	stats.GetWebhookStats("webhook-1")
	stats.GetWebhookStats("webhook-2")

	done := make(chan bool)
	const iterations = 100

	// Writer goroutine for queued requests
	go func() {
		for i := 0; i < iterations; i++ {
			stats.RecordQueuedRequest()
		}
		done <- true
	}()

	// Writer goroutine for dropped requests
	go func() {
		for i := 0; i < iterations; i++ {
			stats.RecordDroppedRequest()
		}
		done <- true
	}()

	// Writer goroutine for retries
	go func() {
		for i := 0; i < iterations; i++ {
			stats.RecordRetryAttempt("webhook-1")
		}
		done <- true
	}()

	// Reader goroutine
	go func() {
		for i := 0; i < iterations; i++ {
			_ = stats.GetSnapshot()
		}
		done <- true
	}()

	// Wait for all goroutines
	for i := 0; i < 4; i++ {
		<-done
	}

	// Verify final state
	snapshot := stats.GetSnapshot()
	if snapshot.RequestsQueued != iterations {
		t.Errorf("RequestsQueued = %d, want %d", snapshot.RequestsQueued, iterations)
	}
	if snapshot.RequestsDropped != iterations {
		t.Errorf("RequestsDropped = %d, want %d", snapshot.RequestsDropped, iterations)
	}
}
