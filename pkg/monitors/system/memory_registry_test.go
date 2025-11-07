package system

import (
	"context"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/monitors"
	"github.com/supporttools/node-doctor/pkg/types"
)

func TestMemoryMonitorRegistration(t *testing.T) {
	// Test that the Memory monitor is properly registered
	info := monitors.GetMonitorInfo("system-memory")
	if info == nil {
		t.Fatal("system-memory monitor is not registered")
	}

	if info.Type != "system-memory" {
		t.Errorf("Monitor type: got %q, want %q", info.Type, "system-memory")
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

	expectedDescription := "Monitors memory usage, swap usage, and out-of-memory conditions"
	if info.Description != expectedDescription {
		t.Errorf("Monitor description: got %q, want %q", info.Description, expectedDescription)
	}
}

func TestMemoryMonitorFactoryViaRegistry(t *testing.T) {
	// Test creating Memory monitor through the registry
	config := types.MonitorConfig{
		Name:     "registry-test-memory",
		Type:     "system-memory",
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Config: map[string]interface{}{
			"warningThreshold":  85.0,
			"criticalThreshold": 95.0,
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

	// Verify we can cast it to MemoryMonitor
	memoryMonitor, ok := monitor.(*MemoryMonitor)
	if !ok {
		t.Error("monitor is not a MemoryMonitor")
	}

	if memoryMonitor.name != "registry-test-memory" {
		t.Errorf("monitor name: got %q, want %q", memoryMonitor.name, "registry-test-memory")
	}
}

func TestMemoryMonitorValidatorViaRegistry(t *testing.T) {
	tests := []struct {
		name      string
		config    types.MonitorConfig
		wantError bool
	}{
		{
			name: "valid config",
			config: types.MonitorConfig{
				Name: "test-memory",
				Type: "system-memory",
				Config: map[string]interface{}{
					"warningThreshold":  85.0,
					"criticalThreshold": 95.0,
				},
			},
			wantError: false,
		},
		{
			name: "invalid config - negative threshold",
			config: types.MonitorConfig{
				Name: "test-memory",
				Type: "system-memory",
				Config: map[string]interface{}{
					"warningThreshold": -0.5,
				},
			},
			wantError: true,
		},
		{
			name: "invalid config - warning >= critical",
			config: types.MonitorConfig{
				Name: "test-memory",
				Type: "system-memory",
				Config: map[string]interface{}{
					"warningThreshold":  95.0,
					"criticalThreshold": 85.0,
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

func TestRegistryContainsMemoryMonitor(t *testing.T) {
	// Test that the Memory monitor appears in the registry list
	registeredTypes := monitors.GetRegisteredTypes()

	found := false
	for _, monitorType := range registeredTypes {
		if monitorType == "system-memory" {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("system-memory monitor not found in registered types: %v", registeredTypes)
	}

	// Check if the monitor type is registered
	if !monitors.IsRegistered("system-memory") {
		t.Error("system-memory monitor is not registered according to IsRegistered check")
	}
}
