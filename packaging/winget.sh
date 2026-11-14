#!/bin/sh
# Writes the winget manifests: winget.sh <version> <base URL> <folder with the binaries> <output folder>
# The binaries are ttrace-windows-amd64.exe and ttrace-windows-arm64.exe.
# The output folder gets the three files winget-pkgs wants for one version.
set -eu
ver=$1 base=$2 dir=$3 out=$4
sha() { sha256sum "$dir/$1" | cut -d' ' -f1 | tr a-f A-F; }
mkdir -p "$out"
id=trelowney.ttrace
mv=1.12.0

cat > "$out/$id.yaml" <<EOF
# yaml-language-server: \$schema=https://aka.ms/winget-manifest.version.$mv.schema.json

PackageIdentifier: $id
PackageVersion: $ver
DefaultLocale: en-US
ManifestType: version
ManifestVersion: $mv
EOF

cat > "$out/$id.installer.yaml" <<EOF
# yaml-language-server: \$schema=https://aka.ms/winget-manifest.installer.$mv.schema.json

PackageIdentifier: $id
PackageVersion: $ver
InstallerType: portable
Commands:
- ttrace
ReleaseDate: $(date -u +%Y-%m-%d)
Installers:
- Architecture: x64
  InstallerUrl: $base/ttrace-windows-amd64.exe
  InstallerSha256: $(sha ttrace-windows-amd64.exe)
- Architecture: arm64
  InstallerUrl: $base/ttrace-windows-arm64.exe
  InstallerSha256: $(sha ttrace-windows-arm64.exe)
ManifestType: installer
ManifestVersion: $mv
EOF

cat > "$out/$id.locale.en-US.yaml" <<EOF
# yaml-language-server: \$schema=https://aka.ms/winget-manifest.defaultLocale.$mv.schema.json

PackageIdentifier: $id
PackageVersion: $ver
PackageLocale: en-US
Publisher: trelowney
PublisherUrl: https://github.com/trelowney
PublisherSupportUrl: https://github.com/trelowney/ttrace/issues
PackageName: ttrace
PackageUrl: https://github.com/trelowney/ttrace
License: GPL-3.0-or-later
LicenseUrl: https://github.com/trelowney/ttrace/blob/main/LICENSE
ShortDescription: Network route tracer with AS number, AS name and country for every hop.
Description: |-
  ttrace traces the route to a host like mtr or tracert and shows, for every hop, who runs it:
  AS number, AS name and country, next to loss and latency statistics. It also names internet
  exchanges, prints the AS path, marks latency jumps, notices DSCP rewrites, compares two saved
  measurements and draws the route on a world map in the terminal.
Moniker: ttrace
Tags:
- network
- traceroute
- tracert
- mtr
- asn
- bgp
ReleaseNotesUrl: https://github.com/trelowney/ttrace/releases/tag/v$ver
ManifestType: defaultLocale
ManifestVersion: $mv
EOF
