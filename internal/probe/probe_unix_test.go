//go:build !windows

package probe

import (
	"encoding/binary"
	"net"
	"testing"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// quoted builds the start of an IPv4 packet as routers quote it in ICMP errors.
func quoted(proto byte, dst net.IP, transport []byte) []byte {
	b := make([]byte, 20, 20+len(transport))
	b[0] = 0x45
	b[9] = proto
	copy(b[16:20], dst.To4())
	return append(b, transport...)
}

func TestParseUDPWithMPLS(t *testing.T) {
	dst := net.ParseIP("192.0.2.10")
	p := &unixProber{dst: dst, proto: "udp", port: 33434}
	udp := make([]byte, 8)
	binary.BigEndian.PutUint16(udp[0:2], 40000)
	binary.BigEndian.PutUint16(udp[2:4], 33434)
	m := icmp.Message{Type: ipv4.ICMPTypeTimeExceeded, Body: &icmp.TimeExceeded{
		Data: quoted(17, dst, udp),
		Extensions: []icmp.Extension{&icmp.MPLSLabelStack{Class: 1, Type: 1,
			Labels: []icmp.MPLSLabel{{Label: 24001, S: false, TTL: 1}, {Label: 16005, S: true, TTL: 1}}}},
	}}
	b, err := m.Marshal(nil)
	if err != nil {
		t.Fatal(err)
	}
	key, r, ok := p.parse(1, b, net.ParseIP("198.51.100.1"), time.Now())
	if !ok || key != 40000 || r.reached || r.unreach {
		t.Fatalf("key=%d ok=%v r=%+v", key, ok, r)
	}
	if len(r.mpls) != 2 || r.mpls[0] != 24001 || r.mpls[1] != 16005 {
		t.Errorf("mpls: %v", r.mpls)
	}
	// another destination port is somebody else's probe
	binary.BigEndian.PutUint16(udp[2:4], 53)
	if _, ok := p.inner(quoted(17, dst, udp)); ok {
		t.Error("foreign port accepted")
	}
}

func TestParsePortUnreachableFromTarget(t *testing.T) {
	dst := net.ParseIP("192.0.2.10")
	p := &unixProber{dst: dst, proto: "udp", port: 33434}
	udp := make([]byte, 8)
	binary.BigEndian.PutUint16(udp[0:2], 40001)
	binary.BigEndian.PutUint16(udp[2:4], 33434)
	m := icmp.Message{Type: ipv4.ICMPTypeDestinationUnreachable, Code: 3, Body: &icmp.DstUnreach{Data: quoted(17, dst, udp)}}
	b, _ := m.Marshal(nil)
	key, r, ok := p.parse(1, b, dst, time.Now())
	if !ok || key != 40001 || !r.reached || r.unreach {
		t.Fatalf("key=%d ok=%v r=%+v", key, ok, r)
	}
}

func TestInnerICMPAndTCP(t *testing.T) {
	dst := net.ParseIP("192.0.2.10")
	echo := []byte{8, 0, 0, 0, 0x12, 0x34, 0, 7}
	p := &unixProber{dst: dst, proto: "icmp", id: 0x1234}
	if seq, ok := p.inner(quoted(1, dst, echo)); !ok || seq != 7 {
		t.Errorf("icmp: %d %v", seq, ok)
	}
	if _, ok := p.inner(quoted(1, net.ParseIP("192.0.2.11"), echo)); ok {
		t.Error("other destination accepted")
	}
	tcp := []byte{0x9c, 0x40, 0, 80, 0, 0, 0, 0}
	pt := &unixProber{dst: dst, proto: "tcp", port: 80}
	if port, ok := pt.inner(quoted(6, dst, tcp)); !ok || port != 40000 {
		t.Errorf("tcp: %d %v", port, ok)
	}
	if _, ok := pt.inner(quoted(17, dst, tcp)); ok {
		t.Error("udp accepted as tcp")
	}
}
