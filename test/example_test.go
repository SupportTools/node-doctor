package test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// Example tests demonstrating the test infrastructure usage.

func TestProblemFixture_Example(t *testing.T) {
	// Create a problem using the fixture builder
	problem := NewProblemFixture().
		WithType("test-problem").
		WithResource("test-resource-001").
		WithSeverity(types.ProblemCritical).
		WithMessage("Something went wrong").
		WithMetadata("component", "test-component").
		WithMetadata("error_code", "ERR_001").
		Build()

	// Verify the problem was created correctly
	AssertEqual(t, "test-problem", problem.Type, "problem type should match")
	AssertEqual(t, "test-resource-001", problem.Resource, "resource should match")
	AssertEqual(t, types.ProblemCritical, problem.Severity, "severity should match")
	AssertEqual(t, "Something went wrong", problem.Message, "message should match")
	AssertEqual(t, "test-component", problem.Metadata["component"], "metadata component should match")
	AssertEqual(t, "ERR_001", problem.Metadata["error_code"], "metadata error_code should match")
}

func TestStatusFixture_Example(t *testing.T) {
	// Create a status with multiple events and conditions
	event1 := types.NewEvent(types.EventError, "KubeletFailed", "Kubelet service has failed")
	event2 := types.NewEvent(types.EventWarning, "MemoryPressure", "Memory pressure detected")
	condition1 := types.NewCondition("KubeletReady", types.ConditionFalse, "ServiceFailed", "Kubelet service has failed")
	condition2 := types.NewCondition("MemoryPressure", types.ConditionTrue, "HighMemoryUsage", "Memory usage is high")

	status := NewStatusFixture().
		WithSource("node-monitor").
		WithEvent(event1).
		WithEvent(event2).
		WithCondition(condition1).
		WithCondition(condition2).
		Build()

	// Verify the status
	AssertEqual(t, "node-monitor", status.Source, "source should match")
	AssertEqual(t, 2, len(status.Events), "should have 2 events")
	AssertEqual(t, 2, len(status.Conditions), "should have 2 conditions")
	AssertEqual(t, types.EventError, status.Events[0].Severity, "first event should be error")
	AssertEqual(t, types.ConditionFalse, status.Conditions[0].Status, "first condition should be false")
}

func TestMockKubernetesClient_Example(t *testing.T) {
	ctx := context.Background()

	// Create mock client
	client := NewMockKubernetesClient()

	// Configure node response
	nodeData := map[string]interface{}{
		"name":   "test-node-01",
		"status": "Ready",
	}
	client.SetNodeResponse("test-node-01", nodeData)

	// Get node
	node, err := client.GetNode(ctx, "test-node-01")
	AssertNoError(t, err, "should get node without error")

	// Verify response
	nodeMap := node.(map[string]interface{})
	AssertEqual(t, "test-node-01", nodeMap["name"], "node name should match")
	AssertEqual(t, "Ready", nodeMap["status"], "node status should match")

	// Verify request was tracked
	requests := client.GetNodeRequests()
	AssertEqual(t, 1, len(requests), "should have 1 request")
	AssertEqual(t, "test-node-01", requests[0], "request should be for test-node-01")
}

func TestMockHTTPServer_Example(t *testing.T) {
	// Create mock HTTP server
	server := NewMockHTTPServer()
	defer server.Close()

	// Configure response
	server.SetResponse(200, `{"status":"ok"}`)

	// Make request to mock server
	resp, err := server.Client().Get(server.URL)
	AssertNoError(t, err, "HTTP request should succeed")
	defer resp.Body.Close()

	// Verify response
	AssertEqual(t, 200, resp.StatusCode, "status code should be 200")

	// Verify request was tracked
	requests := server.GetRequests()
	AssertEqual(t, 1, len(requests), "should have 1 request")
	AssertEqual(t, "GET", requests[0].Method, "request method should be GET")
}

func TestMockCommandExecutor_Example(t *testing.T) {
	ctx := context.Background()

	// Create mock executor
	executor := NewMockCommandExecutor()

	// Configure command behaviors
	executor.SetCommand("systemctl", "active", nil)
	executor.SetCommand("docker", "Docker version 20.10.0", nil)
	executor.SetDefaultBehavior("", fmt.Errorf("command not found"))

	// Execute commands
	output1, err1 := executor.Execute(ctx, "systemctl", "is-active", "kubelet")
	AssertNoError(t, err1, "systemctl should succeed")
	AssertEqual(t, "active", output1, "systemctl output should be 'active'")

	output2, err2 := executor.Execute(ctx, "docker", "version")
	AssertNoError(t, err2, "docker should succeed")
	AssertEqual(t, "Docker version 20.10.0", output2, "docker output should match")

	// Unknown command should use default behavior
	_, err3 := executor.Execute(ctx, "unknown-command")
	AssertError(t, err3, "unknown command should fail")

	// Verify commands were tracked
	commands := executor.GetExecutedCommands()
	AssertEqual(t, 3, len(commands), "should have 3 executed commands")
	AssertTrue(t, executor.WasCommandExecuted("systemctl"), "systemctl should be executed")
	AssertTrue(t, executor.WasCommandExecuted("docker"), "docker should be executed")
}

func TestTestContext_Example(t *testing.T) {
	// Create a test context with timeout
	ctx, cancel := TestContext(t, 2*time.Second)
	defer cancel()

	// Use the context in operations
	select {
	case <-ctx.Done():
		t.Fatal("context should not be cancelled yet")
	case <-time.After(100 * time.Millisecond):
		// Context is still valid
	}
}

func TestAssertionHelpers_Example(t *testing.T) {
	// Test assertion helpers
	AssertNoError(t, nil, "nil error should pass")
	AssertEqual(t, 42, 42, "equal values should pass")
	AssertTrue(t, true, "true condition should pass")
	AssertFalse(t, false, "false condition should pass")
}

func TestEventually_Example(t *testing.T) {
	counter := 0

	// Eventually will retry until condition is true
	Eventually(t, func() bool {
		counter++
		return counter >= 3
	}, 1*time.Second, 10*time.Millisecond, "counter should reach 3")

	AssertTrue(t, counter >= 3, "counter should be at least 3")
}

func TestLoadTestData_Example(t *testing.T) {
	// Load test data files (this will fail if files don't exist, which is expected)
	// In real usage, ensure files exist in testdata/

	// Example of how to use it:
	// data, err := LoadTestDataFile("configs/detector-config.yaml")
	// AssertNoError(t, err, "should load config file")
	// AssertTrue(t, len(data) > 0, "config file should not be empty")
}

func TestCommonFixtures_Example(t *testing.T) {
	// Use predefined problem fixtures
	kubeletProblem := KubeletFailedProblem()
	AssertEqual(t, "systemd-service-failed", kubeletProblem.Type)
	AssertEqual(t, types.ProblemCritical, kubeletProblem.Severity)

	memoryProblem := MemoryPressureProblem()
	AssertEqual(t, "node-memory-pressure", memoryProblem.Type)
	AssertEqual(t, types.ProblemWarning, memoryProblem.Severity)

	diskProblem := DiskPressureProblem()
	AssertEqual(t, "node-disk-pressure", diskProblem.Type)

	// Use predefined status fixtures
	healthyStatus := HealthyKubeletStatus()
	AssertEqual(t, "kubelet-monitor", healthyStatus.Source)
	AssertEqual(t, 1, len(healthyStatus.Events))
	AssertEqual(t, 1, len(healthyStatus.Conditions))

	unhealthyStatus := UnhealthyKubeletStatus()
	AssertEqual(t, "kubelet-monitor", unhealthyStatus.Source)
	AssertEqual(t, 1, len(unhealthyStatus.Events))
	AssertEqual(t, 1, len(unhealthyStatus.Conditions))

	multiStatus := MultiProblemStatus()
	AssertEqual(t, "node-monitor", multiStatus.Source)
	AssertEqual(t, 3, len(multiStatus.Events))
	AssertEqual(t, 3, len(multiStatus.Conditions))
}
