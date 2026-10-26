# Quick start

This page installs node-doctor with Helm and checks that it works. You need a Kubernetes cluster with Linux nodes, `kubectl` access with cluster-admin rights, and Helm 3. To install without Helm, see [raw manifests](../deployment/README.md).

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

This starts two DaemonSets in the `node-doctor` namespace: `node-doctor`, the agent, and `node-doctor-overlay-test`, which the agent probes to test pod-network connectivity between nodes. Remediation is on globally, but no monitor remediates until you enable it for that monitor.

To change settings, put them in a values file and pass it with `-f`. The [Helm chart](../helm/node-doctor/README.md) page lists every value.

## Check that it works

### Pods

Every node should run one agent pod, and all of them should be Ready.

```bash
kubectl get daemonset --namespace node-doctor
```

```bash
kubectl get pods --namespace node-doctor -l app.kubernetes.io/name=node-doctor -o wide
```

```bash
kubectl logs --namespace node-doctor -l app.kubernetes.io/name=node-doctor --tail=50
```

### Node conditions

The agent writes node conditions whose type starts with `NodeDoctor`. `NodeDoctorHealthy` is its heartbeat and should be `True` on every node.

```bash
kubectl get nodes -o custom-columns='NAME:.metadata.name,NODEDOCTOR:.status.conditions[?(@.type=="NodeDoctorHealthy")].status'
```

To see all node-doctor conditions on one node:

```bash
kubectl get node <node> -o jsonpath='{range .status.conditions[*]}{.type}{"\t"}{.status}{"\t"}{.reason}{"\n"}{end}' | grep NodeDoctor
```

The agent also records Kubernetes events against the Node, in the `node-doctor` namespace:

```bash
kubectl get events --namespace node-doctor --field-selector involvedObject.kind=Node
```

### Health endpoints

Each agent serves `/healthz`, `/ready`, `/status` and `/remediation/history` on port 8080. The image has no `curl` or `wget`, so reach them through the API server proxy. Use a pod name from the `get pods` output above.

```bash
kubectl get --raw /api/v1/namespaces/node-doctor/pods/<pod>:8080/proxy/status
```

`/status` returns JSON with the state of each monitor. If the proxy call fails, see [Troubleshooting](troubleshooting.md#reach-the-health-endpoints).

### Metrics

Each agent serves Prometheus metrics on port 9101. All names start with `node_doctor_monitor_`.

```bash
kubectl get --raw /api/v1/namespaces/node-doctor/pods/<pod>:9101/proxy/metrics | grep '^node_doctor_monitor_info'
```

The `version` label shows the running agent version. [Metrics](metrics.md) lists every metric.

## Connect Prometheus and Grafana

If you run the Prometheus Operator, turn on the ServiceMonitor and the alert rules:

```bash
helm upgrade node-doctor supporttools/node-doctor --namespace node-doctor --set serviceMonitor.enabled=true --set prometheusRule.enabled=true
```

Use `-f` with your values file instead of `--set` once you have one, so later upgrades keep these settings. Without the operator, scrape the `node-doctor-metrics` headless Service on port 9101.

To import the Grafana dashboards, follow [Dashboards](../dashboards/README.md).

## Next steps

- [Monitors](monitors.md): what each check does
- [Remediation](remediation.md): turn on automatic fixes
- [Configuration](configuration.md): every config field
- [Troubleshooting](troubleshooting.md): when something looks wrong
