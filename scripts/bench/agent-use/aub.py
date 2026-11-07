#!/usr/bin/env python3
"""Agent-use benchmark ("AUB"): one result format, one report, for mewla_chrome / mewla_computer / mewla_phone and
the comparison tool layers. Standard library only.

  aub.py published [--out FILE]        fetch Scale's per-trial Cua Driver Bench dataset and write the aggregates
  aub.py convert-web SUITE.json ...    browser/suite.mjs output  -> results/<run>.json
  aub.py import-cdb RUN_DIR ...        Cua Driver Bench results.jsonl -> results/<run>.json
  aub.py report [FILE ...]             table per suite/condition (default: every file in results/), with the
                                       published rows next to ours when the suite matches
  aub.py trend [--suite S --tool T]    pass rate per run over time
  aub.py check FILE ...                validate result files against the schema

A result file ("aub/1") is one run: one suite, one tool layer, one model, one condition, a list of trial rows.
Pass rate, background pass rate, non-interference violations, time, steps and tokens are all computed from the
rows by `report`, never stored, so they cannot disagree with the trials. See README.md and DESIGN.md.
"""
import argparse, datetime, glob, json, math, os, statistics, subprocess, sys, urllib.error, urllib.parse, urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
RESULTS = os.path.join(HERE, "results")
SCHEMA = "aub/1"
HF = "https://datasets-server.huggingface.co/rows"
HF_DATASET = "earakely-scale/CuaDriverBench-AgentEnv"
# The BenchLab tasks Mewla can run on one Mac without LibreOffice, GnuCash or Chrome (see DESIGN.md).
CDB_MEWLA_SUBSET = ["IR-01", "IR-02", "IR-03", "IR-04", "MB-09", "MB-10", "MB-11"]


# ---------------------------------------------------------------- statistics

def wilson(k, n, z=1.96):
    if not n:
        return (None, None)
    p = k / n
    d = 1 + z * z / n
    c = p + z * z / (2 * n)
    r = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n))
    return (round(max(0, (c - r) / d) * 100), round(min(1, (c + r) / d) * 100))


def med(xs):
    xs = [x for x in xs if x is not None]
    return round(statistics.median(xs), 2) if xs else None


def git_sha():
    try:
        return subprocess.run(["git", "-C", HERE, "rev-parse", "--short", "HEAD"], capture_output=True, text=True).stdout.strip()
    except OSError:
        return ""


# ---------------------------------------------------------------- the schema

def new_run(run_id, suite, platform, tool, condition, model, harness, rows, **extra):
    d = {"schema": SCHEMA, "run_id": run_id, "date": datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%MZ"),
         "suite": suite, "platform": platform, "tool": tool, "condition": condition, "model": model, "harness": harness,
         "mewla_commit": git_sha(), "source": "own", "rows": rows}
    d.update(extra)
    return d


def row(task, trial, ok, wall_s=None, turns=None, steps=None, tokens=None, cost_usd=None, undisturbed=None, disturbed_by=None, note=None):
    """full_pass: the task checker passed, and, when the condition measures it, the person was not disturbed."""
    t = tokens or {}
    total = sum(v for v in (t.get("input"), t.get("output"), t.get("cache_read"), t.get("cache_write")) if v)
    r = {"task": task, "trial": trial, "ok": bool(ok), "undisturbed": undisturbed, "disturbed_by": disturbed_by or [],
         "full_pass": bool(ok) and undisturbed is not False, "wall_s": wall_s, "turns": turns, "steps": steps,
         "tokens": dict(t, total=total or None), "cost_usd": cost_usd}
    if note:
        r["note"] = note
    return r


def save(run):
    os.makedirs(RESULTS, exist_ok=True)
    path = os.path.join(RESULTS, run["run_id"] + ".json")
    json.dump(run, open(path, "w"), indent=1)
    print(path)
    return path


def check(path):
    d = json.load(open(path))
    errs = []
    if d.get("schema") != SCHEMA:
        errs.append("schema is not " + SCHEMA)
    for k in ("run_id", "suite", "platform", "tool", "condition", "model", "rows"):
        if k not in d:
            errs.append("missing " + k)
    for i, r in enumerate(d.get("rows", [])):
        for k in ("task", "trial", "ok", "full_pass"):
            if k not in r:
                errs.append("row %d lacks %s" % (i, k))
        if d.get("condition") == "background" and d.get("source") == "own" and r.get("undisturbed") is None:
            errs.append("row %d: a background trial must say whether the person was disturbed" % i)
    return errs


# ---------------------------------------------------------------- converters

def convert_web(files):
    """browser/suite.mjs rows. `side` mewla = mewla_chrome (real chain, or stand-in: the file's meta says which)."""
    for f in files:
        d = json.load(open(f))
        meta = d.get("meta") or {}
        rows = []
        for r in d["rows"]:
            ni = r.get("nonint")
            disturbed = [k for k in (ni or {}).get("measured", []) if (ni or {}).get(k)] if ni else None
            tok = {"input": r.get("input"), "output": r.get("output"), "cache_read": r.get("cache_read"), "cache_write": r.get("cache_create")}
            rows.append(row(r["task"], r["rep"], r["ok"], round(r["wall_ms"] / 1000, 1), r.get("turns"), r.get("tool_calls"), tok,
                            r.get("cost_usd"), None if ni is None else ni.get("undisturbed"), disturbed))
            if ni:
                rows[-1]["nonint"] = {k: ni.get(k) for k in ("typed", "keys_received", "typing_lost", "key_focus_lost", "raised_over_user", "pointer_moved", "user_page_hidden")}
        tool = {"mewla": "mewla_chrome", "ab": "agent-browser"}.get(d.get("side"), d.get("side"))
        run_id = meta.get("run_id") or os.path.basename(f).rsplit(".", 1)[0]
        extra = {k: meta[k] for k in ("transport", "tool_version", "notes", "nonint_measured", "nonint_not_measured") if k in meta}
        save(new_run(run_id, meta.get("suite", "web-fixture"), "linux", tool, "background" if any(r.get("nonint") for r in d["rows"]) else "foreground",
                     d.get("model", "claude-sonnet-5-5"), "claude -p, one tool layer, --max-turns 30", rows, **extra))


def import_cdb(dirs):
    """Cua Driver Bench runner output: runs/<id>/results.jsonl, one row per attempt (final ones only)."""
    for d in dirs:
        rows_by = {}
        for line in open(os.path.join(d, "results.jsonl")):
            r = json.loads(line)
            if r.get("final") is False or r.get("excluded"):
                continue
            rows_by.setdefault((r.get("arm"), r.get("condition")), []).append(r)
        for (arm, cond), rs in rows_by.items():
            out = []
            for r in rs:
                mp = r.get("multiplayer") or {}
                crit = mp.get("criteria") or {}
                bg = cond == "background"
                undist = mp.get("user_undisturbed") if bg else None
                disturbed = [k for k, v in crit.items() if v is not True] if bg and undist is False else []
                tools = r.get("tool_calls") or {}
                out.append(row(r.get("task"), r.get("run"), r.get("passed"), r.get("agent_wall_s") or r.get("wall_s"), r.get("turns"),
                               tools.get("total") if isinstance(tools, dict) else tools, r.get("tokens"), r.get("cost_usd"), undist, disturbed,
                               note=("gui-only violation" if (r.get("gui_only") or {}).get("violation") else None)))
            tool = {"cc-mewla-computer": "mewla_computer", "cc-agent-desktop": "agent-desktop", "cc-cua-driver-script": "cua-driver",
                    "cc-codex-cu": "codex-computer-use", "cc-browser-use": "browser-use"}.get(arm, arm)
            run_id = "%s-%s-%s-%s" % (datetime.date.today().strftime("%Y%m%d"), "cdb", tool, cond)
            save(new_run(run_id, "cdb-macos-benchlab", "macos", tool, cond, "claude-sonnet-5-5",
                         "Cua Driver Bench runner (claude -p, 360 s / 45 turns)", out, arm=arm, cdb_run_dir=os.path.basename(d.rstrip("/")),
                         nonint_measured=["key_focus", "window_raised", "typing", "pointer", "clipboard", "space", "dialog", "activation"],
                         nonint_not_measured=[]))


# ---------------------------------------------------------------- published rows (Scale's dataset)

def published(out, rows_file=None):
    """Aggregates of the 1596 trials Scale published: macOS, per driver x condition x task. Counts only: no
    transcripts, no recordings, no task text (those are Cua's under FSL-1.1-MIT)."""
    rows, off = [], 0
    if rows_file:
        rows, off = json.load(open(rows_file)), 1 << 60
    while off < 1 << 60:
        q = urllib.parse.urlencode({"dataset": HF_DATASET, "config": "trials", "split": "eval", "offset": off, "length": 100})
        for wait in (0, 5, 15, 40, 90):  # the dataset server rate-limits bursts
            import time; time.sleep(wait)
            try:
                d = json.load(urllib.request.urlopen(HF + "?" + q, timeout=60))
                break
            except urllib.error.HTTPError as e:
                if e.code not in (429, 500, 502, 503, 504):
                    raise
        else:
            sys.exit("the dataset server kept failing; try again later or pass --rows with a saved dump")
        rows += [x["row"] for x in d["rows"]]
        off += 100
        if off >= d["num_rows_total"]:
            break
    agg = {}
    for r in rows:
        if r.get("os") != "macos" or not r.get("on_board", True) or r.get("harness") != "claude-code":
            continue
        k = (r["driver"], r["condition"], r["probe"], r["model"].split("/")[-1])
        a = agg.setdefault(k, {"n": 0, "task_done": 0, "full_pass": 0, "turns": [], "dur": [], "cost": []})
        a["n"] += 1
        a["task_done"] += bool(r.get("task_passed"))
        a["full_pass"] += bool(r.get("passed"))
        a["turns"].append(r.get("num_turns"))
        a["dur"].append(r.get("duration_s"))
        a["cost"].append(r.get("cost_usd"))
    cells = [{"driver": k[0], "condition": k[1], "task": k[2], "model": k[3], "n": a["n"], "task_done": a["task_done"], "full_pass": a["full_pass"],
              "turns_med": med(a["turns"]), "duration_s_med": med(a["dur"]), "cost_usd_med": med(a["cost"])} for k, a in sorted(agg.items())]
    doc = {"schema": "aub-published/1", "source": "Scale AI, CuaDriverBench-AgentEnv dataset (Apache-2.0 for Scale's grades); tasks (c) Cua AI, Inc., FSL-1.1-MIT",
           "url": "https://huggingface.co/datasets/" + HF_DATASET, "blog": "https://github.com/trycua/cua/blob/main/blog/cua-driver-bench.md",
           "fetched": datetime.date.today().isoformat(), "model": "claude-sonnet-5-5", "os": "macos (plain VM, 1920x1080)", "trials_total": len(rows), "cells": cells}
    json.dump(doc, open(out, "w"), indent=1)
    print(out, len(cells), "cells from", len(rows), "trials")


PUB_DRIVER = {"cua-driver": "cua-driver", "agent-desktop": "agent-desktop", "browser-use": "macos-harness"}


def published_rate(doc, driver, condition, tasks, model="claude-sonnet-5-5"):
    k = n = d = 0
    for c in doc["cells"]:
        if c["driver"] == PUB_DRIVER.get(driver, driver) and c["condition"] == condition and c["model"] == model and (tasks is None or c["task"] in tasks):
            k += c["full_pass"]; n += c["n"]; d += c["task_done"]
    return k, n, d


# ---------------------------------------------------------------- report / trend

def summarize(run):
    rs = run["rows"]
    n = len(rs)
    ok = sum(r["ok"] for r in rs)
    full = sum(r["full_pass"] for r in rs)
    measured = [r for r in rs if r.get("undisturbed") is not None]
    disturbed = [r for r in measured if r["undisturbed"] is False]
    by = {}
    for r in disturbed:
        for k in r.get("disturbed_by") or ["unknown"]:
            by[k] = by.get(k, 0) + 1
    return {"n": n, "ok": ok, "full": full, "ci": wilson(full, n), "measured": len(measured), "disturbed": len(disturbed), "by": by,
            "wall": med([r["wall_s"] for r in rs]), "steps": med([r.get("steps") for r in rs]), "turns": med([r.get("turns") for r in rs]),
            "tokens": med([(r.get("tokens") or {}).get("total") for r in rs]), "cost": med([r.get("cost_usd") for r in rs])}


def pct(k, n):
    return "%d%% (%d/%d)" % (round(100 * k / n), k, n) if n else "-"


def report(files):
    files = files or sorted(glob.glob(os.path.join(RESULTS, "*.json")))
    runs = []
    pub = None
    for f in files:
        d = json.load(open(f))
        if d.get("schema") == "aub-published/1":
            pub = d
        elif d.get("schema") == SCHEMA:
            runs.append(d)
    groups = {}
    for r in runs:
        groups.setdefault((r["suite"], r["condition"]), []).append(r)
    for (suite, cond), rs in sorted(groups.items()):
        print("\n### %s, %s\n" % (suite, cond))
        print("| tool layer | run | model | pass (task done) | %s | disturbed | wall s (med) | steps (med) | tokens (med) |" %
              ("full pass (done + undisturbed)" if cond == "background" else "full pass"))
        print("|---|---|---|---|---|---|---|---|---|")
        for r in rs:
            s = summarize(r)
            dist = "%d/%d%s" % (s["disturbed"], s["measured"], " (" + ", ".join("%s %d" % kv for kv in sorted(s["by"].items())) + ")" if s["by"] else "") if s["measured"] else "n/a"
            print("| %s | %s | %s | %s | %s | %s | %s | %s | %s |" % (r["tool"], r["run_id"], r["model"], pct(s["ok"], s["n"]), pct(s["full"], s["n"]), dist,
                                                                     s["wall"], s["steps"], s["tokens"]))
        if pub and suite.startswith("cdb-"):
            tasks = {t for r in rs for t in (x["task"] for x in r["rows"])}
            for drv in ("cua-driver", "agent-desktop", "browser-use"):
                k, n, dn = published_rate(pub, drv, cond, tasks)
                if n:
                    print("| %s | published (Scale VM) | claude-sonnet-5-5 | %s | %s | | | | |" % (drv, pct(dn, n), pct(k, n)))
    if not groups:
        print("no results yet")


def trend(suite, tool):
    runs = [json.load(open(f)) for f in sorted(glob.glob(os.path.join(RESULTS, "*.json")))]
    runs = [r for r in runs if r.get("schema") == SCHEMA and (not suite or r["suite"] == suite) and (not tool or r["tool"] == tool)]
    runs.sort(key=lambda r: r["date"])
    print("| date | commit | suite | tool | cond | full pass | disturbed | wall s | tokens |\n|---|---|---|---|---|---|---|---|---|")
    for r in runs:
        s = summarize(r)
        print("| %s | %s | %s | %s | %s | %s | %s | %s | %s |" % (r["date"], r.get("mewla_commit", ""), r["suite"], r["tool"], r["condition"], pct(s["full"], s["n"]),
                                                              "%d/%d" % (s["disturbed"], s["measured"]) if s["measured"] else "n/a", s["wall"], s["tokens"]))


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = ap.add_subparsers(dest="cmd", required=True)
    p = sub.add_parser("published"); p.add_argument("--out", default=os.path.join(RESULTS, "published-cdb-scale.json")); p.add_argument("--rows", help="a saved JSON list of the dataset's trial rows, instead of fetching")
    p = sub.add_parser("convert-web"); p.add_argument("files", nargs="+")
    p = sub.add_parser("import-cdb"); p.add_argument("dirs", nargs="+")
    p = sub.add_parser("report"); p.add_argument("files", nargs="*")
    p = sub.add_parser("trend"); p.add_argument("--suite"); p.add_argument("--tool")
    p = sub.add_parser("check"); p.add_argument("files", nargs="+")
    a = ap.parse_args()
    os.makedirs(RESULTS, exist_ok=True)
    if a.cmd == "published":
        published(a.out, a.rows)
    elif a.cmd == "convert-web":
        convert_web(a.files)
    elif a.cmd == "import-cdb":
        import_cdb(a.dirs)
    elif a.cmd == "report":
        report(a.files)
    elif a.cmd == "trend":
        trend(a.suite, a.tool)
    elif a.cmd == "check":
        bad = 0
        for f in a.files:
            e = check(f)
            bad += bool(e)
            print(f, "ok" if not e else "; ".join(e))
        sys.exit(1 if bad else 0)


if __name__ == "__main__":
    main()
