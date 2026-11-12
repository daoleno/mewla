#!/usr/bin/env bash
# Every Preview APK (com.daoleno.mewla.preview) is signed with Mewla's release key, so each Preview
# installs over the last one in place and keeps its pairing. Until 11-12 a dispatched Preview was signed
# with the CI runner's own debug key, a new one per run, and no Preview could upgrade another.
#
# Usage: preview-signer.sh <apk>        prints the APK's signer (SHA-256, lower-case hex, no colons);
#        preview-signer.sh --expected   prints the release certificate's (the public fingerprint
#                                       scripts/verify-apk-release.sh pins).
# Exit 1 when the APK's signer is not the release certificate. Needs apksigner (Android build-tools).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

expected() {
  sed -n 's/^EXPECTED_CERT="\([0-9A-Fa-f:]*\)"$/\1/p' "$ROOT/scripts/verify-apk-release.sh" | tr -d ':' | tr 'A-F' 'a-f'
}

signer() {
  local tool="${APKSIGNER:-}"
  if [[ -z "$tool" ]]; then
    tool="$(command -v apksigner || true)"
    local sdk="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}"
    if [[ -z "$tool" && -n "$sdk" ]]; then
      tool="$(find "$sdk/build-tools" -type f -name apksigner 2>/dev/null | sort -V | tail -1)"
    fi
  fi
  [[ -n "$tool" ]] || { echo "error: apksigner not found (set APKSIGNER or ANDROID_HOME)" >&2; exit 2; }
  # apksigner names the first signer "Signer #1" or, by scheme, "V2 Signer" / "V3 Signer"; one key here.
  "$tool" verify --print-certs "$1" 2>/dev/null | sed -n -E 's/^(Signer #1|V[0-9]+ Signer):? certificate SHA-256 digest: *//p' \
    | head -1 | tr -d ':' | tr 'A-F' 'a-f'
}

want="$(expected)"
[[ ${#want} -eq 64 ]] || { echo "error: no release certificate pinned in scripts/verify-apk-release.sh" >&2; exit 2; }
if [[ "${1:-}" == --expected ]]; then
  echo "$want"
  exit 0
fi
apk="${1:?usage: preview-signer.sh <apk> | --expected}"
got="$(signer "$apk")"
echo "$got"
if [[ "$got" != "$want" ]]; then
  echo "error: $apk is signed by $got, not Mewla's release key ($want): it cannot install over another Preview" >&2
  exit 1
fi
