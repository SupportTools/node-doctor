// Package network provides network health monitoring capabilities.
package network

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net"
	"sync/atomic"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// pingSequence is a global counter for ICMP sequence numbers
var pingSequence uint32

// pingID is a random ID generated at startup to avoid collisions with other processes.
// Using math/rand is acceptable here - cryptographic randomness is not required for ICMP ping IDs.
var pingID = uint16(rand.Uint32()) //nolint:gosec // ping ID doesn't require crypto/rand

// PingResult represents the result of a single ping operation.
type PingResult struct {
	// Success indicates whether the ping succeeded.
	Success bool
	// RTT is the round-trip time for successful pings.
	RTT time.Duration
	// Error contains the error if the ping failed.
	Error error
}

// Pinger is an interface for connectivity probe operations (ICMP or HTTP).
// This interface allows for mocking probe operations in tests.
type Pinger interface {
	// Ping sends connectivity probes to the target IP address.
	// It returns a slice of results, one for each probe attempt.
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
	// Use unique sequence number for each ping to avoid collisions
	seq := uint16(atomic.AddUint32(&pingSequence, 1) & 0xFFFF)

	// Create ICMP echo request message with unique ID and sequence
	msg := icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID:   int(pingID),
			Seq:  int(seq),
			Data: []byte("node-doctor-ping"),
		},
	}

	msgBytes, err := msg.Marshal(nil)
	if err != nil {
		log.Printf("[DEBUG] Ping to %s: failed to marshal ICMP message: %v", ip, err)
		return PingResult{
			Success: false,
			Error:   fmt.Errorf("failed to marshal ICMP message: %w", err),
		}
	}

	// Set deadline for this ping
	deadline := time.Now().Add(timeout)
	if err := conn.SetDeadline(deadline); err != nil {
		log.Printf("[DEBUG] Ping to %s: failed to set deadline: %v", ip, err)
		return PingResult{
			Success: false,
			Error:   fmt.Errorf("failed to set deadline: %w", err),
		}
	}

	// Send echo request
	start := time.Now()
	_, err = conn.WriteTo(msgBytes, &net.IPAddr{IP: ip})
	if err != nil {
		log.Printf("[DEBUG] Ping to %s: failed to send: %v", ip, err)
		return PingResult{
			Success: false,
			Error:   fmt.Errorf("failed to send ICMP echo request: %w", err),
		}
	}

	// Wait for echo reply - loop to handle receiving other processes' ICMP replies
	reply := make([]byte, 1500)
	for {
		// Check context
		select {
		case <-ctx.Done():
			return PingResult{
				Success: false,
				Error:   ctx.Err(),
			}
		default:
		}

		n, peer, err := conn.ReadFrom(reply)
		rtt := time.Since(start)

		if err != nil {
			// Only log actual failures (usually timeout)
			log.Printf("[DEBUG] Ping to %s: timeout after %v: %v", ip, rtt, err)
			return PingResult{
				Success: false,
				Error:   fmt.Errorf("failed to receive ICMP echo reply: %w", err),
			}
		}

		// Parse reply
		replyMsg, err := icmp.ParseMessage(1, reply[:n]) // 1 = ICMP protocol for IPv4
		if err != nil {
			log.Printf("[DEBUG] Ping to %s: failed to parse reply from %s: %v", ip, peer.String(), err)
			return PingResult{
				Success: false,
				Error:   fmt.Errorf("failed to parse ICMP reply: %w", err),
			}
		}

		// Skip non-echo-reply messages (e.g., destination unreachable)
		if replyMsg.Type != ipv4.ICMPTypeEchoReply {
			continue
		}

		// Verify it's from the target IP
		if peer.String() != ip.String() {
			continue
		}

		// Verify ID and Seq match our request
		echoReply, ok := replyMsg.Body.(*icmp.Echo)
		if !ok {
			continue
		}

		if echoReply.ID != int(pingID) || echoReply.Seq != int(seq) {
			continue
		}

		return PingResult{
			Success: true,
			RTT:     rtt,
			Error:   nil,
		}
	}
}
