package network

import (
	"testing"
	"time"
)

func TestWarningLatencyFor(t *testing.T) {
	mk := func(warn, crossWarn time.Duration) *CNIMonitor {
		return &CNIMonitor{config: &CNIMonitorConfig{
			Connectivity: ConnectivityConfig{
				WarningLatency:          warn,
				CrossZoneWarningLatency: crossWarn,
			},
		}}
	}
	sameZone := Peer{NodeName: "a", SameZone: true}
	crossZone := Peer{NodeName: "b", SameZone: false}

	// Cross-zone threshold configured: same-zone peer uses tight, cross-zone uses loose.
	m := mk(200*time.Millisecond, 800*time.Millisecond)
	if got := m.warningLatencyFor(sameZone); got != 200*time.Millisecond {
		t.Errorf("same-zone threshold = %v, want 200ms", got)
	}
	if got := m.warningLatencyFor(crossZone); got != 800*time.Millisecond {
		t.Errorf("cross-zone threshold = %v, want 800ms", got)
	}

	// Cross-zone threshold unset (0): topology awareness is inert — both use WarningLatency.
	inert := mk(200*time.Millisecond, 0)
	if got := inert.warningLatencyFor(crossZone); got != 200*time.Millisecond {
		t.Errorf("inert cross-zone threshold = %v, want 200ms (same as warning)", got)
	}
	if got := inert.warningLatencyFor(sameZone); got != 200*time.Millisecond {
		t.Errorf("inert same-zone threshold = %v, want 200ms", got)
	}
}

func TestConnectivityCrossZoneLatencyParsing(t *testing.T) {
	cfg, err := parseCNIConfig(map[string]interface{}{
		"connectivity": map[string]interface{}{
			"warningLatency":           "200ms",
			"criticalLatency":          "500ms",
			"crossZoneWarningLatency":  "1s",
			"crossZoneCriticalLatency": "2s",
		},
	})
	if err != nil {
		t.Fatalf("parseCNIConfig: %v", err)
	}
	if cfg.Connectivity.CrossZoneWarningLatency != time.Second {
		t.Errorf("crossZoneWarningLatency = %v, want 1s", cfg.Connectivity.CrossZoneWarningLatency)
	}
	if cfg.Connectivity.CrossZoneCriticalLatency != 2*time.Second {
		t.Errorf("crossZoneCriticalLatency = %v, want 2s", cfg.Connectivity.CrossZoneCriticalLatency)
	}
}
