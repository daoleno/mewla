"""scripts/terminal-corpus/changed.sh: CI runs the terminal corpus only when the commits under test touch the terminal."""

import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]


class TerminalCorpusChangedTests(unittest.TestCase):
    def run_changed(self, repo, event):
        env = {**os.environ, "GITHUB_EVENT_PATH": str(event) if event else ""}
        env.pop("GITHUB_OUTPUT", None)
        result = subprocess.run(["bash", str(repo / "scripts/terminal-corpus/changed.sh")], env=env, capture_output=True, text=True, check=True)
        return result.stdout.strip()

    def test_changed_sh_compares_with_the_events_base(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo = Path(tmp) / "repo"
            (repo / "scripts/terminal-corpus").mkdir(parents=True)
            for name in ("changed.sh", "paths.txt"):
                shutil.copy(ROOT / "scripts/terminal-corpus" / name, repo / "scripts/terminal-corpus" / name)

            def git(*args):
                return subprocess.run(["git", "-c", "user.name=t", "-c", "user.email=t@t", *args], cwd=repo, capture_output=True, text=True, check=True).stdout.strip()

            def commit(path):
                (repo / path).parent.mkdir(parents=True, exist_ok=True)
                (repo / path).write_text(path + os.urandom(4).hex())
                git("add", "-A")
                git("commit", "-q", "-m", path)
                return git("rev-parse", "HEAD")

            git("init", "-q")
            base = commit("docs/a.md")
            event = Path(tmp) / "event.json"

            def verdict(before, pull_request=False):
                event.write_text(json.dumps({"pull_request": {"base": {"sha": before}}} if pull_request else {"before": before}))
                return self.run_changed(repo, event)

            commit("client/ui/chat/Chat.tsx")
            self.assertEqual(verdict(base), "run=false")
            touched = commit("client/ui/terminal/TerminalView.tsx")
            self.assertEqual(verdict(base), "run=true")
            self.assertEqual(verdict(base, pull_request=True), "run=true")
            commit("docs/b.md")
            self.assertEqual(verdict(touched), "run=false", "only the commits after the base count")
            # No event, no base, or a first push (all-zero before): when in doubt it runs.
            self.assertEqual(self.run_changed(repo, None), "run=true")
            self.assertEqual(verdict("0" * 40), "run=true")
            self.assertEqual(verdict("f" * 40), "run=true", "a base that cannot be had runs it")


if __name__ == "__main__":
    unittest.main()
