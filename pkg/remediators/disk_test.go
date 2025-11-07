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

// mockDiskExecutor is a mock implementation of DiskExecutor for testing.
type mockDiskExecutor struct {
	mu sync.Mutex

	// Configuration
	shouldFailCommand   bool
	shouldFailDiskUsage bool
	commandDelay        time.Duration
	usedGB              float64
	availableGB         float64
	totalGB             float64
	spaceReclaimedGB    float64 // Amount of space to reclaim after cleanup

	// Tracking
	executedCommands []string
	diskUsageCalls   int
	cleanupExecuted  bool
}

// ExecuteCommand executes a mock command.
func (m *mockDiskExecutor) ExecuteCommand(ctx context.Context, name string, args ...string) (string, error) {
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

	// Mark cleanup as executed and simulate space reclamation
	if !m.cleanupExecuted {
		m.cleanupExecuted = true
		m.usedGB -= m.spaceReclaimedGB
		m.availableGB += m.spaceReclaimedGB
	}

	// Return appropriate output based on command
	switch name {
	case "journalctl":
		return "Vacuumed 500.0M from journal logs", nil
	case "docker":
		if len(args) > 0 && args[0] == "image" {
			return "Deleted Images:\nsha256:abc123 100MB\nTotal reclaimed space: 100MB", nil
		} else if len(args) > 0 && args[0] == "system" {
			return "Total reclaimed space: 500MB", nil
		}
	case "find":
		return "", nil // find with -delete returns no output
	}

	return "success", nil
}

// GetDiskUsage returns mock disk usage information.
func (m *mockDiskExecutor) GetDiskUsage(ctx context.Context, path string) (used, available, total float64, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.diskUsageCalls++

	if m.shouldFailDiskUsage {
		return 0, 0, 0, fmt.Errorf("mock disk usage error")
	}

	return m.usedGB, m.availableGB, m.totalGB, nil
}

// TestNewDiskRemediator tests the constructor validation.
func TestNewDiskRemediator(t *testing.T) {
	tests := []struct {
		name    string
		config  DiskConfig
		wantErr bool
	}{
		{
			name: "valid clean journal logs config",
			config: DiskConfig{
				Operation:         DiskCleanJournalLogs,
				JournalVacuumSize: "500M",
			},
			wantErr: false,
		},
		{
			name: "valid clean docker images config",
			config: DiskConfig{
				Operation: DiskCleanDockerImages,
			},
			wantErr: false,
		},
		{
			name: "valid clean tmp config",
			config: DiskConfig{
				Operation:  DiskCleanTmp,
				TmpFileAge: 7,
			},
			wantErr: false,
		},
		{
			name: "valid clean container layers config",
			config: DiskConfig{
				Operation: DiskCleanContainerLayers,
			},
			wantErr: false,
		},
		{
			name: "valid config with min free space",
			config: DiskConfig{
				Operation:      DiskCleanJournalLogs,
				MinFreeSpaceGB: 1.0,
			},
			wantErr: false,
		},
		{
			name: "invalid operation",
			config: DiskConfig{
				Operation: "invalid-op",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewDiskRemediator(tt.config)
			if tt.wantErr {
				if err == nil {
					t.Errorf("NewDiskRemediator() expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("NewDiskRemediator() unexpected error: %v", err)
				}
				if r == nil {
					t.Errorf("NewDiskRemediator() returned nil remediator")
				}
			}
		})
	}
}

// TestDiskRemediator_CleanJournalLogs tests journal log cleanup.
func TestDiskRemediator_CleanJournalLogs(t *testing.T) {
	config := DiskConfig{
		Operation:         DiskCleanJournalLogs,
		JournalVacuumSize: "500M",
	}

	r, err := NewDiskRemediator(config)
	if err != nil {
		t.Fatalf("NewDiskRemediator() error: %v", err)
	}

	mockExec := &mockDiskExecutor{
		usedGB:           30.0,
		availableGB:      20.0,
		totalGB:          50.0,
		spaceReclaimedGB: 0.5,
	}
	r.SetDiskExecutor(mockExec)

	problem := types.Problem{
		Type:     "disk-pressure",
		Resource: "/var/log/journal",
		Severity: types.ProblemCritical,
	}

	ctx := context.Background()
	err = r.Remediate(ctx, problem)
	if err != nil {
		t.Errorf("Remediate() unexpected error: %v", err)
	}

	// Verify journalctl was executed
	mockExec.mu.Lock()
	commands := mockExec.executedCommands
	mockExec.mu.Unlock()

	found := false
	for _, cmd := range commands {
		if strings.Contains(cmd, "journalctl") && strings.Contains(cmd, "--vacuum-size=500M") {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("journalctl command was not executed")
	}
}

// TestDiskRemediator_CleanDockerImages tests Docker image cleanup.
func TestDiskRemediator_CleanDockerImages(t *testing.T) {
	config := DiskConfig{
		Operation: DiskCleanDockerImages,
	}

	r, err := NewDiskRemediator(config)
	if err != nil {
		t.Fatalf("NewDiskRemediator() error: %v", err)
	}

	mockExec := &mockDiskExecutor{
		usedGB:           30.0,
		availableGB:      20.0,
		totalGB:          50.0,
		spaceReclaimedGB: 2.0,
	}
	r.SetDiskExecutor(mockExec)

	problem := types.Problem{
		Type:     "disk-pressure",
		Resource: "/var/lib/docker",
		Severity: types.ProblemCritical,
	}

	ctx := context.Background()
	err = r.Remediate(ctx, problem)
	if err != nil {
		t.Errorf("Remediate() unexpected error: %v", err)
	}

	// Verify docker image prune was executed
	mockExec.mu.Lock()
	commands := mockExec.executedCommands
	mockExec.mu.Unlock()

	found := false
	for _, cmd := range commands {
		if strings.Contains(cmd, "docker image prune -a -f") {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("docker image prune command was not executed")
	}
}

// TestDiskRemediator_CleanTmp tests /tmp cleanup.
func TestDiskRemediator_CleanTmp(t *testing.T) {
	config := DiskConfig{
		Operation:  DiskCleanTmp,
		TmpFileAge: 7,
	}

	r, err := NewDiskRemediator(config)
	if err != nil {
		t.Fatalf("NewDiskRemediator() error: %v", err)
	}

	mockExec := &mockDiskExecutor{
		usedGB:           30.0,
		availableGB:      20.0,
		totalGB:          50.0,
		spaceReclaimedGB: 1.0,
	}
	r.SetDiskExecutor(mockExec)

	problem := types.Problem{
		Type:     "disk-pressure",
		Resource: "/tmp",
		Severity: types.ProblemWarning,
	}

	ctx := context.Background()
	err = r.Remediate(ctx, problem)
	if err != nil {
		t.Errorf("Remediate() unexpected error: %v", err)
	}

	// Verify find command was executed
	mockExec.mu.Lock()
	commands := mockExec.executedCommands
	mockExec.mu.Unlock()

	found := false
	for _, cmd := range commands {
		if strings.Contains(cmd, "find /tmp") && strings.Contains(cmd, "-mtime +7") && strings.Contains(cmd, "-delete") {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("find command with -delete was not executed")
	}
}

// TestDiskRemediator_CleanContainerLayers tests container layer cleanup.
func TestDiskRemediator_CleanContainerLayers(t *testing.T) {
	config := DiskConfig{
		Operation: DiskCleanContainerLayers,
	}

	r, err := NewDiskRemediator(config)
	if err != nil {
		t.Fatalf("NewDiskRemediator() error: %v", err)
	}

	mockExec := &mockDiskExecutor{
		usedGB:           30.0,
		availableGB:      20.0,
		totalGB:          50.0,
		spaceReclaimedGB: 3.0,
	}
	r.SetDiskExecutor(mockExec)

	problem := types.Problem{
		Type:     "disk-pressure",
		Resource: "/var/lib/docker",
		Severity: types.ProblemCritical,
	}

	ctx := context.Background()
	err = r.Remediate(ctx, problem)
	if err != nil {
		t.Errorf("Remediate() unexpected error: %v", err)
	}

	// Verify docker system prune was executed
	mockExec.mu.Lock()
	commands := mockExec.executedCommands
	mockExec.mu.Unlock()

	found := false
	for _, cmd := range commands {
		if strings.Contains(cmd, "docker system prune -a -f --volumes") {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("docker system prune command was not executed")
	}
}

// TestDiskRemediator_MinFreeSpace tests minimum free space check.
func TestDiskRemediator_MinFreeSpace(t *testing.T) {
	tests := []struct {
		name        string
		minFreeGB   float64
		availableGB float64
		wantErr     bool
	}{
		{
			name:        "sufficient free space",
			minFreeGB:   5.0,
			availableGB: 10.0,
			wantErr:     false,
		},
		{
			name:        "insufficient free space",
			minFreeGB:   10.0,
			availableGB: 5.0,
			wantErr:     true,
		},
		{
			name:        "no minimum check (0)",
			minFreeGB:   0.0,
			availableGB: 1.0,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DiskConfig{
				Operation:      DiskCleanJournalLogs,
				MinFreeSpaceGB: tt.minFreeGB,
			}

			r, err := NewDiskRemediator(config)
			if err != nil {
				t.Fatalf("NewDiskRemediator() error: %v", err)
			}

			mockExec := &mockDiskExecutor{
				usedGB:           30.0,
				availableGB:      tt.availableGB,
				totalGB:          50.0,
				spaceReclaimedGB: 0.5,
			}
			r.SetDiskExecutor(mockExec)

			problem := types.Problem{
				Type:     "disk-pressure",
				Resource: "/",
				Severity: types.ProblemCritical,
			}

			ctx := context.Background()
			err = r.Remediate(ctx, problem)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Remediate() expected error for insufficient space, got nil")
				}
				if err != nil && !strings.Contains(err.Error(), "insufficient free space") {
					t.Errorf("Remediate() expected 'insufficient free space' error, got: %v", err)
				}
			} else {
				if err != nil {
					t.Errorf("Remediate() unexpected error: %v", err)
				}
			}
		})
	}
}

// TestDiskRemediator_VerifyDiskSpace tests disk space verification after cleanup.
func TestDiskRemediator_VerifyDiskSpace(t *testing.T) {
	config := DiskConfig{
		Operation:   DiskCleanJournalLogs,
		VerifyAfter: true,
	}

	r, err := NewDiskRemediator(config)
	if err != nil {
		t.Fatalf("NewDiskRemediator() error: %v", err)
	}

	mockExec := &mockDiskExecutor{
		usedGB:           30.0,
		availableGB:      20.0,
		totalGB:          50.0,
		spaceReclaimedGB: 2.0, // Reclaim 2GB
	}
	r.SetDiskExecutor(mockExec)

	problem := types.Problem{
		Type:     "disk-pressure",
		Resource: "/",
		Severity: types.ProblemCritical,
	}

	ctx := context.Background()
	err = r.Remediate(ctx, problem)
	if err != nil {
		t.Errorf("Remediate() unexpected error: %v", err)
	}

	// Verify disk usage was checked multiple times (before and after)
	mockExec.mu.Lock()
	usageCalls := mockExec.diskUsageCalls
	mockExec.mu.Unlock()

	if usageCalls < 2 {
		t.Errorf("Disk usage should be checked at least twice (before and after), got %d calls", usageCalls)
	}
}

// TestDiskRemediator_VerifyDiskSpace_NoSpaceReclaimed tests verification when minimal space is reclaimed.
func TestDiskRemediator_VerifyDiskSpace_NoSpaceReclaimed(t *testing.T) {
	config := DiskConfig{
		Operation:   DiskCleanJournalLogs,
		VerifyAfter: true,
	}

	r, err := NewDiskRemediator(config)
	if err != nil {
		t.Fatalf("NewDiskRemediator() error: %v", err)
	}

	mockExec := &mockDiskExecutor{
		usedGB:           30.0,
		availableGB:      20.0,
		totalGB:          50.0,
		spaceReclaimedGB: 0.0, // No space reclaimed
	}
	r.SetDiskExecutor(mockExec)

	problem := types.Problem{
		Type:     "disk-pressure",
		Resource: "/",
		Severity: types.ProblemCritical,
	}

	ctx := context.Background()
	err = r.Remediate(ctx, problem)
	// Should succeed even if minimal space reclaimed (just logs warning)
	if err != nil {
		t.Errorf("Remediate() unexpected error: %v", err)
	}
}

// TestDiskRemediator_ExecutionFailure tests command execution failure.
func TestDiskRemediator_ExecutionFailure(t *testing.T) {
	config := DiskConfig{
		Operation: DiskCleanJournalLogs,
	}

	r, err := NewDiskRemediator(config)
	if err != nil {
		t.Fatalf("NewDiskRemediator() error: %v", err)
	}

	mockExec := &mockDiskExecutor{
		usedGB:            30.0,
		availableGB:       20.0,
		totalGB:           50.0,
		shouldFailCommand: true,
	}
	r.SetDiskExecutor(mockExec)

	problem := types.Problem{
		Type:     "disk-pressure",
		Resource: "/",
		Severity: types.ProblemCritical,
	}

	ctx := context.Background()
	err = r.Remediate(ctx, problem)
	if err == nil {
		t.Errorf("Remediate() expected error for command failure, got nil")
	}
}

// TestDiskRemediator_DryRun tests dry-run mode.
func TestDiskRemediator_DryRun(t *testing.T) {
	config := DiskConfig{
		Operation: DiskCleanJournalLogs,
		DryRun:    true,
	}

	r, err := NewDiskRemediator(config)
	if err != nil {
		t.Fatalf("NewDiskRemediator() error: %v", err)
	}

	mockExec := &mockDiskExecutor{
		usedGB:      30.0,
		availableGB: 20.0,
		totalGB:     50.0,
	}
	r.SetDiskExecutor(mockExec)

	problem := types.Problem{
		Type:     "disk-pressure",
		Resource: "/",
		Severity: types.ProblemCritical,
	}

	ctx := context.Background()
	err = r.Remediate(ctx, problem)
	if err != nil {
		t.Errorf("Remediate() unexpected error in dry-run: %v", err)
	}

	// Verify no cleanup commands were executed in dry-run mode
	mockExec.mu.Lock()
	commands := mockExec.executedCommands
	mockExec.mu.Unlock()

	if len(commands) != 0 {
		t.Errorf("Cleanup commands should not be executed in dry-run mode, got %d commands", len(commands))
	}
}

// TestDiskRemediator_ContextCancellation tests context cancellation.
func TestDiskRemediator_ContextCancellation(t *testing.T) {
	config := DiskConfig{
		Operation: DiskCleanJournalLogs,
	}

	r, err := NewDiskRemediator(config)
	if err != nil {
		t.Fatalf("NewDiskRemediator() error: %v", err)
	}

	mockExec := &mockDiskExecutor{
		usedGB:       30.0,
		availableGB:  20.0,
		totalGB:      50.0,
		commandDelay: 2 * time.Second,
	}
	r.SetDiskExecutor(mockExec)

	problem := types.Problem{
		Type:     "disk-pressure",
		Resource: "/",
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

// TestDiskRemediator_Cooldown tests cooldown behavior.
func TestDiskRemediator_Cooldown(t *testing.T) {
	config := DiskConfig{
		Operation: DiskCleanJournalLogs,
	}

	r, err := NewDiskRemediator(config)
	if err != nil {
		t.Fatalf("NewDiskRemediator() error: %v", err)
	}

	// Set a very short cooldown for testing
	r.cooldown = 100 * time.Millisecond

	mockExec := &mockDiskExecutor{
		usedGB:      30.0,
		availableGB: 20.0,
		totalGB:     50.0,
	}
	r.SetDiskExecutor(mockExec)

	problem := types.Problem{
		Type:     "disk-pressure",
		Resource: "/",
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

// TestDiskRemediator_MultipleOperations tests all disk cleanup operations.
func TestDiskRemediator_MultipleOperations(t *testing.T) {
	operations := []struct {
		name      string
		operation DiskOperation
		config    DiskConfig
	}{
		{
			name:      "clean-journal-logs",
			operation: DiskCleanJournalLogs,
			config: DiskConfig{
				Operation:         DiskCleanJournalLogs,
				JournalVacuumSize: "500M",
			},
		},
		{
			name:      "clean-docker-images",
			operation: DiskCleanDockerImages,
			config: DiskConfig{
				Operation: DiskCleanDockerImages,
			},
		},
		{
			name:      "clean-tmp",
			operation: DiskCleanTmp,
			config: DiskConfig{
				Operation:  DiskCleanTmp,
				TmpFileAge: 7,
			},
		},
		{
			name:      "clean-container-layers",
			operation: DiskCleanContainerLayers,
			config: DiskConfig{
				Operation: DiskCleanContainerLayers,
			},
		},
	}

	for _, op := range operations {
		t.Run(op.name, func(t *testing.T) {
			r, err := NewDiskRemediator(op.config)
			if err != nil {
				t.Fatalf("NewDiskRemediator() error: %v", err)
			}

			mockExec := &mockDiskExecutor{
				usedGB:           30.0,
				availableGB:      20.0,
				totalGB:          50.0,
				spaceReclaimedGB: 1.0,
			}
			r.SetDiskExecutor(mockExec)

			problem := types.Problem{
				Type:     "disk-pressure",
				Resource: "/",
				Severity: types.ProblemCritical,
			}

			ctx := context.Background()
			err = r.Remediate(ctx, problem)
			if err != nil {
				t.Errorf("Remediate() for %s failed: %v", op.name, err)
			}

			// Verify operation-specific command was executed
			mockExec.mu.Lock()
			commands := mockExec.executedCommands
			mockExec.mu.Unlock()

			if len(commands) == 0 {
				t.Errorf("No commands executed for %s", op.name)
			}
		})
	}
}

// TestDiskRemediator_DiskUsageFailure tests disk usage check failure.
func TestDiskRemediator_DiskUsageFailure(t *testing.T) {
	config := DiskConfig{
		Operation: DiskCleanJournalLogs,
	}

	r, err := NewDiskRemediator(config)
	if err != nil {
		t.Fatalf("NewDiskRemediator() error: %v", err)
	}

	mockExec := &mockDiskExecutor{
		shouldFailDiskUsage: true,
	}
	r.SetDiskExecutor(mockExec)

	problem := types.Problem{
		Type:     "disk-pressure",
		Resource: "/",
		Severity: types.ProblemCritical,
	}

	ctx := context.Background()
	err = r.Remediate(ctx, problem)
	// Should still succeed even if disk usage check fails (just logs warning)
	if err != nil {
		t.Errorf("Remediate() unexpected error: %v", err)
	}
}

// TestDiskRemediator_DefaultConfig tests default configuration values.
func TestDiskRemediator_DefaultConfig(t *testing.T) {
	tests := []struct {
		name           string
		config         DiskConfig
		expectedTarget string
		expectedAge    int
		expectedSize   string
	}{
		{
			name: "default target path",
			config: DiskConfig{
				Operation: DiskCleanJournalLogs,
			},
			expectedTarget: "/",
			expectedSize:   "500M",
		},
		{
			name: "default tmp file age",
			config: DiskConfig{
				Operation: DiskCleanTmp,
			},
			expectedAge: 7,
		},
		{
			name: "default journal vacuum size",
			config: DiskConfig{
				Operation: DiskCleanJournalLogs,
			},
			expectedSize: "500M",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewDiskRemediator(tt.config)
			if err != nil {
				t.Fatalf("NewDiskRemediator() error: %v", err)
			}

			if tt.expectedTarget != "" && r.config.TargetPath != tt.expectedTarget {
				t.Errorf("Expected TargetPath %s, got %s", tt.expectedTarget, r.config.TargetPath)
			}

			if tt.expectedAge != 0 && r.config.TmpFileAge != tt.expectedAge {
				t.Errorf("Expected TmpFileAge %d, got %d", tt.expectedAge, r.config.TmpFileAge)
			}

			if tt.expectedSize != "" && r.config.JournalVacuumSize != tt.expectedSize {
				t.Errorf("Expected JournalVacuumSize %s, got %s", tt.expectedSize, r.config.JournalVacuumSize)
			}
		})
	}
}
