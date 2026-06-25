package remediators

import (
	"context"
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	"github.com/supporttools/node-doctor/pkg/types"
)

const testNodeName = "test-node"

// recordingRunner records reboot invocations and the order in which they
// occurred relative to other actions.
type recordingRunner struct {
	mu     sync.Mutex
	calls  int
	args   [][]string
	onCall func()
}

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	r.args = append(r.args, append([]string{name}, args...))
	if r.onCall != nil {
		r.onCall()
	}
	return "ok", nil
}

func (r *recordingRunner) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// makePod builds a pod scheduled on testNodeName with the given properties.
func makePod(name, namespace string, opts ...func(*corev1.Pod)) *corev1.Pod {
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			NodeName: testNodeName,
		},
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

func asDaemonSet(p *corev1.Pod) {
	p.OwnerReferences = []metav1.OwnerReference{{Kind: "DaemonSet", Name: "ds"}}
}

func asMirror(p *corev1.Pod) {
	if p.Annotations == nil {
		p.Annotations = map[string]string{}
	}
	p.Annotations[mirrorPodAnnotationKey] = "abc123"
}

func newTestNode(unschedulable bool) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: testNodeName},
		Spec:       corev1.NodeSpec{Unschedulable: unschedulable},
	}
}

func TestNodeReboot_DryRun_DoesNotExecute(t *testing.T) {
	node := newTestNode(false)
	pod := makePod("app", "default")
	client := fake.NewSimpleClientset(node, pod)
	runner := &recordingRunner{}

	r, err := NewNodeRebootRemediator(client, NodeRebootConfig{
		NodeName: testNodeName,
		DryRun:   true,
	})
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	r.SetCommandRunner(runner)

	if err := r.Remediate(context.Background(), types.Problem{Type: StrategyNodeReboot}); err != nil {
		t.Fatalf("dry-run remediate: %v", err)
	}

	// Reboot runner must NOT have been called.
	if runner.callCount() != 0 {
		t.Fatalf("dry-run executed reboot runner %d times, want 0", runner.callCount())
	}

	// Node must NOT have been cordoned in dry-run.
	got, err := client.CoreV1().Nodes().Get(context.Background(), testNodeName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get node: %v", err)
	}
	if got.Spec.Unschedulable {
		t.Fatalf("dry-run cordoned the node (Unschedulable=true), want unchanged")
	}

	// Pod must still be present.
	if _, err := client.CoreV1().Pods("default").Get(context.Background(), "app", metav1.GetOptions{}); err != nil {
		t.Fatalf("dry-run deleted/evicted the pod: %v", err)
	}
}

func TestNodeReboot_NonDryRun_CordonsDrainsAndReboots(t *testing.T) {
	node := newTestNode(false)
	appPod := makePod("app", "default")
	dsPod := makePod("ds-pod", "kube-system", asDaemonSet)
	mirrorPod := makePod("mirror-pod", "kube-system", asMirror)
	selfPod := makePod("node-doctor-xyz", "monitoring")

	client := fake.NewSimpleClientset(node, appPod, dsPod, mirrorPod, selfPod)

	// Track ordering: record whether any pod eviction/deletion happened, and
	// whether the reboot occurred before any such drain action.
	var (
		mu                sync.Mutex
		rebooted          bool
		drainObserved     bool
		rebootBeforeDrain bool
	)

	// The fake clientset does not delete the pod on an Eviction create, so wire a
	// reactor that turns an eviction create on the pods/eviction subresource into
	// an actual pod delete (mimicking the real API). Also record drain ordering.
	podsGVR := schema.GroupVersionResource{Version: "v1", Resource: "pods"}
	client.PrependReactor("create", "pods/eviction", func(action ktesting.Action) (bool, runtime.Object, error) {
		mu.Lock()
		drainObserved = true
		if rebooted {
			rebootBeforeDrain = true
		}
		mu.Unlock()

		evictAction := action.(ktesting.CreateActionImpl)
		evicted := evictAction.GetObject().(*policyv1.Eviction)
		_ = client.Tracker().Delete(podsGVR, evictAction.GetNamespace(), evicted.Name)
		return true, nil, nil
	})

	runner := &recordingRunner{onCall: func() {
		mu.Lock()
		rebooted = true
		mu.Unlock()
	}}

	r, err := NewNodeRebootRemediator(client, NodeRebootConfig{
		NodeName:         testNodeName,
		DryRun:           false,
		SelfPodName:      "node-doctor-xyz",
		SelfPodNamespace: "monitoring",
	})
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	r.SetCommandRunner(runner)

	if err := r.Remediate(context.Background(), types.Problem{Type: StrategyNodeReboot}); err != nil {
		t.Fatalf("remediate: %v", err)
	}

	// Node cordoned.
	got, err := client.CoreV1().Nodes().Get(context.Background(), testNodeName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get node: %v", err)
	}
	if !got.Spec.Unschedulable {
		t.Fatalf("node not cordoned (Unschedulable=false), want true")
	}

	// Reboot runner called exactly once.
	if runner.callCount() != 1 {
		t.Fatalf("reboot runner called %d times, want 1", runner.callCount())
	}

	// Ordering: drain (eviction) must have occurred, and the reboot must NOT have
	// happened before the drain.
	mu.Lock()
	gotDrain := drainObserved
	gotRebootBeforeDrain := rebootBeforeDrain
	mu.Unlock()
	if !gotDrain {
		t.Fatalf("expected drain (eviction) to occur before reboot, but no eviction observed")
	}
	if gotRebootBeforeDrain {
		t.Fatalf("reboot occurred BEFORE drain; ordering violated")
	}

	// app pod must be gone (evicted or deleted).
	if _, err := client.CoreV1().Pods("default").Get(context.Background(), "app", metav1.GetOptions{}); err == nil {
		t.Fatalf("app pod still present, expected eviction/deletion")
	}

	// DaemonSet pod must remain.
	if _, err := client.CoreV1().Pods("kube-system").Get(context.Background(), "ds-pod", metav1.GetOptions{}); err != nil {
		t.Fatalf("DaemonSet pod was removed, expected skip: %v", err)
	}
	// Mirror pod must remain.
	if _, err := client.CoreV1().Pods("kube-system").Get(context.Background(), "mirror-pod", metav1.GetOptions{}); err != nil {
		t.Fatalf("mirror pod was removed, expected skip: %v", err)
	}
	// Self pod must remain.
	if _, err := client.CoreV1().Pods("monitoring").Get(context.Background(), "node-doctor-xyz", metav1.GetOptions{}); err != nil {
		t.Fatalf("self pod was removed, expected skip: %v", err)
	}
}

func TestNodeReboot_CordonFailureAbortsReboot(t *testing.T) {
	// No node object -> Get fails -> cordon fails -> reboot must NOT run.
	client := fake.NewSimpleClientset()
	runner := &recordingRunner{}

	r, err := NewNodeRebootRemediator(client, NodeRebootConfig{
		NodeName: testNodeName,
		DryRun:   false,
	})
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	r.SetCommandRunner(runner)

	if err := r.Remediate(context.Background(), types.Problem{Type: StrategyNodeReboot}); err == nil {
		t.Fatalf("expected error when cordon fails, got nil")
	}
	if runner.callCount() != 0 {
		t.Fatalf("reboot ran %d times despite cordon failure, want 0", runner.callCount())
	}
}

func TestNodeReboot_RequiresClientAndNodeName(t *testing.T) {
	if _, err := NewNodeRebootRemediator(nil, NodeRebootConfig{NodeName: testNodeName}); err == nil {
		t.Fatalf("expected error with nil client")
	}
	client := fake.NewSimpleClientset()
	if _, err := NewNodeRebootRemediator(client, NodeRebootConfig{NodeName: ""}); err == nil {
		t.Fatalf("expected error with empty node name")
	}
}
