package remediation

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/remediators"
	"github.com/supporttools/node-doctor/pkg/types"
	"github.com/supporttools/node-doctor/test"
)

// TestRemediationEndToEndFlow tests the complete remediation workflow
func TestRemediationEndToEndFlow(t *testing.T) {
	ctx := context.Background()

	// Create a mock remediator that tracks remediation attempts
	mockRemediator := newMockRemediator("test-remediator", 1*time.Second)

	// Create registry (0 = no rate limit, 100 = history size)
	registry := remediators.NewRegistry(0, 100)

	// Register the mock remediator
	registry.Register(remediators.RemediatorInfo{
		Type: "test-problem",
		Factory: func() (types.Remediator, error) {
			return mockRemediator, nil
		},
		Description: "Mock remediator for testing",
	})

	// Create a problem
	problem := types.NewProblem(
		"test-problem",
		"test-resource",
		types.ProblemCritical,
		"Test problem for remediation",
	)

	// Test 1: Remediate the problem
	err := registry.Remediate(ctx, "test-problem", *problem)
	test.AssertNoError(t, err, "First remediation should succeed")

	// Verify remediation was attempted
	test.AssertEqual(t, 1, mockRemediator.GetAttemptCount(), "Expected 1 remediation attempt")

	// Test 2: Attempt immediate re-remediation (should be blocked by cooldown)
	err = registry.Remediate(ctx, "test-problem", *problem)
	test.AssertError(t, err, "Second remediation should fail due to cooldown")
	test.AssertEqual(t, 1, mockRemediator.GetAttemptCount(), "Should still have only 1 attempt")

	// Test 3: Wait for cooldown and retry
	time.Sleep(1100 * time.Millisecond) // Wait for cooldown to expire

	err = registry.Remediate(ctx, "test-problem", *problem)
	test.AssertNoError(t, err, "Remediation after cooldown should succeed")
	test.AssertEqual(t, 2, mockRemediator.GetAttemptCount(), "Should have 2 attempts after cooldown")
}

// TestCircuitBreakerIntegration tests that circuit breaker prevents cascading failures
func TestCircuitBreakerIntegration(t *testing.T) {
	ctx := context.Background()

	// Create a failing remediator
	failingRemediator := newFailingRemediator()

	// Create registry with circuit breaker (0 = no rate limit, 100 = history size)
	registry := remediators.NewRegistry(0, 100)
	err := registry.SetCircuitBreakerConfig(remediators.CircuitBreakerConfig{
		Threshold:        3,               // Open after 3 failures
		Timeout:          2 * time.Second, // Stay open for 2 seconds
		SuccessThreshold: 2,               // Close after 2 successes
	})
	test.AssertNoError(t, err, "Failed to set circuit breaker config")

	registry.Register(remediators.RemediatorInfo{
		Type: "failing-problem",
		Factory: func() (types.Remediator, error) {
			return failingRemediator, nil
		},
		Description: "Failing remediator for testing",
	})

	// Test 1: Trigger circuit breaker with failures
	for i := 0; i < 3; i++ {
		problem := types.NewProblem(
			"failing-problem",
			fmt.Sprintf("resource-%d", i),
			types.ProblemCritical,
			"Failing problem",
		)
		registry.Remediate(ctx, "failing-problem", *problem)
	}

	// Verify circuit is now open
	stats := registry.GetStats()
	test.AssertEqual(t, remediators.CircuitOpen, stats.CircuitState, "Circuit should be open")

	// Test 2: Verify remediation is blocked while circuit is open
	problem := types.NewProblem(
		"failing-problem",
		"blocked-resource",
		types.ProblemCritical,
		"This should be blocked",
	)
	err = registry.Remediate(ctx, "failing-problem", *problem)
	test.AssertError(t, err, "Remediation should be blocked by open circuit")

	// Test 3: Wait for circuit to enter half-open state
	time.Sleep(2100 * time.Millisecond)

	// Make the remediator succeed now
	failingRemediator.SetShouldFail(false)

	// Circuit should be in half-open, try to remediate
	err = registry.Remediate(ctx, "failing-problem", *problem)
	test.AssertNoError(t, err, "First remediation in half-open should succeed")

	// Send another success to close the circuit
	problem2 := types.NewProblem(
		"failing-problem",
		"recovery-resource",
		types.ProblemCritical,
		"Recovery attempt",
	)
	err = registry.Remediate(ctx, "failing-problem", *problem2)
	test.AssertNoError(t, err, "Second success should close circuit")

	// Verify circuit is closed
	stats = registry.GetStats()
	test.AssertEqual(t, remediators.CircuitClosed, stats.CircuitState, "Circuit should be closed")
}

// TestRateLimitingIntegration tests that rate limiting prevents remediation storms
func TestRateLimitingIntegration(t *testing.T) {
	ctx := context.Background()

	mockRemediator := newMockRemediator("rate-test", 1*time.Millisecond) // Minimal cooldown

	// Create registry with rate limiting (10 per hour, 100 history)
	registry := remediators.NewRegistry(10, 100)

	registry.Register(remediators.RemediatorInfo{
		Type: "rate-test-problem",
		Factory: func() (types.Remediator, error) {
			return mockRemediator, nil
		},
		Description: "Rate test remediator",
	})

	// Test 1: Perform remediations up to the limit
	for i := 0; i < 10; i++ {
		problem := types.NewProblem(
			"rate-test-problem",
			fmt.Sprintf("resource-%d", i),
			types.ProblemCritical,
			"Rate test problem",
		)
		err := registry.Remediate(ctx, "rate-test-problem", *problem)
		test.AssertNoError(t, err, "Remediation %d should succeed", i+1)
	}

	test.AssertEqual(t, 10, mockRemediator.GetAttemptCount(), "Should have 10 attempts")

	// Test 2: Next remediation should be rate limited
	problem := types.NewProblem(
		"rate-test-problem",
		"rate-limited-resource",
		types.ProblemCritical,
		"This should be rate limited",
	)
	err := registry.Remediate(ctx, "rate-test-problem", *problem)
	test.AssertError(t, err, "Remediation should be blocked by rate limit")
	test.AssertEqual(t, 10, mockRemediator.GetAttemptCount(), "Attempt count should not increase")
}

// TestConcurrentRemediationAttempts tests thread safety of remediation
func TestConcurrentRemediationAttempts(t *testing.T) {
	ctx := context.Background()

	mockRemediator := newMockRemediator("concurrent-test", 1*time.Millisecond)

	registry := remediators.NewRegistry(0, 100)
	registry.Register(remediators.RemediatorInfo{
		Type: "concurrent-problem",
		Factory: func() (types.Remediator, error) {
			return mockRemediator, nil
		},
		Description: "Concurrent test remediator",
	})

	// Launch concurrent remediation attempts
	const numGoroutines = 20
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			problem := types.NewProblem(
				"concurrent-problem",
				fmt.Sprintf("resource-%d", id),
				types.ProblemCritical,
				fmt.Sprintf("Concurrent problem %d", id),
			)
			if err := registry.Remediate(ctx, "concurrent-problem", *problem); err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for unexpected errors
	errorCount := 0
	for err := range errors {
		t.Logf("Remediation error: %v", err)
		errorCount++
	}

	// All should succeed (different resources, no conflicts)
	test.AssertEqual(t, 0, errorCount, "Expected no errors from concurrent remediations")
	test.AssertEqual(t, numGoroutines, mockRemediator.GetAttemptCount(), "All remediations should succeed")
}

// TestMaxAttemptsLimiting tests that remediator respects max attempts
func TestMaxAttemptsLimiting(t *testing.T) {
	ctx := context.Background()

	// Create remediator with max 3 attempts
	mockRemediator := newMockRemediatorWithMaxAttempts("max-attempts-test", 1*time.Millisecond, 3)

	registry := remediators.NewRegistry(0, 100)
	registry.Register(remediators.RemediatorInfo{
		Type: "max-attempts-problem",
		Factory: func() (types.Remediator, error) {
			return mockRemediator, nil
		},
		Description: "Max attempts test remediator",
	})

	problem := types.NewProblem(
		"max-attempts-problem",
		"test-resource",
		types.ProblemCritical,
		"Max attempts test",
	)

	// Test 1: First 3 attempts should succeed
	for i := 0; i < 3; i++ {
		err := registry.Remediate(ctx, "max-attempts-problem", *problem)
		test.AssertNoError(t, err, "Attempt %d should succeed", i+1)
		// Sleep to allow cooldown to expire before next attempt
		time.Sleep(2 * time.Millisecond)
	}

	test.AssertEqual(t, 3, mockRemediator.GetAttemptCount(), "Should have 3 attempts")

	// Test 2: 4th attempt should fail due to max attempts (even after cooldown)
	err := registry.Remediate(ctx, "max-attempts-problem", *problem)
	test.AssertError(t, err, "4th attempt should fail due to max attempts")
	test.AssertEqual(t, 3, mockRemediator.GetAttemptCount(), "Attempt count should not exceed max")
}

// mockRemediator is a mock Remediator implementation
type mockRemediator struct {
	*remediators.BaseRemediator
	mu           sync.Mutex
	attemptCount int
}

func newMockRemediator(name string, cooldown time.Duration) *mockRemediator {
	m := &mockRemediator{
		attemptCount: 0,
	}

	base, _ := remediators.NewBaseRemediator(name, cooldown)
	base.SetRemediateFunc(m.remediate)

	m.BaseRemediator = base
	return m
}

func newMockRemediatorWithMaxAttempts(name string, cooldown time.Duration, maxAttempts int) *mockRemediator {
	m := &mockRemediator{
		attemptCount: 0,
	}

	base, _ := remediators.NewBaseRemediator(name, cooldown)
	base.SetRemediateFunc(m.remediate)
	base.SetMaxAttempts(maxAttempts)

	m.BaseRemediator = base
	return m
}

func (m *mockRemediator) remediate(ctx context.Context, problem types.Problem) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.attemptCount++
	return nil // Success
}

func (m *mockRemediator) GetAttemptCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.attemptCount
}

// failingRemediator is a mock Remediator that can be configured to fail
type failingRemediator struct {
	*remediators.BaseRemediator
	mu           sync.Mutex
	shouldFail   bool
	attemptCount int
}

func newFailingRemediator() *failingRemediator {
	f := &failingRemediator{
		shouldFail: true,
	}

	base, _ := remediators.NewBaseRemediator("failing-remediator", 1*time.Millisecond)
	base.SetRemediateFunc(f.remediate)

	f.BaseRemediator = base
	return f
}

func (f *failingRemediator) remediate(ctx context.Context, problem types.Problem) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.attemptCount++

	if f.shouldFail {
		return fmt.Errorf("remediation failed intentionally")
	}
	return nil
}

func (f *failingRemediator) SetShouldFail(fail bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.shouldFail = fail
}

func (f *failingRemediator) GetAttemptCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.attemptCount
}
