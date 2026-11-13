package probe

import "net"

// MTUHop is one hop on the way and the largest packet that got to it.
type MTUHop struct {
	TTL  int    `json:"ttl"`
	Addr net.IP `json:"addr,omitempty"` // nil when the hop never answered
	Size int    `json:"size,omitempty"` // largest packet in bytes that reached this hop
}

// MTUDrop is a place where the largest packet that gets through becomes smaller.
type MTUDrop struct {
	TTL  int    `json:"ttl"`            // the drop is on the way to this hop
	Addr net.IP `json:"addr,omitempty"` // router that reported it (Reported) or the hop that stopped answering big packets
	From int    `json:"from"`
	To   int    `json:"to"`
	// Reported is true when a router said so (ICMP "fragmentation needed"
	// or "packet too big"). False means big packets simply vanished without
	// a message: a so-called PMTU black hole.
	Reported bool `json:"reported"`
}

// MTUResult is what PathMTU found out.
type MTUResult struct {
	Start   int       `json:"start"`     // size we started with: the MTU of the local network card
	Iface   string    `json:"interface"` // that network card
	PathMTU int       `json:"path_mtu"`  // largest packet that reached the last answering hop
	Reached bool      `json:"reached"`   // the target itself answered
	Hops    []MTUHop  `json:"hops"`
	Drops   []MTUDrop `json:"drops,omitempty"`
}

// plateaus are the usual MTU sizes (RFC 1191), used when a router reports
// "fragmentation needed" without saying how much fits.
var plateaus = []int{65535, 32000, 17914, 9000, 8166, 4352, 2002, 1500, 1492, 1480, 1450, 1400, 1280, 1006, 576, 508, 296, 68}

// nextPlateau is the largest usual size below size, but not below min.
func nextPlateau(size, min int) int {
	for _, p := range plateaus {
		if p < size {
			if p < min {
				return min
			}
			return p
		}
	}
	return min
}
