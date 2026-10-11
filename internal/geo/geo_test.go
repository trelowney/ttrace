package geo

import (
	"net"
	"strings"
	"testing"
)

func TestLand(t *testing.T) {
	for _, c := range []struct {
		name     string
		lat, lon float64
		want     bool
	}{
		{"Prague", 50.08, 14.43, true},
		{"Atlantic", 0, -30, false},
		{"Sydney inland", -33.8, 150.9, true},
		{"Pacific", 0, -150, false},
		{"Pacific, east of 180", 0, 190, false},
		{"Moscow", 55.75, 37.6, true},
	} {
		if got := Land(c.lat, c.lon); got != c.want {
			t.Errorf("%s: Land = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestRenderLabelsAndRoute(t *testing.T) {
	pts := []Point{
		{TTL: 3, Lat: 50.08, Lon: 14.43},
		{TTL: 4, Lat: 50.08, Lon: 14.43}, // same place: no own label
		{TTL: 7, Lat: 50.11, Lon: 8.68},
		{TTL: 12, Lat: 52.37, Lon: 4.89, Last: true},
	}
	g := Render(80, 24, pts)
	var text strings.Builder
	routes, targets := 0, 0
	for _, r := range g {
		for _, c := range r {
			switch c.Kind {
			case Route:
				routes++
			case Target:
				targets++
			}
			if c.Ch == 0 {
				text.WriteRune(' ')
			} else {
				text.WriteRune(c.Ch)
			}
		}
		text.WriteRune('\n')
	}
	s := text.String()
	for _, l := range []string{"3", "7", "12"} {
		if !strings.Contains(s, l) {
			t.Errorf("label %s missing:\n%s", l, s)
		}
	}
	if strings.Contains(s, "4") {
		t.Errorf("hop 4 shares the place of hop 3 and should not get a label:\n%s", s)
	}
	if routes == 0 || targets != 2 {
		t.Errorf("routes=%d targets=%d\n%s", routes, targets, s)
	}
}

func TestRenderAcrossPacific(t *testing.T) {
	// Tokyo -> Los Angeles must take the short way, keeping the map narrow
	// enough that both labels are visible.
	g := Render(60, 20, []Point{{TTL: 1, Lat: 35.7, Lon: 139.7}, {TTL: 2, Lat: 34.05, Lon: -118.25, Last: true}})
	found := map[rune]bool{}
	for _, r := range g {
		for _, c := range r {
			if c.Kind == Label || c.Kind == Target {
				found[c.Ch] = true
			}
		}
	}
	if !found['1'] || !found['2'] {
		t.Errorf("labels missing: %v", found)
	}
}

func TestLookupWithoutDatabase(t *testing.T) {
	d, err := Open(t.TempDir() + "/none.mmdb")
	if err != nil {
		t.Fatal(err)
	}
	if d.Ready() {
		t.Fatal("ready without a file")
	}
	if _, ok := d.Lookup(net.ParseIP("1.1.1.1")); ok {
		t.Fatal("found a place without a database")
	}
	var nilDB *DB
	if _, ok := nilDB.Lookup(net.ParseIP("1.1.1.1")); ok {
		t.Fatal("nil DB found a place")
	}
}

func TestPlaceName(t *testing.T) {
	if n := (Place{City: "Prague", Country: "CZ"}).Name(); n != "Prague, CZ" {
		t.Error(n)
	}
	if n := (Place{Country: "CZ"}).Name(); n != "CZ" {
		t.Error(n)
	}
}
