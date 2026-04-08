package prometheus

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// startHTTPServer starts an HTTP server to serve the /metrics endpoint
func startHTTPServer(ctx context.Context, addr, path string, registry *prometheus.Registry) (*http.Server, error) {
	if registry == nil {
		return nil, fmt.Errorf("registry cannot be nil")
	}

	// Create prometheus handler
	handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
		ErrorLog:          log.Default(),
		ErrorHandling:     promhttp.ContinueOnError,
	})

	// Create HTTP mux
	mux := http.NewServeMux()
	mux.Handle(path, handler)

	// Add health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy","service":"prometheus-exporter"}`))
	})

	// Eagerly bind the listener so bind failures propagate synchronously.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("prometheus server failed to bind %s: %w", addr, err)
	}

	// Create HTTP server
	server := &http.Server{
		Addr:         ln.Addr().String(),
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server using the already-bound listener
	go func() {
		log.Printf("[INFO] Starting Prometheus metrics server on %s%s", ln.Addr(), path)
		if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("[ERROR] Prometheus metrics server error: %v", err)
		}
	}()

	return server, nil
}

// shutdownServer gracefully shuts down the HTTP server
func shutdownServer(server *http.Server, timeout time.Duration) error {
	if server == nil {
		return nil
	}

	log.Printf("[INFO] Shutting down Prometheus metrics server...")

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Attempt graceful shutdown
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("[WARN] Prometheus server shutdown error: %v", err)
		return err
	}

	log.Printf("[INFO] Prometheus metrics server shut down successfully")
	return nil
}
