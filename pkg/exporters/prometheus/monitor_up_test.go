package prometheus

import (
	"context"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

func TestExportStatusSetsMonitorUp(t *testing.T) {
	exporter, err := newEphemeralExporter(&types.GlobalSettings{NodeName: "test-node"})
	if err != nil {
		t.Fatalf("failed to create exporter: %v", err)
	}
	ctx := context.Background()
	if err := exporter.Start(ctx); err != nil {
		t.Fatalf("failed to start exporter: %v", err)
	}
	defer exporter.Stop()

	monitorUp := func() float64 {
		t.Helper()
		families, err := exporter.registry.Gather()
		if err != nil {
			t.Fatalf("failed to gather metrics: %v", err)
		}
		got, ok := gaugeValue(families, "test_monitor_up", map[string]string{"monitor_name": "disk-monitor"})
		if !ok {
			t.Fatalf("monitor_up{monitor_name=disk-monitor} not found")
		}
		return got
	}

	healthy := &types.Status{
		Source:    "disk-monitor",
		Timestamp: time.Now(),
		Conditions: []types.Condition{
			{Type: "DiskPressure", Status: types.ConditionFalse, Reason: "OK", Message: "ok", Transition: time.Now()},
		},
	}
	if err := exporter.ExportStatus(ctx, healthy); err != nil {
		t.Fatalf("export healthy: %v", err)
	}
	if got := monitorUp(); got != 1 {
		t.Errorf("monitor_up after healthy export = %v, want 1", got)
	}

	failed := &types.Status{
		Source:    "disk-monitor",
		Timestamp: time.Now(),
		Conditions: []types.Condition{
			{Type: "MonitorHealthy", Status: types.ConditionFalse, Reason: "CheckFailed", Message: "timeout", Transition: time.Now()},
		},
	}
	if err := exporter.ExportStatus(ctx, failed); err != nil {
		t.Fatalf("export failed status: %v", err)
	}
	if got := monitorUp(); got != 0 {
		t.Errorf("monitor_up after check failure = %v, want 0", got)
	}

	if err := exporter.ExportStatus(ctx, healthy); err != nil {
		t.Fatalf("export recovered: %v", err)
	}
	if got := monitorUp(); got != 1 {
		t.Errorf("monitor_up after recovery = %v, want 1", got)
	}
}
