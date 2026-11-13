package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"github.com/trelowney/ttrace/internal/probe"
)

// MTUHop is one hop of a path MTU check with its name and network.
type MTUHop struct {
	TTL    int    `json:"ttl"`
	Addr   string `json:"addr,omitempty"`
	Host   string `json:"host,omitempty"`
	ASN    string `json:"asn,omitempty"`
	ASName string `json:"as_name,omitempty"`
	Size   int    `json:"size,omitempty"`
}

// MTUReport is the result of ttrace --mtu for one target.
type MTUReport struct {
	Target  string          `json:"target"`
	IP      string          `json:"ip"`
	Family  string          `json:"family"`
	Start   int             `json:"start"`
	Iface   string          `json:"interface,omitempty"`
	PathMTU int             `json:"path_mtu"`
	Reached bool            `json:"reached"`
	Hops    []MTUHop        `json:"hops"`
	Drops   []probe.MTUDrop `json:"drops,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// MTUJSON writes the reports as JSON: one object, or a list for several targets.
func MTUJSON(w io.Writer, reps []MTUReport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if len(reps) == 1 {
		return enc.Encode(reps[0])
	}
	return enc.Encode(reps)
}

// MTUText writes a path MTU check as a readable table.
func MTUText(w io.Writer, r MTUReport, m NameMode, showAS, wide bool) {
	from := ""
	if r.Iface != "" {
		from = " (MTU of " + r.Iface + ")"
	}
	fmt.Fprintf(w, "ttrace --mtu to %s (%s), starting with %d bytes%s\n", r.Target, r.IP, r.Start, from)
	nameW := 40
	if wide {
		nameW = 0
	}
	drops := map[int][]probe.MTUDrop{}
	for _, d := range r.Drops {
		drops[d.TTL] = append(drops[d.TTL], d)
	}
	for _, h := range r.Hops {
		name := "???"
		if h.Addr != "" {
			name = h.Addr
			switch {
			case m == NameHost && h.Host != "":
				name = h.Host
			case m == NameBoth && h.Host != "":
				name = h.Host + " (" + h.Addr + ")"
			}
		}
		if nameW > 0 {
			name = Fit(name, nameW)
		}
		as := ""
		if showAS && h.ASN != "" {
			as = "AS" + h.ASN
			if h.ASName != "" {
				as += " " + ShortAS(h.ASName)
			}
			if nameW > 0 {
				as = Fit(as, 24)
			}
		}
		size := ""
		if h.Size > 0 {
			size = strconv.Itoa(h.Size)
		}
		line := fmt.Sprintf("%3d. %s  %s", h.TTL, name, size)
		if showAS {
			line = fmt.Sprintf("%3d. %s  %-24s  %5s", h.TTL, name, as, size)
		}
		fmt.Fprintln(w, trimRight(line))
		for _, d := range drops[h.TTL] {
			if d.Reported {
				fmt.Fprintf(w, "     ▼ smaller here: %d → %d bytes (the router said so)\n", d.From, d.To)
			} else {
				fmt.Fprintf(w, "     ▼ smaller here: %d → %d bytes — bigger packets vanish without any message (PMTU black hole)\n", d.From, d.To)
			}
		}
	}
	switch {
	case r.PathMTU == 0:
		fmt.Fprintln(w, "No hop answered, so the size could not be measured. Some networks block ping (ICMP); try another target.")
	case r.Reached:
		fmt.Fprintf(w, "Path MTU: %d bytes — the biggest packet that gets all the way to %s in one piece.\n", r.PathMTU, r.Target)
	default:
		last := 0
		for _, h := range r.Hops {
			if h.Size > 0 {
				last = h.TTL
			}
		}
		fmt.Fprintf(w, "The target did not answer. Up to hop %d (the last one that answered) the path MTU is %d bytes.\n", last, r.PathMTU)
	}
	for _, d := range r.Drops {
		if !d.Reported {
			fmt.Fprintln(w, "A black hole makes big downloads or web pages hang while small ones work. The fix belongs to whoever runs that hop: let the ICMP \"fragmentation needed\" / \"packet too big\" messages through, or lower the MTU / MSS clamping there.")
			break
		}
	}
	if r.Error != "" {
		fmt.Fprintln(w, "Stopped: "+r.Error)
	}
}

func trimRight(s string) string {
	for len(s) > 0 && s[len(s)-1] == ' ' {
		s = s[:len(s)-1]
	}
	return s
}
