package kubernetes

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// KubernetesExporter exports Node Doctor status updates and problems to Kubernetes
// It implements the types.Exporter interface and manages events and node conditions
type KubernetesExporter struct {
	client           *K8sClient
	config           *types.KubernetesExporterConfig
	settings         *types.GlobalSettings
	eventManager     *EventManager
	conditionManager *ConditionManager
	mu               sync.RWMutex
	started          bool
	stopCh           chan struct{}
	wg               sync.WaitGroup
	stats            *ExporterStats
}

// ExporterStats tracks exporter statistics
type ExporterStats struct {
	mu                    sync.RWMutex
	StartTime             time.Time
	StatusExportsTotal    int64
	StatusExportsSuccess  int64
	StatusExportsFailed   int64
	ProblemExportsTotal   int64
	ProblemExportsSuccess int64
	ProblemExportsFailed  int64
	EventsCreated         int64
	ConditionsUpdated     int64
	LastExportTime        time.Time
	LastError             error
	LastErrorTime         time.Time
}

// NewKubernetesExporter creates a new Kubernetes exporter with the given configuration
func NewKubernetesExporter(config *types.KubernetesExporterConfig, settings *types.GlobalSettings) (*KubernetesExporter, error) {
	if !config.Enabled {
		return nil, fmt.Errorf("Kubernetes exporter is disabled")
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid Kubernetes exporter configuration: %w", err)
	}

	// Create Kubernetes client
	client, err := NewClient(config, settings)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Create managers
	eventManager := NewEventManager(client, config)
	conditionManager := NewConditionManager(client, config)

	exporter := &KubernetesExporter{
		client:           client,
		config:           config,
		settings:         settings,
		eventManager:     eventManager,
		conditionManager: conditionManager,
		stopCh:           make(chan struct{}),
		stats: &ExporterStats{
			StartTime: time.Now(),
		},
	}

	log.Printf("[INFO] Kubernetes exporter created for node %s", settings.NodeName)
	return exporter, nil
}

// Start begins the exporter background processes
func (ke *KubernetesExporter) Start(ctx context.Context) error {
	ke.mu.Lock()
	defer ke.mu.Unlock()

	if ke.started {
		return fmt.Errorf("Kubernetes exporter is already started")
	}

	log.Printf("[INFO] Starting Kubernetes exporter...")

	// Start sub-components
	ke.eventManager.Start(ctx)
	ke.conditionManager.Start(ctx)

	// Add custom conditions from configuration
	ke.conditionManager.AddCustomConditions()

	// Start background processes
	ke.wg.Add(2)
	go ke.healthCheckLoop(ctx)
	go ke.metricsLoop(ctx)

	// Initialize node annotations if configured
	if len(ke.config.Annotations) > 0 {
		go ke.initializeAnnotations(ctx)
	}

	ke.started = true
	ke.stats.StartTime = time.Now()

	log.Printf("[INFO] Kubernetes exporter started successfully")
	return nil
}

// Stop gracefully stops the exporter
func (ke *KubernetesExporter) Stop() error {
	ke.mu.Lock()
	defer ke.mu.Unlock()

	if !ke.started {
		return nil
	}

	log.Printf("[INFO] Stopping Kubernetes exporter...")

	// Signal shutdown
	close(ke.stopCh)

	// Stop sub-components
	ke.eventManager.Stop()
	ke.conditionManager.Stop()

	// Wait for background processes
	ke.wg.Wait()

	ke.started = false
	log.Printf("[INFO] Kubernetes exporter stopped")
	return nil
}

// ExportStatus exports a status update to Kubernetes as events and conditions
func (ke *KubernetesExporter) ExportStatus(ctx context.Context, status *types.Status) error {
	if !ke.isReady() {
		return fmt.Errorf("Kubernetes exporter is not ready")
	}

	ke.updateStats(func(s *ExporterStats) {
		s.StatusExportsTotal++
		s.LastExportTime = time.Now()
	})

	log.Printf("[DEBUG] Exporting status from %s with %d events, %d conditions",
		status.Source, len(status.Events), len(status.Conditions))

	var lastErr error

	// Export events
	if len(status.Events) > 0 {
		if err := ke.eventManager.CreateEventsFromStatus(ctx, status); err != nil {
			// Don't log here - EventManager logs aggregated summaries periodically
			// Some events may have succeeded even if others were rate limited
			lastErr = err
		} else {
			ke.updateStats(func(s *ExporterStats) {
				s.EventsCreated += int64(len(status.Events))
			})
		}
	}

	// Export conditions
	if len(status.Conditions) > 0 {
		for _, condition := range status.Conditions {
			ke.conditionManager.UpdateCondition(condition)
		}
		ke.updateStats(func(s *ExporterStats) {
			s.ConditionsUpdated += int64(len(status.Conditions))
		})
	}

	// Update statistics
	if lastErr != nil {
		ke.updateStats(func(s *ExporterStats) {
			s.StatusExportsFailed++
			s.LastError = lastErr
			s.LastErrorTime = time.Now()
		})
		return fmt.Errorf("status export completed with errors: %w", lastErr)
	}

	ke.updateStats(func(s *ExporterStats) {
		s.StatusExportsSuccess++
	})

	log.Printf("[DEBUG] Status export completed successfully")
	return nil
}

// ExportProblem exports a problem to Kubernetes as a node condition and event
func (ke *KubernetesExporter) ExportProblem(ctx context.Context, problem *types.Problem) error {
	if !ke.isReady() {
		return fmt.Errorf("Kubernetes exporter is not ready")
	}

	ke.updateStats(func(s *ExporterStats) {
		s.ProblemExportsTotal++
		s.LastExportTime = time.Now()
	})

	log.Printf("[DEBUG] Exporting problem: %s/%s (severity: %s)",
		problem.Type, problem.Resource, problem.Severity)

	var lastErr error

	// Create condition from problem
	ke.conditionManager.UpdateConditionFromProblem(problem)
	ke.updateStats(func(s *ExporterStats) {
		s.ConditionsUpdated++
	})

	// Create event from problem
	if err := ke.eventManager.CreateEventFromProblem(ctx, problem); err != nil {
		// Don't log here - EventManager logs aggregated summaries periodically
		lastErr = err
	} else {
		ke.updateStats(func(s *ExporterStats) {
			s.EventsCreated++
		})
	}

	// Update statistics
	if lastErr != nil {
		ke.updateStats(func(s *ExporterStats) {
			s.ProblemExportsFailed++
			s.LastError = lastErr
			s.LastErrorTime = time.Now()
		})
		return fmt.Errorf("problem export completed with errors: %w", lastErr)
	}

	ke.updateStats(func(s *ExporterStats) {
		s.ProblemExportsSuccess++
	})

	log.Printf("[DEBUG] Problem export completed successfully")
	return nil
}

// Reload implements types.ReloadableExporter interface for configuration reload.
func (ke *KubernetesExporter) Reload(config interface{}) error {
	// Type assert with safety check
	kubeConfig, ok := config.(*types.KubernetesExporterConfig)
	if !ok {
		return fmt.Errorf("invalid config type for Kubernetes exporter: expected *types.KubernetesExporterConfig, got %T", config)
	}

	// Validate before applying
	if kubeConfig == nil {
		return fmt.Errorf("kubernetes exporter config cannot be nil")
	}

	ke.mu.Lock()
	defer ke.mu.Unlock()

	log.Printf("[INFO] Reloading Kubernetes exporter configuration")

	// Validate new configuration
	if err := kubeConfig.Validate(); err != nil {
		return fmt.Errorf("new configuration validation failed: %w", err)
	}

	oldConfig := ke.config

	// Check if client recreation is needed
	if ke.needsClientRecreation(kubeConfig) {
		log.Printf("[INFO] Recreating Kubernetes client due to configuration changes")

		newClient, err := NewClient(kubeConfig, ke.settings)
		if err != nil {
			return fmt.Errorf("failed to create new Kubernetes client: %w", err)
		}

		// Replace client
		ke.client = newClient

		// Recreate managers with new client
		ke.eventManager.Stop()
		ke.conditionManager.Stop()

		ke.eventManager = NewEventManager(newClient, kubeConfig)
		ke.conditionManager = NewConditionManager(newClient, kubeConfig)

		// Restart managers if exporter is running
		if ke.started {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			ke.eventManager.Start(ctx)
			ke.conditionManager.Start(ctx)
			ke.conditionManager.AddCustomConditions()
		}
	} else {
		// Configuration changes that don't require recreating managers
		log.Printf("[DEBUG] Kubernetes exporter configuration unchanged")
	}

	// Update configuration
	ke.config = kubeConfig

	// Initialize new annotations if they were added
	if ke.started && len(kubeConfig.Annotations) > 0 && !ke.annotationsEqual(oldConfig.Annotations, kubeConfig.Annotations) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		go ke.initializeAnnotations(ctx)
	}

	log.Printf("[INFO] Successfully reloaded Kubernetes exporter configuration")
	log.Printf("[DEBUG] Config changes: conditions %d->%d, annotations %d->%d, namespace %s->%s, events.maxPerMinute %d->%d",
		len(oldConfig.Conditions), len(kubeConfig.Conditions),
		len(oldConfig.Annotations), len(kubeConfig.Annotations),
		oldConfig.Namespace, kubeConfig.Namespace,
		oldConfig.Events.MaxEventsPerMinute, kubeConfig.Events.MaxEventsPerMinute)

	return nil
}

// IsReloadable implements types.ReloadableExporter interface.
func (ke *KubernetesExporter) IsReloadable() bool {
	return true
}

// needsClientRecreation determines if the Kubernetes client needs to be recreated
// based on configuration changes that affect client behavior.
func (ke *KubernetesExporter) needsClientRecreation(newConfig *types.KubernetesExporterConfig) bool {
	if ke.config == nil {
		return true // First time configuration
	}

	// Check if namespace changed
	if ke.config.Namespace != newConfig.Namespace {
		return true
	}

	// Check if update intervals changed significantly (affects rate limiting)
	if ke.config.UpdateInterval != newConfig.UpdateInterval ||
		ke.config.ResyncInterval != newConfig.ResyncInterval ||
		ke.config.HeartbeatInterval != newConfig.HeartbeatInterval {
		return true
	}

	// Check if event configuration changed significantly (affects event client behavior)
	if ke.config.Events.MaxEventsPerMinute != newConfig.Events.MaxEventsPerMinute ||
		ke.config.Events.EventTTL != newConfig.Events.EventTTL ||
		ke.config.Events.DeduplicationWindow != newConfig.Events.DeduplicationWindow {
		return true
	}

	return false
}

// annotationsEqual compares two annotation slices for equality
func (ke *KubernetesExporter) annotationsEqual(old, new []types.AnnotationConfig) bool {
	if len(old) != len(new) {
		return false
	}

	oldMap := make(map[string]string)
	for _, ann := range old {
		oldMap[ann.Key] = ann.Value
	}

	for _, ann := range new {
		if oldMap[ann.Key] != ann.Value {
			return false
		}
	}

	return true
}

// GetStats returns exporter statistics
func (ke *KubernetesExporter) GetStats() map[string]interface{} {
	ke.stats.mu.RLock()
	defer ke.stats.mu.RUnlock()

	stats := map[string]interface{}{
		"start_time":              ke.stats.StartTime.Format(time.RFC3339),
		"uptime":                  time.Since(ke.stats.StartTime).String(),
		"status_exports_total":    ke.stats.StatusExportsTotal,
		"status_exports_success":  ke.stats.StatusExportsSuccess,
		"status_exports_failed":   ke.stats.StatusExportsFailed,
		"problem_exports_total":   ke.stats.ProblemExportsTotal,
		"problem_exports_success": ke.stats.ProblemExportsSuccess,
		"problem_exports_failed":  ke.stats.ProblemExportsFailed,
		"events_created":          ke.stats.EventsCreated,
		"conditions_updated":      ke.stats.ConditionsUpdated,
		"last_export_time":        ke.stats.LastExportTime.Format(time.RFC3339),
		"started":                 ke.started,
		"node_name":               ke.settings.NodeName,
		"namespace":               ke.config.Namespace,
	}

	if !ke.stats.LastErrorTime.IsZero() {
		stats["last_error"] = ke.stats.LastError.Error()
		stats["last_error_time"] = ke.stats.LastErrorTime.Format(time.RFC3339)
	}

	// Add sub-component stats
	stats["event_manager"] = ke.eventManager.GetStats()
	stats["condition_manager"] = ke.conditionManager.GetStats()

	return stats
}

// IsHealthy returns true if the exporter and all its components are healthy
func (ke *KubernetesExporter) IsHealthy() bool {
	if !ke.isReady() {
		return false
	}

	// Check client health
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if !ke.client.IsHealthy(ctx) {
		log.Printf("[WARN] Kubernetes client is not healthy")
		return false
	}

	// Check sub-component health
	if !ke.eventManager.IsHealthy() {
		log.Printf("[WARN] Event manager is not healthy")
		return false
	}

	if !ke.conditionManager.IsHealthy() {
		log.Printf("[WARN] Condition manager is not healthy")
		return false
	}

	return true
}

// GetNodeName returns the node name this exporter is configured for
func (ke *KubernetesExporter) GetNodeName() string {
	return ke.settings.NodeName
}

// GetConfiguration returns the exporter configuration
func (ke *KubernetesExporter) GetConfiguration() *types.KubernetesExporterConfig {
	return ke.config
}

// Internal helper methods

// isReady returns true if the exporter is started and ready to handle requests
func (ke *KubernetesExporter) isReady() bool {
	ke.mu.RLock()
	defer ke.mu.RUnlock()
	return ke.started
}

// updateStats safely updates exporter statistics
func (ke *KubernetesExporter) updateStats(updateFunc func(*ExporterStats)) {
	ke.stats.mu.Lock()
	defer ke.stats.mu.Unlock()
	updateFunc(ke.stats)
}

// healthCheckLoop periodically checks the health of sub-components
func (ke *KubernetesExporter) healthCheckLoop(ctx context.Context) {
	defer ke.wg.Done()

	ticker := time.NewTicker(30 * time.Second) // Health check every 30 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("[DEBUG] Kubernetes exporter health check loop stopping due to context cancellation")
			return
		case <-ke.stopCh:
			log.Printf("[DEBUG] Kubernetes exporter health check loop stopping due to stop signal")
			return
		case <-ticker.C:
			ke.performHealthCheck(ctx)
		}
	}
}

// performHealthCheck checks the health of all components
func (ke *KubernetesExporter) performHealthCheck(ctx context.Context) {
	healthy := ke.IsHealthy()
	if !healthy {
		log.Printf("[WARN] Kubernetes exporter health check failed")
	} else {
		log.Printf("[DEBUG] Kubernetes exporter health check passed")
	}

	// Update health condition
	healthCondition := types.NewCondition(
		"NodeDoctorExporterHealthy",
		types.ConditionTrue,
		"HealthCheck",
		"Kubernetes exporter is healthy",
	)

	if !healthy {
		healthCondition.Status = types.ConditionFalse
		healthCondition.Reason = "HealthCheckFailed"
		healthCondition.Message = "Kubernetes exporter health check failed"
	}

	ke.conditionManager.UpdateCondition(healthCondition)
}

// metricsLoop periodically logs metrics and statistics
func (ke *KubernetesExporter) metricsLoop(ctx context.Context) {
	defer ke.wg.Done()

	ticker := time.NewTicker(5 * time.Minute) // Log metrics every 5 minutes
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("[DEBUG] Kubernetes exporter metrics loop stopping due to context cancellation")
			return
		case <-ke.stopCh:
			log.Printf("[DEBUG] Kubernetes exporter metrics loop stopping due to stop signal")
			return
		case <-ticker.C:
			ke.logMetrics()
		}
	}
}

// logMetrics logs current exporter metrics
func (ke *KubernetesExporter) logMetrics() {
	stats := ke.GetStats()
	log.Printf("[INFO] Kubernetes exporter metrics: status_exports=%d, problem_exports=%d, events_created=%d, conditions_updated=%d, uptime=%s",
		stats["status_exports_total"], stats["problem_exports_total"],
		stats["events_created"], stats["conditions_updated"], stats["uptime"])
}

// initializeAnnotations sets up configured node annotations
func (ke *KubernetesExporter) initializeAnnotations(ctx context.Context) {
	if len(ke.config.Annotations) == 0 {
		return
	}

	log.Printf("[DEBUG] Initializing %d node annotations", len(ke.config.Annotations))

	annotations := make(map[string]string)
	for _, annotation := range ke.config.Annotations {
		annotations[annotation.Key] = annotation.Value
	}

	// Add timestamp annotation to show when Node Doctor last updated
	annotations["node-doctor.io/last-update"] = time.Now().Format(time.RFC3339)

	if err := ke.client.UpdateNodeAnnotations(ctx, annotations); err != nil {
		log.Printf("[WARN] Failed to initialize node annotations: %v", err)
		return
	}

	log.Printf("[DEBUG] Successfully initialized node annotations")
}

// ForceFlush immediately flushes all pending updates (primarily for testing)
func (ke *KubernetesExporter) ForceFlush(ctx context.Context) error {
	if !ke.isReady() {
		return fmt.Errorf("exporter is not ready")
	}
	return ke.conditionManager.ForceFlush(ctx)
}

// GetClient returns the underlying Kubernetes client (primarily for testing)
func (ke *KubernetesExporter) GetClient() *K8sClient {
	return ke.client
}

// GetEventManager returns the event manager (primarily for testing)
func (ke *KubernetesExporter) GetEventManager() *EventManager {
	return ke.eventManager
}

// GetConditionManager returns the condition manager (primarily for testing)
func (ke *KubernetesExporter) GetConditionManager() *ConditionManager {
	return ke.conditionManager
}

// ClearManagedConditions removes all node-doctor managed conditions except the heartbeat.
// This should be called when monitors are disabled or during cleanup.
func (ke *KubernetesExporter) ClearManagedConditions() []string {
	if !ke.isReady() {
		log.Printf("[WARN] Cannot clear conditions: exporter is not ready")
		return nil
	}
	return ke.conditionManager.ClearManagedConditions()
}

// RemoveCondition removes a specific condition from the node.
func (ke *KubernetesExporter) RemoveCondition(conditionType string) {
	if !ke.isReady() {
		log.Printf("[WARN] Cannot remove condition %s: exporter is not ready", conditionType)
		return
	}
	ke.conditionManager.RemoveCondition(conditionType)
}

// GetManagedConditionTypes returns all condition types currently managed by node-doctor.
func (ke *KubernetesExporter) GetManagedConditionTypes() []string {
	if !ke.isReady() {
		return nil
	}
	return ke.conditionManager.GetManagedConditionTypes()
}
