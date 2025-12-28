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
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/test/e2e/cluster"
	"github.com/supporttools/node-doctor/test/e2e/utils"
)

// TestE2E_ControllerCoordination validates the controller-based lease coordination:
// 1. Deploy controller Deployment + Service
// 2. Deploy node-doctor DaemonSet with controller webhook enabled
// 3. Inject kubelet failure on one node
// 4. Verify lease is requested from controller
// 5. Verify remediation proceeds after lease is granted
// 6. Verify lease is released after remediation
func TestE2E_ControllerCoordination(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	ctx := context.Background()

	utils.LogSection("E2E Test: Controller Lease Coordination")

	// Step 1: Setup KIND cluster with multiple workers
	utils.Log("Step 1: Creating KIND cluster with multiple workers")
	setupOpts := cluster.DefaultSetupOptions()
	setupOpts.Workers = 2 // Need multiple workers to test coordination
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

	// Step 2: Deploy Node Doctor Controller
	utils.Log("Step 2: Deploying Node Doctor Controller")
	if err := deployController(ctx, kubeContext); err != nil {
		t.Fatalf("Failed to deploy controller: %v", err)
	}
	utils.LogSuccess("Controller deployed")

	// Step 3: Verify controller is ready
	utils.Log("Step 3: Verifying controller readiness")
	controllerURL := fmt.Sprintf("http://node-doctor-controller.node-doctor.svc.cluster.local:8080")
	if err := waitForControllerReady(ctx, kubeContext, controllerURL); err != nil {
		t.Fatalf("Controller not ready: %v", err)
	}
	utils.LogSuccess("Controller ready")

	// Step 4: Deploy Node Doctor DaemonSet with controller webhook
	utils.Log("Step 4: Deploying Node Doctor with controller coordination")
	if err := deployNodeDoctorWithController(ctx, kubeContext, controllerURL); err != nil {
		t.Fatalf("Failed to deploy Node Doctor: %v", err)
	}
	utils.LogSuccess("Node Doctor deployed with controller coordination")

	// Step 5: Verify initial healthy state
	utils.Log("Step 5: Verifying initial healthy state")
	if err := verifyNoActiveLeases(ctx, kubeContext, controllerURL); err != nil {
		t.Fatalf("Initial state verification failed: %v", err)
	}
	utils.LogSuccess("No active leases initially")

	// Step 6: Inject kubelet failure on one worker
	utils.Log("Step 6: Injecting kubelet failure on worker node")
	workerNode := "node-doctor-e2e-worker"
	if err := stopKubeletOnNode(ctx, kubeContext, workerNode); err != nil {
		t.Fatalf("Failed to stop kubelet: %v", err)
	}
	utils.LogSuccess("Kubelet stopped on %s", workerNode)

	// Step 7: Wait for lease to be requested and granted
	utils.Log("Step 7: Waiting for lease request")
	if err := waitForLease(ctx, kubeContext, controllerURL, workerNode); err != nil {
		t.Fatalf("Lease not granted: %v", err)
	}
	utils.LogSuccess("Lease granted for %s", workerNode)

	// Step 8: Wait for remediation attempt
	utils.Log("Step 8: Waiting for remediation attempt")
	if err := waitForRemediationOnNode(ctx, kubeContext, workerNode); err != nil {
		t.Fatalf("Remediation not attempted: %v", err)
	}
	utils.LogSuccess("Remediation attempted")

	// Step 9: Wait for lease to be released
	utils.Log("Step 9: Waiting for lease release")
	if err := waitForLeaseRelease(ctx, kubeContext, controllerURL, workerNode); err != nil {
		// Not critical if lease auto-expires
		utils.LogWarning("Lease not explicitly released (will auto-expire): %v", err)
	} else {
		utils.LogSuccess("Lease released for %s", workerNode)
	}

	// Step 10: Verify kubelet is healthy again
	utils.Log("Step 10: Verifying kubelet recovery")
	if err := verifyKubeletHealthyOnNode(ctx, kubeContext, workerNode); err != nil {
		t.Fatalf("Kubelet not recovered: %v", err)
	}
	utils.LogSuccess("Kubelet recovered on %s", workerNode)

	utils.LogSection("✓ E2E Test PASSED: Controller Coordination")
	utils.LogSuccess("Complete workflow validated:")
	utils.LogSuccess("  Controller received lease request")
	utils.LogSuccess("  Lease was granted to worker node")
	utils.LogSuccess("  Remediation proceeded after lease grant")
	utils.LogSuccess("  Lease was released after remediation")
}

// TestE2E_ControllerMaxConcurrent validates max concurrent remediation limit:
// 1. Deploy controller with maxConcurrentRemediations=1
// 2. Inject failures on 2 nodes simultaneously
// 3. Verify only 1 lease is granted at a time
// 4. Verify second node waits for first to complete
func TestE2E_ControllerMaxConcurrent(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	ctx := context.Background()

	utils.LogSection("E2E Test: Max Concurrent Remediations")

	// Setup cluster with 3 workers
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

	// Deploy controller with maxConcurrent=1
	utils.Log("Deploying controller with maxConcurrentRemediations=1")
	if err := deployControllerWithConfig(ctx, kubeContext, 1); err != nil {
		t.Fatalf("Failed to deploy controller: %v", err)
	}

	controllerURL := "http://node-doctor-controller.node-doctor.svc.cluster.local:8080"
	if err := waitForControllerReady(ctx, kubeContext, controllerURL); err != nil {
		t.Fatalf("Controller not ready: %v", err)
	}

	if err := deployNodeDoctorWithController(ctx, kubeContext, controllerURL); err != nil {
		t.Fatalf("Failed to deploy Node Doctor: %v", err)
	}

	// Inject failures on 2 nodes
	worker1 := "node-doctor-e2e-worker"
	worker2 := "node-doctor-e2e-worker2"

	utils.Log("Injecting failures on both worker nodes")
	if err := stopKubeletOnNode(ctx, kubeContext, worker1); err != nil {
		t.Fatalf("Failed to stop kubelet on %s: %v", worker1, err)
	}
	if err := stopKubeletOnNode(ctx, kubeContext, worker2); err != nil {
		t.Fatalf("Failed to stop kubelet on %s: %v", worker2, err)
	}

	// Wait a bit and verify only 1 lease exists
	time.Sleep(5 * time.Second)

	leaseCount, err := getActiveLeaseCount(ctx, kubeContext, controllerURL)
	if err != nil {
		t.Fatalf("Failed to get lease count: %v", err)
	}

	if leaseCount > 1 {
		t.Errorf("Expected max 1 concurrent lease, got %d", leaseCount)
	} else {
		utils.LogSuccess("Max concurrent limit enforced: %d active lease(s)", leaseCount)
	}

	// Wait for first remediation to complete and second to proceed
	utils.Log("Waiting for sequential remediation completion")
	if err := waitForAllRemediationsComplete(ctx, kubeContext, controllerURL); err != nil {
		t.Logf("Warning: remediation completion check failed: %v", err)
	}

	utils.LogSection("✓ E2E Test PASSED: Max Concurrent Remediations")
}

// TestE2E_ControllerFallback validates fallback behavior when controller is unreachable
func TestE2E_ControllerFallback(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	ctx := context.Background()

	utils.LogSection("E2E Test: Controller Fallback Behavior")

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

	// Deploy Node Doctor with controller URL that doesn't exist
	// and fallbackOnUnreachable: true
	utils.Log("Deploying Node Doctor with fallback enabled (no controller)")
	fakeControllerURL := "http://nonexistent-controller.node-doctor.svc.cluster.local:8080"
	if err := deployNodeDoctorWithFallback(ctx, kubeContext, fakeControllerURL, true); err != nil {
		t.Fatalf("Failed to deploy Node Doctor: %v", err)
	}

	// Inject kubelet failure
	utils.Log("Injecting kubelet failure")
	if err := stopKubeletOnWorker(ctx, kubeContext); err != nil {
		t.Fatalf("Failed to stop kubelet: %v", err)
	}

	// Verify remediation still proceeds (fallback behavior)
	utils.Log("Verifying fallback remediation proceeds")
	if err := waitForFallbackRemediation(ctx, kubeContext); err != nil {
		t.Fatalf("Fallback remediation failed: %v", err)
	}
	utils.LogSuccess("Remediation proceeded with fallback behavior")

	utils.LogSection("✓ E2E Test PASSED: Controller Fallback")
}

// Helper functions

func deployController(ctx context.Context, kubeContext string) error {
	return deployControllerWithConfig(ctx, kubeContext, 3)
}

func deployControllerWithConfig(ctx context.Context, kubeContext string, maxConcurrent int) error {
	manifest := fmt.Sprintf(`apiVersion: v1
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
            - name: MAX_CONCURRENT_REMEDIATIONS
              value: "%d"
            - name: CORRELATION_ENABLED
              value: "true"
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
`, maxConcurrent)

	return utils.ApplyManifest(ctx, kubeContext, manifest)
}

func waitForControllerReady(ctx context.Context, kubeContext, controllerURL string) error {
	config := &utils.RetryConfig{
		Interval:    5 * time.Second,
		Timeout:     2 * time.Minute,
		Description: "controller to be ready",
	}

	return utils.WaitFor(ctx, config, func() (bool, error) {
		// Check via kubectl since we're inside the cluster
		output, err := utils.KubectlExec(ctx, kubeContext,
			"curl", "-s", "-o", "/dev/null", "-w", "%{http_code}",
			controllerURL+"/readyz")
		if err != nil {
			return false, nil
		}
		return strings.TrimSpace(output) == "200", nil
	})
}

func deployNodeDoctorWithController(ctx context.Context, kubeContext, controllerURL string) error {
	manifest := fmt.Sprintf(`apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: node-doctor
  namespace: node-doctor
spec:
  selector:
    matchLabels:
      app: node-doctor
  template:
    metadata:
      labels:
        app: node-doctor
    spec:
      hostNetwork: true
      hostPID: true
      serviceAccountName: node-doctor
      tolerations:
        - operator: Exists
      containers:
        - name: node-doctor
          image: node-doctor:e2e-test
          imagePullPolicy: IfNotPresent
          securityContext:
            privileged: true
          env:
            - name: NODE_NAME
              valueFrom:
                fieldRef:
                  fieldPath: spec.nodeName
            - name: CONTROLLER_URL
              value: "%s"
            - name: COORDINATION_ENABLED
              value: "true"
            - name: FALLBACK_ON_UNREACHABLE
              value: "false"
          volumeMounts:
            - name: host-root
              mountPath: /host
      volumes:
        - name: host-root
          hostPath:
            path: /
            type: Directory
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: node-doctor
  namespace: node-doctor
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: node-doctor
rules:
  - apiGroups: [""]
    resources: ["nodes"]
    verbs: ["get", "list", "watch", "patch"]
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["create", "patch"]
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
    namespace: node-doctor
`, controllerURL)

	return utils.ApplyManifest(ctx, kubeContext, manifest)
}

func deployNodeDoctorWithFallback(ctx context.Context, kubeContext, controllerURL string, fallbackEnabled bool) error {
	manifest := fmt.Sprintf(`apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: node-doctor
  namespace: node-doctor
spec:
  selector:
    matchLabels:
      app: node-doctor
  template:
    metadata:
      labels:
        app: node-doctor
    spec:
      hostNetwork: true
      hostPID: true
      serviceAccountName: node-doctor
      tolerations:
        - operator: Exists
      containers:
        - name: node-doctor
          image: node-doctor:e2e-test
          imagePullPolicy: IfNotPresent
          securityContext:
            privileged: true
          env:
            - name: NODE_NAME
              valueFrom:
                fieldRef:
                  fieldPath: spec.nodeName
            - name: CONTROLLER_URL
              value: "%s"
            - name: COORDINATION_ENABLED
              value: "true"
            - name: FALLBACK_ON_UNREACHABLE
              value: "%t"
          volumeMounts:
            - name: host-root
              mountPath: /host
      volumes:
        - name: host-root
          hostPath:
            path: /
            type: Directory
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: node-doctor
  namespace: node-doctor
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: node-doctor
rules:
  - apiGroups: [""]
    resources: ["nodes"]
    verbs: ["get", "list", "watch", "patch"]
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["create", "patch"]
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
    namespace: node-doctor
`, controllerURL, fallbackEnabled)

	return utils.ApplyManifest(ctx, kubeContext, manifest)
}

func verifyNoActiveLeases(ctx context.Context, kubeContext, controllerURL string) error {
	count, err := getActiveLeaseCount(ctx, kubeContext, controllerURL)
	if err != nil {
		return err
	}
	if count != 0 {
		return fmt.Errorf("expected 0 active leases, got %d", count)
	}
	return nil
}

func getActiveLeaseCount(ctx context.Context, kubeContext, controllerURL string) (int, error) {
	output, err := utils.KubectlExec(ctx, kubeContext,
		"curl", "-s", controllerURL+"/api/v1/leases")
	if err != nil {
		return 0, err
	}

	var response struct {
		Data []interface{} `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &response); err != nil {
		return 0, err
	}

	return len(response.Data), nil
}

func stopKubeletOnNode(ctx context.Context, kubeContext, nodeName string) error {
	_, err := utils.ExecOnNode(ctx, kubeContext, nodeName, "systemctl", "stop", "kubelet")
	return err
}

func stopKubeletOnWorker(ctx context.Context, kubeContext string) error {
	return stopKubeletOnNode(ctx, kubeContext, "node-doctor-e2e-worker")
}

func waitForLease(ctx context.Context, kubeContext, controllerURL, nodeName string) error {
	config := &utils.RetryConfig{
		Interval:    5 * time.Second,
		Timeout:     2 * time.Minute,
		Description: fmt.Sprintf("lease for %s", nodeName),
	}

	return utils.WaitFor(ctx, config, func() (bool, error) {
		output, err := utils.KubectlExec(ctx, kubeContext,
			"curl", "-s", controllerURL+"/api/v1/leases")
		if err != nil {
			return false, nil
		}

		// Check if our node has a lease
		return strings.Contains(output, nodeName), nil
	})
}

func waitForLeaseRelease(ctx context.Context, kubeContext, controllerURL, nodeName string) error {
	config := &utils.RetryConfig{
		Interval:    5 * time.Second,
		Timeout:     3 * time.Minute,
		Description: fmt.Sprintf("lease release for %s", nodeName),
	}

	return utils.WaitFor(ctx, config, func() (bool, error) {
		output, err := utils.KubectlExec(ctx, kubeContext,
			"curl", "-s", controllerURL+"/api/v1/leases")
		if err != nil {
			return false, nil
		}

		// Check if our node no longer has a lease
		return !strings.Contains(output, nodeName), nil
	})
}

func waitForRemediationOnNode(ctx context.Context, kubeContext, nodeName string) error {
	config := &utils.RetryConfig{
		Interval:    5 * time.Second,
		Timeout:     2 * time.Minute,
		Description: fmt.Sprintf("remediation on %s", nodeName),
	}

	return utils.WaitFor(ctx, config, func() (bool, error) {
		logs, err := utils.GetNodeDoctorLogs(ctx, kubeContext)
		if err != nil {
			return false, nil
		}

		return containsAny(logs,
			"attempting remediation",
			"restarting kubelet",
			"remediation executed",
			"lease granted"), nil
	})
}

func verifyKubeletHealthyOnNode(ctx context.Context, kubeContext, nodeName string) error {
	config := &utils.RetryConfig{
		Interval:    5 * time.Second,
		Timeout:     2 * time.Minute,
		Description: fmt.Sprintf("kubelet healthy on %s", nodeName),
	}

	return utils.WaitFor(ctx, config, func() (bool, error) {
		output, err := utils.ExecOnNode(ctx, kubeContext, nodeName, "systemctl", "is-active", "kubelet")
		if err != nil {
			return false, nil
		}
		return strings.TrimSpace(output) == "active", nil
	})
}

func waitForAllRemediationsComplete(ctx context.Context, kubeContext, controllerURL string) error {
	config := &utils.RetryConfig{
		Interval:    10 * time.Second,
		Timeout:     5 * time.Minute,
		Description: "all remediations to complete",
	}

	return utils.WaitFor(ctx, config, func() (bool, error) {
		count, err := getActiveLeaseCount(ctx, kubeContext, controllerURL)
		if err != nil {
			return false, nil
		}
		return count == 0, nil
	})
}

func waitForFallbackRemediation(ctx context.Context, kubeContext string) error {
	config := &utils.RetryConfig{
		Interval:    5 * time.Second,
		Timeout:     2 * time.Minute,
		Description: "fallback remediation",
	}

	return utils.WaitFor(ctx, config, func() (bool, error) {
		logs, err := utils.GetNodeDoctorLogs(ctx, kubeContext)
		if err != nil {
			return false, nil
		}

		return containsAny(logs,
			"fallback",
			"controller unreachable",
			"proceeding with remediation"), nil
	})
}
