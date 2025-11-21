package kubernetes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestAuthConfig_Validate tests the validation of authentication configuration.
func TestAuthConfig_Validate(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := t.TempDir()

	// Create test certificate and key files
	certFile := filepath.Join(tmpDir, "client.crt")
	keyFile := filepath.Join(tmpDir, "client.key")
	tokenFile := filepath.Join(tmpDir, "token")

	// Write minimal test files
	if err := os.WriteFile(certFile, []byte("cert"), 0644); err != nil {
		t.Fatalf("failed to create cert file: %v", err)
	}
	if err := os.WriteFile(keyFile, []byte("key"), 0600); err != nil {
		t.Fatalf("failed to create key file: %v", err)
	}
	if err := os.WriteFile(tokenFile, []byte("test-token"), 0644); err != nil {
		t.Fatalf("failed to create token file: %v", err)
	}

	tests := []struct {
		name      string
		config    *AuthConfig
		expectErr bool
		errMsg    string
	}{
		{
			name:      "nil config",
			config:    nil,
			expectErr: false,
		},
		{
			name: "valid none type",
			config: &AuthConfig{
				Type: "none",
			},
			expectErr: false,
		},
		{
			name: "valid serviceaccount with default token",
			config: &AuthConfig{
				Type: "serviceaccount",
			},
			expectErr: false,
		},
		{
			name: "valid serviceaccount with custom token file",
			config: &AuthConfig{
				Type:      "serviceaccount",
				TokenFile: tokenFile,
			},
			expectErr: false,
		},
		{
			name: "serviceaccount with non-existent token file",
			config: &AuthConfig{
				Type:      "serviceaccount",
				TokenFile: "/nonexistent/token",
			},
			expectErr: true,
			errMsg:    "tokenFile does not exist",
		},
		{
			name: "valid bearer token",
			config: &AuthConfig{
				Type:        "bearer",
				BearerToken: "test-bearer-token",
			},
			expectErr: false,
		},
		{
			name: "bearer token missing token",
			config: &AuthConfig{
				Type: "bearer",
			},
			expectErr: true,
			errMsg:    "bearerToken is required",
		},
		{
			name: "valid certificate",
			config: &AuthConfig{
				Type:     "certificate",
				CertFile: certFile,
				KeyFile:  keyFile,
			},
			expectErr: false,
		},
		{
			name: "certificate missing cert file",
			config: &AuthConfig{
				Type:    "certificate",
				KeyFile: keyFile,
			},
			expectErr: true,
			errMsg:    "certFile is required",
		},
		{
			name: "certificate missing key file",
			config: &AuthConfig{
				Type:     "certificate",
				CertFile: certFile,
			},
			expectErr: true,
			errMsg:    "keyFile is required",
		},
		{
			name: "certificate with non-existent cert file",
			config: &AuthConfig{
				Type:     "certificate",
				CertFile: "/nonexistent/cert",
				KeyFile:  keyFile,
			},
			expectErr: true,
			errMsg:    "certFile does not exist",
		},
		{
			name: "certificate with non-existent key file",
			config: &AuthConfig{
				Type:     "certificate",
				CertFile: certFile,
				KeyFile:  "/nonexistent/key",
			},
			expectErr: true,
			errMsg:    "keyFile does not exist",
		},
		{
			name: "invalid auth type",
			config: &AuthConfig{
				Type: "invalid",
			},
			expectErr: true,
			errMsg:    "invalid auth type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.validate()
			if tt.expectErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errMsg)
					return
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestParseAuthConfig tests parsing of authentication configuration.
func TestParseAuthConfig(t *testing.T) {
	tests := []struct {
		name      string
		configMap map[string]interface{}
		expectErr bool
		errMsg    string
		expected  *AuthConfig
	}{
		{
			name:      "nil config",
			configMap: nil,
			expected:  nil,
		},
		{
			name:      "empty config",
			configMap: map[string]interface{}{},
			expected:  &AuthConfig{},
		},
		{
			name: "valid serviceaccount config",
			configMap: map[string]interface{}{
				"type":      "serviceaccount",
				"tokenFile": "/custom/path/token",
			},
			expected: &AuthConfig{
				Type:      "serviceaccount",
				TokenFile: "/custom/path/token",
			},
		},
		{
			name: "valid bearer config",
			configMap: map[string]interface{}{
				"type":        "bearer",
				"bearerToken": "test-token-123",
			},
			expected: &AuthConfig{
				Type:        "bearer",
				BearerToken: "test-token-123",
			},
		},
		{
			name: "valid certificate config",
			configMap: map[string]interface{}{
				"type":     "certificate",
				"certFile": "/path/to/cert",
				"keyFile":  "/path/to/key",
			},
			expected: &AuthConfig{
				Type:     "certificate",
				CertFile: "/path/to/cert",
				KeyFile:  "/path/to/key",
			},
		},
		{
			name: "config with insecureSkipVerify",
			configMap: map[string]interface{}{
				"type":               "bearer",
				"bearerToken":        "test-token",
				"insecureSkipVerify": true,
			},
			expected: &AuthConfig{
				Type:               "bearer",
				BearerToken:        "test-token",
				InsecureSkipVerify: true,
			},
		},
		{
			name: "invalid type field type",
			configMap: map[string]interface{}{
				"type": 123,
			},
			expectErr: true,
			errMsg:    "type must be a string",
		},
		{
			name: "invalid tokenFile field type",
			configMap: map[string]interface{}{
				"tokenFile": 123,
			},
			expectErr: true,
			errMsg:    "tokenFile must be a string",
		},
		{
			name: "invalid bearerToken field type",
			configMap: map[string]interface{}{
				"bearerToken": 123,
			},
			expectErr: true,
			errMsg:    "bearerToken must be a string",
		},
		{
			name: "invalid certFile field type",
			configMap: map[string]interface{}{
				"certFile": 123,
			},
			expectErr: true,
			errMsg:    "certFile must be a string",
		},
		{
			name: "invalid keyFile field type",
			configMap: map[string]interface{}{
				"keyFile": 123,
			},
			expectErr: true,
			errMsg:    "keyFile must be a string",
		},
		{
			name: "invalid insecureSkipVerify field type",
			configMap: map[string]interface{}{
				"insecureSkipVerify": "true",
			},
			expectErr: true,
			errMsg:    "insecureSkipVerify must be a boolean",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseAuthConfig(tt.configMap)
			if tt.expectErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errMsg)
					return
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
					return
				}
				if tt.expected == nil {
					if result != nil {
						t.Errorf("expected nil, got %+v", result)
					}
				} else {
					if result == nil {
						t.Errorf("expected %+v, got nil", tt.expected)
						return
					}
					if result.Type != tt.expected.Type {
						t.Errorf("Type: expected %q, got %q", tt.expected.Type, result.Type)
					}
					if result.TokenFile != tt.expected.TokenFile {
						t.Errorf("TokenFile: expected %q, got %q", tt.expected.TokenFile, result.TokenFile)
					}
					if result.BearerToken != tt.expected.BearerToken {
						t.Errorf("BearerToken: expected %q, got %q", tt.expected.BearerToken, result.BearerToken)
					}
					if result.CertFile != tt.expected.CertFile {
						t.Errorf("CertFile: expected %q, got %q", tt.expected.CertFile, result.CertFile)
					}
					if result.KeyFile != tt.expected.KeyFile {
						t.Errorf("KeyFile: expected %q, got %q", tt.expected.KeyFile, result.KeyFile)
					}
					if result.InsecureSkipVerify != tt.expected.InsecureSkipVerify {
						t.Errorf("InsecureSkipVerify: expected %v, got %v", tt.expected.InsecureSkipVerify, result.InsecureSkipVerify)
					}
				}
			}
		})
	}
}

// TestAddAuthHeader tests adding authentication headers to HTTP requests.
func TestAddAuthHeader(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := t.TempDir()

	// Create test token file
	tokenFile := filepath.Join(tmpDir, "token")
	tokenContent := "test-service-account-token"
	if err := os.WriteFile(tokenFile, []byte(tokenContent), 0644); err != nil {
		t.Fatalf("failed to create token file: %v", err)
	}

	tests := []struct {
		name           string
		auth           *AuthConfig
		expectErr      bool
		errMsg         string
		expectedHeader string
	}{
		{
			name:           "no auth",
			auth:           nil,
			expectedHeader: "",
		},
		{
			name: "none type",
			auth: &AuthConfig{
				Type: "none",
			},
			expectedHeader: "",
		},
		{
			name: "serviceaccount with custom token file",
			auth: &AuthConfig{
				Type:      "serviceaccount",
				TokenFile: tokenFile,
			},
			expectedHeader: "Bearer " + tokenContent,
		},
		{
			name: "serviceaccount with non-existent token file",
			auth: &AuthConfig{
				Type:      "serviceaccount",
				TokenFile: "/nonexistent/token",
			},
			expectErr: true,
			errMsg:    "failed to read ServiceAccount token",
		},
		{
			name: "bearer token",
			auth: &AuthConfig{
				Type:        "bearer",
				BearerToken: "my-custom-bearer-token",
			},
			expectedHeader: "Bearer my-custom-bearer-token",
		},
		{
			name: "bearer token without token",
			auth: &AuthConfig{
				Type: "bearer",
			},
			expectErr: true,
			errMsg:    "bearerToken is required",
		},
		{
			name: "certificate type (no header)",
			auth: &AuthConfig{
				Type: "certificate",
			},
			expectedHeader: "",
		},
		{
			name: "invalid auth type",
			auth: &AuthConfig{
				Type: "invalid",
			},
			expectErr: true,
			errMsg:    "unsupported authentication type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &defaultKubeletClient{
				healthzURL: "http://localhost:10248/healthz",
				metricsURL: "http://localhost:10250/metrics",
				timeout:    5 * time.Second,
				auth:       tt.auth,
			}

			req, err := http.NewRequest("GET", client.healthzURL, nil)
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}

			err = client.addAuthHeader(req)
			if tt.expectErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errMsg)
					return
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
					return
				}
				authHeader := req.Header.Get("Authorization")
				if authHeader != tt.expectedHeader {
					t.Errorf("expected Authorization header %q, got %q", tt.expectedHeader, authHeader)
				}
			}
		})
	}
}

// TestKubeletClient_WithAuthentication tests end-to-end authentication with a mock server.
func TestKubeletClient_WithAuthentication(t *testing.T) {
	tests := []struct {
		name          string
		auth          *AuthConfig
		serverHandler http.HandlerFunc
		expectErr     bool
		errMsg        string
	}{
		{
			name: "no auth required",
			auth: &AuthConfig{
				Type: "none",
			},
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
			expectErr: false,
		},
		{
			name: "bearer token accepted",
			auth: &AuthConfig{
				Type:        "bearer",
				BearerToken: "valid-token",
			},
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				authHeader := r.Header.Get("Authorization")
				if authHeader != "Bearer valid-token" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				w.WriteHeader(http.StatusOK)
			},
			expectErr: false,
		},
		{
			name: "bearer token rejected",
			auth: &AuthConfig{
				Type:        "bearer",
				BearerToken: "invalid-token",
			},
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				authHeader := r.Header.Get("Authorization")
				if authHeader != "Bearer valid-token" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				w.WriteHeader(http.StatusOK)
			},
			expectErr: true,
			errMsg:    "401",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock server
			server := httptest.NewServer(tt.serverHandler)
			defer server.Close()

			config := &KubeletMonitorConfig{
				HealthzURL:  server.URL,
				MetricsURL:  server.URL,
				HTTPTimeout: 5 * time.Second,
				Auth:        tt.auth,
			}

			client := newDefaultKubeletClient(config)

			// Test health check
			ctx := context.Background()
			err := client.CheckHealth(ctx)

			if tt.expectErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errMsg)
					return
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestConfigureTLS tests TLS configuration.
func TestConfigureTLS(t *testing.T) {
	tests := []struct {
		name      string
		auth      *AuthConfig
		expectNil bool
		expectErr bool
	}{
		{
			name:      "nil auth",
			auth:      nil,
			expectNil: true,
		},
		{
			name: "no tls config needed",
			auth: &AuthConfig{
				Type:        "bearer",
				BearerToken: "token",
			},
			expectNil: false,
		},
		{
			name: "insecure skip verify",
			auth: &AuthConfig{
				Type:               "bearer",
				BearerToken:        "token",
				InsecureSkipVerify: true,
			},
			expectNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tlsConfig, err := configureTLS(tt.auth)
			if tt.expectErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if tt.expectNil {
				if tlsConfig != nil {
					t.Errorf("expected nil TLS config, got %+v", tlsConfig)
				}
			} else {
				if tlsConfig == nil {
					t.Errorf("expected non-nil TLS config, got nil")
					return
				}
				// Verify InsecureSkipVerify setting
				if tt.auth.InsecureSkipVerify != tlsConfig.InsecureSkipVerify {
					t.Errorf("InsecureSkipVerify: expected %v, got %v",
						tt.auth.InsecureSkipVerify, tlsConfig.InsecureSkipVerify)
				}
			}
		})
	}
}
