#!/bin/sh
# Writes the Homebrew formula: brew-formula.sh <version> <base URL> <folder with the binaries>
# The binaries are ttrace-darwin-arm64, ttrace-darwin-amd64, ttrace-linux-arm64, ttrace-linux-amd64.
set -eu
ver=$1 base=$2 dir=$3
sha() { shasum -a 256 "$dir/$1" | cut -d' ' -f1; }
cat <<RB
class Ttrace < Formula
  desc "Network route tracer with AS number, AS name and country for every hop"
  homepage "https://github.com/trelowney/ttrace"
  version "$ver"
  license "GPL-3.0-or-later"

  on_macos do
    on_arm do
      url "$base/ttrace-darwin-arm64"
      sha256 "$(sha ttrace-darwin-arm64)"
    end
    on_intel do
      url "$base/ttrace-darwin-amd64"
      sha256 "$(sha ttrace-darwin-amd64)"
    end
  end

  on_linux do
    on_arm do
      url "$base/ttrace-linux-arm64"
      sha256 "$(sha ttrace-linux-arm64)"
    end
    on_intel do
      url "$base/ttrace-linux-amd64"
      sha256 "$(sha ttrace-linux-amd64)"
    end
  end

  def install
    bin.install Dir["ttrace-*"].first => "ttrace"
  end

  def caveats
    <<~EOS
      macOS: ttrace works without sudo; only UDP and TCP probes (-P udp|tcp) need sudo.
      Linux: sending raw packets needs extra rights, once:
        sudo setcap cap_net_raw+ep #{opt_bin}/ttrace   (or run it with sudo)
    EOS
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/ttrace --version")
  end
end
RB
