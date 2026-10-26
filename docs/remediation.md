# Remediation

Node Doctor can run a repair action when a monitor reports a problem. Remediation runs only when both the global `remediation.enabled` and the monitor's own `remediation.enabled` are true. Every dispatch then passes the monitor's dispatch policy and the registry's global safety checks before anything runs.

For the full config schema see [configuration.md](configuration.md). For the conditions each monitor reports see [monitors.md](monitors.md).

## The gate, in order

Each status a monitor sends goes through these checks. The first one that fails stops the dispatch.

```text
monitor status
  │
  ├─ 1. remediation.enabled (global) was true at startup
  ├─ 2. the monitor has remediation.enabled: true and a strategy
  ├─ 3. a condition is in its problem state (trigger rule)
  ├─ 4. monitor dispatch policy: maxAttemptsPerIncident → cooldown → preconditions
  │
  └─ for each strategy, in order (first success wins):
       ├─ 5. circuit breaker is not open
       ├─ 6. per-hour limit, then per-minute limit
       ├─ 7. controller lease granted (only when coordination is enabled)
       ├─ 8. the strategy is registered
       ├─ 9. the strategy's built-in cooldown and attempt cap allow it
       ├─ 10. dry run: log and count as success
       └─ 11. run the action
```

Steps 1 to 4 run in the detector. Steps 5 to 11 run in the remediator registry, once per strategy attempt.

The detector runs remediation on the same goroutine that exports statuses. While an action runs (a script can take up to 5 minutes, a node drain up to 2), no other status is exported or evaluated.

## Global settings

These live under the top-level `remediation:` key.

| Field | Default | What it does |
|---|---|---|
| `enabled` | `false` | Builds the registry and registers strategies at startup. When false, nothing remediates and `/remediation/history` returns 503. |
| `dryRun` | `false` | Runs every check but skips the action. `settings.dryRunMode` and the `-dry-run` flag have the same effect. |
| `maxRemediationsPerHour` | `10` | Sliding one-hour window. `0` is replaced by 10, so there is no unlimited value. |
| `maxRemediationsPerMinute` | `2` | Token bucket with a burst equal to the limit. `0` is replaced by 2. |
| `historySize` | `100` | Records kept in memory for `/remediation/history`. Capped at 10000. |
| `circuitBreaker.enabled` | `true` | `false` keeps the breaker closed no matter how many attempts fail. |
| `circuitBreaker.threshold` | `5` | Consecutive failures that open the breaker. |
| `circuitBreaker.timeout` | `5m` or `30m` | How long the breaker stays open. `5m` when the block sets no field. `30m` when the block sets another field but not `timeout`. |
| `circuitBreaker.successThreshold` | `2` | Consecutive successes in half-open that close the breaker. |
| `coordination.*` | off | Controller leases. See [Controller coordination](#controller-coordination). |

The Helm chart sets `enabled: true`, `maxRemediationsPerHour: 5` and `maxRemediationsPerMinute: 1`, and leaves every monitor's remediation off.

## Per-monitor settings

These live under `monitors[].remediation`.

| Field | Used by | What it does |
|---|---|---|
| `enabled` | all | Must be true for this monitor to dispatch. |
| `strategy` | all | Strategy name. Required when `enabled` is true, even if `strategies` is set. |
| `service` | `systemd-restart` | Unit to restart. Required for that strategy. |
| `requireServiceEnabled` | `systemd-restart` | Refuse unless `systemctl is-enabled <service>` prints `enabled`. |
| `scriptPath`, `args` | `custom-script` | Absolute path without `..`, and its arguments. |
| `interface` | `restart-interface` | Interface to bounce. Required for that strategy. |
| `strategies` | all | Ordered fallback list. Each entry has its own `strategy`, `service`, `scriptPath`, `args`, `interface` and `requireServiceEnabled`. When the list is set, the top-level strategy fields are not dispatched. |
| `triggerConditions` | policy | Condition types that start an incident when `True`. |
| `maxAttemptsPerIncident` | policy | Dispatches per incident. `0` means unlimited. |
| `cooldown` | policy | Minimum time between dispatches for the same monitor and condition, across incidents. |
| `preconditions` | policy | Conditions that must hold before a dispatch. |

The policy fields are read from the top-level block only. They are ignored inside `strategies`.

## Triggers and incidents

The trigger rule decides which conditions start an incident.

| `triggerConditions` | Relevant conditions | Problem state |
|---|---|---|
| set | only the listed condition types | `True` |
| empty | every condition the monitor reports | `False` |

The empty case is the legacy rule. It misfires on monitors with problem-named conditions such as `KubeletUnhealthy`, which report `False` when healthy. Set `triggerConditions` for those monitors.

An incident is one monitor and one condition type staying in its problem state. It ends when the monitor reports that condition out of its problem state. For each triggered condition the detector checks:

1. **maxAttemptsPerIncident.** Refuse once the incident has used its attempts. The count resets when the incident ends.
2. **cooldown.** Refuse if the last dispatch for this monitor and condition was less than `cooldown` ago. The timer survives the end of an incident, so a flapping condition cannot cause a restart on every flap.
3. **preconditions.** Refuse if any precondition fails. This does not use an attempt, and the check repeats on the next status. The warning is logged once per incident.

When all three pass, the detector records the attempt before it runs. A dispatch that fails, hangs or panics still counts.

### Preconditions

| Field | Required | Meaning |
|---|---|---|
| `type` | yes | Condition type as the monitor emits it, without the `NodeDoctor` prefix (for example `ReadonlyFilesystem`). |
| `status` | yes | `"True"` or `"False"`. |
| `maxAge` | yes | The newest matching observation must be younger than this. |
| `monitor` | no | Only match conditions from this monitor name. |

The detector looks at the latest status of every monitor, or only the named one, and takes the newest observation of that condition type. The precondition fails when the condition was never seen, is older than `maxAge`, or has a different status.

## Registry safety checks

The registry applies these checks to every strategy attempt from every monitor.

### Rate limits

- **Per hour.** A sliding one-hour window that counts successful and dry-run attempts.
- **Per minute.** A token bucket. An attempt takes a token once it passes the per-hour check, even if it fails later.

An attempt refused by either limit is recorded in history as a failure. It does not count toward the circuit breaker.

### Circuit breaker

The breaker has three states. When the Prometheus exporter is enabled, the state is exported as `node_doctor_remediator_circuit_breaker_state` (0 closed, 1 open, 2 half-open).

| State | Behavior | Moves to |
|---|---|---|
| Closed | All attempts allowed. | Open after `threshold` consecutive failures. |
| Open | All attempts refused. | Half-open on the first attempt after `timeout`. |
| Half-open | All attempts allowed. | Closed after `successThreshold` consecutive successes. Open on any failure. |

These count as failures: a lease request error, an unregistered strategy, a refusal by the strategy's built-in cooldown or attempt cap, and an action error. A lease denial, a rate-limit refusal and an open breaker do not count. A dry-run attempt counts as a success.

### Built-in cooldown and attempt cap

Each strategy also keeps its own state per condition type: a cooldown (listed in the [strategy table](#strategies)) and a cap of 3 attempts.

The cap never resets while the process runs. After 3 real attempts for the same strategy and condition type, every later attempt is refused until the pod restarts, and each refusal counts as a circuit breaker failure. Dry-run attempts use neither the cap nor the cooldown.

## Dry run

The effective dry-run flag is `remediation.dryRun` OR `settings.dryRunMode` OR the `-dry-run` flag.

In dry run the registry still applies the circuit breaker, the rate limits and the controller lease. It checks the built-in cooldown and cap, records history and logs `[DRY-RUN] Would execute remediation`, but it does not call the strategy. The detector's dispatch policy also runs, so dry run uses up `maxAttemptsPerIncident` and starts the `cooldown` timer.

## Controller coordination

With `remediation.coordination.enabled: true`, the agent asks the controller for a lease before every strategy attempt. The lease client is built once at startup.

| Field | Default | Meaning |
|---|---|---|
| `controllerURL` | required | Base URL. Must start with `http://` or `https://`. |
| `leaseTimeout` | `5m` | Lease duration the agent requests. |
| `requestTimeout` | `10s` | HTTP timeout per request. |
| `maxRetries` | `3` | Total tries when the controller is unreachable or returns an error. |
| `retryInterval` | `5s` | Wait between tries. |
| `fallbackOnUnreachable` | `false` | When true and every try fails, the agent proceeds without a lease. This lets every node remediate at once while the controller is down. |

The agent sends `POST {controllerURL}/api/v1/leases` and, after the attempt, releases the lease with `DELETE {controllerURL}/api/v1/leases/{id}`. A `429` or `409` reply is a denial: the attempt fails, but not as a circuit breaker failure. If the controller cannot be reached after all tries, the attempt fails as a circuit breaker failure.

The controller denies a lease when:

| Rule | Controller setting | Default |
|---|---|---|
| Too many active leases cluster-wide | `coordination.maxConcurrentRemediations` | 3 |
| The node already holds an active lease | none | |
| The node's last lease completed too recently (needs controller storage) | `coordination.cooldownPeriod` | 10m |

The node cooldown applies per strategy attempt. With coordination on, a fallback strategy in the same dispatch is denied for `cooldownPeriod` after the first strategy's lease is released.

See [controller-deployment.md](controller-deployment.md) for running the controller.

## Strategies

At startup, when `remediation.enabled` is true, the agent registers six strategies. It registers `node-reboot` and `pod-delete` only when it can build a Kubernetes client (from `settings.kubeconfig`, or in-cluster) and knows its node name. Without them, a monitor that names those strategies fails with `unknown remediator type`.

| Strategy | Action | Parameter | Built-in cooldown | Destructive |
|---|---|---|---|---|
| `systemd-restart` | `systemctl restart <service>` | `service` | 5m | no |
| `custom-script` | Runs a script | `scriptPath`, `args` | 5m | depends on the script |
| `flush-dns` | `resolvectl flush-caches`, falling back to `systemd-resolve --flush-caches` | none | 3m | no |
| `restart-interface` | `ip link set <iface> down`, wait 500ms, `ip link set <iface> up` | `interface` | 3m | drops traffic on that interface briefly |
| `reset-routing` | `ip route flush cache` | none | 5m | no |
| `flush-ipv6-route` | `ip -6 route flush cache` | none | 3m | no |
| `node-reboot` | Cordon, drain, `systemctl reboot` | none | 30m | **yes** |
| `pod-delete` | Deletes one pod | pod namespace and name | 5m | **yes** |

### systemd-restart

The strategy restarts the unit named in `service`. It enters the host with `nsenter --target 1 --mount --uts --ipc --net --pid -- systemctl restart <service>` when three things hold: PID 1 is systemd, the container is in a different mount namespace from PID 1, and `nsenter` is on the PATH. Otherwise it runs `systemctl` directly. Seeing the host's PID 1 requires `hostPID: true`.

If `systemctl` prints `Running in chroot, ignoring`, the attempt fails instead of reporting success.

With `requireServiceEnabled: true`, the strategy first runs `systemctl is-enabled <service>` and refuses unless it prints `enabled`. On an RKE2 server node `rke2-agent.service` is installed but disabled, and restarting it would start a second kubelet.

### custom-script

The strategy runs `scriptPath` with `args` inside the node-doctor container. The path must be absolute, contain no `..`, and point to an existing regular file with an execute bit. The working directory is the script's directory and the timeout is 5 minutes. A non-zero exit or a timeout is a failure. Stdout and stderr go to the agent log.

The script gets the agent's environment plus:

| Variable | Value |
|---|---|
| `PROBLEM_TYPE` | Strategy name (`custom-script`) |
| `PROBLEM_RESOURCE` | Triggering condition type |
| `PROBLEM_MESSAGE` | Condition message |
| `PROBLEM_SEVERITY` | Always `Warning` |
| `PROBLEM_DETECTED_AT` | Condition transition time, RFC 3339 |
| `PROBLEM_META_SOURCE` | Monitor name |
| `PROBLEM_META_CONDITION` | Condition type |
| `PROBLEM_META_REASON` | Condition reason |
| `PROBLEM_META_SCRIPTPATH`, `PROBLEM_META_ARGS` | The script path, and the args as a JSON array |

### Network strategies

`flush-dns`, `restart-interface`, `reset-routing` and `flush-ipv6-route` run `resolvectl` or `ip` in the container, without `nsenter`. They act on the host network only when the pod uses `hostNetwork: true`. The shipped image (debian:12-slim plus the `systemd` package) does not install `iproute2`.

`reset-routing` flushes the IPv4 route cache only. It does not change or rebuild the routing table. If the flush fails, it logs a warning and still reports success.

`restart-interface` checks with `ip link show` that the interface exists before it bounces it.

### node-reboot

This strategy reboots the node the agent runs on, in three steps:

1. **Cordon.** Sets `spec.unschedulable: true`. A node that is already cordoned is left as is. If the cordon fails, the strategy stops and the node is not rebooted.
2. **Drain.** Evicts each pod on the node with a 30s grace period. It skips mirror (static) pods, DaemonSet pods, and node-doctor's own pod (from the `POD_NAME` and `POD_NAMESPACE` env vars). When an eviction is refused, for example by a PodDisruptionBudget, it deletes the pod instead. Errors are logged and the drain continues. The whole drain is limited to 2 minutes.
3. **Reboot.** Runs `systemctl reboot` in the container with a 30s timeout.

The strategy never uncordons the node. In dry run it reads the node and its pods and logs what it would cordon, evict and skip.

### pod-delete

This strategy deletes one pod with a 30s grace period. It reads the target from the `namespace` and `pod` keys of the dispatch metadata. It refuses mirror pods and node-doctor's own pod, and treats a pod that is already gone as success. Missing metadata is an error.

## Remediation history

The health server serves the in-memory history at `GET /remediation/history`, on TCP port 8080 and on the unix socket `/var/run/node-doctor/health.sock`. History is lost when the pod restarts.

| Query | Returns |
|---|---|
| none | Latest 100 records |
| `limit=N` | Latest N records, at most 1000 |
| `limit=0` or negative | Every record kept |

When remediation is disabled the endpoint returns `503` with `{"error": "Remediation history not available"}`. Otherwise it returns `200` with the oldest record first:

```json
{
  "count": 1,
  "limit": 100,
  "history": [
    {
      "remediatorType": "systemd-restart",
      "problem": {
        "Type": "systemd-restart",
        "Resource": "KubeletUnhealthy",
        "Severity": "Warning",
        "Message": "Kubelet has failed 3 consecutive health checks (threshold: 3)",
        "DetectedAt": "2026-10-24T10:00:00Z",
        "Metadata": {
          "source": "kubelet-health",
          "condition": "KubeletUnhealthy",
          "reason": "RepeatedHealthCheckFailures",
          "service": "rke2-agent",
          "requireEnabled": "true"
        }
      },
      "startTime": "2026-10-24T10:00:01Z",
      "endTime": "2026-10-24T10:00:04Z",
      "duration": "3.2s",
      "success": true
    }
  ]
}
```

Every attempt is recorded, including attempts refused by the circuit breaker, a rate limit or a lease denial. `error` appears only on failures. The `problem` keys are capitalized because the `Problem` type has no JSON tags.

The agent does not create Kubernetes Events for remediation attempts.

```bash
kubectl -n node-doctor port-forward <pod> 8080:8080
curl -s 'localhost:8080/remediation/history?limit=20'
```

## Config reload

A ConfigMap edit applies to remediation as follows. See [architecture.md](architecture.md#config-hot-reload) for the reload lifecycle.

| Change | Result |
|---|---|
| `remediation.dryRun`, `maxRemediationsPerHour`, `circuitBreaker.*` | Applied in place. Rate limit and breaker state are kept. |
| `remediation.enabled` false to true | Restart required. A `ConfigReloadRestartRequired` event says so. |
| `remediation.coordination.enabled` or `controllerURL` | Restart required, reported the same way. |
| `remediation.maxRemediationsPerMinute` alone | Not detected. It is applied only when a field in the first row also changes. |
| `settings.dryRunMode` alone | Not detected. |
| A monitor's `remediation` block alone | Not detected. The monitor keeps its old policy until its `type`, `enabled`, `interval`, `timeout` or `config` also changes. |

Strategies copy the dry-run flag when they are registered at startup. If the agent started in dry run and a reload turns dry run off, the registry calls the strategies, but they still only log `DRY-RUN` and report success. Turning dry run off needs a restart.

## Known limitations

| Limitation | Effect |
|---|---|
| No config field sets the `pod-delete` target, and the detector does not add `namespace` or `pod` to the dispatch metadata. | Every `pod-delete` dispatch fails with missing metadata. |
| The controller wraps its lease reply in `{"success", "data", "timestamp"}`. The agent reads `approved` from the top level. | Against the shipped controller every lease reads as denied, and the granted lease is held until it expires. |
| `node-reboot` runs `systemctl reboot` without `nsenter`. | It may not reach the host's systemd from inside the container. |
| The built-in 3-attempt cap never resets. | A strategy stops acting on a condition type after 3 real attempts, until the pod restarts. |

## Example: guarded rke2-agent restart

The Helm values `monitors.kubelet.remediation` render this block on the `kubelet-health` monitor. It restarts `rke2-agent` once per kubelet incident, at most every 30 minutes, only when the unit is enabled and the disk write probe passed in the last 3 minutes.

```yaml
remediation:
  enabled: true
  strategy: systemd-restart
  service: rke2-agent
  requireServiceEnabled: true
  triggerConditions:
    - KubeletUnhealthy
  maxAttemptsPerIncident: 1
  cooldown: 30m
  preconditions:
    - type: ReadonlyFilesystem
      status: "False"
      monitor: disk-health
      maxAge: 3m
```

The chart fails to render the precondition unless `monitors.disk.enabled` and `monitors.disk.writeProbe.enabled` are true.
