package network

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func zoneNode(name, zone string) *corev1.Node {
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name:   name,
		Labels: map[string]string{"topology.kubernetes.io/zone": zone},
	}}
}

func runningPodOn(name, node, hostIP string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "node-doctor", Labels: map[string]string{"app": "node-doctor"}},
		Spec:       corev1.PodSpec{NodeName: node},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning, HostIP: hostIP, PodIP: hostIP},
	}
}

// TestDiscoverPeersPopulatesZone verifies peers are tagged with their node zone and
// SameZone is computed relative to the local node, driving topology-aware thresholds.
func TestDiscoverPeersPopulatesZone(t *testing.T) {
	client := fake.NewSimpleClientset(
		zoneNode("self", "site-a"),
		zoneNode("peer-same", "site-a"),
		zoneNode("peer-cross", "site-b"),
		runningPodOn("nd-self", "self", "10.0.0.1"),
		runningPodOn("nd-same", "peer-same", "10.0.0.2"),
		runningPodOn("nd-cross", "peer-cross", "10.1.0.3"),
	)
	pd, err := NewKubernetesPeerDiscoveryWithClient(&PeerDiscoveryConfig{
		Namespace:     "node-doctor",
		LabelSelector: "app=node-doctor",
		SelfNodeName:  "self",
	}, client)
	if err != nil {
		t.Fatalf("NewKubernetesPeerDiscoveryWithClient: %v", err)
	}
	if err := pd.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	peers := pd.GetPeers()
	byNode := map[string]Peer{}
	for _, p := range peers {
		byNode[p.NodeName] = p
	}
	if len(peers) != 2 { // self excluded
		t.Fatalf("got %d peers, want 2: %+v", len(peers), peers)
	}
	if p := byNode["peer-same"]; p.Zone != "site-a" || !p.SameZone {
		t.Errorf("peer-same = zone %q sameZone %v, want site-a/true", p.Zone, p.SameZone)
	}
	if p := byNode["peer-cross"]; p.Zone != "site-b" || p.SameZone {
		t.Errorf("peer-cross = zone %q sameZone %v, want site-b/false", p.Zone, p.SameZone)
	}
}

// TestDiscoverPeersUnlabeledIsSameZone verifies the inert path: with no zone labels,
// every peer is same-zone (unchanged pre-topology behaviour).
func TestDiscoverPeersUnlabeledIsSameZone(t *testing.T) {
	client := fake.NewSimpleClientset(
		runningPodOn("nd-self", "self", "10.0.0.1"),
		runningPodOn("nd-a", "node-a", "10.0.0.2"),
	)
	pd, err := NewKubernetesPeerDiscoveryWithClient(&PeerDiscoveryConfig{
		Namespace:     "node-doctor",
		LabelSelector: "app=node-doctor",
		SelfNodeName:  "self",
	}, client)
	if err != nil {
		t.Fatalf("NewKubernetesPeerDiscoveryWithClient: %v", err)
	}
	if err := pd.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	for _, p := range pd.GetPeers() {
		if p.Zone != "" || !p.SameZone {
			t.Errorf("unlabeled peer %s = zone %q sameZone %v, want ''/true", p.NodeName, p.Zone, p.SameZone)
		}
	}
}
