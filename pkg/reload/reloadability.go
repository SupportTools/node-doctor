package reload

import (
	"fmt"
	"sort"
	"strings"

	"github.com/supporttools/node-doctor/pkg/types"
)

// Reloadability classifies a ConfigDiff into the changes the running agent can
// genuinely re-initialize in place versus the changes that are latched at
// process startup and therefore require a pod rollout.
//
// This exists because the failure mode it guards against is SILENT STALENESS:
// before it, a ConfigMap edit that the agent could not actually apply produced
// a cheerful "reload succeeded" event while the running component kept its old
// behaviour, and the only way an operator discovered the difference was by
// noticing the alert never stopped firing. An honest "this change needs a
// rollout" is an acceptable outcome; pretending it was applied is not.
type Reloadability struct {
	// MonitorsAdded/Removed/Modified are monitor NAMES the detector will
	// create, stop, and stop-then-recreate respectively. All three are true
	// hot reloads: monitor instances are rebuilt from the new config.
	MonitorsAdded    []string
	MonitorsRemoved  []string
	MonitorsModified []string

	// ExportersReconfigured is true when exporter config changed and the
	// running exporters implement types.ReloadableExporter (all built-in
	// exporters do, including rebinding their listener on a port change).
	ExportersReconfigured bool

	// RemediationReconfigured is true when remediation settings changed and
	// the running remediator registry can adopt them in place (dry-run,
	// rate limits, circuit breaker).
	RemediationReconfigured bool

	// RestartRequired lists changes that were detected but CANNOT be applied
	// to the running process. Each entry is "field: reason". A non-empty list
	// must be surfaced to the operator as a warning — it means the on-disk
	// config and the running behaviour have legitimately diverged.
	RestartRequired []string
}

// ClassifyReload compares the previously-active config against the newly-loaded
// one and reports what a reload can and cannot apply.
//
// oldConfig and newConfig must both be fully normalized (defaults applied) so
// that classification compares like with like; diff may be nil, in which case
// only the process-latched settings are examined.
func ClassifyReload(oldConfig, newConfig *types.NodeDoctorConfig, diff *ConfigDiff) *Reloadability {
	r := &Reloadability{}
	if oldConfig == nil || newConfig == nil {
		return r
	}

	if diff != nil {
		for _, m := range diff.MonitorsAdded {
			r.MonitorsAdded = append(r.MonitorsAdded, m.Name)
		}
		for _, m := range diff.MonitorsRemoved {
			r.MonitorsRemoved = append(r.MonitorsRemoved, m.Name)
		}
		for _, m := range diff.MonitorsModified {
			r.MonitorsModified = append(r.MonitorsModified, m.New.Name)
		}
		sort.Strings(r.MonitorsAdded)
		sort.Strings(r.MonitorsRemoved)
		sort.Strings(r.MonitorsModified)
		r.ExportersReconfigured = diff.ExportersChanged
		r.RemediationReconfigured = diff.RemediationChanged
	}

	// --- Settings latched at process startup -------------------------------

	// nodeName is baked into every exported condition, event and metric label,
	// and into the Kubernetes exporter's node client. Changing it live would
	// leave conditions stranded on the old node object.
	if oldConfig.Settings.NodeName != newConfig.Settings.NodeName {
		r.RestartRequired = append(r.RestartRequired,
			fmt.Sprintf("settings.nodeName (%q -> %q): node identity is bound at startup",
				oldConfig.Settings.NodeName, newConfig.Settings.NodeName))
	}

	// The logging DESTINATION is opened once at startup. Level and format are
	// re-applied on reload (see logger re-init in the detector), but switching
	// stdout->file or changing the file path needs a fresh process.
	if oldConfig.Settings.LogOutput != newConfig.Settings.LogOutput {
		r.RestartRequired = append(r.RestartRequired,
			fmt.Sprintf("settings.logOutput (%q -> %q): log destination is opened at startup",
				oldConfig.Settings.LogOutput, newConfig.Settings.LogOutput))
	}
	if oldConfig.Settings.LogFile != newConfig.Settings.LogFile {
		r.RestartRequired = append(r.RestartRequired,
			fmt.Sprintf("settings.logFile (%q -> %q): log file handle is opened at startup",
				oldConfig.Settings.LogFile, newConfig.Settings.LogFile))
	}

	// pprof listener is started (or not) once, from the startup config.
	if oldConfig.Features.EnableProfiling != newConfig.Features.EnableProfiling {
		r.RestartRequired = append(r.RestartRequired,
			fmt.Sprintf("features.enableProfiling (%v -> %v): the pprof listener is started at startup",
				oldConfig.Features.EnableProfiling, newConfig.Features.EnableProfiling))
	}
	if newConfig.Features.EnableProfiling && oldConfig.Features.ProfilingPort != newConfig.Features.ProfilingPort {
		r.RestartRequired = append(r.RestartRequired,
			fmt.Sprintf("features.profilingPort (%d -> %d): the pprof listener is bound at startup",
				oldConfig.Features.ProfilingPort, newConfig.Features.ProfilingPort))
	}

	// An exporter that was DISABLED at startup was never constructed, so there
	// is no instance for reloadExporter to hand the new config to. Enabling it
	// therefore needs a rollout. (Disabling a running exporter is handled by
	// its own Reload.)
	classifyExporterEnable(oldConfig, newConfig, r)

	// Remediation as a whole is wired at startup: when disabled, no registry,
	// no remediator strategies and no cluster client exist to reconfigure.
	if !oldConfig.Remediation.Enabled && newConfig.Remediation.Enabled {
		r.RestartRequired = append(r.RestartRequired,
			"remediation.enabled (false -> true): the remediator registry and cluster client are wired at startup")
	}

	// The controller lease client is constructed once from coordination config.
	classifyCoordination(oldConfig, newConfig, r)

	return r
}

// classifyExporterEnable appends a restart-required entry for each exporter
// that transitions from disabled to enabled, since no instance exists to reload.
func classifyExporterEnable(oldConfig, newConfig *types.NodeDoctorConfig, r *Reloadability) {
	type exp struct {
		name       string
		oldEnabled bool
		newEnabled bool
	}
	exps := []exp{
		{"exporters.kubernetes", exporterEnabled(oldConfig.Exporters.Kubernetes != nil, func() bool { return oldConfig.Exporters.Kubernetes.Enabled }),
			exporterEnabled(newConfig.Exporters.Kubernetes != nil, func() bool { return newConfig.Exporters.Kubernetes.Enabled })},
		{"exporters.http", exporterEnabled(oldConfig.Exporters.HTTP != nil, func() bool { return oldConfig.Exporters.HTTP.Enabled }),
			exporterEnabled(newConfig.Exporters.HTTP != nil, func() bool { return newConfig.Exporters.HTTP.Enabled })},
		{"exporters.prometheus", exporterEnabled(oldConfig.Exporters.Prometheus != nil, func() bool { return oldConfig.Exporters.Prometheus.Enabled }),
			exporterEnabled(newConfig.Exporters.Prometheus != nil, func() bool { return newConfig.Exporters.Prometheus.Enabled })},
	}
	for _, e := range exps {
		if !e.oldEnabled && e.newEnabled {
			r.RestartRequired = append(r.RestartRequired,
				fmt.Sprintf("%s.enabled (false -> true): the exporter is constructed at startup, so there is no instance to reload", e.name))
		}
	}
}

// exporterEnabled guards a nil exporter config block.
func exporterEnabled(present bool, enabled func() bool) bool {
	if !present {
		return false
	}
	return enabled()
}

// classifyCoordination flags lease-client changes, which are wired once at startup.
func classifyCoordination(oldConfig, newConfig *types.NodeDoctorConfig, r *Reloadability) {
	oldCoord := oldConfig.Remediation.Coordination
	newCoord := newConfig.Remediation.Coordination
	oldEnabled := oldCoord != nil && oldCoord.Enabled
	newEnabled := newCoord != nil && newCoord.Enabled

	if oldEnabled != newEnabled {
		r.RestartRequired = append(r.RestartRequired,
			fmt.Sprintf("remediation.coordination.enabled (%v -> %v): the controller lease client is wired at startup",
				oldEnabled, newEnabled))
		return
	}
	if oldEnabled && newEnabled && oldCoord.ControllerURL != newCoord.ControllerURL {
		r.RestartRequired = append(r.RestartRequired,
			fmt.Sprintf("remediation.coordination.controllerURL (%q -> %q): the controller lease client is wired at startup",
				oldCoord.ControllerURL, newCoord.ControllerURL))
	}
}

// HasRestartRequired reports whether any detected change cannot be hot-applied.
func (r *Reloadability) HasRestartRequired() bool {
	return r != nil && len(r.RestartRequired) > 0
}

// HasHotChanges reports whether anything at all will be re-initialized in place.
func (r *Reloadability) HasHotChanges() bool {
	if r == nil {
		return false
	}
	return len(r.MonitorsAdded) > 0 || len(r.MonitorsRemoved) > 0 || len(r.MonitorsModified) > 0 ||
		r.ExportersReconfigured || r.RemediationReconfigured
}

// Summary renders a single operator-facing line naming exactly which components
// were reconfigured, and which changes still need a rollout. This is the log
// line an operator greps for after `kubectl patch cm` to confirm the edit
// actually took effect.
func (r *Reloadability) Summary() string {
	if r == nil {
		return "config reload: nothing to apply"
	}

	parts := make([]string, 0, 6)
	if len(r.MonitorsModified) > 0 {
		parts = append(parts, "monitors reconfigured=["+strings.Join(r.MonitorsModified, " ")+"]")
	}
	if len(r.MonitorsAdded) > 0 {
		parts = append(parts, "monitors started=["+strings.Join(r.MonitorsAdded, " ")+"]")
	}
	if len(r.MonitorsRemoved) > 0 {
		parts = append(parts, "monitors stopped=["+strings.Join(r.MonitorsRemoved, " ")+"]")
	}
	if r.ExportersReconfigured {
		parts = append(parts, "exporters reconfigured")
	}
	if r.RemediationReconfigured {
		parts = append(parts, "remediation reconfigured")
	}

	msg := "config reload applied: "
	if len(parts) == 0 {
		msg += "no hot-reloadable changes"
	} else {
		msg += strings.Join(parts, "; ")
	}

	if len(r.RestartRequired) > 0 {
		msg += fmt.Sprintf(" | RESTART REQUIRED for %d change(s) that cannot be applied to the running process: %s",
			len(r.RestartRequired), strings.Join(r.RestartRequired, "; "))
	}

	return msg
}
