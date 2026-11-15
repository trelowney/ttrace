class Ttrace < Formula
  desc "Network route tracer with AS number, AS name and country for every hop"
  homepage "https://github.com/trelowney/ttrace"
  version "0.8.2"
  license "GPL-3.0-or-later"

  on_macos do
    on_arm do
      url "https://github.com/trelowney/ttrace/releases/download/v0.8.2/ttrace-darwin-arm64"
      sha256 "5428a17849ebe611b03e31d21d65bf6e30e9fda95bb4930198a113355ea7bc99"
    end
    on_intel do
      url "https://github.com/trelowney/ttrace/releases/download/v0.8.2/ttrace-darwin-amd64"
      sha256 "a8551d02312f66a8965e244f9e89db476eb67c41ed57af184a4c0006afd7cc84"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/trelowney/ttrace/releases/download/v0.8.2/ttrace-linux-arm64"
      sha256 "d859a25027b5fd0581269f8dd02e14ca820f3f0d7f18f61a134f07d0343d0f65"
    end
    on_intel do
      url "https://github.com/trelowney/ttrace/releases/download/v0.8.2/ttrace-linux-amd64"
      sha256 "9384aa3992ff65271f197c95b857a4d5e14a238d5da9ab387e5d84ad8c296b21"
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
