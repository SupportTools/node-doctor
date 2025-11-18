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
	"time"
)

// TeardownOptions contains configuration for cluster teardown
type TeardownOptions struct {
	// ClusterName to delete
	ClusterName string

	// ExportLogs exports cluster logs before deletion
	ExportLogs bool

	// LogsPath where to export logs (defaults to ./e2e-logs)
	LogsPath string

	// Timeout for teardown operations
	Timeout time.Duration
}

// DefaultTeardownOptions returns default teardown options
func DefaultTeardownOptions() *TeardownOptions {
	// Check if we should keep cluster (for debugging)
	keepCluster := os.Getenv("E2E_KEEP_CLUSTER") == "1"
	if keepCluster {
		fmt.Println("E2E_KEEP_CLUSTER=1, skipping cluster teardown for debugging")
	}

	return &TeardownOptions{
		ClusterName: ClusterName,
		ExportLogs:  os.Getenv("E2E_EXPORT_LOGS") == "1",
		LogsPath:    "./e2e-logs",
		Timeout:     2 * time.Minute,
	}
}

// Teardown cleans up the KIND cluster and optionally exports logs
func Teardown(ctx context.Context, opts *TeardownOptions) error {
	if opts == nil {
		opts = DefaultTeardownOptions()
	}

	// Check if we should keep cluster for debugging
	if os.Getenv("E2E_KEEP_CLUSTER") == "1" {
		fmt.Printf("Keeping cluster %s for debugging (E2E_KEEP_CLUSTER=1)\n", opts.ClusterName)
		fmt.Printf("To delete manually: kind delete cluster --name %s\n", opts.ClusterName)
		return nil
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	// Export logs if requested
	if opts.ExportLogs {
		fmt.Printf("Exporting cluster logs to %s...\n", opts.LogsPath)
		if err := ExportLogs(ctx, opts.ClusterName, opts.LogsPath); err != nil {
			// Don't fail teardown on log export failure
			fmt.Printf("Warning: failed to export logs: %v\n", err)
		}
	}

	// Check if cluster exists
	exists, err := clusterExists(ctx, opts.ClusterName)
	if err != nil {
		return fmt.Errorf("failed to check cluster existence: %w", err)
	}

	if !exists {
		fmt.Printf("Cluster %s does not exist, nothing to clean up\n", opts.ClusterName)
		return nil
	}

	// Delete cluster
	fmt.Printf("Deleting KIND cluster: %s\n", opts.ClusterName)
	if err := deleteCluster(ctx, opts.ClusterName); err != nil {
		return fmt.Errorf("failed to delete cluster: %w", err)
	}

	fmt.Printf("Cluster %s deleted successfully\n", opts.ClusterName)
	return nil
}

// ExportLogs exports cluster logs using KIND
func ExportLogs(ctx context.Context, clusterName, logsPath string) error {
	// KIND provides built-in log export
	// This exports logs from all cluster components
	cmd := exec.CommandContext(ctx, "kind", "export", "logs",
		"--name", clusterName,
		logsPath)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kind export logs failed: %w", err)
	}

	fmt.Printf("Logs exported to: %s\n", logsPath)
	return nil
}

// Cleanup is a convenience function that combines common cleanup tasks
func Cleanup(ctx context.Context) error {
	opts := DefaultTeardownOptions()
	return Teardown(ctx, opts)
}
