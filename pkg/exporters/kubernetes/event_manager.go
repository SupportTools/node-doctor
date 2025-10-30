package kubernetes

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/supporttools/node-doctor/pkg/types"
)

// EventManager handles event creation with deduplication and rate limiting
type EventManager struct {
	client           *K8sClient
	config           *types.KubernetesExporterConfig
	mu               sync.RWMutex
	eventCache       map[string]time.Time       // Event signature -> last seen time
	rateLimiter      map[time.Time]int          // Minute timestamp -> event count
	maxEventsPerMin  int                        // Maximum events per minute
	deduplicationTTL time.Duration              // How long to remember events for deduplication
	cleanupInterval  time.Duration              // How often to clean up old cache entries
	stopCh           chan struct{}
	stopped          bool
	wg               sync.WaitGroup
}

// NewEventManager creates a new event manager with the specified configuration
func NewEventManager(client *K8sClient, config *types.KubernetesExporterConfig) *EventManager {
	maxEventsPerMin := 10 // Default rate limit
	if config.Events.MaxEventsPerMinute > 0 {
		maxEventsPerMin = config.Events.MaxEventsPerMinute
	}

	deduplicationTTL := 10 * time.Minute // Default deduplication window
	if config.Events.DeduplicationWindow > 0 {
		deduplicationTTL = config.Events.DeduplicationWindow
	}

	manager := &EventManager{
		client:           client,
		config:           config,
		eventCache:       make(map[string]time.Time),
		rateLimiter:      make(map[time.Time]int),
		maxEventsPerMin:  maxEventsPerMin,
		deduplicationTTL: deduplicationTTL,
		cleanupInterval:  time.Minute, // Clean up every minute
		stopCh:           make(chan struct{}),
	}

	return manager
}

// Start begins the event manager background processes
func (em *EventManager) Start(ctx context.Context) {
	em.mu.Lock()
	defer em.mu.Unlock()

	if em.stopped {
		// Reset the manager for restart
		em.stopCh = make(chan struct{})
		em.stopped = false
	}

	em.wg.Add(1)
	go em.cleanupLoop(ctx)
	log.Printf("[INFO] Event manager started with rate limit %d/min, deduplication TTL %v",
		em.maxEventsPerMin, em.deduplicationTTL)
}

// Stop gracefully stops the event manager
func (em *EventManager) Stop() {
	em.mu.Lock()
	if em.stopped {
		em.mu.Unlock()
		return // Already stopped
	}

	close(em.stopCh)
	em.stopped = true

	// We need to wait for goroutines while holding the lock to prevent race conditions
	// We can't unlock and relock because Start() could be called concurrently
	// Instead, we'll use a separate mechanism to wait for completion
	stopComplete := make(chan struct{})
	go func() {
		em.wg.Wait()
		close(stopComplete)
	}()

	em.mu.Unlock()
	<-stopComplete // Wait for all goroutines to complete

	log.Printf("[INFO] Event manager stopped")
}

// CreateEvent creates a Kubernetes event with deduplication and rate limiting
func (em *EventManager) CreateEvent(ctx context.Context, event corev1.Event) error {
	namespace := em.config.Namespace
	if namespace == "" {
		namespace = "default"
	}

	// Check rate limiting first
	if !em.checkRateLimit() {
		log.Printf("[WARN] Event rate limit exceeded (%d/min), dropping event: %s",
			em.maxEventsPerMin, event.Reason)
		return fmt.Errorf("event rate limit exceeded")
	}

	// Check deduplication
	signature := CreateEventSignature(event)
	if em.isDuplicate(signature) {
		log.Printf("[DEBUG] Duplicate event suppressed: %s (signature: %s)",
			event.Reason, signature.Hash())
		return nil
	}

	// Create the event
	err := em.client.CreateEvent(ctx, event, namespace)
	if err != nil {
		return fmt.Errorf("failed to create event: %w", err)
	}

	// Record the event for deduplication and rate limiting
	em.recordEvent(signature)

	log.Printf("[DEBUG] Event created successfully: %s/%s (reason: %s)",
		namespace, event.Name, event.Reason)
	return nil
}

// CreateEventsFromStatus creates events from a Node Doctor status
func (em *EventManager) CreateEventsFromStatus(ctx context.Context, status *types.Status) error {
	events := ConvertStatusToEvents(status, em.client.GetNodeName())

	var lastErr error
	createdCount := 0
	droppedCount := 0

	for _, event := range events {
		err := em.CreateEvent(ctx, event)
		if err != nil {
			log.Printf("[WARN] Failed to create event from status: %v", err)
			lastErr = err
			droppedCount++
		} else {
			createdCount++
		}
	}

	log.Printf("[DEBUG] Created %d events from status (dropped: %d)", createdCount, droppedCount)

	// Return the last error if any occurred
	return lastErr
}

// CreateEventFromProblem creates an event from a Node Doctor problem
func (em *EventManager) CreateEventFromProblem(ctx context.Context, problem *types.Problem) error {
	event := ConvertProblemToEvent(problem, em.client.GetNodeName())
	return em.CreateEvent(ctx, event)
}

// checkRateLimit checks if we're within the rate limit for event creation
func (em *EventManager) checkRateLimit() bool {
	em.mu.Lock()
	defer em.mu.Unlock()

	now := time.Now()
	currentMinute := now.Truncate(time.Minute)

	// Clean up old rate limiter entries (older than 2 minutes)
	for timestamp := range em.rateLimiter {
		if now.Sub(timestamp) > 2*time.Minute {
			delete(em.rateLimiter, timestamp)
		}
	}

	// Check current minute's count
	currentCount := em.rateLimiter[currentMinute]
	if currentCount >= em.maxEventsPerMin {
		return false
	}

	// Increment the count for this minute
	em.rateLimiter[currentMinute] = currentCount + 1
	return true
}

// isDuplicate checks if an event is a duplicate based on its signature
func (em *EventManager) isDuplicate(signature EventSignature) bool {
	em.mu.RLock()
	defer em.mu.RUnlock()

	hash := signature.Hash()
	lastSeen, exists := em.eventCache[hash]

	if !exists {
		return false
	}

	// Check if the event is within the deduplication window
	return time.Since(lastSeen) < em.deduplicationTTL
}

// recordEvent records an event signature for deduplication and rate limiting
func (em *EventManager) recordEvent(signature EventSignature) {
	em.mu.Lock()
	defer em.mu.Unlock()

	hash := signature.Hash()
	// Use the signature's timestamp if it's not zero, otherwise use current time
	timestamp := signature.Timestamp
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	em.eventCache[hash] = timestamp
}

// cleanupLoop periodically cleans up old cache entries
func (em *EventManager) cleanupLoop(ctx context.Context) {
	defer em.wg.Done()

	ticker := time.NewTicker(em.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("[DEBUG] Event manager cleanup loop stopping due to context cancellation")
			return
		case <-em.stopCh:
			log.Printf("[DEBUG] Event manager cleanup loop stopping due to stop signal")
			return
		case <-ticker.C:
			em.cleanup()
		}
	}
}

// cleanup removes old entries from the event cache and rate limiter
func (em *EventManager) cleanup() {
	em.mu.Lock()
	defer em.mu.Unlock()

	now := time.Now()
	cleanupCount := 0

	// Clean up event cache entries older than TTL
	for hash, timestamp := range em.eventCache {
		if now.Sub(timestamp) > em.deduplicationTTL {
			delete(em.eventCache, hash)
			cleanupCount++
		}
	}

	// Clean up rate limiter entries older than 2 minutes
	for timestamp := range em.rateLimiter {
		if now.Sub(timestamp) > 2*time.Minute {
			delete(em.rateLimiter, timestamp)
		}
	}

	if cleanupCount > 0 {
		log.Printf("[DEBUG] Event manager cleanup: removed %d expired cache entries", cleanupCount)
	}
}

// GetStats returns statistics about the event manager
func (em *EventManager) GetStats() map[string]interface{} {
	em.mu.RLock()
	defer em.mu.RUnlock()

	now := time.Now()
	currentMinute := now.Truncate(time.Minute)
	currentCount := em.rateLimiter[currentMinute]

	return map[string]interface{}{
		"cached_signatures":    len(em.eventCache),
		"current_minute_count": currentCount,
		"max_events_per_min":   em.maxEventsPerMin,
		"deduplication_ttl":    em.deduplicationTTL.String(),
	}
}

// ResetRateLimit resets the rate limiter (primarily for testing)
func (em *EventManager) ResetRateLimit() {
	em.mu.Lock()
	defer em.mu.Unlock()

	em.rateLimiter = make(map[time.Time]int)
	log.Printf("[DEBUG] Event manager rate limiter reset")
}

// ClearCache clears the deduplication cache (primarily for testing)
func (em *EventManager) ClearCache() {
	em.mu.Lock()
	defer em.mu.Unlock()

	em.eventCache = make(map[string]time.Time)
	log.Printf("[DEBUG] Event manager cache cleared")
}

// IsHealthy returns true if the event manager is operating normally
func (em *EventManager) IsHealthy() bool {
	em.mu.RLock()
	defer em.mu.RUnlock()

	// Check if caches are reasonable size (not growing unbounded)
	maxCacheSize := 10000
	if len(em.eventCache) > maxCacheSize {
		log.Printf("[WARN] Event cache size is large: %d entries", len(em.eventCache))
		return false
	}

	// Check if rate limiter has reasonable number of entries
	if len(em.rateLimiter) > 60 { // More than 60 minutes of data
		log.Printf("[WARN] Rate limiter cache size is large: %d entries", len(em.rateLimiter))
		return false
	}

	return true
}