# Configuration Reference

The node-doctor agent reads one YAML or JSON file of kind `NodeDoctorConfig`. This page lists every field the agent parses, its default, and its validation rules. The schema lives in `pkg/types/config.go`.

Related pages:

- [monitors.md](monitors.md): the `config` block of each monitor type.
- [remediation.md](remediation.md): what each remediation strategy does on the node.
- [metrics.md](metrics.md): the Prometheus metrics the agent and controller export.
- [config/examples/](../config/examples/): complete example files. [config/node-doctor.yaml](../config/node-doctor.yaml) is a complete config with comments. The Helm chart renders its own from `helm/node-doctor/values.yaml`.

## Loading

### File search path

With `-config`, the agent loads that file. Without it, the agent uses the first file that exists:

1. `/etc/node-doctor/config.yaml`
2. `/etc/node-doctor/config.yml`
3. `./config.yaml`
4. `./config.yml`

If none exists, the agent exits with an error. There is no built-in fallback config.

Files ending in `.yaml` or `.yml` are parsed as YAML, `.json` as JSON. Any other extension is tried as YAML, then as JSON.

### Load order

1. Read the file and expand environment variables in the raw text.
2. Parse YAML or JSON.
3. Expand environment variables again in selected string fields (see below).
4. Apply defaults and validate (`pkg/util/config.go`).
5. Add a default entry for every monitor type that has one and is missing from the file (see [Default monitors](#default-monitors)).
6. Apply CLI overrides (`-debug`, `-log-level`, `-log-format`, `-dry-run`).
7. Apply defaults again and validate against the monitor registry. Unknown monitor types and `dependsOn` cycles fail here.

A hot reload runs the same steps, plus the extra checks in [Reload](#reload).

### Environment variables

The agent calls Go's `os.ExpandEnv` on the whole file before parsing. This means:

- `$VAR` and `${VAR}` both expand anywhere in the file, including in numbers and booleans (`port: ${METRICS_PORT}`).
- An unset variable expands to an empty string. A required field that becomes empty fails validation.
- Shell defaults such as `${VAR:-default}` are not supported. They expand to an empty string.
- There is no escape. `$$` becomes empty, and `$` followed by a letter, digit or `_` is read as a variable. A `$` at the end of a string or before a quote, `)` or space is kept. Check regex patterns in monitor configs for this.

After parsing, the agent expands these string fields a second time: `settings.nodeName`, `settings.kubeconfig`, `settings.logFile`, every string in each monitor's `config` map, monitor `remediation.scriptPath` and `args`, `exporters.kubernetes.namespace` and `annotations`, `exporters.http.headers`, each webhook's `url`, `auth` and `headers`, and `exporters.prometheus.bindAddress`, `namespace`, `subsystem` and `labels`.

The DaemonSet sets `NODE_NAME` from the pod's `spec.nodeName`, so `settings.nodeName: ${NODE_NAME}` is the usual setting. The `pod-delete` and `node-reboot` remediators read `POD_NAME` and `POD_NAMESPACE` directly.

## CLI flags

Flags are defined in `cmd/node-doctor/main.go`. Go flags accept both `-flag` and `--flag`.

| Flag | Default | Effect |
|------|---------|--------|
| `-config` | `""` | Config file path. Empty means use the [search path](#file-search-path). |
| `-validate-config` | `false` | Load, default and validate the config (including the registry check), then exit. Exit code 1 on failure. |
| `-dump-config` | `false` | Print the effective config as JSON after defaults and overrides, then exit. |
| `-list-monitors` | `false` | Print the registered monitor types and exit. |
| `-version` | `false` | Print version, commit, build time, Go version and OS/arch, then exit. |
| `-debug` | `false` | Set `settings.logLevel` to `debug`. |
| `-log-level` | `""` | Override `settings.logLevel`. Wins over `-debug`. |
| `-log-format` | `""` | Override `settings.logFormat`. |
| `-dry-run` | `false` | Set `settings.dryRunMode` and `remediation.dryRun` to `true`. |
| `-enable-profiling` | `false` | Sets `features.enableProfiling`. No effect: the agent has no pprof server. |
| `-profiling-port` | `6060` | Sets `features.profilingPort` when `-enable-profiling` is set. No effect. |
| `-health-socket` | `/var/run/node-doctor/health.sock` | Unix socket that the health server listens on and the probe flags dial. |
| `-healthcheck` | `false` | GET `/healthz` over the health socket. Exit 0 on a 2xx response, 1 otherwise. Does not load the config. |
| `-healthcheck-ready` | `false` | Same as `-healthcheck`, for `/ready`. |

The agent reapplies CLI overrides on every hot reload, so a ConfigMap edit cannot undo them.

## Top-level structure

```yaml
apiVersion: node-doctor.io/v1alpha1   # required, any non-empty string
kind: NodeDoctorConfig                # required, exactly this value
metadata:
  name: node-doctor                   # required
settings: {}
monitors: []
exporters: {}
remediation: {}
reload: {}
features: {}
```

### metadata

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `name` | string | none | Required. A hot reload also requires a lowercase DNS-style name of at most 63 characters. |
| `namespace` | string | `""` | Parsed, not used. |
| `labels` | map | none | Parsed, not used. |

## settings

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `nodeName` | string | none | Required. Used in every condition, event, metric label and report. Changing it needs a restart. |
| `logLevel` | string | `info` | `debug`, `info`, `warn`, `error` or `fatal`. Applied on reload. |
| `logFormat` | string | `json` | `json` or `text`. Applied on reload. |
| `logOutput` | string | `stdout` | `stdout`, `stderr` or `file`. Changing it needs a restart. |
| `logFile` | string | `""` | Required when `logOutput: file`. Opened in append mode. Changing it needs a restart. |
| `updateInterval` | duration | `10s` | Must be positive. Parsed and validated, not used. Use `exporters.kubernetes.updateInterval`. |
| `resyncInterval` | duration | `60s` | Same as `updateInterval`. |
| `heartbeatInterval` | duration | `5m` | Same as `updateInterval`. Minimum `5s`. |
| `dryRunMode` | bool | `false` | Same effect as `remediation.dryRun`. Either one turns on dry run. |
| `kubeconfig` | string | `""` | Empty means in-cluster config. Used by the Kubernetes exporter and the `node-reboot` and `pod-delete` remediators. |
| `qps` | float | `50` | Kubernetes client QPS. Above 0, at most 10000. |
| `burst` | int | `100` | Kubernetes client burst. 1 to 100000. |

Durations use Go syntax: `500ms`, `30s`, `5m`, `1h`.

## monitors

Each entry runs one monitor. The `config` keys for each type are in [monitors.md](monitors.md). `-list-monitors` prints the registered types.

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `name` | string | none | Required and unique. A hot reload also requires a lowercase DNS-style name. |
| `type` | string | none | Required. Must be a registered type. |
| `enabled` | bool | `false` | Omitting it disables the monitor. |
| `interval` | duration | `30s` | See [Monitor interval minimums](#monitor-interval-minimums). |
| `timeout` | duration | `10s` | Must be less than `interval`. |
| `config` | map | none | Type-specific. See [monitors.md](monitors.md). |
| `dependsOn` | list of names | none | See [Dependencies](#dependencies). |
| `remediation` | object | none | See [Monitor remediation](#monitor-remediation). |

### Default monitors

These types have a built-in default entry. If the file has no monitor of that type, the agent adds the default with `enabled: true`.

| Type | Default name |
|------|--------------|
| `system-cpu` | `cpu-health` |
| `system-memory` | `memory-health` |
| `system-disk` | `disk-health` |
| `network-dns-check` | `dns-health` |
| `network-gateway-check` | `gateway-health` |
| `network-ip-forwarding` | `ip-forwarding-check` |
| `network-ipv6-sysctl` | `ipv6-sysctl-check` |
| `network-ipv6-route` | `ipv6-route-check` |
| `network-ipv6-neighbor` | `ipv6-neighbor-check` |
| `network-ipv6-firewall` | `ipv6-firewall-check` |

To turn one off, add an entry of that type with `enabled: false`. Do not give a monitor of another type one of these names, or the added default fails the duplicate-name check. Run `-dump-config` to see the final list.

### Monitor interval minimums

| Rule | Startup | Hot reload |
|------|---------|------------|
| Minimum `interval` | `1s` | `5s` |
| `timeout` | less than `interval` | less than `interval` |

A config with a monitor interval from 1s up to 5s starts, but every reload of it is rejected.

### Threshold validation

For every monitor, these keys in `config` must be numbers from 0 to 100 if present: `warningLoadFactor`, `criticalLoadFactor`, `warningThreshold`, `criticalThreshold`, `swapWarningThreshold`, `swapCriticalThreshold`, `inodeWarning`, `inodeCritical`. The monitor itself checks its other keys. See [monitors.md](monitors.md).

### Dependencies

`dependsOn` lists monitor names. Every name must exist, and cycles fail validation. A hot reload allows at most 20 entries per monitor.

When a dependency's latest status has any condition with status `False`, or the dependency is itself blocked, the agent replaces this monitor's status with one `MonitorBlocked=Unknown` condition (reason `DependencyUnhealthy`). A dependency that has not reported yet does not block. A monitor that reports its healthy state as `False` (for example `KubeletDown=False`) always blocks its dependents.

## Monitor remediation

A monitor's `remediation` block tells the agent what to run when the monitor reports a problem. It only runs when the global `remediation.enabled` is `true`. [remediation.md](remediation.md) describes each strategy.

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `enabled` | bool | `false` | When `false`, the block is not validated or used. |
| `strategy` | string | none | Required when enabled. See [Strategies](#strategies). |
| `service` | string | `""` | Required for `systemd-restart`. |
| `requireServiceEnabled` | bool | `false` | `systemd-restart` only. Refuse to act unless `systemctl is-enabled <service>` reports `enabled`, because restarting a disabled unit starts it. |
| `scriptPath` | string | `""` | Required for `custom-script`. Absolute path without `..`. |
| `args` | list of strings | none | Arguments for `custom-script`. |
| `interface` | string | `""` | Required for `restart-interface`, for example `eth0`. |
| `action` | string | `""` | Parsed, not used. |
| `strategies` | list | none | Ordered fallback list. See [Multiple strategies](#multiple-strategies). |
| `triggerConditions` | list of strings | none | Condition types that start an incident when `True`. See [Dispatch policy](#dispatch-policy). |
| `maxAttemptsPerIncident` | int | `0` | Dispatches per incident. `0` means unlimited. |
| `cooldown` | duration | none | Minimum time between two dispatches for the same monitor and condition, across incidents. |
| `preconditions` | list | none | All must hold, or the dispatch is skipped. See [Preconditions](#preconditions). |

### Strategies

These are the only accepted values:

| Strategy | Required field | Registered when |
|----------|----------------|-----------------|
| `systemd-restart` | `service` | always |
| `custom-script` | `scriptPath` | always |
| `flush-dns` | none | always |
| `restart-interface` | `interface` | always |
| `reset-routing` | none | always |
| `flush-ipv6-route` | none | always |
| `node-reboot` | none | the agent has a Kubernetes client (in-cluster or `settings.kubeconfig`) |
| `pod-delete` | none | the agent has a Kubernetes client |

A dispatch to a strategy that is not registered fails. `node-reboot` and `pod-delete` change the cluster; read [remediation.md](remediation.md) before you enable them.

### Dispatch policy

On each check, the agent looks at every condition the monitor reported:

- With `triggerConditions` set, only the listed condition types count, and status `True` is the problem state. Use this for monitors that report problems as `True`, such as `KubeletUnhealthy`.
- Without `triggerConditions`, every condition counts, and status `False` is the problem state. Do not use this rule on a monitor that reports its healthy state as `False` (for example `KubeletDown=False`), or the agent remediates on every passing check.

A condition in its problem state is an incident. The incident ends, and its attempt count resets, when the monitor reports the condition out of its problem state.

Before each dispatch the agent checks `maxAttemptsPerIncident`, then `cooldown`, then `preconditions`. An admitted dispatch counts as an attempt before it runs, so a failed or hung remediation still uses up the attempt. A failed precondition does not use up an attempt, and the agent checks it again on the next cycle.

The agent reads the policy fields from the monitor's top-level `remediation` block only. They are ignored inside `strategies`.

### Preconditions

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `type` | string | none | Required. Condition type as the monitor reports it, for example `ReadonlyFilesystem`. |
| `status` | string | none | Required. `"True"` or `"False"`. Quote it in YAML. |
| `monitor` | string | `""` | Only match this monitor's condition. Empty means any monitor. |
| `maxAge` | duration | none | Required and positive. The newest matching observation must be at most this old. |

The agent uses the newest observation of the condition. The precondition fails if the condition was never observed, is older than `maxAge`, or has another status.

The Helm chart renders this block when `monitors.kubelet.remediation.enabled` is `true`:

```yaml
monitors:
  - name: kubelet-health
    type: kubernetes-kubelet-check
    enabled: true
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

### Multiple strategies

When `strategies` has entries, the agent tries them in order and stops at the first success. The top-level `strategy`, `service` and related fields are then not dispatched, but validation still requires `strategy`. Each entry takes `strategy`, `service`, `scriptPath`, `args`, `interface` and `requireServiceEnabled`. An entry is only validated when it sets `enabled: true`. Nesting is limited to 10 levels.

```yaml
remediation:
  enabled: true
  strategy: systemd-restart
  service: containerd
  strategies:
    - enabled: true
      strategy: systemd-restart
      service: containerd
    - enabled: true
      strategy: custom-script
      scriptPath: /opt/node-doctor/fix-runtime.sh
      args: ["--force"]
```

## exporters

An exporter that is disabled at startup is never created. Enabling it later needs a restart.

### exporters.kubernetes

Writes node conditions, events and annotations to the API server.

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `enabled` | bool | `false` | |
| `updateInterval` | duration | `10s` | How often pending condition changes are written. |
| `resyncInterval` | duration | `60s` | How often the agent re-reads the node. |
| `heartbeatInterval` | duration | `5m` | How often the `NodeDoctorHealthy` heartbeat condition is refreshed. |
| `conditionStaleTTL` | duration | `6m` | The agent removes a `NodeDoctor*` condition that no monitor has refreshed for this long. Raised to at least 3 times `resyncInterval`. `NodeDoctorHealthy` never expires. |
| `namespace` | string | `""` | Namespace for events. Empty means `default`. A hot reload requires it to be set. |
| `conditions` | list | none | Conditions written at startup. See below. |
| `annotations` | list | none | Node annotations to set: `key` (required) and `value`. |
| `events.maxEventsPerMinute` | int | `0` | Event rate limit. `0` means the built-in limit of 10. |
| `events.deduplicationWindow` | duration | none | Identical events inside this window are dropped. Unset means `10m`. |
| `events.eventTTL` | duration | none | Validated, not used. |

The agent writes every monitor condition to the node with a `NodeDoctor` prefix: `KubeletDown` becomes `NodeDoctorKubeletDown`. It never writes, expires or removes a condition without that prefix.

Each `conditions` entry has `type` (required), `defaultStatus` (`True`, `False` or `Unknown`; any other value means `True`), `defaultReason` and `defaultMessage`. The `type` must start with `NodeDoctor`, or the agent refuses to write it.

### exporters.http

Sends status and problems as JSON to webhooks, and can report to the node-doctor controller.

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `enabled` | bool | `false` | At startup, needs at least one webhook or an enabled `controller`. A hot reload needs at least one webhook. |
| `workers` | int | `5` | Must be positive. |
| `queueSize` | int | `100` | Must be positive. |
| `timeout` | duration | `30s` | Default webhook timeout. |
| `retry.maxAttempts` | int | `3` | Default for webhooks. |
| `retry.baseDelay` | duration | `1s` | Must not exceed `maxDelay`. |
| `retry.maxDelay` | duration | `30s` | |
| `headers` | map | none | Parsed, not sent. Set headers per webhook. |
| `webhooks` | list | none | See below. |
| `controller` | object | none | See [exporters.http.controller](#exportershttpcontroller). |

Each webhook:

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `name` | string | `""` | Must be unique when set. |
| `url` | string | none | Required. Must start with `http://` or `https://`. |
| `timeout` | duration | parent `timeout` | |
| `retry` | object | parent `retry` | Missing fields come from the parent. |
| `headers` | map | none | Sent with each request. |
| `auth.type` | string | `none` | `none`, `bearer` or `basic`. |
| `auth.token` | string | `""` | Required for `bearer`. |
| `auth.username`, `auth.password` | string | `""` | Required for `basic`. |
| `sendStatus` | bool | see note | If `sendStatus` and `sendProblems` are both `false`, both default to `true`. |
| `sendProblems` | bool | see note | |

```yaml
exporters:
  http:
    enabled: true
    webhooks:
      - name: alerts
        url: ${ALERT_WEBHOOK_URL}
        sendStatus: false
        sendProblems: true
        auth:
          type: bearer
          token: ${ALERT_WEBHOOK_TOKEN}
```

#### exporters.http.controller

Sends a node report to the controller's `/api/v1/reports` endpoint at startup and then every `interval`. It runs inside the HTTP exporter, so `exporters.http.enabled` must also be `true`.

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `enabled` | bool | `false` | |
| `url` | string | none | Required when enabled. Must start with `http://` or `https://`. |
| `interval` | duration | `30s` | Time between reports. |
| `timeout` | duration | `10s` | Request timeout. |
| `auth` | object | `type: none` | Same fields as webhook `auth`. |
| `headers` | map | none | Sent with each request. |
| `retry.maxAttempts` | int | `3` | |
| `retry.baseDelay` | duration | `1s` | |
| `retry.maxDelay` | duration | `10s` | |

### exporters.prometheus

Serves metrics over HTTP. [metrics.md](metrics.md) lists them.

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `enabled` | bool | `false` | |
| `port` | int | `9100` | 1 to 65535, and at least 1024 on reload. The Helm chart and `deployment/` manifests set `9101`. |
| `bindAddress` | string | `::` | Dual-stack. Falls back to `0.0.0.0` if the IPv6 bind fails. |
| `path` | string | `/metrics` | Must start with `/`. The same server also answers `/health`. |
| `namespace` | string | `node_doctor` | Metric name prefix. Must match `^[a-zA-Z_:][a-zA-Z0-9_:]*$`. |
| `subsystem` | string | `""` | Added after the namespace. Same pattern. |
| `labels` | map | none | Constant labels added to every node-doctor metric. |

## remediation

Global remediation settings. When `enabled` is `false`, the agent creates no remediators and ignores every monitor's `remediation` block. Turning it on later needs a restart.

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `enabled` | bool | `false` | A hot reload rejects a config where a monitor has remediation enabled and this is `false`. |
| `dryRun` | bool | `false` | Log what would run, change nothing. `settings.dryRunMode` and `-dry-run` do the same. |
| `maxRemediationsPerHour` | int | `10` | Limit over a sliding hour, across all monitors. `0` is replaced by the default. |
| `maxRemediationsPerMinute` | int | `2` | Per-minute limit. `0` is replaced by the default. |
| `historySize` | int | `100` | Records kept for `/remediation/history`. |
| `circuitBreaker` | object | see below | |
| `coordination` | object | none | See [remediation.coordination](#remediationcoordination). |

### remediation.circuitBreaker

The breaker is on by default and applies from startup. After `threshold` consecutive failed remediations it opens and blocks all remediations for `timeout`. It then moves to half-open, lets remediations through, and closes after `successThreshold` successes. [metrics.md](metrics.md#remediation) lists the state metric.

| Field | Type | Default without the block | Default when the block is present | Notes |
|-------|------|---------------------------|-----------------------------------|-------|
| `enabled` | bool | on | on | `false` turns the breaker off. |
| `threshold` | int | `5` | `5` | Must be positive. |
| `timeout` | duration | `5m` | `30m` | Must be positive. |
| `successThreshold` | int | `2` | `2` | Must be positive. |

Set `timeout` explicitly. The two defaults differ because an absent block uses the remediator's built-in values (`pkg/remediators/registry.go`), while a present block is filled from `pkg/types/config.go`.

### remediation.coordination

The agent asks the controller for a lease before each remediation, so only a limited number of nodes remediate at the same time.

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `enabled` | bool | `false` | Changing it needs a restart. |
| `controllerURL` | string | none | Required when enabled. Must start with `http://` or `https://`. Changing it needs a restart. |
| `leaseTimeout` | duration | `5m` | Lease duration the agent requests. |
| `requestTimeout` | duration | `10s` | Timeout for each lease request. |
| `maxRetries` | int | `3` | Total lease request attempts. `0` is replaced by the default. |
| `retryInterval` | duration | `5s` | Wait between attempts. |
| `fallbackOnUnreachable` | bool | `false` | When `true` and the controller is unreachable, the agent remediates without a lease. |

Keep `fallbackOnUnreachable: false`. With `true`, a controller outage lets every node remediate at once.

If the lease client cannot be created, the agent logs a warning and remediates without coordination.

## reload

The agent watches the config file's directory, so it picks up ConfigMap updates without a restart.

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `enabled` | bool | `true` | `false` turns off the watcher. The agent logs a warning at startup. |
| `debounceInterval` | duration | `500ms` | Wait for file events to settle before reloading. |

A reload adds, removes and restarts changed monitors, and reconfigures exporters and remediation in place. The agent logs these changes as "RESTART REQUIRED". They only apply after a pod restart:

- `settings.nodeName`, `settings.logOutput`, `settings.logFile`
- enabling an exporter that was disabled at startup
- `remediation.enabled` from `false` to `true`
- `remediation.coordination.enabled` or `controllerURL`
- `features.enableProfiling` or `features.profilingPort`

The agent rejects a reload and keeps the running config if the new config fails startup validation or any of these extra checks (`pkg/reload/validator.go`):

| Check | Rule |
|-------|------|
| Monitors | at most 100 |
| Monitor `interval` | at least `5s` |
| `metadata.name`, monitor `name` | lowercase letters, digits, `-` and `.`, at most 63 characters |
| Enabled exporters | at least one |
| `exporters.kubernetes.namespace` | set, lowercase DNS-style name |
| `exporters.http.webhooks` | 1 to 50 |
| `exporters.prometheus.port` | 1024 to 65535 |
| Monitor `remediation` | `strategy` set, even when disabled; not enabled while `remediation.enabled` is `false` |
| Monitor `dependsOn` | at most 20 entries |

[metrics.md](metrics.md#config-reload) lists the reload metrics.

## features

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `enableProfiling` | bool | `false` | Parsed, not used. The agent has no pprof server. |
| `profilingPort` | int | `0` | Parsed, not used. |

## IPv6 / dual-stack

The agent's listeners bind `::` and fall back to `0.0.0.0` when the IPv6 bind fails:

- the health server on TCP port 8080 (address and port are fixed in code)
- the Prometheus exporter, set by `exporters.prometheus.bindAddress`

[monitors.md](monitors.md) documents the IPv6 monitors and the dual-stack options of the gateway and DNS monitors.

## Health endpoints

The agent always starts a health server (`pkg/health/server.go`) with two listeners:

| Listener | Address | Used by |
|----------|---------|---------|
| TCP | `[::]:8080`, falls back to `0.0.0.0:8080` | External checks. Best effort: if another process owns port 8080 on a host-network node, the agent logs a warning and keeps running. |
| Unix socket | `-health-socket`, default `/var/run/node-doctor/health.sock` | The `-healthcheck` and `-healthcheck-ready` exec probes. |

The health server starts before the networked exporters, so probes answer while the Kubernetes or HTTP exporter is still connecting.

| Path | Purpose | 200 when | 503 when |
|------|---------|----------|----------|
| `/healthz` | Liveness | The process is serving. | An internal health check fails. Downstream failures never cause a 503 here. |
| `/ready` | Readiness | At least one monitor has reported, and no exporter has failed 3 exports in a row. | No monitor has reported yet, or an exporter failed 3 exports in a row. One success clears it. |
| `/status` | Debugging | Always. JSON with `healthy`, `ready`, `uptime`, `lastUpdate` and the most recent status from any one monitor. | never |
| `/remediation/history` | Remediation records | JSON `{count, limit, history}`. `?limit=N` defaults to 100, maximum 1000. `0` or a negative value returns all records. | Remediation is disabled. |

Liveness and readiness are split on purpose. A broken API server or webhook makes the pod NotReady but never restarts the agent. A restart cannot fix the node, and would crash-loop the agent on the nodes it should observe.

The Helm chart uses exec probes:

```yaml
livenessProbe:
  exec:
    command: ["/usr/local/bin/node-doctor", "-healthcheck"]
readinessProbe:
  exec:
    command: ["/usr/local/bin/node-doctor", "-healthcheck-ready"]
```

The socket directory must be writable, for example an `emptyDir` mounted at `/var/run/node-doctor`.
