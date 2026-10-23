"""The Zig/Xcode workaround retires itself: its check fails once Xcode stops needing it."""

import os
from pathlib import Path
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
CHECK = ROOT / "scripts/check-zig-xcode-compat.sh"
LIBRARY = ROOT / "scripts/lib/zig-xcode-compat.sh"


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

    def check(self, targets):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            sdk = sdk_with_targets(root, targets)
            bin_dir = root / "bin"
            bin_dir.mkdir()
            xcrun = bin_dir / "xcrun"
            xcrun.write_text(f'#!/bin/sh\necho "{sdk}"\n')
            xcrun.chmod(0o755)
            env = {"PATH": f"{bin_dir}:{os.environ['PATH']}"}
            return subprocess.run(["bash", str(CHECK)], capture_output=True, text=True, env=env)

    def test_the_sdk_layouts_are_told_apart(self):
        self.assertEqual(self.support("x86_64-macos, arm64e-macos"), "arm64e-only")
        self.assertEqual(self.support("x86_64-macos, arm64-macos, arm64e-macos"), "native")
        self.assertEqual(self.support("x86_64-macos"), "unknown")
        self.assertEqual(self.support(None), "unknown")

    def test_check_passes_while_the_workaround_is_needed(self):
        result = self.check("x86_64-macos, arm64e-macos")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("still needed", result.stdout)

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

    def test_the_build_script_and_the_check_share_one_predicate(self):
        build = (ROOT / "scripts/build-libghostty-ios.sh").read_text()
        self.assertIn("mewla_libsystem_arm64_support", build)
        self.assertIn("lib/zig-xcode-compat.sh", build)


if __name__ == "__main__":
    unittest.main()
