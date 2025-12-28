package controller

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
)

// Correlation types
const (
	CorrelationTypeInfrastructure = "infrastructure" // Same problem across many nodes
	CorrelationTypeCommonCause    = "common-cause"   // Different problems with shared root cause
	CorrelationTypeCascade        = "cascade"        // Sequential problems triggering each other
)

// Correlation statuses
const (
	CorrelationStatusActive        = "active"
	CorrelationStatusInvestigating = "investigating"
	CorrelationStatusResolved      = "resolved"
)

// Correlator detects patterns across node reports and identifies cluster-wide issues.
type Correlator struct {
	config  *CorrelationConfig
	storage Storage
	metrics *ControllerMetrics
	events  *EventRecorder

	mu     sync.RWMutex
	active map[string]*Correlation // id -> correlation

	// Tracking for detection
	nodeReports  map[string]*NodeReport // nodeName -> latest report
	totalNodes   int
	lastEvalTime time.Time

	// Lifecycle
	started bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// NewCorrelator creates a new Correlator instance.
func NewCorrelator(config *CorrelationConfig, storage Storage, metrics *ControllerMetrics, events *EventRecorder) *Correlator {
	if config == nil {
		config = &CorrelationConfig{
			Enabled:                true,
			ClusterWideThreshold:   0.3,
			EvaluationInterval:     30 * time.Second,
			MinNodesForCorrelation: 2,
		}
	}

	return &Correlator{
		config:      config,
		storage:     storage,
		metrics:     metrics,
		events:      events,
		active:      make(map[string]*Correlation),
		nodeReports: make(map[string]*NodeReport),
		stopCh:      make(chan struct{}),
	}
}

// Start begins the background correlation evaluation loop.
func (c *Correlator) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return fmt.Errorf("correlator already started")
	}

	if !c.config.Enabled {
		log.Printf("[INFO] Correlator is disabled, not starting")
		return nil
	}

	// Load active correlations from storage
	if c.storage != nil {
		correlations, err := c.storage.GetActiveCorrelations(ctx)
		if err != nil {
			log.Printf("[WARN] Failed to load active correlations: %v", err)
		} else {
			for _, corr := range correlations {
				c.active[corr.ID] = corr
			}
			log.Printf("[INFO] Loaded %d active correlations from storage", len(correlations))
		}
	}

	c.started = true
	c.stopCh = make(chan struct{})

	// Start evaluation loop
	c.wg.Add(1)
	go c.evaluationLoop(ctx)

	log.Printf("[INFO] Correlator started with interval %v", c.config.EvaluationInterval)
	return nil
}

// Stop stops the correlator.
func (c *Correlator) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.started {
		return nil
	}

	close(c.stopCh)
	c.wg.Wait()
	c.started = false

	log.Printf("[INFO] Correlator stopped")
	return nil
}

// UpdateNodeReport updates the cached report for a node.
func (c *Correlator) UpdateNodeReport(report *NodeReport) {
	if report == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.nodeReports[report.NodeName] = report
	c.totalNodes = len(c.nodeReports)
}

// RemoveNode removes a node from tracking.
func (c *Correlator) RemoveNode(nodeName string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.nodeReports, nodeName)
	c.totalNodes = len(c.nodeReports)
}

// GetActiveCorrelations returns all currently active correlations.
func (c *Correlator) GetActiveCorrelations() []*Correlation {
	c.mu.RLock()
	defer c.mu.RUnlock()

	correlations := make([]*Correlation, 0, len(c.active))
	for _, corr := range c.active {
		// Return a copy
		corrCopy := *corr
		corrCopy.AffectedNodes = append([]string{}, corr.AffectedNodes...)
		corrCopy.ProblemTypes = append([]string{}, corr.ProblemTypes...)
		correlations = append(correlations, &corrCopy)
	}

	// Sort by detected time, newest first
	sort.Slice(correlations, func(i, j int) bool {
		return correlations[i].DetectedAt.After(correlations[j].DetectedAt)
	})

	return correlations
}

// GetCorrelation returns a specific correlation by ID.
func (c *Correlator) GetCorrelation(id string) (*Correlation, error) {
	c.mu.RLock()
	corr, exists := c.active[id]
	c.mu.RUnlock()

	if exists {
		corrCopy := *corr
		corrCopy.AffectedNodes = append([]string{}, corr.AffectedNodes...)
		corrCopy.ProblemTypes = append([]string{}, corr.ProblemTypes...)
		return &corrCopy, nil
	}

	// Try storage
	if c.storage != nil {
		return c.storage.GetCorrelation(context.Background(), id)
	}

	return nil, fmt.Errorf("correlation not found: %s", id)
}

// evaluationLoop runs periodic correlation evaluation.
func (c *Correlator) evaluationLoop(ctx context.Context) {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.EvaluationInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.evaluate(ctx)
		}
	}
}

// evaluate runs all correlation detection algorithms.
func (c *Correlator) evaluate(ctx context.Context) {
	c.mu.Lock()
	reports := c.getReportsCopy()
	totalNodes := c.totalNodes
	c.lastEvalTime = time.Now()
	c.mu.Unlock()

	if len(reports) < c.config.MinNodesForCorrelation {
		return
	}

	// Run detection algorithms
	infrastructureCorrelations := c.detectInfrastructureCorrelation(reports, totalNodes)
	commonCauseCorrelations := c.detectCommonCauseCorrelation(reports)
	cascadeCorrelations := c.detectCascadeCorrelation(reports)

	// Merge all detected correlations
	allDetected := make([]*Correlation, 0)
	allDetected = append(allDetected, infrastructureCorrelations...)
	allDetected = append(allDetected, commonCauseCorrelations...)
	allDetected = append(allDetected, cascadeCorrelations...)

	// Update active correlations
	c.mu.Lock()
	defer c.mu.Unlock()

	// Process newly detected correlations
	for _, corr := range allDetected {
		existing, exists := c.active[corr.ID]
		if exists {
			// Update existing correlation
			existing.AffectedNodes = corr.AffectedNodes
			existing.UpdatedAt = time.Now()
			existing.Confidence = corr.Confidence

			if c.storage != nil {
				if err := c.storage.UpdateCorrelation(ctx, existing); err != nil {
					log.Printf("[WARN] Failed to update correlation %s: %v", existing.ID, err)
				}
			}
		} else {
			// New correlation detected
			corr.DetectedAt = time.Now()
			corr.UpdatedAt = time.Now()
			corr.Status = CorrelationStatusActive

			c.active[corr.ID] = corr

			if c.storage != nil {
				if err := c.storage.SaveCorrelation(ctx, corr); err != nil {
					log.Printf("[WARN] Failed to save correlation %s: %v", corr.ID, err)
				}
			}

			// Record event
			if c.events != nil {
				c.events.RecordCorrelation(ctx, corr)
			}

			// Update metrics
			if c.metrics != nil {
				c.metrics.RecordCorrelationDetected(corr.Type)
			}

			log.Printf("[INFO] New correlation detected: %s (type=%s, nodes=%d, problems=%v)",
				corr.ID, corr.Type, len(corr.AffectedNodes), corr.ProblemTypes)
		}
	}

	// Check for resolved correlations
	c.resolveStaleCorrelations(ctx, reports)

	// Update metrics
	if c.metrics != nil {
		c.metrics.UpdateCorrelationMetrics(len(c.active))
	}
}

// getReportsCopy returns a copy of current node reports.
func (c *Correlator) getReportsCopy() []*NodeReport {
	reports := make([]*NodeReport, 0, len(c.nodeReports))
	for _, report := range c.nodeReports {
		reports = append(reports, report)
	}
	return reports
}

// detectInfrastructureCorrelation detects when the same problem type affects many nodes.
// Example: DNS failures on 30%+ of nodes indicates infrastructure issue.
func (c *Correlator) detectInfrastructureCorrelation(reports []*NodeReport, totalNodes int) []*Correlation {
	if totalNodes < c.config.MinNodesForCorrelation {
		return nil
	}

	// Group problems by type
	problemsByType := make(map[string][]string) // problemType -> []nodeName

	for _, report := range reports {
		for _, problem := range report.ActiveProblems {
			if problemsByType[problem.Type] == nil {
				problemsByType[problem.Type] = make([]string, 0)
			}
			// Avoid duplicates
			found := false
			for _, n := range problemsByType[problem.Type] {
				if n == report.NodeName {
					found = true
					break
				}
			}
			if !found {
				problemsByType[problem.Type] = append(problemsByType[problem.Type], report.NodeName)
			}
		}
	}

	var correlations []*Correlation

	for problemType, nodes := range problemsByType {
		ratio := float64(len(nodes)) / float64(totalNodes)

		if ratio >= c.config.ClusterWideThreshold && len(nodes) >= c.config.MinNodesForCorrelation {
			// Generate deterministic ID based on problem type
			id := c.generateCorrelationID(CorrelationTypeInfrastructure, problemType)

			// Calculate confidence based on ratio
			confidence := ratio
			if confidence > 1.0 {
				confidence = 1.0
			}

			// Determine severity based on ratio
			severity := "warning"
			if ratio >= 0.5 {
				severity = "critical"
			}

			sort.Strings(nodes)

			correlations = append(correlations, &Correlation{
				ID:            id,
				Type:          CorrelationTypeInfrastructure,
				Severity:      severity,
				AffectedNodes: nodes,
				ProblemTypes:  []string{problemType},
				Message:       fmt.Sprintf("Infrastructure issue: %s affecting %.0f%% of nodes (%d/%d)", problemType, ratio*100, len(nodes), totalNodes),
				Confidence:    confidence,
				Metadata: map[string]interface{}{
					"ratio":      ratio,
					"totalNodes": totalNodes,
				},
			})
		}
	}

	return correlations
}

// detectCommonCauseCorrelation detects when different problem types appear together.
// Example: Memory pressure + Disk pressure on same nodes = resource exhaustion.
func (c *Correlator) detectCommonCauseCorrelation(reports []*NodeReport) []*Correlation {
	// Known common-cause patterns
	commonCausePatterns := []struct {
		problems    []string
		name        string
		description string
	}{
		{
			problems:    []string{"MemoryPressure", "DiskPressure"},
			name:        "resource-exhaustion",
			description: "Memory and disk pressure indicate resource exhaustion",
		},
		{
			problems:    []string{"KubeletUnhealthy", "ContainerRuntimeUnhealthy"},
			name:        "node-runtime-failure",
			description: "Kubelet and container runtime both unhealthy indicates node-level failure",
		},
		{
			problems:    []string{"DNSFailure", "NetworkUnreachable"},
			name:        "network-infrastructure",
			description: "DNS and network failures indicate network infrastructure issue",
		},
	}

	var correlations []*Correlation

	for _, pattern := range commonCausePatterns {
		nodesWithPattern := c.findNodesWithAllProblems(reports, pattern.problems)

		if len(nodesWithPattern) >= c.config.MinNodesForCorrelation {
			id := c.generateCorrelationID(CorrelationTypeCommonCause, pattern.name)

			confidence := float64(len(nodesWithPattern)) / float64(len(reports))
			if confidence > 1.0 {
				confidence = 1.0
			}

			sort.Strings(nodesWithPattern)

			correlations = append(correlations, &Correlation{
				ID:            id,
				Type:          CorrelationTypeCommonCause,
				Severity:      "warning",
				AffectedNodes: nodesWithPattern,
				ProblemTypes:  pattern.problems,
				Message:       fmt.Sprintf("Common cause detected: %s on %d nodes", pattern.description, len(nodesWithPattern)),
				Confidence:    confidence,
				Metadata: map[string]interface{}{
					"pattern": pattern.name,
				},
			})
		}
	}

	return correlations
}

// detectCascadeCorrelation detects sequential problem chains.
// Example: Kubelet down -> pods not running -> node NotReady
func (c *Correlator) detectCascadeCorrelation(reports []*NodeReport) []*Correlation {
	// Known cascade patterns (ordered by cause -> effect)
	cascadePatterns := []struct {
		chain       []string
		name        string
		description string
	}{
		{
			chain:       []string{"KubeletUnhealthy", "PodNotRunning", "NodeNotReady"},
			name:        "kubelet-cascade",
			description: "Kubelet failure causing pod and node issues",
		},
		{
			chain:       []string{"DiskPressure", "PodEviction", "CapacityReduced"},
			name:        "disk-cascade",
			description: "Disk pressure causing pod evictions and capacity reduction",
		},
	}

	var correlations []*Correlation

	for _, pattern := range cascadePatterns {
		nodesWithCascade := c.findNodesWithCascade(reports, pattern.chain)

		if len(nodesWithCascade) >= c.config.MinNodesForCorrelation {
			id := c.generateCorrelationID(CorrelationTypeCascade, pattern.name)

			sort.Strings(nodesWithCascade)

			correlations = append(correlations, &Correlation{
				ID:            id,
				Type:          CorrelationTypeCascade,
				Severity:      "critical",
				AffectedNodes: nodesWithCascade,
				ProblemTypes:  pattern.chain,
				Message:       fmt.Sprintf("Cascade failure: %s affecting %d nodes", pattern.description, len(nodesWithCascade)),
				Confidence:    0.8, // Cascade patterns have high confidence when detected
				Metadata: map[string]interface{}{
					"pattern": pattern.name,
					"chain":   pattern.chain,
				},
			})
		}
	}

	return correlations
}

// findNodesWithAllProblems finds nodes that have all specified problem types.
func (c *Correlator) findNodesWithAllProblems(reports []*NodeReport, problemTypes []string) []string {
	var nodes []string

	for _, report := range reports {
		if c.nodeHasAllProblems(report, problemTypes) {
			nodes = append(nodes, report.NodeName)
		}
	}

	return nodes
}

// nodeHasAllProblems checks if a node has all specified problem types.
func (c *Correlator) nodeHasAllProblems(report *NodeReport, problemTypes []string) bool {
	problemSet := make(map[string]bool)
	for _, problem := range report.ActiveProblems {
		problemSet[problem.Type] = true
	}

	for _, pt := range problemTypes {
		if !problemSet[pt] {
			return false
		}
	}

	return true
}

// findNodesWithCascade finds nodes that have at least the first problem in the chain.
// (Cascade detection is more lenient - we look for the root cause)
func (c *Correlator) findNodesWithCascade(reports []*NodeReport, chain []string) []string {
	if len(chain) == 0 {
		return nil
	}

	var nodes []string

	for _, report := range reports {
		// Check if node has at least the first 2 problems in the chain
		matchCount := 0
		for _, chainItem := range chain {
			for _, problem := range report.ActiveProblems {
				if problem.Type == chainItem {
					matchCount++
					break
				}
			}
		}

		// Require at least 2 items in the chain to be present
		if matchCount >= 2 {
			nodes = append(nodes, report.NodeName)
		}
	}

	return nodes
}

// resolveStaleCorrelations marks correlations as resolved when problems clear.
func (c *Correlator) resolveStaleCorrelations(ctx context.Context, reports []*NodeReport) {
	// Build current problem map
	currentProblems := make(map[string]map[string]bool) // nodeName -> problemType -> exists

	for _, report := range reports {
		if currentProblems[report.NodeName] == nil {
			currentProblems[report.NodeName] = make(map[string]bool)
		}
		for _, problem := range report.ActiveProblems {
			currentProblems[report.NodeName][problem.Type] = true
		}
	}

	// Check each active correlation
	for id, corr := range c.active {
		// Check if any affected node still has the problems
		stillActive := false

		for _, nodeName := range corr.AffectedNodes {
			nodeProblems := currentProblems[nodeName]
			if nodeProblems == nil {
				continue
			}

			for _, problemType := range corr.ProblemTypes {
				if nodeProblems[problemType] {
					stillActive = true
					break
				}
			}

			if stillActive {
				break
			}
		}

		if !stillActive {
			// Mark as resolved
			corr.Status = CorrelationStatusResolved
			corr.UpdatedAt = time.Now()

			if c.storage != nil {
				if err := c.storage.UpdateCorrelation(ctx, corr); err != nil {
					log.Printf("[WARN] Failed to update resolved correlation %s: %v", id, err)
				}
			}

			if c.events != nil {
				c.events.RecordCorrelationResolved(ctx, corr)
			}

			delete(c.active, id)

			log.Printf("[INFO] Correlation resolved: %s (type=%s)", id, corr.Type)
		}
	}
}

// generateCorrelationID creates a deterministic ID for a correlation.
func (c *Correlator) generateCorrelationID(corrType, identifier string) string {
	data := fmt.Sprintf("%s:%s", corrType, identifier)
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("corr-%s-%x", corrType[:4], hash[:4])
}

// GetStats returns correlator statistics.
func (c *Correlator) GetStats() CorrelatorStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return CorrelatorStats{
		ActiveCorrelations: len(c.active),
		TrackedNodes:       len(c.nodeReports),
		LastEvalTime:       c.lastEvalTime,
		Started:            c.started,
	}
}

// CorrelatorStats contains correlator statistics.
type CorrelatorStats struct {
	ActiveCorrelations int       `json:"activeCorrelations"`
	TrackedNodes       int       `json:"trackedNodes"`
	LastEvalTime       time.Time `json:"lastEvalTime"`
	Started            bool      `json:"started"`
}

// EvaluateNow triggers an immediate correlation evaluation.
func (c *Correlator) EvaluateNow(ctx context.Context) {
	c.evaluate(ctx)
}

// InjectProblemPattern allows adding custom problem patterns for detection.
// This is useful for extending the correlator with domain-specific patterns.
func (c *Correlator) InjectProblemPattern(patternType string, problems []string, name, description string) {
	// This is a hook for future extensibility
	log.Printf("[DEBUG] Pattern injection not yet implemented: %s", name)
}

// ForceResolve forces a correlation to be resolved (for manual intervention).
func (c *Correlator) ForceResolve(ctx context.Context, correlationID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	corr, exists := c.active[correlationID]
	if !exists {
		return fmt.Errorf("correlation not found: %s", correlationID)
	}

	corr.Status = CorrelationStatusResolved
	corr.UpdatedAt = time.Now()

	if c.storage != nil {
		if err := c.storage.UpdateCorrelation(ctx, corr); err != nil {
			return fmt.Errorf("failed to update correlation: %w", err)
		}
	}

	if c.events != nil {
		c.events.RecordCorrelationResolved(ctx, corr)
	}

	delete(c.active, correlationID)

	log.Printf("[INFO] Correlation force-resolved: %s", correlationID)
	return nil
}

// Ensure unused import doesn't cause issues
var _ = strings.TrimSpace
