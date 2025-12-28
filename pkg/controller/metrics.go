package controller

import (
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ControllerMetrics contains all Prometheus metrics for the controller
type ControllerMetrics struct {
	// Cluster-level metrics
	NodesTotal     prometheus.Gauge
	NodesHealthy   prometheus.Gauge
	NodesDegraded  prometheus.Gauge
	NodesCritical  prometheus.Gauge
	NodesUnknown   prometheus.Gauge
	ActiveProblems prometheus.Gauge

	// Problem aggregation
	ProblemNodes  *prometheus.GaugeVec
	ProblemActive *prometheus.GaugeVec

	// Correlation metrics
	CorrelationActive   prometheus.Gauge
	CorrelationDetected *prometheus.CounterVec

	// Remediation metrics
	LeasesActive  prometheus.Gauge
	LeasesGranted *prometheus.CounterVec
	LeasesDenied  *prometheus.CounterVec

	// Report ingestion metrics
	ReportsReceived prometheus.Counter
	ReportErrors    *prometheus.CounterVec

	// Storage metrics
	StorageOperations *prometheus.CounterVec
	StorageErrors     *prometheus.CounterVec

	// Server metrics
	RequestDuration *prometheus.HistogramVec
	RequestsTotal   *prometheus.CounterVec

	registry *prometheus.Registry
	mu       sync.RWMutex
}

// NewControllerMetrics creates and registers all controller metrics
func NewControllerMetrics() *ControllerMetrics {
	m := &ControllerMetrics{
		registry: prometheus.NewRegistry(),
	}

	// Cluster-level metrics
	m.NodesTotal = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "node_doctor",
		Subsystem: "cluster",
		Name:      "nodes_total",
		Help:      "Total number of nodes reporting to the controller",
	})

	m.NodesHealthy = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "node_doctor",
		Subsystem: "cluster",
		Name:      "nodes_healthy",
		Help:      "Number of nodes with healthy status",
	})

	m.NodesDegraded = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "node_doctor",
		Subsystem: "cluster",
		Name:      "nodes_degraded",
		Help:      "Number of nodes with degraded status",
	})

	m.NodesCritical = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "node_doctor",
		Subsystem: "cluster",
		Name:      "nodes_critical",
		Help:      "Number of nodes with critical status",
	})

	m.NodesUnknown = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "node_doctor",
		Subsystem: "cluster",
		Name:      "nodes_unknown",
		Help:      "Number of nodes with unknown status",
	})

	m.ActiveProblems = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "node_doctor",
		Subsystem: "cluster",
		Name:      "problems_active",
		Help:      "Total number of active problems across all nodes",
	})

	// Problem aggregation metrics
	m.ProblemNodes = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "node_doctor",
			Subsystem: "cluster",
			Name:      "problem_nodes",
			Help:      "Number of nodes affected by each problem type",
		},
		[]string{"problem_type", "severity"},
	)

	m.ProblemActive = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "node_doctor",
			Subsystem: "cluster",
			Name:      "problem_active",
			Help:      "Whether a problem type is currently active (1) or not (0)",
		},
		[]string{"problem_type"},
	)

	// Correlation metrics
	m.CorrelationActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "node_doctor",
		Subsystem: "correlation",
		Name:      "active_total",
		Help:      "Number of active correlations",
	})

	m.CorrelationDetected = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "node_doctor",
			Subsystem: "correlation",
			Name:      "detected_total",
			Help:      "Total number of correlations detected",
		},
		[]string{"type"},
	)

	// Remediation metrics
	m.LeasesActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "node_doctor",
		Subsystem: "leases",
		Name:      "active_total",
		Help:      "Number of active remediation leases",
	})

	m.LeasesGranted = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "node_doctor",
			Subsystem: "leases",
			Name:      "granted_total",
			Help:      "Total number of leases granted",
		},
		[]string{"remediation_type"},
	)

	m.LeasesDenied = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "node_doctor",
			Subsystem: "leases",
			Name:      "denied_total",
			Help:      "Total number of leases denied",
		},
		[]string{"reason"},
	)

	// Report ingestion metrics
	m.ReportsReceived = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "node_doctor",
		Subsystem: "reports",
		Name:      "received_total",
		Help:      "Total number of reports received from nodes",
	})

	m.ReportErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "node_doctor",
			Subsystem: "reports",
			Name:      "errors_total",
			Help:      "Total number of report processing errors",
		},
		[]string{"error_type"},
	)

	// Storage metrics
	m.StorageOperations = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "node_doctor",
			Subsystem: "storage",
			Name:      "operations_total",
			Help:      "Total number of storage operations",
		},
		[]string{"operation"},
	)

	m.StorageErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "node_doctor",
			Subsystem: "storage",
			Name:      "errors_total",
			Help:      "Total number of storage errors",
		},
		[]string{"operation"},
	)

	// Server metrics
	m.RequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "node_doctor",
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "HTTP request duration in seconds",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"method", "path", "status"},
	)

	m.RequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "node_doctor",
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "Total number of HTTP requests",
		},
		[]string{"method", "path", "status"},
	)

	// Register all metrics
	m.registry.MustRegister(
		m.NodesTotal,
		m.NodesHealthy,
		m.NodesDegraded,
		m.NodesCritical,
		m.NodesUnknown,
		m.ActiveProblems,
		m.ProblemNodes,
		m.ProblemActive,
		m.CorrelationActive,
		m.CorrelationDetected,
		m.LeasesActive,
		m.LeasesGranted,
		m.LeasesDenied,
		m.ReportsReceived,
		m.ReportErrors,
		m.StorageOperations,
		m.StorageErrors,
		m.RequestDuration,
		m.RequestsTotal,
	)

	return m
}

// Handler returns an http.Handler for the /metrics endpoint
func (m *ControllerMetrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

// UpdateClusterMetrics updates all cluster-level metrics from current state
func (m *ControllerMetrics) UpdateClusterMetrics(status *ClusterStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.NodesTotal.Set(float64(status.TotalNodes))
	m.NodesHealthy.Set(float64(status.HealthyNodes))
	m.NodesDegraded.Set(float64(status.DegradedNodes))
	m.NodesCritical.Set(float64(status.CriticalNodes))
	m.NodesUnknown.Set(float64(status.UnknownNodes))
	m.ActiveProblems.Set(float64(status.ActiveProblems))
}

// UpdateProblemMetrics updates problem-related metrics
func (m *ControllerMetrics) UpdateProblemMetrics(problemCounts map[string]map[string]int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Reset problem metrics
	m.ProblemNodes.Reset()
	m.ProblemActive.Reset()

	// Update with current counts
	for problemType, severityCounts := range problemCounts {
		m.ProblemActive.WithLabelValues(problemType).Set(1)
		for severity, count := range severityCounts {
			m.ProblemNodes.WithLabelValues(problemType, severity).Set(float64(count))
		}
	}
}

// UpdateLeaseMetrics updates lease-related metrics
func (m *ControllerMetrics) UpdateLeaseMetrics(activeCount int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.LeasesActive.Set(float64(activeCount))
}

// UpdateCorrelationMetrics updates correlation-related metrics
func (m *ControllerMetrics) UpdateCorrelationMetrics(activeCount int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.CorrelationActive.Set(float64(activeCount))
}

// RecordLeaseGranted increments the lease granted counter
func (m *ControllerMetrics) RecordLeaseGranted(remediationType string) {
	m.LeasesGranted.WithLabelValues(remediationType).Inc()
}

// RecordLeaseDenied increments the lease denied counter
func (m *ControllerMetrics) RecordLeaseDenied(reason string) {
	m.LeasesDenied.WithLabelValues(reason).Inc()
}

// RecordReportReceived increments the report received counter
func (m *ControllerMetrics) RecordReportReceived() {
	m.ReportsReceived.Inc()
}

// RecordReportError increments the report error counter
func (m *ControllerMetrics) RecordReportError(errorType string) {
	m.ReportErrors.WithLabelValues(errorType).Inc()
}

// RecordCorrelationDetected increments the correlation detected counter
func (m *ControllerMetrics) RecordCorrelationDetected(correlationType string) {
	m.CorrelationDetected.WithLabelValues(correlationType).Inc()
}

// RecordStorageOperation increments the storage operation counter
func (m *ControllerMetrics) RecordStorageOperation(operation string) {
	m.StorageOperations.WithLabelValues(operation).Inc()
}

// RecordStorageError increments the storage error counter
func (m *ControllerMetrics) RecordStorageError(operation string) {
	m.StorageErrors.WithLabelValues(operation).Inc()
}

// RecordRequest records an HTTP request
func (m *ControllerMetrics) RecordRequest(method, path, status string, duration float64) {
	m.RequestDuration.WithLabelValues(method, path, status).Observe(duration)
	m.RequestsTotal.WithLabelValues(method, path, status).Inc()
}
