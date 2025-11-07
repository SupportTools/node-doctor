package remediators

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// mockLogger implements the Logger interface for testing
type mockLogger struct {
	infoMessages  []string
	warnMessages  []string
	errorMessages []string
	mu            sync.Mutex
}

func (m *mockLogger) Infof(format string, args ...interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.infoMessages = append(m.infoMessages, fmt.Sprintf(format, args...))
}

func (m *mockLogger) Warnf(format string, args ...interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.warnMessages = append(m.warnMessages, fmt.Sprintf(format, args...))
}

func (m *mockLogger) Errorf(format string, args ...interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errorMessages = append(m.errorMessages, fmt.Sprintf(format, args...))
}

func (m *mockLogger) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.infoMessages = nil
	m.warnMessages = nil
	m.errorMessages = nil
}

// Test helpers

func createTestProblem(problemType, resource string) types.Problem {
	p := types.NewProblem(problemType, resource, types.ProblemCritical, "Test problem")
	return *p // Dereference pointer to return value
}

// TestNewBaseRemediator tests the constructor
func TestNewBaseRemediator(t *testing.T) {
	tests := []struct {
		name        string
		remName     string
		cooldown    time.Duration
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid configuration",
			remName:     "test-remediator",
			cooldown:    5 * time.Minute,
			expectError: false,
		},
		{
			name:        "empty name",
			remName:     "",
			cooldown:    5 * time.Minute,
			expectError: true,
			errorMsg:    "name cannot be empty",
		},
		{
			name:        "zero cooldown",
			remName:     "test",
			cooldown:    0,
			expectError: true,
			errorMsg:    "cooldown must be greater than 0",
		},
		{
			name:        "negative cooldown",
			remName:     "test",
			cooldown:    -1 * time.Second,
			expectError: true,
			errorMsg:    "cooldown must be greater than 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			remediator, err := NewBaseRemediator(tt.remName, tt.cooldown)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errorMsg)
				} else if err.Error() == "" || tt.errorMsg == "" {
					// Skip message check if not specified
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if remediator == nil {
					t.Fatal("expected non-nil remediator")
				}
				if remediator.GetName() != tt.remName {
					t.Errorf("name = %q, want %q", remediator.GetName(), tt.remName)
				}
				if remediator.GetCooldown() != tt.cooldown {
					t.Errorf("cooldown = %v, want %v", remediator.GetCooldown(), tt.cooldown)
				}
				if remediator.GetMaxAttempts() != DefaultMaxAttempts {
					t.Errorf("maxAttempts = %d, want %d", remediator.GetMaxAttempts(), DefaultMaxAttempts)
				}
			}
		})
	}
}

// TestSetRemediateFunc tests setting the remediate function
func TestSetRemediateFunc(t *testing.T) {
	remediator, _ := NewBaseRemediator("test", 5*time.Minute)

	t.Run("valid function", func(t *testing.T) {
		fn := func(ctx context.Context, p types.Problem) error {
			return nil
		}
		err := remediator.SetRemediateFunc(fn)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("nil function", func(t *testing.T) {
		err := remediator.SetRemediateFunc(nil)
		if err == nil {
			t.Error("expected error for nil function, got nil")
		}
	})
}

// TestSetLogger tests setting the logger
func TestSetLogger(t *testing.T) {
	remediator, _ := NewBaseRemediator("test", 5*time.Minute)
	logger := &mockLogger{}

	err := remediator.SetLogger(logger)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Verify logger works by triggering a log
	remediator.logInfof("test message")
	if len(logger.infoMessages) != 1 {
		t.Errorf("expected 1 info message, got %d", len(logger.infoMessages))
	}
}

// TestSetMaxAttempts tests setting max attempts
func TestSetMaxAttempts(t *testing.T) {
	remediator, _ := NewBaseRemediator("test", 5*time.Minute)

	tests := []struct {
		name        string
		maxAttempts int
		expectError bool
	}{
		{"valid max attempts", 5, false},
		{"max attempts = 1", 1, false},
		{"max attempts = 0", 0, true},
		{"negative max attempts", -1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := remediator.SetMaxAttempts(tt.maxAttempts)
			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if remediator.GetMaxAttempts() != tt.maxAttempts {
					t.Errorf("maxAttempts = %d, want %d", remediator.GetMaxAttempts(), tt.maxAttempts)
				}
			}
		})
	}
}

// TestIsInCooldown tests cooldown detection
func TestIsInCooldown(t *testing.T) {
	remediator, _ := NewBaseRemediator("test", 2*time.Second)
	_ = remediator.SetRemediateFunc(func(ctx context.Context, p types.Problem) error {
		return nil
	})

	problem := createTestProblem("test-problem", "test-resource")

	t.Run("no previous attempt", func(t *testing.T) {
		if remediator.IsInCooldown(problem) {
			t.Error("expected not in cooldown when no attempts made")
		}
	})

	t.Run("immediately after attempt", func(t *testing.T) {
		_ = remediator.Remediate(context.Background(), problem)
		if !remediator.IsInCooldown(problem) {
			t.Error("expected in cooldown immediately after attempt")
		}
	})

	t.Run("after cooldown expires", func(t *testing.T) {
		time.Sleep(2100 * time.Millisecond) // Wait for cooldown to expire
		if remediator.IsInCooldown(problem) {
			t.Error("expected not in cooldown after cooldown period")
		}
	})
}

// TestGetCooldownRemaining tests cooldown remaining time
func TestGetCooldownRemaining(t *testing.T) {
	cooldownPeriod := 2 * time.Second
	remediator, _ := NewBaseRemediator("test", cooldownPeriod)
	_ = remediator.SetRemediateFunc(func(ctx context.Context, p types.Problem) error {
		return nil
	})

	problem := createTestProblem("test-problem", "test-resource")

	t.Run("no previous attempt", func(t *testing.T) {
		remaining := remediator.GetCooldownRemaining(problem)
		if remaining != 0 {
			t.Errorf("expected 0 remaining, got %v", remaining)
		}
	})

	t.Run("after attempt", func(t *testing.T) {
		_ = remediator.Remediate(context.Background(), problem)
		remaining := remediator.GetCooldownRemaining(problem)

		// Should be close to cooldown period (allow 100ms tolerance)
		if remaining < cooldownPeriod-100*time.Millisecond || remaining > cooldownPeriod {
			t.Errorf("expected remaining ~%v, got %v", cooldownPeriod, remaining)
		}
	})

	t.Run("after cooldown expires", func(t *testing.T) {
		time.Sleep(2100 * time.Millisecond)
		remaining := remediator.GetCooldownRemaining(problem)
		if remaining != 0 {
			t.Errorf("expected 0 remaining after cooldown, got %v", remaining)
		}
	})
}

// TestClearCooldown tests clearing cooldown
func TestClearCooldown(t *testing.T) {
	remediator, _ := NewBaseRemediator("test", 10*time.Second)
	_ = remediator.SetRemediateFunc(func(ctx context.Context, p types.Problem) error {
		return nil
	})

	problem := createTestProblem("test-problem", "test-resource")

	// Make an attempt to start cooldown
	_ = remediator.Remediate(context.Background(), problem)
	if !remediator.IsInCooldown(problem) {
		t.Fatal("expected to be in cooldown after attempt")
	}

	// Clear cooldown
	remediator.ClearCooldown(problem)

	// Should not be in cooldown anymore
	if remediator.IsInCooldown(problem) {
		t.Error("expected not in cooldown after clearing")
	}
}

// TestGetAttemptCount tests attempt counting
func TestGetAttemptCount(t *testing.T) {
	remediator, _ := NewBaseRemediator("test", 1*time.Millisecond) // Very short cooldown
	_ = remediator.SetRemediateFunc(func(ctx context.Context, p types.Problem) error {
		return nil
	})

	problem := createTestProblem("test-problem", "test-resource")

	t.Run("initial count", func(t *testing.T) {
		count := remediator.GetAttemptCount(problem)
		if count != 0 {
			t.Errorf("expected 0 attempts initially, got %d", count)
		}
	})

	t.Run("after first attempt", func(t *testing.T) {
		_ = remediator.Remediate(context.Background(), problem)
		count := remediator.GetAttemptCount(problem)
		if count != 1 {
			t.Errorf("expected 1 attempt, got %d", count)
		}
	})

	t.Run("after second attempt", func(t *testing.T) {
		time.Sleep(2 * time.Millisecond) // Wait for cooldown
		_ = remediator.Remediate(context.Background(), problem)
		count := remediator.GetAttemptCount(problem)
		if count != 2 {
			t.Errorf("expected 2 attempts, got %d", count)
		}
	})
}

// TestResetAttempts tests resetting attempt counter
func TestResetAttempts(t *testing.T) {
	remediator, _ := NewBaseRemediator("test", 1*time.Millisecond)
	_ = remediator.SetRemediateFunc(func(ctx context.Context, p types.Problem) error {
		return nil
	})

	problem := createTestProblem("test-problem", "test-resource")

	// Make some attempts
	_ = remediator.Remediate(context.Background(), problem)
	time.Sleep(2 * time.Millisecond)
	_ = remediator.Remediate(context.Background(), problem)

	if remediator.GetAttemptCount(problem) != 2 {
		t.Fatalf("expected 2 attempts, got %d", remediator.GetAttemptCount(problem))
	}

	// Reset attempts
	remediator.ResetAttempts(problem)

	// Count should be 0
	if remediator.GetAttemptCount(problem) != 0 {
		t.Errorf("expected 0 attempts after reset, got %d", remediator.GetAttemptCount(problem))
	}
}

// TestCanRemediate tests the CanRemediate logic
func TestCanRemediate(t *testing.T) {
	cooldown := 2 * time.Second
	remediator, _ := NewBaseRemediator("test", cooldown)
	_ = remediator.SetRemediateFunc(func(ctx context.Context, p types.Problem) error {
		return nil
	})
	_ = remediator.SetMaxAttempts(3)

	problem := createTestProblem("test-problem", "test-resource")

	t.Run("no attempts made", func(t *testing.T) {
		if !remediator.CanRemediate(problem) {
			t.Error("expected can remediate when no attempts made")
		}
	})

	t.Run("during cooldown", func(t *testing.T) {
		_ = remediator.Remediate(context.Background(), problem)
		if remediator.CanRemediate(problem) {
			t.Error("expected cannot remediate during cooldown")
		}
	})

	t.Run("after cooldown expires", func(t *testing.T) {
		time.Sleep(cooldown + 100*time.Millisecond)
		if !remediator.CanRemediate(problem) {
			t.Error("expected can remediate after cooldown expires")
		}
	})

	t.Run("max attempts exceeded", func(t *testing.T) {
		// Reset and make max attempts
		remediator.ResetAttempts(problem)
		remediator.ClearCooldown(problem)

		for i := 0; i < 3; i++ {
			_ = remediator.Remediate(context.Background(), problem)
			remediator.ClearCooldown(problem) // Clear cooldown to allow next attempt
		}

		// Now should not be able to remediate
		if remediator.CanRemediate(problem) {
			t.Error("expected cannot remediate after max attempts exceeded")
		}
	})
}

// TestRemediate tests the Remediate method
func TestRemediate(t *testing.T) {
	t.Run("successful remediation", func(t *testing.T) {
		remediator, _ := NewBaseRemediator("test", 5*time.Minute)
		called := false
		_ = remediator.SetRemediateFunc(func(ctx context.Context, p types.Problem) error {
			called = true
			return nil
		})

		problem := createTestProblem("test-problem", "test-resource")
		err := remediator.Remediate(context.Background(), problem)

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if !called {
			t.Error("remediateFunc was not called")
		}
	})

	t.Run("remediation error", func(t *testing.T) {
		remediator, _ := NewBaseRemediator("test", 5*time.Minute)
		testErr := errors.New("remediation failed")
		_ = remediator.SetRemediateFunc(func(ctx context.Context, p types.Problem) error {
			return testErr
		})

		problem := createTestProblem("test-problem", "test-resource")
		err := remediator.Remediate(context.Background(), problem)

		if err == nil {
			t.Error("expected error, got nil")
		}
		if !errors.Is(err, testErr) {
			t.Errorf("expected error to wrap %v, got %v", testErr, err)
		}
	})

	t.Run("panic recovery", func(t *testing.T) {
		remediator, _ := NewBaseRemediator("test", 5*time.Minute)
		_ = remediator.SetRemediateFunc(func(ctx context.Context, p types.Problem) error {
			panic("test panic")
		})

		problem := createTestProblem("test-problem", "test-resource")
		err := remediator.Remediate(context.Background(), problem)

		if err == nil {
			t.Error("expected error from panic recovery, got nil")
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		remediator, _ := NewBaseRemediator("test", 5*time.Minute)
		_ = remediator.SetRemediateFunc(func(ctx context.Context, p types.Problem) error {
			t.Error("remediateFunc should not be called with cancelled context")
			return nil
		})

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel before calling Remediate

		problem := createTestProblem("test-problem", "test-resource")
		err := remediator.Remediate(ctx, problem)

		if err == nil {
			t.Error("expected error for cancelled context, got nil")
		}
	})

	t.Run("no remediateFunc set", func(t *testing.T) {
		remediator, _ := NewBaseRemediator("test", 5*time.Minute)
		// Don't set remediateFunc

		problem := createTestProblem("test-problem", "test-resource")
		err := remediator.Remediate(context.Background(), problem)

		if err == nil {
			t.Error("expected error when remediateFunc not set, got nil")
		}
	})

	t.Run("logging integration", func(t *testing.T) {
		remediator, _ := NewBaseRemediator("test", 5*time.Minute)
		logger := &mockLogger{}
		_ = remediator.SetLogger(logger)
		_ = remediator.SetRemediateFunc(func(ctx context.Context, p types.Problem) error {
			return nil
		})

		problem := createTestProblem("test-problem", "test-resource")
		_ = remediator.Remediate(context.Background(), problem)

		if len(logger.infoMessages) < 2 {
			t.Errorf("expected at least 2 info messages, got %d", len(logger.infoMessages))
		}
	})
}

// TestGenerateProblemKey tests problem key generation
func TestGenerateProblemKey(t *testing.T) {
	tests := []struct {
		name     string
		problem  types.Problem
		expected string
	}{
		{
			name:     "standard problem",
			problem:  createTestProblem("kubelet-unhealthy", "kubelet.service"),
			expected: "kubelet-unhealthy:kubelet.service",
		},
		{
			name:     "disk pressure problem",
			problem:  createTestProblem("disk-pressure", "/var/lib/docker"),
			expected: "disk-pressure:/var/lib/docker",
		},
		{
			name:     "memory pressure problem",
			problem:  createTestProblem("memory-pressure", "node"),
			expected: "memory-pressure:node",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := GenerateProblemKey(tt.problem)
			if key != tt.expected {
				t.Errorf("GenerateProblemKey() = %q, want %q", key, tt.expected)
			}
		})
	}
}

// TestDifferentProblemKeys tests that different problems generate different keys
func TestDifferentProblemKeys(t *testing.T) {
	problem1 := createTestProblem("type1", "resource1")
	problem2 := createTestProblem("type2", "resource1")
	problem3 := createTestProblem("type1", "resource2")

	key1 := GenerateProblemKey(problem1)
	key2 := GenerateProblemKey(problem2)
	key3 := GenerateProblemKey(problem3)

	if key1 == key2 {
		t.Error("different problem types should generate different keys")
	}
	if key1 == key3 {
		t.Error("different resources should generate different keys")
	}
}

// TestConcurrentRemediation tests thread safety
func TestConcurrentRemediation(t *testing.T) {
	remediator, _ := NewBaseRemediator("test", 1*time.Millisecond)
	var counter int
	var mu sync.Mutex

	_ = remediator.SetRemediateFunc(func(ctx context.Context, p types.Problem) error {
		mu.Lock()
		counter++
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		return nil
	})

	// Create multiple different problems
	problems := []types.Problem{
		createTestProblem("type1", "resource1"),
		createTestProblem("type2", "resource2"),
		createTestProblem("type3", "resource3"),
	}

	var wg sync.WaitGroup
	for _, problem := range problems {
		wg.Add(1)
		go func(p types.Problem) {
			defer wg.Done()
			_ = remediator.Remediate(context.Background(), p)
		}(problem)
	}

	wg.Wait()

	mu.Lock()
	finalCount := counter
	mu.Unlock()

	if finalCount != 3 {
		t.Errorf("expected 3 remediation calls, got %d", finalCount)
	}
}

// TestConcurrentStateQueries tests concurrent state access
func TestConcurrentStateQueries(t *testing.T) {
	remediator, _ := NewBaseRemediator("test", 100*time.Millisecond)
	_ = remediator.SetRemediateFunc(func(ctx context.Context, p types.Problem) error {
		return nil
	})

	problem := createTestProblem("test-problem", "test-resource")

	var wg sync.WaitGroup

	// Start remediation in background
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			_ = remediator.Remediate(context.Background(), problem)
			time.Sleep(20 * time.Millisecond)
		}
	}()

	// Query state concurrently
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_ = remediator.IsInCooldown(problem)
				_ = remediator.GetAttemptCount(problem)
				_ = remediator.GetCooldownRemaining(problem)
				_ = remediator.CanRemediate(problem)
				time.Sleep(10 * time.Millisecond)
			}
		}()
	}

	wg.Wait()
	// If we get here without deadlock or race, test passes
}

// TestEmbeddingPattern demonstrates embedding BaseRemediator
func TestEmbeddingPattern(t *testing.T) {
	// Define a concrete remediator that embeds BaseRemediator
	type TestRemediator struct {
		*BaseRemediator
		config string
	}

	// Constructor
	newTestRemediator := func(name string, config string) (*TestRemediator, error) {
		base, err := NewBaseRemediator(name, CooldownMedium)
		if err != nil {
			return nil, err
		}

		tr := &TestRemediator{
			BaseRemediator: base,
			config:         config,
		}

		// Note: Remediation logic will be set by the test
		// to demonstrate the pattern

		return tr, nil
	}

	// Test the pattern
	t.Run("successful remediation", func(t *testing.T) {
		tr, err := newTestRemediator("test-rem", "success")
		if err != nil {
			t.Fatalf("failed to create remediator: %v", err)
		}

		// Manually set the remediate func with closure
		_ = tr.SetRemediateFunc(func(ctx context.Context, problem types.Problem) error {
			// Use the config from the concrete remediator
			if tr.config == "fail" {
				return errors.New("configured to fail")
			}
			return nil
		})

		problem := createTestProblem("test", "resource")
		err = tr.Remediate(context.Background(), problem)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("configured failure", func(t *testing.T) {
		tr, err := newTestRemediator("test-rem", "fail")
		if err != nil {
			t.Fatalf("failed to create remediator: %v", err)
		}

		// Manually set the remediate func with closure
		_ = tr.SetRemediateFunc(func(ctx context.Context, problem types.Problem) error {
			// Use the config from the concrete remediator
			if tr.config == "fail" {
				return errors.New("configured to fail")
			}
			return nil
		})

		problem := createTestProblem("test", "resource")
		err = tr.Remediate(context.Background(), problem)
		if err == nil {
			t.Error("expected error, got nil")
		}
	})
}
