"""Every host builds on one official NSH release (longbridge/nsh), never a fork: the CLI, the Android AARs and the iOS
XCFrameworks that client/scripts/fetch-nsh.sh installs, and the crates client/macos/Cargo.toml (and its lock) builds."""

import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


class NshPinTest(unittest.TestCase):
    def test_one_official_release_for_every_host(self):
        fetch = (ROOT / "client/scripts/fetch-nsh.sh").read_text()
        version = re.search(r'^version="v(\d+\.\d+\.\d+)"$', fetch, re.M)
        self.assertIsNotNone(version, 'fetch-nsh.sh pins the release as version="vX.Y.Z"')
        version = version.group(1)
        self.assertIn('base="https://github.com/longbridge/nsh/releases/download/$version"', fetch)
        # Each archive comes from that release, checked against a pinned checksum; nothing from anywhere else.
        for archive in ("nsh-linux-x86_64.tar.gz", "nsh-macos-arm64.tar.gz", "nsh-embed-android.zip", "nsh-embed-ios.zip"):
            self.assertRegex(fetch, rf"fetch {re.escape(archive)} [0-9a-f]{{64}}\)")
        self.assertEqual(re.findall(r"https://[^\s\"']+", fetch), ["https://github.com/longbridge/nsh/releases/download/$version"])

        cargo = (ROOT / "client/macos/Cargo.toml").read_text()
        self.assertNotIn("[patch", cargo, "client/macos/Cargo.toml patches no crate")
        self.assertNotIn("git =", cargo, "client/macos/Cargo.toml takes no crate from a repository")
        pinned = re.findall(r'^(nsh-[a-z-]+) = (?:"=([^"]+)"|\{ version = "=([^"]+)")', cargo, re.M)
        self.assertTrue(pinned, "client/macos/Cargo.toml depends on NSH's crates")
        for name, plain, table in pinned:
            self.assertEqual(plain or table, version, f"{name} is the release fetch-nsh.sh installs")

        lock = (ROOT / "client/macos/Cargo.lock").read_text()
        packages = re.findall(r'\[\[package\]\]\nname = "(nsh-[a-z-]+)"\nversion = "([^"]+)"\nsource = "([^"]+)"', lock)
        self.assertTrue(packages, "client/macos/Cargo.lock resolves NSH's crates")
        for name, locked, source in packages:
            self.assertEqual(locked, version, f"{name} resolves to the release")
            self.assertEqual(source, "registry+https://github.com/rust-lang/crates.io-index", f"{name} comes from crates.io")


if __name__ == "__main__":
    unittest.main()
