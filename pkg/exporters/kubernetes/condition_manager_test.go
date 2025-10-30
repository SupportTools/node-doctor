package kubernetes

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/supporttools/node-doctor/pkg/types"
)

// Helper function to create a test condition manager
func createTestConditionManager(updateInterval, resyncInterval, heartbeatInterval time.Duration) *ConditionManager {
	fakeClientset := fake.NewSimpleClientset()

	// Create test node with some existing conditions
	testNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
			UID:  "test-uid-12345",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:               corev1.NodeReady,
					Status:             corev1.ConditionTrue,
					LastTransitionTime: metav1.NewTime(time.Now()),
					Reason:             "KubeletReady",
					Message:            "kubelet is ready",
				},
				{
					Type:               corev1.NodeMemoryPressure,
					Status:             corev1.ConditionFalse,
					LastTransitionTime: metav1.NewTime(time.Now()),
					Reason:             "KubeletHasSufficientMemory",
					Message:            "kubelet has sufficient memory available",
				},
			},
		},
	}

	fakeClientset.CoreV1().Nodes().Create(context.Background(), testNode, metav1.CreateOptions{})

	client := &K8sClient{
		clientset: fakeClientset,
		nodeName:  "test-node",
		nodeUID:   "test-uid-12345",
	}

	config := &types.KubernetesExporterConfig{
		UpdateInterval:    updateInterval,
		ResyncInterval:    resyncInterval,
		HeartbeatInterval: heartbeatInterval,
		Conditions: []types.ConditionConfig{
			{
				Type:           "TestCustomCondition",
				DefaultStatus:  "True",
				DefaultReason:  "TestReason",
				DefaultMessage: "Test custom condition",
			},
		},
	}

	return NewConditionManager(client, config)
}

// TestNewConditionManager tests condition manager creation
func TestNewConditionManager(t *testing.T) {
	config := &types.KubernetesExporterConfig{
		UpdateInterval:    5 * time.Second,
		ResyncInterval:    30 * time.Second,
		HeartbeatInterval: 2 * time.Minute,
	}

	fakeClientset := fake.NewSimpleClientset()
	client := &K8sClient{
		clientset: fakeClientset,
		nodeName:  "test-node",
		nodeUID:   "test-uid",
	}

	manager := NewConditionManager(client, config)

	if manager == nil {
		t.Error("NewConditionManager() returned nil")
	}
	if manager.updateInterval != 5*time.Second {
		t.Errorf("Expected updateInterval = 5s, got %v", manager.updateInterval)
	}
	if manager.resyncInterval != 30*time.Second {
		t.Errorf("Expected resyncInterval = 30s, got %v", manager.resyncInterval)
	}
	if manager.heartbeatInterval != 2*time.Minute {
		t.Errorf("Expected heartbeatInterval = 2m, got %v", manager.heartbeatInterval)
	}
}

// TestConditionManagerLifecycle tests start and stop functionality
func TestConditionManagerLifecycle(t *testing.T) {
	manager := createTestConditionManager(100*time.Millisecond, 200*time.Millisecond, 300*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Test start
	manager.Start(ctx)

	// Let it run briefly
	time.Sleep(150 * time.Millisecond)

	// Test stop
	manager.Stop()

	// Should be able to start and stop multiple times
	manager.Start(ctx)
	manager.Stop()
}

// TestInitialSync tests the initial synchronization with Kubernetes
func TestInitialSync(t *testing.T) {
	manager := createTestConditionManager(time.Second, time.Minute, time.Minute)
	ctx := context.Background()

	// Perform initial sync
	err := manager.initialSync(ctx)
	if err != nil {
		t.Errorf("initialSync() error = %v", err)
	}

	// Check that existing conditions were loaded
	conditions := manager.GetConditions()
	if len(conditions) < 2 {
		t.Errorf("Expected at least 2 conditions after initial sync, got %d", len(conditions))
	}

	// Check that Ready condition exists
	if _, exists := conditions["Ready"]; !exists {
		t.Error("Ready condition should exist after initial sync")
	}

	// Check that custom condition is pending
	pendingUpdates := manager.GetPendingUpdates()
	if _, exists := pendingUpdates["TestCustomCondition"]; !exists {
		t.Error("Custom condition should be pending after initial sync")
	}
}

// TestUpdateCondition tests condition updates
func TestUpdateCondition(t *testing.T) {
	manager := createTestConditionManager(time.Second, time.Minute, time.Minute)
	ctx := context.Background()

	// Initialize
	manager.initialSync(ctx)

	// Test updating a condition
	condition := types.Condition{
		Type:       "TestCondition",
		Status:     types.ConditionTrue,
		Transition: time.Now(),
		Reason:     "TestReason",
		Message:    "Test condition message",
	}

	manager.UpdateCondition(condition)

	// Check that the condition is pending
	pendingUpdates := manager.GetPendingUpdates()
	if _, exists := pendingUpdates["NodeDoctorTestCondition"]; !exists {
		t.Error("Updated condition should be pending")
	}

	// Test updating the same condition again (should not create duplicate)
	manager.UpdateCondition(condition)
	if len(pendingUpdates) != len(manager.GetPendingUpdates()) {
		t.Error("Updating the same condition should not create duplicate pending updates")
	}
}

// TestUpdateConditionFromProblem tests updating conditions from problems
func TestUpdateConditionFromProblem(t *testing.T) {
	manager := createTestConditionManager(time.Second, time.Minute, time.Minute)
	ctx := context.Background()

	// Initialize
	manager.initialSync(ctx)

	problem := &types.Problem{
		Type:       "service-failed",
		Resource:   "kubelet.service",
		Severity:   types.ProblemCritical,
		Message:    "Service has failed",
		DetectedAt: time.Now(),
		Metadata:   make(map[string]string),
	}

	manager.UpdateConditionFromProblem(problem)

	// Check that the condition is pending
	pendingUpdates := manager.GetPendingUpdates()
	expectedType := "NodeDoctorServiceFailed"
	if _, exists := pendingUpdates[expectedType]; !exists {
		t.Errorf("Problem-based condition %s should be pending", expectedType)
	}

	// Verify the condition details
	condition := pendingUpdates[expectedType]
	if condition.Status != corev1.ConditionFalse {
		t.Errorf("Expected condition status = False, got %s", condition.Status)
	}
	if condition.Reason != "CriticalProblem" {
		t.Errorf("Expected condition reason = CriticalProblem, got %s", condition.Reason)
	}
}

// TestAddCustomConditions tests adding custom conditions from configuration
func TestAddCustomConditions(t *testing.T) {
	manager := createTestConditionManager(time.Second, time.Minute, time.Minute)

	// Clear any existing pending updates
	manager.pendingUpdates = make(map[string]corev1.NodeCondition)

	manager.AddCustomConditions()

	// Check that custom condition is pending
	pendingUpdates := manager.GetPendingUpdates()
	if _, exists := pendingUpdates["TestCustomCondition"]; !exists {
		t.Error("Custom condition should be pending after AddCustomConditions()")
	}

	// Verify the condition details
	condition := pendingUpdates["TestCustomCondition"]
	if condition.Status != corev1.ConditionTrue {
		t.Errorf("Expected custom condition status = True, got %s", condition.Status)
	}
	if condition.Reason != "TestReason" {
		t.Errorf("Expected custom condition reason = TestReason, got %s", condition.Reason)
	}
	if condition.Message != "Test custom condition" {
		t.Errorf("Expected custom condition message = 'Test custom condition', got %s", condition.Message)
	}
}

// TestFlushPendingUpdates tests flushing pending updates to Kubernetes
func TestFlushPendingUpdates(t *testing.T) {
	manager := createTestConditionManager(time.Second, time.Minute, time.Minute)
	ctx := context.Background()

	// Initialize
	manager.initialSync(ctx)

	// Add a condition update
	condition := types.Condition{
		Type:       "FlushTest",
		Status:     types.ConditionTrue,
		Transition: time.Now(),
		Reason:     "FlushTestReason",
		Message:    "Flush test message",
	}

	manager.UpdateCondition(condition)

	// Should have pending updates
	pendingUpdates := manager.GetPendingUpdates()
	if len(pendingUpdates) == 0 {
		t.Error("Should have pending updates before flush")
	}

	// Flush updates
	err := manager.flushPendingUpdates(ctx)
	if err != nil {
		t.Errorf("flushPendingUpdates() error = %v", err)
	}

	// Should have no pending updates after flush
	pendingUpdates = manager.GetPendingUpdates()
	if len(pendingUpdates) != 0 {
		t.Errorf("Should have no pending updates after flush, got %d", len(pendingUpdates))
	}

	// The condition should now be in the current conditions
	conditions := manager.GetConditions()
	if _, exists := conditions["NodeDoctorFlushTest"]; !exists {
		t.Error("Flushed condition should be in current conditions")
	}
}

// TestPerformResync tests resynchronization with Kubernetes
func TestPerformResync(t *testing.T) {
	manager := createTestConditionManager(time.Second, time.Minute, time.Minute)
	ctx := context.Background()

	// Initialize
	manager.initialSync(ctx)

	// Get initial condition count
	initialConditions := manager.GetConditions()
	initialCount := len(initialConditions)

	// Perform resync
	err := manager.performResync(ctx)
	if err != nil {
		t.Errorf("performResync() error = %v", err)
	}

	// Should still have the same conditions (or more)
	conditions := manager.GetConditions()
	if len(conditions) < initialCount {
		t.Errorf("Expected at least %d conditions after resync, got %d", initialCount, len(conditions))
	}

	// Last resync time should be updated
	stats := manager.GetStats()
	lastResync := stats["last_resync"].(string)
	if lastResync == "" {
		t.Error("Last resync time should be set after resync")
	}
}

// TestSendHeartbeat tests heartbeat functionality
func TestSendHeartbeat(t *testing.T) {
	manager := createTestConditionManager(time.Second, time.Minute, time.Minute)

	// Clear any existing pending updates
	manager.pendingUpdates = make(map[string]corev1.NodeCondition)

	manager.sendHeartbeat()

	// Check that heartbeat condition is pending
	pendingUpdates := manager.GetPendingUpdates()
	if _, exists := pendingUpdates["NodeDoctorHealthy"]; !exists {
		t.Error("Heartbeat condition should be pending after sendHeartbeat()")
	}

	// Verify the heartbeat condition
	condition := pendingUpdates["NodeDoctorHealthy"]
	if condition.Status != corev1.ConditionTrue {
		t.Errorf("Expected heartbeat condition status = True, got %s", condition.Status)
	}
	if condition.Reason != "Heartbeat" {
		t.Errorf("Expected heartbeat condition reason = Heartbeat, got %s", condition.Reason)
	}
}

// TestConditionManagerStats tests statistics collection
func TestConditionManagerStats(t *testing.T) {
	manager := createTestConditionManager(time.Second, time.Minute, time.Minute)
	ctx := context.Background()

	// Initialize to populate some stats
	manager.initialSync(ctx)

	stats := manager.GetStats()
	if stats == nil {
		t.Error("GetStats() returned nil")
	}

	// Check expected fields
	expectedFields := []string{
		"current_conditions",
		"pending_updates",
		"last_update",
		"last_resync",
		"last_heartbeat",
		"update_interval",
		"resync_interval",
		"heartbeat_interval",
	}

	for _, field := range expectedFields {
		if _, exists := stats[field]; !exists {
			t.Errorf("GetStats() missing field: %s", field)
		}
	}

	// Verify some values
	if stats["update_interval"] != time.Second.String() {
		t.Errorf("Expected update_interval = %s, got %v", time.Second.String(), stats["update_interval"])
	}
}

// TestConditionManagerHealthCheck tests health check functionality
func TestConditionManagerHealthCheck(t *testing.T) {
	manager := createTestConditionManager(time.Second, time.Minute, time.Minute)
	ctx := context.Background()

	// Initialize
	manager.initialSync(ctx)

	// Should be healthy initially
	if !manager.IsHealthy() {
		t.Error("ConditionManager should be healthy initially")
	}

	// Add too many pending updates
	for i := 0; i < 51; i++ {
		condition := corev1.NodeCondition{
			Type:               corev1.NodeConditionType("TooManyConditions" + string(rune('0'+i))),
			Status:             corev1.ConditionTrue,
			LastTransitionTime: metav1.NewTime(time.Now()),
			Reason:             "Test",
			Message:            "Test message",
		}
		manager.pendingUpdates[string(condition.Type)] = condition
	}

	// Should not be healthy with too many pending updates
	if manager.IsHealthy() {
		t.Error("ConditionManager should not be healthy with too many pending updates")
	}
}

// TestConditionsEqual tests the condition equality comparison
func TestConditionsEqual(t *testing.T) {
	now := time.Now()
	later := now.Add(time.Minute)

	condition1 := corev1.NodeCondition{
		Type:               "TestCondition",
		Status:             corev1.ConditionTrue,
		LastTransitionTime: metav1.NewTime(now),
		Reason:             "TestReason",
		Message:            "Test message",
	}

	condition2 := corev1.NodeCondition{
		Type:               "TestCondition",
		Status:             corev1.ConditionTrue,
		LastTransitionTime: metav1.NewTime(later), // Different time
		Reason:             "TestReason",
		Message:            "Test message",
	}

	condition3 := corev1.NodeCondition{
		Type:               "TestCondition",
		Status:             corev1.ConditionFalse, // Different status
		LastTransitionTime: metav1.NewTime(now),
		Reason:             "TestReason",
		Message:            "Test message",
	}

	// Should be equal (ignoring LastTransitionTime)
	if !conditionsEqual(condition1, condition2) {
		t.Error("Conditions should be equal (ignoring LastTransitionTime)")
	}

	// Should not be equal (different status)
	if conditionsEqual(condition1, condition3) {
		t.Error("Conditions should not be equal (different status)")
	}
}

// TestForceFlushAndResync tests the force methods
func TestForceFlushAndResync(t *testing.T) {
	manager := createTestConditionManager(time.Second, time.Minute, time.Minute)
	ctx := context.Background()

	// Initialize
	manager.initialSync(ctx)

	// Add a condition update
	condition := types.Condition{
		Type:       "ForceTest",
		Status:     types.ConditionTrue,
		Transition: time.Now(),
		Reason:     "ForceTestReason",
		Message:    "Force test message",
	}

	manager.UpdateCondition(condition)

	// Test ForceFlush
	err := manager.ForceFlush(ctx)
	if err != nil {
		t.Errorf("ForceFlush() error = %v", err)
	}

	// Should have no pending updates after flush
	pendingUpdates := manager.GetPendingUpdates()
	if len(pendingUpdates) != 0 {
		t.Errorf("Should have no pending updates after ForceFlush, got %d", len(pendingUpdates))
	}

	// Test ForceResync
	err = manager.ForceResync(ctx)
	if err != nil {
		t.Errorf("ForceResync() error = %v", err)
	}
}

// TestConditionManagerWithFailures tests behavior with simulated failures
func TestConditionManagerWithFailures(t *testing.T) {
	manager := createTestConditionManager(time.Second, time.Minute, time.Minute)
	ctx := context.Background()

	// Test initial sync with nonexistent node (should fail gracefully)
	manager.client.nodeName = "nonexistent-node"

	err := manager.initialSync(ctx)
	if err == nil {
		t.Error("initialSync() should have failed with nonexistent node")
	}

	// Test perform resync with nonexistent node
	err = manager.performResync(ctx)
	if err == nil {
		t.Error("performResync() should have failed with nonexistent node")
	}

	// Test flush with nonexistent node (should fail gracefully)
	condition := types.Condition{
		Type:       "FailureTest",
		Status:     types.ConditionTrue,
		Transition: time.Now(),
		Reason:     "FailureTestReason",
		Message:    "Failure test message",
	}

	manager.UpdateCondition(condition)

	err = manager.flushPendingUpdates(ctx)
	if err == nil {
		t.Error("flushPendingUpdates() should have failed with nonexistent node")
	}
}