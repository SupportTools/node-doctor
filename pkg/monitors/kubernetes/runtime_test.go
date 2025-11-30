package kubernetes

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// mockRuntimeClient implements RuntimeClient for testing.
type mockRuntimeClient struct {
	socketErr     error
	systemdActive bool
	systemdErr    error
	infoResult    *RuntimeInfo
	infoErr       error
	delay         time.Duration
}

func (m *mockRuntimeClient) CheckSocketConnectivity(ctx context.Context) error {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return m.socketErr
}

func (m *mockRuntimeClient) CheckSystemdStatus(ctx context.Context) (bool, error) {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	return m.systemdActive, m.systemdErr
}

func (m *mockRuntimeClient) GetRuntimeInfo(ctx context.Context) (*RuntimeInfo, error) {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return m.infoResult, m.infoErr
}

// createTestMonitor is a helper function to create a properly initialized RuntimeMonitor for testing.
// This uses NewRuntimeMonitorForTesting to skip actual runtime detection on the filesystem.
func createTestMonitor(t *testing.T, configMap map[string]interface{}, mockClient RuntimeClient) *RuntimeMonitor {
	t.Helper()

	monitorConfig := types.MonitorConfig{
		Name:     "test-runtime-monitor",
		Type:     "kubernetes-runtime-check",
		Interval: 10 * time.Second,
		Timeout:  5 * time.Second,
		Config:   configMap,
	}

	// Determine runtime type from config, default to docker for tests
	runtimeType := runtimeTypeDocker
	socket := "/var/run/docker.sock"
	service := "docker"

	if rt, ok := configMap["runtimeType"].(string); ok && rt != "" && rt != "auto" {
		switch rt {
		case "docker":
			runtimeType = runtimeTypeDocker
			socket = "/var/run/docker.sock"
			service = "docker"
		case "containerd":
			runtimeType = runtimeTypeContainerd
			socket = "/run/containerd/containerd.sock"
			service = "containerd"
		case "crio":
			runtimeType = runtimeTypeCrio
			socket = "/var/run/crio/crio.sock"
			service = "crio"
		}
	}

	monitor, err := NewRuntimeMonitorForTesting(monitorConfig, mockClient, runtimeType, socket, service)
	if err != nil {
		t.Fatalf("createTestMonitor() error: %v", err)
	}

	return monitor
}

// TestNewRuntimeMonitor tests monitor creation.
// Note: Some tests are environment-dependent and will be skipped if the required
// runtime is not available on the test machine.
func TestNewRuntimeMonitor(t *testing.T) {
	tests := []struct {
		name               string
		config             types.MonitorConfig
		wantErr            bool
		environmentDepends bool // true if test depends on runtime availability
		skipReason         string
	}{
		{
			name: "invalid monitor type",
			config: types.MonitorConfig{
				Name:     "runtime-test",
				Type:     "wrong-type",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Enabled:  true,
				Config:   map[string]interface{}{},
			},
			wantErr: true,
		},
		{
			name: "missing name",
			config: types.MonitorConfig{
				Name:     "",
				Type:     "kubernetes-runtime-check",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Enabled:  true,
				Config:   map[string]interface{}{},
			},
			wantErr: true,
		},
		{
			name: "valid config with docker runtime",
			config: types.MonitorConfig{
				Name:     "runtime-test",
				Type:     "kubernetes-runtime-check",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Enabled:  true,
				Config: map[string]interface{}{
					"runtimeType": "docker",
				},
			},
			wantErr:            false,
			environmentDepends: true,
			skipReason:         "docker socket not available",
		},
		{
			name: "valid config with containerd runtime",
			config: types.MonitorConfig{
				Name:     "runtime-test",
				Type:     "kubernetes-runtime-check",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Enabled:  true,
				Config: map[string]interface{}{
					"runtimeType":      "containerd",
					"failureThreshold": 5,
				},
			},
			wantErr:            false,
			environmentDepends: true,
			skipReason:         "containerd socket not available",
		},
		{
			name: "nil config with auto detection",
			config: types.MonitorConfig{
				Name:     "runtime-test",
				Type:     "kubernetes-runtime-check",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Enabled:  true,
				Config:   nil,
			},
			wantErr:            false,
			environmentDepends: true,
			skipReason:         "no container runtime available",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			_, err := NewRuntimeMonitor(ctx, tt.config)

			// For environment-dependent tests, skip if the expected error is due to runtime detection
			if tt.environmentDepends && err != nil && !tt.wantErr {
				// Check if the error is related to runtime detection (not a config error)
				errStr := err.Error()
				if strings.Contains(errStr, "no container runtime detected") ||
					strings.Contains(errStr, "socket not found") {
					t.Skipf("Skipping: %s", tt.skipReason)
				}
			}

			if (err != nil) != tt.wantErr {
				t.Errorf("NewRuntimeMonitor() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestParseRuntimeConfig tests configuration parsing.
func TestParseRuntimeConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]interface{}
		wantErr bool
		check   func(*testing.T, *RuntimeMonitorConfig)
	}{
		{
			name:    "nil config uses defaults",
			config:  nil,
			wantErr: false,
			check: func(t *testing.T, c *RuntimeMonitorConfig) {
				if c.RuntimeType != runtimeTypeAuto {
					t.Errorf("RuntimeType = %s, want %s", c.RuntimeType, runtimeTypeAuto)
				}
				if c.FailureThreshold != defaultRuntimeFailureThreshold {
					t.Errorf("FailureThreshold = %d, want %d", c.FailureThreshold, defaultRuntimeFailureThreshold)
				}
				if !c.CheckSocketConnectivity {
					t.Error("CheckSocketConnectivity should be true by default")
				}
			},
		},
		{
			name: "custom runtime type",
			config: map[string]interface{}{
				"runtimeType": "docker",
			},
			wantErr: false,
			check: func(t *testing.T, c *RuntimeMonitorConfig) {
				if c.RuntimeType != runtimeTypeDocker {
					t.Errorf("RuntimeType = %s, want %s", c.RuntimeType, runtimeTypeDocker)
				}
			},
		},
		{
			name: "invalid runtime type",
			config: map[string]interface{}{
				"runtimeType": "invalid",
			},
			wantErr: true,
		},
		{
			name: "custom failure threshold",
			config: map[string]interface{}{
				"failureThreshold": 5,
			},
			wantErr: false,
			check: func(t *testing.T, c *RuntimeMonitorConfig) {
				if c.FailureThreshold != 5 {
					t.Errorf("FailureThreshold = %d, want 5", c.FailureThreshold)
				}
			},
		},
		{
			name: "failure threshold as float",
			config: map[string]interface{}{
				"failureThreshold": 3.0,
			},
			wantErr: false,
			check: func(t *testing.T, c *RuntimeMonitorConfig) {
				if c.FailureThreshold != 3 {
					t.Errorf("FailureThreshold = %d, want 3", c.FailureThreshold)
				}
			},
		},
		{
			name: "invalid failure threshold - zero",
			config: map[string]interface{}{
				"failureThreshold": 0,
			},
			wantErr: true,
		},
		{
			name: "invalid failure threshold - negative",
			config: map[string]interface{}{
				"failureThreshold": -1,
			},
			wantErr: true,
		},
		{
			name: "timeout as string",
			config: map[string]interface{}{
				"timeout": "10s",
			},
			wantErr: false,
			check: func(t *testing.T, c *RuntimeMonitorConfig) {
				if c.Timeout != 10*time.Second {
					t.Errorf("Timeout = %v, want 10s", c.Timeout)
				}
			},
		},
		{
			name: "timeout as int (seconds)",
			config: map[string]interface{}{
				"timeout": 15,
			},
			wantErr: false,
			check: func(t *testing.T, c *RuntimeMonitorConfig) {
				if c.Timeout != 15*time.Second {
					t.Errorf("Timeout = %v, want 15s", c.Timeout)
				}
			},
		},
		{
			name: "custom socket paths",
			config: map[string]interface{}{
				"dockerSocket":     "/custom/docker.sock",
				"containerdSocket": "/custom/containerd.sock",
				"crioSocket":       "/custom/crio.sock",
			},
			wantErr: false,
			check: func(t *testing.T, c *RuntimeMonitorConfig) {
				if c.DockerSocket != "/custom/docker.sock" {
					t.Errorf("DockerSocket = %s, want /custom/docker.sock", c.DockerSocket)
				}
			},
		},
		{
			name: "disable checks",
			config: map[string]interface{}{
				"checkSocketConnectivity": false,
				"checkSystemdStatus":      false,
				"checkRuntimeInfo":        false,
			},
			wantErr: false,
			check: func(t *testing.T, c *RuntimeMonitorConfig) {
				if c.CheckSocketConnectivity {
					t.Error("CheckSocketConnectivity should be false")
				}
				if c.CheckSystemdStatus {
					t.Error("CheckSystemdStatus should be false")
				}
				if c.CheckRuntimeInfo {
					t.Error("CheckRuntimeInfo should be false")
				}
			},
		},
		{
			name: "invalid type for checkSocketConnectivity",
			config: map[string]interface{}{
				"checkSocketConnectivity": "true",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := parseRuntimeConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseRuntimeConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err == nil && tt.check != nil {
				tt.check(t, config)
			}
		})
	}
}

// TestRuntimeMonitor_AllChecksPass tests successful runtime checks.
func TestRuntimeMonitor_AllChecksPass(t *testing.T) {
	mockClient := &mockRuntimeClient{
		socketErr:     nil,
		systemdActive: true,
		systemdErr:    nil,
		infoResult:    &RuntimeInfo{Runtime: "docker", Version: "20.10.0"},
		infoErr:       nil,
	}

	monitor := createTestMonitor(t, map[string]interface{}{
		"runtimeType":             "docker",
		"checkSocketConnectivity": true,
		"checkSystemdStatus":      true,
		"checkRuntimeInfo":        true,
		"failureThreshold":        3,
	}, mockClient)

	ctx := context.Background()
	status, err := monitor.checkRuntime(ctx)

	if err != nil {
		t.Fatalf("checkRuntime() unexpected error: %v", err)
	}

	// Should have RuntimeHealthy event
	foundHealthy := false
	for _, event := range status.Events {
		if event.Reason == "RuntimeHealthy" {
			foundHealthy = true
			break
		}
	}
	if !foundHealthy {
		t.Error("Expected RuntimeHealthy event not found")
	}

	// Should not have any unhealthy condition
	for _, cond := range status.Conditions {
		if cond.Type == "ContainerRuntimeUnhealthy" && cond.Status == types.ConditionTrue {
			t.Error("Should not have ContainerRuntimeUnhealthy condition when healthy")
		}
	}
}

// TestRuntimeMonitor_SocketConnectivityFails tests socket connectivity failure.
func TestRuntimeMonitor_SocketConnectivityFails(t *testing.T) {
	mockClient := &mockRuntimeClient{
		socketErr: fmt.Errorf("connection refused"),
	}

	monitor := createTestMonitor(t, map[string]interface{}{
		"runtimeType":             "docker",
		"checkSocketConnectivity": true,
		"checkSystemdStatus":      false,
		"checkRuntimeInfo":        false,
		"failureThreshold":        3,
	}, mockClient)

	ctx := context.Background()
	status, err := monitor.checkRuntime(ctx)

	if err != nil {
		t.Fatalf("checkRuntime() unexpected error: %v", err)
	}

	// Should have RuntimeSocketUnreachable event
	foundEvent := false
	for _, event := range status.Events {
		if event.Reason == "RuntimeSocketUnreachable" {
			foundEvent = true
			break
		}
	}
	if !foundEvent {
		t.Error("Expected RuntimeSocketUnreachable event not found")
	}
}

// TestRuntimeMonitor_SystemdInactive tests systemd service inactive.
func TestRuntimeMonitor_SystemdInactive(t *testing.T) {
	mockClient := &mockRuntimeClient{
		systemdActive: false,
		systemdErr:    nil,
	}

	monitor := createTestMonitor(t, map[string]interface{}{
		"runtimeType":             "docker",
		"checkSocketConnectivity": false,
		"checkSystemdStatus":      true,
		"checkRuntimeInfo":        false,
		"failureThreshold":        3,
	}, mockClient)

	ctx := context.Background()
	status, err := monitor.checkRuntime(ctx)

	if err != nil {
		t.Fatalf("checkRuntime() unexpected error: %v", err)
	}

	// Should have RuntimeSystemdInactive event
	foundEvent := false
	for _, event := range status.Events {
		if event.Reason == "RuntimeSystemdInactive" {
			foundEvent = true
			break
		}
	}
	if !foundEvent {
		t.Error("Expected RuntimeSystemdInactive event not found")
	}
}

// TestRuntimeMonitor_FailureThreshold tests failure threshold tracking.
func TestRuntimeMonitor_FailureThreshold(t *testing.T) {
	mockClient := &mockRuntimeClient{
		socketErr: fmt.Errorf("connection refused"),
	}

	monitor := createTestMonitor(t, map[string]interface{}{
		"runtimeType":             "docker",
		"checkSocketConnectivity": true,
		"checkSystemdStatus":      false,
		"checkRuntimeInfo":        false,
		"failureThreshold":        3,
	}, mockClient)

	ctx := context.Background()

	// First failure - should not trigger condition
	status1, _ := monitor.checkRuntime(ctx)
	hasCondition := false
	for _, cond := range status1.Conditions {
		if cond.Type == "ContainerRuntimeUnhealthy" && cond.Status == types.ConditionTrue {
			hasCondition = true
		}
	}
	if hasCondition {
		t.Error("Should not have ContainerRuntimeUnhealthy condition after 1 failure")
	}

	// Second failure - should not trigger condition
	status2, _ := monitor.checkRuntime(ctx)
	hasCondition = false
	for _, cond := range status2.Conditions {
		if cond.Type == "ContainerRuntimeUnhealthy" && cond.Status == types.ConditionTrue {
			hasCondition = true
		}
	}
	if hasCondition {
		t.Error("Should not have ContainerRuntimeUnhealthy condition after 2 failures")
	}

	// Third failure - should trigger condition
	status3, _ := monitor.checkRuntime(ctx)
	hasCondition = false
	for _, cond := range status3.Conditions {
		if cond.Type == "ContainerRuntimeUnhealthy" && cond.Status == types.ConditionTrue {
			hasCondition = true
		}
	}
	if !hasCondition {
		t.Error("Should have ContainerRuntimeUnhealthy condition after 3 failures")
	}
}

// TestRuntimeMonitor_Recovery tests recovery after failures.
func TestRuntimeMonitor_Recovery(t *testing.T) {
	mockClient := &mockRuntimeClient{
		socketErr: fmt.Errorf("connection refused"),
	}

	monitor := createTestMonitor(t, map[string]interface{}{
		"runtimeType":             "docker",
		"checkSocketConnectivity": true,
		"checkSystemdStatus":      false,
		"checkRuntimeInfo":        false,
		"failureThreshold":        2,
	}, mockClient)

	ctx := context.Background()

	// Generate failures to reach threshold
	monitor.checkRuntime(ctx)
	status2, _ := monitor.checkRuntime(ctx)

	// Verify unhealthy condition is set
	hasUnhealthy := false
	for _, cond := range status2.Conditions {
		if cond.Type == "ContainerRuntimeUnhealthy" && cond.Status == types.ConditionTrue {
			hasUnhealthy = true
		}
	}
	if !hasUnhealthy {
		t.Error("Should have ContainerRuntimeUnhealthy condition after threshold")
	}

	// Fix the issue
	mockClient.socketErr = nil

	// Check should detect recovery
	statusRecovery, _ := monitor.checkRuntime(ctx)

	// Should have RuntimeRecovered event
	foundRecovery := false
	for _, event := range statusRecovery.Events {
		if event.Reason == "RuntimeRecovered" {
			foundRecovery = true
			break
		}
	}
	if !foundRecovery {
		t.Error("Expected RuntimeRecovered event not found")
	}

	// Should clear the unhealthy condition
	hasUnhealthyTrue := false
	for _, cond := range statusRecovery.Conditions {
		if cond.Type == "ContainerRuntimeUnhealthy" && cond.Status == types.ConditionTrue {
			hasUnhealthyTrue = true
		}
	}
	if hasUnhealthyTrue {
		t.Error("ContainerRuntimeUnhealthy condition should be cleared after recovery")
	}
}

// TestRuntimeMonitor_ContextCancellation tests context cancellation.
func TestRuntimeMonitor_ContextCancellation(t *testing.T) {
	mockClient := &mockRuntimeClient{
		delay: 2 * time.Second, // Longer than context timeout
	}

	monitor := createTestMonitor(t, map[string]interface{}{
		"runtimeType":             "docker",
		"checkSocketConnectivity": true,
		"checkSystemdStatus":      false,
		"checkRuntimeInfo":        false,
		"failureThreshold":        3,
	}, mockClient)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	status, err := monitor.checkRuntime(ctx)

	// Should complete (not hang)
	if err != nil {
		t.Fatalf("checkRuntime() unexpected error: %v", err)
	}

	// Status should be returned even with timeout
	if status == nil {
		t.Error("Expected status to be returned even with context cancellation")
	}
}

// TestRuntimeMonitor_ConcurrentChecks tests thread safety.
func TestRuntimeMonitor_ConcurrentChecks(t *testing.T) {
	mockClient := &mockRuntimeClient{
		socketErr:     nil,
		systemdActive: true,
		infoResult:    &RuntimeInfo{Runtime: "docker", Version: "1.0"},
		delay:         10 * time.Millisecond, // Small delay to increase chance of race
	}

	monitor := createTestMonitor(t, map[string]interface{}{
		"runtimeType":             "docker",
		"checkSocketConnectivity": true,
		"checkSystemdStatus":      true,
		"checkRuntimeInfo":        true,
		"failureThreshold":        3,
	}, mockClient)

	ctx := context.Background()

	// Run multiple checks concurrently
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			_, err := monitor.checkRuntime(ctx)
			if err != nil {
				t.Errorf("checkRuntime() unexpected error: %v", err)
			}
			done <- true
		}()
	}

	// Wait for all checks to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestValidateRuntimeConfig tests configuration validation.
func TestValidateRuntimeConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  types.MonitorConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: types.MonitorConfig{
				Name:     "runtime-test",
				Type:     "kubernetes-runtime-check",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Enabled:  true,
				Config: map[string]interface{}{
					"runtimeType": "auto",
				},
			},
			wantErr: false,
		},
		{
			name: "missing name",
			config: types.MonitorConfig{
				Name:     "",
				Type:     "kubernetes-runtime-check",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Enabled:  true,
				Config:   map[string]interface{}{},
			},
			wantErr: true,
		},
		{
			name: "wrong type",
			config: types.MonitorConfig{
				Name:     "runtime-test",
				Type:     "wrong-type",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Enabled:  true,
				Config:   map[string]interface{}{},
			},
			wantErr: true,
		},
		{
			name: "invalid runtime type in config",
			config: types.MonitorConfig{
				Name:     "runtime-test",
				Type:     "kubernetes-runtime-check",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Enabled:  true,
				Config: map[string]interface{}{
					"runtimeType": "invalid",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRuntimeConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRuntimeConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestApplyDefaults tests default application.
func TestApplyDefaults(t *testing.T) {
	tests := []struct {
		name    string
		config  *RuntimeMonitorConfig
		wantErr bool
	}{
		{
			name: "all checks enabled",
			config: &RuntimeMonitorConfig{
				CheckSocketConnectivity: true,
				CheckSystemdStatus:      true,
				CheckRuntimeInfo:        true,
			},
			wantErr: false,
		},
		{
			name: "only socket check enabled",
			config: &RuntimeMonitorConfig{
				CheckSocketConnectivity: true,
				CheckSystemdStatus:      false,
				CheckRuntimeInfo:        false,
			},
			wantErr: false,
		},
		{
			name: "no checks enabled",
			config: &RuntimeMonitorConfig{
				CheckSocketConnectivity: false,
				CheckSystemdStatus:      false,
				CheckRuntimeInfo:        false,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.applyDefaults()
			if (err != nil) != tt.wantErr {
				t.Errorf("applyDefaults() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestParseDuration tests duration parsing.
func TestParseDuration(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		want    time.Duration
		wantErr bool
	}{
		{
			name:    "string duration",
			input:   "5s",
			want:    5 * time.Second,
			wantErr: false,
		},
		{
			name:    "int seconds",
			input:   10,
			want:    10 * time.Second,
			wantErr: false,
		},
		{
			name:    "float seconds",
			input:   2.5,
			want:    2500 * time.Millisecond,
			wantErr: false,
		},
		{
			name:    "invalid type",
			input:   true,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDuration(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseDuration() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseDuration() = %v, want %v", got, tt.want)
			}
		})
	}
}
