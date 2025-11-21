// Package network provides network health monitoring capabilities.
package network

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/supporttools/node-doctor/pkg/monitors"
	"github.com/supporttools/node-doctor/pkg/types"
)

const (
	// Default configuration values for connectivity monitor
	defaultConnectivityTimeout          = 10 * time.Second
	defaultConnectivityMethod           = "HEAD"
	defaultConnectivityExpectedStatus   = 200
	defaultConnectivityFailureThreshold = 3
	defaultConnectivityFollowRedirects  = false

	// Resource limits
	maxEndpoints = 50 // Maximum number of endpoints to prevent resource exhaustion
)

// ConnectivityMonitorConfig holds the configuration for the connectivity monitor.
type ConnectivityMonitorConfig struct {
	// Endpoints is the list of endpoints to check for connectivity.
	Endpoints []EndpointConfig
	// FailureThreshold is the number of consecutive failures before reporting NetworkUnreachable.
	FailureThreshold int
}

// ConnectivityMonitor monitors external network connectivity by checking HTTP/HTTPS endpoints.
type ConnectivityMonitor struct {
	name             string
	config           *ConnectivityMonitorConfig
	client           HTTPClient
	mu               sync.Mutex
	endpointFailures map[string]int // Tracks consecutive failures per endpoint

	*monitors.BaseMonitor
}

// init registers the connectivity monitor with the monitor registry.
func init() {
	monitors.Register(monitors.MonitorInfo{
		Type:        "network-connectivity-check",
		Factory:     NewConnectivityMonitor,
		Validator:   ValidateConnectivityConfig,
		Description: "Monitors external network connectivity by checking HTTP/HTTPS endpoints",
	})
}

// NewConnectivityMonitor creates a new connectivity monitor instance.
func NewConnectivityMonitor(ctx context.Context, config types.MonitorConfig) (types.Monitor, error) {
	// Parse connectivity-specific configuration
	connectivityConfig, err := parseConnectivityConfig(config.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse connectivity config: %w", err)
	}

	// Validate that at least one endpoint is configured
	if len(connectivityConfig.Endpoints) == 0 {
		return nil, fmt.Errorf("at least one endpoint must be configured")
	}

	// Enforce resource limits to prevent configuration-based DoS
	if len(connectivityConfig.Endpoints) > maxEndpoints {
		return nil, fmt.Errorf("too many endpoints configured: %d (maximum: %d)", len(connectivityConfig.Endpoints), maxEndpoints)
	}

	// Create base monitor for lifecycle management
	baseMonitor, err := monitors.NewBaseMonitor(config.Name, config.Interval, config.Timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to create base monitor: %w", err)
	}

	// Create connectivity monitor
	monitor := &ConnectivityMonitor{
		name:             config.Name,
		config:           connectivityConfig,
		client:           newDefaultHTTPClient(),
		endpointFailures: make(map[string]int),
		BaseMonitor:      baseMonitor,
	}

	// Set the check function
	if err := baseMonitor.SetCheckFunc(monitor.checkConnectivity); err != nil {
		return nil, fmt.Errorf("failed to set check function: %w", err)
	}

	return monitor, nil
}

// parseConnectivityConfig parses the connectivity monitor configuration from a map.
func parseConnectivityConfig(configMap map[string]interface{}) (*ConnectivityMonitorConfig, error) {
	config := &ConnectivityMonitorConfig{
		Endpoints:        []EndpointConfig{},
		FailureThreshold: defaultConnectivityFailureThreshold,
	}

	if configMap == nil {
		return nil, fmt.Errorf("connectivity monitor requires configuration")
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

	// Parse endpoints
	if v, ok := configMap["endpoints"]; ok {
		endpointsList, ok := v.([]interface{})
		if !ok {
			return nil, fmt.Errorf("endpoints must be a list, got %T", v)
		}

		for i, endpointInterface := range endpointsList {
			endpointMap, ok := endpointInterface.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("endpoint %d must be a map, got %T", i, endpointInterface)
			}

			endpoint, err := parseEndpointConfig(endpointMap)
			if err != nil {
				return nil, fmt.Errorf("failed to parse endpoint %d: %w", i, err)
			}

			config.Endpoints = append(config.Endpoints, endpoint)
		}
	}

	return config, nil
}

// parseEndpointConfig parses a single endpoint configuration.
func parseEndpointConfig(configMap map[string]interface{}) (EndpointConfig, error) {
	endpoint := EndpointConfig{
		Method:             defaultConnectivityMethod,
		ExpectedStatusCode: defaultConnectivityExpectedStatus,
		Timeout:            defaultConnectivityTimeout,
		FollowRedirects:    defaultConnectivityFollowRedirects,
		Headers:            make(map[string]string),
	}

	// Parse URL (required)
	if v, ok := configMap["url"]; ok {
		strVal, ok := v.(string)
		if !ok {
			return endpoint, fmt.Errorf("url must be a string, got %T", v)
		}
		endpoint.URL = strVal

		// Validate URL format at configuration time
		if err := validateURL(strVal); err != nil {
			return endpoint, fmt.Errorf("invalid url: %w", err)
		}
	} else {
		return endpoint, fmt.Errorf("url is required for endpoint")
	}

	// Parse name (optional, defaults to URL)
	if v, ok := configMap["name"]; ok {
		strVal, ok := v.(string)
		if !ok {
			return endpoint, fmt.Errorf("name must be a string, got %T", v)
		}
		endpoint.Name = strVal
	} else {
		endpoint.Name = endpoint.URL
	}

	// Parse method (restrict to safe HTTP methods)
	if v, ok := configMap["method"]; ok {
		strVal, ok := v.(string)
		if !ok {
			return endpoint, fmt.Errorf("method must be a string, got %T", v)
		}
		// Validate method is safe for connectivity checking
		if !isValidHTTPMethod(strVal) {
			return endpoint, fmt.Errorf("invalid HTTP method: %s (allowed: GET, HEAD, OPTIONS)", strVal)
		}
		endpoint.Method = strVal
	}

	// Parse expected status code
	if v, ok := configMap["expectedStatusCode"]; ok {
		switch val := v.(type) {
		case int:
			endpoint.ExpectedStatusCode = val
		case float64:
			endpoint.ExpectedStatusCode = int(val)
		default:
			return endpoint, fmt.Errorf("expectedStatusCode must be an integer, got %T", v)
		}
	}

	// Parse timeout
	if v, ok := configMap["timeout"]; ok {
		timeout, err := parseDuration(v)
		if err != nil {
			return endpoint, fmt.Errorf("invalid timeout: %w", err)
		}
		endpoint.Timeout = timeout
	}

	// Parse follow redirects
	if v, ok := configMap["followRedirects"]; ok {
		boolVal, ok := v.(bool)
		if !ok {
			return endpoint, fmt.Errorf("followRedirects must be a boolean, got %T", v)
		}
		endpoint.FollowRedirects = boolVal
	}

	// Parse headers
	if v, ok := configMap["headers"]; ok {
		headersMap, ok := v.(map[string]interface{})
		if !ok {
			return endpoint, fmt.Errorf("headers must be a map, got %T", v)
		}
		for key, value := range headersMap {
			strValue, ok := value.(string)
			if !ok {
				return endpoint, fmt.Errorf("header value for %s must be a string, got %T", key, value)
			}
			endpoint.Headers[key] = strValue
		}
	}

	return endpoint, nil
}

// ValidateConnectivityConfig validates the connectivity monitor configuration.
func ValidateConnectivityConfig(config types.MonitorConfig) error {
	_, err := parseConnectivityConfig(config.Config)
	return err
}

// validateURL validates that a URL is well-formed and uses safe protocols.
func validateURL(urlStr string) error {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("failed to parse URL: %w", err)
	}

	// Ensure URL has a scheme
	if parsedURL.Scheme == "" {
		return fmt.Errorf("URL must include a scheme (http:// or https://)")
	}

	// Only allow HTTP and HTTPS for security
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("unsupported URL scheme: %s (allowed: http, https)", parsedURL.Scheme)
	}

	// Ensure URL has a host
	if parsedURL.Host == "" {
		return fmt.Errorf("URL must include a host")
	}

	return nil
}

// isValidHTTPMethod checks if the HTTP method is safe for connectivity checks.
// Only GET, HEAD, and OPTIONS are allowed to prevent accidental modifications.
func isValidHTTPMethod(method string) bool {
	normalized := strings.ToUpper(method)
	switch normalized {
	case "GET", "HEAD", "OPTIONS":
		return true
	default:
		return false
	}
}

// checkConnectivity performs a connectivity health check.
func (m *ConnectivityMonitor) checkConnectivity(ctx context.Context) (*types.Status, error) {
	status := types.NewStatus(m.name)

	// Check each endpoint
	successfulEndpoints := 0
	failedEndpoints := 0
	var dnsFailures []string
	var httpFailures []string

	for _, endpoint := range m.config.Endpoints {
		result, err := m.client.CheckEndpoint(ctx, endpoint)
		if err != nil {
			// Log unexpected errors but continue checking other endpoints
			status.AddEvent(types.NewEvent(
				types.EventError,
				"EndpointCheckError",
				fmt.Sprintf("Unexpected error checking endpoint %s: %v", endpoint.Name, err),
			))
			continue
		}

		if result.Success {
			successfulEndpoints++
			m.updateEndpointFailureCount(endpoint.Name, true, status)

			// Add info event for successful check
			status.AddEvent(types.NewEvent(
				types.EventInfo,
				"EndpointReachable",
				fmt.Sprintf("Endpoint %s is reachable (status: %d, response time: %v)",
					endpoint.Name, result.StatusCode, result.ResponseTime),
			))
		} else {
			failedEndpoints++
			m.updateEndpointFailureCount(endpoint.Name, false, status)

			// Classify failure type
			if result.ErrorType == "DNS" {
				dnsFailures = append(dnsFailures, endpoint.Name)
				status.AddEvent(types.NewEvent(
					types.EventWarning,
					"EndpointDNSFailure",
					fmt.Sprintf("DNS resolution failed for endpoint %s: %v", endpoint.Name, result.Error),
				))
			} else {
				httpFailures = append(httpFailures, endpoint.Name)
				status.AddEvent(types.NewEvent(
					types.EventWarning,
					"EndpointUnreachable",
					fmt.Sprintf("Endpoint %s is unreachable (error type: %s): %v",
						endpoint.Name, result.ErrorType, result.Error),
				))
			}
		}
	}

	// Determine overall connectivity status
	// At least one endpoint must succeed for external connectivity
	if successfulEndpoints == 0 && failedEndpoints > 0 {
		// All endpoints failed
		if len(dnsFailures) > 0 && len(httpFailures) == 0 {
			// Pure DNS failures suggest DNS resolution issues
			status.AddEvent(types.NewEvent(
				types.EventError,
				"DNSResolutionFailure",
				fmt.Sprintf("All %d endpoints failed due to DNS resolution issues", failedEndpoints),
			))
		} else if len(httpFailures) > 0 && len(dnsFailures) == 0 {
			// Pure HTTP failures suggest connectivity issues
			status.AddEvent(types.NewEvent(
				types.EventError,
				"ConnectivityFailure",
				fmt.Sprintf("All %d endpoints failed connectivity checks", failedEndpoints),
			))
		} else {
			// Mixed failures
			status.AddEvent(types.NewEvent(
				types.EventError,
				"ExternalConnectivityFailure",
				fmt.Sprintf("All %d endpoints failed (%d DNS failures, %d connectivity failures)",
					failedEndpoints, len(dnsFailures), len(httpFailures)),
			))
		}
	} else if successfulEndpoints > 0 && failedEndpoints > 0 {
		// Partial connectivity
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"PartialConnectivity",
			fmt.Sprintf("Partial connectivity: %d/%d endpoints reachable",
				successfulEndpoints, successfulEndpoints+failedEndpoints),
		))
	} else if successfulEndpoints > 0 {
		// Full connectivity
		status.AddEvent(types.NewEvent(
			types.EventInfo,
			"ConnectivityHealthy",
			fmt.Sprintf("External connectivity healthy: all %d endpoints reachable", successfulEndpoints),
		))
	}

	return status, nil
}

// updateEndpointFailureCount updates the failure counter for an endpoint and manages conditions.
func (m *ConnectivityMonitor) updateEndpointFailureCount(endpointName string, success bool, status *types.Status) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !success {
		m.endpointFailures[endpointName]++
		failureCount := m.endpointFailures[endpointName]

		// Check if this endpoint has reached the failure threshold
		if failureCount >= m.config.FailureThreshold {
			// Report NetworkUnreachable condition for this specific endpoint
			status.AddCondition(types.NewCondition(
				"NetworkUnreachable",
				types.ConditionTrue,
				"EndpointUnreachable",
				fmt.Sprintf("Endpoint %s has been unreachable for %d consecutive checks", endpointName, failureCount),
			))
		}
	} else {
		// Success - check if we're recovering from failures
		if m.endpointFailures[endpointName] > 0 {
			previousFailures := m.endpointFailures[endpointName]
			m.endpointFailures[endpointName] = 0

			// Add recovery event
			status.AddEvent(types.NewEvent(
				types.EventInfo,
				"EndpointRecovered",
				fmt.Sprintf("Endpoint %s connectivity restored after %d consecutive failures", endpointName, previousFailures),
			))

			// Clear NetworkUnreachable condition for this endpoint
			status.AddCondition(types.NewCondition(
				"NetworkUnreachable",
				types.ConditionFalse,
				"EndpointReachable",
				fmt.Sprintf("Endpoint %s is reachable", endpointName),
			))
		}
	}
}
