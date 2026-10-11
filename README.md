# ttrace

Trace the route to a host and see, for every hop, **who runs it**: AS number,
AS name and the country where the address block is registered — next to the
usual loss and latency statistics.

> **Work in progress.** ttrace is under active development and largely untested.
> So far it has only been tried on Linux (IPv4 and IPv6); the macOS and Windows
> builds have not been tested yet. Expect bugs and changing options.

```
$ ttrace -r -c 3 1.1.1.1
  #  Host                           ASN       AS name              CC   Loss%  Snt   Last    Avg   Best   Wrst  StDev
  1. 172.17.0.1                     private                              0.0%    3    0.2    0.2    0.1    0.4    0.1
  2. 5.175.174.1                    AS209874  TECHTIDE             DE    0.0%    3    1.4    1.3    1.2    1.4    0.1
  3. ae1.987.edge2.egh.as49581.net  AS49581   TUBE-HOSTING         DE    0.0%    3    1.0    2.7    1.0    6.2    2.4
  4. as13335.frys-ix.net            AS39326   HSO-GROUP            NL    0.0%    3    5.2   12.3    5.2   23.6    8.1
  5. 141.101.65.184                 AS13335   CLOUDFLARENET - Clo… US    0.0%    3    5.8    7.3    5.8    9.1    1.4
  6. one.one.one.one                AS13335   CLOUDFLARENET - Clo… AU    0.0%    3   11.3    8.8    4.7   11.3    2.9
```

Builds for Linux, macOS and Windows. IPv4 and IPv6.

## Usage

```
ttrace example.com              live view (q quit, p pause, r reset, n names, a AS columns)
ttrace -r -c 10 example.com     run 10 rounds and print a report
ttrace -j -c 5 example.com      the same as JSON
ttrace --json-stream host       one JSON line after every round
```

Run `ttrace -h` for all options.

### Several targets

`ttrace example.com example.net` traces both at once. In the live view each
target has a tab (Tab / arrow keys switch); reports print one after another.

### Route as a picture

`--format dot` writes the route as a [Graphviz](https://graphviz.org) graph,
routers grouped by network operator (AS):

```
ttrace -c 10 --format dot example.com > route.dot
dot -Tsvg route.dot -o route.svg
```

### Route on a map

Press **m** in the live view to see the route drawn on a world map in the
terminal. Nothing needs to be installed or set up: on the first run ttrace
downloads the database of IP locations by itself (about 60 MB, in the
background while the trace runs) and refreshes it once a month. Use
`--no-geo` to stop that, e.g. on a metered connection.

JSON and CSV output also contain the approximate place
(city, country, latitude, longitude) of every router.

Router places are **approximate** — often only the city or even the country of
the network owner, not where the router really stands.

### Windows window

For Windows there is also a window version: download
`ttrace-gui-windows-x64.zip` (or `-arm64.zip`) from the releases, unpack the
whole zip into one folder and start `ttrace-gui.exe`. Nothing has to be
installed; ttrace.exe and everything else it needs are inside. Type the host,
press Start, and you get the same table plus the route on a world map.

So far the window has only been checked by an automatic test on a build
machine, not by a person.

### Settings file

Options you always use can go into a settings file, one per line, with the
same names as on the command line:

```
# Linux: ~/.config/ttrace/config
# macOS: ~/Library/Application Support/ttrace/config
# Windows: %AppData%\ttrace\config
interval = 2s
give-up = 5
no-as
```

Options on the command line win over the file. `--config path` reads another file.

### Probe types

* **ICMP** (default) — echo requests, like `ping`. Works everywhere.
* **UDP** (`-P udp`, port 33434 unless `-p` says otherwise) — every probe
  leaves from a new source port, so routers that spread traffic over parallel
  links (ECMP) show up as extra lines under a hop.
* **TCP** (`-P tcp`, port 80 unless `-p` says otherwise) — opens a connection,
  which gets through firewalls that drop ICMP and UDP. Use the port of a
  service the target really runs, e.g. `-P tcp -p 443`.

UDP and TCP probes work on Linux and macOS; Windows has ICMP only for now.

Routers that report **MPLS labels** (RFC 4950) get a `[MPLS …]` line under
their hop, in every probe type.

### Permissions

* **Linux:** sending raw ICMP needs root. Either use `sudo`, or allow it once:
  `sudo setcap cap_net_raw+ep $(which ttrace)`. Packages do this for you.
* **macOS:** use `sudo`.
* **Windows:** works as a normal user.

## Where the data comes from

AS number, prefix, country and AS name come from the free
[Team Cymru IP to ASN service](https://www.team-cymru.com/ip-asn-mapping) over DNS.
The country is where the address block is **registered**, which is not always
where the router physically stands.

Places on the map come from the free
[DB-IP IP to City Lite](https://db-ip.com/db/download/ip-to-city-lite) database
(IP Geolocation by [DB-IP](https://db-ip.com), CC BY 4.0), which ttrace
downloads into your cache folder when you ask for it. The land outline is
[Natural Earth](https://www.naturalearthdata.com) 1:50m land (public domain).

## License

GPL-3.0-or-later. Copyright © 2026 trelowney.
