package reload

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/supporttools/node-doctor/pkg/types"

	// Blank imports register the real monitor types so the validator accepts them.
	_ "github.com/supporttools/node-doctor/pkg/monitors/network"
	_ "github.com/supporttools/node-doctor/pkg/monitors/system"
)

// configWithMonitors renders a minimal but realistic config file listing only
// the named monitors. It deliberately omits monitor types that the registry
// would auto-add via ApplyDefaultMonitors, which is what makes the asymmetry
// bug reproducible.
func configWithMonitors(clusterDomains string) string {
	return `apiVersion: v1
kind: NodeDoctorConfig
metadata:
  name: node-doctor
settings:
  nodeName: "test-node"
monitors:
  - name: dns-health
    type: network-dns-check
    enabled: true
    interval: 30s
    timeout: 10s
    config:
      clusterDomains: ` + clusterDomains + `
      externalDomains:
        - google.com
exporters:
  prometheus:
    enabled: true
`
}

// captureCoordinator wires a coordinator that records the diff handed to the
// reload callback.
type captureCoordinator struct {
	mu     sync.Mutex
	diffs  []*ConfigDiff
	events []string
}

func (c *captureCoordinator) callback(_ context.Context, _ *types.NodeDoctorConfig, diff *ConfigDiff) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.diffs = append(c.diffs, diff)
	return nil
}

func (c *captureCoordinator) emit(_ types.EventSeverity, reason, message string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, reason+": "+message)
}

func (c *captureCoordinator) lastDiff() *ConfigDiff {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.diffs) == 0 {
		return nil
	}
	return c.diffs[len(c.diffs)-1]
}

// TestReloadWithoutNormalizerDropsDefaultMonitors documents the ORIGINAL bug
// (#node-doctor-243) so the fix cannot be quietly reverted: with no normalizer,
// the freshly-loaded config lacks the monitors that ApplyDefaultMonitors added
// at startup, so the diff reports them as REMOVED and the detector stops them.
func TestReloadWithoutNormalizerDropsDefaultMonitors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(configWithMonitors("[]")), 0o644); err != nil {
		t.Fatal(err)
	}

	startCfg := loadAndNormalize(t, path, true)

	cap := &captureCoordinator{}
	rc := NewReloadCoordinator(path, startCfg, cap.callback, cap.emit)
	// NO normalizer installed — the historical behaviour.

	if err := os.WriteFile(path, []byte(configWithMonitors(`["kubernetes.default.svc.cluster.local"]`)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := rc.TriggerReload(context.Background()); err != nil {
		t.Fatalf("reload failed: %v", err)
	}

	diff := cap.lastDiff()
	if diff == nil {
		t.Fatal("expected the reload callback to run")
	}
	if len(diff.MonitorsRemoved) == 0 {
		t.Skip("registry has no auto-defaultable monitor types absent from this config; nothing to demonstrate")
	}
	t.Logf("without a normalizer the diff spuriously removes %d monitor(s): %v",
		len(diff.MonitorsRemoved), monitorNames(diff.MonitorsRemoved))
}

// TestReloadWithNormalizerPreservesDefaultMonitors is the actual regression
// guard: with the normalizer installed (as main.go does), a ConfigMap edit
// touching ONE monitor must not report any monitor as removed.
func TestReloadWithNormalizerPreservesDefaultMonitors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(configWithMonitors("[]")), 0o644); err != nil {
		t.Fatal(err)
	}

	startCfg := loadAndNormalize(t, path, true)

	cap := &captureCoordinator{}
	rc := NewReloadCoordinator(path, startCfg, cap.callback, cap.emit)
	rc.SetConfigNormalizer(func(c *types.NodeDoctorConfig) error {
		return normalizeForTest(c, true)
	})

	// The operator edits ONLY dns-health.
	if err := os.WriteFile(path, []byte(configWithMonitors(`["kubernetes.default.svc.cluster.local"]`)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := rc.TriggerReload(context.Background()); err != nil {
		t.Fatalf("reload failed: %v", err)
	}

	diff := cap.lastDiff()
	if diff == nil {
		t.Fatal("expected the reload callback to run")
	}

	if len(diff.MonitorsRemoved) != 0 {
		t.Errorf("editing one monitor must not remove any others; got removed=%v. "+
			"This means the reload path and the startup path disagree about the config again.",
			monitorNames(diff.MonitorsRemoved))
	}
	if len(diff.MonitorsAdded) != 0 {
		t.Errorf("editing one monitor must not add any; got added=%v", monitorNames(diff.MonitorsAdded))
	}

	// And the edit itself must be seen.
	if len(diff.MonitorsModified) != 1 || diff.MonitorsModified[0].New.Name != "dns-health" {
		t.Fatalf("expected exactly dns-health to be modified, got %d: %+v",
			len(diff.MonitorsModified), diff.MonitorsModified)
	}

	// The new config must actually carry the operator's value, i.e. the running
	// monitor gets rebuilt from the NEW clusterDomains, not the old one.
	newClusterDomains := diff.MonitorsModified[0].New.Config["clusterDomains"]
	got := strings.TrimSpace(strings.Trim(sprint(newClusterDomains), "[]"))
	if got != "kubernetes.default.svc.cluster.local" {
		t.Errorf("modified monitor must carry the NEW clusterDomains, got %v", newClusterDomains)
	}
}

// TestReloadNormalizerFailurePropagates ensures a broken normalizer fails the
// reload loudly rather than silently applying a half-normalized config.
func TestReloadNormalizerFailurePropagates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(configWithMonitors("[]")), 0o644); err != nil {
		t.Fatal(err)
	}
	startCfg := loadAndNormalize(t, path, true)

	cap := &captureCoordinator{}
	rc := NewReloadCoordinator(path, startCfg, cap.callback, cap.emit)
	rc.SetConfigNormalizer(func(_ *types.NodeDoctorConfig) error {
		return errBoom
	})

	if err := os.WriteFile(path, []byte(configWithMonitors(`["a.b.c"]`)), 0o644); err != nil {
		t.Fatal(err)
	}
	err := rc.TriggerReload(context.Background())
	if err == nil {
		t.Fatal("a failing normalizer must fail the reload")
	}
	if !strings.Contains(err.Error(), "normalize") {
		t.Errorf("error should identify normalization as the cause, got %v", err)
	}
	if cap.lastDiff() != nil {
		t.Error("the reload callback must not run when normalization failed")
	}
}

// TestReloadEmitsRestartRequiredEventForSettingsOnlyChange guards the case that
// used to report a cheerful "no changes": ComputeConfigDiff ignores settings, so
// a settings-only edit produced a success event while the process kept the old
// value.
func TestReloadEmitsRestartRequiredEventForSettingsOnlyChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(configWithMonitors("[]")), 0o644); err != nil {
		t.Fatal(err)
	}
	startCfg := loadAndNormalize(t, path, true)

	cap := &captureCoordinator{}
	rc := NewReloadCoordinator(path, startCfg, cap.callback, cap.emit)
	rc.SetConfigNormalizer(func(c *types.NodeDoctorConfig) error {
		return normalizeForTest(c, true)
	})

	// Change ONLY the node name — invisible to ComputeConfigDiff.
	renamed := strings.Replace(configWithMonitors("[]"), `nodeName: "test-node"`, `nodeName: "other-node"`, 1)
	if err := os.WriteFile(path, []byte(renamed), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := rc.TriggerReload(context.Background()); err != nil {
		t.Fatalf("reload failed: %v", err)
	}

	cap.mu.Lock()
	events := strings.Join(cap.events, "\n")
	cap.mu.Unlock()

	if !strings.Contains(events, "ConfigReloadRestartRequired") {
		t.Errorf("a settings-only change that cannot be hot-applied must emit "+
			"ConfigReloadRestartRequired, not a silent success. Events:\n%s", events)
	}
	if strings.Contains(events, "ConfigReloadNoChanges") {
		t.Errorf("must not claim 'no changes' when a restart-required change was detected. Events:\n%s", events)
	}

	if r := rc.GetLastReloadability(); r == nil || !r.HasRestartRequired() {
		t.Error("GetLastReloadability must expose the restart-required classification")
	}
}

func monitorNames(ms []types.MonitorConfig) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.Name)
	}
	return out
}
