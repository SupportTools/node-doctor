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
				Severity:  EventWarning,
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
	problem := &Problem{
		Type:       "systemd-service-failed",
		Resource:   "kubelet.service",
		Severity:   ProblemCritical,
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

// TestEventSeverity verifies EventSeverity constants.
func TestEventSeverity(t *testing.T) {
	tests := []struct {
		name     string
		severity EventSeverity
		expected string
	}{
		{"Info severity", EventInfo, "Info"},
		{"Warning severity", EventWarning, "Warning"},
		{"Error severity", EventError, "Error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.severity) != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, string(tt.severity))
			}
		})
	}
}

// TestProblemSeverity verifies ProblemSeverity constants.
func TestProblemSeverity(t *testing.T) {
	tests := []struct {
		name     string
		severity ProblemSeverity
		expected string
	}{
		{"Info severity", ProblemInfo, "Info"},
		{"Warning severity", ProblemWarning, "Warning"},
		{"Critical severity", ProblemCritical, "Critical"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.severity) != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, string(tt.severity))
			}
		})
	}
}

// TestNewEvent verifies NewEvent constructor.
func TestNewEvent(t *testing.T) {
	event := NewEvent(EventWarning, "TestReason", "Test message")

	if event.Severity != EventWarning {
		t.Errorf("expected severity Warning, got %s", event.Severity)
	}
	if event.Reason != "TestReason" {
		t.Errorf("expected reason TestReason, got %s", event.Reason)
	}
	if event.Message != "Test message" {
		t.Errorf("expected message 'Test message', got %s", event.Message)
	}
	if event.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

// TestNewCondition verifies NewCondition constructor.
func TestNewCondition(t *testing.T) {
	condition := NewCondition("TestType", ConditionTrue, "TestReason", "Test message")

	if condition.Type != "TestType" {
		t.Errorf("expected type TestType, got %s", condition.Type)
	}
	if condition.Status != ConditionTrue {
		t.Errorf("expected status True, got %s", condition.Status)
	}
	if condition.Reason != "TestReason" {
		t.Errorf("expected reason TestReason, got %s", condition.Reason)
	}
	if condition.Message != "Test message" {
		t.Errorf("expected message 'Test message', got %s", condition.Message)
	}
	if condition.Transition.IsZero() {
		t.Error("expected non-zero transition time")
	}
}

// TestNewStatus verifies NewStatus constructor.
func TestNewStatus(t *testing.T) {
	status := NewStatus("test-monitor")

	if status.Source != "test-monitor" {
		t.Errorf("expected source test-monitor, got %s", status.Source)
	}
	if status.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
	if status.Events == nil {
		t.Error("expected initialized Events slice")
	}
	if status.Conditions == nil {
		t.Error("expected initialized Conditions slice")
	}
	if len(status.Events) != 0 {
		t.Errorf("expected 0 events, got %d", len(status.Events))
	}
	if len(status.Conditions) != 0 {
		t.Errorf("expected 0 conditions, got %d", len(status.Conditions))
	}
}

// TestNewProblem verifies NewProblem constructor.
func TestNewProblem(t *testing.T) {
	problem := NewProblem("test-type", "test-resource", ProblemCritical, "Test message")

	if problem.Type != "test-type" {
		t.Errorf("expected type test-type, got %s", problem.Type)
	}
	if problem.Resource != "test-resource" {
		t.Errorf("expected resource test-resource, got %s", problem.Resource)
	}
	if problem.Severity != ProblemCritical {
		t.Errorf("expected severity Critical, got %s", problem.Severity)
	}
	if problem.Message != "Test message" {
		t.Errorf("expected message 'Test message', got %s", problem.Message)
	}
	if problem.DetectedAt.IsZero() {
		t.Error("expected non-zero detected time")
	}
	if problem.Metadata == nil {
		t.Error("expected initialized Metadata map")
	}
}

// TestEventValidation verifies Event validation.
func TestEventValidation(t *testing.T) {
	tests := []struct {
		name    string
		event   Event
		wantErr bool
	}{
		{
			name:    "valid event",
			event:   NewEvent(EventInfo, "TestReason", "Test message"),
			wantErr: false,
		},
		{
			name: "missing severity",
			event: Event{
				Timestamp: time.Now(),
				Reason:    "TestReason",
				Message:   "Test message",
			},
			wantErr: true,
		},
		{
			name: "invalid severity",
			event: Event{
				Severity:  "InvalidSeverity",
				Timestamp: time.Now(),
				Reason:    "TestReason",
				Message:   "Test message",
			},
			wantErr: true,
		},
		{
			name: "missing reason",
			event: Event{
				Severity:  EventInfo,
				Timestamp: time.Now(),
				Message:   "Test message",
			},
			wantErr: true,
		},
		{
			name: "missing message",
			event: Event{
				Severity:  EventInfo,
				Timestamp: time.Now(),
				Reason:    "TestReason",
			},
			wantErr: true,
		},
		{
			name: "missing timestamp",
			event: Event{
				Severity: EventInfo,
				Reason:   "TestReason",
				Message:  "Test message",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.event.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestConditionValidation verifies Condition validation.
func TestConditionValidation(t *testing.T) {
	tests := []struct {
		name      string
		condition Condition
		wantErr   bool
	}{
		{
			name:      "valid condition",
			condition: NewCondition("TestType", ConditionTrue, "TestReason", "Test message"),
			wantErr:   false,
		},
		{
			name: "missing type",
			condition: Condition{
				Status:     ConditionTrue,
				Transition: time.Now(),
				Reason:     "TestReason",
				Message:    "Test message",
			},
			wantErr: true,
		},
		{
			name: "missing status",
			condition: Condition{
				Type:       "TestType",
				Transition: time.Now(),
				Reason:     "TestReason",
				Message:    "Test message",
			},
			wantErr: true,
		},
		{
			name: "invalid status",
			condition: Condition{
				Type:       "TestType",
				Status:     "InvalidStatus",
				Transition: time.Now(),
				Reason:     "TestReason",
				Message:    "Test message",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.condition.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestStatusValidation verifies Status validation.
func TestStatusValidation(t *testing.T) {
	tests := []struct {
		name    string
		status  *Status
		wantErr bool
	}{
		{
			name:    "valid empty status",
			status:  NewStatus("test-monitor"),
			wantErr: false,
		},
		{
			name: "valid status with events and conditions",
			status: NewStatus("test-monitor").
				AddEvent(NewEvent(EventInfo, "TestReason", "Test message")).
				AddCondition(NewCondition("TestType", ConditionTrue, "TestReason", "Test message")),
			wantErr: false,
		},
		{
			name: "missing source",
			status: &Status{
				Timestamp:  time.Now(),
				Events:     []Event{},
				Conditions: []Condition{},
			},
			wantErr: true,
		},
		{
			name: "invalid event",
			status: &Status{
				Source:    "test-monitor",
				Timestamp: time.Now(),
				Events: []Event{
					{Severity: EventInfo}, // missing required fields
				},
				Conditions: []Condition{},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.status.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestProblemValidation verifies Problem validation.
func TestProblemValidation(t *testing.T) {
	tests := []struct {
		name    string
		problem *Problem
		wantErr bool
	}{
		{
			name:    "valid problem",
			problem: NewProblem("test-type", "test-resource", ProblemWarning, "Test message"),
			wantErr: false,
		},
		{
			name: "missing type",
			problem: &Problem{
				Resource:   "test-resource",
				Severity:   ProblemWarning,
				Message:    "Test message",
				DetectedAt: time.Now(),
			},
			wantErr: true,
		},
		{
			name: "missing resource",
			problem: &Problem{
				Type:       "test-type",
				Severity:   ProblemWarning,
				Message:    "Test message",
				DetectedAt: time.Now(),
			},
			wantErr: true,
		},
		{
			name: "invalid severity",
			problem: &Problem{
				Type:       "test-type",
				Resource:   "test-resource",
				Severity:   "InvalidSeverity",
				Message:    "Test message",
				DetectedAt: time.Now(),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.problem.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestStatusBuilders verifies Status builder methods.
func TestStatusBuilders(t *testing.T) {
	status := NewStatus("test-monitor")

	// Test AddEvent
	event := NewEvent(EventInfo, "TestReason", "Test message")
	status.AddEvent(event)
	if len(status.Events) != 1 {
		t.Errorf("expected 1 event, got %d", len(status.Events))
	}

	// Test AddCondition
	condition := NewCondition("TestType", ConditionTrue, "TestReason", "Test message")
	status.AddCondition(condition)
	if len(status.Conditions) != 1 {
		t.Errorf("expected 1 condition, got %d", len(status.Conditions))
	}

	// Test method chaining
	status.AddEvent(event).AddCondition(condition)
	if len(status.Events) != 2 {
		t.Errorf("expected 2 events after chaining, got %d", len(status.Events))
	}
	if len(status.Conditions) != 2 {
		t.Errorf("expected 2 conditions after chaining, got %d", len(status.Conditions))
	}

	// Test ClearEvents
	status.ClearEvents()
	if len(status.Events) != 0 {
		t.Errorf("expected 0 events after clear, got %d", len(status.Events))
	}

	// Test ClearConditions
	status.ClearConditions()
	if len(status.Conditions) != 0 {
		t.Errorf("expected 0 conditions after clear, got %d", len(status.Conditions))
	}
}

// TestProblemMetadata verifies Problem metadata methods.
func TestProblemMetadata(t *testing.T) {
	problem := NewProblem("test-type", "test-resource", ProblemWarning, "Test message")

	// Test WithMetadata
	problem.WithMetadata("key1", "value1")
	if val, ok := problem.GetMetadata("key1"); !ok || val != "value1" {
		t.Errorf("expected metadata key1=value1, got %s (found: %v)", val, ok)
	}

	// Test method chaining
	problem.WithMetadata("key2", "value2").WithMetadata("key3", "value3")
	if len(problem.Metadata) != 3 {
		t.Errorf("expected 3 metadata entries, got %d", len(problem.Metadata))
	}

	// Test GetMetadata for non-existent key
	if _, ok := problem.GetMetadata("nonexistent"); ok {
		t.Error("expected GetMetadata to return false for non-existent key")
	}
}

// TestEventString verifies Event String formatting.
func TestEventString(t *testing.T) {
	event := NewEvent(EventWarning, "TestReason", "Test message")
	str := event.String()

	if str == "" {
		t.Error("expected non-empty string")
	}
	// Verify key components are present using simple string search
	if !stringContains(str, "Warning") {
		t.Error("expected severity in string")
	}
	if !stringContains(str, "TestReason") {
		t.Error("expected reason in string")
	}
	if !stringContains(str, "Test message") {
		t.Error("expected message in string")
	}
}

// TestConditionString verifies Condition String formatting.
func TestConditionString(t *testing.T) {
	condition := NewCondition("TestType", ConditionTrue, "TestReason", "Test message")
	str := condition.String()

	if str == "" {
		t.Error("expected non-empty string")
	}
	// Verify key components are present
	if !stringContains(str, "TestType") {
		t.Error("expected type in string")
	}
	if !stringContains(str, "True") {
		t.Error("expected status in string")
	}
}

// TestStatusString verifies Status String formatting.
func TestStatusString(t *testing.T) {
	status := NewStatus("test-monitor")
	str := status.String()

	if str == "" {
		t.Error("expected non-empty string")
	}
	// Verify key components are present
	if !stringContains(str, "test-monitor") {
		t.Error("expected source in string")
	}
}

// TestProblemString verifies Problem String formatting.
func TestProblemString(t *testing.T) {
	problem := NewProblem("test-type", "test-resource", ProblemCritical, "Test message")
	str := problem.String()

	if str == "" {
		t.Error("expected non-empty string")
	}
	// Verify key components are present
	if !stringContains(str, "Critical") {
		t.Error("expected severity in string")
	}
	if !stringContains(str, "test-type") {
		t.Error("expected type in string")
	}
	if !stringContains(str, "test-resource") {
		t.Error("expected resource in string")
	}
}

// Helper function for string contains checks
func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestNilPointerHandling verifies nil pointer safety.
func TestNilPointerHandling(t *testing.T) {
	var problem *Problem
	
	// WithMetadata should handle nil gracefully
	result := problem.WithMetadata("key", "value")
	if result != nil {
		t.Error("expected nil result from WithMetadata on nil pointer")
	}
	
	// GetMetadata should handle nil gracefully
	val, ok := problem.GetMetadata("key")
	if ok || val != "" {
		t.Error("expected GetMetadata on nil pointer to return empty string and false")
	}
}
