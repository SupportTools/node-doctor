// Package detector provides thread-safe statistics tracking for the Problem Detector.
package detector

import (
	"sync"
	"time"
)

// Statistics tracks operational metrics for the Problem Detector.
// All methods are thread-safe and can be called concurrently.
type Statistics struct {
	mu                   sync.RWMutex // Protects all fields
	monitorsStarted      int64        // Number of monitors successfully started
	monitorsFailed       int64        // Number of monitors that failed to start
	statusesReceived     int64        // Total status updates received
	problemsDetected     int64        // Total problems detected from statuses
	problemsDeduplicated int64        // Total problems after deduplication
	exportsSucceeded     int64        // Successful export operations
	exportsFailed        int64        // Failed export operations
	startTime            time.Time    // When statistics tracking started
}

// NewStatistics creates a new Statistics instance with current timestamp.
func NewStatistics() *Statistics {
	return &Statistics{
		startTime: time.Now(),
	}
}

// IncrementMonitorsStarted atomically increments the monitors started counter.
func (s *Statistics) IncrementMonitorsStarted() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.monitorsStarted++
}

// IncrementMonitorsFailed atomically increments the monitors failed counter.
func (s *Statistics) IncrementMonitorsFailed() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.monitorsFailed++
}

// IncrementStatusesReceived atomically increments the statuses received counter.
func (s *Statistics) IncrementStatusesReceived() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statusesReceived++
}

// AddProblemsDetected atomically adds to the problems detected counter.
func (s *Statistics) AddProblemsDetected(count int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.problemsDetected += int64(count)
}

// AddProblemsDeduplicated atomically adds to the problems deduplicated counter.
func (s *Statistics) AddProblemsDeduplicated(count int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.problemsDeduplicated += int64(count)
}

// IncrementExportsSucceeded atomically increments the successful exports counter.
func (s *Statistics) IncrementExportsSucceeded() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.exportsSucceeded++
}

// IncrementExportsFailed atomically increments the failed exports counter.
func (s *Statistics) IncrementExportsFailed() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.exportsFailed++
}

// GetMonitorsStarted returns the number of monitors successfully started.
func (s *Statistics) GetMonitorsStarted() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.monitorsStarted
}

// GetMonitorsFailed returns the number of monitors that failed to start.
func (s *Statistics) GetMonitorsFailed() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.monitorsFailed
}

// GetStatusesReceived returns the total number of status updates received.
func (s *Statistics) GetStatusesReceived() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.statusesReceived
}

// GetProblemsDetected returns the total number of problems detected.
func (s *Statistics) GetProblemsDetected() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.problemsDetected
}

// GetProblemsDeduplicated returns the total number of problems after deduplication.
func (s *Statistics) GetProblemsDeduplicated() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.problemsDeduplicated
}

// GetExportsSucceeded returns the number of successful export operations.
func (s *Statistics) GetExportsSucceeded() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.exportsSucceeded
}

// GetExportsFailed returns the number of failed export operations.
func (s *Statistics) GetExportsFailed() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.exportsFailed
}

// GetStartTime returns when statistics tracking started.
func (s *Statistics) GetStartTime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.startTime
}

// GetUptime returns how long statistics have been tracked.
func (s *Statistics) GetUptime() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return time.Since(s.startTime)
}

// GetTotalExports returns the total number of export operations (successful + failed).
func (s *Statistics) GetTotalExports() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.exportsSucceeded + s.exportsFailed
}

// GetExportSuccessRate returns the export success rate as a percentage (0-100).
// Returns 0 if no exports have been attempted.
func (s *Statistics) GetExportSuccessRate() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	total := s.exportsSucceeded + s.exportsFailed
	if total == 0 {
		return 0.0
	}

	return float64(s.exportsSucceeded) / float64(total) * 100.0
}

// GetDeduplicationRate returns the deduplication rate as a percentage (0-100).
// This shows what percentage of detected problems were new after deduplication.
// Returns 0 if no problems have been detected.
func (s *Statistics) GetDeduplicationRate() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.problemsDetected == 0 {
		return 0.0
	}

	return float64(s.problemsDeduplicated) / float64(s.problemsDetected) * 100.0
}

// Copy returns a complete copy of the current statistics.
// This is useful for taking a snapshot without holding the lock.
func (s *Statistics) Copy() Statistics {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return Statistics{
		monitorsStarted:      s.monitorsStarted,
		monitorsFailed:       s.monitorsFailed,
		statusesReceived:     s.statusesReceived,
		problemsDetected:     s.problemsDetected,
		problemsDeduplicated: s.problemsDeduplicated,
		exportsSucceeded:     s.exportsSucceeded,
		exportsFailed:        s.exportsFailed,
		startTime:            s.startTime,
	}
}

// Reset resets all counters to zero and updates the start time.
// This is primarily useful for testing scenarios.
func (s *Statistics) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.monitorsStarted = 0
	s.monitorsFailed = 0
	s.statusesReceived = 0
	s.problemsDetected = 0
	s.problemsDeduplicated = 0
	s.exportsSucceeded = 0
	s.exportsFailed = 0
	s.startTime = time.Now()
}

// Summary returns a human-readable summary of all statistics.
func (s *Statistics) Summary() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]interface{}{
		"uptime":                  time.Since(s.startTime).String(),
		"monitors_started":        s.monitorsStarted,
		"monitors_failed":         s.monitorsFailed,
		"statuses_received":       s.statusesReceived,
		"problems_detected":       s.problemsDetected,
		"problems_deduplicated":   s.problemsDeduplicated,
		"exports_succeeded":       s.exportsSucceeded,
		"exports_failed":          s.exportsFailed,
		"total_exports":           s.exportsSucceeded + s.exportsFailed,
		"export_success_rate_pct": s.getExportSuccessRateUnsafe(),
		"deduplication_rate_pct":  s.getDeduplicationRateUnsafe(),
	}
}

// getExportSuccessRateUnsafe calculates export success rate without locking.
// This is used internally when the lock is already held.
func (s *Statistics) getExportSuccessRateUnsafe() float64 {
	total := s.exportsSucceeded + s.exportsFailed
	if total == 0 {
		return 0.0
	}
	return float64(s.exportsSucceeded) / float64(total) * 100.0
}

// getDeduplicationRateUnsafe calculates deduplication rate without locking.
// This is used internally when the lock is already held.
func (s *Statistics) getDeduplicationRateUnsafe() float64 {
	if s.problemsDetected == 0 {
		return 0.0
	}
	return float64(s.problemsDeduplicated) / float64(s.problemsDetected) * 100.0
}
