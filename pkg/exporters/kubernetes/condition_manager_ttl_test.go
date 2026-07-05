package kubernetes

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/supporttools/node-doctor/pkg/types"
)

// newTTLTestConditionManager builds a ConditionManager wired to a fake
// clientset (matching createTestConditionManager's pattern) with a small,
// deterministic staleTTL and a fake clock the test fully controls, so no
// real sleeping is ever required.
func newTTLTestConditionManager(t *testing.T) (*ConditionManager, *fakeClockCtl) {
	t.Helper()

	fakeClientset := fake.NewSimpleClientset()

	testNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
			UID:  "test-uid-12345",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:               corev1.NodeMemoryPressure,
					Status:             corev1.ConditionFalse,
					LastTransitionTime: metav1.NewTime(time.Now()),
					Reason:             "KubeletHasSufficientMemory",
					Message:            "kubelet has sufficient memory available",
				},
			},
		},
	}

	if _, err := fakeClientset.CoreV1().Nodes().Create(context.Background(), testNode, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to seed fake node: %v", err)
	}

	client := &K8sClient{
		clientset: fakeClientset,
		nodeName:  "test-node",
		nodeUID:   "test-uid-12345",
	}

	config := &types.KubernetesExporterConfig{
		UpdateInterval:    time.Second,
		ResyncInterval:    time.Second, // small, so 3*resyncInterval never overrides our requested staleTTL
		HeartbeatInterval: time.Minute,
		ConditionStaleTTL: time.Minute,
	}

	cm := NewConditionManager(client, config)

	clock := &fakeClockCtl{current: time.Now()}
	cm.now = clock.Now

	return cm, clock
}

// fakeClockCtl is a controllable clock for injecting into ConditionManager.now.
type fakeClockCtl struct {
	current time.Time
}

func (f *fakeClockCtl) Now() time.Time {
	return f.current
}

func (f *fakeClockCtl) Advance(d time.Duration) {
	f.current = f.current.Add(d)
}

// TestExpireStaleConditions_TrackedConditionExpiresAfterTTL verifies a
// condition set via UpdateCondition (and therefore tracked in lastRefreshed)
// is removed once it hasn't been refreshed within cm.staleTTL.
func TestExpireStaleConditions_TrackedConditionExpiresAfterTTL(t *testing.T) {
	cm, clock := newTTLTestConditionManager(t)
	ctx := context.Background()

	cm.UpdateCondition(types.NewCondition("CustomStaleCheck", types.ConditionTrue, "Seen", "seen once"))
	if err := cm.ForceFlush(ctx); err != nil {
		t.Fatalf("initial ForceFlush() error = %v", err)
	}

	if _, exists := cm.GetConditions()["NodeDoctorCustomStaleCheck"]; !exists {
		t.Fatalf("expected CustomStaleCheck to be present after initial flush")
	}

	// Advance the fake clock past staleTTL without ever refreshing the condition again.
	clock.Advance(cm.staleTTL + time.Minute)

	if err := cm.ForceFlush(ctx); err != nil {
		t.Fatalf("expiring ForceFlush() error = %v", err)
	}

	conditions := cm.GetConditions()
	if _, exists := conditions["NodeDoctorCustomStaleCheck"]; exists {
		t.Errorf("expected CustomStaleCheck to be expired/removed, but it is still present")
	}
}

// TestExpireStaleConditions_RefreshedConditionIsRetained verifies a condition
// refreshed within the TTL window is NOT expired.
func TestExpireStaleConditions_RefreshedConditionIsRetained(t *testing.T) {
	cm, clock := newTTLTestConditionManager(t)
	ctx := context.Background()

	cm.UpdateCondition(types.NewCondition("CustomFreshCheck", types.ConditionTrue, "Seen", "seen once"))
	if err := cm.ForceFlush(ctx); err != nil {
		t.Fatalf("initial ForceFlush() error = %v", err)
	}

	// Advance less than staleTTL, then refresh it again (still unchanged value).
	clock.Advance(cm.staleTTL / 2)
	cm.UpdateCondition(types.NewCondition("CustomFreshCheck", types.ConditionTrue, "Seen", "seen once"))

	// Advance again such that total time since the LAST refresh is still < staleTTL,
	// even though time since the FIRST write now exceeds staleTTL.
	clock.Advance(cm.staleTTL / 2)

	if err := cm.ForceFlush(ctx); err != nil {
		t.Fatalf("ForceFlush() error = %v", err)
	}

	if _, exists := cm.GetConditions()["NodeDoctorCustomFreshCheck"]; !exists {
		t.Errorf("expected CustomFreshCheck to be retained since it was refreshed within TTL")
	}
}

// TestExpireStaleConditions_UntrackedConditionNeverExpires verifies that a
// condition with no lastRefreshed entry (e.g. a Kubernetes built-in condition
// such as MemoryPressure, set by kubelet and never written by us) is never
// expired, no matter how far the clock is advanced.
func TestExpireStaleConditions_UntrackedConditionNeverExpires(t *testing.T) {
	cm, clock := newTTLTestConditionManager(t)
	ctx := context.Background()

	// Simulate a Kubernetes built-in condition loaded into cm.conditions without
	// ever going through UpdateCondition, so it has NO lastRefreshed entry.
	cm.mu.Lock()
	cm.conditions["MemoryPressure"] = corev1.NodeCondition{
		Type:   "MemoryPressure",
		Status: corev1.ConditionFalse,
		Reason: "KubeletHasSufficientMemory",
	}
	if _, tracked := cm.lastRefreshed["MemoryPressure"]; tracked {
		t.Fatal("test setup invariant violated: MemoryPressure must not have a lastRefreshed entry")
	}
	cm.mu.Unlock()

	clock.Advance(cm.staleTTL * 100)

	if err := cm.ForceFlush(ctx); err != nil {
		t.Fatalf("ForceFlush() error = %v", err)
	}

	if _, exists := cm.GetConditions()["MemoryPressure"]; !exists {
		t.Errorf("expected untracked built-in condition MemoryPressure to survive expiry logic, but it was removed")
	}
}

// TestExpireStaleConditions_HeartbeatNeverExpires verifies NodeDoctorHealthy
// is exempt from expiry even when its lastRefreshed entry is very stale.
func TestExpireStaleConditions_HeartbeatNeverExpires(t *testing.T) {
	cm, clock := newTTLTestConditionManager(t)
	ctx := context.Background()

	cm.sendHeartbeat()
	if err := cm.ForceFlush(ctx); err != nil {
		t.Fatalf("initial ForceFlush() error = %v", err)
	}

	if _, exists := cm.GetConditions()["NodeDoctorHealthy"]; !exists {
		t.Fatalf("expected NodeDoctorHealthy to be present after heartbeat + flush")
	}

	clock.Advance(cm.staleTTL * 100)

	if err := cm.ForceFlush(ctx); err != nil {
		t.Fatalf("ForceFlush() error = %v", err)
	}

	if _, exists := cm.GetConditions()["NodeDoctorHealthy"]; !exists {
		t.Errorf("expected NodeDoctorHealthy to never expire, but it was removed")
	}
}

// TestPerformResync_PreservesLastRefreshed verifies that performResync does
// NOT reset lastRefreshed for condition types that already have an entry, so
// a stale True condition reloaded from the node keeps its OLD timestamp and
// can still expire on the next flush (i.e. resync must not accidentally grant
// a fresh TTL window to an already-stale condition).
func TestPerformResync_PreservesLastRefreshed(t *testing.T) {
	cm, clock := newTTLTestConditionManager(t)
	ctx := context.Background()

	cm.UpdateCondition(types.NewCondition("CustomResyncCheck", types.ConditionTrue, "Seen", "seen once"))
	if err := cm.ForceFlush(ctx); err != nil {
		t.Fatalf("initial ForceFlush() error = %v", err)
	}

	// Confirm the condition made it onto the fake node (PatchNodeConditions
	// applies to the fake clientset's stored Node object), so performResync's
	// GetNode call will reload it.
	node, err := cm.client.GetNode(ctx)
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}
	found := false
	for _, c := range node.Status.Conditions {
		if string(c.Type) == "NodeDoctorCustomResyncCheck" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected CustomResyncCheck to have been patched onto the fake node")
	}

	// Advance the clock so the condition is now old (but don't flush yet).
	clock.Advance(cm.staleTTL + time.Minute)

	if err := cm.ForceResync(ctx); err != nil {
		t.Fatalf("ForceResync() error = %v", err)
	}

	// If resync had reset lastRefreshed to "now", the condition would survive
	// the next flush. We assert it does NOT survive, proving the OLD
	// timestamp was preserved across the resync.
	if err := cm.ForceFlush(ctx); err != nil {
		t.Fatalf("post-resync ForceFlush() error = %v", err)
	}

	if _, exists := cm.GetConditions()["NodeDoctorCustomResyncCheck"]; exists {
		t.Errorf("expected CustomResyncCheck to expire after resync + flush (lastRefreshed should have been preserved, not reset)")
	}
}

// TestExpireStaleConditions_BuiltinNeverExpiresEvenIfTracked is the regression test for
// the v1.8.5 safety bug: a Kubernetes built-in condition (e.g. Ready) that somehow has a
// STALE lastRefreshed entry must STILL never be expired, because eligibility is gated on the
// NodeDoctor name prefix (ownership), not on lastRefreshed. Removing Ready/PIDPressure from a
// live node can trigger evictions.
func TestExpireStaleConditions_BuiltinNeverExpiresEvenIfTracked(t *testing.T) {
	cm, clock := newTTLTestConditionManager(t)
	ctx := context.Background()

	cm.mu.Lock()
	for _, builtin := range []string{"Ready", "PIDPressure", "NetworkUnavailable", "EtcdIsVoter"} {
		cm.conditions[builtin] = corev1.NodeCondition{Type: corev1.NodeConditionType(builtin), Status: corev1.ConditionTrue}
		// Simulate the bug: a stale lastRefreshed entry for a built-in.
		cm.lastRefreshed[builtin] = clock.current.Add(-100 * cm.staleTTL)
	}
	cm.mu.Unlock()

	clock.Advance(cm.staleTTL * 100)
	if err := cm.ForceFlush(ctx); err != nil {
		t.Fatalf("ForceFlush() error = %v", err)
	}

	conds := cm.GetConditions()
	for _, builtin := range []string{"Ready", "PIDPressure", "NetworkUnavailable", "EtcdIsVoter"} {
		if _, exists := conds[builtin]; !exists {
			t.Errorf("built-in condition %s was expired — MUST NEVER happen (ownership guard failed)", builtin)
		}
	}
}

// TestExpireStaleConditions_OwnedUntrackedExpiresAfterStartupGrace verifies a node-doctor
// condition loaded off the node (in cm.conditions, no lastRefreshed entry — e.g. a stale True
// left by an old build) expires once cm.startTime + staleTTL has passed with no monitor re-asserting it.
func TestExpireStaleConditions_OwnedUntrackedExpiresAfterStartupGrace(t *testing.T) {
	cm, clock := newTTLTestConditionManager(t)
	ctx := context.Background()

	cm.mu.Lock()
	cm.startTime = clock.current // anchor grace to the fake clock
	cm.conditions["NodeDoctorKubeletDown"] = corev1.NodeCondition{Type: "NodeDoctorKubeletDown", Status: corev1.ConditionTrue}
	if _, tracked := cm.lastRefreshed["NodeDoctorKubeletDown"]; tracked {
		t.Fatal("setup invariant: NodeDoctorKubeletDown must be untracked")
	}
	cm.mu.Unlock()

	// Within grace → retained.
	clock.Advance(cm.staleTTL / 2)
	if err := cm.ForceFlush(ctx); err != nil {
		t.Fatalf("ForceFlush() error = %v", err)
	}
	if _, exists := cm.GetConditions()["NodeDoctorKubeletDown"]; !exists {
		t.Fatalf("owned-untracked condition expired within startup grace window")
	}

	// Past grace → expired.
	clock.Advance(cm.staleTTL)
	if err := cm.ForceFlush(ctx); err != nil {
		t.Fatalf("ForceFlush() error = %v", err)
	}
	if _, exists := cm.GetConditions()["NodeDoctorKubeletDown"]; exists {
		t.Errorf("expected stale owned-untracked NodeDoctorKubeletDown to expire after startup grace + TTL")
	}
}

// TestRemoveNodeConditions_RefusesNonNodeDoctor verifies the defense-in-depth guard: even if
// asked to remove a Kubernetes built-in, RemoveNodeConditions removes only NodeDoctor* types.
func TestRemoveNodeConditions_RefusesNonNodeDoctor(t *testing.T) {
	fakeClientset := fake.NewSimpleClientset()
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "n1", UID: "u1"},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: "Ready", Status: corev1.ConditionTrue},
			{Type: "PIDPressure", Status: corev1.ConditionFalse},
			{Type: "NodeDoctorKubeletDown", Status: corev1.ConditionTrue},
		}},
	}
	if _, err := fakeClientset.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{}); err != nil {
		t.Fatalf("seed node: %v", err)
	}
	c := &K8sClient{clientset: fakeClientset, nodeName: "n1", nodeUID: "u1"}

	// Ask to remove a built-in AND a node-doctor condition.
	if err := c.RemoveNodeConditions(context.Background(), []string{"Ready", "PIDPressure", "NodeDoctorKubeletDown"}); err != nil {
		t.Fatalf("RemoveNodeConditions error = %v", err)
	}

	got, err := fakeClientset.CoreV1().Nodes().Get(context.Background(), "n1", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get node: %v", err)
	}
	present := map[string]bool{}
	for _, cnd := range got.Status.Conditions {
		present[string(cnd.Type)] = true
	}
	if !present["Ready"] || !present["PIDPressure"] {
		t.Errorf("built-in conditions must NOT be removed; got conditions present=%v", present)
	}
	if present["NodeDoctorKubeletDown"] {
		t.Errorf("NodeDoctorKubeletDown should have been removed")
	}
}
