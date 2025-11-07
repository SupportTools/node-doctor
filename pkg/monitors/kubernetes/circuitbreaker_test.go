package kubernetes

import (
	"testing"
	"time"
)

// mockClock provides a controllable time source for testing.
type mockClock struct {
	current time.Time
}

func (m *mockClock) now() time.Time {
	return m.current
}

func (m *mockClock) advance(d time.Duration) {
	m.current = m.current.Add(d)
}

func TestCircuitBreakerStates(t *testing.T) {
	tests := []struct {
		name          string
		config        *CircuitBreakerConfig
		wantErr       bool
		initialState  CircuitState
		expectedState CircuitState
	}{
		{
			name: "default configuration",
			config: &CircuitBreakerConfig{
				Enabled: true,
			},
			wantErr:       false,
			initialState:  StateClosed,
			expectedState: StateClosed,
		},
		{
			name: "custom thresholds",
			config: &CircuitBreakerConfig{
				Enabled:             true,
				FailureThreshold:    2,
				OpenTimeout:         10 * time.Second,
				HalfOpenMaxRequests: 2,
			},
			wantErr:       false,
			initialState:  StateClosed,
			expectedState: StateClosed,
		},
		{
			name: "zero failure threshold gets default",
			config: &CircuitBreakerConfig{
				Enabled:          true,
				FailureThreshold: 0, // Will get default value (5)
			},
			wantErr:       false,
			initialState:  StateClosed,
			expectedState: StateClosed,
		},
		{
			name: "zero open timeout gets default",
			config: &CircuitBreakerConfig{
				Enabled:          true,
				FailureThreshold: 1,
				OpenTimeout:      0, // Will get default value (30s)
			},
			wantErr:       false,
			initialState:  StateClosed,
			expectedState: StateClosed,
		},
		{
			name: "zero half-open max requests gets default",
			config: &CircuitBreakerConfig{
				Enabled:             true,
				FailureThreshold:    1,
				OpenTimeout:         10 * time.Second,
				HalfOpenMaxRequests: 0, // Will get default value (3)
			},
			wantErr:       false,
			initialState:  StateClosed,
			expectedState: StateClosed,
		},
		{
			name: "max backoff less than open timeout",
			config: &CircuitBreakerConfig{
				Enabled:           true,
				FailureThreshold:  1,
				OpenTimeout:       60 * time.Second,
				MaxBackoffTimeout: 30 * time.Second,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb, err := NewCircuitBreaker(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewCircuitBreaker() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}

			if state := cb.State(); state != tt.expectedState {
				t.Errorf("Circuit breaker state = %v, want %v", state, tt.expectedState)
			}
		})
	}
}

func TestCircuitBreakerClosedToOpen(t *testing.T) {
	clock := &mockClock{current: time.Now()}
	config := &CircuitBreakerConfig{
		Enabled:             true,
		FailureThreshold:    3,
		OpenTimeout:         30 * time.Second,
		HalfOpenMaxRequests: 2,
	}

	cb, err := NewCircuitBreaker(config)
	if err != nil {
		t.Fatalf("Failed to create circuit breaker: %v", err)
	}
	cb.now = clock.now

	// Initial state should be Closed
	if state := cb.State(); state != StateClosed {
		t.Errorf("Initial state = %v, want %v", state, StateClosed)
	}

	// Should allow attempts in Closed state
	if !cb.CanAttempt() {
		t.Error("CanAttempt() = false, want true in Closed state")
	}

	// Record failures below threshold - should stay Closed
	cb.RecordFailure()
	cb.RecordFailure()
	if state := cb.State(); state != StateClosed {
		t.Errorf("State after 2 failures = %v, want %v", state, StateClosed)
	}

	// Third failure should open circuit
	cb.RecordFailure()
	if state := cb.State(); state != StateOpen {
		t.Errorf("State after 3 failures = %v, want %v", state, StateOpen)
	}

	// Verify metrics
	metrics := cb.Metrics()
	if metrics.TotalOpenings != 1 {
		t.Errorf("TotalOpenings = %d, want 1", metrics.TotalOpenings)
	}
	if metrics.StateTransitions != 1 {
		t.Errorf("StateTransitions = %d, want 1", metrics.StateTransitions)
	}

	// Should not allow attempts in Open state (before timeout)
	if cb.CanAttempt() {
		t.Error("CanAttempt() = true, want false in Open state before timeout")
	}
}

func TestCircuitBreakerOpenToHalfOpen(t *testing.T) {
	clock := &mockClock{current: time.Now()}
	config := &CircuitBreakerConfig{
		Enabled:             true,
		FailureThreshold:    2,
		OpenTimeout:         10 * time.Second,
		HalfOpenMaxRequests: 2,
	}

	cb, err := NewCircuitBreaker(config)
	if err != nil {
		t.Fatalf("Failed to create circuit breaker: %v", err)
	}
	cb.now = clock.now

	// Open the circuit
	cb.RecordFailure()
	cb.RecordFailure()
	if state := cb.State(); state != StateOpen {
		t.Errorf("State = %v, want %v", state, StateOpen)
	}

	// Should not allow attempts before timeout
	if cb.CanAttempt() {
		t.Error("CanAttempt() = true, want false before timeout")
	}

	// Advance time past timeout
	clock.advance(15 * time.Second)

	// CanAttempt should transition to HalfOpen and return true
	if !cb.CanAttempt() {
		t.Error("CanAttempt() = false, want true after timeout")
	}
	if state := cb.State(); state != StateHalfOpen {
		t.Errorf("State after timeout = %v, want %v", state, StateHalfOpen)
	}

	// Verify metrics
	metrics := cb.Metrics()
	if metrics.StateTransitions != 2 { // Closed->Open, Open->HalfOpen
		t.Errorf("StateTransitions = %d, want 2", metrics.StateTransitions)
	}
}

func TestCircuitBreakerHalfOpenToClosed(t *testing.T) {
	clock := &mockClock{current: time.Now()}
	config := &CircuitBreakerConfig{
		Enabled:             true,
		FailureThreshold:    2,
		OpenTimeout:         10 * time.Second,
		HalfOpenMaxRequests: 3,
	}

	cb, err := NewCircuitBreaker(config)
	if err != nil {
		t.Fatalf("Failed to create circuit breaker: %v", err)
	}
	cb.now = clock.now

	// Open the circuit
	cb.RecordFailure()
	cb.RecordFailure()

	// Transition to HalfOpen
	clock.advance(15 * time.Second)
	cb.CanAttempt()

	if state := cb.State(); state != StateHalfOpen {
		t.Errorf("State = %v, want %v", state, StateHalfOpen)
	}

	// Record successes below threshold
	cb.RecordSuccess()
	cb.RecordSuccess()
	if state := cb.State(); state != StateHalfOpen {
		t.Errorf("State after 2 successes = %v, want %v", state, StateHalfOpen)
	}

	// Third success should close circuit
	cb.RecordSuccess()
	if state := cb.State(); state != StateClosed {
		t.Errorf("State after 3 successes = %v, want %v", state, StateClosed)
	}

	// Verify metrics
	metrics := cb.Metrics()
	if metrics.TotalRecoveries != 1 {
		t.Errorf("TotalRecoveries = %d, want 1", metrics.TotalRecoveries)
	}
	// Should have reset backoff multiplier
	if metrics.CurrentBackoffMultiplier != 0 {
		t.Errorf("CurrentBackoffMultiplier = %d, want 0 after recovery", metrics.CurrentBackoffMultiplier)
	}
}

func TestCircuitBreakerHalfOpenToOpen(t *testing.T) {
	clock := &mockClock{current: time.Now()}
	config := &CircuitBreakerConfig{
		Enabled:             true,
		FailureThreshold:    2,
		OpenTimeout:         10 * time.Second,
		HalfOpenMaxRequests: 3,
	}

	cb, err := NewCircuitBreaker(config)
	if err != nil {
		t.Fatalf("Failed to create circuit breaker: %v", err)
	}
	cb.now = clock.now

	// Open the circuit
	cb.RecordFailure()
	cb.RecordFailure()
	initialOpenings := cb.Metrics().TotalOpenings

	// Transition to HalfOpen
	clock.advance(15 * time.Second)
	cb.CanAttempt()

	if state := cb.State(); state != StateHalfOpen {
		t.Errorf("State = %v, want %v", state, StateHalfOpen)
	}

	// Any failure in HalfOpen should reopen circuit
	cb.RecordFailure()
	if state := cb.State(); state != StateOpen {
		t.Errorf("State after failure in HalfOpen = %v, want %v", state, StateOpen)
	}

	// Verify metrics
	metrics := cb.Metrics()
	if metrics.TotalOpenings != initialOpenings+1 {
		t.Errorf("TotalOpenings = %d, want %d", metrics.TotalOpenings, initialOpenings+1)
	}

	// Should not allow attempts immediately after reopening
	if cb.CanAttempt() {
		t.Error("CanAttempt() = true, want false after reopening circuit")
	}
}

func TestCircuitBreakerExponentialBackoff(t *testing.T) {
	clock := &mockClock{current: time.Now()}
	config := &CircuitBreakerConfig{
		Enabled:               true,
		FailureThreshold:      2,
		OpenTimeout:           10 * time.Second,
		HalfOpenMaxRequests:   2,
		UseExponentialBackoff: true,
		MaxBackoffTimeout:     2 * time.Minute,
	}

	cb, err := NewCircuitBreaker(config)
	if err != nil {
		t.Fatalf("Failed to create circuit breaker: %v", err)
	}
	cb.now = clock.now

	// First opening - should use base timeout (10s)
	cb.RecordFailure()
	cb.RecordFailure()
	if state := cb.State(); state != StateOpen {
		t.Errorf("State = %v, want %v", state, StateOpen)
	}

	metrics := cb.Metrics()
	if metrics.CurrentBackoffMultiplier != 1 {
		t.Errorf("CurrentBackoffMultiplier = %d, want 1", metrics.CurrentBackoffMultiplier)
	}

	// Advance time by base timeout
	clock.advance(10 * time.Second)
	cb.CanAttempt() // Transition to HalfOpen

	// Fail recovery - should increase multiplier
	cb.RecordFailure()
	metrics = cb.Metrics()
	if metrics.CurrentBackoffMultiplier != 2 {
		t.Errorf("CurrentBackoffMultiplier after first failure = %d, want 2", metrics.CurrentBackoffMultiplier)
	}

	// Second opening - should use 20s (10s * 2^(2-1) = 10s * 2 = 20s)
	// Shouldn't allow attempt before 20s
	clock.advance(10 * time.Second)
	if cb.CanAttempt() {
		t.Error("CanAttempt() = true with exponential backoff before doubled timeout")
	}

	// After 20s from opening, should allow attempt
	clock.advance(10 * time.Second)
	if !cb.CanAttempt() {
		t.Error("CanAttempt() = false after exponential backoff timeout")
	}

	// Fail recovery again - multiplier should increase
	cb.RecordFailure()
	metrics = cb.Metrics()
	if metrics.CurrentBackoffMultiplier != 3 {
		t.Errorf("CurrentBackoffMultiplier after second failure = %d, want 3", metrics.CurrentBackoffMultiplier)
	}

	// Third opening - should use 40s (10s * 2^(3-1) = 10s * 4 = 40s)
	clock.advance(40 * time.Second)
	if !cb.CanAttempt() {
		t.Error("CanAttempt() = false after third exponential backoff timeout")
	}

	// Now succeed to close circuit
	cb.RecordSuccess()
	cb.RecordSuccess()

	// After successful recovery, multiplier should reset
	metrics = cb.Metrics()
	if metrics.CurrentBackoffMultiplier != 0 {
		t.Errorf("CurrentBackoffMultiplier after recovery = %d, want 0", metrics.CurrentBackoffMultiplier)
	}
}

func TestCircuitBreakerMaxBackoff(t *testing.T) {
	clock := &mockClock{current: time.Now()}
	config := &CircuitBreakerConfig{
		Enabled:               true,
		FailureThreshold:      1,
		OpenTimeout:           5 * time.Second,
		HalfOpenMaxRequests:   1,
		UseExponentialBackoff: true,
		MaxBackoffTimeout:     30 * time.Second,
	}

	cb, err := NewCircuitBreaker(config)
	if err != nil {
		t.Fatalf("Failed to create circuit breaker: %v", err)
	}
	cb.now = clock.now

	// Open and fail recovery multiple times to exceed max backoff
	for i := 0; i < 10; i++ {
		cb.RecordFailure()
		clock.advance(5 * time.Minute) // Advance past any backoff
		cb.CanAttempt()                // Transition to HalfOpen
	}

	// Multiplier should be high
	metrics := cb.Metrics()
	if metrics.CurrentBackoffMultiplier < 5 {
		t.Errorf("Expected high multiplier, got %d", metrics.CurrentBackoffMultiplier)
	}

	// Calculate timeout - should be capped at MaxBackoffTimeout
	timeout := cb.calculateTimeout()
	if timeout > config.MaxBackoffTimeout {
		t.Errorf("Timeout %v exceeds max %v", timeout, config.MaxBackoffTimeout)
	}
	if timeout != config.MaxBackoffTimeout {
		t.Errorf("Timeout %v should equal max %v with high multiplier", timeout, config.MaxBackoffTimeout)
	}
}

func TestCircuitBreakerReset(t *testing.T) {
	config := &CircuitBreakerConfig{
		Enabled:             true,
		FailureThreshold:    2,
		OpenTimeout:         10 * time.Second,
		HalfOpenMaxRequests: 2,
	}

	cb, err := NewCircuitBreaker(config)
	if err != nil {
		t.Fatalf("Failed to create circuit breaker: %v", err)
	}

	// Open the circuit
	cb.RecordFailure()
	cb.RecordFailure()
	if state := cb.State(); state != StateOpen {
		t.Errorf("State = %v, want %v", state, StateOpen)
	}

	// Reset
	cb.Reset()

	// Should be closed with cleared counters
	if state := cb.State(); state != StateClosed {
		t.Errorf("State after reset = %v, want %v", state, StateClosed)
	}
	if failures := cb.GetConsecutiveFailures(); failures != 0 {
		t.Errorf("ConsecutiveFailures after reset = %d, want 0", failures)
	}
	metrics := cb.Metrics()
	if metrics.CurrentBackoffMultiplier != 0 {
		t.Errorf("CurrentBackoffMultiplier after reset = %d, want 0", metrics.CurrentBackoffMultiplier)
	}
}

func TestCircuitBreakerDisabled(t *testing.T) {
	config := &CircuitBreakerConfig{
		Enabled:          false,
		FailureThreshold: 1,
		OpenTimeout:      10 * time.Second,
	}

	cb, err := NewCircuitBreaker(config)
	if err != nil {
		t.Fatalf("Failed to create circuit breaker: %v", err)
	}

	// Should always allow attempts when disabled
	if !cb.CanAttempt() {
		t.Error("CanAttempt() = false when circuit breaker is disabled")
	}

	// Record many failures
	for i := 0; i < 10; i++ {
		cb.RecordFailure()
	}

	// Should still allow attempts
	if !cb.CanAttempt() {
		t.Error("CanAttempt() = false after failures when circuit breaker is disabled")
	}

	// State should remain closed
	if state := cb.State(); state != StateClosed {
		t.Errorf("State when disabled = %v, want %v", state, StateClosed)
	}
}

func TestCircuitBreakerStateTransitions(t *testing.T) {
	clock := &mockClock{current: time.Now()}
	config := &CircuitBreakerConfig{
		Enabled:             true,
		FailureThreshold:    2,
		OpenTimeout:         10 * time.Second,
		HalfOpenMaxRequests: 2,
	}

	cb, err := NewCircuitBreaker(config)
	if err != nil {
		t.Fatalf("Failed to create circuit breaker: %v", err)
	}
	cb.now = clock.now

	// Track state transitions
	type transition struct {
		from CircuitState
		to   CircuitState
	}
	var transitions []transition

	// Helper to record transition
	recordTransition := func(from, to CircuitState) {
		transitions = append(transitions, transition{from, to})
	}

	// Closed -> Open
	currentState := cb.State()
	cb.RecordFailure()
	cb.RecordFailure()
	newState := cb.State()
	if currentState != newState {
		recordTransition(currentState, newState)
	}

	// Open -> HalfOpen
	currentState = cb.State()
	clock.advance(15 * time.Second)
	cb.CanAttempt()
	newState = cb.State()
	if currentState != newState {
		recordTransition(currentState, newState)
	}

	// HalfOpen -> Closed
	currentState = cb.State()
	cb.RecordSuccess()
	cb.RecordSuccess()
	newState = cb.State()
	if currentState != newState {
		recordTransition(currentState, newState)
	}

	// Verify transitions
	expected := []transition{
		{StateClosed, StateOpen},
		{StateOpen, StateHalfOpen},
		{StateHalfOpen, StateClosed},
	}

	if len(transitions) != len(expected) {
		t.Fatalf("Number of transitions = %d, want %d", len(transitions), len(expected))
	}

	for i, tr := range transitions {
		if tr.from != expected[i].from || tr.to != expected[i].to {
			t.Errorf("Transition %d = %v -> %v, want %v -> %v",
				i, tr.from, tr.to, expected[i].from, expected[i].to)
		}
	}

	// Verify metrics
	metrics := cb.Metrics()
	if metrics.StateTransitions != 3 {
		t.Errorf("Total state transitions = %d, want 3", metrics.StateTransitions)
	}
}

func TestCircuitStateString(t *testing.T) {
	tests := []struct {
		state CircuitState
		want  string
	}{
		{StateClosed, "Closed"},
		{StateOpen, "Open"},
		{StateHalfOpen, "HalfOpen"},
		{CircuitState(99), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.state.String(); got != tt.want {
				t.Errorf("CircuitState.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCircuitBreakerConcurrency(t *testing.T) {
	config := &CircuitBreakerConfig{
		Enabled:             true,
		FailureThreshold:    100,
		OpenTimeout:         1 * time.Second,
		HalfOpenMaxRequests: 10,
	}

	cb, err := NewCircuitBreaker(config)
	if err != nil {
		t.Fatalf("Failed to create circuit breaker: %v", err)
	}

	// Run concurrent operations
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				cb.CanAttempt()
				if j%2 == 0 {
					cb.RecordSuccess()
				} else {
					cb.RecordFailure()
				}
				cb.State()
				cb.Metrics()
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Just verify no panic occurred and we can still query state
	_ = cb.State()
	_ = cb.Metrics()
}
