#!/usr/bin/env python3
"""Run one shard of the client's UI suite (`nsh test` in client/ui), so CI can spread it over runners.

`nsh test` runs every test file of client/ui one after the other in one process: 60 files and
681 tests took 14 min on CI's runner (11-14). The tests wait on the clock (10 s offline bounds,
15 s failures), so the suite is long but not busy; separate runners, not processes side by side
on one, keep each file's timing what it was.

  ui-test-shard.py --shard 2/4      run shard 2 of 4: `nsh test <file>` for each of its files
  ui-test-shard.py --list 2/4       print shard 2's files and their expected seconds, run nothing
  ui-test-shard.py --plan 4         print every shard of 4

The files are the ones `nsh test` itself runs: `*.test.tsx` and `*.test.ts` under client/ui that
import `nsh/test` (node_modules aside). Every file goes to exactly one shard: the longest first,
each to the shard with the fewest expected seconds so far (ui-test-seconds.json, measured on CI;
a file not in it counts as DEFAULT_SECONDS). A shard runs all of its files even after one fails,
then exits 1, and prints how many tests it ran (from each file's --junit report).

NSH: $NSH (a path), else ${NSH_HOME:-~/.nsh}/bin/nsh.
"""

import argparse
import json
import os
import re
import subprocess
import sys
import tempfile
import time
import xml.etree.ElementTree as ET
from pathlib import Path

CLIENT_UI = Path(__file__).resolve().parents[1] / "ui"
WEIGHTS = Path(__file__).resolve().with_name("ui-test-seconds.json")
DEFAULT_SECONDS = 10
IMPORTS_NSH_TEST = re.compile(r"""from\s+["']nsh/test["']""")


def test_files(root=CLIENT_UI):
    """Paths relative to client/ui, sorted, of the files `nsh test` runs there."""
    found = []
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = sorted(d for d in dirnames if d != "node_modules" and not d.startswith("."))
        for name in filenames:
            if name.endswith((".test.tsx", ".test.ts")):
                path = Path(dirpath) / name
                if IMPORTS_NSH_TEST.search(path.read_text(encoding="utf-8")):
                    found.append(path.relative_to(root).as_posix())
    return sorted(found)


def load_weights(path=WEIGHTS):
    return json.loads(path.read_text(encoding="utf-8")) if path.exists() else {}


def shards(files, count, weights):
    """`count` lists of files; together they hold each file exactly once."""
    if count < 1:
        raise ValueError("a suite has at least one shard")
    buckets = [[] for _ in range(count)]
    totals = [0.0] * count
    for name in sorted(files, key=lambda f: (-weights.get(f, DEFAULT_SECONDS), f)):
        index = min(range(count), key=lambda i: (totals[i], i))
        buckets[index].append(name)
        totals[index] += weights.get(name, DEFAULT_SECONDS)
    return [sorted(bucket) for bucket in buckets]


def parse_shard(text):
    match = re.fullmatch(r"(\d+)/(\d+)", text)
    if not match or not 1 <= int(match.group(1)) <= int(match.group(2)):
        raise argparse.ArgumentTypeError(f"a shard is i/n with 1 <= i <= n, not {text!r}")
    return int(match.group(1)), int(match.group(2))


def junit_counts(path):
    """(tests, failed) from an `nsh test --junit` report; (0, 1) when there is none."""
    try:
        suites = ET.parse(path).getroot()
    except (OSError, ET.ParseError):
        return 0, 1
    tests = int(suites.get("tests", 0))
    failed = int(suites.get("failures", 0)) + int(suites.get("errors", 0))
    return tests, failed


def run(files, nsh):
    results = []
    with tempfile.TemporaryDirectory(prefix="ui-test-shard-") as reports:
        for index, name in enumerate(files):
            print(f"\n== [{index + 1}/{len(files)}] nsh test {name}", flush=True)
            report = Path(reports) / f"{index}.xml"
            started = time.monotonic()
            code = subprocess.run([nsh, "test", name, "--junit", str(report)], cwd=CLIENT_UI).returncode
            seconds = time.monotonic() - started
            tests, failed = junit_counts(report)
            results.append((name, code == 0 and failed == 0, tests, failed, seconds))
    return results


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    group = parser.add_mutually_exclusive_group(required=True)
    group.add_argument("--shard", type=parse_shard, help="run shard i of n (i/n)")
    group.add_argument("--list", type=parse_shard, help="print shard i of n's files")
    group.add_argument("--plan", type=int, help="print every shard of n")
    args = parser.parse_args()

    files, weights = test_files(), load_weights()
    if not files:
        sys.exit(f"error: no test files under {CLIENT_UI}")
    if args.plan:
        for index, bucket in enumerate(shards(files, args.plan, weights), 1):
            expected = sum(weights.get(f, DEFAULT_SECONDS) for f in bucket)
            print(f"shard {index}/{args.plan}: {len(bucket)} files, ~{expected:.0f} s")
            for name in bucket:
                print(f"  {weights.get(name, DEFAULT_SECONDS):5.0f} s  {name}")
        return 0
    index, count = args.shard or args.list
    bucket = shards(files, count, weights)[index - 1]
    if args.list:
        for name in bucket:
            print(f"{weights.get(name, DEFAULT_SECONDS):5.0f} s  {name}")
        return 0

    nsh = os.environ.get("NSH") or str(Path(os.environ.get("NSH_HOME") or Path.home() / ".nsh") / "bin/nsh")
    started = time.monotonic()
    results = run(bucket, nsh)
    print(f"\nui-test-shard {index}/{count}: {len(bucket)} of {len(files)} files")
    for name, ok, tests, failed, seconds in sorted(results, key=lambda r: -r[4]):
        print(f"  {'ok  ' if ok else 'FAIL'} {seconds:6.1f} s  {tests:3} tests  {name}" + (f"  ({failed} failed)" if failed else ""))
    total_tests = sum(r[2] for r in results)
    bad = [r[0] for r in results if not r[1]]
    print(f"  {total_tests} tests in {time.monotonic() - started:.0f} s: " + (f"FAILED in {', '.join(bad)}" if bad else "all passed"))
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
