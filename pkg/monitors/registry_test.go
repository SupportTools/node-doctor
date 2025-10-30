package monitors

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/supporttools/node-doctor/pkg/types"
)

// Mock monitor implementation for testing
type mockMonitor struct {
	name      string
	started   bool
	stopped   bool
	statusCh  chan *types.Status
	mu        sync.Mutex
}

func newMockMonitor(name string) *mockMonitor {
	return &mockMonitor{
		name:     name,
		statusCh: make(chan *types.Status, 1),
	}
}

func (m *mockMonitor) Start() (<-chan *types.Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return nil, errors.New("monitor already started")
	}

	m.started = true
	return m.statusCh, nil
}

func (m *mockMonitor) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.stopped = true
	close(m.statusCh)
}

func (m *mockMonitor) isStarted() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.started
}

func (m *mockMonitor) isStopped() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopped
}

// Test factory functions
func testFactory1(ctx context.Context, config types.MonitorConfig) (types.Monitor, error) {
	return newMockMonitor("test1"), nil
}

func testFactory2(ctx context.Context, config types.MonitorConfig) (types.Monitor, error) {
	return newMockMonitor("test2"), nil
}

func failingFactory(ctx context.Context, config types.MonitorConfig) (types.Monitor, error) {
	return nil, errors.New("factory failure")
}

func contextCancelFactory(ctx context.Context, config types.MonitorConfig) (types.Monitor, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return newMockMonitor("context-test"), nil
	}
}

func panickingFactory(ctx context.Context, config types.MonitorConfig) (types.Monitor, error) {
	panic("factory panic for testing")
}

// Test validator functions
func testValidator1(config types.MonitorConfig) error {
	if config.Name == "" {
		return errors.New("name is required")
	}
	return nil
}

func testValidator2(config types.MonitorConfig) error {
	if config.Config == nil {
		return errors.New("config is required")
	}
	return nil
}

func failingValidator(config types.MonitorConfig) error {
	return errors.New("validation failed")
}

// Helper function to create test config
func createTestConfig(monitorType, name string) types.MonitorConfig {
	return types.MonitorConfig{
		Name:    name,
		Type:    monitorType,
		Enabled: true,
		Config:  map[string]interface{}{"test": "value"},
	}
}

func TestNewRegistry(t *testing.T) {
	registry := NewRegistry()

	if registry == nil {
		t.Fatal("NewRegistry returned nil")
	}

	if registry.monitors == nil {
		t.Fatal("Registry monitors map is nil")
	}

	if len(registry.monitors) != 0 {
		t.Fatalf("New registry should be empty, got %d monitors", len(registry.monitors))
	}
}

func TestRegistry_Register(t *testing.T) {
	tests := []struct {
		name      string
		info      MonitorInfo
		shouldPanic bool
		panicMsg    string
	}{
		{
			name: "valid registration",
			info: MonitorInfo{
				Type:        "test-monitor",
				Factory:     testFactory1,
				Validator:   testValidator1,
				Description: "Test monitor",
			},
			shouldPanic: false,
		},
		{
			name: "valid registration without validator",
			info: MonitorInfo{
				Type:        "test-monitor-no-validator",
				Factory:     testFactory1,
				Description: "Test monitor without validator",
			},
			shouldPanic: false,
		},
		{
			name: "empty type",
			info: MonitorInfo{
				Type:    "",
				Factory: testFactory1,
			},
			shouldPanic: true,
			panicMsg:    "monitor type cannot be empty",
		},
		{
			name: "nil factory",
			info: MonitorInfo{
				Type:    "test-monitor",
				Factory: nil,
			},
			shouldPanic: true,
			panicMsg:    "monitor factory cannot be nil for type \"test-monitor\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewRegistry()

			if tt.shouldPanic {
				defer func() {
					if r := recover(); r != nil {
						if str, ok := r.(string); ok {
							if str != tt.panicMsg {
								t.Errorf("Expected panic message %q, got %q", tt.panicMsg, str)
							}
						} else {
							t.Errorf("Expected string panic message, got %T: %v", r, r)
						}
					} else {
						t.Error("Expected panic, but none occurred")
					}
				}()
			}

			registry.Register(tt.info)

			if !tt.shouldPanic {
				// Verify registration succeeded
				if !registry.IsRegistered(tt.info.Type) {
					t.Errorf("Monitor type %q was not registered", tt.info.Type)
				}
			}
		})
	}
}

func TestRegistry_RegisterDuplicate(t *testing.T) {
	registry := NewRegistry()

	info := MonitorInfo{
		Type:    "test-monitor",
		Factory: testFactory1,
	}

	// First registration should succeed
	registry.Register(info)

	// Second registration should panic
	defer func() {
		if r := recover(); r != nil {
			expected := "monitor type \"test-monitor\" is already registered"
			if str, ok := r.(string); ok {
				if str != expected {
					t.Errorf("Expected panic message %q, got %q", expected, str)
				}
			} else {
				t.Errorf("Expected string panic message, got %T: %v", r, r)
			}
		} else {
			t.Error("Expected panic on duplicate registration")
		}
	}()

	registry.Register(info)
}

func TestRegistry_GetRegisteredTypes(t *testing.T) {
	registry := NewRegistry()

	// Test empty registry
	registeredTypes := registry.GetRegisteredTypes()
	if len(registeredTypes) != 0 {
		t.Errorf("Empty registry should return empty slice, got %v", registeredTypes)
	}

	// Register some monitors
	monitors := []string{"monitor-c", "monitor-a", "monitor-b"}
	for _, monitorType := range monitors {
		registry.Register(MonitorInfo{
			Type:    monitorType,
			Factory: testFactory1,
		})
	}

	// Get registered types
	registeredTypes = registry.GetRegisteredTypes()

	// Should be sorted
	expected := []string{"monitor-a", "monitor-b", "monitor-c"}
	if !reflect.DeepEqual(registeredTypes, expected) {
		t.Errorf("Expected %v, got %v", expected, registeredTypes)
	}

	// Verify it's a copy (modifying shouldn't affect registry)
	registeredTypes[0] = "modified"
	typesAgain := registry.GetRegisteredTypes()
	if typesAgain[0] != "monitor-a" {
		t.Error("GetRegisteredTypes should return a copy")
	}
}

func TestRegistry_IsRegistered(t *testing.T) {
	registry := NewRegistry()

	// Test unregistered type
	if registry.IsRegistered("nonexistent") {
		t.Error("IsRegistered should return false for unregistered type")
	}

	// Register a monitor
	registry.Register(MonitorInfo{
		Type:    "test-monitor",
		Factory: testFactory1,
	})

	// Test registered type
	if !registry.IsRegistered("test-monitor") {
		t.Error("IsRegistered should return true for registered type")
	}

	// Test case sensitivity
	if registry.IsRegistered("Test-Monitor") {
		t.Error("IsRegistered should be case sensitive")
	}
}

func TestRegistry_GetMonitorInfo(t *testing.T) {
	registry := NewRegistry()

	// Test unregistered type
	info := registry.GetMonitorInfo("nonexistent")
	if info != nil {
		t.Error("GetMonitorInfo should return nil for unregistered type")
	}

	// Register a monitor
	originalInfo := MonitorInfo{
		Type:        "test-monitor",
		Factory:     testFactory1,
		Validator:   testValidator1,
		Description: "Test monitor description",
	}
	registry.Register(originalInfo)

	// Get monitor info
	info = registry.GetMonitorInfo("test-monitor")
	if info == nil {
		t.Fatal("GetMonitorInfo should return info for registered type")
	}

	// Verify fields
	if info.Type != originalInfo.Type {
		t.Errorf("Expected type %q, got %q", originalInfo.Type, info.Type)
	}
	if info.Description != originalInfo.Description {
		t.Errorf("Expected description %q, got %q", originalInfo.Description, info.Description)
	}

	// Verify it's a copy (modifying shouldn't affect registry)
	info.Description = "modified"
	infoAgain := registry.GetMonitorInfo("test-monitor")
	if infoAgain.Description != originalInfo.Description {
		t.Error("GetMonitorInfo should return a copy")
	}
}

func TestRegistry_ValidateConfig(t *testing.T) {
	registry := NewRegistry()

	// Register test monitors
	registry.Register(MonitorInfo{
		Type:      "valid-monitor",
		Factory:   testFactory1,
		Validator: testValidator1,
	})
	registry.Register(MonitorInfo{
		Type:    "no-validator",
		Factory: testFactory2,
	})
	registry.Register(MonitorInfo{
		Type:      "failing-validator",
		Factory:   testFactory1,
		Validator: failingValidator,
	})

	tests := []struct {
		name    string
		config  types.MonitorConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config with validator",
			config: types.MonitorConfig{
				Name: "test",
				Type: "valid-monitor",
			},
			wantErr: false,
		},
		{
			name: "valid config without validator",
			config: types.MonitorConfig{
				Name: "test",
				Type: "no-validator",
			},
			wantErr: false,
		},
		{
			name: "empty type",
			config: types.MonitorConfig{
				Name: "test",
				Type: "",
			},
			wantErr: true,
			errMsg:  "monitor type is required",
		},
		{
			name: "unknown type",
			config: types.MonitorConfig{
				Name: "test",
				Type: "unknown",
			},
			wantErr: true,
			errMsg:  "unknown monitor type",
		},
		{
			name: "failing validator",
			config: types.MonitorConfig{
				Name: "test",
				Type: "failing-validator",
			},
			wantErr: true,
			errMsg:  "validation failed",
		},
		{
			name: "validator rejects empty name",
			config: types.MonitorConfig{
				Name: "",
				Type: "valid-monitor",
			},
			wantErr: true,
			errMsg:  "name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := registry.ValidateConfig(tt.config)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				} else if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
			}
		})
	}
}

func TestRegistry_CreateMonitor(t *testing.T) {
	registry := NewRegistry()

	// Register test monitors
	registry.Register(MonitorInfo{
		Type:    "success-monitor",
		Factory: testFactory1,
	})
	registry.Register(MonitorInfo{
		Type:    "failing-monitor",
		Factory: failingFactory,
	})
	registry.Register(MonitorInfo{
		Type:    "context-monitor",
		Factory: contextCancelFactory,
	})
	registry.Register(MonitorInfo{
		Type:    "panicking-monitor",
		Factory: panickingFactory,
	})

	tests := []struct {
		name       string
		config     types.MonitorConfig
		setupCtx   func() context.Context
		wantErr    bool
		errMsg     string
		verifyFunc func(t *testing.T, monitor types.Monitor)
	}{
		{
			name: "successful creation",
			config: types.MonitorConfig{
				Name:    "test",
				Type:    "success-monitor",
				Enabled: true,
			},
			setupCtx: func() context.Context { return context.Background() },
			wantErr:  false,
			verifyFunc: func(t *testing.T, monitor types.Monitor) {
				if monitor == nil {
					t.Error("Expected monitor, got nil")
				}
				if mock, ok := monitor.(*mockMonitor); ok {
					if mock.name != "test1" {
						t.Errorf("Expected monitor name 'test1', got %q", mock.name)
					}
				} else {
					t.Errorf("Expected *mockMonitor, got %T", monitor)
				}
			},
		},
		{
			name: "invalid config",
			config: types.MonitorConfig{
				Name: "test",
				Type: "",
			},
			setupCtx: func() context.Context { return context.Background() },
			wantErr:  true,
			errMsg:   "monitor type is required",
		},
		{
			name: "factory failure",
			config: types.MonitorConfig{
				Name:    "test",
				Type:    "failing-monitor",
				Enabled: true,
			},
			setupCtx: func() context.Context { return context.Background() },
			wantErr:  true,
			errMsg:   "factory failure",
		},
		{
			name: "context cancellation",
			config: types.MonitorConfig{
				Name:    "test",
				Type:    "context-monitor",
				Enabled: true,
			},
			setupCtx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel() // Cancel immediately
				return ctx
			},
			wantErr: true,
			errMsg:  "context canceled",
		},
		{
			name: "factory panic recovery",
			config: types.MonitorConfig{
				Name:    "test",
				Type:    "panicking-monitor",
				Enabled: true,
			},
			setupCtx: func() context.Context { return context.Background() },
			wantErr:  true,
			errMsg:   "panicked",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupCtx()
			monitor, err := registry.CreateMonitor(ctx, tt.config)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				} else if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
				if tt.verifyFunc != nil {
					tt.verifyFunc(t, monitor)
				}
			}
		})
	}
}

func TestRegistry_CreateMonitorsFromConfigs(t *testing.T) {
	registry := NewRegistry()

	// Register test monitors
	registry.Register(MonitorInfo{
		Type:    "success-monitor",
		Factory: testFactory1,
	})
	registry.Register(MonitorInfo{
		Type:    "failing-monitor",
		Factory: failingFactory,
	})

	tests := []struct {
		name     string
		configs  []types.MonitorConfig
		wantErr  bool
		errMsg   string
		expected int
	}{
		{
			name:     "empty configs",
			configs:  []types.MonitorConfig{},
			wantErr:  false,
			expected: 0,
		},
		{
			name:     "nil configs",
			configs:  nil,
			wantErr:  false,
			expected: 0,
		},
		{
			name: "all successful",
			configs: []types.MonitorConfig{
				{Name: "test1", Type: "success-monitor", Enabled: true},
				{Name: "test2", Type: "success-monitor", Enabled: true},
			},
			wantErr:  false,
			expected: 2,
		},
		{
			name: "skip disabled",
			configs: []types.MonitorConfig{
				{Name: "test1", Type: "success-monitor", Enabled: true},
				{Name: "test2", Type: "success-monitor", Enabled: false},
				{Name: "test3", Type: "success-monitor", Enabled: true},
			},
			wantErr:  false,
			expected: 2,
		},
		{
			name: "one failure",
			configs: []types.MonitorConfig{
				{Name: "test1", Type: "success-monitor", Enabled: true},
				{Name: "test2", Type: "failing-monitor", Enabled: true},
			},
			wantErr: true,
			errMsg:  "failed to create monitor 1",
		},
		{
			name: "unknown type",
			configs: []types.MonitorConfig{
				{Name: "test1", Type: "unknown-monitor", Enabled: true},
			},
			wantErr: true,
			errMsg:  "unknown monitor type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			monitors, err := registry.CreateMonitorsFromConfigs(ctx, tt.configs)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				} else if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
				if len(monitors) != tt.expected {
					t.Errorf("Expected %d monitors, got %d", tt.expected, len(monitors))
				}
			}
		})
	}
}

func TestRegistry_GetRegistryStats(t *testing.T) {
	registry := NewRegistry()

	// Test empty registry
	stats := registry.GetRegistryStats()
	if stats.RegisteredTypes != 0 {
		t.Errorf("Expected 0 registered types, got %d", stats.RegisteredTypes)
	}
	if len(stats.TypesList) != 0 {
		t.Errorf("Expected empty types list, got %v", stats.TypesList)
	}

	// Register some monitors
	monitors := []string{"monitor-c", "monitor-a", "monitor-b"}
	for _, monitorType := range monitors {
		registry.Register(MonitorInfo{
			Type:    monitorType,
			Factory: testFactory1,
		})
	}

	// Get stats
	stats = registry.GetRegistryStats()

	if stats.RegisteredTypes != 3 {
		t.Errorf("Expected 3 registered types, got %d", stats.RegisteredTypes)
	}

	expected := []string{"monitor-a", "monitor-b", "monitor-c"}
	if !reflect.DeepEqual(stats.TypesList, expected) {
		t.Errorf("Expected %v, got %v", expected, stats.TypesList)
	}
}

func TestPackageLevelFunctions(t *testing.T) {
	// Save original registry
	originalRegistry := DefaultRegistry
	defer func() {
		DefaultRegistry = originalRegistry
	}()

	// Use a clean registry for testing
	DefaultRegistry = NewRegistry()

	// Test Register
	info := MonitorInfo{
		Type:        "package-test",
		Factory:     testFactory1,
		Description: "Package level test",
	}
	Register(info)

	// Test GetRegisteredTypes
	registeredTypes := GetRegisteredTypes()
	if len(registeredTypes) != 1 || registeredTypes[0] != "package-test" {
		t.Errorf("Expected ['package-test'], got %v", registeredTypes)
	}

	// Test IsRegistered
	if !IsRegistered("package-test") {
		t.Error("IsRegistered should return true")
	}
	if IsRegistered("nonexistent") {
		t.Error("IsRegistered should return false for nonexistent type")
	}

	// Test GetMonitorInfo
	gotInfo := GetMonitorInfo("package-test")
	if gotInfo == nil {
		t.Fatal("GetMonitorInfo returned nil")
	}
	if gotInfo.Type != info.Type {
		t.Errorf("Expected type %q, got %q", info.Type, gotInfo.Type)
	}

	// Test ValidateConfig
	config := createTestConfig("package-test", "test")
	if err := ValidateConfig(config); err != nil {
		t.Errorf("ValidateConfig failed: %v", err)
	}

	// Test CreateMonitor
	ctx := context.Background()
	monitor, err := CreateMonitor(ctx, config)
	if err != nil {
		t.Errorf("CreateMonitor failed: %v", err)
	}
	if monitor == nil {
		t.Error("CreateMonitor returned nil monitor")
	}

	// Test CreateMonitorsFromConfigs
	configs := []types.MonitorConfig{config}
	monitors, err := CreateMonitorsFromConfigs(ctx, configs)
	if err != nil {
		t.Errorf("CreateMonitorsFromConfigs failed: %v", err)
	}
	if len(monitors) != 1 {
		t.Errorf("Expected 1 monitor, got %d", len(monitors))
	}

	// Test GetRegistryStats
	stats := GetRegistryStats()
	if stats.RegisteredTypes != 1 {
		t.Errorf("Expected 1 registered type, got %d", stats.RegisteredTypes)
	}
}

// Test concurrent registration (should be safe since it happens at init time)
func TestConcurrentRegistration(t *testing.T) {
	registry := NewRegistry()

	var wg sync.WaitGroup
	numGoroutines := 10

	// This test verifies that concurrent registration doesn't cause data races
	// In practice, registration only happens at init time, so true concurrency is rare
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Each goroutine registers a different monitor type
			registry.Register(MonitorInfo{
				Type:    fmt.Sprintf("concurrent-monitor-%d", id),
				Factory: testFactory1,
			})
		}(i)
	}

	wg.Wait()

	// Verify all monitors were registered
	registeredTypes := registry.GetRegisteredTypes()
	if len(registeredTypes) != numGoroutines {
		t.Errorf("Expected %d registered types, got %d", numGoroutines, len(registeredTypes))
	}
}

// Test concurrent monitor creation (common in real usage)
func TestConcurrentMonitorCreation(t *testing.T) {
	registry := NewRegistry()

	// Register a monitor
	registry.Register(MonitorInfo{
		Type:    "concurrent-test",
		Factory: testFactory1,
	})

	var wg sync.WaitGroup
	numGoroutines := 100
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			config := createTestConfig("concurrent-test", fmt.Sprintf("test-%d", id))
			ctx := context.Background()

			monitor, err := registry.CreateMonitor(ctx, config)
			if err != nil {
				errors <- err
				return
			}

			if monitor == nil {
				errors <- fmt.Errorf("monitor %d is nil", id)
				return
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for any errors
	for err := range errors {
		t.Errorf("Concurrent creation error: %v", err)
	}
}

// Race detector test - this specifically tests for data races
func TestRaceDetector(t *testing.T) {
	registry := NewRegistry()

	// Register a monitor
	registry.Register(MonitorInfo{
		Type:    "race-test",
		Factory: testFactory1,
	})

	var wg sync.WaitGroup

	// Start multiple goroutines doing different operations
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Read operations
			_ = registry.GetRegisteredTypes()
			_ = registry.IsRegistered("race-test")
			_ = registry.GetMonitorInfo("race-test")
			_ = registry.GetRegistryStats()

			// Validation and creation
			config := createTestConfig("race-test", "race-test")
			_ = registry.ValidateConfig(config)

			ctx := context.Background()
			monitor, err := registry.CreateMonitor(ctx, config)
			if err == nil && monitor != nil {
				// Use the monitor briefly
				_, _ = monitor.Start()
				monitor.Stop()
			}
		}()
	}

	wg.Wait()
}

// Benchmark tests
func BenchmarkRegistry_CreateMonitor(b *testing.B) {
	registry := NewRegistry()
	registry.Register(MonitorInfo{
		Type:    "benchmark-test",
		Factory: testFactory1,
	})

	config := createTestConfig("benchmark-test", "bench")
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			monitor, err := registry.CreateMonitor(ctx, config)
			if err != nil {
				b.Fatalf("CreateMonitor failed: %v", err)
			}
			if monitor == nil {
				b.Fatal("Monitor is nil")
			}
		}
	})
}

func BenchmarkRegistry_GetRegisteredTypes(b *testing.B) {
	registry := NewRegistry()

	// Register many monitors
	for i := 0; i < 100; i++ {
		registry.Register(MonitorInfo{
			Type:    fmt.Sprintf("benchmark-monitor-%d", i),
			Factory: testFactory1,
		})
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			registeredTypes := registry.GetRegisteredTypes()
			if len(registeredTypes) != 100 {
				b.Fatalf("Expected 100 types, got %d", len(registeredTypes))
			}
		}
	})
}