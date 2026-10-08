#!/usr/bin/env bash
# Materialize a PKCS#12/JKS keystore from base64 into a temp file for CI/local use.
#
# Inputs (env, never printed):
#   MEWLA_ANDROID_KEYSTORE_BASE64  — required; base64 of the keystore binary
#   MEWLA_ANDROID_KEYSTORE         — optional output path; if empty, a temp path is used
#
# Outputs:
#   Writes the keystore file (mode 0600) and prints ONLY the absolute path on stdout.
#   Does not print base64, passwords, or keystore bytes.
#
# Usage:
#   export MEWLA_ANDROID_KEYSTORE_BASE64='…'   # do not commit; do not echo
#   KS="$(./scripts/materialize-android-keystore.sh)"
#   export MEWLA_ANDROID_KEYSTORE="$KS"
#   # later: shred/rm "$KS"

set -euo pipefail

if [[ -z "${MEWLA_ANDROID_KEYSTORE_BASE64:-}" ]]; then
  echo "error: MEWLA_ANDROID_KEYSTORE_BASE64 is required" >&2
  exit 1
fi

OUT="${MEWLA_ANDROID_KEYSTORE:-}"
if [[ -z "$OUT" ]]; then
  OUT="$(mktemp "${TMPDIR:-/tmp}/mewla-android-keystore.XXXXXX.p12")"
fi

# Ensure parent exists; create/truncate target with restrictive perms before write.
mkdir -p "$(dirname "$OUT")"
: >"$OUT"
chmod 600 "$OUT"

# Decode without logging secret material. Use python to avoid shell history edge cases.
python3 - "$OUT" <<'PY'
import base64, os, sys
from pathlib import Path

out = Path(sys.argv[1])
raw = os.environ.get("MEWLA_ANDROID_KEYSTORE_BASE64", "")
if not raw.strip():
    sys.exit("MEWLA_ANDROID_KEYSTORE_BASE64 empty")
# tolerate whitespace/newlines in secret storage
data = base64.b64decode("".join(raw.split()), validate=False)
if len(data) < 32:
    sys.exit("decoded keystore too small; refusing")
# write via fd with 0600 already set
out.write_bytes(data)
os.chmod(out, 0o600)
print(out.resolve())
PY
