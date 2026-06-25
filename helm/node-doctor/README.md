# Node Doctor Helm Chart

A Kubernetes DaemonSet that monitors node health, detects issues, and optionally remediates problems across your cluster.

## Prerequisites

- Kubernetes 1.19+
- Helm 3.0+
- PodSecurityPolicy (if enabled) must allow privileged containers

## Installation

### Add the Helm Repository

```bash
helm repo add supporttools https://charts.support.tools
helm repo update
```

### Install the Chart

```bash
helm install node-doctor supporttools/node-doctor \
  --namespace node-doctor \
  --create-namespace
```

### Install with Custom Values

```bash
helm install node-doctor supporttools/node-doctor \
  --namespace node-doctor \
  --create-namespace \
  -f custom-values.yaml
```

## Configuration

The following table lists the configurable parameters of the Node Doctor chart and their default values.

### General Settings

| Parameter | Description | Default |
|-----------|-------------|---------|
| `settings.logLevel` | Log level (debug, info, warn, error) | `info` |
| `settings.logFormat` | Log format (json, text) | `json` |
| `settings.updateInterval` | Monitor update interval | `30s` |
| `settings.resyncInterval` | Resync interval | `5m` |
| `settings.heartbeatInterval` | Heartbeat interval | `1m` |
| `settings.enableRemediation` | Enable remediation actions | `true` |
| `settings.dryRunMode` | Dry run mode (log without executing) | `false` |

### Image Settings

| Parameter | Description | Default |
|-----------|-------------|---------|
| `image.repository` | Image repository | `harbor.support.tools/node-doctor/node-doctor` |
| `image.pullPolicy` | Image pull policy | `IfNotPresent` |
| `image.tag` | Image tag | Chart appVersion |

### Resources

| Parameter | Description | Default |
|-----------|-------------|---------|
| `resources.requests.cpu` | CPU request | `50m` |
| `resources.requests.memory` | Memory request | `128Mi` |
| `resources.limits.cpu` | CPU limit | `200m` |
| `resources.limits.memory` | Memory limit | `256Mi` |

### Monitor Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `monitors.cpu.enabled` | Enable CPU monitor | `true` |
| `monitors.memory.enabled` | Enable memory monitor | `true` |
| `monitors.disk.enabled` | Enable disk monitor | `true` |

### Exporter Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `exporters.kubernetes.enabled` | Enable Kubernetes exporter | `true` |
| `exporters.prometheus.enabled` | Enable Prometheus exporter | `true` |
| `exporters.prometheus.port` | Prometheus metrics port | `9101` |

### ServiceMonitor (Prometheus Operator)

| Parameter | Description | Default |
|-----------|-------------|---------|
| `serviceMonitor.enabled` | Create ServiceMonitor | `false` |
| `serviceMonitor.interval` | Scrape interval | `30s` |
| `serviceMonitor.scrapeTimeout` | Scrape timeout | `10s` |

### IPv6 / Dual-Stack

Node Doctor ships four **detection-only** IPv6 monitors (they never modify host
settings) and binds its HTTP/metrics endpoints dual-stack by default.

| Parameter | Description | Default |
|-----------|-------------|---------|
| `monitors.ipv6Sysctl.enabled` | Detect `disable_ipv6` sysctl set when IPv6 is expected on | `true` |
| `monitors.ipv6Route.enabled` | Detect a missing IPv6 default route when expected | `true` |
| `monitors.ipv6Neighbor.enabled` | Detect missing RA/SLAAC address (link-local/global) and `accept_ra` disabled | `true` |
| `monitors.ipv6Firewall.enabled` | Sanity-check ip6tables/nftables for an IPv6 black-hole (detection only) | `true` |
| `monitors.ipv6Sysctl.expectIPv6Enabled` | Treat IPv6-disabled as a problem (shared key across the IPv6 monitors) | `true` |
| `monitors.ipv6Firewall.backend` | Firewall backend to read: `auto`, `ip6tables`, or `nft` | `auto` |
| `exporters.http.bindAddress` | Listen address; `::` = dual-stack (IPv4+IPv6), falls back to `0.0.0.0` if the kernel has IPv6 disabled | `"::"` |

These monitors **degrade gracefully on IPv4-only nodes**: a missing IPv6 stack is
reported as a warning, not an error, and the conditions stay healthy when IPv6
cannot be confirmed. On purely IPv4 clusters you can silence them by setting
`expectIPv6Enabled: false` (or `enabled: false`) on each:

```yaml
monitors:
  ipv6Sysctl:
    enabled: false
  ipv6Route:
    enabled: false
  ipv6Neighbor:
    enabled: false
  ipv6Firewall:
    enabled: false
```

Network metrics (`gateway_latency_seconds`, `peer_latency_seconds`,
`peer_reachable`, `dns_latency_seconds`) carry an `address_family` label
(`ipv4`/`ipv6`/`unknown`) so dashboards and alerts can distinguish the families;
the bundled PrometheusRule alerts (`prometheusRule.enabled`) include a
`NodeDoctorIPv6Misconfigured` alert and per-family peer alerts.

## Security Considerations

Node Doctor requires privileged access to monitor system health effectively. This includes:

- **Privileged Container**: Required for accessing `/proc`, `/sys`, and other system interfaces
- **Host Network**: Required for network monitoring and kubelet communication
- **Host PID**: Required for process monitoring
- **Volume Mounts**: Access to host filesystem for disk and log monitoring

## Uninstalling

```bash
helm uninstall node-doctor -n node-doctor
kubectl delete namespace node-doctor
```

## Upgrading

```bash
helm repo update
helm upgrade node-doctor supporttools/node-doctor -n node-doctor
```

## Troubleshooting

### Check DaemonSet Status

```bash
kubectl get daemonset node-doctor -n node-doctor
```

### View Pod Logs

```bash
kubectl logs -n node-doctor -l app.kubernetes.io/name=node-doctor --tail=100
```

### Check Node Conditions

```bash
kubectl get nodes -o custom-columns='NAME:.metadata.name,HEALTHY:.status.conditions[?(@.type=="NodeDoctorHealthy")].status'
```

### View Prometheus Metrics

```bash
kubectl exec -n node-doctor -it $(kubectl get pods -n node-doctor -l app.kubernetes.io/name=node-doctor -o jsonpath='{.items[0].metadata.name}') -- wget -qO- http://localhost:9101/metrics | head -50
```

## License

Apache 2.0 - See [LICENSE](https://github.com/supporttools/node-doctor/blob/main/LICENSE) for details.
