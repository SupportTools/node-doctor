package system

import (
	"context"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/monitors"
	"github.com/supporttools/node-doctor/pkg/types"
)

func TestCPUMonitorRegistration(t *testing.T) {
	// Test that the CPU monitor is properly registered
	info := monitors.GetMonitorInfo("system-cpu")
	if info == nil {
		t.Fatal("system-cpu monitor is not registered")
	}

	if info.Type != "system-cpu" {
		t.Errorf("Monitor type: got %q, want %q", info.Type, "system-cpu")
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

	expectedDescription := "Monitors CPU load average and thermal throttling conditions"
	if info.Description != expectedDescription {
		t.Errorf("Monitor description: got %q, want %q", info.Description, expectedDescription)
	}
}

func TestCPUMonitorFactoryViaRegistry(t *testing.T) {
	// Test creating CPU monitor through the registry
	config := types.MonitorConfig{
		Name:     "registry-test-cpu",
		Type:     "system-cpu",
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Config: map[string]interface{}{
			"warningLoadFactor":  0.8,
			"criticalLoadFactor": 1.5,
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

	// Verify we can cast it to CPUMonitor
	cpuMonitor, ok := monitor.(*CPUMonitor)
	if !ok {
		t.Error("monitor is not a CPUMonitor")
	}

	if cpuMonitor.name != "registry-test-cpu" {
		t.Errorf("monitor name: got %q, want %q", cpuMonitor.name, "registry-test-cpu")
	}
}

func TestCPUMonitorValidatorViaRegistry(t *testing.T) {
	tests := []struct {
		name      string
		config    types.MonitorConfig
		wantError bool
	}{
		{
			name: "valid config",
			config: types.MonitorConfig{
				Name: "test-cpu",
				Type: "system-cpu",
				Config: map[string]interface{}{
					"warningLoadFactor":  0.8,
					"criticalLoadFactor": 1.5,
				},
			},
			wantError: false,
		},
		{
			name: "invalid config",
			config: types.MonitorConfig{
				Name: "test-cpu",
				Type: "system-cpu",
				Config: map[string]interface{}{
					"warningLoadFactor": -0.5,
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

func TestRegistryContainsCPUMonitor(t *testing.T) {
	// Test that the CPU monitor appears in the registry list
	registeredTypes := monitors.GetRegisteredTypes()

	found := false
	for _, monitorType := range registeredTypes {
		if monitorType == "system-cpu" {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("system-cpu monitor not found in registered types: %v", registeredTypes)
	}

	// Check if the monitor type is registered
	if !monitors.IsRegistered("system-cpu") {
		t.Error("system-cpu monitor is not registered according to IsRegistered check")
	}
}
