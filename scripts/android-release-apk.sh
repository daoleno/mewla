#!/usr/bin/env bash
# Build the Android release of the NSH client (client/android): the arm64-v8a
# sideload APK (GitHub channel: Phone use, in-app update), or with --aab the
# Google Play App Bundle (Play channel: neither, no REQUEST_INSTALL_PACKAGES).
# Both are the production identity com.daoleno.mewla, version and versionCode
# from client/ui/nsh.json (scripts/lib/release_identity.py).
#
# Prerequisites:
#   - JDK 17, Android SDK (ANDROID_HOME) with NDK 27.1.12297006 and CMake 3.22.1
#   - NSH (client/scripts/fetch-nsh.sh; NSH_HOME, default ~/.nsh)
#   - release_grade libghostty arm64 (./scripts/build-libghostty.sh --abis arm64-v8a)
#
# Signing (secret-safe):
#   Default: the debug keystore (local/dev sideload only).
#   Release keystore via environment only (never commit; never printed):
#     MEWLA_ANDROID_KEYSTORE=/absolute/path/to/release.keystore
#     MEWLA_ANDROID_KEYSTORE_PASSWORD=...
#     MEWLA_ANDROID_KEY_ALIAS=...
#     MEWLA_ANDROID_KEY_PASSWORD=...
#   client/android/app/build.gradle.kts reads these with System.getenv.
#   This script does not pass secrets as -P flags.
#
# Usage:
#   ./scripts/android-release-apk.sh          # GitHub APK -> dist-download/android-native/
#   ./scripts/android-release-apk.sh --aab    # Play AAB -> dist-download/android-play/; then scripts/verify-aab-play.py

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

AAB=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --aab) AAB=1; shift ;;
    -h|--help) sed -n '2,26p' "$0"; exit 0 ;;
    *) echo "error: unknown arg $1" >&2; exit 2 ;;
  esac
done

export JAVA_HOME="${JAVA_HOME:-/usr/lib/jvm/java-17-openjdk}"
export PATH="$JAVA_HOME/bin:$PATH"

if [[ ! -x "$JAVA_HOME/bin/java" ]]; then
  echo "error: JDK 17 not found at JAVA_HOME=$JAVA_HOME" >&2
  exit 1
fi

if [[ -z "${ANDROID_HOME:-}${ANDROID_SDK_ROOT:-}" ]]; then
  if [[ -d "$HOME/Android/Sdk" ]]; then
    export ANDROID_HOME="$HOME/Android/Sdk"
  else
    echo "error: set ANDROID_HOME" >&2
    exit 1
  fi
fi

NSH_BIN="${NSH:-${NSH_HOME:-$HOME/.nsh}/bin/nsh}"
if [[ ! -x "$NSH_BIN" ]]; then
  echo "error: NSH not found at $NSH_BIN; run client/scripts/fetch-nsh.sh" >&2
  exit 1
fi

"$ROOT/scripts/verify-libghostty.sh" --release

# Signing env completeness (do not print values)
if [[ -n "${MEWLA_ANDROID_KEYSTORE:-}" ]]; then
  for v in MEWLA_ANDROID_KEYSTORE_PASSWORD MEWLA_ANDROID_KEY_ALIAS MEWLA_ANDROID_KEY_PASSWORD; do
    if [[ -z "${!v:-}" ]]; then
      echo "error: $v is required when MEWLA_ANDROID_KEYSTORE is set" >&2
      exit 1
    fi
  done
  if [[ ! -f "$MEWLA_ANDROID_KEYSTORE" ]]; then
    echo "error: keystore file not found (path redacted)" >&2
    exit 1
  fi
  echo "note: MEWLA_ANDROID_KEYSTORE is set; the release is signed with it."
else
  echo "note: no MEWLA_ANDROID_KEYSTORE set — the build uses the debug keystore (local/dev sideload only)."
fi

IDENTITY="$ROOT/scripts/lib/release_identity.py"
VERSION="$(python3 "$IDENTITY" version)"
VERSION_CODE="$(python3 "$IDENTITY" version_code)"
PACKAGE="$(python3 "$IDENTITY" package)"
echo "identity: package=$PACKAGE versionName=$VERSION versionCode=$VERSION_CODE"

GRADLE_APP="$ROOT/client/android/app/build.gradle.kts"
grep -q 'System.getenv("MEWLA_ANDROID_KEYSTORE_PASSWORD")' "$GRADLE_APP" || {
  echo "error: client/android/app/build.gradle.kts lacks the MEWLA_ANDROID_* signing config" >&2
  exit 1
}

if [[ $AAB -eq 1 ]]; then
  CHANNEL=play
  TASK=:app:bundleProductionRelease
  OUTPUT="$ROOT/client/android/app/build/outputs/bundle/productionRelease/app-production-release.aab"
else
  CHANNEL=github
  TASK=:app:assembleProductionRelease
  OUTPUT="$ROOT/client/android/app/build/outputs/apk/production/release/app-production-release.apk"
fi
# Never pick up an earlier build's output.
rm -f "$OUTPUT"
(
  cd "$ROOT/client/android"
  ./gradlew --no-daemon --console=plain --build-cache "$TASK" \
    -Pmewla.channel="$CHANNEL" -Pmewla.abis=arm64-v8a
)
if [[ ! -f "$OUTPUT" ]]; then
  echo "error: $(basename "$OUTPUT") not found after $TASK" >&2
  exit 1
fi

if [[ $AAB -eq 1 ]]; then
  OUT_DIR="$ROOT/dist-download/android-play"
  mkdir -p "$OUT_DIR"
  COPY="$OUT_DIR/mewla-android-v${VERSION}-${VERSION_CODE}.aab"
  cp -f "$OUTPUT" "$COPY"
  (cd "$OUT_DIR" && sha256sum "$(basename "$COPY")" | tee "$(basename "$COPY").sha256")
  echo ""
  echo "AAB:  $COPY"
  echo "Next: ./scripts/verify-aab-play.py --bundletool <bundletool-all.jar> $COPY"
  echo "Done (no upload)."
  exit 0
fi

APK="$OUTPUT"
"$ROOT/scripts/verify-android-native-symbols.py" --lock \
  "$ROOT/native/terminal-vt/native.lock.json" --apk "$APK"
"$ROOT/scripts/verify-apk-notice.sh" "$APK"

# The built APK carries the tracked identity (versionName / versionCode from client/ui/nsh.json).
AAPT="$(command -v aapt2 || find "${ANDROID_HOME:-$ANDROID_SDK_ROOT}/build-tools" -type f -name aapt2 2>/dev/null | sort -V | tail -1)"
[[ -n "$AAPT" ]] || { echo "error: aapt2 not found; install Android build-tools" >&2; exit 1; }
# The whole output, then its first line: `| head -1` would SIGPIPE aapt2 under pipefail.
BADGING="$("$AAPT" dump badging "$APK")"
BADGING="${BADGING%%$'\n'*}"
EXPECTED="package: name='$PACKAGE' versionCode='$VERSION_CODE' versionName='$VERSION'"
[[ "$BADGING" == "$EXPECTED"* ]] || {
  echo "error: APK identity mismatch: got ${BADGING@Q}, want ${EXPECTED@Q}" >&2
  exit 1
}
echo "ok: APK identity package=$PACKAGE versionName=$VERSION versionCode=$VERSION_CODE"

OUT_DIR="$ROOT/dist-download/android-native"
mkdir -p "$OUT_DIR"
# Name from canonical app identity + pin short + content hash (not a formal release).
PIN_SHORT="$(python3 -c "import json;print(json.load(open('$ROOT/native/terminal-vt/native.lock.json'))['ghostty']['commit'][:12])")"
APK_SHA="$(sha256sum "$APK" | awk '{print $1}')"
COPY="$OUT_DIR/mewla-android-arm64-v${VERSION}-${PIN_SHORT}-${APK_SHA:0:12}.apk"
cp -f "$APK" "$COPY"
echo "$APK_SHA  $(basename "$COPY")" | tee "$COPY.sha256"
# Adjacent copy for humans who unpack the directory (APK itself already verified)
cp -f "$ROOT/client/ui/more/settings/licenses/texts/Ghostty-MIT.txt" "$OUT_DIR/GHOSTTY-MIT.txt"
if [[ -f "$ROOT/native/terminal-vt/libs/android/SHA256SUMS" ]]; then
  cp -f "$ROOT/native/terminal-vt/libs/android/SHA256SUMS" "$OUT_DIR/libghostty-SHA256SUMS"
fi
if [[ -f "$ROOT/native/terminal-vt/libs/android/build-manifest.json" ]]; then
  cp -f "$ROOT/native/terminal-vt/libs/android/build-manifest.json" "$OUT_DIR/libghostty-build-manifest.json"
fi

echo ""
echo "APK:            $COPY"
echo "Checksum:       $COPY.sha256"
echo "Notice in APK:  assets/notices/GHOSTTY-MIT.txt (verified)"
echo "Notice adjacent:$OUT_DIR/GHOSTTY-MIT.txt"
echo "Done (no publish)."
