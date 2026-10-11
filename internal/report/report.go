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
		if l := len([]rune(h.ASName)); wide && l > name {
			name = l
		}
	}
	if host > 50 && !wide {
		host = 50
	}
	fmt.Fprintf(w, "ttrace to %s (%s, %s), %d rounds\n", s.Target, s.IP, s.Family, s.Round)
	head := fmt.Sprintf("%3s  %s", "#", Fit("Host", host))
	if showAS {
		head += fmt.Sprintf("  %-9s %s %-2s", "ASN", Fit("AS name", name), "CC")
	}
	fmt.Fprintln(w, head+"  "+StatsHeader)
	for _, h := range s.Hops {
		row := fmt.Sprintf("%3d. %s", h.TTL, Fit(Label(h, m), host))
		if showAS {
			row += fmt.Sprintf("  %-9s %s %-2s", ASN(h), Fit(h.ASName, name), h.Country)
		}
		fmt.Fprintln(w, row+"  "+Stats(h))
	}
	if !s.Reached {
		fmt.Fprintln(w, "Target did not answer.")
	}
}

func ms(v float64, recv int) string {
	if recv == 0 {
		return ""
	}
	return strconv.FormatFloat(v, 'f', 2, 64)
}

// CSV writes one line per hop with a header line.
func CSV(w io.Writer, s trace.Snapshot) error {
	c := csv.NewWriter(w)
	c.Write([]string{"target", "ip", "ttl", "host", "addr", "asn", "as_name", "country", "prefix", "registry",
		"loss_pct", "sent", "recv", "last_ms", "avg_ms", "best_ms", "worst_ms", "stdev_ms"})
	for _, h := range s.Hops {
		host := h.Host
		if h.Hidden {
			host = "(hidden)"
		}
		c.Write([]string{s.Target, s.IP, strconv.Itoa(h.TTL), host, h.Addr, ASN(h), h.ASName, h.Country, h.Prefix, h.Registry,
			strconv.FormatFloat(h.Loss, 'f', 1, 64), strconv.Itoa(h.Sent), strconv.Itoa(h.Recv),
			ms(h.Last, h.Recv), ms(h.Avg, h.Recv), ms(h.Best, h.Recv), ms(h.Worst, h.Recv), ms(h.StDev, h.Recv)})
	}
	c.Flush()
	return c.Error()
}

// Markdown writes a table for pasting into tickets, chats and wikis.
func Markdown(w io.Writer, s trace.Snapshot, m NameMode, showAS bool) {
	esc := func(v string) string { return strings.ReplaceAll(v, "|", "\\|") }
	fmt.Fprintf(w, "**ttrace to %s** (%s, %s), %d rounds\n\n", esc(s.Target), s.IP, s.Family, s.Round)
	if showAS {
		fmt.Fprintln(w, "| # | Host | ASN | AS name | CC | Loss % | Sent | Last | Avg | Best | Worst | StDev |")
		fmt.Fprintln(w, "|--:|:--|:--|:--|:--|--:|--:|--:|--:|--:|--:|--:|")
	} else {
		fmt.Fprintln(w, "| # | Host | Loss % | Sent | Last | Avg | Best | Worst | StDev |")
		fmt.Fprintln(w, "|--:|:--|--:|--:|--:|--:|--:|--:|--:|")
	}
	for _, h := range s.Hops {
		row := fmt.Sprintf("| %d | %s |", h.TTL, esc(Label(h, m)))
		if showAS {
			row += fmt.Sprintf(" %s | %s | %s |", ASN(h), esc(h.ASName), h.Country)
		}
		row += fmt.Sprintf(" %.1f | %d | %s | %s | %s | %s | %s |", h.Loss, h.Sent,
			ms(h.Last, h.Recv), ms(h.Avg, h.Recv), ms(h.Best, h.Recv), ms(h.Worst, h.Recv), ms(h.StDev, h.Recv))
		fmt.Fprintln(w, row)
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
