// Copyright 2025 Support Tools Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

//go:build e2e
// +build e2e

package observers

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/supporttools/node-doctor/test/e2e/utils"
)

// ConditionObserver watches for Node conditions
type ConditionObserver struct {
	kubeContext string
	nodeName    string
}

// NewConditionObserver creates a new condition observer
// If nodeName is empty, uses the first node in the cluster
func NewConditionObserver(kubeContext, nodeName string) *ConditionObserver {
	return &ConditionObserver{
		kubeContext: kubeContext,
		nodeName:    nodeName,
	}
}

// WaitForCondition waits for a specific node condition to appear
func (o *ConditionObserver) WaitForCondition(ctx context.Context, conditionType string, timeout time.Duration) error {
	config := &utils.RetryConfig{
		Interval:    3 * time.Second,
		Timeout:     timeout,
		Description: fmt.Sprintf("node condition %s", conditionType),
	}

	return utils.WaitFor(ctx, config, func() (bool, error) {
		exists, err := o.ConditionExists(ctx, conditionType)
		if err != nil {
			utils.LogDebug("Condition check error: %v", err)
			return false, nil // Retry on error
		}
		return exists, nil
	})
}

// ConditionExists checks if a node condition exists
func (o *ConditionObserver) ConditionExists(ctx context.Context, conditionType string) (bool, error) {
	nodeName, err := o.getNodeName(ctx)
	if err != nil {
		return false, err
	}

	cmd := exec.CommandContext(ctx, "kubectl",
		"--context", o.kubeContext,
		"get", "node", nodeName,
		"-o", fmt.Sprintf("jsonpath={.status.conditions[?(@.type=='%s')].type}", conditionType))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("kubectl get node condition failed: %w", err)
	}

	// Condition exists if output matches the type
	return strings.TrimSpace(string(output)) == conditionType, nil
}

// GetConditionStatus returns the status of a node condition (True/False/Unknown)
func (o *ConditionObserver) GetConditionStatus(ctx context.Context, conditionType string) (string, error) {
	nodeName, err := o.getNodeName(ctx)
	if err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, "kubectl",
		"--context", o.kubeContext,
		"get", "node", nodeName,
		"-o", fmt.Sprintf("jsonpath={.status.conditions[?(@.type=='%s')].status}", conditionType))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("kubectl get condition status failed: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// WaitForConditionStatus waits for a condition to have a specific status
func (o *ConditionObserver) WaitForConditionStatus(ctx context.Context, conditionType, expectedStatus string, timeout time.Duration) error {
	config := &utils.RetryConfig{
		Interval:    3 * time.Second,
		Timeout:     timeout,
		Description: fmt.Sprintf("condition %s to have status %s", conditionType, expectedStatus),
	}

	return utils.WaitFor(ctx, config, func() (bool, error) {
		status, err := o.GetConditionStatus(ctx, conditionType)
		if err != nil {
			utils.LogDebug("Condition status check error: %v", err)
			return false, nil // Retry
		}

		return status == expectedStatus, nil
	})
}

// GetConditionReason returns the reason for a node condition
func (o *ConditionObserver) GetConditionReason(ctx context.Context, conditionType string) (string, error) {
	nodeName, err := o.getNodeName(ctx)
	if err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, "kubectl",
		"--context", o.kubeContext,
		"get", "node", nodeName,
		"-o", fmt.Sprintf("jsonpath={.status.conditions[?(@.type=='%s')].reason}", conditionType))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("kubectl get condition reason failed: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// GetConditionMessage returns the message for a node condition
func (o *ConditionObserver) GetConditionMessage(ctx context.Context, conditionType string) (string, error) {
	nodeName, err := o.getNodeName(ctx)
	if err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, "kubectl",
		"--context", o.kubeContext,
		"get", "node", nodeName,
		"-o", fmt.Sprintf("jsonpath={.status.conditions[?(@.type=='%s')].message}", conditionType))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("kubectl get condition message failed: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// ListAllConditions lists all node conditions (for debugging)
func (o *ConditionObserver) ListAllConditions(ctx context.Context) (string, error) {
	nodeName, err := o.getNodeName(ctx)
	if err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, "kubectl",
		"--context", o.kubeContext,
		"get", "node", nodeName,
		"-o", "jsonpath={.status.conditions}")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("kubectl get conditions failed: %w", err)
	}

	return string(output), nil
}

// getNodeName returns the node name to use for queries
func (o *ConditionObserver) getNodeName(ctx context.Context) (string, error) {
	if o.nodeName != "" {
		return o.nodeName, nil
	}

	// Get first node in cluster
	cmd := exec.CommandContext(ctx, "kubectl",
		"--context", o.kubeContext,
		"get", "nodes",
		"-o", "jsonpath={.items[0].metadata.name}")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("kubectl get nodes failed: %w", err)
	}

	nodeName := strings.TrimSpace(string(output))
	if nodeName == "" {
		return "", fmt.Errorf("no nodes found in cluster")
	}

	// Cache for future calls
	o.nodeName = nodeName
	return nodeName, nil
}
