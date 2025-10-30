# Node Doctor Architecture

## Overview

Node Doctor is a Kubernetes node health monitoring and auto-remediation system built in Go. It is based on the proven architecture of Node Problem Detector (NPD) but extends it with comprehensive health checks, automated remediation capabilities, HTTP health endpoints, and multiple reporting mechanisms.

## Design Principles

1. **Plugin Architecture**: Extensible system where monitors, exporters, and remediators are pluggable components
2. **Channel-Based Communication**: Decoupled components communicating via Go channels for clean separation and testability
3. **Configuration-Driven**: Declarative YAML/JSON configuration for all health checks and remediation actions
4. **Safety-First**: Multiple safety mechanisms (cooldowns, rate limits, circuit breakers) to prevent remediation storms
5. **Kubernetes-Native**: Deep integration with Kubernetes API, following k8s patterns and conventions
6. **Observability**: Comprehensive metrics, events, and status reporting for full visibility

## System Architecture

### High-Level Component Diagram

```
┌─────────────────────────────────────────────────────────────┐
│                        Node Doctor                           │
│                                                               │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │   System     │  │   Network    │  │ Kubernetes   │      │
│  │   Monitors   │  │   Monitors   │  │   Monitors   │ ...  │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘      │
│         │                  │                  │               │
│         └──────────────────┼──────────────────┘              │
│                            ▼                                  │
│                  ┌─────────────────┐                         │
│                  │     Problem     │                         │
│                  │    Detector     │                         │
│                  │  (Orchestrator) │                         │
│                  └─────────┬───────┘                         │
│                            │                                  │
│         ┌──────────────────┼──────────────────┐             │
│         ▼                  ▼                   ▼             │
│  ┌──────────┐      ┌─────────────┐    ┌─────────────┐      │
│  │    K8s   │      │    HTTP     │    │ Prometheus  │      │
│  │ Exporter │      │  Exporter   │    │  Exporter   │      │
│  └──────────┘      └─────────────┘    └─────────────┘      │
│         │                  │                   │             │
│         ▼                  ▼                   ▼             │
│  ┌──────────┐      ┌─────────────┐    ┌─────────────┐      │
│  │Conditions│      │hostPort:8080│    │   Metrics   │      │
│  │  Events  │      │  /health    │    │    :9100    │      │
│  │Annotations│     │  /metrics   │    │             │      │
│  └──────────┘      └─────────────┘    └─────────────┘      │
│         │                                                     │
│         └─────────────────┐                                  │
│                           ▼                                  │
│                  ┌─────────────────┐                         │
│                  │   Remediator    │                         │
│                  │    Registry     │                         │
│                  └─────────┬───────┘                         │
│                            │                                  │
│         ┌──────────────────┼──────────────────┐             │
│         ▼                  ▼                   ▼             │
│  ┌──────────┐      ┌─────────────┐    ┌─────────────┐      │
│  │ Systemd  │      │   Network   │    │    Disk     │      │
│  │Remediator│      │ Remediator  │    │ Remediator  │      │
│  └──────────┘      └─────────────┘    └─────────────┘      │
└─────────────────────────────────────────────────────────────┘
```

### Data Flow

1. **Detection Phase**:
   - Multiple monitors run concurrently in separate goroutines
   - Each monitor performs health checks at configured intervals
   - Problems detected are sent to Problem Detector via channels

2. **Orchestration Phase**:
   - Problem Detector fan-ins from all monitor channels
   - Aggregates and deduplicates problems
   - Fan-outs to exporters and remediators

3. **Export Phase**:
   - Kubernetes Exporter: Updates node conditions, creates events, updates annotations
   - HTTP Exporter: Exposes health status via HTTP endpoints
   - Prometheus Exporter: Exports metrics for scraping

4. **Remediation Phase**:
   - Remediator Registry matches problems to remediation strategies
   - Safety checks (cooldown, rate limits, circuit breaker)
   - Execute remediation actions
   - Report remediation results via events

## Core Components

### 1. Monitor Interface

The foundation of the plugin architecture:

```go
type Monitor interface {
    // Start starts the monitor and returns a channel for status updates
    Start() (<-chan *Status, error)

    // Stop gracefully stops the monitor
    Stop()
}
```

Key characteristics:
- Each monitor runs in its own goroutine
- Non-blocking status reporting via channels
- Graceful shutdown via context cancellation

### 2. Status Types

```go
// Status represents the health status from a monitor
type Status struct {
    Source     string      // Monitor identifier
    Events     []Event     // Temporary problems
    Conditions []Condition // Permanent problems affecting node conditions
    Timestamp  time.Time   // When the status was generated
}

// Event represents a temporary problem
type Event struct {
    Severity string    // info, warning, error
    Reason   string    // Machine-readable reason
    Message  string    // Human-readable message
}

// Condition represents a permanent problem affecting node availability
type Condition struct {
    Type    string    // NodeCondition type (e.g., KubeletUnhealthy)
    Status  string    // True, False, Unknown
    Reason  string    // Machine-readable reason
    Message string    // Human-readable message
}
```

Design decisions:
- **Events vs Conditions**: Events are transient, conditions affect node scheduling
- **Source tracking**: Each status identifies its source monitor
- **Immutable**: Status objects are created once and never modified

### 3. Problem Detector (Orchestrator)

Central component that coordinates all operations:

```go
type ProblemDetector struct {
    monitors    []Monitor
    exporters   []Exporter
    remediators RemediatorRegistry
    statusChan  chan *Status
}
```

Responsibilities:
- Start/stop all monitors
- Fan-in status from all monitors
- Aggregate and deduplicate problems
- Fan-out to exporters
- Trigger remediation actions
- Handle graceful shutdown

### 4. Exporter Interface

```go
type Exporter interface {
    // ExportProblems exports the detected problems
    ExportProblems(status *Status) error
}
```

Implementations:
- **KubernetesExporter**: Node conditions, events, annotations
- **HTTPExporter**: Health endpoints on hostPort
- **PrometheusExporter**: Metrics in Prometheus format

### 5. Remediator Interface

```go
type Remediator interface {
    // CanRemediate checks if this remediator can handle the problem
    CanRemediate(problem Problem) bool

    // Remediate attempts to fix the problem
    Remediate(ctx context.Context, problem Problem) error

    // GetCooldown returns the cooldown period for this remediator
    GetCooldown() time.Duration
}
```

Implementations:
- **SystemdRemediator**: Restart systemd services
- **NetworkRemediator**: Reset network interfaces, flush DNS cache
- **DiskRemediator**: Clean up logs, docker images, temp files
- **ContainerRuntimeRemediator**: Restart container runtime
- **CustomScriptRemediator**: Execute custom remediation scripts

### 6. Remediator Registry

Manages remediation execution with safety mechanisms:

```go
type RemediatorRegistry struct {
    remediators      []Remediator
    cooldownTracker  map[string]time.Time
    attemptTracker   map[string]int
    circuitBreaker   map[string]*CircuitBreaker
    maxAttempts      int
    remediationBudget RateLimiter
}
```

Safety features:
- **Cooldown tracking**: Prevents rapid re-remediation
- **Attempt limiting**: Max attempts before giving up
- **Circuit breaker**: Opens after N consecutive failures
- **Rate limiting**: Max remediations per time window
- **Dry-run mode**: Test remediation logic without execution

## Configuration System

### Configuration Format

YAML (primary) with JSON support for NPD compatibility:

```yaml
apiVersion: node-doctor.io/v1alpha1
kind: NodeDoctorConfig
metadata:
  name: default-config

# Global settings
settings:
  nodeName: "${NODE_NAME}"  # From downward API
  updateInterval: 10s
  enableRemediation: true
  dryRunMode: false

# Monitor configurations
monitors:
  - name: kubelet-health
    type: http-health-check
    enabled: true
    interval: 30s
    timeout: 10s
    config:
      url: "http://127.0.0.1:10248/healthz"
      expectedStatusCode: 200
    remediation:
      enabled: true
      strategy: systemd-restart
      service: kubelet
      cooldown: 5m
      maxAttempts: 3

  - name: disk-space
    type: disk-health-check
    enabled: true
    interval: 5m
    config:
      paths:
        - path: "/"
          warningThreshold: 85
          criticalThreshold: 95
        - path: "/var/lib/docker"
          warningThreshold: 80
          criticalThreshold: 90
    remediation:
      enabled: true
      strategy: disk-cleanup
      cooldown: 10m

# Exporter configurations
exporters:
  kubernetes:
    enabled: true
    updateInterval: 10s
    resyncInterval: 60s
    heartbeatInterval: 5m

  http:
    enabled: true
    hostPort: 8080
    bindAddress: "0.0.0.0"
    tlsEnabled: false
    endpoints:
      - path: /health
        handler: overall-health
      - path: /ready
        handler: readiness
      - path: /metrics
        handler: prometheus-metrics
      - path: /status
        handler: detailed-status
      - path: /remediation/history
        handler: remediation-history

  prometheus:
    enabled: true
    port: 9100
    namespace: node_doctor

# Remediation settings
remediation:
  enabled: true
  dryRun: false
  maxRetriesPerHour: 10
  cooldownPeriod: 5m
  circuitBreakerThreshold: 5
  circuitBreakerTimeout: 30m
```

### Configuration Loading

1. **Default configuration**: Embedded in binary
2. **ConfigMap mount**: Primary configuration source
3. **Environment variables**: Override specific values
4. **Command-line flags**: Highest precedence

### Configuration Validation

Validation webhook ensures:
- Required fields are present
- Intervals are valid durations
- Thresholds are within valid ranges
- Remediation strategies exist
- No circular dependencies

## Deployment Architecture

### DaemonSet Deployment

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: node-doctor
  namespace: kube-system
spec:
  selector:
    matchLabels:
      app: node-doctor
  template:
    metadata:
      labels:
        app: node-doctor
    spec:
      hostNetwork: true
      hostPID: true
      dnsPolicy: ClusterFirstWithHostNet
      serviceAccountName: node-doctor
      priorityClassName: system-node-critical

      tolerations:
      - effect: NoSchedule
        operator: Exists
      - effect: NoExecute
        operator: Exists

      containers:
      - name: node-doctor
        image: node-doctor:latest
        imagePullPolicy: IfNotPresent

        securityContext:
          privileged: true
          capabilities:
            add:
            - NET_ADMIN
            - SYS_ADMIN

        env:
        - name: NODE_NAME
          valueFrom:
            fieldRef:
              fieldPath: spec.nodeName

        ports:
        - name: health
          containerPort: 8080
          hostPort: 8080
          protocol: TCP
        - name: metrics
          containerPort: 9100
          hostPort: 9100
          protocol: TCP

        resources:
          requests:
            cpu: 50m
            memory: 128Mi
          limits:
            cpu: 200m
            memory: 256Mi

        volumeMounts:
        - name: config
          mountPath: /etc/node-doctor
        - name: log
          mountPath: /var/log
          readOnly: true
        - name: kmsg
          mountPath: /dev/kmsg
          readOnly: true
        - name: docker-sock
          mountPath: /var/run/docker.sock
        - name: containerd-sock
          mountPath: /run/containerd/containerd.sock
        - name: kubernetes
          mountPath: /etc/kubernetes
          readOnly: true

      volumes:
      - name: config
        configMap:
          name: node-doctor-config
      - name: log
        hostPath:
          path: /var/log
      - name: kmsg
        hostPath:
          path: /dev/kmsg
      - name: docker-sock
        hostPath:
          path: /var/run/docker.sock
      - name: containerd-sock
        hostPath:
          path: /run/containerd/containerd.sock
      - name: kubernetes
        hostPath:
          path: /etc/kubernetes
```

### RBAC Configuration

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: node-doctor
  namespace: kube-system

---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: node-doctor
rules:
- apiGroups: [""]
  resources: ["nodes"]
  verbs: ["get", "patch"]
- apiGroups: [""]
  resources: ["nodes/status"]
  verbs: ["patch"]
- apiGroups: [""]
  resources: ["events"]
  verbs: ["create", "patch"]
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["list"]

---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: node-doctor
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: node-doctor
subjects:
- kind: ServiceAccount
  name: node-doctor
  namespace: kube-system
```

## Package Structure

```
node-doctor/
├── cmd/
│   └── node-doctor/              # Main entry point
│       ├── main.go               # Application bootstrap
│       └── options/              # CLI flag definitions
│           └── options.go
│
├── pkg/
│   ├── types/                    # Core type definitions
│   │   ├── types.go              # Monitor, Status, Event, Condition
│   │   └── config.go             # Configuration types
│   │
│   ├── detector/                 # Problem Detector orchestrator
│   │   ├── detector.go           # Main orchestration logic
│   │   └── aggregator.go         # Problem aggregation/deduplication
│   │
│   ├── monitors/                 # Health check monitors
│   │   ├── registry.go           # Monitor registration
│   │   ├── base.go               # Base monitor implementation
│   │   │
│   │   ├── system/               # System health monitors
│   │   │   ├── cpu.go
│   │   │   ├── memory.go
│   │   │   └── disk.go
│   │   │
│   │   ├── network/              # Network health monitors
│   │   │   ├── dns.go
│   │   │   ├── gateway.go
│   │   │   └── connectivity.go
│   │   │
│   │   ├── kubernetes/           # Kubernetes component monitors
│   │   │   ├── kubelet.go
│   │   │   ├── apiserver.go
│   │   │   └── runtime.go
│   │   │
│   │   └── custom/               # Custom plugin execution
│   │       └── plugin.go
│   │
│   ├── remediators/              # Remediation implementations
│   │   ├── registry.go           # Remediator registry with safety
│   │   ├── interface.go          # Remediator interface
│   │   ├── systemd.go            # Service management
│   │   ├── network.go            # Network remediation
│   │   ├── disk.go               # Disk cleanup
│   │   ├── runtime.go            # Container runtime
│   │   └── custom.go             # Custom scripts
│   │
│   ├── exporters/                # Problem exporters
│   │   ├── interface.go          # Exporter interface
│   │   │
│   │   ├── k8s/                  # Kubernetes exporter
│   │   │   ├── exporter.go
│   │   │   ├── conditions.go     # Condition manager
│   │   │   ├── events.go         # Event reporter
│   │   │   └── annotations.go    # Annotation updater
│   │   │
│   │   ├── http/                 # HTTP health endpoint
│   │   │   ├── server.go
│   │   │   ├── handlers.go
│   │   │   └── status.go
│   │   │
│   │   └── prometheus/           # Prometheus metrics
│   │       ├── exporter.go
│   │       └── metrics.go
│   │
│   └── util/                     # Utility functions
│       ├── config.go             # Configuration loading
│       ├── kube.go               # Kubernetes client helpers
│       ├── log.go                # Logging utilities
│       └── system.go             # System command helpers
│
├── config/                       # Example configurations
│   ├── node-doctor.yaml          # Default configuration
│   └── examples/
│       ├── minimal.yaml
│       ├── full-featured.yaml
│       └── custom-plugins.yaml
│
├── deployment/                   # Kubernetes manifests
│   ├── daemonset.yaml
│   ├── rbac.yaml
│   ├── configmap.yaml
│   └── service.yaml
│
├── test/                         # Tests
│   ├── e2e/                      # End-to-end tests
│   ├── integration/              # Integration tests
│   └── testdata/                 # Test fixtures
│
└── docs/                         # Documentation
    ├── architecture.md           # This document
    ├── monitors.md               # Monitor implementation guide
    ├── remediation.md            # Remediation guide
    └── configuration.md          # Configuration reference
```

## Threading Model

### Concurrency Architecture

```
Main Goroutine
    ├─> Monitor 1 (goroutine)
    │       └─> Periodic health checks
    │       └─> Send Status -> Channel
    │
    ├─> Monitor 2 (goroutine)
    │       └─> Periodic health checks
    │       └─> Send Status -> Channel
    │
    ├─> Monitor N (goroutine)
    │       └─> Periodic health checks
    │       └─> Send Status -> Channel
    │
    └─> Problem Detector (goroutine)
            ├─> Fan-in: Receive from all monitors
            │
            ├─> Process Status
            │       ├─> Aggregate/Deduplicate
            │       └─> Update internal state
            │
            ├─> Export (concurrent)
            │       ├─> K8s Exporter (goroutine)
            │       ├─> HTTP Exporter (main goroutine, HTTP handlers)
            │       └─> Prometheus Exporter (goroutine)
            │
            └─> Remediate (sequential, safety-critical)
                    └─> Remediator Registry
                            ├─> Check cooldown
                            ├─> Check circuit breaker
                            ├─> Execute remediation
                            └─> Update tracking
```

### Synchronization Primitives

- **Channels**: Primary communication mechanism (buffered, size 1000)
- **Mutexes**: Protect shared state in condition manager and remediation registry
- **Context**: Graceful shutdown and timeout management
- **WaitGroups**: Coordinated goroutine shutdown

### Shutdown Sequence

1. Cancel root context
2. Stop all monitors (via context cancellation)
3. Wait for all monitors to finish
4. Drain status channel
5. Stop exporters
6. Final state sync to Kubernetes API
7. Exit

## Error Handling

### Error Propagation

- Monitors: Log errors, continue operation (resilient)
- Exporters: Retry with exponential backoff, eventual consistency
- Remediators: Circuit breaker pattern, fail-safe behavior

### Failure Modes

1. **Monitor Failure**: Other monitors continue, problem logged
2. **API Server Unavailable**: Queue updates, retry on reconnection
3. **Remediation Failure**: Circuit breaker opens, alerts generated
4. **Configuration Error**: Fail-fast on startup
5. **Resource Exhaustion**: Graceful degradation, shed load

## Performance Considerations

### Resource Usage

- **CPU**: Minimal (50m request), spikes during health checks
- **Memory**: ~128Mi baseline, scales with monitor count
- **Network**: Low (periodic API updates), scales with problem frequency
- **Disk**: Minimal (configuration, logs)

### Optimization Strategies

1. **Batch API Updates**: Aggregate multiple changes into single API call
2. **Caching**: Cache node conditions to minimize API reads
3. **Rate Limiting**: Prevent API server overload
4. **Configurable Intervals**: Tune health check frequency
5. **Channel Buffering**: Prevent goroutine blocking

## Security Considerations

### Privilege Requirements

- **Privileged container**: Required for system-level operations
- **hostNetwork**: Required for hostPort and network diagnostics
- **hostPID**: Required for process management
- **CAP_NET_ADMIN**: Required for network remediation
- **CAP_SYS_ADMIN**: Required for system-level operations

### Attack Surface

- **HTTP Endpoints**: No authentication (internal only, hostPort)
- **Configuration**: Read-only ConfigMap, validated on load
- **Remediation**: Limited to predefined actions, no arbitrary code execution
- **API Access**: Restricted by RBAC, minimal permissions

### Mitigation Strategies

1. **Input Validation**: Strict validation of configuration and runtime inputs
2. **Least Privilege**: Minimal RBAC permissions
3. **Audit Logging**: All remediation actions logged as events
4. **Rate Limiting**: Prevent abuse of remediation
5. **Network Policies**: Restrict outbound connections (optional)

## Observability

### Metrics

Prometheus metrics exposed on `:9100/metrics`:

- `node_doctor_monitor_checks_total`: Counter of health checks by monitor and result
- `node_doctor_monitor_duration_seconds`: Histogram of check duration
- `node_doctor_problems_detected_total`: Counter of problems detected
- `node_doctor_remediations_attempted_total`: Counter of remediation attempts
- `node_doctor_remediations_succeeded_total`: Counter of successful remediations
- `node_doctor_circuit_breaker_state`: Gauge of circuit breaker states
- `node_doctor_api_requests_total`: Counter of API requests by operation and result
- `node_doctor_status_channel_size`: Gauge of status channel depth

### Kubernetes Events

Events created for:
- Problem detection (temporary and permanent)
- Remediation attempts (started, succeeded, failed)
- Circuit breaker state changes
- Configuration changes

### Logs

Structured JSON logging with fields:
- `timestamp`: RFC3339 timestamp
- `level`: debug, info, warn, error
- `component`: Which component generated the log
- `monitor`: Monitor name (if applicable)
- `problem`: Problem type (if applicable)
- `action`: Remediation action (if applicable)
- `message`: Human-readable message

## Testing Strategy

### Unit Tests

- All interfaces mocked
- Test individual components in isolation
- Table-driven tests for monitors
- Coverage target: 80%+

### Integration Tests

- Test monitor + exporter combinations
- Test remediation workflows
- Mock Kubernetes API server
- Test configuration loading and validation

### End-to-End Tests

- Deploy to kind/minikube cluster
- Inject real problems
- Verify detection and remediation
- Test all HTTP endpoints
- Verify Kubernetes API updates

### Chaos Testing

- Kill monitors randomly
- Disconnect from API server
- Inject rapid problem flapping
- Resource exhaustion scenarios

## Future Enhancements

### Planned Features

1. **Dynamic Configuration Reload**: Watch ConfigMap, reload without restart
2. **Custom Metrics**: Allow monitors to export custom metrics
3. **Notification Webhooks**: Alert external systems on problems
4. **Multi-Cluster Support**: Aggregate status across clusters
5. **ML-Based Anomaly Detection**: Learn normal patterns, detect deviations
6. **Advanced Remediation**: Node drain, cordon, reboot
7. **Health Check Profiles**: Pre-defined configurations for common use cases
8. **Web UI**: Dashboard for node health visualization

### Extension Points

- Custom monitor plugins (dynamic loading)
- Custom remediator plugins
- Custom exporters
- Webhook integrations
- Custom health check scripts

## References

- [Node Problem Detector](https://github.com/kubernetes/node-problem-detector)
- [Kubernetes Node Conditions](https://kubernetes.io/docs/concepts/architecture/nodes/#condition)
- [Kubernetes Events](https://kubernetes.io/docs/reference/kubernetes-api/cluster-resources/event-v1/)
- [Prometheus Exposition Format](https://prometheus.io/docs/instrumenting/exposition_formats/)
