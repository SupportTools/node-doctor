# Integration Tests

This directory contains integration tests for Node Doctor that test complete workflows and component interactions.

## Test Organization

### `workflows/` - Monitor → Detector → Exporter Workflows
Tests the complete data flow from monitors through the detector to exporters.

**Tests:**
- `TestMonitorToDetectorToExporterFlow` - Basic end-to-end workflow
- `TestMultipleMonitorsWorkflow` - Multiple monitors running concurrently
- `TestProblemDeduplication` - Deduplication of duplicate problems

**Key Scenarios:**
- Healthy status updates (Info events, True conditions)
- Unhealthy status updates (Error/Warning events, False conditions)
- Multiple monitors sending statuses simultaneously
- Problem detection from events and conditions
- Problem export to exporters

### `remediation/` - Remediation End-to-End Flows
Tests the complete remediation pipeline from problem detection to remediation execution.

**Tests:**
- `TestRemediationEndToEndFlow` - Basic remediation workflow with cooldown
- `TestCircuitBreakerIntegration` - Circuit breaker failure prevention
- `TestRateLimitingIntegration` - Rate limiting prevents remediation storms
- `TestConcurrentRemediationAttempts` - Thread safety of remediation
- `TestMaxAttemptsLimiting` - Max attempts per problem limiting

**Key Scenarios:**
- Successful remediation execution
- Cooldown period enforcement (per-problem)
- Circuit breaker states (Closed → Open → Half-Open → Closed)
- Rate limiting (max remediations per hour)
- Concurrent remediation attempts on different resources
- Max attempts limiting per problem

### `config/` - Configuration Loading and Validation
Tests configuration file loading, parsing, and validation.

**Tests:**
- `TestLoadConfigYAML` - YAML file loading
- `TestLoadConfigJSON` - JSON file loading
- `TestEnvironmentVariableSubstitution` - Env var substitution in configs
- `TestConfigValidation` - Validation rules enforcement
- `TestDefaultConfig` - Default configuration generation
- `TestLoadConfigOrDefault` - Fallback to default config
- `TestSaveAndLoadConfig` - Round-trip save/load
- `TestComplexConfiguration` - Complex real-world configuration

**Key Scenarios:**
- YAML and JSON parsing
- Environment variable substitution (${VAR_NAME})
- Validation of required fields
- Validation of interval > timeout
- Default value application
- Configuration file save/load round-trip

### `concurrent/` - Concurrent Operation Tests
Tests thread safety and concurrent operations across the system.

**Tests:**
- `TestConcurrentMonitorStatusUpdates` - Multiple monitors sending concurrently
- `TestConcurrentRemediationWithDifferentResources` - Parallel remediation
- `TestConcurrentRemediationSameResource` - Cooldown/locking on shared resource
- `TestConcurrentExporterOperations` - Multiple exporters receiving concurrently
- `TestRaceConditionDetection` - Data race detection under load

**Key Scenarios:**
- 10+ monitors sending 50+ statuses each concurrently
- 100+ concurrent remediation attempts on different resources
- Concurrent remediation attempts on the same resource (cooldown enforcement)
- Multiple exporters receiving the same statuses concurrently
- Continuous read/write operations to detect race conditions

## Running Integration Tests

### Run All Integration Tests
```bash
go test ./test/integration/... -v
```

### Run Specific Test Suite
```bash
go test ./test/integration/workflows -v
go test ./test/integration/remediation -v
go test ./test/integration/config -v
go test ./test/integration/concurrent -v
```

### Run with Race Detector (Recommended for Concurrent Tests)
```bash
go test ./test/integration/concurrent -v -race
```

### Run Specific Test
```bash
go test ./test/integration/workflows -v -run TestMonitorToDetectorToExporterFlow
```

### Run with Coverage
```bash
go test ./test/integration/... -v -coverprofile=coverage.out
go tool cover -html=coverage.out
```

## Test Utilities

Integration tests use shared test utilities from `test/helpers.go` and `test/fixtures.go`:

### Helper Functions (test/helpers.go)
- `TestContext(t, timeout)` - Create test context with timeout
- `AssertNoError(t, err, msg)` - Assert no error occurred
- `AssertError(t, err, msg)` - Assert error occurred
- `AssertEqual(t, expected, actual, msg)` - Assert equality
- `AssertTrue(t, condition, msg)` - Assert boolean true
- `AssertFalse(t, condition, msg)` - Assert boolean false
- `Eventually(t, condition, timeout, interval, msg)` - Retry until condition met
- `TempDir(t)` - Create temporary test directory
- `TempFile(t, content)` - Create temporary test file

### Mock Objects (test/helpers.go)
- `MockKubernetesClient` - Mock Kubernetes API client
- `MockHTTPServer` - Mock HTTP server using httptest
- `MockCommandExecutor` - Mock for system command execution

### Test Fixtures (test/fixtures.go)
- `NewProblemFixture()` - Fluent builder for Problem objects
- `NewStatusFixture()` - Fluent builder for Status objects
- Pre-built fixtures:
  - `KubeletFailedProblem()`
  - `MemoryPressureProblem()`
  - `DiskPressureProblem()`
  - `HealthyKubeletStatus()`
  - `UnhealthyMemoryStatus()`
  - etc.

## Integration Test Patterns

### Mock Monitor Pattern
```go
mockMonitor := newMockMonitor("test-monitor")

// Send statuses
mockMonitor.SendStatus(&types.Status{
    Source: "test-monitor",
    Events: []types.Event{...},
    Conditions: []types.Condition{...},
})
```

### Mock Exporter Pattern
```go
mockExporter := newMockExporter()

// After running test...
statuses := mockExporter.GetStatuses()
problems := mockExporter.GetProblems()

// Verify counts and content
test.AssertEqual(t, 5, len(statuses), "Expected 5 statuses")
```

### Mock Remediator Pattern
```go
mockRemediator := newMockRemediator("test-remediator", 1*time.Second)

// Register with registry
registry.RegisterRemediator("problem-type", mockRemediator)

// After remediation...
count := mockRemediator.GetAttemptCount()
test.AssertEqual(t, 1, count, "Expected 1 remediation attempt")
```

### Detector Workflow Pattern
```go
// Create components
monitor := newMockMonitor("test")
exporter := newMockExporter()

// Create detector
pd, err := detector.NewProblemDetector(config, []types.Monitor{monitor}, []types.Exporter{exporter})

// Run in background
var wg sync.WaitGroup
wg.Add(1)
go func() {
    defer wg.Done()
    pd.Run(ctx)
}()

// Give time to start
time.Sleep(100 * time.Millisecond)

// Send test data...
monitor.SendStatus(...)

// Wait for processing
time.Sleep(200 * time.Millisecond)

// Verify results
statuses := exporter.GetStatuses()
problems := exporter.GetProblems()

// Cleanup
cancel()
wg.Wait()
```

## Common Issues and Solutions

### Issue: Test timeouts
**Solution:** Increase context timeout or add more wait time for async operations

### Issue: Flaky tests due to timing
**Solution:** Use `Eventually()` helper instead of fixed `time.Sleep()`

### Issue: Race conditions detected
**Solution:** Review locking in the tested code, run with `-race` flag

### Issue: Channel deadlocks
**Solution:** Ensure channels are buffered or properly drained in goroutines

## Coverage Goals

Integration tests should focus on:
- ✅ Complete workflows (monitor → detector → exporter)
- ✅ Cross-component interactions
- ✅ Concurrency and thread safety
- ✅ Configuration loading and validation
- ✅ Error handling and recovery
- ✅ Safety mechanisms (cooldown, circuit breaker, rate limiting)

Unit tests (in pkg/) should focus on:
- Individual component logic
- Edge cases and error conditions
- Input validation
- State transitions

## Adding New Integration Tests

1. Choose appropriate directory based on what you're testing
2. Create `*_test.go` file with descriptive name
3. Use existing patterns (mock objects, helper functions)
4. Include both success and failure scenarios
5. Test concurrent operations where applicable
6. Add test to this README

## Test Execution Order

Integration tests are designed to be:
- **Independent** - Can run in any order
- **Isolated** - Each test creates its own components
- **Concurrent-safe** - Can run in parallel with `-p` flag
- **Repeatable** - Same results every time

Run tests in parallel for faster execution:
```bash
go test ./test/integration/... -v -p 4
```
