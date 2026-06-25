// Package network provides network health monitoring capabilities.
package network

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"
)

// httpPinger implements the Pinger interface using HTTP GET requests.
// This is used instead of ICMP on CNI implementations (like Cilium) where
// raw ICMP echo requests to overlay pod IPs are silently dropped.
type httpPinger struct {
	port   int
	path   string
	client *http.Client
	// resolver pre-resolves hostname targets so the reported address family is
	// truthful and the dial is pinned to the resolved address. It is injectable
	// for testing; production uses the package default (system) resolver.
	resolver Resolver
}

// newHTTPPinger creates a new HTTP-based pinger targeting the given port and path.
func newHTTPPinger(port int, path string) Pinger {
	if port == 0 {
		port = defaultProbePort
	}
	if path == "" {
		path = defaultProbePath
	}
	return &httpPinger{
		port:     port,
		path:     path,
		resolver: newDefaultResolver(),
		client: &http.Client{
			// Per-request timeout is set via context; this is a safety net.
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				DisableKeepAlives: true, // Each probe should be independent
				DialContext: (&net.Dialer{
					Timeout: 5 * time.Second,
				}).DialContext,
			},
		},
	}
}

// hostFamily classifies the target as an IPv4 literal, IPv6 literal, or
// hostname. For IP literals it returns the matching family constant; for
// hostnames family is empty and the resolver will pick a family at dial time.
func hostFamily(target string) string {
	ip := net.ParseIP(target)
	if ip == nil {
		return ""
	}
	if ip.To4() != nil {
		return FamilyIPv4
	}
	return FamilyIPv6
}

// Ping sends HTTP GET requests to the target IP and returns results.
// It follows the same contract as the ICMP pinger: sends `count` probes
// with 100ms inter-probe delay, measuring RTT for each.
func (p *httpPinger) Ping(ctx context.Context, target string, count int, timeout time.Duration) ([]PingResult, error) {
	results := make([]PingResult, 0, count)

	// The URL host always remains the original target (hostname or IP literal)
	// so TLS/vhost routing keeps working; only the dial target IP is pinned.
	url := "http://" + net.JoinHostPort(target, strconv.Itoa(p.port)) + p.path

	// family is the reported address family. For IP literals it is derived
	// directly from the literal. For hostnames it is empty until resolution.
	family := hostFamily(target)

	// client is the HTTP client used for probes. For hostname targets we build
	// a per-target client whose DialContext is pinned to the resolved address so
	// the emitted family is accurate and matches the connection actually made.
	client := p.client

	if family == "" {
		// target is a hostname: pre-resolve to determine the true address family
		// and pin the dial to the chosen address.
		if resolved, resolvedFamily, ok := p.resolveTarget(ctx, target); ok {
			family = resolvedFamily
			client = p.pinnedClient(resolved)
		}
		// On resolution failure we fall through with the original URL/client and
		// empty family (graceful fallback): a resolvable-but-unreachable host is
		// not turned into a resolution error; only the DNS step failing here
		// leaves family empty and lets the probe surface its own error.
	}

	for i := 0; i < count; i++ {
		// Check context before each probe
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		result := p.singleProbe(ctx, client, url, family, timeout)
		results = append(results, result)

		// 100ms delay between probes (same as ICMP pinger)
		if i < count-1 {
			select {
			case <-ctx.Done():
				return results, ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
	}

	return results, nil
}

// resolveTarget resolves a hostname target to a single address and reports the
// address family to emit on results. Selection policy: prefer the first IPv4
// address (matching the existing pinger's hostname IPv4-preference); if only
// IPv6 addresses are returned, use the first IPv6 address. The returned ok is
// false when resolution fails or yields no usable address, in which case the
// caller falls back to the original (unpinned) behavior.
func (p *httpPinger) resolveTarget(ctx context.Context, host string) (ip net.IP, family string, ok bool) {
	ips, err := p.resolver.LookupIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return nil, "", false
	}

	var firstV6 net.IP
	for _, candidate := range ips {
		if candidate.To4() != nil {
			// Prefer IPv4: return immediately on the first IPv4 address.
			return candidate, FamilyIPv4, true
		}
		if firstV6 == nil {
			firstV6 = candidate
		}
	}

	if firstV6 != nil {
		return firstV6, FamilyIPv6, true
	}
	return nil, "", false
}

// pinnedClient returns an HTTP client that dials the given resolved IP for every
// connection while preserving the requested port. It clones the base client's
// transport settings (timeouts, keep-alive behavior) and only overrides the
// dial target, so the URL host header is unaffected.
func (p *httpPinger) pinnedClient(ip net.IP) *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	pinnedAddr := ip.String()

	transport := &http.Transport{
		DisableKeepAlives: true, // Each probe should be independent.
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// Replace the host with the resolved IP, preserving the original
			// port. net.JoinHostPort handles IPv6 bracketing correctly.
			_, port, err := net.SplitHostPort(addr)
			if err != nil {
				port = strconv.Itoa(p.port)
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(pinnedAddr, port))
		},
	}

	return &http.Client{
		Timeout:   p.client.Timeout,
		Transport: transport,
	}
}

// singleProbe performs a single HTTP GET using the given client and measures RTT.
func (p *httpPinger) singleProbe(ctx context.Context, client *http.Client, url, family string, timeout time.Duration) PingResult {
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return PingResult{
			Success: false,
			Error:   fmt.Errorf("failed to create request: %w", err),
			Family:  family,
		}
	}

	start := time.Now()
	resp, err := client.Do(req)
	rtt := time.Since(start)

	if err != nil {
		return PingResult{
			Success: false,
			Error:   fmt.Errorf("HTTP probe failed: %w", err),
			Family:  family,
		}
	}
	defer func() {
		// Drain the body to allow TCP connection to close cleanly.
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1024)) //nolint:errcheck
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return PingResult{
			Success: false,
			Error:   fmt.Errorf("HTTP probe returned status %d", resp.StatusCode),
			Family:  family,
		}
	}

	return PingResult{
		Success: true,
		RTT:     rtt,
		Family:  family,
	}
}
