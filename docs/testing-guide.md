# Node Doctor Developer Testing Guide

This guide covers testing practices, patterns, and how to run tests for the Node Doctor project.

## Test Organization

```
node-doctor/
├── pkg/
│   ├── monitors/
│   │   ├── cpu/
│   │   │   ├── cpu_test.go          # Unit tests
│   │   │   └── ...
│   ├── controller/
│   │   └── *_test.go                # Unit tests
│   └── */
│       └── *_test.go                # Package-level unit tests
├── test/
│   ├── fixtures.go                  # Common test fixtures
│   ├── helpers.go                   # Test assertion helpers
│   ├── mocks.go                     # Mock implementations
│   ├── example_test.go              # Example test patterns
│   ├── integration/
│   │   ├── controller/              # Controller integration tests
│   │   ├── kubernetes/              # K8s client integration tests
│   │   └── workflows/               # End-to-end workflow tests
│   └── e2e/
│       ├── cluster/                 # KIND cluster configs
│       ├── scenarios/               # E2E test scenarios
│       └── utils/                   # E2E test utilities
└── testdata/                        # Test data files
```

## Running Tests

### Unit Tests

```bash
# Run all unit tests
make test

# Run with verbose output
make test-verbose

# Run with race detector
make test-race

# Run tests for a specific package
go test ./pkg/monitors/cpu/...
go test ./pkg/controller/...

# Run with coverage
go test -cover ./pkg/...
```

### Integration Tests

```bash
# Run integration tests (requires local environment)
make test-integration

# Run controller integration tests specifically
go test -tags=integration ./test/integration/controller/...

# Run with race detector
go test -race -tags=integration ./test/integration/...
```

### E2E Tests

```bash
# Create KIND cluster and run E2E tests
make test-e2e

# Run E2E tests against existing cluster
go test -tags=e2e ./test/e2e/scenarios/...

# Run specific E2E test
go test -tags=e2e -run TestE2E_ControllerCoordination ./test/e2e/scenarios/...
```

### Coverage Requirements

The project enforces minimum coverage thresholds:

| Package | Minimum Coverage |
|---------|-----------------|
| `pkg/monitors/*` | 80% |
| `pkg/controller/*` | 75% |
| `pkg/detectors/*` | 75% |
| Overall | 70% |

Check coverage:

```bash
# Generate coverage report
make coverage

# View HTML coverage report
go test -coverprofile=coverage.out ./pkg/...
go tool cover -html=coverage.out
```

---

## Test Patterns

### Table-Driven Tests

Use table-driven tests for comprehensive test coverage:

```go
func TestCPUMonitor_CheckHealth(t *testing.T) {
    tests := []struct {
        name         string
        loadAvg      float64
        numCPUs      int
        wantHealthy  bool
        wantSeverity types.ProblemSeverity
    }{
        {
            name:         "healthy under threshold",
            loadAvg:      2.0,
            numCPUs:      4,
            wantHealthy:  true,
            wantSeverity: types.ProblemNone,
        },
        {
            name:         "warning at 80% threshold",
            loadAvg:      3.2,
            numCPUs:      4,
            wantHealthy:  false,
            wantSeverity: types.ProblemWarning,
        },
        {
            name:         "critical at 95% threshold",
            loadAvg:      3.8,
            numCPUs:      4,
            wantHealthy:  false,
            wantSeverity: types.ProblemCritical,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            monitor := NewCPUMonitor(tt.numCPUs)
            status := monitor.CheckHealth(tt.loadAvg)

            test.AssertEqual(t, tt.wantHealthy, status.Healthy)
            if !tt.wantHealthy {
                test.AssertEqual(t, tt.wantSeverity, status.Problems[0].Severity)
            }
        })
    }
}
```

### Using Fixtures

The `test` package provides fixture builders for common types:

```go
import "github.com/supporttools/node-doctor/test"

func TestProblemHandling(t *testing.T) {
    // Using the fixture builder
    problem := test.NewProblemFixture().
        WithType("test-problem").
        WithResource("node-1").
        WithSeverity(types.ProblemCritical).
        WithMessage("Test error occurred").
        WithMetadata("component", "kubelet").
        Build()

    // Using predefined fixtures
    kubeletProblem := test.KubeletFailedProblem()
    memoryProblem := test.MemoryPressureProblem()
    diskProblem := test.DiskPressureProblem()

    // Using status fixtures
    healthyStatus := test.HealthyKubeletStatus()
    unhealthyStatus := test.UnhealthyKubeletStatus()
    multiStatus := test.MultiProblemStatus()
}
```

### Using Mocks

The `test` package provides mock implementations:

```go
import "github.com/supporttools/node-doctor/test"

func TestWithMockKubernetesClient(t *testing.T) {
    ctx := context.Background()
    client := test.NewMockKubernetesClient()

    // Configure mock response
    client.SetNodeResponse("node-1", map[string]interface{}{
        "name":   "node-1",
        "status": "Ready",
    })

    // Use the mock
    node, err := client.GetNode(ctx, "node-1")
    test.AssertNoError(t, err)

    // Verify interactions
    requests := client.GetNodeRequests()
    test.AssertEqual(t, 1, len(requests))
}

func TestWithMockCommandExecutor(t *testing.T) {
    ctx := context.Background()
    executor := test.NewMockCommandExecutor()

    // Configure command outputs
    executor.SetCommand("systemctl", "active", nil)
    executor.SetCommand("docker", "Docker version 20.10", nil)
    executor.SetDefaultBehavior("", fmt.Errorf("command not found"))

    // Execute commands
    output, err := executor.Execute(ctx, "systemctl", "is-active", "kubelet")
    test.AssertNoError(t, err)
    test.AssertEqual(t, "active", output)

    // Verify execution
    test.AssertTrue(t, executor.WasCommandExecuted("systemctl"))
}

func TestWithMockHTTPServer(t *testing.T) {
    server := test.NewMockHTTPServer()
    defer server.Close()

    // Configure response
    server.SetResponse(200, `{"status":"ok"}`)

    // Make request
    resp, err := server.Client().Get(server.URL)
    test.AssertNoError(t, err)

    // Verify
    test.AssertEqual(t, 200, resp.StatusCode)
    requests := server.GetRequests()
    test.AssertEqual(t, 1, len(requests))
}
```

### HTTP Handler Testing

Use `httptest` for testing HTTP handlers:

```go
import (
    "net/http"
    "net/http/httptest"
    "github.com/supporttools/node-doctor/test"
)

func TestControllerAPI(t *testing.T) {
    // Create server
    config := controller.DefaultControllerConfig()
    server, err := controller.NewServer(config)
    test.AssertNoError(t, err)

    // Create test server using Handler() method
    ts := httptest.NewServer(server.Handler())
    defer ts.Close()

    // Make requests
    resp, err := http.Get(ts.URL + "/healthz")
    test.AssertNoError(t, err)
    test.AssertEqual(t, http.StatusOK, resp.StatusCode)
}
```

### Assertion Helpers

Use the test assertion helpers:

```go
import "github.com/supporttools/node-doctor/test"

func TestWithAssertions(t *testing.T) {
    // Equality
    test.AssertEqual(t, expected, actual, "values should match")

    // Error handling
    test.AssertNoError(t, err, "operation should succeed")
    test.AssertError(t, err, "operation should fail")

    // Boolean conditions
    test.AssertTrue(t, condition, "condition should be true")
    test.AssertFalse(t, condition, "condition should be false")

    // Nil checks
    test.AssertNil(t, value, "value should be nil")
    test.AssertNotNil(t, value, "value should not be nil")

    // Eventually (for async operations)
    test.Eventually(t, func() bool {
        return checkCondition()
    }, 5*time.Second, 100*time.Millisecond, "condition should become true")
}
```

### Test Context with Timeout

```go
import "github.com/supporttools/node-doctor/test"

func TestWithTimeout(t *testing.T) {
    ctx, cancel := test.TestContext(t, 5*time.Second)
    defer cancel()

    // Use ctx for operations that should complete within 5 seconds
    result, err := doOperationWithContext(ctx)
    test.AssertNoError(t, err)
}
```

---

## Integration Tests

Integration tests verify component interactions and are located in `test/integration/`.

### Controller Integration Tests

`test/integration/controller/controller_integration_test.go`:

```go
//go:build integration
// +build integration

package controller

import (
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/supporttools/node-doctor/pkg/controller"
    "github.com/supporttools/node-doctor/test"
)

func TestLeaseCoordinationFlow(t *testing.T) {
    config := controller.DefaultControllerConfig()
    config.Coordination.MaxConcurrentRemediations = 2

    t.Run("single node lease lifecycle", func(t *testing.T) {
        server, err := controller.NewServer(config)
        test.AssertNoError(t, err)

        ts := httptest.NewServer(server.Handler())
        defer ts.Close()

        // Request lease
        leaseReq := controller.LeaseRequest{
            NodeName:        "node-1",
            RemediationType: "restart-kubelet",
        }
        resp, data := doRequest(t, ts, http.MethodPost, "/api/v1/leases", leaseReq)
        test.AssertEqual(t, http.StatusOK, resp.StatusCode)

        // Verify lease is active
        resp, _ = doRequest(t, ts, http.MethodGet, "/api/v1/leases", nil)
        test.AssertEqual(t, http.StatusOK, resp.StatusCode)

        // Release lease
        leaseID := data["data"].(map[string]interface{})["leaseId"].(string)
        resp, _ = doRequest(t, ts, http.MethodDelete, "/api/v1/leases/"+leaseID, nil)
        test.AssertEqual(t, http.StatusOK, resp.StatusCode)
    })
}
```

### Running Integration Tests

```bash
# All integration tests
go test -tags=integration ./test/integration/...

# Specific test
go test -tags=integration -run TestLeaseCoordinationFlow ./test/integration/controller/...

# With race detection and verbose output
go test -race -v -tags=integration ./test/integration/...
```

---

## E2E Tests

E2E tests run against a real Kubernetes cluster (typically KIND).

### E2E Test Structure

```go
//go:build e2e
// +build e2e

package scenarios

import (
    "context"
    "testing"
    "time"

    "github.com/supporttools/node-doctor/test/e2e/utils"
)

func TestE2E_ControllerCoordination(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
    defer cancel()

    kubeContext := utils.GetKubeContext()

    // Setup
    err := deployController(ctx, kubeContext)
    if err != nil {
        t.Fatalf("Failed to deploy controller: %v", err)
    }
    defer cleanupController(ctx, kubeContext)

    // Wait for ready
    err = utils.WaitForDeploymentReady(ctx, kubeContext, "node-doctor", "node-doctor-controller")
    if err != nil {
        t.Fatalf("Controller not ready: %v", err)
    }

    // Run tests
    t.Run("lease request and release", func(t *testing.T) {
        // Test implementation
    })
}
```

### KIND Cluster Setup

```bash
# Create KIND cluster with config
kind create cluster --config test/e2e/cluster/kind-config.yaml

# Get kubeconfig
export KUBECONFIG="$(kind get kubeconfig-path)"

# Run E2E tests
go test -tags=e2e ./test/e2e/scenarios/...

# Cleanup
kind delete cluster
```

### E2E Test Utilities

`test/e2e/utils/kubectl.go` provides kubectl helpers:

```go
import "github.com/supporttools/node-doctor/test/e2e/utils"

// Apply manifest
err := utils.ApplyManifest(ctx, kubeContext, manifestYAML)

// Wait for deployment
err := utils.WaitForDeploymentReady(ctx, kubeContext, "namespace", "deployment-name")

// Get nodes
nodes, err := utils.GetNodeNames(ctx, kubeContext)
workerNodes, err := utils.GetWorkerNodes(ctx, kubeContext)

// Execute command on KIND node
output, err := utils.ExecOnNode(ctx, kubeContext, "node-name", "command", "args")

// Port forward
cmd, err := utils.PortForward(ctx, kubeContext, "namespace", "service", 8080, 8080)
defer cmd.Process.Kill()
```

---

## Writing New Tests

### Unit Test Template

```go
package mypackage

import (
    "testing"
    "github.com/supporttools/node-doctor/test"
)

func TestMyFunction(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        want     string
        wantErr  bool
    }{
        {
            name:    "valid input",
            input:   "test",
            want:    "TEST",
            wantErr: false,
        },
        {
            name:    "empty input",
            input:   "",
            want:    "",
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := MyFunction(tt.input)

            if tt.wantErr {
                test.AssertError(t, err, "expected error")
                return
            }

            test.AssertNoError(t, err, "unexpected error")
            test.AssertEqual(t, tt.want, got, "result mismatch")
        })
    }
}
```

### Integration Test Template

```go
//go:build integration
// +build integration

package mypackage_test

import (
    "context"
    "testing"
    "time"

    "github.com/supporttools/node-doctor/test"
)

func TestMyIntegration(t *testing.T) {
    ctx, cancel := test.TestContext(t, 30*time.Second)
    defer cancel()

    // Setup
    resource := setupTestResource(t)
    defer cleanupTestResource(t, resource)

    // Test
    t.Run("test case", func(t *testing.T) {
        result, err := operationUnderTest(ctx, resource)
        test.AssertNoError(t, err)
        test.AssertEqual(t, expected, result)
    })
}
```

### E2E Test Template

```go
//go:build e2e
// +build e2e

package scenarios

import (
    "context"
    "testing"
    "time"

    "github.com/supporttools/node-doctor/test/e2e/utils"
)

func TestE2E_MyScenario(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
    defer cancel()

    kubeContext := utils.GetKubeContext()
    if kubeContext == "" {
        t.Skip("No kubernetes context available")
    }

    // Deploy resources
    t.Log("Deploying test resources...")
    err := deployResources(ctx, kubeContext)
    if err != nil {
        t.Fatalf("Failed to deploy: %v", err)
    }
    defer cleanupResources(ctx, kubeContext)

    // Wait for ready
    err = utils.WaitForDeploymentReady(ctx, kubeContext, "namespace", "deployment")
    if err != nil {
        t.Fatalf("Resources not ready: %v", err)
    }

    // Run test scenarios
    t.Run("scenario 1", func(t *testing.T) {
        // Test implementation
    })

    t.Run("scenario 2", func(t *testing.T) {
        // Test implementation
    })
}
```

---

## Debugging Tests

### Verbose Output

```bash
# Verbose test output
go test -v ./pkg/...

# Show individual test timing
go test -v -json ./pkg/... | jq 'select(.Action == "pass" or .Action == "fail")'
```

### Debugging with Delve

```bash
# Debug specific test
dlv test ./pkg/monitors/cpu -- -test.run TestCPUMonitor

# Set breakpoints and step through
(dlv) break cpu_test.go:42
(dlv) continue
(dlv) step
```

### Race Detection

```bash
# Run with race detector
go test -race ./pkg/...

# Integration tests with race detector
go test -race -tags=integration ./test/integration/...
```

### Test Caching

```bash
# Clear test cache
go clean -testcache

# Force test re-run
go test -count=1 ./pkg/...
```

---

## CI/CD Integration

Tests run automatically in CI:

1. **Pull Request Checks**: All unit and integration tests
2. **Merge to Main**: Full test suite including E2E
3. **Release**: Complete test suite with extended timeouts

See `.github/workflows/` for CI configuration.

### Running CI Locally

```bash
# Run full CI validation
make ci

# Run quick checks
make check

# Lint + test
make lint test
```

---

## Additional Resources

- [Operational Testing Guide](testing.md) - Testing monitors in live clusters
- [Configuration Guide](configuration.md) - Test configuration options
- [Architecture Overview](architecture.md) - System design for test planning
