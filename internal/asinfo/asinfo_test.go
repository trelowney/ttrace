package asinfo

import (
	"net"
	"testing"
)

func TestOriginQuery(t *testing.T) {
	if got := OriginQuery(net.ParseIP("8.8.4.4")); got != "4.4.8.8.origin.asn.cymru.com" {
		t.Errorf("v4: %s", got)
	}
	got := OriginQuery(net.ParseIP("2001:db8::1"))
	want := "1.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.8.b.d.0.1.0.0.2.origin6.asn.cymru.com"
	if got != want {
		t.Errorf("v6:\n got %s\nwant %s", got, want)
	}
}

func TestParseOrigin(t *testing.T) {
	i := ParseOrigin("15169 36040 | 8.8.8.0/24 | us | arin | 2023-12-28")
	if i.ASN != "15169" || i.Prefix != "8.8.8.0/24" || i.Country != "US" || i.Registry != "arin" {
		t.Errorf("%+v", i)
	}
}

func TestParseName(t *testing.T) {
	cases := map[string]string{
		"15169 | US | arin | 2000-03-30 | GOOGLE, US":          "GOOGLE",
		"2852 | CZ | ripencc | 1993-11-25 | CESNET2 Czech, CZ": "CESNET2 Czech",
		"1 | US | arin | 2001-09-20 | LVLT-1":                  "LVLT-1",
		"broken": "",
	}
	for in, want := range cases {
		if got := ParseName(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestIsPrivate(t *testing.T) {
	for _, s := range []string{"10.1.2.3", "192.168.0.1", "100.64.1.1", "fd00::1", "fe80::1", "127.0.0.1"} {
		if !IsPrivate(net.ParseIP(s)) {
			t.Errorf("%s should be private", s)
		}
	}
	for _, s := range []string{"8.8.8.8", "2001:4860:4860::8888", "100.128.0.1"} {
		if IsPrivate(net.ParseIP(s)) {
			t.Errorf("%s should be public", s)
		}
	}
}
