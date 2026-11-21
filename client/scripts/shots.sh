#!/usr/bin/env bash
# Lane 1, a look at the app without a device: the app drawn in headless Chrome by `nsh test --target web`,
# no emulator, no device, no model. One command, a few seconds (3 s a picture, 14 s for a three-scene tour): the steps you give it run on the app's fixture
# (client/ui/fixture: a scripted computer, the soft-final mock's scenario), and every `shot(name)` writes
# <name>.png as ONE image: phone 390x844 light | phone dark | desktop 1280x800 (a reviewer reads one picture).
#
#   client/scripts/shots.sh                          the default tour (scripts/shots/tour.js)
#   client/scripts/shots.sh scripts/shots/x.js       your steps: a file of `await app...` lines (stdin steps of `nsh test`)
#   client/scripts/shots.sh -o DIR -e 'await app.press("id:home-cat"); await shot("computer")'
#   --entry FILE   another app entry (default: client/ui/fixture/FixtureApp.tsx)
#
# In the steps: `app` is NSH's test app (waitFor, press, wait, texts, shows, setViewport, setColorScheme, ...)
# `shot(name)` and `snap(name)` (the current viewport only, faster). Output goes to DIR (default: a fresh directory under $TMPDIR), printed at the end.
# Differs from a phone: no status bar, navigation bar or keyboard, the browser's fonts, platform.os is "web".
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
client="$(cd "$here/.." && pwd)"
nsh="${NSH:-${NSH_HOME:-$HOME/.nsh}/bin/nsh}"
entry="$client/ui/fixture/FixtureApp.tsx"
out="" steps="" inline=""
while (($#)); do
  case "$1" in
    -o) out="$2"; shift 2 ;;
    -e) inline="$2"; shift 2 ;;
    --entry) entry="$2"; shift 2 ;;
    -h|--help) sed -n 2,16p "$0"; exit 0 ;;
    *) steps="$1"; shift ;;
  esac
done
[[ -n "$out" ]] || out="$(mktemp -d "${TMPDIR:-/tmp}/shots.XXXXXX")"
mkdir -p "$out"; out="$(cd "$out" && pwd)"
if [[ -n "$inline" ]]; then body="$inline"; else body="$(cat "${steps:-$here/shots/tour.js}")"; fi
# `nsh test --target web` needs a Chrome, Edge or Chromium; without --browser it can wait for ever.
browser="${BROWSER:-}"
for candidate in google-chrome-stable google-chrome chromium chromium-browser microsoft-edge; do
  [[ -n "$browser" ]] || browser="$(command -v "$candidate" || true)"
done
[[ -n "$browser" ]] || { echo "shots.sh: no Chrome, Edge or Chromium (BROWSER=/path)" >&2; exit 1; }
read -r -d '' preamble <<'JS' || true
const shot = async (name) => {
  const set = async (w, h, scheme) => { await app.setViewport(w, h); await app.setColorScheme(scheme); await app.wait(300); };
  await set(390, 844, "light"); await app.screenshot(name + ".phone.png");
  await set(390, 844, "dark"); await app.screenshot(name + ".dark.png");
  await set(1280, 800, "light"); await app.screenshot(name + ".desktop.png");
  await set(390, 844, "light");
};
const snap = async (name) => { await app.screenshot(name + ".png"); }; // the current viewport only, 1 s
JS
cd "$out"
printf '%s\n%s\n' "$preamble" "$body" | "$nsh" test --target web --browser "$browser" "$entry" 2>&1 | grep -v -E '^\[[0-9]+:[0-9]+:' || true
status=${PIPESTATUS[1]}
python3 -I - "$out" <<'PY'
import sys, pathlib
try:
    from PIL import Image
except ImportError:
    sys.exit(0)
out = pathlib.Path(sys.argv[1])
for phone in sorted(out.glob("*.phone.png")):
    name = phone.name[: -len(".phone.png")]
    parts = [out / f"{name}.{k}.png" for k in ("phone", "dark", "desktop")]
    if not all(p.exists() for p in parts):
        continue
    ims = [Image.open(p).convert("RGB") for p in parts]
    canvas = Image.new("RGB", (sum(i.width for i in ims) + 20 * 2, max(i.height for i in ims)), (60, 60, 60))
    x = 0
    for im in ims:
        canvas.paste(im, (x, 0)); x += im.width + 20
    canvas.save(out / f"{name}.png")
    for p in parts:
        p.unlink()
PY
echo "shots: $out"; ls "$out"
exit "$status"
