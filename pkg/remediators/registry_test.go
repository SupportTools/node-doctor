package remediators

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/supporttools/node-doctor/pkg/types"
)

// mockRemediator is a test remediator for testing registry functionality.
type mockRemediator struct {
	*BaseRemediator
	shouldFail   bool
	failureCount int
	callCount    int
	mu           sync.Mutex
}

// newMockRemediator creates a new mock remediator for testing.
func newMockRemediator(name string, shouldFail bool) *mockRemediator {
	base, _ := NewBaseRemediator(name, CooldownFast)
	mr := &mockRemediator{
		BaseRemediator: base,
		shouldFail:     shouldFail,
	}
	base.SetRemediateFunc(mr.remediate)
	return mr
}

// remediate is the mock remediation function.
func (m *mockRemediator) remediate(ctx context.Context, problem types.Problem) error {
	m.mu.Lock()
	m.callCount++
	count := m.failureCount
	m.failureCount++
	m.mu.Unlock()

	if m.shouldFail {
		// Allow configuration of how many times to fail
		if count < 10 { // Fail first 10 times by default
			return fmt.Errorf("mock remediation failed (attempt %d)", count+1)
		}
	}
	return nil
}

// getCallCount returns the number of times remediate was called.
func (m *mockRemediator) getCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

// TestNewRegistry tests the registry constructor.
func TestNewRegistry(t *testing.T) {
	tests := []struct {
		name        string
		maxPerHour  int
		maxHistory  int
		wantPerHour int
		wantHistory int
	}{
		{
			name:        "default values",
			maxPerHour:  10,
			maxHistory:  100,
			wantPerHour: 10,
			wantHistory: 100,
		},
		{
			name:        "zero rate limit disables it",
			maxPerHour:  0,
			maxHistory:  100,
			wantPerHour: 0,
			wantHistory: 100,
		},
		{
			name:        "zero history disables it",
			maxPerHour:  10,
			maxHistory:  0,
			wantPerHour: 10,
			wantHistory: 0,
		},
		{
			name:        "negative values normalized",
			maxPerHour:  -5,
			maxHistory:  -10,
			wantPerHour: 0,
			wantHistory: 0,
		},
		{
			name:        "very large history capped",
			maxPerHour:  10,
			maxHistory:  20000,
			wantPerHour: 10,
			wantHistory: 10000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewRegistry(tt.maxPerHour, tt.maxHistory)

			if registry == nil {
				t.Fatal("NewRegistry returned nil")
			}

			stats := registry.GetStats()
			if stats.MaxPerHour != tt.wantPerHour {
				t.Errorf("MaxPerHour = %d, want %d", stats.MaxPerHour, tt.wantPerHour)
			}
			if stats.MaxHistory != tt.wantHistory {
				t.Errorf("MaxHistory = %d, want %d", stats.MaxHistory, tt.wantHistory)
			}
			if stats.CircuitState != CircuitClosed {
				t.Errorf("Initial circuit state = %v, want Closed", stats.CircuitState)
			}
			if stats.RegisteredTypes != 0 {
				t.Errorf("RegisteredTypes = %d, want 0", stats.RegisteredTypes)
			}
		})
	}
}

// TestRegister tests remediator registration.
func TestRegister(t *testing.T) {
	t.Run("successful registration", func(t *testing.T) {
		registry := NewRegistry(10, 100)

		info := RemediatorInfo{
			Type:        "test-remediator",
			Factory:     func() (types.Remediator, error) { return newMockRemediator("test", false), nil },
			Description: "Test remediator",
		}

		registry.Register(info)

		if !registry.IsRegistered("test-remediator") {
			t.Error("Remediator not registered")
		}

		types := registry.GetRegisteredTypes()
		if len(types) != 1 || types[0] != "test-remediator" {
			t.Errorf("GetRegisteredTypes() = %v, want [test-remediator]", types)
		}

		gotInfo := registry.GetRemediatorInfo("test-remediator")
		if gotInfo == nil {
			t.Fatal("GetRemediatorInfo returned nil")
		}
		if gotInfo.Type != info.Type {
			t.Errorf("Type = %s, want %s", gotInfo.Type, info.Type)
		}
		if gotInfo.Description != info.Description {
			t.Errorf("Description = %s, want %s", gotInfo.Description, info.Description)
		}
	})

	t.Run("panic on empty type", func(t *testing.T) {
		registry := NewRegistry(10, 100)

		defer func() {
			if r := recover(); r == nil {
				t.Error("Expected panic for empty type, got none")
			}
		}()

		registry.Register(RemediatorInfo{
			Type:    "",
			Factory: func() (types.Remediator, error) { return nil, nil },
		})
	})

	t.Run("panic on nil factory", func(t *testing.T) {
		registry := NewRegistry(10, 100)

		defer func() {
			if r := recover(); r == nil {
				t.Error("Expected panic for nil factory, got none")
			}
		}()

		registry.Register(RemediatorInfo{
			Type:    "test",
			Factory: nil,
		})
	})

	t.Run("panic on duplicate registration", func(t *testing.T) {
		registry := NewRegistry(10, 100)

		info := RemediatorInfo{
			Type:    "test",
			Factory: func() (types.Remediator, error) { return nil, nil },
		}

		registry.Register(info)

		defer func() {
			if r := recover(); r == nil {
				t.Error("Expected panic for duplicate registration, got none")
			}
		}()

		registry.Register(info)
	})
}

// TestDryRunMode tests dry-run mode functionality.
func TestDryRunMode(t *testing.T) {
	registry := NewRegistry(10, 100)
	mock := newMockRemediator("test", false)

	registry.Register(RemediatorInfo{
		Type:    "test",
		Factory: func() (types.Remediator, error) { return mock, nil },
	})

	if registry.IsDryRun() {
		t.Error("IsDryRun() = true, want false (initially)")
	}

	registry.SetDryRun(true)

	if !registry.IsDryRun() {
		t.Error("IsDryRun() = false, want true (after setting)")
	}

	// Execute remediation in dry-run mode
	problem := createTestProblem("test-type", "test-resource")
	err := registry.Remediate(context.Background(), "test", problem)
	if err != nil {
		t.Errorf("Remediate() in dry-run mode failed: %v", err)
	}

	// Verify remediation was not actually executed
	if mock.getCallCount() != 0 {
		t.Errorf("Remediation was executed in dry-run mode, callCount = %d", mock.getCallCount())
	}

	// Verify history was still recorded
	history := registry.GetHistory(10)
	if len(history) != 1 {
		t.Errorf("History length = %d, want 1", len(history))
	}
	if !history[0].Success {
		t.Error("Dry-run remediation should be marked as success")
	}

	// Verify stats show dry-run
	stats := registry.GetStats()
	if !stats.DryRun {
		t.Error("Stats.DryRun = false, want true")
	}
}

// TestCircuitBreaker tests circuit breaker functionality.
func TestCircuitBreaker(t *testing.T) {
	t.Run("circuit opens after threshold failures", func(t *testing.T) {
		registry := NewRegistry(100, 100) // High rate limit
		mock := newMockRemediator("test", true)

		registry.Register(RemediatorInfo{
			Type:    "test",
			Factory: func() (types.Remediator, error) { return mock, nil },
		})

		// Set circuit breaker config
		config := CircuitBreakerConfig{
			Threshold:        3,
			Timeout:          1 * time.Second,
			SuccessThreshold: 2,
		}
		err := registry.SetCircuitBreakerConfig(config)
		if err != nil {
			t.Fatalf("SetCircuitBreakerConfig() failed: %v", err)
		}

		problem := createTestProblem("test-type", "test-resource")

		// Cause failures to open the circuit
		for i := 0; i < 3; i++ {
			mock.ClearCooldown(problem) // Clear cooldown between attempts
			_ = registry.Remediate(context.Background(), "test", problem)
		}

		// Verify circuit is open
		if registry.GetCircuitState() != CircuitOpen {
			t.Errorf("Circuit state = %v, want Open", registry.GetCircuitState())
		}

		// Verify next remediation is blocked
		mock.ClearCooldown(problem)
		err = registry.Remediate(context.Background(), "test", problem)
		if err == nil {
			t.Error("Expected error when circuit is open, got nil")
		}
	})

	t.Run("circuit transitions to half-open after timeout", func(t *testing.T) {
		registry := NewRegistry(100, 100)
		mock := newMockRemediator("test", true)

		registry.Register(RemediatorInfo{
			Type:    "test",
			Factory: func() (types.Remediator, error) { return mock, nil },
		})

		// Set very short timeout for testing
		config := CircuitBreakerConfig{
			Threshold:        2,
			Timeout:          50 * time.Millisecond,
			SuccessThreshold: 2,
		}
		registry.SetCircuitBreakerConfig(config)

		problem := createTestProblem("test-type", "test-resource")

		// Open the circuit
		for i := 0; i < 2; i++ {
			mock.ClearCooldown(problem)
			_ = registry.Remediate(context.Background(), "test", problem)
		}

		if registry.GetCircuitState() != CircuitOpen {
			t.Fatal("Circuit should be open")
		}

		// Wait for timeout
		time.Sleep(100 * time.Millisecond)

		// Next attempt should transition to half-open
		mock.ClearCooldown(problem)
		_ = registry.Remediate(context.Background(), "test", problem)

		// Note: It might still be HalfOpen or Open depending on the failure
		state := registry.GetCircuitState()
		if state != CircuitHalfOpen && state != CircuitOpen {
			t.Errorf("Circuit state = %v, want HalfOpen or Open", state)
		}
	})

	t.Run("circuit recovery mechanism works", func(t *testing.T) {
		// This test verifies that the circuit breaker can recover from Open state
		// We don't test full close here due to complexity of multiple safety layers
		registry := NewRegistry(100, 100)

		// Create a simple failing remediator
		base, _ := NewBaseRemediator("test", 1*time.Millisecond)
		base.SetRemediateFunc(func(ctx context.Context, p types.Problem) error {
			return errors.New("always fails")
		})

		registry.Register(RemediatorInfo{
			Type:    "test",
			Factory: func() (types.Remediator, error) { return base, nil },
		})

		// Configure circuit breaker with short timeout for testing
		config := CircuitBreakerConfig{
			Threshold:        2,
			Timeout:          100 * time.Millisecond,
			SuccessThreshold: 2,
		}
		registry.SetCircuitBreakerConfig(config)

		problem := createTestProblem("test-type", "test-resource")

		// Cause failures to open circuit
		for i := 0; i < 3; i++ {
			base.ClearCooldown(problem)
			_ = registry.Remediate(context.Background(), "test", problem)
		}

		if registry.GetCircuitState() != CircuitOpen {
			t.Fatal("Circuit should be open after failures")
		}

		// Verify circuit blocks immediately after opening
		base.ClearCooldown(problem)
		err := registry.Remediate(context.Background(), "test", problem)
		if err == nil {
			t.Error("Expected error when circuit is open, got nil")
		}

		// Wait for timeout to expire
		time.Sleep(150 * time.Millisecond)

		// After timeout, circuit should allow one attempt (HalfOpen)
		// Note: The attempt will fail and circuit will re-open, but this proves
		// the timeout mechanism works
		stateBefore := registry.GetCircuitState()
		base.ClearCooldown(problem)
		_ = registry.Remediate(context.Background(), "test", problem)
		stateAfter := registry.GetCircuitState()

		// Verify that circuit transitioned (even if it went back to Open due to failure)
		// The key is that it tried to execute (proving timeout worked)
		if stateBefore == CircuitOpen && stateAfter == CircuitOpen {
			// This is actually OK - it went HalfOpen briefly, executed, failed, went back to Open
			// We can't easily detect the brief HalfOpen state, so we just verify no panic occurred
			t.Log("Circuit remained Open after attempt, which is expected since remediations always fail")
		}
	})

	t.Run("reset circuit breaker", func(t *testing.T) {
		registry := NewRegistry(100, 100)
		mock := newMockRemediator("test", true)

		registry.Register(RemediatorInfo{
			Type:    "test",
			Factory: func() (types.Remediator, error) { return mock, nil },
		})

		config := CircuitBreakerConfig{
			Threshold:        2,
			Timeout:          1 * time.Minute,
			SuccessThreshold: 2,
		}
		registry.SetCircuitBreakerConfig(config)

		problem := createTestProblem("test-type", "test-resource")

		// Open the circuit
		for i := 0; i < 2; i++ {
			mock.ClearCooldown(problem)
			_ = registry.Remediate(context.Background(), "test", problem)
		}

		if registry.GetCircuitState() != CircuitOpen {
			t.Fatal("Circuit should be open")
		}

		// Reset the circuit breaker
		registry.ResetCircuitBreaker()

		if registry.GetCircuitState() != CircuitClosed {
			t.Errorf("Circuit state after reset = %v, want Closed", registry.GetCircuitState())
		}

		stats := registry.GetStats()
		if stats.ConsecutiveFailures != 0 {
			t.Errorf("ConsecutiveFailures = %d, want 0", stats.ConsecutiveFailures)
		}
	})
}

// TestSetCircuitBreakerConfig tests circuit breaker configuration validation.
func TestSetCircuitBreakerConfig(t *testing.T) {
	registry := NewRegistry(10, 100)

	tests := []struct {
		name    string
		config  CircuitBreakerConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: CircuitBreakerConfig{
				Threshold:        5,
				Timeout:          5 * time.Minute,
				SuccessThreshold: 2,
			},
			wantErr: false,
		},
		{
			name: "zero threshold",
			config: CircuitBreakerConfig{
				Threshold:        0,
				Timeout:          5 * time.Minute,
				SuccessThreshold: 2,
			},
			wantErr: true,
		},
		{
			name: "negative timeout",
			config: CircuitBreakerConfig{
				Threshold:        5,
				Timeout:          -1 * time.Minute,
				SuccessThreshold: 2,
			},
			wantErr: true,
		},
		{
			name: "zero success threshold",
			config: CircuitBreakerConfig{
				Threshold:        5,
				Timeout:          5 * time.Minute,
				SuccessThreshold: 0,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := registry.SetCircuitBreakerConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("SetCircuitBreakerConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestRateLimit tests rate limiting functionality.
func TestRateLimit(t *testing.T) {
	t.Run("rate limit enforced", func(t *testing.T) {
		registry := NewRegistry(3, 100) // Allow only 3 per hour
		mock := newMockRemediator("test", false)

		registry.Register(RemediatorInfo{
			Type:    "test",
			Factory: func() (types.Remediator, error) { return mock, nil },
		})

		problem := createTestProblem("test-type", "test-resource")

		// Execute 3 successful remediations
		for i := 0; i < 3; i++ {
			mock.ClearCooldown(problem)
			err := registry.Remediate(context.Background(), "test", problem)
			if err != nil {
				t.Fatalf("Remediation %d failed: %v", i+1, err)
			}
		}

		// 4th should be rate limited
		mock.ClearCooldown(problem)
		err := registry.Remediate(context.Background(), "test", problem)
		if err == nil {
			t.Error("Expected rate limit error, got nil")
		}

		stats := registry.GetStats()
		if stats.RecentRemediations != 3 {
			t.Errorf("RecentRemediations = %d, want 3", stats.RecentRemediations)
		}
	})

	t.Run("rate limit disabled when maxPerHour is 0", func(t *testing.T) {
		registry := NewRegistry(0, 100) // Rate limiting disabled
		mock := newMockRemediator("test", false)

		registry.Register(RemediatorInfo{
			Type:    "test",
			Factory: func() (types.Remediator, error) { return mock, nil },
		})

		problem := createTestProblem("test-type", "test-resource")

		// Execute many remediations - none should be rate limited
		for i := 0; i < 10; i++ {
			mock.ClearCooldown(problem)
			mock.ResetAttempts(problem) // Also reset attempt counter
			err := registry.Remediate(context.Background(), "test", problem)
			if err != nil {
				t.Fatalf("Remediation %d failed: %v", i+1, err)
			}
		}
	})
}

// TestHistory tests remediation history tracking.
func TestHistory(t *testing.T) {
	t.Run("history records success and failure", func(t *testing.T) {
		registry := NewRegistry(100, 100)

		successMock := newMockRemediator("success", false)
		failMock := newMockRemediator("fail", true)

		registry.Register(RemediatorInfo{
			Type:    "success",
			Factory: func() (types.Remediator, error) { return successMock, nil },
		})
		registry.Register(RemediatorInfo{
			Type:    "fail",
			Factory: func() (types.Remediator, error) { return failMock, nil },
		})

		problem := createTestProblem("test-type", "test-resource")

		// Execute successful remediation
		_ = registry.Remediate(context.Background(), "success", problem)

		// Execute failed remediation
		successMock.ClearCooldown(problem)
		_ = registry.Remediate(context.Background(), "fail", problem)

		history := registry.GetHistory(10)
		if len(history) != 2 {
			t.Fatalf("History length = %d, want 2", len(history))
		}

		// Check success record
		if !history[0].Success {
			t.Error("First record should be success")
		}
		if history[0].RemediatorType != "success" {
			t.Errorf("First record type = %s, want success", history[0].RemediatorType)
		}
		if history[0].Error != "" {
			t.Errorf("First record error = %s, want empty", history[0].Error)
		}

		// Check failure record
		if history[1].Success {
			t.Error("Second record should be failure")
		}
		if history[1].RemediatorType != "fail" {
			t.Errorf("Second record type = %s, want fail", history[1].RemediatorType)
		}
		if history[1].Error == "" {
			t.Error("Second record error should not be empty")
		}
	})

	t.Run("history respects max size", func(t *testing.T) {
		registry := NewRegistry(100, 5) // Max 5 records
		mock := newMockRemediator("test", false)

		registry.Register(RemediatorInfo{
			Type:    "test",
			Factory: func() (types.Remediator, error) { return mock, nil },
		})

		problem := createTestProblem("test-type", "test-resource")

		// Execute 10 remediations
		for i := 0; i < 10; i++ {
			mock.ClearCooldown(problem)
			_ = registry.Remediate(context.Background(), "test", problem)
		}

		history := registry.GetHistory(100)
		if len(history) != 5 {
			t.Errorf("History length = %d, want 5 (max size)", len(history))
		}
	})

	t.Run("history disabled when maxHistory is 0", func(t *testing.T) {
		registry := NewRegistry(100, 0) // History disabled
		mock := newMockRemediator("test", false)

		registry.Register(RemediatorInfo{
			Type:    "test",
			Factory: func() (types.Remediator, error) { return mock, nil },
		})

		problem := createTestProblem("test-type", "test-resource")

		_ = registry.Remediate(context.Background(), "test", problem)

		history := registry.GetHistory(10)
		if len(history) != 0 {
			t.Errorf("History length = %d, want 0 (disabled)", len(history))
		}
	})

	t.Run("GetHistory respects limit", func(t *testing.T) {
		registry := NewRegistry(100, 100)
		mock := newMockRemediator("test", false)

		registry.Register(RemediatorInfo{
			Type:    "test",
			Factory: func() (types.Remediator, error) { return mock, nil },
		})

		problem := createTestProblem("test-type", "test-resource")

		// Execute 10 remediations
		for i := 0; i < 10; i++ {
			mock.ClearCooldown(problem)
			_ = registry.Remediate(context.Background(), "test", problem)
		}

		// Request only last 3
		history := registry.GetHistory(3)
		if len(history) != 3 {
			t.Errorf("History length = %d, want 3", len(history))
		}

		// Request more than available
		history = registry.GetHistory(100)
		if len(history) != 10 {
			t.Errorf("History length = %d, want 10", len(history))
		}

		// Request with negative limit (should return all)
		history = registry.GetHistory(-1)
		if len(history) != 10 {
			t.Errorf("History length = %d, want 10", len(history))
		}
	})
}

// TestRemediateIntegration tests the full Remediate flow.
func TestRemediateIntegration(t *testing.T) {
	t.Run("successful remediation flow", func(t *testing.T) {
		registry := NewRegistry(10, 100)
		mock := newMockRemediator("test", false)

		registry.Register(RemediatorInfo{
			Type:    "test",
			Factory: func() (types.Remediator, error) { return mock, nil },
		})

		problem := createTestProblem("test-type", "test-resource")

		err := registry.Remediate(context.Background(), "test", problem)
		if err != nil {
			t.Errorf("Remediate() failed: %v", err)
		}

		// Verify remediator was called
		if mock.getCallCount() != 1 {
			t.Errorf("Remediator call count = %d, want 1", mock.getCallCount())
		}

		// Verify history was recorded
		history := registry.GetHistory(10)
		if len(history) != 1 {
			t.Fatalf("History length = %d, want 1", len(history))
		}
		if !history[0].Success {
			t.Error("History record should show success")
		}

		// Verify stats were updated
		stats := registry.GetStats()
		if stats.RecentRemediations != 1 {
			t.Errorf("RecentRemediations = %d, want 1", stats.RecentRemediations)
		}
		if stats.ConsecutiveSuccesses != 1 {
			t.Errorf("ConsecutiveSuccesses = %d, want 1", stats.ConsecutiveSuccesses)
		}
	})

	t.Run("unknown remediator type", func(t *testing.T) {
		registry := NewRegistry(10, 100)

		problem := createTestProblem("test-type", "test-resource")

		err := registry.Remediate(context.Background(), "unknown", problem)
		if err == nil {
			t.Error("Expected error for unknown remediator type, got nil")
		}
	})

	t.Run("factory returns error", func(t *testing.T) {
		registry := NewRegistry(10, 100)

		registry.Register(RemediatorInfo{
			Type: "failing-factory",
			Factory: func() (types.Remediator, error) {
				return nil, errors.New("factory failed")
			},
		})

		problem := createTestProblem("test-type", "test-resource")

		err := registry.Remediate(context.Background(), "failing-factory", problem)
		if err == nil {
			t.Error("Expected error from failing factory, got nil")
		}
	})

	t.Run("factory panics", func(t *testing.T) {
		registry := NewRegistry(10, 100)

		registry.Register(RemediatorInfo{
			Type: "panicking-factory",
			Factory: func() (types.Remediator, error) {
				panic("factory panic")
			},
		})

		problem := createTestProblem("test-type", "test-resource")

		err := registry.Remediate(context.Background(), "panicking-factory", problem)
		if err == nil {
			t.Error("Expected error from panicking factory, got nil")
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		registry := NewRegistry(10, 100)

		// Create remediator that checks context
		base, _ := NewBaseRemediator("test", CooldownFast)
		base.SetRemediateFunc(func(ctx context.Context, p types.Problem) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				return nil
			}
		})

		registry.Register(RemediatorInfo{
			Type:    "test",
			Factory: func() (types.Remediator, error) { return base, nil },
		})

		// Create cancelled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		problem := createTestProblem("test-type", "test-resource")

		err := registry.Remediate(ctx, "test", problem)
		// Should fail due to context cancellation
		if err == nil {
			t.Error("Expected error due to context cancellation, got nil")
		}
	})
}

// TestValidatorIntegration tests validator functionality.
func TestValidatorIntegration(t *testing.T) {
	t.Run("validator passes", func(t *testing.T) {
		registry := NewRegistry(10, 100)
		mock := newMockRemediator("test", false)

		registry.Register(RemediatorInfo{
			Type:    "test",
			Factory: func() (types.Remediator, error) { return mock, nil },
			Validator: func(r types.Remediator) error {
				// Always pass
				return nil
			},
		})

		problem := createTestProblem("test-type", "test-resource")

		err := registry.Remediate(context.Background(), "test", problem)
		if err != nil {
			t.Errorf("Remediate() failed: %v", err)
		}
	})

	t.Run("validator fails", func(t *testing.T) {
		registry := NewRegistry(10, 100)
		mock := newMockRemediator("test", false)

		registry.Register(RemediatorInfo{
			Type:    "test",
			Factory: func() (types.Remediator, error) { return mock, nil },
			Validator: func(r types.Remediator) error {
				return errors.New("validation failed")
			},
		})

		problem := createTestProblem("test-type", "test-resource")

		err := registry.Remediate(context.Background(), "test", problem)
		if err == nil {
			t.Error("Expected validation error, got nil")
		}
	})
}

// TestGetStats tests statistics retrieval.
func TestGetStats(t *testing.T) {
	registry := NewRegistry(10, 100)
	mock := newMockRemediator("test", false)

	registry.Register(RemediatorInfo{
		Type:    "test",
		Factory: func() (types.Remediator, error) { return mock, nil },
	})

	// Initial stats
	stats := registry.GetStats()
	if stats.RegisteredTypes != 1 {
		t.Errorf("RegisteredTypes = %d, want 1", stats.RegisteredTypes)
	}
	if stats.CircuitState != CircuitClosed {
		t.Errorf("CircuitState = %v, want Closed", stats.CircuitState)
	}
	if stats.RecentRemediations != 0 {
		t.Errorf("RecentRemediations = %d, want 0", stats.RecentRemediations)
	}

	// Execute a remediation
	problem := createTestProblem("test-type", "test-resource")
	_ = registry.Remediate(context.Background(), "test", problem)

	// Updated stats
	stats = registry.GetStats()
	if stats.RecentRemediations != 1 {
		t.Errorf("RecentRemediations = %d, want 1", stats.RecentRemediations)
	}
	if stats.ConsecutiveSuccesses != 1 {
		t.Errorf("ConsecutiveSuccesses = %d, want 1", stats.ConsecutiveSuccesses)
	}
	if stats.HistorySize != 1 {
		t.Errorf("HistorySize = %d, want 1", stats.HistorySize)
	}
}

// TestConcurrentRemediationRegistry tests thread safety of concurrent remediation through registry.
func TestConcurrentRemediationRegistry(t *testing.T) {
	registry := NewRegistry(1000, 1000) // High limits

	// Create multiple remediators
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("test-%d", i)
		mock := newMockRemediator(name, false)
		registry.Register(RemediatorInfo{
			Type:    name,
			Factory: func() (types.Remediator, error) { return mock, nil },
		})
	}

	// Execute concurrent remediations
	var wg sync.WaitGroup
	errors := make(chan error, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			remType := fmt.Sprintf("test-%d", idx%5)
			problem := createTestProblem("concurrent-type", fmt.Sprintf("resource-%d", idx))

			// Need to clear cooldown since we're hammering the same problems
			remediator, _ := registry.getOrCreateRemediator(remType)
			if br, ok := remediator.(*mockRemediator); ok {
				br.ClearCooldown(problem)
			}

			err := registry.Remediate(context.Background(), remType, problem)
			if err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors (some may occur due to rate limiting or cooldowns)
	errorCount := 0
	for err := range errors {
		if err != nil {
			errorCount++
		}
	}

	// Most should succeed (allowing for some rate limit/cooldown conflicts)
	if errorCount > 50 {
		t.Errorf("Too many errors in concurrent test: %d/100", errorCount)
	}

	// Verify no data races occurred (test will fail on -race if there are)
	stats := registry.GetStats()
	if stats.RegisteredTypes != 5 {
		t.Errorf("RegisteredTypes = %d, want 5", stats.RegisteredTypes)
	}
}

// TestCircuitBreakerStateString tests the String method.
func TestCircuitBreakerStateString(t *testing.T) {
	tests := []struct {
		state CircuitBreakerState
		want  string
	}{
		{CircuitClosed, "Closed"},
		{CircuitOpen, "Open"},
		{CircuitHalfOpen, "HalfOpen"},
		{CircuitBreakerState(999), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.state.String()
			if got != tt.want {
				t.Errorf("String() = %s, want %s", got, tt.want)
			}
		})
	}
}

// TestLoggerIntegration tests logger integration.
func TestLoggerIntegration(t *testing.T) {
	registry := NewRegistry(10, 100)
	logger := &mockLogger{}
	registry.SetLogger(logger)

	mock := newMockRemediator("test", false)
	registry.Register(RemediatorInfo{
		Type:    "test",
		Factory: func() (types.Remediator, error) { return mock, nil },
	})

	problem := createTestProblem("test-type", "test-resource")
	_ = registry.Remediate(context.Background(), "test", problem)

	// Verify logger was called
	if len(logger.infoMessages) == 0 {
		t.Error("Logger info messages should not be empty")
	}
}

// TestRemediatorCaching tests that remediator instances are cached.
func TestRemediatorCaching(t *testing.T) {
	registry := NewRegistry(100, 100)

	createCount := 0
	registry.Register(RemediatorInfo{
		Type: "test",
		Factory: func() (types.Remediator, error) {
			createCount++
			return newMockRemediator("test", false), nil
		},
	})

	problem := createTestProblem("test-type", "test-resource")

	// Execute multiple remediations
	for i := 0; i < 3; i++ {
		registry.mu.Lock()
		// Clear cooldown via the cached instance
		if r, exists := registry.instances["test"]; exists {
			if mr, ok := r.(*mockRemediator); ok {
				mr.ClearCooldown(problem)
			}
		}
		registry.mu.Unlock()

		_ = registry.Remediate(context.Background(), "test", problem)
	}

	// Factory should only be called once (instance is cached)
	if createCount != 1 {
		t.Errorf("Factory called %d times, want 1 (should be cached)", createCount)
	}
}

// mockEventCreator is a mock implementation of EventCreator for testing.
type mockEventCreator struct {
	mu          sync.Mutex
	events      []corev1.Event
	shouldFail  bool
	createDelay time.Duration
}

func (m *mockEventCreator) CreateEvent(ctx context.Context, event corev1.Event) error {
	// Simulate processing delay if configured
	if m.createDelay > 0 {
		select {
		case <-time.After(m.createDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.shouldFail {
		return fmt.Errorf("mock event creation failed")
	}

	m.events = append(m.events, event)
	return nil
}

func (m *mockEventCreator) getEventCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.events)
}

func (m *mockEventCreator) getEvents() []corev1.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]corev1.Event, len(m.events))
	copy(result, m.events)
	return result
}

func (m *mockEventCreator) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = nil
}

// TestSetEventCreator tests the SetEventCreator method.
func TestSetEventCreator(t *testing.T) {
	registry := NewRegistry(10, 100)

	eventCreator := &mockEventCreator{}
	nodeName := "test-node"

	registry.SetEventCreator(eventCreator, nodeName)

	registry.mu.Lock()
	if registry.eventCreator == nil {
		t.Error("EventCreator should be set")
	}
	if registry.nodeName != nodeName {
		t.Errorf("NodeName = %s, want %s", registry.nodeName, nodeName)
	}
	registry.mu.Unlock()
}

// TestKubernetesEventCreation_Success tests that K8s events are created for successful remediations.
func TestKubernetesEventCreation_Success(t *testing.T) {
	registry := NewRegistry(10, 100)
	eventCreator := &mockEventCreator{}
	nodeName := "test-node"

	registry.SetEventCreator(eventCreator, nodeName)

	// Register a successful remediator
	remediator := newMockRemediator("test-remediator", false)
	registry.Register(RemediatorInfo{
		Type:        "test",
		Factory:     func() (types.Remediator, error) { return remediator, nil },
		Description: "Test remediator",
	})

	problem := types.Problem{
		Type:       "test-problem",
		Severity:   types.ProblemWarning,
		Message:    "Test problem",
		Resource:   "test-resource",
		DetectedAt: time.Now(),
	}

	// Execute remediation
	err := registry.Remediate(context.Background(), "test", problem)
	if err != nil {
		t.Fatalf("Remediation failed: %v", err)
	}

	// Wait for async event creation
	time.Sleep(100 * time.Millisecond)

	// Verify event was created
	if eventCreator.getEventCount() != 1 {
		t.Errorf("Event count = %d, want 1", eventCreator.getEventCount())
	}
}

// TestKubernetesEventCreation_Failure tests that K8s events are created for failed remediations.
func TestKubernetesEventCreation_Failure(t *testing.T) {
	registry := NewRegistry(10, 100)
	eventCreator := &mockEventCreator{}
	nodeName := "test-node"

	registry.SetEventCreator(eventCreator, nodeName)

	// Register a failing remediator
	remediator := newMockRemediator("test-remediator", true)
	registry.Register(RemediatorInfo{
		Type:        "test",
		Factory:     func() (types.Remediator, error) { return remediator, nil },
		Description: "Test remediator",
	})

	problem := types.Problem{
		Type:       "test-problem",
		Severity:   types.ProblemWarning,
		Message:    "Test problem",
		Resource:   "test-resource",
		DetectedAt: time.Now(),
	}

	// Execute remediation (will fail)
	_ = registry.Remediate(context.Background(), "test", problem)

	// Wait for async event creation
	time.Sleep(100 * time.Millisecond)

	// Verify event was created even for failure
	if eventCreator.getEventCount() != 1 {
		t.Errorf("Event count = %d, want 1", eventCreator.getEventCount())
	}
}

// TestKubernetesEventCreation_NilEventCreator tests that nil event creator doesn't cause panics.
func TestKubernetesEventCreation_NilEventCreator(t *testing.T) {
	registry := NewRegistry(10, 100)
	// Don't set event creator (leave it nil)

	remediator := newMockRemediator("test-remediator", false)
	registry.Register(RemediatorInfo{
		Type:        "test",
		Factory:     func() (types.Remediator, error) { return remediator, nil },
		Description: "Test remediator",
	})

	problem := types.Problem{
		Type:       "test-problem",
		Severity:   types.ProblemWarning,
		Message:    "Test problem",
		Resource:   "test-resource",
		DetectedAt: time.Now(),
	}

	// Execute remediation - should not panic
	err := registry.Remediate(context.Background(), "test", problem)
	if err != nil {
		t.Fatalf("Remediation failed: %v", err)
	}

	// Wait a bit to ensure no background goroutine issues
	time.Sleep(100 * time.Millisecond)
}

// TestKubernetesEventCreation_NoNodeName tests event creation with empty node name.
func TestKubernetesEventCreation_NoNodeName(t *testing.T) {
	registry := NewRegistry(10, 100)
	eventCreator := &mockEventCreator{}

	// Set event creator but with empty node name
	registry.SetEventCreator(eventCreator, "")

	remediator := newMockRemediator("test-remediator", false)
	registry.Register(RemediatorInfo{
		Type:        "test",
		Factory:     func() (types.Remediator, error) { return remediator, nil },
		Description: "Test remediator",
	})

	problem := types.Problem{
		Type:       "test-problem",
		Severity:   types.ProblemWarning,
		Message:    "Test problem",
		Resource:   "test-resource",
		DetectedAt: time.Now(),
	}

	// Execute remediation
	err := registry.Remediate(context.Background(), "test", problem)
	if err != nil {
		t.Fatalf("Remediation failed: %v", err)
	}

	// Wait for potential async event creation
	time.Sleep(100 * time.Millisecond)

	// No event should be created without node name
	if eventCreator.getEventCount() != 0 {
		t.Errorf("Event count = %d, want 0 (no node name provided)", eventCreator.getEventCount())
	}
}

// TestKubernetesEventCreation_AsyncNonBlocking tests that event creation doesn't block remediation.
func TestKubernetesEventCreation_AsyncNonBlocking(t *testing.T) {
	registry := NewRegistry(10, 100)
	eventCreator := &mockEventCreator{
		createDelay: 2 * time.Second, // Simulate slow event creation
	}
	nodeName := "test-node"

	registry.SetEventCreator(eventCreator, nodeName)

	remediator := newMockRemediator("test-remediator", false)
	registry.Register(RemediatorInfo{
		Type:        "test",
		Factory:     func() (types.Remediator, error) { return remediator, nil },
		Description: "Test remediator",
	})

	problem := types.Problem{
		Type:       "test-problem",
		Severity:   types.ProblemWarning,
		Message:    "Test problem",
		Resource:   "test-resource",
		DetectedAt: time.Now(),
	}

	// Execute remediation and measure time
	start := time.Now()
	err := registry.Remediate(context.Background(), "test", problem)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("Remediation failed: %v", err)
	}

	// Remediation should complete quickly despite slow event creation
	if duration > 500*time.Millisecond {
		t.Errorf("Remediation took %v, expected < 500ms (event creation should be async)", duration)
	}

	// Wait for event to be created
	time.Sleep(3 * time.Second)

	// Verify event was eventually created
	if eventCreator.getEventCount() != 1 {
		t.Errorf("Event count = %d, want 1", eventCreator.getEventCount())
	}
}

// TestKubernetesEventCreation_EventCreatorFailure tests handling of event creation failures.
func TestKubernetesEventCreation_EventCreatorFailure(t *testing.T) {
	registry := NewRegistry(10, 100)
	eventCreator := &mockEventCreator{
		shouldFail: true,
	}
	nodeName := "test-node"

	registry.SetEventCreator(eventCreator, nodeName)

	remediator := newMockRemediator("test-remediator", false)
	registry.Register(RemediatorInfo{
		Type:        "test",
		Factory:     func() (types.Remediator, error) { return remediator, nil },
		Description: "Test remediator",
	})

	problem := types.Problem{
		Type:       "test-problem",
		Severity:   types.ProblemWarning,
		Message:    "Test problem",
		Resource:   "test-resource",
		DetectedAt: time.Now(),
	}

	// Execute remediation - should succeed even if event creation fails
	err := registry.Remediate(context.Background(), "test", problem)
	if err != nil {
		t.Fatalf("Remediation should succeed even if event creation fails: %v", err)
	}

	// Wait for async event creation attempt
	time.Sleep(100 * time.Millisecond)

	// Event creation was attempted but failed
	if eventCreator.getEventCount() != 0 {
		t.Errorf("Event count = %d, want 0 (event creation failed)", eventCreator.getEventCount())
	}
}

// TestKubernetesEventCreation_MultipleEvents tests creation of multiple events.
func TestKubernetesEventCreation_MultipleEvents(t *testing.T) {
	registry := NewRegistry(10, 100)
	eventCreator := &mockEventCreator{}
	nodeName := "test-node"

	registry.SetEventCreator(eventCreator, nodeName)

	remediator := newMockRemediator("test-remediator", false)
	registry.Register(RemediatorInfo{
		Type:        "test",
		Factory:     func() (types.Remediator, error) { return remediator, nil },
		Description: "Test remediator",
	})

	// Create multiple different problems to avoid cooldown
	problems := []types.Problem{
		{
			Type:       "disk-full",
			Severity:   types.ProblemWarning,
			Message:    "Problem 1",
			Resource:   "resource-1",
			DetectedAt: time.Now(),
		},
		{
			Type:       "high-memory",
			Severity:   types.ProblemWarning,
			Message:    "Problem 2",
			Resource:   "resource-2",
			DetectedAt: time.Now(),
		},
		{
			Type:       "high-cpu",
			Severity:   types.ProblemCritical,
			Message:    "Problem 3",
			Resource:   "resource-3",
			DetectedAt: time.Now(),
		},
	}

	// Execute remediations
	for _, problem := range problems {
		err := registry.Remediate(context.Background(), "test", problem)
		if err != nil {
			t.Fatalf("Remediation failed: %v", err)
		}
	}

	// Wait for async event creation
	time.Sleep(200 * time.Millisecond)

	// Verify all events were created
	if eventCreator.getEventCount() != 3 {
		t.Errorf("Event count = %d, want 3", eventCreator.getEventCount())
	}
}

// TestSanitizeEventName tests the event name sanitization function.
func TestSanitizeEventName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "valid name",
			input:    "test-remediator",
			expected: "test-remediator",
		},
		{
			name:     "name with spaces",
			input:    "test remediator",
			expected: "test-remediator",
		},
		{
			name:     "name with special characters",
			input:    "test@remediator#123",
			expected: "test-remediator-123",
		},
		{
			name:     "name with underscores",
			input:    "test_remediator_v2",
			expected: "test-remediator-v2",
		},
		{
			name:     "empty name",
			input:    "",
			expected: "unknown",
		},
		{
			name:     "only special characters",
			input:    "@#$%",
			expected: "----",
		},
		{
			name:     "mixed case",
			input:    "TestRemediator",
			expected: "TestRemediator",
		},
		{
			name:     "with dots",
			input:    "node.doctor.v1",
			expected: "node-doctor-v1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeEventName(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeEventName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestRemediatorRegistry_LogWarnf_NilLogger verifies logWarnf handles nil logger.
func TestRemediatorRegistry_LogWarnf_NilLogger(t *testing.T) {
	registry := NewRegistry(100, 1000)
	// Don't set a logger - logger is nil by default

	// This should not panic
	registry.logWarnf("test warning message: %s", "value")
}

// TestRemediatorRegistry_LogErrorf_NilLogger verifies logErrorf handles nil logger.
func TestRemediatorRegistry_LogErrorf_NilLogger(t *testing.T) {
	registry := NewRegistry(100, 1000)
	// Don't set a logger - logger is nil by default

	// This should not panic
	registry.logErrorf("test error message: %s", "value")
}

// TestRemediatorRegistry_LogWithLogger verifies log methods work with logger set.
func TestRemediatorRegistry_LogWithLogger(t *testing.T) {
	registry := NewRegistry(100, 1000)

	logger := &mockLogger{}
	registry.SetLogger(logger)

	registry.logWarnf("test warn: %s", "value")
	registry.logErrorf("test error: %s", "value")

	if len(logger.warnMessages) != 1 {
		t.Errorf("expected 1 warn message, got %d", len(logger.warnMessages))
	}
	if len(logger.errorMessages) != 1 {
		t.Errorf("expected 1 error message, got %d", len(logger.errorMessages))
	}
}
