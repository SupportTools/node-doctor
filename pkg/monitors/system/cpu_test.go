package system

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// mockFileReader implements FileReader for testing
type mockFileReader struct {
	files map[string]string
	dirs  map[string][]os.DirEntry
}

func newMockFileReader() *mockFileReader {
	return &mockFileReader{
		files: make(map[string]string),
		dirs:  make(map[string][]os.DirEntry),
	}
}

func (m *mockFileReader) ReadFile(path string) ([]byte, error) {
	content, exists := m.files[path]
	if !exists {
		return nil, os.ErrNotExist
	}
	return []byte(content), nil
}

func (m *mockFileReader) ReadDir(path string) ([]os.DirEntry, error) {
	entries, exists := m.dirs[path]
	if !exists {
		return nil, os.ErrNotExist
	}
	return entries, nil
}

func (m *mockFileReader) setFile(path, content string) {
	m.files[path] = content
}

func (m *mockFileReader) setDir(path string, entries []os.DirEntry) {
	m.dirs[path] = entries
}

// mockDirEntry implements os.DirEntry for testing
type mockDirEntry struct {
	name  string
	isDir bool
}

func (m mockDirEntry) Name() string               { return m.name }
func (m mockDirEntry) IsDir() bool                { return m.isDir }
func (m mockDirEntry) Type() os.FileMode          { return 0 }
func (m mockDirEntry) Info() (os.FileInfo, error) { return nil, nil }

func TestParseCPUConfig(t *testing.T) {
	tests := []struct {
		name      string
		configMap map[string]interface{}
		expected  *CPUMonitorConfig
		wantError bool
	}{
		{
			name:      "nil config",
			configMap: nil,
			expected:  &CPUMonitorConfig{},
			wantError: false,
		},
		{
			name:      "empty config",
			configMap: map[string]interface{}{},
			expected:  &CPUMonitorConfig{},
			wantError: false,
		},
		{
			name: "valid config with all fields",
			configMap: map[string]interface{}{
				"warningLoadFactor":       0.7,
				"criticalLoadFactor":      1.2,
				"sustainedHighLoadChecks": 5,
				"checkThermalThrottle":    true,
				"checkLoadAverage":        false,
				"loadAvgPath":             "/custom/loadavg",
				"cpuInfoPath":             "/custom/cpuinfo",
				"thermalBasePath":         "/custom/thermal",
			},
			expected: &CPUMonitorConfig{
				WarningLoadFactor:       0.7,
				CriticalLoadFactor:      1.2,
				SustainedHighLoadChecks: 5,
				CheckThermalThrottle:    true,
				CheckLoadAverage:        false,
				LoadAvgPath:             "/custom/loadavg",
				CPUInfoPath:             "/custom/cpuinfo",
				ThermalBasePath:         "/custom/thermal",
			},
			wantError: false,
		},
		{
			name: "invalid warning load factor type",
			configMap: map[string]interface{}{
				"warningLoadFactor": "invalid",
			},
			wantError: true,
		},
		{
			name: "invalid critical load factor type",
			configMap: map[string]interface{}{
				"criticalLoadFactor": "invalid",
			},
			wantError: true,
		},
		{
			name: "invalid sustained high load checks type",
			configMap: map[string]interface{}{
				"sustainedHighLoadChecks": "invalid",
			},
			wantError: true,
		},
		{
			name: "sustained high load checks as float",
			configMap: map[string]interface{}{
				"sustainedHighLoadChecks": 5.0,
			},
			expected: &CPUMonitorConfig{
				SustainedHighLoadChecks: 5,
			},
			wantError: false,
		},
		{
			name: "invalid check thermal throttle type",
			configMap: map[string]interface{}{
				"checkThermalThrottle": "invalid",
			},
			wantError: true,
		},
		{
			name: "invalid check load average type",
			configMap: map[string]interface{}{
				"checkLoadAverage": "invalid",
			},
			wantError: true,
		},
		{
			name: "invalid load avg path type",
			configMap: map[string]interface{}{
				"loadAvgPath": 123,
			},
			wantError: true,
		},
		{
			name: "invalid cpu info path type",
			configMap: map[string]interface{}{
				"cpuInfoPath": 123,
			},
			wantError: true,
		},
		{
			name: "invalid thermal base path type",
			configMap: map[string]interface{}{
				"thermalBasePath": 123,
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseCPUConfig(tt.configMap)

			if tt.wantError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result.WarningLoadFactor != tt.expected.WarningLoadFactor {
				t.Errorf("WarningLoadFactor: got %f, want %f", result.WarningLoadFactor, tt.expected.WarningLoadFactor)
			}
			if result.CriticalLoadFactor != tt.expected.CriticalLoadFactor {
				t.Errorf("CriticalLoadFactor: got %f, want %f", result.CriticalLoadFactor, tt.expected.CriticalLoadFactor)
			}
			if result.SustainedHighLoadChecks != tt.expected.SustainedHighLoadChecks {
				t.Errorf("SustainedHighLoadChecks: got %d, want %d", result.SustainedHighLoadChecks, tt.expected.SustainedHighLoadChecks)
			}
			if result.CheckThermalThrottle != tt.expected.CheckThermalThrottle {
				t.Errorf("CheckThermalThrottle: got %v, want %v", result.CheckThermalThrottle, tt.expected.CheckThermalThrottle)
			}
			if result.CheckLoadAverage != tt.expected.CheckLoadAverage {
				t.Errorf("CheckLoadAverage: got %v, want %v", result.CheckLoadAverage, tt.expected.CheckLoadAverage)
			}
			if result.LoadAvgPath != tt.expected.LoadAvgPath {
				t.Errorf("LoadAvgPath: got %q, want %q", result.LoadAvgPath, tt.expected.LoadAvgPath)
			}
			if result.CPUInfoPath != tt.expected.CPUInfoPath {
				t.Errorf("CPUInfoPath: got %q, want %q", result.CPUInfoPath, tt.expected.CPUInfoPath)
			}
			if result.ThermalBasePath != tt.expected.ThermalBasePath {
				t.Errorf("ThermalBasePath: got %q, want %q", result.ThermalBasePath, tt.expected.ThermalBasePath)
			}
		})
	}
}

func TestCPUMonitorConfigApplyDefaults(t *testing.T) {
	config := &CPUMonitorConfig{}
	err := config.applyDefaults()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if config.WarningLoadFactor != 0.8 {
		t.Errorf("WarningLoadFactor: got %f, want 0.8", config.WarningLoadFactor)
	}
	if config.CriticalLoadFactor != 1.5 {
		t.Errorf("CriticalLoadFactor: got %f, want 1.5", config.CriticalLoadFactor)
	}
	if config.SustainedHighLoadChecks != 3 {
		t.Errorf("SustainedHighLoadChecks: got %d, want 3", config.SustainedHighLoadChecks)
	}
	if config.LoadAvgPath != "/proc/loadavg" {
		t.Errorf("LoadAvgPath: got %q, want /proc/loadavg", config.LoadAvgPath)
	}
	if config.CPUInfoPath != "/proc/cpuinfo" {
		t.Errorf("CPUInfoPath: got %q, want /proc/cpuinfo", config.CPUInfoPath)
	}
	if config.ThermalBasePath != "/sys/devices/system/cpu" {
		t.Errorf("ThermalBasePath: got %q, want /sys/devices/system/cpu", config.ThermalBasePath)
	}
	if !config.CheckLoadAverage {
		t.Errorf("CheckLoadAverage: got false, want true")
	}
	if !config.CheckThermalThrottle {
		t.Errorf("CheckThermalThrottle: got false, want true")
	}
}

func TestValidateCPUConfig(t *testing.T) {
	tests := []struct {
		name      string
		config    types.MonitorConfig
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid config",
			config: types.MonitorConfig{
				Name: "test-cpu",
				Type: "system-cpu",
				Config: map[string]interface{}{
					"warningLoadFactor":  0.8,
					"criticalLoadFactor": 1.5,
				},
			},
			wantError: false,
		},
		{
			name: "missing name",
			config: types.MonitorConfig{
				Type: "system-cpu",
			},
			wantError: true,
			errorMsg:  "monitor name is required",
		},
		{
			name: "wrong type",
			config: types.MonitorConfig{
				Name: "test-cpu",
				Type: "wrong-type",
			},
			wantError: true,
			errorMsg:  "invalid monitor type",
		},
		{
			name: "invalid warning load factor",
			config: types.MonitorConfig{
				Name: "test-cpu",
				Type: "system-cpu",
				Config: map[string]interface{}{
					"warningLoadFactor": -0.5,
				},
			},
			wantError: true,
			errorMsg:  "warningLoadFactor must be positive",
		},
		{
			name: "invalid critical load factor",
			config: types.MonitorConfig{
				Name: "test-cpu",
				Type: "system-cpu",
				Config: map[string]interface{}{
					"criticalLoadFactor": -1.0,
				},
			},
			wantError: true,
			errorMsg:  "criticalLoadFactor must be positive",
		},
		{
			name: "warning >= critical",
			config: types.MonitorConfig{
				Name: "test-cpu",
				Type: "system-cpu",
				Config: map[string]interface{}{
					"warningLoadFactor":  1.5,
					"criticalLoadFactor": 1.0,
				},
			},
			wantError: true,
			errorMsg:  "warningLoadFactor",
		},
		{
			name: "invalid sustained high load checks",
			config: types.MonitorConfig{
				Name: "test-cpu",
				Type: "system-cpu",
				Config: map[string]interface{}{
					"sustainedHighLoadChecks": -1,
				},
			},
			wantError: true,
			errorMsg:  "sustainedHighLoadChecks must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCPUConfig(tt.config)

			if tt.wantError {
				if err == nil {
					t.Errorf("expected error but got none")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error to contain %q but got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestParseLoadAvg(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		expected  *LoadAverage
		wantError bool
	}{
		{
			name:    "valid load average",
			content: "0.08 0.03 0.05 1/180 12345",
			expected: &LoadAverage{
				Load1:  0.08,
				Load5:  0.03,
				Load15: 0.05,
			},
			wantError: false,
		},
		{
			name:    "high load average",
			content: "4.75 3.21 2.15 5/240 98765",
			expected: &LoadAverage{
				Load1:  4.75,
				Load5:  3.21,
				Load15: 2.15,
			},
			wantError: false,
		},
		{
			name:      "insufficient fields",
			content:   "0.08 0.03",
			wantError: true,
		},
		{
			name:      "invalid first field",
			content:   "invalid 0.03 0.05 1/180 12345",
			wantError: true,
		},
		{
			name:      "invalid second field",
			content:   "0.08 invalid 0.05 1/180 12345",
			wantError: true,
		},
		{
			name:      "invalid third field",
			content:   "0.08 0.03 invalid 1/180 12345",
			wantError: true,
		},
		{
			name:      "empty content",
			content:   "",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockReader := newMockFileReader()
			mockReader.setFile("/proc/loadavg", tt.content)

			monitor := &CPUMonitor{
				config: &CPUMonitorConfig{
					LoadAvgPath: "/proc/loadavg",
				},
				fileReader: mockReader,
			}

			result, err := monitor.parseLoadAvg()

			if tt.wantError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result.Load1 != tt.expected.Load1 {
				t.Errorf("Load1: got %f, want %f", result.Load1, tt.expected.Load1)
			}
			if result.Load5 != tt.expected.Load5 {
				t.Errorf("Load5: got %f, want %f", result.Load5, tt.expected.Load5)
			}
			if result.Load15 != tt.expected.Load15 {
				t.Errorf("Load15: got %f, want %f", result.Load15, tt.expected.Load15)
			}
		})
	}
}

func TestGetCPUCount(t *testing.T) {
	tests := []struct {
		name         string
		cpuInfoData  string
		fileExists   bool
		expectedMin  int
		wantError    bool
	}{
		{
			name: "valid cpuinfo with 4 cores",
			cpuInfoData: `processor	: 0
vendor_id	: GenuineIntel
cpu family	: 6

processor	: 1
vendor_id	: GenuineIntel
cpu family	: 6

processor	: 2
vendor_id	: GenuineIntel
cpu family	: 6

processor	: 3
vendor_id	: GenuineIntel
cpu family	: 6`,
			fileExists:  true,
			expectedMin: 4,
			wantError:   false,
		},
		{
			name: "valid cpuinfo with 2 cores",
			cpuInfoData: `processor	: 0
vendor_id	: GenuineIntel

processor	: 1
vendor_id	: GenuineIntel`,
			fileExists:  true,
			expectedMin: 2,
			wantError:   false,
		},
		{
			name:         "file not exists - fallback to runtime",
			fileExists:   false,
			expectedMin:  1, // runtime.NumCPU() should return at least 1
			wantError:    false,
		},
		{
			name:        "empty cpuinfo - fallback to runtime",
			cpuInfoData: "",
			fileExists:  true,
			expectedMin: 1,
			wantError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockReader := newMockFileReader()
			if tt.fileExists {
				mockReader.setFile("/proc/cpuinfo", tt.cpuInfoData)
			}

			monitor := &CPUMonitor{
				config: &CPUMonitorConfig{
					CPUInfoPath: "/proc/cpuinfo",
				},
				fileReader: mockReader,
			}

			result, err := monitor.getCPUCount()

			if tt.wantError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result < tt.expectedMin {
				t.Errorf("CPU count: got %d, want at least %d", result, tt.expectedMin)
			}
		})
	}
}

func TestCheckLoadAverage(t *testing.T) {
	tests := []struct {
		name              string
		loadAvgContent    string
		cpuInfoContent    string
		highLoadCount     int
		expectedEvents    int
		expectedConditions int
		expectedHighLoadCount int
		conditionStatus   types.ConditionStatus
	}{
		{
			name:              "normal load",
			loadAvgContent:    "0.50 0.40 0.30 1/180 12345",
			cpuInfoContent:    "processor : 0\nprocessor : 1\nprocessor : 2\nprocessor : 3", // 4 cores
			highLoadCount:     0,
			expectedEvents:    0,
			expectedConditions: 1,
			expectedHighLoadCount: 0,
			conditionStatus:   types.ConditionFalse, // No pressure
		},
		{
			name:              "elevated load",
			loadAvgContent:    "3.50 3.00 2.50 1/180 12345", // 3.5/4 = 0.875 > 0.8 warning threshold
			cpuInfoContent:    "processor : 0\nprocessor : 1\nprocessor : 2\nprocessor : 3",
			highLoadCount:     0,
			expectedEvents:    1,
			expectedConditions: 1,
			expectedHighLoadCount: 0,
			conditionStatus:   types.ConditionFalse, // Elevated but not critical
		},
		{
			name:              "critical load - first occurrence",
			loadAvgContent:    "7.00 6.00 5.00 1/180 12345", // 7.0/4 = 1.75 > 1.5 critical threshold
			cpuInfoContent:    "processor : 0\nprocessor : 1\nprocessor : 2\nprocessor : 3",
			highLoadCount:     0,
			expectedEvents:    1,
			expectedConditions: 0, // No condition until sustained
			expectedHighLoadCount: 1,
		},
		{
			name:              "critical load - sustained (3rd occurrence)",
			loadAvgContent:    "7.00 6.00 5.00 1/180 12345",
			cpuInfoContent:    "processor : 0\nprocessor : 1\nprocessor : 2\nprocessor : 3",
			highLoadCount:     2, // This will be the 3rd occurrence
			expectedEvents:    1,
			expectedConditions: 1,
			expectedHighLoadCount: 3,
			conditionStatus:   types.ConditionTrue, // Sustained pressure
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockReader := newMockFileReader()
			mockReader.setFile("/proc/loadavg", tt.loadAvgContent)
			mockReader.setFile("/proc/cpuinfo", tt.cpuInfoContent)

			config := &CPUMonitorConfig{
				WarningLoadFactor:       0.8,
				CriticalLoadFactor:      1.5,
				SustainedHighLoadChecks: 3,
				CheckLoadAverage:        true,
				LoadAvgPath:             "/proc/loadavg",
				CPUInfoPath:             "/proc/cpuinfo",
			}

			monitor := &CPUMonitor{
				name:          "test-cpu",
				config:        config,
				highLoadCount: tt.highLoadCount,
				fileReader:    mockReader,
			}

			status := types.NewStatus("test-cpu")
			err := monitor.checkLoadAverage(context.Background(), status)

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if len(status.Events) != tt.expectedEvents {
				t.Errorf("Events: got %d, want %d", len(status.Events), tt.expectedEvents)
			}

			if len(status.Conditions) != tt.expectedConditions {
				t.Errorf("Conditions: got %d, want %d", len(status.Conditions), tt.expectedConditions)
			}

			if monitor.highLoadCount != tt.expectedHighLoadCount {
				t.Errorf("HighLoadCount: got %d, want %d", monitor.highLoadCount, tt.expectedHighLoadCount)
			}

			// Check condition status if we expect a condition
			if tt.expectedConditions > 0 && len(status.Conditions) > 0 {
				condition := status.Conditions[0]
				if condition.Status != tt.conditionStatus {
					t.Errorf("Condition status: got %s, want %s", condition.Status, tt.conditionStatus)
				}
				if condition.Type != "CPUPressure" {
					t.Errorf("Condition type: got %s, want CPUPressure", condition.Type)
				}
			}
		})
	}
}

func TestCheckThermalThrottle(t *testing.T) {
	tests := []struct {
		name               string
		setupMock          func(*mockFileReader)
		initialThrottleState map[int]int64
		expectedEvents     int
		expectedConditions int
		conditionStatus    types.ConditionStatus
	}{
		{
			name: "no thermal directory",
			setupMock: func(m *mockFileReader) {
				// Don't set up any directories
			},
			initialThrottleState: nil,
			expectedEvents:       0,
			expectedConditions:   0,
		},
		{
			name: "no new throttling",
			setupMock: func(m *mockFileReader) {
				m.setDir("/sys/devices/system/cpu", []os.DirEntry{
					mockDirEntry{name: "cpu0", isDir: true},
					mockDirEntry{name: "cpu1", isDir: true},
				})
				m.setFile("/sys/devices/system/cpu/cpu0/thermal_throttle/core_throttle_count", "100")
				m.setFile("/sys/devices/system/cpu/cpu1/thermal_throttle/core_throttle_count", "50")
			},
			initialThrottleState: map[int]int64{0: 100, 1: 50},
			expectedEvents:       0,
			expectedConditions:   1,
			conditionStatus:      types.ConditionTrue, // Healthy
		},
		{
			name: "new throttling detected",
			setupMock: func(m *mockFileReader) {
				m.setDir("/sys/devices/system/cpu", []os.DirEntry{
					mockDirEntry{name: "cpu0", isDir: true},
					mockDirEntry{name: "cpu1", isDir: true},
				})
				m.setFile("/sys/devices/system/cpu/cpu0/thermal_throttle/core_throttle_count", "105")
				m.setFile("/sys/devices/system/cpu/cpu1/thermal_throttle/core_throttle_count", "53")
			},
			initialThrottleState: map[int]int64{0: 100, 1: 50},
			expectedEvents:       1,
			expectedConditions:   1,
			conditionStatus:      types.ConditionFalse, // Unhealthy
		},
		{
			name: "first time check with throttling",
			setupMock: func(m *mockFileReader) {
				m.setDir("/sys/devices/system/cpu", []os.DirEntry{
					mockDirEntry{name: "cpu0", isDir: true},
				})
				m.setFile("/sys/devices/system/cpu/cpu0/thermal_throttle/core_throttle_count", "10")
			},
			initialThrottleState: nil,
			expectedEvents:       0,
			expectedConditions:   1,
			conditionStatus:      types.ConditionTrue, // Healthy (no previous state to compare)
		},
		{
			name: "mixed cpu directories",
			setupMock: func(m *mockFileReader) {
				m.setDir("/sys/devices/system/cpu", []os.DirEntry{
					mockDirEntry{name: "cpu0", isDir: true},
					mockDirEntry{name: "cpuidle", isDir: true}, // Should be ignored
					mockDirEntry{name: "some_file", isDir: false}, // Should be ignored
					mockDirEntry{name: "cpu1", isDir: true},
				})
				m.setFile("/sys/devices/system/cpu/cpu0/thermal_throttle/core_throttle_count", "100")
				m.setFile("/sys/devices/system/cpu/cpu1/thermal_throttle/core_throttle_count", "50")
			},
			initialThrottleState: map[int]int64{0: 100, 1: 50},
			expectedEvents:       0,
			expectedConditions:   1,
			conditionStatus:      types.ConditionTrue,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockReader := newMockFileReader()
			tt.setupMock(mockReader)

			config := &CPUMonitorConfig{
				CheckThermalThrottle: true,
				ThermalBasePath:      "/sys/devices/system/cpu",
			}

			monitor := &CPUMonitor{
				name:              "test-cpu",
				config:            config,
				lastThrottleState: make(map[int]int64),
				fileReader:        mockReader,
			}

			// Set initial throttle state
			if tt.initialThrottleState != nil {
				for cpu, count := range tt.initialThrottleState {
					monitor.lastThrottleState[cpu] = count
				}
			}

			status := types.NewStatus("test-cpu")
			err := monitor.checkThermalThrottle(context.Background(), status)

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if len(status.Events) != tt.expectedEvents {
				t.Errorf("Events: got %d, want %d", len(status.Events), tt.expectedEvents)
			}

			if len(status.Conditions) != tt.expectedConditions {
				t.Errorf("Conditions: got %d, want %d", len(status.Conditions), tt.expectedConditions)
			}

			// Check condition status if we expect a condition
			if tt.expectedConditions > 0 && len(status.Conditions) > 0 {
				condition := status.Conditions[0]
				if condition.Status != tt.conditionStatus {
					t.Errorf("Condition status: got %s, want %s", condition.Status, tt.conditionStatus)
				}
				if condition.Type != "CPUThermalHealthy" {
					t.Errorf("Condition type: got %s, want CPUThermalHealthy", condition.Type)
				}
			}
		})
	}
}

func TestNewCPUMonitor(t *testing.T) {
	tests := []struct {
		name      string
		config    types.MonitorConfig
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid config",
			config: types.MonitorConfig{
				Name:     "test-cpu",
				Type:     "system-cpu",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Config: map[string]interface{}{
					"warningLoadFactor":  0.8,
					"criticalLoadFactor": 1.5,
				},
			},
			wantError: false,
		},
		{
			name: "invalid config",
			config: types.MonitorConfig{
				Name:     "test-cpu",
				Type:     "system-cpu",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Config: map[string]interface{}{
					"warningLoadFactor": "invalid",
				},
			},
			wantError: true,
			errorMsg:  "failed to parse CPU config",
		},
		{
			name: "invalid interval",
			config: types.MonitorConfig{
				Name:     "test-cpu",
				Type:     "system-cpu",
				Interval: 10 * time.Second,
				Timeout:  20 * time.Second, // timeout > interval
			},
			wantError: true,
			errorMsg:  "timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			monitor, err := NewCPUMonitor(ctx, tt.config)

			if tt.wantError {
				if err == nil {
					t.Errorf("expected error but got none")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error to contain %q but got %q", tt.errorMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if monitor == nil {
				t.Errorf("monitor is nil")
				return
			}

			// Verify it implements the Monitor interface
			_, ok := monitor.(types.Monitor)
			if !ok {
				t.Errorf("monitor does not implement types.Monitor interface")
			}
		})
	}
}

func TestCPUMonitorIntegration(t *testing.T) {
	// Create a minimal config
	config := types.MonitorConfig{
		Name:     "integration-test-cpu",
		Type:     "system-cpu",
		Interval: 100 * time.Millisecond,
		Timeout:  50 * time.Millisecond,
		Config: map[string]interface{}{
			"checkLoadAverage":     true,
			"checkThermalThrottle": false, // Disable thermal for simpler test
		},
	}

	ctx := context.Background()
	monitor, err := NewCPUMonitor(ctx, config)
	if err != nil {
		t.Fatalf("failed to create monitor: %v", err)
	}

	// Setup mock reader for the monitor
	cpuMonitor := monitor.(*CPUMonitor)
	mockReader := newMockFileReader()
	mockReader.setFile("/proc/loadavg", "0.50 0.40 0.30 1/180 12345")
	mockReader.setFile("/proc/cpuinfo", "processor : 0\nprocessor : 1") // 2 cores
	cpuMonitor.fileReader = mockReader

	// Start the monitor
	statusCh, err := monitor.Start()
	if err != nil {
		t.Fatalf("failed to start monitor: %v", err)
	}

	// Wait for at least one status update
	select {
	case status := <-statusCh:
		if status == nil {
			t.Errorf("received nil status")
		} else {
			if status.Source != "integration-test-cpu" {
				t.Errorf("status source: got %q, want %q", status.Source, "integration-test-cpu")
			}
			// Should have at least one condition (CPU pressure or healthy)
			if len(status.Conditions) == 0 {
				t.Errorf("expected at least one condition")
			}
		}
	case <-time.After(200 * time.Millisecond):
		t.Errorf("timeout waiting for status update")
	}

	// Stop the monitor
	monitor.Stop()

	// Verify channel is closed
	select {
	case _, ok := <-statusCh:
		if ok {
			t.Errorf("status channel should be closed after stop")
		}
	case <-time.After(100 * time.Millisecond):
		t.Errorf("timeout waiting for channel to close")
	}
}

func TestCPUMonitorErrorHandling(t *testing.T) {
	tests := []struct {
		name           string
		setupMock      func(*mockFileReader)
		expectedEvents int
	}{
		{
			name: "load average file not found",
			setupMock: func(m *mockFileReader) {
				// Don't set up loadavg file
				m.setFile("/proc/cpuinfo", "processor : 0")
			},
			expectedEvents: 1, // Should generate error event
		},
		{
			name: "cpu info file not found",
			setupMock: func(m *mockFileReader) {
				m.setFile("/proc/loadavg", "0.50 0.40 0.30 1/180 12345")
				// Don't set up cpuinfo file - should fallback to runtime.NumCPU()
			},
			expectedEvents: 0, // Should work with fallback
		},
		{
			name: "malformed load average",
			setupMock: func(m *mockFileReader) {
				m.setFile("/proc/loadavg", "invalid data")
				m.setFile("/proc/cpuinfo", "processor : 0")
			},
			expectedEvents: 1, // Should generate error event
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &CPUMonitorConfig{
				CheckLoadAverage:        true,
				CheckThermalThrottle:    false,
				LoadAvgPath:             "/proc/loadavg",
				CPUInfoPath:             "/proc/cpuinfo",
				WarningLoadFactor:       0.8,
				CriticalLoadFactor:      1.5,
				SustainedHighLoadChecks: 3,
			}

			mockReader := newMockFileReader()
			tt.setupMock(mockReader)

			monitor := &CPUMonitor{
				name:       "test-cpu",
				config:     config,
				fileReader: mockReader,
			}

			_, err := monitor.checkCPU(context.Background())

			// The monitor should handle errors gracefully and not return errors
			if err != nil {
				t.Errorf("checkCPU should not return errors, got: %v", err)
			}
		})
	}
}

// Benchmark tests
func BenchmarkParseLoadAvg(b *testing.B) {
	mockReader := newMockFileReader()
	mockReader.setFile("/proc/loadavg", "0.08 0.03 0.05 1/180 12345")

	monitor := &CPUMonitor{
		config: &CPUMonitorConfig{
			LoadAvgPath: "/proc/loadavg",
		},
		fileReader: mockReader,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := monitor.parseLoadAvg()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetCPUCount(b *testing.B) {
	mockReader := newMockFileReader()
	mockReader.setFile("/proc/cpuinfo", "processor : 0\nprocessor : 1\nprocessor : 2\nprocessor : 3")

	monitor := &CPUMonitor{
		config: &CPUMonitorConfig{
			CPUInfoPath: "/proc/cpuinfo",
		},
		fileReader: mockReader,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := monitor.getCPUCount()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCheckLoadAverage(b *testing.B) {
	mockReader := newMockFileReader()
	mockReader.setFile("/proc/loadavg", "0.50 0.40 0.30 1/180 12345")
	mockReader.setFile("/proc/cpuinfo", "processor : 0\nprocessor : 1\nprocessor : 2\nprocessor : 3")

	config := &CPUMonitorConfig{
		WarningLoadFactor:       0.8,
		CriticalLoadFactor:      1.5,
		SustainedHighLoadChecks: 3,
		CheckLoadAverage:        true,
		LoadAvgPath:             "/proc/loadavg",
		CPUInfoPath:             "/proc/cpuinfo",
	}

	monitor := &CPUMonitor{
		name:       "bench-cpu",
		config:     config,
		fileReader: mockReader,
	}

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		status := types.NewStatus("bench-cpu")
		err := monitor.checkLoadAverage(ctx, status)
		if err != nil {
			b.Fatal(err)
		}
	}
}