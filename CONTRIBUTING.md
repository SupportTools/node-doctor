# Contributing to Node Doctor

Thank you for your interest in contributing to Node Doctor! This document provides guidelines and instructions for contributing to this Kubernetes node health monitoring and auto-remediation system.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Development Workflow](#development-workflow)
- [Coding Standards](#coding-standards)
- [Testing Requirements](#testing-requirements)
- [Pull Request Process](#pull-request-process)
- [Commit Message Guidelines](#commit-message-guidelines)
- [Issue Reporting](#issue-reporting)
- [Security Disclosure Policy](#security-disclosure-policy)
- [Additional Resources](#additional-resources)

## Code of Conduct

This project adheres to the [Contributor Covenant Code of Conduct](https://www.contributor-covenant.org/version/2/1/code_of_conduct/). By participating, you are expected to uphold this code. Please report unacceptable behavior to the project maintainers through GitHub Issues.

### Our Standards

- **Be Respectful**: Treat all contributors with respect and consideration
- **Be Collaborative**: Work together to improve the project
- **Be Professional**: Focus on constructive feedback and solutions
- **Be Inclusive**: Welcome contributors of all backgrounds and experience levels

## Getting Started

### Prerequisites

Before you begin, ensure you have the following installed:

- **Go 1.21 or higher** - Required for building and testing
- **Docker** - For building container images
- **kubectl** - For interacting with Kubernetes
- **kind or minikube** - For local Kubernetes testing (optional but recommended)
- **make** - For using project build targets

### Initial Setup

1. **Fork and Clone the Repository**

   ```bash
   # Fork the repository on GitHub first, then:
   git clone https://github.com/YOUR_USERNAME/node-doctor.git
   cd node-doctor

   # Add upstream remote
   git remote add upstream https://github.com/SupportTools/node-doctor.git
   ```

2. **Install Dependencies**

   ```bash
   # Download Go dependencies
   go mod download

   # Verify Go version
   go version  # Should be 1.21 or higher
   ```

3. **Verify Build**

   ```bash
   # Build the binary
   make build-node-doctor-local

   # Verify it works
   ./bin/node-doctor --version
   ```

4. **Run Tests**

   ```bash
   # Run all tests
   make test

   # Run specific package tests
   go test -v ./pkg/monitors/...
   ```

### Project Structure

```
node-doctor/
├── cmd/node-doctor/      # Main entry point
├── pkg/                  # Core packages
│   ├── monitors/        # Health monitoring implementations
│   ├── remediators/     # Auto-remediation implementations
│   ├── exporters/       # Kubernetes/Prometheus/HTTP exporters
│   ├── detector/        # Problem detection orchestration
│   └── types/           # Core interfaces and types
├── deployment/          # Kubernetes manifests
├── docs/                # Architecture and development guides
├── config/              # Example configurations
└── test/                # Integration and E2E tests
```

## Development Workflow

### Branch Naming Conventions

Use descriptive branch names that indicate the type of change:

- `feature/<description>` - For new features
- `fix/<description>` - For bug fixes
- `docs/<description>` - For documentation changes
- `refactor/<description>` - For code refactoring
- `test/<description>` - For test improvements

**Examples**:
- `feature/cpu-throttling-monitor`
- `fix/memory-leak-in-exporter`
- `docs/update-architecture-guide`
- `refactor/simplify-monitor-interface`

### Development Cycle

1. **Create a Feature Branch**

   ```bash
   git checkout -b feature/your-feature-name
   ```

2. **Make Your Changes**
   - Write code following the [Coding Standards](#coding-standards)
   - Write tests alongside your code (not after)
   - Run validation locally before pushing

3. **Validate Locally**

   ```bash
   # Quick validation (format, vet, lint)
   make validate-quick

   # Full validation (mirrors CI/CD pipeline)
   make validate-pipeline-local
   ```

4. **Commit Your Changes**
   - Follow [Commit Message Guidelines](#commit-message-guidelines)
   - Make atomic commits with clear descriptions

5. **Push and Create Pull Request**

   ```bash
   git push origin feature/your-feature-name
   ```
   - Create a PR on GitHub with a clear description
   - Reference any related issues

6. **Address Review Feedback**
   - Make requested changes
   - Push updates to the same branch
   - Re-request review when ready

## Coding Standards

### Go Style Guidelines

Follow standard Go conventions and best practices:

- **Formatting**: Use `gofmt` or `goimports` (enforced by CI)
- **Linting**: Code must pass `go vet` and `staticcheck`
- **Naming**: Follow Go naming conventions
  - Exported names: `CamelCase`
  - Unexported names: `camelCase`
  - Package names: lowercase, single word
- **Documentation**: All exported functions, types, and packages must have documentation comments

### Code Organization

- **Package Documentation**: Every package should have a package-level comment
- **Interface Design**: Prefer interface-based design for testability and extensibility
- **Error Handling**: Always handle errors; don't ignore them
  - Use `fmt.Errorf` with `%w` for error wrapping
  - Provide context in error messages
- **Concurrency**: Use channels for communication between goroutines
- **Resource Cleanup**: Always use `defer` for cleanup operations

### Monitor Implementation Pattern

When implementing a new monitor, follow this pattern:

```go
package monitors

import (
    "context"
    "time"
    "github.com/supporttools/node-doctor/pkg/types"
)

// CPUMonitor monitors CPU health metrics
type CPUMonitor struct {
    config   *CPUMonitorConfig
    stopCh   chan struct{}
    statusCh chan *types.Status
}

// NewCPUMonitor creates a new CPU monitor instance
func NewCPUMonitor(config *CPUMonitorConfig) (*CPUMonitor, error) {
    if err := config.Validate(); err != nil {
        return nil, fmt.Errorf("invalid config: %w", err)
    }

    return &CPUMonitor{
        config:   config,
        stopCh:   make(chan struct{}),
        statusCh: make(chan *types.Status, 10),
    }, nil
}

// Start begins monitoring (runs in goroutine)
func (m *CPUMonitor) Start() (<-chan *types.Status, error) {
    go m.monitorLoop()
    return m.statusCh, nil
}

// Stop halts monitoring
func (m *CPUMonitor) Stop() {
    close(m.stopCh)
}

func (m *CPUMonitor) monitorLoop() {
    ticker := time.NewTicker(m.config.Interval)
    defer ticker.Stop()
    defer close(m.statusCh)

    for {
        select {
        case <-m.stopCh:
            return
        case <-ticker.C:
            status := m.check()
            if status != nil {
                m.statusCh <- status
            }
        }
    }
}
```

### Remediator Implementation Pattern

**CRITICAL**: All remediators MUST implement safety mechanisms:

```go
package remediators

import (
    "context"
    "time"
    "github.com/supporttools/node-doctor/pkg/types"
)

// ServiceRemediator handles service restart remediation
type ServiceRemediator struct {
    config          *ServiceRemediatorConfig
    circuitBreaker  *CircuitBreaker
    rateLimiter     *RateLimiter
    lastRemediation map[string]time.Time
    mu              sync.Mutex
}

// CanRemediate checks if this remediator handles the problem
func (r *ServiceRemediator) CanRemediate(problem types.Problem) bool {
    return problem.Type == "service-failed"
}

// Remediate attempts to fix the problem with safety checks
func (r *ServiceRemediator) Remediate(ctx context.Context, problem types.Problem) error {
    // Check circuit breaker
    if r.circuitBreaker.IsOpen() {
        return fmt.Errorf("circuit breaker open, too many failures")
    }

    // Check rate limiter
    if !r.rateLimiter.Allow() {
        return fmt.Errorf("rate limit exceeded")
    }

    // Check cooldown period
    r.mu.Lock()
    lastTime, exists := r.lastRemediation[problem.Resource]
    r.mu.Unlock()

    if exists && time.Since(lastTime) < r.GetCooldown() {
        return fmt.Errorf("cooldown period not elapsed")
    }

    // Perform remediation
    err := r.performRemediation(ctx, problem)

    // Update tracking
    if err != nil {
        r.circuitBreaker.RecordFailure()
        return err
    }

    r.circuitBreaker.RecordSuccess()
    r.mu.Lock()
    r.lastRemediation[problem.Resource] = time.Now()
    r.mu.Unlock()

    return nil
}

// GetCooldown returns the cooldown period
func (r *ServiceRemediator) GetCooldown() time.Duration {
    return r.config.CooldownPeriod
}
```

### Safety-First Principles

**For Remediators** (MANDATORY):

1. **Cooldown Period**: Minimum time between attempts (default: 5 minutes)
2. **Attempt Limiting**: Maximum attempts per time window (default: 3 per hour)
3. **Circuit Breaker**: Stop after consecutive failures (default: 5 failures)
4. **Rate Limiting**: Global rate limit (default: 10 per minute)
5. **Input Validation**: Validate ALL inputs before system calls
6. **Audit Logging**: Log ALL remediation attempts with outcomes
7. **Dry-Run Mode**: Support testing without actual changes

**Never**:
- Execute system commands without validation
- Disable safety mechanisms
- Skip cooldown periods
- Bypass rate limiting
- Ignore circuit breaker state

## Testing Requirements

### Test Coverage Expectations

- **Minimum Coverage**: 80% overall code coverage
- **Target Coverage**: 85%+ for production code
- **New Code**: Must include tests for all new functionality

### Unit Tests

All code must have corresponding unit tests:

```bash
# Run all unit tests
make test

# Run specific package tests
go test -v ./pkg/monitors/...
go test -v ./pkg/remediators/...

# Run with coverage
go test -cover -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

**Unit Test Requirements**:
- Test happy path scenarios
- Test error cases and edge conditions
- Test boundary conditions
- Use table-driven tests for multiple scenarios
- Mock external dependencies
- Test concurrent operations for thread safety

**Example Table-Driven Test**:

```go
func TestCPUMonitor_Check(t *testing.T) {
    tests := []struct {
        name        string
        cpuUsage    float64
        threshold   float64
        wantProblem bool
    }{
        {
            name:        "CPU usage below threshold",
            cpuUsage:    50.0,
            threshold:   80.0,
            wantProblem: false,
        },
        {
            name:        "CPU usage above threshold",
            cpuUsage:    85.0,
            threshold:   80.0,
            wantProblem: true,
        },
        {
            name:        "CPU usage at threshold",
            cpuUsage:    80.0,
            threshold:   80.0,
            wantProblem: false,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test implementation
        })
    }
}
```

### Integration Tests

Test component integration:

```bash
# Run integration tests
make test-integration

# Run with build tag
go test -tags=integration -v ./...
```

**Integration tests should verify**:
- Monitor integration with Problem Detector
- Remediator safety mechanisms under load
- Exporter publishing to Prometheus/Kubernetes
- Channel communication patterns
- Configuration loading and validation

### E2E Tests

For major features, provide end-to-end tests:

```bash
# Run E2E tests (requires Kubernetes cluster)
make test-e2e

# Run with build tag
go test -tags=e2e -v ./test/e2e/...
```

**E2E test scenarios**:
- Complete monitoring workflow (detect → export)
- Remediation workflow (detect → remediate → verify)
- DaemonSet deployment and configuration
- Multi-node scenarios

### Race Detection

All tests must pass race detector:

```bash
go test -race -v ./...
```

## Pull Request Process

### Before Creating a PR

1. **Ensure tests pass**:
   ```bash
   make validate-pipeline-local
   ```

2. **Check code coverage** (should not decrease):
   ```bash
   go test -cover ./...
   ```

3. **Update documentation** if needed:
   - Update relevant docs in `docs/`
   - Update README.md if adding new features
   - Add examples if introducing new configuration

### Creating the Pull Request

1. **Write a Clear Title**
   - Use the same format as commit messages
   - Examples: `feat: add disk space monitor`, `fix: memory leak in prometheus exporter`

2. **Provide a Detailed Description**
   - What problem does this solve?
   - What changes were made?
   - How was it tested?
   - Any breaking changes or migration notes?

3. **Link Related Issues**
   - Use keywords: `Fixes #123`, `Closes #456`, `Related to #789`

### PR Review Process

1. **Automated Checks**
   - All CI checks must pass (build, tests, linting)
   - Code coverage must be maintained or improved
   - Security scanning must pass

2. **Code Review**
   - At least one approval required from maintainers
   - Address all review comments
   - Re-request review after making changes

3. **Merge Criteria**
   - All CI checks passing
   - Approved by at least one maintainer
   - No unresolved review comments
   - Up-to-date with main branch
   - Squash merge preferred for clean history

## Commit Message Guidelines

### Format

```
<type>: <short description>

<optional longer description>

<optional footer>
```

### Types

- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation changes
- `refactor`: Code refactoring (no functional changes)
- `test`: Adding or updating tests
- `chore`: Maintenance tasks, dependency updates
- `perf`: Performance improvements
- `style`: Code style changes (formatting, no logic changes)

### Examples

**Simple commit**:
```
feat: add CPU throttling monitor

Implements monitor to detect CPU throttling on nodes.
Includes unit tests with 85% coverage.
```

**Bug fix with issue reference**:
```
fix: resolve memory leak in prometheus exporter

The exporter was not properly cleaning up metric collectors
when monitors were stopped, causing gradual memory growth.

Fixes #234
```

**Breaking change**:
```
refactor: update Monitor interface signature

BREAKING CHANGE: Monitor.Start() now returns error as second
return value to allow initialization failures to be propagated.

Migration: Update all Monitor implementations to return (channel, error)
instead of just channel.
```

### Best Practices

- Keep the first line under 72 characters
- Use imperative mood ("add feature" not "added feature")
- Explain the "why" not just the "what" in the body
- Reference issues and pull requests where relevant
- Use bullet points for multiple changes

## Issue Reporting

### Bug Reports

When reporting a bug, please include:

**Required Information**:
- **Description**: Clear description of the issue
- **Expected Behavior**: What you expected to happen
- **Actual Behavior**: What actually happened
- **Steps to Reproduce**: Detailed steps to reproduce the issue
- **Environment**:
  - Node Doctor version
  - Kubernetes version
  - Node OS and version
  - Go version (if building from source)
- **Logs**: Relevant logs from node-doctor pods
- **Configuration**: Sanitized configuration (remove secrets)

**Example**:

```markdown
### Description
Memory monitor incorrectly reports OOM when swap is enabled

### Expected Behavior
Monitor should account for swap space when calculating available memory

### Actual Behavior
Monitor triggers OOM alert even when swap space is available

### Steps to Reproduce
1. Deploy node-doctor on node with swap enabled
2. Set memory threshold to 80%
3. Fill memory to 75% with swap available
4. Observe false OOM alert

### Environment
- Node Doctor: v0.3.0
- Kubernetes: v1.28.2
- Node OS: Ubuntu 22.04
- Swap: 4GB enabled

### Logs
[Paste relevant logs]

### Configuration
[Paste sanitized config]
```

### Feature Requests

For feature requests, please provide:

- **Use Case**: Why is this feature needed?
- **Proposed Solution**: How should it work?
- **Alternatives Considered**: Other approaches considered
- **Additional Context**: Any other relevant information

## Security Disclosure Policy

### Reporting Security Vulnerabilities

**DO NOT** open public issues for security vulnerabilities.

If you discover a security vulnerability in Node Doctor, please report it responsibly:

1. **Private Disclosure**: Use GitHub Security Advisory:
   - Go to the [Security tab](https://github.com/SupportTools/node-doctor/security)
   - Click "Report a vulnerability"
   - Provide detailed information about the vulnerability

2. **Alternative Contact**: If GitHub Security Advisory is not available:
   - Open a GitHub Issue with the label "Security" (for non-critical issues)
   - For critical vulnerabilities, contact maintainers directly

### What to Include

When reporting a security issue, please provide:

- **Description**: Detailed description of the vulnerability
- **Impact**: What can an attacker accomplish?
- **Steps to Reproduce**: Detailed reproduction steps
- **Affected Versions**: Which versions are affected?
- **Proposed Fix**: If you have suggestions for fixing
- **Public Disclosure Timeline**: Your intended disclosure timeline

### Our Commitment

- **Acknowledgment**: We will acknowledge receipt within 48 hours
- **Initial Assessment**: We will provide an initial assessment within 5 business days
- **Fix Timeline**: We aim to release fixes for critical vulnerabilities within 30 days
- **Credit**: We will credit security researchers (unless they prefer to remain anonymous)
- **Coordinated Disclosure**: We follow a 90-day responsible disclosure policy

### Security Best Practices

When contributing to Node Doctor:

- **Never commit secrets**: No credentials, API keys, or tokens in code
- **Validate all inputs**: Especially for remediators executing system commands
- **Follow least privilege**: Request only necessary Kubernetes permissions
- **Use secure defaults**: Safe defaults in configurations
- **Audit logging**: Log security-relevant operations
- **Dependency scanning**: Keep dependencies up-to-date

## Additional Resources

### Documentation

- **[Architecture Guide](docs/architecture.md)** - System design and component overview
- **[Monitors Guide](docs/monitors.md)** - Monitor implementation details
- **[Remediation Guide](docs/remediation.md)** - Remediator patterns and safety mechanisms
- **[Task Execution Workflow](docs/development/task-execution-workflow.md)** - Development process
- **[Validation Quick Reference](docs/development/validation-quick-reference.md)** - Testing and validation

### Build Targets

```bash
# Build node-doctor binary
make build-node-doctor-local

# Run all tests
make test

# Run validation pipeline (mirrors CI/CD)
make validate-pipeline-local

# Quick validation (format, vet, lint only)
make validate-quick

# Build and push Docker image
make build-image-node-doctor VERSION=v1.0.0
make push-image-node-doctor VERSION=v1.0.0

# Deploy to Kubernetes
kubectl apply -f deployment/
```

### Useful Commands

```bash
# Format code
go fmt ./...

# Run linter
staticcheck ./...

# Run vet
go vet ./...

# Check test coverage
go test -cover ./...

# Run race detector
go test -race ./...

# Update dependencies
go mod tidy
```

### Communication

- **GitHub Issues**: For bug reports and feature requests
- **Pull Requests**: For code contributions and discussions
- **GitHub Discussions**: For general questions and community discussion

### Getting Help

If you need help:

1. Check existing documentation in the `docs/` directory
2. Search existing GitHub Issues
3. Ask in GitHub Discussions
4. Open a new issue with the "question" label

---

## Thank You!

Thank you for contributing to Node Doctor! Your contributions help improve Kubernetes node health monitoring and make infrastructure more reliable for everyone.

We appreciate your time and effort in following these guidelines. Together, we can build a robust, safe, and maintainable project that serves the Kubernetes community.

**Happy coding!** 🚀
