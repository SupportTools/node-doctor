package network

import (
	"context"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// switchablePinger returns whatever result set is currently assigned to a target, so
// a test can change a peer's latency between checks.
type switchablePinger struct {
	results map[string][]PingResult
}

func (p *switchablePinger) Ping(_ context.Context, target string, _ int, _ time.Duration) ([]PingResult, error) {
	return p.results[target], nil
}

func rtts(ms ...int) []PingResult {
	out := make([]PingResult, 0, len(ms))
	for _, m := range ms {
		out = append(out, PingResult{Success: true, RTT: time.Duration(m) * time.Millisecond})
	}
	return out
}

func newDebounceMonitor(t *testing.T, pinger Pinger, threshold int) *CNIMonitor {
	t.Helper()
	return &CNIMonitor{
		name: "test-cni-debounce",
		config: &CNIMonitorConfig{
			Connectivity: ConnectivityConfig{
				PingCount:            3,
				PingTimeout:          5 * time.Second,
				WarningLatency:       200 * time.Millisecond,
				CriticalLatency:      500 * time.Millisecond,
				FailureThreshold:     3,
				HighLatencyThreshold: threshold,
				MinReachablePeers:    80,
			},
		},
		peerDiscovery: NewStaticPeerDiscovery([]Peer{
			{Name: "peer-2", NodeName: "node-2", NodeIP: "10.0.0.2"},
		}),
		pinger:       pinger,
		peerStatuses: make(map[string]*PeerStatus),
	}
}

func runCheck(t *testing.T, m *CNIMonitor) *types.Status {
	t.Helper()
	status, err := m.checkCNI(context.Background())
	if err != nil {
		t.Fatalf("checkCNI() error: %v", err)
	}
	return status
}

func degraded(t *testing.T, s *types.Status) bool {
	t.Helper()
	for _, c := range s.Conditions {
		if c.Type == "NetworkDegraded" {
			return c.Status == types.ConditionTrue
		}
	}
	t.Fatal("no NetworkDegraded condition emitted")
	return false
}

func countEvents(s *types.Status, reason string) int {
	n := 0
	for _, e := range s.Events {
		if e.Reason == reason {
			n++
		}
	}
	return n
}

// One lost SYN costs a 1s TCP retransmit, which on its own pushes a 3-probe
// average past 200ms. That single outlier must not flip NetworkDegraded.
func TestCNIHighLatencySingleOutlierDoesNotDegrade(t *testing.T) {
	p := &switchablePinger{results: map[string][]PingResult{"10.0.0.2": rtts(1, 1, 1)}}
	m := newDebounceMonitor(t, p, 3)

	runCheck(t, m)
	p.results["10.0.0.2"] = rtts(1001, 1, 1) // avg ~334ms: one SYN retransmit
	if degraded(t, runCheck(t, m)) {
		t.Fatal("NetworkDegraded=True after a single slow check; want the debounce to absorb it")
	}
	p.results["10.0.0.2"] = rtts(1, 1, 1)
	if degraded(t, runCheck(t, m)) {
		t.Fatal("NetworkDegraded=True after recovery")
	}
}

// Sustained high latency still degrades, on exactly the threshold-th check.
func TestCNIHighLatencySustainedDegradesAtThreshold(t *testing.T) {
	p := &switchablePinger{results: map[string][]PingResult{"10.0.0.2": rtts(300, 300, 300)}}
	m := newDebounceMonitor(t, p, 3)

	for i := 1; i <= 2; i++ {
		if degraded(t, runCheck(t, m)) {
			t.Fatalf("NetworkDegraded=True on check %d; want it only from check 3", i)
		}
	}
	if !degraded(t, runCheck(t, m)) {
		t.Fatal("NetworkDegraded=False after 3 consecutive slow checks; want True")
	}

	// A fast check resets the streak: it takes another 3 slow checks to degrade again.
	p.results["10.0.0.2"] = rtts(1, 1, 1)
	if degraded(t, runCheck(t, m)) {
		t.Fatal("NetworkDegraded=True after a fast check")
	}
	p.results["10.0.0.2"] = rtts(300, 300, 300)
	if degraded(t, runCheck(t, m)) {
		t.Fatal("streak was not reset by the fast check")
	}
}

// HighLatencyThreshold=1 (and an unset 0) keep the old single-check behaviour.
func TestCNIHighLatencyThresholdOneIsImmediate(t *testing.T) {
	for _, threshold := range []int{0, 1} {
		p := &switchablePinger{results: map[string][]PingResult{"10.0.0.2": rtts(300, 300, 300)}}
		m := newDebounceMonitor(t, p, threshold)
		if !degraded(t, runCheck(t, m)) {
			t.Fatalf("threshold %d: NetworkDegraded=False on first slow check; want True", threshold)
		}
	}
}

// The summary event must fire once when a peer becomes slow, not on every check while
// it stays slow. The old code compared display strings that embed the measured latency
// ("node-2 (301.00ms)"), which differ every check, so it re-emitted every cycle and
// exhausted the event rate limit.
func TestCNIHighLatencySummaryEmittedOnceWhileLatencyVaries(t *testing.T) {
	p := &switchablePinger{results: map[string][]PingResult{}}
	m := newDebounceMonitor(t, p, 1)

	total := 0
	for i := 0; i < 5; i++ {
		v := 300 + i*7 // latency differs every check, as it always does in reality
		p.results["10.0.0.2"] = rtts(v, v, v)
		total += countEvents(runCheck(t, m), "HighPeerLatencySummary")
	}
	if total != 1 {
		t.Fatalf("HighPeerLatencySummary emitted %d times over 5 slow checks with varying latency; want 1", total)
	}

	p.results["10.0.0.2"] = rtts(1, 1, 1)
	if got := countEvents(runCheck(t, m), "HighPeerLatencyRecovered"); got != 1 {
		t.Fatalf("HighPeerLatencyRecovered emitted %d times; want 1", got)
	}
}

// Same defect on the unreachable side: the display string embeds the consecutive
// failure count, which grows every check.
func TestCNIUnreachableSummaryEmittedOnceWhileFailureCountGrows(t *testing.T) {
	failing := []PingResult{{Success: false}, {Success: false}, {Success: false}}
	p := &switchablePinger{results: map[string][]PingResult{"10.0.0.2": failing}}
	m := newDebounceMonitor(t, p, 3)

	total := 0
	for i := 0; i < 6; i++ {
		total += countEvents(runCheck(t, m), "PeersUnreachableSummary")
	}
	if total != 1 {
		t.Fatalf("PeersUnreachableSummary emitted %d times over 6 failing checks; want 1", total)
	}
}

func TestParseCNIConfigHighLatencyThreshold(t *testing.T) {
	cfg, err := parseCNIConfig(nil)
	if err != nil {
		t.Fatalf("parseCNIConfig(nil) error: %v", err)
	}
	if cfg.Connectivity.HighLatencyThreshold != defaultCNIHighLatencyThreshold {
		t.Fatalf("default HighLatencyThreshold = %d, want %d", cfg.Connectivity.HighLatencyThreshold, defaultCNIHighLatencyThreshold)
	}

	cfg, err = parseCNIConfig(map[string]interface{}{
		"connectivity": map[string]interface{}{"highLatencyThreshold": float64(5)},
	})
	if err != nil {
		t.Fatalf("parseCNIConfig error: %v", err)
	}
	if cfg.Connectivity.HighLatencyThreshold != 5 {
		t.Fatalf("HighLatencyThreshold = %d, want 5", cfg.Connectivity.HighLatencyThreshold)
	}

	for _, bad := range []interface{}{0, -1, "3"} {
		if _, err := parseCNIConfig(map[string]interface{}{
			"connectivity": map[string]interface{}{"highLatencyThreshold": bad},
		}); err == nil {
			t.Errorf("highLatencyThreshold=%v: want an error", bad)
		}
	}
}
