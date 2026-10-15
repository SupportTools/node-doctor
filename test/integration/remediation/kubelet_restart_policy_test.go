package remediation

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gopkg.in/yaml.v2"

	"github.com/supporttools/node-doctor/pkg/detector"
	"github.com/supporttools/node-doctor/pkg/remediators"
	"github.com/supporttools/node-doctor/pkg/types"
	"github.com/supporttools/node-doctor/pkg/util"
)

// End-to-end proof of the kubelet restart policy (cluster-services-831), from the
// chart down to the systemctl call:
//
//	helm template (monitors.kubelet.remediation.enabled=true)
//	  -> util.LoadConfig on the rendered config.yaml
//	  -> detector dispatch policy (trigger, per-incident cap, cooldown, write-probe precondition)
//	  -> RemediatorRegistry (global rate limits, circuit breaker)
//	  -> SystemdRemediator (is-enabled guard)
//	  -> systemctl, replaced here by a recording fake. Nothing touches the host.

// recordingSystemctl stands in for systemctl. isEnabled is the answer to
// `systemctl is-enabled <unit>`.
type recordingSystemctl struct {
	mu        sync.Mutex
	isEnabled string
	commands  []string
}

func (f *recordingSystemctl) ExecuteSystemctl(_ context.Context, args ...string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commands = append(f.commands, strings.Join(args, " "))
	if args[0] == "is-enabled" {
		if f.isEnabled != "enabled" {
			return f.isEnabled, &exec.ExitError{}
		}
		return "enabled", nil
	}
	return "", nil
}

func (f *recordingSystemctl) IsActive(context.Context, string) (bool, error) { return true, nil }

func (f *recordingSystemctl) Commands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.commands...)
}

// chanMonitor emits whatever the test pushes into it.
type chanMonitor struct{ ch chan *types.Status }

func (m *chanMonitor) Start() (<-chan *types.Status, error) { return m.ch, nil }
func (m *chanMonitor) Stop()                                {}

type chanFactory struct {
	mu       sync.Mutex
	monitors map[string]*chanMonitor
}

func (f *chanFactory) CreateMonitor(cfg types.MonitorConfig) (types.Monitor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if m, ok := f.monitors[cfg.Name]; ok {
		return m, nil
	}
	m := &chanMonitor{ch: make(chan *types.Status, 16)}
	f.monitors[cfg.Name] = m
	return m, nil
}

func (f *chanFactory) get(name string) *chanMonitor {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.monitors[name]
}

type countingExporter struct{ n atomic.Int64 }

func (e *countingExporter) ExportStatus(context.Context, *types.Status) error { e.n.Add(1); return nil }
func (e *countingExporter) ExportProblem(context.Context, *types.Problem) error {
	return nil
}

// renderChartConfig renders the chart's config.yaml with the kubelet monitor and its
// remediation enabled, exactly as the a1-ops-prd values do.
func renderChartConfig(t *testing.T, extraSets ...string) (string, error) {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm not installed; skipping chart-driven remediation test")
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	chart := filepath.Join(wd, "..", "..", "..", "helm", "node-doctor")
	args := []string{"template", "node-doctor", chart, "--show-only", "templates/configmap.yaml",
		"--set", "monitors.kubelet.enabled=true",
		"--set", "monitors.kubelet.remediation.enabled=true"}
	for _, s := range extraSets {
		args = append(args, "--set", s)
	}
	out, err := exec.Command("helm", args...).CombinedOutput()
	if err != nil {
		return string(out), err
	}
	var cm struct {
		Data map[string]string `yaml:"data"`
	}
	if err := yaml.Unmarshal(out, &cm); err != nil {
		t.Fatalf("parse rendered ConfigMap: %v", err)
	}
	return cm.Data["config.yaml"], nil
}

type harness struct {
	pd        *detector.ProblemDetector
	factory   *chanFactory
	exporter  *countingExporter
	systemctl *recordingSystemctl
}

func newHarness(t *testing.T, isEnabled string) *harness {
	t.Helper()
	raw, err := renderChartConfig(t)
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, raw)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NODE_NAME", "test-node")
	cfg, err := util.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig on the rendered chart config: %v", err)
	}
	off := false
	cfg.Reload.Enabled = &off // no file watcher in the test

	// Sanity: the rendered policy is the one the chart documents.
	var kubelet *types.MonitorConfig
	for i := range cfg.Monitors {
		if cfg.Monitors[i].Type == "kubernetes-kubelet-check" {
			kubelet = &cfg.Monitors[i]
		}
	}
	if kubelet == nil || kubelet.Remediation == nil {
		t.Fatal("rendered config has no kubelet monitor with remediation")
	}
	r := kubelet.Remediation
	if !r.Enabled || r.Strategy != "systemd-restart" || r.Service != "rke2-agent" ||
		!r.RequireServiceEnabled || !reflect.DeepEqual(r.TriggerConditions, []string{"KubeletUnhealthy"}) ||
		r.MaxAttemptsPerIncident != 1 || r.Cooldown != 30*time.Minute || len(r.Preconditions) != 1 {
		t.Fatalf("unexpected rendered kubelet remediation: %+v", *r)
	}

	registry := remediators.NewRegistry(cfg.Remediation.MaxRemediationsPerHour, 100)
	if err := registry.ApplyConfig(&cfg.Remediation, cfg.Settings.DryRunMode); err != nil {
		t.Fatalf("registry.ApplyConfig: %v", err)
	}
	fake := &recordingSystemctl{isEnabled: isEnabled}
	// Read outside the factory: the registry calls factories under its own lock.
	dryRun := registry.IsDryRun()
	// Same registration as RegisterBuiltinRemediators, with systemctl faked.
	registry.Register(remediators.RemediatorInfo{
		Type: remediators.StrategySystemdRestart,
		Factory: func() (types.Remediator, error) {
			r, err := remediators.NewSystemdRemediator(remediators.SystemdConfig{
				Operation: remediators.SystemdRestart,
				DryRun:    dryRun,
			})
			if err != nil {
				return nil, err
			}
			r.SetSystemdExecutor(fake)
			return r, nil
		},
	})

	factory := &chanFactory{monitors: map[string]*chanMonitor{}}
	exporter := &countingExporter{}
	pd, err := detector.NewProblemDetector(cfg, nil, []types.Exporter{exporter}, path, factory, nil)
	if err != nil {
		t.Fatalf("NewProblemDetector: %v", err)
	}
	pd.SetRemediatorRegistry(registry)
	if err := pd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = pd.Stop() })
	return &harness{pd: pd, factory: factory, exporter: exporter, systemctl: fake}
}

// send pushes one status and waits until the detector has fully processed it.
// Statuses are processed one at a time in arrival order, so once this returns
// every earlier status, including its remediation, is done too.
func (h *harness) send(t *testing.T, monitor string, conds ...types.Condition) {
	t.Helper()
	m := h.factory.get(monitor)
	if m == nil {
		t.Fatalf("no monitor %q in the rendered config", monitor)
	}
	before := h.exporter.n.Load()
	st := types.NewStatus(monitor)
	for _, c := range conds {
		st.AddCondition(c)
	}
	m.ch <- st
	deadline := time.Now().Add(5 * time.Second)
	for h.exporter.n.Load() <= before {
		if time.Now().After(deadline) {
			t.Fatalf("status from %s was not processed", monitor)
		}
		time.Sleep(2 * time.Millisecond)
	}
	// The exporter runs before remediation inside the same processStatus call;
	// a following no-op status is the barrier that proves this one finished.
}

func (h *harness) barrier(t *testing.T) {
	t.Helper()
	h.send(t, "memory-health", types.NewCondition("Barrier", types.ConditionTrue, "x", ""))
}

var (
	diskOK       = types.NewCondition("ReadonlyFilesystem", types.ConditionFalse, "WriteProbeOK", "")
	diskReadOnly = types.NewCondition("ReadonlyFilesystem", types.ConditionTrue, "WriteProbeFailed", "")
	kubeletDown  = types.NewCondition("KubeletUnhealthy", types.ConditionTrue, "RepeatedHealthCheckFailures", "")
)

func kubeletHealthyConds() []types.Condition {
	return []types.Condition{
		types.NewCondition("KubeletHealthy", types.ConditionTrue, "HealthCheckPassed", ""),
		types.NewCondition("KubeletDown", types.ConditionFalse, "KubeletUp", ""),
		types.NewCondition("KubeletUnhealthy", types.ConditionFalse, "HealthCheckPassed", ""),
	}
}

func TestKubeletRestartPolicy_AgentNode_OneRestartPerIncident(t *testing.T) {
	h := newHarness(t, "enabled")

	// Healthy for a while: nothing happens.
	for i := 0; i < 5; i++ {
		h.send(t, "disk-health", diskOK)
		h.send(t, "kubelet-health", kubeletHealthyConds()...)
	}
	h.barrier(t)
	if cmds := h.systemctl.Commands(); len(cmds) != 0 {
		t.Fatalf("healthy kubelet produced systemctl calls: %q", cmds)
	}

	// Kubelet goes down and stays down.
	for i := 0; i < 6; i++ {
		h.send(t, "disk-health", diskOK)
		h.send(t, "kubelet-health", kubeletDown)
	}
	h.barrier(t)

	want := []string{"is-enabled rke2-agent", "restart rke2-agent"}
	if got := h.systemctl.Commands(); !reflect.DeepEqual(got, want) {
		t.Fatalf("systemctl calls = %q, want exactly %q", got, want)
	}
}

func TestKubeletRestartPolicy_ReadOnlyDisk_NoRestart(t *testing.T) {
	h := newHarness(t, "enabled")

	for i := 0; i < 6; i++ {
		h.send(t, "disk-health", diskReadOnly)
		h.send(t, "kubelet-health", kubeletDown)
	}
	h.barrier(t)
	if cmds := h.systemctl.Commands(); len(cmds) != 0 {
		t.Fatalf("restart attempted while the write probe failed: %q", cmds)
	}
}

// On an RKE2 server node kubelet belongs to rke2-server and rke2-agent is disabled.
// The guard must stop at is-enabled and never restart (= start) rke2-agent.
func TestKubeletRestartPolicy_ServerNode_Refused(t *testing.T) {
	h := newHarness(t, "disabled")

	for i := 0; i < 4; i++ {
		h.send(t, "disk-health", diskOK)
		h.send(t, "kubelet-health", kubeletDown)
	}
	h.barrier(t)
	want := []string{"is-enabled rke2-agent"}
	if got := h.systemctl.Commands(); !reflect.DeepEqual(got, want) {
		t.Fatalf("systemctl calls = %q, want only %q (one attempt, refused)", got, want)
	}
}

func TestKubeletRestartPolicy_ChartDefaultsOff(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm not installed")
	}
	wd, _ := os.Getwd()
	chart := filepath.Join(wd, "..", "..", "..", "helm", "node-doctor")
	out, err := exec.Command("helm", "template", "node-doctor", chart,
		"--show-only", "templates/configmap.yaml").CombinedOutput()
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "kubernetes-kubelet-check") {
		t.Error("the kubelet monitor must be opt-in; the default render contains it")
	}
	if strings.Contains(string(out), "systemd-restart") {
		t.Error("the default render must not configure any systemd restart")
	}
}

func TestKubeletRestartPolicy_ChartRefusesWithoutWriteProbe(t *testing.T) {
	out, err := renderChartConfig(t, "monitors.disk.writeProbe.enabled=false")
	if err == nil {
		t.Fatal("chart rendered a write-probe-gated restart with the write probe disabled")
	}
	if !strings.Contains(out, "requireWriteProbeOK needs") {
		t.Errorf("unexpected helm error: %s", out)
	}
}
