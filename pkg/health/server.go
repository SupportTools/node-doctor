// Package health provides HTTP health check endpoints for Kubernetes probes.
// This is a lightweight standalone health server that doesn't require webhook configuration.
package health

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// Server provides HTTP health check endpoints for Kubernetes probes.
type Server struct {
	config             *Config
	httpServer         *http.Server
	mu                 sync.RWMutex
	started            bool
	healthy            bool
	ready              bool
	lastStatus         *types.Status
	lastUpdate         time.Time
	startTime          time.Time
	healthChecks       []HealthCheck
	remediationHistory RemediationHistoryProvider
}

// Config contains configuration for the health server.
type Config struct {
	// Enabled controls whether the health server is running
	Enabled bool

	// BindAddress is the address to bind to (default: 0.0.0.0)
	BindAddress string

	// Port is the port to listen on (default: 8080)
	Port int

	// ReadTimeout for HTTP requests
	ReadTimeout time.Duration

	// WriteTimeout for HTTP responses
	WriteTimeout time.Duration
}

// HealthCheck represents a health check function.
type HealthCheck struct {
	Name  string
	Check func() error
}

// RemediationHistoryProvider provides access to remediation history.
// This interface allows the health server to access remediation history
// without creating a circular dependency with the remediators package.
//
// The implementation should return remediation records that can be marshaled to JSON.
// Each record should have fields: remediatorType, problem, startTime, endTime, duration, success, error.
type RemediationHistoryProvider interface {
	// GetHistory returns the most recent remediation records up to the specified limit.
	// If limit is 0 or negative, all history is returned.
	// Returns a slice of records that can be marshaled to JSON.
	GetHistory(limit int) interface{}
}

// HealthResponse represents the JSON response for /healthz endpoint.
type HealthResponse struct {
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
	Uptime    string    `json:"uptime"`
	Checks    []Check   `json:"checks,omitempty"`
}

// Check represents an individual health check result.
type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// ReadinessResponse represents the JSON response for /ready endpoint.
type ReadinessResponse struct {
	Ready     bool      `json:"ready"`
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message,omitempty"`
}

// StatusResponse represents the JSON response for /status endpoint.
type StatusResponse struct {
	Healthy       bool              `json:"healthy"`
	Ready         bool              `json:"ready"`
	Uptime        string            `json:"uptime"`
	LastUpdate    time.Time         `json:"lastUpdate"`
	MonitorStatus *types.Status     `json:"monitorStatus,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

// NewServer creates a new health server with the given configuration.
func NewServer(config *Config) (*Server, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	// Apply defaults
	if config.BindAddress == "" {
		config.BindAddress = "0.0.0.0"
	}
	if config.Port == 0 {
		config.Port = 8080
	}
	if config.ReadTimeout == 0 {
		config.ReadTimeout = 5 * time.Second
	}
	if config.WriteTimeout == 0 {
		config.WriteTimeout = 10 * time.Second
	}

	server := &Server{
		config:       config,
		started:      false,
		healthy:      true,
		ready:        false,
		startTime:    time.Now(),
		healthChecks: make([]HealthCheck, 0),
	}

	return server, nil
}

// Start starts the health server.
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return fmt.Errorf("health server already started")
	}

	// Create HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/ready", s.handleReady)
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/remediation/history", s.handleRemediationHistory)

	addr := fmt.Sprintf("%s:%d", s.config.BindAddress, s.config.Port)
	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  s.config.ReadTimeout,
		WriteTimeout: s.config.WriteTimeout,
	}

	// Start HTTP server in background
	go func() {
		log.Printf("[INFO] Starting health server on %s", addr)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[ERROR] Health server failed: %v", err)
		}
	}()

	// Wait a moment for server to start
	time.Sleep(100 * time.Millisecond)

	s.started = true
	log.Printf("[INFO] Health server started successfully on %s", addr)

	return nil
}

// Stop stops the health server gracefully (implements ExporterLifecycle).
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return nil
	}

	log.Printf("[INFO] Stopping health server...")

	// Shutdown HTTP server with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown health server: %w", err)
	}

	s.started = false
	log.Printf("[INFO] Health server stopped")

	return nil
}

// UpdateStatus updates the monitor status (called by monitor engine).
func (s *Server) UpdateStatus(status *types.Status) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lastStatus = status
	s.lastUpdate = time.Now()

	// Determine readiness based on monitor status
	// Ready = at least one monitor has run successfully
	s.ready = status != nil
}

// SetHealthy sets the overall health status.
func (s *Server) SetHealthy(healthy bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.healthy = healthy
}

// SetReady sets the readiness status.
func (s *Server) SetReady(ready bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ready = ready
}

// AddHealthCheck adds a custom health check.
func (s *Server) AddHealthCheck(name string, check func() error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.healthChecks = append(s.healthChecks, HealthCheck{Name: name, Check: check})
}

// SetRemediationHistory sets the remediation history provider for the /remediation/history endpoint.
func (s *Server) SetRemediationHistory(provider RemediationHistoryProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.remediationHistory = provider
}

// handleHealthz handles the /healthz endpoint (liveness probe).
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Run all health checks
	checks := make([]Check, 0, len(s.healthChecks))
	allHealthy := s.healthy

	for _, hc := range s.healthChecks {
		check := Check{Name: hc.Name, Status: "ok"}
		if err := hc.Check(); err != nil {
			check.Status = "failed"
			check.Error = err.Error()
			allHealthy = false
		}
		checks = append(checks, check)
	}

	// Calculate uptime
	uptime := time.Since(s.startTime).Round(time.Second).String()

	response := HealthResponse{
		Status:    "ok",
		Timestamp: time.Now(),
		Uptime:    uptime,
		Checks:    checks,
	}

	if !allHealthy {
		response.Status = "unhealthy"
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleReady handles the /ready endpoint (readiness probe).
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	response := ReadinessResponse{
		Ready:     s.ready,
		Timestamp: time.Now(),
	}

	if !s.ready {
		response.Message = "Not ready: monitors not yet initialized"
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		response.Message = "Ready"
		w.WriteHeader(http.StatusOK)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleStatus handles the /status endpoint (detailed status).
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Calculate uptime
	uptime := time.Since(s.startTime).Round(time.Second).String()

	response := StatusResponse{
		Healthy:       s.healthy,
		Ready:         s.ready,
		Uptime:        uptime,
		LastUpdate:    s.lastUpdate,
		MonitorStatus: s.lastStatus,
		Metadata: map[string]string{
			"version":    "v0.1.0",
			"started_at": s.startTime.Format(time.RFC3339),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// handleRemediationHistory handles the /remediation/history endpoint.
// Returns the remediation history from the registry.
//
// Query parameters:
//   - limit: Maximum number of records to return (default: 100, max: 1000)
func (s *Server) handleRemediationHistory(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	provider := s.remediationHistory
	s.mu.RUnlock()

	// Check if remediation history is available
	if provider == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Remediation history not available",
		})
		return
	}

	// Parse limit parameter
	limit := 100 // default
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil {
			limit = parsedLimit
			// Cap at 1000 to prevent excessive memory usage
			if limit > 1000 {
				limit = 1000
			} else if limit < 0 {
				limit = 0 // 0 means all records
			}
		}
	}

	// Get history from provider
	history := provider.GetHistory(limit)

	// Calculate count based on type
	count := 0
	if history != nil {
		// Use reflection to get length if it's a slice
		switch v := history.(type) {
		case []interface{}:
			count = len(v)
		case []map[string]interface{}:
			count = len(v)
		default:
			// Try to marshal and unmarshal to get accurate count
			// This handles any slice type
			if data, err := json.Marshal(history); err == nil {
				var arr []interface{}
				if err := json.Unmarshal(data, &arr); err == nil {
					count = len(arr)
				}
			}
		}
	}

	// Return as JSON
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"count":   count,
		"limit":   limit,
		"history": history,
	})
}

// Name returns the name of the health server.
func (s *Server) Name() string {
	return "health-server"
}

// ExportStatus updates the status for health endpoints (implements types.Exporter).
func (s *Server) ExportStatus(ctx context.Context, status *types.Status) error {
	// Update status for health endpoints
	s.UpdateStatus(status)
	return nil
}

// ExportProblem is a no-op for health server (implements types.Exporter).
func (s *Server) ExportProblem(ctx context.Context, problem *types.Problem) error {
	// Health server doesn't handle problems, just maintains overall health status
	return nil
}
