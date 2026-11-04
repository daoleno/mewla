#!/usr/bin/env python3
"""Measure one task run by the Codex Desktop app from its own session log.

  codex_desktop.py begin                      note the time, before the prompt is given to Codex.app
  codex_desktop.py end TASK_ID DIR OUT.json   after the turn has ended: find the rollout this run wrote
                                              (the newest one touched since begin whose originator is Codex
                                              Desktop), measure it with metrics.py and judge success with tasks.py

The prompt itself comes from `tasks.py prompt TASK_ID DIR`; a claude Worker with Mewla's
computer tools puts it into Codex.app (new chat, composer, send) and waits for the turn to end.
Nothing from the log but counts, timings and the pass/fail is written.
"""
import glob, json, os, subprocess, sys, tempfile, time

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import metrics

SESSIONS = os.path.expanduser("~/.codex/sessions")
STATE = os.path.join(tempfile.gettempdir(), "codex-desktop-begin.json")


def rollouts_since(t0):
    out = []
    for path in glob.glob(SESSIONS + "/**/rollout-*.jsonl", recursive=True):
        if os.path.getmtime(path) >= t0 - 1:
            out.append(path)
    return sorted(out, key=os.path.getmtime)


def meta_of(path):
    for line in open(path):
        try:
            o = json.loads(line)
        except ValueError:
            continue
        if o.get("type") == "session_meta":
            return o.get("payload") or {}
    return {}


def last_answer(path):
    answer = ""
    for line in open(path):
        try:
            o = json.loads(line)
        except ValueError:
            continue
        p = o.get("payload") or {}
        if p.get("type") == "agent_message":
            answer = p.get("message", answer)
        elif p.get("type") == "task_complete" and p.get("last_agent_message"):
            answer = p["last_agent_message"]
    return answer


def main():
    a = sys.argv[1:]
    if a[:1] == ["begin"]:
        json.dump({"t0": time.time()}, open(STATE, "w"))
        return print("noted")
    if a[:1] == ["end"] and len(a) == 4:
        task, d, out = a[1:]
        t0 = json.load(open(STATE))["t0"]
        desktop = [p for p in rollouts_since(t0) if "desktop" in str(meta_of(p).get("originator", "")).lower()]
        if not desktop:
            sys.exit("no Codex Desktop rollout was written since begin")
        path = desktop[-1]
        m = metrics.codex(path, t0 - 1, None)
        ans = tempfile.mktemp(prefix="codex-answer-")
        open(ans, "w").write(last_answer(path))
        chk = json.loads(subprocess.run([sys.executable, "-I", os.path.join(HERE, "tasks.py"), "check", task, d, ans],
                                        capture_output=True, text=True).stdout or "{}")
        os.remove(ans)
        rec = {"task": task, "arm": "codex-desktop", "originator": meta_of(path).get("originator"),
               "model": m.get("model"), "wall_s": m["wall_s"], "turns": m["turns"], "tool_calls": m["tool_calls"],
               "tools": m["tools"], "tokens": m["tokens"], "success": chk.get("ok"), "why": chk.get("why")}
        runs = json.load(open(out)) if os.path.exists(out) else []
        runs.append(rec)
        json.dump(runs, open(out, "w"), indent=1)
        return print(json.dumps({k: rec[k] for k in ("task", "wall_s", "turns", "tool_calls", "success")}))
    sys.exit(__doc__)


if __name__ == "__main__":
    main()
