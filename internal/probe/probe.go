// Package probe sends single probes (ICMP echo, UDP or TCP SYN) with a given
// TTL / hop limit and reports who answered. Platform code lives in probe_unix.go and
// probe_windows.go.
package probe

import (
	"context"
	"net"
	"time"
)

// Result of one probe. Addr is nil when nothing answered before the timeout.
type Result struct {
	TTL     int
	Seq     int
	Addr    net.IP
	RTT     time.Duration
	Reached bool // echo reply from the target itself
	Unreach bool  // destination unreachable
	MPLS    []int // MPLS labels the router reported in the ICMP extensions (RFC 4950)
}

// Prober sends probes towards one destination. Probe is safe for concurrent use.
type Prober interface {
	Probe(ctx context.Context, ttl, seq int, timeout time.Duration) Result
	Close() error
}

// Options shared by all platforms.
type Options struct {
	Size   int    // whole packet size in bytes incl. IP and ICMP headers (0 = 64)
	TOS    int    // IPv4 TOS / IPv6 traffic class byte
	Source net.IP // send from this local address (nil = automatic)
	Proto  string // "icmp" (default), "udp" or "tcp"
	Port   int    // destination port for UDP and TCP
}

// Default destination ports.
const (
	DefaultUDPPort = 33434 // the classic traceroute port, nothing usually listens there
	DefaultTCPPort = 80
)

// makePayload fills the echo data so that the whole packet has opts.Size bytes.
func makePayload(size int, v6 bool) []byte {
	if size == 0 {
		size = 64
	}
	hdr := 28 // IPv4 + ICMP header
	if v6 {
		hdr = 48
	}
	n := size - hdr
	if n < 8 {
		n = 8
	}
	const pattern = "ttrace."
	b := make([]byte, n)
	for i := range b {
		b[i] = pattern[i%len(pattern)]
	}
	return b
}
