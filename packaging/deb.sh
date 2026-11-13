#!/bin/sh
# Builds a Debian package: deb.sh <version> <arch: amd64|arm64> <binary> <output folder>
set -eu
ver=$1 arch=$2 bin=$3 out=$4
root=$(mktemp -d)
trap 'rm -rf "$root"' EXIT
mkdir -p "$root/DEBIAN" "$root/usr/bin" "$root/usr/share/doc/ttrace"
install -m 755 "$bin" "$root/usr/bin/ttrace"
cat > "$root/DEBIAN/control" <<CTL
Package: ttrace
Version: $ver
Architecture: $arch
Maintainer: trelowney <54177826+trelowney@users.noreply.github.com>
Depends: libcap2-bin
Section: net
Priority: optional
Homepage: https://github.com/trelowney/ttrace
Description: network route tracer with AS number, AS name and country for every hop
 Traces the route to a host like mtr or traceroute and shows, for every hop,
 who runs it: AS number, AS name and country, next to loss and latency.
 Also marks internet exchanges, latency jumps, DSCP rewrites, finds the path
 MTU and draws the route on a world map in the terminal.
CTL
cat > "$root/DEBIAN/postinst" <<'POST'
#!/bin/sh
set -e
if [ "$1" = configure ]; then
	# lets ttrace send probes without sudo
	setcap cap_net_raw+ep /usr/bin/ttrace || echo "ttrace: could not allow raw sockets, run it with sudo" >&2
fi
POST
chmod 755 "$root/DEBIAN/postinst"
cat > "$root/usr/share/doc/ttrace/copyright" <<'CPY'
Format: https://www.debian.org/doc/packaging-manuals/copyright-format/1.0/
Upstream-Name: ttrace
Source: https://github.com/trelowney/ttrace

Files: *
Copyright: 2026 trelowney
License: GPL-3.0-or-later
 On Debian systems the full text is in /usr/share/common-licenses/GPL-3.
CPY
mkdir -p "$out"
dpkg-deb --root-owner-group -Zxz --build "$root" "$out/ttrace_${ver}_${arch}.deb"
