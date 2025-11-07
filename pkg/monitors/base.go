// Package monitors provides common functionality for monitor implementations.
// The BaseMonitor struct provides reusable functionality that concrete monitor
// implementations can embed or use as a foundation.
package monitors

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// CheckFunc is a function type that performs the actual monitoring check.
// It receives a context for cancellation/timeout and returns a Status or an error.
// If the function returns nil status and nil error, it's treated as a healthy check
// with no specific status to report.
type CheckFunc func(ctx context.Context) (*types.Status, error)

// Logger provides optional logging functionality for monitors.
// If a logger is not provided, logging operations are silently ignored.
type Logger interface {
	// Infof logs an informational message with formatting.
	Infof(format string, args ...interface{})

	// Warnf logs a warning message with formatting.
	Warnf(format string, args ...interface{})

	// Errorf logs an error message with formatting.
	Errorf(format string, args ...interface{})
}

// BaseMonitor provides common functionality for monitor implementations.
// It handles the standard monitor lifecycle (start/stop), status channel management,
// timeout enforcement, interval timing, and error handling. Concrete monitors can
// embed this struct or use it as a foundation for their implementation.
//
// The BaseMonitor is designed to be thread-safe and provides graceful shutdown
// capabilities with proper resource cleanup.
//
// Example usage:
//
//	monitor := NewBaseMonitor("disk-monitor", 30*time.Second, 10*time.Second)
//	monitor.SetCheckFunc(func(ctx context.Context) (*types.Status, error) {
//		// Perform monitoring check
//		return status, nil
//	})
//	statusCh, err := monitor.Start()
//	if err != nil {
//		log.Fatal(err)
//	}
//	// Process status updates from statusCh
//	monitor.Stop() // Graceful shutdown
type BaseMonitor struct {
	// Configuration fields (immutable after creation)
	name     string
	interval time.Duration
	timeout  time.Duration

	// Runtime state
	statusChan chan *types.Status
	stopChan   chan struct{}
	wg         sync.WaitGroup
	mu         sync.RWMutex
	running    bool
	stopped    bool // Track if monitor was stopped (for restart handling)

	// Check function and logger
	checkFunc CheckFunc
	logger    Logger
}

// NewBaseMonitor creates a new BaseMonitor with the specified configuration.
// The monitor is created in a stopped state and must be started with Start().
//
// Parameters:
//   - name: Unique identifier for this monitor instance
//   - interval: How often to perform monitoring checks
//   - timeout: Maximum time allowed for each check operation
//
// Validation rules:
//   - name must not be empty
//   - interval must be positive
//   - timeout must be positive
//   - timeout must be less than interval
//
// Returns an error if validation fails.
func NewBaseMonitor(name string, interval, timeout time.Duration) (*BaseMonitor, error) {
	// Validate parameters
	if name == "" {
		return nil, fmt.Errorf("monitor name cannot be empty")
	}
	if interval <= 0 {
		return nil, fmt.Errorf("interval must be positive, got %v", interval)
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("timeout must be positive, got %v", timeout)
	}
	if timeout >= interval {
		return nil, fmt.Errorf("timeout (%v) must be less than interval (%v)", timeout, interval)
	}

	return &BaseMonitor{
		name:       name,
		interval:   interval,
		timeout:    timeout,
		statusChan: make(chan *types.Status, 10), // Buffered for non-blocking sends
		stopChan:   make(chan struct{}),
	}, nil
}

// SetCheckFunc sets the function that will be called to perform monitoring checks.
// This function must be set before calling Start().
//
// The checkFunc will be called periodically according to the monitor's interval.
// It should perform the actual monitoring logic and return a Status describing
// the current state, or an error if the check fails.
//
// The function receives a context that will be cancelled if the check exceeds
// the monitor's timeout. Implementations should respect this context.
//
// Returns an error if the monitor is currently running.
func (b *BaseMonitor) SetCheckFunc(checkFunc CheckFunc) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.running {
		return fmt.Errorf("cannot change check function while monitor %q is running", b.name)
	}

	b.checkFunc = checkFunc
	return nil
}

// SetLogger sets an optional logger for the monitor.
// If no logger is set, logging operations are silently ignored.
//
// Returns an error if the monitor is currently running.
func (b *BaseMonitor) SetLogger(logger Logger) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.running {
		return fmt.Errorf("cannot change logger while monitor %q is running", b.name)
	}

	b.logger = logger
	return nil
}

// GetName returns the monitor's name.
// This method is thread-safe and can be called concurrently.
func (b *BaseMonitor) GetName() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.name
}

// GetInterval returns the monitor's check interval.
// This method is thread-safe and can be called concurrently.
func (b *BaseMonitor) GetInterval() time.Duration {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.interval
}

// GetTimeout returns the monitor's check timeout.
// This method is thread-safe and can be called concurrently.
func (b *BaseMonitor) GetTimeout() time.Duration {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.timeout
}

// IsRunning returns whether the monitor is currently running.
// This method is thread-safe and can be called concurrently.
func (b *BaseMonitor) IsRunning() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.running
}

// Start begins the monitoring process and returns a channel for status updates.
// The monitor runs asynchronously and sends Status updates through the channel
// until Stop() is called.
//
// Start can be called multiple times to restart a stopped monitor. If the monitor
// was previously stopped, new channels are created to ensure safe restart.
//
// Returns an error if:
//   - The monitor is already running
//   - No check function has been set
//
// The returned channel will be closed when the monitor stops.
func (b *BaseMonitor) Start() (<-chan *types.Status, error) {
	b.mu.Lock()

	if b.running {
		b.mu.Unlock()
		return nil, fmt.Errorf("monitor %q is already running", b.name)
	}

	if b.checkFunc == nil {
		b.mu.Unlock()
		return nil, fmt.Errorf("check function must be set before starting monitor %q", b.name)
	}

	// If monitor was previously stopped, recreate channels for safe restart
	if b.stopped {
		b.statusChan = make(chan *types.Status, 10)
		b.stopChan = make(chan struct{})
		b.stopped = false
	}

	b.running = true

	// Get values for logging without holding lock
	name := b.name
	interval := b.interval
	timeout := b.timeout

	// Start the monitoring goroutine
	b.wg.Add(1)
	go b.run()

	statusChan := b.statusChan
	b.mu.Unlock()

	// Log after releasing the lock to avoid potential deadlock
	b.logInfof("Starting monitor %q with interval %v, timeout %v", name, interval, timeout)

	return statusChan, nil
}

// Stop gracefully stops the monitor.
// This method blocks until the monitoring goroutine has completely stopped
// and all resources have been cleaned up, or until a timeout occurs.
//
// Stop is safe to call multiple times. If the monitor is not running,
// this method returns immediately.
//
// Stop has a built-in timeout of 30 seconds to prevent indefinite hangs.
// If the monitor doesn't stop within this time, a warning is logged but
// Stop returns to allow the application to continue.
//
// After Stop returns:
//   - The status channel will be closed
//   - No more status updates will be sent
//   - The monitor can be restarted with Start()
//   - IsRunning() will return false
func (b *BaseMonitor) Stop() {
	b.mu.Lock()
	if !b.running {
		b.mu.Unlock()
		return
	}
	name := b.name
	b.mu.Unlock()

	b.logInfof("Stopping monitor %q", name)

	// Signal the monitoring goroutine to stop
	close(b.stopChan)

	// Wait for the goroutine to complete with timeout
	done := make(chan struct{})
	go func() {
		b.wg.Wait()
		close(done)
	}()

	// Wait for completion or timeout (30 seconds)
	timeout := time.NewTimer(30 * time.Second)
	defer timeout.Stop()

	select {
	case <-done:
		// Normal completion
		b.mu.Lock()
		b.running = false
		b.stopped = true
		b.mu.Unlock()
		b.logInfof("Monitor %q stopped", name)
	case <-timeout.C:
		// Timeout - force state update and log warning
		b.mu.Lock()
		b.running = false
		b.stopped = true
		b.mu.Unlock()
		b.logWarnf("Monitor %q stop timeout after 30 seconds - forcing shutdown", name)
	}
}

// run contains the main monitoring loop.
// This method runs in a separate goroutine and performs the following:
//  1. Sets up a ticker for periodic checks
//  2. Performs an initial check
//  3. Loops until stopped, performing checks on each tick
//  4. Handles errors and timeouts gracefully
//  5. Cleans up resources on shutdown
func (b *BaseMonitor) run() {
	defer func() {
		close(b.statusChan)
		b.wg.Done()
	}()

	b.mu.RLock()
	interval := b.interval
	name := b.name
	b.mu.RUnlock()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	b.logInfof("Monitor %q started, performing initial check", name)

	// Perform initial check
	b.executeCheck()

	// Main monitoring loop
	for {
		select {
		case <-b.stopChan:
			// Stop requested - clean shutdown
			b.logInfof("Monitor %q received stop signal", name)
			return

		case <-ticker.C:
			// Periodic check
			b.executeCheck()
		}
	}
}

// executeCheck performs a single monitoring check with timeout enforcement.
// This method handles:
//   - Context creation with timeout
//   - Panic recovery from checkFunc
//   - Error handling and logging
//   - Status channel sending (non-blocking)
func (b *BaseMonitor) executeCheck() {
	// Get values needed for check
	b.mu.RLock()
	timeout := b.timeout
	name := b.name
	checkFunc := b.checkFunc
	b.mu.RUnlock()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var status *types.Status
	var err error

	// Execute check function with panic recovery
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("check function panicked: %v", r)
				b.logErrorf("Monitor %q check function panicked: %v", name, r)
			}
		}()

		if checkFunc != nil {
			status, err = checkFunc(ctx)
		} else {
			err = fmt.Errorf("check function is nil")
		}
	}()

	// Handle check results
	if err != nil {
		// Check failed - create error status
		b.logWarnf("Monitor %q check failed: %v", name, err)
		status = b.createErrorStatus(err)
	} else if status == nil {
		// Check succeeded but no status returned - create healthy status
		status = b.createHealthyStatus()
	}

	// Send status (non-blocking)
	b.sendStatus(status)
}

// createErrorStatus creates a Status indicating a check error.
func (b *BaseMonitor) createErrorStatus(err error) *types.Status {
	b.mu.RLock()
	name := b.name
	b.mu.RUnlock()

	status := types.NewStatus(name)

	// Add error event
	event := types.NewEvent(
		types.EventError,
		"CheckFailed",
		fmt.Sprintf("Monitor check failed: %v", err),
	)
	status.AddEvent(event)

	// Add unhealthy condition
	condition := types.NewCondition(
		"MonitorHealthy",
		types.ConditionFalse,
		"CheckFailed",
		fmt.Sprintf("Monitor %q check failed: %v", name, err),
	)
	status.AddCondition(condition)

	return status
}

// createHealthyStatus creates a Status indicating a successful check with no specific status.
func (b *BaseMonitor) createHealthyStatus() *types.Status {
	b.mu.RLock()
	name := b.name
	b.mu.RUnlock()

	status := types.NewStatus(name)

	// Add healthy condition
	condition := types.NewCondition(
		"MonitorHealthy",
		types.ConditionTrue,
		"CheckSucceeded",
		fmt.Sprintf("Monitor %q check completed successfully", name),
	)
	status.AddCondition(condition)

	return status
}

// sendStatus sends a status update through the status channel.
// This method uses a non-blocking send to prevent the monitoring loop
// from being blocked by slow consumers.
func (b *BaseMonitor) sendStatus(status *types.Status) {
	if status == nil {
		return
	}

	b.mu.RLock()
	name := b.name
	b.mu.RUnlock()

	select {
	case b.statusChan <- status:
		// Status sent successfully
	default:
		// Channel is full - drop the status to prevent blocking
		// Log this condition for debugging
		b.logWarnf("Monitor %q status channel full, dropping status update", name)
	}
}

// Helper logging methods that check for logger availability

// logInfof logs an informational message if a logger is available.
func (b *BaseMonitor) logInfof(format string, args ...interface{}) {
	b.mu.RLock()
	logger := b.logger
	b.mu.RUnlock()

	if logger != nil {
		logger.Infof(format, args...)
	}
}

// logWarnf logs a warning message if a logger is available.
func (b *BaseMonitor) logWarnf(format string, args ...interface{}) {
	b.mu.RLock()
	logger := b.logger
	b.mu.RUnlock()

	if logger != nil {
		logger.Warnf(format, args...)
	}
}

// logErrorf logs an error message if a logger is available.
func (b *BaseMonitor) logErrorf(format string, args ...interface{}) {
	b.mu.RLock()
	logger := b.logger
	b.mu.RUnlock()

	if logger != nil {
		logger.Errorf(format, args...)
	}
}
