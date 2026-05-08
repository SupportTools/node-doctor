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
	"golang.org/x/net/ipv6"
)

// Address family identifiers attached to probe results so downstream
// consumers (exporters, correlators, dashboards) can distinguish IPv4
// from IPv6 connectivity outcomes.
const (
	FamilyIPv4 = "ipv4"
	FamilyIPv6 = "ipv6"
)

// ICMP protocol numbers used by golang.org/x/net/icmp.ParseMessage.
const (
	protocolICMPv4 = 1
	protocolICMPv6 = 58
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
	// Family is the address family used for the probe ("ipv4" or "ipv6").
	// Empty when the probe never selected a family (e.g. resolution failure).
	Family string
}

// Pinger is an interface for connectivity probe operations (ICMP or HTTP).
// This interface allows for mocking probe operations in tests.
type Pinger interface {
	// Ping sends connectivity probes to the target IP address.
	// It returns a slice of results, one for each probe attempt.
	Ping(ctx context.Context, target string, count int, timeout time.Duration) ([]PingResult, error)
}

// defaultPinger implements the Pinger interface using ICMP packets.
// It supports both IPv4 (ICMP) and IPv6 (ICMPv6) probes, dispatching
// based on the resolved target address family.
type defaultPinger struct {
	// No state needed for default implementation
}

// newDefaultPinger creates a new default pinger that uses ICMP echo requests.
func newDefaultPinger() Pinger {
	return &defaultPinger{}
}

// resolveTarget parses the target as an IP literal, or resolves it as a
// hostname. Returns the chosen IP and family. When the target is a hostname,
// IPv4 is preferred for backward compatibility; if no IPv4 address is
// available, the first IPv6 address is used.
func resolveTarget(target string) (net.IP, string, error) {
	if ip := net.ParseIP(target); ip != nil {
		if ip.To4() != nil {
			return ip.To4(), FamilyIPv4, nil
		}
		return ip, FamilyIPv6, nil
	}

	ips, err := net.LookupIP(target)
	if err != nil || len(ips) == 0 {
		return nil, "", fmt.Errorf("failed to resolve target %s: %w", target, err)
	}

	for _, resolvedIP := range ips {
		if resolvedIP.To4() != nil {
			return resolvedIP.To4(), FamilyIPv4, nil
		}
	}
	for _, resolvedIP := range ips {
		if resolvedIP.To16() != nil {
			return resolvedIP, FamilyIPv6, nil
		}
	}

	return nil, "", fmt.Errorf("no usable IP address found for target %s", target)
}

// listenICMP opens an ICMP packet connection for the given address family
// and returns it together with the protocol number used for parsing replies
// and the ICMP message type that represents an echo request.
func listenICMP(family string) (*icmp.PacketConn, int, icmp.Type, error) {
	switch family {
	case FamilyIPv4:
		conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
		if err != nil {
			return nil, 0, nil, fmt.Errorf("failed to create IPv4 ICMP listener (may require elevated privileges): %w", err)
		}
		return conn, protocolICMPv4, ipv4.ICMPTypeEcho, nil
	case FamilyIPv6:
		conn, err := icmp.ListenPacket("ip6:ipv6-icmp", "::")
		if err != nil {
			return nil, 0, nil, fmt.Errorf("failed to create IPv6 ICMP listener (may require elevated privileges): %w", err)
		}
		return conn, protocolICMPv6, ipv6.ICMPTypeEchoRequest, nil
	default:
		return nil, 0, nil, fmt.Errorf("unsupported address family %q", family)
	}
}

// isEchoReply reports whether the given parsed ICMP message is an echo reply
// for the address family selected. Other ICMP message types (e.g. destination
// unreachable, neighbor discovery on v6) are not echo replies and should be
// skipped by the receive loop.
func isEchoReply(family string, msgType icmp.Type) bool {
	switch family {
	case FamilyIPv4:
		return msgType == ipv4.ICMPTypeEchoReply
	case FamilyIPv6:
		return msgType == ipv6.ICMPTypeEchoReply
	default:
		return false
	}
}

// Ping sends ICMP echo requests to the target IP address. It auto-selects
// IPv4 or IPv6 based on the resolved target. Returns one PingResult per
// attempt; each result carries the address family used.
func (p *defaultPinger) Ping(ctx context.Context, target string, count int, timeout time.Duration) ([]PingResult, error) {
	ip, family, err := resolveTarget(target)
	if err != nil {
		return nil, err
	}

	conn, protocol, echoType, err := listenICMP(family)
	if err != nil {
		return nil, err
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

		result := p.singlePing(ctx, conn, ip, family, protocol, echoType, timeout)
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

// singlePing performs a single ICMP echo request/reply for the given family.
func (p *defaultPinger) singlePing(
	ctx context.Context,
	conn *icmp.PacketConn,
	ip net.IP,
	family string,
	protocol int,
	echoType icmp.Type,
	timeout time.Duration,
) PingResult {
	// Use unique sequence number for each ping to avoid collisions
	seq := uint16(atomic.AddUint32(&pingSequence, 1) & 0xFFFF)

	// Create ICMP echo request message with unique ID and sequence
	msg := icmp.Message{
		Type: echoType,
		Code: 0,
		Body: &icmp.Echo{
			ID:   int(pingID),
			Seq:  int(seq),
			Data: []byte("node-doctor-ping"),
		},
	}

	msgBytes, err := msg.Marshal(nil)
	if err != nil {
		log.Printf("[DEBUG] Ping to %s (%s): failed to marshal ICMP message: %v", ip, family, err)
		return PingResult{
			Success: false,
			Error:   fmt.Errorf("failed to marshal ICMP message: %w", err),
			Family:  family,
		}
	}

	// Set deadline for this ping
	deadline := time.Now().Add(timeout)
	if err := conn.SetDeadline(deadline); err != nil {
		log.Printf("[DEBUG] Ping to %s (%s): failed to set deadline: %v", ip, family, err)
		return PingResult{
			Success: false,
			Error:   fmt.Errorf("failed to set deadline: %w", err),
			Family:  family,
		}
	}

	// Send echo request
	start := time.Now()
	_, err = conn.WriteTo(msgBytes, &net.IPAddr{IP: ip})
	if err != nil {
		log.Printf("[DEBUG] Ping to %s (%s): failed to send: %v", ip, family, err)
		return PingResult{
			Success: false,
			Error:   fmt.Errorf("failed to send ICMP echo request: %w", err),
			Family:  family,
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
				Family:  family,
			}
		default:
		}

		n, peer, err := conn.ReadFrom(reply)
		rtt := time.Since(start)

		if err != nil {
			// Only log actual failures (usually timeout)
			log.Printf("[DEBUG] Ping to %s (%s): timeout after %v: %v", ip, family, rtt, err)
			return PingResult{
				Success: false,
				Error:   fmt.Errorf("failed to receive ICMP echo reply: %w", err),
				Family:  family,
			}
		}

		// Parse reply. ICMPv6 sockets receive a lot of unrelated multicast
		// traffic (ND, MLD, RA) that may occasionally fail to parse; treat
		// any parse error as "not our reply" and keep waiting for the real one
		// until the deadline kicks in.
		replyMsg, err := icmp.ParseMessage(protocol, reply[:n])
		if err != nil {
			log.Printf("[DEBUG] Ping to %s (%s): skipping unparseable reply from %s: %v", ip, family, peer.String(), err)
			continue
		}

		// Skip non-echo-reply messages (e.g., destination unreachable, ND on v6)
		if !isEchoReply(family, replyMsg.Type) {
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
			Family:  family,
		}
	}
}
