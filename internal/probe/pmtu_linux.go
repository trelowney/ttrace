//go:build linux

package probe

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
	"golang.org/x/sys/unix"
)

type mtuKind int

const (
	mtuNone    mtuKind = iota // nothing came back in time
	mtuPassed                 // a router said the hop limit ran out: the packet got that far
	mtuReached                // the target answered
	mtuTooBig                 // a router (or this computer) said the packet is too big
	mtuRefused                // the target or a router refused the packet
)

type mtuAnswer struct {
	kind mtuKind
	addr net.IP
	mtu  int // with mtuTooBig: the size the router said fits, 0 = not said
}

type mtuProber struct {
	conn    *net.IPConn
	pc4     *ipv4.PacketConn
	pc6     *ipv6.PacketConn
	v6      bool
	dst     net.IP
	id, seq int
	timeout time.Duration
	buf     []byte
}

// PathMTU finds the largest packet that gets to dst without being cut into
// pieces, hop by hop, and where it gets smaller. Packets are sent with
// "don't fragment"; routers that cannot pass them answer "fragmentation
// needed" (IPv4) or "packet too big" (IPv6) with the size that fits. Where
// big packets vanish without such an answer, the size is found by trying.
// progress (may be nil) gets a short line about what is being tried.
func PathMTU(ctx context.Context, dst, src net.IP, maxTTL int, timeout time.Duration, progress func(string)) (MTUResult, error) {
	v6 := dst.To4() == nil
	network, laddr := "ip4:icmp", "0.0.0.0"
	floor, check := 68, 576 // smallest size allowed / size every IPv4 path must pass
	if v6 {
		network, laddr = "ip6:ipv6-icmp", "::"
		floor, check = 1280, 1280
	}
	if src != nil {
		if (src.To4() == nil) != v6 {
			return MTUResult{}, fmt.Errorf("source address %s is not the same IP version as the target %s", src, dst)
		}
		laddr = src.String()
	}
	c, err := net.ListenPacket(network, laddr)
	if err != nil {
		return MTUResult{}, permissionHint(err)
	}
	conn := c.(*net.IPConn)
	defer conn.Close()

	// "Don't fragment", and do not let the kernel shrink our packets to a
	// size it learned earlier: we want to see for ourselves.
	rc, err := conn.SyscallConn()
	if err != nil {
		return MTUResult{}, err
	}
	var serr error
	err = rc.Control(func(fd uintptr) {
		if v6 {
			serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_MTU_DISCOVER, unix.IPV6_PMTUDISC_PROBE)
			if serr == nil {
				serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_DONTFRAG, 1)
			}
		} else {
			serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_MTU_DISCOVER, unix.IP_PMTUDISC_PROBE)
		}
	})
	if err == nil {
		err = serr
	}
	if err != nil {
		return MTUResult{}, fmt.Errorf("cannot switch off fragmenting: %v", err)
	}

	m := &mtuProber{conn: conn, v6: v6, dst: dst, id: (os.Getpid() + 1) & 0xffff, timeout: timeout, buf: make([]byte, 65536)}
	if v6 {
		m.pc6 = ipv6.NewPacketConn(conn)
	} else {
		m.pc4 = ipv4.NewPacketConn(conn)
	}
	say := func(f string, a ...any) {
		if progress != nil {
			progress(fmt.Sprintf(f, a...))
		}
	}

	iface, size := localMTU(dst, src)
	res := MTUResult{Start: size, Iface: iface}
	silent := 0
	for ttl := 1; ttl <= maxTTL; {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		say("hop %d: trying %d bytes", ttl, size)
		a := m.try(ctx, ttl, size, 2)
		switch a.kind {
		case mtuPassed, mtuReached, mtuRefused:
			silent = 0
			res.Hops = append(res.Hops, MTUHop{TTL: ttl, Addr: a.addr, Size: size})
			res.PathMTU = size
			if a.kind != mtuPassed {
				res.Reached = a.kind == mtuReached
				return res, nil
			}
			ttl++
		case mtuTooBig:
			next := a.mtu
			if next <= 0 || next >= size {
				next = nextPlateau(size, floor)
			}
			if next < floor {
				next = floor
			}
			if next >= size {
				return res, fmt.Errorf("packets of %d bytes do not get through hop %d and a smaller size is not allowed", size, ttl)
			}
			res.Drops = append(res.Drops, MTUDrop{TTL: ttl, Addr: a.addr, From: size, To: next, Reported: true})
			size = next // the same hop again with the smaller size
		default:
			// Nothing came back. Either the hop never answers, or big
			// packets vanish here without a message (black hole).
			small := check
			if small > size {
				small = size
			}
			b := mtuAnswer{}
			if small < size {
				say("hop %d: no answer to %d bytes, trying %d", ttl, size, small)
				b = m.try(ctx, ttl, small, 2)
			}
			if b.kind == mtuNone || b.kind == mtuTooBig {
				res.Hops = append(res.Hops, MTUHop{TTL: ttl})
				if silent++; silent >= 5 {
					return res, nil // the rest of the way does not answer
				}
				ttl++
				continue
			}
			lo, hi := small, size // lo gets through, hi does not
			for hi-lo > 1 && ctx.Err() == nil {
				mid := (lo + hi) / 2
				say("hop %d: big packets vanish, trying %d bytes", ttl, mid)
				switch m.try(ctx, ttl, mid, 2).kind {
				case mtuNone, mtuTooBig:
					hi = mid
				default:
					lo = mid
				}
			}
			res.Drops = append(res.Drops, MTUDrop{TTL: ttl, Addr: b.addr, From: size, To: lo})
			size = lo
		}
	}
	return res, nil
}

// localMTU is the network card the way to dst leaves through and its MTU.
func localMTU(dst, src net.IP) (string, int) {
	network := "udp4"
	if dst.To4() == nil {
		network = "udp6"
	}
	var la *net.UDPAddr
	if src != nil {
		la = &net.UDPAddr{IP: src}
	}
	c, err := net.DialUDP(network, la, &net.UDPAddr{IP: dst, Port: 33434}) // sends nothing
	if err != nil {
		return "", 1500
	}
	local := c.LocalAddr().(*net.UDPAddr).IP
	c.Close()
	ifs, _ := net.Interfaces()
	for _, i := range ifs {
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.Equal(local) {
				return i.Name, i.MTU
			}
		}
	}
	return "", 1500
}

// try sends up to tries probes of size bytes with the given hop limit and
// returns the first answer.
func (m *mtuProber) try(ctx context.Context, ttl, size, tries int) mtuAnswer {
	for i := 0; i < tries && ctx.Err() == nil; i++ {
		if a := m.once(ttl, size); a.kind != mtuNone {
			return a
		}
	}
	return mtuAnswer{}
}

func (m *mtuProber) once(ttl, size int) mtuAnswer {
	m.seq = (m.seq + 1) & 0xffff
	hdr := 28 // IPv4 + ICMP header
	var typ icmp.Type = ipv4.ICMPTypeEcho
	if m.v6 {
		hdr, typ = 48, ipv6.ICMPTypeEchoRequest
	}
	n := size - hdr
	if n < 0 {
		n = 0
	}
	b, err := (&icmp.Message{Type: typ, Body: &icmp.Echo{ID: m.id, Seq: m.seq, Data: make([]byte, n)}}).Marshal(nil)
	if err != nil {
		return mtuAnswer{}
	}
	if m.v6 {
		err = m.pc6.SetHopLimit(ttl)
	} else {
		err = m.pc4.SetTTL(ttl)
	}
	if err != nil {
		return mtuAnswer{}
	}
	if _, err := m.conn.WriteTo(b, &net.IPAddr{IP: m.dst}); err != nil {
		if errors.Is(err, syscall.EMSGSIZE) {
			return mtuAnswer{kind: mtuTooBig} // too big already for this computer's own link
		}
		return mtuAnswer{}
	}
	deadline := time.Now().Add(m.timeout)
	for {
		m.conn.SetReadDeadline(deadline)
		k, from, err := m.conn.ReadFromIP(m.buf)
		if err != nil {
			return mtuAnswer{}
		}
		if a, ok := m.match(m.buf[:k], from.IP); ok {
			return a
		}
	}
}

// match reads one ICMP message (without the IPv4 header, which the kernel
// strips) and tells whether it answers our latest probe.
func (m *mtuProber) match(b []byte, from net.IP) (mtuAnswer, bool) {
	if len(b) < 8 {
		return mtuAnswer{}, false
	}
	typ, code := b[0], b[1]
	from = append(net.IP(nil), from...)
	echoReply, timeExceeded, unreach := byte(0), byte(11), byte(3)
	if m.v6 {
		echoReply, timeExceeded, unreach = 129, 3, 1
	}
	switch {
	case typ == echoReply:
		if from.Equal(m.dst) && int(binary.BigEndian.Uint16(b[4:6])) == m.id && int(binary.BigEndian.Uint16(b[6:8])) == m.seq {
			return mtuAnswer{kind: mtuReached, addr: from}, true
		}
	case typ == timeExceeded:
		if m.ours(b[8:]) {
			return mtuAnswer{kind: mtuPassed, addr: from}, true
		}
	case !m.v6 && typ == 3 && code == 4: // fragmentation needed, the size that fits in bytes 6-7
		if m.ours(b[8:]) {
			return mtuAnswer{kind: mtuTooBig, addr: from, mtu: int(binary.BigEndian.Uint16(b[6:8]))}, true
		}
	case m.v6 && typ == 2: // packet too big, the size that fits in bytes 4-7
		if m.ours(b[8:]) {
			return mtuAnswer{kind: mtuTooBig, addr: from, mtu: int(binary.BigEndian.Uint32(b[4:8]))}, true
		}
	case typ == unreach:
		if m.ours(b[8:]) {
			return mtuAnswer{kind: mtuRefused, addr: from}, true
		}
	}
	return mtuAnswer{}, false
}

// ours checks that a packet quoted in an ICMP error is our latest probe.
func (m *mtuProber) ours(data []byte) bool {
	var dst net.IP
	var t []byte
	if m.v6 {
		if len(data) < 48 || data[6] != 58 {
			return false
		}
		dst, t = net.IP(data[24:40]), data[40:]
		if t[0] != 128 {
			return false
		}
	} else {
		if len(data) < 20 {
			return false
		}
		ihl := int(data[0]&0x0f) * 4
		if ihl < 20 || len(data) < ihl+8 || data[9] != 1 {
			return false
		}
		dst, t = net.IP(data[16:20]), data[ihl:]
		if t[0] != 8 {
			return false
		}
	}
	return dst.Equal(m.dst) && int(binary.BigEndian.Uint16(t[4:6])) == m.id && int(binary.BigEndian.Uint16(t[6:8])) == m.seq
}
