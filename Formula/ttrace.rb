class Ttrace < Formula
  desc "Network route tracer with AS number, AS name and country for every hop"
  homepage "https://github.com/trelowney/ttrace"
  version "0.8.0"
  license "GPL-3.0-or-later"

  on_macos do
    on_arm do
      url "https://github.com/trelowney/ttrace/releases/download/v0.8.0/ttrace-darwin-arm64"
      sha256 "c4299a01f3f7889e5466d4c678043063a16e153f555afb118b027f520812ee6f"
    end
    on_intel do
      url "https://github.com/trelowney/ttrace/releases/download/v0.8.0/ttrace-darwin-amd64"
      sha256 "7d8af685f3c5bcd721a812bca71f831532889ac83cef7206b498fafd3d09ddae"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/trelowney/ttrace/releases/download/v0.8.0/ttrace-linux-arm64"
      sha256 "744dad89b9bfd2affb13286ad0d1627d60cb78cc9bd0adfdfc7ed948d12ccada"
    end
    on_intel do
      url "https://github.com/trelowney/ttrace/releases/download/v0.8.0/ttrace-linux-amd64"
      sha256 "2dd42db647f390d87b2e2aca5e9b2e43d905e624d078bf6339b321b1531cfdcd"
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
