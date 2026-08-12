package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v2"
)

// chartDir locates the packaged chart relative to this test file.
func chartDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// test/integration -> repo root
	return filepath.Join(wd, "..", "..", "helm", "node-doctor")
}

// renderTemplate runs `helm template` for one chart template and returns the YAML.
func renderTemplate(t *testing.T, showOnly string) string {
	t.Helper()

	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm not installed; skipping chart rendering test")
	}

	cmd := exec.Command("helm", "template", "node-doctor", chartDir(t), "--show-only", showOnly)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helm template %s failed: %v\n%s", showOnly, err, out)
	}
	return string(out)
}

type probeSpec struct {
	Exec *struct {
		Command []string `yaml:"command"`
	} `yaml:"exec"`
	HTTPGet *struct {
		Path string      `yaml:"path"`
		Port interface{} `yaml:"port"`
	} `yaml:"httpGet"`
	InitialDelaySeconds int `yaml:"initialDelaySeconds"`
	PeriodSeconds       int `yaml:"periodSeconds"`
	TimeoutSeconds      int `yaml:"timeoutSeconds"`
	FailureThreshold    int `yaml:"failureThreshold"`
}

type daemonSetDoc struct {
	Spec struct {
		Template struct {
			Spec struct {
				Containers []struct {
					Name           string    `yaml:"name"`
					LivenessProbe  probeSpec `yaml:"livenessProbe"`
					ReadinessProbe probeSpec `yaml:"readinessProbe"`
					StartupProbe   probeSpec `yaml:"startupProbe"`
				} `yaml:"containers"`
			} `yaml:"spec"`
		} `yaml:"template"`
	} `yaml:"spec"`
}

func nodeDoctorContainerProbes(t *testing.T) (liveness, readiness, startup probeSpec) {
	t.Helper()

	rendered := renderTemplate(t, "templates/daemonset.yaml")

	var ds daemonSetDoc
	for _, doc := range strings.Split(rendered, "\n---\n") {
		if !strings.Contains(doc, "kind: DaemonSet") {
			continue
		}
		if err := yaml.Unmarshal([]byte(doc), &ds); err != nil {
			t.Fatalf("parse DaemonSet: %v", err)
		}
		break
	}

	for _, c := range ds.Spec.Template.Spec.Containers {
		if c.Name == "node-doctor" {
			return c.LivenessProbe, c.ReadinessProbe, c.StartupProbe
		}
	}
	t.Fatal("node-doctor container not found in the rendered DaemonSet")
	return
}

// TestProbesUseExecNotHostPort8080 guards the fix that moved probes OFF
// hostPort 8080.
//
// node-doctor runs with hostNetwork:true. A foreign host process (an IP-float
// daemon) owning :8080 answers an httpGet probe with a 404 and crashloops the
// pod. That port conflict still exists in the fleet today; node-doctor is only
// immune because its probes exec against a per-pod unix socket. Reintroducing
// httpGet on 8080 would silently re-arm the crashloop.
func TestProbesUseExecNotHostPort8080(t *testing.T) {
	liveness, readiness, startup := nodeDoctorContainerProbes(t)

	for name, p := range map[string]probeSpec{
		"liveness":  liveness,
		"readiness": readiness,
		"startup":   startup,
	} {
		if p.HTTPGet != nil {
			t.Errorf("%s probe uses httpGet (port %v). Probes MUST exec against the per-pod unix "+
				"socket: on a hostNetwork node a foreign process squatting :8080 answers the probe "+
				"with a 404 and crashloops the pod.", name, p.HTTPGet.Port)
		}
		if p.Exec == nil || len(p.Exec.Command) == 0 {
			t.Errorf("%s probe must use an exec command against the health unix socket", name)
		}
	}
}

// TestLivenessAndReadinessUseDistinctProbes verifies the two signals are wired
// to genuinely different endpoints (#node-doctor-246). If both ran
// `-healthcheck`, a downstream failure could never surface as NotReady, and if
// both ran `-healthcheck-ready`, a downstream failure would RESTART the pod.
func TestLivenessAndReadinessUseDistinctProbes(t *testing.T) {
	liveness, readiness, startup := nodeDoctorContainerProbes(t)

	livenessCmd := strings.Join(liveness.Exec.Command, " ")
	readinessCmd := strings.Join(readiness.Exec.Command, " ")
	startupCmd := strings.Join(startup.Exec.Command, " ")

	if !strings.Contains(livenessCmd, "-healthcheck") || strings.Contains(livenessCmd, "-healthcheck-ready") {
		t.Errorf("liveness must probe /healthz via -healthcheck, got %q", livenessCmd)
	}
	if !strings.Contains(readinessCmd, "-healthcheck-ready") {
		t.Errorf("readiness must probe /ready via -healthcheck-ready, got %q. Without this a "+
			"downstream failure cannot make the pod NotReady.", readinessCmd)
	}
	if livenessCmd == readinessCmd {
		t.Error("liveness and readiness must not be the same probe: conflating them either " +
			"restarts the pod on downstream failure or never reports NotReady")
	}
	// Startup gates the container coming up at all, so it must use LIVENESS
	// semantics — a downstream that is down at boot must not prevent start.
	if strings.Contains(startupCmd, "-healthcheck-ready") {
		t.Errorf("startup probe must use liveness semantics (-healthcheck), got %q: a downstream "+
			"that is unreachable at boot must not prevent the agent from starting", startupCmd)
	}
}

// TestStartupProbeBudgetHasHeadroom confirms the slow-but-legitimate init
// budget (#node-doctor-246 item 3).
func TestStartupProbeBudgetHasHeadroom(t *testing.T) {
	_, _, startup := nodeDoctorContainerProbes(t)

	budget := startup.InitialDelaySeconds + startup.FailureThreshold*startup.PeriodSeconds

	const minBudgetSeconds = 100
	if budget < minBudgetSeconds {
		t.Errorf("startup budget is %ds (initialDelay %d + failureThreshold %d x period %d), "+
			"want >= %ds. Too little headroom is what crashlooped a1pinode01 125x when a blocked "+
			"exporter delayed init.",
			budget, startup.InitialDelaySeconds, startup.FailureThreshold, startup.PeriodSeconds, minBudgetSeconds)
	}

	if startup.FailureThreshold < 12 {
		t.Errorf("startupProbe.failureThreshold = %d, want >= 12 for cold-start headroom on a loaded node",
			startup.FailureThreshold)
	}
}

// TestRenderedConfigEnablesHotReload guards the chart<->code contract for
// #node-doctor-243: the shipped ConfigMap must actually turn hot reload on, and
// must parse into the agent's config type.
func TestRenderedConfigEnablesHotReload(t *testing.T) {
	rendered := renderTemplate(t, "templates/configmap.yaml")

	var cm struct {
		Data map[string]string `yaml:"data"`
	}
	if err := yaml.Unmarshal([]byte(rendered), &cm); err != nil {
		t.Fatalf("parse ConfigMap: %v", err)
	}

	raw, ok := cm.Data["config.yaml"]
	if !ok {
		t.Fatal("rendered ConfigMap has no config.yaml key")
	}

	var parsed struct {
		Reload struct {
			Enabled          *bool  `yaml:"enabled"`
			DebounceInterval string `yaml:"debounceInterval"`
		} `yaml:"reload"`
	}
	if err := yaml.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("the shipped config.yaml does not parse: %v", err)
	}

	if parsed.Reload.Enabled == nil {
		t.Fatal("the shipped config must state reload.enabled explicitly so operators can see the knob")
	}
	if !*parsed.Reload.Enabled {
		t.Error("the shipped chart must enable config hot reload; otherwise every ConfigMap edit " +
			"silently requires a rollout")
	}
	if parsed.Reload.DebounceInterval == "" {
		t.Error("reload.debounceInterval should be set explicitly in the shipped config")
	}
}
