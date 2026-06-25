package remediators

import (
	"context"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// TestRegisterBuiltinRemediators_RegistersSafeStrategiesOnly verifies that the
// built-in registration registers the safe strategies (systemd-restart,
// custom-script, and the four network operations) and explicitly does NOT
// register the destructive node-reboot / pod-delete strategies (those are
// Phase 2, registered only by RegisterClusterRemediators).
func TestRegisterBuiltinRemediators_RegistersSafeStrategiesOnly(t *testing.T) {
	registry := NewRegistry(10, 100)
	RegisterBuiltinRemediators(registry, &types.NodeDoctorConfig{})

	// All safe built-in strategies must be registered.
	safe := []string{
		StrategySystemdRestart,
		StrategyCustomScript,
		StrategyFlushDNS,
		StrategyRestartInterface,
		StrategyResetRouting,
		StrategyFlushIPv6Route,
	}
	for _, s := range safe {
		if !registry.IsRegistered(s) {
			t.Errorf("expected %q to be registered", s)
		}
	}

	// flush-ipv6-route reachable closes #17222.
	if !registry.IsRegistered("flush-ipv6-route") {
		t.Errorf("flush-ipv6-route must be registered (closes #17222)")
	}

	// Destructive strategies must NOT be registered by the built-ins.
	if registry.IsRegistered(StrategyNodeReboot) {
		t.Errorf("%q must NOT be registered by built-ins (destructive, cluster-only)", StrategyNodeReboot)
	}
	if registry.IsRegistered(StrategyPodDelete) {
		t.Errorf("%q must NOT be registered by built-ins (destructive, cluster-only)", StrategyPodDelete)
	}

	got := registry.GetRegisteredTypes()
	if len(got) != 6 {
		t.Errorf("expected exactly 6 registered types, got %d: %v", len(got), got)
	}
}

// TestRegisterBuiltinRemediators_NilRegistryNoPanic verifies the nil guard.
func TestRegisterBuiltinRemediators_NilRegistryNoPanic(t *testing.T) {
	RegisterBuiltinRemediators(nil, &types.NodeDoctorConfig{})
}

// TestBuiltinSystemdRestart_DryRunReachesRemediatorWithMetadataService verifies
// that a Problem carrying metadata["service"] dispatched via
// Remediate("systemd-restart", ...) reaches the SystemdRemediator and succeeds
// in dry-run (no real systemctl runs).
func TestBuiltinSystemdRestart_DryRunReachesRemediatorWithMetadataService(t *testing.T) {
	registry := NewRegistry(10, 100)
	registry.SetDryRun(true)
	RegisterBuiltinRemediators(registry, &types.NodeDoctorConfig{})

	problem := types.Problem{
		Type:     StrategySystemdRestart,
		Resource: "KubeletHealthy",
		Severity: types.ProblemWarning,
		Metadata: map[string]string{metadataKeyService: "kubelet"},
	}

	if err := registry.Remediate(context.Background(), StrategySystemdRestart, problem); err != nil {
		t.Fatalf("Remediate(systemd-restart) dry-run failed: %v", err)
	}
}

// TestBuiltinSystemd_MetadataServiceUsedOverConfig verifies that the systemd
// remediator resolves the service from Problem.Metadata at Remediate time and
// actually issues the systemctl command against it (injected executor, no real
// systemctl).
func TestBuiltinSystemd_MetadataServiceUsedOverConfig(t *testing.T) {
	// Build the same remediator the builtin factory builds: empty ServiceName,
	// restart operation — a metadata-driven singleton.
	r, err := NewSystemdRemediator(SystemdConfig{Operation: SystemdRestart})
	if err != nil {
		t.Fatalf("NewSystemdRemediator: %v", err)
	}
	mock := &mockSystemdExecutor{serviceActive: true}
	r.SetSystemdExecutor(mock)

	problem := types.Problem{
		Type:     StrategySystemdRestart,
		Metadata: map[string]string{metadataKeyService: "containerd"},
	}
	if err := r.Remediate(context.Background(), problem); err != nil {
		t.Fatalf("Remediate: %v", err)
	}

	cmds := mock.getExecutedCommands()
	foundRestart := false
	for _, c := range cmds {
		if c == "systemctl [restart containerd]" {
			foundRestart = true
		}
	}
	if !foundRestart {
		t.Errorf("expected systemctl restart containerd, executed: %v", cmds)
	}
}

// TestBuiltinSystemd_NoServiceFails verifies that a metadata-driven singleton
// with neither config nor metadata service fails at Remediate time with a clear
// error (rather than panicking or restarting an empty service).
func TestBuiltinSystemd_NoServiceFails(t *testing.T) {
	r, err := NewSystemdRemediator(SystemdConfig{Operation: SystemdRestart})
	if err != nil {
		t.Fatalf("NewSystemdRemediator: %v", err)
	}
	r.SetSystemdExecutor(&mockSystemdExecutor{})

	problem := types.Problem{Type: StrategySystemdRestart} // no metadata service
	if err := r.Remediate(context.Background(), problem); err == nil {
		t.Fatal("expected error when no service is specified, got nil")
	}
}

// TestBuiltinCustomScript_MetadataScriptPathReachesRemediator verifies that a
// Problem carrying metadata["scriptPath"]/["args"] reaches the CustomRemediator,
// which executes the metadata path with the metadata args (injected executor).
func TestBuiltinCustomScript_MetadataScriptPathReachesRemediator(t *testing.T) {
	// Build the same remediator the builtin factory builds: empty ScriptPath.
	r, err := NewCustomRemediator(CustomConfig{Timeout: time.Minute})
	if err != nil {
		t.Fatalf("NewCustomRemediator: %v", err)
	}
	mock := &mockScriptExecutor{}
	r.SetScriptExecutor(mock)

	problem := types.Problem{
		Type: StrategyCustomScript,
		Metadata: map[string]string{
			metadataKeyScriptPath: "/opt/remediate.sh",
			metadataKeyArgs:       `["--force","a b"]`,
		},
	}
	if err := r.Remediate(context.Background(), problem); err != nil {
		t.Fatalf("Remediate: %v", err)
	}

	scripts := mock.getExecutedScripts()
	if len(scripts) != 1 || scripts[0] != "/opt/remediate.sh" {
		t.Fatalf("expected /opt/remediate.sh executed, got %v", scripts)
	}
	if len(mock.executedArgs) != 1 || len(mock.executedArgs[0]) != 2 ||
		mock.executedArgs[0][0] != "--force" || mock.executedArgs[0][1] != "a b" {
		t.Errorf("expected args [--force, 'a b'], got %v", mock.executedArgs)
	}
}

// TestBuiltinCustomScript_DryRunDispatch verifies the registry dispatch path for
// custom-script in dry-run: the metadata scriptPath reaches the remediator and
// the dry run succeeds WITHOUT executing the script.
func TestBuiltinCustomScript_DryRunDispatch(t *testing.T) {
	registry := NewRegistry(10, 100)
	registry.SetDryRun(true)
	RegisterBuiltinRemediators(registry, &types.NodeDoctorConfig{})

	problem := types.Problem{
		Type:     StrategyCustomScript,
		Metadata: map[string]string{metadataKeyScriptPath: "/opt/fix.sh"},
	}
	if err := registry.Remediate(context.Background(), StrategyCustomScript, problem); err != nil {
		t.Fatalf("Remediate(custom-script) dry-run failed: %v", err)
	}
}

// TestBuiltinCustomScript_InvalidMetadataPathRejected verifies that the metadata
// supplied script path is held to the SAME safety validation (absolute, no "..")
// even on the metadata-driven path: relative and traversal paths are rejected
// and the script is NOT executed.
func TestBuiltinCustomScript_InvalidMetadataPathRejected(t *testing.T) {
	cases := []struct {
		name       string
		scriptPath string
	}{
		{"relative", "relative/fix.sh"},
		{"traversal", "/opt/../../etc/fix.sh"},
		{"empty (no fallback)", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := NewCustomRemediator(CustomConfig{Timeout: time.Minute})
			if err != nil {
				t.Fatalf("NewCustomRemediator: %v", err)
			}
			mock := &mockScriptExecutor{}
			r.SetScriptExecutor(mock)

			problem := types.Problem{Type: StrategyCustomScript}
			if tc.scriptPath != "" {
				problem.Metadata = map[string]string{metadataKeyScriptPath: tc.scriptPath}
			}

			if err := r.Remediate(context.Background(), problem); err == nil {
				t.Fatalf("expected error for invalid script path %q, got nil", tc.scriptPath)
			}
			if got := mock.getExecutedScripts(); len(got) != 0 {
				t.Errorf("script must NOT be executed for invalid path, executed: %v", got)
			}
		})
	}
}

// TestBuiltinCustomScript_InvalidArgsJSONRejected verifies that an unparseable
// args metadata value is rejected rather than silently ignored.
func TestBuiltinCustomScript_InvalidArgsJSONRejected(t *testing.T) {
	r, err := NewCustomRemediator(CustomConfig{Timeout: time.Minute})
	if err != nil {
		t.Fatalf("NewCustomRemediator: %v", err)
	}
	mock := &mockScriptExecutor{}
	r.SetScriptExecutor(mock)

	problem := types.Problem{
		Type: StrategyCustomScript,
		Metadata: map[string]string{
			metadataKeyScriptPath: "/opt/fix.sh",
			metadataKeyArgs:       "not-json",
		},
	}
	if err := r.Remediate(context.Background(), problem); err == nil {
		t.Fatal("expected error for unparseable args JSON, got nil")
	}
	if got := mock.getExecutedScripts(); len(got) != 0 {
		t.Errorf("script must NOT be executed when args are invalid, executed: %v", got)
	}
}

// TestBuiltinFlushIPv6Route_DryRunDispatch verifies that flush-ipv6-route is
// registered and dispatchable via the registry in dry-run, reaching the
// NetworkRemediator path without executing a real command. This is the wiring
// that makes flush-ipv6-route reachable (closes Task #17222).
func TestBuiltinFlushIPv6Route_DryRunDispatch(t *testing.T) {
	registry := NewRegistry(10, 100)
	registry.SetDryRun(true)
	RegisterBuiltinRemediators(registry, &types.NodeDoctorConfig{})

	if !registry.IsRegistered(StrategyFlushIPv6Route) {
		t.Fatalf("flush-ipv6-route must be registered after RegisterBuiltinRemediators")
	}

	problem := types.Problem{
		Type:     StrategyFlushIPv6Route,
		Resource: "ipv6-routing-table",
		Severity: types.ProblemCritical,
	}
	if err := registry.Remediate(context.Background(), StrategyFlushIPv6Route, problem); err != nil {
		t.Fatalf("Remediate(flush-ipv6-route) dry-run failed: %v", err)
	}
}

// TestBuiltinFlushIPv6Route_DryRunNoRealCommand verifies the remediator built by
// the flush-ipv6-route factory honors dry-run: with an injected executor and
// DryRun=true, the ipv6 flush op runs through the remediator but NO real command
// is executed.
func TestBuiltinFlushIPv6Route_DryRunNoRealCommand(t *testing.T) {
	r, err := NewNetworkRemediator(NetworkConfig{
		Operation: NetworkFlushIPv6Route,
		DryRun:    true,
	})
	if err != nil {
		t.Fatalf("NewNetworkRemediator(flush-ipv6-route): %v", err)
	}
	mock := &mockNetworkExecutor{}
	r.SetNetworkExecutor(mock)

	problem := types.Problem{Type: StrategyFlushIPv6Route}
	if err := r.Remediate(context.Background(), problem); err != nil {
		t.Fatalf("Remediate(flush-ipv6-route) dry-run: %v", err)
	}
	if len(mock.executedCommands) != 0 {
		t.Errorf("dry-run must execute NO real command, executed: %v", mock.executedCommands)
	}
}

// TestBuiltinFlushIPv6Route_ReachesExecutor verifies a non-dry-run flush-ipv6-route
// remediation reaches the injected executor and issues the ipv6 flush command.
func TestBuiltinFlushIPv6Route_ReachesExecutor(t *testing.T) {
	r, err := NewNetworkRemediator(NetworkConfig{Operation: NetworkFlushIPv6Route})
	if err != nil {
		t.Fatalf("NewNetworkRemediator(flush-ipv6-route): %v", err)
	}
	mock := &mockNetworkExecutor{}
	r.SetNetworkExecutor(mock)

	problem := types.Problem{Type: StrategyFlushIPv6Route}
	if err := r.Remediate(context.Background(), problem); err != nil {
		t.Fatalf("Remediate(flush-ipv6-route): %v", err)
	}

	found := false
	for _, c := range mock.executedCommands {
		if c == "ip -6 route flush cache" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'ip -6 route flush cache', executed: %v", mock.executedCommands)
	}
}

// TestBuiltinRestartInterface_MetadataInterface verifies the restart-interface
// remediator built by the builtin factory (AllowMetadataInterface, empty fixed
// interface) resolves its target interface from Problem.Metadata["interface"]
// and bounces it via the injected executor.
func TestBuiltinRestartInterface_MetadataInterface(t *testing.T) {
	// Same construction the builtin factory uses for restart-interface.
	r, err := NewNetworkRemediator(NetworkConfig{
		Operation:              NetworkRestartInterface,
		AllowMetadataInterface: true,
	})
	if err != nil {
		t.Fatalf("NewNetworkRemediator(restart-interface): %v", err)
	}
	mock := &mockNetworkExecutor{interfaceExists: true}
	r.SetNetworkExecutor(mock)

	problem := types.Problem{
		Type:     StrategyRestartInterface,
		Metadata: map[string]string{metadataKeyInterface: "eth0"},
	}
	if err := r.Remediate(context.Background(), problem); err != nil {
		t.Fatalf("Remediate(restart-interface): %v", err)
	}

	wantDown, wantUp := false, false
	for _, c := range mock.executedCommands {
		if c == "ip link set eth0 down" {
			wantDown = true
		}
		if c == "ip link set eth0 up" {
			wantUp = true
		}
	}
	if !wantDown || !wantUp {
		t.Errorf("expected eth0 to be bounced (down+up), executed: %v", mock.executedCommands)
	}
}

// TestBuiltinRestartInterface_MissingInterfaceFails verifies a restart-interface
// remediation with neither config nor metadata interface fails at Remediate time
// (rather than acting on an empty interface), and executes no command.
func TestBuiltinRestartInterface_MissingInterfaceFails(t *testing.T) {
	r, err := NewNetworkRemediator(NetworkConfig{
		Operation:              NetworkRestartInterface,
		AllowMetadataInterface: true,
	})
	if err != nil {
		t.Fatalf("NewNetworkRemediator(restart-interface): %v", err)
	}
	mock := &mockNetworkExecutor{interfaceExists: true}
	r.SetNetworkExecutor(mock)

	problem := types.Problem{Type: StrategyRestartInterface} // no interface metadata
	if err := r.Remediate(context.Background(), problem); err == nil {
		t.Fatal("expected error when no interface is specified, got nil")
	}
	if len(mock.executedCommands) != 0 {
		t.Errorf("no command must run when interface is missing, executed: %v", mock.executedCommands)
	}
}
