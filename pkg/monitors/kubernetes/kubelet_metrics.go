// Package kubernetes provides Kubernetes component health monitoring capabilities.
package kubernetes

import (
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
)

// KubeletMonitorMetrics holds Prometheus metrics for kubelet monitor
type KubeletMonitorMetrics struct {
	// Check duration histograms
	CheckDuration        *prometheus.HistogramVec
	HealthCheckDuration  *prometheus.HistogramVec
	SystemdCheckDuration *prometheus.HistogramVec
	PLEGCheckDuration    *prometheus.HistogramVec

	// Result counters
	ChecksTotal *prometheus.CounterVec

	// Failure tracking
	ConsecutiveFailures *prometheus.GaugeVec

	// Circuit breaker metrics
	CircuitBreakerState       *prometheus.GaugeVec
	CircuitBreakerTransitions *prometheus.CounterVec
	CircuitBreakerOpenings    *prometheus.CounterVec
	CircuitBreakerRecoveries  *prometheus.CounterVec
	CircuitBreakerBackoffMult *prometheus.GaugeVec

	// PLEG metrics
	PLEGRelistDuration  *prometheus.GaugeVec
	PLEGParsingDuration *prometheus.HistogramVec
}

// NewKubeletMonitorMetrics creates and registers all kubelet monitor metrics
func NewKubeletMonitorMetrics(registry *prometheus.Registry) (*KubeletMonitorMetrics, error) {
	if registry == nil {
		return nil, fmt.Errorf("registry cannot be nil")
	}

	// Define histogram buckets according to design document
	checkDurationBuckets := []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0}
	healthSystemdPLEGBuckets := []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5}
	plegParsingBuckets := []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.025, 0.05}

	metrics := &KubeletMonitorMetrics{
		// Overall check duration histogram
		CheckDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "node_doctor_kubelet_monitor_check_duration_seconds",
				Help:    "Duration of kubelet monitor check execution in seconds",
				Buckets: checkDurationBuckets,
			},
			[]string{"node", "monitor_name", "result"},
		),

		// Health check duration histogram
		HealthCheckDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "node_doctor_kubelet_monitor_health_check_duration_seconds",
				Help:    "Duration of kubelet /healthz endpoint check in seconds",
				Buckets: healthSystemdPLEGBuckets,
			},
			[]string{"node", "monitor_name", "result"},
		),

		// Systemd check duration histogram
		SystemdCheckDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "node_doctor_kubelet_monitor_systemd_check_duration_seconds",
				Help:    "Duration of kubelet systemd status check in seconds",
				Buckets: healthSystemdPLEGBuckets,
			},
			[]string{"node", "monitor_name", "result"},
		),

		// PLEG check duration histogram
		PLEGCheckDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "node_doctor_kubelet_monitor_pleg_check_duration_seconds",
				Help:    "Duration of PLEG metrics retrieval and parsing in seconds",
				Buckets: healthSystemdPLEGBuckets,
			},
			[]string{"node", "monitor_name", "result"},
		),

		// Check results total counter
		ChecksTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "node_doctor_kubelet_monitor_checks_total",
				Help: "Total number of kubelet monitor checks performed",
			},
			[]string{"node", "monitor_name", "check_type", "result"},
		),

		// Consecutive failures gauge
		ConsecutiveFailures: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "node_doctor_kubelet_monitor_consecutive_failures",
				Help: "Current count of consecutive failures",
			},
			[]string{"node", "monitor_name"},
		),

		// Circuit breaker state gauge
		CircuitBreakerState: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "node_doctor_kubelet_monitor_circuit_breaker_state",
				Help: "Circuit breaker state (0=closed, 1=half_open, 2=open, -1=disabled)",
			},
			[]string{"node", "monitor_name"},
		),

		// Circuit breaker state transitions counter
		CircuitBreakerTransitions: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "node_doctor_kubelet_monitor_circuit_breaker_transitions_total",
				Help: "Total number of circuit breaker state transitions",
			},
			[]string{"node", "monitor_name", "from_state", "to_state"},
		),

		// Circuit breaker openings counter
		CircuitBreakerOpenings: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "node_doctor_kubelet_monitor_circuit_breaker_openings_total",
				Help: "Total number of times circuit breaker has opened",
			},
			[]string{"node", "monitor_name"},
		),

		// Circuit breaker recoveries counter
		CircuitBreakerRecoveries: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "node_doctor_kubelet_monitor_circuit_breaker_recoveries_total",
				Help: "Total number of successful circuit breaker recoveries",
			},
			[]string{"node", "monitor_name"},
		),

		// Circuit breaker backoff multiplier gauge
		CircuitBreakerBackoffMult: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "node_doctor_kubelet_monitor_circuit_breaker_backoff_multiplier",
				Help: "Current exponential backoff multiplier for circuit breaker",
			},
			[]string{"node", "monitor_name"},
		),

		// PLEG relist duration gauge
		PLEGRelistDuration: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "node_doctor_kubelet_monitor_pleg_relist_duration_seconds",
				Help: "Most recent PLEG relist duration observed from kubelet metrics",
			},
			[]string{"node", "monitor_name"},
		),

		// PLEG parsing duration histogram
		PLEGParsingDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "node_doctor_kubelet_monitor_pleg_parsing_duration_seconds",
				Help:    "Duration of PLEG metrics parsing in seconds",
				Buckets: plegParsingBuckets,
			},
			[]string{"node", "monitor_name"},
		),
	}

	// Register all metrics
	collectors := []prometheus.Collector{
		metrics.CheckDuration,
		metrics.HealthCheckDuration,
		metrics.SystemdCheckDuration,
		metrics.PLEGCheckDuration,
		metrics.ChecksTotal,
		metrics.ConsecutiveFailures,
		metrics.CircuitBreakerState,
		metrics.CircuitBreakerTransitions,
		metrics.CircuitBreakerOpenings,
		metrics.CircuitBreakerRecoveries,
		metrics.CircuitBreakerBackoffMult,
		metrics.PLEGRelistDuration,
		metrics.PLEGParsingDuration,
	}

	for _, collector := range collectors {
		if err := registry.Register(collector); err != nil {
			return nil, err
		}
	}

	return metrics, nil
}

// RecordCheckDuration records the duration of an overall check execution
func (m *KubeletMonitorMetrics) RecordCheckDuration(node, monitorName, result string, duration float64) {
	if m == nil || m.CheckDuration == nil {
		return
	}
	m.CheckDuration.WithLabelValues(node, monitorName, result).Observe(duration)
}

// RecordHealthCheck records the duration and result of a health check
func (m *KubeletMonitorMetrics) RecordHealthCheck(node, monitorName, result string, duration float64) {
	if m == nil || m.HealthCheckDuration == nil {
		return
	}
	m.HealthCheckDuration.WithLabelValues(node, monitorName, result).Observe(duration)
}

// RecordSystemdCheck records the duration and result of a systemd check
func (m *KubeletMonitorMetrics) RecordSystemdCheck(node, monitorName, result string, duration float64) {
	if m == nil || m.SystemdCheckDuration == nil {
		return
	}
	m.SystemdCheckDuration.WithLabelValues(node, monitorName, result).Observe(duration)
}

// RecordPLEGCheck records the duration and result of a PLEG check
func (m *KubeletMonitorMetrics) RecordPLEGCheck(node, monitorName, result string, duration float64) {
	if m == nil || m.PLEGCheckDuration == nil {
		return
	}
	m.PLEGCheckDuration.WithLabelValues(node, monitorName, result).Observe(duration)
}

// RecordCheckResult records the result of a specific check type
func (m *KubeletMonitorMetrics) RecordCheckResult(node, monitorName, checkType, result string) {
	if m == nil || m.ChecksTotal == nil {
		return
	}
	m.ChecksTotal.WithLabelValues(node, monitorName, checkType, result).Inc()
}

// SetConsecutiveFailures sets the current count of consecutive failures
func (m *KubeletMonitorMetrics) SetConsecutiveFailures(node, monitorName string, count int) {
	if m == nil || m.ConsecutiveFailures == nil {
		return
	}
	m.ConsecutiveFailures.WithLabelValues(node, monitorName).Set(float64(count))
}

// SetCircuitBreakerState sets the circuit breaker state
// state values: -1=disabled, 0=closed, 1=half_open, 2=open
func (m *KubeletMonitorMetrics) SetCircuitBreakerState(node, monitorName string, state int) {
	if m == nil || m.CircuitBreakerState == nil {
		return
	}
	m.CircuitBreakerState.WithLabelValues(node, monitorName).Set(float64(state))
}

// RecordCircuitBreakerTransition records a state transition
func (m *KubeletMonitorMetrics) RecordCircuitBreakerTransition(node, monitorName, fromState, toState string) {
	if m == nil || m.CircuitBreakerTransitions == nil {
		return
	}
	m.CircuitBreakerTransitions.WithLabelValues(node, monitorName, fromState, toState).Inc()
}

// RecordCircuitBreakerOpening records a circuit breaker opening
func (m *KubeletMonitorMetrics) RecordCircuitBreakerOpening(node, monitorName string) {
	if m == nil || m.CircuitBreakerOpenings == nil {
		return
	}
	m.CircuitBreakerOpenings.WithLabelValues(node, monitorName).Inc()
}

// RecordCircuitBreakerRecovery records a successful circuit breaker recovery
func (m *KubeletMonitorMetrics) RecordCircuitBreakerRecovery(node, monitorName string) {
	if m == nil || m.CircuitBreakerRecoveries == nil {
		return
	}
	m.CircuitBreakerRecoveries.WithLabelValues(node, monitorName).Inc()
}

// SetCircuitBreakerBackoffMultiplier sets the current backoff multiplier
func (m *KubeletMonitorMetrics) SetCircuitBreakerBackoffMultiplier(node, monitorName string, multiplier float64) {
	if m == nil || m.CircuitBreakerBackoffMult == nil {
		return
	}
	m.CircuitBreakerBackoffMult.WithLabelValues(node, monitorName).Set(multiplier)
}

// SetPLEGRelistDuration sets the most recent PLEG relist duration observed
func (m *KubeletMonitorMetrics) SetPLEGRelistDuration(node, monitorName string, duration float64) {
	if m == nil || m.PLEGRelistDuration == nil {
		return
	}
	m.PLEGRelistDuration.WithLabelValues(node, monitorName).Set(duration)
}

// RecordPLEGParsingDuration records the duration of PLEG metrics parsing
func (m *KubeletMonitorMetrics) RecordPLEGParsingDuration(node, monitorName string, duration float64) {
	if m == nil || m.PLEGParsingDuration == nil {
		return
	}
	m.PLEGParsingDuration.WithLabelValues(node, monitorName).Observe(duration)
}