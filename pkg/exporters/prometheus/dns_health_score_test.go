package prometheus

import (
	"bufio"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// TestDNSHealthScore_InsufficientDataSkipped verifies that nameservers whose
// health status is "insufficient_data" are NOT written to the gauge.
func TestDNSHealthScore_InsufficientDataSkipped(t *testing.T) {
	exporter, port := newStartedExporter(t)
	ctx := context.Background()

	status := (&types.Status{Source: "test", Timestamp: time.Now()}).
		SetLatencyMetrics(&types.LatencyMetrics{
			NameserverHealthScores: []types.NameserverHealthScore{
				{Nameserver: "8.8.8.8", Score: 92.0, Status: "healthy"},
				{Nameserver: "1.1.1.1", Score: 0.0, Status: "insufficient_data"},
			},
		})

	if err := exporter.ExportStatus(ctx, status); err != nil {
		t.Fatalf("ExportStatus: %v", err)
	}

	body := scrapeMetrics(t, port, "/metrics")

	// 8.8.8.8 is healthy — it must appear.
	if !strings.Contains(body, `nameserver="8.8.8.8"`) {
		t.Error("expected 8.8.8.8 health-score gauge to be exported")
	}

	// 1.1.1.1 has insufficient_data — it must NOT appear.
	if strings.Contains(body, `nameserver="1.1.1.1"`) {
		t.Error("insufficient_data nameserver 1.1.1.1 must not be exported to Prometheus")
	}
}

// TestDNSHealthScore_GaugeValueRecording verifies that the composite score value
// recorded in the gauge matches the value in NameserverHealthScore.Score.
func TestDNSHealthScore_GaugeValueRecording(t *testing.T) {
	exporter, port := newStartedExporter(t)
	ctx := context.Background()

	cases := []types.NameserverHealthScore{
		{Nameserver: "8.8.8.8", Score: 100.0, Status: "healthy"},
		{Nameserver: "1.1.1.1", Score: 75.5, Status: "degraded"},
		{Nameserver: "192.168.1.1", Score: 0.0, Status: "unhealthy"},
	}

	status := (&types.Status{Source: "test", Timestamp: time.Now()}).
		SetLatencyMetrics(&types.LatencyMetrics{NameserverHealthScores: cases})

	if err := exporter.ExportStatus(ctx, status); err != nil {
		t.Fatalf("ExportStatus: %v", err)
	}

	body := scrapeMetrics(t, port, "/metrics")

	for _, c := range cases {
		expected := prometheusFloat(c.Score)
		if !containsNameserverHealthScore(body, c.Nameserver, expected) {
			t.Errorf("nameserver=%s: expected gauge value %s in output", c.Nameserver, expected)
		}
	}
}

// TestDNSHealthScore_LabelCorrectness verifies that the node label reflects the
// configured node name and the nameserver label contains the nameserver address.
func TestDNSHealthScore_LabelCorrectness(t *testing.T) {
	port := freePort(t)
	cfg := &types.PrometheusExporterConfig{
		Enabled:   true,
		Port:      port,
		Path:      "/metrics",
		Namespace: "test",
	}
	settings := &types.GlobalSettings{NodeName: "my-custom-node"}

	exp, err := NewPrometheusExporter(cfg, settings)
	if err != nil {
		t.Fatalf("NewPrometheusExporter: %v", err)
	}
	ctx := context.Background()
	if err := exp.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { exp.Stop() })
	if err := waitForServerReady(fmt.Sprintf("localhost:%d", port), 5*time.Second); err != nil {
		t.Fatalf("server not ready: %v", err)
	}

	status := (&types.Status{Source: "test", Timestamp: time.Now()}).
		SetLatencyMetrics(&types.LatencyMetrics{
			NameserverHealthScores: []types.NameserverHealthScore{
				{Nameserver: "9.9.9.9", Score: 88.0, Status: "healthy"},
			},
		})

	if err := exp.ExportStatus(ctx, status); err != nil {
		t.Fatalf("ExportStatus: %v", err)
	}

	body := scrapeMetrics(t, port, "/metrics")

	// Both labels must appear on the same dns_nameserver_health_score line.
	if !containsLineWith(body, "dns_nameserver_health_score",
		`node="my-custom-node"`, `nameserver="9.9.9.9"`) {
		t.Errorf("dns_nameserver_health_score metric missing expected labels; body excerpt:\n%s",
			extractLines(body, "dns_nameserver_health_score"))
	}
}

// TestDNSHealthScore_MetricNameSpecification verifies the exported metric name,
// HELP text, and TYPE declaration all match the specification.
func TestDNSHealthScore_MetricNameSpecification(t *testing.T) {
	exporter, port := newStartedExporter(t)
	ctx := context.Background()

	status := (&types.Status{Source: "test", Timestamp: time.Now()}).
		SetLatencyMetrics(&types.LatencyMetrics{
			NameserverHealthScores: []types.NameserverHealthScore{
				{Nameserver: "8.8.8.8", Score: 95.0, Status: "healthy"},
			},
		})

	if err := exporter.ExportStatus(ctx, status); err != nil {
		t.Fatalf("ExportStatus: %v", err)
	}

	body := scrapeMetrics(t, port, "/metrics")

	// Metric name: {namespace}_dns_nameserver_health_score
	if !strings.Contains(body, "test_dns_nameserver_health_score") {
		t.Error("metric name test_dns_nameserver_health_score not found; spec requires {namespace}_dns_nameserver_health_score")
	}
	// HELP comment must be present.
	if !strings.Contains(body, "# HELP test_dns_nameserver_health_score") {
		t.Error("# HELP line for test_dns_nameserver_health_score not found")
	}
	// TYPE must be gauge.
	if !strings.Contains(body, "# TYPE test_dns_nameserver_health_score gauge") {
		t.Error("expected # TYPE test_dns_nameserver_health_score gauge")
	}
}

// TestDNSHealthScore_EmptyListClearsGauges verifies that exporting an empty
// NameserverHealthScores list removes all previously recorded gauges.
func TestDNSHealthScore_EmptyListClearsGauges(t *testing.T) {
	exporter, port := newStartedExporter(t)
	ctx := context.Background()

	// Seed: export a score so there's something to clear.
	statusWith := (&types.Status{Source: "test", Timestamp: time.Now()}).
		SetLatencyMetrics(&types.LatencyMetrics{
			NameserverHealthScores: []types.NameserverHealthScore{
				{Nameserver: "8.8.8.8", Score: 90.0, Status: "healthy"},
			},
		})
	if err := exporter.ExportStatus(ctx, statusWith); err != nil {
		t.Fatalf("ExportStatus (seed): %v", err)
	}

	// Now export an empty list.
	statusEmpty := (&types.Status{Source: "test", Timestamp: time.Now()}).
		SetLatencyMetrics(&types.LatencyMetrics{
			NameserverHealthScores: []types.NameserverHealthScore{},
		})
	if err := exporter.ExportStatus(ctx, statusEmpty); err != nil {
		t.Fatalf("ExportStatus (empty): %v", err)
	}

	body := scrapeMetrics(t, port, "/metrics")

	// Stale gauge must be gone.
	if strings.Contains(body, `nameserver="8.8.8.8"`) {
		t.Error("stale 8.8.8.8 gauge persisted after empty export — Reset() not effective")
	}
}

// TestDNSHealthScore_AllInsufficientDataClearsGauges verifies that when every
// nameserver transitions to "insufficient_data", the gauge is fully cleared.
func TestDNSHealthScore_AllInsufficientDataClearsGauges(t *testing.T) {
	exporter, port := newStartedExporter(t)
	ctx := context.Background()

	// Seed valid score.
	statusWith := (&types.Status{Source: "test", Timestamp: time.Now()}).
		SetLatencyMetrics(&types.LatencyMetrics{
			NameserverHealthScores: []types.NameserverHealthScore{
				{Nameserver: "8.8.8.8", Score: 90.0, Status: "healthy"},
				{Nameserver: "1.1.1.1", Score: 85.0, Status: "healthy"},
			},
		})
	if err := exporter.ExportStatus(ctx, statusWith); err != nil {
		t.Fatalf("ExportStatus (seed): %v", err)
	}

	// All nameservers now report insufficient_data.
	statusInsuff := (&types.Status{Source: "test", Timestamp: time.Now()}).
		SetLatencyMetrics(&types.LatencyMetrics{
			NameserverHealthScores: []types.NameserverHealthScore{
				{Nameserver: "8.8.8.8", Score: 0.0, Status: "insufficient_data"},
				{Nameserver: "1.1.1.1", Score: 0.0, Status: "insufficient_data"},
			},
		})
	if err := exporter.ExportStatus(ctx, statusInsuff); err != nil {
		t.Fatalf("ExportStatus (insufficient): %v", err)
	}

	body := scrapeMetrics(t, port, "/metrics")

	if strings.Contains(body, `nameserver="8.8.8.8"`) {
		t.Error("8.8.8.8 gauge must not be exported when status is insufficient_data")
	}
	if strings.Contains(body, `nameserver="1.1.1.1"`) {
		t.Error("1.1.1.1 gauge must not be exported when status is insufficient_data")
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

// newStartedExporter creates and starts a PrometheusExporter on a free port
// with namespace "test" and node name "test-node". It registers t.Cleanup to
// stop the exporter and blocks until the server is ready.
func newStartedExporter(t *testing.T) (*PrometheusExporter, int) {
	t.Helper()
	port := freePort(t)
	cfg := &types.PrometheusExporterConfig{
		Enabled:   true,
		Port:      port,
		Path:      "/metrics",
		Namespace: "test",
	}
	exp, err := NewPrometheusExporter(cfg, &types.GlobalSettings{NodeName: "test-node"})
	if err != nil {
		t.Fatalf("NewPrometheusExporter: %v", err)
	}
	if err := exp.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { exp.Stop() })
	if err := waitForServerReady(fmt.Sprintf("localhost:%d", port), 5*time.Second); err != nil {
		t.Fatalf("server not ready: %v", err)
	}
	return exp, port
}

// containsNameserverHealthScore reports whether body contains a
// dns_nameserver_health_score line for the given nameserver with the given value.
func containsNameserverHealthScore(body, nameserver, value string) bool {
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "dns_nameserver_health_score") &&
			strings.Contains(line, fmt.Sprintf(`nameserver="%s"`, nameserver)) {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[len(fields)-1] == value {
				return true
			}
		}
	}
	return false
}

// containsLineWith reports whether body contains a line that includes metricName
// and all the given label fragments.
func containsLineWith(body, metricName string, labels ...string) bool {
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, metricName) {
			continue
		}
		match := true
		for _, lbl := range labels {
			if !strings.Contains(line, lbl) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// extractLines returns all lines from body that contain substr, for use in
// diagnostic messages.
func extractLines(body, substr string) string {
	var b strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, substr) {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// prometheusFloat formats a float64 the same way the Prometheus Go client does
// in its text exposition format.
func prometheusFloat(f float64) string {
	switch f {
	case 1:
		return "1"
	case 0:
		return "0"
	case -1:
		return "-1"
	default:
		return fmt.Sprintf("%g", f)
	}
}
