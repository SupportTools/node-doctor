package system

import "testing"

func TestThresholdsAcceptYAMLIntegers(t *testing.T) {
	mem, err := parseMemoryConfig(map[string]interface{}{
		"warningThreshold": 85, "criticalThreshold": 95, "swapWarningThreshold": 50, "swapCriticalThreshold": 80,
	})
	if err != nil {
		t.Fatalf("memory: %v", err)
	}
	if mem.WarningThreshold != 85 || mem.SwapCriticalThreshold != 80 {
		t.Fatalf("memory thresholds not applied: %+v", mem)
	}

	mp, err := parseMountPointConfig(map[string]interface{}{
		"path": "/", "warningThreshold": 85, "criticalThreshold": 95, "inodeWarningThreshold": 85, "inodeCriticalThreshold": 95,
	})
	if err != nil {
		t.Fatalf("mount point: %v", err)
	}
	if mp.CriticalThreshold != 95 || mp.InodeWarningThreshold != 85 {
		t.Fatalf("mount point thresholds not applied: %+v", mp)
	}

	cpu, err := parseCPUConfig(map[string]interface{}{"warningLoadFactor": 1, "criticalLoadFactor": 2})
	if err != nil {
		t.Fatalf("cpu: %v", err)
	}
	if cpu.WarningLoadFactor != 1 || cpu.CriticalLoadFactor != 2 {
		t.Fatalf("cpu load factors not applied: %+v", cpu)
	}

	if _, err := parseMemoryConfig(map[string]interface{}{"warningThreshold": "85"}); err == nil {
		t.Fatal("string threshold should be rejected")
	}
}
