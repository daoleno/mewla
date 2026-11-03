#!/usr/bin/env python3
"""Primitive timings for Mac computer use: Mewla's tools and agent-desktop on
the same apps. Standard library only, so it can be pasted onto a Mac.

  bench.py mewla  -n 20 --out DIR   run inside a live Mewla Worker (needs MEWLA_WORKER_ID)
  bench.py ad     -n 20 --out DIR   needs agent-desktop on PATH
  bench.py report DIR [DIR...]      p50/p95 table as markdown + small JSON

Mewla is driven over MCP stdio exactly as an agent does (`mewla agent-use
computer-mcp`), so every call includes the MCP and control-socket hops, which
is the fair match for agent-desktop's per-command cost. No model is involved.
Run it while the Mac is idle: a sample where Mewla says the agent was paused
(a person used the keyboard) is dropped and counted, never timed.
Output holds timings and sizes only, never what an app showed.
"""
import argparse, json, os, shutil, statistics, subprocess, sys, time

TRUNC = ("more than was read", "not shown", "left out", "truncated")
DEFAULT_APPS = ["Finder", "System Settings", "Notes", "TextEdit"]
ELECTRON = ["Slack", "Cursor", "Visual Studio Code", "Discord", "Notion", "Obsidian"]


def pct(xs, p):
    xs = sorted(xs)
    if not xs:
        return None
    return xs[min(len(xs) - 1, max(0, int(round(p / 100 * len(xs) + 0.4999)) - 1))]


class MCP:
    """Just enough MCP client: initialize, tools/call, over newline JSON."""

    def __init__(self, cmd):
        self.p = subprocess.Popen(cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                  stderr=subprocess.DEVNULL, bufsize=0)
        self.n = 0
        self._buf = b""
        self.rpc("initialize", {"protocolVersion": "2025-06-18", "capabilities": {},
                                "clientInfo": {"name": "computer-bench", "version": "1"}})
        self._send({"jsonrpc": "2.0", "method": "notifications/initialized"})

    def _send(self, obj):
        self.p.stdin.write((json.dumps(obj) + "\n").encode())
        self.p.stdin.flush()

    def _line(self):
        while b"\n" not in self._buf:
            chunk = os.read(self.p.stdout.fileno(), 1 << 20)
            if not chunk:
                raise RuntimeError("mcp server exited")
            self._buf += chunk
        line, self._buf = self._buf.split(b"\n", 1)
        return line

    def rpc(self, method, params):
        self.n += 1
        self._send({"jsonrpc": "2.0", "id": self.n, "method": method, "params": params})
        while True:
            msg = json.loads(self._line())
            if msg.get("id") == self.n:
                return msg

    def call(self, name, args):
        t = time.perf_counter()
        msg = self.rpc("tools/call", {"name": name, "arguments": args})
        ms = (time.perf_counter() - t) * 1000
        res = msg.get("result") or {}
        text, img = "", 0
        for c in res.get("content", []):
            if c.get("type") == "text":
                text += c.get("text", "")
            elif c.get("type") == "image":
                img += len(c.get("data", "")) * 3 // 4
        err = bool(res.get("isError")) or "error" in msg
        if "error" in msg:
            text = json.dumps(msg["error"])
        return {"ms": ms, "bytes": len(text.encode()), "image_bytes": img, "error": err,
                "paused": "paused" in text.lower() and "agent" in text.lower(), "text": text}

    def close(self):
        try:
            self.p.stdin.close()
            self.p.terminate()
        except Exception:
            pass


class Rec:
    """Samples per primitive. Text is used for sizing and then dropped."""

    def __init__(self):
        self.s = {}

    def add(self, name, r, calls=1, rows=None):
        e = self.s.setdefault(name, {"ms": [], "bytes": [], "rows": [], "first_ms": None,
                                     "trunc": 0, "errors": 0, "paused_dropped": 0, "calls": calls, "codes": {}})
        if r.get("paused"):
            e["paused_dropped"] += 1
            return
        if r.get("error"):
            e["errors"] += 1
            code = str(r.get("code") or "error")[:60]
            e["codes"][code] = e["codes"].get(code, 0) + 1  # which error, never its text
            return
        if e["first_ms"] is None:
            e["first_ms"] = r["ms"]  # cold; kept apart from the warm samples
        else:
            e["ms"].append(r["ms"])
        e["bytes"].append(r["bytes"] + r.get("image_bytes", 0))
        if rows is not None:
            e["rows"].append(rows)
        e["trunc"] += int(r.get("trunc", False))
        e["calls"] = calls

    def summary(self):
        out = {}
        for k, e in self.s.items():
            out[k] = {"n": len(e["ms"]), "first_ms": round(e["first_ms"], 1) if e["first_ms"] else None,
                      "p50_ms": round(pct(e["ms"], 50), 1) if e["ms"] else None,
                      "p95_ms": round(pct(e["ms"], 95), 1) if e["ms"] else None,
                      "bytes_p50": pct(e["bytes"], 50), "rows_p50": pct(e["rows"], 50),
                      "truncated": e["trunc"], "errors": e["errors"],
                      "paused_dropped": e["paused_dropped"], "calls_per_sample": e["calls"],
                      "error_codes": e["codes"]}
        return out


def rows_of(text):
    return sum(1 for l in text.splitlines() if l.lstrip().startswith("["))


def is_trunc(text):
    return any(m in text for m in TRUNC)


# ---- Mewla ----------------------------------------------------------------

def mewla_run(n, apps, cmd, scratch):
    m = MCP(cmd)
    rec = Rec()
    try:
        def go(name, tool, args, rowsf=False):
            r = m.call(tool, args)
            r["trunc"] = is_trunc(r["text"])
            rec.add(name, r, rows=rows_of(r["text"]) if rowsf else None)
            return r
        listing = m.call("apps", {})["text"]
        present = [a for a in apps if a.lower() in listing.lower()]
        skipped = [a for a in apps if a not in present]
        for _ in range(n):
            go("apps", "apps", {})
        for a in present:
            m.call("app_state", {"app": a})  # warm the app, not counted twice
            for _ in range(n):
                go("app_state/" + a, "app_state", {"app": a}, rowsf=True)
        for a in present:
            for _ in range(n):
                go("find/" + a, "steps", {"steps": [{"action": "expect", "app": a, "match": {"role": "Button"}}]})
        if "TextEdit" in present:
            m.call("app_state", {"app": "TextEdit"})
            for _ in range(n):
                go("wait_for(true)/TextEdit", "steps", {"steps": [
                    {"action": "wait_for", "app": "TextEdit", "match": {"role": "TextArea"}, "duration": 3}]})
            for i in range(n):
                go("set_value->answer/TextEdit", "steps", {"steps": [
                    {"action": "element", "app": "TextEdit", "match": {"role": "TextArea"},
                     "element_action": "set_value", "value": "bench %d" % i}]})
            for _ in range(n):  # find, set, read, check in one call
                go("steps(4)/TextEdit", "steps", {"steps": [
                    {"action": "expect", "app": "TextEdit", "match": {"role": "TextArea"}},
                    {"action": "element", "match": {"role": "TextArea"}, "element_action": "set_value", "value": "x"},
                    {"action": "element", "match": {"role": "TextArea"}, "element_action": "read"},
                    {"action": "expect", "match": {"role": "TextArea"}}]})
        if "Calculator" in present:
            m.call("app_state", {"app": "Calculator"})
            for _ in range(n):
                go("press->answer/Calculator", "steps", {"steps": [
                    {"action": "element", "app": "Calculator", "match": {"role": "Button", "title": "1"},
                     "element_action": "press"}]})
        for _ in range(n):
            go("screenshot/screen", "computer", {"action": "screenshot"})
        for a in present[:1]:
            for _ in range(n):
                go("screenshot/app", "computer", {"action": "screenshot", "app": a})
        return {"tool": "mewla", "n": n, "skipped_apps": skipped, "primitives": rec.summary()}
    finally:
        m.close()


# ---- agent-desktop ---------------------------------------------------------

def sh(args, timeout=60):
    t = time.perf_counter()
    p = subprocess.run(args, capture_output=True, text=True, timeout=timeout)
    ms = (time.perf_counter() - t) * 1000
    try:
        data = json.loads(p.stdout)
    except Exception:
        data = {}
    err = data.get("error") or {}
    code = err.get("code") if isinstance(err, dict) else None
    if not data:
        code = "exit%d_nojson" % p.returncode
    elif not code and not data.get("ok", False):
        code = "not_ok"
    return {"ms": ms, "bytes": len(p.stdout.encode()), "error": not data.get("ok", False), "json": data,
            "text": p.stdout, "code": code}


def walk(node, out):
    if isinstance(node, dict):
        if node.get("ref_id"):
            out.append(node)
        for v in node.values():
            walk(v, out)
    elif isinstance(node, list):
        for v in node:
            walk(v, out)


def ad_matches(r):
    """agent-desktop's find answers with data.matches (a list) or, with --first,
    data.match (one); read either."""
    d = r["json"].get("data", {}) or {}
    if d.get("matches"):
        return d["matches"]
    return [d["match"]] if d.get("match") else []


def window_ids(node, out):
    if isinstance(node, dict):
        for v in node.values():
            window_ids(v, out)
    elif isinstance(node, list):
        for v in node:
            window_ids(v, out)
    elif isinstance(node, str) and node.startswith("w-") and node not in out:
        out.append(node)


def ad_run(n, apps, ad, scratch):
    rec = Rec()
    # list-apps can fail on its own (application_inventory_unstable), so every
    # named app is tried; one that isn't running shows as errors with codes.
    present, skipped = list(apps), []

    # An app with several windows is AMBIGUOUS_TARGET without --window-id: take
    # the first candidate the error names, as an agent would after reading it.
    target = {}

    def scoped(args):
        app = args[args.index("--app") + 1] if "--app" in args else None
        if app in target:
            return args + ["--window-id", target[app]]
        return args

    def run(args):
        r = sh([ad] + scoped(args))
        app = args[args.index("--app") + 1] if "--app" in args else None
        if r["code"] == "AMBIGUOUS_TARGET" and app and app not in target:
            ids = []
            window_ids(r["json"].get("error", {}), ids)
            if ids:
                target[app] = ids[0]
                r = sh([ad] + scoped(args))
        return r

    def go(name, args, rows=None):
        r = run(args)
        r["trunc"] = bool(r["json"].get("data", {}).get("truncated")) or r["json"].get("data", {}).get("complete") is False
        rec.add(name, r, rows=rows)
        return r
    for _ in range(n):
        go("apps", ["list-apps"])
    for a in present:
        run(["snapshot", "--app", a, "-i", "--compact"])
        for _ in range(n):
            r = run(["snapshot", "--app", a, "-i", "--compact"])
            refs = []
            walk(r["json"].get("data", {}).get("tree", {}), refs)
            r["trunc"] = r["json"].get("data", {}).get("complete") is False
            r["trunc"] = r["json"].get("data", {}).get("complete") is False
            rec.add("app_state/" + a, r, rows=len(refs))
    for a in present:
        for _ in range(n):
            go("find/" + a, ["find", "--app", a, "--role", "button", "--first"])
    if "TextEdit" in present:
        for _ in range(n):
            go("wait_for(true)/TextEdit", ["wait", "--window", "", "--app", "TextEdit", "--timeout", "3000"])
        # set-value then the snapshot their docs say to take to verify
        for i in range(n):
            f = run(["find", "--app", "TextEdit", "--role", "textfield", "--first"])
            ms0, by0 = f["ms"], f["bytes"]
            ms = by = 0
            matches = ad_matches(f)
            if not matches:
                rec.add("set_value->answer/TextEdit", {"error": True, "ms": 0, "bytes": 0, "code": f["code"] or "no_match"})
                continue
            ref = matches[0]["ref_id"]
            s = sh([ad, "set-value", ref, "bench %d" % i])
            v = run(["snapshot", "--app", "TextEdit", "-i", "--compact"])
            rec.add("set_value->answer/TextEdit", {"ms": s["ms"] + v["ms"], "bytes": s["bytes"] + v["bytes"],
                                                   "error": s["error"], "code": s["code"]}, calls=2)
            rec.add("steps(4)/TextEdit", {"ms": ms0 + s["ms"] + v["ms"] + 0, "bytes": by0 + s["bytes"] + v["bytes"],
                                          "error": s["error"]}, calls=3)
    if "Calculator" in present:
        for _ in range(n):
            f = run(["find", "--app", "Calculator", "--role", "button", "--name", "1", "--exact", "--first"])
            matches = ad_matches(f)
            if not matches:
                rec.add("press->answer/Calculator", {"error": True, "ms": 0, "bytes": 0, "code": f["code"] or "no_match"})
                continue
            c = sh([ad, "click", matches[0]["ref_id"]])
            v = run(["snapshot", "--app", "Calculator", "-i", "--compact"])
            rec.add("press->answer/Calculator", {"ms": f["ms"] + c["ms"] + v["ms"],
                                                 "bytes": f["bytes"] + c["bytes"] + v["bytes"],
                                                 "error": c["error"], "code": c["code"]}, calls=3)
    os.makedirs(scratch, exist_ok=True)
    shot = os.path.join(scratch, "shot.png")
    for _ in range(n):
        r = sh([ad, "screenshot", "--screen", "0", shot])
        r["bytes"] = os.path.getsize(shot) if os.path.exists(shot) else r["bytes"]
        rec.add("screenshot/screen", r)
    if present:
        for _ in range(n):
            r = run(["screenshot", "--app", present[0], shot])
            r["bytes"] = os.path.getsize(shot) if os.path.exists(shot) else r["bytes"]
            rec.add("screenshot/app", r)
    version = sh([ad, "version"])["text"].strip()[:200]
    return {"tool": "agent-desktop", "n": n, "version": version,
            "launcher": shutil.which("agent-desktop"), "skipped_apps": skipped, "primitives": rec.summary()}


# ---- report ----------------------------------------------------------------

def report(dirs):
    runs = []
    for d in dirs:
        for f in sorted(os.listdir(d)):
            if f.endswith(".json") and f.startswith("prims-"):
                runs.append((f[6:-5], json.load(open(os.path.join(d, f)))))
    names = sorted({k for _, r in runs for k in r["primitives"]})
    head = "| primitive | " + " | ".join("%s p50 / p95 ms (bytes, rows)" % l for l, _ in runs) + " |"
    lines = [head, "|---|" + "---|" * len(runs)]
    for k in names:
        cells = []
        for _, r in runs:
            p = r["primitives"].get(k)
            cells.append("-" if not p or p["p50_ms"] is None else "%s / %s (%s, %s)%s" % (
                p["p50_ms"], p["p95_ms"], p["bytes_p50"], p["rows_p50"] if p["rows_p50"] is not None else "-",
                " T" if p["truncated"] else ""))
        lines.append("| %s | %s |" % (k, " | ".join(cells)))
    print("\n".join(lines))


def audit(path):
    """Server-side time per action from Mewla's audit file (lengths and times
    only): p50/p95 of ms, and bytes, by action and app, to tell the daemon's
    time from the transport's. Run it after bench.py mewla; it reads the whole file."""
    by = {}
    for line in open(path):
        try:
            o = json.loads(line)
        except ValueError:
            continue
        if "ms" not in o:
            continue
        by.setdefault((o.get("action"), o.get("app") or ""), []).append((o["ms"], o.get("bytes", 0), o.get("rows", 0)))
    out = {}
    for (action, app), v in sorted(by.items()):
        out["%s/%s" % (action, app)] = {"n": len(v), "p50_ms": pct([x[0] for x in v], 50), "p95_ms": pct([x[0] for x in v], 95),
                                         "bytes_p50": pct([x[1] for x in v], 50), "rows_p50": pct([x[2] for x in v], 50)}
    print(json.dumps(out, separators=(",", ":")))


def dump_tools(cmd, out):
    """tools/list of any MCP server (no tool is called): names, descriptions and
    input schemas to OUT/tools.json, and a one-line summary on stdout."""
    import shlex
    m = MCP(shlex.split(cmd))
    try:
        res = m.rpc("tools/list", {}).get("result", {})
    finally:
        m.close()
    os.makedirs(out, exist_ok=True)
    json.dump(res, open(os.path.join(out, "tools.json"), "w"), indent=1)
    print(json.dumps([{"name": t.get("name"), "desc_chars": len(t.get("description", "")),
                       "args": sorted((t.get("inputSchema") or {}).get("properties", {}))} for t in res.get("tools", [])]))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("mode", choices=["mewla", "ad", "report", "tools", "audit"])
    ap.add_argument("dirs", nargs="*")
    ap.add_argument("-n", type=int, default=20)
    ap.add_argument("--out", default=".")
    ap.add_argument("--apps", default=",".join(DEFAULT_APPS))
    ap.add_argument("--mewla", default="mewla")
    ap.add_argument("--ad", default="agent-desktop")
    ap.add_argument("--label", default=None)
    ap.add_argument("--cmd", default="", help="tools: the MCP server command, run as one shell-split command")
    a = ap.parse_args()
    if a.mode == "report":
        return report(a.dirs)
    if a.mode == "audit":
        return audit(a.dirs[0] if a.dirs else os.path.expanduser("~/.mewla/screen-agent-audit.jsonl"))
    if a.mode == "tools":
        return dump_tools(a.cmd, a.out)
    apps = [x for x in a.apps.split(",") if x]
    os.makedirs(a.out, exist_ok=True)
    scratch = os.path.join(a.out, "scratch")
    if a.mode == "mewla":
        if not os.environ.get("MEWLA_WORKER_ID"):
            sys.exit("run this inside a Mewla Worker (MEWLA_WORKER_ID is not set)")
        res = mewla_run(a.n, apps, [a.mewla, "agent-use", "computer-mcp"], scratch)
    else:
        res = ad_run(a.n, apps, a.ad, scratch)
    label = a.label or res["tool"]
    json.dump(res, open(os.path.join(a.out, "prims-%s.json" % label), "w"), indent=1)
    print(json.dumps(res["primitives"], separators=(",", ":")))


if __name__ == "__main__":
    main()
