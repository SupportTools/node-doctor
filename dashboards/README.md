# Node Doctor Grafana Dashboards

This directory contains Grafana dashboard JSON files for visualizing Node Doctor metrics.

## Available Dashboards

| Dashboard | File | UID | Description |
|-----------|------|-----|-------------|
| Overview | `node-doctor-overview.json` | `node-doctor-overview` | Executive summary with drill-down links to detailed dashboards |
| System Health | `node-doctor-system-health.json` | `node-doctor-system` | CPU, Memory, and Disk health monitoring |
| Kubernetes Health | `node-doctor-kubernetes-health.json` | `node-doctor-k8s` | Kubelet, API Server connectivity, and Node Doctor agent status |
| CNI & Network Health | `node-doctor-cni-network-health.json` | `node-doctor-cni` | Network partition detection, CNI health, DNS health, peer connectivity |

## Quick Start

### Deploy All Dashboards (Rancher Monitoring / kube-prometheus-stack)

```bash
# Create ConfigMaps for all dashboards
kubectl create configmap node-doctor-dashboard-overview \
  --namespace=cattle-dashboards \
  --from-file=node-doctor-overview.json=dashboards/node-doctor-overview.json

kubectl create configmap node-doctor-dashboard-system \
  --namespace=cattle-dashboards \
  --from-file=node-doctor-system-health.json=dashboards/node-doctor-system-health.json

kubectl create configmap node-doctor-dashboard-k8s \
  --namespace=cattle-dashboards \
  --from-file=node-doctor-kubernetes-health.json=dashboards/node-doctor-kubernetes-health.json

kubectl create configmap node-doctor-dashboard-cni \
  --namespace=cattle-dashboards \
  --from-file=node-doctor-cni-network-health.json=dashboards/node-doctor-cni-network-health.json

# Add sidecar labels to all ConfigMaps
kubectl label configmap node-doctor-dashboard-overview -n cattle-dashboards grafana_dashboard=1
kubectl label configmap node-doctor-dashboard-system -n cattle-dashboards grafana_dashboard=1
kubectl label configmap node-doctor-dashboard-k8s -n cattle-dashboards grafana_dashboard=1
kubectl label configmap node-doctor-dashboard-cni -n cattle-dashboards grafana_dashboard=1
```

For kube-prometheus-stack, use the appropriate namespace (often `monitoring` or the release namespace).

### Option 2: Grafana UI Import

1. Open Grafana
2. Navigate to Dashboards > Import
3. Upload the JSON file or paste its contents
4. Select your Prometheus datasource
5. Click Import

### Option 3: Grafana Provisioning

Copy dashboards to Grafana's provisioning directory:

```yaml
# /etc/grafana/provisioning/dashboards/node-doctor.yaml
apiVersion: 1
providers:
  - name: 'node-doctor'
    folder: 'Node Doctor'
    type: file
    options:
      path: /var/lib/grafana/dashboards/node-doctor
```

## Required Metrics

These dashboards require the following Prometheus metrics exported by Node Doctor:

| Metric | Type | Description |
|--------|------|-------------|
| `node_doctor_monitor_info` | Gauge | Node information (version, go_version, build_time) |
| `node_doctor_monitor_conditions_total` | Counter | Condition state changes by condition_type and status |
| `node_doctor_monitor_problems_active` | Gauge | Currently active problems by problem_type and severity |
| `node_doctor_monitor_events_total` | Counter | Events by source and severity |
| `node_doctor_monitor_status_updates_total` | Counter | Status update count by source |
| `node_doctor_monitor_uptime_seconds` | Gauge | Monitor uptime per node |

## Dashboard Variables

All dashboards support the following template variables:

- **datasource**: Prometheus datasource selector
- **node**: Multi-select node filter (defaults to all nodes)

## Dashboard Navigation

All dashboards include navigation links to related dashboards in the top-right corner, allowing quick switching between:
- Overview
- System Health
- Kubernetes Health
- CNI & Network Health

---

## Node Doctor Overview Dashboard

**UID:** `node-doctor-overview`

An executive summary dashboard providing a high-level view of all monitored nodes with drill-down links.

### Panels

1. **Cluster Overview Row**
   - Total Nodes (stat)
   - Healthy Nodes (stat)
   - Warning Nodes (stat)
   - Critical Nodes (stat)
   - Total Active Problems (stat)
   - Average Uptime (stat)

2. **Health by Category Row**
   - System Health Score (gauge)
   - Kubernetes Health Score (gauge)
   - Network Health Score (gauge)

3. **Problems Summary**
   - Active Problems by Severity (bar chart)
   - Recent Events (time series)

4. **Node Status Table**
   - Node name, status, uptime, active problems, last event

### Use Cases
- Quick health check across all nodes
- Identify which subsystem has issues
- Drill down to detailed dashboards

---

## Node Doctor System Health Dashboard

**UID:** `node-doctor-system`

Detailed monitoring of CPU, Memory, and Disk health across all nodes.

### Panels

1. **CPU Health Row**
   - CPUHealthy Nodes (stat) - Green/Red
   - CPU Pressure Nodes (stat)
   - Thermal Pressure Nodes (stat)
   - CPU Conditions by Node (table)

2. **Memory Health Row**
   - MemoryHealthy Nodes (stat)
   - Memory Pressure Nodes (stat)
   - Swap Pressure Nodes (stat)
   - OOM Events (stat)
   - Memory Conditions by Node (table)

3. **Disk Health Row**
   - DiskHealthy Nodes (stat)
   - Disk Pressure Nodes (stat)
   - Inode Pressure Nodes (stat)
   - Read-Only Filesystems (stat)
   - Disk Conditions by Node (table)

4. **System Health Trends**
   - System Events Rate (time series)
   - System Conditions Over Time (time series)

5. **Active System Problems**
   - Problems table filtered to system-related issues

### Key Condition Types

| Condition | Description |
|-----------|-------------|
| `CPUHealthy` | Overall CPU health status |
| `CPUPressure` | High CPU utilization detected |
| `ThermalPressure` | CPU thermal throttling detected |
| `MemoryHealthy` | Overall memory health status |
| `MemoryPressure` | High memory utilization detected |
| `SwapPressure` | High swap utilization detected |
| `OOMKilled` | Out-of-memory events detected |
| `DiskHealthy` | Overall disk health status |
| `DiskPressure` | Low disk space detected |
| `InodePressure` | Low inode count detected |
| `ReadOnlyFilesystem` | Filesystem mounted read-only |

---

## Node Doctor Kubernetes Health Dashboard

**UID:** `node-doctor-k8s`

Monitoring of Kubelet health, API Server connectivity, and Node Doctor agent status.

### Panels

1. **Kubelet Health Row**
   - KubeletHealthy Nodes (stat)
   - Kubelet Service Running (stat)
   - Kubelet Ready (stat)
   - Kubelet Conditions by Node (table)

2. **API Server Connectivity Row**
   - API Server Reachable (stat)
   - API Server Latency High (stat)
   - API Server Conditions by Node (table)

3. **Node Doctor Agent Status Row**
   - Monitored Nodes (stat)
   - Min Agent Uptime (stat)
   - Max Agent Uptime (stat)
   - Status Updates Rate (stat)
   - Agent Status by Node (table with version info)

4. **Kubernetes Health Trends**
   - Kubernetes Events Rate (time series)
   - Kubernetes Conditions Over Time (time series)

5. **Active Kubernetes Problems**
   - Problems table filtered to Kubernetes-related issues

### Key Condition Types

| Condition | Description |
|-----------|-------------|
| `KubeletHealthy` | Overall kubelet health status |
| `KubeletServiceRunning` | Kubelet systemd service is running |
| `KubeletReady` | Kubelet /healthz endpoint returns healthy |
| `APIServerReachable` | Node can reach the Kubernetes API server |
| `APIServerLatencyHigh` | High latency communicating with API server |
| `NodeDoctorHealthy` | Node Doctor agent is healthy |

---

## Node Doctor CNI & Network Health Dashboard

**UID:** `node-doctor-cni`

Network partition detection, CNI health, DNS resolution, and peer connectivity monitoring.

### Panels

1. **Overview Row**
   - Monitored Nodes (stat)
   - Partitioned Nodes (stat) - Red when > 0
   - Degraded Nodes (stat) - Yellow when > 0
   - Active Problems (stat)
   - Min Uptime (stat)
   - Unreachable Peers (stat)

2. **Node Health Status**
   - Network Partition Status by Node (table)
   - CNI Health Status by Node (table)

3. **DNS Health Row**
   - DNS Healthy Nodes (stat)
   - DNS Resolution Failed (stat)
   - DNS Latency High (stat)
   - DNS Conditions by Node (table)

4. **Events & Trends**
   - CNI Events Rate (time series)
   - DNS Events Rate (time series)
   - CNI Status Updates (time series)

5. **All Conditions**
   - Network Conditions by Type (stacked bar chart)
   - DNS Conditions by Type (stacked bar chart)

6. **Problems & Alerts**
   - Active Network Problems (table)

### Key Condition Types

| Condition | Description |
|-----------|-------------|
| `NetworkPartitioned` | Node cannot reach minimum required peers |
| `NetworkDegraded` | High latency or partial peer connectivity |
| `CNIHealthy` | Overall CNI health status |
| `CNIConfigValid` | CNI configuration files are valid |
| `CNIInterfacesHealthy` | CNI network interfaces are up |
| `DNSHealthy` | Overall DNS resolution health |
| `DNSResolutionFailed` | DNS queries are failing |
| `DNSLatencyHigh` | DNS resolution latency is high |

---

## Alerting

You can create alerts based on these PromQL expressions:

### Critical Alerts

```promql
# Network partition detected
node_doctor_monitor_conditions_total{condition_type="NetworkPartitioned", status="True"} > 0

# Any active critical problems
node_doctor_monitor_problems_active{severity="Critical"} > 0

# Node down (no metrics for 5 minutes)
absent(node_doctor_monitor_uptime_seconds{node="<node-name>"})

# Kubelet unhealthy
node_doctor_monitor_conditions_total{condition_type="KubeletHealthy", status="False"} > 0

# Read-only filesystem
node_doctor_monitor_conditions_total{condition_type="ReadOnlyFilesystem", status="True"} > 0
```

### Warning Alerts

```promql
# Memory pressure
node_doctor_monitor_conditions_total{condition_type="MemoryPressure", status="True"} > 0

# Disk pressure
node_doctor_monitor_conditions_total{condition_type="DiskPressure", status="True"} > 0

# DNS resolution issues
node_doctor_monitor_conditions_total{condition_type="DNSResolutionFailed", status="True"} > 0

# API server latency high
node_doctor_monitor_conditions_total{condition_type="APIServerLatencyHigh", status="True"} > 0
```

### Informational

```promql
# Node Doctor restart (uptime reset)
changes(node_doctor_monitor_uptime_seconds[5m]) > 0

# High event rate (potential issue)
rate(node_doctor_monitor_events_total[5m]) > 10
```

## Troubleshooting

### Dashboards Not Loading

1. Verify ConfigMaps have the correct label:
   ```bash
   kubectl get configmaps -n cattle-dashboards -l grafana_dashboard=1
   ```

2. Check Grafana sidecar logs:
   ```bash
   kubectl logs -n cattle-monitoring-system -l app.kubernetes.io/name=grafana -c grafana-sc-dashboard
   ```

3. Verify the dashboard JSON is valid:
   ```bash
   cat dashboards/node-doctor-overview.json | jq .
   ```

### No Data in Dashboards

1. Verify Node Doctor is exporting metrics:
   ```bash
   kubectl port-forward -n node-doctor svc/node-doctor 9101:9101
   curl http://localhost:9101/metrics | grep node_doctor
   ```

2. Check Prometheus is scraping Node Doctor:
   ```bash
   kubectl port-forward -n cattle-monitoring-system svc/rancher-monitoring-prometheus 9090:9090
   # Visit http://localhost:9090/targets and look for node-doctor
   ```

3. Verify ServiceMonitor exists:
   ```bash
   kubectl get servicemonitor -A | grep node-doctor
   ```

### Wrong Datasource

If dashboards show "No data" but metrics exist:
1. Open dashboard settings
2. Go to Variables
3. Verify `datasource` variable points to your Prometheus instance
