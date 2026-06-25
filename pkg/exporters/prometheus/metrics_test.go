package prometheus

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/supporttools/node-doctor/pkg/types"
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
			if metrics.ConditionStatus == nil {
				t.Error("ConditionStatus metric not created")
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
			if metrics.MonitorCyclesTotal == nil {
				t.Error("MonitorCyclesTotal metric not created")
			}
			if metrics.MonitorCycleLastTimestamp == nil {
				t.Error("MonitorCycleLastTimestamp metric not created")
			}
			if metrics.ExporterHealthy == nil {
				t.Error("ExporterHealthy metric not created")
			}
			if metrics.ExporterLastSuccessTimestamp == nil {
				t.Error("ExporterLastSuccessTimestamp metric not created")
			}
			if metrics.ExporterConsecutiveFailures == nil {
				t.Error("ExporterConsecutiveFailures metric not created")
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
	metrics.ConditionStatus.WithLabelValues("test-node", "NetworkPartitioned").Set(1)
	metrics.ConditionStatus.WithLabelValues("test-node", "CNIHealthy").Set(0)
	metrics.Info.WithLabelValues("test-node", "1.0.0", "abc123", "go1.21", "2023-01-01").Set(1)
	metrics.StartTimeSeconds.WithLabelValues("test-node").Set(1640995200)
	metrics.UptimeSeconds.WithLabelValues("test-node").Set(3600)

	// Test histogram metrics
	timer := prometheus.NewTimer(metrics.MonitorCheckDuration.WithLabelValues("test-node", "disk-monitor"))
	timer.ObserveDuration()

	timer2 := prometheus.NewTimer(metrics.ExportDuration.WithLabelValues("test-node", "prometheus", "status"))
	timer2.ObserveDuration()

	// Monitor-cycle self-metrics
	metrics.MonitorCyclesTotal.WithLabelValues("test-node", "disk-monitor", "success").Inc()
	metrics.MonitorCycleLastTimestamp.WithLabelValues("test-node", "disk-monitor").Set(1640995200)

	// Exporter-health self-metrics
	metrics.ExporterHealthy.WithLabelValues("test-node", "prometheus").Set(1)
	metrics.ExporterLastSuccessTimestamp.WithLabelValues("test-node", "prometheus").Set(1640995200)
	metrics.ExporterConsecutiveFailures.WithLabelValues("test-node", "prometheus").Set(0)

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
		"test_condition_status",
		"test_info",
		"test_start_time_seconds",
		"test_uptime_seconds",
		"test_monitor_check_duration_seconds",
		"test_export_duration_seconds",
		"test_monitor_cycles_total",
		"test_monitor_cycle_last_timestamp_seconds",
		"test_exporter_healthy",
		"test_exporter_last_success_timestamp_seconds",
		"test_exporter_consecutive_failures",
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

func TestFamilyLabel(t *testing.T) {
	cases := map[string]string{
		"ipv4":      "ipv4",
		"ipv6":      "ipv6",
		"":          "unknown",
		"IPv4":      "unknown", // case-sensitive: only exact "ipv4"/"ipv6" pass through
		"dualstack": "unknown",
	}
	for in, want := range cases {
		if got := familyLabel(in); got != want {
			t.Errorf("familyLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

// findLabelValue returns the value of the named label on the first sample of the
// metric family with the given name, or "" if not found.
func findLabelValue(t *testing.T, families []*dto.MetricFamily, metricName, labelName string) (string, bool) {
	t.Helper()
	for _, mf := range families {
		if mf.GetName() != metricName {
			continue
		}
		for _, metric := range mf.Metric {
			for _, label := range metric.Label {
				if label.GetName() == labelName {
					return label.GetValue(), true
				}
			}
		}
	}
	return "", false
}

func TestAddressFamilyLabelEmitted(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics, err := NewMetrics("test", "", nil)
	if err != nil {
		t.Fatalf("failed to create metrics: %v", err)
	}
	if err := metrics.Register(registry); err != nil {
		t.Fatalf("failed to register metrics: %v", err)
	}

	e := &PrometheusExporter{
		nodeName: "test-node",
		registry: registry,
		metrics:  metrics,
	}

	status := (&types.Status{Source: "test"}).SetLatencyMetrics(&types.LatencyMetrics{
		Gateway: &types.GatewayLatency{
			GatewayIP:     "10.0.0.1",
			LatencyMs:     1.0,
			AddressFamily: "ipv4",
		},
		Peers: []types.PeerLatency{
			{
				PeerNode:      "peer-v6",
				PeerIP:        "fd00::1",
				LatencyMs:     2.0,
				AvgLatencyMs:  2.0,
				Reachable:     true,
				AddressFamily: "ipv6",
			},
			{
				PeerNode:     "peer-unknown",
				PeerIP:       "10.0.0.9",
				LatencyMs:    3.0,
				AvgLatencyMs: 3.0,
				Reachable:    true,
				// AddressFamily intentionally empty -> "unknown"
			},
		},
		DNS: []types.DNSLatency{
			{
				DNSServer:     "8.8.8.8",
				Domain:        "example.com",
				RecordType:    "AAAA",
				DomainType:    "external",
				LatencyMs:     4.0,
				Success:       true,
				AddressFamily: "ipv6",
			},
		},
	})

	e.recordLatencyMetrics(status)

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	checks := []struct {
		metric string
		want   string
	}{
		{"test_gateway_latency_seconds", "ipv4"},
		{"test_peer_latency_seconds", ""}, // multiple series; checked below
		{"test_dns_latency_seconds", "ipv6"},
	}
	// Gateway and DNS each have a single series, so the first-sample lookup is deterministic.
	for _, c := range checks {
		if c.metric == "test_peer_latency_seconds" {
			continue
		}
		got, ok := findLabelValue(t, families, c.metric, "address_family")
		if !ok {
			t.Errorf("%s: address_family label not found", c.metric)
			continue
		}
		if got != c.want {
			t.Errorf("%s: address_family = %q, want %q", c.metric, got, c.want)
		}
	}

	// Peer metric has two series; assert that both expected family labels are present.
	wantPeerFamilies := map[string]bool{"ipv6": false, "unknown": false}
	for _, mf := range families {
		if mf.GetName() != "test_peer_latency_seconds" {
			continue
		}
		for _, metric := range mf.Metric {
			for _, label := range metric.Label {
				if label.GetName() == "address_family" {
					if _, expected := wantPeerFamilies[label.GetValue()]; expected {
						wantPeerFamilies[label.GetValue()] = true
					}
				}
			}
		}
	}
	for fam, seen := range wantPeerFamilies {
		if !seen {
			t.Errorf("peer_latency_seconds: expected an address_family=%q series, none found", fam)
		}
	}
}

// counterValue returns the value of the first sample of the named counter metric
// family whose labels include all of wantLabels, or (0, false) if not found.
func counterValue(families []*dto.MetricFamily, metricName string, wantLabels map[string]string) (float64, bool) {
	for _, mf := range families {
		if mf.GetName() != metricName {
			continue
		}
		for _, metric := range mf.Metric {
			labels := make(map[string]string)
			for _, l := range metric.Label {
				labels[l.GetName()] = l.GetValue()
			}
			match := true
			for k, v := range wantLabels {
				if labels[k] != v {
					match = false
					break
				}
			}
			if match && metric.Counter != nil {
				return metric.Counter.GetValue(), true
			}
		}
	}
	return 0, false
}

// gaugeValue returns the value of the first sample of the named gauge metric
// family whose labels include all of wantLabels, or (0, false) if not found.
func gaugeValue(families []*dto.MetricFamily, metricName string, wantLabels map[string]string) (float64, bool) {
	for _, mf := range families {
		if mf.GetName() != metricName {
			continue
		}
		for _, metric := range mf.Metric {
			labels := make(map[string]string)
			for _, l := range metric.Label {
				labels[l.GetName()] = l.GetValue()
			}
			match := true
			for k, v := range wantLabels {
				if labels[k] != v {
					match = false
					break
				}
			}
			if match && metric.Gauge != nil {
				return metric.Gauge.GetValue(), true
			}
		}
	}
	return 0, false
}

// histogramSampleCount returns the sample count of the named histogram metric
// family whose labels include all of wantLabels, or (0, false) if not found.
func histogramSampleCount(families []*dto.MetricFamily, metricName string, wantLabels map[string]string) (uint64, bool) {
	for _, mf := range families {
		if mf.GetName() != metricName {
			continue
		}
		for _, metric := range mf.Metric {
			labels := make(map[string]string)
			for _, l := range metric.Label {
				labels[l.GetName()] = l.GetValue()
			}
			match := true
			for k, v := range wantLabels {
				if labels[k] != v {
					match = false
					break
				}
			}
			if match && metric.Histogram != nil {
				return metric.Histogram.GetSampleCount(), true
			}
		}
	}
	return 0, false
}

func TestRecordMonitorCycle(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics, err := NewMetrics("test", "", nil)
	if err != nil {
		t.Fatalf("failed to create metrics: %v", err)
	}
	if err := metrics.Register(registry); err != nil {
		t.Fatalf("failed to register metrics: %v", err)
	}

	e := &PrometheusExporter{
		nodeName: "test-node",
		registry: registry,
		metrics:  metrics,
	}

	// Two successful cycles and one errored cycle for the same monitor.
	e.RecordMonitorCycle("disk-monitor", 50*time.Millisecond, nil)
	e.RecordMonitorCycle("disk-monitor", 75*time.Millisecond, nil)
	e.RecordMonitorCycle("disk-monitor", 10*time.Millisecond, fmt.Errorf("check failed"))

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	// Success counter should be 2.
	if got, ok := counterValue(families, "test_monitor_cycles_total", map[string]string{
		"monitor_name": "disk-monitor", "result": "success",
	}); !ok || got != 2 {
		t.Errorf("monitor_cycles_total{result=success} = %v (found=%v), want 2", got, ok)
	}

	// Error counter should be 1.
	if got, ok := counterValue(families, "test_monitor_cycles_total", map[string]string{
		"monitor_name": "disk-monitor", "result": "error",
	}); !ok || got != 1 {
		t.Errorf("monitor_cycles_total{result=error} = %v (found=%v), want 1", got, ok)
	}

	// MonitorCheckDuration should have observed all 3 cycles.
	if got, ok := histogramSampleCount(families, "test_monitor_check_duration_seconds", map[string]string{
		"monitor_name": "disk-monitor",
	}); !ok || got != 3 {
		t.Errorf("monitor_check_duration_seconds sample count = %v (found=%v), want 3", got, ok)
	}

	// Last-timestamp heartbeat gauge should be set to a positive unix time.
	if got, ok := gaugeValue(families, "test_monitor_cycle_last_timestamp_seconds", map[string]string{
		"monitor_name": "disk-monitor",
	}); !ok || got <= 0 {
		t.Errorf("monitor_cycle_last_timestamp_seconds = %v (found=%v), want > 0", got, ok)
	}
}

func TestRecordExportHealth(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics, err := NewMetrics("test", "", nil)
	if err != nil {
		t.Fatalf("failed to create metrics: %v", err)
	}
	if err := metrics.Register(registry); err != nil {
		t.Fatalf("failed to register metrics: %v", err)
	}

	e := &PrometheusExporter{
		nodeName: "test-node",
		registry: registry,
		metrics:  metrics,
	}

	healthLabels := map[string]string{"node": "test-node", "exporter": "prometheus"}

	// recordExportHealth requires the caller to hold e.mu; mirror real usage.
	record := func(success bool) {
		e.mu.Lock()
		e.recordExportHealth(success)
		e.mu.Unlock()
	}

	// A successful export: Healthy=1, timestamp>0, consecutive failures=0.
	record(true)

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	if got, ok := gaugeValue(families, "test_exporter_healthy", healthLabels); !ok || got != 1 {
		t.Errorf("exporter_healthy after success = %v (found=%v), want 1", got, ok)
	}
	if got, ok := gaugeValue(families, "test_exporter_last_success_timestamp_seconds", healthLabels); !ok || got <= 0 {
		t.Errorf("exporter_last_success_timestamp_seconds after success = %v (found=%v), want > 0", got, ok)
	}
	if got, ok := gaugeValue(families, "test_exporter_consecutive_failures", healthLabels); !ok || got != 0 {
		t.Errorf("exporter_consecutive_failures after success = %v (found=%v), want 0", got, ok)
	}

	// Capture the last-success timestamp so we can confirm failures don't bump it.
	lastSuccess, _ := gaugeValue(families, "test_exporter_last_success_timestamp_seconds", healthLabels)

	// Two consecutive failures: Healthy=0, consecutive failures increments to 2.
	record(false)
	record(false)

	families, err = registry.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	if got, ok := gaugeValue(families, "test_exporter_healthy", healthLabels); !ok || got != 0 {
		t.Errorf("exporter_healthy after failures = %v (found=%v), want 0", got, ok)
	}
	if got, ok := gaugeValue(families, "test_exporter_consecutive_failures", healthLabels); !ok || got != 2 {
		t.Errorf("exporter_consecutive_failures after 2 failures = %v (found=%v), want 2", got, ok)
	}
	// Last-success timestamp must not change on failure.
	if got, ok := gaugeValue(families, "test_exporter_last_success_timestamp_seconds", healthLabels); !ok || got != lastSuccess {
		t.Errorf("exporter_last_success_timestamp_seconds changed on failure = %v, want %v", got, lastSuccess)
	}

	// A success after failures: Healthy=1, consecutive failures reset to 0.
	record(true)

	families, err = registry.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	if got, ok := gaugeValue(families, "test_exporter_healthy", healthLabels); !ok || got != 1 {
		t.Errorf("exporter_healthy after recovery = %v (found=%v), want 1", got, ok)
	}
	if got, ok := gaugeValue(families, "test_exporter_consecutive_failures", healthLabels); !ok || got != 0 {
		t.Errorf("exporter_consecutive_failures after recovery = %v (found=%v), want 0", got, ok)
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

func TestRemediatorCircuitBreakerStateGauge(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics, err := NewMetrics("test", "", nil)
	if err != nil {
		t.Fatalf("failed to create metrics: %v", err)
	}
	if err := metrics.Register(registry); err != nil {
		t.Fatalf("failed to register metrics: %v", err)
	}

	e := &PrometheusExporter{
		nodeName: "test-node",
		registry: registry,
		metrics:  metrics,
	}

	// ObserveCircuitState(2) should set the gauge to 2 (half-open).
	e.ObserveCircuitState(2)

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	const metricName = "test_remediator_circuit_breaker_state"

	// The gauge must be present in the gathered (registered) set.
	found := false
	for _, mf := range families {
		if mf.GetName() == metricName {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("%s not present in registered/gathered metrics", metricName)
	}

	got, ok := gaugeValue(families, metricName, map[string]string{"node": "test-node"})
	if !ok {
		t.Fatalf("%s{node=test-node} not found", metricName)
	}
	if got != 2 {
		t.Errorf("%s = %v, want 2 (half-open)", metricName, got)
	}

	// A subsequent transition value should overwrite the gauge.
	e.ObserveCircuitState(0)
	families, err = registry.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}
	got, ok = gaugeValue(families, metricName, map[string]string{"node": "test-node"})
	if !ok {
		t.Fatalf("%s{node=test-node} not found after second observe", metricName)
	}
	if got != 0 {
		t.Errorf("%s = %v, want 0 (closed)", metricName, got)
	}
}

func TestRecordConfigReload(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics, err := NewMetrics("test", "", nil)
	if err != nil {
		t.Fatalf("failed to create metrics: %v", err)
	}
	if err := metrics.Register(registry); err != nil {
		t.Fatalf("failed to register metrics: %v", err)
	}

	e := &PrometheusExporter{
		nodeName: "test-node",
		registry: registry,
		metrics:  metrics,
	}

	nodeLabels := map[string]string{"node": "test-node"}

	// A successful reload: LastSuccess=1, timestamp>0, success counter=1,
	// duration histogram observed once.
	e.RecordConfigReload(true, 25*time.Millisecond)

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	if got, ok := gaugeValue(families, "test_config_reload_last_success", nodeLabels); !ok || got != 1 {
		t.Errorf("config_reload_last_success after success = %v (found=%v), want 1", got, ok)
	}
	if got, ok := gaugeValue(families, "test_config_reload_last_timestamp_seconds", nodeLabels); !ok || got <= 0 {
		t.Errorf("config_reload_last_timestamp_seconds after success = %v (found=%v), want > 0", got, ok)
	}
	if got, ok := counterValue(families, "test_config_reloads_total", map[string]string{
		"node": "test-node", "result": "success",
	}); !ok || got != 1 {
		t.Errorf("config_reloads_total{result=success} = %v (found=%v), want 1", got, ok)
	}
	if got, ok := histogramSampleCount(families, "test_config_reload_duration_seconds", nodeLabels); !ok || got != 1 {
		t.Errorf("config_reload_duration_seconds sample count = %v (found=%v), want 1", got, ok)
	}

	// A failed reload: LastSuccess flips to 0, failure counter=1, timestamp still
	// advances (last-attempt heartbeat), duration histogram observed again.
	e.RecordConfigReload(false, 10*time.Millisecond)

	families, err = registry.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	if got, ok := gaugeValue(families, "test_config_reload_last_success", nodeLabels); !ok || got != 0 {
		t.Errorf("config_reload_last_success after failure = %v (found=%v), want 0", got, ok)
	}
	if got, ok := counterValue(families, "test_config_reloads_total", map[string]string{
		"node": "test-node", "result": "failure",
	}); !ok || got != 1 {
		t.Errorf("config_reloads_total{result=failure} = %v (found=%v), want 1", got, ok)
	}
	// Success counter must be unchanged.
	if got, ok := counterValue(families, "test_config_reloads_total", map[string]string{
		"node": "test-node", "result": "success",
	}); !ok || got != 1 {
		t.Errorf("config_reloads_total{result=success} after failure = %v (found=%v), want 1", got, ok)
	}
	if got, ok := histogramSampleCount(families, "test_config_reload_duration_seconds", nodeLabels); !ok || got != 2 {
		t.Errorf("config_reload_duration_seconds sample count = %v (found=%v), want 2", got, ok)
	}
}

// TestSelfMetricsRegistered is the authoritative "all self-metrics registered"
// test for task #17215. It proves the full self-metrics surface (observability
// about node-doctor itself) is both registered in the exporter's registry and
// actually gather-able after each metric has been recorded.
//
// Registry choice: it wires NewRegistry(...) — the exact constructor the
// production exporter uses (see NewPrometheusExporter in exporter.go) — and then
// registers the node-doctor Metrics into it via metrics.Register. This is the
// only configuration that lets one test assert BOTH the node-doctor self-metric
// families AND the standard go_*/process_* collector families that NewRegistry
// adds. A namespace of "node_doctor" (the production default) is used so the
// asserted family names carry the real production prefix, e.g.
// node_doctor_monitor_cycles_total.
//
// Population: each self-metric is exercised through the same recorder method
// production uses (RecordMonitorCycle, recordExportHealth, RecordConfigReload,
// ObserveCircuitState). The three export-operation self-metrics
// (export_operations_total, export_errors_total, export_duration_seconds) have
// no single dedicated recorder, so they are populated directly via
// WithLabelValues(...).Inc()/Observe() to yield at least one series each.
func TestSelfMetricsRegistered(t *testing.T) {
	const (
		namespace = "node_doctor"
		nodeName  = "test-node"
		exporter  = "prometheus"
	)

	// Use the production registry constructor so go_*/process_* collectors are
	// present, then register node-doctor metrics into it exactly as the exporter
	// does.
	registry := NewRegistry(prometheus.Labels{})
	metrics, err := NewMetrics(namespace, "", nil)
	if err != nil {
		t.Fatalf("NewMetrics() error: %v", err)
	}
	if err := metrics.Register(registry); err != nil {
		t.Fatalf("metrics.Register() error: %v", err)
	}

	e := &PrometheusExporter{
		nodeName: nodeName,
		registry: registry,
		metrics:  metrics,
	}

	// --- Populate every self-metric so each family yields at least one series. ---

	// Monitor-cycle self-metrics: monitor_cycles_total,
	// monitor_check_duration_seconds, monitor_cycle_last_timestamp_seconds.
	e.RecordMonitorCycle("disk-monitor", 25*time.Millisecond, nil)

	// Exporter-health self-metrics: exporter_healthy,
	// exporter_last_success_timestamp_seconds, exporter_consecutive_failures.
	// recordExportHealth requires the caller to hold e.mu; mirror real usage.
	e.mu.Lock()
	e.recordExportHealth(true)
	e.mu.Unlock()

	// Export-operation self-metrics: export_operations_total, export_errors_total,
	// export_duration_seconds. No single recorder covers these, so drive the vecs
	// directly to create a series in each family.
	metrics.ExportOperationsTotal.WithLabelValues(nodeName, exporter, "status", "success").Inc()
	metrics.ExportErrorsTotal.WithLabelValues(nodeName, exporter, "timeout").Inc()
	metrics.ExportDuration.WithLabelValues(nodeName, exporter, "status").Observe(0.01)

	// Circuit-breaker self-metric: remediator_circuit_breaker_state.
	e.ObserveCircuitState(0)

	// Config-reload self-metrics: config_reloads_total,
	// config_reload_last_timestamp_seconds, config_reload_last_success,
	// config_reload_duration_seconds.
	e.RecordConfigReload(true, 15*time.Millisecond)

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("registry.Gather() error: %v", err)
	}

	present := make(map[string]bool, len(families))
	for _, mf := range families {
		present[mf.GetName()] = true
	}

	// Full self-metrics surface, with the production node_doctor_ prefix derived
	// from the namespace passed to NewMetrics (subsystem is empty).
	expected := []string{
		// monitor cycle
		"node_doctor_monitor_cycles_total",
		"node_doctor_monitor_cycle_last_timestamp_seconds",
		"node_doctor_monitor_check_duration_seconds",
		// exporter health
		"node_doctor_exporter_healthy",
		"node_doctor_exporter_last_success_timestamp_seconds",
		"node_doctor_exporter_consecutive_failures",
		// export ops
		"node_doctor_export_operations_total",
		"node_doctor_export_errors_total",
		"node_doctor_export_duration_seconds",
		// circuit breaker
		"node_doctor_remediator_circuit_breaker_state",
		// config reload
		"node_doctor_config_reloads_total",
		"node_doctor_config_reload_last_timestamp_seconds",
		"node_doctor_config_reload_last_success",
		"node_doctor_config_reload_duration_seconds",
	}

	var missing []string
	for _, name := range expected {
		if !present[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Errorf("self-metric families missing from registered/gathered set: %s", strings.Join(missing, ", "))
	}

	// Because NewRegistry wires the Go and process collectors, the runtime/process
	// self-observability families must also be exposed. go_goroutines is present
	// on every platform; process_* is only emitted on platforms the collector
	// supports (Linux in CI/production).
	if !present["go_goroutines"] {
		t.Errorf("expected go_goroutines from the Go collector wired by NewRegistry; not present")
	}
	if runtime.GOOS == "linux" {
		if !present["process_start_time_seconds"] {
			t.Errorf("expected process_start_time_seconds from the process collector on linux; not present")
		}
	}
}
