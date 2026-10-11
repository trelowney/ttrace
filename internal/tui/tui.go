// Package tui is the live full-screen view.
package tui

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/trelowney/ttrace/internal/probe"
	"github.com/trelowney/ttrace/internal/report"
	"github.com/trelowney/ttrace/internal/trace"
)

// Switcher resolves the target again for IPv4 (v6=false) or IPv6 and opens a
// prober for it. nil when the target was given as an IP address.
type Switcher func(v6 bool) (net.IP, probe.Prober, error)

type Options struct {
	Version string
	Names   report.NameMode
	ShowAS  bool
	Switch  Switcher
}

type view struct {
	scr    tcell.Screen
	eng    *trace.Engine
	opt    Options
	pulse  bool // show recent latency bars instead of statistics
	msg    string
	msgEnd time.Time
}

// Run shows the live table until the user quits.
func Run(ctx context.Context, eng *trace.Engine, opt Options) error {
	scr, err := tcell.NewScreen()
	if err != nil {
		return err
	}
	if err := scr.Init(); err != nil {
		return err
	}
	defer scr.Fini()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go eng.Run(ctx, func(trace.Snapshot) { scr.PostEvent(tcell.NewEventInterrupt(nil)) })
	go func() { // names and AS data arrive between rounds
		t := time.NewTicker(500 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				scr.PostEvent(tcell.NewEventInterrupt(nil))
			}
		}
	}()

	v := &view{scr: scr, eng: eng, opt: opt}
	v.draw()
	for {
		switch ev := scr.PollEvent().(type) {
		case nil:
			return nil
		case *tcell.EventResize:
			scr.Sync()
		case *tcell.EventKey:
			switch {
			case ev.Key() == tcell.KeyCtrlC, ev.Key() == tcell.KeyEscape, ev.Rune() == 'q':
				return nil
			case ev.Rune() == 'p':
				eng.SetPaused(!eng.Paused())
			case ev.Rune() == 'r':
				eng.Reset()
				v.say("statistics cleared")
			case ev.Rune() == 'n':
				v.opt.Names = (v.opt.Names + 1) % 3
			case ev.Rune() == 'a':
				v.opt.ShowAS = !v.opt.ShowAS
			case ev.Rune() == 'd':
				v.pulse = !v.pulse
			case ev.Rune() == '4', ev.Rune() == '6':
				v.switchFamily(ev.Rune() == '6')
			}
		}
		if ctx.Err() != nil {
			return nil
		}
		v.draw()
	}
}

func (v *view) say(m string) {
	v.msg, v.msgEnd = m, time.Now().Add(4*time.Second)
}

func (v *view) switchFamily(v6 bool) {
	want := "IPv4"
	if v6 {
		want = "IPv6"
	}
	cur := v.eng.IP()
	if (cur.To4() == nil) == v6 {
		v.say("already using " + want)
		return
	}
	if v.opt.Switch == nil {
		v.say("the target is an IP address, so it cannot switch to " + want + " — start ttrace with a host name")
		return
	}
	ip, p, err := v.opt.Switch(v6)
	if err != nil {
		v.say(err.Error())
		return
	}
	v.eng.Replace(ip, p)
	v.say("switched to " + want + " (" + ip.String() + ")")
}

var (
	stDefault = tcell.StyleDefault
	stBold    = stDefault.Bold(true)
	stDim     = stDefault.Dim(true)
	stHead    = stDefault.Reverse(true)
	stWarn    = stDefault.Foreground(tcell.ColorYellow)
	stBad     = stDefault.Foreground(tcell.ColorRed)
	stAS      = stDefault.Foreground(tcell.ColorTeal)
	stGood    = stDefault.Foreground(tcell.ColorGreen)
	stMsg     = stDefault.Foreground(tcell.ColorYellow).Bold(true)
)

func (v *view) put(x, y int, s string, st tcell.Style) int {
	w, _ := v.scr.Size()
	for _, r := range s {
		if x >= w {
			break
		}
		v.scr.SetContent(x, y, r, nil, st)
		x++
	}
	return x
}

func (v *view) draw() {
	s := v.eng.Snapshot()
	v.scr.Clear()
	w, hgt := v.scr.Size()

	state := fmt.Sprintf("round %d", s.Round)
	if v.eng.Paused() {
		state = "PAUSED"
	}
	x := v.put(0, 0, "ttrace "+v.opt.Version, stBold)
	x = v.put(x, 0, "  →  "+s.Target, stDefault)
	if s.Target != s.IP {
		x = v.put(x, 0, " ("+s.IP+")", stDim)
	}
	x = v.put(x, 0, "  "+s.Family, stAS)
	if s.Probe != "" {
		x = v.put(x, 0, " "+s.Probe, stAS)
	}
	v.put(x, 0, "   "+state, stDim)
	if v.msg != "" && time.Now().Before(v.msgEnd) {
		v.put(0, 1, v.msg, stMsg)
	} else {
		v.put(0, 1, fmt.Sprintf("keys: q quit  p pause  r reset  n names (%s)  a AS  d pulse  4/6 IPv4/IPv6", v.opt.Names), stDim)
	}

	// columns: "#" 4, host flexible, AS block, stats (or pulse)
	rightW := len(report.StatsHeader)
	if v.pulse {
		rightW = 7 + 1 + 40 // loss + bars
	}
	asW, nameW := 0, 0
	if v.opt.ShowAS {
		nameW = 22
		if w < 110 {
			nameW = 12
		}
		asW = 11 + nameW + 1 + 3 // " ASN " + name + " " + "CC "
	}
	hostW := w - 4 - asW - rightW - 2
	if hostW < 12 {
		hostW = 12
	}

	head := fmt.Sprintf("%3s %s", "#", report.Fit("Host", hostW))
	if v.opt.ShowAS {
		head += fmt.Sprintf(" %-9s %s %-2s ", "ASN", report.Fit("AS name", nameW), "CC")
	}
	if v.pulse {
		head += "  Loss%  recent rounds (newest on the right, · = no answer)"
	} else {
		head += " " + report.StatsHeader
	}
	v.put(0, 3, report.Fit(head, w), stHead)

	y := 3
	for _, h := range s.Hops {
		if y++; y >= hgt {
			break
		}
		x := v.put(0, y, fmt.Sprintf("%3d ", h.TTL), stDim)
		hs := stDefault
		if h.Addr == "" {
			hs = stDim
		}
		x = v.put(x, y, report.Fit(report.Label(h, v.opt.Names), hostW), hs)
		if v.opt.ShowAS {
			x = v.put(x, y, " "+report.Fit(report.ASN(h), 9)+" ", stAS)
			x = v.put(x, y, report.Fit(h.ASName, nameW)+" ", stDefault)
			x = v.put(x, y, report.Fit(h.Country, 2)+" ", stBold)
		}
		st := stDefault
		switch {
		case h.Sent > 0 && h.Loss >= 50:
			st = stBad
		case h.Loss > 0:
			st = stWarn
		}
		if v.pulse {
			x = v.put(x, y, fmt.Sprintf(" %5.1f%% ", h.Loss), st)
			v.bars(x, y, h.Recent, w-x)
		} else {
			v.put(x, y, " "+report.Stats(h), st)
		}
		y = v.extra(y, h, hostW, nameW, hgt)
	}
	if len(s.Hops) == 0 {
		v.put(0, 4, "waiting for the first round…", stDim)
	}
	v.scr.Show()
}

// extra draws the lines under a hop: its MPLS labels and the other routers
// that answered for it. Returns the last line used.
func (v *view) extra(y int, h trace.Hop, hostW, nameW, hgt int) int {
	if l := report.MPLS(h.MPLS); l != "" && y+1 < hgt {
		y++
		v.put(6, y, "["+l+"]", stDim)
	}
	for _, o := range h.Others {
		if y+1 >= hgt {
			break
		}
		y++
		x := v.put(4, y, report.Fit(report.OtherLabel(o, v.opt.Names), hostW), stDim)
		if v.opt.ShowAS {
			x = v.put(x, y, " "+report.Fit(report.OtherASN(o), 9)+" ", stAS)
			x = v.put(x, y, report.Fit(o.ASName, nameW)+" ", stDim)
			x = v.put(x, y, report.Fit(o.Country, 2)+" ", stDim)
		}
		note := " " + report.Seen(o)
		if l := report.MPLS(o.MPLS); l != "" {
			note += "  [" + l + "]"
		}
		v.put(x, y, note, stDim)
	}
	return y
}

var levels = []rune("▁▂▃▄▅▆▇█")

// bars draws the most recent results of one hop, scaled to that hop's own
// fastest and slowest answer, so jitter stands out even on slow hops.
func (v *view) bars(x, y int, recent []float64, width int) {
	if width <= 0 {
		return
	}
	if len(recent) > width {
		recent = recent[len(recent)-width:]
	}
	lo, hi := -1.0, -1.0
	for _, r := range recent {
		if r < 0 {
			continue
		}
		if lo < 0 || r < lo {
			lo = r
		}
		if r > hi {
			hi = r
		}
	}
	for _, r := range recent {
		if r < 0 {
			v.scr.SetContent(x, y, '·', nil, stBad)
			x++
			continue
		}
		i := 0
		if hi-lo >= 0.5 { // under half a millisecond it is noise
			i = int((r - lo) / (hi - lo) * float64(len(levels)-1))
		}
		st := stGood
		if i >= len(levels)*3/4 {
			st = stWarn
		}
		v.scr.SetContent(x, y, levels[i], nil, st)
		x++
	}
}
