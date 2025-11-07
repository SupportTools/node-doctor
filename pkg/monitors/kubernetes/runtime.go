// Package kubernetes provides Kubernetes component health monitoring capabilities.
package kubernetes

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/supporttools/node-doctor/pkg/monitors"
	"github.com/supporttools/node-doctor/pkg/types"
)

const (
	// Default socket paths for container runtimes
	defaultDockerSocket     = "/var/run/docker.sock"
	defaultContainerdSocket = "/run/containerd/containerd.sock"
	defaultCrioSocket       = "/var/run/crio/crio.sock"

	// Default systemd service names
	defaultDockerService     = "docker"
	defaultContainerdService = "containerd"
	defaultCrioService       = "crio"

	// Default configuration values for runtime monitor
	defaultRuntimeTimeout          = 5 * time.Second
	defaultRuntimeFailureThreshold = 3

	// Runtime type constants
	runtimeTypeAuto       = "auto"
	runtimeTypeDocker     = "docker"
	runtimeTypeContainerd = "containerd"
	runtimeTypeCrio       = "crio"
)

// RuntimeMonitorConfig holds the configuration for the container runtime monitor.
type RuntimeMonitorConfig struct {
	// RuntimeType specifies which runtime to monitor (auto, docker, containerd, crio)
	RuntimeType string `json:"runtimeType"`

	// Socket paths (can be overridden from defaults)
	DockerSocket     string `json:"dockerSocket"`
	ContainerdSocket string `json:"containerdSocket"`
	CrioSocket       string `json:"crioSocket"`

	// Check configuration
	CheckSocketConnectivity bool `json:"checkSocketConnectivity"`
	CheckSystemdStatus      bool `json:"checkSystemdStatus"`
	CheckRuntimeInfo        bool `json:"checkRuntimeInfo"`

	// Thresholds
	FailureThreshold int           `json:"failureThreshold"`
	Timeout          time.Duration `json:"timeout"`

	// Detected runtime (populated during initialization)
	detectedRuntime string
	detectedSocket  string
	detectedService string
}

// RuntimeInfo represents basic runtime information.
type RuntimeInfo struct {
	Runtime string
	Version string
}

// RuntimeClient interface abstracts container runtime health checking for testability.
type RuntimeClient interface {
	// CheckSocketConnectivity verifies that the runtime socket is accessible
	CheckSocketConnectivity(ctx context.Context) error

	// CheckSystemdStatus checks if the runtime systemd service is active
	CheckSystemdStatus(ctx context.Context) (bool, error)

	// GetRuntimeInfo retrieves basic runtime information (version, etc.)
	GetRuntimeInfo(ctx context.Context) (*RuntimeInfo, error)
}

// defaultRuntimeClient implements RuntimeClient using standard system calls.
type defaultRuntimeClient struct {
	socketPath  string
	serviceName string
	runtime     string
	timeout     time.Duration
}

// newDefaultRuntimeClient creates a new default runtime client.
func newDefaultRuntimeClient(config *RuntimeMonitorConfig) RuntimeClient {
	return &defaultRuntimeClient{
		socketPath:  config.detectedSocket,
		serviceName: config.detectedService,
		runtime:     config.detectedRuntime,
		timeout:     config.Timeout,
	}
}

// CheckSocketConnectivity verifies that the runtime socket is accessible.
func (c *defaultRuntimeClient) CheckSocketConnectivity(ctx context.Context) error {
	// Check if socket file exists
	if _, err := os.Stat(c.socketPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("socket does not exist: %s", c.socketPath)
		}
		return fmt.Errorf("failed to stat socket: %w", err)
	}

	// Attempt to connect to the socket with timeout
	dialer := &net.Dialer{
		Timeout: c.timeout,
	}

	conn, err := dialer.DialContext(ctx, "unix", c.socketPath)
	if err != nil {
		return fmt.Errorf("failed to connect to socket: %w", err)
	}
	defer conn.Close()

	return nil
}

// CheckSystemdStatus checks if the runtime systemd service is active.
func (c *defaultRuntimeClient) CheckSystemdStatus(ctx context.Context) (bool, error) {
	cmd := exec.CommandContext(ctx, "systemctl", "is-active", c.serviceName)

	output, err := cmd.CombinedOutput()
	status := strings.TrimSpace(string(output))

	// systemctl is-active returns:
	// - "active" with exit code 0 if service is running
	// - Other states (inactive, failed, etc.) with non-zero exit code
	if err != nil {
		// Check for known states
		switch status {
		case "inactive":
			return false, nil // Service is stopped (not an error)
		case "failed":
			return false, nil // Service has failed (not an error in checking)
		case "activating", "deactivating", "reloading":
			return false, fmt.Errorf("service in transitional state: %s", status)
		default:
			return false, fmt.Errorf("systemctl check failed: %w (status: %s)", err, status)
		}
	}

	return status == "active", nil
}

// GetRuntimeInfo retrieves basic runtime information.
func (c *defaultRuntimeClient) GetRuntimeInfo(ctx context.Context) (*RuntimeInfo, error) {
	// For now, we'll do a simple connectivity test
	// In the future, this could make actual API calls to get version info
	// For Docker: HTTP GET to /var/run/docker.sock/version
	// For containerd/CRI-O: gRPC calls would be needed

	// Verify socket connectivity as a basic health check
	if err := c.CheckSocketConnectivity(ctx); err != nil {
		return nil, fmt.Errorf("runtime info check failed: %w", err)
	}

	return &RuntimeInfo{
		Runtime: c.runtime,
		Version: "detected", // Placeholder - would need actual API calls
	}, nil
}

// RuntimeMonitor monitors container runtime health.
type RuntimeMonitor struct {
	*monitors.BaseMonitor

	config *RuntimeMonitorConfig
	client RuntimeClient

	// State tracking for failure threshold
	mu                  sync.Mutex
	consecutiveFailures int
	unhealthy           bool
}

// init registers the runtime monitor with the monitor registry.
func init() {
	monitors.Register(monitors.MonitorInfo{
		Type:        "kubernetes-runtime-check",
		Factory:     NewRuntimeMonitor,
		Validator:   ValidateRuntimeConfig,
		Description: "Monitors container runtime health (Docker, containerd, CRI-O)",
	})
}

// NewRuntimeMonitor creates a new container runtime monitor instance.
func NewRuntimeMonitor(ctx context.Context, config types.MonitorConfig) (types.Monitor, error) {
	// Validate configuration
	if err := ValidateRuntimeConfig(config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Parse runtime-specific configuration
	runtimeConfig, err := parseRuntimeConfig(config.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse runtime config: %w", err)
	}

	// Apply defaults
	if err := runtimeConfig.applyDefaults(); err != nil {
		return nil, fmt.Errorf("failed to apply defaults: %w", err)
	}

	// Detect runtime if auto mode
	if err := runtimeConfig.detectRuntime(); err != nil {
		return nil, fmt.Errorf("failed to detect runtime: %w", err)
	}

	// Create client
	client := newDefaultRuntimeClient(runtimeConfig)

	// Create base monitor
	baseMonitor, err := monitors.NewBaseMonitor(config.Name, config.Interval, config.Timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to create base monitor: %w", err)
	}

	// Create runtime monitor
	monitor := &RuntimeMonitor{
		BaseMonitor: baseMonitor,
		config:      runtimeConfig,
		client:      client,
	}

	// Set check function
	if err := baseMonitor.SetCheckFunc(monitor.checkRuntime); err != nil {
		return nil, fmt.Errorf("failed to set check function: %w", err)
	}

	return monitor, nil
}

// NewRuntimeMonitorWithClient creates a new container runtime monitor instance with a custom client.
// This is primarily used for testing to allow dependency injection of mock clients.
func NewRuntimeMonitorWithClient(ctx context.Context, config types.MonitorConfig, client RuntimeClient) (types.Monitor, error) {
	// Validate configuration
	if err := ValidateRuntimeConfig(config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Parse runtime-specific configuration
	runtimeConfig, err := parseRuntimeConfig(config.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse runtime config: %w", err)
	}

	// Apply defaults
	if err := runtimeConfig.applyDefaults(); err != nil {
		return nil, fmt.Errorf("failed to apply defaults: %w", err)
	}

	// Detect runtime if auto mode (even with custom client, we need proper config)
	if err := runtimeConfig.detectRuntime(); err != nil {
		return nil, fmt.Errorf("failed to detect runtime: %w", err)
	}

	// Create base monitor
	baseMonitor, err := monitors.NewBaseMonitor(config.Name, config.Interval, config.Timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to create base monitor: %w", err)
	}

	// Create runtime monitor with injected client
	monitor := &RuntimeMonitor{
		BaseMonitor: baseMonitor,
		config:      runtimeConfig,
		client:      client, // Use injected client instead of creating default
	}

	// Set check function
	if err := baseMonitor.SetCheckFunc(monitor.checkRuntime); err != nil {
		return nil, fmt.Errorf("failed to set check function: %w", err)
	}

	return monitor, nil
}

// ValidateRuntimeConfig validates the runtime monitor configuration.
func ValidateRuntimeConfig(config types.MonitorConfig) error {
	if config.Name == "" {
		return fmt.Errorf("monitor name is required")
	}

	if config.Type != "kubernetes-runtime-check" {
		return fmt.Errorf("invalid monitor type: %s (expected: kubernetes-runtime-check)", config.Type)
	}

	// Parse and validate runtime-specific config
	_, err := parseRuntimeConfig(config.Config)
	return err
}

// parseRuntimeConfig parses the runtime monitor configuration from a map.
func parseRuntimeConfig(configMap map[string]interface{}) (*RuntimeMonitorConfig, error) {
	config := &RuntimeMonitorConfig{
		RuntimeType:             runtimeTypeAuto,
		DockerSocket:            defaultDockerSocket,
		ContainerdSocket:        defaultContainerdSocket,
		CrioSocket:              defaultCrioSocket,
		CheckSocketConnectivity: true,
		CheckSystemdStatus:      true,
		CheckRuntimeInfo:        true,
		FailureThreshold:        defaultRuntimeFailureThreshold,
		Timeout:                 defaultRuntimeTimeout,
	}

	if configMap == nil {
		return config, nil // Use all defaults
	}

	// Parse runtimeType
	if v, ok := configMap["runtimeType"]; ok {
		strVal, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("runtimeType must be a string, got %T", v)
		}
		config.RuntimeType = strings.ToLower(strVal)

		// Validate runtime type
		validTypes := []string{runtimeTypeAuto, runtimeTypeDocker, runtimeTypeContainerd, runtimeTypeCrio}
		isValid := false
		for _, vt := range validTypes {
			if config.RuntimeType == vt {
				isValid = true
				break
			}
		}
		if !isValid {
			return nil, fmt.Errorf("invalid runtimeType: %s (must be one of: auto, docker, containerd, crio)", config.RuntimeType)
		}
	}

	// Parse socket paths (optional overrides)
	if v, ok := configMap["dockerSocket"]; ok {
		strVal, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("dockerSocket must be a string, got %T", v)
		}
		config.DockerSocket = strVal
	}

	if v, ok := configMap["containerdSocket"]; ok {
		strVal, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("containerdSocket must be a string, got %T", v)
		}
		config.ContainerdSocket = strVal
	}

	if v, ok := configMap["crioSocket"]; ok {
		strVal, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("crioSocket must be a string, got %T", v)
		}
		config.CrioSocket = strVal
	}

	// Parse check flags
	if v, ok := configMap["checkSocketConnectivity"]; ok {
		boolVal, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("checkSocketConnectivity must be a boolean, got %T", v)
		}
		config.CheckSocketConnectivity = boolVal
	}

	if v, ok := configMap["checkSystemdStatus"]; ok {
		boolVal, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("checkSystemdStatus must be a boolean, got %T", v)
		}
		config.CheckSystemdStatus = boolVal
	}

	if v, ok := configMap["checkRuntimeInfo"]; ok {
		boolVal, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("checkRuntimeInfo must be a boolean, got %T", v)
		}
		config.CheckRuntimeInfo = boolVal
	}

	// Parse failure threshold
	if v, ok := configMap["failureThreshold"]; ok {
		switch val := v.(type) {
		case int:
			config.FailureThreshold = val
		case float64:
			config.FailureThreshold = int(val)
		default:
			return nil, fmt.Errorf("failureThreshold must be an integer, got %T", v)
		}

		if config.FailureThreshold < 1 {
			return nil, fmt.Errorf("failureThreshold must be at least 1, got %d", config.FailureThreshold)
		}
	}

	// Parse timeout
	if v, ok := configMap["timeout"]; ok {
		timeout, err := parseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid timeout: %w", err)
		}
		config.Timeout = timeout
	}

	return config, nil
}

// applyDefaults ensures all configuration fields have valid values.
func (c *RuntimeMonitorConfig) applyDefaults() error {
	// Validate at least one check is enabled
	if !c.CheckSocketConnectivity && !c.CheckSystemdStatus && !c.CheckRuntimeInfo {
		return fmt.Errorf("at least one check must be enabled")
	}

	return nil
}

// detectRuntime attempts to auto-detect the container runtime on the system.
func (c *RuntimeMonitorConfig) detectRuntime() error {
	// If already detected (e.g., in tests), skip detection
	if c.detectedRuntime != "" && c.detectedSocket != "" && c.detectedService != "" {
		return nil
	}

	// If runtime is explicitly specified, verify it
	if c.RuntimeType != runtimeTypeAuto {
		switch c.RuntimeType {
		case runtimeTypeDocker:
			c.detectedRuntime = runtimeTypeDocker
			c.detectedSocket = c.DockerSocket
			c.detectedService = defaultDockerService
		case runtimeTypeContainerd:
			c.detectedRuntime = runtimeTypeContainerd
			c.detectedSocket = c.ContainerdSocket
			c.detectedService = defaultContainerdService
		case runtimeTypeCrio:
			c.detectedRuntime = runtimeTypeCrio
			c.detectedSocket = c.CrioSocket
			c.detectedService = defaultCrioService
		}

		// Verify the specified runtime's socket exists
		if _, err := os.Stat(c.detectedSocket); err != nil {
			return fmt.Errorf("specified runtime %s socket not found at %s: %w",
				c.RuntimeType, c.detectedSocket, err)
		}

		return nil
	}

	// Auto-detect: try each runtime in order of popularity
	runtimes := []struct {
		name    string
		socket  string
		service string
	}{
		{runtimeTypeDocker, c.DockerSocket, defaultDockerService},
		{runtimeTypeContainerd, c.ContainerdSocket, defaultContainerdService},
		{runtimeTypeCrio, c.CrioSocket, defaultCrioService},
	}

	for _, rt := range runtimes {
		if _, err := os.Stat(rt.socket); err == nil {
			// Socket exists, use this runtime
			c.detectedRuntime = rt.name
			c.detectedSocket = rt.socket
			c.detectedService = rt.service
			return nil
		}
	}

	return fmt.Errorf("no container runtime detected (checked: docker, containerd, crio)")
}

// checkRuntime performs the container runtime health check.
func (m *RuntimeMonitor) checkRuntime(ctx context.Context) (*types.Status, error) {
	status := types.NewStatus(m.GetName())

	// Perform enabled checks
	healthy := true
	var errors []string

	// Check 1: Socket connectivity
	if m.config.CheckSocketConnectivity {
		if err := m.client.CheckSocketConnectivity(ctx); err != nil {
			healthy = false
			errors = append(errors, fmt.Sprintf("socket connectivity: %v", err))
			status.AddEvent(types.NewEvent(
				types.EventWarning,
				"RuntimeSocketUnreachable",
				fmt.Sprintf("Container runtime socket is not accessible: %v", err),
			))
		}
	}

	// Check 2: Systemd status
	if m.config.CheckSystemdStatus {
		active, err := m.client.CheckSystemdStatus(ctx)
		if err != nil {
			healthy = false
			errors = append(errors, fmt.Sprintf("systemd status: %v", err))
			status.AddEvent(types.NewEvent(
				types.EventWarning,
				"RuntimeSystemdCheckFailed",
				fmt.Sprintf("Failed to check systemd status: %v", err),
			))
		} else if !active {
			healthy = false
			errors = append(errors, "systemd service is not active")
			status.AddEvent(types.NewEvent(
				types.EventWarning,
				"RuntimeSystemdInactive",
				fmt.Sprintf("Container runtime systemd service (%s) is not active", m.config.detectedService),
			))
		}
	}

	// Check 3: Runtime info
	if m.config.CheckRuntimeInfo {
		if _, err := m.client.GetRuntimeInfo(ctx); err != nil {
			healthy = false
			errors = append(errors, fmt.Sprintf("runtime info: %v", err))
			status.AddEvent(types.NewEvent(
				types.EventWarning,
				"RuntimeInfoFailed",
				fmt.Sprintf("Failed to retrieve runtime info: %v", err),
			))
		}
	}

	// Update failure tracking and report conditions
	m.updateFailureTracking(healthy, status)

	// If all checks passed, add success event
	if healthy {
		status.AddEvent(types.NewEvent(
			types.EventInfo,
			"RuntimeHealthy",
			fmt.Sprintf("Container runtime (%s) is healthy", m.config.detectedRuntime),
		))
	}

	return status, nil
}

// updateFailureTracking updates the failure counter and manages conditions.
func (m *RuntimeMonitor) updateFailureTracking(healthy bool, status *types.Status) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !healthy {
		m.consecutiveFailures++

		// Check if we've reached the failure threshold
		if m.consecutiveFailures >= m.config.FailureThreshold {
			// Report ContainerRuntimeUnhealthy condition
			m.unhealthy = true
			status.AddCondition(types.NewCondition(
				"ContainerRuntimeUnhealthy",
				types.ConditionTrue,
				"HealthCheckFailed",
				fmt.Sprintf("Container runtime (%s) has failed health checks for %d consecutive attempts",
					m.config.detectedRuntime, m.consecutiveFailures),
			))
		}
	} else {
		// Check if we're recovering from failures
		wasUnhealthy := m.unhealthy
		previousFailures := m.consecutiveFailures

		// Reset counters
		m.consecutiveFailures = 0
		m.unhealthy = false

		// If we were unhealthy, report recovery
		if wasUnhealthy {
			status.AddEvent(types.NewEvent(
				types.EventInfo,
				"RuntimeRecovered",
				fmt.Sprintf("Container runtime (%s) has recovered after %d consecutive failures",
					m.config.detectedRuntime, previousFailures),
			))

			// Clear the unhealthy condition
			status.AddCondition(types.NewCondition(
				"ContainerRuntimeUnhealthy",
				types.ConditionFalse,
				"HealthCheckPassed",
				fmt.Sprintf("Container runtime (%s) is healthy", m.config.detectedRuntime),
			))
		}
	}
}

// parseDuration parses a duration from various input types.
func parseDuration(v interface{}) (time.Duration, error) {
	switch val := v.(type) {
	case string:
		return time.ParseDuration(val)
	case int:
		return time.Duration(val) * time.Second, nil
	case float64:
		return time.Duration(val * float64(time.Second)), nil
	default:
		return 0, fmt.Errorf("invalid duration type: %T", v)
	}
}
