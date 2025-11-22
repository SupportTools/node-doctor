package detector

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// TestMonitorHandle_GetConfig tests the GetConfig method of MonitorHandle.
func TestMonitorHandle_GetConfig(t *testing.T) {
	config := types.MonitorConfig{
		Name:     "test-monitor",
		Type:     "test",
		Enabled:  true,
		Interval: 10 * time.Second,
		Timeout:  5 * time.Second,
		Config: map[string]interface{}{
			"key": "value",
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mock := NewMockMonitor(config.Name)
	statusCh := make(chan *types.Status)

	mh := &MonitorHandle{
		monitor:    mock,
		config:     config,
		statusCh:   statusCh,
		cancelFunc: cancel,
		wg:         &sync.WaitGroup{},
		ctx:        ctx,
		stopped:    false,
	}

	// Test GetConfig returns the correct config
	got := mh.GetConfig()

	if got.Name != config.Name {
		t.Errorf("GetConfig().Name = %s, want %s", got.Name, config.Name)
	}
	if got.Type != config.Type {
		t.Errorf("GetConfig().Type = %s, want %s", got.Type, config.Type)
	}
	if got.Enabled != config.Enabled {
		t.Errorf("GetConfig().Enabled = %v, want %v", got.Enabled, config.Enabled)
	}
	if got.Interval != config.Interval {
		t.Errorf("GetConfig().Interval = %v, want %v", got.Interval, config.Interval)
	}
	if got.Timeout != config.Timeout {
		t.Errorf("GetConfig().Timeout = %v, want %v", got.Timeout, config.Timeout)
	}
}

// TestMonitorHandle_IsRunning tests the IsRunning method of MonitorHandle.
func TestMonitorHandle_IsRunning(t *testing.T) {
	tests := []struct {
		name    string
		stopped bool
		want    bool
	}{
		{
			name:    "running when not stopped",
			stopped: false,
			want:    true,
		},
		{
			name:    "not running when stopped",
			stopped: true,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			mock := NewMockMonitor("test")
			statusCh := make(chan *types.Status)

			mh := &MonitorHandle{
				monitor:    mock,
				config:     types.MonitorConfig{Name: "test"},
				statusCh:   statusCh,
				cancelFunc: cancel,
				wg:         &sync.WaitGroup{},
				ctx:        ctx,
				stopped:    tt.stopped,
			}

			got := mh.IsRunning()
			if got != tt.want {
				t.Errorf("IsRunning() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestMonitorHandle_IsRunning_ConcurrentAccess tests thread safety of IsRunning.
func TestMonitorHandle_IsRunning_ConcurrentAccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mock := NewMockMonitor("test")
	statusCh := make(chan *types.Status)

	mh := &MonitorHandle{
		monitor:    mock,
		config:     types.MonitorConfig{Name: "test"},
		statusCh:   statusCh,
		cancelFunc: cancel,
		wg:         &sync.WaitGroup{},
		ctx:        ctx,
		stopped:    false,
	}

	var wg sync.WaitGroup
	const goroutines = 10
	const iterations = 100

	// Start multiple goroutines calling IsRunning
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = mh.IsRunning()
			}
		}()
	}

	// Stop the monitor midway through
	go func() {
		time.Sleep(5 * time.Millisecond)
		mh.mu.Lock()
		mh.stopped = true
		mh.mu.Unlock()
	}()

	wg.Wait()
	// Test passes if no race conditions or panics occurred
}

// TestMonitorHandle_GetName tests the GetName method of MonitorHandle.
func TestMonitorHandle_GetName(t *testing.T) {
	tests := []struct {
		name         string
		monitorName  string
		expectedName string
	}{
		{
			name:         "simple name",
			monitorName:  "test-monitor",
			expectedName: "test-monitor",
		},
		{
			name:         "name with spaces",
			monitorName:  "my test monitor",
			expectedName: "my test monitor",
		},
		{
			name:         "empty name",
			monitorName:  "",
			expectedName: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			mh := &MonitorHandle{
				monitor:    NewMockMonitor(tt.monitorName),
				config:     types.MonitorConfig{Name: tt.monitorName},
				statusCh:   make(chan *types.Status),
				cancelFunc: cancel,
				wg:         &sync.WaitGroup{},
				ctx:        ctx,
				stopped:    false,
			}

			got := mh.GetName()
			if got != tt.expectedName {
				t.Errorf("GetName() = %s, want %s", got, tt.expectedName)
			}
		})
	}
}

// TestProblemDetector_Run tests the Run method which is an alias for Start.
func TestProblemDetector_Run(t *testing.T) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()
	factory := NewMockMonitorFactory()

	detector, err := NewProblemDetector(
		config,
		[]types.Monitor{NewMockMonitor("test")},
		[]types.Exporter{NewMockExporter("test")},
		"/tmp/test-config.yaml",
		factory,
	)
	if err != nil {
		t.Fatalf("NewProblemDetector() error = %v", err)
	}
	defer detector.Stop()

	// Test that Run() starts the detector (same as Start())
	err = detector.Run()
	if err != nil {
		t.Errorf("Run() error = %v", err)
	}

	// Verify detector is running
	if !detector.IsRunning() {
		t.Error("Run() should have started the detector")
	}

	// Test that calling Run() again returns an error (same as Start())
	err = detector.Run()
	if err == nil {
		t.Error("Run() should error when detector is already running")
	}
}

// TestMonitorHandle_Stop tests the Stop method of MonitorHandle.
func TestMonitorHandle_Stop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	mock := NewMockMonitor("test")
	statusCh := make(chan *types.Status)

	mh := &MonitorHandle{
		monitor:    mock,
		config:     types.MonitorConfig{Name: "test"},
		statusCh:   statusCh,
		cancelFunc: cancel,
		wg:         &sync.WaitGroup{},
		ctx:        ctx,
		stopped:    false,
	}

	// Stop should succeed the first time
	err := mh.Stop()
	if err != nil {
		t.Errorf("Stop() error = %v", err)
	}

	// Verify IsRunning returns false after stop
	if mh.IsRunning() {
		t.Error("IsRunning() should return false after Stop()")
	}

	// Stop should be idempotent
	err = mh.Stop()
	if err != nil {
		t.Errorf("Stop() should be idempotent, got error = %v", err)
	}
}

// TestMonitorHandle_StopConcurrent tests concurrent Stop calls.
func TestMonitorHandle_StopConcurrent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	mock := NewMockMonitor("test")
	statusCh := make(chan *types.Status)

	mh := &MonitorHandle{
		monitor:    mock,
		config:     types.MonitorConfig{Name: "test"},
		statusCh:   statusCh,
		cancelFunc: cancel,
		wg:         &sync.WaitGroup{},
		ctx:        ctx,
		stopped:    false,
	}

	var wg sync.WaitGroup
	const goroutines = 10

	// Start multiple goroutines calling Stop concurrently
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = mh.Stop()
		}()
	}

	wg.Wait()

	// Verify monitor is stopped
	if mh.IsRunning() {
		t.Error("Monitor should be stopped after concurrent Stop calls")
	}
}
