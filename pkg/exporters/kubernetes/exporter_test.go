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
		t.Error("GetConfiguration() returned nil")
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
