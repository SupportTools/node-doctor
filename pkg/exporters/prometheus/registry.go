package prometheus

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// NewRegistry creates a new Prometheus registry with the given constant labels
// This registry is separate from the default global registry to avoid conflicts
func NewRegistry(constLabels prometheus.Labels) *prometheus.Registry {
	registry := prometheus.NewRegistry()

	// Add Go runtime metrics
	registry.MustRegister(collectors.NewGoCollector())

	// Add process metrics
	registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	return registry
}
