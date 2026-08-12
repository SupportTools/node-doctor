package detector

import (
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// TestReloadEnabledKnobIsHonored guards that reload.enabled actually does
// something in BOTH directions.
//
// The field was previously parsed and then never read: the config watcher
// started unconditionally, so an operator who set reload.enabled=false got no
// behaviour change and no warning. A knob that silently does nothing is the
// same class of bug as config that silently goes stale.
func TestReloadEnabledKnobIsHonored(t *testing.T) {
	tests := []struct {
		name        string
		enabled     *bool
		wantWatcher bool
	}{
		{"explicitly enabled", boolPtr(true), true},
		{"explicitly disabled", boolPtr(false), false},
		// Absent means enabled, preserving historical behaviour for every
		// existing deployment whose ConfigMap has no reload section.
		{"absent defaults to enabled", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := CreateTestConfigWithMonitors([]types.MonitorConfig{
				{
					Name:           "test-monitor",
					Type:           "mock",
					Enabled:        true,
					IntervalString: "30s",
					TimeoutString:  "10s",
				},
			})
			cfg.Reload = types.ReloadConfig{
				Enabled:          tt.enabled,
				DebounceInterval: 50 * time.Millisecond,
			}

			helper := NewReloadTestHelper(t)
			helper.Setup(t, cfg)
			t.Cleanup(func() { _ = helper.detector.Stop() })

			if err := helper.detector.Start(); err != nil {
				t.Fatalf("start detector: %v", err)
			}

			gotWatcher := helper.detector.configChangeCh != nil
			if gotWatcher != tt.wantWatcher {
				t.Errorf("reload.enabled=%v: watcher running = %v, want %v",
					tt.enabled, gotWatcher, tt.wantWatcher)
			}
		})
	}
}

// TestReloadConfigIsEnabledDefault pins the nil-means-true semantics that every
// existing deployment relies on.
func TestReloadConfigIsEnabledDefault(t *testing.T) {
	var nilCfg *types.ReloadConfig
	if !nilCfg.IsEnabled() {
		t.Error("a nil ReloadConfig must report enabled (historical default)")
	}

	cfg := &types.ReloadConfig{}
	if !cfg.IsEnabled() {
		t.Error("an unset Enabled must report enabled (historical default)")
	}

	if err := cfg.ApplyDefaults(); err != nil {
		t.Fatal(err)
	}
	if cfg.Enabled == nil || !*cfg.Enabled {
		t.Error("ApplyDefaults must materialize Enabled=true when unset")
	}

	off := false
	explicit := &types.ReloadConfig{Enabled: &off}
	if err := explicit.ApplyDefaults(); err != nil {
		t.Fatal(err)
	}
	if explicit.IsEnabled() {
		t.Error("ApplyDefaults must not override an explicit false")
	}
}
