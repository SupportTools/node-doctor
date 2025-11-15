# Node Doctor Configuration Reference

**Source:** `pkg/types/config.go:1457`

## Overview

Node Doctor uses a declarative configuration system based on YAML with support for environment variable substitution, comprehensive validation, and dynamic updates. The configuration schema is defined in `pkg/types/config.go` and follows Kubernetes-style resource definitions.

## Configuration Sources and Precedence

Configuration is loaded from multiple sources in this order (later sources override earlier ones):

1. **Embedded Defaults**: Compiled into the binary (`pkg/types/config.go:12-38`)
2. **ConfigMap**: Mounted at `/etc/node-doctor/config.yaml`
3. **Environment Variables**: Override specific values via `${VAR}` syntax
4. **Command-Line Flags**: Highest precedence

## Configuration Schema

### Top-Level Structure

```yaml
apiVersion: node-doctor.io/v1alpha1  # Required
kind: NodeDoctorConfig               # Required
metadata:                            # Required
  name: string                       # Required
  namespace: string                  # Optional
  labels:                            # Optional
    key: value
settings:                            # Global settings
  # See Global Settings section
monitors:                            # List of monitor configurations
  - name: string
    # See Monitors section
exporters:                           # Exporter configurations
  # See Exporters section
remediation:                         # Remediation settings
  # See Remediation section
features:                            # Feature flags
  # See Features section
```

## Global Settings

**Source:** `pkg/types/config.go:110-139`

```yaml
settings:
  # Node identification
  nodeName: "${NODE_NAME}"           # Required, usually from downward API

  # Logging configuration
  logLevel: info                     # Default: "info"
  logFormat: json                    # Default: "json"
  logOutput: stdout                  # Default: "stdout"
  logFile: ""                        # Required if logOutput: file

  # Update intervals
  updateInterval: 10s                # Default: "10s"
  resyncInterval: 60s                # Default: "60s"
  heartbeatInterval: 5m              # Default: "5m"

  # Remediation master switches
  enableRemediation: true            # Default: false
  dryRunMode: false                  # Default: false

  # Kubernetes client configuration
  kubeconfig: ""                     # Default: "" (in-cluster)
  qps: 50                            # Default: 50 (max: 10000)
  burst: 100                         # Default: 100 (max: 100000)
```

### Global Settings Defaults

**Source:** `pkg/types/config.go:12-38`

| Field | Default | Valid Values | Description |
|-------|---------|--------------|-------------|
| `logLevel` | `info` | debug, info, warn, error, fatal | Logging verbosity |
| `logFormat` | `json` | json, text | Log output format |
| `logOutput` | `stdout` | stdout, stderr, file | Log destination |
| `updateInterval` | `10s` | > 0 | Kubernetes API update interval |
| `resyncInterval` | `60s` | > 0 | Full resynchronization interval |
| `heartbeatInterval` | `5m` | > 0 | Condition update interval |
| `qps` | `50` | > 0, ≤ 10000 | Kubernetes API QPS limit |
| `burst` | `100` | > 0, ≤ 100000 | Kubernetes API burst limit |

### Global Settings Validation

**Source:** `pkg/types/config.go:875-936`

- ✅ `nodeName` must not be empty
- ✅ `logLevel` must be one of: debug, info, warn, error, fatal
- ✅ `logFormat` must be one of: json, text
- ✅ `logOutput` must be one of: stdout, stderr, file
- ✅ `logFile` required when `logOutput: file`
- ✅ All intervals must be positive durations
- ✅ `heartbeatInterval` must be ≥ MinHeartbeatInterval (5 seconds)
- ✅ `qps` must be > 0 and ≤ 10000
- ✅ `burst` must be > 0 and ≤ 100000

## Monitor Configuration

**Source:** `pkg/types/config.go:141-166`

Each monitor follows this structure:

```yaml
monitors:
  - name: monitor-name               # Required, unique identifier
    type: monitor-type               # Required, from monitor registry
    enabled: true                    # Default: false
    interval: 30s                    # Default: "30s"
    timeout: 10s                     # Default: "10s"
    dependsOn: []                    # Optional, list of monitor names this depends on
    config:                          # Monitor-specific configuration
      # Fields vary by monitor type
    remediation:                     # Optional remediation config
      # See Remediation section
```

### Monitor Dependencies

**Source:** `pkg/types/config.go:169`

Monitors can declare dependencies on other monitors using the `dependsOn` field. This allows you to enforce execution order and prevent circular dependencies:

```yaml
monitors:
  - name: disk-health
    type: system-disk-check
    enabled: true

  - name: runtime-health
    type: kubernetes-runtime-check
    enabled: true
    dependsOn:
      - disk-health          # Runtime check depends on disk check

  - name: kubelet-health
    type: kubernetes-kubelet-check
    enabled: true
    dependsOn:
      - runtime-health       # Kubelet depends on runtime
      - disk-health          # Kubelet also depends on disk
```

**Dependency Validation** (`pkg/types/config.go:1478-1539`):
- All referenced monitors must exist in the configuration
- Circular dependencies are detected using depth-first search
- Self-dependencies are not allowed
- Dependency chains can be arbitrarily deep (no limit)

**Example Circular Dependency Detection:**
```yaml
# ❌ Invalid: Circular dependency
monitors:
  - name: monitor-a
    dependsOn: [monitor-b]
  - name: monitor-b
    dependsOn: [monitor-c]
  - name: monitor-c
    dependsOn: [monitor-a]    # Creates cycle: A → B → C → A
```

Error message: `circular dependency detected in monitors: monitor-a → monitor-b → monitor-c → monitor-a`

### Monitor Configuration Defaults

**Source:** `pkg/types/config.go:26-27`

| Field | Default | Validation |
|-------|---------|------------|
| `interval` | `30s` | Must be ≥ 1s (MinMonitorInterval) |
| `timeout` | `10s` | Must be > 0 and < interval |

### Monitor Interval Minimums

**Source:** `pkg/types/config.go:75-78`

To prevent system overload from excessive polling, Node Doctor enforces minimum interval thresholds:

| Constant | Value | Purpose |
|----------|-------|---------|
| `MinMonitorInterval` | 1 second | Minimum monitor check interval |
| `MinHeartbeatInterval` | 5 seconds | Minimum heartbeat update interval |
| `MinCooldownPeriod` | 10 seconds | Minimum remediation cooldown |

These conservative minimums ensure Node Doctor doesn't overwhelm the system with excessive API calls, disk I/O, or remediation attempts.

### Monitor Types

See [docs/monitors.md](./monitors.md) for detailed configuration of all 11 monitor types:

1. **System Monitors:** cpu-check, memory-check, disk-check
2. **Network Monitors:** dns-check, gateway-check, connectivity-check
3. **Kubernetes Monitors:** kubelet-check, apiserver-check, runtime-check, capacity-check
4. **Custom Monitors:** plugin-check, log-pattern-check

### Monitor Validation

**Source:** `pkg/types/config.go:953-996`

- ✅ `name` must not be empty and be unique across all monitors
- ✅ `type` must not be empty and be registered in monitor registry
- ✅ `interval` must be ≥ MinMonitorInterval (1 second)
- ✅ `timeout` must be positive and less than `interval`
- ✅ All `dependsOn` monitors must exist in configuration
- ✅ No circular dependencies in monitor dependency graph
- ✅ Threshold values must be in range 0-100 for percentage-based thresholds
- ✅ Monitor-specific config validated by monitor factory

### Threshold Validation

**Source:** `pkg/types/config.go:1541-1589`

Node Doctor validates all percentage-based threshold fields to ensure they are in the valid 0-100 range. The following threshold fields are automatically validated:

**System Monitor Thresholds:**
- `warningLoadFactor`, `criticalLoadFactor` - CPU load thresholds
- `warningThreshold`, `criticalThreshold` - Memory/disk usage thresholds
- `swapWarningThreshold`, `swapCriticalThreshold` - Swap usage thresholds
- `inodeWarning`, `inodeCritical` - Inode usage thresholds

**Example Validation:**
```yaml
monitors:
  - name: cpu-health
    type: system-cpu-check
    config:
      warningLoadFactor: 80    # ✅ Valid: 0-100
      criticalLoadFactor: 150  # ❌ Invalid: exceeds 100
```

Error message: `monitor "cpu-health": threshold "criticalLoadFactor" value 150.00 must be in range 0-100`

**Type-Safe Validation:**
- Handles multiple numeric types: `int`, `int32`, `int64`, `float32`, `float64`
- Non-threshold fields are ignored (e.g., strings, booleans)
- Validation runs automatically during config parsing

### ValidateWithRegistry

**Source:** `pkg/types/config.go:1591-1616`

The `ValidateWithRegistry()` method performs enhanced validation that requires knowledge of registered monitor types:

```go
// Validate with monitor registry
err := config.ValidateWithRegistry(monitors.DefaultRegistry)
```

**Additional Validations:**
- ✅ All monitor types are registered in the monitor registry
- ✅ No circular dependencies exist in monitor dependency graph
- ✅ All standard validations from `Validate()` method

This is typically called during application startup after all monitors have self-registered via `func init()`.

## Monitor Remediation Configuration

**Source:** `pkg/types/config.go:168-207`

```yaml
remediation:
  enabled: true                      # Default: false
  strategy: systemd-restart          # Required if enabled

  # Strategy-specific fields
  service: kubelet                   # For systemd-restart
  scriptPath: /path/to/script.sh     # For custom-script (absolute path)
  args: []                           # Script arguments

  # Timing and retry
  cooldown: 5m                       # Default: "5m"
  maxAttempts: 3                     # Default: 3

  # Graceful stop configuration
  gracefulStop: false                # Default: false
  waitTimeout: 30s                   # Default: 0 (not set)

  # Priority for multi-step remediation
  priority: 1                        # Default: 0

  # Nested strategies for complex remediation
  strategies:                        # Optional, max depth: 10
    - strategy: systemd-restart
      # Same fields as above
```

### Remediation Strategy Types

**Source:** `pkg/types/config.go:68-74`

| Strategy | Required Fields | Description |
|----------|----------------|-------------|
| `systemd-restart` | `service` | Restart systemd service |
| `custom-script` | `scriptPath`, `args` (optional) | Execute custom remediation script |
| `node-reboot` | none | Reboot the node (use with caution) |
| `pod-delete` | none | Delete problematic pods |

### Remediation Validation

**Source:** `pkg/types/config.go:966-1022`

- ✅ `strategy` must be one of: systemd-restart, custom-script, node-reboot, pod-delete
- ✅ `systemd-restart`: `service` field required
- ✅ `custom-script`: `scriptPath` required, must be absolute path, cannot contain `..`
- ✅ `cooldown` must be positive
- ✅ `maxAttempts` must be positive
- ✅ `waitTimeout` must be positive if specified
- ✅ Nested `strategies` limited to 10 levels deep (prevents recursion issues)

### Remediation Defaults

**Source:** `pkg/types/config.go:28-29`

| Field | Default |
|-------|---------|
| `cooldown` | `5m` |
| `maxAttempts` | `3` |

## Exporter Configuration

### Kubernetes Exporter

**Source:** `pkg/types/config.go:216-240`

```yaml
exporters:
  kubernetes:
    enabled: true                    # Default: false

    # Update intervals
    updateInterval: 10s              # Default: "10s"
    resyncInterval: 60s              # Default: "60s"
    heartbeatInterval: 5m            # Default: "5m"

    # Event namespace
    namespace: default               # Default: "" (current namespace)

    # Custom node conditions
    conditions:
      - type: NodeDoctorHealthy      # Required
        defaultStatus: "True"        # Optional
        defaultReason: "Running"     # Optional
        defaultMessage: "Healthy"    # Optional

    # Node annotations to manage
    annotations:
      - key: node-doctor.io/status  # Required
        value: "healthy"             # Required

    # Event configuration
    events:
      maxEventsPerMinute: 10         # Default: 0 (unlimited)
      eventTTL: 1h                   # Default: 0 (use K8s default)
      deduplicationWindow: 5m        # Default: 0 (no deduplication)
```

#### Kubernetes Exporter Validation

**Source:** `pkg/types/config.go:1024-1067`

- ✅ All intervals must be positive
- ✅ `conditions[].type` must not be empty
- ✅ `annotations[].key` must not be empty
- ✅ `events.maxEventsPerMinute` must be non-negative
- ✅ `events.eventTTL` must be positive if specified
- ✅ `events.deduplicationWindow` must be positive if specified

### HTTP Exporter

**Source:** `pkg/types/config.go:265-279`

```yaml
exporters:
  http:
    enabled: true                    # Default: false

    # Worker pool configuration
    workers: 5                       # Default: 5
    queueSize: 100                   # Default: 100

    # Default timeout for all webhooks
    timeout: 30s                     # Default: "30s"

    # Default retry configuration
    retry:
      maxAttempts: 3                 # Default: 3
      baseDelay: 1s                  # Default: "1s"
      maxDelay: 30s                  # Default: "30s"

    # Default headers for all webhooks
    headers:
      User-Agent: node-doctor/v1
      Content-Type: application/json

    # Webhook endpoints
    webhooks:
      - name: slack                  # Optional, must be unique
        url: https://hooks.slack.com/services/XXX  # Required

        # Authentication
        auth:
          type: bearer               # Default: "none"
          token: "${SLACK_TOKEN}"    # For bearer auth
          # OR
          type: basic
          username: user
          password: "${PASSWORD}"

        # Per-webhook timeout (overrides default)
        timeout: 10s                 # Optional

        # Per-webhook retry config (overrides default)
        retry:
          maxAttempts: 5
          baseDelay: 2s
          maxDelay: 1m

        # Per-webhook headers (merged with defaults)
        headers:
          X-Custom-Header: value

        # Control what gets sent
        sendStatus: true             # Default: true
        sendProblems: true           # Default: true
```

#### HTTP Exporter Validation

**Source:** `pkg/types/config.go:1069-1151`

- ✅ `workers` must be positive
- ✅ `queueSize` must be positive
- ✅ `timeout` must be positive
- ✅ `retry.maxAttempts` must be non-negative
- ✅ `retry.baseDelay` must be positive and ≤ `maxDelay`
- ✅ At least one webhook required when HTTP exporter enabled
- ✅ Webhook names must be unique
- ✅ `webhooks[].url` required and must start with http:// or https://
- ✅ `webhooks[].auth.type` must be one of: none, bearer, basic
- ✅ Bearer auth requires `token` field
- ✅ Basic auth requires `username` and `password` fields
- ✅ At least one of `sendStatus` or `sendProblems` must be true

### Prometheus Exporter

**Source:** `pkg/types/config.go:323-331`

```yaml
exporters:
  prometheus:
    enabled: true                    # Default: false
    port: 9100                       # Default: 9100
    path: /metrics                   # Default: "/metrics"
    namespace: node_doctor           # Default: "node_doctor"
    subsystem: ""                    # Default: ""
    labels:
      environment: production
      cluster: main
```

#### Prometheus Exporter Validation

**Source:** `pkg/types/config.go:1192-1219`

- ✅ `port` must be in range 1-65535
- ✅ `path` must start with `/`
- ✅ `namespace` must match pattern: `^[a-zA-Z_:][a-zA-Z0-9_:]*$`
- ✅ `subsystem` must match pattern: `^[a-zA-Z_:][a-zA-Z0-9_:]*$`

## Remediation Configuration

**Source:** `pkg/types/config.go:333-377`

```yaml
remediation:
  # Master switches
  enabled: true                      # Default: false
  dryRun: false                      # Default: false

  # Safety limits
  maxRemediationsPerHour: 10         # Default: 10
  maxRemediationsPerMinute: 2        # Default: 2
  cooldownPeriod: 5m                 # Default: "5m"
  maxAttemptsGlobal: 3               # Default: 3

  # Circuit breaker settings
  circuitBreaker:
    enabled: true                    # Default: false
    threshold: 5                     # Default: 5 (failures before open)
    timeout: 30m                     # Default: "30m" (time before retry)
    successThreshold: 2              # Default: 2 (successes before close)

  # Remediation history tracking
  historySize: 100                   # Default: 100

  # Problem-specific overrides
  overrides:
    - problem: kubelet-unhealthy    # Required, problem type name
      cooldown: 3m                   # Optional
      maxAttempts: 5                 # Optional
      circuitBreakerThreshold: 10    # Optional
```

### Remediation Defaults

**Source:** `pkg/types/config.go:28-35`

| Field | Default | Description |
|-------|---------|-------------|
| `cooldownPeriod` | `5m` | Minimum time between remediation attempts |
| `maxAttemptsGlobal` | `3` | Maximum remediation attempts per problem |
| `maxRemediationsPerHour` | `10` | Rate limit per hour |
| `maxRemediationsPerMinute` | `2` | Rate limit per minute |
| `circuitBreakerThreshold` | `5` | Failures before circuit opens |
| `circuitBreakerTimeout` | `30m` | Wait time before retrying after circuit opens |
| `historySize` | `100` | Number of remediation records to keep |

### Circuit Breaker States

The circuit breaker protects against cascading failures:

- **Closed**: Normal operation, remediations allowed
- **Open**: Too many failures, remediations blocked until timeout
- **Half-Open**: Testing if system recovered, limited remediations allowed

After `threshold` failures → Open state → Wait `timeout` → Half-Open state → After `successThreshold` successes → Closed state

### Remediation Validation

**Source:** `pkg/types/config.go:1221-1282`

- ✅ `maxRemediationsPerHour` must be non-negative
- ✅ `maxRemediationsPerMinute` must be non-negative
- ✅ `cooldownPeriod` must be positive
- ✅ `maxAttemptsGlobal` must be positive
- ✅ `historySize` must be positive
- ✅ If circuit breaker enabled:
  - `threshold` must be positive
  - `timeout` must be positive
  - `successThreshold` must be positive
- ✅ Override problem names must be unique
- ✅ Override cooldown must be positive if specified
- ✅ Override values must be non-negative

## Feature Flags

**Source:** `pkg/types/config.go:378-385`

```yaml
features:
  enableMetrics: true                # Default: true
  enableProfiling: false             # Default: false
  profilingPort: 6060                # Default: 0
  enableTracing: false               # Default: false
  tracingEndpoint: ""                # Default: ""
```

## Environment Variable Substitution

**Source:** `pkg/types/config.go:1287-1456`

Node Doctor supports environment variable substitution using `${VAR}` or `$VAR` syntax. Substitution is performed recursively through all configuration fields.

### Substitution Examples

```yaml
settings:
  nodeName: "${NODE_NAME}"           # ✅ Required pattern
  logFile: "/var/log/${NAMESPACE}/node-doctor.log"

exporters:
  kubernetes:
    namespace: "${POD_NAMESPACE}"
    annotations:
      - key: node-doctor.io/version
        value: "${VERSION}"
      - key: node-doctor.io/build
        value: "${BUILD_ID}"

  http:
    webhooks:
      - name: slack
        url: "${SLACK_WEBHOOK_URL}"
        auth:
          type: bearer
          token: "${SLACK_TOKEN}"

monitors:
  - name: custom-check
    type: custom-plugin-check
    config:
      path: "${PLUGIN_DIR}/check.sh"
      env:
        API_KEY: "${API_KEY}"
        ENDPOINT: "${ENDPOINT}"

remediation:
  - scriptPath: "${REMEDIATION_DIR}/fix.sh"
    args:
      - "--cluster=${CLUSTER_NAME}"
```

### Substitution Order

**Source:** `pkg/types/config.go:1289-1311`

Substitution is performed in this order:

1. Global settings
2. Monitors (including nested config maps)
3. Kubernetes exporter
4. HTTP exporter (including webhooks)
5. Prometheus exporter
6. Remediation config

### Recursive Substitution

**Source:** `pkg/types/config.go:1402-1456`

Environment variables are substituted recursively in:

- ✅ All string fields
- ✅ Map values (recursively)
- ✅ Array elements (recursively)
- ✅ Nested structures (monitors, webhooks, strategies)

Non-string values (numbers, booleans) are preserved as-is.

## Configuration Validation

### Validation Process

**Source:** `pkg/types/config.go:811-873`

Validation occurs in this order:

1. **Schema validation**: API version, kind, metadata
2. **Settings validation**: Global settings, durations, ranges
3. **Monitor validation**: Each monitor configuration
4. **Exporter validation**: Kubernetes, HTTP, Prometheus
5. **Remediation validation**: Global and per-monitor settings

### Required Fields

- ✅ `apiVersion` must not be empty
- ✅ `kind` must be "NodeDoctorConfig"
- ✅ `metadata.name` must not be empty
- ✅ `settings.nodeName` must not be empty
- ✅ All monitor names must be unique
- ✅ All webhook names must be unique (if specified)

### Common Validation Errors

```
# Duration errors
ERROR: Invalid configuration: interval must be positive (got: -1s)
ERROR: Invalid configuration: timeout (15s) must be less than interval (10s)

# Range errors
ERROR: Invalid configuration: threshold must be between 0 and 100 (got: 150)
ERROR: Invalid configuration: port must be in range 1-65535 (got: 70000)
ERROR: Invalid configuration: qps exceeds maximum allowed value 10000 (got: 50000)

# Path errors
ERROR: Invalid configuration: scriptPath must be an absolute path (got: ../script.sh)
ERROR: Invalid configuration: scriptPath cannot contain '..' path traversal

# Field errors
ERROR: Invalid configuration: nodeName is required
ERROR: Invalid configuration: duplicate monitor name 'kubelet-health' found
ERROR: Invalid configuration: at least one webhook must be configured when HTTP exporter is enabled

# Strategy errors
ERROR: Invalid configuration: service is required for systemd-restart strategy
ERROR: Invalid configuration: scriptPath is required for custom-script strategy
ERROR: Invalid configuration: invalid strategy "unknown", must be one of: systemd-restart, custom-script, node-reboot, pod-delete

# Auth errors
ERROR: Invalid configuration: token is required for bearer auth
ERROR: Invalid configuration: username is required for basic auth
ERROR: Invalid configuration: invalid auth type "digest", must be one of: none, bearer, basic
```

## Configuration Examples

### Minimal Configuration

```yaml
apiVersion: node-doctor.io/v1alpha1
kind: NodeDoctorConfig
metadata:
  name: minimal

settings:
  nodeName: "${NODE_NAME}"

monitors:
  - name: kubelet-health
    type: kubernetes-kubelet-check
    enabled: true
    config:
      healthzURL: http://127.0.0.1:10248/healthz

exporters:
  kubernetes:
    enabled: true
```

### Production Configuration

**Source:** `config/node-doctor.yaml:168`

```yaml
apiVersion: node-doctor.io/v1alpha1
kind: NodeDoctorConfig
metadata:
  name: production
  namespace: kube-system
  labels:
    app: node-doctor
    environment: production

settings:
  nodeName: "${NODE_NAME}"
  logLevel: info
  logFormat: json
  logOutput: stdout
  updateInterval: 10s
  resyncInterval: 60s
  heartbeatInterval: 5m
  enableRemediation: true
  dryRunMode: false
  qps: 50
  burst: 100

monitors:
  # Kubernetes health
  - name: kubelet-health
    type: kubernetes-kubelet-check
    enabled: true
    interval: 30s
    timeout: 10s
    config:
      healthzURL: http://127.0.0.1:10248/healthz
      checkSystemdStatus: true
    remediation:
      enabled: true
      strategy: systemd-restart
      service: kubelet
      cooldown: 5m
      maxAttempts: 3
      gracefulStop: true
      waitTimeout: 30s

  # Disk health with multi-step remediation
  - name: disk-health
    type: system-disk-check
    enabled: true
    interval: 5m
    timeout: 30s
    config:
      paths:
        - path: "/"
          warningThreshold: 85
          criticalThreshold: 95
        - path: "/var/lib/docker"
          warningThreshold: 80
          criticalThreshold: 90
        - path: "/var/lib/kubelet"
          warningThreshold: 80
          criticalThreshold: 90
    remediation:
      enabled: true
      strategy: custom-script
      scriptPath: /usr/local/bin/cleanup-docker-logs.sh
      cooldown: 1h
      maxAttempts: 1

  # Container runtime
  - name: containerd-health
    type: kubernetes-runtime-check
    enabled: true
    interval: 1m
    timeout: 15s
    config:
      runtimeType: auto
      checkSocketConnectivity: true
      checkSystemdStatus: true
      checkRuntimeInfo: true
    remediation:
      enabled: true
      strategy: systemd-restart
      service: containerd
      cooldown: 10m
      maxAttempts: 2

exporters:
  kubernetes:
    enabled: true
    updateInterval: 10s
    resyncInterval: 60s
    heartbeatInterval: 5m
    namespace: default
    conditions:
      - type: NodeDoctorHealthy
        defaultStatus: "True"
        defaultReason: "NodeDoctorRunning"
        defaultMessage: "Node Doctor is running and monitoring the node"
      - type: KubeletHealthy
        defaultStatus: "Unknown"
        defaultReason: "NotYetChecked"
        defaultMessage: "Kubelet health not yet verified"
    annotations:
      - key: node-doctor.io/status
        value: "healthy"
      - key: node-doctor.io/version
        value: "${VERSION}"
    events:
      maxEventsPerMinute: 10
      eventTTL: 1h
      deduplicationWindow: 5m

  http:
    enabled: true
    workers: 5
    queueSize: 100
    timeout: 30s
    retry:
      maxAttempts: 3
      baseDelay: 1s
      maxDelay: 30s
    headers:
      User-Agent: node-doctor/v1
    webhooks:
      - name: slack-alerts
        url: "${SLACK_WEBHOOK_URL}"
        auth:
          type: bearer
          token: "${SLACK_TOKEN}"
        timeout: 10s
        sendStatus: false
        sendProblems: true

      - name: monitoring-system
        url: "https://monitoring.example.com/webhook"
        auth:
          type: basic
          username: "${WEBHOOK_USER}"
          password: "${WEBHOOK_PASSWORD}"
        headers:
          X-Cluster-ID: "${CLUSTER_ID}"
        sendStatus: true
        sendProblems: true

  prometheus:
    enabled: true
    port: 9100
    path: /metrics
    namespace: node_doctor
    subsystem: monitor
    labels:
      environment: production
      cluster: main

remediation:
  enabled: true
  dryRun: false
  maxRemediationsPerHour: 10
  maxRemediationsPerMinute: 2
  cooldownPeriod: 5m
  maxAttemptsGlobal: 3
  circuitBreaker:
    enabled: true
    threshold: 5
    timeout: 30m
    successThreshold: 2
  historySize: 100
  overrides:
    - problem: kubelet-unhealthy
      cooldown: 3m
      maxAttempts: 5
      circuitBreakerThreshold: 10
    - problem: disk-pressure
      cooldown: 30m
      maxAttempts: 2
      circuitBreakerThreshold: 3

features:
  enableMetrics: true
  enableProfiling: false
  profilingPort: 6060
  enableTracing: false
```

### Development Configuration

```yaml
apiVersion: node-doctor.io/v1alpha1
kind: NodeDoctorConfig
metadata:
  name: development

settings:
  nodeName: "${NODE_NAME}"
  logLevel: debug
  logFormat: text
  logOutput: stdout
  enableRemediation: true
  dryRunMode: true              # ✅ Dry-run for testing

monitors:
  - name: kubelet-health
    type: kubernetes-kubelet-check
    enabled: true
    interval: 10s               # ✅ More frequent checks
    timeout: 5s
    config:
      healthzURL: http://127.0.0.1:10248/healthz
    remediation:
      enabled: true
      strategy: systemd-restart
      service: kubelet
      cooldown: 1m              # ✅ Shorter cooldown for testing

exporters:
  kubernetes:
    enabled: true
  http:
    enabled: false              # ✅ Disable webhooks in dev
  prometheus:
    enabled: true
    port: 9100

remediation:
  enabled: true
  dryRun: true                  # ✅ Test without execution
  maxRemediationsPerHour: 100   # ✅ Higher limits for testing
  maxRemediationsPerMinute: 10

features:
  enableMetrics: true
  enableProfiling: true         # ✅ Enable profiling
  profilingPort: 6060
```

### Webhook Integration Examples

#### Slack Webhook

```yaml
exporters:
  http:
    enabled: true
    webhooks:
      - name: slack
        url: "https://hooks.slack.com/services/${SLACK_WORKSPACE}/${SLACK_CHANNEL}/${SLACK_TOKEN}"
        auth:
          type: none
        timeout: 5s
        retry:
          maxAttempts: 3
          baseDelay: 1s
          maxDelay: 10s
        headers:
          Content-Type: application/json
        sendStatus: false
        sendProblems: true
```

#### PagerDuty Integration

```yaml
exporters:
  http:
    enabled: true
    webhooks:
      - name: pagerduty
        url: "https://events.pagerduty.com/v2/enqueue"
        auth:
          type: bearer
          token: "${PAGERDUTY_ROUTING_KEY}"
        timeout: 10s
        retry:
          maxAttempts: 5
          baseDelay: 2s
          maxDelay: 30s
        sendStatus: false
        sendProblems: true
```

#### Custom Monitoring System

```yaml
exporters:
  http:
    enabled: true
    timeout: 30s
    retry:
      maxAttempts: 3
      baseDelay: 1s
      maxDelay: 30s
    headers:
      User-Agent: node-doctor/v1
      X-Cluster-ID: "${CLUSTER_ID}"
    webhooks:
      - name: custom-monitoring
        url: "https://monitoring.internal.example.com/api/v1/events"
        auth:
          type: basic
          username: "${MONITORING_USER}"
          password: "${MONITORING_PASSWORD}"
        timeout: 15s
        headers:
          X-Node-Name: "${NODE_NAME}"
          X-Environment: production
        sendStatus: true
        sendProblems: true
```

## Dynamic Configuration Updates

### Hot Reload Support

**Not fully implemented** - The configuration structure supports hot reload, but implementation may require restart for some changes.

#### Reloadable Without Restart

- Monitor intervals and timeouts
- Logging levels and formats
- Rate limits and thresholds
- Cooldown periods

#### Requires Restart

- Adding/removing monitors
- Changing monitor types
- Exporter enable/disable
- Authentication credentials
- Webhook URLs

### ConfigMap Updates

Deploy configuration via ConfigMap:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: node-doctor-config
  namespace: kube-system
data:
  config.yaml: |
    apiVersion: node-doctor.io/v1alpha1
    kind: NodeDoctorConfig
    # ... configuration here
```

Mount in DaemonSet:

```yaml
volumes:
  - name: config
    configMap:
      name: node-doctor-config
volumeMounts:
  - name: config
    mountPath: /etc/node-doctor
    readOnly: true
```

## Best Practices

### Security

1. **Use Secrets for Sensitive Data**
   ```yaml
   # ❌ Bad: Hardcoded credentials
   auth:
     token: "xoxb-1234-5678-abcd"

   # ✅ Good: Environment variables from secrets
   auth:
     token: "${SLACK_TOKEN}"
   ```

2. **Absolute Paths for Scripts**
   ```yaml
   # ❌ Bad: Relative or traversal paths
   scriptPath: ../scripts/fix.sh

   # ✅ Good: Absolute paths
   scriptPath: /usr/local/bin/remediation/fix.sh
   ```

3. **Validate Scripts Exist**
   - Ensure all script paths exist before deployment
   - Test scripts independently before enabling remediation

### Configuration Management

1. **Version Control**: Store configs in Git
2. **Environment-Specific**: Separate configs for dev/staging/prod
3. **Incremental Deployment**: Test in dev → staging → production
4. **Documentation**: Comment complex configurations
5. **Validation**: Use `--validate-only` flag before deployment

### Monitoring and Remediation

1. **Start Conservative**
   ```yaml
   # Phase 1: Monitoring only
   settings:
     enableRemediation: false

   # Phase 2: Dry-run mode
   remediation:
     enabled: true
     dryRun: true

   # Phase 3: Limited remediation
   remediation:
     enabled: true
     dryRun: false
     maxRemediationsPerHour: 5

   # Phase 4: Full remediation
   remediation:
     maxRemediationsPerHour: 10
   ```

2. **Circuit Breaker Protection**
   ```yaml
   # Always enable circuit breaker
   circuitBreaker:
     enabled: true
     threshold: 5      # Conservative threshold
     timeout: 30m      # Long enough to investigate
     successThreshold: 2
   ```

3. **Problem-Specific Tuning**
   ```yaml
   # Override defaults for critical problems
   overrides:
     - problem: kubelet-unhealthy
       cooldown: 5m
       maxAttempts: 3
       circuitBreakerThreshold: 3

     # More conservative for disk operations
     - problem: disk-pressure
       cooldown: 1h
       maxAttempts: 1
       circuitBreakerThreshold: 2
   ```

### Performance Tuning

1. **Adjust Intervals Based on Load**
   ```yaml
   # High-priority checks
   - name: kubelet-health
     interval: 30s

   # Medium-priority checks
   - name: disk-health
     interval: 5m

   # Low-priority checks
   - name: connectivity-health
     interval: 10m
   ```

2. **Kubernetes API Rate Limits**
   ```yaml
   settings:
     # High-traffic clusters
     qps: 100
     burst: 200

     # Low-traffic clusters
     qps: 20
     burst: 50
   ```

3. **HTTP Worker Pool Sizing**
   ```yaml
   http:
     workers: 10        # More workers for high webhook volume
     queueSize: 500     # Larger queue for burst traffic
   ```

## Troubleshooting

### Configuration Loading Issues

```bash
# Verify ConfigMap exists
kubectl get configmap node-doctor-config -n kube-system

# Check ConfigMap contents
kubectl describe configmap node-doctor-config -n kube-system

# Verify mount in pod
kubectl exec -n kube-system node-doctor-xxxxx -- ls -la /etc/node-doctor/

# Check configuration file
kubectl exec -n kube-system node-doctor-xxxxx -- cat /etc/node-doctor/config.yaml
```

### Validation Errors

```bash
# Validate configuration before deployment
node-doctor --config=config.yaml --validate-only

# Check for YAML syntax errors
yamllint config.yaml

# Validate against JSON schema (if available)
# yq eval -o=json config.yaml | jq -s '.[0]'
```

### Environment Variable Issues

```bash
# Check environment variables in pod
kubectl exec -n kube-system node-doctor-xxxxx -- env | grep NODE_

# Verify substitution
kubectl exec -n kube-system node-doctor-xxxxx -- \
  node-doctor --config=/etc/node-doctor/config.yaml --dump-config
```

### Common Issues

| Issue | Cause | Solution |
|-------|-------|----------|
| "nodeName is required" | Missing NODE_NAME env var | Add downward API env var |
| "timeout must be less than interval" | Invalid values | Ensure timeout < interval |
| "duplicate monitor name" | Copy-paste error | Make monitor names unique |
| "script not found" | Wrong path | Use absolute paths, verify files exist |
| "invalid auth type" | Typo in auth config | Use: none, bearer, or basic |
| "at least one webhook required" | HTTP exporter enabled, no webhooks | Add webhook or disable exporter |

## Configuration Reference Summary

### Complete Field Reference

```yaml
# Top-level (pkg/types/config.go:76-101)
apiVersion: string (required)
kind: string (required, must be "NodeDoctorConfig")
metadata:
  name: string (required)
  namespace: string
  labels: map[string]string

# Settings (pkg/types/config.go:110-139)
settings:
  nodeName: string (required)
  logLevel: string (default: "info")
  logFormat: string (default: "json")
  logOutput: string (default: "stdout")
  logFile: string
  updateInterval: duration (default: "10s")
  resyncInterval: duration (default: "60s")
  heartbeatInterval: duration (default: "5m")
  enableRemediation: bool (default: false)
  dryRunMode: bool (default: false)
  kubeconfig: string
  qps: float32 (default: 50)
  burst: int (default: 100)

# Monitors (pkg/types/config.go:141-166)
monitors:
  - name: string (required)
    type: string (required)
    enabled: bool (default: false)
    interval: duration (default: "30s")
    timeout: duration (default: "10s")
    config: map[string]interface{}
    remediation: # See remediation section

# Monitor Remediation (pkg/types/config.go:168-207)
remediation:
  enabled: bool
  strategy: string (required if enabled)
  service: string
  scriptPath: string
  args: []string
  cooldown: duration (default: "5m")
  maxAttempts: int (default: 3)
  priority: int
  gracefulStop: bool
  waitTimeout: duration
  strategies: []MonitorRemediationConfig

# Exporters (pkg/types/config.go:209-214)
exporters:
  kubernetes: # KubernetesExporterConfig
  http: # HTTPExporterConfig
  prometheus: # PrometheusExporterConfig

# Kubernetes Exporter (pkg/types/config.go:216-240)
exporters.kubernetes:
  enabled: bool
  updateInterval: duration (default: "10s")
  resyncInterval: duration (default: "60s")
  heartbeatInterval: duration (default: "5m")
  namespace: string
  conditions:
    - type: string (required)
      defaultStatus: string
      defaultReason: string
      defaultMessage: string
  annotations:
    - key: string (required)
      value: string (required)
  events:
    maxEventsPerMinute: int
    eventTTL: duration
    deduplicationWindow: duration

# HTTP Exporter (pkg/types/config.go:265-279)
exporters.http:
  enabled: bool
  workers: int (default: 5)
  queueSize: int (default: 100)
  timeout: duration (default: "30s")
  retry:
    maxAttempts: int (default: 3)
    baseDelay: duration (default: "1s")
    maxDelay: duration (default: "30s")
  headers: map[string]string
  webhooks:
    - name: string
      url: string (required)
      auth:
        type: string (default: "none")
        token: string
        username: string
        password: string
      timeout: duration
      retry: RetryConfig
      headers: map[string]string
      sendStatus: bool (default: true)
      sendProblems: bool (default: true)

# Prometheus Exporter (pkg/types/config.go:323-331)
exporters.prometheus:
  enabled: bool
  port: int (default: 9100)
  path: string (default: "/metrics")
  namespace: string (default: "node_doctor")
  subsystem: string
  labels: map[string]string

# Remediation (pkg/types/config.go:333-377)
remediation:
  enabled: bool
  dryRun: bool
  maxRemediationsPerHour: int (default: 10)
  maxRemediationsPerMinute: int (default: 2)
  cooldownPeriod: duration (default: "5m")
  maxAttemptsGlobal: int (default: 3)
  circuitBreaker:
    enabled: bool
    threshold: int (default: 5)
    timeout: duration (default: "30m")
    successThreshold: int (default: 2)
  historySize: int (default: 100)
  overrides:
    - problem: string (required)
      cooldown: duration
      maxAttempts: int
      circuitBreakerThreshold: int

# Features (pkg/types/config.go:378-385)
features:
  enableMetrics: bool (default: true)
  enableProfiling: bool (default: false)
  profilingPort: int (default: 0)
  enableTracing: bool (default: false)
  tracingEndpoint: string
```

## Summary

Node Doctor's configuration system provides:

- ✅ **Type-safe schema** defined in `pkg/types/config.go`
- ✅ **Comprehensive validation** at startup with clear error messages
- ✅ **Environment variable substitution** for all string fields
- ✅ **Sensible defaults** for all optional fields
- ✅ **Multiple configuration sources** with clear precedence
- ✅ **Webhook support** with authentication and retry logic
- ✅ **Circuit breaker protection** for remediation safety
- ✅ **Problem-specific overrides** for fine-tuned control
- ✅ **Extensive examples** for common scenarios

Start with a minimal configuration and expand incrementally. Always test in non-production environments first, use dry-run mode before enabling remediation, and leverage the circuit breaker for safety.
