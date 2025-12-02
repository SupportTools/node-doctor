package custom

import (
	"regexp"
	"testing"
)

// TestGetDefaultPatterns tests the GetDefaultPatterns function.
func TestGetDefaultPatterns(t *testing.T) {
	patterns := GetDefaultPatterns()

	// Verify we get patterns back
	if len(patterns) == 0 {
		t.Error("GetDefaultPatterns() returned empty slice, expected default patterns")
	}

	// Verify it's a copy (not the same slice)
	if &patterns[0] == &DefaultLogPatterns[0] {
		t.Error("GetDefaultPatterns() returned same slice, expected a copy")
	}

	// Verify all expected patterns are present (58 patterns total: 7 original + 12 kubelet + 9 others + 3 vmxnet3 + 27 networking)
	expectedPatterns := map[string]bool{
		"oom-killer":          true,
		"disk-io-error":       true,
		"network-timeout":     true,
		"kernel-panic":        true,
		"memory-corruption":   true,
		"device-failure":      true,
		"filesystem-readonly": true,
		// 12 kubelet patterns
		"kubelet-oom":                  true,
		"kubelet-pleg-unhealthy":       true,
		"kubelet-eviction":             true,
		"kubelet-disk-pressure":        true,
		"kubelet-memory-pressure":      true,
		"kubelet-image-pull-failed":    true,
		"kubelet-cni-error":            true,
		"kubelet-runtime-error":        true,
		"kubelet-certificate-rotation": true,
		"kubelet-node-not-ready":       true,
		"kubelet-api-connection-error": true,
		"kubelet-volume-mount-error":   true,
		// Other patterns
		"containerd-error":         true,
		"docker-error":             true,
		"cpu-thermal-throttle":     true,
		"nfs-server-timeout":       true,
		"nfs-stale-filehandle":     true,
		"numa-balancing-failure":   true,
		"hardware-mce-error":       true,
		"edac-uncorrectable-error": true,
		"edac-correctable-error":   true,
		// VMware vmxnet3 patterns
		"vmxnet3-tx-hang":     true,
		"vmxnet3-nic-reset":   true,
		"soft-lockup-storage": true,
		// Networking patterns - Conntrack/Netfilter
		"conntrack-table-full": true,
		"conntrack-dropping":   true,
		"netfilter-error":      true,
		// Networking patterns - iptables
		"iptables-error":       true,
		"iptables-sync-failed": true,
		// Networking patterns - NIC/Driver
		"nic-link-down":      true,
		"nic-driver-error":   true,
		"nic-tx-timeout":     true,
		"nic-firmware-error": true,
		"carrier-lost":       true,
		// Networking patterns - Network Stack
		"socket-buffer-overrun": true,
		"tcp-retransmit-error":  true,
		"arp-resolution-failed": true,
		"route-error":           true,
		// Networking patterns - CNI
		"calico-error":      true,
		"flannel-error":     true,
		"cilium-error":      true,
		"cni-plugin-failed": true,
		// Networking patterns - kube-proxy/IPVS
		"ipvs-sync-error":      true,
		"kube-proxy-error":     true,
		"endpoint-sync-failed": true,
		// Networking patterns - Pod Networking
		"veth-error":               true,
		"network-namespace-error":  true,
		"pod-network-setup-failed": true,
		// Networking patterns - Cloud Provider
		"aws-eni-error":       true,
		"azure-network-error": true,
		"nsx-error":           true,
	}

	foundPatterns := make(map[string]bool)
	for _, pattern := range patterns {
		foundPatterns[pattern.Name] = true
	}

	for expected := range expectedPatterns {
		if !foundPatterns[expected] {
			t.Errorf("GetDefaultPatterns() missing expected pattern: %s", expected)
		}
	}

	// Verify we have the right number of patterns
	if len(patterns) != len(expectedPatterns) {
		t.Errorf("GetDefaultPatterns() returned %d patterns, expected %d", len(patterns), len(expectedPatterns))
	}
}

// TestMergeWithDefaults tests the MergeWithDefaults function.
func TestMergeWithDefaults(t *testing.T) {
	tests := []struct {
		name         string
		userPatterns []LogPatternConfig
		useDefaults  bool
		expected     int // Expected number of patterns in result
	}{
		{
			name:         "no user patterns, no defaults",
			userPatterns: []LogPatternConfig{},
			useDefaults:  false,
			expected:     0,
		},
		{
			name:         "no user patterns, with defaults",
			userPatterns: []LogPatternConfig{},
			useDefaults:  true,
			expected:     58, // All default patterns (31 original + 27 networking)
		},
		{
			name: "one user pattern, no defaults",
			userPatterns: []LogPatternConfig{
				{Name: "custom-pattern", Regex: "test", Severity: "info"},
			},
			useDefaults: false,
			expected:    1,
		},
		{
			name: "one user pattern, with defaults",
			userPatterns: []LogPatternConfig{
				{Name: "custom-pattern", Regex: "test", Severity: "info"},
			},
			useDefaults: true,
			expected:    59, // 1 user + 58 defaults
		},
		{
			name: "user pattern overrides default",
			userPatterns: []LogPatternConfig{
				{Name: "oom-killer", Regex: "custom-oom", Severity: "info"}, // Override default
			},
			useDefaults: true,
			expected:    58, // Still 58 total (user pattern replaces default)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MergeWithDefaults(tt.userPatterns, tt.useDefaults)

			if len(result) != tt.expected {
				t.Errorf("MergeWithDefaults() returned %d patterns, expected %d", len(result), tt.expected)
			}

			// Verify no duplicates
			names := make(map[string]bool)
			for _, pattern := range result {
				if names[pattern.Name] {
					t.Errorf("MergeWithDefaults() resulted in duplicate pattern: %s", pattern.Name)
				}
				names[pattern.Name] = true
			}
		})
	}
}

// TestPatternAccuracy tests a subset of patterns to ensure they match expected strings
func TestPatternAccuracy(t *testing.T) {
	tests := []struct {
		patternName    string
		shouldMatch    []string
		shouldNotMatch []string
	}{
		{
			patternName: "oom-killer",
			shouldMatch: []string{
				"Out of memory: Kill process 1234 (test)",
				"Killed process 5678 (chrome)",
				"oom-killer: Task in /system.slice killed as a result of limit of /system.slice",
				"OOM killer activated",
			},
			shouldNotMatch: []string{
				"Process started normally",
				"Memory allocation successful",
			},
		},
		{
			patternName: "disk-io-error",
			shouldMatch: []string{
				"Buffer I/O error on device sda1",
				"EXT4-fs error (device sda1): ext4_lookup",
				"XFS error: failed to read directory",
				"sda: I/O error, dev sda, sector 12345",
				"SCSI error : <0 0 0 0> return code = 0x8000002",
			},
			shouldNotMatch: []string{
				"Disk operation completed successfully",
				"EXT4-fs: mounted filesystem with ordered data mode",
			},
		},
		{
			patternName: "network-timeout",
			shouldMatch: []string{
				"NETDEV WATCHDOG: eth0 (e1000e): transmit queue 0 timed out",
				"eth0: link down",
				"connect() failed: connection timed out",
				"No route to host",
				"Network is unreachable",
			},
			shouldNotMatch: []string{
				"eth0: link up",
				"Network connection established",
			},
		},
		{
			patternName: "kernel-panic",
			shouldMatch: []string{
				"kernel panic - not syncing: VFS: Unable to mount root fs",
				"Oops: 0000 [#1] SMP",
				"BUG: unable to handle kernel NULL pointer dereference",
				"general protection fault: 0000 [#1] PREEMPT SMP",
			},
			shouldNotMatch: []string{
				"Kernel started successfully",
				"System boot completed",
			},
		},
		{
			patternName: "device-failure",
			shouldMatch: []string{
				"USB device descriptor read error",
				"Failed to connect to device",
				"USB device error detected",
				"USB failed to enumerate",
			},
			shouldNotMatch: []string{
				"USB disconnect, device number 2", // Normal disconnect
				"USB device connected successfully",
			},
		},
		{
			patternName: "kubelet-oom",
			shouldMatch: []string{
				"kubelet: OOM killer activated for container",
				"kubelet: container out of memory condition detected",
				"kubelet: memory cgroup out of memory",
			},
			shouldNotMatch: []string{
				"containerd: OOM condition detected", // Not kubelet
				"kubelet: memory usage normal",
			},
		},
		{
			patternName: "kubelet-pleg-unhealthy",
			shouldMatch: []string{
				"PLEG is not healthy: pleg has yet to be successful",
				"relist operation taking too long: 5.2s",
			},
			shouldNotMatch: []string{
				"PLEG operation completed successfully",
				"relist completed in 100ms",
			},
		},
		// VMware vmxnet3 patterns
		{
			patternName: "vmxnet3-tx-hang",
			shouldMatch: []string{
				"vmxnet3 0000:03:00.0 ens160: tx hang",
				"vmxnet3 0000:0b:00.0 eth0: tx hang",
				"kernel: vmxnet3 0000:13:00.0: tx hang on queue 0",
			},
			shouldNotMatch: []string{
				"vmxnet3 driver loaded successfully",
				"vmxnet3 0000:03:00.0: intr type 3, mode 0, 9 vectors allocated",
				"NETDEV WATCHDOG: ens160 (vmxnet3): transmit queue 0 timed out", // Covered by network-timeout
			},
		},
		{
			patternName: "vmxnet3-nic-reset",
			shouldMatch: []string{
				"vmxnet3 0000:03:00.0 ens160: resetting",
				"vmxnet3 0000:0b:00.0 eth0: resetting",
			},
			shouldNotMatch: []string{
				"vmxnet3 reset complete",
				"vmxnet3 driver initialized",
			},
		},
		{
			patternName: "soft-lockup-storage",
			shouldMatch: []string{
				"watchdog: BUG: soft lockup - CPU#2 stuck for 22s! [longhorn-instan:12345]",
				"watchdog: BUG: soft lockup - CPU#0 stuck for 23s! [mpt_put_msg_fra:6789]",
				"BUG: soft lockup - CPU#1 stuck for 21s! [scsi_eh_0:1234]",
				"soft lockup - CPU#3 stuck for 25s! [iscsi_eh_0:5678]",
				"soft lockup detected in nvme_poll",
				"BUG: soft lockup - CPU#0 stuck for 22s! [nfs4_rpc_workqu:9012]",
			},
			shouldNotMatch: []string{
				"soft lockup - CPU#1 stuck for 21s! [chrome:9999]",
				"soft lockup - CPU#2 stuck for 22s! [firefox:8888]",
				"soft lockup - CPU#0 stuck for 20s! [python:7777]",
				"watchdog: BUG: soft lockup - CPU#3 stuck for 24s! [java:6666]",
			},
		},
		// Networking patterns - Conntrack
		{
			patternName: "conntrack-table-full",
			shouldMatch: []string{
				"nf_conntrack: table full, dropping packet",
				"kernel: nf_conntrack: table full",
			},
			shouldNotMatch: []string{
				"nf_conntrack: registered",
				"conntrack module loaded",
			},
		},
		{
			patternName: "conntrack-dropping",
			shouldMatch: []string{
				"nf_conntrack: dropping packet",
				"kernel: nf_conntrack: dropping packet due to timeout",
			},
			shouldNotMatch: []string{
				"nf_conntrack initialized",
				"conntrack entry created",
			},
		},
		// Networking patterns - iptables
		{
			patternName: "iptables-error",
			shouldMatch: []string{
				"iptables: error loading target",
				"iptables failed to apply rule",
				"iptables: invalid argument",
			},
			shouldNotMatch: []string{
				"iptables rule added successfully",
				"iptables version 1.8.7",
			},
		},
		// Networking patterns - NIC/Driver
		{
			patternName: "nic-link-down",
			shouldMatch: []string{
				"e1000e: eth0 NIC Link is Down",
				"igb 0000:01:00.0: Link is Down",
				"ixgbe 0000:03:00.0 eth1: NIC Link is Down",
				"mlx5_core 0000:5e:00.0: Link is Down",
				"bnxt_en: enp3s0f0 Link is Down",
			},
			shouldNotMatch: []string{
				"e1000e: eth0 NIC Link is Up",
				"igb: driver loaded successfully",
			},
		},
		{
			patternName: "nic-tx-timeout",
			shouldMatch: []string{
				"NETDEV WATCHDOG: eth0 (e1000e): transmit queue 0 timed out",
				"NETDEV WATCHDOG: ens192 (vmxnet3): transmit queue 1 timed out",
			},
			shouldNotMatch: []string{
				"transmit completed successfully",
				"NETDEV initialized",
			},
		},
		{
			patternName: "carrier-lost",
			shouldMatch: []string{
				"eth0: carrier lost",
				"bond0: carrier off",
			},
			shouldNotMatch: []string{
				"carrier detected",
				"carrier on",
			},
		},
		// Networking patterns - Network Stack
		{
			patternName: "socket-buffer-overrun",
			shouldMatch: []string{
				"socket buffer overrun detected",
				"1234 packets pruned from receive queue because of socket buffer",
				"RcvbufErrors: 5000",
			},
			shouldNotMatch: []string{
				"socket created successfully",
				"buffer allocation complete",
			},
		},
		{
			patternName: "arp-resolution-failed",
			shouldMatch: []string{
				"ARP resolution failed for 10.0.0.1",
				"no ARP reply received",
				"neighbor 192.168.1.1 FAILED",
			},
			shouldNotMatch: []string{
				"ARP reply received",
				"neighbor resolved",
			},
		},
		// Networking patterns - CNI
		{
			patternName: "calico-error",
			shouldMatch: []string{
				"calico-node: error connecting to datastore",
				"felix: error applying policy",
			},
			shouldNotMatch: []string{
				"calico-node: started successfully",
				"felix: policy applied",
			},
		},
		{
			patternName: "cilium-error",
			shouldMatch: []string{
				"cilium-agent: error loading BPF program",
				"cilium-agent: panic during initialization",
				"BPF program load failed: permission denied",
			},
			shouldNotMatch: []string{
				"cilium-agent: started successfully",
				"BPF program loaded",
			},
		},
		// Networking patterns - kube-proxy
		{
			patternName: "ipvs-sync-error",
			shouldMatch: []string{
				"ipvs: error adding service",
				"ipvs sync failed: connection refused",
				"kube-proxy: ipvs failed to update",
			},
			shouldNotMatch: []string{
				"ipvs: service added",
				"ipvs sync completed",
			},
		},
		// Networking patterns - Pod Networking
		{
			patternName: "veth-error",
			shouldMatch: []string{
				"veth: error creating pair",
				"veth: failed to set link up",
				"veth: cannot create interface",
			},
			shouldNotMatch: []string{
				"veth pair created",
				"veth initialized",
			},
		},
		// Networking patterns - Cloud Provider
		{
			patternName: "aws-eni-error",
			shouldMatch: []string{
				"ENI attachment failed",
				"aws: eni error during attach",
				"vpc-cni: error allocating IP",
			},
			shouldNotMatch: []string{
				"ENI attached successfully",
				"vpc-cni: IP allocated",
			},
		},
		{
			patternName: "nsx-error",
			shouldMatch: []string{
				"nsx-node-agent: error connecting to manager",
				"nsx: failed to apply firewall rule",
				"nsx-t: timeout waiting for response",
			},
			shouldNotMatch: []string{
				"nsx-node-agent: connected",
				"nsx: rule applied",
			},
		},
	}

	// Get default patterns and create a map
	defaultPatterns := GetDefaultPatterns()
	patternMap := make(map[string]LogPatternConfig)
	for _, pattern := range defaultPatterns {
		patternMap[pattern.Name] = pattern
	}

	for _, test := range tests {
		t.Run(test.patternName, func(t *testing.T) {
			pattern, exists := patternMap[test.patternName]
			if !exists {
				t.Fatalf("Pattern %s not found in DefaultLogPatterns", test.patternName)
			}

			// Compile the regex
			regex, err := regexp.Compile(pattern.Regex)
			if err != nil {
				t.Fatalf("Failed to compile regex for pattern %s: %v", test.patternName, err)
			}

			// Test positive matches
			for _, should := range test.shouldMatch {
				if !regex.MatchString(should) {
					t.Errorf("Pattern %s should match: %q", test.patternName, should)
				}
			}

			// Test negative matches
			for _, shouldNot := range test.shouldNotMatch {
				if regex.MatchString(shouldNot) {
					t.Errorf("Pattern %s should NOT match: %q", test.patternName, shouldNot)
				}
			}
		})
	}
}

// TestPatternValidation tests that all default patterns compile correctly
func TestPatternValidation(t *testing.T) {
	patterns := GetDefaultPatterns()

	for _, pattern := range patterns {
		t.Run(pattern.Name, func(t *testing.T) {
			// Test regex compilation
			_, err := regexp.Compile(pattern.Regex)
			if err != nil {
				t.Errorf("Pattern %s has invalid regex: %v", pattern.Name, err)
			}

			// Test required fields
			if pattern.Name == "" {
				t.Error("Pattern has empty Name")
			}
			if pattern.Regex == "" {
				t.Error("Pattern has empty Regex")
			}
			if pattern.Severity == "" {
				t.Error("Pattern has empty Severity")
			}
			if pattern.Description == "" {
				t.Error("Pattern has empty Description")
			}

			// Test valid severity values
			validSeverities := []string{"error", "warning", "info"}
			validSeverity := false
			for _, valid := range validSeverities {
				if pattern.Severity == valid {
					validSeverity = true
					break
				}
			}
			if !validSeverity {
				t.Errorf("Pattern %s has invalid severity: %s", pattern.Name, pattern.Severity)
			}

			// Test valid source values
			validSources := []string{"kmsg", "journal", "both"}
			validSource := false
			for _, valid := range validSources {
				if pattern.Source == valid {
					validSource = true
					break
				}
			}
			if !validSource {
				t.Errorf("Pattern %s has invalid source: %s", pattern.Name, pattern.Source)
			}
		})
	}
}
