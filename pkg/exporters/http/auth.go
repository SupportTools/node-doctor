package http

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"github.com/supporttools/node-doctor/pkg/types"
)

// AuthProvider interface for different authentication methods
type AuthProvider interface {
	// AddAuth adds authentication headers to the HTTP request
	AddAuth(req *http.Request) error

	// Validate checks if the auth configuration is valid
	Validate() error

	// Type returns the authentication type
	Type() string
}

// NoAuthProvider implements no authentication
type NoAuthProvider struct{}

// NewNoAuthProvider creates a new no-auth provider
func NewNoAuthProvider() *NoAuthProvider {
	return &NoAuthProvider{}
}

// AddAuth adds no authentication (no-op)
func (p *NoAuthProvider) AddAuth(req *http.Request) error {
	// No authentication required
	return nil
}

// Validate validates the no-auth configuration (always valid)
func (p *NoAuthProvider) Validate() error {
	return nil
}

// Type returns the authentication type
func (p *NoAuthProvider) Type() string {
	return "none"
}

// BearerAuthProvider implements Bearer token authentication
type BearerAuthProvider struct {
	token string
}

// NewBearerAuthProvider creates a new Bearer auth provider
func NewBearerAuthProvider(token string) *BearerAuthProvider {
	return &BearerAuthProvider{
		token: token,
	}
}

// AddAuth adds Bearer token authentication to the request
func (p *BearerAuthProvider) AddAuth(req *http.Request) error {
	if p.token == "" {
		return fmt.Errorf("bearer token is empty")
	}

	req.Header.Set("Authorization", "Bearer "+p.token)
	return nil
}

// Validate validates the Bearer auth configuration
func (p *BearerAuthProvider) Validate() error {
	if p.token == "" {
		return fmt.Errorf("bearer token cannot be empty")
	}

	// Basic token format validation
	if strings.ContainsAny(p.token, "\r\n") {
		return fmt.Errorf("bearer token contains invalid characters")
	}

	return nil
}

// Type returns the authentication type
func (p *BearerAuthProvider) Type() string {
	return "bearer"
}

// BasicAuthProvider implements HTTP Basic authentication
type BasicAuthProvider struct {
	username string
	password string
}

// NewBasicAuthProvider creates a new Basic auth provider
func NewBasicAuthProvider(username, password string) *BasicAuthProvider {
	return &BasicAuthProvider{
		username: username,
		password: password,
	}
}

// AddAuth adds Basic authentication to the request
func (p *BasicAuthProvider) AddAuth(req *http.Request) error {
	if p.username == "" {
		return fmt.Errorf("basic auth username is empty")
	}
	if p.password == "" {
		return fmt.Errorf("basic auth password is empty")
	}

	// Create the Basic auth header value
	auth := p.username + ":" + p.password
	encoded := base64.StdEncoding.EncodeToString([]byte(auth))
	req.Header.Set("Authorization", "Basic "+encoded)

	return nil
}

// Validate validates the Basic auth configuration
func (p *BasicAuthProvider) Validate() error {
	if p.username == "" {
		return fmt.Errorf("basic auth username cannot be empty")
	}
	if p.password == "" {
		return fmt.Errorf("basic auth password cannot be empty")
	}

	// Basic format validation
	if strings.ContainsAny(p.username, ":\r\n") {
		return fmt.Errorf("username contains invalid characters")
	}
	if strings.ContainsAny(p.password, "\r\n") {
		return fmt.Errorf("password contains invalid characters")
	}

	return nil
}

// Type returns the authentication type
func (p *BasicAuthProvider) Type() string {
	return "basic"
}

// NewAuthProvider creates an appropriate auth provider based on configuration
func NewAuthProvider(config types.AuthConfig) (AuthProvider, error) {
	switch config.Type {
	case "none", "":
		return NewNoAuthProvider(), nil

	case "bearer":
		if config.Token == "" {
			return nil, fmt.Errorf("bearer token is required for bearer auth")
		}
		provider := NewBearerAuthProvider(config.Token)
		if err := provider.Validate(); err != nil {
			return nil, fmt.Errorf("bearer auth validation failed: %w", err)
		}
		return provider, nil

	case "basic":
		if config.Username == "" {
			return nil, fmt.Errorf("username is required for basic auth")
		}
		if config.Password == "" {
			return nil, fmt.Errorf("password is required for basic auth")
		}
		provider := NewBasicAuthProvider(config.Username, config.Password)
		if err := provider.Validate(); err != nil {
			return nil, fmt.Errorf("basic auth validation failed: %w", err)
		}
		return provider, nil

	default:
		return nil, fmt.Errorf("unsupported auth type: %s", config.Type)
	}
}

// AuthBuilder helps build auth providers with validation
type AuthBuilder struct {
	authType string
	token    string
	username string
	password string
}

// NewAuthBuilder creates a new auth builder
func NewAuthBuilder() *AuthBuilder {
	return &AuthBuilder{}
}

// WithType sets the authentication type
func (b *AuthBuilder) WithType(authType string) *AuthBuilder {
	b.authType = authType
	return b
}

// WithToken sets the bearer token
func (b *AuthBuilder) WithToken(token string) *AuthBuilder {
	b.token = token
	return b
}

// WithCredentials sets the basic auth credentials
func (b *AuthBuilder) WithCredentials(username, password string) *AuthBuilder {
	b.username = username
	b.password = password
	return b
}

// Build creates the auth provider with validation
func (b *AuthBuilder) Build() (AuthProvider, error) {
	config := types.AuthConfig{
		Type:     b.authType,
		Token:    b.token,
		Username: b.username,
		Password: b.password,
	}

	return NewAuthProvider(config)
}

// ValidateAuthConfig validates an auth configuration without creating a provider
func ValidateAuthConfig(config types.AuthConfig) error {
	_, err := NewAuthProvider(config)
	return err
}
