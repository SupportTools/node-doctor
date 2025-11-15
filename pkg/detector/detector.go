// Package detector implements the Problem Detector orchestrator for Node Doctor.
// It coordinates the flow from monitors through status processing to problem export.
package detector

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/supporttools/node-doctor/pkg/reload"
	"github.com/supporttools/node-doctor/pkg/types"
)

// ProblemDetector orchestrates the entire problem detection pipeline.
// It collects status updates from monitors, processes them into problems,
// deduplicates issues, and distributes results to exporters.
type ProblemDetector struct {
	config          *types.NodeDoctorConfig
	configPath      string                   // Path to configuration file (for reload)
	monitors        []types.Monitor
	exporters       []types.Exporter
	statusChan      chan *types.Status       // Buffered channel for status updates
	stopChan        chan struct{}            // Signal for graceful shutdown
	wg              sync.WaitGroup           // Track goroutines for shutdown
	mu              sync.RWMutex             // Protects running state
	running         bool                     // Current running state
	problems        map[string]*problemEntry // Deduplication map with TTL
	problemsMu      sync.RWMutex             // Protects problems map
	stats           Statistics               // Thread-safe statistics
	problemTTL      time.Duration            // TTL for problem entries
	cleanupInterval time.Duration            // Cleanup interval

	// Hot reload components
	configWatcher      *reload.ConfigWatcher      // Watches config file for changes
	reloadCoordinator  *reload.ReloadCoordinator  // Coordinates reload operations
	reloadMutex        sync.Mutex                 // Prevents concurrent reloads
	monitorFactory     MonitorFactory             // Factory for creating monitors dynamically
}

// problemEntry wraps a problem with TTL tracking
type problemEntry struct {
	problem  *types.Problem
	lastSeen time.Time
}

// MonitorFactory creates monitors from configuration.
// This interface allows hot reload to dynamically create new monitors.
type MonitorFactory interface {
	CreateMonitor(config types.MonitorConfig) (types.Monitor, error)
}

// NewProblemDetector creates a new ProblemDetector instance.
// It validates the configuration and initializes all internal structures.
// If configPath is provided and reload is enabled, hot reload will be configured.
func NewProblemDetector(config *types.NodeDoctorConfig, monitors []types.Monitor, exporters []types.Exporter, configPath string, monitorFactory MonitorFactory) (*ProblemDetector, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}
	if len(monitors) == 0 {
		return nil, fmt.Errorf("at least one monitor is required")
	}
	if len(exporters) == 0 {
		return nil, fmt.Errorf("at least one exporter is required")
	}

	pd := &ProblemDetector{
		config:          config,
		configPath:      configPath,
		monitors:        monitors,
		exporters:       exporters,
		monitorFactory:  monitorFactory,
		statusChan:      make(chan *types.Status, 1000), // Buffered to prevent blocking
		stopChan:        make(chan struct{}),
		problems:        make(map[string]*problemEntry),
		stats:           NewStatistics(),
		problemTTL:      1 * time.Hour,    // Problems expire after 1 hour
		cleanupInterval: 10 * time.Minute, // Cleanup every 10 minutes
	}

	// Initialize hot reload if enabled
	if config.Reload.Enabled && configPath != "" {
		if err := pd.initializeReload(); err != nil {
			return nil, fmt.Errorf("failed to initialize hot reload: %w", err)
		}
		log.Printf("[INFO] Hot reload enabled for config file: %s (debounce: %v)", configPath, config.Reload.DebounceInterval)
	}

	return pd, nil
}

// initializeReload sets up the configuration hot reload components.
func (pd *ProblemDetector) initializeReload() error {
	// Create config watcher
	watcher, err := reload.NewConfigWatcher(pd.configPath, pd.config.Reload.DebounceInterval)
	if err != nil {
		return fmt.Errorf("failed to create config watcher: %w", err)
	}
	pd.configWatcher = watcher

	// Create reload coordinator
	pd.reloadCoordinator = reload.NewReloadCoordinator(
		pd.configPath,
		pd.config,
		pd.applyConfigReload,
		pd.emitReloadEvent,
	)

	return nil
}

// Run starts the problem detector orchestrator.
// This is the main entry point that coordinates the entire pipeline.
func (pd *ProblemDetector) Run(ctx context.Context) error {
	pd.mu.Lock()
	if pd.running {
		pd.mu.Unlock()
		return fmt.Errorf("problem detector is already running")
	}
	// Don't set running=true yet - wait until startup succeeds
	pd.mu.Unlock()

	log.Printf("[INFO] Starting Problem Detector with %d monitors and %d exporters", len(pd.monitors), len(pd.exporters))

	// Start all monitors and collect their status channels
	if err := pd.startMonitors(ctx); err != nil {
		return fmt.Errorf("failed to start monitors: %w", err)
	}

	// Now that startup succeeded, mark as running
	pd.mu.Lock()
	pd.running = true
	pd.mu.Unlock()

	// Start the main processing loop
	pd.wg.Add(1)
	go func() {
		defer pd.wg.Done()
		pd.processStatuses(ctx)
	}()

	// Start problem cleanup goroutine
	pd.wg.Add(1)
	go func() {
		defer pd.wg.Done()
		pd.cleanupExpiredProblems(ctx)
	}()

	// Start config watcher if hot reload is enabled
	var reloadCh <-chan struct{}
	if pd.configWatcher != nil {
		ch, err := pd.configWatcher.Start(ctx)
		if err != nil {
			log.Printf("[ERROR] Failed to start config watcher: %v", err)
		} else {
			reloadCh = ch
			log.Printf("[INFO] Config watcher started")
		}
	}

	log.Printf("[INFO] Problem Detector started successfully")

	// Wait for context cancellation, stop signal, or reload events
	for {
		select {
		case <-ctx.Done():
			log.Printf("[INFO] Problem Detector stopping due to context cancellation")
			return pd.shutdown()
		case <-pd.stopChan:
			log.Printf("[INFO] Problem Detector stopping due to stop signal")
			return pd.shutdown()
		case <-reloadCh:
			if reloadCh != nil {
				log.Printf("[INFO] Config file change detected, triggering reload")
				if err := pd.reloadCoordinator.TriggerReload(ctx); err != nil {
					log.Printf("[WARN] Config reload failed: %v", err)
				}
			}
		}
	}
}

// startMonitors starts all monitors and sets up fan-in from their status channels.
func (pd *ProblemDetector) startMonitors(ctx context.Context) error {
	var successful int
	var errors []error

	for i, monitor := range pd.monitors {
		statusCh, err := monitor.Start()
		if err != nil {
			pd.stats.IncrementMonitorsFailed()
			errors = append(errors, fmt.Errorf("monitor %d failed to start: %w", i, err))
			log.Printf("[ERROR] Monitor %d failed to start: %v", i, err)
			continue
		}

		pd.stats.IncrementMonitorsStarted()
		successful++

		// Start fan-in goroutine for this monitor
		pd.wg.Add(1)
		go func(ch <-chan *types.Status, monitorIdx int) {
			defer pd.wg.Done()
			pd.fanInFromMonitor(ctx, ch, monitorIdx)
		}(statusCh, i)

		log.Printf("[INFO] Monitor %d started successfully", i)
	}

	// Require at least one successful monitor
	if successful == 0 {
		return fmt.Errorf("no monitors started successfully: %v", errors)
	}

	if len(errors) > 0 {
		log.Printf("[WARN] %d monitors failed to start, continuing with %d successful monitors", len(errors), successful)
	}

	return nil
}

// fanInFromMonitor reads status updates from a single monitor channel
// and forwards them to the central status channel.
func (pd *ProblemDetector) fanInFromMonitor(ctx context.Context, statusCh <-chan *types.Status, monitorIdx int) {
	log.Printf("[INFO] Starting fan-in for monitor %d", monitorIdx)

	for {
		select {
		case <-ctx.Done():
			log.Printf("[INFO] Fan-in for monitor %d stopping due to context cancellation", monitorIdx)
			return
		case <-pd.stopChan:
			log.Printf("[INFO] Fan-in for monitor %d stopping due to stop signal", monitorIdx)
			return
		case status, ok := <-statusCh:
			if !ok {
				log.Printf("[INFO] Monitor %d channel closed, stopping fan-in", monitorIdx)
				return
			}

			// Validate status before forwarding
			if status == nil {
				log.Printf("[WARN] Monitor %d sent nil status, ignoring", monitorIdx)
				continue
			}

			// Non-blocking send to central channel
			select {
			case pd.statusChan <- status:
				// Status sent successfully
			case <-ctx.Done():
				log.Printf("[INFO] Fan-in for monitor %d stopping during send", monitorIdx)
				return
			case <-pd.stopChan:
				log.Printf("[INFO] Fan-in for monitor %d stopping during send", monitorIdx)
				return
			default:
				// Channel is full, log and drop the status
				log.Printf("[WARN] Status channel full, dropping status from monitor %d", monitorIdx)
			}
		}
	}
}

// processStatuses is the main processing loop that handles status updates.
// It converts statuses to problems, deduplicates them, and exports results.
func (pd *ProblemDetector) processStatuses(ctx context.Context) {
	log.Printf("[INFO] Starting status processing loop")

	for {
		select {
		case <-ctx.Done():
			log.Printf("[INFO] Status processing stopping due to context cancellation")
			return
		case <-pd.stopChan:
			log.Printf("[INFO] Status processing stopping due to stop signal")
			return
		case status := <-pd.statusChan:
			if status == nil {
				continue
			}

			pd.stats.IncrementStatusesReceived()

			// Export the raw status to all exporters
			pd.exportStatus(ctx, status)

			// Convert status to problems
			problems := pd.statusToProblems(status)
			if len(problems) == 0 {
				continue
			}

			pd.stats.AddProblemsDetected(len(problems))

			// Deduplicate problems
			newProblems := pd.deduplicateProblems(problems)
			if len(newProblems) == 0 {
				continue
			}

			pd.stats.AddProblemsDeduplicated(len(newProblems))

			// Export new problems
			pd.exportProblems(ctx, newProblems)
		}
	}
}

// statusToProblems converts a Status into a slice of Problems based on events and conditions.
func (pd *ProblemDetector) statusToProblems(status *types.Status) []*types.Problem {
	var problems []*types.Problem

	// Process events
	for _, event := range status.Events {
		var severity types.ProblemSeverity
		switch event.Severity {
		case types.EventError:
			severity = types.ProblemCritical
		case types.EventWarning:
			severity = types.ProblemWarning
		case types.EventInfo:
			// Skip informational events
			continue
		default:
			log.Printf("[WARN] Unknown event severity: %s", event.Severity)
			continue
		}

		problem := types.NewProblem(
			fmt.Sprintf("event-%s", event.Reason),
			status.Source,
			severity,
			event.Message,
		)
		problem.WithMetadata("event_reason", event.Reason)
		problem.WithMetadata("event_timestamp", event.Timestamp.Format(time.RFC3339))
		problem.WithMetadata("source", status.Source)

		problems = append(problems, problem)
	}

	// Process conditions
	for _, condition := range status.Conditions {
		switch condition.Status {
		case types.ConditionFalse:
			// False conditions indicate problems
			problem := types.NewProblem(
				fmt.Sprintf("condition-%s", condition.Type),
				status.Source,
				types.ProblemCritical,
				condition.Message,
			)
			problem.WithMetadata("condition_type", condition.Type)
			problem.WithMetadata("condition_reason", condition.Reason)
			problem.WithMetadata("condition_transition", condition.Transition.Format(time.RFC3339))
			problem.WithMetadata("source", status.Source)

			problems = append(problems, problem)
		case types.ConditionTrue, types.ConditionUnknown:
			// Skip healthy or unknown conditions
			continue
		default:
			log.Printf("[WARN] Unknown condition status: %s", condition.Status)
			continue
		}
	}

	return problems
}

// deduplicateProblems filters out duplicate problems based on Type+Resource key.
// Returns only new or significantly changed problems.
func (pd *ProblemDetector) deduplicateProblems(problems []*types.Problem) []*types.Problem {
	pd.problemsMu.Lock()
	defer pd.problemsMu.Unlock()

	var newProblems []*types.Problem
	now := time.Now()

	for _, problem := range problems {
		key := problem.Type + ":" + problem.Resource
		entry, exists := pd.problems[key]

		if !exists {
			// New problem
			pd.problems[key] = &problemEntry{
				problem:  problem,
				lastSeen: now,
			}
			newProblems = append(newProblems, problem)
			continue
		}

		// Update last seen time
		entry.lastSeen = now

		// Check if problem has significantly changed
		if entry.problem.Severity != problem.Severity || entry.problem.Message != problem.Message {
			// Problem changed, update and report
			entry.problem = problem
			newProblems = append(newProblems, problem)
			continue
		}

		// Problem already exists and hasn't changed significantly, skip
	}

	return newProblems
}

// cleanupExpiredProblems periodically removes stale problems from the map.
// This prevents unbounded memory growth by expiring problems after the TTL.
func (pd *ProblemDetector) cleanupExpiredProblems(ctx context.Context) {
	ticker := time.NewTicker(pd.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pd.cleanupOnce()
		}
	}
}

// cleanupOnce performs a single cleanup pass of expired problems.
func (pd *ProblemDetector) cleanupOnce() {
	pd.problemsMu.Lock()
	defer pd.problemsMu.Unlock()

	now := time.Now()
	var expiredCount int

	for key, entry := range pd.problems {
		if now.Sub(entry.lastSeen) > pd.problemTTL {
			delete(pd.problems, key)
			expiredCount++
		}
	}

	if expiredCount > 0 {
		log.Printf("[INFO] Cleaned up %d expired problems (TTL: %v)", expiredCount, pd.problemTTL)
	}
}

// exportStatus sends a status update to all exporters in parallel with timeout.
func (pd *ProblemDetector) exportStatus(ctx context.Context, status *types.Status) {
	if len(pd.exporters) == 0 {
		return
	}

	// Create a channel to signal completion
	done := make(chan struct{})
	var wg sync.WaitGroup

	for i, exporter := range pd.exporters {
		wg.Add(1)
		go func(exp types.Exporter, exporterIdx int) {
			defer wg.Done()

			// Add per-exporter timeout
			exportCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()

			if err := exp.ExportStatus(exportCtx, status); err != nil {
				pd.stats.IncrementExportsFailed()
				log.Printf("[ERROR] Exporter %d failed to export status: %v", exporterIdx, err)
			} else {
				pd.stats.IncrementExportsSucceeded()
			}
		}(exporter, i)
	}

	// Wait for completion or timeout in separate goroutine
	go func() {
		wg.Wait()
		close(done)
	}()

	// Wait with timeout to prevent blocking indefinitely
	select {
	case <-done:
		// All exporters completed
	case <-time.After(15 * time.Second):
		log.Printf("[WARN] Export status timed out after 15s, some exporters may still be processing")
	}
}

// exportProblems sends problems to all exporters in parallel with timeout.
func (pd *ProblemDetector) exportProblems(ctx context.Context, problems []*types.Problem) {
	if len(pd.exporters) == 0 || len(problems) == 0 {
		return
	}

	// Create a channel to signal completion
	done := make(chan struct{})
	var wg sync.WaitGroup

	for i, exporter := range pd.exporters {
		wg.Add(1)
		go func(exp types.Exporter, exporterIdx int) {
			defer wg.Done()

			for _, problem := range problems {
				// Add per-export timeout
				exportCtx, cancel := context.WithTimeout(ctx, 10*time.Second)

				if err := exp.ExportProblem(exportCtx, problem); err != nil {
					pd.stats.IncrementExportsFailed()
					log.Printf("[ERROR] Exporter %d failed to export problem: %v", exporterIdx, err)
				} else {
					pd.stats.IncrementExportsSucceeded()
				}

				cancel()
			}
		}(exporter, i)
	}

	// Wait for completion or timeout in separate goroutine
	go func() {
		wg.Wait()
		close(done)
	}()

	// Wait with timeout to prevent blocking indefinitely
	select {
	case <-done:
		// All exporters completed
	case <-time.After(15 * time.Second):
		log.Printf("[WARN] Export problems timed out after 15s, some exporters may still be processing")
	}
}

// shutdown performs graceful shutdown with timeout.
func (pd *ProblemDetector) shutdown() error {
	pd.mu.Lock()
	if !pd.running {
		pd.mu.Unlock()
		return nil
	}
	pd.running = false
	pd.mu.Unlock()

	log.Printf("[INFO] Starting graceful shutdown")

	// Signal all goroutines to stop
	close(pd.stopChan)

	// Stop config watcher if enabled
	if pd.configWatcher != nil {
		pd.configWatcher.Stop()
		log.Printf("[INFO] Stopped config watcher")
	}

	// Stop all monitors
	for i, monitor := range pd.monitors {
		monitor.Stop()
		log.Printf("[INFO] Stopped monitor %d", i)
	}

	// Wait for all goroutines to finish with timeout
	done := make(chan struct{})
	go func() {
		pd.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Printf("[INFO] Graceful shutdown completed")
		return nil
	case <-time.After(30 * time.Second):
		log.Printf("[WARN] Shutdown timeout reached, some goroutines may still be running")
		return fmt.Errorf("shutdown timeout exceeded")
	}
}

// GetStatistics returns a copy of the current statistics.
func (pd *ProblemDetector) GetStatistics() Statistics {
	return pd.stats.Copy()
}

// IsRunning returns whether the detector is currently running.
func (pd *ProblemDetector) IsRunning() bool {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	return pd.running
}

// applyConfigReload applies configuration changes during hot reload.
// This is called by the ReloadCoordinator after validation succeeds.
func (pd *ProblemDetector) applyConfigReload(ctx context.Context, newConfig *types.NodeDoctorConfig, diff *reload.ConfigDiff) error {
	pd.reloadMutex.Lock()
	defer pd.reloadMutex.Unlock()

	log.Printf("[INFO] Applying configuration reload")

	// Step 1: Stop monitors that were removed
	for _, removedConfig := range diff.MonitorsRemoved {
		if err := pd.stopMonitorByName(removedConfig.Name); err != nil {
			log.Printf("[WARN] Failed to stop removed monitor %s: %v", removedConfig.Name, err)
		}
	}

	// Step 2: Restart modified monitors
	for _, change := range diff.MonitorsModified {
		// Stop old monitor
		if err := pd.stopMonitorByName(change.Old.Name); err != nil {
			log.Printf("[WARN] Failed to stop monitor %s for restart: %v", change.Old.Name, err)
			continue
		}

		// Create and start new monitor with updated config
		if pd.monitorFactory != nil {
			newMonitor, err := pd.monitorFactory.CreateMonitor(change.New)
			if err != nil {
				log.Printf("[ERROR] Failed to create modified monitor %s: %v", change.New.Name, err)
				continue
			}

			statusCh, err := newMonitor.Start()
			if err != nil {
				log.Printf("[ERROR] Failed to start modified monitor %s: %v", change.New.Name, err)
				continue
			}

			// Replace in monitors array and start fan-in
			pd.replaceMonitor(change.Old.Name, newMonitor)
			pd.wg.Add(1)
			go func(ch <-chan *types.Status, name string) {
				defer pd.wg.Done()
				pd.fanInFromMonitor(ctx, ch, pd.getMonitorIndex(name))
			}(statusCh, change.New.Name)

			log.Printf("[INFO] Restarted monitor %s with new configuration", change.New.Name)
		}
	}

	// Step 3: Start new monitors
	for _, addedConfig := range diff.MonitorsAdded {
		if pd.monitorFactory != nil {
			newMonitor, err := pd.monitorFactory.CreateMonitor(addedConfig)
			if err != nil {
				log.Printf("[ERROR] Failed to create new monitor %s: %v", addedConfig.Name, err)
				continue
			}

			statusCh, err := newMonitor.Start()
			if err != nil {
				log.Printf("[ERROR] Failed to start new monitor %s: %v", addedConfig.Name, err)
				continue
			}

			// Add to monitors array and start fan-in
			pd.monitors = append(pd.monitors, newMonitor)
			pd.wg.Add(1)
			go func(ch <-chan *types.Status, idx int) {
				defer pd.wg.Done()
				pd.fanInFromMonitor(ctx, ch, idx)
			}(statusCh, len(pd.monitors)-1)

			log.Printf("[INFO] Started new monitor %s", addedConfig.Name)
		}
	}

	// Step 4: Update configuration
	// Note: Exporters and remediation changes don't require restarts,
	// they just need the updated config reference
	pd.mu.Lock()
	pd.config = newConfig
	pd.mu.Unlock()

	log.Printf("[INFO] Configuration reload completed successfully")
	return nil
}

// stopMonitorByName stops a monitor by its name.
func (pd *ProblemDetector) stopMonitorByName(name string) error {
	for i, monitor := range pd.monitors {
		// Try to get monitor name from config (this requires monitors to store their config)
		// For now, we'll match by index - production would need better monitor identification
		if i < len(pd.config.Monitors) && pd.config.Monitors[i].Name == name {
			monitor.Stop()
			log.Printf("[INFO] Stopped monitor %s", name)
			return nil
		}
	}
	return fmt.Errorf("monitor %s not found", name)
}

// replaceMonitor replaces a monitor in the monitors array.
func (pd *ProblemDetector) replaceMonitor(name string, newMonitor types.Monitor) {
	for i := range pd.monitors {
		if i < len(pd.config.Monitors) && pd.config.Monitors[i].Name == name {
			pd.monitors[i] = newMonitor
			return
		}
	}
}

// getMonitorIndex returns the index of a monitor by name.
func (pd *ProblemDetector) getMonitorIndex(name string) int {
	for i := range pd.config.Monitors {
		if pd.config.Monitors[i].Name == name {
			return i
		}
	}
	return -1
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
