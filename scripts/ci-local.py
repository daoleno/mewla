#!/usr/bin/env python3
"""Run the checks of .github/workflows/ci.yml that can run on a Linux dev box.

Run it before pushing to main. The step commands are read from ci.yml, so the
script cannot drift from CI by itself; scripts/tests/test_ci_local.py fails when
ci.yml gains a job or an action this script does not know how to handle.

Usage (repository root):
  scripts/ci-local.py              # only the jobs the diff touches
  scripts/ci-local.py --all        # every job that can run here
  scripts/ci-local.py --list       # print the plan, run nothing
  scripts/ci-local.py --base REF   # diff against REF (default origin/main)
  scripts/ci-local.py --job app    # one job, whatever the diff is
  scripts/ci-local.py --job app --step licenses   # only steps whose name contains the text

The diff is origin/main..HEAD plus uncommitted and untracked files.
"""

import argparse
import fnmatch
import os
import re
import shutil
import subprocess
import sys
import tempfile
import time
from pathlib import Path

try:
    import yaml
except ImportError:  # pragma: no cover
    sys.exit("error: python3 needs PyYAML to read ci.yml (apt install python3-yaml)")

ROOT = Path(__file__).resolve().parents[1]
WORKFLOW = ROOT / ".github/workflows/ci.yml"

# Jobs that need hardware or SDKs a Linux dev box does not have.
SKIPPED_JOBS = {
    "ios-native": "needs macOS and Xcode (unsigned Simulator build)",
    "android-native": "needs the Android SDK and a Gradle native link",
    "windows": "needs a Windows runner (Windows PowerShell and the Win32 API)",
}

# Path globs (git paths, fnmatch with * crossing "/") that make a job relevant.
# A change to ci.yml or to this script runs everything.
EVERYTHING = [".github/workflows/ci.yml", "scripts/ci-local.py", "bunfig.toml"]
JOB_PATHS = {
    "installer": ["install.sh", "scripts/tests/install_test.sh", "scripts/install*"],
    "daemon": ["daemon/*", "scripts/*plugin-publisher*", "scripts/build-daemon-linux.sh", "scripts/build-web-ui.sh"],
    "app": [
        "app/*", "bun.lock", "package.json", "patches/*", "scripts/app-licenses/*",
        "scripts/generate-*.mjs", "docs/*",
    ],
    "client": ["client/*"],
    "native-contract": ["scripts/verify-libghostty.sh", "scripts/lib/*", "app/modules/terminal-vt/*"],
    "release-identity": [
        "scripts/*", "app/app.base.json", "app/ios-build.json", "app/package.json",
        "package.json", "CHANGELOG.md", "release/*", "docs/*",
    ],
}

# Actions that only set up the runner or publish results; nothing to run here.
IGNORED_ACTIONS = (
    "actions/checkout@", "oven-sh/setup-bun@", "actions/setup-go@", "actions/upload-artifact@", "actions/cache/restore@",
)

# Steps CI runs only on tags and manual dispatch.
RELEASE_ONLY = re.compile(r"workflow_dispatch|ref_type\s*==\s*'tag'")

# Environment that would make a local run differ from a clean runner: the live
# daemon's state directory and model credentials.
SCRUBBED_ENV = re.compile(r"^(MEWLA_STATE_DIR|ANTHROPIC_.*|CLAUDE_CODE_.*)$")


class Step:
    def __init__(self, job, name, run, cwd, env, skip=None, prerequisites=()):
        self.job, self.name, self.run, self.cwd, self.env = job, name, run, cwd, env
        self.skip, self.prerequisites = skip, tuple(prerequisites)


def expand(text, runner_temp):
    """Substitute the few ${{ }} expressions CI uses; refuse any other."""
    values = {
        "runner.temp": runner_temp,
        "github.event_name": "push",
        "github.ref_type": "branch",
        "github.sha": "local",
        "github.workspace": str(ROOT),
    }

    def replace(match):
        key = match.group(1).strip()
        if key not in values:
            raise ValueError(f"ci.yml uses ${{{{ {key} }}}}, which scripts/ci-local.py does not know")
        return values[key]

    return re.sub(r"\$\{\{(.*?)\}\}", replace, text)


def load_workflow():
    return yaml.safe_load(WORKFLOW.read_text())


def plan(runner_temp):
    """Every step of every job, in ci.yml order, with what to do with it."""
    workflow = load_workflow()
    steps = []
    for job_id, job in workflow["jobs"].items():
        if job_id in SKIPPED_JOBS:
            steps.append(Step(job_id, f"(whole job) {job.get('name', job_id)}", None, ROOT, {}, skip=SKIPPED_JOBS[job_id]))
            continue
        if job_id not in JOB_PATHS:
            raise ValueError(f"job {job_id} in ci.yml is neither in JOB_PATHS nor in SKIPPED_JOBS of scripts/ci-local.py")
        job_dir = (job.get("defaults") or {}).get("run", {}).get("working-directory", ".")
        for index, step in enumerate(job["steps"]):
            name = step.get("name") or step.get("uses") or f"step {index + 1}"
            condition = str(step.get("if", ""))
            if "uses" in step:
                if not step["uses"].startswith(IGNORED_ACTIONS) and not step["uses"].startswith("./"):
                    raise ValueError(f"job {job_id}: action {step['uses']} is unknown to scripts/ci-local.py")
                if step["uses"].startswith("./"):
                    steps.append(Step(job_id, name, None, ROOT, {}, skip="local action (native caches)"))
                continue
            if "run" not in step:
                raise ValueError(f"job {job_id}: step {name!r} has neither run nor uses")
            if condition and RELEASE_ONLY.search(condition):
                steps.append(Step(job_id, name, None, ROOT, {}, skip="runs only on release tags and manual dispatch"))
                continue
            if condition:
                raise ValueError(f"job {job_id}: step {name!r} has condition {condition!r} that scripts/ci-local.py does not know")
            cwd = ROOT / expand(step.get("working-directory", job_dir), runner_temp)
            env = {key: expand(str(value), runner_temp) for key, value in {**(job.get("env") or {}), **(step.get("env") or {})}.items()}
            run = expand(step["run"], runner_temp)
            if "apt-get" in run:
                packages = re.findall(r"install\s+(?:-y\s+)?([\w .+-]+)", run)
                steps.append(Step(job_id, name, None, cwd, env, prerequisites=" ".join(packages).split(),
                                  skip="apt-get install replaced by a check that the tools exist"))
                continue
            steps.append(Step(job_id, name, run, cwd, env))
    return steps


def git_lines(*args):
    result = subprocess.run(["git", *args], cwd=ROOT, capture_output=True, text=True)
    return result.stdout.splitlines() if result.returncode == 0 else []


def changed_paths(base):
    paths = set(git_lines("diff", "--name-only", f"{base}...HEAD"))
    paths |= set(git_lines("diff", "--name-only", "HEAD"))
    paths |= set(git_lines("ls-files", "--others", "--exclude-standard"))
    return paths


def matches(path, patterns):
    return any(fnmatch.fnmatchcase(path, pattern) for pattern in patterns)


def select_jobs(workflow_jobs, every, only, base):
    if only:
        unknown = [job for job in only if job not in workflow_jobs]
        if unknown:
            sys.exit(f"error: no job {', '.join(unknown)} in ci.yml (jobs: {', '.join(workflow_jobs)})")
        return {job: f"--job {job}" for job in only}
    if every:
        return {job: "--all" for job in workflow_jobs}
    paths = changed_paths(base)
    if any(matches(path, EVERYTHING) for path in paths):
        return {job: "ci.yml or ci-local.py changed" for job in workflow_jobs}
    chosen = {}
    for job, patterns in JOB_PATHS.items():
        hit = sorted(path for path in paths if matches(path, patterns))
        if hit:
            chosen[job] = f"{len(hit)} changed path(s), e.g. {hit[0]}"
    return chosen


def child_env(runner_temp):
    env = {key: value for key, value in os.environ.items() if not SCRUBBED_ENV.match(key)}
    env["RUNNER_TEMP"] = runner_temp
    env["CI"] = "true"
    return env


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--all", action="store_true", help="run every job that can run here")
    parser.add_argument("--list", action="store_true", help="print the plan and run nothing")
    parser.add_argument("--base", default="origin/main", help="diff base (default origin/main)")
    parser.add_argument("--job", action="append", help="run only this ci.yml job (repeatable)")
    parser.add_argument("--step", action="append", help="with --job: only steps whose name contains this text")
    args = parser.parse_args()

    scratch = tempfile.mkdtemp(prefix="mewla-ci-local-", dir=os.environ.get("TMPDIR") or None)
    try:
        return run_plan(args, scratch)
    finally:
        shutil.rmtree(scratch, ignore_errors=True)


def run_plan(args, runner_temp):
    steps = plan(runner_temp)
    jobs = list(dict.fromkeys(step.job for step in steps))
    chosen = select_jobs(jobs, args.all, args.job, args.base)

    print("ci-local: reading steps from .github/workflows/ci.yml")
    for job in jobs:
        if job in SKIPPED_JOBS:
            print(f"  skip   {job}: {SKIPPED_JOBS[job]}")
        elif job in chosen:
            print(f"  run    {job}: {chosen[job]}")
        else:
            print(f"  idle   {job}: the diff does not touch it (--all to force)")
    if not chosen:
        print("ci-local: nothing to run for this diff")
    if args.list:
        for step in steps:
            if step.job in chosen or step.job in SKIPPED_JOBS:
                print(f"    [{step.job}] {step.name}" + (f"  -- skipped: {step.skip}" if step.skip else ""))
        return 0

    results, failed = [], False
    started = time.monotonic()
    if True:  # one block for the jobs
        for job in jobs:
            if job not in chosen:
                continue
            for step in (s for s in steps if s.job == job):
                if args.step and not any(text.lower() in step.name.lower() for text in args.step):
                    continue
                label = f"[{job}] {step.name}"
                if step.prerequisites:
                    missing = [tool for tool in step.prerequisites if not shutil.which(tool)]
                    print(f"\n== {label}: needs {', '.join(step.prerequisites)}" + (f"; MISSING {', '.join(missing)}" if missing else "; present"))
                    if missing:
                        results.append((label, "FAIL", 0.0))
                        failed = True
                        break
                    continue
                if step.skip:
                    print(f"\n== {label}: skipped ({step.skip})")
                    continue
                run = step.run
                print(f"\n== {label}\n$ {run.strip()}", flush=True)
                t0 = time.monotonic()
                env = child_env(runner_temp)
                env.update(step.env)
                code = subprocess.run(["bash", "-e", "-c", run], cwd=step.cwd, env=env).returncode
                seconds = time.monotonic() - t0
                results.append((label, "ok" if code == 0 else "FAIL", seconds))
                if code != 0:
                    print(f"\nFAILED [{job}] {step.name} (exit {code}); CI would stop this job here", flush=True)
                    failed = True
                    break

    print("\nci-local summary")
    for label, status, seconds in results:
        print(f"  {status:4} {seconds:7.1f}s  {label}")
    skipped = [f"{job} ({reason})" for job, reason in SKIPPED_JOBS.items()]
    print("  skipped here: " + "; ".join(skipped))
    print(f"  total {time.monotonic() - started:.1f}s: " + ("FAILED" if failed else "all green"))
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
