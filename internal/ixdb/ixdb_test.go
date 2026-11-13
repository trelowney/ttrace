package ixdb

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLookup(t *testing.T) {
	f := file{
		Updated: time.Now(),
		IXes:    map[string]ixInfo{"3512": {Name: "Frys-IX", Country: "NL"}},
		Prefixes: []prefix{
			{Prefix: "185.1.160.0/23", IX: "3512"},
			{Prefix: "2001:7f8:10f::/64", IX: "3512"},
		},
		Members: map[string]int{"185.1.160.217": 13335, "2001:7f8:10f::3417:217": 13335},
	}
	path := filepath.Join(t.TempDir(), "ix.json")
	b, _ := json.Marshal(f)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	d := Open(path)
	if !d.Ready() || d.Stale() {
		t.Fatalf("ready=%v stale=%v", d.Ready(), d.Stale())
	}
	for _, c := range []struct {
		ip, ix, asn string
		ok          bool
	}{
		{"185.1.160.217", "Frys-IX", "13335", true},
		{"2001:7f8:10f::3417:217", "Frys-IX", "13335", true},
		{"185.1.161.5", "Frys-IX", "", true},
		{"192.0.2.1", "", "", false},
	} {
		h, ok := d.Lookup(net.ParseIP(c.ip))
		if ok != c.ok || h.IX != c.ix || h.ASN != c.asn {
			t.Errorf("%s: got %+v %v", c.ip, h, ok)
		}
	}
}

func TestMissingFile(t *testing.T) {
	d := Open(filepath.Join(t.TempDir(), "none.json"))
	if d.Ready() || !d.Stale() {
		t.Fatal("a missing file must be stale and not ready")
	}
	if _, ok := d.Lookup(net.ParseIP("185.1.160.217")); ok {
		t.Fatal("found something without data")
	}
	var nilDB *DB
	if nilDB.Ready() || nilDB.Stale() || nilDB.Running() {
		t.Fatal("nil DB")
	}
}
