// Package kubernetes provides Kubernetes component health monitoring capabilities.
package kubernetes

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/supporttools/node-doctor/pkg/types"
)

// Integration tests for kubelet monitor metrics

// TestKubeletMonitor_MetricsInstrumentation tests that metrics are properly updated during checks
func TestKubeletMonitor_MetricsInstrumentation(t *testing.T) {
	registry := prometheus.NewRegistry()

	ctx := context.Background()
	config := types.MonitorConfig{
		Name:     "test-kubelet-metrics",
		Type:     "kubelet",
		Interval: 30 * time.Second,
		Timeout:  25 * time.Second,
	}

	// Create monitor with metrics
	monitor, err := NewKubeletMonitorWithMetrics(ctx, config, registry)
	if err != nil {
		t.Fatalf("Failed to create monitor with metrics: %v", err)
	}

	kubeletMonitor := monitor.(*KubeletMonitor)

	// Replace client with mock that will fail health checks
	kubeletMonitor.client = &mockKubeletClient{
		healthErr: fmt.Errorf("health check failed"),
	}

	// Run a check
	_, err = kubeletMonitor.checkKubelet(ctx)
	if err != nil {
		t.Fatalf("checkKubelet failed: %v", err)
	}

	// Verify metrics were updated
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	foundHealthCheck := false
	foundCheckResult := false
	foundCheckDuration := false

	for _, family := range families {
		switch family.GetName() {
		case "node_doctor_kubelet_monitor_health_check_duration_seconds":
			foundHealthCheck = true
		case "node_doctor_kubelet_monitor_checks_total":
			foundCheckResult = true
		case "node_doctor_kubelet_monitor_check_duration_seconds":
			foundCheckDuration = true
		}
	}

	if !foundHealthCheck {
		t.Error("Expected health check duration metric not found")
	}
	if !foundCheckResult {
		t.Error("Expected check result metric not found")
	}
	if !foundCheckDuration {
		t.Error("Expected check duration metric not found")
	}
}

// TestKubeletMonitor_MetricsWithNilRegistry tests that monitor works without metrics
func TestKubeletMonitor_MetricsWithNilRegistry(t *testing.T) {
	ctx := context.Background()
	config := types.MonitorConfig{
		Name:     "test-kubelet-no-metrics",
		Type:     "kubelet",
		Interval: 30 * time.Second,
		Timeout:  25 * time.Second,
	}

	// Create monitor without metrics (nil registry)
	monitor, err := NewKubeletMonitor(ctx, config)
	if err != nil {
		t.Fatalf("Failed to create monitor: %v", err)
	}

	kubeletMonitor := monitor.(*KubeletMonitor)

	// Should have nil metrics
	if kubeletMonitor.metrics != nil {
		t.Error("Expected nil metrics when no registry provided")
	}

	// Should still work without metrics
	_, err = kubeletMonitor.checkKubelet(ctx)
	if err != nil {
		t.Fatalf("checkKubelet failed without metrics: %v", err)
	}
}

// TestKubeletMonitor_CircuitBreakerMetrics tests that circuit breaker metrics are recorded
func TestKubeletMonitor_CircuitBreakerMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()

	ctx := context.Background()

	mockClient := &mockKubeletClient{
		healthErr: fmt.Errorf("connection refused"),
	}

	config := &KubeletMonitorConfig{
		HealthzURL:       "http://127.0.0.1:10248/healthz",
		MetricsURL:       "http://127.0.0.1:10250/metrics",
		FailureThreshold: 3,
		HTTPTimeout:      5 * time.Second,
		CircuitBreaker: &CircuitBreakerConfig{
			Enabled:             true,
			FailureThreshold:    2,
			OpenTimeout:         100 * time.Millisecond,
			HalfOpenMaxRequests: 2,
		},
	}

	// Create metrics
	metrics, err := NewKubeletMonitorMetrics(registry)
	if err != nil {
		t.Fatalf("Failed to create metrics: %v", err)
	}

	monitor := &KubeletMonitor{
		name:    "kubelet-circuit-test",
		config:  config,
		client:  mockClient,
		metrics: metrics,
	}

	// Initialize circuit breaker
	cb, err := NewCircuitBreaker(config.CircuitBreaker)
	if err != nil {
		t.Fatalf("Failed to create circuit breaker: %v", err)
	}
	monitor.circuitBreaker = cb

	// Perform checks that will open the circuit breaker
	monitor.checkKubelet(ctx)
	monitor.checkKubelet(ctx)

	// Verify circuit breaker metrics were recorded
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	foundMetrics := make(map[string]bool)
	for _, family := range families {
		foundMetrics[family.GetName()] = true
	}

	// Note: We only expect metrics that are actually recorded by the kubelet monitor implementation
	// The transitions_total metric is not currently recorded by the kubelet monitor
	expectedMetrics := []string{
		"node_doctor_kubelet_monitor_circuit_breaker_state",
		"node_doctor_kubelet_monitor_circuit_breaker_openings_total",
	}

	for _, expectedMetric := range expectedMetrics {
		if !foundMetrics[expectedMetric] {
			t.Errorf("Expected circuit breaker metric %s not found in registry", expectedMetric)
		}
	}
}
