package network

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/supporttools/node-doctor/pkg/types"
)

// recordingPinger records every target it was asked to probe, per cycle, and
// reports every target as reachable unless listed in dead.
type recordingPinger struct {
	mu      sync.Mutex
	targets []string
	dead    map[string]bool
}

func (p *recordingPinger) Ping(_ context.Context, target string, count int, _ time.Duration) ([]PingResult, error) {
	p.mu.Lock()
	p.targets = append(p.targets, target)
	dead := p.dead[target]
	p.mu.Unlock()

	results := make([]PingResult, count)
	for i := range results {
		results[i] = PingResult{Success: !dead, RTT: 2 * time.Millisecond, Family: "ipv4"}
		if dead {
			// A dead target times out at the probe timeout, which is what produced
			// the fake 200-500ms "latency" in the incident.
			results[i].RTT = 500 * time.Millisecond
		}
	}
	return results, nil
}

func (p *recordingPinger) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.targets = nil
}

func (p *recordingPinger) probed() map[string]bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := map[string]bool{}
	for _, t := range p.targets {
		out[t] = true
	}
	return out
}

func testPeer(node, ip string) Peer {
	return Peer{Name: "nd-" + node, NodeName: node, NodeIP: ip, PodIP: ip, SameZone: true, LastSeen: time.Now()}
}

// newPruneTestMonitor builds a CNI monitor over a mutable static discovery so a
// test can simulate peers appearing and disappearing between check cycles.
func newPruneTestMonitor(t *testing.T, pinger Pinger, peers ...Peer) (*CNIMonitor, *staticPeerDiscovery) {
	t.Helper()
	discovery := NewStaticPeerDiscovery(peers)
	static, ok := discovery.(*staticPeerDiscovery)
	if !ok {
		t.Fatalf("NewStaticPeerDiscovery returned %T, want *staticPeerDiscovery", discovery)
	}
	return &CNIMonitor{
		name: "test-cni",
		config: &CNIMonitorConfig{
			Connectivity: ConnectivityConfig{
				PingCount:         3,
				PingTimeout:       5 * time.Second,
				WarningLatency:    50 * time.Millisecond,
				CriticalLatency:   200 * time.Millisecond,
				FailureThreshold:  3,
				MinReachablePeers: 80,
			},
		},
		peerDiscovery: discovery,
		pinger:        pinger,
		peerStatuses:  make(map[string]*PeerStatus),
	}, static
}

func publishedPeers(t *testing.T, status *types.Status) map[[2]string]types.PeerLatency {
	t.Helper()
	out := map[[2]string]types.PeerLatency{}
	lm := status.GetLatencyMetrics()
	if lm == nil {
		return out
	}
	for _, p := range lm.Peers {
		out[[2]string{p.PeerNode, p.PeerIP}] = p
	}
	return out
}

// TestPeerRemovedFromDiscoveryIsDroppedNextCycle is the core regression test for
// node-doctor-251: the CNI monitor's peerStatuses map only ever grew, so a peer
// that disappeared from discovery kept being published as a latency/reachability
// series at its last written value until the agent was restarted.
func TestPeerRemovedFromDiscoveryIsDroppedNextCycle(t *testing.T) {
	pinger := &recordingPinger{dead: map[string]bool{"10.0.0.4": true}}
	m, discovery := newPruneTestMonitor(t, pinger,
		testPeer("node-a", "10.0.0.1"),
		testPeer("node-b", "10.0.0.2"),
		testPeer("node-c", "10.0.0.3"),
		testPeer("node-dead", "10.0.0.4"),
	)

	ctx := context.Background()

	// Cycle 1: the doomed node is still in discovery but already unreachable.
	first, err := m.checkCNI(ctx)
	if err != nil {
		t.Fatalf("checkCNI: %v", err)
	}
	if got := len(publishedPeers(t, first)); got != 4 {
		t.Fatalf("cycle 1 published %d peers, want 4", got)
	}

	// The node is removed/renamed; discovery no longer returns it.
	discovery.SetPeers([]Peer{
		testPeer("node-a", "10.0.0.1"),
		testPeer("node-b", "10.0.0.2"),
		testPeer("node-c", "10.0.0.3"),
	})
	pinger.reset()

	second, err := m.checkCNI(ctx)
	if err != nil {
		t.Fatalf("checkCNI: %v", err)
	}

	// It must not be probed any more...
	if pinger.probed()["10.0.0.4"] {
		t.Error("removed peer 10.0.0.4 was still probed after it left discovery")
	}

	// ...and it must not be tracked or published any more.
	m.mu.Lock()
	_, tracked := m.peerStatuses["node-dead"]
	trackedCount := len(m.peerStatuses)
	m.mu.Unlock()
	if tracked {
		t.Error("peerStatuses still holds node-dead after it left discovery")
	}
	if trackedCount != 3 {
		t.Errorf("peerStatuses has %d entries, want 3", trackedCount)
	}

	published := publishedPeers(t, second)
	if len(published) != 3 {
		t.Errorf("cycle 2 published %d peers, want 3: %v", len(published), published)
	}
	if _, stale := published[[2]string{"node-dead", "10.0.0.4"}]; stale {
		t.Error("cycle 2 still publishes latency metrics for the removed peer")
	}

	// The whole point: reachability must read 100%, not the 3-of-4 = 75% that a
	// retained dead peer produces on every long-lived agent.
	reachable := 0
	for _, p := range published {
		if p.Reachable {
			reachable++
		}
	}
	if reachable != len(published) {
		t.Errorf("reachable %d/%d, want all peers reachable", reachable, len(published))
	}
	for k, p := range published {
		if p.LatencyMs > 100 {
			t.Errorf("peer %v published %.1fms latency; a dead peer's timeout is leaking into max-latency alerting", k, p.LatencyMs)
		}
	}
}

// TestRenamedPeerLeavesNoOldIdentity covers the incident's rename/re-address
// pattern: a1pidnsp02/172.28.1.41 became a1pinode01/172.28.1.14. Neither the old
// name nor the old address may survive the next cycle.
func TestRenamedPeerLeavesNoOldIdentity(t *testing.T) {
	pinger := &recordingPinger{dead: map[string]bool{"172.28.1.41": true}}
	m, discovery := newPruneTestMonitor(t, pinger,
		testPeer("a1pidnsp02", "172.28.1.41"),
		testPeer("a1pinode02", "172.28.1.15"),
	)

	ctx := context.Background()
	if _, err := m.checkCNI(ctx); err != nil {
		t.Fatalf("checkCNI: %v", err)
	}

	discovery.SetPeers([]Peer{
		testPeer("a1pinode01", "172.28.1.14"),
		testPeer("a1pinode02", "172.28.1.15"),
	})
	pinger.reset()

	status, err := m.checkCNI(ctx)
	if err != nil {
		t.Fatalf("checkCNI: %v", err)
	}

	if pinger.probed()["172.28.1.41"] {
		t.Error("the pre-rename address 172.28.1.41 is still being probed")
	}

	m.mu.Lock()
	_, oldName := m.peerStatuses["a1pidnsp02"]
	count := len(m.peerStatuses)
	m.mu.Unlock()
	if oldName {
		t.Error("peerStatuses still holds the pre-rename node name a1pidnsp02")
	}
	if count != 2 {
		t.Errorf("peerStatuses has %d entries, want 2", count)
	}

	published := publishedPeers(t, status)
	if _, stale := published[[2]string{"a1pidnsp02", "172.28.1.41"}]; stale {
		t.Error("the pre-rename identity is still published")
	}
	if _, ok := published[[2]string{"a1pinode01", "172.28.1.14"}]; !ok {
		t.Error("the post-rename identity is missing from published peers")
	}
}

// TestPeerSetConvergesRegardlessOfHistory proves the fix is history-independent:
// a monitor that has churned through many peer identities ends up with exactly the
// same peer set as a freshly constructed one. Agent uptime must not change the
// answer -- that correlation was the incident's tell.
func TestPeerSetConvergesRegardlessOfHistory(t *testing.T) {
	final := []Peer{
		testPeer("node-a", "10.0.0.1"),
		testPeer("node-b", "10.0.0.2"),
	}

	ctx := context.Background()

	fresh, _ := newPruneTestMonitor(t, &recordingPinger{}, final...)
	freshStatus, err := fresh.checkCNI(ctx)
	if err != nil {
		t.Fatalf("checkCNI: %v", err)
	}

	aged, discovery := newPruneTestMonitor(t, &recordingPinger{dead: map[string]bool{
		"172.28.1.41": true, "172.28.1.43": true, "10.9.9.9": true,
	}})
	for _, generation := range [][]Peer{
		{testPeer("a1pinode01", "172.28.1.41"), testPeer("a1pinode03", "172.28.1.43")},
		{testPeer("a1pidnsp02", "172.28.1.41"), testPeer("phantom", "10.9.9.9")},
		{testPeer("node-a", "10.0.0.1")},
		final,
	} {
		discovery.SetPeers(generation)
		if _, err := aged.checkCNI(ctx); err != nil {
			t.Fatalf("checkCNI: %v", err)
		}
	}
	agedStatus, err := aged.checkCNI(ctx)
	if err != nil {
		t.Fatalf("checkCNI: %v", err)
	}

	freshPeers := publishedPeers(t, freshStatus)
	agedPeers := publishedPeers(t, agedStatus)
	if len(agedPeers) != len(freshPeers) {
		t.Fatalf("aged agent publishes %d peers, fresh agent publishes %d; peer set must not depend on uptime: %v", len(agedPeers), len(freshPeers), agedPeers)
	}
	for k := range freshPeers {
		if _, ok := agedPeers[k]; !ok {
			t.Errorf("aged agent is missing peer %v", k)
		}
	}
	for k := range agedPeers {
		if _, ok := freshPeers[k]; !ok {
			t.Errorf("aged agent still carries stale peer %v", k)
		}
	}
}

// TestDiscoveryErrorKeepsPreviousPeerSet is the fail-safe guard for the replace
// approach: a transient API/list failure must NOT wipe the peer set. Replacing it
// with an empty set would make every agent believe the mesh is gone.
func TestDiscoveryErrorKeepsPreviousPeerSet(t *testing.T) {
	client := fake.NewSimpleClientset(
		runningPodOn("nd-self", "self", "10.0.0.1"),
		runningPodOn("nd-a", "node-a", "10.0.0.2"),
		runningPodOn("nd-b", "node-b", "10.0.0.3"),
	)
	pd, err := NewKubernetesPeerDiscoveryWithClient(&PeerDiscoveryConfig{
		Namespace:     "node-doctor",
		LabelSelector: "app=node-doctor",
		SelfNodeName:  "self",
	}, client)
	if err != nil {
		t.Fatalf("NewKubernetesPeerDiscoveryWithClient: %v", err)
	}

	ctx := context.Background()
	if err := pd.Refresh(ctx); err != nil {
		t.Fatalf("initial Refresh: %v", err)
	}
	before := pd.GetPeers()
	if len(before) != 2 {
		t.Fatalf("got %d peers, want 2", len(before))
	}

	// Now make the pod list fail, as a transient apiserver outage would.
	listErr := errors.New("etcdserver: request timed out")
	client.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, listErr
	})

	if err := pd.Refresh(ctx); !errors.Is(err, listErr) {
		t.Fatalf("Refresh error = %v, want it to surface the list failure", err)
	}

	after := pd.GetPeers()
	if len(after) != 2 {
		t.Fatalf("peer set has %d entries after a discovery error, want the previous 2 kept (fail safe)", len(after))
	}
	byNode := map[string]Peer{}
	for _, p := range after {
		byNode[p.NodeName] = p
	}
	for _, want := range []string{"node-a", "node-b"} {
		if _, ok := byNode[want]; !ok {
			t.Errorf("peer %s was lost by a failed discovery pass", want)
		}
	}
}

// TestDiscoveryReplacesRenamedPeer verifies the discovery layer itself is
// authoritative: a node that is renamed and re-addressed in the API produces only
// the new identity, never a merged old+new set.
func TestDiscoveryReplacesRenamedPeer(t *testing.T) {
	oldPod := runningPodOn("nd-old", "a1pidnsp02", "172.28.1.41")
	client := fake.NewSimpleClientset(
		runningPodOn("nd-self", "self", "10.0.0.1"),
		oldPod,
	)
	pd, err := NewKubernetesPeerDiscoveryWithClient(&PeerDiscoveryConfig{
		Namespace:     "node-doctor",
		LabelSelector: "app=node-doctor",
		SelfNodeName:  "self",
	}, client)
	if err != nil {
		t.Fatalf("NewKubernetesPeerDiscoveryWithClient: %v", err)
	}

	ctx := context.Background()
	if err := pd.Refresh(ctx); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if got := len(pd.GetPeers()); got != 1 {
		t.Fatalf("got %d peers, want 1", got)
	}

	// The Pi node is renamed and re-addressed: old pod gone, new pod present.
	if err := client.CoreV1().Pods("node-doctor").Delete(ctx, "nd-old", metav1.DeleteOptions{}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := client.CoreV1().Pods("node-doctor").Create(ctx, runningPodOn("nd-new", "a1pinode01", "172.28.1.14"), metav1.CreateOptions{}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := pd.Refresh(ctx); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	peers := pd.GetPeers()
	if len(peers) != 1 {
		t.Fatalf("got %d peers after rename, want 1: %+v", len(peers), peers)
	}
	if peers[0].NodeName != "a1pinode01" || peers[0].NodeIP != "172.28.1.14" {
		t.Errorf("peer = %s/%s, want a1pinode01/172.28.1.14", peers[0].NodeName, peers[0].NodeIP)
	}
}

// TestDiscoveryStartFailureLeavesEmptySetNotStale documents the startup case: an
// agent whose very first discovery fails has no peers at all (nothing to keep),
// and the CNI monitor reports NoPeersFound rather than probing phantom targets.
func TestDiscoveryStartFailureLeavesEmptySetNotStale(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("connection refused")
	})
	pd, err := NewKubernetesPeerDiscoveryWithClient(&PeerDiscoveryConfig{
		Namespace:     "node-doctor",
		LabelSelector: "app=node-doctor",
		SelfNodeName:  "self",
		// Long interval: the background goroutine must not fire during the test.
		RefreshInterval: time.Hour,
	}, client)
	if err != nil {
		t.Fatalf("NewKubernetesPeerDiscoveryWithClient: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := pd.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer pd.Stop()

	if got := len(pd.GetPeers()); got != 0 {
		t.Errorf("got %d peers after a failed initial discovery, want 0", got)
	}
}
