// Package trace runs repeated probe rounds towards a target and keeps
// per-hop statistics, reverse DNS names and AS data.
package trace

import (
	"context"
	"math"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/trelowney/ttrace/internal/asinfo"
	"github.com/trelowney/ttrace/internal/probe"
)

type Config struct {
	Target   string // what the user typed
	IP       net.IP
	MaxTTL   int
	Interval time.Duration
	Timeout  time.Duration
	Count    int  // rounds to run, 0 = until cancelled
	DNS      bool // reverse DNS lookups
	AS       bool // AS lookups
}

// Hop is one row of the result. Times are in milliseconds.
type Hop struct {
	TTL      int     `json:"ttl"`
	Addr     string  `json:"addr,omitempty"`
	Host     string  `json:"host,omitempty"`
	ASN      string  `json:"asn,omitempty"`
	ASName   string  `json:"as_name,omitempty"`
	Country  string  `json:"country,omitempty"`
	Prefix   string  `json:"prefix,omitempty"`
	Registry string  `json:"registry,omitempty"`
	Private  bool    `json:"private,omitempty"`
	Sent     int     `json:"sent"`
	Recv     int     `json:"recv"`
	Loss     float64 `json:"loss_pct"`
	Last     float64 `json:"last_ms"`
	Avg      float64 `json:"avg_ms"`
	Best     float64 `json:"best_ms"`
	Worst    float64 `json:"worst_ms"`
	StDev    float64 `json:"stdev_ms"`
}

// Snapshot is a consistent copy of the current state.
type Snapshot struct {
	Target  string `json:"target"`
	IP      string `json:"ip"`
	Round   int    `json:"round"`
	Reached bool   `json:"reached"`
	Hops    []Hop  `json:"hops"`
}

type hopStats struct {
	addr                    string
	sent, recv              int
	last, best, worst, mean float64
	m2                      float64 // Welford running variance
}

type Engine struct {
	cfg    Config
	prober probe.Prober
	as     *asinfo.Resolver

	mu      sync.Mutex
	hops    []hopStats
	destTTL int
	round   int
	names   map[string]string
	asinfo  map[string]asinfo.Info
	asked   map[string]bool
	lookups sync.WaitGroup

	seq    atomic.Uint32
	paused atomic.Bool
}

func New(cfg Config, p probe.Prober) *Engine {
	e := &Engine{cfg: cfg, prober: p, as: asinfo.NewResolver()}
	e.reset()
	e.names = map[string]string{}
	e.asinfo = map[string]asinfo.Info{}
	e.asked = map[string]bool{}
	return e
}

func (e *Engine) reset() {
	e.hops = make([]hopStats, e.cfg.MaxTTL)
	e.destTTL = 0
	e.round = 0
}

// Reset clears statistics (name and AS caches are kept).
func (e *Engine) Reset() {
	e.mu.Lock()
	e.reset()
	e.mu.Unlock()
}

func (e *Engine) SetPaused(v bool) { e.paused.Store(v) }
func (e *Engine) Paused() bool     { return e.paused.Load() }

// Run sends rounds until ctx is cancelled or Count rounds are done.
// update is called after every round.
func (e *Engine) Run(ctx context.Context, update func(Snapshot)) {
	for ctx.Err() == nil {
		if e.paused.Load() {
			sleep(ctx, 100*time.Millisecond)
			continue
		}
		start := time.Now()
		e.mu.Lock()
		limit := e.cfg.MaxTTL
		if e.destTTL > 0 {
			limit = e.destTTL
		}
		e.mu.Unlock()

		results := make([]probe.Result, limit)
		var wg sync.WaitGroup
		for ttl := 1; ttl <= limit; ttl++ {
			seq := int(e.seq.Add(1) & 0xffff)
			wg.Add(1)
			go func(ttl, seq int) {
				defer wg.Done()
				results[ttl-1] = e.prober.Probe(ctx, ttl, seq, e.cfg.Timeout)
			}(ttl, seq)
			sleep(ctx, 10*time.Millisecond) // spread probes a little; routers rate-limit ICMP
		}
		wg.Wait()
		if ctx.Err() != nil {
			return
		}
		done := e.record(results)
		if update != nil {
			update(e.Snapshot())
		}
		if done {
			return
		}
		sleep(ctx, time.Until(start.Add(e.cfg.Interval)))
	}
}

func sleep(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}

func (e *Engine) record(results []probe.Result) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.round++
	for _, r := range results {
		if r.TTL < 1 || r.TTL > len(e.hops) {
			continue
		}
		h := &e.hops[r.TTL-1]
		h.sent++
		if r.Addr == nil {
			continue
		}
		ms := float64(r.RTT.Microseconds()) / 1000
		h.recv++
		h.last = ms
		if h.recv == 1 || ms < h.best {
			h.best = ms
		}
		if ms > h.worst {
			h.worst = ms
		}
		d := ms - h.mean
		h.mean += d / float64(h.recv)
		h.m2 += d * (ms - h.mean)

		addr := r.Addr.String()
		h.addr = addr
		if !e.asked[addr] {
			e.asked[addr] = true
			e.lookup(r.Addr)
		}
		if (r.Reached || r.Unreach) && (e.destTTL == 0 || r.TTL < e.destTTL) {
			e.destTTL = r.TTL
		}
	}
	return e.cfg.Count > 0 && e.round >= e.cfg.Count
}

func (e *Engine) lookup(ip net.IP) {
	addr := ip.String()
	if e.cfg.DNS {
		e.lookups.Add(1)
		go func() {
			defer e.lookups.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			if names, err := net.DefaultResolver.LookupAddr(ctx, addr); err == nil && len(names) > 0 {
				e.mu.Lock()
				e.names[addr] = strings.TrimSuffix(names[0], ".")
				e.mu.Unlock()
			}
		}()
	}
	if e.cfg.AS {
		e.lookups.Add(1)
		go func() {
			defer e.lookups.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			if info, err := e.as.Lookup(ctx, ip); err == nil {
				e.mu.Lock()
				e.asinfo[addr] = info
				e.mu.Unlock()
			}
		}()
	}
}

// WaitLookups waits (at most d) for pending DNS/AS lookups, used before a final report.
func (e *Engine) WaitLookups(d time.Duration) {
	done := make(chan struct{})
	go func() { e.lookups.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
	}
}

func (e *Engine) Snapshot() Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := Snapshot{Target: e.cfg.Target, IP: e.cfg.IP.String(), Round: e.round, Reached: e.destTTL > 0}

	n := e.destTTL
	if n == 0 {
		// target not reached (yet): show up to the last hop that answered,
		// plus one silent row so it is visible where the path goes quiet
		for i := range e.hops {
			if e.hops[i].recv > 0 {
				n = i + 1
			}
		}
		if e.round > 0 && n < len(e.hops) {
			n++
		}
	}
	for i := 0; i < n; i++ {
		h := e.hops[i]
		row := Hop{TTL: i + 1, Addr: h.addr, Sent: h.sent, Recv: h.recv}
		if h.sent > 0 {
			row.Loss = float64(h.sent-h.recv) / float64(h.sent) * 100
		}
		if h.recv > 0 {
			row.Last, row.Best, row.Worst = round2(h.last), round2(h.best), round2(h.worst)
			row.Avg = round2(h.mean)
			row.StDev = round2(math.Sqrt(h.m2 / float64(h.recv)))
		}
		if h.addr != "" {
			row.Host = e.names[h.addr]
			if a, ok := e.asinfo[h.addr]; ok {
				row.ASN, row.ASName, row.Country = a.ASN, a.Name, a.Country
				row.Prefix, row.Registry, row.Private = a.Prefix, a.Registry, a.Private
			}
		}
		s.Hops = append(s.Hops, row)
	}
	return s
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
