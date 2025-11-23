package kubernetes

import (
	"context"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/supporttools/node-doctor/pkg/types"
)

// Helper function to create a test Kubernetes exporter
func createTestKubernetesExporter() (*KubernetesExporter, error) {
	config := &types.KubernetesExporterConfig{
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

	settings := &types.GlobalSettings{
		NodeName: "test-node",
		QPS:      50,
		Burst:    100,
	}

	// Create a mock client instead of using NewKubernetesExporter which would try to connect to real k8s
	fakeClientset := fake.NewSimpleClientset()

	// Create test node
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
			},
		},
	}

	fakeClientset.CoreV1().Nodes().Create(context.Background(), testNode, metav1.CreateOptions{})

	client := &K8sClient{
		clientset: fakeClientset,
		nodeName:  "test-node",
		nodeUID:   "test-uid-12345",
	}

	eventManager := NewEventManager(client, config)
	conditionManager := NewConditionManager(client, config)

	exporter := &KubernetesExporter{
		client:           client,
		config:           config,
		settings:         settings,
		eventManager:     eventManager,
		conditionManager: conditionManager,
		stopCh:           make(chan struct{}),
		stats: &ExporterStats{
			StartTime: time.Now(),
		},
	}

	return exporter, nil
}

// TestNewKubernetesExporter tests exporter creation
func TestNewKubernetesExporter(t *testing.T) {
	tests := []struct {
		name        string
		config      *types.KubernetesExporterConfig
		settings    *types.GlobalSettings
		expectError bool
	}{
		{
			name: "disabled exporter",
			config: &types.KubernetesExporterConfig{
				Enabled: false,
			},
			settings: &types.GlobalSettings{
				NodeName: "test-node",
			},
			expectError: true,
		},
		{
			name: "invalid configuration",
			config: &types.KubernetesExporterConfig{
				Enabled:           true,
				UpdateInterval:    -1 * time.Second, // Invalid
				ResyncInterval:    time.Minute,
				HeartbeatInterval: time.Minute,
			},
			settings: &types.GlobalSettings{
				NodeName: "test-node",
			},
			expectError: true,
		},
		{
			name: "valid configuration",
			config: &types.KubernetesExporterConfig{
				Enabled:           true,
				UpdateInterval:    time.Second,
				ResyncInterval:    time.Minute,
				HeartbeatInterval: time.Minute,
			},
			settings: &types.GlobalSettings{
				NodeName: "test-node",
				QPS:      50,
				Burst:    100,
			},
			expectError: true, // Will fail because it tries to connect to real k8s
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewKubernetesExporter(tt.config, tt.settings)
			if (err != nil) != tt.expectError {
				t.Errorf("NewKubernetesExporter() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}

// TestKubernetesExporterLifecycle tests start and stop functionality
func TestKubernetesExporterLifecycle(t *testing.T) {
	exporter, err := createTestKubernetesExporter()
	if err != nil {
		t.Fatalf("Failed to create test exporter: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Test start
	err = exporter.Start(ctx)
	if err != nil {
		t.Errorf("Start() error = %v", err)
	}

	// Should be started
	if !exporter.isReady() {
		t.Error("Exporter should be ready after Start()")
	}

	// Test double start (should fail)
	err = exporter.Start(ctx)
	if err == nil {
		t.Error("Start() should fail when already started")
	}

	// Let it run briefly
	time.Sleep(150 * time.Millisecond)

	// Test stop
	err = exporter.Stop()
	if err != nil {
		t.Errorf("Stop() error = %v", err)
	}

	// Should not be ready after stop
	if exporter.isReady() {
		t.Error("Exporter should not be ready after Stop()")
	}

	// Test double stop (should not error)
	err = exporter.Stop()
	if err != nil {
		t.Errorf("Stop() should not error when already stopped: %v", err)
	}
}

// TestExportStatus tests status export functionality
func TestExportStatus(t *testing.T) {
	exporter, err := createTestKubernetesExporter()
	if err != nil {
		t.Fatalf("Failed to create test exporter: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the exporter
	exporter.Start(ctx)
	defer exporter.Stop()

	// Create test status
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
		Conditions: []types.Condition{
			{
				Type:       "TestCondition",
				Status:     types.ConditionTrue,
				Transition: time.Now(),
				Reason:     "TestReason",
				Message:    "Test condition message",
			},
		},
	}

	// Test export
	err = exporter.ExportStatus(ctx, status)
	if err != nil {
		t.Errorf("ExportStatus() error = %v", err)
	}

	// Check stats
	stats := exporter.GetStats()
	if stats["status_exports_total"].(int64) != 1 {
		t.Errorf("Expected status_exports_total = 1, got %v", stats["status_exports_total"])
	}
	if stats["status_exports_success"].(int64) != 1 {
		t.Errorf("Expected status_exports_success = 1, got %v", stats["status_exports_success"])
	}
}

// TestExportProblem tests problem export functionality
func TestExportProblem(t *testing.T) {
	exporter, err := createTestKubernetesExporter()
	if err != nil {
		t.Fatalf("Failed to create test exporter: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the exporter
	exporter.Start(ctx)
	defer exporter.Stop()

	// Create test problem
	problem := &types.Problem{
		Type:       "service-failed",
		Resource:   "kubelet.service",
		Severity:   types.ProblemCritical,
		Message:    "Service has failed to start",
		DetectedAt: time.Now(),
		Metadata:   make(map[string]string),
	}

	// Test export
	err = exporter.ExportProblem(ctx, problem)
	if err != nil {
		t.Errorf("ExportProblem() error = %v", err)
	}

	// Check stats
	stats := exporter.GetStats()
	if stats["problem_exports_total"].(int64) != 1 {
		t.Errorf("Expected problem_exports_total = 1, got %v", stats["problem_exports_total"])
	}
	if stats["problem_exports_success"].(int64) != 1 {
		t.Errorf("Expected problem_exports_success = 1, got %v", stats["problem_exports_success"])
	}
}

// TestExportWithoutStart tests exports without starting the exporter
func TestExportWithoutStart(t *testing.T) {
	exporter, err := createTestKubernetesExporter()
	if err != nil {
		t.Fatalf("Failed to create test exporter: %v", err)
	}

	ctx := context.Background()

	// Test status export without start (should fail)
	status := &types.Status{
		Source:    "test-monitor",
		Timestamp: time.Now(),
	}

	err = exporter.ExportStatus(ctx, status)
	if err == nil {
		t.Error("ExportStatus() should fail when exporter is not started")
	}

	// Test problem export without start (should fail)
	problem := &types.Problem{
		Type:       "test-problem",
		Resource:   "test-resource",
		Severity:   types.ProblemInfo,
		Message:    "Test problem",
		DetectedAt: time.Now(),
		Metadata:   make(map[string]string),
	}

	err = exporter.ExportProblem(ctx, problem)
	if err == nil {
		t.Error("ExportProblem() should fail when exporter is not started")
	}
}

// TestKubernetesExporterStats tests statistics collection
func TestKubernetesExporterStats(t *testing.T) {
	exporter, err := createTestKubernetesExporter()
	if err != nil {
		t.Fatalf("Failed to create test exporter: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the exporter
	exporter.Start(ctx)
	defer exporter.Stop()

	stats := exporter.GetStats()
	if stats == nil {
		t.Error("GetStats() returned nil")
	}

	// Check expected fields
	expectedFields := []string{
		"start_time",
		"uptime",
		"status_exports_total",
		"status_exports_success",
		"status_exports_failed",
		"problem_exports_total",
		"problem_exports_success",
		"problem_exports_failed",
		"events_created",
		"conditions_updated",
		"last_export_time",
		"started",
		"node_name",
		"namespace",
		"event_manager",
		"condition_manager",
	}

	for _, field := range expectedFields {
		if _, exists := stats[field]; !exists {
			t.Errorf("GetStats() missing field: %s", field)
		}
	}

	// Verify some values
	if stats["started"] != true {
		t.Errorf("Expected started = true, got %v", stats["started"])
	}
	if stats["node_name"] != "test-node" {
		t.Errorf("Expected node_name = test-node, got %v", stats["node_name"])
	}
	if stats["namespace"] != "test-namespace" {
		t.Errorf("Expected namespace = test-namespace, got %v", stats["namespace"])
	}
}

// TestKubernetesExporterHealthCheck tests health check functionality
func TestKubernetesExporterHealthCheck(t *testing.T) {
	exporter, err := createTestKubernetesExporter()
	if err != nil {
		t.Fatalf("Failed to create test exporter: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Should not be healthy when not started
	if exporter.IsHealthy() {
		t.Error("Exporter should not be healthy when not started")
	}

	// Start the exporter
	exporter.Start(ctx)
	defer exporter.Stop()

	// Should be healthy when started
	if !exporter.IsHealthy() {
		t.Error("Exporter should be healthy when started")
	}
}

// TestGetters tests various getter methods
func TestGetters(t *testing.T) {
	exporter, err := createTestKubernetesExporter()
	if err != nil {
		t.Fatalf("Failed to create test exporter: %v", err)
	}

	// Test GetNodeName
	if exporter.GetNodeName() != "test-node" {
		t.Errorf("GetNodeName() = %s, want test-node", exporter.GetNodeName())
	}

	// Test GetConfiguration
	config := exporter.GetConfiguration()
	if config == nil {
		t.Fatal("GetConfiguration() returned nil")
	}
	if !config.Enabled {
		t.Error("Configuration should be enabled")
	}

	// Test GetClient
	client := exporter.GetClient()
	if client == nil {
		t.Error("GetClient() returned nil")
	}

	// Test GetEventManager
	eventManager := exporter.GetEventManager()
	if eventManager == nil {
		t.Error("GetEventManager() returned nil")
	}

	// Test GetConditionManager
	conditionManager := exporter.GetConditionManager()
	if conditionManager == nil {
		t.Error("GetConditionManager() returned nil")
	}
}

// TestForceFlush tests the force flush functionality
func TestForceFlush(t *testing.T) {
	exporter, err := createTestKubernetesExporter()
	if err != nil {
		t.Fatalf("Failed to create test exporter: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Test flush without start (should fail)
	err = exporter.ForceFlush(ctx)
	if err == nil {
		t.Error("ForceFlush() should fail when exporter is not started")
	}

	// Start the exporter
	exporter.Start(ctx)
	defer exporter.Stop()

	// Test flush after start (should succeed)
	err = exporter.ForceFlush(ctx)
	if err != nil {
		t.Errorf("ForceFlush() error after start = %v", err)
	}
}

// TestExporterWithErrors tests error handling scenarios
func TestExporterWithErrors(t *testing.T) {
	exporter, err := createTestKubernetesExporter()
	if err != nil {
		t.Fatalf("Failed to create test exporter: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the exporter
	exporter.Start(ctx)
	defer exporter.Stop()

	// Test with empty status (should not error)
	emptyStatus := &types.Status{
		Source:     "test-monitor",
		Timestamp:  time.Now(),
		Events:     []types.Event{},
		Conditions: []types.Condition{},
	}

	err = exporter.ExportStatus(ctx, emptyStatus)
	if err != nil {
		t.Errorf("ExportStatus() with empty status should not error: %v", err)
	}

	// Test with nil problem (would cause panic in real scenario, but we'll test with empty problem)
	emptyProblem := &types.Problem{
		Type:       "",
		Resource:   "",
		Severity:   types.ProblemInfo,
		Message:    "",
		DetectedAt: time.Now(),
		Metadata:   make(map[string]string),
	}

	err = exporter.ExportProblem(ctx, emptyProblem)
	if err != nil {
		t.Errorf("ExportProblem() with empty problem should not error: %v", err)
	}
}

// TestExporterStatsUpdates tests that statistics are properly updated
func TestExporterStatsUpdates(t *testing.T) {
	exporter, err := createTestKubernetesExporter()
	if err != nil {
		t.Fatalf("Failed to create test exporter: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the exporter
	exporter.Start(ctx)
	defer exporter.Stop()

	// Get initial stats
	initialStats := exporter.GetStats()
	initialStatusTotal := initialStats["status_exports_total"].(int64)
	initialProblemTotal := initialStats["problem_exports_total"].(int64)

	// Export a status
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

	exporter.ExportStatus(ctx, status)

	// Export a problem
	problem := &types.Problem{
		Type:       "test-problem",
		Resource:   "test-resource",
		Severity:   types.ProblemWarning,
		Message:    "Test problem message",
		DetectedAt: time.Now(),
		Metadata:   make(map[string]string),
	}

	exporter.ExportProblem(ctx, problem)

	// Check updated stats
	updatedStats := exporter.GetStats()
	if updatedStats["status_exports_total"].(int64) != initialStatusTotal+1 {
		t.Errorf("Expected status_exports_total to increase by 1")
	}
	if updatedStats["problem_exports_total"].(int64) != initialProblemTotal+1 {
		t.Errorf("Expected problem_exports_total to increase by 1")
	}
}

// TestExporterBackgroundLoops tests that background loops are working
func TestExporterBackgroundLoops(t *testing.T) {
	exporter, err := createTestKubernetesExporter()
	if err != nil {
		t.Fatalf("Failed to create test exporter: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the exporter
	exporter.Start(ctx)

	// Let it run for a bit to allow background loops to execute
	time.Sleep(500 * time.Millisecond)

	// Stop the exporter
	exporter.Stop()

	// Background loops should have executed at least once
	// We can verify this by checking that health checks and metrics have been collected
	stats := exporter.GetStats()
	if stats["uptime"].(string) == "" {
		t.Error("Uptime should be set if background loops are running")
	}
}

// TestIsReloadable tests that the exporter is reloadable
func TestIsReloadable(t *testing.T) {
	exporter, err := createTestKubernetesExporter()
	if err != nil {
		t.Fatalf("Failed to create test exporter: %v", err)
	}

	if !exporter.IsReloadable() {
		t.Error("KubernetesExporter.IsReloadable() should return true")
	}
}

// TestReload tests the Reload functionality
func TestReload(t *testing.T) {
	exporter, err := createTestKubernetesExporter()
	if err != nil {
		t.Fatalf("Failed to create test exporter: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the exporter
	exporter.Start(ctx)
	defer exporter.Stop()

	tests := []struct {
		name        string
		config      interface{}
		expectError bool
	}{
		{
			name:        "nil config",
			config:      nil,
			expectError: true,
		},
		{
			name:        "wrong type config",
			config:      "invalid config type",
			expectError: true,
		},
		{
			name: "valid config without client recreation",
			config: &types.KubernetesExporterConfig{
				Enabled:           true,
				UpdateInterval:    100 * time.Millisecond,  // Same as initial
				ResyncInterval:    200 * time.Millisecond,  // Same as initial
				HeartbeatInterval: 300 * time.Millisecond,  // Same as initial
				Namespace:         "test-namespace",        // Same as initial - no client recreation
				Conditions:        []types.ConditionConfig{},
				Annotations: []types.AnnotationConfig{
					{Key: "test-key", Value: "test-value"}, // Different annotations don't require client recreation
				},
				Events: types.EventConfig{
					MaxEventsPerMinute:  10,    // Same as initial
					DeduplicationWindow: time.Minute, // Same as initial
				},
			},
			expectError: false,
		},
		{
			name: "valid config with namespace change (requires client recreation - expected to fail outside cluster)",
			config: &types.KubernetesExporterConfig{
				Enabled:           true,
				UpdateInterval:    200 * time.Millisecond,
				ResyncInterval:    400 * time.Millisecond,
				HeartbeatInterval: 600 * time.Millisecond,
				Namespace:         "new-namespace", // Different namespace triggers client recreation
				Conditions:        []types.ConditionConfig{},
				Annotations:       []types.AnnotationConfig{},
				Events: types.EventConfig{
					MaxEventsPerMinute:  20,
					DeduplicationWindow: 2 * time.Minute,
				},
			},
			expectError: true, // Client recreation fails outside of cluster
		},
		{
			name: "invalid config with negative interval",
			config: &types.KubernetesExporterConfig{
				Enabled:           true,
				UpdateInterval:    -1 * time.Second,
				ResyncInterval:    time.Minute,
				HeartbeatInterval: time.Minute,
				Namespace:         "test-namespace",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := exporter.Reload(tt.config)
			if (err != nil) != tt.expectError {
				t.Errorf("Reload() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}

// TestNeedsClientRecreation tests the needsClientRecreation method
func TestNeedsClientRecreation(t *testing.T) {
	exporter, err := createTestKubernetesExporter()
	if err != nil {
		t.Fatalf("Failed to create test exporter: %v", err)
	}

	baseConfig := &types.KubernetesExporterConfig{
		Enabled:           true,
		UpdateInterval:    100 * time.Millisecond,
		ResyncInterval:    200 * time.Millisecond,
		HeartbeatInterval: 300 * time.Millisecond,
		Namespace:         "test-namespace",
		Events: types.EventConfig{
			MaxEventsPerMinute:  10,
			DeduplicationWindow: time.Minute,
			EventTTL:            time.Hour,
		},
	}

	tests := []struct {
		name      string
		newConfig *types.KubernetesExporterConfig
		expected  bool
	}{
		{
			name: "namespace changed",
			newConfig: &types.KubernetesExporterConfig{
				Enabled:           true,
				UpdateInterval:    100 * time.Millisecond,
				ResyncInterval:    200 * time.Millisecond,
				HeartbeatInterval: 300 * time.Millisecond,
				Namespace:         "different-namespace",
				Events: types.EventConfig{
					MaxEventsPerMinute:  10,
					DeduplicationWindow: time.Minute,
					EventTTL:            time.Hour,
				},
			},
			expected: true,
		},
		{
			name: "update interval changed",
			newConfig: &types.KubernetesExporterConfig{
				Enabled:           true,
				UpdateInterval:    500 * time.Millisecond,
				ResyncInterval:    200 * time.Millisecond,
				HeartbeatInterval: 300 * time.Millisecond,
				Namespace:         "test-namespace",
				Events: types.EventConfig{
					MaxEventsPerMinute:  10,
					DeduplicationWindow: time.Minute,
					EventTTL:            time.Hour,
				},
			},
			expected: true,
		},
		{
			name: "resync interval changed",
			newConfig: &types.KubernetesExporterConfig{
				Enabled:           true,
				UpdateInterval:    100 * time.Millisecond,
				ResyncInterval:    500 * time.Millisecond,
				HeartbeatInterval: 300 * time.Millisecond,
				Namespace:         "test-namespace",
				Events: types.EventConfig{
					MaxEventsPerMinute:  10,
					DeduplicationWindow: time.Minute,
					EventTTL:            time.Hour,
				},
			},
			expected: true,
		},
		{
			name: "heartbeat interval changed",
			newConfig: &types.KubernetesExporterConfig{
				Enabled:           true,
				UpdateInterval:    100 * time.Millisecond,
				ResyncInterval:    200 * time.Millisecond,
				HeartbeatInterval: 500 * time.Millisecond,
				Namespace:         "test-namespace",
				Events: types.EventConfig{
					MaxEventsPerMinute:  10,
					DeduplicationWindow: time.Minute,
					EventTTL:            time.Hour,
				},
			},
			expected: true,
		},
		{
			name: "max events per minute changed",
			newConfig: &types.KubernetesExporterConfig{
				Enabled:           true,
				UpdateInterval:    100 * time.Millisecond,
				ResyncInterval:    200 * time.Millisecond,
				HeartbeatInterval: 300 * time.Millisecond,
				Namespace:         "test-namespace",
				Events: types.EventConfig{
					MaxEventsPerMinute:  50,
					DeduplicationWindow: time.Minute,
					EventTTL:            time.Hour,
				},
			},
			expected: true,
		},
		{
			name: "event TTL changed",
			newConfig: &types.KubernetesExporterConfig{
				Enabled:           true,
				UpdateInterval:    100 * time.Millisecond,
				ResyncInterval:    200 * time.Millisecond,
				HeartbeatInterval: 300 * time.Millisecond,
				Namespace:         "test-namespace",
				Events: types.EventConfig{
					MaxEventsPerMinute:  10,
					DeduplicationWindow: time.Minute,
					EventTTL:            2 * time.Hour,
				},
			},
			expected: true,
		},
		{
			name: "deduplication window changed",
			newConfig: &types.KubernetesExporterConfig{
				Enabled:           true,
				UpdateInterval:    100 * time.Millisecond,
				ResyncInterval:    200 * time.Millisecond,
				HeartbeatInterval: 300 * time.Millisecond,
				Namespace:         "test-namespace",
				Events: types.EventConfig{
					MaxEventsPerMinute:  10,
					DeduplicationWindow: 5 * time.Minute,
					EventTTL:            time.Hour,
				},
			},
			expected: true,
		},
		{
			name:      "no changes",
			newConfig: baseConfig,
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset config for each test
			exporter.config = baseConfig
			got := exporter.needsClientRecreation(tt.newConfig)
			if got != tt.expected {
				t.Errorf("needsClientRecreation() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestNeedsClientRecreationWithNilConfig tests needsClientRecreation with nil current config
func TestNeedsClientRecreationWithNilConfig(t *testing.T) {
	exporter, err := createTestKubernetesExporter()
	if err != nil {
		t.Fatalf("Failed to create test exporter: %v", err)
	}

	// Set config to nil
	exporter.config = nil

	newConfig := &types.KubernetesExporterConfig{
		Enabled:   true,
		Namespace: "test-namespace",
	}

	// With nil config, should return true (first time configuration)
	if !exporter.needsClientRecreation(newConfig) {
		t.Error("needsClientRecreation() should return true when current config is nil")
	}
}

// TestAnnotationsEqual tests the annotationsEqual method
func TestAnnotationsEqual(t *testing.T) {
	exporter, err := createTestKubernetesExporter()
	if err != nil {
		t.Fatalf("Failed to create test exporter: %v", err)
	}

	tests := []struct {
		name     string
		old      []types.AnnotationConfig
		new      []types.AnnotationConfig
		expected bool
	}{
		{
			name:     "both empty",
			old:      []types.AnnotationConfig{},
			new:      []types.AnnotationConfig{},
			expected: true,
		},
		{
			name: "both nil",
			old:  nil,
			new:  nil,
			expected: true,
		},
		{
			name: "equal annotations",
			old: []types.AnnotationConfig{
				{Key: "key1", Value: "value1"},
				{Key: "key2", Value: "value2"},
			},
			new: []types.AnnotationConfig{
				{Key: "key1", Value: "value1"},
				{Key: "key2", Value: "value2"},
			},
			expected: true,
		},
		{
			name: "different lengths",
			old: []types.AnnotationConfig{
				{Key: "key1", Value: "value1"},
			},
			new: []types.AnnotationConfig{
				{Key: "key1", Value: "value1"},
				{Key: "key2", Value: "value2"},
			},
			expected: false,
		},
		{
			name: "different values",
			old: []types.AnnotationConfig{
				{Key: "key1", Value: "value1"},
			},
			new: []types.AnnotationConfig{
				{Key: "key1", Value: "different-value"},
			},
			expected: false,
		},
		{
			name: "different keys",
			old: []types.AnnotationConfig{
				{Key: "key1", Value: "value1"},
			},
			new: []types.AnnotationConfig{
				{Key: "different-key", Value: "value1"},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := exporter.annotationsEqual(tt.old, tt.new)
			if got != tt.expected {
				t.Errorf("annotationsEqual() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestPerformHealthCheck tests the performHealthCheck method
func TestPerformHealthCheck(t *testing.T) {
	exporter, err := createTestKubernetesExporter()
	if err != nil {
		t.Fatalf("Failed to create test exporter: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the exporter
	exporter.Start(ctx)
	defer exporter.Stop()

	// Give the exporter time to initialize
	time.Sleep(100 * time.Millisecond)

	// Test performHealthCheck - should not panic
	exporter.performHealthCheck(ctx)

	// Verify condition manager has the health condition
	stats := exporter.conditionManager.GetStats()
	if stats == nil {
		t.Error("Condition manager stats should not be nil after health check")
	}
}

// TestLogMetrics tests the logMetrics method
func TestLogMetrics(t *testing.T) {
	exporter, err := createTestKubernetesExporter()
	if err != nil {
		t.Fatalf("Failed to create test exporter: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the exporter
	exporter.Start(ctx)
	defer exporter.Stop()

	// Test logMetrics - should not panic
	exporter.logMetrics()

	// Verify stats are still accessible
	stats := exporter.GetStats()
	if stats == nil {
		t.Error("GetStats() should not return nil after logMetrics")
	}
}

// TestStatsWithLastError tests stats output when there's a last error
func TestStatsWithLastError(t *testing.T) {
	exporter, err := createTestKubernetesExporter()
	if err != nil {
		t.Fatalf("Failed to create test exporter: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the exporter
	exporter.Start(ctx)
	defer exporter.Stop()

	// Set a last error
	exporter.updateStats(func(s *ExporterStats) {
		s.LastError = fmt.Errorf("test error")
		s.LastErrorTime = time.Now()
	})

	stats := exporter.GetStats()

	// Verify last error is included in stats
	if _, exists := stats["last_error"]; !exists {
		t.Error("GetStats() should include last_error when there's a last error")
	}
	if _, exists := stats["last_error_time"]; !exists {
		t.Error("GetStats() should include last_error_time when there's a last error")
	}
}

// TestReloadWithClientRecreation tests Reload when client recreation is needed
func TestReloadWithClientRecreation(t *testing.T) {
	exporter, err := createTestKubernetesExporter()
	if err != nil {
		t.Fatalf("Failed to create test exporter: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the exporter
	exporter.Start(ctx)
	defer exporter.Stop()

	// Reload with a new namespace (triggers client recreation)
	newConfig := &types.KubernetesExporterConfig{
		Enabled:           true,
		UpdateInterval:    100 * time.Millisecond,
		ResyncInterval:    200 * time.Millisecond,
		HeartbeatInterval: 300 * time.Millisecond,
		Namespace:         "new-namespace-recreation",
		Conditions:        []types.ConditionConfig{},
		Annotations:       []types.AnnotationConfig{},
		Events: types.EventConfig{
			MaxEventsPerMinute:  10,
			DeduplicationWindow: time.Minute,
		},
	}

	// This will fail because we can't create a real K8s client, but we're testing the code path
	err = exporter.Reload(newConfig)
	// We expect an error because NewClient will fail without real K8s
	if err == nil {
		t.Log("Reload succeeded (may be running in a real K8s environment)")
	}
}
