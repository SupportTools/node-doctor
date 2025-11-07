// Package network provides network health monitoring capabilities.
package network

import (
	"context"
	"net"
)

// Resolver is an interface for DNS resolution operations.
// This interface allows for mocking DNS resolution in tests.
type Resolver interface {
	// LookupHost looks up the given host using the local resolver.
	// It returns a slice of that host's addresses.
	LookupHost(ctx context.Context, host string) ([]string, error)

	// LookupAddr performs a reverse lookup for the given address,
	// returning a list of names mapping to that address.
	LookupAddr(ctx context.Context, addr string) ([]string, error)
}

// defaultResolver wraps the standard library's net.Resolver.
type defaultResolver struct {
	resolver *net.Resolver
}

// newDefaultResolver creates a new default resolver that uses the system DNS configuration.
func newDefaultResolver() Resolver {
	return &defaultResolver{
		resolver: net.DefaultResolver,
	}
}

// LookupHost implements the Resolver interface using net.Resolver.
func (r *defaultResolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	return r.resolver.LookupHost(ctx, host)
}

// LookupAddr implements the Resolver interface using net.Resolver.
func (r *defaultResolver) LookupAddr(ctx context.Context, addr string) ([]string, error) {
	return r.resolver.LookupAddr(ctx, addr)
}
