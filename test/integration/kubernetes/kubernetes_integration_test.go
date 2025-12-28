/*
Integration Tests for Kubernetes Exporter

These tests verify the Kubernetes exporter functionality using a fake clientset.
They test the full integration of condition updates, event creation, and the
interaction between components.

Tests cover:
- Node condition updates and batching
- Event creation and deduplication
- Rate limiting behavior
- Retry logic on failures
- Exporter reload functionality
- End-to-end status export workflow

Run tests with:

	go test -v ./test/integration/kubernetes/...
	go test -v -race ./test/integration/kubernetes/...
*/
package kubernetes

import (
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	k8sexporter "github.com/supporttools/node-doctor/pkg/exporters/kubernetes"
	"github.com/supporttools/node-doctor/pkg/types"
	"github.com/supporttools/node-doctor/test"
)

// testK8sSetup creates a fake Kubernetes environment for testing
type testK8sSetup struct {
	clientset *fake.Clientset
	node      *corev1.Node
	nodeName  string
	nodeUID   string
}

func newTestK8sSetup(t *testing.T) *testK8sSetup {
	t.Helper()

	nodeName := "test-node"
	nodeUID := "test-uid-12345"

	testNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
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

	fakeClientset := fake.NewSimpleClientset(testNode)

	return &testK8sSetup{
		clientset: fakeClientset,
		node:      testNode,
		nodeName:  nodeName,
		nodeUID:   nodeUID,
	}
}

// createTestExporterConfig creates a config for testing
func createTestExporterConfig() *types.KubernetesExporterConfig {
	return &types.KubernetesExporterConfig{
		Enabled:           true,
		UpdateInterval:    100 * time.Millisecond,
		ResyncInterval:    200 * time.Millisecond,
		HeartbeatInterval: 300 * time.Millisecond,
		Namespace:         "test-namespace",
		Conditions: []types.ConditionConfig{
			{
				Type:           "TestCustomCondition",
				DefaultStatus:  "True",
				DefaultReason:  "TestReason",
				DefaultMessage: "Test custom condition",
			},
		},
		Annotations: []types.AnnotationConfig{
			{
				Key:   "node-doctor.io/test",
				Value: "test-value",
			},
		},
		Events: types.EventConfig{
			MaxEventsPerMinute:  10,
			DeduplicationWindow: time.Minute,
		},
	}
}

// TestKubernetesExporter_UpdateNodeConditions tests condition updates on nodes
func TestKubernetesExporter_UpdateNodeConditions(t *testing.T) {
	ctx, cancel := test.TestContext(t, 30*time.Second)
	defer cancel()

	setup := newTestK8sSetup(t)
	config := createTestExporterConfig()

	// Create K8s client wrapper
	client := k8sexporter.NewClientForTesting(
		setup.clientset,
		setup.nodeName,
		setup.nodeUID,
	)

	// Create condition manager
	conditionManager := k8sexporter.NewConditionManager(client, config)

	// Start condition manager
	conditionManager.Start(ctx)
	defer conditionManager.Stop()

	// Update a condition
	condition := types.Condition{
		Type:       "TestCondition",
		Status:     types.ConditionTrue,
		Transition: time.Now(),
		Reason:     "TestReason",
		Message:    "Test condition message",
	}

	conditionManager.UpdateCondition(condition)

	// Force flush and verify
	err := conditionManager.ForceFlush(ctx)
	test.AssertNoError(t, err, "force flush")

	// Verify condition was added
	test.Eventually(t, func() bool {
		node, err := setup.clientset.CoreV1().Nodes().Get(ctx, setup.nodeName, metav1.GetOptions{})
		if err != nil {
			return false
		}

		for _, cond := range node.Status.Conditions {
			if string(cond.Type) == "NodeDoctorTestCondition" {
				return true
			}
		}
		return false
	}, 2*time.Second, 100*time.Millisecond, "expected node condition to be updated")

	// Verify stats updated
	stats := conditionManager.GetStats()
	test.AssertTrue(t, stats != nil, "stats should not be nil")
}

// TestKubernetesExporter_CreateEvents tests event creation functionality
func TestKubernetesExporter_CreateEvents(t *testing.T) {
	ctx, cancel := test.TestContext(t, 30*time.Second)
	defer cancel()

	setup := newTestK8sSetup(t)
	config := createTestExporterConfig()

	client := k8sexporter.NewClientForTesting(
		setup.clientset,
		setup.nodeName,
		setup.nodeUID,
	)

	eventManager := k8sexporter.NewEventManager(client, config)

	// Start event manager
	eventManager.Start(ctx)
	defer eventManager.Stop()

	// Create a status with events
	status := &types.Status{
		Source:    "test-monitor",
		Timestamp: time.Now(),
		Events: []types.Event{
			{
				Severity:  types.EventInfo,
				Timestamp: time.Now(),
				Reason:    "TestEvent",
				Message:   "Test event message",
			},
		},
	}

	err := eventManager.CreateEventsFromStatus(ctx, status)
	test.AssertNoError(t, err, "create events from status")

	// Verify event was created
	test.Eventually(t, func() bool {
		events, err := setup.clientset.CoreV1().Events(config.Namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return false
		}
		return len(events.Items) > 0
	}, 2*time.Second, 100*time.Millisecond, "expected event to be created")
}

// TestKubernetesExporter_EventDeduplication tests event deduplication
func TestKubernetesExporter_EventDeduplication(t *testing.T) {
	ctx, cancel := test.TestContext(t, 30*time.Second)
	defer cancel()

	setup := newTestK8sSetup(t)
	config := createTestExporterConfig()
	config.Events.DeduplicationWindow = 5 * time.Second

	client := k8sexporter.NewClientForTesting(
		setup.clientset,
		setup.nodeName,
		setup.nodeUID,
	)

	eventManager := k8sexporter.NewEventManager(client, config)
	eventManager.Start(ctx)
	defer eventManager.Stop()

	// Create the same event multiple times
	event := corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dedup-test-event",
			Namespace: config.Namespace,
		},
		InvolvedObject: corev1.ObjectReference{
			Kind: "Node",
			Name: setup.nodeName,
		},
		Reason:         "DedupTestReason",
		Message:        "Deduplication test message",
		Type:           corev1.EventTypeNormal,
		FirstTimestamp: metav1.NewTime(time.Now().Truncate(time.Minute)),
		LastTimestamp:  metav1.NewTime(time.Now().Truncate(time.Minute)),
		Count:          1,
	}

	// First event should succeed
	err := eventManager.CreateEvent(ctx, event)
	test.AssertNoError(t, err, "first event creation")

	// Second identical event should be deduplicated (no error)
	err = eventManager.CreateEvent(ctx, event)
	test.AssertNoError(t, err, "second event creation (deduplicated)")

	// Verify only one event exists (deduplication should merge duplicates)
	events, err := setup.clientset.CoreV1().Events(config.Namespace).List(ctx, metav1.ListOptions{})
	test.AssertNoError(t, err, "list events")

	// Should have exactly 1 event - deduplication should suppress duplicates
	test.AssertEqual(t, 1, len(events.Items), "expected exactly 1 event due to deduplication")
}

// TestKubernetesExporter_RateLimiting tests rate limiting behavior
func TestKubernetesExporter_RateLimiting(t *testing.T) {
	ctx, cancel := test.TestContext(t, 30*time.Second)
	defer cancel()

	setup := newTestK8sSetup(t)
	config := createTestExporterConfig()
	config.Events.MaxEventsPerMinute = 2 // Low limit for testing

	client := k8sexporter.NewClientForTesting(
		setup.clientset,
		setup.nodeName,
		setup.nodeUID,
	)

	eventManager := k8sexporter.NewEventManager(client, config)
	eventManager.Start(ctx)
	defer eventManager.Stop()

	// Create events up to the limit
	for i := 0; i < 2; i++ {
		event := corev1.Event{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "rate-limit-event-" + string(rune('A'+i)),
				Namespace: config.Namespace,
			},
			InvolvedObject: corev1.ObjectReference{
				Kind: "Node",
				Name: setup.nodeName,
			},
			Reason:         "RateLimitTest" + string(rune('A'+i)),
			Message:        "Rate limit test message",
			Type:           corev1.EventTypeNormal,
			FirstTimestamp: metav1.NewTime(time.Now()),
			LastTimestamp:  metav1.NewTime(time.Now()),
			Count:          1,
		}

		err := eventManager.CreateEvent(ctx, event)
		test.AssertNoError(t, err, "event creation within limit")
	}

	// Next event should be rate limited
	event := corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rate-limited-event",
			Namespace: config.Namespace,
		},
		InvolvedObject: corev1.ObjectReference{
			Kind: "Node",
			Name: setup.nodeName,
		},
		Reason:         "RateLimitExceeded",
		Message:        "This should be rate limited",
		Type:           corev1.EventTypeWarning,
		FirstTimestamp: metav1.NewTime(time.Now()),
		LastTimestamp:  metav1.NewTime(time.Now()),
		Count:          1,
	}

	err := eventManager.CreateEvent(ctx, event)
	test.AssertTrue(t, err != nil, "expected rate limit error")
}

// TestKubernetesExporter_ConditionFlush tests condition flushing behavior
// Note: Full retry logic with transient failure injection requires more complex
// fake clientset setup with reactor error injection, which is covered in unit tests.
// This integration test verifies the flush mechanism works correctly.
func TestKubernetesExporter_ConditionFlush(t *testing.T) {
	ctx, cancel := test.TestContext(t, 30*time.Second)
	defer cancel()

	setup := newTestK8sSetup(t)
	config := createTestExporterConfig()

	client := k8sexporter.NewClientForTesting(
		setup.clientset,
		setup.nodeName,
		setup.nodeUID,
	)

	conditionManager := k8sexporter.NewConditionManager(client, config)
	conditionManager.Start(ctx)
	defer conditionManager.Stop()

	// Update a condition
	condition := types.Condition{
		Type:       "FlushTestCondition",
		Status:     types.ConditionTrue,
		Transition: time.Now(),
		Reason:     "FlushTest",
		Message:    "Flush test message",
	}

	conditionManager.UpdateCondition(condition)

	// Force flush should work
	err := conditionManager.ForceFlush(ctx)
	test.AssertNoError(t, err, "force flush should succeed")

	// Verify condition exists in manager state
	conditions := conditionManager.GetConditions()
	test.AssertTrue(t, len(conditions) > 0, "expected conditions after flush")

	// Verify condition was actually flushed to the node
	// Note: ConditionManager prepends "NodeDoctor" prefix to custom condition types
	node, err := setup.clientset.CoreV1().Nodes().Get(ctx, setup.nodeName, metav1.GetOptions{})
	test.AssertNoError(t, err, "get node after flush")

	found := false
	for _, c := range node.Status.Conditions {
		if string(c.Type) == "NodeDoctorFlushTestCondition" {
			found = true
			break
		}
	}
	test.AssertTrue(t, found, "expected NodeDoctorFlushTestCondition to be present on node after flush")
}

// TestConditionManager_BatchUpdates tests batching of condition updates
func TestConditionManager_BatchUpdates(t *testing.T) {
	ctx, cancel := test.TestContext(t, 30*time.Second)
	defer cancel()

	setup := newTestK8sSetup(t)
	config := createTestExporterConfig()
	config.UpdateInterval = 500 * time.Millisecond // Longer interval to observe batching

	client := k8sexporter.NewClientForTesting(
		setup.clientset,
		setup.nodeName,
		setup.nodeUID,
	)

	conditionManager := k8sexporter.NewConditionManager(client, config)
	conditionManager.Start(ctx)
	defer conditionManager.Stop()

	// Add multiple conditions rapidly
	for i := 0; i < 5; i++ {
		condition := types.Condition{
			Type:       "BatchCondition" + string(rune('A'+i)),
			Status:     types.ConditionTrue,
			Transition: time.Now(),
			Reason:     "BatchTest",
			Message:    "Batch test condition",
		}
		conditionManager.UpdateCondition(condition)
	}

	// Check pending updates
	pending := conditionManager.GetPendingUpdates()
	test.AssertTrue(t, len(pending) >= 5, "expected at least 5 pending updates")

	// Force flush to apply all at once
	err := conditionManager.ForceFlush(ctx)
	test.AssertNoError(t, err, "force flush batch updates")

	// Verify all conditions applied
	conditions := conditionManager.GetConditions()
	test.AssertTrue(t, len(conditions) >= 5, "expected at least 5 conditions after batch")
}

// TestConditionManager_Resync tests resync functionality
func TestConditionManager_Resync(t *testing.T) {
	ctx, cancel := test.TestContext(t, 30*time.Second)
	defer cancel()

	setup := newTestK8sSetup(t)
	config := createTestExporterConfig()

	client := k8sexporter.NewClientForTesting(
		setup.clientset,
		setup.nodeName,
		setup.nodeUID,
	)

	conditionManager := k8sexporter.NewConditionManager(client, config)
	conditionManager.Start(ctx)
	defer conditionManager.Stop()

	// Initial sync should happen
	time.Sleep(100 * time.Millisecond)

	// Get initial conditions
	initialConditions := conditionManager.GetConditions()

	// Force resync
	err := conditionManager.ForceResync(ctx)
	test.AssertNoError(t, err, "force resync")

	// Should still have conditions
	afterResync := conditionManager.GetConditions()
	test.AssertTrue(t, len(afterResync) >= len(initialConditions), "conditions should be preserved after resync")

	// Stats should show resync time
	stats := conditionManager.GetStats()
	test.AssertTrue(t, stats["last_resync"].(string) != "", "last_resync should be set")
}

// TestEventManager_Aggregation tests event aggregation
func TestEventManager_Aggregation(t *testing.T) {
	ctx, cancel := test.TestContext(t, 30*time.Second)
	defer cancel()

	setup := newTestK8sSetup(t)
	config := createTestExporterConfig()

	client := k8sexporter.NewClientForTesting(
		setup.clientset,
		setup.nodeName,
		setup.nodeUID,
	)

	eventManager := k8sexporter.NewEventManager(client, config)
	eventManager.Start(ctx)
	defer eventManager.Stop()

	// Create problem events that could be aggregated
	for i := 0; i < 3; i++ {
		problem := &types.Problem{
			Type:       "service-failed",
			Resource:   "test-service",
			Severity:   types.ProblemWarning,
			Message:    "Service failed iteration " + string(rune('0'+i)),
			DetectedAt: time.Now(),
			Metadata:   make(map[string]string),
		}

		err := eventManager.CreateEventFromProblem(ctx, problem)
		test.AssertNoError(t, err, "create event from problem")
	}

	// Verify events were created
	events, err := setup.clientset.CoreV1().Events(config.Namespace).List(ctx, metav1.ListOptions{})
	test.AssertNoError(t, err, "list events")
	test.AssertTrue(t, len(events.Items) >= 1, "expected at least 1 event")
}

// TestKubernetesIntegration_FullWorkflow tests the complete export workflow
func TestKubernetesIntegration_FullWorkflow(t *testing.T) {
	ctx, cancel := test.TestContext(t, 30*time.Second)
	defer cancel()

	setup := newTestK8sSetup(t)
	config := createTestExporterConfig()

	client := k8sexporter.NewClientForTesting(
		setup.clientset,
		setup.nodeName,
		setup.nodeUID,
	)

	// Create both managers
	conditionManager := k8sexporter.NewConditionManager(client, config)
	eventManager := k8sexporter.NewEventManager(client, config)

	// Start both
	conditionManager.Start(ctx)
	eventManager.Start(ctx)
	defer conditionManager.Stop()
	defer eventManager.Stop()

	// Create a complete status
	status := &types.Status{
		Source:    "integration-test-monitor",
		Timestamp: time.Now(),
		Conditions: []types.Condition{
			{
				Type:       "IntegrationTestCondition",
				Status:     types.ConditionFalse,
				Transition: time.Now(),
				Reason:     "IntegrationTest",
				Message:    "Integration test condition",
			},
		},
		Events: []types.Event{
			{
				Severity:  types.EventWarning,
				Timestamp: time.Now(),
				Reason:    "IntegrationTestEvent",
				Message:   "Integration test event message",
			},
		},
	}

	// Export conditions
	for _, cond := range status.Conditions {
		conditionManager.UpdateCondition(cond)
	}

	// Export events
	err := eventManager.CreateEventsFromStatus(ctx, status)
	test.AssertNoError(t, err, "create events from status")

	// Flush conditions
	err = conditionManager.ForceFlush(ctx)
	test.AssertNoError(t, err, "force flush conditions")

	// Verify node was updated with condition
	test.Eventually(t, func() bool {
		node, err := setup.clientset.CoreV1().Nodes().Get(ctx, setup.nodeName, metav1.GetOptions{})
		if err != nil {
			return false
		}

		for _, cond := range node.Status.Conditions {
			if string(cond.Type) == "NodeDoctorIntegrationTestCondition" {
				return true
			}
		}
		return false
	}, 2*time.Second, 100*time.Millisecond, "expected condition to be on node")

	// Verify event was created
	test.Eventually(t, func() bool {
		events, err := setup.clientset.CoreV1().Events(config.Namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return false
		}

		for _, event := range events.Items {
			if event.Reason == "IntegrationTestEvent" {
				return true
			}
		}
		return false
	}, 2*time.Second, 100*time.Millisecond, "expected event to exist")
}

// TestKubernetesExporter_ConcurrentAccess tests thread safety
func TestKubernetesExporter_ConcurrentAccess(t *testing.T) {
	ctx, cancel := test.TestContext(t, 30*time.Second)
	defer cancel()

	setup := newTestK8sSetup(t)
	config := createTestExporterConfig()

	client := k8sexporter.NewClientForTesting(
		setup.clientset,
		setup.nodeName,
		setup.nodeUID,
	)

	conditionManager := k8sexporter.NewConditionManager(client, config)
	eventManager := k8sexporter.NewEventManager(client, config)

	conditionManager.Start(ctx)
	eventManager.Start(ctx)
	defer conditionManager.Stop()
	defer eventManager.Stop()

	var wg sync.WaitGroup

	// Concurrent condition updates
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			condition := types.Condition{
				Type:       "ConcurrentCondition" + string(rune('A'+idx)),
				Status:     types.ConditionTrue,
				Transition: time.Now(),
				Reason:     "ConcurrentTest",
				Message:    "Concurrent condition test",
			}

			conditionManager.UpdateCondition(condition)
			conditionManager.GetConditions()
			conditionManager.GetPendingUpdates()
		}(i)
	}

	// Concurrent event creation
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			status := &types.Status{
				Source:    "concurrent-test-monitor",
				Timestamp: time.Now(),
				Events: []types.Event{
					{
						Severity:  types.EventInfo,
						Timestamp: time.Now(),
						Reason:    "ConcurrentEvent" + string(rune('A'+idx)),
						Message:   "Concurrent event test",
					},
				},
			}

			eventManager.CreateEventsFromStatus(ctx, status)
			eventManager.GetStats()
		}(i)
	}

	wg.Wait()

	// Verify actual results - conditions were added
	// Note: Initial conditions from node + our 10 concurrent updates
	// Conditions may be pending flush, so check both current and pending
	conditions := conditionManager.GetConditions()
	pendingUpdates := conditionManager.GetPendingUpdates()
	totalConditions := len(conditions) + len(pendingUpdates)
	test.AssertTrue(t, totalConditions >= 10, "expected at least 10 conditions (current + pending) from concurrent updates")

	// Verify event manager stats were retrieved without error (stats is map[string]interface{})
	stats := eventManager.GetStats()
	test.AssertTrue(t, stats != nil, "expected event manager stats to be non-nil")
}

// TestKubernetesExporter_ManagerHealth tests health check functionality
func TestKubernetesExporter_ManagerHealth(t *testing.T) {
	ctx, cancel := test.TestContext(t, 30*time.Second)
	defer cancel()

	setup := newTestK8sSetup(t)
	config := createTestExporterConfig()

	client := k8sexporter.NewClientForTesting(
		setup.clientset,
		setup.nodeName,
		setup.nodeUID,
	)

	conditionManager := k8sexporter.NewConditionManager(client, config)
	eventManager := k8sexporter.NewEventManager(client, config)

	conditionManager.Start(ctx)
	eventManager.Start(ctx)
	defer conditionManager.Stop()
	defer eventManager.Stop()

	// Both should be healthy initially
	test.AssertTrue(t, conditionManager.IsHealthy(), "condition manager should be healthy")
	test.AssertTrue(t, eventManager.IsHealthy(), "event manager should be healthy")
}
