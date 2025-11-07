package custom

import (
	"encoding/json"
	"fmt"
	"strings"
)

// PluginStatus represents the health status returned by a plugin
type PluginStatus string

const (
	// PluginStatusHealthy indicates the check passed
	PluginStatusHealthy PluginStatus = "healthy"

	// PluginStatusWarning indicates a warning condition
	PluginStatusWarning PluginStatus = "warning"

	// PluginStatusCritical indicates a critical condition
	PluginStatusCritical PluginStatus = "critical"

	// PluginStatusUnknown indicates an unknown or error state
	PluginStatusUnknown PluginStatus = "unknown"
)

// PluginResult represents the parsed output from a plugin execution
type PluginResult struct {
	Status  PluginStatus
	Message string
	Metrics map[string]interface{}
}

// JSONOutput represents the expected JSON structure from plugins
type JSONOutput struct {
	Status  string                 `json:"status"`
	Message string                 `json:"message"`
	Metrics map[string]interface{} `json:"metrics,omitempty"`
}

// OutputFormat specifies how to parse plugin output
type OutputFormat string

const (
	// OutputFormatJSON expects JSON-formatted output
	OutputFormatJSON OutputFormat = "json"

	// OutputFormatSimple expects simple text output (OK: or ERROR: prefix)
	OutputFormatSimple OutputFormat = "simple"
)

// OutputParser handles parsing plugin output into structured results
type OutputParser struct {
	format OutputFormat
}

// NewOutputParser creates a new output parser for the specified format
func NewOutputParser(format OutputFormat) *OutputParser {
	return &OutputParser{
		format: format,
	}
}

// Parse parses the plugin output according to the configured format
func (p *OutputParser) Parse(stdout, stderr string, exitCode int) (*PluginResult, error) {
	switch p.format {
	case OutputFormatJSON:
		return p.parseJSON(stdout, stderr, exitCode)
	case OutputFormatSimple:
		return p.parseSimple(stdout, stderr, exitCode)
	default:
		return nil, fmt.Errorf("unsupported output format: %s", p.format)
	}
}

// parseJSON parses JSON-formatted plugin output
func (p *OutputParser) parseJSON(stdout, stderr string, exitCode int) (*PluginResult, error) {
	// If stdout is empty, use exit code and stderr
	if strings.TrimSpace(stdout) == "" {
		return p.fallbackToExitCode(exitCode, stderr), nil
	}

	// Try to parse JSON
	var output JSONOutput
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		// JSON parsing failed, fall back to exit code interpretation
		return p.fallbackToExitCode(exitCode, fmt.Sprintf("JSON parse error: %v\nOutput: %s", err, stdout)), nil
	}

	// Validate and normalize status
	status := normalizeStatus(output.Status)

	// Use message from JSON or fall back to stderr
	message := output.Message
	if message == "" && stderr != "" {
		message = stderr
	}

	// If status is unknown from JSON, use exit code
	if status == PluginStatusUnknown && output.Status != "" {
		status = statusFromExitCode(exitCode)
	}

	return &PluginResult{
		Status:  status,
		Message: message,
		Metrics: output.Metrics,
	}, nil
}

// parseSimple parses simple text-formatted plugin output
func (p *OutputParser) parseSimple(stdout, stderr string, exitCode int) (*PluginResult, error) {
	output := strings.TrimSpace(stdout)

	// If stdout is empty, use stderr
	if output == "" {
		output = strings.TrimSpace(stderr)
	}

	// Determine status from exit code
	status := statusFromExitCode(exitCode)

	// Try to extract more specific status from output prefix
	if strings.HasPrefix(output, "OK:") {
		status = PluginStatusHealthy
		output = strings.TrimSpace(strings.TrimPrefix(output, "OK:"))
	} else if strings.HasPrefix(output, "WARNING:") {
		status = PluginStatusWarning
		output = strings.TrimSpace(strings.TrimPrefix(output, "WARNING:"))
	} else if strings.HasPrefix(output, "CRITICAL:") {
		status = PluginStatusCritical
		output = strings.TrimSpace(strings.TrimPrefix(output, "CRITICAL:"))
	} else if strings.HasPrefix(output, "ERROR:") {
		status = PluginStatusCritical
		output = strings.TrimSpace(strings.TrimPrefix(output, "ERROR:"))
	} else if strings.HasPrefix(output, "UNKNOWN:") {
		status = PluginStatusUnknown
		output = strings.TrimSpace(strings.TrimPrefix(output, "UNKNOWN:"))
	}

	return &PluginResult{
		Status:  status,
		Message: output,
		Metrics: nil, // Simple format doesn't support metrics
	}, nil
}

// fallbackToExitCode creates a result based on exit code when parsing fails
func (p *OutputParser) fallbackToExitCode(exitCode int, message string) *PluginResult {
	return &PluginResult{
		Status:  statusFromExitCode(exitCode),
		Message: message,
		Metrics: nil,
	}
}

// statusFromExitCode maps exit codes to plugin status
func statusFromExitCode(exitCode int) PluginStatus {
	switch exitCode {
	case 0:
		return PluginStatusHealthy
	case 1:
		return PluginStatusWarning
	case 2:
		return PluginStatusCritical
	default:
		return PluginStatusUnknown
	}
}

// normalizeStatus converts string status to PluginStatus enum
func normalizeStatus(status string) PluginStatus {
	normalized := strings.ToLower(strings.TrimSpace(status))

	switch normalized {
	case "healthy", "ok", "good", "pass", "passed":
		return PluginStatusHealthy
	case "warning", "warn":
		return PluginStatusWarning
	case "critical", "crit", "error", "fail", "failed":
		return PluginStatusCritical
	case "unknown":
		return PluginStatusUnknown
	default:
		return PluginStatusUnknown
	}
}
