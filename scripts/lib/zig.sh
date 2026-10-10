# shellcheck shell=bash
# Resolve the Zig pinned in app/modules/terminal-vt/native.lock.json.
#
# Source this file, then run:
#   ZIG_BIN="$(mewla_resolve_zig "$LOCK" "$TOOL_CACHE")"
#
# A zig already on PATH (or in $ZIG_BIN) is used when its version matches the
# lock. Otherwise the pinned tarball for this host is downloaded into the tool
# cache, checked against the locked SHA-256 and unpacked there. Only the
# resolved path goes to stdout.

mewla_resolve_zig() {
  local lock="$1"
  local tool_cache="$2"
  local host
  case "$(uname -s)/$(uname -m)" in
    Linux/x86_64|Linux/amd64) host=x86_64-linux ;;
    Linux/aarch64|Linux/arm64) host=aarch64-linux ;;
    Darwin/arm64) host=aarch64-macos ;;
    *)
      echo "error: no pinned Zig for $(uname -s)/$(uname -m)" >&2
      return 1
      ;;
  esac

  local version url sha256 archive_root
  {
    read -r version
    read -r url
    read -r sha256
    read -r archive_root
  } < <(python3 - "$lock" "$host" <<'PY'
import json, sys
zig = json.load(open(sys.argv[1]))["zig"]
download = zig["downloads"][sys.argv[2]]
print(zig["version"])
print(download["tarball"])
print(download["sha256"])
print(download["archive_root"])
PY
  )
  if [[ -z "$archive_root" ]]; then
    echo "error: could not read the pinned Zig for $host from $lock" >&2
    return 1
  fi

  local candidate="${ZIG_BIN:-zig}"
  if command -v "$candidate" >/dev/null 2>&1; then
    local have
    have="$("$candidate" version)"
    if [[ "$have" == "$version" || "$have" == "$version".* ]]; then
      command -v "$candidate"
      return
    fi
  fi

  local archive="$tool_cache/${url##*/}"
  local zig="$tool_cache/$archive_root/zig"
  mkdir -p "$tool_cache"
  if [[ ! -f "$archive" ]]; then
    echo "Downloading pinned Zig $version for $host..." >&2
    curl -fsSL --retry 3 "$url" -o "$archive.part"
    mv "$archive.part" "$archive"
  fi
  local actual
  if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "$archive" | awk '{print $1}')"
  else
    actual="$(shasum -a 256 "$archive" | awk '{print $1}')"
  fi
  if [[ "$actual" != "$sha256" ]]; then
    echo "error: Zig archive checksum mismatch: got $actual want $sha256" >&2
    rm -f "$archive"
    return 1
  fi
  if [[ ! -x "$zig" ]]; then
    tar -xf "$archive" -C "$tool_cache"
  fi
  local got
  got="$("$zig" version)"
  if [[ "$got" != "$version" && "$got" != "$version".* ]]; then
    echo "error: unpacked Zig reports $got; lock wants $version" >&2
    return 1
  fi
  echo "$zig"
}
