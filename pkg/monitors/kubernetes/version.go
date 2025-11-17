// Package kubernetes provides Kubernetes component health monitoring capabilities.
package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Kubernetes version support constants
const (
	// MinSupportedVersion is the minimum Kubernetes version supported by node-doctor
	MinSupportedVersion = "1.24.0"

	// MinSupportedMajor is the minimum major version
	MinSupportedMajor = 1

	// MinSupportedMinor is the minimum minor version
	MinSupportedMinor = 24

	// TestedMaxMinor is the maximum tested minor version
	// This should be updated as new versions are tested
	TestedMaxMinor = 30
)

// KubeletVersionInfo represents version information from kubelet
type KubeletVersionInfo struct {
	Major        string `json:"major"`
	Minor        string `json:"minor"`
	GitVersion   string `json:"gitVersion"`
	GitCommit    string `json:"gitCommit"`
	GitTreeState string `json:"gitTreeState"`
	BuildDate    string `json:"buildDate"`
	GoVersion    string `json:"goVersion"`
	Compiler     string `json:"compiler"`
	Platform     string `json:"platform"`
}

// SemanticVersion represents a parsed semantic version
type SemanticVersion struct {
	Major int
	Minor int
	Patch int
}

// String returns the string representation of the version
func (v *SemanticVersion) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// IsSupported checks if the version is supported (>= 1.24.0)
func (v *SemanticVersion) IsSupported() bool {
	if v.Major < MinSupportedMajor {
		return false
	}
	if v.Major == MinSupportedMajor && v.Minor < MinSupportedMinor {
		return false
	}
	return true
}

// IsTested checks if the version has been explicitly tested
func (v *SemanticVersion) IsTested() bool {
	if v.Major != MinSupportedMajor {
		return false
	}
	return v.Minor <= TestedMaxMinor
}

// GetCompatibilityStatus returns a human-readable compatibility status
func (v *SemanticVersion) GetCompatibilityStatus() string {
	if !v.IsSupported() {
		return fmt.Sprintf("UNSUPPORTED (minimum: %s)", MinSupportedVersion)
	}
	if !v.IsTested() {
		return fmt.Sprintf("UNTESTED (tested up to 1.%d.x)", TestedMaxMinor)
	}
	return "SUPPORTED"
}

// ParseSemanticVersion parses a version string like "v1.28.0" or "1.28.0"
// into a SemanticVersion struct
func ParseSemanticVersion(versionStr string) (*SemanticVersion, error) {
	// Remove 'v' prefix if present
	versionStr = strings.TrimPrefix(versionStr, "v")

	// Split by '.' to get major.minor.patch
	parts := strings.Split(versionStr, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid version format: %s (expected major.minor.patch)", versionStr)
	}

	var major, minor, patch int
	var err error

	// Parse major version
	if _, err = fmt.Sscanf(parts[0], "%d", &major); err != nil {
		return nil, fmt.Errorf("invalid major version: %s", parts[0])
	}

	// Parse minor version (may have suffix like "28+" or "28-gke.1")
	minorStr := parts[1]
	// Extract numeric part before any non-numeric character
	for i, c := range minorStr {
		if c < '0' || c > '9' {
			minorStr = minorStr[:i]
			break
		}
	}
	if _, err = fmt.Sscanf(minorStr, "%d", &minor); err != nil {
		return nil, fmt.Errorf("invalid minor version: %s", parts[1])
	}

	// Parse patch version (optional)
	if len(parts) >= 3 {
		patchStr := parts[2]
		originalPatchStr := patchStr
		// Extract numeric part before any non-numeric character
		for i, c := range patchStr {
			if c < '0' || c > '9' {
				patchStr = patchStr[:i]
				break
			}
		}
		// If patchStr is empty but original had content, it means it started with non-numeric
		if patchStr == "" && originalPatchStr != "" {
			return nil, fmt.Errorf("invalid patch version: %s", parts[2])
		}
		if patchStr != "" {
			if _, err = fmt.Sscanf(patchStr, "%d", &patch); err != nil {
				return nil, fmt.Errorf("invalid patch version: %s", parts[2])
			}
		}
	}

	return &SemanticVersion{
		Major: major,
		Minor: minor,
		Patch: patch,
	}, nil
}

// KubeletVersionClient interface for version detection (for testing)
type KubeletVersionClient interface {
	GetVersion(ctx context.Context) (*KubeletVersionInfo, error)
}

// DefaultKubeletVersionClient implements version detection using HTTP
type DefaultKubeletVersionClient struct {
	versionURL string
	timeout    int
	client     *http.Client
	auth       *AuthConfig
}

// NewKubeletVersionClient creates a new version detection client
func NewKubeletVersionClient(versionURL string, timeout int, auth *AuthConfig) KubeletVersionClient {
	client := &http.Client{
		Timeout: timeoutToDuration(timeout),
	}

	return &DefaultKubeletVersionClient{
		versionURL: versionURL,
		timeout:    timeout,
		client:     client,
		auth:       auth,
	}
}

// GetVersion retrieves version information from the kubelet /version endpoint
func (c *DefaultKubeletVersionClient) GetVersion(ctx context.Context) (*KubeletVersionInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.versionURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create version request: %w", err)
	}

	// Add authentication header if configured
	if c.auth != nil {
		if err := addAuthHeaderToRequest(req, c.auth); err != nil {
			return nil, fmt.Errorf("failed to add authentication header: %w", err)
		}
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("version request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Drain response body
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("version endpoint returned status %d, expected 200", resp.StatusCode)
	}

	// Read and parse JSON response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read version response: %w", err)
	}

	var versionInfo KubeletVersionInfo
	if err := json.Unmarshal(body, &versionInfo); err != nil {
		return nil, fmt.Errorf("failed to parse version JSON: %w", err)
	}

	return &versionInfo, nil
}

// addAuthHeaderToRequest adds authentication headers based on auth configuration
func addAuthHeaderToRequest(req *http.Request, auth *AuthConfig) error {
	if auth == nil || auth.Type == "none" || auth.Type == "" {
		return nil
	}

	switch auth.Type {
	case "serviceaccount":
		tokenFile := auth.TokenFile
		if tokenFile == "" {
			tokenFile = defaultServiceAccountTokenPath
		}
		token, err := readFile(tokenFile)
		if err != nil {
			return fmt.Errorf("failed to read ServiceAccount token from %s: %w", tokenFile, err)
		}
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))

	case "bearer":
		if auth.BearerToken == "" {
			return fmt.Errorf("bearerToken is required for auth type 'bearer'")
		}
		req.Header.Set("Authorization", "Bearer "+auth.BearerToken)

	case "certificate":
		// Client certificate authentication is handled by TLS config
		return nil

	default:
		return fmt.Errorf("unsupported authentication type: %s", auth.Type)
	}

	return nil
}

// Helper function to read file content (abstracted for testing)
var readFile = func(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Helper function to convert timeout seconds to duration
func timeoutToDuration(seconds int) time.Duration {
	if seconds <= 0 {
		return 5 * time.Second // Default 5 seconds
	}
	return time.Duration(seconds) * time.Second
}
