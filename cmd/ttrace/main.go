// ttrace — network route tracer with AS number, AS name and country for every hop.
//
// Copyright (C) 2026 trelowney
//
// This program is free software: you can redistribute it and/or modify it
// under the terms of the GNU General Public License as published by the Free
// Software Foundation, either version 3 of the License, or (at your option)
// any later version. See the LICENSE file.
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/trelowney/ttrace/internal/config"
	"github.com/trelowney/ttrace/internal/geo"
	"github.com/trelowney/ttrace/internal/probe"
	"github.com/trelowney/ttrace/internal/report"
	"github.com/trelowney/ttrace/internal/trace"
	"github.com/trelowney/ttrace/internal/tui"
)

var version = "dev"

const usage = `ttrace %s — trace the route to a host, with AS number, AS name and country for every hop

Usage:
  ttrace [options] <host or IP> [more hosts…]

Examples:
  ttrace example.com                      live view (q quit, d pulse, 4/6 switch IPv4/IPv6)
  ttrace -6 example.com                   trace over IPv6
  ttrace -r -c 10 example.com             run 10 rounds and print a report
  ttrace -r --format markdown example.com report as a Markdown table
  ttrace --hide 2 example.com             hide your own first 2 hops (for screenshots)
  ttrace -P tcp -p 443 example.com        probe with TCP to port 443, gets through most firewalls
  ttrace -P udp example.com               probe with UDP; also shows parallel paths (ECMP)
  ttrace example.com example.net          two targets at once, Tab switches between them
  ttrace -r --format dot example.com > route.dot && dot -Tsvg route.dot -o route.svg
                                          draw the route as a picture with Graphviz
  ttrace example.com, then press m        the route on a world map; the location data
                                          (%s) is downloaded automatically on the first run

Settings file: options can also be written into %s,
one per line with the same names as below, e.g. "interval = 2s" or "no-as".
Options given on the command line win.

Options:
`

func main() {
	defaultConfig := config.DefaultPath()
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), usage, version, geo.Size, defaultConfig)
		flag.PrintDefaults()
	}
	flag.String("config", defaultConfig, "settings file to read")
	opts := defineFlags()
	loadConfig(defaultConfig)
	flag.Parse()
	run(opts)
}

// loadConfig applies the settings file before the command line is parsed,
// so that command line options override it.
func loadConfig(def string) {
	path, must := def, false
	args := os.Args[1:]
	for i, a := range args {
		if a == "--" {
			break
		}
		name, val, hasVal := strings.Cut(strings.TrimLeft(a, "-"), "=")
		if !strings.HasPrefix(a, "-") || name != "config" {
			continue
		}
		if !hasVal && i+1 < len(args) {
			val = args[i+1]
		}
		path, must = val, true
	}
	if path == "" {
		return
	}
	settings, err := config.Load(path, must)
	if err != nil {
		fail("cannot read the settings file: " + err.Error())
	}
	for _, st := range settings {
		f := flag.Lookup(st.Name)
		if f == nil || st.Name == "config" {
			fail(fmt.Sprintf("%s line %d: unknown option %q — see ttrace -h for the names", path, st.Line, st.Name))
		}
		if err := f.Value.Set(st.Value); err != nil {
			fail(fmt.Sprintf("%s line %d: bad value for %s: %v", path, st.Line, st.Name, err))
		}
	}
}

type options struct {
	count, firstTTL, maxTTL, giveUp, hide, size, tos, port *int
	reportMode, jsonMode, wide, jsonStream, only4, only6   *bool
	noDNS, noAS, showVer, geoUpdate, noGeo                 *bool
	format, source, probeType                              *string
	interval, timeout                                      *time.Duration
}

func defineFlags() options {
	return options{
		count:      flag.Int("c", 0, "number of rounds (default: endless in live view, 10 with -r/-j)"),
		reportMode: flag.Bool("r", false, "report mode: run -c rounds, print the result and exit"),
		format:     flag.String("format", "text", "report format: text, json, csv, markdown or dot (Graphviz)"),
		jsonMode:   flag.Bool("j", false, "short for -r --format json"),
		wide:       flag.Bool("wide", false, "do not shorten long host and AS names in text reports"),
		jsonStream: flag.Bool("json-stream", false, "print one JSON line after every round (for scripts and the GUI)"),
		interval:   flag.Duration("i", time.Second, "time between rounds"),
		timeout:    flag.Duration("w", 2*time.Second, "how long to wait for an answer"),
		firstTTL:   flag.Int("f", 1, "first hop to probe (skip the hops before it)"),
		maxTTL:     flag.Int("m", 30, "maximum number of hops"),
		giveUp:     flag.Int("give-up", 0, "stop probing past this many silent hops in a row after the last answer (0 = never)"),
		hide:       flag.Int("hide", 0, "hide address, name and AS of the first N hops, e.g. your home network"),
		probeType:  flag.String("P", "icmp", "probe type: icmp, udp or tcp (udp and tcp: Linux and macOS only)"),
		port:       flag.Int("p", 0, "destination port for -P udp (default 33434) and -P tcp (default 80)"),
		size:       flag.Int("s", 64, "packet size in bytes, including IP and ICMP/UDP headers (not used with -P tcp)"),
		tos:        flag.Int("tos", 0, "TOS / traffic class byte, e.g. 184 for DSCP EF"),
		source:     flag.String("a", "", "send from this local IP address"),
		only4:      flag.Bool("4", false, "use IPv4 only"),
		only6:      flag.Bool("6", false, "use IPv6 only"),
		noDNS:      flag.Bool("n", false, "do not look up host names, show IP addresses (key n in the live view switches name / IP / both)"),
		noAS:       flag.Bool("no-as", false, "do not look up AS number, AS name and country"),
		showVer:    flag.Bool("v", false, "print version and exit"),
		geoUpdate:  flag.Bool("geo-update", false, "download the newest location data for the map now and exit (normally this happens automatically)"),
		noGeo:      flag.Bool("no-geo", false, "do not download the location data for the map ("+geo.Size+"); a copy downloaded before is still used"),
	}
}

func run(o options) {
	count, reportMode, format, jsonMode, wide, jsonStream := o.count, o.reportMode, o.format, o.jsonMode, o.wide, o.jsonStream
	interval, timeout, firstTTL, maxTTL, giveUp, hide := o.interval, o.timeout, o.firstTTL, o.maxTTL, o.giveUp, o.hide
	probeType, port, size, tos, source := o.probeType, o.port, o.size, o.tos, o.source
	only4, only6, noDNS, noAS, showVer := o.only4, o.only6, o.noDNS, o.noAS, o.showVer

	if *showVer {
		fmt.Println("ttrace", version)
		return
	}
	gdb, gerr := geo.Open(geo.DefaultPath())
	if *o.geoUpdate {
		updateGeo(gdb)
		return
	}
	if gerr != nil && *o.noGeo {
		fmt.Fprintln(os.Stderr, "ttrace:", gerr) // the trace still works, only without places
	}
	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(2)
	}
	if *maxTTL < 1 || *maxTTL > 255 {
		fail("-m must be between 1 and 255")
	}
	if *firstTTL < 1 || *firstTTL > *maxTTL {
		fail("-f must be between 1 and the maximum number of hops (-m)")
	}
	if *size < 28 || *size > 9000 {
		fail("-s must be between 28 and 9000 bytes")
	}
	if *tos < 0 || *tos > 255 {
		fail("--tos must be between 0 and 255")
	}
	switch *probeType {
	case "icmp", "udp", "tcp":
	default:
		fail("-P must be icmp, udp or tcp")
	}
	if *port < 0 || *port > 65535 {
		fail("-p must be a port number between 1 and 65535")
	}
	if *port != 0 && *probeType == "icmp" {
		fail("-p only works with UDP or TCP probes, add -P udp or -P tcp")
	}
	if *jsonMode {
		*reportMode, *format = true, "json"
	}
	if *format != "text" && !*jsonStream {
		*reportMode = true // other formats only make sense as a report
	}
	switch *format {
	case "text", "json", "csv", "markdown", "md", "dot":
	default:
		fail("--format must be text, json, csv, markdown or dot")
	}
	popts := probe.Options{Size: *size, TOS: *tos, Proto: *probeType, Port: *port}
	probeDesc := "ICMP"
	switch *probeType {
	case "udp":
		if *port == 0 {
			*port = probe.DefaultUDPPort
		}
		probeDesc = fmt.Sprintf("UDP port %d", *port)
	case "tcp":
		if *port == 0 {
			*port = probe.DefaultTCPPort
		}
		probeDesc = fmt.Sprintf("TCP port %d", *port)
	}
	if *source != "" {
		if popts.Source = net.ParseIP(*source); popts.Source == nil {
			fail("-a needs an IP address, e.g. -a 192.0.2.5")
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if !*o.noGeo && gdb.Stale() {
		// Missing or older than a month: fetch it while the trace runs. The
		// live view shows the progress in the map (key m).
		gdb.Start(ctx, nil)
	}

	batch := *reportMode
	if batch && *count == 0 {
		*count = 10
	}
	names := report.NameHost
	if *noDNS {
		names = report.NameIP
	}

	var tabs []tui.Tab
	for _, target := range flag.Args() {
		ip, err := resolve(ctx, target, *only4, *only6)
		if err != nil {
			fail(err.Error())
		}
		p, err := probe.New(ip, popts)
		if err != nil {
			fail(err.Error())
		}
		eng := trace.New(trace.Config{
			Target:   target,
			Probe:    probeDesc,
			IP:       ip,
			FirstTTL: *firstTTL,
			MaxTTL:   *maxTTL,
			GiveUp:   *giveUp,
			Hide:     *hide,
			Interval: *interval,
			Timeout:  *timeout,
			Count:    *count,
			DNS:      !*noDNS,
			AS:       !*noAS,
			Geo:      gdb,
		}, p)
		defer eng.Close()
		tab := tui.Tab{Eng: eng}
		if net.ParseIP(target) == nil {
			tab.Switch = func(v6 bool) (net.IP, probe.Prober, error) {
				ip, err := resolve(ctx, target, !v6, v6)
				if err != nil {
					return nil, nil, err
				}
				if popts.Source != nil && (popts.Source.To4() == nil) != v6 {
					return nil, nil, fmt.Errorf("the source address %s (-a) is a different IP version", popts.Source)
				}
				p, err := probe.New(ip, popts)
				return ip, p, err
			}
		}
		tabs = append(tabs, tab)
	}

	switch {
	case *jsonStream:
		var mu sync.Mutex
		runAll(ctx, tabs, func(s trace.Snapshot) {
			s.Geo = geoNote(gdb)
			mu.Lock()
			report.JSON(os.Stdout, s, false)
			mu.Unlock()
		})
	case batch:
		runAll(ctx, tabs, nil)
		if gdb.Status().Running {
			fmt.Fprintf(os.Stderr, "ttrace: finishing the download of the location data (%s, first run only)…\n", geo.Size)
			gdb.Wait()
		}
		var snaps []trace.Snapshot
		for _, t := range tabs {
			t.Eng.WaitLookups(5 * time.Second)
			snaps = append(snaps, t.Eng.Snapshot())
		}
		switch *format {
		case "json":
			if len(snaps) == 1 {
				report.JSON(os.Stdout, snaps[0], true)
			} else {
				report.JSONList(os.Stdout, snaps)
			}
		case "csv":
			report.CSV(os.Stdout, snaps)
		case "dot":
			report.Dot(os.Stdout, snaps, names, !*noAS)
		default:
			for i, s := range snaps {
				if i > 0 {
					fmt.Println()
				}
				if *format == "text" {
					report.Text(os.Stdout, s, names, !*noAS, *wide)
				} else {
					report.Markdown(os.Stdout, s, names, !*noAS)
				}
			}
		}
	default:
		if err := tui.Run(ctx, tabs, tui.Options{Version: version, Names: names, ShowAS: !*noAS, Geo: gdb}); err != nil {
			fail(err.Error())
		}
	}
}

// geoNote describes a running or failed download of the location data.
func geoNote(d *geo.DB) string {
	st := d.Status()
	switch {
	case st.Running && st.Total > 0:
		return fmt.Sprintf("Downloading the location data for the map: %d of %d MB", st.Done>>20, st.Total>>20)
	case st.Running:
		return fmt.Sprintf("Downloading the location data for the map: %d MB", st.Done>>20)
	case st.Err != "":
		return "The location data for the map could not be downloaded: " + st.Err
	}
	return ""
}

// updateGeo downloads the location database with a progress line.
func updateGeo(d *geo.DB) {
	fmt.Fprintf(os.Stderr, "Downloading the location database (%s) from db-ip.com…\n", geo.Size)
	last := time.Time{}
	err := d.Download(context.Background(), func(done, total int64) {
		if time.Since(last) < 200*time.Millisecond {
			return
		}
		last = time.Now()
		if total > 0 {
			fmt.Fprintf(os.Stderr, "\r  %d of %d MB", done>>20, total>>20)
		} else {
			fmt.Fprintf(os.Stderr, "\r  %d MB", done>>20)
		}
	})
	fmt.Fprintln(os.Stderr)
	if err != nil {
		fail(err.Error())
	}
	fmt.Fprintf(os.Stderr, "Saved to %s\nThe live view now shows the route on a map: press m.\n%s\n", d.Path(), geo.Attribution)
}

// runAll traces all targets at the same time until each is done.
func runAll(ctx context.Context, tabs []tui.Tab, update func(trace.Snapshot)) {
	var wg sync.WaitGroup
	for _, t := range tabs {
		wg.Add(1)
		go func(e *trace.Engine) {
			defer wg.Done()
			e.Run(ctx, update)
		}(t.Eng)
	}
	wg.Wait()
}

func resolve(ctx context.Context, host string, only4, only6 bool) (net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		if only4 && ip.To4() == nil || only6 && ip.To4() != nil {
			return nil, fmt.Errorf("%s does not match the -4/-6 option", host)
		}
		return ip, nil
	}
	network := "ip"
	switch {
	case only4:
		network = "ip4"
	case only6:
		network = "ip6"
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(ctx, network, host)
	if err != nil || len(ips) == 0 {
		fam := ""
		switch {
		case only4:
			fam = "IPv4 "
		case only6:
			fam = "IPv6 "
		}
		return nil, fmt.Errorf("cannot find an %saddress for %q — check the name, or try the other IP version (-4 / -6)", fam, host)
	}
	return ips[0], nil
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "ttrace:", msg)
	os.Exit(1)
}
