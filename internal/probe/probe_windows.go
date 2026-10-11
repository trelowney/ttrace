//go:build windows

package probe

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows does not let normal users read ICMP errors from raw sockets, so we
// use the ICMP helper API from iphlpapi.dll. It works without admin rights.
var (
	iphlpapi            = windows.NewLazySystemDLL("iphlpapi.dll")
	procIcmpCreateFile  = iphlpapi.NewProc("IcmpCreateFile")
	procIcmp6CreateFile = iphlpapi.NewProc("Icmp6CreateFile")
	procIcmpCloseHandle = iphlpapi.NewProc("IcmpCloseHandle")
	procIcmpSendEcho2   = iphlpapi.NewProc("IcmpSendEcho2")
	procIcmp6SendEcho2  = iphlpapi.NewProc("Icmp6SendEcho2")
)

// IP_OPTION_INFORMATION
type ipOptionInformation struct {
	TTL         uint8
	Tos         uint8
	Flags       uint8
	OptionsSize uint8
	OptionsData uintptr
}

// IP_STATUS values from ipexport.h
const (
	ipSuccess              = 0
	ipDestNetUnreachable   = 11002
	ipDestHostUnreachable  = 11003
	ipDestProtUnreachable  = 11004
	ipDestPortUnreachable  = 11005
	ipTTLExpiredTransit    = 11013
	ipTTLExpiredReassembly = 11014
	ipDestUnreachable      = 11040 // IPv6
	ipTimeExceeded         = 11041 // IPv6
)

type winProber struct {
	dst     net.IP
	v6      bool
	tos     uint8
	payload []byte
}

// New prepares probing towards dst and checks that the ICMP API is usable.
func New(dst net.IP, opts Options) (Prober, error) {
	if opts.Source != nil {
		return nil, errors.New("choosing the source address (-a) is not supported on Windows yet")
	}
	v6 := dst.To4() == nil
	p := &winProber{dst: dst, v6: v6, tos: uint8(opts.TOS), payload: makePayload(opts.Size, v6)}
	h, err := p.open()
	if err != nil {
		return nil, err
	}
	procIcmpCloseHandle.Call(h)
	return p, nil
}

func (p *winProber) open() (uintptr, error) {
	proc := procIcmpCreateFile
	if p.v6 {
		proc = procIcmp6CreateFile
	}
	if err := proc.Find(); err != nil {
		return 0, err
	}
	h, _, err := proc.Call()
	if windows.Handle(h) == windows.InvalidHandle {
		return 0, err
	}
	return h, nil
}

func (p *winProber) Close() error { return nil }

// Probe uses its own handle so that probes for different TTLs can run in parallel.
func (p *winProber) Probe(ctx context.Context, ttl, seq int, timeout time.Duration) Result {
	res := Result{TTL: ttl, Seq: seq}
	h, err := p.open()
	if err != nil {
		return res
	}
	defer procIcmpCloseHandle.Call(h)

	opts := ipOptionInformation{TTL: uint8(ttl), Tos: p.tos}
	req := p.payload
	buf := make([]byte, 1024)
	ms := uintptr(timeout.Milliseconds())

	var addr net.IP
	var status uint32
	start := time.Now()
	if p.v6 {
		var src, dst windows.RawSockaddrInet6
		src.Family = windows.AF_INET6
		dst.Family = windows.AF_INET6
		copy(dst.Addr[:], p.dst.To16())
		procIcmp6SendEcho2.Call(h, 0, 0, 0,
			uintptr(unsafe.Pointer(&src)), uintptr(unsafe.Pointer(&dst)),
			uintptr(unsafe.Pointer(&req[0])), uintptr(len(req)),
			uintptr(unsafe.Pointer(&opts)),
			uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), ms)
		res.RTT = time.Since(start)
		// ICMPV6_ECHO_REPLY: IPV6_ADDRESS_EX is packed (port 2, flowinfo 4,
		// addr 16, scope 4), then Status and RoundTripTime.
		addr = append(net.IP(nil), buf[6:22]...)
		status = binary.LittleEndian.Uint32(buf[26:30])
	} else {
		dst := binary.LittleEndian.Uint32(p.dst.To4()) // IPAddr keeps network byte order in memory
		procIcmpSendEcho2.Call(h, 0, 0, 0, uintptr(dst),
			uintptr(unsafe.Pointer(&req[0])), uintptr(len(req)),
			uintptr(unsafe.Pointer(&opts)),
			uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), ms)
		res.RTT = time.Since(start)
		// ICMP_ECHO_REPLY: Address, Status, RoundTripTime (same offsets on 32 and 64 bit)
		addr = net.IPv4(buf[0], buf[1], buf[2], buf[3])
		status = binary.LittleEndian.Uint32(buf[4:8])
	}
	if ctx.Err() != nil || addr.IsUnspecified() {
		return res
	}

	switch status {
	case ipSuccess:
		res.Addr, res.Reached = addr, true
	case ipTTLExpiredTransit, ipTTLExpiredReassembly, ipTimeExceeded:
		res.Addr = addr
	case ipDestNetUnreachable, ipDestHostUnreachable, ipDestProtUnreachable, ipDestPortUnreachable, ipDestUnreachable:
		res.Addr, res.Unreach = addr, true
	default:
		res.RTT = 0
	}
	return res
}
