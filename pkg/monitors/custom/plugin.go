package custom

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/supporttools/node-doctor/pkg/monitors"
	"github.com/supporttools/node-doctor/pkg/types"
)

const (
	// Default configuration values
	defaultOutputFormat     = OutputFormatSimple
	defaultFailureThreshold = 3
	defaultAPITimeout       = 10 * time.Second
)

// PluginMonitorConfig holds the configuration for the plugin monitor
type PluginMonitorConfig struct {
	PluginPath       string            // Absolute path to the plugin executable
	Args             []string          // Arguments to pass to the plugin
	OutputFormat     OutputFormat      // Format of plugin output (json or simple)
	FailureThreshold int               // Number of consecutive failures before reporting unhealthy
	APITimeout       time.Duration     // Timeout for plugin execution
	Env              map[string]string // Additional environment variables
}

// PluginMonitor monitors health by executing custom plugins
type PluginMonitor struct {
	name   string
	config *PluginMonitorConfig

	// Executor and parser
	executor PluginExecutor
	parser   *OutputParser

	// Thread-safe state tracking
	mu                  sync.Mutex
	consecutiveFailures int
	lastStatus          PluginStatus
	unhealthyReported   bool

	// BaseMonitor for lifecycle management
	*monitors.BaseMonitor
}

// NewPluginMonitor creates a new plugin monitor
func NewPluginMonitor(ctx context.Context, config types.MonitorConfig) (types.Monitor, error) {
	// Parse plugin-specific configuration
	pluginConfig, err := parsePluginConfig(config.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse plugin config: %w", err)
	}

	// Apply defaults
	pluginConfig.applyDefaults()

	// Create executor
	executor := NewPluginExecutor(pluginConfig.APITimeout)

	return NewPluginMonitorWithExecutor(ctx, config, pluginConfig, executor)
}

// NewPluginMonitorWithExecutor creates a plugin monitor with a custom executor (for testing)
func NewPluginMonitorWithExecutor(ctx context.Context, config types.MonitorConfig, pluginConfig *PluginMonitorConfig, executor PluginExecutor) (types.Monitor, error) {
	// Create base monitor
	baseMonitor, err := monitors.NewBaseMonitor(config.Name, config.Interval, config.Timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to create base monitor: %w", err)
	}

	// Create parser
	parser := NewOutputParser(pluginConfig.OutputFormat)

	// Create plugin monitor
	monitor := &PluginMonitor{
		name:        config.Name,
		config:      pluginConfig,
		executor:    executor,
		parser:      parser,
		BaseMonitor: baseMonitor,
	}

	// Initialize state
	monitor.lastStatus = PluginStatusUnknown

	// Set the check function
	if err := baseMonitor.SetCheckFunc(monitor.checkPlugin); err != nil {
		return nil, fmt.Errorf("failed to set check function: %w", err)
	}

	return monitor, nil
}

// checkPlugin executes the plugin and evaluates the result
func (m *PluginMonitor) checkPlugin(ctx context.Context) (*types.Status, error) {
	status := types.NewStatus(m.GetName())

	// Prepare environment variables
	env := m.prepareEnvironment()

	// Execute the plugin
	stdout, stderr, exitCode, err := m.executor.Execute(ctx, m.config.PluginPath, m.config.Args, env)

	// Handle execution errors (timeout, file not found, etc.)
	if err != nil {
		m.trackFailure(status, fmt.Sprintf("Plugin execution error: %v", err))
		return status, nil
	}

	// Parse the output
	result, parseErr := m.parser.Parse(stdout, stderr, exitCode)
	if parseErr != nil {
		m.trackFailure(status, fmt.Sprintf("Failed to parse plugin output: %v", parseErr))
		return status, nil
	}

	// Evaluate the result
	m.evaluateResult(result, status)

	return status, nil
}

// evaluateResult evaluates the plugin result and updates status
func (m *PluginMonitor) evaluateResult(result *PluginResult, status *types.Status) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Update last status
	previousStatus := m.lastStatus
	m.lastStatus = result.Status

	// Reset consecutive failures on healthy status
	if result.Status == PluginStatusHealthy {
		// Check for recovery
		if m.unhealthyReported {
			event := types.NewEvent(
				types.EventInfo,
				"PluginRecovered",
				fmt.Sprintf("Plugin check recovered: %s", result.Message),
			)
			status.AddEvent(event)

			// Clear unhealthy condition
			condition := types.NewCondition(
				"PluginUnhealthy",
				types.ConditionFalse,
				"PluginHealthy",
				"Plugin is now healthy",
			)
			status.AddCondition(condition)

			m.unhealthyReported = false
		}

		m.consecutiveFailures = 0

		// Add healthy condition
		condition := types.NewCondition(
			"PluginHealthy",
			types.ConditionTrue,
			"PluginCheckPassed",
			fmt.Sprintf("Plugin check passed: %s", result.Message),
		)
		status.AddCondition(condition)
		return
	}

	// Handle warning status
	if result.Status == PluginStatusWarning {
		// Emit warning event only on state change
		if previousStatus != PluginStatusWarning {
			event := types.NewEvent(
				types.EventWarning,
				"PluginWarning",
				fmt.Sprintf("Plugin check warning: %s", result.Message),
			)
			status.AddEvent(event)
		}

		// Add warning condition
		condition := types.NewCondition(
			"PluginWarning",
			types.ConditionTrue,
			"WarningThresholdExceeded",
			fmt.Sprintf("Plugin check warning: %s", result.Message),
		)
		status.AddCondition(condition)

		m.consecutiveFailures = 0 // Don't count warnings as failures
		return
	}

	// Handle critical status
	if result.Status == PluginStatusCritical {
		// Emit critical event only on state change
		if previousStatus != PluginStatusCritical {
			event := types.NewEvent(
				types.EventError,
				"PluginCritical",
				fmt.Sprintf("Plugin check critical: %s", result.Message),
			)
			status.AddEvent(event)
		}

		// Track failure for threshold
		m.consecutiveFailures++

		// Check if we've reached the failure threshold
		if m.consecutiveFailures >= m.config.FailureThreshold {
			if !m.unhealthyReported {
				// Report unhealthy condition
				condition := types.NewCondition(
					"PluginUnhealthy",
					types.ConditionTrue,
					"PluginCritical",
					fmt.Sprintf("Plugin check critical after %d consecutive failures: %s", m.consecutiveFailures, result.Message),
				)
				status.AddCondition(condition)
				m.unhealthyReported = true
			} else {
				// Update existing critical condition
				condition := types.NewCondition(
					"PluginCritical",
					types.ConditionTrue,
					"CriticalThresholdExceeded",
					fmt.Sprintf("Plugin check critical (failure %d/%d): %s", m.consecutiveFailures, m.config.FailureThreshold, result.Message),
				)
				status.AddCondition(condition)
			}
		} else {
			// Still within threshold - add condition but not unhealthy
			condition := types.NewCondition(
				"PluginCritical",
				types.ConditionTrue,
				"CriticalThresholdNotYetExceeded",
				fmt.Sprintf("Plugin check critical (failure %d/%d): %s", m.consecutiveFailures, m.config.FailureThreshold, result.Message),
			)
			status.AddCondition(condition)
		}
		return
	}

	// Handle unknown status (treat as failure)
	m.trackFailure(status, fmt.Sprintf("Plugin check unknown: %s", result.Message))
}

// trackFailure tracks consecutive failures and reports unhealthy condition after threshold
func (m *PluginMonitor) trackFailure(status *types.Status, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.consecutiveFailures++
	m.lastStatus = PluginStatusUnknown

	// Emit failure event
	event := types.NewEvent(
		types.EventError,
		"PluginCheckFailed",
		message,
	)
	status.AddEvent(event)

	// Check if we've reached the failure threshold
	if m.consecutiveFailures >= m.config.FailureThreshold {
		if !m.unhealthyReported {
			// Report unhealthy condition
			condition := types.NewCondition(
				"PluginUnhealthy",
				types.ConditionTrue,
				"PluginCheckFailed",
				fmt.Sprintf("Plugin check failed after %d consecutive failures: %s", m.consecutiveFailures, message),
			)
			status.AddCondition(condition)
			m.unhealthyReported = true
		} else {
			// Update failure condition
			condition := types.NewCondition(
				"PluginCheckFailed",
				types.ConditionTrue,
				"CheckError",
				fmt.Sprintf("Plugin check failed (failure %d/%d): %s", m.consecutiveFailures, m.config.FailureThreshold, message),
			)
			status.AddCondition(condition)
		}
	} else {
		// Still within threshold
		condition := types.NewCondition(
			"PluginCheckFailed",
			types.ConditionTrue,
			"CheckError",
			fmt.Sprintf("Plugin check failed (failure %d/%d): %s", m.consecutiveFailures, m.config.FailureThreshold, message),
		)
		status.AddCondition(condition)
	}
}

// prepareEnvironment prepares environment variables for plugin execution
func (m *PluginMonitor) prepareEnvironment() map[string]string {
	env := make(map[string]string)

	// Add standard environment variables from DaemonSet
	if nodeName := os.Getenv("NODE_NAME"); nodeName != "" {
		env["NODE_NAME"] = nodeName
	}
	if podName := os.Getenv("POD_NAME"); podName != "" {
		env["POD_NAME"] = podName
	}
	if podNamespace := os.Getenv("POD_NAMESPACE"); podNamespace != "" {
		env["POD_NAMESPACE"] = podNamespace
	}
	if podIP := os.Getenv("POD_IP"); podIP != "" {
		env["POD_IP"] = podIP
	}

	// Add custom environment variables from configuration
	for key, value := range m.config.Env {
		env[key] = value
	}

	return env
}

// parsePluginConfig parses the plugin-specific configuration
func parsePluginConfig(configMap map[string]interface{}) (*PluginMonitorConfig, error) {
	config := &PluginMonitorConfig{
		Env: make(map[string]string),
	}

	// Parse pluginPath (required)
	if val, ok := configMap["pluginPath"]; ok {
		strVal, ok := val.(string)
		if !ok {
			return nil, fmt.Errorf("pluginPath must be a string, got %T", val)
		}
		config.PluginPath = strVal
	} else {
		return nil, fmt.Errorf("pluginPath is required")
	}

	// Parse args (optional, array of strings)
	if val, ok := configMap["args"]; ok {
		// Handle both []string and []interface{} from YAML
		switch v := val.(type) {
		case []string:
			config.Args = v
		case []interface{}:
			args := make([]string, len(v))
			for i, arg := range v {
				strArg, ok := arg.(string)
				if !ok {
					return nil, fmt.Errorf("args[%d] must be a string, got %T", i, arg)
				}
				args[i] = strArg
			}
			config.Args = args
		default:
			return nil, fmt.Errorf("args must be an array of strings, got %T", val)
		}
	}

	// Parse outputFormat (optional, string)
	if val, ok := configMap["outputFormat"]; ok {
		strVal, ok := val.(string)
		if !ok {
			return nil, fmt.Errorf("outputFormat must be a string, got %T", val)
		}
		format := OutputFormat(strVal)
		if format != OutputFormatJSON && format != OutputFormatSimple {
			return nil, fmt.Errorf("outputFormat must be 'json' or 'simple', got: %s", strVal)
		}
		config.OutputFormat = format
	}

	// Parse failureThreshold (optional, int)
	if val, ok := configMap["failureThreshold"]; ok {
		switch v := val.(type) {
		case int:
			config.FailureThreshold = v
		case float64:
			config.FailureThreshold = int(v)
		default:
			return nil, fmt.Errorf("failureThreshold must be an integer, got %T", val)
		}

		if config.FailureThreshold < 1 {
			return nil, fmt.Errorf("failureThreshold must be at least 1, got: %d", config.FailureThreshold)
		}
	}

	// Parse apiTimeout (optional, duration)
	if val, ok := configMap["apiTimeout"]; ok {
		switch v := val.(type) {
		case string:
			duration, err := time.ParseDuration(v)
			if err != nil {
				return nil, fmt.Errorf("failed to parse apiTimeout: %w", err)
			}
			config.APITimeout = duration
		case float64:
			config.APITimeout = time.Duration(v) * time.Second
		case int:
			config.APITimeout = time.Duration(v) * time.Second
		default:
			return nil, fmt.Errorf("apiTimeout must be a duration string or number, got %T", val)
		}

		if config.APITimeout <= 0 {
			return nil, fmt.Errorf("apiTimeout must be positive, got: %v", config.APITimeout)
		}
	}

	// Parse env (optional, map of strings)
	if val, ok := configMap["env"]; ok {
		envMap, ok := val.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("env must be a map, got %T", val)
		}

		for key, value := range envMap {
			strVal, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("env[%s] must be a string, got %T", key, value)
			}
			config.Env[key] = strVal
		}
	}

	return config, nil
}

// applyDefaults applies default values to optional configuration fields
func (c *PluginMonitorConfig) applyDefaults() {
	if c.OutputFormat == "" {
		c.OutputFormat = defaultOutputFormat
	}

	if c.FailureThreshold == 0 {
		c.FailureThreshold = defaultFailureThreshold
	}

	if c.APITimeout == 0 {
		c.APITimeout = defaultAPITimeout
	}

	if c.Env == nil {
		c.Env = make(map[string]string)
	}
}

// ValidatePluginConfig validates the plugin monitor configuration
func ValidatePluginConfig(config types.MonitorConfig) error {
	// Parse the configuration
	pluginConfig, err := parsePluginConfig(config.Config)
	if err != nil {
		return err
	}

	// Apply defaults for validation
	pluginConfig.applyDefaults()

	// Validate plugin path security
	if err := validatePluginPath(pluginConfig.PluginPath); err != nil {
		return fmt.Errorf("invalid plugin path: %w", err)
	}

	// Note: We don't validate if the file exists here because it might not be
	// available at validation time (e.g., mounted volume not yet available)

	return nil
}

// init registers the plugin monitor
func init() {
	monitors.MustRegister(monitors.MonitorInfo{
		Type:        "custom-plugin",
		Factory:     NewPluginMonitor,
		Validator:   ValidatePluginConfig,
		Description: "Executes custom monitoring plugins (scripts or binaries)",
	})
}
