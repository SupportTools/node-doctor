package remediators

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// BaseRemediator provides common functionality for remediator implementations.
// Concrete remediators should embed this struct and set the remediateFunc
// to provide their specific remediation logic.
//
// BaseRemediator handles:
//   - Cooldown tracking per unique problem
//   - Attempt counting with max attempt limits
//   - Thread-safe state management
//   - Optional logging
//   - Panic recovery
//   - Context cancellation support
//
// Example usage:
//
//	type MyRemediator struct {
//	    *remediators.BaseRemediator
//	    config MyConfig
//	}
//
//	func NewMyRemediator() (*MyRemediator, error) {
//	    base, err := remediators.NewBaseRemediator("my-remediator", remediators.CooldownMedium)
//	    if err != nil {
//	        return nil, err
//	    }
//	    mr := &MyRemediator{BaseRemediator: base}
//	    err = base.SetRemediateFunc(mr.doRemediation)
//	    return mr, err
//	}
type BaseRemediator struct {
	// Configuration (immutable after creation)
	name     string
	cooldown time.Duration

	// State tracking (protected by mutex)
	mu              sync.RWMutex
	lastAttemptTime map[string]time.Time // problemKey -> last attempt timestamp
	attemptCount    map[string]int       // problemKey -> attempt count

	// Optional components
	remediateFunc RemediateFunc
	logger        Logger

	// Configuration options (can be modified via setters)
	maxAttempts int
}

// NewBaseRemediator creates a new BaseRemediator with the specified name and cooldown period.
//
// Parameters:
//   - name: A descriptive name for the remediator (must not be empty)
//   - cooldown: The minimum time between remediation attempts for the same problem (must be > 0)
//
// Returns an error if validation fails.
func NewBaseRemediator(name string, cooldown time.Duration) (*BaseRemediator, error) {
	if name == "" {
		return nil, fmt.Errorf("remediator name cannot be empty")
	}
	if cooldown <= 0 {
		return nil, fmt.Errorf("cooldown must be greater than 0, got: %v", cooldown)
	}

	return &BaseRemediator{
		name:            name,
		cooldown:        cooldown,
		lastAttemptTime: make(map[string]time.Time),
		attemptCount:    make(map[string]int),
		maxAttempts:     DefaultMaxAttempts,
	}, nil
}

// SetRemediateFunc sets the function that performs the actual remediation logic.
// This must be called before the remediator can be used.
//
// Returns an error if fn is nil.
func (b *BaseRemediator) SetRemediateFunc(fn RemediateFunc) error {
	if fn == nil {
		return fmt.Errorf("remediateFunc cannot be nil")
	}
	b.remediateFunc = fn
	return nil
}

// SetLogger sets an optional logger for the remediator.
// If not set, logging calls will be silently ignored.
func (b *BaseRemediator) SetLogger(logger Logger) error {
	b.logger = logger
	return nil
}

// SetMaxAttempts sets the maximum number of remediation attempts per problem.
// Must be greater than 0.
//
// Returns an error if max is invalid.
func (b *BaseRemediator) SetMaxAttempts(max int) error {
	if max <= 0 {
		return fmt.Errorf("maxAttempts must be greater than 0, got: %d", max)
	}
	b.maxAttempts = max
	return nil
}

// GetName returns the remediator's name.
func (b *BaseRemediator) GetName() string {
	return b.name
}

// GetCooldown returns the cooldown period for this remediator.
// This implements the types.Remediator interface.
func (b *BaseRemediator) GetCooldown() time.Duration {
	return b.cooldown
}

// GetMaxAttempts returns the maximum number of remediation attempts per problem.
func (b *BaseRemediator) GetMaxAttempts() int {
	return b.maxAttempts
}

// CanRemediate checks if remediation is allowed for the given problem.
// It enforces both cooldown periods and max attempt limits.
//
// Returns true if:
//   - The problem is not in cooldown period
//   - The attempt count has not exceeded maxAttempts
//
// This implements the types.Remediator interface.
func (b *BaseRemediator) CanRemediate(problem types.Problem) bool {
	// Check if we're in cooldown
	if b.IsInCooldown(problem) {
		b.logWarnf("Problem %s is in cooldown, cannot remediate yet (remaining: %v)",
			GenerateProblemKey(problem), b.GetCooldownRemaining(problem))
		return false
	}

	// Check if we've exceeded max attempts
	if b.GetAttemptCount(problem) >= b.maxAttempts {
		b.logWarnf("Problem %s has exceeded max attempts (%d/%d), cannot remediate",
			GenerateProblemKey(problem), b.GetAttemptCount(problem), b.maxAttempts)
		return false
	}

	return true
}

// Remediate performs the remediation with safety checks and state tracking.
// This implements the types.Remediator interface.
//
// The method:
//   - Checks if remediateFunc is set
//   - Records the attempt (updates timestamp and count)
//   - Calls the remediateFunc with panic recovery
//   - Respects context cancellation
//   - Logs all actions
//
// Returns an error if remediation fails or if remediateFunc is not set.
func (b *BaseRemediator) Remediate(ctx context.Context, problem types.Problem) error {
	if b.remediateFunc == nil {
		return fmt.Errorf("remediateFunc not set for remediator %s", b.name)
	}

	problemKey := GenerateProblemKey(problem)
	b.logInfof("Starting remediation for problem: %s (attempt %d/%d)",
		problemKey, b.GetAttemptCount(problem)+1, b.maxAttempts)

	// Record the attempt before executing (for cooldown tracking)
	b.recordAttempt(problemKey)

	// Execute remediation with panic recovery
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic during remediation of %s: %v", problemKey, r)
				b.logErrorf("Panic recovered: %v", err)
			}
		}()

		// Check context cancellation before executing
		select {
		case <-ctx.Done():
			err = fmt.Errorf("context cancelled before remediation: %w", ctx.Err())
			return
		default:
		}

		// Execute the remediation
		err = b.remediateFunc(ctx, problem)
	}()

	if err != nil {
		b.logErrorf("Remediation failed for %s: %v", problemKey, err)
		return fmt.Errorf("remediation failed for %s: %w", problemKey, err)
	}

	b.logInfof("Remediation successful for problem: %s", problemKey)
	return nil
}

// IsInCooldown checks if the problem is currently in its cooldown period.
// Returns false if no previous attempt has been made.
func (b *BaseRemediator) IsInCooldown(problem types.Problem) bool {
	return b.GetCooldownRemaining(problem) > 0
}

// GetAttemptCount returns the current attempt count for the given problem.
// Returns 0 if no attempts have been made.
func (b *BaseRemediator) GetAttemptCount(problem types.Problem) int {
	problemKey := GenerateProblemKey(problem)

	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.attemptCount[problemKey]
}

// GetCooldownRemaining returns the time remaining in the cooldown period.
// Returns 0 if not in cooldown or no previous attempt has been made.
func (b *BaseRemediator) GetCooldownRemaining(problem types.Problem) time.Duration {
	problemKey := GenerateProblemKey(problem)

	b.mu.RLock()
	lastAttempt, exists := b.lastAttemptTime[problemKey]
	b.mu.RUnlock()

	if !exists {
		return 0
	}

	elapsed := time.Since(lastAttempt)
	if elapsed >= b.cooldown {
		return 0
	}

	return b.cooldown - elapsed
}

// ResetAttempts resets the attempt counter for the given problem to zero.
// This does not affect the cooldown timer.
//
// This method is primarily useful for testing or manual intervention.
func (b *BaseRemediator) ResetAttempts(problem types.Problem) {
	problemKey := GenerateProblemKey(problem)

	b.mu.Lock()
	defer b.mu.Unlock()

	delete(b.attemptCount, problemKey)
	b.logInfof("Reset attempt counter for problem: %s", problemKey)
}

// ClearCooldown clears the cooldown timer for the given problem.
// This allows immediate remediation regardless of when the last attempt was made.
//
// This method is primarily useful for testing or manual intervention.
func (b *BaseRemediator) ClearCooldown(problem types.Problem) {
	problemKey := GenerateProblemKey(problem)

	b.mu.Lock()
	defer b.mu.Unlock()

	delete(b.lastAttemptTime, problemKey)
	b.logInfof("Cleared cooldown for problem: %s", problemKey)
}

// recordAttempt updates both the timestamp and attempt count for a problem.
// This must be called with appropriate locking from the Remediate method.
func (b *BaseRemediator) recordAttempt(problemKey string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.lastAttemptTime[problemKey] = time.Now()
	b.attemptCount[problemKey]++
}

// logInfof logs an informational message if a logger is configured.
func (b *BaseRemediator) logInfof(format string, args ...interface{}) {
	if b.logger != nil {
		b.logger.Infof("[%s] "+format, append([]interface{}{b.name}, args...)...)
	}
}

// logWarnf logs a warning message if a logger is configured.
func (b *BaseRemediator) logWarnf(format string, args ...interface{}) {
	if b.logger != nil {
		b.logger.Warnf("[%s] "+format, append([]interface{}{b.name}, args...)...)
	}
}

// logErrorf logs an error message if a logger is configured.
func (b *BaseRemediator) logErrorf(format string, args ...interface{}) {
	if b.logger != nil {
		b.logger.Errorf("[%s] "+format, append([]interface{}{b.name}, args...)...)
	}
}
