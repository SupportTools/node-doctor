package network

import (
	"context"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

func findCondition(status *types.Status, condType string) *types.Condition {
	for i := range status.Conditions {
		if status.Conditions[i].Type == condType {
			return &status.Conditions[i]
		}
	}
	return nil
}

func TestDNSLatencyHighCondition(t *testing.T) {
	tests := []struct {
		name       string
		threshold  time.Duration
		latency    time.Duration
		wantStatus types.ConditionStatus
	}{
		{"above threshold", 20 * time.Millisecond, 60 * time.Millisecond, types.ConditionTrue},
		{"below threshold", time.Second, 0, types.ConditionFalse},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newMockResolver()
			mock.setResponse("example.com", []string{"93.184.216.34"})
			if tt.latency > 0 {
				mock.setLatency("example.com", tt.latency)
			}

			monitor := &DNSMonitor{
				name: "test-dns",
				config: &DNSMonitorConfig{
					ExternalDomains:       []string{"example.com"},
					LatencyThreshold:      tt.threshold,
					FailureCountThreshold: 3,
					SuccessRateTracking:   &SuccessRateConfig{WindowSize: 10},
				},
				resolver:               mock,
				clusterSuccessTracker:  NewRingBuffer(10),
				externalSuccessTracker: NewRingBuffer(10),
			}

			status, err := monitor.checkDNS(context.Background())
			if err != nil {
				t.Fatalf("checkDNS: %v", err)
			}
			cond := findCondition(status, "DNSLatencyHigh")
			if cond == nil {
				t.Fatalf("DNSLatencyHigh condition missing; got %+v", status.Conditions)
			}
			if cond.Status != tt.wantStatus {
				t.Errorf("DNSLatencyHigh = %s, want %s (%s)", cond.Status, tt.wantStatus, cond.Message)
			}
		})
	}
}

func TestDNSLatencyHighSkippedWithoutSuccessfulQueries(t *testing.T) {
	mock := newMockResolver()
	monitor := &DNSMonitor{
		name: "test-dns",
		config: &DNSMonitorConfig{
			ExternalDomains:       []string{"missing.example"},
			LatencyThreshold:      time.Second,
			FailureCountThreshold: 3,
			SuccessRateTracking:   &SuccessRateConfig{WindowSize: 10},
		},
		resolver:               mock,
		clusterSuccessTracker:  NewRingBuffer(10),
		externalSuccessTracker: NewRingBuffer(10),
	}

	status, err := monitor.checkDNS(context.Background())
	if err != nil {
		t.Fatalf("checkDNS: %v", err)
	}
	if cond := findCondition(status, "DNSLatencyHigh"); cond != nil {
		t.Errorf("expected no DNSLatencyHigh condition when every query failed, got %+v", cond)
	}
}
