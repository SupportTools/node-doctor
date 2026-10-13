package kubernetes

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/supporttools/node-doctor/pkg/types"
)

// TestFlushNeverWritesKubeletOwnedConditions: the system-disk / system-memory monitors emit
// "DiskPressure" / "MemoryPressure". node-doctor must write them as NodeDoctorDiskPressure /
// NodeDoctorMemoryPressure and leave kubelet's own DiskPressure/MemoryPressure/Ready exactly
// as kubelet wrote them (cluster-services-24760).
func TestFlushNeverWritesKubeletOwnedConditions(t *testing.T) {
	cm := createTestConditionManager(time.Hour, time.Hour, time.Hour)
	ctx := context.Background()
	if err := cm.initialSync(ctx); err != nil {
		t.Fatalf("initialSync: %v", err)
	}

	// Simulate the node-lifecycle controller marking the node Ready=Unknown after the
	// manager's sync (kubelet died). A stale cached Ready=True must not be written back.
	node, err := cm.client.clientset.CoreV1().Nodes().Get(ctx, "test-node", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for i := range node.Status.Conditions {
		if node.Status.Conditions[i].Type == corev1.NodeReady {
			node.Status.Conditions[i].Status = corev1.ConditionUnknown
			node.Status.Conditions[i].Reason = "NodeStatusUnknown"
		}
	}
	if _, err := cm.client.clientset.CoreV1().Nodes().UpdateStatus(ctx, node, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	cm.UpdateCondition(types.NewCondition("DiskPressure", types.ConditionFalse, "NormalDisk", "disk ok"))
	cm.UpdateCondition(types.NewCondition("MemoryPressure", types.ConditionFalse, "NormalMemory", "mem ok"))
	if err := cm.flushPendingUpdates(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}

	node, err = cm.client.clientset.CoreV1().Nodes().Get(ctx, "test-node", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[corev1.NodeConditionType]corev1.NodeCondition{}
	for _, c := range node.Status.Conditions {
		got[c.Type] = c
	}
	for _, want := range []corev1.NodeConditionType{"NodeDoctorDiskPressure", "NodeDoctorMemoryPressure"} {
		if _, ok := got[want]; !ok {
			t.Errorf("expected node-doctor condition %s on node, got %v", want, keys(got))
		}
	}
	if c, ok := got[corev1.NodeMemoryPressure]; !ok || c.Reason != "KubeletHasSufficientMemory" {
		t.Errorf("kubelet-owned MemoryPressure was overwritten: %+v", c)
	}
	if _, ok := got[corev1.NodeDiskPressure]; ok {
		t.Errorf("node-doctor created a bare DiskPressure condition: %+v", got[corev1.NodeDiskPressure])
	}
	if c := got[corev1.NodeReady]; c.Status != corev1.ConditionUnknown {
		t.Errorf("stale cached Ready was written back over the live value: Ready=%s (%s)", c.Status, c.Reason)
	}
}

// TestUpdateConditionKeepsTransitionTimeWhileStatusUnchanged: monitors stamp Transition=now
// on every check; lastTransitionTime must only move when the status actually flips.
func TestUpdateConditionKeepsTransitionTimeWhileStatusUnchanged(t *testing.T) {
	cm := createTestConditionManager(time.Hour, time.Hour, time.Hour)
	ctx := context.Background()
	if err := cm.initialSync(ctx); err != nil {
		t.Fatalf("initialSync: %v", err)
	}

	first := types.NewCondition("DiskPressure", types.ConditionFalse, "NormalDisk", "disk / at 40%")
	first.Transition = time.Now().Add(-time.Hour).Truncate(time.Second)
	cm.UpdateCondition(first)
	if err := cm.flushPendingUpdates(ctx); err != nil {
		t.Fatal(err)
	}

	second := types.NewCondition("DiskPressure", types.ConditionFalse, "ElevatedDisk", "disk / at 82%")
	cm.UpdateCondition(second)
	if err := cm.flushPendingUpdates(ctx); err != nil {
		t.Fatal(err)
	}
	c := cm.GetConditions()["NodeDoctorDiskPressure"]
	if !c.LastTransitionTime.Time.Equal(first.Transition) {
		t.Errorf("lastTransitionTime moved without a status change: got %v, want %v", c.LastTransitionTime.Time, first.Transition)
	}
	if c.Reason != "ElevatedDisk" {
		t.Errorf("reason not updated: %s", c.Reason)
	}

	flipped := types.NewCondition("DiskPressure", types.ConditionTrue, "SustainedHighDisk", "disk / at 96%")
	cm.UpdateCondition(flipped)
	if err := cm.flushPendingUpdates(ctx); err != nil {
		t.Fatal(err)
	}
	c = cm.GetConditions()["NodeDoctorDiskPressure"]
	if c.LastTransitionTime.Time.Equal(first.Transition) {
		t.Errorf("lastTransitionTime did not move on a status flip")
	}
}

func keys(m map[corev1.NodeConditionType]corev1.NodeCondition) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, string(k))
	}
	return out
}
