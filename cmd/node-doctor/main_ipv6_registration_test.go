package main

import (
	"testing"

	"github.com/supporttools/node-doctor/pkg/monitors"
	"github.com/supporttools/node-doctor/pkg/types"
)

// newIPv6MonitorTypes are the IPv6/dual-stack monitors added under feature 1125.
// They self-register via their package init() (reached through the blank import
// of pkg/monitors/network in main.go) and are auto-enabled at startup by
// monitors.ApplyDefaultMonitors because each provides a DefaultConfig.
//
// This test is the deliverable for task #17209 "Register new IPv6 monitors in
// cmd/node-doctor": it pins the contract that these types are reachable from the
// command binary and applied by default, so a future change to the blank import
// or a monitor's init()/DefaultConfig fails loudly here.
var newIPv6MonitorTypes = []string{
	"network-ipv6-sysctl",
	"network-ipv6-route",
	"network-ipv6-neighbor",
	"network-ipv6-firewall",
}

func TestIPv6Monitors_RegisteredInCommand(t *testing.T) {
	for _, monitorType := range newIPv6MonitorTypes {
		t.Run(monitorType, func(t *testing.T) {
			if !monitors.IsRegistered(monitorType) {
				t.Fatalf("monitor type %q is not registered; check the blank import of pkg/monitors/network in main.go and the monitor's init()", monitorType)
			}

			info := monitors.GetMonitorInfo(monitorType)
			if info == nil {
				t.Fatalf("GetMonitorInfo(%q) returned nil despite IsRegistered=true", monitorType)
			}
			if info.Factory == nil {
				t.Errorf("monitor %q has a nil Factory", monitorType)
			}
			if info.DefaultConfig == nil {
				t.Errorf("monitor %q has a nil DefaultConfig and so will NOT be auto-enabled by ApplyDefaultMonitors", monitorType)
			}
		})
	}
}

func TestIPv6Monitors_AutoAppliedAsDefaults(t *testing.T) {
	// Start from an empty configuration: ApplyDefaultMonitors should inject a
	// default config for every registered monitor type that has a DefaultConfig,
	// including the four new IPv6 monitors.
	cfg := &types.NodeDoctorConfig{}

	added := monitors.ApplyDefaultMonitors(cfg)

	addedSet := make(map[string]bool, len(added))
	for _, monitorType := range added {
		addedSet[monitorType] = true
	}

	configuredSet := make(map[string]bool, len(cfg.Monitors))
	for _, m := range cfg.Monitors {
		configuredSet[m.Type] = true
	}

	for _, monitorType := range newIPv6MonitorTypes {
		if !addedSet[monitorType] {
			t.Errorf("ApplyDefaultMonitors did not add %q to a fresh config", monitorType)
		}
		if !configuredSet[monitorType] {
			t.Errorf("after ApplyDefaultMonitors, config.Monitors is missing %q", monitorType)
		}
	}
}
