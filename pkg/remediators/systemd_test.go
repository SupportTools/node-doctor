package remediators

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// mockSystemdExecutor is a mock implementation of SystemdExecutor for testing.
type mockSystemdExecutor struct {
	mu sync.Mutex

	// Configuration
	shouldFailExecution bool
	shouldFailStatus    bool
	serviceActive       bool
	executionDelay      time.Duration
	preventActivation   bool // If true, restart/start won't automatically activate service

	// Tracking
	executedCommands  []string
	isActiveCallCount int

	// For simulating service becoming active after restart
	becomeActiveAfter int // Number of IsActive calls before returning true
	activeCallCount   int
}

func (m *mockSystemdExecutor) ExecuteSystemctl(ctx context.Context, args ...string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Record the command
	m.executedCommands = append(m.executedCommands, fmt.Sprintf("systemctl %v", args))

	// Simulate execution delay
	if m.executionDelay > 0 {
		select {
		case <-time.After(m.executionDelay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	if m.shouldFailExecution {
		return "execution failed", fmt.Errorf("mock execution error")
	}

	// Simulate successful execution
	if !m.preventActivation {
		switch args[0] {
		case "restart", "start":
			m.serviceActive = true
		case "stop":
			m.serviceActive = false
		case "reload":
			// Reload doesn't change service active state
		}
	}

	return "success", nil
}

func (m *mockSystemdExecutor) IsActive(ctx context.Context, serviceName string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.isActiveCallCount++

	if m.shouldFailStatus {
		return false, fmt.Errorf("mock status check error")
	}

	// Simulate service becoming active after N calls
	if m.becomeActiveAfter > 0 {
		m.activeCallCount++
		if m.activeCallCount >= m.becomeActiveAfter {
			m.serviceActive = true
		}
	}

	return m.serviceActive, nil
}

func (m *mockSystemdExecutor) getExecutedCommands() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]string, len(m.executedCommands))
	copy(result, m.executedCommands)
	return result
}

func (m *mockSystemdExecutor) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.executedCommands = nil
	m.isActiveCallCount = 0
	m.activeCallCount = 0
}

// TestNewSystemdRemediator tests the constructor.
func TestNewSystemdRemediator(t *testing.T) {
	tests := []struct {
		name    string
		config  SystemdConfig
		wantErr bool
	}{
		{
			name: "valid restart config",
			config: SystemdConfig{
				Operation:   SystemdRestart,
				ServiceName: "kubelet",
			},
			wantErr: false,
		},
		{
			name: "valid start config",
			config: SystemdConfig{
				Operation:   SystemdStart,
				ServiceName: "docker",
			},
			wantErr: false,
		},
		{
			name: "valid stop config",
			config: SystemdConfig{
				Operation:   SystemdStop,
				ServiceName: "containerd",
			},
			wantErr: false,
		},
		{
			name: "valid reload config",
			config: SystemdConfig{
				Operation:   SystemdReload,
				ServiceName: "kubelet",
			},
			wantErr: false,
		},
		{
			name: "missing service name",
			config: SystemdConfig{
				Operation: SystemdRestart,
			},
			wantErr: true,
		},
		{
			name: "invalid operation",
			config: SystemdConfig{
				Operation:   "invalid",
				ServiceName: "kubelet",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			remediator, err := NewSystemdRemediator(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewSystemdRemediator() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && remediator == nil {
				t.Error("NewSystemdRemediator() returned nil remediator")
			}
		})
	}
}

// TestSystemdRemediator_Restart tests the restart operation.
func TestSystemdRemediator_Restart(t *testing.T) {
	config := SystemdConfig{
		Operation:   SystemdRestart,
		ServiceName: "kubelet",
	}

	remediator, err := NewSystemdRemediator(config)
	if err != nil {
		t.Fatalf("Failed to create remediator: %v", err)
	}

	mockExec := &mockSystemdExecutor{
		serviceActive: false,
	}
	remediator.SetSystemdExecutor(mockExec)

	problem := types.Problem{
		Type:       "systemd-service-failed",
		Severity:   types.ProblemCritical,
		Message:    "Kubelet service failed",
		Resource:   "kubelet.service",
		DetectedAt: time.Now(),
	}

	err = remediator.Remediate(context.Background(), problem)
	if err != nil {
		t.Fatalf("Remediation failed: %v", err)
	}

	commands := mockExec.getExecutedCommands()
	if len(commands) != 1 {
		t.Fatalf("Expected 1 command, got %d", len(commands))
	}

	expected := "systemctl [restart kubelet]"
	if commands[0] != expected {
		t.Errorf("Expected command %q, got %q", expected, commands[0])
	}
}

// TestSystemdRemediator_Stop tests the stop operation.
func TestSystemdRemediator_Stop(t *testing.T) {
	config := SystemdConfig{
		Operation:   SystemdStop,
		ServiceName: "docker",
	}

	remediator, err := NewSystemdRemediator(config)
	if err != nil {
		t.Fatalf("Failed to create remediator: %v", err)
	}

	mockExec := &mockSystemdExecutor{
		serviceActive: true,
	}
	remediator.SetSystemdExecutor(mockExec)

	problem := types.Problem{
		Type:       "systemd-service-problem",
		Severity:   types.ProblemWarning,
		Message:    "Docker service needs to stop",
		Resource:   "docker.service",
		DetectedAt: time.Now(),
	}

	err = remediator.Remediate(context.Background(), problem)
	if err != nil {
		t.Fatalf("Remediation failed: %v", err)
	}

	commands := mockExec.getExecutedCommands()
	if len(commands) != 1 {
		t.Fatalf("Expected 1 command, got %d", len(commands))
	}

	expected := "systemctl [stop docker]"
	if commands[0] != expected {
		t.Errorf("Expected command %q, got %q", expected, commands[0])
	}

	// Verify service is no longer active
	if mockExec.serviceActive {
		t.Error("Service should be inactive after stop")
	}
}

// TestSystemdRemediator_Start tests the start operation.
func TestSystemdRemediator_Start(t *testing.T) {
	config := SystemdConfig{
		Operation:   SystemdStart,
		ServiceName: "containerd",
	}

	remediator, err := NewSystemdRemediator(config)
	if err != nil {
		t.Fatalf("Failed to create remediator: %v", err)
	}

	mockExec := &mockSystemdExecutor{
		serviceActive: false,
	}
	remediator.SetSystemdExecutor(mockExec)

	problem := types.Problem{
		Type:       "systemd-service-stopped",
		Severity:   types.ProblemCritical,
		Message:    "Containerd service stopped",
		Resource:   "containerd.service",
		DetectedAt: time.Now(),
	}

	err = remediator.Remediate(context.Background(), problem)
	if err != nil {
		t.Fatalf("Remediation failed: %v", err)
	}

	commands := mockExec.getExecutedCommands()
	if len(commands) != 1 {
		t.Fatalf("Expected 1 command, got %d", len(commands))
	}

	expected := "systemctl [start containerd]"
	if commands[0] != expected {
		t.Errorf("Expected command %q, got %q", expected, commands[0])
	}

	// Verify service is now active
	if !mockExec.serviceActive {
		t.Error("Service should be active after start")
	}
}

// TestSystemdRemediator_Reload tests the reload operation.
func TestSystemdRemediator_Reload(t *testing.T) {
	config := SystemdConfig{
		Operation:   SystemdReload,
		ServiceName: "kubelet",
	}

	remediator, err := NewSystemdRemediator(config)
	if err != nil {
		t.Fatalf("Failed to create remediator: %v", err)
	}

	mockExec := &mockSystemdExecutor{
		serviceActive: true,
	}
	remediator.SetSystemdExecutor(mockExec)

	problem := types.Problem{
		Type:       "config-changed",
		Severity:   types.ProblemInfo,
		Message:    "Kubelet config changed",
		Resource:   "kubelet.service",
		DetectedAt: time.Now(),
	}

	err = remediator.Remediate(context.Background(), problem)
	if err != nil {
		t.Fatalf("Remediation failed: %v", err)
	}

	commands := mockExec.getExecutedCommands()
	if len(commands) != 1 {
		t.Fatalf("Expected 1 command, got %d", len(commands))
	}

	expected := "systemctl [reload kubelet]"
	if commands[0] != expected {
		t.Errorf("Expected command %q, got %q", expected, commands[0])
	}
}

// TestSystemdRemediator_VerifyStatus tests status verification after remediation.
func TestSystemdRemediator_VerifyStatus(t *testing.T) {
	config := SystemdConfig{
		Operation:     SystemdRestart,
		ServiceName:   "kubelet",
		VerifyStatus:  true,
		VerifyTimeout: 5 * time.Second,
	}

	remediator, err := NewSystemdRemediator(config)
	if err != nil {
		t.Fatalf("Failed to create remediator: %v", err)
	}

	mockExec := &mockSystemdExecutor{
		serviceActive:     false,
		becomeActiveAfter: 2, // Service becomes active after 2 IsActive calls
	}
	remediator.SetSystemdExecutor(mockExec)

	problem := types.Problem{
		Type:       "systemd-service-failed",
		Severity:   types.ProblemCritical,
		Message:    "Kubelet service failed",
		Resource:   "kubelet.service",
		DetectedAt: time.Now(),
	}

	err = remediator.Remediate(context.Background(), problem)
	if err != nil {
		t.Fatalf("Remediation failed: %v", err)
	}

	// Verify that IsActive was called multiple times
	if mockExec.isActiveCallCount < 2 {
		t.Errorf("Expected at least 2 IsActive calls, got %d", mockExec.isActiveCallCount)
	}

	// Verify service is now active
	if !mockExec.serviceActive {
		t.Error("Service should be active after verification")
	}
}

// TestSystemdRemediator_VerifyStatusTimeout tests verification timeout.
func TestSystemdRemediator_VerifyStatusTimeout(t *testing.T) {
	config := SystemdConfig{
		Operation:     SystemdRestart,
		ServiceName:   "kubelet",
		VerifyStatus:  true,
		VerifyTimeout: 2 * time.Second,
	}

	remediator, err := NewSystemdRemediator(config)
	if err != nil {
		t.Fatalf("Failed to create remediator: %v", err)
	}

	mockExec := &mockSystemdExecutor{
		serviceActive:     false,
		preventActivation: true, // Service never becomes active even after restart
	}
	remediator.SetSystemdExecutor(mockExec)

	problem := types.Problem{
		Type:       "systemd-service-failed",
		Severity:   types.ProblemCritical,
		Message:    "Kubelet service failed",
		Resource:   "kubelet.service",
		DetectedAt: time.Now(),
	}

	err = remediator.Remediate(context.Background(), problem)
	if err == nil {
		t.Fatal("Expected error due to verification timeout")
	}

	if !contains(err.Error(), "timeout") {
		t.Errorf("Expected timeout error, got: %v", err)
	}
}

// TestSystemdRemediator_ExecutionFailure tests handling of execution failures.
func TestSystemdRemediator_ExecutionFailure(t *testing.T) {
	config := SystemdConfig{
		Operation:   SystemdRestart,
		ServiceName: "kubelet",
	}

	remediator, err := NewSystemdRemediator(config)
	if err != nil {
		t.Fatalf("Failed to create remediator: %v", err)
	}

	mockExec := &mockSystemdExecutor{
		shouldFailExecution: true,
	}
	remediator.SetSystemdExecutor(mockExec)

	problem := types.Problem{
		Type:       "systemd-service-failed",
		Severity:   types.ProblemCritical,
		Message:    "Kubelet service failed",
		Resource:   "kubelet.service",
		DetectedAt: time.Now(),
	}

	err = remediator.Remediate(context.Background(), problem)
	if err == nil {
		t.Fatal("Expected error due to execution failure")
	}

	if !contains(err.Error(), "failed to execute") {
		t.Errorf("Expected execution error, got: %v", err)
	}
}

// TestSystemdRemediator_DryRun tests dry-run mode.
func TestSystemdRemediator_DryRun(t *testing.T) {
	config := SystemdConfig{
		Operation:   SystemdRestart,
		ServiceName: "kubelet",
		DryRun:      true,
	}

	remediator, err := NewSystemdRemediator(config)
	if err != nil {
		t.Fatalf("Failed to create remediator: %v", err)
	}

	mockExec := &mockSystemdExecutor{}
	remediator.SetSystemdExecutor(mockExec)

	problem := types.Problem{
		Type:       "systemd-service-failed",
		Severity:   types.ProblemCritical,
		Message:    "Kubelet service failed",
		Resource:   "kubelet.service",
		DetectedAt: time.Now(),
	}

	err = remediator.Remediate(context.Background(), problem)
	if err != nil {
		t.Fatalf("Dry-run remediation failed: %v", err)
	}

	// Verify no commands were executed
	commands := mockExec.getExecutedCommands()
	if len(commands) != 0 {
		t.Errorf("Expected no commands in dry-run mode, got %d", len(commands))
	}
}

// TestSystemdRemediator_ContextCancellation tests context cancellation during remediation.
func TestSystemdRemediator_ContextCancellation(t *testing.T) {
	config := SystemdConfig{
		Operation:   SystemdRestart,
		ServiceName: "kubelet",
	}

	remediator, err := NewSystemdRemediator(config)
	if err != nil {
		t.Fatalf("Failed to create remediator: %v", err)
	}

	mockExec := &mockSystemdExecutor{
		executionDelay: 5 * time.Second, // Long delay to allow cancellation
	}
	remediator.SetSystemdExecutor(mockExec)

	problem := types.Problem{
		Type:       "systemd-service-failed",
		Severity:   types.ProblemCritical,
		Message:    "Kubelet service failed",
		Resource:   "kubelet.service",
		DetectedAt: time.Now(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err = remediator.Remediate(ctx, problem)
	if err == nil {
		t.Fatal("Expected error due to context cancellation")
	}

	if !contains(err.Error(), "context") {
		t.Errorf("Expected context error, got: %v", err)
	}
}

// TestSystemdRemediator_Cooldown tests cooldown behavior.
func TestSystemdRemediator_Cooldown(t *testing.T) {
	config := SystemdConfig{
		Operation:   SystemdRestart,
		ServiceName: "kubelet",
	}

	remediator, err := NewSystemdRemediator(config)
	if err != nil {
		t.Fatalf("Failed to create remediator: %v", err)
	}

	// Verify cooldown is set to CooldownMedium (5 minutes)
	if remediator.GetCooldown() != CooldownMedium {
		t.Errorf("Expected cooldown %v, got %v", CooldownMedium, remediator.GetCooldown())
	}

	mockExec := &mockSystemdExecutor{}
	remediator.SetSystemdExecutor(mockExec)

	problem := types.Problem{
		Type:       "systemd-service-failed",
		Severity:   types.ProblemCritical,
		Message:    "Kubelet service failed",
		Resource:   "kubelet.service",
		DetectedAt: time.Now(),
	}

	// First remediation should succeed
	err = remediator.Remediate(context.Background(), problem)
	if err != nil {
		t.Fatalf("First remediation failed: %v", err)
	}

	// Second immediate remediation should be blocked by CanRemediate
	if remediator.CanRemediate(problem) {
		t.Error("Expected CanRemediate to return false due to cooldown")
	}

	// Clear cooldown for testing
	remediator.ClearCooldown(problem)

	// Now CanRemediate should return true
	if !remediator.CanRemediate(problem) {
		t.Error("Expected CanRemediate to return true after clearing cooldown")
	}
}

// TestSystemdRemediator_MultipleServices tests different service configurations.
func TestSystemdRemediator_MultipleServices(t *testing.T) {
	services := []string{"kubelet", "docker", "containerd"}

	for _, service := range services {
		t.Run(service, func(t *testing.T) {
			config := SystemdConfig{
				Operation:   SystemdRestart,
				ServiceName: service,
			}

			remediator, err := NewSystemdRemediator(config)
			if err != nil {
				t.Fatalf("Failed to create remediator for %s: %v", service, err)
			}

			mockExec := &mockSystemdExecutor{}
			remediator.SetSystemdExecutor(mockExec)

			problem := types.Problem{
				Type:       "systemd-service-failed",
				Severity:   types.ProblemCritical,
				Message:    fmt.Sprintf("%s service failed", service),
				Resource:   fmt.Sprintf("%s.service", service),
				DetectedAt: time.Now(),
			}

			err = remediator.Remediate(context.Background(), problem)
			if err != nil {
				t.Fatalf("Remediation failed for %s: %v", service, err)
			}

			commands := mockExec.getExecutedCommands()
			if len(commands) != 1 {
				t.Fatalf("Expected 1 command for %s, got %d", service, len(commands))
			}

			expected := fmt.Sprintf("systemctl [restart %s]", service)
			if commands[0] != expected {
				t.Errorf("Expected command %q for %s, got %q", expected, service, commands[0])
			}
		})
	}
}

// Helper function to check if a string contains a substring.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			containsMiddle(s, substr)))
}

func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
