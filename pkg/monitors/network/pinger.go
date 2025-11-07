// Package network provides network health monitoring capabilities.
package network

import (
	"context"
	"fmt"
	"net"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// PingResult represents the result of a single ping operation.
type PingResult struct {
	// Success indicates whether the ping succeeded.
	Success bool
	// RTT is the round-trip time for successful pings.
	RTT time.Duration
	// Error contains the error if the ping failed.
	Error error
}

// Pinger is an interface for ICMP ping operations.
// This interface allows for mocking ping operations in tests.
type Pinger interface {
	// Ping sends ICMP echo requests to the target IP address.
	// It returns a slice of results, one for each ping attempt.
	Ping(ctx context.Context, target string, count int, timeout time.Duration) ([]PingResult, error)
}

// defaultPinger implements the Pinger interface using ICMP packets.
type defaultPinger struct {
	// No state needed for default implementation
}

// newDefaultPinger creates a new default pinger that uses ICMP echo requests.
func newDefaultPinger() Pinger {
	return &defaultPinger{}
}

// Ping sends ICMP echo requests to the target IP address.
// It returns a slice of PingResult, one for each ping attempt.
func (p *defaultPinger) Ping(ctx context.Context, target string, count int, timeout time.Duration) ([]PingResult, error) {
	// Resolve target to IP address
	ip := net.ParseIP(target)
	if ip == nil {
		// Try to resolve as hostname
		ips, err := net.LookupIP(target)
		if err != nil || len(ips) == 0 {
			return nil, fmt.Errorf("failed to resolve target %s: %w", target, err)
		}
		// Use first IPv4 address
		for _, resolvedIP := range ips {
			if resolvedIP.To4() != nil {
				ip = resolvedIP
				break
			}
		}
		if ip == nil {
			return nil, fmt.Errorf("no IPv4 address found for target %s", target)
		}
	}

	// Create ICMP listener
	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return nil, fmt.Errorf("failed to create ICMP listener (may require elevated privileges): %w", err)
	}
	defer conn.Close()

	results := make([]PingResult, 0, count)

	for i := 0; i < count; i++ {
		// Check context before each ping
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		result := p.singlePing(ctx, conn, ip, timeout)
		results = append(results, result)

		// Small delay between pings (100ms)
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

// singlePing performs a single ICMP echo request/reply.
func (p *defaultPinger) singlePing(ctx context.Context, conn *icmp.PacketConn, ip net.IP, timeout time.Duration) PingResult {
	// Create ICMP echo request message
	msg := icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID:   1, // Process ID would be better but keeping it simple
			Seq:  1,
			Data: []byte("node-doctor-gateway-ping"),
		},
	}

	msgBytes, err := msg.Marshal(nil)
	if err != nil {
		return PingResult{
			Success: false,
			Error:   fmt.Errorf("failed to marshal ICMP message: %w", err),
		}
	}

	// Set deadline for this ping
	deadline := time.Now().Add(timeout)
	if err := conn.SetDeadline(deadline); err != nil {
		return PingResult{
			Success: false,
			Error:   fmt.Errorf("failed to set deadline: %w", err),
		}
	}

	// Send echo request
	start := time.Now()
	_, err = conn.WriteTo(msgBytes, &net.IPAddr{IP: ip})
	if err != nil {
		return PingResult{
			Success: false,
			Error:   fmt.Errorf("failed to send ICMP echo request: %w", err),
		}
	}

	// Wait for echo reply
	reply := make([]byte, 1500)
	n, peer, err := conn.ReadFrom(reply)
	rtt := time.Since(start)

	if err != nil {
		return PingResult{
			Success: false,
			Error:   fmt.Errorf("failed to receive ICMP echo reply: %w", err),
		}
	}

	// Parse reply
	replyMsg, err := icmp.ParseMessage(1, reply[:n]) // 1 = ICMP protocol for IPv4
	if err != nil {
		return PingResult{
			Success: false,
			Error:   fmt.Errorf("failed to parse ICMP reply: %w", err),
		}
	}

	// Verify it's an echo reply
	if replyMsg.Type != ipv4.ICMPTypeEchoReply {
		return PingResult{
			Success: false,
			Error:   fmt.Errorf("received unexpected ICMP type: %v", replyMsg.Type),
		}
	}

	// Verify it's from the target IP
	if peer.String() != ip.String() {
		return PingResult{
			Success: false,
			Error:   fmt.Errorf("received reply from unexpected host: %s (expected %s)", peer.String(), ip.String()),
		}
	}

	return PingResult{
		Success: true,
		RTT:     rtt,
		Error:   nil,
	}
}
