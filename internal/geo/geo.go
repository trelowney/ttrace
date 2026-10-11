// Package geo finds the approximate place of an IP address and knows where
// land is, for drawing the route on a map.
//
// Places come from the free DB-IP "IP to City Lite" database (CC BY 4.0),
// downloaded once into the user's cache folder. The land outline is Natural
// Earth 1:50m land (public domain), stored as a 0.25° bitmap in land.bin.
package geo

import (
	"compress/gzip"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/oschwald/maxminddb-golang"
)

// Attribution must be shown wherever the places are used.
const Attribution = "IP locations: DB-IP.com (CC BY 4.0) · Map: Natural Earth"

// Place is where an address is registered or believed to be. For routers
// this is often only the city (or even the country) of the network owner.
type Place struct {
	City    string  `json:"city,omitempty"`
	Region  string  `json:"region,omitempty"`
	Country string  `json:"country,omitempty"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
}

// Name is "City, CC", or just the country when the city is unknown.
func (p Place) Name() string {
	switch {
	case p.City != "" && p.Country != "":
		return p.City + ", " + p.Country
	case p.City != "":
		return p.City
	}
	return p.Country
}

//go:embed land.bin
var land []byte

const landW, landH = 1440, 720 // cells of 0.25°, row 0 starts at 90°N, column 0 at 180°W

// Land reports whether the point is on land.
func Land(lat, lon float64) bool {
	lon = math.Mod(lon+180, 360)
	if lon < 0 {
		lon += 360
	}
	r := int((90 - lat) * landH / 180)
	c := int(lon * landW / 360)
	if r < 0 || r >= landH || c < 0 || c >= landW {
		return false
	}
	i := r*landW + c
	return land[i>>3]&(0x80>>(i&7)) != 0
}

// DB holds the opened location database. The zero value is usable and
// finds nothing until a database is opened or downloaded.
type DB struct {
	mu   sync.RWMutex
	r    *maxminddb.Reader
	path string
}

// DefaultPath is where the database is kept, e.g. ~/.cache/ttrace/dbip-city-lite.mmdb.
func DefaultPath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "ttrace", "dbip-city-lite.mmdb")
}

// Open uses the database at path if it was downloaded before. A missing file
// is not an error: Ready stays false and Download can fetch it later.
func Open(path string) (*DB, error) {
	d := &DB{path: path}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return d, nil
	}
	r, err := maxminddb.Open(path)
	if err != nil {
		return d, fmt.Errorf("the location database %s cannot be read (%v); run ttrace --geo-update to download it again", path, err)
	}
	d.r = r
	return d, nil
}

// Ready reports whether places can be looked up.
func (d *DB) Ready() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.r != nil
}

// Path is the database file.
func (d *DB) Path() string { return d.path }

// Age is how long ago the database was built (0 when not ready).
func (d *DB) Age() time.Duration {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.r == nil {
		return 0
	}
	return time.Since(time.Unix(int64(d.r.Metadata.BuildEpoch), 0))
}

type record struct {
	City struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"city"`
	Country struct {
		ISOCode string `maxminddb:"iso_code"`
	} `maxminddb:"country"`
	Subdivisions []struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"subdivisions"`
	Location struct {
		Lat *float64 `maxminddb:"latitude"`
		Lon *float64 `maxminddb:"longitude"`
	} `maxminddb:"location"`
}

// Lookup returns the place of a public address.
func (d *DB) Lookup(ip net.IP) (Place, bool) {
	if d == nil || ip == nil || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return Place{}, false
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.r == nil {
		return Place{}, false
	}
	var rec record
	if err := d.r.Lookup(ip, &rec); err != nil || rec.Location.Lat == nil || rec.Location.Lon == nil {
		return Place{}, false
	}
	p := Place{City: rec.City.Names["en"], Country: rec.Country.ISOCode, Lat: *rec.Location.Lat, Lon: *rec.Location.Lon}
	if len(rec.Subdivisions) > 0 {
		p.Region = rec.Subdivisions[0].Names["en"]
	}
	return p, true
}

// Close releases the database file.
func (d *DB) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.r == nil {
		return nil
	}
	err := d.r.Close()
	d.r = nil
	return err
}

// Size is roughly how much Download fetches, for telling the user.
const Size = "about 60 MB"

// Download fetches the newest monthly database from db-ip.com and switches
// to it. progress (may be nil) gets the downloaded and total bytes.
func (d *DB) Download(ctx context.Context, progress func(done, total int64)) error {
	if err := os.MkdirAll(filepath.Dir(d.path), 0o755); err != nil {
		return fmt.Errorf("cannot create the folder for the location database: %v", err)
	}
	// A new file appears on the 1st of each month; early on that day it may
	// not be there yet, so fall back to the previous month.
	now := time.Now().UTC()
	var resp *http.Response
	for _, t := range []time.Time{now, now.AddDate(0, 0, -now.Day())} {
		url := "https://download.db-ip.com/free/dbip-city-lite-" + t.Format("2006-01") + ".mmdb.gz"
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", "ttrace (https://github.com/trelowney/ttrace)")
		r, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("cannot reach download.db-ip.com: %v — check the internet connection and try again", err)
		}
		if r.StatusCode == http.StatusOK {
			resp = r
			break
		}
		r.Body.Close()
	}
	if resp == nil {
		return errors.New("download.db-ip.com has no database file right now — try again later")
	}
	defer resp.Body.Close()

	tmp, err := os.CreateTemp(filepath.Dir(d.path), "download-*.tmp")
	if err != nil {
		return fmt.Errorf("cannot write into %s: %v", filepath.Dir(d.path), err)
	}
	defer os.Remove(tmp.Name())
	cr := &countReader{r: resp.Body, total: resp.ContentLength, progress: progress}
	gz, err := gzip.NewReader(cr)
	if err == nil {
		_, err = io.Copy(tmp, gz)
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("the download did not finish (%v) — try again", err)
	}
	if r, err := maxminddb.Open(tmp.Name()); err != nil {
		return fmt.Errorf("the downloaded file is damaged (%v) — try again", err)
	} else {
		r.Close()
	}

	// Windows cannot replace a file that is open, so close the old one first.
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.r != nil {
		d.r.Close()
		d.r = nil
	}
	if err := os.Rename(tmp.Name(), d.path); err != nil {
		return fmt.Errorf("cannot save the location database to %s: %v", d.path, err)
	}
	r, err := maxminddb.Open(d.path)
	if err != nil {
		return err
	}
	d.r = r
	return nil
}

// client gives up when the server does not start answering in time; the
// download itself may take long on slow lines and stops only on cancel.
var client = &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 30 * time.Second}}

type countReader struct {
	r           io.Reader
	done, total int64
	progress    func(done, total int64)
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.done += int64(n)
	if c.progress != nil {
		c.progress(c.done, c.total)
	}
	return n, err
}
