package http

import (
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

func TestNewStatusRequest(t *testing.T) {
	status := &types.Status{
		Source:    "test-monitor",
		Timestamp: time.Now(),
		Events:    []types.Event{},
		Conditions: []types.Condition{},
	}

	metadata := RequestMetadata{
		ExporterVersion: "1.0.0",
		RequestID:       "test-123",
		RetryAttempt:    0,
		WebhookName:     "test-webhook",
	}

	req := NewStatusRequest(status, "test-node", metadata)

	if req.Type != "status" {
		t.Errorf("Expected type 'status', got %s", req.Type)
	}
	if req.NodeName != "test-node" {
		t.Errorf("Expected nodeName 'test-node', got %s", req.NodeName)
	}
	if req.Status != status {
		t.Errorf("Status not properly set")
	}
	if req.Problem != nil {
		t.Errorf("Problem should be nil for status request")
	}
	if req.Metadata["exporterVersion"] != "1.0.0" {
		t.Errorf("Metadata exporterVersion not set correctly")
	}
	if req.Metadata["requestId"] != "test-123" {
		t.Errorf("Metadata requestId not set correctly")
	}
}

func TestNewProblemRequest(t *testing.T) {
	problem := &types.Problem{
		Type:       "test-problem",
		Resource:   "test-resource",
		Severity:   types.ProblemCritical,
		Message:    "Test problem",
		DetectedAt: time.Now(),
		Metadata:   make(map[string]string),
	}

	metadata := RequestMetadata{
		ExporterVersion: "1.0.0",
		RequestID:       "test-456",
		RetryAttempt:    1,
		WebhookName:     "test-webhook",
	}

	req := NewProblemRequest(problem, "test-node", metadata)

	if req.Type != "problem" {
		t.Errorf("Expected type 'problem', got %s", req.Type)
	}
	if req.NodeName != "test-node" {
		t.Errorf("Expected nodeName 'test-node', got %s", req.NodeName)
	}
	if req.Problem != problem {
		t.Errorf("Problem not properly set")
	}
	if req.Status != nil {
		t.Errorf("Status should be nil for problem request")
	}
	if req.Metadata["retryAttempt"] != 1 {
		t.Errorf("Metadata retryAttempt not set correctly")
	}
}

func TestWebhookRequestValidate(t *testing.T) {
	tests := []struct {
		name        string
		request     *WebhookRequest
		expectError bool
		errorField  string
	}{
		{
			name: "valid status request",
			request: &WebhookRequest{
				Type:      "status",
				Timestamp: time.Now(),
				NodeName:  "test-node",
				Status: &types.Status{
					Source:     "test-monitor",
					Timestamp:  time.Now(),
					Events:     []types.Event{},
					Conditions: []types.Condition{},
				},
			},
			expectError: false,
		},
		{
			name: "valid problem request",
			request: &WebhookRequest{
				Type:      "problem",
				Timestamp: time.Now(),
				NodeName:  "test-node",
				Problem: &types.Problem{
					Type:       "test-problem",
					Resource:   "test-resource",
					Severity:   types.ProblemCritical,
					Message:    "Test problem",
					DetectedAt: time.Now(),
					Metadata:   make(map[string]string),
				},
			},
			expectError: false,
		},
		{
			name: "missing type",
			request: &WebhookRequest{
				Timestamp: time.Now(),
				NodeName:  "test-node",
			},
			expectError: true,
			errorField:  "type",
		},
		{
			name: "invalid type",
			request: &WebhookRequest{
				Type:      "invalid",
				Timestamp: time.Now(),
				NodeName:  "test-node",
			},
			expectError: true,
			errorField:  "type",
		},
		{
			name: "missing node name",
			request: &WebhookRequest{
				Type:      "status",
				Timestamp: time.Now(),
			},
			expectError: true,
			errorField:  "nodeName",
		},
		{
			name: "missing timestamp",
			request: &WebhookRequest{
				Type:     "status",
				NodeName: "test-node",
			},
			expectError: true,
			errorField:  "timestamp",
		},
		{
			name: "status type without status data",
			request: &WebhookRequest{
				Type:      "status",
				Timestamp: time.Now(),
				NodeName:  "test-node",
			},
			expectError: true,
			errorField:  "status",
		},
		{
			name: "problem type without problem data",
			request: &WebhookRequest{
				Type:      "problem",
				Timestamp: time.Now(),
				NodeName:  "test-node",
			},
			expectError: true,
			errorField:  "problem",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.request.Validate()
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
					return
				}
				if validationErr, ok := err.(*ValidationError); ok {
					if validationErr.Field != tt.errorField {
						t.Errorf("Expected error field %s, got %s", tt.errorField, validationErr.Field)
					}
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

func TestValidationError(t *testing.T) {
	err := &ValidationError{
		Field:   "testField",
		Message: "test message",
	}

	expected := "testField: test message"
	if err.Error() != expected {
		t.Errorf("Expected error message %q, got %q", expected, err.Error())
	}

	// Test without field
	err = &ValidationError{
		Message: "test message",
	}

	expected = "test message"
	if err.Error() != expected {
		t.Errorf("Expected error message %q, got %q", expected, err.Error())
	}
}

func TestHTTPError(t *testing.T) {
	err := &HTTPError{
		StatusCode: 500,
		Message:    "Internal Server Error",
	}

	if !err.IsRetryable() {
		t.Errorf("Expected 5xx error to be retryable")
	}

	err = &HTTPError{
		StatusCode: 400,
		Message:    "Bad Request",
	}

	if err.IsRetryable() {
		t.Errorf("Expected 400 error to not be retryable")
	}

	err = &HTTPError{
		StatusCode: 429,
		Message:    "Too Many Requests",
	}

	if !err.IsRetryable() {
		t.Errorf("Expected 429 error to be retryable")
	}

	err = &HTTPError{
		StatusCode: 408,
		Message:    "Request Timeout",
	}

	if !err.IsRetryable() {
		t.Errorf("Expected 408 error to be retryable")
	}
}

func TestNetworkError(t *testing.T) {
	cause := &ValidationError{Message: "connection refused"}
	err := &NetworkError{
		Message: "network error",
		Cause:   cause,
	}

	if !err.IsRetryable() {
		t.Errorf("Expected network error to be retryable")
	}

	expected := "network error: connection refused"
	if err.Error() != expected {
		t.Errorf("Expected error message %q, got %q", expected, err.Error())
	}

	if err.Unwrap() != cause {
		t.Errorf("Expected unwrapped error to be the cause")
	}

	// Test without cause
	err = &NetworkError{
		Message: "network error",
	}

	expected = "network error"
	if err.Error() != expected {
		t.Errorf("Expected error message %q, got %q", expected, err.Error())
	}
}

func TestTimeoutError(t *testing.T) {
	err := &TimeoutError{
		Message: "request timeout",
		Timeout: 30 * time.Second,
	}

	if !err.IsRetryable() {
		t.Errorf("Expected timeout error to be retryable")
	}

	expected := "request timeout"
	if err.Error() != expected {
		t.Errorf("Expected error message %q, got %q", expected, err.Error())
	}
}