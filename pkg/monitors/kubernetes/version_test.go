package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestParseSemanticVersion tests version string parsing
func TestParseSemanticVersion(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantMajor   int
		wantMinor   int
		wantPatch   int
		wantErr     bool
		errContains string
	}{
		{
			name:      "Valid version with v prefix",
			input:     "v1.28.0",
			wantMajor: 1,
			wantMinor: 28,
			wantPatch: 0,
			wantErr:   false,
		},
		{
			name:      "Valid version without v prefix",
			input:     "1.28.0",
			wantMajor: 1,
			wantMinor: 28,
			wantPatch: 0,
			wantErr:   false,
		},
		{
			name:      "Kubernetes 1.24.0",
			input:     "v1.24.0",
			wantMajor: 1,
			wantMinor: 24,
			wantPatch: 0,
			wantErr:   false,
		},
		{
			name:      "Kubernetes 1.27.5",
			input:     "v1.27.5",
			wantMajor: 1,
			wantMinor: 27,
			wantPatch: 5,
			wantErr:   false,
		},
		{
			name:      "Kubernetes 1.30.0",
			input:     "v1.30.0",
			wantMajor: 1,
			wantMinor: 30,
			wantPatch: 0,
			wantErr:   false,
		},
		{
			name:      "Version with suffix - GKE",
			input:     "v1.28.0-gke.1234",
			wantMajor: 1,
			wantMinor: 28,
			wantPatch: 0,
			wantErr:   false,
		},
		{
			name:      "Version with suffix - EKS",
			input:     "v1.27.3-eks-a5565ad",
			wantMajor: 1,
			wantMinor: 27,
			wantPatch: 3,
			wantErr:   false,
		},
		{
			name:      "Version with plus suffix",
			input:     "v1.28.0+k3s1",
			wantMajor: 1,
			wantMinor: 28,
			wantPatch: 0,
			wantErr:   false,
		},
		{
			name:      "Version without patch",
			input:     "v1.28",
			wantMajor: 1,
			wantMinor: 28,
			wantPatch: 0,
			wantErr:   false,
		},
		{
			name:      "Version with minor suffix only",
			input:     "1.28+",
			wantMajor: 1,
			wantMinor: 28,
			wantPatch: 0,
			wantErr:   false,
		},
		{
			name:        "Invalid version - single number",
			input:       "1",
			wantErr:     true,
			errContains: "invalid version format",
		},
		{
			name:        "Invalid version - non-numeric major",
			input:       "abc.28.0",
			wantErr:     true,
			errContains: "invalid major version",
		},
		{
			name:        "Invalid version - non-numeric minor",
			input:       "1.abc.0",
			wantErr:     true,
			errContains: "invalid minor version",
		},
		{
			name:        "Invalid version - non-numeric patch",
			input:       "1.28.abc",
			wantErr:     true,
			errContains: "invalid patch version",
		},
		{
			name:        "Empty string",
			input:       "",
			wantErr:     true,
			errContains: "invalid version format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSemanticVersion(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error but got none")
				} else if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got.Major != tt.wantMajor {
				t.Errorf("Major = %d, want %d", got.Major, tt.wantMajor)
			}
			if got.Minor != tt.wantMinor {
				t.Errorf("Minor = %d, want %d", got.Minor, tt.wantMinor)
			}
			if got.Patch != tt.wantPatch {
				t.Errorf("Patch = %d, want %d", got.Patch, tt.wantPatch)
			}
		})
	}
}

// TestSemanticVersion_IsSupported tests version support checks
func TestSemanticVersion_IsSupported(t *testing.T) {
	tests := []struct {
		name    string
		version *SemanticVersion
		want    bool
	}{
		{
			name:    "Kubernetes 1.24.0 - minimum supported",
			version: &SemanticVersion{Major: 1, Minor: 24, Patch: 0},
			want:    true,
		},
		{
			name:    "Kubernetes 1.28.0 - supported",
			version: &SemanticVersion{Major: 1, Minor: 28, Patch: 0},
			want:    true,
		},
		{
			name:    "Kubernetes 1.30.0 - latest tested",
			version: &SemanticVersion{Major: 1, Minor: 30, Patch: 0},
			want:    true,
		},
		{
			name:    "Kubernetes 1.23.0 - below minimum",
			version: &SemanticVersion{Major: 1, Minor: 23, Patch: 0},
			want:    false,
		},
		{
			name:    "Kubernetes 1.20.0 - old version",
			version: &SemanticVersion{Major: 1, Minor: 20, Patch: 0},
			want:    false,
		},
		{
			name:    "Kubernetes 0.x - very old",
			version: &SemanticVersion{Major: 0, Minor: 99, Patch: 0},
			want:    false,
		},
		{
			name:    "Kubernetes 2.0.0 - future major version",
			version: &SemanticVersion{Major: 2, Minor: 0, Patch: 0},
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.version.IsSupported()
			if got != tt.want {
				t.Errorf("IsSupported() = %v, want %v for version %s", got, tt.want, tt.version.String())
			}
		})
	}
}

// TestSemanticVersion_IsTested tests version testing coverage checks
func TestSemanticVersion_IsTested(t *testing.T) {
	tests := []struct {
		name    string
		version *SemanticVersion
		want    bool
	}{
		{
			name:    "Kubernetes 1.24.0 - tested",
			version: &SemanticVersion{Major: 1, Minor: 24, Patch: 0},
			want:    true,
		},
		{
			name:    "Kubernetes 1.28.0 - tested",
			version: &SemanticVersion{Major: 1, Minor: 28, Patch: 0},
			want:    true,
		},
		{
			name:    "Kubernetes 1.30.0 - tested",
			version: &SemanticVersion{Major: 1, Minor: 30, Patch: 0},
			want:    true,
		},
		{
			name:    "Kubernetes 1.31.0 - untested (beyond max)",
			version: &SemanticVersion{Major: 1, Minor: 31, Patch: 0},
			want:    false,
		},
		{
			name:    "Kubernetes 1.35.0 - untested",
			version: &SemanticVersion{Major: 1, Minor: 35, Patch: 0},
			want:    false,
		},
		{
			name:    "Kubernetes 2.0.0 - untested (major version change)",
			version: &SemanticVersion{Major: 2, Minor: 0, Patch: 0},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.version.IsTested()
			if got != tt.want {
				t.Errorf("IsTested() = %v, want %v for version %s", got, tt.want, tt.version.String())
			}
		})
	}
}

// TestSemanticVersion_GetCompatibilityStatus tests status message generation
func TestSemanticVersion_GetCompatibilityStatus(t *testing.T) {
	tests := []struct {
		name    string
		version *SemanticVersion
		want    string
	}{
		{
			name:    "Kubernetes 1.28.0 - supported and tested",
			version: &SemanticVersion{Major: 1, Minor: 28, Patch: 0},
			want:    "SUPPORTED",
		},
		{
			name:    "Kubernetes 1.23.0 - unsupported",
			version: &SemanticVersion{Major: 1, Minor: 23, Patch: 0},
			want:    "UNSUPPORTED (minimum: 1.24.0)",
		},
		{
			name:    "Kubernetes 1.31.0 - untested",
			version: &SemanticVersion{Major: 1, Minor: 31, Patch: 0},
			want:    "UNTESTED (tested up to 1.30.x)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.version.GetCompatibilityStatus()
			if got != tt.want {
				t.Errorf("GetCompatibilityStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestSemanticVersion_String tests string representation
func TestSemanticVersion_String(t *testing.T) {
	tests := []struct {
		name    string
		version *SemanticVersion
		want    string
	}{
		{
			name:    "Standard version",
			version: &SemanticVersion{Major: 1, Minor: 28, Patch: 0},
			want:    "1.28.0",
		},
		{
			name:    "Version with patch",
			version: &SemanticVersion{Major: 1, Minor: 27, Patch: 5},
			want:    "1.27.5",
		},
		{
			name:    "Zero patch version",
			version: &SemanticVersion{Major: 1, Minor: 24, Patch: 0},
			want:    "1.24.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.version.String()
			if got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDefaultKubeletVersionClient_GetVersion tests version retrieval from kubelet
func TestDefaultKubeletVersionClient_GetVersion(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		responseBody   *KubeletVersionInfo
		wantErr        bool
		errContains    string
		wantGitVersion string
		serverDelay    time.Duration
	}{
		{
			name:       "Success - Kubernetes 1.28.0",
			statusCode: 200,
			responseBody: &KubeletVersionInfo{
				Major:        "1",
				Minor:        "28",
				GitVersion:   "v1.28.0",
				GitCommit:    "abc123",
				GitTreeState: "clean",
				BuildDate:    "2024-01-01T00:00:00Z",
				GoVersion:    "go1.21.0",
				Compiler:     "gc",
				Platform:     "linux/amd64",
			},
			wantGitVersion: "v1.28.0",
			wantErr:        false,
		},
		{
			name:       "Success - Kubernetes 1.24.0",
			statusCode: 200,
			responseBody: &KubeletVersionInfo{
				Major:      "1",
				Minor:      "24",
				GitVersion: "v1.24.0",
			},
			wantGitVersion: "v1.24.0",
			wantErr:        false,
		},
		{
			name:       "Success - GKE version with suffix",
			statusCode: 200,
			responseBody: &KubeletVersionInfo{
				Major:      "1",
				Minor:      "28",
				GitVersion: "v1.28.0-gke.1234",
			},
			wantGitVersion: "v1.28.0-gke.1234",
			wantErr:        false,
		},
		{
			name:        "Failure - HTTP 404",
			statusCode:  404,
			wantErr:     true,
			errContains: "version endpoint returned status 404",
		},
		{
			name:        "Failure - HTTP 500",
			statusCode:  500,
			wantErr:     true,
			errContains: "version endpoint returned status 500",
		},
		{
			name:        "Failure - HTTP 403 Forbidden",
			statusCode:  403,
			wantErr:     true,
			errContains: "version endpoint returned status 403",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request path
				if r.URL.Path != "/version" && r.URL.Path != "" {
					t.Errorf("unexpected request path: %s", r.URL.Path)
				}

				if tt.serverDelay > 0 {
					time.Sleep(tt.serverDelay)
				}

				w.WriteHeader(tt.statusCode)
				if tt.statusCode == 200 && tt.responseBody != nil {
					data, _ := json.Marshal(tt.responseBody)
					w.Write(data)
				}
			}))
			defer server.Close()

			// Create client
			client := NewKubeletVersionClient(server.URL+"/version", 5, nil)

			// Get version
			ctx := context.Background()
			versionInfo, err := client.GetVersion(ctx)

			// Verify results
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error but got none")
				} else if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error = %v, want error containing %q", err, tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if versionInfo == nil {
					t.Fatal("expected version info but got nil")
				}
				if versionInfo.GitVersion != tt.wantGitVersion {
					t.Errorf("GitVersion = %q, want %q", versionInfo.GitVersion, tt.wantGitVersion)
				}
			}
		})
	}
}

// TestDefaultKubeletVersionClient_GetVersion_Timeout tests timeout handling
func TestDefaultKubeletVersionClient_GetVersion_Timeout(t *testing.T) {
	// Create slow server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		versionInfo := &KubeletVersionInfo{
			Major:      "1",
			Minor:      "28",
			GitVersion: "v1.28.0",
		}
		data, _ := json.Marshal(versionInfo)
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	}))
	defer server.Close()

	// Create client with short timeout (100ms)
	client := &DefaultKubeletVersionClient{
		versionURL: server.URL + "/version",
		timeout:    1,
		client: &http.Client{
			Timeout: 100 * time.Millisecond,
		},
	}

	ctx := context.Background()
	_, err := client.GetVersion(ctx)

	if err == nil {
		t.Error("expected timeout error but got none")
	}
}

// TestDefaultKubeletVersionClient_GetVersion_InvalidJSON tests invalid JSON handling
func TestDefaultKubeletVersionClient_GetVersion_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("invalid json"))
	}))
	defer server.Close()

	client := NewKubeletVersionClient(server.URL+"/version", 5, nil)

	ctx := context.Background()
	_, err := client.GetVersion(ctx)

	if err == nil {
		t.Error("expected JSON parse error but got none")
	}
	if !strings.Contains(err.Error(), "failed to parse version JSON") {
		t.Errorf("error = %v, want error containing 'failed to parse version JSON'", err)
	}
}

// TestDefaultKubeletVersionClient_GetVersion_WithAuth tests authentication
func TestDefaultKubeletVersionClient_GetVersion_WithAuth(t *testing.T) {
	// Mock file reading for ServiceAccount token
	originalReadFile := readFile
	defer func() { readFile = originalReadFile }()
	readFile = func(path string) (string, error) {
		if path == "/var/run/secrets/kubernetes.io/serviceaccount/token" {
			return "test-token-12345", nil
		}
		return "", fmt.Errorf("file not found: %s", path)
	}

	tests := []struct {
		name           string
		auth           *AuthConfig
		wantAuthHeader string
		wantErr        bool
		errContains    string
	}{
		{
			name:           "No authentication",
			auth:           nil,
			wantAuthHeader: "",
			wantErr:        false,
		},
		{
			name: "ServiceAccount authentication",
			auth: &AuthConfig{
				Type:      "serviceaccount",
				TokenFile: "/var/run/secrets/kubernetes.io/serviceaccount/token",
			},
			wantAuthHeader: "Bearer test-token-12345",
			wantErr:        false,
		},
		{
			name: "Bearer token authentication",
			auth: &AuthConfig{
				Type:        "bearer",
				BearerToken: "my-bearer-token",
			},
			wantAuthHeader: "Bearer my-bearer-token",
			wantErr:        false,
		},
		{
			name:           "None authentication type",
			auth:           &AuthConfig{Type: "none"},
			wantAuthHeader: "",
			wantErr:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var receivedAuthHeader string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				receivedAuthHeader = r.Header.Get("Authorization")
				versionInfo := &KubeletVersionInfo{
					Major:      "1",
					Minor:      "28",
					GitVersion: "v1.28.0",
				}
				data, _ := json.Marshal(versionInfo)
				w.WriteHeader(http.StatusOK)
				w.Write(data)
			}))
			defer server.Close()

			client := NewKubeletVersionClient(server.URL+"/version", 5, tt.auth)

			ctx := context.Background()
			_, err := client.GetVersion(ctx)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if receivedAuthHeader != tt.wantAuthHeader {
				t.Errorf("Authorization header = %q, want %q", receivedAuthHeader, tt.wantAuthHeader)
			}
		})
	}
}
