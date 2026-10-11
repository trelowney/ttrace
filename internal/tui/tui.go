// Package tui is the live full-screen view.
package tui

import (
	"context"
	"fmt"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/trelowney/ttrace/internal/report"
	"github.com/trelowney/ttrace/internal/trace"
)

type view struct {
	scr    tcell.Screen
	eng    *trace.Engine
	names  report.NameMode
	showAS bool
}

// Run shows the live table until the user quits.
func Run(ctx context.Context, eng *trace.Engine, version string) error {
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

	v := &view{scr: scr, eng: eng, showAS: true}
	v.draw(version)
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
			case ev.Rune() == 'n':
				v.names = (v.names + 1) % 3
			case ev.Rune() == 'a':
				v.showAS = !v.showAS
			}
		}
		if ctx.Err() != nil {
			return nil
		}
		v.draw(version)
	}
}

var (
	stDefault = tcell.StyleDefault
	stBold    = stDefault.Bold(true)
	stDim     = stDefault.Dim(true)
	stHead    = stDefault.Reverse(true)
	stWarn    = stDefault.Foreground(tcell.ColorYellow)
	stBad     = stDefault.Foreground(tcell.ColorRed)
	stAS      = stDefault.Foreground(tcell.ColorTeal)
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

func (v *view) draw(version string) {
	s := v.eng.Snapshot()
	v.scr.Clear()
	w, hgt := v.scr.Size()

	state := fmt.Sprintf("round %d", s.Round)
	if v.eng.Paused() {
		state = "PAUSED"
	}
	x := v.put(0, 0, "ttrace "+version, stBold)
	x = v.put(x, 0, "  →  "+s.Target, stDefault)
	if s.Target != s.IP {
		x = v.put(x, 0, " ("+s.IP+")", stDim)
	}
	v.put(x, 0, "   "+state, stDim)
	v.put(0, 1, fmt.Sprintf("keys: q quit  p pause  r reset  n names (%s)  a AS columns", v.names), stDim)

	// columns: "#" 4, host flexible, AS block, stats
	statsW := len(report.StatsHeader)
	asW, nameW := 0, 0
	if v.showAS {
		nameW = 22
		if w < 110 {
			nameW = 12
		}
		asW = 11 + nameW + 1 + 3 // " ASN " + name + " " + "CC "
	}
	hostW := w - 4 - asW - statsW - 2
	if hostW < 12 {
		hostW = 12
	}

	head := fmt.Sprintf("%3s %s", "#", report.Fit("Host", hostW))
	if v.showAS {
		head += fmt.Sprintf(" %-9s %s %-2s ", "ASN", report.Fit("AS name", nameW), "CC")
	}
	head += " " + report.StatsHeader
	v.put(0, 3, report.Fit(head, w), stHead)

	for i, h := range s.Hops {
		y := 4 + i
		if y >= hgt {
			break
		}
		x := v.put(0, y, fmt.Sprintf("%3d ", h.TTL), stDim)
		hs := stDefault
		if h.Addr == "" {
			hs = stDim
		}
		x = v.put(x, y, report.Fit(report.Label(h, v.names), hostW), hs)
		if v.showAS {
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
		v.put(x, y, " "+report.Stats(h), st)
	}
	if len(s.Hops) == 0 {
		v.put(0, 4, "waiting for the first round…", stDim)
	}
	v.scr.Show()
}
