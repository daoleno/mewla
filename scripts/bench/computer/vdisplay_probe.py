#!/usr/bin/env python3
"""Run inside a Mewla Worker while the virtual display spike holds a display
(MEWLA_VDISPLAY_SPIKE=<secs> MEWLA_VDISPLAY_MOVE=<pid> go test ./screen -run
VirtualDisplaySpike -v, in daemon/). Tries each way of acting on an app whose
window sits on that display and prints one timestamped line per attempt; the
spike prints the pointer's and the front app's changes with timestamps, so the
two logs together say which path moved the person's cursor or took their focus.

  vdisplay_probe.py APP [--mewla mewla]
Only app_state, element set_value/press and keys/clicks to that app; no text
is read out of the app. Run with nobody at the keyboard.
"""
import argparse, json, os, sys, time
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from bench import MCP


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("app")
    ap.add_argument("--mewla", default="mewla")
    a = ap.parse_args()
    if not os.environ.get("MEWLA_WORKER_ID"):
        sys.exit("run this inside a Mewla Worker")
    m = MCP([a.mewla, "agent-use", "computer-mcp"])

    def attempt(name, tool, args):
        t = time.strftime("%H:%M:%S") + ".%03d" % (int(time.time() * 1000) % 1000)
        r = m.call(tool, args)
        print(json.dumps({"at": t, "path": name, "ms": round(r["ms"]), "error": r["error"],
                          "answer_head": r["text"][:110].replace("\n", " | ")}), flush=True)
        time.sleep(1.0)  # leave a gap so the spike's log separates the paths
        return r

    attempt("read (apps)", "apps", {})
    attempt("read app_state", "app_state", {"app": a.app})
    attempt("AX set_value on a text area", "steps", {"steps": [
        {"action": "element", "app": a.app, "match": {"role": "TextArea"}, "element_action": "set_value", "value": "vdisplay probe"}]})
    attempt("AX press (first Button)", "steps", {"steps": [
        {"action": "app_state", "app": a.app, "match": {"role": "Button"}}]})
    attempt("keys to the pid (type)", "computer", {"action": "type", "text": "x", "app": a.app})
    attempt("keys to the pid (key cmd+a)", "computer", {"action": "key", "text": "cmd+a", "app": a.app})
    attempt("menu by name (list)", "menu", {"app": a.app})
    attempt("background click at (200,200)", "computer", {"action": "left_click", "coordinate": [200, 200], "app": a.app})
    attempt("window screenshot", "computer", {"action": "screenshot", "app": a.app})
    attempt("screen screenshot", "computer", {"action": "screenshot"})
    m.close()


if __name__ == "__main__":
    main()
