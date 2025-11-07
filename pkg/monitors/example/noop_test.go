package example

import (
	"context"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/monitors"
	"github.com/supporttools/node-doctor/pkg/types"
)

func TestNoOpMonitor_Registration(t *testing.T) {
	// Test that the noop monitor is properly registered
	if !monitors.IsRegistered("noop") {
		t.Error("noop monitor should be registered")
	}

	info := monitors.GetMonitorInfo("noop")
	if info == nil {
		t.Fatal("noop monitor info should not be nil")
	}

	if info.Type != "noop" {
		t.Errorf("Expected type 'noop', got %q", info.Type)
	}
	if info.Factory == nil {
		t.Error("noop monitor factory should not be nil")
	}
	if info.Validator == nil {
		t.Error("noop monitor validator should not be nil")
	}
	if info.Description == "" {
		t.Error("noop monitor description should not be empty")
	}
}

func TestNoOpMonitor_ValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  types.MonitorConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: types.MonitorConfig{
				Name:    "test-noop",
				Type:    "noop",
				Enabled: true,
				Config: map[string]interface{}{
					"interval":      "5s",
					"testMessage":   "Test message",
					"includeEvents": true,
				},
			},
			wantErr: false,
		},
		{
			name: "valid minimal config",
			config: types.MonitorConfig{
				Name:    "test-noop",
				Type:    "noop",
				Enabled: true,
			},
			wantErr: false,
		},
		{
			name: "empty name",
			config: types.MonitorConfig{
				Name: "",
				Type: "noop",
			},
			wantErr: true,
			errMsg:  "name is required",
		},
		{
			name: "wrong type",
			config: types.MonitorConfig{
				Name: "test",
				Type: "wrong",
			},
			wantErr: true,
			errMsg:  "invalid monitor type",
		},
		{
			name: "invalid interval",
			config: types.MonitorConfig{
				Name: "test-noop",
				Type: "noop",
				Config: map[string]interface{}{
					"interval": "invalid",
				},
			},
			wantErr: true,
			errMsg:  "invalid interval",
		},
		{
			name: "negative interval",
			config: types.MonitorConfig{
				Name: "test-noop",
				Type: "noop",
				Config: map[string]interface{}{
					"interval": "-5s",
				},
			},
			wantErr: true,
			errMsg:  "interval must be positive",
		},
		{
			name: "too small interval",
			config: types.MonitorConfig{
				Name: "test-noop",
				Type: "noop",
				Config: map[string]interface{}{
					"interval": "500ms",
				},
			},
			wantErr: true,
			errMsg:  "interval too small",
		},
		{
			name: "invalid testMessage type",
			config: types.MonitorConfig{
				Name: "test-noop",
				Type: "noop",
				Config: map[string]interface{}{
					"testMessage": 123,
				},
			},
			wantErr: true,
			errMsg:  "testMessage must be a string",
		},
		{
			name: "invalid includeEvents type",
			config: types.MonitorConfig{
				Name: "test-noop",
				Type: "noop",
				Config: map[string]interface{}{
					"includeEvents": "true",
				},
			},
			wantErr: true,
			errMsg:  "includeEvents must be a boolean",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNoOpConfig(tt.config)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				} else if tt.errMsg != "" && err.Error() != tt.errMsg && !contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
			}
		})
	}
}

func TestNoOpMonitor_Creation(t *testing.T) {
	tests := []struct {
		name    string
		config  types.MonitorConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "successful creation",
			config: types.MonitorConfig{
				Name:    "test-noop",
				Type:    "noop",
				Enabled: true,
				Config: map[string]interface{}{
					"interval":      "2s",
					"testMessage":   "Custom test message",
					"includeEvents": true,
				},
			},
			wantErr: false,
		},
		{
			name: "creation with defaults",
			config: types.MonitorConfig{
				Name:    "test-noop",
				Type:    "noop",
				Enabled: true,
			},
			wantErr: false,
		},
		{
			name: "invalid interval",
			config: types.MonitorConfig{
				Name: "test-noop",
				Type: "noop",
				Config: map[string]interface{}{
					"interval": "invalid",
				},
			},
			wantErr: true,
			errMsg:  "invalid interval",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			monitor, err := NewNoOpMonitor(ctx, tt.config)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				} else if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
				if monitor == nil {
					t.Error("Expected monitor, got nil")
				}

				// Test monitor properties
				if noopMonitor, ok := monitor.(*NoOpMonitor); ok {
					if noopMonitor.GetName() != tt.config.Name {
						t.Errorf("Expected name %q, got %q", tt.config.Name, noopMonitor.GetName())
					}
					if noopMonitor.IsRunning() {
						t.Error("Monitor should not be running initially")
					}
				} else {
					t.Errorf("Expected *NoOpMonitor, got %T", monitor)
				}
			}
		})
	}
}

func TestNoOpMonitor_StartStop(t *testing.T) {
	config := types.MonitorConfig{
		Name:    "test-noop",
		Type:    "noop",
		Enabled: true,
		Config: map[string]interface{}{
			"interval":      "100ms", // Fast for testing
			"testMessage":   "Test message",
			"includeEvents": true,
		},
	}

	ctx := context.Background()
	monitor, err := NewNoOpMonitor(ctx, config)
	if err != nil {
		t.Fatalf("Failed to create monitor: %v", err)
	}

	noopMonitor := monitor.(*NoOpMonitor)

	// Test initial state
	if noopMonitor.IsRunning() {
		t.Error("Monitor should not be running initially")
	}

	// Start the monitor
	statusCh, err := monitor.Start()
	if err != nil {
		t.Fatalf("Failed to start monitor: %v", err)
	}

	if !noopMonitor.IsRunning() {
		t.Error("Monitor should be running after start")
	}

	// Test double start
	_, err = monitor.Start()
	if err == nil {
		t.Error("Expected error on double start")
	}

	// Collect some status updates
	var statuses []*types.Status
	timeout := time.After(500 * time.Millisecond)
	done := false

	for !done && len(statuses) < 3 {
		select {
		case status := <-statusCh:
			if status != nil {
				statuses = append(statuses, status)
			}
		case <-timeout:
			done = true
		}
	}

	// Stop the monitor
	monitor.Stop()

	// Test state after stop
	if noopMonitor.IsRunning() {
		t.Error("Monitor should not be running after stop")
	}

	// Verify we received status updates
	if len(statuses) == 0 {
		t.Error("Expected at least one status update")
	}

	// Verify status content
	for i, status := range statuses {
		if status.Source != config.Name {
			t.Errorf("Status %d: expected source %q, got %q", i, config.Name, status.Source)
		}

		if len(status.Conditions) == 0 {
			t.Errorf("Status %d: expected at least one condition", i)
		} else {
			condition := status.Conditions[0]
			if condition.Type != "NoOpMonitorReady" {
				t.Errorf("Status %d: expected condition type 'NoOpMonitorReady', got %q", i, condition.Type)
			}
			if condition.Status != types.ConditionTrue {
				t.Errorf("Status %d: expected condition status True, got %q", i, condition.Status)
			}
		}

		// Since includeEvents is true, we should have events
		if len(status.Events) == 0 {
			t.Errorf("Status %d: expected at least one event when includeEvents is true", i)
		} else {
			event := status.Events[0]
			if event.Severity != types.EventInfo {
				t.Errorf("Status %d: expected event severity Info, got %q", i, event.Severity)
			}
			if event.Reason != "PeriodicCheck" {
				t.Errorf("Status %d: expected event reason 'PeriodicCheck', got %q", i, event.Reason)
			}
		}
	}

	// Channel should be closed after stop
	select {
	case _, ok := <-statusCh:
		if ok {
			t.Error("Status channel should be closed after stop")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Status channel should be closed immediately after stop")
	}
}

func TestNoOpMonitor_WithoutEvents(t *testing.T) {
	config := types.MonitorConfig{
		Name:    "test-noop-no-events",
		Type:    "noop",
		Enabled: true,
		Config: map[string]interface{}{
			"interval":      "50ms",
			"includeEvents": false, // No events
		},
	}

	ctx := context.Background()
	monitor, err := NewNoOpMonitor(ctx, config)
	if err != nil {
		t.Fatalf("Failed to create monitor: %v", err)
	}

	statusCh, err := monitor.Start()
	if err != nil {
		t.Fatalf("Failed to start monitor: %v", err)
	}

	// Get one status update
	var status *types.Status
	select {
	case status = <-statusCh:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Timeout waiting for status")
	}

	monitor.Stop()

	// Should have conditions but no events
	if len(status.Conditions) == 0 {
		t.Error("Expected at least one condition")
	}
	if len(status.Events) != 0 {
		t.Errorf("Expected no events when includeEvents is false, got %d", len(status.Events))
	}
}

func TestNoOpMonitor_RegistryIntegration(t *testing.T) {
	// Test that the monitor can be created through the registry
	config := types.MonitorConfig{
		Name:    "registry-test",
		Type:    "noop",
		Enabled: true,
		Config: map[string]interface{}{
			"interval": "1s",
		},
	}

	// Validate through registry
	err := monitors.ValidateConfig(config)
	if err != nil {
		t.Errorf("Registry validation failed: %v", err)
	}

	// Create through registry
	ctx := context.Background()
	monitor, err := monitors.CreateMonitor(ctx, config)
	if err != nil {
		t.Errorf("Registry creation failed: %v", err)
	}

	if monitor == nil {
		t.Fatal("Registry returned nil monitor")
	}

	// Verify it's the right type
	if _, ok := monitor.(*NoOpMonitor); !ok {
		t.Errorf("Expected *NoOpMonitor, got %T", monitor)
	}

	// Test basic functionality
	statusCh, err := monitor.Start()
	if err != nil {
		t.Errorf("Failed to start monitor: %v", err)
	}

	// Quick check for status
	select {
	case status := <-statusCh:
		if status.Source != config.Name {
			t.Errorf("Expected source %q, got %q", config.Name, status.Source)
		}
	case <-time.After(100 * time.Millisecond):
		// Don't fail if no status immediately available
	}

	monitor.Stop()
}

func TestNoOpMonitor_ConfigParsing(t *testing.T) {
	tests := []struct {
		name        string
		configMap   map[string]interface{}
		wantErr     bool
		errMsg      string
		expectValue func(t *testing.T, config *NoOpConfig)
	}{
		{
			name:      "nil config",
			configMap: nil,
			wantErr:   false,
			expectValue: func(t *testing.T, config *NoOpConfig) {
				if config.Interval != "" {
					t.Errorf("Expected empty interval, got %q", config.Interval)
				}
				if config.TestMessage != "" {
					t.Errorf("Expected empty test message, got %q", config.TestMessage)
				}
				if config.IncludeEvents {
					t.Error("Expected includeEvents false")
				}
			},
		},
		{
			name: "valid config",
			configMap: map[string]interface{}{
				"interval":      "5s",
				"testMessage":   "Test",
				"includeEvents": true,
			},
			wantErr: false,
			expectValue: func(t *testing.T, config *NoOpConfig) {
				if config.Interval != "5s" {
					t.Errorf("Expected interval '5s', got %q", config.Interval)
				}
				if config.TestMessage != "Test" {
					t.Errorf("Expected test message 'Test', got %q", config.TestMessage)
				}
				if !config.IncludeEvents {
					t.Error("Expected includeEvents true")
				}
			},
		},
		{
			name: "invalid interval type",
			configMap: map[string]interface{}{
				"interval": 123,
			},
			wantErr: true,
			errMsg:  "interval must be a string",
		},
		{
			name: "invalid testMessage type",
			configMap: map[string]interface{}{
				"testMessage": 123,
			},
			wantErr: true,
			errMsg:  "testMessage must be a string",
		},
		{
			name: "invalid includeEvents type",
			configMap: map[string]interface{}{
				"includeEvents": "yes",
			},
			wantErr: true,
			errMsg:  "includeEvents must be a boolean",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := parseNoOpConfig(tt.configMap)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				} else if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
				if config == nil {
					t.Fatal("Expected config, got nil")
				}
				if tt.expectValue != nil {
					tt.expectValue(t, config)
				}
			}
		})
	}
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > len(substr) &&
			(s[:len(substr)] == substr ||
				s[len(s)-len(substr):] == substr ||
				hasSubstring(s, substr))))
}

func hasSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
