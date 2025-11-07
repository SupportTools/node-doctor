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
	Info             *prometheus.GaugeVec
	StartTimeSeconds *prometheus.GaugeVec
	UptimeSeconds    *prometheus.GaugeVec

	// Histogram metrics
	MonitorCheckDuration *prometheus.HistogramVec
	ExportDuration       *prometheus.HistogramVec
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
		m.Info,
		m.StartTimeSeconds,
		m.UptimeSeconds,
		m.MonitorCheckDuration,
		m.ExportDuration,
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
		m.Info,
		m.StartTimeSeconds,
		m.UptimeSeconds,
		m.MonitorCheckDuration,
		m.ExportDuration,
	}

	for _, collector := range collectors {
		registry.Unregister(collector)
	}
}
