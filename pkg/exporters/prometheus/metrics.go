package prometheus

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics contains all the Prometheus metrics used by the Node Doctor
type Metrics struct {
	// Counter metrics
	ProblemsTotal         *prometheus.CounterVec
	StatusUpdatesTotal    *prometheus.CounterVec
	EventsTotal           *prometheus.CounterVec
	ConditionsTotal       *prometheus.CounterVec
	ExportOperationsTotal *prometheus.CounterVec
	ExportErrorsTotal     *prometheus.CounterVec

	// Gauge metrics
	ProblemsActive   *prometheus.GaugeVec
	MonitorUp        *prometheus.GaugeVec
	ConditionStatus  *prometheus.GaugeVec
	Info             *prometheus.GaugeVec
	StartTimeSeconds *prometheus.GaugeVec
	UptimeSeconds    *prometheus.GaugeVec

	// Network latency gauge metrics
	GatewayLatencySeconds   *prometheus.GaugeVec
	PeerLatencySeconds      *prometheus.GaugeVec
	PeerLatencyAvgSeconds   *prometheus.GaugeVec
	PeerReachable           *prometheus.GaugeVec
	PeersTotal              *prometheus.GaugeVec
	PeersReachableTotal     *prometheus.GaugeVec
	DNSLatencySeconds       *prometheus.GaugeVec
	APIServerLatencySeconds *prometheus.GaugeVec

	// Histogram metrics
	MonitorCheckDuration      *prometheus.HistogramVec
	ExportDuration            *prometheus.HistogramVec
	GatewayLatencyHistogram   *prometheus.HistogramVec
	PeerLatencyHistogram      *prometheus.HistogramVec
	DNSLatencyHistogram       *prometheus.HistogramVec
	APIServerLatencyHistogram *prometheus.HistogramVec
}

// NewMetrics creates a new Metrics instance with all metric definitions
func NewMetrics(namespace, subsystem string, constLabels prometheus.Labels) (*Metrics, error) {
	if namespace == "" {
		namespace = "node_doctor"
	}
	if subsystem == "" {
		subsystem = ""
	}

	// Merge constant labels with node label
	labels := make(prometheus.Labels)
	for k, v := range constLabels {
		labels[k] = v
	}

	m := &Metrics{
		// Counter metrics
		ProblemsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace:   namespace,
				Subsystem:   subsystem,
				Name:        "problems_total",
				Help:        "Total number of problems detected by Node Doctor",
				ConstLabels: labels,
			},
			[]string{"node", "problem_type", "severity", "source"},
		),

		StatusUpdatesTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace:   namespace,
				Subsystem:   subsystem,
				Name:        "status_updates_total",
				Help:        "Total number of status updates from Node Doctor",
				ConstLabels: labels,
			},
			[]string{"node", "source"},
		),

		EventsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace:   namespace,
				Subsystem:   subsystem,
				Name:        "events_total",
				Help:        "Total number of events processed by Node Doctor",
				ConstLabels: labels,
			},
			[]string{"node", "source", "severity"},
		),

		ConditionsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace:   namespace,
				Subsystem:   subsystem,
				Name:        "conditions_total",
				Help:        "Total number of node conditions processed by Node Doctor",
				ConstLabels: labels,
			},
			[]string{"node", "condition_type", "status"},
		),

		ExportOperationsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace:   namespace,
				Subsystem:   subsystem,
				Name:        "export_operations_total",
				Help:        "Total number of export operations performed by Node Doctor",
				ConstLabels: labels,
			},
			[]string{"node", "exporter", "operation", "result"},
		),

		ExportErrorsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace:   namespace,
				Subsystem:   subsystem,
				Name:        "export_errors_total",
				Help:        "Total number of export errors encountered by Node Doctor",
				ConstLabels: labels,
			},
			[]string{"node", "exporter", "error_type"},
		),

		// Gauge metrics
		ConditionStatus: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace:   namespace,
				Subsystem:   subsystem,
				Name:        "condition_status",
				Help:        "Current status of node conditions (1=active/True, 0=inactive/False)",
				ConstLabels: labels,
			},
			[]string{"node", "condition_type"},
		),

		ProblemsActive: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace:   namespace,
				Subsystem:   subsystem,
				Name:        "problems_active",
				Help:        "Current number of active problems detected by Node Doctor",
				ConstLabels: labels,
			},
			[]string{"node", "problem_type", "severity"},
		),

		MonitorUp: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace:   namespace,
				Subsystem:   subsystem,
				Name:        "monitor_up",
				Help:        "Whether Node Doctor monitors are running (1 = up, 0 = down)",
				ConstLabels: labels,
			},
			[]string{"node", "monitor_name", "monitor_type"},
		),

		Info: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace:   namespace,
				Subsystem:   subsystem,
				Name:        "info",
				Help:        "Node Doctor version and build information",
				ConstLabels: labels,
			},
			[]string{"node", "version", "git_commit", "go_version", "build_time"},
		),

		StartTimeSeconds: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace:   namespace,
				Subsystem:   subsystem,
				Name:        "start_time_seconds",
				Help:        "Unix timestamp when Node Doctor was started",
				ConstLabels: labels,
			},
			[]string{"node"},
		),

		UptimeSeconds: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace:   namespace,
				Subsystem:   subsystem,
				Name:        "uptime_seconds",
				Help:        "Number of seconds Node Doctor has been running",
				ConstLabels: labels,
			},
			[]string{"node"},
		),

		// Network latency gauge metrics
		GatewayLatencySeconds: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace:   namespace,
				Subsystem:   subsystem,
				Name:        "gateway_latency_seconds",
				Help:        "Current latency to the default gateway in seconds",
				ConstLabels: labels,
			},
			[]string{"node", "gateway_ip"},
		),

		PeerLatencySeconds: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace:   namespace,
				Subsystem:   subsystem,
				Name:        "peer_latency_seconds",
				Help:        "Last measured latency to peer node in seconds",
				ConstLabels: labels,
			},
			[]string{"node", "peer_node", "peer_ip"},
		),

		PeerLatencyAvgSeconds: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace:   namespace,
				Subsystem:   subsystem,
				Name:        "peer_latency_avg_seconds",
				Help:        "Average latency to peer node in seconds",
				ConstLabels: labels,
			},
			[]string{"node", "peer_node", "peer_ip"},
		),

		PeerReachable: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace:   namespace,
				Subsystem:   subsystem,
				Name:        "peer_reachable",
				Help:        "Whether peer node is reachable (1 = reachable, 0 = unreachable)",
				ConstLabels: labels,
			},
			[]string{"node", "peer_node", "peer_ip"},
		),

		PeersTotal: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace:   namespace,
				Subsystem:   subsystem,
				Name:        "peers_total",
				Help:        "Total number of discovered peer nodes",
				ConstLabels: labels,
			},
			[]string{"node"},
		),

		PeersReachableTotal: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace:   namespace,
				Subsystem:   subsystem,
				Name:        "peers_reachable_total",
				Help:        "Number of reachable peer nodes",
				ConstLabels: labels,
			},
			[]string{"node"},
		),

		DNSLatencySeconds: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace:   namespace,
				Subsystem:   subsystem,
				Name:        "dns_latency_seconds",
				Help:        "DNS resolution latency in seconds",
				ConstLabels: labels,
			},
			[]string{"node", "dns_server", "domain", "record_type"},
		),

		APIServerLatencySeconds: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace:   namespace,
				Subsystem:   subsystem,
				Name:        "apiserver_latency_seconds",
				Help:        "Kubernetes API server response latency in seconds",
				ConstLabels: labels,
			},
			[]string{"node"},
		),

		// Histogram metrics
		MonitorCheckDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace:   namespace,
				Subsystem:   subsystem,
				Name:        "monitor_check_duration_seconds",
				Help:        "Duration of monitor checks in seconds",
				ConstLabels: labels,
				Buckets:     prometheus.DefBuckets,
			},
			[]string{"node", "monitor_name"},
		),

		ExportDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace:   namespace,
				Subsystem:   subsystem,
				Name:        "export_duration_seconds",
				Help:        "Duration of export operations in seconds",
				ConstLabels: labels,
				Buckets:     prometheus.DefBuckets,
			},
			[]string{"node", "exporter", "operation"},
		),

		GatewayLatencyHistogram: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace:   namespace,
				Subsystem:   subsystem,
				Name:        "gateway_latency_histogram_seconds",
				Help:        "Distribution of gateway latency in seconds",
				ConstLabels: labels,
				Buckets:     []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0},
			},
			[]string{"node", "gateway_ip"},
		),

		PeerLatencyHistogram: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace:   namespace,
				Subsystem:   subsystem,
				Name:        "peer_latency_histogram_seconds",
				Help:        "Distribution of peer node latency in seconds",
				ConstLabels: labels,
				Buckets:     []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0},
			},
			[]string{"node", "peer_node"},
		),

		DNSLatencyHistogram: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace:   namespace,
				Subsystem:   subsystem,
				Name:        "dns_latency_histogram_seconds",
				Help:        "Distribution of DNS resolution latency in seconds",
				ConstLabels: labels,
				Buckets:     []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.0},
			},
			[]string{"node", "domain_type"},
		),

		APIServerLatencyHistogram: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace:   namespace,
				Subsystem:   subsystem,
				Name:        "apiserver_latency_histogram_seconds",
				Help:        "Distribution of API server response latency in seconds",
				ConstLabels: labels,
				Buckets:     []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0},
			},
			[]string{"node"},
		),
	}

	return m, nil
}

// Register registers all metrics with the provided registry
func (m *Metrics) Register(registry *prometheus.Registry) error {
	collectors := []prometheus.Collector{
		m.ProblemsTotal,
		m.StatusUpdatesTotal,
		m.EventsTotal,
		m.ConditionsTotal,
		m.ExportOperationsTotal,
		m.ExportErrorsTotal,
		m.ProblemsActive,
		m.MonitorUp,
		m.ConditionStatus,
		m.Info,
		m.StartTimeSeconds,
		m.UptimeSeconds,
		m.MonitorCheckDuration,
		m.ExportDuration,
		// Network latency metrics
		m.GatewayLatencySeconds,
		m.PeerLatencySeconds,
		m.PeerLatencyAvgSeconds,
		m.PeerReachable,
		m.PeersTotal,
		m.PeersReachableTotal,
		m.DNSLatencySeconds,
		m.APIServerLatencySeconds,
		m.GatewayLatencyHistogram,
		m.PeerLatencyHistogram,
		m.DNSLatencyHistogram,
		m.APIServerLatencyHistogram,
	}

	for _, collector := range collectors {
		if err := registry.Register(collector); err != nil {
			return fmt.Errorf("failed to register metric: %w", err)
		}
	}

	return nil
}

// Unregister removes all metrics from the provided registry
func (m *Metrics) Unregister(registry *prometheus.Registry) {
	collectors := []prometheus.Collector{
		m.ProblemsTotal,
		m.StatusUpdatesTotal,
		m.EventsTotal,
		m.ConditionsTotal,
		m.ExportOperationsTotal,
		m.ExportErrorsTotal,
		m.ProblemsActive,
		m.MonitorUp,
		m.ConditionStatus,
		m.Info,
		m.StartTimeSeconds,
		m.UptimeSeconds,
		m.MonitorCheckDuration,
		m.ExportDuration,
		// Network latency metrics
		m.GatewayLatencySeconds,
		m.PeerLatencySeconds,
		m.PeerLatencyAvgSeconds,
		m.PeerReachable,
		m.PeersTotal,
		m.PeersReachableTotal,
		m.DNSLatencySeconds,
		m.APIServerLatencySeconds,
		m.GatewayLatencyHistogram,
		m.PeerLatencyHistogram,
		m.DNSLatencyHistogram,
		m.APIServerLatencyHistogram,
	}

	for _, collector := range collectors {
		registry.Unregister(collector)
	}
}
