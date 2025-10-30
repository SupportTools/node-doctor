// Package types defines configuration types for Node Doctor.
package types

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

// Package-level defaults
const (
	DefaultLogLevel                 = "info"
	DefaultLogFormat                = "json"
	DefaultLogOutput                = "stdout"
	DefaultUpdateInterval           = "10s"
	DefaultResyncInterval           = "60s"
	DefaultHeartbeatInterval        = "5m"
	DefaultQPS                      = 50
	DefaultBurst                    = 100
	DefaultHTTPPort                 = 8080
	DefaultHTTPBindAddress          = "0.0.0.0"
	DefaultPrometheusPort           = 9100
	DefaultPrometheusPath           = "/metrics"
	DefaultMonitorInterval          = "30s"
	DefaultMonitorTimeout           = "10s"
	DefaultCooldownPeriod           = "5m"
	DefaultMaxAttemptsGlobal        = 3
	DefaultMaxRemediationsPerHour   = 10
	DefaultMaxRemediationsPerMinute = 2
	DefaultCircuitBreakerThreshold  = 5
	DefaultCircuitBreakerTimeout    = "30m"
	DefaultHistorySize              = 100
	MaxRecursionDepth               = 10  // Maximum nesting depth for strategies
	MaxQPS                          = 10000
	MaxBurst                        = 100000
)

// Package-level variables for validation
var (
	// Prometheus namespace validation regex
	prometheusNamespaceRegex = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)

	// Valid log levels
	validLogLevels = map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
		"fatal": true,
	}

	// Valid log formats
	validLogFormats = map[string]bool{
		"json": true,
		"text": true,
	}

	// Valid log outputs
	validLogOutputs = map[string]bool{
		"stdout": true,
		"stderr": true,
		"file":   true,
	}

	// Valid remediation strategies
	validRemediationStrategies = map[string]bool{
		"systemd-restart": true,
		"custom-script":   true,
		"node-reboot":     true,
		"pod-delete":      true,
	}
)

// NodeDoctorConfig is the top-level configuration structure.
type NodeDoctorConfig struct {
	// APIVersion of the configuration schema
	APIVersion string `json:"apiVersion" yaml:"apiVersion"`

	// Kind of resource (always "NodeDoctorConfig")
	Kind string `json:"kind" yaml:"kind"`

	// Metadata contains name, namespace, labels, etc.
	Metadata ConfigMetadata `json:"metadata" yaml:"metadata"`

	// Settings contains global configuration
	Settings GlobalSettings `json:"settings" yaml:"settings"`

	// Monitors contains all monitor configurations
	Monitors []MonitorConfig `json:"monitors" yaml:"monitors"`

	// Exporters contains exporter configurations
	Exporters ExporterConfigs `json:"exporters" yaml:"exporters"`

	// Remediation contains global remediation settings
	Remediation RemediationConfig `json:"remediation" yaml:"remediation"`

	// Features contains feature flags
	Features FeatureFlags `json:"features,omitempty" yaml:"features,omitempty"`
}

// ConfigMetadata contains metadata about the configuration.
type ConfigMetadata struct {
	Name      string            `json:"name" yaml:"name"`
	Namespace string            `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Labels    map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

// GlobalSettings contains global configuration settings.
type GlobalSettings struct {
	// NodeName is the Kubernetes node name (usually from ${NODE_NAME})
	NodeName string `json:"nodeName" yaml:"nodeName"`

	// Logging configuration
	LogLevel  string `json:"logLevel,omitempty" yaml:"logLevel,omitempty"`
	LogFormat string `json:"logFormat,omitempty" yaml:"logFormat,omitempty"`
	LogOutput string `json:"logOutput,omitempty" yaml:"logOutput,omitempty"`
	LogFile   string `json:"logFile,omitempty" yaml:"logFile,omitempty"`

	// Update intervals (stored as strings, parsed to time.Duration)
	UpdateIntervalString    string `json:"updateInterval,omitempty" yaml:"updateInterval,omitempty"`
	ResyncIntervalString    string `json:"resyncInterval,omitempty" yaml:"resyncInterval,omitempty"`
	HeartbeatIntervalString string `json:"heartbeatInterval,omitempty" yaml:"heartbeatInterval,omitempty"`

	// Parsed duration fields (not in JSON/YAML)
	UpdateInterval    time.Duration `json:"-" yaml:"-"`
	ResyncInterval    time.Duration `json:"-" yaml:"-"`
	HeartbeatInterval time.Duration `json:"-" yaml:"-"`

	// Remediation master switches
	EnableRemediation bool `json:"enableRemediation,omitempty" yaml:"enableRemediation,omitempty"`
	DryRunMode        bool `json:"dryRunMode,omitempty" yaml:"dryRunMode,omitempty"`

	// Kubernetes client configuration
	Kubeconfig string  `json:"kubeconfig,omitempty" yaml:"kubeconfig,omitempty"`
	QPS        float32 `json:"qps,omitempty" yaml:"qps,omitempty"`
	Burst      int     `json:"burst,omitempty" yaml:"burst,omitempty"`
}

// MonitorConfig represents a single monitor configuration.
type MonitorConfig struct {
	// Name is the unique identifier for this monitor
	Name string `json:"name" yaml:"name"`

	// Type is the monitor type (e.g., "system-disk-check")
	Type string `json:"type" yaml:"type"`

	// Enabled indicates whether this monitor is active
	Enabled bool `json:"enabled" yaml:"enabled"`

	// Interval and timeout (stored as strings)
	IntervalString string `json:"interval,omitempty" yaml:"interval,omitempty"`
	TimeoutString  string `json:"timeout,omitempty" yaml:"timeout,omitempty"`

	// Parsed duration fields
	Interval time.Duration `json:"-" yaml:"-"`
	Timeout  time.Duration `json:"-" yaml:"-"`

	// Config contains monitor-specific configuration as a map
	// Each monitor type will parse this according to its needs
	Config map[string]interface{} `json:"config,omitempty" yaml:"config,omitempty"`

	// Remediation contains optional remediation configuration for this monitor
	Remediation *MonitorRemediationConfig `json:"remediation,omitempty" yaml:"remediation,omitempty"`
}

// MonitorRemediationConfig contains remediation settings for a monitor.
type MonitorRemediationConfig struct {
	// Enabled indicates whether remediation is enabled for this monitor
	Enabled bool `json:"enabled" yaml:"enabled"`

	// Strategy is the remediation strategy type
	Strategy string `json:"strategy,omitempty" yaml:"strategy,omitempty"`

	// Action is the specific action to take
	Action string `json:"action,omitempty" yaml:"action,omitempty"`

	// Service is the systemd service name (for systemd-restart strategy)
	Service string `json:"service,omitempty" yaml:"service,omitempty"`

	// ScriptPath is the path to remediation script (for custom-script strategy)
	ScriptPath string `json:"scriptPath,omitempty" yaml:"scriptPath,omitempty"`

	// Args are arguments to pass to the script
	Args []string `json:"args,omitempty" yaml:"args,omitempty"`

	// Cooldown period (stored as string)
	CooldownString string        `json:"cooldown,omitempty" yaml:"cooldown,omitempty"`
	Cooldown       time.Duration `json:"-" yaml:"-"`

	// MaxAttempts is the maximum remediation attempts
	MaxAttempts int `json:"maxAttempts,omitempty" yaml:"maxAttempts,omitempty"`

	// Priority for multiple remediation strategies
	Priority int `json:"priority,omitempty" yaml:"priority,omitempty"`

	// GracefulStop indicates whether to stop gracefully
	GracefulStop bool `json:"gracefulStop,omitempty" yaml:"gracefulStop,omitempty"`

	// WaitTimeout for graceful stop (stored as string)
	WaitTimeoutString string        `json:"waitTimeout,omitempty" yaml:"waitTimeout,omitempty"`
	WaitTimeout       time.Duration `json:"-" yaml:"-"`

	// Additional strategies for multi-step remediation
	Strategies []MonitorRemediationConfig `json:"strategies,omitempty" yaml:"strategies,omitempty"`
}

// ExporterConfigs contains all exporter configurations.
type ExporterConfigs struct {
	Kubernetes *KubernetesExporterConfig `json:"kubernetes,omitempty" yaml:"kubernetes,omitempty"`
	HTTP       *HTTPExporterConfig       `json:"http,omitempty" yaml:"http,omitempty"`
	Prometheus *PrometheusExporterConfig `json:"prometheus,omitempty" yaml:"prometheus,omitempty"`
}

// KubernetesExporterConfig configures the Kubernetes exporter.
type KubernetesExporterConfig struct {
	Enabled bool `json:"enabled" yaml:"enabled"`

	// Update intervals
	UpdateIntervalString    string `json:"updateInterval,omitempty" yaml:"updateInterval,omitempty"`
	ResyncIntervalString    string `json:"resyncInterval,omitempty" yaml:"resyncInterval,omitempty"`
	HeartbeatIntervalString string `json:"heartbeatInterval,omitempty" yaml:"heartbeatInterval,omitempty"`

	UpdateInterval    time.Duration `json:"-" yaml:"-"`
	ResyncInterval    time.Duration `json:"-" yaml:"-"`
	HeartbeatInterval time.Duration `json:"-" yaml:"-"`

	// Namespace for events
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`

	// Custom node conditions
	Conditions []ConditionConfig `json:"conditions,omitempty" yaml:"conditions,omitempty"`

	// Node annotations to manage
	Annotations []AnnotationConfig `json:"annotations,omitempty" yaml:"annotations,omitempty"`

	// Event configuration
	Events EventConfig `json:"events,omitempty" yaml:"events,omitempty"`
}

// ConditionConfig defines a custom node condition.
type ConditionConfig struct {
	Type           string `json:"type" yaml:"type"`
	DefaultStatus  string `json:"defaultStatus,omitempty" yaml:"defaultStatus,omitempty"`
	DefaultReason  string `json:"defaultReason,omitempty" yaml:"defaultReason,omitempty"`
	DefaultMessage string `json:"defaultMessage,omitempty" yaml:"defaultMessage,omitempty"`
}

// AnnotationConfig defines a node annotation to manage.
type AnnotationConfig struct {
	Key   string `json:"key" yaml:"key"`
	Value string `json:"value" yaml:"value"`
}

// EventConfig configures Kubernetes event behavior.
type EventConfig struct {
	MaxEventsPerMinute        int           `json:"maxEventsPerMinute,omitempty" yaml:"maxEventsPerMinute,omitempty"`
	EventTTLString            string        `json:"eventTTL,omitempty" yaml:"eventTTL,omitempty"`
	EventTTL                  time.Duration `json:"-" yaml:"-"`
	DeduplicationWindowString string        `json:"deduplicationWindow,omitempty" yaml:"deduplicationWindow,omitempty"`
	DeduplicationWindow       time.Duration `json:"-" yaml:"-"`
}

// HTTPExporterConfig configures the HTTP exporter.
type HTTPExporterConfig struct {
	Enabled     bool   `json:"enabled" yaml:"enabled"`
	BindAddress string `json:"bindAddress,omitempty" yaml:"bindAddress,omitempty"`
	HostPort    int    `json:"hostPort,omitempty" yaml:"hostPort,omitempty"`

	// TLS configuration
	TLSEnabled  bool   `json:"tlsEnabled,omitempty" yaml:"tlsEnabled,omitempty"`
	TLSCertFile string `json:"tlsCertFile,omitempty" yaml:"tlsCertFile,omitempty"`
	TLSKeyFile  string `json:"tlsKeyFile,omitempty" yaml:"tlsKeyFile,omitempty"`

	// Endpoints to expose
	Endpoints []HTTPEndpointConfig `json:"endpoints,omitempty" yaml:"endpoints,omitempty"`
}

// HTTPEndpointConfig defines an HTTP endpoint.
type HTTPEndpointConfig struct {
	Path        string `json:"path" yaml:"path"`
	Handler     string `json:"handler" yaml:"handler"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// PrometheusExporterConfig configures the Prometheus exporter.
type PrometheusExporterConfig struct {
	Enabled   bool              `json:"enabled" yaml:"enabled"`
	Port      int               `json:"port,omitempty" yaml:"port,omitempty"`
	Path      string            `json:"path,omitempty" yaml:"path,omitempty"`
	Namespace string            `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Subsystem string            `json:"subsystem,omitempty" yaml:"subsystem,omitempty"`
	Labels    map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

// RemediationConfig contains global remediation settings.
type RemediationConfig struct {
	// Master switches
	Enabled bool `json:"enabled" yaml:"enabled"`
	DryRun  bool `json:"dryRun,omitempty" yaml:"dryRun,omitempty"`

	// Safety limits
	MaxRemediationsPerHour   int `json:"maxRemediationsPerHour,omitempty" yaml:"maxRemediationsPerHour,omitempty"`
	MaxRemediationsPerMinute int `json:"maxRemediationsPerMinute,omitempty" yaml:"maxRemediationsPerMinute,omitempty"`

	// Cooldown configuration
	CooldownPeriodString string        `json:"cooldownPeriod,omitempty" yaml:"cooldownPeriod,omitempty"`
	CooldownPeriod       time.Duration `json:"-" yaml:"-"`

	// Global max attempts
	MaxAttemptsGlobal int `json:"maxAttemptsGlobal,omitempty" yaml:"maxAttemptsGlobal,omitempty"`

	// Circuit breaker settings
	CircuitBreaker CircuitBreakerConfig `json:"circuitBreaker,omitempty" yaml:"circuitBreaker,omitempty"`

	// History configuration
	HistorySize int `json:"historySize,omitempty" yaml:"historySize,omitempty"`

	// Problem-specific overrides
	Overrides []RemediationOverride `json:"overrides,omitempty" yaml:"overrides,omitempty"`
}

// CircuitBreakerConfig configures circuit breaker behavior.
type CircuitBreakerConfig struct {
	Enabled          bool          `json:"enabled" yaml:"enabled"`
	Threshold        int           `json:"threshold,omitempty" yaml:"threshold,omitempty"`
	TimeoutString    string        `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Timeout          time.Duration `json:"-" yaml:"-"`
	SuccessThreshold int           `json:"successThreshold,omitempty" yaml:"successThreshold,omitempty"`
}

// RemediationOverride allows problem-specific remediation overrides.
type RemediationOverride struct {
	Problem                 string        `json:"problem" yaml:"problem"`
	CooldownString          string        `json:"cooldown,omitempty" yaml:"cooldown,omitempty"`
	Cooldown                time.Duration `json:"-" yaml:"-"`
	MaxAttempts             int           `json:"maxAttempts,omitempty" yaml:"maxAttempts,omitempty"`
	CircuitBreakerThreshold int           `json:"circuitBreakerThreshold,omitempty" yaml:"circuitBreakerThreshold,omitempty"`
}

// FeatureFlags contains experimental feature flags.
type FeatureFlags struct {
	EnableMetrics   bool   `json:"enableMetrics,omitempty" yaml:"enableMetrics,omitempty"`
	EnableProfiling bool   `json:"enableProfiling,omitempty" yaml:"enableProfiling,omitempty"`
	ProfilingPort   int    `json:"profilingPort,omitempty" yaml:"profilingPort,omitempty"`
	EnableTracing   bool   `json:"enableTracing,omitempty" yaml:"enableTracing,omitempty"`
	TracingEndpoint string `json:"tracingEndpoint,omitempty" yaml:"tracingEndpoint,omitempty"`
}

// ApplyDefaults applies default values to the configuration.
func (c *NodeDoctorConfig) ApplyDefaults() error {
	// Apply defaults to global settings
	if err := c.Settings.ApplyDefaults(); err != nil {
		return fmt.Errorf("failed to apply defaults to settings: %w", err)
	}

	// Apply defaults to monitors
	for i := range c.Monitors {
		if err := c.Monitors[i].ApplyDefaults(); err != nil {
			return fmt.Errorf("failed to apply defaults to monitor %s: %w", c.Monitors[i].Name, err)
		}
	}

	// Apply defaults to exporters
	if c.Exporters.Kubernetes != nil {
		if err := c.Exporters.Kubernetes.ApplyDefaults(); err != nil {
			return fmt.Errorf("failed to apply defaults to kubernetes exporter: %w", err)
		}
	}
	if c.Exporters.HTTP != nil {
		if err := c.Exporters.HTTP.ApplyDefaults(); err != nil {
			return fmt.Errorf("failed to apply defaults to http exporter: %w", err)
		}
	}
	if c.Exporters.Prometheus != nil {
		if err := c.Exporters.Prometheus.ApplyDefaults(); err != nil {
			return fmt.Errorf("failed to apply defaults to prometheus exporter: %w", err)
		}
	}

	// Apply defaults to remediation
	if err := c.Remediation.ApplyDefaults(); err != nil {
		return fmt.Errorf("failed to apply defaults to remediation: %w", err)
	}

	// Apply defaults to features
	c.Features.ApplyDefaults()

	return nil
}

// ApplyDefaults applies default values to GlobalSettings.
func (s *GlobalSettings) ApplyDefaults() error {
	if s.LogLevel == "" {
		s.LogLevel = DefaultLogLevel
	}
	if s.LogFormat == "" {
		s.LogFormat = DefaultLogFormat
	}
	if s.LogOutput == "" {
		s.LogOutput = DefaultLogOutput
	}

	if s.UpdateIntervalString == "" {
		s.UpdateIntervalString = DefaultUpdateInterval
	}
	if s.ResyncIntervalString == "" {
		s.ResyncIntervalString = DefaultResyncInterval
	}
	if s.HeartbeatIntervalString == "" {
		s.HeartbeatIntervalString = DefaultHeartbeatInterval
	}

	// Parse durations
	var err error
	s.UpdateInterval, err = time.ParseDuration(s.UpdateIntervalString)
	if err != nil {
		return fmt.Errorf("invalid updateInterval %q: %w", s.UpdateIntervalString, err)
	}
	s.ResyncInterval, err = time.ParseDuration(s.ResyncIntervalString)
	if err != nil {
		return fmt.Errorf("invalid resyncInterval %q: %w", s.ResyncIntervalString, err)
	}
	s.HeartbeatInterval, err = time.ParseDuration(s.HeartbeatIntervalString)
	if err != nil {
		return fmt.Errorf("invalid heartbeatInterval %q: %w", s.HeartbeatIntervalString, err)
	}

	if s.QPS == 0 {
		s.QPS = DefaultQPS
	}
	if s.Burst == 0 {
		s.Burst = DefaultBurst
	}

	return nil
}

// ApplyDefaults applies default values to MonitorConfig.
func (m *MonitorConfig) ApplyDefaults() error {
	if m.IntervalString == "" {
		m.IntervalString = DefaultMonitorInterval
	}
	if m.TimeoutString == "" {
		m.TimeoutString = DefaultMonitorTimeout
	}

	// Parse durations
	var err error
	m.Interval, err = time.ParseDuration(m.IntervalString)
	if err != nil {
		return fmt.Errorf("invalid interval %q: %w", m.IntervalString, err)
	}
	m.Timeout, err = time.ParseDuration(m.TimeoutString)
	if err != nil {
		return fmt.Errorf("invalid timeout %q: %w", m.TimeoutString, err)
	}

	// Apply defaults to remediation
	if m.Remediation != nil {
		if err := m.Remediation.ApplyDefaults(); err != nil {
			return fmt.Errorf("failed to apply defaults to remediation: %w", err)
		}
	}

	return nil
}

// ApplyDefaults applies default values to MonitorRemediationConfig.
func (r *MonitorRemediationConfig) ApplyDefaults() error {
	if r.CooldownString == "" {
		r.CooldownString = DefaultCooldownPeriod
	}
	if r.MaxAttempts == 0 {
		r.MaxAttempts = DefaultMaxAttemptsGlobal
	}

	// Parse durations
	var err error
	r.Cooldown, err = time.ParseDuration(r.CooldownString)
	if err != nil {
		return fmt.Errorf("invalid cooldown %q: %w", r.CooldownString, err)
	}

	if r.WaitTimeoutString != "" {
		r.WaitTimeout, err = time.ParseDuration(r.WaitTimeoutString)
		if err != nil {
			return fmt.Errorf("invalid waitTimeout %q: %w", r.WaitTimeoutString, err)
		}
	}

	// Apply defaults to nested strategies with recursion protection
	if err := r.applyDefaultsWithDepth(0); err != nil {
		return err
	}

	return nil
}

// applyDefaultsWithDepth applies defaults to nested strategies with recursion depth protection.
func (r *MonitorRemediationConfig) applyDefaultsWithDepth(depth int) error {
	if depth > MaxRecursionDepth {
		return fmt.Errorf("maximum recursion depth (%d) exceeded in nested strategies", MaxRecursionDepth)
	}

	for i := range r.Strategies {
		if err := r.Strategies[i].ApplyDefaults(); err != nil {
			return fmt.Errorf("failed to apply defaults to strategy %d: %w", i, err)
		}
		if err := r.Strategies[i].applyDefaultsWithDepth(depth + 1); err != nil {
			return err
		}
	}
	return nil
}

// validateWithDepth validates nested strategies with recursion depth protection.
func (r *MonitorRemediationConfig) validateWithDepth(depth int) error {
	if depth > MaxRecursionDepth {
		return fmt.Errorf("maximum recursion depth (%d) exceeded in nested strategies", MaxRecursionDepth)
	}

	for i, strategy := range r.Strategies {
		if err := strategy.Validate(); err != nil {
			return fmt.Errorf("strategy %d validation failed: %w", i, err)
		}
		if err := strategy.validateWithDepth(depth + 1); err != nil {
			return err
		}
	}
	return nil
}

// ApplyDefaults applies default values to KubernetesExporterConfig.
func (k *KubernetesExporterConfig) ApplyDefaults() error {
	if k.UpdateIntervalString == "" {
		k.UpdateIntervalString = DefaultUpdateInterval
	}
	if k.ResyncIntervalString == "" {
		k.ResyncIntervalString = DefaultResyncInterval
	}
	if k.HeartbeatIntervalString == "" {
		k.HeartbeatIntervalString = DefaultHeartbeatInterval
	}

	// Parse durations
	var err error
	k.UpdateInterval, err = time.ParseDuration(k.UpdateIntervalString)
	if err != nil {
		return fmt.Errorf("invalid updateInterval %q: %w", k.UpdateIntervalString, err)
	}
	k.ResyncInterval, err = time.ParseDuration(k.ResyncIntervalString)
	if err != nil {
		return fmt.Errorf("invalid resyncInterval %q: %w", k.ResyncIntervalString, err)
	}
	k.HeartbeatInterval, err = time.ParseDuration(k.HeartbeatIntervalString)
	if err != nil {
		return fmt.Errorf("invalid heartbeatInterval %q: %w", k.HeartbeatIntervalString, err)
	}

	// Event defaults
	if k.Events.EventTTLString != "" {
		k.Events.EventTTL, err = time.ParseDuration(k.Events.EventTTLString)
		if err != nil {
			return fmt.Errorf("invalid eventTTL %q: %w", k.Events.EventTTLString, err)
		}
	}
	if k.Events.DeduplicationWindowString != "" {
		k.Events.DeduplicationWindow, err = time.ParseDuration(k.Events.DeduplicationWindowString)
		if err != nil {
			return fmt.Errorf("invalid deduplicationWindow %q: %w", k.Events.DeduplicationWindowString, err)
		}
	}

	return nil
}

// ApplyDefaults applies default values to HTTPExporterConfig.
func (h *HTTPExporterConfig) ApplyDefaults() error {
	if h.BindAddress == "" {
		h.BindAddress = DefaultHTTPBindAddress
	}
	if h.HostPort == 0 {
		h.HostPort = DefaultHTTPPort
	}
	return nil
}

// ApplyDefaults applies default values to PrometheusExporterConfig.
func (p *PrometheusExporterConfig) ApplyDefaults() error {
	if p.Port == 0 {
		p.Port = DefaultPrometheusPort
	}
	if p.Path == "" {
		p.Path = DefaultPrometheusPath
	}
	if p.Namespace == "" {
		p.Namespace = "node_doctor"
	}
	return nil
}

// ApplyDefaults applies default values to RemediationConfig.
func (r *RemediationConfig) ApplyDefaults() error {
	if r.MaxRemediationsPerHour == 0 {
		r.MaxRemediationsPerHour = DefaultMaxRemediationsPerHour
	}
	if r.MaxRemediationsPerMinute == 0 {
		r.MaxRemediationsPerMinute = DefaultMaxRemediationsPerMinute
	}
	if r.CooldownPeriodString == "" {
		r.CooldownPeriodString = DefaultCooldownPeriod
	}
	if r.MaxAttemptsGlobal == 0 {
		r.MaxAttemptsGlobal = DefaultMaxAttemptsGlobal
	}
	if r.HistorySize == 0 {
		r.HistorySize = DefaultHistorySize
	}

	// Parse duration
	var err error
	r.CooldownPeriod, err = time.ParseDuration(r.CooldownPeriodString)
	if err != nil {
		return fmt.Errorf("invalid cooldownPeriod %q: %w", r.CooldownPeriodString, err)
	}

	// Circuit breaker defaults
	if r.CircuitBreaker.Enabled {
		if r.CircuitBreaker.Threshold == 0 {
			r.CircuitBreaker.Threshold = DefaultCircuitBreakerThreshold
		}
		if r.CircuitBreaker.TimeoutString == "" {
			r.CircuitBreaker.TimeoutString = DefaultCircuitBreakerTimeout
		}
		r.CircuitBreaker.Timeout, err = time.ParseDuration(r.CircuitBreaker.TimeoutString)
		if err != nil {
			return fmt.Errorf("invalid circuit breaker timeout %q: %w", r.CircuitBreaker.TimeoutString, err)
		}
		if r.CircuitBreaker.SuccessThreshold == 0 {
			r.CircuitBreaker.SuccessThreshold = 2
		}
	}

	// Apply defaults to overrides
	for i := range r.Overrides {
		if r.Overrides[i].CooldownString != "" {
			r.Overrides[i].Cooldown, err = time.ParseDuration(r.Overrides[i].CooldownString)
			if err != nil {
				return fmt.Errorf("invalid cooldown in override for %s: %w", r.Overrides[i].Problem, err)
			}
		}
	}

	return nil
}

// ApplyDefaults applies default values to FeatureFlags.
func (f *FeatureFlags) ApplyDefaults() {
	// EnableMetrics defaults to true
	if !f.EnableMetrics {
		f.EnableMetrics = true
	}
}

// ===============================================
// PHASE 3: VALIDATION METHODS
// ===============================================

// Validate validates the entire configuration.
func (c *NodeDoctorConfig) Validate() error {
	// Validate API version and kind
	if c.APIVersion == "" {
		return fmt.Errorf("apiVersion is required")
	}
	if c.Kind == "" {
		return fmt.Errorf("kind is required")
	}
	if c.Kind != "NodeDoctorConfig" {
		return fmt.Errorf("kind must be 'NodeDoctorConfig', got %q", c.Kind)
	}

	// Validate metadata
	if c.Metadata.Name == "" {
		return fmt.Errorf("metadata.name is required")
	}

	// Validate settings
	if err := c.Settings.Validate(); err != nil {
		return fmt.Errorf("settings validation failed: %w", err)
	}

	// Validate monitors for duplicates and individual validation
	monitorNames := make(map[string]bool)
	for i, monitor := range c.Monitors {
		if monitor.Name == "" {
			return fmt.Errorf("monitor %d: name is required", i)
		}
		if monitorNames[monitor.Name] {
			return fmt.Errorf("duplicate monitor name %q found", monitor.Name)
		}
		monitorNames[monitor.Name] = true

		if err := monitor.Validate(); err != nil {
			return fmt.Errorf("monitor %q validation failed: %w", monitor.Name, err)
		}
	}

	// Validate exporters
	if c.Exporters.Kubernetes != nil {
		if err := c.Exporters.Kubernetes.Validate(); err != nil {
			return fmt.Errorf("kubernetes exporter validation failed: %w", err)
		}
	}
	if c.Exporters.HTTP != nil {
		if err := c.Exporters.HTTP.Validate(); err != nil {
			return fmt.Errorf("http exporter validation failed: %w", err)
		}
	}
	if c.Exporters.Prometheus != nil {
		if err := c.Exporters.Prometheus.Validate(); err != nil {
			return fmt.Errorf("prometheus exporter validation failed: %w", err)
		}
	}

	// Validate remediation
	if err := c.Remediation.Validate(); err != nil {
		return fmt.Errorf("remediation validation failed: %w", err)
	}

	return nil
}

// Validate validates the GlobalSettings configuration.
func (s *GlobalSettings) Validate() error {
	// Validate node name
	if s.NodeName == "" {
		return fmt.Errorf("nodeName is required")
	}

	// Validate log level
	if !validLogLevels[s.LogLevel] {
		return fmt.Errorf("invalid logLevel %q, must be one of: debug, info, warn, error, fatal", s.LogLevel)
	}

	// Validate log format
	if !validLogFormats[s.LogFormat] {
		return fmt.Errorf("invalid logFormat %q, must be one of: json, text", s.LogFormat)
	}

	// Validate log output
	if !validLogOutputs[s.LogOutput] {
		return fmt.Errorf("invalid logOutput %q, must be one of: stdout, stderr, file", s.LogOutput)
	}

	// Validate log file if output is file
	if s.LogOutput == "file" && s.LogFile == "" {
		return fmt.Errorf("logFile is required when logOutput is 'file'")
	}

	// Validate intervals are positive
	if s.UpdateInterval <= 0 {
		return fmt.Errorf("updateInterval must be positive, got %v", s.UpdateInterval)
	}
	if s.ResyncInterval <= 0 {
		return fmt.Errorf("resyncInterval must be positive, got %v", s.ResyncInterval)
	}
	if s.HeartbeatInterval <= 0 {
		return fmt.Errorf("heartbeatInterval must be positive, got %v", s.HeartbeatInterval)
	}

	// Validate QPS and burst with bounds checking
	if s.QPS <= 0 {
		return fmt.Errorf("qps must be positive, got %f", s.QPS)
	}
	if s.QPS > MaxQPS {
		return fmt.Errorf("qps exceeds maximum allowed value %d, got %f", MaxQPS, s.QPS)
	}
	if s.Burst <= 0 {
		return fmt.Errorf("burst must be positive, got %d", s.Burst)
	}
	if s.Burst > MaxBurst {
		return fmt.Errorf("burst exceeds maximum allowed value %d, got %d", MaxBurst, s.Burst)
	}

	// Note: Kubeconfig file existence check skipped to support containerized
	// deployments where config may be mounted at runtime

	return nil
}

// Validate validates the MonitorConfig configuration.
func (m *MonitorConfig) Validate() error {
	// Validate required fields
	if m.Name == "" {
		return fmt.Errorf("name is required")
	}
	if m.Type == "" {
		return fmt.Errorf("type is required")
	}

	// Validate intervals are positive
	if m.Interval <= 0 {
		return fmt.Errorf("interval must be positive, got %v", m.Interval)
	}
	if m.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive, got %v", m.Timeout)
	}

	// Validate timeout < interval
	if m.Timeout >= m.Interval {
		return fmt.Errorf("timeout (%v) must be less than interval (%v)", m.Timeout, m.Interval)
	}

	// Validate remediation if present
	if m.Remediation != nil {
		if err := m.Remediation.Validate(); err != nil {
			return fmt.Errorf("remediation validation failed: %w", err)
		}
	}

	return nil
}

// Validate validates the MonitorRemediationConfig configuration.
func (r *MonitorRemediationConfig) Validate() error {
	if !r.Enabled {
		return nil // No validation needed if disabled
	}

	// Validate strategy
	if r.Strategy == "" {
		return fmt.Errorf("strategy is required when remediation is enabled")
	}
	if !validRemediationStrategies[r.Strategy] {
		return fmt.Errorf("invalid strategy %q, must be one of: systemd-restart, custom-script, node-reboot, pod-delete", r.Strategy)
	}

	// Strategy-specific validation
	switch r.Strategy {
	case "systemd-restart":
		if r.Service == "" {
			return fmt.Errorf("service is required for systemd-restart strategy")
		}
	case "custom-script":
		if r.ScriptPath == "" {
			return fmt.Errorf("scriptPath is required for custom-script strategy")
		}
		// Basic path security validation
		if strings.Contains(r.ScriptPath, "..") {
			return fmt.Errorf("scriptPath cannot contain '..' path traversal: %q", r.ScriptPath)
		}
		if !strings.HasPrefix(r.ScriptPath, "/") {
			return fmt.Errorf("scriptPath must be an absolute path, got: %q", r.ScriptPath)
		}
		// Note: File existence check skipped to support containerized deployments
		// where scripts may be mounted at runtime
	}

	// Validate cooldown is positive
	if r.Cooldown <= 0 {
		return fmt.Errorf("cooldown must be positive, got %v", r.Cooldown)
	}

	// Validate max attempts
	if r.MaxAttempts <= 0 {
		return fmt.Errorf("maxAttempts must be positive, got %d", r.MaxAttempts)
	}

	// Validate wait timeout if specified
	if r.WaitTimeoutString != "" && r.WaitTimeout <= 0 {
		return fmt.Errorf("waitTimeout must be positive when specified, got %v", r.WaitTimeout)
	}

	// Validate nested strategies with recursion protection
	if err := r.validateWithDepth(0); err != nil {
		return err
	}

	return nil
}

// Validate validates the KubernetesExporterConfig configuration.
func (k *KubernetesExporterConfig) Validate() error {
	if !k.Enabled {
		return nil // No validation needed if disabled
	}

	// Validate intervals are positive
	if k.UpdateInterval <= 0 {
		return fmt.Errorf("updateInterval must be positive, got %v", k.UpdateInterval)
	}
	if k.ResyncInterval <= 0 {
		return fmt.Errorf("resyncInterval must be positive, got %v", k.ResyncInterval)
	}
	if k.HeartbeatInterval <= 0 {
		return fmt.Errorf("heartbeatInterval must be positive, got %v", k.HeartbeatInterval)
	}

	// Validate conditions
	for i, condition := range k.Conditions {
		if condition.Type == "" {
			return fmt.Errorf("condition %d: type is required", i)
		}
	}

	// Validate annotations
	for i, annotation := range k.Annotations {
		if annotation.Key == "" {
			return fmt.Errorf("annotation %d: key is required", i)
		}
	}

	// Validate event configuration
	if k.Events.MaxEventsPerMinute < 0 {
		return fmt.Errorf("events.maxEventsPerMinute must be non-negative, got %d", k.Events.MaxEventsPerMinute)
	}
	if k.Events.EventTTLString != "" && k.Events.EventTTL <= 0 {
		return fmt.Errorf("events.eventTTL must be positive when specified, got %v", k.Events.EventTTL)
	}
	if k.Events.DeduplicationWindowString != "" && k.Events.DeduplicationWindow <= 0 {
		return fmt.Errorf("events.deduplicationWindow must be positive when specified, got %v", k.Events.DeduplicationWindow)
	}

	return nil
}

// Validate validates the HTTPExporterConfig configuration.
func (h *HTTPExporterConfig) Validate() error {
	if !h.Enabled {
		return nil // No validation needed if disabled
	}

	// Validate port range
	if h.HostPort <= 0 || h.HostPort > 65535 {
		return fmt.Errorf("hostPort must be in range 1-65535, got %d", h.HostPort)
	}

	// Validate TLS configuration
	if h.TLSEnabled {
		if h.TLSCertFile == "" {
			return fmt.Errorf("tlsCertFile is required when TLS is enabled")
		}
		if h.TLSKeyFile == "" {
			return fmt.Errorf("tlsKeyFile is required when TLS is enabled")
		}

		// Note: TLS file existence checks skipped to support containerized deployments
		// where certificates may be mounted at runtime via secrets
	}

	// Validate endpoints
	paths := make(map[string]bool)
	for i, endpoint := range h.Endpoints {
		if endpoint.Path == "" {
			return fmt.Errorf("endpoint %d: path is required", i)
		}
		if endpoint.Handler == "" {
			return fmt.Errorf("endpoint %d: handler is required", i)
		}
		if !strings.HasPrefix(endpoint.Path, "/") {
			return fmt.Errorf("endpoint %d: path must start with '/', got %q", i, endpoint.Path)
		}
		if paths[endpoint.Path] {
			return fmt.Errorf("duplicate endpoint path %q found", endpoint.Path)
		}
		paths[endpoint.Path] = true
	}

	return nil
}

// Validate validates the PrometheusExporterConfig configuration.
func (p *PrometheusExporterConfig) Validate() error {
	if !p.Enabled {
		return nil // No validation needed if disabled
	}

	// Validate port range
	if p.Port <= 0 || p.Port > 65535 {
		return fmt.Errorf("port must be in range 1-65535, got %d", p.Port)
	}

	// Validate path starts with /
	if !strings.HasPrefix(p.Path, "/") {
		return fmt.Errorf("path must start with '/', got %q", p.Path)
	}

	// Validate Prometheus namespace format
	if p.Namespace != "" && !prometheusNamespaceRegex.MatchString(p.Namespace) {
		return fmt.Errorf("namespace %q is invalid, must match pattern ^[a-zA-Z_:][a-zA-Z0-9_:]*$", p.Namespace)
	}

	// Validate subsystem format (same as namespace)
	if p.Subsystem != "" && !prometheusNamespaceRegex.MatchString(p.Subsystem) {
		return fmt.Errorf("subsystem %q is invalid, must match pattern ^[a-zA-Z_:][a-zA-Z0-9_:]*$", p.Subsystem)
	}

	return nil
}

// Validate validates the RemediationConfig configuration.
func (r *RemediationConfig) Validate() error {
	// Validate safety limits
	if r.MaxRemediationsPerHour < 0 {
		return fmt.Errorf("maxRemediationsPerHour must be non-negative, got %d", r.MaxRemediationsPerHour)
	}
	if r.MaxRemediationsPerMinute < 0 {
		return fmt.Errorf("maxRemediationsPerMinute must be non-negative, got %d", r.MaxRemediationsPerMinute)
	}

	// Validate cooldown period
	if r.CooldownPeriod <= 0 {
		return fmt.Errorf("cooldownPeriod must be positive, got %v", r.CooldownPeriod)
	}

	// Validate max attempts
	if r.MaxAttemptsGlobal <= 0 {
		return fmt.Errorf("maxAttemptsGlobal must be positive, got %d", r.MaxAttemptsGlobal)
	}

	// Validate history size
	if r.HistorySize <= 0 {
		return fmt.Errorf("historySize must be positive, got %d", r.HistorySize)
	}

	// Validate circuit breaker
	if r.CircuitBreaker.Enabled {
		if r.CircuitBreaker.Threshold <= 0 {
			return fmt.Errorf("circuitBreaker.threshold must be positive, got %d", r.CircuitBreaker.Threshold)
		}
		if r.CircuitBreaker.Timeout <= 0 {
			return fmt.Errorf("circuitBreaker.timeout must be positive, got %v", r.CircuitBreaker.Timeout)
		}
		if r.CircuitBreaker.SuccessThreshold <= 0 {
			return fmt.Errorf("circuitBreaker.successThreshold must be positive, got %d", r.CircuitBreaker.SuccessThreshold)
		}
	}

	// Validate overrides
	problemNames := make(map[string]bool)
	for i, override := range r.Overrides {
		if override.Problem == "" {
			return fmt.Errorf("override %d: problem is required", i)
		}
		if problemNames[override.Problem] {
			return fmt.Errorf("duplicate override for problem %q found", override.Problem)
		}
		problemNames[override.Problem] = true

		if override.CooldownString != "" && override.Cooldown <= 0 {
			return fmt.Errorf("override %q: cooldown must be positive when specified, got %v", override.Problem, override.Cooldown)
		}
		if override.MaxAttempts < 0 {
			return fmt.Errorf("override %q: maxAttempts must be non-negative, got %d", override.Problem, override.MaxAttempts)
		}
		if override.CircuitBreakerThreshold < 0 {
			return fmt.Errorf("override %q: circuitBreakerThreshold must be non-negative, got %d", override.Problem, override.CircuitBreakerThreshold)
		}
	}

	return nil
}

// ===============================================
// PHASE 4: ENVIRONMENT VARIABLE SUBSTITUTION
// ===============================================

// SubstituteEnvVars performs environment variable substitution on the configuration.
func (c *NodeDoctorConfig) SubstituteEnvVars() {
	// Substitute in settings
	c.Settings.SubstituteEnvVars()

	// Substitute in monitors
	for i := range c.Monitors {
		c.Monitors[i].SubstituteEnvVars()
	}

	// Substitute in exporters
	if c.Exporters.Kubernetes != nil {
		c.Exporters.Kubernetes.SubstituteEnvVars()
	}
	if c.Exporters.HTTP != nil {
		c.Exporters.HTTP.SubstituteEnvVars()
	}
	if c.Exporters.Prometheus != nil {
		c.Exporters.Prometheus.SubstituteEnvVars()
	}

	// Substitute in remediation
	c.Remediation.SubstituteEnvVars()
}

// SubstituteEnvVars performs environment variable substitution on GlobalSettings.
func (s *GlobalSettings) SubstituteEnvVars() {
	s.NodeName = os.ExpandEnv(s.NodeName)
	s.Kubeconfig = os.ExpandEnv(s.Kubeconfig)
	s.LogFile = os.ExpandEnv(s.LogFile)
}

// SubstituteEnvVars performs environment variable substitution on MonitorConfig.
func (m *MonitorConfig) SubstituteEnvVars() {
	// Substitute in config map recursively
	if m.Config != nil {
		m.Config = substituteEnvInMap(m.Config)
	}

	// Substitute in remediation
	if m.Remediation != nil {
		m.Remediation.SubstituteEnvVars()
	}
}

// SubstituteEnvVars performs environment variable substitution on MonitorRemediationConfig.
func (r *MonitorRemediationConfig) SubstituteEnvVars() {
	r.ScriptPath = os.ExpandEnv(r.ScriptPath)

	// Substitute in args slice
	for i := range r.Args {
		r.Args[i] = os.ExpandEnv(r.Args[i])
	}

	// Substitute in nested strategies
	for i := range r.Strategies {
		r.Strategies[i].SubstituteEnvVars()
	}
}

// SubstituteEnvVars performs environment variable substitution on KubernetesExporterConfig.
func (k *KubernetesExporterConfig) SubstituteEnvVars() {
	k.Namespace = os.ExpandEnv(k.Namespace)

	// Substitute in annotations
	for i := range k.Annotations {
		k.Annotations[i].Key = os.ExpandEnv(k.Annotations[i].Key)
		k.Annotations[i].Value = os.ExpandEnv(k.Annotations[i].Value)
	}
}

// SubstituteEnvVars performs environment variable substitution on HTTPExporterConfig.
func (h *HTTPExporterConfig) SubstituteEnvVars() {
	h.TLSCertFile = os.ExpandEnv(h.TLSCertFile)
	h.TLSKeyFile = os.ExpandEnv(h.TLSKeyFile)
}

// SubstituteEnvVars performs environment variable substitution on PrometheusExporterConfig.
func (p *PrometheusExporterConfig) SubstituteEnvVars() {
	p.Namespace = os.ExpandEnv(p.Namespace)
	p.Subsystem = os.ExpandEnv(p.Subsystem)

	// Substitute in labels map
	for key, value := range p.Labels {
		p.Labels[key] = os.ExpandEnv(value)
	}
}

// SubstituteEnvVars performs environment variable substitution on RemediationConfig.
func (r *RemediationConfig) SubstituteEnvVars() {
	// No string fields that typically need environment variable substitution
	// This method is provided for consistency and future extensibility
}

// substituteEnvInMap recursively substitutes environment variables in a map.
func substituteEnvInMap(m map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for key, value := range m {
		switch v := value.(type) {
		case string:
			result[key] = os.ExpandEnv(v)
		case map[string]interface{}:
			result[key] = substituteEnvInMap(v)
		case []interface{}:
			result[key] = substituteEnvInSlice(v)
		case map[interface{}]interface{}:
			// Handle YAML-style maps with interface{} keys
			stringMap := make(map[string]interface{})
			for k, val := range v {
				if strKey, ok := k.(string); ok {
					stringMap[strKey] = val
				}
			}
			result[key] = substituteEnvInMap(stringMap)
		default:
			// Keep other types as-is (numbers, booleans, etc.)
			result[key] = value
		}
	}
	return result
}

// substituteEnvInSlice recursively substitutes environment variables in a slice.
func substituteEnvInSlice(s []interface{}) []interface{} {
	result := make([]interface{}, len(s))
	for i, value := range s {
		switch v := value.(type) {
		case string:
			result[i] = os.ExpandEnv(v)
		case map[string]interface{}:
			result[i] = substituteEnvInMap(v)
		case []interface{}:
			result[i] = substituteEnvInSlice(v)
		case map[interface{}]interface{}:
			// Handle YAML-style maps with interface{} keys
			stringMap := make(map[string]interface{})
			for k, val := range v {
				if strKey, ok := k.(string); ok {
					stringMap[strKey] = val
				}
			}
			result[i] = substituteEnvInMap(stringMap)
		default:
			// Keep other types as-is (numbers, booleans, etc.)
			result[i] = value
		}
	}
	return result
}