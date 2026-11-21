"""scripts/ci-local.py must cover every job and step of .github/workflows/ci.yml."""

import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("ci_local", ROOT / "scripts/ci-local.py")
ci_local = importlib.util.module_from_spec(spec)
spec.loader.exec_module(ci_local)


class CILocalCoversCITests(unittest.TestCase):
    def test_every_run_step_of_ci_yml_is_planned_or_skipped_with_a_reason(self):
        # plan() raises on a job, action, condition or ${{ }} expression it does not know.
        planned = ci_local.plan("/tmp/runner")
        workflow = ci_local.load_workflow()
        expected = {
            (job_id, step.get("name") or step.get("uses"))
            for job_id, job in workflow["jobs"].items()
            if job_id not in ci_local.SKIPPED_JOBS
            for step in job["steps"]
            if "run" in step
        }
        seen = {(step.job, step.name) for step in planned}
        self.assertEqual(expected - seen, set(), "ci.yml steps that scripts/ci-local.py does not plan")
        for step in planned:
            self.assertTrue(step.run or step.skip, f"{step.job}/{step.name} has neither a command nor a skip reason")

    def test_every_job_has_a_path_rule_or_a_skip_reason(self):
        jobs = set(ci_local.load_workflow()["jobs"])
        self.assertEqual(jobs - set(ci_local.JOB_PATHS) - set(ci_local.SKIPPED_JOBS), set())
        self.assertEqual((set(ci_local.JOB_PATHS) | set(ci_local.SKIPPED_JOBS)) - jobs, set(), "stale job in ci-local.py")

    def test_commands_are_ci_yml_text_not_copies(self):
        planned = {(s.job, s.name): s.run for s in ci_local.plan("/tmp/runner") if s.run}
        self.assertIn("go test ./...", planned[("daemon", "Test")])
        self.assertIn("licenses/generate.py --check", planned[("client", "Open-source licenses are current")])

    def test_the_macos_daemon_job_runs_only_on_a_mac(self):
        planned = {(s.job, s.name): s for s in ci_local.plan("/tmp/runner")}
        if sys.platform == "darwin":
            self.assertIn("go test ./...", planned[("daemon-macos", "Test")].run)
            self.assertNotIn("daemon-macos", ci_local.SKIPPED_JOBS)
        else:
            self.assertIn("macOS runner", ci_local.SKIPPED_JOBS["daemon-macos"])


class TerminalCorpusGateTests(unittest.TestCase):
    """CI runs the terminal corpus only when the commits under test touch the terminal (changed.sh); ci-local agrees."""

    def test_ci_local_and_changed_sh_read_the_same_paths(self):
        patterns = ci_local.JOB_PATHS["terminal-corpus"]
        self.assertIn("client/ui/terminal/*", patterns)
        self.assertTrue(ci_local.matches("client/native/terminal/TerminalVt.cpp", patterns))
        self.assertFalse(ci_local.matches("client/ui/chat/Chat.tsx", patterns))

    def test_corpus_step_is_gated_in_ci_and_planned_here(self):
        planned = {(s.job, s.name): s for s in ci_local.plan("/tmp/runner")}
        self.assertIn("run.sh", planned[("terminal-corpus", "Terminal render corpus")].run)
        workflow = ci_local.load_workflow()
        step = next(s for s in workflow["jobs"]["terminal-corpus"]["steps"] if s.get("name") == "Terminal render corpus")
        self.assertEqual(step["if"], "steps.changed.outputs.run == 'true'")

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
