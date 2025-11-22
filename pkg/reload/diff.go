package reload

import (
	"fmt"

	"github.com/supporttools/node-doctor/pkg/types"
)

// ConfigDiff represents the differences between two configurations.
type ConfigDiff struct {
	MonitorsAdded      []types.MonitorConfig
	MonitorsRemoved    []types.MonitorConfig
	MonitorsModified   []MonitorChange
	ExportersChanged   bool
	RemediationChanged bool
}

// MonitorChange represents a modification to a monitor configuration.
type MonitorChange struct {
	Old types.MonitorConfig
	New types.MonitorConfig
}

// ComputeConfigDiff calculates the differences between old and new configurations.
func ComputeConfigDiff(oldConfig, newConfig *types.NodeDoctorConfig) *ConfigDiff {
	diff := &ConfigDiff{
		MonitorsAdded:    make([]types.MonitorConfig, 0),
		MonitorsRemoved:  make([]types.MonitorConfig, 0),
		MonitorsModified: make([]MonitorChange, 0),
	}

	// Build maps for efficient lookup
	oldMonitors := makeMonitorMap(oldConfig.Monitors)
	newMonitors := makeMonitorMap(newConfig.Monitors)

	// Find added and modified monitors
	for name, newMon := range newMonitors {
		if oldMon, exists := oldMonitors[name]; !exists {
			// Monitor added
			diff.MonitorsAdded = append(diff.MonitorsAdded, newMon)
		} else if !monitorsEqual(oldMon, newMon) {
			// Monitor modified
			diff.MonitorsModified = append(diff.MonitorsModified, MonitorChange{
				Old: oldMon,
				New: newMon,
			})
		}
	}

	// Find removed monitors
	for name, oldMon := range oldMonitors {
		if _, exists := newMonitors[name]; !exists {
			diff.MonitorsRemoved = append(diff.MonitorsRemoved, oldMon)
		}
	}

	// Check exporter changes
	diff.ExportersChanged = !exportersEqual(&oldConfig.Exporters, &newConfig.Exporters)

	// Check remediation changes
	diff.RemediationChanged = !remediationEqual(&oldConfig.Remediation, &newConfig.Remediation)

	return diff
}

// HasChanges returns true if there are any configuration changes.
func (d *ConfigDiff) HasChanges() bool {
	return len(d.MonitorsAdded) > 0 ||
		len(d.MonitorsRemoved) > 0 ||
		len(d.MonitorsModified) > 0 ||
		d.ExportersChanged ||
		d.RemediationChanged
}

// makeMonitorMap creates a map of monitor name -> monitor config.
func makeMonitorMap(monitors []types.MonitorConfig) map[string]types.MonitorConfig {
	m := make(map[string]types.MonitorConfig)
	for _, mon := range monitors {
		m[mon.Name] = mon
	}
	return m
}

// monitorsEqual checks if two monitor configurations are equal.
func monitorsEqual(a, b types.MonitorConfig) bool {
	// Compare basic fields
	if a.Name != b.Name ||
		a.Type != b.Type ||
		a.Enabled != b.Enabled ||
		a.Interval != b.Interval ||
		a.Timeout != b.Timeout {
		return false
	}

	// Compare config maps
	if !configMapsEqual(a.Config, b.Config) {
		return false
	}

	return true
}

// configMapsEqual compares two config maps for equality.
func configMapsEqual(a, b map[string]interface{}) bool {
	if len(a) != len(b) {
		return false
	}

	for key, aVal := range a {
		bVal, exists := b[key]
		if !exists {
			return false
		}

		// Simple comparison (works for primitives and strings)
		// For complex nested structures, would need deep comparison
		if fmt.Sprint(aVal) != fmt.Sprint(bVal) {
			return false
		}
	}

	return true
}

// exportersEqual checks if exporter configurations are equal.
func exportersEqual(a, b *types.ExporterConfigs) bool {
	// Handle nil pointers
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	// Kubernetes exporter
	if (a.Kubernetes == nil) != (b.Kubernetes == nil) {
		return false
	}
	if a.Kubernetes != nil && b.Kubernetes != nil {
		if a.Kubernetes.Enabled != b.Kubernetes.Enabled ||
			a.Kubernetes.UpdateInterval != b.Kubernetes.UpdateInterval {
			return false
		}
	}

	// Prometheus exporter
	if (a.Prometheus == nil) != (b.Prometheus == nil) {
		return false
	}
	if a.Prometheus != nil && b.Prometheus != nil {
		if a.Prometheus.Enabled != b.Prometheus.Enabled ||
			a.Prometheus.Port != b.Prometheus.Port ||
			a.Prometheus.Path != b.Prometheus.Path {
			return false
		}
	}

	// HTTP exporter
	if (a.HTTP == nil) != (b.HTTP == nil) {
		return false
	}
	if a.HTTP != nil && b.HTTP != nil {
		if a.HTTP.Enabled != b.HTTP.Enabled ||
			a.HTTP.Workers != b.HTTP.Workers ||
			a.HTTP.QueueSize != b.HTTP.QueueSize ||
			a.HTTP.Timeout != b.HTTP.Timeout ||
			len(a.HTTP.Webhooks) != len(b.HTTP.Webhooks) {
			return false
		}
		// Compare webhook contents
		for i := range a.HTTP.Webhooks {
			if !webhookEqual(&a.HTTP.Webhooks[i], &b.HTTP.Webhooks[i]) {
				return false
			}
		}
	}

	return true
}

// webhookEqual checks if two webhook endpoints are equal.
func webhookEqual(a, b *types.WebhookEndpoint) bool {
	return a.Name == b.Name &&
		a.URL == b.URL &&
		a.SendStatus == b.SendStatus &&
		a.SendProblems == b.SendProblems
}

// remediationEqual checks if remediation configurations are equal.
func remediationEqual(a, b *types.RemediationConfig) bool {
	// Handle nil pointers
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	return a.Enabled == b.Enabled &&
		a.DryRun == b.DryRun &&
		a.MaxRemediationsPerHour == b.MaxRemediationsPerHour &&
		circuitBreakerEqual(&a.CircuitBreaker, &b.CircuitBreaker)
}

// circuitBreakerEqual checks if circuit breaker configurations are equal.
func circuitBreakerEqual(a, b *types.CircuitBreakerConfig) bool {
	return a.Threshold == b.Threshold &&
		a.Timeout == b.Timeout &&
		a.SuccessThreshold == b.SuccessThreshold
}
