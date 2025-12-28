// Package controller provides the Node Doctor Controller for multi-node aggregation,
// correlation, and coordinated remediation.
package controller

import (
	"time"
)

// API Version
const (
	APIVersion = "v1"
)

// =====================
// Node Report Types
// =====================

// NodeReport represents a health report from a node-doctor DaemonSet pod.
// This is the primary data structure sent from nodes to the controller.
type NodeReport struct {
	// Node identification
	NodeName string `json:"nodeName"`
	NodeUID  string `json:"nodeUID,omitempty"`

	// Report metadata
	Timestamp  time.Time `json:"timestamp"`
	ReportID   string    `json:"reportId,omitempty"`
	Version    string    `json:"version,omitempty"`    // node-doctor version
	Uptime     string    `json:"uptime,omitempty"`     // node-doctor uptime
	ReportType string    `json:"reportType,omitempty"` // "periodic", "on-demand", "startup"

	// Health summary
	OverallHealth   HealthStatus     `json:"overallHealth"`
	MonitorStatuses []MonitorStatus  `json:"monitorStatuses,omitempty"`
	ActiveProblems  []ProblemSummary `json:"activeProblems,omitempty"`
	Conditions      []NodeCondition  `json:"conditions,omitempty"`

	// Statistics
	Stats *NodeStats `json:"stats,omitempty"`
}

// HealthStatus represents the overall health state
type HealthStatus string

const (
	HealthStatusHealthy  HealthStatus = "healthy"
	HealthStatusDegraded HealthStatus = "degraded"
	HealthStatusCritical HealthStatus = "critical"
	HealthStatusUnknown  HealthStatus = "unknown"
)

// MonitorStatus represents the status of a single monitor
type MonitorStatus struct {
	Name       string       `json:"name"`
	Type       string       `json:"type"`
	Status     HealthStatus `json:"status"`
	LastRun    time.Time    `json:"lastRun"`
	Message    string       `json:"message,omitempty"`
	ErrorCount int          `json:"errorCount,omitempty"`
}

// ProblemSummary represents an active problem on the node
type ProblemSummary struct {
	Type        string    `json:"type"`
	Severity    string    `json:"severity"`
	Message     string    `json:"message"`
	Source      string    `json:"source"`
	DetectedAt  time.Time `json:"detectedAt"`
	LastSeenAt  time.Time `json:"lastSeenAt"`
	Occurrences int       `json:"occurrences,omitempty"`
}

// NodeCondition represents a Kubernetes-style node condition
type NodeCondition struct {
	Type               string    `json:"type"`
	Status             string    `json:"status"` // True, False, Unknown
	Reason             string    `json:"reason,omitempty"`
	Message            string    `json:"message,omitempty"`
	LastTransitionTime time.Time `json:"lastTransitionTime,omitempty"`
	LastHeartbeatTime  time.Time `json:"lastHeartbeatTime,omitempty"`
}

// NodeStats contains statistics about the node-doctor instance
type NodeStats struct {
	StatusesProcessed int64  `json:"statusesProcessed"`
	ProblemsDetected  int64  `json:"problemsDetected"`
	RemediationsRun   int64  `json:"remediationsRun"`
	MemoryUsageBytes  int64  `json:"memoryUsageBytes,omitempty"`
	GoroutineCount    int    `json:"goroutineCount,omitempty"`
	CPUUsagePercent   string `json:"cpuUsagePercent,omitempty"`
}

// =====================
// Cluster Status Types
// =====================

// ClusterStatus represents the overall cluster health status
type ClusterStatus struct {
	Timestamp      time.Time        `json:"timestamp"`
	OverallHealth  HealthStatus     `json:"overallHealth"`
	TotalNodes     int              `json:"totalNodes"`
	HealthyNodes   int              `json:"healthyNodes"`
	DegradedNodes  int              `json:"degradedNodes"`
	CriticalNodes  int              `json:"criticalNodes"`
	UnknownNodes   int              `json:"unknownNodes"`
	ActiveProblems int              `json:"activeProblems"`
	Correlations   int              `json:"activeCorrelations"`
	NodeSummaries  []NodeSummary    `json:"nodeSummaries,omitempty"`
	RecentProblems []ClusterProblem `json:"recentProblems,omitempty"`
}

// NodeSummary provides a brief overview of a node's status
type NodeSummary struct {
	NodeName       string       `json:"nodeName"`
	Health         HealthStatus `json:"health"`
	LastReportAt   time.Time    `json:"lastReportAt"`
	ProblemCount   int          `json:"problemCount"`
	ConditionCount int          `json:"conditionCount"`
}

// ClusterProblem represents a problem affecting the cluster
type ClusterProblem struct {
	ID            string    `json:"id"`
	Type          string    `json:"type"`
	Severity      string    `json:"severity"`
	AffectedNodes []string  `json:"affectedNodes"`
	Message       string    `json:"message"`
	DetectedAt    time.Time `json:"detectedAt"`
	IsCorrelated  bool      `json:"isCorrelated"`
	CorrelationID string    `json:"correlationId,omitempty"`
}

// =====================
// Node Detail Types
// =====================

// NodeDetail provides detailed information about a specific node
type NodeDetail struct {
	NodeName       string           `json:"nodeName"`
	NodeUID        string           `json:"nodeUID,omitempty"`
	Health         HealthStatus     `json:"health"`
	LastReportAt   time.Time        `json:"lastReportAt"`
	FirstSeenAt    time.Time        `json:"firstSeenAt"`
	ReportCount    int64            `json:"reportCount"`
	LatestReport   *NodeReport      `json:"latestReport,omitempty"`
	ActiveProblems []ProblemSummary `json:"activeProblems"`
	Conditions     []NodeCondition  `json:"conditions"`
	RecentHistory  []ReportSummary  `json:"recentHistory,omitempty"`
}

// ReportSummary is a condensed view of a historical report
type ReportSummary struct {
	ReportID      string       `json:"reportId"`
	Timestamp     time.Time    `json:"timestamp"`
	OverallHealth HealthStatus `json:"overallHealth"`
	ProblemCount  int          `json:"problemCount"`
}

// =====================
// Correlation Types
// =====================

// Correlation represents a detected pattern across multiple nodes
type Correlation struct {
	ID            string                 `json:"id"`
	Type          string                 `json:"type"` // infrastructure, common-cause, cascade
	Severity      string                 `json:"severity"`
	AffectedNodes []string               `json:"affectedNodes"`
	ProblemTypes  []string               `json:"problemTypes"`
	Message       string                 `json:"message"`
	DetectedAt    time.Time              `json:"detectedAt"`
	UpdatedAt     time.Time              `json:"updatedAt"`
	Status        string                 `json:"status"` // active, resolved, investigating
	Confidence    float64                `json:"confidence"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// =====================
// Remediation Lease Types
// =====================

// LeaseRequest represents a request for a remediation lease
type LeaseRequest struct {
	NodeName          string `json:"node"`
	RemediationType   string `json:"remediation"`
	RequestedDuration string `json:"requestedDuration,omitempty"` // e.g., "5m"
	Reason            string `json:"reason,omitempty"`
	Priority          int    `json:"priority,omitempty"` // higher = more urgent
}

// LeaseResponse represents the response to a lease request
type LeaseResponse struct {
	LeaseID   string    `json:"leaseId,omitempty"`
	Approved  bool      `json:"approved"`
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
	Message   string    `json:"message,omitempty"`
	RetryAt   time.Time `json:"retryAt,omitempty"`  // when to retry if denied
	Position  int       `json:"position,omitempty"` // queue position if waiting
}

// Lease represents an active remediation lease
type Lease struct {
	ID              string    `json:"id"`
	NodeName        string    `json:"nodeName"`
	RemediationType string    `json:"remediationType"`
	GrantedAt       time.Time `json:"grantedAt"`
	ExpiresAt       time.Time `json:"expiresAt"`
	CompletedAt     time.Time `json:"completedAt,omitempty"`
	Status          string    `json:"status"` // active, completed, expired, cancelled
	Reason          string    `json:"reason,omitempty"`
}

// =====================
// API Response Wrappers
// =====================

// APIResponse is a generic wrapper for API responses
type APIResponse struct {
	Success   bool        `json:"success"`
	Data      interface{} `json:"data,omitempty"`
	Error     *APIError   `json:"error,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
}

// APIError represents an API error
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}

// PaginatedResponse wraps paginated list responses
type PaginatedResponse struct {
	Items      interface{} `json:"items"`
	TotalCount int         `json:"totalCount"`
	Page       int         `json:"page"`
	PageSize   int         `json:"pageSize"`
	HasMore    bool        `json:"hasMore"`
}

// =====================
// Configuration Types
// =====================

// ControllerConfig holds the controller configuration
type ControllerConfig struct {
	// Server settings
	Server ServerConfig `json:"server" yaml:"server"`

	// Storage settings
	Storage StorageConfig `json:"storage" yaml:"storage"`

	// Correlation settings
	Correlation CorrelationConfig `json:"correlation" yaml:"correlation"`

	// Coordination settings
	Coordination CoordinationConfig `json:"coordination" yaml:"coordination"`

	// Prometheus settings
	Prometheus PrometheusConfig `json:"prometheus" yaml:"prometheus"`

	// Kubernetes settings
	Kubernetes KubernetesConfig `json:"kubernetes" yaml:"kubernetes"`
}

// ServerConfig contains HTTP server configuration
type ServerConfig struct {
	BindAddress  string        `json:"bindAddress" yaml:"bindAddress"`
	Port         int           `json:"port" yaml:"port"`
	ReadTimeout  time.Duration `json:"readTimeout" yaml:"readTimeout"`
	WriteTimeout time.Duration `json:"writeTimeout" yaml:"writeTimeout"`
	EnableCORS   bool          `json:"enableCORS" yaml:"enableCORS"`
}

// StorageConfig contains SQLite storage configuration
type StorageConfig struct {
	Path      string        `json:"path" yaml:"path"`
	Retention time.Duration `json:"retention" yaml:"retention"` // How long to keep reports
}

// CorrelationConfig contains correlation engine settings
type CorrelationConfig struct {
	Enabled                bool          `json:"enabled" yaml:"enabled"`
	ClusterWideThreshold   float64       `json:"clusterWideThreshold" yaml:"clusterWideThreshold"`
	EvaluationInterval     time.Duration `json:"evaluationInterval" yaml:"evaluationInterval"`
	MinNodesForCorrelation int           `json:"minNodesForCorrelation" yaml:"minNodesForCorrelation"`
}

// CoordinationConfig contains remediation coordination settings
type CoordinationConfig struct {
	Enabled                   bool          `json:"enabled" yaml:"enabled"`
	MaxConcurrentRemediations int           `json:"maxConcurrentRemediations" yaml:"maxConcurrentRemediations"`
	DefaultLeaseDuration      time.Duration `json:"defaultLeaseDuration" yaml:"defaultLeaseDuration"`
	CooldownPeriod            time.Duration `json:"cooldownPeriod" yaml:"cooldownPeriod"`
}

// PrometheusConfig contains Prometheus metrics configuration
type PrometheusConfig struct {
	Enabled bool   `json:"enabled" yaml:"enabled"`
	Port    int    `json:"port" yaml:"port"`
	Path    string `json:"path" yaml:"path"`
}

// KubernetesConfig contains Kubernetes integration settings
type KubernetesConfig struct {
	Enabled      bool   `json:"enabled" yaml:"enabled"`
	Kubeconfig   string `json:"kubeconfig" yaml:"kubeconfig"`
	InCluster    bool   `json:"inCluster" yaml:"inCluster"`
	Namespace    string `json:"namespace" yaml:"namespace"`
	CreateEvents bool   `json:"createEvents" yaml:"createEvents"`
}

// DefaultControllerConfig returns a configuration with sensible defaults
func DefaultControllerConfig() *ControllerConfig {
	return &ControllerConfig{
		Server: ServerConfig{
			BindAddress:  "0.0.0.0",
			Port:         8080,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
			EnableCORS:   false,
		},
		Storage: StorageConfig{
			Path:      "/data/node-doctor.db",
			Retention: 30 * 24 * time.Hour, // 30 days
		},
		Correlation: CorrelationConfig{
			Enabled:                true,
			ClusterWideThreshold:   0.3, // 30% of nodes
			EvaluationInterval:     30 * time.Second,
			MinNodesForCorrelation: 2,
		},
		Coordination: CoordinationConfig{
			Enabled:                   true,
			MaxConcurrentRemediations: 3,
			DefaultLeaseDuration:      5 * time.Minute,
			CooldownPeriod:            10 * time.Minute,
		},
		Prometheus: PrometheusConfig{
			Enabled: true,
			Port:    9090,
			Path:    "/metrics",
		},
		Kubernetes: KubernetesConfig{
			Enabled:      true,
			InCluster:    true,
			Namespace:    "node-doctor",
			CreateEvents: true,
		},
	}
}
