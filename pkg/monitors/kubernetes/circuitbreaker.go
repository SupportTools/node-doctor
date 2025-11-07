// Package kubernetes provides Kubernetes component health monitoring capabilities.
package kubernetes

import (
	"fmt"
	"sync"
	"time"
)

// CircuitState represents the state of the circuit breaker.
type CircuitState int

const (
	// StateClosed - Circuit is closed, requests pass through normally
	StateClosed CircuitState = iota

	// StateOpen - Circuit is open, requests fail fast without attempting
	StateOpen

	// StateHalfOpen - Circuit is testing recovery, limited requests allowed
	StateHalfOpen
)

// String returns the string representation of the circuit state.
func (s CircuitState) String() string {
	switch s {
	case StateClosed:
		return "Closed"
	case StateOpen:
		return "Open"
	case StateHalfOpen:
		return "HalfOpen"
	default:
		return "Unknown"
	}
}

// CircuitBreakerConfig holds configuration for the circuit breaker.
type CircuitBreakerConfig struct {
	// Enabled determines if circuit breaker is active
	Enabled bool `json:"enabled"`

	// FailureThreshold is the number of consecutive failures before opening circuit
	// Must be >= 1. Default: 5
	FailureThreshold int `json:"failureThreshold"`

	// OpenTimeout is the duration to wait before attempting recovery (transition to HalfOpen)
	// Must be > 0. Default: 30s
	OpenTimeout time.Duration `json:"openTimeout"`

	// HalfOpenMaxRequests is the number of successful requests needed in HalfOpen state
	// before closing the circuit. Must be >= 1. Default: 3
	HalfOpenMaxRequests int `json:"halfOpenMaxRequests"`

	// UseExponentialBackoff enables exponential backoff for repeated circuit openings
	// Default: true
	UseExponentialBackoff bool `json:"useExponentialBackoff"`

	// MaxBackoffTimeout is the maximum timeout when using exponential backoff
	// Must be >= OpenTimeout. Default: 5m
	MaxBackoffTimeout time.Duration `json:"maxBackoffTimeout"`
}

// applyDefaults applies default values to circuit breaker configuration.
func (c *CircuitBreakerConfig) applyDefaults() {
	if c.FailureThreshold == 0 {
		c.FailureThreshold = 5
	}
	if c.OpenTimeout == 0 {
		c.OpenTimeout = 30 * time.Second
	}
	if c.HalfOpenMaxRequests == 0 {
		c.HalfOpenMaxRequests = 3
	}
	// UseExponentialBackoff defaults to false (zero value) unless explicitly set
	if c.MaxBackoffTimeout == 0 {
		c.MaxBackoffTimeout = 5 * time.Minute
	}
}

// validate validates the circuit breaker configuration.
func (c *CircuitBreakerConfig) validate() error {
	if c.FailureThreshold < 1 {
		return fmt.Errorf("circuit breaker failureThreshold must be at least 1, got %d", c.FailureThreshold)
	}
	if c.OpenTimeout <= 0 {
		return fmt.Errorf("circuit breaker openTimeout must be positive, got %v", c.OpenTimeout)
	}
	if c.HalfOpenMaxRequests < 1 {
		return fmt.Errorf("circuit breaker halfOpenMaxRequests must be at least 1, got %d", c.HalfOpenMaxRequests)
	}
	if c.MaxBackoffTimeout < c.OpenTimeout {
		return fmt.Errorf("circuit breaker maxBackoffTimeout (%v) must be >= openTimeout (%v)", c.MaxBackoffTimeout, c.OpenTimeout)
	}
	return nil
}

// CircuitBreakerMetrics holds metrics for the circuit breaker.
type CircuitBreakerMetrics struct {
	// StateTransitions tracks the number of state transitions
	StateTransitions int64 `json:"stateTransitions"`

	// TotalOpenings tracks how many times circuit has opened
	TotalOpenings int64 `json:"totalOpenings"`

	// TotalRecoveries tracks how many times circuit has recovered (HalfOpen -> Closed)
	TotalRecoveries int64 `json:"totalRecoveries"`

	// LastStateChange is the timestamp of the last state change
	LastStateChange time.Time `json:"lastStateChange"`

	// CurrentBackoffMultiplier is the current backoff multiplier for exponential backoff
	CurrentBackoffMultiplier int `json:"currentBackoffMultiplier"`
}

// CircuitBreaker implements the circuit breaker pattern for protecting against cascading failures.
//
// States:
//   - Closed: Normal operation, requests pass through. Failures are counted.
//   - Open: Circuit is open, requests fail fast without attempting the operation. After timeout, transitions to HalfOpen.
//   - HalfOpen: Testing recovery. Limited requests are allowed. Successful requests lead to Closed, failures lead back to Open.
//
// This prevents resource exhaustion from repeatedly attempting operations that are likely to fail.
type CircuitBreaker struct {
	config  *CircuitBreakerConfig
	mu      sync.RWMutex
	state   CircuitState
	metrics CircuitBreakerMetrics

	// Failure tracking
	consecutiveFailures  int
	consecutiveSuccesses int

	// Timing for state transitions
	openedAt time.Time

	// For testing - allows injection of time function
	now func() time.Time
}

// NewCircuitBreaker creates a new circuit breaker with the given configuration.
func NewCircuitBreaker(config *CircuitBreakerConfig) (*CircuitBreaker, error) {
	if config == nil {
		return nil, fmt.Errorf("circuit breaker config cannot be nil")
	}

	// Apply defaults
	config.applyDefaults()

	// Validate configuration
	if err := config.validate(); err != nil {
		return nil, err
	}

	return &CircuitBreaker{
		config: config,
		state:  StateClosed,
		now:    time.Now,
	}, nil
}

// State returns the current state of the circuit breaker.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// Metrics returns a copy of the current metrics.
func (cb *CircuitBreaker) Metrics() CircuitBreakerMetrics {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.metrics
}

// CanAttempt checks if a request should be attempted based on circuit state.
// Returns true if the request should proceed, false if it should fail fast.
func (cb *CircuitBreaker) CanAttempt() bool {
	if !cb.config.Enabled {
		return true
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		// Normal operation - allow request
		return true

	case StateOpen:
		// Check if it's time to attempt recovery
		timeout := cb.calculateTimeout()
		if cb.now().Sub(cb.openedAt) >= timeout {
			// Transition to HalfOpen
			cb.transitionTo(StateHalfOpen)
			return true
		}
		// Circuit is still open - fail fast
		return false

	case StateHalfOpen:
		// Allow limited requests in half-open state
		return true

	default:
		// Unknown state - fail safe by allowing request
		return true
	}
}

// RecordSuccess records a successful operation.
func (cb *CircuitBreaker) RecordSuccess() {
	if !cb.config.Enabled {
		return
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		// Reset failure counter on success
		cb.consecutiveFailures = 0

	case StateHalfOpen:
		// Count successes in half-open state
		cb.consecutiveSuccesses++
		cb.consecutiveFailures = 0

		// If enough successful requests, close the circuit
		if cb.consecutiveSuccesses >= cb.config.HalfOpenMaxRequests {
			cb.transitionTo(StateClosed)
			cb.consecutiveSuccesses = 0
			// Reset backoff multiplier on successful recovery
			cb.metrics.CurrentBackoffMultiplier = 0
			cb.metrics.TotalRecoveries++
		}

	case StateOpen:
		// Shouldn't happen, but reset failures just in case
		cb.consecutiveFailures = 0
	}
}

// RecordFailure records a failed operation.
func (cb *CircuitBreaker) RecordFailure() {
	if !cb.config.Enabled {
		return
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		cb.consecutiveFailures++
		cb.consecutiveSuccesses = 0

		// Check if failures exceed threshold
		if cb.consecutiveFailures >= cb.config.FailureThreshold {
			cb.transitionTo(StateOpen)
			cb.openedAt = cb.now()
			cb.consecutiveFailures = 0
			cb.metrics.TotalOpenings++

			// Increment backoff multiplier if using exponential backoff
			if cb.config.UseExponentialBackoff {
				cb.metrics.CurrentBackoffMultiplier++
			}
		}

	case StateHalfOpen:
		// Any failure in half-open state reopens the circuit
		cb.transitionTo(StateOpen)
		cb.openedAt = cb.now()
		cb.consecutiveSuccesses = 0
		cb.consecutiveFailures = 0
		cb.metrics.TotalOpenings++

		// Increment backoff multiplier on failed recovery attempt
		if cb.config.UseExponentialBackoff {
			cb.metrics.CurrentBackoffMultiplier++
		}

	case StateOpen:
		// Already open, nothing to do
	}
}

// Reset resets the circuit breaker to closed state with cleared counters.
// This should only be used for testing or administrative reset.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = StateClosed
	cb.consecutiveFailures = 0
	cb.consecutiveSuccesses = 0
	cb.metrics.CurrentBackoffMultiplier = 0
}

// transitionTo transitions the circuit breaker to a new state.
// Caller must hold the lock.
func (cb *CircuitBreaker) transitionTo(newState CircuitState) {
	if cb.state != newState {
		cb.state = newState
		cb.metrics.StateTransitions++
		cb.metrics.LastStateChange = cb.now()
	}
}

// calculateTimeout calculates the timeout duration based on exponential backoff.
// Caller must hold the lock.
func (cb *CircuitBreaker) calculateTimeout() time.Duration {
	if !cb.config.UseExponentialBackoff || cb.metrics.CurrentBackoffMultiplier == 0 {
		return cb.config.OpenTimeout
	}

	// Calculate exponential backoff: OpenTimeout * 2^(multiplier-1)
	// multiplier-1 because first opening should use base timeout
	multiplier := 1 << (cb.metrics.CurrentBackoffMultiplier - 1)
	timeout := cb.config.OpenTimeout * time.Duration(multiplier)

	// Cap at maximum backoff timeout
	if timeout > cb.config.MaxBackoffTimeout {
		timeout = cb.config.MaxBackoffTimeout
	}

	return timeout
}

// GetConsecutiveFailures returns the current count of consecutive failures.
// Primarily for testing and metrics.
func (cb *CircuitBreaker) GetConsecutiveFailures() int {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.consecutiveFailures
}
