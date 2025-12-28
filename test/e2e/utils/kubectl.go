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

// ApplyManifest applies a Kubernetes manifest to the cluster
func ApplyManifest(ctx context.Context, kubeContext, manifest string) error {
	// Write manifest to temporary file
	tmpFile, err := os.CreateTemp("", "manifest-*.yaml")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(manifest); err != nil {
		return fmt.Errorf("failed to write manifest: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Apply the manifest
	cmd := exec.CommandContext(ctx, "kubectl",
		"--context", kubeContext,
		"apply", "-f", tmpFile.Name())

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("kubectl apply failed: %w\nOutput: %s", err, string(output))
	}

	Log("Applied manifest:\n%s", string(output))
	return nil
}

// KubectlExec executes a command using kubectl exec in a pod
func KubectlExec(ctx context.Context, kubeContext string, command ...string) (string, error) {
	// Find a pod to exec into (use a curl pod or similar)
	// For simplicity, we'll create a temporary curl pod
	output, err := execWithCurlPod(ctx, kubeContext, command...)
	if err != nil {
		return "", err
	}
	return output, nil
}

// execWithCurlPod creates a temporary pod to execute commands like curl
func execWithCurlPod(ctx context.Context, kubeContext string, command ...string) (string, error) {
	// Use kubectl run with --rm to create a temporary pod
	args := []string{
		"--context", kubeContext,
		"-n", "node-doctor",
		"run", "temp-exec-pod",
		"--image=curlimages/curl:latest",
		"--rm", "-i",
		"--restart=Never",
		"--",
	}
	args = append(args, command...)

	cmd := exec.CommandContext(ctx, "kubectl", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Clean up the pod if it wasn't removed
		cleanupCmd := exec.CommandContext(ctx, "kubectl",
			"--context", kubeContext,
			"-n", "node-doctor",
			"delete", "pod", "temp-exec-pod",
			"--ignore-not-found=true")
		cleanupCmd.Run()
		return string(output), fmt.Errorf("exec failed: %w", err)
	}

	return string(output), nil
}

// ExecOnNode executes a command on a specific node via docker exec (for KIND)
func ExecOnNode(ctx context.Context, kubeContext, nodeName string, command ...string) (string, error) {
	// In KIND, nodes are docker containers
	// The container name matches the node name
	containerName := nodeName

	args := []string{"exec", containerName}
	args = append(args, command...)

	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("docker exec failed on %s: %w", nodeName, err)
	}

	return string(output), nil
}

// GetNodeDoctorPods returns the list of Node Doctor pod names
func GetNodeDoctorPods(ctx context.Context, kubeContext string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "kubectl",
		"--context", kubeContext,
		"-n", "node-doctor",
		"get", "pods",
		"-l", "app=node-doctor",
		"-o", "jsonpath={.items[*].metadata.name}")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to get pods: %w", err)
	}

	names := strings.Fields(strings.TrimSpace(string(output)))
	return names, nil
}

// GetControllerPod returns the controller pod name
func GetControllerPod(ctx context.Context, kubeContext string) (string, error) {
	cmd := exec.CommandContext(ctx, "kubectl",
		"--context", kubeContext,
		"-n", "node-doctor",
		"get", "pods",
		"-l", "app=node-doctor-controller",
		"-o", "jsonpath={.items[0].metadata.name}")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get controller pod: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// GetControllerLogs retrieves logs from the controller pod
func GetControllerLogs(ctx context.Context, kubeContext string) (string, error) {
	cmd := exec.CommandContext(ctx, "kubectl",
		"--context", kubeContext,
		"-n", "node-doctor",
		"logs",
		"-l", "app=node-doctor-controller",
		"--tail=100")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get controller logs: %w", err)
	}

	return string(output), nil
}

// WaitForDeploymentReady waits for a Deployment to be ready
func WaitForDeploymentReady(ctx context.Context, kubeContext, namespace, name string) error {
	config := &RetryConfig{
		Interval:    5 * time.Second,
		Timeout:     2 * time.Minute,
		Description: fmt.Sprintf("Deployment %s/%s to be ready", namespace, name),
	}

	return WaitFor(ctx, config, func() (bool, error) {
		cmd := exec.CommandContext(ctx, "kubectl",
			"--context", kubeContext,
			"-n", namespace,
			"get", "deployment", name,
			"-o", "jsonpath={.status.readyReplicas}/{.status.replicas}")

		output, err := cmd.CombinedOutput()
		if err != nil {
			return false, nil
		}

		parts := strings.Split(strings.TrimSpace(string(output)), "/")
		if len(parts) != 2 {
			return false, nil
		}

		return parts[0] == parts[1] && parts[0] != "0", nil
	})
}

// PortForward creates a port forward to a service
func PortForward(ctx context.Context, kubeContext, namespace, service string, localPort, remotePort int) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, "kubectl",
		"--context", kubeContext,
		"-n", namespace,
		"port-forward",
		fmt.Sprintf("svc/%s", service),
		fmt.Sprintf("%d:%d", localPort, remotePort))

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("port-forward failed: %w", err)
	}

	return cmd, nil
}

// GetNodeNames returns the list of node names in the cluster
func GetNodeNames(ctx context.Context, kubeContext string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "kubectl",
		"--context", kubeContext,
		"get", "nodes",
		"-o", "jsonpath={.items[*].metadata.name}")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to get nodes: %w", err)
	}

	names := strings.Fields(strings.TrimSpace(string(output)))
	return names, nil
}

// GetWorkerNodes returns the list of worker node names (excludes control-plane)
func GetWorkerNodes(ctx context.Context, kubeContext string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "kubectl",
		"--context", kubeContext,
		"get", "nodes",
		"--selector=!node-role.kubernetes.io/control-plane",
		"-o", "jsonpath={.items[*].metadata.name}")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to get worker nodes: %w", err)
	}

	names := strings.Fields(strings.TrimSpace(string(output)))
	return names, nil
}
