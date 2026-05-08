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
		port: port,
		path: path,
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

	family := hostFamily(target)
	url := "http://" + net.JoinHostPort(target, strconv.Itoa(p.port)) + p.path

	for i := 0; i < count; i++ {
		// Check context before each probe
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		result := p.singleProbe(ctx, url, family, timeout)
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

// singleProbe performs a single HTTP GET and measures RTT.
func (p *httpPinger) singleProbe(ctx context.Context, url, family string, timeout time.Duration) PingResult {
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
	resp, err := p.client.Do(req)
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
