#!/bin/bash
# =============================================================================
# Node Doctor Comprehensive Monitor Test Script
# =============================================================================
# Tests all node-doctor monitors in a real Kubernetes cluster.
# Categorized by risk level to prevent accidental cluster damage.
#
# Usage: sudo ./test-monitors.sh [--safe|--caution|--destructive|--patterns|--all]
#        ./test-monitors.sh --help
#
# Risk Levels:
#   SAFE       - Read-only tests, no system impact
#   CAUTION    - May generate alerts, minimal impact
#   DESTRUCTIVE - Can break node/cluster, requires dedicated test node
# =============================================================================

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m'

# Logging functions
log_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[✓]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[⚠]${NC} $1"; }
log_error() { echo -e "${RED}[✗]${NC} $1"; }
log_safe() { echo -e "${GREEN}[🟢 SAFE]${NC} $1"; }
log_caution() { echo -e "${YELLOW}[🟡 CAUTION]${NC} $1"; }
log_destructive() { echo -e "${RED}[🔴 DESTRUCTIVE]${NC} $1"; }

# Configuration
NAMESPACE="${NAMESPACE:-node-doctor}"
NODE_NAME="${NODE_NAME:-$(hostname)}"
STRESS_DURATION="${STRESS_DURATION:-30}"
TEST_TIMEOUT="${TEST_TIMEOUT:-60}"

# =============================================================================
# HELPER FUNCTIONS
# =============================================================================

check_root() {
    if [ "$EUID" -ne 0 ]; then
        log_error "This script must be run as root for /dev/kmsg access"
        exit 1
    fi
}

check_stress_ng() {
    if ! command -v stress-ng &> /dev/null; then
        log_warn "stress-ng not found. Install with: apt install stress-ng"
        return 1
    fi
    return 0
}

check_fio() {
    if ! command -v fio &> /dev/null; then
        log_warn "fio not found. Install with: apt install fio"
        return 1
    fi
    return 0
}

inject_kmsg() {
    local message="$1"
    echo "$message" > /dev/kmsg
    log_success "Injected to /dev/kmsg: $message"
}

wait_for_event() {
    local reason="$1"
    local timeout="${2:-30}"
    log_info "Waiting up to ${timeout}s for event: $reason"

    local end=$((SECONDS + timeout))
    while [ $SECONDS -lt $end ]; do
        if kubectl get events -A --field-selector reason="$reason" 2>/dev/null | grep -q "$reason"; then
            log_success "Event detected: $reason"
            return 0
        fi
        sleep 2
    done
    log_warn "Event not detected within timeout: $reason"
    return 1
}

show_node_doctor_logs() {
    log_info "Recent node-doctor logs:"
    kubectl logs -l app=node-doctor -n "$NAMESPACE" --tail=20 2>/dev/null || \
        log_warn "Could not fetch node-doctor logs"
}

show_recent_events() {
    log_info "Recent Kubernetes events:"
    kubectl get events -A --sort-by='.lastTimestamp' 2>/dev/null | tail -15 || \
        log_warn "Could not fetch events"
}

cleanup_iptables() {
    log_info "Cleaning up iptables rules..."
    iptables -F 2>/dev/null || true
    log_success "iptables rules flushed"
}

cleanup_tc() {
    log_info "Cleaning up traffic control rules..."
    for iface in $(ip -o link show | awk -F': ' '{print $2}' | grep -v lo); do
        tc qdisc del dev "$iface" root 2>/dev/null || true
    done
    log_success "tc rules cleaned"
}

cleanup_stress() {
    log_info "Stopping stress processes..."
    pkill -9 stress-ng 2>/dev/null || true
    pkill -9 stress 2>/dev/null || true
    log_success "Stress processes stopped"
}

cleanup_all() {
    echo ""
    log_info "=== Running full cleanup ==="
    cleanup_stress
    cleanup_iptables
    cleanup_tc
    rm -f /tmp/testfile-* 2>/dev/null || true
    log_success "Full cleanup complete"
}

trap cleanup_all EXIT

# =============================================================================
# SAFE TESTS - Read-only, no system impact
# =============================================================================

test_safe_log_patterns() {
    log_safe "Testing Log Pattern Monitor (message injection)"
    echo ""

    # OOM Kill
    log_info "Injecting OOM killer message..."
    inject_kmsg "Out of memory: Killed process 12345 (stress-test) total-vm:1024kB"
    sleep 1

    # Disk I/O Error
    log_info "Injecting disk I/O error message..."
    inject_kmsg "Buffer I/O error on device sda1, logical block 12345"
    sleep 1

    # Conntrack table full
    log_info "Injecting conntrack table full message..."
    inject_kmsg "nf_conntrack: table full, dropping packet"
    sleep 1

    # NIC link down
    log_info "Injecting NIC link down message..."
    inject_kmsg "e1000e 0000:00:19.0 eth0: NIC Link is Down"
    sleep 1

    # Calico error (via logger)
    log_info "Injecting Calico error message..."
    logger -t calico "error: Unable to connect to datastore"
    sleep 1

    # vmxnet3 TX hang
    log_info "Injecting vmxnet3 TX hang message..."
    inject_kmsg "vmxnet3 0000:03:00.0 ens160: tx hang"
    sleep 1

    log_success "Log pattern injection complete"
    echo ""
    show_recent_events
}

test_safe_cpu_normal() {
    log_safe "Testing CPU Monitor (normal state)"
    log_info "Verifying CPU monitor reports healthy state..."
    show_node_doctor_logs
    log_success "CPU monitor test complete"
}

test_safe_memory_normal() {
    log_safe "Testing Memory Monitor (normal state)"
    log_info "Verifying memory monitor reports healthy state..."
    show_node_doctor_logs
    log_success "Memory monitor test complete"
}

test_safe_disk_normal() {
    log_safe "Testing Disk Monitor (normal state)"
    log_info "Verifying disk monitor reports healthy state..."
    df -h /
    show_node_doctor_logs
    log_success "Disk monitor test complete"
}

test_safe_gateway() {
    log_safe "Testing Gateway Monitor"
    log_info "Verifying gateway reachability..."
    local gateway
    gateway=$(ip route | grep default | awk '{print $3}' | head -1)
    if [ -n "$gateway" ]; then
        ping -c 3 "$gateway" && log_success "Gateway $gateway is reachable"
    else
        log_warn "No default gateway found"
    fi
}

test_safe_connectivity() {
    log_safe "Testing Connectivity Monitor"
    log_info "Testing external endpoints..."

    # Test common endpoints
    for endpoint in "google.com:443" "cloudflare.com:443" "1.1.1.1:443"; do
        host="${endpoint%:*}"
        port="${endpoint#*:}"
        if timeout 5 bash -c "echo > /dev/tcp/$host/$port" 2>/dev/null; then
            log_success "Endpoint reachable: $endpoint"
        else
            log_warn "Endpoint unreachable: $endpoint"
        fi
    done
}

run_safe_tests() {
    echo ""
    echo "=========================================="
    log_safe "RUNNING SAFE TESTS"
    echo "=========================================="
    echo ""

    test_safe_log_patterns
    echo ""
    test_safe_cpu_normal
    echo ""
    test_safe_memory_normal
    echo ""
    test_safe_disk_normal
    echo ""
    test_safe_gateway
    echo ""
    test_safe_connectivity

    echo ""
    log_success "All safe tests completed!"
}

# =============================================================================
# CAUTION TESTS - May generate alerts, minimal system impact
# =============================================================================

test_caution_cpu_stress() {
    log_caution "Testing CPU Monitor (high load)"

    if ! check_stress_ng; then
        return 1
    fi

    log_warn "This will stress CPU for ${STRESS_DURATION}s"
    read -p "Continue? (y/N): " confirm
    [ "$confirm" != "y" ] && return 0

    log_info "Starting CPU stress test..."
    stress-ng --cpu "$(nproc)" --timeout "${STRESS_DURATION}s" &
    local pid=$!

    # Monitor for events
    sleep 5
    log_info "CPU usage during stress:"
    top -bn1 | head -10

    wait $pid 2>/dev/null || true
    log_success "CPU stress test complete"
    show_recent_events
}

test_caution_memory_stress() {
    log_caution "Testing Memory Monitor (elevated usage)"

    if ! check_stress_ng; then
        return 1
    fi

    log_warn "This will consume ~70% memory for ${STRESS_DURATION}s"
    read -p "Continue? (y/N): " confirm
    [ "$confirm" != "y" ] && return 0

    log_info "Starting memory stress test (70%)..."
    stress-ng --vm 1 --vm-bytes 70% --timeout "${STRESS_DURATION}s" &
    local pid=$!

    sleep 5
    log_info "Memory usage during stress:"
    free -h

    wait $pid 2>/dev/null || true
    log_success "Memory stress test complete"
    show_recent_events
}

test_caution_disk_usage() {
    log_caution "Testing Disk Monitor (elevated usage)"

    log_warn "This will create a 5GB test file"
    read -p "Continue? (y/N): " confirm
    [ "$confirm" != "y" ] && return 0

    log_info "Creating 5GB test file..."
    fallocate -l 5G /tmp/testfile-disk-test 2>/dev/null || \
        dd if=/dev/zero of=/tmp/testfile-disk-test bs=1G count=5 2>/dev/null

    log_info "Current disk usage:"
    df -h /tmp

    sleep 10
    show_recent_events

    log_info "Cleaning up test file..."
    rm -f /tmp/testfile-disk-test
    log_success "Disk test complete"
}

test_caution_gateway_latency() {
    log_caution "Testing Gateway Monitor (high latency)"

    log_warn "This will add 200ms latency to network interface"
    read -p "Continue? (y/N): " confirm
    [ "$confirm" != "y" ] && return 0

    local iface
    iface=$(ip route | grep default | awk '{print $5}' | head -1)

    if [ -z "$iface" ]; then
        log_error "Could not determine network interface"
        return 1
    fi

    log_info "Adding 200ms latency to $iface..."
    tc qdisc add dev "$iface" root netem delay 200ms

    log_info "Testing gateway with latency..."
    local gateway
    gateway=$(ip route | grep default | awk '{print $3}' | head -1)
    ping -c 5 "$gateway"

    sleep 15
    show_recent_events

    log_info "Removing latency..."
    tc qdisc del dev "$iface" root 2>/dev/null || true
    log_success "Gateway latency test complete"
}

run_caution_tests() {
    echo ""
    echo "=========================================="
    log_caution "RUNNING CAUTION TESTS"
    echo "=========================================="
    log_warn "These tests may generate alerts and use system resources"
    read -p "Continue with caution tests? (y/N): " confirm
    [ "$confirm" != "y" ] && return 0
    echo ""

    test_caution_cpu_stress
    echo ""
    test_caution_memory_stress
    echo ""
    test_caution_disk_usage
    echo ""
    test_caution_gateway_latency

    echo ""
    log_success "All caution tests completed!"
}

# =============================================================================
# DESTRUCTIVE TESTS - Can break node/cluster, dedicated test node only!
# =============================================================================

confirm_destructive() {
    echo ""
    log_destructive "=== DESTRUCTIVE TEST WARNING ==="
    echo ""
    log_error "The following tests CAN BREAK your node and may affect the cluster!"
    log_error "Only run on a DEDICATED TEST NODE that has been:"
    log_error "  1. Cordoned: kubectl cordon $NODE_NAME"
    log_error "  2. Drained: kubectl drain $NODE_NAME --ignore-daemonsets --delete-emptydir-data"
    echo ""
    log_warn "Tests that will be run:"
    log_warn "  - Stop/start kubelet (node goes NotReady)"
    log_warn "  - Block API server connectivity"
    log_warn "  - Block DNS resolution"
    log_warn "  - High memory stress (may trigger OOM)"
    echo ""
    read -p "Type 'I UNDERSTAND THE RISKS' to continue: " confirm
    [ "$confirm" != "I UNDERSTAND THE RISKS" ] && return 1
    return 0
}

test_destructive_kubelet_stop() {
    log_destructive "Testing Kubelet Monitor (stop/start)"

    log_error "This will STOP kubelet - node will go NotReady!"
    read -p "Continue? (y/N): " confirm
    [ "$confirm" != "y" ] && return 0

    log_info "Stopping kubelet..."
    systemctl stop kubelet

    log_info "Waiting 30s for detection..."
    sleep 30

    log_info "Node status:"
    kubectl get node "$NODE_NAME" 2>/dev/null || log_warn "Cannot reach API server"

    show_recent_events

    log_info "Restarting kubelet..."
    systemctl start kubelet

    log_info "Waiting for kubelet to recover..."
    sleep 30

    log_info "Node status after recovery:"
    kubectl get node "$NODE_NAME" 2>/dev/null || log_warn "Cannot reach API server"

    log_success "Kubelet stop/start test complete"
}

test_destructive_api_block() {
    log_destructive "Testing API Server Monitor (block connectivity)"

    log_error "This will block API server connectivity!"
    read -p "Continue? (y/N): " confirm
    [ "$confirm" != "y" ] && return 0

    log_info "Blocking API server port 6443..."
    iptables -A OUTPUT -p tcp --dport 6443 -j DROP

    log_info "Waiting 30s for detection..."
    sleep 30

    log_info "Checking kubelet logs for API errors..."
    journalctl -u kubelet --no-pager -n 20 | grep -i "error\|failed" || true

    log_info "Restoring API server connectivity..."
    iptables -D OUTPUT -p tcp --dport 6443 -j DROP 2>/dev/null || true

    sleep 10
    show_recent_events

    log_success "API block test complete"
}

test_destructive_dns_block() {
    log_destructive "Testing DNS Monitor (block DNS)"

    log_error "This will block ALL DNS resolution!"
    read -p "Continue? (y/N): " confirm
    [ "$confirm" != "y" ] && return 0

    log_info "Blocking DNS port 53..."
    iptables -A OUTPUT -p udp --dport 53 -j DROP
    iptables -A OUTPUT -p tcp --dport 53 -j DROP

    log_info "Testing DNS resolution (should fail)..."
    nslookup google.com 2>&1 | head -5 || log_success "DNS blocked as expected"

    log_info "Waiting 30s for detection..."
    sleep 30

    log_info "Restoring DNS connectivity..."
    iptables -D OUTPUT -p udp --dport 53 -j DROP 2>/dev/null || true
    iptables -D OUTPUT -p tcp --dport 53 -j DROP 2>/dev/null || true

    log_info "Testing DNS resolution (should work)..."
    nslookup google.com 2>&1 | head -5 || log_warn "DNS still not working"

    log_success "DNS block test complete"
}

test_destructive_oom() {
    log_destructive "Testing Memory Monitor (OOM trigger)"

    if ! check_stress_ng; then
        return 1
    fi

    log_error "This will consume 95% memory and may trigger OOM killer!"
    log_error "Some processes may be killed!"
    read -p "Continue? (y/N): " confirm
    [ "$confirm" != "y" ] && return 0

    log_info "Starting extreme memory stress (95%)..."
    stress-ng --vm 1 --vm-bytes 95% --timeout 30s &
    local pid=$!

    log_info "Monitoring for OOM events..."
    sleep 10
    dmesg | tail -20 | grep -i "oom\|killed" || log_info "No OOM detected yet"

    wait $pid 2>/dev/null || log_warn "stress-ng may have been killed by OOM"

    log_info "Checking dmesg for OOM events..."
    dmesg | tail -30 | grep -i "oom\|killed" || true

    show_recent_events
    log_success "OOM test complete"
}

run_destructive_tests() {
    echo ""
    echo "=========================================="
    log_destructive "RUNNING DESTRUCTIVE TESTS"
    echo "=========================================="

    if ! confirm_destructive; then
        log_info "Destructive tests cancelled"
        return 0
    fi

    echo ""
    test_destructive_api_block
    echo ""
    test_destructive_dns_block
    echo ""
    test_destructive_oom
    echo ""
    test_destructive_kubelet_stop

    echo ""
    log_success "All destructive tests completed!"
    log_warn "Remember to uncordon your node: kubectl uncordon $NODE_NAME"
}

# =============================================================================
# PATTERN INJECTION TESTS - All 58 log patterns
# =============================================================================

test_all_patterns() {
    check_root

    echo ""
    echo "=========================================="
    log_safe "TESTING ALL 58 LOG PATTERNS"
    echo "=========================================="
    echo ""

    # System patterns
    log_info "=== System Patterns ==="
    inject_kmsg "Out of memory: Killed process 1234 (test)"
    inject_kmsg "Buffer I/O error on device sda1, logical block 0"
    inject_kmsg "NETDEV WATCHDOG: eth0: transmit queue 0 timed out"
    inject_kmsg "kernel panic - not syncing: Fatal exception"
    inject_kmsg "bad page state in process stress"
    inject_kmsg "device descriptor read error"
    inject_kmsg "EXT4-fs error: remounted read-only"
    echo ""

    # Kubelet patterns
    log_info "=== Kubelet Patterns ==="
    logger -t kubelet "memory cgroup out of memory"
    logger -t kubelet "PLEG is not healthy"
    logger -t kubelet "eviction manager: evicting pod"
    logger -t kubelet "DiskPressure condition true"
    logger -t kubelet "MemoryPressure detected"
    logger -t kubelet "Failed to pull image: ErrImagePull"
    logger -t kubelet "CNI network plugin error"
    logger -t kubelet "container runtime error"
    logger -t kubelet "certificate rotation failed"
    logger -t kubelet "Node status: NotReady"
    logger -t kubelet "failed to contact API server"
    logger -t kubelet "MountVolume.SetUp failed"
    echo ""

    # Container runtime patterns
    log_info "=== Container Runtime Patterns ==="
    logger -t containerd "containerd failed to start"
    logger -t dockerd "dockerd error: connection refused"
    echo ""

    # Hardware patterns
    log_info "=== Hardware Patterns ==="
    inject_kmsg "CPU0: Package temperature/speed high"
    inject_kmsg "nfs: server not responding"
    inject_kmsg "NFS_STALE: stale file handle"
    inject_kmsg "numa balancing failed"
    inject_kmsg "mce: [Hardware Error] Machine check error"
    inject_kmsg "EDAC MC0: UE memory error"
    inject_kmsg "EDAC MC0: CE correctable error"
    echo ""

    # VMware patterns
    log_info "=== VMware Patterns ==="
    inject_kmsg "vmxnet3 0000:03:00.0: tx hang"
    inject_kmsg "vmxnet3 0000:03:00.0: resetting"
    inject_kmsg "BUG: soft lockup - CPU#0 stuck for 22s! [longhorn:1234]"
    echo ""

    # Networking patterns
    log_info "=== Networking Patterns ==="
    inject_kmsg "nf_conntrack: table full, dropping packet"
    inject_kmsg "nf_conntrack: dropping packet"
    inject_kmsg "nf_tables error: rule insertion failed"
    inject_kmsg "iptables error: invalid argument"
    logger -t kube-proxy "iptables sync failed"
    inject_kmsg "e1000e 0000:00:1f.6 eth0: NIC Link is Down"
    inject_kmsg "igb 0000:01:00.0: error reading PHY"
    inject_kmsg "NETDEV WATCHDOG: eth0: transmit queue timed out"
    inject_kmsg "Unable to load firmware for device"
    inject_kmsg "carrier lost on eth0"
    inject_kmsg "socket buffer overrun detected"
    inject_kmsg "TCP retransmit timeout exceeded"
    inject_kmsg "ARP resolution failed for 10.0.0.1"
    inject_kmsg "RTNETLINK error: No route to host"
    echo ""

    # CNI patterns
    log_info "=== CNI Patterns ==="
    logger -t calico "error: connection failed"
    logger -t flannel "error: network setup failed"
    logger -t cilium-agent "error: BPF load failed"
    logger -t cni "CNI plugin failed"
    logger -t kube-proxy "ipvs sync failed"
    logger -t kube-proxy "kube-proxy error: endpoint update failed"
    logger -t kube-proxy "endpoint syncing failed"
    echo ""

    # Pod networking patterns
    log_info "=== Pod Networking Patterns ==="
    inject_kmsg "veth: cannot create virtual ethernet device"
    inject_kmsg "network namespace error: failed to create"
    logger -t kubelet "failed to setup network for pod"
    echo ""

    # Cloud provider patterns
    log_info "=== Cloud Provider Patterns ==="
    logger -t vpc-cni "ENI allocation error"
    logger -t azure-cni "network configuration failed"
    logger -t nsx-agent "nsx error: timeout"
    echo ""

    log_success "All 58 patterns injected!"
    echo ""
    log_info "Waiting 10s for processing..."
    sleep 10

    show_recent_events
}

# =============================================================================
# MAIN
# =============================================================================

show_help() {
    cat << 'EOF'
Node Doctor Comprehensive Monitor Test Script

Usage: sudo ./test-monitors.sh [OPTION]

Options:
  --safe        Run safe tests only (read-only, no system impact)
  --caution     Run caution tests (may generate alerts, minimal impact)
  --destructive Run destructive tests (CAN BREAK NODE - test node only!)
  --patterns    Inject all 58 log patterns for detection testing
  --all         Run all tests (safe + caution + destructive)
  --cleanup     Run cleanup only (stop stress, flush iptables/tc)
  --help        Show this help

Environment Variables:
  NAMESPACE       node-doctor namespace (default: node-doctor)
  NODE_NAME       Target node name (default: hostname)
  STRESS_DURATION Stress test duration in seconds (default: 30)
  TEST_TIMEOUT    Timeout for event detection (default: 60)

Risk Levels:
  🟢 SAFE        - Read-only tests, no system impact
  🟡 CAUTION     - May generate alerts, uses resources temporarily
  🔴 DESTRUCTIVE - Can break node/cluster, requires dedicated test node

Examples:
  # Run safe tests (recommended first)
  sudo ./test-monitors.sh --safe

  # Test log pattern detection only
  sudo ./test-monitors.sh --patterns

  # Run on dedicated test node (after cordon/drain)
  kubectl cordon test-node
  kubectl drain test-node --ignore-daemonsets --delete-emptydir-data
  sudo ./test-monitors.sh --all
  kubectl uncordon test-node

Verification:
  # Watch node-doctor logs
  kubectl logs -f -l app=node-doctor -n node-doctor

  # Watch Kubernetes events
  kubectl get events -A --watch

  # Check node conditions
  kubectl describe node $(hostname) | grep -A20 Conditions

EOF
}

main() {
    case "${1:-}" in
        --safe)
            check_root
            run_safe_tests
            ;;
        --caution)
            check_root
            run_caution_tests
            ;;
        --destructive)
            check_root
            run_destructive_tests
            ;;
        --patterns)
            test_all_patterns
            ;;
        --all)
            check_root
            run_safe_tests
            echo ""
            run_caution_tests
            echo ""
            run_destructive_tests
            ;;
        --cleanup)
            cleanup_all
            ;;
        --help|-h|"")
            show_help
            ;;
        *)
            log_error "Unknown option: $1"
            show_help
            exit 1
            ;;
    esac
}

main "$@"
