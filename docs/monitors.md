# Node Doctor Monitors

## Overview

Monitors are the core components responsible for detecting problems on Kubernetes nodes. Each monitor focuses on a specific aspect of node health and runs independently in its own goroutine, performing periodic health checks and reporting problems via Go channels.

## Monitor Interface

All monitors implement the following interface:

```go
type Monitor interface {
    // Start starts the monitor and returns a channel for status updates
    // Returns nil channel for metrics-only monitors
    Start() (<-chan *Status, error)

    // Stop gracefully stops the monitor
    Stop()
}
```

## Monitor Categories

### 1. System Health Monitors

Monitor core system resources and kernel health.

#### CPU Monitor

**Purpose**: Detect CPU pressure, thermal throttling, and high load

**Checks**:
- CPU load average (1min, 5min, 15min)
- CPU usage per core
- Thermal throttling events
- CPU frequency scaling
- Process count

**Thresholds**:
- Warning: Load average > 0.8 * CPU cores
- Critical: Load average > 1.5 * CPU cores
- Thermal: Frequency scaled below 50% of max

**Problems Reported**:
- `CPUPressure` (permanent condition)
- `ThermalThrottling` (temporary event)
- `HighCPULoad` (temporary event)

**Configuration**:
```yaml
- name: cpu-health
  type: system-cpu-check
  enabled: true
  interval: 30s
  config:
    warningLoadFactor: 0.8
    criticalLoadFactor: 1.5
    checkThermalEvents: true
```

**Implementation Notes**:
- Read `/proc/loadavg` for load average
- Read `/proc/stat` for CPU usage
- Read `/sys/devices/system/cpu/cpu*/cpufreq/scaling_cur_freq` for throttling
- Parse `/proc/cpuinfo` for CPU count

---

#### Memory Monitor

**Purpose**: Detect memory pressure, OOM conditions, and swap usage

**Checks**:
- Available memory
- Memory pressure (PSI)
- Swap usage
- OOM kills (from kernel logs)
- Memory cgroups

**Thresholds**:
- Warning: Available < 20% total
- Critical: Available < 10% total
- Swap: Swap usage > 50%

**Problems Reported**:
- `MemoryPressure` (permanent condition)
- `OOMKilling` (temporary event)
- `HighSwapUsage` (temporary event)

**Configuration**:
```yaml
- name: memory-health
  type: system-memory-check
  enabled: true
  interval: 30s
  config:
    warningThreshold: 20  # percent
    criticalThreshold: 10  # percent
    swapThreshold: 50      # percent
    checkOOMKills: true
```

**Implementation Notes**:
- Read `/proc/meminfo` for memory stats
- Read `/proc/pressure/memory` for PSI (if available)
- Watch `/dev/kmsg` for OOM kill messages
- Parse cgroup memory stats

---

#### Disk Monitor

**Purpose**: Detect disk space, inode exhaustion, and I/O issues

**Checks**:
- Disk space usage per mount point
- Inode usage
- Disk I/O utilization
- Read-only filesystems
- Disk errors (SMART)

**Thresholds**:
- Warning: Usage > 85%
- Critical: Usage > 95%
- Inode Warning: > 90%

**Problems Reported**:
- `DiskPressure` (permanent condition)
- `ReadonlyFilesystem` (permanent condition)
- `InodeExhaustion` (temporary event)
- `DiskErrors` (temporary event)

**Configuration**:
```yaml
- name: disk-health
  type: system-disk-check
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
      - path: "/var/lib/kubelet"
        warningThreshold: 80
        criticalThreshold: 90
    checkInodes: true
    checkReadonly: true
    checkSMART: false
```

**Implementation Notes**:
- Use `syscall.Statfs()` for disk space
- Check `/proc/mounts` for mount options (ro)
- Use `smartctl` for SMART errors (if enabled)
- Monitor `/proc/diskstats` for I/O errors

---

### 2. Network Health Monitors

Monitor network connectivity and DNS resolution.

#### DNS Monitor

**Purpose**: Detect DNS resolution failures

**Checks**:
- Cluster DNS (kubernetes.default.svc.cluster.local)
- External DNS (google.com, cloudflare.com)
- Custom DNS queries
- DNS server reachability
- DNS latency

**Thresholds**:
- Failure: Any resolution failure
- Latency: > 1 second

**Problems Reported**:
- `ClusterDNSDown` (permanent condition)
- `ExternalDNSDown` (temporary event)
- `HighDNSLatency` (temporary event)

**Configuration**:
```yaml
- name: dns-health
  type: network-dns-check
  enabled: true
  interval: 60s
  timeout: 5s
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
    nameservers:
      - /etc/resolv.conf
```

**Implementation Notes**:
- Use `net.LookupHost()` for DNS queries
- Measure query latency
- Test both IPv4 and IPv6 (if enabled)
- Verify nameserver reachability with ping

---

#### Gateway Monitor

**Purpose**: Detect default gateway reachability

**Checks**:
- Default gateway ICMP ping
- Gateway ARP table entry
- Route table validity

**Thresholds**:
- Failure: 3 consecutive ping failures
- Latency: > 100ms

**Problems Reported**:
- `GatewayUnreachable` (permanent condition)
- `HighGatewayLatency` (temporary event)

**Configuration**:
```yaml
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
```

**Implementation Notes**:
- Parse `/proc/net/route` for default gateway
- Use `net` package for ICMP ping
- Measure RTT for latency

---

#### Connectivity Monitor

**Purpose**: Detect external network connectivity

**Checks**:
- HTTP/HTTPS connectivity to well-known sites
- Custom endpoint checks
- Network interface status
- IP address assignment

**Thresholds**:
- Failure: Unable to reach any endpoint

**Problems Reported**:
- `NetworkUnreachable` (permanent condition)
- `InterfaceDown` (permanent condition)

**Configuration**:
```yaml
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
      - url: https://kubernetes.io
        method: GET
        expectedStatusCode: 200
    interfaces:
      - name: eth0
        expectUp: true
      - name: docker0
        expectUp: true
```

**Implementation Notes**:
- Use `http.Client` with timeout
- Check `/sys/class/net/*/operstate` for interface status
- Verify IP assignment with `netlink`

---

### 3. Kubernetes Component Monitors

Monitor Kubernetes components and container runtime.

#### Kubelet Monitor

**Purpose**: Detect kubelet health issues

**Checks**:
- Kubelet `/healthz` endpoint
- Kubelet `/metrics` endpoint
- Kubelet systemd service status
- Kubelet log errors
- PLEG (Pod Lifecycle Event Generator) health

**Thresholds**:
- Failure: healthz returns non-200
- Degraded: PLEG > 5 seconds

**Problems Reported**:
- `KubeletUnhealthy` (permanent condition)
- `KubeletDown` (permanent condition)
- `PLEGSlow` (temporary event)

**Configuration**:
```yaml
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
  remediation:
    enabled: true
    strategy: systemd-restart
    service: kubelet
    cooldown: 5m
    maxAttempts: 3
```

**Implementation Notes**:
- HTTP GET to healthz endpoint
- Parse Prometheus metrics for PLEG
- Check systemd with `systemctl is-active kubelet`
- Monitor kubelet logs for errors

---

#### API Server Monitor

**Purpose**: Detect kube-apiserver connectivity issues

**Checks**:
- API server reachability
- API server latency
- API server authentication
- API server version
- Rate limit status

**Thresholds**:
- Failure: Connection refused or timeout
- Degraded: Latency > 2 seconds

**Problems Reported**:
- `APIServerUnreachable` (permanent condition)
- `APIServerSlow` (temporary event)
- `APIServerAuthFailure` (temporary event)

**Configuration**:
```yaml
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
```

**Implementation Notes**:
- Use in-cluster Kubernetes client
- Measure API call latency
- Test with simple `GET /version` request
- Monitor 429 (rate limit) responses

---

#### Container Runtime Monitor

**Purpose**: Detect container runtime (Docker/containerd/CRI-O) health issues

**Checks**:
- Runtime API reachability
- Runtime service status
- Container list operation
- Image list operation
- Runtime version
- Runtime log errors

**Thresholds**:
- Failure: API unreachable or timeout

**Problems Reported**:
- `ContainerRuntimeDown` (permanent condition)
- `RuntimeSlow` (temporary event)

**Configuration**:
```yaml
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
  remediation:
    enabled: true
    strategy: systemd-restart
    cooldown: 10m
    maxAttempts: 2
```

**Implementation Notes**:
- Auto-detect runtime from kubelet flags
- Docker: Use Docker API client
- containerd: Use containerd client (CRI plugin)
- CRI-O: Use CRI client
- Check systemd service status

---

#### Pod Capacity Monitor

**Purpose**: Detect pod scheduling capacity issues

**Checks**:
- Current pod count vs max pods
- Allocatable resources vs capacity
- Node conditions affecting scheduling
- DaemonSet pod health

**Thresholds**:
- Warning: > 80% pod capacity
- Critical: > 95% pod capacity

**Problems Reported**:
- `PodCapacityReached` (temporary event)
- `NodeNotReady` (permanent condition)

**Configuration**:
```yaml
- name: pod-capacity
  type: kubernetes-capacity-check
  enabled: true
  interval: 5m
  config:
    warningThreshold: 80   # percent
    criticalThreshold: 95  # percent
    checkDaemonSets: true
```

**Implementation Notes**:
- Use Kubernetes API to list pods on node
- Read node status for max pods
- Check node allocatable resources

---

### 4. Custom Plugin Monitor

Monitor using custom scripts or executables.

#### Custom Plugin Monitor

**Purpose**: Execute custom health check scripts

**Execution Model**:
- Executes external programs/scripts
- Interprets exit codes
- Captures stdout/stderr
- Enforces timeouts
- Concurrent execution with limits

**Exit Code Convention**:
- `0`: Healthy (no problem)
- `1`: Problem detected
- Other: Unknown/error state

**Problems Reported**: Defined in configuration

**Configuration**:
```yaml
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
    maxOutputLength: 4096
    problem:
      type: permanent
      condition: NTPProblem
      reason: NTPDown
  remediation:
    enabled: true
    strategy: custom-script
    script: /usr/local/bin/fix_ntp.sh
    cooldown: 15m

- name: custom-app-health
  type: custom-plugin-check
  enabled: true
  interval: 2m
  timeout: 30s
  config:
    path: /opt/myapp/health_check.py
    args: ["--verbose"]
    workingDir: /opt/myapp
    problem:
      type: temporary
      reason: AppUnhealthy
```

**Implementation Notes**:
- Use `exec.CommandContext` for timeout
- Capture both stdout and stderr
- Semaphore for concurrency control
- Kill process group on timeout

---

### 5. Log Pattern Monitor

Monitor system logs for error patterns.

#### System Log Monitor

**Purpose**: Detect problems by matching log patterns

**Log Sources**:
- Kernel messages (`/dev/kmsg`)
- Systemd journal
- File logs

**Pattern Matching**:
- Regular expressions
- Multi-line support
- Lookback window
- Rate limiting

**Problems Reported**: Defined by rules

**Configuration**:
```yaml
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

      - type: permanent
        condition: KernelDeadlock
        reason: TaskHung
        pattern: "task .+ blocked for more than \\d+ seconds"
        rateLimit: 5m

- name: docker-monitor
  type: log-pattern-check
  enabled: true
  config:
    source: journald
    unit: docker.service
    lookback: 10m
    rules:
      - type: temporary
        reason: DockerError
        pattern: "Error response from daemon"
```

**Implementation Notes**:
- Use `bufio.Scanner` for line-based reading
- Compile regex patterns at startup
- Circular buffer for lookback
- Deduplication with rate limiting

---

## Monitor Lifecycle

### 1. Registration

Monitors register themselves via the registry pattern:

```go
func init() {
    monitors.Register("system-cpu-check", &monitors.MonitorFactory{
        Create: NewCPUMonitor,
        ValidateConfig: ValidateCPUConfig,
    })
}
```

### 2. Initialization

1. Load configuration from YAML
2. Validate configuration
3. Create monitor instance
4. Initialize resources (channels, timers, etc.)

### 3. Execution

```go
func (m *CPUMonitor) Start() (<-chan *Status, error) {
    m.statusChan = make(chan *Status, 10)
    m.tomb = tomb.Tomb{}

    m.tomb.Go(func() error {
        ticker := time.NewTicker(m.interval)
        defer ticker.Stop()

        for {
            select {
            case <-ticker.C:
                status := m.check()
                if status != nil {
                    m.statusChan <- status
                }
            case <-m.tomb.Dying():
                return nil
            }
        }
    })

    return m.statusChan, nil
}
```

### 4. Shutdown

```go
func (m *CPUMonitor) Stop() {
    m.tomb.Kill(nil)
    m.tomb.Wait()
    close(m.statusChan)
}
```

## Common Patterns

### 1. Base Monitor

Common functionality for all monitors:

```go
type BaseMonitor struct {
    Name       string
    Interval   time.Duration
    Timeout    time.Duration
    StatusChan chan *Status
    Tomb       *tomb.Tomb
}

func (b *BaseMonitor) GetName() string {
    return b.Name
}

func (b *BaseMonitor) sendStatus(status *Status) {
    select {
    case b.StatusChan <- status:
    case <-time.After(b.Timeout):
        log.Warnf("Failed to send status from %s: timeout", b.Name)
    case <-b.Tomb.Dying():
        // Shutting down
    }
}
```

### 2. HTTP Health Check

Common pattern for HTTP-based health checks:

```go
func httpHealthCheck(url string, timeout time.Duration) error {
    client := &http.Client{
        Timeout: timeout,
    }

    resp, err := client.Get(url)
    if err != nil {
        return fmt.Errorf("HTTP request failed: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
    }

    return nil
}
```

### 3. Systemd Service Check

Common pattern for checking systemd services:

```go
func checkSystemdService(service string) (bool, error) {
    cmd := exec.Command("systemctl", "is-active", service)
    output, err := cmd.CombinedOutput()

    if err != nil {
        return false, err
    }

    return strings.TrimSpace(string(output)) == "active", nil
}
```

### 4. File-Based Checks

Common pattern for reading system files:

```go
func readSystemFile(path string) (string, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return "", fmt.Errorf("failed to read %s: %w", path, err)
    }
    return string(data), nil
}

func parseIntFromFile(path string) (int64, error) {
    data, err := readSystemFile(path)
    if err != nil {
        return 0, err
    }
    return strconv.ParseInt(strings.TrimSpace(data), 10, 64)
}
```

## Testing Monitors

### Unit Testing

```go
func TestCPUMonitor(t *testing.T) {
    config := &CPUMonitorConfig{
        Interval: 1 * time.Second,
        WarningLoadFactor: 0.8,
    }

    monitor := NewCPUMonitor(config)
    statusChan, err := monitor.Start()
    require.NoError(t, err)

    // Wait for status
    select {
    case status := <-statusChan:
        assert.Equal(t, "cpu-health", status.Source)
        assert.NotNil(t, status)
    case <-time.After(2 * time.Second):
        t.Fatal("Timeout waiting for status")
    }

    monitor.Stop()
}
```

### Integration Testing

Test monitors against real system:

```go
func TestCPUMonitor_Integration(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration test")
    }

    // Create real monitor
    monitor := NewCPUMonitor(&CPUMonitorConfig{
        Interval: 100 * time.Millisecond,
    })

    statusChan, err := monitor.Start()
    require.NoError(t, err)
    defer monitor.Stop()

    // Collect statuses
    var statuses []*Status
    timeout := time.After(2 * time.Second)

    for {
        select {
        case status := <-statusChan:
            statuses = append(statuses, status)
            if len(statuses) >= 5 {
                goto done
            }
        case <-timeout:
            goto done
        }
    }

done:
    assert.GreaterOrEqual(t, len(statuses), 5)
}
```

### Mock Implementation

```go
type MockMonitor struct {
    Name       string
    StatusChan chan *Status
    StartErr   error
    StopCalled bool
}

func (m *MockMonitor) Start() (<-chan *Status, error) {
    if m.StartErr != nil {
        return nil, m.StartErr
    }
    m.StatusChan = make(chan *Status, 10)
    return m.StatusChan, nil
}

func (m *MockMonitor) Stop() {
    m.StopCalled = true
    close(m.StatusChan)
}
```

## Performance Considerations

### 1. Check Intervals

- **Fast checks** (< 1 second): 30s interval
- **Medium checks** (1-5 seconds): 1-5m interval
- **Slow checks** (> 5 seconds): 5-15m interval
- **Expensive checks**: 15-30m interval

### 2. Resource Usage

- **CPU**: Minimal during idle, spikes during checks
- **Memory**: Bounded by buffer sizes
- **Disk I/O**: Minimal, mostly reads
- **Network**: Only for remote checks

### 3. Concurrency

- Each monitor in separate goroutine
- Plugin monitors use semaphore for concurrency control
- Channel buffers prevent blocking

### 4. Optimization Tips

- Cache expensive operations
- Use buffered channels
- Set appropriate timeouts
- Implement circuit breakers for failing checks
- Use context for cancellation
- Avoid polling, use event-based when possible

## Troubleshooting

### Monitor Not Starting

1. Check configuration validation errors
2. Verify required files/services exist
3. Check permissions for file/socket access
4. Review initialization logs

### Monitor Not Reporting Problems

1. Verify thresholds are appropriate
2. Check monitor execution with debug logging
3. Test health check logic independently
4. Verify channel communication

### High Resource Usage

1. Reduce check frequency (increase interval)
2. Implement caching for expensive operations
3. Add timeout enforcement
4. Check for resource leaks

### False Positives

1. Adjust thresholds
2. Implement rate limiting
3. Add confirmation checks (multiple failures)
4. Filter transient errors

## Best Practices

1. **Keep checks simple**: Each monitor should do one thing well
2. **Use timeouts**: Every external operation needs a timeout
3. **Handle errors gracefully**: Log and continue, don't crash
4. **Provide context**: Include relevant details in problem messages
5. **Test thoroughly**: Unit, integration, and chaos tests
6. **Document thresholds**: Explain why specific values were chosen
7. **Version configurations**: Track changes to monitor configs
8. **Monitor the monitors**: Track monitor execution time and errors
9. **Progressive severity**: Start with warnings before critical alerts
10. **Avoid noisy monitors**: Use rate limiting and deduplication

## Example: Implementing a New Monitor

### Step 1: Define Configuration

```go
type MyMonitorConfig struct {
    Enabled  bool          `yaml:"enabled"`
    Interval time.Duration `yaml:"interval"`
    Timeout  time.Duration `yaml:"timeout"`
    // Custom fields
    Threshold int `yaml:"threshold"`
}
```

### Step 2: Implement Monitor

```go
type MyMonitor struct {
    BaseMonitor
    config *MyMonitorConfig
}

func NewMyMonitor(config *MyMonitorConfig) *MyMonitor {
    return &MyMonitor{
        BaseMonitor: BaseMonitor{
            Name:     "my-monitor",
            Interval: config.Interval,
            Timeout:  config.Timeout,
        },
        config: config,
    }
}

func (m *MyMonitor) Start() (<-chan *Status, error) {
    m.StatusChan = make(chan *Status, 10)
    m.Tomb = &tomb.Tomb{}

    m.Tomb.Go(func() error {
        return m.run()
    })

    return m.StatusChan, nil
}

func (m *MyMonitor) run() error {
    ticker := time.NewTicker(m.Interval)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            if status := m.check(); status != nil {
                m.sendStatus(status)
            }
        case <-m.Tomb.Dying():
            return nil
        }
    }
}

func (m *MyMonitor) check() *Status {
    // Implement health check logic
    ctx, cancel := context.WithTimeout(context.Background(), m.Timeout)
    defer cancel()

    // Perform check
    healthy, err := m.performCheck(ctx)

    if err != nil {
        log.Errorf("Check failed: %v", err)
        return nil
    }

    if !healthy {
        return &Status{
            Source: m.Name,
            Events: []Event{
                {
                    Severity: "warning",
                    Reason:   "MyProblem",
                    Message:  "Problem detected",
                },
            },
            Timestamp: time.Now(),
        }
    }

    return nil
}

func (m *MyMonitor) Stop() {
    m.Tomb.Kill(nil)
    m.Tomb.Wait()
    close(m.StatusChan)
}
```

### Step 3: Register Monitor

```go
func init() {
    monitors.Register("my-check", &monitors.MonitorFactory{
        Create: func(cfg interface{}) (Monitor, error) {
            config := cfg.(*MyMonitorConfig)
            return NewMyMonitor(config), nil
        },
        ValidateConfig: func(cfg interface{}) error {
            config := cfg.(*MyMonitorConfig)
            if config.Interval <= 0 {
                return fmt.Errorf("interval must be positive")
            }
            return nil
        },
    })
}
```

### Step 4: Add Configuration

```yaml
monitors:
  - name: my-custom-monitor
    type: my-check
    enabled: true
    interval: 1m
    timeout: 10s
    config:
      threshold: 100
```

### Step 5: Write Tests

```go
func TestMyMonitor(t *testing.T) {
    config := &MyMonitorConfig{
        Enabled:  true,
        Interval: 100 * time.Millisecond,
        Timeout:  5 * time.Second,
        Threshold: 50,
    }

    monitor := NewMyMonitor(config)
    ch, err := monitor.Start()
    require.NoError(t, err)
    defer monitor.Stop()

    // Test implementation
}
```

## Summary

Monitors are the eyes of Node Doctor, continuously watching for problems. They follow a consistent pattern:
- Implement the Monitor interface
- Run independently in goroutines
- Report via channels
- Handle errors gracefully
- Support graceful shutdown

By following these patterns and best practices, you can create reliable, efficient, and maintainable health checks for your Kubernetes nodes.
