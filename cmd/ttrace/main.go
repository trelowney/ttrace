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
	"time"

	"github.com/trelowney/ttrace/internal/probe"
	"github.com/trelowney/ttrace/internal/report"
	"github.com/trelowney/ttrace/internal/trace"
	"github.com/trelowney/ttrace/internal/tui"
)

var version = "dev"

const usage = `ttrace %s — trace the route to a host, with AS number, AS name and country for every hop

Usage:
  ttrace [options] <host or IP>

Examples:
  ttrace example.com                      live view (q quit, d pulse, 4/6 switch IPv4/IPv6)
  ttrace -6 example.com                   trace over IPv6
  ttrace -r -c 10 example.com             run 10 rounds and print a report
  ttrace -r --format markdown example.com report as a Markdown table
  ttrace --hide 2 example.com             hide your own first 2 hops (for screenshots)

Options:
`

func main() {
	var (
		count      = flag.Int("c", 0, "number of rounds (default: endless in live view, 10 with -r/-j)")
		reportMode = flag.Bool("r", false, "report mode: run -c rounds, print the result and exit")
		format     = flag.String("format", "text", "report format: text, json, csv or markdown")
		jsonMode   = flag.Bool("j", false, "short for -r --format json")
		wide       = flag.Bool("wide", false, "do not shorten long host and AS names in text reports")
		jsonStream = flag.Bool("json-stream", false, "print one JSON line after every round (for scripts and the GUI)")
		interval   = flag.Duration("i", time.Second, "time between rounds")
		timeout    = flag.Duration("w", 2*time.Second, "how long to wait for an answer")
		firstTTL   = flag.Int("f", 1, "first hop to probe (skip the hops before it)")
		maxTTL     = flag.Int("m", 30, "maximum number of hops")
		giveUp     = flag.Int("give-up", 0, "stop probing past this many silent hops in a row after the last answer (0 = never)")
		hide       = flag.Int("hide", 0, "hide address, name and AS of the first N hops, e.g. your home network")
		size       = flag.Int("s", 64, "packet size in bytes, including IP and ICMP headers")
		tos        = flag.Int("tos", 0, "TOS / traffic class byte, e.g. 184 for DSCP EF")
		source     = flag.String("a", "", "send from this local IP address")
		only4      = flag.Bool("4", false, "use IPv4 only")
		only6      = flag.Bool("6", false, "use IPv6 only")
		noDNS      = flag.Bool("n", false, "do not look up host names, show IP addresses (key n in the live view switches name / IP / both)")
		noAS       = flag.Bool("no-as", false, "do not look up AS number, AS name and country")
		showVer    = flag.Bool("v", false, "print version and exit")
	)
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), usage, version)
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVer {
		fmt.Println("ttrace", version)
		return
	}
	if flag.NArg() != 1 {
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
	if *jsonMode {
		*reportMode, *format = true, "json"
	}
	switch *format {
	case "text", "json", "csv", "markdown", "md":
	default:
		fail("--format must be text, json, csv or markdown")
	}
	popts := probe.Options{Size: *size, TOS: *tos}
	if *source != "" {
		if popts.Source = net.ParseIP(*source); popts.Source == nil {
			fail("-a needs an IP address, e.g. -a 192.0.2.5")
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	target := flag.Arg(0)
	ip, err := resolve(ctx, target, *only4, *only6)
	if err != nil {
		fail(err.Error())
	}

	p, err := probe.New(ip, popts)
	if err != nil {
		fail(err.Error())
	}

	batch := *reportMode
	if batch && *count == 0 {
		*count = 10
	}
	eng := trace.New(trace.Config{
		Target:   target,
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
	}, p)
	defer eng.Close()

	names := report.NameHost
	if *noDNS {
		names = report.NameIP
	}

	switch {
	case *jsonStream:
		eng.Run(ctx, func(s trace.Snapshot) { report.JSON(os.Stdout, s, false) })
	case batch:
		eng.Run(ctx, nil)
		eng.WaitLookups(5 * time.Second)
		s := eng.Snapshot()
		switch *format {
		case "json":
			report.JSON(os.Stdout, s, true)
		case "csv":
			report.CSV(os.Stdout, s)
		case "markdown", "md":
			report.Markdown(os.Stdout, s, names, !*noAS)
		default:
			report.Text(os.Stdout, s, names, !*noAS, *wide)
		}
	default:
		opt := tui.Options{Version: version, Names: names, ShowAS: !*noAS}
		if net.ParseIP(target) == nil {
			opt.Switch = func(v6 bool) (net.IP, probe.Prober, error) {
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
		if err := tui.Run(ctx, eng, opt); err != nil {
			fail(err.Error())
		}
	}
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
