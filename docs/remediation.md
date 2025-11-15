# Node Doctor Remediation System

## Overview

Node Doctor's remediation system provides automated problem resolution with multiple layers of safety mechanisms. The system is designed to safely fix detected problems without human intervention while preventing cascading failures and remediation storms.

**Source**: `pkg/remediators/` package

## Table of Contents

1. [Design Principles](#design-principles)
2. [Architecture](#architecture)
3. [Core Interfaces](#core-interfaces)
4. [Safety Mechanisms](#safety-mechanisms)
5. [Remediator Implementations](#remediator-implementations)
6. [Configuration](#configuration)
7. [Monitoring & Observability](#monitoring--observability)
8. [Dry-Run Mode](#dry-run-mode)
9. [Creating Custom Remediators](#creating-custom-remediators)
10. [Troubleshooting](#troubleshooting)
11. [Best Practices](#best-practices)

---

## Design Principles

The remediation system is built on these core principles (from `pkg/remediators/doc.go`):

1. **Safety First** - Multiple layers of protection prevent dangerous remediation loops
2. **Defense in Depth** - Global + per-remediator safety mechanisms work together
3. **Fail-Safe** - Errors during remediation should not make things worse
4. **Auditable** - All remediation actions are logged, tracked, and create Kubernetes events
5. **Testable** - Dry-run mode for validating strategies without actual execution
6. **Observable** - Comprehensive history tracking for analysis and debugging

---

## Architecture

### Component Hierarchy

```
RemediatorRegistry
├── Circuit Breaker (Global)
├── Rate Limiter (Global)
├── Remediation History
└── Remediators (Instances)
    └── BaseRemediator
        ├── Cooldown Tracking (Per-problem)
        ├── Attempt Counting (Per-problem)
        └── Panic Recovery
```

### Remediation Flow

```
Problem Detected
      |
      v
[RemediatorRegistry.Remediate]
      |
      v
[Phase 1: Circuit Breaker Check] --Open--> Skip (fail fast)
      |
   Allowed
      v
[Phase 2: Rate Limit Check] --Exceeded--> Skip (too many remediations)
      |
   Within Limit
      v
[Phase 3: Get/Create Remediator] --Not Found--> Error
      |
    Found
      v
[Phase 4: Remediator.CanRemediate]
      |                |
      v                v
  Cooldown      Max Attempts
   Active         Exceeded
      |                |
      +-----> Skip <---+
      |
   Allowed
      v
[Phase 5: Execute Remediation]
      |
      v
[Phase 6: Record in History]
      |
      v
[Phase 7: Update Circuit Breaker]
      |
      v
[Phase 8: Record Rate Limit Entry]
```

**Source**: `pkg/remediators/registry.go:433-521`

---

## Core Interfaces

### Remediator Interface

All remediators must implement this interface from `pkg/types/types.go:137-147`:

```go
type Remediator interface {
    // CanRemediate returns true if this remediator can handle the problem
    // Checks cooldown period and max attempt limits
    CanRemediate(problem Problem) bool

    // Remediate attempts to fix the problem
    // Returns an error if remediation fails or is not allowed
    Remediate(ctx context.Context, problem Problem) error

    // GetCooldown returns the minimum time between remediation attempts
    GetCooldown() time.Duration
}
```

### RemediateFunc Type

Function signature for actual remediation logic (`pkg/remediators/interface.go:11-14`):

```go
type RemediateFunc func(ctx context.Context, problem types.Problem) error
```

### Logger Interface

Optional logging interface for remediators (`pkg/remediators/interface.go:16-28`):

```go
type Logger interface {
    Infof(format string, args ...interface{})
    Warnf(format string, args ...interface{})
    Errorf(format string, args ...interface{})
}
```

---

## Safety Mechanisms

### 1. Cooldown Period (Per-Problem)

**Location**: `pkg/remediators/base.go:209-245`

**Purpose**: Prevents rapid re-remediation of the same problem. Allows time for remediation to take effect before trying again.

**Implementation**: Each unique problem (identified by `type:resource` key) has its own cooldown timer tracked in BaseRemediator.

**Cooldown Presets** (`pkg/remediators/interface.go:30-44`):
- `CooldownFast` = 3 minutes - For quick, low-risk remediations (e.g., DNS cache flush)
- `CooldownMedium` = 5 minutes - For standard service restarts (e.g., systemd services)
- `CooldownSlow` = 10 minutes - For slow-starting services (e.g., database restarts)
- `CooldownDestructive` = 30 minutes - For high-impact actions (e.g., node reboot)

**Minimum Cooldown** (`pkg/types/config.go:75-78`):
- `MinCooldownPeriod` = 10 seconds - Absolute minimum cooldown to prevent excessive remediation attempts
- All cooldown configurations are validated to be ≥ 10 seconds during configuration parsing
- This prevents accidental misconfiguration that could lead to remediation storms

**Problem Key Generation** (`pkg/remediators/interface.go:61-63`):
```go
func GenerateProblemKey(problem types.Problem) string {
    return fmt.Sprintf("%s:%s", problem.Type, problem.Resource)
}
// Examples: "kubelet-unhealthy:kubelet.service", "disk-pressure:/var/lib/docker"
```

**Cooldown Check** (`pkg/remediators/base.go:226-245`):
```go
func (b *BaseRemediator) GetCooldownRemaining(problem types.Problem) time.Duration {
    lastAttempt, exists := b.lastAttemptTime[problemKey]
    if !exists {
        return 0  // No cooldown if never attempted
    }

    elapsed := time.Since(lastAttempt)
    if elapsed >= b.cooldown {
        return 0  // Cooldown expired
    }

    return b.cooldown - elapsed
}
```

---

### 2. Attempt Limiting (Per-Problem)

**Location**: `pkg/remediators/base.go:129-153`

**Purpose**: Prevents infinite remediation loops for unfixable problems.

**Default**: `DefaultMaxAttempts = 3` (`pkg/remediators/interface.go:48-51`)

**Configuration**: Can be set per-remediator via `SetMaxAttempts()`

**Implementation**:
```go
func (b *BaseRemediator) CanRemediate(problem types.Problem) bool {
    // Check if we're in cooldown
    if b.IsInCooldown(problem) {
        return false
    }

    // Check if we've exceeded max attempts
    if b.GetAttemptCount(problem) >= b.maxAttempts {
        return false
    }

    return true
}
```

**Attempt Tracking** (`pkg/remediators/base.go:217-224`):
- Incremented before each remediation attempt
- Tracked per unique problem key
- Can be manually reset via `ResetAttempts()`

---

### 3. Circuit Breaker (Global)

**Location**: `pkg/remediators/registry.go:41-67`

**Purpose**: Protect against cascading failures by blocking all remediations after too many failures.

**States** (`pkg/remediators/registry.go:44-53`):
- **CircuitClosed** (0): Normal operation - remediations are allowed
- **CircuitOpen** (1): Too many failures - remediations are blocked
- **CircuitHalfOpen** (2): Testing recovery - limited remediations allowed

**Default Configuration** (`pkg/remediators/registry.go:188-193`):
```go
DefaultCircuitBreakerConfig = CircuitBreakerConfig{
    Threshold:        5,               // Open after 5 consecutive failures
    Timeout:          5 * time.Minute, // Stay open for 5 minutes
    SuccessThreshold: 2,               // Close after 2 consecutive successes
}
```

**State Transitions**:

```
Closed → [threshold failures] → Open
Open → [timeout elapsed] → Half-Open
Half-Open → [successThreshold successes] → Closed
Half-Open → [any failure] → Open
```

**Check Logic** (`pkg/remediators/registry.go:523-550`):
```go
func (r *RemediatorRegistry) checkCircuitBreaker() error {
    switch r.circuitState {
    case CircuitClosed:
        return nil  // Allow remediation

    case CircuitOpen:
        if time.Since(r.circuitOpenedAt) >= r.circuitConfig.Timeout {
            r.circuitState = CircuitHalfOpen  // Try half-open
            return nil
        }
        return fmt.Errorf("circuit breaker is Open")

    case CircuitHalfOpen:
        return nil  // Allow one test remediation
    }
}
```

**Failure Recording** (`pkg/remediators/registry.go:616-644`):
```go
func (r *RemediatorRegistry) recordCircuitBreakerFailure() {
    r.consecutiveSuccesses = 0
    r.consecutiveFailures++

    // If in HalfOpen and got failure, open the circuit again
    if r.circuitState == CircuitHalfOpen {
        r.circuitState = CircuitOpen
        r.circuitOpenedAt = time.Now()
        return
    }

    // If in Closed and hit threshold, open the circuit
    if r.circuitState == CircuitClosed {
        if r.consecutiveFailures >= r.circuitConfig.Threshold {
            r.circuitState = CircuitOpen
            r.circuitOpenedAt = time.Now()
        }
    }
}
```

---

### 4. Rate Limiting (Global)

**Location**: `pkg/remediators/registry.go:552-595`

**Purpose**: Prevent remediation storms that could destabilize the node by limiting total remediations across all problems.

**Configuration**:
- `maxPerHour` - Maximum remediations allowed per hour (0 = unlimited)
- Sliding window implementation removes old entries

**Implementation**:
```go
func (r *RemediatorRegistry) checkRateLimit() error {
    if r.maxPerHour == 0 {
        return nil  // Rate limiting disabled
    }

    // Clean up old entries (outside the window)
    now := time.Now()
    cutoff := now.Add(-r.rateLimitWindow)  // 1 hour window

    // Keep only entries within the window
    validEntries := 0
    for i := len(r.remediationTimes) - 1; i >= 0; i-- {
        if r.remediationTimes[i].After(cutoff) {
            validEntries++
        } else {
            break  // Entries are sorted
        }
    }

    // Check if we're at the limit
    if len(r.remediationTimes) >= r.maxPerHour {
        return fmt.Errorf("rate limit exceeded: %d remediations in the last hour",
            len(r.remediationTimes))
    }

    return nil
}
```

**Recording Entries** (`pkg/remediators/registry.go:585-595`):
```go
func (r *RemediatorRegistry) recordRateLimitEntry() {
    if r.maxPerHour == 0 {
        return  // Disabled
    }

    r.mu.Lock()
    defer r.mu.Unlock()

    r.remediationTimes = append(r.remediationTimes, time.Now())
}
```

---

### 5. Panic Recovery

**Location**: `pkg/remediators/base.go:179-198`

**Purpose**: Prevent panics in remediation logic from crashing Node Doctor.

**Implementation**:
```go
func (b *BaseRemediator) Remediate(ctx context.Context, problem types.Problem) error {
    // Execute remediation with panic recovery
    var err error
    func() {
        defer func() {
            if r := recover(); r != nil {
                err = fmt.Errorf("panic during remediation of %s: %v", problemKey, r)
                b.logErrorf("Panic recovered: %v", err)
            }
        }()

        // Check context cancellation before executing
        select {
        case <-ctx.Done():
            err = fmt.Errorf("context cancelled before remediation: %w", ctx.Err())
            return
        default:
        }

        // Execute the remediation
        err = b.remediateFunc(ctx, problem)
    }()

    return err
}
```

---

## Remediator Implementations

### 1. SystemdRemediator

**Source**: `pkg/remediators/systemd.go`

**Purpose**: Manages systemd services through restart, stop, start, and reload operations.

**Supported Operations** (`pkg/remediators/systemd.go:13-28`):
- `restart` - Restarts the service
- `stop` - Stops the service
- `start` - Starts the service
- `reload` - Reloads the service configuration

**Configuration**:
```go
type SystemdConfig struct {
    Operation      SystemdOperation  // restart, stop, start, reload
    ServiceName    string           // e.g., "kubelet", "docker", "containerd"
    VerifyStatus   bool             // Verify service is active after remediation
    VerifyTimeout  time.Duration    // Max time to wait for verification (default: 30s)
    DryRun         bool             // Only simulate, don't execute
}
```

**Example - Kubelet Restart**:
```go
config := SystemdConfig{
    Operation:     SystemdRestart,
    ServiceName:   "kubelet",
    VerifyStatus:  true,
    VerifyTimeout: 30 * time.Second,
}

remediator, err := NewSystemdRemediator(config)
// Cooldown: CooldownMedium (5 minutes)
```

**Verification** (`pkg/remediators/systemd.go:244-273`):
- Polls service status every 1 second
- Waits up to `VerifyTimeout` for service to become active
- Uses `systemctl is-active <service>` command

**Service Status States** (`pkg/remediators/systemd.go:79-100`):
- `active` - Service is running (success)
- `inactive` - Service is stopped
- `failed` - Service has failed
- `activating`, `deactivating`, `reloading` - Transitional states (error)

---

### 2. CustomRemediator

**Source**: `pkg/remediators/custom.go`

**Purpose**: Executes custom user-defined remediation scripts with full safety checks and environment variable injection.

**Configuration**:
```go
type CustomConfig struct {
    ScriptPath       string            // Absolute path to remediation script
    ScriptArgs       []string          // Optional arguments
    Timeout          time.Duration     // Max execution time (default: 5m, max: 30m)
    Environment      map[string]string // Additional environment variables
    CaptureOutput    bool              // Capture and log stdout/stderr
    AllowNonZeroExit bool              // Don't treat non-zero exit as failure
    WorkingDir       string            // Working directory (default: script's dir)
    DryRun           bool              // Only simulate
}
```

**Problem Metadata Injection** (`pkg/remediators/custom.go:271-291`):

The script receives problem details as environment variables:

```bash
PROBLEM_TYPE=kubelet-unhealthy
PROBLEM_RESOURCE=kubelet.service
PROBLEM_MESSAGE=Kubelet is not responding
PROBLEM_SEVERITY=Critical
PROBLEM_DETECTED_AT=2025-01-15T10:30:00Z
PROBLEM_META_<KEY>=<VALUE>  # Problem metadata converted to UPPERCASE_UNDERSCORE
```

**Safety Checks** (`pkg/remediators/custom.go:109-131`):
1. Script must exist
2. Script must be a regular file
3. Script must be executable (mode & 0111)
4. Script path cannot contain `..` (path traversal prevention)
5. Execution timeout enforced

**Example Custom Remediation Script**:
```bash
#!/bin/bash
# /opt/node-doctor/scripts/fix-kubelet.sh

set -e

echo "Problem: $PROBLEM_TYPE on $PROBLEM_RESOURCE"
echo "Detected at: $PROBLEM_DETECTED_AT"

# Perform remediation
if systemctl is-active --quiet kubelet; then
    echo "Kubelet is running, restarting..."
    systemctl restart kubelet
else
    echo "Kubelet is not running, starting..."
    systemctl start kubelet
fi

# Wait for kubelet to be ready
timeout 30 bash -c 'until systemctl is-active --quiet kubelet; do sleep 1; done'

echo "Remediation complete"
exit 0
```

**Configuration Example**:
```go
config := CustomConfig{
    ScriptPath:    "/opt/node-doctor/scripts/fix-kubelet.sh",
    ScriptArgs:    []string{"--verbose"},
    Timeout:       5 * time.Minute,
    CaptureOutput: true,
    Environment: map[string]string{
        "NODE_ENV": "production",
    },
}

remediator, err := NewCustomRemediator(config)
// Cooldown: CooldownMedium (5 minutes)
```

---

### 3. NetworkRemediator

**Source**: `pkg/remediators/network.go`

**Purpose**: Fixes network connectivity issues through DNS cache flushing, interface restarts, and routing table operations.

**Supported Operations** (`pkg/remediators/network.go:13-25`):
- `flush-dns` - Flushes DNS resolver cache
- `restart-interface` - Restarts network interface (down/up)
- `reset-routing` - Resets routing table to defaults

**Configuration**:
```go
type NetworkConfig struct {
    Operation       NetworkOperation  // flush-dns, restart-interface, reset-routing
    InterfaceName   string           // Required for restart-interface (e.g., "eth0")
    BackupRouting   bool             // Backup routing table before reset
    VerifyAfter     bool             // Verify operation succeeded
    VerifyTimeout   time.Duration    // Max time for verification (default: 10s)
    DryRun          bool             // Only simulate
}
```

**DNS Cache Flush** (`pkg/remediators/network.go:212-230`):

Tries multiple methods in order:
1. `resolvectl flush-caches` (modern systemd)
2. `systemd-resolve --flush-caches` (older systemd)

**Interface Restart** (`pkg/remediators/network.go:233-263`):
```go
// Safety: Verify interface exists first
// 1. Bring interface down: ip link set <iface> down
// 2. Wait 500ms
// 3. Bring interface up: ip link set <iface> up
// 4. Verify interface is UP (if VerifyAfter=true)
```

**Routing Reset** (`pkg/remediators/network.go:266-295`):
```go
// 1. Backup routing table: ip route show (if BackupRouting=true)
// 2. Flush routing cache: ip route flush cache
// 3. Verify routing table accessible (if VerifyAfter=true)
```

**Cooldown Values**:
- DNS flush: `CooldownFast` (3 minutes)
- Interface restart: `CooldownFast` (3 minutes)
- Routing reset: `CooldownMedium` (5 minutes) - More impactful

**Example - DNS Issues**:
```go
config := NetworkConfig{
    Operation:   NetworkFlushDNS,
    VerifyAfter: false,  // DNS flush is immediate
}

remediator, err := NewNetworkRemediator(config)
```

**Example - Interface Restart**:
```go
config := NetworkConfig{
    Operation:     NetworkRestartInterface,
    InterfaceName: "eth0",
    VerifyAfter:   true,
    VerifyTimeout: 10 * time.Second,
}

remediator, err := NewNetworkRemediator(config)
```

---

### 4. DiskRemediator

**Source**: `pkg/remediators/disk.go`

**Purpose**: Frees up disk space through various cleanup operations.

**Supported Operations** (`pkg/remediators/disk.go:13-28`):
- `clean-journal-logs` - Cleans old systemd journal logs
- `clean-docker-images` - Removes unused Docker images
- `clean-tmp` - Removes old files from /tmp
- `clean-container-layers` - Removes unused container layers (docker system prune)

**Configuration**:
```go
type DiskConfig struct {
    Operation        DiskOperation  // Cleanup operation to perform
    JournalVacuumSize string        // Target size for journal (e.g., "500M", "1G")
    TmpFileAge       int            // Age in days for /tmp cleanup (default: 7)
    MinFreeSpaceGB   float64        // Minimum free space before cleanup (0 = disabled)
    TargetPath       string         // Path to check for free space (default: "/")
    VerifyAfter      bool           // Verify disk space was reclaimed
    DryRun           bool           // Only simulate
}
```

**Journal Cleanup** (`pkg/remediators/disk.go:276-287`):
```bash
journalctl --vacuum-size=500M
```

**Docker Image Cleanup** (`pkg/remediators/disk.go:289-301`):
```bash
docker image prune -a -f
# -a: Remove all unused images, not just dangling
# -f: Force without confirmation
```

**Tmp Cleanup** (`pkg/remediators/disk.go:303-320`):
```bash
find /tmp -mindepth 1 -type f -mtime +7 -delete
# -mindepth 1: Don't delete /tmp itself
# -type f: Only files, not directories
# -mtime +7: Modified more than 7 days ago
```

**Container Layers Cleanup** (`pkg/remediators/disk.go:322-337`):
```bash
docker system prune -a -f --volumes
# -a: All unused images
# -f: Force
# --volumes: Also remove unused volumes
```

**Disk Usage Checking** (`pkg/remediators/disk.go:88-129`):
```go
// Uses df -BG to get sizes in gigabytes
// Returns: used, available, total (in GB)

func (e *defaultDiskExecutor) GetDiskUsage(ctx context.Context, path string)
    (used, available, total float64, err error)
```

**Verification** (`pkg/remediators/disk.go:244-258`):
- Compares disk usage before and after cleanup
- Logs amount of space reclaimed
- Warns if less than 10MB reclaimed (but doesn't fail)

**Example - Clean Journal Logs**:
```go
config := DiskConfig{
    Operation:         DiskCleanJournalLogs,
    JournalVacuumSize: "500M",
    TargetPath:        "/var",
    MinFreeSpaceGB:    5.0,  // Only cleanup if < 5GB free
    VerifyAfter:       true,
}

remediator, err := NewDiskRemediator(config)
// Cooldown: CooldownMedium (5 minutes)
```

---

### 5. RuntimeRemediator

**Source**: `pkg/remediators/runtime.go`

**Purpose**: Manages container runtimes (Docker, containerd, CRI-O) with auto-detection support.

**Supported Runtimes** (`pkg/remediators/runtime.go:12-27`):
- `docker` - Docker container runtime
- `containerd` - containerd container runtime
- `crio` - CRI-O container runtime
- `auto` - Automatically detect runtime

**Supported Operations** (`pkg/remediators/runtime.go:29-41`):
- `restart-daemon` - Restarts the runtime daemon via systemd
- `clean-containers` - Cleans up stopped containers
- `prune-volumes` - Prunes dangling volumes (Docker only)

**Configuration**:
```go
type RuntimeConfig struct {
    Operation    RuntimeOperation  // restart-daemon, clean-containers, prune-volumes
    RuntimeType  RuntimeType       // docker, containerd, crio, auto
    VerifyAfter  bool              // Verify operation succeeded
    DryRun       bool              // Only simulate
}
```

**Runtime Auto-Detection** (`pkg/remediators/runtime.go:236-253`):
```go
// Tries in order: Docker, containerd, CRI-O
// Tests each with version command:
//   - docker version
//   - ctr version
//   - crictl version
```

**Systemd Service Mapping** (`pkg/remediators/runtime.go:123-135`):
```go
Docker      → "docker"
Containerd  → "containerd"
CRI-O       → "crio"
```

**Clean Containers** (`pkg/remediators/runtime.go:288-320`):
```bash
# Docker
docker container prune -f

# Containerd
ctr containers rm $(ctr containers list -q)

# CRI-O
crictl rmp -a  # Remove all stopped containers
```

**Prune Volumes** (`pkg/remediators/runtime.go:322-340`):
```bash
# Docker only (containerd/CRI-O handle volumes differently)
docker volume prune -f
```

**Verification** (`pkg/remediators/runtime.go:360-380`):
- For daemon restart: Checks `systemctl is-active <service>`
- For cleanup operations: Verification skipped (not critical)

**Example - Docker Daemon Restart**:
```go
config := RuntimeConfig{
    Operation:    RuntimeRestartDaemon,
    RuntimeType:  RuntimeDocker,
    VerifyAfter:  true,
}

remediator, err := NewRuntimeRemediator(config)
// Cooldown: CooldownMedium (5 minutes)
```

**Example - Auto-Detect Runtime**:
```go
config := RuntimeConfig{
    Operation:    RuntimeCleanContainers,
    RuntimeType:  RuntimeAuto,  // Automatically detect
    VerifyAfter:  false,
}

remediator, err := NewRuntimeRemediator(config)
```

---

## Configuration

### Global Remediation Settings

```yaml
remediation:
  enabled: true                      # Enable/disable remediation globally
  maxRemediationsPerHour: 10         # Rate limit (0 = unlimited)
  cooldownPeriod: 5m                 # Default cooldown (minimum: 10s)
  maxAttemptsGlobal: 3               # Default max attempts per problem
  circuitBreaker:
    enabled: true
    threshold: 5                     # Failures before opening circuit
    timeout: 30m                     # Time before trying half-open
    successThreshold: 2              # Successes to close circuit
  features:
    dryRun: false                    # Global dry-run mode
    historySize: 100                 # Max remediation records to keep (0-10000)
```

**Cooldown Period Validation:**
- All cooldown periods must be ≥ 10 seconds (MinCooldownPeriod)
- This applies to both global `cooldownPeriod` and per-monitor `cooldown` settings
- Values below 10 seconds will fail configuration validation
- See [Configuration Guide](configuration.md#monitor-interval-minimums) for details

### Per-Monitor Remediation

```yaml
monitors:
  - name: kubelet-health
    type: kubernetes-kubelet-check
    interval: 30s
    remediation:
      enabled: true
      strategy: systemd-restart
      service: kubelet
      cooldown: 5m                   # Override per-monitor (minimum: 10s)
      maxAttempts: 3                 # Override per-monitor
      verifyStatus: true
      verifyTimeout: 30s
```

**Validation Notes:**
- `cooldown` must be ≥ 10 seconds (MinCooldownPeriod)
- `maxAttempts` must be positive
- `verifyTimeout` must be positive if specified

### Configuration Mapping to Implementation

**From `pkg/types/config.go`:**

```yaml
remediation:
  enabled: true                           # RemediationConfig.Enabled
  maxRemediationsPerHour: 10              # RemediationConfig.MaxRemediationsPerHour
  maxRemediationsPerMinute: 2             # RemediationConfig.MaxRemediationsPerMinute (optional)
  cooldownPeriod: 5m                      # RemediationConfig.CooldownPeriod
  maxAttemptsGlobal: 3                    # RemediationConfig.MaxAttemptsGlobal
  historySize: 100                        # RemediationConfig.HistorySize
  circuitBreaker:
    enabled: true                         # CircuitBreakerConfig.Enabled
    threshold: 5                          # CircuitBreakerConfig.Threshold
    timeout: 30m                          # CircuitBreakerConfig.Timeout
    successThreshold: 2                   # CircuitBreakerConfig.SuccessThreshold
  strategies:                             # RemediationConfig.Strategies (nested)
    kubelet-unhealthy:                    # Problem-specific override
      enabled: true
      strategy: systemd-restart
      service: kubelet
      cooldown: 5m
      maxAttempts: 3
```

**See**: `docs/configuration.md` for complete remediation configuration reference.

---

## Monitoring & Observability

### Remediation History

**Location**: `pkg/remediators/registry.go:646-701`

All remediation attempts are recorded in history:

```go
type RemediationRecord struct {
    RemediatorType string         // Type of remediator used
    Problem        types.Problem  // Problem that was remediated
    StartTime      time.Time      // When remediation started
    EndTime        time.Time      // When remediation completed
    Duration       time.Duration  // How long it took
    Success        bool           // Whether it succeeded
    Error          string         // Error message if failed
}
```

**Retrieving History**:
```go
// Get last 50 remediation records
history := registry.GetHistory(50)

for _, record := range history {
    fmt.Printf("[%s] %s on %s: %s (duration: %v)\n",
        record.StartTime.Format(time.RFC3339),
        record.RemediatorType,
        record.Problem.Resource,
        record.Result,
        record.Duration)
}
```

**History Size Management** (`pkg/remediators/registry.go:195-225`):
- Configurable max size (default: 100 records)
- Automatically trims to keep only most recent records
- Upper limit: 10,000 records to prevent memory issues

---

### Kubernetes Events

**Location**: `pkg/remediators/registry.go:776-817`

Remediation attempts create Kubernetes events:

```go
// Success Event
Type:     Normal
Reason:   RemediationSuccess
Message:  Successfully remediated kubelet-unhealthy on kubelet.service
          using systemd-restart-kubelet (duration: 15.3s)

// Failure Event
Type:     Warning
Reason:   RemediationFailure
Message:  Failed to remediate disk-pressure on /var/lib/docker
          using disk-clean-journal-logs: journalctl command failed (duration: 2.1s)
```

**Event Naming** (`pkg/remediators/registry.go:792-794`):
```
node-doctor-<nodename>-remediation-<remediator-type>-<timestamp>
```

**Event Creation** (`pkg/remediators/registry.go:666-680`):
- Created asynchronously in background goroutine
- 5-second timeout for event creation
- Errors logged but don't fail remediation

---

### Registry Statistics

**Location**: `pkg/remediators/registry.go:710-760`

Query registry state:

```go
type RegistryStats struct {
    RegisteredTypes      int                  // Number of registered remediator types
    CircuitState         CircuitBreakerState  // Current circuit breaker state
    ConsecutiveFailures  int                  // Current failure count
    ConsecutiveSuccesses int                  // Current success count (half-open)
    CircuitOpenedAt      time.Time            // When circuit was last opened
    RecentRemediations   int                  // Count in rate limit window
    MaxPerHour           int                  // Rate limit
    HistorySize          int                  // Current history records
    MaxHistory           int                  // Max history size
    DryRun               bool                 // Dry-run mode status
}

stats := registry.GetStats()
fmt.Printf("Circuit State: %s\n", stats.CircuitState)
fmt.Printf("Recent Remediations: %d/%d\n", stats.RecentRemediations, stats.MaxPerHour)
```

**Circuit Breaker State String** (`pkg/remediators/registry.go:56-67`):
- `Closed` - Normal operation
- `Open` - Blocking remediations
- `HalfOpen` - Testing recovery

---

## Dry-Run Mode

**Purpose**: Test remediation logic without actually executing changes. All safety mechanisms (circuit breaker, rate limiting, cooldown, attempt counting) still function normally.

### Global Dry-Run

**Location**: `pkg/remediators/registry.go:278-293`

```go
registry.SetDryRun(true)

// All remediations will be simulated
err := registry.Remediate(ctx, "systemd-restart", problem)
// Logs: [DRY-RUN] Would execute remediation for kubelet-unhealthy:kubelet.service
```

### Per-Remediator Dry-Run

Each remediator supports `DryRun` in its configuration:

```go
// SystemdRemediator dry-run
config := SystemdConfig{
    Operation:   SystemdRestart,
    ServiceName: "kubelet",
    DryRun:      true,  // Only this remediator in dry-run
}

// CustomRemediator dry-run
config := CustomConfig{
    ScriptPath: "/path/to/script.sh",
    DryRun:     true,
}
```

**Implementation** (`pkg/remediators/systemd.go:156-160`, etc.):
```go
if r.config.DryRun {
    r.logInfof("DRY-RUN: Would execute systemctl %s %s", r.config.Operation, r.config.ServiceName)
    return nil
}
```

### Testing Workflow

1. **Enable Dry-Run**:
   ```yaml
   remediation:
     dryRun: true
   ```

2. **Trigger Problems**: Manually trigger or wait for problems to be detected

3. **Review Logs**: Check logs for `[DRY-RUN]` messages

4. **Verify Safety**: Confirm circuit breaker, rate limiting, cooldown all function

5. **Disable Dry-Run**: Once confident, set `dryRun: false`

---

## Creating Custom Remediators

### Using BaseRemediator

**Example**: Creating a remediator for PostgreSQL restarts

```go
package remediators

import (
    "context"
    "fmt"
    "os/exec"
    "time"

    "github.com/supporttools/node-doctor/pkg/types"
)

// PostgresRemediator restarts PostgreSQL service
type PostgresRemediator struct {
    *BaseRemediator
    serviceName string
}

// NewPostgresRemediator creates a new PostgreSQL remediator
func NewPostgresRemediator(serviceName string) (*PostgresRemediator, error) {
    // Create base with slow cooldown (databases take time to start)
    base, err := NewBaseRemediator(
        "postgres-restart",
        CooldownSlow,  // 10 minutes
    )
    if err != nil {
        return nil, err
    }

    remediator := &PostgresRemediator{
        BaseRemediator: base,
        serviceName:    serviceName,
    }

    // Set the remediation function
    if err := base.SetRemediateFunc(remediator.restartPostgres); err != nil {
        return nil, err
    }

    return remediator, nil
}

// restartPostgres performs the actual remediation
func (r *PostgresRemediator) restartPostgres(ctx context.Context, problem types.Problem) error {
    // 1. Stop PostgreSQL gracefully
    cmd := exec.CommandContext(ctx, "systemctl", "stop", r.serviceName)
    if err := cmd.Run(); err != nil {
        return fmt.Errorf("failed to stop postgres: %w", err)
    }

    // 2. Wait for proper shutdown
    time.Sleep(5 * time.Second)

    // 3. Start PostgreSQL
    cmd = exec.CommandContext(ctx, "systemctl", "start", r.serviceName)
    if err := cmd.Run(); err != nil {
        return fmt.Errorf("failed to start postgres: %w", err)
    }

    // 4. Wait for PostgreSQL to be ready
    for i := 0; i < 30; i++ {
        cmd = exec.CommandContext(ctx, "pg_isready")
        if err := cmd.Run(); err == nil {
            return nil  // Success!
        }
        time.Sleep(1 * time.Second)
    }

    return fmt.Errorf("postgres did not become ready after 30 seconds")
}
```

### Registering with Registry

```go
func init() {
    // Register factory function
    remediators.Register(remediators.RemediatorInfo{
        Type:        "postgres-restart",
        Factory:     NewPostgresRemediatorFactory,
        Description: "Restarts PostgreSQL database service",
    })
}

func NewPostgresRemediatorFactory() (types.Remediator, error) {
    return NewPostgresRemediator("postgresql")
}
```

### Testing Custom Remediators

```go
func TestPostgresRemediator(t *testing.T) {
    remediator, err := NewPostgresRemediator("postgresql")
    require.NoError(t, err)

    problem := types.Problem{
        Type:     "postgres-unhealthy",
        Resource: "postgresql.service",
        Severity: types.ProblemCritical,
        Message:  "PostgreSQL is not accepting connections",
    }

    // Test CanRemediate
    assert.True(t, remediator.CanRemediate(problem))

    // Test actual remediation (requires postgres installed)
    ctx := context.Background()
    err = remediator.Remediate(ctx, problem)
    assert.NoError(t, err)

    // Verify cooldown is active
    assert.False(t, remediator.CanRemediate(problem))
    assert.Greater(t, remediator.GetCooldownRemaining(problem), time.Duration(0))
}
```

---

## Troubleshooting

### Remediation Not Triggering

**Symptoms**: Problems detected but no remediation attempts

**Checklist**:
1. ✓ Is remediation enabled globally? (`remediation.enabled: true`)
2. ✓ Is remediation enabled for this monitor? (monitor config)
3. ✓ Is cooldown period active? (check logs for cooldown messages)
4. ✓ Are max attempts exceeded? (check attempt count in logs)
5. ✓ Is circuit breaker open? (check circuit state: `registry.GetCircuitState()`)
6. ✓ Is rate limit exceeded? (check recent remediation count)
7. ✓ Is dry-run mode enabled? (check for `[DRY-RUN]` logs)

**Diagnostic Commands**:
```go
// Check circuit breaker state
stats := registry.GetStats()
fmt.Printf("Circuit: %s\n", stats.CircuitState)

// Check cooldown for specific problem
remaining := remediator.GetCooldownRemaining(problem)
fmt.Printf("Cooldown remaining: %v\n", remaining)

// Check attempt count
attempts := remediator.GetAttemptCount(problem)
fmt.Printf("Attempts: %d/%d\n", attempts, remediator.GetMaxAttempts())
```

---

### Remediation Failing Repeatedly

**Symptoms**: Multiple remediation attempts all failing

**Investigation Steps**:

1. **Check Error Messages**:
   ```bash
   # Review logs for specific error
   grep "Remediation failed" /var/log/node-doctor.log
   ```

2. **Verify Permissions**:
   ```bash
   # Check if Node Doctor has systemd permissions
   systemctl --user show-environment

   # Check script permissions for custom remediators
   ls -la /path/to/remediation/script.sh
   ```

3. **Test Manually**:
   ```bash
   # Try the remediation command manually
   systemctl restart kubelet

   # Or run custom script
   /path/to/script.sh
   ```

4. **Check Resource Availability**:
   ```bash
   # Verify service exists
   systemctl cat kubelet

   # Check disk space for disk remediators
   df -h
   ```

5. **Review Circuit Breaker**:
   - After threshold failures, circuit opens
   - No remediations attempted until timeout expires
   - Check `CircuitState` in stats

**Common Issues**:

| Error | Cause | Solution |
|-------|-------|----------|
| `Permission denied` | Node Doctor running without systemd access | Run as root or configure sudo |
| `Script not executable` | Custom script missing execute permission | `chmod +x script.sh` |
| `Timeout waiting for service` | Service taking too long to start | Increase `VerifyTimeout` |
| `Circuit breaker is Open` | Too many consecutive failures | Fix underlying issue, then reset circuit |

---

### Remediation Storms

**Symptoms**: Excessive remediation attempts causing instability

**Immediate Actions**:
1. **Disable Remediation**:
   ```yaml
   remediation:
     enabled: false  # Immediately stops all remediations
   ```

2. **Check Rate Limits**:
   ```go
   stats := registry.GetStats()
   fmt.Printf("Recent remediations: %d/%d\n",
       stats.RecentRemediations, stats.MaxPerHour)
   ```

3. **Review History**:
   ```go
   history := registry.GetHistory(100)
   // Look for patterns - same problem repeatedly, rapid attempts
   ```

**Root Cause Analysis**:

1. **Configuration Errors**:
   - Cooldown too short (< 1 minute)
   - Max attempts too high (> 5)
   - Rate limit too high or disabled

2. **Problem Not Actually Remediated**:
   - Remediation succeeds but problem persists
   - Check if remediation is appropriate for problem type

3. **Detection Too Sensitive**:
   - Problem detected immediately after remediation
   - Increase monitor interval or adjust thresholds

**Prevention**:
```yaml
remediation:
  maxRemediationsPerHour: 10      # Reasonable limit
  maxRemediationsPerMinute: 2     # Prevent bursts
  cooldownPeriod: 5m              # Adequate cooldown
  maxAttemptsGlobal: 3            # Reasonable attempts
  circuitBreaker:
    enabled: true                 # Enable circuit breaker
    threshold: 5                  # Open after 5 failures
```

---

### Circuit Breaker Stuck Open

**Symptoms**: No remediations attempted, circuit breaker in Open state

**Diagnosis**:
```go
stats := registry.GetStats()
fmt.Printf("Circuit State: %s\n", stats.CircuitState)
fmt.Printf("Opened At: %s\n", stats.CircuitOpenedAt)
fmt.Printf("Consecutive Failures: %d\n", stats.ConsecutiveFailures)

// Calculate when circuit will try half-open
timeout := 30 * time.Minute  // From config
reopenTime := stats.CircuitOpenedAt.Add(timeout)
fmt.Printf("Will try half-open at: %s\n", reopenTime)
```

**Solutions**:

1. **Wait for Timeout**: Circuit will automatically try half-open after configured timeout

2. **Manual Reset** (if underlying issue is fixed):
   ```go
   registry.ResetCircuitBreaker()
   ```

3. **Fix Root Cause**: Before reset, ensure the underlying problem is resolved

4. **Adjust Configuration**: If circuit opens too easily:
   ```yaml
   remediation:
     circuitBreaker:
       threshold: 10     # Increase threshold
       timeout: 15m      # Reduce timeout
   ```

---

## Best Practices

### 1. Start Conservative

**Recommended Approach**:

```yaml
# Phase 1: Dry-run + Monitoring (Week 1)
remediation:
  enabled: true
  dryRun: true                      # Safe testing
  maxRemediationsPerHour: 5         # Conservative limit

monitors:
  - name: kubelet-health
    remediation:
      enabled: true                 # Enable dry-run testing
```

```yaml
# Phase 2: Single Monitor (Week 2)
remediation:
  enabled: true
  dryRun: false                     # Actually remediate
  maxRemediationsPerHour: 10        # Still limited

monitors:
  - name: kubelet-health            # Start with one critical monitor
    remediation:
      enabled: true
      cooldown: 10m                 # Longer cooldown initially
      maxAttempts: 2                # Fewer attempts
```

```yaml
# Phase 3: Gradual Expansion (Weeks 3-4)
# Enable additional monitors one at a time
# Monitor metrics and history closely
# Adjust cooldowns and limits based on observed behavior
```

---

### 2. Set Appropriate Cooldowns

**Guidelines by Service Type**:

| Service Type | Startup Time | Recommended Cooldown | Example |
|--------------|--------------|---------------------|---------|
| Quick services | < 10s | 3-5 minutes | DNS cache, network flush |
| Standard services | 10-30s | 5-10 minutes | kubelet, docker, containerd |
| Slow services | 30s-2m | 10-15 minutes | databases, large applications |
| Destructive actions | N/A | 30+ minutes | node reboot, network reconfiguration |

**Configuration Example**:
```yaml
monitors:
  - name: kubelet-health
    remediation:
      cooldown: 5m          # kubelet restarts in ~15s

  - name: postgres-health
    remediation:
      cooldown: 15m         # PostgreSQL can take 30-60s

  - name: dns-health
    remediation:
      cooldown: 3m          # DNS flush is instant
```

---

### 3. Limit Attempts

**Recommended Max Attempts**:
- Standard services: **3 attempts**
- Critical services: **2 attempts** (avoid too many disruptions)
- High-risk actions: **1 attempt** (manual intervention preferred)

**Why Limit Attempts?**:
- If 3 attempts fail, the problem likely requires human intervention
- Prevents infinite loops for unfixable issues
- Reduces unnecessary service disruptions

**Configuration**:
```yaml
remediation:
  maxAttemptsGlobal: 3      # Default for all

monitors:
  - name: kubelet-health
    remediation:
      maxAttempts: 3        # Standard service

  - name: network-health
    remediation:
      maxAttempts: 1        # High-risk, prefer manual
```

---

### 4. Enable Circuit Breaker

**Always enable circuit breaker** to prevent cascading failures:

```yaml
remediation:
  circuitBreaker:
    enabled: true
    threshold: 5            # Open after 5 consecutive failures
    timeout: 30m            # Wait 30 minutes before trying again
    successThreshold: 2     # Need 2 successes to close
```

**Tuning Guidelines**:
- **Threshold**: 5-10 failures (lower = more conservative)
- **Timeout**: 30-60 minutes (longer for critical systems)
- **Success Threshold**: 2-3 successes (higher = more confidence)

---

### 5. Monitor Remediation Activity

**Essential Metrics to Track**:

1. **Remediation Success Rate**:
   ```
   Count(successful remediations) / Count(total remediations)
   Target: > 80%
   ```

2. **Remediation Frequency**:
   ```
   Count(remediations per hour)
   Alert if: > maxRemediationsPerHour * 0.8
   ```

3. **Circuit Breaker State**:
   ```
   Alert if: Circuit remains Open > 1 hour
   ```

4. **Failed Remediations by Type**:
   ```
   Group by: remediator_type, problem_type
   Alert if: Same problem failing repeatedly
   ```

**Review Schedule**:
- **Daily**: Check remediation history for anomalies
- **Weekly**: Review success rates and adjust cooldowns/limits
- **Monthly**: Analyze patterns and optimize strategies

---

### 6. Document Custom Remediators

**Required Documentation for Custom Scripts**:

```bash
#!/bin/bash
# /opt/node-doctor/scripts/fix-custom-app.sh
#
# Purpose: Restart custom application when health check fails
# Author: DevOps Team
# Last Updated: 2025-01-15
#
# Prerequisites:
#   - Script must run as root
#   - Application installed in /opt/custom-app
#   - systemd service: custom-app.service
#
# Environment Variables:
#   PROBLEM_TYPE - Type of problem detected
#   PROBLEM_RESOURCE - Resource affected
#   PROBLEM_SEVERITY - Severity level
#
# Exit Codes:
#   0 - Success
#   1 - Application restart failed
#   2 - Prerequisites not met
#
# Safety:
#   - Verifies service exists before restart
#   - Waits up to 30s for service to become active
#   - Logs all actions to /var/log/custom-app-remediation.log

set -e  # Exit on error

LOG_FILE="/var/log/custom-app-remediation.log"

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*" | tee -a "$LOG_FILE"
}

log "Starting remediation for $PROBLEM_TYPE on $PROBLEM_RESOURCE"

# ... rest of script ...
```

**Configuration Documentation**:
```yaml
# config/node-doctor.yaml

monitors:
  - name: custom-app-health
    type: custom-plugin-check
    config:
      scriptPath: /opt/custom-app/healthcheck.sh

    # Custom App Remediation
    # Restarts the application service when health check fails
    # Safety: 10-minute cooldown, max 2 attempts, 5-minute timeout
    remediation:
      enabled: true
      strategy: custom-script
      scriptPath: /opt/node-doctor/scripts/fix-custom-app.sh
      timeout: 5m
      cooldown: 10m
      maxAttempts: 2
      captureOutput: true
```

---

### 7. Test Thoroughly

**Pre-Production Testing**:

1. **Unit Tests** (all remediators):
   ```go
   func TestKubeletRemediator(t *testing.T) {
       // Test CanRemediate logic
       // Test cooldown tracking
       // Test attempt limiting
       // Test panic recovery
   }
   ```

2. **Integration Tests** (with actual services):
   ```go
   func TestKubeletRemediation_Integration(t *testing.T) {
       // Stop service
       // Trigger remediation
       // Verify service restarted
       // Verify cooldown active
   }
   ```

3. **Chaos Testing** (in staging):
   - Intentionally break services
   - Verify remediation succeeds
   - Verify safety mechanisms (cooldown, circuit breaker)
   - Simulate remediation storms (disable rate limiting temporarily)

4. **Dry-Run in Production** (initial deployment):
   ```yaml
   remediation:
     dryRun: true  # Safe testing with real problems
   ```

---

### 8. Have Rollback Plan

**Emergency Disable**:

```yaml
# Quick disable via configuration
remediation:
  enabled: false  # Stops all remediations immediately
```

```bash
# Or via kubectl (if using ConfigMap)
kubectl edit configmap node-doctor-config -n kube-system
# Set enabled: false

# Restart Node Doctor to apply
kubectl delete pod -l app=node-doctor -n kube-system
```

**Circuit Breaker Reset** (if needed after fixing root cause):
```go
// Via HTTP API (if exposed)
POST /api/remediation/circuit/reset

// Or restart Node Doctor to reset circuit breaker state
```

**Selective Disable** (disable specific problem types):
```yaml
monitors:
  - name: kubelet-health
    remediation:
      enabled: false  # Disable only this monitor's remediation
```

---

### 9. Audit Regularly

**Monthly Audit Checklist**:

- [ ] Review remediation history for past 30 days
- [ ] Check remediation success rates by problem type
- [ ] Identify problems that remediate successfully but recur
- [ ] Review cooldown periods - are they appropriate?
- [ ] Check max attempts - are they being hit frequently?
- [ ] Verify circuit breaker configurations
- [ ] Review rate limit settings
- [ ] Update documentation for any configuration changes
- [ ] Test dry-run mode still works
- [ ] Verify custom scripts still execute correctly

**Red Flags to Investigate**:
- Success rate < 70% for any problem type
- Same problem remediated > 5 times/day
- Circuit breaker opening frequently (> once/week)
- Rate limit hit regularly
- Custom scripts timing out

---

### 10. Coordinate with Operations

**Communication Plan**:

1. **Before Enabling Remediation**:
   - Notify ops team of remediation being enabled
   - Share configuration and strategy
   - Explain what actions will be automated
   - Provide rollback procedure

2. **Ongoing Communication**:
   - Weekly summary of remediation activity
   - Immediate notification of circuit breaker openings
   - Monthly review meetings

3. **Escalation**:
   - Define when remediation should be disabled (ops judgment)
   - Establish on-call procedures for remediation failures
   - Document who can reset circuit breakers

**Example Communication Template**:

```
Subject: Node Doctor Remediation Enabled - Kubelet Health

Team,

Node Doctor will begin automatically remediating kubelet health issues
starting 2025-01-20.

What will happen:
- When kubelet fails health check, Node Doctor will restart kubelet.service
- Cooldown: 5 minutes between attempts
- Max attempts: 3 per problem
- Rate limit: 10 remediations/hour across all nodes

Safety mechanisms:
- Dry-run tested for 1 week with 0 issues
- Circuit breaker will disable remediation after 5 failures
- All actions logged and create Kubernetes events

Rollback:
- Set remediation.enabled=false in ConfigMap
- Or contact: [on-call engineer]

Monitoring:
- Dashboard: http://grafana/node-doctor-remediation
- Alerts: Slack #node-doctor-alerts

Questions? Reply to this thread.
```

---

## Summary

Node Doctor's remediation system provides safe, automated problem resolution through:

1. **Multiple Safety Layers**:
   - Per-problem cooldowns and attempt limits (BaseRemediator)
   - Global circuit breaker (RemediatorRegistry)
   - Global rate limiting (RemediatorRegistry)
   - Panic recovery (BaseRemediator)

2. **Five Built-in Remediators**:
   - SystemdRemediator - Manage systemd services
   - CustomRemediator - Execute custom scripts safely
   - NetworkRemediator - Fix network issues
   - DiskRemediator - Clean up disk space
   - RuntimeRemediator - Manage container runtimes

3. **Comprehensive Observability**:
   - Remediation history with detailed records
   - Kubernetes events for all attempts
   - Registry statistics for monitoring
   - Dry-run mode for safe testing

4. **Best Practices**:
   - Start with dry-run mode
   - Set appropriate cooldowns and limits
   - Enable circuit breaker
   - Monitor actively
   - Test thoroughly
   - Have rollback plans

By following these guidelines and using the built-in safety mechanisms, you can confidently deploy automated remediation while maintaining system stability.

**For More Information**:
- Configuration: See `docs/configuration.md` for complete remediation configuration reference
- Monitors: See `docs/monitors.md` for monitor-specific remediation strategies
- Source Code: `pkg/remediators/` for implementation details
