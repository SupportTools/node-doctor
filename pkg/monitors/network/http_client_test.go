package network

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestDefaultHTTPClient_CheckEndpoint_Success tests successful endpoint checks.
func TestDefaultHTTPClient_CheckEndpoint_Success(t *testing.T) {
	tests := []struct {
		name               string
		method             string
		expectedStatusCode int
		serverStatusCode   int
		serverDelay        time.Duration
		wantSuccess        bool
		wantErrorType      string
	}{
		{
			name:               "HEAD request with exact status match",
			method:             "HEAD",
			expectedStatusCode: 200,
			serverStatusCode:   200,
			wantSuccess:        true,
		},
		{
			name:               "GET request with exact status match",
			method:             "GET",
			expectedStatusCode: 200,
			serverStatusCode:   200,
			wantSuccess:        true,
		},
		{
			name:               "Status mismatch",
			method:             "HEAD",
			expectedStatusCode: 200,
			serverStatusCode:   404,
			wantSuccess:        false,
			wantErrorType:      "UnexpectedStatusCode",
		},
		{
			name:               "No expected status code - 2xx success",
			method:             "HEAD",
			expectedStatusCode: 0,
			serverStatusCode:   200,
			wantSuccess:        true,
		},
		{
			name:               "No expected status code - 3xx success",
			method:             "HEAD",
			expectedStatusCode: 0,
			serverStatusCode:   301,
			wantSuccess:        true,
		},
		{
			name:               "No expected status code - 4xx failure",
			method:             "HEAD",
			expectedStatusCode: 0,
			serverStatusCode:   404,
			wantSuccess:        false,
			wantErrorType:      "HTTPError",
		},
		{
			name:               "No expected status code - 5xx failure",
			method:             "HEAD",
			expectedStatusCode: 0,
			serverStatusCode:   500,
			wantSuccess:        false,
			wantErrorType:      "HTTPError",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify method
				if r.Method != tt.method {
					t.Errorf("unexpected method: got %s, want %s", r.Method, tt.method)
				}

				// Simulate delay if specified
				if tt.serverDelay > 0 {
					time.Sleep(tt.serverDelay)
				}

				w.WriteHeader(tt.serverStatusCode)
			}))
			defer server.Close()

			// Create client and endpoint config
			client := newDefaultHTTPClient()
			endpoint := EndpointConfig{
				Name:               "test-endpoint",
				URL:                server.URL,
				Method:             tt.method,
				ExpectedStatusCode: tt.expectedStatusCode,
				Timeout:            5 * time.Second,
			}

			// Check endpoint
			ctx := context.Background()
			result, err := client.CheckEndpoint(ctx, endpoint)

			// Verify no unexpected error
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify result
			if result.Success != tt.wantSuccess {
				t.Errorf("success = %v, want %v", result.Success, tt.wantSuccess)
			}

			if result.StatusCode != tt.serverStatusCode {
				t.Errorf("status code = %d, want %d", result.StatusCode, tt.serverStatusCode)
			}

			if !tt.wantSuccess && result.ErrorType != tt.wantErrorType {
				t.Errorf("error type = %s, want %s", result.ErrorType, tt.wantErrorType)
			}

			if result.ResponseTime <= 0 {
				t.Error("response time should be positive")
			}
		})
	}
}

// TestDefaultHTTPClient_CheckEndpoint_Timeout tests timeout handling.
func TestDefaultHTTPClient_CheckEndpoint_Timeout(t *testing.T) {
	// Create test server with delay
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newDefaultHTTPClient()
	endpoint := EndpointConfig{
		Name:               "test-endpoint",
		URL:                server.URL,
		Method:             "HEAD",
		ExpectedStatusCode: 200,
		Timeout:            100 * time.Millisecond, // Short timeout
	}

	ctx := context.Background()
	result, err := client.CheckEndpoint(ctx, endpoint)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Success {
		t.Error("expected timeout failure")
	}

	if result.ErrorType != "Timeout" && result.ErrorType != "Network" {
		t.Errorf("error type = %s, want Timeout or Network", result.ErrorType)
	}

	if result.Error == nil {
		t.Error("expected error for timeout")
	}
}

// TestDefaultHTTPClient_CheckEndpoint_ContextCancellation tests context cancellation.
func TestDefaultHTTPClient_CheckEndpoint_ContextCancellation(t *testing.T) {
	// Create test server with delay
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newDefaultHTTPClient()
	endpoint := EndpointConfig{
		Name:               "test-endpoint",
		URL:                server.URL,
		Method:             "HEAD",
		ExpectedStatusCode: 200,
		Timeout:            5 * time.Second,
	}

	// Create context and cancel immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := client.CheckEndpoint(ctx, endpoint)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Success {
		t.Error("expected cancellation failure")
	}

	if result.ErrorType != "Canceled" && result.ErrorType != "Network" {
		t.Errorf("error type = %s, want Canceled or Network", result.ErrorType)
	}
}

// TestDefaultHTTPClient_CheckEndpoint_CustomHeaders tests custom header support.
func TestDefaultHTTPClient_CheckEndpoint_CustomHeaders(t *testing.T) {
	expectedHeaders := map[string]string{
		"X-Custom-Header": "custom-value",
		"Authorization":   "Bearer token123",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify custom headers
		for key, expectedValue := range expectedHeaders {
			actualValue := r.Header.Get(key)
			if actualValue != expectedValue {
				t.Errorf("header %s = %s, want %s", key, actualValue, expectedValue)
			}
		}

		// Verify user agent is set
		if r.Header.Get("User-Agent") == "" {
			t.Error("User-Agent header should be set")
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newDefaultHTTPClient()
	endpoint := EndpointConfig{
		Name:               "test-endpoint",
		URL:                server.URL,
		Method:             "GET",
		ExpectedStatusCode: 200,
		Headers:            expectedHeaders,
	}

	ctx := context.Background()
	result, err := client.CheckEndpoint(ctx, endpoint)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Success {
		t.Errorf("expected success, got failure: %v", result.Error)
	}
}

// TestDefaultHTTPClient_CheckEndpoint_InvalidURL tests invalid URL handling.
func TestDefaultHTTPClient_CheckEndpoint_InvalidURL(t *testing.T) {
	client := newDefaultHTTPClient()
	endpoint := EndpointConfig{
		Name:               "test-endpoint",
		URL:                "://invalid-url",
		Method:             "GET",
		ExpectedStatusCode: 200,
	}

	ctx := context.Background()
	result, err := client.CheckEndpoint(ctx, endpoint)

	if err == nil {
		t.Fatal("expected error for invalid URL")
	}

	if result.Success {
		t.Error("expected failure for invalid URL")
	}

	if result.ErrorType != "RequestCreation" {
		t.Errorf("error type = %s, want RequestCreation", result.ErrorType)
	}
}

// TestDefaultHTTPClient_CheckEndpoint_DNSFailure tests DNS failure handling.
func TestDefaultHTTPClient_CheckEndpoint_DNSFailure(t *testing.T) {
	client := newDefaultHTTPClient()
	endpoint := EndpointConfig{
		Name:               "test-endpoint",
		URL:                "http://this-domain-does-not-exist-12345.invalid",
		Method:             "GET",
		ExpectedStatusCode: 200,
		Timeout:            2 * time.Second,
	}

	ctx := context.Background()
	result, err := client.CheckEndpoint(ctx, endpoint)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Success {
		t.Error("expected DNS failure")
	}

	// Should be classified as DNS or Network error
	if result.ErrorType != "DNS" && result.ErrorType != "Network" {
		t.Errorf("error type = %s, want DNS or Network", result.ErrorType)
	}

	if result.Error == nil {
		t.Error("expected error for DNS failure")
	}
}

// TestDefaultHTTPClient_CheckEndpoint_RedirectHandling tests redirect handling.
func TestDefaultHTTPClient_CheckEndpoint_RedirectHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/redirect")
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()

	client := newDefaultHTTPClient()
	endpoint := EndpointConfig{
		Name:    "test-endpoint",
		URL:     server.URL,
		Method:  "GET",
		Timeout: 5 * time.Second,
	}

	ctx := context.Background()
	result, err := client.CheckEndpoint(ctx, endpoint)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Default behavior: redirects are blocked, returns 3xx status
	// 3xx is considered success when no expected status code
	if !result.Success {
		t.Errorf("expected success for 3xx status (got error: %v)", result.Error)
	}

	if result.StatusCode != 302 {
		t.Errorf("status code = %d, want 302", result.StatusCode)
	}
}

// TestClassifyHTTPError tests error classification.
func TestClassifyHTTPError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantType string
	}{
		{
			name:     "nil error",
			err:      nil,
			wantType: "",
		},
		{
			name:     "deadline exceeded",
			err:      context.DeadlineExceeded,
			wantType: "Timeout",
		},
		{
			name:     "context canceled",
			err:      context.Canceled,
			wantType: "Canceled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errorType := classifyHTTPError(tt.err)
			if errorType != tt.wantType {
				t.Errorf("classifyHTTPError() = %s, want %s", errorType, tt.wantType)
			}
		})
	}
}

// TestClassifyHTTPError_NetworkErrors tests network-related error classification.
func TestClassifyHTTPError_NetworkErrors(t *testing.T) {
	// Test DNS error
	dnsErr := &net.DNSError{
		Err:  "no such host",
		Name: "invalid.host",
	}
	if got := classifyHTTPError(dnsErr); got != "DNS" {
		t.Errorf("classifyHTTPError(DNSError) = %s, want DNS", got)
	}

	// Test OpError with dial
	opErr := &net.OpError{
		Op:  "dial",
		Net: "tcp",
		Err: &net.DNSError{Err: "lookup failed"},
	}
	result := classifyHTTPError(opErr)
	if result != "DNS" && result != "Connection" {
		t.Errorf("classifyHTTPError(OpError with DNS) = %s, want DNS or Connection", result)
	}

	// Test OpError with dial operation
	dialErr := &net.OpError{
		Op:  "dial",
		Net: "tcp",
		Err: &net.AddrError{Err: "connection refused"},
	}
	if got := classifyHTTPError(dialErr); got != "Connection" {
		t.Errorf("classifyHTTPError(dial OpError) = %s, want Connection", got)
	}

	// Test generic error falls back to Network
	genericErr := &net.OpError{
		Op:  "read",
		Net: "tcp",
		Err: &net.AddrError{Err: "connection reset"},
	}
	if got := classifyHTTPError(genericErr); got != "Network" {
		t.Errorf("classifyHTTPError(generic network error) = %s, want Network", got)
	}
}

// mockHTTPClient implements HTTPClient for testing.
type mockHTTPClient struct {
	result *EndpointResult
	err    error
	delay  time.Duration
}

func (m *mockHTTPClient) CheckEndpoint(ctx context.Context, endpoint EndpointConfig) (*EndpointResult, error) {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return &EndpointResult{
				Success:   false,
				Error:     ctx.Err(),
				ErrorType: "Canceled",
			}, nil
		}
	}
	return m.result, m.err
}

// TestMockHTTPClient tests the mock implementation.
func TestMockHTTPClient(t *testing.T) {
	expectedResult := &EndpointResult{
		Success:      true,
		StatusCode:   200,
		ResponseTime: 50 * time.Millisecond,
	}

	mock := &mockHTTPClient{
		result: expectedResult,
	}

	endpoint := EndpointConfig{
		URL:    "http://example.com",
		Method: "HEAD",
	}

	ctx := context.Background()
	result, err := mock.CheckEndpoint(ctx, endpoint)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != expectedResult {
		t.Error("mock should return expected result")
	}
}
