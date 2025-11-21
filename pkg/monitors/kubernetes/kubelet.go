// Package kubernetes provides Kubernetes component health monitoring capabilities.
package kubernetes

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/supporttools/node-doctor/pkg/monitors"
	"github.com/supporttools/node-doctor/pkg/types"
)

const (
	// Default configuration values for kubelet monitor
	defaultKubeletHealthzURL = "http://127.0.0.1:10248/healthz"

	// defaultKubeletMetricsURL uses port 10250, the secure kubelet metrics port.
	// Port 10255 (read-only port) is deprecated and disabled by default in modern Kubernetes.
	//
	// AUTHENTICATION REQUIREMENTS:
	// Port 10250 requires authentication via one of the following methods:
	//   1. Client certificates (recommended for production)
	//   2. Bearer token authentication
	//   3. Service Account tokens (when running in-cluster)
	//
	// DEPLOYMENT CONSIDERATIONS:
	// When deployed as a DaemonSet in Kubernetes (recommended deployment model):
	//   - The monitor runs on the same node as the kubelet
	//   - Uses the ServiceAccount token mounted at /var/run/secrets/kubernetes.io/serviceaccount/token
	//   - Requires RBAC permissions to access kubelet metrics (see deployment/rbac.yaml)
	//   - Connection is local (127.0.0.1), reducing network exposure
	//
	// CONFIGURATION OPTIONS:
	//   - For environments with kubelet --anonymous-auth=true: Use auth.type="none"
	//   - For HTTPS: Use https://127.0.0.1:10250/metrics and configure TLS
	//   - For in-cluster: Use auth.type="serviceaccount" (default when running as DaemonSet)
	//
	// SECURITY NOTES:
	//   - Production deployments should use HTTPS with proper authentication
	//   - Consider using --anonymous-auth=false with proper RBAC for production
	defaultKubeletMetricsURL = "http://127.0.0.1:10250/metrics"

	defaultKubeletTimeout          = 5 * time.Second
	defaultKubeletPLEGThreshold    = 5 * time.Second
	defaultKubeletFailureThreshold = 3
	defaultServiceAccountTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
)

// KubeletMonitorConfig holds the configuration for the kubelet monitor.
type KubeletMonitorConfig struct {
	// HealthzURL is the URL for the kubelet health endpoint
	HealthzURL string `json:"healthzURL"`

	// MetricsURL is the URL for the kubelet metrics endpoint
	MetricsURL string `json:"metricsURL"`

	// CheckSystemdStatus enables systemd service status verification
	CheckSystemdStatus bool `json:"checkSystemdStatus"`

	// CheckPLEG enables PLEG (Pod Lifecycle Event Generator) performance monitoring
	CheckPLEG bool `json:"checkPLEG"`

	// PLEGThreshold is the maximum acceptable PLEG relist duration
	PLEGThreshold time.Duration `json:"plegThreshold"`

	// FailureThreshold is the number of consecutive failures before reporting KubeletUnhealthy
	FailureThreshold int `json:"failureThreshold"`

	// HTTPTimeout is the timeout for HTTP requests
	HTTPTimeout time.Duration `json:"httpTimeout"`

	// Authentication configuration for kubelet endpoints
	Auth *AuthConfig `json:"auth,omitempty"`

	// CircuitBreaker configuration for protecting against cascading failures
	CircuitBreaker *CircuitBreakerConfig `json:"circuitBreaker,omitempty"`
}

// AuthConfig holds authentication configuration for kubelet endpoints.
type AuthConfig struct {
	// Type specifies the authentication method: "none", "serviceaccount", "bearer", "certificate"
	Type string `json:"type"`

	// TokenFile is the path to the ServiceAccount token file (for type="serviceaccount")
	// Default: /var/run/secrets/kubernetes.io/serviceaccount/token
	TokenFile string `json:"tokenFile,omitempty"`

	// BearerToken is an explicit bearer token string (for type="bearer")
	BearerToken string `json:"bearerToken,omitempty"`

	// CertFile is the path to the client certificate file (for type="certificate")
	CertFile string `json:"certFile,omitempty"`

	// KeyFile is the path to the client private key file (for type="certificate")
	KeyFile string `json:"keyFile,omitempty"`

	// InsecureSkipVerify disables TLS certificate verification (for HTTPS URLs)
	// WARNING: This should only be used for testing/development
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`
}

// validate validates the authentication configuration.
func (a *AuthConfig) validate() error {
	if a == nil {
		return nil
	}

	// Validate auth type
	validTypes := []string{"none", "serviceaccount", "bearer", "certificate"}
	isValid := false
	for _, t := range validTypes {
		if a.Type == t {
			isValid = true
			break
		}
	}
	if !isValid && a.Type != "" {
		return fmt.Errorf("invalid auth type: %s (must be one of: none, serviceaccount, bearer, certificate)", a.Type)
	}

	// Type-specific validation
	switch a.Type {
	case "serviceaccount":
		// TokenFile is optional, will use default if not specified
		// Validate that the file exists if specified
		if a.TokenFile != "" {
			if _, err := os.Stat(a.TokenFile); err != nil {
				return fmt.Errorf("tokenFile does not exist: %s: %w", a.TokenFile, err)
			}
		}

	case "bearer":
		// BearerToken is required for bearer type
		if a.BearerToken == "" {
			return fmt.Errorf("bearerToken is required when auth type is 'bearer'")
		}

	case "certificate":
		// Both CertFile and KeyFile are required for certificate auth
		if a.CertFile == "" {
			return fmt.Errorf("certFile is required when auth type is 'certificate'")
		}
		if a.KeyFile == "" {
			return fmt.Errorf("keyFile is required when auth type is 'certificate'")
		}
		// Validate that the files exist
		if _, err := os.Stat(a.CertFile); err != nil {
			return fmt.Errorf("certFile does not exist: %s: %w", a.CertFile, err)
		}
		if _, err := os.Stat(a.KeyFile); err != nil {
			return fmt.Errorf("keyFile does not exist: %s: %w", a.KeyFile, err)
		}
	}

	return nil
}

// KubeletMetrics represents parsed kubelet metrics.
type KubeletMetrics struct {
	// PLEGRelistDuration is the duration of PLEG relist operations in seconds
	PLEGRelistDuration float64
}

// KubeletClient interface abstracts kubelet health checking for testability.
type KubeletClient interface {
	// CheckHealth performs a health check against the kubelet healthz endpoint
	CheckHealth(ctx context.Context) error

	// GetMetrics retrieves and parses kubelet metrics
	GetMetrics(ctx context.Context) (*KubeletMetrics, error)

	// CheckSystemdStatus checks if the kubelet systemd service is active
	CheckSystemdStatus(ctx context.Context) (bool, error)
}

// defaultKubeletClient implements KubeletClient using the standard http.Client.
type defaultKubeletClient struct {
	healthzURL string
	metricsURL string
	timeout    time.Duration
	auth       *AuthConfig
	client     *http.Client
}

// newDefaultKubeletClient creates a new default kubelet client.
func newDefaultKubeletClient(config *KubeletMonitorConfig) KubeletClient {
	// Configure TLS if needed
	var tlsConfig *tls.Config
	if config.Auth != nil {
		var err error
		tlsConfig, err = configureTLS(config.Auth)
		if err != nil {
			// Log error but don't fail - fallback to no TLS
			// This allows the monitor to start even if TLS config has issues
			fmt.Fprintf(os.Stderr, "Warning: Failed to configure TLS: %v\n", err)
		}
	}

	// Configure HTTP transport with proper connection pooling limits
	// to prevent resource leaks in long-running scenarios
	transport := &http.Transport{
		MaxIdleConns:        10, // Maximum idle connections across all hosts
		MaxIdleConnsPerHost: 2,  // Maximum idle connections per host (localhost)
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false, // Enable keep-alive for connection reuse
		// Additional timeouts for robustness
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       tlsConfig,
	}

	return &defaultKubeletClient{
		healthzURL: config.HealthzURL,
		metricsURL: config.MetricsURL,
		timeout:    config.HTTPTimeout,
		auth:       config.Auth,
		client: &http.Client{
			Timeout:   config.HTTPTimeout,
			Transport: transport,
		},
	}
}

// configureTLS configures TLS settings based on authentication configuration.
func configureTLS(auth *AuthConfig) (*tls.Config, error) {
	if auth == nil {
		return nil, nil
	}

	tlsConfig := &tls.Config{
		InsecureSkipVerify: auth.InsecureSkipVerify,
	}

	// Configure client certificates if specified
	if auth.Type == "certificate" && auth.CertFile != "" && auth.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(auth.CertFile, auth.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	// Load system CA pool for HTTPS verification
	if !auth.InsecureSkipVerify {
		caCertPool, err := x509.SystemCertPool()
		if err != nil {
			// If we can't load system pool, create empty pool
			caCertPool = x509.NewCertPool()
		}
		tlsConfig.RootCAs = caCertPool
	}

	return tlsConfig, nil
}

// addAuthHeader adds authentication headers to the HTTP request based on auth configuration.
func (c *defaultKubeletClient) addAuthHeader(req *http.Request) error {
	if c.auth == nil || c.auth.Type == "none" || c.auth.Type == "" {
		return nil
	}

	switch c.auth.Type {
	case "serviceaccount":
		// Read ServiceAccount token from file
		tokenFile := c.auth.TokenFile
		if tokenFile == "" {
			tokenFile = defaultServiceAccountTokenPath
		}
		token, err := os.ReadFile(tokenFile)
		if err != nil {
			return fmt.Errorf("failed to read ServiceAccount token from %s: %w", tokenFile, err)
		}
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))

	case "bearer":
		// Use explicit bearer token
		if c.auth.BearerToken == "" {
			return fmt.Errorf("bearerToken is required for auth type 'bearer'")
		}
		req.Header.Set("Authorization", "Bearer "+c.auth.BearerToken)

	case "certificate":
		// Client certificate authentication is handled by TLS config, no header needed
		// Certificates are presented during TLS handshake
		return nil

	default:
		return fmt.Errorf("unsupported authentication type: %s", c.auth.Type)
	}

	return nil
}

// CheckHealth performs a health check against the kubelet healthz endpoint.
func (c *defaultKubeletClient) CheckHealth(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.healthzURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	// Add authentication header if configured
	if err := c.addAuthHeader(req); err != nil {
		return fmt.Errorf("failed to add authentication header: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("health check request failed: %w", err)
	}
	defer resp.Body.Close()

	// Drain response body to enable connection reuse
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned status %d, expected 200", resp.StatusCode)
	}

	return nil
}

// GetMetrics retrieves and parses kubelet metrics.
func (c *defaultKubeletClient) GetMetrics(ctx context.Context) (*KubeletMetrics, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.metricsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create metrics request: %w", err)
	}

	// Add authentication header if configured
	if err := c.addAuthHeader(req); err != nil {
		return nil, fmt.Errorf("failed to add authentication header: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("metrics request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Drain body before returning error
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("metrics endpoint returned status %d, expected 200", resp.StatusCode)
	}

	// Parse Prometheus metrics
	metrics, err := parsePrometheusMetrics(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse metrics: %w", err)
	}

	return metrics, nil
}

// CheckSystemdStatus checks if the kubelet systemd service is active.
func (c *defaultKubeletClient) CheckSystemdStatus(ctx context.Context) (bool, error) {
	// Use systemctl is-active for simple active/inactive check
	cmd := exec.CommandContext(ctx, "systemctl", "is-active", "kubelet")

	output, err := cmd.CombinedOutput()
	if err != nil {
		// systemctl is-active returns non-zero exit code if service is not active
		// Check if it's just inactive vs an actual error
		status := strings.TrimSpace(string(output))
		if status == "inactive" || status == "failed" {
			return false, nil
		}
		// Don't include full output to avoid information disclosure
		// Only include the status if it's a recognized state
		if status == "activating" || status == "deactivating" || status == "reloading" {
			return false, fmt.Errorf("kubelet service in transitional state: %s", status)
		}
		return false, fmt.Errorf("failed to check systemd status: %w", err)
	}

	status := strings.TrimSpace(string(output))
	return status == "active", nil
}

// parsePrometheusMetrics parses Prometheus metrics format to extract PLEG duration.
func parsePrometheusMetrics(reader io.Reader) (*KubeletMetrics, error) {
	metrics := &KubeletMetrics{}
	scanner := bufio.NewScanner(reader)

	// Look for kubelet_pleg_relist_duration_seconds metric
	// Example format: kubelet_pleg_relist_duration_seconds{quantile="0.9"} 0.003
	for scanner.Scan() {
		line := scanner.Text()

		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Look for PLEG metric with quantile labels (excludes _sum and _count aggregates)
		if strings.HasPrefix(line, "kubelet_pleg_relist_duration_seconds{") && strings.Contains(line, "quantile=") {
			// Extract the value (last field)
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				// Get the last field which should be the value
				valueStr := fields[len(fields)-1]
				value, err := strconv.ParseFloat(valueStr, 64)
				if err != nil {
					continue // Skip this line if parse fails
				}

				// Validate metric value is reasonable
				// PLEG duration should be positive and less than 1 hour (3600s)
				// Values outside this range indicate parsing errors or extreme issues
				if value < 0 || value > 3600 {
					continue // Skip invalid values
				}

				// Take the maximum value (usually the 0.99 quantile)
				if value > metrics.PLEGRelistDuration {
					metrics.PLEGRelistDuration = value
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading metrics: %w", err)
	}

	return metrics, nil
}

// KubeletMonitor monitors kubelet health.
//
// CONCURRENCY SAFETY GUARANTEES:
//
// Thread-Safe Operations (safe for concurrent use):
//   - Check(): Multiple goroutines can call Check() concurrently. The method uses
//     mutex protection for shared state (consecutiveFailures) and the circuit breaker
//     is internally thread-safe.
//   - Start(): Safe to call once. Returns error if already started. Uses BaseMonitor's
//     thread-safe lifecycle management.
//   - Stop(): Safe to call multiple times. Idempotent. Uses BaseMonitor's thread-safe
//     lifecycle management.
//   - GetConfig(): Read-only access to immutable config. Safe for concurrent use.
//   - GetType(): Read-only access to immutable type. Safe for concurrent use.
//
// Concurrency Implementation Details:
//   - consecutiveFailures: Protected by sync.Mutex (mu). All reads and writes acquire
//     the mutex, preventing race conditions during concurrent Check() calls.
//   - circuitBreaker: Uses internal sync.RWMutex for thread-safe state transitions
//     (closed -> open -> half-open). Safe for concurrent CanAttempt(), RecordSuccess(),
//     and RecordFailure() calls.
//   - metrics: Prometheus metrics use atomic operations internally. Safe for concurrent
//     updates from multiple Check() calls.
//   - client: KubeletClient implementations must be thread-safe. The default
//     defaultKubeletClient uses http.Client which has a thread-safe connection pool.
//   - config: Immutable after construction. Never modified, only read.
//   - BaseMonitor: Provides thread-safe Start()/Stop() lifecycle with internal
//     synchronization primitives.
//
// Testing: Stress tested with 100+ concurrent goroutines performing 1000+ operations.
// See kubelet_stress_test.go and kubelet_benchmark_test.go for validation.
//
// Performance Characteristics (from benchmarks):
//   - Check() single-threaded: ~2500 ns/op
//   - Check() parallel: ~1400 ns/op (demonstrates good concurrent scaling)
//   - Circuit breaker overhead: ~1600 ns/op additional
//   - Mutex contention: Minimal, measured under 100-goroutine load
type KubeletMonitor struct {
	name           string
	config         *KubeletMonitorConfig
	client         KubeletClient
	circuitBreaker *CircuitBreaker
	metrics        *KubeletMonitorMetrics // NEW: Prometheus metrics

	// Failure tracking (protected by mu for thread-safety)
	mu                  sync.Mutex
	consecutiveFailures int

	// BaseMonitor for lifecycle management
	*monitors.BaseMonitor
}

// init registers the kubelet monitor with the monitor registry.
func init() {
	monitors.Register(monitors.MonitorInfo{
		Type:        "kubernetes-kubelet-check",
		Factory:     NewKubeletMonitor,
		Validator:   ValidateKubeletConfig,
		Description: "Monitors kubelet health including /healthz endpoint, systemd status, and PLEG performance",
	})
}

// getNodeName returns the node name for metrics labeling
func getNodeName() string {
	// Try to get node name from environment variable (common in K8s DaemonSets)
	if nodeName := os.Getenv("NODE_NAME"); nodeName != "" {
		return nodeName
	}
	// Fallback to hostname
	if hostname, err := os.Hostname(); err == nil {
		return hostname
	}
	// Last resort fallback
	return "unknown"
}

// NewKubeletMonitor creates a new kubelet monitor instance.
func NewKubeletMonitor(ctx context.Context, config types.MonitorConfig) (types.Monitor, error) {
	return NewKubeletMonitorWithMetrics(ctx, config, nil)
}

// NewKubeletMonitorWithMetrics creates a new kubelet monitor instance with optional Prometheus metrics.
func NewKubeletMonitorWithMetrics(ctx context.Context, config types.MonitorConfig, registry *prometheus.Registry) (types.Monitor, error) {
	// Parse kubelet-specific configuration
	kubeletConfig, err := parseKubeletConfig(config.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse kubelet config: %w", err)
	}

	// Apply defaults
	if err := kubeletConfig.applyDefaults(); err != nil {
		return nil, fmt.Errorf("failed to apply defaults: %w", err)
	}

	// Create base monitor
	baseMonitor, err := monitors.NewBaseMonitor(config.Name, config.Interval, config.Timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to create base monitor: %w", err)
	}

	// Create circuit breaker if configured
	var circuitBreaker *CircuitBreaker
	if kubeletConfig.CircuitBreaker != nil && kubeletConfig.CircuitBreaker.Enabled {
		circuitBreaker, err = NewCircuitBreaker(kubeletConfig.CircuitBreaker)
		if err != nil {
			return nil, fmt.Errorf("failed to create circuit breaker: %w", err)
		}
	}

	// Create metrics if registry provided
	var metrics *KubeletMonitorMetrics
	if registry != nil {
		var err error
		metrics, err = NewKubeletMonitorMetrics(registry)
		if err != nil {
			return nil, fmt.Errorf("failed to create metrics: %w", err)
		}
	}

	// Create kubelet monitor
	monitor := &KubeletMonitor{
		name:           config.Name,
		config:         kubeletConfig,
		client:         newDefaultKubeletClient(kubeletConfig),
		circuitBreaker: circuitBreaker,
		metrics:        metrics,
		BaseMonitor:    baseMonitor,
	}

	// Set the check function
	if err := baseMonitor.SetCheckFunc(monitor.checkKubelet); err != nil {
		return nil, fmt.Errorf("failed to set check function: %w", err)
	}

	return monitor, nil
}

// ValidateKubeletConfig validates the kubelet monitor configuration.
func ValidateKubeletConfig(config types.MonitorConfig) error {
	if config.Name == "" {
		return fmt.Errorf("monitor name is required")
	}

	if config.Type != "kubernetes-kubelet-check" {
		return fmt.Errorf("invalid monitor type: expected kubernetes-kubelet-check, got %s", config.Type)
	}

	// Parse and validate kubelet-specific config
	kubeletConfig, err := parseKubeletConfig(config.Config)
	if err != nil {
		return fmt.Errorf("failed to parse kubelet config: %w", err)
	}

	// Apply defaults for validation
	if err := kubeletConfig.applyDefaults(); err != nil {
		return fmt.Errorf("failed to apply defaults: %w", err)
	}

	// Validate failure threshold
	if kubeletConfig.FailureThreshold < 1 {
		return fmt.Errorf("failureThreshold must be at least 1, got %d", kubeletConfig.FailureThreshold)
	}

	// Validate PLEG threshold
	if kubeletConfig.CheckPLEG && kubeletConfig.PLEGThreshold <= 0 {
		return fmt.Errorf("plegThreshold must be positive when checkPLEG is enabled, got %v", kubeletConfig.PLEGThreshold)
	}

	// Validate HTTP timeout
	if kubeletConfig.HTTPTimeout <= 0 {
		return fmt.Errorf("httpTimeout must be positive, got %v", kubeletConfig.HTTPTimeout)
	}

	// Validate circuit breaker configuration if present
	if kubeletConfig.CircuitBreaker != nil && kubeletConfig.CircuitBreaker.Enabled {
		if err := kubeletConfig.CircuitBreaker.validate(); err != nil {
			return fmt.Errorf("circuit breaker configuration invalid: %w", err)
		}
	}

	return nil
}

// parseKubeletConfig parses the kubelet monitor configuration from a map.
func parseKubeletConfig(configMap map[string]interface{}) (*KubeletMonitorConfig, error) {
	if configMap == nil {
		return &KubeletMonitorConfig{}, nil
	}

	config := &KubeletMonitorConfig{}

	// Parse HealthzURL
	if val, ok := configMap["healthzURL"]; ok {
		strVal, ok := val.(string)
		if !ok {
			return nil, fmt.Errorf("healthzURL must be a string, got %T", val)
		}
		config.HealthzURL = strVal
	}

	// Parse MetricsURL
	if val, ok := configMap["metricsURL"]; ok {
		strVal, ok := val.(string)
		if !ok {
			return nil, fmt.Errorf("metricsURL must be a string, got %T", val)
		}
		config.MetricsURL = strVal
	}

	// Parse CheckSystemdStatus
	if val, ok := configMap["checkSystemdStatus"]; ok {
		boolVal, ok := val.(bool)
		if !ok {
			return nil, fmt.Errorf("checkSystemdStatus must be a boolean, got %T", val)
		}
		config.CheckSystemdStatus = boolVal
	}

	// Parse CheckPLEG
	if val, ok := configMap["checkPLEG"]; ok {
		boolVal, ok := val.(bool)
		if !ok {
			return nil, fmt.Errorf("checkPLEG must be a boolean, got %T", val)
		}
		config.CheckPLEG = boolVal
	}

	// Parse PLEGThreshold
	if val, ok := configMap["plegThreshold"]; ok {
		switch v := val.(type) {
		case string:
			duration, err := time.ParseDuration(v)
			if err != nil {
				return nil, fmt.Errorf("invalid plegThreshold duration: %w", err)
			}
			config.PLEGThreshold = duration
		case float64:
			// Treat as seconds
			config.PLEGThreshold = time.Duration(v * float64(time.Second))
		case int:
			// Treat as seconds
			config.PLEGThreshold = time.Duration(v) * time.Second
		default:
			return nil, fmt.Errorf("plegThreshold must be a duration string or number, got %T", val)
		}
	}

	// Parse FailureThreshold
	if val, ok := configMap["failureThreshold"]; ok {
		switch v := val.(type) {
		case float64:
			config.FailureThreshold = int(v)
		case int:
			config.FailureThreshold = v
		default:
			return nil, fmt.Errorf("failureThreshold must be an integer, got %T", val)
		}
	}

	// Parse HTTPTimeout
	if val, ok := configMap["httpTimeout"]; ok {
		switch v := val.(type) {
		case string:
			duration, err := time.ParseDuration(v)
			if err != nil {
				return nil, fmt.Errorf("invalid httpTimeout duration: %w", err)
			}
			config.HTTPTimeout = duration
		case float64:
			// Treat as seconds
			config.HTTPTimeout = time.Duration(v * float64(time.Second))
		case int:
			// Treat as seconds
			config.HTTPTimeout = time.Duration(v) * time.Second
		default:
			return nil, fmt.Errorf("httpTimeout must be a duration string or number, got %T", val)
		}
	}

	// Parse Auth configuration
	if val, ok := configMap["auth"]; ok {
		authMap, ok := val.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("auth must be an object, got %T", val)
		}

		authConfig, err := parseAuthConfig(authMap)
		if err != nil {
			return nil, fmt.Errorf("failed to parse auth config: %w", err)
		}
		config.Auth = authConfig
	}

	// Parse CircuitBreaker configuration
	if val, ok := configMap["circuitBreaker"]; ok {
		cbMap, ok := val.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("circuitBreaker must be an object, got %T", val)
		}

		cbConfig, err := parseCircuitBreakerConfig(cbMap)
		if err != nil {
			return nil, fmt.Errorf("failed to parse circuit breaker config: %w", err)
		}
		config.CircuitBreaker = cbConfig
	}

	return config, nil
}

// parseAuthConfig parses authentication configuration from a map.
func parseAuthConfig(configMap map[string]interface{}) (*AuthConfig, error) {
	if configMap == nil {
		return nil, nil
	}

	config := &AuthConfig{}

	// Parse Type
	if val, ok := configMap["type"]; ok {
		strVal, ok := val.(string)
		if !ok {
			return nil, fmt.Errorf("type must be a string, got %T", val)
		}
		config.Type = strVal
	}

	// Parse TokenFile
	if val, ok := configMap["tokenFile"]; ok {
		strVal, ok := val.(string)
		if !ok {
			return nil, fmt.Errorf("tokenFile must be a string, got %T", val)
		}
		config.TokenFile = strVal
	}

	// Parse BearerToken
	if val, ok := configMap["bearerToken"]; ok {
		strVal, ok := val.(string)
		if !ok {
			return nil, fmt.Errorf("bearerToken must be a string, got %T", val)
		}
		config.BearerToken = strVal
	}

	// Parse CertFile
	if val, ok := configMap["certFile"]; ok {
		strVal, ok := val.(string)
		if !ok {
			return nil, fmt.Errorf("certFile must be a string, got %T", val)
		}
		config.CertFile = strVal
	}

	// Parse KeyFile
	if val, ok := configMap["keyFile"]; ok {
		strVal, ok := val.(string)
		if !ok {
			return nil, fmt.Errorf("keyFile must be a string, got %T", val)
		}
		config.KeyFile = strVal
	}

	// Parse InsecureSkipVerify
	if val, ok := configMap["insecureSkipVerify"]; ok {
		boolVal, ok := val.(bool)
		if !ok {
			return nil, fmt.Errorf("insecureSkipVerify must be a boolean, got %T", val)
		}
		config.InsecureSkipVerify = boolVal
	}

	return config, nil
}

// parseCircuitBreakerConfig parses circuit breaker configuration from a map.
func parseCircuitBreakerConfig(configMap map[string]interface{}) (*CircuitBreakerConfig, error) {
	if configMap == nil {
		return nil, nil
	}

	config := &CircuitBreakerConfig{}

	// Parse Enabled
	if val, ok := configMap["enabled"]; ok {
		boolVal, ok := val.(bool)
		if !ok {
			return nil, fmt.Errorf("enabled must be a boolean, got %T", val)
		}
		config.Enabled = boolVal
	}

	// Parse FailureThreshold
	if val, ok := configMap["failureThreshold"]; ok {
		switch v := val.(type) {
		case float64:
			config.FailureThreshold = int(v)
		case int:
			config.FailureThreshold = v
		default:
			return nil, fmt.Errorf("failureThreshold must be an integer, got %T", val)
		}
	}

	// Parse OpenTimeout
	if val, ok := configMap["openTimeout"]; ok {
		switch v := val.(type) {
		case string:
			duration, err := time.ParseDuration(v)
			if err != nil {
				return nil, fmt.Errorf("invalid openTimeout duration: %w", err)
			}
			config.OpenTimeout = duration
		case float64:
			// Treat as seconds
			config.OpenTimeout = time.Duration(v * float64(time.Second))
		case int:
			// Treat as seconds
			config.OpenTimeout = time.Duration(v) * time.Second
		default:
			return nil, fmt.Errorf("openTimeout must be a duration string or number, got %T", val)
		}
	}

	// Parse HalfOpenMaxRequests
	if val, ok := configMap["halfOpenMaxRequests"]; ok {
		switch v := val.(type) {
		case float64:
			config.HalfOpenMaxRequests = int(v)
		case int:
			config.HalfOpenMaxRequests = v
		default:
			return nil, fmt.Errorf("halfOpenMaxRequests must be an integer, got %T", val)
		}
	}

	// Parse UseExponentialBackoff
	if val, ok := configMap["useExponentialBackoff"]; ok {
		boolVal, ok := val.(bool)
		if !ok {
			return nil, fmt.Errorf("useExponentialBackoff must be a boolean, got %T", val)
		}
		config.UseExponentialBackoff = boolVal
	}

	// Parse MaxBackoffTimeout
	if val, ok := configMap["maxBackoffTimeout"]; ok {
		switch v := val.(type) {
		case string:
			duration, err := time.ParseDuration(v)
			if err != nil {
				return nil, fmt.Errorf("invalid maxBackoffTimeout duration: %w", err)
			}
			config.MaxBackoffTimeout = duration
		case float64:
			// Treat as seconds
			config.MaxBackoffTimeout = time.Duration(v * float64(time.Second))
		case int:
			// Treat as seconds
			config.MaxBackoffTimeout = time.Duration(v) * time.Second
		default:
			return nil, fmt.Errorf("maxBackoffTimeout must be a duration string or number, got %T", val)
		}
	}

	return config, nil
}

// applyDefaults applies default values to the kubelet monitor configuration.
func (c *KubeletMonitorConfig) applyDefaults() error {
	// Default healthz URL
	if c.HealthzURL == "" {
		c.HealthzURL = defaultKubeletHealthzURL
	}

	// Default metrics URL
	if c.MetricsURL == "" {
		c.MetricsURL = defaultKubeletMetricsURL
	}

	// Default to checking systemd status
	// Note: CheckSystemdStatus defaults to false (zero value) unless explicitly set

	// Default to checking PLEG
	// Note: CheckPLEG defaults to false (zero value) unless explicitly set

	// Default PLEG threshold
	if c.PLEGThreshold == 0 {
		c.PLEGThreshold = defaultKubeletPLEGThreshold
	}

	// Default failure threshold
	if c.FailureThreshold == 0 {
		c.FailureThreshold = defaultKubeletFailureThreshold
	}

	// Default HTTP timeout
	if c.HTTPTimeout == 0 {
		c.HTTPTimeout = defaultKubeletTimeout
	}

	// Validate URLs
	if err := validateKubeletURL(c.HealthzURL, "healthzURL"); err != nil {
		return err
	}
	if err := validateKubeletURL(c.MetricsURL, "metricsURL"); err != nil {
		return err
	}

	// Validate authentication configuration if present
	if c.Auth != nil {
		if err := c.Auth.validate(); err != nil {
			return fmt.Errorf("auth configuration invalid: %w", err)
		}
	}

	return nil
}

// validateKubeletURL validates that a URL is well-formed and uses HTTP/HTTPS scheme.
func validateKubeletURL(urlStr, fieldName string) error {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("%s is not a valid URL: %w", fieldName, err)
	}

	// Ensure URL has a scheme
	if parsedURL.Scheme == "" {
		return fmt.Errorf("%s must include a scheme (http:// or https://)", fieldName)
	}

	// Only allow HTTP and HTTPS
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("%s has unsupported scheme: %s (allowed: http, https)", fieldName, parsedURL.Scheme)
	}

	// Ensure URL has a host
	if parsedURL.Host == "" {
		return fmt.Errorf("%s must include a host", fieldName)
	}

	return nil
}

// checkKubelet performs the kubelet health check.
func (m *KubeletMonitor) checkKubelet(ctx context.Context) (*types.Status, error) {
	status := types.NewStatus(m.name)
	nodeName := getNodeName()

	// Overall check timing
	checkStart := time.Now()
	var checkResult string
	defer func() {
		if m.metrics != nil {
			duration := time.Since(checkStart).Seconds()
			m.metrics.RecordCheckDuration(nodeName, m.name, checkResult, duration)
			m.metrics.RecordCheckResult(nodeName, m.name, "overall", checkResult)
		}
	}()

	// Check circuit breaker state before attempting health checks
	if m.circuitBreaker != nil && !m.circuitBreaker.CanAttempt() {
		checkResult = "circuit_open"
		// Circuit is open - fail fast
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"CircuitBreakerOpen",
			fmt.Sprintf("Circuit breaker is open, skipping health check (state: %s)", m.circuitBreaker.State()),
		))
		status.AddCondition(types.NewCondition(
			"KubeletUnhealthy",
			types.ConditionTrue,
			"CircuitBreakerOpen",
			"Circuit breaker is open due to repeated failures",
		))

		// Add circuit breaker metrics to status
		m.addCircuitBreakerMetrics(status)

		return status, nil
	}

	// Track if all checks passed
	allHealthy := true

	// Check kubelet health endpoint
	healthOK := m.checkHealth(ctx, status, nodeName)
	if !healthOK {
		allHealthy = false
	}

	// Check systemd status if enabled
	if m.config.CheckSystemdStatus {
		systemdOK := m.checkSystemd(ctx, status, nodeName)
		if !systemdOK {
			allHealthy = false
		}
	}

	// Check PLEG performance if enabled
	if m.config.CheckPLEG {
		plegOK := m.checkPLEG(ctx, status, nodeName)
		if !plegOK {
			allHealthy = false
		}
	}

	// Determine final result
	if allHealthy {
		checkResult = "success"
	} else {
		checkResult = "failure"
	}

	// Record result with circuit breaker
	if m.circuitBreaker != nil {
		if allHealthy {
			m.circuitBreaker.RecordSuccess()
		} else {
			m.circuitBreaker.RecordFailure()
		}

		// Add circuit breaker metrics to status
		m.addCircuitBreakerMetrics(status)
	}

	// Update failure tracking and conditions
	m.updateFailureTracking(allHealthy, status, nodeName)

	return status, nil
}

// checkHealth checks the kubelet health endpoint.
func (m *KubeletMonitor) checkHealth(ctx context.Context, status *types.Status, nodeName string) bool {
	start := time.Now()
	var result string
	defer func() {
		if m.metrics != nil {
			duration := time.Since(start).Seconds()
			m.metrics.RecordHealthCheck(nodeName, m.name, result, duration)
			m.metrics.RecordCheckResult(nodeName, m.name, "health", result)
		}
	}()

	err := m.client.CheckHealth(ctx)
	if err != nil {
		result = "failure"
		status.AddEvent(types.NewEvent(
			types.EventError,
			"KubeletHealthCheckFailed",
			fmt.Sprintf("Kubelet health check failed: %v", err),
		))
		return false
	}

	result = "success"
	status.AddEvent(types.NewEvent(
		types.EventInfo,
		"KubeletHealthy",
		"Kubelet health endpoint is responding",
	))
	return true
}

// checkSystemd checks the kubelet systemd service status.
func (m *KubeletMonitor) checkSystemd(ctx context.Context, status *types.Status, nodeName string) bool {
	start := time.Now()
	var result string
	defer func() {
		if m.metrics != nil {
			duration := time.Since(start).Seconds()
			m.metrics.RecordSystemdCheck(nodeName, m.name, result, duration)
			m.metrics.RecordCheckResult(nodeName, m.name, "systemd", result)
		}
	}()

	active, err := m.client.CheckSystemdStatus(ctx)
	if err != nil {
		result = "error"
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"SystemdCheckFailed",
			fmt.Sprintf("Failed to check kubelet systemd status: %v", err),
		))
		// Don't count systemd check errors as health failures
		return true
	}

	if !active {
		result = "inactive"
		status.AddEvent(types.NewEvent(
			types.EventError,
			"KubeletSystemdInactive",
			"Kubelet systemd service is not active",
		))

		// Report KubeletDown condition immediately for systemd failures
		status.AddCondition(types.NewCondition(
			"KubeletDown",
			types.ConditionTrue,
			"SystemdServiceInactive",
			"Kubelet systemd service is not active",
		))
		return false
	}

	result = "active"
	status.AddEvent(types.NewEvent(
		types.EventInfo,
		"KubeletSystemdActive",
		"Kubelet systemd service is active",
	))
	return true
}

// checkPLEG checks PLEG (Pod Lifecycle Event Generator) performance.
func (m *KubeletMonitor) checkPLEG(ctx context.Context, status *types.Status, nodeName string) bool {
	start := time.Now()
	var result string
	defer func() {
		if m.metrics != nil {
			duration := time.Since(start).Seconds()
			m.metrics.RecordPLEGCheck(nodeName, m.name, result, duration)
			m.metrics.RecordCheckResult(nodeName, m.name, "pleg", result)
		}
	}()

	// Track PLEG parsing time separately
	parseStart := time.Now()
	metrics, err := m.client.GetMetrics(ctx)
	if m.metrics != nil {
		parseDuration := time.Since(parseStart).Seconds()
		m.metrics.RecordPLEGParsingDuration(nodeName, m.name, parseDuration)
	}

	if err != nil {
		result = "parse_error"
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"PLEGMetricsFailed",
			fmt.Sprintf("Failed to retrieve PLEG metrics: %v", err),
		))
		// Don't count metrics fetch errors as health failures
		return true
	}

	// Record the PLEG relist duration
	if m.metrics != nil {
		m.metrics.SetPLEGRelistDuration(nodeName, m.name, metrics.PLEGRelistDuration)
	}

	// Check if PLEG duration exceeds threshold
	plegDuration := time.Duration(metrics.PLEGRelistDuration * float64(time.Second))
	if plegDuration > m.config.PLEGThreshold {
		result = "slow"
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"PLEGSlow",
			fmt.Sprintf("PLEG relist duration (%v) exceeds threshold (%v)", plegDuration, m.config.PLEGThreshold),
		))
		// PLEG slowness is a warning, not a failure
		return true
	}

	result = "healthy"
	status.AddEvent(types.NewEvent(
		types.EventInfo,
		"PLEGHealthy",
		fmt.Sprintf("PLEG relist duration is healthy (%v)", plegDuration),
	))
	return true
}

// updateFailureTracking updates failure counters and sets conditions based on health state.
func (m *KubeletMonitor) updateFailureTracking(healthy bool, status *types.Status, nodeName string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !healthy {
		m.consecutiveFailures++

		// Update metrics
		if m.metrics != nil {
			m.metrics.SetConsecutiveFailures(nodeName, m.name, m.consecutiveFailures)
		}

		// Check if failures exceed threshold
		if m.consecutiveFailures >= m.config.FailureThreshold {
			status.AddCondition(types.NewCondition(
				"KubeletUnhealthy",
				types.ConditionTrue,
				"RepeatedHealthCheckFailures",
				fmt.Sprintf("Kubelet has failed %d consecutive health checks (threshold: %d)",
					m.consecutiveFailures, m.config.FailureThreshold),
			))
		}
	} else {
		// Check if we're recovering from failures
		if m.consecutiveFailures > 0 {
			previousFailures := m.consecutiveFailures
			m.consecutiveFailures = 0

			// Update metrics
			if m.metrics != nil {
				m.metrics.SetConsecutiveFailures(nodeName, m.name, 0)
			}

			// Add recovery event
			status.AddEvent(types.NewEvent(
				types.EventInfo,
				"KubeletRecovered",
				fmt.Sprintf("Kubelet health restored after %d consecutive failures", previousFailures),
			))

			// Clear KubeletUnhealthy condition
			status.AddCondition(types.NewCondition(
				"KubeletHealthy",
				types.ConditionTrue,
				"HealthCheckPassed",
				"Kubelet health checks are passing",
			))
		} else {
			// Normal healthy state
			status.AddCondition(types.NewCondition(
				"KubeletHealthy",
				types.ConditionTrue,
				"HealthCheckPassed",
				"Kubelet health checks are passing",
			))
		}
	}
}

// addCircuitBreakerMetrics adds circuit breaker state and metrics to the status.
func (m *KubeletMonitor) addCircuitBreakerMetrics(status *types.Status) {
	if m.circuitBreaker == nil {
		return
	}

	nodeName := getNodeName()
	metrics := m.circuitBreaker.Metrics()
	state := m.circuitBreaker.State()

	// Update Prometheus metrics if available
	if m.metrics != nil {
		// Map circuit state to numeric value for Prometheus
		var stateValue int
		switch state {
		case StateClosed:
			stateValue = 0
		case StateHalfOpen:
			stateValue = 1
		case StateOpen:
			stateValue = 2
		default:
			stateValue = -1 // Unknown/disabled
		}
		m.metrics.SetCircuitBreakerState(nodeName, m.name, stateValue)
		m.metrics.SetCircuitBreakerBackoffMultiplier(nodeName, m.name, float64(metrics.CurrentBackoffMultiplier))
	}

	// Add informational event with circuit breaker metrics
	metricsMsg := fmt.Sprintf("Circuit breaker state: %s | Transitions: %d | Openings: %d | Recoveries: %d | Backoff: %dx",
		state.String(),
		metrics.StateTransitions,
		metrics.TotalOpenings,
		metrics.TotalRecoveries,
		metrics.CurrentBackoffMultiplier)

	status.AddEvent(types.NewEvent(
		types.EventInfo,
		"CircuitBreakerMetrics",
		metricsMsg,
	))

	// Add state-specific events
	if state == StateOpen {
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"CircuitBreakerOpen",
			fmt.Sprintf("Circuit breaker is open due to repeated failures (openings: %d)", metrics.TotalOpenings),
		))
		// Record opening metric
		if m.metrics != nil {
			m.metrics.RecordCircuitBreakerOpening(nodeName, m.name)
		}
	} else if state == StateHalfOpen {
		status.AddEvent(types.NewEvent(
			types.EventInfo,
			"CircuitBreakerHalfOpen",
			"Circuit breaker is testing recovery",
		))
	} else if state == StateClosed && metrics.TotalRecoveries > 0 {
		status.AddEvent(types.NewEvent(
			types.EventInfo,
			"CircuitBreakerRecovered",
			fmt.Sprintf("Circuit breaker recovered successfully (total recoveries: %d)", metrics.TotalRecoveries),
		))
		// Record recovery metric
		if m.metrics != nil {
			m.metrics.RecordCircuitBreakerRecovery(nodeName, m.name)
		}
	}
}