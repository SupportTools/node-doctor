#!/bin/bash
# Node Doctor Post-Deployment Smoke Test
# Run this script after deploying Node Doctor to verify everything is working correctly.
#
# Usage: ./smoke-test.sh [namespace] [prometheus-namespace]
#   namespace:            Node Doctor namespace (default: node-doctor)
#   prometheus-namespace: Prometheus namespace (default: monitoring)
#
# Exit codes:
#   0 - All tests passed
#   1 - One or more tests failed

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
NAMESPACE="${1:-node-doctor}"
PROMETHEUS_NS="${2:-monitoring}"
TIMEOUT=60

# Counters
PASSED=0
FAILED=0
WARNINGS=0

# Helper functions
print_header() {
    echo ""
    echo "=================================================="
    echo "$1"
    echo "=================================================="
}

print_pass() {
    echo -e "${GREEN}[PASS]${NC} $1"
    ((PASSED++))
}

print_fail() {
    echo -e "${RED}[FAIL]${NC} $1"
    ((FAILED++))
}

print_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
    ((WARNINGS++))
}

print_info() {
    echo -e "[INFO] $1"
}

# Check if kubectl is available
check_kubectl() {
    if ! command -v kubectl &> /dev/null; then
        echo "Error: kubectl not found in PATH"
        exit 1
    fi
}

# Test 1: Check DaemonSet is running
test_daemonset() {
    print_header "Test 1: DaemonSet Status"

    local desired=$(kubectl get daemonset node-doctor -n "$NAMESPACE" -o jsonpath='{.status.desiredNumberScheduled}' 2>/dev/null || echo "0")
    local ready=$(kubectl get daemonset node-doctor -n "$NAMESPACE" -o jsonpath='{.status.numberReady}' 2>/dev/null || echo "0")

    if [[ "$desired" == "0" ]]; then
        print_fail "DaemonSet not found in namespace $NAMESPACE"
        return
    fi

    print_info "Desired: $desired, Ready: $ready"

    if [[ "$ready" == "$desired" ]]; then
        print_pass "All $ready/$desired pods are ready"
    else
        print_fail "Only $ready/$desired pods are ready"
    fi
}

# Test 2: Check pods are running and not restarting
test_pods() {
    print_header "Test 2: Pod Health"

    local pods=$(kubectl get pods -n "$NAMESPACE" -l app=node-doctor -o jsonpath='{range .items[*]}{.metadata.name}{" "}{.status.phase}{" "}{range .status.containerStatuses[*]}{.restartCount}{end}{"\n"}{end}' 2>/dev/null)

    if [[ -z "$pods" ]]; then
        print_fail "No pods found"
        return
    fi

    local all_running=true
    local high_restarts=false

    while IFS= read -r line; do
        local pod_name=$(echo "$line" | awk '{print $1}')
        local phase=$(echo "$line" | awk '{print $2}')
        local restarts=$(echo "$line" | awk '{print $3}')

        if [[ "$phase" != "Running" ]]; then
            print_fail "Pod $pod_name is in $phase state"
            all_running=false
        fi

        if [[ "$restarts" -gt 5 ]]; then
            print_warn "Pod $pod_name has $restarts restarts"
            high_restarts=true
        fi
    done <<< "$pods"

    if $all_running; then
        print_pass "All pods are running"
    fi

    if ! $high_restarts; then
        print_pass "No pods have excessive restarts"
    fi
}

# Test 3: Check metrics endpoint
test_metrics_endpoint() {
    print_header "Test 3: Metrics Endpoint"

    # Get a running pod
    local pod=$(kubectl get pods -n "$NAMESPACE" -l app=node-doctor -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

    if [[ -z "$pod" ]]; then
        print_fail "No pods found to test metrics endpoint"
        return
    fi

    print_info "Testing metrics endpoint on pod $pod"

    # Port-forward and test metrics (background process)
    kubectl port-forward -n "$NAMESPACE" "pod/$pod" 9101:9101 &>/dev/null &
    local pf_pid=$!
    sleep 3

    # Test metrics endpoint
    local metrics=$(curl -s --max-time 10 http://localhost:9101/metrics 2>/dev/null || echo "")

    # Kill port-forward
    kill $pf_pid 2>/dev/null || true
    wait $pf_pid 2>/dev/null || true

    if [[ -z "$metrics" ]]; then
        print_fail "Could not retrieve metrics from pod"
        return
    fi

    # Check for key metrics
    if echo "$metrics" | grep -q "node_doctor_monitor_uptime_seconds"; then
        print_pass "Uptime metric present"
    else
        print_fail "Uptime metric missing"
    fi

    if echo "$metrics" | grep -q "node_doctor_monitor_conditions_total"; then
        print_pass "Conditions metric present"
    else
        print_fail "Conditions metric missing"
    fi

    # Check critical conditions
    local cni_valid=$(echo "$metrics" | grep 'condition_type="CNIConfigValid".*status="True"' || echo "")
    if [[ -n "$cni_valid" ]]; then
        print_pass "CNIConfigValid=True"
    else
        local cni_invalid=$(echo "$metrics" | grep 'condition_type="CNIConfigValid".*status="False"' || echo "")
        if [[ -n "$cni_invalid" ]]; then
            print_fail "CNIConfigValid=False - CNI configuration problem detected!"
        else
            print_warn "CNIConfigValid condition not found"
        fi
    fi

    local cni_healthy=$(echo "$metrics" | grep 'condition_type="CNIHealthy".*status="True"' || echo "")
    if [[ -n "$cni_healthy" ]]; then
        print_pass "CNIHealthy=True"
    else
        print_warn "CNIHealthy may not be True"
    fi
}

# Test 4: Check Service exists
test_service() {
    print_header "Test 4: Service Configuration"

    local svc=$(kubectl get svc node-doctor-metrics -n "$NAMESPACE" -o jsonpath='{.metadata.name}' 2>/dev/null || echo "")

    if [[ -z "$svc" ]]; then
        print_fail "Service node-doctor-metrics not found"
        return
    fi

    print_pass "Service node-doctor-metrics exists"

    # Check service labels
    local labels=$(kubectl get svc node-doctor-metrics -n "$NAMESPACE" -o jsonpath='{.metadata.labels.app}' 2>/dev/null || echo "")
    if [[ "$labels" == "node-doctor" ]]; then
        print_pass "Service has correct labels"
    else
        print_warn "Service may have incorrect labels"
    fi
}

# Test 5: Check ServiceMonitor exists (if Prometheus Operator is used)
test_servicemonitor() {
    print_header "Test 5: ServiceMonitor Configuration"

    # Check if ServiceMonitor CRD exists
    if ! kubectl api-resources | grep -q servicemonitors; then
        print_warn "ServiceMonitor CRD not found - skipping (Prometheus Operator may not be installed)"
        return
    fi

    # Check in both node-doctor and prometheus namespace
    local sm_nd=$(kubectl get servicemonitor node-doctor -n "$NAMESPACE" -o jsonpath='{.metadata.name}' 2>/dev/null || echo "")
    local sm_prom=$(kubectl get servicemonitor node-doctor -n "$PROMETHEUS_NS" -o jsonpath='{.metadata.name}' 2>/dev/null || echo "")

    if [[ -n "$sm_nd" ]]; then
        print_pass "ServiceMonitor found in $NAMESPACE namespace"
    elif [[ -n "$sm_prom" ]]; then
        print_pass "ServiceMonitor found in $PROMETHEUS_NS namespace"
    else
        print_fail "ServiceMonitor not found - Prometheus will not scrape Node Doctor metrics!"
        print_info "Deploy with: kubectl apply -f deployment/servicemonitor.yaml -n $PROMETHEUS_NS"
    fi
}

# Test 6: Check PrometheusRule exists (if Prometheus Operator is used)
test_prometheusrule() {
    print_header "Test 6: PrometheusRule Configuration"

    # Check if PrometheusRule CRD exists
    if ! kubectl api-resources | grep -q prometheusrules; then
        print_warn "PrometheusRule CRD not found - skipping"
        return
    fi

    local pr_nd=$(kubectl get prometheusrule node-doctor -n "$NAMESPACE" -o jsonpath='{.metadata.name}' 2>/dev/null || echo "")
    local pr_prom=$(kubectl get prometheusrule node-doctor -n "$PROMETHEUS_NS" -o jsonpath='{.metadata.name}' 2>/dev/null || echo "")

    if [[ -n "$pr_nd" ]] || [[ -n "$pr_prom" ]]; then
        print_pass "PrometheusRule found - alerts are configured"
    else
        print_warn "PrometheusRule not found - no alerts configured"
        print_info "Deploy with: kubectl apply -f deployment/prometheusrule.yaml -n $PROMETHEUS_NS"
    fi
}

# Test 7: Check if Prometheus is scraping Node Doctor
test_prometheus_scraping() {
    print_header "Test 7: Prometheus Scraping"

    # Find Prometheus service
    local prom_svc=$(kubectl get svc -n "$PROMETHEUS_NS" -l app.kubernetes.io/name=prometheus -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")

    if [[ -z "$prom_svc" ]]; then
        # Try alternative label
        prom_svc=$(kubectl get svc -n "$PROMETHEUS_NS" -l app=prometheus -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
    fi

    if [[ -z "$prom_svc" ]]; then
        print_warn "Prometheus service not found in $PROMETHEUS_NS namespace - skipping scrape test"
        return
    fi

    print_info "Testing Prometheus scraping via service $prom_svc"

    # Port-forward to Prometheus
    kubectl port-forward -n "$PROMETHEUS_NS" "svc/$prom_svc" 9090:9090 &>/dev/null &
    local pf_pid=$!
    sleep 3

    # Query for Node Doctor metrics
    local result=$(curl -s --max-time 10 "http://localhost:9090/api/v1/query?query=node_doctor_monitor_uptime_seconds" 2>/dev/null || echo "")

    # Kill port-forward
    kill $pf_pid 2>/dev/null || true
    wait $pf_pid 2>/dev/null || true

    if [[ -z "$result" ]]; then
        print_warn "Could not query Prometheus"
        return
    fi

    if echo "$result" | grep -q '"result":\[\]'; then
        print_fail "Prometheus has no Node Doctor metrics - ServiceMonitor may not be working"
    elif echo "$result" | grep -q '"result":\['; then
        local count=$(echo "$result" | grep -o '"__name__":"node_doctor' | wc -l)
        print_pass "Prometheus is scraping Node Doctor metrics ($count time series found)"
    else
        print_warn "Unexpected Prometheus response"
    fi
}

# Test 8: Check Node Conditions
test_node_conditions() {
    print_header "Test 8: Node Conditions"

    # Get a node with Node Doctor conditions
    local nodes=$(kubectl get nodes -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}')

    if [[ -z "$nodes" ]]; then
        print_fail "No nodes found"
        return
    fi

    local found_conditions=false
    while IFS= read -r node; do
        local conditions=$(kubectl get node "$node" -o jsonpath='{range .status.conditions[*]}{.type}{" "}{.status}{"\n"}{end}' | grep NodeDoctor || echo "")

        if [[ -n "$conditions" ]]; then
            found_conditions=true
            print_info "Conditions on $node:"
            echo "$conditions" | while read -r cond; do
                local type=$(echo "$cond" | awk '{print $1}')
                local status=$(echo "$cond" | awk '{print $2}')
                if [[ "$type" == *"Healthy"* && "$status" == "False" ]]; then
                    print_warn "  $type: $status"
                elif [[ "$type" == *"Pressure"* && "$status" == "True" ]]; then
                    print_warn "  $type: $status"
                else
                    print_info "  $type: $status"
                fi
            done
            break  # Only check first node with conditions
        fi
    done <<< "$nodes"

    if $found_conditions; then
        print_pass "Node Doctor conditions are being set on nodes"
    else
        print_warn "No NodeDoctor conditions found on any node"
    fi
}

# Main execution
main() {
    echo ""
    echo "=============================================="
    echo "   Node Doctor Post-Deployment Smoke Test"
    echo "=============================================="
    echo ""
    echo "Namespace: $NAMESPACE"
    echo "Prometheus NS: $PROMETHEUS_NS"
    echo ""

    check_kubectl

    test_daemonset
    test_pods
    test_metrics_endpoint
    test_service
    test_servicemonitor
    test_prometheusrule
    test_prometheus_scraping
    test_node_conditions

    print_header "Summary"
    echo -e "${GREEN}Passed: $PASSED${NC}"
    echo -e "${RED}Failed: $FAILED${NC}"
    echo -e "${YELLOW}Warnings: $WARNINGS${NC}"
    echo ""

    if [[ $FAILED -gt 0 ]]; then
        echo -e "${RED}Some tests failed! Please review the output above.${NC}"
        exit 1
    elif [[ $WARNINGS -gt 0 ]]; then
        echo -e "${YELLOW}All tests passed but there are warnings to review.${NC}"
        exit 0
    else
        echo -e "${GREEN}All tests passed!${NC}"
        exit 0
    fi
}

main "$@"
