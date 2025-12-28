// Node Doctor Controller - Multi-node aggregation and coordination
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

	"gopkg.in/yaml.v3"

	"github.com/supporttools/node-doctor/pkg/controller"
)

// Build-time variables set by goreleaser or make
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildTime = "unknown"
)

func main() {
	// Parse command line flags
	var (
		configFile     = flag.String("config", "", "Path to configuration file")
		version        = flag.Bool("version", false, "Show version information")
		validateConfig = flag.Bool("validate-config", false, "Validate configuration and exit")
		bindAddress    = flag.String("bind-address", "", "Override bind address")
		port           = flag.Int("port", 0, "Override port")
		debug          = flag.Bool("debug", false, "Enable debug logging")
	)
	flag.Parse()

	if *version {
		fmt.Printf("Node Doctor Controller %s\n", Version)
		fmt.Printf("Git Commit: %s\n", GitCommit)
		fmt.Printf("Build Time: %s\n", BuildTime)
		fmt.Printf("Go Version: %s\n", runtime.Version())
		fmt.Printf("OS/Arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
		return
	}

	log.Printf("[INFO] Starting Node Doctor Controller %s (commit: %s, built: %s)",
		Version, GitCommit, BuildTime)

	// Load configuration
	config, err := loadConfig(*configFile)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Apply command line overrides
	if *bindAddress != "" {
		config.Server.BindAddress = *bindAddress
	}
	if *port > 0 {
		config.Server.Port = *port
	}
	if *debug {
		log.Printf("[DEBUG] Debug logging enabled")
	}

	if *validateConfig {
		log.Printf("[INFO] Configuration validation passed")
		return
	}

	// Log configuration summary
	log.Printf("[INFO] Server: %s:%d", config.Server.BindAddress, config.Server.Port)
	log.Printf("[INFO] Storage: %s (retention: %s)", config.Storage.Path, config.Storage.Retention)
	log.Printf("[INFO] Correlation: enabled=%v, threshold=%.0f%%",
		config.Correlation.Enabled, config.Correlation.ClusterWideThreshold*100)
	log.Printf("[INFO] Coordination: enabled=%v, maxConcurrent=%d",
		config.Coordination.Enabled, config.Coordination.MaxConcurrentRemediations)

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Initialize storage if path is configured
	var storage controller.Storage
	if config.Storage.Path != "" {
		log.Printf("[INFO] Initializing SQLite storage at: %s", config.Storage.Path)
		sqliteStorage, err := controller.NewSQLiteStorage(&config.Storage)
		if err != nil {
			log.Fatalf("Failed to initialize storage: %v", err)
		}
		storage = sqliteStorage
		defer func() {
			if err := sqliteStorage.Close(); err != nil {
				log.Printf("[ERROR] Error closing storage: %v", err)
			}
		}()
		log.Printf("[INFO] SQLite storage initialized successfully")
	} else {
		log.Printf("[INFO] No storage path configured, using in-memory storage only")
	}

	// Create and start server
	server, err := controller.NewServer(config)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	// Set storage if available
	if storage != nil {
		server.SetStorage(storage)
	}

	if err := server.Start(ctx); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}

	log.Printf("[INFO] Node Doctor Controller started successfully")

	// Start background cleanup goroutine if storage is configured
	if storage != nil {
		go func() {
			ticker := time.NewTicker(1 * time.Hour)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := storage.RunCleanup(ctx); err != nil {
						log.Printf("[ERROR] Storage cleanup failed: %v", err)
					} else {
						log.Printf("[DEBUG] Storage cleanup completed")
					}
				}
			}
		}()
	}

	// Wait for shutdown signal
	sig := <-sigCh
	log.Printf("[INFO] Received signal %v, shutting down...", sig)

	// Create shutdown context with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Stop server gracefully
	if err := server.Stop(shutdownCtx); err != nil {
		log.Printf("[ERROR] Error during shutdown: %v", err)
	}

	log.Printf("[INFO] Node Doctor Controller stopped")
}

// loadConfig loads the controller configuration from file or defaults
func loadConfig(configFile string) (*controller.ControllerConfig, error) {
	// Start with defaults
	config := controller.DefaultControllerConfig()

	// If no config file specified, look in standard locations
	if configFile == "" {
		candidates := []string{
			"/etc/node-doctor/controller.yaml",
			"/etc/node-doctor/controller.yml",
			"./controller.yaml",
			"./controller.yml",
		}

		for _, candidate := range candidates {
			if _, err := os.Stat(candidate); err == nil {
				configFile = candidate
				break
			}
		}
	}

	// Load config from file if found
	if configFile != "" {
		log.Printf("[INFO] Loading configuration from: %s", configFile)

		data, err := os.ReadFile(configFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}

		if err := yaml.Unmarshal(data, config); err != nil {
			return nil, fmt.Errorf("failed to parse config file: %w", err)
		}
	} else {
		log.Printf("[INFO] No configuration file found, using defaults")
	}

	// Apply environment variable overrides
	applyEnvOverrides(config)

	return config, nil
}

// applyEnvOverrides applies environment variable overrides to the config
func applyEnvOverrides(config *controller.ControllerConfig) {
	// Server settings
	if v := os.Getenv("CONTROLLER_BIND_ADDRESS"); v != "" {
		config.Server.BindAddress = v
	}
	if v := os.Getenv("CONTROLLER_PORT"); v != "" {
		var port int
		if _, err := fmt.Sscanf(v, "%d", &port); err == nil {
			config.Server.Port = port
		}
	}

	// Storage settings
	if v := os.Getenv("CONTROLLER_STORAGE_PATH"); v != "" {
		config.Storage.Path = v
	}
	if v := os.Getenv("CONTROLLER_STORAGE_RETENTION"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			config.Storage.Retention = d
		}
	}

	// Coordination settings
	if v := os.Getenv("CONTROLLER_MAX_CONCURRENT_REMEDIATIONS"); v != "" {
		var max int
		if _, err := fmt.Sscanf(v, "%d", &max); err == nil {
			config.Coordination.MaxConcurrentRemediations = max
		}
	}

	// Prometheus settings
	if v := os.Getenv("CONTROLLER_PROMETHEUS_PORT"); v != "" {
		var port int
		if _, err := fmt.Sscanf(v, "%d", &port); err == nil {
			config.Prometheus.Port = port
		}
	}

	// Kubernetes settings
	if v := os.Getenv("KUBECONFIG"); v != "" {
		config.Kubernetes.Kubeconfig = v
		config.Kubernetes.InCluster = false
	}
	if v := os.Getenv("CONTROLLER_NAMESPACE"); v != "" {
		config.Kubernetes.Namespace = v
	}
}
