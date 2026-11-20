#!/usr/bin/env bash
# Mewla for Mac: <out>/<Name>.app and <Name>-<version>-macos-arm64.zip, run on a Mac (Xcode, Rust >= 1.97, NSH).
#
#   client/macos/package.sh --daemon <mewla darwin-arm64 binary> [--dev] [--out <dir>]
#
# The bundle: Contents/MacOS/<Name> (this host, client/macos), Contents/Helpers/mewla (the daemon and CLI: one
# file, its own directory so the daemon can put it first on PATH; APFS is case-insensitive, so it cannot sit
# beside MacOS/Mewla), Contents/Resources/nsh (client/ui, built by `nsh build`), the icon and Info.plist.
# --dev makes Mewla Dev (com.daoleno.mewla.dev), which installs beside Mewla. Signed ad hoc, inside out; Developer
# ID and notarization come with the release job (FACTS.md §4).
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
daemon=""
out="$root/client/macos/dist"
dev=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --daemon) daemon="${2:?}"; shift 2 ;;
    --out) out="${2:?}"; shift 2 ;;
    --dev) dev=1; shift ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done
[[ -x "$daemon" ]] || { echo "--daemon: no executable at '$daemon'" >&2; exit 2; }
file "$daemon" | grep -q 'Mach-O 64-bit.*arm64' || { echo "--daemon is not a macOS arm64 binary" >&2; exit 2; }

if [[ $dev == 1 ]]; then name="Mewla Dev"; id="com.daoleno.mewla.dev"; else name="Mewla"; id="com.daoleno.mewla"; fi
version=$(python3 -I -c 'import json,sys; print(json.load(open(sys.argv[1]))["version"])' "$root/client/ui/nsh.json")
nsh="${NSH:-$HOME/.nsh/bin/nsh}"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# The app: client/ui as every host bundles it, at the Mac's type sizes (density.json in place of nsh.json's tailwind
# fontSize): built from a copy of client/core and client/ui, so the checkout is never changed. The source map stays
# out of the app.
mkdir -p "$work/src"
rsync -a --exclude .nsh --exclude node_modules "$root/client/core" "$root/client/ui" "$work/src/"
python3 -I - "$work/src/ui/nsh.json" "$root/client/macos/density.json" <<'PY'
import json, sys
path, density = sys.argv[1:3]
config = json.load(open(path))
config.setdefault("tailwind", {})["fontSize"] = json.load(open(density))["fontSize"]
json.dump(config, open(path, "w"), indent=2)
PY
"$nsh" build --release -o "$work/nsh" "$work/src/ui"
rm -f "$work/nsh/bundle.js.map"
python3 -I - "$work/nsh/app.json" "$id" "$name" <<'PY'
import json, sys
path, app_id, name = sys.argv[1:4]
try:
    info = json.load(open(path))
except FileNotFoundError:
    info = {}
info.update({"id": app_id, "name": name})
json.dump(info, open(path, "w"))
PY

# The host.
cargo build --release --manifest-path "$root/client/macos/Cargo.toml"
target="${CARGO_TARGET_DIR:-$root/client/macos/target}"
host="$target/release/Mewla"

app="$out/$name.app"
rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Helpers" "$app/Contents/Resources"
cp "$host" "$app/Contents/MacOS/$name"
cp "$daemon" "$app/Contents/Helpers/mewla"
chmod 755 "$app/Contents/MacOS/$name" "$app/Contents/Helpers/mewla"
cp -R "$work/nsh" "$app/Contents/Resources/nsh"

# The daemon's LaunchAgent, which the app registers with SMAppService (src/agent.rs). Mewla Dev runs its daemon as
# its child on its own state and carries none.
if [[ $dev == 0 ]]; then
  mkdir -p "$app/Contents/Library/LaunchAgents"
  cat > "$app/Contents/Library/LaunchAgents/com.daoleno.mewla.daemon.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.daoleno.mewla.daemon</string>
  <key>BundleProgram</key><string>Contents/Helpers/mewla</string>
  <key>ProgramArguments</key><array><string>mewla</string><string>serve</string></array>
  <key>EnvironmentVariables</key><dict><key>MEWLA_NO_AUTOUPDATE</key><string>1</string></dict>
  <key>AssociatedBundleIdentifiers</key><array><string>com.daoleno.mewla</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>5</integer>
  <key>LimitLoadToSessionType</key><string>Aqua</string>
  <key>ProcessType</key><string>Interactive</string>
  <key>AbandonProcessGroup</key><true/>
</dict>
</plist>
PLIST
  plutil -lint "$app/Contents/Library/LaunchAgents/com.daoleno.mewla.daemon.plist" >/dev/null
fi

iconset="$work/AppIcon.iconset"
mkdir -p "$iconset"
for s in 16 32 128 256 512; do
  sips -z $s $s "$root/client/ios/Assets.xcassets/AppIcon.appiconset/icon.png" --out "$iconset/icon_${s}x${s}.png" >/dev/null
  d=$((s * 2))
  sips -z $d $d "$root/client/ios/Assets.xcassets/AppIcon.appiconset/icon.png" --out "$iconset/icon_${s}x${s}@2x.png" >/dev/null
done
iconutil -c icns "$iconset" -o "$app/Contents/Resources/AppIcon.icns"

cat > "$app/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleDevelopmentRegion</key><string>en</string>
  <key>CFBundleName</key><string>$name</string>
  <key>CFBundleDisplayName</key><string>$name</string>
  <key>CFBundleIdentifier</key><string>$id</string>
  <key>CFBundleExecutable</key><string>$name</string>
  <key>CFBundleIconFile</key><string>AppIcon</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>$version</string>
  <key>CFBundleVersion</key><string>$version</string>
  <key>CFBundleInfoDictionaryVersion</key><string>6.0</string>
  <key>CFBundleSupportedPlatforms</key><array><string>MacOSX</string></array>
  <key>LSMinimumSystemVersion</key><string>14.0</string>
  <key>LSApplicationCategoryType</key><string>public.app-category.productivity</string>
  <key>NSHighResolutionCapable</key><true/>
  <key>NSPrincipalClass</key><string>NSApplication</string>
</dict>
</plist>
PLIST
plutil -lint "$app/Contents/Info.plist" >/dev/null

# Inside out: the daemon first, under its own identifier, then the app.
codesign --force --sign - --identifier "$id.daemon" "$app/Contents/Helpers/mewla"
codesign --force --sign - "$app"
codesign --verify --deep --strict "$app"

zip="$out/${name// /-}-$version-macos-arm64.zip"
rm -f "$zip"
ditto -c -k --keepParent "$app" "$zip"
echo "$app"
echo "$zip"
