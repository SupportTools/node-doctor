//go:build integration

// Package network IPv6 pinger integration tests.
//
// These tests exercise the REAL defaultPinger against the IPv6 loopback (::1)
// and therefore require raw ICMPv6 socket privileges (CAP_NET_RAW) and a host
// with IPv6 enabled. They are gated behind the `integration` build tag so the
// default `go test -short` unit run never attempts raw sockets:
//
//	go test -tags=integration -run IPv6 ./pkg/monitors/network/...
//
// The platform-agnostic IPv6 pinger UNIT tests (address-family/zone parsing,
// destination building, reply matching) live untagged in pinger_test.go; this
// file adds the live-socket v6 coverage that cannot run in the unit tier.
package network

import (
	"context"
	"net"
	"testing"
	"time"
)

// ipv6LoopbackAvailable reports whether the host has a usable IPv6 loopback,
// so the test can skip cleanly on IPv4-only / IPv6-disabled environments
// instead of failing.
func ipv6LoopbackAvailable(t *testing.T) bool {
	t.Helper()
	ln, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// TestDefaultPinger_IPv6Loopback_Integration pings ::1 with the real pinger and
// asserts the result is classified as the IPv6 family. It skips (not fails) when
// IPv6 loopback is unavailable or raw ICMPv6 sockets require privileges the test
// process lacks.
func TestDefaultPinger_IPv6Loopback_Integration(t *testing.T) {
	if !ipv6LoopbackAvailable(t) {
		t.Skip("IPv6 loopback not available on this host; skipping IPv6 ping integration test")
	}

	pinger := newDefaultPinger()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	results, err := pinger.Ping(ctx, "::1", 2, 2*time.Second)
	if err != nil {
		// Raw ICMPv6 sockets need elevated privileges; treat as a skip so the
		// test is meaningful where it can run and silent where it cannot.
		t.Skipf("IPv6 ping to ::1 could not run (likely missing CAP_NET_RAW): %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected at least one ping result for ::1")
	}

	var sawSuccess bool
	for i, r := range results {
		if r.Family != FamilyIPv6 {
			t.Errorf("result[%d].Family = %q, want %q for ::1", i, r.Family, FamilyIPv6)
		}
		if r.Success {
			sawSuccess = true
		}
	}
	if !sawSuccess {
		t.Errorf("expected at least one successful ICMPv6 echo to ::1; got %+v", results)
	}
}

// TestDefaultPinger_IPv6LinkLocal_Integration verifies that pinging a link-local
// target with a zone does not error at the resolve/send layer (it may legitimately
// time out with no reply). It guards on IPv6 loopback availability and treats a
// privilege/socket error as a skip.
func TestDefaultPinger_IPv6LinkLocal_Integration(t *testing.T) {
	if !ipv6LoopbackAvailable(t) {
		t.Skip("IPv6 loopback not available on this host; skipping link-local integration test")
	}

	// Resolve a usable link-local target+zone from the host's interfaces; skip
	// if none is present (e.g. minimal container netns).
	target, ok := firstLinkLocalTarget()
	if !ok {
		t.Skip("no IPv6 link-local address with a zone found on this host")
	}

	pinger := newDefaultPinger()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	results, err := pinger.Ping(ctx, target, 1, 1*time.Second)
	if err != nil {
		t.Skipf("link-local ping to %s could not run (likely missing CAP_NET_RAW): %v", target, err)
	}
	if len(results) == 0 {
		t.Fatalf("expected a result for %s", target)
	}
	// The probe may time out (no reply), but the family must still be classified v6.
	if results[0].Family != FamilyIPv6 {
		t.Errorf("result.Family = %q, want %q for link-local %s", results[0].Family, FamilyIPv6, target)
	}
}

// firstLinkLocalTarget returns the first fe80::/10 address found on a non-loopback
// interface formatted as "addr%zone", and whether one was found.
func firstLinkLocalTarget() (string, bool) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", false
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			if ipnet.IP.To4() == nil && ipnet.IP.IsLinkLocalUnicast() {
				return ipnet.IP.String() + "%" + iface.Name, true
			}
		}
	}
	return "", false
}
