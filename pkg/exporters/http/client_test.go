package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

func TestNewHTTPClient(t *testing.T) {
	endpoint := types.WebhookEndpoint{
		Name:    "test-webhook",
		URL:     "https://example.com/webhook",
		Timeout: 30 * time.Second,
		Auth: types.AuthConfig{
			Type: "none",
		},
		Retry: &types.RetryConfig{
			MaxAttempts: 3,
			BaseDelay:   1 * time.Second,
			MaxDelay:    30 * time.Second,
		},
		SendStatus:   true,
		SendProblems: true,
	}

	client, err := NewHTTPClient(endpoint)
	if err != nil {
		t.Fatalf("Failed to create HTTP client: %v", err)
	}

	if client == nil {
		t.Fatal("Expected non-nil client")
	}

	info := client.GetEndpointInfo()
	if info["name"] != "test-webhook" {
		t.Errorf("Expected webhook name 'test-webhook', got %v", info["name"])
	}
	if info["url"] != "https://example.com/webhook" {
		t.Errorf("Expected URL 'https://example.com/webhook', got %v", info["url"])
	}
}

func TestNewHTTPClientInvalidAuth(t *testing.T) {
	endpoint := types.WebhookEndpoint{
		Name:    "test-webhook",
		URL:     "https://example.com/webhook",
		Timeout: 30 * time.Second,
		Auth: types.AuthConfig{
			Type: "bearer", // Missing token
		},
	}

	_, err := NewHTTPClient(endpoint)
	if err == nil {
		t.Error("Expected error for invalid auth config")
	}
}

func TestHTTPClientSendRequestSuccess(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method and headers
		if r.Method != "POST" {
			t.Errorf("Expected POST method, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}

		// Read and verify request body
		var webhookReq WebhookRequest
		if err := json.NewDecoder(r.Body).Decode(&webhookReq); err != nil {
			t.Errorf("Failed to decode request body: %v", err)
		}

		if webhookReq.Type != "status" {
			t.Errorf("Expected type 'status', got %s", webhookReq.Type)
		}
		if webhookReq.NodeName != "test-node" {
			t.Errorf("Expected nodeName 'test-node', got %s", webhookReq.NodeName)
		}

		// Send success response
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(WebhookResponse{
			Success: true,
			Message: "Request processed successfully",
		})
	}))
	defer server.Close()

	// Create client
	endpoint := types.WebhookEndpoint{
		Name:    "test-webhook",
		URL:     server.URL,
		Timeout: 30 * time.Second,
		Auth: types.AuthConfig{
			Type: "none",
		},
		Retry: &types.RetryConfig{
			MaxAttempts: 3,
			BaseDelay:   100 * time.Millisecond,
			MaxDelay:    1 * time.Second,
		},
	}

	client, err := NewHTTPClient(endpoint)
	if err != nil {
		t.Fatalf("Failed to create HTTP client: %v", err)
	}

	// Create request
	status := &types.Status{
		Source:     "test-monitor",
		Timestamp:  time.Now(),
		Events:     []types.Event{},
		Conditions: []types.Condition{},
	}

	metadata := RequestMetadata{
		ExporterVersion: "1.0.0",
		RequestID:       "test-123",
		WebhookName:     "test-webhook",
	}

	request := NewStatusRequest(status, "test-node", metadata)

	// Send request
	ctx := context.Background()
	err = client.SendRequest(ctx, request)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
}

func TestHTTPClientSendRequestWithAuth(t *testing.T) {
	// Create test server that verifies auth
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token-123" {
			t.Errorf("Expected Bearer token, got %s", auth)
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(WebhookResponse{Success: true})
	}))
	defer server.Close()

	// Create client with bearer auth
	endpoint := types.WebhookEndpoint{
		Name:    "test-webhook",
		URL:     server.URL,
		Timeout: 30 * time.Second,
		Auth: types.AuthConfig{
			Type:  "bearer",
			Token: "test-token-123",
		},
		Retry: &types.RetryConfig{
			MaxAttempts: 1,
			BaseDelay:   100 * time.Millisecond,
			MaxDelay:    1 * time.Second,
		},
	}

	client, err := NewHTTPClient(endpoint)
	if err != nil {
		t.Fatalf("Failed to create HTTP client: %v", err)
	}

	// Create and send request
	status := &types.Status{
		Source:     "test-monitor",
		Timestamp:  time.Now(),
		Events:     []types.Event{},
		Conditions: []types.Condition{},
	}

	request := NewStatusRequest(status, "test-node", RequestMetadata{})

	err = client.SendRequest(context.Background(), request)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
}

func TestHTTPClientSendRequestWithCustomHeaders(t *testing.T) {
	// Create test server that verifies custom headers
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom-Header") != "custom-value" {
			t.Errorf("Expected custom header, got %s", r.Header.Get("X-Custom-Header"))
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(WebhookResponse{Success: true})
	}))
	defer server.Close()

	// Create client with custom headers
	endpoint := types.WebhookEndpoint{
		Name:    "test-webhook",
		URL:     server.URL,
		Timeout: 30 * time.Second,
		Auth: types.AuthConfig{
			Type: "none",
		},
		Headers: map[string]string{
			"X-Custom-Header": "custom-value",
		},
		Retry: &types.RetryConfig{
			MaxAttempts: 1,
			BaseDelay:   100 * time.Millisecond,
			MaxDelay:    1 * time.Second,
		},
	}

	client, err := NewHTTPClient(endpoint)
	if err != nil {
		t.Fatalf("Failed to create HTTP client: %v", err)
	}

	// Create and send request
	status := &types.Status{
		Source:     "test-monitor",
		Timestamp:  time.Now(),
		Events:     []types.Event{},
		Conditions: []types.Condition{},
	}

	request := NewStatusRequest(status, "test-node", RequestMetadata{})

	err = client.SendRequest(context.Background(), request)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
}

func TestHTTPClientSendRequestHTTPError(t *testing.T) {
	// Create test server that returns 500 error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	endpoint := types.WebhookEndpoint{
		Name:    "test-webhook",
		URL:     server.URL,
		Timeout: 30 * time.Second,
		Auth: types.AuthConfig{
			Type: "none",
		},
		Retry: &types.RetryConfig{
			MaxAttempts: 1, // Only one attempt to test error
			BaseDelay:   100 * time.Millisecond,
			MaxDelay:    1 * time.Second,
		},
	}

	client, err := NewHTTPClient(endpoint)
	if err != nil {
		t.Fatalf("Failed to create HTTP client: %v", err)
	}

	// Create request
	status := &types.Status{
		Source:     "test-monitor",
		Timestamp:  time.Now(),
		Events:     []types.Event{},
		Conditions: []types.Condition{},
	}

	request := NewStatusRequest(status, "test-node", RequestMetadata{})

	// Send request - should fail
	err = client.SendRequest(context.Background(), request)
	if err == nil {
		t.Error("Expected error for 500 response")
	}

	// Check if it's an HTTP error (might be wrapped)
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		if httpErr.StatusCode != 500 {
			t.Errorf("Expected status code 500, got %d", httpErr.StatusCode)
		}
		if !httpErr.IsRetryable() {
			t.Error("Expected 500 error to be retryable")
		}
	} else {
		t.Errorf("Expected HTTPError in error chain, got %T: %v", err, err)
	}
}

func TestHTTPClientSendRequestWithRetry(t *testing.T) {
	attempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt < 3 {
			// Return 500 for first two attempts
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			// Succeed on third attempt
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(WebhookResponse{Success: true})
		}
	}))
	defer server.Close()

	endpoint := types.WebhookEndpoint{
		Name:    "test-webhook",
		URL:     server.URL,
		Timeout: 30 * time.Second,
		Auth: types.AuthConfig{
			Type: "none",
		},
		Retry: &types.RetryConfig{
			MaxAttempts: 3,
			BaseDelay:   10 * time.Millisecond, // Short delay for testing
			MaxDelay:    100 * time.Millisecond,
		},
	}

	client, err := NewHTTPClient(endpoint)
	if err != nil {
		t.Fatalf("Failed to create HTTP client: %v", err)
	}

	status := &types.Status{
		Source:     "test-monitor",
		Timestamp:  time.Now(),
		Events:     []types.Event{},
		Conditions: []types.Condition{},
	}

	request := NewStatusRequest(status, "test-node", RequestMetadata{})

	// Should succeed after retries
	err = client.SendRequest(context.Background(), request)
	if err != nil {
		t.Errorf("Expected no error after retries, got: %v", err)
	}

	if attempt != 3 {
		t.Errorf("Expected 3 attempts, got %d", attempt)
	}
}

func TestHTTPClientSendRequestNonRetryableError(t *testing.T) {
	attempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		w.WriteHeader(http.StatusBadRequest) // 400 is not retryable
		w.Write([]byte("Bad Request"))
	}))
	defer server.Close()

	endpoint := types.WebhookEndpoint{
		Name:    "test-webhook",
		URL:     server.URL,
		Timeout: 30 * time.Second,
		Auth: types.AuthConfig{
			Type: "none",
		},
		Retry: &types.RetryConfig{
			MaxAttempts: 3,
			BaseDelay:   10 * time.Millisecond,
			MaxDelay:    100 * time.Millisecond,
		},
	}

	client, err := NewHTTPClient(endpoint)
	if err != nil {
		t.Fatalf("Failed to create HTTP client: %v", err)
	}

	status := &types.Status{
		Source:     "test-monitor",
		Timestamp:  time.Now(),
		Events:     []types.Event{},
		Conditions: []types.Condition{},
	}

	request := NewStatusRequest(status, "test-node", RequestMetadata{})

	// Should fail without retries
	err = client.SendRequest(context.Background(), request)
	if err == nil {
		t.Error("Expected error for 400 response")
	}

	// Should only attempt once (no retries for 400)
	if attempt != 1 {
		t.Errorf("Expected 1 attempt, got %d", attempt)
	}
}

func TestHTTPClientSendRequestTimeout(t *testing.T) {
	// Create server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond) // Delay longer than client timeout
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	endpoint := types.WebhookEndpoint{
		Name:    "test-webhook",
		URL:     server.URL,
		Timeout: 50 * time.Millisecond, // Short timeout
		Auth: types.AuthConfig{
			Type: "none",
		},
		Retry: &types.RetryConfig{
			MaxAttempts: 1,
			BaseDelay:   10 * time.Millisecond,
			MaxDelay:    100 * time.Millisecond,
		},
	}

	client, err := NewHTTPClient(endpoint)
	if err != nil {
		t.Fatalf("Failed to create HTTP client: %v", err)
	}

	status := &types.Status{
		Source:     "test-monitor",
		Timestamp:  time.Now(),
		Events:     []types.Event{},
		Conditions: []types.Condition{},
	}

	request := NewStatusRequest(status, "test-node", RequestMetadata{})

	err = client.SendRequest(context.Background(), request)
	if err == nil {
		t.Error("Expected timeout error")
	}

	// Check if it's a timeout error or network error
	isTimeoutError := false
	var timeoutErr *TimeoutError
	var networkErr *NetworkError
	if errors.As(err, &timeoutErr) {
		isTimeoutError = true
		if !timeoutErr.IsRetryable() {
			t.Error("Expected timeout error to be retryable")
		}
	} else if errors.As(err, &networkErr) {
		// Network error due to timeout is also acceptable
		if !networkErr.IsRetryable() {
			t.Error("Expected network error to be retryable")
		}
	} else if strings.Contains(err.Error(), "timeout") {
		// Generic timeout error is also acceptable
		isTimeoutError = true
	}

	if !isTimeoutError && !strings.Contains(err.Error(), "timeout") {
		t.Errorf("Expected timeout-related error, got: %v", err)
	}
}

func TestHTTPClientCalculateDelay(t *testing.T) {
	endpoint := types.WebhookEndpoint{
		Retry: &types.RetryConfig{
			BaseDelay: 1 * time.Second,
			MaxDelay:  30 * time.Second,
		},
	}

	client, _ := NewHTTPClient(endpoint)

	// Test exponential backoff
	delay1 := client.calculateDelay(1)
	delay2 := client.calculateDelay(2)
	delay3 := client.calculateDelay(3)

	if delay1 >= delay2 {
		t.Errorf("Expected delay1 < delay2, got %v >= %v", delay1, delay2)
	}
	if delay2 >= delay3 {
		t.Errorf("Expected delay2 < delay3, got %v >= %v", delay2, delay3)
	}

	// Test max delay cap
	largeDelay := client.calculateDelay(10)
	if largeDelay > endpoint.Retry.MaxDelay+100*time.Millisecond { // Allow for jitter
		t.Errorf("Expected delay to be capped at max delay, got %v", largeDelay)
	}

	// Test minimum delay
	if delay1 < 100*time.Millisecond {
		t.Errorf("Expected minimum delay of 100ms, got %v", delay1)
	}
}

func TestHTTPClientIsRetryableError(t *testing.T) {
	endpoint := types.WebhookEndpoint{}
	client, _ := NewHTTPClient(endpoint)

	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{
			name:      "HTTP 500 error",
			err:       &HTTPError{StatusCode: 500},
			retryable: true,
		},
		{
			name:      "HTTP 400 error",
			err:       &HTTPError{StatusCode: 400},
			retryable: false,
		},
		{
			name:      "HTTP 429 error",
			err:       &HTTPError{StatusCode: 429},
			retryable: true,
		},
		{
			name:      "Network error",
			err:       &NetworkError{Message: "connection failed"},
			retryable: true,
		},
		{
			name:      "Timeout error",
			err:       &TimeoutError{Message: "timeout"},
			retryable: true,
		},
		{
			name:      "Connection refused",
			err:       fmt.Errorf("connection refused"),
			retryable: true,
		},
		{
			name:      "Unknown error",
			err:       fmt.Errorf("unknown error"),
			retryable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			retryable := client.isRetryableError(tt.err)
			if retryable != tt.retryable {
				t.Errorf("Expected retryable=%v for error %v, got %v", tt.retryable, tt.err, retryable)
			}
		})
	}
}

func TestHTTPClientValidation(t *testing.T) {
	endpoint := types.WebhookEndpoint{
		Name:    "test-webhook",
		URL:     "https://example.com/webhook",
		Timeout: 30 * time.Second,
		Auth: types.AuthConfig{
			Type: "none",
		},
		Retry: &types.RetryConfig{
			MaxAttempts: 3,
			BaseDelay:   1 * time.Second,
			MaxDelay:    30 * time.Second,
		},
	}

	client, err := NewHTTPClient(endpoint)
	if err != nil {
		t.Fatalf("Failed to create HTTP client: %v", err)
	}

	// Test nil request
	err = client.SendRequest(context.Background(), nil)
	if err == nil {
		t.Error("Expected error for nil request")
	}

	// Test invalid request
	invalidRequest := &WebhookRequest{
		Type: "invalid",
	}

	err = client.SendRequest(context.Background(), invalidRequest)
	if err == nil {
		t.Error("Expected error for invalid request")
	}
}

func TestHTTPClientClose(t *testing.T) {
	endpoint := types.WebhookEndpoint{
		Name:    "test-webhook",
		URL:     "https://example.com/webhook",
		Timeout: 30 * time.Second,
		Auth: types.AuthConfig{
			Type: "none",
		},
	}

	client, err := NewHTTPClient(endpoint)
	if err != nil {
		t.Fatalf("Failed to create HTTP client: %v", err)
	}

	err = client.Close()
	if err != nil {
		t.Errorf("Expected no error closing client, got: %v", err)
	}
}
