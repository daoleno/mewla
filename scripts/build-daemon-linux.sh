#!/usr/bin/env bash
# Build release-shaped mewla binaries for Linux amd64/arm64, macOS arm64 and
# Windows amd64.
#
# Release binaries carry the session host's terminal engine (build tag
# ghosttyvt): libghostty-vt linked statically through cgo, built by
# scripts/build-libghostty-desktop.sh from the signed, pinned release with the
# pinned Zig. Linux binaries are linked fully static against musl with
# `zig cc`, and the Windows one (mewla-windows-amd64.exe) statically against
# MinGW with `zig cc` too, both from any Linux host; the macOS binary links with
# the system clang and so builds only on a Mac (minimum macOS 12, as before).
# netgo and osusergo keep DNS and user lookups the same pure-Go code the
# CGO_ENABLED=0 releases used.
#
# Reads product version from client/ui/nsh.json (scripts/lib/release_identity.py) unless MEWLA_VERSION is set.
# Embeds the web UI via scripts/build-web-ui.sh (needs `bun install`).
# Does not publish, tag, sign Android APKs, or read release keystores.
#
# Usage:
#   ./scripts/build-daemon-linux.sh                        # this host's targets, with the engine
#   ./scripts/build-daemon-linux.sh --targets linux-amd64,linux-arm64 --out-dir dist-download/staging/bin
#   ./scripts/build-daemon-linux.sh --targets windows-amd64
#   ./scripts/build-daemon-linux.sh --engine off           # every target, CGO_ENABLED=0, no engine
#   MEWLA_VERSION=0.1.2 ./scripts/build-daemon-linux.sh
#
# --targets defaults to linux-amd64,linux-arm64,windows-amd64 on Linux and
# darwin-arm64 on a Mac (all four with --engine off). Library cache:
# ${MEWLA_GHOSTTY_DESKTOP_CACHE:-$HOME/.cache/mewla/ghostty-desktop}.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

OUT_DIR=""
TARGETS=""
ENGINE="required"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --out-dir)
      OUT_DIR="${2:?}"
      shift 2
      ;;
    --targets)
      TARGETS="${2:?--targets needs a list}"
      shift 2
      ;;
    --engine)
      ENGINE="${2:?--engine needs required or off}"
      shift 2
      ;;
    -h|--help)
      sed -n '2,28p' "$0"
      exit 0
      ;;
    *)
      echo "error: unknown arg $1" >&2
      exit 2
      ;;
  esac
done

case "$ENGINE" in
  required|off) ;;
  *) echo "error: --engine must be required or off, not $ENGINE" >&2; exit 2 ;;
esac

if [[ -z "$TARGETS" ]]; then
  if [[ "$ENGINE" == off ]]; then
    TARGETS="linux-amd64,linux-arm64,darwin-arm64,windows-amd64"
  else
    case "$(uname -s)" in
      Linux) TARGETS="linux-amd64,linux-arm64,windows-amd64" ;;
      Darwin) TARGETS="darwin-arm64" ;;
      *) echo "error: no default targets on $(uname -s); pass --targets" >&2; exit 2 ;;
    esac
  fi
fi
IFS=, read -r -a TARGET_LIST <<<"$TARGETS"
for target in "${TARGET_LIST[@]}"; do
  case "$target" in
    linux-amd64|linux-arm64|darwin-arm64|windows-amd64) ;;
    *) echo "error: unknown target $target (linux-amd64, linux-arm64, darwin-arm64, windows-amd64)" >&2; exit 2 ;;
  esac
  if [[ "$ENGINE" == required && "$target" == darwin-* && "$(uname -s)" != Darwin ]]; then
    echo "error: darwin-arm64 with the session host's terminal engine links with Apple's toolchain and builds only on a Mac; build it there or pass --engine off" >&2
    exit 2
  fi
done

VERSION="${MEWLA_VERSION:-}"
if [[ -z "$VERSION" ]]; then
  VERSION="$(python3 "$ROOT/scripts/lib/release_identity.py" version)"
fi

if [[ -z "$OUT_DIR" ]]; then
  OUT_DIR="$ROOT/dist-download/staging/bin"
elif [[ "$OUT_DIR" != /* ]]; then
  OUT_DIR="$ROOT/$OUT_DIR"
fi
mkdir -p "$OUT_DIR"

export GOFLAGS="${GOFLAGS:-} -trimpath"
if [[ -n "${SOURCE_DATE_EPOCH:-}" ]]; then
  export GOSUMDB="${GOSUMDB:-sum.golang.org}"
fi

LIBS=""
ZIG_BIN_PATH=""
ZIG_CC_CACHE=""
if [[ "$ENGINE" == required ]]; then
  LOCK="$ROOT/app/modules/terminal-vt/native.lock.json"
  LIBS="${MEWLA_GHOSTTY_DESKTOP_CACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/mewla/ghostty-desktop}"
  lib_targets=()
  for target in "${TARGET_LIST[@]}"; do
    case "$target" in
      linux-amd64) lib_targets+=(linux_amd64_musl) ;;
      linux-arm64) lib_targets+=(linux_arm64_musl) ;;
      darwin-arm64) lib_targets+=(darwin_arm64) ;;
      windows-amd64) lib_targets+=(windows_amd64) ;;
    esac
  done
  "$ROOT/scripts/build-libghostty-desktop.sh" --out "$LIBS" --targets "$(IFS=,; echo "${lib_targets[*]}")"
  if [[ "$TARGETS" == *linux-* || "$TARGETS" == *windows-* ]]; then
    # shellcheck source=lib/zig.sh
    source "$ROOT/scripts/lib/zig.sh"
    ZIG_BIN_PATH="$(mewla_resolve_zig "$LOCK" "${MEWLA_TOOL_CACHE:-$ROOT/.cache/mewla-tools}")"
    ZIG_CC_CACHE="${XDG_CACHE_HOME:-$HOME/.cache}/mewla/zig-cc"
    mkdir -p "$ZIG_CC_CACHE"
  fi
fi

"$ROOT/scripts/build-web-ui.sh"

PUBLISHER_FLAGS="$(python3 "$ROOT/scripts/plugin-publisher-flags.py")"
# releaseBuild marks a binary that may update itself; local and dev builds never do.
LDFLAGS="-s -w -buildid= -X main.Version=${VERSION} -X main.releaseBuild=1 ${PUBLISHER_FLAGS}"

build_one() {
  local target="$1"
  local goos="${target%-*}"
  local goarch="${target#*-}"
  local out_path="$OUT_DIR/mewla-$target"
  [[ "$goos" != windows ]] || out_path="$out_path.exe"
  if [[ "$ENGINE" == off ]]; then
    echo "Building ${goos}/${goarch} CGO=0 → ${out_path} (version=${VERSION})"
    (
      cd "$ROOT/daemon"
      CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -buildvcs=false -ldflags="$LDFLAGS" -o "$out_path" ./cmd/mewla/
    )
  elif [[ "$goos" == windows ]]; then
    echo "Building ${goos}/${goarch} CGO=1 ghosttyvt static MinGW (zig cc) → ${out_path} (version=${VERSION})"
    (
      cd "$ROOT/daemon"
      ZIG_GLOBAL_CACHE_DIR="$ZIG_CC_CACHE" ZIG_LOCAL_CACHE_DIR="$ZIG_CC_CACHE" \
      CC="$ZIG_BIN_PATH cc -target x86_64-windows-gnu" \
      CGO_ENABLED=1 GOOS="$goos" GOARCH="$goarch" \
      CGO_CFLAGS="-I$LIBS/include" CGO_LDFLAGS="-L$LIBS/windows_amd64" \
        go build -buildvcs=false -tags ghosttyvt,netgo,osusergo -ldflags="$LDFLAGS" -o "$out_path" ./cmd/mewla/
    )
  elif [[ "$goos" == linux ]]; then
    local zig_arch
    case "$goarch" in amd64) zig_arch=x86_64 ;; arm64) zig_arch=aarch64 ;; esac
    echo "Building ${goos}/${goarch} CGO=1 ghosttyvt static musl (zig cc) → ${out_path} (version=${VERSION})"
    (
      cd "$ROOT/daemon"
      ZIG_GLOBAL_CACHE_DIR="$ZIG_CC_CACHE" ZIG_LOCAL_CACHE_DIR="$ZIG_CC_CACHE" \
      CC="$ZIG_BIN_PATH cc -target ${zig_arch}-linux-musl" \
      CGO_ENABLED=1 GOOS="$goos" GOARCH="$goarch" \
      CGO_CFLAGS="-I$LIBS/include" CGO_LDFLAGS="-L$LIBS/linux_${goarch}_musl" \
        go build -buildvcs=false -tags ghosttyvt,netgo,osusergo \
        -ldflags="$LDFLAGS -linkmode external -extldflags -static" -o "$out_path" ./cmd/mewla/
    )
  else
    echo "Building ${goos}/${goarch} CGO=1 ghosttyvt (clang, macOS 12+) → ${out_path} (version=${VERSION})"
    (
      cd "$ROOT/daemon"
      MACOSX_DEPLOYMENT_TARGET=12.0 \
      CGO_ENABLED=1 GOOS="$goos" GOARCH="$goarch" \
      CGO_CFLAGS="-I$LIBS/include -mmacosx-version-min=12.0" \
      CGO_LDFLAGS="-L$LIBS/darwin_arm64 -mmacosx-version-min=12.0" \
        go build -buildvcs=false -tags ghosttyvt,netgo,osusergo -ldflags="$LDFLAGS" -o "$out_path" ./cmd/mewla/
    )
  fi
  chmod +x "$out_path"
  if [[ "$ENGINE" == required ]]; then
    # The binary must carry the engine, not the stub.
    local info
    info="$(go version -m "$out_path")"
    grep -Eq $'^\tbuild\t-tags=([^ ]*,)?ghosttyvt(,|$)' <<<"$info" &&
      grep -Eq $'^\tbuild\tCGO_ENABLED=1$' <<<"$info" || {
      echo "error: $out_path was not built with the ghosttyvt tag and cgo" >&2
      exit 1
    }
  fi
}

for target in "${TARGET_LIST[@]}"; do
  build_one "$target"
done

echo ""
echo "VERSION=$VERSION"
echo "ENGINE=$ENGINE"
echo "OUT_DIR=$OUT_DIR"
for target in "${TARGET_LIST[@]}"; do
  ls -la "$OUT_DIR/mewla-$target"*
done
echo "Done (no publish)."
