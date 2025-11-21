// Package system provides system-level monitors for Node Doctor.
// This package contains monitors that check various system resources
// and conditions such as CPU, memory, disk, and other system health indicators.
package system

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/supporttools/node-doctor/pkg/monitors"
	"github.com/supporttools/node-doctor/pkg/types"
)

// Register the Disk monitor with the global registry during package initialization.
func init() {
	monitors.Register(monitors.MonitorInfo{
		Type:        "system-disk",
		Factory:     NewDiskMonitor,
		Validator:   ValidateDiskConfig,
		Description: "Monitors disk space, inode usage, readonly filesystems, and I/O health conditions",
	})
}

// StatfsProvider provides an abstraction for filesystem statistics for testing.
type StatfsProvider interface {
	Statfs(path string) (*FilesystemStats, error)
}

// FilesystemStats represents filesystem statistics returned by statfs.
type FilesystemStats struct {
	Blocks      uint64 // Total blocks
	BlocksFree  uint64 // Free blocks
	BlocksAvail uint64 // Available blocks (non-root)
	Files       uint64 // Total inodes
	FilesFree   uint64 // Free inodes
	BlockSize   int64  // Block size
	Flags       uint64 // Mount flags
}

// defaultStatfsProvider implements StatfsProvider using syscall.Statfs.
type defaultStatfsProvider struct{}

func (p *defaultStatfsProvider) Statfs(path string) (*FilesystemStats, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return nil, err
	}
	return &FilesystemStats{
		Blocks:      stat.Blocks,
		BlocksFree:  stat.Bfree,
		BlocksAvail: stat.Bavail,
		Files:       stat.Files,
		FilesFree:   stat.Ffree,
		BlockSize:   stat.Bsize,
		Flags:       uint64(stat.Flags),
	}, nil
}

// DiskMonitorConfig contains configuration options specific to the Disk monitor.
type DiskMonitorConfig struct {
	// MountPoints defines the mount points to monitor.
	// Default: ["/", "/var/lib/docker", "/var/lib/containerd"]
	MountPoints []MountPointConfig `json:"mountPoints,omitempty"`

	// SustainedHighDiskChecks is the number of consecutive critical checks before generating a condition.
	// Default: 3 checks
	SustainedHighDiskChecks int `json:"sustainedHighDiskChecks,omitempty"`

	// CheckDiskSpace enables disk space monitoring.
	// Default: true
	CheckDiskSpace bool `json:"checkDiskSpace,omitempty"`

	// CheckInodes enables inode usage monitoring.
	// Default: true
	CheckInodes bool `json:"checkInodes,omitempty"`

	// CheckReadonly enables readonly filesystem detection.
	// Default: true
	CheckReadonly bool `json:"checkReadonly,omitempty"`

	// CheckIOHealth enables I/O health monitoring via /proc/diskstats.
	// Default: true
	CheckIOHealth bool `json:"checkIOHealth,omitempty"`

	// DiskStatsPath is the path to the disk statistics file.
	// Default: "/proc/diskstats"
	DiskStatsPath string `json:"diskStatsPath,omitempty"`
}

// MountPointConfig contains configuration for a specific mount point.
type MountPointConfig struct {
	// Path is the mount point path to monitor.
	Path string `json:"path"`

	// WarningThreshold is the disk usage percentage that triggers warning events.
	// Default: 85% (0.85)
	WarningThreshold float64 `json:"warningThreshold,omitempty"`

	// CriticalThreshold is the disk usage percentage that triggers critical events/conditions.
	// Default: 95% (0.95)
	CriticalThreshold float64 `json:"criticalThreshold,omitempty"`

	// InodeWarningThreshold is the inode usage percentage that triggers warning events.
	// Default: 85% (0.85)
	InodeWarningThreshold float64 `json:"inodeWarningThreshold,omitempty"`

	// InodeCriticalThreshold is the inode usage percentage that triggers critical events.
	// Default: 95% (0.95)
	InodeCriticalThreshold float64 `json:"inodeCriticalThreshold,omitempty"`
}

// DiskMonitor monitors disk health conditions including space usage, inode usage, and I/O health.
// It tracks sustained high disk usage conditions and generates appropriate events and conditions.
type DiskMonitor struct {
	name        string
	baseMonitor *monitors.BaseMonitor
	config      *DiskMonitorConfig

	// State tracking for sustained conditions per mount point
	mu              sync.RWMutex
	highDiskCount   map[string]int      // mount path -> consecutive critical count
	lastIOStats     map[string]*IOStats // device -> last IO stats
	lastIOCheckTime int64               // last time IO stats were checked (nanoseconds)

	// System interfaces (for testing)
	statfsProvider StatfsProvider
	fileReader     FileReader
}

// DiskInfo represents disk information for a mount point.
type DiskInfo struct {
	Path           string  // Mount point path
	TotalSpace     uint64  // Total space in bytes
	FreeSpace      uint64  // Free space in bytes
	AvailableSpace uint64  // Available space in bytes (for non-root)
	UsedSpace      uint64  // Used space in bytes
	UsagePercent   float64 // Usage percentage
	TotalInodes    uint64  // Total inodes
	FreeInodes     uint64  // Free inodes
	UsedInodes     uint64  // Used inodes
	InodePercent   float64 // Inode usage percentage
	IsReadonly     bool    // Whether filesystem is readonly
}

// IOStats represents I/O statistics for a device.
type IOStats struct {
	Device          string // Device name
	ReadsCompleted  uint64 // Number of reads completed
	ReadsMerged     uint64 // Number of reads merged
	SectorsRead     uint64 // Number of sectors read
	TimeReading     uint64 // Time spent reading (ms)
	WritesCompleted uint64 // Number of writes completed
	WritesMerged    uint64 // Number of writes merged
	SectorsWritten  uint64 // Number of sectors written
	TimeWriting     uint64 // Time spent writing (ms)
	IOsInProgress   uint64 // Number of I/Os currently in progress
	TimeDoingIO     uint64 // Time spent doing I/Os (ms)
	WeightedTimeIO  uint64 // Weighted time spent doing I/Os (ms)
}

// NewDiskMonitor creates a new Disk monitor instance.
// This factory function parses the monitor configuration and creates a properly
// configured Disk monitor with the specified settings.
func NewDiskMonitor(ctx context.Context, config types.MonitorConfig) (types.Monitor, error) {
	// Parse Disk-specific configuration
	diskConfig, err := parseDiskConfig(config.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Disk config: %w", err)
	}

	// Apply defaults
	if err := diskConfig.applyDefaults(); err != nil {
		return nil, fmt.Errorf("failed to apply defaults: %w", err)
	}

	// Create base monitor with parsed intervals
	baseMonitor, err := monitors.NewBaseMonitor(config.Name, config.Interval, config.Timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to create base monitor: %w", err)
	}

	// Create Disk monitor
	diskMonitor := &DiskMonitor{
		name:           config.Name,
		baseMonitor:    baseMonitor,
		config:         diskConfig,
		highDiskCount:  make(map[string]int),
		lastIOStats:    make(map[string]*IOStats),
		statfsProvider: &defaultStatfsProvider{},
		fileReader:     &defaultFileReader{},
	}

	// Set the check function
	err = baseMonitor.SetCheckFunc(diskMonitor.checkDisk)
	if err != nil {
		return nil, fmt.Errorf("failed to set check function: %w", err)
	}

	return diskMonitor, nil
}

// ValidateDiskConfig validates the Disk monitor configuration.
// This function performs early validation of configuration parameters to provide
// fail-fast behavior during configuration parsing.
func ValidateDiskConfig(config types.MonitorConfig) error {
	// Basic validation
	if config.Name == "" {
		return fmt.Errorf("monitor name is required")
	}
	if config.Type != "system-disk" {
		return fmt.Errorf("invalid monitor type %q for Disk monitor", config.Type)
	}

	// Parse and validate Disk-specific config
	diskConfig, err := parseDiskConfig(config.Config)
	if err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	// Apply defaults for validation
	if err := diskConfig.applyDefaults(); err != nil {
		return fmt.Errorf("failed to apply defaults during validation: %w", err)
	}

	return nil
}

// Start begins the monitoring process using the base monitor.
func (d *DiskMonitor) Start() (<-chan *types.Status, error) {
	return d.baseMonitor.Start()
}

// Stop gracefully stops the monitor using the base monitor.
func (d *DiskMonitor) Stop() {
	d.baseMonitor.Stop()
}

// checkDisk performs the Disk health check and returns a status report.
// This is the main check function that gets called periodically by the base monitor.
func (d *DiskMonitor) checkDisk(ctx context.Context) (*types.Status, error) {
	status := types.NewStatus(d.name)

	// Check disk space and inodes for each mount point
	for _, mountPoint := range d.config.MountPoints {
		if d.config.CheckDiskSpace || d.config.CheckInodes || d.config.CheckReadonly {
			if err := d.checkMountPoint(ctx, status, mountPoint); err != nil {
				// Log error but continue with other checks
				status.AddEvent(types.NewEvent(
					types.EventError,
					"DiskCheckFailed",
					fmt.Sprintf("Failed to check mount point %s: %v", mountPoint.Path, err),
				))
			}
		}
	}

	// Check I/O health if enabled
	if d.config.CheckIOHealth {
		if err := d.checkIOHealth(ctx, status); err != nil {
			// Log error but continue with other checks
			status.AddEvent(types.NewEvent(
				types.EventError,
				"IOHealthCheckFailed",
				fmt.Sprintf("Failed to check I/O health: %v", err),
			))
		}
	}

	// If no events were added and checks are enabled, add a healthy condition
	if len(status.Events) == 0 && (d.config.CheckDiskSpace || d.config.CheckInodes || d.config.CheckReadonly || d.config.CheckIOHealth) {
		status.AddCondition(types.NewCondition(
			"DiskHealthy",
			types.ConditionTrue,
			"DiskHealthy",
			"All disk checks are operating normally",
		))
	}

	return status, nil
}

// checkMountPoint checks disk space, inodes, and readonly status for a specific mount point.
func (d *DiskMonitor) checkMountPoint(ctx context.Context, status *types.Status, mountPoint MountPointConfig) error {
	// Get filesystem statistics - this will handle missing mount points via the provider
	fsStats, err := d.statfsProvider.Statfs(mountPoint.Path)
	if err != nil {
		// Check if this is a "not exists" error that we should handle gracefully
		if err == syscall.ENOENT {
			// Skip with debug-level information - this is expected for optional mount points
			return nil
		}
		return fmt.Errorf("failed to get filesystem stats for %s: %w", mountPoint.Path, err)
	}

	// Parse disk information
	diskInfo := d.parseDiskInfo(mountPoint.Path, fsStats)

	// Check readonly filesystem first (immediate condition)
	if d.config.CheckReadonly && diskInfo.IsReadonly {
		status.AddEvent(types.NewEvent(
			types.EventError,
			"ReadonlyFilesystem",
			fmt.Sprintf("Readonly filesystem detected on %s", mountPoint.Path),
		))
		status.AddCondition(types.NewCondition(
			"ReadonlyFilesystem",
			types.ConditionTrue,
			"ReadonlyFilesystem",
			fmt.Sprintf("Filesystem %s is mounted readonly", mountPoint.Path),
		))
		return nil
	}

	// Check disk space usage
	if d.config.CheckDiskSpace {
		d.checkDiskSpaceUsage(status, mountPoint, diskInfo)
	}

	// Check inode usage
	if d.config.CheckInodes {
		d.checkInodeUsage(status, mountPoint, diskInfo)
	}

	return nil
}

// checkDiskSpaceUsage monitors disk space usage and generates appropriate events/conditions.
func (d *DiskMonitor) checkDiskSpaceUsage(status *types.Status, mountPoint MountPointConfig, diskInfo *DiskInfo) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Check for critical disk usage
	if diskInfo.UsagePercent >= mountPoint.CriticalThreshold {
		d.highDiskCount[mountPoint.Path]++

		// Generate immediate event for critical disk usage
		status.AddEvent(types.NewEvent(
			types.EventError,
			"HighDiskUsage",
			fmt.Sprintf("High disk usage detected on %s: %.1f%% (%.2f GB used of %.2f GB total, %.2f GB available)",
				mountPoint.Path,
				diskInfo.UsagePercent,
				float64(diskInfo.UsedSpace)/1024/1024/1024,
				float64(diskInfo.TotalSpace)/1024/1024/1024,
				float64(diskInfo.AvailableSpace)/1024/1024/1024),
		))

		// Always generate condition for critical disk usage
		if d.highDiskCount[mountPoint.Path] >= d.config.SustainedHighDiskChecks {
			status.AddCondition(types.NewCondition(
				"DiskPressure",
				types.ConditionTrue,
				"SustainedHighDisk",
				fmt.Sprintf("Disk %s has been under sustained high usage for %d consecutive checks (%.1f%% >= %.1f%%)",
					mountPoint.Path, d.highDiskCount[mountPoint.Path], diskInfo.UsagePercent, mountPoint.CriticalThreshold),
			))
		} else {
			status.AddCondition(types.NewCondition(
				"DiskPressure",
				types.ConditionFalse,
				"HighDisk",
				fmt.Sprintf("Disk usage on %s is critical but not yet sustained (%.1f%% >= %.1f%%)",
					mountPoint.Path, diskInfo.UsagePercent, mountPoint.CriticalThreshold),
			))
		}
	} else if diskInfo.UsagePercent >= mountPoint.WarningThreshold {
		// Increment counter for elevated usage (between warning and critical)
		d.highDiskCount[mountPoint.Path]++

		// Generate warning event
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"ElevatedDiskUsage",
			fmt.Sprintf("Elevated disk usage detected on %s: %.1f%% (%.2f GB used of %.2f GB total, %.2f GB available)",
				mountPoint.Path,
				diskInfo.UsagePercent,
				float64(diskInfo.UsedSpace)/1024/1024/1024,
				float64(diskInfo.TotalSpace)/1024/1024/1024,
				float64(diskInfo.AvailableSpace)/1024/1024/1024),
		))

		// Add condition indicating no pressure but elevated usage
		status.AddCondition(types.NewCondition(
			"DiskPressure",
			types.ConditionFalse,
			"ElevatedDisk",
			fmt.Sprintf("Disk usage on %s is elevated but not critical (%.1f%%)", mountPoint.Path, diskInfo.UsagePercent),
		))
	} else {
		// Reset high disk counter on normal usage
		d.highDiskCount[mountPoint.Path] = 0

		// Add condition indicating normal disk state
		status.AddCondition(types.NewCondition(
			"DiskPressure",
			types.ConditionFalse,
			"NormalDisk",
			fmt.Sprintf("Disk usage on %s is normal (%.1f%%)", mountPoint.Path, diskInfo.UsagePercent),
		))
	}
}

// checkInodeUsage monitors inode usage and generates appropriate events.
func (d *DiskMonitor) checkInodeUsage(status *types.Status, mountPoint MountPointConfig, diskInfo *DiskInfo) {
	// Skip inode checks if filesystem doesn't support inodes
	if diskInfo.TotalInodes == 0 {
		return
	}

	// Check for critical inode usage
	if diskInfo.InodePercent >= mountPoint.InodeCriticalThreshold {
		status.AddEvent(types.NewEvent(
			types.EventError,
			"HighInodeUsage",
			fmt.Sprintf("High inode usage detected on %s: %.1f%% (%d used of %d total)",
				mountPoint.Path,
				diskInfo.InodePercent,
				diskInfo.UsedInodes,
				diskInfo.TotalInodes),
		))
		status.AddCondition(types.NewCondition(
			"InodePressure",
			types.ConditionTrue,
			"CriticalInode",
			fmt.Sprintf("Inode usage on %s is critical (%.1f%% >= %.1f%%)", mountPoint.Path, diskInfo.InodePercent, mountPoint.InodeCriticalThreshold),
		))
	} else if diskInfo.InodePercent >= mountPoint.InodeWarningThreshold {
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"ElevatedInodeUsage",
			fmt.Sprintf("Elevated inode usage detected on %s: %.1f%% (%d used of %d total)",
				mountPoint.Path,
				diskInfo.InodePercent,
				diskInfo.UsedInodes,
				diskInfo.TotalInodes),
		))
		status.AddCondition(types.NewCondition(
			"InodePressure",
			types.ConditionFalse,
			"ElevatedInode",
			fmt.Sprintf("Inode usage on %s is elevated but not critical (%.1f%%)", mountPoint.Path, diskInfo.InodePercent),
		))
	} else {
		// Add condition indicating normal inode state
		status.AddCondition(types.NewCondition(
			"InodePressure",
			types.ConditionFalse,
			"NormalInode",
			fmt.Sprintf("Inode usage on %s is normal (%.1f%%)", mountPoint.Path, diskInfo.InodePercent),
		))
	}
}

// checkIOHealth monitors I/O health via /proc/diskstats.
func (d *DiskMonitor) checkIOHealth(ctx context.Context, status *types.Status) error {
	data, err := d.fileReader.ReadFile(d.config.DiskStatsPath)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", d.config.DiskStatsPath, err)
	}

	currentTime := ctx.Value("timestamp")
	if currentTime == nil {
		return nil // Skip if no timestamp context
	}

	currentTimeNs, ok := currentTime.(int64)
	if !ok {
		return nil // Skip if invalid timestamp
	}

	// Parse diskstats
	currentStats := d.parseDiskStats(string(data))

	// Check I/O utilization if we have previous stats
	if d.lastIOCheckTime > 0 {
		elapsedMs := (currentTimeNs - d.lastIOCheckTime) / 1000000 // Convert to milliseconds

		for device, currentStat := range currentStats {
			if lastStat, exists := d.lastIOStats[device]; exists && elapsedMs > 0 {
				// Calculate I/O utilization: (io_time_delta / elapsed_time_ms) * 100
				ioTimeDelta := currentStat.TimeDoingIO - lastStat.TimeDoingIO
				ioUtilization := float64(ioTimeDelta) / float64(elapsedMs) * 100.0

				// Generate event if I/O utilization is high
				if ioUtilization > 80.0 {
					status.AddEvent(types.NewEvent(
						types.EventWarning,
						"HighIOWait",
						fmt.Sprintf("High I/O utilization detected on device %s: %.1f%%", device, ioUtilization),
					))
				}
			}
		}
	}

	// Update state
	d.lastIOStats = currentStats
	d.lastIOCheckTime = currentTimeNs

	return nil
}

// parseDiskInfo converts filesystem statistics to DiskInfo.
func (d *DiskMonitor) parseDiskInfo(path string, fsStats *FilesystemStats) *DiskInfo {
	// Constants for readonly flag (following syscall naming convention)
	const stRdonly = 0x0001 //nolint:revive // matches syscall constant naming

	// Calculate sizes in bytes
	totalSpace := fsStats.Blocks * uint64(fsStats.BlockSize)
	availableSpace := fsStats.BlocksAvail * uint64(fsStats.BlockSize)
	freeSpace := fsStats.BlocksFree * uint64(fsStats.BlockSize)
	usedSpace := totalSpace - availableSpace

	// Calculate usage percentage
	var usagePercent float64
	if totalSpace > 0 {
		usagePercent = (float64(usedSpace) / float64(totalSpace)) * 100.0
	}

	// Calculate inode usage
	usedInodes := fsStats.Files - fsStats.FilesFree
	var inodePercent float64
	if fsStats.Files > 0 {
		inodePercent = (float64(usedInodes) / float64(fsStats.Files)) * 100.0
	}

	// Check readonly status
	isReadonly := (fsStats.Flags & stRdonly) != 0

	return &DiskInfo{
		Path:           path,
		TotalSpace:     totalSpace,
		FreeSpace:      freeSpace,
		AvailableSpace: availableSpace,
		UsedSpace:      usedSpace,
		UsagePercent:   usagePercent,
		TotalInodes:    fsStats.Files,
		FreeInodes:     fsStats.FilesFree,
		UsedInodes:     usedInodes,
		InodePercent:   inodePercent,
		IsReadonly:     isReadonly,
	}
}

// parseDiskStats parses /proc/diskstats format.
func (d *DiskMonitor) parseDiskStats(data string) map[string]*IOStats {
	stats := make(map[string]*IOStats)

	lines := strings.Split(data, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Parse diskstats format: major minor name reads ...
		fields := strings.Fields(line)
		if len(fields) < 14 {
			continue // Skip malformed lines
		}

		device := fields[2]

		// Parse numeric fields, skip invalid lines
		readsCompleted, err := strconv.ParseUint(fields[3], 10, 64)
		if err != nil {
			continue
		}
		readsMerged, err := strconv.ParseUint(fields[4], 10, 64)
		if err != nil {
			continue
		}
		sectorsRead, err := strconv.ParseUint(fields[5], 10, 64)
		if err != nil {
			continue
		}
		timeReading, err := strconv.ParseUint(fields[6], 10, 64)
		if err != nil {
			continue
		}
		writesCompleted, err := strconv.ParseUint(fields[7], 10, 64)
		if err != nil {
			continue
		}
		writesMerged, err := strconv.ParseUint(fields[8], 10, 64)
		if err != nil {
			continue
		}
		sectorsWritten, err := strconv.ParseUint(fields[9], 10, 64)
		if err != nil {
			continue
		}
		timeWriting, err := strconv.ParseUint(fields[10], 10, 64)
		if err != nil {
			continue
		}
		iosInProgress, err := strconv.ParseUint(fields[11], 10, 64)
		if err != nil {
			continue
		}
		timeDoingIO, err := strconv.ParseUint(fields[12], 10, 64)
		if err != nil {
			continue
		}
		weightedTimeIO, err := strconv.ParseUint(fields[13], 10, 64)
		if err != nil {
			continue
		}

		stats[device] = &IOStats{
			Device:          device,
			ReadsCompleted:  readsCompleted,
			ReadsMerged:     readsMerged,
			SectorsRead:     sectorsRead,
			TimeReading:     timeReading,
			WritesCompleted: writesCompleted,
			WritesMerged:    writesMerged,
			SectorsWritten:  sectorsWritten,
			TimeWriting:     timeWriting,
			IOsInProgress:   iosInProgress,
			TimeDoingIO:     timeDoingIO,
			WeightedTimeIO:  weightedTimeIO,
		}
	}

	return stats
}

// parseDiskConfig extracts Disk-specific configuration from the generic config map.
func parseDiskConfig(configMap map[string]interface{}) (*DiskMonitorConfig, error) {
	config := &DiskMonitorConfig{}

	if configMap == nil {
		return config, nil
	}

	// Parse mount points
	if val, exists := configMap["mountPoints"]; exists {
		if mountPointsSlice, ok := val.([]interface{}); ok {
			for i, mp := range mountPointsSlice {
				if mpMap, ok := mp.(map[string]interface{}); ok {
					mountPoint, err := parseMountPointConfig(mpMap)
					if err != nil {
						return nil, fmt.Errorf("invalid mount point at index %d: %w", i, err)
					}
					config.MountPoints = append(config.MountPoints, *mountPoint)
				} else {
					return nil, fmt.Errorf("mount point at index %d must be an object, got %T", i, mp)
				}
			}
		} else {
			return nil, fmt.Errorf("mountPoints must be an array, got %T", val)
		}
	}

	// Parse sustained high disk checks
	if val, exists := configMap["sustainedHighDiskChecks"]; exists {
		if i, ok := val.(int); ok {
			config.SustainedHighDiskChecks = i
		} else if f, ok := val.(float64); ok {
			config.SustainedHighDiskChecks = int(f)
		} else {
			return nil, fmt.Errorf("sustainedHighDiskChecks must be a number, got %T", val)
		}
	}

	// Parse check disk space
	if val, exists := configMap["checkDiskSpace"]; exists {
		if b, ok := val.(bool); ok {
			config.CheckDiskSpace = b
		} else {
			return nil, fmt.Errorf("checkDiskSpace must be a boolean, got %T", val)
		}
	}

	// Parse check inodes
	if val, exists := configMap["checkInodes"]; exists {
		if b, ok := val.(bool); ok {
			config.CheckInodes = b
		} else {
			return nil, fmt.Errorf("checkInodes must be a boolean, got %T", val)
		}
	}

	// Parse check readonly
	if val, exists := configMap["checkReadonly"]; exists {
		if b, ok := val.(bool); ok {
			config.CheckReadonly = b
		} else {
			return nil, fmt.Errorf("checkReadonly must be a boolean, got %T", val)
		}
	}

	// Parse check I/O health
	if val, exists := configMap["checkIOHealth"]; exists {
		if b, ok := val.(bool); ok {
			config.CheckIOHealth = b
		} else {
			return nil, fmt.Errorf("checkIOHealth must be a boolean, got %T", val)
		}
	}

	// Parse disk stats path
	if val, exists := configMap["diskStatsPath"]; exists {
		if s, ok := val.(string); ok {
			config.DiskStatsPath = s
		} else {
			return nil, fmt.Errorf("diskStatsPath must be a string, got %T", val)
		}
	}

	return config, nil
}

// parseMountPointConfig parses a mount point configuration.
func parseMountPointConfig(configMap map[string]interface{}) (*MountPointConfig, error) {
	config := &MountPointConfig{}

	// Parse path (required)
	if val, exists := configMap["path"]; exists {
		if s, ok := val.(string); ok {
			config.Path = s
		} else {
			return nil, fmt.Errorf("path must be a string, got %T", val)
		}
	} else {
		return nil, fmt.Errorf("path is required for mount point")
	}

	// Parse warning threshold
	if val, exists := configMap["warningThreshold"]; exists {
		if f, ok := val.(float64); ok {
			config.WarningThreshold = f
		} else {
			return nil, fmt.Errorf("warningThreshold must be a number, got %T", val)
		}
	}

	// Parse critical threshold
	if val, exists := configMap["criticalThreshold"]; exists {
		if f, ok := val.(float64); ok {
			config.CriticalThreshold = f
		} else {
			return nil, fmt.Errorf("criticalThreshold must be a number, got %T", val)
		}
	}

	// Parse inode warning threshold
	if val, exists := configMap["inodeWarningThreshold"]; exists {
		if f, ok := val.(float64); ok {
			config.InodeWarningThreshold = f
		} else {
			return nil, fmt.Errorf("inodeWarningThreshold must be a number, got %T", val)
		}
	}

	// Parse inode critical threshold
	if val, exists := configMap["inodeCriticalThreshold"]; exists {
		if f, ok := val.(float64); ok {
			config.InodeCriticalThreshold = f
		} else {
			return nil, fmt.Errorf("inodeCriticalThreshold must be a number, got %T", val)
		}
	}

	return config, nil
}

// applyDefaults applies default values to the Disk monitor configuration.
func (c *DiskMonitorConfig) applyDefaults() error {
	if c.SustainedHighDiskChecks == 0 {
		c.SustainedHighDiskChecks = 3
	}
	if c.DiskStatsPath == "" {
		c.DiskStatsPath = "/proc/diskstats"
	}

	// Default mount points if none specified
	if len(c.MountPoints) == 0 {
		c.MountPoints = []MountPointConfig{
			{
				Path:                   "/",
				WarningThreshold:       85.0,
				CriticalThreshold:      95.0,
				InodeWarningThreshold:  85.0,
				InodeCriticalThreshold: 95.0,
			},
			{
				Path:                   "/var/lib/docker",
				WarningThreshold:       85.0,
				CriticalThreshold:      95.0,
				InodeWarningThreshold:  85.0,
				InodeCriticalThreshold: 95.0,
			},
			{
				Path:                   "/var/lib/containerd",
				WarningThreshold:       85.0,
				CriticalThreshold:      95.0,
				InodeWarningThreshold:  85.0,
				InodeCriticalThreshold: 95.0,
			},
		}
	}

	// Apply defaults to mount points
	for i := range c.MountPoints {
		mp := &c.MountPoints[i]
		if mp.WarningThreshold == 0 {
			mp.WarningThreshold = 85.0
		}
		if mp.CriticalThreshold == 0 {
			mp.CriticalThreshold = 95.0
		}
		if mp.InodeWarningThreshold == 0 {
			mp.InodeWarningThreshold = 85.0
		}
		if mp.InodeCriticalThreshold == 0 {
			mp.InodeCriticalThreshold = 95.0
		}
	}

	// Default to checking all if not explicitly configured
	if !c.CheckDiskSpace && !c.CheckInodes && !c.CheckReadonly && !c.CheckIOHealth {
		c.CheckDiskSpace = true
		c.CheckInodes = true
		c.CheckReadonly = true
		c.CheckIOHealth = true
	}

	// Validate thresholds after applying defaults
	for i, mp := range c.MountPoints {
		if mp.WarningThreshold <= 0 || mp.WarningThreshold > 100 {
			return fmt.Errorf("mount point %d: warningThreshold must be between 0 and 100, got %f", i, mp.WarningThreshold)
		}
		if mp.CriticalThreshold <= 0 || mp.CriticalThreshold > 100 {
			return fmt.Errorf("mount point %d: criticalThreshold must be between 0 and 100, got %f", i, mp.CriticalThreshold)
		}
		if mp.WarningThreshold >= mp.CriticalThreshold {
			return fmt.Errorf("mount point %d: warningThreshold (%f) must be less than criticalThreshold (%f)",
				i, mp.WarningThreshold, mp.CriticalThreshold)
		}

		if mp.InodeWarningThreshold <= 0 || mp.InodeWarningThreshold > 100 {
			return fmt.Errorf("mount point %d: inodeWarningThreshold must be between 0 and 100, got %f", i, mp.InodeWarningThreshold)
		}
		if mp.InodeCriticalThreshold <= 0 || mp.InodeCriticalThreshold > 100 {
			return fmt.Errorf("mount point %d: inodeCriticalThreshold must be between 0 and 100, got %f", i, mp.InodeCriticalThreshold)
		}
		if mp.InodeWarningThreshold >= mp.InodeCriticalThreshold {
			return fmt.Errorf("mount point %d: inodeWarningThreshold (%f) must be less than inodeCriticalThreshold (%f)",
				i, mp.InodeWarningThreshold, mp.InodeCriticalThreshold)
		}

		if mp.Path == "" {
			return fmt.Errorf("mount point %d: path is required", i)
		}
	}

	// Validate sustained disk checks
	if c.SustainedHighDiskChecks <= 0 {
		return fmt.Errorf("sustainedHighDiskChecks must be positive, got %d", c.SustainedHighDiskChecks)
	}

	return nil
}
