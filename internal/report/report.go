// Package report formats snapshots as text tables and JSON.
package report

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/trelowney/ttrace/internal/trace"
)

// NameMode chooses what is shown in the host column.
type NameMode int

const (
	NameHost NameMode = iota // hostname, IP when there is none
	NameIP
	NameBoth
)

func (m NameMode) String() string {
	return [...]string{"host", "ip", "both"}[m]
}

// Label is the host column text.
func Label(h trace.Hop, m NameMode) string {
	if h.Hidden {
		return "(hidden)"
	}
	if h.Addr == "" {
		return "???"
	}
	switch {
	case m == NameIP || h.Host == "":
		return h.Addr
	case m == NameBoth:
		return h.Host + " (" + h.Addr + ")"
	}
	return h.Host
}

// OtherLabel is the host text of an additional router on the same hop.
func OtherLabel(o trace.Other, m NameMode) string {
	switch {
	case m == NameIP || o.Host == "":
		return o.Addr
	case m == NameBoth:
		return o.Host + " (" + o.Addr + ")"
	}
	return o.Host
}

// OtherASN is the ASN column text of an additional router.
func OtherASN(o trace.Other) string {
	return ASN(trace.Hop{ASN: o.ASN, Private: o.Private})
}

// ShortAS cuts the description off an AS name the way Team Cymru writes
// them: "CLOUDFLARENET - Cloudflare, Inc." becomes "CLOUDFLARENET".
func ShortAS(name string) string {
	if i := strings.Index(name, " - "); i > 0 {
		return name[:i]
	}
	return name
}

// ASLabel is the AS name column text. On an internet exchange it names the
// exchange and the member network using the address, e.g. "Frys-IX (CLOUDFLARENET)".
func ASLabel(name, ix string, short bool) string {
	if short {
		name = ShortAS(name)
	}
	switch {
	case ix == "":
		return name
	case name == "":
		return ix
	}
	return ix + " (" + name + ")"
}

// ASPath is the route as a chain of networks, e.g.
// "AS64500 EXAMPLE → Frys-IX → AS13335 CLOUDFLARENET"; "" when unknown.
func ASPath(s trace.Snapshot) string {
	var parts []string
	for _, p := range s.ASPath {
		switch {
		case p.IX != "":
			parts = append(parts, p.IX)
		case p.Name != "":
			parts = append(parts, "AS"+p.ASN+" "+ShortAS(p.Name))
		default:
			parts = append(parts, "AS"+p.ASN)
		}
	}
	return strings.Join(parts, " → ")
}

// dscpNames are the usual names of DSCP values.
var dscpNames = map[int]string{0: "CS0", 8: "CS1", 10: "AF11", 12: "AF12", 14: "AF13", 16: "CS2", 18: "AF21", 20: "AF22", 22: "AF23",
	24: "CS3", 26: "AF31", 28: "AF32", 30: "AF33", 32: "CS4", 34: "AF41", 36: "AF42", 38: "AF43", 40: "CS5", 44: "VA", 46: "EF", 48: "CS6", 56: "CS7"}

// DSCP writes a TOS byte's DSCP value with its name, e.g. "46 (EF)".
func DSCP(tos int) string {
	d := tos >> 2
	if n, ok := dscpNames[d]; ok {
		return fmt.Sprintf("%d (%s)", d, n)
	}
	return fmt.Sprint(d)
}

// DSCPNotes finds the hops where the DSCP value of our probes was changed
// on the way: routers quote our packet as it reached them, so a different
// value than at the hop before means a router in between rewrote it. The
// ECN bits are ignored. Returns a note per TTL.
func DSCPNotes(s trace.Snapshot) map[int]string {
	notes := map[int]string{}
	prev := s.TOSSent
	for _, h := range s.Hops {
		if h.TOSSeen == nil || h.Hidden {
			continue
		}
		if *h.TOSSeen>>2 != prev>>2 {
			notes[h.TTL] = "DSCP changed on the way here: " + DSCP(prev) + " → " + DSCP(*h.TOSSeen)
		}
		prev = *h.TOSSeen
	}
	return notes
}

// Jump is the mark of a latency rise, e.g. "▲ +27.0 ms"; "" when none.
func Jump(h trace.Hop) string {
	if h.Jump <= 0 {
		return ""
	}
	return fmt.Sprintf("▲ +%.1f ms", h.Jump)
}

// MPLS formats a label stack, e.g. "MPLS 24001 16005"; empty without labels.
func MPLS(labels []int) string {
	if len(labels) == 0 {
		return ""
	}
	parts := make([]string, len(labels))
	for i, l := range labels {
		parts[i] = strconv.Itoa(l)
	}
	return "MPLS " + strings.Join(parts, " ")
}

// Seen describes how often an additional router answered.
func Seen(o trace.Other) string {
	if o.Seen == 1 {
		return "also answered once"
	}
	return fmt.Sprintf("also answered %d times", o.Seen)
}

// ASN column text: "AS15169", "private" or empty while unknown.
func ASN(h trace.Hop) string {
	switch {
	case h.Private:
		return "private"
	case h.ASN != "":
		return "AS" + h.ASN
	}
	return ""
}

// Fit cuts s to n runes (with an ellipsis) or pads it with spaces.
func Fit(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) > n {
		if n <= 1 {
			return string(r[:n])
		}
		return string(r[:n-1]) + "…"
	}
	return s + strings.Repeat(" ", n-len(r))
}

// Stats is the right-hand part of a row (fixed width, see StatsHeader).
func Stats(h trace.Hop) string {
	if h.Recv == 0 {
		return fmt.Sprintf("%5.1f%% %4d %6s %6s %6s %6s %6s", h.Loss, h.Sent, "-", "-", "-", "-", "-")
	}
	return fmt.Sprintf("%5.1f%% %4d %6.1f %6.1f %6.1f %6.1f %6.1f", h.Loss, h.Sent, h.Last, h.Avg, h.Best, h.Worst, h.StDev)
}

const StatsHeader = " Loss%  Snt   Last    Avg   Best   Wrst  StDev"

// Text writes a plain table. wide = never shorten host and AS names.
func Text(w io.Writer, s trace.Snapshot, m NameMode, showAS, wide bool) {
	host, name := 20, 20
	for _, h := range s.Hops {
		if l := len([]rune(Label(h, m))); l > host {
			host = l
		}
		if l := len([]rune(ASLabel(h.ASName, h.IX, !wide))); l > name {
			name = l
		}
	}
	if host > 50 && !wide {
		host = 50
	}
	if name > 28 && !wide {
		name = 28
	}
	fmt.Fprintf(w, "ttrace to %s (%s, %s%s), %d rounds\n", s.Target, s.IP, s.Family, probeNote(s), s.Round)
	head := fmt.Sprintf("%3s  %s", "#", Fit("Host", host))
	if showAS {
		head += fmt.Sprintf("  %-9s %s %-2s", "ASN", Fit("AS name", name), "CC")
	}
	fmt.Fprintln(w, head+"  "+StatsHeader)
	dscp := DSCPNotes(s)
	for _, h := range s.Hops {
		row := fmt.Sprintf("%3d. %s", h.TTL, Fit(Label(h, m), host))
		if showAS {
			row += fmt.Sprintf("  %-9s %s %-2s", ASN(h), Fit(ASLabel(h.ASName, h.IX, !wide), name), h.Country)
		}
		row += "  " + Stats(h)
		if j := Jump(h); j != "" {
			row += "  " + j
		}
		fmt.Fprintln(w, row)
		pad := strings.Repeat(" ", 5)
		if l := MPLS(h.MPLS); l != "" {
			fmt.Fprintln(w, pad+"  ["+l+"]")
		}
		if n := dscp[h.TTL]; n != "" {
			fmt.Fprintln(w, pad+"  ["+n+"]")
		}
		for _, o := range h.Others {
			row := pad + Fit(OtherLabel(o, m), host)
			if showAS {
				row += fmt.Sprintf("  %-9s %s %-2s", OtherASN(o), Fit(ASLabel(o.ASName, o.IX, !wide), name), o.Country)
			}
			row += "  " + Seen(o)
			if l := MPLS(o.MPLS); l != "" {
				row += "  [" + l + "]"
			}
			fmt.Fprintln(w, row)
		}
	}
	if !s.Reached {
		fmt.Fprintln(w, "Target did not answer.")
	}
	if p := ASPath(s); showAS && p != "" {
		fmt.Fprintln(w, "AS path: "+p)
	}
}

func probeNote(s trace.Snapshot) string {
	if s.Probe == "" {
		return ""
	}
	return ", " + s.Probe
}

func ms(v float64, recv int) string {
	if recv == 0 {
		return ""
	}
	return strconv.FormatFloat(v, 'f', 2, 64)
}

// CSV writes one line per hop (of all targets) with a header line.
func CSV(w io.Writer, snaps []trace.Snapshot) error {
	c := csv.NewWriter(w)
	c.Write([]string{"target", "ip", "ttl", "host", "addr", "asn", "as_name", "country", "prefix", "registry",
		"loss_pct", "sent", "recv", "last_ms", "avg_ms", "best_ms", "worst_ms", "stdev_ms", "mpls", "other_addrs", "place", "lat", "lon", "ix", "jump_ms"})
	for _, s := range snaps {
		for _, h := range s.Hops {
			host := h.Host
			if h.Hidden {
				host = "(hidden)"
			}
			var others []string
			for _, o := range h.Others {
				others = append(others, o.Addr)
			}
			jump := ""
			if h.Jump > 0 {
				jump = strconv.FormatFloat(h.Jump, 'f', 2, 64)
			}
			place, lat, lon := "", "", ""
			if h.Place != nil {
				place = h.Place.Name()
				lat, lon = strconv.FormatFloat(h.Place.Lat, 'f', 4, 64), strconv.FormatFloat(h.Place.Lon, 'f', 4, 64)
			}
			c.Write([]string{s.Target, s.IP, strconv.Itoa(h.TTL), host, h.Addr, ASN(h), h.ASName, h.Country, h.Prefix, h.Registry,
				strconv.FormatFloat(h.Loss, 'f', 1, 64), strconv.Itoa(h.Sent), strconv.Itoa(h.Recv),
				ms(h.Last, h.Recv), ms(h.Avg, h.Recv), ms(h.Best, h.Recv), ms(h.Worst, h.Recv), ms(h.StDev, h.Recv),
				strings.TrimPrefix(MPLS(h.MPLS), "MPLS "), strings.Join(others, " "), place, lat, lon, h.IX, jump})
		}
	}
	c.Flush()
	return c.Error()
}

// Markdown writes a table for pasting into tickets, chats and wikis.
func Markdown(w io.Writer, s trace.Snapshot, m NameMode, showAS bool) {
	esc := func(v string) string { return strings.ReplaceAll(v, "|", "\\|") }
	fmt.Fprintf(w, "**ttrace to %s** (%s, %s%s), %d rounds\n\n", esc(s.Target), s.IP, s.Family, probeNote(s), s.Round)
	if showAS {
		fmt.Fprintln(w, "| # | Host | ASN | AS name | CC | Loss % | Sent | Last | Avg | Best | Worst | StDev |")
		fmt.Fprintln(w, "|--:|:--|:--|:--|:--|--:|--:|--:|--:|--:|--:|--:|")
	} else {
		fmt.Fprintln(w, "| # | Host | Loss % | Sent | Last | Avg | Best | Worst | StDev |")
		fmt.Fprintln(w, "|--:|:--|--:|--:|--:|--:|--:|--:|--:|")
	}
	dscp := DSCPNotes(s)
	for _, h := range s.Hops {
		label := esc(Label(h, m))
		if l := MPLS(h.MPLS); l != "" {
			label += " [" + l + "]"
		}
		if j := Jump(h); j != "" {
			label += " " + j
		}
		if n := dscp[h.TTL]; n != "" {
			label += "<br>" + n
		}
		for _, o := range h.Others {
			label += "<br>also: " + esc(OtherLabel(o, m))
			if showAS && OtherASN(o) != "" {
				label += " (" + OtherASN(o) + ")"
			}
		}
		row := fmt.Sprintf("| %d | %s |", h.TTL, label)
		if showAS {
			row += fmt.Sprintf(" %s | %s | %s |", ASN(h), esc(ASLabel(h.ASName, h.IX, false)), h.Country)
		}
		row += fmt.Sprintf(" %.1f | %d | %s | %s | %s | %s | %s |", h.Loss, h.Sent,
			ms(h.Last, h.Recv), ms(h.Avg, h.Recv), ms(h.Best, h.Recv), ms(h.Worst, h.Recv), ms(h.StDev, h.Recv))
		fmt.Fprintln(w, row)
	}
	if p := ASPath(s); showAS && p != "" {
		fmt.Fprintln(w, "\n**AS path:** "+esc(p))
	}
}

// JSON writes one snapshot as JSON (indent=false gives one line, for streaming).
func JSON(w io.Writer, s trace.Snapshot, indent bool) error {
	enc := json.NewEncoder(w)
	if indent {
		enc.SetIndent("", "  ")
	}
	return enc.Encode(s)
}

// JSONList writes several snapshots as one indented JSON array.
func JSONList(w io.Writer, snaps []trace.Snapshot) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(snaps)
}
