package reload

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
	"github.com/supporttools/node-doctor/pkg/util"
)

// ReloadCallback is called when a configuration reload is needed.
// It receives the new configuration and the diff, and should apply the changes.
type ReloadCallback func(ctx context.Context, newConfig *types.NodeDoctorConfig, diff *ConfigDiff) error

// EventEmitter emits reload status events.
type EventEmitter func(severity types.EventSeverity, reason, message string)

// ReloadMetricsRecorder records the outcome of a completed reload attempt.
// success is true when the reload applied (or determined there were no changes)
// without error, false when any step failed. duration is the wall-clock time
// spent in performReload. It is invoked exactly once per reload attempt.
//
// This is an injected hook (mirroring EventEmitter) so the reload package never
// imports the prometheus exporter, avoiding coupling/cycles.
type ReloadMetricsRecorder func(success bool, duration time.Duration)

// ReloadCoordinator orchestrates configuration reload operations.
type ReloadCoordinator struct {
	configPath       string
	currentConfig    *types.NodeDoctorConfig
	reloadCallback   ReloadCallback
	eventEmitter     EventEmitter
	metricsRecorder  ReloadMetricsRecorder
	validator        *ConfigValidator
	mu               sync.Mutex
	reloadInProgress bool
}

// NewReloadCoordinator creates a new reload coordinator.
func NewReloadCoordinator(
	configPath string,
	initialConfig *types.NodeDoctorConfig,
	reloadCallback ReloadCallback,
	eventEmitter EventEmitter,
) *ReloadCoordinator {
	return &ReloadCoordinator{
		configPath:     configPath,
		currentConfig:  initialConfig,
		reloadCallback: reloadCallback,
		eventEmitter:   eventEmitter,
		validator:      NewConfigValidator(),
	}
}

// NewReloadCoordinatorWithValidator creates a new reload coordinator with a custom validator.
func NewReloadCoordinatorWithValidator(
	configPath string,
	initialConfig *types.NodeDoctorConfig,
	reloadCallback ReloadCallback,
	eventEmitter EventEmitter,
	validator *ConfigValidator,
) *ReloadCoordinator {
	return &ReloadCoordinator{
		configPath:     configPath,
		currentConfig:  initialConfig,
		reloadCallback: reloadCallback,
		eventEmitter:   eventEmitter,
		validator:      validator,
	}
}

// TriggerReload attempts to reload the configuration from disk.
// This method is safe to call concurrently; only one reload happens at a time.
func (rc *ReloadCoordinator) TriggerReload(ctx context.Context) error {
	rc.mu.Lock()
	if rc.reloadInProgress {
		rc.mu.Unlock()
		return fmt.Errorf("reload already in progress")
	}
	rc.reloadInProgress = true
	rc.mu.Unlock()

	defer func() {
		rc.mu.Lock()
		rc.reloadInProgress = false
		rc.mu.Unlock()
	}()

	return rc.performReload(ctx)
}

// performReload executes the reload process.
//
// The named return value err is inspected by a deferred closure that records
// reload self-metrics exactly once, on EVERY return path (load error, validation
// error, no-changes success, callback error, full success). success is derived
// from err == nil at the moment of return, so adding a new early return cannot
// silently skip metric recording.
func (rc *ReloadCoordinator) performReload(ctx context.Context) (err error) {
	startTime := time.Now()

	// Record reload self-metrics exactly once when performReload returns,
	// regardless of which path produced the result.
	defer func() {
		rc.recordMetrics(err == nil, time.Since(startTime))
	}()

	// Emit start event
	rc.emitEvent(types.EventInfo, "ConfigReloadStarted", "Configuration reload initiated")

	// Step 1: Load new configuration
	newConfig, err := util.LoadConfig(rc.configPath)
	if err != nil {
		rc.emitEvent(types.EventWarning, "ConfigReloadFailed",
			fmt.Sprintf("Failed to load configuration: %v", err))
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Step 2: Validate new configuration
	if rc.validator != nil {
		validationResult := rc.validator.Validate(newConfig)
		if !validationResult.Valid {
			errorMsg := FormatValidationErrors(validationResult.Errors)
			rc.emitEvent(types.EventWarning, "ConfigValidationFailed",
				fmt.Sprintf("Configuration validation failed: %s", errorMsg))
			return fmt.Errorf("configuration validation failed: %s", errorMsg)
		}

		// Emit validation success for observability
		rc.emitEvent(types.EventInfo, "ConfigValidationSucceeded",
			"Configuration validation completed successfully")
	}

	// Step 3: Compute diff
	rc.mu.Lock()
	diff := ComputeConfigDiff(rc.currentConfig, newConfig)
	rc.mu.Unlock()

	// Step 4: Check if there are any changes
	if !diff.HasChanges() {
		rc.emitEvent(types.EventInfo, "ConfigReloadNoChanges",
			"Configuration reload completed with no changes")
		return nil
	}

	// Step 5: Apply changes via callback
	if err := rc.reloadCallback(ctx, newConfig, diff); err != nil {
		rc.emitEvent(types.EventWarning, "ConfigReloadFailed",
			fmt.Sprintf("Failed to apply configuration changes: %v", err))
		return fmt.Errorf("failed to apply changes: %w", err)
	}

	// Step 6: Update current config
	rc.mu.Lock()
	rc.currentConfig = newConfig
	rc.mu.Unlock()

	// Emit success event with statistics
	duration := time.Since(startTime)
	stats := rc.buildReloadStats(diff, duration)
	rc.emitEvent(types.EventInfo, "ConfigReloadSucceeded", stats)

	return nil
}

// buildReloadStats creates a summary message of what was reloaded.
func (rc *ReloadCoordinator) buildReloadStats(diff *ConfigDiff, duration time.Duration) string {
	msg := fmt.Sprintf("Configuration reload completed in %v. ", duration.Round(time.Millisecond))

	changes := make([]string, 0)

	if len(diff.MonitorsAdded) > 0 {
		changes = append(changes, fmt.Sprintf("%d monitor(s) added", len(diff.MonitorsAdded)))
	}
	if len(diff.MonitorsRemoved) > 0 {
		changes = append(changes, fmt.Sprintf("%d monitor(s) removed", len(diff.MonitorsRemoved)))
	}
	if len(diff.MonitorsModified) > 0 {
		changes = append(changes, fmt.Sprintf("%d monitor(s) modified", len(diff.MonitorsModified)))
	}
	if diff.ExportersChanged {
		changes = append(changes, "exporters updated")
	}
	if diff.RemediationChanged {
		changes = append(changes, "remediation config updated")
	}

	if len(changes) == 0 {
		msg += "No changes detected."
	} else {
		msg += "Changes: "
		for i, change := range changes {
			if i > 0 {
				msg += ", "
			}
			msg += change
		}
	}

	return msg
}

// emitEvent emits a reload status event.
func (rc *ReloadCoordinator) emitEvent(severity types.EventSeverity, reason, message string) {
	if rc.eventEmitter != nil {
		rc.eventEmitter(severity, reason, message)
	}
}

// SetMetricsRecorder sets (or clears) the reload self-metrics recorder. It is
// nil-safe: passing nil disables metric recording. Safe to call before the
// coordinator is used to trigger reloads.
func (rc *ReloadCoordinator) SetMetricsRecorder(recorder ReloadMetricsRecorder) {
	rc.metricsRecorder = recorder
}

// recordMetrics invokes the metrics recorder, if one is set.
func (rc *ReloadCoordinator) recordMetrics(success bool, duration time.Duration) {
	if rc.metricsRecorder != nil {
		rc.metricsRecorder(success, duration)
	}
}

// GetCurrentConfig returns the current active configuration (thread-safe).
func (rc *ReloadCoordinator) GetCurrentConfig() *types.NodeDoctorConfig {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return rc.currentConfig
}

// GetValidator returns the configuration validator used by this coordinator.
func (rc *ReloadCoordinator) GetValidator() *ConfigValidator {
	return rc.validator
}

// SetValidator sets a new configuration validator.
// This is useful for testing or when different validation rules are needed.
func (rc *ReloadCoordinator) SetValidator(validator *ConfigValidator) {
	rc.validator = validator
}
