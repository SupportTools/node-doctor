package network

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/clusterdns"
	"github.com/supporttools/node-doctor/pkg/types"
)

// startFakeOverlayServers starts a single HTTP server that binds on all local
// addresses (":0") so that distinct loopback IPs (127.0.0.2, 127.0.0.3, ...) can each
// act as a distinct simulated overlay-test pod while sharing one ProbePort, mirroring
// how ClusterDNSPodMonitor addresses peers via peer.PodIP + a single configured
// ProbePort. The handler dispatches on the request Host header (which carries the
// dialed peer.PodIP:port) to return a canned clusterdns.ProbeResult per "pod". Hosts
// with no registered result respond 404, simulating an unreachable/errored pod.
func startFakeOverlayServers(t *testing.T, hostToResult map[string]clusterdns.ProbeResult) (port int, cleanup func()) {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/clusterdns", func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(r.Host); err == nil {
			host = h
		}
		result, ok := hostToResult[host]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(result)
	})

	ts := httptest.NewUnstartedServer(mux)
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	_ = ts.Listener.Close()
	ts.Listener = listener
	ts.Start()

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("unexpected listener address type: %T", listener.Addr())
	}

	return tcpAddr.Port, ts.Close
}

func newTestClusterDNSPodMonitor(port int, peers []Peer, opts func(*ClusterDNSPodConfig)) *ClusterDNSPodMonitor {
	cfg := &ClusterDNSPodConfig{
		ProbePort:             port,
		ProbePath:             "/clusterdns",
		Timeout:               2 * time.Second,
		FailureCountThreshold: 3,
		MinSuccessPods:        1,
	}
	if opts != nil {
		opts(cfg)
	}

	return &ClusterDNSPodMonitor{
		name:      "test-cluster-dns-pod",
		config:    cfg,
		discovery: NewStaticPeerDiscovery(peers),
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func hasCondition(status *types.Status, condType string, condStatus types.ConditionStatus, reason string) bool {
	for _, c := range status.Conditions {
		if c.Type == condType && c.Status == condStatus && c.Reason == reason {
			return true
		}
	}
	return false
}

func hasAnyCondition(status *types.Status, condType string) bool {
	for _, c := range status.Conditions {
		if c.Type == condType {
			return true
		}
	}
	return false
}

func hasEvent(status *types.Status, reason string) bool {
	for _, e := range status.Events {
		if e.Reason == reason {
			return true
		}
	}
	return false
}

func TestClusterDNSPodMonitor_AllPodsResolved(t *testing.T) {
	port, cleanup := startFakeOverlayServers(t, map[string]clusterdns.ProbeResult{
		"127.0.0.2": {Target: "kubernetes.default.svc.cluster.local", Resolved: true, Addresses: []string{"10.96.0.1"}},
		"127.0.0.3": {Target: "kubernetes.default.svc.cluster.local", Resolved: true, Addresses: []string{"10.96.0.1"}},
	})
	defer cleanup()

	peers := []Peer{
		{Name: "overlay-a", NodeName: "node-a", PodIP: "127.0.0.2"},
		{Name: "overlay-b", NodeName: "node-b", PodIP: "127.0.0.3"},
	}
	monitor := newTestClusterDNSPodMonitor(port, peers, nil)

	status, err := monitor.checkClusterDNSPod(context.Background())
	if err != nil {
		t.Fatalf("checkClusterDNSPod() unexpected error: %v", err)
	}

	if !hasCondition(status, "ClusterDNSDown", types.ConditionFalse, "ClusterDNSResolvedViaPods") {
		t.Errorf("expected ClusterDNSDown=False/ClusterDNSResolvedViaPods, got conditions: %+v", status.Conditions)
	}
	if !hasEvent(status, "ClusterDNSPodProbeSummary") {
		t.Errorf("expected ClusterDNSPodProbeSummary event, got events: %+v", status.Events)
	}
}

func TestClusterDNSPodMonitor_AllPodsFailRepeatedly(t *testing.T) {
	// Register no hosts, so every probe gets a 404 -> unresolved.
	port, cleanup := startFakeOverlayServers(t, map[string]clusterdns.ProbeResult{})
	defer cleanup()

	peers := []Peer{
		{Name: "overlay-a", NodeName: "node-a", PodIP: "127.0.0.2"},
		{Name: "overlay-b", NodeName: "node-b", PodIP: "127.0.0.3"},
	}
	monitor := newTestClusterDNSPodMonitor(port, peers, func(c *ClusterDNSPodConfig) {
		c.FailureCountThreshold = 3
	})

	var status *types.Status
	var err error
	for i := 0; i < monitor.config.FailureCountThreshold; i++ {
		status, err = monitor.checkClusterDNSPod(context.Background())
		if err != nil {
			t.Fatalf("checkClusterDNSPod() unexpected error on cycle %d: %v", i, err)
		}
	}

	if !hasCondition(status, "ClusterDNSDown", types.ConditionTrue, "RepeatedClusterDNSPodProbeFailures") {
		t.Errorf("expected ClusterDNSDown=True/RepeatedClusterDNSPodProbeFailures after %d cycles, got conditions: %+v",
			monitor.config.FailureCountThreshold, status.Conditions)
	}
}

func TestClusterDNSPodMonitor_PartialSuccessMeetsMinSuccessPods(t *testing.T) {
	port, cleanup := startFakeOverlayServers(t, map[string]clusterdns.ProbeResult{
		"127.0.0.2": {Target: "kubernetes.default.svc.cluster.local", Resolved: true, Addresses: []string{"10.96.0.1"}},
		// 127.0.0.3 intentionally unregistered -> 404 -> unresolved
	})
	defer cleanup()

	peers := []Peer{
		{Name: "overlay-a", NodeName: "node-a", PodIP: "127.0.0.2"},
		{Name: "overlay-b", NodeName: "node-b", PodIP: "127.0.0.3"},
	}
	monitor := newTestClusterDNSPodMonitor(port, peers, func(c *ClusterDNSPodConfig) {
		c.MinSuccessPods = 1
	})

	status, err := monitor.checkClusterDNSPod(context.Background())
	if err != nil {
		t.Fatalf("checkClusterDNSPod() unexpected error: %v", err)
	}

	if !hasCondition(status, "ClusterDNSDown", types.ConditionFalse, "ClusterDNSResolvedViaPods") {
		t.Errorf("expected ClusterDNSDown=False/ClusterDNSResolvedViaPods with partial success, got conditions: %+v", status.Conditions)
	}
	if !hasEvent(status, "ClusterDNSPodProbeSummary") {
		t.Errorf("expected ClusterDNSPodProbeSummary event, got events: %+v", status.Events)
	}
}

func TestClusterDNSPodMonitor_NoPeersFound(t *testing.T) {
	monitor := newTestClusterDNSPodMonitor(8023, nil, nil)
	// Pre-seed a nonzero failure count to prove the no-peers path leaves it untouched.
	monitor.consecutiveFailures = 5

	status, err := monitor.checkClusterDNSPod(context.Background())
	if err != nil {
		t.Fatalf("checkClusterDNSPod() unexpected error: %v", err)
	}

	if !hasEvent(status, "NoOverlayTestPodsFound") {
		t.Errorf("expected NoOverlayTestPodsFound event, got events: %+v", status.Events)
	}
	if hasAnyCondition(status, "ClusterDNSDown") {
		t.Errorf("expected no ClusterDNSDown condition on no-peers cycle, got conditions: %+v", status.Conditions)
	}
	if monitor.consecutiveFailures != 5 {
		t.Errorf("expected consecutiveFailures to remain unchanged at 5 on no-peers cycle, got %d", monitor.consecutiveFailures)
	}
}
