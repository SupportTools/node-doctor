// Node Doctor - Kubernetes node monitoring and auto-remediation tool
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/supporttools/node-doctor/pkg/detector"
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

// Command-line flags
var (
	configPath = flag.String("config", "/etc/node-doctor/config.yaml", "Path to configuration file")
	nodeName   = flag.String("node-name", "", "Override node name (defaults to config or $NODE_NAME)")
	logLevel   = flag.String("log-level", "", "Override log level (debug, info, warn, error, fatal)")
	logFormat  = flag.String("log-format", "", "Override log format (json, text)")
	kubeconfig = flag.String("kubeconfig", "", "Override kubeconfig path")
	dryRun     = flag.Bool("dry-run", false, "Enable dry-run mode (disable remediation)")
	version    = flag.Bool("version", false, "Show version information and exit")
)

func main() {
	flag.Parse()

	// Handle version flag
	if *version {
		printVersion()
		os.Exit(0)
	}

	log.Printf("[INFO] Node Doctor %s starting...", Version)

	// Load configuration
	config, err := loadConfiguration()
	if err != nil {
		log.Fatalf("[ERROR] Failed to load configuration: %v", err)
	}

	// Setup logging based on configuration
	setupLogging(config.Settings)

	log.Printf("[INFO] Configuration loaded successfully from %s", *configPath)
	log.Printf("[INFO] Node: %s, Log Level: %s, Log Format: %s",
		config.Settings.NodeName, config.Settings.LogLevel, config.Settings.LogFormat)

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create monitors
	monitors, err := createMonitors(ctx, config)
	if err != nil {
		log.Fatalf("[ERROR] Failed to create monitors: %v", err)
	}

	log.Printf("[INFO] Created %d monitors", len(monitors))

	// Create exporters (stub implementation)
	exporters := createExporters(config)
	log.Printf("[INFO] Created %d exporters", len(exporters))

	// Create problem detector
	detector, err := detector.NewProblemDetector(config, monitors, exporters)
	if err != nil {
		log.Fatalf("[ERROR] Failed to create problem detector: %v", err)
	}

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	// Start detector in goroutine
	errChan := make(chan error, 1)
	go func() {
		if err := detector.Run(ctx); err != nil {
			errChan <- fmt.Errorf("detector failed: %w", err)
		}
	}()

	log.Printf("[INFO] Node Doctor started successfully")

	// Wait for shutdown signal or error
	select {
	case sig := <-sigChan:
		log.Printf("[INFO] Received signal %v, initiating graceful shutdown", sig)
	case err := <-errChan:
		log.Printf("[ERROR] Detector error: %v", err)
	}

	// Cancel context to signal shutdown
	cancel()

	// Give detector time to shutdown gracefully
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	shutdownDone := make(chan struct{})
	go func() {
		// Wait for detector to finish
		<-errChan
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
		log.Printf("[INFO] Graceful shutdown completed")
	case <-shutdownCtx.Done():
		log.Printf("[WARN] Shutdown timeout exceeded, forcing exit")
	}

	log.Printf("[INFO] Node Doctor stopped")
}

// loadConfiguration loads and validates the configuration with proper precedence:
// 1. Start with file config or defaults if file doesn't exist
// 2. Apply CLI flag overrides
// 3. Re-validate the final configuration
func loadConfiguration() (*types.NodeDoctorConfig, error) {
	var config *types.NodeDoctorConfig
	var err error

	// Try to load config from file, fall back to defaults
	if _, statErr := os.Stat(*configPath); os.IsNotExist(statErr) {
		log.Printf("[WARN] Config file %s not found, using defaults", *configPath)
		config, err = util.DefaultConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to create default config: %w", err)
		}
	} else {
		config, err = util.LoadConfig(*configPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load config from %s: %w", *configPath, err)
		}
	}

	// Apply CLI flag overrides
	applyFlagOverrides(config)

	// Re-validate configuration after overrides
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed after applying overrides: %w", err)
	}

	return config, nil
}

// applyFlagOverrides applies command-line flag overrides to the configuration
func applyFlagOverrides(config *types.NodeDoctorConfig) {
	if *nodeName != "" {
		log.Printf("[INFO] Overriding node name: %s -> %s", config.Settings.NodeName, *nodeName)
		config.Settings.NodeName = *nodeName
	}

	if *logLevel != "" {
		log.Printf("[INFO] Overriding log level: %s -> %s", config.Settings.LogLevel, *logLevel)
		config.Settings.LogLevel = *logLevel
	}

	if *logFormat != "" {
		log.Printf("[INFO] Overriding log format: %s -> %s", config.Settings.LogFormat, *logFormat)
		config.Settings.LogFormat = *logFormat
	}

	if *kubeconfig != "" {
		log.Printf("[INFO] Overriding kubeconfig: %s -> %s", config.Settings.Kubeconfig, *kubeconfig)
		config.Settings.Kubeconfig = *kubeconfig
	}

	if *dryRun {
		log.Printf("[INFO] Enabling dry-run mode (remediation disabled)")
		config.Settings.DryRunMode = true
		config.Remediation.DryRun = true
	}
}

// setupLogging configures the logging subsystem based on the configuration
func setupLogging(settings types.GlobalSettings) {
	// Configure log flags based on format
	switch settings.LogFormat {
	case "json":
		log.SetFlags(0) // JSON format handles timestamps internally
	case "text":
		log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	default:
		log.SetFlags(log.LstdFlags)
	}

	// Configure output destination
	switch settings.LogOutput {
	case "stdout":
		log.SetOutput(os.Stdout)
	case "stderr":
		log.SetOutput(os.Stderr)
	case "file":
		if settings.LogFile != "" {
			file, err := os.OpenFile(settings.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			if err != nil {
				log.Fatalf("[ERROR] Failed to open log file %s: %v", settings.LogFile, err)
			}
			log.SetOutput(file)
			log.Printf("[INFO] Logging to file: %s", settings.LogFile)
		} else {
			log.Printf("[WARN] Log output set to 'file' but no log file specified, using stderr")
			log.SetOutput(os.Stderr)
		}
	default:
		log.SetOutput(os.Stderr)
	}

	log.Printf("[INFO] Logging configured: level=%s, format=%s, output=%s",
		settings.LogLevel, settings.LogFormat, settings.LogOutput)
}

// createMonitors creates and initializes all enabled monitors from the configuration
func createMonitors(ctx context.Context, config *types.NodeDoctorConfig) ([]types.Monitor, error) {
	monitors, err := monitors.CreateMonitorsFromConfigs(ctx, config.Monitors)
	if err != nil {
		return nil, fmt.Errorf("failed to create monitors: %w", err)
	}

	if len(monitors) == 0 {
		return nil, fmt.Errorf("no monitors were created (all disabled or invalid)")
	}

	// Log monitor status
	enabledCount := 0
	disabledCount := 0
	for _, monitorConfig := range config.Monitors {
		if monitorConfig.Enabled {
			enabledCount++
			log.Printf("[INFO] Monitor enabled: %s (%s)", monitorConfig.Name, monitorConfig.Type)
		} else {
			disabledCount++
			log.Printf("[INFO] Monitor disabled: %s (%s)", monitorConfig.Name, monitorConfig.Type)
		}
	}

	log.Printf("[INFO] Monitor summary: %d enabled, %d disabled, %d created",
		enabledCount, disabledCount, len(monitors))

	return monitors, nil
}

// createExporters creates exporter instances (stub implementation for now)
// TODO: Task #3058 - Implement Kubernetes exporter
// TODO: Task #3059 - Implement HTTP exporter
// TODO: Task #3060 - Implement Prometheus exporter
func createExporters(config *types.NodeDoctorConfig) []types.Exporter {
	var exporters []types.Exporter

	// Log what would be created
	if config.Exporters.Kubernetes != nil && config.Exporters.Kubernetes.Enabled {
		log.Printf("[INFO] Kubernetes exporter would be enabled (Task #3058 not implemented)")
	}
	if config.Exporters.HTTP != nil && config.Exporters.HTTP.Enabled {
		log.Printf("[INFO] HTTP exporter would be enabled (Task #3059 not implemented)")
	}
	if config.Exporters.Prometheus != nil && config.Exporters.Prometheus.Enabled {
		log.Printf("[INFO] Prometheus exporter would be enabled (Task #3060 not implemented)")
	}

	// For now, use a no-op exporter to satisfy the detector requirements
	exporters = append(exporters, &noopExporter{})

	return exporters
}

// printVersion prints version information to stdout
func printVersion() {
	fmt.Printf("node-doctor %s\n", Version)
	fmt.Printf("  Git Commit: %s\n", GitCommit)
	fmt.Printf("  Built: %s\n", BuildTime)
	fmt.Printf("  Go Version: %s\n", runtime.Version())
	fmt.Printf("  OS/Arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
}

// noopExporter is a stub exporter implementation that logs received data
// This will be replaced by real exporters in Tasks #3058-3060
type noopExporter struct{}

// ExportStatus implements the types.Exporter interface
func (e *noopExporter) ExportStatus(ctx context.Context, status *types.Status) error {
	log.Printf("[DEBUG] NoOp exporter: received status from %s with %d events, %d conditions",
		status.Source, len(status.Events), len(status.Conditions))
	return nil
}

// ExportProblem implements the types.Exporter interface
func (e *noopExporter) ExportProblem(ctx context.Context, problem *types.Problem) error {
	log.Printf("[DEBUG] NoOp exporter: received problem %s/%s (severity: %s): %s",
		problem.Type, problem.Resource, problem.Severity, problem.Message)
	return nil
}
