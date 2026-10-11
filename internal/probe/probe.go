// Package probe sends single ICMP echo probes with a given TTL / hop limit
// and reports who answered. Platform code lives in probe_unix.go and
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
	Unreach bool // destination unreachable
}

// Prober sends probes towards one destination. Probe is safe for concurrent use.
type Prober interface {
	Probe(ctx context.Context, ttl, seq int, timeout time.Duration) Result
	Close() error
}

// payload carried in every echo request (padding to a common ping size).
var payload = []byte("ttrace-probe-ttrace-probe-ttrace-probe-ttrace-pr")
