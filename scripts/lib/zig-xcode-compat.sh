# shellcheck shell=bash
# The Zig/Xcode workarounds for the iOS native build. There are two; each is
# retired on its own by scripts/check-zig-xcode-compat.sh once Xcode or Zig no
# longer needs it.
#
# 1. libSystem. Xcode 26.6's macOS 26.5 SDK lists the top-level libSystem stub
#    as arm64e only, while Zig 0.15.2 requires an exact arm64 match.
#    build-libghostty-ios.sh therefore builds against a patched SDK clone and
#    scripts/zig-xcode-compat/xcrun points Zig at it.
#
# 2. libcxx. With the Xcode 27 macOS SDK, INFINITY is undeclared inside Zig
#    0.15.2's bundled libcxx (__random/clamp_to_integral.h), so building libcxx
#    fails. The Ghostty pin's build.zig rejects any Zig minor version but
#    0.15, so a newer Zig is not an option. The build uses a private copy of
#    Zig's lib dir with the patches of native.lock.json's zig.lib_patches
#    applied, selected with ZIG_LIB_DIR; the Zig install stays untouched.
#
# mewla_libsystem_arm64_support SDK prints one word:
#   native       the SDK already lists arm64-macos; no workaround is needed
#   arm64e-only  the SDK lists only arm64e; the workaround is needed
#   unknown      the stub is missing or its layout changed

mewla_libsystem_arm64_support() {
  local libsystem="$1/usr/lib/libSystem.B.tbd"
  local targets
  if [[ ! -r "$libsystem" ]] || ! targets="$(grep -m1 '^targets:' "$libsystem")"; then
    echo unknown
  elif grep -q 'arm64-macos' <<<"$targets"; then
    echo native
  elif grep -q 'arm64e-macos' <<<"$targets"; then
    echo arm64e-only
  else
    echo unknown
  fi
}

# mewla_zig_libcxx_support ZIG_BIN prints one word, from compiling the stock
# Zig's own libcxx header the way the build does, for the macOS target with
# the SDK that xcrun names:
#   fixed    it compiles; the libcxx workaround is no longer needed
#   broken   it fails on INFINITY, the condition the workaround exists for
#   unknown  it fails some other way, or there was nothing to compile
# The failing output is left in $MEWLA_ZIG_PROBE_LOG when that is set.
mewla_zig_libcxx_support() {
  local zig="$1"
  local work
  work="$(mktemp -d "${TMPDIR:-/tmp}/mewla-zig-probe.XXXXXX")" || { echo unknown; return; }
  printf '#include <random>\nint main() { return 0; }\n' >"$work/probe.cpp"
  local log="$work/probe.log" result
  # Zig's lib dir and caches are the stock ones: a patched copy or a cached
  # libcxx from an earlier build would hide the condition.
  if env -u ZIG_LIB_DIR ZIG_GLOBAL_CACHE_DIR="$work/global" ZIG_LOCAL_CACHE_DIR="$work/local" \
      "$zig" c++ -target aarch64-macos.13.0 -std=c++20 -c "$work/probe.cpp" -o "$work/probe.o" >"$log" 2>&1; then
    result=fixed
  elif grep -q "undeclared identifier 'INFINITY'" "$log"; then
    result=broken
  else
    result=unknown
  fi
  if [[ -n "${MEWLA_ZIG_PROBE_LOG:-}" ]]; then
    cp "$log" "$MEWLA_ZIG_PROBE_LOG" 2>/dev/null || true
  fi
  rm -rf "$work"
  echo "$result"
}

# mewla_patched_zig_lib ZIG_BIN LOCK ROOT TOOL_CACHE prints a lib dir for ZIG_LIB_DIR:
# a copy of ZIG_BIN's own with every patch of the lock's zig.lib_patches
# applied. A patch must match the Zig version, the file it has in the lock and
# the lib dir's current text, or this fails: a stale patch is not skipped. The
# copy is named for the Zig version and the patches' checksums, built aside and
# renamed into place, so an interrupted build is never used.
mewla_patched_zig_lib() {
  local zig="$1" lock="$2" root="$3" tool_cache="$4"
  local version plan
  version="$("$zig" version)"
  # First line: the copy's name. Then one "path<TAB>sha256" per patch.
  if ! plan="$(python3 - "$lock" "$root" "$version" <<'PY'
import hashlib, json, pathlib, sys
lock = json.load(open(sys.argv[1]))
root = pathlib.Path(sys.argv[2]).resolve()
version = sys.argv[3]
patches = lock["zig"].get("lib_patches") or []
if not patches:
    sys.exit("native.lock.json has no zig.lib_patches")
digest = hashlib.sha256()
rows = []
for patch in patches:
    want = patch["applies_to_zig_version"]
    if version != want and not version.startswith(want + "."):
        sys.exit(f"{patch['path']} is for Zig {want}, not {version}")
    path = (root / patch["path"]).resolve()
    if not path.is_relative_to(root) or not path.is_file():
        sys.exit(f"missing Zig lib patch {patch['path']}")
    actual = hashlib.sha256(path.read_bytes()).hexdigest()
    if actual != patch["sha256"]:
        sys.exit(f"{patch['path']} has sha256 {actual}, the lock says {patch['sha256']}")
    digest.update(actual.encode())
    rows.append(f"{path}\t{actual}")
print(f"zig-lib-{version}-{digest.hexdigest()[:12]}")
print("\n".join(rows))
PY
  )"; then
    echo "error: cannot prepare the patched Zig lib dir (see above)" >&2
    return 1
  fi
  local name
  name="$(head -n1 <<<"$plan")"

  local stock
  stock="$(env -u ZIG_LIB_DIR "$zig" env | sed -n 's/^ *\.lib_dir = "\(.*\)",$/\1/p')"
  if [[ -z "$stock" || ! -d "$stock/libcxx" ]]; then
    echo "error: cannot find Zig's lib dir with libcxx (zig env gave '$stock')" >&2
    return 1
  fi

  local dest="$tool_cache/$name"
  if [[ -f "$dest/.mewla-lib-patched" ]]; then
    echo "$dest"
    return
  fi
  local partial="$dest.partial.$$"
  rm -rf "$partial"
  mkdir -p "$tool_cache"
  echo "Preparing a patched copy of Zig's lib dir: $dest" >&2
  # APFS clones are free; elsewhere it is a plain copy.
  if [[ "$(uname -s)" == Darwin ]]; then
    cp -cR "$stock" "$partial"
  else
    cp -R "$stock" "$partial"
  fi
  local path sha
  while IFS=$'\t' read -r path sha; do
    [[ -n "$path" ]] || continue
    # --forward refuses a patch that is already applied; -F0 allows no fuzz.
    if ! patch --quiet --forward -F0 -p1 -d "$partial" -i "$path" >&2; then
      echo "error: $path does not apply to $stock; Zig's lib changed under it" >&2
      rm -rf "$partial"
      return 1
    fi
    printf '%s  %s\n' "$sha" "${path##*/}" >>"$partial/.mewla-lib-patched"
  done < <(tail -n +2 <<<"$plan")
  rm -rf "$dest"
  mv "$partial" "$dest"
  echo "$dest"
}
