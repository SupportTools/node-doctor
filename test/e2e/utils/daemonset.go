// Copyright 2025 Support Tools Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

//go:build e2e
// +build e2e

package utils

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// DeployNodeDoctor deploys the Node Doctor DaemonSet to the cluster
func DeployNodeDoctor(ctx context.Context, kubeContext string) error {
	LogSection("Deploying Node Doctor DaemonSet")

	// Create namespace if it doesn't exist
	if err := createNamespace(ctx, kubeContext, "node-doctor"); err != nil {
		return fmt.Errorf("failed to create namespace: %w", err)
	}

	// Apply DaemonSet manifest
	// For E2E tests, we'll create a minimal DaemonSet YAML inline
	manifestPath := "/tmp/node-doctor-e2e-daemonset.yaml"
	if err := createDaemonSetManifest(manifestPath); err != nil {
		return fmt.Errorf("failed to create manifest: %w", err)
	}
	defer func() {
		if err := os.Remove(manifestPath); err != nil {
			LogWarning("Failed to remove temporary manifest: %v", err)
		}
	}()

	// Apply the manifest
	cmd := exec.CommandContext(ctx, "kubectl",
		"--context", kubeContext,
		"apply", "-f", manifestPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kubectl apply failed: %w", err)
	}

	// Wait for DaemonSet to be ready
	if err := waitForDaemonSetReady(ctx, kubeContext, "node-doctor", "node-doctor"); err != nil {
		return fmt.Errorf("DaemonSet not ready: %w", err)
	}

	LogSuccess("Node Doctor DaemonSet deployed and ready")
	return nil
}

// createNamespace creates a Kubernetes namespace
func createNamespace(ctx context.Context, kubeContext, namespace string) error {
	cmd := exec.CommandContext(ctx, "kubectl",
		"--context", kubeContext,
		"create", "namespace", namespace,
		"--dry-run=client", "-o", "yaml")

	createYaml, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to generate namespace yaml: %w", err)
	}

	cmd = exec.CommandContext(ctx, "kubectl",
		"--context", kubeContext,
		"apply", "-f", "-")
	cmd.Stdin = strings.NewReader(string(createYaml))

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create namespace: %w", err)
	}

	return nil
}

// createDaemonSetManifest creates a Node Doctor DaemonSet manifest for E2E testing
func createDaemonSetManifest(path string) error {
	// Minimal DaemonSet configuration for E2E testing
	// In production, this would reference actual deployment YAML
	manifest := `apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: node-doctor
  namespace: node-doctor
  labels:
    app: node-doctor
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
          image: node-doctor:e2e-test  # Built from local source
          imagePullPolicy: IfNotPresent
          securityContext:
            privileged: true
          env:
            - name: NODE_NAME
              valueFrom:
                fieldRef:
                  fieldPath: spec.nodeName
            - name: E2E_TEST_MODE
              value: "true"
          volumeMounts:
            - name: host-root
              mountPath: /host
              readOnly: false
            - name: systemd
              mountPath: /run/systemd
              readOnly: false
      volumes:
        - name: host-root
          hostPath:
            path: /
            type: Directory
        - name: systemd
          hostPath:
            path: /run/systemd
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
    namespace: node-doctor
`

	return os.WriteFile(path, []byte(manifest), 0644)
}

// waitForDaemonSetReady waits for a DaemonSet to be ready
func waitForDaemonSetReady(ctx context.Context, kubeContext, namespace, name string) error {
	config := &RetryConfig{
		Interval:    5 * time.Second,
		Timeout:     2 * time.Minute,
		Description: fmt.Sprintf("DaemonSet %s/%s to be ready", namespace, name),
	}

	return WaitFor(ctx, config, func() (bool, error) {
		cmd := exec.CommandContext(ctx, "kubectl",
			"--context", kubeContext,
			"-n", namespace,
			"get", "daemonset", name,
			"-o", "jsonpath={.status.numberReady}/{.status.desiredNumberScheduled}")

		output, err := cmd.CombinedOutput()
		if err != nil {
			return false, nil // Retry on error
		}

		// Parse output like "1/1"
		parts := strings.Split(strings.TrimSpace(string(output)), "/")
		if len(parts) != 2 {
			return false, nil
		}

		// Check if ready == desired
		return parts[0] == parts[1] && parts[0] != "0", nil
	})
}

// DeleteNodeDoctor removes the Node Doctor DaemonSet from the cluster
func DeleteNodeDoctor(ctx context.Context, kubeContext string) error {
	Log("Deleting Node Doctor DaemonSet")

	cmd := exec.CommandContext(ctx, "kubectl",
		"--context", kubeContext,
		"delete", "namespace", "node-doctor",
		"--ignore-not-found=true",
		"--timeout=60s")

	if err := cmd.Run(); err != nil {
		LogWarning("Failed to delete namespace (non-fatal): %v", err)
	}

	return nil
}

// GetNodeDoctorLogs retrieves logs from Node Doctor pods
func GetNodeDoctorLogs(ctx context.Context, kubeContext string) (string, error) {
	cmd := exec.CommandContext(ctx, "kubectl",
		"--context", kubeContext,
		"-n", "node-doctor",
		"logs",
		"-l", "app=node-doctor",
		"--tail=100")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get logs: %w", err)
	}

	return string(output), nil
}

// ExecInNodeDoctor executes a command in a Node Doctor pod
func ExecInNodeDoctor(ctx context.Context, kubeContext string, command ...string) (string, error) {
	// Get the pod name
	getPodCmd := exec.CommandContext(ctx, "kubectl",
		"--context", kubeContext,
		"-n", "node-doctor",
		"get", "pods",
		"-l", "app=node-doctor",
		"-o", "jsonpath={.items[0].metadata.name}")

	podName, err := getPodCmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get pod name: %w", err)
	}

	// Execute command in pod
	args := []string{
		"--context", kubeContext,
		"-n", "node-doctor",
		"exec", string(podName),
		"--",
	}
	args = append(args, command...)

	cmd := exec.CommandContext(ctx, "kubectl", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("exec failed: %w", err)
	}

	return string(output), nil
}
