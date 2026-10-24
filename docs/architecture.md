# Architecture

Node Doctor is a per-node agent that runs health checks, publishes the results to Kubernetes, Prometheus and HTTP endpoints, and can run repair actions. An optional cluster controller collects reports from every agent and hands out remediation leases.

## Binaries

| Binary | Runs as | Job |
|---|---|---|
| `node-doctor` | DaemonSet, one pod per node | Monitors, detector, exporters, health server, remediation |
| `node-doctor-controller` | Deployment | Aggregates agent reports, correlates problems across nodes, grants remediation leases. See [controller-deployment.md](controller-deployment.md). |
| `overlay-test-server` | DaemonSet | Answers HTTP probes on the pod network so the CNI monitor can test overlay connectivity |

## Agent data flow

```text
 monitor ─┐
 monitor ─┼─► fan-in ─► status channel ─► detector (one goroutine, per status):
 monitor ─┘   (one per    (buffer 1000)      1. replace with MonitorBlocked if a dependency is unhealthy
               monitor)                      2. cache as the monitor's latest status
                                             3. ExportStatus to every exporter, in order
                                             4. evaluate remediation ─► remediator registry
                                                   │
             exporters: health server, Kubernetes, HTTP, Prometheus
```

Each monitor runs its own goroutine and sends a `Status` on its own channel. A fan-in goroutine per monitor copies statuses into one shared channel. If the shared channel stays full for 5 seconds, the fan-in logs a warning and drops that status.

One goroutine processes the shared channel. Export and remediation run inline, so a slow exporter call or a long remediation delays every later status. Exporters see every status, healthy or not. The detector only calls `ExportStatus`; it never calls `ExportProblem`.

Monitor types and their conditions are in [monitors.md](monitors.md). Configuration is in [configuration.md](configuration.md). Metric names are in [metrics.md](metrics.md). Remediation is in [remediation.md](remediation.md).

## Interfaces

These are the contracts in `pkg/types`.

```go
type Monitor interface {
    Start() (<-chan *Status, error)
    Stop()
}

type Exporter interface {
    ExportStatus(ctx context.Context, status *Status) error
    ExportProblem(ctx context.Context, problem *Problem) error
}

type ReloadableExporter interface {
    Exporter
    Reload(config interface{}) error
    IsReloadable() bool
}

type Remediator interface {
    CanRemediate(problem Problem) bool
    Remediate(ctx context.Context, problem Problem) error
    GetCooldown() time.Duration
}

type Status struct {
    Source     string
    Events     []Event
    Conditions []Condition
    Timestamp  time.Time
    Metadata   map[string]interface{}
}

type Condition struct {
    Type       string
    Status     ConditionStatus // "True", "False", "Unknown"
    Transition time.Time
    Reason     string
    Message    string
}
```

`Source` is the monitor name from the config. The CNI monitor attaches peer latency data to `Status.Metadata`, which the Prometheus exporter reads.

## Startup order

1. Load the config file. Normalize it: add default monitors for registered types the file omits, apply command-line overrides (`-debug`, `-dry-run`, `-log-level`, `-log-format`, `-enable-profiling`), apply defaults. Validate.
2. Initialize logging.
3. If `remediation.enabled`, build the remediator registry, register strategies, and wire the controller lease client.
4. Start the health server (TCP and unix socket).
5. Create and start the Kubernetes, HTTP and Prometheus exporters that are enabled. An exporter that fails to start is logged and skipped. With no exporter at all, a no-op exporter is used.
6. Create the detector and start it: start the config watcher (unless `reload.enabled: false`), then start monitors in dependency order (`dependsOn`), then start the status loop.
7. Exit with an error if no configured monitor started.

On SIGINT or SIGTERM the agent cancels its context and stops the exporters.

## Monitor dependencies

A monitor can list other monitor names in `dependsOn`. Monitors start in dependency order, and a cycle fails startup.

When a dependency's latest status has any condition with status `False`, or a `MonitorBlocked` condition, the detector replaces the dependent's status with a single condition `MonitorBlocked=Unknown` (reason `DependencyUnhealthy`). Blocking is transitive. A dependency that has not reported yet does not block.

## Exporters

| Exporter | Enabled by | Output |
|---|---|---|
| Health server | always | `/healthz`, `/ready`, `/status`, `/remediation/history` on TCP 8080 and the unix socket |
| Kubernetes | `exporters.kubernetes.enabled` | Node conditions, Kubernetes Events, node annotations |
| HTTP | `exporters.http.enabled` | Webhook POSTs, and periodic reports to the controller (`exporters.http.controller`) |
| Prometheus | `exporters.prometheus.enabled` | `node_doctor_*` metrics on port 9100 by default (the Helm chart uses 9101) |

The health server is an exporter too. It keeps the last status it received for `/status`.

## Node condition ownership

The Kubernetes exporter writes only conditions whose type starts with `NodeDoctor`. It adds the prefix to every condition a monitor reports: `KubeletUnhealthy` becomes `NodeDoctorKubeletUnhealthy`. It never writes kubelet or control-plane conditions such as `Ready`, `MemoryPressure` or `DiskPressure`, even though it loads them from the node at startup. A custom condition in `exporters.kubernetes.conditions` whose type lacks the prefix is refused with a warning.

The exporter batches condition changes and patches the node every `updateInterval` (default 10s). It re-reads the node every `resyncInterval` (default 60s) and writes a `NodeDoctorHealthy` heartbeat every `heartbeatInterval` (default 5m).

A `NodeDoctor*` condition that no monitor refreshes for `conditionStaleTTL` (default 6m, at least 3 times `resyncInterval`) is removed from the node. A condition loaded at startup gets the full TTL from process start before it expires. `NodeDoctorHealthy` never expires.

When a reload removes a monitor, the detector removes the node conditions of that monitor type, for the types it has a mapping for.

## Config hot reload

The agent watches its config file and applies changes without a restart where it can. Reload is on unless `reload.enabled: false`.

```text
fsnotify write/create on the file or the ConfigMap ..data symlink
  └─ debounce (reload.debounceInterval, default 500ms)
       └─ load → normalize (same as startup) → validate
            └─ ComputeConfigDiff + ClassifyReload
                 ├─ restart-required changes → ConfigReloadRestartRequired event
                 ├─ no diff → stop here (adopt the new file as the baseline)
                 └─ apply: stop removed monitors → restart modified monitors
                           → start added monitors → Reload() exporters
                           → registry.ApplyConfig() → commit config, re-apply log level/format
```

`ClassifyReload` sorts each change into what the running process can apply and what it cannot.

| Change | Applied how |
|---|---|
| Monitor added, removed, or `type`, `enabled`, `interval`, `timeout`, `config` changed | Monitor stopped and recreated |
| Kubernetes `enabled`/`updateInterval`, Prometheus `enabled`/`port`/`path`, HTTP `enabled`/`workers`/`queueSize`/`timeout`/webhooks | `Reload()` on the running exporter |
| `remediation.dryRun`, `maxRemediationsPerHour`, `circuitBreaker` | `ApplyConfig()` on the registry |
| `settings.logLevel`, `settings.logFormat` | Re-applied, but only when another change in this table triggers the apply step |
| `settings.nodeName`, `logOutput`, `logFile` | Restart required |
| `features.enableProfiling`, `profilingPort` | Restart required |
| An exporter going from disabled to enabled | Restart required |
| `remediation.enabled` false to true | Restart required |
| `remediation.coordination.enabled` or `controllerURL` | Restart required |

Restart-required changes produce a `ConfigReloadRestartRequired` event listing each one. Changes in no row of the table are not detected: for example a monitor's `remediation` or `dependsOn` block, `remediation.maxRemediationsPerMinute`, or `settings.dryRunMode` on its own.

The apply step commits the new config only when it has no critical error. Critical errors are a monitor that fails to stop, build or start, an exporter whose `Reload()` fails, and a failed `ApplyConfig()`. On a critical error, changes made before the failure stay in effect and the agent emits `ReloadPartialFailure`.

Reload events go through the status channel with source `config-reload`, so the Kubernetes exporter turns them into Events. The coordinator emits `ConfigReloadStarted`, `ConfigValidationSucceeded` or `ConfigValidationFailed`, `ConfigReloadRestartRequired`, `ConfigReloadNoChanges`, `ConfigReloadFailed` and `ConfigReloadSucceeded`. The Prometheus exporter records each reload in `node_doctor_config_reloads_total` and related metrics.

## Health server and probes

The health server listens on two addresses with the same handlers:

- TCP `[::]:8080`, falling back to `0.0.0.0:8080` if the dual-stack bind fails
- the unix socket `/var/run/node-doctor/health.sock` (flag `-health-socket`)

It starts before the networked exporters, so probes do not wait on the API server. If one listener fails to bind, the server keeps the other. Startup fails only if both fail. The TCP port is fixed at 8080.

| Endpoint | Probe | Returns 503 when |
|---|---|---|
| `/healthz` | liveness | The server was marked unhealthy or a liveness check failed. The agent registers no liveness checks, so this returns 200 while the process serves HTTP. |
| `/ready` | readiness | No status has arrived yet, or any exporter failed 3 export attempts in a row. One successful export clears it. |
| `/status` | none | Never. Returns the last status received and uptime. |
| `/remediation/history` | none | Remediation is disabled. See [remediation.md](remediation.md#remediation-history). |

A failing exporter, for example one that cannot reach the API server, makes the pod NotReady but never fails liveness. The kubelet therefore does not restart the agent on the degraded nodes it is meant to observe.

The Helm chart uses exec probes. `node-doctor -healthcheck` and `node-doctor -healthcheck-ready` dial the unix socket, request `/healthz` or `/ready` with a 3s timeout, and exit 0 on a 2xx reply. On a `hostNetwork` node a foreign host process can own port 8080; the socket lives on a pod-local volume and cannot collide. The chart's Service sets `publishNotReadyAddresses` so Prometheus keeps scraping NotReady pods.

## CNI peer discovery and pruning

The CNI monitor (`network-cni-check`) probes other nodes to test cross-node networking.

**Discovery.** Every `refreshInterval` (default 5m) the monitor lists running pods in its namespace (default `node-doctor`) by label and skips pods on its own node. With `overlayTestEnabled` (default true) it lists the overlay-test pods (`app=node-doctor-overlay-test`) and probes their pod IPs over HTTP on port 8023. Otherwise it lists node-doctor pods (`app=node-doctor`) and probes their node IPs with ICMP. It also reads node zone labels, so peers in another zone can use looser latency thresholds. A `static` discovery method takes a fixed IP list instead.

**Failure handling.** If a pod list fails, discovery keeps serving the previous peer set. If no peers are known, the check reports `NoPeersFound` and leaves the previous peer state alone.

**Pruning.** The peer set from discovery is authoritative for each check. The monitor builds a fresh per-peer state map each cycle, so a removed, renamed or re-addressed peer disappears on the next check. The Prometheus exporter does the same for metrics: it deletes peer series that are not in the current set and counts additions and removals in `node_doctor_peer_set_churn_total`.

**Debounce.** A peer counts as high latency only after `highLatencyThreshold` consecutive slow checks (default 3). A peer counts as unreachable for alerting after `failureThreshold` consecutive failures (default 3).

## Controller

Agents send reports to the controller with the HTTP exporter: `exporters.http.controller.url` receives a POST every `interval` (default 30s). The controller stores reports in SQLite, correlates problems that appear on many nodes, and serves its API under `/api/v1/` (reports, cluster status and problems, nodes, correlations, leases), plus `/healthz`, `/readyz` and `/metrics`.

Agents with `remediation.coordination.enabled` ask the controller for a lease before each remediation attempt. The lease rules are in [remediation.md](remediation.md#controller-coordination). Deployment is in [controller-deployment.md](controller-deployment.md).
