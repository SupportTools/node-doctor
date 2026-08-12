package detector

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/monitors"
	"github.com/supporttools/node-doctor/pkg/reload"
	"github.com/supporttools/node-doctor/pkg/types"
	"github.com/supporttools/node-doctor/pkg/util"
)

// recordingFactory captures the MonitorConfig used to build each monitor, so a
// test can assert what the RUNNING monitor was actually configured with — as
// opposed to what is merely sitting in the file on disk. That distinction is
// the whole of #node-doctor-243.
type recordingFactory struct {
	mu      sync.Mutex
	created []types.MonitorConfig
}

func (f *recordingFactory) CreateMonitor(config types.MonitorConfig) (types.Monitor, error) {
	f.mu.Lock()
	f.created = append(f.created, config)
	f.mu.Unlock()
	return NewMockMonitor(config.Name), nil
}

// effectiveConfigFor returns the config of the most recent build of the named
// monitor — i.e. the config the running instance is actually using.
func (f *recordingFactory) effectiveConfigFor(name string) (types.MonitorConfig, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.created) - 1; i >= 0; i-- {
		if f.created[i].Name == name {
			return f.created[i], true
		}
	}
	return types.MonitorConfig{}, false
}

func (f *recordingFactory) buildCount(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.created {
		if c.Name == name {
			n++
		}
	}
	return n
}

// dnsConfigYAML renders a config whose dns-health monitor carries the given
// clusterDomains value — the exact field edited during the incident.
func dnsConfigYAML(clusterDomains string) string {
	return fmt.Sprintf(`apiVersion: v1
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
      clusterDomains: %s
      externalDomains:
        - google.com
exporters:
  prometheus:
    enabled: true
reload:
  enabled: true
  debounceInterval: 50ms
`, clusterDomains)
}

// writeConfigMapUpdate emulates the kubelet's atomic ConfigMap writer: a new
// timestamped data directory, then a rename(2) of the ..data symlink onto it.
func writeConfigMapUpdate(t *testing.T, dir, timestamp, content string) {
	t.Helper()

	dataDir := filepath.Join(dir, ".."+timestamp)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "config.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	tmpLink := filepath.Join(dir, "..data_tmp")
	_ = os.Remove(tmpLink)
	if err := os.Symlink(".."+timestamp, tmpLink); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmpLink, filepath.Join(dir, "..data")); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(dir, "config.yaml")
	if _, err := os.Lstat(link); os.IsNotExist(err) {
		if err := os.Symlink("..data/config.yaml", link); err != nil {
			t.Fatal(err)
		}
	}
}

// TestConfigMapEditReinitializesRunningMonitor is the end-to-end reproduction of
// the reported incident (#node-doctor-243), driven entirely through the real
// machinery: a Kubernetes-style ConfigMap symlink swap, the fsnotify watcher,
// the reload coordinator, and the detector's monitor re-initialization.
//
// The reported symptom was that after patching the ConfigMap the file on the pod
// showed the NEW value while the running DNS monitor kept its OLD behaviour and
// kept firing, only taking effect after a manual pod restart. This test fails if
// the running monitor is not rebuilt from the new config.
func TestConfigMapEditReinitializesRunningMonitor(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	// Start with the cluster-domain probe ENABLED (the pre-incident state).
	writeConfigMapUpdate(t, dir, "2026_08_12_00_00_00.111111",
		dnsConfigYAML(`["kubernetes.default.svc.cluster.local"]`))
	configPath := filepath.Join(dir, "config.yaml")

	// Startup sequence, mirroring main.go.
	normalize := func(c *types.NodeDoctorConfig) error {
		monitors.ApplyDefaultMonitors(c)
		return c.ApplyDefaults()
	}
	cfg, err := util.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if err := normalize(cfg); err != nil {
		t.Fatalf("normalize: %v", err)
	}

	factory := &recordingFactory{}
	det, err := NewProblemDetector(cfg, nil,
		[]types.Exporter{NewMockExporter("test-exporter")}, configPath, factory, nil)
	if err != nil {
		t.Fatalf("new detector: %v", err)
	}
	det.SetConfigNormalizer(func(c *types.NodeDoctorConfig) error { return normalize(c) })

	if err := det.Start(); err != nil {
		t.Fatalf("start detector: %v", err)
	}
	defer func() { _ = det.Stop() }()

	// The running monitor starts with the OLD cluster domain.
	initial, ok := factory.effectiveConfigFor("dns-health")
	if !ok {
		t.Fatal("dns-health monitor was never built at startup")
	}
	if got := fmt.Sprint(initial.Config["clusterDomains"]); got != "[kubernetes.default.svc.cluster.local]" {
		t.Fatalf("startup clusterDomains = %v, want the configured cluster domain", initial.Config["clusterDomains"])
	}
	buildsBefore := factory.buildCount("dns-health")

	det.handlesMu.Lock()
	monitorsBefore := len(det.monitorHandles)
	det.handlesMu.Unlock()

	// --- THE INCIDENT ACTION: patch the ConfigMap to disable cluster probes ---
	writeConfigMapUpdate(t, dir, "2026_08_12_00_00_30.222222", dnsConfigYAML(`[]`))
	_ = os.RemoveAll(filepath.Join(dir, "..2026_08_12_00_00_00.111111"))

	// Wait for the watcher + coordinator to re-initialize the monitor.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if factory.buildCount("dns-health") > buildsBefore {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if factory.buildCount("dns-health") <= buildsBefore {
		t.Fatal("the dns-health monitor was NEVER rebuilt after the ConfigMap edit. " +
			"The file on disk shows the new value while the running monitor keeps the old " +
			"behaviour — exactly the silent staleness #node-doctor-243 reported.")
	}

	effective, _ := factory.effectiveConfigFor("dns-health")
	got := fmt.Sprint(effective.Config["clusterDomains"])
	if got != "[]" {
		t.Errorf("running monitor's effective clusterDomains = %v, want [] — the monitor was "+
			"rebuilt but not from the NEW config", effective.Config["clusterDomains"])
	}

	// Applying the operator's one-monitor edit must not take any OTHER monitor
	// down with it. Without the startup/reload normalization symmetry, every
	// monitor that ApplyDefaultMonitors auto-added looks "removed" on reload and
	// is silently stopped.
	det.handlesMu.Lock()
	monitorsAfter := len(det.monitorHandles)
	det.handlesMu.Unlock()

	if monitorsAfter != monitorsBefore {
		t.Errorf("running monitor count changed from %d to %d after editing a single monitor. "+
			"The reload path and the startup path disagree about the config, so auto-defaulted "+
			"monitors were silently stopped.", monitorsBefore, monitorsAfter)
	}
}

// TestConfigMapEditDoesNotStopUnrelatedDefaultMonitors is the companion guard
// for the normalization asymmetry: an edit to ONE monitor must not collaterally
// stop the monitors that ApplyDefaultMonitors auto-added at startup.
func TestConfigMapEditDoesNotStopUnrelatedDefaultMonitors(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	writeConfigMapUpdate(t, dir, "2026_08_12_00_00_00.111111",
		dnsConfigYAML(`["kubernetes.default.svc.cluster.local"]`))
	configPath := filepath.Join(dir, "config.yaml")

	normalize := func(c *types.NodeDoctorConfig) error {
		monitors.ApplyDefaultMonitors(c)
		return c.ApplyDefaults()
	}
	cfg, err := util.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if err := normalize(cfg); err != nil {
		t.Fatalf("normalize: %v", err)
	}

	// Auto-defaulted monitors are what silently disappeared on the first reload.
	if len(cfg.Monitors) < 2 {
		t.Skip("registry contributed no default monitors; nothing to protect here")
	}
	startupMonitorCount := len(cfg.Monitors)

	factory := &recordingFactory{}
	det, err := NewProblemDetector(cfg, nil,
		[]types.Exporter{NewMockExporter("test-exporter")}, configPath, factory, nil)
	if err != nil {
		t.Fatalf("new detector: %v", err)
	}
	det.SetConfigNormalizer(func(c *types.NodeDoctorConfig) error { return normalize(c) })

	if err := det.Start(); err != nil {
		t.Fatalf("start detector: %v", err)
	}
	defer func() { _ = det.Stop() }()

	det.handlesMu.Lock()
	monitorsBefore := len(det.monitorHandles)
	det.handlesMu.Unlock()

	// Edit ONLY dns-health.
	newCfg, err := util.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = normalize(newCfg)
	editedRaw := dnsConfigYAML(`[]`)
	tmpPath := filepath.Join(t.TempDir(), "edited.yaml")
	if err := os.WriteFile(tmpPath, []byte(editedRaw), 0o644); err != nil {
		t.Fatal(err)
	}
	edited, err := util.LoadConfig(tmpPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := normalize(edited); err != nil {
		t.Fatal(err)
	}

	diff := reload.ComputeConfigDiff(cfg, edited)
	if len(diff.MonitorsRemoved) != 0 {
		t.Fatalf("editing one monitor must not mark others removed, got %d removed. "+
			"This is the asymmetry that silently stopped auto-defaulted monitors.",
			len(diff.MonitorsRemoved))
	}

	if err := det.applyConfigReload(context.Background(), edited, diff); err != nil {
		t.Fatalf("applyConfigReload: %v", err)
	}

	det.handlesMu.Lock()
	monitorsAfter := len(det.monitorHandles)
	det.handlesMu.Unlock()

	if monitorsAfter != monitorsBefore {
		t.Errorf("monitor count changed from %d to %d after editing a single monitor "+
			"(startup config had %d monitors). Auto-defaulted monitors were silently stopped.",
			monitorsBefore, monitorsAfter, startupMonitorCount)
	}
}
