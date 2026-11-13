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

// ecmpProber: hop 2 alternates between two routers, one of them reports an MPLS label.
type ecmpProber struct{ dst net.IP }

func (ecmpProber) Close() error { return nil }
func (f ecmpProber) Probe(_ context.Context, ttl, seq int, _ time.Duration) probe.Result {
	r := probe.Result{TTL: ttl, Seq: seq, RTT: time.Millisecond}
	switch ttl {
	case 1:
		r.Addr = net.ParseIP("192.168.1.1")
	case 2:
		r.Addr = net.ParseIP("198.51.100.1")
		if seq%2 == 0 {
			r.Addr, r.MPLS = net.ParseIP("198.51.100.2"), []int{24001}
		}
	default:
		r.Addr, r.Reached = f.dst, true
	}
	return r
}

func TestOthersAndMPLS(t *testing.T) {
	dst := net.ParseIP("192.0.2.10")
	e := New(Config{Target: "x", IP: dst, MaxTTL: 30, Interval: time.Millisecond, Timeout: time.Second, Count: 4}, ecmpProber{dst})
	e.Run(context.Background(), nil)
	h := e.Snapshot().Hops[1]
	if len(h.Others) != 1 || h.Others[0].Seen+countOf(h, e) != 4 {
		t.Fatalf("others: %+v", h.Others)
	}
	withLabel := h.MPLS
	if h.Addr == "198.51.100.1" {
		withLabel = h.Others[0].MPLS
	}
	if len(withLabel) != 1 || withLabel[0] != 24001 {
		t.Errorf("mpls: main %v, other %v", h.MPLS, h.Others[0].MPLS)
	}
}

func countOf(h Hop, e *Engine) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.hops[h.TTL-1].seen[h.Addr]
}

func TestLinks(t *testing.T) {
	dst := net.ParseIP("192.0.2.10")
	e := New(Config{Target: "x", IP: dst, MaxTTL: 30, Hide: 1, Interval: time.Millisecond, Timeout: time.Second, Count: 2}, fakeProber{dst})
	e.Run(context.Background(), nil)
	l := e.Snapshot().Links
	// you -> hop 1 (hidden) -> (hop 2 silent) -> target at hop 3
	if len(l) != 2 || l[0].From != "" || l[0].To != "(hidden)" || l[1].From != "(hidden)" || l[1].To != "192.0.2.10" || l[1].FromTTL != 1 || l[1].ToTTL != 3 || l[1].Seen != 2 {
		t.Fatalf("links: %+v", l)
	}
}

func TestMarkJumps(t *testing.T) {
	hop := func(best float64) Hop { return Hop{Recv: 1, Best: best} }
	hops := []Hop{
		hop(1), hop(2),
		hop(40),          // slow to answer itself, the next hops are fast again: not marked
		hop(3), {Recv: 0}, // silent hop is skipped
		hop(30),           // real jump of 27 ms that stays
		hop(31), hop(29),
	}
	MarkJumps(hops)
	for i, want := range []float64{0, 0, 0, 0, 0, 27, 0, 0} {
		if hops[i].Jump != want {
			t.Errorf("hop %d: jump %v, want %v", i+1, hops[i].Jump, want)
		}
	}
	// a small rise is not a jump
	small := []Hop{hop(100), hop(108), hop(109)}
	MarkJumps(small)
	if small[1].Jump != 0 {
		t.Errorf("8 ms on 100 ms marked: %v", small[1].Jump)
	}
}

func TestASPath(t *testing.T) {
	hops := []Hop{
		{Addr: "192.168.1.1", Private: true},
		{Addr: "198.51.100.1", ASN: "209874", ASName: "TECHTIDE - Tech Tide"},
		{Addr: "198.51.100.2", ASN: "49581", ASName: "TUBE-HOSTING"},
		{},
		{Addr: "198.51.100.3", ASN: "49581", ASName: "TUBE-HOSTING"},
		{Addr: "185.1.160.217", ASN: "13335", ASName: "CLOUDFLARENET", IX: "Frys-IX"},
		{Addr: "203.0.113.1", ASN: "13335", ASName: "CLOUDFLARENET"},
		{Addr: "203.0.113.2"}, // AS not known (yet)
	}
	got := ASPath(hops)
	want := []PathStep{{ASN: "209874", Name: "TECHTIDE - Tech Tide"}, {ASN: "49581", Name: "TUBE-HOSTING"}, {IX: "Frys-IX"}, {ASN: "13335", Name: "CLOUDFLARENET"}}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("step %d: %+v, want %+v", i, got[i], want[i])
		}
	}
}

// tosProber is fakeProber whose second router reports a changed TOS byte.
type tosProber struct{ fakeProber }

func (f tosProber) Probe(ctx context.Context, ttl, seq int, d time.Duration) probe.Result {
	r := f.fakeProber.Probe(ctx, ttl, seq, d)
	if ttl == 1 {
		r.TOS, r.HasTOS = 184, true
	}
	return r
}

func TestTOSSeen(t *testing.T) {
	dst := net.ParseIP("192.0.2.10")
	e := New(Config{Target: "example", IP: dst, MaxTTL: 30, Interval: time.Millisecond, Timeout: time.Second, Count: 1, TOS: 184}, tosProber{fakeProber{dst}})
	e.Run(context.Background(), nil)
	s := e.Snapshot()
	if s.TOSSent != 184 || s.Hops[0].TOSSeen == nil || *s.Hops[0].TOSSeen != 184 || s.Hops[2].TOSSeen != nil {
		t.Fatalf("sent %d, hops %+v", s.TOSSent, s.Hops)
	}
}
