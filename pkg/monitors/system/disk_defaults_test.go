package system

import (
	"strings"
	"testing"

	"github.com/supporttools/node-doctor/pkg/monitors"
)

func TestRegistryDefaultDiskConfigTakesEffect(t *testing.T) {
	info := monitors.GetMonitorInfo("system-disk")
	if info == nil || info.DefaultConfig == nil {
		t.Fatal("system-disk has no registered DefaultConfig")
	}
	parsed, err := parseDiskConfig(info.DefaultConfig.Config)
	if err != nil {
		t.Fatalf("parseDiskConfig(DefaultConfig): %v", err)
	}

	var paths []string
	for _, mp := range parsed.MountPoints {
		paths = append(paths, mp.Path)
	}
	want := []string{"/", "/var/lib/kubelet", "/var/lib/containerd"}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Fatalf("default mount points = %v, want %v", paths, want)
	}
	if containerd := parsed.MountPoints[2]; containerd.WarningThreshold != 80 || containerd.CriticalThreshold != 90 {
		t.Errorf("containerd thresholds = %v/%v, want 80/90", containerd.WarningThreshold, containerd.CriticalThreshold)
	}
	if !parsed.CheckDiskSpace || !parsed.CheckInodes || !parsed.CheckReadonly || !parsed.CheckIOHealth {
		t.Errorf("default checks not all enabled: %+v", parsed)
	}
}

func TestParseThresholdsAcceptIntegers(t *testing.T) {
	disk, err := parseDiskConfig(map[string]interface{}{
		"mountPoints": []interface{}{
			map[string]interface{}{"path": "/", "warningThreshold": 70, "criticalThreshold": 90, "inodeWarningThreshold": 75},
		},
	})
	if err != nil {
		t.Fatalf("parseDiskConfig: %v", err)
	}
	if mp := disk.MountPoints[0]; mp.WarningThreshold != 70 || mp.CriticalThreshold != 90 || mp.InodeWarningThreshold != 75 {
		t.Errorf("disk thresholds = %+v", mp)
	}

	mem, err := parseMemoryConfig(map[string]interface{}{"warningThreshold": 80, "swapCriticalThreshold": 60})
	if err != nil {
		t.Fatalf("parseMemoryConfig: %v", err)
	}
	if mem.WarningThreshold != 80 || mem.SwapCriticalThreshold != 60 {
		t.Errorf("memory thresholds = %+v", mem)
	}

	cpu, err := parseCPUConfig(map[string]interface{}{"warningLoadFactor": 1, "criticalLoadFactor": 2})
	if err != nil {
		t.Fatalf("parseCPUConfig: %v", err)
	}
	if cpu.WarningLoadFactor != 1 || cpu.CriticalLoadFactor != 2 {
		t.Errorf("cpu load factors = %+v", cpu)
	}
}
