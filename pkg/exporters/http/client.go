package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// HTTPClient handles HTTP requests to webhook endpoints with retry logic
type HTTPClient struct {
	client   *http.Client
	endpoint types.WebhookEndpoint
	auth     AuthProvider
}

// NewHTTPClient creates a new HTTP client for a webhook endpoint
func NewHTTPClient(endpoint types.WebhookEndpoint) (*HTTPClient, error) {
	// Create auth provider
	auth, err := NewAuthProvider(endpoint.Auth)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth provider: %w", err)
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: endpoint.Timeout,
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: endpoint.Timeout,
			ExpectContinueTimeout: 1 * time.Second,
			MaxIdleConns:          10,
			MaxIdleConnsPerHost:   2,
			IdleConnTimeout:       90 * time.Second,
		},
	}

	return &HTTPClient{
		client:   client,
		endpoint: endpoint,
		auth:     auth,
	}, nil
}

// SendRequest sends a webhook request with retry logic
func (c *HTTPClient) SendRequest(ctx context.Context, request *WebhookRequest) error {
	if request == nil {
		return fmt.Errorf("request cannot be nil")
	}

	// Validate request
	if err := request.Validate(); err != nil {
		return fmt.Errorf("request validation failed: %w", err)
	}

	var lastErr error
	maxAttempts := c.endpoint.Retry.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 1 // At least one attempt
	}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		// Calculate delay for this attempt (exponential backoff)
		if attempt > 0 {
			delay := c.calculateDelay(attempt)
			log.Printf("[DEBUG] HTTP client retry attempt %d/%d after %v delay for webhook %s",
				attempt+1, maxAttempts, delay, c.endpoint.Name)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
				// Continue with retry
			}
		}

		// Update retry attempt in metadata
		if request.Metadata == nil {
			request.Metadata = make(map[string]interface{})
		}
		request.Metadata["retryAttempt"] = attempt

		// Attempt the request
		err := c.doRequest(ctx, request)
		if err == nil {
			// Success
			if attempt > 0 {
				log.Printf("[INFO] HTTP client succeeded on retry attempt %d/%d for webhook %s",
					attempt+1, maxAttempts, c.endpoint.Name)
			}
			return nil
		}

		lastErr = err

		// Check if error is retryable
		if !c.isRetryableError(err) {
			log.Printf("[WARN] HTTP client non-retryable error for webhook %s: %v", c.endpoint.Name, err)
			break
		}

		// Check if we have more attempts
		if attempt+1 >= maxAttempts {
			log.Printf("[WARN] HTTP client exhausted all %d attempts for webhook %s, last error: %v",
				maxAttempts, c.endpoint.Name, err)
			break
		}

		log.Printf("[WARN] HTTP client attempt %d/%d failed for webhook %s: %v",
			attempt+1, maxAttempts, c.endpoint.Name, err)
	}

	return fmt.Errorf("request failed after %d attempts: %w", maxAttempts, lastErr)
}

// doRequest performs a single HTTP request
func (c *HTTPClient) doRequest(ctx context.Context, request *WebhookRequest) error {
	// Marshal request to JSON
	body, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", c.endpoint.URL, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "node-doctor-http-exporter/1.0")

	// Add custom headers from configuration
	for key, value := range c.endpoint.Headers {
		req.Header.Set(key, value)
	}

	// Add authentication
	if err := c.auth.AddAuth(req); err != nil {
		return fmt.Errorf("failed to add authentication: %w", err)
	}

	// Perform request
	resp, err := c.client.Do(req)
	if err != nil {
		// Check for timeout
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return &TimeoutError{
				Message: "request timeout",
				Timeout: c.endpoint.Timeout,
			}
		}

		// Other network errors
		return &NetworkError{
			Message: "network error",
			Cause:   err,
		}
	}
	defer resp.Body.Close()

	// Read response body (limit to 1MB to prevent abuse)
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return &NetworkError{
			Message: "failed to read response body",
			Cause:   err,
		}
	}

	// Check status code
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		httpErr := &HTTPError{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(responseBody)),
		}

		// Handle rate limiting
		if resp.StatusCode == 429 {
			if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
				if duration, parseErr := time.ParseDuration(retryAfter + "s"); parseErr == nil {
					httpErr.RetryAfter = duration
				}
			}
		}

		return httpErr
	}

	// Try to parse response as JSON (optional)
	var webhookResp WebhookResponse
	if err := json.Unmarshal(responseBody, &webhookResp); err == nil {
		// Successfully parsed, check for application-level errors
		if !webhookResp.Success && webhookResp.Error != "" {
			log.Printf("[WARN] Webhook %s returned application error: %s", c.endpoint.Name, webhookResp.Error)
		}
	}

	log.Printf("[DEBUG] HTTP client successful request to webhook %s: %d %s",
		c.endpoint.Name, resp.StatusCode, resp.Status)

	return nil
}

// calculateDelay calculates the delay for exponential backoff
func (c *HTTPClient) calculateDelay(attempt int) time.Duration {
	// Exponential backoff: baseDelay * 2^(attempt-1)
	multiplier := math.Pow(2, float64(attempt-1))
	delay := time.Duration(float64(c.endpoint.Retry.BaseDelay) * multiplier)

	// Cap at maximum delay
	if delay > c.endpoint.Retry.MaxDelay {
		delay = c.endpoint.Retry.MaxDelay
	}

	// Add jitter (±10% random variation)
	jitter := 0.1
	variation := float64(delay) * jitter * (2*float64(time.Now().UnixNano()%1000)/1000.0 - 1)
	delay += time.Duration(variation)

	// Ensure minimum delay
	if delay < time.Millisecond*100 {
		delay = time.Millisecond * 100
	}

	// Cap again after jitter to ensure we never exceed max delay
	if delay > c.endpoint.Retry.MaxDelay {
		delay = c.endpoint.Retry.MaxDelay
	}

	return delay
}

// isRetryableError determines if an error should trigger a retry
func (c *HTTPClient) isRetryableError(err error) bool {
	if retryable, ok := err.(RetryableError); ok {
		return retryable.IsRetryable()
	}

	// Check for specific error types
	switch e := err.(type) {
	case *HTTPError:
		return e.IsRetryable()
	case *NetworkError:
		return e.IsRetryable()
	case *TimeoutError:
		return e.IsRetryable()
	}

	// Check for common retryable errors
	errStr := strings.ToLower(err.Error())
	retryablePatterns := []string{
		"connection refused",
		"connection reset",
		"connection timeout",
		"temporary failure",
		"network is unreachable",
		"no such host",
		"timeout",
	}

	for _, pattern := range retryablePatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}

	return false
}

// Close closes the HTTP client and releases resources
func (c *HTTPClient) Close() error {
	// Close idle connections
	if transport, ok := c.client.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}
	return nil
}

// GetEndpointInfo returns information about the configured endpoint
func (c *HTTPClient) GetEndpointInfo() map[string]interface{} {
	return map[string]interface{}{
		"name":           c.endpoint.Name,
		"url":            c.endpoint.URL,
		"authType":       c.auth.Type(),
		"timeout":        c.endpoint.Timeout.String(),
		"maxAttempts":    c.endpoint.Retry.MaxAttempts,
		"baseDelay":      c.endpoint.Retry.BaseDelay.String(),
		"maxDelay":       c.endpoint.Retry.MaxDelay.String(),
		"sendStatus":     c.endpoint.SendStatus,
		"sendProblems":   c.endpoint.SendProblems,
		"customHeaders":  len(c.endpoint.Headers),
	}
}