// Node Doctor - Kubernetes node monitoring and auto-remediation tool
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/supporttools/node-doctor/pkg/detector"
	httpexporter "github.com/supporttools/node-doctor/pkg/exporters/http"
	kubernetesexporter "github.com/supporttools/node-doctor/pkg/exporters/kubernetes"
	prometheusexporter "github.com/supporttools/node-doctor/pkg/exporters/prometheus"
	"github.com/supporttools/node-doctor/pkg/health"
	"github.com/supporttools/node-doctor/pkg/logger"
	"github.com/supporttools/node-doctor/pkg/monitors"
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
	logger.WithFields(logrus.Fields{
		"component":  "noopExporter",
		"source":     status.Source,
		"events":     len(status.Events),
		"conditions": len(status.Conditions),
	}).Debug("Status export (noop)")
	return nil
}

func (e *noopExporter) ExportProblem(ctx context.Context, problem *types.Problem) error {
	logger.WithFields(logrus.Fields{
		"component": "noopExporter",
		"type":      problem.Type,
		"resource":  problem.Resource,
		"severity":  problem.Severity,
	}).Debugf("Problem export (noop): %s", problem.Message)
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

		fmt.Println("Searching for configuration file in standard locations...")
		for _, candidate := range candidates {
			if _, err := os.Stat(candidate); err == nil {
				*configFile = candidate
				fmt.Printf("Found configuration file: %s\n", candidate)
				break
			}
		}

		if *configFile == "" {
			fmt.Fprintf(os.Stderr, "No configuration file found. Use -config flag or place config.yaml in current directory or /etc/node-doctor/\n")
			os.Exit(1)
		}
	}

	fmt.Printf("Starting Node Doctor %s (commit: %s, built: %s)\n", Version, GitCommit, BuildTime)
	fmt.Printf("Loading configuration from: %s\n", *configFile)

	// Load and validate configuration
	config, err := util.LoadConfig(*configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
		os.Exit(1)
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
		fmt.Fprintf(os.Stderr, "Failed to apply configuration defaults: %v\n", err)
		os.Exit(1)
	}

	if err := config.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Configuration validation failed: %v\n", err)
		os.Exit(1)
	}

	// Initialize structured logging
	if err := logger.Initialize(config.Settings.LogLevel, config.Settings.LogFormat, config.Settings.LogOutput, config.Settings.LogFile); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	if *validateConfig {
		logger.Info("Configuration validation passed")
		return
	}

	if *dumpConfig {
		dumpConfiguration(config)
		return
	}

	// Setup basic logging (detailed logging setup would need more implementation)
	logger.WithFields(logrus.Fields{
		"node": config.Settings.NodeName,
		"log_level": config.Settings.LogLevel,
		"log_format": config.Settings.LogFormat,
	}).Info("Node Doctor starting")

	if config.Settings.DryRunMode {
		logger.Warn("Running in DRY-RUN mode - no actual remediation will be performed")
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Create and start monitors
	logger.Info("Creating monitors...")
	monitorInstances, err := createMonitors(ctx, config.Monitors)
	if err != nil {
		logger.Fatal("Failed to create monitors: " + err.Error())
	}

	if len(monitorInstances) == 0 {
		logger.Fatal("No valid monitors configured")
	}

	logger.WithFields(logrus.Fields{
		"count": len(monitorInstances),
	}).Info("Created monitors")

	// Create exporters
	logger.Info("Creating exporters...")
	exporters, exporterInterfaces, err := createExporters(ctx, config)
	if err != nil {
		logger.Fatal("Failed to create exporters: " + err.Error())
	}

	logger.WithFields(logrus.Fields{
		"count": len(exporters),
	}).Info("Created exporters")

	// Create monitor factory for hot reload
	monitorFactory := &monitorFactoryAdapter{ctx: ctx}

	// Create the detector with configured monitors and exporters
	logger.Info("Creating detector...")

	det, err := detector.NewProblemDetector(config, monitorInstances, exporterInterfaces, *configFile, monitorFactory)
	if err != nil {
		logger.Fatal("Failed to create detector: " + err.Error())
	}

	// Start the detector
	logger.Info("Starting detector...")
	if err := det.Start(); err != nil {
		logger.Fatal("Failed to start detector: " + err.Error())
	}

	logger.Info("Node Doctor started successfully")

	// Wait for shutdown signal
	<-sigCh
	logger.Info("Received shutdown signal, stopping...")

	// Ensure logs are flushed on exit
	defer func() {
		logger.Info("Flushing logs...")
		if err := logger.Flush(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to flush logs: %v\n", err)
		}
		if err := logger.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to close logger: %v\n", err)
		}
	}()

	// Create shutdown context with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Stop detector (the Run method handles its own cleanup)
	logger.Info("Stopping detector...")
	// Cancel the context to signal shutdown
	cancel()

	// Stop exporters
	logger.Info("Stopping exporters...")
	for _, exporter := range exporters {
		if err := exporter.Stop(); err != nil {
			logger.WithError(err).Warn("Error stopping exporter")
		}
	}

	// Wait for shutdown to complete or timeout
	select {
	case <-shutdownCtx.Done():
		logger.Warn("Shutdown timeout exceeded")
	default:
		logger.Info("Node Doctor stopped successfully")
	}
}

// createMonitors creates and configures all monitors from the configuration
func createMonitors(ctx context.Context, monitorConfigs []types.MonitorConfig) ([]types.Monitor, error) {
	var monitorInstances []types.Monitor

	for _, config := range monitorConfigs {
		if !config.Enabled {
			logger.WithFields(logrus.Fields{
				"monitor": config.Name,
			}).Info("Monitor is disabled, skipping")
			continue
		}

		logger.WithFields(logrus.Fields{
			"monitor": config.Name,
			"type": config.Type,
		}).Info("Creating monitor")

		monitor, err := monitors.CreateMonitor(ctx, config)
		if err != nil {
			logger.WithFields(logrus.Fields{
				"monitor": config.Name,
				"error": err,
			}).Error("Failed to create monitor")
			continue
		}

		monitorInstances = append(monitorInstances, monitor)
		logger.WithFields(logrus.Fields{
			"monitor": config.Name,
		}).Info("Created monitor")
	}

	return monitorInstances, nil
}

// createExporters creates and configures all exporters from the configuration
func createExporters(ctx context.Context, config *types.NodeDoctorConfig) ([]ExporterLifecycle, []types.Exporter, error) {
	var exporters []ExporterLifecycle
	var exporterInterfaces []types.Exporter

	// Create Kubernetes exporter if enabled
	if config.Exporters.Kubernetes != nil && config.Exporters.Kubernetes.Enabled {
		logger.WithFields(logrus.Fields{
			"exporter_type": "kubernetes",
		}).Info("Creating Kubernetes exporter...")
		k8sExporter, err := kubernetesexporter.NewKubernetesExporter(
			config.Exporters.Kubernetes,
			&config.Settings,
		)
		if err != nil {
			logger.WithFields(logrus.Fields{
				"exporter_type": "kubernetes",
				"error": err,
			}).Warn("Failed to create Kubernetes exporter")
		} else {
			if err := k8sExporter.Start(ctx); err != nil {
				logger.WithFields(logrus.Fields{
					"exporter_type": "kubernetes",
					"error": err,
				}).Warn("Failed to start Kubernetes exporter")
			} else {
				exporters = append(exporters, k8sExporter)
				exporterInterfaces = append(exporterInterfaces, k8sExporter)
				logger.WithFields(logrus.Fields{
					"exporter_type": "kubernetes",
				}).Info("Kubernetes exporter created and started")
			}
		}
	}

	// Create HTTP exporter if enabled
	if config.Exporters.HTTP != nil && config.Exporters.HTTP.Enabled {
		logger.WithFields(logrus.Fields{
			"exporter_type": "http",
		}).Info("Creating HTTP exporter...")
		httpExporter, err := httpexporter.NewHTTPExporter(
			config.Exporters.HTTP,
			&config.Settings,
		)
		if err != nil {
			logger.WithFields(logrus.Fields{
				"exporter_type": "http",
				"error": err,
			}).Warn("Failed to create HTTP exporter")
		} else {
			if err := httpExporter.Start(ctx); err != nil {
				logger.WithFields(logrus.Fields{
					"exporter_type": "http",
					"error": err,
				}).Warn("Failed to start HTTP exporter")
			} else {
				exporters = append(exporters, httpExporter)
				exporterInterfaces = append(exporterInterfaces, httpExporter)
				logger.WithFields(logrus.Fields{
					"exporter_type": "http",
				}).Info("HTTP exporter created and started")
			}
		}
	}

	// Create Health Server (always enabled for Kubernetes probes)
	logger.WithFields(logrus.Fields{
		"exporter_type": "health",
	}).Info("Creating health server...")
	healthServer, err := health.NewServer(&health.Config{
		Enabled:      true,
		BindAddress:  "0.0.0.0",
		Port:         8080,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	})
	if err != nil {
		logger.WithFields(logrus.Fields{
			"exporter_type": "health",
			"error": err,
		}).Warn("Failed to create health server")
	} else {
		if err := healthServer.Start(ctx); err != nil {
			logger.WithFields(logrus.Fields{
				"exporter_type": "health",
				"error": err,
			}).Warn("Failed to start health server")
		} else {
			exporters = append(exporters, healthServer)
			exporterInterfaces = append(exporterInterfaces, healthServer)
			logger.WithFields(logrus.Fields{
				"exporter_type": "health",
				"port": 8080,
			}).Info("Health server created and started")
		}
	}

	// Create Prometheus exporter if enabled
	if config.Exporters.Prometheus != nil && config.Exporters.Prometheus.Enabled {
		logger.WithFields(logrus.Fields{
			"exporter_type": "prometheus",
		}).Info("Creating Prometheus exporter...")
		promExporter, err := prometheusexporter.NewPrometheusExporter(
			config.Exporters.Prometheus,
			&config.Settings,
		)
		if err != nil {
			logger.WithFields(logrus.Fields{
				"exporter_type": "prometheus",
				"error": err,
			}).Warn("Failed to create Prometheus exporter")
		} else {
			if err := promExporter.Start(ctx); err != nil {
				logger.WithFields(logrus.Fields{
					"exporter_type": "prometheus",
					"error": err,
				}).Warn("Failed to start Prometheus exporter")
			} else {
				exporters = append(exporters, promExporter)
				exporterInterfaces = append(exporterInterfaces, promExporter)
				logger.WithFields(logrus.Fields{
					"exporter_type": "prometheus",
					"port": config.Exporters.Prometheus.Port,
				}).Info("Prometheus exporter created and started")
			}
		}
	}

	// If no exporters were created, use a no-op exporter to satisfy the detector requirements
	if len(exporterInterfaces) == 0 {
		logger.Info("No exporters enabled, using no-op exporter")
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
		logger.Fatal("Failed to marshal configuration: " + err.Error())
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