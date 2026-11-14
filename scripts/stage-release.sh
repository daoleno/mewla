#!/usr/bin/env bash
# Deterministic local staging for a release candidate.
#
# Stages under dist-download/v<version>/ (gitignored), always from a clean dir:
#   - Linux amd64/arm64 and macOS arm64 daemon archives (.tar.gz) and the
#     Windows amd64 one (.zip)
#   - SHA256SUMS, release-manifest.json, and its detached Ed25519 signature
#   - optional Android APK
#
# Does not read release keystores, commit, push, tag, or create a GitHub Release.
#
# Usage:
#   ./scripts/stage-release.sh
#   ./scripts/stage-release.sh --skip-build
#   ./scripts/stage-release.sh --with-apk
#   ./scripts/stage-release.sh --apk path/to.apk
#   MEWLA_BUILD_TMPDIR=/durable/path ./scripts/stage-release.sh

set -euo pipefail
umask 077

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

SKIP_BUILD=0
WITH_APK=0
APK_PATH=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --skip-build) SKIP_BUILD=1; shift ;;
    --with-apk) WITH_APK=1; shift ;;
    --apk)
      APK_PATH="${2:?}"
      shift 2
      ;;
    -h|--help)
      sed -n '2,20p' "$0"
      exit 0
      ;;
    *)
      echo "error: unknown arg $1" >&2
      exit 2
      ;;
  esac
done

if [[ -n "${MEWLA_ANDROID_KEYSTORE:-}" && "$WITH_APK" -eq 0 && -z "$APK_PATH" ]]; then
  echo "note: MEWLA_ANDROID_KEYSTORE is set but this run is not building an APK; env ignored."
fi

# Version from client/ui/nsh.json; the APK's versionCode follows from it
# (scripts/lib/release_identity.py), the same number the client's Gradle build uses.
IDENTITY="$ROOT/scripts/lib/release_identity.py"
VERSION="$(python3 "$IDENTITY" version)"
PACKAGE="$(python3 "$IDENTITY" package)"
VERSION_CODE="$(python3 "$IDENTITY" version_code)"
CANONICAL_CONFIG="$(python3 "$IDENTITY" source)"

# Validate version before any path join or rm (reject traversal / separators).
if ! python3 -c "
import re, sys
v = sys.argv[1]
if not re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.]+)?', v):
    sys.exit('invalid version format: ' + v)
if any(x in v for x in ('/', '\\\\', '..', '\x00')):
    sys.exit('version must not contain path elements: ' + v)
" "$VERSION"; then
  echo "error: refusing unsafe or invalid version from $CANONICAL_CONFIG: ${VERSION@Q}" >&2
  exit 1
fi

NOTES_SRC="$ROOT/docs/releases/v${VERSION}.md"
if [[ ! -f "$NOTES_SRC" ]]; then
  echo "error: tracked release notes missing: docs/releases/v${VERSION}.md" >&2
  exit 1
fi

STAGE_DIR="$ROOT/dist-download/v${VERSION}"
# Resolve and ensure stage is exactly under dist-download/v<version>
STAGE_PARENT="$(cd "$ROOT/dist-download" 2>/dev/null && pwd || true)"
mkdir -p "$ROOT/dist-download"
STAGE_PARENT="$(cd "$ROOT/dist-download" && pwd)"
EXPECTED_STAGE="$STAGE_PARENT/v${VERSION}"

# Pre-build into a durable cache so a cross-platform release build never
# consumes the host's global temporary filesystem. Delegated sessions inject
# MEWLA_BUILD_TMPDIR as their private lifecycle-owned resource directory.
default_build_tmpdir() {
  if [[ -n "${XDG_CACHE_HOME:-}" ]]; then
    printf '%s\n' "$XDG_CACHE_HOME/mewla/build-tmp"
  elif [[ "$(uname -s)" == "Darwin" ]]; then
    printf '%s\n' "$HOME/Library/Caches/mewla/build-tmp"
  else
    printf '%s\n' "$HOME/.cache/mewla/build-tmp"
  fi
}
BUILD_TMP_ROOT="${MEWLA_BUILD_TMPDIR:-$(default_build_tmpdir)}"
if [[ "$BUILD_TMP_ROOT" != /* ]]; then
  echo "error: MEWLA_BUILD_TMPDIR must be an absolute path: $BUILD_TMP_ROOT" >&2
  exit 1
fi
mkdir -p "$BUILD_TMP_ROOT"
chmod 700 "$BUILD_TMP_ROOT"
BUILD_TMP="$(mktemp -d "$BUILD_TMP_ROOT/mewla-stage-build.XXXXXX")"
cleanup() { rm -rf "$BUILD_TMP"; }
trap cleanup EXIT

export MEWLA_VERSION="$VERSION"

if [[ $SKIP_BUILD -eq 0 ]]; then
  "$ROOT/scripts/build-daemon-linux.sh" --out-dir "$BUILD_TMP"
else
  # Prefer existing stage binaries if present; else staging default from build script.
  for f in mewla-linux-amd64 mewla-linux-arm64 mewla-darwin-arm64 mewla-windows-amd64.exe; do
    if [[ -f "$EXPECTED_STAGE/$f" ]]; then
      cp -f "$EXPECTED_STAGE/$f" "$BUILD_TMP/$f"
    elif [[ -f "$EXPECTED_STAGE/bin/$f" ]]; then
      cp -f "$EXPECTED_STAGE/bin/$f" "$BUILD_TMP/$f"
    elif [[ -f "$ROOT/dist-download/staging/bin/$f" ]]; then
      cp -f "$ROOT/dist-download/staging/bin/$f" "$BUILD_TMP/$f"
    else
      echo "error: --skip-build but missing $f (looked in stage and dist-download/staging/bin)" >&2
      exit 1
    fi
  done
fi

for f in mewla-linux-amd64 mewla-linux-arm64 mewla-darwin-arm64 mewla-windows-amd64.exe; do
  if [[ ! -f "$BUILD_TMP/$f" ]]; then
    echo "error: build output missing $f" >&2
    exit 1
  fi
done

# Safe clean: only the exact validated stage directory under dist-download.
if [[ -e "$EXPECTED_STAGE" || -L "$EXPECTED_STAGE" ]]; then
  # Refuse if path is not exactly dist-download/v<version> after resolve.
  resolved="$(python3 -c "
import os, sys
path = sys.argv[1]
parent = sys.argv[2]
version = sys.argv[3]
real = os.path.realpath(path)
expect = os.path.realpath(os.path.join(parent, 'v' + version))
if real != expect:
    sys.exit(f'refuse rm: {real} != {expect}')
if not real.startswith(os.path.realpath(parent) + os.sep):
    sys.exit(f'refuse rm outside dist-download: {real}')
print(real)
" "$EXPECTED_STAGE" "$STAGE_PARENT" "$VERSION")"
  echo "Cleaning stage directory: $resolved"
  rm -rf --one-file-system -- "$resolved"
fi

mkdir -p "$EXPECTED_STAGE"
STAGE_DIR="$EXPECTED_STAGE"

# Package each daemon with the legal files that apply to the daemon distribution.
# gzip -n and normalized tar metadata keep archives stable across CI runs.
ARCHIVE_EPOCH="${SOURCE_DATE_EPOCH:-0}"
# package_daemon BINARY ARCHIVE: the archive carries the binary as `mewla`.
package_daemon() {
  local binary_name="$1"
  local archive_name="$2"
  local entry=mewla
  local package_dir="$BUILD_TMP/package"
  rm -rf "$package_dir"
  mkdir -p "$package_dir"
  cp -f "$BUILD_TMP/$binary_name" "$package_dir/$entry"
  chmod +x "$package_dir/$entry"
  cp -f "$ROOT/LICENSE" "$package_dir/LICENSE"
  cp -f "$ROOT/NOTICE" "$package_dir/NOTICE"
  cp -f "$ROOT/TRADEMARKS.md" "$package_dir/TRADEMARKS.md"
  tar --sort=name --mtime="@${ARCHIVE_EPOCH}" --owner=0 --group=0 --numeric-owner \
    -C "$package_dir" -cf - LICENSE NOTICE TRADEMARKS.md "$entry" | gzip -n > "$STAGE_DIR/$archive_name"
}

for platform in linux-amd64 linux-arm64 darwin-arm64; do
  package_daemon "mewla-$platform" "mewla-$platform.tar.gz"
done

# Windows: a zip holding exactly LICENSE, NOTICE, TRADEMARKS.md and mewla.exe
# at its root (what install.ps1 and the updater accept), with fixed metadata so
# the bytes are stable across CI runs.
python3 - "$BUILD_TMP/mewla-windows-amd64.exe" "$ROOT" "$STAGE_DIR/mewla-windows-amd64.zip" "$ARCHIVE_EPOCH" <<'PY'
import sys, time, zipfile
from pathlib import Path

exe, root, out, epoch = Path(sys.argv[1]), Path(sys.argv[2]), Path(sys.argv[3]), int(sys.argv[4])
stamp = time.gmtime(max(epoch, 315532800))[:6]  # zip dates start in 1980
entries = [("LICENSE", root / "LICENSE", 0o644), ("NOTICE", root / "NOTICE", 0o644),
           ("TRADEMARKS.md", root / "TRADEMARKS.md", 0o644), ("mewla.exe", exe, 0o755)]
with zipfile.ZipFile(out, "w") as archive:
    for name, source, mode in entries:
        info = zipfile.ZipInfo(name, date_time=stamp)
        info.compress_type = zipfile.ZIP_DEFLATED
        info.create_system = 3
        info.external_attr = (0o100000 | mode) << 16
        archive.writestr(info, source.read_bytes(), compresslevel=9)
PY

STAGED_APK=""
if [[ $WITH_APK -eq 1 ]]; then
  "$ROOT/scripts/android-release-apk.sh"
fi

if [[ -n "$APK_PATH" ]]; then
  if [[ ! -f "$APK_PATH" ]]; then
    echo "error: --apk path not found" >&2
    exit 1
  fi
  STAGED_APK="$STAGE_DIR/mewla-android-arm64-v${VERSION}.apk"
  cp -f "$APK_PATH" "$STAGED_APK"
elif [[ $WITH_APK -eq 1 ]]; then
  if compgen -G "$ROOT/dist-download/android-native/mewla-android-arm64-*.apk" > /dev/null; then
    newest="$(ls -t "$ROOT"/dist-download/android-native/mewla-android-arm64-*.apk | head -1)"
    STAGED_APK="$STAGE_DIR/mewla-android-arm64-v${VERSION}.apk"
    cp -f "$newest" "$STAGED_APK"
  fi
fi

if [[ -n "$STAGED_APK" ]]; then
  "$ROOT/scripts/verify-apk-notice.sh" "$STAGED_APK"
fi

# Checksums for every payload file (not identity.json / SHA256SUMS itself).
SUMS="$STAGE_DIR/SHA256SUMS"
(
  cd "$STAGE_DIR"
  files=(
    mewla-linux-amd64.tar.gz
    mewla-linux-arm64.tar.gz
    mewla-darwin-arm64.tar.gz
    mewla-windows-amd64.zip
  )
  if [[ -n "$STAGED_APK" && -f "$(basename "$STAGED_APK")" ]]; then
    files+=("$(basename "$STAGED_APK")")
  fi
  for rel in "${files[@]}"; do
    sha256sum "$rel"
  done | LC_ALL=C sort -k2
) > "$SUMS"

python3 - "$STAGE_DIR" "$VERSION" "$PACKAGE" "$VERSION_CODE" "${STAGED_APK:-}" "$BUILD_TMP" "$CANONICAL_CONFIG" <<'PY'
import hashlib, json, sys
from pathlib import Path

stage, version, package, version_code, staged_apk, build_tmp, canonical_config = sys.argv[1:8]
stage_p = Path(stage)
build_p = Path(build_tmp)

def sha256(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()

def artifact(rel: str, role: str, **extra):
    p = stage_p / rel
    if not p.is_file():
        raise SystemExit(f"missing staged file: {rel}")
    entry = {
        "path": rel,
        "role": role,
        "sha256": sha256(p),
        "size": p.stat().st_size,
    }
    entry.update(extra)
    return entry

artifacts = [
    artifact("mewla-linux-amd64.tar.gz", "daemon_archive", goos="linux", goarch="amd64"),
    artifact("mewla-linux-arm64.tar.gz", "daemon_archive", goos="linux", goarch="arm64"),
    artifact("mewla-darwin-arm64.tar.gz", "daemon_archive", goos="darwin", goarch="arm64"),
    artifact("mewla-windows-amd64.zip", "daemon_archive", goos="windows", goarch="amd64"),
]

if staged_apk:
    apk_p = Path(staged_apk)
    if apk_p.is_file():
        artifacts.append(
            artifact(
                apk_p.name,
                "android_sideload_apk",
                abi="arm64-v8a",
                status="staged_not_published",
            )
        )

identity = {
    "schema_version": 2,
    "product": "mewla",
    "version": version,
    "android": {
        "package": package,
        "version_name": version,
        "version_code": int(version_code),
        "canonical_config": canonical_config,
        "certificate_sha256_fingerprint": (
            "C2:FC:5B:09:B3:86:92:EE:70:59:71:1F:E7:ED:B8:79:"
            "4C:E3:65:FE:1C:7A:06:AB:95:4E:5D:D1:BD:CD:A4:FD"
        ),
    },
    "daemon": {
        "module": "github.com/daoleno/mewla/daemon",
        "targets": ["linux/amd64", "linux/arm64", "darwin/arm64", "windows/amd64"],
        # Every release daemon links the session host's terminal engine.
        "cgo": {
            "linux/amd64": True,
            "linux/arm64": True,
            "darwin/arm64": True,
            "windows/amd64": True,
        },
        "artifact_layout": "platform_archives",
    },
    "artifacts": artifacts,
    "notes": {
        "release_status": "staged",
        "github_release": "publication_is_separate",
        "release_notes_source": f"docs/releases/v{version}.md",
        "signed_apk": "requires_maintainer_keystore_via_env_not_in_repo",
    },
}
(stage_p / "release-manifest.json").write_text(json.dumps(identity, indent=2) + "\n", encoding="utf-8")
print(f"wrote {stage_p / 'release-manifest.json'}")
PY

"$ROOT/scripts/sign-release-manifest.sh" "$STAGE_DIR/release-manifest.json"

echo ""
echo "Staged:   $STAGE_DIR"
echo "Version:  $VERSION"
echo "Package:  $PACKAGE"
echo "Checksums:$SUMS"
ls -la "$STAGE_DIR"
echo "Done (no commit/push/tag/release)."
