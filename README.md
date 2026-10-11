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

## License

GPL-3.0-or-later. Copyright © 2026 trelowney.
