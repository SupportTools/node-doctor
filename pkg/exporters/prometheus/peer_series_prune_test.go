package prometheus

import (
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/supporttools/node-doctor/pkg/types"
)

// peerStatus builds a status carrying the given peer set as latency metrics, the
// same shape the CNI monitor publishes each check cycle.
func peerStatus(peers ...types.PeerLatency) *types.Status {
	s := types.NewStatus("network-cni-check")
	s.SetLatencyMetrics(&types.LatencyMetrics{Peers: peers})
	return s
}

func peer(node, ip string, reachable bool, latencyMs float64) types.PeerLatency {
	return types.PeerLatency{
		PeerNode:      node,
		PeerIP:        ip,
		LatencyMs:     latencyMs,
		AvgLatencyMs:  latencyMs,
		Reachable:     reachable,
		AddressFamily: "ipv4",
	}
}

// peerSeriesLabels returns the (peer_node, peer_ip) pairs currently exported for
// the named metric family.
func peerSeriesLabels(t *testing.T, families []*dto.MetricFamily, metricName string) map[[2]string]float64 {
	t.Helper()
	out := map[[2]string]float64{}
	for _, mf := range families {
		if mf.GetName() != metricName {
			continue
		}
		for _, m := range mf.Metric {
			var node, ip string
			for _, l := range m.Label {
				switch l.GetName() {
				case "peer_node":
					node = l.GetValue()
				case "peer_ip":
					ip = l.GetValue()
				}
			}
			var v float64
			switch {
			case m.Gauge != nil:
				v = m.Gauge.GetValue()
			case m.Counter != nil:
				v = m.Counter.GetValue()
			case m.Histogram != nil:
				v = float64(m.Histogram.GetSampleCount())
			}
			out[[2]string{node, ip}] = v
		}
	}
	return out
}

func gaugeValueWithLabels(t *testing.T, families []*dto.MetricFamily, metricName string, want map[string]string) (float64, bool) {
	t.Helper()
	for _, mf := range families {
		if mf.GetName() != metricName {
			continue
		}
	metric:
		for _, m := range mf.Metric {
			have := map[string]string{}
			for _, l := range m.Label {
				have[l.GetName()] = l.GetValue()
			}
			for k, v := range want {
				if have[k] != v {
					continue metric
				}
			}
			switch {
			case m.Gauge != nil:
				return m.Gauge.GetValue(), true
			case m.Counter != nil:
				return m.Counter.GetValue(), true
			}
		}
	}
	return 0, false
}

// TestPeerSeriesPrunedWhenPeerLeavesDiscovery is the regression test for
// node-doctor-251. Prometheus *Vec collectors retain every label combination they
// have ever seen, so a peer that vanished from discovery used to keep exporting
// its last written peer_reachable=0 forever. With three live peers plus one dead
// identity, avg(peer_reachable) reads exactly 75% on an agent that has been up
// long enough to have seen the dead peer -- the uniform-75%-across-many-nodes
// signature from the incident -- while a freshly restarted agent reads 100%.
func TestPeerSeriesPrunedWhenPeerLeavesDiscovery(t *testing.T) {
	e, err := newEphemeralExporter(&types.GlobalSettings{NodeName: "test-node"})
	if err != nil {
		t.Fatalf("newEphemeralExporter: %v", err)
	}

	// Cycle 1: four peers, one of them already failing (the node is being retired).
	e.recordLatencyMetrics(peerStatus(
		peer("node-a", "10.0.0.1", true, 1),
		peer("node-b", "10.0.0.2", true, 1),
		peer("node-c", "10.0.0.3", true, 1),
		peer("node-dead", "10.0.0.4", false, 500),
	))

	// Cycle 2: the retired node is gone from discovery.
	e.recordLatencyMetrics(peerStatus(
		peer("node-a", "10.0.0.1", true, 1),
		peer("node-b", "10.0.0.2", true, 1),
		peer("node-c", "10.0.0.3", true, 1),
	))

	families, err := e.registry.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}

	reachable := peerSeriesLabels(t, families, "test_peer_reachable")
	if len(reachable) != 3 {
		t.Errorf("peer_reachable has %d series, want 3: %v", len(reachable), reachable)
	}
	if _, stale := reachable[[2]string{"node-dead", "10.0.0.4"}]; stale {
		t.Error("peer_reachable still exports the removed peer node-dead/10.0.0.4")
	}

	// avg(peer_reachable) -- what NodeDoctorLowPeerConnectivity evaluates -- must be
	// 100%, not the 75% a stale fourth series produces.
	var sum float64
	for _, v := range reachable {
		sum += v
	}
	if got := sum / float64(len(reachable)); got != 1.0 {
		t.Errorf("avg(peer_reachable) = %v, want 1.0 (a stale dead peer pins this at 0.75)", got)
	}

	// max(peer_latency_seconds) -- what NodeDoctorHighPeerLatency evaluates -- must
	// not be dominated by the dead peer's frozen 500ms timeout reading.
	latency := peerSeriesLabels(t, families, "test_peer_latency_seconds")
	if _, stale := latency[[2]string{"node-dead", "10.0.0.4"}]; stale {
		t.Error("peer_latency_seconds still exports the removed peer's frozen timeout latency")
	}
	for k, v := range latency {
		if v > 0.1 {
			t.Errorf("peer_latency_seconds%v = %v, want no series above 100ms", k, v)
		}
	}

	if _, stale := peerSeriesLabels(t, families, "test_peer_latency_avg_seconds")[[2]string{"node-dead", "10.0.0.4"}]; stale {
		t.Error("peer_latency_avg_seconds still exports the removed peer")
	}
}

// TestPeerSeriesPrunedOnRenameAndReAddress covers the exact identities seen in the
// incident: a node renamed (a1pidnsp02 -> a1pinode01) and re-addressed
// (172.28.1.41 -> 172.28.1.14). Neither the old name nor the old IP may survive.
func TestPeerSeriesPrunedOnRenameAndReAddress(t *testing.T) {
	e, err := newEphemeralExporter(&types.GlobalSettings{NodeName: "test-node"})
	if err != nil {
		t.Fatalf("newEphemeralExporter: %v", err)
	}

	e.recordLatencyMetrics(peerStatus(
		peer("a1pidnsp02", "172.28.1.41", true, 2),
		peer("a1pinode02", "172.28.1.15", true, 2),
	))
	e.recordLatencyMetrics(peerStatus(
		peer("a1pinode01", "172.28.1.14", true, 2),
		peer("a1pinode02", "172.28.1.15", true, 2),
	))

	families, err := e.registry.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}

	for _, metric := range []string{"test_peer_reachable", "test_peer_latency_seconds", "test_peer_latency_avg_seconds"} {
		series := peerSeriesLabels(t, families, metric)
		if len(series) != 2 {
			t.Errorf("%s has %d series, want 2: %v", metric, len(series), series)
		}
		if _, stale := series[[2]string{"a1pidnsp02", "172.28.1.41"}]; stale {
			t.Errorf("%s still exports the pre-rename identity a1pidnsp02/172.28.1.41", metric)
		}
		if _, ok := series[[2]string{"a1pinode01", "172.28.1.14"}]; !ok {
			t.Errorf("%s missing the current identity a1pinode01/172.28.1.14", metric)
		}
	}

	// The latency histogram is keyed by peer_node only; the vanished name must go.
	hist := peerSeriesLabels(t, families, "test_peer_latency_histogram_seconds")
	if _, stale := hist[[2]string{"a1pidnsp02", ""}]; stale {
		t.Error("peer_latency_histogram_seconds still exports the pre-rename peer_node")
	}
}

// TestPeerSetSizeAndChurnMetrics verifies the peer-set size gauge and churn counter
// track reality, so a stale-peer regression is visible instead of silent.
func TestPeerSetSizeAndChurnMetrics(t *testing.T) {
	e, err := newEphemeralExporter(&types.GlobalSettings{NodeName: "test-node"})
	if err != nil {
		t.Fatalf("newEphemeralExporter: %v", err)
	}

	nodeLabels := map[string]string{"node": "test-node"}
	addedLabels := map[string]string{"node": "test-node", "change": "added"}
	removedLabels := map[string]string{"node": "test-node", "change": "removed"}

	gather := func() []*dto.MetricFamily {
		t.Helper()
		families, err := e.registry.Gather()
		if err != nil {
			t.Fatalf("Gather: %v", err)
		}
		return families
	}

	// Cycle 1: three peers discovered for the first time.
	e.recordLatencyMetrics(peerStatus(
		peer("node-a", "10.0.0.1", true, 1),
		peer("node-b", "10.0.0.2", true, 1),
		peer("node-c", "10.0.0.3", true, 1),
	))
	families := gather()
	if got, ok := gaugeValueWithLabels(t, families, "test_peer_set_size", nodeLabels); !ok || got != 3 {
		t.Errorf("peer_set_size = %v (found=%v), want 3", got, ok)
	}
	if got, ok := gaugeValueWithLabels(t, families, "test_peer_set_churn_total", addedLabels); !ok || got != 3 {
		t.Errorf("peer_set_churn_total{change=added} = %v (found=%v), want 3", got, ok)
	}
	if got, ok := gaugeValueWithLabels(t, families, "test_peer_set_churn_total", removedLabels); !ok || got != 0 {
		t.Errorf("peer_set_churn_total{change=removed} = %v (found=%v), want 0", got, ok)
	}

	// Cycle 2: identical set -> no churn.
	e.recordLatencyMetrics(peerStatus(
		peer("node-a", "10.0.0.1", true, 1),
		peer("node-b", "10.0.0.2", true, 1),
		peer("node-c", "10.0.0.3", true, 1),
	))
	families = gather()
	if got, _ := gaugeValueWithLabels(t, families, "test_peer_set_churn_total", addedLabels); got != 3 {
		t.Errorf("added churn after a no-change cycle = %v, want 3", got)
	}
	if got, _ := gaugeValueWithLabels(t, families, "test_peer_set_churn_total", removedLabels); got != 0 {
		t.Errorf("removed churn after a no-change cycle = %v, want 0", got)
	}

	// Cycle 3: node-c is re-addressed -> one identity out, one in; size unchanged.
	e.recordLatencyMetrics(peerStatus(
		peer("node-a", "10.0.0.1", true, 1),
		peer("node-b", "10.0.0.2", true, 1),
		peer("node-c", "10.0.0.99", true, 1),
	))
	families = gather()
	if got, _ := gaugeValueWithLabels(t, families, "test_peer_set_size", nodeLabels); got != 3 {
		t.Errorf("peer_set_size after re-address = %v, want 3", got)
	}
	if got, _ := gaugeValueWithLabels(t, families, "test_peer_set_churn_total", addedLabels); got != 4 {
		t.Errorf("added churn after re-address = %v, want 4", got)
	}
	if got, _ := gaugeValueWithLabels(t, families, "test_peer_set_churn_total", removedLabels); got != 1 {
		t.Errorf("removed churn after re-address = %v, want 1", got)
	}

	// Cycle 4: one peer leaves for good.
	e.recordLatencyMetrics(peerStatus(
		peer("node-a", "10.0.0.1", true, 1),
		peer("node-b", "10.0.0.2", true, 1),
	))
	families = gather()
	if got, _ := gaugeValueWithLabels(t, families, "test_peer_set_size", nodeLabels); got != 2 {
		t.Errorf("peer_set_size after removal = %v, want 2", got)
	}
	if got, _ := gaugeValueWithLabels(t, families, "test_peer_set_churn_total", removedLabels); got != 2 {
		t.Errorf("removed churn after removal = %v, want 2", got)
	}
}

// TestPeerlessStatusDoesNotWipePeerSeries is the fail-safe guard: a status that
// carries no peers (a monitor publishing only gateway/DNS latency, or a CNI cycle
// that found no peers at all because discovery is failing) must leave the last
// known-good peer series in place rather than blanking the mesh.
func TestPeerlessStatusDoesNotWipePeerSeries(t *testing.T) {
	e, err := newEphemeralExporter(&types.GlobalSettings{NodeName: "test-node"})
	if err != nil {
		t.Fatalf("newEphemeralExporter: %v", err)
	}

	e.recordLatencyMetrics(peerStatus(
		peer("node-a", "10.0.0.1", true, 1),
		peer("node-b", "10.0.0.2", true, 1),
	))

	// A gateway-only status from a different monitor.
	gwStatus := types.NewStatus("network-gateway-check")
	gwStatus.SetLatencyMetrics(&types.LatencyMetrics{
		Gateway: &types.GatewayLatency{GatewayIP: "10.0.0.254", LatencyMs: 1, AddressFamily: "ipv4"},
	})
	e.recordLatencyMetrics(gwStatus)

	families, err := e.registry.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	if got := len(peerSeriesLabels(t, families, "test_peer_reachable")); got != 2 {
		t.Errorf("peer_reachable has %d series after a peerless status, want 2 (must not be wiped)", got)
	}
	if got, _ := gaugeValueWithLabels(t, families, "test_peer_set_churn_total", map[string]string{"node": "test-node", "change": "removed"}); got != 0 {
		t.Errorf("removed churn after a peerless status = %v, want 0", got)
	}
}
