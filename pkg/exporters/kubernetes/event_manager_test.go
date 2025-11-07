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

// Helper function to create a test event manager
func createTestEventManager(maxEventsPerMin int, deduplicationWindow time.Duration) *EventManager {
	fakeClientset := fake.NewSimpleClientset()

	// Create test node
	testNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
			UID:  "test-uid-12345",
		},
	}
	fakeClientset.CoreV1().Nodes().Create(context.Background(), testNode, metav1.CreateOptions{})

	client := &K8sClient{
		clientset: fakeClientset,
		nodeName:  "test-node",
		nodeUID:   "test-uid-12345",
	}

	config := &types.KubernetesExporterConfig{
		Namespace: "test-namespace",
		Events: types.EventConfig{
			MaxEventsPerMinute:  maxEventsPerMin,
			DeduplicationWindow: deduplicationWindow,
		},
	}

	return NewEventManager(client, config)
}

// TestNewEventManager tests event manager creation
func TestNewEventManager(t *testing.T) {
	config := &types.KubernetesExporterConfig{
		Events: types.EventConfig{
			MaxEventsPerMinute:  5,
			DeduplicationWindow: 2 * time.Minute,
		},
	}

	fakeClientset := fake.NewSimpleClientset()
	client := &K8sClient{
		clientset: fakeClientset,
		nodeName:  "test-node",
		nodeUID:   "test-uid",
	}

	manager := NewEventManager(client, config)

	if manager == nil {
		t.Error("NewEventManager() returned nil")
	}
	if manager.maxEventsPerMin != 5 {
		t.Errorf("Expected maxEventsPerMin = 5, got %d", manager.maxEventsPerMin)
	}
	if manager.deduplicationTTL != 2*time.Minute {
		t.Errorf("Expected deduplicationTTL = 2m, got %v", manager.deduplicationTTL)
	}
}

// TestEventManagerLifecycle tests start and stop functionality
func TestEventManagerLifecycle(t *testing.T) {
	manager := createTestEventManager(10, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Test start
	manager.Start(ctx)

	// Test stop
	manager.Stop()

	// Should be able to start and stop multiple times
	manager.Start(ctx)
	manager.Stop()
}

// TestCreateEvent tests basic event creation
func TestCreateEvent(t *testing.T) {
	manager := createTestEventManager(10, time.Minute)
	ctx := context.Background()

	event := corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-event",
			Namespace: "test-namespace",
		},
		InvolvedObject: corev1.ObjectReference{
			Kind: "Node",
			Name: "test-node",
		},
		Reason:         "TestReason",
		Message:        "Test message",
		Type:           corev1.EventTypeNormal,
		FirstTimestamp: metav1.NewTime(time.Now()),
		LastTimestamp:  metav1.NewTime(time.Now()),
		Count:          1,
	}

	err := manager.CreateEvent(ctx, event)
	if err != nil {
		t.Errorf("CreateEvent() error = %v", err)
	}
}

// TestEventDeduplication tests event deduplication functionality
func TestEventDeduplication(t *testing.T) {
	manager := createTestEventManager(10, time.Minute)
	ctx := context.Background()

	// Create the same event twice
	event := corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "duplicate-event",
			Namespace: "test-namespace",
		},
		InvolvedObject: corev1.ObjectReference{
			Kind: "Node",
			Name: "test-node",
		},
		Reason:         "DuplicateTest",
		Message:        "Duplicate message",
		Type:           corev1.EventTypeNormal,
		FirstTimestamp: metav1.NewTime(time.Now().Truncate(time.Minute)),
		LastTimestamp:  metav1.NewTime(time.Now().Truncate(time.Minute)),
		Count:          1,
	}

	// First event should succeed
	err := manager.CreateEvent(ctx, event)
	if err != nil {
		t.Errorf("First CreateEvent() error = %v", err)
	}

	// Second identical event should be deduplicated (no error, just skipped)
	err = manager.CreateEvent(ctx, event)
	if err != nil {
		t.Errorf("Second CreateEvent() error = %v", err)
	}

	// Verify deduplication worked by checking signature exists
	signature := CreateEventSignature(event)
	if !manager.isDuplicate(signature) {
		t.Error("Event should have been marked as duplicate")
	}
}

// TestEventRateLimiting tests rate limiting functionality
func TestEventRateLimiting(t *testing.T) {
	manager := createTestEventManager(2, time.Minute) // Limit to 2 events per minute
	ctx := context.Background()

	// Create events up to the limit
	for i := 0; i < 2; i++ {
		event := corev1.Event{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "rate-limit-event-" + string(rune('1'+i)),
				Namespace: "test-namespace",
			},
			InvolvedObject: corev1.ObjectReference{
				Kind: "Node",
				Name: "test-node",
			},
			Reason:         "RateLimitTest",
			Message:        "Rate limit message",
			Type:           corev1.EventTypeNormal,
			FirstTimestamp: metav1.NewTime(time.Now()),
			LastTimestamp:  metav1.NewTime(time.Now()),
			Count:          1,
		}

		err := manager.CreateEvent(ctx, event)
		if err != nil {
			t.Errorf("CreateEvent() %d error = %v", i, err)
		}
	}

	// This event should be rate limited
	event := corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rate-limited-event",
			Namespace: "test-namespace",
		},
		InvolvedObject: corev1.ObjectReference{
			Kind: "Node",
			Name: "test-node",
		},
		Reason:         "RateLimitExceeded",
		Message:        "This should be rate limited",
		Type:           corev1.EventTypeWarning,
		FirstTimestamp: metav1.NewTime(time.Now()),
		LastTimestamp:  metav1.NewTime(time.Now()),
		Count:          1,
	}

	err := manager.CreateEvent(ctx, event)
	if err == nil {
		t.Error("CreateEvent() should have been rate limited")
	}
}

// TestCreateEventsFromStatus tests creating events from a status
func TestCreateEventsFromStatus(t *testing.T) {
	manager := createTestEventManager(10, time.Minute)
	ctx := context.Background()

	status := &types.Status{
		Source:    "test-monitor",
		Timestamp: time.Now(),
		Events: []types.Event{
			{
				Severity:  types.EventInfo,
				Timestamp: time.Now(),
				Reason:    "TestEvent1",
				Message:   "Test event 1",
			},
			{
				Severity:  types.EventWarning,
				Timestamp: time.Now(),
				Reason:    "TestEvent2",
				Message:   "Test event 2",
			},
		},
	}

	err := manager.CreateEventsFromStatus(ctx, status)
	if err != nil {
		t.Errorf("CreateEventsFromStatus() error = %v", err)
	}
}

// TestCreateEventFromProblem tests creating an event from a problem
func TestCreateEventFromProblem(t *testing.T) {
	manager := createTestEventManager(10, time.Minute)
	ctx := context.Background()

	problem := &types.Problem{
		Type:       "test-problem",
		Resource:   "test-resource",
		Severity:   types.ProblemCritical,
		Message:    "Test problem message",
		DetectedAt: time.Now(),
		Metadata:   make(map[string]string),
	}

	err := manager.CreateEventFromProblem(ctx, problem)
	if err != nil {
		t.Errorf("CreateEventFromProblem() error = %v", err)
	}
}

// TestEventManagerStats tests statistics collection
func TestEventManagerStats(t *testing.T) {
	manager := createTestEventManager(5, time.Minute)

	stats := manager.GetStats()
	if stats == nil {
		t.Error("GetStats() returned nil")
	}

	// Check expected fields
	expectedFields := []string{
		"cached_signatures",
		"current_minute_count",
		"max_events_per_min",
		"deduplication_ttl",
	}

	for _, field := range expectedFields {
		if _, exists := stats[field]; !exists {
			t.Errorf("GetStats() missing field: %s", field)
		}
	}

	// Verify values
	if stats["max_events_per_min"] != 5 {
		t.Errorf("Expected max_events_per_min = 5, got %v", stats["max_events_per_min"])
	}
}

// TestEventManagerCleanup tests cache cleanup functionality
func TestEventManagerCleanup(t *testing.T) {
	manager := createTestEventManager(10, 200*time.Millisecond) // TTL longer than sleep

	// Add some events to the cache
	signature1 := EventSignature{
		Reason:    "Test1",
		Message:   "Message1",
		Type:      "Normal",
		Source:    "test",
		Timestamp: time.Now().Add(-2 * time.Minute), // Old timestamp
	}
	signature2 := EventSignature{
		Reason:    "Test2",
		Message:   "Message2",
		Type:      "Normal",
		Source:    "test",
		Timestamp: time.Now(), // Recent timestamp
	}

	manager.recordEvent(signature1)
	manager.recordEvent(signature2)

	// Should have 2 cached signatures
	stats := manager.GetStats()
	if stats["cached_signatures"] != 2 {
		t.Errorf("Expected 2 cached signatures, got %v", stats["cached_signatures"])
	}

	// Wait for cleanup and run it manually
	time.Sleep(150 * time.Millisecond)
	manager.cleanup()

	// Should have 1 cached signature (the recent one)
	stats = manager.GetStats()
	if stats["cached_signatures"] != 1 {
		t.Errorf("Expected 1 cached signature after cleanup, got %v", stats["cached_signatures"])
	}
}

// TestEventManagerHealthCheck tests health check functionality
func TestEventManagerHealthCheck(t *testing.T) {
	manager := createTestEventManager(10, time.Minute)

	// Should be healthy initially
	if !manager.IsHealthy() {
		t.Error("EventManager should be healthy initially")
	}

	// Add too many cached signatures
	for i := 0; i < 10001; i++ {
		signature := EventSignature{
			Reason:    "Test",
			Message:   "Message",
			Type:      "Normal",
			Source:    "test",
			Timestamp: time.Now(),
		}
		signature.Reason = signature.Reason + string(rune(i)) // Make each unique
		manager.recordEvent(signature)
	}

	// Should not be healthy with too many cached entries
	if manager.IsHealthy() {
		t.Error("EventManager should not be healthy with too many cached entries")
	}
}

// TestResetAndClearMethods tests the reset and clear utility methods
func TestResetAndClearMethods(t *testing.T) {
	manager := createTestEventManager(5, time.Minute)
	ctx := context.Background()

	// Add some data to the manager
	event := corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-event",
			Namespace: "test-namespace",
		},
		InvolvedObject: corev1.ObjectReference{
			Kind: "Node",
			Name: "test-node",
		},
		Reason:         "TestReason",
		Message:        "Test message",
		Type:           corev1.EventTypeNormal,
		FirstTimestamp: metav1.NewTime(time.Now()),
		LastTimestamp:  metav1.NewTime(time.Now()),
		Count:          1,
	}

	// Create an event to populate rate limiter
	manager.CreateEvent(ctx, event)

	// Test ResetRateLimit
	manager.ResetRateLimit()
	stats := manager.GetStats()
	if stats["current_minute_count"] != 0 {
		t.Errorf("Expected current_minute_count = 0 after reset, got %v", stats["current_minute_count"])
	}

	// Test ClearCache
	manager.ClearCache()
	stats = manager.GetStats()
	if stats["cached_signatures"] != 0 {
		t.Errorf("Expected cached_signatures = 0 after clear, got %v", stats["cached_signatures"])
	}
}

// TestEventSignature tests event signature creation and hashing
func TestEventSignature(t *testing.T) {
	event := corev1.Event{
		Reason:  "TestReason",
		Message: "Test message",
		Type:    corev1.EventTypeNormal,
		Source: corev1.EventSource{
			Component: "test-component",
		},
		FirstTimestamp: metav1.NewTime(time.Date(2023, 1, 1, 12, 30, 0, 0, time.UTC)),
	}

	signature := CreateEventSignature(event)

	// Test signature fields
	if signature.Reason != "TestReason" {
		t.Errorf("Expected reason = TestReason, got %s", signature.Reason)
	}
	if signature.Message != "Test message" {
		t.Errorf("Expected message = Test message, got %s", signature.Message)
	}
	if signature.Type != corev1.EventTypeNormal {
		t.Errorf("Expected type = Normal, got %s", signature.Type)
	}
	if signature.Source != "test-component" {
		t.Errorf("Expected source = test-component, got %s", signature.Source)
	}

	// Test string representation
	str := signature.String()
	if str == "" {
		t.Error("EventSignature.String() returned empty string")
	}

	// Test hash
	hash := signature.Hash()
	if hash == "" {
		t.Error("EventSignature.Hash() returned empty string")
	}
	if len(hash) != 16 {
		t.Errorf("Expected hash length = 16, got %d", len(hash))
	}

	// Same signature should produce same hash
	signature2 := CreateEventSignature(event)
	if signature.Hash() != signature2.Hash() {
		t.Error("Same event should produce same signature hash")
	}
}
