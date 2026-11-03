#!/usr/bin/env python3
"""The agent-desktop arm of the task suite: claude -p with Bash only (no Mewla
computer MCP), the same model as the Mewla arm, the same prompts.

  arm_ad.py DIR OUT [--tasks t1,t2] [--ad PATH] [--model claude-sonnet-5-5] [--timeout 600]

Per task it writes OUT/<id>.json: wall seconds, turns, tokens, success (judged by
tasks.py check), and the Claude result's length. The answer text goes to
OUT/<id>.answer only for the check and is deleted after it. tasks.py reset runs
between tasks. Run with the Mac idle.
"""
import argparse, json, os, subprocess, sys, time

HERE = os.path.dirname(os.path.abspath(__file__))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("dir"); ap.add_argument("out")
    ap.add_argument("--tasks", default="")
    ap.add_argument("--ad", default="agent-desktop")
    ap.add_argument("--model", default="claude-sonnet-5-5")
    ap.add_argument("--timeout", type=int, default=600)
    a = ap.parse_args()
    os.makedirs(a.out, exist_ok=True)
    tasks = json.load(open(os.path.join(HERE, "tasks.json")))["tasks"]
    want = [t for t in a.tasks.split(",") if t]
    skill = subprocess.run([a.ad, "skills", "get", "desktop"], capture_output=True, text=True).stdout
    for t in tasks:
        if want and t["id"] not in want:
            continue
        subprocess.run([sys.executable, "-I", os.path.join(HERE, "tasks.py"), "reset", a.dir])
        prompt = subprocess.run([sys.executable, "-I", os.path.join(HERE, "tasks.py"), "prompt", t["id"], a.dir],
                                capture_output=True, text=True).stdout.strip()
        system = ("You operate this Mac with the agent-desktop command (%s) through Bash. Its own guide follows.\n\n%s"
                  % (a.ad, skill))
        cmd = ["claude", "-p", prompt, "--model", a.model, "--output-format", "json", "--strict-mcp-config",
               "--mcp-config", '{"mcpServers":{}}', "--allowedTools", "Bash", "--tools", "Bash",
               "--append-system-prompt", system, "--permission-mode", "bypassPermissions"]
        t0 = time.time()
        try:
            p = subprocess.run(cmd, capture_output=True, text=True, timeout=a.timeout)
            out = json.loads(p.stdout) if p.stdout.strip() else {}
        except (subprocess.TimeoutExpired, ValueError):
            out = {"timeout": True}
        wall = time.time() - t0
        ans = os.path.join(a.out, t["id"] + ".answer")
        open(ans, "w").write(str(out.get("result", "")))
        chk = json.loads(subprocess.run([sys.executable, "-I", os.path.join(HERE, "tasks.py"), "check", t["id"],
                                         a.dir, ans], capture_output=True, text=True).stdout or "{}")
        os.remove(ans)
        u = out.get("usage") or {}
        rec = {"task": t["id"], "arm": "agent-desktop", "wall_s": round(wall, 1), "turns": out.get("num_turns"),
               "api_s": round((out.get("duration_api_ms") or 0) / 1000, 1), "success": chk.get("ok"),
               "why": chk.get("why"), "result_chars": len(str(out.get("result", ""))), "timeout": out.get("timeout", False),
               "tokens": {"input": u.get("input_tokens"), "output": u.get("output_tokens"),
                          "cache_read": u.get("cache_read_input_tokens"), "cache_write": u.get("cache_creation_input_tokens")}}
        json.dump(rec, open(os.path.join(a.out, t["id"] + ".json"), "w"))
        print(json.dumps(rec, separators=(",", ":")), flush=True)


if __name__ == "__main__":
    main()
