#!/usr/bin/env python3
"""Is vX.Y.Z released? The one definition (docs/ci-release.md#released), checked before anyone is told.

  released.py --tag vX.Y.Z [--repo OWNER/NAME]

A release counts only when all of these hold:
  1. the tag is an annotated tag whose commit is on main (an ancestor of origin/main);
  2. that commit passed CI on main, or is the version change on top of one that did
     (scripts/release-green.py; a run cancelled by the next push is not a pass);
  3. the GitHub release is public and carries every asset, none empty;
  4. a stable tag is marked Latest and is the highest stable version published (or a higher
     release has since taken the mark); a beta is a prerelease.
Prints each check; exits 1 if any fails. GH_TOKEN reads the release and the runs.
"""

from __future__ import annotations

import argparse
import importlib.util
import json
import os
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
STABLE = re.compile(r"^v(\d+)\.(\d+)\.(\d+)$")
TAG = re.compile(r"^v\d+\.\d+\.\d+(-beta\.\d+)?$")


def assets(version: str) -> list[str]:
    return sorted([
        "mewla-linux-amd64.tar.gz",
        "mewla-linux-arm64.tar.gz",
        "mewla-darwin-arm64.tar.gz",
        "mewla-windows-amd64.zip",
        f"mewla-android-arm64-v{version}.apk",
        "SHA256SUMS",
        "release-manifest.json",
        "release-manifest.json.sig",
    ])


def git(*args: str, check: bool = True) -> subprocess.CompletedProcess:
    return subprocess.run(["git", "-C", str(ROOT), *args], check=check, capture_output=True, text=True)


def gh(*args: str) -> str:
    return subprocess.run(["gh", *args], check=True, capture_output=True, text=True, timeout=60).stdout


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    parser.add_argument("--tag", required=True)
    parser.add_argument("--repo", default=os.environ.get("GITHUB_REPOSITORY", "daoleno/mewla"))
    args = parser.parse_args(argv)
    tag, repo = args.tag, args.repo
    if not TAG.match(tag):
        raise SystemExit(f"error: not a release tag: {tag}")
    failed: list[str] = []

    def report(ok: bool, what: str) -> None:
        print(f"{'ok  ' if ok else 'FAIL'} {what}")
        if not ok:
            failed.append(what)

    git("fetch", "--quiet", "--no-tags", "origin", "+refs/heads/main:refs/remotes/origin/main", check=False)
    annotated = git("cat-file", "-t", f"refs/tags/{tag}", check=False).stdout.strip() == "tag"
    on_main = annotated and git("merge-base", "--is-ancestor", f"{tag}^{{commit}}", "origin/main", check=False).returncode == 0
    report(annotated and on_main, f"{tag} is an annotated tag on main")

    spec = importlib.util.spec_from_file_location("release_green", ROOT / "scripts/release-green.py")
    green = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(green)
    try:
        result = green.check(repo, git("rev-parse", f"{tag}^{{commit}}").stdout.strip(), wait_minutes=0)
        report(True, f"CI passed on main: run {result['run']} on {result['commit'][:12]}")
    except (green.NotGreen, subprocess.CalledProcessError) as exc:
        report(False, f"CI passed on main ({exc})")

    try:
        release = json.loads(gh("api", f"repos/{repo}/releases/tags/{tag}"))
    except subprocess.CalledProcessError:
        release = None
    report(release is not None and not release["draft"], f"{tag} is published")
    if release is not None:
        have = sorted(a["name"] for a in release["assets"] if a["size"] > 0)
        want = assets(tag.removeprefix("v"))
        report(have == want, f"every asset, none empty ({len(have)}/{len(want)})")
        if STABLE.match(tag):
            published = [r["tag_name"] for r in json.loads(gh("api", f"repos/{repo}/releases?per_page=100"))
                         if not r["draft"] and not r["prerelease"] and STABLE.match(r["tag_name"])]
            highest = max(published, key=lambda t: tuple(int(x) for x in STABLE.match(t).groups()))
            latest = json.loads(gh("api", f"repos/{repo}/releases/latest"))["tag_name"]
            newer = "" if tag == highest else f"; {tag} was since superseded"
            report(latest == highest and tag in published, f"Latest is {latest}, the highest stable is {highest}{newer}")
        else:
            report(release["prerelease"], f"{tag} is a prerelease")

    print(f"{tag}: {'released' if not failed else 'NOT released'}")
    return 1 if failed else 0


if __name__ == "__main__":
    raise SystemExit(main())
