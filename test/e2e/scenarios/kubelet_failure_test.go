// Copyright 2025 Support Tools Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

//go:build e2e
// +build e2e

package scenarios

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/test/e2e/cluster"
	"github.com/supporttools/node-doctor/test/e2e/utils"
)

// TestE2E_KubeletFailure validates the complete workflow when kubelet fails:
// 1. Deploy Node Doctor to KIND cluster
// 2. Stop kubelet service (simulate failure)
// 3. Verify Node Doctor detects kubelet failure
// 4. Verify Kubernetes event is created
// 5. Verify Node condition is updated
// 6. Verify remediation is attempted
// 7. Verify kubelet is restarted (remediation success)
func TestE2E_KubeletFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	ctx := context.Background()

	utils.LogSection("E2E Test: Kubelet Failure Detection and Remediation")

	// Step 1: Setup KIND cluster
	utils.Log("Step 1: Creating KIND cluster")
	setupOpts := cluster.DefaultSetupOptions()
	if err := cluster.Setup(ctx, setupOpts); err != nil {
		t.Fatalf("Failed to setup cluster: %v", err)
	}

	// Ensure cleanup
	defer func() {
		teardownOpts := cluster.DefaultTeardownOptions()
		if err := cluster.Teardown(ctx, teardownOpts); err != nil {
			t.Logf("Warning: cluster teardown failed: %v", err)
		}
	}()

	kubeContext := cluster.GetContext(setupOpts.ClusterName)
	utils.LogSuccess("KIND cluster ready: %s", setupOpts.ClusterName)

	// Step 2: Deploy Node Doctor DaemonSet
	utils.Log("Step 2: Deploying Node Doctor")
	if err := utils.DeployNodeDoctor(ctx, kubeContext); err != nil {
		t.Fatalf("Failed to deploy Node Doctor: %v", err)
	}
	utils.LogSuccess("Node Doctor deployed")

	// Step 3: Verify initial healthy state
	utils.Log("Step 3: Verifying initial healthy state")
	if err := verifyKubeletHealthy(ctx, kubeContext); err != nil {
		t.Fatalf("Initial kubelet health check failed: %v", err)
	}
	utils.LogSuccess("Kubelet initially healthy")

	// Step 4: Inject kubelet failure (stop kubelet)
	utils.Log("Step 4: Injecting kubelet failure (stopping kubelet)")
	if err := stopKubelet(ctx, kubeContext); err != nil {
		t.Fatalf("Failed to stop kubelet: %v", err)
	}
	utils.LogSuccess("Kubelet stopped")

	// Step 5: Wait for Node Doctor to detect failure
	utils.Log("Step 5: Waiting for failure detection")
	if err := waitForKubeletFailureDetection(ctx, kubeContext); err != nil {
		t.Fatalf("Failure detection timeout: %v", err)
	}
	utils.LogSuccess("Kubelet failure detected")

	// Step 6: Verify Kubernetes event was created
	utils.Log("Step 6: Verifying Kubernetes event")
	if err := verifyKubeletFailureEvent(ctx, kubeContext); err != nil {
		t.Fatalf("Event verification failed: %v", err)
	}
	utils.LogSuccess("Kubernetes event created")

	// Step 7: Verify Node condition was updated
	utils.Log("Step 7: Verifying Node condition")
	if err := verifyNodeCondition(ctx, kubeContext, "KubeletUnhealthy"); err != nil {
		t.Fatalf("Node condition verification failed: %v", err)
	}
	utils.LogSuccess("Node condition updated")

	// Step 8: Wait for remediation attempt
	utils.Log("Step 8: Waiting for remediation attempt")
	if err := waitForRemediationAttempt(ctx, kubeContext); err != nil {
		t.Fatalf("Remediation not attempted: %v", err)
	}
	utils.LogSuccess("Remediation attempted")

	// Step 9: Verify kubelet was restarted
	utils.Log("Step 9: Verifying kubelet restart")
	if err := verifyKubeletHealthy(ctx, kubeContext); err != nil {
		t.Fatalf("Kubelet restart verification failed: %v", err)
	}
	utils.LogSuccess("Kubelet restarted successfully")

	// Step 10: Verify recovery event
	utils.Log("Step 10: Verifying recovery event")
	if err := verifyKubeletRecoveryEvent(ctx, kubeContext); err != nil {
		// Recovery event is nice-to-have, not required
		utils.LogWarning("Recovery event not found (non-critical): %v", err)
	} else {
		utils.LogSuccess("Recovery event created")
	}

	utils.LogSection("✓ E2E Test PASSED: Kubelet Failure")
	utils.LogSuccess("Complete workflow validated:")
	utils.LogSuccess("  Monitor detected kubelet failure")
	utils.LogSuccess("  Event exporter created Kubernetes event")
	utils.LogSuccess("  Condition exporter updated Node condition")
	utils.LogSuccess("  Remediator successfully restarted kubelet")
}

// verifyKubeletHealthy checks if kubelet is healthy
func verifyKubeletHealthy(ctx context.Context, kubeContext string) error {
	config := &utils.RetryConfig{
		Interval:    5 * time.Second,
		Timeout:     1 * time.Minute,
		Description: "kubelet to be healthy",
	}

	return utils.WaitFor(ctx, config, func() (bool, error) {
		// Check if kubelet is running via systemctl
		output, err := utils.ExecInNodeDoctor(ctx, kubeContext,
			"systemctl", "is-active", "kubelet")
		if err != nil {
			return false, nil // Not healthy yet
		}

		return output == "active\n", nil
	})
}

// stopKubelet stops the kubelet service to simulate failure
func stopKubelet(ctx context.Context, kubeContext string) error {
	_, err := utils.ExecInNodeDoctor(ctx, kubeContext,
		"systemctl", "stop", "kubelet")
	if err != nil {
		return err
	}

	// Give it a moment to actually stop
	time.Sleep(2 * time.Second)
	return nil
}

// waitForKubeletFailureDetection waits for Node Doctor to detect kubelet failure
func waitForKubeletFailureDetection(ctx context.Context, kubeContext string) error {
	config := &utils.RetryConfig{
		Interval:    5 * time.Second,
		Timeout:     2 * time.Minute,
		Description: "kubelet failure detection",
	}

	return utils.WaitFor(ctx, config, func() (bool, error) {
		// Check Node Doctor logs for failure detection
		logs, err := utils.GetNodeDoctorLogs(ctx, kubeContext)
		if err != nil {
			return false, nil
		}

		// Look for kubelet failure indication in logs
		// The exact log message depends on the monitor implementation
		return containsAny(logs,
			"kubelet health check failed",
			"kubelet is not healthy",
			"KubeletUnhealthy"), nil
	})
}

// verifyKubeletFailureEvent verifies that a Kubernetes event was created
func verifyKubeletFailureEvent(ctx context.Context, kubeContext string) error {
	config := &utils.RetryConfig{
		Interval:    3 * time.Second,
		Timeout:     1 * time.Minute,
		Description: "kubelet failure event",
	}

	return utils.WaitFor(ctx, config, func() (bool, error) {
		output, err := utils.ExecInNodeDoctor(ctx, kubeContext,
			"kubectl", "get", "events",
			"--field-selector", "reason=KubeletUnhealthy",
			"-o", "name")
		if err != nil {
			return false, nil
		}

		// Event exists if output is not empty
		return output != "", nil
	})
}

// verifyNodeCondition verifies that Node condition was updated
func verifyNodeCondition(ctx context.Context, kubeContext string, conditionType string) error {
	config := &utils.RetryConfig{
		Interval:    3 * time.Second,
		Timeout:     1 * time.Minute,
		Description: fmt.Sprintf("Node condition %s", conditionType),
	}

	return utils.WaitFor(ctx, config, func() (bool, error) {
		// Get node conditions
		output, err := utils.ExecInNodeDoctor(ctx, kubeContext,
			"kubectl", "get", "node",
			"-o", "jsonpath={.items[0].status.conditions[?(@.type=='" + conditionType + "')].status}")
		if err != nil {
			return false, nil
		}

		// Condition exists if we got output
		return output != "", nil
	})
}

// waitForRemediationAttempt waits for Node Doctor to attempt remediation
func waitForRemediationAttempt(ctx context.Context, kubeContext string) error {
	config := &utils.RetryConfig{
		Interval:    5 * time.Second,
		Timeout:     2 * time.Minute,
		Description: "remediation attempt",
	}

	return utils.WaitFor(ctx, config, func() (bool, error) {
		logs, err := utils.GetNodeDoctorLogs(ctx, kubeContext)
		if err != nil {
			return false, nil
		}

		// Look for remediation attempt in logs
		return containsAny(logs,
			"attempting remediation",
			"restarting kubelet",
			"remediation executed"), nil
	})
}

// verifyKubeletRecoveryEvent verifies that a recovery event was created
func verifyKubeletRecoveryEvent(ctx context.Context, kubeContext string) error {
	config := &utils.RetryConfig{
		Interval:    3 * time.Second,
		Timeout:     30 * time.Second,
		Description: "kubelet recovery event",
	}

	return utils.WaitFor(ctx, config, func() (bool, error) {
		output, err := utils.ExecInNodeDoctor(ctx, kubeContext,
			"kubectl", "get", "events",
			"--field-selector", "reason=KubeletHealthy",
			"-o", "name")
		if err != nil {
			return false, nil
		}

		return output != "", nil
	})
}

// containsAny checks if a string contains any of the given substrings
func containsAny(s string, substrs ...string) bool {
	for _, substr := range substrs {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}
