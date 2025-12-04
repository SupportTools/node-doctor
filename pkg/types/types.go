// Package types defines the core interfaces and types for Node Doctor.
// Based on architecture.md specification.
package types

import (
	"context"
	"fmt"
	"time"
)

// Monitor is the interface that all monitors must implement.
// Monitors detect problems on the node and report them via a channel.
type Monitor interface {
	// Start begins the monitoring process and returns a channel for status updates.
	// The monitor runs asynchronously and sends Status updates through the channel.
	Start() (<-chan *Status, error)

	// Stop gracefully stops the monitor.
	Stop()
}

// Status represents the current state reported by a monitor.
type Status struct {
	// Source identifies the monitor that generated this status.
	Source string

	// Events are notable occurrences detected by the monitor.
	Events []Event

	// Conditions represent the current state of the monitored resource.
	Conditions []Condition

	// Timestamp when this status was generated.
	Timestamp time.Time

	// Metadata holds monitor-specific observability data (metrics, diagnostics, etc.)
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// EventSeverity represents the severity level of an event.
type EventSeverity string

const (
	// EventInfo indicates an informational event with no action required.
	EventInfo EventSeverity = "Info"

	// EventWarning indicates a warning that may require attention.
	EventWarning EventSeverity = "Warning"

	// EventError indicates an error condition that requires immediate attention.
	EventError EventSeverity = "Error"
)

// Event represents a discrete occurrence detected by a monitor.
type Event struct {
	// Severity indicates the importance of the event (Info, Warning, Error).
	Severity EventSeverity

	// Timestamp when the event occurred.
	Timestamp time.Time

	// Reason is a short, machine-readable string that describes the event.
	Reason string

	// Message is a human-readable description of the event.
	Message string
}

// Condition represents the current state of a monitored resource.
type Condition struct {
	// Type is the type of condition (e.g., "KubeletReady", "DiskPressure").
	Type string

	// Status is the current status of the condition (True, False, Unknown).
	Status ConditionStatus

	// Transition is when the condition last transitioned.
	Transition time.Time

	// Reason is a brief machine-readable string explaining the condition.
	Reason string

	// Message is a human-readable explanation of the condition.
	Message string
}

// ConditionStatus represents the status of a condition.
type ConditionStatus string

const (
	// ConditionTrue indicates the condition is true/healthy.
	ConditionTrue ConditionStatus = "True"

	// ConditionFalse indicates the condition is false/unhealthy.
	ConditionFalse ConditionStatus = "False"

	// ConditionUnknown indicates the condition status cannot be determined.
	ConditionUnknown ConditionStatus = "Unknown"
)

// ProblemSeverity represents the severity level of a problem.
type ProblemSeverity string

const (
	// ProblemInfo indicates an informational problem with no immediate impact.
	ProblemInfo ProblemSeverity = "Info"

	// ProblemWarning indicates a problem that may impact node health if not addressed.
	ProblemWarning ProblemSeverity = "Warning"

	// ProblemCritical indicates a critical problem requiring immediate remediation.
	ProblemCritical ProblemSeverity = "Critical"
)

// Problem represents an issue detected that may require remediation.
type Problem struct {
	// Type categorizes the problem (e.g., "systemd-service-failed").
	Type string

	// Resource identifies the affected resource (e.g., "kubelet.service").
	Resource string

	// Severity indicates how critical the problem is.
	Severity ProblemSeverity

	// Message describes the problem in detail.
	Message string

	// DetectedAt is when the problem was first detected.
	DetectedAt time.Time

	// Metadata contains additional context about the problem.
	Metadata map[string]string
}

// Remediator is the interface for components that can fix problems.
type Remediator interface {
	// CanRemediate returns true if this remediator can handle the given problem.
	CanRemediate(problem Problem) bool

	// Remediate attempts to fix the problem.
	// Returns an error if remediation fails or is not allowed (cooldown, rate limit, etc.).
	Remediate(ctx context.Context, problem Problem) error

	// GetCooldown returns the minimum time between remediation attempts for this remediator.
	GetCooldown() time.Duration
}

// Exporter is the interface for components that export status and problems.
// Exporters publish information to external systems (Prometheus, Kubernetes API, logs).
type Exporter interface {
	// ExportStatus publishes a status update.
	ExportStatus(ctx context.Context, status *Status) error

	// ExportProblem publishes a problem report.
	ExportProblem(ctx context.Context, problem *Problem) error
}

// Constructor functions

// NewEvent creates a new Event with the specified parameters.
// Timestamp is automatically set to the current time.
func NewEvent(severity EventSeverity, reason, message string) Event {
	return Event{
		Severity:  severity,
		Timestamp: time.Now(),
		Reason:    reason,
		Message:   message,
	}
}

// NewCondition creates a new Condition with the specified parameters.
// Transition time is automatically set to the current time.
func NewCondition(conditionType string, status ConditionStatus, reason, message string) Condition {
	return Condition{
		Type:       conditionType,
		Status:     status,
		Transition: time.Now(),
		Reason:     reason,
		Message:    message,
	}
}

// NewStatus creates a new Status with the specified source.
// Timestamp is automatically set to the current time.
// Events and Conditions slices are initialized as empty.
func NewStatus(source string) *Status {
	return &Status{
		Source:     source,
		Timestamp:  time.Now(),
		Events:     []Event{},
		Conditions: []Condition{},
	}
}

// NewProblem creates a new Problem with the specified parameters.
// DetectedAt time is automatically set to the current time.
// Metadata map is initialized as empty.
func NewProblem(problemType, resource string, severity ProblemSeverity, message string) *Problem {
	return &Problem{
		Type:       problemType,
		Resource:   resource,
		Severity:   severity,
		Message:    message,
		DetectedAt: time.Now(),
		Metadata:   make(map[string]string),
	}
}

// Validation methods

// Validate checks if the Event has all required fields populated.
// Returns an error if any required field is missing or invalid.
func (e *Event) Validate() error {
	if e.Severity == "" {
		return fmt.Errorf("event severity is required")
	}
	if e.Severity != EventInfo && e.Severity != EventWarning && e.Severity != EventError {
		return fmt.Errorf("invalid event severity: %s", e.Severity)
	}
	if e.Reason == "" {
		return fmt.Errorf("event reason is required")
	}
	if e.Message == "" {
		return fmt.Errorf("event message is required")
	}
	if e.Timestamp.IsZero() {
		return fmt.Errorf("event timestamp is required")
	}
	return nil
}

// Validate checks if the Condition has all required fields populated.
// Returns an error if any required field is missing or invalid.
func (c *Condition) Validate() error {
	if c.Type == "" {
		return fmt.Errorf("condition type is required")
	}
	if c.Status == "" {
		return fmt.Errorf("condition status is required")
	}
	if c.Status != ConditionTrue && c.Status != ConditionFalse && c.Status != ConditionUnknown {
		return fmt.Errorf("invalid condition status: %s", c.Status)
	}
	if c.Reason == "" {
		return fmt.Errorf("condition reason is required")
	}
	if c.Message == "" {
		return fmt.Errorf("condition message is required")
	}
	if c.Transition.IsZero() {
		return fmt.Errorf("condition transition time is required")
	}
	return nil
}

// Validate checks if the Status has all required fields populated.
// Returns an error if any required field is missing or invalid.
func (s *Status) Validate() error {
	if s.Source == "" {
		return fmt.Errorf("status source is required")
	}
	if s.Timestamp.IsZero() {
		return fmt.Errorf("status timestamp is required")
	}

	// Validate all events
	for i, event := range s.Events {
		if err := event.Validate(); err != nil {
			return fmt.Errorf("event[%d] validation failed: %w", i, err)
		}
	}

	// Validate all conditions
	for i, condition := range s.Conditions {
		if err := condition.Validate(); err != nil {
			return fmt.Errorf("condition[%d] validation failed: %w", i, err)
		}
	}

	return nil
}

// Validate checks if the Problem has all required fields populated.
// Returns an error if any required field is missing or invalid.
func (p *Problem) Validate() error {
	if p.Type == "" {
		return fmt.Errorf("problem type is required")
	}
	if p.Resource == "" {
		return fmt.Errorf("problem resource is required")
	}
	if p.Severity == "" {
		return fmt.Errorf("problem severity is required")
	}
	if p.Severity != ProblemInfo && p.Severity != ProblemWarning && p.Severity != ProblemCritical {
		return fmt.Errorf("invalid problem severity: %s", p.Severity)
	}
	if p.Message == "" {
		return fmt.Errorf("problem message is required")
	}
	if p.DetectedAt.IsZero() {
		return fmt.Errorf("problem detected time is required")
	}
	return nil
}

// Builder and helper methods

// AddEvent adds an event to the Status.
// Returns the Status pointer for method chaining.
func (s *Status) AddEvent(event Event) *Status {
	s.Events = append(s.Events, event)
	return s
}

// AddCondition adds a condition to the Status.
// Returns the Status pointer for method chaining.
func (s *Status) AddCondition(condition Condition) *Status {
	s.Conditions = append(s.Conditions, condition)
	return s
}

// ClearEvents removes all events from the Status.
// Returns the Status pointer for method chaining.
func (s *Status) ClearEvents() *Status {
	s.Events = []Event{}
	return s
}

// ClearConditions removes all conditions from the Status.
// Returns the Status pointer for method chaining.
func (s *Status) ClearConditions() *Status {
	s.Conditions = []Condition{}
	return s
}

// WithMetadata adds a metadata key-value pair to the Problem.
// Returns the Problem pointer for method chaining.
// If the Problem pointer is nil, this is a no-op and returns nil.
func (p *Problem) WithMetadata(key, value string) *Problem {
	if p == nil {
		return nil
	}
	if p.Metadata == nil {
		p.Metadata = make(map[string]string)
	}
	p.Metadata[key] = value
	return p
}

// GetMetadata retrieves a metadata value by key from the Problem.
// Returns the value and true if found, empty string and false otherwise.
// If the Problem pointer is nil, returns empty string and false.
func (p *Problem) GetMetadata(key string) (string, bool) {
	if p == nil || p.Metadata == nil {
		return "", false
	}
	val, ok := p.Metadata[key]
	return val, ok
}

// String formatting methods

// String returns a human-readable string representation of the Event.
func (e *Event) String() string {
	return fmt.Sprintf("[%s] %s at %s: %s",
		e.Severity, e.Reason, e.Timestamp.Format(time.RFC3339), e.Message)
}

// String returns a human-readable string representation of the Condition.
func (c *Condition) String() string {
	return fmt.Sprintf("%s=%s (since %s): %s",
		c.Type, c.Status, c.Transition.Format(time.RFC3339), c.Message)
}

// String returns a human-readable string representation of the Status.
func (s *Status) String() string {
	return fmt.Sprintf("Status from %s at %s: %d events, %d conditions",
		s.Source, s.Timestamp.Format(time.RFC3339), len(s.Events), len(s.Conditions))
}

// String returns a human-readable string representation of the Problem.
func (p *Problem) String() string {
	return fmt.Sprintf("[%s] %s on %s: %s (detected at %s)",
		p.Severity, p.Type, p.Resource, p.Message, p.DetectedAt.Format(time.RFC3339))
}

// LatencyMetrics contains network latency measurements for Prometheus export.
// Monitors should populate this in Status.Metadata["latency_metrics"].
type LatencyMetrics struct {
	// Gateway latency metrics
	Gateway *GatewayLatency `json:"gateway,omitempty"`

	// Peer latency metrics (CNI/cross-node connectivity)
	Peers []PeerLatency `json:"peers,omitempty"`

	// DNS latency metrics
	DNS []DNSLatency `json:"dns,omitempty"`

	// API server latency
	APIServer *APIServerLatency `json:"apiserver,omitempty"`
}

// GatewayLatency represents latency to the default gateway.
type GatewayLatency struct {
	GatewayIP    string  `json:"gateway_ip"`
	LatencyMs    float64 `json:"latency_ms"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	MaxLatencyMs float64 `json:"max_latency_ms"`
	Reachable    bool    `json:"reachable"`
	PingCount    int     `json:"ping_count"`
	SuccessCount int     `json:"success_count"`
}

// PeerLatency represents latency to a peer node.
type PeerLatency struct {
	PeerNode     string  `json:"peer_node"`
	PeerIP       string  `json:"peer_ip"`
	LatencyMs    float64 `json:"latency_ms"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	Reachable    bool    `json:"reachable"`
}

// DNSLatency represents DNS resolution latency.
type DNSLatency struct {
	DNSServer  string  `json:"dns_server"`
	Domain     string  `json:"domain"`
	RecordType string  `json:"record_type"`
	DomainType string  `json:"domain_type"` // "cluster", "external", "custom"
	LatencyMs  float64 `json:"latency_ms"`
	Success    bool    `json:"success"`
}

// APIServerLatency represents Kubernetes API server response latency.
type APIServerLatency struct {
	LatencyMs float64 `json:"latency_ms"`
	Reachable bool    `json:"reachable"`
}

// SetLatencyMetrics is a helper to set latency metrics in Status.Metadata.
func (s *Status) SetLatencyMetrics(metrics *LatencyMetrics) *Status {
	if s.Metadata == nil {
		s.Metadata = make(map[string]interface{})
	}
	s.Metadata["latency_metrics"] = metrics
	return s
}

// GetLatencyMetrics retrieves latency metrics from Status.Metadata.
// Returns nil if not set or if type assertion fails.
func (s *Status) GetLatencyMetrics() *LatencyMetrics {
	if s.Metadata == nil {
		return nil
	}
	if metrics, ok := s.Metadata["latency_metrics"].(*LatencyMetrics); ok {
		return metrics
	}
	return nil
}
