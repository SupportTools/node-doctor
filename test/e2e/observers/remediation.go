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
	"strings"
	"time"

	"github.com/supporttools/node-doctor/test/e2e/utils"
)

// RemediationObserver watches for remediation actions
type RemediationObserver struct {
	kubeContext string
	namespace   string
}

// NewRemediationObserver creates a new remediation observer
func NewRemediationObserver(kubeContext, namespace string) *RemediationObserver {
	return &RemediationObserver{
		kubeContext: kubeContext,
		namespace:   namespace,
	}
}

// WaitForRemediationAttempt waits for any remediation attempt to appear in logs
func (o *RemediationObserver) WaitForRemediationAttempt(ctx context.Context, timeout time.Duration) error {
	config := &utils.RetryConfig{
		Interval:    5 * time.Second,
		Timeout:     timeout,
		Description: "remediation attempt",
	}

	return utils.WaitFor(ctx, config, func() (bool, error) {
		logs, err := utils.GetNodeDoctorLogs(ctx, o.kubeContext)
		if err != nil {
			utils.LogDebug("Log retrieval error: %v", err)
			return false, nil
		}

		// Look for remediation keywords in logs
		return o.containsRemediationKeywords(logs), nil
	})
}

// WaitForSpecificRemediation waits for a specific remediation action
func (o *RemediationObserver) WaitForSpecificRemediation(ctx context.Context, remediationType string, timeout time.Duration) error {
	config := &utils.RetryConfig{
		Interval:    5 * time.Second,
		Timeout:     timeout,
		Description: fmt.Sprintf("remediation type: %s", remediationType),
	}

	return utils.WaitFor(ctx, config, func() (bool, error) {
		logs, err := utils.GetNodeDoctorLogs(ctx, o.kubeContext)
		if err != nil {
			utils.LogDebug("Log retrieval error: %v", err)
			return false, nil
		}

		return strings.Contains(strings.ToLower(logs), strings.ToLower(remediationType)), nil
	})
}

// VerifyRemediationSuccess checks if remediation succeeded
func (o *RemediationObserver) VerifyRemediationSuccess(ctx context.Context, timeout time.Duration) error {
	config := &utils.RetryConfig{
		Interval:    5 * time.Second,
		Timeout:     timeout,
		Description: "remediation success",
	}

	return utils.WaitFor(ctx, config, func() (bool, error) {
		logs, err := utils.GetNodeDoctorLogs(ctx, o.kubeContext)
		if err != nil {
			utils.LogDebug("Log retrieval error: %v", err)
			return false, nil
		}

		// Look for success indicators
		return o.containsSuccessKeywords(logs), nil
	})
}

// VerifyRemediationFailure checks if remediation failed (for negative testing)
func (o *RemediationObserver) VerifyRemediationFailure(ctx context.Context, timeout time.Duration) error {
	config := &utils.RetryConfig{
		Interval:    5 * time.Second,
		Timeout:     timeout,
		Description: "remediation failure",
	}

	return utils.WaitFor(ctx, config, func() (bool, error) {
		logs, err := utils.GetNodeDoctorLogs(ctx, o.kubeContext)
		if err != nil {
			utils.LogDebug("Log retrieval error: %v", err)
			return false, nil
		}

		// Look for failure indicators
		return o.containsFailureKeywords(logs), nil
	})
}

// WaitForCircuitBreakerOpen waits for circuit breaker to open
func (o *RemediationObserver) WaitForCircuitBreakerOpen(ctx context.Context, timeout time.Duration) error {
	config := &utils.RetryConfig{
		Interval:    3 * time.Second,
		Timeout:     timeout,
		Description: "circuit breaker to open",
	}

	return utils.WaitFor(ctx, config, func() (bool, error) {
		logs, err := utils.GetNodeDoctorLogs(ctx, o.kubeContext)
		if err != nil {
			utils.LogDebug("Log retrieval error: %v", err)
			return false, nil
		}

		// Look for circuit breaker open messages
		return strings.Contains(logs, "circuit breaker opened") ||
			strings.Contains(logs, "circuit breaker is open") ||
			strings.Contains(logs, "CircuitBreakerOpen"), nil
	})
}

// WaitForRateLimitHit waits for rate limiting to trigger
func (o *RemediationObserver) WaitForRateLimitHit(ctx context.Context, timeout time.Duration) error {
	config := &utils.RetryConfig{
		Interval:    3 * time.Second,
		Timeout:     timeout,
		Description: "rate limit to trigger",
	}

	return utils.WaitFor(ctx, config, func() (bool, error) {
		logs, err := utils.GetNodeDoctorLogs(ctx, o.kubeContext)
		if err != nil {
			utils.LogDebug("Log retrieval error: %v", err)
			return false, nil
		}

		// Look for rate limit messages
		return strings.Contains(logs, "rate limit exceeded") ||
			strings.Contains(logs, "rate limited") ||
			strings.Contains(logs, "RateLimitExceeded"), nil
	})
}

// WaitForCooldownActive waits for cooldown period to be active
func (o *RemediationObserver) WaitForCooldownActive(ctx context.Context, timeout time.Duration) error {
	config := &utils.RetryConfig{
		Interval:    3 * time.Second,
		Timeout:     timeout,
		Description: "cooldown period to activate",
	}

	return utils.WaitFor(ctx, config, func() (bool, error) {
		logs, err := utils.GetNodeDoctorLogs(ctx, o.kubeContext)
		if err != nil {
			utils.LogDebug("Log retrieval error: %v", err)
			return false, nil
		}

		// Look for cooldown messages
		return strings.Contains(logs, "cooldown period active") ||
			strings.Contains(logs, "in cooldown") ||
			strings.Contains(logs, "cooldown not expired"), nil
	})
}

// GetRemediationCount counts how many remediations were attempted
func (o *RemediationObserver) GetRemediationCount(ctx context.Context) (int, error) {
	logs, err := utils.GetNodeDoctorLogs(ctx, o.kubeContext)
	if err != nil {
		return 0, err
	}

	// Count occurrences of remediation attempt indicators
	count := 0
	keywords := []string{
		"attempting remediation",
		"remediation executed",
		"executing remediation",
	}

	for _, keyword := range keywords {
		count += strings.Count(strings.ToLower(logs), strings.ToLower(keyword))
	}

	return count, nil
}

// containsRemediationKeywords checks if logs contain remediation indicators
func (o *RemediationObserver) containsRemediationKeywords(logs string) bool {
	keywords := []string{
		"attempting remediation",
		"remediation executed",
		"executing remediation",
		"remediation action",
		"restarting kubelet",
		"cleaning disk",
		"killing process",
	}

	logsLower := strings.ToLower(logs)
	for _, keyword := range keywords {
		if strings.Contains(logsLower, keyword) {
			return true
		}
	}

	return false
}

// containsSuccessKeywords checks if logs contain success indicators
func (o *RemediationObserver) containsSuccessKeywords(logs string) bool {
	keywords := []string{
		"remediation succeeded",
		"remediation successful",
		"remediation completed",
		"successfully restarted",
		"successfully cleaned",
	}

	logsLower := strings.ToLower(logs)
	for _, keyword := range keywords {
		if strings.Contains(logsLower, keyword) {
			return true
		}
	}

	return false
}

// containsFailureKeywords checks if logs contain failure indicators
func (o *RemediationObserver) containsFailureKeywords(logs string) bool {
	keywords := []string{
		"remediation failed",
		"remediation error",
		"failed to remediate",
		"remediation unsuccessful",
	}

	logsLower := strings.ToLower(logs)
	for _, keyword := range keywords {
		if strings.Contains(logsLower, keyword) {
			return true
		}
	}

	return false
}
