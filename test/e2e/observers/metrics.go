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
	"strconv"
	"strings"
	"time"

	"github.com/supporttools/node-doctor/test/e2e/utils"
)

// MetricsObserver watches for Prometheus metrics
type MetricsObserver struct {
	kubeContext string
	namespace   string
	podSelector string
	metricsPort int
}

// NewMetricsObserver creates a new metrics observer
// podSelector: e.g., "app=node-doctor"
// metricsPort: e.g., 9101 (Node Doctor Prometheus exporter port)
func NewMetricsObserver(kubeContext, namespace, podSelector string, metricsPort int) *MetricsObserver {
	return &MetricsObserver{
		kubeContext: kubeContext,
		namespace:   namespace,
		podSelector: podSelector,
		metricsPort: metricsPort,
	}
}

// WaitForMetric waits for a specific metric to appear
func (o *MetricsObserver) WaitForMetric(ctx context.Context, metricName string, timeout time.Duration) error {
	config := &utils.RetryConfig{
		Interval:    5 * time.Second,
		Timeout:     timeout,
		Description: fmt.Sprintf("metric %s to appear", metricName),
	}

	return utils.WaitFor(ctx, config, func() (bool, error) {
		exists, err := o.MetricExists(ctx, metricName)
		if err != nil {
			utils.LogDebug("Metric check error: %v", err)
			return false, nil // Retry
		}
		return exists, nil
	})
}

// MetricExists checks if a metric exists in the metrics endpoint
func (o *MetricsObserver) MetricExists(ctx context.Context, metricName string) (bool, error) {
	metrics, err := o.GetMetrics(ctx)
	if err != nil {
		return false, err
	}

	// Simple check: does the metric name appear in the output?
	return strings.Contains(metrics, metricName), nil
}

// GetMetrics retrieves all metrics from the Prometheus endpoint
func (o *MetricsObserver) GetMetrics(ctx context.Context) (string, error) {
	// Get pod name
	podName, err := o.getPodName(ctx)
	if err != nil {
		return "", err
	}

	// Use kubectl exec to curl the metrics endpoint
	// This avoids needing port-forward complexity
	cmd := exec.CommandContext(ctx, "kubectl",
		"--context", o.kubeContext,
		"-n", o.namespace,
		"exec", podName,
		"--",
		"wget", "-q", "-O", "-",
		fmt.Sprintf("http://localhost:%d/metrics", o.metricsPort))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get metrics: %w (output: %s)", err, string(output))
	}

	return string(output), nil
}

// GetMetricValue retrieves the numeric value of a metric
// For simplicity, this assumes a simple gauge/counter metric without labels
func (o *MetricsObserver) GetMetricValue(ctx context.Context, metricName string) (float64, error) {
	metrics, err := o.GetMetrics(ctx)
	if err != nil {
		return 0, err
	}

	// Parse metric value from output
	// Format: metric_name <value>
	lines := strings.Split(metrics, "\n")
	for _, line := range lines {
		// Skip comments and empty lines
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}

		// Check if line starts with metric name
		if strings.HasPrefix(line, metricName) {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				value, err := strconv.ParseFloat(parts[len(parts)-1], 64)
				if err != nil {
					return 0, fmt.Errorf("failed to parse metric value: %w", err)
				}
				return value, nil
			}
		}
	}

	return 0, fmt.Errorf("metric %s not found", metricName)
}

// WaitForMetricValue waits for a metric to reach a specific value
func (o *MetricsObserver) WaitForMetricValue(ctx context.Context, metricName string, expectedValue float64, timeout time.Duration) error {
	config := &utils.RetryConfig{
		Interval:    5 * time.Second,
		Timeout:     timeout,
		Description: fmt.Sprintf("metric %s to have value %v", metricName, expectedValue),
	}

	return utils.WaitFor(ctx, config, func() (bool, error) {
		value, err := o.GetMetricValue(ctx, metricName)
		if err != nil {
			utils.LogDebug("Metric value check error: %v", err)
			return false, nil // Retry
		}

		return value == expectedValue, nil
	})
}

// WaitForMetricGreaterThan waits for a metric to exceed a threshold
func (o *MetricsObserver) WaitForMetricGreaterThan(ctx context.Context, metricName string, threshold float64, timeout time.Duration) error {
	config := &utils.RetryConfig{
		Interval:    5 * time.Second,
		Timeout:     timeout,
		Description: fmt.Sprintf("metric %s to exceed %v", metricName, threshold),
	}

	return utils.WaitFor(ctx, config, func() (bool, error) {
		value, err := o.GetMetricValue(ctx, metricName)
		if err != nil {
			utils.LogDebug("Metric threshold check error: %v", err)
			return false, nil // Retry
		}

		return value > threshold, nil
	})
}

// getPodName returns the first pod matching the selector
func (o *MetricsObserver) getPodName(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "kubectl",
		"--context", o.kubeContext,
		"-n", o.namespace,
		"get", "pods",
		"-l", o.podSelector,
		"-o", "jsonpath={.items[0].metadata.name}")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get pod name: %w (output: %s)", err, string(output))
	}

	podName := strings.TrimSpace(string(output))
	if podName == "" {
		return "", fmt.Errorf("no pods found with selector %s", o.podSelector)
	}

	return podName, nil
}

// GetMetricWithLabels retrieves a metric value with specific label values
// This is more complex and would require full Prometheus parsing
// For now, returning a simple implementation
func (o *MetricsObserver) GetMetricWithLabels(ctx context.Context, metricName string, labels map[string]string) (float64, error) {
	metrics, err := o.GetMetrics(ctx)
	if err != nil {
		return 0, err
	}

	// Build label matcher string
	// Format: metric_name{label1="value1",label2="value2"}
	labelStrs := []string{}
	for k, v := range labels {
		labelStrs = append(labelStrs, fmt.Sprintf("%s=\"%s\"", k, v))
	}
	labelMatcher := strings.Join(labelStrs, ",")
	searchStr := fmt.Sprintf("%s{%s}", metricName, labelMatcher)

	lines := strings.Split(metrics, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, metricName) && strings.Contains(line, labelMatcher) {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				value, err := strconv.ParseFloat(parts[len(parts)-1], 64)
				if err != nil {
					return 0, fmt.Errorf("failed to parse metric value: %w", err)
				}
				return value, nil
			}
		}
	}

	return 0, fmt.Errorf("metric %s not found", searchStr)
}
