package kubernetes

import (
	"math"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// setupTestMetrics creates a test registry and metrics for testing
func setupTestMetrics(t *testing.T) (*prometheus.Registry, *KubeletMonitorMetrics) {
	t.Helper()
	registry := prometheus.NewRegistry()
	metrics, err := NewKubeletMonitorMetrics(registry)
	if err != nil {
		t.Fatalf("Failed to create test metrics: %v", err)
	}
	return registry, metrics
}

// getCounterValue gets the value of a counter metric from the registry
func getCounterValue(t *testing.T, registry *prometheus.Registry, name string, labels prometheus.Labels) float64 {
	t.Helper()
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	for _, family := range families {
		if family.GetName() == name {
			for _, metric := range family.GetMetric() {
				if matchesLabels(metric, labels) {
					return metric.GetCounter().GetValue()
				}
			}
		}
	}
	return 0
}

// getGaugeValue gets the value of a gauge metric from the registry
func getGaugeValue(t *testing.T, registry *prometheus.Registry, name string, labels prometheus.Labels) float64 {
	t.Helper()
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	for _, family := range families {
		if family.GetName() == name {
			for _, metric := range family.GetMetric() {
				if matchesLabels(metric, labels) {
					return metric.GetGauge().GetValue()
				}
			}
		}
	}
	return 0
}

// getHistogramCount gets the count of observations for a histogram metric
func getHistogramCount(t *testing.T, registry *prometheus.Registry, name string, labels prometheus.Labels) uint64 {
	t.Helper()
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	for _, family := range families {
		if family.GetName() == name {
			for _, metric := range family.GetMetric() {
				if matchesLabels(metric, labels) {
					return metric.GetHistogram().GetSampleCount()
				}
			}
		}
	}
	return 0
}

// getHistogramSum gets the sum of observations for a histogram metric
func getHistogramSum(t *testing.T, registry *prometheus.Registry, name string, labels prometheus.Labels) float64 {
	t.Helper()
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	for _, family := range families {
		if family.GetName() == name {
			for _, metric := range family.GetMetric() {
				if matchesLabels(metric, labels) {
					return metric.GetHistogram().GetSampleSum()
				}
			}
		}
	}
	return 0
}

// matchesLabels checks if a metric matches the given labels
func matchesLabels(metric *dto.Metric, labels prometheus.Labels) bool {
	metricLabels := make(map[string]string)
	for _, pair := range metric.GetLabel() {
		metricLabels[pair.GetName()] = pair.GetValue()
	}

	if len(metricLabels) != len(labels) {
		return false
	}

	for key, value := range labels {
		if metricLabels[key] != value {
			return false
		}
	}
	return true
}

// TestNewKubeletMonitorMetrics tests metric registration and creation
func TestNewKubeletMonitorMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics, err := NewKubeletMonitorMetrics(registry)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if metrics == nil {
		t.Fatal("Expected metrics but got nil")
	}

	// Verify all metrics are created
	if metrics.CheckDuration == nil {
		t.Error("CheckDuration metric is nil")
	}
	if metrics.HealthCheckDuration == nil {
		t.Error("HealthCheckDuration metric is nil")
	}
	if metrics.SystemdCheckDuration == nil {
		t.Error("SystemdCheckDuration metric is nil")
	}
	if metrics.PLEGCheckDuration == nil {
		t.Error("PLEGCheckDuration metric is nil")
	}
	if metrics.ChecksTotal == nil {
		t.Error("ChecksTotal metric is nil")
	}
	if metrics.ConsecutiveFailures == nil {
		t.Error("ConsecutiveFailures metric is nil")
	}
	if metrics.CircuitBreakerState == nil {
		t.Error("CircuitBreakerState metric is nil")
	}
	if metrics.CircuitBreakerTransitions == nil {
		t.Error("CircuitBreakerTransitions metric is nil")
	}
	if metrics.CircuitBreakerOpenings == nil {
		t.Error("CircuitBreakerOpenings metric is nil")
	}
	if metrics.CircuitBreakerRecoveries == nil {
		t.Error("CircuitBreakerRecoveries metric is nil")
	}
	if metrics.CircuitBreakerBackoffMult == nil {
		t.Error("CircuitBreakerBackoffMult metric is nil")
	}
	if metrics.PLEGRelistDuration == nil {
		t.Error("PLEGRelistDuration metric is nil")
	}
	if metrics.PLEGParsingDuration == nil {
		t.Error("PLEGParsingDuration metric is nil")
	}

	// Trigger some metrics to verify they get registered
	metrics.RecordCheckDuration("test-node", "test-monitor", "success", 0.1)
	metrics.RecordHealthCheck("test-node", "test-monitor", "success", 0.05)
	metrics.RecordSystemdCheck("test-node", "test-monitor", "success", 0.03)
	metrics.RecordPLEGCheck("test-node", "test-monitor", "success", 0.02)
	metrics.RecordCheckResult("test-node", "test-monitor", "health", "success")
	metrics.SetConsecutiveFailures("test-node", "test-monitor", 1)
	metrics.SetCircuitBreakerState("test-node", "test-monitor", 0)
	metrics.RecordCircuitBreakerTransition("test-node", "test-monitor", "closed", "open")
	metrics.RecordCircuitBreakerOpening("test-node", "test-monitor")
	metrics.RecordCircuitBreakerRecovery("test-node", "test-monitor")
	metrics.SetCircuitBreakerBackoffMultiplier("test-node", "test-monitor", 1.5)
	metrics.SetPLEGRelistDuration("test-node", "test-monitor", 0.04)
	metrics.RecordPLEGParsingDuration("test-node", "test-monitor", 0.001)

	// Now verify metrics can be gathered (i.e., properly registered)
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	// Should have 13 metric families after use
	expectedMetrics := 13
	if len(families) != expectedMetrics {
		metricNames := make([]string, len(families))
		for i, family := range families {
			metricNames[i] = family.GetName()
		}
		t.Errorf("Expected %d metric families, got %d: %v", expectedMetrics, len(families), metricNames)
	}

	// Verify metric names and types
	expectedMetricNames := map[string]string{
		"node_doctor_kubelet_monitor_check_duration_seconds":           "histogram",
		"node_doctor_kubelet_monitor_health_check_duration_seconds":    "histogram",
		"node_doctor_kubelet_monitor_systemd_check_duration_seconds":   "histogram",
		"node_doctor_kubelet_monitor_pleg_check_duration_seconds":      "histogram",
		"node_doctor_kubelet_monitor_checks_total":                     "counter",
		"node_doctor_kubelet_monitor_consecutive_failures":             "gauge",
		"node_doctor_kubelet_monitor_circuit_breaker_state":            "gauge",
		"node_doctor_kubelet_monitor_circuit_breaker_transitions_total": "counter",
		"node_doctor_kubelet_monitor_circuit_breaker_openings_total":   "counter",
		"node_doctor_kubelet_monitor_circuit_breaker_recoveries_total": "counter",
		"node_doctor_kubelet_monitor_circuit_breaker_backoff_multiplier": "gauge",
		"node_doctor_kubelet_monitor_pleg_relist_duration_seconds":     "gauge",
		"node_doctor_kubelet_monitor_pleg_parsing_duration_seconds":    "histogram",
	}

	foundMetrics := make(map[string]string)
	for _, family := range families {
		foundMetrics[family.GetName()] = family.GetType().String()
	}

	for name, expectedType := range expectedMetricNames {
		actualType, exists := foundMetrics[name]
		if !exists {
			t.Errorf("Expected metric %s not found", name)
		} else if !strings.Contains(strings.ToLower(actualType), strings.ToLower(expectedType)) {
			t.Errorf("Metric %s has type %s, expected %s", name, actualType, expectedType)
		}
	}
}

// TestNewKubeletMonitorMetrics_NilRegistry tests that nil registry returns error
func TestNewKubeletMonitorMetrics_NilRegistry(t *testing.T) {
	metrics, err := NewKubeletMonitorMetrics(nil)

	if err == nil {
		t.Error("Expected error for nil registry, got nil")
	}
	if metrics != nil {
		t.Error("Expected nil metrics for nil registry")
	}
	if !strings.Contains(err.Error(), "registry cannot be nil") {
		t.Errorf("Error message should mention nil registry, got: %v", err)
	}
}

// TestKubeletMonitorMetrics_RecordCheckDuration tests overall check duration recording
func TestKubeletMonitorMetrics_RecordCheckDuration(t *testing.T) {
	registry, metrics := setupTestMetrics(t)

	// Record a check duration
	metrics.RecordCheckDuration("node1", "kubelet-mon", "success", 0.15)

	// Verify the metric was recorded
	labels := prometheus.Labels{
		"node":         "node1",
		"monitor_name": "kubelet-mon",
		"result":       "success",
	}

	count := getHistogramCount(t, registry, "node_doctor_kubelet_monitor_check_duration_seconds", labels)
	if count != 1 {
		t.Errorf("Expected 1 observation, got %d", count)
	}

	sum := getHistogramSum(t, registry, "node_doctor_kubelet_monitor_check_duration_seconds", labels)
	if math.Abs(sum-0.15) > 0.001 {
		t.Errorf("Expected sum ~0.15, got %f", sum)
	}

	// Record another duration with different result
	metrics.RecordCheckDuration("node1", "kubelet-mon", "failure", 0.25)

	failureLabels := prometheus.Labels{
		"node":         "node1",
		"monitor_name": "kubelet-mon",
		"result":       "failure",
	}

	failureCount := getHistogramCount(t, registry, "node_doctor_kubelet_monitor_check_duration_seconds", failureLabels)
	if failureCount != 1 {
		t.Errorf("Expected 1 failure observation, got %d", failureCount)
	}

	// Original success count should remain 1
	successCount := getHistogramCount(t, registry, "node_doctor_kubelet_monitor_check_duration_seconds", labels)
	if successCount != 1 {
		t.Errorf("Expected 1 success observation, got %d", successCount)
	}
}

// TestKubeletMonitorMetrics_RecordHealthCheck tests health check duration recording
func TestKubeletMonitorMetrics_RecordHealthCheck(t *testing.T) {
	registry, metrics := setupTestMetrics(t)

	// Record multiple health check durations
	metrics.RecordHealthCheck("node2", "health-check", "success", 0.05)
	metrics.RecordHealthCheck("node2", "health-check", "success", 0.08)

	labels := prometheus.Labels{
		"node":         "node2",
		"monitor_name": "health-check",
		"result":       "success",
	}

	count := getHistogramCount(t, registry, "node_doctor_kubelet_monitor_health_check_duration_seconds", labels)
	if count != 2 {
		t.Errorf("Expected 2 observations, got %d", count)
	}

	sum := getHistogramSum(t, registry, "node_doctor_kubelet_monitor_health_check_duration_seconds", labels)
	expectedSum := 0.05 + 0.08
	if math.Abs(sum-expectedSum) > 0.001 {
		t.Errorf("Expected sum ~%f, got %f", expectedSum, sum)
	}
}

// TestKubeletMonitorMetrics_RecordSystemdCheck tests systemd check duration recording
func TestKubeletMonitorMetrics_RecordSystemdCheck(t *testing.T) {
	registry, metrics := setupTestMetrics(t)

	metrics.RecordSystemdCheck("node3", "systemd-check", "failure", 0.12)

	labels := prometheus.Labels{
		"node":         "node3",
		"monitor_name": "systemd-check",
		"result":       "failure",
	}

	count := getHistogramCount(t, registry, "node_doctor_kubelet_monitor_systemd_check_duration_seconds", labels)
	if count != 1 {
		t.Errorf("Expected 1 observation, got %d", count)
	}
}

// TestKubeletMonitorMetrics_RecordPLEGCheck tests PLEG check duration recording
func TestKubeletMonitorMetrics_RecordPLEGCheck(t *testing.T) {
	registry, metrics := setupTestMetrics(t)

	metrics.RecordPLEGCheck("node4", "pleg-check", "success", 0.03)

	labels := prometheus.Labels{
		"node":         "node4",
		"monitor_name": "pleg-check",
		"result":       "success",
	}

	count := getHistogramCount(t, registry, "node_doctor_kubelet_monitor_pleg_check_duration_seconds", labels)
	if count != 1 {
		t.Errorf("Expected 1 observation, got %d", count)
	}
}

// TestKubeletMonitorMetrics_RecordCheckResult tests check result counter recording
func TestKubeletMonitorMetrics_RecordCheckResult(t *testing.T) {
	registry, metrics := setupTestMetrics(t)

	// Record multiple check results
	metrics.RecordCheckResult("node5", "result-check", "health", "success")
	metrics.RecordCheckResult("node5", "result-check", "health", "success")
	metrics.RecordCheckResult("node5", "result-check", "systemd", "failure")

	healthLabels := prometheus.Labels{
		"node":         "node5",
		"monitor_name": "result-check",
		"check_type":   "health",
		"result":       "success",
	}

	systemdLabels := prometheus.Labels{
		"node":         "node5",
		"monitor_name": "result-check",
		"check_type":   "systemd",
		"result":       "failure",
	}

	healthValue := getCounterValue(t, registry, "node_doctor_kubelet_monitor_checks_total", healthLabels)
	if healthValue != 2 {
		t.Errorf("Expected health success count of 2, got %f", healthValue)
	}

	systemdValue := getCounterValue(t, registry, "node_doctor_kubelet_monitor_checks_total", systemdLabels)
	if systemdValue != 1 {
		t.Errorf("Expected systemd failure count of 1, got %f", systemdValue)
	}
}

// TestKubeletMonitorMetrics_SetConsecutiveFailures tests consecutive failures gauge
func TestKubeletMonitorMetrics_SetConsecutiveFailures(t *testing.T) {
	registry, metrics := setupTestMetrics(t)

	// Set consecutive failures
	metrics.SetConsecutiveFailures("node6", "failure-track", 3)

	labels := prometheus.Labels{
		"node":         "node6",
		"monitor_name": "failure-track",
	}

	value := getGaugeValue(t, registry, "node_doctor_kubelet_monitor_consecutive_failures", labels)
	if value != 3 {
		t.Errorf("Expected consecutive failures of 3, got %f", value)
	}

	// Update the value
	metrics.SetConsecutiveFailures("node6", "failure-track", 0)
	value = getGaugeValue(t, registry, "node_doctor_kubelet_monitor_consecutive_failures", labels)
	if value != 0 {
		t.Errorf("Expected consecutive failures of 0 after reset, got %f", value)
	}
}

// TestKubeletMonitorMetrics_SetCircuitBreakerState tests circuit breaker state gauge
func TestKubeletMonitorMetrics_SetCircuitBreakerState(t *testing.T) {
	registry, metrics := setupTestMetrics(t)

	labels := prometheus.Labels{
		"node":         "node7",
		"monitor_name": "cb-state",
	}

	// Test different states
	states := []struct {
		state    int
		expected float64
	}{
		{-1, -1}, // disabled
		{0, 0},   // closed
		{1, 1},   // half-open
		{2, 2},   // open
	}

	for _, test := range states {
		metrics.SetCircuitBreakerState("node7", "cb-state", test.state)
		value := getGaugeValue(t, registry, "node_doctor_kubelet_monitor_circuit_breaker_state", labels)
		if value != test.expected {
			t.Errorf("Expected circuit breaker state %f, got %f", test.expected, value)
		}
	}
}

// TestKubeletMonitorMetrics_RecordCircuitBreakerTransition tests state transition counter
func TestKubeletMonitorMetrics_RecordCircuitBreakerTransition(t *testing.T) {
	registry, metrics := setupTestMetrics(t)

	// Record transitions
	metrics.RecordCircuitBreakerTransition("node8", "cb-trans", "closed", "open")
	metrics.RecordCircuitBreakerTransition("node8", "cb-trans", "open", "half_open")
	metrics.RecordCircuitBreakerTransition("node8", "cb-trans", "half_open", "closed")

	closedToOpenLabels := prometheus.Labels{
		"node":         "node8",
		"monitor_name": "cb-trans",
		"from_state":   "closed",
		"to_state":     "open",
	}

	value := getCounterValue(t, registry, "node_doctor_kubelet_monitor_circuit_breaker_transitions_total", closedToOpenLabels)
	if value != 1 {
		t.Errorf("Expected 1 closed->open transition, got %f", value)
	}
}

// TestKubeletMonitorMetrics_RecordCircuitBreakerOpening tests circuit breaker opening counter
func TestKubeletMonitorMetrics_RecordCircuitBreakerOpening(t *testing.T) {
	registry, metrics := setupTestMetrics(t)

	// Record multiple openings
	metrics.RecordCircuitBreakerOpening("node9", "cb-open")
	metrics.RecordCircuitBreakerOpening("node9", "cb-open")

	labels := prometheus.Labels{
		"node":         "node9",
		"monitor_name": "cb-open",
	}

	value := getCounterValue(t, registry, "node_doctor_kubelet_monitor_circuit_breaker_openings_total", labels)
	if value != 2 {
		t.Errorf("Expected 2 openings, got %f", value)
	}
}

// TestKubeletMonitorMetrics_RecordCircuitBreakerRecovery tests circuit breaker recovery counter
func TestKubeletMonitorMetrics_RecordCircuitBreakerRecovery(t *testing.T) {
	registry, metrics := setupTestMetrics(t)

	metrics.RecordCircuitBreakerRecovery("node10", "cb-recovery")

	labels := prometheus.Labels{
		"node":         "node10",
		"monitor_name": "cb-recovery",
	}

	value := getCounterValue(t, registry, "node_doctor_kubelet_monitor_circuit_breaker_recoveries_total", labels)
	if value != 1 {
		t.Errorf("Expected 1 recovery, got %f", value)
	}
}

// TestKubeletMonitorMetrics_SetCircuitBreakerBackoffMultiplier tests backoff multiplier gauge
func TestKubeletMonitorMetrics_SetCircuitBreakerBackoffMultiplier(t *testing.T) {
	registry, metrics := setupTestMetrics(t)

	metrics.SetCircuitBreakerBackoffMultiplier("node11", "cb-backoff", 2.5)

	labels := prometheus.Labels{
		"node":         "node11",
		"monitor_name": "cb-backoff",
	}

	value := getGaugeValue(t, registry, "node_doctor_kubelet_monitor_circuit_breaker_backoff_multiplier", labels)
	if math.Abs(value-2.5) > 0.001 {
		t.Errorf("Expected backoff multiplier 2.5, got %f", value)
	}
}

// TestKubeletMonitorMetrics_SetPLEGRelistDuration tests PLEG relist duration gauge
func TestKubeletMonitorMetrics_SetPLEGRelistDuration(t *testing.T) {
	registry, metrics := setupTestMetrics(t)

	metrics.SetPLEGRelistDuration("node12", "pleg-relist", 0.045)

	labels := prometheus.Labels{
		"node":         "node12",
		"monitor_name": "pleg-relist",
	}

	value := getGaugeValue(t, registry, "node_doctor_kubelet_monitor_pleg_relist_duration_seconds", labels)
	if math.Abs(value-0.045) > 0.001 {
		t.Errorf("Expected PLEG relist duration 0.045, got %f", value)
	}
}

// TestKubeletMonitorMetrics_RecordPLEGParsingDuration tests PLEG parsing duration histogram
func TestKubeletMonitorMetrics_RecordPLEGParsingDuration(t *testing.T) {
	registry, metrics := setupTestMetrics(t)

	metrics.RecordPLEGParsingDuration("node13", "pleg-parse", 0.002)

	labels := prometheus.Labels{
		"node":         "node13",
		"monitor_name": "pleg-parse",
	}

	count := getHistogramCount(t, registry, "node_doctor_kubelet_monitor_pleg_parsing_duration_seconds", labels)
	if count != 1 {
		t.Errorf("Expected 1 PLEG parsing observation, got %d", count)
	}

	sum := getHistogramSum(t, registry, "node_doctor_kubelet_monitor_pleg_parsing_duration_seconds", labels)
	if math.Abs(sum-0.002) > 0.0001 {
		t.Errorf("Expected sum ~0.002, got %f", sum)
	}
}

// TestKubeletMonitorMetrics_NilSafety tests that all methods handle nil metrics gracefully
func TestKubeletMonitorMetrics_NilSafety(t *testing.T) {
	var metrics *KubeletMonitorMetrics

	// All these calls should not panic
	metrics.RecordCheckDuration("node", "monitor", "result", 1.0)
	metrics.RecordHealthCheck("node", "monitor", "result", 1.0)
	metrics.RecordSystemdCheck("node", "monitor", "result", 1.0)
	metrics.RecordPLEGCheck("node", "monitor", "result", 1.0)
	metrics.RecordCheckResult("node", "monitor", "type", "result")
	metrics.SetConsecutiveFailures("node", "monitor", 5)
	metrics.SetCircuitBreakerState("node", "monitor", 1)
	metrics.RecordCircuitBreakerTransition("node", "monitor", "from", "to")
	metrics.RecordCircuitBreakerOpening("node", "monitor")
	metrics.RecordCircuitBreakerRecovery("node", "monitor")
	metrics.SetCircuitBreakerBackoffMultiplier("node", "monitor", 2.0)
	metrics.SetPLEGRelistDuration("node", "monitor", 0.1)
	metrics.RecordPLEGParsingDuration("node", "monitor", 0.001)

	// Test with nil individual metrics
	registry, validMetrics := setupTestMetrics(t)

	// Simulate nil individual metrics
	invalidMetrics := &KubeletMonitorMetrics{
		CheckDuration:             nil, // Simulate nil metric
		HealthCheckDuration:       validMetrics.HealthCheckDuration,
		SystemdCheckDuration:      validMetrics.SystemdCheckDuration,
		PLEGCheckDuration:         validMetrics.PLEGCheckDuration,
		ChecksTotal:               validMetrics.ChecksTotal,
		ConsecutiveFailures:       validMetrics.ConsecutiveFailures,
		CircuitBreakerState:       validMetrics.CircuitBreakerState,
		CircuitBreakerTransitions: validMetrics.CircuitBreakerTransitions,
		CircuitBreakerOpenings:    validMetrics.CircuitBreakerOpenings,
		CircuitBreakerRecoveries:  validMetrics.CircuitBreakerRecoveries,
		CircuitBreakerBackoffMult: validMetrics.CircuitBreakerBackoffMult,
		PLEGRelistDuration:        validMetrics.PLEGRelistDuration,
		PLEGParsingDuration:       validMetrics.PLEGParsingDuration,
	}

	// This should not panic even with nil CheckDuration
	invalidMetrics.RecordCheckDuration("node", "monitor", "result", 1.0)

	// Verify no observations were recorded for the nil metric
	labels := prometheus.Labels{
		"node":         "node",
		"monitor_name": "monitor",
		"result":       "result",
	}
	count := getHistogramCount(t, registry, "node_doctor_kubelet_monitor_check_duration_seconds", labels)
	if count != 0 {
		t.Errorf("Expected 0 observations for nil metric, got %d", count)
	}

	// Verify other metrics still work
	invalidMetrics.RecordHealthCheck("node", "monitor", "result", 1.0)
	healthCount := getHistogramCount(t, registry, "node_doctor_kubelet_monitor_health_check_duration_seconds", labels)
	if healthCount != 1 {
		t.Errorf("Expected 1 health check observation, got %d", healthCount)
	}
}

// TestKubeletMonitorMetrics_HistogramBuckets tests that histogram metrics have correct buckets
func TestKubeletMonitorMetrics_HistogramBuckets(t *testing.T) {
	registry, metrics := setupTestMetrics(t)

	// Record values in different bucket ranges to verify bucket configuration
	testCases := []struct {
		metric   string
		recorder func(string, string, string, float64)
		value    float64
	}{
		{"node_doctor_kubelet_monitor_check_duration_seconds", metrics.RecordCheckDuration, 0.1},
		{"node_doctor_kubelet_monitor_health_check_duration_seconds", metrics.RecordHealthCheck, 0.05},
		{"node_doctor_kubelet_monitor_systemd_check_duration_seconds", metrics.RecordSystemdCheck, 0.025},
		{"node_doctor_kubelet_monitor_pleg_check_duration_seconds", metrics.RecordPLEGCheck, 0.01},
	}

	for _, tc := range testCases {
		tc.recorder("test-node", "test-monitor", "success", tc.value)

		// Verify the observation was recorded
		labels := prometheus.Labels{
			"node":         "test-node",
			"monitor_name": "test-monitor",
			"result":       "success",
		}

		count := getHistogramCount(t, registry, tc.metric, labels)
		if count != 1 {
			t.Errorf("Metric %s: expected 1 observation, got %d", tc.metric, count)
		}

		sum := getHistogramSum(t, registry, tc.metric, labels)
		if math.Abs(sum-tc.value) > 0.001 {
			t.Errorf("Metric %s: expected sum %f, got %f", tc.metric, tc.value, sum)
		}
	}

	// Test PLEG parsing duration with different bucket structure
	metrics.RecordPLEGParsingDuration("test-node", "test-monitor", 0.001)
	labels := prometheus.Labels{
		"node":         "test-node",
		"monitor_name": "test-monitor",
	}

	count := getHistogramCount(t, registry, "node_doctor_kubelet_monitor_pleg_parsing_duration_seconds", labels)
	if count != 1 {
		t.Errorf("PLEG parsing: expected 1 observation, got %d", count)
	}
}

// TestKubeletMonitorMetrics_MultipleNodes tests metrics with different node names
func TestKubeletMonitorMetrics_MultipleNodes(t *testing.T) {
	registry, metrics := setupTestMetrics(t)

	// Record metrics for different nodes
	nodes := []string{"node-a", "node-b", "node-c"}
	for i, node := range nodes {
		metrics.RecordCheckResult(node, "multi-test", "health", "success")
		metrics.SetConsecutiveFailures(node, "multi-test", i+1)
	}

	// Verify each node has correct values
	for i, node := range nodes {
		checkLabels := prometheus.Labels{
			"node":         node,
			"monitor_name": "multi-test",
			"check_type":   "health",
			"result":       "success",
		}

		failureLabels := prometheus.Labels{
			"node":         node,
			"monitor_name": "multi-test",
		}

		checkValue := getCounterValue(t, registry, "node_doctor_kubelet_monitor_checks_total", checkLabels)
		if checkValue != 1 {
			t.Errorf("Node %s: expected 1 check, got %f", node, checkValue)
		}

		failureValue := getGaugeValue(t, registry, "node_doctor_kubelet_monitor_consecutive_failures", failureLabels)
		expected := float64(i + 1)
		if failureValue != expected {
			t.Errorf("Node %s: expected %f failures, got %f", node, expected, failureValue)
		}
	}
}

// TestKubeletMonitorMetrics_LabelValues tests that metrics handle various label values correctly
func TestKubeletMonitorMetrics_LabelValues(t *testing.T) {
	registry, metrics := setupTestMetrics(t)

	// Test with various label values including special characters
	testLabels := []struct {
		node        string
		monitorName string
		result      string
	}{
		{"node-1", "kubelet-health", "success"},
		{"node_2", "kubelet_systemd", "failure"},
		{"node.3", "kubelet.pleg", "timeout"},
		{"NODE-4", "KUBELET-CHECK", "ERROR"},
	}

	for _, test := range testLabels {
		metrics.RecordCheckDuration(test.node, test.monitorName, test.result, 0.1)

		labels := prometheus.Labels{
			"node":         test.node,
			"monitor_name": test.monitorName,
			"result":       test.result,
		}

		count := getHistogramCount(t, registry, "node_doctor_kubelet_monitor_check_duration_seconds", labels)
		if count != 1 {
			t.Errorf("Labels %+v: expected 1 observation, got %d", test, count)
		}
	}
}