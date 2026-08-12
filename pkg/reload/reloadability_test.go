package reload

import (
	"strings"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

func baseConfig() *types.NodeDoctorConfig {
	return &types.NodeDoctorConfig{
		APIVersion: "v1",
		Kind:       "NodeDoctorConfig",
		Metadata:   types.ConfigMetadata{Name: "node-doctor"},
		Settings: types.GlobalSettings{
			NodeName:  "node-a",
			LogLevel:  "info",
			LogFormat: "json",
			LogOutput: "stdout",
		},
		Exporters: types.ExporterConfigs{
			Prometheus: &types.PrometheusExporterConfig{Enabled: true, Port: 9100},
			Kubernetes: &types.KubernetesExporterConfig{Enabled: true},
		},
		Remediation: types.RemediationConfig{Enabled: true},
	}
}

func TestClassifyReload_NodeNameChangeRequiresRestart(t *testing.T) {
	oldCfg := baseConfig()
	newCfg := baseConfig()
	newCfg.Settings.NodeName = "node-b"

	r := ClassifyReload(oldCfg, newCfg, nil)

	if !r.HasRestartRequired() {
		t.Fatal("changing settings.nodeName must be reported as restart-required, not silently ignored")
	}
	if !strings.Contains(strings.Join(r.RestartRequired, " "), "settings.nodeName") {
		t.Errorf("restart-required list should name the field, got %v", r.RestartRequired)
	}
}

func TestClassifyReload_LogDestinationRequiresRestartButLevelDoesNot(t *testing.T) {
	// Level-only change: hot-reloadable, so nothing should demand a restart.
	oldCfg := baseConfig()
	newCfg := baseConfig()
	newCfg.Settings.LogLevel = "debug"

	if r := ClassifyReload(oldCfg, newCfg, nil); r.HasRestartRequired() {
		t.Errorf("a log LEVEL change is hot-reloadable; should not demand a restart, got %v", r.RestartRequired)
	}

	// Destination change: the file handle is opened at startup.
	newCfg2 := baseConfig()
	newCfg2.Settings.LogOutput = "file"
	newCfg2.Settings.LogFile = "/var/log/nd.log"

	r2 := ClassifyReload(oldCfg, newCfg2, nil)
	if !r2.HasRestartRequired() {
		t.Fatal("changing the log destination must be reported as restart-required")
	}
}

func TestClassifyReload_EnablingDisabledExporterRequiresRestart(t *testing.T) {
	oldCfg := baseConfig()
	oldCfg.Exporters.HTTP = &types.HTTPExporterConfig{Enabled: false}
	newCfg := baseConfig()
	newCfg.Exporters.HTTP = &types.HTTPExporterConfig{Enabled: true}

	r := ClassifyReload(oldCfg, newCfg, nil)

	// The exporter was never constructed at startup, so there is no instance for
	// reloadExporter to hand the new config to.
	joined := strings.Join(r.RestartRequired, " ")
	if !strings.Contains(joined, "exporters.http.enabled") {
		t.Errorf("enabling a previously-disabled exporter must be restart-required, got %v", r.RestartRequired)
	}
}

func TestClassifyReload_EnablingRemediationRequiresRestart(t *testing.T) {
	oldCfg := baseConfig()
	oldCfg.Remediation.Enabled = false
	newCfg := baseConfig()
	newCfg.Remediation.Enabled = true

	r := ClassifyReload(oldCfg, newCfg, nil)

	if !strings.Contains(strings.Join(r.RestartRequired, " "), "remediation.enabled") {
		t.Errorf("enabling remediation must be restart-required (registry wired at startup), got %v", r.RestartRequired)
	}
}

func TestClassifyReload_CoordinationChangeRequiresRestart(t *testing.T) {
	oldCfg := baseConfig()
	oldCfg.Remediation.Coordination = &types.RemediationCoordinationConfig{
		Enabled: true, ControllerURL: "http://a", LeaseTimeout: time.Minute,
	}
	newCfg := baseConfig()
	newCfg.Remediation.Coordination = &types.RemediationCoordinationConfig{
		Enabled: true, ControllerURL: "http://b", LeaseTimeout: time.Minute,
	}

	r := ClassifyReload(oldCfg, newCfg, nil)

	if !strings.Contains(strings.Join(r.RestartRequired, " "), "controllerURL") {
		t.Errorf("changing the controller URL must be restart-required (lease client wired at startup), got %v", r.RestartRequired)
	}
}

func TestClassifyReload_IdenticalConfigNeedsNothing(t *testing.T) {
	r := ClassifyReload(baseConfig(), baseConfig(), nil)
	if r.HasRestartRequired() {
		t.Errorf("identical configs must not demand a restart, got %v", r.RestartRequired)
	}
	if r.HasHotChanges() {
		t.Error("identical configs must not report hot changes")
	}
}

func TestReloadabilitySummaryNamesReconfiguredMonitors(t *testing.T) {
	// The ticket explicitly requires a log line naming which monitors were
	// reconfigured, so an operator can confirm their ConfigMap edit landed.
	diff := &ConfigDiff{
		MonitorsModified: []MonitorChange{
			{New: types.MonitorConfig{Name: "dns-health"}},
		},
		MonitorsAdded:   []types.MonitorConfig{{Name: "new-mon"}},
		MonitorsRemoved: []types.MonitorConfig{{Name: "old-mon"}},
	}
	r := ClassifyReload(baseConfig(), baseConfig(), diff)
	summary := r.Summary()

	for _, want := range []string{"dns-health", "new-mon", "old-mon", "reconfigured"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary %q must mention %q", summary, want)
		}
	}
}

func TestReloadabilitySummaryFlagsRestartRequired(t *testing.T) {
	oldCfg := baseConfig()
	newCfg := baseConfig()
	newCfg.Settings.NodeName = "node-b"

	summary := ClassifyReload(oldCfg, newCfg, nil).Summary()

	if !strings.Contains(summary, "RESTART REQUIRED") {
		t.Errorf("summary must shout about restart-required changes, got %q", summary)
	}
}

func TestClassifyReload_NilConfigsAreSafe(t *testing.T) {
	if r := ClassifyReload(nil, nil, nil); r == nil || r.HasRestartRequired() {
		t.Error("nil configs must produce an empty, non-nil classification")
	}
	var nilR *Reloadability
	if nilR.HasRestartRequired() || nilR.HasHotChanges() {
		t.Error("nil Reloadability must be safe to query")
	}
	if nilR.Summary() == "" {
		t.Error("nil Reloadability must still render a summary")
	}
}
