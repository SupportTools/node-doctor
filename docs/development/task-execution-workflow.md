# Task Execution Workflow

**⭐ Core Framework Document** - This workflow defines the mandatory process for executing all development tasks in the Node Doctor project.

## 📝 About This Document

This workflow is adapted from production-tested practices and customized for Node Doctor - a Kubernetes DaemonSet monitoring tool with auto-remediation capabilities.

**Node Doctor Specifics:**
- Single repository, single binary architecture
- Go 1.21+ required
- Kubernetes DaemonSet deployment model
- Monitor/Remediator plugin system
- Direct node access with elevated privileges
- Safety-first remediation approach

## Available Resources

### Task Management System

The project uses PostgreSQL-backed TaskForge for persistent task management:
- **Tasks organized by project** (P1-P5 priority levels, 14 features)
- **Priority-based**: Urgent (🔥), High (🔴), Medium (🟡), Low (🟢)
- **Persistent across sessions**: All Claude Code instances share the same task state
- **MCP Tools**: `mcp__taskforge__getTasks`, `mcp__taskforge__updateTask`, `mcp__taskforge__getBlockedTasks`
- See [TaskForge Usage Guide](taskforge-usage-guide.md) and [Master Task Management](master-task-management.md)

### Development Environment

- Local Kubernetes cluster access available via configured kubeconfig (see Makefile KUBECONFIG_* variables)
- DaemonSet testing environment for node-level operations
- Use this environment to:
  - Test monitor implementations with real node metrics
  - Validate remediator safety mechanisms
  - Run integration tests with Kubernetes API
  - Verify DaemonSet deployment and RBAC configurations
  - Debug monitor/remediator interactions
  - Test exporter functionality (Prometheus metrics, Kubernetes events)
- Always verify environment context before running operations requiring elevated privileges
- Use kube-system namespace for DaemonSet deployments

### Single Repository Structure

Node Doctor uses a single repository with the following structure:

```
node-doctor/
├── cmd/node-doctor/          # Main entry point
├── pkg/
│   ├── monitors/             # Monitor implementations
│   ├── remediators/          # Auto-remediation implementations
│   ├── exporters/            # Prometheus/Kubernetes exporters
│   ├── problemdetector/      # Core orchestration
│   ├── config/               # Configuration types
│   └── utils/                # Shared utilities
├── deploy/                   # Kubernetes manifests
│   ├── daemonset.yaml        # DaemonSet configuration
│   ├── rbac.yaml             # ServiceAccount/ClusterRole
│   └── configmap.yaml        # Monitor/remediator configs
├── docs/                     # Documentation
└── tests/                    # Integration/E2E tests
```

---

## Workflow Steps

### 1. Receive and Initiate Task

- I will provide you with a specific task from TaskForge or by name/description
- Retrieve tasks using `mcp__taskforge__getTasks` with appropriate filters (featureId, priority, status)
- Verify the task priority level and ensure no higher priority tasks are pending
- Check for task dependencies (blockedByTaskId field)
- If task has unmet dependencies or is already completed, report this and await further instruction
- Update task status to "in_progress" using `mcp__taskforge__updateTask` when starting work

**TodoWrite Integration (MANDATORY for session tracking)**:
```
Use TodoWrite tool to create initial task breakdown:
- Research and design phase
- Implementation phase
- Build verification
- QA review
- Testing
- Deployment
- Devils advocate verification
- Task completion

Mark first item as "in_progress" immediately.

NOTE: TodoWrite tracks workflow steps WITHIN the current session.
TaskForge tracks project-level task completion ACROSS all sessions.
Both are mandatory - use TodoWrite for workflow progress, TaskForge for task completion.
```

### 2. Research & Design Phase (MANDATORY - NO CODING WITHOUT THIS)

**Thoroughly research the task requirements:**
- Review architecture.md for system design specifications
- Review monitors.md for monitor specifications and patterns
- Review remediation.md for safety mechanisms and remediator patterns
- Identify integration points with Problem Detector orchestrator
- Review existing monitors/remediators for similar patterns
- Understand safety requirements and failure modes

**Present a comprehensive implementation plan that includes:**

#### Task Overview
Clear summary aligned with Node Doctor architecture (monitors, remediators, exporters)

#### Design Approach
Technical strategy following Node Doctor's established patterns:
- **Monitor Pattern**: Implement Monitor interface with Start()/Stop() methods
- **Channel-Based Communication**: Use Go channels for status updates to Problem Detector
- **Remediator Pattern**: Implement Remediator interface with safety mechanisms
- **Safety First**: Cooldown periods, attempt limiting, circuit breakers, rate limiting
- **Configuration-Driven**: YAML/JSON config with validation
- **Kubernetes Integration**: Use client-go for API interactions, respect RBAC boundaries

#### Files Affected
Complete list with brief explanation:
- **Monitor implementations**: `pkg/monitors/[monitor-name].go`, `pkg/monitors/[monitor-name]_test.go`
- **Remediator implementations**: `pkg/remediators/[remediator-name].go`, `pkg/remediators/[remediator-name]_test.go`
- **Exporter updates**: `pkg/exporters/prometheus.go`, `pkg/exporters/kubernetes.go`
- **Configuration types**: `pkg/config/types.go`, `pkg/config/validation.go`
- **Problem Detector integration**: `pkg/problemdetector/orchestrator.go`
- **Deployment manifests**: `deploy/daemonset.yaml`, `deploy/rbac.yaml`, `deploy/configmap.yaml`
- **Documentation**: `docs/monitors.md`, `docs/remediation.md`

#### Dependencies
Prerequisites and blockers:
- Node access requirements (hostPath, hostNetwork, hostPID)
- Kubernetes permissions (RBAC for ServiceAccount)
- System dependencies (systemd, network tools, disk utilities)
- Go packages (client-go, prometheus/client_golang, etc.)
- Safety mechanism implementations (circuit breaker, rate limiter)

#### Success Criteria
Specific, measurable conditions:
- Project builds successfully: `make build-node-doctor-local`
- All tests pass: `make test-node-doctor-local`
- Validation passes: `make validate-pipeline-local`
- QA agent verification passes
- DaemonSet deploys successfully: `kubectl apply -f deploy/`
- Monitor collects data and reports problems correctly
- Remediator respects safety limits and cooldowns
- Exporters publish metrics/events correctly
- Devils advocate verification passes

#### Risks & Mitigations
Potential issues and fallbacks:
- **Node access risks**: Privileged operations, validate inputs rigorously
- **Remediation risks**: Implement circuit breakers, cooldown periods, attempt limits
- **Performance risks**: Resource limits, efficient polling, async operations
- **Failure modes**: Graceful degradation, proper error handling, monitoring fallbacks

**TodoWrite Update**: Mark "Research and design phase" as completed, mark "Implementation phase" as in_progress.

### 3. Wait for Approval (MANDATORY GATE)

**You must pause here and await explicit instruction.** Do not proceed to coding without approval.

Based on feedback:
- `✅ Approve`: Proceed immediately to Step 4 (Implementation)
- `🔄 Request changes`: Revise plan according to feedback and re-present
- `⏭️ Skip`: Mark task as skipped and await new assignment

**TodoWrite Update**: When approval received, mark "Implementation phase" as in_progress.

### 4. Implementation Phase

Implement the approved design with strict adherence to:

#### Go Version
- **ALWAYS use Go 1.21+** - verify go.mod specifies "go 1.21" or higher

#### Monitor Implementation Pattern
```go
package monitors

import (
    "context"
    "time"
    "github.com/supporttools/node-doctor/pkg/problemdetector"
)

// CPUMonitor implements the Monitor interface
type CPUMonitor struct {
    config    *CPUMonitorConfig
    stopCh    chan struct{}
    statusCh  chan *problemdetector.Status
}

// NewCPUMonitor creates a new CPU monitor instance
func NewCPUMonitor(config *CPUMonitorConfig) (*CPUMonitor, error) {
    // Validate config
    if err := config.Validate(); err != nil {
        return nil, fmt.Errorf("invalid config: %w", err)
    }

    return &CPUMonitor{
        config:   config,
        stopCh:   make(chan struct{}),
        statusCh: make(chan *problemdetector.Status, 10),
    }, nil
}

// Start begins monitoring (runs in goroutine)
func (m *CPUMonitor) Start() (<-chan *problemdetector.Status, error) {
    go m.monitorLoop()
    return m.statusCh, nil
}

// Stop halts monitoring
func (m *CPUMonitor) Stop() {
    close(m.stopCh)
}

func (m *CPUMonitor) monitorLoop() {
    ticker := time.NewTicker(m.config.PollInterval)
    defer ticker.Stop()
    defer close(m.statusCh)

    for {
        select {
        case <-m.stopCh:
            return
        case <-ticker.C:
            status := m.checkCPU()
            if status != nil {
                m.statusCh <- status
            }
        }
    }
}
```

#### Remediator Implementation Pattern
```go
package remediators

import (
    "context"
    "time"
    "github.com/supporttools/node-doctor/pkg/problemdetector"
)

// SystemdRemediator handles systemd service issues
type SystemdRemediator struct {
    config          *SystemdRemediatorConfig
    circuitBreaker  *CircuitBreaker
    rateLimiter     *RateLimiter
    lastRemediation map[string]time.Time
}

// CanRemediate checks if this remediator handles the problem
func (r *SystemdRemediator) CanRemediate(problem problemdetector.Problem) bool {
    return problem.Type == "systemd-service-failed"
}

// Remediate attempts to fix the problem with safety checks
func (r *SystemdRemediator) Remediate(ctx context.Context, problem problemdetector.Problem) error {
    // Check circuit breaker
    if r.circuitBreaker.IsOpen() {
        return fmt.Errorf("circuit breaker open, too many failures")
    }

    // Check rate limiter
    if !r.rateLimiter.Allow() {
        return fmt.Errorf("rate limit exceeded")
    }

    // Check cooldown period
    if lastTime, ok := r.lastRemediation[problem.Resource]; ok {
        if time.Since(lastTime) < r.GetCooldown() {
            return fmt.Errorf("cooldown period not elapsed")
        }
    }

    // Perform remediation
    err := r.restartService(ctx, problem.Resource)

    // Update circuit breaker and tracking
    if err != nil {
        r.circuitBreaker.RecordFailure()
        return err
    }

    r.circuitBreaker.RecordSuccess()
    r.lastRemediation[problem.Resource] = time.Now()
    return nil
}

// GetCooldown returns the cooldown period for this remediator
func (r *SystemdRemediator) GetCooldown() time.Duration {
    return r.config.CooldownPeriod
}
```

#### Configuration Pattern
```go
package config

// NodeDoctorConfig is the root configuration
type NodeDoctorConfig struct {
    Monitors    []MonitorConfig    `json:"monitors" yaml:"monitors"`
    Remediators []RemediatorConfig `json:"remediators" yaml:"remediators"`
    Exporters   ExporterConfig     `json:"exporters" yaml:"exporters"`
    Global      GlobalConfig       `json:"global" yaml:"global"`
}

// Validate performs comprehensive config validation
func (c *NodeDoctorConfig) Validate() error {
    if len(c.Monitors) == 0 {
        return fmt.Errorf("at least one monitor required")
    }

    for i, monitor := range c.Monitors {
        if err := monitor.Validate(); err != nil {
            return fmt.Errorf("monitor[%d]: %w", i, err)
        }
    }

    // Validate remediators, exporters, global config...
    return nil
}
```

#### DaemonSet Deployment
```yaml
# deploy/daemonset.yaml
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
      serviceAccountName: node-doctor
      hostNetwork: true      # Access host networking
      hostPID: true          # Access host processes
      containers:
      - name: node-doctor
        image: ghcr.io/supporttools/node-doctor:latest
        securityContext:
          privileged: true   # Required for remediation
        volumeMounts:
        - name: config
          mountPath: /etc/node-doctor
        - name: host-root
          mountPath: /host
          readOnly: true
      volumes:
      - name: config
        configMap:
          name: node-doctor-config
      - name: host-root
        hostPath:
          path: /
```

#### RBAC Configuration
```yaml
# deploy/rbac.yaml
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
  verbs: ["get", "list", "watch"]
- apiGroups: [""]
  resources: ["events"]
  verbs: ["create", "patch"]
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list"]
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

#### Repository Cleanliness
- Keep repository root clean (only essential config/docs)
- Proper file placement in pkg/, cmd/, deploy/ directories
- No generated files in repository (binaries, coverage files)
- Run `make validate-pipeline-local` to verify standards

#### Write Tests as You Code (MANDATORY)

**Critical Rule: NEVER write code without corresponding tests. Tests are not optional or an afterthought.**

**Unit Tests (Go):**
- For EVERY monitor/remediator, write corresponding tests in `*_test.go` files
- Write tests BEFORE or ALONGSIDE implementation - not after
- Follow Go testing conventions with table-driven tests
- Minimum coverage requirements:
  - **Happy path**: Valid inputs with expected behavior
  - **Edge cases**: Boundary conditions, empty values, maximum limits
  - **Error cases**: Error handling, validation failures, recovery
  - **Safety mechanisms**: Circuit breaker, rate limiter, cooldown enforcement
  - **Concurrent operations**: Race conditions and thread safety
- Run tests incrementally: `go test -v ./pkg/monitors/...`
- Maintain >80% code coverage
- Use mocks for system calls (systemd, disk ops, network)

**Integration Tests:**
- Test monitor integration with Problem Detector
- Test remediator safety mechanisms under load
- Test exporter publishing to Prometheus/Kubernetes
- Verify channel communication patterns
- Place in `*_integration_test.go` files
- Run with build tags: `go test -tags=integration -v ./...`

**End-to-End (E2E) Tests:**
- Required for complete monitoring workflows
- Test scenarios:
  - Monitor detects problem → Problem Detector receives → Exporter publishes
  - Problem detected → Remediator attempts fix → Safety limits enforced
  - DaemonSet deployment → Configuration loading → Monitoring active
- Use realistic test scenarios with Docker/kind clusters
- Place in `tests/e2e/` directory
- Document test setup and requirements

**Testing Workflow Integration:**
```bash
# 1. Write test first (TDD) or alongside code
# 2. Implement feature
# 3. Run unit tests
go test -v ./pkg/monitors/...
go test -v ./pkg/remediators/...

# 4. Run integration tests
go test -tags=integration -v ./...

# 5. Run E2E tests
go test -tags=e2e -v ./tests/e2e/...

# 6. Check coverage
go test -cover -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

**Quality Gates for Testing:**
- [ ] Every new monitor/remediator has unit tests
- [ ] All edge cases and error paths covered
- [ ] Integration tests pass for Problem Detector interaction
- [ ] E2E tests pass for complete workflows
- [ ] Code coverage >80% (target: 85%+)
- [ ] No flaky tests - all deterministic
- [ ] Tests run in reasonable time (<2min for unit, <10min for full suite)

**If implementation reveals issues with approved plan, stop and report immediately**

**TodoWrite Update**: Mark "Implementation phase" as completed, mark "Build verification" as in_progress.

### 5. Quality Assurance Cycle (MANDATORY)

#### Step 5.1: Build Verification (BLOCKER)

```bash
# Build node-doctor binary
make build-node-doctor-local

# Verify binary exists and is executable
./bin/node-doctor --version
```

- **MUST PASS**: Zero compilation errors, zero linting errors
- If build fails, fix immediately before proceeding
- Build must complete successfully before moving to QA

**TodoWrite Update**: Mark "Build verification" as completed, mark "QA review" as in_progress.

#### Step 5.2: QA Agent Review (BLOCKER)

- Invoke QA agent using Task tool with qa-engineer subagent
- Provide QA agent with:
  - Changed files and context
  - Original task requirements
  - Success criteria
  - Architecture document references (architecture.md, monitors.md, remediation.md)

- QA agent will verify:
  - Code quality and Go standards compliance
  - Monitor interface implementation correct
  - Remediator safety mechanisms implemented
  - Configuration validation comprehensive
  - Channel-based communication patterns correct
  - Proper error handling and logging
  - DaemonSet/RBAC configurations secure
  - Integration with Problem Detector correct
  - Security considerations (privileged access, input validation)
  - Test coverage adequate (>80%)

- **BLOCKER**: Address ALL QA findings before proceeding

**TodoWrite Update**: Mark "QA review" as completed, mark "Testing" as in_progress.

#### Step 5.3: Run Full Test Suite

```bash
# Component tests
make test-node-doctor-local

# Full validation (mirrors CI/CD pipeline exactly)
make validate-pipeline-local

# Quick validation (format, vet, lint only - no tests)
make validate-quick

# Race detector
go test -race -v ./...
```

**Validation checks:**
- Code formatting (gofmt)
- Static analysis (go vet)
- Advanced linting (staticcheck)
- Security scanning (gosec)
- Tests with race detection
- Coverage analysis

- Report test coverage and results
- **If tests fail**: Fix issues, re-run QA review if needed, re-test
- Test results must include: files tested, pass/fail counts, coverage %

See [Validation Quick Reference](validation-quick-reference.md) for troubleshooting

**TodoWrite Update**: Mark "Testing" as completed, mark "Deployment" as in_progress.

### 6. Deployment (MANDATORY)

For Node Doctor, deployment means applying Kubernetes manifests to the cluster:

```bash
# Build and push container image
make build-image-node-doctor VERSION=v$(date +%s)
make push-image-node-doctor VERSION=v$(date +%s)

# Update DaemonSet manifest with new image version
# (Or use kustomize/helm for version management)

# Apply Kubernetes resources
kubectl apply -f deploy/rbac.yaml
kubectl apply -f deploy/configmap.yaml
kubectl apply -f deploy/daemonset.yaml

# Verify DaemonSet rollout
kubectl rollout status daemonset/node-doctor -n kube-system

# Check pod status on all nodes
kubectl get pods -n kube-system -l app=node-doctor -o wide

# Verify monitoring is active (check logs)
kubectl logs -n kube-system -l app=node-doctor --tail=50
```

#### Deployment Success Criteria
- DaemonSet manifest applies without errors
- Pods created on all nodes (or selected nodes if nodeSelector used)
- All pods reach Running state
- Health checks pass (if implemented)
- Monitors start and begin reporting status
- Exporters publish metrics to Prometheus
- Kubernetes events created successfully
- No CrashLoopBackOff or ImagePullBackOff errors

**If deployment fails, enter fix cycle (fix → build → QA → deploy)**

**TodoWrite Update**: Mark "Deployment" as completed, mark "Devils advocate verification" as in_progress.

### 7. Devils Advocate Verification (MANDATORY)

- Use Task tool with devils-advocate-qa subagent
- Provide complete context:
  - Original task specification
  - Implementation details
  - Test results (coverage, pass/fail)
  - Deployment status
  - Monitor/remediator behavior observations

- Devils advocate will verify:
  - Original request was fully fulfilled
  - Business requirements met
  - No edge cases missed
  - Implementation matches task specification
  - All success criteria satisfied
  - Safety mechanisms working correctly
  - No security issues introduced
  - DaemonSet deployment stable

#### If issues found: Enter Quality Cycle
- Fix identified issues
- Build verification (`make build-node-doctor-local`)
- QA agent re-review
- Re-test (`make test-node-doctor-local`)
- Re-deploy (kubectl apply)
- Devils advocate re-verification
- **STAY IN CYCLE** until fully resolved

**TodoWrite Update**: When devils advocate approves, mark "Devils advocate verification" as completed, mark "Task completion" as in_progress.

### 8. Task Completion (ONLY AFTER DEVILS ADVOCATE APPROVAL)

#### Provide completion summary:
- **Files changed/created/deleted**:
  - Monitors: `pkg/monitors/[name].go` and tests
  - Remediators: `pkg/remediators/[name].go` and tests
  - Configuration: `pkg/config/types.go` updates
  - Deployment: `deploy/*.yaml` manifests
  - Documentation: `docs/*.md` updates

- **Tests written/updated**:
  - Unit tests with coverage metrics (target: >80%)
  - Integration tests results
  - E2E test scenarios

- **Test results**: All passing with coverage report

- **Deployment confirmation**:
  - DaemonSet deployed successfully
  - Running on X nodes
  - Monitors active and reporting
  - Exporters publishing metrics

- **Safety verification** (for remediator tasks):
  - Cooldown periods enforced
  - Circuit breaker tested
  - Rate limiting active
  - Attempt limits respected

- Any warnings or notes about behavior or limitations

#### Update TaskForge:
```javascript
// Mark task as completed
mcp__taskforge__updateTask({
  taskId: <id>,
  status: "done",
  actualHours: <hours-spent>
});

// This automatically records completion timestamp in database
// Task is now marked complete across all sessions
```

#### Commit with proper format:
```bash
cd /home/mmattox/go/src/github.com/supporttools/node-doctor

git add .
git commit -m "feat(monitors): add CPU throttling monitor

- Implement CPUMonitor with throttling detection
- Add systemd remediator with safety mechanisms
- Update DaemonSet with host access permissions
- Add comprehensive unit tests (87% coverage)
- Add integration tests for Problem Detector
- Task completion: [TaskForge task description]"

git push origin main
```

**TodoWrite Update**: Mark "Task completion" as completed. Clean up todo list - all workflow steps done.

**Await next task assignment**

---

## Node Doctor-Specific Requirements

### ABSOLUTE RULES

1. **ONE TASK AT A TIME** - Never work on multiple tasks simultaneously
2. **RESEARCH BEFORE CODING** - Mandatory research phase, no exceptions
3. **DESIGN COMPLIANCE** - Must follow architecture.md specifications
4. **GO 1.21+ ONLY** - Verify version in go.mod file
5. **BUILD VERIFICATION** - Must pass before QA review
6. **QA VALIDATION** - Must pass before deployment
7. **DEPLOYMENT MANDATORY** - Must deploy DaemonSet to cluster
8. **DEVILS ADVOCATE** - Must pass before completion
9. **QUALITY CYCLE** - Stay in cycle until fully resolved
10. **PRIORITY ENFORCEMENT** - Never skip priority levels (P1 → P2 → P3 → P4 → P5)
11. **SAFETY FIRST** - All remediators must implement safety mechanisms

### FORBIDDEN ACTIONS

- Starting new task before current fully completed
- Skipping workflow steps
- Proceeding with failed builds
- Ignoring QA findings
- Bypassing deployment
- Marking complete without devils advocate approval
- Creating files in repository root (except allowed config/docs)
- Using Go version < 1.21
- Implementing remediators without safety mechanisms
- Skipping cooldown periods in remediators
- Disabling circuit breakers or rate limiters
- Running privileged operations without input validation
- Ignoring RBAC boundaries in Kubernetes operations

### SAFETY-CRITICAL RULES (Remediators)

**Every remediator MUST implement:**
1. **Cooldown Period**: Minimum time between remediation attempts (default: 5 minutes)
2. **Attempt Limiting**: Maximum attempts per time window (default: 3 per hour)
3. **Circuit Breaker**: Stop attempting after consecutive failures (default: 5 failures)
4. **Rate Limiting**: Global rate limit across all problems (default: 10 per minute)
5. **Input Validation**: Validate ALL inputs before system calls
6. **Audit Logging**: Log ALL remediation attempts with outcomes
7. **Dry-Run Mode**: Support dry-run testing without actual changes

**Never:**
- Execute system commands without validation
- Disable safety mechanisms "temporarily"
- Skip cooldown periods for "urgent" fixes
- Ignore circuit breaker state
- Bypass rate limiting

### KUBERNETES DEPLOYMENT RULES

1. **DaemonSet Only**: Node Doctor must run as DaemonSet in kube-system
2. **RBAC Minimal**: Grant only necessary permissions, follow least privilege
3. **Resource Limits**: Always specify CPU/memory limits
4. **Node Selection**: Support nodeSelector/tolerations for selective deployment
5. **Config Management**: Use ConfigMaps for configuration, not environment variables
6. **Secrets Management**: Never hardcode credentials, use Kubernetes Secrets
7. **Health Checks**: Implement liveness and readiness probes

### EMERGENCY PROCEDURES

If workflow cannot be completed due to infrastructure issues:

1. Document blocker in task description
2. Use TaskForge to track blocker:
   ```javascript
   mcp__taskforge__createTask({
     projectId: 41,
     featureId: 250,  // Core Framework or appropriate feature
     name: "Blocker: [description]",
     priority: "urgent",
     status: "todo"
   });
   ```
3. Use `mcp__taskforge__setDependency` to link blocked task
4. Do not start new tasks until blocker resolved
5. Escalate for assistance if needed

If safety mechanisms trigger (circuit breaker, rate limits):
1. **DO NOT disable safety mechanisms**
2. Investigate root cause of failures
3. Fix underlying issue, not the safety mechanism
4. Reset circuit breaker only after verifying fix
5. Document incident and lessons learned

---

## Common Node Doctor Scenarios

### Scenario 1: Adding New Monitor

**Files affected**: `pkg/monitors/`, tests, config, docs

**Workflow**:
1. Create monitor implementation (`pkg/monitors/[name].go`)
2. Implement Monitor interface (Start, Stop methods)
3. Use channels for status communication
4. Write comprehensive unit tests
5. Add configuration type and validation
6. Register monitor in orchestrator
7. Update ConfigMap with monitor config
8. Test in local cluster
9. Update docs/monitors.md

### Scenario 2: Adding New Remediator

**Files affected**: `pkg/remediators/`, tests, config, docs

**Workflow**:
1. Create remediator implementation (`pkg/remediators/[name].go`)
2. Implement Remediator interface (CanRemediate, Remediate, GetCooldown)
3. **MANDATORY**: Implement ALL safety mechanisms
   - Circuit breaker
   - Rate limiter
   - Cooldown tracker
   - Attempt counter
4. Write tests for safety mechanisms
5. Write tests for remediation logic
6. Add configuration type and validation
7. Register remediator in orchestrator
8. Test in isolated environment first
9. Update deploy/rbac.yaml if new permissions needed
10. Update docs/remediation.md

### Scenario 3: Adding New Exporter

**Files affected**: `pkg/exporters/`, tests, config, deployment

**Workflow**:
1. Create exporter implementation
2. Implement Exporter interface
3. Add metrics definitions (Prometheus) or event types (Kubernetes)
4. Write tests for metric accuracy
5. Update DaemonSet with necessary service ports
6. Create Service resource if needed (for Prometheus scraping)
7. Test metric publishing
8. Update documentation

### Scenario 4: Updating DaemonSet Configuration

**Files affected**: `deploy/daemonset.yaml`, `deploy/configmap.yaml`

**Workflow**:
1. Modify DaemonSet manifest
2. Update resource limits if needed
3. Add volume mounts for new host access
4. Update RBAC if new permissions required
5. Test deployment in dev cluster
6. Verify backward compatibility
7. Document changes in deployment guide
8. Roll out with `kubectl apply`

### Scenario 5: Performance Optimization

**Files affected**: Multiple `pkg/` files, tests

**Workflow**:
1. Profile current performance (CPU, memory, goroutines)
2. Identify bottlenecks
3. Implement optimizations
4. Verify no functionality regression
5. Run benchmark tests
6. Update resource limits in DaemonSet
7. Document performance improvements

---

## Quality Gates Summary

| Phase | Gate | Blocker | Tool |
|-------|------|---------|------|
| Research | Design approved | Yes | Manual approval |
| Build | Binary compiles | Yes | `make build-node-doctor-local` |
| QA | Agent review passes | Yes | Task tool (qa-engineer) |
| Test | All tests pass | Yes | `make validate-pipeline-local` |
| Deploy | DaemonSet healthy | Yes | `kubectl apply` + rollout status |
| Verify | Devils advocate approves | Yes | Task tool (devils-advocate-qa) |
| Complete | All gates passed | Yes | TaskForge update + commit |

---

## Workflow Diagram

```
┌─────────────────────────────────────────────────────────────┐
│ 1. Receive Task                                             │
│    - Verify priority & dependencies (TaskForge)             │
│    - Update status to "in_progress"                         │
│    - Create TodoWrite breakdown                             │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│ 2. Research & Design Phase (MANDATORY)                      │
│    - Review architecture.md, monitors.md, remediation.md    │
│    - Identify integration points                            │
│    - Design safety mechanisms                               │
│    - Present implementation plan                            │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│ 3. Wait for Approval (GATE)                                 │
│    ✅ Approve → Continue                                     │
│    🔄 Revise → Back to Step 2                               │
│    ⏭️ Skip → Await new task                                 │
└────────────────────────┬────────────────────────────────────┘
                         │ (approved)
                         ▼
┌─────────────────────────────────────────────────────────────┐
│ 4. Implementation Phase                                     │
│    - Implement monitors/remediators/exporters               │
│    - Follow coding standards                                │
│    - Write tests alongside code (TDD)                       │
│    - Implement safety mechanisms                            │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│ 5.1 Build Verification (BLOCKER)                            │
│     make build-node-doctor-local                            │
│     ❌ Failed → Fix → Retry                                  │
└────────────────────────┬────────────────────────────────────┘
                         │ (passed)
                         ▼
┌─────────────────────────────────────────────────────────────┐
│ 5.2 QA Agent Review (BLOCKER)                               │
│     Review code quality, safety, security                   │
│     ❌ Issues found → Fix → Retry                            │
└────────────────────────┬────────────────────────────────────┘
                         │ (passed)
                         ▼
┌─────────────────────────────────────────────────────────────┐
│ 5.3 Run Test Suite (BLOCKER)                                │
│     make validate-pipeline-local                            │
│     ❌ Tests failed → Fix → Back to 5.2                      │
└────────────────────────┬────────────────────────────────────┘
                         │ (passed)
                         ▼
┌─────────────────────────────────────────────────────────────┐
│ 6. Deployment (MANDATORY)                                   │
│    kubectl apply -f deploy/                                 │
│    kubectl rollout status daemonset/node-doctor             │
│    ❌ Failed → Fix → Back to 5.1                             │
└────────────────────────┬────────────────────────────────────┘
                         │ (deployed)
                         ▼
┌─────────────────────────────────────────────────────────────┐
│ 7. Devils Advocate Verification (MANDATORY)                 │
│    Verify task completion and safety                        │
│    ❌ Issues found → Back to 5.1 (Quality Cycle)            │
└────────────────────────┬────────────────────────────────────┘
                         │ (approved)
                         ▼
┌─────────────────────────────────────────────────────────────┐
│ 8. Task Completion                                          │
│    - Provide completion summary                             │
│    - Update TaskForge status to "done"                      │
│    - Commit with proper message format                      │
│    - Clean up TodoWrite                                     │
│    - Await next task                                        │
└─────────────────────────────────────────────────────────────┘
```

### Monitor/Remediator Development Flow

```
New Feature Request
        │
        ▼
┌──────────────────────────────────────────────────────────────┐
│              Research & Design Phase                         │
│  - Review similar monitors/remediators                       │
│  - Design safety mechanisms (for remediators)                │
│  - Plan channel communication                                │
│  - Design configuration schema                               │
└───────────────────────┬──────────────────────────────────────┘
                        │
                        ▼
┌──────────────────────────────────────────────────────────────┐
│             Implementation                                   │
│  - pkg/monitors/[name].go OR pkg/remediators/[name].go      │
│  - pkg/monitors/[name]_test.go (unit tests)                 │
│  - pkg/config/types.go (config schema)                      │
│  - tests/integration/[name]_test.go                         │
└───────────────────────┬──────────────────────────────────────┘
                        │
                        ▼
┌──────────────────────────────────────────────────────────────┐
│         Quality Assurance                                    │
│  - Build verification                                        │
│  - QA agent review                                           │
│  - Full test suite (unit, integration, e2e)                  │
│  - Safety mechanism validation (remediators)                 │
└───────────────────────┬──────────────────────────────────────┘
                        │
                        ▼
┌──────────────────────────────────────────────────────────────┐
│          Deployment to Cluster                               │
│  - Update deploy/configmap.yaml with new config              │
│  - Update deploy/rbac.yaml if new permissions needed         │
│  - kubectl apply -f deploy/                                  │
│  - Verify DaemonSet rollout                                  │
│  - Monitor logs for new feature activity                     │
└───────────────────────┬──────────────────────────────────────┘
                        │
                        ▼
┌──────────────────────────────────────────────────────────────┐
│    Devils Advocate Verification                              │
│  - Verify feature works as designed                          │
│  - Verify safety mechanisms active (remediators)             │
│  - Verify no regressions in existing monitors                │
│  - Check resource usage impact                               │
└───────────────────────┬──────────────────────────────────────┘
                        │
                        ▼
               Feature Complete
         Monitor active on all nodes
```

---

This workflow ensures systematic, quality-driven development aligned with Node Doctor architecture and Kubernetes best practices. All work follows the **Safety First** principle, especially for remediators with privileged access to nodes.
