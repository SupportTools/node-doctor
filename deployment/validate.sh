#!/bin/bash
set -e

# Node Doctor DaemonSet Validation Script
echo "🏥 Node Doctor DaemonSet Validation"
echo "=================================="

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print status
print_status() {
    if [ $1 -eq 0 ]; then
        echo -e "${GREEN}✅ $2${NC}"
    else
        echo -e "${RED}❌ $2${NC}"
        return 1
    fi
}

# Function to print warning
print_warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

# Function to print info
print_info() {
    echo -e "${BLUE}ℹ️  $1${NC}"
}

# Check if kubectl is available
echo
print_info "Checking prerequisites..."
if ! command -v kubectl &> /dev/null; then
    echo -e "${RED}❌ kubectl is not installed or not in PATH${NC}"
    exit 1
fi
print_status 0 "kubectl is available"

# Check if we can connect to cluster
if ! kubectl cluster-info &> /dev/null; then
    echo -e "${RED}❌ Cannot connect to Kubernetes cluster${NC}"
    exit 1
fi
print_status 0 "Connected to Kubernetes cluster"

# Check if DaemonSet exists
echo
print_info "Checking DaemonSet deployment..."
if kubectl get daemonset -n kube-system node-doctor &> /dev/null; then
    print_status 0 "DaemonSet node-doctor exists in kube-system namespace"
else
    print_status 1 "DaemonSet node-doctor not found in kube-system namespace"
    echo "Run: kubectl apply -f deployment/daemonset.yaml"
    exit 1
fi

# Check DaemonSet status
DESIRED=$(kubectl get daemonset -n kube-system node-doctor -o jsonpath='{.status.desiredNumberScheduled}')
READY=$(kubectl get daemonset -n kube-system node-doctor -o jsonpath='{.status.numberReady}')
AVAILABLE=$(kubectl get daemonset -n kube-system node-doctor -o jsonpath='{.status.numberAvailable}')

echo "DaemonSet Status:"
echo "  Desired: $DESIRED"
echo "  Ready: $READY"
echo "  Available: $AVAILABLE"

if [ "$DESIRED" = "$READY" ] && [ "$DESIRED" = "$AVAILABLE" ]; then
    print_status 0 "All DaemonSet pods are ready and available"
else
    print_warning "Not all DaemonSet pods are ready (Desired: $DESIRED, Ready: $READY)"
fi

# Check individual pods
echo
print_info "Checking pod status..."
PODS=$(kubectl get pods -n kube-system -l app=node-doctor --no-headers | wc -l)
RUNNING_PODS=$(kubectl get pods -n kube-system -l app=node-doctor --no-headers | grep Running | wc -l)

echo "Pod Status:"
echo "  Total pods: $PODS"
echo "  Running pods: $RUNNING_PODS"

if [ "$PODS" = "$RUNNING_PODS" ] && [ "$PODS" -gt 0 ]; then
    print_status 0 "All Node Doctor pods are running"
else
    print_warning "Some Node Doctor pods are not running"
    echo "Pod details:"
    kubectl get pods -n kube-system -l app=node-doctor
fi

# Check ServiceAccount
echo
print_info "Checking RBAC configuration..."
if kubectl get serviceaccount -n kube-system node-doctor &> /dev/null; then
    print_status 0 "ServiceAccount node-doctor exists"
else
    print_status 1 "ServiceAccount node-doctor not found"
fi

if kubectl get clusterrole node-doctor &> /dev/null; then
    print_status 0 "ClusterRole node-doctor exists"
else
    print_status 1 "ClusterRole node-doctor not found"
fi

if kubectl get clusterrolebinding node-doctor &> /dev/null; then
    print_status 0 "ClusterRoleBinding node-doctor exists"
else
    print_status 1 "ClusterRoleBinding node-doctor not found"
fi

# Check ConfigMap
if kubectl get configmap -n kube-system node-doctor-config &> /dev/null; then
    print_status 0 "ConfigMap node-doctor-config exists"
else
    print_status 1 "ConfigMap node-doctor-config not found"
fi

# Check Service
if kubectl get service -n kube-system node-doctor-metrics &> /dev/null; then
    print_status 0 "Service node-doctor-metrics exists"
else
    print_status 1 "Service node-doctor-metrics not found"
fi

# Test health endpoint
echo
print_info "Testing health endpoints..."
POD_NAME=$(kubectl get pods -n kube-system -l app=node-doctor -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -n "$POD_NAME" ]; then
    print_info "Testing pod: $POD_NAME"

    # Test health endpoint
    if kubectl exec -n kube-system $POD_NAME -- curl -s -f localhost:8080/healthz > /dev/null 2>&1; then
        print_status 0 "Health endpoint (/healthz) is responding"
    else
        print_status 1 "Health endpoint (/healthz) is not responding"
    fi

    # Test readiness endpoint
    if kubectl exec -n kube-system $POD_NAME -- curl -s -f localhost:8080/ready > /dev/null 2>&1; then
        print_status 0 "Readiness endpoint (/ready) is responding"
    else
        print_status 1 "Readiness endpoint (/ready) is not responding"
    fi

    # Test metrics endpoint
    if kubectl exec -n kube-system $POD_NAME -- curl -s localhost:9100/metrics | grep -q "node_doctor" 2>/dev/null; then
        print_status 0 "Metrics endpoint (/metrics) is responding with Node Doctor metrics"
    else
        print_status 1 "Metrics endpoint (/metrics) is not responding or no Node Doctor metrics found"
    fi
else
    print_warning "No Node Doctor pods found for endpoint testing"
fi

# Check node conditions
echo
print_info "Checking node conditions..."
NODES_WITH_CONDITION=$(kubectl get nodes -o json | jq -r '.items[] | select(.status.conditions[]?.type == "NodeDoctorHealthy") | .metadata.name' 2>/dev/null | wc -l)
TOTAL_NODES=$(kubectl get nodes --no-headers | wc -l)

echo "Node Conditions:"
echo "  Total nodes: $TOTAL_NODES"
echo "  Nodes with NodeDoctorHealthy condition: $NODES_WITH_CONDITION"

if [ "$NODES_WITH_CONDITION" -gt 0 ]; then
    print_status 0 "Node Doctor is setting node conditions"

    # Show condition details for first node
    FIRST_NODE=$(kubectl get nodes -o json | jq -r '.items[] | select(.status.conditions[]?.type == "NodeDoctorHealthy") | .metadata.name' 2>/dev/null | head -1)
    if [ -n "$FIRST_NODE" ]; then
        echo "Sample node condition from $FIRST_NODE:"
        kubectl get node $FIRST_NODE -o json | jq -r '.status.conditions[] | select(.type == "NodeDoctorHealthy") | "  Status: \(.status), Reason: \(.reason), Message: \(.message)"' 2>/dev/null || echo "  Unable to parse condition details"
    fi
else
    print_warning "No nodes have NodeDoctorHealthy condition yet (may take a few minutes after deployment)"
fi

# Check for recent events
echo
print_info "Checking recent events..."
RECENT_EVENTS=$(kubectl get events -n kube-system --field-selector involvedObject.name=node-doctor --sort-by='.lastTimestamp' 2>/dev/null | tail -5)
if [ -n "$RECENT_EVENTS" ]; then
    echo "Recent Node Doctor events:"
    echo "$RECENT_EVENTS"
else
    print_info "No recent Node Doctor events found"
fi

# Resource usage check
echo
print_info "Checking resource usage..."
echo "Node Doctor pod resource usage:"
kubectl top pods -n kube-system -l app=node-doctor 2>/dev/null || print_warning "Metrics server not available for resource usage"

# Final summary
echo
echo "🏥 Validation Summary"
echo "===================="

if [ "$PODS" -gt 0 ] && [ "$PODS" = "$RUNNING_PODS" ]; then
    print_status 0 "Node Doctor is deployed and running on $PODS nodes"
else
    print_status 1 "Node Doctor deployment has issues"
    echo
    echo "💡 Troubleshooting tips:"
    echo "1. Check pod logs: kubectl logs -n kube-system -l app=node-doctor"
    echo "2. Describe pods: kubectl describe pods -n kube-system -l app=node-doctor"
    echo "3. Check node taints: kubectl describe nodes | grep Taints"
    echo "4. Verify privileged containers are allowed in your cluster"
    exit 1
fi

echo
print_info "For more detailed monitoring:"
echo "  - Logs: kubectl logs -n kube-system -l app=node-doctor -f"
echo "  - Metrics: kubectl exec -n kube-system $POD_NAME -- curl -s localhost:9100/metrics"
echo "  - Status: kubectl exec -n kube-system $POD_NAME -- curl -s localhost:8080/status"

echo
echo -e "${GREEN}🎉 Node Doctor validation completed successfully!${NC}"