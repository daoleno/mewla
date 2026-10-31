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
_MEWLA_COMPAT_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

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

# mewla_prepare_macos_sdk_for_zig TOOL_CACHE prints the macOS SDK Zig should
# use (the build sets MEWLA_MACOS_SDKROOT to it for scripts/zig-xcode-compat/xcrun):
# the SDK xcrun names when its libSystem stub lists arm64, otherwise a
# copy-on-write clone of it with arm64 added. Xcode itself is never touched.
mewla_prepare_macos_sdk_for_zig() {
  local tool_cache="$1" sdk
  sdk="$(realpath "$(xcrun --sdk macosx --show-sdk-path)")"
  local libsystem="$sdk/usr/lib/libSystem.B.tbd"

  case "$(mewla_libsystem_arm64_support "$sdk")" in
    native)
      echo "$sdk"
      return
      ;;
    arm64e-only) ;;
    *)
      echo "error: unsupported libSystem target list in $libsystem" >&2
      return 1
      ;;
  esac

  # Xcode 26.6's macOS 26.5 SDK advertises the top-level libSystem stub as
  # arm64e-only. Apple's linker accepts it for arm64 hosts, while Zig 0.15.2
  # intentionally requires an exact target match. Give Zig a clone with arm64
  # added to those target lists.
  local xcode_build
  xcode_build="$(xcodebuild -version | awk '/Build version/ {print $3}')"
  local compat_sdk="$tool_cache/macos-sdk-$xcode_build-arm64"
  local marker="$compat_sdk/.zig-arm64-libsystem"
  if [[ ! -f "$marker" ]]; then
    echo "Preparing Zig-compatible macOS SDK clone for Xcode $xcode_build..." >&2
    rm -rf "$compat_sdk"
    mkdir -p "$(dirname "$compat_sdk")"
    if [[ "$(uname -s)" == Darwin ]]; then
      cp -cR "$sdk" "$compat_sdk"
    else
      cp -R "$sdk" "$compat_sdk"
    fi
    python3 - "$compat_sdk/usr/lib/libSystem.B.tbd" <<'PY'
import pathlib, sys
path = pathlib.Path(sys.argv[1])
text = path.read_text()
text = text.replace("arm64e-macos", "arm64-macos, arm64e-macos")
text = text.replace("arm64e-maccatalyst", "arm64-maccatalyst, arm64e-maccatalyst")
path.write_text(text)
PY
    touch "$marker"
  fi
  echo "$compat_sdk"
}

# mewla_zig_stock_lib ZIG_BIN prints the lib dir Zig itself uses.
mewla_zig_stock_lib() {
  env -u ZIG_LIB_DIR "$1" env | sed -n 's/^ *\.lib_dir = "\(.*\)",$/\1/p'
}

# mewla_lock_lib_patches LOCK ROOT prints the absolute path of each patch in
# the lock's zig.lib_patches, one per line.
mewla_lock_lib_patches() {
  python3 -c 'import json, pathlib, sys
for patch in json.load(open(sys.argv[1]))["zig"].get("lib_patches") or []:
    print(pathlib.Path(sys.argv[2]) / patch["path"])' "$1" "$2"
}

# mewla_zig_lib_patch_state LIB PATCH prints unpatched, applied or mismatch by
# reading the text the patch changes. It does not ask `patch`: Apple's patch
# 2.0-12u11 (macOS) answers "Unreversed (or previously applied) patch
# detected! Ignore -R? [y]" with yes on an unpatched tree, and so takes the
# stock header for a patched one.
mewla_zig_lib_patch_state() {
  python3 -I "$_MEWLA_COMPAT_LIB_DIR/zig_lib_patch_state.py" "$1" "$2"
}

# mewla_zig_lib_already_patched ZIG_BIN LOCK ROOT succeeds when Zig's own lib
# dir already carries the lock's lib patches, as after a hand patch from
# before the build did this itself. The build would then fail to apply them
# and the libcxx probe would call the workaround retired; the fix is a Zig
# unpacked afresh from the pinned tarball.
mewla_zig_lib_already_patched() {
  local zig="$1" lock="$2" root="$3" stock path found=1
  stock="$(mewla_zig_stock_lib "$zig")"
  [[ -d "$stock" ]] || return 1
  while IFS= read -r path; do
    [[ -n "$path" ]] || continue
    [[ "$(mewla_zig_lib_patch_state "$stock" "$path")" == applied ]] || return 1
    found=0
  done < <(mewla_lock_lib_patches "$lock" "$root")
  return "$found"
}

# mewla_zig_libcxx_support ZIG_BIN [SDKROOT] prints one word, from building a
# C++ program for the macOS target with the stock Zig lib dir. Building or
# linking a C++ program is what makes Zig build its own libcxx, and the answer
# comes from that libcxx build. It is built the ways that matter, each with
# empty caches, because a single configuration once said "fixed" where the real
# Ghostty build failed (Zig builds libcxx per optimize mode, so one mode's
# success says nothing about another's):
#   debug-exe          `zig c++`, Debug: libcxx built Debug, with the UBSan runtime
#   releasefast-dylib  `zig build-lib -dynamic -O ReleaseFast -lc++`: what
#                      Ghostty's "compile lib ghostty-vt ReleaseFast
#                      aarch64-macos.13.0" does; the libcxx sub-compilation
#                      that failed on INFINITY is the ReleaseFast one
# and the result is:
#   broken   libcxx fails on INFINITY in any of them: the condition the
#            workaround exists for
#   fixed    every one builds; the libcxx workaround is no longer needed
#   unknown  one fails some other way and none on INFINITY
# SDKROOT is the SDK from mewla_prepare_macos_sdk_for_zig; without it Zig finds
# the SDK itself. $MEWLA_ZIG_PROBE_LOG, when set, gets one "== variant NAME:
# RESULT" line per configuration followed by that configuration's compiler
# output, so a Mac run shows which configuration builds and which does not.
mewla_zig_libcxx_support() {
  local zig="$1" sdkroot="${2:-}"
  local work
  work="$(mktemp -d "${TMPDIR:-/tmp}/mewla-zig-probe.XXXXXX")" || { echo unknown; return; }
  printf '#include <random>\nint main() { std::mt19937 g(1); return static_cast<int>(g() & 1) * 0; }\n' >"$work/probe.cpp"
  local combined="$work/probe.log" overall=fixed saw_other=0 name result
  : >"$combined"
  local -a probe_env=(env -u ZIG_LIB_DIR)
  if [[ -n "$sdkroot" ]]; then
    probe_env+=(PATH="$_MEWLA_COMPAT_LIB_DIR/../zig-xcode-compat:$PATH" MEWLA_MACOS_SDKROOT="$sdkroot")
  fi
  for name in debug-exe releasefast-dylib; do
    local -a command
    case "$name" in
      debug-exe)
        command=("$zig" c++ -target aarch64-macos.13.0 -std=c++20 "$work/probe.cpp" -o "$work/$name.out") ;;
      releasefast-dylib)
        command=("$zig" build-lib -dynamic -O ReleaseFast -target aarch64-macos.13.0 -lc++
                 "$work/probe.cpp" "-femit-bin=$work/$name.dylib") ;;
    esac
    # One cache per optimize mode: libcxx is built per mode, never reused across modes.
    local cache="$work/cache-${name%%-*}"
    local log="$work/$name.log"
    if "${probe_env[@]}" ZIG_GLOBAL_CACHE_DIR="$cache/global" ZIG_LOCAL_CACHE_DIR="$cache/local" "${command[@]}" >"$log" 2>&1; then
      result=builds
    elif grep -q "undeclared identifier 'INFINITY'" "$log"; then
      result=INFINITY
      overall=broken
    else
      result=other-failure
      saw_other=1
    fi
    { echo "== variant $name: $result"; cat "$log"; } >>"$combined"
  done
  if [[ "$overall" != broken && "$saw_other" == 1 ]]; then
    overall=unknown
  fi
  if [[ -n "${MEWLA_ZIG_PROBE_LOG:-}" ]]; then
    cp "$combined" "$MEWLA_ZIG_PROBE_LOG" 2>/dev/null || true
  fi
  rm -rf "$work"
  echo "$overall"
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
  stock="$(mewla_zig_stock_lib "$zig")"
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
    # Decide from the text, not from patch's exit code or prompts: only an
    # unpatched file is patched, and the result is read back afterwards.
    case "$(mewla_zig_lib_patch_state "$partial" "$path")" in
      unpatched) ;;
      applied)
        echo "error: $stock is already patched (a hand patch?). Remove that Zig and let the build unpack the pinned one afresh; also unset ZIG_LIB_DIR and clear any Zig cache built with the hand patch." >&2
        rm -rf "$partial"
        return 1
        ;;
      *)
        echo "error: $path does not apply to $stock; Zig's lib changed under it" >&2
        rm -rf "$partial"
        return 1
        ;;
    esac
    if ! patch --quiet --forward -F0 -p1 -d "$partial" -i "$path" >&2 \
        || [[ "$(mewla_zig_lib_patch_state "$partial" "$path")" != applied ]]; then
      echo "error: $path did not apply to $stock ($(patch --version 2>&1 | head -n1))" >&2
      rm -rf "$partial"
      return 1
    fi
    printf '%s  %s\n' "$sha" "${path##*/}" >>"$partial/.mewla-lib-patched"
  done < <(tail -n +2 <<<"$plan")
  rm -rf "$dest"
  mv "$partial" "$dest"
  echo "$dest"
}
