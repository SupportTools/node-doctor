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

// EventObserver watches for Kubernetes events
type EventObserver struct {
	kubeContext string
	namespace   string
}

// NewEventObserver creates a new event observer
func NewEventObserver(kubeContext, namespace string) *EventObserver {
	return &EventObserver{
		kubeContext: kubeContext,
		namespace:   namespace,
	}
}

// WaitForEvent waits for a specific event to appear
func (o *EventObserver) WaitForEvent(ctx context.Context, reason string, timeout time.Duration) error {
	config := &utils.RetryConfig{
		Interval:    3 * time.Second,
		Timeout:     timeout,
		Description: fmt.Sprintf("event with reason=%s", reason),
	}

	return utils.WaitFor(ctx, config, func() (bool, error) {
		exists, err := o.EventExists(ctx, reason)
		if err != nil {
			utils.LogDebug("Event check error: %v", err)
			return false, nil // Retry on error
		}
		return exists, nil
	})
}

// EventExists checks if an event with the given reason exists
func (o *EventObserver) EventExists(ctx context.Context, reason string) (bool, error) {
	args := []string{
		"--context", o.kubeContext,
		"get", "events",
		"--field-selector", fmt.Sprintf("reason=%s", reason),
		"-o", "name",
	}

	if o.namespace != "" {
		args = append([]string{"--context", o.kubeContext, "-n", o.namespace}, args[2:]...)
	} else {
		args = append([]string{"--context", o.kubeContext, "--all-namespaces"}, args[2:]...)
	}

	cmd := exec.CommandContext(ctx, "kubectl", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("kubectl get events failed: %w (output: %s)", err, string(output))
	}

	// Event exists if output is not empty
	return strings.TrimSpace(string(output)) != "", nil
}

// GetEventCount returns the count of events with a given reason
func (o *EventObserver) GetEventCount(ctx context.Context, reason string) (int, error) {
	args := []string{
		"--context", o.kubeContext,
		"get", "events",
		"--field-selector", fmt.Sprintf("reason=%s", reason),
		"-o", "name",
	}

	if o.namespace != "" {
		args = append([]string{"--context", o.kubeContext, "-n", o.namespace}, args[2:]...)
	}

	cmd := exec.CommandContext(ctx, "kubectl", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("kubectl get events failed: %w", err)
	}

	// Count non-empty lines
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return 0, nil
	}

	return len(lines), nil
}

// GetEventMessage retrieves the message from an event with given reason
func (o *EventObserver) GetEventMessage(ctx context.Context, reason string) (string, error) {
	args := []string{
		"--context", o.kubeContext,
		"get", "events",
		"--field-selector", fmt.Sprintf("reason=%s", reason),
		"-o", "jsonpath={.items[0].message}",
	}

	if o.namespace != "" {
		args = append([]string{"--context", o.kubeContext, "-n", o.namespace}, args[2:]...)
	}

	cmd := exec.CommandContext(ctx, "kubectl", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("kubectl get event message failed: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// WaitForEventWithMessage waits for an event with specific reason and message substring
func (o *EventObserver) WaitForEventWithMessage(ctx context.Context, reason, messageSubstr string, timeout time.Duration) error {
	config := &utils.RetryConfig{
		Interval:    3 * time.Second,
		Timeout:     timeout,
		Description: fmt.Sprintf("event with reason=%s and message containing '%s'", reason, messageSubstr),
	}

	return utils.WaitFor(ctx, config, func() (bool, error) {
		message, err := o.GetEventMessage(ctx, reason)
		if err != nil {
			return false, nil // Event doesn't exist yet, retry
		}

		return strings.Contains(message, messageSubstr), nil
	})
}

// ListAllEvents lists all events in the namespace (for debugging)
func (o *EventObserver) ListAllEvents(ctx context.Context) (string, error) {
	args := []string{
		"--context", o.kubeContext,
		"get", "events",
		"--sort-by", ".lastTimestamp",
	}

	if o.namespace != "" {
		args = append([]string{"--context", o.kubeContext, "-n", o.namespace}, args[2:]...)
	} else {
		args = append([]string{"--context", o.kubeContext, "--all-namespaces"}, args[2:]...)
	}

	cmd := exec.CommandContext(ctx, "kubectl", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("kubectl get events failed: %w", err)
	}

	return string(output), nil
}
