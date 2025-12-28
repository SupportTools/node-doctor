package http

import (
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/supporttools/node-doctor/pkg/controller"
	"github.com/supporttools/node-doctor/pkg/types"
)

// ReportBuilder aggregates monitor statuses and problems into a NodeReport
// suitable for sending to the node-doctor controller.
type ReportBuilder struct {
	nodeName  string
	nodeUID   string
	version   string
	startTime time.Time

	mu              sync.RWMutex
	monitorStatuses map[string]*controller.MonitorStatus
	activeProblems  map[string]*controller.ProblemSummary
	conditions      map[string]*controller.NodeCondition

	// Statistics
	statusesProcessed int64
	problemsDetected  int64
	remediationsRun   int64
}

// NewReportBuilder creates a new ReportBuilder for the given node.
func NewReportBuilder(nodeName, nodeUID, version string) *ReportBuilder {
	return &ReportBuilder{
		nodeName:        nodeName,
		nodeUID:         nodeUID,
		version:         version,
		startTime:       time.Now(),
		monitorStatuses: make(map[string]*controller.MonitorStatus),
		activeProblems:  make(map[string]*controller.ProblemSummary),
		conditions:      make(map[string]*controller.NodeCondition),
	}
}

// UpdateFromStatus updates the report builder with a new status from a monitor.
func (rb *ReportBuilder) UpdateFromStatus(status *types.Status) {
	if status == nil {
		return
	}

	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.statusesProcessed++

	// Convert and store monitor status
	monitorStatus := rb.convertMonitorStatus(status)
	rb.monitorStatuses[status.Source] = monitorStatus

	// Update conditions from status
	for _, cond := range status.Conditions {
		nodeCondition := rb.convertCondition(cond)
		rb.conditions[cond.Type] = nodeCondition
	}
}

// AddProblem adds or updates an active problem.
func (rb *ReportBuilder) AddProblem(problem *types.Problem) {
	if problem == nil {
		return
	}

	rb.mu.Lock()
	defer rb.mu.Unlock()

	key := fmt.Sprintf("%s:%s", problem.Type, problem.Resource)

	existing, exists := rb.activeProblems[key]
	if exists {
		// Update existing problem
		existing.LastSeenAt = time.Now()
		existing.Occurrences++
		existing.Message = problem.Message
	} else {
		// Add new problem
		rb.problemsDetected++
		rb.activeProblems[key] = &controller.ProblemSummary{
			Type:        problem.Type,
			Severity:    string(problem.Severity),
			Message:     problem.Message,
			Source:      problem.Resource,
			DetectedAt:  problem.DetectedAt,
			LastSeenAt:  time.Now(),
			Occurrences: 1,
		}
	}
}

// RemoveProblem removes a resolved problem.
func (rb *ReportBuilder) RemoveProblem(problemType, resource string) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	key := fmt.Sprintf("%s:%s", problemType, resource)
	delete(rb.activeProblems, key)
}

// ClearProblemsForSource removes all problems from a specific source.
func (rb *ReportBuilder) ClearProblemsForSource(source string) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	for key, problem := range rb.activeProblems {
		if problem.Source == source {
			delete(rb.activeProblems, key)
		}
	}
}

// IncrementRemediations increments the remediation counter.
func (rb *ReportBuilder) IncrementRemediations() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.remediationsRun++
}

// BuildReport constructs a NodeReport from the current state.
func (rb *ReportBuilder) BuildReport(reportType string) *controller.NodeReport {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	// Collect monitor statuses
	monitorStatuses := make([]controller.MonitorStatus, 0, len(rb.monitorStatuses))
	for _, ms := range rb.monitorStatuses {
		monitorStatuses = append(monitorStatuses, *ms)
	}

	// Collect active problems
	activeProblems := make([]controller.ProblemSummary, 0, len(rb.activeProblems))
	for _, p := range rb.activeProblems {
		activeProblems = append(activeProblems, *p)
	}

	// Collect conditions
	conditions := make([]controller.NodeCondition, 0, len(rb.conditions))
	for _, c := range rb.conditions {
		conditions = append(conditions, *c)
	}

	// Calculate overall health
	overallHealth := rb.calculateOverallHealth(monitorStatuses, activeProblems)

	// Build uptime string
	uptime := time.Since(rb.startTime).Round(time.Second).String()

	// Generate report ID
	reportID := fmt.Sprintf("%s-%d", rb.nodeName, time.Now().UnixNano())

	// Gather stats
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	report := &controller.NodeReport{
		NodeName:        rb.nodeName,
		NodeUID:         rb.nodeUID,
		Timestamp:       time.Now(),
		ReportID:        reportID,
		Version:         rb.version,
		Uptime:          uptime,
		ReportType:      reportType,
		OverallHealth:   overallHealth,
		MonitorStatuses: monitorStatuses,
		ActiveProblems:  activeProblems,
		Conditions:      conditions,
		Stats: &controller.NodeStats{
			StatusesProcessed: rb.statusesProcessed,
			ProblemsDetected:  rb.problemsDetected,
			RemediationsRun:   rb.remediationsRun,
			MemoryUsageBytes:  int64(memStats.Alloc),
			GoroutineCount:    runtime.NumGoroutine(),
		},
	}

	return report
}

// convertMonitorStatus converts a types.Status to a controller.MonitorStatus.
func (rb *ReportBuilder) convertMonitorStatus(status *types.Status) *controller.MonitorStatus {
	// Determine health status from conditions and events
	health := controller.HealthStatusHealthy
	message := ""
	errorCount := 0

	// Check conditions for unhealthy state
	for _, cond := range status.Conditions {
		if cond.Status == types.ConditionFalse || cond.Status == types.ConditionUnknown {
			if cond.Status == types.ConditionFalse {
				health = controller.HealthStatusCritical
			} else if health != controller.HealthStatusCritical {
				health = controller.HealthStatusDegraded
			}
			message = cond.Message
		}
	}

	// Check events for errors
	for _, event := range status.Events {
		if event.Severity == types.EventError {
			errorCount++
			if health != controller.HealthStatusCritical {
				health = controller.HealthStatusDegraded
			}
			if message == "" {
				message = event.Message
			}
		} else if event.Severity == types.EventWarning && health == controller.HealthStatusHealthy {
			health = controller.HealthStatusDegraded
			if message == "" {
				message = event.Message
			}
		}
	}

	// Extract monitor type from source
	monitorType := status.Source
	if md, ok := status.Metadata["monitorType"].(string); ok {
		monitorType = md
	}

	return &controller.MonitorStatus{
		Name:       status.Source,
		Type:       monitorType,
		Status:     health,
		LastRun:    status.Timestamp,
		Message:    message,
		ErrorCount: errorCount,
	}
}

// convertCondition converts a types.Condition to a controller.NodeCondition.
func (rb *ReportBuilder) convertCondition(cond types.Condition) *controller.NodeCondition {
	// Convert condition status to string
	var statusStr string
	switch cond.Status {
	case types.ConditionTrue:
		statusStr = "True"
	case types.ConditionFalse:
		statusStr = "False"
	case types.ConditionUnknown:
		statusStr = "Unknown"
	default:
		statusStr = string(cond.Status)
	}

	return &controller.NodeCondition{
		Type:               cond.Type,
		Status:             statusStr,
		Reason:             cond.Reason,
		Message:            cond.Message,
		LastTransitionTime: cond.Transition,
		LastHeartbeatTime:  time.Now(),
	}
}

// calculateOverallHealth determines the overall health based on monitor statuses and problems.
func (rb *ReportBuilder) calculateOverallHealth(
	monitorStatuses []controller.MonitorStatus,
	activeProblems []controller.ProblemSummary,
) controller.HealthStatus {
	// Default to healthy
	health := controller.HealthStatusHealthy

	// Check for critical problems
	for _, problem := range activeProblems {
		if problem.Severity == "critical" || problem.Severity == "Critical" {
			return controller.HealthStatusCritical
		}
		if problem.Severity == "warning" || problem.Severity == "Warning" {
			if health == controller.HealthStatusHealthy {
				health = controller.HealthStatusDegraded
			}
		}
	}

	// Check monitor statuses
	for _, ms := range monitorStatuses {
		switch ms.Status {
		case controller.HealthStatusCritical:
			return controller.HealthStatusCritical
		case controller.HealthStatusDegraded:
			if health == controller.HealthStatusHealthy {
				health = controller.HealthStatusDegraded
			}
		case controller.HealthStatusUnknown:
			if health == controller.HealthStatusHealthy {
				health = controller.HealthStatusDegraded
			}
		}
	}

	// If no monitors have reported, status is unknown
	if len(monitorStatuses) == 0 {
		return controller.HealthStatusUnknown
	}

	return health
}

// GetStats returns current statistics.
func (rb *ReportBuilder) GetStats() (statusesProcessed, problemsDetected, remediationsRun int64) {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.statusesProcessed, rb.problemsDetected, rb.remediationsRun
}

// GetActiveProblemsCount returns the number of active problems.
func (rb *ReportBuilder) GetActiveProblemsCount() int {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return len(rb.activeProblems)
}

// GetMonitorStatusCount returns the number of tracked monitor statuses.
func (rb *ReportBuilder) GetMonitorStatusCount() int {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return len(rb.monitorStatuses)
}
