package system

import (
	"context"
	"syscall"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// Test configuration parsing
func TestParseDiskConfig(t *testing.T) {
	tests := []struct {
		name      string
		config    map[string]interface{}
		wantError bool
	}{
		{
			name:      "nil config",
			config:    nil,
			wantError: false,
		},
		{
			name:      "empty config",
			config:    map[string]interface{}{},
			wantError: false,
		},
		{
			name: "valid config with all fields",
			config: map[string]interface{}{
				"mountPoints": []interface{}{
					map[string]interface{}{
						"path":                   "/",
						"warningThreshold":       80.0,
						"criticalThreshold":      90.0,
						"inodeWarningThreshold":  80.0,
						"inodeCriticalThreshold": 90.0,
					},
					map[string]interface{}{
						"path":              "/var",
						"warningThreshold":  75.0,
						"criticalThreshold": 85.0,
					},
				},
				"sustainedHighDiskChecks": 5,
				"checkDiskSpace":          true,
				"checkInodes":             true,
				"checkReadonly":           true,
				"checkIOHealth":           true,
				"diskStatsPath":           "/custom/proc/diskstats",
			},
			wantError: false,
		},
		{
			name: "invalid mount points type",
			config: map[string]interface{}{
				"mountPoints": "invalid",
			},
			wantError: true,
		},
		{
			name: "invalid mount point structure",
			config: map[string]interface{}{
				"mountPoints": []interface{}{
					"invalid",
				},
			},
			wantError: true,
		},
		{
			name: "mount point missing path",
			config: map[string]interface{}{
				"mountPoints": []interface{}{
					map[string]interface{}{
						"warningThreshold": 80.0,
					},
				},
			},
			wantError: true,
		},
		{
			name: "invalid sustained high disk checks type",
			config: map[string]interface{}{
				"sustainedHighDiskChecks": "invalid",
			},
			wantError: true,
		},
		{
			name: "sustained high disk checks as float",
			config: map[string]interface{}{
				"sustainedHighDiskChecks": 5.0,
			},
			wantError: false,
		},
		{
			name: "invalid check disk space type",
			config: map[string]interface{}{
				"checkDiskSpace": "invalid",
			},
			wantError: true,
		},
		{
			name: "invalid check inodes type",
			config: map[string]interface{}{
				"checkInodes": "invalid",
			},
			wantError: true,
		},
		{
			name: "invalid check readonly type",
			config: map[string]interface{}{
				"checkReadonly": "invalid",
			},
			wantError: true,
		},
		{
			name: "invalid check IO health type",
			config: map[string]interface{}{
				"checkIOHealth": "invalid",
			},
			wantError: true,
		},
		{
			name: "invalid disk stats path type",
			config: map[string]interface{}{
				"diskStatsPath": 123,
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseDiskConfig(tt.config)
			if (err != nil) != tt.wantError {
				t.Errorf("parseDiskConfig() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

// Test mount point configuration parsing
func TestParseMountPointConfig(t *testing.T) {
	tests := []struct {
		name      string
		config    map[string]interface{}
		wantError bool
	}{
		{
			name: "valid mount point config",
			config: map[string]interface{}{
				"path":                   "/test",
				"warningThreshold":       75.0,
				"criticalThreshold":      85.0,
				"inodeWarningThreshold":  80.0,
				"inodeCriticalThreshold": 90.0,
			},
			wantError: false,
		},
		{
			name: "missing path",
			config: map[string]interface{}{
				"warningThreshold": 75.0,
			},
			wantError: true,
		},
		{
			name: "invalid path type",
			config: map[string]interface{}{
				"path": 123,
			},
			wantError: true,
		},
		{
			name: "invalid warning threshold type",
			config: map[string]interface{}{
				"path":             "/test",
				"warningThreshold": "invalid",
			},
			wantError: true,
		},
		{
			name: "invalid critical threshold type",
			config: map[string]interface{}{
				"path":              "/test",
				"criticalThreshold": "invalid",
			},
			wantError: true,
		},
		{
			name: "invalid inode warning threshold type",
			config: map[string]interface{}{
				"path":                  "/test",
				"inodeWarningThreshold": "invalid",
			},
			wantError: true,
		},
		{
			name: "invalid inode critical threshold type",
			config: map[string]interface{}{
				"path":                   "/test",
				"inodeCriticalThreshold": "invalid",
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseMountPointConfig(tt.config)
			if (err != nil) != tt.wantError {
				t.Errorf("parseMountPointConfig() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

// Test disk information parsing
func TestParseDiskInfo(t *testing.T) {
	tests := []struct {
		name     string
		fsStats  FilesystemStats
		expected DiskInfo
	}{
		{
			name: "normal filesystem",
			fsStats: FilesystemStats{
				BlockSize:   4096,
				Blocks:      1000000,
				BlocksFree:  800000,
				BlocksAvail: 750000,
				Files:       100000,
				FilesFree:   80000,
				Flags:       0, // No special flags
			},
			expected: DiskInfo{
				TotalSpace:      4096000000,   // 4096 * 1000000
				FreeSpace:       3276800000,   // 4096 * 800000
				AvailableSpace:  3072000000,   // 4096 * 750000
				UsagePercent:    25.0,         // (750000 / 1000000) * 100 = 75% free, so 25% used
				TotalInodes:     100000,
				FreeInodes:      80000,
				InodePercent:    20.0,         // (20000 / 100000) * 100 = 20%
				IsReadonly:      false,
			},
		},
		{
			name: "readonly filesystem",
			fsStats: FilesystemStats{
				BlockSize:   4096,
				Blocks:      1000000,
				BlocksFree:  800000,
				BlocksAvail: 750000,
				Files:       100000,
				FilesFree:   80000,
				Flags:       0x0001, // ST_RDONLY
			},
			expected: DiskInfo{
				TotalSpace:     4096000000,
				FreeSpace:      3276800000,
				AvailableSpace: 3072000000,
				UsagePercent:   25.0,
				TotalInodes:    100000,
				FreeInodes:     80000,
				InodePercent:   20.0,
				IsReadonly:     true, // Should detect readonly flag
			},
		},
		{
			name: "no inodes filesystem",
			fsStats: FilesystemStats{
				BlockSize:   4096,
				Blocks:      1000000,
				BlocksFree:  800000,
				BlocksAvail: 750000,
				Files:       0, // No inodes (e.g., tmpfs)
				FilesFree:   0,
				Flags:       0,
			},
			expected: DiskInfo{
				TotalSpace:     4096000000,
				FreeSpace:      3276800000,
				AvailableSpace: 3072000000,
				UsagePercent:   25.0,
				TotalInodes:    0,
				FreeInodes:     0,
				InodePercent:   0.0, // Should handle zero inodes gracefully
				IsReadonly:     false,
			},
		},
	}

	// Create a monitor instance to test the method
	monitor := &DiskMonitor{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := monitor.parseDiskInfo("/test", &tt.fsStats)

			if result.TotalSpace != tt.expected.TotalSpace {
				t.Errorf("TotalSpace: got %d, want %d", result.TotalSpace, tt.expected.TotalSpace)
			}
			if result.FreeSpace != tt.expected.FreeSpace {
				t.Errorf("FreeSpace: got %d, want %d", result.FreeSpace, tt.expected.FreeSpace)
			}
			if result.AvailableSpace != tt.expected.AvailableSpace {
				t.Errorf("AvailableSpace: got %d, want %d", result.AvailableSpace, tt.expected.AvailableSpace)
			}
			if result.UsagePercent != tt.expected.UsagePercent {
				t.Errorf("UsagePercent: got %f, want %f", result.UsagePercent, tt.expected.UsagePercent)
			}
			if result.TotalInodes != tt.expected.TotalInodes {
				t.Errorf("TotalInodes: got %d, want %d", result.TotalInodes, tt.expected.TotalInodes)
			}
			if result.FreeInodes != tt.expected.FreeInodes {
				t.Errorf("FreeInodes: got %d, want %d", result.FreeInodes, tt.expected.FreeInodes)
			}
			if result.InodePercent != tt.expected.InodePercent {
				t.Errorf("InodePercent: got %f, want %f", result.InodePercent, tt.expected.InodePercent)
			}
			if result.IsReadonly != tt.expected.IsReadonly {
				t.Errorf("IsReadonly: got %t, want %t", result.IsReadonly, tt.expected.IsReadonly)
			}
		})
	}
}

// Test diskstats parsing
func TestParseDiskStats(t *testing.T) {
	tests := []struct {
		name     string
		data     string
		expected map[string]*IOStats
	}{
		{
			name: "valid diskstats",
			data: `   8       0 sda 2000 100 4000 300 1500 50 3000 200 0 400 500
   8       1 sda1 1000 50 2000 150 500 25 1500 100 0 200 250
 259       0 nvme0n1 3000 150 6000 450 2000 75 4000 300 0 600 750`,
			expected: map[string]*IOStats{
				"sda": {
					Device:      "sda",
					TimeDoingIO: 400,
				},
				"sda1": {
					Device:      "sda1",
					TimeDoingIO: 200,
				},
				"nvme0n1": {
					Device:      "nvme0n1",
					TimeDoingIO: 600,
				},
			},
		},
		{
			name:     "empty data",
			data:     "",
			expected: map[string]*IOStats{},
		},
		{
			name: "malformed lines ignored",
			data: `   8       0 sda 2000 100 4000 300 1500 50 3000 200 0 400 500
invalid line
   8       1 sda1`,
			expected: map[string]*IOStats{
				"sda": {
					Device:      "sda",
					TimeDoingIO: 400,
				},
			},
		},
	}

	// Create a monitor instance to test the method
	monitor := &DiskMonitor{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := monitor.parseDiskStats(tt.data)

			if len(result) != len(tt.expected) {
				t.Errorf("Result length: got %d, want %d", len(result), len(tt.expected))
			}

			for device, expectedStats := range tt.expected {
				if stats, exists := result[device]; exists {
					if stats.Device != expectedStats.Device {
						t.Errorf("Device name for %s: got %s, want %s", device, stats.Device, expectedStats.Device)
					}
					if stats.TimeDoingIO != expectedStats.TimeDoingIO {
						t.Errorf("TimeDoingIO for %s: got %d, want %d", device, stats.TimeDoingIO, expectedStats.TimeDoingIO)
					}
				} else {
					t.Errorf("Missing device %s in result", device)
				}
			}
		})
	}
}

// Mock StatfsProvider for testing
type mockStatfsProvider struct {
	stats  map[string]*FilesystemStats
	errors map[string]error
}

func newMockStatfsProvider() *mockStatfsProvider {
	return &mockStatfsProvider{
		stats:  make(map[string]*FilesystemStats),
		errors: make(map[string]error),
	}
}

func (m *mockStatfsProvider) setStats(path string, stats *FilesystemStats) {
	m.stats[path] = stats
}

func (m *mockStatfsProvider) setError(path string, err error) {
	m.errors[path] = err
}

func (m *mockStatfsProvider) Statfs(path string) (*FilesystemStats, error) {
	if err, exists := m.errors[path]; exists {
		return nil, err
	}
	if stats, exists := m.stats[path]; exists {
		return stats, nil
	}
	return nil, syscall.ENOENT // Path not found
}

// Test disk space usage checking
func TestCheckDiskSpaceUsage(t *testing.T) {
	tests := []struct {
		name               string
		diskInfo           DiskInfo
		mountPoint         MountPointConfig
		currentCount       int
		sustainedThreshold int
		expectedCondition  bool
		expectedEvent      bool
		expectedSeverity   types.EventSeverity
		expectedNewCount   int
	}{
		{
			name: "normal usage",
			diskInfo: DiskInfo{
				UsagePercent: 70.0,
			},
			mountPoint: MountPointConfig{
				Path:              "/",
				WarningThreshold:  85.0,
				CriticalThreshold: 95.0,
			},
			currentCount:       0,
			sustainedThreshold: 3,
			expectedCondition:  true,  // Condition always added
			expectedEvent:      false, // No event for normal usage
			expectedNewCount:   0,     // Count reset
		},
		{
			name: "elevated usage",
			diskInfo: DiskInfo{
				UsagePercent: 90.0,
			},
			mountPoint: MountPointConfig{
				Path:              "/",
				WarningThreshold:  85.0,
				CriticalThreshold: 95.0,
			},
			currentCount:       0,
			sustainedThreshold: 3,
			expectedCondition:  true,                  // Condition added
			expectedEvent:      true,                  // Event for elevated usage
			expectedSeverity:   types.EventWarning,
			expectedNewCount:   1, // Count incremented
		},
		{
			name: "critical usage - first occurrence",
			diskInfo: DiskInfo{
				UsagePercent: 97.0,
			},
			mountPoint: MountPointConfig{
				Path:              "/",
				WarningThreshold:  85.0,
				CriticalThreshold: 95.0,
			},
			currentCount:       0,
			sustainedThreshold: 3,
			expectedCondition:  true,                // Condition added
			expectedEvent:      true,                // Event for critical usage
			expectedSeverity:   types.EventError,
			expectedNewCount:   1, // Count incremented
		},
		{
			name: "critical usage - sustained (3rd occurrence)",
			diskInfo: DiskInfo{
				UsagePercent: 97.0,
			},
			mountPoint: MountPointConfig{
				Path:              "/",
				WarningThreshold:  85.0,
				CriticalThreshold: 95.0,
			},
			currentCount:       2, // Already 2 occurrences
			sustainedThreshold: 3,
			expectedCondition:  true,                // Condition added
			expectedEvent:      true,                // Event for sustained critical
			expectedSeverity:   types.EventError,
			expectedNewCount:   3, // Count incremented to threshold
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := types.NewStatus("test-disk")
			highDiskCount := map[string]int{tt.mountPoint.Path: tt.currentCount}

			// Create a monitor with the counter map
			monitor := &DiskMonitor{
				name:          "test-disk",
				highDiskCount: highDiskCount,
				config: &DiskMonitorConfig{
					SustainedHighDiskChecks: tt.sustainedThreshold,
				},
			}

			monitor.checkDiskSpaceUsage(status, tt.mountPoint, &tt.diskInfo)

			// Check condition
			conditionFound := false
			for _, condition := range status.Conditions {
				if condition.Type == "DiskPressure" {
					conditionFound = true
					break
				}
			}
			if conditionFound != tt.expectedCondition {
				t.Errorf("Condition found: got %t, want %t", conditionFound, tt.expectedCondition)
			}

			// Check events
			if len(status.Events) > 0 != tt.expectedEvent {
				t.Errorf("Event generated: got %t, want %t", len(status.Events) > 0, tt.expectedEvent)
			}

			if tt.expectedEvent && len(status.Events) > 0 {
				event := status.Events[0]
				if event.Severity != tt.expectedSeverity {
					t.Errorf("Event severity: got %s, want %s", event.Severity, tt.expectedSeverity)
				}
			}

			// Check counter update
			newCount := highDiskCount[tt.mountPoint.Path]
			if newCount != tt.expectedNewCount {
				t.Errorf("Counter: got %d, want %d", newCount, tt.expectedNewCount)
			}
		})
	}
}

// Test inode usage checking
func TestCheckInodeUsage(t *testing.T) {
	tests := []struct {
		name              string
		diskInfo          DiskInfo
		mountPoint        MountPointConfig
		expectedCondition bool
		expectedEvent     bool
		expectedSeverity  types.EventSeverity
	}{
		{
			name: "normal inode usage",
			diskInfo: DiskInfo{
				InodePercent: 70.0,
				TotalInodes:  100000,
			},
			mountPoint: MountPointConfig{
				Path:                   "/",
				InodeWarningThreshold:  85.0,
				InodeCriticalThreshold: 95.0,
			},
			expectedCondition: true,  // Condition always added for inode check
			expectedEvent:     false, // No event for normal usage
		},
		{
			name: "elevated inode usage",
			diskInfo: DiskInfo{
				InodePercent: 90.0,
				TotalInodes:  100000,
			},
			mountPoint: MountPointConfig{
				Path:                   "/",
				InodeWarningThreshold:  85.0,
				InodeCriticalThreshold: 95.0,
			},
			expectedCondition: true,                  // Condition added
			expectedEvent:     true,                  // Event for elevated usage
			expectedSeverity:  types.EventWarning,
		},
		{
			name: "critical inode usage",
			diskInfo: DiskInfo{
				InodePercent: 97.0,
				TotalInodes:  100000,
			},
			mountPoint: MountPointConfig{
				Path:                   "/",
				InodeWarningThreshold:  85.0,
				InodeCriticalThreshold: 95.0,
			},
			expectedCondition: true,                // Condition added
			expectedEvent:     true,                // Event for critical usage
			expectedSeverity:  types.EventError,
		},
		{
			name: "no inodes filesystem",
			diskInfo: DiskInfo{
				InodePercent: 0.0,
				TotalInodes:  0, // No inodes (e.g., tmpfs)
			},
			mountPoint: MountPointConfig{
				Path:                   "/tmp",
				InodeWarningThreshold:  85.0,
				InodeCriticalThreshold: 95.0,
			},
			expectedCondition: false, // No condition for zero inodes
			expectedEvent:     false, // No event for zero inodes
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := types.NewStatus("test-disk")

			// Create a monitor instance to call the method
			monitor := &DiskMonitor{
				name: "test-disk",
			}

			monitor.checkInodeUsage(status, tt.mountPoint, &tt.diskInfo)

			// Check condition
			conditionFound := false
			for _, condition := range status.Conditions {
				if condition.Type == "InodePressure" {
					conditionFound = true
					break
				}
			}
			if conditionFound != tt.expectedCondition {
				t.Errorf("Condition found: got %t, want %t", conditionFound, tt.expectedCondition)
			}

			// Check events
			if len(status.Events) > 0 != tt.expectedEvent {
				t.Errorf("Event generated: got %t, want %t", len(status.Events) > 0, tt.expectedEvent)
			}

			if tt.expectedEvent && len(status.Events) > 0 {
				event := status.Events[0]
				if event.Severity != tt.expectedSeverity {
					t.Errorf("Event severity: got %s, want %s", event.Severity, tt.expectedSeverity)
				}
			}
		})
	}
}

// Test mount point checking
func TestCheckMountPoint(t *testing.T) {
	tests := []struct {
		name           string
		setupMock      func(*mockStatfsProvider)
		mountPoint     MountPointConfig
		config         *DiskMonitorConfig
		expectedEvents int
		expectError    bool
	}{
		{
			name: "normal mount point",
			setupMock: func(m *mockStatfsProvider) {
				m.setStats("/", &FilesystemStats{
					BlockSize:   4096,
					Blocks:      1000000,
					BlocksFree:  800000,
					BlocksAvail: 750000,
					Files:       100000,
					FilesFree:   80000,
					Flags:       0,
				})
			},
			mountPoint: MountPointConfig{
				Path:                   "/",
				WarningThreshold:       85.0,
				CriticalThreshold:      95.0,
				InodeWarningThreshold:  85.0,
				InodeCriticalThreshold: 95.0,
			},
			config: &DiskMonitorConfig{
				CheckDiskSpace: true,
				CheckInodes:    true,
				CheckReadonly:  true,
			},
			expectedEvents: 0, // Normal disk usage -> only conditions added, no events
			expectError:    false,
		},
		{
			name: "readonly filesystem",
			setupMock: func(m *mockStatfsProvider) {
				m.setStats("/mnt/readonly", &FilesystemStats{
					BlockSize:   4096,
					Blocks:      1000000,
					BlocksFree:  800000,
					BlocksAvail: 750000,
					Files:       100000,
					FilesFree:   80000,
					Flags:       0x0001, // ST_RDONLY
				})
			},
			mountPoint: MountPointConfig{
				Path:                   "/mnt/readonly",
				WarningThreshold:       85.0,
				CriticalThreshold:      95.0,
				InodeWarningThreshold:  85.0,
				InodeCriticalThreshold: 95.0,
			},
			config: &DiskMonitorConfig{
				CheckDiskSpace: true,
				CheckInodes:    true,
				CheckReadonly:  true,
			},
			expectedEvents: 1, // Readonly event + normal conditions
			expectError:    false,
		},
		{
			name: "missing mount point",
			setupMock: func(m *mockStatfsProvider) {
				// Don't set up any stats - will return ENOENT
			},
			mountPoint: MountPointConfig{
				Path: "/nonexistent",
			},
			config:         &DiskMonitorConfig{},
			expectedEvents: 0, // Should return silently for missing mount points
			expectError:    false,
		},
		{
			name: "statfs error",
			setupMock: func(m *mockStatfsProvider) {
				m.setError("/error", syscall.EIO) // I/O error
			},
			mountPoint: MountPointConfig{
				Path: "/error",
			},
			config:         &DiskMonitorConfig{},
			expectedEvents: 0,
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockProvider := newMockStatfsProvider()
			tt.setupMock(mockProvider)

			monitor := &DiskMonitor{
				name:           "test-disk",
				config:         tt.config,
				highDiskCount:  make(map[string]int),
				statfsProvider: mockProvider,
			}

			status := types.NewStatus("test-disk")
			err := monitor.checkMountPoint(context.Background(), status, tt.mountPoint)

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}

			if len(status.Events) != tt.expectedEvents {
				t.Errorf("Events: got %d, want %d", len(status.Events), tt.expectedEvents)
			}
		})
	}
}

// Test I/O health checking
func TestCheckIOHealth(t *testing.T) {
	tests := []struct {
		name           string
		setupMock      func(*mockFileReader)
		setupMonitor   func(*DiskMonitor)
		context        context.Context
		expectedEvents int
	}{
		{
			name: "no previous stats",
			setupMock: func(m *mockFileReader) {
				m.setFile("/proc/diskstats", "8 0 sda 2000 100 4000 300 1500 50 3000 200 0 400 500")
			},
			setupMonitor: func(m *DiskMonitor) {
				// No previous stats
			},
			context:        context.WithValue(context.Background(), "timestamp", int64(1000000000)),
			expectedEvents: 0, // No events without previous stats
		},
		{
			name: "high IO utilization",
			setupMock: func(m *mockFileReader) {
				m.setFile("/proc/diskstats", "8 0 sda 2000 100 4000 300 1500 50 3000 200 0 1210 500") // TimeDoingIO = 1210 for >80% utilization
			},
			setupMonitor: func(m *DiskMonitor) {
				m.lastIOStats = map[string]*IOStats{
					"sda": {
						Device:      "sda",
						TimeDoingIO: 400, // Previous time
					},
				}
				m.lastIOCheckTime = 1 // Small positive value to enable the check
			},
			context:        context.WithValue(context.Background(), "timestamp", int64(1000000000)), // Now: 1000000000 nanoseconds
			expectedEvents: 1, // High IO event: (1210-400)/((1000000000-1)/1000000)*100 = 810/999.999*100 ≈ 81% > 80%
		},
		{
			name: "normal IO utilization",
			setupMock: func(m *mockFileReader) {
				m.setFile("/proc/diskstats", "8 0 sda 2000 100 4000 300 1500 50 3000 200 0 450 500") // Small increase
			},
			setupMonitor: func(m *DiskMonitor) {
				m.lastIOStats = map[string]*IOStats{
					"sda": {
						Device:      "sda",
						TimeDoingIO: 400, // Previous time
					},
				}
				m.lastIOCheckTime = 1 // Small positive value to enable the check
			},
			context:        context.WithValue(context.Background(), "timestamp", int64(1000000000)),
			expectedEvents: 0, // No events for normal IO
		},
		{
			name: "missing diskstats file",
			setupMock: func(m *mockFileReader) {
				// Don't set up the file
			},
			setupMonitor: func(m *DiskMonitor) {},
			context:      context.WithValue(context.Background(), "timestamp", int64(1000000000)),
			expectedEvents: 0, // Should return error, not events
		},
		{
			name: "no timestamp context",
			setupMock: func(m *mockFileReader) {
				m.setFile("/proc/diskstats", "8 0 sda 2000 100 4000 300 1500 50 3000 200 0 400 500")
			},
			setupMonitor: func(m *DiskMonitor) {},
			context:      context.Background(), // No timestamp
			expectedEvents: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockReader := newMockFileReader()
			tt.setupMock(mockReader)

			config := &DiskMonitorConfig{
				DiskStatsPath: "/proc/diskstats",
			}

			monitor := &DiskMonitor{
				name:        "test-disk",
				config:      config,
				lastIOStats: make(map[string]*IOStats),
				fileReader:  mockReader,
			}
			tt.setupMonitor(monitor)

			status := types.NewStatus("test-disk")
			err := monitor.checkIOHealth(tt.context, status)

			// Check if we expect an error (file not found case)
			if tt.name == "missing diskstats file" {
				if err == nil {
					t.Errorf("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}

			if len(status.Events) != tt.expectedEvents {
				t.Errorf("Events: got %d, want %d", len(status.Events), tt.expectedEvents)
			}

			// Check event type if we expect an event
			if tt.expectedEvents > 0 && len(status.Events) > 0 {
				event := status.Events[0]
				if event.Reason != "HighIOWait" {
					t.Errorf("Event reason: got %s, want HighIOWait", event.Reason)
				}
			}
		})
	}
}

// Test monitor creation
func TestNewDiskMonitor(t *testing.T) {
	tests := []struct {
		name        string
		config      types.MonitorConfig
		expectError bool
	}{
		{
			name: "valid config",
			config: types.MonitorConfig{
				Name:     "test-disk",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Config: map[string]interface{}{
					"mountPoints": []interface{}{
						map[string]interface{}{
							"path":              "/",
							"warningThreshold":  85.0,
							"criticalThreshold": 95.0,
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "invalid config",
			config: types.MonitorConfig{
				Name:     "test-disk",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Config: map[string]interface{}{
					"mountPoints": "invalid", // Invalid type
				},
			},
			expectError: true,
		},
		{
			name: "invalid interval",
			config: types.MonitorConfig{
				Name:     "test-disk",
				Interval: 0, // Invalid interval
				Timeout:  10 * time.Second,
				Config:   nil,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			monitor, err := NewDiskMonitor(ctx, tt.config)

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if monitor == nil {
					t.Error("monitor is nil")
				}
			}
		})
	}
}

// Test config apply defaults
func TestDiskMonitorConfigApplyDefaults(t *testing.T) {
	config := &DiskMonitorConfig{}

	err := config.applyDefaults()
	if err != nil {
		t.Fatalf("applyDefaults() failed: %v", err)
	}

	// Check sustained high disk checks default
	if config.SustainedHighDiskChecks != 3 {
		t.Errorf("SustainedHighDiskChecks: got %d, want 3", config.SustainedHighDiskChecks)
	}

	// Check disk stats path default
	if config.DiskStatsPath != "/proc/diskstats" {
		t.Errorf("DiskStatsPath: got %s, want /proc/diskstats", config.DiskStatsPath)
	}

	// Check default mount points
	expectedPaths := []string{"/", "/var/lib/docker", "/var/lib/containerd"}
	if len(config.MountPoints) != len(expectedPaths) {
		t.Errorf("Mount points count: got %d, want %d", len(config.MountPoints), len(expectedPaths))
	} else {
		for i, expectedPath := range expectedPaths {
			if config.MountPoints[i].Path != expectedPath {
				t.Errorf("Mount point %d: got %s, want %s", i, config.MountPoints[i].Path, expectedPath)
			}
		}
	}

	// Check default check flags
	if !config.CheckDiskSpace {
		t.Error("CheckDiskSpace should be true by default")
	}
	if !config.CheckInodes {
		t.Error("CheckInodes should be true by default")
	}
	if !config.CheckReadonly {
		t.Error("CheckReadonly should be true by default")
	}
	if !config.CheckIOHealth {
		t.Error("CheckIOHealth should be true by default")
	}
}

// Integration test with BaseMonitor
func TestDiskMonitorIntegration(t *testing.T) {
	config := types.MonitorConfig{
		Name:     "integration-test-disk",
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Config: map[string]interface{}{
			"mountPoints": []interface{}{
				map[string]interface{}{
					"path":              "/",
					"warningThreshold":  85.0,
					"criticalThreshold": 95.0,
				},
			},
		},
	}

	ctx := context.Background()
	monitor, err := NewDiskMonitor(ctx, config)
	if err != nil {
		t.Fatalf("Failed to create monitor: %v", err)
	}

	// Test that it implements the Monitor interface
	var _ types.Monitor = monitor

	// Test that we can cast it to DiskMonitor to access methods
	diskMonitor, ok := monitor.(*DiskMonitor)
	if !ok {
		t.Fatal("monitor is not a DiskMonitor")
	}

	// Test basic monitor operations
	if diskMonitor.name != "integration-test-disk" {
		t.Errorf("Name: got %s, want integration-test-disk", diskMonitor.name)
	}

	// Test check method (should not panic)
	status, err := diskMonitor.checkDisk(ctx)
	if err != nil {
		t.Errorf("checkDisk() error: %v", err)
	}
	if status == nil {
		t.Error("checkDisk() returned nil status")
	}
}

// Test error handling scenarios
func TestDiskMonitorErrorHandling(t *testing.T) {
	tests := []struct {
		name           string
		setupMock      func(*mockStatfsProvider)
		mountPoint     string
		expectedEvents int
		expectError    bool
	}{
		{
			name: "statfs error for mount point",
			setupMock: func(m *mockStatfsProvider) {
				m.setError("/error", syscall.EIO)
			},
			mountPoint:     "/error",
			expectedEvents: 0,
			expectError:    true,
		},
		{
			name: "missing mount point",
			setupMock: func(m *mockStatfsProvider) {
				// No setup - will return ENOENT
			},
			mountPoint:     "/missing",
			expectedEvents: 0,
			expectError:    false, // Missing mount points are handled gracefully
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockProvider := newMockStatfsProvider()
			tt.setupMock(mockProvider)

			config := &DiskMonitorConfig{
				CheckDiskSpace: true,
				CheckInodes:    true,
				CheckReadonly:  true,
			}

			monitor := &DiskMonitor{
				name:           "test-disk",
				config:         config,
				highDiskCount:  make(map[string]int),
				statfsProvider: mockProvider,
			}

			mountPoint := MountPointConfig{
				Path:                   tt.mountPoint,
				WarningThreshold:       85.0,
				CriticalThreshold:      95.0,
				InodeWarningThreshold:  85.0,
				InodeCriticalThreshold: 95.0,
			}

			status := types.NewStatus("test-disk")
			err := monitor.checkMountPoint(context.Background(), status, mountPoint)

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}

			if len(status.Events) != tt.expectedEvents {
				t.Errorf("Events: got %d, want %d", len(status.Events), tt.expectedEvents)
			}
		})
	}
}

// Benchmark tests
func BenchmarkDiskMonitorCheck(b *testing.B) {
	config := types.MonitorConfig{
		Name:     "benchmark-disk",
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Config:   nil, // Use defaults
	}

	ctx := context.Background()
	monitor, err := NewDiskMonitor(ctx, config)
	if err != nil {
		b.Fatalf("Failed to create monitor: %v", err)
	}

	// Cast to DiskMonitor to access checkDisk method
	diskMonitor := monitor.(*DiskMonitor)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = diskMonitor.checkDisk(ctx)
	}
}

func BenchmarkParseDiskStats(b *testing.B) {
	data := `   8       0 sda 2000 100 4000 300 1500 50 3000 200 0 400 500
   8       1 sda1 1000 50 2000 150 500 25 1500 100 0 200 250
   8       2 sda2 500 25 1000 75 250 12 750 50 0 100 125
 259       0 nvme0n1 3000 150 6000 450 2000 75 4000 300 0 600 750
 259       1 nvme0n1p1 2000 100 4000 300 1500 50 3000 200 0 400 500
 259       2 nvme0n1p2 1000 50 2000 150 500 25 1000 100 0 200 250`

	monitor := &DiskMonitor{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = monitor.parseDiskStats(data)
	}
}

func BenchmarkParseDiskInfo(b *testing.B) {
	fsStats := &FilesystemStats{
		BlockSize:   4096,
		Blocks:      1000000,
		BlocksFree:  800000,
		BlocksAvail: 750000,
		Files:       100000,
		FilesFree:   80000,
		Flags:       0,
	}

	monitor := &DiskMonitor{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = monitor.parseDiskInfo("/test", fsStats)
	}
}