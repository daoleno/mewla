#!/usr/bin/env python3
"""A release ships a main commit CI already passed: check that, in seconds, without running a suite.

  release-green.py --sha SHA [--repo OWNER/NAME] [--wait-minutes 15]

Walks main's first-parent history back from SHA to the nearest commit with a CI run (ci.yml).
Every commit passed on the way must need no run of its own: one whose files are all in ci.yml's
push `paths-ignore` (docs, site, root Markdown) or a release preparation commit ("Prepare vX.Y.Z"
by github-actions[bot], which only moves the version). The commits before the run's commit are
in it: one push runs CI once, on its newest commit.

That run must have succeeded. One still running is waited for up to --wait-minutes, so a release
dispatched right after a merge works; a red, cancelled or timed-out run fails the check, and so
does a commit that should have had a run and has none. GH_TOKEN reads the runs (actions: read).
Prints one JSON line: the commit that passed, its run, and the commits it covers.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CI = ROOT / ".github/workflows/ci.yml"
BOT = "github-actions[bot]"
PREPARE = re.compile(r"^Prepare v\d+\.\d+\.\d+(-beta\.\d+)?$")
MAX_COMMITS = 200


class NotGreen(RuntimeError):
    pass


def ignored_globs(workflow: str) -> list[str]:
    """ci.yml's push paths-ignore list."""
    match = re.search(r"\n  push:\n(?:.*\n)*?    paths-ignore:\n((?:      - .*\n)+)", workflow)
    if not match:
        raise NotGreen("ci.yml has no push paths-ignore list")
    return [line.split("- ", 1)[1].strip().strip("\"'") for line in match.group(1).splitlines()]


def glob_regex(glob: str) -> re.Pattern[str]:
    """GitHub's path filter glob: ** crosses directories, * and ? do not."""
    out = ""
    i = 0
    while i < len(glob):
        if glob.startswith("**", i):
            out += ".*"
            i += 2
        elif glob[i] == "*":
            out += "[^/]*"
            i += 1
        elif glob[i] == "?":
            out += "[^/]"
            i += 1
        else:
            out += re.escape(glob[i])
            i += 1
    return re.compile(out + r"\Z")


def git(*args: str) -> str:
    return subprocess.run(["git", "-C", str(ROOT), *args], check=True, capture_output=True, text=True).stdout


def runs_for(repo: str, sha: str) -> list[dict]:
    out = subprocess.run(
        ["gh", "api", f"repos/{repo}/actions/workflows/ci.yml/runs?head_sha={sha}&per_page=20"],
        check=True, capture_output=True, text=True, timeout=60,
    ).stdout
    return [r for r in json.loads(out)["workflow_runs"] if r["event"] in ("push", "workflow_dispatch")]


def verdict(runs: list[dict]) -> tuple[str, dict | None]:
    """passed, running or failed: any green run passes; else a running one is waited for."""
    for run in runs:
        if run["status"] == "completed" and run["conclusion"] == "success":
            return "passed", run
    for run in runs:
        if run["status"] != "completed":
            return "running", run
    return "failed", runs[0] if runs else None


def needs_no_run(sha: str, ignored: list[re.Pattern[str]]) -> str | None:
    """Why this commit has no CI run of its own, or None when it should have one."""
    author, subject = git("show", "-s", "--format=%an%x00%s", sha).rstrip("\n").split("\x00", 1)
    if author == BOT and PREPARE.match(subject):
        return "release preparation"
    files = [f for f in git("diff-tree", "--no-commit-id", "--name-only", "-r", sha).splitlines() if f]
    if files and all(any(p.match(f) for p in ignored) for f in files):
        return "docs only"
    return None


def check(repo: str, sha: str, wait_minutes: float, poll: float = 20.0) -> dict:
    ignored = [glob_regex(g) for g in ignored_globs(CI.read_text(encoding="utf-8"))]
    covered: list[dict] = []
    commit = git("rev-parse", f"{sha}^{{commit}}").strip()
    for _ in range(MAX_COMMITS):
        runs = runs_for(repo, commit)
        if runs:
            deadline = time.monotonic() + wait_minutes * 60
            state, run = verdict(runs)
            while state == "running" and time.monotonic() < deadline:
                print(f"CI run {run['id']} on {commit[:12]} is {run['status']}; waiting", file=sys.stderr, flush=True)
                time.sleep(poll)
                state, run = verdict(runs_for(repo, commit))
            if state == "passed":
                return {"green": True, "commit": commit, "run": run["id"], "url": run["html_url"], "covers": covered}
            if state == "running":
                raise NotGreen(f"CI run {run['html_url']} on {commit[:12]} did not finish within {wait_minutes:g} min")
            raise NotGreen(
                f"CI on {commit[:12]} is {run['conclusion'] if run else 'missing'} ({run['html_url'] if run else '-'}): "
                "a red main is a bug to fix now; release a commit with a green run"
            )
        reason = needs_no_run(commit, ignored)
        if reason is None:
            raise NotGreen(f"{commit[:12]} changes checked files but has no CI run")
        covered.append({"commit": commit, "why": reason})
        parents = git("rev-list", "--parents", "-n", "1", commit).split()
        if len(parents) < 2:
            raise NotGreen("reached the first commit without a CI run")
        commit = parents[1]
    raise NotGreen(f"no CI run in the {MAX_COMMITS} commits before {sha[:12]}")


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    parser.add_argument("--sha", required=True)
    parser.add_argument("--repo", default=os.environ.get("GITHUB_REPOSITORY", "daoleno/mewla"))
    parser.add_argument("--wait-minutes", type=float, default=15)
    args = parser.parse_args(argv)
    try:
        result = check(args.repo, args.sha, args.wait_minutes)
    except NotGreen as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1
    print(json.dumps(result))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
