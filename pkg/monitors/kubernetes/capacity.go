// Package kubernetes provides Kubernetes-specific health monitors
package kubernetes

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/supporttools/node-doctor/pkg/monitors"
	"github.com/supporttools/node-doctor/pkg/types"
)

// Constants for default configuration values
const (
	defaultCapacityWarningThreshold  = 90.0 // 90%
	defaultCapacityCriticalThreshold = 95.0 // 95%
	defaultCapacityFailureThreshold  = 3
	defaultCapacityAPITimeout        = 10 * time.Second
)

// CapacityClient defines the interface for querying pod capacity
// This interface allows for dependency injection during testing
type CapacityClient interface {
	GetNode(ctx context.Context, nodeName string) (*corev1.Node, error)
	ListPods(ctx context.Context, fieldSelector string) (*corev1.PodList, error)
}

// defaultCapacityClient implements CapacityClient using the Kubernetes clientset
type defaultCapacityClient struct {
	clientset kubernetes.Interface
	nodeName  string
	timeout   time.Duration
}

// newDefaultCapacityClient creates a new default capacity client
func newDefaultCapacityClient(config *CapacityMonitorConfig) (*defaultCapacityClient, error) {
	// Create in-cluster configuration
	restConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to create in-cluster config: %w", err)
	}

	// Set timeout
	restConfig.Timeout = config.APITimeout

	// Create clientset
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes clientset: %w", err)
	}

	return &defaultCapacityClient{
		clientset: clientset,
		nodeName:  config.NodeName,
		timeout:   config.APITimeout,
	}, nil
}

// GetNode retrieves the node object from the Kubernetes API
func (c *defaultCapacityClient) GetNode(ctx context.Context, nodeName string) (*corev1.Node, error) {
	node, err := c.clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get node %s: %w", nodeName, err)
	}
	return node, nil
}

// ListPods retrieves the list of pods matching the field selector
func (c *defaultCapacityClient) ListPods(ctx context.Context, fieldSelector string) (*corev1.PodList, error) {
	podList, err := c.clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: fieldSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}
	return podList, nil
}

// CapacityMonitorConfig holds the configuration for the capacity monitor
type CapacityMonitorConfig struct {
	// Node to monitor (auto-detected from NODE_NAME env var if not set)
	NodeName string

	// Threshold percentages for alerting
	WarningThreshold  float64 // Default: 90.0 (%)
	CriticalThreshold float64 // Default: 95.0 (%)

	// Failure tracking
	FailureThreshold int // Default: 3 consecutive failures

	// Timeouts and feature flags
	APITimeout       time.Duration // Default: 10s
	CheckAllocatable bool          // Use allocatable vs capacity (default: true)
}

// CapacityMonitor monitors pod capacity on a Kubernetes node
type CapacityMonitor struct {
	*monitors.BaseMonitor
	config *CapacityMonitorConfig
	client CapacityClient

	// Thread-safe state tracking
	mu                  sync.Mutex
	consecutiveFailures int
	currentUtilization  float64
	warningState        bool // Currently in warning state
	criticalState       bool // Currently in critical state
}

// NewCapacityMonitor creates a new pod capacity monitor instance
func NewCapacityMonitor(ctx context.Context, config types.MonitorConfig) (types.Monitor, error) {
	// Parse and validate configuration
	capacityConfig, err := parseCapacityConfig(config.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse capacity config: %w", err)
	}

	// Apply defaults
	if err := capacityConfig.applyDefaults(); err != nil {
		return nil, fmt.Errorf("failed to apply defaults: %w", err)
	}

	// Create Kubernetes client
	client, err := newDefaultCapacityClient(capacityConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create capacity client: %w", err)
	}

	return createCapacityMonitor(ctx, config, capacityConfig, client)
}

// NewCapacityMonitorWithClient creates a new capacity monitor with a custom client
// This is primarily used for testing to allow dependency injection of mock clients
func NewCapacityMonitorWithClient(ctx context.Context, config types.MonitorConfig, client CapacityClient) (types.Monitor, error) {
	// Parse and validate configuration
	capacityConfig, err := parseCapacityConfig(config.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse capacity config: %w", err)
	}

	// Apply defaults
	if err := capacityConfig.applyDefaults(); err != nil {
		return nil, fmt.Errorf("failed to apply defaults: %w", err)
	}

	return createCapacityMonitor(ctx, config, capacityConfig, client)
}

// createCapacityMonitor is the common factory logic for creating a capacity monitor
func createCapacityMonitor(ctx context.Context, config types.MonitorConfig, capacityConfig *CapacityMonitorConfig, client CapacityClient) (types.Monitor, error) {
	// Create base monitor
	baseMonitor, err := monitors.NewBaseMonitor(config.Name, config.Interval, config.Timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to create base monitor: %w", err)
	}

	// Create capacity monitor
	monitor := &CapacityMonitor{
		BaseMonitor: baseMonitor,
		config:      capacityConfig,
		client:      client,
	}

	// Set check function
	if err := baseMonitor.SetCheckFunc(monitor.checkCapacity); err != nil {
		return nil, fmt.Errorf("failed to set check function: %w", err)
	}

	return monitor, nil
}

// checkCapacity performs the pod capacity check
func (m *CapacityMonitor) checkCapacity(ctx context.Context) (*types.Status, error) {
	status := types.NewStatus(m.GetName())

	// Query node capacity
	node, err := m.client.GetNode(ctx, m.config.NodeName)
	if err != nil {
		m.trackFailure(status, fmt.Sprintf("Failed to get node: %v", err))
		return status, nil
	}

	// Get capacity metric (allocatable or total)
	var capacityMetric resource.Quantity
	if m.config.CheckAllocatable {
		capacityMetric = node.Status.Allocatable[corev1.ResourcePods]
	} else {
		capacityMetric = node.Status.Capacity[corev1.ResourcePods]
	}

	capacity := capacityMetric.Value()
	if capacity == 0 {
		m.trackFailure(status, "Node has zero pod capacity")
		return status, nil
	}

	// List and count running pods on this node
	fieldSelector := fmt.Sprintf("spec.nodeName=%s", m.config.NodeName)
	podList, err := m.client.ListPods(ctx, fieldSelector)
	if err != nil {
		m.trackFailure(status, fmt.Sprintf("Failed to list pods: %v", err))
		return status, nil
	}

	runningPods := countRunningPods(podList)

	// Calculate utilization percentage
	utilization := (float64(runningPods) / float64(capacity)) * 100

	// Evaluate thresholds and report
	m.evaluateThresholds(utilization, runningPods, capacity, status)

	return status, nil
}

// countRunningPods counts the number of pods in Running phase
func countRunningPods(podList *corev1.PodList) int64 {
	count := int64(0)
	for _, pod := range podList.Items {
		if pod.Status.Phase == corev1.PodRunning {
			count++
		}
	}
	return count
}

// evaluateThresholds determines the current state based on utilization
func (m *CapacityMonitor) evaluateThresholds(utilization float64, running int64, capacity int64, status *types.Status) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.currentUtilization = utilization

	if utilization >= m.config.CriticalThreshold {
		// CRITICAL: >= 95%
		m.handleCriticalState(running, capacity, status)
	} else if utilization >= m.config.WarningThreshold {
		// WARNING: 90-94%
		m.handleWarningState(running, capacity, status)
	} else {
		// NORMAL: < 90%
		m.handleNormalState(running, capacity, status)
	}
}

// handleCriticalState reports critical pod capacity pressure
func (m *CapacityMonitor) handleCriticalState(running, capacity int64, status *types.Status) {
	if !m.criticalState {
		// First time entering critical state
		status.AddEvent(types.NewEvent(
			types.EventError,
			"PodCapacityPressure",
			fmt.Sprintf("Node pod capacity at %.1f%% (%d/%d pods)", m.currentUtilization, running, capacity),
		))
	}

	// Always set condition when in critical state
	status.AddCondition(types.NewCondition(
		"PodCapacityPressure",
		types.ConditionTrue,
		"HighPodUtilization",
		fmt.Sprintf("Pod capacity at %.1f%% (%d/%d), exceeds %.0f%% threshold", m.currentUtilization, running, capacity, m.config.CriticalThreshold),
	))

	m.criticalState = true
	m.warningState = false
	m.consecutiveFailures = 0
}

// handleWarningState reports warning-level pod capacity
func (m *CapacityMonitor) handleWarningState(running, capacity int64, status *types.Status) {
	if !m.warningState {
		// First time entering warning state
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"PodCapacityWarning",
			fmt.Sprintf("Node pod capacity at %.1f%% (%d/%d pods)", m.currentUtilization, running, capacity),
		))
	}

	m.warningState = true
	m.criticalState = false
	m.consecutiveFailures = 0
}

// handleNormalState reports normal capacity and handles recovery
func (m *CapacityMonitor) handleNormalState(running, capacity int64, status *types.Status) {
	// Recovery event if coming from warning/critical
	if m.criticalState || m.warningState {
		status.AddEvent(types.NewEvent(
			types.EventInfo,
			"PodCapacityRecovered",
			fmt.Sprintf("Pod capacity recovered to %.1f%% (%d/%d pods)", m.currentUtilization, running, capacity),
		))
	}

	// Clear condition if recovering from critical
	if m.criticalState {
		status.AddCondition(types.NewCondition(
			"PodCapacityPressure",
			types.ConditionFalse,
			"NormalPodUtilization",
			fmt.Sprintf("Pod capacity at %.1f%% (%d/%d), below thresholds", m.currentUtilization, running, capacity),
		))
	}

	m.criticalState = false
	m.warningState = false
	m.consecutiveFailures = 0
}

// trackFailure tracks consecutive failures and reports unhealthy state
func (m *CapacityMonitor) trackFailure(status *types.Status, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.consecutiveFailures++

	// Report error event
	status.AddEvent(types.NewEvent(
		types.EventError,
		"CapacityCheckFailed",
		message,
	))

	// If we've exceeded failure threshold, set unhealthy condition
	if m.consecutiveFailures >= m.config.FailureThreshold {
		status.AddCondition(types.NewCondition(
			"PodCapacityUnhealthy",
			types.ConditionTrue,
			"RepeatedCheckFailures",
			fmt.Sprintf("Pod capacity check has failed %d consecutive times", m.consecutiveFailures),
		))
	}
}

// parseCapacityConfig parses the capacity monitor configuration from a map
func parseCapacityConfig(configMap map[string]interface{}) (*CapacityMonitorConfig, error) {
	if configMap == nil {
		return &CapacityMonitorConfig{}, nil
	}

	config := &CapacityMonitorConfig{}

	// Parse nodeName (string)
	if val, ok := configMap["nodeName"]; ok {
		strVal, ok := val.(string)
		if !ok {
			return nil, fmt.Errorf("nodeName must be a string, got %T", val)
		}
		config.NodeName = strVal
	}

	// Parse warningThreshold (float64)
	if val, ok := configMap["warningThreshold"]; ok {
		switch v := val.(type) {
		case float64:
			config.WarningThreshold = v
		case int:
			config.WarningThreshold = float64(v)
		default:
			return nil, fmt.Errorf("warningThreshold must be a number, got %T", val)
		}
	}

	// Parse criticalThreshold (float64)
	if val, ok := configMap["criticalThreshold"]; ok {
		switch v := val.(type) {
		case float64:
			config.CriticalThreshold = v
		case int:
			config.CriticalThreshold = float64(v)
		default:
			return nil, fmt.Errorf("criticalThreshold must be a number, got %T", val)
		}
	}

	// Parse failureThreshold (int)
	if val, ok := configMap["failureThreshold"]; ok {
		switch v := val.(type) {
		case int:
			config.FailureThreshold = v
		case float64:
			config.FailureThreshold = int(v)
		default:
			return nil, fmt.Errorf("failureThreshold must be a number, got %T", val)
		}
	}

	// Parse apiTimeout (duration)
	if val, ok := configMap["apiTimeout"]; ok {
		switch v := val.(type) {
		case string:
			duration, err := time.ParseDuration(v)
			if err != nil {
				return nil, fmt.Errorf("invalid apiTimeout duration: %w", err)
			}
			config.APITimeout = duration
		case float64:
			config.APITimeout = time.Duration(v * float64(time.Second))
		case int:
			config.APITimeout = time.Duration(v) * time.Second
		default:
			return nil, fmt.Errorf("apiTimeout must be duration string or number, got %T", val)
		}
	}

	// Parse checkAllocatable (bool)
	if val, ok := configMap["checkAllocatable"]; ok {
		boolVal, ok := val.(bool)
		if !ok {
			return nil, fmt.Errorf("checkAllocatable must be a boolean, got %T", val)
		}
		config.CheckAllocatable = boolVal
	} else {
		// Default to true if not specified
		config.CheckAllocatable = true
	}

	return config, nil
}

// applyDefaults applies default values to the configuration
func (c *CapacityMonitorConfig) applyDefaults() error {
	// Auto-detect node name from environment if not set
	if c.NodeName == "" {
		nodeName := os.Getenv("NODE_NAME")
		if nodeName == "" {
			return fmt.Errorf("nodeName not configured and NODE_NAME environment variable not set")
		}
		c.NodeName = nodeName
	}

	// Apply warning threshold default
	if c.WarningThreshold == 0 {
		c.WarningThreshold = defaultCapacityWarningThreshold
	}

	// Apply critical threshold default
	if c.CriticalThreshold == 0 {
		c.CriticalThreshold = defaultCapacityCriticalThreshold
	}

	// Apply failure threshold default
	if c.FailureThreshold == 0 {
		c.FailureThreshold = defaultCapacityFailureThreshold
	}

	// Apply API timeout default
	if c.APITimeout == 0 {
		c.APITimeout = defaultCapacityAPITimeout
	}

	// Validate thresholds
	if c.WarningThreshold < 0 || c.WarningThreshold > 100 {
		return fmt.Errorf("warningThreshold must be between 0 and 100, got %.1f", c.WarningThreshold)
	}
	if c.CriticalThreshold < 0 || c.CriticalThreshold > 100 {
		return fmt.Errorf("criticalThreshold must be between 0 and 100, got %.1f", c.CriticalThreshold)
	}
	if c.WarningThreshold >= c.CriticalThreshold {
		return fmt.Errorf("warningThreshold (%.1f) must be less than criticalThreshold (%.1f)", c.WarningThreshold, c.CriticalThreshold)
	}
	if c.FailureThreshold < 1 {
		return fmt.Errorf("failureThreshold must be at least 1, got %d", c.FailureThreshold)
	}

	return nil
}

// ValidateCapacityConfig validates the capacity monitor configuration
func ValidateCapacityConfig(config types.MonitorConfig) error {
	if config.Name == "" {
		return fmt.Errorf("monitor name is required")
	}

	if config.Type != "kubernetes-capacity-check" {
		return fmt.Errorf("invalid monitor type: %s", config.Type)
	}

	// Parse and validate configuration
	capacityConfig, err := parseCapacityConfig(config.Config)
	if err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	// Apply defaults and validate
	if err := capacityConfig.applyDefaults(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	return nil
}

// init registers the capacity monitor with the monitor registry
func init() {
	monitors.Register(monitors.MonitorInfo{
		Type:        "kubernetes-capacity-check",
		Factory:     NewCapacityMonitor,
		Validator:   ValidateCapacityConfig,
		Description: "Monitors pod capacity and alerts when approaching node limits",
	})
}
