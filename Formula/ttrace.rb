class Ttrace < Formula
  desc "Network route tracer with AS number, AS name and country for every hop"
  homepage "https://github.com/trelowney/ttrace"
  version "0.7.0"
  license "GPL-3.0-or-later"

  on_macos do
    on_arm do
      url "https://github.com/trelowney/ttrace/releases/download/v0.7.0/ttrace-darwin-arm64"
      sha256 "01a52f84d6c62903f4ab5b2503cf34e590c72a905eedf3671f67b8e8ccea61d2"
    end
    on_intel do
      url "https://github.com/trelowney/ttrace/releases/download/v0.7.0/ttrace-darwin-amd64"
      sha256 "9be43f3c50c976efec8494bda32ed0bdeb0d8a24e7c73e6c888a19808f304761"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/trelowney/ttrace/releases/download/v0.7.0/ttrace-linux-arm64"
      sha256 "fc9f0bbb6369a92ec639be077cc7bf628012e65910e21861fff8da98079ad480"
    end
    on_intel do
      url "https://github.com/trelowney/ttrace/releases/download/v0.7.0/ttrace-linux-amd64"
      sha256 "765ec9819cc454376c70d579746f4120f7e30bc6c9ac499255e29307d455bdc0"
    end
  end

  def install
    bin.install Dir["ttrace-*"].first => "ttrace"
  end

  def caveats
    <<~EOS
      ttrace sends raw network packets, which needs extra rights:
        macOS: run it with sudo, e.g.  sudo ttrace example.com
        Linux: sudo setcap cap_net_raw+ep #{opt_bin}/ttrace  (once), or use sudo
    EOS
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/ttrace --version")
  end
end
