// Package system provides system-level monitors for Node Doctor.
// This package contains monitors that check various system resources
// and conditions such as CPU, memory, disk, and other system health indicators.
package system

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/supporttools/node-doctor/pkg/monitors"
	"github.com/supporttools/node-doctor/pkg/types"
)

// Register the CPU monitor with the global registry during package initialization.
func init() {
	monitors.Register(monitors.MonitorInfo{
		Type:        "system-cpu",
		Factory:     NewCPUMonitor,
		Validator:   ValidateCPUConfig,
		Description: "Monitors CPU load average and thermal throttling conditions",
	})
}

// FileReader provides an abstraction for file system operations.
// This interface allows for easy testing by mocking file system access.
type FileReader interface {
	ReadFile(path string) ([]byte, error)
	ReadDir(path string) ([]os.DirEntry, error)
}

// defaultFileReader implements FileReader using standard Go file operations.
type defaultFileReader struct{}

func (r *defaultFileReader) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (r *defaultFileReader) ReadDir(path string) ([]os.DirEntry, error) {
	return os.ReadDir(path)
}

// CPUMonitorConfig contains configuration options specific to the CPU monitor.
type CPUMonitorConfig struct {
	// WarningLoadFactor is the load average factor (load/cores) that triggers warning events.
	// Default: 0.8 (80% of CPU cores)
	WarningLoadFactor float64 `json:"warningLoadFactor,omitempty"`

	// CriticalLoadFactor is the load average factor that triggers critical events/conditions.
	// Default: 1.5 (150% of CPU cores, indicating oversubscription)
	CriticalLoadFactor float64 `json:"criticalLoadFactor,omitempty"`

	// SustainedHighLoadChecks is the number of consecutive critical checks before generating a condition.
	// Default: 3 checks
	SustainedHighLoadChecks int `json:"sustainedHighLoadChecks,omitempty"`

	// CheckThermalThrottle enables thermal throttling detection.
	// Default: true
	CheckThermalThrottle bool `json:"checkThermalThrottle,omitempty"`

	// CheckLoadAverage enables load average monitoring.
	// Default: true
	CheckLoadAverage bool `json:"checkLoadAverage,omitempty"`

	// LoadAvgPath is the path to the load average file.
	// Default: "/proc/loadavg"
	LoadAvgPath string `json:"loadAvgPath,omitempty"`

	// CPUInfoPath is the path to the CPU info file.
	// Default: "/proc/cpuinfo"
	CPUInfoPath string `json:"cpuInfoPath,omitempty"`

	// ThermalBasePath is the base path for thermal throttle files.
	// Default: "/sys/devices/system/cpu"
	ThermalBasePath string `json:"thermalBasePath,omitempty"`
}

// CPUMonitor monitors CPU health conditions including load average and thermal throttling.
// It tracks sustained high load conditions and generates appropriate events and conditions.
type CPUMonitor struct {
	name        string
	baseMonitor *monitors.BaseMonitor
	config      *CPUMonitorConfig

	// State tracking for sustained conditions
	mu                sync.RWMutex
	highLoadCount     int
	lastThrottleState map[int]int64 // CPU ID -> throttle count

	// System interfaces (for testing)
	fileReader FileReader
}

// NewCPUMonitor creates a new CPU monitor instance.
// This factory function parses the monitor configuration and creates a properly
// configured CPU monitor with the specified settings.
func NewCPUMonitor(ctx context.Context, config types.MonitorConfig) (types.Monitor, error) {
	// Parse CPU-specific configuration
	cpuConfig, err := parseCPUConfig(config.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CPU config: %w", err)
	}

	// Apply defaults
	if err := cpuConfig.applyDefaults(); err != nil {
		return nil, fmt.Errorf("failed to apply defaults: %w", err)
	}

	// Create base monitor with parsed intervals
	baseMonitor, err := monitors.NewBaseMonitor(config.Name, config.Interval, config.Timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to create base monitor: %w", err)
	}

	// Create CPU monitor
	cpuMonitor := &CPUMonitor{
		name:              config.Name,
		baseMonitor:       baseMonitor,
		config:            cpuConfig,
		lastThrottleState: make(map[int]int64),
		fileReader:        &defaultFileReader{},
	}

	// Set the check function
	err = baseMonitor.SetCheckFunc(cpuMonitor.checkCPU)
	if err != nil {
		return nil, fmt.Errorf("failed to set check function: %w", err)
	}

	return cpuMonitor, nil
}

// ValidateCPUConfig validates the CPU monitor configuration.
// This function performs early validation of configuration parameters to provide
// fail-fast behavior during configuration parsing.
func ValidateCPUConfig(config types.MonitorConfig) error {
	// Basic validation
	if config.Name == "" {
		return fmt.Errorf("monitor name is required")
	}
	if config.Type != "system-cpu" {
		return fmt.Errorf("invalid monitor type %q for CPU monitor", config.Type)
	}

	// Parse and validate CPU-specific config
	cpuConfig, err := parseCPUConfig(config.Config)
	if err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	// Apply defaults for validation
	if err := cpuConfig.applyDefaults(); err != nil {
		return fmt.Errorf("failed to apply defaults during validation: %w", err)
	}

	// Validate load factors
	if cpuConfig.WarningLoadFactor <= 0 {
		return fmt.Errorf("warningLoadFactor must be positive, got %f", cpuConfig.WarningLoadFactor)
	}
	if cpuConfig.CriticalLoadFactor <= 0 {
		return fmt.Errorf("criticalLoadFactor must be positive, got %f", cpuConfig.CriticalLoadFactor)
	}
	if cpuConfig.WarningLoadFactor >= cpuConfig.CriticalLoadFactor {
		return fmt.Errorf("warningLoadFactor (%f) must be less than criticalLoadFactor (%f)",
			cpuConfig.WarningLoadFactor, cpuConfig.CriticalLoadFactor)
	}

	// Validate sustained load checks
	if cpuConfig.SustainedHighLoadChecks <= 0 {
		return fmt.Errorf("sustainedHighLoadChecks must be positive, got %d", cpuConfig.SustainedHighLoadChecks)
	}

	return nil
}

// Start begins the monitoring process using the base monitor.
func (c *CPUMonitor) Start() (<-chan *types.Status, error) {
	return c.baseMonitor.Start()
}

// Stop gracefully stops the monitor using the base monitor.
func (c *CPUMonitor) Stop() {
	c.baseMonitor.Stop()
}

// checkCPU performs the CPU health check and returns a status report.
// This is the main check function that gets called periodically by the base monitor.
func (c *CPUMonitor) checkCPU(ctx context.Context) (*types.Status, error) {
	status := types.NewStatus(c.name)

	// Check load average if enabled
	if c.config.CheckLoadAverage {
		if err := c.checkLoadAverage(ctx, status); err != nil {
			// Log error but continue with other checks
			status.AddEvent(types.NewEvent(
				types.EventError,
				"LoadAverageCheckFailed",
				fmt.Sprintf("Failed to check load average: %v", err),
			))
		}
	}

	// Check thermal throttling if enabled
	if c.config.CheckThermalThrottle {
		if err := c.checkThermalThrottle(ctx, status); err != nil {
			// Log error but continue with other checks
			status.AddEvent(types.NewEvent(
				types.EventError,
				"ThermalThrottleCheckFailed",
				fmt.Sprintf("Failed to check thermal throttling: %v", err),
			))
		}
	}

	// If no events were added and both checks are enabled, add a healthy condition
	if len(status.Events) == 0 && (c.config.CheckLoadAverage || c.config.CheckThermalThrottle) {
		status.AddCondition(types.NewCondition(
			"CPUHealthy",
			types.ConditionTrue,
			"CPUHealthy",
			"CPU is operating normally",
		))
	}

	return status, nil
}

// checkLoadAverage monitors CPU load average and generates appropriate events/conditions.
func (c *CPUMonitor) checkLoadAverage(ctx context.Context, status *types.Status) error {
	// Parse load average
	loadAvg, err := c.parseLoadAvg()
	if err != nil {
		return fmt.Errorf("failed to parse load average: %w", err)
	}

	// Get CPU count
	cpuCount, err := c.getCPUCount()
	if err != nil {
		return fmt.Errorf("failed to get CPU count: %w", err)
	}

	// Calculate load factors
	load1Factor := loadAvg.Load1 / float64(cpuCount)
	// Note: load5Factor and load15Factor are calculated for completeness
	// and future use, even though we primarily use load1Factor for decisions
	_ = loadAvg.Load5 / float64(cpuCount)  // load5Factor
	_ = loadAvg.Load15 / float64(cpuCount) // load15Factor

	// Use 1-minute load average for immediate decision making
	currentLoadFactor := load1Factor

	c.mu.Lock()
	defer c.mu.Unlock()

	// Check for critical load
	if currentLoadFactor >= c.config.CriticalLoadFactor {
		c.highLoadCount++

		// Generate immediate event for high load
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"HighCPULoad",
			fmt.Sprintf("High CPU load detected: 1m=%.2f (%.1f%% of %d cores), 5m=%.2f, 15m=%.2f",
				loadAvg.Load1, currentLoadFactor*100, cpuCount, loadAvg.Load5, loadAvg.Load15),
		))

		// Generate condition for sustained high load
		if c.highLoadCount >= c.config.SustainedHighLoadChecks {
			status.AddCondition(types.NewCondition(
				"CPUPressure",
				types.ConditionTrue,
				"SustainedHighLoad",
				fmt.Sprintf("CPU has been under sustained high load for %d consecutive checks (load factor %.2f >= %.2f)",
					c.highLoadCount, currentLoadFactor, c.config.CriticalLoadFactor),
			))
		}
	} else if currentLoadFactor >= c.config.WarningLoadFactor {
		// Reset high load counter on non-critical load
		c.highLoadCount = 0

		// Generate warning event
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"ElevatedCPULoad",
			fmt.Sprintf("Elevated CPU load detected: 1m=%.2f (%.1f%% of %d cores), 5m=%.2f, 15m=%.2f",
				loadAvg.Load1, currentLoadFactor*100, cpuCount, loadAvg.Load5, loadAvg.Load15),
		))

		// Add condition indicating no pressure but elevated load
		status.AddCondition(types.NewCondition(
			"CPUPressure",
			types.ConditionFalse,
			"ElevatedLoad",
			fmt.Sprintf("CPU load is elevated but not critical (load factor %.2f)", currentLoadFactor),
		))
	} else {
		// Reset high load counter on normal load
		c.highLoadCount = 0

		// Add condition indicating normal CPU state
		status.AddCondition(types.NewCondition(
			"CPUPressure",
			types.ConditionFalse,
			"NormalLoad",
			fmt.Sprintf("CPU load is normal (load factor %.2f)", currentLoadFactor),
		))
	}

	return nil
}

// checkThermalThrottle monitors CPU thermal throttling events.
func (c *CPUMonitor) checkThermalThrottle(ctx context.Context, status *types.Status) error {
	cpuDirs, err := c.fileReader.ReadDir(c.config.ThermalBasePath)
	if err != nil {
		// Thermal throttling info may not be available on all systems
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	throttleDetected := false
	var throttleEvents []string

	// Check each CPU directory
	for _, dir := range cpuDirs {
		if !dir.IsDir() || !strings.HasPrefix(dir.Name(), "cpu") {
			continue
		}

		// Extract CPU number
		cpuNumStr := strings.TrimPrefix(dir.Name(), "cpu")
		if cpuNumStr == "" {
			continue
		}
		cpuNum, err := strconv.Atoi(cpuNumStr)
		if err != nil {
			continue
		}

		// Check thermal throttle file
		throttlePath := fmt.Sprintf("%s/%s/thermal_throttle/core_throttle_count", c.config.ThermalBasePath, dir.Name())
		data, err := c.fileReader.ReadFile(throttlePath)
		if err != nil {
			// File may not exist on all systems/CPUs
			continue
		}

		// Parse throttle count
		throttleCount, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
		if err != nil {
			continue
		}

		// Check for throttle count increase
		lastCount, exists := c.lastThrottleState[cpuNum]
		if exists && throttleCount > lastCount {
			throttleDetected = true
			increase := throttleCount - lastCount
			throttleEvents = append(throttleEvents, fmt.Sprintf("CPU%d: %d new throttle events (total: %d)", cpuNum, increase, throttleCount))
		}

		// Update state
		c.lastThrottleState[cpuNum] = throttleCount
	}

	// Generate events if throttling detected
	if throttleDetected {
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"CPUThermalThrottle",
			fmt.Sprintf("CPU thermal throttling detected: %s", strings.Join(throttleEvents, ", ")),
		))

		status.AddCondition(types.NewCondition(
			"CPUThermalHealthy",
			types.ConditionFalse,
			"ThermalThrottle",
			"CPU thermal throttling has been detected",
		))
	} else if len(c.lastThrottleState) > 0 {
		// We have data and no new throttling
		status.AddCondition(types.NewCondition(
			"CPUThermalHealthy",
			types.ConditionTrue,
			"NoThermalThrottle",
			"No CPU thermal throttling detected",
		))
	}

	return nil
}

// LoadAverage represents parsed load average data from /proc/loadavg.
type LoadAverage struct {
	Load1  float64 // 1-minute load average
	Load5  float64 // 5-minute load average
	Load15 float64 // 15-minute load average
}

// parseLoadAvg reads and parses the load average from /proc/loadavg.
func (c *CPUMonitor) parseLoadAvg() (*LoadAverage, error) {
	data, err := c.fileReader.ReadFile(c.config.LoadAvgPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", c.config.LoadAvgPath, err)
	}

	// Parse load average line: "0.08 0.03 0.05 1/180 12345"
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return nil, fmt.Errorf("invalid load average format: %s", string(data))
	}

	load1, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse 1-minute load average %q: %w", fields[0], err)
	}

	load5, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse 5-minute load average %q: %w", fields[1], err)
	}

	load15, err := strconv.ParseFloat(fields[2], 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse 15-minute load average %q: %w", fields[2], err)
	}

	return &LoadAverage{
		Load1:  load1,
		Load5:  load5,
		Load15: load15,
	}, nil
}

// getCPUCount determines the number of CPU cores available.
// It first tries to parse /proc/cpuinfo, then falls back to runtime.NumCPU().
func (c *CPUMonitor) getCPUCount() (int, error) {
	// Try to read CPU count from /proc/cpuinfo
	data, err := c.fileReader.ReadFile(c.config.CPUInfoPath)
	if err == nil {
		// Count processor entries
		processorRegex := regexp.MustCompile(`^processor\s*:`)
		count := 0
		for _, line := range strings.Split(string(data), "\n") {
			if processorRegex.MatchString(line) {
				count++
			}
		}
		if count > 0 {
			return count, nil
		}
	}

	// Fallback to runtime CPU count
	count := runtime.NumCPU()
	if count <= 0 {
		return 0, fmt.Errorf("unable to determine CPU count")
	}

	return count, nil
}

// parseCPUConfig extracts CPU-specific configuration from the generic config map.
func parseCPUConfig(configMap map[string]interface{}) (*CPUMonitorConfig, error) {
	config := &CPUMonitorConfig{}

	if configMap == nil {
		return config, nil
	}

	// Parse warning load factor
	if val, exists := configMap["warningLoadFactor"]; exists {
		if f, ok := val.(float64); ok {
			config.WarningLoadFactor = f
		} else {
			return nil, fmt.Errorf("warningLoadFactor must be a number, got %T", val)
		}
	}

	// Parse critical load factor
	if val, exists := configMap["criticalLoadFactor"]; exists {
		if f, ok := val.(float64); ok {
			config.CriticalLoadFactor = f
		} else {
			return nil, fmt.Errorf("criticalLoadFactor must be a number, got %T", val)
		}
	}

	// Parse sustained high load checks
	if val, exists := configMap["sustainedHighLoadChecks"]; exists {
		if i, ok := val.(int); ok {
			config.SustainedHighLoadChecks = i
		} else if f, ok := val.(float64); ok {
			config.SustainedHighLoadChecks = int(f)
		} else {
			return nil, fmt.Errorf("sustainedHighLoadChecks must be a number, got %T", val)
		}
	}

	// Parse check thermal throttle
	if val, exists := configMap["checkThermalThrottle"]; exists {
		if b, ok := val.(bool); ok {
			config.CheckThermalThrottle = b
		} else {
			return nil, fmt.Errorf("checkThermalThrottle must be a boolean, got %T", val)
		}
	}

	// Parse check load average
	if val, exists := configMap["checkLoadAverage"]; exists {
		if b, ok := val.(bool); ok {
			config.CheckLoadAverage = b
		} else {
			return nil, fmt.Errorf("checkLoadAverage must be a boolean, got %T", val)
		}
	}

	// Parse load average path
	if val, exists := configMap["loadAvgPath"]; exists {
		if s, ok := val.(string); ok {
			config.LoadAvgPath = s
		} else {
			return nil, fmt.Errorf("loadAvgPath must be a string, got %T", val)
		}
	}

	// Parse CPU info path
	if val, exists := configMap["cpuInfoPath"]; exists {
		if s, ok := val.(string); ok {
			config.CPUInfoPath = s
		} else {
			return nil, fmt.Errorf("cpuInfoPath must be a string, got %T", val)
		}
	}

	// Parse thermal base path
	if val, exists := configMap["thermalBasePath"]; exists {
		if s, ok := val.(string); ok {
			config.ThermalBasePath = s
		} else {
			return nil, fmt.Errorf("thermalBasePath must be a string, got %T", val)
		}
	}

	return config, nil
}

// applyDefaults applies default values to the CPU monitor configuration.
func (c *CPUMonitorConfig) applyDefaults() error {
	if c.WarningLoadFactor == 0 {
		c.WarningLoadFactor = 0.8
	}
	if c.CriticalLoadFactor == 0 {
		c.CriticalLoadFactor = 1.5
	}
	if c.SustainedHighLoadChecks == 0 {
		c.SustainedHighLoadChecks = 3
	}
	if c.LoadAvgPath == "" {
		c.LoadAvgPath = "/proc/loadavg"
	}
	if c.CPUInfoPath == "" {
		c.CPUInfoPath = "/proc/cpuinfo"
	}
	if c.ThermalBasePath == "" {
		c.ThermalBasePath = "/sys/devices/system/cpu"
	}

	// Default to checking both if not explicitly configured
	if !c.CheckLoadAverage && !c.CheckThermalThrottle {
		c.CheckLoadAverage = true
		c.CheckThermalThrottle = true
	}

	return nil
}