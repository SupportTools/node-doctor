package helm

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/supporttools/node-doctor/pkg/monitors"
	"github.com/supporttools/node-doctor/pkg/util"

	_ "github.com/supporttools/node-doctor/pkg/monitors/kubernetes"
	_ "github.com/supporttools/node-doctor/pkg/monitors/network"
	_ "github.com/supporttools/node-doctor/pkg/monitors/system"
)

func renderConfigMap(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm not installed")
	}
	out, err := exec.Command("helm", "template", "node-doctor", "node-doctor",
		"--namespace", "node-doctor", "--show-only", "templates/configmap.yaml").Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Fatalf("helm template: %v\n%s", err, ee.Stderr)
		}
		t.Fatalf("helm template: %v", err)
	}

	var cm struct {
		Data map[string]string `yaml:"data"`
	}
	if err := yaml.Unmarshal(out, &cm); err != nil {
		t.Fatalf("parse rendered ConfigMap: %v", err)
	}
	cfg, ok := cm.Data["config.yaml"]
	if !ok {
		t.Fatal("rendered ConfigMap has no config.yaml")
	}
	return cfg
}

func TestDefaultChartConfigLoadsAndEnablesKubernetesMonitors(t *testing.T) {
	cfgYAML := renderConfigMap(t)
	t.Setenv("NODE_NAME", "test-node")

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(cfgYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := util.LoadConfig(path)
	if err != nil {
		t.Fatalf("default chart config does not load: %v", err)
	}
	if err := cfg.ValidateWithRegistry(monitors.DefaultRegistry); err != nil {
		t.Fatalf("default chart config references unregistered monitors: %v", err)
	}

	byType := map[string]int{}
	for _, m := range cfg.Monitors {
		byType[m.Type]++
		if err := monitors.ValidateConfig(m); err != nil {
			t.Errorf("monitor %s (%s) rejected by its validator: %v", m.Name, m.Type, err)
		}
	}
	for _, want := range []string{
		"kubernetes-kubelet-check",
		"kubernetes-apiserver-check",
		"kubernetes-runtime-check",
		"kubernetes-capacity-check",
		"network-gateway-check",
		"network-connectivity-check",
	} {
		if byType[want] != 1 {
			t.Errorf("default chart renders %d %s monitors, want 1", byType[want], want)
		}
	}
}
