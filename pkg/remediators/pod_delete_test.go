package remediators

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/supporttools/node-doctor/pkg/types"
)

func podDeleteProblem(namespace, pod string) types.Problem {
	return types.Problem{
		Type: StrategyPodDelete,
		Metadata: map[string]string{
			metadataKeyNamespace: namespace,
			metadataKeyPod:       pod,
		},
	}
}

func TestPodDelete_DryRun_DoesNotDelete(t *testing.T) {
	pod := makePod("victim", "default")
	client := fake.NewSimpleClientset(pod)

	r, err := NewPodDeleteRemediator(client, PodDeleteConfig{DryRun: true})
	if err != nil {
		t.Fatalf("construct: %v", err)
	}

	if err := r.Remediate(context.Background(), podDeleteProblem("default", "victim")); err != nil {
		t.Fatalf("dry-run remediate: %v", err)
	}

	if _, err := client.CoreV1().Pods("default").Get(context.Background(), "victim", metav1.GetOptions{}); err != nil {
		t.Fatalf("dry-run deleted the pod: %v", err)
	}
}

func TestPodDelete_NonDryRun_DeletesTarget(t *testing.T) {
	pod := makePod("victim", "default")
	client := fake.NewSimpleClientset(pod)

	r, err := NewPodDeleteRemediator(client, PodDeleteConfig{DryRun: false})
	if err != nil {
		t.Fatalf("construct: %v", err)
	}

	if err := r.Remediate(context.Background(), podDeleteProblem("default", "victim")); err != nil {
		t.Fatalf("remediate: %v", err)
	}

	if _, err := client.CoreV1().Pods("default").Get(context.Background(), "victim", metav1.GetOptions{}); err == nil {
		t.Fatalf("pod still present, expected deletion")
	}
}

func TestPodDelete_MissingMetadata_Errors(t *testing.T) {
	client := fake.NewSimpleClientset()
	r, err := NewPodDeleteRemediator(client, PodDeleteConfig{})
	if err != nil {
		t.Fatalf("construct: %v", err)
	}

	cases := []types.Problem{
		{Type: StrategyPodDelete}, // nil metadata
		{Type: StrategyPodDelete, Metadata: map[string]string{metadataKeyNamespace: "default"}},              // missing pod
		{Type: StrategyPodDelete, Metadata: map[string]string{metadataKeyPod: "victim"}},                     // missing namespace
		{Type: StrategyPodDelete, Metadata: map[string]string{metadataKeyNamespace: "", metadataKeyPod: ""}}, // empty
	}
	for i, p := range cases {
		if err := r.Remediate(context.Background(), p); err == nil {
			t.Fatalf("case %d: expected error for missing/empty metadata, got nil", i)
		}
	}
}

func TestPodDelete_RefusesMirrorPod(t *testing.T) {
	pod := makePod("mirror", "kube-system", asMirror)
	client := fake.NewSimpleClientset(pod)

	r, err := NewPodDeleteRemediator(client, PodDeleteConfig{})
	if err != nil {
		t.Fatalf("construct: %v", err)
	}

	if err := r.Remediate(context.Background(), podDeleteProblem("kube-system", "mirror")); err == nil {
		t.Fatalf("expected refusal to delete mirror pod, got nil")
	}
	// Pod must remain.
	if _, err := client.CoreV1().Pods("kube-system").Get(context.Background(), "mirror", metav1.GetOptions{}); err != nil {
		t.Fatalf("mirror pod was removed: %v", err)
	}
}

func TestPodDelete_RefusesSelfPod(t *testing.T) {
	pod := makePod("node-doctor-xyz", "monitoring")
	client := fake.NewSimpleClientset(pod)

	r, err := NewPodDeleteRemediator(client, PodDeleteConfig{
		SelfPodName:      "node-doctor-xyz",
		SelfPodNamespace: "monitoring",
	})
	if err != nil {
		t.Fatalf("construct: %v", err)
	}

	if err := r.Remediate(context.Background(), podDeleteProblem("monitoring", "node-doctor-xyz")); err == nil {
		t.Fatalf("expected refusal to delete self pod, got nil")
	}
	if _, err := client.CoreV1().Pods("monitoring").Get(context.Background(), "node-doctor-xyz", metav1.GetOptions{}); err != nil {
		t.Fatalf("self pod was removed: %v", err)
	}
}

func TestPodDelete_RequiresClient(t *testing.T) {
	if _, err := NewPodDeleteRemediator(nil, PodDeleteConfig{}); err == nil {
		t.Fatalf("expected error with nil client")
	}
}
