package util

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfigFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadConfigRejectsUnknownYAMLKey(t *testing.T) {
	path := writeConfigFile(t, "typo.yaml", `
apiVersion: node-doctor.io/v1alpha1
kind: NodeDoctorConfig
metadata:
  name: typo
settings:
  nodeName: n1
  logLevle: info
monitors:
  - name: m
    type: system-cpu
    enabled: true
`)
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for unknown key")
	}
	if !strings.Contains(err.Error(), "logLevle") {
		t.Fatalf("error should name the unknown key, got: %v", err)
	}
}

func TestLoadConfigRejectsUnknownJSONKey(t *testing.T) {
	path := writeConfigFile(t, "typo.json", `{
  "apiVersion": "node-doctor.io/v1alpha1",
  "kind": "NodeDoctorConfig",
  "metadata": {"name": "typo"},
  "settings": {"nodeName": "n1"},
  "monitors": [{"name": "m", "type": "system-cpu", "enabled": true}],
  "exporters": {"http": {"enabled": false, "hostPort": 8080}}
}`)
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for unknown key")
	}
	if !strings.Contains(err.Error(), "hostPort") {
		t.Fatalf("error should name the unknown key, got: %v", err)
	}
}

func TestLoadConfigKeepsMonitorConfigFreeForm(t *testing.T) {
	path := writeConfigFile(t, "free.yaml", `
apiVersion: node-doctor.io/v1alpha1
kind: NodeDoctorConfig
metadata:
  name: free
settings:
  nodeName: n1
monitors:
  - name: m
    type: system-cpu
    enabled: true
    config:
      anythingGoesHere: true
      nested:
        deeper: 1
`)
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("monitor config maps must stay free-form: %v", err)
	}
	if config.Monitors[0].Config["anythingGoesHere"] != true {
		t.Fatalf("monitor config not preserved: %#v", config.Monitors[0].Config)
	}
}
