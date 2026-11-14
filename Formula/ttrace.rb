class Ttrace < Formula
  desc "Network route tracer with AS number, AS name and country for every hop"
  homepage "https://github.com/trelowney/ttrace"
  version "0.8.1"
  license "GPL-3.0-or-later"

  on_macos do
    on_arm do
      url "https://github.com/trelowney/ttrace/releases/download/v0.8.1/ttrace-darwin-arm64"
      sha256 "d7863e1d44ebd6bf41be4cbd495996924d9622f97fc9a6cd8ea887c16bc705d7"
    end
    on_intel do
      url "https://github.com/trelowney/ttrace/releases/download/v0.8.1/ttrace-darwin-amd64"
      sha256 "33dab0a6f6b88557c2d66c2a7741712139e7f293d8c2f5051f9c0baf8373ecd5"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/trelowney/ttrace/releases/download/v0.8.1/ttrace-linux-arm64"
      sha256 "74df6e233b139e67146f201feb79a54535e44a9f75ce35638d40e23ea47b2602"
    end
    on_intel do
      url "https://github.com/trelowney/ttrace/releases/download/v0.8.1/ttrace-linux-amd64"
      sha256 "f6655bf914d7a3084d702e3cebe74bfd3e779b5e770d6ad02716e85c3cad2522"
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
