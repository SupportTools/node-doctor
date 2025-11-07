package custom

import (
	"context"

	"github.com/supporttools/node-doctor/pkg/types"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestStress_Concurrent_100Goroutines tests the monitor with 100+ concurrent goroutines
// to detect race conditions and verify thread-safety. Run with: go test -race
func TestStress_Concurrent_100Goroutines(t *testing.T) {
	// Create a monitor with multiple patterns
	mockFS := newMockFileReader()

	// Create log content with multiple types of errors
	var logContent strings.Builder
	for i := 0; i < 1000; i++ {
		logContent.WriteString(fmt.Sprintf("ERROR%d: test message %d\n", i%10, i))
		logContent.WriteString(fmt.Sprintf("WARNING%d: test warning %d\n", i%5, i))
	}
	mockFS.setFile("/dev/kmsg", logContent.String())

	config := &LogPatternMonitorConfig{
		Patterns: []LogPatternConfig{
			{
				Regex:       `ERROR[0-9]:`,
				Severity: "error",
    Source:      "kmsg",
				Description: "Error pattern",
			},
			{
				Regex:       `WARNING[0-9]:`,
				Severity: "warning",
    Source:      "kmsg",
				Description: "Warning pattern",
			},
		},
		KmsgPath:           "/dev/kmsg",
		CheckKmsg:          true,
		CheckJournal:       false,
		MaxEventsPerPattern: 100,
		DedupWindow:        1 * time.Second,
	}

	ctx := context.Background()


	mockFS = newMockFileReader()


	mockExecutor := &mockCommandExecutor{}


	


	err := config.applyDefaults()


	if err != nil {


		t.Fatalf("failed to apply defaults: %v", err)


	}


	


	monitorConfig := types.MonitorConfig{


		Name:     "test-concurrent",


		Type:     "log-pattern",


		Interval: 30 * time.Second,


		Timeout:  10 * time.Second,


		Config:   map[string]interface{}{},


	}


	


	mon, err := NewLogPatternMonitorWithDependencies(ctx, monitorConfig, config, mockFS, mockExecutor)


	if err != nil {


		t.Fatalf("failed to create monitor: %v", err)


	}


	monitor := mon.(*LogPatternMonitor)
	if err != nil {
		t.Fatalf("Failed to create monitor: %v", err)
	}
	monitor.fileReader = mockFS

	// Launch 100 goroutines concurrently calling Check()
	const numGoroutines = 100
	var wg sync.WaitGroup
	var successCount int32
	var errorCount int32
	errors := make([]error, 0)
	var errorsMu sync.Mutex

	ctx = context.Background()

	startTime := time.Now()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Each goroutine performs a check
			status, err := monitor.checkLogPatterns(ctx)
			if err != nil {
				atomic.AddInt32(&errorCount, 1)
				errorsMu.Lock()
				errors = append(errors, fmt.Errorf("goroutine %d: %w", id, err))
				errorsMu.Unlock()
				return
			}

			if status == nil {
				atomic.AddInt32(&errorCount, 1)
				errorsMu.Lock()
				errors = append(errors, fmt.Errorf("goroutine %d: nil status", id))
				errorsMu.Unlock()
				return
			}

			atomic.AddInt32(&successCount, 1)
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	duration := time.Since(startTime)

	// Report results
	t.Logf("Concurrent test completed in %v", duration)
	t.Logf("Success: %d, Errors: %d", successCount, errorCount)

	if errorCount > 0 {
		t.Errorf("Some goroutines failed (%d/%d)", errorCount, numGoroutines)
		for i, err := range errors {
			if i < 10 { // Limit error output
				t.Logf("Error %d: %v", i+1, err)
			}
		}
	}

	// All goroutines should succeed
	if successCount != numGoroutines {
		t.Errorf("Expected %d successful checks, got %d", numGoroutines, successCount)
	}

	// Performance check - 100 checks should complete in reasonable time
	if duration > 10*time.Second {
		t.Errorf("Concurrent checks took too long: %v (expected <10s)", duration)
	}
}

// TestStress_Concurrent_RateLimiting tests concurrent access to rate limiting logic
// to verify atomic operations and deduplication work correctly under contention.
func TestStress_Concurrent_RateLimiting(t *testing.T) {
	mockFS := newMockFileReader()

	// Same error repeated many times
	var logContent strings.Builder
	for i := 0; i < 500; i++ {
		logContent.WriteString("ERROR: duplicate message\n")
	}
	mockFS.setFile("/dev/kmsg", logContent.String())

	config := &LogPatternMonitorConfig{
		Patterns: []LogPatternConfig{
			{
				Regex:       `ERROR:`,
				Severity: "error",
    Source:      "kmsg",
				Description: "Error found",
			},
		},
		KmsgPath:           "/dev/kmsg",
		CheckKmsg:          true,
		CheckJournal:       false,
		MaxEventsPerPattern: 10,  // Low limit to test rate limiting
		DedupWindow:        1 * time.Second, // Minimum valid value
	}

	ctx := context.Background()


	mockFS = newMockFileReader()


	mockExecutor := &mockCommandExecutor{}


	


	err := config.applyDefaults()


	if err != nil {


		t.Fatalf("failed to apply defaults: %v", err)


	}


	


	monitorConfig := types.MonitorConfig{


		Name:     "test-ratelimit",


		Type:     "log-pattern",


		Interval: 30 * time.Second,


		Timeout:  10 * time.Second,


		Config:   map[string]interface{}{},


	}


	


	mon, err := NewLogPatternMonitorWithDependencies(ctx, monitorConfig, config, mockFS, mockExecutor)


	if err != nil {


		t.Fatalf("failed to create monitor: %v", err)


	}


	monitor := mon.(*LogPatternMonitor)
	if err != nil {
		t.Fatalf("Failed to create monitor: %v", err)
	}
	monitor.fileReader = mockFS

	// Run concurrent checks
	const numGoroutines = 50
	var wg sync.WaitGroup
	ctx = context.Background()

	allEvents := make([]int, numGoroutines)
	var eventsMu sync.Mutex

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			status, err := monitor.checkLogPatterns(ctx)
			if err != nil {
				t.Errorf("Goroutine %d check failed: %v", id, err)
				return
			}

			eventsMu.Lock()
			allEvents[id] = len(status.Events)
			eventsMu.Unlock()
		}(i)
	}

	wg.Wait()

	// Verify rate limiting is working - not all checks should generate events
	// due to deduplication
	totalEvents := 0
	for _, count := range allEvents {
		totalEvents += count
	}

	t.Logf("Total events across all goroutines: %d", totalEvents)

	// With rate limiting, we shouldn't get 500*50 = 25000 events
	// Should be much less due to MaxEventsPerPattern and dedup
	maxExpected := 10 * numGoroutines // MaxEventsPerPattern * goroutines (worst case)
	if totalEvents > maxExpected {
		t.Errorf("Rate limiting not working: got %d events, expected <%d", totalEvents, maxExpected)
	}
}

// TestStress_Concurrent_MapCleanup tests that map cleanup works correctly
// during concurrent access, without losing events or causing corruption.
func TestStress_Concurrent_MapCleanup(t *testing.T) {
	mockFS := newMockFileReader()

	config := &LogPatternMonitorConfig{
		Patterns: []LogPatternConfig{
			{
				Regex:       `ERROR[0-9]+:`,
				Severity: "error",
    Source:      "kmsg",
				Description: "Error found",
			},
		},
		KmsgPath:           "/dev/kmsg",
		CheckKmsg:          true,
		CheckJournal:       false,
		MaxEventsPerPattern: 50,
		DedupWindow:        1 * time.Second, // Minimum valid value to trigger cleanup
	}

	ctx := context.Background()


	mockFS = newMockFileReader()


	mockExecutor := &mockCommandExecutor{}


	


	err := config.applyDefaults()


	if err != nil {


		t.Fatalf("failed to apply defaults: %v", err)


	}


	


	monitorConfig := types.MonitorConfig{


		Name:     "test-cleanup",


		Type:     "log-pattern",


		Interval: 30 * time.Second,


		Timeout:  10 * time.Second,


		Config:   map[string]interface{}{},


	}


	


	mon, err := NewLogPatternMonitorWithDependencies(ctx, monitorConfig, config, mockFS, mockExecutor)


	if err != nil {


		t.Fatalf("failed to create monitor: %v", err)


	}


	monitor := mon.(*LogPatternMonitor)
	if err != nil {
		t.Fatalf("Failed to create monitor: %v", err)
	}
	monitor.fileReader = mockFS

	// Run checks with different log content over time
	const numIterations = 100
	var wg sync.WaitGroup
	ctx = context.Background()

	for i := 0; i < numIterations; i++ {
		// Generate unique log content for each iteration
		logContent := fmt.Sprintf("ERROR%d: message at iteration %d\n", i%10, i)
		mockFS.setFile("/dev/kmsg", logContent)

		// Launch concurrent checks
		wg.Add(10)
		for j := 0; j < 10; j++ {
			go func(iter, gid int) {
				defer wg.Done()

				_, err := monitor.checkLogPatterns(ctx)
				if err != nil {
					t.Errorf("Iteration %d, goroutine %d failed: %v", iter, gid, err)
				}
			}(i, j)
		}

		// Small delay to allow cleanup to happen
		if i%10 == 0 {
			time.Sleep(60 * time.Millisecond)
		}
	}

	wg.Wait()

	// Verify monitor is still functional after all the concurrent access
	finalCheck, err := monitor.checkLogPatterns(ctx)
	if err != nil {
		t.Errorf("Final check failed: %v", err)
	}
	if finalCheck == nil {
		t.Error("Final check returned nil status")
	}

	t.Log("Map cleanup stress test completed successfully")
}

// TestStress_SustainedLoad_1000Checks tests the monitor under sustained load
// to verify no memory leaks and bounded resource usage.
func TestStress_SustainedLoad_1000Checks(t *testing.T) {
	mockFS := newMockFileReader()

	// Create varied log content
	var logContent strings.Builder
	for i := 0; i < 100; i++ {
		logContent.WriteString(fmt.Sprintf("ERROR: message %d\n", i))
		logContent.WriteString(fmt.Sprintf("WARNING: warning %d\n", i))
		logContent.WriteString(fmt.Sprintf("INFO: info %d\n", i))
	}
	mockFS.setFile("/dev/kmsg", logContent.String())

	config := &LogPatternMonitorConfig{
		Patterns: []LogPatternConfig{
			{
				Regex:       `ERROR:`,
				Severity: "error",
    Source:      "kmsg",
				Description: "Error",
			},
			{
				Regex:       `WARNING:`,
				Severity: "warning",
    Source:      "kmsg",
				Description: "Warning",
			},
			{
				Regex:       `INFO:`,
				Severity: "info",
    Source:      "kmsg",
				Description: "Info",
			},
		},
		KmsgPath:           "/dev/kmsg",
		CheckKmsg:          true,
		CheckJournal:       false,
		MaxEventsPerPattern: 100,
		DedupWindow:        1 * time.Second,
	}

	ctx := context.Background()


	mockFS = newMockFileReader()


	mockExecutor := &mockCommandExecutor{}


	


	err := config.applyDefaults()


	if err != nil {


		t.Fatalf("failed to apply defaults: %v", err)


	}


	


	monitorConfig := types.MonitorConfig{


		Name:     "test-sustained",


		Type:     "log-pattern",


		Interval: 30 * time.Second,


		Timeout:  10 * time.Second,


		Config:   map[string]interface{}{},


	}


	


	mon, err := NewLogPatternMonitorWithDependencies(ctx, monitorConfig, config, mockFS, mockExecutor)


	if err != nil {


		t.Fatalf("failed to create monitor: %v", err)


	}


	monitor := mon.(*LogPatternMonitor)
	if err != nil {
		t.Fatalf("Failed to create monitor: %v", err)
	}
	monitor.fileReader = mockFS

	// Force GC and get baseline memory
	runtime.GC()
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	// Run 1000 checks
	const numChecks = 1000
	ctx = context.Background()
	startTime := time.Now()

	for i := 0; i < numChecks; i++ {
		status, err := monitor.checkLogPatterns(ctx)
		if err != nil {
			t.Errorf("Check %d failed: %v", i, err)
		}
		if status == nil {
			t.Errorf("Check %d returned nil status", i)
		}

		// Occasionally force GC to detect leaks
		if i%100 == 0 {
			runtime.GC()
		}
	}

	duration := time.Since(startTime)

	// Force GC and check final memory
	runtime.GC()
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	// Calculate metrics
	avgDuration := duration / numChecks
	memIncrease := int64(memAfter.Alloc) - int64(memBefore.Alloc)

	t.Logf("Sustained load test results:")
	t.Logf("  Total time: %v", duration)
	t.Logf("  Average per check: %v", avgDuration)
	t.Logf("  Memory before: %d bytes", memBefore.Alloc)
	t.Logf("  Memory after: %d bytes", memAfter.Alloc)
	t.Logf("  Memory increase: %d bytes (%.2f MB)", memIncrease, float64(memIncrease)/(1024*1024))

	// Performance check - average should be well under 10ms
	if avgDuration > 10*time.Millisecond {
		t.Errorf("Average check duration too slow: %v (expected <10ms)", avgDuration)
	}

	// Memory check - increase should be bounded (allow up to 10MB growth)
	maxMemIncrease := int64(10 * 1024 * 1024)
	if memIncrease > maxMemIncrease {
		t.Errorf("Excessive memory growth: %.2f MB (expected <10MB)",
			float64(memIncrease)/(1024*1024))
	}
}

// TestStress_MapGrowth_10000Events tests that internal maps don't grow unbounded
// with many events, and that cleanup is effective.
func TestStress_MapGrowth_10000Events(t *testing.T) {
	mockFS := newMockFileReader()

	config := &LogPatternMonitorConfig{
		Patterns: []LogPatternConfig{
			{
				Regex:       `ERROR:`,
				Severity: "error",
    Source:      "kmsg",
				Description: "Error",
			},
		},
		KmsgPath:           "/dev/kmsg",
		CheckKmsg:          true,
		CheckJournal:       false,
		MaxEventsPerPattern: 1000,
		DedupWindow:        1 * time.Second, // Minimum valid value for aggressive cleanup
	}

	ctx := context.Background()


	mockFS = newMockFileReader()


	mockExecutor := &mockCommandExecutor{}


	


	err := config.applyDefaults()


	if err != nil {


		t.Fatalf("failed to apply defaults: %v", err)


	}


	


	monitorConfig := types.MonitorConfig{


		Name:     "test-mapgrowth",


		Type:     "log-pattern",


		Interval: 30 * time.Second,


		Timeout:  10 * time.Second,


		Config:   map[string]interface{}{},


	}


	


	mon, err := NewLogPatternMonitorWithDependencies(ctx, monitorConfig, config, mockFS, mockExecutor)


	if err != nil {


		t.Fatalf("failed to create monitor: %v", err)


	}


	monitor := mon.(*LogPatternMonitor)
	if err != nil {
		t.Fatalf("Failed to create monitor: %v", err)
	}
	monitor.fileReader = mockFS

	ctx = context.Background()

	// Generate 10,000 unique events over time
	for i := 0; i < 10000; i++ {
		// Each check has a unique timestamp to avoid dedup by content
		logContent := fmt.Sprintf("ERROR: unique message %d at %d\n", i, time.Now().UnixNano())
		mockFS.setFile("/dev/kmsg", logContent)

		_, err := monitor.checkLogPatterns(ctx)
		if err != nil {
			t.Errorf("Check %d failed: %v", i, err)
		}

		// Periodically sleep to allow cleanup
		if i%100 == 0 {
			time.Sleep(15 * time.Millisecond)
		}
	}

	// Verify monitor is still functional
	finalStatus, err := monitor.checkLogPatterns(ctx)
	if err != nil {
		t.Errorf("Final check failed: %v", err)
	}
	if finalStatus == nil {
		t.Error("Final check returned nil status")
	}

	t.Log("Map growth test with 10,000 events completed successfully")
}

// TestStress_LockContention_Measurement measures lock contention under concurrent load.
func TestStress_LockContention_Measurement(t *testing.T) {
	mockFS := newMockFileReader()

	// Create log content with many patterns to match
	var logContent strings.Builder
	for i := 0; i < 200; i++ {
		logContent.WriteString(fmt.Sprintf("ERROR%d: test\n", i%20))
	}
	mockFS.setFile("/dev/kmsg", logContent.String())

	// Create config with many patterns (increases lock contention)
	patterns := make([]LogPatternConfig, 20)
	for i := 0; i < 20; i++ {
		patterns[i] = LogPatternConfig{
			Regex:       fmt.Sprintf(`ERROR%d:`, i),
			Severity:    "error",
   Source:      "kmsg",
			Description: fmt.Sprintf("Error %d", i),
		}
	}

	config := &LogPatternMonitorConfig{
		Patterns:            patterns,
		KmsgPath:           "/dev/kmsg",
		CheckKmsg:          true,
		CheckJournal:       false,
		MaxEventsPerPattern: 50,
		DedupWindow:        1 * time.Second,
	}

	ctx := context.Background()


	mockFS = newMockFileReader()


	mockExecutor := &mockCommandExecutor{}


	


	err := config.applyDefaults()


	if err != nil {


		t.Fatalf("failed to apply defaults: %v", err)


	}


	


	monitorConfig := types.MonitorConfig{


		Name:     "test-contention",


		Type:     "log-pattern",


		Interval: 30 * time.Second,


		Timeout:  10 * time.Second,


		Config:   map[string]interface{}{},


	}


	


	mon, err := NewLogPatternMonitorWithDependencies(ctx, monitorConfig, config, mockFS, mockExecutor)


	if err != nil {


		t.Fatalf("failed to create monitor: %v", err)


	}


	monitor := mon.(*LogPatternMonitor)
	if err != nil {
		t.Fatalf("Failed to create monitor: %v", err)
	}
	monitor.fileReader = mockFS

	// Run highly concurrent checks
	const numGoroutines = 200
	var wg sync.WaitGroup
	ctx = context.Background()

	// Measure time under contention
	startTime := time.Now()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			status, err := monitor.checkLogPatterns(ctx)
			if err != nil {
				t.Errorf("Goroutine %d failed: %v", id, err)
				return
			}
			if status == nil {
				t.Errorf("Goroutine %d got nil status", id)
			}
		}(i)
	}

	wg.Wait()

	duration := time.Since(startTime)
	avgPerGoroutine := duration / numGoroutines

	t.Logf("Lock contention test results:")
	t.Logf("  %d goroutines completed in %v", numGoroutines, duration)
	t.Logf("  Average per goroutine: %v", avgPerGoroutine)

	// Even with high contention, average time should be reasonable
	if avgPerGoroutine > 50*time.Millisecond {
		t.Errorf("High lock contention detected: avg %v per goroutine (expected <50ms)",
			avgPerGoroutine)
	}

	// Total time should still be reasonable
	if duration > 20*time.Second {
		t.Errorf("Total time under contention too high: %v (expected <20s)", duration)
	}
}

// TestStress_ContextCancellation_DuringLoad tests that context cancellation
// is handled correctly even under high load.
func TestStress_ContextCancellation_DuringLoad(t *testing.T) {
	mockFS := newMockFileReader()
	mockFS.setFile("/dev/kmsg", "ERROR: test\n")

	config := &LogPatternMonitorConfig{
		Patterns: []LogPatternConfig{
			{
				Regex:       `ERROR:`,
				Severity: "error",
    Source:      "kmsg",
				Description: "Error",
			},
		},
		KmsgPath:  "/dev/kmsg",
		CheckKmsg: true,
		CheckJournal: false,
	}

	ctx := context.Background()


	mockFS = newMockFileReader()


	mockExecutor := &mockCommandExecutor{}


	


	err := config.applyDefaults()


	if err != nil {


		t.Fatalf("failed to apply defaults: %v", err)


	}


	


	monitorConfig := types.MonitorConfig{


		Name:     "test-cancel",


		Type:     "log-pattern",


		Interval: 30 * time.Second,


		Timeout:  10 * time.Second,


		Config:   map[string]interface{}{},


	}


	


	mon, err := NewLogPatternMonitorWithDependencies(ctx, monitorConfig, config, mockFS, mockExecutor)


	if err != nil {


		t.Fatalf("failed to create monitor: %v", err)


	}


	monitor := mon.(*LogPatternMonitor)
	if err != nil {
		t.Fatalf("Failed to create monitor: %v", err)
	}
	monitor.fileReader = mockFS

	// Create a context that we'll cancel mid-flight
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Launch many goroutines that will be interrupted
	const numGoroutines = 100
	var wg sync.WaitGroup
	var canceledCount int32

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Keep checking until context is canceled
			for ctx.Err() == nil {
				_, err := monitor.checkLogPatterns(ctx)
				if err != nil && ctx.Err() != nil {
					atomic.AddInt32(&canceledCount, 1)
					return
				}
			}
		}()
	}

	// Wait for context to expire
	<-ctx.Done()

	// Wait for all goroutines to clean up
	wg.Wait()

	t.Logf("Context cancellation handled by %d goroutines", canceledCount)

	// Verify monitor still works after cancellation storm
	newCtx := context.Background()
	status, err := monitor.checkLogPatterns(newCtx)
	if err != nil {
		t.Errorf("Monitor broken after cancellation storm: %v", err)
	}
	if status == nil {
		t.Error("Monitor returned nil status after cancellation")
	}
}
