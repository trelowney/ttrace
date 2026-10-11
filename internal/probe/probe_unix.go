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
}

type unixProber struct {
	dst  net.IP
	v6      bool
	id      int
	conn    *icmp.PacketConn
	payload []byte

	wmu sync.Mutex // set TTL + write must not interleave between probes

	pmu     sync.Mutex
	pending map[int]chan reply

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
	c, err := icmp.ListenPacket(network, laddr)
	if err != nil {
		return nil, permissionHint(err)
	}
	if opts.TOS != 0 {
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
	p := &unixProber{
		dst:     dst,
		v6:      v6,
		id:      os.Getpid() & 0xffff,
		conn:    c,
		payload: makePayload(opts.Size, v6),
		pending: map[int]chan reply{},
		closed:  make(chan struct{}),
	}
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
	res := Result{TTL: ttl, Seq: seq}
	seq &= 0xffff
	ch := make(chan reply, 1)
	p.pmu.Lock()
	p.pending[seq] = ch
	p.pmu.Unlock()
	defer func() {
		p.pmu.Lock()
		delete(p.pending, seq)
		p.pmu.Unlock()
	}()

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

	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case r := <-ch:
		res.Addr = r.addr
		res.RTT = r.at.Sub(start)
		res.Reached = r.reached
		res.Unreach = r.unreach
	case <-t.C:
	case <-ctx.Done():
	}
	return res
}

func (p *unixProber) deliver(seq int, r reply) {
	p.pmu.Lock()
	ch := p.pending[seq]
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
		m, err := icmp.ParseMessage(proto, buf[:n])
		if err != nil {
			continue
		}
		from := append(net.IP(nil), ipa.IP...)
		switch m.Type {
		case ipv4.ICMPTypeEchoReply, ipv6.ICMPTypeEchoReply:
			e, ok := m.Body.(*icmp.Echo)
			if !ok || e.ID != p.id || !from.Equal(p.dst) {
				continue
			}
			p.deliver(e.Seq, reply{addr: from, at: at, reached: true})
		case ipv4.ICMPTypeTimeExceeded, ipv6.ICMPTypeTimeExceeded:
			te, ok := m.Body.(*icmp.TimeExceeded)
			if !ok {
				continue
			}
			if seq, ok := p.inner(te.Data); ok {
				p.deliver(seq, reply{addr: from, at: at})
			}
		case ipv4.ICMPTypeDestinationUnreachable, ipv6.ICMPTypeDestinationUnreachable:
			du, ok := m.Body.(*icmp.DstUnreach)
			if !ok {
				continue
			}
			if seq, ok := p.inner(du.Data); ok {
				p.deliver(seq, reply{addr: from, at: at, unreach: true})
			}
		}
	}
}

// inner reads our echo request quoted inside an ICMP error and returns its
// sequence number when it belongs to this prober.
func (p *unixProber) inner(data []byte) (int, bool) {
	if p.v6 {
		if len(data) < 48 || data[6] != 58 || data[40] != 128 {
			return 0, false
		}
		if !net.IP(data[24:40]).Equal(p.dst) {
			return 0, false
		}
		if int(binary.BigEndian.Uint16(data[44:46])) != p.id {
			return 0, false
		}
		return int(binary.BigEndian.Uint16(data[46:48])), true
	}
	if len(data) < 20 {
		return 0, false
	}
	ihl := int(data[0]&0x0f) * 4
	if ihl < 20 || len(data) < ihl+8 || data[9] != 1 || data[ihl] != 8 {
		return 0, false
	}
	if !net.IP(data[16:20]).Equal(p.dst.To4()) {
		return 0, false
	}
	if int(binary.BigEndian.Uint16(data[ihl+4:ihl+6])) != p.id {
		return 0, false
	}
	return int(binary.BigEndian.Uint16(data[ihl+6 : ihl+8])), true
}
