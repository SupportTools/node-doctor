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

	// LookupIP looks up host using the local resolver, restricted to the
	// given network ("ip" for both, "ip4" for A records only, "ip6" for
	// AAAA records only). It enables explicit per-family DNS probing.
	LookupIP(ctx context.Context, network, host string) ([]net.IP, error)
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

// LookupIP implements the Resolver interface using net.Resolver.
// network must be "ip", "ip4", or "ip6" — see net.Resolver.LookupIP for details.
func (r *defaultResolver) LookupIP(ctx context.Context, network, host string) ([]net.IP, error) {
	return r.resolver.LookupIP(ctx, network, host)
}
