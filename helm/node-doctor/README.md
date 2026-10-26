# Node Doctor Helm chart

This chart installs the node-doctor agent as a DaemonSet on every Linux node. It also installs the overlay-test DaemonSet (on by default) and can install the controller (off by default). This page is the values reference. For a first install and how to check that it works, see [Quick Start](../../docs/quick-start.md).

## Install

```bash
helm repo add supporttools https://charts.support.tools
```

```bash
helm repo update
```

```bash
helm install node-doctor supporttools/node-doctor --namespace node-doctor --create-namespace
```

Pass overrides with `-f my-values.yaml` or `--set key=value`.

## Upgrade

```bash
helm upgrade node-doctor supporttools/node-doctor --namespace node-doctor -f my-values.yaml
```

Each chart release sets `image.tag` and `overlayTest.image.tag` to its own version. `helm upgrade --reuse-values` keeps the old chart's defaults, so the pods stay on the old image tag. Pass your values file again instead, or use `--reset-then-reuse-values` (Helm 3.14+).

## Uninstall

```bash
helm uninstall node-doctor --namespace node-doctor
```

Node conditions that node-doctor wrote stay on the nodes after uninstall. They all start with `NodeDoctor`.

## Values

Defaults come from [values.yaml.template](values.yaml.template). CI fills in `${IMAGE_TAG}` when it publishes the chart. Image tags have no `v` prefix: release `v1.10.0` publishes image `1.10.0` and chart `1.10.0`.

### Image

| Key | Default | Notes |
|---|---|---|
| `image.repository` | `supporttools/node-doctor` | Docker Hub |
| `image.tag` | chart version | Falls back to `appVersion` when empty. `appVersion` has a `v` prefix that no image tag has, so keep this set. |
| `image.pullPolicy` | `IfNotPresent` | |
| `imagePullSecrets` | `[]` | Used by the agent and overlay-test pods |
| `nameOverride`, `fullnameOverride` | `""` | |

### Agent settings

These go into the agent config file. [Configuration](../../docs/configuration.md) explains each field.

| Key | Default |
|---|---|
| `settings.logLevel` | `info` |
| `settings.logFormat` | `json` |
| `settings.updateInterval` | `30s` |
| `settings.resyncInterval` | `5m` |
| `settings.heartbeatInterval` | `1m` |
| `settings.dryRunMode` | `false` |
| `settings.qps` | `50` |
| `settings.burst` | `100` |
| `reload.enabled` | `true` |
| `reload.debounceInterval` | `500ms` |
| `features.enableProfiling` | `false` |

With `reload.enabled: true` the agent applies ConfigMap changes without a restart. A change it cannot apply in place produces a `ConfigReloadRestartRequired` event; see [Troubleshooting](../../docs/troubleshooting.md#a-config-change-did-not-take-effect).

### Pod

| Key | Default |
|---|---|
| `serviceAccount.create` | `true` |
| `serviceAccount.name` | `node-doctor` |
| `serviceAccount.annotations` | `{}` |
| `podAnnotations` | `prometheus.io/scrape: "true"`, `prometheus.io/port: "9101"`, `prometheus.io/path: /metrics` |
| `podLabels` | `{}` |
| `podSecurityContext` | root, `supplementalGroups: [0, 997, 998, 999]` |
| `securityContext` | `privileged: true` plus `SYS_ADMIN`, `SYS_PTRACE`, `NET_ADMIN`, `SYS_TIME`, `SETUID`, `SETGID` |
| `hostNetwork` | `true` |
| `hostPID` | `true` |
| `dnsPolicy` | `ClusterFirstWithHostNet` |
| `priorityClassName` | `system-node-critical` |
| `resources.requests` | `cpu: 50m`, `memory: 128Mi` |
| `resources.limits` | `cpu: 500m`, `memory: 256Mi` |
| `nodeSelector` | `kubernetes.io/os: linux` |
| `tolerations` | `operator: Exists` plus the common node taints |
| `affinity` | `{}` |
| `terminationGracePeriodSeconds` | `30` |
| `updateStrategy.type` | `RollingUpdate` |
| `updateStrategy.rollingUpdate.maxUnavailable` | `25%` |

Keep the CPU limit at 500m. At 200m the agent was CFS-throttled 30-71% of the time on a 13-node fleet. The agent measures network latency itself, so throttling shows up as false `NetworkDegraded` and `HighPeerLatency` findings.

Keep `maxUnavailable` a percentage. With `1`, a single crash-looping pod uses the whole budget and the rollout never moves. Do not add `maxSurge`: the agent uses host ports, so a surge pod cannot start next to the pod it replaces. See [stuck rollout](../../docs/troubleshooting.md#the-daemonset-rollout-is-stuck).

### Probes

All three probes use `exec`. They run `node-doctor -healthcheck` (liveness and startup, `/healthz`) or `node-doctor -healthcheck-ready` (readiness, `/ready`) against the unix socket `/var/run/node-doctor/health.sock`. The socket sits in a per-pod memory volume, so a host process that owns port 8080 cannot break the probes.

| Key | Default |
|---|---|
| `probes.liveness.enabled` | `true` |
| `probes.liveness.exec.command` | `["/usr/local/bin/node-doctor", "-healthcheck"]` |
| `probes.liveness` timing | delay 30s, period 10s, timeout 5s, failures 3 |
| `probes.readiness.enabled` | `true` |
| `probes.readiness.exec.command` | `["/usr/local/bin/node-doctor", "-healthcheck-ready"]` |
| `probes.readiness` timing | delay 10s, period 5s, timeout 3s, failures 3 |
| `probes.startup.enabled` | `true` |
| `probes.startup.exec.command` | `["/usr/local/bin/node-doctor", "-healthcheck"]` |
| `probes.startup` timing | delay 10s, period 5s, timeout 3s, failures 20 |

Liveness checks only the process itself. Readiness fails when an exporter (for example the Kubernetes API exporter) fails 3 times in a row; the pod goes NotReady but is not restarted. The startup budget is 110 seconds. Do not lower its `failureThreshold` below about 12.

To use HTTP probes, remove the `exec` key and set `httpGet: {path: /healthz, port: 8080}`.

### Service and Prometheus

| Key | Default | Notes |
|---|---|---|
| `service.type` | `ClusterIP` | |
| `service.clusterIP` | `None` | Headless |
| `service.httpPort` | `8080` | Health server |
| `service.metricsPort` | `9101` | 9100 is left to node-exporter |
| `service.publishNotReadyAddresses` | `true` | Keep it true |
| `service.annotations` | `prometheus.io/*` for port 9101 | |
| `exporters.prometheus.enabled` | `true` | |
| `exporters.prometheus.port` | `9101` | Must match `service.metricsPort` |
| `exporters.prometheus.path` | `/metrics` | |
| `exporters.prometheus.namespace` | `node_doctor` | |
| `exporters.prometheus.subsystem` | `monitor` | Metric names are `node_doctor_monitor_*`. The dashboards and alerts expect this. |
| `serviceMonitor.enabled` | `false` | Needs the Prometheus Operator CRDs |
| `serviceMonitor.namespace` | release namespace | |
| `serviceMonitor.labels` | `{}` | |
| `serviceMonitor.interval` | `30s` | |
| `serviceMonitor.scrapeTimeout` | `10s` | |
| `serviceMonitor.port` | `metrics` | |
| `serviceMonitor.path` | `/metrics` | |
| `serviceMonitor.honorLabels` | `true` | |
| `serviceMonitor.metricRelabelings`, `serviceMonitor.relabelings` | `[]` | |

The Service exists only so Prometheus can find the agents. With `publishNotReadyAddresses: false`, a NotReady agent drops out of the Endpoints and Prometheus stops scraping that node just when it has a problem. `NodeDoctorNoMetrics` fires only when every node stops reporting, so the gap would not alert.

[Metrics](../../docs/metrics.md) lists the metric names.

### PrometheusRule

`prometheusRule.enabled: true` creates a PrometheusRule. It needs the Prometheus Operator CRDs.

| Key | Default |
|---|---|
| `prometheusRule.enabled` | `false` |
| `prometheusRule.namespace` | release namespace |
| `prometheusRule.labels` | `{}` |
| `prometheusRule.additionalRules` | `[]` |
| `prometheusRule.critical.enabled` | `true` |
| `prometheusRule.warning.enabled` | `true` |
| `prometheusRule.info.enabled` | `true` |

Each alert key sits under its group, for example `prometheusRule.critical.networkPartitioned.for`.

| Group | Alert key | Alert | `for` | Other keys |
|---|---|---|---|---|
| critical | `cniConfigInvalid` | `NodeDoctorCNIConfigInvalid` | 5m | `labels`, `annotations` |
| critical | `cniUnhealthy` | `NodeDoctorCNIUnhealthy` | 5m | `labels`, `annotations` |
| critical | `networkPartitioned` | `NodeDoctorNetworkPartitioned` | 2m | `labels`, `annotations` |
| critical | `kubeletUnhealthy` | `NodeDoctorKubeletUnhealthy` | 5m | `labels`, `annotations` |
| critical | `readOnlyFilesystem` | `NodeDoctorReadOnlyFilesystem` | 1m | `labels`, `annotations` |
| critical | `ipForwardingDisabled` | `NodeDoctorIPForwardingDisabled` | 2m | `labels`, `annotations` |
| warning | `dnsResolutionFailed` | `NodeDoctorDNSResolutionFailed` | 5m | |
| warning | `dnsLatencyHigh` | `NodeDoctorDNSLatencyHigh` | 10m | |
| warning | `memoryPressure` | `NodeDoctorMemoryPressure` | 10m | |
| warning | `diskPressure` | `NodeDoctorDiskPressure` | 10m | |
| warning | `networkDegraded` | `NodeDoctorNetworkDegraded` | 10m | |
| warning | `highPeerLatency` | `NodeDoctorHighPeerLatency` | 10m | `thresholdMs: 100` |
| warning | `lowPeerConnectivity` | `NodeDoctorLowPeerConnectivity` | 5m | `thresholdPercent: 90` |
| warning | `apiServerLatencyHigh` | `NodeDoctorAPIServerLatencyHigh` | 10m | |
| warning | `ipv6Misconfigured` | `NodeDoctorIPv6Misconfigured` | 10m | `enabled: false`, `labels`, `annotations` |
| info | none | `NodeDoctorAgentRestarted` | 0m | |
| info | `highEventRate` | `NodeDoctorHighEventRate` | 5m | `threshold: 10` |
| info | `noMetrics` | `NodeDoctorNoMetrics` | 10m | |

`ipv6Misconfigured` is off by default because IPv4-only nodes raise IPv6 warnings that would page. Keep the key in your values even when it is off, because the template reads it.

### Monitors

Each block becomes one monitor in the agent config. [Monitors](../../docs/monitors.md) explains what each one checks.

| Key | Default |
|---|---|
| `monitors.cpu.enabled` | `true` |
| `monitors.cpu.interval`, `.timeout` | `30s`, `10s` |
| `monitors.cpu.warningLoadFactor`, `.criticalLoadFactor` | `0.8`, `0.95` |
| `monitors.memory.enabled` | `true` |
| `monitors.memory.interval`, `.timeout` | `30s`, `10s` |
| `monitors.memory.warningThreshold`, `.criticalThreshold` | `85`, `95` |
| `monitors.memory.swapWarningThreshold`, `.swapCriticalThreshold` | `50`, `80` |
| `monitors.memory.checkOOMKills` | `true` |
| `monitors.disk.enabled` | `true` |
| `monitors.disk.interval`, `.timeout` | `1m`, `30s` |
| `monitors.disk.mountPoints` | `/` and `/var/lib/kubelet` at 85/95, `/var/lib/docker` and `/var/lib/containerd` at 80/90 |
| `monitors.disk.checkDiskSpace`, `.checkInodes`, `.checkReadonly`, `.checkIOHealth` | `true` |
| `monitors.disk.writeProbe.enabled` | `true` |
| `monitors.disk.writeProbe.hostPath` | `/var/lib/node-doctor/write-probe` |
| `monitors.disk.writeProbe.mountPath` | `/host-write-probe` |
| `monitors.disk.writeProbe.timeout` | `10s` |
| `monitors.kubelet.enabled` | `false` |
| `monitors.kubelet.interval`, `.timeout` | `30s`, `10s` |
| `monitors.kubelet.healthzURL` | `http://127.0.0.1:10248/healthz` |
| `monitors.kubelet.failureThreshold` | `3` |
| `monitors.kubelet.checkSystemdStatus` | `false` |
| `monitors.kubelet.checkPLEG` | `false` |
| `monitors.ipForwarding.enabled` | `true` |
| `monitors.ipForwarding.interval`, `.timeout` | `30s`, `5s` |
| `monitors.ipForwarding.checkIPv4`, `.checkIPv6` | `true` |
| `monitors.ipForwarding.checkPerInterface` | `false` |
| `monitors.ipv6Sysctl.enabled` | `true` |
| `monitors.ipv6Sysctl.expectIPv6Enabled` | `true` |
| `monitors.ipv6Sysctl.checkPerInterface` | `false` |
| `monitors.ipv6Route.enabled` | `true` |
| `monitors.ipv6Route.expectDefaultRoute` | `true` |
| `monitors.ipv6Neighbor.enabled` | `true` |
| `monitors.ipv6Neighbor.expectIPv6Enabled` | `true` |
| `monitors.ipv6Neighbor.checkPerInterface` | `true` |
| `monitors.ipv6Neighbor.requireGlobalAddress` | `false` |
| `monitors.ipv6Firewall.enabled` | `true` |
| `monitors.ipv6Firewall.expectIPv6Enabled` | `true` |
| `monitors.ipv6Firewall.backend` | `auto` (or `ip6tables`, `nft`) |

Leave `checkSystemdStatus` false on RKE2 and k3s, where kubelet is not its own systemd unit. `checkPLEG` needs authenticated access to the kubelet port 10250.

`enabled: false` renders the monitor with `enabled: false`, so the agent does not add its built-in default for that type.

The IPv6 monitors run every 60s with a 5s timeout, read `/host/proc`, and never change host settings. On IPv4-only nodes they report warnings. To quiet them, set `expectIPv6Enabled: false` on `ipv6Sysctl`, `ipv6Neighbor` and `ipv6Firewall`, and `expectDefaultRoute: false` on `ipv6Route`.

The disk write probe creates, writes, syncs and deletes a file in `hostPath` on every check. It catches a root filesystem that stopped taking writes, which the read-only flag check misses on Linux 6.7 and later. The chart mounts only `hostPath` read-write; the rest of the host root stays read-only.

Two more monitors have no values block. The chart always adds `dns-health`, which resolves `google.com` and `cloudflare.com` and skips cluster domains, because a hostNetwork pod cannot resolve cluster records on some CNIs. It adds `cni-connectivity`, which probes the overlay-test pods, when `overlayTest.enabled` is true. The agent itself adds `gateway-health` with defaults.

### Kubelet remediation

`monitors.kubelet.remediation.enabled: true` lets the agent restart the systemd unit that runs kubelet, once per incident, while `KubeletUnhealthy` is True. It needs `monitors.kubelet.enabled: true`. Read [Remediation](../../docs/remediation.md) before turning it on.

| Key | Default | Notes |
|---|---|---|
| `monitors.kubelet.remediation.enabled` | `false` | |
| `monitors.kubelet.remediation.service` | `rke2-agent` | Required when enabled |
| `monitors.kubelet.remediation.requireServiceEnabled` | `true` | Skips nodes where the unit is not `enabled`, such as RKE2 servers |
| `monitors.kubelet.remediation.maxAttemptsPerIncident` | `1` | The incident ends when kubelet is healthy again |
| `monitors.kubelet.remediation.cooldown` | `30m` | Minimum time between restarts |
| `monitors.kubelet.remediation.requireWriteProbeOK` | `true` | Restart only if the disk write probe passed within `writeProbeMaxAge`. Rendering fails if the write probe is off. |
| `monitors.kubelet.remediation.writeProbeMaxAge` | `3m` | |

### Exporters and remediation

| Key | Default | Notes |
|---|---|---|
| `exporters.kubernetes.enabled` | `true` | Writes node conditions and events |
| `exporters.kubernetes.updateInterval` | `30s` | |
| `exporters.kubernetes.resyncInterval` | `5m` | |
| `exporters.kubernetes.heartbeatInterval` | `1m` | |
| `exporters.kubernetes.maxEventsPerMinute` | `10` | |
| `exporters.kubernetes.eventTTL` | `1h` | |
| `exporters.kubernetes.deduplicationWindow` | `5m` | |
| `exporters.http.enabled` | `false` | Report webhook. `controller.enabled` turns it on. |
| `exporters.http.port` | `8080` | |
| `exporters.http.tlsEnabled` | `false` | |
| `remediation.enabled` | `true` | Global switch. A monitor remediates only if its own block enables it. |
| `remediation.dryRun` | `false` | |
| `remediation.maxRemediationsPerHour` | `5` | |
| `remediation.maxRemediationsPerMinute` | `1` | |

### Overlay test

The overlay-test DaemonSet runs a small HTTP server on the pod network of every node. The agent's CNI monitor probes these pods to measure node-to-node connectivity over the overlay.

| Key | Default | Notes |
|---|---|---|
| `overlayTest.enabled` | `true` | Also adds or removes the `cni-connectivity` monitor |
| `overlayTest.image.repository` | `supporttools/node-doctor-overlay-test` | |
| `overlayTest.image.tag` | chart version | |
| `overlayTest.image.pullPolicy` | `IfNotPresent` | |
| `overlayTest.port` | `8023` | |
| `overlayTest.resources.requests` | `cpu: 5m`, `memory: 8Mi` | |
| `overlayTest.resources.limits` | `cpu: 100m`, `memory: 16Mi` | At 10m CPU the pod added 300-500ms to every probe |
| `overlayTest.updateStrategy.maxUnavailable` | `25%` | |
| `overlayTest.priorityClassName` | `""` | |

### Cluster DNS pod probe

`clusterDnsPodProbe.enabled: true` adds a monitor that asks the overlay-test pods to resolve cluster DNS and sets `ClusterDNSDown` from their answers. It needs overlay-test 1.8.3 or later. Older images return 404, so every probe fails.

| Key | Default |
|---|---|
| `clusterDnsPodProbe.enabled` | `false` |
| `clusterDnsPodProbe.interval`, `.timeout` | `30s`, `5s` |
| `clusterDnsPodProbe.labelSelector` | `app=node-doctor-overlay-test` |
| `clusterDnsPodProbe.probePort` | `8023` |
| `clusterDnsPodProbe.probePath` | `/clusterdns` |
| `clusterDnsPodProbe.minSuccessPods` | `1` |
| `clusterDnsPodProbe.failureCountThreshold` | `3` |

### Controller

`controller.enabled: true` installs the controller Deployment, Service, ConfigMap, RBAC and PVC, and points every agent at it. No controller image is published, so read [Controller](../../docs/controller-deployment.md) first.

| Key | Default | Notes |
|---|---|---|
| `controller.enabled` | `false` | |
| `controller.image.repository` | `supporttools/node-doctor-controller` | |
| `controller.image.tag` | `""` | Falls back to the `v`-prefixed `appVersion`. Set it. |
| `controller.image.pullPolicy` | `IfNotPresent` | |
| `controller.serviceAccount.create` | `true` | |
| `controller.serviceAccount.name` | `""` | Generated when empty |
| `controller.serviceAccount.annotations` | `{}` | |
| `controller.service.type` | `ClusterIP` | |
| `controller.service.port` | `8080` | Agents connect to this port |
| `controller.service.annotations` | `{}` | |
| `controller.persistence.enabled` | `true` | SQLite on a PVC |
| `controller.persistence.storageClassName` | `""` | Cluster default |
| `controller.persistence.accessMode` | `ReadWriteOnce` | |
| `controller.persistence.size` | `1Gi` | |
| `controller.persistence.annotations` | `{}` | |
| `controller.resources.requests` | `cpu: 100m`, `memory: 64Mi` | |
| `controller.resources.limits` | `cpu: 500m`, `memory: 256Mi` | |
| `controller.config.server.port` | `8080` | API, health and `/metrics` |
| `controller.config.server.readTimeout`, `.writeTimeout` | `30s` | |
| `controller.config.server.enableCORS` | `false` | |
| `controller.config.storage.path` | `/data/node-doctor.db` | |
| `controller.config.storage.retention` | `720h` | |
| `controller.config.correlation.enabled` | `true` | |
| `controller.config.correlation.clusterWideThreshold` | `0.3` | |
| `controller.config.correlation.evaluationInterval` | `30s` | |
| `controller.config.correlation.minNodesForCorrelation` | `2` | |
| `controller.config.coordination.enabled` | `true` | |
| `controller.config.coordination.maxConcurrentRemediations` | `3` | |
| `controller.config.coordination.defaultLeaseDuration` | `5m` | Also the agents' lease timeout |
| `controller.config.coordination.cooldownPeriod` | `10m` | |
| `controller.config.coordination.fallbackOnUnreachable` | `false` | See below |
| `controller.preferControlPlane` | `true` | Prefers control-plane nodes and tolerates their taints |
| `controller.tolerations` | `[]` | |
| `controller.podAnnotations` | `{}` | The chart sets the `prometheus.io/*` annotations itself |
| `controller.podSecurityContext` | non-root, UID 65534 | |
| `controller.securityContext` | no privilege escalation, read-only root, drop all | |

With `fallbackOnUnreachable: false`, agents do not remediate while the controller is down. With `true`, every agent approves its own lease during an outage, so all nodes can remediate at once.
