package network

import (
	"testing"

	"github.com/supporttools/node-doctor/pkg/types"
)

// newFailureTrackingMonitor builds a minimal DNSMonitor sufficient to exercise
// updateFailureTracking with the given cluster domains.
func newFailureTrackingMonitor(clusterDomains []string) *DNSMonitor {
	return &DNSMonitor{
		config: &DNSMonitorConfig{
			ClusterDomains:        clusterDomains,
			FailureCountThreshold: 3,
			SuccessRateTracking:   &SuccessRateConfig{Enabled: false, WindowSize: 10},
		},
		clusterSuccessTracker:  NewRingBuffer(10),
		externalSuccessTracker: NewRingBuffer(10),
	}
}

func hasConditionType(s *types.Status, condType string) bool {
	for _, c := range s.Conditions {
		if c.Type == condType {
			return true
		}
	}
	return false
}

// TestClusterDNSDownNotEmittedWhenClusterDomainsEmpty verifies the fix for the
// two-monitor conflict: when clusterDomains is empty (in-agent cluster check disabled
// because a hostNetwork agent can't resolve ClusterIP cluster records), the DNS monitor
// must NOT emit ClusterDNSDown — otherwise its unconditional False would mask the
// pod-network cluster-dns-pod monitor's True.
func TestClusterDNSDownNotEmittedWhenClusterDomainsEmpty(t *testing.T) {
	m := newFailureTrackingMonitor(nil) // no cluster domains

	// Even a "healthy" cluster result must not emit ClusterDNSDown.
	s := types.NewStatus("test-dns")
	m.updateFailureTracking(true, true, s)
	if hasConditionType(s, "ClusterDNSDown") {
		t.Errorf("ClusterDNSDown must NOT be emitted when clusterDomains is empty, got conditions: %+v", s.Conditions)
	}

	// Even repeated cluster "failures" must not emit ClusterDNSDown when disabled.
	for i := 0; i < 5; i++ {
		s = types.NewStatus("test-dns")
		m.updateFailureTracking(false, true, s)
	}
	if hasConditionType(s, "ClusterDNSDown") {
		t.Errorf("ClusterDNSDown must NOT be emitted when clusterDomains is empty even after failures, got: %+v", s.Conditions)
	}

	// ExternalDNSDown is unaffected — still emitted (False on healthy external).
	if !hasConditionType(s, "ExternalDNSDown") {
		t.Errorf("ExternalDNSDown should still be emitted regardless of clusterDomains")
	}
}

// TestClusterDNSDownEmittedWhenClusterDomainsSet verifies the normal path still works:
// with cluster domains configured, the DNS monitor owns ClusterDNSDown as before.
func TestClusterDNSDownEmittedWhenClusterDomainsSet(t *testing.T) {
	m := newFailureTrackingMonitor([]string{"kubernetes.default.svc.cluster.local"})

	// Healthy -> ClusterDNSDown=False.
	s := types.NewStatus("test-dns")
	m.updateFailureTracking(true, true, s)
	if !hasCondition(s, "ClusterDNSDown", types.ConditionFalse, "ClusterDNSResolved") {
		t.Errorf("expected ClusterDNSDown=False when clusterDomains set and healthy, got: %+v", s.Conditions)
	}

	// Repeated failures past threshold -> ClusterDNSDown=True.
	for i := 0; i < 3; i++ {
		s = types.NewStatus("test-dns")
		m.updateFailureTracking(false, true, s)
	}
	if !hasCondition(s, "ClusterDNSDown", types.ConditionTrue, "RepeatedClusterDNSFailures") {
		t.Errorf("expected ClusterDNSDown=True after threshold failures, got: %+v", s.Conditions)
	}
}
