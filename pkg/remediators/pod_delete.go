package remediators

import (
	"context"
	"fmt"
	"log"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/supporttools/node-doctor/pkg/types"
)

// Problem.Metadata keys used by the pod-delete strategy to identify the target
// pod. The detector populates these from the triggering monitor's remediation
// config; without BOTH the remediator refuses to act (it never guesses or
// deletes broadly).
const (
	// metadataKeyNamespace carries the target pod's namespace.
	metadataKeyNamespace = "namespace"

	// metadataKeyPod carries the target pod's name.
	metadataKeyPod = "pod"
)

const (
	// defaultPodDeleteTimeout bounds the get+delete calls.
	defaultPodDeleteTimeout = 30 * time.Second

	// defaultPodDeleteGracePeriod is the grace period (seconds) for the delete.
	defaultPodDeleteGracePeriod int64 = 30
)

// PodDeleteConfig configures the PodDeleteRemediator.
type PodDeleteConfig struct {
	// DryRun, when true, makes the delete log-only (the pod is NOT deleted).
	DryRun bool

	// SelfPodName / SelfPodNamespace identify node-doctor's OWN pod so it can
	// never delete itself.
	SelfPodName      string
	SelfPodNamespace string

	// GracePeriodSeconds is the delete grace period. Zero falls back to the
	// package default.
	GracePeriodSeconds int64

	// Timeout bounds the get+delete API calls. Zero falls back to the default.
	Timeout time.Duration
}

// PodDeleteRemediator deletes a single target pod identified via Problem
// metadata. It is DESTRUCTIVE and only registered when a real Kubernetes client
// is available (see RegisterClusterRemediators). It honors dry-run, refuses to
// delete mirror/static pods, and refuses to delete node-doctor's own pod.
type PodDeleteRemediator struct {
	*BaseRemediator
	config PodDeleteConfig
	client kubernetes.Interface
}

// NewPodDeleteRemediator constructs a PodDeleteRemediator. A non-nil client is
// required.
func NewPodDeleteRemediator(client kubernetes.Interface, config PodDeleteConfig) (*PodDeleteRemediator, error) {
	if client == nil {
		return nil, fmt.Errorf("pod-delete remediator requires a non-nil Kubernetes client")
	}

	if config.GracePeriodSeconds <= 0 {
		config.GracePeriodSeconds = defaultPodDeleteGracePeriod
	}
	if config.Timeout <= 0 {
		config.Timeout = defaultPodDeleteTimeout
	}

	base, err := NewBaseRemediator("pod-delete", CooldownMedium)
	if err != nil {
		return nil, fmt.Errorf("failed to create base remediator: %w", err)
	}

	r := &PodDeleteRemediator{
		BaseRemediator: base,
		config:         config,
		client:         client,
	}

	if err := base.SetRemediateFunc(r.remediate); err != nil {
		return nil, fmt.Errorf("failed to set remediate function: %w", err)
	}

	return r, nil
}

// remediate deletes the pod named in Problem.Metadata["namespace"]/["pod"].
// Missing metadata is a hard error (no broad/guessed deletion). Mirror/static
// pods and node-doctor's own pod are refused. In dry-run the delete is logged
// only.
func (r *PodDeleteRemediator) remediate(ctx context.Context, problem types.Problem) error {
	namespace, podName, err := r.resolveTarget(problem)
	if err != nil {
		return err
	}

	// Refuse to delete our own pod regardless of dry-run.
	if r.isSelfPod(namespace, podName) {
		return fmt.Errorf("refusing to delete node-doctor's own pod %s/%s", namespace, podName)
	}

	deleteCtx, cancel := context.WithTimeout(ctx, r.config.Timeout)
	defer cancel()

	// Read the pod first so we can refuse mirror/static pods.
	pod, err := r.client.CoreV1().Pods(namespace).Get(deleteCtx, podName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Printf("[INFO] [pod-delete] Target pod %s/%s not found; nothing to delete", namespace, podName)
			return nil
		}
		return fmt.Errorf("failed to get target pod %s/%s: %w", namespace, podName, err)
	}

	if isMirrorPod(pod) {
		return fmt.Errorf("refusing to delete mirror/static pod %s/%s", namespace, podName)
	}

	if r.config.DryRun {
		log.Printf("[WARN] [pod-delete] DRY-RUN: would delete pod %s/%s (grace=%ds). No action taken.",
			namespace, podName, r.config.GracePeriodSeconds)
		return nil
	}

	grace := r.config.GracePeriodSeconds
	log.Printf("[WARN] [pod-delete] Deleting pod %s/%s (grace=%ds)", namespace, podName, grace)
	if err := r.client.CoreV1().Pods(namespace).Delete(deleteCtx, podName, metav1.DeleteOptions{
		GracePeriodSeconds: &grace,
	}); err != nil {
		if apierrors.IsNotFound(err) {
			log.Printf("[INFO] [pod-delete] Pod %s/%s already gone", namespace, podName)
			return nil
		}
		return fmt.Errorf("failed to delete pod %s/%s: %w", namespace, podName, err)
	}

	log.Printf("[WARN] [pod-delete] Deleted pod %s/%s", namespace, podName)
	return nil
}

// resolveTarget extracts the target namespace and pod name from problem
// metadata. Both are required.
func (r *PodDeleteRemediator) resolveTarget(problem types.Problem) (namespace, podName string, err error) {
	if problem.Metadata == nil {
		return "", "", fmt.Errorf("pod-delete requires problem metadata %q and %q but none was provided", metadataKeyNamespace, metadataKeyPod)
	}
	namespace = problem.Metadata[metadataKeyNamespace]
	podName = problem.Metadata[metadataKeyPod]
	if namespace == "" || podName == "" {
		return "", "", fmt.Errorf("pod-delete requires non-empty problem metadata %q and %q (got namespace=%q pod=%q)",
			metadataKeyNamespace, metadataKeyPod, namespace, podName)
	}
	return namespace, podName, nil
}

// isSelfPod reports whether the target is node-doctor's own pod.
func (r *PodDeleteRemediator) isSelfPod(namespace, podName string) bool {
	if r.config.SelfPodName == "" {
		return false
	}
	return podName == r.config.SelfPodName && namespace == r.config.SelfPodNamespace
}

// CanRemediate returns true for pod-delete problems, deferring to the base
// remediator's cooldown/attempt gating.
func (r *PodDeleteRemediator) CanRemediate(problem types.Problem) bool {
	return r.BaseRemediator.CanRemediate(problem)
}
