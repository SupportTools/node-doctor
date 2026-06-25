package remediators

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"github.com/supporttools/node-doctor/pkg/types"
)

// mirrorPodAnnotationKey is the annotation Kubernetes places on mirror pods
// (the API representation of static pods managed directly by the kubelet).
// Such pods must NEVER be evicted/deleted: the kubelet recreates them and the
// API delete is meaningless and potentially disruptive to control-plane
// components running as static pods.
const mirrorPodAnnotationKey = "kubernetes.io/config.mirror"

// daemonSetKind is the OwnerReference kind for DaemonSet-managed pods. Such
// pods tolerate node unschedulability by design and are intentionally skipped
// during drain (matching kubectl drain's default behavior).
const daemonSetKind = "DaemonSet"

// Default tuning for the node-reboot remediator. These are deliberately
// conservative: a node reboot is the single most destructive action this tool
// can take, so every phase is bounded and best-effort.
const (
	// defaultCordonTimeout bounds the node patch (cordon) call.
	defaultCordonTimeout = 30 * time.Second

	// defaultDrainTimeout bounds the entire drain/evict phase. After this the
	// reboot still proceeds (best-effort drain), but only ever AFTER a
	// successful cordon.
	defaultDrainTimeout = 2 * time.Minute

	// defaultPodEvictionGracePeriod is the grace period (seconds) handed to the
	// Eviction API / pod delete during drain.
	defaultPodEvictionGracePeriod int64 = 30

	// defaultRebootTimeout bounds the reboot command itself.
	defaultRebootTimeout = 30 * time.Second
)

// defaultRebootCommand is the command invoked to reboot the node. It is
// overridable via NodeRebootConfig.RebootCommand for environments that reboot
// differently (e.g. nsenter into the host, or a custom helper).
var defaultRebootCommand = []string{"systemctl", "reboot"}

// CommandRunner executes an arbitrary host command. It is injected into the
// NodeRebootRemediator so the actual (destructive) reboot can be mocked in
// tests, mirroring the SystemdExecutor/NetworkExecutor pattern used elsewhere
// in this package.
type CommandRunner interface {
	// Run executes name with args and returns the combined output.
	Run(ctx context.Context, name string, args ...string) (string, error)
}

// defaultCommandRunner is the production CommandRunner that actually shells
// out. It is only ever invoked on the non-dry-run reboot path.
type defaultCommandRunner struct{}

// Run executes the command and returns its combined output.
func (e *defaultCommandRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

// NodeRebootConfig configures the NodeRebootRemediator.
type NodeRebootConfig struct {
	// NodeName is the node this remediator operates on (this node). Required.
	NodeName string

	// DryRun, when true, makes every destructive step (cordon, evict, reboot)
	// log-only. The reboot command is NEVER executed in dry-run.
	DryRun bool

	// RebootCommand overrides the default reboot command. When empty the
	// package default ("systemctl reboot") is used.
	RebootCommand []string

	// SelfPodName / SelfPodNamespace identify node-doctor's OWN pod so the
	// drain never evicts the remediator out from under itself. When known
	// (typically from the downward-API POD_NAME/POD_NAMESPACE env vars) the
	// matching pod is skipped during drain.
	SelfPodName      string
	SelfPodNamespace string

	// CordonTimeout / DrainTimeout / RebootTimeout / PodEvictionGracePeriod
	// bound the respective phases. Zero values fall back to the package
	// defaults.
	CordonTimeout          time.Duration
	DrainTimeout           time.Duration
	RebootTimeout          time.Duration
	PodEvictionGracePeriod int64
}

// NodeRebootRemediator drains and reboots the local node. It is DESTRUCTIVE and
// is only ever registered when a real Kubernetes client and node name are
// available (see RegisterClusterRemediators). It honors dry-run by logging
// every intended action without executing it, cordons before draining, drains
// before rebooting, and skips DaemonSet-owned, mirror/static, and node-doctor's
// own pods during drain.
type NodeRebootRemediator struct {
	*BaseRemediator
	config NodeRebootConfig
	client kubernetes.Interface
	runner CommandRunner
}

// NewNodeRebootRemediator constructs a NodeRebootRemediator. The client and a
// non-empty NodeName are required; without them the remediator could not
// cordon/drain and must not be constructed (RegisterClusterRemediators
// enforces this before calling).
func NewNodeRebootRemediator(client kubernetes.Interface, config NodeRebootConfig) (*NodeRebootRemediator, error) {
	if client == nil {
		return nil, fmt.Errorf("node-reboot remediator requires a non-nil Kubernetes client")
	}
	if config.NodeName == "" {
		return nil, fmt.Errorf("node-reboot remediator requires a non-empty node name")
	}

	if len(config.RebootCommand) == 0 {
		config.RebootCommand = append([]string{}, defaultRebootCommand...)
	}
	if config.CordonTimeout <= 0 {
		config.CordonTimeout = defaultCordonTimeout
	}
	if config.DrainTimeout <= 0 {
		config.DrainTimeout = defaultDrainTimeout
	}
	if config.RebootTimeout <= 0 {
		config.RebootTimeout = defaultRebootTimeout
	}
	if config.PodEvictionGracePeriod <= 0 {
		config.PodEvictionGracePeriod = defaultPodEvictionGracePeriod
	}

	// Node reboot is the highest-impact action: use the destructive cooldown so
	// repeated reboots are heavily rate-limited at the remediator level in
	// addition to the registry-wide limits.
	base, err := NewBaseRemediator(fmt.Sprintf("node-reboot-%s", config.NodeName), CooldownDestructive)
	if err != nil {
		return nil, fmt.Errorf("failed to create base remediator: %w", err)
	}

	r := &NodeRebootRemediator{
		BaseRemediator: base,
		config:         config,
		client:         client,
		runner:         &defaultCommandRunner{},
	}

	if err := base.SetRemediateFunc(r.remediate); err != nil {
		return nil, fmt.Errorf("failed to set remediate function: %w", err)
	}

	return r, nil
}

// SetCommandRunner overrides the reboot command runner (used in tests).
func (r *NodeRebootRemediator) SetCommandRunner(runner CommandRunner) {
	if runner != nil {
		r.runner = runner
	}
}

// remediate cordons the node, drains its pods (best-effort, with exclusions),
// and finally reboots — strictly in that order. In dry-run every step is
// logged but nothing is mutated and the reboot command is NOT invoked.
func (r *NodeRebootRemediator) remediate(ctx context.Context, problem types.Problem) error {
	if r.config.DryRun {
		log.Printf("[WARN] [node-reboot] DRY-RUN: would cordon, drain, and reboot node %q (problem=%s). No action taken.",
			r.config.NodeName, problem.Type)
		// Still walk the (read-only-ish) plan in dry-run so operators see what
		// WOULD happen, but never mutate anything.
		r.logCordonPlan(ctx)
		r.logDrainPlan(ctx)
		log.Printf("[WARN] [node-reboot] DRY-RUN: would execute reboot command %v on node %q. Skipped.",
			r.config.RebootCommand, r.config.NodeName)
		return nil
	}

	log.Printf("[WARN] [node-reboot] Beginning DESTRUCTIVE node reboot sequence for node %q (problem=%s)",
		r.config.NodeName, problem.Type)

	// Phase 1: cordon. Must succeed before we touch pods or reboot.
	if err := r.cordon(ctx); err != nil {
		return fmt.Errorf("cordon failed, aborting reboot (node not rebooted): %w", err)
	}

	// Phase 2: drain (best-effort). Failures are logged and tolerated, but the
	// drain only runs AFTER a successful cordon.
	r.drain(ctx)

	// Phase 3: reboot. Only reached after cordon (and best-effort drain).
	if err := r.reboot(ctx); err != nil {
		return fmt.Errorf("reboot command failed on node %q: %w", r.config.NodeName, err)
	}

	log.Printf("[WARN] [node-reboot] Reboot command issued on node %q", r.config.NodeName)
	return nil
}

// cordon patches the node spec.unschedulable=true. It is a no-op if the node is
// already unschedulable.
func (r *NodeRebootRemediator) cordon(ctx context.Context) error {
	cordonCtx, cancel := context.WithTimeout(ctx, r.config.CordonTimeout)
	defer cancel()

	node, err := r.client.CoreV1().Nodes().Get(cordonCtx, r.config.NodeName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get node %q: %w", r.config.NodeName, err)
	}

	if node.Spec.Unschedulable {
		log.Printf("[INFO] [node-reboot] Node %q already cordoned (unschedulable), skipping cordon", r.config.NodeName)
		return nil
	}

	log.Printf("[WARN] [node-reboot] Cordoning node %q (spec.unschedulable=true)", r.config.NodeName)
	patch := []byte(`{"spec":{"unschedulable":true}}`)
	if _, err := r.client.CoreV1().Nodes().Patch(cordonCtx, r.config.NodeName, k8stypes.StrategicMergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("failed to patch node %q unschedulable: %w", r.config.NodeName, err)
	}
	return nil
}

// logCordonPlan logs (without mutating) whether a cordon would occur. Used in
// dry-run only.
func (r *NodeRebootRemediator) logCordonPlan(ctx context.Context) {
	cordonCtx, cancel := context.WithTimeout(ctx, r.config.CordonTimeout)
	defer cancel()
	node, err := r.client.CoreV1().Nodes().Get(cordonCtx, r.config.NodeName, metav1.GetOptions{})
	if err != nil {
		log.Printf("[WARN] [node-reboot] DRY-RUN: could not read node %q to plan cordon: %v", r.config.NodeName, err)
		return
	}
	if node.Spec.Unschedulable {
		log.Printf("[WARN] [node-reboot] DRY-RUN: node %q already cordoned; cordon would be a no-op", r.config.NodeName)
	} else {
		log.Printf("[WARN] [node-reboot] DRY-RUN: would cordon node %q (spec.unschedulable=true)", r.config.NodeName)
	}
}

// drain evicts eligible pods on the node, skipping DaemonSet-owned, mirror, and
// self pods. It is best-effort: individual failures are logged and the drain
// continues. The whole phase is bounded by DrainTimeout.
func (r *NodeRebootRemediator) drain(ctx context.Context) {
	drainCtx, cancel := context.WithTimeout(ctx, r.config.DrainTimeout)
	defer cancel()

	pods, err := r.listNodePods(drainCtx)
	if err != nil {
		log.Printf("[WARN] [node-reboot] Failed to list pods on node %q for drain (continuing best-effort): %v",
			r.config.NodeName, err)
		return
	}

	log.Printf("[WARN] [node-reboot] Draining node %q: %d pod(s) found", r.config.NodeName, len(pods))
	for i := range pods {
		pod := &pods[i]
		if skip, reason := r.shouldSkipPod(pod); skip {
			log.Printf("[INFO] [node-reboot] Skipping pod %s/%s during drain: %s", pod.Namespace, pod.Name, reason)
			continue
		}
		r.evictPod(drainCtx, pod)
	}
}

// logDrainPlan logs (without evicting) which pods would be evicted/skipped.
// Used in dry-run only.
func (r *NodeRebootRemediator) logDrainPlan(ctx context.Context) {
	drainCtx, cancel := context.WithTimeout(ctx, r.config.DrainTimeout)
	defer cancel()
	pods, err := r.listNodePods(drainCtx)
	if err != nil {
		log.Printf("[WARN] [node-reboot] DRY-RUN: could not list pods on node %q to plan drain: %v",
			r.config.NodeName, err)
		return
	}
	for i := range pods {
		pod := &pods[i]
		if skip, reason := r.shouldSkipPod(pod); skip {
			log.Printf("[WARN] [node-reboot] DRY-RUN: would SKIP pod %s/%s (%s)", pod.Namespace, pod.Name, reason)
			continue
		}
		log.Printf("[WARN] [node-reboot] DRY-RUN: would EVICT pod %s/%s", pod.Namespace, pod.Name)
	}
}

// listNodePods returns the pods scheduled on this node.
func (r *NodeRebootRemediator) listNodePods(ctx context.Context) ([]corev1.Pod, error) {
	fieldSelector := fmt.Sprintf("spec.nodeName=%s", r.config.NodeName)
	list, err := r.client.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{
		FieldSelector: fieldSelector,
	})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

// shouldSkipPod returns true (with a reason) if the pod must NOT be evicted
// during drain: mirror/static pods, DaemonSet-owned pods, and node-doctor's own
// pod are always preserved.
func (r *NodeRebootRemediator) shouldSkipPod(pod *corev1.Pod) (bool, string) {
	if isMirrorPod(pod) {
		return true, "mirror/static pod"
	}
	if isDaemonSetPod(pod) {
		return true, "DaemonSet-owned pod"
	}
	if r.isSelfPod(pod) {
		return true, "node-doctor's own pod"
	}
	return false, ""
}

// isSelfPod reports whether pod is node-doctor's own pod (so drain never evicts
// the remediator itself).
func (r *NodeRebootRemediator) isSelfPod(pod *corev1.Pod) bool {
	if r.config.SelfPodName == "" {
		return false
	}
	return pod.Name == r.config.SelfPodName && pod.Namespace == r.config.SelfPodNamespace
}

// evictPod evicts a single pod via the Eviction API, falling back to a graceful
// delete if eviction is unsupported. Failures are logged and tolerated.
func (r *NodeRebootRemediator) evictPod(ctx context.Context, pod *corev1.Pod) {
	grace := r.config.PodEvictionGracePeriod
	log.Printf("[WARN] [node-reboot] Evicting pod %s/%s (grace=%ds)", pod.Namespace, pod.Name, grace)

	eviction := &policyv1.Eviction{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
		},
		DeleteOptions: &metav1.DeleteOptions{
			GracePeriodSeconds: &grace,
		},
	}

	err := r.client.PolicyV1().Evictions(pod.Namespace).Evict(ctx, eviction)
	if err == nil {
		return
	}

	// Eviction not supported (older clusters / fakes) -> fall back to delete.
	if apierrors.IsNotFound(err) {
		log.Printf("[INFO] [node-reboot] Pod %s/%s already gone during eviction", pod.Namespace, pod.Name)
		return
	}
	log.Printf("[WARN] [node-reboot] Eviction of pod %s/%s failed (%v); falling back to graceful delete",
		pod.Namespace, pod.Name, err)

	if delErr := r.client.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{
		GracePeriodSeconds: &grace,
	}); delErr != nil && !apierrors.IsNotFound(delErr) {
		log.Printf("[WARN] [node-reboot] Graceful delete of pod %s/%s also failed (continuing): %v",
			pod.Namespace, pod.Name, delErr)
	}
}

// reboot invokes the configured reboot command via the injected runner. It is
// only ever called on the non-dry-run path, after cordon+drain.
func (r *NodeRebootRemediator) reboot(ctx context.Context) error {
	rebootCtx, cancel := context.WithTimeout(ctx, r.config.RebootTimeout)
	defer cancel()

	cmd := r.config.RebootCommand
	log.Printf("[WARN] [node-reboot] Executing reboot command %v on node %q", cmd, r.config.NodeName)
	output, err := r.runner.Run(rebootCtx, cmd[0], cmd[1:]...)
	if err != nil {
		return fmt.Errorf("reboot command %v failed: %w (output: %s)", cmd, err, output)
	}
	return nil
}

// CanRemediate returns true for node-reboot problems. It defers to the base
// remediator's cooldown/attempt gating.
func (r *NodeRebootRemediator) CanRemediate(problem types.Problem) bool {
	return r.BaseRemediator.CanRemediate(problem)
}

// isMirrorPod reports whether a pod is a mirror (static) pod.
func isMirrorPod(pod *corev1.Pod) bool {
	if pod.Annotations == nil {
		return false
	}
	_, ok := pod.Annotations[mirrorPodAnnotationKey]
	return ok
}

// isDaemonSetPod reports whether a pod is owned by a DaemonSet.
func isDaemonSetPod(pod *corev1.Pod) bool {
	for _, ref := range pod.OwnerReferences {
		if ref.Kind == daemonSetKind {
			return true
		}
	}
	return false
}
