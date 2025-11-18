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
	"time"
)

// RetryConfig contains configuration for retry operations
type RetryConfig struct {
	// Interval between retry attempts
	Interval time.Duration

	// Timeout for total retry duration
	Timeout time.Duration

	// Description of what we're waiting for (for logging)
	Description string
}

// DefaultRetryConfig returns sensible defaults for retry operations
func DefaultRetryConfig(description string) *RetryConfig {
	return &RetryConfig{
		Interval:    5 * time.Second,
		Timeout:     2 * time.Minute,
		Description: description,
	}
}

// WaitFor polls a condition function until it returns true or timeout occurs
func WaitFor(ctx context.Context, config *RetryConfig, conditionFn func() (bool, error)) error {
	if config == nil {
		config = DefaultRetryConfig("condition")
	}

	// Create timeout context
	ctx, cancel := context.WithTimeout(ctx, config.Timeout)
	defer cancel()

	ticker := time.NewTicker(config.Interval)
	defer ticker.Stop()

	// Try immediately first
	ready, err := conditionFn()
	if err != nil {
		return fmt.Errorf("condition check failed: %w", err)
	}
	if ready {
		return nil
	}

	Log("Waiting for: %s (timeout: %v, interval: %v)", config.Description, config.Timeout, config.Interval)

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for: %s", config.Description)

		case <-ticker.C:
			ready, err := conditionFn()
			if err != nil {
				// Log error but continue retrying
				Log("Condition check error: %v (retrying...)", err)
				continue
			}

			if ready {
				Log("✓ Condition met: %s", config.Description)
				return nil
			}

			Log("Still waiting for: %s...", config.Description)
		}
	}
}

// WaitForFunc is a convenience wrapper for WaitFor with default config
func WaitForFunc(ctx context.Context, description string, conditionFn func() (bool, error)) error {
	config := DefaultRetryConfig(description)
	return WaitFor(ctx, config, conditionFn)
}

// Retry executes a function repeatedly until it succeeds or timeout occurs
func Retry(ctx context.Context, config *RetryConfig, fn func() error) error {
	if config == nil {
		config = DefaultRetryConfig("operation")
	}

	var lastErr error

	// Wrap fn into a condition function
	conditionFn := func() (bool, error) {
		err := fn()
		if err == nil {
			return true, nil // Success
		}
		// Save error and retry
		lastErr = err
		return false, nil
	}

	err := WaitFor(ctx, config, conditionFn)
	if err != nil && lastErr != nil {
		// Return the actual error instead of just timeout
		return fmt.Errorf("%w (last error: %v)", err, lastErr)
	}

	return err
}

// RetryFunc is a convenience wrapper for Retry with default config
func RetryFunc(ctx context.Context, description string, fn func() error) error {
	config := DefaultRetryConfig(description)
	return Retry(ctx, config, fn)
}

// Eventually is similar to WaitFor but allows the condition to fail multiple times
// before succeeding (useful for flaky conditions)
func Eventually(ctx context.Context, config *RetryConfig, conditionFn func() error) error {
	if config == nil {
		config = DefaultRetryConfig("eventually")
	}

	// Create timeout context
	ctx, cancel := context.WithTimeout(ctx, config.Timeout)
	defer cancel()

	ticker := time.NewTicker(config.Interval)
	defer ticker.Stop()

	var lastErr error

	for {
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("timeout waiting for %s: last error: %w", config.Description, lastErr)
			}
			return fmt.Errorf("timeout waiting for: %s", config.Description)

		case <-ticker.C:
			err := conditionFn()
			if err == nil {
				Log("✓ Eventually succeeded: %s", config.Description)
				return nil
			}

			lastErr = err
			Log("Eventually check failed (retrying...): %v", err)
		}
	}
}
