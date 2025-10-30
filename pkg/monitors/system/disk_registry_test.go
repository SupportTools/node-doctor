package system

import (
	"context"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/monitors"
	"github.com/supporttools/node-doctor/pkg/types"
)

func TestDiskMonitorRegistration(t *testing.T) {
	// Test that the Disk monitor is properly registered
	info := monitors.GetMonitorInfo("system-disk")
	if info == nil {
		t.Fatal("system-disk monitor is not registered")
	}

	if info.Type != "system-disk" {
		t.Errorf("Monitor type: got %q, want %q", info.Type, "system-disk")
	}

	if info.Factory == nil {
		t.Error("Monitor factory is nil")
	}

	if info.Validator == nil {
		t.Error("Monitor validator is nil")
	}

	if info.Description == "" {
		t.Error("Monitor description is empty")
	}

	expectedDescription := "Monitors disk space, inode usage, readonly filesystems, and I/O health conditions"
	if info.Description != expectedDescription {
		t.Errorf("Monitor description: got %q, want %q", info.Description, expectedDescription)
	}
}

func TestDiskMonitorFactoryViaRegistry(t *testing.T) {
	// Test creating Disk monitor through the registry
	config := types.MonitorConfig{
		Name:     "registry-test-disk",
		Type:     "system-disk",
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Config: map[string]interface{}{
			"mountPoints": []interface{}{
				map[string]interface{}{
					"path":              "/",
					"warningThreshold":  85.0,
					"criticalThreshold": 95.0,
				},
			},
			"checkDiskSpace": true,
			"checkInodes":    true,
		},
	}

	ctx := context.Background()
	monitor, err := monitors.CreateMonitor(ctx, config)
	if err != nil {
		t.Fatalf("failed to create monitor via registry: %v", err)
	}

	if monitor == nil {
		t.Fatal("monitor is nil")
	}

	// Verify it implements the Monitor interface
	_, ok := monitor.(types.Monitor)
	if !ok {
		t.Error("monitor does not implement types.Monitor interface")
	}

	// Verify we can cast it to DiskMonitor
	diskMonitor, ok := monitor.(*DiskMonitor)
	if !ok {
		t.Error("monitor is not a DiskMonitor")
	}

	if diskMonitor.name != "registry-test-disk" {
		t.Errorf("monitor name: got %q, want %q", diskMonitor.name, "registry-test-disk")
	}
}

func TestDiskMonitorValidatorViaRegistry(t *testing.T) {
	tests := []struct {
		name      string
		config    types.MonitorConfig
		wantError bool
	}{
		{
			name: "valid config",
			config: types.MonitorConfig{
				Name: "test-disk",
				Type: "system-disk",
				Config: map[string]interface{}{
					"mountPoints": []interface{}{
						map[string]interface{}{
							"path":              "/",
							"warningThreshold":  85.0,
							"criticalThreshold": 95.0,
						},
					},
				},
			},
			wantError: false,
		},
		{
			name: "invalid config - threshold out of range",
			config: types.MonitorConfig{
				Name: "test-disk",
				Type: "system-disk",
				Config: map[string]interface{}{
					"mountPoints": []interface{}{
						map[string]interface{}{
							"path":              "/",
							"warningThreshold":  150.0,
							"criticalThreshold": 160.0,
						},
					},
				},
			},
			wantError: true,
		},
		{
			name: "invalid config - warning >= critical",
			config: types.MonitorConfig{
				Name: "test-disk",
				Type: "system-disk",
				Config: map[string]interface{}{
					"mountPoints": []interface{}{
						map[string]interface{}{
							"path":              "/",
							"warningThreshold":  95.0,
							"criticalThreshold": 85.0,
						},
					},
				},
			},
			wantError: true,
		},
		{
			name: "invalid config - negative sustained checks",
			config: types.MonitorConfig{
				Name: "test-disk",
				Type: "system-disk",
				Config: map[string]interface{}{
					"sustainedHighDiskChecks": -1,
				},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := monitors.ValidateConfig(tt.config)

			if tt.wantError {
				if err == nil {
					t.Error("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestRegistryContainsDiskMonitor(t *testing.T) {
	// Test that the Disk monitor appears in the registry list
	registeredTypes := monitors.GetRegisteredTypes()

	found := false
	for _, monitorType := range registeredTypes {
		if monitorType == "system-disk" {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("system-disk monitor not found in registered types: %v", registeredTypes)
	}

	// Check if the monitor type is registered
	if !monitors.IsRegistered("system-disk") {
		t.Error("system-disk monitor is not registered according to IsRegistered check")
	}
}

func TestDiskMonitorConfigurationDefaults(t *testing.T) {
	// Test that defaults are properly applied when creating via registry
	config := types.MonitorConfig{
		Name:     "default-test-disk",
		Type:     "system-disk",
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Config:   nil, // No custom config - should use defaults
	}

	ctx := context.Background()
	monitor, err := monitors.CreateMonitor(ctx, config)
	if err != nil {
		t.Fatalf("failed to create monitor with defaults: %v", err)
	}

	diskMonitor, ok := monitor.(*DiskMonitor)
	if !ok {
		t.Fatal("monitor is not a DiskMonitor")
	}

	// Check that defaults were applied
	if diskMonitor.config.SustainedHighDiskChecks != 3 {
		t.Errorf("SustainedHighDiskChecks: got %d, want 3", diskMonitor.config.SustainedHighDiskChecks)
	}

	if len(diskMonitor.config.MountPoints) == 0 {
		t.Error("No default mount points were configured")
	}

	// Check default mount points
	expectedPaths := []string{"/", "/var/lib/docker", "/var/lib/containerd"}
	if len(diskMonitor.config.MountPoints) != len(expectedPaths) {
		t.Errorf("Mount points count: got %d, want %d", len(diskMonitor.config.MountPoints), len(expectedPaths))
	} else {
		for i, expectedPath := range expectedPaths {
			if diskMonitor.config.MountPoints[i].Path != expectedPath {
				t.Errorf("Mount point %d: got %q, want %q", i, diskMonitor.config.MountPoints[i].Path, expectedPath)
			}
		}
	}

	// Check default check flags
	if !diskMonitor.config.CheckDiskSpace {
		t.Error("CheckDiskSpace should be true by default")
	}
	if !diskMonitor.config.CheckInodes {
		t.Error("CheckInodes should be true by default")
	}
	if !diskMonitor.config.CheckReadonly {
		t.Error("CheckReadonly should be true by default")
	}
	if !diskMonitor.config.CheckIOHealth {
		t.Error("CheckIOHealth should be true by default")
	}
}

func TestDiskMonitorWithCustomMountPoints(t *testing.T) {
	// Test creating monitor with custom mount points
	config := types.MonitorConfig{
		Name:     "custom-mount-test-disk",
		Type:     "system-disk",
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Config: map[string]interface{}{
			"mountPoints": []interface{}{
				map[string]interface{}{
					"path":                   "/custom",
					"warningThreshold":       80.0,
					"criticalThreshold":      90.0,
					"inodeWarningThreshold":  75.0,
					"inodeCriticalThreshold": 85.0,
				},
				map[string]interface{}{
					"path":              "/another",
					"warningThreshold":  70.0,
					"criticalThreshold": 80.0,
				},
			},
			"sustainedHighDiskChecks": 5,
		},
	}

	ctx := context.Background()
	monitor, err := monitors.CreateMonitor(ctx, config)
	if err != nil {
		t.Fatalf("failed to create monitor with custom mount points: %v", err)
	}

	diskMonitor, ok := monitor.(*DiskMonitor)
	if !ok {
		t.Fatal("monitor is not a DiskMonitor")
	}

	// Check custom configuration
	if diskMonitor.config.SustainedHighDiskChecks != 5 {
		t.Errorf("SustainedHighDiskChecks: got %d, want 5", diskMonitor.config.SustainedHighDiskChecks)
	}

	if len(diskMonitor.config.MountPoints) != 2 {
		t.Errorf("Mount points count: got %d, want 2", len(diskMonitor.config.MountPoints))
	} else {
		// Check first mount point
		mp1 := diskMonitor.config.MountPoints[0]
		if mp1.Path != "/custom" {
			t.Errorf("Mount point 0 path: got %q, want %q", mp1.Path, "/custom")
		}
		if mp1.WarningThreshold != 80.0 {
			t.Errorf("Mount point 0 warning threshold: got %f, want 80.0", mp1.WarningThreshold)
		}
		if mp1.CriticalThreshold != 90.0 {
			t.Errorf("Mount point 0 critical threshold: got %f, want 90.0", mp1.CriticalThreshold)
		}
		if mp1.InodeWarningThreshold != 75.0 {
			t.Errorf("Mount point 0 inode warning threshold: got %f, want 75.0", mp1.InodeWarningThreshold)
		}
		if mp1.InodeCriticalThreshold != 85.0 {
			t.Errorf("Mount point 0 inode critical threshold: got %f, want 85.0", mp1.InodeCriticalThreshold)
		}

		// Check second mount point (should have defaults applied)
		mp2 := diskMonitor.config.MountPoints[1]
		if mp2.Path != "/another" {
			t.Errorf("Mount point 1 path: got %q, want %q", mp2.Path, "/another")
		}
		if mp2.WarningThreshold != 70.0 {
			t.Errorf("Mount point 1 warning threshold: got %f, want 70.0", mp2.WarningThreshold)
		}
		if mp2.CriticalThreshold != 80.0 {
			t.Errorf("Mount point 1 critical threshold: got %f, want 80.0", mp2.CriticalThreshold)
		}
		// These should have defaults applied
		if mp2.InodeWarningThreshold != 85.0 {
			t.Errorf("Mount point 1 inode warning threshold: got %f, want 85.0", mp2.InodeWarningThreshold)
		}
		if mp2.InodeCriticalThreshold != 95.0 {
			t.Errorf("Mount point 1 inode critical threshold: got %f, want 95.0", mp2.InodeCriticalThreshold)
		}
	}
}