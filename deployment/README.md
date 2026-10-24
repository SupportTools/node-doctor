# Install with raw manifests

Use these manifests to run node-doctor without Helm. They install only the agent DaemonSet: no overlay-test pods, no CNI connectivity monitor, no write probe and no controller. For those, use the [Helm chart](../helm/node-doctor/README.md).

## Install

Use the copies attached to a GitHub release. The release copy of `daemonset.yaml` pins that release's image (for example `supporttools/node-doctor:1.10.0`). The copy in this directory is only updated by hand and is usually behind.

The manifests expect the `node-doctor` namespace, and none of them creates it.

```bash
kubectl create namespace node-doctor
```

```bash
kubectl apply -f https://github.com/supporttools/node-doctor/releases/download/v1.10.0/rbac.yaml
```

```bash
kubectl apply -f https://github.com/supporttools/node-doctor/releases/download/v1.10.0/daemonset.yaml
```

Replace `v1.10.0` with the release you want. The download path keeps the `v`; the image tag does not.

If you run the Prometheus Operator, also apply the ServiceMonitor and alert rules from this directory:

```bash
kubectl apply -f deployment/servicemonitor.yaml
```

```bash
kubectl apply -f deployment/prometheusrule.yaml
```

## What gets installed

| File | Objects |
|---|---|
| `rbac.yaml` | ServiceAccount, ClusterRole and ClusterRoleBinding, all named `node-doctor` |
| `daemonset.yaml` | DaemonSet `node-doctor`, ConfigMap `node-doctor-config`, headless Service `node-doctor-metrics` |
| `servicemonitor.yaml` | ServiceMonitor `node-doctor` |
| `prometheusrule.yaml` | PrometheusRule `node-doctor` |

The DaemonSet:

- runs privileged with `hostNetwork` and `hostPID`, and tolerates every taint
- serves health on port 8080 and metrics on port 9101 (`node_doctor_monitor_*`)
- requests 50m CPU and 128Mi memory, with limits of 500m CPU and 256Mi memory
- rolls with `maxUnavailable: 25%`

See the [Helm chart](../helm/node-doctor/README.md#pod) for why the CPU limit and `maxUnavailable` have these values.

## How it differs from the Helm chart

- Probes use `httpGet` on port 8080 (`/healthz` for liveness and startup, `/ready` for readiness). The chart uses `exec` probes over a unix socket. On a host where another process owns port 8080, these HTTP probes fail and the pod restarts. To switch, copy the `health-runtime` volume and the `exec` probes from the chart.
- The Service does not set `publishNotReadyAddresses`, so Prometheus stops scraping a pod while it is NotReady.
- The ConfigMap has no `dns-health` monitor. The agent adds one with defaults, including a cluster-domain check. From the host network that check can report false failures on some CNIs (for example Cilium). Add a `network-dns-check` monitor with `clusterDomains: []` to the ConfigMap to stop it, as the chart does.

## Change the config

Edit the `node-doctor-config` ConfigMap. The agent reloads it without a restart. See [Troubleshooting](../docs/troubleshooting.md#a-config-change-did-not-take-effect) when a change does not apply, and [Configuration](../docs/configuration.md) for the fields.

```bash
kubectl edit configmap node-doctor-config --namespace node-doctor
```

## Verify

`smoke-test.sh` checks the DaemonSet, pod restarts, the metrics endpoint, the Service, the ServiceMonitor and PrometheusRule, Prometheus scraping, and node conditions. It needs `kubectl` and `curl` on your machine.

```bash
./deployment/smoke-test.sh node-doctor monitoring
```

The arguments are the node-doctor namespace and the Prometheus namespace. For manual checks, see [Quick Start](../docs/quick-start.md#check-that-it-works).

## Uninstall

```bash
kubectl delete -f https://github.com/supporttools/node-doctor/releases/download/v1.10.0/daemonset.yaml
```

```bash
kubectl delete -f https://github.com/supporttools/node-doctor/releases/download/v1.10.0/rbac.yaml
```

```bash
kubectl delete namespace node-doctor
```
