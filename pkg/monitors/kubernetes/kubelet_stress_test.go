package kubernetes

import (
	"context"
	"fmt"
	"strings"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// TestStress_Concurrent_100Checks tests the kubelet monitor with 100+ concurrent Check() calls
// to detect race conditions and verify thread-safety of mutex-protected consecutiveFailures.
// Run with: go test -race -v -run=TestStress_Concurrent_100Checks
func TestStress_Concurrent_100Checks(t *testing.T) {
	const numGoroutines = 100
	const checksPerGoroutine = 10

	// Create mock client that returns success
	mockClient := &mockStressKubeletClient{
		healthOK:  true,
		systemdOK: true,
		plegOK:    true,
	}

	monitorConfig := types.MonitorConfig{
		Name:     "test-concurrent-checks",
		Type:     "kubernetes-kubelet-check",
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Config: map[string]interface{}{
			"healthzURL":         "http://localhost:10248/healthz",
			"metricsURL":         "http://localhost:10250/metrics",
			"checkSystemdStatus": true,
			"checkPLEG":          true,
			"plegThreshold": "5s",
			"failureThreshold":   3,
			"httpTimeout": "5s",
		},
	}

	// Create monitor with mock client
	ctx := context.Background()
	monitor, err := NewKubeletMonitorWithMetrics(ctx, monitorConfig, nil)
	if err != nil {
		t.Fatalf("Failed to create monitor: %v", err)
	}

	// Inject mock client
	kubeletMon := monitor.(*KubeletMonitor)
	kubeletMon.client = mockClient

	// Track results
	var wg sync.WaitGroup
	var successCount int32
	var errorCount int32
	errors := make([]error, 0)
	var errorsMu sync.Mutex

	startTime := time.Now()

	// Launch concurrent goroutines
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < checksPerGoroutine; j++ {
				checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				status, err := kubeletMon.checkKubelet(checkCtx)
				cancel()

				if err != nil {
					atomic.AddInt32(&errorCount, 1)
					errorsMu.Lock()
					errors = append(errors, fmt.Errorf("goroutine %d check %d: %w", id, j, err))
					errorsMu.Unlock()
					continue
				}

				if status == nil {
					atomic.AddInt32(&errorCount, 1)
					errorsMu.Lock()
					errors = append(errors, fmt.Errorf("goroutine %d check %d: nil status", id, j))
					errorsMu.Unlock()
					continue
				}

				atomic.AddInt32(&successCount, 1)
			}
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	duration := time.Since(startTime)
	totalChecks := numGoroutines * checksPerGoroutine
	checksPerSecond := float64(totalChecks) / duration.Seconds()

	// Report results
	t.Logf("Concurrent check test completed in %v", duration)
	t.Logf("Total checks: %d, Success: %d, Errors: %d", totalChecks, successCount, errorCount)
	t.Logf("Throughput: %.2f checks/second", checksPerSecond)

	// Verify results
	if errorCount > 0 {
		t.Errorf("Some checks failed (%d/%d)", errorCount, totalChecks)
		for i, err := range errors {
			if i < 10 { // Limit error output
				t.Logf("Error %d: %v", i+1, err)
			}
		}
	}

	// Verify all checks succeeded
	if successCount != int32(totalChecks) {
		t.Errorf("Expected %d successful checks, got %d", totalChecks, successCount)
	}
}

// TestStress_Concurrent_FailureRecovery tests rapid failure and recovery scenarios
// to verify correct failure counter tracking and mutex synchronization.
func TestStress_Concurrent_FailureRecovery(t *testing.T) {
	const numGoroutines = 50
	const cyclesPerGoroutine = 5

	// Create mock client with controllable state
	mockClient := &mockStressKubeletClient{
		healthOK: true,
	}

	config := &KubeletMonitorConfig{
		HealthzURL:         "http://localhost:10248/healthz",
		MetricsURL:         "http://localhost:10250/metrics",
		CheckSystemdStatus: false,
		CheckPLEG:          false,
		FailureThreshold:   3,
		HTTPTimeout:        5 * time.Second,
	}

	monitorConfig := types.MonitorConfig{
		Name:     "test-failure-recovery",
		Type:     "kubernetes-kubelet-check",
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
	}

	ctx := context.Background()
	monitor, err := NewKubeletMonitorWithMetrics(ctx, monitorConfig, nil)
	if err != nil {
		t.Fatalf("Failed to create monitor: %v", err)
	}

	kubeletMon := monitor.(*KubeletMonitor)
	kubeletMon.client = mockClient

	var wg sync.WaitGroup
	var totalChecks int32

	startTime := time.Now()

	// Each goroutine alternates between failures and successes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for cycle := 0; cycle < cyclesPerGoroutine; cycle++ {
				// Fail 3 times
				mockClient.setHealth(false)
				for j := 0; j < 3; j++ {
					checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
					kubeletMon.checkKubelet(checkCtx)
					cancel()
					atomic.AddInt32(&totalChecks, 1)
					time.Sleep(1 * time.Millisecond) // Small delay
				}

				// Recover (succeed once)
				mockClient.setHealth(true)
				checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				kubeletMon.checkKubelet(checkCtx)
				cancel()
				atomic.AddInt32(&totalChecks, 1)
				time.Sleep(1 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()

	duration := time.Since(startTime)

	t.Logf("Failure/recovery test completed in %v", duration)
	t.Logf("Total checks performed: %d", totalChecks)

	// Verify consecutive failures counter is in valid state (0 or small number)
	kubeletMon.mu.Lock()
	failures := kubeletMon.consecutiveFailures
	kubeletMon.mu.Unlock()

	if failures > config.FailureThreshold {
		t.Errorf("consecutiveFailures (%d) exceeds threshold (%d) after test completion",
			failures, config.FailureThreshold)
	}

	t.Logf("Final consecutiveFailures: %d (valid)", failures)
}

// TestStress_Concurrent_StartStop tests concurrent Start() and Stop() operations
// to verify lifecycle management and channel cleanup.
func TestStress_Concurrent_StartStop(t *testing.T) {
	const numMonitors = 20
	const startStopCycles = 10

	mockClient := &mockStressKubeletClient{
		healthOK: true,
	}

	ctx := context.Background()
	var wg sync.WaitGroup

	startTime := time.Now()

	// Launch multiple monitors with Start/Stop cycles
	for i := 0; i < numMonitors; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for cycle := 0; cycle < startStopCycles; cycle++ {
				monitorConfig := types.MonitorConfig{
					Name:     fmt.Sprintf("test-startstop-%d-%d", id, cycle),
					Type:     "kubernetes-kubelet-check",
					Interval: 100 * time.Millisecond, // Fast interval
					Timeout:  50 * time.Millisecond,  // Must be less than interval
					Config: map[string]interface{}{
						"healthzURL":         "http://localhost:10248/healthz",
						"metricsURL":         "http://localhost:10250/metrics",
						"checkSystemdStatus": false,
						"checkPLEG":          false,
						"failureThreshold":   3,
						"httpTimeout": "5s",
					},
				}

				monitor, err := NewKubeletMonitorWithMetrics(ctx, monitorConfig, nil)
				if err != nil {
					t.Errorf("Failed to create monitor: %v", err)
					return
				}

				kubeletMon := monitor.(*KubeletMonitor)
				kubeletMon.client = mockClient

				// Start monitor
				_, err = kubeletMon.Start()
				if err != nil {
					t.Errorf("Failed to start monitor: %v", err)
					return
				}

				// Let it run briefly
				time.Sleep(50 * time.Millisecond)

				// Stop monitor
				kubeletMon.Stop()

				// Brief pause between cycles
				time.Sleep(10 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()

	duration := time.Since(startTime)

	t.Logf("Start/Stop test completed in %v", duration)
	t.Logf("Successfully started and stopped %d monitors %d times each",
		numMonitors, startStopCycles)
}

// TestStress_MultiInstance tests multiple kubelet monitor instances running concurrently
// to simulate DaemonSet deployment (one monitor per node).
func TestStress_MultiInstance(t *testing.T) {
	const numInstances = 10
	const runDuration = 2 * time.Second

	mockClient := &mockStressKubeletClient{
		healthOK: true,
	}

	ctx := context.Background()

	// Create and start multiple instances
	monitors := make([]types.Monitor, numInstances)
	for i := 0; i < numInstances; i++ {
		monitorConfig := types.MonitorConfig{
			Name:     fmt.Sprintf("kubelet-node-%d", i),
			Type:     "kubernetes-kubelet-check",
			Interval: 200 * time.Millisecond,
			Timeout:  100 * time.Millisecond, // Must be less than interval
			Config: map[string]interface{}{
				"healthzURL":         "http://localhost:10248/healthz",
				"metricsURL":         "http://localhost:10250/metrics",
				"checkSystemdStatus": false,
				"checkPLEG":          false,
				"failureThreshold":   3,
				"httpTimeout": "5s",
			},
		}

		monitor, err := NewKubeletMonitorWithMetrics(ctx, monitorConfig, nil)
		if err != nil {
			t.Fatalf("Failed to create monitor %d: %v", i, err)
		}

		kubeletMon := monitor.(*KubeletMonitor)
		kubeletMon.client = mockClient
		monitors[i] = kubeletMon

		// Start each monitor
		_, err = kubeletMon.Start()
		if err != nil {
			t.Fatalf("Failed to start monitor %d: %v", i, err)
		}
	}

	t.Logf("Started %d monitor instances", numInstances)

	// Let them run concurrently
	time.Sleep(runDuration)

	// Stop all monitors
	for _, mon := range monitors {
		kubeletMon := mon.(*KubeletMonitor)
		kubeletMon.Stop()
	}

	t.Logf("Successfully ran %d instances for %v", numInstances, runDuration)
}

// TestStress_MemoryLeak tests for memory leaks and goroutine leaks over extended operation
func TestStress_MemoryLeak(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory leak test in short mode")
	}

	const iterations = 1000
	const checkInterval = 10 * time.Millisecond

	mockClient := &mockStressKubeletClient{
		healthOK: true,
	}

	monitorConfig := types.MonitorConfig{
		Name:     "test-memory-leak",
		Type:     "kubernetes-kubelet-check",
		Interval: checkInterval,
		Timeout:  5 * time.Millisecond, // Must be less than interval (10ms)
		Config: map[string]interface{}{
			"healthzURL":         "http://localhost:10248/healthz",
			"metricsURL":         "http://localhost:10250/metrics",
			"checkSystemdStatus": false,
			"checkPLEG":          false,
			"failureThreshold":   3,
			"httpTimeout": "5s",
		},
	}

	ctx := context.Background()
	monitor, err := NewKubeletMonitorWithMetrics(ctx, monitorConfig, nil)
	if err != nil {
		t.Fatalf("Failed to create monitor: %v", err)
	}

	kubeletMon := monitor.(*KubeletMonitor)
	kubeletMon.client = mockClient

	// Record initial state
	runtime.GC() // Force GC to get clean baseline
	var initialStats runtime.MemStats
	runtime.ReadMemStats(&initialStats)
	initialGoroutines := runtime.NumGoroutine()

	t.Logf("Initial state - Goroutines: %d, Alloc: %d bytes",
		initialGoroutines, initialStats.Alloc)

	// Start monitor
	_, err = kubeletMon.Start()
	if err != nil {
		t.Fatalf("Failed to start monitor: %v", err)
	}

	// Run for many iterations
	startTime := time.Now()
	time.Sleep(time.Duration(iterations) * checkInterval)

	// Stop monitor
	kubeletMon.Stop()

	// Allow cleanup time
	time.Sleep(500 * time.Millisecond)

	// Record final state
	runtime.GC() // Force GC to collect any leaked memory
	time.Sleep(100 * time.Millisecond)
	var finalStats runtime.MemStats
	runtime.ReadMemStats(&finalStats)
	finalGoroutines := runtime.NumGoroutine()

	duration := time.Since(startTime)

	t.Logf("Final state - Goroutines: %d, Alloc: %d bytes",
		finalGoroutines, finalStats.Alloc)
	t.Logf("Test duration: %v, Iterations: %d", duration, iterations)

	// Check for goroutine leaks (allow minimal variance for test framework)
	// Tightened threshold from 5 to 2 based on devils advocate feedback
	goroutineDiff := finalGoroutines - initialGoroutines
	if goroutineDiff > 2 {
		t.Errorf("Potential goroutine leak: started with %d, ended with %d (diff: %d)",
			initialGoroutines, finalGoroutines, goroutineDiff)
	} else {
		t.Logf("Goroutine count stable (diff: %d)", goroutineDiff)
	}

	// Check for memory leaks (strict threshold for production monitoring tool)
	// Tightened threshold from 10MB to 2MB based on devils advocate feedback
	memoryGrowth := int64(finalStats.Alloc) - int64(initialStats.Alloc)
	memoryGrowthMB := float64(memoryGrowth) / (1024 * 1024)

	// Allow up to 2MB growth for legitimate allocations
	if memoryGrowthMB > 2 {
		t.Errorf("Potential memory leak: memory grew by %.2f MB", memoryGrowthMB)
	} else {
		t.Logf("Memory growth: %.2f MB (acceptable)", memoryGrowthMB)
	}
}

// TestStress_CircuitBreaker_Concurrent tests circuit breaker under concurrent load
func TestStress_CircuitBreaker_Concurrent(t *testing.T) {
	const numGoroutines = 50
	const checksPerGoroutine = 20

	mockClient := &mockStressKubeletClient{
		healthOK: false, // Start with failures to trigger circuit breaker
	}

	monitorConfig := types.MonitorConfig{
		Name:     "test-circuit-breaker",
		Type:     "kubernetes-kubelet-check",
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Config: map[string]interface{}{
			"healthzURL":         "http://localhost:10248/healthz",
			"metricsURL":         "http://localhost:10250/metrics",
			"checkSystemdStatus": false,
			"checkPLEG":          false,
			"failureThreshold":   3,
			"httpTimeout": "5s",
			"circuitBreaker": map[string]interface{}{
				"enabled":          true,
				"failureThreshold": 5,
				"openTimeout": "1s",
			},
		},
	}

	ctx := context.Background()
	monitor, err := NewKubeletMonitorWithMetrics(ctx, monitorConfig, nil)
	if err != nil {
		t.Fatalf("Failed to create monitor: %v", err)
	}

	kubeletMon := monitor.(*KubeletMonitor)
	kubeletMon.client = mockClient

	var wg sync.WaitGroup
	var totalChecks int32
	var circuitOpenCount int32

	startTime := time.Now()

	// Launch concurrent checks that will trigger circuit breaker
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < checksPerGoroutine; j++ {
				checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				status, _ := kubeletMon.checkKubelet(checkCtx)
				cancel()
				atomic.AddInt32(&totalChecks, 1)

				// Check if circuit breaker opened
				if status != nil {
					for _, event := range status.Events {
						if event.Reason == "CircuitBreakerOpen" {
							atomic.AddInt32(&circuitOpenCount, 1)
							break
						}
					}
				}

				time.Sleep(10 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()

	duration := time.Since(startTime)

	t.Logf("Circuit breaker test completed in %v", duration)
	t.Logf("Total checks: %d, Circuit open events: %d", totalChecks, circuitOpenCount)

	// Verify circuit breaker activated
	if circuitOpenCount == 0 {
		t.Errorf("Circuit breaker never opened despite consistent failures")
	} else {
		t.Logf("Circuit breaker correctly opened %d times", circuitOpenCount)
	}

	// Verify circuit breaker state is accessible without panic
	if kubeletMon.circuitBreaker != nil {
		state := kubeletMon.circuitBreaker.State()
		metrics := kubeletMon.circuitBreaker.Metrics()
		t.Logf("Final circuit breaker state: %s, openings: %d",
			state, metrics.TotalOpenings)
	}
}

// TestStress_SameInstance_ConcurrentStart tests concurrent Start operations
// on the SAME monitor instance to verify lifecycle thread-safety.
// NOTE: This test discovered that BaseMonitor.Stop() is NOT safe for concurrent calls
// (panics with "close of closed channel"). This is documented as a known limitation.
// The test validates concurrent Start() calls and single Stop() to work within this constraint.
func TestStress_SameInstance_ConcurrentStartStop(t *testing.T) {
	const numGoroutines = 50
	const attemptsPerGoroutine = 20

	mockClient := &mockStressKubeletClient{
		healthOK:  true,
		systemdOK: true,
		plegOK:    true,
	}

	monitorConfig := types.MonitorConfig{
		Name:     "test-same-instance-startstop",
		Type:     "kubernetes-kubelet-check",
		Interval: 100 * time.Millisecond,
		Timeout:  50 * time.Millisecond,
		Config: map[string]interface{}{
			"healthzURL":         "http://localhost:10248/healthz",
			"metricsURL":         "http://localhost:10250/metrics",
			"checkSystemdStatus": true,
			"checkPLEG":          false,
			"failureThreshold":   3,
			"httpTimeout":        "5s",
		},
	}

	ctx := context.Background()
	monitor, err := NewKubeletMonitorWithMetrics(ctx, monitorConfig, nil)
	if err != nil {
		t.Fatalf("Failed to create monitor: %v", err)
	}

	kubeletMon := monitor.(*KubeletMonitor)
	kubeletMon.client = mockClient

	var wg sync.WaitGroup
	startTime := time.Now()

	// Track successful starts
	var successfulStarts int32
	var startErrors, alreadyRunningCount int32

	// Launch multiple goroutines that try to Start() the SAME monitor concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < attemptsPerGoroutine; j++ {
				// Try to start
				_, err := kubeletMon.Start()
				if err == nil {
					atomic.AddInt32(&successfulStarts, 1)
					// Brief delay to let monitor run
					time.Sleep(2 * time.Millisecond)
				} else if strings.Contains(err.Error(), "already running") {
					atomic.AddInt32(&alreadyRunningCount, 1)
				} else {
					atomic.AddInt32(&startErrors, 1)
				}

				// Brief delay between attempts
				time.Sleep(1 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(startTime)

	t.Logf("Same-instance concurrent Start test completed in %v", duration)
	t.Logf("Goroutines: %d, Attempts per goroutine: %d (total: %d attempts)",
		numGoroutines, attemptsPerGoroutine, numGoroutines*attemptsPerGoroutine)
	t.Logf("Successful starts: %d, Already running: %d, Errors: %d",
		successfulStarts, alreadyRunningCount, startErrors)

	// Verify no unexpected errors
	if startErrors > 0 {
		t.Errorf("Unexpected errors during concurrent Start: %d", startErrors)
	}

	// Verify we got exactly one successful start
	if successfulStarts != 1 {
		t.Errorf("Expected exactly 1 successful start, got: %d", successfulStarts)
	}

	// Single Stop() call - safe as only one caller
	kubeletMon.Stop()

	t.Logf("✓ Same-instance concurrent Start() operations handled correctly")
	t.Logf("✓ Note: Concurrent Stop() calls not tested due to known BaseMonitor limitation")
}


// mockStressKubeletClient is a thread-safe mock implementation for stress testing
type mockStressKubeletClient struct {
	mu        sync.RWMutex
	healthOK  bool
	systemdOK bool
	plegOK    bool
}

func (m *mockStressKubeletClient) CheckHealth(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.healthOK {
		return fmt.Errorf("mock health check failed")
	}
	return nil
}

func (m *mockStressKubeletClient) GetMetrics(ctx context.Context) (*KubeletMetrics, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.plegOK {
		return nil, fmt.Errorf("mock metrics fetch failed")
	}

	return &KubeletMetrics{
		PLEGRelistDuration: 0.001, // 1ms - healthy
	}, nil
}

func (m *mockStressKubeletClient) CheckSystemdStatus(ctx context.Context) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.systemdOK {
		return false, nil
	}
	return true, nil
}

func (m *mockStressKubeletClient) setHealth(healthy bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.healthOK = healthy
}

func (m *mockStressKubeletClient) setSystemd(active bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.systemdOK = active
}

func (m *mockStressKubeletClient) setPLEG(ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.plegOK = ok
}
