package test

import (
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// Common test fixtures for creating Problems, Status objects, and other test data.

// ProblemFixture provides a fluent builder for creating test Problem instances.
type ProblemFixture struct {
	problem types.Problem
}

// NewProblemFixture creates a new Problem fixture with sensible defaults.
func NewProblemFixture() *ProblemFixture {
	return &ProblemFixture{
		problem: types.Problem{
			Type:       "test-problem",
			Resource:   "test-resource",
			Severity:   types.ProblemWarning,
			Message:    "Test problem message",
			DetectedAt: time.Now(),
			Metadata:   make(map[string]string),
		},
	}
}

// WithType sets the problem type.
func (f *ProblemFixture) WithType(problemType string) *ProblemFixture {
	f.problem.Type = problemType
	return f
}

// WithResource sets the resource identifier.
func (f *ProblemFixture) WithResource(resource string) *ProblemFixture {
	f.problem.Resource = resource
	return f
}

// WithSeverity sets the problem severity.
func (f *ProblemFixture) WithSeverity(severity types.ProblemSeverity) *ProblemFixture {
	f.problem.Severity = severity
	return f
}

// WithMessage sets the problem message.
func (f *ProblemFixture) WithMessage(message string) *ProblemFixture {
	f.problem.Message = message
	return f
}

// WithDetectedAt sets the detection timestamp.
func (f *ProblemFixture) WithDetectedAt(t time.Time) *ProblemFixture {
	f.problem.DetectedAt = t
	return f
}

// WithMetadata sets a metadata key-value pair.
func (f *ProblemFixture) WithMetadata(key, value string) *ProblemFixture {
	if f.problem.Metadata == nil {
		f.problem.Metadata = make(map[string]string)
	}
	f.problem.Metadata[key] = value
	return f
}

// Build returns the constructed Problem.
func (f *ProblemFixture) Build() types.Problem {
	return f.problem
}

// StatusFixture provides a fluent builder for creating test Status instances.
type StatusFixture struct {
	status types.Status
}

// NewStatusFixture creates a new Status fixture with sensible defaults.
func NewStatusFixture() *StatusFixture {
	return &StatusFixture{
		status: types.Status{
			Source:     "test-monitor",
			Events:     make([]types.Event, 0),
			Conditions: make([]types.Condition, 0),
			Timestamp:  time.Now(),
		},
	}
}

// WithSource sets the status source (monitor name).
func (f *StatusFixture) WithSource(source string) *StatusFixture {
	f.status.Source = source
	return f
}

// WithTimestamp sets the status timestamp.
func (f *StatusFixture) WithTimestamp(t time.Time) *StatusFixture {
	f.status.Timestamp = t
	return f
}

// WithEvent adds an event to the status.
func (f *StatusFixture) WithEvent(event types.Event) *StatusFixture {
	if f.status.Events == nil {
		f.status.Events = make([]types.Event, 0)
	}
	f.status.Events = append(f.status.Events, event)
	return f
}

// WithEvents sets multiple events.
func (f *StatusFixture) WithEvents(events []types.Event) *StatusFixture {
	f.status.Events = events
	return f
}

// WithCondition adds a condition to the status.
func (f *StatusFixture) WithCondition(condition types.Condition) *StatusFixture {
	if f.status.Conditions == nil {
		f.status.Conditions = make([]types.Condition, 0)
	}
	f.status.Conditions = append(f.status.Conditions, condition)
	return f
}

// WithConditions sets multiple conditions.
func (f *StatusFixture) WithConditions(conditions []types.Condition) *StatusFixture {
	f.status.Conditions = conditions
	return f
}

// Build returns the constructed Status.
func (f *StatusFixture) Build() types.Status {
	return f.status
}

// Common problem fixtures for different scenarios.

// KubeletFailedProblem returns a problem representing a failed kubelet service.
func KubeletFailedProblem() types.Problem {
	return NewProblemFixture().
		WithType("systemd-service-failed").
		WithResource("kubelet.service").
		WithSeverity(types.ProblemCritical).
		WithMessage("kubelet service has failed").
		WithMetadata("service", "kubelet").
		WithMetadata("state", "failed").
		Build()
}

// MemoryPressureProblem returns a problem representing memory pressure.
func MemoryPressureProblem() types.Problem {
	return NewProblemFixture().
		WithType("node-memory-pressure").
		WithResource("node-memory").
		WithSeverity(types.ProblemWarning).
		WithMessage("Node is experiencing memory pressure").
		WithMetadata("usage_percent", "92.5").
		WithMetadata("threshold", "90").
		Build()
}

// DiskPressureProblem returns a problem representing disk pressure.
func DiskPressureProblem() types.Problem {
	return NewProblemFixture().
		WithType("node-disk-pressure").
		WithResource("/var").
		WithSeverity(types.ProblemWarning).
		WithMessage("Disk usage is high on /var").
		WithMetadata("usage_percent", "87").
		WithMetadata("threshold", "85").
		WithMetadata("path", "/var").
		Build()
}

// ContainerRuntimeFailedProblem returns a problem representing a failed container runtime.
func ContainerRuntimeFailedProblem() types.Problem {
	return NewProblemFixture().
		WithType("container-runtime-failed").
		WithResource("docker.service").
		WithSeverity(types.ProblemCritical).
		WithMessage("Container runtime is not responding").
		WithMetadata("runtime", "docker").
		WithMetadata("error", "connection refused").
		Build()
}

// NetworkPluginNotReadyProblem returns a problem representing network plugin not ready.
func NetworkPluginNotReadyProblem() types.Problem {
	return NewProblemFixture().
		WithType("network-plugin-not-ready").
		WithResource("cni").
		WithSeverity(types.ProblemCritical).
		WithMessage("Network plugin returns error: cni plugin not initialized").
		WithMetadata("plugin", "cni").
		WithMetadata("error", "not initialized").
		Build()
}

// NodeNotReadyProblem returns a problem representing a node not ready condition.
func NodeNotReadyProblem() types.Problem {
	return NewProblemFixture().
		WithType("node-not-ready").
		WithResource("test-node").
		WithSeverity(types.ProblemCritical).
		WithMessage("Node is not ready").
		WithMetadata("condition", "Ready=False").
		WithMetadata("reason", "KubeletNotReady").
		Build()
}

// Common status fixtures for different scenarios.

// HealthyKubeletStatus returns a healthy status from kubelet monitor.
func HealthyKubeletStatus() types.Status {
	return NewStatusFixture().
		WithSource("kubelet-monitor").
		WithCondition(types.NewCondition("KubeletReady", types.ConditionTrue, "KubeletHealthy", "Kubelet is healthy and running")).
		WithEvent(types.NewEvent(types.EventInfo, "ServiceActive", "Kubelet service is active")).
		Build()
}

// UnhealthyKubeletStatus returns an unhealthy status from kubelet monitor.
func UnhealthyKubeletStatus() types.Status {
	return NewStatusFixture().
		WithSource("kubelet-monitor").
		WithCondition(types.NewCondition("KubeletReady", types.ConditionFalse, "ServiceFailed", "Kubelet service has failed")).
		WithEvent(types.NewEvent(types.EventError, "ServiceFailed", "Kubelet service has failed")).
		Build()
}

// HealthyMemoryStatus returns a healthy status from memory monitor.
func HealthyMemoryStatus() types.Status {
	return NewStatusFixture().
		WithSource("memory-monitor").
		WithCondition(types.NewCondition("MemoryPressure", types.ConditionFalse, "SufficientMemory", "Memory usage is normal (65.3%)")).
		WithEvent(types.NewEvent(types.EventInfo, "MemoryNormal", "Memory usage is normal")).
		Build()
}

// UnhealthyMemoryStatus returns an unhealthy status from memory monitor.
func UnhealthyMemoryStatus() types.Status {
	return NewStatusFixture().
		WithSource("memory-monitor").
		WithCondition(types.NewCondition("MemoryPressure", types.ConditionTrue, "HighMemoryUsage", "Node is experiencing memory pressure (92.5%)")).
		WithEvent(types.NewEvent(types.EventWarning, "MemoryPressure", "Node is experiencing memory pressure")).
		Build()
}

// HealthyDiskStatus returns a healthy status from disk monitor.
func HealthyDiskStatus() types.Status {
	return NewStatusFixture().
		WithSource("disk-monitor").
		WithCondition(types.NewCondition("DiskPressure", types.ConditionFalse, "SufficientDisk", "Disk usage is normal on /var (45%)")).
		WithEvent(types.NewEvent(types.EventInfo, "DiskNormal", "Disk usage is normal")).
		Build()
}

// UnhealthyDiskStatus returns an unhealthy status from disk monitor.
func UnhealthyDiskStatus() types.Status {
	return NewStatusFixture().
		WithSource("disk-monitor").
		WithCondition(types.NewCondition("DiskPressure", types.ConditionTrue, "HighDiskUsage", "Disk usage is high on /var (87%)")).
		WithEvent(types.NewEvent(types.EventWarning, "DiskPressure", "Disk usage is high on /var")).
		Build()
}

// MultiProblemStatus returns a status with multiple problems.
func MultiProblemStatus() types.Status {
	return NewStatusFixture().
		WithSource("node-monitor").
		WithCondition(types.NewCondition("KubeletReady", types.ConditionFalse, "ServiceFailed", "Kubelet service has failed")).
		WithCondition(types.NewCondition("MemoryPressure", types.ConditionTrue, "HighMemoryUsage", "Memory pressure detected")).
		WithCondition(types.NewCondition("DiskPressure", types.ConditionTrue, "HighDiskUsage", "Disk pressure detected")).
		WithEvent(types.NewEvent(types.EventError, "KubeletFailed", "Kubelet service has failed")).
		WithEvent(types.NewEvent(types.EventWarning, "MemoryPressure", "Memory pressure detected")).
		WithEvent(types.NewEvent(types.EventWarning, "DiskPressure", "Disk pressure detected")).
		Build()
}
