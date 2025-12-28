// Package remediators provides a pluggable remediator registry system for Node Doctor.
//
// The registry manages remediator instances with global safety mechanisms including:
//   - Circuit breaker pattern to prevent cascading failures
//   - Rate limiting to prevent remediation storms
//   - Remediation history for audit and analysis
//   - Dry-run mode for testing
//
// These global safety mechanisms work in conjunction with BaseRemediator's per-problem
// safety features (cooldown and max attempts) to provide defense in depth.
//
// Usage Example:
//
//	// Create registry
//	registry := remediators.NewRegistry()
//
//	// Register remediators
//	registry.Register(remediators.RemediatorInfo{
//		Type:        "kubelet-restart",
//		Factory:     NewKubeletRemediator,
//		Description: "Restarts kubelet service",
//	})
//
//	// Execute remediation (with automatic safety checks)
//	err := registry.Remediate(ctx, "kubelet-restart", problem)
package remediators

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/supporttools/node-doctor/pkg/types"
)

// CircuitBreakerState represents the state of the circuit breaker.
type CircuitBreakerState int

const (
	// CircuitClosed means normal operation - remediations are allowed
	CircuitClosed CircuitBreakerState = iota

	// CircuitOpen means too many failures occurred - remediations are blocked
	CircuitOpen

	// CircuitHalfOpen means testing if the system has recovered - limited remediations allowed
	CircuitHalfOpen
)

// String returns the string representation of the circuit breaker state.
func (s CircuitBreakerState) String() string {
	switch s {
	case CircuitClosed:
		return "Closed"
	case CircuitOpen:
		return "Open"
	case CircuitHalfOpen:
		return "HalfOpen"
	default:
		return "Unknown"
	}
}

// RemediatorFactory is a function that creates a new remediator instance.
// It returns a remediator that implements the types.Remediator interface.
type RemediatorFactory func() (types.Remediator, error)

// RemediatorValidator is a function that validates a remediator instance.
// This is optional but recommended for early validation.
type RemediatorValidator func(remediator types.Remediator) error

// EventCreator is an interface for creating Kubernetes events.
// This allows the registry to persist remediation attempts as Kubernetes events
// without creating a circular dependency with the kubernetes exporter package.
type EventCreator interface {
	// CreateEvent creates a Kubernetes event
	CreateEvent(ctx context.Context, event corev1.Event) error
}

// RemediatorInfo contains metadata and factory functions for a remediator type.
// This is used to register remediator implementations with the registry.
type RemediatorInfo struct {
	// Type is the unique identifier for this remediator type.
	Type string

	// Factory is the function used to create new instances of this remediator.
	// The factory function should be thread-safe and stateless.
	Factory RemediatorFactory

	// Validator is the function used to validate remediator instances.
	// This is optional but recommended for early validation.
	Validator RemediatorValidator

	// Description provides human-readable documentation for this remediator type.
	Description string
}

// RemediationRecord represents a single remediation attempt in the history.
type RemediationRecord struct {
	// RemediatorType is the type of remediator used
	RemediatorType string

	// Problem is the problem that was remediated
	Problem types.Problem

	// StartTime is when the remediation started
	StartTime time.Time

	// EndTime is when the remediation completed
	EndTime time.Time

	// Duration is how long the remediation took
	Duration time.Duration

	// Success indicates whether the remediation succeeded
	Success bool

	// Error contains the error message if the remediation failed
	Error string
}

// CircuitBreakerConfig contains configuration for the circuit breaker.
type CircuitBreakerConfig struct {
	// Threshold is the number of consecutive failures before opening the circuit
	Threshold int

	// Timeout is how long to keep the circuit open before trying again
	Timeout time.Duration

	// SuccessThreshold is the number of consecutive successes in half-open state
	// before closing the circuit
	SuccessThreshold int
}

// RemediatorRegistry manages the registration and execution of remediators.
// It provides:
//   - Factory pattern for creating remediators
//   - Circuit breaker to prevent cascading failures
//   - Rate limiting to prevent remediation storms
//   - Remediation history for audit and analysis
//   - Dry-run mode for testing
//   - Controller coordination via lease system
//
// The registry uses a read-write mutex to optimize for concurrent remediation
// operations while protecting internal state.
type RemediatorRegistry struct {
	// mu protects all internal state
	mu sync.RWMutex

	// remediators maps remediator type names to their registration info
	remediators map[string]*RemediatorInfo

	// instances caches created remediator instances (type -> instance)
	instances map[string]types.Remediator

	// Circuit breaker state
	circuitState           CircuitBreakerState
	circuitConfig          CircuitBreakerConfig
	consecutiveFailures    int
	consecutiveSuccesses   int
	circuitOpenedAt        time.Time
	circuitLastStateChange time.Time

	// Rate limiting (sliding window)
	remediationTimes []time.Time // timestamps of recent remediations
	maxPerHour       int         // max remediations per hour
	rateLimitWindow  time.Duration

	// History tracking
	history    []RemediationRecord
	maxHistory int

	// Dry-run mode
	dryRun bool

	// Optional logger
	logger Logger

	// Kubernetes Events integration (optional)
	eventCreator EventCreator
	nodeName     string

	// Controller coordination (optional)
	leaseClient *LeaseClient
}

// DefaultCircuitBreakerConfig provides sensible defaults for the circuit breaker.
var DefaultCircuitBreakerConfig = CircuitBreakerConfig{
	Threshold:        5,               // Open after 5 consecutive failures
	Timeout:          5 * time.Minute, // Stay open for 5 minutes
	SuccessThreshold: 2,               // Close after 2 consecutive successes
}

// NewRegistry creates a new remediator registry with default configuration.
//
// Parameters:
//   - maxPerHour: Maximum number of remediations allowed per hour (0 = unlimited)
//   - maxHistory: Maximum number of remediation records to keep (0 = unlimited, capped at 10000)
//
// Returns a configured registry ready for use.
func NewRegistry(maxPerHour, maxHistory int) *RemediatorRegistry {
	if maxHistory < 0 {
		maxHistory = 0
	}
	if maxHistory > 10000 {
		maxHistory = 10000 // Reasonable upper bound to prevent memory issues
	}
	if maxPerHour < 0 {
		maxPerHour = 0
	}

	return &RemediatorRegistry{
		remediators:      make(map[string]*RemediatorInfo),
		instances:        make(map[string]types.Remediator),
		circuitState:     CircuitClosed,
		circuitConfig:    DefaultCircuitBreakerConfig,
		remediationTimes: make([]time.Time, 0),
		maxPerHour:       maxPerHour,
		rateLimitWindow:  1 * time.Hour,
		history:          make([]RemediationRecord, 0),
		maxHistory:       maxHistory,
		dryRun:           false,
	}
}

// Register adds a new remediator type to the registry.
// This function is typically called during initialization to register
// available remediators.
//
// Register panics if:
//   - info.Type is empty
//   - info.Factory is nil
//   - A remediator with the same type is already registered
//
// Panicking is appropriate here because registration happens at init time,
// and registration conflicts indicate programming errors that should be
// caught during development.
func (r *RemediatorRegistry) Register(info RemediatorInfo) {
	if info.Type == "" {
		panic("remediator type cannot be empty")
	}
	if info.Factory == nil {
		panic(fmt.Sprintf("remediator factory cannot be nil for type %q", info.Type))
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.remediators[info.Type]; exists {
		panic(fmt.Sprintf("remediator type %q is already registered", info.Type))
	}

	// Create a copy to avoid potential issues with pointer sharing
	infoCopy := info
	r.remediators[info.Type] = &infoCopy

	r.logInfof("Registered remediator type: %s", info.Type)
}

// SetLogger sets an optional logger for the registry.
func (r *RemediatorRegistry) SetLogger(logger Logger) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logger = logger
}

// SetEventCreator sets an optional event creator for persisting remediation attempts
// as Kubernetes events. The nodeName parameter specifies the node these events should
// be associated with.
func (r *RemediatorRegistry) SetEventCreator(eventCreator EventCreator, nodeName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.eventCreator = eventCreator
	r.nodeName = nodeName
}

// SetLeaseClient sets an optional lease client for controller coordination.
// When set, the registry will request a lease from the controller before
// executing any remediation.
func (r *RemediatorRegistry) SetLeaseClient(leaseClient *LeaseClient) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.leaseClient = leaseClient
	r.logInfof("Lease client configured for controller coordination")
}

// GetLeaseClient returns the configured lease client, if any.
func (r *RemediatorRegistry) GetLeaseClient() *LeaseClient {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.leaseClient
}

// SetDryRun enables or disables dry-run mode.
// In dry-run mode, remediations are not actually executed but all other
// logic (rate limiting, circuit breaker, history) still runs.
func (r *RemediatorRegistry) SetDryRun(dryRun bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dryRun = dryRun
	r.logInfof("Dry-run mode set to: %v", dryRun)
}

// IsDryRun returns whether the registry is in dry-run mode.
func (r *RemediatorRegistry) IsDryRun() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.dryRun
}

// SetCircuitBreakerConfig updates the circuit breaker configuration.
// This can be called at runtime to adjust circuit breaker behavior.
func (r *RemediatorRegistry) SetCircuitBreakerConfig(config CircuitBreakerConfig) error {
	if config.Threshold <= 0 {
		return fmt.Errorf("circuit breaker threshold must be positive, got %d", config.Threshold)
	}
	if config.Timeout <= 0 {
		return fmt.Errorf("circuit breaker timeout must be positive, got %v", config.Timeout)
	}
	if config.SuccessThreshold <= 0 {
		return fmt.Errorf("circuit breaker success threshold must be positive, got %d", config.SuccessThreshold)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.circuitConfig = config
	r.logInfof("Circuit breaker config updated: %+v", config)
	return nil
}

// GetRegisteredTypes returns a sorted list of all registered remediator types.
func (r *RemediatorRegistry) GetRegisteredTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.remediators))
	for remediatorType := range r.remediators {
		types = append(types, remediatorType)
	}

	sort.Strings(types)
	return types
}

// IsRegistered checks whether a remediator type is registered.
func (r *RemediatorRegistry) IsRegistered(remediatorType string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, exists := r.remediators[remediatorType]
	return exists
}

// GetRemediatorInfo returns the registration information for a remediator type.
// Returns nil if the remediator type is not registered.
func (r *RemediatorRegistry) GetRemediatorInfo(remediatorType string) *RemediatorInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	info, exists := r.remediators[remediatorType]
	if !exists {
		return nil
	}

	// Return a copy to prevent external modification
	infoCopy := *info
	return &infoCopy
}

// getOrCreateRemediator gets a cached remediator instance or creates a new one.
// This method is thread-safe and uses double-checked locking for efficiency.
func (r *RemediatorRegistry) getOrCreateRemediator(remediatorType string) (types.Remediator, error) {
	// Fast path: check with read lock first (most common case)
	r.mu.RLock()
	if instance, exists := r.instances[remediatorType]; exists {
		r.mu.RUnlock()
		return instance, nil
	}
	r.mu.RUnlock()

	// Slow path: need to create instance with write lock
	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring write lock (another goroutine may have created it)
	if instance, exists := r.instances[remediatorType]; exists {
		return instance, nil
	}

	// Get the remediator info
	info, exists := r.remediators[remediatorType]
	if !exists {
		return nil, fmt.Errorf("unknown remediator type %q, available types: %v",
			remediatorType, r.getRegisteredTypesUnsafe())
	}

	// Create new instance with panic recovery
	var remediator types.Remediator
	var err error
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				err = fmt.Errorf("remediator factory %q panicked: %v", remediatorType, rec)
			}
		}()
		remediator, err = info.Factory()
	}()

	if err != nil {
		return nil, fmt.Errorf("failed to create remediator %q: %w", remediatorType, err)
	}

	// Validate if validator is provided
	if info.Validator != nil {
		if err := info.Validator(remediator); err != nil {
			return nil, fmt.Errorf("validation failed for remediator %q: %w", remediatorType, err)
		}
	}

	// Cache the instance
	r.instances[remediatorType] = remediator
	return remediator, nil
}

// getRegisteredTypesUnsafe returns registered types without locking.
// This is used internally when we already hold the lock.
func (r *RemediatorRegistry) getRegisteredTypesUnsafe() []string {
	types := make([]string, 0, len(r.remediators))
	for remediatorType := range r.remediators {
		types = append(types, remediatorType)
	}
	sort.Strings(types)
	return types
}

// Remediate performs remediation using the specified remediator type.
// This is the main entry point for executing remediations with full safety checks:
//
//  1. Circuit breaker check (fail fast if circuit is open)
//  2. Rate limit check (prevent remediation storms)
//  3. Get/create remediator instance
//  4. Check remediator-specific CanRemediate (cooldown, max attempts)
//  5. Execute remediation (or skip in dry-run mode)
//  6. Record in history
//  7. Update circuit breaker state
//  8. Clean up old rate limit entries
//
// Returns an error if any safety check fails or if remediation fails.
func (r *RemediatorRegistry) Remediate(ctx context.Context, remediatorType string, problem types.Problem) error {
	startTime := time.Now()
	var success bool
	var remediationErr error

	// Defer recording this attempt in history
	defer func() {
		r.recordRemediation(RemediationRecord{
			RemediatorType: remediatorType,
			Problem:        problem,
			StartTime:      startTime,
			EndTime:        time.Now(),
			Duration:       time.Since(startTime),
			Success:        success,
			Error:          formatError(remediationErr),
		})
	}()

	// Phase 1: Circuit breaker check
	r.mu.Lock()
	if err := r.checkCircuitBreaker(); err != nil {
		r.mu.Unlock()
		remediationErr = err
		return err
	}
	r.mu.Unlock()

	// Phase 2: Rate limit check
	r.mu.Lock()
	if err := r.checkRateLimit(); err != nil {
		r.mu.Unlock()
		remediationErr = err
		return err
	}
	r.mu.Unlock()

	// Phase 2.5: Controller lease check (if coordination enabled)
	var leaseID string
	r.mu.RLock()
	leaseClient := r.leaseClient
	r.mu.RUnlock()

	if leaseClient != nil {
		leaseResp, err := leaseClient.RequestLease(ctx, remediatorType, problem.Message)
		if err != nil {
			remediationErr = fmt.Errorf("failed to request remediation lease: %w", err)
			r.recordCircuitBreakerFailure()
			return remediationErr
		}

		if !leaseResp.Approved {
			remediationErr = fmt.Errorf("remediation lease denied: %s", leaseResp.Message)
			r.logInfof("Remediation blocked by controller: %s", leaseResp.Message)
			return remediationErr
		}

		leaseID = leaseResp.LeaseID
		r.logInfof("Remediation lease granted: id=%s expires=%v", leaseID, leaseResp.ExpiresAt)

		// Ensure lease is released when we're done
		defer func() {
			if leaseID != "" {
				if err := leaseClient.ReleaseLease(context.Background(), leaseID); err != nil {
					r.logInfof("Warning: failed to release lease %s: %v", leaseID, err)
				}
			}
		}()
	}

	// Phase 3: Get or create remediator instance (handles its own locking)
	remediator, err := r.getOrCreateRemediator(remediatorType)
	if err != nil {
		remediationErr = err
		r.recordCircuitBreakerFailure()
		return err
	}

	// Phase 4: Check remediator-specific CanRemediate (cooldown, max attempts)
	if !remediator.CanRemediate(problem) {
		remediationErr = fmt.Errorf("remediator %q cannot remediate problem %s",
			remediatorType, GenerateProblemKey(problem))
		r.recordCircuitBreakerFailure()
		return remediationErr
	}

	r.logInfof("Executing remediation: type=%s problem=%s dry-run=%v",
		remediatorType, GenerateProblemKey(problem), r.dryRun)

	// Phase 5: Execute remediation (or skip in dry-run mode)
	if r.dryRun {
		r.logInfof("[DRY-RUN] Would execute remediation for %s", GenerateProblemKey(problem))
		success = true
		r.recordCircuitBreakerSuccess()
		r.recordRateLimitEntry()
		return nil
	}

	// Execute with panic recovery
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				remediationErr = fmt.Errorf("panic during remediation: %v", rec)
			}
		}()
		remediationErr = remediator.Remediate(ctx, problem)
	}()

	// Phase 6-8: Update state based on result
	if remediationErr != nil {
		r.logErrorf("Remediation failed: type=%s problem=%s error=%v",
			remediatorType, GenerateProblemKey(problem), remediationErr)
		r.recordCircuitBreakerFailure()
		return fmt.Errorf("remediation failed: %w", remediationErr)
	}

	success = true
	r.logInfof("Remediation successful: type=%s problem=%s",
		remediatorType, GenerateProblemKey(problem))
	r.recordCircuitBreakerSuccess()
	r.recordRateLimitEntry()
	return nil
}

// checkCircuitBreaker checks if the circuit breaker allows remediation.
// This must be called with the lock held.
func (r *RemediatorRegistry) checkCircuitBreaker() error {
	switch r.circuitState {
	case CircuitClosed:
		// Normal operation
		return nil

	case CircuitOpen:
		// Check if timeout has elapsed to try half-open
		if time.Since(r.circuitOpenedAt) >= r.circuitConfig.Timeout {
			r.circuitState = CircuitHalfOpen
			r.circuitLastStateChange = time.Now()
			r.consecutiveSuccesses = 0
			r.logInfof("Circuit breaker transitioning to HalfOpen (timeout elapsed)")
			return nil
		}
		return fmt.Errorf("circuit breaker is Open (opened %v ago, timeout: %v)",
			time.Since(r.circuitOpenedAt), r.circuitConfig.Timeout)

	case CircuitHalfOpen:
		// Allow one remediation to test if system has recovered
		return nil

	default:
		return fmt.Errorf("unknown circuit breaker state: %v", r.circuitState)
	}
}

// checkRateLimit checks if rate limiting allows remediation.
// This must be called with the lock held.
func (r *RemediatorRegistry) checkRateLimit() error {
	if r.maxPerHour == 0 {
		return nil // Rate limiting disabled
	}

	// Clean up old entries (outside the window)
	now := time.Now()
	cutoff := now.Add(-r.rateLimitWindow)
	validEntries := 0
	for i := len(r.remediationTimes) - 1; i >= 0; i-- {
		if r.remediationTimes[i].After(cutoff) {
			validEntries++
		} else {
			break // Entries are sorted, so we can stop here
		}
	}

	// Keep only valid entries
	if validEntries < len(r.remediationTimes) {
		r.remediationTimes = r.remediationTimes[len(r.remediationTimes)-validEntries:]
	}

	// Check if we're at the limit
	if len(r.remediationTimes) >= r.maxPerHour {
		return fmt.Errorf("rate limit exceeded: %d remediations in the last hour (max: %d)",
			len(r.remediationTimes), r.maxPerHour)
	}

	return nil
}

// recordRateLimitEntry records a successful remediation for rate limiting.
func (r *RemediatorRegistry) recordRateLimitEntry() {
	if r.maxPerHour == 0 {
		return // Rate limiting disabled
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.remediationTimes = append(r.remediationTimes, time.Now())
}

// recordCircuitBreakerSuccess records a successful remediation for the circuit breaker.
func (r *RemediatorRegistry) recordCircuitBreakerSuccess() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.consecutiveFailures = 0
	r.consecutiveSuccesses++

	// If we're in HalfOpen and have enough successes, close the circuit
	if r.circuitState == CircuitHalfOpen {
		if r.consecutiveSuccesses >= r.circuitConfig.SuccessThreshold {
			r.circuitState = CircuitClosed
			r.circuitLastStateChange = time.Now()
			r.consecutiveSuccesses = 0
			r.logInfof("Circuit breaker transitioning to Closed (success threshold reached)")
		}
	}
}

// recordCircuitBreakerFailure records a failed remediation for the circuit breaker.
func (r *RemediatorRegistry) recordCircuitBreakerFailure() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.consecutiveSuccesses = 0
	r.consecutiveFailures++

	// If we're in HalfOpen and got a failure, open the circuit again
	if r.circuitState == CircuitHalfOpen {
		r.circuitState = CircuitOpen
		r.circuitOpenedAt = time.Now()
		r.circuitLastStateChange = time.Now()
		r.consecutiveFailures = 1 // Reset counter
		r.logWarnf("Circuit breaker transitioning to Open (failure in HalfOpen state)")
		return
	}

	// If we're in Closed and hit the threshold, open the circuit
	if r.circuitState == CircuitClosed {
		if r.consecutiveFailures >= r.circuitConfig.Threshold {
			r.circuitState = CircuitOpen
			r.circuitOpenedAt = time.Now()
			r.circuitLastStateChange = time.Now()
			r.logWarnf("Circuit breaker transitioning to Open (failure threshold %d reached)",
				r.circuitConfig.Threshold)
		}
	}
}

// recordRemediation adds a remediation record to the history.
func (r *RemediatorRegistry) recordRemediation(record RemediationRecord) {
	r.mu.Lock()

	// Record to history if enabled
	if r.maxHistory > 0 {
		r.history = append(r.history, record)

		// Trim history if it exceeds max size
		if len(r.history) > r.maxHistory {
			// Keep only the most recent records
			r.history = r.history[len(r.history)-r.maxHistory:]
		}
	}

	// Get event creator and node name for K8s event creation (outside lock)
	eventCreator := r.eventCreator
	nodeName := r.nodeName
	r.mu.Unlock()

	// Create Kubernetes event if event creator is configured
	if eventCreator != nil && nodeName != "" {
		event := r.createRemediationEvent(record, nodeName)

		// Create event in background to avoid blocking
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if err := eventCreator.CreateEvent(ctx, event); err != nil {
				r.logWarnf("Failed to create Kubernetes event for remediation: %v", err)
			}
		}()
	}
}

// GetHistory returns a copy of the remediation history.
// Results are ordered from oldest to newest.
func (r *RemediatorRegistry) GetHistory(limit int) []RemediationRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if limit <= 0 || limit > len(r.history) {
		limit = len(r.history)
	}

	// Return the most recent 'limit' records
	startIdx := len(r.history) - limit
	if startIdx < 0 {
		startIdx = 0
	}

	result := make([]RemediationRecord, limit)
	copy(result, r.history[startIdx:])
	return result
}

// GetCircuitState returns the current circuit breaker state.
func (r *RemediatorRegistry) GetCircuitState() CircuitBreakerState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.circuitState
}

// GetStats returns statistics about the registry state.
type RegistryStats struct {
	// RegisteredTypes is the number of registered remediator types
	RegisteredTypes int

	// CircuitState is the current circuit breaker state
	CircuitState CircuitBreakerState

	// ConsecutiveFailures is the current count of consecutive failures
	ConsecutiveFailures int

	// ConsecutiveSuccesses is the current count of consecutive successes
	ConsecutiveSuccesses int

	// CircuitOpenedAt is when the circuit was last opened (if currently open)
	CircuitOpenedAt time.Time

	// RecentRemediations is the count of remediations in the current rate limit window
	RecentRemediations int

	// MaxPerHour is the rate limit
	MaxPerHour int

	// HistorySize is the current number of records in history
	HistorySize int

	// MaxHistory is the maximum history size
	MaxHistory int

	// DryRun indicates if the registry is in dry-run mode
	DryRun bool
}

// GetStats returns current statistics about the registry.
func (r *RemediatorRegistry) GetStats() RegistryStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return RegistryStats{
		RegisteredTypes:      len(r.remediators),
		CircuitState:         r.circuitState,
		ConsecutiveFailures:  r.consecutiveFailures,
		ConsecutiveSuccesses: r.consecutiveSuccesses,
		CircuitOpenedAt:      r.circuitOpenedAt,
		RecentRemediations:   len(r.remediationTimes),
		MaxPerHour:           r.maxPerHour,
		HistorySize:          len(r.history),
		MaxHistory:           r.maxHistory,
		DryRun:               r.dryRun,
	}
}

// ResetCircuitBreaker manually resets the circuit breaker to closed state.
// This is primarily useful for testing or manual intervention.
func (r *RemediatorRegistry) ResetCircuitBreaker() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.circuitState = CircuitClosed
	r.consecutiveFailures = 0
	r.consecutiveSuccesses = 0
	r.circuitOpenedAt = time.Time{}
	r.circuitLastStateChange = time.Now()
	r.logInfof("Circuit breaker manually reset to Closed")
}

// createRemediationEvent creates a Kubernetes event from a remediation record.
func (r *RemediatorRegistry) createRemediationEvent(record RemediationRecord, nodeName string) corev1.Event {
	// Determine event type based on success/failure
	eventType := corev1.EventTypeNormal
	reason := "RemediationSuccess"
	message := fmt.Sprintf("Successfully remediated %s on %s using %s (duration: %v)",
		record.Problem.Type, record.Problem.Resource, record.RemediatorType, record.Duration)

	if !record.Success {
		eventType = corev1.EventTypeWarning
		reason = "RemediationFailure"
		message = fmt.Sprintf("Failed to remediate %s on %s using %s: %s (duration: %v)",
			record.Problem.Type, record.Problem.Resource, record.RemediatorType, record.Error, record.Duration)
	}

	// Generate unique event name
	timestamp := record.StartTime.Unix()
	eventName := fmt.Sprintf("node-doctor-%s-remediation-%s-%d",
		nodeName, sanitizeEventName(record.RemediatorType), timestamp)

	return corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      eventName,
			Namespace: "default", // Will be overridden by event creator
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:      "Node",
			Name:      nodeName,
			Namespace: "",
		},
		Reason:  reason,
		Message: message,
		Source: corev1.EventSource{
			Component: "node-doctor-remediator",
			Host:      nodeName,
		},
		FirstTimestamp: metav1.NewTime(record.StartTime),
		LastTimestamp:  metav1.NewTime(record.EndTime),
		Count:          1,
		Type:           eventType,
	}
}

// sanitizeEventName ensures the event name is valid for Kubernetes.
func sanitizeEventName(name string) string {
	sanitized := ""
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			sanitized += string(r)
		} else {
			sanitized += "-"
		}
	}
	if sanitized == "" {
		return "unknown"
	}
	return sanitized
}

// logInfof logs an informational message if a logger is configured.
func (r *RemediatorRegistry) logInfof(format string, args ...interface{}) {
	if r.logger != nil {
		r.logger.Infof("[RemediatorRegistry] "+format, args...)
	}
}

// logWarnf logs a warning message if a logger is configured.
func (r *RemediatorRegistry) logWarnf(format string, args ...interface{}) {
	if r.logger != nil {
		r.logger.Warnf("[RemediatorRegistry] "+format, args...)
	}
}

// logErrorf logs an error message if a logger is configured.
func (r *RemediatorRegistry) logErrorf(format string, args ...interface{}) {
	if r.logger != nil {
		r.logger.Errorf("[RemediatorRegistry] "+format, args...)
	}
}

// formatError converts an error to a string for history recording.
func formatError(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
