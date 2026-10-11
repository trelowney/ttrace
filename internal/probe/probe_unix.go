//go:build !windows

package probe

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

type reply struct {
	addr    net.IP
	at      time.Time
	reached bool
	unreach bool
	mpls    []int
}

// unixProber reads every answer from one raw ICMP socket. ICMP probes go out
// through that socket too; UDP and TCP probes use a fresh socket each, and
// its local port tells the answers apart.
type unixProber struct {
	dst     net.IP
	v6      bool
	id      int
	proto   string
	port    int
	src     net.IP
	tos     int
	conn    *icmp.PacketConn
	payload []byte

	wmu sync.Mutex // set TTL + write must not interleave between ICMP probes

	pmu     sync.Mutex
	pending map[int]chan reply // ICMP: sequence number, UDP/TCP: local port

	closed chan struct{}
}

// New opens a raw ICMP socket towards dst.
func New(dst net.IP, opts Options) (Prober, error) {
	v6 := dst.To4() == nil
	network, laddr := "ip4:icmp", "0.0.0.0"
	if v6 {
		network, laddr = "ip6:ipv6-icmp", "::"
	}
	if opts.Source != nil {
		if (opts.Source.To4() == nil) != v6 {
			return nil, fmt.Errorf("source address %s is not the same IP version as the target %s", opts.Source, dst)
		}
		laddr = opts.Source.String()
	}
	p := &unixProber{
		dst:     dst,
		v6:      v6,
		id:      os.Getpid() & 0xffff,
		proto:   opts.Proto,
		port:    opts.Port,
		src:     opts.Source,
		tos:     opts.TOS,
		payload: makePayload(opts.Size, v6),
		pending: map[int]chan reply{},
		closed:  make(chan struct{}),
	}
	switch p.proto {
	case "", "icmp":
		p.proto = "icmp"
	case "udp":
		if p.port == 0 {
			p.port = DefaultUDPPort
		}
	case "tcp":
		if p.port == 0 {
			p.port = DefaultTCPPort
		}
	default:
		return nil, fmt.Errorf("unknown probe type %q, use icmp, udp or tcp", opts.Proto)
	}
	c, err := icmp.ListenPacket(network, laddr)
	if err != nil {
		return nil, permissionHint(err)
	}
	if opts.TOS != 0 && p.proto == "icmp" {
		if v6 {
			err = c.IPv6PacketConn().SetTrafficClass(opts.TOS)
		} else {
			err = c.IPv4PacketConn().SetTOS(opts.TOS)
		}
		if err != nil {
			c.Close()
			return nil, fmt.Errorf("cannot set TOS: %w", err)
		}
	}
	p.conn = c
	go p.readLoop()
	return p, nil
}

func permissionHint(err error) error {
	if !errors.Is(err, os.ErrPermission) && !errors.Is(err, syscall.EPERM) && !errors.Is(err, syscall.EACCES) {
		return err
	}
	if runtime.GOOS == "linux" {
		exe, _ := os.Executable()
		if exe == "" {
			exe = "ttrace"
		}
		return fmt.Errorf("%w\nttrace needs permission to send raw ICMP packets.\nRun it with sudo, or allow it once:  sudo setcap cap_net_raw+ep %s", err, exe)
	}
	return fmt.Errorf("%w\nttrace needs permission to send raw ICMP packets. Run it with sudo.", err)
}

func (p *unixProber) Close() error {
	close(p.closed)
	return p.conn.Close()
}

func (p *unixProber) Probe(ctx context.Context, ttl, seq int, timeout time.Duration) Result {
	switch p.proto {
	case "udp":
		return p.probeUDP(ctx, ttl, seq, timeout)
	case "tcp":
		return p.probeTCP(ctx, ttl, seq, timeout)
	}
	return p.probeICMP(ctx, ttl, seq, timeout)
}

func (p *unixProber) register(key int, ch chan reply) {
	p.pmu.Lock()
	p.pending[key] = ch
	p.pmu.Unlock()
}

func (p *unixProber) unregister(key int) {
	p.pmu.Lock()
	delete(p.pending, key)
	p.pmu.Unlock()
}

// wait fills res with the answer to a probe sent at start, if one comes in time.
func wait(ctx context.Context, ch chan reply, start time.Time, timeout time.Duration, res *Result) {
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case r := <-ch:
		res.fill(r, start)
	case <-t.C:
	case <-ctx.Done():
	}
}

func (res *Result) fill(r reply, start time.Time) {
	res.Addr = r.addr
	res.RTT = r.at.Sub(start)
	res.Reached = r.reached
	res.Unreach = r.unreach
	res.MPLS = r.mpls
}

func (p *unixProber) probeICMP(ctx context.Context, ttl, seq int, timeout time.Duration) Result {
	res := Result{TTL: ttl, Seq: seq}
	seq &= 0xffff
	ch := make(chan reply, 1)
	p.register(seq, ch)
	defer p.unregister(seq)

	var typ icmp.Type = ipv4.ICMPTypeEcho
	if p.v6 {
		typ = ipv6.ICMPTypeEchoRequest
	}
	msg := icmp.Message{Type: typ, Body: &icmp.Echo{ID: p.id, Seq: seq, Data: p.payload}}
	b, err := msg.Marshal(nil) // ICMPv6 checksum is filled in by the kernel
	if err != nil {
		return res
	}

	p.wmu.Lock()
	if p.v6 {
		err = p.conn.IPv6PacketConn().SetHopLimit(ttl)
	} else {
		err = p.conn.IPv4PacketConn().SetTTL(ttl)
	}
	start := time.Now()
	if err == nil {
		_, err = p.conn.WriteTo(b, &net.IPAddr{IP: p.dst})
	}
	p.wmu.Unlock()
	if err != nil {
		return res
	}
	wait(ctx, ch, start, timeout, &res)
	return res
}

func (p *unixProber) probeUDP(ctx context.Context, ttl, seq int, timeout time.Duration) Result {
	res := Result{TTL: ttl, Seq: seq}
	network := "udp4"
	if p.v6 {
		network = "udp6"
	}
	c, err := net.ListenUDP(network, &net.UDPAddr{IP: p.src})
	if err != nil {
		return res
	}
	defer c.Close()
	if p.v6 {
		pc := ipv6.NewPacketConn(c)
		err = pc.SetHopLimit(ttl)
		if err == nil && p.tos != 0 {
			err = pc.SetTrafficClass(p.tos)
		}
	} else {
		pc := ipv4.NewPacketConn(c)
		err = pc.SetTTL(ttl)
		if err == nil && p.tos != 0 {
			err = pc.SetTOS(p.tos)
		}
	}
	if err != nil {
		return res
	}
	port := c.LocalAddr().(*net.UDPAddr).Port
	ch := make(chan reply, 1)
	p.register(port, ch)
	defer p.unregister(port)

	start := time.Now()
	if _, err := c.WriteToUDP(p.payload, &net.UDPAddr{IP: p.dst, Port: p.port}); err != nil {
		return res
	}
	wait(ctx, ch, start, timeout, &res)
	return res
}

// probeTCP starts a TCP connection with a low TTL. Routers on the way answer
// with ICMP time exceeded; the target answers SYN-ACK (port open) or RST
// (port closed), and both count as reaching it.
func (p *unixProber) probeTCP(ctx context.Context, ttl, seq int, timeout time.Duration) Result {
	res := Result{TTL: ttl, Seq: seq}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ch := make(chan reply, 1)
	d := net.Dialer{Control: func(_, _ string, rc syscall.RawConn) error {
		var serr error
		err := rc.Control(func(fd uintptr) {
			port, err := p.prepareTCP(int(fd), ttl)
			if err != nil {
				serr = err
				return
			}
			p.register(port, ch)
			go func() { // drop the entry once the probe is over
				<-ctx.Done()
				p.unregister(port)
			}()
		})
		if err != nil {
			return err
		}
		return serr
	}}

	type dialResult struct {
		err error
		at  time.Time
	}
	done := make(chan dialResult, 1)
	addr := net.JoinHostPort(p.dst.String(), strconv.Itoa(p.port))
	start := time.Now()
	go func() {
		c, err := d.DialContext(ctx, "tcp", addr)
		at := time.Now()
		if c != nil {
			c.(*net.TCPConn).SetLinger(0) // reset instead of a slow close
			c.Close()
		}
		done <- dialResult{err, at}
	}()

	select {
	case r := <-ch:
		res.fill(r, start)
	case dr := <-done:
		if dr.err == nil || errors.Is(dr.err, syscall.ECONNREFUSED) {
			res.Addr, res.RTT, res.Reached = p.dst, dr.at.Sub(start), true
			break
		}
		select { // the connection may have failed because of an ICMP error we also read
		case r := <-ch:
			res.fill(r, start)
		case <-time.After(50 * time.Millisecond):
		}
	case <-ctx.Done():
	}
	return res
}

// prepareTCP sets TTL and TOS on a new TCP socket and binds it, so that the
// local port is known before the SYN goes out.
func (p *unixProber) prepareTCP(fd, ttl int) (int, error) {
	if p.v6 {
		if err := syscall.SetsockoptInt(fd, syscall.IPPROTO_IPV6, syscall.IPV6_UNICAST_HOPS, ttl); err != nil {
			return 0, err
		}
		if p.tos != 0 {
			if err := syscall.SetsockoptInt(fd, syscall.IPPROTO_IPV6, syscall.IPV6_TCLASS, p.tos); err != nil {
				return 0, err
			}
		}
		sa := &syscall.SockaddrInet6{}
		if p.src != nil {
			copy(sa.Addr[:], p.src.To16())
		}
		if err := syscall.Bind(fd, sa); err != nil {
			return 0, err
		}
	} else {
		if err := syscall.SetsockoptInt(fd, syscall.IPPROTO_IP, syscall.IP_TTL, ttl); err != nil {
			return 0, err
		}
		if p.tos != 0 {
			if err := syscall.SetsockoptInt(fd, syscall.IPPROTO_IP, syscall.IP_TOS, p.tos); err != nil {
				return 0, err
			}
		}
		sa := &syscall.SockaddrInet4{}
		if p.src != nil {
			copy(sa.Addr[:], p.src.To4())
		}
		if err := syscall.Bind(fd, sa); err != nil {
			return 0, err
		}
	}
	sa, err := syscall.Getsockname(fd)
	if err != nil {
		return 0, err
	}
	switch a := sa.(type) {
	case *syscall.SockaddrInet4:
		return a.Port, nil
	case *syscall.SockaddrInet6:
		return a.Port, nil
	}
	return 0, errors.New("unexpected socket address")
}

func (p *unixProber) deliver(key int, r reply) {
	p.pmu.Lock()
	ch := p.pending[key]
	p.pmu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- r:
	default:
	}
}

func (p *unixProber) readLoop() {
	proto := 1
	if p.v6 {
		proto = 58
	}
	buf := make([]byte, 1500)
	for {
		n, peer, err := p.conn.ReadFrom(buf)
		at := time.Now()
		if err != nil {
			select {
			case <-p.closed:
				return
			default:
			}
			time.Sleep(10 * time.Millisecond)
			continue
		}
		ipa, ok := peer.(*net.IPAddr)
		if !ok {
			continue
		}
		if key, r, ok := p.parse(proto, buf[:n], ipa.IP, at); ok {
			p.deliver(key, r)
		}
	}
}

// parse turns one received ICMP packet into an answer for one of our probes.
func (p *unixProber) parse(proto int, b []byte, peer net.IP, at time.Time) (int, reply, bool) {
	m, err := icmp.ParseMessage(proto, b)
	if err != nil {
		return 0, reply{}, false
	}
	from := append(net.IP(nil), peer...)
	switch m.Type {
	case ipv4.ICMPTypeEchoReply, ipv6.ICMPTypeEchoReply:
		e, ok := m.Body.(*icmp.Echo)
		if !ok || p.proto != "icmp" || e.ID != p.id || !from.Equal(p.dst) {
			break
		}
		return e.Seq, reply{addr: from, at: at, reached: true}, true
	case ipv4.ICMPTypeTimeExceeded, ipv6.ICMPTypeTimeExceeded:
		te, ok := m.Body.(*icmp.TimeExceeded)
		if !ok {
			break
		}
		if key, ok := p.inner(te.Data); ok {
			return key, reply{addr: from, at: at, mpls: mplsLabels(te.Extensions)}, true
		}
	case ipv4.ICMPTypeDestinationUnreachable, ipv6.ICMPTypeDestinationUnreachable:
		du, ok := m.Body.(*icmp.DstUnreach)
		if !ok {
			break
		}
		if key, ok := p.inner(du.Data); ok {
			// for UDP and TCP an unreachable from the target itself (normally
			// "port unreachable") means the probe got there
			reached := p.proto != "icmp" && from.Equal(p.dst)
			return key, reply{addr: from, at: at, reached: reached, unreach: !reached, mpls: mplsLabels(du.Extensions)}, true
		}
	}
	return 0, reply{}, false
}

// inner reads our probe quoted inside an ICMP error and returns its key
// (sequence number or local port) when it belongs to this prober.
func (p *unixProber) inner(data []byte) (int, bool) {
	var proto int
	var dst net.IP
	var t []byte // transport header
	if p.v6 {
		if len(data) < 48 {
			return 0, false
		}
		proto, dst, t = int(data[6]), net.IP(data[24:40]), data[40:]
	} else {
		if len(data) < 20 {
			return 0, false
		}
		ihl := int(data[0]&0x0f) * 4
		if ihl < 20 || len(data) < ihl+8 {
			return 0, false
		}
		proto, dst, t = int(data[9]), net.IP(data[16:20]), data[ihl:]
	}
	if !dst.Equal(p.dst) {
		return 0, false
	}
	switch p.proto {
	case "udp", "tcp":
		want := 17
		if p.proto == "tcp" {
			want = 6
		}
		if proto != want || int(binary.BigEndian.Uint16(t[2:4])) != p.port {
			return 0, false
		}
		return int(binary.BigEndian.Uint16(t[0:2])), true
	}
	wantProto, echo := 1, byte(8)
	if p.v6 {
		wantProto, echo = 58, 128
	}
	if proto != wantProto || t[0] != echo || int(binary.BigEndian.Uint16(t[4:6])) != p.id {
		return 0, false
	}
	return int(binary.BigEndian.Uint16(t[6:8])), true
}

// mplsLabels picks the MPLS label stack out of ICMP extensions.
func mplsLabels(exts []icmp.Extension) []int {
	var out []int
	for _, e := range exts {
		if s, ok := e.(*icmp.MPLSLabelStack); ok {
			for _, l := range s.Labels {
				out = append(out, l.Label)
			}
		}
	}
	return out
}
