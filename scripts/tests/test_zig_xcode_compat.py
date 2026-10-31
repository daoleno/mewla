"""The Zig/Xcode workarounds retire themselves: the check fails once Xcode or Zig stops needing one."""

import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
CHECK = ROOT / "scripts/check-zig-xcode-compat.sh"
LIBRARY = ROOT / "scripts/lib/zig-xcode-compat.sh"
LOCK = ROOT / "app/modules/terminal-vt/native.lock.json"
PATCH = ROOT / "scripts/zig-xcode-compat/zig-0.15.2-libcxx-infinity.patch"

# What Zig 0.15.2's libcxx has around the line the patch changes.
HEADER_TAIL = """\
_LIBCPP_HIDE_FROM_ABI _IntT __clamp_to_integral(_RealT __r) _NOEXCEPT {
  using _Lim            = numeric_limits<_IntT>;
  const _IntT __max_val = __max_representable_int_for_float<_IntT, _RealT>();
  if (__r >= ::nextafter(static_cast<_RealT>(__max_val), INFINITY)) {
    return _Lim::max();
  } else if (__r <= _Lim::lowest()) {
    return _Lim::min();
  }
"""
HEADER_PATH = "libcxx/include/__random/clamp_to_integral.h"


def fake_zig(directory: Path, lib_dir: Path, compile_result: str) -> Path:
    """A zig that answers version, env and c++, as the real one would for the cases the scripts tell apart."""
    messages = {
        "ok": "exit 0",
        "infinity": 'echo "clamp_to_integral.h:47:58: note: use of undeclared identifier \'INFINITY\'" >&2; exit 1',
        "other": 'echo "error: something else broke" >&2; exit 1',
    }
    zig = directory / "zig"
    zig.write_text(
        "#!/bin/sh\n"
        'case "$1" in\n'
        "  version) echo 0.15.2 ;;\n"
        f'  env) echo \'.{{ .lib_dir = "{lib_dir}", }}\' | sed \'s/^.{{ //; s/ }}$//\' ;;\n'
        f"  c++) {messages[compile_result]} ;;\n"
        "esac\n"
    )
    zig.chmod(0o755)
    return zig


def fake_lib(directory: Path, header_tail: str = HEADER_TAIL) -> Path:
    lib = directory / "zig-lib"
    header = lib / HEADER_PATH
    header.parent.mkdir(parents=True)
    # The patch is line-numbered; patch finds it wherever it sits, with no fuzz.
    header.write_text("// filler\n" * 40 + header_tail)
    return lib


def sdk_with_targets(parent: Path, targets: str | None) -> Path:
    sdk = parent / "MacOSX.sdk"
    (sdk / "usr/lib").mkdir(parents=True)
    if targets is not None:
        (sdk / "usr/lib/libSystem.B.tbd").write_text(
            f"--- !tapi-tbd\ntbd-version: 4\ntargets: [ {targets} ]\ninstall-name: /usr/lib/libSystem.B.dylib\n"
        )
    return sdk


class ZigXcodeCompatTests(unittest.TestCase):
    def support(self, targets):
        with tempfile.TemporaryDirectory() as directory:
            sdk = sdk_with_targets(Path(directory), targets)
            result = subprocess.run(
                ["bash", "-c", f'source "{LIBRARY}"; mewla_libsystem_arm64_support "{sdk}"'],
                capture_output=True, text=True, check=True,
            )
            return result.stdout.strip()

    def check(self, targets, libcxx="infinity"):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            sdk = sdk_with_targets(root, targets)
            bin_dir = root / "bin"
            bin_dir.mkdir()
            xcrun = bin_dir / "xcrun"
            xcrun.write_text(f'#!/bin/sh\necho "{sdk}"\n')
            xcrun.chmod(0o755)
            zig = fake_zig(bin_dir, fake_lib(root), libcxx)
            env = {"PATH": f"{bin_dir}:{os.environ['PATH']}", "ZIG_BIN": str(zig), "MEWLA_TOOL_CACHE": str(root / "tools"), "TMPDIR": str(root)}
            return subprocess.run(["bash", str(CHECK)], capture_output=True, text=True, env=env)

    def test_the_sdk_layouts_are_told_apart(self):
        self.assertEqual(self.support("x86_64-macos, arm64e-macos"), "arm64e-only")
        self.assertEqual(self.support("x86_64-macos, arm64-macos, arm64e-macos"), "native")
        self.assertEqual(self.support("x86_64-macos"), "unknown")
        self.assertEqual(self.support(None), "unknown")

    def test_check_passes_while_the_workarounds_are_needed(self):
        result = self.check("x86_64-macos, arm64e-macos")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("libSystem workaround: still needed", result.stdout)
        self.assertIn("libcxx workaround: still needed", result.stdout)

    def test_check_fails_and_says_what_to_delete_once_xcode_is_fixed(self):
        result = self.check("x86_64-macos, arm64-macos, arm64e-macos")
        self.assertEqual(result.returncode, 1)
        self.assertIn("retired", result.stderr)
        self.assertIn("scripts/zig-xcode-compat/", result.stderr)
        self.assertIn("prepare_macos_sdk_for_zig", result.stderr)

    def test_check_fails_differently_when_the_layout_changed(self):
        result = self.check(None)
        self.assertEqual(result.returncode, 2)
        self.assertIn("layout changed", result.stderr)

    def test_the_libcxx_workaround_retires_on_its_own(self):
        result = self.check("x86_64-macos, arm64e-macos", libcxx="ok")
        self.assertEqual(result.returncode, 1)
        self.assertIn("libcxx workaround is retired", result.stderr)
        self.assertIn("zig.lib_patches", result.stderr)
        # The other one is still reported as needed, not hidden by the failure.
        self.assertIn("libSystem workaround: still needed", result.stdout)
        self.assertNotIn("libSystem workaround is retired", result.stderr)

    def test_the_libsystem_workaround_retires_without_hiding_libcxx(self):
        result = self.check("x86_64-macos, arm64-macos, arm64e-macos", libcxx="infinity")
        self.assertEqual(result.returncode, 1)
        self.assertIn("libSystem workaround is retired", result.stderr)
        self.assertIn("libcxx workaround: still needed", result.stdout)

    def test_an_unrelated_libcxx_failure_is_not_taken_for_the_known_one(self):
        result = self.check("x86_64-macos, arm64e-macos", libcxx="other")
        self.assertEqual(result.returncode, 2)
        self.assertIn("something else broke", result.stderr)
        self.assertNotIn("libcxx workaround: still needed", result.stdout)

    def patched_lib(self, root, lib, lock=LOCK, cache=None):
        zig = fake_zig(root, lib, "ok")
        cache = cache or root / "tools"
        return subprocess.run(
            ["bash", "-c", f'source "{LIBRARY}"; mewla_patched_zig_lib "{zig}" "{lock}" "{ROOT}" "{cache}"'],
            capture_output=True, text=True,
        )

    def test_the_patched_lib_is_a_copy_and_the_install_is_untouched(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            lib = fake_lib(root)
            first = self.patched_lib(root, lib)
            self.assertEqual(first.returncode, 0, first.stderr)
            patched = Path(first.stdout.strip())
            self.assertNotEqual(patched, lib)
            self.assertIn("numeric_limits<_RealT>::infinity()", (patched / HEADER_PATH).read_text())
            self.assertNotIn("INFINITY", (patched / HEADER_PATH).read_text())
            self.assertIn("INFINITY", (lib / HEADER_PATH).read_text())
            self.assertTrue((patched / ".mewla-lib-patched").is_file())
            # Named for the Zig version and the patch, and reused once built.
            self.assertTrue(patched.name.startswith("zig-lib-0.15.2-"))
            again = self.patched_lib(root, lib)
            self.assertEqual(again.stdout.strip(), str(patched))
            self.assertEqual(list((root / "tools").iterdir()), [patched])

    def test_a_patch_that_no_longer_applies_fails_instead_of_being_skipped(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            lib = fake_lib(root, HEADER_TAIL.replace("INFINITY", "HUGE_VALF"))
            result = self.patched_lib(root, lib)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("does not apply", result.stderr)
            self.assertEqual(list((root / "tools").iterdir()), [], "a failed copy must not be left to be reused")

    def test_a_patch_that_differs_from_the_lock_fails(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            lock = json.loads(LOCK.read_text())
            lock["zig"]["lib_patches"][0]["sha256"] = "0" * 64
            changed = root / "lock.json"
            changed.write_text(json.dumps(lock))
            result = self.patched_lib(root, fake_lib(root), lock=changed)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("the lock says", result.stderr)

    def test_the_lock_names_a_patch_that_is_the_file_in_the_tree(self):
        import hashlib
        patches = json.loads(LOCK.read_text())["zig"]["lib_patches"]
        self.assertEqual([p["sha256"] for p in patches], [hashlib.sha256(PATCH.read_bytes()).hexdigest()])
        self.assertIn(f"--- a/{patches[0]['target']}", PATCH.read_text())

    def test_the_build_script_and_the_check_share_one_predicate(self):
        build = (ROOT / "scripts/build-libghostty-ios.sh").read_text()
        self.assertIn("mewla_libsystem_arm64_support", build)
        self.assertIn("lib/zig-xcode-compat.sh", build)

    def test_the_build_uses_the_patched_lib_and_records_it(self):
        build = (ROOT / "scripts/build-libghostty-ios.sh").read_text()
        self.assertIn("mewla_patched_zig_lib", build)
        self.assertIn('ZIG_LIB_DIR="$ZIG_LIB"', build)
        self.assertIn('"zig_lib_patches"', build)
        verify = (ROOT / "scripts/verify-libghostty-ios.sh").read_text()
        self.assertIn("zig_lib_patches", verify)

    def test_the_ios_cache_key_covers_what_the_workarounds_are_made_of(self):
        action = (ROOT / ".github/actions/ios-native/action.yml").read_text()
        for name in ("scripts/lib/zig-xcode-compat.sh", "scripts/zig-xcode-compat/**", "scripts/build-libghostty-ios.sh"):
            self.assertGreaterEqual(action.count(name), 2, f"{name} must be in both cache keys")


if __name__ == "__main__":
    unittest.main()
