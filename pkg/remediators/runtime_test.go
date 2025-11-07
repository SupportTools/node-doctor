package remediators

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// mockRuntimeExecutor is a mock implementation of RuntimeExecutor for testing.
type mockRuntimeExecutor struct {
	mu sync.Mutex

	// Configuration
	shouldFailCommand   bool
	shouldFailAvailable bool
	availableRuntimes   map[RuntimeType]bool
	daemonActive        bool
	commandDelay        time.Duration

	// Tracking
	executedCommands []string
	availableChecks  int
}

// ExecuteCommand executes a mock command.
func (m *mockRuntimeExecutor) ExecuteCommand(ctx context.Context, name string, args ...string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	command := fmt.Sprintf("%s %s", name, strings.Join(args, " "))
	m.executedCommands = append(m.executedCommands, command)

	if m.commandDelay > 0 {
		select {
		case <-time.After(m.commandDelay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	if m.shouldFailCommand {
		return "command failed", fmt.Errorf("mock command error")
	}

	// Handle specific commands
	switch name {
	case "systemctl":
		if len(args) >= 2 {
			action := args[0]
			if action == "restart" {
				m.daemonActive = true
				return "success", nil
			} else if action == "is-active" {
				if m.daemonActive {
					return "active", nil
				}
				return "inactive", fmt.Errorf("service is not active")
			}
		}
	case "docker":
		if len(args) > 0 {
			if args[0] == "version" {
				return "Docker version 20.10.0", nil
			} else if args[0] == "container" && len(args) > 1 && args[1] == "prune" {
				return "Deleted Containers:\nabc123\nTotal reclaimed space: 500MB", nil
			} else if args[0] == "volume" && len(args) > 1 && args[1] == "prune" {
				return "Deleted Volumes:\nvol1\nTotal reclaimed space: 1GB", nil
			}
		}
	case "ctr":
		if len(args) > 0 && args[0] == "version" {
			return "containerd version 1.5.0", nil
		}
	case "crictl":
		if len(args) > 0 && args[0] == "version" {
			return "crictl version 1.22.0", nil
		} else if args[0] == "rmp" {
			return "Removed containers: 3", nil
		}
	}

	return "success", nil
}

// IsRuntimeAvailable checks if a mock runtime is available.
func (m *mockRuntimeExecutor) IsRuntimeAvailable(ctx context.Context, runtime RuntimeType) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.availableChecks++

	if m.shouldFailAvailable {
		return false, fmt.Errorf("mock availability check error")
	}

	if m.availableRuntimes == nil {
		return false, nil
	}

	return m.availableRuntimes[runtime], nil
}

// GetSystemdServiceName returns the mock systemd service name.
func (m *mockRuntimeExecutor) GetSystemdServiceName(runtime RuntimeType) string {
	switch runtime {
	case RuntimeDocker:
		return "docker"
	case RuntimeContainerd:
		return "containerd"
	case RuntimeCRIO:
		return "crio"
	default:
		return ""
	}
}

// TestNewRuntimeRemediator tests the constructor validation.
func TestNewRuntimeRemediator(t *testing.T) {
	tests := []struct {
		name    string
		config  RuntimeConfig
		wantErr bool
	}{
		{
			name: "valid restart daemon config with docker",
			config: RuntimeConfig{
				Operation:   RuntimeRestartDaemon,
				RuntimeType: RuntimeDocker,
			},
			wantErr: false,
		},
		{
			name: "valid clean containers config with containerd",
			config: RuntimeConfig{
				Operation:   RuntimeCleanContainers,
				RuntimeType: RuntimeContainerd,
			},
			wantErr: false,
		},
		{
			name: "valid prune volumes config with docker",
			config: RuntimeConfig{
				Operation:   RuntimePruneVolumes,
				RuntimeType: RuntimeDocker,
			},
			wantErr: false,
		},
		{
			name: "valid config with auto-detect",
			config: RuntimeConfig{
				Operation:   RuntimeRestartDaemon,
				RuntimeType: RuntimeAuto,
			},
			wantErr: false,
		},
		{
			name: "invalid operation",
			config: RuntimeConfig{
				Operation:   "invalid-op",
				RuntimeType: RuntimeDocker,
			},
			wantErr: true,
		},
		{
			name: "invalid runtime type",
			config: RuntimeConfig{
				Operation:   RuntimeRestartDaemon,
				RuntimeType: "invalid-runtime",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewRuntimeRemediator(tt.config)
			if tt.wantErr {
				if err == nil {
					t.Errorf("NewRuntimeRemediator() expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("NewRuntimeRemediator() unexpected error: %v", err)
				}
				if r == nil {
					t.Errorf("NewRuntimeRemediator() returned nil remediator")
				}
			}
		})
	}
}

// TestRuntimeRemediator_RestartDaemon tests daemon restart.
func TestRuntimeRemediator_RestartDaemon(t *testing.T) {
	tests := []struct {
		name        string
		runtimeType RuntimeType
		serviceName string
	}{
		{
			name:        "restart docker daemon",
			runtimeType: RuntimeDocker,
			serviceName: "docker",
		},
		{
			name:        "restart containerd daemon",
			runtimeType: RuntimeContainerd,
			serviceName: "containerd",
		},
		{
			name:        "restart crio daemon",
			runtimeType: RuntimeCRIO,
			serviceName: "crio",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := RuntimeConfig{
				Operation:   RuntimeRestartDaemon,
				RuntimeType: tt.runtimeType,
			}

			r, err := NewRuntimeRemediator(config)
			if err != nil {
				t.Fatalf("NewRuntimeRemediator() error: %v", err)
			}

			mockExec := &mockRuntimeExecutor{
				daemonActive: false,
			}
			r.SetRuntimeExecutor(mockExec)

			problem := types.Problem{
				Type:     "runtime-unhealthy",
				Resource: string(tt.runtimeType),
				Severity: types.ProblemCritical,
			}

			ctx := context.Background()
			err = r.Remediate(ctx, problem)
			if err != nil {
				t.Errorf("Remediate() unexpected error: %v", err)
			}

			// Verify systemctl restart was executed
			mockExec.mu.Lock()
			commands := mockExec.executedCommands
			mockExec.mu.Unlock()

			found := false
			for _, cmd := range commands {
				if strings.Contains(cmd, "systemctl restart "+tt.serviceName) {
					found = true
					break
				}
			}

			if !found {
				t.Errorf("systemctl restart %s command was not executed", tt.serviceName)
			}

			// Verify daemon is now active
			if !mockExec.daemonActive {
				t.Errorf("Daemon should be active after restart")
			}
		})
	}
}

// TestRuntimeRemediator_CleanContainers tests container cleanup.
func TestRuntimeRemediator_CleanContainers(t *testing.T) {
	tests := []struct {
		name        string
		runtimeType RuntimeType
		expectCmd   string
	}{
		{
			name:        "clean docker containers",
			runtimeType: RuntimeDocker,
			expectCmd:   "docker container prune -f",
		},
		{
			name:        "clean containerd containers",
			runtimeType: RuntimeContainerd,
			expectCmd:   "ctr containers",
		},
		{
			name:        "clean crio containers",
			runtimeType: RuntimeCRIO,
			expectCmd:   "crictl rmp -a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := RuntimeConfig{
				Operation:   RuntimeCleanContainers,
				RuntimeType: tt.runtimeType,
			}

			r, err := NewRuntimeRemediator(config)
			if err != nil {
				t.Fatalf("NewRuntimeRemediator() error: %v", err)
			}

			mockExec := &mockRuntimeExecutor{}
			r.SetRuntimeExecutor(mockExec)

			problem := types.Problem{
				Type:     "disk-pressure",
				Resource: "/var/lib/" + string(tt.runtimeType),
				Severity: types.ProblemWarning,
			}

			ctx := context.Background()
			err = r.Remediate(ctx, problem)
			if err != nil {
				t.Errorf("Remediate() unexpected error: %v", err)
			}

			// Verify appropriate container cleanup command was executed
			mockExec.mu.Lock()
			commands := mockExec.executedCommands
			mockExec.mu.Unlock()

			found := false
			for _, cmd := range commands {
				if strings.Contains(cmd, tt.expectCmd) {
					found = true
					break
				}
			}

			if !found {
				t.Errorf("Container cleanup command not found, expected to contain: %s", tt.expectCmd)
			}
		})
	}
}

// TestRuntimeRemediator_PruneVolumes tests volume pruning.
func TestRuntimeRemediator_PruneVolumes(t *testing.T) {
	config := RuntimeConfig{
		Operation:   RuntimePruneVolumes,
		RuntimeType: RuntimeDocker,
	}

	r, err := NewRuntimeRemediator(config)
	if err != nil {
		t.Fatalf("NewRuntimeRemediator() error: %v", err)
	}

	mockExec := &mockRuntimeExecutor{}
	r.SetRuntimeExecutor(mockExec)

	problem := types.Problem{
		Type:     "disk-pressure",
		Resource: "/var/lib/docker",
		Severity: types.ProblemWarning,
	}

	ctx := context.Background()
	err = r.Remediate(ctx, problem)
	if err != nil {
		t.Errorf("Remediate() unexpected error: %v", err)
	}

	// Verify docker volume prune was executed
	mockExec.mu.Lock()
	commands := mockExec.executedCommands
	mockExec.mu.Unlock()

	found := false
	for _, cmd := range commands {
		if strings.Contains(cmd, "docker volume prune -f") {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("docker volume prune command was not executed")
	}
}

// TestRuntimeRemediator_PruneVolumes_NonDocker tests volume pruning skips for non-Docker runtimes.
func TestRuntimeRemediator_PruneVolumes_NonDocker(t *testing.T) {
	config := RuntimeConfig{
		Operation:   RuntimePruneVolumes,
		RuntimeType: RuntimeContainerd,
	}

	r, err := NewRuntimeRemediator(config)
	if err != nil {
		t.Fatalf("NewRuntimeRemediator() error: %v", err)
	}

	mockExec := &mockRuntimeExecutor{}
	r.SetRuntimeExecutor(mockExec)

	problem := types.Problem{
		Type:     "disk-pressure",
		Resource: "/var/lib/containerd",
		Severity: types.ProblemWarning,
	}

	ctx := context.Background()
	err = r.Remediate(ctx, problem)
	if err != nil {
		t.Errorf("Remediate() unexpected error: %v", err)
	}

	// Verify no volume prune command was executed (only for Docker)
	mockExec.mu.Lock()
	commands := mockExec.executedCommands
	mockExec.mu.Unlock()

	if len(commands) != 0 {
		t.Errorf("No commands should be executed for non-Docker volume prune, got %d commands", len(commands))
	}
}

// TestRuntimeRemediator_AutoDetect tests runtime auto-detection.
func TestRuntimeRemediator_AutoDetect(t *testing.T) {
	tests := []struct {
		name              string
		availableRuntimes map[RuntimeType]bool
		expectedRuntime   RuntimeType
		wantErr           bool
	}{
		{
			name: "detect docker",
			availableRuntimes: map[RuntimeType]bool{
				RuntimeDocker: true,
			},
			expectedRuntime: RuntimeDocker,
			wantErr:         false,
		},
		{
			name: "detect containerd when docker not available",
			availableRuntimes: map[RuntimeType]bool{
				RuntimeDocker:     false,
				RuntimeContainerd: true,
			},
			expectedRuntime: RuntimeContainerd,
			wantErr:         false,
		},
		{
			name: "detect crio when docker and containerd not available",
			availableRuntimes: map[RuntimeType]bool{
				RuntimeDocker:     false,
				RuntimeContainerd: false,
				RuntimeCRIO:       true,
			},
			expectedRuntime: RuntimeCRIO,
			wantErr:         false,
		},
		{
			name: "fail when no runtime available",
			availableRuntimes: map[RuntimeType]bool{
				RuntimeDocker:     false,
				RuntimeContainerd: false,
				RuntimeCRIO:       false,
			},
			expectedRuntime: "",
			wantErr:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := RuntimeConfig{
				Operation:   RuntimeRestartDaemon,
				RuntimeType: RuntimeAuto,
			}

			r, err := NewRuntimeRemediator(config)
			if err != nil {
				t.Fatalf("NewRuntimeRemediator() error: %v", err)
			}

			mockExec := &mockRuntimeExecutor{
				availableRuntimes: tt.availableRuntimes,
				daemonActive:      true,
			}
			r.SetRuntimeExecutor(mockExec)

			problem := types.Problem{
				Type:     "runtime-unhealthy",
				Resource: "runtime",
				Severity: types.ProblemCritical,
			}

			ctx := context.Background()
			err = r.Remediate(ctx, problem)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Remediate() expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Remediate() unexpected error: %v", err)
				}

				// Verify correct runtime was detected
				detected := r.GetDetectedRuntime()
				if detected != tt.expectedRuntime {
					t.Errorf("Expected runtime %s, got %s", tt.expectedRuntime, detected)
				}
			}
		})
	}
}

// TestRuntimeRemediator_VerifyDaemon tests daemon verification.
func TestRuntimeRemediator_VerifyDaemon(t *testing.T) {
	config := RuntimeConfig{
		Operation:   RuntimeRestartDaemon,
		RuntimeType: RuntimeDocker,
		VerifyAfter: true,
	}

	r, err := NewRuntimeRemediator(config)
	if err != nil {
		t.Fatalf("NewRuntimeRemediator() error: %v", err)
	}

	mockExec := &mockRuntimeExecutor{
		daemonActive: false,
	}
	r.SetRuntimeExecutor(mockExec)

	problem := types.Problem{
		Type:     "runtime-unhealthy",
		Resource: "docker",
		Severity: types.ProblemCritical,
	}

	ctx := context.Background()
	err = r.Remediate(ctx, problem)
	if err != nil {
		t.Errorf("Remediate() unexpected error: %v", err)
	}

	// Verify is-active check was executed
	mockExec.mu.Lock()
	commands := mockExec.executedCommands
	mockExec.mu.Unlock()

	foundRestart := false
	foundVerify := false
	for _, cmd := range commands {
		if strings.Contains(cmd, "systemctl restart docker") {
			foundRestart = true
		}
		if strings.Contains(cmd, "systemctl is-active docker") {
			foundVerify = true
		}
	}

	if !foundRestart {
		t.Errorf("systemctl restart command was not executed")
	}
	if !foundVerify {
		t.Errorf("systemctl is-active command was not executed")
	}
}

// TestRuntimeRemediator_ExecutionFailure tests command execution failure.
func TestRuntimeRemediator_ExecutionFailure(t *testing.T) {
	config := RuntimeConfig{
		Operation:   RuntimeRestartDaemon,
		RuntimeType: RuntimeDocker,
	}

	r, err := NewRuntimeRemediator(config)
	if err != nil {
		t.Fatalf("NewRuntimeRemediator() error: %v", err)
	}

	mockExec := &mockRuntimeExecutor{
		shouldFailCommand: true,
	}
	r.SetRuntimeExecutor(mockExec)

	problem := types.Problem{
		Type:     "runtime-unhealthy",
		Resource: "docker",
		Severity: types.ProblemCritical,
	}

	ctx := context.Background()
	err = r.Remediate(ctx, problem)
	if err == nil {
		t.Errorf("Remediate() expected error for command failure, got nil")
	}
}

// TestRuntimeRemediator_DryRun tests dry-run mode.
func TestRuntimeRemediator_DryRun(t *testing.T) {
	config := RuntimeConfig{
		Operation:   RuntimeRestartDaemon,
		RuntimeType: RuntimeDocker,
		DryRun:      true,
	}

	r, err := NewRuntimeRemediator(config)
	if err != nil {
		t.Fatalf("NewRuntimeRemediator() error: %v", err)
	}

	mockExec := &mockRuntimeExecutor{}
	r.SetRuntimeExecutor(mockExec)

	problem := types.Problem{
		Type:     "runtime-unhealthy",
		Resource: "docker",
		Severity: types.ProblemCritical,
	}

	ctx := context.Background()
	err = r.Remediate(ctx, problem)
	if err != nil {
		t.Errorf("Remediate() unexpected error in dry-run: %v", err)
	}

	// Verify no commands were executed in dry-run mode
	mockExec.mu.Lock()
	commands := mockExec.executedCommands
	mockExec.mu.Unlock()

	if len(commands) != 0 {
		t.Errorf("Commands should not be executed in dry-run mode, got %d commands", len(commands))
	}
}

// TestRuntimeRemediator_ContextCancellation tests context cancellation.
func TestRuntimeRemediator_ContextCancellation(t *testing.T) {
	config := RuntimeConfig{
		Operation:   RuntimeRestartDaemon,
		RuntimeType: RuntimeDocker,
	}

	r, err := NewRuntimeRemediator(config)
	if err != nil {
		t.Fatalf("NewRuntimeRemediator() error: %v", err)
	}

	mockExec := &mockRuntimeExecutor{
		commandDelay: 2 * time.Second,
	}
	r.SetRuntimeExecutor(mockExec)

	problem := types.Problem{
		Type:     "runtime-unhealthy",
		Resource: "docker",
		Severity: types.ProblemCritical,
	}

	// Create a context that will be cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err = r.Remediate(ctx, problem)
	if err == nil {
		t.Errorf("Remediate() expected error for cancelled context, got nil")
	}
}

// TestRuntimeRemediator_Cooldown tests cooldown behavior.
func TestRuntimeRemediator_Cooldown(t *testing.T) {
	config := RuntimeConfig{
		Operation:   RuntimeRestartDaemon,
		RuntimeType: RuntimeDocker,
	}

	r, err := NewRuntimeRemediator(config)
	if err != nil {
		t.Fatalf("NewRuntimeRemediator() error: %v", err)
	}

	// Set a very short cooldown for testing
	r.cooldown = 100 * time.Millisecond

	mockExec := &mockRuntimeExecutor{
		daemonActive: true,
	}
	r.SetRuntimeExecutor(mockExec)

	problem := types.Problem{
		Type:     "runtime-unhealthy",
		Resource: "docker",
		Severity: types.ProblemCritical,
	}

	ctx := context.Background()

	// First remediation should succeed
	err = r.Remediate(ctx, problem)
	if err != nil {
		t.Errorf("First remediation failed: %v", err)
	}

	// Second remediation should be blocked by cooldown
	if r.CanRemediate(problem) {
		t.Errorf("CanRemediate() should return false during cooldown")
	}

	// Wait for cooldown to expire
	time.Sleep(150 * time.Millisecond)

	// Third remediation should succeed after cooldown
	if !r.CanRemediate(problem) {
		t.Errorf("CanRemediate() should return true after cooldown expires")
	}
}

// TestRuntimeRemediator_MultipleOperations tests all runtime operations.
func TestRuntimeRemediator_MultipleOperations(t *testing.T) {
	operations := []struct {
		name      string
		operation RuntimeOperation
		config    RuntimeConfig
	}{
		{
			name:      "restart-daemon",
			operation: RuntimeRestartDaemon,
			config: RuntimeConfig{
				Operation:   RuntimeRestartDaemon,
				RuntimeType: RuntimeDocker,
			},
		},
		{
			name:      "clean-containers",
			operation: RuntimeCleanContainers,
			config: RuntimeConfig{
				Operation:   RuntimeCleanContainers,
				RuntimeType: RuntimeDocker,
			},
		},
		{
			name:      "prune-volumes",
			operation: RuntimePruneVolumes,
			config: RuntimeConfig{
				Operation:   RuntimePruneVolumes,
				RuntimeType: RuntimeDocker,
			},
		},
	}

	for _, op := range operations {
		t.Run(op.name, func(t *testing.T) {
			r, err := NewRuntimeRemediator(op.config)
			if err != nil {
				t.Fatalf("NewRuntimeRemediator() error: %v", err)
			}

			mockExec := &mockRuntimeExecutor{
				daemonActive: true,
			}
			r.SetRuntimeExecutor(mockExec)

			problem := types.Problem{
				Type:     "runtime-issue",
				Resource: "docker",
				Severity: types.ProblemWarning,
			}

			ctx := context.Background()
			err = r.Remediate(ctx, problem)
			if err != nil {
				t.Errorf("Remediate() for %s failed: %v", op.name, err)
			}

			// Verify operation-specific command was executed (except prune-volumes which may skip)
			mockExec.mu.Lock()
			commands := mockExec.executedCommands
			mockExec.mu.Unlock()

			// All operations except volume prune should execute commands
			if op.operation != RuntimePruneVolumes && len(commands) == 0 {
				t.Errorf("No commands executed for %s", op.name)
			}
		})
	}
}
