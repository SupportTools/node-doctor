// Package kubernetes provides Kubernetes-native exporting capabilities for Node Doctor.
// It exports status updates as Kubernetes events and node conditions, and problems as node conditions.
package kubernetes

import (
	"crypto/sha256"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/supporttools/node-doctor/pkg/types"
)

// Internal type definitions for the Kubernetes exporter

// EventSignature represents a unique signature for event deduplication
type EventSignature struct {
	Reason    string
	Message   string
	Type      string
	Source    string
	Timestamp time.Time
}

// String returns a string representation of the event signature for hashing
func (s EventSignature) String() string {
	return fmt.Sprintf("%s:%s:%s:%s:%d", s.Reason, s.Message, s.Type, s.Source, s.Timestamp.Unix())
}

// Hash returns a SHA256 hash of the event signature for deduplication
func (s EventSignature) Hash() string {
	h := sha256.Sum256([]byte(s.String()))
	return fmt.Sprintf("%x", h)[:16] // Use first 16 chars for brevity
}

// ConditionUpdate represents a pending condition update
type ConditionUpdate struct {
	Type               string
	Status             corev1.ConditionStatus
	Reason             string
	Message            string
	LastTransitionTime metav1.Time
}

// String returns a string representation of the condition update
func (c ConditionUpdate) String() string {
	return fmt.Sprintf("%s=%s: %s", c.Type, c.Status, c.Message)
}

// Mapping functions for converting Node Doctor types to Kubernetes types

// ConvertStatusToEvents converts a Node Doctor Status to Kubernetes Events
func ConvertStatusToEvents(status *types.Status, nodeName string) []corev1.Event {
	var events []corev1.Event

	for _, event := range status.Events {
		k8sEvent := convertEventToK8sEvent(status, event, nodeName)
		events = append(events, k8sEvent)
	}

	return events
}

// ConvertConditionsToNodeConditions converts Node Doctor Conditions to Kubernetes NodeConditions
func ConvertConditionsToNodeConditions(conditions []types.Condition) []corev1.NodeCondition {
	var nodeConditions []corev1.NodeCondition

	for _, condition := range conditions {
		nodeCondition := convertConditionToNodeCondition(condition)
		nodeConditions = append(nodeConditions, nodeCondition)
	}

	return nodeConditions
}

// ConvertProblemToCondition converts a Node Doctor Problem to a Kubernetes NodeCondition
func ConvertProblemToCondition(problem *types.Problem) corev1.NodeCondition {
	conditionType := fmt.Sprintf("NodeDoctor%s", sanitizeConditionType(problem.Type))
	status := corev1.ConditionFalse // Problems indicate unhealthy state

	// Map severity to condition reason
	reason := "ProblemDetected"
	switch problem.Severity {
	case types.ProblemCritical:
		reason = "CriticalProblem"
	case types.ProblemWarning:
		reason = "WarningProblem"
	case types.ProblemInfo:
		reason = "InfoProblem"
	}

	message := fmt.Sprintf("%s on %s: %s", problem.Type, problem.Resource, problem.Message)

	return corev1.NodeCondition{
		Type:               corev1.NodeConditionType(conditionType),
		Status:             status,
		LastTransitionTime: metav1.NewTime(problem.DetectedAt),
		Reason:             reason,
		Message:            message,
	}
}

// ConvertProblemToEvent converts a Node Doctor Problem to a Kubernetes Event
func ConvertProblemToEvent(problem *types.Problem, nodeName string) corev1.Event {
	eventType := corev1.EventTypeNormal
	if problem.Severity == types.ProblemCritical {
		eventType = corev1.EventTypeWarning
	}

	reason := fmt.Sprintf("Problem%s", sanitizeEventReason(string(problem.Severity)))
	message := fmt.Sprintf("%s detected on %s: %s", problem.Type, problem.Resource, problem.Message)

	now := metav1.NewTime(time.Now())

	return corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generateEventName(problem, nodeName),
			Namespace: "default", // Will be overridden by exporter configuration
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:      "Node",
			Name:      nodeName,
			UID:       "", // Will be filled by the client
			Namespace: "",
		},
		Reason:  reason,
		Message: message,
		Source: corev1.EventSource{
			Component: "node-doctor",
			Host:      nodeName,
		},
		FirstTimestamp: now,
		LastTimestamp:  now,
		Count:          1,
		Type:           eventType,
	}
}

// Helper functions

// convertEventToK8sEvent converts a single Node Doctor Event to a Kubernetes Event
func convertEventToK8sEvent(status *types.Status, event types.Event, nodeName string) corev1.Event {
	eventType := corev1.EventTypeNormal
	if event.Severity == types.EventError {
		eventType = corev1.EventTypeWarning
	}

	reason := sanitizeEventReason(event.Reason)
	k8sTimestamp := metav1.NewTime(event.Timestamp)

	return corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generateEventNameFromEvent(event, status.Source, nodeName),
			Namespace: "default", // Will be overridden by exporter configuration
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:      "Node",
			Name:      nodeName,
			UID:       "", // Will be filled by the client
			Namespace: "",
		},
		Reason:  reason,
		Message: event.Message,
		Source: corev1.EventSource{
			Component: "node-doctor",
			Host:      nodeName,
		},
		FirstTimestamp: k8sTimestamp,
		LastTimestamp:  k8sTimestamp,
		Count:          1,
		Type:           eventType,
	}
}

// convertConditionToNodeCondition converts a Node Doctor Condition to a Kubernetes NodeCondition
func convertConditionToNodeCondition(condition types.Condition) corev1.NodeCondition {
	// Map Node Doctor condition status to Kubernetes condition status
	var status corev1.ConditionStatus
	switch condition.Status {
	case types.ConditionTrue:
		status = corev1.ConditionTrue
	case types.ConditionFalse:
		status = corev1.ConditionFalse
	case types.ConditionUnknown:
		status = corev1.ConditionUnknown
	default:
		status = corev1.ConditionUnknown
	}

	// Ensure condition type is prefixed with NodeDoctor
	conditionType := condition.Type
	if !isStandardNodeCondition(conditionType) {
		conditionType = fmt.Sprintf("NodeDoctor%s", sanitizeConditionType(conditionType))
	}

	return corev1.NodeCondition{
		Type:               corev1.NodeConditionType(conditionType),
		Status:             status,
		LastTransitionTime: metav1.NewTime(condition.Transition),
		Reason:             sanitizeConditionReason(condition.Reason),
		Message:            condition.Message,
	}
}

// sanitizeConditionType ensures the condition type is valid for Kubernetes
func sanitizeConditionType(conditionType string) string {
	// Remove special characters and ensure it starts with uppercase
	sanitized := ""
	makeUpper := true
	for _, r := range conditionType {
		if r >= 'a' && r <= 'z' {
			if makeUpper {
				sanitized += string(r - 32) // Convert to uppercase
				makeUpper = false
			} else {
				sanitized += string(r)
			}
		} else if r >= 'A' && r <= 'Z' {
			sanitized += string(r)
			makeUpper = false
		} else if r >= '0' && r <= '9' {
			sanitized += string(r)
			makeUpper = false
		} else {
			makeUpper = true // Next letter should be uppercase
		}
	}
	return sanitized
}

// sanitizeEventReason ensures the event reason is valid for Kubernetes
func sanitizeEventReason(reason string) string {
	// Similar to sanitizeConditionType but preserve the original case better
	sanitized := ""
	for _, r := range reason {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			sanitized += string(r)
		}
	}
	if sanitized == "" {
		return "Unknown"
	}
	return sanitized
}

// sanitizeConditionReason ensures the condition reason is valid for Kubernetes
func sanitizeConditionReason(reason string) string {
	return sanitizeEventReason(reason)
}

// isStandardNodeCondition checks if the condition type is a standard Kubernetes node condition
func isStandardNodeCondition(conditionType string) bool {
	standardConditions := map[string]bool{
		"Ready":              true,
		"MemoryPressure":     true,
		"DiskPressure":       true,
		"PIDPressure":        true,
		"NetworkUnavailable": true,
	}
	return standardConditions[conditionType]
}

// generateEventName generates a unique event name for a problem
func generateEventName(problem *types.Problem, nodeName string) string {
	timestamp := problem.DetectedAt.Unix()
	return fmt.Sprintf("node-doctor-%s-%s-%d", nodeName, sanitizeEventReason(problem.Type), timestamp)
}

// generateEventNameFromEvent generates a unique event name from a Node Doctor event
func generateEventNameFromEvent(event types.Event, source, nodeName string) string {
	timestamp := event.Timestamp.Unix()
	return fmt.Sprintf("node-doctor-%s-%s-%s-%d", nodeName, source, sanitizeEventReason(event.Reason), timestamp)
}

// CreateEventSignature creates an event signature for deduplication
func CreateEventSignature(event corev1.Event) EventSignature {
	return EventSignature{
		Reason:    event.Reason,
		Message:   event.Message,
		Type:      event.Type,
		Source:    event.Source.Component,
		Timestamp: event.FirstTimestamp.Time.Truncate(time.Minute), // Truncate to minute for deduplication
	}
}
