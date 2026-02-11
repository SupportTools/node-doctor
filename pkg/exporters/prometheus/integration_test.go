package prometheus

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

func TestMetricsEndpoint(t *testing.T) {
	config := &types.PrometheusExporterConfig{
		Enabled:   true,
		Port:      9106,
		Path:      "/metrics",
		Namespace: "test",
	}
	settings := &types.GlobalSettings{NodeName: "test-node"}

	exporter, err := NewPrometheusExporter(config, settings)
	if err != nil {
		t.Fatalf("failed to create exporter: %v", err)
	}

	ctx := context.Background()
	err = exporter.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start exporter: %v", err)
	}
	defer exporter.Stop()

	// Wait for server to start
	time.Sleep(200 * time.Millisecond)

	// Test metrics endpoint
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d%s", config.Port, config.Path))
	if err != nil {
		t.Fatalf("failed to get metrics: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Check content type
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/plain") && !strings.Contains(contentType, "application/openmetrics-text") {
		t.Errorf("unexpected content type: %s", contentType)
	}

	// Read body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	bodyStr := string(body)

	// Verify some expected metrics are present
	expectedMetrics := []string{
		"test_info",
		"test_start_time_seconds",
		"go_info",
		"process_start_time_seconds",
	}

	for _, metric := range expectedMetrics {
		if !strings.Contains(bodyStr, metric) {
			t.Errorf("expected metric '%s' not found in output", metric)
		}
	}
}

func TestHealthEndpoint(t *testing.T) {
	config := &types.PrometheusExporterConfig{
		Enabled:   true,
		Port:      9107,
		Path:      "/metrics",
		Namespace: "test",
	}
	settings := &types.GlobalSettings{NodeName: "test-node"}

	exporter, err := NewPrometheusExporter(config, settings)
	if err != nil {
		t.Fatalf("failed to create exporter: %v", err)
	}

	ctx := context.Background()
	err = exporter.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start exporter: %v", err)
	}
	defer exporter.Stop()

	// Wait for server to start
	time.Sleep(200 * time.Millisecond)

	// Test health endpoint
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/health", config.Port))
	if err != nil {
		t.Fatalf("failed to get health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Check content type
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Errorf("expected JSON content type, got: %s", contentType)
	}

	// Read body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	bodyStr := string(body)

	// Verify health response contains expected fields
	if !strings.Contains(bodyStr, `"status":"healthy"`) {
		t.Errorf("expected healthy status in response: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, `"service":"prometheus-exporter"`) {
		t.Errorf("expected service name in response: %s", bodyStr)
	}
}

func TestPrometheusFormat(t *testing.T) {
	config := &types.PrometheusExporterConfig{
		Enabled:   true,
		Port:      9108,
		Path:      "/metrics",
		Namespace: "test",
	}
	settings := &types.GlobalSettings{NodeName: "test-node"}

	exporter, err := NewPrometheusExporter(config, settings)
	if err != nil {
		t.Fatalf("failed to create exporter: %v", err)
	}

	ctx := context.Background()
	err = exporter.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start exporter: %v", err)
	}
	defer exporter.Stop()

	// Export some data to create metrics
	status := &types.Status{
		Source:    "test",
		Timestamp: time.Now(),
		Events: []types.Event{
			{
				Severity:  types.EventInfo,
				Timestamp: time.Now(),
				Reason:    "test-event",
				Message:   "Test event",
			},
		},
		Conditions: []types.Condition{
			{
				Type:       "Ready",
				Status:     types.ConditionTrue,
				Reason:     "NodeReady",
				Message:    "Node is ready",
				Transition: time.Now(),
			},
		},
	}
	exporter.ExportStatus(ctx, status)

	problem := &types.Problem{
		Type:       "TestProblem",
		Severity:   types.ProblemWarning,
		Resource:   "/test/resource",
		DetectedAt: time.Now(),
		Message:    "Test problem",
	}
	exporter.ExportProblem(ctx, problem)

	// Wait for metrics to be updated
	time.Sleep(100 * time.Millisecond)

	// Get metrics
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d%s", config.Port, config.Path))
	if err != nil {
		t.Fatalf("failed to get metrics: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	bodyStr := string(body)

	// Parse Prometheus format
	lines := strings.Split(bodyStr, "\n")

	var helpLines, typeLines, metricLines int

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			if strings.HasPrefix(line, "# HELP") {
				helpLines++
			} else if strings.HasPrefix(line, "# TYPE") {
				typeLines++
			}
		} else {
			// This should be a metric line
			if strings.Contains(line, "{") && strings.Contains(line, "}") {
				metricLines++
			} else if strings.Contains(line, " ") {
				// Metric without labels
				metricLines++
			}
		}
	}

	if helpLines == 0 {
		t.Error("no HELP lines found in Prometheus output")
	}
	if typeLines == 0 {
		t.Error("no TYPE lines found in Prometheus output")
	}
	if metricLines == 0 {
		t.Error("no metric lines found in Prometheus output")
	}

	// Verify our custom metrics are present
	expectedCustomMetrics := []string{
		"test_problems_total",
		"test_status_updates_total",
		"test_condition_status",
		"test_info",
	}

	for _, metric := range expectedCustomMetrics {
		if !strings.Contains(bodyStr, metric) {
			t.Errorf("expected custom metric '%s' not found in output", metric)
		}
	}
}

func TestConditionStatusGauge(t *testing.T) {
	config := &types.PrometheusExporterConfig{
		Enabled:   true,
		Port:      9112,
		Path:      "/metrics",
		Namespace: "test",
	}
	settings := &types.GlobalSettings{NodeName: "test-node"}

	exporter, err := NewPrometheusExporter(config, settings)
	if err != nil {
		t.Fatalf("failed to create exporter: %v", err)
	}

	ctx := context.Background()
	err = exporter.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start exporter: %v", err)
	}
	defer exporter.Stop()

	// Export status with condition True
	status := &types.Status{
		Source:    "test",
		Timestamp: time.Now(),
		Conditions: []types.Condition{
			{
				Type:       "NetworkPartitioned",
				Status:     types.ConditionTrue,
				Reason:     "PeerUnreachable",
				Message:    "Cannot reach peers",
				Transition: time.Now(),
			},
		},
	}
	exporter.ExportStatus(ctx, status)

	time.Sleep(100 * time.Millisecond)

	// Scrape and verify gauge == 1
	body := scrapeMetrics(t, config.Port, config.Path)
	if !strings.Contains(body, `test_condition_status{condition_type="NetworkPartitioned"`) {
		t.Error("condition_status metric not found for NetworkPartitioned")
	}
	if !containsMetricWithValue(body, "test_condition_status", "NetworkPartitioned", "1") {
		t.Error("expected condition_status=1 for NetworkPartitioned=True")
	}

	// Export status with condition False — gauge should update to 0
	status2 := &types.Status{
		Source:    "test",
		Timestamp: time.Now(),
		Conditions: []types.Condition{
			{
				Type:       "NetworkPartitioned",
				Status:     types.ConditionFalse,
				Reason:     "PeersReachable",
				Message:    "All peers reachable",
				Transition: time.Now(),
			},
		},
	}
	exporter.ExportStatus(ctx, status2)

	time.Sleep(100 * time.Millisecond)

	// Scrape and verify gauge == 0
	body = scrapeMetrics(t, config.Port, config.Path)
	if !containsMetricWithValue(body, "test_condition_status", "NetworkPartitioned", "0") {
		t.Error("expected condition_status=0 for NetworkPartitioned=False, but gauge did not update")
	}
}

// scrapeMetrics fetches the /metrics endpoint and returns the body as a string.
func scrapeMetrics(t *testing.T, port int, path string) string {
	t.Helper()
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d%s", port, path))
	if err != nil {
		t.Fatalf("failed to get metrics: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	return string(body)
}

// containsMetricWithValue checks if a Prometheus text exposition contains a metric
// with the given name, condition_type label value, and numeric value.
func containsMetricWithValue(body, metricName, conditionType, value string) bool {
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, metricName) &&
			strings.Contains(line, fmt.Sprintf(`condition_type="%s"`, conditionType)) &&
			strings.HasSuffix(strings.TrimSpace(line), " "+value) {
			return true
		}
	}
	return false
}

func TestConcurrentScrapes(t *testing.T) {
	config := &types.PrometheusExporterConfig{
		Enabled:   true,
		Port:      9109,
		Path:      "/metrics",
		Namespace: "test",
	}
	settings := &types.GlobalSettings{NodeName: "test-node"}

	exporter, err := NewPrometheusExporter(config, settings)
	if err != nil {
		t.Fatalf("failed to create exporter: %v", err)
	}

	ctx := context.Background()
	err = exporter.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start exporter: %v", err)
	}
	defer exporter.Stop()

	// Wait for server to start
	time.Sleep(200 * time.Millisecond)

	// Run concurrent scrapes
	const numGoroutines = 10
	const numScrapes = 5

	var wg sync.WaitGroup
	errCh := make(chan error, numGoroutines*numScrapes)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < numScrapes; j++ {
				resp, err := http.Get(fmt.Sprintf("http://localhost:%d%s", config.Port, config.Path))
				if err != nil {
					errCh <- fmt.Errorf("scrape %d-%d failed: %w", id, j, err)
					return
				}

				if resp.StatusCode != http.StatusOK {
					errCh <- fmt.Errorf("scrape %d-%d got status %d", id, j, resp.StatusCode)
					resp.Body.Close()
					return
				}

				// Read and discard body
				_, err = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if err != nil {
					errCh <- fmt.Errorf("reading body %d-%d failed: %w", id, j, err)
					return
				}
			}
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Check for errors
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent scrape error: %v", err)
	}
}

func TestServerShutdown(t *testing.T) {
	config := &types.PrometheusExporterConfig{
		Enabled:   true,
		Port:      9110,
		Path:      "/metrics",
		Namespace: "test",
	}
	settings := &types.GlobalSettings{NodeName: "test-node"}

	exporter, err := NewPrometheusExporter(config, settings)
	if err != nil {
		t.Fatalf("failed to create exporter: %v", err)
	}

	ctx := context.Background()
	err = exporter.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start exporter: %v", err)
	}

	// Wait for server to start
	time.Sleep(200 * time.Millisecond)

	// Verify server is running
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d%s", config.Port, config.Path))
	if err != nil {
		t.Fatalf("failed to connect before shutdown: %v", err)
	}
	resp.Body.Close()

	// Stop the exporter
	err = exporter.Stop()
	if err != nil {
		t.Errorf("failed to stop exporter: %v", err)
	}

	// Wait for shutdown
	time.Sleep(500 * time.Millisecond)

	// Verify server is no longer running
	_, err = http.Get(fmt.Sprintf("http://localhost:%d%s", config.Port, config.Path))
	if err == nil {
		t.Error("expected connection to fail after shutdown")
	}
}

func TestMetricValues(t *testing.T) {
	config := &types.PrometheusExporterConfig{
		Enabled:   true,
		Port:      9111,
		Path:      "/metrics",
		Namespace: "test",
	}
	settings := &types.GlobalSettings{NodeName: "test-node"}

	exporter, err := NewPrometheusExporter(config, settings)
	if err != nil {
		t.Fatalf("failed to create exporter: %v", err)
	}

	ctx := context.Background()
	err = exporter.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start exporter: %v", err)
	}
	defer exporter.Stop()

	// Export multiple status updates
	for i := 0; i < 3; i++ {
		status := &types.Status{
			Source:    "test",
			Timestamp: time.Now(),
		}
		exporter.ExportStatus(ctx, status)
	}

	// Export multiple problems
	for i := 0; i < 2; i++ {
		problem := &types.Problem{
			Type:       fmt.Sprintf("TestProblem%d", i),
			Severity:   types.ProblemWarning,
			Resource:   fmt.Sprintf("/test/resource%d", i),
			DetectedAt: time.Now(),
			Message:    fmt.Sprintf("Test problem %d", i),
		}
		exporter.ExportProblem(ctx, problem)
	}

	// Wait for metrics to be updated
	time.Sleep(100 * time.Millisecond)

	// Get metrics
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d%s", config.Port, config.Path))
	if err != nil {
		t.Fatalf("failed to get metrics: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	bodyStr := string(body)

	// Parse metrics and check values
	scanner := bufio.NewScanner(strings.NewReader(bodyStr))

	var statusUpdatesCount, problemsCount int
	var foundStatusUpdates, foundProblems bool

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Look for our counter metrics
		if strings.HasPrefix(line, "test_status_updates_total") {
			foundStatusUpdates = true
			// Extract value
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				if parts[1] == "3" {
					statusUpdatesCount = 3
				}
			}
		}

		if strings.HasPrefix(line, "test_problems_total") {
			foundProblems = true
			// Extract value
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				if parts[1] == "1" {
					problemsCount++
				}
			}
		}
	}

	if !foundStatusUpdates {
		t.Error("status_updates_total metric not found")
	}
	if statusUpdatesCount != 3 {
		t.Errorf("expected status updates count to be 3, but couldn't verify from output")
	}

	if !foundProblems {
		t.Error("problems_total metric not found")
	}
	if problemsCount != 2 {
		t.Errorf("expected to find 2 problems_total metrics, found %d", problemsCount)
	}
}
