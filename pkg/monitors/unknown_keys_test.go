package monitors

import (
	"bytes"
	"log"
	"reflect"
	"strings"
	"testing"
)

func captureLog(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)
	fn()
	return buf.String()
}

func TestUnknownKeysSortedAndExcludesKnown(t *testing.T) {
	cfg := map[string]interface{}{"zeta": 1, "alpha": 2, "known": 3}
	got := UnknownKeys(cfg, "known")
	if want := []string{"alpha", "zeta"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := UnknownKeys(nil, "known"); len(got) != 0 {
		t.Fatalf("nil config should have no unknown keys, got %v", got)
	}
}

func TestWarnUnknownKeysLogsOnlyWhenNeeded(t *testing.T) {
	out := captureLog(t, func() {
		WarnUnknownKeys("cpu-health", map[string]interface{}{"warningLoadFactor": 0.8}, "warningLoadFactor")
	})
	if out != "" {
		t.Fatalf("expected no warning, got %q", out)
	}

	out = captureLog(t, func() {
		WarnUnknownKeys("cpu-health", map[string]interface{}{"loadAverageThresholds": 1}, "warningLoadFactor")
	})
	if !strings.Contains(out, "[WARN] monitor cpu-health: unknown config keys [loadAverageThresholds] (ignored)") {
		t.Fatalf("unexpected warning text: %q", out)
	}
}

func TestWarnUnknownNestedKeysHandlesMapsAndLists(t *testing.T) {
	cfg := map[string]interface{}{
		"auth": map[string]interface{}{"type": "bearer", "tokenFiel": "x"},
		"mountPoints": []interface{}{
			map[string]interface{}{"path": "/"},
			map[string]interface{}{"path": "/var", "warnThreshold": 1},
		},
	}
	out := captureLog(t, func() {
		WarnUnknownNestedKeys("m", cfg, "auth", "type", "tokenFile")
		WarnUnknownNestedKeys("m", cfg, "mountPoints", "path", "warningThreshold")
		WarnUnknownNestedKeys("m", cfg, "absent", "anything")
	})
	for _, want := range []string{
		"monitor m.auth: unknown config keys [tokenFiel]",
		"monitor m.mountPoints[1]: unknown config keys [warnThreshold]",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "mountPoints[0]") {
		t.Errorf("clean entry should not warn:\n%s", out)
	}
}
