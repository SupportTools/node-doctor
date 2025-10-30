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
	"github.com/supporttools/node-doctor/pkg/monitors"
	"github.com/supporttools/node-doctor/pkg/types"
	"github.com/supporttools/node-doctor/pkg/util"

	// Import example monitors to register them
	_ "github.com/supporttools/node-doctor/pkg/monitors/example"
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

	if err := config.Validate(); err != nil {
		log.Fatalf("Configuration validation failed: %v", err)
	}

	if *validateConfig {
		log.Printf("[INFO] Configuration validation passed")
		return
	}

	if *dumpConfig {
		dumpConfiguration(config)
		return
	}

	// Setup basic logging (detailed logging setup would need more implementation)
	log.Printf("[INFO] Node Doctor starting on node: %s", config.Settings.NodeName)
	log.Printf("[INFO] Log level: %s, format: %s", config.Settings.LogLevel, config.Settings.LogFormat)
	if config.Settings.DryRunMode {
		log.Printf("[WARN] Running in DRY-RUN mode - no actual remediation will be performed")
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Create and start monitors
	log.Printf("[INFO] Creating monitors...")
	monitorInstances, err := createMonitors(ctx, config.Monitors)
	if err != nil {
		log.Fatalf("Failed to create monitors: %v", err)
	}

	if len(monitorInstances) == 0 {
		log.Fatalf("No valid monitors configured")
	}

	log.Printf("[INFO] Created %d monitors", len(monitorInstances))

	// Create exporters
	log.Printf("[INFO] Creating exporters...")
	exporters, exporterInterfaces, err := createExporters(ctx, config)
	if err != nil {
		log.Fatalf("Failed to create exporters: %v", err)
	}

	log.Printf("[INFO] Created %d exporters", len(exporters))

	// Create the detector with configured monitors and exporters
	log.Printf("[INFO] Creating detector...")

	det, err := detector.NewProblemDetector(config, monitorInstances, exporterInterfaces)
	if err != nil {
		log.Fatalf("Failed to create detector: %v", err)
	}

	// Start the detector
	log.Printf("[INFO] Starting detector...")
	if err := det.Run(ctx); err != nil {
		log.Fatalf("Failed to start detector: %v", err)
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

// createExporters creates and configures all exporters from the configuration
func createExporters(ctx context.Context, config *types.NodeDoctorConfig) ([]ExporterLifecycle, []types.Exporter, error) {
	var exporters []ExporterLifecycle
	var exporterInterfaces []types.Exporter

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

	// Create Prometheus exporter if enabled (not implemented yet)
	if config.Exporters.Prometheus != nil && config.Exporters.Prometheus.Enabled {
		log.Printf("[INFO] Prometheus exporter would be enabled (Task #3060 not implemented)")
	}

	// If no exporters were created, use a no-op exporter to satisfy the detector requirements
	if len(exporterInterfaces) == 0 {
		log.Printf("[INFO] No exporters enabled, using no-op exporter")
		noopExp := &noopExporter{}
		exporters = append(exporters, noopExp)
		exporterInterfaces = append(exporterInterfaces, noopExp)
	}

	return exporters, exporterInterfaces, nil
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