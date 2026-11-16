"""scripts/release-green.py: a release ships a main commit CI passed."""

import importlib.util
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("release_green", ROOT / "scripts/release-green.py")
release_green = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release_green)


class ReleaseGreenTests(unittest.TestCase):
    def test_it_reads_ci_ymls_ignored_paths(self):
        globs = release_green.ignored_globs((ROOT / ".github/workflows/ci.yml").read_text(encoding="utf-8"))
        self.assertEqual(globs, ["docs/**", "site/**", "*.md"])

    def test_globs_follow_githubs_path_filters(self):
        docs, markdown = release_green.glob_regex("docs/**"), release_green.glob_regex("*.md")
        self.assertTrue(docs.match("docs/releases/v1.md"))
        self.assertTrue(markdown.match("CHANGELOG.md"))
        self.assertFalse(markdown.match("client/README.md"))
        self.assertFalse(docs.match("daemon/docs.go"))

    def test_a_green_run_passes_a_running_one_waits_and_anything_else_fails(self):
        green = {"status": "completed", "conclusion": "success"}
        red = {"status": "completed", "conclusion": "failure"}
        running = {"status": "in_progress", "conclusion": None}
        self.assertEqual(release_green.verdict([red, green])[0], "passed")
        self.assertEqual(release_green.verdict([red, running])[0], "running")
        self.assertEqual(release_green.verdict([red])[0], "failed")
        self.assertEqual(release_green.verdict([{"status": "completed", "conclusion": "cancelled"}])[0], "failed")


if __name__ == "__main__":
    unittest.main()
