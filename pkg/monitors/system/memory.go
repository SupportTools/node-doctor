// Package system provides system-level monitors for Node Doctor.
// This package contains monitors that check various system resources
// and conditions such as CPU, memory, disk, and other system health indicators.
package system

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/supporttools/node-doctor/pkg/monitors"
	"github.com/supporttools/node-doctor/pkg/types"
)

// Register the Memory monitor with the global registry during package initialization.
func init() {
	monitors.MustRegister(monitors.MonitorInfo{
		Type:        "system-memory",
		Factory:     NewMemoryMonitor,
		Validator:   ValidateMemoryConfig,
		Description: "Monitors memory usage, swap usage, and out-of-memory conditions",
		DefaultConfig: &types.MonitorConfig{
			Name:           "memory-health",
			Type:           "system-memory",
			Enabled:        true,
			IntervalString: "30s",
			TimeoutString:  "10s",
			Config: map[string]interface{}{
				"warningThreshold":          85.0,
				"criticalThreshold":         95.0,
				"swapWarningThreshold":      50.0,
				"swapCriticalThreshold":     80.0,
				"sustainedHighMemoryChecks": 3,
				"checkOOMKills":             true,
				"checkMemoryUsage":          true,
				"checkSwapUsage":            true,
			},
		},
	})
}

// MemoryMonitorConfig contains configuration options specific to the Memory monitor.
type MemoryMonitorConfig struct {
	// WarningThreshold is the memory usage percentage that triggers warning events.
	// Default: 85 (85% memory usage)
	WarningThreshold float64 `json:"warningThreshold,omitempty"`

	// CriticalThreshold is the memory usage percentage that triggers critical events/conditions.
	// Default: 95 (95% memory usage)
	CriticalThreshold float64 `json:"criticalThreshold,omitempty"`

	// SwapWarningThreshold is the swap usage percentage that triggers warning events.
	// Default: 50 (50% swap usage)
	SwapWarningThreshold float64 `json:"swapWarningThreshold,omitempty"`

	// SwapCriticalThreshold is the swap usage percentage that triggers critical events.
	// Default: 80 (80% swap usage)
	SwapCriticalThreshold float64 `json:"swapCriticalThreshold,omitempty"`

	// SustainedHighMemoryChecks is the number of consecutive critical checks before generating a condition.
	// Default: 3 checks
	SustainedHighMemoryChecks int `json:"sustainedHighMemoryChecks,omitempty"`

	// CheckOOMKills enables out-of-memory kill detection.
	// Default: true
	CheckOOMKills bool `json:"checkOOMKills,omitempty"`

	// CheckMemoryUsage enables memory usage monitoring.
	// Default: true
	CheckMemoryUsage bool `json:"checkMemoryUsage,omitempty"`

	// CheckSwapUsage enables swap usage monitoring.
	// Default: true
	CheckSwapUsage bool `json:"checkSwapUsage,omitempty"`

	// MemInfoPath is the path to the memory info file.
	// Default: "/proc/meminfo"
	MemInfoPath string `json:"memInfoPath,omitempty"`

	// KmsgPath is the path to the kernel message file for OOM detection.
	// Default: "/dev/kmsg"
	KmsgPath string `json:"kmsgPath,omitempty"`
}

// MemoryMonitor monitors memory health conditions including usage and OOM kills.
// It tracks sustained high memory conditions and generates appropriate events and conditions.
type MemoryMonitor struct {
	name        string
	baseMonitor *monitors.BaseMonitor
	config      *MemoryMonitorConfig

	// State tracking for sustained conditions
	mu                  sync.RWMutex
	highMemoryCount     int
	lastOOMKillTime     time.Time
	oomPermissionWarned bool
	oomReadErrorWarned  bool // Track if we've warned about kmsg read errors (e.g., EINVAL on ARM64)

	// System interfaces (for testing)
	fileReader FileReader
}

// MemoryInfo represents parsed memory data from /proc/meminfo.
type MemoryInfo struct {
	MemTotal     uint64 // Total memory in kB
	MemFree      uint64 // Free memory in kB
	MemAvailable uint64 // Available memory in kB (primary metric)
	Buffers      uint64 // Buffer cache in kB
	Cached       uint64 // Page cache in kB
	SwapTotal    uint64 // Total swap in kB
	SwapFree     uint64 // Free swap in kB
}

// NewMemoryMonitor creates a new Memory monitor instance.
// This factory function parses the monitor configuration and creates a properly
// configured Memory monitor with the specified settings.
func NewMemoryMonitor(ctx context.Context, config types.MonitorConfig) (types.Monitor, error) {
	// Parse Memory-specific configuration
	memoryConfig, err := parseMemoryConfig(config.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Memory config: %w", err)
	}

	// Apply defaults
	if err := memoryConfig.applyDefaults(); err != nil {
		return nil, fmt.Errorf("failed to apply defaults: %w", err)
	}

	// Create base monitor with parsed intervals
	baseMonitor, err := monitors.NewBaseMonitor(config.Name, config.Interval, config.Timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to create base monitor: %w", err)
	}

	// Create Memory monitor
	memoryMonitor := &MemoryMonitor{
		name:        config.Name,
		baseMonitor: baseMonitor,
		config:      memoryConfig,
		fileReader:  &defaultFileReader{},
	}

	// Set the check function
	err = baseMonitor.SetCheckFunc(memoryMonitor.checkMemory)
	if err != nil {
		return nil, fmt.Errorf("failed to set check function: %w", err)
	}

	return memoryMonitor, nil
}

// ValidateMemoryConfig validates the Memory monitor configuration.
// This function performs early validation of configuration parameters to provide
// fail-fast behavior during configuration parsing.
func ValidateMemoryConfig(config types.MonitorConfig) error {
	// Basic validation
	if config.Name == "" {
		return fmt.Errorf("monitor name is required")
	}
	if config.Type != "system-memory" {
		return fmt.Errorf("invalid monitor type %q for Memory monitor", config.Type)
	}

	// Parse and validate Memory-specific config
	memoryConfig, err := parseMemoryConfig(config.Config)
	if err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	// Apply defaults for validation
	if err := memoryConfig.applyDefaults(); err != nil {
		return fmt.Errorf("failed to apply defaults during validation: %w", err)
	}

	return nil
}

// Start begins the monitoring process using the base monitor.
func (m *MemoryMonitor) Start() (<-chan *types.Status, error) {
	return m.baseMonitor.Start()
}

// Stop gracefully stops the monitor using the base monitor.
func (m *MemoryMonitor) Stop() {
	m.baseMonitor.Stop()
}

// checkMemory performs the Memory health check and returns a status report.
// This is the main check function that gets called periodically by the base monitor.
func (m *MemoryMonitor) checkMemory(ctx context.Context) (*types.Status, error) {
	status := types.NewStatus(m.name)

	// Check memory usage if enabled
	if m.config.CheckMemoryUsage {
		if err := m.checkMemoryUsage(ctx, status); err != nil {
			// Log error but continue with other checks
			status.AddEvent(types.NewEvent(
				types.EventError,
				"MemoryCheckFailed",
				fmt.Sprintf("Failed to check memory usage: %v", err),
			))
		}
	}

	// Check swap usage if enabled
	if m.config.CheckSwapUsage {
		if err := m.checkSwapUsage(ctx, status); err != nil {
			// Log error but continue with other checks
			status.AddEvent(types.NewEvent(
				types.EventError,
				"SwapCheckFailed",
				fmt.Sprintf("Failed to check swap usage: %v", err),
			))
		}
	}

	// Check OOM kills if enabled
	if m.config.CheckOOMKills {
		if err := m.checkOOMKills(ctx, status); err != nil {
			// Log error but continue with other checks
			status.AddEvent(types.NewEvent(
				types.EventError,
				"OOMCheckFailed",
				fmt.Sprintf("Failed to check OOM kills: %v", err),
			))
		}
	}

	// If no events were added and checks are enabled, add a healthy condition
	if len(status.Events) == 0 && (m.config.CheckMemoryUsage || m.config.CheckSwapUsage || m.config.CheckOOMKills) {
		status.AddCondition(types.NewCondition(
			"MemoryHealthy",
			types.ConditionTrue,
			"MemoryHealthy",
			"Memory is operating normally",
		))
	}

	return status, nil
}

// checkMemoryUsage monitors memory usage and generates appropriate events/conditions.
func (m *MemoryMonitor) checkMemoryUsage(ctx context.Context, status *types.Status) error {
	// Parse memory info
	memInfo, err := m.parseMemInfo()
	if err != nil {
		return fmt.Errorf("failed to parse memory info: %w", err)
	}

	// Calculate memory usage percentage
	// Use MemAvailable if available, otherwise fallback to MemFree
	var availableMemory uint64
	if memInfo.MemAvailable > 0 {
		availableMemory = memInfo.MemAvailable
	} else {
		availableMemory = memInfo.MemFree
	}

	if memInfo.MemTotal == 0 {
		return fmt.Errorf("invalid MemTotal: 0")
	}

	memoryUsagePercent := (1.0 - float64(availableMemory)/float64(memInfo.MemTotal)) * 100.0

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check for critical memory usage
	if memoryUsagePercent >= m.config.CriticalThreshold {
		m.highMemoryCount++

		// Generate immediate event for high memory usage
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"HighMemoryUsage",
			fmt.Sprintf("High memory usage detected: %.1f%% (%.2f GB used of %.2f GB total, %.2f GB available)",
				memoryUsagePercent,
				float64(memInfo.MemTotal-availableMemory)/1024/1024,
				float64(memInfo.MemTotal)/1024/1024,
				float64(availableMemory)/1024/1024),
		))

		// Generate condition for sustained high memory usage
		if m.highMemoryCount >= m.config.SustainedHighMemoryChecks {
			status.AddCondition(types.NewCondition(
				"MemoryPressure",
				types.ConditionTrue,
				"SustainedHighMemory",
				fmt.Sprintf("Memory has been under sustained high usage for %d consecutive checks (%.1f%% >= %.1f%%)",
					m.highMemoryCount, memoryUsagePercent, m.config.CriticalThreshold),
			))
		}
	} else if memoryUsagePercent >= m.config.WarningThreshold {
		// Reset high memory counter on non-critical usage
		m.highMemoryCount = 0

		// Generate warning event
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"ElevatedMemoryUsage",
			fmt.Sprintf("Elevated memory usage detected: %.1f%% (%.2f GB used of %.2f GB total, %.2f GB available)",
				memoryUsagePercent,
				float64(memInfo.MemTotal-availableMemory)/1024/1024,
				float64(memInfo.MemTotal)/1024/1024,
				float64(availableMemory)/1024/1024),
		))

		// Add condition indicating no pressure but elevated usage
		status.AddCondition(types.NewCondition(
			"MemoryPressure",
			types.ConditionFalse,
			"ElevatedMemory",
			fmt.Sprintf("Memory usage is elevated but not critical (%.1f%%)", memoryUsagePercent),
		))
	} else {
		// Reset high memory counter on normal usage
		m.highMemoryCount = 0

		// Add condition indicating normal memory state
		status.AddCondition(types.NewCondition(
			"MemoryPressure",
			types.ConditionFalse,
			"NormalMemory",
			fmt.Sprintf("Memory usage is normal (%.1f%%)", memoryUsagePercent),
		))
	}

	return nil
}

// checkSwapUsage monitors swap usage and generates appropriate events.
func (m *MemoryMonitor) checkSwapUsage(ctx context.Context, status *types.Status) error {
	// Parse memory info
	memInfo, err := m.parseMemInfo()
	if err != nil {
		return fmt.Errorf("failed to parse memory info: %w", err)
	}

	// Skip swap checks if no swap is configured
	if memInfo.SwapTotal == 0 {
		return nil
	}

	// Calculate swap usage percentage
	swapUsedKB := memInfo.SwapTotal - memInfo.SwapFree
	swapUsagePercent := (float64(swapUsedKB) / float64(memInfo.SwapTotal)) * 100.0

	// Check for critical swap usage
	if swapUsagePercent >= m.config.SwapCriticalThreshold {
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"HighSwapUsage",
			fmt.Sprintf("High swap usage detected: %.1f%% (%.2f GB used of %.2f GB total)",
				swapUsagePercent,
				float64(swapUsedKB)/1024/1024,
				float64(memInfo.SwapTotal)/1024/1024),
		))
	} else if swapUsagePercent >= m.config.SwapWarningThreshold {
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"ElevatedSwapUsage",
			fmt.Sprintf("Elevated swap usage detected: %.1f%% (%.2f GB used of %.2f GB total)",
				swapUsagePercent,
				float64(swapUsedKB)/1024/1024,
				float64(memInfo.SwapTotal)/1024/1024),
		))
	}

	return nil
}

// checkOOMKills monitors for out-of-memory kills and generates appropriate events.
func (m *MemoryMonitor) checkOOMKills(ctx context.Context, status *types.Status) error {
	// Try to read kernel messages for OOM kills
	data, err := m.fileReader.ReadFile(m.config.KmsgPath)
	if err != nil {
		// Handle permission denied gracefully (log warning once, continue)
		if os.IsPermission(err) {
			m.mu.Lock()
			if !m.oomPermissionWarned {
				m.oomPermissionWarned = true
				m.mu.Unlock()
				status.AddEvent(types.NewEvent(
					types.EventWarning,
					"OOMCheckPermissionDenied",
					fmt.Sprintf("Cannot access %s for OOM detection: %v", m.config.KmsgPath, err),
				))
			} else {
				m.mu.Unlock()
			}
			return nil
		}

		// Handle EINVAL errors gracefully (occurs on ARM64 platforms when reading /dev/kmsg)
		// This is a platform-specific limitation, not a critical failure
		var pathErr *os.PathError
		if errors.As(err, &pathErr) && errors.Is(pathErr.Err, syscall.EINVAL) {
			m.mu.Lock()
			if !m.oomReadErrorWarned {
				m.oomReadErrorWarned = true
				m.mu.Unlock()
				status.AddEvent(types.NewEvent(
					types.EventWarning,
					"OOMCheckNotSupported",
					fmt.Sprintf("OOM detection via %s not supported on this platform (ARM64): %v. Memory monitoring will continue without OOM detection.", m.config.KmsgPath, err),
				))
			} else {
				m.mu.Unlock()
			}
			return nil
		}

		return fmt.Errorf("failed to read %s: %w", m.config.KmsgPath, err)
	}

	// Look for OOM kill patterns
	oomPattern := regexp.MustCompile(`(Killed process \d+|Out of memory)`)
	lines := strings.Split(string(data), "\n")

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for _, line := range lines {
		if oomPattern.MatchString(line) {
			// Only report if this is a new OOM kill (not seen in the last minute)
			if now.Sub(m.lastOOMKillTime) > time.Minute {
				m.lastOOMKillTime = now
				status.AddEvent(types.NewEvent(
					types.EventError,
					"OOMKillDetected",
					fmt.Sprintf("Out-of-memory kill detected: %s", strings.TrimSpace(line)),
				))
				break // Only report one OOM kill per check
			}
		}
	}

	return nil
}

// parseMemInfo reads and parses memory information from /proc/meminfo.
func (m *MemoryMonitor) parseMemInfo() (*MemoryInfo, error) {
	data, err := m.fileReader.ReadFile(m.config.MemInfoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", m.config.MemInfoPath, err)
	}

	memInfo := &MemoryInfo{}
	lines := strings.Split(string(data), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Parse lines like "MemTotal:       192213048 kB"
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		key := strings.TrimSuffix(fields[0], ":")
		valueStr := fields[1]

		value, err := strconv.ParseUint(valueStr, 10, 64)
		if err != nil {
			continue
		}

		switch key {
		case "MemTotal":
			memInfo.MemTotal = value
		case "MemFree":
			memInfo.MemFree = value
		case "MemAvailable":
			memInfo.MemAvailable = value
		case "Buffers":
			memInfo.Buffers = value
		case "Cached":
			memInfo.Cached = value
		case "SwapTotal":
			memInfo.SwapTotal = value
		case "SwapFree":
			memInfo.SwapFree = value
		}
	}

	// Validate required fields
	if memInfo.MemTotal == 0 {
		return nil, fmt.Errorf("MemTotal not found or zero in %s", m.config.MemInfoPath)
	}

	return memInfo, nil
}

// parseMemoryConfig extracts Memory-specific configuration from the generic config map.
func parseMemoryConfig(configMap map[string]interface{}) (*MemoryMonitorConfig, error) {
	config := &MemoryMonitorConfig{}

	if configMap == nil {
		return config, nil
	}

	// Parse warning threshold
	if val, exists := configMap["warningThreshold"]; exists {
		if f, ok := val.(float64); ok {
			config.WarningThreshold = f
		} else {
			return nil, fmt.Errorf("warningThreshold must be a number, got %T", val)
		}
	}

	// Parse critical threshold
	if val, exists := configMap["criticalThreshold"]; exists {
		if f, ok := val.(float64); ok {
			config.CriticalThreshold = f
		} else {
			return nil, fmt.Errorf("criticalThreshold must be a number, got %T", val)
		}
	}

	// Parse swap warning threshold
	if val, exists := configMap["swapWarningThreshold"]; exists {
		if f, ok := val.(float64); ok {
			config.SwapWarningThreshold = f
		} else {
			return nil, fmt.Errorf("swapWarningThreshold must be a number, got %T", val)
		}
	}

	// Parse swap critical threshold
	if val, exists := configMap["swapCriticalThreshold"]; exists {
		if f, ok := val.(float64); ok {
			config.SwapCriticalThreshold = f
		} else {
			return nil, fmt.Errorf("swapCriticalThreshold must be a number, got %T", val)
		}
	}

	// Parse sustained high memory checks
	if val, exists := configMap["sustainedHighMemoryChecks"]; exists {
		if i, ok := val.(int); ok {
			config.SustainedHighMemoryChecks = i
		} else if f, ok := val.(float64); ok {
			config.SustainedHighMemoryChecks = int(f)
		} else {
			return nil, fmt.Errorf("sustainedHighMemoryChecks must be a number, got %T", val)
		}
	}

	// Parse check OOM kills
	if val, exists := configMap["checkOOMKills"]; exists {
		if b, ok := val.(bool); ok {
			config.CheckOOMKills = b
		} else {
			return nil, fmt.Errorf("checkOOMKills must be a boolean, got %T", val)
		}
	}

	// Parse check memory usage
	if val, exists := configMap["checkMemoryUsage"]; exists {
		if b, ok := val.(bool); ok {
			config.CheckMemoryUsage = b
		} else {
			return nil, fmt.Errorf("checkMemoryUsage must be a boolean, got %T", val)
		}
	}

	// Parse check swap usage
	if val, exists := configMap["checkSwapUsage"]; exists {
		if b, ok := val.(bool); ok {
			config.CheckSwapUsage = b
		} else {
			return nil, fmt.Errorf("checkSwapUsage must be a boolean, got %T", val)
		}
	}

	// Parse meminfo path
	if val, exists := configMap["memInfoPath"]; exists {
		if s, ok := val.(string); ok {
			config.MemInfoPath = s
		} else {
			return nil, fmt.Errorf("memInfoPath must be a string, got %T", val)
		}
	}

	// Parse kmsg path
	if val, exists := configMap["kmsgPath"]; exists {
		if s, ok := val.(string); ok {
			config.KmsgPath = s
		} else {
			return nil, fmt.Errorf("kmsgPath must be a string, got %T", val)
		}
	}

	return config, nil
}

// applyDefaults applies default values to the Memory monitor configuration.
func (c *MemoryMonitorConfig) applyDefaults() error {
	if c.WarningThreshold == 0 {
		c.WarningThreshold = 85.0
	}
	if c.CriticalThreshold == 0 {
		c.CriticalThreshold = 95.0
	}
	if c.SwapWarningThreshold == 0 {
		c.SwapWarningThreshold = 50.0
	}
	if c.SwapCriticalThreshold == 0 {
		c.SwapCriticalThreshold = 80.0
	}
	if c.SustainedHighMemoryChecks == 0 {
		c.SustainedHighMemoryChecks = 3
	}
	if c.MemInfoPath == "" {
		c.MemInfoPath = "/proc/meminfo"
	}
	if c.KmsgPath == "" {
		c.KmsgPath = "/dev/kmsg"
	}

	// Default to checking all if not explicitly configured
	if !c.CheckMemoryUsage && !c.CheckSwapUsage && !c.CheckOOMKills {
		c.CheckMemoryUsage = true
		c.CheckSwapUsage = true
		c.CheckOOMKills = true
	}

	// Validate thresholds after applying defaults
	if c.WarningThreshold <= 0 || c.WarningThreshold > 100 {
		return fmt.Errorf("warningThreshold must be between 0 and 100, got %f", c.WarningThreshold)
	}
	if c.CriticalThreshold <= 0 || c.CriticalThreshold > 100 {
		return fmt.Errorf("criticalThreshold must be between 0 and 100, got %f", c.CriticalThreshold)
	}
	if c.WarningThreshold >= c.CriticalThreshold {
		return fmt.Errorf("warningThreshold (%f) must be less than criticalThreshold (%f)",
			c.WarningThreshold, c.CriticalThreshold)
	}

	// Validate swap thresholds
	if c.SwapWarningThreshold <= 0 || c.SwapWarningThreshold > 100 {
		return fmt.Errorf("swapWarningThreshold must be between 0 and 100, got %f", c.SwapWarningThreshold)
	}
	if c.SwapCriticalThreshold <= 0 || c.SwapCriticalThreshold > 100 {
		return fmt.Errorf("swapCriticalThreshold must be between 0 and 100, got %f", c.SwapCriticalThreshold)
	}
	if c.SwapWarningThreshold >= c.SwapCriticalThreshold {
		return fmt.Errorf("swapWarningThreshold (%f) must be less than swapCriticalThreshold (%f)",
			c.SwapWarningThreshold, c.SwapCriticalThreshold)
	}

	// Validate sustained memory checks
	if c.SustainedHighMemoryChecks <= 0 {
		return fmt.Errorf("sustainedHighMemoryChecks must be positive, got %d", c.SustainedHighMemoryChecks)
	}

	return nil
}
