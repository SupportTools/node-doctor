package prometheus

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"runtime"
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
}

// NewPrometheusExporter creates a new Prometheus exporter with the given configuration
func NewPrometheusExporter(config *types.PrometheusExporterConfig, settings *types.GlobalSettings) (*PrometheusExporter, error) {
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

	// Set defaults
	if config.Port == 0 {
		config.Port = 9100
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

	// Start HTTP server
	addr := fmt.Sprintf("0.0.0.0:%d", e.config.Port)
	server, err := startHTTPServer(ctx, addr, e.config.Path, e.registry)
	if err != nil {
		return fmt.Errorf("failed to start HTTP server: %w", err)
	}

	e.server = server
	e.started = true

	log.Printf("[INFO] Prometheus exporter started successfully on %s%s",
		addr, e.config.Path)

	return nil
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
		return fmt.Errorf("status validation failed: %w", err)
	}

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

	log.Printf("[DEBUG] Exported status from %s to Prometheus", status.Source)

	return nil
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
			e.nodeName, gw.GatewayIP).Set(latencySeconds)

		e.metrics.GatewayLatencyHistogram.WithLabelValues(
			e.nodeName, gw.GatewayIP).Observe(latencySeconds)
	}

	// Record peer latency metrics
	if len(latencyMetrics.Peers) > 0 {
		reachableCount := 0
		for _, peer := range latencyMetrics.Peers {
			latencySeconds := peer.LatencyMs / 1000.0
			avgLatencySeconds := peer.AvgLatencyMs / 1000.0

			e.metrics.PeerLatencySeconds.WithLabelValues(
				e.nodeName, peer.PeerNode, peer.PeerIP).Set(latencySeconds)

			e.metrics.PeerLatencyAvgSeconds.WithLabelValues(
				e.nodeName, peer.PeerNode, peer.PeerIP).Set(avgLatencySeconds)

			reachable := 0.0
			if peer.Reachable {
				reachable = 1.0
				reachableCount++
			}
			e.metrics.PeerReachable.WithLabelValues(
				e.nodeName, peer.PeerNode, peer.PeerIP).Set(reachable)

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
			e.nodeName, dns.DNSServer, dns.Domain, dns.RecordType).Set(latencySeconds)

		e.metrics.DNSLatencyHistogram.WithLabelValues(
			e.nodeName, dns.DomainType).Observe(latencySeconds)
	}

	// Record API server latency metrics
	if latencyMetrics.APIServer != nil {
		latencySeconds := latencyMetrics.APIServer.LatencyMs / 1000.0

		e.metrics.APIServerLatencySeconds.WithLabelValues(e.nodeName).Set(latencySeconds)
		e.metrics.APIServerLatencyHistogram.WithLabelValues(e.nodeName).Observe(latencySeconds)
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

	// Record successful export
	e.metrics.ExportOperationsTotal.WithLabelValues(
		e.nodeName, "prometheus", "problem", "success").Inc()

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

	// Set defaults for new config
	if prometheusConfig.Port == 0 {
		prometheusConfig.Port = 9100
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

			addr := fmt.Sprintf("0.0.0.0:%d", prometheusConfig.Port)
			server, err := startHTTPServer(ctx, addr, prometheusConfig.Path, e.registry)
			if err != nil {
				return fmt.Errorf("failed to start new HTTP server: %w", err)
			}
			e.server = server

			log.Printf("[INFO] Prometheus server restarted on %s%s", addr, prometheusConfig.Path)
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
