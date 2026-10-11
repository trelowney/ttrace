// Package asinfo finds the autonomous system (AS) behind an IP address:
// AS number, AS name, announced prefix and the country where the address
// block is registered. Data comes from the Team Cymru IP-to-ASN DNS service,
// which is free and needs no API key.
package asinfo

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"strings"
	"sync"
)

// Info about one address. Private is set for addresses that are not routed
// on the internet (RFC 1918, CGNAT, link-local, ULA…).
type Info struct {
	ASN      string `json:"asn,omitempty"`
	Name     string `json:"as_name,omitempty"`
	Prefix   string `json:"prefix,omitempty"`
	Country  string `json:"country,omitempty"`
	Registry string `json:"registry,omitempty"`
	Private  bool   `json:"private,omitempty"`
}

// Resolver caches answers; safe for concurrent use.
type Resolver struct {
	r     *net.Resolver
	mu    sync.Mutex
	names map[string]string // ASN -> name
}

func NewResolver() *Resolver {
	return &Resolver{r: net.DefaultResolver, names: map[string]string{}}
}

var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// IsPrivate reports addresses that have no public AS.
func IsPrivate(ip net.IP) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
		ip.IsUnspecified() || ip.IsMulticast() || cgnat.Contains(ip)
}

// Lookup returns AS data for ip.
func (r *Resolver) Lookup(ctx context.Context, ip net.IP) (Info, error) {
	if IsPrivate(ip) {
		return Info{Private: true}, nil
	}
	txt, err := r.r.LookupTXT(ctx, OriginQuery(ip))
	if err != nil || len(txt) == 0 {
		return Info{}, err
	}
	info := ParseOrigin(txt[0])
	if info.ASN == "" {
		return info, nil
	}
	r.mu.Lock()
	name, ok := r.names[info.ASN]
	r.mu.Unlock()
	if !ok {
		if t, err := r.r.LookupTXT(ctx, "AS"+info.ASN+".asn.cymru.com"); err == nil && len(t) > 0 {
			name = ParseName(t[0])
			r.mu.Lock()
			r.names[info.ASN] = name
			r.mu.Unlock()
		}
	}
	info.Name = name
	return info, nil
}

// OriginQuery builds the DNS name for the origin lookup of ip.
func OriginQuery(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return fmt.Sprintf("%d.%d.%d.%d.origin.asn.cymru.com", v4[3], v4[2], v4[1], v4[0])
	}
	v6 := ip.To16()
	var b strings.Builder
	for i := len(v6) - 1; i >= 0; i-- {
		fmt.Fprintf(&b, "%x.%x.", v6[i]&0x0f, v6[i]>>4)
	}
	b.WriteString("origin6.asn.cymru.com")
	return b.String()
}

func fields(txt string) []string {
	parts := strings.Split(txt, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// ParseOrigin parses "15169 | 8.8.8.0/24 | US | arin | 2023-12-28".
// When a prefix is announced by several ASes, the first one is used.
func ParseOrigin(txt string) Info {
	f := fields(txt)
	var info Info
	if len(f) > 0 {
		if asns := strings.Fields(f[0]); len(asns) > 0 {
			info.ASN = asns[0]
		}
	}
	if len(f) > 1 {
		info.Prefix = f[1]
	}
	if len(f) > 2 {
		info.Country = strings.ToUpper(f[2])
	}
	if len(f) > 3 {
		info.Registry = f[3]
	}
	return info
}

var trailingCC = regexp.MustCompile(`,\s*[A-Z]{2}$`)

// ParseName parses "15169 | US | arin | 2000-03-30 | GOOGLE, US" into "GOOGLE".
func ParseName(txt string) string {
	f := fields(txt)
	if len(f) < 5 {
		return ""
	}
	return strings.TrimSpace(trailingCC.ReplaceAllString(f[4], ""))
}
