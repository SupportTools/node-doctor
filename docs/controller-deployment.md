# Node Doctor Controller Deployment Guide

This guide covers deploying the Node Doctor Controller, which provides cluster-wide health aggregation, pattern correlation, and remediation coordination.

## Overview

The Node Doctor Controller is an optional central component that:

- **Aggregates** health reports from all node-doctor agents across the cluster
- **Correlates** patterns to detect cluster-wide issues (e.g., 30% of nodes have DNS problems)
- **Coordinates** remediation actions to prevent remediation storms
- **Exposes** cluster-level metrics and a REST API for observability

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│  Node 1 (DaemonSet)          Node 2              Node N         │
│  ┌──────────────────┐   ┌──────────────┐   ┌──────────────┐    │
│  │ Monitors         │   │ Monitors     │   │ Monitors     │    │
│  │ ↓                │   │ ↓            │   │ ↓            │    │
│  │ HTTP Exporter    │   │ HTTP Export  │   │ HTTP Export  │    │
│  │ (webhook push)   │   │ (webhook)    │   │ (webhook)    │    │
│  └────────┬─────────┘   └──────┬───────┘   └──────┬───────┘    │
│           │                    │                   │            │
│           │    POST /api/v1/reports (every 30s)   │            │
│           └────────────────────┼──────────────────┘            │
│                                ↓                               │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │           Node Doctor Controller (Deployment)           │   │
│  │                                                         │   │
│  │  ┌─────────────┐   ┌─────────────┐   ┌───────────────┐ │   │
│  │  │ Aggregator  │ → │ Correlator  │ → │ Lease Manager │ │   │
│  │  └─────────────┘   └─────────────┘   └───────────────┘ │   │
│  │         ↓                                               │   │
│  │  ┌─────────────────────────────────────────────────┐   │   │
│  │  │           SQLite Storage (PVC)                  │   │   │
│  │  └─────────────────────────────────────────────────┘   │   │
│  │         ↓                 ↓                  ↓          │   │
│  │  ┌───────────┐   ┌───────────┐   ┌─────────────────┐   │   │
│  │  │ REST API  │   │ Prometheus│   │ K8s Events      │   │   │
│  │  │ /api/v1/* │   │ /metrics  │   │ (cluster-level) │   │   │
│  │  └───────────┘   └───────────┘   └─────────────────┘   │   │
│  └─────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

## Prerequisites

- Kubernetes cluster 1.19+
- Persistent storage class for SQLite database (1Gi minimum)
- Cluster admin privileges for RBAC resources
- Node Doctor DaemonSet already deployed (or will be deployed)

## Deployment Options

| Method | Recommended For | Description |
|--------|----------------|-------------|
| **Kustomize** | Production | Apply manifests with optional overlays |
| **Helm** | Production | Coming soon - configurable Helm chart |
| **Manual** | Development | Step-by-step manifest application |

---

## Option 1: Kustomize Deployment (Recommended)

### Deploy All Resources

```bash
kubectl apply -k deploy/controller/
```

This deploys:
- Namespace (if not exists)
- ServiceAccount
- ClusterRole and ClusterRoleBinding
- PersistentVolumeClaim (1Gi)
- ConfigMap with controller configuration
- Deployment (single replica)
- Service (ClusterIP on port 8080)

### Verify Deployment

```bash
# Check all resources
kubectl get all -n node-doctor -l app.kubernetes.io/component=controller

# Check deployment status
kubectl rollout status deployment/node-doctor-controller -n node-doctor

# Verify pod is running
kubectl get pods -n node-doctor -l app.kubernetes.io/name=node-doctor-controller
```

---

## Option 2: Manual Deployment

### Step 1: Create Namespace

```bash
kubectl apply -f deploy/controller/namespace.yaml
```

### Step 2: Apply RBAC Resources

```bash
kubectl apply -f deploy/controller/rbac.yaml

# Verify
kubectl get serviceaccount -n node-doctor node-doctor-controller
kubectl get clusterrole node-doctor-controller
kubectl get clusterrolebinding node-doctor-controller
```

### Step 3: Create Persistent Storage

```bash
# Edit storageClassName if needed for your cluster
kubectl apply -f deploy/controller/pvc.yaml

# Wait for PVC to be bound
kubectl get pvc -n node-doctor node-doctor-controller-data -w
```

### Step 4: Deploy ConfigMap

```bash
kubectl apply -f deploy/controller/configmap.yaml

# Review configuration
kubectl get configmap -n node-doctor node-doctor-controller-config -o yaml
```

### Step 5: Deploy Controller and Service

```bash
kubectl apply -f deploy/controller/deployment.yaml
kubectl apply -f deploy/controller/service.yaml

# Watch rollout
kubectl rollout status deployment/node-doctor-controller -n node-doctor
```

---

## Configuration

The controller is configured via the ConfigMap `node-doctor-controller-config`:

```yaml
# Server settings
server:
  bindAddress: "0.0.0.0"
  port: 8080
  readTimeout: 30s
  writeTimeout: 30s
  enableCORS: false

# SQLite storage
storage:
  path: /data/node-doctor.db
  retention: 720h  # 30 days

# Correlation engine
correlation:
  enabled: true
  clusterWideThreshold: 0.3  # 30% of nodes = cluster issue
  evaluationInterval: 30s
  minNodesForCorrelation: 2

# Remediation coordination
coordination:
  enabled: true
  maxConcurrentRemediations: 3
  defaultLeaseDuration: 5m
  cooldownPeriod: 10m

# Prometheus metrics
prometheus:
  enabled: true
  port: 9090
  path: /metrics

# Kubernetes integration
kubernetes:
  enabled: true
  inCluster: true
  namespace: node-doctor
  createEvents: true
```

### Key Configuration Options

| Setting | Default | Description |
|---------|---------|-------------|
| `storage.retention` | 720h (30 days) | How long to retain node reports |
| `correlation.clusterWideThreshold` | 0.3 | Fraction of nodes with same problem to trigger cluster correlation |
| `coordination.maxConcurrentRemediations` | 3 | Max simultaneous remediations cluster-wide |
| `coordination.defaultLeaseDuration` | 5m | Default lease duration for remediation |
| `coordination.cooldownPeriod` | 10m | Time between repeated remediations on same node |

### Updating Configuration

```bash
# Edit the ConfigMap
kubectl edit configmap -n node-doctor node-doctor-controller-config

# Restart controller to pick up changes
kubectl rollout restart deployment/node-doctor-controller -n node-doctor
```

---

## Configuring Node-Doctor DaemonSet

To send reports to the controller, configure the HTTP exporter in your DaemonSet ConfigMap:

```yaml
exporters:
  http:
    webhooks:
      - name: controller
        url: "http://node-doctor-controller.node-doctor:8080/api/v1/reports"
        interval: 30s
        timeout: 10s
```

### Enable Remediation Coordination

For coordinated remediations, configure the lease client:

```yaml
remediation:
  coordination:
    enabled: true
    controllerURL: "http://node-doctor-controller.node-doctor:8080"
    leaseTimeout: 5m
    fallbackOnUnreachable: false  # Block if controller down

  actions:
    restart-kubelet:
      requiresApproval: true    # Must get lease from controller
    flush-dns-cache:
      requiresApproval: false   # Can proceed without lease
```

---

## API Endpoints

### Health Checks

| Endpoint | Description |
|----------|-------------|
| `GET /healthz` | Liveness probe |
| `GET /readyz` | Readiness probe |

### Report Ingestion

| Endpoint | Description |
|----------|-------------|
| `POST /api/v1/reports` | Receive node health report |

### Cluster Status

| Endpoint | Description |
|----------|-------------|
| `GET /api/v1/cluster/status` | Overall cluster health summary |
| `GET /api/v1/cluster/problems` | Active cluster-wide problems |
| `GET /api/v1/nodes` | List all nodes with status |
| `GET /api/v1/nodes/{name}` | Single node details |
| `GET /api/v1/nodes/{name}/history` | Historical reports for a node |

### Correlation

| Endpoint | Description |
|----------|-------------|
| `GET /api/v1/correlations` | Active correlations |
| `GET /api/v1/correlations/{id}` | Correlation details |

### Remediation Coordination

| Endpoint | Description |
|----------|-------------|
| `POST /api/v1/leases` | Request remediation lease |
| `GET /api/v1/leases` | List active leases |
| `DELETE /api/v1/leases/{id}` | Release lease early |

### Metrics

| Endpoint | Description |
|----------|-------------|
| `GET /metrics` | Prometheus metrics |

---

## Prometheus Integration

The controller exposes cluster-level metrics at `/metrics`:

```
# Cluster-level node counts
node_doctor_cluster_nodes_total
node_doctor_cluster_nodes_healthy
node_doctor_cluster_nodes_unhealthy
node_doctor_cluster_nodes_unknown

# Problem aggregation
node_doctor_cluster_problem_nodes{problem_type="dns", severity="warning"}
node_doctor_cluster_problem_active{problem_type="dns"}

# Correlation tracking
node_doctor_correlation_active_total
node_doctor_correlation_detected_total{type="infrastructure"}

# Remediation coordination
node_doctor_leases_active_total
node_doctor_leases_granted_total
node_doctor_leases_denied_total{reason="max_concurrent"}
node_doctor_remediation_coordinated_total{type="restart-kubelet"}
```

### Prometheus ServiceMonitor

Create a ServiceMonitor to scrape controller metrics:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: node-doctor-controller
  namespace: node-doctor
  labels:
    app.kubernetes.io/name: node-doctor-controller
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: node-doctor-controller
  endpoints:
    - port: http
      path: /metrics
      interval: 30s
```

### Alerting Rules

Example Prometheus alerting rules:

```yaml
groups:
  - name: node-doctor-controller
    rules:
      - alert: NodeDoctorClusterUnhealthyNodes
        expr: node_doctor_cluster_nodes_unhealthy > 0
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "{{ $value }} unhealthy nodes in cluster"

      - alert: NodeDoctorInfrastructureCorrelation
        expr: node_doctor_correlation_active_total{type="infrastructure"} > 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "Infrastructure-wide issue detected affecting multiple nodes"

      - alert: NodeDoctorControllerDown
        expr: up{job="node-doctor-controller"} == 0
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Node Doctor Controller is down"
```

---

## Verifying Deployment

### Check Controller Health

```bash
# Test health endpoints
kubectl exec -n node-doctor \
  $(kubectl get pods -n node-doctor -l app.kubernetes.io/name=node-doctor-controller -o jsonpath='{.items[0].metadata.name}') \
  -- curl -s localhost:8080/healthz

# Check readiness
kubectl exec -n node-doctor \
  $(kubectl get pods -n node-doctor -l app.kubernetes.io/name=node-doctor-controller -o jsonpath='{.items[0].metadata.name}') \
  -- curl -s localhost:8080/readyz
```

### Check Cluster Status

```bash
# Port-forward to controller
kubectl port-forward -n node-doctor svc/node-doctor-controller 8080:8080 &

# Get cluster status
curl -s http://localhost:8080/api/v1/cluster/status | jq .

# List nodes
curl -s http://localhost:8080/api/v1/nodes | jq .

# Check active correlations
curl -s http://localhost:8080/api/v1/correlations | jq .

# View metrics
curl -s http://localhost:8080/metrics | grep node_doctor
```

### Check Logs

```bash
# View controller logs
kubectl logs -n node-doctor -l app.kubernetes.io/name=node-doctor-controller -f

# Check for correlation events
kubectl get events -n node-doctor --field-selector reason=InfrastructureCorrelation
```

---

## Troubleshooting

### Controller Not Starting

```bash
# Check pod status
kubectl describe pod -n node-doctor -l app.kubernetes.io/name=node-doctor-controller

# Common issues:
# - Pending: Check PVC is bound
# - CrashLoopBackOff: Check logs for errors
# - ImagePullBackOff: Verify image name and registry access
```

### PVC Not Binding

```bash
# Check PVC status
kubectl get pvc -n node-doctor node-doctor-controller-data

# Check storage class availability
kubectl get sc

# Edit PVC to specify storageClassName if needed
kubectl edit pvc -n node-doctor node-doctor-controller-data
```

### Nodes Not Reporting

```bash
# Verify DaemonSet webhook configuration
kubectl get configmap -n node-doctor node-doctor-config -o yaml | grep -A10 webhooks

# Check agent logs for webhook errors
kubectl logs -n node-doctor -l app=node-doctor --tail=100 | grep webhook

# Test connectivity from agent to controller
kubectl exec -n node-doctor \
  $(kubectl get pods -n node-doctor -l app=node-doctor -o jsonpath='{.items[0].metadata.name}') \
  -- curl -s http://node-doctor-controller.node-doctor:8080/healthz
```

### Correlations Not Detecting

```bash
# Check correlation config
kubectl get configmap -n node-doctor node-doctor-controller-config -o yaml | grep -A10 correlation

# Verify enough nodes reporting (minNodesForCorrelation)
curl -s http://localhost:8080/api/v1/nodes | jq 'length'

# Check correlation evaluation in logs
kubectl logs -n node-doctor -l app.kubernetes.io/name=node-doctor-controller | grep correlation
```

---

## Uninstalling

### Remove Controller Only

```bash
kubectl delete -k deploy/controller/
```

### Remove with Data

```bash
# Delete all resources including PVC
kubectl delete -k deploy/controller/
kubectl delete pvc -n node-doctor node-doctor-controller-data
```

---

## Production Considerations

1. **Storage Class**: Use a reliable storage class with backup support for the SQLite database
2. **Resource Limits**: Adjust CPU/memory based on cluster size (more nodes = more data)
3. **Retention**: Configure appropriate data retention based on storage capacity
4. **High Availability**: Single replica design - ensure PVC can reattach on node failure
5. **Network Policies**: Consider network policies if running in restricted environments
6. **Backup**: Regularly backup the SQLite database PVC
7. **Monitoring**: Set up alerts for controller health and correlation events

---

## Support

- GitHub: https://github.com/supporttools/node-doctor
- Documentation: See `/docs` directory
- Issues: Report at GitHub issues
