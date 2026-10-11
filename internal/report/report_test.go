package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/trelowney/ttrace/internal/trace"
)

func sample() trace.Snapshot {
	return trace.Snapshot{Target: "example.com", IP: "192.0.2.10", Family: "IPv4", Round: 3, Reached: true,
		Hops: []trace.Hop{
			{TTL: 1, Addr: "192.168.1.1", Private: true, Sent: 3, Recv: 3, Avg: 1},
			{TTL: 2, Addr: "198.51.100.1", ASN: "64500", ASName: "EXAMPLE-NET", Country: "CZ", Sent: 3, Recv: 3, Avg: 5,
				MPLS: []int{24001}, Others: []trace.Other{{Addr: "198.51.100.2", ASN: "64500", Seen: 1}}},
			{TTL: 3, Addr: "192.0.2.10", Host: "example.com", Sent: 3, Recv: 3, Avg: 9},
		},
		Links: []trace.Link{
			{From: "", To: "192.168.1.1", ToTTL: 1, Seen: 3},
			{From: "192.168.1.1", To: "198.51.100.1", FromTTL: 1, ToTTL: 2, Seen: 2},
			{From: "192.168.1.1", To: "198.51.100.2", FromTTL: 1, ToTTL: 2, Seen: 1},
			{From: "198.51.100.1", To: "192.0.2.10", FromTTL: 2, ToTTL: 3, Seen: 2},
		}}
}

func TestDot(t *testing.T) {
	var b bytes.Buffer
	Dot(&b, []trace.Snapshot{sample()}, NameHost, true)
	out := b.String()
	for _, want := range []string{"digraph ttrace {", `subgraph "cluster_AS64500"`, `"192.168.1.1" -> "198.51.100.2";`, `"" -> "192.168.1.1";`, "penwidth=2"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

func TestTextShowsOthersAndMPLS(t *testing.T) {
	var b bytes.Buffer
	Text(&b, sample(), NameHost, true, false)
	out := b.String()
	for _, want := range []string{"[MPLS 24001]", "198.51.100.2", "also answered once"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}
