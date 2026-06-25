package prometheus

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/supporttools/node-doctor/pkg/types"
)

// PrometheusExporter exports Node Doctor metrics to Prometheus
type PrometheusExporter struct {
	config         *types.PrometheusExporterConfig
	settings       *types.GlobalSettings
	nodeName       string
	registry       *prometheus.Registry
	metrics        *Metrics
	server         *http.Server
	startTime      time.Time
	activeProblems map[string]*types.Problem // key is problem ID for tracking active problems
	mu             sync.RWMutex
	started        bool

	// boundAddr is the actual host:port the HTTP server is listening on, captured
	// from the bound net.Listener after Start. When an ephemeral port (0) is
	// requested, this is the only place the real, kernel-assigned port can be
	// read. It is guarded by mu and exposed via BoundAddr/BoundPort.
	boundAddr string

	// ephemeral, when true, suppresses the production Port==0 -> 9100 default so
	// the server binds an OS-assigned ephemeral port (bind :0). This is a test
	// seam (see newEphemeralExporter) that lets tests bind hermetically and read
	// the real port back via BoundPort, eliminating the freePort close-then-rebind
	// TOCTOU race.
	ephemeral bool

	// consecutiveFailures tracks the running count of failed exports since the
	// last successful export. It backs the ExporterConsecutiveFailures gauge and
	// is guarded by mu to avoid racy read-modify-write on the gauge itself.
	consecutiveFailures int
}

// NewPrometheusExporter creates a new Prometheus exporter with the given configuration
func NewPrometheusExporter(config *types.PrometheusExporterConfig, settings *types.GlobalSettings) (*PrometheusExporter, error) {
	return newPrometheusExporter(config, settings, false)
}

// newEphemeralExporter is a test-only constructor that builds an exporter which
// binds an OS-assigned ephemeral port (bind :0) instead of applying the
// production Port==0 -> 9100 default. After Start, the real bound port is
// available via BoundPort/BoundAddr. This makes the exporter test path hermetic:
// the port is bound exactly once, with no close-then-rebind window for another
// process to grab it.
func newEphemeralExporter(settings *types.GlobalSettings) (*PrometheusExporter, error) {
	config := &types.PrometheusExporterConfig{
		Enabled:   true,
		Port:      0, // ephemeral: bound by the OS, read back via BoundPort
		Path:      "/metrics",
		Namespace: "test",
	}
	return newPrometheusExporter(config, settings, true)
}

// newPrometheusExporter is the shared constructor. When ephemeral is true the
// Port==0 -> 9100 production default is skipped so the server binds an OS-assigned
// ephemeral port.
func newPrometheusExporter(config *types.PrometheusExporterConfig, settings *types.GlobalSettings, ephemeral bool) (*PrometheusExporter, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}
	if settings == nil {
		return nil, fmt.Errorf("settings cannot be nil")
	}
	if !config.Enabled {
		return nil, fmt.Errorf("Prometheus exporter is disabled")
	}

	// Validate configuration
	if err := validateConfig(config); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	// Validate node name
	if settings.NodeName == "" {
		return nil, fmt.Errorf("node name is required")
	}

	// Set defaults. In ephemeral mode, leave Port at 0 so the OS assigns a free
	// port at bind time (the real port is read back via BoundPort after Start);
	// otherwise apply the production default of 9100.
	if config.Port == 0 && !ephemeral {
		config.Port = 9100
	}
	if config.BindAddress == "" {
		config.BindAddress = types.DefaultHTTPBindAddress
	}
	if config.Path == "" {
		config.Path = "/metrics"
	}
	if config.Namespace == "" {
		config.Namespace = "node_doctor"
	}

	// Create constant labels
	constLabels := make(prometheus.Labels)
	for k, v := range config.Labels {
		constLabels[k] = v
	}

	// Create registry
	registry := NewRegistry(constLabels)

	// Create metrics
	metrics, err := NewMetrics(config.Namespace, config.Subsystem, constLabels)
	if err != nil {
		return nil, fmt.Errorf("failed to create metrics: %w", err)
	}

	// Register metrics
	if err := metrics.Register(registry); err != nil {
		return nil, fmt.Errorf("failed to register metrics: %w", err)
	}

	exporter := &PrometheusExporter{
		config:         config,
		settings:       settings,
		nodeName:       settings.NodeName,
		registry:       registry,
		metrics:        metrics,
		startTime:      time.Now(),
		activeProblems: make(map[string]*types.Problem),
		ephemeral:      ephemeral,
	}

	log.Printf("[INFO] Created Prometheus exporter on port %d with namespace '%s'",
		config.Port, config.Namespace)

	return exporter, nil
}

// Start initializes the Prometheus exporter and starts the HTTP server
func (e *PrometheusExporter) Start(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.started {
		return fmt.Errorf("Prometheus exporter already started")
	}

	log.Printf("[INFO] Starting Prometheus exporter...")

	// Initialize static metrics
	e.initializeStaticMetrics()

	// Start HTTP server. Binds to the configured BindAddress ("::" by default
	// for dual-stack), with graceful IPv4 fallback handled by startHTTPServer.
	server, err := startHTTPServer(ctx, e.config.BindAddress, e.config.Port, e.config.Path, e.registry)
	if err != nil {
		return fmt.Errorf("failed to start HTTP server: %w", err)
	}

	e.server = server
	// Capture the actual bound address from the listener (server.Addr is set to
	// ln.Addr().String() by startHTTPServer). This is set synchronously before
	// Start returns and is the authoritative source for the real port, which
	// matters when an ephemeral port (0) was requested.
	e.boundAddr = server.Addr
	e.started = true

	log.Printf("[INFO] Prometheus exporter started successfully on %s%s",
		server.Addr, e.config.Path)

	return nil
}

// BoundAddr returns the actual host:port the exporter's HTTP server is listening
// on, as captured from the bound listener after Start. It returns "" before Start
// has succeeded. When an ephemeral port (0) was requested, this reports the real,
// kernel-assigned port. Safe for concurrent use.
func (e *PrometheusExporter) BoundAddr() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.boundAddr
}

// BoundPort returns the actual port the exporter's HTTP server is listening on,
// extracted from BoundAddr. It returns 0 before Start has succeeded or if the
// bound address cannot be parsed. Safe for concurrent use.
func (e *PrometheusExporter) BoundPort() int {
	addr := e.BoundAddr()
	if addr == "" {
		return 0
	}
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return 0
	}
	return port
}

// Stop gracefully stops the Prometheus exporter
func (e *PrometheusExporter) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.started {
		return nil // Already stopped or never started
	}

	log.Printf("[INFO] Stopping Prometheus exporter...")

	// Stop HTTP server
	if err := shutdownServer(e.server, 30*time.Second); err != nil {
		log.Printf("[WARN] Error stopping HTTP server: %v", err)
	}

	e.started = false
	log.Printf("[INFO] Prometheus exporter stopped")

	return nil
}

// ExportStatus implements types.Exporter interface for status exports
func (e *PrometheusExporter) ExportStatus(ctx context.Context, status *types.Status) error {
	if status == nil {
		return fmt.Errorf("status cannot be nil")
	}

	e.mu.RLock()
	started := e.started
	e.mu.RUnlock()

	if !started {
		return fmt.Errorf("Prometheus exporter not started")
	}

	// Validate status
	if err := status.Validate(); err != nil {
		e.metrics.ExportErrorsTotal.WithLabelValues(
			e.nodeName, "prometheus", "validation").Inc()
		e.mu.Lock()
		e.recordExportHealth(false)
		e.mu.Unlock()
		return fmt.Errorf("status validation failed: %w", err)
	}

	// One ExportStatus call corresponds to one completed monitor check cycle:
	// the monitor ran its check and emitted a status, which the detector forwards
	// here. Time the cycle and record self-metrics at the end via RecordMonitorCycle.
	cycleStart := time.Now()
	cycleHadError := statusHasError(status)

	timer := prometheus.NewTimer(e.metrics.ExportDuration.WithLabelValues(
		e.nodeName, "prometheus", "status"))
	defer timer.ObserveDuration()

	// Update status metrics
	e.metrics.StatusUpdatesTotal.WithLabelValues(
		e.nodeName, status.Source).Inc()

	// Update conditions
	for _, condition := range status.Conditions {
		e.metrics.ConditionsTotal.WithLabelValues(
			e.nodeName, condition.Type, string(condition.Status)).Inc()

		// Set gauge: 1 when condition is True, 0 when False/Unknown
		val := 0.0
		if condition.Status == types.ConditionTrue {
			val = 1.0
		}
		e.metrics.ConditionStatus.WithLabelValues(
			e.nodeName, condition.Type).Set(val)
	}

	// Update events
	for _, event := range status.Events {
		e.metrics.EventsTotal.WithLabelValues(
			e.nodeName, status.Source, string(event.Severity)).Inc()
	}

	// Update uptime metric (calculated from start time)
	uptime := time.Since(e.startTime).Seconds()
	e.metrics.UptimeSeconds.WithLabelValues(e.nodeName).Set(uptime)

	// Extract and record latency metrics from status metadata
	e.recordLatencyMetrics(status)

	// Record successful export
	e.metrics.ExportOperationsTotal.WithLabelValues(
		e.nodeName, "prometheus", "status", "success").Inc()
	e.mu.Lock()
	e.recordExportHealth(true)
	e.mu.Unlock()

	// Record monitor-cycle self-metrics. status.Source is the monitor name.
	// A status carrying any ConditionFalse is treated as a failed cycle.
	var cycleErr error
	if cycleHadError {
		cycleErr = fmt.Errorf("monitor %s reported an unhealthy condition", status.Source)
	}
	e.RecordMonitorCycle(status.Source, time.Since(cycleStart), cycleErr)

	log.Printf("[DEBUG] Exported status from %s to Prometheus", status.Source)

	return nil
}

// statusHasError reports whether a status carries any condition signalling an
// unhealthy/failed monitor cycle (ConditionFalse). Conditions that are True or
// Unknown (e.g. the synthetic MonitorBlocked condition) do not count as errors.
func statusHasError(status *types.Status) bool {
	for _, cond := range status.Conditions {
		if cond.Status == types.ConditionFalse {
			return true
		}
	}
	return false
}

// RecordMonitorCycle records self-metrics for one completed monitor check cycle:
//   - increments MonitorCyclesTotal with result="success" or result="error"
//   - observes the cycle duration into MonitorCheckDuration
//   - sets MonitorCycleLastTimestamp to the current time (a "last run" heartbeat)
//
// monitorName is the name of the monitor (status.Source). A non-nil err marks
// the cycle as an error. This is the seam the detector's per-cycle path reaches
// via ExportStatus; it is also safe to call directly.
func (e *PrometheusExporter) RecordMonitorCycle(monitorName string, duration time.Duration, err error) {
	result := "success"
	if err != nil {
		result = "error"
	}

	e.metrics.MonitorCyclesTotal.WithLabelValues(e.nodeName, monitorName, result).Inc()
	e.metrics.MonitorCheckDuration.WithLabelValues(e.nodeName, monitorName).Observe(duration.Seconds())
	e.metrics.MonitorCycleLastTimestamp.WithLabelValues(e.nodeName, monitorName).Set(float64(time.Now().Unix()))
}

// RecordConfigReload records self-metrics for one completed config hot-reload
// attempt:
//   - increments ConfigReloadsTotal with result="success" or result="failure"
//   - sets ConfigReloadLastSuccess to 1 (success) or 0 (failure)
//   - sets ConfigReloadLastTimestamp to the current time (last-attempt heartbeat,
//     updated on both success and failure)
//   - observes the reload duration into ConfigReloadDuration
//
// It implements the reload.ReloadMetricsRecorder signature so the reload
// coordinator can push reload outcomes here via an injected closure without
// importing this package. Safe to call directly and concurrently (the
// underlying prometheus vecs are goroutine-safe).
func (e *PrometheusExporter) RecordConfigReload(success bool, duration time.Duration) {
	result := "failure"
	lastSuccess := 0.0
	if success {
		result = "success"
		lastSuccess = 1.0
	}

	e.metrics.ConfigReloadsTotal.WithLabelValues(e.nodeName, result).Inc()
	e.metrics.ConfigReloadLastSuccess.WithLabelValues(e.nodeName).Set(lastSuccess)
	e.metrics.ConfigReloadLastTimestamp.WithLabelValues(e.nodeName).Set(float64(time.Now().Unix()))
	e.metrics.ConfigReloadDuration.WithLabelValues(e.nodeName).Observe(duration.Seconds())
}

// recordExportHealth updates the exporter-health self-metrics for the
// "prometheus" exporter after an export attempt:
//   - on success: ExporterHealthy=1, ExporterLastSuccessTimestamp=now, and the
//     consecutive-failure counter is reset to 0 (ExporterConsecutiveFailures=0).
//   - on failure: ExporterHealthy=0 and the consecutive-failure counter is
//     incremented (ExporterConsecutiveFailures=count).
//
// The running failure count is tracked in the exporter's consecutiveFailures
// field rather than via a racy gauge read-modify-write. The caller MUST hold
// e.mu (write lock) so the field update is safe.
func (e *PrometheusExporter) recordExportHealth(success bool) {
	const exporterLabel = "prometheus"

	if success {
		e.consecutiveFailures = 0
		e.metrics.ExporterHealthy.WithLabelValues(e.nodeName, exporterLabel).Set(1)
		e.metrics.ExporterLastSuccessTimestamp.WithLabelValues(e.nodeName, exporterLabel).Set(float64(time.Now().Unix()))
		e.metrics.ExporterConsecutiveFailures.WithLabelValues(e.nodeName, exporterLabel).Set(0)
		return
	}

	e.consecutiveFailures++
	e.metrics.ExporterHealthy.WithLabelValues(e.nodeName, exporterLabel).Set(0)
	e.metrics.ExporterConsecutiveFailures.WithLabelValues(e.nodeName, exporterLabel).Set(float64(e.consecutiveFailures))
}

// ObserveCircuitState sets the remediator circuit-breaker state gauge to the
// supplied value. It implements the remediators.CircuitStateObserver interface
// so the remediator registry can push state transitions here without importing
// this package. The state int uses the registry's encoding: 0=closed, 1=open,
// 2=half-open.
func (e *PrometheusExporter) ObserveCircuitState(state int) {
	e.metrics.RemediatorCircuitBreakerState.WithLabelValues(e.nodeName).Set(float64(state))
}

// recordLatencyMetrics extracts latency metrics from status metadata and records them
func (e *PrometheusExporter) recordLatencyMetrics(status *types.Status) {
	latencyMetrics := status.GetLatencyMetrics()
	if latencyMetrics == nil {
		return
	}

	// Record gateway latency metrics
	if latencyMetrics.Gateway != nil {
		gw := latencyMetrics.Gateway
		latencySeconds := gw.LatencyMs / 1000.0

		e.metrics.GatewayLatencySeconds.WithLabelValues(
			e.nodeName, gw.GatewayIP, familyLabel(gw.AddressFamily)).Set(latencySeconds)

		e.metrics.GatewayLatencyHistogram.WithLabelValues(
			e.nodeName, gw.GatewayIP).Observe(latencySeconds)
	}

	// Record peer latency metrics
	if len(latencyMetrics.Peers) > 0 {
		reachableCount := 0
		for _, peer := range latencyMetrics.Peers {
			latencySeconds := peer.LatencyMs / 1000.0
			avgLatencySeconds := peer.AvgLatencyMs / 1000.0

			family := familyLabel(peer.AddressFamily)

			e.metrics.PeerLatencySeconds.WithLabelValues(
				e.nodeName, peer.PeerNode, peer.PeerIP, family).Set(latencySeconds)

			e.metrics.PeerLatencyAvgSeconds.WithLabelValues(
				e.nodeName, peer.PeerNode, peer.PeerIP, family).Set(avgLatencySeconds)

			reachable := 0.0
			if peer.Reachable {
				reachable = 1.0
				reachableCount++
			}
			e.metrics.PeerReachable.WithLabelValues(
				e.nodeName, peer.PeerNode, peer.PeerIP, family).Set(reachable)

			e.metrics.PeerLatencyHistogram.WithLabelValues(
				e.nodeName, peer.PeerNode).Observe(latencySeconds)
		}

		e.metrics.PeersTotal.WithLabelValues(e.nodeName).Set(float64(len(latencyMetrics.Peers)))
		e.metrics.PeersReachableTotal.WithLabelValues(e.nodeName).Set(float64(reachableCount))
	}

	// Record DNS latency metrics
	for _, dns := range latencyMetrics.DNS {
		latencySeconds := dns.LatencyMs / 1000.0

		e.metrics.DNSLatencySeconds.WithLabelValues(
			e.nodeName, dns.DNSServer, dns.Domain, dns.RecordType, familyLabel(dns.AddressFamily)).Set(latencySeconds)

		e.metrics.DNSLatencyHistogram.WithLabelValues(
			e.nodeName, dns.DomainType).Observe(latencySeconds)
	}

	// Record per-nameserver composite and component health scores.
	// Reset clears all previously-recorded nameserver label combinations so that
	// gauges for removed nameservers do not persist across scrape cycles.
	e.metrics.DNSNameserverHealthScore.Reset()
	e.metrics.DNSNameserverSuccessScore.Reset()
	e.metrics.DNSNameserverLatencyScore.Reset()
	e.metrics.DNSNameserverErrorScore.Reset()
	e.metrics.DNSNameserverConsistencyScore.Reset()
	for _, hs := range latencyMetrics.NameserverHealthScores {
		if hs.Status != "insufficient_data" {
			e.metrics.DNSNameserverHealthScore.WithLabelValues(
				e.nodeName, hs.Nameserver).Set(hs.Score)
			e.metrics.DNSNameserverSuccessScore.WithLabelValues(
				e.nodeName, hs.Nameserver).Set(hs.SuccessScore)
			e.metrics.DNSNameserverLatencyScore.WithLabelValues(
				e.nodeName, hs.Nameserver).Set(hs.LatencyScore)
			e.metrics.DNSNameserverErrorScore.WithLabelValues(
				e.nodeName, hs.Nameserver).Set(hs.ErrorScore)
			e.metrics.DNSNameserverConsistencyScore.WithLabelValues(
				e.nodeName, hs.Nameserver).Set(hs.ConsistencyScore)
		}
	}

	// Record DNS predictive alerting metrics.
	// Reset first so that stale label combinations (removed nameservers, disabled feature)
	// don't persist across scrapes.
	e.metrics.DNSPredictedBreachSeconds.Reset()
	e.metrics.DNSPredictionConfidence.Reset()
	for _, pa := range latencyMetrics.DNSPredictiveAlerts {
		// Always record confidence so dashboards can observe regression quality
		// even when no breach is predicted within the prediction window.
		e.metrics.DNSPredictionConfidence.WithLabelValues(
			e.nodeName, pa.DomainType).Set(pa.Confidence)
		// Only set the breach-seconds gauge when a breach is actually predicted
		// within the prediction window. Omitting the Set (rather than writing -1)
		// avoids false-positive alert rules that test for small positive values.
		if pa.WillBreach {
			e.metrics.DNSPredictedBreachSeconds.WithLabelValues(
				e.nodeName, pa.DomainType).Set(pa.TimeToBreach)
		}
	}

	// Record API server latency metrics
	if latencyMetrics.APIServer != nil {
		latencySeconds := latencyMetrics.APIServer.LatencyMs / 1000.0

		e.metrics.APIServerLatencySeconds.WithLabelValues(e.nodeName).Set(latencySeconds)
		e.metrics.APIServerLatencyHistogram.WithLabelValues(e.nodeName).Observe(latencySeconds)
	}
}

// familyLabel normalizes an address-family string for use as a Prometheus
// label value. It returns "ipv4" or "ipv6" only when the input matches one of
// those exactly; any other value (including an empty string) maps to "unknown"
// so the address_family label is never emitted empty.
func familyLabel(s string) string {
	switch s {
	case "ipv4", "ipv6":
		return s
	default:
		return "unknown"
	}
}

// ExportProblem implements types.Exporter interface for problem exports
func (e *PrometheusExporter) ExportProblem(ctx context.Context, problem *types.Problem) error {
	if problem == nil {
		return fmt.Errorf("problem cannot be nil")
	}

	e.mu.Lock()
	started := e.started
	e.mu.Unlock()

	if !started {
		return fmt.Errorf("Prometheus exporter not started")
	}

	// Validate problem
	if err := problem.Validate(); err != nil {
		e.metrics.ExportErrorsTotal.WithLabelValues(
			e.nodeName, "prometheus", "validation").Inc()
		e.mu.Lock()
		e.recordExportHealth(false)
		e.mu.Unlock()
		return fmt.Errorf("problem validation failed: %w", err)
	}

	timer := prometheus.NewTimer(e.metrics.ExportDuration.WithLabelValues(
		e.nodeName, "prometheus", "problem"))
	defer timer.ObserveDuration()

	e.mu.Lock()
	defer e.mu.Unlock()

	// Generate problem ID for tracking
	problemID := fmt.Sprintf("%s-%s-%s", problem.Type, problem.Resource, string(problem.Severity))

	// Update problem counters - use a dummy source since Problem doesn't have Source field
	source := "unknown"
	e.metrics.ProblemsTotal.WithLabelValues(
		e.nodeName, problem.Type, string(problem.Severity), source).Inc()

	// For active problems tracking, we'll use a simple approach based on the problem itself
	// Since Problem type doesn't have Status field, we'll track all problems as active when reported
	e.activeProblems[problemID] = problem
	e.updateActiveProblemsGauge()

	// Record event - use severity from problem
	e.metrics.EventsTotal.WithLabelValues(
		e.nodeName, source, string(problem.Severity)).Inc()

	// Update uptime metric (calculated from start time)
	uptime := time.Since(e.startTime).Seconds()
	e.metrics.UptimeSeconds.WithLabelValues(e.nodeName).Set(uptime)

	// Record successful export. mu is already held here, so recordExportHealth
	// is called directly (it must not re-acquire the lock).
	e.metrics.ExportOperationsTotal.WithLabelValues(
		e.nodeName, "prometheus", "problem", "success").Inc()
	e.recordExportHealth(true)

	log.Printf("[DEBUG] Exported problem %s on %s to Prometheus", problem.Type, problem.Resource)

	return nil
}

// Reload implements types.ReloadableExporter interface for configuration reload
func (e *PrometheusExporter) Reload(config interface{}) error {
	// Type assert with safety check
	prometheusConfig, ok := config.(*types.PrometheusExporterConfig)
	if !ok {
		return fmt.Errorf("invalid config type for Prometheus exporter: expected *types.PrometheusExporterConfig, got %T", config)
	}

	// Validate before applying
	if prometheusConfig == nil {
		return fmt.Errorf("prometheus exporter config cannot be nil")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	log.Printf("[INFO] Reloading Prometheus exporter configuration")

	// Validate new configuration
	if err := validateConfig(prometheusConfig); err != nil {
		return fmt.Errorf("new configuration validation failed: %w", err)
	}

	oldConfig := e.config

	// Set defaults for new config. In ephemeral mode (test seam), leave Port at 0
	// so a restart binds a fresh OS-assigned port rather than the production 9100
	// default; the real port is read back via BoundPort.
	if prometheusConfig.Port == 0 && !e.ephemeral {
		prometheusConfig.Port = 9100
	}
	if prometheusConfig.BindAddress == "" {
		prometheusConfig.BindAddress = types.DefaultHTTPBindAddress
	}
	if prometheusConfig.Path == "" {
		prometheusConfig.Path = "/metrics"
	}
	if prometheusConfig.Namespace == "" {
		prometheusConfig.Namespace = "node_doctor"
	}

	// Check if server needs to be restarted due to significant changes
	if e.needsServerRestart(oldConfig, prometheusConfig) {
		log.Printf("[INFO] Restarting Prometheus server due to configuration changes")

		// Stop existing server if running
		if e.started && e.server != nil {
			if err := shutdownServer(e.server, 10*time.Second); err != nil {
				log.Printf("[WARN] Error stopping existing server: %v", err)
			}
		}

		// Check if metrics need to be recreated due to namespace/subsystem changes
		if e.needsMetricsRecreation(oldConfig, prometheusConfig) {
			log.Printf("[INFO] Recreating metrics due to namespace/subsystem changes")

			// Create new constant labels
			constLabels := make(prometheus.Labels)
			for k, v := range prometheusConfig.Labels {
				constLabels[k] = v
			}

			// Create new registry
			e.registry = NewRegistry(constLabels)

			// Create new metrics
			metrics, err := NewMetrics(prometheusConfig.Namespace, prometheusConfig.Subsystem, constLabels)
			if err != nil {
				return fmt.Errorf("failed to create new metrics: %w", err)
			}

			// Register new metrics
			if err := metrics.Register(e.registry); err != nil {
				return fmt.Errorf("failed to register new metrics: %w", err)
			}

			e.metrics = metrics

			// Re-initialize static metrics
			e.initializeStaticMetrics()

			// Reset active problems tracking
			e.activeProblems = make(map[string]*types.Problem)
		}

		// Start new server if exporter was running
		if e.started {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			server, err := startHTTPServer(ctx, prometheusConfig.BindAddress, prometheusConfig.Port, prometheusConfig.Path, e.registry)
			if err != nil {
				return fmt.Errorf("failed to start new HTTP server: %w", err)
			}
			e.server = server
			e.boundAddr = server.Addr

			log.Printf("[INFO] Prometheus server restarted on %s%s", server.Addr, prometheusConfig.Path)
		}
	}

	// Update configuration
	e.config = prometheusConfig

	log.Printf("[INFO] Successfully reloaded Prometheus exporter configuration")
	log.Printf("[DEBUG] Config changes: port %d->%d, path %s->%s, namespace %s->%s, subsystem %s->%s",
		oldConfig.Port, prometheusConfig.Port,
		oldConfig.Path, prometheusConfig.Path,
		oldConfig.Namespace, prometheusConfig.Namespace,
		oldConfig.Subsystem, prometheusConfig.Subsystem)

	return nil
}

// IsReloadable implements types.ReloadableExporter interface
func (e *PrometheusExporter) IsReloadable() bool {
	return true
}

// needsServerRestart determines if the HTTP server needs to be restarted
// based on configuration changes that affect server behavior.
func (e *PrometheusExporter) needsServerRestart(oldConfig, newConfig *types.PrometheusExporterConfig) bool {
	if oldConfig == nil {
		return true // First time configuration
	}

	// Check if port changed
	if oldConfig.Port != newConfig.Port {
		return true
	}

	// Check if bind address changed
	if oldConfig.BindAddress != newConfig.BindAddress {
		return true
	}

	// Check if path changed
	if oldConfig.Path != newConfig.Path {
		return true
	}

	// Check if metrics need recreation (which requires server restart)
	if e.needsMetricsRecreation(oldConfig, newConfig) {
		return true
	}

	return false
}

// needsMetricsRecreation determines if metrics need to be recreated
// based on configuration changes that affect metric names or labels.
func (e *PrometheusExporter) needsMetricsRecreation(oldConfig, newConfig *types.PrometheusExporterConfig) bool {
	if oldConfig == nil {
		return true // First time configuration
	}

	// Check if namespace changed
	if oldConfig.Namespace != newConfig.Namespace {
		return true
	}

	// Check if subsystem changed
	if oldConfig.Subsystem != newConfig.Subsystem {
		return true
	}

	// Check if labels changed
	if !labelsEqual(oldConfig.Labels, newConfig.Labels) {
		return true
	}

	return false
}

// labelsEqual compares two label maps for equality
func labelsEqual(old, new map[string]string) bool {
	if len(old) != len(new) {
		return false
	}

	for k, v := range old {
		if new[k] != v {
			return false
		}
	}

	return true
}

// validateConfig validates the Prometheus exporter configuration
func validateConfig(config *types.PrometheusExporterConfig) error {
	if config.Port < 0 || config.Port > 65535 {
		return fmt.Errorf("invalid port: %d", config.Port)
	}

	if config.Path != "" && config.Path[0] != '/' {
		return fmt.Errorf("path must start with '/'")
	}

	if config.Namespace != "" && !isValidMetricName(config.Namespace) {
		return fmt.Errorf("invalid namespace: %s", config.Namespace)
	}

	if config.Subsystem != "" && !isValidMetricName(config.Subsystem) {
		return fmt.Errorf("invalid subsystem: %s", config.Subsystem)
	}

	return nil
}

// isValidMetricName checks if a string is a valid Prometheus metric name component
func isValidMetricName(name string) bool {
	if len(name) == 0 {
		return false
	}

	for i, r := range name {
		if i == 0 {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || r == ':') {
				return false
			}
		} else {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == ':') {
				return false
			}
		}
	}

	return true
}

// initializeStaticMetrics sets up static metrics that don't change often
func (e *PrometheusExporter) initializeStaticMetrics() {
	// Set start time
	e.metrics.StartTimeSeconds.WithLabelValues(e.nodeName).Set(float64(e.startTime.Unix()))

	// Set version info (using runtime info as placeholder)
	e.metrics.Info.WithLabelValues(
		e.nodeName,
		"unknown",                        // version
		"unknown",                        // git_commit
		runtime.Version(),                // go_version
		e.startTime.Format(time.RFC3339), // build_time
	).Set(1)
}

// updateActiveProblemsGauge updates the active problems gauge based on current active problems
func (e *PrometheusExporter) updateActiveProblemsGauge() {
	// Reset all active problems gauges
	e.metrics.ProblemsActive.Reset()

	// Count active problems by type and severity
	counts := make(map[string]map[string]int)
	for _, problem := range e.activeProblems {
		problemType := problem.Type
		severity := string(problem.Severity)

		if counts[problemType] == nil {
			counts[problemType] = make(map[string]int)
		}
		counts[problemType][severity]++
	}

	// Set gauge values
	for problemType, severities := range counts {
		for severity, count := range severities {
			e.metrics.ProblemsActive.WithLabelValues(
				e.nodeName, problemType, severity).Set(float64(count))
		}
	}
}
