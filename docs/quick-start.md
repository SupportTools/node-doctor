# Quick Start Guide

Get Node Doctor deployed and monitoring your Kubernetes cluster in minutes.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Deploy via Helm Chart](#deploy-via-helm-chart)
- [Import Grafana Dashboards](#import-grafana-dashboards)
- [Configure the DaemonSet](#configure-the-daemonset)
- [What is Collected](#what-is-collected)
- [Verification](#verification)

---

## Prerequisites

Before deploying Node Doctor, ensure you have:

| Requirement | Minimum | Recommended |
|-------------|---------|-------------|
| Kubernetes | 1.20+ | 1.28+ |
| Helm | 3.x | 3.12+ |
| Node OS | Linux (kernel 4.15+) | Linux (kernel 5.4+) |
| kubectl | Configured with cluster access | |
| Prometheus | For metrics collection | kube-prometheus-stack |
| Grafana | For dashboards | 9.x+ |

---

## Deploy via Helm Chart

### Step 1: Add the Helm Repository

```bash
helm repo add supporttools https://charts.support.tools
helm repo update
```

### Step 2: Install Node Doctor

**Basic Installation:**

```bash
helm install node-doctor supporttools/node-doctor \
  --namespace node-doctor \
  --create-namespace
```

**Installation with Custom Values:**

```bash
# Create a custom values file
cat <<EOF > custom-values.yaml
settings:
  logLevel: info
  enableRemediation: true
  dryRunMode: false

monitors:
  cpu:
    enabled: true
    loadAverageThresholds:
      warning: 80
      critical: 95
  memory:
    enabled: true
    memoryThresholds:
      warning: 85
      critical: 95
  disk:
    enabled: true
    paths:
      - path: "/"
        warningThreshold: 85
        criticalThreshold: 95

serviceMonitor:
  enabled: true  # Enable if using Prometheus Operator
EOF

# Install with custom values
helm install node-doctor supporttools/node-doctor \
  --namespace node-doctor \
  --create-namespace \
  -f custom-values.yaml
```

**Quick Overrides:**

```bash
# Disable remediation (monitoring only)
helm install node-doctor supporttools/node-doctor \
  --namespace node-doctor \
  --create-namespace \
  --set settings.enableRemediation=false

# Enable debug logging
helm install node-doctor supporttools/node-doctor \
  --namespace node-doctor \
  --create-namespace \
  --set settings.logLevel=debug

# Enable dry-run mode (log actions without executing)
helm install node-doctor supporttools/node-doctor \
  --namespace node-doctor \
  --create-namespace \
  --set settings.dryRunMode=true
```

### Step 3: Verify Deployment

```bash
# Check DaemonSet status
kubectl get daemonset -n node-doctor

# Verify pods are running on all nodes
kubectl get pods -n node-doctor -o wide

# Check pod logs
kubectl logs -n node-doctor -l app.kubernetes.io/name=node-doctor --tail=50

# Verify Node Doctor version
kubectl exec -n node-doctor -it $(kubectl get pod -n node-doctor -l app.kubernetes.io/name=node-doctor -o jsonpath='{.items[0].metadata.name}') -- /node-doctor --version
```

---

## Import Grafana Dashboards

Node Doctor provides 4 pre-built Grafana dashboards:

| Dashboard | UID | Description |
|-----------|-----|-------------|
| **Overview** | `node-doctor-overview` | Executive summary with drill-down links |
| **System Health** | `node-doctor-system` | CPU, Memory, Disk monitoring |
| **Kubernetes Health** | `node-doctor-k8s` | Kubelet, API Server, Node Doctor agent status |
| **CNI & Network** | `node-doctor-cni` | Network partition, CNI health, DNS resolution |

### Method A: Grafana UI Import (Quickest)

1. Download the dashboard JSON files from the [dashboards/](../dashboards/) directory
2. Open Grafana and navigate to **Dashboards > Import**
3. Click **Upload JSON file** or paste the JSON content
4. Select your Prometheus datasource
5. Click **Import**

### Method B: ConfigMap (kube-prometheus-stack / Rancher Monitoring)

For Grafana with sidecar provisioning (common in kube-prometheus-stack and Rancher Monitoring):

```bash
# Clone the repository (or download dashboards)
git clone https://github.com/supporttools/node-doctor.git
cd node-doctor

# Determine your dashboard namespace
# - Rancher Monitoring: cattle-dashboards
# - kube-prometheus-stack: monitoring (or your release namespace)
DASHBOARD_NS="cattle-dashboards"  # Adjust as needed

# Create ConfigMaps for all dashboards
kubectl create configmap node-doctor-dashboard-overview \
  --namespace=$DASHBOARD_NS \
  --from-file=node-doctor-overview.json=dashboards/node-doctor-overview.json

kubectl create configmap node-doctor-dashboard-system \
  --namespace=$DASHBOARD_NS \
  --from-file=node-doctor-system-health.json=dashboards/node-doctor-system-health.json

kubectl create configmap node-doctor-dashboard-k8s \
  --namespace=$DASHBOARD_NS \
  --from-file=node-doctor-kubernetes-health.json=dashboards/node-doctor-kubernetes-health.json

kubectl create configmap node-doctor-dashboard-cni \
  --namespace=$DASHBOARD_NS \
  --from-file=node-doctor-cni-network-health.json=dashboards/node-doctor-cni-network-health.json

# Add sidecar labels (required for auto-discovery)
kubectl label configmap node-doctor-dashboard-overview -n $DASHBOARD_NS grafana_dashboard=1
kubectl label configmap node-doctor-dashboard-system -n $DASHBOARD_NS grafana_dashboard=1
kubectl label configmap node-doctor-dashboard-k8s -n $DASHBOARD_NS grafana_dashboard=1
kubectl label configmap node-doctor-dashboard-cni -n $DASHBOARD_NS grafana_dashboard=1
```

### Method C: Grafana Provisioning

For standalone Grafana installations, copy dashboards to the provisioning directory:

```bash
# Copy dashboards to Grafana provisioning directory
sudo mkdir -p /var/lib/grafana/dashboards/node-doctor
sudo cp dashboards/*.json /var/lib/grafana/dashboards/node-doctor/

# Create provisioning configuration
sudo cat <<EOF > /etc/grafana/provisioning/dashboards/node-doctor.yaml
apiVersion: 1
providers:
  - name: 'node-doctor'
    folder: 'Node Doctor'
    type: file
    disableDeletion: false
    updateIntervalSeconds: 30
    options:
      path: /var/lib/grafana/dashboards/node-doctor
EOF

# Restart Grafana
sudo systemctl restart grafana-server
```

### Verify Dashboard Import

1. Open Grafana
2. Navigate to **Dashboards**
3. Search for "Node Doctor"
4. Open the **Node Doctor Overview** dashboard
5. Verify data is populating (may take 1-2 minutes)

---

## Configure the DaemonSet

### Key Configuration Areas

#### Global Settings

| Setting | Default | Description |
|---------|---------|-------------|
| `settings.logLevel` | `info` | Log verbosity: `debug`, `info`, `warn`, `error` |
| `settings.logFormat` | `json` | Log format: `json`, `text` |
| `settings.updateInterval` | `30s` | Monitor execution interval |
| `settings.enableRemediation` | `true` | Enable auto-remediation |
| `settings.dryRunMode` | `false` | Log actions without executing |

#### Resource Limits

```yaml
resources:
  requests:
    cpu: 50m
    memory: 128Mi
  limits:
    cpu: 200m
    memory: 256Mi
```

#### Monitor Configuration

Enable/disable and tune individual monitors:

```yaml
monitors:
  cpu:
    enabled: true
    interval: 30s
    loadAverageThresholds:
      warning: 80
      critical: 95

  memory:
    enabled: true
    memoryThresholds:
      warning: 85
      critical: 95
    oomDetection: true

  disk:
    enabled: true
    paths:
      - path: "/"
        warningThreshold: 85
        criticalThreshold: 95
      - path: "/var/lib/kubelet"
        warningThreshold: 85
        criticalThreshold: 95
```

#### Exporter Configuration

```yaml
exporters:
  kubernetes:
    enabled: true           # Update node conditions and events
    maxEventsPerMinute: 10

  prometheus:
    enabled: true           # Expose metrics on port 9101
    port: 9101
    path: /metrics

  http:
    enabled: false          # HTTP health endpoint (port 8080)
```

#### Remediation Configuration

```yaml
remediation:
  enabled: true
  dryRun: false
  maxRemediationsPerHour: 5
  maxRemediationsPerMinute: 1
  cooldownPeriod: 10m
  maxAttemptsGlobal: 2
```

#### Node Selection

Control which nodes run Node Doctor:

```yaml
# Run only on specific nodes
nodeSelector:
  kubernetes.io/os: linux
  node-type: worker

# Tolerations (default: runs on ALL nodes)
tolerations:
  - operator: Exists  # Tolerates all taints
```

### Example Configurations

**Minimal (Monitoring Only):**

```yaml
settings:
  logLevel: info
  enableRemediation: false

monitors:
  cpu:
    enabled: true
  memory:
    enabled: true
  disk:
    enabled: true

exporters:
  kubernetes:
    enabled: true
  prometheus:
    enabled: true
```

**Production with Remediation:**

```yaml
settings:
  logLevel: info
  enableRemediation: true
  dryRunMode: false

monitors:
  cpu:
    enabled: true
    loadAverageThresholds:
      warning: 75
      critical: 90
  memory:
    enabled: true
    memoryThresholds:
      warning: 80
      critical: 90
  disk:
    enabled: true
    paths:
      - path: "/"
        warningThreshold: 80
        criticalThreshold: 90
      - path: "/var/lib/kubelet"
        warningThreshold: 80
        criticalThreshold: 90
      - path: "/var/lib/containerd"
        warningThreshold: 75
        criticalThreshold: 85

remediation:
  enabled: true
  maxRemediationsPerHour: 10
  cooldownPeriod: 5m

serviceMonitor:
  enabled: true
```

**Debug/Testing:**

```yaml
settings:
  logLevel: debug
  enableRemediation: true
  dryRunMode: true  # Logs actions without executing

monitors:
  cpu:
    enabled: true
    interval: 10s  # Faster checks for testing
  memory:
    enabled: true
    interval: 10s
  disk:
    enabled: true
    interval: 10s
```

### Applying Configuration Changes

```bash
# Update Helm release with new values
helm upgrade node-doctor supporttools/node-doctor \
  --namespace node-doctor \
  -f custom-values.yaml

# Or override specific values
helm upgrade node-doctor supporttools/node-doctor \
  --namespace node-doctor \
  --set settings.logLevel=debug \
  --set remediation.dryRun=true
```

---

## What is Collected

Node Doctor monitors your nodes across four categories:

### System Monitors

| Monitor | Type ID | What It Checks |
|---------|---------|----------------|
| **CPU** | `system-cpu-check` | Load average, thermal throttling, CPU usage |
| **Memory** | `system-memory-check` | Memory usage, swap usage, OOM events |
| **Disk** | `system-disk-check` | Disk space, inode usage, I/O health, read-only filesystems |

**Conditions Generated:**
- `CPUHealthy`, `CPUPressure`, `ThermalPressure`
- `MemoryHealthy`, `MemoryPressure`, `SwapPressure`, `OOMKilled`
- `DiskHealthy`, `DiskPressure`, `InodePressure`, `ReadOnlyFilesystem`

### Network Monitors

| Monitor | What It Checks |
|---------|----------------|
| **DNS** | Internal/external DNS resolution, latency |
| **Gateway** | Default gateway reachability, latency |
| **Connectivity** | External endpoint connectivity |
| **CNI** | CNI plugin health, interface status, configuration validity |
| **Peer Discovery** | Node-to-node connectivity, network partitioning |

**Conditions Generated:**
- `DNSHealthy`, `DNSResolutionFailed`, `DNSLatencyHigh`
- `GatewayReachable`, `ConnectivityHealthy`
- `CNIHealthy`, `CNIConfigValid`, `CNIInterfacesHealthy`
- `NetworkPartitioned`, `NetworkDegraded`

### Kubernetes Monitors

| Monitor | What It Checks |
|---------|----------------|
| **Kubelet** | Health endpoint (`/healthz`), systemd service status, PLEG performance |
| **API Server** | Connectivity, authentication, latency |
| **Container Runtime** | Docker/containerd/CRI-O health |
| **Capacity** | Available pod slots |

**Conditions Generated:**
- `KubeletHealthy`, `KubeletServiceRunning`, `KubeletReady`
- `APIServerReachable`, `APIServerLatencyHigh`
- `ContainerRuntimeHealthy`
- `PodCapacityAvailable`

### Custom Monitors

| Monitor | What It Checks |
|---------|----------------|
| **Plugin** | Execute custom health check scripts |
| **Log Pattern** | Match problematic patterns in log files |

### Prometheus Metrics

Node Doctor exports the following metrics on port `9101`:

```promql
# Monitor information
node_doctor_monitor_info{node, version, go_version}

# Condition tracking
node_doctor_monitor_conditions_total{node, condition_type, status}

# Active problems
node_doctor_monitor_problems_active{node, problem_type, severity}

# Events
node_doctor_monitor_events_total{node, source, severity}

# Uptime
node_doctor_monitor_uptime_seconds{node}

# Remediation
node_doctor_remediations_attempted_total{problem, remediator, result}
node_doctor_remediations_succeeded_total{problem, remediator}
node_doctor_circuit_breaker_state{problem}
```

---

## Verification

### Check Node Doctor Status

```bash
# DaemonSet health
kubectl get daemonset -n node-doctor
# Expected: DESIRED = CURRENT = READY = number of nodes

# Pod status
kubectl get pods -n node-doctor -o wide
# Expected: All pods Running, one per node

# Logs (no errors)
kubectl logs -n node-doctor -l app.kubernetes.io/name=node-doctor --tail=20
```

### Check Node Conditions

```bash
# View Node Doctor conditions on a node
kubectl get node <node-name> -o jsonpath='{.status.conditions[*]}' | jq '.[] | select(.type | startswith("NodeDoctor") or test("Pressure|Healthy"))'

# Quick health check across all nodes
kubectl get nodes -o custom-columns='NAME:.metadata.name,HEALTHY:.status.conditions[?(@.type=="NodeDoctorHealthy")].status'
```

### Check Metrics

```bash
# Port-forward to a Node Doctor pod
kubectl port-forward -n node-doctor $(kubectl get pod -n node-doctor -l app.kubernetes.io/name=node-doctor -o jsonpath='{.items[0].metadata.name}') 9101:9101

# Query metrics
curl -s http://localhost:9101/metrics | grep node_doctor
```

### Check HTTP Endpoints

```bash
# From within the cluster (or node)
curl http://localhost:8080/health    # Overall health (200=healthy, 503=unhealthy)
curl http://localhost:8080/ready     # Readiness
curl http://localhost:8080/status    # Detailed monitor status (JSON)
```

### Troubleshooting

If Node Doctor isn't working as expected:

1. **Check pod logs for errors:**
   ```bash
   kubectl logs -n node-doctor -l app.kubernetes.io/name=node-doctor --tail=100 | grep -i error
   ```

2. **Verify RBAC permissions:**
   ```bash
   kubectl auth can-i --as=system:serviceaccount:node-doctor:node-doctor patch nodes
   ```

3. **Check ServiceMonitor (if using Prometheus Operator):**
   ```bash
   kubectl get servicemonitor -n node-doctor
   ```

4. **Verify metrics are being scraped:**
   - Open Prometheus UI
   - Navigate to Status > Targets
   - Look for `node-doctor` targets

For more detailed troubleshooting, see the [Troubleshooting Guide](troubleshooting.md).

---

## Next Steps

- [Configuration Reference](configuration.md) - Complete configuration options
- [Monitors Documentation](monitors.md) - Detailed monitor documentation
- [Remediation Guide](remediation.md) - Auto-remediation configuration
- [Architecture](architecture.md) - System design and architecture

---

**Last Updated**: 2025-01-21
