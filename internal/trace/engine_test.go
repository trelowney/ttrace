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

func TestFirstHideRecent(t *testing.T) {
	dst := net.ParseIP("192.0.2.10")
	e := New(Config{Target: "example", IP: dst, FirstTTL: 2, MaxTTL: 30, Hide: 1, Interval: time.Millisecond, Timeout: time.Second, Count: 4}, fakeProber{dst})
	e.Run(context.Background(), nil)
	s := e.Snapshot()
	if len(s.Hops) != 2 || s.Hops[0].TTL != 2 {
		t.Fatalf("hops: %+v", s.Hops)
	}
	if len(s.Hops[1].Recent) != 4 || s.Hops[0].Recent[0] != -1 {
		t.Errorf("recent: %+v / %+v", s.Hops[1].Recent, s.Hops[0].Recent)
	}
	// TTL 1 is hidden but not shown at all (first=2); check Hide on a new engine
	e2 := New(Config{Target: "example", IP: dst, MaxTTL: 30, Hide: 1, Interval: time.Millisecond, Timeout: time.Second, Count: 1}, fakeProber{dst})
	e2.Run(context.Background(), nil)
	if h := e2.Snapshot().Hops[0]; !h.Hidden || h.Addr != "" {
		t.Errorf("hidden: %+v", h)
	}
}

// silentProber: only hop 1 answers, the target never does.
type silentProber struct{}

func (silentProber) Close() error { return nil }
func (silentProber) Probe(_ context.Context, ttl, seq int, _ time.Duration) probe.Result {
	r := probe.Result{TTL: ttl, Seq: seq}
	if ttl == 1 {
		r.Addr, r.RTT = net.ParseIP("192.168.1.1"), time.Millisecond
	}
	return r
}

func TestGiveUp(t *testing.T) {
	e := New(Config{Target: "x", IP: net.ParseIP("192.0.2.1"), MaxTTL: 30, GiveUp: 3, Interval: time.Millisecond, Timeout: time.Second, Count: 2}, silentProber{})
	e.Run(context.Background(), nil)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.hops[3].sent != 2 || e.hops[4].sent != 1 {
		t.Errorf("hop4 sent %d (want 2), hop5 sent %d (want 1)", e.hops[3].sent, e.hops[4].sent)
	}
}
