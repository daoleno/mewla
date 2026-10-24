#!/usr/bin/env bash
# Verify the shape and identity of an unsigned Simulator .app or signed IPA.

set -euo pipefail

MODE="${1:-}"
ARTIFACT="${2:-}"
EXPECTED_BUNDLE_ID="${MEWLA_IOS_BUNDLE_ID:-com.daoleno.mewla}"
EXPECTED_DISPLAY_NAME="${MEWLA_IOS_DISPLAY_NAME:-Mewla}"
EXPECTED_BUILD_NUMBER="${MEWLA_IOS_BUILD_NUMBER:-}"
EXPECTED_VERSION="${MEWLA_IOS_VERSION:-}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
NOTICE_SRC="${MEWLA_IOS_NOTICE_SRC:-$ROOT/app/assets/notices/GHOSTTY-MIT.txt}"
NOTICE_BUNDLE_REL="${MEWLA_IOS_NOTICE_BUNDLE_REL:-GHOSTTY-MIT.txt}"

usage() {
  echo "usage: $0 simulator path/to/Mewla.app | ipa path/to/Mewla.ipa" >&2
  exit 2
}

[[ "$MODE" == "simulator" || "$MODE" == "ipa" ]] || usage
[[ -n "$ARTIFACT" ]] || usage

verify_ghostty_notice() {
  local app="$1"
  local notice="$app/$NOTICE_BUNDLE_REL"
  [[ -f "$NOTICE_SRC" ]] || {
    echo "error: Ghostty MIT notice source missing: $NOTICE_SRC" >&2
    exit 1
  }
  [[ -f "$notice" ]] || {
    echo "error: app bundle missing Ghostty MIT notice at $NOTICE_BUNDLE_REL" >&2
    exit 1
  }
  if ! cmp -s "$NOTICE_SRC" "$notice"; then
    echo "error: bundled Ghostty MIT notice differs from $NOTICE_SRC" >&2
    exit 1
  fi
  echo "ok: Ghostty MIT notice present at $NOTICE_BUNDLE_REL"
}

verify_app() {
  local app="$1"
  local require_signature="$2"
  local plist="$app/Info.plist"
  [[ -d "$app" ]] || { echo "error: app bundle is missing: $app" >&2; exit 1; }
  [[ -f "$plist" ]] || { echo "error: Info.plist is missing: $plist" >&2; exit 1; }

  local bundle_id display_name executable build_number version
  bundle_id="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$plist")"
  display_name="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleDisplayName' "$plist")"
  executable="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleExecutable' "$plist")"
  build_number="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleVersion' "$plist")"
  version="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$plist")"
  [[ "$bundle_id" == "$EXPECTED_BUNDLE_ID" ]] || {
    echo "error: bundle identifier is $bundle_id; expected $EXPECTED_BUNDLE_ID" >&2
    exit 1
  }
  [[ "$display_name" == "$EXPECTED_DISPLAY_NAME" ]] || {
    echo "error: display name is $display_name; expected $EXPECTED_DISPLAY_NAME" >&2
    exit 1
  }
  if [[ -n "$EXPECTED_BUILD_NUMBER" && "$build_number" != "$EXPECTED_BUILD_NUMBER" ]]; then
    echo "error: build number is $build_number; expected $EXPECTED_BUILD_NUMBER" >&2
    exit 1
  fi
  if [[ -n "$EXPECTED_VERSION" && "$version" != "$EXPECTED_VERSION" ]]; then
    echo "error: marketing version is $version; expected $EXPECTED_VERSION" >&2
    exit 1
  fi
  [[ -x "$app/$executable" ]] || {
    echo "error: app executable is missing or not executable: $app/$executable" >&2
    exit 1
  }
  file "$app/$executable" | grep -q 'Mach-O' || {
    echo "error: app executable is not Mach-O" >&2
    exit 1
  }

  verify_ghostty_notice "$app"

  if [[ "$require_signature" == "yes" ]]; then
    [[ -f "$app/embedded.mobileprovision" ]] || {
      echo "error: signed IPA has no embedded provisioning profile" >&2
      exit 1
    }
    codesign --verify --deep --strict "$app"
  fi

  echo "ok: $display_name ($bundle_id) app bundle contains a Mach-O executable"

  if [[ "${MEWLA_IOS_LIVE:-}" == "1" ]]; then
    verify_live_extension "$app" "$require_signature" "$bundle_id" "$build_number" "$version"
  fi
}

# The Live Activity / widget extension rides inside the app: same version and build, its own
# bundle id, and (signed) the shared App Group in both entitlements.
verify_live_extension() {
  local app="$1" require_signature="$2" app_id="$3" build_number="$4" version="$5"
  local appex="$app/PlugIns/MewlaLiveWidgets.appex"
  local plist="$appex/Info.plist"
  [[ -d "$appex" ]] || { echo "error: Live Activity extension is missing: $appex" >&2; exit 1; }
  local id ext_build ext_version point
  id="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$plist")"
  ext_build="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleVersion' "$plist")"
  ext_version="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$plist")"
  point="$(/usr/libexec/PlistBuddy -c 'Print :NSExtension:NSExtensionPointIdentifier' "$plist")"
  [[ "$id" == "$app_id.live" ]] || {
    echo "error: extension bundle identifier is $id; expected $app_id.live" >&2
    exit 1
  }
  [[ "$ext_build" == "$build_number" && "$ext_version" == "$version" ]] || {
    echo "error: extension version $ext_version ($ext_build) differs from the app's $version ($build_number)" >&2
    exit 1
  }
  [[ "$point" == "com.apple.widgetkit-extension" ]] || {
    echo "error: extension point is $point; expected com.apple.widgetkit-extension" >&2
    exit 1
  }
  /usr/libexec/PlistBuddy -c 'Print :NSSupportsLiveActivities' "$app/Info.plist" | grep -q true || {
    echo "error: app Info.plist does not set NSSupportsLiveActivities" >&2
    exit 1
  }
  if [[ "$require_signature" == "yes" ]]; then
    [[ -f "$appex/embedded.mobileprovision" ]] || {
      echo "error: signed extension has no embedded provisioning profile" >&2
      exit 1
    }
    codesign --verify --deep --strict "$appex"
    local bundle_group="group.$app_id"
    local target
    for target in "$app" "$appex"; do
      codesign -d --entitlements :- "$target" 2>/dev/null | grep -q "$bundle_group" || {
        echo "error: $(basename "$target") is not signed with App Group $bundle_group" >&2
        exit 1
      }
    done
  fi
  echo "ok: Live Activity extension ($id) matches the app version and App Group"
}

if [[ "$MODE" == "simulator" ]]; then
  verify_app "$ARTIFACT" no
  echo "iOS Simulator artifact verification passed"
  exit 0
fi

[[ -s "$ARTIFACT" ]] || { echo "error: IPA is missing or empty: $ARTIFACT" >&2; exit 1; }
unzip -tq "$ARTIFACT" >/dev/null
TEMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TEMP_DIR"' EXIT
unzip -q "$ARTIFACT" -d "$TEMP_DIR"
mapfile_supported=false
if builtin help mapfile >/dev/null 2>&1; then mapfile_supported=true; fi
if [[ "$mapfile_supported" == true ]]; then
  mapfile -t APPS < <(find "$TEMP_DIR/Payload" -mindepth 1 -maxdepth 1 -type d -name '*.app')
else
  APPS=()
  while IFS= read -r app; do APPS+=("$app"); done < <(find "$TEMP_DIR/Payload" -mindepth 1 -maxdepth 1 -type d -name '*.app')
fi
[[ ${#APPS[@]} -eq 1 ]] || {
  echo "error: IPA must contain exactly one Payload/*.app; found ${#APPS[@]}" >&2
  exit 1
}
verify_app "${APPS[0]}" yes
echo "signed iOS IPA verification passed"
