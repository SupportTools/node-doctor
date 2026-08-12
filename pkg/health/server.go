// Package health provides HTTP health check endpoints for Kubernetes probes.
// This is a lightweight standalone health server that doesn't require webhook configuration.
package health

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// Server provides HTTP health check endpoints for Kubernetes probes.
type Server struct {
	config             *Config
	httpServer         *http.Server
	tcpListener        net.Listener
	unixListener       net.Listener
	socketPath         string
	mu                 sync.RWMutex
	started            bool
	healthy            bool
	ready              bool
	lastStatus         *types.Status
	lastUpdate         time.Time
	startTime          time.Time
	healthChecks       []HealthCheck
	readinessChecks    []HealthCheck
	dependencies       map[string]string // dependency name -> error text, only once past the failure threshold
	dependencyFailures map[string]int    // dependency name -> consecutive failure count
	remediationHistory RemediationHistoryProvider
}

// Config contains configuration for the health server.
type Config struct {
	// Enabled controls whether the health server is running
	Enabled bool

	// BindAddress is the address to bind to (default: "::" for dual-stack,
	// which accepts both IPv4 and IPv6 on Linux when net.ipv6.bindv6only=0).
	// Falls back to "0.0.0.0" when the IPv6/dual-stack bind fails.
	BindAddress string

	// Port is the port to listen on (default: 8080)
	Port int

	// SocketPath, when non-empty, makes the server ALSO serve the same health
	// endpoints on a per-pod unix domain socket. This is the reliable channel for
	// Kubernetes probes: because node-doctor runs with hostNetwork:true, the TCP
	// Port can be squatted by a foreign host process (e.g. an IP-float/VIP daemon
	// on hostPort 8080), which makes an httpGet probe fail with a 404 from the
	// squatter and crashloop the pod. A unix socket lives in the pod's own
	// (empty-dir) filesystem and cannot collide, so the exec `-healthcheck` probe
	// always reaches node-doctor's real health server. The TCP listener is still
	// served best-effort for external monitoring/load-balancers.
	SocketPath string

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

// isDualStackHost reports whether host is a dual-stack/IPv6 wildcard bind that
// may fail on nodes where IPv6 is disabled. Covers the empty host, the IPv6
// unspecified address "::", and any other IPv6 literal.
func isDualStackHost(host string) bool {
	if host == "" || host == "::" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.To4() == nil
}

// listenWithFallback opens a TCP listener on host:port using net.JoinHostPort
// for correct IPv6 bracketing. When the host is a dual-stack/IPv6 wildcard and
// the bind fails (typically because IPv6 is disabled on the node), it logs a
// warning and retries on the IPv4 wildcard "0.0.0.0".
func listenWithFallback(host string, port int) (net.Listener, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	ln, err := net.Listen("tcp", addr)
	if err == nil {
		return ln, nil
	}
	if isDualStackHost(host) {
		fallbackAddr := net.JoinHostPort("0.0.0.0", strconv.Itoa(port))
		log.Printf("[WARN] failed to bind %s (%v); falling back to IPv4 %s", addr, err, fallbackAddr)
		fln, ferr := net.Listen("tcp", fallbackAddr)
		if ferr != nil {
			return nil, fmt.Errorf("bind failed on %s (%v) and IPv4 fallback %s (%w)", addr, err, fallbackAddr, ferr)
		}
		return fln, nil
	}
	return nil, fmt.Errorf("failed to bind %s: %w", addr, err)
}

// NewServer creates a new health server with the given configuration.
func NewServer(config *Config) (*Server, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	// Apply defaults
	if config.BindAddress == "" {
		// "::" binds dual-stack (both IPv4 and IPv6) on Linux when
		// net.ipv6.bindv6only=0; Start() falls back to "0.0.0.0" if IPv6
		// is disabled on the node.
		config.BindAddress = "::"
	}
	// Port 0 is intentionally allowed — net.Listen("tcp", "host:0") lets the OS
	// pick a free port atomically. Tests use Port: 0 to avoid TOCTOU port-grab races.
	// Production deployments should set an explicit port in their Config.
	if config.ReadTimeout == 0 {
		config.ReadTimeout = 5 * time.Second
	}
	if config.WriteTimeout == 0 {
		config.WriteTimeout = 10 * time.Second
	}

	server := &Server{
		config:             config,
		socketPath:         config.SocketPath,
		started:            false,
		healthy:            true,
		ready:              false,
		startTime:          time.Now(),
		healthChecks:       make([]HealthCheck, 0),
		readinessChecks:    make([]HealthCheck, 0),
		dependencies:       make(map[string]string),
		dependencyFailures: make(map[string]int),
	}

	return server, nil
}

// listenUnix creates a unix domain socket listener at path, ensuring the parent
// directory exists and removing any stale socket left by a previous (crashed)
// process. The socket is chmod 0660 so co-located sidecars/probes in the same
// pod can reach it while it stays off the network entirely.
func listenUnix(path string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create socket dir: %w", err)
	}
	// A leftover socket file from a prior crash would make Listen fail with
	// "address already in use"; remove it first (it is a per-pod path we own).
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("remove stale socket %s: %w", path, err)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(path, 0o660)
	return ln, nil
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

	// A single *http.Server can Serve() multiple listeners concurrently; Shutdown
	// closes all of them. We serve on two:
	//   1. TCP (best-effort) — for external monitoring/load-balancers. On a
	//      hostNetwork node this can fail if a foreign process owns the port; that
	//      is NON-FATAL (we log and continue) because it must not crashloop the pod.
	//   2. Unix socket (reliable) — the per-pod channel the exec `-healthcheck`
	//      probe uses. This is the one that must come up for the pod to be usable.
	s.httpServer = &http.Server{
		Handler:      mux,
		ReadTimeout:  s.config.ReadTimeout,
		WriteTimeout: s.config.WriteTimeout,
	}

	var served bool

	// 1. TCP listener (best-effort). listenWithFallback uses net.JoinHostPort
	// (correct IPv6 bracketing) and retries on "0.0.0.0" when a dual-stack bind fails.
	if tcpLn, err := listenWithFallback(s.config.BindAddress, s.config.Port); err != nil {
		// Non-fatal: a foreign process squatting on the hostPort (common cause:
		// address already in use) must not take node-doctor down. Probes use the
		// unix socket; only external HTTP health on this node is unavailable.
		log.Printf("[WARN] health server TCP bind on port %d failed (%v); continuing without external HTTP health on this node — Kubernetes probes use the unix socket", s.config.Port, err)
	} else {
		s.tcpListener = tcpLn
		// Record the bound TCP address. http.Server.Serve ignores Addr, but callers
		// (and tests) read it to discover the actual host:port, especially with an
		// ephemeral Port: 0.
		s.httpServer.Addr = tcpLn.Addr().String()
		go func() {
			log.Printf("[INFO] Health server (TCP) serving on %s", tcpLn.Addr())
			if err := s.httpServer.Serve(tcpLn); err != nil && err != http.ErrServerClosed {
				log.Printf("[ERROR] Health server (TCP) failed: %v", err)
			}
		}()
		served = true
	}

	// 2. Unix socket listener (reliable, per-pod). Used by exec probes.
	if s.socketPath != "" {
		if unixLn, err := listenUnix(s.socketPath); err != nil {
			log.Printf("[WARN] health server unix socket bind at %s failed: %v", s.socketPath, err)
		} else {
			s.unixListener = unixLn
			go func() {
				log.Printf("[INFO] Health server (unix) serving on %s", s.socketPath)
				if err := s.httpServer.Serve(unixLn); err != nil && err != http.ErrServerClosed {
					log.Printf("[ERROR] Health server (unix) failed: %v", err)
				}
			}()
			served = true
		}
	}

	if !served {
		return fmt.Errorf("health server failed to bind any listener (tcp port %d and unix socket %q both failed)", s.config.Port, s.socketPath)
	}

	s.started = true
	log.Printf("[INFO] Health server started successfully (tcp=%v unix=%v)", s.tcpListener != nil, s.unixListener != nil)

	return nil
}

// Stop stops the health server gracefully (implements ExporterLifecycle).
//
// IMPORTANT: the mutex is released before calling httpServer.Shutdown so that
// in-flight HTTP handlers can still acquire s.mu.RLock. Holding Lock() across
// Shutdown() would deadlock: Shutdown() waits for handlers to return, but any
// handler that tries RLock() after Stop() acquires Lock() will block forever.
func (s *Server) Stop() error {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return nil
	}
	s.started = false
	httpServer := s.httpServer
	s.mu.Unlock() // release before Shutdown to avoid deadlock with in-flight handlers

	log.Printf("[INFO] Stopping health server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown health server: %w", err)
	}

	// Remove the unix socket file so a subsequent start doesn't trip over a stale
	// path (listenUnix also removes it, but clean up eagerly on graceful stop).
	if s.socketPath != "" {
		if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
			log.Printf("[WARN] failed to remove health socket %s: %v", s.socketPath, err)
		}
	}

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

// AddHealthCheck adds a custom LIVENESS check.
//
// LIVENESS vs READINESS — read this before adding a check here:
//
// /healthz (liveness) answers "is this process still functioning, or is it
// wedged and in need of a restart?". A failing liveness probe makes the kubelet
// KILL the container. Therefore a liveness check may ONLY inspect
// process-internal state (a deadlocked goroutine, an exhausted worker pool).
//
// It must NEVER depend on something outside the process — the API server,
// cluster DNS, a webhook endpoint. node-doctor runs on exactly the degraded
// nodes where those are broken; wiring a downstream dependency into liveness
// converts "the node is unhealthy" into "restart node-doctor forever", which is
// the crashloop class that #node-doctor-246 exists to prevent.
//
// For downstream dependencies use AddReadinessCheck or SetDependencyStatus
// instead: those make the pod NotReady (traffic/roll-out gating) without ever
// restarting it.
func (s *Server) AddHealthCheck(name string, check func() error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.healthChecks = append(s.healthChecks, HealthCheck{Name: name, Check: check})
}

// AddReadinessCheck adds a custom READINESS check.
//
// Readiness answers "can this agent currently do its job?". A failing readiness
// check marks the pod NotReady but never restarts it, which is the correct
// response to a broken downstream (API server unreachable, exporter failing).
// See AddHealthCheck for the liveness counterpart and why the two must not be
// conflated.
func (s *Server) AddReadinessCheck(name string, check func() error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.readinessChecks = append(s.readinessChecks, HealthCheck{Name: name, Check: check})
}

// dependencyFailureThreshold is the number of CONSECUTIVE failed reports a
// downstream dependency must accumulate before it is allowed to make the pod
// NotReady.
//
// Hysteresis matters here because node-doctor is a DaemonSet: a single
// transient export error flipping every pod to NotReady would stall rolling
// updates fleet-wide for a blip that has already healed. One success resets the
// counter, so a genuinely broken downstream still trips within a few cycles
// (and the kubelet's own readiness failureThreshold adds a second layer).
const dependencyFailureThreshold = 3

// SetDependencyStatus records the latest outcome for a named downstream
// dependency. A nil err marks it healthy and immediately clears any accumulated
// failures; a non-nil err increments its consecutive-failure count, and once
// that reaches dependencyFailureThreshold the dependency makes /ready return
// 503 until it recovers.
//
// This affects READINESS ONLY. /healthz deliberately ignores dependency state
// entirely — a downstream failure must never restart the process. See
// AddHealthCheck.
func (s *Server) SetDependencyStatus(name string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.dependencies == nil {
		s.dependencies = make(map[string]string)
	}
	if s.dependencyFailures == nil {
		s.dependencyFailures = make(map[string]int)
	}

	if err == nil {
		delete(s.dependencies, name)
		delete(s.dependencyFailures, name)
		return
	}

	s.dependencyFailures[name]++
	if s.dependencyFailures[name] >= dependencyFailureThreshold {
		s.dependencies[name] = err.Error()
	}
}

// failingDependencies returns the sorted names of dependencies that have
// exceeded the consecutive-failure threshold. Caller must hold at least a read
// lock.
func (s *Server) failingDependencies() []string {
	if len(s.dependencies) == 0 {
		return nil
	}
	names := make([]string, 0, len(s.dependencies))
	for name := range s.dependencies {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// SetRemediationHistory sets the remediation history provider for the /remediation/history endpoint.
func (s *Server) SetRemediationHistory(provider RemediationHistoryProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.remediationHistory = provider
}

// handleHealthz handles the /healthz endpoint (LIVENESS probe).
//
// Liveness == "the process is alive and not wedged". A 503 here causes the
// kubelet to KILL and restart the container, so this handler deliberately
// consults ONLY process-internal state:
//
//   - s.healthy, set explicitly via SetHealthy
//   - s.healthChecks, which are documented as process-internal only
//
// It must NOT consult s.ready, s.readinessChecks or s.dependencies. Downstream
// failures (API server unreachable, exporter erroring) belong to /ready: they
// make the pod NotReady, never restart it. Restarting node-doctor because the
// node it is diagnosing is broken is the exact crashloop this split prevents
// (#node-doctor-246). TestLivenessIgnoresDownstreamFailures locks this in.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Run all LIVENESS checks
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
	_ = json.NewEncoder(w).Encode(response)
}

// handleReady handles the /ready endpoint (READINESS probe).
//
// Readiness == "this agent can currently do its job". Unlike /healthz, a 503
// here only marks the pod NotReady — it never restarts the container. That is
// the correct response to a downstream failure, so this handler DOES consult:
//
//   - s.ready: at least one monitor has produced a status
//   - s.readinessChecks: caller-registered "can I work?" predicates
//   - s.dependencies: per-exporter export outcomes reported by the detector
//
// Reached over the same per-pod unix socket as liveness via the
// `-healthcheck-ready` exec probe, so it is immune to a foreign host process
// squatting hostPort 8080 on a hostNetwork node.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	response := ReadinessResponse{
		Ready:     s.ready,
		Timestamp: time.Now(),
	}

	switch {
	case !s.ready:
		response.Message = "Not ready: monitors not yet initialized"
	default:
		// Downstream dependency failures reported by the detector.
		if failing := s.failingDependencies(); len(failing) > 0 {
			response.Ready = false
			response.Message = "Not ready: downstream dependency failure: " + strings.Join(failing, ", ")
			break
		}
		// Caller-registered readiness predicates.
		var failed []string
		for _, rc := range s.readinessChecks {
			if err := rc.Check(); err != nil {
				failed = append(failed, rc.Name+": "+err.Error())
			}
		}
		if len(failed) > 0 {
			response.Ready = false
			response.Message = "Not ready: " + strings.Join(failed, "; ")
			break
		}
		response.Message = "Ready"
	}

	if !response.Ready {
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
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
	_ = json.NewEncoder(w).Encode(response)
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
		_ = json.NewEncoder(w).Encode(map[string]string{
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
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
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
