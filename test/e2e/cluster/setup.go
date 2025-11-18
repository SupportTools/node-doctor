// Copyright 2025 Support Tools Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

//go:build e2e
// +build e2e

package cluster

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	// ClusterName is the name of the KIND cluster for E2E tests
	ClusterName = "node-doctor-e2e"

	// DefaultTimeout is the default timeout for cluster operations
	DefaultTimeout = 5 * time.Minute

	// NodeReadyTimeout is the timeout waiting for nodes to be ready
	NodeReadyTimeout = 2 * time.Minute
)

// SetupOptions contains configuration for cluster setup
type SetupOptions struct {
	// ClusterName override (defaults to ClusterName constant)
	ClusterName string

	// ConfigPath to KIND config YAML (defaults to cluster/kind-config.yaml)
	ConfigPath string

	// KeepCluster prevents cleanup on failure (for debugging)
	KeepCluster bool

	// Timeout for setup operations
	Timeout time.Duration
}

// DefaultSetupOptions returns default setup options
func DefaultSetupOptions() *SetupOptions {
	// Check for CI mode environment variable
	keepCluster := os.Getenv("E2E_KEEP_CLUSTER") == "1"

	// Determine config path relative to test location
	configPath := filepath.Join("cluster", "kind-config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Try alternate path from scenarios/ directory
		configPath = filepath.Join("..", "cluster", "kind-config.yaml")
	}

	return &SetupOptions{
		ClusterName: ClusterName,
		ConfigPath:  configPath,
		KeepCluster: keepCluster,
		Timeout:     DefaultTimeout,
	}
}

// Setup creates a KIND cluster for E2E testing
func Setup(ctx context.Context, opts *SetupOptions) error {
	if opts == nil {
		opts = DefaultSetupOptions()
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	// Check if KIND is installed
	if err := checkKINDInstalled(ctx); err != nil {
		return fmt.Errorf("KIND not installed: %w", err)
	}

	// Check if kubectl is installed
	if err := checkKubectlInstalled(ctx); err != nil {
		return fmt.Errorf("kubectl not installed: %w", err)
	}

	// Check if cluster already exists
	exists, err := clusterExists(ctx, opts.ClusterName)
	if err != nil {
		return fmt.Errorf("failed to check cluster existence: %w", err)
	}

	if exists {
		fmt.Printf("Cluster %s already exists, deleting...\n", opts.ClusterName)
		if err := deleteCluster(ctx, opts.ClusterName); err != nil {
			return fmt.Errorf("failed to delete existing cluster: %w", err)
		}
	}

	// Create cluster
	fmt.Printf("Creating KIND cluster: %s\n", opts.ClusterName)
	if err := createCluster(ctx, opts); err != nil {
		return fmt.Errorf("failed to create cluster: %w", err)
	}

	// Wait for cluster to be ready
	fmt.Println("Waiting for cluster to be ready...")
	if err := waitForClusterReady(ctx, opts.ClusterName); err != nil {
		if !opts.KeepCluster {
			// Cleanup on failure
			_ = deleteCluster(context.Background(), opts.ClusterName)
		}
		return fmt.Errorf("cluster not ready: %w", err)
	}

	// Build and load Node Doctor image
	fmt.Println("Building and loading Node Doctor image...")
	if err := BuildAndLoadImage(ctx, opts.ClusterName); err != nil {
		if !opts.KeepCluster {
			// Cleanup on failure
			_ = deleteCluster(context.Background(), opts.ClusterName)
		}
		return fmt.Errorf("failed to build/load image: %w", err)
	}

	fmt.Printf("Cluster %s is ready!\n", opts.ClusterName)
	return nil
}

// checkKINDInstalled verifies KIND CLI is available
func checkKINDInstalled(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "kind", "version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kind command not found (install: go install sigs.k8s.io/kind@latest)")
	}
	return nil
}

// checkKubectlInstalled verifies kubectl CLI is available
func checkKubectlInstalled(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "kubectl", "version", "--client")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kubectl command not found")
	}
	return nil
}

// clusterExists checks if a KIND cluster exists
func clusterExists(ctx context.Context, name string) (bool, error) {
	cmd := exec.CommandContext(ctx, "kind", "get", "clusters")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("failed to list clusters: %w", err)
	}

	// Parse cluster names from output (one per line)
	clusterNames := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, clusterName := range clusterNames {
		if strings.TrimSpace(clusterName) == name {
			return true, nil
		}
	}

	return false, nil
}

// createCluster creates a new KIND cluster
func createCluster(ctx context.Context, opts *SetupOptions) error {
	args := []string{
		"create", "cluster",
		"--name", opts.ClusterName,
	}

	// Add config if specified
	if opts.ConfigPath != "" {
		if _, err := os.Stat(opts.ConfigPath); err == nil {
			args = append(args, "--config", opts.ConfigPath)
		} else {
			fmt.Printf("Warning: KIND config not found at %s, using defaults\n", opts.ConfigPath)
		}
	}

	cmd := exec.CommandContext(ctx, "kind", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kind create cluster failed: %w", err)
	}

	return nil
}

// waitForClusterReady waits for cluster nodes to be ready
func waitForClusterReady(ctx context.Context, clusterName string) error {
	// Create context with node ready timeout
	ctx, cancel := context.WithTimeout(ctx, NodeReadyTimeout)
	defer cancel()

	kubeContext := "kind-" + clusterName

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for cluster to be ready")

		case <-ticker.C:
			cmd := exec.CommandContext(ctx, "kubectl",
				"--context", kubeContext,
				"get", "nodes",
				"-o", "jsonpath={.items[*].status.conditions[?(@.type==\"Ready\")].status}")

			output, err := cmd.CombinedOutput()
			if err != nil {
				continue // Retry
			}

			// Check if output contains "True" (node is ready)
			if string(output) == "True" {
				return nil
			}

			fmt.Println("Waiting for nodes to be ready...")
		}
	}
}

// deleteCluster deletes a KIND cluster
func deleteCluster(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "kind", "delete", "cluster", "--name", name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kind delete cluster failed: %w", err)
	}

	return nil
}

// GetKubeConfig returns the path to the kubeconfig for the cluster
func GetKubeConfig(clusterName string) string {
	// KIND uses the default kubeconfig with context "kind-<cluster-name>"
	return os.Getenv("KUBECONFIG")
}

// GetContext returns the kubectl context name for the cluster
func GetContext(clusterName string) string {
	return "kind-" + clusterName
}
