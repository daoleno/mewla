#!/usr/bin/env python3
"""The warm Mac loop's yield rules (.github/workflows/macos-loop.yml, mac-loop.sh), read from the Actions API.

  yield.py waiters <seconds> <own run id>   prints the macOS jobs of other runs queued at least <seconds>; exit 0
  yield.py blockers                          prints release runs and other loops queued or in progress; exit 0

A loop never starts beside a release, and leaves as soon as another macOS job has waited <seconds> for a runner: the
account's macOS runners are few (5 at most at once, fewer in practice) and the releases and proof lanes come first.
Needs GH_TOKEN (actions: read) and GITHUB_REPOSITORY (owner/name).
"""
import json
import os
import sys
import time
import urllib.request
from datetime import datetime, timezone

REPO = os.environ["GITHUB_REPOSITORY"]
# The runs a loop never starts beside: the releases (and the Mac release gate, a dispatch of macos-release.yml).
RELEASES = (".github/workflows/release-artifacts.yml", ".github/workflows/macos-release.yml",
            ".github/workflows/ios-release.yml", ".github/workflows/release-next-beta.yml")
LOOP = ".github/workflows/macos-loop.yml"


def api(path):
    request = urllib.request.Request(f"https://api.github.com/repos/{REPO}/{path}", headers={
        "Authorization": f"Bearer {os.environ['GH_TOKEN']}", "Accept": "application/vnd.github+json"})
    with urllib.request.urlopen(request, timeout=20) as response:
        return json.load(response)


def active_runs():
    runs = []
    # A queued job sits in a run that is queued or in progress.
    for status in ("queued", "in_progress"):
        runs += api(f"actions/runs?status={status}&per_page=100")["workflow_runs"]
    return runs


def waiters(seconds, own):
    now = time.time()
    for run in active_runs():
        if run["id"] == own:
            continue
        for job in api(f"actions/runs/{run['id']}/jobs?filter=latest&per_page=100")["jobs"]:
            if job["status"] != "queued" or not any(label.startswith("macos") for label in job["labels"]):
                continue
            queued = now - datetime.fromisoformat(job["created_at"].replace("Z", "+00:00")).timestamp()
            if queued >= seconds:
                print(f"{run['name']} / {job['name']} (run {run['id']}) queued {int(queued)} s")


def blockers():
    for run in active_runs():
        if run["path"] in RELEASES or "release gate" in (run.get("display_title") or "").lower():
            print(f"release: {run['name']} (run {run['id']}, {run['status']})")
        elif run["path"] == LOOP:
            print(f"another loop: run {run['id']} ({run['status']})")


if __name__ == "__main__":
    if sys.argv[1] == "waiters":
        waiters(int(sys.argv[2]), int(sys.argv[3]))
    elif sys.argv[1] == "blockers":
        blockers()
    else:
        sys.exit(__doc__)
