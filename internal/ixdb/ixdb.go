// Package ixdb knows the address ranges of internet exchanges (IX) and which
// network uses which address on them. A router address inside an IX range
// belongs to the exchange's shared network, so the AS that the address block
// is registered to (often the exchange operator) says little; the member
// network that uses the address is what matters.
//
// The data comes from PeeringDB (https://www.peeringdb.com), is downloaded
// into the user's cache folder and refreshed every two weeks.
package ixdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// Hit is what is known about one address on an exchange.
type Hit struct {
	IX      string // name of the exchange, e.g. "Frys-IX"
	Country string // country of the exchange
	ASN     string // member network using the address, "" when unknown
}

// file is the cached form of the data.
type file struct {
	Updated  time.Time         `json:"updated"`
	IXes     map[string]ixInfo `json:"ixes"`     // id -> exchange
	Prefixes []prefix          `json:"prefixes"` // shared networks of the exchanges
	Members  map[string]int    `json:"members"`  // address -> ASN of the member using it
}

type ixInfo struct {
	Name    string `json:"name"`
	Country string `json:"country,omitempty"`
}

type prefix struct {
	Prefix string `json:"prefix"`
	IX     string `json:"ix"`
}

type parsed struct {
	f    file
	nets []*net.IPNet
	ixes []string // exchange id of nets[i]
}

// DB is safe for concurrent use. The zero value finds nothing.
type DB struct {
	mu   sync.RWMutex
	data *parsed
	path string

	smu     sync.Mutex
	running bool
	err     string
	wg      sync.WaitGroup
}

// MaxAge is when the data counts as old and Start fetches it again.
const MaxAge = 14 * 24 * time.Hour

// Size is roughly how much a download fetches, for telling the user.
const Size = "about 10 MB"

// DefaultPath is where the data is kept, e.g. ~/.cache/ttrace/peeringdb-ix.json.
func DefaultPath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "ttrace", "peeringdb-ix.json")
}

// Open uses the data at path if it was downloaded before. A missing or
// damaged file is not an error: Stale then reports true and Start fetches it.
func Open(path string) *DB {
	d := &DB{path: path}
	b, err := os.ReadFile(path)
	if err != nil {
		return d
	}
	var f file
	if json.Unmarshal(b, &f) == nil {
		d.data = parse(f)
	}
	return d
}

func parse(f file) *parsed {
	p := &parsed{f: f}
	for _, x := range f.Prefixes {
		if _, n, err := net.ParseCIDR(x.Prefix); err == nil {
			p.nets = append(p.nets, n)
			p.ixes = append(p.ixes, x.IX)
		}
	}
	return p
}

// Ready reports whether lookups can find anything.
func (d *DB) Ready() bool {
	if d == nil {
		return false
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.data != nil
}

// Stale reports whether the data is missing or old.
func (d *DB) Stale() bool {
	if d == nil {
		return false
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.data == nil || time.Since(d.data.f.Updated) > MaxAge
}

// Lookup tells whether ip is on an exchange and who uses it.
func (d *DB) Lookup(ip net.IP) (Hit, bool) {
	if d == nil || ip == nil {
		return Hit{}, false
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.data == nil {
		return Hit{}, false
	}
	for i, n := range d.data.nets {
		if !n.Contains(ip) {
			continue
		}
		ix := d.data.f.IXes[d.data.ixes[i]]
		h := Hit{IX: ix.Name, Country: ix.Country}
		if asn, ok := d.data.f.Members[ip.String()]; ok && asn > 0 {
			h.ASN = strconv.Itoa(asn)
		}
		if h.IX == "" {
			h.IX = "IX"
		}
		return h, true
	}
	return Hit{}, false
}

// Start downloads the data in the background unless a download already
// runs. Lookups keep using the old data until the new data is complete.
func (d *DB) Start(ctx context.Context) {
	if d == nil {
		return
	}
	d.smu.Lock()
	defer d.smu.Unlock()
	if d.running {
		return
	}
	d.running, d.err = true, ""
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		err := d.Download(ctx)
		d.smu.Lock()
		d.running = false
		if err != nil && ctx.Err() == nil {
			d.err = err.Error()
		}
		d.smu.Unlock()
	}()
}

// Running reports whether a background download is in progress.
func (d *DB) Running() bool {
	if d == nil {
		return false
	}
	d.smu.Lock()
	defer d.smu.Unlock()
	return d.running
}

// Err is why the last background download failed, "" when it did not.
func (d *DB) Err() string {
	if d == nil {
		return ""
	}
	d.smu.Lock()
	defer d.smu.Unlock()
	return d.err
}

// Wait blocks until a background download has finished.
func (d *DB) Wait() {
	if d != nil {
		d.wg.Wait()
	}
}

// Path is the cache file.
func (d *DB) Path() string { return d.path }

var client = &http.Client{Timeout: 3 * time.Minute, Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 60 * time.Second}}

// get fetches one PeeringDB API list, e.g. "ix?fields=id,name,country".
// When PeeringDB is busy or limits how often it may be asked (HTTP 429),
// it waits as long as PeeringDB says (at most a minute) and tries again.
func get(ctx context.Context, query string, into any) error {
	for try := 1; ; try++ {
		wait, err := getOnce(ctx, query, into)
		if wait == 0 || try == 3 {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// getOnce makes one request; wait > 0 means it is worth trying again after that time.
func getOnce(ctx context.Context, query string, into any) (time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.peeringdb.com/api/"+query, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "ttrace (https://github.com/trelowney/ttrace)")
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("cannot reach www.peeringdb.com: %v — check the internet connection and try again", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		wait := 15 * time.Second
		if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 {
			wait = time.Duration(s) * time.Second
		}
		err := fmt.Errorf("www.peeringdb.com answered %s — it will be tried again on the next run", resp.Status)
		if wait > time.Minute {
			return 0, err
		}
		return wait, err
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("www.peeringdb.com answered %s — it will be tried again on the next run", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		return 0, fmt.Errorf("the list of internet exchanges did not download completely (%v) — it will be tried again on the next run", err)
	}
	return 0, nil
}

// Download fetches the newest data from PeeringDB, saves it and switches to it.
func (d *DB) Download(ctx context.Context) error {
	var ixes struct {
		Data []struct {
			ID      int    `json:"id"`
			Name    string `json:"name"`
			Country string `json:"country"`
		} `json:"data"`
	}
	var lans struct {
		Data []struct {
			ID   int `json:"id"`
			IXID int `json:"ix_id"`
		} `json:"data"`
	}
	var pfxs struct {
		Data []struct {
			Prefix  string `json:"prefix"`
			IXLanID int    `json:"ixlan_id"`
		} `json:"data"`
	}
	var members struct {
		Data []struct {
			ASN   int     `json:"asn"`
			Addr4 *string `json:"ipaddr4"`
			Addr6 *string `json:"ipaddr6"`
		} `json:"data"`
	}
	for _, q := range []struct {
		query string
		into  any
	}{
		{"ix?fields=id,name,country", &ixes},
		{"ixlan?fields=id,ix_id", &lans},
		{"ixpfx?fields=prefix,ixlan_id", &pfxs},
		{"netixlan?fields=asn,ipaddr4,ipaddr6", &members},
	} {
		if err := get(ctx, q.query, q.into); err != nil {
			return err
		}
	}
	if len(ixes.Data) == 0 || len(pfxs.Data) == 0 {
		return errors.New("www.peeringdb.com sent an empty list of internet exchanges — try again later")
	}

	f := file{Updated: time.Now().UTC(), IXes: map[string]ixInfo{}, Members: map[string]int{}}
	for _, x := range ixes.Data {
		f.IXes[strconv.Itoa(x.ID)] = ixInfo{Name: x.Name, Country: x.Country}
	}
	lanIX := map[int]int{}
	for _, l := range lans.Data {
		lanIX[l.ID] = l.IXID
	}
	for _, p := range pfxs.Data {
		ix, ok := lanIX[p.IXLanID]
		if !ok {
			continue
		}
		f.Prefixes = append(f.Prefixes, prefix{Prefix: p.Prefix, IX: strconv.Itoa(ix)})
	}
	for _, m := range members.Data {
		for _, a := range []*string{m.Addr4, m.Addr6} {
			if a == nil {
				continue
			}
			if ip := net.ParseIP(*a); ip != nil {
				f.Members[ip.String()] = m.ASN
			}
		}
	}

	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(d.path), 0o755); err != nil {
		return fmt.Errorf("cannot create the folder for the list of internet exchanges: %v", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(d.path), "ix-*.tmp")
	if err != nil {
		return fmt.Errorf("cannot write into %s: %v", filepath.Dir(d.path), err)
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(b)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), d.path)
	}
	if err != nil {
		return fmt.Errorf("cannot save the list of internet exchanges to %s: %v", d.path, err)
	}
	p := parse(f)
	d.mu.Lock()
	d.data = p
	d.mu.Unlock()
	return nil
}
