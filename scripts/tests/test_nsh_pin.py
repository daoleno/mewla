"""Every host embeds NSH from one commit of the daoleno/nsh line: the Android AARs and iOS XCFrameworks that
client/scripts/fetch-nsh.sh installs, and the crates client/macos/Cargo.toml (and its lock) builds."""

import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


class NshPinTest(unittest.TestCase):
    def test_one_fork_commit_for_every_host(self):
        fetch = (ROOT / "client/scripts/fetch-nsh.sh").read_text()
        fork = re.search(r'^fork="([0-9a-f]{40})"$', fetch, re.M)
        self.assertIsNotNone(fork, "fetch-nsh.sh pins the fork commit as fork=\"<40 hex>\"")
        fork = fork.group(1)
        self.assertIn('embed_base="https://github.com/daoleno/nsh/releases/download/mewla-$fork"', fetch)
        # Both embedding archives come from that commit's release, each checked against a pinned checksum.
        for archive in ("nsh-embed-android.zip", "nsh-embed-ios.zip"):
            self.assertRegex(fetch, rf'fetch {re.escape(archive)} [0-9a-f]{{64}} "\$embed_base"')

        cargo = (ROOT / "client/macos/Cargo.toml").read_text()
        revs = set(re.findall(r'git = "https://github.com/daoleno/nsh", rev = "([0-9a-f]+)"', cargo))
        self.assertEqual(revs, {fork}, "client/macos/Cargo.toml pins the same daoleno/nsh commit")
        lock = (ROOT / "client/macos/Cargo.lock").read_text()
        locked = set(re.findall(r'git\+https://github\.com/daoleno/nsh\?rev=[0-9a-f]+#([0-9a-f]+)', lock))
        self.assertEqual(locked, {fork}, "client/macos/Cargo.lock resolves the same daoleno/nsh commit")

    def test_the_pin_names_what_retires_it(self):
        fetch = (ROOT / "client/scripts/fetch-nsh.sh").read_text()
        self.assertIn("longbridge/nsh#2964", fetch)
        self.assertIn("#3014", fetch)


if __name__ == "__main__":
    unittest.main()
