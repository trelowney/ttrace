package tui

import (
	"fmt"
	"strconv"

	"github.com/gdamore/tcell/v2"

	"github.com/trelowney/ttrace/internal/geo"
	"github.com/trelowney/ttrace/internal/report"
	"github.com/trelowney/ttrace/internal/trace"
)

var (
	stCoast  = stDefault.Foreground(tcell.ColorGray)
	stRoute  = stDefault.Foreground(tcell.ColorYellow)
	stLabel  = stDefault.Foreground(tcell.ColorGreen).Bold(true)
	stTarget = stDefault.Foreground(tcell.ColorAqua).Bold(true)
)

// group is a run of consecutive hops at the same place.
type group struct {
	first, last int
	place       geo.Place
	host        string
}

func (v *view) drawMap(s trace.Snapshot, top, w, hgt int) {
	db := v.opt.Geo
	var st geo.Status
	if db != nil {
		st = db.Status()
	}
	running, done, total, dlErr := st.Running, st.Done, st.Total, st.Err

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
			line("This happens once; the map appears by itself when it is done.", stDim)
			line("The trace keeps running meanwhile. Press m to go back to the table.", stDim)
		case dlErr != "":
			line("The download did not work:", stBad)
			line(dlErr, stDefault)
			y++
			line("Press g to try again, or m to go back to the table.", stMsg)
		default: // only with --no-geo
			line("The map is turned off (--no-geo), so ttrace does not download the location data.", stDefault)
			line("Press g to download it now ("+geo.Size+", once), or m to go back to the table.", stMsg)
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
