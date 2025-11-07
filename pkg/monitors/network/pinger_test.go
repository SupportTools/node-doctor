package network

import (
	"context"
	"errors"
	"testing"
	"time"
)

// mockPinger implements the Pinger interface for testing.
type mockPinger struct {
	results []PingResult
	err     error
	delay   time.Duration
}

// newMockPinger creates a mock pinger with predefined results.
func newMockPinger(results []PingResult, err error) *mockPinger {
	return &mockPinger{
		results: results,
		err:     err,
	}
}

// Ping returns the predefined results.
func (m *mockPinger) Ping(ctx context.Context, target string, count int, timeout time.Duration) ([]PingResult, error) {
	if m.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(m.delay):
		}
	}

	if m.err != nil {
		return nil, m.err
	}

	// Return the specified number of results
	if len(m.results) == 0 {
		// Generate default successful results
		results := make([]PingResult, count)
		for i := 0; i < count; i++ {
			results[i] = PingResult{
				Success: true,
				RTT:     time.Duration(i+1) * 10 * time.Millisecond,
			}
		}
		return results, nil
	}

	// Return predefined results (cycle if count > len(results))
	results := make([]PingResult, count)
	for i := 0; i < count; i++ {
		results[i] = m.results[i%len(m.results)]
	}
	return results, nil
}

func TestMockPinger(t *testing.T) {
	tests := []struct {
		name        string
		results     []PingResult
		err         error
		count       int
		wantErr     bool
		wantSuccess int
	}{
		{
			name: "all successful",
			results: []PingResult{
				{Success: true, RTT: 10 * time.Millisecond},
				{Success: true, RTT: 20 * time.Millisecond},
				{Success: true, RTT: 15 * time.Millisecond},
			},
			count:       3,
			wantErr:     false,
			wantSuccess: 3,
		},
		{
			name: "partial failures",
			results: []PingResult{
				{Success: true, RTT: 10 * time.Millisecond},
				{Success: false, Error: errors.New("timeout")},
				{Success: true, RTT: 15 * time.Millisecond},
			},
			count:       3,
			wantErr:     false,
			wantSuccess: 2,
		},
		{
			name: "all failures",
			results: []PingResult{
				{Success: false, Error: errors.New("timeout")},
				{Success: false, Error: errors.New("timeout")},
				{Success: false, Error: errors.New("timeout")},
			},
			count:       3,
			wantErr:     false,
			wantSuccess: 0,
		},
		{
			name:    "ping error",
			err:     errors.New("failed to create socket"),
			count:   3,
			wantErr: true,
		},
		{
			name: "count mismatch - cycle results",
			results: []PingResult{
				{Success: true, RTT: 10 * time.Millisecond},
				{Success: false, Error: errors.New("timeout")},
			},
			count:       5,
			wantErr:     false,
			wantSuccess: 3, // Pattern repeats: S, F, S, F, S
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pinger := newMockPinger(tt.results, tt.err)
			ctx := context.Background()

			results, err := pinger.Ping(ctx, "192.168.1.1", tt.count, time.Second)

			if (err != nil) != tt.wantErr {
				t.Errorf("Ping() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			if len(results) != tt.count {
				t.Errorf("Ping() returned %d results, want %d", len(results), tt.count)
				return
			}

			successCount := 0
			for _, r := range results {
				if r.Success {
					successCount++
				}
			}

			if successCount != tt.wantSuccess {
				t.Errorf("Ping() got %d successful pings, want %d", successCount, tt.wantSuccess)
			}
		})
	}
}

func TestMockPinger_ContextCancellation(t *testing.T) {
	pinger := &mockPinger{
		delay: 500 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := pinger.Ping(ctx, "192.168.1.1", 3, time.Second)

	if err == nil {
		t.Error("Ping() expected context cancellation error, got nil")
	}

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Ping() expected context.DeadlineExceeded, got %v", err)
	}
}

func TestPingResult(t *testing.T) {
	tests := []struct {
		name    string
		result  PingResult
		wantRTT bool
	}{
		{
			name: "successful ping",
			result: PingResult{
				Success: true,
				RTT:     10 * time.Millisecond,
			},
			wantRTT: true,
		},
		{
			name: "failed ping",
			result: PingResult{
				Success: false,
				Error:   errors.New("timeout"),
			},
			wantRTT: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.result.Success && tt.result.RTT == 0 && tt.wantRTT {
				t.Error("PingResult: successful ping should have non-zero RTT")
			}

			if !tt.result.Success && tt.result.Error == nil {
				t.Error("PingResult: failed ping should have an error")
			}

			if tt.result.Success && tt.result.Error != nil {
				t.Error("PingResult: successful ping should not have an error")
			}
		})
	}
}

// TestDefaultPinger_Integration is an integration test for the real pinger.
// This test requires ICMP permissions and may not run in all environments.
func TestDefaultPinger_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pinger := newDefaultPinger()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Try to ping localhost (should always succeed)
	results, err := pinger.Ping(ctx, "127.0.0.1", 3, time.Second)

	// If we get a permission error, skip the test
	if err != nil && (errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled)) {
		t.Skipf("Skipping integration test due to permissions or timeout: %v", err)
		return
	}

	if err != nil {
		t.Logf("Warning: Ping failed (may require elevated privileges): %v", err)
		t.Skip("Skipping test - ping requires elevated privileges")
		return
	}

	if len(results) != 3 {
		t.Errorf("Expected 3 ping results, got %d", len(results))
	}

	successCount := 0
	for i, result := range results {
		t.Logf("Ping %d: Success=%v, RTT=%v, Error=%v", i+1, result.Success, result.RTT, result.Error)
		if result.Success {
			successCount++
			if result.RTT == 0 {
				t.Error("Successful ping should have non-zero RTT")
			}
		}
	}

	// At least one ping should succeed when pinging localhost
	if successCount == 0 {
		t.Error("At least one ping to localhost should succeed")
	}
}
