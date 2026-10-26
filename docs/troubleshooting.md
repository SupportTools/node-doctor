# Troubleshooting

This page covers problems with the node-doctor agent itself and how to read what it reports. The commands assume the Helm install in the `node-doctor` namespace with release name `node-doctor`. For the basic install checks, see [Quick Start](quick-start.md#check-that-it-works).

## The image has no debug tools

The agent image is Debian slim plus `systemd` (for `journalctl`). It has a shell, but no `curl`, `wget`, `nslookup` or `dig`, so `kubectl exec ... curl` fails. Use one of these instead:

- the API server proxy, for the HTTP endpoints (next section)
- the agent binary's own health check, through `kubectl exec`
- `kubectl debug`, to attach a container that has the tools

## Reach the health endpoints

The agent serves `/healthz`, `/ready`, `/status` and `/remediation/history` on TCP port 8080 and on the unix socket `/var/run/node-doctor/health.sock`. Metrics are on port 9101.

Through the API server proxy:

```bash
kubectl get --raw /api/v1/namespaces/node-doctor/pods/<pod>:8080/proxy/status
```

```bash
kubectl get --raw /api/v1/namespaces/node-doctor/pods/<pod>:9101/proxy/metrics
```

Through the socket, the same way the Helm probes do it. The command prints nothing and exits 0 when the check passes, and prints the error otherwise.

```bash
kubectl exec --namespace node-doctor <pod> -- /usr/local/bin/node-doctor -healthcheck-ready
```

With a debug container. The pod uses the host network, so the agent listens on the node's localhost:

```bash
kubectl debug --namespace node-doctor -it <pod> --image=busybox:1.36 --target=node-doctor -- wget -qO- http://127.0.0.1:8080/status
```

The agent runs with `hostNetwork`, so port 8080 is a port on the node. If another host process already holds it, the agent logs `health server TCP bind on port 8080 failed` and serves health only on the socket. The proxy then reaches the other process. The Helm probes keep working because they use the socket; the raw manifests use HTTP probes and do not.

## Read node conditions and events

Every condition node-doctor writes has a type that starts with `NodeDoctor`, for example `NodeDoctorKubeletUnhealthy`. Metrics and alerts use the name without the prefix (`condition_type="KubeletUnhealthy"`). `NodeDoctorHealthy` is the agent's heartbeat.

```bash
kubectl get node <node> -o jsonpath='{range .status.conditions[*]}{.type}{"\t"}{.status}{"\t"}{.reason}{"\t"}{.message}{"\n"}{end}' | grep NodeDoctor
```

The agent records events against the Node object, in its own namespace:

```bash
kubectl get events --namespace node-doctor --field-selector involvedObject.kind=Node,involvedObject.name=<node>
```

Agent logs show which monitors started, which default monitors it added, and remediation attempts:

```bash
kubectl logs --namespace node-doctor <pod> --tail=200
```

[Metrics](metrics.md) lists every metric. These show the agent's own health:

| Metric | What to look for |
|---|---|
| `node_doctor_monitor_uptime_seconds` | Resets when the agent restarts |
| `node_doctor_monitor_monitor_up` | 0 for a monitor that is not running |
| `node_doctor_monitor_monitor_cycle_last_timestamp_seconds` | Stops moving when a monitor hangs |
| `node_doctor_monitor_exporter_healthy` | 0 for an exporter that is failing |
| `node_doctor_monitor_exporter_consecutive_failures` | Climbs while an exporter fails |
| `node_doctor_monitor_config_reload_last_success` | 0 after a failed reload |
| `node_doctor_monitor_remediator_circuit_breaker_state` | 1 (open) or 2 (half-open) when remediation is paused after failures |

## A pod is NotReady but not restarting

This is by design. Liveness (`/healthz`) checks only that the process is alive. Readiness (`/ready`) fails when an exporter, usually the Kubernetes API exporter, fails 3 times in a row. The pod goes NotReady but keeps running and keeps monitoring.

Check `/status` and the `exporter_*` metrics above to see which exporter is failing. On these nodes the cause is often the node itself: the API server or DNS is unreachable from it.

Prometheus keeps scraping NotReady pods, because the Helm Service sets `publishNotReadyAddresses: true`. Do not turn that off; see the [Helm chart](../helm/node-doctor/README.md#service-and-prometheus). The raw-manifest Service does not set it.

## A config change did not take effect

The agent watches its ConfigMap and applies most changes in place. It logs which monitors it reconfigured and emits one of these events:

| Event reason | Meaning |
|---|---|
| `ConfigReloadSucceeded` | Applied |
| `ConfigReloadNoChanges` | The file changed but the effective config did not |
| `ConfigValidationFailed`, `ConfigReloadFailed` | Rejected; the old config stays active |
| `ConfigReloadRestartRequired` | Some changes cannot be applied to a running process |

```bash
kubectl get events --namespace node-doctor --field-selector reason=ConfigReloadRestartRequired
```

The event message lists the changes that need a restart, such as `settings.nodeName`, the log destination, or turning on an exporter or remediation that was off. Restart the agents to apply them:

```bash
kubectl rollout restart daemonset/node-doctor --namespace node-doctor
```

To check a config before you roll it out, validate it inside a running pod:

```bash
kubectl exec --namespace node-doctor <pod> -- /usr/local/bin/node-doctor -validate-config -config /etc/node-doctor/config.yaml
```

Kubernetes can take a minute or more to update a mounted ConfigMap, so wait before you decide nothing happened. With `reload.enabled: false` every change needs a restart, and the agent says so in its startup log.

In Helm, `monitors.<name>.enabled: false` does not stop the cpu, memory, disk, ipForwarding or IPv6 monitors. See the [Helm chart](../helm/node-doctor/README.md#monitors).

## The DaemonSet rollout is stuck

A DaemonSet replaces pods only while fewer than `maxUnavailable` pods are unavailable. Pods that were already broken before the rollout (CrashLoopBackOff, ImagePullBackOff, Pending) count against that budget. When they use it all, `kubectl rollout restart` stays at 0 updated pods forever. The new revision is fine; the rollout never starts. The chart uses `maxUnavailable: 25%` so one bad pod cannot block it, but many bad pods still can.

Compare unavailable pods with the budget:

```bash
kubectl get daemonset node-doctor --namespace node-doctor -o custom-columns='MAXUNAVAILABLE:.spec.updateStrategy.rollingUpdate.maxUnavailable,DESIRED:.status.desiredNumberScheduled,READY:.status.numberReady,UPDATED:.status.updatedNumberScheduled,UNAVAILABLE:.status.numberUnavailable'
```

Find the pods that hold the budget:

```bash
kubectl get pods --namespace node-doctor -l app.kubernetes.io/name=node-doctor -o wide | grep -v Running
```

Delete the broken pods first. The DaemonSet recreates them at the new revision.

```bash
kubectl delete pod --namespace node-doctor <stuck-pod>
```

If the rollout still does not move, delete healthy pods a few at a time and wait for each batch to become Ready:

```bash
kubectl rollout status daemonset/node-doctor --namespace node-doctor --timeout=120s
```

Deleting agent pods is safe. The agent keeps no state, and a node without its pod only stops reporting new findings. Do not delete all pods at once. If a pod is stuck because its node is unreachable, the pod will not terminate; drain or delete the node instead.

## NetworkPartitioned is True during a rollout

During a chart upgrade the overlay-test pods restart along with the agents. While a node's peers are restarting, its CNI monitor can see too few reachable peers and set `NodeDoctorNetworkPartitioned` to True. On a lab cluster this cleared within about 5 minutes, once the peers were Ready again.

`NodeDoctorNetworkPartitioned` alerts after 2 minutes, so it can fire during an upgrade. If that is noise for you, silence it for the upgrade window or raise `prometheusRule.critical.networkPartitioned.for`. If the condition stays True after all pods are Ready, it is a real finding.

## Remediation does not run

Remediation runs only when all of these hold:

- `remediation.enabled` is true and `remediation.dryRun` is false
- the monitor has its own `remediation` block with `enabled: true`. In Helm, only `monitors.kubelet.remediation` exists, and it is off by default.
- the per-incident attempt limit, cooldown, and the global per-hour and per-minute limits are not used up
- the circuit breaker is closed
- with the controller enabled, the controller granted a lease

A failed precondition is logged as a warning. Other skip reasons, such as the attempt limit or cooldown, are logged only at `settings.logLevel: debug`. Past attempts are in `/remediation/history`:

```bash
kubectl get --raw /api/v1/namespaces/node-doctor/pods/<pod>:8080/proxy/remediation/history
```

[Remediation](remediation.md) explains each guard and how to configure them.

## Get help

Open an issue at [github.com/supporttools/node-doctor/issues](https://github.com/supporttools/node-doctor/issues). Include the agent version (the `version` label of `node_doctor_monitor_info`), the output of `/status`, and the agent logs.
