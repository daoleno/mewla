#!/usr/bin/env python3
"""Every commit in Mewla is daoleno's: fail on any other author or committer, or a Co-authored-by trailer.

Checks the whole history reachable from each REV (default HEAD), so a branch built on history from
before the 2026-11-25 rewrite fails too, not only its newest commits. The one other committer allowed
is GitHub's own (GitHub <noreply@github.com>), which commits a merge made on github.com.
Prints one line per bad commit and exits 1; prints the count checked and exits 0 when all are clean.
"""

from __future__ import annotations

import argparse
import re
import subprocess
import sys

OWNER = ("daoleno", "daodaoonline@gmail.com")
GITHUB_WEB = ("GitHub", "noreply@github.com")
TRAILER = re.compile(r"^co-authored-by:", re.IGNORECASE | re.MULTILINE)


def problems(fields: list[str]) -> list[str]:
    """What is wrong with one commit: author name/email, committer name/email, message."""
    an, ae, cn, ce, message = fields
    found = []
    if (an, ae) != OWNER:
        found.append(f"author {an} <{ae}>")
    if (cn, ce) not in (OWNER, GITHUB_WEB):
        found.append(f"committer {cn} <{ce}>")
    if TRAILER.search(message):
        found.append("Co-authored-by trailer")
    return found


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("revs", nargs="*", default=["HEAD"])
    args = parser.parse_args()
    out = subprocess.run(
        ["git", "log", "--format=%H%x1f%an%x1f%ae%x1f%cn%x1f%ce%x1f%B%x1e", *args.revs, "--"],
        capture_output=True, text=True, check=True,
    ).stdout
    checked = bad = 0
    for record in out.split("\x1e"):
        record = record.strip("\n")
        if not record:
            continue
        sha, *fields = record.split("\x1f")
        checked += 1
        found = problems(fields)
        if found:
            bad += 1
            print(f"{sha[:12]}: {'; '.join(found)}")
    if bad:
        print(f"{bad} of {checked} commits are not daoleno's alone. Rewrite them (git commit --amend --reset-author, "
              "drop the trailer) with repo-local user.name daoleno / user.email daodaoonline@gmail.com.", file=sys.stderr)
        return 1
    print(f"{checked} commits, all daoleno's")
    return 0


if __name__ == "__main__":
    sys.exit(main())
