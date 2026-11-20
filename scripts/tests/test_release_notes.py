import contextlib
import io
import json
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "scripts" / "lib"))
import release_notes  # noqa: E402


def highlight(version: str, name: str = "thing", **extra) -> dict:
    item = {
        "id": f"{version}-{name}",
        "title": "Watch agents work live",
        "line": "Open Screen and see the agent's window.",
        "media": {"scene": "agent-windows"},
        "try": {"kind": "open", "target": "screen"},
    }
    item.update(extra)
    return item


class ReleaseNotesTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="mewla-release-notes-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / "release-notes").mkdir()

    def write(self, name: str, **data) -> None:
        body = {"version": name, "date": "2026-11-16", "highlights": [], "fixes": ["A fix."]}
        body.update(data)
        (self.root / "release-notes" / f"{name}.json").write_text(json.dumps(body), encoding="utf-8")

    def run_main(self, *args: str) -> int:
        return release_notes.main(["--repo", str(self.root), *args])

    def test_the_repository_notes_are_valid_and_current(self):
        self.assertEqual(release_notes.main(["--repo", str(ROOT), "--check"]), 0)

    def test_writes_both_bundles_newest_first_and_checks_them(self):
        self.write("0.4.8", highlights=[highlight("0.4.8")])
        self.write("0.4.10")
        self.write("0.4.10-beta.1")
        self.assertEqual(self.run_main("--check"), 1)
        self.assertEqual(self.run_main(), 0)
        self.assertEqual(self.run_main("--check", "--require", "0.4.10"), 0)
        for relative in release_notes.BUNDLES:
            bundle = json.loads((self.root / relative).read_text(encoding="utf-8"))
            self.assertEqual([r["version"] for r in bundle["releases"]], ["0.4.10", "0.4.10-beta.1", "0.4.8"])
        self.assertEqual(self.run_main("--check", "--require", "0.4.11"), 1)

    def test_a_missing_version_is_refused_in_one_sentence(self):
        self.write("0.4.10")
        self.assertEqual(self.run_main(), 0)
        stderr = io.StringIO()
        with contextlib.redirect_stderr(stderr):
            self.assertEqual(self.run_main("--check", "--require", "0.4.11"), 1)
        self.assertEqual(stderr.getvalue(), f"error: {release_notes.missing('0.4.11')}\n")
        self.assertIn("release-notes/0.4.11.json", release_notes.missing("0.4.11"))

    def test_rejects_what_the_sheet_cannot_show(self):
        cases = {
            "long title": highlight("0.5.0", title="One two three four five six seven"),
            "version in title": highlight("0.5.0", title="New in 0.5.0"),
            "ellipsis": highlight("0.5.0", line="Open Screen and…"),
            "two lines": highlight("0.5.0", line="One\nTwo"),
            "bad id": highlight("0.5.0", id="other-thing"),
            "unknown target": highlight("0.5.0", **{"try": {"kind": "open", "target": "nowhere"}}),
            "image media": highlight("0.5.0", media={"image": "x.png"}),
            "unknown key": highlight("0.5.0", note="engineering detail"),
        }
        for name, item in cases.items():
            with self.subTest(name=name):
                self.write("0.5.0", highlights=[item])
                self.assertEqual(self.run_main(), 1)

    def test_a_highlight_id_is_used_once_and_the_file_names_its_version(self):
        self.write("0.5.0", highlights=[highlight("0.5.0"), highlight("0.5.0")])
        self.assertEqual(self.run_main(), 1)
        self.write("0.5.0", version="0.5.1")
        self.assertEqual(self.run_main(), 1)

    def test_highlights_are_optional_but_a_release_says_something(self):
        self.write("0.5.0", highlights=[], fixes=[])
        self.assertEqual(self.run_main(), 1)
        self.write("0.5.0", highlights=[], fixes=["Sessions open faster."])
        self.assertEqual(self.run_main(), 0)


if __name__ == "__main__":
    unittest.main()
