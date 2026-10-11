package tui

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/trelowney/ttrace/internal/geo"
	"github.com/trelowney/ttrace/internal/report"
	"github.com/trelowney/ttrace/internal/trace"
)

// download is the state of fetching the location database in the background.
type download struct {
	mu          sync.Mutex
	running     bool
	done, total int64
	err         string
}

var (
	stCoast  = stDefault.Foreground(tcell.ColorGray)
	stRoute  = stDefault.Foreground(tcell.ColorYellow)
	stLabel  = stDefault.Foreground(tcell.ColorGreen).Bold(true)
	stTarget = stDefault.Foreground(tcell.ColorAqua).Bold(true)
)

func (v *view) startDownload(ctx context.Context) {
	d := &v.dl
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.running || v.opt.Geo == nil {
		return
	}
	d.running, d.done, d.total, d.err = true, 0, 0, ""
	go func() {
		err := v.opt.Geo.Download(ctx, func(done, total int64) {
			d.mu.Lock()
			d.done, d.total = done, total
			d.mu.Unlock()
		})
		d.mu.Lock()
		d.running = false
		if err != nil && ctx.Err() == nil {
			d.err = err.Error()
		}
		d.mu.Unlock()
		v.scr.PostEvent(tcell.NewEventInterrupt(nil))
	}()
}

// group is a run of consecutive hops at the same place.
type group struct {
	first, last int
	place       geo.Place
	host        string
}

func (v *view) drawMap(s trace.Snapshot, top, w, hgt int) {
	db := v.opt.Geo
	v.dl.mu.Lock()
	running, done, total, dlErr := v.dl.running, v.dl.done, v.dl.total, v.dl.err
	v.dl.mu.Unlock()

	y := top + 1
	line := func(text string, st tcell.Style) {
		if y < hgt {
			v.put(2, y, text, st)
		}
		y++
	}
	progress := func() {
		if total > 0 {
			line(fmt.Sprintf("Downloading the location database… %d of %d MB", done>>20, total>>20), stMsg)
		} else {
			line(fmt.Sprintf("Downloading the location database… %d MB", done>>20), stMsg)
		}
	}

	if db == nil || !db.Ready() {
		switch {
		case running:
			progress()
			line("The trace keeps running meanwhile. Press m to go back to the table.", stDim)
		case dlErr != "":
			line("The download did not work:", stBad)
			line(dlErr, stDefault)
			y++
			line("Press g to try again, or m to go back to the table.", stMsg)
		default:
			line("The map needs a list of where IP addresses are located ("+geo.Size+").", stDefault)
			line("It is downloaded once from db-ip.com and saved on this computer.", stDefault)
			y++
			line("Press g to download it now, or m to go back to the table.", stMsg)
			if db != nil {
				y++
				line("It will be saved to "+db.Path(), stDim)
			}
		}
		return
	}

	// hops with a known place, grouped by consecutive same place
	var pts []geo.Point
	var groups []group
	for _, h := range s.Hops {
		if h.Place == nil {
			continue
		}
		pts = append(pts, geo.Point{TTL: h.TTL, Lat: h.Place.Lat, Lon: h.Place.Lon, Last: s.Reached && h.TTL == s.Hops[len(s.Hops)-1].TTL})
		if n := len(groups); n > 0 && groups[n-1].place.Lat == h.Place.Lat && groups[n-1].place.Lon == h.Place.Lon {
			groups[n-1].last = h.TTL
			continue
		}
		groups = append(groups, group{first: h.TTL, last: h.TTL, place: *h.Place, host: report.Label(h, v.opt.Names)})
	}

	foot := []string{"Router places are approximate — often just the city or country of the network owner."}
	if age := db.Age(); age > 45*24*time.Hour {
		foot = append(foot, fmt.Sprintf("The location data is %d days old — press g to update it.", int(age.Hours()/24)))
	}
	foot = append(foot, geo.Attribution)
	if running {
		foot = append(foot, "") // room for the progress line
	}
	if dlErr != "" {
		foot = append(foot, "Update failed: "+dlErr+" — press g to try again.")
	}

	avail := hgt - top - len(foot) - 1
	if avail < 3 {
		line("The window is too small for the map — make it taller, or press m to go back.", stMsg)
		return
	}
	legend := len(groups)
	if len(groups) == 0 {
		legend = 2
	}
	if legend > avail/3 {
		legend = avail / 3
	}
	rows := avail - legend
	if rows < 3 {
		rows, legend = avail, 0
	}

	grid := geo.Render(w, rows, pts)
	for r, row := range grid {
		for c, cell := range row {
			if cell.Kind == geo.Empty {
				continue
			}
			st := map[int]tcell.Style{geo.Coast: stCoast, geo.Route: stRoute, geo.Label: stLabel, geo.Target: stTarget}[cell.Kind]
			v.scr.SetContent(c, top+r, cell.Ch, nil, st)
		}
	}

	y = top + rows
	if len(groups) == 0 && legend > 0 {
		line("No router on this route has a known place yet. Private addresses (like your home network)", stDim)
		line("and silent hops cannot be placed. Wait a few rounds, or press m to go back to the table.", stDim)
	}
	for i, g := range groups {
		if i >= legend {
			break
		}
		if i == legend-1 && len(groups) > legend {
			line(fmt.Sprintf("… and %d more places (make the window taller to see them)", len(groups)-i), stDim)
			break
		}
		hops := strconv.Itoa(g.first)
		if g.last != g.first {
			hops += "–" + strconv.Itoa(g.last)
		}
		st := stLabel
		if s.Reached && g.last == s.Hops[len(s.Hops)-1].TTL {
			st = stTarget
		}
		x := v.put(2, y, fmt.Sprintf("%-7s", hops), st)
		x = v.put(x, y, report.Fit(g.place.Name(), 28)+"  ", stDefault)
		v.put(x, y, report.Fit(g.host, w-x-1), stDim)
		y++
	}
	y = hgt - len(foot)
	for _, f := range foot {
		line(f, stDim)
	}
	if running {
		y = hgt - 1
		progress()
	}
}
