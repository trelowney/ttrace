package geo

import (
	"math"
	"strconv"
)

// Point is one hop on the map.
type Point struct {
	TTL      int
	Lat, Lon float64
	Last     bool // the target
}

// Cell kinds of a rendered map.
const (
	Empty = iota
	Coast
	Route
	Label
	Target
)

type Cell struct {
	Ch   rune
	Kind int
}

// Render draws the coastline and the route between the points into a grid
// of cols×rows terminal cells, using braille characters (2×4 dots per cell).
// The view is zoomed to fit all points. Consecutive points at the same place
// get one label: the first hop number there.
func Render(cols, rows int, pts []Point) [][]Cell {
	grid := make([][]Cell, rows)
	for i := range grid {
		grid[i] = make([]Cell, cols)
	}
	if cols < 4 || rows < 2 {
		return grid
	}
	w, h := cols*2, rows*4

	// Longitudes are unwrapped so a route over the Pacific stays connected.
	lons := make([]float64, len(pts))
	for i, p := range pts {
		lons[i] = p.Lon
		if i > 0 {
			for lons[i]-lons[i-1] > 180 {
				lons[i] -= 360
			}
			for lons[i]-lons[i-1] < -180 {
				lons[i] += 360
			}
		}
	}

	// bounding box with a sensible minimum size and some margin
	minLat, maxLat, minLon, maxLon := 90.0, -90.0, 1e9, -1e9
	for i, p := range pts {
		minLat, maxLat = math.Min(minLat, p.Lat), math.Max(maxLat, p.Lat)
		minLon, maxLon = math.Min(minLon, lons[i]), math.Max(maxLon, lons[i])
	}
	if len(pts) == 0 {
		minLat, maxLat, minLon, maxLon = -60, 80, -180, 180
	}
	midLat := (minLat + maxLat) / 2
	k := math.Max(math.Cos(midLat*math.Pi/180), 0.2) // shrink east-west distances away from the equator
	cx, cy := (minLon+maxLon)/2*k, midLat
	spanX := math.Max((maxLon-minLon)*k*1.3, 12)
	spanY := math.Max((maxLat-minLat)*1.3, 6)
	s := math.Max(spanX/float64(w), spanY/float64(h)) // degrees per dot

	toDot := func(lat, lon float64) (int, int) {
		x := (lon*k-cx)/s + float64(w)/2
		y := (cy-lat)/s + float64(h)/2
		return int(math.Floor(x)), int(math.Floor(y))
	}
	landAt := func(x, y int) bool {
		lat := cy - (float64(y)+0.5-float64(h)/2)*s
		lon := (cx + (float64(x)+0.5-float64(w)/2)*s) / k
		return lat > -90 && lat < 90 && Land(lat, lon)
	}

	// coastline: land dots next to water
	coast := make([][]bool, h)
	row := func(y int) []bool {
		r := make([]bool, w+2)
		for x := -1; x <= w; x++ {
			r[x+1] = landAt(x, y)
		}
		return r
	}
	prev, cur := row(-1), row(0)
	for y := 0; y < h; y++ {
		next := row(y + 1)
		coast[y] = make([]bool, w)
		for x := 0; x < w; x++ {
			if cur[x+1] && (!cur[x] || !cur[x+2] || !prev[x+1] || !next[x+1]) {
				coast[y][x] = true
			}
		}
		prev, cur = cur, next
	}

	route := make([][]bool, h)
	for y := range route {
		route[y] = make([]bool, w)
	}
	set := func(x, y int) {
		if x >= 0 && x < w && y >= 0 && y < h {
			route[y][x] = true
		}
	}
	for i := 1; i < len(pts); i++ {
		x0, y0 := toDot(pts[i-1].Lat, lons[i-1])
		x1, y1 := toDot(pts[i].Lat, lons[i])
		line(x0, y0, x1, y1, set)
	}

	for cy := 0; cy < rows; cy++ {
		for cx := 0; cx < cols; cx++ {
			var c, r rune
			for dy := 0; dy < 4; dy++ {
				for dx := 0; dx < 2; dx++ {
					y, x := cy*4+dy, cx*2+dx
					if coast[y][x] {
						c |= dotBit[dy][dx]
					}
					if route[y][x] {
						r |= dotBit[dy][dx]
					}
				}
			}
			switch {
			case r != 0:
				grid[cy][cx] = Cell{0x2800 + (c | r), Route}
			case c != 0:
				grid[cy][cx] = Cell{0x2800 + c, Coast}
			}
		}
	}

	// labels, drawn last so they stay readable
	for i, p := range pts {
		if i > 0 && pts[i-1].Lat == p.Lat && pts[i-1].Lon == p.Lon && !p.Last {
			continue
		}
		x, y := toDot(p.Lat, lons[i])
		cx, cy := x/2, y/4
		if cy < 0 || cy >= rows {
			continue
		}
		kind := Label
		if p.Last {
			kind = Target
		}
		for j, ch := range strconv.Itoa(p.TTL) {
			if cx+j >= 0 && cx+j < cols {
				grid[cy][cx+j] = Cell{ch, kind}
			}
		}
	}
	return grid
}

// dotBit[row][col] is the braille dot for that position in a cell.
var dotBit = [4][2]rune{{0x01, 0x08}, {0x02, 0x10}, {0x04, 0x20}, {0x40, 0x80}}

// line calls set for every dot on the straight line between two dots.
func line(x0, y0, x1, y1 int, set func(x, y int)) {
	dx, dy := abs(x1-x0), -abs(y1-y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	e := dx + dy
	for {
		set(x0, y0)
		if x0 == x1 && y0 == y1 {
			return
		}
		if 2*e >= dy {
			e += dy
			x0 += sx
		}
		if 2*e <= dx {
			e += dx
			y0 += sy
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
