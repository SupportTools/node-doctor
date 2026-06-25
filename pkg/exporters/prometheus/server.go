package prometheus

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// isDualStackHost reports whether host represents a dual-stack/IPv6 wildcard
// bind that may fail on nodes where IPv6 is disabled. This covers the empty
// host, the IPv6 unspecified address "::", and any other IPv6 literal.
// For these we attempt a graceful fallback to the IPv4 wildcard "0.0.0.0".
func isDualStackHost(host string) bool {
	if host == "" || host == "::" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.To4() == nil
}

// listenWithFallback opens a TCP listener on host:port. When the host is a
// dual-stack/IPv6 wildcard (e.g. "::" or empty) and the bind fails — typically
// because IPv6 is disabled on the node (net.ipv6.bindv6only / missing kernel
// module) — it logs a warning and retries on the IPv4 wildcard "0.0.0.0".
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

// startHTTPServer starts an HTTP server to serve the /metrics endpoint.
// It binds host:port with a graceful IPv4 fallback for dual-stack hosts.
func startHTTPServer(ctx context.Context, host string, port int, path string, registry *prometheus.Registry) (*http.Server, error) {
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
	// listenWithFallback handles IPv6/dual-stack -> IPv4 fallback.
	ln, err := listenWithFallback(host, port)
	if err != nil {
		return nil, fmt.Errorf("prometheus server failed to bind: %w", err)
	}

	// Create HTTP server. BaseContext propagates the caller's context to every
	// in-flight request so they observe shutdown/cancellation.
	server := &http.Server{
		Addr:         ln.Addr().String(),
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
		BaseContext:  func(net.Listener) context.Context { return ctx },
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
