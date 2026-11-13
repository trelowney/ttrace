package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/trelowney/ttrace/internal/probe"
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

func TestShortAndIXLabels(t *testing.T) {
	if got := ShortAS("CLOUDFLARENET - Cloudflare, Inc."); got != "CLOUDFLARENET" {
		t.Errorf("short: %q", got)
	}
	if got := ShortAS("GOOGLE"); got != "GOOGLE" {
		t.Errorf("short without description: %q", got)
	}
	if got := ASLabel("CLOUDFLARENET - Cloudflare, Inc.", "Frys-IX", true); got != "Frys-IX (CLOUDFLARENET)" {
		t.Errorf("ix: %q", got)
	}
	if got := ASLabel("", "Frys-IX", true); got != "Frys-IX" {
		t.Errorf("ix without member: %q", got)
	}
	if got := ASLabel("CLOUDFLARENET - Cloudflare, Inc.", "", false); got != "CLOUDFLARENET - Cloudflare, Inc." {
		t.Errorf("wide: %q", got)
	}
}

func TestTextPathDSCPJump(t *testing.T) {
	s := sample()
	s.TOSSent = 184
	ef, zero := 184, 1 // 1 = DSCP 0 with an ECN bit, the ECN bit must not matter
	s.Hops[0].TOSSeen = &ef
	s.Hops[1].TOSSeen = &zero
	s.Hops[2].Jump = 27
	s.ASPath = []trace.PathStep{{ASN: "64500", Name: "EXAMPLE-NET - Example"}, {IX: "Frys-IX"}, {ASN: "13335", Name: "CLOUDFLARENET"}}
	var b bytes.Buffer
	Text(&b, s, NameHost, true, false)
	out := b.String()
	for _, want := range []string{"AS path: AS64500 EXAMPLE-NET → Frys-IX → AS13335 CLOUDFLARENET",
		"[DSCP changed on the way here: 46 (EF) → 0 (CS0)]", "▲ +27.0 ms"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if n := DSCPNotes(s); len(n) != 1 {
		t.Errorf("notes: %v", n)
	}
}

func TestCompare(t *testing.T) {
	a, b := sample(), sample()
	b.Hops = append([]trace.Hop(nil), b.Hops...)
	b.Hops[1].Addr, b.Hops[1].ASN = "198.51.100.9", "64501"
	b.Hops[2].Loss, b.Hops[2].Avg = 10, 40
	var out bytes.Buffer
	if err := Compare(&out, "rano.json", []trace.Snapshot{a}, "vecer.json", []trace.Snapshot{b}, NameIP); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"hop 2 (other network AS64500 → AS64501)", "hop 3 +10.0 %", "hop 3 +31.0 ms", "same"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in\n%s", want, out.String())
		}
	}
	out.Reset()
	Compare(&out, "a", []trace.Snapshot{a}, "b", []trace.Snapshot{a}, NameIP)
	if !strings.Contains(out.String(), "No noticeable differences.") {
		t.Errorf("same input:\n%s", out.String())
	}
}

func TestMTUText(t *testing.T) {
	r := MTUReport{Target: "example.com", IP: "192.0.2.10", Family: "IPv4", Start: 1500, Iface: "eth0", PathMTU: 1400, Reached: true,
		Hops: []MTUHop{
			{TTL: 1, Addr: "192.0.2.1", Host: "gw.example.net", Size: 1500},
			{TTL: 2},
			{TTL: 3, Addr: "198.51.100.1", ASN: "64500", ASName: "EXAMPLENET - Example, Inc.", Size: 1492},
			{TTL: 4, Addr: "192.0.2.10", Size: 1400},
		},
		Drops: []probe.MTUDrop{{TTL: 3, From: 1500, To: 1492, Reported: true}, {TTL: 4, From: 1492, To: 1400}},
	}
	var b strings.Builder
	MTUText(&b, r, NameHost, true, false)
	out := b.String()
	for _, want := range []string{
		"starting with 1500 bytes (MTU of eth0)",
		"gw.example.net",
		"  2. ???",
		"AS64500 EXAMPLENET",
		"▼ smaller here: 1500 → 1492 bytes (the router said so)",
		"▼ smaller here: 1492 → 1400 bytes — bigger packets vanish",
		"Path MTU: 1400 bytes",
		"black hole makes big downloads",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Example, Inc.") {
		t.Errorf("AS name not shortened:\n%s", out)
	}
}
