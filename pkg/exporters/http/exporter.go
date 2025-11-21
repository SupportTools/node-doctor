package http

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// HTTPExporter exports Node Doctor status updates and problems to HTTP webhooks
// It implements the types.Exporter interface and manages asynchronous webhook delivery
type HTTPExporter struct {
	config     *types.HTTPExporterConfig
	settings   *types.GlobalSettings
	workerPool *WorkerPool
	stats      *Stats
	mu         sync.RWMutex
	started    bool
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

// NewHTTPExporter creates a new HTTP exporter with the given configuration
func NewHTTPExporter(config *types.HTTPExporterConfig, settings *types.GlobalSettings) (*HTTPExporter, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}
	if settings == nil {
		return nil, fmt.Errorf("settings cannot be nil")
	}
	if !config.Enabled {
		return nil, fmt.Errorf("HTTP exporter is disabled")
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	// Ensure we have at least one webhook
	if len(config.Webhooks) == 0 {
		return nil, fmt.Errorf("no webhooks configured")
	}

	// Validate node name
	if settings.NodeName == "" {
		return nil, fmt.Errorf("node name is required")
	}

	// Create statistics tracker
	stats := NewStats()

	// Create worker pool
	workerPool, err := NewWorkerPool(config.Workers, config.QueueSize, settings.NodeName, stats)
	if err != nil {
		return nil, fmt.Errorf("failed to create worker pool: %w", err)
	}

	exporter := &HTTPExporter{
		config:     config,
		settings:   settings,
		workerPool: workerPool,
		stats:      stats,
		stopCh:     make(chan struct{}),
	}

	log.Printf("[INFO] Created HTTP exporter with %d workers, queue size %d, and %d webhooks",
		config.Workers, config.QueueSize, len(config.Webhooks))

	return exporter, nil
}

// Start initializes the HTTP exporter and starts the worker pool
func (e *HTTPExporter) Start(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.started {
		return fmt.Errorf("HTTP exporter already started")
	}

	log.Printf("[INFO] Starting HTTP exporter...")

	// Validate all webhooks before starting
	for i, webhook := range e.config.Webhooks {
		if err := webhook.Validate(); err != nil {
			return fmt.Errorf("webhook %d validation failed: %w", i, err)
		}

		// Initialize webhook stats
		e.stats.GetWebhookStats(webhook.Name)

		log.Printf("[INFO] Configured webhook %s: %s (auth: %s, timeout: %v)",
			webhook.Name, webhook.URL, webhook.Auth.Type, webhook.Timeout)
	}

	// Start worker pool
	if err := e.workerPool.Start(); err != nil {
		return fmt.Errorf("failed to start worker pool: %w", err)
	}

	// Start health check routine
	e.wg.Add(1)
	go e.healthCheckLoop(ctx)

	e.started = true
	log.Printf("[INFO] HTTP exporter started successfully")

	return nil
}

// Stop gracefully stops the HTTP exporter
func (e *HTTPExporter) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.started {
		return nil // Already stopped or never started
	}

	log.Printf("[INFO] Stopping HTTP exporter...")

	// Signal health check routine to stop
	close(e.stopCh)

	// Stop worker pool
	if err := e.workerPool.Stop(); err != nil {
		log.Printf("[WARN] Error stopping worker pool: %v", err)
	}

	// Wait for health check routine to finish
	e.wg.Wait()

	e.started = false
	log.Printf("[INFO] HTTP exporter stopped")

	return nil
}

// ExportStatus implements types.Exporter interface for status exports
func (e *HTTPExporter) ExportStatus(ctx context.Context, status *types.Status) error {
	if status == nil {
		return fmt.Errorf("status cannot be nil")
	}

	e.mu.RLock()
	started := e.started
	e.mu.RUnlock()

	if !started {
		return fmt.Errorf("HTTP exporter not started")
	}

	// Validate status
	if err := status.Validate(); err != nil {
		return fmt.Errorf("status validation failed: %w", err)
	}

	// Generate request ID for tracing
	requestID := e.generateRequestID("status")

	log.Printf("[DEBUG] Exporting status from %s (request: %s)", status.Source, requestID)

	// Submit to worker pool
	err := e.workerPool.SubmitStatusRequest(status, e.config.Webhooks, requestID)
	if err != nil {
		log.Printf("[WARN] Failed to submit status export request: %v", err)
		return err
	}

	return nil
}

// ExportProblem implements types.Exporter interface for problem exports
func (e *HTTPExporter) ExportProblem(ctx context.Context, problem *types.Problem) error {
	if problem == nil {
		return fmt.Errorf("problem cannot be nil")
	}

	e.mu.RLock()
	started := e.started
	e.mu.RUnlock()

	if !started {
		return fmt.Errorf("HTTP exporter not started")
	}

	// Validate problem
	if err := problem.Validate(); err != nil {
		return fmt.Errorf("problem validation failed: %w", err)
	}

	// Generate request ID for tracing
	requestID := e.generateRequestID("problem")

	log.Printf("[DEBUG] Exporting problem %s on %s (request: %s)", problem.Type, problem.Resource, requestID)

	// Submit to worker pool
	err := e.workerPool.SubmitProblemRequest(problem, e.config.Webhooks, requestID)
	if err != nil {
		log.Printf("[WARN] Failed to submit problem export request: %v", err)
		return err
	}

	return nil
}

// Reload implements types.ReloadableExporter interface for configuration reload
func (e *HTTPExporter) Reload(config interface{}) error {
	// Type assert with safety check
	httpConfig, ok := config.(*types.HTTPExporterConfig)
	if !ok {
		return fmt.Errorf("invalid config type for HTTP exporter: expected *types.HTTPExporterConfig, got %T", config)
	}

	// Validate before applying
	if httpConfig == nil {
		return fmt.Errorf("http exporter config cannot be nil")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	log.Printf("[INFO] Reloading HTTP exporter configuration")

	// Validate new configuration
	if err := httpConfig.Validate(); err != nil {
		return fmt.Errorf("new configuration validation failed: %w", err)
	}

	// Ensure we have at least one webhook
	if len(httpConfig.Webhooks) == 0 {
		return fmt.Errorf("no webhooks configured in new configuration")
	}

	oldConfig := e.config

	// Check if worker pool needs to be recreated
	if e.needsWorkerPoolRecreation(oldConfig, httpConfig) {
		log.Printf("[INFO] Recreating worker pool due to configuration changes")

		// Stop existing worker pool if running
		if e.started && e.workerPool != nil {
			if err := e.workerPool.Stop(); err != nil {
				log.Printf("[WARN] Error stopping existing worker pool: %v", err)
			}
		}

		// Create new worker pool
		newWorkerPool, err := NewWorkerPool(httpConfig.Workers, httpConfig.QueueSize, e.settings.NodeName, e.stats)
		if err != nil {
			return fmt.Errorf("failed to create new worker pool: %w", err)
		}

		// Replace worker pool
		e.workerPool = newWorkerPool

		// Start new worker pool if exporter is running
		if e.started {
			if err := e.workerPool.Start(); err != nil {
				return fmt.Errorf("failed to start new worker pool: %w", err)
			}
		}
	} else {
		// Worker pool configuration unchanged, no action needed
		log.Printf("[DEBUG] Worker pool configuration unchanged")
	}

	// Update configuration
	e.config = httpConfig

	// Initialize stats for new webhooks
	if e.started {
		for _, webhook := range httpConfig.Webhooks {
			if err := webhook.Validate(); err != nil {
				log.Printf("[WARN] Invalid webhook %s in new config: %v", webhook.Name, err)
				continue
			}

			// Initialize webhook stats
			e.stats.GetWebhookStats(webhook.Name)
		}
	}

	log.Printf("[INFO] Successfully reloaded HTTP exporter configuration")
	log.Printf("[DEBUG] Config changes: workers %d->%d, queueSize %d->%d, timeout %v->%v, webhooks %d->%d",
		oldConfig.Workers, httpConfig.Workers,
		oldConfig.QueueSize, httpConfig.QueueSize,
		oldConfig.Timeout, httpConfig.Timeout,
		len(oldConfig.Webhooks), len(httpConfig.Webhooks))

	return nil
}

// IsReloadable implements types.ReloadableExporter interface
func (e *HTTPExporter) IsReloadable() bool {
	return true
}

// needsWorkerPoolRecreation determines if the worker pool needs to be recreated
// based on configuration changes that affect worker pool behavior.
func (e *HTTPExporter) needsWorkerPoolRecreation(oldConfig, newConfig *types.HTTPExporterConfig) bool {
	if oldConfig == nil {
		return true // First time configuration
	}

	// Check if worker count changed
	if oldConfig.Workers != newConfig.Workers {
		return true
	}

	// Check if queue size changed
	if oldConfig.QueueSize != newConfig.QueueSize {
		return true
	}

	// Check if timeout changed significantly
	if oldConfig.Timeout != newConfig.Timeout {
		return true
	}

	return false
}

// GetStats returns current exporter statistics
func (e *HTTPExporter) GetStats() StatsSnapshot {
	return e.stats.GetSnapshot()
}

// GetHealthStatus returns the health status of the exporter
func (e *HTTPExporter) GetHealthStatus() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	stats := e.stats.GetSnapshot()

	health := map[string]interface{}{
		"started":        e.started,
		"uptime":         stats.GetUptime().String(),
		"workerCount":    e.workerPool.GetWorkerCount(),
		"queueLength":    e.workerPool.GetQueueLength(),
		"queueCapacity":  e.workerPool.GetQueueCapacity(),
		"totalExports":   stats.GetTotalExports(),
		"successRate":    fmt.Sprintf("%.1f%%", stats.GetSuccessRate()),
		"lastExportTime": stats.LastExportTime,
		"lastError":      stats.LastError,
		"lastErrorTime":  stats.LastErrorTime,
		"webhookCount":   len(e.config.Webhooks),
		"webhookHealth":  make(map[string]interface{}),
	}

	// Add per-webhook health
	webhookHealthMap, ok := health["webhookHealth"].(map[string]interface{})
	if ok {
		for name, webhookStats := range stats.WebhookStats {
			webhookHealthMap[name] = map[string]interface{}{
				"healthy":         webhookStats.IsHealthy(),
				"successRate":     fmt.Sprintf("%.1f%%", webhookStats.GetSuccessRate()),
				"totalRequests":   webhookStats.RequestsTotal,
				"lastSuccess":     webhookStats.LastSuccessTime,
				"lastError":       webhookStats.LastError,
				"avgResponseTime": webhookStats.AvgResponseTime.String(),
				"retryAttempts":   webhookStats.RetryAttempts,
			}
		}
	}

	return health
}

// GetConfiguration returns the current configuration (sanitized)
func (e *HTTPExporter) GetConfiguration() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	webhooks := make([]map[string]interface{}, len(e.config.Webhooks))
	for i, webhook := range e.config.Webhooks {
		webhooks[i] = map[string]interface{}{
			"name":          webhook.Name,
			"url":           webhook.URL,
			"authType":      webhook.Auth.Type,
			"timeout":       webhook.Timeout.String(),
			"sendStatus":    webhook.SendStatus,
			"sendProblems":  webhook.SendProblems,
			"maxAttempts":   webhook.Retry.MaxAttempts,
			"baseDelay":     webhook.Retry.BaseDelay.String(),
			"maxDelay":      webhook.Retry.MaxDelay.String(),
			"customHeaders": len(webhook.Headers),
		}
	}

	return map[string]interface{}{
		"enabled":       e.config.Enabled,
		"workers":       e.config.Workers,
		"queueSize":     e.config.QueueSize,
		"timeout":       e.config.Timeout.String(),
		"webhooks":      webhooks,
		"globalHeaders": len(e.config.Headers),
	}
}

// generateRequestID generates a unique request ID for tracing
func (e *HTTPExporter) generateRequestID(requestType string) string {
	return fmt.Sprintf("%s-%d-%s", requestType, time.Now().UnixNano(), e.settings.NodeName[:min(8, len(e.settings.NodeName))])
}

// healthCheckLoop periodically checks the health of webhooks
func (e *HTTPExporter) healthCheckLoop(ctx context.Context) {
	defer e.wg.Done()

	ticker := time.NewTicker(5 * time.Minute) // Health check every 5 minutes
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.stopCh:
			return
		case <-ticker.C:
			e.performHealthCheck()
		}
	}
}

// performHealthCheck checks the health of all webhooks
func (e *HTTPExporter) performHealthCheck() {
	stats := e.stats.GetSnapshot()
	healthyCount := 0
	totalCount := len(stats.WebhookStats)

	for name, webhookStats := range stats.WebhookStats {
		if webhookStats.IsHealthy() {
			healthyCount++
		} else {
			log.Printf("[WARN] Webhook %s is unhealthy: success rate %.1f%%, last success %v ago",
				name, webhookStats.GetSuccessRate(), time.Since(webhookStats.LastSuccessTime))
		}
	}

	if totalCount > 0 {
		healthyPercent := float64(healthyCount) / float64(totalCount) * 100
		log.Printf("[INFO] HTTP exporter health check: %d/%d webhooks healthy (%.1f%%)",
			healthyCount, totalCount, healthyPercent)

		if healthyPercent < 50 {
			log.Printf("[WARN] HTTP exporter health critical: less than 50%% of webhooks are healthy")
		}
	}
}

// min returns the smaller of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
