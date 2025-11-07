package http

import (
	"sync"
	"time"
)

// Stats tracks HTTP exporter statistics
type Stats struct {
	mu                    sync.RWMutex
	startTime             time.Time
	statusExportsTotal    int64
	statusExportsSuccess  int64
	statusExportsFailed   int64
	problemExportsTotal   int64
	problemExportsSuccess int64
	problemExportsFailed  int64
	requestsQueued        int64
	requestsDropped       int64
	lastExportTime        time.Time
	lastError             error
	lastErrorTime         time.Time
	webhookStats          map[string]*WebhookStats
}

// WebhookStats tracks per-webhook statistics
type WebhookStats struct {
	mu                sync.RWMutex
	name              string
	requestsTotal     int64
	requestsSuccess   int64
	requestsFailed    int64
	lastRequestTime   time.Time
	lastSuccessTime   time.Time
	lastError         error
	lastErrorTime     time.Time
	totalResponseTime time.Duration
	avgResponseTime   time.Duration
	retryAttempts     int64
}

// NewStats creates a new Stats instance
func NewStats() *Stats {
	return &Stats{
		startTime:    time.Now(),
		webhookStats: make(map[string]*WebhookStats),
	}
}

// NewWebhookStats creates a new WebhookStats instance
func NewWebhookStats(name string) *WebhookStats {
	return &WebhookStats{
		name: name,
	}
}

// GetWebhookStats returns or creates webhook-specific stats
func (s *Stats) GetWebhookStats(webhookName string) *WebhookStats {
	s.mu.Lock()
	defer s.mu.Unlock()

	stats, exists := s.webhookStats[webhookName]
	if !exists {
		stats = NewWebhookStats(webhookName)
		s.webhookStats[webhookName] = stats
	}
	return stats
}

// RecordStatusExport records a status export attempt
func (s *Stats) RecordStatusExport(success bool, webhookName string, responseTime time.Duration, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.statusExportsTotal++
	s.lastExportTime = time.Now()

	if success {
		s.statusExportsSuccess++
	} else {
		s.statusExportsFailed++
		s.lastError = err
		s.lastErrorTime = time.Now()
	}

	// Update webhook-specific stats
	if webhookStats, exists := s.webhookStats[webhookName]; exists {
		webhookStats.recordRequest(success, responseTime, err)
	}
}

// RecordProblemExport records a problem export attempt
func (s *Stats) RecordProblemExport(success bool, webhookName string, responseTime time.Duration, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.problemExportsTotal++
	s.lastExportTime = time.Now()

	if success {
		s.problemExportsSuccess++
	} else {
		s.problemExportsFailed++
		s.lastError = err
		s.lastErrorTime = time.Now()
	}

	// Update webhook-specific stats
	if webhookStats, exists := s.webhookStats[webhookName]; exists {
		webhookStats.recordRequest(success, responseTime, err)
	}
}

// RecordQueuedRequest records a request being queued
func (s *Stats) RecordQueuedRequest() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requestsQueued++
}

// RecordDroppedRequest records a request being dropped
func (s *Stats) RecordDroppedRequest() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requestsDropped++
}

// RecordRetryAttempt records a retry attempt for a webhook
func (s *Stats) RecordRetryAttempt(webhookName string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if webhookStats, exists := s.webhookStats[webhookName]; exists {
		webhookStats.mu.Lock()
		webhookStats.retryAttempts++
		webhookStats.mu.Unlock()
	}
}

// GetSnapshot returns a snapshot of current statistics
func (s *Stats) GetSnapshot() StatsSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snapshot := StatsSnapshot{
		StartTime:             s.startTime,
		StatusExportsTotal:    s.statusExportsTotal,
		StatusExportsSuccess:  s.statusExportsSuccess,
		StatusExportsFailed:   s.statusExportsFailed,
		ProblemExportsTotal:   s.problemExportsTotal,
		ProblemExportsSuccess: s.problemExportsSuccess,
		ProblemExportsFailed:  s.problemExportsFailed,
		RequestsQueued:        s.requestsQueued,
		RequestsDropped:       s.requestsDropped,
		LastExportTime:        s.lastExportTime,
		LastError:             s.lastError,
		LastErrorTime:         s.lastErrorTime,
		WebhookStats:          make(map[string]WebhookStatsSnapshot),
	}

	// Copy webhook stats
	for name, stats := range s.webhookStats {
		snapshot.WebhookStats[name] = stats.getSnapshot()
	}

	return snapshot
}

// recordRequest records a request for webhook-specific stats
func (ws *WebhookStats) recordRequest(success bool, responseTime time.Duration, err error) {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	ws.requestsTotal++
	ws.lastRequestTime = time.Now()

	if success {
		ws.requestsSuccess++
		ws.lastSuccessTime = time.Now()
	} else {
		ws.requestsFailed++
		ws.lastError = err
		ws.lastErrorTime = time.Now()
	}

	// Update response time statistics
	if responseTime > 0 {
		ws.totalResponseTime += responseTime
		if ws.requestsTotal > 0 {
			ws.avgResponseTime = ws.totalResponseTime / time.Duration(ws.requestsTotal)
		}
	}
}

// getSnapshot returns a snapshot of webhook statistics
func (ws *WebhookStats) getSnapshot() WebhookStatsSnapshot {
	ws.mu.RLock()
	defer ws.mu.RUnlock()

	return WebhookStatsSnapshot{
		Name:            ws.name,
		RequestsTotal:   ws.requestsTotal,
		RequestsSuccess: ws.requestsSuccess,
		RequestsFailed:  ws.requestsFailed,
		LastRequestTime: ws.lastRequestTime,
		LastSuccessTime: ws.lastSuccessTime,
		LastError:       ws.lastError,
		LastErrorTime:   ws.lastErrorTime,
		AvgResponseTime: ws.avgResponseTime,
		RetryAttempts:   ws.retryAttempts,
	}
}

// StatsSnapshot provides a point-in-time view of statistics
type StatsSnapshot struct {
	StartTime             time.Time                       `json:"startTime"`
	StatusExportsTotal    int64                           `json:"statusExportsTotal"`
	StatusExportsSuccess  int64                           `json:"statusExportsSuccess"`
	StatusExportsFailed   int64                           `json:"statusExportsFailed"`
	ProblemExportsTotal   int64                           `json:"problemExportsTotal"`
	ProblemExportsSuccess int64                           `json:"problemExportsSuccess"`
	ProblemExportsFailed  int64                           `json:"problemExportsFailed"`
	RequestsQueued        int64                           `json:"requestsQueued"`
	RequestsDropped       int64                           `json:"requestsDropped"`
	LastExportTime        time.Time                       `json:"lastExportTime"`
	LastError             error                           `json:"lastError,omitempty"`
	LastErrorTime         time.Time                       `json:"lastErrorTime"`
	WebhookStats          map[string]WebhookStatsSnapshot `json:"webhookStats"`
}

// WebhookStatsSnapshot provides a point-in-time view of webhook statistics
type WebhookStatsSnapshot struct {
	Name            string        `json:"name"`
	RequestsTotal   int64         `json:"requestsTotal"`
	RequestsSuccess int64         `json:"requestsSuccess"`
	RequestsFailed  int64         `json:"requestsFailed"`
	LastRequestTime time.Time     `json:"lastRequestTime"`
	LastSuccessTime time.Time     `json:"lastSuccessTime"`
	LastError       error         `json:"lastError,omitempty"`
	LastErrorTime   time.Time     `json:"lastErrorTime"`
	AvgResponseTime time.Duration `json:"avgResponseTime"`
	RetryAttempts   int64         `json:"retryAttempts"`
}

// GetUptime returns the uptime duration
func (s StatsSnapshot) GetUptime() time.Duration {
	return time.Since(s.StartTime)
}

// GetTotalExports returns the total number of exports
func (s StatsSnapshot) GetTotalExports() int64 {
	return s.StatusExportsTotal + s.ProblemExportsTotal
}

// GetTotalSuccesses returns the total number of successful exports
func (s StatsSnapshot) GetTotalSuccesses() int64 {
	return s.StatusExportsSuccess + s.ProblemExportsSuccess
}

// GetTotalFailures returns the total number of failed exports
func (s StatsSnapshot) GetTotalFailures() int64 {
	return s.StatusExportsFailed + s.ProblemExportsFailed
}

// GetSuccessRate returns the success rate as a percentage (0-100)
func (s StatsSnapshot) GetSuccessRate() float64 {
	total := s.GetTotalExports()
	if total == 0 {
		return 0.0
	}
	return float64(s.GetTotalSuccesses()) / float64(total) * 100.0
}

// GetStatusSuccessRate returns the status export success rate as a percentage
func (s StatsSnapshot) GetStatusSuccessRate() float64 {
	if s.StatusExportsTotal == 0 {
		return 0.0
	}
	return float64(s.StatusExportsSuccess) / float64(s.StatusExportsTotal) * 100.0
}

// GetProblemSuccessRate returns the problem export success rate as a percentage
func (s StatsSnapshot) GetProblemSuccessRate() float64 {
	if s.ProblemExportsTotal == 0 {
		return 0.0
	}
	return float64(s.ProblemExportsSuccess) / float64(s.ProblemExportsTotal) * 100.0
}

// GetSuccessRate returns the webhook success rate as a percentage
func (ws WebhookStatsSnapshot) GetSuccessRate() float64 {
	if ws.RequestsTotal == 0 {
		return 0.0
	}
	return float64(ws.RequestsSuccess) / float64(ws.RequestsTotal) * 100.0
}

// IsHealthy returns true if the webhook is considered healthy
func (ws WebhookStatsSnapshot) IsHealthy() bool {
	// Consider healthy if:
	// 1. No requests yet (neutral state)
	// 2. Success rate >= 90% and at least 1 successful request in last 5 minutes
	if ws.RequestsTotal == 0 {
		return true
	}

	successRate := ws.GetSuccessRate()
	recentSuccess := time.Since(ws.LastSuccessTime) <= 5*time.Minute

	return successRate >= 90.0 && recentSuccess
}
