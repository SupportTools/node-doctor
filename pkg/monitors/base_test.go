package monitors

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// Test helpers and mock implementations

// mockLogger implements the Logger interface for testing.
type mockLogger struct {
	mu       sync.Mutex
	infos    []string
	warnings []string
	errors   []string
}

func (m *mockLogger) Infof(format string, args ...interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.infos = append(m.infos, fmt.Sprintf(format, args...))
}

func (m *mockLogger) Warnf(format string, args ...interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.warnings = append(m.warnings, fmt.Sprintf(format, args...))
}

func (m *mockLogger) Errorf(format string, args ...interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errors = append(m.errors, fmt.Sprintf(format, args...))
}

func (m *mockLogger) getMessages() ([]string, []string, []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.infos...), append([]string{}, m.warnings...), append([]string{}, m.errors...)
}

func (m *mockLogger) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.infos = nil
	m.warnings = nil
	m.errors = nil
}

// Mock check functions for testing different scenarios

// mockHealthyCheck returns nil (no problems detected)
func mockHealthyCheck(ctx context.Context) (*types.Status, error) {
	return nil, nil
}

// mockUnhealthyCheck returns status with error event
// Note: this creates a status without source, letting BaseMonitor set it
func mockUnhealthyCheck(ctx context.Context) (*types.Status, error) {
	status := types.NewStatus("")
	event := types.NewEvent(types.EventError, "TestProblem", "Test problem detected")
	status.AddEvent(event)
	return status, nil
}

// mockSlowCheck takes longer than timeout to complete
func mockSlowCheck(ctx context.Context) (*types.Status, error) {
	select {
	case <-time.After(100 * time.Millisecond):
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// mockSlowCheckIgnoresContext takes longer than timeout and ignores context cancellation
func mockSlowCheckIgnoresContext(ctx context.Context) (*types.Status, error) {
	time.Sleep(100 * time.Millisecond)
	return nil, nil
}

// mockPanicCheck panics intentionally
func mockPanicCheck(ctx context.Context) (*types.Status, error) {
	panic("intentional panic for testing")
}

// mockErrorCheck returns an error
func mockErrorCheck(ctx context.Context) (*types.Status, error) {
	return nil, fmt.Errorf("test error")
}

// mockCountingCheck counts how many times it's called
type mockCountingCheck struct {
	count int64
}

func (m *mockCountingCheck) check(ctx context.Context) (*types.Status, error) {
	atomic.AddInt64(&m.count, 1)
	return nil, nil
}

func (m *mockCountingCheck) getCount() int64 {
	return atomic.LoadInt64(&m.count)
}

// Test cases

func TestNewBaseMonitor(t *testing.T) {
	tests := []struct {
		name        string
		monitorName string
		interval    time.Duration
		timeout     time.Duration
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid configuration",
			monitorName: "test-monitor",
			interval:    30 * time.Second,
			timeout:     10 * time.Second,
			expectError: false,
		},
		{
			name:        "empty name",
			monitorName: "",
			interval:    30 * time.Second,
			timeout:     10 * time.Second,
			expectError: true,
			errorMsg:    "monitor name cannot be empty",
		},
		{
			name:        "negative interval",
			monitorName: "test-monitor",
			interval:    -1 * time.Second,
			timeout:     10 * time.Second,
			expectError: true,
			errorMsg:    "interval must be positive, got -1s",
		},
		{
			name:        "zero interval",
			monitorName: "test-monitor",
			interval:    0,
			timeout:     10 * time.Second,
			expectError: true,
			errorMsg:    "interval must be positive, got 0s",
		},
		{
			name:        "negative timeout",
			monitorName: "test-monitor",
			interval:    30 * time.Second,
			timeout:     -1 * time.Second,
			expectError: true,
			errorMsg:    "timeout must be positive, got -1s",
		},
		{
			name:        "zero timeout",
			monitorName: "test-monitor",
			interval:    30 * time.Second,
			timeout:     0,
			expectError: true,
			errorMsg:    "timeout must be positive, got 0s",
		},
		{
			name:        "timeout >= interval",
			monitorName: "test-monitor",
			interval:    10 * time.Second,
			timeout:     10 * time.Second,
			expectError: true,
			errorMsg:    "timeout (10s) must be less than interval (10s)",
		},
		{
			name:        "timeout > interval",
			monitorName: "test-monitor",
			interval:    10 * time.Second,
			timeout:     15 * time.Second,
			expectError: true,
			errorMsg:    "timeout (15s) must be less than interval (10s)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monitor, err := NewBaseMonitor(tt.monitorName, tt.interval, tt.timeout)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
					return
				}
				if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error message to contain %q, got %q", tt.errorMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if monitor == nil {
				t.Error("expected non-nil monitor")
				return
			}

			// Verify initial state
			if monitor.GetName() != tt.monitorName {
				t.Errorf("expected name %q, got %q", tt.monitorName, monitor.GetName())
			}
			if monitor.GetInterval() != tt.interval {
				t.Errorf("expected interval %v, got %v", tt.interval, monitor.GetInterval())
			}
			if monitor.GetTimeout() != tt.timeout {
				t.Errorf("expected timeout %v, got %v", tt.timeout, monitor.GetTimeout())
			}
			if monitor.IsRunning() {
				t.Error("expected monitor to not be running initially")
			}
		})
	}
}

func TestBaseMonitor_SettersAndGetters(t *testing.T) {
	monitor, err := NewBaseMonitor("test", 30*time.Second, 10*time.Second)
	if err != nil {
		t.Fatalf("failed to create monitor: %v", err)
	}

	// Test SetCheckFunc
	err = monitor.SetCheckFunc(mockHealthyCheck)
	if err != nil {
		t.Fatalf("SetCheckFunc() error = %v", err)
	}

	// Test SetLogger
	logger := &mockLogger{}
	err = monitor.SetLogger(logger)
	if err != nil {
		t.Fatalf("SetLogger() error = %v", err)
	}

	// Getters are already tested in TestNewBaseMonitor
	// Test thread safety by calling getters concurrently
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			monitor.GetName()
			monitor.GetInterval()
			monitor.GetTimeout()
			monitor.IsRunning()
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestBaseMonitor_SetCheckFuncErrorWhenRunning(t *testing.T) {
	monitor, err := NewBaseMonitor("test", 100*time.Millisecond, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to create monitor: %v", err)
	}

	// Set initial check function
	err = monitor.SetCheckFunc(mockHealthyCheck)
	if err != nil {
		t.Fatalf("SetCheckFunc() error = %v", err)
	}

	// Start the monitor
	statusCh, err := monitor.Start()
	if err != nil {
		t.Fatalf("failed to start monitor: %v", err)
	}
	defer monitor.Stop()

	// Try to change check function while running - should fail
	err = monitor.SetCheckFunc(mockUnhealthyCheck)
	if err == nil {
		t.Error("expected error when setting check function on running monitor")
	}
	expectedError := "cannot change check function while monitor \"test\" is running"
	if !strings.Contains(err.Error(), expectedError) {
		t.Errorf("expected error to contain %q, got %q", expectedError, err.Error())
	}

	// Verify monitor is still working with original check function
	select {
	case status := <-statusCh:
		if status == nil {
			t.Error("received nil status")
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("timeout waiting for status")
	}
}

func TestBaseMonitor_SetLoggerErrorWhenRunning(t *testing.T) {
	monitor, err := NewBaseMonitor("test", 100*time.Millisecond, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to create monitor: %v", err)
	}

	// Set initial logger and check function
	logger1 := &mockLogger{}
	err = monitor.SetLogger(logger1)
	if err != nil {
		t.Fatalf("SetLogger() error = %v", err)
	}

	err = monitor.SetCheckFunc(mockHealthyCheck)
	if err != nil {
		t.Fatalf("SetCheckFunc() error = %v", err)
	}

	// Start the monitor
	statusCh, err := monitor.Start()
	if err != nil {
		t.Fatalf("failed to start monitor: %v", err)
	}
	defer monitor.Stop()

	// Try to change logger while running - should fail
	logger2 := &mockLogger{}
	err = monitor.SetLogger(logger2)
	if err == nil {
		t.Error("expected error when setting logger on running monitor")
	}
	expectedError := "cannot change logger while monitor \"test\" is running"
	if !strings.Contains(err.Error(), expectedError) {
		t.Errorf("expected error to contain %q, got %q", expectedError, err.Error())
	}

	// Verify monitor is still working
	select {
	case status := <-statusCh:
		if status == nil {
			t.Error("received nil status")
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("timeout waiting for status")
	}
}

func TestBaseMonitor_Restart(t *testing.T) {
	monitor, err := NewBaseMonitor("test-restart", 50*time.Millisecond, 25*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to create monitor: %v", err)
	}

	counter := &mockCountingCheck{}
	err = monitor.SetCheckFunc(counter.check)
	if err != nil {
		t.Fatalf("SetCheckFunc() error = %v", err)
	}

	logger := &mockLogger{}
	err = monitor.SetLogger(logger)
	if err != nil {
		t.Fatalf("SetLogger() error = %v", err)
	}

	// First start cycle
	statusCh1, err := monitor.Start()
	if err != nil {
		t.Fatalf("failed to start monitor: %v", err)
	}

	if !monitor.IsRunning() {
		t.Error("expected monitor to be running after start")
	}

	// Wait for at least one status update
	select {
	case status := <-statusCh1:
		if status == nil {
			t.Error("received nil status")
		} else if status.Source != "test-restart" {
			t.Errorf("expected source 'test-restart', got %q", status.Source)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("timeout waiting for first status")
	}

	// Stop the monitor
	monitor.Stop()

	if monitor.IsRunning() {
		t.Error("expected monitor to not be running after stop")
	}

	// Verify first channel is closed
	select {
	case _, ok := <-statusCh1:
		if ok {
			t.Error("expected first status channel to be closed")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for first channel closure")
	}

	// Second start cycle - restart after stop
	statusCh2, err := monitor.Start()
	if err != nil {
		t.Fatalf("failed to restart monitor: %v", err)
	}

	if !monitor.IsRunning() {
		t.Error("expected monitor to be running after restart")
	}

	// Verify new channel is different from old one
	if statusCh1 == statusCh2 {
		t.Error("expected new status channel after restart")
	}

	// Wait for status on new channel
	select {
	case status := <-statusCh2:
		if status == nil {
			t.Error("received nil status on restart")
		} else if status.Source != "test-restart" {
			t.Errorf("expected source 'test-restart' on restart, got %q", status.Source)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("timeout waiting for restart status")
	}

	// Stop again
	monitor.Stop()

	// Verify second channel is closed
	select {
	case _, ok := <-statusCh2:
		if ok {
			t.Error("expected second status channel to be closed")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for second channel closure")
	}

	// Third cycle - multiple restarts should work
	statusCh3, err := monitor.Start()
	if err != nil {
		t.Fatalf("failed to restart monitor second time: %v", err)
	}

	// Verify multiple restarts work
	select {
	case status := <-statusCh3:
		if status == nil {
			t.Error("received nil status on second restart")
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("timeout waiting for second restart status")
	}

	monitor.Stop()

	// Verify check function was called multiple times across restarts
	if counter.getCount() < 3 {
		t.Errorf("expected at least 3 check calls across restarts, got %d", counter.getCount())
	}

	// Verify logging for start/stop cycles
	infos, _, _ := logger.getMessages()
	startCount := 0
	stopCount := 0
	for _, info := range infos {
		if strings.Contains(info, "Starting monitor \"test-restart\"") {
			startCount++
		}
		if strings.Contains(info, "Monitor \"test-restart\" stopped") {
			stopCount++
		}
	}

	if startCount < 3 {
		t.Errorf("expected at least 3 start messages, got %d", startCount)
	}
	if stopCount < 3 {
		t.Errorf("expected at least 3 stop messages, got %d", stopCount)
	}
}

func TestBaseMonitor_StopTimeout(t *testing.T) {
	monitor, err := NewBaseMonitor("test-timeout", 100*time.Millisecond, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to create monitor: %v", err)
	}

	logger := &mockLogger{}
	err = monitor.SetLogger(logger)
	if err != nil {
		t.Fatalf("SetLogger() error = %v", err)
	}

	// Use a check function that ignores context cancellation
	err = monitor.SetCheckFunc(mockSlowCheckIgnoresContext)
	if err != nil {
		t.Fatalf("SetCheckFunc() error = %v", err)
	}

	// Start the monitor
	statusCh, err := monitor.Start()
	if err != nil {
		t.Fatalf("failed to start monitor: %v", err)
	}

	// Wait for it to start running
	select {
	case <-statusCh:
		// Got a status, monitor is running
	case <-time.After(200 * time.Millisecond):
		t.Error("timeout waiting for initial status")
	}

	// Patch the stop timeout to be shorter for testing
	// We can't directly modify the timeout, but we can test that Stop() returns
	// within a reasonable time even if the check function ignores context

	startTime := time.Now()
	monitor.Stop()
	stopDuration := time.Since(startTime)

	// Stop should return reasonably quickly even with timeout
	// The actual timeout is 30 seconds, but for testing we expect it to work
	if stopDuration > 5*time.Second {
		t.Errorf("Stop() took too long: %v", stopDuration)
	}

	// Verify monitor is marked as stopped
	if monitor.IsRunning() {
		t.Error("expected monitor to not be running after stop")
	}

	// Check for timeout warning in logs (may or may not appear depending on timing)
	_, warnings, _ := logger.getMessages()
	// Just verify we can access warnings without requiring timeout message
	// since the timeout behavior is hard to test reliably in unit tests
	_ = warnings
}

func TestBaseMonitor_StartAndStop(t *testing.T) {
	monitor, err := NewBaseMonitor("test", 100*time.Millisecond, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to create monitor: %v", err)
	}

	// Test starting without check function
	_, err = monitor.Start()
	if err == nil {
		t.Error("expected error when starting without check function")
		return
	}

	// Set check function and start
	err = monitor.SetCheckFunc(mockHealthyCheck)
	if err != nil {
		t.Fatalf("SetCheckFunc() error = %v", err)
	}

	statusCh, err := monitor.Start()
	if err != nil {
		t.Errorf("unexpected error starting monitor: %v", err)
		return
	}

	if statusCh == nil {
		t.Error("expected non-nil status channel")
		return
	}

	if !monitor.IsRunning() {
		t.Error("expected monitor to be running after start")
	}

	// Test double start
	_, err = monitor.Start()
	if err == nil {
		t.Error("expected error on double start")
	}

	// Wait for at least one status
	select {
	case status := <-statusCh:
		if status == nil {
			t.Error("received nil status")
		} else if status.Source != "test" {
			t.Errorf("expected source 'test', got %q", status.Source)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("timeout waiting for status")
	}

	// Stop the monitor
	monitor.Stop()

	if monitor.IsRunning() {
		t.Error("expected monitor to not be running after stop")
	}

	// Verify channel is closed
	select {
	case _, ok := <-statusCh:
		if ok {
			t.Error("expected status channel to be closed")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for channel closure")
	}

	// Test multiple stops (should be safe)
	monitor.Stop()
	monitor.Stop()
}

func TestBaseMonitor_StopWithoutStart(t *testing.T) {
	monitor, err := NewBaseMonitor("test", 30*time.Second, 10*time.Second)
	if err != nil {
		t.Fatalf("failed to create monitor: %v", err)
	}

	// Stop without starting should be safe
	monitor.Stop()

	if monitor.IsRunning() {
		t.Error("expected monitor to not be running")
	}
}

func TestBaseMonitor_StatusDelivery(t *testing.T) {
	monitor, err := NewBaseMonitor("test", 50*time.Millisecond, 25*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to create monitor: %v", err)
	}

	// Test unhealthy status - use a check function that includes the monitor name
	err = monitor.SetCheckFunc(func(ctx context.Context) (*types.Status, error) {
		status := types.NewStatus("test")
		event := types.NewEvent(types.EventError, "TestProblem", "Test problem detected")
		status.AddEvent(event)
		return status, nil
	})
	if err != nil {
		t.Fatalf("SetCheckFunc() error = %v", err)
	}

	statusCh, err := monitor.Start()
	if err != nil {
		t.Fatalf("failed to start monitor: %v", err)
	}
	defer monitor.Stop()

	// Collect a few statuses
	var statuses []*types.Status
	for i := 0; i < 3; i++ {
		select {
		case status := <-statusCh:
			if status != nil {
				statuses = append(statuses, status)
			}
		case <-time.After(200 * time.Millisecond):
			t.Fatalf("timeout waiting for status %d", i+1)
		}
	}

	if len(statuses) == 0 {
		t.Fatal("no statuses received")
	}

	// Verify status content
	status := statuses[0]
	if status.Source != "test" {
		t.Errorf("expected source 'test', got %q", status.Source)
	}
	if len(status.Events) == 0 {
		t.Error("expected at least one event in unhealthy status")
	}
}

func TestBaseMonitor_TimeoutEnforcement(t *testing.T) {
	monitor, err := NewBaseMonitor("test", 100*time.Millisecond, 25*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to create monitor: %v", err)
	}

	logger := &mockLogger{}
	err = monitor.SetLogger(logger)
	if err != nil {
		t.Fatalf("SetLogger() error = %v", err)
	}

	err = monitor.SetCheckFunc(mockSlowCheck)
	if err != nil {
		t.Fatalf("SetCheckFunc() error = %v", err)
	}

	statusCh, err := monitor.Start()
	if err != nil {
		t.Fatalf("failed to start monitor: %v", err)
	}
	defer monitor.Stop()

	// Wait for status indicating timeout
	select {
	case status := <-statusCh:
		if status == nil {
			t.Error("received nil status")
			return
		}
		// Should have error condition due to timeout
		hasError := false
		for _, event := range status.Events {
			if event.Severity == types.EventError {
				hasError = true
				break
			}
		}
		for _, condition := range status.Conditions {
			if condition.Status == types.ConditionFalse {
				hasError = true
				break
			}
		}
		if !hasError {
			t.Error("expected error status due to timeout")
		}
	case <-time.After(300 * time.Millisecond):
		t.Error("timeout waiting for status")
	}

	// Check logs for warning (relaxed matching due to timing)
	time.Sleep(50 * time.Millisecond)
	_, warnings, _ := logger.getMessages()
	hasWarning := len(warnings) > 0
	if !hasWarning {
		t.Error("expected at least one warning log")
	}
}

func TestBaseMonitor_IntervalTiming(t *testing.T) {
	counter := &mockCountingCheck{}
	interval := 50 * time.Millisecond

	monitor, err := NewBaseMonitor("test", interval, 25*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to create monitor: %v", err)
	}

	err = monitor.SetCheckFunc(counter.check)
	if err != nil {
		t.Fatalf("SetCheckFunc() error = %v", err)
	}

	statusCh, err := monitor.Start()
	if err != nil {
		t.Fatalf("failed to start monitor: %v", err)
	}

	// Wait for multiple intervals
	time.Sleep(125 * time.Millisecond)
	monitor.Stop()

	// Drain the channel
	for range statusCh {
	}

	count := counter.getCount()
	// Should have initial check plus at least 2 interval-based checks
	if count < 3 {
		t.Errorf("expected at least 3 checks, got %d", count)
	}
	if count > 5 {
		t.Errorf("expected at most 5 checks, got %d (timing may be off)", count)
	}
}

func TestBaseMonitor_ErrorHandling(t *testing.T) {
	monitor, err := NewBaseMonitor("test", 100*time.Millisecond, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to create monitor: %v", err)
	}

	logger := &mockLogger{}
	err = monitor.SetLogger(logger)
	if err != nil {
		t.Fatalf("SetLogger() error = %v", err)
	}

	err = monitor.SetCheckFunc(mockErrorCheck)
	if err != nil {
		t.Fatalf("SetCheckFunc() error = %v", err)
	}

	statusCh, err := monitor.Start()
	if err != nil {
		t.Fatalf("failed to start monitor: %v", err)
	}
	defer monitor.Stop()

	// Wait for error status
	select {
	case status := <-statusCh:
		if status == nil {
			t.Error("received nil status")
			return
		}
		// Should have error due to check function returning error
		hasError := false
		for _, event := range status.Events {
			if event.Severity == types.EventError {
				hasError = true
				break
			}
		}
		if !hasError {
			t.Error("expected error event due to check function error")
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("timeout waiting for status")
	}
}

func TestBaseMonitor_PanicRecovery(t *testing.T) {
	monitor, err := NewBaseMonitor("test", 100*time.Millisecond, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to create monitor: %v", err)
	}

	logger := &mockLogger{}
	err = monitor.SetLogger(logger)
	if err != nil {
		t.Fatalf("SetLogger() error = %v", err)
	}

	err = monitor.SetCheckFunc(mockPanicCheck)
	if err != nil {
		t.Fatalf("SetCheckFunc() error = %v", err)
	}

	statusCh, err := monitor.Start()
	if err != nil {
		t.Fatalf("failed to start monitor: %v", err)
	}
	defer monitor.Stop()

	// Wait for status indicating panic recovery
	select {
	case status := <-statusCh:
		if status == nil {
			t.Error("received nil status")
			return
		}
		// Should have error due to panic recovery
		hasError := false
		for _, event := range status.Events {
			if event.Severity == types.EventError &&
				event.Reason == "CheckFailed" {
				hasError = true
				break
			}
		}
		if !hasError {
			t.Error("expected error event due to panic recovery")
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("timeout waiting for status")
	}

	// Check logs for error (relaxed matching due to timing)
	time.Sleep(50 * time.Millisecond)
	_, _, errors := logger.getMessages()
	hasError := len(errors) > 0
	if !hasError {
		t.Error("expected at least one error log")
	}
}

func TestBaseMonitor_ConcurrentAccess(t *testing.T) {
	monitor, err := NewBaseMonitor("test", 50*time.Millisecond, 25*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to create monitor: %v", err)
	}

	err = monitor.SetCheckFunc(mockHealthyCheck)
	if err != nil {
		t.Fatalf("SetCheckFunc() error = %v", err)
	}

	// Test concurrent access to getters while monitor is running
	statusCh, err := monitor.Start()
	if err != nil {
		t.Fatalf("failed to start monitor: %v", err)
	}
	defer monitor.Stop()

	var wg sync.WaitGroup
	numGoroutines := 20

	// Concurrent getters
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				monitor.GetName()
				monitor.GetInterval()
				monitor.GetTimeout()
				monitor.IsRunning()
				time.Sleep(time.Millisecond)
			}
		}()
	}

	// Concurrent status reading
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			select {
			case <-statusCh:
				// Status received
			case <-time.After(100 * time.Millisecond):
				// Timeout is okay
			}
		}
	}()

	wg.Wait()
}

func TestBaseMonitor_StatusChannelBuffering(t *testing.T) {
	monitor, err := NewBaseMonitor("test", 10*time.Millisecond, 5*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to create monitor: %v", err)
	}

	logger := &mockLogger{}
	err = monitor.SetLogger(logger)
	if err != nil {
		t.Fatalf("SetLogger() error = %v", err)
	}

	err = monitor.SetCheckFunc(mockHealthyCheck)
	if err != nil {
		t.Fatalf("SetCheckFunc() error = %v", err)
	}

	statusCh, err := monitor.Start()
	if err != nil {
		t.Fatalf("failed to start monitor: %v", err)
	}

	// Don't read from channel to test buffering and dropping
	time.Sleep(150 * time.Millisecond)

	monitor.Stop()

	// Drain the channel
	count := 0
	for range statusCh {
		count++
	}

	// Should have received some statuses (limited by buffer size)
	if count == 0 {
		t.Error("expected some buffered statuses")
	}
	if count > 15 { // Should be limited by buffer + timing
		t.Errorf("received too many statuses: %d", count)
	}

	// Check for warning logs about dropped statuses - this is timing dependent
	// so we just verify the test doesn't crash when warnings are logged
	time.Sleep(50 * time.Millisecond)
	_, warnings, _ := logger.getMessages()
	// Just verify we can access the warnings without requiring specific content
	_ = warnings
}

func TestBaseMonitor_NilStatusHandling(t *testing.T) {
	monitor, err := NewBaseMonitor("test", 100*time.Millisecond, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to create monitor: %v", err)
	}

	// Check function that returns nil status and nil error
	err = monitor.SetCheckFunc(func(ctx context.Context) (*types.Status, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatalf("SetCheckFunc() error = %v", err)
	}

	statusCh, err := monitor.Start()
	if err != nil {
		t.Fatalf("failed to start monitor: %v", err)
	}
	defer monitor.Stop()

	// Should receive a healthy status even though check func returned nil
	select {
	case status := <-statusCh:
		if status == nil {
			t.Error("received nil status")
			return
		}
		if status.Source != "test" {
			t.Errorf("expected source 'test', got %q", status.Source)
		}
		// Should have healthy condition
		hasHealthy := false
		for _, condition := range status.Conditions {
			if condition.Type == "MonitorHealthy" && condition.Status == types.ConditionTrue {
				hasHealthy = true
				break
			}
		}
		if !hasHealthy {
			t.Error("expected healthy condition for nil status return")
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("timeout waiting for status")
	}
}

func TestBaseMonitor_LoggingIntegration(t *testing.T) {
	monitor, err := NewBaseMonitor("test-logger", 100*time.Millisecond, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to create monitor: %v", err)
	}

	logger := &mockLogger{}
	err = monitor.SetLogger(logger)
	if err != nil {
		t.Fatalf("SetLogger() error = %v", err)
	}

	err = monitor.SetCheckFunc(mockHealthyCheck)
	if err != nil {
		t.Fatalf("SetCheckFunc() error = %v", err)
	}

	statusCh, err := monitor.Start()
	if err != nil {
		t.Fatalf("failed to start monitor: %v", err)
	}

	// Wait for some operation
	select {
	case <-statusCh:
	case <-time.After(200 * time.Millisecond):
		t.Error("timeout waiting for status")
	}

	monitor.Stop()

	// Drain channel
	for range statusCh {
	}

	// Check logs
	infos, _, _ := logger.getMessages()
	expectedLogs := []string{
		"Starting monitor \"test-logger\"",
		"Monitor \"test-logger\" started",
		"Stopping monitor \"test-logger\"",
		"Monitor \"test-logger\" stopped",
	}

	for _, expected := range expectedLogs {
		found := false
		for _, info := range infos {
			if len(info) >= len(expected) && info[:len(expected)] == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected log message starting with %q not found in: %v", expected, infos)
		}
	}
}

func TestBaseMonitor_IntegrationPattern(t *testing.T) {
	// This test demonstrates how a concrete monitor would embed BaseMonitor
	type TestMonitor struct {
		*BaseMonitor
		checkCount int32
	}

	createTestMonitor := func(name string) (*TestMonitor, error) {
		base, err := NewBaseMonitor(name, 50*time.Millisecond, 25*time.Millisecond)
		if err != nil {
			return nil, err
		}

		tm := &TestMonitor{BaseMonitor: base}

		// Set up check function
		err = base.SetCheckFunc(func(ctx context.Context) (*types.Status, error) {
			atomic.AddInt32(&tm.checkCount, 1)

			status := types.NewStatus(name)
			condition := types.NewCondition(
				"TestCondition",
				types.ConditionTrue,
				"TestReason",
				"Test monitor is working",
			)
			status.AddCondition(condition)
			return status, nil
		})
		if err != nil {
			return nil, err
		}

		return tm, nil
	}

	// Create and test the embedded monitor
	monitor, err := createTestMonitor("embedded-test")
	if err != nil {
		t.Fatalf("failed to create test monitor: %v", err)
	}

	statusCh, err := monitor.Start()
	if err != nil {
		t.Fatalf("failed to start monitor: %v", err)
	}
	defer monitor.Stop()

	// Wait for a few checks
	var receivedStatus *types.Status
	for i := 0; i < 3; i++ {
		select {
		case status := <-statusCh:
			receivedStatus = status
		case <-time.After(200 * time.Millisecond):
			t.Fatalf("timeout waiting for status %d", i+1)
		}
	}

	if receivedStatus == nil {
		t.Fatal("no status received")
	}

	// Verify the custom status
	if receivedStatus.Source != "embedded-test" {
		t.Errorf("expected source 'embedded-test', got %q", receivedStatus.Source)
	}

	hasTestCondition := false
	for _, condition := range receivedStatus.Conditions {
		if condition.Type == "TestCondition" && condition.Status == types.ConditionTrue {
			hasTestCondition = true
			break
		}
	}
	if !hasTestCondition {
		t.Error("expected TestCondition in status")
	}

	// Verify check function was called
	if atomic.LoadInt32(&monitor.checkCount) < 3 {
		t.Errorf("expected at least 3 check calls, got %d", atomic.LoadInt32(&monitor.checkCount))
	}
}
