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
  ttrace example.com              live view (q to quit)
  ttrace -r -c 10 example.com     run 10 rounds and print a report
  ttrace -j -c 5 2001:db8::1      run 5 rounds and print JSON

Options:
`

func main() {
	var (
		count      = flag.Int("c", 0, "number of rounds (default: endless in live view, 10 with -r/-j)")
		reportMode = flag.Bool("r", false, "report mode: run -c rounds, print a table and exit")
		jsonMode   = flag.Bool("j", false, "like -r but print JSON")
		jsonStream = flag.Bool("json-stream", false, "print one JSON line after every round (for scripts and the GUI)")
		interval   = flag.Duration("i", time.Second, "time between rounds")
		timeout    = flag.Duration("w", 2*time.Second, "how long to wait for an answer")
		maxTTL     = flag.Int("m", 30, "maximum number of hops")
		only4      = flag.Bool("4", false, "use IPv4 only")
		only6      = flag.Bool("6", false, "use IPv6 only")
		noDNS      = flag.Bool("n", false, "show IP addresses, do not look up host names")
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	target := flag.Arg(0)
	ip, err := resolve(ctx, target, *only4, *only6)
	if err != nil {
		fail(err.Error())
	}

	p, err := probe.New(ip)
	if err != nil {
		fail(err.Error())
	}
	defer p.Close()

	batch := *reportMode || *jsonMode
	if batch && *count == 0 {
		*count = 10
	}
	eng := trace.New(trace.Config{
		Target:   target,
		IP:       ip,
		MaxTTL:   *maxTTL,
		Interval: *interval,
		Timeout:  *timeout,
		Count:    *count,
		DNS:      !*noDNS,
		AS:       !*noAS,
	}, p)

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
		if *jsonMode {
			report.JSON(os.Stdout, s, true)
		} else {
			report.Text(os.Stdout, s, names, !*noAS)
		}
	default:
		if err := tui.Run(ctx, eng, version); err != nil {
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
		return nil, fmt.Errorf("cannot find the address of %q — check the name, or try -4 / -6", host)
	}
	return ips[0], nil
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "ttrace:", msg)
	os.Exit(1)
}
