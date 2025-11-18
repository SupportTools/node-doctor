// Copyright 2025 Support Tools Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

//go:build e2e
// +build e2e

package utils

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// WaitForCoreDNSReady validates CoreDNS deployment is ready
func WaitForCoreDNSReady(ctx context.Context, kubeContext string) error {
	config := &RetryConfig{
		Interval:    5 * time.Second,
		Timeout:     3 * time.Minute,
		Description: "CoreDNS pods to be ready",
	}

	return WaitFor(ctx, config, func() (bool, error) {
		// Check CoreDNS pod readiness
		cmd := exec.CommandContext(ctx, "kubectl",
			"--context", kubeContext,
			"get", "pod", "-n", "kube-system",
			"-l", "k8s-app=kube-dns",
			"-o", "jsonpath={.items[*].status.conditions[?(@.type==\"Ready\")].status}")

		output, err := cmd.CombinedOutput()
		if err != nil {
			Log("CoreDNS check error (will retry): %v", err)
			return false, nil // Transient error, retry
		}

		// For 2 CoreDNS replicas, we expect "True True"
		statuses := strings.Fields(strings.TrimSpace(string(output)))
		if len(statuses) == 0 {
			Log("No CoreDNS pods found yet (will retry)")
			return false, nil // No pods found yet
		}

		// All pods must be ready
		for _, status := range statuses {
			if status != "True" {
				Log("CoreDNS pods not all ready yet: %v (will retry)", statuses)
				return false, nil
			}
		}

		return len(statuses) >= 1, nil // At least 1 pod ready
	})
}

// WaitForCNIReady validates CNI DaemonSet is ready
func WaitForCNIReady(ctx context.Context, kubeContext string) error {
	config := &RetryConfig{
		Interval:    5 * time.Second,
		Timeout:     3 * time.Minute,
		Description: "CNI DaemonSet to be ready",
	}

	return WaitFor(ctx, config, func() (bool, error) {
		// Check for common CNI plugins (Flannel, Calico, Kindnet)
		// KIND typically uses kindnet (built-in CNI)
		cniChecks := []struct {
			namespace string
			label     string
		}{
			{"kube-system", "app=kindnet"},           // KIND's built-in CNI
			{"kube-flannel", "app=flannel"},          // Flannel
			{"calico-system", "k8s-app=calico-node"}, // Calico
			{"kube-system", "k8s-app=flannel"},       // Flannel alternative
			{"kube-system", "k8s-app=calico-node"},   // Calico alternative
			{"kube-system", "k8s-app=weave-net"},     // Weave
		}

		for _, check := range cniChecks {
			cmd := exec.CommandContext(ctx, "kubectl",
				"--context", kubeContext,
				"get", "daemonset", "-n", check.namespace,
				"-l", check.label,
				"-o", "jsonpath={.items[0].status.numberReady}/{.items[0].status.desiredNumberScheduled}")

			output, err := cmd.CombinedOutput()
			if err != nil || string(output) == "" {
				continue // Try next CNI
			}

			// Parse "X/Y" format
			parts := strings.Split(strings.TrimSpace(string(output)), "/")
			if len(parts) == 2 && parts[0] == parts[1] && parts[0] != "0" {
				Log("✓ Found CNI: %s in namespace %s (%s ready)", check.label, check.namespace, parts[0])
				return true, nil // Found ready CNI
			}
		}

		// Fallback: Check for any DaemonSet in kube-system that looks network-related
		cmd := exec.CommandContext(ctx, "kubectl",
			"--context", kubeContext,
			"get", "daemonset", "-n", "kube-system",
			"-o", "jsonpath={.items[*].metadata.name}")

		output, err := cmd.CombinedOutput()
		if err == nil {
			daemonsets := strings.Fields(string(output))
			for _, ds := range daemonsets {
				// Check if any DaemonSet name suggests networking
				if strings.Contains(strings.ToLower(ds), "net") ||
					strings.Contains(strings.ToLower(ds), "cni") ||
					strings.Contains(strings.ToLower(ds), "calico") ||
					strings.Contains(strings.ToLower(ds), "flannel") {
					Log("Found potential CNI DaemonSet: %s (assuming ready)", ds)
					return true, nil
				}
			}
		}

		Log("No CNI DaemonSet found yet (will retry)")
		return false, nil // No CNI ready yet
	})
}

// WaitForDNSResolution verifies DNS is functional by testing resolution
func WaitForDNSResolution(ctx context.Context, kubeContext string) error {
	config := &RetryConfig{
		Interval:    5 * time.Second,
		Timeout:     1 * time.Minute,
		Description: "DNS resolution in cluster",
	}

	// Compile regex for IP address validation
	ipRegex := regexp.MustCompile(`Address:\s*(\d+\.\d+\.\d+\.\d+)`)

	return WaitFor(ctx, config, func() (bool, error) {
		// Use kubectl run with --rm to test DNS from a pod
		// This is more reliable than checking CoreDNS pods
		cmd := exec.CommandContext(ctx, "kubectl",
			"--context", kubeContext,
			"run", "dns-test-"+randomSuffix(), "--image=busybox:1.35",
			"--rm", "-i", "--restart=Never",
			"--", "nslookup", "kubernetes.default.svc.cluster.local")

		output, err := cmd.CombinedOutput()
		outputStr := string(output)

		if err != nil {
			// Check if error is due to DNS failure or other issue
			if strings.Contains(outputStr, "server can't find") ||
				strings.Contains(outputStr, "connection timed out") {
				Log("DNS not working yet (will retry): %s", strings.TrimSpace(outputStr))
				return false, nil // DNS not working yet, retry
			}
			// Other errors might be transient (pod creation, etc.)
			Log("DNS test error (will retry): %v", err)
			return false, nil
		}

		// Check if we got a valid IP address using regex
		matches := ipRegex.FindStringSubmatch(outputStr)
		if len(matches) > 1 {
			Log("✓ DNS resolution successful, resolved to: %s", matches[1])
			return true, nil
		}

		Log("DNS test didn't return valid IP (will retry)")
		return false, nil
	})
}

// WaitForPodScheduling verifies the cluster can schedule and run pods
func WaitForPodScheduling(ctx context.Context, kubeContext string) error {
	config := &RetryConfig{
		Interval:    5 * time.Second,
		Timeout:     2 * time.Minute,
		Description: "pod scheduling capability",
	}

	testDeploymentName := "e2e-readiness-test-" + randomSuffix()
	deployed := false

	// Ensure cleanup happens even if context is cancelled
	// Use context.WithoutCancel to inherit values but prevent cleanup cancellation
	defer func() {
		if deployed {
			// Create new context for cleanup with timeout, but don't inherit cancellation
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()

			cmd := exec.CommandContext(cleanupCtx, "kubectl",
				"--context", kubeContext,
				"delete", "deployment", testDeploymentName,
				"--ignore-not-found=true",
				"--timeout=20s")

			if err := cmd.Run(); err != nil {
				LogWarning("Failed to cleanup test deployment %s (non-fatal): %v", testDeploymentName, err)
			} else {
				Log("✓ Cleaned up test deployment %s", testDeploymentName)
			}
		}
	}()

	return WaitFor(ctx, config, func() (bool, error) {
		if !deployed {
			// Deploy test pod
			cmd := exec.CommandContext(ctx, "kubectl",
				"--context", kubeContext,
				"create", "deployment", testDeploymentName,
				"--image=busybox:1.35",
				"--", "sleep", "30")

			err := cmd.Run()
			if err != nil {
				// Deployment may already exist from previous attempt
				if strings.Contains(err.Error(), "already exists") {
					Log("Test deployment %s already exists, checking status", testDeploymentName)
					deployed = true
				} else {
					Log("Failed to create test deployment (will retry): %v", err)
					return false, nil
				}
			} else {
				Log("Created test deployment %s", testDeploymentName)
				deployed = true
			}
		}

		// Wait for rollout
		cmd := exec.CommandContext(ctx, "kubectl",
			"--context", kubeContext,
			"rollout", "status", "deployment/"+testDeploymentName,
			"--timeout=30s")

		err := cmd.Run()
		if err != nil {
			Log("Deployment not ready yet (will retry): %v", err)
			return false, nil
		}

		Log("✓ Test deployment is ready, pod scheduling works")
		return true, nil
	})
}

// randomSuffix generates a random suffix for unique resource names
// Combines cryptographic randomness with timestamp and process ID to ensure uniqueness
func randomSuffix() string {
	timestamp := time.Now().UnixNano() % 100000 // Last 5 digits of nanoseconds
	pid := os.Getpid() % 10000                  // Last 4 digits of PID

	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp+PID if random fails
		return fmt.Sprintf("%d-%d", timestamp, pid)
	}

	// Combine hex random bytes with timestamp for collision resistance
	return fmt.Sprintf("%s-%d", hex.EncodeToString(b), timestamp)
}
