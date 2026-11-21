"""scripts/released.py: what a public release carries, and that the release track uploads exactly that."""

import importlib.util
import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("released", ROOT / "scripts/released.py")
released = importlib.util.module_from_spec(spec)
spec.loader.exec_module(released)

RELEASE = (ROOT / ".github/workflows/release-artifacts.yml").read_text(encoding="utf-8")
MAC_FILES = [
    "Mewla-0.4.19-macos-aarch64.zip",
    "Mewla-0.4.19-macos-arm64.dmg",
    "Mewla-macos-arm64.dmg",
    "latest.json",
    "latest.json.sig",
]


class ReleasedAssetsTests(unittest.TestCase):
    def test_a_stable_release_carries_the_mac_files_and_a_beta_does_not(self):
        stable = released.assets("0.4.19", stable=True)
        beta = released.assets("0.4.19", stable=False)
        for name in MAC_FILES:
            self.assertIn(name, stable)
            self.assertNotIn(name, beta)
        self.assertEqual(sorted(set(stable) - set(beta)), sorted(MAC_FILES))
        self.assertEqual(len(beta), 8)
        self.assertEqual(len(stable), 13)

    def test_publish_uploads_the_files_released_checks_for(self):
        # The Mac files of publish's asset list are the ones released.py requires, version for version.
        block = RELEASE[RELEASE.index("mac=(\n"):]
        block = block[: block.index("\n            )")]
        listed = re.findall(r'"?(Mewla[^"\n ]*|latest\.json(?:\.sig)?)"?\s*$', block, flags=re.M)
        listed = [name.replace("${VERSION}", "0.4.19") for name in listed]
        self.assertEqual(sorted(listed), sorted(MAC_FILES))
        self.assertEqual(sorted(listed), sorted(set(released.assets("0.4.19")) - set(released.assets("0.4.19", stable=False))))


if __name__ == "__main__":
    unittest.main()
