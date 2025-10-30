# Node Doctor Remediation

## Overview

Remediation is the automated process of fixing detected problems without human intervention. Node Doctor implements a safe, controlled remediation system with multiple safety mechanisms to prevent cascading failures and remediation storms.

## Design Principles

1. **Safety First**: Multiple layers of protection prevent dangerous remediation loops
2. **Fail-Safe**: Errors during remediation should not make things worse
3. **Auditable**: All remediation actions are logged and tracked
4. **Configurable**: Per-problem remediation strategies with fine-grained control
5. **Testable**: Dry-run mode for testing without actual execution
6. **Observable**: Comprehensive metrics and events for monitoring

## Remediator Interface

```go
type Remediator interface {
    // CanRemediate checks if this remediator can handle the problem
    CanRemediate(problem Problem) bool

    // Remediate attempts to fix the problem
    // Returns error if remediation failed
    Remediate(ctx context.Context, problem Problem) error

    // GetCooldown returns the cooldown period for this remediator
    GetCooldown() time.Duration

    // GetMaxAttempts returns max remediation attempts before giving up
    GetMaxAttempts() int

    // GetName returns the remediator name for tracking
    GetName() string
}
```

## Remediation Registry

The Remediation Registry coordinates all remediation activities with safety mechanisms.

### Architecture

```go
type RemediatorRegistry struct {
    // Registered remediators
    remediators []Remediator

    // Safety tracking
    cooldownTracker  map[string]time.Time      // Problem -> last remediation time
    attemptTracker   map[string]int            // Problem -> attempt count
    circuitBreaker   map[string]*CircuitBreaker // Problem -> circuit breaker

    // Global limits
    maxAttemptsGlobal int
    cooldownGlobal    time.Duration
    rateLimiter       *RateLimiter

    // Configuration
    dryRun            bool
    enableRemediation bool

    // Synchronization
    mu sync.RWMutex

    // Metrics
    metrics *RemediationMetrics
}
```

### Remediation Flow

```
Problem Detected
      |
      v
[Is Remediation Enabled?] --No--> [Report Only]
      |
     Yes
      v
[Find Matching Remediator] --None--> [Skip]
      |
    Found
      v
[Check Circuit Breaker] --Open--> [Skip, Log]
      |
   Closed
      v
[Check Cooldown] --Active--> [Skip, Log]
      |
   Expired
      v
[Check Attempt Limit] --Exceeded--> [Give Up, Alert]
      |
  Within Limit
      v
[Check Rate Limit] --Exceeded--> [Defer]
      |
  Available
      v
[Execute Remediation]
      |
      v
[Update Tracking]
      |
      +---> [Success] ---> [Reset Attempts, Record Time]
      |
      +---> [Failure] ---> [Increment Attempts, Update Circuit Breaker]
```

## Safety Mechanisms

### 1. Cooldown Period

Prevents rapid re-remediation of the same problem.

**Purpose**: Allow time for remediation to take effect before trying again

**Configuration**:
```yaml
remediation:
  cooldownPeriod: 5m  # Global default

monitors:
  - name: kubelet-health
    remediation:
      cooldown: 5m  # Per-monitor override
```

**Implementation**:
```go
func (r *RemediatorRegistry) isInCooldown(problemKey string) bool {
    r.mu.RLock()
    defer r.mu.RUnlock()

    lastRemediationTime, exists := r.cooldownTracker[problemKey]
    if !exists {
        return false
    }

    cooldown := r.getCooldownForProblem(problemKey)
    return time.Since(lastRemediationTime) < cooldown
}
```

**Cooldown Values**:
- **Fast services** (< 10s startup): 3-5 minutes
- **Medium services** (10-30s startup): 5-10 minutes
- **Slow services** (> 30s startup): 10-15 minutes
- **Destructive actions**: 30+ minutes

---

### 2. Attempt Limiting

Limits the number of remediation attempts before giving up.

**Purpose**: Prevent infinite remediation loops for unfixable problems

**Configuration**:
```yaml
remediation:
  maxAttemptsGlobal: 3  # Global default

monitors:
  - name: kubelet-health
    remediation:
      maxAttempts: 3  # Per-monitor override
```

**Implementation**:
```go
func (r *RemediatorRegistry) canAttemptRemediation(problemKey string) bool {
    r.mu.RLock()
    defer r.mu.RUnlock()

    attempts := r.attemptTracker[problemKey]
    maxAttempts := r.getMaxAttemptsForProblem(problemKey)

    return attempts < maxAttempts
}

func (r *RemediatorRegistry) incrementAttempts(problemKey string) {
    r.mu.Lock()
    defer r.mu.Unlock()

    r.attemptTracker[problemKey]++
}

func (r *RemediatorRegistry) resetAttempts(problemKey string) {
    r.mu.Lock()
    defer r.mu.Unlock()

    delete(r.attemptTracker, problemKey)
}
```

**Reset Conditions**:
- Successful remediation
- Problem cleared naturally (without remediation)
- Manual reset via API
- After extended problem-free period (e.g., 24 hours)

---

### 3. Circuit Breaker

Stops remediation attempts after repeated failures.

**Purpose**: Protect against persistent failures that remediation cannot fix

**States**:
- **Closed**: Normal operation, remediations allowed
- **Open**: Too many failures, block all remediations
- **Half-Open**: Testing if problem is resolved

**Configuration**:
```yaml
remediation:
  circuitBreakerThreshold: 5   # Failures before opening
  circuitBreakerTimeout: 30m   # Time before trying half-open
  circuitBreakerSuccessThreshold: 2  # Successes to close
```

**State Machine**:
```
     [Closed]
        |
        | Failure count >= threshold
        v
     [Open] ----> Block all remediations
        |
        | After timeout
        v
   [Half-Open]
        |
        +---> Success count >= threshold --> [Closed]
        |
        +---> Any failure --> [Open]
```

**Implementation**:
```go
type CircuitBreaker struct {
    state              CircuitState
    failureCount       int
    successCount       int
    lastFailureTime    time.Time
    threshold          int
    timeout            time.Duration
    successThreshold   int
    mu                 sync.RWMutex
}

func (cb *CircuitBreaker) AllowRemediation() bool {
    cb.mu.RLock()
    defer cb.mu.RUnlock()

    switch cb.state {
    case Closed:
        return true
    case Open:
        // Check if we should transition to half-open
        if time.Since(cb.lastFailureTime) > cb.timeout {
            cb.transitionToHalfOpen()
            return true
        }
        return false
    case HalfOpen:
        return true
    }
}

func (cb *CircuitBreaker) RecordSuccess() {
    cb.mu.Lock()
    defer cb.mu.Unlock()

    cb.failureCount = 0

    if cb.state == HalfOpen {
        cb.successCount++
        if cb.successCount >= cb.successThreshold {
            cb.transitionToClosed()
        }
    }
}

func (cb *CircuitBreaker) RecordFailure() {
    cb.mu.Lock()
    defer cb.mu.Unlock()

    cb.failureCount++
    cb.lastFailureTime = time.Now()

    if cb.state == HalfOpen {
        cb.transitionToOpen()
    } else if cb.failureCount >= cb.threshold {
        cb.transitionToOpen()
    }
}
```

---

### 4. Rate Limiting

Limits total remediations across all problems within a time window.

**Purpose**: Prevent remediation storms that could destabilize the node

**Configuration**:
```yaml
remediation:
  maxRemediationsPerHour: 10
  maxRemediationsPerMinute: 2
```

**Implementation**:
```go
type RateLimiter struct {
    hourlyLimit   int
    minuteLimit   int
    hourlyBucket  []time.Time
    minuteBucket  []time.Time
    mu            sync.Mutex
}

func (rl *RateLimiter) Allow() bool {
    rl.mu.Lock()
    defer rl.mu.Unlock()

    now := time.Now()

    // Clean old entries
    rl.clean(now)

    // Check limits
    if len(rl.hourlyBucket) >= rl.hourlyLimit {
        return false
    }
    if len(rl.minuteBucket) >= rl.minuteLimit {
        return false
    }

    // Record this remediation
    rl.hourlyBucket = append(rl.hourlyBucket, now)
    rl.minuteBucket = append(rl.minuteBucket, now)

    return true
}

func (rl *RateLimiter) clean(now time.Time) {
    // Remove entries older than 1 hour
    cutoffHour := now.Add(-1 * time.Hour)
    for i, t := range rl.hourlyBucket {
        if t.After(cutoffHour) {
            rl.hourlyBucket = rl.hourlyBucket[i:]
            break
        }
    }

    // Remove entries older than 1 minute
    cutoffMinute := now.Add(-1 * time.Minute)
    for i, t := range rl.minuteBucket {
        if t.After(cutoffMinute) {
            rl.minuteBucket = rl.minuteBucket[i:]
            break
        }
    }
}
```

---

### 5. Dry-Run Mode

Tests remediation logic without actually executing changes.

**Purpose**: Validate remediation strategies before enabling them in production

**Configuration**:
```yaml
remediation:
  dryRun: true  # Global dry-run mode

monitors:
  - name: kubelet-health
    remediation:
      dryRun: false  # Can override per-monitor
```

**Implementation**:
```go
func (r *RemediatorRegistry) Remediate(ctx context.Context, problem Problem) error {
    remediator := r.findRemediator(problem)
    if remediator == nil {
        return fmt.Errorf("no remediator found for problem: %s", problem.Reason)
    }

    if r.dryRun {
        log.Infof("[DRY-RUN] Would remediate %s using %s",
            problem.Reason, remediator.GetName())

        // Create dry-run event
        r.eventRecorder.Eventf(
            &v1.ObjectReference{Kind: "Node", Name: r.nodeName},
            v1.EventTypeNormal,
            "DryRunRemediation",
            "Would remediate %s using %s",
            problem.Reason,
            remediator.GetName(),
        )

        return nil
    }

    // Actually execute remediation
    return remediator.Remediate(ctx, problem)
}
```

---

## Remediator Implementations

### 1. Systemd Service Remediator

Restarts systemd services that are unhealthy.

**Targets**:
- kubelet
- docker
- containerd
- cri-o
- Custom services

**Strategy**:
```go
type SystemdRemediator struct {
    service      string
    cooldown     time.Duration
    maxAttempts  int
    gracefulStop bool
}

func (s *SystemdRemediator) Remediate(ctx context.Context, problem Problem) error {
    log.Infof("Restarting systemd service: %s", s.service)

    if s.gracefulStop {
        // Try graceful stop first
        if err := s.stopService(ctx); err != nil {
            log.Warnf("Graceful stop failed: %v, forcing restart", err)
        }
        time.Sleep(2 * time.Second)
    }

    // Restart service
    cmd := exec.CommandContext(ctx, "systemctl", "restart", s.service)
    output, err := cmd.CombinedOutput()

    if err != nil {
        return fmt.Errorf("failed to restart %s: %w, output: %s",
            s.service, err, string(output))
    }

    // Wait for service to start
    if err := s.waitForService(ctx); err != nil {
        return fmt.Errorf("service did not start successfully: %w", err)
    }

    log.Infof("Successfully restarted %s", s.service)
    return nil
}

func (s *SystemdRemediator) waitForService(ctx context.Context) error {
    ticker := time.NewTicker(1 * time.Second)
    defer ticker.Stop()

    timeout := time.After(30 * time.Second)

    for {
        select {
        case <-ticker.C:
            cmd := exec.CommandContext(ctx, "systemctl", "is-active", s.service)
            output, err := cmd.CombinedOutput()

            if err == nil && strings.TrimSpace(string(output)) == "active" {
                return nil
            }

        case <-timeout:
            return fmt.Errorf("timeout waiting for service to start")

        case <-ctx.Done():
            return ctx.Err()
        }
    }
}
```

**Configuration**:
```yaml
monitors:
  - name: kubelet-health
    remediation:
      enabled: true
      strategy: systemd-restart
      service: kubelet
      cooldown: 5m
      maxAttempts: 3
      gracefulStop: true
```

---

### 2. Network Remediator

Fixes network connectivity issues.

**Strategies**:
- Flush DNS cache
- Restart network interface
- Reset iptables rules (cautiously)
- Restart CNI plugins

**Implementation**:
```go
type NetworkRemediator struct {
    strategy     string
    interface    string
    cooldown     time.Duration
    maxAttempts  int
}

func (n *NetworkRemediator) Remediate(ctx context.Context, problem Problem) error {
    switch n.strategy {
    case "flush-dns":
        return n.flushDNSCache(ctx)
    case "restart-interface":
        return n.restartInterface(ctx)
    case "reset-cni":
        return n.resetCNI(ctx)
    default:
        return fmt.Errorf("unknown network remediation strategy: %s", n.strategy)
    }
}

func (n *NetworkRemediator) flushDNSCache(ctx context.Context) error {
    log.Info("Flushing DNS cache")

    // Flush systemd-resolved cache
    cmd := exec.CommandContext(ctx, "systemd-resolve", "--flush-caches")
    if err := cmd.Run(); err != nil {
        log.Warnf("Failed to flush systemd-resolved cache: %v", err)
    }

    // Restart nscd if running
    cmd = exec.CommandContext(ctx, "systemctl", "restart", "nscd")
    if err := cmd.Run(); err != nil {
        log.Debugf("nscd not running or restart failed: %v", err)
    }

    return nil
}

func (n *NetworkRemediator) restartInterface(ctx context.Context) error {
    log.Infof("Restarting network interface: %s", n.interface)

    // Bring down
    if err := netlink.LinkSetDown(n.interface); err != nil {
        return fmt.Errorf("failed to bring down interface: %w", err)
    }

    time.Sleep(1 * time.Second)

    // Bring up
    if err := netlink.LinkSetUp(n.interface); err != nil {
        return fmt.Errorf("failed to bring up interface: %w", err)
    }

    // Wait for interface to be ready
    return n.waitForInterface(ctx)
}
```

**Configuration**:
```yaml
monitors:
  - name: dns-health
    remediation:
      enabled: true
      strategy: network
      action: flush-dns
      cooldown: 10m
      maxAttempts: 2

  - name: gateway-health
    remediation:
      enabled: false  # Disabled by default, too risky
      strategy: network
      action: restart-interface
      interface: eth0
      cooldown: 30m
      maxAttempts: 1
```

---

### 3. Disk Cleanup Remediator

Frees up disk space when thresholds are exceeded.

**Strategies**:
- Clean Docker/containerd logs
- Remove unused Docker images
- Clean system logs
- Clean temp files
- Clean kubelet cache

**Implementation**:
```go
type DiskRemediator struct {
    strategy       string
    targetPath     string
    minSpaceMB     int64
    cooldown       time.Duration
    maxAttempts    int
}

func (d *DiskRemediator) Remediate(ctx context.Context, problem Problem) error {
    switch d.strategy {
    case "docker-logs":
        return d.cleanDockerLogs(ctx)
    case "docker-images":
        return d.cleanDockerImages(ctx)
    case "system-logs":
        return d.cleanSystemLogs(ctx)
    case "temp-files":
        return d.cleanTempFiles(ctx)
    default:
        return fmt.Errorf("unknown disk remediation strategy: %s", d.strategy)
    }
}

func (d *DiskRemediator) cleanDockerLogs(ctx context.Context) error {
    log.Info("Cleaning Docker container logs")

    // Find all container log files
    logDir := "/var/lib/docker/containers"
    var cleaned int64

    err := filepath.Walk(logDir, func(path string, info os.FileInfo, err error) error {
        if err != nil {
            return nil // Skip errors
        }

        if !info.IsDir() && strings.HasSuffix(path, "-json.log") {
            // Truncate log file instead of deleting
            if info.Size() > 100*1024*1024 { // > 100MB
                if err := os.Truncate(path, 0); err != nil {
                    log.Warnf("Failed to truncate %s: %v", path, err)
                } else {
                    cleaned += info.Size()
                    log.Debugf("Truncated %s (%d bytes)", path, info.Size())
                }
            }
        }

        return nil
    })

    if err != nil {
        return fmt.Errorf("failed to walk docker logs: %w", err)
    }

    log.Infof("Cleaned %d MB of Docker logs", cleaned/1024/1024)
    return nil
}

func (d *DiskRemediator) cleanDockerImages(ctx context.Context) error {
    log.Info("Cleaning unused Docker images")

    cmd := exec.CommandContext(ctx, "docker", "image", "prune", "-a", "-f")
    output, err := cmd.CombinedOutput()

    if err != nil {
        return fmt.Errorf("docker image prune failed: %w, output: %s", err, output)
    }

    log.Infof("Docker image cleanup output: %s", output)
    return nil
}

func (d *DiskRemediator) cleanSystemLogs(ctx context.Context) error {
    log.Info("Cleaning system logs")

    // Clean journald logs
    cmd := exec.CommandContext(ctx, "journalctl", "--vacuum-size=500M")
    if err := cmd.Run(); err != nil {
        log.Warnf("Failed to vacuum journald: %v", err)
    }

    // Clean old logs in /var/log
    cmd = exec.CommandContext(ctx, "find", "/var/log",
        "-type", "f", "-name", "*.log.*", "-mtime", "+7", "-delete")
    if err := cmd.Run(); err != nil {
        log.Warnf("Failed to clean /var/log: %v", err)
    }

    return nil
}
```

**Configuration**:
```yaml
monitors:
  - name: disk-health
    config:
      paths:
        - path: "/var/lib/docker"
          criticalThreshold: 90
    remediation:
      enabled: true
      strategies:
        - strategy: disk
          action: docker-logs
          cooldown: 1h
          maxAttempts: 1
        - strategy: disk
          action: docker-images
          cooldown: 6h
          maxAttempts: 1
        - strategy: disk
          action: system-logs
          cooldown: 12h
          maxAttempts: 1
```

---

### 4. Container Runtime Remediator

Fixes container runtime (Docker/containerd/CRI-O) issues.

**Strategies**:
- Restart runtime service
- Clean up dead containers
- Restart specific containers
- Reset runtime state

**Implementation**:
```go
type RuntimeRemediator struct {
    runtimeType string  // docker, containerd, cri-o
    strategy    string
    cooldown    time.Duration
    maxAttempts int
}

func (r *RuntimeRemediator) Remediate(ctx context.Context, problem Problem) error {
    switch r.strategy {
    case "restart-service":
        return r.restartRuntimeService(ctx)
    case "cleanup-containers":
        return r.cleanupDeadContainers(ctx)
    default:
        return fmt.Errorf("unknown runtime remediation strategy: %s", r.strategy)
    }
}

func (r *RuntimeRemediator) restartRuntimeService(ctx context.Context) error {
    var service string

    switch r.runtimeType {
    case "docker":
        service = "docker"
    case "containerd":
        service = "containerd"
    case "cri-o":
        service = "crio"
    default:
        return fmt.Errorf("unknown runtime type: %s", r.runtimeType)
    }

    log.Infof("Restarting container runtime service: %s", service)

    // Use systemd remediator
    systemdRem := &SystemdRemediator{
        service:      service,
        gracefulStop: true,
    }

    return systemdRem.Remediate(ctx, problem)
}

func (r *RuntimeRemediator) cleanupDeadContainers(ctx context.Context) error {
    log.Info("Cleaning up dead containers")

    switch r.runtimeType {
    case "docker":
        cmd := exec.CommandContext(ctx, "docker", "container", "prune", "-f")
        return cmd.Run()

    case "containerd", "cri-o":
        // Use crictl
        cmd := exec.CommandContext(ctx, "crictl", "rmp", "-a")
        return cmd.Run()

    default:
        return fmt.Errorf("unknown runtime type: %s", r.runtimeType)
    }
}
```

**Configuration**:
```yaml
monitors:
  - name: runtime-health
    remediation:
      enabled: true
      strategy: runtime
      action: restart-service
      runtimeType: auto  # auto-detect
      cooldown: 15m
      maxAttempts: 2
```

---

### 5. Custom Script Remediator

Executes custom remediation scripts.

**Purpose**: Allow users to define custom remediation logic for specific problems

**Implementation**:
```go
type CustomScriptRemediator struct {
    scriptPath  string
    args        []string
    env         map[string]string
    workingDir  string
    timeout     time.Duration
    cooldown    time.Duration
    maxAttempts int
}

func (c *CustomScriptRemediator) Remediate(ctx context.Context, problem Problem) error {
    log.Infof("Executing custom remediation script: %s", c.scriptPath)

    // Create context with timeout
    ctx, cancel := context.WithTimeout(ctx, c.timeout)
    defer cancel()

    // Prepare command
    cmd := exec.CommandContext(ctx, c.scriptPath, c.args...)

    // Set environment
    cmd.Env = os.Environ()
    for k, v := range c.env {
        cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
    }

    // Set working directory
    if c.workingDir != "" {
        cmd.Dir = c.workingDir
    }

    // Execute
    output, err := cmd.CombinedOutput()

    if err != nil {
        return fmt.Errorf("script execution failed: %w, output: %s", err, output)
    }

    log.Infof("Script output: %s", output)

    // Check exit code
    if cmd.ProcessState.ExitCode() != 0 {
        return fmt.Errorf("script exited with code %d", cmd.ProcessState.ExitCode())
    }

    return nil
}
```

**Configuration**:
```yaml
monitors:
  - name: custom-app-health
    type: custom-plugin-check
    config:
      path: /opt/myapp/health_check.sh
    remediation:
      enabled: true
      strategy: custom-script
      scriptPath: /opt/myapp/remediate.sh
      args: ["--fix", "all"]
      env:
        APP_ENV: production
      workingDir: /opt/myapp
      timeout: 5m
      cooldown: 10m
      maxAttempts: 2
```

---

## Remediation Tracking and Observability

### Metrics

Prometheus metrics for remediation:

```go
var (
    remediationAttempts = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Namespace: "node_doctor",
            Subsystem: "remediation",
            Name:      "attempts_total",
            Help:      "Total number of remediation attempts",
        },
        []string{"problem", "remediator", "result"},
    )

    remediationDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Namespace: "node_doctor",
            Subsystem: "remediation",
            Name:      "duration_seconds",
            Help:      "Time taken to execute remediation",
            Buckets:   []float64{1, 5, 10, 30, 60, 120, 300},
        },
        []string{"problem", "remediator"},
    )

    circuitBreakerState = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Namespace: "node_doctor",
            Subsystem: "remediation",
            Name:      "circuit_breaker_state",
            Help:      "Circuit breaker state (0=closed, 1=half-open, 2=open)",
        },
        []string{"problem"},
    )

    cooldownActive = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Namespace: "node_doctor",
            Subsystem: "remediation",
            Name:      "cooldown_active",
            Help:      "Whether cooldown is active for a problem (1=active, 0=inactive)",
        },
        []string{"problem"},
    )
)
```

### Kubernetes Events

Create events for remediation activities:

```go
func (r *RemediatorRegistry) recordRemediationEvent(
    result string,
    problem Problem,
    remediator Remediator,
    err error,
) {
    eventType := v1.EventTypeNormal
    reason := "RemediationSucceeded"
    message := fmt.Sprintf("Successfully remediated %s using %s",
        problem.Reason, remediator.GetName())

    if err != nil {
        eventType = v1.EventTypeWarning
        reason = "RemediationFailed"
        message = fmt.Sprintf("Failed to remediate %s using %s: %v",
            problem.Reason, remediator.GetName(), err)
    }

    r.eventRecorder.Eventf(
        &v1.ObjectReference{
            Kind: "Node",
            Name: r.nodeName,
        },
        eventType,
        reason,
        message,
    )
}
```

### Remediation History

Track remediation history in memory and expose via HTTP:

```go
type RemediationRecord struct {
    Timestamp   time.Time
    Problem     string
    Remediator  string
    Result      string
    Error       string
    Duration    time.Duration
}

type RemediationHistory struct {
    records []RemediationRecord
    maxSize int
    mu      sync.RWMutex
}

func (h *RemediationHistory) Add(record RemediationRecord) {
    h.mu.Lock()
    defer h.mu.Unlock()

    h.records = append(h.records, record)

    // Keep only last N records
    if len(h.records) > h.maxSize {
        h.records = h.records[len(h.records)-h.maxSize:]
    }
}

func (h *RemediationHistory) GetRecent(limit int) []RemediationRecord {
    h.mu.RLock()
    defer h.mu.RUnlock()

    start := len(h.records) - limit
    if start < 0 {
        start = 0
    }

    return h.records[start:]
}
```

Expose via HTTP endpoint:

```
GET /remediation/history?limit=50

Response:
{
  "records": [
    {
      "timestamp": "2025-01-15T10:30:00Z",
      "problem": "KubeletUnhealthy",
      "remediator": "systemd-restart",
      "result": "success",
      "duration": "15.3s"
    },
    {
      "timestamp": "2025-01-15T09:45:00Z",
      "problem": "DiskPressure",
      "remediator": "disk-cleanup",
      "result": "success",
      "duration": "42.1s"
    }
  ]
}
```

---

## Testing Remediation

### Unit Tests

```go
func TestSystemdRemediator(t *testing.T) {
    remediator := &SystemdRemediator{
        service:     "test-service",
        cooldown:    5 * time.Minute,
        maxAttempts: 3,
    }

    problem := Problem{
        Reason: "ServiceUnhealthy",
    }

    ctx := context.Background()
    err := remediator.Remediate(ctx, problem)

    // Assertions...
}
```

### Integration Tests

Test with actual services in test environment:

```go
func TestKubeletRemediation_Integration(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration test")
    }

    // Stop kubelet
    exec.Command("systemctl", "stop", "kubelet").Run()

    // Wait for detection
    time.Sleep(5 * time.Second)

    // Trigger remediation
    registry := NewRemediatorRegistry(config)
    err := registry.Remediate(context.Background(), problem)
    require.NoError(t, err)

    // Verify kubelet is running
    output, err := exec.Command("systemctl", "is-active", "kubelet").Output()
    require.NoError(t, err)
    assert.Equal(t, "active", strings.TrimSpace(string(output)))
}
```

### Dry-Run Testing

```yaml
remediation:
  dryRun: true  # Enable dry-run mode

# Run Node Doctor and verify it logs remediation actions
# without actually executing them
```

---

## Best Practices

1. **Start Conservative**: Begin with dry-run mode and monitoring only
2. **Enable Gradually**: Enable remediation for one problem type at a time
3. **Set Appropriate Cooldowns**: Allow enough time for remediation to take effect
4. **Limit Attempts**: Prevent infinite loops with max attempts
5. **Monitor Remediation**: Track metrics and events closely
6. **Document Custom Scripts**: Clearly document what custom remediation scripts do
7. **Test Thoroughly**: Test remediation in non-production before enabling
8. **Have Rollback Plan**: Know how to quickly disable remediation if needed
9. **Audit Regularly**: Review remediation history and adjust strategies
10. **Coordinate with Operations**: Ensure ops team is aware of auto-remediation behavior

## Troubleshooting

### Remediation Not Triggering

1. Check if remediation is enabled globally and per-monitor
2. Verify problem is being detected
3. Check cooldown period hasn't been exceeded
4. Verify circuit breaker isn't open
5. Check rate limits aren't exceeded
6. Review logs for errors

### Remediation Failing Repeatedly

1. Check if problem is actually remediable
2. Verify remediation strategy is appropriate
3. Check for permission issues
4. Review remediation logs for specific errors
5. Consider disabling auto-remediation for this problem
6. Investigate root cause instead of relying on remediation

### Remediation Storms

1. Immediately disable remediation
2. Check for configuration errors
3. Review cooldown and rate limit settings
4. Investigate why problems are recurring
5. Fix underlying issues before re-enabling

---

## Future Enhancements

1. **Machine Learning**: Learn optimal remediation strategies over time
2. **Remediation Chaining**: Execute multiple remediation steps in sequence
3. **Conditional Remediation**: Only remediate under certain conditions
4. **Rollback Capability**: Automatically rollback failed remediations
5. **External Approvals**: Require approval via webhook for critical remediations
6. **Remediation Templates**: Pre-defined remediation workflows
7. **Impact Analysis**: Predict impact before executing remediation
8. **A/B Testing**: Test different remediation strategies automatically

---

## Summary

Node Doctor's remediation system provides safe, automated problem resolution with multiple layers of protection. By following the design principles and best practices, you can confidently deploy auto-remediation while maintaining system stability and security.
