# Node Doctor Configuration Reference

## Overview

Node Doctor uses a declarative configuration system based on YAML (primary) with JSON support for Node Problem Detector compatibility. Configuration can be loaded from multiple sources with a clear precedence order.

## Configuration Sources and Precedence

Configuration is loaded from multiple sources in this order (later sources override earlier ones):

1. **Embedded Defaults**: Compiled into the binary
2. **ConfigMap**: Mounted at `/etc/node-doctor/config.yaml`
3. **Environment Variables**: Override specific values
4. **Command-Line Flags**: Highest precedence

## Complete Configuration Example

```yaml
apiVersion: node-doctor.io/v1alpha1
kind: NodeDoctorConfig
metadata:
  name: default-config
  namespace: kube-system

# Global settings
settings:
  # Node name (from downward API or hostname)
  nodeName: "${NODE_NAME}"

  # Logging configuration
  logLevel: info  # debug, info, warn, error
  logFormat: json  # json, text
  logOutput: stdout  # stdout, stderr, file
  logFile: /var/log/node-doctor.log

  # Update intervals
  updateInterval: 10s
  resyncInterval: 60s
  heartbeatInterval: 5m

  # Remediation settings
  enableRemediation: true
  dryRunMode: false

  # Kubernetes client configuration
  kubeconfig: ""  # Empty for in-cluster
  qps: 50
  burst: 100

# Monitors configuration
monitors:
  # System Health Monitors
  - name: cpu-health
    type: system-cpu-check
    enabled: true
    interval: 30s
    timeout: 10s
    config:
      warningLoadFactor: 0.8
      criticalLoadFactor: 1.5
      checkThermalEvents: true
      thermalThreshold: 50  # percent of max frequency

  - name: memory-health
    type: system-memory-check
    enabled: true
    interval: 30s
    timeout: 10s
    config:
      warningThreshold: 20  # percent available
      criticalThreshold: 10
      swapThreshold: 50
      checkOOMKills: true
      checkPSI: true  # Pressure Stall Information

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
      checkInodes: true
      inodeWarningThreshold: 90
      checkReadonly: true
      checkSMART: false
    remediation:
      enabled: true
      strategies:
        - strategy: disk
          action: docker-logs
          priority: 1
          cooldown: 1h
          maxAttempts: 1
        - strategy: disk
          action: docker-images
          priority: 2
          cooldown: 6h
          maxAttempts: 1
        - strategy: disk
          action: system-logs
          priority: 3
          cooldown: 12h
          maxAttempts: 1

  # Network Health Monitors
  - name: dns-health
    type: network-dns-check
    enabled: true
    interval: 60s
    timeout: 10s
    config:
      clusterDomains:
        - kubernetes.default.svc.cluster.local
        - kube-dns.kube-system.svc.cluster.local
      externalDomains:
        - google.com
        - cloudflare.com
      customQueries:
        - domain: myservice.mynamespace.svc.cluster.local
          recordType: A
      latencyThreshold: 1s
      retries: 3
      nameservers:
        - /etc/resolv.conf
    remediation:
      enabled: true
      strategy: network
      action: flush-dns
      cooldown: 10m
      maxAttempts: 2

  - name: gateway-health
    type: network-gateway-check
    enabled: true
    interval: 30s
    timeout: 5s
    config:
      pingCount: 3
      pingTimeout: 1s
      latencyThreshold: 100ms
      autoDetectGateway: true
      manualGateway: ""  # Override auto-detection
      packetLossThreshold: 50  # percent

  - name: connectivity-health
    type: network-connectivity-check
    enabled: true
    interval: 60s
    timeout: 10s
    config:
      endpoints:
        - url: https://google.com
          method: HEAD
          expectedStatusCode: 200
          timeout: 5s
        - url: https://kubernetes.io
          method: GET
          expectedStatusCode: 200
          timeout: 5s
      interfaces:
        - name: eth0
          expectUp: true
        - name: docker0
          expectUp: true

  # Kubernetes Component Monitors
  - name: kubelet-health
    type: kubernetes-kubelet-check
    enabled: true
    interval: 30s
    timeout: 10s
    config:
      healthzURL: http://127.0.0.1:10248/healthz
      metricsURL: http://127.0.0.1:10255/metrics
      checkSystemdStatus: true
      checkLogs: true
      plegThreshold: 5s
      logErrorPatterns:
        - "Failed to.*"
        - "Error:.*"
    remediation:
      enabled: true
      strategy: systemd-restart
      service: kubelet
      cooldown: 5m
      maxAttempts: 3
      gracefulStop: true
      waitTimeout: 30s

  - name: apiserver-health
    type: kubernetes-apiserver-check
    enabled: true
    interval: 60s
    timeout: 10s
    config:
      endpoint: https://kubernetes.default.svc.cluster.local
      latencyThreshold: 2s
      checkVersion: true
      checkAuth: true
      retries: 3

  - name: runtime-health
    type: kubernetes-runtime-check
    enabled: true
    interval: 60s
    timeout: 10s
    config:
      runtimeType: auto  # auto, docker, containerd, cri-o
      dockerSocket: /var/run/docker.sock
      containerdSocket: /run/containerd/containerd.sock
      crioSocket: /var/run/crio/crio.sock
      checkContainers: true
      checkImages: true
      checkSystemdStatus: true
    remediation:
      enabled: true
      strategy: systemd-restart
      cooldown: 15m
      maxAttempts: 2
      gracefulStop: true

  - name: pod-capacity
    type: kubernetes-capacity-check
    enabled: true
    interval: 5m
    timeout: 30s
    config:
      warningThreshold: 80   # percent
      criticalThreshold: 95
      checkDaemonSets: true
      checkAllocatable: true

  # Custom Plugin Monitors
  - name: ntp-health
    type: custom-plugin-check
    enabled: true
    interval: 5m
    timeout: 10s
    config:
      path: /usr/local/bin/check_ntp.sh
      args:
        - --threshold
        - "100"
      env:
        NTP_SERVER: pool.ntp.org
      workingDir: /usr/local/bin
      maxOutputLength: 4096
      concurrency: 1
      problem:
        type: permanent
        condition: NTPProblem
        reason: NTPDown
    remediation:
      enabled: true
      strategy: custom-script
      scriptPath: /usr/local/bin/fix_ntp.sh
      args: []
      timeout: 5m
      cooldown: 15m
      maxAttempts: 2

  # Log Pattern Monitors
  - name: kernel-monitor
    type: log-pattern-check
    enabled: true
    config:
      source: kmsg
      path: /dev/kmsg
      lookback: 5m
      bufferSize: 100
      rules:
        - type: temporary
          reason: OOMKilling
          pattern: "Killed process \\d+ .+ total-vm:\\d+kB"
          rateLimit: 10m
          severity: warning

        - type: permanent
          condition: KernelDeadlock
          reason: TaskHung
          pattern: "task .+ blocked for more than \\d+ seconds"
          rateLimit: 5m
          severity: error

        - type: permanent
          condition: ReadonlyFilesystem
          reason: FilesystemReadonly
          pattern: "Remounting filesystem read-only"
          rateLimit: 1h
          severity: critical

  - name: docker-monitor
    type: log-pattern-check
    enabled: true
    config:
      source: journald
      unit: docker.service
      lookback: 10m
      bufferSize: 50
      rules:
        - type: temporary
          reason: DockerError
          pattern: "Error response from daemon"
          rateLimit: 5m
          severity: warning

        - type: temporary
          reason: DockerTimeout
          pattern: "context deadline exceeded"
          rateLimit: 5m
          severity: warning

# Exporters configuration
exporters:
  # Kubernetes Exporter
  kubernetes:
    enabled: true
    updateInterval: 10s
    resyncInterval: 60s
    heartbeatInterval: 5m
    namespace: default  # Namespace for events
    conditions:
      # Define custom node conditions
      - type: NodeDoctorHealthy
        defaultStatus: "True"
        defaultReason: "NodeDoctorRunning"
        defaultMessage: "Node Doctor is running and monitoring node health"
    annotations:
      # Node annotations to update
      - key: node-doctor.io/status
        value: "healthy"
      - key: node-doctor.io/version
        value: "${VERSION}"
      - key: node-doctor.io/last-check
        value: "${TIMESTAMP}"
    events:
      # Event rate limiting
      maxEventsPerMinute: 10
      eventTTL: 1h
      deduplicationWindow: 5m

  # HTTP Exporter
  http:
    enabled: true
    bindAddress: "0.0.0.0"
    hostPort: 8080
    tlsEnabled: false
    tlsCertFile: /etc/node-doctor/tls/tls.crt
    tlsKeyFile: /etc/node-doctor/tls/tls.key
    endpoints:
      - path: /health
        handler: overall-health
        description: "Overall node health status (200=healthy, 503=unhealthy)"
      - path: /ready
        handler: readiness
        description: "Node readiness status"
      - path: /metrics
        handler: prometheus-metrics
        description: "Prometheus metrics"
      - path: /status
        handler: detailed-status
        description: "Detailed status of all monitors (JSON)"
      - path: /remediation/history
        handler: remediation-history
        description: "History of remediation actions (JSON)"
      - path: /conditions
        handler: node-conditions
        description: "Current node conditions (JSON)"

  # Prometheus Exporter
  prometheus:
    enabled: true
    port: 9100
    path: /metrics
    namespace: node_doctor
    subsystem: ""
    labels:
      environment: production
      cluster: my-cluster

# Remediation settings
remediation:
  # Global remediation settings
  enabled: true
  dryRun: false

  # Safety limits
  maxRemediationsPerHour: 10
  maxRemediationsPerMinute: 2
  cooldownPeriod: 5m
  maxAttemptsGlobal: 3

  # Circuit breaker settings
  circuitBreaker:
    enabled: true
    threshold: 5  # Open after N failures
    timeout: 30m  # Try again after timeout
    successThreshold: 2  # Close after N successes

  # Remediation history
  historySize: 100  # Keep last N remediation records

  # Problem-specific overrides
  overrides:
    - problem: KubeletUnhealthy
      cooldown: 5m
      maxAttempts: 3
      circuitBreakerThreshold: 3

    - problem: DiskPressure
      cooldown: 1h
      maxAttempts: 2
      circuitBreakerThreshold: 2

# Feature flags
features:
  enableMetrics: true
  enableProfiling: false
  profilingPort: 6060
  enableTracing: false
  tracingEndpoint: ""
```

## Configuration Sections

### Global Settings

```yaml
settings:
  nodeName: "${NODE_NAME}"
  logLevel: info
  logFormat: json
  enableRemediation: true
  dryRunMode: false
```

**Fields**:
- `nodeName`: Node name (usually from downward API `${NODE_NAME}`)
- `logLevel`: Logging level (debug, info, warn, error)
- `logFormat`: Log format (json, text)
- `logOutput`: Where to write logs (stdout, stderr, file)
- `logFile`: Log file path (if logOutput=file)
- `updateInterval`: How often to update Kubernetes API
- `resyncInterval`: Full resync interval
- `heartbeatInterval`: Heartbeat interval for condition updates
- `enableRemediation`: Master switch for remediation
- `dryRunMode`: Test mode without actual changes

### Monitor Configuration

Each monitor has this structure:

```yaml
- name: monitor-name
  type: monitor-type
  enabled: true
  interval: 30s
  timeout: 10s
  config:
    # Monitor-specific configuration
  remediation:
    # Remediation configuration
```

**Common Fields**:
- `name`: Unique monitor name
- `type`: Monitor type (from registry)
- `enabled`: Whether monitor is active
- `interval`: Check interval
- `timeout`: Check timeout
- `config`: Monitor-specific configuration
- `remediation`: Optional remediation configuration

### Remediation Configuration

```yaml
remediation:
  enabled: true
  strategy: systemd-restart
  service: kubelet
  cooldown: 5m
  maxAttempts: 3
```

**Fields**:
- `enabled`: Enable remediation for this monitor
- `strategy`: Remediation strategy type
- Additional fields depend on strategy type

**Strategy Types**:
- `systemd-restart`: Restart systemd service
- `network`: Network remediation (flush-dns, restart-interface)
- `disk`: Disk cleanup (docker-logs, docker-images, system-logs)
- `runtime`: Container runtime remediation
- `custom-script`: Execute custom script

### Exporter Configuration

#### Kubernetes Exporter

```yaml
exporters:
  kubernetes:
    enabled: true
    updateInterval: 10s
    namespace: default
    conditions: [...]
    annotations: [...]
```

**Fields**:
- `enabled`: Enable Kubernetes exporter
- `updateInterval`: How often to update Kubernetes API
- `resyncInterval`: Full resync interval
- `namespace`: Namespace for events
- `conditions`: Custom node conditions
- `annotations`: Node annotations to manage

#### HTTP Exporter

```yaml
exporters:
  http:
    enabled: true
    bindAddress: "0.0.0.0"
    hostPort: 8080
    tlsEnabled: false
```

**Fields**:
- `enabled`: Enable HTTP exporter
- `bindAddress`: Bind address (use 0.0.0.0 for hostPort)
- `hostPort`: Host port to expose
- `tlsEnabled`: Enable TLS
- `tlsCertFile`: TLS certificate path
- `tlsKeyFile`: TLS key path
- `endpoints`: List of HTTP endpoints

#### Prometheus Exporter

```yaml
exporters:
  prometheus:
    enabled: true
    port: 9100
    namespace: node_doctor
```

**Fields**:
- `enabled`: Enable Prometheus exporter
- `port`: Metrics port
- `path`: Metrics path (default: /metrics)
- `namespace`: Metric namespace
- `labels`: Additional labels for all metrics

## Environment Variable Overrides

Environment variables can override specific configuration values:

```bash
# Global settings
NODE_DOCTOR_LOG_LEVEL=debug
NODE_DOCTOR_DRY_RUN=true
NODE_DOCTOR_ENABLE_REMEDIATION=false

# Kubernetes client
NODE_DOCTOR_QPS=100
NODE_DOCTOR_BURST=200

# HTTP exporter
NODE_DOCTOR_HTTP_ENABLED=true
NODE_DOCTOR_HTTP_PORT=8080

# Prometheus exporter
NODE_DOCTOR_PROMETHEUS_ENABLED=true
NODE_DOCTOR_PROMETHEUS_PORT=9100

# Remediation
NODE_DOCTOR_REMEDIATION_ENABLED=true
NODE_DOCTOR_REMEDIATION_DRY_RUN=false
NODE_DOCTOR_REMEDIATION_MAX_PER_HOUR=10
```

## Command-Line Flags

Override configuration via command-line flags:

```bash
node-doctor \
  --config=/etc/node-doctor/config.yaml \
  --log-level=debug \
  --dry-run \
  --enable-remediation=false \
  --http-port=8080 \
  --metrics-port=9100
```

**Common Flags**:
- `--config`: Configuration file path
- `--log-level`: Log level (debug, info, warn, error)
- `--log-format`: Log format (json, text)
- `--dry-run`: Enable dry-run mode
- `--enable-remediation`: Enable/disable remediation
- `--http-port`: HTTP exporter port
- `--metrics-port`: Prometheus metrics port
- `--node-name`: Override node name
- `--kubeconfig`: Path to kubeconfig (for out-of-cluster)

## Configuration Validation

Node Doctor validates configuration on startup:

### Required Fields

- `settings.nodeName`
- At least one monitor enabled

### Field Validation

- Intervals must be positive durations
- Thresholds must be within valid ranges
- File paths must exist (for scripts, logs)
- Ports must be valid (1-65535)
- Service names must be valid systemd services

### Validation Errors

Configuration errors cause startup failure:

```
FATAL: Invalid configuration: interval must be positive (got: -1s)
FATAL: Invalid configuration: threshold must be between 0 and 100 (got: 150)
FATAL: Invalid configuration: script not found: /path/to/script.sh
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
    interval: 30s
    config:
      healthzURL: http://127.0.0.1:10248/healthz

exporters:
  kubernetes:
    enabled: true
  http:
    enabled: true
    hostPort: 8080
```

### Production Configuration

```yaml
apiVersion: node-doctor.io/v1alpha1
kind: NodeDoctorConfig
metadata:
  name: production
  labels:
    environment: production

settings:
  nodeName: "${NODE_NAME}"
  logLevel: info
  logFormat: json
  enableRemediation: true
  dryRunMode: false

monitors:
  # Essential health checks
  - name: kubelet-health
    type: kubernetes-kubelet-check
    enabled: true
    interval: 30s
    config:
      healthzURL: http://127.0.0.1:10248/healthz
    remediation:
      enabled: true
      strategy: systemd-restart
      service: kubelet
      cooldown: 5m
      maxAttempts: 3

  - name: disk-health
    type: system-disk-check
    enabled: true
    interval: 5m
    config:
      paths:
        - path: "/"
          criticalThreshold: 95
        - path: "/var/lib/docker"
          criticalThreshold: 90
    remediation:
      enabled: true
      strategies:
        - strategy: disk
          action: docker-logs

  - name: dns-health
    type: network-dns-check
    enabled: true
    interval: 60s
    config:
      clusterDomains:
        - kubernetes.default.svc.cluster.local
    remediation:
      enabled: true
      strategy: network
      action: flush-dns

exporters:
  kubernetes:
    enabled: true
  http:
    enabled: true
    hostPort: 8080
  prometheus:
    enabled: true
    port: 9100

remediation:
  enabled: true
  maxRemediationsPerHour: 10
  circuitBreaker:
    enabled: true
    threshold: 5
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
  enableRemediation: true
  dryRunMode: true  # Dry-run for development

monitors:
  - name: kubelet-health
    type: kubernetes-kubelet-check
    enabled: true
    interval: 10s  # More frequent checks
    config:
      healthzURL: http://127.0.0.1:10248/healthz

exporters:
  kubernetes:
    enabled: true
  http:
    enabled: true
    hostPort: 8080

remediation:
  dryRun: true  # Test remediation without execution
```

## Dynamic Configuration Updates

Node Doctor can watch for configuration changes and reload:

### ConfigMap Watch

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: node-doctor-config
  namespace: kube-system
data:
  config.yaml: |
    # Configuration here
```

Node Doctor watches this ConfigMap and reloads on changes.

### Reload Behavior

- **Hot-reloadable**: Monitor intervals, thresholds, log levels
- **Requires restart**: New monitors, exporter changes, remediation strategies

### Reload Signal

Send SIGHUP to reload configuration:

```bash
kill -HUP $(pidof node-doctor)
```

## Best Practices

1. **Use ConfigMaps**: Store configuration in ConfigMaps for easy updates
2. **Version Control**: Keep configuration in version control
3. **Environment-Specific**: Use different configs for dev/staging/prod
4. **Start Conservative**: Begin with monitoring only, enable remediation gradually
5. **Document Changes**: Comment configuration changes
6. **Validate Before Deploy**: Test configuration in non-production first
7. **Monitor Configuration**: Track configuration changes via GitOps
8. **Use Secrets**: Store sensitive data (TLS certs) in Secrets
9. **Set Appropriate Intervals**: Balance monitoring frequency with resource usage
10. **Enable Dry-Run First**: Test remediation in dry-run before enabling

## Troubleshooting

### Configuration Not Loading

1. Check ConfigMap exists and is mounted
2. Verify file permissions
3. Check for YAML syntax errors
4. Review startup logs

### Overrides Not Working

1. Verify precedence order (flags > env > ConfigMap > defaults)
2. Check environment variable names
3. Verify flag syntax

### Validation Errors

1. Read error message carefully
2. Check field types and ranges
3. Verify file paths exist
4. Test configuration with `--validate-only` flag

## Configuration Schema

Full JSON Schema available at: `https://node-doctor.io/schema/v1alpha1`

Validate configuration:

```bash
node-doctor --config=config.yaml --validate-only
```

## Summary

Node Doctor's configuration system provides:
- **Flexible**: Multiple configuration sources
- **Validated**: Strict validation on startup
- **Dynamic**: Hot-reload for many settings
- **Documented**: Clear schema and examples
- **Safe**: Defaults for all optional fields

Start with a minimal configuration and expand as needed, always testing in non-production environments first.
