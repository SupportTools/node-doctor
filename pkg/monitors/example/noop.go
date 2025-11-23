// Package example provides example monitor implementations that demonstrate
// the monitor registration and factory patterns used by Node Doctor.
//
// This package serves as both documentation and a testing utility. The no-op
// monitor is particularly useful for testing the registry system and as a
// template for implementing new monitor types.
package example

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/supporttools/node-doctor/pkg/monitors"
	"github.com/supporttools/node-doctor/pkg/types"
)

// Register the no-op monitor with the global registry during package initialization.
// This demonstrates the self-registration pattern that all monitors should follow.
func init() {
	monitors.MustRegister(monitors.MonitorInfo{
		Type:        "noop",
		Factory:     NewNoOpMonitor,
		Validator:   ValidateNoOpConfig,
		Description: "A no-operation monitor that demonstrates the registration pattern",
	})
}

// NoOpMonitor is a monitor implementation that does nothing but report healthy status.
// It serves as an example of the monitor interface implementation and can be used
// for testing registry functionality without side effects.
//
// The NoOpMonitor periodically sends status updates but never reports problems.
// It can be configured with custom intervals and test data for validation purposes.
type NoOpMonitor struct {
	// Configuration
	name          string
	interval      time.Duration
	testMessage   string
	includeEvents bool

	// Runtime state
	statusCh chan *types.Status
	stopCh   chan struct{}
	running  bool
	mu       sync.RWMutex
}

// NoOpConfig contains configuration options specific to the no-op monitor.
// This demonstrates how monitors can define their own configuration schemas
// within the generic MonitorConfig.Config map.
type NoOpConfig struct {
	// Interval controls how often the monitor sends status updates.
	// Defaults to 30 seconds if not specified.
	Interval string `json:"interval,omitempty"`

	// TestMessage is included in status reports for testing purposes.
	// Defaults to "NoOp monitor is healthy".
	TestMessage string `json:"testMessage,omitempty"`

	// IncludeEvents controls whether the monitor includes test events
	// in its status reports. Defaults to false.
	IncludeEvents bool `json:"includeEvents,omitempty"`
}

// NewNoOpMonitor creates a new no-op monitor instance.
// This factory function demonstrates the standard pattern for monitor creation:
//
//  1. Parse monitor-specific configuration from config.Config
//  2. Validate required parameters
//  3. Apply defaults for optional parameters
//  4. Create and return the monitor instance
//
// The factory is thread-safe and stateless, allowing concurrent creation
// of multiple monitor instances.
func NewNoOpMonitor(ctx context.Context, config types.MonitorConfig) (types.Monitor, error) {
	// Parse monitor-specific configuration
	noopConfig, err := parseNoOpConfig(config.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse noop config: %w", err)
	}

	// Parse interval with default
	interval := 30 * time.Second
	if noopConfig.Interval != "" {
		parsed, err := time.ParseDuration(noopConfig.Interval)
		if err != nil {
			return nil, fmt.Errorf("invalid interval %q: %w", noopConfig.Interval, err)
		}
		if parsed <= 0 {
			return nil, fmt.Errorf("interval must be positive, got %v", parsed)
		}
		interval = parsed
	}

	// Apply default test message
	testMessage := noopConfig.TestMessage
	if testMessage == "" {
		testMessage = "NoOp monitor is healthy"
	}

	// Create monitor instance
	monitor := &NoOpMonitor{
		name:          config.Name,
		interval:      interval,
		testMessage:   testMessage,
		includeEvents: noopConfig.IncludeEvents,
		statusCh:      make(chan *types.Status, 10), // Buffered to prevent blocking
		stopCh:        make(chan struct{}),
	}

	return monitor, nil
}

// ValidateNoOpConfig validates the no-op monitor configuration.
// This function demonstrates configuration validation patterns and provides
// early error detection before monitor creation.
//
// While the no-op monitor is very permissive, real monitors should validate
// required parameters, file paths, network addresses, etc.
func ValidateNoOpConfig(config types.MonitorConfig) error {
	// Basic validation - all monitors should check this
	if config.Name == "" {
		return fmt.Errorf("monitor name is required")
	}
	if config.Type != "noop" {
		return fmt.Errorf("invalid monitor type %q for noop monitor", config.Type)
	}

	// Parse and validate monitor-specific config
	noopConfig, err := parseNoOpConfig(config.Config)
	if err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	// Validate interval if specified
	if noopConfig.Interval != "" {
		interval, err := time.ParseDuration(noopConfig.Interval)
		if err != nil {
			return fmt.Errorf("invalid interval %q: %w", noopConfig.Interval, err)
		}
		if interval <= 0 {
			return fmt.Errorf("interval must be positive, got %v", interval)
		}
		if interval < time.Second {
			return fmt.Errorf("interval too small, minimum 1s, got %v", interval)
		}
	}

	return nil
}

// Start begins the monitoring process and returns a channel for status updates.
// This implements the types.Monitor interface and demonstrates the standard
// monitoring loop pattern.
//
// The monitor runs asynchronously and sends periodic status updates through
// the returned channel until Stop() is called.
func (n *NoOpMonitor) Start() (<-chan *types.Status, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.running {
		return nil, fmt.Errorf("monitor %q is already running", n.name)
	}

	n.running = true

	// Start the monitoring goroutine
	go n.run()

	return n.statusCh, nil
}

// Stop gracefully stops the monitor.
// This implements the types.Monitor interface and demonstrates proper cleanup.
//
// Stop blocks until the monitoring goroutine has fully stopped and cleaned up.
func (n *NoOpMonitor) Stop() {
	n.mu.Lock()
	if !n.running {
		n.mu.Unlock()
		return
	}
	n.mu.Unlock()

	// Signal the monitoring goroutine to stop
	close(n.stopCh)

	// Wait for the goroutine to acknowledge shutdown by closing statusCh
	// This ensures clean shutdown and prevents resource leaks
	for range n.statusCh {
		// Drain any remaining status updates
	}

	n.mu.Lock()
	n.running = false
	n.mu.Unlock()
}

// run contains the main monitoring loop.
// This is the core pattern that most monitors will follow:
//
//  1. Set up a ticker for periodic checks
//  2. Loop until stopped, performing checks on each tick
//  3. Send status updates through the channel
//  4. Clean up resources on shutdown
func (n *NoOpMonitor) run() {
	defer close(n.statusCh)

	ticker := time.NewTicker(n.interval)
	defer ticker.Stop()

	// Send initial status
	n.sendStatus()

	for {
		select {
		case <-n.stopCh:
			// Stop requested - clean shutdown
			return

		case <-ticker.C:
			// Periodic status update
			n.sendStatus()
		}
	}
}

// sendStatus creates and sends a status update.
// This demonstrates how to construct status reports with events and conditions.
func (n *NoOpMonitor) sendStatus() {
	status := types.NewStatus(n.name)

	// Add a condition indicating the monitor is healthy
	condition := types.NewCondition(
		"NoOpMonitorReady",
		types.ConditionTrue,
		"MonitorHealthy",
		n.testMessage,
	)
	status.AddCondition(condition)

	// Optionally add test events
	if n.includeEvents {
		event := types.NewEvent(
			types.EventInfo,
			"PeriodicCheck",
			fmt.Sprintf("NoOp monitor %q completed periodic check", n.name),
		)
		status.AddEvent(event)
	}

	// Send the status update (non-blocking)
	select {
	case n.statusCh <- status:
		// Status sent successfully
	default:
		// Channel full - drop the update to prevent blocking
		// In production monitors, consider logging this condition
	}
}

// parseNoOpConfig extracts no-op specific configuration from the generic config map.
// This demonstrates the pattern for parsing monitor-specific configuration.
func parseNoOpConfig(configMap map[string]interface{}) (*NoOpConfig, error) {
	config := &NoOpConfig{}

	if configMap == nil {
		return config, nil
	}

	// Parse interval
	if val, exists := configMap["interval"]; exists {
		if str, ok := val.(string); ok {
			config.Interval = str
		} else {
			return nil, fmt.Errorf("interval must be a string, got %T", val)
		}
	}

	// Parse test message
	if val, exists := configMap["testMessage"]; exists {
		if str, ok := val.(string); ok {
			config.TestMessage = str
		} else {
			return nil, fmt.Errorf("testMessage must be a string, got %T", val)
		}
	}

	// Parse include events flag
	if val, exists := configMap["includeEvents"]; exists {
		if b, ok := val.(bool); ok {
			config.IncludeEvents = b
		} else {
			return nil, fmt.Errorf("includeEvents must be a boolean, got %T", val)
		}
	}

	return config, nil
}

// GetName returns the monitor name for debugging purposes.
// This is not part of the Monitor interface but can be useful for testing.
func (n *NoOpMonitor) GetName() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.name
}

// IsRunning returns whether the monitor is currently running.
// This is not part of the Monitor interface but can be useful for testing.
func (n *NoOpMonitor) IsRunning() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.running
}
