#!/usr/bin/env python3
"""One arm of the task suite: claude -p with a single computer tool, the same
model and the same prompts for every arm.

  arm.py ad     DIR OUT [--tasks t1,t2] [--ad PATH]          Bash only, agent-desktop and its own guide
  arm.py mewla  DIR OUT [--tasks t1,t2] [--mewla PATH]       only Mewla's computer MCP tools; run inside a
                                                             live Mewla Worker (MEWLA_WORKER_ID) so the daemon
                                                             accepts the calls
  common: [--model claude-sonnet-5-5] [--timeout 600]

Per task it writes OUT/<id>.json: wall seconds, turns, tokens, success (judged by
tasks.py check), and the Claude result's length. The answer text goes to
OUT/<id>.answer only for the check and is deleted after it. tasks.py reset runs
between tasks. Run with the Mac idle.
"""
import argparse, json, os, subprocess, sys, time

HERE = os.path.dirname(os.path.abspath(__file__))


def stream(text):
    """The final result record and a trail of the tool calls: name, whether it errored, and the first 160
    characters of each result; the caller drops those heads unless the task failed (scratch tasks only)."""
    out, trail, names = {}, [], {}
    for line in text.splitlines():
        try:
            o = json.loads(line)
        except ValueError:
            continue
        if o.get("type") == "result":
            out = o
        for c in ((o.get("message") or {}).get("content") or []):
            if not isinstance(c, dict):
                continue
            if c.get("type") == "tool_use":
                inp = c.get("input") or {}
                names[c.get("id")] = len(trail)
                trail.append({"tool": str(c.get("name", "")).replace("mcp__mewla-computer__", ""),
                              "action": inp.get("action") or inp.get("tool") or None})
            elif c.get("type") == "tool_result" and c.get("tool_use_id") in names:
                body = c.get("content")
                if isinstance(body, list):
                    body = " ".join(str(x.get("text", "")) for x in body if isinstance(x, dict))
                trail[names[c["tool_use_id"]]]["head"] = str(body).strip()[:160]
                if c.get("is_error"):
                    trail[names[c["tool_use_id"]]]["error"] = True
    return out, trail


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("arm", choices=["ad", "mewla"])
    ap.add_argument("dir"); ap.add_argument("out")
    ap.add_argument("--mewla", default="mewla")
    ap.add_argument("--tasks", default="")
    ap.add_argument("--ad", default="agent-desktop")
    ap.add_argument("--model", default="claude-sonnet-5-5")
    ap.add_argument("--timeout", type=int, default=600)
    a = ap.parse_args()
    os.makedirs(a.out, exist_ok=True)
    tasks = json.load(open(os.path.join(HERE, "tasks.json")))["tasks"]
    want = [t for t in a.tasks.split(",") if t]
    skill = ""
    if a.arm == "ad":
        skill = subprocess.run([a.ad, "skills", "get", "desktop"], capture_output=True, text=True).stdout
    elif not os.environ.get("MEWLA_WORKER_ID"):
        sys.exit("the mewla arm runs inside a Mewla Worker (MEWLA_WORKER_ID is not set)")
    for t in tasks:
        if want and t["id"] not in want:
            continue
        subprocess.run([sys.executable, "-I", os.path.join(HERE, "tasks.py"), "reset", a.dir])
        prompt = subprocess.run([sys.executable, "-I", os.path.join(HERE, "tasks.py"), "prompt", t["id"], a.dir],
                                capture_output=True, text=True).stdout.strip()
        if a.arm == "ad":
            system = ("You operate this Mac with the agent-desktop command (%s) through Bash. Its own guide follows.\n\n%s"
                      % (a.ad, skill))
            tools = ["--allowedTools", "Bash", "--tools", "Bash", "--strict-mcp-config", "--mcp-config", '{"mcpServers":{}}']
        else:
            system = "You operate this Mac with the computer tools you have (apps, app_state, element, menu, steps, computer)."
            config = json.dumps({"mcpServers": {"mewla-computer": {"command": a.mewla, "args": ["agent-use", "computer-mcp"]}}})
            tools = ["--tools", "", "--allowedTools", "mcp__mewla-computer", "--strict-mcp-config", "--mcp-config", config]
        cmd = ["claude", "-p", prompt, "--model", a.model, "--output-format", "stream-json", "--verbose"] + tools + [
            "--append-system-prompt", system, "--permission-mode", "bypassPermissions"]
        t0 = time.time()
        try:
            p = subprocess.run(cmd, capture_output=True, text=True, timeout=a.timeout)
            out, trail = stream(p.stdout)
        except subprocess.TimeoutExpired as e:
            out, trail = stream((e.stdout or b"").decode() if isinstance(e.stdout, bytes) else (e.stdout or ""))
            out["timeout"] = True
        wall = time.time() - t0
        ans = os.path.join(a.out, t["id"] + ".answer")
        open(ans, "w").write(str(out.get("result", "")))
        chk = json.loads(subprocess.run([sys.executable, "-I", os.path.join(HERE, "tasks.py"), "check", t["id"],
                                         a.dir, ans], capture_output=True, text=True).stdout or "{}")
        os.remove(ans)
        u = out.get("usage") or {}
        rec = {"trail": trail, "task": t["id"], "arm": a.arm, "wall_s": round(wall, 1), "turns": out.get("num_turns"),
               "api_s": round((out.get("duration_api_ms") or 0) / 1000, 1), "success": chk.get("ok"),
               "why": chk.get("why"), "result_chars": len(str(out.get("result", ""))), "timeout": out.get("timeout", False),
               "tokens": {"input": u.get("input_tokens"), "output": u.get("output_tokens"),
                          "cache_read": u.get("cache_read_input_tokens"), "cache_write": u.get("cache_creation_input_tokens")}}
        if chk.get("ok") is not False:
            for step in rec["trail"]:
                step.pop("head", None)
        else:
            rec["final_text"] = str(out.get("result", ""))[:400]
        json.dump(rec, open(os.path.join(a.out, t["id"] + ".json"), "w"))
        print(json.dumps(rec, separators=(",", ":")), flush=True)


if __name__ == "__main__":
    main()
