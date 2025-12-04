// Package kubernetes provides Kubernetes component health monitoring capabilities.
package kubernetes

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/supporttools/node-doctor/pkg/monitors"
	"github.com/supporttools/node-doctor/pkg/types"
)

const (
	// Default configuration values for API server monitor
	defaultAPIServerEndpoint         = "https://kubernetes.default.svc.cluster.local"
	defaultAPIServerTimeout          = 10 * time.Second
	defaultAPIServerLatencyThreshold = 2 * time.Second
	defaultAPIServerFailureThreshold = 3
)

// APIServerMonitorConfig holds the configuration for the API server monitor.
type APIServerMonitorConfig struct {
	// Endpoint is the Kubernetes API server endpoint
	// Default: https://kubernetes.default.svc.cluster.local (in-cluster)
	Endpoint string `json:"endpoint"`

	// LatencyThreshold is the maximum acceptable API call latency
	// API calls exceeding this threshold generate APIServerSlow events
	// Default: 2 seconds
	LatencyThreshold time.Duration `json:"latencyThreshold"`

	// CheckVersion enables API server version checking
	// Uses GET /version endpoint to verify API server responsiveness
	// Default: true
	CheckVersion bool `json:"checkVersion"`

	// CheckAuth enables authentication verification
	// Monitors for 401/403 responses indicating auth issues
	// Default: true
	CheckAuth bool `json:"checkAuth"`

	// FailureThreshold is the number of consecutive failures before reporting APIServerUnreachable
	// Prevents false positives from transient network issues
	// Default: 3
	FailureThreshold int `json:"failureThreshold"`

	// HTTPTimeout is the timeout for API requests
	// Default: 10 seconds
	HTTPTimeout time.Duration `json:"httpTimeout"`
}

// APIServerMetrics contains API server health metrics.
type APIServerMetrics struct {
	// Latency is the duration of the API call in seconds
	Latency time.Duration

	// Version is the API server version information
	Version *version.Info

	// StatusCode is the HTTP status code from the health check
	StatusCode int

	// Authenticated indicates whether the request was authenticated
	Authenticated bool

	// RateLimited indicates whether the request was rate limited (429)
	RateLimited bool
}

// APIServerClient interface abstracts API server health checking for testability.
type APIServerClient interface {
	// CheckHealth performs a health check against the API server
	// Returns metrics including latency and status code
	CheckHealth(ctx context.Context) (*APIServerMetrics, error)

	// GetVersion retrieves the API server version information
	GetVersion(ctx context.Context) (*version.Info, error)
}

// defaultAPIServerClient implements APIServerClient using Kubernetes client-go.
type defaultAPIServerClient struct {
	clientset kubernetes.Interface
	timeout   time.Duration
}

// newDefaultAPIServerClient creates a new default API server client.
// Uses in-cluster configuration (ServiceAccount token) for authentication.
func newDefaultAPIServerClient(config *APIServerMonitorConfig) (APIServerClient, error) {
	// Use in-cluster config (ServiceAccount token from mounted secret)
	restConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to create in-cluster config: %w", err)
	}

	// Override endpoint if specified in config
	if config.Endpoint != "" && config.Endpoint != defaultAPIServerEndpoint {
		restConfig.Host = config.Endpoint
	}

	// Set timeout
	restConfig.Timeout = config.HTTPTimeout

	// Create clientset
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes clientset: %w", err)
	}

	return &defaultAPIServerClient{
		clientset: clientset,
		timeout:   config.HTTPTimeout,
	}, nil
}

// CheckHealth performs a health check against the API server.
// Note: Kubernetes client-go doesn't support context-based cancellation directly,
// but respects the timeout configured in restConfig.Timeout.
func (c *defaultAPIServerClient) CheckHealth(ctx context.Context) (*APIServerMetrics, error) {
	// Check if context is already cancelled before making the API call
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	metrics := &APIServerMetrics{
		Authenticated: true, // Assume authenticated unless we get 401/403
	}

	// Measure latency for the health check
	startTime := time.Now()

	// Use discovery client to check health (lightweight operation)
	// GET /version is a simple endpoint that verifies API server responsiveness
	// Note: ServerVersion() doesn't accept context, but HTTP client timeout is configured
	versionInfo, err := c.clientset.Discovery().ServerVersion()
	latency := time.Since(startTime)
	metrics.Latency = latency

	if err != nil {
		// Check for specific error types
		if isAuthError(err) {
			metrics.StatusCode = 401 // or 403
			metrics.Authenticated = false
			return metrics, fmt.Errorf("authentication failed: %w", err)
		}
		if isRateLimitError(err) {
			metrics.StatusCode = 429
			metrics.RateLimited = true
			return metrics, fmt.Errorf("rate limited: %w", err)
		}

		// Generic error - API server unreachable
		return metrics, fmt.Errorf("API server health check failed: %w", err)
	}

	metrics.StatusCode = 200
	metrics.Version = versionInfo
	return metrics, nil
}

// GetVersion retrieves the API server version information.
func (c *defaultAPIServerClient) GetVersion(ctx context.Context) (*version.Info, error) {
	return c.clientset.Discovery().ServerVersion()
}

// isAuthError checks if an error indicates authentication failure (401/403).
func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	// Check error message for common auth failure patterns (case-insensitive)
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "unauthorized") ||
		strings.Contains(errMsg, "forbidden") ||
		strings.Contains(errMsg, "401") ||
		strings.Contains(errMsg, "403")
}

// isRateLimitError checks if an error indicates rate limiting (429).
func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "rate limit") ||
		strings.Contains(errMsg, "429") ||
		strings.Contains(errMsg, "too many requests")
}

// sanitizeError removes potentially sensitive information from error messages.
// Prevents leaking authentication tokens, internal URLs, or other sensitive data in logs.
func sanitizeError(err error) string {
	if err == nil {
		return ""
	}

	// Convert to lowercase for case-insensitive matching
	errMsg := strings.ToLower(err.Error())

	// Return generic categorized errors instead of full details
	if strings.Contains(errMsg, "unauthorized") || strings.Contains(errMsg, "401") {
		return "authentication failed (401 Unauthorized)"
	}
	if strings.Contains(errMsg, "forbidden") || strings.Contains(errMsg, "403") {
		return "access denied (403 Forbidden)"
	}
	if strings.Contains(errMsg, "rate limit") || strings.Contains(errMsg, "429") || strings.Contains(errMsg, "too many requests") {
		return "rate limit exceeded (429 Too Many Requests)"
	}
	if strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "deadline exceeded") || strings.Contains(errMsg, "context deadline exceeded") {
		return "request timeout"
	}
	if strings.Contains(errMsg, "connection refused") || strings.Contains(errMsg, "connect:") {
		return "connection refused"
	}
	if strings.Contains(errMsg, "no route to host") || strings.Contains(errMsg, "network is unreachable") {
		return "network unreachable"
	}
	if strings.Contains(errMsg, "certificate") || strings.Contains(errMsg, "tls") || strings.Contains(errMsg, "x509") {
		return "TLS/certificate error"
	}

	// Generic error for anything else
	return "API server connection failed"
}

// APIServerMonitor monitors Kubernetes API server health.
// It uses the BaseMonitor pattern for lifecycle management and channel-based status reporting.
type APIServerMonitor struct {
	*monitors.BaseMonitor

	// Configuration
	config *APIServerMonitorConfig
	client APIServerClient

	// Consecutive failure tracking
	mu                  sync.Mutex
	consecutiveFailures int
	lastFailureTime     time.Time
	unhealthy           bool // Track if we've reported APIServerUnreachable
}

// NewAPIServerMonitor creates a new API server monitor from a MonitorConfig.
func NewAPIServerMonitor(ctx context.Context, config types.MonitorConfig) (types.Monitor, error) {
	// Validate basic monitor config
	if err := ValidateAPIServerConfig(config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Parse API server specific configuration
	apiConfig, err := ParseAPIServerConfig(config.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse API server config: %w", err)
	}

	// Apply defaults
	if err := apiConfig.applyDefaults(); err != nil {
		return nil, fmt.Errorf("failed to apply defaults: %w", err)
	}

	// Create API server client
	client, err := newDefaultAPIServerClient(apiConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create API server client: %w", err)
	}

	// Create base monitor
	baseMonitor, err := monitors.NewBaseMonitor(config.Name, config.Interval, config.Timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to create base monitor: %w", err)
	}

	// Create API server monitor
	monitor := &APIServerMonitor{
		BaseMonitor: baseMonitor,
		config:      apiConfig,
		client:      client,
	}

	// Set the check function
	if err := baseMonitor.SetCheckFunc(monitor.checkAPIServer); err != nil {
		return nil, fmt.Errorf("failed to set check function: %w", err)
	}

	return monitor, nil
}

// checkAPIServer performs the actual API server health check.
// This function is called periodically by BaseMonitor.
func (m *APIServerMonitor) checkAPIServer(ctx context.Context) (*types.Status, error) {
	// Perform health check
	metrics, err := m.client.CheckHealth(ctx)

	// Track consecutive failures
	m.mu.Lock()
	defer m.mu.Unlock()

	status := &types.Status{
		Source:    m.GetName(),
		Timestamp: time.Now(),
	}

	if err != nil {
		// Health check failed
		m.consecutiveFailures++
		m.lastFailureTime = time.Now()

		// Check if we've exceeded failure threshold
		if m.consecutiveFailures >= m.config.FailureThreshold && !m.unhealthy {
			// Report APIServerUnreachable condition
			m.unhealthy = true
			status.Conditions = append(status.Conditions, types.Condition{
				Type:       "APIServerUnreachable",
				Status:     "True",
				Reason:     "APIServerUnreachable",
				Message:    fmt.Sprintf("API server unreachable after %d consecutive failures: %s", m.consecutiveFailures, sanitizeError(err)),
				Transition: time.Now(),
			})
			// Also report APIServerReachable=False
			status.Conditions = append(status.Conditions, types.Condition{
				Type:       "APIServerReachable",
				Status:     "False",
				Reason:     "APIServerUnreachable",
				Message:    fmt.Sprintf("API server unreachable: %s", sanitizeError(err)),
				Transition: time.Now(),
			})
		}

		// Always create an event for failures (even before threshold)
		severity := types.EventWarning
		if m.consecutiveFailures >= m.config.FailureThreshold {
			severity = types.EventError
		}

		status.Events = append(status.Events, types.Event{
			Severity: severity,
			Reason:   "APIServerCheckFailed",
			Message:  fmt.Sprintf("API server health check failed (attempt %d/%d): %s", m.consecutiveFailures, m.config.FailureThreshold, sanitizeError(err)),
		})

		// Check for specific failure types
		if metrics != nil {
			if !metrics.Authenticated {
				status.Events = append(status.Events, types.Event{
					Severity: types.EventError,
					Reason:   "APIServerAuthFailure",
					Message:  "API server authentication failed (401/403)",
				})
			}
			if metrics.RateLimited {
				status.Events = append(status.Events, types.Event{
					Severity: types.EventWarning,
					Reason:   "APIServerRateLimited",
					Message:  "API server rate limit detected (429)",
				})
			}
		}

		return status, nil
	}

	// Health check succeeded
	wasUnhealthy := m.unhealthy

	// Reset failure tracking
	m.consecutiveFailures = 0
	m.unhealthy = false

	// Always report APIServerReachable=True when healthy
	status.Conditions = append(status.Conditions, types.Condition{
		Type:       "APIServerReachable",
		Status:     "True",
		Reason:     "APIServerHealthy",
		Message:    fmt.Sprintf("API server is reachable (latency: %.2fms)", float64(metrics.Latency.Microseconds())/1000.0),
		Transition: time.Now(),
	})

	// If we were previously unhealthy, report recovery
	if wasUnhealthy {
		status.Conditions = append(status.Conditions, types.Condition{
			Type:       "APIServerUnreachable",
			Status:     "False",
			Reason:     "APIServerHealthy",
			Message:    "API server is now reachable and responding normally",
			Transition: time.Now(),
		})

		status.Events = append(status.Events, types.Event{
			Severity: types.EventInfo,
			Reason:   "APIServerRecovered",
			Message:  "API server health check succeeded - recovered from previous failures",
		})
	}

	// Check latency threshold and report condition only when there's a problem
	// Note: Only emit APIServerLatencyHigh=True when latency exceeds threshold
	// Do not emit APIServerLatencyHigh=False to avoid false "problems" in the detector
	if metrics.Latency > m.config.LatencyThreshold {
		status.Conditions = append(status.Conditions, types.Condition{
			Type:       "APIServerLatencyHigh",
			Status:     "True",
			Reason:     "LatencyExceeded",
			Message:    fmt.Sprintf("API server latency %.2fs exceeds threshold %.2fs", metrics.Latency.Seconds(), m.config.LatencyThreshold.Seconds()),
			Transition: time.Now(),
		})
		status.Events = append(status.Events, types.Event{
			Severity: types.EventWarning,
			Reason:   "APIServerSlow",
			Message:  fmt.Sprintf("API server latency %.2fs exceeds threshold %.2fs", metrics.Latency.Seconds(), m.config.LatencyThreshold.Seconds()),
		})
	}
	// Note: Latency info is already captured in APIServerReachable message above

	// Set API server latency metrics for Prometheus export
	status.SetLatencyMetrics(&types.LatencyMetrics{
		APIServer: &types.APIServerLatency{
			LatencyMs: float64(metrics.Latency.Microseconds()) / 1000.0,
			Reachable: true,
		},
	})

	return status, nil
}

// ParseAPIServerConfig parses API server configuration from a generic config map.
func ParseAPIServerConfig(configMap map[string]interface{}) (*APIServerMonitorConfig, error) {
	config := &APIServerMonitorConfig{
		CheckVersion: true, // Default to true
		CheckAuth:    true, // Default to true
	}

	// Parse endpoint
	if endpoint, ok := configMap["endpoint"].(string); ok {
		config.Endpoint = endpoint
	}

	// Parse latencyThreshold (can be string duration or number in seconds)
	if threshold, ok := configMap["latencyThreshold"].(string); ok {
		duration, err := time.ParseDuration(threshold)
		if err != nil {
			return nil, fmt.Errorf("invalid latencyThreshold format: %w", err)
		}
		config.LatencyThreshold = duration
	} else if threshold, ok := configMap["latencyThreshold"].(float64); ok {
		config.LatencyThreshold = time.Duration(threshold * float64(time.Second))
	} else if threshold, ok := configMap["latencyThreshold"].(int); ok {
		config.LatencyThreshold = time.Duration(threshold) * time.Second
	}

	// Parse checkVersion
	if checkVersion, ok := configMap["checkVersion"].(bool); ok {
		config.CheckVersion = checkVersion
	}

	// Parse checkAuth
	if checkAuth, ok := configMap["checkAuth"].(bool); ok {
		config.CheckAuth = checkAuth
	}

	// Parse failureThreshold
	if threshold, ok := configMap["failureThreshold"].(int); ok {
		config.FailureThreshold = threshold
	} else if threshold, ok := configMap["failureThreshold"].(float64); ok {
		config.FailureThreshold = int(threshold)
	}

	// Parse httpTimeout (can be string duration or number in seconds)
	if timeout, ok := configMap["httpTimeout"].(string); ok {
		duration, err := time.ParseDuration(timeout)
		if err != nil {
			return nil, fmt.Errorf("invalid httpTimeout format: %w", err)
		}
		config.HTTPTimeout = duration
	} else if timeout, ok := configMap["httpTimeout"].(float64); ok {
		config.HTTPTimeout = time.Duration(timeout * float64(time.Second))
	} else if timeout, ok := configMap["httpTimeout"].(int); ok {
		config.HTTPTimeout = time.Duration(timeout) * time.Second
	}

	return config, nil
}

// applyDefaults applies default values to unset configuration fields.
func (c *APIServerMonitorConfig) applyDefaults() error {
	if c.Endpoint == "" {
		c.Endpoint = defaultAPIServerEndpoint
	}

	if c.LatencyThreshold == 0 {
		c.LatencyThreshold = defaultAPIServerLatencyThreshold
	}

	if c.FailureThreshold == 0 {
		c.FailureThreshold = defaultAPIServerFailureThreshold
	}

	if c.HTTPTimeout == 0 {
		c.HTTPTimeout = defaultAPIServerTimeout
	}

	// Validate timeout is reasonable relative to latency threshold
	// HTTPTimeout can be larger since it includes connection establishment, TLS handshake, etc.
	if c.HTTPTimeout > c.LatencyThreshold*5 {
		return fmt.Errorf("httpTimeout (%v) should not be more than 5x latencyThreshold (%v)", c.HTTPTimeout, c.LatencyThreshold)
	}

	return nil
}

// ValidateAPIServerConfig validates the API server monitor configuration.
func ValidateAPIServerConfig(config types.MonitorConfig) error {
	if config.Name == "" {
		return fmt.Errorf("monitor name is required")
	}

	if config.Type != "kubernetes-apiserver-check" {
		return fmt.Errorf("invalid monitor type: expected 'kubernetes-apiserver-check', got '%s'", config.Type)
	}

	if config.Interval <= 0 {
		return fmt.Errorf("interval must be positive")
	}

	if config.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}

	if config.Timeout >= config.Interval {
		return fmt.Errorf("timeout must be less than interval")
	}

	return nil
}

// init registers the API server monitor with the monitor registry.
func init() {
	monitors.MustRegister(monitors.MonitorInfo{
		Type:        "kubernetes-apiserver-check",
		Factory:     NewAPIServerMonitor,
		Validator:   ValidateAPIServerConfig,
		Description: "Monitors Kubernetes API server health including connectivity, latency, authentication, and rate limiting",
	})
}
