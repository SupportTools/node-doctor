// Copyright 2025 Support Tools Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package kubernetes

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// BenchmarkKubeletMonitor_Check_SingleThreaded measures baseline Check() performance
// without concurrency to establish performance baseline.
func BenchmarkKubeletMonitor_Check_SingleThreaded(b *testing.B) {
	ctx := context.Background()

	mockClient := &mockBenchKubeletClient{
		healthOK:  true,
		systemdOK: true,
		plegOK:    true,
	}

	monitorConfig := types.MonitorConfig{
		Name:     "kubelet-benchmark",
		Type:     "kubernetes-kubelet-check",
		Enabled:  true,
		Interval: 30 * time.Second,
		Timeout:  5 * time.Second,
		Config: map[string]interface{}{
			"healthzURL":         "http://127.0.0.1:10248/healthz",
			"metricsURL":         "http://127.0.0.1:10250/metrics",
			"checkSystemdStatus": true,
			"checkPLEG":          true,
			"plegThreshold":      "5s",
			"failureThreshold":   3,
			"httpTimeout":        "5s",
		},
	}

	monitor, err := NewKubeletMonitorWithMetrics(ctx, monitorConfig, nil)
	if err != nil {
		b.Fatalf("Failed to create monitor: %v", err)
	}

	kubeletMon := monitor.(*KubeletMonitor)
	kubeletMon.client = mockClient

	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := kubeletMon.checkKubelet(checkCtx)
		if err != nil {
			b.Errorf("Check failed: %v", err)
		}
	}
}

// BenchmarkKubeletMonitor_Check_Parallel measures Check() performance under
// concurrent load to identify mutex contention and scaling behavior.
func BenchmarkKubeletMonitor_Check_Parallel(b *testing.B) {
	ctx := context.Background()

	mockClient := &mockBenchKubeletClient{
		healthOK:  true,
		systemdOK: true,
		plegOK:    true,
	}

	monitorConfig := types.MonitorConfig{
		Name:     "kubelet-benchmark",
		Type:     "kubernetes-kubelet-check",
		Enabled:  true,
		Interval: 30 * time.Second,
		Timeout:  5 * time.Second,
		Config: map[string]interface{}{
			"healthzURL":         "http://127.0.0.1:10248/healthz",
			"metricsURL":         "http://127.0.0.1:10250/metrics",
			"checkSystemdStatus": true,
			"checkPLEG":          true,
			"plegThreshold":      "5s",
			"failureThreshold":   3,
			"httpTimeout":        "5s",
		},
	}

	monitor, err := NewKubeletMonitorWithMetrics(ctx, monitorConfig, nil)
	if err != nil {
		b.Fatalf("Failed to create monitor: %v", err)
	}

	kubeletMon := monitor.(*KubeletMonitor)
	kubeletMon.client = mockClient

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		for pb.Next() {
			_, err := kubeletMon.checkKubelet(checkCtx)
			if err != nil {
				b.Errorf("Check failed: %v", err)
			}
		}
	})
}

// BenchmarkKubeletMonitor_FailureTracking measures the performance of
// mutex-protected failure counter updates under load.
func BenchmarkKubeletMonitor_FailureTracking(b *testing.B) {
	ctx := context.Background()

	mockClient := &mockBenchKubeletClient{
		healthOK:  false, // Trigger failures
		systemdOK: true,
		plegOK:    true,
	}

	monitorConfig := types.MonitorConfig{
		Name:     "kubelet-benchmark",
		Type:     "kubernetes-kubelet-check",
		Enabled:  true,
		Interval: 30 * time.Second,
		Timeout:  5 * time.Second,
		Config: map[string]interface{}{
			"healthzURL":         "http://127.0.0.1:10248/healthz",
			"metricsURL":         "http://127.0.0.1:10250/metrics",
			"checkSystemdStatus": true,
			"checkPLEG":          true,
			"plegThreshold":      "5s",
			"failureThreshold":   1000, // High threshold to avoid triggering alerts
			"httpTimeout":        "5s",
		},
	}

	monitor, err := NewKubeletMonitorWithMetrics(ctx, monitorConfig, nil)
	if err != nil {
		b.Fatalf("Failed to create monitor: %v", err)
	}

	kubeletMon := monitor.(*KubeletMonitor)
	kubeletMon.client = mockClient

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		for pb.Next() {
			// This will increment consecutiveFailures (mutex-protected)
			kubeletMon.checkKubelet(checkCtx)
		}
	})

	b.StopTimer()

	// Report final failure count for verification
	kubeletMon.mu.Lock()
	finalFailures := kubeletMon.consecutiveFailures
	kubeletMon.mu.Unlock()

	b.Logf("Final consecutive failures: %d (from %d iterations)", finalFailures, b.N)
}

// BenchmarkKubeletMonitor_FailureRecovery measures the performance of
// alternating failure/recovery cycles.
func BenchmarkKubeletMonitor_FailureRecovery(b *testing.B) {
	ctx := context.Background()

	mockClient := &mockBenchKubeletClient{
		healthOK:  true,
		systemdOK: true,
		plegOK:    true,
	}

	monitorConfig := types.MonitorConfig{
		Name:     "kubelet-benchmark",
		Type:     "kubernetes-kubelet-check",
		Enabled:  true,
		Interval: 30 * time.Second,
		Timeout:  5 * time.Second,
		Config: map[string]interface{}{
			"healthzURL":         "http://127.0.0.1:10248/healthz",
			"metricsURL":         "http://127.0.0.1:10250/metrics",
			"checkSystemdStatus": true,
			"checkPLEG":          true,
			"plegThreshold":      "5s",
			"failureThreshold":   3,
			"httpTimeout":        "5s",
		},
	}

	monitor, err := NewKubeletMonitorWithMetrics(ctx, monitorConfig, nil)
	if err != nil {
		b.Fatalf("Failed to create monitor: %v", err)
	}

	kubeletMon := monitor.(*KubeletMonitor)
	kubeletMon.client = mockClient

	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Alternate between failure and recovery
		if i%2 == 0 {
			mockClient.setHealth(false) // Fail
		} else {
			mockClient.setHealth(true) // Recover
		}

		kubeletMon.checkKubelet(checkCtx)
	}
}

// BenchmarkKubeletMonitor_CircuitBreaker measures circuit breaker operation
// performance under load.
func BenchmarkKubeletMonitor_CircuitBreaker(b *testing.B) {
	ctx := context.Background()

	mockClient := &mockBenchKubeletClient{
		healthOK:  false, // All checks fail to trigger circuit breaker
		systemdOK: true,
		plegOK:    true,
	}

	monitorConfig := types.MonitorConfig{
		Name:     "kubelet-benchmark",
		Type:     "kubernetes-kubelet-check",
		Enabled:  true,
		Interval: 30 * time.Second,
		Timeout:  5 * time.Second,
		Config: map[string]interface{}{
			"healthzURL":         "http://127.0.0.1:10248/healthz",
			"metricsURL":         "http://127.0.0.1:10250/metrics",
			"checkSystemdStatus": true,
			"checkPLEG":          true,
			"plegThreshold":      "5s",
			"failureThreshold":   3,
			"httpTimeout":        "5s",
			"circuitBreaker": map[string]interface{}{
				"enabled":               true,
				"failureThreshold":      5,
				"openTimeout":           "1s",
				"halfOpenMaxRequests":   3,
				"useExponentialBackoff": false,
				"maxBackoffTimeout":     "5m",
			},
		},
	}

	monitor, err := NewKubeletMonitorWithMetrics(ctx, monitorConfig, nil)
	if err != nil {
		b.Fatalf("Failed to create monitor: %v", err)
	}

	kubeletMon := monitor.(*KubeletMonitor)
	kubeletMon.client = mockClient

	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		kubeletMon.checkKubelet(checkCtx)
	}
}

// BenchmarkKubeletMonitor_StartStop measures the performance of
// starting and stopping monitors repeatedly.
func BenchmarkKubeletMonitor_StartStop(b *testing.B) {
	ctx := context.Background()

	mockClient := &mockBenchKubeletClient{
		healthOK:  true,
		systemdOK: true,
		plegOK:    true,
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		monitorConfig := types.MonitorConfig{
			Name:     fmt.Sprintf("kubelet-benchmark-%d", i),
			Type:     "kubernetes-kubelet-check",
			Enabled:  true,
			Interval: 100 * time.Millisecond, // Fast for benchmark
			Timeout:  50 * time.Millisecond,  // Must be less than interval
			Config: map[string]interface{}{
				"healthzURL":         "http://127.0.0.1:10248/healthz",
				"metricsURL":         "http://127.0.0.1:10250/metrics",
				"checkSystemdStatus": true,
				"checkPLEG":          true,
				"plegThreshold":      "5s",
				"failureThreshold":   3,
				"httpTimeout":        "5s",
			},
		}

		monitor, err := NewKubeletMonitorWithMetrics(ctx, monitorConfig, nil)
		if err != nil {
			b.Fatalf("Failed to create monitor: %v", err)
		}

		kubeletMon := monitor.(*KubeletMonitor)
		kubeletMon.client = mockClient

		// Start monitor
		_, err = kubeletMon.Start()
		if err != nil {
			b.Fatalf("Failed to start monitor: %v", err)
		}

		// Let it run briefly
		time.Sleep(10 * time.Millisecond)

		// Stop monitor
		kubeletMon.Stop()
	}
}

// BenchmarkKubeletMonitor_MultiInstance measures performance with multiple
// monitor instances running concurrently (simulates DaemonSet deployment).
func BenchmarkKubeletMonitor_MultiInstance(b *testing.B) {
	ctx := context.Background()

	const numInstances = 10

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		monitors := make([]*KubeletMonitor, numInstances)
		var wg sync.WaitGroup

		// Start all instances
		for j := 0; j < numInstances; j++ {
			mockClient := &mockBenchKubeletClient{
				healthOK:  true,
				systemdOK: true,
				plegOK:    true,
			}

			monitorConfig := types.MonitorConfig{
				Name:     fmt.Sprintf("kubelet-bench-%d-%d", i, j),
				Type:     "kubernetes-kubelet-check",
				Enabled:  true,
				Interval: 100 * time.Millisecond,
				Timeout:  50 * time.Millisecond, // Must be less than interval
				Config: map[string]interface{}{
					"healthzURL":         "http://127.0.0.1:10248/healthz",
					"metricsURL":         "http://127.0.0.1:10250/metrics",
					"checkSystemdStatus": true,
					"checkPLEG":          true,
					"plegThreshold":      "5s",
					"failureThreshold":   3,
					"httpTimeout":        "5s",
				},
			}

			monitor, err := NewKubeletMonitorWithMetrics(ctx, monitorConfig, nil)
			if err != nil {
				b.Fatalf("Failed to create monitor %d: %v", j, err)
			}

			kubeletMon := monitor.(*KubeletMonitor)
			kubeletMon.client = mockClient
			monitors[j] = kubeletMon

			wg.Add(1)
			go func(m *KubeletMonitor) {
				defer wg.Done()
				if _, err := m.Start(); err != nil {
					b.Errorf("Failed to start monitor: %v", err)
				}
			}(kubeletMon)
		}

		wg.Wait()

		// Let them run briefly
		time.Sleep(50 * time.Millisecond)

		// Stop all instances
		for j := 0; j < numInstances; j++ {
			wg.Add(1)
			go func(m *KubeletMonitor) {
				defer wg.Done()
				m.Stop()
			}(monitors[j])
		}

		wg.Wait()
	}
}

// mockBenchKubeletClient is a thread-safe mock for benchmarking
type mockBenchKubeletClient struct {
	mu        sync.RWMutex
	healthOK  bool
	systemdOK bool
	plegOK    bool
}

func (m *mockBenchKubeletClient) CheckHealth(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.healthOK {
		return fmt.Errorf("mock health check failed")
	}
	return nil
}

func (m *mockBenchKubeletClient) CheckSystemdStatus(ctx context.Context) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.systemdOK, nil
}

func (m *mockBenchKubeletClient) GetMetrics(ctx context.Context) (*KubeletMetrics, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.plegOK {
		return nil, fmt.Errorf("mock PLEG check failed")
	}

	return &KubeletMetrics{
		PLEGRelistDuration: 0.001, // 1ms
	}, nil
}

func (m *mockBenchKubeletClient) setHealth(healthy bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.healthOK = healthy
}

func (m *mockBenchKubeletClient) setSystemd(ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.systemdOK = ok
}

func (m *mockBenchKubeletClient) setPLEG(ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.plegOK = ok
}
