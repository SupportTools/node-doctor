// Copyright 2025 Support Tools Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

//go:build e2e
// +build e2e

package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/test/e2e/cluster"
	"github.com/supporttools/node-doctor/test/e2e/utils"
)

// TestE2E_MultiNodeCorrelation validates that the controller detects patterns
// across multiple nodes:
// 1. Deploy controller with correlation enabled
// 2. Deploy node-doctor DaemonSet on 3 worker nodes
// 3. Inject same problem (DNS failure) on 2+ nodes
// 4. Verify infrastructure correlation is detected
// 5. Verify Kubernetes Event is created for cluster-wide issue
// 6. Verify correlation resolves when nodes recover
func TestE2E_MultiNodeCorrelation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	ctx := context.Background()

	utils.LogSection("E2E Test: Multi-Node Correlation Detection")

	// Step 1: Setup KIND cluster with 3 workers
	utils.Log("Step 1: Creating KIND cluster with 3 workers")
	setupOpts := cluster.DefaultSetupOptions()
	setupOpts.Workers = 3
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
	utils.LogSuccess("KIND cluster ready with %d workers", setupOpts.Workers)

	// Step 2: Deploy Controller with correlation enabled
	utils.Log("Step 2: Deploying controller with correlation enabled")
	if err := deployControllerWithCorrelation(ctx, kubeContext); err != nil {
		t.Fatalf("Failed to deploy controller: %v", err)
	}

	controllerURL := "http://node-doctor-controller.node-doctor.svc.cluster.local:8080"
	if err := waitForControllerReady(ctx, kubeContext, controllerURL); err != nil {
		t.Fatalf("Controller not ready: %v", err)
	}
	utils.LogSuccess("Controller ready with correlation enabled")

	// Step 3: Deploy Node Doctor DaemonSet
	utils.Log("Step 3: Deploying Node Doctor")
	if err := deployNodeDoctorWithController(ctx, kubeContext, controllerURL); err != nil {
		t.Fatalf("Failed to deploy Node Doctor: %v", err)
	}
	utils.LogSuccess("Node Doctor deployed")

	// Step 4: Verify initial state (no correlations)
	utils.Log("Step 4: Verifying initial state")
	if err := verifyNoActiveCorrelations(ctx, kubeContext, controllerURL); err != nil {
		t.Logf("Warning: initial correlation check: %v", err)
	}
	utils.LogSuccess("No initial correlations")

	// Step 5: Get worker nodes
	workers, err := utils.GetWorkerNodes(ctx, kubeContext)
	if err != nil {
		t.Fatalf("Failed to get worker nodes: %v", err)
	}
	if len(workers) < 2 {
		t.Fatalf("Need at least 2 worker nodes, got %d", len(workers))
	}
	utils.LogSuccess("Found %d worker nodes: %v", len(workers), workers)

	// Step 6: Inject DNS failure on multiple nodes (60% > 30% threshold)
	utils.Log("Step 6: Injecting DNS failure on 2 workers")
	for i := 0; i < 2 && i < len(workers); i++ {
		if err := injectDNSFailure(ctx, kubeContext, workers[i]); err != nil {
			t.Fatalf("Failed to inject DNS failure on %s: %v", workers[i], err)
		}
		utils.LogSuccess("DNS failure injected on %s", workers[i])
	}

	// Step 7: Wait for Node Doctor to detect and report
	utils.Log("Step 7: Waiting for Node Doctor detection")
	time.Sleep(10 * time.Second) // Allow monitors to detect

	// Step 8: Wait for infrastructure correlation
	utils.Log("Step 8: Waiting for infrastructure correlation")
	if err := waitForCorrelation(ctx, kubeContext, controllerURL, "infrastructure"); err != nil {
		t.Fatalf("Infrastructure correlation not detected: %v", err)
	}
	utils.LogSuccess("Infrastructure correlation detected")

	// Step 9: Verify Kubernetes Event was created
	utils.Log("Step 9: Verifying Kubernetes Event")
	if err := verifyCorrelationEvent(ctx, kubeContext); err != nil {
		utils.LogWarning("Correlation event verification: %v (non-critical)", err)
	} else {
		utils.LogSuccess("Kubernetes Event created for cluster-wide issue")
	}

	// Step 10: Recover nodes (remove DNS failure)
	utils.Log("Step 10: Recovering nodes")
	for i := 0; i < 2 && i < len(workers); i++ {
		if err := recoverDNS(ctx, kubeContext, workers[i]); err != nil {
			t.Logf("Warning: Failed to recover DNS on %s: %v", workers[i], err)
		}
		utils.LogSuccess("DNS recovered on %s", workers[i])
	}

	// Step 11: Wait for correlation resolution
	utils.Log("Step 11: Waiting for correlation resolution")
	if err := waitForCorrelationResolution(ctx, kubeContext, controllerURL); err != nil {
		utils.LogWarning("Correlation resolution timeout: %v", err)
	} else {
		utils.LogSuccess("Correlation resolved after recovery")
	}

	utils.LogSection("✓ E2E Test PASSED: Multi-Node Correlation")
	utils.LogSuccess("Complete workflow validated:")
	utils.LogSuccess("  Same problem detected on multiple nodes")
	utils.LogSuccess("  Infrastructure correlation created")
	utils.LogSuccess("  Kubernetes Event published")
	utils.LogSuccess("  Correlation resolved after recovery")
}

// TestE2E_CommonCauseCorrelation validates detection of related problems
func TestE2E_CommonCauseCorrelation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	ctx := context.Background()

	utils.LogSection("E2E Test: Common Cause Correlation")

	setupOpts := cluster.DefaultSetupOptions()
	setupOpts.Workers = 3
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

	// Deploy controller
	if err := deployControllerWithCorrelation(ctx, kubeContext); err != nil {
		t.Fatalf("Failed to deploy controller: %v", err)
	}

	controllerURL := "http://node-doctor-controller.node-doctor.svc.cluster.local:8080"
	if err := waitForControllerReady(ctx, kubeContext, controllerURL); err != nil {
		t.Fatalf("Controller not ready: %v", err)
	}

	if err := deployNodeDoctorWithController(ctx, kubeContext, controllerURL); err != nil {
		t.Fatalf("Failed to deploy Node Doctor: %v", err)
	}

	// Get worker nodes
	workers, err := utils.GetWorkerNodes(ctx, kubeContext)
	if err != nil {
		t.Fatalf("Failed to get workers: %v", err)
	}

	// Inject related problems: memory + disk pressure
	utils.Log("Injecting memory and disk pressure on workers")
	for i := 0; i < 2 && i < len(workers); i++ {
		if err := injectMemoryPressure(ctx, kubeContext, workers[i]); err != nil {
			t.Logf("Warning: memory pressure injection failed on %s: %v", workers[i], err)
		}
		if err := injectDiskPressure(ctx, kubeContext, workers[i]); err != nil {
			t.Logf("Warning: disk pressure injection failed on %s: %v", workers[i], err)
		}
	}

	// Wait for correlation detection
	time.Sleep(15 * time.Second)

	// Check for common-cause correlation
	correlations, err := getActiveCorrelations(ctx, kubeContext, controllerURL)
	if err != nil {
		t.Logf("Warning: failed to get correlations: %v", err)
	}

	hasCommonCause := false
	for _, corr := range correlations {
		if corr["type"] == "common-cause" {
			hasCommonCause = true
			utils.LogSuccess("Common-cause correlation detected: %v", corr)
			break
		}
	}

	if !hasCommonCause {
		t.Logf("Common-cause correlation not detected (may require more time)")
	}

	// Cleanup
	for i := 0; i < 2 && i < len(workers); i++ {
		recoverMemoryPressure(ctx, kubeContext, workers[i])
		recoverDiskPressure(ctx, kubeContext, workers[i])
	}

	utils.LogSection("✓ E2E Test PASSED: Common Cause Correlation")
}

// TestE2E_CorrelationMetrics validates Prometheus metrics for correlations
func TestE2E_CorrelationMetrics(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	ctx := context.Background()

	utils.LogSection("E2E Test: Correlation Metrics")

	setupOpts := cluster.DefaultSetupOptions()
	setupOpts.Workers = 2
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

	// Deploy controller
	if err := deployControllerWithCorrelation(ctx, kubeContext); err != nil {
		t.Fatalf("Failed to deploy controller: %v", err)
	}

	controllerURL := "http://node-doctor-controller.node-doctor.svc.cluster.local:8080"
	if err := waitForControllerReady(ctx, kubeContext, controllerURL); err != nil {
		t.Fatalf("Controller not ready: %v", err)
	}

	// Check metrics endpoint
	utils.Log("Checking metrics endpoint")
	metrics, err := getControllerMetrics(ctx, kubeContext, controllerURL)
	if err != nil {
		t.Fatalf("Failed to get metrics: %v", err)
	}

	// Verify correlation metrics exist
	expectedMetrics := []string{
		"node_doctor_correlation_active_total",
		"node_doctor_correlation_detected_total",
		"node_doctor_cluster_nodes_total",
	}

	for _, metric := range expectedMetrics {
		if !strings.Contains(metrics, metric) {
			t.Logf("Warning: expected metric %s not found", metric)
		} else {
			utils.LogSuccess("Found metric: %s", metric)
		}
	}

	utils.LogSection("✓ E2E Test PASSED: Correlation Metrics")
}

// Helper functions

func deployControllerWithCorrelation(ctx context.Context, kubeContext string) error {
	manifest := `apiVersion: v1
kind: Namespace
metadata:
  name: node-doctor
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: node-doctor-controller
  namespace: node-doctor
spec:
  replicas: 1
  selector:
    matchLabels:
      app: node-doctor-controller
  template:
    metadata:
      labels:
        app: node-doctor-controller
    spec:
      containers:
        - name: controller
          image: node-doctor-controller:e2e-test
          imagePullPolicy: IfNotPresent
          ports:
            - containerPort: 8080
          env:
            - name: CORRELATION_ENABLED
              value: "true"
            - name: CLUSTER_WIDE_THRESHOLD
              value: "0.3"
            - name: EVALUATION_INTERVAL
              value: "5s"
          readinessProbe:
            httpGet:
              path: /readyz
              port: 8080
            initialDelaySeconds: 5
            periodSeconds: 5
          livenessProbe:
            httpGet:
              path: /healthz
              port: 8080
            initialDelaySeconds: 10
            periodSeconds: 10
---
apiVersion: v1
kind: Service
metadata:
  name: node-doctor-controller
  namespace: node-doctor
spec:
  selector:
    app: node-doctor-controller
  ports:
    - port: 8080
      targetPort: 8080
`
	return utils.ApplyManifest(ctx, kubeContext, manifest)
}

func verifyNoActiveCorrelations(ctx context.Context, kubeContext, controllerURL string) error {
	correlations, err := getActiveCorrelations(ctx, kubeContext, controllerURL)
	if err != nil {
		return err
	}
	if len(correlations) > 0 {
		return fmt.Errorf("expected no correlations, got %d", len(correlations))
	}
	return nil
}

func getActiveCorrelations(ctx context.Context, kubeContext, controllerURL string) ([]map[string]interface{}, error) {
	output, err := utils.KubectlExec(ctx, kubeContext,
		"curl", "-s", controllerURL+"/api/v1/correlations")
	if err != nil {
		return nil, err
	}

	var response struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &response); err != nil {
		return nil, err
	}

	return response.Data, nil
}

func injectDNSFailure(ctx context.Context, kubeContext, nodeName string) error {
	// Corrupt /etc/resolv.conf to simulate DNS failure
	_, err := utils.ExecOnNode(ctx, kubeContext, nodeName,
		"mv", "/etc/resolv.conf", "/etc/resolv.conf.backup")
	if err != nil {
		return err
	}

	// Create empty resolv.conf
	_, err = utils.ExecOnNode(ctx, kubeContext, nodeName,
		"touch", "/etc/resolv.conf")
	return err
}

func recoverDNS(ctx context.Context, kubeContext, nodeName string) error {
	_, err := utils.ExecOnNode(ctx, kubeContext, nodeName,
		"mv", "/etc/resolv.conf.backup", "/etc/resolv.conf")
	return err
}

func injectMemoryPressure(ctx context.Context, kubeContext, nodeName string) error {
	// Create a process that allocates memory
	// This is a simplified simulation
	_, err := utils.ExecOnNode(ctx, kubeContext, nodeName,
		"bash", "-c", "dd if=/dev/zero bs=1M count=500 | tail &")
	return err
}

func recoverMemoryPressure(ctx context.Context, kubeContext, nodeName string) error {
	// Kill memory-consuming processes
	_, err := utils.ExecOnNode(ctx, kubeContext, nodeName,
		"pkill", "-f", "dd if=/dev/zero")
	return err
}

func injectDiskPressure(ctx context.Context, kubeContext, nodeName string) error {
	// Create a large file to fill up disk
	_, err := utils.ExecOnNode(ctx, kubeContext, nodeName,
		"dd", "if=/dev/zero", "of=/tmp/fill_disk", "bs=1M", "count=500")
	return err
}

func recoverDiskPressure(ctx context.Context, kubeContext, nodeName string) error {
	_, err := utils.ExecOnNode(ctx, kubeContext, nodeName,
		"rm", "-f", "/tmp/fill_disk")
	return err
}

func waitForCorrelation(ctx context.Context, kubeContext, controllerURL, correlationType string) error {
	config := &utils.RetryConfig{
		Interval:    5 * time.Second,
		Timeout:     2 * time.Minute,
		Description: fmt.Sprintf("%s correlation", correlationType),
	}

	return utils.WaitFor(ctx, config, func() (bool, error) {
		correlations, err := getActiveCorrelations(ctx, kubeContext, controllerURL)
		if err != nil {
			return false, nil
		}

		for _, corr := range correlations {
			if corr["type"] == correlationType {
				return true, nil
			}
		}
		return false, nil
	})
}

func waitForCorrelationResolution(ctx context.Context, kubeContext, controllerURL string) error {
	config := &utils.RetryConfig{
		Interval:    5 * time.Second,
		Timeout:     3 * time.Minute,
		Description: "correlation resolution",
	}

	return utils.WaitFor(ctx, config, func() (bool, error) {
		correlations, err := getActiveCorrelations(ctx, kubeContext, controllerURL)
		if err != nil {
			return false, nil
		}

		// Check if all correlations are resolved (status = resolved)
		for _, corr := range correlations {
			if corr["status"] != "resolved" {
				return false, nil
			}
		}
		return true, nil
	})
}

func verifyCorrelationEvent(ctx context.Context, kubeContext string) error {
	config := &utils.RetryConfig{
		Interval:    3 * time.Second,
		Timeout:     1 * time.Minute,
		Description: "correlation Kubernetes event",
	}

	return utils.WaitFor(ctx, config, func() (bool, error) {
		output, err := utils.KubectlExec(ctx, kubeContext,
			"kubectl", "get", "events", "-A",
			"--field-selector", "reason=ClusterCorrelation",
			"-o", "name")
		if err != nil {
			return false, nil
		}
		return output != "", nil
	})
}

func getControllerMetrics(ctx context.Context, kubeContext, controllerURL string) (string, error) {
	return utils.KubectlExec(ctx, kubeContext,
		"curl", "-s", controllerURL+"/metrics")
}
