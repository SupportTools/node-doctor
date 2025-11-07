// Package network provides network health monitoring capabilities.
package network

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// EndpointConfig defines the configuration for a connectivity endpoint check.
type EndpointConfig struct {
	// Name is a human-readable identifier for this endpoint.
	Name string
	// URL is the endpoint URL to check.
	URL string
	// Method is the HTTP method to use (GET, HEAD, POST, etc.).
	Method string
	// ExpectedStatusCode is the HTTP status code expected for a successful check.
	ExpectedStatusCode int
	// Timeout is the maximum duration for the HTTP request.
	Timeout time.Duration
	// Headers are optional HTTP headers to include in the request.
	Headers map[string]string
	// FollowRedirects determines whether to follow HTTP redirects.
	FollowRedirects bool
}

// EndpointResult contains the result of an endpoint connectivity check.
type EndpointResult struct {
	// Success indicates whether the endpoint check succeeded.
	Success bool
	// StatusCode is the HTTP response status code (0 if request failed).
	StatusCode int
	// ResponseTime is the duration of the HTTP request.
	ResponseTime time.Duration
	// Error contains any error that occurred during the check.
	Error error
	// ErrorType classifies the error (DNS, Connection, HTTP, Timeout, etc.).
	ErrorType string
}

// HTTPClient interface abstracts HTTP endpoint checking for testability.
type HTTPClient interface {
	// CheckEndpoint performs a connectivity check against the given endpoint.
	CheckEndpoint(ctx context.Context, endpoint EndpointConfig) (*EndpointResult, error)
}

// defaultHTTPClient implements HTTPClient using the standard http.Client.
type defaultHTTPClient struct {
	client *http.Client
}

// newDefaultHTTPClient creates a new default HTTP client for connectivity checks.
func newDefaultHTTPClient() HTTPClient {
	return &defaultHTTPClient{
		client: &http.Client{
			// Default timeout - will be overridden per request
			Timeout: 10 * time.Second,
			// Configure transport with reasonable timeouts
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout:   5 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				TLSHandshakeTimeout:   5 * time.Second,
				ResponseHeaderTimeout: 5 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
				MaxIdleConns:          10,
				MaxIdleConnsPerHost:   2,
				IdleConnTimeout:       90 * time.Second,
				// SECURITY NOTE: InsecureSkipVerify is intentionally enabled for connectivity monitoring.
				// This monitor checks network connectivity, NOT certificate validity or service security.
				// Use cases:
				// - Testing connectivity to services with expired/self-signed certificates
				// - Monitoring internal services with private CAs
				// - Detecting network path issues independent of TLS configuration
				//
				// WARNING: Do NOT use this client for sensitive operations or data transmission.
				// For security validation, use a separate certificate validation monitor.
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
			// Limit redirects to prevent redirect loops (max 10 redirects)
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("stopped after 10 redirects")
				}
				// By default, don't follow redirects
				return http.ErrUseLastResponse
			},
		},
	}
}

// CheckEndpoint performs a connectivity check against the given endpoint.
func (c *defaultHTTPClient) CheckEndpoint(ctx context.Context, endpoint EndpointConfig) (*EndpointResult, error) {
	result := &EndpointResult{
		Success: false,
	}

	// Apply endpoint-specific timeout if configured
	if endpoint.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, endpoint.Timeout)
		defer cancel()
	}

	// Note: Redirect handling is configured at the client level (max 10 redirects).
	// By default, redirects are blocked (returns http.ErrUseLastResponse).
	// The FollowRedirects endpoint option is currently not supported to avoid
	// creating new client instances which can cause connection leaks.

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, endpoint.Method, endpoint.URL, nil)
	if err != nil {
		result.Error = err
		result.ErrorType = "RequestCreation"
		return result, fmt.Errorf("failed to create request: %w", err)
	}

	// Add custom headers
	for key, value := range endpoint.Headers {
		req.Header.Set(key, value)
	}

	// Set a user agent if not provided
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "node-doctor-connectivity-monitor/1.0")
	}

	// Perform the request and measure response time
	startTime := time.Now()
	resp, err := c.client.Do(req)
	responseTime := time.Since(startTime)
	result.ResponseTime = responseTime

	if err != nil {
		result.Error = err
		result.ErrorType = classifyHTTPError(err)
		return result, nil
	}
	defer resp.Body.Close()

	// Drain and discard response body to enable connection reuse
	// This is critical for proper HTTP/1.1 and HTTP/2 connection handling
	_, _ = io.Copy(io.Discard, resp.Body)

	// Record status code
	result.StatusCode = resp.StatusCode

	// Check if status code matches expected
	if endpoint.ExpectedStatusCode > 0 {
		if resp.StatusCode == endpoint.ExpectedStatusCode {
			result.Success = true
		} else {
			result.Error = fmt.Errorf("unexpected status code: got %d, expected %d", resp.StatusCode, endpoint.ExpectedStatusCode)
			result.ErrorType = "UnexpectedStatusCode"
		}
	} else {
		// If no expected status code, consider 2xx and 3xx as success
		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			result.Success = true
		} else {
			result.Error = fmt.Errorf("HTTP error status: %d %s", resp.StatusCode, resp.Status)
			result.ErrorType = "HTTPError"
		}
	}

	return result, nil
}

// classifyHTTPError classifies the type of HTTP error for better diagnostics.
func classifyHTTPError(err error) string {
	if err == nil {
		return ""
	}

	// Check for context errors (timeout, cancellation)
	if err == context.DeadlineExceeded {
		return "Timeout"
	}
	if err == context.Canceled {
		return "Canceled"
	}

	// Check for DNS errors
	if netErr, ok := err.(*net.OpError); ok {
		if _, ok := netErr.Err.(*net.DNSError); ok {
			return "DNS"
		}
		// Check for specific DNS error type
		if netErr.Op == "dial" && netErr.Net == "tcp" {
			return "Connection"
		}
	}

	// Direct DNS error check
	if _, ok := err.(*net.DNSError); ok {
		return "DNS"
	}

	// Check for connection refused
	if opErr, ok := err.(*net.OpError); ok {
		if opErr.Op == "dial" {
			return "Connection"
		}
	}

	// Check for TLS errors
	if _, ok := err.(tls.RecordHeaderError); ok {
		return "TLS"
	}

	// Default to generic network error
	return "Network"
}
