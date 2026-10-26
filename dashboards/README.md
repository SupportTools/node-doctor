# Grafana dashboards

This directory has five Grafana dashboards for the metrics node-doctor exports. They link to each other from the top-right corner.

| File | UID | Shows |
|---|---|---|
| `node-doctor-overview.json` | `node-doctor-overview` | Node count, active problems and condition changes across the cluster, with links to the other dashboards |
| `node-doctor-system-health.json` | `node-doctor-system` | CPU, memory and disk conditions per node |
| `node-doctor-kubernetes-health.json` | `node-doctor-k8s` | Kubelet and API server conditions, and the agent's own uptime and version |
| `node-doctor-cni-network-health.json` | `node-doctor-cni` | Network partition, CNI and DNS conditions, peer reachability and latency |
| `node-doctor-ipv6.json` | `node-doctor-ipv6` | IPv6 monitor conditions, IPv6 peer and gateway latency, AAAA DNS latency, IPv4 vs IPv6 comparison |

Every dashboard has a `datasource` and a `node` variable. The CNI and IPv6 dashboards also have `address_family` (`ipv4`, `ipv6`).

## Import

### Grafana sidecar (kube-prometheus-stack, Rancher Monitoring)

The Grafana sidecar loads every ConfigMap labeled `grafana_dashboard=1` in the namespace it watches. Rancher Monitoring watches `cattle-dashboards`; kube-prometheus-stack usually watches its own release namespace. Run these from the repository root with `NS` set to that namespace.

```bash
for f in dashboards/*.json; do kubectl create configmap "$(basename "$f" .json)" --namespace "$NS" --from-file="$f"; done
```

```bash
for f in dashboards/*.json; do kubectl label configmap "$(basename "$f" .json)" --namespace "$NS" grafana_dashboard=1; done
```

### Grafana UI

In Grafana, open Dashboards > New > Import, upload a JSON file, and pick your Prometheus data source.

### Grafana provisioning

Copy the JSON files to a directory on the Grafana host and point a file provider at it:

```yaml
apiVersion: 1
providers:
  - name: node-doctor
    folder: Node Doctor
    type: file
    options:
      path: /var/lib/grafana/dashboards/node-doctor
```

## Metrics used

The panels query these metrics. All of them exist in the agent today. [Metrics](../docs/metrics.md) describes each one.

| Dashboard | Metrics (`node_doctor_monitor_` prefix left out) |
|---|---|
| Overview | `info`, `uptime_seconds`, `conditions_total`, `problems_active`, `events_total`, `status_updates_total` |
| System Health | `info`, `conditions_total`, `problems_active`, `events_total`, `status_updates_total` |
| Kubernetes Health | `info`, `uptime_seconds`, `conditions_total`, `problems_active`, `events_total`, `status_updates_total` |
| CNI & Network | `info`, `conditions_total`, `problems_active`, `events_total`, `status_updates_total`, `peer_latency_seconds`, `peer_latency_histogram_seconds`, `peer_reachable`, `peers_total`, `peers_reachable_total` |
| IPv6 | `info`, `monitor_up`, `condition_status`, `conditions_total`, `events_total`, `peer_latency_seconds`, `peer_reachable`, `gateway_latency_seconds`, `dns_latency_seconds` |

The dashboards expect the `node_doctor_monitor_` prefix. If you change `exporters.prometheus.namespace` or `exporters.prometheus.subsystem`, they show no data.

Some condition panels filter on condition types the agent does not emit, so they stay empty: `ThermalPressure`, `SwapPressure`, `KubeletReady`, `KubeletServiceRunning`, `APIServerAuthenticated`, `DNSLatencyHigh` and `ReadOnlyFilesystem` (the agent emits `ReadonlyFilesystem`).

## No data

Check that the agent exports metrics. Use any agent pod name:

```bash
kubectl get --raw /api/v1/namespaces/node-doctor/pods/<pod>:9101/proxy/metrics | grep -c '^node_doctor_monitor_'
```

Check that Prometheus finds the agents. With the Prometheus Operator, the ServiceMonitor must exist and match your Prometheus selectors:

```bash
kubectl get servicemonitor --all-namespaces | grep node-doctor
```

If metrics exist but panels are empty, open the dashboard settings and check that the `datasource` variable points at the Prometheus that scrapes node-doctor.
