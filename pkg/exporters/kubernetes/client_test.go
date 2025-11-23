package kubernetes

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	"github.com/supporttools/node-doctor/pkg/types"
)

// TestNewClient tests the client creation with different configurations
func TestNewClient(t *testing.T) {
	tests := []struct {
		name        string
		config      *types.KubernetesExporterConfig
		settings    *types.GlobalSettings
		expectError bool
	}{
		{
			name: "valid configuration",
			config: &types.KubernetesExporterConfig{
				Enabled: true,
			},
			settings: &types.GlobalSettings{
				NodeName: "test-node",
				QPS:      50,
				Burst:    100,
			},
			expectError: true, // Will fail because no real kubeconfig
		},
		{
			name: "empty node name",
			config: &types.KubernetesExporterConfig{
				Enabled: true,
			},
			settings: &types.GlobalSettings{
				NodeName: "",
				QPS:      50,
				Burst:    100,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewClient(tt.config, tt.settings)
			if (err != nil) != tt.expectError {
				t.Errorf("NewClient() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}

// TestK8sClientOperations tests various client operations with fake client
func TestK8sClientOperations(t *testing.T) {
	// Create a fake clientset
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

	// Add the node to the fake clientset
	_, err := fakeClientset.CoreV1().Nodes().Create(context.Background(), testNode, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create test node: %v", err)
	}

	// Create client with fake clientset
	client := &K8sClient{
		clientset: fakeClientset,
		nodeName:  "test-node",
		nodeUID:   "test-uid-12345",
	}

	ctx := context.Background()

	t.Run("GetNode", func(t *testing.T) {
		node, err := client.GetNode(ctx)
		if err != nil {
			t.Errorf("GetNode() error = %v", err)
			return
		}
		if node.Name != "test-node" {
			t.Errorf("GetNode() returned wrong node name: %s", node.Name)
		}
	})

	t.Run("PatchNodeConditions", func(t *testing.T) {
		conditions := []corev1.NodeCondition{
			{
				Type:               "TestCondition",
				Status:             corev1.ConditionTrue,
				LastTransitionTime: metav1.NewTime(time.Now()),
				Reason:             "TestReason",
				Message:            "Test message",
			},
		}

		err := client.PatchNodeConditions(ctx, conditions)
		if err != nil {
			t.Errorf("PatchNodeConditions() error = %v", err)
		}
	})

	t.Run("CreateEvent", func(t *testing.T) {
		event := corev1.Event{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-event",
				Namespace: "default",
			},
			InvolvedObject: corev1.ObjectReference{
				Kind: "Node",
				Name: "test-node",
			},
			Reason:  "TestEvent",
			Message: "This is a test event",
			Type:    corev1.EventTypeNormal,
			Source: corev1.EventSource{
				Component: "node-doctor",
				Host:      "test-node",
			},
			FirstTimestamp: metav1.NewTime(time.Now()),
			LastTimestamp:  metav1.NewTime(time.Now()),
			Count:          1,
		}

		err := client.CreateEvent(ctx, event, "default")
		if err != nil {
			t.Errorf("CreateEvent() error = %v", err)
		}

		// Verify event was created
		events, err := fakeClientset.CoreV1().Events("default").List(ctx, metav1.ListOptions{})
		if err != nil {
			t.Errorf("Failed to list events: %v", err)
		}
		if len(events.Items) != 1 {
			t.Errorf("Expected 1 event, got %d", len(events.Items))
		}
	})

	t.Run("UpdateNodeAnnotations", func(t *testing.T) {
		annotations := map[string]string{
			"test-annotation":        "test-value",
			"node-doctor.io/version": "test-version",
		}

		err := client.UpdateNodeAnnotations(ctx, annotations)
		if err != nil {
			t.Errorf("UpdateNodeAnnotations() error = %v", err)
		}
	})

	t.Run("ListEvents", func(t *testing.T) {
		events, err := client.ListEvents(ctx, "default")
		if err != nil {
			t.Errorf("ListEvents() error = %v", err)
		}
		if events == nil {
			t.Error("ListEvents() returned nil")
		}
	})

	t.Run("IsHealthy", func(t *testing.T) {
		healthy := client.IsHealthy(ctx)
		if !healthy {
			t.Error("IsHealthy() returned false for healthy client")
		}
	})

	t.Run("GetNodeName", func(t *testing.T) {
		name := client.GetNodeName()
		if name != "test-node" {
			t.Errorf("GetNodeName() = %s, want test-node", name)
		}
	})

	t.Run("GetNodeUID", func(t *testing.T) {
		uid := client.GetNodeUID()
		if uid != "test-uid-12345" {
			t.Errorf("GetNodeUID() = %s, want test-uid-12345", uid)
		}
	})
}

// TestK8sClientRetryLogic tests the retry logic with simulated failures
func TestK8sClientRetryLogic(t *testing.T) {
	fakeClientset := fake.NewSimpleClientset()

	// Create test node
	testNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
			UID:  "test-uid-12345",
		},
	}
	_, err := fakeClientset.CoreV1().Nodes().Create(context.Background(), testNode, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create test node: %v", err)
	}

	client := &K8sClient{
		clientset: fakeClientset,
		nodeName:  "test-node",
		nodeUID:   "test-uid-12345",
	}

	ctx := context.Background()

	t.Run("retry on transient errors", func(t *testing.T) {
		failCount := 0
		maxFails := 2

		// Add reactor to simulate transient failures
		fakeClientset.PrependReactor("patch", "nodes", func(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
			if failCount < maxFails {
				failCount++
				return true, nil, errors.NewServiceUnavailable("service unavailable")
			}
			return false, nil, nil // Let the original action proceed
		})

		conditions := []corev1.NodeCondition{
			{
				Type:               "TestCondition",
				Status:             corev1.ConditionTrue,
				LastTransitionTime: metav1.NewTime(time.Now()),
				Reason:             "TestReason",
				Message:            "Test message",
			},
		}

		// This should succeed after retries
		err := client.PatchNodeConditions(ctx, conditions)
		if err != nil {
			t.Errorf("PatchNodeConditions() should have succeeded after retries, got error: %v", err)
		}

		if failCount != maxFails {
			t.Errorf("Expected %d failures before success, got %d", maxFails, failCount)
		}

		// Clear the reactor
		fakeClientset.ReactionChain = []ktesting.Reactor{}
	})

	t.Run("duplicate event handling", func(t *testing.T) {
		event := corev1.Event{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "duplicate-test-event",
				Namespace: "default",
			},
			InvolvedObject: corev1.ObjectReference{
				Kind: "Node",
				Name: "test-node",
				UID:  k8stypes.UID("test-uid-12345"),
			},
			Reason:  "DuplicateTest",
			Message: "This is a duplicate test event",
			Type:    corev1.EventTypeNormal,
		}

		// Create the event first time
		err := client.CreateEvent(ctx, event, "default")
		if err != nil {
			t.Errorf("First CreateEvent() failed: %v", err)
		}

		// Try to create the same event again - should not error
		err = client.CreateEvent(ctx, event, "default")
		if err != nil {
			t.Errorf("Second CreateEvent() should not error on duplicate: %v", err)
		}
	})
}

// TestK8sClientErrorHandling tests error handling scenarios
func TestK8sClientErrorHandling(t *testing.T) {
	fakeClientset := fake.NewSimpleClientset()

	client := &K8sClient{
		clientset: fakeClientset,
		nodeName:  "nonexistent-node",
		nodeUID:   "nonexistent-uid",
	}

	ctx := context.Background()

	t.Run("GetNode with nonexistent node", func(t *testing.T) {
		_, err := client.GetNode(ctx)
		if err == nil {
			t.Error("GetNode() should have failed for nonexistent node")
		}
	})

	t.Run("IsHealthy with nonexistent node", func(t *testing.T) {
		healthy := client.IsHealthy(ctx)
		if healthy {
			t.Error("IsHealthy() should return false for nonexistent node")
		}
	})

	t.Run("PatchNodeConditions with empty conditions", func(t *testing.T) {
		err := client.PatchNodeConditions(ctx, []corev1.NodeCondition{})
		if err != nil {
			t.Errorf("PatchNodeConditions() with empty conditions should not error: %v", err)
		}
	})

	t.Run("UpdateNodeAnnotations with empty annotations", func(t *testing.T) {
		err := client.UpdateNodeAnnotations(ctx, map[string]string{})
		if err != nil {
			t.Errorf("UpdateNodeAnnotations() with empty annotations should not error: %v", err)
		}
	})
}

// TestInitializeNodeUID tests the node UID initialization
func TestInitializeNodeUID(t *testing.T) {
	fakeClientset := fake.NewSimpleClientset()

	// Create test node
	testNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
			UID:  "test-uid-12345",
		},
	}
	_, err := fakeClientset.CoreV1().Nodes().Create(context.Background(), testNode, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create test node: %v", err)
	}

	client := &K8sClient{
		clientset: fakeClientset,
		nodeName:  "test-node",
	}

	ctx := context.Background()

	t.Run("successful initialization", func(t *testing.T) {
		err := client.initializeNodeUID(ctx)
		if err != nil {
			t.Errorf("initializeNodeUID() error = %v", err)
		}
		if client.nodeUID != "test-uid-12345" {
			t.Errorf("initializeNodeUID() set wrong UID: %s", client.nodeUID)
		}
	})

	t.Run("initialization with nonexistent node", func(t *testing.T) {
		client := &K8sClient{
			clientset: fakeClientset,
			nodeName:  "nonexistent-node",
		}

		err := client.initializeNodeUID(ctx)
		if err == nil {
			t.Error("initializeNodeUID() should have failed for nonexistent node")
		}
	})
}

// TestJsonMarshal tests the custom JSON marshaling function
func TestJsonMarshal(t *testing.T) {
	tests := []struct {
		name        string
		input       interface{}
		expectError bool
	}{
		{
			name: "node conditions patch",
			input: map[string]interface{}{
				"status": map[string]interface{}{
					"conditions": []corev1.NodeCondition{
						{
							Type:               "TestCondition",
							Status:             corev1.ConditionTrue,
							LastTransitionTime: metav1.NewTime(time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)),
							Reason:             "TestReason",
							Message:            "Test message",
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "node annotations patch",
			input: map[string]interface{}{
				"metadata": map[string]interface{}{
					"annotations": map[string]string{
						"test-key": "test-value",
					},
				},
			},
			expectError: false,
		},
		{
			name: "arbitrary data type",
			input: map[string]interface{}{
				"arbitrary": "data",
				"number":    42,
				"boolean":   true,
			},
			expectError: false,
		},
		{
			name: "invalid data type (function)",
			input: map[string]interface{}{
				"function": func() {},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := jsonMarshal(tt.input)
			if (err != nil) != tt.expectError {
				t.Errorf("jsonMarshal() error = %v, expectError %v", err, tt.expectError)
				return
			}
			if !tt.expectError && len(result) == 0 {
				t.Error("jsonMarshal() returned empty result")
			}
		})
	}
}

// TestConditionUpdateString tests the String method of ConditionUpdate
func TestConditionUpdateString(t *testing.T) {
	tests := []struct {
		name     string
		update   ConditionUpdate
		expected string
	}{
		{
			name: "true condition",
			update: ConditionUpdate{
				Type:    "TestCondition",
				Status:  corev1.ConditionTrue,
				Reason:  "TestReason",
				Message: "Test message",
			},
			expected: "TestCondition=True: Test message",
		},
		{
			name: "false condition",
			update: ConditionUpdate{
				Type:    "FailedCondition",
				Status:  corev1.ConditionFalse,
				Reason:  "Failed",
				Message: "Something failed",
			},
			expected: "FailedCondition=False: Something failed",
		},
		{
			name: "unknown condition",
			update: ConditionUpdate{
				Type:    "UnknownCondition",
				Status:  corev1.ConditionUnknown,
				Reason:  "Unknown",
				Message: "Status is unknown",
			},
			expected: "UnknownCondition=Unknown: Status is unknown",
		},
		{
			name: "empty message",
			update: ConditionUpdate{
				Type:    "EmptyMessage",
				Status:  corev1.ConditionTrue,
				Reason:  "EmptyReason",
				Message: "",
			},
			expected: "EmptyMessage=True: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.update.String()
			if got != tt.expected {
				t.Errorf("ConditionUpdate.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestConvertConditionsToNodeConditions tests the ConvertConditionsToNodeConditions function
func TestConvertConditionsToNodeConditions(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name       string
		conditions []types.Condition
		wantCount  int
	}{
		{
			name:       "empty conditions",
			conditions: []types.Condition{},
			wantCount:  0,
		},
		{
			name:       "nil conditions",
			conditions: nil,
			wantCount:  0,
		},
		{
			name: "single condition",
			conditions: []types.Condition{
				{
					Type:       "TestCondition",
					Status:     types.ConditionTrue,
					Transition: now,
					Reason:     "TestReason",
					Message:    "Test message",
				},
			},
			wantCount: 1,
		},
		{
			name: "multiple conditions",
			conditions: []types.Condition{
				{
					Type:       "Condition1",
					Status:     types.ConditionTrue,
					Transition: now,
					Reason:     "Reason1",
					Message:    "Message 1",
				},
				{
					Type:       "Condition2",
					Status:     types.ConditionFalse,
					Transition: now,
					Reason:     "Reason2",
					Message:    "Message 2",
				},
				{
					Type:       "Condition3",
					Status:     types.ConditionUnknown,
					Transition: now,
					Reason:     "Reason3",
					Message:    "Message 3",
				},
			},
			wantCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertConditionsToNodeConditions(tt.conditions)
			if len(result) != tt.wantCount {
				t.Errorf("ConvertConditionsToNodeConditions() returned %d conditions, want %d", len(result), tt.wantCount)
			}

			// Verify the conversion was correct for non-empty results
			for i, cond := range result {
				if tt.wantCount > 0 {
					// Check that the condition type was set
					if string(cond.Type) == "" {
						t.Errorf("Condition %d has empty type", i)
					}
					// Check that status was mapped correctly
					if cond.Status == "" {
						t.Errorf("Condition %d has empty status", i)
					}
				}
			}
		})
	}
}

// TestConvertConditionsToNodeConditionsStatusMapping tests status mapping in ConvertConditionsToNodeConditions
func TestConvertConditionsToNodeConditionsStatusMapping(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name         string
		inputStatus  types.ConditionStatus
		expectStatus corev1.ConditionStatus
	}{
		{
			name:         "True status",
			inputStatus:  types.ConditionTrue,
			expectStatus: corev1.ConditionTrue,
		},
		{
			name:         "False status",
			inputStatus:  types.ConditionFalse,
			expectStatus: corev1.ConditionFalse,
		},
		{
			name:         "Unknown status",
			inputStatus:  types.ConditionUnknown,
			expectStatus: corev1.ConditionUnknown,
		},
		{
			name:         "Invalid status defaults to Unknown",
			inputStatus:  types.ConditionStatus("Invalid"),
			expectStatus: corev1.ConditionUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conditions := []types.Condition{
				{
					Type:       "TestCondition",
					Status:     tt.inputStatus,
					Transition: now,
					Reason:     "TestReason",
					Message:    "Test message",
				},
			}

			result := ConvertConditionsToNodeConditions(conditions)
			if len(result) != 1 {
				t.Fatalf("Expected 1 condition, got %d", len(result))
			}

			if result[0].Status != tt.expectStatus {
				t.Errorf("ConvertConditionsToNodeConditions() status = %v, want %v", result[0].Status, tt.expectStatus)
			}
		})
	}
}

// TestConvertConditionsToNodeConditionsTypePrefixing tests that non-standard conditions get prefixed
func TestConvertConditionsToNodeConditionsTypePrefixing(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name             string
		inputType        string
		shouldBePrefixed bool
	}{
		{
			name:             "Ready is not prefixed (standard)",
			inputType:        "Ready",
			shouldBePrefixed: false,
		},
		{
			name:             "MemoryPressure is not prefixed (standard)",
			inputType:        "MemoryPressure",
			shouldBePrefixed: false,
		},
		{
			name:             "DiskPressure is not prefixed (standard)",
			inputType:        "DiskPressure",
			shouldBePrefixed: false,
		},
		{
			name:             "PIDPressure is not prefixed (standard)",
			inputType:        "PIDPressure",
			shouldBePrefixed: false,
		},
		{
			name:             "NetworkUnavailable is not prefixed (standard)",
			inputType:        "NetworkUnavailable",
			shouldBePrefixed: false,
		},
		{
			name:             "CustomCondition is prefixed",
			inputType:        "CustomCondition",
			shouldBePrefixed: true,
		},
		{
			name:             "ServiceFailed is prefixed",
			inputType:        "ServiceFailed",
			shouldBePrefixed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conditions := []types.Condition{
				{
					Type:       tt.inputType,
					Status:     types.ConditionTrue,
					Transition: now,
					Reason:     "TestReason",
					Message:    "Test message",
				},
			}

			result := ConvertConditionsToNodeConditions(conditions)
			if len(result) != 1 {
				t.Fatalf("Expected 1 condition, got %d", len(result))
			}

			condType := string(result[0].Type)
			hasPrefix := strings.HasPrefix(condType, "NodeDoctor")

			if tt.shouldBePrefixed && !hasPrefix {
				t.Errorf("Expected condition type %q to be prefixed with NodeDoctor, got %q", tt.inputType, condType)
			}
			if !tt.shouldBePrefixed && hasPrefix {
				t.Errorf("Expected condition type %q to NOT be prefixed, got %q", tt.inputType, condType)
			}
		})
	}
}
