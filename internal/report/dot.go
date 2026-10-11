package report

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/trelowney/ttrace/internal/trace"
)

type dotNode struct {
	label, asn, asName, country string
	private, target             bool
	stats                       string
}

// Dot writes the routes of one or more traces as a Graphviz graph.
// Render it with e.g.:  dot -Tsvg route.dot -o route.svg
func Dot(w io.Writer, snaps []trace.Snapshot, m NameMode, showAS bool) {
	nodes := map[string]*dotNode{}
	var links []trace.Link
	var titles []string
	for _, s := range snaps {
		titles = append(titles, s.Target)
		for _, h := range s.Hops {
			if h.Addr != "" {
				n := &dotNode{label: Label(h, m), asn: h.ASN, asName: h.ASName, country: h.Country, private: h.Private,
					target: h.Addr == s.IP}
				if h.Recv > 0 {
					n.stats = fmt.Sprintf("hop %d · loss %.0f%% · avg %.1f ms", h.TTL, h.Loss, h.Avg)
				}
				if old := nodes[h.Addr]; old == nil || old.stats == "" {
					nodes[h.Addr] = n
				}
			}
			for _, o := range h.Others {
				if nodes[o.Addr] == nil {
					nodes[o.Addr] = &dotNode{label: OtherLabel(o, m), asn: o.ASN, asName: o.ASName, country: o.Country,
						private: o.Private, target: o.Addr == s.IP, stats: fmt.Sprintf("hop %d · %s", h.TTL, Seen(o))}
				}
			}
		}
		links = append(links, s.Links...)
	}
	for _, l := range links {
		for _, a := range []string{l.From, l.To} {
			if a != "" && nodes[a] == nil {
				nodes[a] = &dotNode{label: a}
			}
		}
	}

	q := func(v string) string { return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v) + `"` }
	fmt.Fprintf(w, "digraph ttrace {\n")
	fmt.Fprintf(w, "  label=%s;\n  labelloc=t;\n  rankdir=TB;\n", q("ttrace to "+strings.Join(titles, ", ")))
	fmt.Fprintf(w, "  node [shape=box, style=rounded, fontname=\"Helvetica\", fontsize=10];\n")
	fmt.Fprintf(w, "  edge [fontname=\"Helvetica\", fontsize=9];\n")
	fmt.Fprintf(w, "  \"\" [label=\"this computer\", shape=ellipse];\n")

	// group routers by network operator (AS)
	groups := map[string][]string{}
	for addr, n := range nodes {
		key := ""
		switch {
		case !showAS:
		case n.private:
			key = "private"
		case n.asn != "":
			key = "AS" + n.asn
		}
		groups[key] = append(groups[key], addr)
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		addrs := groups[k]
		sort.Strings(addrs)
		indent := "  "
		if k != "" {
			title := "private network"
			if k != "private" {
				n := nodes[addrs[0]]
				title = strings.TrimSpace(k + " " + n.asName)
				if n.country != "" {
					title += " (" + n.country + ")"
				}
			}
			fmt.Fprintf(w, "  subgraph %s {\n    label=%s;\n    style=dashed;\n", q("cluster_"+k), q(title))
			indent = "    "
		}
		for _, addr := range addrs {
			n := nodes[addr]
			label := n.label
			if n.stats != "" {
				label += "\n" + n.stats
			}
			attrs := "label=" + q(label)
			if n.target {
				attrs += ", penwidth=2"
			}
			fmt.Fprintf(w, "%s%s [%s];\n", indent, q(addr), attrs)
		}
		if k != "" {
			fmt.Fprintf(w, "  }\n")
		}
	}

	seen := map[[2]string]bool{}
	for _, l := range links {
		k := [2]string{l.From, l.To}
		if seen[k] {
			continue
		}
		seen[k] = true
		attrs := ""
		if gap := l.ToTTL - l.FromTTL - 1; gap > 0 && l.From != "" {
			attrs = fmt.Sprintf(" [style=dashed, label=%s]", q(fmt.Sprintf("%d silent", gap)))
		}
		fmt.Fprintf(w, "  %s -> %s%s;\n", q(l.From), q(l.To), attrs)
	}
	fmt.Fprintf(w, "}\n")
}
