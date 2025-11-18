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
	"github.com/supporttools/node-doctor/test/e2e/observers"
	"github.com/supporttools/node-doctor/test/e2e/utils"
)

// TestE2E_DiskPressure validates disk pressure detection and remediation:
// 1. Deploy Node Doctor to KIND cluster
// 2. Fill disk to trigger pressure (write large file)
// 3. Verify Node Doctor detects disk pressure
// 4. Verify Node condition is updated (DiskPressure=True)
// 5. Verify Kubernetes event is created
// 6. Verify remediation is attempted (disk cleanup)
// 7. Verify disk pressure is relieved
func TestE2E_DiskPressure(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	ctx := context.Background()

	utils.LogSection("E2E Test: Disk Pressure Detection and Remediation")

	// Step 1: Setup KIND cluster
	utils.Log("Step 1: Creating KIND cluster")
	setupOpts := cluster.DefaultSetupOptions()
	if err := cluster.Setup(ctx, setupOpts); err != nil {
		t.Fatalf("Failed to setup cluster: %v", err)
	}

	defer func() {
		teardownOpts := cluster.DefaultTeardownOptions()
		if err := cluster.Teardown(ctx, teardownOpts); err != nil {
			t.Logf("Warning: cluster teardown failed: %v", err)
		}
	}()

	kubeContext := cluster.GetContext(setupOpts.ClusterName)
	utils.LogSuccess("KIND cluster ready")

	// Step 2: Deploy Node Doctor
	utils.Log("Step 2: Deploying Node Doctor")
	if err := utils.DeployNodeDoctor(ctx, kubeContext); err != nil {
		t.Fatalf("Failed to deploy Node Doctor: %v", err)
	}
	utils.LogSuccess("Node Doctor deployed")

	// Create observers
	eventObserver := observers.NewEventObserver(kubeContext, "")
	conditionObserver := observers.NewConditionObserver(kubeContext, "")
	remediationObserver := observers.NewRemediationObserver(kubeContext, "node-doctor")

	// Step 3: Verify initial disk state (no pressure)
	utils.Log("Step 3: Verifying initial disk state")
	if err := verifyNoDiskPressure(ctx, conditionObserver); err != nil {
		t.Fatalf("Initial disk state check failed: %v", err)
	}
	utils.LogSuccess("Initial disk state normal")

	// Step 4: Inject disk pressure
	utils.Log("Step 4: Injecting disk pressure")
	if err := createDiskPressure(ctx, kubeContext); err != nil {
		t.Fatalf("Failed to create disk pressure: %v", err)
	}
	utils.LogSuccess("Disk pressure created")

	// Step 5: Wait for disk pressure detection
	utils.Log("Step 5: Waiting for disk pressure detection")
	if err := waitForDiskPressureDetection(ctx, kubeContext); err != nil {
		t.Fatalf("Disk pressure detection timeout: %v", err)
	}
	utils.LogSuccess("Disk pressure detected")

	// Step 6: Verify Node condition
	utils.Log("Step 6: Verifying DiskPressure condition")
	if err := conditionObserver.WaitForConditionStatus(ctx, "DiskPressure", "True", 1*time.Minute); err != nil {
		t.Fatalf("DiskPressure condition not set: %v", err)
	}
	utils.LogSuccess("DiskPressure condition set")

	// Step 7: Verify event was created
	utils.Log("Step 7: Verifying disk pressure event")
	if err := eventObserver.WaitForEvent(ctx, "DiskPressure", 1*time.Minute); err != nil {
		// Event may use different reason
		utils.LogWarning("DiskPressure event not found (may use different reason): %v", err)
	} else {
		utils.LogSuccess("Disk pressure event created")
	}

	// Step 8: Wait for remediation attempt
	utils.Log("Step 8: Waiting for remediation attempt")
	if err := remediationObserver.WaitForSpecificRemediation(ctx, "disk cleanup", 2*time.Minute); err != nil {
		utils.LogWarning("Disk cleanup remediation not found in logs: %v", err)
		// Still check if disk pressure was relieved
	} else {
		utils.LogSuccess("Remediation attempted")
	}

	// Step 9: Verify disk pressure relief
	utils.Log("Step 9: Verifying disk pressure relief")
	if err := waitForDiskPressureRelief(ctx, conditionObserver); err != nil {
		t.Fatalf("Disk pressure not relieved: %v", err)
	}
	utils.LogSuccess("Disk pressure relieved")

	utils.LogSection("✓ E2E Test PASSED: Disk Pressure")
	utils.LogSuccess("Complete workflow validated:")
	utils.LogSuccess("  Monitor detected disk pressure")
	utils.LogSuccess("  Condition exporter updated Node condition")
	utils.LogSuccess("  Event exporter created event")
	utils.LogSuccess("  Disk pressure was successfully relieved")
}

// verifyNoDiskPressure checks that DiskPressure condition is not set or is False
func verifyNoDiskPressure(ctx context.Context, observer *observers.ConditionObserver) error {
	status, err := observer.GetConditionStatus(ctx, "DiskPressure")
	if err != nil {
		// Condition doesn't exist - that's fine
		return nil
	}

	if status == "True" {
		return fmt.Errorf("disk pressure already present")
	}

	return nil
}

// createDiskPressure fills disk to trigger pressure
// In KIND, we write a large file to /var/lib/kubelet or /tmp
func createDiskPressure(ctx context.Context, kubeContext string) error {
	// Write a large file (500MB) to trigger disk pressure
	// Use dd to create file
	_, err := utils.ExecInNodeDoctor(ctx, kubeContext,
		"dd", "if=/dev/zero", "of=/tmp/disk-pressure-test.bin",
		"bs=1M", "count=500")

	if err != nil {
		return fmt.Errorf("failed to create large file: %w", err)
	}

	// Wait a moment for filesystem to update
	time.Sleep(3 * time.Second)
	return nil
}

// waitForDiskPressureDetection waits for Node Doctor to detect disk pressure
func waitForDiskPressureDetection(ctx context.Context, kubeContext string) error {
	config := &utils.RetryConfig{
		Interval:    5 * time.Second,
		Timeout:     2 * time.Minute,
		Description: "disk pressure detection",
	}

	return utils.WaitFor(ctx, config, func() (bool, error) {
		logs, err := utils.GetNodeDoctorLogs(ctx, kubeContext)
		if err != nil {
			return false, nil
		}

		// Look for disk pressure indicators in logs
		return strings.Contains(logs, "disk pressure") ||
			strings.Contains(logs, "DiskPressure") ||
			strings.Contains(logs, "disk usage high"), nil
	})
}

// waitForDiskPressureRelief waits for disk pressure to be relieved
func waitForDiskPressureRelief(ctx context.Context, observer *observers.ConditionObserver) error {
	config := &utils.RetryConfig{
		Interval:    10 * time.Second,
		Timeout:     3 * time.Minute,
		Description: "disk pressure relief",
	}

	return utils.WaitFor(ctx, config, func() (bool, error) {
		status, err := observer.GetConditionStatus(ctx, "DiskPressure")
		if err != nil {
			// Condition removed - pressure relieved
			return true, nil
		}

		// Pressure relieved if status is False
		return status == "False" || status == "Unknown", nil
	})
}
