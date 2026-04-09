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

	// Remediation
	remediatorRegistry RemediationExecutor
	configIndexMu      sync.RWMutex                   // Protects monitorConfigIndex (separate from pd.mu to avoid deadlock in addMonitor callers)
	monitorConfigIndex map[string]types.MonitorConfig // monitor name -> config (for remediation lookup)
	handlesMu          sync.Mutex                     // Protects monitorHandles slice (inner lock; never held when acquiring pd.mu or reloadMutex)

	// Dependency scheduling.
	// lastStatus caches each monitor's most recent *effective* *types.Status for blocked-state
	// evaluation. It is written on every processStatus call (continuously at runtime) and is
	// therefore NOT read-only after Start(). Access is protected by lastStatusMu.
	// dependents is a reverse-lookup from dependency name → monitors that declared it in DependsOn.
	// It is written once during Start() and is effectively read-only thereafter (no mutex needed).
	// Reserved: currently unused at runtime. Intended for a future push-notification model where
	// a dependency state-change immediately re-evaluates its declared dependents rather than
	// waiting for the dependent's next scheduled status emission.
	lastStatusMu sync.RWMutex
	lastStatus   map[string]*types.Status // monitor name -> most recent effective status (protected by lastStatusMu)
	dependents   map[string][]string      // dependency name -> monitors that depend on it (written once in Start; reserved for future push model)
}

// MonitorFactory interface for creating monitor instances during hot reload
type MonitorFactory interface {
	CreateMonitor(config types.MonitorConfig) (types.Monitor, error)
}

// RemediationExecutor is the minimal interface the detector needs from the remediator registry.
// Using an interface (rather than the concrete *remediators.RemediatorRegistry) keeps the
// detector package free of the remediators dependency and makes unit tests straightforward.
type RemediationExecutor interface {
	// Remediate executes the named remediator strategy for the given problem.
	Remediate(ctx context.Context, remediatorType string, problem types.Problem) error
	// IsDryRun reports whether the executor is running in dry-run mode.
	IsDryRun() bool
}

// NewProblemDetector creates a new problem detector with the given configuration.
// registry is an optional MonitorRegistryValidator; when non-nil, startup validation
// also checks that all configured monitor types are registered before the detector
// starts. Pass nil to skip registry type checks (useful in tests with mock types).
func NewProblemDetector(config *types.NodeDoctorConfig, monitors []types.Monitor, exporters []types.Exporter, configFilePath string, monitorFactory MonitorFactory, registry types.MonitorRegistryValidator) (*ProblemDetector, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	if configFilePath == "" {
		return nil, fmt.Errorf("config file path cannot be empty")
	}

	// Registry-aware validation: checks monitor types against the registry and
	// detects circular monitor dependencies, in addition to basic field validation.
	// registry may be nil in tests; ValidateWithRegistry handles nil gracefully.
	if err := config.ValidateWithRegistry(registry); err != nil {
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
		config:             config,
		monitorHandles:     make([]*MonitorHandle, 0),
		exporters:          make([]types.Exporter, 0),
		statusChan:         make(chan *types.Status, 1000),
		ctx:                ctx,
		cancel:             cancel,
		stats:              monitorStats,
		configWatcher:      configWatcher,
		configFilePath:     configFilePath,
		monitorFactory:     monitorFactory,
		passedMonitors:     monitors, // Store passed monitors to start in Start()
		monitorConfigIndex: make(map[string]types.MonitorConfig),
		lastStatus:         make(map[string]*types.Status),
		dependents:         make(map[string][]string),
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

// SetRemediatorRegistry attaches a remediation executor to the detector.
// If set before Start(), it will be used to remediate unhealthy conditions found in
// processed statuses (subject to global config.Remediation.Enabled check).
// Passing nil disables remediation without error.
func (pd *ProblemDetector) SetRemediatorRegistry(r RemediationExecutor) {
	pd.mu.Lock()
	defer pd.mu.Unlock()
	pd.remediatorRegistry = r
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

	// Build a set of already-registered monitor names to prevent duplicate starts.
	// passedMonitors are started first (highest priority), then config-derived monitors
	// are created via the factory — but only if no monitor with that name was already added.
	startedNames := make(map[string]bool)

	// Start passed monitors (for testing or programmatic injection of additional monitors).
	// These are monitors supplied directly to the constructor and are NOT derived from config.
	for i, monitor := range pd.passedMonitors {
		monitorConfig := types.MonitorConfig{
			Name:    fmt.Sprintf("passed-monitor-%d", i),
			Enabled: true,
		}

		if err := pd.addMonitor(pd.ctx, monitor, monitorConfig); err != nil {
			log.Printf("[ERROR] Failed to start passed monitor %d: %v", i, err)
			pd.stats.IncrementMonitorsFailed()
			continue
		}

		startedNames[monitorConfig.Name] = true
		log.Printf("[INFO] Started passed monitor: %s", monitorConfig.Name)
	}

	// Start monitors from config via the MonitorFactory.
	// Monitors are sorted topologically so that every dependency is started before the
	// monitors that declare it in DependsOn. The sort also detects cycles not caught
	// earlier (e.g. if validation was bypassed), failing loudly rather than silently.
	//
	// Note: passed monitors use synthetic names ("passed-monitor-N") so they will never
	// collide with config monitor names here. The startedNames guard primarily protects
	// against duplicate entries in config.Monitors itself.
	sortedMonitors, sortErr := topologicalSortMonitors(pd.config.Monitors)
	if sortErr != nil {
		return fmt.Errorf("monitor dependency cycle detected, cannot start: %w", sortErr)
	}

	// Build the reverse-dependency index (dependency name → dependents).
	// Reserved for a future push-notification model. processStatus currently evaluates
	// blocked state via lastStatus (pull model) and does not consult this index.
	for _, mc := range sortedMonitors {
		for _, dep := range mc.DependsOn {
			pd.dependents[dep] = append(pd.dependents[dep], mc.Name)
		}
	}

	for _, monitorConfig := range sortedMonitors {
		if startedNames[monitorConfig.Name] {
			log.Printf("[WARN] Monitor %s already started (skipping duplicate from config)", monitorConfig.Name)
			continue
		}

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

		startedNames[monitorConfig.Name] = true
		log.Printf("[INFO] Started monitor: %s (dependsOn=%v)", monitorConfig.Name, monitorConfig.DependsOn)
	}

	// Start status processing goroutine
	pd.wg.Add(1)
	go func() {
		defer pd.wg.Done()
		pd.processStatuses()
	}()

	pd.started = true
	pd.handlesMu.Lock()
	monitorCount := len(pd.monitorHandles)
	pd.handlesMu.Unlock()
	log.Printf("[INFO] Problem detector started successfully with %d monitors", monitorCount)

	return nil
}

// Stop stops the problem detector gracefully
func (pd *ProblemDetector) Stop() error {
	pd.mu.Lock()

	if !pd.started {
		pd.mu.Unlock()
		return nil // Already stopped
	}

	log.Printf("[INFO] Stopping problem detector...")

	// Stop config watcher
	pd.configWatcher.Stop()

	// Snapshot handles under handlesMu, then stop outside the lock.
	// handle.Stop() blocks up to 5 s — holding handlesMu during it would
	// prevent concurrent addMonitor/stopMonitorByName callers from proceeding.
	pd.handlesMu.Lock()
	snapshot := make([]*MonitorHandle, len(pd.monitorHandles))
	copy(snapshot, pd.monitorHandles)
	pd.handlesMu.Unlock()

	// Stop all monitors
	for _, handle := range snapshot {
		if err := handle.Stop(); err != nil {
			log.Printf("[WARN] Error stopping monitor %s: %v", handle.GetName(), err)
		}
	}

	// Cancel context to stop all goroutines
	pd.cancel()

	// Close status channel
	close(pd.statusChan)

	// Mark stopped and release the lock BEFORE waiting for goroutines.
	// evaluateRemediation (called from processStatuses goroutines tracked by pd.wg)
	// acquires pd.mu.RLock; holding the write lock through wg.Wait() would deadlock.
	pd.started = false
	pd.mu.Unlock()

	pd.wg.Wait()

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

	// If this monitor's dependencies are unhealthy, replace the outgoing status
	// with a synthetic "blocked" status. This prevents exporters from seeing
	// misleading results from a monitor whose prerequisites are not satisfied.
	if blocked, blockedBy := pd.isMonitorBlocked(status.Source); blocked {
		log.Printf("[INFO] Monitor %s is blocked: dependency %q is unhealthy; emitting blocked status", status.Source, blockedBy)
		status = synthBlockedStatus(status.Source, blockedBy)
	}

	// Cache the EFFECTIVE status (after possible blocked replacement) so that
	// transitive dependents — monitors that depend on this monitor — can detect
	// that it is blocked. Without this, a chain A→B→C where A fails would block B
	// correctly but C would see B's pre-substitution real status and remain unblocked.
	pd.lastStatusMu.Lock()
	pd.lastStatus[status.Source] = status
	pd.lastStatusMu.Unlock()

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

	// Evaluate remediation candidates for unhealthy conditions
	pd.evaluateRemediation(status)
}

// evaluateRemediation checks whether any conditions in the status warrant remediation
// and invokes the registry if configured. It is a no-op when:
//   - no RemediationExecutor is attached
//   - global config.Remediation.Enabled is false
//   - the monitor has no remediation config or it is disabled
func (pd *ProblemDetector) evaluateRemediation(status *types.Status) {
	pd.mu.RLock()
	registry := pd.remediatorRegistry
	cfg := pd.config
	pd.mu.RUnlock()

	pd.configIndexMu.RLock()
	monitorCfg, hasCfg := pd.monitorConfigIndex[status.Source]
	pd.configIndexMu.RUnlock()

	if registry == nil {
		return
	}

	if !cfg.Remediation.Enabled {
		return
	}

	if !hasCfg || monitorCfg.Remediation == nil || !monitorCfg.Remediation.Enabled {
		return
	}

	remCfg := monitorCfg.Remediation

	for _, cond := range status.Conditions {
		if cond.Status != types.ConditionFalse {
			continue
		}

		problem := types.Problem{
			Type:       remCfg.Strategy,
			Resource:   cond.Type,
			Severity:   types.ProblemWarning,
			Message:    cond.Message,
			DetectedAt: cond.Transition,
			Metadata: map[string]string{
				"source":    status.Source,
				"condition": cond.Type,
				"reason":    cond.Reason,
			},
		}

		if err := registry.Remediate(pd.ctx, remCfg.Strategy, problem); err != nil {
			log.Printf("[WARN] Remediation failed for %s/%s (strategy=%s): %v",
				status.Source, cond.Type, remCfg.Strategy, err)
			pd.stats.IncrementRemediationsFailed()
		} else {
			log.Printf("[INFO] Remediation triggered for %s/%s (strategy=%s, dry-run=%v)",
				status.Source, cond.Type, remCfg.Strategy, registry.IsDryRun())
			pd.stats.IncrementRemediationsTriggered()
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
	// Guard against reloads that fire during the startup window — between when the
	// config-watcher goroutine is launched in Start() and when pd.started is set to true.
	// Without this guard, a rapid on-disk change can trigger applyConfigReload concurrently
	// with Start()'s own addMonitor calls, racing on pd.monitorHandles.
	pd.mu.RLock()
	started := pd.started
	pd.mu.RUnlock()
	if !started {
		log.Printf("[DEBUG] Ignoring config reload: detector not yet started")
		return nil
	}

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

// stopMonitorByName stops a monitor by its name and removes it from the handles list.
// The slice mutation is done under handlesMu; handle.Stop() (which can block up to 5 s)
// is called after releasing the lock so it does not delay concurrent addMonitor callers.
func (pd *ProblemDetector) stopMonitorByName(name string) error {
	// Find and splice out under handlesMu.
	pd.handlesMu.Lock()
	var found *MonitorHandle
	for i, handle := range pd.monitorHandles {
		if handle.GetName() == name {
			found = handle
			pd.monitorHandles = append(pd.monitorHandles[:i], pd.monitorHandles[i+1:]...)
			break
		}
	}
	pd.handlesMu.Unlock()

	if found == nil {
		return fmt.Errorf("monitor %s not found", name)
	}

	// Remove from config index (configIndexMu is independent of handlesMu).
	pd.configIndexMu.Lock()
	delete(pd.monitorConfigIndex, name)
	pd.configIndexMu.Unlock()

	// Stop outside handlesMu — can block up to 5 s per MonitorHandle.Stop().
	if err := found.Stop(); err != nil {
		return err
	}
	log.Printf("[INFO] Stopped and removed monitor: %s", name)
	return nil
}

// topologicalSortMonitors returns monitors sorted so that each monitor's DependsOn
// predecessors appear before it. Returns an error when a dependency cycle is detected.
// Uses Kahn's algorithm (BFS) for O(V+E) performance.
func topologicalSortMonitors(monitors []types.MonitorConfig) ([]types.MonitorConfig, error) {
	// Build adjacency list and in-degree count.
	// Edge direction: dep → monitor (dep must come before monitor).
	inDegree := make(map[string]int, len(monitors))
	successors := make(map[string][]string, len(monitors)) // dep → monitors that depend on it
	nameSet := make(map[string]struct{}, len(monitors))

	for _, m := range monitors {
		nameSet[m.Name] = struct{}{}
		if _, ok := inDegree[m.Name]; !ok {
			inDegree[m.Name] = 0
		}
	}

	for _, m := range monitors {
		for _, dep := range m.DependsOn {
			if _, ok := nameSet[dep]; !ok {
				// Unknown dependency — validator should have caught this; skip gracefully.
				continue
			}
			inDegree[m.Name]++
			successors[dep] = append(successors[dep], m.Name)
		}
	}

	// Seed queue with zero-in-degree nodes (no dependencies).
	queue := make([]string, 0, len(monitors))
	for _, m := range monitors {
		if inDegree[m.Name] == 0 {
			queue = append(queue, m.Name)
		}
	}

	// Build name → config index for output reconstruction.
	byName := make(map[string]types.MonitorConfig, len(monitors))
	for _, m := range monitors {
		byName[m.Name] = m
	}

	sorted := make([]types.MonitorConfig, 0, len(monitors))
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		sorted = append(sorted, byName[cur])
		for _, succ := range successors[cur] {
			inDegree[succ]--
			if inDegree[succ] == 0 {
				queue = append(queue, succ)
			}
		}
	}

	if len(sorted) != len(monitors) {
		return nil, fmt.Errorf("dependsOn cycle detected among monitors: topological sort incomplete (%d of %d sorted)", len(sorted), len(monitors))
	}

	return sorted, nil
}

// isMonitorBlocked reports whether any of the named monitor's declared dependencies
// currently reports an unhealthy condition (ConditionFalse). Returns the first
// blocking dependency name or "" when unblocked.
// Uses lastStatusMu for concurrent-safe reads of lastStatus (which is written continuously
// by processStatus) and configIndexMu for reads of monitorConfigIndex.
func (pd *ProblemDetector) isMonitorBlocked(name string) (blocked bool, blockedBy string) {
	pd.configIndexMu.RLock()
	cfg, ok := pd.monitorConfigIndex[name]
	pd.configIndexMu.RUnlock()
	if !ok || len(cfg.DependsOn) == 0 {
		return false, ""
	}

	pd.lastStatusMu.RLock()
	defer pd.lastStatusMu.RUnlock()
	for _, dep := range cfg.DependsOn {
		depStatus, exists := pd.lastStatus[dep]
		if !exists {
			// Dependency has not yet reported — treat as unknown, not blocked.
			continue
		}
		for _, cond := range depStatus.Conditions {
			// Block if the dependency is explicitly unhealthy (ConditionFalse) OR
			// if it is itself blocked (MonitorBlocked condition). The latter enables
			// transitive blocking: if B→A and A fails, B becomes blocked; if C→B,
			// C should also become blocked because B's effective status is "blocked".
			if cond.Status == types.ConditionFalse || cond.Type == "MonitorBlocked" {
				return true, dep
			}
		}
	}
	return false, ""
}

// synthBlockedStatus returns a synthetic Status that signals the named monitor is
// blocked waiting for a dependency to become healthy.
func synthBlockedStatus(monitorName, blockedBy string) *types.Status {
	s := types.NewStatus(monitorName)
	s.AddCondition(types.NewCondition(
		"MonitorBlocked",
		types.ConditionUnknown,
		"DependencyUnhealthy",
		fmt.Sprintf("Monitor %s is blocked: dependency %q is unhealthy", monitorName, blockedBy),
	))
	return s
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

	// Add to handles list under handlesMu to prevent concurrent slice mutation
	// (Start holds pd.mu; applyConfigReload holds reloadMutex — different locks,
	// so addMonitor can be called concurrently from both paths).
	pd.handlesMu.Lock()
	pd.monitorHandles = append(pd.monitorHandles, handle)
	pd.handlesMu.Unlock()

	// Index by name so evaluateRemediation can look up MonitorConfig from status.Source
	pd.configIndexMu.Lock()
	pd.monitorConfigIndex[config.Name] = config
	pd.configIndexMu.Unlock()

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
