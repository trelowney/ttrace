package report

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	"github.com/trelowney/ttrace/internal/trace"
)

// Differences smaller than these are not pointed out.
const (
	cmpLossPts = 2.0  // percentage points of loss
	cmpAvgMs   = 5.0  // milliseconds of average latency …
	cmpAvgPct  = 20.0 // … and percent of the earlier value
)

// ReadSaved reads a report saved with ttrace -j: one trace or a list of them.
func ReadSaved(path string) ([]trace.Snapshot, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %v", path, err)
	}
	var list []trace.Snapshot
	if err := json.Unmarshal(b, &list); err == nil && len(list) > 0 {
		return list, nil
	}
	var one trace.Snapshot
	if err := json.Unmarshal(b, &one); err != nil || one.Target == "" {
		return nil, fmt.Errorf("%s is not a ttrace report — save one with: ttrace -j -c 20 <host> > %s", path, path)
	}
	return []trace.Snapshot{one}, nil
}

// Compare writes two saved measurements side by side, hop by hop, and lists
// what changed: a different router or network on a hop, more or less loss,
// higher or lower latency. Traces are paired by target; a target in only
// one of the files is mentioned.
func Compare(w io.Writer, beforeName string, before []trace.Snapshot, afterName string, after []trace.Snapshot, m NameMode) error {
	if len(before) == 0 || len(after) == 0 {
		return errors.New("nothing to compare")
	}
	fmt.Fprintf(w, "Comparing %s (before) with %s (after)\n", beforeName, afterName)
	used := map[int]bool{}
	for _, b := range before {
		j := -1
		for i, a := range after {
			if !used[i] && a.Target == b.Target {
				j = i
				break
			}
		}
		if j < 0 && len(before) == 1 && len(after) == 1 {
			j = 0 // one trace each: compare them even under different names
		}
		if j < 0 {
			fmt.Fprintf(w, "\n%s is only in %s.\n", b.Target, beforeName)
			continue
		}
		used[j] = true
		fmt.Fprintln(w)
		compareOne(w, b, after[j], m)
	}
	for i, a := range after {
		if !used[i] {
			fmt.Fprintf(w, "\n%s is only in %s.\n", a.Target, afterName)
		}
	}
	return nil
}

func compareOne(w io.Writer, b, a trace.Snapshot, m NameMode) {
	title := b.Target
	if a.Target != b.Target {
		title += " / " + a.Target
	}
	fmt.Fprintf(w, "ttrace to %s: %s, %d rounds → %s, %d rounds\n", title, b.IP, b.Round, a.IP, a.Round)
	if b.Family != a.Family {
		fmt.Fprintf(w, "Note: the first was measured over %s, the second over %s.\n", b.Family, a.Family)
	}

	byTTL := func(s trace.Snapshot) map[int]trace.Hop {
		out := map[int]trace.Hop{}
		for _, h := range s.Hops {
			out[h.TTL] = h
		}
		return out
	}
	bh, ah := byTTL(b), byTTL(a)
	last := 0
	for _, s := range []trace.Snapshot{b, a} {
		for _, h := range s.Hops {
			if h.TTL > last {
				last = h.TTL
			}
		}
	}

	const col = 32
	fmt.Fprintf(w, "%3s  %s  %s  %-15s  %s\n", "#", Fit("Before", col), Fit("After", col), "Loss%", "Avg ms")
	var route, loss, lat []string
	for ttl := 1; ttl <= last; ttl++ {
		x, okB := bh[ttl]
		y, okA := ah[ttl]
		if !okB && !okA {
			continue
		}
		left, right := "—", "—"
		if okB {
			left = cmpLabel(x, m)
		}
		if okA {
			right = cmpLabel(y, m)
		}
		switch {
		case okB && okA && x.Addr == y.Addr:
			right = "same"
		case okB && okA && x.Addr != "" && y.Addr != "":
			what := "other router"
			if x.ASN != y.ASN && x.ASN != "" && y.ASN != "" {
				what = "other network AS" + x.ASN + " → AS" + y.ASN
			}
			route = append(route, fmt.Sprintf("hop %d (%s)", ttl, what))
		case okB && okA:
			// one side did not answer on this hop; that is loss, not a new route
		case okB:
			route = append(route, fmt.Sprintf("hop %d (only before)", ttl))
		default:
			route = append(route, fmt.Sprintf("hop %d (only after)", ttl))
		}

		lossCol, avgCol := "", ""
		if okB && okA {
			lossCol = fmt.Sprintf("%5.1f → %5.1f", x.Loss, y.Loss)
			if d := y.Loss - x.Loss; math.Abs(d) >= cmpLossPts {
				lossCol += " !"
				loss = append(loss, fmt.Sprintf("hop %d %+.1f %%", ttl, d))
			}
			if x.Recv > 0 && y.Recv > 0 {
				avgCol = fmt.Sprintf("%6.1f → %6.1f", x.Avg, y.Avg)
				if d := y.Avg - x.Avg; math.Abs(d) >= cmpAvgMs && math.Abs(d) >= x.Avg*cmpAvgPct/100 {
					avgCol += " !"
					lat = append(lat, fmt.Sprintf("hop %d %+.1f ms", ttl, d))
				}
			}
		}
		fmt.Fprintf(w, "%3d. %s  %s  %-15s  %s\n", ttl, Fit(left, col), Fit(right, col), lossCol, avgCol)
	}

	if pb, pa := ASPath(b), ASPath(a); pb != pa && pb != "" && pa != "" {
		fmt.Fprintf(w, "AS path before: %s\nAS path after:  %s\n", pb, pa)
	}
	if len(route)+len(loss)+len(lat) == 0 {
		fmt.Fprintln(w, "No noticeable differences.")
		return
	}
	if len(route) > 0 {
		fmt.Fprintln(w, "Route changed at "+strings.Join(route, ", ")+".")
	}
	if len(loss) > 0 {
		fmt.Fprintln(w, "Loss changed at "+strings.Join(loss, ", ")+".")
	}
	if len(lat) > 0 {
		fmt.Fprintln(w, "Latency changed at "+strings.Join(lat, ", ")+".")
	}
	if len(loss) > 0 {
		fmt.Fprintln(w, "Loss on a router in the middle that does not continue to the hops behind it is only the router answering slowly, not real loss.")
	}
}

// cmpLabel is a hop as written in the comparison: name and AS.
func cmpLabel(h trace.Hop, m NameMode) string {
	l := Label(h, m)
	if a := ASN(h); a != "" && a != "private" {
		l += " " + a
	}
	return l
}
