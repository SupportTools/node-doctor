package http

import (
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// WebhookRequest represents the JSON payload sent to webhooks
type WebhookRequest struct {
	// Type indicates the payload type: "status" or "problem"
	Type string `json:"type"`

	// Timestamp when the payload was created
	Timestamp time.Time `json:"timestamp"`

	// NodeName identifies the source node
	NodeName string `json:"nodeName"`

	// Status data (only present when Type is "status")
	Status *types.Status `json:"status,omitempty"`

	// Problem data (only present when Type is "problem")
	Problem *types.Problem `json:"problem,omitempty"`

	// Metadata provides additional context
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// WebhookResponse represents the expected response from webhooks
type WebhookResponse struct {
	// Success indicates if the webhook processed the request successfully
	Success bool `json:"success"`

	// Message provides additional information about the processing result
	Message string `json:"message,omitempty"`

	// Error contains error details if Success is false
	Error string `json:"error,omitempty"`
}

// RequestMetadata contains additional context for webhook requests
type RequestMetadata struct {
	// ExporterVersion identifies the HTTP exporter version
	ExporterVersion string `json:"exporterVersion"`

	// RequestID provides a unique identifier for tracing
	RequestID string `json:"requestId"`

	// RetryAttempt indicates the current retry attempt (0 for initial request)
	RetryAttempt int `json:"retryAttempt"`

	// WebhookName identifies which webhook configuration was used
	WebhookName string `json:"webhookName"`
}

// NewStatusRequest creates a webhook request for status data
func NewStatusRequest(status *types.Status, nodeName string, metadata RequestMetadata) *WebhookRequest {
	req := &WebhookRequest{
		Type:      "status",
		Timestamp: time.Now(),
		NodeName:  nodeName,
		Status:    status,
		Metadata:  make(map[string]interface{}),
	}

	// Add metadata fields
	req.Metadata["exporterVersion"] = metadata.ExporterVersion
	req.Metadata["requestId"] = metadata.RequestID
	req.Metadata["retryAttempt"] = metadata.RetryAttempt
	req.Metadata["webhookName"] = metadata.WebhookName

	return req
}

// NewProblemRequest creates a webhook request for problem data
func NewProblemRequest(problem *types.Problem, nodeName string, metadata RequestMetadata) *WebhookRequest {
	req := &WebhookRequest{
		Type:      "problem",
		Timestamp: time.Now(),
		NodeName:  nodeName,
		Problem:   problem,
		Metadata:  make(map[string]interface{}),
	}

	// Add metadata fields
	req.Metadata["exporterVersion"] = metadata.ExporterVersion
	req.Metadata["requestId"] = metadata.RequestID
	req.Metadata["retryAttempt"] = metadata.RetryAttempt
	req.Metadata["webhookName"] = metadata.WebhookName

	return req
}

// Validate validates the webhook request
func (w *WebhookRequest) Validate() error {
	if w.Type == "" {
		return &ValidationError{Field: "type", Message: "type is required"}
	}

	if w.Type != "status" && w.Type != "problem" {
		return &ValidationError{Field: "type", Message: "type must be 'status' or 'problem'"}
	}

	if w.NodeName == "" {
		return &ValidationError{Field: "nodeName", Message: "nodeName is required"}
	}

	if w.Timestamp.IsZero() {
		return &ValidationError{Field: "timestamp", Message: "timestamp is required"}
	}

	// Type-specific validation
	switch w.Type {
	case "status":
		if w.Status == nil {
			return &ValidationError{Field: "status", Message: "status is required when type is 'status'"}
		}
		if err := w.Status.Validate(); err != nil {
			return &ValidationError{Field: "status", Message: "status validation failed: " + err.Error()}
		}
	case "problem":
		if w.Problem == nil {
			return &ValidationError{Field: "problem", Message: "problem is required when type is 'problem'"}
		}
		if err := w.Problem.Validate(); err != nil {
			return &ValidationError{Field: "problem", Message: "problem validation failed: " + err.Error()}
		}
	}

	return nil
}

// ValidationError represents a validation error
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Field != "" {
		return e.Field + ": " + e.Message
	}
	return e.Message
}

// HTTPError represents an HTTP-related error
type HTTPError struct {
	StatusCode int
	Message    string
	RetryAfter time.Duration // For 429 responses
}

func (e *HTTPError) Error() string {
	return e.Message
}

// IsRetryable returns true if the error should trigger a retry
func (e *HTTPError) IsRetryable() bool {
	// Retry on 5xx errors and specific 4xx errors
	return e.StatusCode >= 500 || e.StatusCode == 408 || e.StatusCode == 429
}

// NetworkError represents a network-related error
type NetworkError struct {
	Message string
	Cause   error
}

func (e *NetworkError) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

func (e *NetworkError) Unwrap() error {
	return e.Cause
}

// IsRetryable returns true since network errors are typically transient
func (e *NetworkError) IsRetryable() bool {
	return true
}

// TimeoutError represents a timeout error
type TimeoutError struct {
	Message string
	Timeout time.Duration
}

func (e *TimeoutError) Error() string {
	return e.Message
}

// IsRetryable returns true since timeouts are typically transient
func (e *TimeoutError) IsRetryable() bool {
	return true
}

// RetryableError interface for errors that can be retried
type RetryableError interface {
	error
	IsRetryable() bool
}
