#!/bin/bash
# =============================================================================
# VMware vmxnet3 Detection Test Script
# =============================================================================
# Injects synthetic kernel messages to verify node-doctor pattern detection.
# Run this on a lab cluster node where node-doctor is deployed.
#
# Usage: sudo ./test-vmxnet3-detection.sh [--all|--tx-hang|--nic-reset|--soft-lockup]
# =============================================================================

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

# Check root
if [ "$EUID" -ne 0 ]; then
    log_error "This script must be run as root (requires /dev/kmsg access)"
    exit 1
fi

# Test functions
inject_vmxnet3_tx_hang() {
    log_info "Injecting vmxnet3 TX hang message..."
    echo "vmxnet3 0000:03:00.0 ens160: tx hang" > /dev/kmsg
    log_success "Injected: vmxnet3-tx-hang (severity: error)"
}

inject_vmxnet3_nic_reset() {
    log_info "Injecting vmxnet3 NIC reset message..."
    echo "vmxnet3 0000:03:00.0 ens160: resetting" > /dev/kmsg
    log_success "Injected: vmxnet3-nic-reset (severity: warning)"
}

inject_soft_lockup_storage() {
    log_info "Injecting soft lockup storage message..."
    echo "watchdog: BUG: soft lockup - CPU#2 stuck for 22s! [longhorn-instan:12345]" > /dev/kmsg
    log_success "Injected: soft-lockup-storage (severity: error)"
}

inject_cascade_sequence() {
    log_info "Simulating full cascade failure sequence..."
    echo ""
    log_warn "=== Simulating vmxnet3 TX Hang Cascade ==="
    echo ""

    # Stage 1: TX hang
    log_info "Stage 1/4: TX hang detected"
    echo "vmxnet3 0000:03:00.0 ens160: tx hang" > /dev/kmsg
    sleep 2

    # Stage 2: Network timeout (covered by existing pattern)
    log_info "Stage 2/4: Network timeout cascade"
    echo "NETDEV WATCHDOG: ens160 (vmxnet3): transmit queue 0 timed out" > /dev/kmsg
    sleep 2

    # Stage 3: NIC reset
    log_info "Stage 3/4: NIC reset initiated"
    echo "vmxnet3 0000:03:00.0 ens160: resetting" > /dev/kmsg
    sleep 2

    # Stage 4: Storage soft lockup
    log_info "Stage 4/4: Storage subsystem soft lockup"
    echo "watchdog: BUG: soft lockup - CPU#2 stuck for 22s! [longhorn-instan:12345]" > /dev/kmsg

    echo ""
    log_success "Cascade sequence complete - check node-doctor logs for detections"
}

show_help() {
    echo "VMware vmxnet3 Detection Test Script"
    echo ""
    echo "Usage: sudo $0 [option]"
    echo ""
    echo "Options:"
    echo "  --tx-hang      Inject vmxnet3 TX hang message"
    echo "  --nic-reset    Inject vmxnet3 NIC reset message"
    echo "  --soft-lockup  Inject soft lockup storage message"
    echo "  --cascade      Simulate full cascade failure sequence"
    echo "  --all          Inject all individual test messages"
    echo "  --help         Show this help"
    echo ""
    echo "After injection, verify detection with:"
    echo "  kubectl logs -l app=node-doctor -n node-doctor --tail=50"
    echo "  dmesg | tail -20"
}

# Main
case "${1:-}" in
    --tx-hang)
        inject_vmxnet3_tx_hang
        ;;
    --nic-reset)
        inject_vmxnet3_nic_reset
        ;;
    --soft-lockup)
        inject_soft_lockup_storage
        ;;
    --cascade)
        inject_cascade_sequence
        ;;
    --all)
        inject_vmxnet3_tx_hang
        echo ""
        inject_vmxnet3_nic_reset
        echo ""
        inject_soft_lockup_storage
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

echo ""
log_info "Verify with: dmesg | tail -10"
