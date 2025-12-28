package controller

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestNewEventRecorder(t *testing.T) {
	tests := []struct {
		name      string
		config    *EventRecorderConfig
		wantErr   bool
		wantNil   bool
		wantEnabled bool
	}{
		{
			name:    "nil config returns error",
			config:  nil,
			wantErr: true,
			wantNil: true,
		},
		{
			name: "disabled recorder",
			config: &EventRecorderConfig{
				Enabled:   false,
				Namespace: "test",
			},
			wantErr:     false,
			wantNil:     false,
			wantEnabled: false,
		},
		{
			name: "enabled but no kubeconfig",
			config: &EventRecorderConfig{
				Enabled:   true,
				Namespace: "test",
				InCluster: false,
			},
			wantErr:     false,
			wantNil:     false,
			wantEnabled: false, // Should be disabled because no config available
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder, err := NewEventRecorder(tt.config)

			if (err != nil) != tt.wantErr {
				t.Errorf("NewEventRecorder() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if (recorder == nil) != tt.wantNil {
				t.Errorf("NewEventRecorder() recorder = %v, wantNil %v", recorder, tt.wantNil)
				return
			}

			if recorder != nil && recorder.IsEnabled() != tt.wantEnabled {
				t.Errorf("NewEventRecorder() enabled = %v, want %v", recorder.IsEnabled(), tt.wantEnabled)
			}
		})
	}
}

func TestEventRecorder_IsEnabled(t *testing.T) {
	// Disabled recorder
	recorder := &EventRecorder{
		enabled: false,
		client:  nil,
	}
	if recorder.IsEnabled() {
		t.Error("expected disabled recorder to return false")
	}

	// Enabled but no client
	recorder = &EventRecorder{
		enabled: true,
		client:  nil,
	}
	if recorder.IsEnabled() {
		t.Error("expected recorder with nil client to return false")
	}

	// Enabled with client
	recorder = &EventRecorder{
		enabled: true,
		client:  fake.NewSimpleClientset(),
	}
	if !recorder.IsEnabled() {
		t.Error("expected enabled recorder with client to return true")
	}
}

func TestEventRecorder_RateLimiting(t *testing.T) {
	recorder := &EventRecorder{
		enabled:         true,
		client:          fake.NewSimpleClientset(),
		namespace:       "test",
		lastEventTime:   make(map[string]time.Time),
		rateLimitPeriod: 1 * time.Second,
	}

	// First call should not be rate limited
	if recorder.shouldRateLimit("test-key") {
		t.Error("first call should not be rate limited")
	}

	// Immediate second call should be rate limited
	if !recorder.shouldRateLimit("test-key") {
		t.Error("immediate second call should be rate limited")
	}

	// Different key should not be rate limited
	if recorder.shouldRateLimit("different-key") {
		t.Error("different key should not be rate limited")
	}

	// Wait for rate limit period
	time.Sleep(1100 * time.Millisecond)

	// Should no longer be rate limited
	if recorder.shouldRateLimit("test-key") {
		t.Error("should not be rate limited after period expires")
	}
}

func TestEventRecorder_CleanupRateLimitCache(t *testing.T) {
	recorder := &EventRecorder{
		enabled:         true,
		client:          fake.NewSimpleClientset(),
		namespace:       "test",
		lastEventTime:   make(map[string]time.Time),
		rateLimitPeriod: 100 * time.Millisecond,
	}

	// Add some entries
	recorder.lastEventTime["old-key"] = time.Now().Add(-1 * time.Hour)
	recorder.lastEventTime["recent-key"] = time.Now()

	recorder.cleanupRateLimitCache()

	// Old entry should be removed
	if _, exists := recorder.lastEventTime["old-key"]; exists {
		t.Error("old key should have been cleaned up")
	}

	// Recent entry should remain
	if _, exists := recorder.lastEventTime["recent-key"]; !exists {
		t.Error("recent key should not have been cleaned up")
	}
}

func TestEventRecorder_RecordClusterHealthChange(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name            string
		status          *ClusterStatus
		previousHealth  HealthStatus
		expectEvent     bool
		expectedReason  string
	}{
		{
			name: "critical cluster creates event",
			status: &ClusterStatus{
				OverallHealth:  HealthStatusCritical,
				TotalNodes:     10,
				CriticalNodes:  3,
				DegradedNodes:  2,
			},
			previousHealth: HealthStatusDegraded,
			expectEvent:    true,
			expectedReason: EventReasonClusterCritical,
		},
		{
			name: "degraded cluster creates event",
			status: &ClusterStatus{
				OverallHealth:  HealthStatusDegraded,
				TotalNodes:     10,
				DegradedNodes:  3,
				HealthyNodes:   7,
			},
			previousHealth: HealthStatusHealthy,
			expectEvent:    true,
			expectedReason: EventReasonClusterDegraded,
		},
		{
			name: "recovery creates event",
			status: &ClusterStatus{
				OverallHealth:  HealthStatusHealthy,
				TotalNodes:     10,
				HealthyNodes:   10,
			},
			previousHealth: HealthStatusCritical,
			expectEvent:    true,
			expectedReason: EventReasonClusterRecovered,
		},
		{
			name: "healthy to healthy no event",
			status: &ClusterStatus{
				OverallHealth:  HealthStatusHealthy,
				TotalNodes:     10,
				HealthyNodes:   10,
			},
			previousHealth: HealthStatusHealthy,
			expectEvent:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fresh client and recorder for each subtest
			fakeClient := fake.NewSimpleClientset()
			recorder := &EventRecorder{
				enabled:         true,
				client:          fakeClient,
				namespace:       "node-doctor",
				controllerName:  "node-doctor-controller",
				componentName:   "node-doctor",
				lastEventTime:   make(map[string]time.Time),
				rateLimitPeriod: 5 * time.Minute,
			}

			recorder.RecordClusterHealthChange(ctx, tt.status, tt.previousHealth)

			events, _ := fakeClient.CoreV1().Events("node-doctor").List(ctx, metav1.ListOptions{})

			if tt.expectEvent && len(events.Items) == 0 {
				t.Error("expected event to be created")
			}

			if !tt.expectEvent && len(events.Items) > 0 {
				t.Errorf("expected no event, got %d", len(events.Items))
			}

			if tt.expectEvent && len(events.Items) > 0 {
				if events.Items[0].Reason != tt.expectedReason {
					t.Errorf("expected reason %s, got %s", tt.expectedReason, events.Items[0].Reason)
				}
			}
		})
	}
}

func TestEventRecorder_RecordClusterWideProblem(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	recorder := &EventRecorder{
		enabled:         true,
		client:          fakeClient,
		namespace:       "node-doctor",
		lastEventTime:   make(map[string]time.Time),
		rateLimitPeriod: 5 * time.Minute,
	}

	ctx := context.Background()

	problem := &ClusterProblem{
		ID:            "test-problem",
		Type:          "dns",
		Severity:      "critical",
		AffectedNodes: []string{"node-1", "node-2", "node-3"},
		Message:       "DNS resolution failing across multiple nodes",
	}

	recorder.RecordClusterWideProblem(ctx, problem)

	events, _ := fakeClient.CoreV1().Events("node-doctor").List(ctx, metav1.ListOptions{})

	if len(events.Items) != 1 {
		t.Errorf("expected 1 event, got %d", len(events.Items))
		return
	}

	event := events.Items[0]
	if event.Reason != EventReasonClusterWideProblem {
		t.Errorf("expected reason %s, got %s", EventReasonClusterWideProblem, event.Reason)
	}

	if event.Type != EventTypeWarning {
		t.Errorf("expected type %s, got %s", EventTypeWarning, event.Type)
	}
}

func TestEventRecorder_RecordCorrelation(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	recorder := &EventRecorder{
		enabled:         true,
		client:          fakeClient,
		namespace:       "node-doctor",
		lastEventTime:   make(map[string]time.Time),
		rateLimitPeriod: 5 * time.Minute,
	}

	ctx := context.Background()

	correlation := &Correlation{
		ID:            "corr-1",
		Type:          "infrastructure",
		AffectedNodes: []string{"node-1", "node-2"},
		Message:       "Common infrastructure issue detected",
		Confidence:    0.95,
	}

	recorder.RecordCorrelation(ctx, correlation)

	events, _ := fakeClient.CoreV1().Events("node-doctor").List(ctx, metav1.ListOptions{})

	if len(events.Items) != 1 {
		t.Errorf("expected 1 event, got %d", len(events.Items))
		return
	}

	if events.Items[0].Reason != EventReasonCorrelationDetected {
		t.Errorf("expected reason %s, got %s", EventReasonCorrelationDetected, events.Items[0].Reason)
	}
}

func TestEventRecorder_RecordNodeHealthChange(t *testing.T) {
	ctx := context.Background()

	t.Run("node going critical creates event", func(t *testing.T) {
		fakeClient := fake.NewSimpleClientset()
		recorder := &EventRecorder{
			enabled:         true,
			client:          fakeClient,
			namespace:       "node-doctor",
			lastEventTime:   make(map[string]time.Time),
			rateLimitPeriod: 5 * time.Minute,
		}

		recorder.RecordNodeHealthChange(ctx, "worker-1", HealthStatusHealthy, HealthStatusCritical)

		events, _ := fakeClient.CoreV1().Events("node-doctor").List(ctx, metav1.ListOptions{})

		if len(events.Items) != 1 {
			t.Errorf("expected 1 event for critical transition, got %d", len(events.Items))
			return
		}

		event := events.Items[0]
		if event.InvolvedObject.Kind != "Node" {
			t.Errorf("expected event to reference Node, got %s", event.InvolvedObject.Kind)
		}
		if event.InvolvedObject.Name != "worker-1" {
			t.Errorf("expected event to reference worker-1, got %s", event.InvolvedObject.Name)
		}
	})

	t.Run("non-critical changes no event", func(t *testing.T) {
		fakeClient := fake.NewSimpleClientset()
		recorder := &EventRecorder{
			enabled:         true,
			client:          fakeClient,
			namespace:       "node-doctor",
			lastEventTime:   make(map[string]time.Time),
			rateLimitPeriod: 5 * time.Minute,
		}

		recorder.RecordNodeHealthChange(ctx, "worker-2", HealthStatusHealthy, HealthStatusDegraded)

		events, _ := fakeClient.CoreV1().Events("node-doctor").List(ctx, metav1.ListOptions{})
		if len(events.Items) != 0 {
			t.Errorf("expected no event for non-critical transition, got %d", len(events.Items))
		}
	})
}

func TestEventRecorder_RecordLeaseEvents(t *testing.T) {
	ctx := context.Background()

	t.Run("lease granted", func(t *testing.T) {
		fakeClient := fake.NewSimpleClientset()
		recorder := &EventRecorder{
			enabled:         true,
			client:          fakeClient,
			namespace:       "node-doctor",
			lastEventTime:   make(map[string]time.Time),
			rateLimitPeriod: 5 * time.Minute,
		}

		lease := &Lease{
			ID:              "lease-1",
			NodeName:        "worker-1",
			RemediationType: "restart-kubelet",
			ExpiresAt:       time.Now().Add(5 * time.Minute),
		}

		recorder.RecordLeaseGranted(ctx, lease)

		events, _ := fakeClient.CoreV1().Events("node-doctor").List(ctx, metav1.ListOptions{})

		if len(events.Items) != 1 {
			t.Errorf("expected 1 event for lease granted, got %d", len(events.Items))
			return
		}

		if events.Items[0].Reason != EventReasonLeaseGranted {
			t.Errorf("expected reason %s, got %s", EventReasonLeaseGranted, events.Items[0].Reason)
		}
	})

	t.Run("lease denied", func(t *testing.T) {
		fakeClient := fake.NewSimpleClientset()
		recorder := &EventRecorder{
			enabled:         true,
			client:          fakeClient,
			namespace:       "node-doctor",
			lastEventTime:   make(map[string]time.Time),
			rateLimitPeriod: 5 * time.Minute,
		}

		recorder.RecordLeaseDenied(ctx, "worker-2", "restart-kubelet", "max concurrent reached")

		events, _ := fakeClient.CoreV1().Events("node-doctor").List(ctx, metav1.ListOptions{})

		if len(events.Items) != 1 {
			t.Errorf("expected 1 event for lease denied, got %d", len(events.Items))
			return
		}

		if events.Items[0].Reason != EventReasonLeaseDenied {
			t.Errorf("expected reason %s, got %s", EventReasonLeaseDenied, events.Items[0].Reason)
		}

		if events.Items[0].Type != EventTypeWarning {
			t.Errorf("expected type %s, got %s", EventTypeWarning, events.Items[0].Type)
		}
	})
}

func TestEventRecorder_RecordProblemDetected(t *testing.T) {
	ctx := context.Background()

	t.Run("critical problem creates event", func(t *testing.T) {
		fakeClient := fake.NewSimpleClientset()
		recorder := &EventRecorder{
			enabled:         true,
			client:          fakeClient,
			namespace:       "node-doctor",
			lastEventTime:   make(map[string]time.Time),
			rateLimitPeriod: 5 * time.Minute,
		}

		criticalProblem := &ProblemSummary{
			Type:     "kubelet",
			Severity: "critical",
			Message:  "Kubelet not responding",
		}

		recorder.RecordProblemDetected(ctx, "worker-1", criticalProblem)

		events, _ := fakeClient.CoreV1().Events("node-doctor").List(ctx, metav1.ListOptions{})

		if len(events.Items) != 1 {
			t.Errorf("expected 1 event for critical problem, got %d", len(events.Items))
		}
	})

	t.Run("warning problem no event", func(t *testing.T) {
		fakeClient := fake.NewSimpleClientset()
		recorder := &EventRecorder{
			enabled:         true,
			client:          fakeClient,
			namespace:       "node-doctor",
			lastEventTime:   make(map[string]time.Time),
			rateLimitPeriod: 5 * time.Minute,
		}

		warningProblem := &ProblemSummary{
			Type:     "disk",
			Severity: "warning",
			Message:  "Disk usage high",
		}

		recorder.RecordProblemDetected(ctx, "worker-1", warningProblem)

		events, _ := fakeClient.CoreV1().Events("node-doctor").List(ctx, metav1.ListOptions{})

		if len(events.Items) != 0 {
			t.Errorf("expected no event for warning problem, got %d", len(events.Items))
		}
	})
}

func TestEventRecorder_DisabledRecorder(t *testing.T) {
	recorder := &EventRecorder{
		enabled:       false,
		client:        nil,
		lastEventTime: make(map[string]time.Time),
	}

	ctx := context.Background()

	// All methods should be no-ops when disabled
	recorder.RecordClusterHealthChange(ctx, &ClusterStatus{}, HealthStatusHealthy)
	recorder.RecordClusterWideProblem(ctx, &ClusterProblem{})
	recorder.RecordCorrelation(ctx, &Correlation{})
	recorder.RecordNodeHealthChange(ctx, "node-1", HealthStatusHealthy, HealthStatusCritical)
	recorder.RecordLeaseGranted(ctx, &Lease{})
	recorder.RecordLeaseDenied(ctx, "node-1", "restart-kubelet", "reason")
	recorder.RecordProblemDetected(ctx, "node-1", &ProblemSummary{})

	// No panics or errors expected
}
