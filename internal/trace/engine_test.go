package trace

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/trelowney/ttrace/internal/probe"
)

// fakeProber answers like a 3-hop path; hop 2 never answers.
type fakeProber struct{ dst net.IP }

func (f fakeProber) Close() error { return nil }
func (f fakeProber) Probe(_ context.Context, ttl, seq int, _ time.Duration) probe.Result {
	r := probe.Result{TTL: ttl, Seq: seq}
	switch {
	case ttl == 1:
		r.Addr, r.RTT = net.ParseIP("192.168.1.1"), 2*time.Millisecond
	case ttl == 2:
	default:
		r.Addr, r.RTT, r.Reached = f.dst, 10*time.Millisecond, true
	}
	return r
}

func TestEngineRounds(t *testing.T) {
	dst := net.ParseIP("192.0.2.10")
	e := New(Config{Target: "example", IP: dst, MaxTTL: 30, Interval: time.Millisecond, Timeout: time.Second, Count: 3}, fakeProber{dst})
	e.Run(context.Background(), nil)
	s := e.Snapshot()
	if s.Round != 3 || !s.Reached || len(s.Hops) != 3 {
		t.Fatalf("round=%d reached=%v hops=%d", s.Round, s.Reached, len(s.Hops))
	}
	if s.Hops[1].Loss != 100 || s.Hops[0].Loss != 0 {
		t.Errorf("loss: %+v", s.Hops)
	}
	if s.Hops[2].Avg != 10 || s.Hops[2].Addr != "192.0.2.10" {
		t.Errorf("hop3: %+v", s.Hops[2])
	}
	// after the first round only 3 TTLs are probed
	if s.Hops[2].Sent != 3 {
		t.Errorf("sent: %d", s.Hops[2].Sent)
	}
}
