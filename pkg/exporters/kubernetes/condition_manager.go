package kubernetes

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/supporttools/node-doctor/pkg/types"
)

// ConditionManager handles node condition updates with batching, resync, and heartbeat
type ConditionManager struct {
	client            *K8sClient
	config            *types.KubernetesExporterConfig
	mu                sync.RWMutex
	conditions        map[string]corev1.NodeCondition // Current known conditions
	pendingUpdates    map[string]corev1.NodeCondition // Pending condition updates
	updateInterval    time.Duration                   // How often to batch update conditions
	resyncInterval    time.Duration                   // How often to resync with Kubernetes
	heartbeatInterval time.Duration                   // How often to send heartbeat updates
	lastUpdate        time.Time                       // Last time conditions were updated
	lastResync        time.Time                       // Last time we resynced with Kubernetes
	lastHeartbeat     time.Time                       // Last time we sent a heartbeat
	stopCh            chan struct{}
	stopped           bool
	wg                sync.WaitGroup
}

// NewConditionManager creates a new condition manager with the specified configuration
func NewConditionManager(client *K8sClient, config *types.KubernetesExporterConfig) *ConditionManager {
	updateInterval := 10 * time.Second // Default batch interval
	if config.UpdateInterval > 0 {
		updateInterval = config.UpdateInterval
	}

	resyncInterval := 60 * time.Second // Default resync interval
	if config.ResyncInterval > 0 {
		resyncInterval = config.ResyncInterval
	}

	heartbeatInterval := 5 * time.Minute // Default heartbeat interval
	if config.HeartbeatInterval > 0 {
		heartbeatInterval = config.HeartbeatInterval
	}

	manager := &ConditionManager{
		client:            client,
		config:            config,
		conditions:        make(map[string]corev1.NodeCondition),
		pendingUpdates:    make(map[string]corev1.NodeCondition),
		updateInterval:    updateInterval,
		resyncInterval:    resyncInterval,
		heartbeatInterval: heartbeatInterval,
		stopCh:            make(chan struct{}),
	}

	return manager
}

// Start begins the condition manager background processes
func (cm *ConditionManager) Start(ctx context.Context) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.stopped {
		// Reset the manager for restart
		cm.stopCh = make(chan struct{})
		cm.stopped = false
	}

	log.Printf("[INFO] Condition manager starting with update interval %v, resync interval %v, heartbeat interval %v",
		cm.updateInterval, cm.resyncInterval, cm.heartbeatInterval)

	// Perform initial sync
	if err := cm.initialSync(ctx); err != nil {
		log.Printf("[WARN] Initial condition sync failed: %v", err)
	}

	// Start background loops
	cm.wg.Add(3)
	go cm.updateLoop(ctx)
	go cm.resyncLoop(ctx)
	go cm.heartbeatLoop(ctx)

	log.Printf("[INFO] Condition manager started")
}

// Stop gracefully stops the condition manager
func (cm *ConditionManager) Stop() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.stopped {
		return // Already stopped
	}

	close(cm.stopCh)
	cm.stopped = true
	cm.mu.Unlock() // Unlock before waiting
	cm.wg.Wait()
	cm.mu.Lock() // Relock before returning
	log.Printf("[INFO] Condition manager stopped")
}

// UpdateCondition adds or updates a condition for the next batch update
func (cm *ConditionManager) UpdateCondition(condition types.Condition) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	nodeCondition := convertConditionToNodeCondition(condition)
	conditionType := string(nodeCondition.Type)

	// Check if this condition has actually changed
	if existing, exists := cm.conditions[conditionType]; exists {
		if conditionsEqual(existing, nodeCondition) {
			log.Printf("[DEBUG] Condition %s unchanged, skipping update", conditionType)
			return
		}
	}

	cm.pendingUpdates[conditionType] = nodeCondition
	log.Printf("[DEBUG] Condition %s queued for update: %s=%s (%s)",
		conditionType, nodeCondition.Type, nodeCondition.Status, nodeCondition.Reason)
}

// UpdateConditionFromProblem updates a condition based on a problem
func (cm *ConditionManager) UpdateConditionFromProblem(problem *types.Problem) {
	nodeCondition := ConvertProblemToCondition(problem)
	conditionType := string(nodeCondition.Type)

	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.pendingUpdates[conditionType] = nodeCondition
	log.Printf("[DEBUG] Problem-based condition %s queued for update: %s=%s (%s)",
		conditionType, nodeCondition.Type, nodeCondition.Status, nodeCondition.Reason)
}

// AddCustomConditions adds custom conditions from configuration
func (cm *ConditionManager) AddCustomConditions() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	for _, conditionConfig := range cm.config.Conditions {
		status := corev1.ConditionTrue
		if conditionConfig.DefaultStatus == "False" {
			status = corev1.ConditionFalse
		} else if conditionConfig.DefaultStatus == "Unknown" {
			status = corev1.ConditionUnknown
		}

		condition := corev1.NodeCondition{
			Type:               corev1.NodeConditionType(conditionConfig.Type),
			Status:             status,
			LastTransitionTime: metav1.NewTime(time.Now()),
			Reason:             conditionConfig.DefaultReason,
			Message:            conditionConfig.DefaultMessage,
		}

		conditionType := string(condition.Type)
		cm.pendingUpdates[conditionType] = condition
		log.Printf("[DEBUG] Custom condition %s queued: %s=%s",
			conditionType, condition.Type, condition.Status)
	}
}

// initialSync performs the initial synchronization with Kubernetes
func (cm *ConditionManager) initialSync(ctx context.Context) error {
	log.Printf("[DEBUG] Performing initial condition sync...")

	node, err := cm.client.GetNode(ctx)
	if err != nil {
		return fmt.Errorf("failed to get current node state: %w", err)
	}

	// Load current conditions from the node
	for _, condition := range node.Status.Conditions {
		conditionType := string(condition.Type)
		cm.conditions[conditionType] = condition
		log.Printf("[DEBUG] Loaded existing condition: %s=%s", condition.Type, condition.Status)
	}

	// Add any custom conditions from configuration
	for _, conditionConfig := range cm.config.Conditions {
		conditionType := conditionConfig.Type
		if _, exists := cm.conditions[conditionType]; !exists {
			// This custom condition doesn't exist yet, add it
			status := corev1.ConditionTrue
			if conditionConfig.DefaultStatus == "False" {
				status = corev1.ConditionFalse
			} else if conditionConfig.DefaultStatus == "Unknown" {
				status = corev1.ConditionUnknown
			}

			condition := corev1.NodeCondition{
				Type:               corev1.NodeConditionType(conditionType),
				Status:             status,
				LastTransitionTime: metav1.NewTime(time.Now()),
				Reason:             conditionConfig.DefaultReason,
				Message:            conditionConfig.DefaultMessage,
			}

			cm.pendingUpdates[conditionType] = condition
			log.Printf("[DEBUG] Custom condition %s will be added", conditionType)
		}
	}

	cm.lastResync = time.Now()
	log.Printf("[DEBUG] Initial sync completed, loaded %d existing conditions", len(cm.conditions))
	return nil
}

// updateLoop periodically applies pending condition updates
func (cm *ConditionManager) updateLoop(ctx context.Context) {
	defer cm.wg.Done()

	ticker := time.NewTicker(cm.updateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("[DEBUG] Condition manager update loop stopping due to context cancellation")
			return
		case <-cm.stopCh:
			log.Printf("[DEBUG] Condition manager update loop stopping due to stop signal")
			return
		case <-ticker.C:
			if err := cm.flushPendingUpdates(ctx); err != nil {
				log.Printf("[WARN] Failed to flush pending condition updates: %v", err)
			}
		}
	}
}

// resyncLoop periodically resyncs conditions with Kubernetes
func (cm *ConditionManager) resyncLoop(ctx context.Context) {
	defer cm.wg.Done()

	ticker := time.NewTicker(cm.resyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("[DEBUG] Condition manager resync loop stopping due to context cancellation")
			return
		case <-cm.stopCh:
			log.Printf("[DEBUG] Condition manager resync loop stopping due to stop signal")
			return
		case <-ticker.C:
			if err := cm.performResync(ctx); err != nil {
				log.Printf("[WARN] Condition resync failed: %v", err)
			}
		}
	}
}

// heartbeatLoop periodically sends heartbeat updates to show Node Doctor is alive
func (cm *ConditionManager) heartbeatLoop(ctx context.Context) {
	defer cm.wg.Done()

	ticker := time.NewTicker(cm.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("[DEBUG] Condition manager heartbeat loop stopping due to context cancellation")
			return
		case <-cm.stopCh:
			log.Printf("[DEBUG] Condition manager heartbeat loop stopping due to stop signal")
			return
		case <-ticker.C:
			cm.sendHeartbeat()
		}
	}
}

// flushPendingUpdates applies all pending condition updates to Kubernetes
func (cm *ConditionManager) flushPendingUpdates(ctx context.Context) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if len(cm.pendingUpdates) == 0 {
		return nil
	}

	// Prepare the conditions slice for the patch
	var conditionsToUpdate []corev1.NodeCondition

	// Merge pending updates with existing conditions
	allConditions := make(map[string]corev1.NodeCondition)

	// Start with existing conditions
	for conditionType, condition := range cm.conditions {
		allConditions[conditionType] = condition
	}

	// Apply pending updates
	for conditionType, condition := range cm.pendingUpdates {
		allConditions[conditionType] = condition
	}

	// Convert to slice
	for _, condition := range allConditions {
		conditionsToUpdate = append(conditionsToUpdate, condition)
	}

	log.Printf("[DEBUG] Flushing %d pending condition updates", len(cm.pendingUpdates))

	// Apply the updates
	err := cm.client.PatchNodeConditions(ctx, conditionsToUpdate)
	if err != nil {
		return fmt.Errorf("failed to patch node conditions: %w", err)
	}

	// Update our local state on success
	for conditionType, condition := range cm.pendingUpdates {
		cm.conditions[conditionType] = condition
	}

	// Clear pending updates
	cm.pendingUpdates = make(map[string]corev1.NodeCondition)
	cm.lastUpdate = time.Now()

	log.Printf("[DEBUG] Successfully flushed condition updates")
	return nil
}

// performResync synchronizes our local state with Kubernetes
func (cm *ConditionManager) performResync(ctx context.Context) error {
	log.Printf("[DEBUG] Performing condition resync...")

	node, err := cm.client.GetNode(ctx)
	if err != nil {
		return fmt.Errorf("failed to get current node state for resync: %w", err)
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Update our local condition state
	oldConditionCount := len(cm.conditions)
	cm.conditions = make(map[string]corev1.NodeCondition)

	for _, condition := range node.Status.Conditions {
		conditionType := string(condition.Type)
		cm.conditions[conditionType] = condition
	}

	cm.lastResync = time.Now()

	log.Printf("[DEBUG] Resync completed: %d -> %d conditions", oldConditionCount, len(cm.conditions))
	return nil
}

// sendHeartbeat updates a heartbeat condition to show Node Doctor is alive
func (cm *ConditionManager) sendHeartbeat() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	heartbeatCondition := corev1.NodeCondition{
		Type:               "NodeDoctorHealthy",
		Status:             corev1.ConditionTrue,
		LastTransitionTime: metav1.NewTime(time.Now()),
		Reason:             "Heartbeat",
		Message:            fmt.Sprintf("Node Doctor is running (last heartbeat: %s)", time.Now().Format(time.RFC3339)),
	}

	cm.pendingUpdates["NodeDoctorHealthy"] = heartbeatCondition
	cm.lastHeartbeat = time.Now()

	log.Printf("[DEBUG] Heartbeat condition queued")
}

// GetConditions returns a copy of the current conditions
func (cm *ConditionManager) GetConditions() map[string]corev1.NodeCondition {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	conditions := make(map[string]corev1.NodeCondition)
	for k, v := range cm.conditions {
		conditions[k] = v
	}
	return conditions
}

// GetPendingUpdates returns a copy of the pending updates
func (cm *ConditionManager) GetPendingUpdates() map[string]corev1.NodeCondition {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	updates := make(map[string]corev1.NodeCondition)
	for k, v := range cm.pendingUpdates {
		updates[k] = v
	}
	return updates
}

// GetStats returns statistics about the condition manager
func (cm *ConditionManager) GetStats() map[string]interface{} {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return map[string]interface{}{
		"current_conditions":   len(cm.conditions),
		"pending_updates":      len(cm.pendingUpdates),
		"last_update":          cm.lastUpdate.Format(time.RFC3339),
		"last_resync":          cm.lastResync.Format(time.RFC3339),
		"last_heartbeat":       cm.lastHeartbeat.Format(time.RFC3339),
		"update_interval":      cm.updateInterval.String(),
		"resync_interval":      cm.resyncInterval.String(),
		"heartbeat_interval":   cm.heartbeatInterval.String(),
	}
}

// IsHealthy returns true if the condition manager is operating normally
func (cm *ConditionManager) IsHealthy() bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	now := time.Now()

	// Check if we've updated recently
	if !cm.lastUpdate.IsZero() && now.Sub(cm.lastUpdate) > 2*cm.updateInterval {
		log.Printf("[WARN] Condition manager hasn't updated in %v", now.Sub(cm.lastUpdate))
		return false
	}

	// Check if we've resynced recently
	if !cm.lastResync.IsZero() && now.Sub(cm.lastResync) > 2*cm.resyncInterval {
		log.Printf("[WARN] Condition manager hasn't resynced in %v", now.Sub(cm.lastResync))
		return false
	}

	// Check if we have too many pending updates
	if len(cm.pendingUpdates) > 50 {
		log.Printf("[WARN] Too many pending condition updates: %d", len(cm.pendingUpdates))
		return false
	}

	return true
}

// conditionsEqual compares two node conditions for equality
func conditionsEqual(a, b corev1.NodeCondition) bool {
	// Compare all fields except LastTransitionTime (which changes when status changes)
	return a.Type == b.Type &&
		a.Status == b.Status &&
		a.Reason == b.Reason &&
		a.Message == b.Message
}

// ForceFlush immediately flushes all pending updates (primarily for testing)
func (cm *ConditionManager) ForceFlush(ctx context.Context) error {
	return cm.flushPendingUpdates(ctx)
}

// ForceResync immediately performs a resync (primarily for testing)
func (cm *ConditionManager) ForceResync(ctx context.Context) error {
	return cm.performResync(ctx)
}