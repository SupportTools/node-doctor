// Package monitors provides a pluggable monitor registry system for Node Doctor.
//
// The registry allows monitors to self-register via func init() and provides
// thread-safe runtime discovery and instantiation of monitors. This enables
// a clean factory pattern where monitor implementations can be discovered
// and created without tight coupling.
//
// Usage Example:
//
//	// Monitor registration (typically in monitor package init())
//	func init() {
//		monitors.MustRegister(monitors.MonitorInfo{
//			Type:        "system-disk-check",
//			Factory:     NewSystemDiskMonitor,
//			Validator:   ValidateSystemDiskConfig,
//			Description: "Monitors system disk usage and health",
//		})
//	}
//
//	// Monitor creation at runtime
//	monitor, err := registry.CreateMonitor(ctx, config)
//	if err != nil {
//		return fmt.Errorf("failed to create monitor: %w", err)
//	}
package monitors

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/supporttools/node-doctor/pkg/types"
)

// MonitorFactory is a function that creates a new monitor instance.
// It takes a context for cancellation and a MonitorConfig containing
// monitor-specific configuration parameters.
type MonitorFactory func(ctx context.Context, config types.MonitorConfig) (types.Monitor, error)

// MonitorValidator is a function that validates a monitor configuration.
// It should perform early validation of the configuration before attempting
// to create the monitor instance. This allows for fail-fast validation
// at configuration parse time.
type MonitorValidator func(config types.MonitorConfig) error

// MonitorInfo contains metadata and factory functions for a monitor type.
// This is used to register monitor implementations with the registry.
type MonitorInfo struct {
	// Type is the unique identifier for this monitor type.
	// This should match the "type" field in MonitorConfig.
	Type string

	// Factory is the function used to create new instances of this monitor.
	// The factory function should be thread-safe and stateless.
	Factory MonitorFactory

	// Validator is the function used to validate configuration for this monitor.
	// This is optional but recommended for early validation.
	Validator MonitorValidator

	// Description provides human-readable documentation for this monitor type.
	// This is used for help text, documentation, and debugging.
	Description string

	// DefaultConfig provides a default configuration for this monitor type.
	// This is used when adding missing monitors to the configuration.
	// If nil, the monitor will not be auto-added when missing from config.
	DefaultConfig *types.MonitorConfig
}

// Registry manages the registration and creation of monitor instances.
// It provides thread-safe operations for registering monitor types at
// init time and creating monitor instances at runtime.
//
// The registry uses a read-write mutex to optimize for the common case
// of many concurrent reads (monitor creation) with infrequent writes
// (monitor registration during init).
type Registry struct {
	// mu protects the monitors map from concurrent access
	mu sync.RWMutex

	// monitors maps monitor type names to their registration info
	monitors map[string]*MonitorInfo
}

// DefaultRegistry is the global monitor registry instance.
// Most applications should use this instance through the package-level
// functions rather than creating their own registry.
var DefaultRegistry = NewRegistry()

// NewRegistry creates a new empty monitor registry.
// In most cases, you should use the DefaultRegistry instead of creating
// a new one. This function is primarily useful for testing or specialized
// use cases that require isolated registries.
func NewRegistry() *Registry {
	return &Registry{
		monitors: make(map[string]*MonitorInfo),
	}
}

// ErrEmptyMonitorType is returned when attempting to register a monitor with an empty type.
var ErrEmptyMonitorType = errors.New("monitor type cannot be empty")

// ErrNilFactory is returned when attempting to register a monitor with a nil factory.
var ErrNilFactory = errors.New("monitor factory cannot be nil")

// ErrDuplicateMonitorType is returned when attempting to register a monitor type that already exists.
var ErrDuplicateMonitorType = errors.New("monitor type is already registered")

// Register adds a new monitor type to the registry.
// This function is typically called from monitor package init() functions
// to self-register monitor implementations.
//
// Register returns an error if:
//   - info.Type is empty
//   - info.Factory is nil
//   - A monitor with the same type is already registered
//
// For use in init() functions where panicking on error is desired, use MustRegister instead.
func (r *Registry) Register(info MonitorInfo) error {
	if info.Type == "" {
		return ErrEmptyMonitorType
	}
	if info.Factory == nil {
		return fmt.Errorf("%w for type %q", ErrNilFactory, info.Type)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.monitors[info.Type]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicateMonitorType, info.Type)
	}

	// Create a copy to avoid potential issues with pointer sharing
	infoCopy := info
	r.monitors[info.Type] = &infoCopy
	return nil
}

// MustRegister adds a new monitor type to the registry and panics on error.
// This is intended for use in init() functions where registration failures
// indicate programming errors that should be caught during development.
//
// MustRegister panics if:
//   - info.Type is empty
//   - info.Factory is nil
//   - A monitor with the same type is already registered
func (r *Registry) MustRegister(info MonitorInfo) {
	if err := r.Register(info); err != nil {
		panic(fmt.Sprintf("monitor registration failed: %v", err))
	}
}

// GetRegisteredTypes returns a sorted list of all registered monitor types.
// This is useful for displaying available monitor types, validation,
// and debugging purposes.
//
// The returned slice is a copy and can be safely modified by the caller.
func (r *Registry) GetRegisteredTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.monitors))
	for monitorType := range r.monitors {
		types = append(types, monitorType)
	}

	// Sort for deterministic output
	sort.Strings(types)
	return types
}

// IsRegistered checks whether a monitor type is registered.
// This is useful for early validation before attempting to create
// a monitor instance.
func (r *Registry) IsRegistered(monitorType string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, exists := r.monitors[monitorType]
	return exists
}

// GetMonitorInfo returns the registration information for a monitor type.
// This is useful for introspection, help text generation, and debugging.
//
// Returns nil if the monitor type is not registered.
func (r *Registry) GetMonitorInfo(monitorType string) *MonitorInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	info, exists := r.monitors[monitorType]
	if !exists {
		return nil
	}

	// Return a copy to prevent external modification
	infoCopy := *info
	return &infoCopy
}

// ValidateConfig validates a monitor configuration using the registered
// validator function. If no validator is registered for the monitor type,
// this function performs basic validation (type exists and is enabled).
//
// This function should be called before CreateMonitor to enable fail-fast
// validation at configuration parse time.
func (r *Registry) ValidateConfig(config types.MonitorConfig) error {
	// Basic validation
	if config.Type == "" {
		return fmt.Errorf("monitor type is required")
	}

	r.mu.RLock()
	info, exists := r.monitors[config.Type]
	r.mu.RUnlock()

	if !exists {
		return fmt.Errorf("unknown monitor type %q, available types: %v",
			config.Type, r.GetRegisteredTypes())
	}

	// Call type-specific validator if available
	if info.Validator != nil {
		if err := info.Validator(config); err != nil {
			return fmt.Errorf("validation failed for monitor %q: %w", config.Type, err)
		}
	}

	return nil
}

// CreateMonitor creates a new monitor instance of the specified type.
// The config parameter must contain a valid monitor type and any required
// configuration parameters for that monitor type.
//
// This function is thread-safe and can be called concurrently.
//
// Returns an error if:
//   - The monitor type is not registered
//   - The configuration is invalid
//   - The factory function returns an error
func (r *Registry) CreateMonitor(ctx context.Context, config types.MonitorConfig) (types.Monitor, error) {
	// Validate configuration first
	if err := r.ValidateConfig(config); err != nil {
		return nil, err
	}

	r.mu.RLock()
	info := r.monitors[config.Type]
	r.mu.RUnlock()

	// info is guaranteed to exist because ValidateConfig succeeded
	// Call factory with panic recovery to prevent crashes from buggy monitors
	var monitor types.Monitor
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("monitor factory %q panicked: %v", config.Type, r)
			}
		}()
		monitor, err = info.Factory(ctx, config)
	}()

	if err != nil {
		return nil, fmt.Errorf("failed to create monitor %q: %w", config.Type, err)
	}

	return monitor, nil
}

// CreateMonitorsFromConfigs creates multiple monitor instances from a slice
// of configurations. This is a convenience function that calls CreateMonitor
// for each configuration.
//
// If any monitor creation fails, this function returns an error and does not
// create any of the remaining monitors. The caller should handle partial
// failures appropriately (e.g., by cleaning up successfully created monitors).
//
// For better error handling in production code, consider calling CreateMonitor
// individually and handling failures per monitor.
func (r *Registry) CreateMonitorsFromConfigs(ctx context.Context, configs []types.MonitorConfig) ([]types.Monitor, error) {
	if len(configs) == 0 {
		return nil, nil
	}

	monitors := make([]types.Monitor, 0, len(configs))

	for i, config := range configs {
		// Skip disabled monitors
		if !config.Enabled {
			continue
		}

		monitor, err := r.CreateMonitor(ctx, config)
		if err != nil {
			return nil, fmt.Errorf("failed to create monitor %d (%s): %w", i, config.Name, err)
		}

		monitors = append(monitors, monitor)
	}

	return monitors, nil
}

// ApplyDefaultMonitors adds default monitor configurations for any registered
// monitor types that are missing from the provided configuration.
// It returns a list of monitor types that were added with defaults.
//
// Monitors are only added if:
// - The monitor type has a DefaultConfig defined
// - No monitor with that type already exists in the config
//
// This enables auto-enabling monitors when their config section is missing.
func (r *Registry) ApplyDefaultMonitors(config *types.NodeDoctorConfig) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Build a set of existing monitor types
	existingTypes := make(map[string]bool)
	for _, monitor := range config.Monitors {
		existingTypes[monitor.Type] = true
	}

	// Find monitors that need defaults
	var addedTypes []string
	for monitorType, info := range r.monitors {
		if existingTypes[monitorType] {
			continue // Monitor already configured
		}

		if info.DefaultConfig == nil {
			continue // No default config available
		}

		// Create a copy of the default config
		defaultCopy := *info.DefaultConfig
		config.Monitors = append(config.Monitors, defaultCopy)
		addedTypes = append(addedTypes, monitorType)
	}

	// Sort for deterministic output
	sort.Strings(addedTypes)
	return addedTypes
}

// GetRegistryStats returns statistics about the current state of the registry.
// This is useful for monitoring, debugging, and health checks.
func (r *Registry) GetRegistryStats() RegistryStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	stats := RegistryStats{
		RegisteredTypes: len(r.monitors),
		TypesList:       make([]string, 0, len(r.monitors)),
	}

	for monitorType := range r.monitors {
		stats.TypesList = append(stats.TypesList, monitorType)
	}

	sort.Strings(stats.TypesList)
	return stats
}

// RegistryStats contains statistics about the registry state.
type RegistryStats struct {
	// RegisteredTypes is the total number of registered monitor types
	RegisteredTypes int

	// TypesList is a sorted list of all registered monitor types
	TypesList []string
}

// Package-level convenience functions that operate on the DefaultRegistry.
// These functions provide a simpler API for the common case where applications
// use a single global registry.

// Register registers a monitor type with the default registry.
// See Registry.Register for details.
func Register(info MonitorInfo) error {
	return DefaultRegistry.Register(info)
}

// MustRegister registers a monitor type with the default registry and panics on error.
// See Registry.MustRegister for details.
func MustRegister(info MonitorInfo) {
	DefaultRegistry.MustRegister(info)
}

// GetRegisteredTypes returns all registered monitor types from the default registry.
// See Registry.GetRegisteredTypes for details.
func GetRegisteredTypes() []string {
	return DefaultRegistry.GetRegisteredTypes()
}

// IsRegistered checks if a monitor type is registered in the default registry.
// See Registry.IsRegistered for details.
func IsRegistered(monitorType string) bool {
	return DefaultRegistry.IsRegistered(monitorType)
}

// GetMonitorInfo returns monitor info from the default registry.
// See Registry.GetMonitorInfo for details.
func GetMonitorInfo(monitorType string) *MonitorInfo {
	return DefaultRegistry.GetMonitorInfo(monitorType)
}

// ValidateConfig validates a monitor configuration using the default registry.
// See Registry.ValidateConfig for details.
func ValidateConfig(config types.MonitorConfig) error {
	return DefaultRegistry.ValidateConfig(config)
}

// CreateMonitor creates a monitor instance using the default registry.
// See Registry.CreateMonitor for details.
func CreateMonitor(ctx context.Context, config types.MonitorConfig) (types.Monitor, error) {
	return DefaultRegistry.CreateMonitor(ctx, config)
}

// CreateMonitorsFromConfigs creates multiple monitors using the default registry.
// See Registry.CreateMonitorsFromConfigs for details.
func CreateMonitorsFromConfigs(ctx context.Context, configs []types.MonitorConfig) ([]types.Monitor, error) {
	return DefaultRegistry.CreateMonitorsFromConfigs(ctx, configs)
}

// GetRegistryStats returns statistics about the default registry.
// See Registry.GetRegistryStats for details.
func GetRegistryStats() RegistryStats {
	return DefaultRegistry.GetRegistryStats()
}

// ApplyDefaultMonitors adds default monitors to the config using the default registry.
// See Registry.ApplyDefaultMonitors for details.
func ApplyDefaultMonitors(config *types.NodeDoctorConfig) []string {
	return DefaultRegistry.ApplyDefaultMonitors(config)
}
