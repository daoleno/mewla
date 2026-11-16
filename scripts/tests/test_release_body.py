"""scripts/release-body.py: a release's notes, written at release time from its commits."""

import importlib.util
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("release_body", ROOT / "scripts/release-body.py")
release_body = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release_body)


class ReleaseBodyTests(unittest.TestCase):
    def test_the_notes_list_the_changes_how_to_get_it_and_how_to_verify(self):
        cert = release_body.certificate()
        notes = release_body.body("v9.9.2", "v9.9.1", ["Brain answers faster", "Fix a crash"], cert)
        self.assertTrue(notes.startswith("# Mewla 9.9.2\n\n## Changes\n\n- Brain answers faster\n- Fix a crash\n"))
        for marker in (
            "## Get it",
            "Run `mewla update`",
            "mewla-darwin-arm64.tar.gz",
            "mewla-linux-amd64.tar.gz",
            "mewla-linux-arm64.tar.gz",
            "mewla-windows-amd64.zip",
            "mewla-android-arm64-v9.9.2.apk",
            "unknown sources",
            "/compare/v9.9.1...v9.9.2",
            "SHA256SUMS",
            f"```text\n{cert}\n```",
        ):
            self.assertIn(marker, notes)

    def test_release_preparation_commits_are_not_changes(self):
        self.assertTrue(release_body.PREPARE.match("Prepare v0.4.10"))
        self.assertFalse(release_body.PREPARE.match("Prepare the release notes"))
        self.assertIn("- Maintenance release.", release_body.body("v9.9.2", "v9.9.1", [], "AA"))


if __name__ == "__main__":
    unittest.main()
