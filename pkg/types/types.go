// Package types defines the core interfaces and types for Node Doctor.
// Based on architecture.md specification.
package types

import (
	"context"
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
}

// Event represents a discrete occurrence detected by a monitor.
type Event struct {
	// Severity indicates the importance of the event (Info, Warning, Error).
	Severity string

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

// Problem represents an issue detected that may require remediation.
type Problem struct {
	// Type categorizes the problem (e.g., "systemd-service-failed").
	Type string

	// Resource identifies the affected resource (e.g., "kubelet.service").
	Resource string

	// Severity indicates how critical the problem is.
	Severity string

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
