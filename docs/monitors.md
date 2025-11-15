# Node Doctor Monitors Documentation

This document provides comprehensive information about all monitor types available in Node Doctor, their configuration options, and how to create custom monitors.

## Table of Contents

- [Overview](#overview)
- [Architecture](#architecture)
- [System Monitors](#system-monitors)
  - [CPU Monitor](#cpu-monitor)
  - [Memory Monitor](#memory-monitor)
  - [Disk Monitor](#disk-monitor)
- [Network Monitors](#network-monitors)
  - [DNS Monitor](#dns-monitor)
  - [Gateway Monitor](#gateway-monitor)
  - [Connectivity Monitor](#connectivity-monitor)
- [Kubernetes Monitors](#kubernetes-monitors)
  - [Kubelet Monitor](#kubelet-monitor)
  - [API Server Monitor](#api-server-monitor)
  - [Runtime Monitor](#runtime-monitor)
  - [Capacity Monitor](#capacity-monitor)
- [Custom Monitors](#custom-monitors)
  - [Plugin Monitor](#plugin-monitor)
  - [Log Pattern Monitor](#log-pattern-monitor)
- [Troubleshooting](#troubleshooting)
- [Creating Custom Monitors](#creating-custom-monitors)

---

## Overview

Node Doctor provides a comprehensive set of health monitors for Kubernetes nodes. Each monitor is designed to detect specific types of issues and report them through a unified status reporting system.

**Key Features:**
- **Pluggable Architecture**: Register monitors via the factory pattern
- **Thread-Safe**: All monitors use mutex protection for concurrent operations
- **Context-Based Timeouts**: Proper cancellation and timeout enforcement
- **Failure Threshold Tracking**: Prevent false positives from transient failures
- **Recovery Detection**: Automatically report when issues are resolved
- **Configurable**: Fine-tune behavior through YAML configuration

---

## Architecture

### BaseMonitor Pattern

All monitors extend `BaseMonitor` which provides:
- Lifecycle management (Start/Stop)
- Periodic check scheduling
- Status channel management
- Graceful shutdown handling

```go
type BaseMonitor struct {
    name       string
    interval   time.Duration
    timeout    time.Duration
    statusChan chan *types.Status
    checkFunc  CheckFunc
    logger     Logger
}
```

### Monitor Registry

Monitors self-register at initialization using the registry pattern:

```go
func init() {
    monitors.Register(monitors.MonitorInfo{
        Type:        "system-cpu-check",
        Factory:     NewCPUMonitor,
        Validator:   ValidateCPUConfig,
        Description: "Monitors CPU load average and thermal throttling",
    })
}
```

### Status Reporting

Monitors report health through `types.Status`:

- **Events**: Point-in-time occurrences (Info, Warning, Error)
- **Conditions**: Persistent states (True/False with Reason and Message)

---

## System Monitors

### CPU Monitor

Monitors CPU load average and thermal throttling on the system.

**Monitor Type:** `system-cpu-check`

**Source File:** `pkg/monitors/system/cpu.go:584`

**Configuration:**

```yaml
monitors:
  - name: cpu-health
    type: system-cpu-check
    interval: 30s
    timeout: 5s
    config:
      warningLoadFactor: 0.8        # 80% of CPU cores (auto-validated: 0-100)
      criticalLoadFactor: 1.5       # 150% of CPU cores (auto-validated: 0-100)
      sustainedHighLoadChecks: 3    # Consecutive checks before alerting
      checkThermalThrottle: true
      checkLoadAverage: true
```

**Default Values:**
- `warningLoadFactor`: 0.8 (80%)
- `criticalLoadFactor`: 1.5 (150%)
- `sustainedHighLoadChecks`: 3
- `checkThermalThrottle`: true
- `checkLoadAverage`: true

**Note:** Threshold values (`warningLoadFactor`, `criticalLoadFactor`) are automatically validated to be in the range 0-100. See [Configuration Guide](configuration.md#threshold-validation) for details.

**Key Features:**
- Reads `/proc/loadavg` for 1-minute load average
- Normalizes load by CPU core count
- Sustained high load tracking (prevents alert spam from transient spikes)
- Thermal throttling detection from `/sys/devices/system/cpu/cpu*/thermal_throttle/core_throttle_count`
- Recovery event generation

**Events Generated:**
- `HighCPULoad` (Warning/Error): CPU load exceeds thresholds
- `CPUThermalThrottle` (Warning): Thermal throttling detected
- `CPULoadRecovered` (Info): CPU load returned to normal

**Conditions:**
- `HighCPULoad`: True when sustained high load detected

**Example Status:**

```yaml
events:
  - severity: Warning
    reason: HighCPULoad
    message: "CPU load at 120% (1.5 load factor), exceeds 80% warning threshold"
conditions:
  - type: HighCPULoad
    status: "True"
    reason: SustainedHighLoad
    message: "CPU load has been high for 3 consecutive checks"
```

---

### Memory Monitor

Monitors memory usage, swap usage, and OOM (Out-Of-Memory) kill events.

**Monitor Type:** `system-memory-check`

**Source File:** `pkg/monitors/system/memory.go:653`

**Configuration:**

```yaml
monitors:
  - name: memory-health
    type: system-memory-check
    interval: 30s
    timeout: 5s
    config:
      warningThreshold: 85          # 85% memory usage (auto-validated: 0-100)
      criticalThreshold: 95         # 95% memory usage (auto-validated: 0-100)
      swapWarningThreshold: 50      # 50% swap usage (auto-validated: 0-100)
      swapCriticalThreshold: 80     # 80% swap usage (auto-validated: 0-100)
      sustainedHighMemoryChecks: 3
      checkOOMKills: true
```

**Default Values:**
- `warningThreshold`: 85%
- `criticalThreshold`: 95%
- `swapWarningThreshold`: 50%
- `swapCriticalThreshold`: 80%
- `sustainedHighMemoryChecks`: 3
- `checkOOMKills`: true

**Note:** Threshold values are automatically validated to be in the range 0-100. See [Configuration Guide](configuration.md#threshold-validation) for details.

**Key Features:**
- Reads `/proc/meminfo` for memory statistics
- Monitors `/dev/kmsg` for OOM kill events
- ARM64 compatibility (handles `/dev/kmsg` EINVAL gracefully)
- Sustained high memory tracking
- Swap usage monitoring
- OOM kill detection with process details

**Events Generated:**
- `HighMemoryUsage` (Warning/Error): Memory usage exceeds thresholds
- `HighSwapUsage` (Warning/Error): Swap usage exceeds thresholds
- `OOMKillDetected` (Error): OOM killer has terminated a process
- `MemoryRecovered` (Info): Memory usage returned to normal

**Conditions:**
- `HighMemoryUsage`: True when sustained high memory detected

**Special Notes:**
- **ARM64 /dev/kmsg Issue**: On ARM64 systems, `/dev/kmsg` may return EINVAL errors. The monitor handles this gracefully and continues without OOM kill detection.

**Example Status:**

```yaml
events:
  - severity: Error
    reason: OOMKillDetected
    message: "OOM killer terminated process 'node-doctor' (PID 12345)"
  - severity: Warning
    reason: HighMemoryUsage
    message: "Memory usage at 87.5%, exceeds 85% warning threshold"
```

---

### Disk Monitor

Monitors disk space, inode usage, read-only filesystems, and I/O health across multiple mount points.

**Monitor Type:** `system-disk-check`

**Source File:** `pkg/monitors/system/disk.go:870`

**Configuration:**

```yaml
monitors:
  - name: disk-health
    type: system-disk-check
    interval: 60s
    timeout: 10s
    config:
      mountPoints:
        - path: /
          warningThreshold: 85
          criticalThreshold: 95
          inodeWarningThreshold: 85
          inodeCriticalThreshold: 95
        - path: /var/lib/kubelet
          warningThreshold: 90
          criticalThreshold: 95
      checkReadOnlyFilesystem: true
      checkIOHealth: true
      ioUtilizationWarning: 80      # 80% I/O utilization
      ioUtilizationCritical: 95     # 95% I/O utilization
```

**Default Values per Mount Point:**
- `warningThreshold`: 85%
- `criticalThreshold`: 95%
- `inodeWarningThreshold`: 85%
- `inodeCriticalThreshold`: 95%

**Note:** All threshold values (`warningThreshold`, `criticalThreshold`, `inodeWarningThreshold`, `inodeCriticalThreshold`, `ioUtilizationWarning`, `ioUtilizationCritical`) are automatically validated to be in the range 0-100. See [Configuration Guide](configuration.md#threshold-validation) for details.

**Key Features:**
- Multi-mount point support with individual thresholds
- Space and inode monitoring via `syscall.Statfs()`
- Read-only filesystem detection (remount attempts)
- I/O health monitoring via `/proc/diskstats`
- I/O utilization tracking (percent time device is busy)
- Per-device metrics aggregation

**Events Generated:**
- `HighDiskUsage` (Warning/Error): Disk space exceeds thresholds
- `HighInodeUsage` (Warning/Error): Inode usage exceeds thresholds
- `ReadOnlyFilesystem` (Error): Filesystem is mounted read-only
- `HighIOUtilization` (Warning/Error): I/O utilization exceeds thresholds
- `DiskRecovered` (Info): Disk usage returned to normal

**Conditions:**
- `HighDiskUsage`: True when disk space critical
- `ReadOnlyFilesystem`: True when filesystem is read-only

**Example Status:**

```yaml
events:
  - severity: Error
    reason: HighDiskUsage
    message: "Disk usage at /: 96.2% exceeds 95% critical threshold"
  - severity: Warning
    reason: HighInodeUsage
    message: "Inode usage at /var/lib/kubelet: 87.3% exceeds 85% warning threshold"
```

---

## Network Monitors

### DNS Monitor

Monitors DNS resolution for cluster-internal and external domains.

**Monitor Type:** `network-dns-check`

**Source File:** `pkg/monitors/network/dns.go:300`

**Configuration:**

```yaml
monitors:
  - name: dns-health
    type: network-dns-check
    interval: 30s
    timeout: 10s
    config:
      clusterDomains:
        - kubernetes.default.svc.cluster.local
        - kube-dns.kube-system.svc.cluster.local
      externalDomains:
        - google.com
        - cloudflare.com
      customQueries:
        - domain: api.example.com
          recordType: A
          expectedAnswers: ["192.168.1.100"]
      latencyThreshold: 1s
      failureCountThreshold: 3
```

**Default Values:**
- `clusterDomains`: ["kubernetes.default.svc.cluster.local"]
- `externalDomains`: ["google.com", "cloudflare.com"]
- `latencyThreshold`: 1 second
- `failureCountThreshold`: 3

**Key Features:**
- Cluster and external DNS resolution testing
- Custom DNS query support with expected answer validation
- Latency measurement and threshold alerting
- Nameserver detection from `/etc/resolv.conf`
- Failure threshold to prevent transient alert spam

**Events Generated:**
- `DNSResolutionFailed` (Error): DNS query failed
- `DNSHighLatency` (Warning): DNS query exceeded latency threshold
- `DNSRecovered` (Info): DNS resolution recovered

**Conditions:**
- `DNSUnhealthy`: True when failure threshold exceeded

**Example Status:**

```yaml
events:
  - severity: Error
    reason: DNSResolutionFailed
    message: "Failed to resolve kubernetes.default.svc.cluster.local: no such host"
  - severity: Warning
    reason: DNSHighLatency
    message: "DNS query for google.com took 1.5s, exceeds 1s threshold"
```

---

### Gateway Monitor

Monitors default gateway reachability via ICMP ping.

**Monitor Type:** `network-gateway-check`

**Source File:** `pkg/monitors/network/gateway.go:300`

**Configuration:**

```yaml
monitors:
  - name: gateway-health
    type: network-gateway-check
    interval: 30s
    timeout: 5s
    config:
      autoDetectGateway: true       # Auto-detect from /proc/net/route
      manualGateway: ""             # Override with specific IP
      pingCount: 3
      pingTimeout: 1s
      latencyThreshold: 100ms
      failureCountThreshold: 3
```

**Default Values:**
- `autoDetectGateway`: true
- `pingCount`: 3
- `pingTimeout`: 1 second
- `latencyThreshold`: 100ms
- `failureCountThreshold`: 3

**Key Features:**
- Auto-detection of default gateway from `/proc/net/route`
- ICMP echo request/reply (ping)
- Average latency calculation
- Packet loss detection
- Manual gateway override option

**Events Generated:**
- `GatewayUnreachable` (Error): Cannot reach default gateway
- `GatewayHighLatency` (Warning): Gateway latency exceeds threshold
- `GatewayRecovered` (Info): Gateway reachability restored

**Conditions:**
- `GatewayUnreachable`: True when failure threshold exceeded

**Example Status:**

```yaml
events:
  - severity: Error
    reason: GatewayUnreachable
    message: "Default gateway 192.168.1.1 is unreachable (0/3 packets received)"
  - severity: Warning
    reason: GatewayHighLatency
    message: "Gateway latency 150ms exceeds 100ms threshold"
```

---

### Connectivity Monitor

Monitors HTTP/HTTPS endpoint connectivity for external services and APIs.

**Monitor Type:** `network-connectivity-check`

**Source File:** `pkg/monitors/network/connectivity.go:300`

**Configuration:**

```yaml
monitors:
  - name: connectivity-health
    type: network-connectivity-check
    interval: 60s
    timeout: 30s
    config:
      endpoints:
        - url: https://kubernetes.default.svc.cluster.local
          name: kubernetes-api
          method: GET
          expectedStatusCode: 200
          timeout: 10s
          followRedirects: false
          headers:
            Authorization: "Bearer ${TOKEN}"
        - url: https://registry.example.com/v2/
          name: container-registry
          method: HEAD
          expectedStatusCode: 200
```

**Default Values per Endpoint:**
- `method`: HEAD (safe, no response body)
- `expectedStatusCode`: 200
- `timeout`: 10 seconds
- `followRedirects`: false

**Key Features:**
- Multiple endpoint monitoring
- HTTP method support: GET, HEAD, OPTIONS (POST/PUT/DELETE restricted for safety)
- Custom headers (e.g., authentication tokens)
- Expected status code validation
- URL and protocol validation (http/https only)
- Resource limits: maximum 50 endpoints

**Events Generated:**
- `EndpointUnreachable` (Error): HTTP request failed
- `EndpointUnexpectedStatus` (Warning): Status code mismatch
- `EndpointRecovered` (Info): Endpoint reachable again

**Security Considerations:**
- Only safe HTTP methods allowed (GET, HEAD, OPTIONS)
- No POST/PUT/DELETE to prevent accidental data modification
- URL validation prevents file:// and other protocols
- Header sanitization in error messages

**Example Status:**

```yaml
events:
  - severity: Error
    reason: EndpointUnreachable
    message: "Failed to reach https://registry.example.com: connection timeout"
  - severity: Warning
    reason: EndpointUnexpectedStatus
    message: "Endpoint kubernetes-api returned 401, expected 200"
```

---

## Kubernetes Monitors

### Kubelet Monitor

Monitors Kubelet health, metrics, and PLEG (Pod Lifecycle Event Generator) performance.

**Monitor Type:** `kubernetes-kubelet-check`

**Source File:** `pkg/monitors/kubernetes/kubelet.go:350`

**Configuration:**

```yaml
monitors:
  - name: kubelet-health
    type: kubernetes-kubelet-check
    interval: 30s
    timeout: 10s
    config:
      healthzURL: http://127.0.0.1:10248/healthz
      metricsURL: http://127.0.0.1:10250/metrics
      checkPLEG: true
      plegThreshold: 5s             # PLEG relist duration threshold
      failureThreshold: 3
      auth:
        type: serviceaccount        # none, serviceaccount, bearer, certificate
        tokenPath: /var/run/secrets/kubernetes.io/serviceaccount/token
        certPath: /path/to/client.crt
        keyPath: /path/to/client.key
        caPath: /path/to/ca.crt
      circuitBreaker:
        enabled: true
        threshold: 5                # Open after 5 consecutive failures
        timeout: 30s                # Half-open after 30s
        maxHalfOpenRequests: 3
```

**Default Values:**
- `healthzURL`: http://127.0.0.1:10248/healthz
- `metricsURL`: http://127.0.0.1:10250/metrics
- `checkPLEG`: true
- `plegThreshold`: 5 seconds
- `failureThreshold`: 3
- `auth.type`: none

**Authentication Types:**
- `none`: No authentication (healthz endpoint)
- `serviceaccount`: Use mounted ServiceAccount token
- `bearer`: Custom bearer token
- `certificate`: mTLS with client certificate

**Circuit Breaker Pattern:**
- **Closed**: Normal operation, requests flow through
- **Open**: Too many failures, fail fast without requests
- **Half-Open**: Testing if service recovered, limited requests

**Key Features:**
- Kubelet /healthz endpoint monitoring
- Metrics endpoint authentication support
- PLEG relist duration monitoring (Kubernetes API responsiveness)
- Circuit breaker for fault protection
- Multiple authentication methods
- TLS verification with custom CA

**Events Generated:**
- `KubeletUnhealthy` (Error): Kubelet healthz check failed
- `KubeletAuthFailure` (Error): Authentication to metrics endpoint failed
- `PLEGSlow` (Warning): PLEG relist duration exceeds threshold
- `KubeletRecovered` (Info): Kubelet health restored

**Conditions:**
- `KubeletUnhealthy`: True when failure threshold exceeded

**Example Status:**

```yaml
events:
  - severity: Warning
    reason: PLEGSlow
    message: "PLEG relist duration 7.2s exceeds 5s threshold"
conditions:
  - type: KubeletUnhealthy
    status: "True"
    reason: HealthzFailed
    message: "Kubelet healthz endpoint failed 3 consecutive checks"
```

---

### API Server Monitor

Monitors Kubernetes API server connectivity, latency, and authentication.

**Monitor Type:** `kubernetes-apiserver-check`

**Source File:** `pkg/monitors/kubernetes/apiserver.go:514`

**Configuration:**

```yaml
monitors:
  - name: apiserver-health
    type: kubernetes-apiserver-check
    interval: 30s
    timeout: 15s
    config:
      endpoint: https://kubernetes.default.svc.cluster.local
      latencyThreshold: 2s
      checkVersion: true
      checkAuth: true
      failureThreshold: 3
      httpTimeout: 10s
```

**Default Values:**
- `endpoint`: https://kubernetes.default.svc.cluster.local (in-cluster)
- `latencyThreshold`: 2 seconds
- `checkVersion`: true
- `checkAuth`: true
- `failureThreshold`: 3
- `httpTimeout`: 10 seconds

**Key Features:**
- Uses Kubernetes client-go for API interaction
- In-cluster ServiceAccount authentication (automatic)
- GET /version endpoint for lightweight health checking
- Latency measurement and threshold alerting
- Authentication failure detection (401/403)
- Rate limiting detection (429)
- Error sanitization (prevents token leakage in logs)

**Events Generated:**
- `APIServerUnreachable` (Error): Cannot reach API server
- `APIServerSlow` (Warning): API latency exceeds threshold
- `APIServerAuthFailure` (Error): Authentication failed
- `APIServerRateLimited` (Warning): Rate limit detected
- `APIServerRecovered` (Info): API server reachable again

**Conditions:**
- `APIServerUnreachable`: True when failure threshold exceeded

**Security:**
- Error message sanitization prevents sensitive data leakage
- No authentication tokens in logs or events
- Generic error messages for auth/TLS failures

**Example Status:**

```yaml
events:
  - severity: Error
    reason: APIServerUnreachable
    message: "API server unreachable after 3 consecutive failures: connection refused"
  - severity: Warning
    reason: APIServerSlow
    message: "API server latency 3.5s exceeds threshold 2.0s"
```

---

### Runtime Monitor

Monitors container runtime health (Docker, containerd, CRI-O).

**Monitor Type:** `kubernetes-runtime-check`

**Source File:** `pkg/monitors/kubernetes/runtime.go:618`

**Configuration:**

```yaml
monitors:
  - name: runtime-health
    type: kubernetes-runtime-check
    interval: 30s
    timeout: 10s
    config:
      runtimeType: auto             # auto, docker, containerd, crio
      dockerSocket: /var/run/docker.sock
      containerdSocket: /run/containerd/containerd.sock
      crioSocket: /var/run/crio/crio.sock
      checkSocketConnectivity: true
      checkSystemdStatus: true
      checkRuntimeInfo: true
      failureThreshold: 3
      timeout: 5s
```

**Default Values:**
- `runtimeType`: auto (auto-detect)
- Socket paths: Standard locations for each runtime
- `checkSocketConnectivity`: true
- `checkSystemdStatus`: true
- `checkRuntimeInfo`: true
- `failureThreshold`: 3
- `timeout`: 5 seconds

**Runtime Auto-Detection:**
1. Check for Docker socket at `/var/run/docker.sock`
2. Check for containerd socket at `/run/containerd/containerd.sock`
3. Check for CRI-O socket at `/var/run/crio/crio.sock`
4. Use first detected runtime

**Check Types:**
1. **Socket Connectivity**: Unix socket connection test
2. **Systemd Status**: `systemctl is-active <service>` check
3. **Runtime Info**: Basic API connectivity verification

**Key Features:**
- Multi-runtime support with auto-detection
- Socket accessibility testing
- Systemd service health checking
- Systemd state awareness (active, inactive, failed, activating, etc.)
- Custom socket path override
- Graceful handling of missing runtimes

**Events Generated:**
- `RuntimeSocketUnreachable` (Warning): Cannot connect to runtime socket
- `RuntimeSystemdInactive` (Warning): Systemd service not active
- `RuntimeInfoFailed` (Warning): Failed to retrieve runtime info
- `RuntimeHealthy` (Info): All runtime checks passed
- `RuntimeRecovered` (Info): Runtime health restored

**Conditions:**
- `ContainerRuntimeUnhealthy`: True when failure threshold exceeded

**Example Status:**

```yaml
events:
  - severity: Warning
    reason: RuntimeSystemdInactive
    message: "Container runtime systemd service (docker) is not active"
conditions:
  - type: ContainerRuntimeUnhealthy
    status: "True"
    reason: HealthCheckFailed
    message: "Container runtime (docker) has failed health checks for 3 consecutive attempts"
```

---

### Capacity Monitor

Monitors pod capacity on Kubernetes nodes and alerts when approaching limits.

**Monitor Type:** `kubernetes-capacity-check`

**Source File:** `pkg/monitors/kubernetes/capacity.go:512`

**Configuration:**

```yaml
monitors:
  - name: capacity-health
    type: kubernetes-capacity-check
    interval: 60s
    timeout: 15s
    config:
      nodeName: ""                  # Auto-detected from NODE_NAME env var
      warningThreshold: 90          # 90% capacity
      criticalThreshold: 95         # 95% capacity
      failureThreshold: 3
      apiTimeout: 10s
      checkAllocatable: true        # Use allocatable vs capacity
```

**Default Values:**
- `nodeName`: Auto-detected from `NODE_NAME` environment variable
- `warningThreshold`: 90%
- `criticalThreshold`: 95%
- `failureThreshold`: 3
- `apiTimeout`: 10 seconds
- `checkAllocatable`: true

**Note:** Threshold values (`warningThreshold`, `criticalThreshold`) are automatically validated to be in the range 0-100. See [Configuration Guide](configuration.md#threshold-validation) for details.

**Allocatable vs Capacity:**
- **Allocatable**: Pods actually schedulable (capacity minus system reservations)
- **Capacity**: Total pod slots on the node

**Key Features:**
- Kubernetes API integration for pod counting
- Only counts pods in Running phase
- Node name auto-detection from environment
- Separate warning and critical thresholds
- State transition tracking (normal → warning → critical)
- Recovery event generation
- In-cluster authentication via ServiceAccount

**Events Generated:**
- `PodCapacityWarning` (Warning): Capacity 90-94%
- `PodCapacityPressure` (Error): Capacity ≥95%
- `PodCapacityRecovered` (Info): Capacity returned to normal
- `CapacityCheckFailed` (Error): Failed to query capacity

**Conditions:**
- `PodCapacityPressure`: True when critical threshold exceeded
- `PodCapacityUnhealthy`: True when repeated check failures

**Example Status:**

```yaml
events:
  - severity: Error
    reason: PodCapacityPressure
    message: "Node pod capacity at 96.5% (110/114 pods)"
conditions:
  - type: PodCapacityPressure
    status: "True"
    reason: HighPodUtilization
    message: "Pod capacity at 96.5% (110/114), exceeds 95% threshold"
```

---

## Custom Monitors

### Plugin Monitor

Executes custom external plugins for health checking with JSON or simple output formats.

**Monitor Type:** `custom-plugin-check`

**Source File:** `pkg/monitors/custom/plugin.go:300`

**Configuration:**

```yaml
monitors:
  - name: custom-gpu-health
    type: custom-plugin-check
    interval: 60s
    timeout: 30s
    config:
      pluginPath: /usr/local/bin/check-gpu-health
      args:
        - --verbose
        - --threshold=80
      outputFormat: json            # json or simple
      failureThreshold: 3
      apiTimeout: 10s
      env:
        GPU_DEVICE: "0"
        LOG_LEVEL: "info"
```

**Default Values:**
- `outputFormat`: json
- `failureThreshold`: 3
- `apiTimeout`: 10 seconds

**Output Formats:**

1. **JSON Output:**
```json
{
  "status": "healthy",
  "message": "GPU utilization at 45%",
  "events": [
    {
      "severity": "info",
      "reason": "GPUNormal",
      "message": "GPU temperature: 65C"
    }
  ]
}
```

Valid status values: `healthy`, `warning`, `critical`, `unknown`

2. **Simple Output:**
```
OK: GPU utilization at 45%
```

Exit codes:
- 0: Healthy
- 1: Warning
- 2: Critical
- Other: Unknown/Error

**Key Features:**
- External plugin execution with timeout
- JSON and simple (Nagios-style) output parsing
- Custom environment variable support
- Command-line argument passing
- Failure threshold tracking
- Plugin state validation
- Error handling and recovery

**Events Generated:**
- Plugin-defined events (from JSON output)
- `PluginCheckFailed` (Error): Plugin execution failed
- `PluginUnknownState` (Warning): Plugin returned unknown status

**Conditions:**
- `PluginUnhealthy`: True when plugin reports critical or repeated failures

**Example Plugin (Bash):**

```bash
#!/bin/bash
# GPU Health Check Plugin

gpu_temp=$(nvidia-smi --query-gpu=temperature.gpu --format=csv,noheader)

if [ "$gpu_temp" -lt 80 ]; then
    status="healthy"
    severity="info"
elif [ "$gpu_temp" -lt 90 ]; then
    status="warning"
    severity="warning"
else
    status="critical"
    severity="error"
fi

cat <<EOF
{
  "status": "$status",
  "message": "GPU temperature: ${gpu_temp}C",
  "events": [
    {
      "severity": "$severity",
      "reason": "GPUTemperature",
      "message": "Current GPU temperature: ${gpu_temp}C"
    }
  ]
}
EOF
```

---

### Log Pattern Monitor

Monitors system logs for specific patterns with ReDoS protection and deduplication.

**Monitor Type:** `custom-logpattern-check`

**Source File:** `pkg/monitors/custom/logpattern.go:350`

**Configuration:**

```yaml
monitors:
  - name: log-pattern-health
    type: custom-logpattern-check
    interval: 30s
    timeout: 10s
    config:
      useDefaults: true             # Include default critical patterns
      patterns:
        - pattern: 'kernel: Out of memory'
          severity: error
          reason: OOMDetected
          message: "Out of memory condition detected in kernel logs"
        - pattern: 'segmentation fault'
          severity: warning
          reason: SegFault
          message: "Segmentation fault detected"
        - pattern: 'DENIED'
          severity: info
          reason: SELinuxDenial
          message: "SELinux denial detected"
      kmsgPath: /dev/kmsg
      checkKmsg: true
      journalUnits:
        - kubelet
        - docker
        - containerd
      checkJournal: true
      maxEventsPerPattern: 10       # Max events per pattern per check (1-1000)
      dedupWindow: 5m               # Deduplication window (1s-1h)
```

**Default Values:**
- `useDefaults`: true
- `kmsgPath`: /dev/kmsg
- `checkKmsg`: true
- `checkJournal`: true
- `maxEventsPerPattern`: 10 (range: 1-1000)
- `dedupWindow`: 5 minutes (range: 1s-1h)

**Default Patterns (when useDefaults=true):**
- OOM kills: `killed process|Out of memory|oom-kill`
- Kernel panics: `Kernel panic|BUG: unable to handle`
- Hardware errors: `Machine check events|Hardware Error`
- Filesystem errors: `EXT4-fs error|XFS.*error|I/O error`
- Network errors: `Link is Down|NIC Link is Down`

**Resource Limits:**
- Maximum 50 patterns
- Maximum 20 journal units
- Pattern complexity scoring to prevent ReDoS
- Deduplication window: 1 second to 1 hour

**ReDoS Protection:**

The monitor validates regex patterns for safety:

1. **Complexity Scoring**: Assigns penalty points for risky patterns
   - Nested quantifiers: `(a+)+` (high risk)
   - Overlapping alternations: `(a|ab)*` (moderate risk)
   - Greedy quantifiers: `.*` (low risk)
2. **Threshold Rejection**: Patterns exceeding complexity score rejected
3. **Timeout Enforcement**: Context-based timeout for regex matching

**Key Features:**
- Kernel message monitoring (`/dev/kmsg`)
- Systemd journal monitoring (multiple units)
- Regex pattern matching with safety validation
- Deduplication to prevent event flooding
- Custom pattern support
- Default critical pattern library
- Event rate limiting per pattern
- ARM64 /dev/kmsg compatibility

**Events Generated:**
- Pattern-defined events (custom severity/reason/message)
- `LogPatternCheckFailed` (Error): Failed to read logs

**Example Status:**

```yaml
events:
  - severity: Error
    reason: OOMDetected
    message: "Out of memory condition detected in kernel logs"
  - severity: Warning
    reason: SegFault
    message: "Segmentation fault detected (2 occurrences in last 5m)"
```

---

## Troubleshooting

### Common Issues

#### 1. Monitor Not Starting

**Symptom:** Monitor appears in config but doesn't generate status updates

**Possible Causes:**
- Invalid configuration (check validation errors in logs)
- Timeout exceeds interval
- Missing dependencies (e.g., Kubernetes client for API server monitor)

**Solution:**
```bash
# Check logs for validation errors
journalctl -u node-doctor -f | grep -i error

# Verify configuration
node-doctor validate --config /etc/node-doctor/config.yaml

# Ensure timeout < interval
# timeout: 10s
# interval: 30s  # Good: interval > timeout
```

#### 2. Permission Denied Errors

**Symptom:** Monitor fails with "permission denied" errors

**Common Locations:**
- `/dev/kmsg`: Requires CAP_SYSLOG or root
- `/proc/diskstats`: Requires read access
- Container runtime sockets: Requires socket access

**Solution:**
```yaml
# DaemonSet securityContext
securityContext:
  privileged: true  # Or specific capabilities
  capabilities:
    add:
      - SYS_ADMIN
      - SYS_RESOURCE
```

#### 3. High Memory Usage

**Symptom:** Node Doctor consuming excessive memory

**Possible Causes:**
- Too many log pattern monitors
- Large deduplication windows
- Too many custom patterns

**Solution:**
```yaml
# Reduce deduplication window
dedupWindow: 1m  # Instead of 1h

# Limit patterns
maxEventsPerPattern: 5  # Instead of 100

# Reduce check frequency
interval: 60s  # Instead of 10s
```

#### 4. Missing Events

**Symptom:** Expected events not appearing

**Troubleshooting Steps:**

1. **Check Monitor Status:**
```bash
# View monitor list
kubectl exec -it <pod> -- node-doctor monitors list

# Check specific monitor
kubectl exec -it <pod> -- node-doctor monitors status <monitor-name>
```

2. **Verify Thresholds:**
```yaml
# Lower thresholds for testing
warningThreshold: 50  # Instead of 85
criticalThreshold: 75  # Instead of 95
```

3. **Check Failure Threshold:**
```yaml
# Reduce to see events sooner
failureThreshold: 1  # Instead of 3
```

4. **Enable Debug Logging:**
```yaml
# In config.yaml
logging:
  level: debug
```

#### 5. Certificate/TLS Errors

**Symptom:** TLS handshake failures, certificate verification errors

**Kubelet Monitor:**
```yaml
config:
  auth:
    type: certificate
    certPath: /path/to/client.crt
    keyPath: /path/to/client.key
    caPath: /path/to/ca.crt  # Ensure CA matches server cert
```

**API Server Monitor:**
```yaml
# Verify ServiceAccount token is mounted
volumeMounts:
  - name: serviceaccount
    mountPath: /var/run/secrets/kubernetes.io/serviceaccount
    readOnly: true
```

#### 6. ARM64 Specific Issues

**Symptom:** `/dev/kmsg` EINVAL errors on ARM64 nodes

**Solution:** This is expected behavior. The Memory Monitor handles this gracefully:

```yaml
# OOM kill detection automatically disabled on ARM64 if /dev/kmsg fails
checkOOMKills: true  # Will gracefully skip on ARM64
```

---

## Creating Custom Monitors

### Step 1: Define Monitor Structure

Create a new package under `pkg/monitors/`:

```go
package custom

import (
    "context"
    "github.com/supporttools/node-doctor/pkg/monitors"
    "github.com/supporttools/node-doctor/pkg/types"
)

type CustomMonitorConfig struct {
    // Your configuration fields
    Threshold float64 `json:"threshold"`
}

type CustomMonitor struct {
    *monitors.BaseMonitor
    config *CustomMonitorConfig
}
```

### Step 2: Implement Factory Function

```go
func NewCustomMonitor(ctx context.Context, config types.MonitorConfig) (types.Monitor, error) {
    // Validate configuration
    if err := ValidateCustomConfig(config); err != nil {
        return nil, fmt.Errorf("invalid configuration: %w", err)
    }

    // Parse custom configuration
    customConfig, err := parseCustomConfig(config.Config)
    if err != nil {
        return nil, fmt.Errorf("failed to parse config: %w", err)
    }

    // Create base monitor
    baseMonitor, err := monitors.NewBaseMonitor(config.Name, config.Interval, config.Timeout)
    if err != nil {
        return nil, fmt.Errorf("failed to create base monitor: %w", err)
    }

    // Create custom monitor
    monitor := &CustomMonitor{
        BaseMonitor: baseMonitor,
        config:      customConfig,
    }

    // Set check function
    if err := baseMonitor.SetCheckFunc(monitor.check); err != nil {
        return nil, fmt.Errorf("failed to set check function: %w", err)
    }

    return monitor, nil
}
```

### Step 3: Implement Check Function

```go
func (m *CustomMonitor) check(ctx context.Context) (*types.Status, error) {
    status := types.NewStatus(m.GetName())

    // Perform your health check logic here
    value := m.doHealthCheck(ctx)

    // Evaluate against threshold
    if value > m.config.Threshold {
        status.AddEvent(types.NewEvent(
            types.EventWarning,
            "ThresholdExceeded",
            fmt.Sprintf("Value %.2f exceeds threshold %.2f", value, m.config.Threshold),
        ))

        status.AddCondition(types.NewCondition(
            "Unhealthy",
            types.ConditionTrue,
            "ThresholdExceeded",
            fmt.Sprintf("Custom metric exceeded threshold"),
        ))
    }

    return status, nil
}
```

### Step 4: Register Monitor

```go
func init() {
    monitors.Register(monitors.MonitorInfo{
        Type:        "custom-mycheck",
        Factory:     NewCustomMonitor,
        Validator:   ValidateCustomConfig,
        Description: "Monitors custom metric",
    })
}
```

### Step 5: Add Configuration Parsing

```go
func parseCustomConfig(configMap map[string]interface{}) (*CustomMonitorConfig, error) {
    config := &CustomMonitorConfig{}

    if val, ok := configMap["threshold"]; ok {
        switch v := val.(type) {
        case float64:
            config.Threshold = v
        case int:
            config.Threshold = float64(v)
        default:
            return nil, fmt.Errorf("threshold must be a number")
        }
    } else {
        config.Threshold = 80.0 // Default
    }

    return config, nil
}
```

### Step 6: Add Validation

```go
func ValidateCustomConfig(config types.MonitorConfig) error {
    if config.Name == "" {
        return fmt.Errorf("monitor name is required")
    }

    if config.Type != "custom-mycheck" {
        return fmt.Errorf("invalid monitor type: %s", config.Type)
    }

    // Validate custom fields
    customConfig, err := parseCustomConfig(config.Config)
    if err != nil {
        return err
    }

    if customConfig.Threshold < 0 || customConfig.Threshold > 100 {
        return fmt.Errorf("threshold must be between 0 and 100")
    }

    return nil
}
```

### Best Practices

1. **Thread Safety**: Use mutexes for shared state
2. **Context Awareness**: Respect context cancellation
3. **Error Handling**: Return meaningful errors, don't panic
4. **Logging**: Use structured logging for debugging
5. **Testing**: Write unit tests with mock dependencies
6. **Documentation**: Document configuration fields and defaults
7. **Validation**: Validate configuration early (fail fast)
8. **Resource Cleanup**: Clean up resources in stop logic
9. **Failure Tracking**: Implement consecutive failure thresholds
10. **Recovery Events**: Report when issues resolve

### Testing Your Monitor

```go
func TestCustomMonitor(t *testing.T) {
    config := types.MonitorConfig{
        Name:     "test-custom",
        Type:     "custom-mycheck",
        Interval: 30 * time.Second,
        Timeout:  5 * time.Second,
        Config: map[string]interface{}{
            "threshold": 75.0,
        },
    }

    monitor, err := NewCustomMonitor(context.Background(), config)
    if err != nil {
        t.Fatalf("Failed to create monitor: %v", err)
    }

    // Start monitor
    statusChan := monitor.Start()

    // Wait for first status
    select {
    case status := <-statusChan:
        // Verify status
        if status.Source != "test-custom" {
            t.Errorf("Expected source 'test-custom', got '%s'", status.Source)
        }
    case <-time.After(10 * time.Second):
        t.Fatal("Timeout waiting for status")
    }

    // Stop monitor
    monitor.Stop()
}
```

---

## Summary

Node Doctor provides a comprehensive monitoring framework with:

- **11 Built-in Monitors** covering system, network, Kubernetes, and custom checks
- **Pluggable Architecture** for easy extension
- **Thread-Safe Design** for production reliability
- **Failure Threshold Tracking** to prevent false positives
- **Recovery Detection** for automatic issue resolution reporting
- **Flexible Configuration** via YAML with sensible defaults

For additional support, see:
- [Configuration Guide](configuration.md)
- [Remediation System](remediation.md)
- [Architecture Overview](architecture.md)
