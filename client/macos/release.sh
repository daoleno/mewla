#!/usr/bin/env bash
# Mewla for Mac's release: the app package.sh made, signed with Developer ID (hardened runtime, secure timestamps,
# inside out), notarized and stapled, then Mewla-<version>-macos-arm64.dmg (the app and a link to /Applications),
# signed, notarized and stapled too. On a Mac (Xcode 26+), with the identity in a keychain on the search list:
#
#   client/macos/release.sh --app <Mewla.app> --identity "Developer ID Application: … (TEAMID)" \
#     --key <AuthKey.p8> --key-id <id> [--issuer <uuid>] [--out <dir>]
#
# The App Store Connect key is the release's (an Individual key needs no --issuer). Nothing here creates anything
# in the Apple account. Without --key the app and .dmg are signed but not notarized (a dry run of the signing);
# --identity - signs ad hoc, which CI uses to prove the .dmg before the Developer ID certificate exists.
set -euo pipefail

app="" identity="" key="" key_id="" issuer="" out=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --app) app="${2:?}"; shift 2 ;;
    --identity) identity="${2:?}"; shift 2 ;;
    --key) key="${2:?}"; shift 2 ;;
    --key-id) key_id="${2:?}"; shift 2 ;;
    --issuer) issuer="${2:?}"; shift 2 ;;
    --out) out="${2:?}"; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done
[[ -d "$app/Contents/MacOS" ]] || { echo "--app: no app bundle at '$app'" >&2; exit 2; }
[[ -n "$identity" ]] || { echo "--identity is required" >&2; exit 2; }
out="${out:-$(dirname "$app")}"
here="$(cd "$(dirname "$0")" && pwd)"
entitlements="$here/entitlements.plist"
version=$(/usr/libexec/PlistBuddy -c "Print :CFBundleShortVersionString" "$app/Contents/Info.plist")
name=$(/usr/libexec/PlistBuddy -c "Print :CFBundleName" "$app/Contents/Info.plist")
id=$(/usr/libexec/PlistBuddy -c "Print :CFBundleIdentifier" "$app/Contents/Info.plist")
host="$app/Contents/MacOS/$(/usr/libexec/PlistBuddy -c "Print :CFBundleExecutable" "$app/Contents/Info.plist")"

if [[ "$identity" == "-" ]]; then stamp=(); else stamp=(--timestamp); fi
sign() {
  codesign --force ${stamp[@]+"${stamp[@]}"} --options runtime --entitlements "$entitlements" --sign "$identity" "$@"
}

# Inside out: the daemon and CLI (its own identifier), then the app.
sign --identifier "$id.daemon" "$app/Contents/Helpers/mewla"
sign "$app"
codesign --verify --deep --strict --verbose=2 "$app"
signature=$(codesign -dv --verbose=2 "$app" 2>&1 || true)
grep -E "Authority=Developer ID|TeamIdentifier|flags=.*runtime" <<<"$signature" || true
if [[ "$identity" != "-" ]] && ! grep -q "Authority=Developer ID Application" <<<"$signature"; then
  echo "the app is not signed with Developer ID" >&2
  exit 1
fi

notarize() {
  [[ -n "$key" ]] || { echo "no --key: $1 is signed, not notarized"; return 0; }
  local auth=(--key "$key" --key-id "$key_id")
  [[ -n "$issuer" ]] && auth+=(--issuer "$issuer")
  local result
  result=$(xcrun notarytool submit "$1" "${auth[@]}" --wait --output-format json)
  echo "$result"
  local status submission
  status=$(python3 -I -c 'import json,sys; print(json.loads(sys.argv[1]).get("status",""))' "$result")
  submission=$(python3 -I -c 'import json,sys; print(json.loads(sys.argv[1]).get("id",""))' "$result")
  if [[ "$status" != "Accepted" ]]; then
    xcrun notarytool log "$submission" "${auth[@]}" || true
    echo "notarization of $1: $status" >&2
    return 1
  fi
}

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# The app: notarized as a zip, the ticket stapled to the bundle.
ditto -c -k --keepParent "$app" "$work/app.zip"
notarize "$work/app.zip"
[[ -z "$key" ]] || xcrun stapler staple "$app"

# The disk image: the app and /Applications, compressed, signed, notarized, stapled.
stage="$work/dmg"
mkdir -p "$stage"
ditto "$app" "$stage/$(basename "$app")"
ln -s /Applications "$stage/Applications"
dmg="$out/${name// /-}-$version-macos-arm64.dmg"
rm -f "$dmg"
hdiutil create -volname "$name" -srcfolder "$stage" -fs HFS+ -format UDZO -ov "$dmg"
codesign --force ${stamp[@]+"${stamp[@]}"} --sign "$identity" "$dmg"
notarize "$dmg"
[[ -z "$key" ]] || xcrun stapler staple "$dmg"
[[ -z "$key" ]] || spctl --assess --type open --context context:primary-signature --verbose=2 "$dmg"
[[ -z "$key" ]] || spctl --assess --type execute --verbose=2 "$app"
shasum -a 256 "$dmg"
echo "$dmg"
