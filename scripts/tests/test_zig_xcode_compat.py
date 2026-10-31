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


def fake_zig(directory: Path, lib_dir: Path, compile_result: str, release_result: str | None = None) -> Path:
    """A zig that answers version, env, build-exe (the Debug probe) and build-lib (the ReleaseFast one), as the real one would for the cases the scripts tell apart.

    release_result, when given, is what the ReleaseFast build-lib says while build-exe says compile_result.
    Every build's arguments are appended to zig-args.log next to the fake."""
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
        f'  build-exe) echo "$@" >>"$(dirname "$0")/zig-args.log"; {messages[compile_result]} ;;\n'
        f'  build-lib) echo "$@" >>"$(dirname "$0")/zig-args.log"; {messages[release_result or compile_result]} ;;\n'
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
    (sdk / "usr/include").mkdir()
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

    def check(self, targets, libcxx="infinity", header_tail=HEADER_TAIL, release=None):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            sdk = sdk_with_targets(root, targets)
            bin_dir = root / "bin"
            bin_dir.mkdir()
            xcrun = bin_dir / "xcrun"
            xcrun.write_text(f'#!/bin/sh\necho "{sdk}"\n')
            xcrun.chmod(0o755)
            xcodebuild = bin_dir / "xcodebuild"
            xcodebuild.write_text('#!/bin/sh\necho "Xcode 27.0"\necho "Build version TESTBUILD"\n')
            xcodebuild.chmod(0o755)
            zig = fake_zig(bin_dir, fake_lib(root, header_tail), libcxx, release)
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
        # A Zig that fixed it itself has changed the line the patch is for.
        result = self.check("x86_64-macos, arm64e-macos", libcxx="ok", header_tail=HEADER_TAIL.replace("INFINITY", "HUGE_VALF"))
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

    def test_a_probe_that_compiles_the_unfixed_header_is_not_believed(self):
        # The stock header still has the bare INFINITY the patch exists for, so
        # a probe that builds is not reproducing the failure: no "retired".
        result = self.check("x86_64-macos, arm64e-macos", libcxx="ok")
        self.assertEqual(result.returncode, 2)
        self.assertIn("probe and Zig's header disagree", result.stderr)
        self.assertNotIn("libcxx workaround is retired", result.stderr)

    def test_one_configuration_that_fails_is_enough_to_need_the_workaround(self):
        # The real Ghostty build failed in ReleaseFast where a Debug `zig c++`
        # probe passed: any configuration failing on INFINITY means "needed".
        result = self.check("x86_64-macos, arm64e-macos", libcxx="ok", release="infinity")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("libcxx workaround: still needed", result.stdout)
        self.assertIn("libcxx probe variant debug-exe: builds", result.stdout)
        self.assertIn("libcxx probe variant releasefast-dylib: INFINITY", result.stdout)

    def test_the_probe_builds_libcxx_the_way_ghostty_does(self):
        # Ghostty's apple-sdk package gives every library a --libc file naming
        # the Xcode SDK; without it Zig compiles libcxx against its bundled
        # headers, which define INFINITY, so a probe lacking it can only say
        # "builds". Check what the probe really passes.
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            sdk = sdk_with_targets(root, "x86_64-macos, arm64e-macos")
            zig = fake_zig(root, fake_lib(root), "ok")
            out = subprocess.run(
                ["bash", "-c", f'source "{LIBRARY}"; MEWLA_ZIG_PROBE_LOG="{root}/probe.log" mewla_zig_libcxx_support "{zig}" "{sdk}"'],
                capture_output=True, text=True, check=True, env={**os.environ, "TMPDIR": str(root)},
            )
            self.assertEqual(out.stdout.strip(), "fixed")
            lines = (root / "zig-args.log").read_text().splitlines()
            self.assertEqual(len(lines), 2, lines)
            debug, release = lines
            self.assertIn("-dynamic -O ReleaseFast -target aarch64-macos.13.0 -lc++", release)
            self.assertIn("-O Debug -target aarch64-macos.13.0 -lc++", debug)
            for line in lines:
                tokens = line.split()
                libc_file = Path(tokens[tokens.index("--libc") + 1])
                # the file was removed with the probe's scratch; its text is in the log
                self.assertEqual(libc_file.name, "libc.txt")
            log = (root / "probe.log").read_text()
            self.assertIn(f"include_dir={sdk}/usr/include", log)
            self.assertIn(f"sys_include_dir={sdk}/usr/include", log)

    def test_a_probe_with_no_sdk_to_name_does_not_claim_the_workaround_is_retired(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            zig = fake_zig(root, fake_lib(root), "ok")
            out = subprocess.run(
                ["bash", "-c", f'source "{LIBRARY}"; mewla_zig_libcxx_support "{zig}" ""'],
                capture_output=True, text=True, check=True, env={**os.environ, "TMPDIR": str(root)},
            )
            self.assertEqual(out.stdout.strip(), "unknown")

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

    def test_a_hand_patched_zig_is_named_not_mistaken_for_a_fixed_one(self):
        patched = HEADER_TAIL.replace("INFINITY", "numeric_limits<_RealT>::infinity()")
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            lib = fake_lib(root, patched)
            result = self.patched_lib(root, lib)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("already patched", result.stderr)
            # The check can't read a patched Zig as "compiles, so retired".
            bin_dir = root / "bin"
            bin_dir.mkdir()
            sdk = sdk_with_targets(root, "x86_64-macos, arm64e-macos")
            xcrun = bin_dir / "xcrun"
            xcrun.write_text(f'#!/bin/sh\necho "{sdk}"\n')
            xcrun.chmod(0o755)
            zig = fake_zig(bin_dir, lib, "ok")
            env = {"PATH": f"{bin_dir}:{os.environ['PATH']}", "ZIG_BIN": str(zig), "MEWLA_TOOL_CACHE": str(root / "tools"), "TMPDIR": str(root)}
            check = subprocess.run(["bash", str(CHECK)], capture_output=True, text=True, env=env)
            self.assertEqual(check.returncode, 2)
            self.assertIn("already carries the libcxx patch", check.stderr)

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

    def state(self, lib, patch=PATCH):
        return subprocess.run(
            ["bash", "-c", f'source "{LIBRARY}"; mewla_zig_lib_patch_state "{lib}" "{patch}"'],
            capture_output=True, text=True, check=True,
        ).stdout.strip()

    def test_the_patch_state_is_read_from_the_text_not_from_patch(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.assertEqual(self.state(fake_lib(root)), "unpatched")
            patched = HEADER_TAIL.replace("INFINITY", "numeric_limits<_RealT>::infinity()")
            b = root / "b"
            b.mkdir()
            self.assertEqual(self.state(fake_lib(b, patched)), "applied")
            other = root / "c"
            other.mkdir()
            self.assertEqual(self.state(fake_lib(other, HEADER_TAIL.replace("INFINITY", "HUGE_VALF"))), "mismatch")
            self.assertEqual(self.state(root / "missing"), "mismatch")

    def test_patch_prompting_to_ignore_reverse_does_not_matter(self):
        """Apple's patch says "Unreversed patch detected! Ignore -R?" and exits 0 on an unpatched tree.

        A stand-in for that patch must not make the stock header look patched,
        nor stop the patch from being applied."""
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            real = shutil.which("patch")
            bin_dir = root / "bin"
            bin_dir.mkdir()
            apple = bin_dir / "patch"
            apple.write_text(
                "#!/bin/sh\n"
                'for a in "$@"; do if [ "$a" = --reverse ] || [ "$a" = -R ]; then '
                'echo "Unreversed (or previously applied) patch detected!  Ignore -R? [y]" >&2; exit 0; fi; done\n'
                f'exec {real} "$@"\n'
            )
            apple.chmod(0o755)
            lib = fake_lib(root)
            zig = fake_zig(root, lib, "ok")
            env = {**os.environ, "PATH": f"{bin_dir}:{os.environ['PATH']}"}
            already = subprocess.run(
                ["bash", "-c", f'source "{LIBRARY}"; mewla_zig_lib_already_patched "{zig}" "{LOCK}" "{ROOT}"'],
                capture_output=True, text=True, env=env,
            )
            self.assertNotEqual(already.returncode, 0, "the stock header was taken for a patched one")
            built = subprocess.run(
                ["bash", "-c", f'source "{LIBRARY}"; mewla_patched_zig_lib "{zig}" "{LOCK}" "{ROOT}" "{root / "tools"}"'],
                capture_output=True, text=True, env=env,
            )
            self.assertEqual(built.returncode, 0, built.stderr)

    def test_the_lock_names_a_patch_that_is_the_file_in_the_tree(self):
        import hashlib
        patches = json.loads(LOCK.read_text())["zig"]["lib_patches"]
        self.assertEqual([p["sha256"] for p in patches], [hashlib.sha256(PATCH.read_bytes()).hexdigest()])
        self.assertIn(f"--- a/{patches[0]['target']}", PATCH.read_text())

    def test_the_build_script_and_the_check_share_one_predicate(self):
        build = (ROOT / "scripts/build-libghostty-ios.sh").read_text()
        # The SDK clone lives in the library, so the check's probe builds against the same SDK.
        self.assertIn("mewla_prepare_macos_sdk_for_zig", build)
        self.assertIn("lib/zig-xcode-compat.sh", build)
        self.assertIn("mewla_prepare_macos_sdk_for_zig", CHECK.read_text())
        self.assertIn("mewla_libsystem_arm64_support", LIBRARY.read_text())

    def test_the_build_uses_the_patched_lib_and_records_it(self):
        build = (ROOT / "scripts/build-libghostty-ios.sh").read_text()
        self.assertIn("mewla_patched_zig_lib", build)
        self.assertIn('ZIG_LIB_DIR="$ZIG_LIB"', build)
        self.assertIn('"zig_lib_patches"', build)
        verify = (ROOT / "scripts/verify-libghostty-ios.sh").read_text()
        self.assertIn("zig_lib_patches", verify)

    def test_the_ios_cache_key_covers_what_the_workarounds_are_made_of(self):
        action = (ROOT / ".github/actions/ios-native/action.yml").read_text()
        for name in ("scripts/lib/zig-xcode-compat.sh", "scripts/lib/zig_lib_patch_state.py", "scripts/zig-xcode-compat/**", "scripts/build-libghostty-ios.sh"):
            self.assertGreaterEqual(action.count(name), 2, f"{name} must be in both cache keys")


if __name__ == "__main__":
    unittest.main()
