#!/bin/bash
# One Mac slot for the Screen video work. Read-only apart from: a scratch
# directory, a Safari window (closed again), a short capture of the display, and
# an iOS simulator that is booted and shut down if it was not already running.
# Everything lands in $OUT; send that directory's summary.txt back.
#   usage: mac-screen-video.sh <commit>  (default: origin/main)
# Run it from a Worker pane the Mewla daemon started, so Screen Recording is the
# daemon's; it installs nothing and leaves Mewla as it is (no rollback needed).
# Needs: network to github.com, Screen Recording granted to the process that
# runs this (it is, when a Mewla Worker runs it).
set -u
COMMIT="${1:-main}"
W="${MEWLA_BUILD_TMPDIR:-$HOME/.mewla/t}/screen-video-bench"
OUT="$W/out"
mkdir -p "$OUT"; cd "$W" || exit 1
exec > >(tee "$OUT/run.log") 2>&1
echo "== $(date) $(sw_vers -productVersion) $(uname -m) commit=$COMMIT"
echo "Mewla: $(command -v mewla) $(mewla version 2>/dev/null | head -1)"
echo "load: $(uptime)"

# ---- 0. relay region latency: which Asian region is nearest -----------------
# 20 TLS handshakes to public endpoints in each region, with and without this
# Mac's proxy. TCP connect is about one round trip; "tls" is the whole
# handshake. Quebec (our relay) is the baseline.
FULL="${FULL:-}" # FULL=1 also runs the region latency, VideoToolbox and iOS probe sections
echo "== region latency"
PROXY="${HTTPS_PROXY:-${https_proxy:-${ALL_PROXY:-${all_proxy:-}}}}"
if [ -z "$PROXY" ] && command -v scutil >/dev/null; then
  SP="$(scutil --proxy 2>/dev/null)"
  if echo "$SP" | grep -q 'HTTPSEnable : 1'; then
    PROXY="http://$(echo "$SP" | awk '/HTTPSProxy :/{h=$3} /HTTPSPort :/{p=$3} END{print h":"p}')"
  elif echo "$SP" | grep -q 'SOCKSEnable : 1'; then
    PROXY="socks5h://$(echo "$SP" | awk '/SOCKSProxy :/{h=$3} /SOCKSPort :/{p=$3} END{print h":"p}')"
  fi
fi
echo "proxy detected: ${PROXY:-none}"
stats() { # reads ms values on stdin: n ok p50 p95
  sort -n | awk '{a[NR]=$1} END{ if(NR==0){print "0 samples"; exit} p50=a[int((NR-1)*0.5)+1]; p95=a[int((NR-1)*0.95)+1]; printf "p50 %.0f ms  p95 %.0f ms  (%d samples)", p50, p95, NR }'
}
probe() { # name host mode(direct|proxy)
  local name="$1" host="$2" mode="$3" tcp="" tls="" ok=0 i out
  for i in $(seq 1 20); do
    if [ "$mode" = direct ]; then
      out="$(curl -s -o /dev/null --noproxy '*' --connect-timeout 5 -m 8 -w '%{time_connect} %{time_appconnect}' "https://$host/" 2>/dev/null)"
    else
      out="$(curl -s -o /dev/null -x "$PROXY" --connect-timeout 5 -m 8 -w '%{time_connect} %{time_appconnect}' "https://$host/" 2>/dev/null)"
    fi
    set -- $out
    if [ -n "${2:-}" ] && [ "${2}" != "0.000000" ]; then
      ok=$((ok+1)); tcp="$tcp$(echo "$1" | awk '{printf "%.1f\n",$1*1000}')\n"; tls="$tls$(echo "$2" | awk '{printf "%.1f\n",$1*1000}')\n"
    fi
    sleep 0.3
  done
  printf '%-16s %-7s reached %2d/20  tcp: %s | tls: %s\n' "$name" "$mode" "$ok" "$(printf "$tcp" | stats)" "$(printf "$tls" | stats)"
}
REGIONS="quebec-relay:connect.mewla.ai
hongkong-aws:ec2.ap-east-1.amazonaws.com
tokyo-aws:ec2.ap-northeast-1.amazonaws.com
tokyo-vultr:hnd-jp-ping.vultr.com
tokyo-linode:speedtest.tokyo2.linode.com
singapore-aws:ec2.ap-southeast-1.amazonaws.com
singapore-vultr:sgp-ping.vultr.com
singapore-linode:speedtest.singapore.linode.com
singapore-ovh:sgp.proof.ovh.net"
[ -n "$FULL" ] && {
  for entry in $REGIONS; do
    probe "${entry%%:*}" "${entry#*:}" direct
    [ -n "$PROXY" ] && probe "${entry%%:*}" "${entry#*:}" proxy
  done
} 2>&1 | tee "$OUT/region.txt"

# ---- 1. source and Go toolchain (scratch only) -----------------------------
[ -d src ] || git clone -q https://github.com/daoleno/mewla.git src
(cd src && git fetch -q origin && git checkout -q "$COMMIT" 2>/dev/null || git checkout -q "origin/$COMMIT")
echo "source: $(cd src && git rev-parse --short HEAD)"
export GOPATH="$W/gopath" GOCACHE="$W/gocache" CGO_ENABLED=0
export GOPROXY="${GOPROXY:-https://goproxy.cn,https://proxy.golang.org,direct}"
[ -n "${PROXY:-}" ] && export HTTPS_PROXY="$PROXY" HTTP_PROXY="$PROXY" https_proxy="$PROXY" http_proxy="$PROXY"
GO="$(command -v go || true)"
if [ -z "$GO" ]; then
  FILE="$(curl -fsSL 'https://go.dev/dl/?mode=json' 2>/dev/null | grep -o 'go1\.2[0-9.]*\.darwin-arm64\.tar\.gz' | head -1)"
  [ -n "$FILE" ] || FILE="$(curl -fsSL 'https://golang.google.cn/dl/?mode=json' | grep -o 'go1\.2[0-9.]*\.darwin-arm64\.tar\.gz' | head -1)"
  curl -fsSL "https://go.dev/dl/$FILE" -o go.tgz || curl -fsSL "https://golang.google.cn/dl/$FILE" -o go.tgz
  mkdir -p gotool && tar -xzf go.tgz -C gotool && GO="$W/gotool/go/bin/go"
fi
export GOTOOLCHAIN=auto
echo "go: $($GO version)"

# ---- 2. the hardware encoder, on synthetic frames --------------------------
cd "$W/src/daemon"
echo "== VideoToolbox encoder"
[ -n "$FULL" ] && MEWLA_SCREEN_VT_BENCH=1 $GO test ./screen -run 'TestVideoToolboxEncodesScreenFrames|TestAVCC' -count=1 -v 2>&1 | tail -15 | tee "$OUT/vt.txt"

# ---- 3. real capture: tiles against video over simulated links ------------
# Opens Safari tabs with a long page that scrolls itself (requestAnimationFrame; no input events) for
# about two minutes; nobody should be using this Mac meanwhile. The tabs
# (titled mewla-scroll-fixture ...) are left open: close them afterwards. No AppleScript
# is used, so no Automation permission is needed.
echo "== real screen, simulated links"
export MEWLA_SCREEN_VIDEO=on MEWLA_SCREEN_BENCH_OUT="$OUT"
MEWLA_SCREEN_REAL_BENCH=1 $GO test ./screen -run 'TestRealScreenStillAndSharpness' -count=1 -v -timeout 10m 2>&1 | tail -20 | tee "$OUT/still.txt"
MEWLA_SCREEN_REAL_BENCH=1 $GO test ./screen -run TestRealScreenVideoOverSimulatedLinks -count=1 -v -timeout 15m 2>&1 | tail -20 | tee "$OUT/real.txt"

# ---- 4. iOS WKWebView: secure context, VideoDecoder, one real key frame ----
echo "== iOS WKWebView probe"
cd "$W"
rm -rf probe && mkdir -p probe/Probe.app && cd probe
cat > ios-probe.html <<'HTMLEOF'
<!doctype html><meta charset=utf-8><body><script>
const post = (o) => window.webkit.messageHandlers.r.postMessage(JSON.stringify(o));
(async () => {
  const o = { secure: window.isSecureContext, origin: String(location.origin), ua: navigator.userAgent, vd: typeof VideoDecoder };
  if (o.vd === 'function') {
    for (const c of ['avc1.42401f', 'avc1.4d401f', 'avc1.640028']) {
      try { o[c] = (await VideoDecoder.isConfigSupported({ codec: c, codedWidth: 1512, codedHeight: 982, optimizeForLatency: true })).supported; } catch (e) { o[c] = 'err ' + e; }
    }
    // noVNC's own check: decode one real key frame.
    try {
      const bin = atob('AAAAAWdCwBTZnpuAgICgAAADACAAAAZB4oVNAAAAAWjJYyyAAAABBgX//4HcRem95tlIt5Ys2CDZI+7veDI2NCAtIGNvcmUgMTY0IHIzMTA4IDMxZTE5ZjkgLSBILjI2NC9NUEVHLTQgQVZDIGNvZGVjIC0gQ29weWxlZnQgMjAwMy0yMDIzIC0gaHR0cDovL3d3dy52aWRlb2xhbi5vcmcveDI2NC5odG1sIC0gb3B0aW9uczogY2FiYWM9MCByZWY9NSBkZWJsb2NrPTE6MDowIGFuYWx5c2U9MHgxOjB4MTExIG1lPWhleCBzdWJtZT04IHBzeT0xIHBzeV9yZD0xLjAwOjAuMDAgbWl4ZWRfcmVmPTEgbWVfcmFuZ2U9MTYgY2hyb21hX21lPTEgdHJlbGxpcz0yIDh4OGRjdD0wIGNxbT0wIGRlYWR6b25lPTIxLDExIGZhc3RfcHNraXA9MSBjaHJvbWFfcXBfb2Zmc2V0PS0yIHRocmVhZHM9MSBsb29rYWhlYWRfdGhyZWFkcz0xIHNsaWNlZF90aHJlYWRzPTAgbnI9MCBkZWNpbWF0ZT0xIGludGVybGFjZWQ9MCBibHVyYXlfY29tcGF0PTAgY29uc3RyYWluZWRfaW50cmE9MCBiZnJhbWVzPTAgd2VpZ2h0cD0wIGtleWludD1pbmZpbml0ZSBrZXlpbnRfbWluPTI1IHNjZW5lY3V0PTQwIGludHJhX3JlZnJlc2g9MCByY19sb29rYWhlYWQ9NTAgcmM9YWJyIG1idHJlZT0xIGJpdHJhdGU9NDAwIHJhdGV0b2w9MS4wIHFjb21wPTAuNjAgcXBtaW49MCBxcG1heD02OSBxcHN0ZXA9NCBpcF9yYXRpbz0xLjQwIGFxPTE6MS4wMACAAAABZYiEBrxmKAAPVccAAS044AA5DRJMnkycJk4TPw=='); const data = new Uint8Array(bin.length); for (let i = 0; i < bin.length; i++) data[i] = bin.charCodeAt(i);
      let got = false, err = null;
      const d = new VideoDecoder({ output: (f) => { got = true; f.close(); }, error: (e) => { err = String(e); } });
      d.configure({ codec: 'avc1.42401f', codedWidth: 1920, codedHeight: 1080, optimizeForLatency: true });
      d.decode(new EncodedVideoChunk({ timestamp: 0, type: 'key', data }));
      await d.flush();
      o.decodedKeyFrame = got && !err; if (err) o.decodeError = err;
    } catch (e) { o.decodedKeyFrame = false; o.decodeError = String(e); }
  }
  post(o);
})();
</script>
HTMLEOF
cat > ios-probe.swift <<'SWIFTEOF'
import UIKit
import WebKit

// Loads ios-probe.html the way the Screen viewer is loaded (HTML string with an
// https base URL) and prints what WebKit reports.
class Probe: UIViewController, WKScriptMessageHandler {
  var web: WKWebView!
  override func viewDidLoad() {
    super.viewDidLoad()
    let config = WKWebViewConfiguration()
    config.userContentController.add(self, name: "r")
    web = WKWebView(frame: view.bounds, configuration: config)
    view.addSubview(web)
    let html = try! String(contentsOfFile: Bundle.main.path(forResource: "ios-probe", ofType: "html")!)
    web.loadHTMLString(html, baseURL: URL(string: ProcessInfo.processInfo.environment["PROBE_BASE"] ?? "https://viewer.mewla.invalid/"))
    DispatchQueue.main.asyncAfter(deadline: .now() + 20) { print("PROBE timeout"); fflush(stdout); exit(1) }
  }
  func userContentController(_ c: WKUserContentController, didReceive m: WKScriptMessage) {
    print("PROBE \(UIDevice.current.systemVersion) \(m.body)"); fflush(stdout); exit(0)
  }
}
@main class App: UIResponder, UIApplicationDelegate {
  var window: UIWindow?
  func application(_ a: UIApplication, didFinishLaunchingWithOptions o: [UIApplication.LaunchOptionsKey: Any]?) -> Bool {
    window = UIWindow(frame: UIScreen.main.bounds); window?.rootViewController = Probe(); window?.makeKeyAndVisible(); return true
  }
}
SWIFTEOF
SDK=""; [ -n "$FULL" ] && SDK="$(xcrun --sdk iphonesimulator --show-sdk-path 2>/dev/null)"
if [ -z "$SDK" ] || [ ! -f ios-probe.swift ]; then
  echo "no iOS simulator SDK or probe files: skipped"
else
  RUNTIMES="$(xcrun simctl list runtimes available 2>/dev/null | grep -i ios)"
  echo "runtimes: $RUNTIMES"
  xcrun --sdk iphonesimulator swiftc -parse-as-library -target arm64-apple-ios16.4-simulator ios-probe.swift -o Probe.app/Probe 2>&1 | tail -5
  cp ios-probe.html Probe.app/
  cat > Probe.app/Info.plist <<'PL'
<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>ai.mewla.probe</string><key>CFBundleExecutable</key><string>Probe</string><key>CFBundleName</key><string>Probe</string>
<key>CFBundlePackageType</key><string>APPL</string><key>CFBundleShortVersionString</key><string>1</string><key>CFBundleVersion</key><string>1</string>
<key>MinimumOSVersion</key><string>16.4</string><key>UILaunchScreen</key><dict/></dict></plist>
PL
  codesign -s - --force Probe.app 2>&1 | tail -1
  # Every available iPhone runtime's newest device, one at a time.
  for RT in $(xcrun simctl list runtimes available -j | grep -o '"identifier" : "com.apple.CoreSimulator.SimRuntime.iOS[^"]*"' | cut -d'"' -f4); do
    DEV="$(xcrun simctl list devices available -j | python3 -c "import json,sys;d=json.load(sys.stdin)['devices'].get('$RT',[]);print(next((x['udid'] for x in d if 'iPhone' in x['name']),''))" 2>/dev/null)"
    [ -n "$DEV" ] || continue
    WAS="$(xcrun simctl list devices | grep "$DEV" | grep -c Booted)"
    xcrun simctl boot "$DEV" 2>/dev/null; xcrun simctl bootstatus "$DEV" -b >/dev/null 2>&1
    xcrun simctl install "$DEV" Probe.app && echo "-- $RT" && \
      (xcrun simctl launch --console-pty "$DEV" ai.mewla.probe 2>&1 | grep PROBE | head -2)
    xcrun simctl terminate "$DEV" ai.mewla.probe >/dev/null 2>&1
    [ "$WAS" = 0 ] && xcrun simctl shutdown "$DEV" 2>/dev/null
  done 2>&1 | tee "$OUT/ios.txt"
fi

# ---- summary ----------------------------------------------------------------
{ echo "# Screen video, Mac run $(date)"; echo "## region latency"; cat "$OUT/region.txt"; echo "## VideoToolbox"; cat "$OUT/vt.txt"; echo "## still frame + sharpness"; cat "$OUT/still.txt"; echo "## real screen"; cat "$OUT/real.txt"; echo "## iOS WKWebView"; cat "$OUT/ios.txt" 2>/dev/null; } > "$OUT/summary.txt"
echo "done: $OUT/summary.txt"
