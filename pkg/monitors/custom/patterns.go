package custom

// DefaultLogPatterns provides sensible default patterns for common system issues
var DefaultLogPatterns = []LogPatternConfig{
	{
		Name:        "oom-killer",
		Regex:       "(Out of memory|Killed process \\d+|oom-killer|OOM killer)",
		Severity:    "error",
		Description: "Out of memory condition detected - kernel OOM killer activated",
		Source:      "both",
	},
	{
		Name:        "disk-io-error",
		Regex:       "(I/O error|Buffer I/O error|EXT4-fs.*error|XFS.*error|sd[a-z]+.*error|SCSI error|Medium Error|critical medium error)",
		Severity:    "error",
		Description: "Disk I/O error detected - possible hardware failure",
		Source:      "kmsg",
	},
	{
		Name:        "network-timeout",
		Regex:       "(NETDEV WATCHDOG.*transmit queue.*timed out|eth\\d+.*link down|connection timed out|No route to host|Network is unreachable)",
		Severity:    "warning",
		Description: "Network timeout or connectivity issue detected",
		Source:      "both",
	},
	{
		Name:        "kernel-panic",
		Regex:       "(kernel panic|Oops|BUG: unable to handle|general protection fault)",
		Severity:    "error",
		Description: "Kernel panic or critical system error detected",
		Source:      "kmsg",
	},
	{
		Name:        "memory-corruption",
		Regex:       "(bad page state|page allocation failure|segfault at)",
		Severity:    "error",
		Description: "Memory corruption or allocation failure detected",
		Source:      "kmsg",
	},
	{
		Name:        "device-failure",
		Regex:       "(device descriptor read error|Failed to connect to device|USB.*(?:error|failed))",
		Severity:    "warning",
		Description: "Device descriptor or connection error (not normal disconnect)",
		Source:      "kmsg",
	},
	{
		Name:        "filesystem-readonly",
		Regex:       "(Remounting filesystem read-only|EXT4-fs.*remounted.*read-only|XFS.*forcing shutdown)",
		Severity:    "error",
		Description: "Filesystem remounted read-only due to errors",
		Source:      "kmsg",
	},
	// Enhanced Kubelet Monitoring Patterns (12 specific patterns)
	{
		Name:        "kubelet-oom",
		Regex:       "\\bkubelet\\b.*(?:OOM|out of memory|memory cgroup out of memory)",
		Severity:    "error",
		Description: "Kubelet detected OOM condition in container/pod",
		Source:      "journal",
	},
	{
		Name:        "kubelet-pleg-unhealthy",
		Regex:       "(?:PLEG is not healthy|relist.*taking too long)",
		Severity:    "error",
		Description: "PLEG is unhealthy - pod lifecycle events are delayed",
		Source:      "journal",
	},
	{
		Name:        "kubelet-eviction",
		Regex:       "(?:eviction manager.*evicting|Evicting pod)",
		Severity:    "warning",
		Description: "Kubelet is evicting pods due to resource pressure",
		Source:      "journal",
	},
	{
		Name:        "kubelet-disk-pressure",
		Regex:       "(?:disk pressure|DiskPressure|out of disk|eviction manager.*disk)",
		Severity:    "warning",
		Description: "Kubelet detected disk pressure condition",
		Source:      "journal",
	},
	{
		Name:        "kubelet-memory-pressure",
		Regex:       "(?:memory pressure|MemoryPressure|eviction manager.*memory)",
		Severity:    "warning",
		Description: "Kubelet detected memory pressure condition",
		Source:      "journal",
	},
	{
		Name:        "kubelet-image-pull-failed",
		Regex:       "(?:Failed to pull image|ErrImagePull|ImagePullBackOff|pull access denied)",
		Severity:    "warning",
		Description: "Kubelet failed to pull container image",
		Source:      "journal",
	},
	{
		Name:        "kubelet-cni-error",
		Regex:       "(?:CNI.*error|network plugin.*error|failed to set up sandbox)",
		Severity:    "error",
		Description: "CNI network plugin error - pod networking may fail",
		Source:      "journal",
	},
	{
		Name:        "kubelet-runtime-error",
		Regex:       "(?:container runtime.*error|runtime.*error|failed to create container|Failed to start container)",
		Severity:    "error",
		Description: "Container runtime error - container operations failing",
		Source:      "journal",
	},
	{
		Name:        "kubelet-certificate-rotation",
		Regex:       "(?:certificate.*(?:expired|rotation.*failed)|unable to rotate|certificate.*(?:renewal.*error|error))",
		Severity:    "error",
		Description: "Kubelet certificate rotation failure - may lose API access",
		Source:      "journal",
	},
	{
		Name:        "kubelet-node-not-ready",
		Regex:       "(?:Node.*NotReady|node status.*NotReady|setting node status.*NotReady)",
		Severity:    "error",
		Description: "Kubelet set node status to NotReady",
		Source:      "journal",
	},
	{
		Name:        "kubelet-api-connection-error",
		Regex:       "(?:Unable to register node|failed to contact API server|connection refused.*apiserver|API server.*(?:unreachable|error))",
		Severity:    "error",
		Description: "Kubelet cannot communicate with API server",
		Source:      "journal",
	},
	{
		Name:        "kubelet-volume-mount-error",
		Regex:       "(?:MountVolume.SetUp failed|Unable to attach or mount|failed to mount volume|volume mount.*(?:timeout|error))",
		Severity:    "error",
		Description: "Volume mount failure - pod may not start",
		Source:      "journal",
	},
	{
		Name:        "containerd-error",
		Regex:       "(containerd.*(?:failed to start|crash|panic))",
		Severity:    "error",
		Description: "Critical containerd error - service may be degraded",
		Source:      "journal",
	},
	{
		Name:        "docker-error",
		Regex:       "(dockerd.*error|docker.*failed|Failed to start docker)",
		Severity:    "error",
		Description: "Docker daemon error detected in systemd journal",
		Source:      "journal",
	},
	{
		Name:        "cpu-thermal-throttle",
		Regex:       "(Package temperature/speed (high|normal)|cpu clock throttled|thermal_sys.*Throttling|CPU\\d+.*throttled)",
		Severity:    "warning",
		Description: "CPU thermal throttling detected - possible thermal/cooling issue",
		Source:      "kmsg",
	},
	{
		Name:        "nfs-server-timeout",
		Regex:       "nfs.*(?:not responding|timed out|Timeout waiting|RPC.*timeout)",
		Severity:    "error",
		Description: "NFS server timeout - mount may hang or become unavailable",
		Source:      "kmsg",
	},
	{
		Name:        "nfs-stale-filehandle",
		Regex:       "(?:Stale file handle|NFS_STALE|Clearing.*NFS_STALE)",
		Severity:    "error",
		Description: "NFS stale file handle - filesystem inconsistency detected",
		Source:      "kmsg",
	},
	{
		Name:        "numa-balancing-failure",
		Regex:       "numa.*(?:balancing failed|memory pressure|high remote|imbalance|disabled)",
		Severity:    "warning",
		Description: "NUMA balancing issue detected - may impact memory performance",
		Source:      "kmsg",
	},
	{
		Name:        "hardware-mce-error",
		Regex:       "mce:.*\\[Hardware Error\\]",
		Severity:    "error",
		Description: "Machine Check Exception - hardware error detected",
		Source:      "kmsg",
	},
	{
		Name:        "edac-uncorrectable-error",
		Regex:       "EDAC.*(?:UE|Uncorrectable|FATAL|poisoned)",
		Severity:    "error",
		Description: "EDAC uncorrectable memory error - hardware failure",
		Source:      "kmsg",
	},
	{
		Name:        "edac-correctable-error",
		Regex:       "(?i)EDAC.*(?:\\bCE\\b|single-bit|\\bcorrectable\\b|\\bcorrected\\b)",
		Severity:    "warning",
		Description: "EDAC correctable memory error - monitor for degradation",
		Source:      "kmsg",
	},
	// VMware vmxnet3 patterns (Case 01607046 - TX hang cascade failures)
	// Note: Using "both" source because kernel messages appear in both kmsg and journal
	{
		Name:        "vmxnet3-tx-hang",
		Regex:       "vmxnet3.*tx hang",
		Severity:    "error",
		Description: "VMware vmxnet3 virtual NIC transmit hang - causes network disruption and can cascade to storage failures (Longhorn, iSCSI)",
		Source:      "both",
	},
	{
		Name:        "vmxnet3-nic-reset",
		Regex:       "vmxnet3.*resetting",
		Severity:    "warning",
		Description: "VMware vmxnet3 NIC reset in progress - brief network outage during recovery",
		Source:      "both",
	},
	{
		Name:        "soft-lockup-storage",
		Regex:       "soft lockup.*(?:longhorn|mpt|scsi|iscsi|nvme|nfs)",
		Severity:    "error",
		Description: "CPU soft lockup in storage subsystem - may indicate I/O stall from network/storage issues",
		Source:      "both",
	},
	// ============================================
	// NETWORKING PATTERNS
	// ============================================

	// Conntrack/Netfilter Issues
	{
		Name:        "conntrack-table-full",
		Regex:       "nf_conntrack.*table full",
		Severity:    "error",
		Description: "Connection tracking table exhausted - packets being dropped",
		Source:      "kmsg",
	},
	{
		Name:        "conntrack-dropping",
		Regex:       "nf_conntrack.*dropping packet",
		Severity:    "error",
		Description: "Conntrack actively dropping packets due to resource exhaustion",
		Source:      "kmsg",
	},
	{
		Name:        "netfilter-error",
		Regex:       "(?:nf_tables|nftables|netfilter).*error",
		Severity:    "error",
		Description: "Netfilter subsystem error - firewall rules may be broken",
		Source:      "kmsg",
	},

	// iptables Issues
	{
		Name:        "iptables-error",
		Regex:       "iptables.*(?:error|failed|invalid argument)",
		Severity:    "error",
		Description: "iptables rule application failure",
		Source:      "both",
	},
	{
		Name:        "iptables-sync-failed",
		Regex:       "(?:iptables|kube-proxy).*sync.*failed",
		Severity:    "error",
		Description: "kube-proxy iptables synchronization failure",
		Source:      "journal",
	},

	// NIC/Driver Errors
	{
		Name:        "nic-link-down",
		Regex:       "(?:e1000e|igb|ixgbe|mlx|bnxt|r8169).*Link is Down",
		Severity:    "warning",
		Description: "Physical NIC link down - check cable/switch",
		Source:      "kmsg",
	},
	{
		Name:        "nic-driver-error",
		Regex:       "(?:e1000|igb|ixgbe|mlx[45]|bnxt|i40e).*error",
		Severity:    "error",
		Description: "NIC driver error - possible hardware issue",
		Source:      "kmsg",
	},
	{
		Name:        "nic-tx-timeout",
		Regex:       "NETDEV WATCHDOG.*transmit.*timed out",
		Severity:    "error",
		Description: "NIC transmit queue timeout - driver/hardware issue",
		Source:      "kmsg",
	},
	{
		Name:        "nic-firmware-error",
		Regex:       "(?:firmware|nvram).*failed|Unable to load firmware",
		Severity:    "error",
		Description: "NIC firmware loading failure",
		Source:      "kmsg",
	},
	{
		Name:        "carrier-lost",
		Regex:       "carrier (?:lost|off)",
		Severity:    "warning",
		Description: "Network carrier signal lost",
		Source:      "kmsg",
	},

	// Network Stack Issues
	{
		Name:        "socket-buffer-overrun",
		Regex:       "(?:socket buffer.*overrun|packets.*pruned.*socket|RcvbufErrors)",
		Severity:    "warning",
		Description: "Socket receive buffer overflow - application not reading fast enough",
		Source:      "kmsg",
	},
	{
		Name:        "tcp-retransmit-error",
		Regex:       "TCP.*retransmit.*timeout|tcp_retries.*exceeded",
		Severity:    "warning",
		Description: "Excessive TCP retransmissions - network congestion or packet loss",
		Source:      "kmsg",
	},
	{
		Name:        "arp-resolution-failed",
		Regex:       "(?:ARP.*failed|no ARP.*reply|neighbor.*FAILED)",
		Severity:    "warning",
		Description: "ARP resolution failure - network segment issue",
		Source:      "kmsg",
	},
	{
		Name:        "route-error",
		Regex:       "(?:RTNETLINK.*error|route.*failed|no route to host)",
		Severity:    "error",
		Description: "Routing table error",
		Source:      "kmsg",
	},

	// CNI Plugin Errors
	{
		Name:        "calico-error",
		Regex:       "(?:calico|felix).*error",
		Severity:    "error",
		Description: "Calico CNI plugin error",
		Source:      "journal",
	},
	{
		Name:        "flannel-error",
		Regex:       "flannel.*(?:error|panic|failed)",
		Severity:    "error",
		Description: "Flannel CNI plugin error",
		Source:      "journal",
	},
	{
		Name:        "cilium-error",
		Regex:       "cilium-agent.*(?:error|panic)|BPF.*load.*failed",
		Severity:    "error",
		Description: "Cilium eBPF agent error",
		Source:      "journal",
	},
	{
		Name:        "cni-plugin-failed",
		Regex:       "CNI plugin.*(?:error|failed|timeout)",
		Severity:    "error",
		Description: "Generic CNI plugin failure",
		Source:      "journal",
	},

	// kube-proxy/IPVS Issues
	{
		Name:        "ipvs-sync-error",
		Regex:       "ipvs.*(?:error|failed|sync failed)",
		Severity:    "error",
		Description: "IPVS load balancer synchronization failure",
		Source:      "journal",
	},
	{
		Name:        "kube-proxy-error",
		Regex:       "kube-proxy.*(?:error|failed)",
		Severity:    "error",
		Description: "kube-proxy service routing error",
		Source:      "journal",
	},
	{
		Name:        "endpoint-sync-failed",
		Regex:       "(?:endpoint.*syncing|UpdateEndpoints).*failed",
		Severity:    "warning",
		Description: "Service endpoint update failure",
		Source:      "journal",
	},

	// Pod Networking Issues
	{
		Name:        "veth-error",
		Regex:       "veth.*(?:error|failed|cannot create)",
		Severity:    "error",
		Description: "Virtual ethernet interface creation failure",
		Source:      "kmsg",
	},
	{
		Name:        "network-namespace-error",
		Regex:       "(?:netns|network namespace).*(?:error|failed)",
		Severity:    "error",
		Description: "Network namespace operation failure",
		Source:      "kmsg",
	},
	{
		Name:        "pod-network-setup-failed",
		Regex:       "failed to (?:set up|setup).*network",
		Severity:    "error",
		Description: "Pod network initialization failure",
		Source:      "journal",
	},

	// Cloud Provider Issues
	{
		Name:        "aws-eni-error",
		Regex:       "(?:eni|ENI|vpc-cni).*(?:error|failed)",
		Severity:    "error",
		Description: "AWS ENI or VPC CNI plugin error",
		Source:      "journal",
	},
	{
		Name:        "azure-network-error",
		Regex:       "azure.*(?:cni|network).*(?:error|failed)",
		Severity:    "error",
		Description: "Azure networking error",
		Source:      "journal",
	},
	{
		Name:        "nsx-error",
		Regex:       "nsx.*(?:error|failed|timeout)",
		Severity:    "error",
		Description: "VMware NSX network virtualization error",
		Source:      "journal",
	},
}

// GetDefaultPatterns returns a copy of the default patterns
func GetDefaultPatterns() []LogPatternConfig {
	patterns := make([]LogPatternConfig, len(DefaultLogPatterns))
	copy(patterns, DefaultLogPatterns)
	return patterns
}

// MergeWithDefaults merges user patterns with defaults, with user patterns taking precedence
func MergeWithDefaults(userPatterns []LogPatternConfig, useDefaults bool) []LogPatternConfig {
	if !useDefaults {
		return userPatterns
	}

	// Create a map of user pattern names for quick lookup
	userPatternMap := make(map[string]bool)
	for _, pattern := range userPatterns {
		userPatternMap[pattern.Name] = true
	}

	// Start with user patterns
	merged := make([]LogPatternConfig, 0, len(userPatterns)+len(DefaultLogPatterns))
	merged = append(merged, userPatterns...)

	// Add default patterns that aren't overridden by user
	for _, defaultPattern := range DefaultLogPatterns {
		if !userPatternMap[defaultPattern.Name] {
			merged = append(merged, defaultPattern)
		}
	}

	return merged
}
