package detector

import (
	"context"
	"fmt"
	"log"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/supporttools/node-doctor/pkg/exporters/kubernetes"
	"github.com/supporttools/node-doctor/pkg/reload"
	"github.com/supporttools/node-doctor/pkg/types"
)

// MonitorHandle represents a running monitor with its context and controls
type MonitorHandle struct {
	monitor    types.Monitor
	config     types.MonitorConfig
	statusCh   <-chan *types.Status
	cancelFunc context.CancelFunc
	wg         *sync.WaitGroup
	ctx        context.Context
	stopped    bool
	mu         sync.Mutex
}

// Stop stops the monitor gracefully with a timeout
func (mh *MonitorHandle) Stop() error {
	mh.mu.Lock()

	// Check and set atomically under lock to fix TOCTOU race condition
	if mh.stopped {
		mh.mu.Unlock()
		return nil // Already stopped
	}
	mh.stopped = true
	mh.mu.Unlock()

	// Now do the actual stop work without holding the lock
	log.Printf("[INFO] Stopping monitor %s", mh.config.Name)

	// Cancel the context first to signal stop
	mh.cancelFunc()

	// Then stop the monitor
	mh.monitor.Stop()

	// Wait for goroutines to finish with timeout
	done := make(chan struct{})
	go func() {
		mh.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Printf("[INFO] Monitor %s stopped cleanly", mh.config.Name)
		return nil
	case <-time.After(5 * time.Second):
		log.Printf("[WARN] Monitor %s stop timeout after 5s", mh.config.Name)
		return fmt.Errorf("monitor stop timeout")
	}
}

// GetName returns the name of the monitor
func (mh *MonitorHandle) GetName() string {
	return mh.config.Name
}

// GetConfig returns the configuration of the monitor
func (mh *MonitorHandle) GetConfig() types.MonitorConfig {
	return mh.config
}

// IsRunning returns true if the monitor is currently running
func (mh *MonitorHandle) IsRunning() bool {
	mh.mu.Lock()
	defer mh.mu.Unlock()
	return !mh.stopped
}

// ProblemDetector manages monitors and exports their output
type ProblemDetector struct {
	mu                sync.RWMutex
	config            *types.NodeDoctorConfig
	monitorHandles    []*MonitorHandle
	exporters         []types.Exporter
	statusChan        chan *types.Status
	ctx               context.Context
	cancel            context.CancelFunc
	wg                sync.WaitGroup
	stats             *Statistics
	configWatcher     *reload.ConfigWatcher
	reloadCoordinator *reload.ReloadCoordinator
	configFilePath    string
	monitorFactory    MonitorFactory
	configChangeCh    <-chan struct{}
	reloadMutex       sync.Mutex // Protects reload operations
	started           bool
	passedMonitors    []types.Monitor // Monitors passed directly to constructor
}

// MonitorFactory interface for creating monitor instances during hot reload
type MonitorFactory interface {
	CreateMonitor(config types.MonitorConfig) (types.Monitor, error)
}

// NewProblemDetector creates a new problem detector with the given configuration
func NewProblemDetector(config *types.NodeDoctorConfig, monitors []types.Monitor, exporters []types.Exporter, configFilePath string, monitorFactory MonitorFactory) (*ProblemDetector, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	if configFilePath == "" {
		return nil, fmt.Errorf("config file path cannot be empty")
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	// Create statistics tracker
	monitorStats := NewStatistics()

	ctx, cancel := context.WithCancel(context.Background())

	// Create config watcher with configFilePath and debounce interval
	debounceInterval := config.Reload.DebounceInterval
	if debounceInterval == 0 {
		debounceInterval = 500 * time.Millisecond // default
	}
	configWatcher, err := reload.NewConfigWatcher(configFilePath, debounceInterval)
	if err != nil {
		cancel() // Clean up the context we created
		return nil, fmt.Errorf("failed to create config watcher: %w", err)
	}

	pd := &ProblemDetector{
		config:         config,
		monitorHandles: make([]*MonitorHandle, 0),
		exporters:      make([]types.Exporter, 0),
		statusChan:     make(chan *types.Status, 1000),
		ctx:            ctx,
		cancel:         cancel,
		stats:          monitorStats,
		configWatcher:  configWatcher,
		configFilePath: configFilePath,
		monitorFactory: monitorFactory,
		passedMonitors: monitors, // Store passed monitors to start in Start()
	}

	// Create reload coordinator with callback and event emitter
	reloadCallback := pd.handleConfigReload
	eventEmitter := pd.emitReloadEvent
	pd.reloadCoordinator = reload.NewReloadCoordinator(configFilePath, config, reloadCallback, eventEmitter)

	for _, exporter := range exporters {
		pd.AddExporter(exporter)
	}

	return pd, nil
}

// AddExporter adds an exporter to the detector
func (pd *ProblemDetector) AddExporter(exporter types.Exporter) {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	pd.exporters = append(pd.exporters, exporter)
	log.Printf("[INFO] Added exporter to detector")
}

// IsRunning returns true if the detector is currently running
func (pd *ProblemDetector) IsRunning() bool {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	return pd.started
}

// Run starts the problem detector (alias for Start for backward compatibility)
func (pd *ProblemDetector) Run() error {
	return pd.Start()
}

// GetStatistics returns the current statistics (copy to avoid lock sharing)
func (pd *ProblemDetector) GetStatistics() Statistics {
	return pd.stats.Copy()
}

// Start starts the problem detector
func (pd *ProblemDetector) Start() error {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	if pd.started {
		return fmt.Errorf("detector already started")
	}

	log.Printf("[INFO] Starting problem detector...")

	// Start config watcher
	configChangeCh, err := pd.configWatcher.Start(pd.ctx)
	if err != nil {
		return fmt.Errorf("failed to start config watcher: %w", err)
	}
	pd.configChangeCh = configChangeCh

	// Start watching for config changes
	pd.wg.Add(1)
	go func() {
		defer pd.wg.Done()
		pd.watchConfigChanges()
	}()

	// Start monitors from config (requires MonitorFactory)
	for _, monitorConfig := range pd.config.Monitors {
		monitor, err := pd.createMonitor(monitorConfig)
		if err != nil {
			log.Printf("[ERROR] Failed to create monitor %s: %v", monitorConfig.Name, err)
			pd.stats.IncrementMonitorsFailed()
			continue
		}

		if err := pd.addMonitor(pd.ctx, monitor, monitorConfig); err != nil {
			log.Printf("[ERROR] Failed to start monitor %s: %v", monitorConfig.Name, err)
			pd.stats.IncrementMonitorsFailed()
			continue
		}

		log.Printf("[INFO] Started monitor: %s", monitorConfig.Name)
	}

	// Start passed monitors (for testing or programmatic use)
	for i, monitor := range pd.passedMonitors {
		// Create a synthetic config for passed monitors
		monitorConfig := types.MonitorConfig{
			Name:    fmt.Sprintf("passed-monitor-%d", i),
			Enabled: true,
		}

		if err := pd.addMonitor(pd.ctx, monitor, monitorConfig); err != nil {
			log.Printf("[ERROR] Failed to start passed monitor %d: %v", i, err)
			pd.stats.IncrementMonitorsFailed()
			continue
		}

		log.Printf("[INFO] Started passed monitor: %s", monitorConfig.Name)
	}

	// Start status processing goroutine
	pd.wg.Add(1)
	go func() {
		defer pd.wg.Done()
		pd.processStatuses()
	}()

	pd.started = true
	log.Printf("[INFO] Problem detector started successfully with %d monitors", len(pd.monitorHandles))

	return nil
}

// Stop stops the problem detector gracefully
func (pd *ProblemDetector) Stop() error {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	if !pd.started {
		return nil // Already stopped
	}

	log.Printf("[INFO] Stopping problem detector...")

	// Stop config watcher
	pd.configWatcher.Stop()

	// Stop all monitors
	for _, handle := range pd.monitorHandles {
		if err := handle.Stop(); err != nil {
			log.Printf("[WARN] Error stopping monitor %s: %v", handle.GetName(), err)
		}
	}

	// Cancel context to stop all goroutines
	pd.cancel()

	// Close status channel
	close(pd.statusChan)

	// Wait for all goroutines to finish
	pd.wg.Wait()

	pd.started = false
	log.Printf("[INFO] Problem detector stopped")

	return nil
}

// createMonitor creates a monitor based on configuration using the MonitorFactory
func (pd *ProblemDetector) createMonitor(config types.MonitorConfig) (types.Monitor, error) {
	if pd.monitorFactory == nil {
		return nil, fmt.Errorf("monitor factory not initialized")
	}

	// Use the factory to create the monitor
	monitor, err := pd.monitorFactory.CreateMonitor(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create monitor %s: %w", config.Name, err)
	}

	return monitor, nil
}

// processStatuses processes status updates from monitors and forwards to exporters
func (pd *ProblemDetector) processStatuses() {
	for {
		select {
		case <-pd.ctx.Done():
			log.Printf("[DEBUG] Status processor stopping")
			return
		case status, ok := <-pd.statusChan:
			if !ok {
				log.Printf("[DEBUG] Status channel closed, processor stopping")
				return
			}

			if status == nil {
				continue
			}

			pd.processStatus(status)
		}
	}
}

// processStatus processes a single status update
func (pd *ProblemDetector) processStatus(status *types.Status) {
	log.Printf("[DEBUG] Processing status from %s", status.Source)

	// Update statistics
	pd.stats.IncrementStatusesReceived()

	// Export to all exporters (single path - Status contains all data)
	// Note: Previously this also called ExportProblem() for converted problems,
	// causing duplicate Kubernetes resources. See GitHub issue #7.
	for _, exporter := range pd.exporters {
		if err := exporter.ExportStatus(pd.ctx, status); err != nil {
			log.Printf("[WARN] Failed to export status to exporter: %v", err)
			pd.stats.IncrementExportsFailed()
		} else {
			pd.stats.IncrementExportsSucceeded()
		}
	}
}

// fanInFromMonitor reads statuses from a monitor and forwards them to the main status channel
func (pd *ProblemDetector) fanInFromMonitor(ctx context.Context, statusCh <-chan *types.Status, monitorName string) {
	log.Printf("[DEBUG] Starting fan-in for monitor %s", monitorName)
	defer log.Printf("[DEBUG] Fan-in stopped for monitor %s", monitorName)

	for {
		select {
		case <-ctx.Done():
			return
		case status, ok := <-statusCh:
			if !ok {
				log.Printf("[DEBUG] Status channel closed for monitor %s", monitorName)
				return
			}

			if status != nil {
				select {
				case pd.statusChan <- status:
					// Status sent successfully
				case <-ctx.Done():
					return
				case <-time.After(5 * time.Second):
					log.Printf("[WARN] Timeout sending status from monitor %s", monitorName)
				}
			}
		}
	}
}

// watchConfigChanges watches for configuration changes and triggers reloads
func (pd *ProblemDetector) watchConfigChanges() {
	log.Printf("[DEBUG] Starting config change watcher")
	defer log.Printf("[DEBUG] Config change watcher stopped")

	for {
		select {
		case <-pd.ctx.Done():
			return
		case _, ok := <-pd.configChangeCh:
			if !ok {
				log.Printf("[DEBUG] Config change channel closed")
				return
			}

			log.Printf("[INFO] Configuration file changed")
			if err := pd.reloadCoordinator.TriggerReload(pd.ctx); err != nil {
				log.Printf("[ERROR] Failed to trigger config reload: %v", err)
				pd.emitReloadEvent(types.EventError, "ReloadFailed", fmt.Sprintf("Failed to trigger reload: %v", err))
			}
		}
	}
}

// handleConfigReload handles configuration reload requests
func (pd *ProblemDetector) handleConfigReload(ctx context.Context, newConfig *types.NodeDoctorConfig, diff *reload.ConfigDiff) error {
	log.Printf("[INFO] Applying configuration reload")

	// Log summary of changes
	if !diff.HasChanges() {
		log.Printf("[INFO] No configuration changes detected")
		pd.emitReloadEvent(types.EventInfo, "NoChanges", "Configuration reload completed with no changes")
		return nil
	}

	log.Printf("[INFO] Config changes detected: %d monitors added, %d modified, %d removed",
		len(diff.MonitorsAdded), len(diff.MonitorsModified), len(diff.MonitorsRemoved))

	// Apply the reload
	if err := pd.applyConfigReload(ctx, newConfig, diff); err != nil {
		log.Printf("[ERROR] Configuration reload failed: %v", err)
		pd.emitReloadEvent(types.EventError, "ReloadFailed", fmt.Sprintf("Configuration reload failed: %v", err))
		return fmt.Errorf("configuration reload failed: %w", err)
	}

	log.Printf("[INFO] Configuration reload completed successfully")
	pd.emitReloadEvent(types.EventInfo, "ReloadSuccess", "Configuration reload completed successfully")

	return nil
}

// applyConfigReload applies configuration changes with fail-fast logic for critical errors
func (pd *ProblemDetector) applyConfigReload(ctx context.Context, newConfig *types.NodeDoctorConfig, diff *reload.ConfigDiff) error {
	pd.reloadMutex.Lock()
	defer pd.reloadMutex.Unlock()

	var errors []error
	var criticalErrors []error

	// Step 1: Stop monitors that were removed and cleanup their conditions
	log.Printf("[INFO] Stopping %d removed monitors", len(diff.MonitorsRemoved))
	for _, removedConfig := range diff.MonitorsRemoved {
		if err := pd.stopMonitorByName(removedConfig.Name); err != nil {
			log.Printf("[ERROR] Failed to stop monitor %s: %v", removedConfig.Name, err)
			errors = append(errors, fmt.Errorf("failed to stop monitor %s: %w", removedConfig.Name, err))
			// Stopping monitors is not critical - continue
		}
		// Clean up conditions associated with this monitor type
		pd.cleanupMonitorConditions(removedConfig.Type)
	}

	// Step 2: Restart modified monitors
	log.Printf("[INFO] Restarting %d modified monitors", len(diff.MonitorsModified))
	for _, modifiedChange := range diff.MonitorsModified {
		newConfig := modifiedChange.New

		// Stop existing
		if err := pd.stopMonitorByName(newConfig.Name); err != nil {
			log.Printf("[ERROR] Failed to stop modified monitor %s: %v", newConfig.Name, err)
			criticalErrors = append(criticalErrors, fmt.Errorf("failed to stop modified monitor %s: %w", newConfig.Name, err))
			continue // Skip this monitor but continue with others
		}

		// Create new
		monitor, err := pd.createMonitor(newConfig)
		if err != nil {
			log.Printf("[ERROR] Failed to create modified monitor %s: %v", newConfig.Name, err)
			criticalErrors = append(criticalErrors, fmt.Errorf("failed to create modified monitor %s: %w", newConfig.Name, err))
			continue
		}

		// Start new
		if err := pd.addMonitor(ctx, monitor, newConfig); err != nil {
			log.Printf("[ERROR] Failed to start modified monitor %s: %v", newConfig.Name, err)
			criticalErrors = append(criticalErrors, fmt.Errorf("failed to start modified monitor %s: %w", newConfig.Name, err))
		}
	}

	// Step 3: Start new monitors
	log.Printf("[INFO] Starting %d new monitors", len(diff.MonitorsAdded))
	for _, addedConfig := range diff.MonitorsAdded {
		monitor, err := pd.createMonitor(addedConfig)
		if err != nil {
			log.Printf("[ERROR] Failed to create new monitor %s: %v", addedConfig.Name, err)
			criticalErrors = append(criticalErrors, fmt.Errorf("failed to create new monitor %s: %w", addedConfig.Name, err))
			continue
		}

		if err := pd.addMonitor(ctx, monitor, addedConfig); err != nil {
			log.Printf("[ERROR] Failed to start new monitor %s: %v", addedConfig.Name, err)
			criticalErrors = append(criticalErrors, fmt.Errorf("failed to start new monitor %s: %w", addedConfig.Name, err))
		}
	}

	// Step 4: Reload exporters (critical operation)
	if diff.ExportersChanged {
		log.Printf("[INFO] Reloading exporters due to configuration changes")

		for _, exporter := range pd.exporters {
			exporterType := pd.getExporterType(exporter)

			if err := pd.reloadExporter(exporter, newConfig); err != nil {
				log.Printf("[ERROR] Failed to reload %s exporter: %v", exporterType, err)
				// Exporter reload failures are CRITICAL
				criticalErrors = append(criticalErrors, fmt.Errorf("critical: failed to reload %s exporter: %w", exporterType, err))
			}
		}
	}

	// Step 5: Update configuration ONLY if no critical errors
	if len(criticalErrors) > 0 {
		log.Printf("[ERROR] Configuration reload failed with %d critical errors", len(criticalErrors))

		// Build detailed error message
		errorMsg := fmt.Sprintf("reload failed with %d critical error(s):\n", len(criticalErrors))
		for i, err := range criticalErrors {
			errorMsg += fmt.Sprintf("  %d. %v\n", i+1, err)
		}

		// Emit event with details
		pd.emitReloadEvent(types.EventError, "ReloadPartialFailure", errorMsg)

		// Return combined error
		return fmt.Errorf("configuration reload partially failed: %w", criticalErrors[0])
	}

	// Only update config if reload was fully successful
	pd.mu.Lock()
	pd.config = newConfig
	pd.mu.Unlock()

	// Report any non-critical warnings
	if len(errors) > 0 {
		log.Printf("[WARN] Configuration reload succeeded with %d warnings", len(errors))
		for _, err := range errors {
			log.Printf("[WARN] - %v", err)
		}
	}

	return nil
}

// getExporterType returns a string representation of the exporter type
func (pd *ProblemDetector) getExporterType(exporter types.Exporter) string {
	exporterType := reflect.TypeOf(exporter)
	if exporterType.Kind() == reflect.Ptr {
		exporterType = exporterType.Elem()
	}

	typeName := exporterType.Name()

	// Convert common naming patterns
	if strings.Contains(strings.ToLower(typeName), "kubernetes") {
		return "kubernetes"
	}
	if strings.Contains(strings.ToLower(typeName), "http") {
		return "http"
	}
	if strings.Contains(strings.ToLower(typeName), "prometheus") {
		return "prometheus"
	}

	return strings.ToLower(typeName)
}

// stopMonitorByName stops a monitor by its name and removes it from the handles list
func (pd *ProblemDetector) stopMonitorByName(name string) error {
	for i, handle := range pd.monitorHandles {
		if handle.GetName() == name {
			if err := handle.Stop(); err != nil {
				return err
			}

			// Remove from handles list
			pd.monitorHandles = append(pd.monitorHandles[:i], pd.monitorHandles[i+1:]...)
			log.Printf("[INFO] Stopped and removed monitor: %s", name)
			return nil
		}
	}

	return fmt.Errorf("monitor %s not found", name)
}

// addMonitor adds a new monitor with its handle and starts it
func (pd *ProblemDetector) addMonitor(ctx context.Context, monitor types.Monitor, config types.MonitorConfig) error {
	// Create monitor handle
	monitorCtx, cancel := context.WithCancel(ctx)
	handle := &MonitorHandle{
		monitor:    monitor,
		config:     config,
		ctx:        monitorCtx,
		cancelFunc: cancel,
		wg:         &sync.WaitGroup{},
	}

	// Start the monitor
	statusCh, err := monitor.Start()
	if err != nil {
		cancel() // Clean up context
		return fmt.Errorf("failed to start monitor: %w", err)
	}

	handle.statusCh = statusCh

	// Start fan-in goroutine
	pd.wg.Add(1)
	handle.wg.Add(1)
	go func() {
		defer pd.wg.Done()
		defer handle.wg.Done()
		pd.fanInFromMonitor(handle.ctx, handle.statusCh, handle.GetName())
	}()

	// Add to handles list
	pd.monitorHandles = append(pd.monitorHandles, handle)

	pd.stats.IncrementMonitorsStarted()
	return nil
}

// reloadExporter attempts to reload an exporter if it supports the ReloadableExporter interface
func (pd *ProblemDetector) reloadExporter(exporter types.Exporter, newConfig *types.NodeDoctorConfig) error {
	if exporter == nil {
		return fmt.Errorf("cannot reload nil exporter")
	}

	if newConfig == nil {
		return fmt.Errorf("cannot reload with nil config")
	}

	// Check if exporter implements ReloadableExporter interface
	reloadableExporter, ok := exporter.(types.ReloadableExporter)
	if !ok {
		log.Printf("[DEBUG] Exporter does not implement ReloadableExporter interface, skipping reload")
		return nil
	}

	if !reloadableExporter.IsReloadable() {
		log.Printf("[DEBUG] Exporter reports it is not reloadable, skipping reload")
		return nil
	}

	// Determine which configuration to pass based on exporter type
	exporterType := pd.getExporterType(exporter)
	var exporterConfig interface{}

	switch exporterType {
	case "kubernetes":
		if newConfig.Exporters.Kubernetes != nil {
			exporterConfig = newConfig.Exporters.Kubernetes
		} else {
			return fmt.Errorf("kubernetes exporter config not found in new configuration")
		}
	case "http":
		if newConfig.Exporters.HTTP != nil {
			exporterConfig = newConfig.Exporters.HTTP
		} else {
			return fmt.Errorf("http exporter config not found in new configuration")
		}
	case "prometheus":
		if newConfig.Exporters.Prometheus != nil {
			exporterConfig = newConfig.Exporters.Prometheus
		} else {
			return fmt.Errorf("prometheus exporter config not found in new configuration")
		}
	default:
		return fmt.Errorf("unknown exporter type: %s", exporterType)
	}

	// Call the Reload method
	log.Printf("[INFO] Reloading %s exporter configuration", exporterType)
	if err := reloadableExporter.Reload(exporterConfig); err != nil {
		return fmt.Errorf("failed to reload %s exporter: %w", exporterType, err)
	}

	log.Printf("[INFO] Successfully reloaded %s exporter configuration", exporterType)
	return nil
}

// emitReloadEvent emits a reload status event.
func (pd *ProblemDetector) emitReloadEvent(severity types.EventSeverity, reason, message string) {
	// Create a reload event
	status := &types.Status{
		Source:    "config-reload",
		Timestamp: time.Now(),
		Events: []types.Event{
			{
				Severity:  severity,
				Timestamp: time.Now(),
				Reason:    reason,
				Message:   message,
			},
		},
		Conditions: []types.Condition{},
	}

	// Send to status channel for export
	select {
	case pd.statusChan <- status:
		// Event sent
	default:
		// Channel full, log warning
		log.Printf("[WARN] Status channel full, reload event dropped: %s", reason)
	}
}

// getKubernetesExporter returns the Kubernetes exporter if present, nil otherwise.
func (pd *ProblemDetector) getKubernetesExporter() *kubernetes.KubernetesExporter {
	for _, exporter := range pd.exporters {
		if ke, ok := exporter.(*kubernetes.KubernetesExporter); ok {
			return ke
		}
	}
	return nil
}

// cleanupMonitorConditions clears conditions for a specific monitor type when it's disabled.
// This maps monitor types to their associated condition types.
func (pd *ProblemDetector) cleanupMonitorConditions(monitorType string) {
	ke := pd.getKubernetesExporter()
	if ke == nil {
		return // No Kubernetes exporter configured
	}

	// Map monitor types to their associated condition types
	// These condition types match what each monitor actually creates
	conditionMap := map[string][]string{
		"system-cpu":    {"CPUHealthy", "CPUPressure", "CPUThermalHealthy"},
		"system-memory": {"MemoryHealthy", "MemoryPressure"},
		"system-disk":   {"DiskHealthy", "DiskPressure", "InodePressure", "ReadonlyFilesystem"},
		"network-dns-check": {
			"ClusterDNSDegraded", "ClusterDNSDown", "ClusterDNSHealthy", "ClusterDNSIntermittent",
			"CustomDNSDown", "CustomDNSHealthy",
			"DNSResolutionConsistent", "DNSResolutionDegraded", "DNSResolutionDown",
			"DNSResolutionInconsistent", "DNSResolutionIntermittent",
			"ExternalDNSDegraded", "ExternalDNSIntermittent",
			"NetworkReachable", "NetworkUnreachable",
		},
		"network-gateway-check": {"NetworkUnreachable"},
		"network-cni-check": {
			"CNIConfigValid", "CNIHealthy", "CNIInterfacesHealthy",
			"NetworkDegraded", "NetworkPartitioned",
		},
	}

	conditions, ok := conditionMap[monitorType]
	if !ok {
		log.Printf("[DEBUG] No condition mapping for monitor type: %s", monitorType)
		return
	}

	log.Printf("[INFO] Cleaning up %d conditions for disabled monitor type %s: %v", len(conditions), monitorType, conditions)
	for _, condType := range conditions {
		// Add the NodeDoctor prefix that the exporter adds when creating conditions
		prefixedCondType := "NodeDoctor" + condType
		ke.RemoveCondition(prefixedCondType)
	}
}
