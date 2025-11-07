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

// mockNetworkExecutor is a mock implementation of NetworkExecutor for testing.
type mockNetworkExecutor struct {
	mu sync.Mutex

	// Configuration
	shouldFailCommand   bool
	shouldFailInterface bool
	interfaceExists     bool
	interfaceUp         bool
	commandDelay        time.Duration
	preventInterfaceUp  bool // Prevents automatic interface up for timeout testing
	routingTable        string
	dnsFlushMethod      string // "resolvectl", "systemd-resolve", or "fail"

	// Tracking
	executedCommands []string
	interfaceChecks  int
	routingBackups   int

	// For simulating gradual interface up
	becomeUpAfter int
	upCheckCount  int
}

// ExecuteCommand executes a mock command.
func (m *mockNetworkExecutor) ExecuteCommand(ctx context.Context, name string, args ...string) (string, error) {
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
	case "resolvectl":
		if m.dnsFlushMethod == "resolvectl" {
			return "Flushed all caches.", nil
		}
		return "", fmt.Errorf("resolvectl not available")

	case "systemd-resolve":
		if m.dnsFlushMethod == "systemd-resolve" {
			return "Flushed all caches.", nil
		}
		return "", fmt.Errorf("systemd-resolve not available")

	case "ip":
		// Handle ip link set commands
		if len(args) >= 3 && args[0] == "link" && args[1] == "set" {
			if len(args) >= 4 {
				action := args[3]

				if !m.preventInterfaceUp {
					if action == "up" {
						m.interfaceUp = true
					} else if action == "down" {
						m.interfaceUp = false
					}
				}
			}
			return "success", nil
		}

		// Handle ip route commands
		if len(args) >= 2 && args[0] == "route" {
			if args[1] == "show" {
				return m.routingTable, nil
			}
			if args[1] == "flush" {
				return "success", nil
			}
		}
	}

	return "success", nil
}

// InterfaceExists checks if a mock interface exists.
func (m *mockNetworkExecutor) InterfaceExists(ctx context.Context, interfaceName string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.interfaceChecks++

	if m.shouldFailInterface {
		return false, fmt.Errorf("mock interface check error")
	}

	return m.interfaceExists, nil
}

// GetRoutingTable returns the mock routing table.
func (m *mockNetworkExecutor) GetRoutingTable(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.routingBackups++

	if m.shouldFailCommand {
		return "", fmt.Errorf("mock routing table error")
	}

	return m.routingTable, nil
}

// IsInterfaceUp checks if the mock interface is up.
func (m *mockNetworkExecutor) IsInterfaceUp(ctx context.Context, interfaceName string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.upCheckCount++

	if m.shouldFailInterface {
		return false, fmt.Errorf("mock interface status check error")
	}

	// Simulate gradual interface up
	if m.becomeUpAfter > 0 && m.upCheckCount >= m.becomeUpAfter {
		m.interfaceUp = true
	}

	return m.interfaceUp, nil
}

// TestNewNetworkRemediator tests the constructor validation.
func TestNewNetworkRemediator(t *testing.T) {
	tests := []struct {
		name    string
		config  NetworkConfig
		wantErr bool
	}{
		{
			name: "valid flush DNS config",
			config: NetworkConfig{
				Operation: NetworkFlushDNS,
			},
			wantErr: false,
		},
		{
			name: "valid restart interface config",
			config: NetworkConfig{
				Operation:     NetworkRestartInterface,
				InterfaceName: "eth0",
			},
			wantErr: false,
		},
		{
			name: "valid reset routing config",
			config: NetworkConfig{
				Operation:     NetworkResetRouting,
				BackupRouting: true,
			},
			wantErr: false,
		},
		{
			name: "invalid operation",
			config: NetworkConfig{
				Operation: "invalid-op",
			},
			wantErr: true,
		},
		{
			name: "restart interface without interface name",
			config: NetworkConfig{
				Operation: NetworkRestartInterface,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewNetworkRemediator(tt.config)
			if tt.wantErr {
				if err == nil {
					t.Errorf("NewNetworkRemediator() expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("NewNetworkRemediator() unexpected error: %v", err)
				}
				if r == nil {
					t.Errorf("NewNetworkRemediator() returned nil remediator")
				}
			}
		})
	}
}

// TestNetworkRemediator_FlushDNS tests DNS cache flushing.
func TestNetworkRemediator_FlushDNS(t *testing.T) {
	tests := []struct {
		name           string
		dnsFlushMethod string
		wantErr        bool
	}{
		{
			name:           "flush DNS with resolvectl",
			dnsFlushMethod: "resolvectl",
			wantErr:        false,
		},
		{
			name:           "flush DNS with systemd-resolve fallback",
			dnsFlushMethod: "systemd-resolve",
			wantErr:        false,
		},
		{
			name:           "flush DNS failure",
			dnsFlushMethod: "fail",
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := NetworkConfig{
				Operation: NetworkFlushDNS,
			}

			r, err := NewNetworkRemediator(config)
			if err != nil {
				t.Fatalf("NewNetworkRemediator() error: %v", err)
			}

			mockExec := &mockNetworkExecutor{
				dnsFlushMethod: tt.dnsFlushMethod,
			}
			r.SetNetworkExecutor(mockExec)

			problem := types.Problem{
				Type:     "dns-failure",
				Resource: "dns",
				Severity: types.ProblemWarning,
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

				// Verify command was executed
				mockExec.mu.Lock()
				commands := mockExec.executedCommands
				mockExec.mu.Unlock()

				if len(commands) == 0 {
					t.Errorf("No commands were executed")
				}
			}
		})
	}
}

// TestNetworkRemediator_RestartInterface tests interface restart.
func TestNetworkRemediator_RestartInterface(t *testing.T) {
	config := NetworkConfig{
		Operation:     NetworkRestartInterface,
		InterfaceName: "eth0",
	}

	r, err := NewNetworkRemediator(config)
	if err != nil {
		t.Fatalf("NewNetworkRemediator() error: %v", err)
	}

	mockExec := &mockNetworkExecutor{
		interfaceExists: true,
		interfaceUp:     false,
	}
	r.SetNetworkExecutor(mockExec)

	problem := types.Problem{
		Type:     "network-interface-down",
		Resource: "eth0",
		Severity: types.ProblemCritical,
	}

	ctx := context.Background()
	err = r.Remediate(ctx, problem)
	if err != nil {
		t.Errorf("Remediate() unexpected error: %v", err)
	}

	// Verify interface was brought down and up
	mockExec.mu.Lock()
	commands := mockExec.executedCommands
	mockExec.mu.Unlock()

	foundDown := false
	foundUp := false
	for _, cmd := range commands {
		if strings.Contains(cmd, "ip link set eth0 down") {
			foundDown = true
		}
		if strings.Contains(cmd, "ip link set eth0 up") {
			foundUp = true
		}
	}

	if !foundDown {
		t.Errorf("Interface was not brought down")
	}
	if !foundUp {
		t.Errorf("Interface was not brought up")
	}

	// Verify interface is now up
	if !mockExec.interfaceUp {
		t.Errorf("Interface should be up after restart")
	}
}

// TestNetworkRemediator_RestartInterface_NotExists tests interface restart when interface doesn't exist.
func TestNetworkRemediator_RestartInterface_NotExists(t *testing.T) {
	config := NetworkConfig{
		Operation:     NetworkRestartInterface,
		InterfaceName: "eth0",
	}

	r, err := NewNetworkRemediator(config)
	if err != nil {
		t.Fatalf("NewNetworkRemediator() error: %v", err)
	}

	mockExec := &mockNetworkExecutor{
		interfaceExists: false, // Interface doesn't exist
	}
	r.SetNetworkExecutor(mockExec)

	problem := types.Problem{
		Type:     "network-interface-down",
		Resource: "eth0",
		Severity: types.ProblemCritical,
	}

	ctx := context.Background()
	err = r.Remediate(ctx, problem)
	if err == nil {
		t.Errorf("Remediate() expected error for non-existent interface, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("Remediate() expected 'does not exist' error, got: %v", err)
	}
}

// TestNetworkRemediator_ResetRouting tests routing table reset.
func TestNetworkRemediator_ResetRouting(t *testing.T) {
	config := NetworkConfig{
		Operation:     NetworkResetRouting,
		BackupRouting: true,
	}

	r, err := NewNetworkRemediator(config)
	if err != nil {
		t.Fatalf("NewNetworkRemediator() error: %v", err)
	}

	mockExec := &mockNetworkExecutor{
		routingTable: "default via 192.168.1.1 dev eth0",
	}
	r.SetNetworkExecutor(mockExec)

	problem := types.Problem{
		Type:     "routing-failure",
		Resource: "routing-table",
		Severity: types.ProblemCritical,
	}

	ctx := context.Background()
	err = r.Remediate(ctx, problem)
	if err != nil {
		t.Errorf("Remediate() unexpected error: %v", err)
	}

	// Verify routing backup was performed
	mockExec.mu.Lock()
	backupCount := mockExec.routingBackups
	commands := mockExec.executedCommands
	mockExec.mu.Unlock()

	if backupCount == 0 {
		t.Errorf("Routing backup was not performed")
	}

	// Verify route flush was executed
	foundFlush := false
	for _, cmd := range commands {
		if strings.Contains(cmd, "ip route flush cache") {
			foundFlush = true
		}
	}

	if !foundFlush {
		t.Errorf("Route flush command was not executed")
	}
}

// TestNetworkRemediator_VerifyInterfaceUp tests interface up verification.
func TestNetworkRemediator_VerifyInterfaceUp(t *testing.T) {
	config := NetworkConfig{
		Operation:     NetworkRestartInterface,
		InterfaceName: "eth0",
		VerifyAfter:   true,
		VerifyTimeout: 2 * time.Second,
	}

	r, err := NewNetworkRemediator(config)
	if err != nil {
		t.Fatalf("NewNetworkRemediator() error: %v", err)
	}

	mockExec := &mockNetworkExecutor{
		interfaceExists:    true,
		interfaceUp:        false,
		preventInterfaceUp: true, // Don't automatically activate during restart
		becomeUpAfter:      2,    // Interface comes up after 2 checks
	}
	r.SetNetworkExecutor(mockExec)

	problem := types.Problem{
		Type:     "network-interface-down",
		Resource: "eth0",
		Severity: types.ProblemCritical,
	}

	ctx := context.Background()
	err = r.Remediate(ctx, problem)
	if err != nil {
		t.Errorf("Remediate() unexpected error: %v", err)
	}

	// Verify interface up was checked multiple times
	mockExec.mu.Lock()
	checkCount := mockExec.upCheckCount
	mockExec.mu.Unlock()

	if checkCount < 2 {
		t.Errorf("Interface up check should have been called at least 2 times, got %d", checkCount)
	}
}

// TestNetworkRemediator_VerifyInterfaceUp_Timeout tests interface up verification timeout.
func TestNetworkRemediator_VerifyInterfaceUp_Timeout(t *testing.T) {
	config := NetworkConfig{
		Operation:     NetworkRestartInterface,
		InterfaceName: "eth0",
		VerifyAfter:   true,
		VerifyTimeout: 1 * time.Second,
	}

	r, err := NewNetworkRemediator(config)
	if err != nil {
		t.Fatalf("NewNetworkRemediator() error: %v", err)
	}

	mockExec := &mockNetworkExecutor{
		interfaceExists:    true,
		interfaceUp:        false,
		preventInterfaceUp: true, // Interface never comes up
	}
	r.SetNetworkExecutor(mockExec)

	problem := types.Problem{
		Type:     "network-interface-down",
		Resource: "eth0",
		Severity: types.ProblemCritical,
	}

	ctx := context.Background()
	err = r.Remediate(ctx, problem)
	if err == nil {
		t.Errorf("Remediate() expected timeout error, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "timeout") {
		t.Errorf("Remediate() expected timeout error, got: %v", err)
	}
}

// TestNetworkRemediator_ExecutionFailure tests command execution failure.
func TestNetworkRemediator_ExecutionFailure(t *testing.T) {
	config := NetworkConfig{
		Operation: NetworkFlushDNS,
	}

	r, err := NewNetworkRemediator(config)
	if err != nil {
		t.Fatalf("NewNetworkRemediator() error: %v", err)
	}

	mockExec := &mockNetworkExecutor{
		shouldFailCommand: true,
		dnsFlushMethod:    "fail",
	}
	r.SetNetworkExecutor(mockExec)

	problem := types.Problem{
		Type:     "dns-failure",
		Resource: "dns",
		Severity: types.ProblemWarning,
	}

	ctx := context.Background()
	err = r.Remediate(ctx, problem)
	if err == nil {
		t.Errorf("Remediate() expected error for command failure, got nil")
	}
}

// TestNetworkRemediator_DryRun tests dry-run mode.
func TestNetworkRemediator_DryRun(t *testing.T) {
	config := NetworkConfig{
		Operation: NetworkFlushDNS,
		DryRun:    true,
	}

	r, err := NewNetworkRemediator(config)
	if err != nil {
		t.Fatalf("NewNetworkRemediator() error: %v", err)
	}

	mockExec := &mockNetworkExecutor{}
	r.SetNetworkExecutor(mockExec)

	problem := types.Problem{
		Type:     "dns-failure",
		Resource: "dns",
		Severity: types.ProblemWarning,
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

// TestNetworkRemediator_ContextCancellation tests context cancellation.
func TestNetworkRemediator_ContextCancellation(t *testing.T) {
	config := NetworkConfig{
		Operation: NetworkFlushDNS,
	}

	r, err := NewNetworkRemediator(config)
	if err != nil {
		t.Fatalf("NewNetworkRemediator() error: %v", err)
	}

	mockExec := &mockNetworkExecutor{
		commandDelay:   2 * time.Second,
		dnsFlushMethod: "resolvectl",
	}
	r.SetNetworkExecutor(mockExec)

	problem := types.Problem{
		Type:     "dns-failure",
		Resource: "dns",
		Severity: types.ProblemWarning,
	}

	// Create a context that will be cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err = r.Remediate(ctx, problem)
	if err == nil {
		t.Errorf("Remediate() expected error for cancelled context, got nil")
	}
}

// TestNetworkRemediator_Cooldown tests cooldown behavior.
func TestNetworkRemediator_Cooldown(t *testing.T) {
	config := NetworkConfig{
		Operation: NetworkFlushDNS,
	}

	r, err := NewNetworkRemediator(config)
	if err != nil {
		t.Fatalf("NewNetworkRemediator() error: %v", err)
	}

	// Set a very short cooldown for testing
	r.cooldown = 100 * time.Millisecond

	mockExec := &mockNetworkExecutor{
		dnsFlushMethod: "resolvectl",
	}
	r.SetNetworkExecutor(mockExec)

	problem := types.Problem{
		Type:     "dns-failure",
		Resource: "dns",
		Severity: types.ProblemWarning,
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

// TestNetworkRemediator_MultipleOperations tests all network operations.
func TestNetworkRemediator_MultipleOperations(t *testing.T) {
	operations := []struct {
		name      string
		operation NetworkOperation
		config    NetworkConfig
	}{
		{
			name:      "flush-dns",
			operation: NetworkFlushDNS,
			config: NetworkConfig{
				Operation: NetworkFlushDNS,
			},
		},
		{
			name:      "restart-interface",
			operation: NetworkRestartInterface,
			config: NetworkConfig{
				Operation:     NetworkRestartInterface,
				InterfaceName: "eth0",
			},
		},
		{
			name:      "reset-routing",
			operation: NetworkResetRouting,
			config: NetworkConfig{
				Operation:     NetworkResetRouting,
				BackupRouting: true,
			},
		},
	}

	for _, op := range operations {
		t.Run(op.name, func(t *testing.T) {
			r, err := NewNetworkRemediator(op.config)
			if err != nil {
				t.Fatalf("NewNetworkRemediator() error: %v", err)
			}

			mockExec := &mockNetworkExecutor{
				interfaceExists: true,
				interfaceUp:     false,
				routingTable:    "default via 192.168.1.1",
				dnsFlushMethod:  "resolvectl",
			}
			r.SetNetworkExecutor(mockExec)

			problem := types.Problem{
				Type:     "network-issue",
				Resource: "network",
				Severity: types.ProblemWarning,
			}

			ctx := context.Background()
			err = r.Remediate(ctx, problem)
			if err != nil {
				t.Errorf("Remediate() for %s failed: %v", op.name, err)
			}

			// Verify operation-specific behavior
			mockExec.mu.Lock()
			commands := mockExec.executedCommands
			mockExec.mu.Unlock()

			if len(commands) == 0 {
				t.Errorf("No commands executed for %s", op.name)
			}
		})
	}
}

// TestNetworkRemediator_InterfaceCheckFailure tests interface existence check failure.
func TestNetworkRemediator_InterfaceCheckFailure(t *testing.T) {
	config := NetworkConfig{
		Operation:     NetworkRestartInterface,
		InterfaceName: "eth0",
	}

	r, err := NewNetworkRemediator(config)
	if err != nil {
		t.Fatalf("NewNetworkRemediator() error: %v", err)
	}

	mockExec := &mockNetworkExecutor{
		shouldFailInterface: true, // Interface check fails
	}
	r.SetNetworkExecutor(mockExec)

	problem := types.Problem{
		Type:     "network-interface-down",
		Resource: "eth0",
		Severity: types.ProblemCritical,
	}

	ctx := context.Background()
	err = r.Remediate(ctx, problem)
	if err == nil {
		t.Errorf("Remediate() expected error for interface check failure, got nil")
	}
}
