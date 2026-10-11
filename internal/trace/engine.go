// Package trace runs repeated probe rounds towards a target and keeps
// per-hop statistics, reverse DNS names and AS data.
package trace

import (
	"context"
	"math"
	"net"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/trelowney/ttrace/internal/asinfo"
	"github.com/trelowney/ttrace/internal/geo"
	"github.com/trelowney/ttrace/internal/probe"
)

type Config struct {
	Target   string // what the user typed
	Probe    string // how probes are sent, shown to the user, e.g. "ICMP" or "UDP port 33434"
	IP       net.IP
	FirstTTL int // first hop to probe (default 1)
	MaxTTL   int
	GiveUp   int // stop probing after this many silent hops in a row past the last answer (0 = never)
	Hide     int // hide address, name and AS of the first N hops (for screenshots)
	Interval time.Duration
	Timeout  time.Duration
	Count    int  // rounds to run, 0 = until cancelled
	DNS      bool // reverse DNS lookups
	AS       bool    // AS lookups
	Geo      *geo.DB // approximate places of routers; nil or not downloaded = none
}

// Hop is one row of the result. Times are in milliseconds.
type Hop struct {
	TTL      int       `json:"ttl"`
	Addr     string    `json:"addr,omitempty"`
	Host     string    `json:"host,omitempty"`
	ASN      string    `json:"asn,omitempty"`
	ASName   string    `json:"as_name,omitempty"`
	Country  string    `json:"country,omitempty"`
	Prefix   string    `json:"prefix,omitempty"`
	Registry string    `json:"registry,omitempty"`
	Private  bool      `json:"private,omitempty"`
	Hidden   bool      `json:"hidden,omitempty"`
	Sent     int       `json:"sent"`
	Recv     int       `json:"recv"`
	Loss     float64   `json:"loss_pct"`
	Last     float64   `json:"last_ms"`
	Avg      float64   `json:"avg_ms"`
	Best     float64   `json:"best_ms"`
	Worst    float64   `json:"worst_ms"`
	StDev    float64   `json:"stdev_ms"`
	Recent   []float64 `json:"recent_ms,omitempty"` // last rounds, oldest first; -1 = no answer
	MPLS     []int     `json:"mpls,omitempty"`      // MPLS labels reported by the router
	Others   []Other   `json:"others,omitempty"`    // more routers that answered for this hop (parallel paths)
	Place    *geo.Place `json:"place,omitempty"`    // approximate place of the router (needs the location database)
}

// Other is an additional address that answered for the same hop, usually
// because traffic is spread over parallel paths (ECMP).
type Other struct {
	Addr    string `json:"addr"`
	Host    string `json:"host,omitempty"`
	ASN     string `json:"asn,omitempty"`
	ASName  string `json:"as_name,omitempty"`
	Country string `json:"country,omitempty"`
	Private bool   `json:"private,omitempty"`
	Seen    int    `json:"seen"` // how many answers came from this address
	MPLS    []int  `json:"mpls,omitempty"`
}

// Snapshot is a consistent copy of the current state.
type Snapshot struct {
	Target  string `json:"target"`
	IP      string `json:"ip"`
	Family  string `json:"family"` // "IPv4" or "IPv6"
	Probe   string `json:"probe,omitempty"`
	Round   int    `json:"round"`
	Reached bool   `json:"reached"`
	Hops    []Hop  `json:"hops"`
	Links   []Link `json:"links,omitempty"`
	Geo     string `json:"geo,omitempty"` // state of the location data download, for showing next to the map
}

// Link says that within one round the router From answered at hop FromTTL
// and To answered at the next answering hop ToTTL (hops in between were
// silent). From is empty for the first answering hop (the link starts at
// this computer). Used for drawing the route as a graph.
type Link struct {
	From    string `json:"from"`
	To      string `json:"to"`
	FromTTL int    `json:"from_ttl"`
	ToTTL   int    `json:"to_ttl"`
	Seen    int    `json:"seen"`
}

// RecentLen is how many past results per hop are kept for the pulse view.
const RecentLen = 60

type hopStats struct {
	addr                    string         // first address that answered
	order                   []string       // all answering addresses, first seen first
	seen                    map[string]int // answers per address
	mpls                    map[string][]int
	sent, recv              int
	last, best, worst, mean float64
	m2                      float64 // Welford running variance
	recent                  []float64
}

type Engine struct {
	mu      sync.Mutex
	cfg     Config
	prober  probe.Prober
	gen     int // bumped when the target changes; results of older rounds are dropped
	hops    []hopStats
	destTTL int
	round   int
	links   map[[2]string]*Link

	as      *asinfo.Resolver
	names   map[string]string
	asinfo  map[string]asinfo.Info
	asked   map[string]bool
	lookups sync.WaitGroup

	seq    atomic.Uint32
	paused atomic.Bool
}

func New(cfg Config, p probe.Prober) *Engine {
	if cfg.FirstTTL < 1 {
		cfg.FirstTTL = 1
	}
	e := &Engine{
		cfg:    cfg,
		prober: p,
		as:     asinfo.NewResolver(),
		names:  map[string]string{},
		asinfo: map[string]asinfo.Info{},
		asked:  map[string]bool{},
	}
	e.reset()
	return e
}

func (e *Engine) reset() {
	e.hops = make([]hopStats, e.cfg.MaxTTL)
	e.destTTL = 0
	e.round = 0
	e.links = map[[2]string]*Link{}
}

// Reset clears statistics (name and AS caches are kept).
func (e *Engine) Reset() {
	e.mu.Lock()
	e.reset()
	e.mu.Unlock()
}

// Replace switches to a new target address (e.g. IPv4 <-> IPv6) and starts over.
func (e *Engine) Replace(ip net.IP, p probe.Prober) {
	e.mu.Lock()
	old := e.prober
	e.prober = p
	e.cfg.IP = ip
	e.gen++
	e.reset()
	timeout := e.cfg.Timeout
	e.mu.Unlock()
	// probes of the running round may still use the old prober
	time.AfterFunc(timeout+time.Second, func() { old.Close() })
}

// Close releases the current prober.
func (e *Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.prober.Close()
}

func (e *Engine) IP() net.IP {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cfg.IP
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
		first, limit := e.cfg.FirstTTL, e.limit()
		p, gen := e.prober, e.gen
		e.mu.Unlock()
		if limit < first {
			limit = first
		}

		results := make([]probe.Result, 0, limit-first+1)
		var rmu sync.Mutex
		var wg sync.WaitGroup
		for ttl := first; ttl <= limit; ttl++ {
			seq := int(e.seq.Add(1) & 0xffff)
			wg.Add(1)
			go func(ttl, seq int) {
				defer wg.Done()
				r := p.Probe(ctx, ttl, seq, e.cfg.Timeout)
				rmu.Lock()
				results = append(results, r)
				rmu.Unlock()
			}(ttl, seq)
			sleep(ctx, 10*time.Millisecond) // spread probes a little; routers rate-limit ICMP
		}
		wg.Wait()
		if ctx.Err() != nil {
			return
		}
		done := e.record(results, gen)
		if update != nil {
			update(e.Snapshot())
		}
		if done {
			return
		}
		sleep(ctx, time.Until(start.Add(e.cfg.Interval)))
	}
}

// limit is the highest TTL worth probing in the next round. Caller holds mu.
func (e *Engine) limit() int {
	if e.destTTL > 0 {
		return e.destTTL
	}
	limit := e.cfg.MaxTTL
	if e.cfg.GiveUp > 0 && e.round > 0 {
		last := e.cfg.FirstTTL - 1
		for i := range e.hops {
			if e.hops[i].recv > 0 {
				last = i + 1
			}
		}
		if l := last + e.cfg.GiveUp; l < limit {
			limit = l
		}
	}
	return limit
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

func (e *Engine) record(results []probe.Result, gen int) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if gen != e.gen {
		return false
	}
	e.round++
	for _, r := range results {
		if r.TTL < 1 || r.TTL > len(e.hops) {
			continue
		}
		h := &e.hops[r.TTL-1]
		h.sent++
		if r.Addr == nil {
			h.push(-1)
			continue
		}
		ms := float64(r.RTT.Microseconds()) / 1000
		h.push(ms)
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
		if h.seen == nil {
			h.addr = addr
			h.seen, h.mpls = map[string]int{}, map[string][]int{}
		}
		if h.seen[addr] == 0 {
			h.order = append(h.order, addr)
		}
		h.seen[addr]++
		if r.MPLS != nil {
			h.mpls[addr] = r.MPLS
		}
		if !e.asked[addr] {
			e.asked[addr] = true
			e.lookup(r.Addr)
		}
		if (r.Reached || r.Unreach) && (e.destTTL == 0 || r.TTL < e.destTTL) {
			e.destTTL = r.TTL
		}
	}
	e.recordLinks(results)
	return e.cfg.Count > 0 && e.round >= e.cfg.Count
}

// recordLinks remembers which answering routers followed each other in one round.
func (e *Engine) recordLinks(results []probe.Result) {
	sorted := append([]probe.Result(nil), results...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].TTL < sorted[j].TTL })
	prev, prevTTL := "", 0
	for _, r := range sorted {
		if r.Addr == nil || (e.destTTL > 0 && r.TTL > e.destTTL) {
			continue
		}
		addr := r.Addr.String()
		if addr == prev {
			continue
		}
		k := [2]string{prev, addr}
		l := e.links[k]
		if l == nil {
			l = &Link{From: prev, To: addr, FromTTL: prevTTL, ToTTL: r.TTL}
			e.links[k] = l
		}
		l.Seen++
		prev, prevTTL = addr, r.TTL
	}
}

func (h *hopStats) push(v float64) {
	h.recent = append(h.recent, v)
	if len(h.recent) > RecentLen {
		h.recent = h.recent[len(h.recent)-RecentLen:]
	}
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
	s := Snapshot{Target: e.cfg.Target, IP: e.cfg.IP.String(), Family: "IPv4", Round: e.round, Reached: e.destTTL > 0, Probe: e.cfg.Probe}
	if e.cfg.IP.To4() == nil {
		s.Family = "IPv6"
	}

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
	for i := e.cfg.FirstTTL - 1; i < n; i++ {
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
		row.Recent = append([]float64(nil), h.recent...)
		switch {
		case i < e.cfg.Hide:
			row.Addr, row.Hidden = "", true
		case h.addr != "":
			row.Host = e.names[h.addr]
			if a, ok := e.asinfo[h.addr]; ok {
				row.ASN, row.ASName, row.Country = a.ASN, a.Name, a.Country
				row.Prefix, row.Registry, row.Private = a.Prefix, a.Registry, a.Private
			}
			row.MPLS = h.mpls[h.addr]
			if p, ok := e.cfg.Geo.Lookup(net.ParseIP(h.addr)); ok {
				row.Place = &p
			}
			for _, addr := range h.order[1:] {
				o := Other{Addr: addr, Host: e.names[addr], Seen: h.seen[addr], MPLS: h.mpls[addr]}
				if a, ok := e.asinfo[addr]; ok {
					o.ASN, o.ASName, o.Country, o.Private = a.ASN, a.Name, a.Country, a.Private
				}
				row.Others = append(row.Others, o)
			}
		}
		s.Hops = append(s.Hops, row)
	}
	hide := func(addr string, ttl int) string {
		if ttl >= 1 && ttl <= e.cfg.Hide {
			return "(hidden)"
		}
		return addr
	}
	for _, l := range e.links {
		c := *l
		c.From, c.To = hide(c.From, c.FromTTL), hide(c.To, c.ToTTL)
		s.Links = append(s.Links, c)
	}
	sort.Slice(s.Links, func(i, j int) bool {
		a, b := s.Links[i], s.Links[j]
		if a.ToTTL != b.ToTTL {
			return a.ToTTL < b.ToTTL
		}
		if a.From != b.From {
			return a.From < b.From
		}
		return a.To < b.To
	})
	return s
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
