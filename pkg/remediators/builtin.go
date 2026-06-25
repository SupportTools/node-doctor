package remediators

import (
	"log"
	"time"

	"k8s.io/client-go/kubernetes"

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

	// StrategyNodeReboot and StrategyPodDelete are DESTRUCTIVE strategies. They are
	// intentionally NOT registered by RegisterBuiltinRemediators; they are
	// registered separately by RegisterClusterRemediators (TaskForge #19263 Phase
	// 2) and ONLY when a real Kubernetes client and node name are available. When
	// the cluster client is unavailable (e.g. out-of-cluster) they remain
	// unregistered and a config naming them fails dispatch with "unknown
	// remediator type" — the desired fail-closed behavior for un-actionable
	// destructive actions.
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

// RegisterClusterRemediators registers the DESTRUCTIVE cluster-scoped remediator
// strategies "node-reboot" and "pod-delete" (TaskForge #19263 Phase 2).
//
// These are registered ONLY when a real Kubernetes client AND a non-empty node
// name are available. If either is missing (e.g. node-doctor is running
// out-of-cluster, or the in-cluster client could not be built), they are NOT
// registered and a clear warning is logged. This is the intended fail-closed
// behavior: a config naming a destructive strategy then fails dispatch with
// "unknown remediator type" rather than silently doing something dangerous (or
// nothing).
//
// Both remediators honor the registry's dry-run state (registry.IsDryRun()): in
// dry-run every destructive step is logged but never executed.
//
//   - "node-reboot" -> NodeRebootRemediator: cordons the node, drains its pods
//     (skipping DaemonSet-owned, mirror/static, and node-doctor's OWN pod), then
//     reboots via an injected command runner. Cordon-before-drain-before-reboot
//     ordering is enforced; cordon failure aborts the reboot.
//
//   - "pod-delete" -> PodDeleteRemediator: deletes the single pod named in
//     Problem.Metadata["namespace"]/["pod"]; refuses mirror/static pods and
//     node-doctor's own pod; missing metadata is a hard error.
//
// selfPodName/selfPodNamespace identify node-doctor's own pod (typically from
// the downward-API POD_NAME/POD_NAMESPACE env vars) so neither remediator can
// act on the pod running this very process. They may be empty (self-skip simply
// disabled), but supplying them is strongly recommended.
//
// Register panics on duplicate types, so RegisterClusterRemediators must be
// called at most once per registry, after RegisterBuiltinRemediators.
func RegisterClusterRemediators(registry *RemediatorRegistry, cfg *types.NodeDoctorConfig, client kubernetes.Interface, nodeName string, selfPodName, selfPodNamespace string) {
	if registry == nil {
		return
	}

	// Fail closed: without a real client and node name we cannot safely cordon,
	// drain, or reboot. Leave the destructive strategies unregistered so any
	// config naming them fails dispatch instead of doing something dangerous.
	if client == nil || nodeName == "" {
		log.Printf("[WARN] Destructive remediators (node-reboot, pod-delete) NOT registered: "+
			"Kubernetes client available=%v, nodeName=%q. A config naming these strategies will "+
			"fail dispatch (fail-closed).", client != nil, nodeName)
		return
	}

	dryRun := registry.IsDryRun()

	// node-reboot: cordon + drain + reboot (destructive, dry-run-aware).
	registry.Register(RemediatorInfo{
		Type: StrategyNodeReboot,
		Factory: func() (types.Remediator, error) {
			return NewNodeRebootRemediator(client, NodeRebootConfig{
				NodeName:         nodeName,
				DryRun:           dryRun,
				SelfPodName:      selfPodName,
				SelfPodNamespace: selfPodNamespace,
			})
		},
		Description: "DESTRUCTIVE (dry-run-aware): cordons, drains (skipping DaemonSet/mirror/self pods), and reboots this node.",
	})

	// pod-delete: deletes the pod named in Problem.Metadata (destructive, dry-run-aware).
	registry.Register(RemediatorInfo{
		Type: StrategyPodDelete,
		Factory: func() (types.Remediator, error) {
			return NewPodDeleteRemediator(client, PodDeleteConfig{
				DryRun:           dryRun,
				SelfPodName:      selfPodName,
				SelfPodNamespace: selfPodNamespace,
			})
		},
		Description: "DESTRUCTIVE (dry-run-aware): deletes the pod named in Problem.Metadata[\"namespace\"]/[\"pod\"]; refuses mirror/self pods.",
	})

	log.Printf("[INFO] Registered destructive cluster remediators: [%s %s] (dry-run=%v, node=%q)",
		StrategyNodeReboot, StrategyPodDelete, dryRun, nodeName)
}
