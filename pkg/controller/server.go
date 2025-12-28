package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Server is the main HTTP server for the Node Doctor Controller
type Server struct {
	config     *ControllerConfig
	httpServer *http.Server
	mux        *http.ServeMux

	// Storage backend (nil uses in-memory fallback)
	storage Storage

	// Metrics
	metrics *ControllerMetrics

	// Correlator for pattern detection
	correlator *Correlator

	// State
	mu        sync.RWMutex
	started   bool
	ready     bool
	startTime time.Time

	// In-memory state (fallback when storage is nil)
	nodeReports map[string]*NodeReport // nodeName -> latest report
	leases      map[string]*Lease      // leaseID -> lease
}

// NewServer creates a new controller server
func NewServer(config *ControllerConfig) (*Server, error) {
	if config == nil {
		config = DefaultControllerConfig()
	}

	s := &Server{
		config:      config,
		nodeReports: make(map[string]*NodeReport),
		leases:      make(map[string]*Lease),
		startTime:   time.Now(),
		metrics:     NewControllerMetrics(),
	}

	// Initialize correlator if enabled
	if config.Correlation.Enabled {
		s.correlator = NewCorrelator(&config.Correlation, nil, s.metrics, nil)
	}

	// Initialize HTTP routes
	s.mux = http.NewServeMux()
	s.setupRoutes()

	return s, nil
}

// setupRoutes configures all HTTP routes
func (s *Server) setupRoutes() {
	// Health endpoints
	s.mux.HandleFunc("/healthz", s.handleHealthz)
	s.mux.HandleFunc("/readyz", s.handleReadyz)

	// API v1 endpoints
	s.mux.HandleFunc("/api/v1/reports", s.handleReports)
	s.mux.HandleFunc("/api/v1/cluster/status", s.handleClusterStatus)
	s.mux.HandleFunc("/api/v1/cluster/problems", s.handleClusterProblems)
	s.mux.HandleFunc("/api/v1/nodes", s.handleNodes)
	s.mux.HandleFunc("/api/v1/nodes/", s.handleNodeDetail)
	s.mux.HandleFunc("/api/v1/correlations", s.handleCorrelations)
	s.mux.HandleFunc("/api/v1/correlations/", s.handleCorrelationDetail)
	s.mux.HandleFunc("/api/v1/leases", s.handleLeases)
	s.mux.HandleFunc("/api/v1/leases/", s.handleLeaseDetail)

	// Prometheus metrics (placeholder - will be implemented in metrics.go)
	s.mux.HandleFunc("/metrics", s.handleMetrics)
}

// Start starts the HTTP server
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return fmt.Errorf("server already started")
	}

	addr := fmt.Sprintf("%s:%d", s.config.Server.BindAddress, s.config.Server.Port)
	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      s.loggingMiddleware(s.mux),
		ReadTimeout:  s.config.Server.ReadTimeout,
		WriteTimeout: s.config.Server.WriteTimeout,
	}

	// Start HTTP server in background
	go func() {
		log.Printf("[INFO] Starting controller server on %s", addr)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[ERROR] Controller server failed: %v", err)
		}
	}()

	// Give server a moment to start
	time.Sleep(100 * time.Millisecond)

	// Start correlator if available
	if s.correlator != nil {
		if err := s.correlator.Start(ctx); err != nil {
			log.Printf("[WARN] Failed to start correlator: %v", err)
		}
	}

	s.started = true
	s.ready = true
	s.startTime = time.Now()

	log.Printf("[INFO] Controller server started on %s", addr)
	return nil
}

// Stop gracefully stops the HTTP server
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return nil
	}

	log.Printf("[INFO] Stopping controller server...")

	// Stop correlator first
	if s.correlator != nil {
		if err := s.correlator.Stop(); err != nil {
			log.Printf("[WARN] Failed to stop correlator: %v", err)
		}
	}

	// Create shutdown context with timeout if not provided
	shutdownCtx := ctx
	if ctx == nil {
		var cancel context.CancelFunc
		shutdownCtx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
	}

	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("failed to shutdown server: %w", err)
	}

	s.started = false
	s.ready = false

	log.Printf("[INFO] Controller server stopped")
	return nil
}

// loggingMiddleware adds request logging
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		// Create a response wrapper to capture status code
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)
		duration := time.Since(start)
		log.Printf("[INFO] %s %s %d %s", r.Method, r.URL.Path, rw.statusCode, duration)
	})
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// =====================
// Health Endpoints
// =====================

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	uptime := time.Since(s.startTime).Round(time.Second).String()

	response := map[string]interface{}{
		"status":    "ok",
		"timestamp": time.Now(),
		"uptime":    uptime,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	response := map[string]interface{}{
		"ready":     s.ready,
		"timestamp": time.Now(),
	}

	if s.ready {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
	} else {
		response["message"] = "Server not ready"
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	json.NewEncoder(w).Encode(response)
}

// =====================
// Report Ingestion
// =====================

func (s *Server) handleReports(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed")
		return
	}

	var report NodeReport
	if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
		s.metrics.RecordReportError("invalid_json")
		s.writeError(w, http.StatusBadRequest, "INVALID_JSON", "Failed to parse request body")
		return
	}

	if report.NodeName == "" {
		s.metrics.RecordReportError("missing_node_name")
		s.writeError(w, http.StatusBadRequest, "MISSING_NODE_NAME", "nodeName is required")
		return
	}

	// Record successful report reception
	s.metrics.RecordReportReceived()

	// Set timestamp if not provided
	if report.Timestamp.IsZero() {
		report.Timestamp = time.Now()
	}

	// Store the report in memory for fast access
	s.mu.Lock()
	s.nodeReports[report.NodeName] = &report
	storage := s.storage
	correlator := s.correlator
	s.mu.Unlock()

	// Persist to storage if available
	if storage != nil {
		if err := storage.SaveNodeReport(context.Background(), &report); err != nil {
			log.Printf("[WARN] Failed to persist report to storage: %v", err)
		}
	}

	// Update correlator with new report
	if correlator != nil {
		correlator.UpdateNodeReport(&report)
	}

	log.Printf("[DEBUG] Received report from node %s (health: %s, problems: %d)",
		report.NodeName, report.OverallHealth, len(report.ActiveProblems))

	s.writeSuccess(w, http.StatusAccepted, map[string]interface{}{
		"accepted":  true,
		"nodeName":  report.NodeName,
		"timestamp": report.Timestamp,
	})
}

// =====================
// Cluster Status
// =====================

func (s *Server) handleClusterStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed")
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	status := s.buildClusterStatus()
	s.writeSuccess(w, http.StatusOK, status)
}

func (s *Server) buildClusterStatus() *ClusterStatus {
	status := &ClusterStatus{
		Timestamp:     time.Now(),
		OverallHealth: HealthStatusHealthy,
		TotalNodes:    len(s.nodeReports),
		NodeSummaries: make([]NodeSummary, 0, len(s.nodeReports)),
	}

	for nodeName, report := range s.nodeReports {
		summary := NodeSummary{
			NodeName:       nodeName,
			Health:         report.OverallHealth,
			LastReportAt:   report.Timestamp,
			ProblemCount:   len(report.ActiveProblems),
			ConditionCount: len(report.Conditions),
		}
		status.NodeSummaries = append(status.NodeSummaries, summary)

		// Count by health status
		switch report.OverallHealth {
		case HealthStatusHealthy:
			status.HealthyNodes++
		case HealthStatusDegraded:
			status.DegradedNodes++
		case HealthStatusCritical:
			status.CriticalNodes++
		default:
			status.UnknownNodes++
		}

		status.ActiveProblems += len(report.ActiveProblems)
	}

	// Determine overall health
	if status.CriticalNodes > 0 {
		status.OverallHealth = HealthStatusCritical
	} else if status.DegradedNodes > 0 {
		status.OverallHealth = HealthStatusDegraded
	} else if status.TotalNodes == 0 {
		status.OverallHealth = HealthStatusUnknown
	}

	return status
}

func (s *Server) handleClusterProblems(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed")
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Aggregate problems from all nodes
	problems := make([]ClusterProblem, 0)
	for nodeName, report := range s.nodeReports {
		for _, p := range report.ActiveProblems {
			problem := ClusterProblem{
				ID:            fmt.Sprintf("%s-%s-%d", nodeName, p.Type, p.DetectedAt.Unix()),
				Type:          p.Type,
				Severity:      p.Severity,
				AffectedNodes: []string{nodeName},
				Message:       p.Message,
				DetectedAt:    p.DetectedAt,
			}
			problems = append(problems, problem)
		}
	}

	s.writeSuccess(w, http.StatusOK, problems)
}

// =====================
// Node Endpoints
// =====================

func (s *Server) handleNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed")
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	summaries := make([]NodeSummary, 0, len(s.nodeReports))
	for nodeName, report := range s.nodeReports {
		summary := NodeSummary{
			NodeName:       nodeName,
			Health:         report.OverallHealth,
			LastReportAt:   report.Timestamp,
			ProblemCount:   len(report.ActiveProblems),
			ConditionCount: len(report.Conditions),
		}
		summaries = append(summaries, summary)
	}

	s.writeSuccess(w, http.StatusOK, summaries)
}

func (s *Server) handleNodeDetail(w http.ResponseWriter, r *http.Request) {
	// Extract node name from path: /api/v1/nodes/{name}
	nodeName := r.URL.Path[len("/api/v1/nodes/"):]
	if nodeName == "" {
		s.writeError(w, http.StatusBadRequest, "MISSING_NODE_NAME", "Node name is required")
		return
	}

	// Check for /history suffix
	if len(nodeName) > 8 && nodeName[len(nodeName)-8:] == "/history" {
		nodeName = nodeName[:len(nodeName)-8]
		s.handleNodeHistory(w, r, nodeName)
		return
	}

	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed")
		return
	}

	s.mu.RLock()
	report, exists := s.nodeReports[nodeName]
	s.mu.RUnlock()

	if !exists {
		s.writeError(w, http.StatusNotFound, "NODE_NOT_FOUND", "Node not found")
		return
	}

	detail := NodeDetail{
		NodeName:       nodeName,
		NodeUID:        report.NodeUID,
		Health:         report.OverallHealth,
		LastReportAt:   report.Timestamp,
		FirstSeenAt:    report.Timestamp, // Will be accurate with storage
		ReportCount:    1,                // Will be accurate with storage
		LatestReport:   report,
		ActiveProblems: report.ActiveProblems,
		Conditions:     report.Conditions,
	}

	s.writeSuccess(w, http.StatusOK, detail)
}

func (s *Server) handleNodeHistory(w http.ResponseWriter, r *http.Request, nodeName string) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed")
		return
	}

	s.mu.RLock()
	report, exists := s.nodeReports[nodeName]
	storage := s.storage
	s.mu.RUnlock()

	// Try to get history from storage first
	if storage != nil {
		reports, err := storage.GetNodeReports(context.Background(), nodeName, 100) // Get last 100 reports
		if err != nil {
			log.Printf("[WARN] Failed to get history from storage: %v", err)
		} else if len(reports) > 0 {
			history := make([]ReportSummary, 0, len(reports))
			for _, r := range reports {
				history = append(history, ReportSummary{
					ReportID:      r.ReportID,
					Timestamp:     r.Timestamp,
					OverallHealth: r.OverallHealth,
					ProblemCount:  len(r.ActiveProblems),
				})
			}
			s.writeSuccess(w, http.StatusOK, history)
			return
		}
	}

	// Fallback to in-memory
	if !exists {
		s.writeError(w, http.StatusNotFound, "NODE_NOT_FOUND", "Node not found")
		return
	}

	history := []ReportSummary{
		{
			ReportID:      report.ReportID,
			Timestamp:     report.Timestamp,
			OverallHealth: report.OverallHealth,
			ProblemCount:  len(report.ActiveProblems),
		},
	}

	s.writeSuccess(w, http.StatusOK, history)
}

// =====================
// Correlation Endpoints
// =====================

func (s *Server) handleCorrelations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed")
		return
	}

	s.mu.RLock()
	correlator := s.correlator
	s.mu.RUnlock()

	if correlator == nil {
		// Correlator not enabled, return empty list
		s.writeSuccess(w, http.StatusOK, []Correlation{})
		return
	}

	correlations := correlator.GetActiveCorrelations()
	s.writeSuccess(w, http.StatusOK, correlations)
}

func (s *Server) handleCorrelationDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed")
		return
	}

	// Extract correlation ID from path: /api/v1/correlations/{id}
	path := r.URL.Path
	prefix := "/api/v1/correlations/"
	if !strings.HasPrefix(path, prefix) {
		s.writeError(w, http.StatusBadRequest, "INVALID_PATH", "Invalid correlation path")
		return
	}
	correlationID := strings.TrimPrefix(path, prefix)
	if correlationID == "" {
		s.writeError(w, http.StatusBadRequest, "MISSING_ID", "Correlation ID is required")
		return
	}

	s.mu.RLock()
	correlator := s.correlator
	s.mu.RUnlock()

	if correlator == nil {
		s.writeError(w, http.StatusNotFound, "NOT_FOUND", "Correlator not enabled")
		return
	}

	correlation, err := correlator.GetCorrelation(correlationID)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "NOT_FOUND", "Correlation not found")
		return
	}

	s.writeSuccess(w, http.StatusOK, correlation)
}

// =====================
// Lease Endpoints
// =====================

func (s *Server) handleLeases(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listLeases(w, r)
	case http.MethodPost:
		s.requestLease(w, r)
	default:
		s.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET and POST are allowed")
	}
}

func (s *Server) listLeases(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	leases := make([]*Lease, 0, len(s.leases))
	for _, lease := range s.leases {
		if lease.Status == "active" {
			leases = append(leases, lease)
		}
	}

	s.writeSuccess(w, http.StatusOK, leases)
}

func (s *Server) requestLease(w http.ResponseWriter, r *http.Request) {
	var req LeaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "INVALID_JSON", "Failed to parse request body")
		return
	}

	if req.NodeName == "" || req.RemediationType == "" {
		s.writeError(w, http.StatusBadRequest, "MISSING_FIELDS", "node and remediation are required")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check current lease count
	activeCount := 0
	for _, lease := range s.leases {
		if lease.Status == "active" && time.Now().Before(lease.ExpiresAt) {
			activeCount++
		}
	}

	if activeCount >= s.config.Coordination.MaxConcurrentRemediations {
		s.metrics.RecordLeaseDenied("max_concurrent")
		response := LeaseResponse{
			Approved: false,
			Message:  "Maximum concurrent remediations reached",
			RetryAt:  time.Now().Add(30 * time.Second),
			Position: activeCount - s.config.Coordination.MaxConcurrentRemediations + 1,
		}
		s.writeSuccess(w, http.StatusTooManyRequests, response)
		return
	}

	// Check if node already has an active lease
	for _, lease := range s.leases {
		if lease.NodeName == req.NodeName && lease.Status == "active" && time.Now().Before(lease.ExpiresAt) {
			s.metrics.RecordLeaseDenied("node_has_lease")
			response := LeaseResponse{
				Approved: false,
				Message:  "Node already has an active lease",
				LeaseID:  lease.ID,
			}
			s.writeSuccess(w, http.StatusConflict, response)
			return
		}
	}

	// Grant lease
	duration := s.config.Coordination.DefaultLeaseDuration
	if req.RequestedDuration != "" {
		if d, err := time.ParseDuration(req.RequestedDuration); err == nil {
			duration = d
		}
	}

	leaseID := fmt.Sprintf("%s-%s-%d", req.NodeName, req.RemediationType, time.Now().UnixNano())
	lease := &Lease{
		ID:              leaseID,
		NodeName:        req.NodeName,
		RemediationType: req.RemediationType,
		GrantedAt:       time.Now(),
		ExpiresAt:       time.Now().Add(duration),
		Status:          "active",
		Reason:          req.Reason,
	}
	s.leases[leaseID] = lease

	// Persist to storage if available
	storage := s.storage
	if storage != nil {
		if err := storage.SaveLease(context.Background(), lease); err != nil {
			log.Printf("[WARN] Failed to persist lease to storage: %v", err)
		}
	}

	// Record metrics
	s.metrics.RecordLeaseGranted(req.RemediationType)

	log.Printf("[INFO] Granted remediation lease %s to node %s for %s (expires: %s)",
		leaseID, req.NodeName, req.RemediationType, lease.ExpiresAt.Format(time.RFC3339))

	response := LeaseResponse{
		LeaseID:   leaseID,
		Approved:  true,
		ExpiresAt: lease.ExpiresAt,
		Message:   "Lease granted",
	}

	s.writeSuccess(w, http.StatusOK, response)
}

func (s *Server) handleLeaseDetail(w http.ResponseWriter, r *http.Request) {
	// Extract lease ID from path: /api/v1/leases/{id}
	leaseID := r.URL.Path[len("/api/v1/leases/"):]
	if leaseID == "" {
		s.writeError(w, http.StatusBadRequest, "MISSING_LEASE_ID", "Lease ID is required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.getLease(w, leaseID)
	case http.MethodDelete:
		s.releaseLease(w, leaseID)
	default:
		s.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET and DELETE are allowed")
	}
}

func (s *Server) getLease(w http.ResponseWriter, leaseID string) {
	s.mu.RLock()
	lease, exists := s.leases[leaseID]
	s.mu.RUnlock()

	if !exists {
		s.writeError(w, http.StatusNotFound, "LEASE_NOT_FOUND", "Lease not found")
		return
	}

	s.writeSuccess(w, http.StatusOK, lease)
}

func (s *Server) releaseLease(w http.ResponseWriter, leaseID string) {
	s.mu.Lock()

	lease, exists := s.leases[leaseID]
	if !exists {
		s.mu.Unlock()
		s.writeError(w, http.StatusNotFound, "LEASE_NOT_FOUND", "Lease not found")
		return
	}

	lease.Status = "completed"
	storage := s.storage
	s.mu.Unlock()

	// Persist status change to storage if available
	if storage != nil {
		if err := storage.UpdateLeaseStatus(context.Background(), leaseID, "completed"); err != nil {
			log.Printf("[WARN] Failed to update lease status in storage: %v", err)
		}
	}

	log.Printf("[INFO] Released lease %s for node %s", leaseID, lease.NodeName)

	s.writeSuccess(w, http.StatusOK, map[string]interface{}{
		"released": true,
		"leaseId":  leaseID,
	})
}

// =====================
// Metrics Endpoint
// =====================

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	// Update metrics before serving
	s.updateMetrics()

	// Serve using Prometheus handler
	s.metrics.Handler().ServeHTTP(w, r)
}

// updateMetrics refreshes all metrics from current state
func (s *Server) updateMetrics() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Build cluster status and update metrics
	status := s.buildClusterStatus()
	s.metrics.UpdateClusterMetrics(status)

	// Update lease metrics
	activeLeaseCount := 0
	for _, lease := range s.leases {
		if lease.Status == "active" && time.Now().Before(lease.ExpiresAt) {
			activeLeaseCount++
		}
	}
	s.metrics.UpdateLeaseMetrics(activeLeaseCount)

	// Update problem metrics
	problemCounts := make(map[string]map[string]int)
	for _, report := range s.nodeReports {
		for _, problem := range report.ActiveProblems {
			if _, exists := problemCounts[problem.Type]; !exists {
				problemCounts[problem.Type] = make(map[string]int)
			}
			problemCounts[problem.Type][problem.Severity]++
		}
	}
	s.metrics.UpdateProblemMetrics(problemCounts)
}

// =====================
// Response Helpers
// =====================

func (s *Server) writeSuccess(w http.ResponseWriter, status int, data interface{}) {
	response := APIResponse{
		Success:   true,
		Data:      data,
		Timestamp: time.Now(),
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(response)
}

func (s *Server) writeError(w http.ResponseWriter, status int, code, message string) {
	response := APIResponse{
		Success: false,
		Error: &APIError{
			Code:    code,
			Message: message,
		},
		Timestamp: time.Now(),
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(response)
}

// IsReady returns whether the server is ready to accept requests
func (s *Server) IsReady() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ready
}

// SetReady sets the readiness state
func (s *Server) SetReady(ready bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ready = ready
}

// SetStorage sets the storage backend for the server
func (s *Server) SetStorage(storage Storage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.storage = storage
}

// GetStorage returns the storage backend
func (s *Server) GetStorage() Storage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.storage
}
