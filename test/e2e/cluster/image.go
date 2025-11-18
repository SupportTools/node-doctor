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

// BuildAndLoadImage builds the Node Doctor Docker image and loads it into KIND cluster
func BuildAndLoadImage(ctx context.Context, clusterName string) error {
	// Get project root (should be 3 levels up from test/e2e/cluster)
	projectRoot, err := getProjectRoot()
	if err != nil {
		return fmt.Errorf("failed to find project root: %w", err)
	}

	imageName := "node-doctor:e2e-test"

	// Step 1: Build Docker image
	fmt.Printf("Building Docker image: %s\n", imageName)
	if err := buildDockerImage(ctx, projectRoot, imageName); err != nil {
		return fmt.Errorf("failed to build image: %w", err)
	}

	// Step 2: Load image into KIND cluster
	fmt.Printf("Loading image into KIND cluster: %s\n", clusterName)
	if err := loadImageIntoKIND(ctx, clusterName, imageName); err != nil {
		return fmt.Errorf("failed to load image into KIND: %w", err)
	}

	fmt.Printf("Image %s built and loaded successfully\n", imageName)
	return nil
}

// buildDockerImage builds the Node Doctor Docker image
func buildDockerImage(ctx context.Context, projectRoot, imageName string) error {
	// Create timeout for build operation
	buildCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	// Check if Dockerfile exists
	dockerfilePath := filepath.Join(projectRoot, "Dockerfile")
	if _, err := os.Stat(dockerfilePath); err != nil {
		return fmt.Errorf("Dockerfile not found at %s: %w", dockerfilePath, err)
	}

	// Build the image
	cmd := exec.CommandContext(buildCtx, "docker", "build",
		"-t", imageName,
		"-f", dockerfilePath,
		projectRoot)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = projectRoot

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}

	return nil
}

// loadImageIntoKIND loads a Docker image into KIND cluster
func loadImageIntoKIND(ctx context.Context, clusterName, imageName string) error {
	cmd := exec.CommandContext(ctx, "kind", "load", "docker-image",
		imageName,
		"--name", clusterName)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kind load docker-image failed: %w", err)
	}

	return nil
}

// getProjectRoot finds the Node Doctor project root directory
func getProjectRoot() (string, error) {
	// Start from current directory and walk up to find go.mod
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	// Walk up the directory tree looking for go.mod
	for {
		goModPath := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(goModPath); err == nil {
			// Verify it's the node-doctor project by checking module name
			content, err := os.ReadFile(goModPath)
			if err != nil {
				return "", err
			}

			if strings.Contains(string(content), "github.com/supporttools/node-doctor") {
				return dir, nil
			}
		}

		// Move up one directory
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached root without finding go.mod
			return "", fmt.Errorf("could not find node-doctor project root (go.mod not found)")
		}
		dir = parent
	}
}
