package http

import (
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"github.com/supporttools/node-doctor/pkg/types"
)

func TestNewAuthProvider(t *testing.T) {
	tests := []struct {
		name         string
		config       types.AuthConfig
		expectError  bool
		expectedType string
	}{
		{
			name: "none auth",
			config: types.AuthConfig{
				Type: "none",
			},
			expectError:  false,
			expectedType: "none",
		},
		{
			name:         "empty type defaults to none",
			config:       types.AuthConfig{},
			expectError:  false,
			expectedType: "none",
		},
		{
			name: "valid bearer auth",
			config: types.AuthConfig{
				Type:  "bearer",
				Token: "test-token-123",
			},
			expectError:  false,
			expectedType: "bearer",
		},
		{
			name: "bearer auth missing token",
			config: types.AuthConfig{
				Type: "bearer",
			},
			expectError: true,
		},
		{
			name: "valid basic auth",
			config: types.AuthConfig{
				Type:     "basic",
				Username: "testuser",
				Password: "testpass",
			},
			expectError:  false,
			expectedType: "basic",
		},
		{
			name: "basic auth missing username",
			config: types.AuthConfig{
				Type:     "basic",
				Password: "testpass",
			},
			expectError: true,
		},
		{
			name: "basic auth missing password",
			config: types.AuthConfig{
				Type:     "basic",
				Username: "testuser",
			},
			expectError: true,
		},
		{
			name: "unsupported auth type",
			config: types.AuthConfig{
				Type: "oauth",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewAuthProvider(tt.config)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Expected no error but got: %v", err)
				return
			}

			if provider.Type() != tt.expectedType {
				t.Errorf("Expected type %s, got %s", tt.expectedType, provider.Type())
			}
		})
	}
}

func TestNoAuthProvider(t *testing.T) {
	provider := NewNoAuthProvider()

	if provider.Type() != "none" {
		t.Errorf("Expected type 'none', got %s", provider.Type())
	}

	if err := provider.Validate(); err != nil {
		t.Errorf("Expected no validation error, got: %v", err)
	}

	req, _ := http.NewRequest("POST", "http://example.com", nil)
	if err := provider.AddAuth(req); err != nil {
		t.Errorf("Expected no error adding auth, got: %v", err)
	}

	// Verify no auth headers were added
	if req.Header.Get("Authorization") != "" {
		t.Errorf("Expected no Authorization header")
	}
}

func TestBearerAuthProvider(t *testing.T) {
	token := "test-bearer-token-123"
	provider := NewBearerAuthProvider(token)

	if provider.Type() != "bearer" {
		t.Errorf("Expected type 'bearer', got %s", provider.Type())
	}

	if err := provider.Validate(); err != nil {
		t.Errorf("Expected no validation error, got: %v", err)
	}

	req, _ := http.NewRequest("POST", "http://example.com", nil)
	if err := provider.AddAuth(req); err != nil {
		t.Errorf("Expected no error adding auth, got: %v", err)
	}

	expected := "Bearer " + token
	if req.Header.Get("Authorization") != expected {
		t.Errorf("Expected Authorization header %q, got %q", expected, req.Header.Get("Authorization"))
	}
}

func TestBearerAuthProviderValidation(t *testing.T) {
	tests := []struct {
		name        string
		token       string
		expectError bool
	}{
		{
			name:        "valid token",
			token:       "valid-token-123",
			expectError: false,
		},
		{
			name:        "empty token",
			token:       "",
			expectError: true,
		},
		{
			name:        "token with newline",
			token:       "invalid\ntoken",
			expectError: true,
		},
		{
			name:        "token with carriage return",
			token:       "invalid\rtoken",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewBearerAuthProvider(tt.token)
			err := provider.Validate()

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected validation error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no validation error but got: %v", err)
				}
			}
		})
	}
}

func TestBearerAuthProviderAddAuthErrors(t *testing.T) {
	provider := NewBearerAuthProvider("")
	req, _ := http.NewRequest("POST", "http://example.com", nil)

	err := provider.AddAuth(req)
	if err == nil {
		t.Errorf("Expected error when adding empty token")
	}
}

func TestBasicAuthProvider(t *testing.T) {
	username := "testuser"
	password := "testpass"
	provider := NewBasicAuthProvider(username, password)

	if provider.Type() != "basic" {
		t.Errorf("Expected type 'basic', got %s", provider.Type())
	}

	if err := provider.Validate(); err != nil {
		t.Errorf("Expected no validation error, got: %v", err)
	}

	req, _ := http.NewRequest("POST", "http://example.com", nil)
	if err := provider.AddAuth(req); err != nil {
		t.Errorf("Expected no error adding auth, got: %v", err)
	}

	auth := req.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Basic ") {
		t.Errorf("Expected Authorization header to start with 'Basic ', got %q", auth)
	}

	// Decode and verify
	encoded := strings.TrimPrefix(auth, "Basic ")
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Errorf("Failed to decode basic auth: %v", err)
	}

	expected := username + ":" + password
	if string(decoded) != expected {
		t.Errorf("Expected decoded auth %q, got %q", expected, string(decoded))
	}
}

func TestBasicAuthProviderValidation(t *testing.T) {
	tests := []struct {
		name        string
		username    string
		password    string
		expectError bool
	}{
		{
			name:        "valid credentials",
			username:    "testuser",
			password:    "testpass",
			expectError: false,
		},
		{
			name:        "empty username",
			username:    "",
			password:    "testpass",
			expectError: true,
		},
		{
			name:        "empty password",
			username:    "testuser",
			password:    "",
			expectError: true,
		},
		{
			name:        "username with colon",
			username:    "test:user",
			password:    "testpass",
			expectError: true,
		},
		{
			name:        "username with newline",
			username:    "test\nuser",
			password:    "testpass",
			expectError: true,
		},
		{
			name:        "password with newline",
			username:    "testuser",
			password:    "test\npass",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewBasicAuthProvider(tt.username, tt.password)
			err := provider.Validate()

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected validation error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no validation error but got: %v", err)
				}
			}
		})
	}
}

func TestBasicAuthProviderAddAuthErrors(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
	}{
		{
			name:     "empty username",
			username: "",
			password: "testpass",
		},
		{
			name:     "empty password",
			username: "testuser",
			password: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewBasicAuthProvider(tt.username, tt.password)
			req, _ := http.NewRequest("POST", "http://example.com", nil)

			err := provider.AddAuth(req)
			if err == nil {
				t.Errorf("Expected error when adding auth with invalid credentials")
			}
		})
	}
}

func TestAuthBuilder(t *testing.T) {
	// Test bearer auth
	provider, err := NewAuthBuilder().
		WithType("bearer").
		WithToken("test-token").
		Build()

	if err != nil {
		t.Errorf("Expected no error building bearer auth, got: %v", err)
	}

	if provider.Type() != "bearer" {
		t.Errorf("Expected bearer auth type, got %s", provider.Type())
	}

	// Test basic auth
	provider, err = NewAuthBuilder().
		WithType("basic").
		WithCredentials("user", "pass").
		Build()

	if err != nil {
		t.Errorf("Expected no error building basic auth, got: %v", err)
	}

	if provider.Type() != "basic" {
		t.Errorf("Expected basic auth type, got %s", provider.Type())
	}

	// Test invalid auth
	_, err = NewAuthBuilder().
		WithType("invalid").
		Build()

	if err == nil {
		t.Errorf("Expected error building invalid auth type")
	}
}

func TestValidateAuthConfig(t *testing.T) {
	// Valid config
	config := types.AuthConfig{
		Type:  "bearer",
		Token: "test-token",
	}

	if err := ValidateAuthConfig(config); err != nil {
		t.Errorf("Expected no validation error, got: %v", err)
	}

	// Invalid config
	config = types.AuthConfig{
		Type: "bearer",
		// Missing token
	}

	if err := ValidateAuthConfig(config); err == nil {
		t.Errorf("Expected validation error for missing token")
	}
}
