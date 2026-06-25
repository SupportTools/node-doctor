package remediators

import (
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// Built-in remediator strategy type names. These match the strategy enum in
// pkg/types/config.go (config.validRemediationStrategies) so a strategy named in
// a MonitorRemediationConfig dispatches to the matching registered remediator.
const (
	// StrategySystemdRestart restarts a systemd service. The target service is
	// supplied per-call via Problem.Metadata["service"].
	StrategySystemdRestart = "systemd-restart"

	// StrategyCustomScript runs a user-provided script. The script path/args are
	// supplied per-call via Problem.Metadata["scriptPath"]/["args"].
	StrategyCustomScript = "custom-script"

	// StrategyNodeReboot and StrategyPodDelete are DESTRUCTIVE strategies that are
	// intentionally NOT registered by RegisterBuiltinRemediators (Phase 1). They
	// are deferred to Phase 2 (TaskForge #19263 Phase 2). Until then, a config
	// that names them will fail dispatch with "unknown remediator type", which is
	// the desired fail-safe behavior for un-implemented destructive actions.
	StrategyNodeReboot = "node-reboot"
	StrategyPodDelete  = "pod-delete"
)

// RegisterBuiltinRemediators registers the SAFE built-in remediator strategies
// with the registry so the detector's remediation dispatch (which addresses a
// remediator by its strategy type) can actually find a remediator. Before this
// wiring (TaskForge #19263) nothing registered any remediator type, so every
// dispatch failed with "unknown remediator type".
//
// Phase 1 registers ONLY the two non-destructive strategies:
//
//   - "systemd-restart" -> a SystemdRemediator configured for the restart
//     operation. It is a metadata-driven singleton: the per-call target service
//     comes from Problem.Metadata["service"] (threaded by the detector from each
//     strategy's MonitorRemediationConfig.Service), falling back to any global
//     default service if one is ever configured. This means a single registered
//     remediator handles kubelet, containerd, docker, etc.
//
//   - "custom-script" -> a CustomRemediator with no fixed script path. It is a
//     metadata-driven singleton: the per-call script path/args come from
//     Problem.Metadata["scriptPath"]/["args"]. The metadata path is held to the
//     same absolute-path / no-".." safety validation as a configured path, so the
//     deferred construction does NOT weaken any safety check.
//
// The DESTRUCTIVE strategies "node-reboot" and "pod-delete" are intentionally
// NOT registered here. They are Phase 2 work; leaving them unregistered means a
// config naming them fails dispatch (fail-safe) until they are implemented.
//
// dryRun is taken from the registry's own dry-run state so the built-in
// remediators honour the global dry-run/dry-run-mode flag even on the path
// (config.Remediation.DryRun) that the registry already applies, and so a script
// that exists is not actually executed during a dry run.
//
// Register panics on duplicate or empty types; RegisterBuiltinRemediators must
// therefore be called exactly once per registry (main wiring does this).
func RegisterBuiltinRemediators(registry *RemediatorRegistry, cfg *types.NodeDoctorConfig) {
	if registry == nil {
		return
	}

	dryRun := registry.IsDryRun()

	// systemd-restart: restart operation, service resolved per-call from metadata.
	registry.Register(RemediatorInfo{
		Type: StrategySystemdRestart,
		Factory: func() (types.Remediator, error) {
			return NewSystemdRemediator(SystemdConfig{
				Operation: SystemdRestart,
				// ServiceName intentionally empty: resolved per-call from
				// Problem.Metadata["service"]. Verification is left off by default;
				// per-call params do not (yet) carry a verify flag.
				DryRun: dryRun,
			})
		},
		Description: "Restarts the systemd service named in the triggering monitor's remediation config (Problem.Metadata[\"service\"]).",
	})

	// custom-script: script path/args resolved per-call from metadata.
	registry.Register(RemediatorInfo{
		Type: StrategyCustomScript,
		Factory: func() (types.Remediator, error) {
			return NewCustomRemediator(CustomConfig{
				// ScriptPath intentionally empty: resolved per-call from
				// Problem.Metadata["scriptPath"] (validated absolute / no "..").
				Timeout:       5 * time.Minute,
				CaptureOutput: true,
				DryRun:        dryRun,
			})
		},
		Description: "Runs the remediation script named in the triggering monitor's remediation config (Problem.Metadata[\"scriptPath\"]/[\"args\"]).",
	})
}
