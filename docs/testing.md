# Node Doctor Testing Guide

This guide covers comprehensive testing procedures for all node-doctor monitors in a real Kubernetes cluster.

## Overview

Node-doctor monitors are categorized by risk level for testing:

| Risk Level | Description | When to Use |
|------------|-------------|-------------|
| **SAFE** | Read-only checks, no system impact | Any node, any time |
| **CAUTION** | May generate alerts, minimal impact | Non-production node |
| **DESTRUCTIVE** | Can break node/cluster | Dedicated test node only |

## Prerequisites

### Required Tools
- `kubectl` configured for your cluster
- `stress-ng` for CPU/memory tests (`apt install stress-ng`)
- `fio` for disk I/O tests (`apt install fio`)
- Root access on the test node

### Cluster Requirements
- Node-doctor DaemonSet deployed and running
- Monitoring/alerting configured to observe events
- Access to node via SSH or console

### Test Node Preparation
For destructive tests, prepare a dedicated test node:

```bash
# Identify test node
kubectl get nodes

# Cordon (prevent new pods)
kubectl cordon <test-node>

# Drain (evict existing pods)
kubectl drain <test-node> --ignore-daemonsets --delete-emptydir-data
```

## Quick Start

### Using the Test Script

The `scripts/test-monitors.sh` script provides automated testing:

```bash
# Safe tests only (recommended first)
sudo ./scripts/test-monitors.sh --safe

# Test log pattern detection
sudo ./scripts/test-monitors.sh --patterns

# Caution tests (with confirmation)
sudo ./scripts/test-monitors.sh --caution

# Destructive tests (dedicated test node only!)
sudo ./scripts/test-monitors.sh --destructive

# All tests
sudo ./scripts/test-monitors.sh --all

# Cleanup only
sudo ./scripts/test-monitors.sh --cleanup
```

### Environment Variables

```bash
export NAMESPACE="node-doctor"        # node-doctor namespace
export NODE_NAME="test-node-1"        # Target node name
export STRESS_DURATION="30"           # Stress test duration (seconds)
export TEST_TIMEOUT="60"              # Event detection timeout
```

## Monitor Test Procedures

### 1. System Monitors

#### CPU Monitor (`system-cpu`)
**Risk: SAFE**

| Test | Command | Expected Event |
|------|---------|----------------|
| Normal state | (no action) | `CPUHealthy: True` |
| High load | `stress-ng --cpu $(nproc) --timeout 120s` | `HighCPULoad` |
| Thermal throttle | `echo "CPU0: Package temperature/speed high" > /dev/kmsg` | `CPUThermalThrottle` |

**Cleanup:** `pkill stress-ng`

#### Memory Monitor (`system-memory`)
**Risk: CAUTION to DESTRUCTIVE**

| Test | Command | Expected Event |
|------|---------|----------------|
| Normal state | (no action) | `MemoryHealthy: True` |
| High usage | `stress-ng --vm 1 --vm-bytes 80% --timeout 60s` | `ElevatedMemoryUsage` |
| OOM trigger | `stress-ng --vm 1 --vm-bytes 95% --timeout 60s` | `OOMKillDetected` |

**Cleanup:** `pkill stress-ng`

#### Disk Monitor (`system-disk`)
**Risk: SAFE to DESTRUCTIVE**

| Test | Command | Expected Event |
|------|---------|----------------|
| Normal state | (no action) | `DiskHealthy: True` |
| High usage | `fallocate -l 10G /tmp/testfile` | `ElevatedDiskUsage` |
| I/O stress | `fio --name=test --rw=randrw --size=1G` | `HighIOWait` |

**Cleanup:** `rm /tmp/testfile`

### 2. Kubernetes Monitors

#### Kubelet Monitor (`kubernetes-kubelet-check`)
**Risk: SAFE to DESTRUCTIVE**

| Test | Command | Expected Event |
|------|---------|----------------|
| Healthy | (no action) | `KubeletHealthy: True` |
| Health endpoint fail | `iptables -A OUTPUT -p tcp --dport 10248 -j DROP` | `KubeletHealthCheckFailed` |
| Stop kubelet | `systemctl stop kubelet` | `KubeletSystemdInactive` |

**Cleanup:** `systemctl start kubelet; iptables -F`

#### API Server Monitor (`kubernetes-apiserver-check`)
**Risk: SAFE to DESTRUCTIVE**

| Test | Command | Expected Event |
|------|---------|----------------|
| Healthy | (no action) | `APIServerHealthy: True` |
| High latency | `tc qdisc add dev eth0 root netem delay 3000ms` | `APIServerSlow` |
| Block API | `iptables -A OUTPUT -p tcp --dport 6443 -j DROP` | `APIServerUnreachable` |

**Cleanup:** `tc qdisc del dev eth0 root; iptables -F`

#### Runtime Monitor (`kubernetes-runtime-check`)
**Risk: SAFE to DESTRUCTIVE**

| Test | Command | Expected Event |
|------|---------|----------------|
| Healthy | (no action) | `RuntimeHealthy: True` |
| Socket blocked | `chmod 000 /run/containerd/containerd.sock` | `RuntimeSocketUnreachable` |
| Stop runtime | `systemctl stop containerd` | `RuntimeServiceInactive` |

**Cleanup:** `systemctl start containerd; chmod 660 /run/containerd/containerd.sock`

### 3. Network Monitors

#### DNS Monitor (`network-dns-check`)
**Risk: SAFE to DESTRUCTIVE**

| Test | Command | Expected Event |
|------|---------|----------------|
| Healthy | (no action) | `NetworkReachable: True` |
| Block DNS | `iptables -A OUTPUT -p udp --dport 53 -j DROP` | `ClusterDNSFailed` |

**Cleanup:** `iptables -F`

#### Gateway Monitor (`network-gateway-check`)
**Risk: SAFE**

| Test | Command | Expected Event |
|------|---------|----------------|
| Healthy | (no action) | `GatewayReachable` |
| Block ICMP | `iptables -A OUTPUT -p icmp -j DROP` | `GatewayUnreachable` |
| High latency | `tc qdisc add dev eth0 root netem delay 200ms` | `GatewayLatencyHigh` |

**Cleanup:** `iptables -F; tc qdisc del dev eth0 root`

#### CNI Monitor (`network-cni-check`)
**Risk: CAUTION**

| Test | Command | Expected Event |
|------|---------|----------------|
| Healthy | (no action) | `CNIHealthy` |
| Block peers | `iptables -A INPUT -s 10.42.0.0/16 -j DROP` | `PeerUnreachable` |

**Cleanup:** `iptables -F`

### 4. Log Pattern Monitor (`custom-logpattern`)
**Risk: SAFE**

Inject test messages to validate pattern detection:

```bash
# Kernel messages (requires root)
echo "nf_conntrack: table full, dropping packet" > /dev/kmsg
echo "vmxnet3 0000:03:00.0: tx hang" > /dev/kmsg
echo "Buffer I/O error on device sda1" > /dev/kmsg

# Journal messages
logger -t calico "error: connection failed"
logger -t kubelet "PLEG is not healthy"
```

## Destructive Tests Summary

These tests can break your node/cluster:

| Test | Impact | Recovery |
|------|--------|----------|
| Stop Kubelet | Node NotReady, pods evicted | `systemctl start kubelet` |
| Stop Container Runtime | All containers killed | `systemctl start containerd` |
| Fill Disk 95%+ | Kubelet/etcd may crash | Delete files, restart services |
| OOM Test | Random process killed | Restart killed process |
| Block DNS | Cluster-wide DNS failure | `iptables -F` |
| Block API Server | Node loses control plane | `iptables -F` |

## Recommended Test Sequence

### Phase 1: Safe Tests (Any Node)
1. Log pattern injection
2. CPU/Memory/Disk normal state
3. Gateway connectivity
4. External endpoint connectivity

### Phase 2: Caution Tests (Non-Critical Node)
1. CPU stress (70% load)
2. Memory stress (70% usage)
3. Disk space test (5-10GB file)
4. Network latency injection

### Phase 3: Destructive Tests (Dedicated Test Node)

**Before:**
```bash
kubectl cordon <test-node>
kubectl drain <test-node> --ignore-daemonsets --delete-emptydir-data
```

**Tests:**
1. API server block
2. DNS block
3. OOM trigger
4. Kubelet stop/start

**After:**
```bash
kubectl uncordon <test-node>
```

## Verification Commands

```bash
# Watch node-doctor logs
kubectl logs -f -l app=node-doctor -n node-doctor

# Watch Kubernetes events
kubectl get events -A --watch --sort-by='.lastTimestamp'

# Check node conditions
kubectl describe node <node-name> | grep -A20 Conditions

# Check dmesg for kernel messages
dmesg | tail -50

# Check journald for service messages
journalctl -u kubelet -n 50
```

## Troubleshooting

### Events Not Generated
1. Verify node-doctor is running: `kubectl get pods -n node-doctor`
2. Check node-doctor logs for errors
3. Verify monitor is enabled in configuration
4. Check pattern regex matches test message

### Test Cleanup Failed
```bash
# Full cleanup
pkill -9 stress-ng
pkill -9 stress
iptables -F
for iface in $(ip -o link show | awk -F': ' '{print $2}' | grep -v lo); do
    tc qdisc del dev "$iface" root 2>/dev/null
done
rm -f /tmp/testfile-*
```

### Node Stuck NotReady
```bash
# Restart kubelet
systemctl restart kubelet

# Check kubelet status
systemctl status kubelet

# Check kubelet logs
journalctl -u kubelet -n 100 --no-pager
```

## Test Configuration

For testing-specific node-doctor configuration, see:
- [config/examples/testing.yaml](../config/examples/testing.yaml)

This configuration includes:
- Increased check frequency for faster event detection
- All monitors enabled
- Debug logging enabled
- All log patterns active
