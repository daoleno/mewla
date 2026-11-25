"""scripts/check-identity.py: every commit is daoleno's, with no Co-authored-by trailer."""

import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "scripts/check-identity.py"


class CheckIdentityTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.repo = self.tmp.name
        self.git("init", "-q", "-b", "main")

    def tearDown(self):
        self.tmp.cleanup()

    def git(self, *args, name="daoleno", email="daodaoonline@gmail.com", committer=None):
        cn, ce = committer or (name, email)
        env = {**os.environ, "GIT_CONFIG_GLOBAL": os.devnull, "GIT_CONFIG_NOSYSTEM": "1",
               "GIT_AUTHOR_NAME": name, "GIT_AUTHOR_EMAIL": email, "GIT_COMMITTER_NAME": cn, "GIT_COMMITTER_EMAIL": ce}
        return subprocess.run(["git", "-C", self.repo, *args], env=env, capture_output=True, text=True, check=True)

    def commit(self, message, **who):
        self.git("commit", "-q", "--allow-empty", "-m", message, **who)

    def check(self):
        return subprocess.run([sys.executable, str(SCRIPT)], cwd=self.repo, capture_output=True, text=True)

    def test_daoleno_and_a_github_web_merge_pass(self):
        self.commit("First")
        self.commit("Merge pull request #1", committer=("GitHub", "noreply@github.com"))
        result = self.check()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("2 commits", result.stdout)

    def test_another_identity_anywhere_in_history_fails(self):
        self.commit("Old", name="paul-freeride", email="paul@freeride-ai.io")
        self.commit("New")
        result = self.check()
        self.assertEqual(result.returncode, 1)
        self.assertIn("author paul-freeride <paul@freeride-ai.io>", result.stdout)
        self.assertIn("committer paul-freeride", result.stdout)

    def test_a_bot_committer_fails(self):
        self.commit("Prepare v1.0.0", committer=("github-actions[bot]", "41898282+github-actions[bot]@users.noreply.github.com"))
        self.assertEqual(self.check().returncode, 1)

    def test_a_co_authored_by_trailer_fails_in_any_case(self):
        self.commit("Change\n\nCo-Authored-By: Claude <noreply@anthropic.com>")
        result = self.check()
        self.assertEqual(result.returncode, 1)
        self.assertIn("Co-authored-by trailer", result.stdout)


if __name__ == "__main__":
    unittest.main()
