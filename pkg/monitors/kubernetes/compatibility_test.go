package kubernetes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// TestKubeletMonitor_VersionCompatibility tests kubelet monitor against different Kubernetes versions
func TestKubeletMonitor_VersionCompatibility(t *testing.T) {
	tests := []struct {
		name            string
		k8sVersion      string
		expectWarning   bool
		warningContains string
		metricsFormat   string // "legacy" or "current"
	}{
		{
			name:          "Kubernetes 1.24.0 - Minimum supported",
			k8sVersion:    "v1.24.0",
			expectWarning: false,
			metricsFormat: "current",
		},
		{
			name:          "Kubernetes 1.27.5 - Supported",
			k8sVersion:    "v1.27.5",
			expectWarning: false,
			metricsFormat: "current",
		},
		{
			name:          "Kubernetes 1.28.0 - Current baseline",
			k8sVersion:    "v1.28.0",
			expectWarning: false,
			metricsFormat: "current",
		},
		{
			name:          "Kubernetes 1.30.0 - Latest tested",
			k8sVersion:    "v1.30.0",
			expectWarning: false,
			metricsFormat: "current",
		},
		{
			name:            "Kubernetes 1.31.0 - Untested version",
			k8sVersion:      "v1.31.0",
			expectWarning:   true,
			warningContains: "untested",
			metricsFormat:   "current",
		},
		{
			name:            "Kubernetes 1.23.0 - Below minimum",
			k8sVersion:      "v1.23.0",
			expectWarning:   true,
			warningContains: "unsupported",
			metricsFormat:   "legacy",
		},
		{
			name:          "GKE version 1.28.0-gke.1234",
			k8sVersion:    "v1.28.0-gke.1234",
			expectWarning: false,
			metricsFormat: "current",
		},
		{
			name:          "EKS version 1.27.3-eks-a5565ad",
			k8sVersion:    "v1.27.3-eks-a5565ad",
			expectWarning: false,
			metricsFormat: "current",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test servers for healthz, metrics, and version
			healthzServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("ok"))
			}))
			defer healthzServer.Close()

			metricsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				// Return PLEG metrics in current format
				metrics := `# HELP kubelet_pleg_relist_duration_seconds Duration in seconds for relisting pods in PLEG.
# TYPE kubelet_pleg_relist_duration_seconds summary
kubelet_pleg_relist_duration_seconds{quantile="0.5"} 0.001
kubelet_pleg_relist_duration_seconds{quantile="0.9"} 0.003
kubelet_pleg_relist_duration_seconds{quantile="0.99"} 0.004
kubelet_pleg_relist_duration_seconds_sum 123.45
kubelet_pleg_relist_duration_seconds_count 67890
`
				w.Write([]byte(metrics))
			}))
			defer metricsServer.Close()

			versionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				versionInfo := &KubeletVersionInfo{
					Major:      "1",
					Minor:      strings.Split(strings.TrimPrefix(tt.k8sVersion, "v"), ".")[1],
					GitVersion: tt.k8sVersion,
					GitCommit:  "test-commit",
					BuildDate:  "2024-01-01T00:00:00Z",
					GoVersion:  "go1.21.0",
					Compiler:   "gc",
					Platform:   "linux/amd64",
				}
				data, _ := json.Marshal(versionInfo)
				w.WriteHeader(http.StatusOK)
				w.Write(data)
			}))
			defer versionServer.Close()

			// Create monitor configuration
			config := types.MonitorConfig{
				Name:     "test-kubelet-compat",
				Type:     "kubernetes-kubelet-check",
				Interval: 30 * time.Second,
				Timeout:  10 * time.Second,
				Config: map[string]interface{}{
					"healthzURL":       healthzServer.URL,
					"metricsURL":       metricsServer.URL,
					"versionURL":       versionServer.URL + "/version",
					"checkPLEG":        true,
					"plegThreshold":    "5s",
					"failureThreshold": 3,
				},
			}

			// Create monitor
			ctx := context.Background()
			monitor, err := NewKubeletMonitor(ctx, config)
			if err != nil {
				t.Fatalf("Failed to create kubelet monitor: %v", err)
			}

			kubeletMonitor := monitor.(*KubeletMonitor)

			// Perform version detection
			versionClient := NewKubeletVersionClient(versionServer.URL+"/version", 5, nil)
			versionInfo, err := versionClient.GetVersion(ctx)
			if err != nil {
				t.Fatalf("Failed to get version: %v", err)
			}

			// Parse version
			version, err := ParseSemanticVersion(versionInfo.GitVersion)
			if err != nil {
				t.Fatalf("Failed to parse version: %v", err)
			}

			// Check compatibility status
			status := version.GetCompatibilityStatus()

			if tt.expectWarning {
				if !strings.Contains(strings.ToLower(status), strings.ToLower(tt.warningContains)) {
					t.Errorf("Expected compatibility status to contain %q, got %q", tt.warningContains, status)
				}
			} else {
				if status != "SUPPORTED" {
					t.Errorf("Expected SUPPORTED status, got %q", status)
				}
			}

			// Verify monitor can perform checks regardless of version
			kubeletStatus, err := kubeletMonitor.checkKubelet(ctx)
			if err != nil {
				t.Fatalf("checkKubelet failed: %v", err)
			}

			if kubeletStatus == nil {
				t.Fatal("Expected status but got nil")
			}
		})
	}
}

// TestKubeletMonitor_PLEGMetricsCompatibility tests PLEG metrics parsing across versions
func TestKubeletMonitor_PLEGMetricsCompatibility(t *testing.T) {
	tests := []struct {
		name          string
		k8sVersion    string
		metricsData   string
		expectPLEG    bool
		expectedValue float64
	}{
		{
			name:       "Kubernetes 1.24 - Summary format",
			k8sVersion: "v1.24.0",
			metricsData: `# TYPE kubelet_pleg_relist_duration_seconds summary
kubelet_pleg_relist_duration_seconds{quantile="0.5"} 0.001
kubelet_pleg_relist_duration_seconds{quantile="0.9"} 0.003
kubelet_pleg_relist_duration_seconds{quantile="0.99"} 0.005
`,
			expectPLEG:    true,
			expectedValue: 0.005,
		},
		{
			name:       "Kubernetes 1.28 - Summary format",
			k8sVersion: "v1.28.0",
			metricsData: `# TYPE kubelet_pleg_relist_duration_seconds summary
kubelet_pleg_relist_duration_seconds{quantile="0.5"} 0.002
kubelet_pleg_relist_duration_seconds{quantile="0.9"} 0.004
kubelet_pleg_relist_duration_seconds{quantile="0.99"} 0.006
`,
			expectPLEG:    true,
			expectedValue: 0.006,
		},
		{
			name:       "Kubernetes 1.30 - Summary format",
			k8sVersion: "v1.30.0",
			metricsData: `# TYPE kubelet_pleg_relist_duration_seconds summary
kubelet_pleg_relist_duration_seconds{quantile="0.5"} 0.001
kubelet_pleg_relist_duration_seconds{quantile="0.9"} 0.002
kubelet_pleg_relist_duration_seconds{quantile="0.99"} 0.003
`,
			expectPLEG:    true,
			expectedValue: 0.003,
		},
		{
			name:          "Missing PLEG metric",
			k8sVersion:    "v1.28.0",
			metricsData:   `# Other metrics only`,
			expectPLEG:    false,
			expectedValue: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metricsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(tt.metricsData))
			}))
			defer metricsServer.Close()

			config := &KubeletMonitorConfig{
				MetricsURL:  metricsServer.URL,
				HTTPTimeout: 5 * time.Second,
			}
			client := newDefaultKubeletClient(config)

			ctx := context.Background()
			metrics, err := client.GetMetrics(ctx)
			if err != nil {
				t.Fatalf("GetMetrics failed: %v", err)
			}

			if metrics == nil {
				t.Fatal("Expected metrics but got nil")
			}

			if tt.expectPLEG {
				if metrics.PLEGRelistDuration != tt.expectedValue {
					t.Errorf("PLEGRelistDuration = %v, want %v", metrics.PLEGRelistDuration, tt.expectedValue)
				}
			} else {
				if metrics.PLEGRelistDuration != 0.0 {
					t.Errorf("Expected no PLEG metric (0.0), got %v", metrics.PLEGRelistDuration)
				}
			}
		})
	}
}

// TestKubeletMonitor_EndpointCompatibility tests healthz and metrics endpoint availability
func TestKubeletMonitor_EndpointCompatibility(t *testing.T) {
	tests := []struct {
		name          string
		k8sVersion    string
		healthzPort   string
		metricsPort   string
		expectHealthz bool
		expectMetrics bool
	}{
		{
			name:          "Kubernetes 1.24 - Standard ports",
			k8sVersion:    "v1.24.0",
			healthzPort:   "10248",
			metricsPort:   "10250",
			expectHealthz: true,
			expectMetrics: true,
		},
		{
			name:          "Kubernetes 1.28 - Standard ports",
			k8sVersion:    "v1.28.0",
			healthzPort:   "10248",
			metricsPort:   "10250",
			expectHealthz: true,
			expectMetrics: true,
		},
		{
			name:          "Kubernetes 1.30 - Standard ports",
			k8sVersion:    "v1.30.0",
			healthzPort:   "10248",
			metricsPort:   "10250",
			expectHealthz: true,
			expectMetrics: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create healthz server
			healthzServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.expectHealthz {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte("ok"))
				} else {
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer healthzServer.Close()

			// Create metrics server
			metricsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.expectMetrics {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(`kubelet_pleg_relist_duration_seconds{quantile="0.99"} 0.003`))
				} else {
					w.WriteHeader(http.StatusForbidden)
				}
			}))
			defer metricsServer.Close()

			config := &KubeletMonitorConfig{
				HealthzURL:  healthzServer.URL,
				MetricsURL:  metricsServer.URL,
				HTTPTimeout: 5 * time.Second,
			}
			client := newDefaultKubeletClient(config)

			ctx := context.Background()

			// Test healthz
			healthErr := client.CheckHealth(ctx)
			if tt.expectHealthz && healthErr != nil {
				t.Errorf("Expected healthz to succeed, got error: %v", healthErr)
			}
			if !tt.expectHealthz && healthErr == nil {
				t.Error("Expected healthz to fail, but it succeeded")
			}

			// Test metrics
			metrics, metricsErr := client.GetMetrics(ctx)
			if tt.expectMetrics {
				if metricsErr != nil {
					t.Errorf("Expected metrics to succeed, got error: %v", metricsErr)
				}
				if metrics == nil {
					t.Error("Expected metrics but got nil")
				}
			} else {
				if metricsErr == nil {
					t.Error("Expected metrics to fail, but it succeeded")
				}
			}
		})
	}
}

// TestKubeletMonitor_AuthenticationCompatibility tests authentication across versions
func TestKubeletMonitor_AuthenticationCompatibility(t *testing.T) {
	// Create temporary token file for ServiceAccount auth testing
	tmpfile, err := os.CreateTemp("", "test-token-*")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte("test-serviceaccount-token")); err != nil {
		t.Fatalf("Failed to write token file: %v", err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatalf("Failed to close token file: %v", err)
	}

	tests := []struct {
		name       string
		k8sVersion string
		authType   string
		expectAuth bool
		authHeader string
	}{
		{
			name:       "Kubernetes 1.24 - No auth (healthz)",
			k8sVersion: "v1.24.0",
			authType:   "none",
			expectAuth: false,
		},
		{
			name:       "Kubernetes 1.24 - ServiceAccount auth",
			k8sVersion: "v1.24.0",
			authType:   "serviceaccount",
			expectAuth: true,
			authHeader: "Bearer test-serviceaccount-token",
		},
		{
			name:       "Kubernetes 1.28 - ServiceAccount auth",
			k8sVersion: "v1.28.0",
			authType:   "serviceaccount",
			expectAuth: true,
			authHeader: "Bearer test-serviceaccount-token",
		},
		{
			name:       "Kubernetes 1.30 - Bearer token auth",
			k8sVersion: "v1.30.0",
			authType:   "bearer",
			expectAuth: true,
			authHeader: "Bearer custom-bearer-token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var receivedAuthHeader string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				receivedAuthHeader = r.Header.Get("Authorization")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("ok"))
			}))
			defer server.Close()

			var authConfig *AuthConfig
			if tt.authType == "serviceaccount" {
				authConfig = &AuthConfig{
					Type:      "serviceaccount",
					TokenFile: tmpfile.Name(),
				}
			} else if tt.authType == "bearer" {
				authConfig = &AuthConfig{
					Type:        "bearer",
					BearerToken: "custom-bearer-token",
				}
			}

			config := &KubeletMonitorConfig{
				HealthzURL:  server.URL,
				HTTPTimeout: 5 * time.Second,
				Auth:        authConfig,
			}
			client := newDefaultKubeletClient(config)

			ctx := context.Background()
			err := client.CheckHealth(ctx)
			if err != nil {
				t.Fatalf("CheckHealth failed: %v", err)
			}

			if tt.expectAuth {
				if receivedAuthHeader != tt.authHeader {
					t.Errorf("Authorization header = %q, want %q", receivedAuthHeader, tt.authHeader)
				}
			} else {
				if receivedAuthHeader != "" {
					t.Errorf("Expected no Authorization header, got %q", receivedAuthHeader)
				}
			}
		})
	}
}
