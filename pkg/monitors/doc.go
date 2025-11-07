// Package monitors provides a pluggable monitor registry system for Node Doctor.
//
// This package implements a factory pattern that allows monitor implementations
// to self-register via func init() and provides thread-safe runtime discovery
// and instantiation. The registry serves as the central coordination point
// between monitor configurations and their implementations.
//
// # Architecture
//
// The monitor registry consists of several key components:
//
//   - Registry: Thread-safe storage and factory for monitor instances
//   - MonitorInfo: Registration metadata including factory and validator functions
//   - Package-level functions: Convenient API for the default global registry
//
// # Monitor Registration
//
// Monitor implementations register themselves during package initialization:
//
//	func init() {
//		monitors.Register(monitors.MonitorInfo{
//			Type:        "system-disk-check",
//			Factory:     NewSystemDiskMonitor,
//			Validator:   ValidateSystemDiskConfig,
//			Description: "Monitors system disk usage and health",
//		})
//	}
//
// The registration process:
//
//  1. Happens at init time (single-threaded, no locking needed for registration)
//  2. Panics on conflicts (fail-fast for development-time errors)
//  3. Stores factory functions, not instances (enables on-demand creation)
//  4. Includes optional validators for early configuration validation
//
// # Monitor Creation
//
// At runtime, monitors are created from configuration:
//
//	// Single monitor
//	config := types.MonitorConfig{
//		Name:    "disk-monitor-1",
//		Type:    "system-disk-check",
//		Enabled: true,
//		Config: map[string]interface{}{
//			"path":      "/var/lib/kubelet",
//			"threshold": 85,
//		},
//	}
//
//	monitor, err := monitors.CreateMonitor(ctx, config)
//	if err != nil {
//		return fmt.Errorf("failed to create monitor: %w", err)
//	}
//
//	// Multiple monitors from configuration
//	allMonitors, err := monitors.CreateMonitorsFromConfigs(ctx, configs)
//
// # Thread Safety
//
// The registry is designed for high read concurrency with infrequent writes:
//
//   - Registration: Single-threaded (init time only)
//   - Read operations: Concurrent with RWMutex protection
//   - Monitor creation: Fully concurrent and thread-safe
//   - Factory functions: Must be thread-safe and stateless
//
// # Integration with Problem Detector
//
// The Problem Detector uses the registry to instantiate monitors:
//
//	// In pkg/detector/detector.go
//	monitors, err := monitors.CreateMonitorsFromConfigs(ctx, config.Monitors)
//	if err != nil {
//		return nil, fmt.Errorf("failed to create monitors: %w", err)
//	}
//
//	detector := &ProblemDetector{
//		monitors: monitors,
//		// ... other fields
//	}
//
// # Error Handling
//
// The registry provides different error handling strategies:
//
//   - Registration errors: Panic (development-time issues)
//   - Validation errors: Return error (configuration issues)
//   - Creation errors: Return error (runtime issues)
//   - Factory errors: Propagated to caller
//
// # Best Practices for Monitor Authors
//
// When implementing a new monitor:
//
//  1. Register in init() function
//  2. Provide a validator function for early config validation
//  3. Make factory functions thread-safe and stateless
//  4. Use context for cancellation in factory functions
//  5. Handle configuration parsing within the factory
//  6. Return meaningful error messages
//
// Example monitor implementation:
//
//	package diskmonitor
//
//	import (
//		"context"
//		"fmt"
//		"github.com/supporttools/node-doctor/pkg/monitors"
//		"github.com/supporttools/node-doctor/pkg/types"
//	)
//
//	func init() {
//		monitors.Register(monitors.MonitorInfo{
//			Type:        "system-disk-check",
//			Factory:     NewDiskMonitor,
//			Validator:   ValidateDiskConfig,
//			Description: "Monitors disk usage and health",
//		})
//	}
//
//	func NewDiskMonitor(ctx context.Context, config types.MonitorConfig) (types.Monitor, error) {
//		// Parse monitor-specific configuration
//		diskConfig, err := parseDiskConfig(config.Config)
//		if err != nil {
//			return nil, fmt.Errorf("invalid disk monitor config: %w", err)
//		}
//
//		return &DiskMonitor{
//			name:      config.Name,
//			path:      diskConfig.Path,
//			threshold: diskConfig.Threshold,
//		}, nil
//	}
//
//	func ValidateDiskConfig(config types.MonitorConfig) error {
//		if config.Config == nil {
//			return fmt.Errorf("config is required")
//		}
//
//		path, ok := config.Config["path"].(string)
//		if !ok || path == "" {
//			return fmt.Errorf("path is required and must be a string")
//		}
//
//		return nil
//	}
//
// # Testing
//
// The package provides comprehensive test coverage including:
//
//   - Unit tests for all public APIs
//   - Concurrent access testing with race detector
//   - Error condition testing
//   - Benchmark tests for performance validation
//   - Mock implementations for testing monitor consumers
//
// Use the provided test utilities when testing monitor implementations:
//
//	func TestMyMonitor(t *testing.T) {
//		registry := monitors.NewRegistry()
//		registry.Register(monitors.MonitorInfo{
//			Type:    "test-monitor",
//			Factory: NewMyMonitor,
//		})
//
//		config := types.MonitorConfig{
//			Name:    "test",
//			Type:    "test-monitor",
//			Enabled: true,
//		}
//
//		monitor, err := registry.CreateMonitor(context.Background(), config)
//		// ... test the monitor
//	}
package monitors
