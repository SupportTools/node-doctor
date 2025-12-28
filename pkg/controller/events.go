package controller

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Event reasons for different scenarios
const (
	// Cluster health events
	EventReasonClusterDegraded   = "ClusterDegraded"
	EventReasonClusterCritical   = "ClusterCritical"
	EventReasonClusterRecovered  = "ClusterRecovered"
	EventReasonNodeHealthChanged = "NodeHealthChanged"

	// Problem events
	EventReasonProblemDetected    = "ProblemDetected"
	EventReasonProblemResolved    = "ProblemResolved"
	EventReasonClusterWideProblem = "ClusterWideProblem"

	// Correlation events
	EventReasonCorrelationDetected = "CorrelationDetected"
	EventReasonCorrelationResolved = "CorrelationResolved"

	// Remediation events
	EventReasonLeaseGranted         = "RemediationLeaseGranted"
	EventReasonLeaseDenied          = "RemediationLeaseDenied"
	EventReasonLeaseExpired         = "RemediationLeaseExpired"
	EventReasonRemediationStarted   = "RemediationStarted"
	EventReasonRemediationCompleted = "RemediationCompleted"
	EventReasonRemediationFailed    = "RemediationFailed"
)

// Event types
const (
	EventTypeNormal  = corev1.EventTypeNormal
	EventTypeWarning = corev1.EventTypeWarning
)

// EventRecorder creates Kubernetes Events for cluster-level issues
type EventRecorder struct {
	client         kubernetes.Interface
	namespace      string
	enabled        bool
	controllerName string
	componentName  string

	// Rate limiting to prevent event spam
	mu              sync.RWMutex
	lastEventTime   map[string]time.Time
	rateLimitPeriod time.Duration
}

// EventRecorderConfig holds configuration for the EventRecorder
type EventRecorderConfig struct {
	Kubeconfig      string
	InCluster       bool
	Namespace       string
	Enabled         bool
	RateLimitPeriod time.Duration
}

// NewEventRecorder creates a new Kubernetes event recorder
func NewEventRecorder(config *EventRecorderConfig) (*EventRecorder, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	recorder := &EventRecorder{
		namespace:       config.Namespace,
		enabled:         config.Enabled,
		controllerName:  "node-doctor-controller",
		componentName:   "node-doctor",
		lastEventTime:   make(map[string]time.Time),
		rateLimitPeriod: config.RateLimitPeriod,
	}

	if recorder.rateLimitPeriod == 0 {
		recorder.rateLimitPeriod = 5 * time.Minute
	}

	if recorder.namespace == "" {
		recorder.namespace = "node-doctor"
	}

	if !config.Enabled {
		log.Printf("[INFO] Kubernetes events disabled")
		return recorder, nil
	}

	// Build Kubernetes client
	var restConfig *rest.Config
	var err error

	if config.InCluster {
		restConfig, err = rest.InClusterConfig()
		if err != nil {
			log.Printf("[WARN] Failed to create in-cluster config, events disabled: %v", err)
			recorder.enabled = false
			return recorder, nil
		}
	} else if config.Kubeconfig != "" {
		restConfig, err = clientcmd.BuildConfigFromFlags("", config.Kubeconfig)
		if err != nil {
			log.Printf("[WARN] Failed to create kubeconfig client, events disabled: %v", err)
			recorder.enabled = false
			return recorder, nil
		}
	} else {
		log.Printf("[WARN] No Kubernetes configuration available, events disabled")
		recorder.enabled = false
		return recorder, nil
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		log.Printf("[WARN] Failed to create Kubernetes client, events disabled: %v", err)
		recorder.enabled = false
		return recorder, nil
	}

	recorder.client = client
	log.Printf("[INFO] Kubernetes event recorder initialized (namespace: %s)", recorder.namespace)

	return recorder, nil
}

// IsEnabled returns whether event recording is enabled
func (r *EventRecorder) IsEnabled() bool {
	return r.enabled && r.client != nil
}

// shouldRateLimit checks if an event should be rate-limited
func (r *EventRecorder) shouldRateLimit(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	lastTime, exists := r.lastEventTime[key]
	if !exists {
		r.lastEventTime[key] = time.Now()
		return false
	}

	if time.Since(lastTime) < r.rateLimitPeriod {
		return true
	}

	r.lastEventTime[key] = time.Now()
	return false
}

// cleanupRateLimitCache removes old entries from the rate limit cache
func (r *EventRecorder) cleanupRateLimitCache() {
	r.mu.Lock()
	defer r.mu.Unlock()

	cutoff := time.Now().Add(-r.rateLimitPeriod * 2)
	for key, lastTime := range r.lastEventTime {
		if lastTime.Before(cutoff) {
			delete(r.lastEventTime, key)
		}
	}
}

// RecordClusterHealthChange records an event when cluster health status changes
func (r *EventRecorder) RecordClusterHealthChange(ctx context.Context, status *ClusterStatus, previousHealth HealthStatus) {
	if !r.IsEnabled() {
		return
	}

	var reason, eventType, message string

	switch status.OverallHealth {
	case HealthStatusCritical:
		reason = EventReasonClusterCritical
		eventType = EventTypeWarning
		message = fmt.Sprintf("Cluster health critical: %d/%d nodes in critical state, %d degraded",
			status.CriticalNodes, status.TotalNodes, status.DegradedNodes)
	case HealthStatusDegraded:
		reason = EventReasonClusterDegraded
		eventType = EventTypeWarning
		message = fmt.Sprintf("Cluster health degraded: %d/%d nodes degraded, %d healthy",
			status.DegradedNodes, status.TotalNodes, status.HealthyNodes)
	case HealthStatusHealthy:
		if previousHealth == HealthStatusCritical || previousHealth == HealthStatusDegraded {
			reason = EventReasonClusterRecovered
			eventType = EventTypeNormal
			message = fmt.Sprintf("Cluster health recovered: %d/%d nodes healthy",
				status.HealthyNodes, status.TotalNodes)
		} else {
			return // Don't emit event for healthy -> healthy
		}
	default:
		return
	}

	key := fmt.Sprintf("cluster-health-%s", status.OverallHealth)
	if r.shouldRateLimit(key) {
		return
	}

	r.recordEvent(ctx, nil, reason, eventType, message)
}

// RecordClusterWideProblem records an event when a cluster-wide problem is detected
func (r *EventRecorder) RecordClusterWideProblem(ctx context.Context, problem *ClusterProblem) {
	if !r.IsEnabled() || problem == nil {
		return
	}

	key := fmt.Sprintf("cluster-problem-%s-%s", problem.Type, problem.Severity)
	if r.shouldRateLimit(key) {
		return
	}

	eventType := EventTypeWarning
	message := fmt.Sprintf("Cluster-wide %s problem detected (%s severity): %s. Affected nodes: %s",
		problem.Type, problem.Severity, problem.Message, strings.Join(problem.AffectedNodes, ", "))

	r.recordEvent(ctx, nil, EventReasonClusterWideProblem, eventType, message)
}

// RecordCorrelation records an event when a correlation is detected
func (r *EventRecorder) RecordCorrelation(ctx context.Context, correlation *Correlation) {
	if !r.IsEnabled() || correlation == nil {
		return
	}

	key := fmt.Sprintf("correlation-%s", correlation.ID)
	if r.shouldRateLimit(key) {
		return
	}

	eventType := EventTypeWarning
	message := fmt.Sprintf("Correlation detected (type: %s, confidence: %.0f%%): %s. Affected nodes: %s",
		correlation.Type, correlation.Confidence*100, correlation.Message,
		strings.Join(correlation.AffectedNodes, ", "))

	r.recordEvent(ctx, nil, EventReasonCorrelationDetected, eventType, message)
}

// RecordCorrelationResolved records an event when a correlation is resolved
func (r *EventRecorder) RecordCorrelationResolved(ctx context.Context, correlation *Correlation) {
	if !r.IsEnabled() || correlation == nil {
		return
	}

	key := fmt.Sprintf("correlation-resolved-%s", correlation.ID)
	if r.shouldRateLimit(key) {
		return
	}

	message := fmt.Sprintf("Correlation resolved (type: %s): %s",
		correlation.Type, correlation.Message)

	r.recordEvent(ctx, nil, EventReasonCorrelationResolved, EventTypeNormal, message)
}

// RecordNodeHealthChange records an event when a node's health changes significantly
func (r *EventRecorder) RecordNodeHealthChange(ctx context.Context, nodeName string, previousHealth, currentHealth HealthStatus) {
	if !r.IsEnabled() {
		return
	}

	// Only record significant changes (to/from critical or first report)
	if currentHealth != HealthStatusCritical && previousHealth != HealthStatusCritical {
		return
	}

	key := fmt.Sprintf("node-%s-health-%s", nodeName, currentHealth)
	if r.shouldRateLimit(key) {
		return
	}

	var eventType, message string
	if currentHealth == HealthStatusCritical {
		eventType = EventTypeWarning
		message = fmt.Sprintf("Node %s entered critical state (was %s)", nodeName, previousHealth)
	} else {
		eventType = EventTypeNormal
		message = fmt.Sprintf("Node %s recovered from critical state (now %s)", nodeName, currentHealth)
	}

	// Create event referencing the node
	r.recordEventForNode(ctx, nodeName, EventReasonNodeHealthChanged, eventType, message)
}

// RecordLeaseGranted records an event when a remediation lease is granted
func (r *EventRecorder) RecordLeaseGranted(ctx context.Context, lease *Lease) {
	if !r.IsEnabled() || lease == nil {
		return
	}

	key := fmt.Sprintf("lease-granted-%s-%s", lease.NodeName, lease.RemediationType)
	if r.shouldRateLimit(key) {
		return
	}

	message := fmt.Sprintf("Remediation lease granted to node %s for %s (expires: %s)",
		lease.NodeName, lease.RemediationType, lease.ExpiresAt.Format(time.RFC3339))

	r.recordEventForNode(ctx, lease.NodeName, EventReasonLeaseGranted, EventTypeNormal, message)
}

// RecordLeaseDenied records an event when a remediation lease is denied
func (r *EventRecorder) RecordLeaseDenied(ctx context.Context, nodeName, remediationType, reason string) {
	if !r.IsEnabled() {
		return
	}

	key := fmt.Sprintf("lease-denied-%s-%s", nodeName, remediationType)
	if r.shouldRateLimit(key) {
		return
	}

	message := fmt.Sprintf("Remediation lease denied for node %s (%s): %s",
		nodeName, remediationType, reason)

	r.recordEventForNode(ctx, nodeName, EventReasonLeaseDenied, EventTypeWarning, message)
}

// RecordLeaseExpired records an event when a remediation lease expires
func (r *EventRecorder) RecordLeaseExpired(ctx context.Context, lease *Lease) {
	if !r.IsEnabled() || lease == nil {
		return
	}

	key := fmt.Sprintf("lease-expired-%s", lease.ID)
	if r.shouldRateLimit(key) {
		return
	}

	message := fmt.Sprintf("Remediation lease for node %s (%s) expired without completion",
		lease.NodeName, lease.RemediationType)

	r.recordEventForNode(ctx, lease.NodeName, EventReasonLeaseExpired, EventTypeWarning, message)
}

// RecordProblemDetected records an event when a significant problem is detected on a node
func (r *EventRecorder) RecordProblemDetected(ctx context.Context, nodeName string, problem *ProblemSummary) {
	if !r.IsEnabled() || problem == nil {
		return
	}

	// Only record critical problems to avoid event spam
	if problem.Severity != "critical" {
		return
	}

	key := fmt.Sprintf("problem-%s-%s", nodeName, problem.Type)
	if r.shouldRateLimit(key) {
		return
	}

	message := fmt.Sprintf("Critical problem detected on node %s: %s - %s",
		nodeName, problem.Type, problem.Message)

	r.recordEventForNode(ctx, nodeName, EventReasonProblemDetected, EventTypeWarning, message)
}

// RecordProblemResolved records an event when a problem is resolved
func (r *EventRecorder) RecordProblemResolved(ctx context.Context, nodeName string, problemType string) {
	if !r.IsEnabled() {
		return
	}

	key := fmt.Sprintf("problem-resolved-%s-%s", nodeName, problemType)
	if r.shouldRateLimit(key) {
		return
	}

	message := fmt.Sprintf("Problem resolved on node %s: %s", nodeName, problemType)

	r.recordEventForNode(ctx, nodeName, EventReasonProblemResolved, EventTypeNormal, message)
}

// recordEvent creates a Kubernetes event for the controller itself
func (r *EventRecorder) recordEvent(ctx context.Context, ref *corev1.ObjectReference, reason, eventType, message string) {
	if ref == nil {
		// Create a reference to a controller-level event
		// We use a ConfigMap or Pod as the involved object
		ref = &corev1.ObjectReference{
			APIVersion: "v1",
			Kind:       "Namespace",
			Name:       r.namespace,
			Namespace:  r.namespace,
		}
	}

	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "node-doctor-controller-",
			Namespace:    r.namespace,
		},
		InvolvedObject: *ref,
		Reason:         reason,
		Message:        message,
		Type:           eventType,
		Source: corev1.EventSource{
			Component: r.componentName,
		},
		FirstTimestamp: metav1.Now(),
		LastTimestamp:  metav1.Now(),
		Count:          1,
	}

	_, err := r.client.CoreV1().Events(r.namespace).Create(ctx, event, metav1.CreateOptions{})
	if err != nil {
		log.Printf("[WARN] Failed to create Kubernetes event: %v", err)
	} else {
		log.Printf("[DEBUG] Created Kubernetes event: %s - %s", reason, message)
	}
}

// recordEventForNode creates a Kubernetes event referencing a specific node
func (r *EventRecorder) recordEventForNode(ctx context.Context, nodeName, reason, eventType, message string) {
	ref := &corev1.ObjectReference{
		APIVersion: "v1",
		Kind:       "Node",
		Name:       nodeName,
	}

	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "node-doctor-",
			Namespace:    r.namespace,
		},
		InvolvedObject: *ref,
		Reason:         reason,
		Message:        message,
		Type:           eventType,
		Source: corev1.EventSource{
			Component: r.componentName,
		},
		FirstTimestamp: metav1.Now(),
		LastTimestamp:  metav1.Now(),
		Count:          1,
	}

	_, err := r.client.CoreV1().Events(r.namespace).Create(ctx, event, metav1.CreateOptions{})
	if err != nil {
		log.Printf("[WARN] Failed to create Kubernetes event for node %s: %v", nodeName, err)
	} else {
		log.Printf("[DEBUG] Created Kubernetes event for node %s: %s - %s", nodeName, reason, message)
	}
}
