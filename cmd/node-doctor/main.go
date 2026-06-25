// Node Doctor - Kubernetes node monitoring and auto-remediation tool
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/supporttools/node-doctor/pkg/detector"
	httpexporter "github.com/supporttools/node-doctor/pkg/exporters/http"
	kubernetesexporter "github.com/supporttools/node-doctor/pkg/exporters/kubernetes"
	prometheusexporter "github.com/supporttools/node-doctor/pkg/exporters/prometheus"
	"github.com/supporttools/node-doctor/pkg/health"
	"github.com/supporttools/node-doctor/pkg/logger"
	"github.com/supporttools/node-doctor/pkg/monitors"
	"github.com/supporttools/node-doctor/pkg/remediators"
	"github.com/supporttools/node-doctor/pkg/types"
	"github.com/supporttools/node-doctor/pkg/util"

	// Import monitors to register them
	_ "github.com/supporttools/node-doctor/pkg/monitors/custom"
	_ "github.com/supporttools/node-doctor/pkg/monitors/example"
	_ "github.com/supporttools/node-doctor/pkg/monitors/kubernetes"
	_ "github.com/supporttools/node-doctor/pkg/monitors/network"
	_ "github.com/supporttools/node-doctor/pkg/monitors/system"
)

// Build-time variables set by goreleaser or make
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildTime = "unknown"
)

// noopExporter is a no-op implementation of types.Exporter for when no real exporters are configured
type noopExporter struct{}

func (e *noopExporter) ExportStatus(ctx context.Context, status *types.Status) error {
	log.Printf("[DEBUG] NoopExporter: Status from %s with %d events, %d conditions",
		status.Source, len(status.Events), len(status.Conditions))
	return nil
}

func (e *noopExporter) ExportProblem(ctx context.Context, problem *types.Problem) error {
	log.Printf("[DEBUG] NoopExporter: Problem %s on %s (severity: %s): %s",
		problem.Type, problem.Resource, problem.Severity, problem.Message)
	return nil
}

func (e *noopExporter) Stop() error {
	// No-op exporter has no resources to clean up
	return nil
}

//nolint:gocyclo // top-level CLI wiring: flag parsing + subsystem init is naturally branchy
func main() {
	// Parse command line flags
	var (
		configFile      = flag.String("config", "", "Path to configuration file")
		version         = flag.Bool("version", false, "Show version information")
		validateConfig  = flag.Bool("validate-config", false, "Validate configuration and exit")
		dumpConfig      = flag.Bool("dump-config", false, "Dump effective configuration and exit")
		listMonitors    = flag.Bool("list-monitors", false, "List available monitor types and exit")
		debug           = flag.Bool("debug", false, "Enable debug logging")
		dryRun          = flag.Bool("dry-run", false, "Enable dry-run mode (no actual remediation)")
		logLevel        = flag.String("log-level", "", "Override log level (debug, info, warn, error)")
		logFormat       = flag.String("log-format", "", "Override log format (json, text)")
		enableProfiling = flag.Bool("enable-profiling", false, "Enable pprof profiling server")
		profilingPort   = flag.Int("profiling-port", 6060, "Port for pprof profiling server")
	)
	flag.Parse()

	if *version {
		fmt.Printf("Node Doctor %s\n", Version)
		fmt.Printf("Git Commit: %s\n", GitCommit)
		fmt.Printf("Build Time: %s\n", BuildTime)
		fmt.Printf("Go Version: %s\n", runtime.Version())
		fmt.Printf("OS/Arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
		return
	}

	if *listMonitors {
		fmt.Println("Available monitor types:")
		for _, monitorType := range monitors.GetRegisteredTypes() {
			info := monitors.GetMonitorInfo(monitorType)
			if info != nil {
				fmt.Printf("  %-20s - %s\n", monitorType, info.Description)
			} else {
				fmt.Printf("  %-20s - (no description)\n", monitorType)
			}
		}
		return
	}

	// Determine config file path
	if *configFile == "" {
		// Look for config in standard locations
		candidates := []string{
			"/etc/node-doctor/config.yaml",
			"/etc/node-doctor/config.yml",
			"./config.yaml",
			"./config.yml",
		}

		for _, candidate := range candidates {
			if _, err := os.Stat(candidate); err == nil {
				*configFile = candidate
				break
			}
		}

		if *configFile == "" {
			log.Fatal("No configuration file found. Use -config flag or place config.yaml in current directory or /etc/node-doctor/")
		}
	}

	log.Printf("[INFO] Starting Node Doctor %s (commit: %s, built: %s)", Version, GitCommit, BuildTime)
	log.Printf("[INFO] Loading configuration from: %s", *configFile)

	// Load and validate configuration
	config, err := util.LoadConfig(*configFile)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Apply default monitors for any missing monitor types
	addedDefaults := monitors.ApplyDefaultMonitors(config)
	if len(addedDefaults) > 0 {
		log.Printf("[INFO] Added default configurations for monitors: %v", addedDefaults)
	}

	// Apply command line overrides
	if *debug {
		config.Settings.LogLevel = "debug"
	}
	if *logLevel != "" {
		config.Settings.LogLevel = *logLevel
	}
	if *logFormat != "" {
		config.Settings.LogFormat = *logFormat
	}
	if *dryRun {
		config.Settings.DryRunMode = true
		config.Remediation.DryRun = true
	}
	if *enableProfiling {
		config.Features.EnableProfiling = true
		config.Features.ProfilingPort = *profilingPort
	}

	// Apply defaults and validate
	if err := config.ApplyDefaults(); err != nil {
		log.Fatalf("Failed to apply configuration defaults: %v", err)
	}

	if err := config.ValidateWithRegistry(monitors.DefaultRegistry); err != nil {
		log.Fatalf("Configuration validation failed: %v", err)
	}

	// Wire structured logging (slog) from the validated config. This installs the
	// configured format/level/destination and bridges the standard log package so
	// all subsequent log.Printf("[LEVEL] ...") calls below honor it. Errors here
	// (e.g. an unopenable log file) are non-fatal: warn and continue on defaults
	// rather than crash the daemon over logging setup.
	if err := logger.Init(config); err != nil {
		log.Printf("[WARN] Structured logging setup failed, continuing with default logging: %v", err)
	}

	if *validateConfig {
		log.Printf("[INFO] Configuration validation passed")
		return
	}

	if *dumpConfig {
		dumpConfiguration(config)
		return
	}

	// Structured startup banner via slog (logging is wired above).
	logger.L().Info("node doctor starting",
		"node", config.Settings.NodeName,
		"logLevel", config.Settings.LogLevel,
		"logFormat", config.Settings.LogFormat,
		"logOutput", config.Settings.LogOutput,
	)
	if config.Settings.DryRunMode {
		logger.L().Warn("running in dry-run mode; no actual remediation will be performed")
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Create remediator registry before exporters so the health server can serve
	// /remediation/history immediately on start (no race window).
	var remediatorRegistry *remediators.RemediatorRegistry
	if config.Remediation.Enabled {
		maxPerHour := config.Remediation.MaxRemediationsPerHour
		if maxPerHour == 0 {
			maxPerHour = 10 // sensible default
		}
		historySize := config.Remediation.HistorySize
		if historySize == 0 {
			historySize = 100
		}
		remediatorRegistry = remediators.NewRegistry(maxPerHour, historySize)
		remediatorRegistry.SetDryRun(config.Remediation.DryRun || config.Settings.DryRunMode)
		// Wire the per-minute token-bucket burst limit. A value of 0 leaves the
		// per-minute check disabled (only the per-hour window applies).
		remediatorRegistry.SetMaxRemediationsPerMinute(config.Remediation.MaxRemediationsPerMinute)
		log.Printf("[INFO] Remediator registry initialized (dry-run=%v, maxPerHour=%d, maxPerMinute=%d)",
			remediatorRegistry.IsDryRun(), maxPerHour, config.Remediation.MaxRemediationsPerMinute)

		// Wire the controller lease client when coordination is opted in.
		// The registry's Remediate path checks for a non-nil lease client and
		// performs RequestLease/ReleaseLease internally; nothing else to wire.
		// Failures here are non-fatal — coordination is opt-in.
		if err := wireLeaseClient(remediatorRegistry, config); err != nil {
			log.Printf("[WARN] Lease client not wired: %v (continuing without controller coordination)", err)
		}
	}

	// Create exporters (health server is wired with the history provider before Start).
	log.Printf("[INFO] Creating exporters...")
	var historyProvider health.RemediationHistoryProvider
	if remediatorRegistry != nil {
		historyProvider = &remediationHistoryAdapter{registry: remediatorRegistry}
	}
	exporters, exporterInterfaces, promExporter, err := createExporters(ctx, config, historyProvider)
	if err != nil {
		log.Fatalf("Failed to create exporters: %v", err)
	}

	log.Printf("[INFO] Created %d exporters", len(exporters))

	// Expose the remediator circuit-breaker state as a Prometheus gauge. Only wire
	// when both the registry (remediation enabled) and the Prometheus exporter are
	// present. SetCircuitStateObserver pushes the current state immediately and on
	// every subsequent transition.
	if remediatorRegistry != nil && promExporter != nil {
		remediatorRegistry.SetCircuitStateObserver(promExporter)
		log.Printf("[INFO] Remediator circuit-breaker state wired to Prometheus gauge")
	}

	// Create monitor factory for hot reload
	monitorFactory := &monitorFactoryAdapter{ctx: ctx}

	// Create the detector with configured exporters.
	// Monitors are created by the monitorFactory during detector.Start() — do not pre-create them here.
	log.Printf("[INFO] Creating detector...")

	det, err := detector.NewProblemDetector(config, nil, exporterInterfaces, *configFile, monitorFactory, monitors.DefaultRegistry)
	if err != nil {
		log.Fatalf("Failed to create detector: %v", err)
	}

	// Wire the registry to the detector (registry was already created above).
	if remediatorRegistry != nil {
		det.SetRemediatorRegistry(remediatorRegistry)
	}

	// Wire config hot-reload self-metrics. The detector owns the reload
	// coordinator but only sees exporters via types.Exporter; pass a closure over
	// the concrete Prometheus exporter's RecordConfigReload. Only wired when the
	// Prometheus exporter is present (nil otherwise).
	if promExporter != nil {
		det.SetReloadMetricsRecorder(promExporter.RecordConfigReload)
		log.Printf("[INFO] Config hot-reload self-metrics wired to Prometheus exporter")
	}

	// Start the detector
	log.Printf("[INFO] Starting detector...")
	if err := det.Start(); err != nil {
		log.Fatalf("Failed to start detector: %v", err)
	}

	// Guard against a silent no-op start: if every monitor failed to create/start,
	// the detector returns nil from Start() but is effectively useless.
	startStats := det.GetStatistics()
	if startStats.GetMonitorsStarted() == 0 && len(config.Monitors) > 0 {
		log.Fatalf("No monitors started successfully — check monitor configuration (all %d configured monitor(s) failed)", len(config.Monitors))
	}

	log.Printf("[INFO] Node Doctor started successfully")

	// Wait for shutdown signal
	<-sigCh
	log.Printf("[INFO] Received shutdown signal, stopping...")

	// Create shutdown context with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Stop detector (the Run method handles its own cleanup)
	log.Printf("[INFO] Stopping detector...")
	// Cancel the context to signal shutdown
	cancel()

	// Stop exporters
	log.Printf("[INFO] Stopping exporters...")
	for _, exporter := range exporters {
		if err := exporter.Stop(); err != nil {
			log.Printf("[WARN] Error stopping exporter: %v", err)
		}
	}

	// Wait for shutdown to complete or timeout
	select {
	case <-shutdownCtx.Done():
		log.Printf("[WARN] Shutdown timeout exceeded")
	default:
		log.Printf("[INFO] Node Doctor stopped successfully")
	}
}

// createMonitors creates and configures all monitors from the configuration
func createMonitors(ctx context.Context, monitorConfigs []types.MonitorConfig) ([]types.Monitor, error) {
	var monitorInstances []types.Monitor

	for _, config := range monitorConfigs {
		if !config.Enabled {
			log.Printf("[INFO] Monitor %s is disabled, skipping", config.Name)
			continue
		}

		log.Printf("[INFO] Creating monitor: %s (type: %s)", config.Name, config.Type)

		monitor, err := monitors.CreateMonitor(ctx, config)
		if err != nil {
			log.Printf("[ERROR] Failed to create monitor %s: %v", config.Name, err)
			continue
		}

		monitorInstances = append(monitorInstances, monitor)
		log.Printf("[INFO] Created monitor: %s", config.Name)
	}

	return monitorInstances, nil
}

// remediationHistoryAdapter adapts *remediators.RemediatorRegistry to health.RemediationHistoryProvider.
// The registry's GetHistory returns a concrete []RemediationRecord; the health interface requires
// interface{} so the server can marshal it without importing the remediators package.
type remediationHistoryAdapter struct {
	registry *remediators.RemediatorRegistry
}

func (a *remediationHistoryAdapter) GetHistory(limit int) interface{} {
	return a.registry.GetHistory(limit)
}

// createExporters creates and configures all exporters from the configuration.
// remediationProvider is optional; when non-nil it is wired to the health server
// before Start() so /remediation/history is available immediately on first request.
func createExporters(ctx context.Context, config *types.NodeDoctorConfig, remediationProvider health.RemediationHistoryProvider) ([]ExporterLifecycle, []types.Exporter, *prometheusexporter.PrometheusExporter, error) {
	var exporters []ExporterLifecycle
	var exporterInterfaces []types.Exporter
	// promExporterTyped keeps a typed reference to the Prometheus exporter (if one
	// is created and started) so the caller can wire it as a circuit-state observer.
	var promExporterTyped *prometheusexporter.PrometheusExporter

	// Create Kubernetes exporter if enabled
	if config.Exporters.Kubernetes != nil && config.Exporters.Kubernetes.Enabled {
		log.Printf("[INFO] Creating Kubernetes exporter...")
		k8sExporter, err := kubernetesexporter.NewKubernetesExporter(
			config.Exporters.Kubernetes,
			&config.Settings,
		)
		if err != nil {
			log.Printf("[WARN] Failed to create Kubernetes exporter: %v", err)
		} else {
			if err := k8sExporter.Start(ctx); err != nil {
				log.Printf("[WARN] Failed to start Kubernetes exporter: %v", err)
			} else {
				exporters = append(exporters, k8sExporter)
				exporterInterfaces = append(exporterInterfaces, k8sExporter)
				log.Printf("[INFO] Kubernetes exporter created and started")
			}
		}
	}

	// Create HTTP exporter if enabled
	if config.Exporters.HTTP != nil && config.Exporters.HTTP.Enabled {
		log.Printf("[INFO] Creating HTTP exporter...")
		httpExporter, err := httpexporter.NewHTTPExporter(
			config.Exporters.HTTP,
			&config.Settings,
		)
		if err != nil {
			log.Printf("[WARN] Failed to create HTTP exporter: %v", err)
		} else {
			if err := httpExporter.Start(ctx); err != nil {
				log.Printf("[WARN] Failed to start HTTP exporter: %v", err)
			} else {
				exporters = append(exporters, httpExporter)
				exporterInterfaces = append(exporterInterfaces, httpExporter)
				log.Printf("[INFO] HTTP exporter created and started")
			}
		}
	}

	// Create Health Server (always enabled for Kubernetes probes)
	log.Printf("[INFO] Creating health server...")
	healthServer, err := health.NewServer(&health.Config{
		Enabled: true,
		// "::" binds dual-stack (IPv4 + IPv6) with graceful fallback to
		// "0.0.0.0" when IPv6 is disabled on the node (handled in Start()).
		BindAddress:  "::",
		Port:         8080,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	})
	if err != nil {
		log.Printf("[WARN] Failed to create health server: %v", err)
	} else {
		// Wire the remediation history provider before Start so the endpoint is
		// available immediately when the listener opens (no race window).
		if remediationProvider != nil {
			healthServer.SetRemediationHistory(remediationProvider)
			log.Printf("[INFO] Remediation history wired to /remediation/history endpoint")
		}
		if err := healthServer.Start(ctx); err != nil {
			log.Printf("[WARN] Failed to start health server: %v", err)
		} else {
			exporters = append(exporters, healthServer)
			exporterInterfaces = append(exporterInterfaces, healthServer)
			log.Printf("[INFO] Health server created and started on port 8080")
		}
	}

	// Create Prometheus exporter if enabled
	if config.Exporters.Prometheus != nil && config.Exporters.Prometheus.Enabled {
		log.Printf("[INFO] Creating Prometheus exporter...")
		promExporter, err := prometheusexporter.NewPrometheusExporter(
			config.Exporters.Prometheus,
			&config.Settings,
		)
		if err != nil {
			log.Printf("[WARN] Failed to create Prometheus exporter: %v", err)
		} else {
			if err := promExporter.Start(ctx); err != nil {
				log.Printf("[WARN] Failed to start Prometheus exporter: %v", err)
			} else {
				exporters = append(exporters, promExporter)
				exporterInterfaces = append(exporterInterfaces, promExporter)
				promExporterTyped = promExporter
				log.Printf("[INFO] Prometheus exporter created and started on port %d", config.Exporters.Prometheus.Port)
			}
		}
	}

	// If no exporters were created, use a no-op exporter to satisfy the detector requirements
	if len(exporterInterfaces) == 0 {
		log.Printf("[INFO] No exporters enabled, using no-op exporter")
		noopExp := &noopExporter{}
		exporters = append(exporters, noopExp)
		exporterInterfaces = append(exporterInterfaces, noopExp)
	}

	return exporters, exporterInterfaces, promExporterTyped, nil
}

// dumpConfiguration prints the effective configuration as JSON
func dumpConfiguration(config *types.NodeDoctorConfig) {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal configuration: %v", err)
	}
	fmt.Println(string(data))
}

// ExporterLifecycle interface for exporters that need lifecycle management
type ExporterLifecycle interface {
	Stop() error
}

// monitorFactoryAdapter adapts the monitors package to the detector.MonitorFactory interface
type monitorFactoryAdapter struct {
	ctx context.Context
}

// CreateMonitor implements the detector.MonitorFactory interface
func (mfa *monitorFactoryAdapter) CreateMonitor(config types.MonitorConfig) (types.Monitor, error) {
	return monitors.CreateMonitor(mfa.ctx, config)
}

// wireLeaseClient constructs a LeaseClient from the coordination config and
// installs it on the registry so registry.Remediate() will request a lease
// from the controller before each remediation. When coordination is disabled
// or unconfigured, this is a no-op (returns nil).
//
// Returning a non-nil error means the operator opted in to coordination but
// construction failed; the caller should warn-and-continue rather than abort
// startup, because remediation is still safe without a lease (the registry
// simply skips the lease phase when its lease client is nil).
func wireLeaseClient(registry *remediators.RemediatorRegistry, config *types.NodeDoctorConfig) error {
	if registry == nil {
		return nil
	}
	coord := config.Remediation.Coordination
	if coord == nil || !coord.Enabled {
		return nil
	}
	leaseClient, err := remediators.NewLeaseClient(coord, config.Settings.NodeName)
	if err != nil {
		return fmt.Errorf("create lease client: %w", err)
	}
	registry.SetLeaseClient(leaseClient)
	log.Printf("[INFO] Lease client wired (controller=%s node=%s leaseTimeout=%v)",
		coord.ControllerURL, config.Settings.NodeName, coord.LeaseTimeout)
	return nil
}
