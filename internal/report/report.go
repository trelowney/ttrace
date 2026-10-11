// Package report formats snapshots as text tables and JSON.
package report

import (
	"encoding/json"
	"fmt"
	"io"
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

// Text writes an mtr-style report.
func Text(w io.Writer, s trace.Snapshot, m NameMode, showAS bool) {
	host := 20
	for _, h := range s.Hops {
		if l := len([]rune(Label(h, m))); l > host {
			host = l
		}
	}
	if host > 50 {
		host = 50
	}
	name := 20
	fmt.Fprintf(w, "ttrace to %s (%s), %d rounds\n", s.Target, s.IP, s.Round)
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

// JSON writes one snapshot as JSON (indent=false gives one line, for streaming).
func JSON(w io.Writer, s trace.Snapshot, indent bool) error {
	enc := json.NewEncoder(w)
	if indent {
		enc.SetIndent("", "  ")
	}
	return enc.Encode(s)
}
