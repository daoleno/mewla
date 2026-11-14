"""scripts/ci-local.py must cover every job and step of .github/workflows/ci.yml."""

import importlib.util
from pathlib import Path
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


if __name__ == "__main__":
    unittest.main()
