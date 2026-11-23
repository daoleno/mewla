#!/usr/bin/env bash
# Mewla for Mac: <out>/<Name>.app and <Name>-<version>-macos-aarch64.zip, run on a Mac (Xcode, Rust >= 1.97, NSH).
#
#   client/macos/package.sh --daemon <mewla darwin-arm64 binary> [--dev] [--out <dir>]
#
# The bundle: Contents/MacOS/<Name> (this host, client/macos), Contents/Helpers/mewla (the daemon and CLI: one
# file, its own directory so the daemon can put it first on PATH; APFS is case-insensitive, so it cannot sit
# beside MacOS/Mewla), Contents/Resources/nsh (client/ui, built by `nsh build`), the icon and Info.plist.
# --dev makes Mewla Dev (com.daoleno.mewla.dev), which installs beside Mewla. Signed ad hoc, inside out; Developer
# ID and notarization come with the release job (FACTS.md §4).
#
# The app's own updates (NSH's updater): the release bundle names its source in nsh/assets/nsh-updater.json, the
# manifest `latest.json` of the GitHub release (daoleno/mewla), signed with the key client/macos/update-public-key.txt
# holds the public half of. Mewla Dev has none. A proof can give either its own: --updater-url <https or loopback
# latest.json URL> --updater-key <base64 public key>, and --version to be a version other than nsh.json's.
# The archive is named for the updater's platform (macos-aarch64): `nsh update-manifest` reads that from its name.
# --fixture (with --dev only): Mewla Dev on the soft-final mock's scenario (client/ui/fixture/MacFixtureApp.tsx), a busy
# desk for the fidelity captures (macos-fidelity.yml); never a release.
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
daemon=""
out="$root/client/macos/dist"
dev=0
version_override=""
updater_url=""
updater_key=""
fixture=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --daemon) daemon="${2:?}"; shift 2 ;;
    --out) out="${2:?}"; shift 2 ;;
    --dev) dev=1; shift ;;
    --version) version_override="${2:?}"; shift 2 ;;
    --updater-url) updater_url="${2:?}"; shift 2 ;;
    --updater-key) updater_key="${2:?}"; shift 2 ;;
    --fixture) fixture=1; shift ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done
[[ -x "$daemon" ]] || { echo "--daemon: no executable at '$daemon'" >&2; exit 2; }
file "$daemon" | grep -q 'Mach-O 64-bit.*arm64' || { echo "--daemon is not a macOS arm64 binary" >&2; exit 2; }

[[ $fixture == 0 || $dev == 1 ]] || { echo "--fixture goes with --dev" >&2; exit 2; }
if [[ $dev == 1 ]]; then name="Mewla Dev"; id="com.daoleno.mewla.dev"; else name="Mewla"; id="com.daoleno.mewla"; fi
version=$(python3 -I -c 'import json,sys; print(json.load(open(sys.argv[1]))["version"])' "$root/client/ui/nsh.json")
[[ -z "$version_override" ]] || version="$version_override"
[[ -z "$updater_url" && -z "$updater_key" ]] || [[ -n "$updater_url" && -n "$updater_key" ]] || { echo "--updater-url and --updater-key go together" >&2; exit 2; }
nsh="${NSH:-$HOME/.nsh/bin/nsh}"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# The app: client/ui as every host bundles it, at the Mac's type sizes (density.json in place of nsh.json's tailwind
# fontSize): built from a copy of client/core and client/ui, so the checkout is never changed. The source map stays
# out of the app.
mkdir -p "$work/src"
rsync -a --exclude .nsh --exclude node_modules "$root/client/core" "$root/client/ui" "$work/src/"
python3 -I - "$work/src/ui/nsh.json" "$root/client/macos/density.json" "$version_override" <<'PY'
import json, sys
path, density, version = sys.argv[1:4]
config = json.load(open(path))
if version:
    config["version"] = version
config.setdefault("tailwind", {})["fontSize"] = json.load(open(density))["fontSize"]
json.dump(config, open(path, "w"), indent=2)
PY
entry="$work/src/ui"
if [[ $fixture == 1 ]]; then
  # Beside nsh.json, which `nsh build` reads next to the entry.
  printf 'import "./fixture/MacFixtureApp";\n' > "$work/src/ui/MacFixture.tsx"
  entry="$work/src/ui/MacFixture.tsx"
fi
"$nsh" build --release -o "$work/nsh" "$entry"
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

# The update source (nsh-updater.json, as `nsh package` writes it): the release's, a proof's, or none (Mewla Dev).
python3 -I - "$work/nsh/assets/nsh-updater.json" "$id" "$version" "$updater_url" "$updater_key" "$dev" "$root/client/macos/update-public-key.txt" <<'PY'
import json, os, sys
path, app_id, version, url, key, dev, pub = sys.argv[1:8]
config = {"id": app_id, "version": version}
if url:
    config.update({"url": url, "publicKey": key})
elif dev != "1":
    config.update({"github": "daoleno/mewla", "publicKey": open(pub).read().strip()})
else:
    sys.exit(0)
os.makedirs(os.path.dirname(path), exist_ok=True)
json.dump(config, open(path, "w"))
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

# The daemon's LaunchAgent, which the app registers with SMAppService (src/agent.rs). Its program is the app's own
# executable, which runs the daemon as its child (`Mewla --daemon serve`, src/host.rs), so that macOS charges both of
# the daemon's permissions to Mewla.app rather than Accessibility to the bare helper. Mewla Dev runs its daemon as its
# child on its own state and carries none.
if [[ $dev == 0 ]]; then
  mkdir -p "$app/Contents/Library/LaunchAgents"
  cat > "$app/Contents/Library/LaunchAgents/com.daoleno.mewla.daemon.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.daoleno.mewla.daemon</string>
  <key>BundleProgram</key><string>Contents/MacOS/Mewla</string>
  <key>ProgramArguments</key><array><string>Mewla</string><string>--daemon</string><string>serve</string></array>
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
  # The CLI wrapper and the GUI historically shared a bundle identifier. For adoption in
  # /Applications, launchd must execute the installed signed helper without resolving that identifier.
  installed_plist="$app/Contents/Library/LaunchAgents/com.daoleno.mewla.installed-daemon.plist"
  cp "$app/Contents/Library/LaunchAgents/com.daoleno.mewla.daemon.plist" "$installed_plist"
  plutil -replace Label -string com.daoleno.mewla.installed-daemon "$installed_plist"
  plutil -remove BundleProgram "$installed_plist"
  plutil -insert Program -string /Applications/Mewla.app/Contents/MacOS/Mewla "$installed_plist"
  # Build the complete argv in one operation. Replacing element zero on this Mac
  # left the original "mewla" word before "serve".
  plutil -replace ProgramArguments -json '["/Applications/Mewla.app/Contents/MacOS/Mewla","--daemon","serve"]' "$installed_plist"
  python3 - "$installed_plist" <<'CHECK_ARGV'
import plistlib
import sys

with open(sys.argv[1], "rb") as plist:
    job = plistlib.load(plist)
assert job["ProgramArguments"] == [job["Program"], "--daemon", "serve"], "incorrect installed helper argv"
CHECK_ARGV
  plutil -lint "$installed_plist" >/dev/null
fi

iconset="$work/AppIcon.iconset"
mkdir -p "$iconset"
for s in 16 32 128 256 512; do
  sips -z $s $s "$root/client/ios/Assets.xcassets/AppIcon.appiconset/icon.png" --out "$iconset/icon_${s}x${s}.png" >/dev/null
  d=$((s * 2))
  sips -z $d $d "$root/client/ios/Assets.xcassets/AppIcon.appiconset/icon.png" --out "$iconset/icon_${s}x${s}@2x.png" >/dev/null
done
iconutil -c icns "$iconset" -o "$app/Contents/Resources/AppIcon.icns"

# `mewla://` links (pairing links, notifications' links) open the release; Mewla Dev claims none, so it never takes
# them from the installed app. NSH hands them to the app's UrlScheme (client/ui/nsh.json `schemes`).
url_types=""
if [[ $dev != 1 ]]; then
  url_types="<key>CFBundleURLTypes</key><array><dict><key>CFBundleURLName</key><string>$id</string><key>CFBundleURLSchemes</key><array><string>mewla</string></array></dict></array>"
fi
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
  $url_types
</dict>
</plist>
PLIST
plutil -lint "$app/Contents/Info.plist" >/dev/null

# Inside out: the daemon first, under its own identifier, then the app.
codesign --force --sign - --identifier "$id.daemon" "$app/Contents/Helpers/mewla"
codesign --force --sign - "$app"
codesign --verify --deep --strict "$app"

zip="$out/${name// /-}-$version-macos-aarch64.zip"
rm -f "$zip"
ditto -c -k --keepParent "$app" "$zip"
echo "$app"
echo "$zip"
