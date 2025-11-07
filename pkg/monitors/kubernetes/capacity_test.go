package kubernetes

import (
	"context"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/supporttools/node-doctor/pkg/types"
)

// mockCapacityClient implements CapacityClient for testing
type mockCapacityClient struct {
	node    *corev1.Node
	nodeErr error
	pods    *corev1.PodList
	podsErr error
	delay   time.Duration
}

func (m *mockCapacityClient) GetNode(ctx context.Context, nodeName string) (*corev1.Node, error) {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return m.node, m.nodeErr
}

func (m *mockCapacityClient) ListPods(ctx context.Context, fieldSelector string) (*corev1.PodList, error) {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return m.pods, m.podsErr
}

// createTestCapacityMonitor creates a properly initialized monitor with mock client for testing
func createTestCapacityMonitor(t *testing.T, configMap map[string]interface{}, mockClient CapacityClient) *CapacityMonitor {
	t.Helper()

	monitorConfig := types.MonitorConfig{
		Name:     "test-capacity-monitor",
		Type:     "kubernetes-capacity-check",
		Interval: 10 * time.Second,
		Timeout:  5 * time.Second,
		Config:   configMap,
	}

	ctx := context.Background()
	monitorInterface, err := NewCapacityMonitorWithClient(ctx, monitorConfig, mockClient)
	if err != nil {
		t.Fatalf("createTestCapacityMonitor() error: %v", err)
	}

	return monitorInterface.(*CapacityMonitor)
}

// createMockNode creates a mock node with specified capacity and allocatable
func createMockNode(capacity, allocatable int64) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				corev1.ResourcePods: *resource.NewQuantity(capacity, resource.DecimalSI),
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourcePods: *resource.NewQuantity(allocatable, resource.DecimalSI),
			},
		},
	}
}

// createMockPodList creates a mock pod list with specified count and phase
func createMockPodList(count int, phase corev1.PodPhase) *corev1.PodList {
	pods := make([]corev1.Pod, count)
	for i := range pods {
		pods[i] = corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod-" + string(rune(i)),
				Namespace: "default",
			},
			Status: corev1.PodStatus{
				Phase: phase,
			},
		}
	}
	return &corev1.PodList{Items: pods}
}

// TestCapacityMonitor_NormalCapacity tests normal capacity (< 90%)
func TestCapacityMonitor_NormalCapacity(t *testing.T) {
	mockClient := &mockCapacityClient{
		node:    createMockNode(110, 100),
		nodeErr: nil,
		pods:    createMockPodList(50, corev1.PodRunning), // 50%
		podsErr: nil,
	}

	monitor := createTestCapacityMonitor(t, map[string]interface{}{
		"nodeName":          "test-node",
		"warningThreshold":  90.0,
		"criticalThreshold": 95.0,
		"failureThreshold":  3,
		"checkAllocatable":  true,
	}, mockClient)

	ctx := context.Background()
	status, err := monitor.checkCapacity(ctx)

	if err != nil {
		t.Fatalf("checkCapacity() unexpected error: %v", err)
	}

	// Should not have any warning or critical conditions
	for _, cond := range status.Conditions {
		if cond.Type == "PodCapacityPressure" && cond.Status == types.ConditionTrue {
			t.Error("Unexpected PodCapacityPressure condition in normal state")
		}
	}

	// Should not have any events in normal state (only on recovery)
	if len(status.Events) > 0 {
		t.Errorf("Expected no events in normal state, got %d events", len(status.Events))
	}
}

// TestCapacityMonitor_WarningThreshold tests warning threshold (90%)
func TestCapacityMonitor_WarningThreshold(t *testing.T) {
	mockClient := &mockCapacityClient{
		node:    createMockNode(110, 100),
		nodeErr: nil,
		pods:    createMockPodList(90, corev1.PodRunning), // 90%
		podsErr: nil,
	}

	monitor := createTestCapacityMonitor(t, map[string]interface{}{
		"nodeName":          "test-node",
		"warningThreshold":  90.0,
		"criticalThreshold": 95.0,
		"failureThreshold":  3,
		"checkAllocatable":  true,
	}, mockClient)

	ctx := context.Background()
	status, err := monitor.checkCapacity(ctx)

	if err != nil {
		t.Fatalf("checkCapacity() unexpected error: %v", err)
	}

	// Should have warning event
	foundWarning := false
	for _, event := range status.Events {
		if event.Reason == "PodCapacityWarning" {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Error("Expected PodCapacityWarning event")
	}

	// Should NOT have critical condition
	for _, cond := range status.Conditions {
		if cond.Type == "PodCapacityPressure" && cond.Status == types.ConditionTrue {
			t.Error("Unexpected critical condition at warning threshold")
		}
	}
}

// TestCapacityMonitor_CriticalThreshold tests critical threshold (95%)
func TestCapacityMonitor_CriticalThreshold(t *testing.T) {
	mockClient := &mockCapacityClient{
		node:    createMockNode(110, 100),
		nodeErr: nil,
		pods:    createMockPodList(95, corev1.PodRunning), // 95%
		podsErr: nil,
	}

	monitor := createTestCapacityMonitor(t, map[string]interface{}{
		"nodeName":          "test-node",
		"warningThreshold":  90.0,
		"criticalThreshold": 95.0,
		"failureThreshold":  3,
		"checkAllocatable":  true,
	}, mockClient)

	ctx := context.Background()
	status, err := monitor.checkCapacity(ctx)

	if err != nil {
		t.Fatalf("checkCapacity() unexpected error: %v", err)
	}

	// Should have critical event
	foundCritical := false
	for _, event := range status.Events {
		if event.Reason == "PodCapacityPressure" {
			foundCritical = true
			break
		}
	}
	if !foundCritical {
		t.Error("Expected PodCapacityPressure event")
	}

	// Should have critical condition
	foundCondition := false
	for _, cond := range status.Conditions {
		if cond.Type == "PodCapacityPressure" && cond.Status == types.ConditionTrue {
			foundCondition = true
			break
		}
	}
	if !foundCondition {
		t.Error("Expected PodCapacityPressure condition")
	}
}

// TestCapacityMonitor_RecoveryFromWarning tests recovery from warning state
func TestCapacityMonitor_RecoveryFromWarning(t *testing.T) {
	mockClient := &mockCapacityClient{
		node:    createMockNode(110, 100),
		nodeErr: nil,
		pods:    createMockPodList(90, corev1.PodRunning), // 90%
		podsErr: nil,
	}

	monitor := createTestCapacityMonitor(t, map[string]interface{}{
		"nodeName":          "test-node",
		"warningThreshold":  90.0,
		"criticalThreshold": 95.0,
		"failureThreshold":  3,
		"checkAllocatable":  true,
	}, mockClient)

	ctx := context.Background()

	// First check - enter warning state
	_, err := monitor.checkCapacity(ctx)
	if err != nil {
		t.Fatalf("First checkCapacity() unexpected error: %v", err)
	}

	// Update mock to return normal capacity
	mockClient.pods = createMockPodList(50, corev1.PodRunning) // 50%

	// Second check - should recover
	status, err := monitor.checkCapacity(ctx)
	if err != nil {
		t.Fatalf("Second checkCapacity() unexpected error: %v", err)
	}

	// Should have recovery event
	foundRecovery := false
	for _, event := range status.Events {
		if event.Reason == "PodCapacityRecovered" {
			foundRecovery = true
			break
		}
	}
	if !foundRecovery {
		t.Error("Expected PodCapacityRecovered event")
	}
}

// TestCapacityMonitor_RecoveryFromCritical tests recovery from critical state
func TestCapacityMonitor_RecoveryFromCritical(t *testing.T) {
	mockClient := &mockCapacityClient{
		node:    createMockNode(110, 100),
		nodeErr: nil,
		pods:    createMockPodList(98, corev1.PodRunning), // 98%
		podsErr: nil,
	}

	monitor := createTestCapacityMonitor(t, map[string]interface{}{
		"nodeName":          "test-node",
		"warningThreshold":  90.0,
		"criticalThreshold": 95.0,
		"failureThreshold":  3,
		"checkAllocatable":  true,
	}, mockClient)

	ctx := context.Background()

	// First check - enter critical state
	_, err := monitor.checkCapacity(ctx)
	if err != nil {
		t.Fatalf("First checkCapacity() unexpected error: %v", err)
	}

	// Update mock to return normal capacity
	mockClient.pods = createMockPodList(50, corev1.PodRunning) // 50%

	// Second check - should recover
	status, err := monitor.checkCapacity(ctx)
	if err != nil {
		t.Fatalf("Second checkCapacity() unexpected error: %v", err)
	}

	// Should have recovery event
	foundRecovery := false
	for _, event := range status.Events {
		if event.Reason == "PodCapacityRecovered" {
			foundRecovery = true
			break
		}
	}
	if !foundRecovery {
		t.Error("Expected PodCapacityRecovered event")
	}

	// Should clear critical condition
	for _, cond := range status.Conditions {
		if cond.Type == "PodCapacityPressure" {
			if cond.Status != types.ConditionFalse {
				t.Error("Expected PodCapacityPressure condition to be False after recovery")
			}
		}
	}
}

// TestCapacityMonitor_NodeAPIError tests handling of node API errors
func TestCapacityMonitor_NodeAPIError(t *testing.T) {
	mockClient := &mockCapacityClient{
		node:    nil,
		nodeErr: fmt.Errorf("API error"),
		pods:    createMockPodList(50, corev1.PodRunning),
		podsErr: nil,
	}

	monitor := createTestCapacityMonitor(t, map[string]interface{}{
		"nodeName":          "test-node",
		"warningThreshold":  90.0,
		"criticalThreshold": 95.0,
		"failureThreshold":  3,
		"checkAllocatable":  true,
	}, mockClient)

	ctx := context.Background()
	status, err := monitor.checkCapacity(ctx)

	if err != nil {
		t.Fatalf("checkCapacity() unexpected error: %v", err)
	}

	// Should have error event
	foundError := false
	for _, event := range status.Events {
		if event.Severity == types.EventError && event.Reason == "CapacityCheckFailed" {
			foundError = true
			break
		}
	}
	if !foundError {
		t.Error("Expected CapacityCheckFailed event")
	}
}

// TestCapacityMonitor_PodListAPIError tests handling of pod list API errors
func TestCapacityMonitor_PodListAPIError(t *testing.T) {
	mockClient := &mockCapacityClient{
		node:    createMockNode(110, 100),
		nodeErr: nil,
		pods:    nil,
		podsErr: fmt.Errorf("API error"),
	}

	monitor := createTestCapacityMonitor(t, map[string]interface{}{
		"nodeName":          "test-node",
		"warningThreshold":  90.0,
		"criticalThreshold": 95.0,
		"failureThreshold":  3,
		"checkAllocatable":  true,
	}, mockClient)

	ctx := context.Background()
	status, err := monitor.checkCapacity(ctx)

	if err != nil {
		t.Fatalf("checkCapacity() unexpected error: %v", err)
	}

	// Should have error event
	foundError := false
	for _, event := range status.Events {
		if event.Severity == types.EventError && event.Reason == "CapacityCheckFailed" {
			foundError = true
			break
		}
	}
	if !foundError {
		t.Error("Expected CapacityCheckFailed event")
	}
}

// TestCapacityMonitor_ContextCancellation tests context cancellation handling
func TestCapacityMonitor_ContextCancellation(t *testing.T) {
	mockClient := &mockCapacityClient{
		node:    createMockNode(110, 100),
		nodeErr: nil,
		pods:    createMockPodList(50, corev1.PodRunning),
		podsErr: nil,
		delay:   2 * time.Second, // Simulate slow API call
	}

	monitor := createTestCapacityMonitor(t, map[string]interface{}{
		"nodeName":          "test-node",
		"warningThreshold":  90.0,
		"criticalThreshold": 95.0,
		"failureThreshold":  3,
		"checkAllocatable":  true,
	}, mockClient)

	// Create context with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	status, err := monitor.checkCapacity(ctx)

	if err != nil {
		t.Fatalf("checkCapacity() unexpected error: %v", err)
	}

	// Should handle cancellation gracefully with error event
	if len(status.Events) == 0 {
		t.Error("Expected at least one event after context cancellation")
	}
}

// TestCapacityMonitor_ConsecutiveFailures tests failure threshold tracking
func TestCapacityMonitor_ConsecutiveFailures(t *testing.T) {
	mockClient := &mockCapacityClient{
		node:    nil,
		nodeErr: fmt.Errorf("API error"),
		pods:    createMockPodList(50, corev1.PodRunning),
		podsErr: nil,
	}

	monitor := createTestCapacityMonitor(t, map[string]interface{}{
		"nodeName":          "test-node",
		"warningThreshold":  90.0,
		"criticalThreshold": 95.0,
		"failureThreshold":  3,
		"checkAllocatable":  true,
	}, mockClient)

	ctx := context.Background()

	// First two failures - should not set unhealthy condition
	for i := 0; i < 2; i++ {
		status, err := monitor.checkCapacity(ctx)
		if err != nil {
			t.Fatalf("checkCapacity() iteration %d unexpected error: %v", i+1, err)
		}

		// Should NOT have unhealthy condition yet
		for _, cond := range status.Conditions {
			if cond.Type == "PodCapacityUnhealthy" {
				t.Errorf("Unexpected PodCapacityUnhealthy condition at failure %d", i+1)
			}
		}
	}

	// Third failure - should set unhealthy condition
	status, err := monitor.checkCapacity(ctx)
	if err != nil {
		t.Fatalf("checkCapacity() third iteration unexpected error: %v", err)
	}

	// Should have unhealthy condition now
	foundCondition := false
	for _, cond := range status.Conditions {
		if cond.Type == "PodCapacityUnhealthy" && cond.Status == types.ConditionTrue {
			foundCondition = true
			break
		}
	}
	if !foundCondition {
		t.Error("Expected PodCapacityUnhealthy condition after 3 consecutive failures")
	}
}

// TestCapacityMonitor_CustomThresholds tests custom threshold values
func TestCapacityMonitor_CustomThresholds(t *testing.T) {
	mockClient := &mockCapacityClient{
		node:    createMockNode(110, 100),
		nodeErr: nil,
		pods:    createMockPodList(85, corev1.PodRunning), // 85%
		podsErr: nil,
	}

	monitor := createTestCapacityMonitor(t, map[string]interface{}{
		"nodeName":          "test-node",
		"warningThreshold":  80.0, // Custom: 80%
		"criticalThreshold": 90.0, // Custom: 90%
		"failureThreshold":  3,
		"checkAllocatable":  true,
	}, mockClient)

	ctx := context.Background()
	status, err := monitor.checkCapacity(ctx)

	if err != nil {
		t.Fatalf("checkCapacity() unexpected error: %v", err)
	}

	// Should trigger warning at custom 80% threshold
	foundWarning := false
	for _, event := range status.Events {
		if event.Reason == "PodCapacityWarning" {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Error("Expected warning event at custom 80% threshold")
	}
}

// TestCapacityMonitor_CountRunningPodsOnly tests that only running pods are counted
func TestCapacityMonitor_CountRunningPodsOnly(t *testing.T) {
	// Create pod list with mixed phases
	podList := &corev1.PodList{
		Items: []corev1.Pod{
			{Status: corev1.PodStatus{Phase: corev1.PodRunning}},
			{Status: corev1.PodStatus{Phase: corev1.PodRunning}},
			{Status: corev1.PodStatus{Phase: corev1.PodPending}},
			{Status: corev1.PodStatus{Phase: corev1.PodSucceeded}},
			{Status: corev1.PodStatus{Phase: corev1.PodFailed}},
		},
	}

	count := countRunningPods(podList)

	if count != 2 {
		t.Errorf("countRunningPods() = %d, want 2", count)
	}
}

// TestParseCapacityConfig tests configuration parsing
func TestParseCapacityConfig(t *testing.T) {
	tests := []struct {
		name      string
		configMap map[string]interface{}
		wantErr   bool
	}{
		{
			name: "valid configuration",
			configMap: map[string]interface{}{
				"nodeName":          "test-node",
				"warningThreshold":  90.0,
				"criticalThreshold": 95.0,
				"failureThreshold":  3,
				"apiTimeout":        "10s",
				"checkAllocatable":  true,
			},
			wantErr: false,
		},
		{
			name: "valid with numeric timeout",
			configMap: map[string]interface{}{
				"nodeName":          "test-node",
				"warningThreshold":  90.0,
				"criticalThreshold": 95.0,
				"apiTimeout":        10, // Numeric seconds
			},
			wantErr: false,
		},
		{
			name: "invalid nodeName type",
			configMap: map[string]interface{}{
				"nodeName": 123,
			},
			wantErr: true,
		},
		{
			name: "invalid warningThreshold type",
			configMap: map[string]interface{}{
				"warningThreshold": "not-a-number",
			},
			wantErr: true,
		},
		{
			name: "invalid apiTimeout format",
			configMap: map[string]interface{}{
				"apiTimeout": "invalid",
			},
			wantErr: true,
		},
		{
			name:      "nil config",
			configMap: nil,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseCapacityConfig(tt.configMap)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseCapacityConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestCapacityMonitorConfig_ApplyDefaults tests default value application
func TestCapacityMonitorConfig_ApplyDefaults(t *testing.T) {
	// Set NODE_NAME environment variable for test
	t.Setenv("NODE_NAME", "test-node")

	config := &CapacityMonitorConfig{}
	err := config.applyDefaults()

	if err != nil {
		t.Fatalf("applyDefaults() unexpected error: %v", err)
	}

	// Check defaults
	if config.NodeName != "test-node" {
		t.Errorf("NodeName = %s, want test-node", config.NodeName)
	}
	if config.WarningThreshold != defaultCapacityWarningThreshold {
		t.Errorf("WarningThreshold = %.1f, want %.1f", config.WarningThreshold, defaultCapacityWarningThreshold)
	}
	if config.CriticalThreshold != defaultCapacityCriticalThreshold {
		t.Errorf("CriticalThreshold = %.1f, want %.1f", config.CriticalThreshold, defaultCapacityCriticalThreshold)
	}
	if config.FailureThreshold != defaultCapacityFailureThreshold {
		t.Errorf("FailureThreshold = %d, want %d", config.FailureThreshold, defaultCapacityFailureThreshold)
	}
	if config.APITimeout != defaultCapacityAPITimeout {
		t.Errorf("APITimeout = %v, want %v", config.APITimeout, defaultCapacityAPITimeout)
	}
	// Note: CheckAllocatable is now defaulted in parseCapacityConfig, not applyDefaults
}

// TestCapacityMonitorConfig_ValidateThresholds tests threshold validation
func TestCapacityMonitorConfig_ValidateThresholds(t *testing.T) {
	t.Setenv("NODE_NAME", "test-node")

	tests := []struct {
		name              string
		warningThreshold  float64
		criticalThreshold float64
		wantErr           bool
	}{
		{
			name:              "valid thresholds",
			warningThreshold:  90.0,
			criticalThreshold: 95.0,
			wantErr:           false,
		},
		{
			name:              "warning equals critical",
			warningThreshold:  90.0,
			criticalThreshold: 90.0,
			wantErr:           true,
		},
		{
			name:              "warning greater than critical",
			warningThreshold:  95.0,
			criticalThreshold: 90.0,
			wantErr:           true,
		},
		{
			name:              "negative warning",
			warningThreshold:  -10.0,
			criticalThreshold: 95.0,
			wantErr:           true,
		},
		{
			name:              "critical over 100",
			warningThreshold:  90.0,
			criticalThreshold: 110.0,
			wantErr:           true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &CapacityMonitorConfig{
				WarningThreshold:  tt.warningThreshold,
				CriticalThreshold: tt.criticalThreshold,
			}
			err := config.applyDefaults()
			if (err != nil) != tt.wantErr {
				t.Errorf("applyDefaults() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidateCapacityConfig tests the validator function
func TestValidateCapacityConfig(t *testing.T) {
	t.Setenv("NODE_NAME", "test-node")

	tests := []struct {
		name    string
		config  types.MonitorConfig
		wantErr bool
	}{
		{
			name: "valid configuration",
			config: types.MonitorConfig{
				Name:     "capacity-monitor",
				Type:     "kubernetes-capacity-check",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Config: map[string]interface{}{
					"warningThreshold":  90.0,
					"criticalThreshold": 95.0,
				},
			},
			wantErr: false,
		},
		{
			name: "missing name",
			config: types.MonitorConfig{
				Name: "",
				Type: "kubernetes-capacity-check",
			},
			wantErr: true,
		},
		{
			name: "invalid type",
			config: types.MonitorConfig{
				Name: "capacity-monitor",
				Type: "wrong-type",
			},
			wantErr: true,
		},
		{
			name: "invalid thresholds",
			config: types.MonitorConfig{
				Name: "capacity-monitor",
				Type: "kubernetes-capacity-check",
				Config: map[string]interface{}{
					"warningThreshold":  95.0,
					"criticalThreshold": 90.0, // Invalid: warning > critical
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCapacityConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCapacityConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestCapacityMonitorConfig_CheckAllocatableDefault tests that checkAllocatable defaults to true
func TestCapacityMonitorConfig_CheckAllocatableDefault(t *testing.T) {
	// Test that checkAllocatable defaults to true when not specified
	config, err := parseCapacityConfig(map[string]interface{}{
		"nodeName": "test-node",
	})
	if err != nil {
		t.Fatalf("parseCapacityConfig() error: %v", err)
	}

	if !config.CheckAllocatable {
		t.Error("CheckAllocatable should default to true when not specified")
	}

	// Test that explicitly setting to false is respected
	config, err = parseCapacityConfig(map[string]interface{}{
		"nodeName":         "test-node",
		"checkAllocatable": false,
	})
	if err != nil {
		t.Fatalf("parseCapacityConfig() error: %v", err)
	}

	if config.CheckAllocatable {
		t.Error("CheckAllocatable should be false when explicitly set to false")
	}

	// Test that explicitly setting to true is respected
	config, err = parseCapacityConfig(map[string]interface{}{
		"nodeName":         "test-node",
		"checkAllocatable": true,
	})
	if err != nil {
		t.Fatalf("parseCapacityConfig() error: %v", err)
	}

	if !config.CheckAllocatable {
		t.Error("CheckAllocatable should be true when explicitly set to true")
	}
}

// TestCapacityMonitor_CheckAllocatableVsCapacity tests the checkAllocatable flag
func TestCapacityMonitor_CheckAllocatableVsCapacity(t *testing.T) {
	tests := []struct {
		name                string
		checkAllocatable    bool
		capacity            int64
		allocatable         int64
		runningPods         int
		expectedUtilization float64
	}{
		{
			name:                "check allocatable",
			checkAllocatable:    true,
			capacity:            110,
			allocatable:         100,
			runningPods:         90,
			expectedUtilization: 90.0, // 90/100 = 90%
		},
		{
			name:                "check capacity",
			checkAllocatable:    false,
			capacity:            110,
			allocatable:         100,
			runningPods:         90,
			expectedUtilization: 81.8, // 90/110 = 81.8%
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockCapacityClient{
				node:    createMockNode(tt.capacity, tt.allocatable),
				nodeErr: nil,
				pods:    createMockPodList(tt.runningPods, corev1.PodRunning),
				podsErr: nil,
			}

			monitor := createTestCapacityMonitor(t, map[string]interface{}{
				"nodeName":          "test-node",
				"warningThreshold":  90.0,
				"criticalThreshold": 95.0,
				"checkAllocatable":  tt.checkAllocatable,
			}, mockClient)

			ctx := context.Background()
			_, err := monitor.checkCapacity(ctx)
			if err != nil {
				t.Fatalf("checkCapacity() unexpected error: %v", err)
			}

			// Verify utilization is calculated correctly
			monitor.mu.Lock()
			utilization := monitor.currentUtilization
			monitor.mu.Unlock()

			// Allow small floating point tolerance
			tolerance := 0.1
			if utilization < tt.expectedUtilization-tolerance || utilization > tt.expectedUtilization+tolerance {
				t.Errorf("Utilization = %.1f%%, want %.1f%%", utilization, tt.expectedUtilization)
			}
		})
	}
}
