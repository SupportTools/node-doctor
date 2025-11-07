package system

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

func TestParseMemoryConfig(t *testing.T) {
	tests := []struct {
		name      string
		configMap map[string]interface{}
		expected  *MemoryMonitorConfig
		wantError bool
	}{
		{
			name:      "nil config",
			configMap: nil,
			expected:  &MemoryMonitorConfig{},
			wantError: false,
		},
		{
			name:      "empty config",
			configMap: map[string]interface{}{},
			expected:  &MemoryMonitorConfig{},
			wantError: false,
		},
		{
			name: "valid config with all fields",
			configMap: map[string]interface{}{
				"warningThreshold":          75.0,
				"criticalThreshold":         90.0,
				"swapWarningThreshold":      40.0,
				"swapCriticalThreshold":     70.0,
				"sustainedHighMemoryChecks": 5,
				"checkOOMKills":             true,
				"checkMemoryUsage":          false,
				"checkSwapUsage":            false,
				"memInfoPath":               "/custom/meminfo",
				"kmsgPath":                  "/custom/kmsg",
			},
			expected: &MemoryMonitorConfig{
				WarningThreshold:          75.0,
				CriticalThreshold:         90.0,
				SwapWarningThreshold:      40.0,
				SwapCriticalThreshold:     70.0,
				SustainedHighMemoryChecks: 5,
				CheckOOMKills:             true,
				CheckMemoryUsage:          false,
				CheckSwapUsage:            false,
				MemInfoPath:               "/custom/meminfo",
				KmsgPath:                  "/custom/kmsg",
			},
			wantError: false,
		},
		{
			name: "invalid warning threshold type",
			configMap: map[string]interface{}{
				"warningThreshold": "invalid",
			},
			wantError: true,
		},
		{
			name: "invalid critical threshold type",
			configMap: map[string]interface{}{
				"criticalThreshold": "invalid",
			},
			wantError: true,
		},
		{
			name: "invalid swap warning threshold type",
			configMap: map[string]interface{}{
				"swapWarningThreshold": "invalid",
			},
			wantError: true,
		},
		{
			name: "invalid swap critical threshold type",
			configMap: map[string]interface{}{
				"swapCriticalThreshold": "invalid",
			},
			wantError: true,
		},
		{
			name: "invalid sustained high memory checks type",
			configMap: map[string]interface{}{
				"sustainedHighMemoryChecks": "invalid",
			},
			wantError: true,
		},
		{
			name: "sustained high memory checks as float",
			configMap: map[string]interface{}{
				"sustainedHighMemoryChecks": 5.0,
			},
			expected: &MemoryMonitorConfig{
				SustainedHighMemoryChecks: 5,
			},
			wantError: false,
		},
		{
			name: "invalid check OOM kills type",
			configMap: map[string]interface{}{
				"checkOOMKills": "invalid",
			},
			wantError: true,
		},
		{
			name: "invalid check memory usage type",
			configMap: map[string]interface{}{
				"checkMemoryUsage": "invalid",
			},
			wantError: true,
		},
		{
			name: "invalid check swap usage type",
			configMap: map[string]interface{}{
				"checkSwapUsage": "invalid",
			},
			wantError: true,
		},
		{
			name: "invalid meminfo path type",
			configMap: map[string]interface{}{
				"memInfoPath": 123,
			},
			wantError: true,
		},
		{
			name: "invalid kmsg path type",
			configMap: map[string]interface{}{
				"kmsgPath": 123,
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseMemoryConfig(tt.configMap)

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

			if result.WarningThreshold != tt.expected.WarningThreshold {
				t.Errorf("WarningThreshold: got %f, want %f", result.WarningThreshold, tt.expected.WarningThreshold)
			}
			if result.CriticalThreshold != tt.expected.CriticalThreshold {
				t.Errorf("CriticalThreshold: got %f, want %f", result.CriticalThreshold, tt.expected.CriticalThreshold)
			}
			if result.SwapWarningThreshold != tt.expected.SwapWarningThreshold {
				t.Errorf("SwapWarningThreshold: got %f, want %f", result.SwapWarningThreshold, tt.expected.SwapWarningThreshold)
			}
			if result.SwapCriticalThreshold != tt.expected.SwapCriticalThreshold {
				t.Errorf("SwapCriticalThreshold: got %f, want %f", result.SwapCriticalThreshold, tt.expected.SwapCriticalThreshold)
			}
			if result.SustainedHighMemoryChecks != tt.expected.SustainedHighMemoryChecks {
				t.Errorf("SustainedHighMemoryChecks: got %d, want %d", result.SustainedHighMemoryChecks, tt.expected.SustainedHighMemoryChecks)
			}
			if result.CheckOOMKills != tt.expected.CheckOOMKills {
				t.Errorf("CheckOOMKills: got %v, want %v", result.CheckOOMKills, tt.expected.CheckOOMKills)
			}
			if result.CheckMemoryUsage != tt.expected.CheckMemoryUsage {
				t.Errorf("CheckMemoryUsage: got %v, want %v", result.CheckMemoryUsage, tt.expected.CheckMemoryUsage)
			}
			if result.CheckSwapUsage != tt.expected.CheckSwapUsage {
				t.Errorf("CheckSwapUsage: got %v, want %v", result.CheckSwapUsage, tt.expected.CheckSwapUsage)
			}
			if result.MemInfoPath != tt.expected.MemInfoPath {
				t.Errorf("MemInfoPath: got %q, want %q", result.MemInfoPath, tt.expected.MemInfoPath)
			}
			if result.KmsgPath != tt.expected.KmsgPath {
				t.Errorf("KmsgPath: got %q, want %q", result.KmsgPath, tt.expected.KmsgPath)
			}
		})
	}
}

func TestMemoryMonitorConfigApplyDefaults(t *testing.T) {
	config := &MemoryMonitorConfig{}
	err := config.applyDefaults()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if config.WarningThreshold != 85.0 {
		t.Errorf("WarningThreshold: got %f, want 85.0", config.WarningThreshold)
	}
	if config.CriticalThreshold != 95.0 {
		t.Errorf("CriticalThreshold: got %f, want 95.0", config.CriticalThreshold)
	}
	if config.SwapWarningThreshold != 50.0 {
		t.Errorf("SwapWarningThreshold: got %f, want 50.0", config.SwapWarningThreshold)
	}
	if config.SwapCriticalThreshold != 80.0 {
		t.Errorf("SwapCriticalThreshold: got %f, want 80.0", config.SwapCriticalThreshold)
	}
	if config.SustainedHighMemoryChecks != 3 {
		t.Errorf("SustainedHighMemoryChecks: got %d, want 3", config.SustainedHighMemoryChecks)
	}
	if config.MemInfoPath != "/proc/meminfo" {
		t.Errorf("MemInfoPath: got %q, want /proc/meminfo", config.MemInfoPath)
	}
	if config.KmsgPath != "/dev/kmsg" {
		t.Errorf("KmsgPath: got %q, want /dev/kmsg", config.KmsgPath)
	}
	if !config.CheckMemoryUsage {
		t.Errorf("CheckMemoryUsage: got false, want true")
	}
	if !config.CheckSwapUsage {
		t.Errorf("CheckSwapUsage: got false, want true")
	}
	if !config.CheckOOMKills {
		t.Errorf("CheckOOMKills: got false, want true")
	}
}

func TestValidateMemoryConfig(t *testing.T) {
	tests := []struct {
		name      string
		config    types.MonitorConfig
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid config",
			config: types.MonitorConfig{
				Name: "test-memory",
				Type: "system-memory",
				Config: map[string]interface{}{
					"warningThreshold":  85.0,
					"criticalThreshold": 95.0,
				},
			},
			wantError: false,
		},
		{
			name: "missing name",
			config: types.MonitorConfig{
				Type: "system-memory",
			},
			wantError: true,
			errorMsg:  "monitor name is required",
		},
		{
			name: "wrong type",
			config: types.MonitorConfig{
				Name: "test-memory",
				Type: "wrong-type",
			},
			wantError: true,
			errorMsg:  "invalid monitor type",
		},
		{
			name: "invalid warning threshold - negative",
			config: types.MonitorConfig{
				Name: "test-memory",
				Type: "system-memory",
				Config: map[string]interface{}{
					"warningThreshold": -0.5,
				},
			},
			wantError: true,
			errorMsg:  "warningThreshold must be between 0 and 100",
		},
		{
			name: "invalid warning threshold - over 100",
			config: types.MonitorConfig{
				Name: "test-memory",
				Type: "system-memory",
				Config: map[string]interface{}{
					"warningThreshold": 150.0,
				},
			},
			wantError: true,
			errorMsg:  "warningThreshold must be between 0 and 100",
		},
		{
			name: "invalid critical threshold - negative",
			config: types.MonitorConfig{
				Name: "test-memory",
				Type: "system-memory",
				Config: map[string]interface{}{
					"criticalThreshold": -0.5,
				},
			},
			wantError: true,
			errorMsg:  "criticalThreshold must be between 0 and 100",
		},
		{
			name: "invalid critical threshold - over 100",
			config: types.MonitorConfig{
				Name: "test-memory",
				Type: "system-memory",
				Config: map[string]interface{}{
					"criticalThreshold": 150.0,
				},
			},
			wantError: true,
			errorMsg:  "criticalThreshold must be between 0 and 100",
		},
		{
			name: "warning >= critical",
			config: types.MonitorConfig{
				Name: "test-memory",
				Type: "system-memory",
				Config: map[string]interface{}{
					"warningThreshold":  95.0,
					"criticalThreshold": 85.0,
				},
			},
			wantError: true,
			errorMsg:  "warningThreshold",
		},
		{
			name: "invalid swap warning threshold",
			config: types.MonitorConfig{
				Name: "test-memory",
				Type: "system-memory",
				Config: map[string]interface{}{
					"swapWarningThreshold": 150.0,
				},
			},
			wantError: true,
			errorMsg:  "swapWarningThreshold must be between 0 and 100",
		},
		{
			name: "invalid swap critical threshold",
			config: types.MonitorConfig{
				Name: "test-memory",
				Type: "system-memory",
				Config: map[string]interface{}{
					"swapCriticalThreshold": 150.0,
				},
			},
			wantError: true,
			errorMsg:  "swapCriticalThreshold must be between 0 and 100",
		},
		{
			name: "swap warning >= critical",
			config: types.MonitorConfig{
				Name: "test-memory",
				Type: "system-memory",
				Config: map[string]interface{}{
					"swapWarningThreshold":  80.0,
					"swapCriticalThreshold": 50.0,
				},
			},
			wantError: true,
			errorMsg:  "swapWarningThreshold",
		},
		{
			name: "invalid sustained checks",
			config: types.MonitorConfig{
				Name: "test-memory",
				Type: "system-memory",
				Config: map[string]interface{}{
					"sustainedHighMemoryChecks": -1,
				},
			},
			wantError: true,
			errorMsg:  "sustainedHighMemoryChecks must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMemoryConfig(tt.config)

			if tt.wantError {
				if err == nil {
					t.Errorf("expected error but got none")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestParseMemInfo(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		expected  *MemoryInfo
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid meminfo",
			content: `MemTotal:       8192000 kB
MemFree:        2048000 kB
MemAvailable:   4096000 kB
Buffers:         512000 kB
Cached:         1024000 kB
SwapTotal:      4096000 kB
SwapFree:       3584000 kB
Active:         2048000 kB
Inactive:       1024000 kB`,
			expected: &MemoryInfo{
				MemTotal:     8192000,
				MemFree:      2048000,
				MemAvailable: 4096000,
				Buffers:      512000,
				Cached:       1024000,
				SwapTotal:    4096000,
				SwapFree:     3584000,
			},
			wantError: false,
		},
		{
			name: "missing MemAvailable (fallback to MemFree)",
			content: `MemTotal:       8192000 kB
MemFree:        2048000 kB
Buffers:         512000 kB
Cached:         1024000 kB
SwapTotal:      4096000 kB
SwapFree:       3584000 kB`,
			expected: &MemoryInfo{
				MemTotal:     8192000,
				MemFree:      2048000,
				MemAvailable: 0, // Should be 0 when missing
				Buffers:      512000,
				Cached:       1024000,
				SwapTotal:    4096000,
				SwapFree:     3584000,
			},
			wantError: false,
		},
		{
			name: "no swap",
			content: `MemTotal:       8192000 kB
MemFree:        2048000 kB
MemAvailable:   4096000 kB
Buffers:         512000 kB
Cached:         1024000 kB
SwapTotal:            0 kB
SwapFree:             0 kB`,
			expected: &MemoryInfo{
				MemTotal:     8192000,
				MemFree:      2048000,
				MemAvailable: 4096000,
				Buffers:      512000,
				Cached:       1024000,
				SwapTotal:    0,
				SwapFree:     0,
			},
			wantError: false,
		},
		{
			name: "missing MemTotal",
			content: `MemFree:        2048000 kB
MemAvailable:   4096000 kB`,
			wantError: true,
			errorMsg:  "MemTotal not found or zero",
		},
		{
			name: "zero MemTotal",
			content: `MemTotal:             0 kB
MemFree:        2048000 kB`,
			wantError: true,
			errorMsg:  "MemTotal not found or zero",
		},
		{
			name: "malformed lines ignored",
			content: `MemTotal:       8192000 kB
invalid line without colon
MemFree kB
MemAvailable:   4096000 kB
another: invalid: line: with: multiple: colons: 123 kB
Buffers:         512000 kB`,
			expected: &MemoryInfo{
				MemTotal:     8192000,
				MemFree:      0, // Missing, defaults to 0
				MemAvailable: 4096000,
				Buffers:      512000,
				Cached:       0,
				SwapTotal:    0,
				SwapFree:     0,
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock file reader
			mock := newMockFileReader()
			mock.setFile("/proc/meminfo", tt.content)

			// Create memory monitor
			monitor := &MemoryMonitor{
				config: &MemoryMonitorConfig{
					MemInfoPath: "/proc/meminfo",
				},
				fileReader: mock,
			}

			result, err := monitor.parseMemInfo()

			if tt.wantError {
				if err == nil {
					t.Errorf("expected error but got none")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result.MemTotal != tt.expected.MemTotal {
				t.Errorf("MemTotal: got %d, want %d", result.MemTotal, tt.expected.MemTotal)
			}
			if result.MemFree != tt.expected.MemFree {
				t.Errorf("MemFree: got %d, want %d", result.MemFree, tt.expected.MemFree)
			}
			if result.MemAvailable != tt.expected.MemAvailable {
				t.Errorf("MemAvailable: got %d, want %d", result.MemAvailable, tt.expected.MemAvailable)
			}
			if result.Buffers != tt.expected.Buffers {
				t.Errorf("Buffers: got %d, want %d", result.Buffers, tt.expected.Buffers)
			}
			if result.Cached != tt.expected.Cached {
				t.Errorf("Cached: got %d, want %d", result.Cached, tt.expected.Cached)
			}
			if result.SwapTotal != tt.expected.SwapTotal {
				t.Errorf("SwapTotal: got %d, want %d", result.SwapTotal, tt.expected.SwapTotal)
			}
			if result.SwapFree != tt.expected.SwapFree {
				t.Errorf("SwapFree: got %d, want %d", result.SwapFree, tt.expected.SwapFree)
			}
		})
	}
}

func TestMemoryUsageThresholds(t *testing.T) {
	tests := []struct {
		name                string
		memTotal            uint64
		memAvailable        uint64
		warningThreshold    float64
		criticalThreshold   float64
		expectedSeverity    string
		expectedEventReason string
		expectedCondition   string
		expectedCondStatus  types.ConditionStatus
	}{
		{
			name:                "normal usage",
			memTotal:            8192000, // 8GB
			memAvailable:        6553600, // 6.4GB (20% used)
			warningThreshold:    85.0,
			criticalThreshold:   95.0,
			expectedEventReason: "",
			expectedCondition:   "MemoryPressure",
			expectedCondStatus:  types.ConditionFalse,
		},
		{
			name:                "warning level",
			memTotal:            8192000, // 8GB
			memAvailable:        1228800, // 1.2GB (85% used)
			warningThreshold:    85.0,
			criticalThreshold:   95.0,
			expectedEventReason: "ElevatedMemoryUsage",
			expectedCondition:   "MemoryPressure",
			expectedCondStatus:  types.ConditionFalse,
		},
		{
			name:                "critical level",
			memTotal:            8192000, // 8GB
			memAvailable:        409600,  // 0.4GB (95% used)
			warningThreshold:    85.0,
			criticalThreshold:   95.0,
			expectedEventReason: "HighMemoryUsage",
			expectedCondition:   "",
			expectedCondStatus:  "",
		},
		{
			name:                "fallback to MemFree when MemAvailable is 0",
			memTotal:            8192000, // 8GB
			memAvailable:        0,       // Not available
			warningThreshold:    85.0,
			criticalThreshold:   95.0,
			expectedEventReason: "HighMemoryUsage", // MemFree defaults to 0, so 100% used
			expectedCondition:   "",
			expectedCondStatus:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock file reader
			mock := newMockFileReader()
			memContent := fmt.Sprintf("MemTotal:       %d kB\nMemFree:        0 kB\nMemAvailable:   %d kB\n",
				tt.memTotal, tt.memAvailable)
			mock.setFile("/proc/meminfo", memContent)

			// Create memory monitor
			monitor := &MemoryMonitor{
				name: "test-memory",
				config: &MemoryMonitorConfig{
					WarningThreshold:          tt.warningThreshold,
					CriticalThreshold:         tt.criticalThreshold,
					MemInfoPath:               "/proc/meminfo",
					CheckMemoryUsage:          true,
					CheckSwapUsage:            false,
					CheckOOMKills:             false,
					SustainedHighMemoryChecks: 3,
				},
				fileReader: mock,
			}

			ctx := context.Background()
			status := types.NewStatus("test")
			err := monitor.checkMemoryUsage(ctx, status)

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			// Check events
			if tt.expectedEventReason == "" {
				// Should have no events
				if len(status.Events) != 0 {
					t.Errorf("expected no events, got %d: %v", len(status.Events), status.Events)
				}
			} else {
				// Should have event with expected reason
				if len(status.Events) == 0 {
					t.Errorf("expected event with reason %q, got no events", tt.expectedEventReason)
				} else if status.Events[0].Reason != tt.expectedEventReason {
					t.Errorf("expected event reason %q, got %q", tt.expectedEventReason, status.Events[0].Reason)
				}
			}

			// Check conditions
			if tt.expectedCondition == "" {
				// For critical level on first check, no condition should be set
				if len(status.Conditions) != 0 {
					t.Errorf("expected no conditions, got %d: %v", len(status.Conditions), status.Conditions)
				}
			} else {
				// Should have condition with expected type and status
				if len(status.Conditions) == 0 {
					t.Errorf("expected condition with type %q, got no conditions", tt.expectedCondition)
				} else if status.Conditions[0].Type != tt.expectedCondition {
					t.Errorf("expected condition type %q, got %q", tt.expectedCondition, status.Conditions[0].Type)
				} else if status.Conditions[0].Status != tt.expectedCondStatus {
					t.Errorf("expected condition status %q, got %q", tt.expectedCondStatus, status.Conditions[0].Status)
				}
			}
		})
	}
}

func TestSustainedHighMemoryPressure(t *testing.T) {
	// Create mock file reader
	mock := newMockFileReader()
	memContent := "MemTotal:       8192000 kB\nMemFree:        0 kB\nMemAvailable:   409600 kB\n" // 95% used
	mock.setFile("/proc/meminfo", memContent)

	// Create memory monitor
	monitor := &MemoryMonitor{
		name: "test-memory",
		config: &MemoryMonitorConfig{
			WarningThreshold:          85.0,
			CriticalThreshold:         95.0,
			MemInfoPath:               "/proc/meminfo",
			CheckMemoryUsage:          true,
			CheckSwapUsage:            false,
			CheckOOMKills:             false,
			SustainedHighMemoryChecks: 3,
		},
		fileReader: mock,
	}

	ctx := context.Background()

	// First check - should have event but no sustained condition
	status1 := types.NewStatus("test")
	err := monitor.checkMemoryUsage(ctx, status1)
	if err != nil {
		t.Errorf("unexpected error on first check: %v", err)
	}

	if len(status1.Events) == 0 {
		t.Error("expected event on first critical check")
	} else if status1.Events[0].Reason != "HighMemoryUsage" {
		t.Errorf("expected HighMemoryUsage event, got %q", status1.Events[0].Reason)
	}

	// Should not have MemoryPressure condition yet
	hasPressureCondition := false
	for _, cond := range status1.Conditions {
		if cond.Type == "MemoryPressure" && cond.Status == types.ConditionTrue {
			hasPressureCondition = true
			break
		}
	}
	if hasPressureCondition {
		t.Error("unexpected MemoryPressure condition on first check")
	}

	// Second check - still no sustained condition
	status2 := types.NewStatus("test")
	err = monitor.checkMemoryUsage(ctx, status2)
	if err != nil {
		t.Errorf("unexpected error on second check: %v", err)
	}

	// Should not have MemoryPressure condition yet
	hasPressureCondition = false
	for _, cond := range status2.Conditions {
		if cond.Type == "MemoryPressure" && cond.Status == types.ConditionTrue {
			hasPressureCondition = true
			break
		}
	}
	if hasPressureCondition {
		t.Error("unexpected MemoryPressure condition on second check")
	}

	// Third check - now should have sustained condition
	status3 := types.NewStatus("test")
	err = monitor.checkMemoryUsage(ctx, status3)
	if err != nil {
		t.Errorf("unexpected error on third check: %v", err)
	}

	// Should have MemoryPressure condition now
	hasPressureCondition = false
	for _, cond := range status3.Conditions {
		if cond.Type == "MemoryPressure" && cond.Status == types.ConditionTrue {
			hasPressureCondition = true
			if !strings.Contains(cond.Reason, "SustainedHighMemory") {
				t.Errorf("expected SustainedHighMemory reason, got %q", cond.Reason)
			}
			break
		}
	}
	if !hasPressureCondition {
		t.Error("expected MemoryPressure condition on third consecutive critical check")
	}

	// Reset by having normal memory usage
	memContentNormal := "MemTotal:       8192000 kB\nMemFree:        0 kB\nMemAvailable:   6553600 kB\n" // 20% used
	mock.setFile("/proc/meminfo", memContentNormal)

	status4 := types.NewStatus("test")
	err = monitor.checkMemoryUsage(ctx, status4)
	if err != nil {
		t.Errorf("unexpected error on reset check: %v", err)
	}

	// Should reset counter and have normal condition
	if len(status4.Events) != 0 {
		t.Errorf("expected no events after reset, got %d", len(status4.Events))
	}

	// Check that memory pressure is now false
	hasFalsePressure := false
	for _, cond := range status4.Conditions {
		if cond.Type == "MemoryPressure" && cond.Status == types.ConditionFalse {
			hasFalsePressure = true
			break
		}
	}
	if !hasFalsePressure {
		t.Error("expected MemoryPressure condition to be false after reset")
	}

	// Verify counter was reset by checking critical again
	mock.setFile("/proc/meminfo", memContent) // Back to critical
	status5 := types.NewStatus("test")
	err = monitor.checkMemoryUsage(ctx, status5)
	if err != nil {
		t.Errorf("unexpected error on counter reset verification: %v", err)
	}

	// Should not have sustained condition (counter was reset)
	hasTruePressure := false
	for _, cond := range status5.Conditions {
		if cond.Type == "MemoryPressure" && cond.Status == types.ConditionTrue {
			hasTruePressure = true
			break
		}
	}
	if hasTruePressure {
		t.Error("sustained memory pressure condition should have been reset")
	}
}

func TestSwapUsageChecks(t *testing.T) {
	tests := []struct {
		name                  string
		swapTotal             uint64
		swapFree              uint64
		swapWarningThreshold  float64
		swapCriticalThreshold float64
		expectedEventReason   string
	}{
		{
			name:                  "no swap configured",
			swapTotal:             0,
			swapFree:              0,
			swapWarningThreshold:  50.0,
			swapCriticalThreshold: 80.0,
			expectedEventReason:   "", // No events when no swap
		},
		{
			name:                  "normal swap usage",
			swapTotal:             4096000, // 4GB
			swapFree:              3276800, // 3.2GB (20% used)
			swapWarningThreshold:  50.0,
			swapCriticalThreshold: 80.0,
			expectedEventReason:   "", // No events at normal usage
		},
		{
			name:                  "warning level swap usage",
			swapTotal:             4096000, // 4GB
			swapFree:              2048000, // 2GB (50% used)
			swapWarningThreshold:  50.0,
			swapCriticalThreshold: 80.0,
			expectedEventReason:   "ElevatedSwapUsage",
		},
		{
			name:                  "critical level swap usage",
			swapTotal:             4096000, // 4GB
			swapFree:              819200,  // 0.8GB (80% used)
			swapWarningThreshold:  50.0,
			swapCriticalThreshold: 80.0,
			expectedEventReason:   "HighSwapUsage",
		},
		{
			name:                  "very high swap usage",
			swapTotal:             4096000, // 4GB
			swapFree:              204800,  // 0.2GB (95% used)
			swapWarningThreshold:  50.0,
			swapCriticalThreshold: 80.0,
			expectedEventReason:   "HighSwapUsage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock file reader
			mock := newMockFileReader()
			memContent := fmt.Sprintf("MemTotal:       8192000 kB\nMemFree:        2048000 kB\nMemAvailable:   4096000 kB\nSwapTotal:      %d kB\nSwapFree:       %d kB\n",
				tt.swapTotal, tt.swapFree)
			mock.setFile("/proc/meminfo", memContent)

			// Create memory monitor
			monitor := &MemoryMonitor{
				name: "test-memory",
				config: &MemoryMonitorConfig{
					SwapWarningThreshold:  tt.swapWarningThreshold,
					SwapCriticalThreshold: tt.swapCriticalThreshold,
					MemInfoPath:           "/proc/meminfo",
					CheckMemoryUsage:      false,
					CheckSwapUsage:        true,
					CheckOOMKills:         false,
				},
				fileReader: mock,
			}

			ctx := context.Background()
			status := types.NewStatus("test")
			err := monitor.checkSwapUsage(ctx, status)

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			// Check events
			if tt.expectedEventReason == "" {
				// Should have no events
				if len(status.Events) != 0 {
					t.Errorf("expected no events, got %d: %v", len(status.Events), status.Events)
				}
			} else {
				// Should have event with expected reason
				if len(status.Events) == 0 {
					t.Errorf("expected event with reason %q, got no events", tt.expectedEventReason)
				} else if status.Events[0].Reason != tt.expectedEventReason {
					t.Errorf("expected event reason %q, got %q", tt.expectedEventReason, status.Events[0].Reason)
				}
			}
		})
	}
}

func TestOOMKillDetection(t *testing.T) {
	tests := []struct {
		name                    string
		kmsgContent             string
		kmsgError               error
		expectedEventReason     string
		expectPermissionWarning bool
	}{
		{
			name: "OOM kill detected",
			kmsgContent: `[12345.678901] some kernel message
[12346.789012] Out of memory: Kill process 1234 (test-process) score 1000 or sacrifice child
[12347.890123] Killed process 1234 (test-process) total-vm:123456kB, anon-rss:78901kB, file-rss:2345kB
[12348.901234] another kernel message`,
			expectedEventReason: "OOMKillDetected",
		},
		{
			name: "Killed process pattern",
			kmsgContent: `[12345.678901] some kernel message
[12346.789012] Killed process 5678 (another-process) total-vm:98765kB, anon-rss:43210kB, file-rss:1234kB
[12347.890123] another kernel message`,
			expectedEventReason: "OOMKillDetected",
		},
		{
			name: "no OOM kills",
			kmsgContent: `[12345.678901] some kernel message
[12346.789012] normal kernel message
[12347.890123] another normal message`,
			expectedEventReason: "",
		},
		{
			name:                    "permission denied",
			kmsgContent:             "",
			kmsgError:               os.ErrPermission,
			expectedEventReason:     "",
			expectPermissionWarning: true,
		},
		{
			name:                "file not found",
			kmsgContent:         "",
			kmsgError:           os.ErrNotExist,
			expectedEventReason: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock file reader
			mock := newMockFileReader()
			if tt.kmsgError != nil {
				// Don't set the file to simulate error
				if tt.kmsgError != os.ErrPermission {
					// For non-permission errors, we expect the error to be returned
					// so we'll handle this in the test
				}
			} else {
				mock.setFile("/dev/kmsg", tt.kmsgContent)
			}

			// Create memory monitor
			monitor := &MemoryMonitor{
				name: "test-memory",
				config: &MemoryMonitorConfig{
					KmsgPath:         "/dev/kmsg",
					CheckMemoryUsage: false,
					CheckSwapUsage:   false,
					CheckOOMKills:    true,
				},
				fileReader: mock,
			}

			// For permission denied, we need to create a custom mock that returns the error
			if tt.kmsgError == os.ErrPermission {
				permissionMock := &mockFileReaderWithError{
					files: map[string]string{},
					errors: map[string]error{
						"/dev/kmsg": os.ErrPermission,
					},
				}
				monitor.fileReader = permissionMock
			}

			ctx := context.Background()
			status := types.NewStatus("test")
			err := monitor.checkOOMKills(ctx, status)

			// Handle expected errors
			if tt.kmsgError != nil && tt.kmsgError != os.ErrPermission {
				if err == nil {
					t.Errorf("expected error %v but got none", tt.kmsgError)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			// Check events
			if tt.expectedEventReason == "" && !tt.expectPermissionWarning {
				// Should have no events
				if len(status.Events) != 0 {
					t.Errorf("expected no events, got %d: %v", len(status.Events), status.Events)
				}
			} else if tt.expectPermissionWarning {
				// Should have permission warning
				if len(status.Events) == 0 {
					t.Error("expected permission warning event")
				} else if status.Events[0].Reason != "OOMCheckPermissionDenied" {
					t.Errorf("expected OOMCheckPermissionDenied event, got %q", status.Events[0].Reason)
				}

				// Second call should not generate another warning
				status2 := types.NewStatus("test")
				err = monitor.checkOOMKills(ctx, status2)
				if err != nil {
					t.Errorf("unexpected error on second call: %v", err)
				}
				if len(status2.Events) != 0 {
					t.Errorf("expected no events on second call (warning already shown), got %d", len(status2.Events))
				}
			} else {
				// Should have event with expected reason
				if len(status.Events) == 0 {
					t.Errorf("expected event with reason %q, got no events", tt.expectedEventReason)
				} else if status.Events[0].Reason != tt.expectedEventReason {
					t.Errorf("expected event reason %q, got %q", tt.expectedEventReason, status.Events[0].Reason)
				}
			}
		})
	}
}

// mockFileReaderWithError allows testing error conditions
type mockFileReaderWithError struct {
	files  map[string]string
	errors map[string]error
}

func (m *mockFileReaderWithError) ReadFile(path string) ([]byte, error) {
	if err, exists := m.errors[path]; exists {
		return nil, err
	}
	if content, exists := m.files[path]; exists {
		return []byte(content), nil
	}
	return nil, os.ErrNotExist
}

func (m *mockFileReaderWithError) ReadDir(path string) ([]os.DirEntry, error) {
	return nil, os.ErrNotExist
}

func TestOOMKillDuplicateDetection(t *testing.T) {
	// Create mock file reader
	mock := newMockFileReader()
	kmsgContent := `[12345.678901] Out of memory: Kill process 1234 (test-process)
[12346.789012] Killed process 1234 (test-process)`
	mock.setFile("/dev/kmsg", kmsgContent)

	// Create memory monitor
	monitor := &MemoryMonitor{
		name: "test-memory",
		config: &MemoryMonitorConfig{
			KmsgPath:         "/dev/kmsg",
			CheckMemoryUsage: false,
			CheckSwapUsage:   false,
			CheckOOMKills:    true,
		},
		fileReader: mock,
	}

	ctx := context.Background()

	// First check - should detect OOM kill
	status1 := types.NewStatus("test")
	err := monitor.checkOOMKills(ctx, status1)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(status1.Events) == 0 {
		t.Error("expected OOM kill event on first check")
	} else if status1.Events[0].Reason != "OOMKillDetected" {
		t.Errorf("expected OOMKillDetected event, got %q", status1.Events[0].Reason)
	}

	// Second check immediately after - should not detect duplicate
	status2 := types.NewStatus("test")
	err = monitor.checkOOMKills(ctx, status2)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(status2.Events) != 0 {
		t.Errorf("expected no events on second check (duplicate), got %d", len(status2.Events))
	}

	// Simulate time passing (more than 1 minute)
	monitor.mu.Lock()
	monitor.lastOOMKillTime = time.Now().Add(-2 * time.Minute)
	monitor.mu.Unlock()

	// Third check - should detect again
	status3 := types.NewStatus("test")
	err = monitor.checkOOMKills(ctx, status3)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(status3.Events) == 0 {
		t.Error("expected OOM kill event after time passed")
	} else if status3.Events[0].Reason != "OOMKillDetected" {
		t.Errorf("expected OOMKillDetected event, got %q", status3.Events[0].Reason)
	}
}

func TestNewMemoryMonitor(t *testing.T) {
	tests := []struct {
		name      string
		config    types.MonitorConfig
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid config",
			config: types.MonitorConfig{
				Name:     "test-memory",
				Type:     "system-memory",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Config: map[string]interface{}{
					"warningThreshold":  85.0,
					"criticalThreshold": 95.0,
				},
			},
			wantError: false,
		},
		{
			name: "invalid config",
			config: types.MonitorConfig{
				Name:     "test-memory",
				Type:     "system-memory",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Config: map[string]interface{}{
					"warningThreshold": "invalid",
				},
			},
			wantError: true,
			errorMsg:  "failed to parse Memory config",
		},
		{
			name: "invalid defaults",
			config: types.MonitorConfig{
				Name:     "test-memory",
				Type:     "system-memory",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Config: map[string]interface{}{
					"warningThreshold":  95.0,
					"criticalThreshold": 85.0, // Invalid: warning >= critical
				},
			},
			wantError: true,
			errorMsg:  "failed to apply defaults",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			monitor, err := NewMemoryMonitor(ctx, tt.config)

			if tt.wantError {
				if err == nil {
					t.Errorf("expected error but got none")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if monitor == nil {
				t.Error("monitor is nil")
				return
			}

			// Verify it's a MemoryMonitor
			memMonitor, ok := monitor.(*MemoryMonitor)
			if !ok {
				t.Error("monitor is not a MemoryMonitor")
				return
			}

			if memMonitor.name != tt.config.Name {
				t.Errorf("monitor name: got %q, want %q", memMonitor.name, tt.config.Name)
			}
		})
	}
}

func TestMemoryMonitorIntegration(t *testing.T) {
	// Create mock file reader
	mock := newMockFileReader()
	memContent := "MemTotal:       8192000 kB\nMemFree:        0 kB\nMemAvailable:   6553600 kB\n" // 20% used - normal
	mock.setFile("/proc/meminfo", memContent)
	mock.setFile("/dev/kmsg", "no OOM kills here")

	// Create memory monitor
	config := types.MonitorConfig{
		Name:     "integration-test",
		Type:     "system-memory",
		Interval: 200 * time.Millisecond, // Reasonable interval
		Timeout:  100 * time.Millisecond,
		Config: map[string]interface{}{
			"warningThreshold":  85.0,
			"criticalThreshold": 95.0,
		},
	}

	ctx := context.Background()
	monitor, err := NewMemoryMonitor(ctx, config)
	if err != nil {
		t.Fatalf("failed to create monitor: %v", err)
	}

	memMonitor := monitor.(*MemoryMonitor)
	memMonitor.fileReader = mock

	// Start monitoring
	statusCh, err := monitor.Start()
	if err != nil {
		t.Fatalf("failed to start monitor: %v", err)
	}

	// Wait for a status update
	select {
	case status := <-statusCh:
		if status == nil {
			t.Error("received nil status")
		} else {
			if status.Source != "integration-test" {
				t.Errorf("status source: got %q, want %q", status.Source, "integration-test")
			}

			// Should have healthy condition
			hasHealthy := false
			for _, cond := range status.Conditions {
				if cond.Type == "MemoryHealthy" && cond.Status == types.ConditionTrue {
					hasHealthy = true
					break
				}
			}
			if !hasHealthy {
				t.Error("expected MemoryHealthy condition")
			}
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for status update")
	}

	// Stop monitoring
	monitor.Stop()

	// Simple verification that stop works (no strict checking)
	time.Sleep(300 * time.Millisecond) // Give time for stop to take effect
}

func TestMemoryCheckErrors(t *testing.T) {
	// Create monitor with non-existent files
	config := &MemoryMonitorConfig{
		MemInfoPath:      "/nonexistent/meminfo",
		KmsgPath:         "/nonexistent/kmsg",
		CheckMemoryUsage: true,
		CheckSwapUsage:   true,
		CheckOOMKills:    true,
	}

	monitor := &MemoryMonitor{
		name:       "test-memory",
		config:     config,
		fileReader: &defaultFileReader{},
	}

	ctx := context.Background()
	status, err := monitor.checkMemory(ctx)

	if err != nil {
		t.Errorf("checkMemory should not return error, got: %v", err)
	}

	if status == nil {
		t.Error("status should not be nil")
		return
	}

	// Should have error events for failed checks
	if len(status.Events) == 0 {
		t.Error("expected error events for failed checks")
		return
	}

	// Check that error events were generated
	errorReasons := []string{"MemoryCheckFailed", "SwapCheckFailed", "OOMCheckFailed"}
	foundReasons := make(map[string]bool)

	for _, event := range status.Events {
		for _, reason := range errorReasons {
			if event.Reason == reason {
				foundReasons[reason] = true
				if event.Severity != types.EventError {
					t.Errorf("expected error severity for %s, got %s", reason, event.Severity)
				}
			}
		}
	}

	for _, reason := range errorReasons {
		if !foundReasons[reason] {
			t.Errorf("expected error event %s", reason)
		}
	}
}

// Benchmark tests
func BenchmarkParseMemInfo(b *testing.B) {
	mock := newMockFileReader()
	memContent := `MemTotal:       8192000 kB
MemFree:        2048000 kB
MemAvailable:   4096000 kB
Buffers:         512000 kB
Cached:         1024000 kB
SwapTotal:      4096000 kB
SwapFree:       3584000 kB
Active:         2048000 kB
Inactive:       1024000 kB
Active(anon):   1536000 kB
Inactive(anon):  512000 kB
Active(file):    512000 kB
Inactive(file):  512000 kB
Unevictable:        616 kB
Mlocked:            616 kB
Dirty:             1280 kB
Writeback:          940 kB`
	mock.setFile("/proc/meminfo", memContent)

	monitor := &MemoryMonitor{
		config: &MemoryMonitorConfig{
			MemInfoPath: "/proc/meminfo",
		},
		fileReader: mock,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := monitor.parseMemInfo()
		if err != nil {
			b.Fatalf("parseMemInfo failed: %v", err)
		}
	}
}

func BenchmarkMemoryUsageCheck(b *testing.B) {
	mock := newMockFileReader()
	memContent := "MemTotal:       8192000 kB\nMemFree:        2048000 kB\nMemAvailable:   4096000 kB\n"
	mock.setFile("/proc/meminfo", memContent)

	monitor := &MemoryMonitor{
		name: "bench-memory",
		config: &MemoryMonitorConfig{
			WarningThreshold:          85.0,
			CriticalThreshold:         95.0,
			MemInfoPath:               "/proc/meminfo",
			CheckMemoryUsage:          true,
			SustainedHighMemoryChecks: 3,
		},
		fileReader: mock,
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		status := types.NewStatus("bench")
		err := monitor.checkMemoryUsage(ctx, status)
		if err != nil {
			b.Fatalf("checkMemoryUsage failed: %v", err)
		}
	}
}

func BenchmarkMemoryMonitorFullCheck(b *testing.B) {
	mock := newMockFileReader()
	memContent := "MemTotal:       8192000 kB\nMemFree:        2048000 kB\nMemAvailable:   4096000 kB\nSwapTotal:      4096000 kB\nSwapFree:       3584000 kB\n"
	mock.setFile("/proc/meminfo", memContent)
	mock.setFile("/dev/kmsg", "no OOM kills")

	monitor := &MemoryMonitor{
		name: "bench-memory",
		config: &MemoryMonitorConfig{
			WarningThreshold:          85.0,
			CriticalThreshold:         95.0,
			SwapWarningThreshold:      50.0,
			SwapCriticalThreshold:     80.0,
			MemInfoPath:               "/proc/meminfo",
			KmsgPath:                  "/dev/kmsg",
			CheckMemoryUsage:          true,
			CheckSwapUsage:            true,
			CheckOOMKills:             true,
			SustainedHighMemoryChecks: 3,
		},
		fileReader: mock,
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := monitor.checkMemory(ctx)
		if err != nil {
			b.Fatalf("checkMemory failed: %v", err)
		}
	}
}
