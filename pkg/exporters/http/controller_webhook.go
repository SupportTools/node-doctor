package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// ControllerWebhook manages sending periodic reports to the node-doctor controller.
type ControllerWebhook struct {
	config        *types.ControllerWebhookConfig
	reportBuilder *ReportBuilder
	httpClient    *http.Client

	mu      sync.RWMutex
	started bool
	stopCh  chan struct{}
	wg      sync.WaitGroup

	// Statistics
	stats ControllerWebhookStats
}

// ControllerWebhookStats tracks statistics for controller webhook.
type ControllerWebhookStats struct {
	mu                sync.RWMutex
	ReportsSent       int64
	ReportsSucceeded  int64
	ReportsFailed     int64
	LastReportTime    time.Time
	LastSuccessTime   time.Time
	LastError         string
	LastErrorTime     time.Time
	AvgResponseTimeMs int64
	totalResponseTime int64
}

// NewControllerWebhook creates a new controller webhook sender.
func NewControllerWebhook(
	config *types.ControllerWebhookConfig,
	nodeName, nodeUID, version string,
) (*ControllerWebhook, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}
	if !config.Enabled {
		return nil, fmt.Errorf("controller webhook is disabled")
	}
	if config.URL == "" {
		return nil, fmt.Errorf("controller URL is required")
	}

	// Create HTTP client with configured timeout
	httpClient := &http.Client{
		Timeout: config.Timeout,
	}

	reportBuilder := NewReportBuilder(nodeName, nodeUID, version)

	return &ControllerWebhook{
		config:        config,
		reportBuilder: reportBuilder,
		httpClient:    httpClient,
		stopCh:        make(chan struct{}),
	}, nil
}

// Start begins the periodic report sending loop.
func (cw *ControllerWebhook) Start(ctx context.Context) error {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	if cw.started {
		return fmt.Errorf("controller webhook already started")
	}

	log.Printf("[INFO] Starting controller webhook to %s (interval: %v)",
		cw.config.URL, cw.config.Interval)

	// Send initial startup report
	cw.wg.Add(1)
	go func() {
		defer cw.wg.Done()
		cw.sendReportWithRetry(ctx, "startup")
	}()

	// Start periodic report loop
	cw.wg.Add(1)
	go cw.reportLoop(ctx)

	cw.started = true
	log.Printf("[INFO] Controller webhook started")

	return nil
}

// Stop stops the controller webhook sender.
func (cw *ControllerWebhook) Stop() error {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	if !cw.started {
		return nil
	}

	log.Printf("[INFO] Stopping controller webhook...")

	close(cw.stopCh)
	cw.wg.Wait()

	cw.started = false
	log.Printf("[INFO] Controller webhook stopped")

	return nil
}

// UpdateFromStatus updates the report builder with a new monitor status.
func (cw *ControllerWebhook) UpdateFromStatus(status *types.Status) {
	cw.reportBuilder.UpdateFromStatus(status)
}

// AddProblem adds or updates an active problem.
func (cw *ControllerWebhook) AddProblem(problem *types.Problem) {
	cw.reportBuilder.AddProblem(problem)
}

// RemoveProblem removes a resolved problem.
func (cw *ControllerWebhook) RemoveProblem(problemType, resource string) {
	cw.reportBuilder.RemoveProblem(problemType, resource)
}

// IncrementRemediations increments the remediation counter.
func (cw *ControllerWebhook) IncrementRemediations() {
	cw.reportBuilder.IncrementRemediations()
}

// GetStats returns the current webhook statistics.
func (cw *ControllerWebhook) GetStats() ControllerWebhookStats {
	cw.stats.mu.RLock()
	defer cw.stats.mu.RUnlock()
	return ControllerWebhookStats{
		ReportsSent:       cw.stats.ReportsSent,
		ReportsSucceeded:  cw.stats.ReportsSucceeded,
		ReportsFailed:     cw.stats.ReportsFailed,
		LastReportTime:    cw.stats.LastReportTime,
		LastSuccessTime:   cw.stats.LastSuccessTime,
		LastError:         cw.stats.LastError,
		LastErrorTime:     cw.stats.LastErrorTime,
		AvgResponseTimeMs: cw.stats.AvgResponseTimeMs,
	}
}

// GetReportBuilder returns the underlying report builder.
func (cw *ControllerWebhook) GetReportBuilder() *ReportBuilder {
	return cw.reportBuilder
}

// reportLoop periodically sends reports to the controller.
func (cw *ControllerWebhook) reportLoop(ctx context.Context) {
	defer cw.wg.Done()

	ticker := time.NewTicker(cw.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("[DEBUG] Controller webhook stopping due to context cancellation")
			return
		case <-cw.stopCh:
			log.Printf("[DEBUG] Controller webhook stopping due to stop signal")
			return
		case <-ticker.C:
			cw.sendReportWithRetry(ctx, "periodic")
		}
	}
}

// sendReportWithRetry sends a report with retry logic.
func (cw *ControllerWebhook) sendReportWithRetry(ctx context.Context, reportType string) {
	var lastErr error
	maxAttempts := 1
	if cw.config.Retry != nil {
		maxAttempts = cw.config.Retry.MaxAttempts
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := cw.sendReport(ctx, reportType)
		if err == nil {
			return // Success
		}

		lastErr = err
		log.Printf("[WARN] Controller webhook report attempt %d/%d failed: %v",
			attempt, maxAttempts, err)

		// Don't retry on last attempt or context cancellation
		if attempt >= maxAttempts {
			break
		}

		select {
		case <-ctx.Done():
			return
		case <-cw.stopCh:
			return
		default:
		}

		// Calculate backoff delay
		delay := cw.calculateBackoff(attempt)
		select {
		case <-ctx.Done():
			return
		case <-cw.stopCh:
			return
		case <-time.After(delay):
			// Continue to next attempt
		}
	}

	if lastErr != nil {
		cw.recordError(lastErr)
	}
}

// sendReport sends a single report to the controller.
func (cw *ControllerWebhook) sendReport(ctx context.Context, reportType string) error {
	// Build the report
	report := cw.reportBuilder.BuildReport(reportType)

	// Marshal to JSON
	body, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("failed to marshal report: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cw.config.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "node-doctor/"+report.Version)

	// Add custom headers
	for key, value := range cw.config.Headers {
		req.Header.Set(key, value)
	}

	// Add authentication
	if err := cw.addAuth(req); err != nil {
		return fmt.Errorf("failed to add authentication: %w", err)
	}

	// Send request and track timing
	startTime := time.Now()
	resp, err := cw.httpClient.Do(req)
	responseTime := time.Since(startTime)

	cw.recordAttempt(responseTime)

	if err != nil {
		return fmt.Errorf("failed to send report: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	// Check response status
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		cw.recordSuccess()
		log.Printf("[DEBUG] Controller report sent successfully (status: %d, time: %v)",
			resp.StatusCode, responseTime)
		return nil
	}

	// Handle error response
	return fmt.Errorf("controller returned status %d: %s", resp.StatusCode, string(respBody))
}

// addAuth adds authentication headers to the request.
func (cw *ControllerWebhook) addAuth(req *http.Request) error {
	switch cw.config.Auth.Type {
	case "none", "":
		// No authentication
	case "bearer":
		if cw.config.Auth.Token == "" {
			return fmt.Errorf("bearer token is required")
		}
		req.Header.Set("Authorization", "Bearer "+cw.config.Auth.Token)
	case "basic":
		if cw.config.Auth.Username == "" || cw.config.Auth.Password == "" {
			return fmt.Errorf("username and password are required for basic auth")
		}
		req.SetBasicAuth(cw.config.Auth.Username, cw.config.Auth.Password)
	default:
		return fmt.Errorf("unsupported auth type: %s", cw.config.Auth.Type)
	}
	return nil
}

// calculateBackoff calculates the backoff delay for a retry attempt.
func (cw *ControllerWebhook) calculateBackoff(attempt int) time.Duration {
	if cw.config.Retry == nil {
		return time.Second
	}

	// Exponential backoff with jitter
	delay := cw.config.Retry.BaseDelay * time.Duration(1<<uint(attempt-1))
	if delay > cw.config.Retry.MaxDelay {
		delay = cw.config.Retry.MaxDelay
	}
	return delay
}

// recordAttempt records that a send attempt was made.
func (cw *ControllerWebhook) recordAttempt(responseTime time.Duration) {
	cw.stats.mu.Lock()
	defer cw.stats.mu.Unlock()

	cw.stats.ReportsSent++
	cw.stats.LastReportTime = time.Now()

	// Update average response time
	cw.stats.totalResponseTime += responseTime.Milliseconds()
	cw.stats.AvgResponseTimeMs = cw.stats.totalResponseTime / cw.stats.ReportsSent
}

// recordSuccess records a successful send.
func (cw *ControllerWebhook) recordSuccess() {
	cw.stats.mu.Lock()
	defer cw.stats.mu.Unlock()

	cw.stats.ReportsSucceeded++
	cw.stats.LastSuccessTime = time.Now()
}

// recordError records a failed send.
func (cw *ControllerWebhook) recordError(err error) {
	cw.stats.mu.Lock()
	defer cw.stats.mu.Unlock()

	cw.stats.ReportsFailed++
	cw.stats.LastError = err.Error()
	cw.stats.LastErrorTime = time.Now()
}

// SendOnDemandReport sends a report immediately (for testing or on-demand requests).
func (cw *ControllerWebhook) SendOnDemandReport(ctx context.Context) error {
	cw.sendReportWithRetry(ctx, "on-demand")
	return nil
}

// IsHealthy returns whether the controller webhook is healthy.
func (cw *ControllerWebhook) IsHealthy() bool {
	cw.stats.mu.RLock()
	defer cw.stats.mu.RUnlock()

	// Unhealthy if no successful reports in the last 3 intervals
	threshold := 3 * cw.config.Interval
	if time.Since(cw.stats.LastSuccessTime) > threshold && cw.stats.ReportsSent > 0 {
		return false
	}

	// Unhealthy if failure rate is too high
	if cw.stats.ReportsSent > 10 {
		failureRate := float64(cw.stats.ReportsFailed) / float64(cw.stats.ReportsSent)
		if failureRate > 0.5 {
			return false
		}
	}

	return true
}
