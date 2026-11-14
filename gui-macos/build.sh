#!/bin/sh
# Builds ttrace.app (Apple silicon and Intel in one) with the measuring
# program inside. Runs on macOS with Xcode and Go:
#   gui-macos/build.sh <version shown by ttrace --version> <version for the app, digits and dots>
# Result: gui-macos/build/ttrace.app
set -eu
V=${1:-dev}
AV=${2:-0.0.0}
cd "$(dirname "$0")"
rm -rf build
APP=build/ttrace.app
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"

for a in arm64 amd64; do
  (cd .. && CGO_ENABLED=0 GOOS=darwin GOARCH=$a go build -trimpath \
    -ldflags "-s -w -X main.version=$V" -o "gui-macos/build/ttrace-$a" ./cmd/ttrace)
done
lipo -create -output "$APP/Contents/MacOS/ttrace" build/ttrace-arm64 build/ttrace-amd64

for t in arm64 x86_64; do
  swiftc -swift-version 5 -parse-as-library -O -target "$t-apple-macos13.0" \
    Sources/*.swift -o "build/gui-$t"
done
lipo -create -output "$APP/Contents/MacOS/ttrace-gui" build/gui-arm64 build/gui-x86_64

cp ../gui/Assets/land.bin "$APP/Contents/Resources/"
sed "s/__VERSION__/$AV/g" Info.plist > "$APP/Contents/Info.plist"
plutil -lint "$APP/Contents/Info.plist"

# no Apple developer certificate: an ad-hoc signature, which Apple silicon needs to run it at all
codesign --force --deep --sign - "$APP"
codesign --verify --deep --strict "$APP"
lipo -info "$APP/Contents/MacOS/ttrace" "$APP/Contents/MacOS/ttrace-gui"
