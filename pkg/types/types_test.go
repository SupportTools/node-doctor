package types

import (
	"testing"
	"time"
)

// TestConditionStatus verifies the condition status constants are defined.
func TestConditionStatus(t *testing.T) {
	tests := []struct {
		name     string
		status   ConditionStatus
		expected string
	}{
		{"True status", ConditionTrue, "True"},
		{"False status", ConditionFalse, "False"},
		{"Unknown status", ConditionUnknown, "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.status) != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, string(tt.status))
			}
		})
	}
}

// TestStatusCreation verifies Status struct can be created.
func TestStatusCreation(t *testing.T) {
	now := time.Now()
	status := &Status{
		Source:    "test-monitor",
		Timestamp: now,
		Events: []Event{
			{
				Severity:  "Warning",
				Timestamp: now,
				Reason:    "TestReason",
				Message:   "Test message",
			},
		},
		Conditions: []Condition{
			{
				Type:       "TestCondition",
				Status:     ConditionTrue,
				Transition: now,
				Reason:     "TestTransition",
				Message:    "Test condition message",
			},
		},
	}

	if status.Source != "test-monitor" {
		t.Errorf("expected source 'test-monitor', got '%s'", status.Source)
	}
	if len(status.Events) != 1 {
		t.Errorf("expected 1 event, got %d", len(status.Events))
	}
	if len(status.Conditions) != 1 {
		t.Errorf("expected 1 condition, got %d", len(status.Conditions))
	}
}

// TestProblemCreation verifies Problem struct can be created.
func TestProblemCreation(t *testing.T) {
	now := time.Now()
	problem := Problem{
		Type:       "systemd-service-failed",
		Resource:   "kubelet.service",
		Severity:   "Critical",
		Message:    "Kubelet service is not running",
		DetectedAt: now,
		Metadata: map[string]string{
			"service": "kubelet",
			"status":  "inactive",
		},
	}

	if problem.Type != "systemd-service-failed" {
		t.Errorf("expected type 'systemd-service-failed', got '%s'", problem.Type)
	}
	if problem.Resource != "kubelet.service" {
		t.Errorf("expected resource 'kubelet.service', got '%s'", problem.Resource)
	}
	if len(problem.Metadata) != 2 {
		t.Errorf("expected 2 metadata entries, got %d", len(problem.Metadata))
	}
}

// TestInterfacesExist verifies that all core interfaces are defined.
// This is a compile-time test - if interfaces are missing, this won't compile.
func TestInterfacesExist(t *testing.T) {
	var _ Monitor
	var _ Remediator
	var _ Exporter
	// If we get here, all interfaces are defined correctly
	t.Log("All core interfaces are defined")
}
