package prometheus

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestNewMetrics(t *testing.T) {
	tests := []struct {
		name        string
		namespace   string
		subsystem   string
		constLabels prometheus.Labels
	}{
		{
			name:        "with namespace and subsystem",
			namespace:   "test_namespace",
			subsystem:   "test_subsystem",
			constLabels: prometheus.Labels{"env": "test"},
		},
		{
			name:        "with empty namespace (should use default)",
			namespace:   "",
			subsystem:   "test_subsystem",
			constLabels: prometheus.Labels{"env": "test"},
		},
		{
			name:        "with empty subsystem",
			namespace:   "test_namespace",
			subsystem:   "",
			constLabels: prometheus.Labels{"env": "test"},
		},
		{
			name:        "with nil constLabels",
			namespace:   "test_namespace",
			subsystem:   "test_subsystem",
			constLabels: nil,
		},
		{
			name:        "minimal configuration",
			namespace:   "",
			subsystem:   "",
			constLabels: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics, err := NewMetrics(tt.namespace, tt.subsystem, tt.constLabels)
			if err != nil {
				t.Errorf("NewMetrics() error = %v", err)
				return
			}

			if metrics == nil {
				t.Errorf("NewMetrics() returned nil")
				return
			}

			// Verify all metrics were created
			if metrics.ProblemsTotal == nil {
				t.Error("ProblemsTotal metric not created")
			}
			if metrics.StatusUpdatesTotal == nil {
				t.Error("StatusUpdatesTotal metric not created")
			}
			if metrics.EventsTotal == nil {
				t.Error("EventsTotal metric not created")
			}
			if metrics.ConditionsTotal == nil {
				t.Error("ConditionsTotal metric not created")
			}
			if metrics.ExportOperationsTotal == nil {
				t.Error("ExportOperationsTotal metric not created")
			}
			if metrics.ExportErrorsTotal == nil {
				t.Error("ExportErrorsTotal metric not created")
			}
			if metrics.ProblemsActive == nil {
				t.Error("ProblemsActive metric not created")
			}
			if metrics.MonitorUp == nil {
				t.Error("MonitorUp metric not created")
			}
			if metrics.Info == nil {
				t.Error("Info metric not created")
			}
			if metrics.StartTimeSeconds == nil {
				t.Error("StartTimeSeconds metric not created")
			}
			if metrics.UptimeSeconds == nil {
				t.Error("UptimeSeconds metric not created")
			}
			if metrics.MonitorCheckDuration == nil {
				t.Error("MonitorCheckDuration metric not created")
			}
			if metrics.ExportDuration == nil {
				t.Error("ExportDuration metric not created")
			}
		})
	}
}

func TestMetricsRegister(t *testing.T) {
	registry := prometheus.NewRegistry()
	constLabels := prometheus.Labels{"env": "test"}

	metrics, err := NewMetrics("test", "", constLabels)
	if err != nil {
		t.Fatalf("failed to create metrics: %v", err)
	}

	// Test successful registration
	err = metrics.Register(registry)
	if err != nil {
		t.Errorf("failed to register metrics: %v", err)
	}

	// Test double registration (should fail)
	err = metrics.Register(registry)
	if err == nil {
		t.Errorf("expected error when registering metrics twice")
	}
}

func TestMetricsUnregister(t *testing.T) {
	registry := prometheus.NewRegistry()
	constLabels := prometheus.Labels{"env": "test"}

	metrics, err := NewMetrics("test", "", constLabels)
	if err != nil {
		t.Fatalf("failed to create metrics: %v", err)
	}

	// Register metrics
	err = metrics.Register(registry)
	if err != nil {
		t.Fatalf("failed to register metrics: %v", err)
	}

	// Unregister metrics
	metrics.Unregister(registry)

	// Should be able to register again after unregistering
	err = metrics.Register(registry)
	if err != nil {
		t.Errorf("failed to re-register metrics after unregistering: %v", err)
	}
}

func TestMetricUpdates(t *testing.T) {
	registry := prometheus.NewRegistry()
	constLabels := prometheus.Labels{"env": "test"}

	metrics, err := NewMetrics("test", "", constLabels)
	if err != nil {
		t.Fatalf("failed to create metrics: %v", err)
	}

	err = metrics.Register(registry)
	if err != nil {
		t.Fatalf("failed to register metrics: %v", err)
	}

	// Test counter metrics
	metrics.ProblemsTotal.WithLabelValues("test-node", "DiskPressure", "warning", "test-source").Inc()
	metrics.StatusUpdatesTotal.WithLabelValues("test-node", "test-source").Inc()
	metrics.EventsTotal.WithLabelValues("test-node", "test-source", "warning").Inc()
	metrics.ConditionsTotal.WithLabelValues("test-node", "Ready", "True").Inc()
	metrics.ExportOperationsTotal.WithLabelValues("test-node", "prometheus", "status", "success").Inc()
	metrics.ExportErrorsTotal.WithLabelValues("test-node", "prometheus", "timeout").Inc()

	// Test gauge metrics
	metrics.ProblemsActive.WithLabelValues("test-node", "DiskPressure", "warning").Set(5)
	metrics.MonitorUp.WithLabelValues("test-node", "disk-monitor", "disk").Set(1)
	metrics.Info.WithLabelValues("test-node", "1.0.0", "abc123", "go1.21", "2023-01-01").Set(1)
	metrics.StartTimeSeconds.WithLabelValues("test-node").Set(1640995200)
	metrics.UptimeSeconds.WithLabelValues("test-node").Set(3600)

	// Test histogram metrics
	timer := prometheus.NewTimer(metrics.MonitorCheckDuration.WithLabelValues("test-node", "disk-monitor"))
	timer.ObserveDuration()

	timer2 := prometheus.NewTimer(metrics.ExportDuration.WithLabelValues("test-node", "prometheus", "status"))
	timer2.ObserveDuration()

	// Gather metrics to verify they were updated
	metricFamilies, err := registry.Gather()
	if err != nil {
		t.Errorf("failed to gather metrics: %v", err)
	}

	if len(metricFamilies) == 0 {
		t.Error("no metrics were gathered")
	}

	// Verify we have expected metrics
	foundMetrics := make(map[string]bool)
	for _, mf := range metricFamilies {
		foundMetrics[*mf.Name] = true
	}

	expectedMetrics := []string{
		"test_problems_total",
		"test_status_updates_total",
		"test_events_total",
		"test_conditions_total",
		"test_export_operations_total",
		"test_export_errors_total",
		"test_problems_active",
		"test_monitor_up",
		"test_info",
		"test_start_time_seconds",
		"test_uptime_seconds",
		"test_monitor_check_duration_seconds",
		"test_export_duration_seconds",
	}

	for _, expectedMetric := range expectedMetrics {
		if !foundMetrics[expectedMetric] {
			t.Errorf("expected metric %s not found", expectedMetric)
		}
	}
}

func TestMetricLabels(t *testing.T) {
	registry := prometheus.NewRegistry()
	constLabels := prometheus.Labels{"env": "test", "cluster": "test-cluster"}

	metrics, err := NewMetrics("test", "", constLabels)
	if err != nil {
		t.Fatalf("failed to create metrics: %v", err)
	}

	err = metrics.Register(registry)
	if err != nil {
		t.Fatalf("failed to register metrics: %v", err)
	}

	// Update a metric with labels
	metrics.ProblemsTotal.WithLabelValues("test-node", "DiskPressure", "critical", "disk-monitor").Inc()

	// Gather and verify labels
	metricFamilies, err := registry.Gather()
	if err != nil {
		t.Errorf("failed to gather metrics: %v", err)
	}

	var problemsMetric *dto.MetricFamily
	for _, mf := range metricFamilies {
		if *mf.Name == "test_problems_total" {
			problemsMetric = mf
			break
		}
	}

	if problemsMetric == nil {
		t.Fatal("problems_total metric not found")
	}

	if len(problemsMetric.Metric) == 0 {
		t.Fatal("no metric samples found")
	}

	metric := problemsMetric.Metric[0]
	labelMap := make(map[string]string)
	for _, label := range metric.Label {
		labelMap[*label.Name] = *label.Value
	}

	// Verify constant labels
	if labelMap["env"] != "test" {
		t.Errorf("expected env label to be 'test', got '%s'", labelMap["env"])
	}
	if labelMap["cluster"] != "test-cluster" {
		t.Errorf("expected cluster label to be 'test-cluster', got '%s'", labelMap["cluster"])
	}

	// Verify variable labels
	if labelMap["node"] != "test-node" {
		t.Errorf("expected node label to be 'test-node', got '%s'", labelMap["node"])
	}
	if labelMap["problem_type"] != "DiskPressure" {
		t.Errorf("expected problem_type label to be 'DiskPressure', got '%s'", labelMap["problem_type"])
	}
	if labelMap["severity"] != "critical" {
		t.Errorf("expected severity label to be 'critical', got '%s'", labelMap["severity"])
	}
	if labelMap["source"] != "disk-monitor" {
		t.Errorf("expected source label to be 'disk-monitor', got '%s'", labelMap["source"])
	}
}

func TestMetricsReset(t *testing.T) {
	registry := prometheus.NewRegistry()
	constLabels := prometheus.Labels{"env": "test"}

	metrics, err := NewMetrics("test", "", constLabels)
	if err != nil {
		t.Fatalf("failed to create metrics: %v", err)
	}

	err = metrics.Register(registry)
	if err != nil {
		t.Fatalf("failed to register metrics: %v", err)
	}

	// Set some gauge values
	metrics.ProblemsActive.WithLabelValues("test-node", "DiskPressure", "warning").Set(5)
	metrics.ProblemsActive.WithLabelValues("test-node", "MemoryPressure", "critical").Set(3)
	metrics.MonitorUp.WithLabelValues("test-node", "disk-monitor", "disk").Set(1)

	// Reset ProblemsActive gauge
	metrics.ProblemsActive.Reset()

	// Gather metrics
	metricFamilies, err := registry.Gather()
	if err != nil {
		t.Errorf("failed to gather metrics: %v", err)
	}

	// Find the problems_active metric
	var problemsActiveMetric *dto.MetricFamily
	for _, mf := range metricFamilies {
		if *mf.Name == "test_problems_active" {
			problemsActiveMetric = mf
			break
		}
	}

	// After reset, the metric should have no samples (or zero values)
	if problemsActiveMetric != nil && len(problemsActiveMetric.Metric) > 0 {
		for _, metric := range problemsActiveMetric.Metric {
			if metric.Gauge != nil && *metric.Gauge.Value != 0 {
				t.Errorf("expected gauge value to be 0 after reset, got %f", *metric.Gauge.Value)
			}
		}
	}

	// MonitorUp should still have its value
	var monitorUpMetric *dto.MetricFamily
	for _, mf := range metricFamilies {
		if *mf.Name == "test_monitor_up" {
			monitorUpMetric = mf
			break
		}
	}

	if monitorUpMetric == nil || len(monitorUpMetric.Metric) == 0 {
		t.Error("monitor_up metric should still exist after ProblemsActive reset")
	}
}
