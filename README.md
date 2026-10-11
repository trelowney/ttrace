# ttrace

Trace the route to a host and see, for every hop, **who runs it**: AS number,
AS name and the country where the address block is registered — next to the
usual loss and latency statistics.

```
  #  Host                      ASN       AS name                CC   Loss%  Snt   Last    Avg   Best   Wrst  StDev
  1  router.lan                private                             0.0%   10    0.6    0.7    0.5    1.1    0.2
  2  ...                       AS2852    CESNET2                CZ   0.0%   10    4.1    4.3    3.9    5.0    0.3
```

Runs on Linux, macOS and Windows. IPv4 and IPv6.

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
