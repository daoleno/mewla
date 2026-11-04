#!/usr/bin/env python3
"""Which sequence of Mewla computer tools really renames a file in Finder? No model: each variant
renames its own scratch file and the script checks the disk. Run inside a Mewla Worker on the Mac
(needs MEWLA_WORKER_ID), Mac idle:

  finder_rename_probe.py DIR [--mewla PATH]

DIR is a new scratch folder (made here). Prints one JSON line per variant: the steps' answer heads
(first 90 characters, scratch names only) and whether the new name exists. Standard library only."""
import json, os, re, subprocess, sys, time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from bench import MCP  # noqa: E402

VARIANTS = ["confirm", "return", "focus-return", "select-return-type", "context-menu"]


def head(r):
    return " ".join(r["text"].split())[:90]


def field(m, name):
    r = m.call("app_state", {"app": "Finder", "match": {"role": "TextField", "value": name}})
    found = re.search(r"\[(\d+)\] TextField = \"%s\"" % re.escape(name), r["text"])
    return (int(found.group(1)) if found else None), r


def key(m, combo):
    return m.call("computer", {"action": "key", "text": combo, "app": "Finder"})


def main():
    d = os.path.abspath(sys.argv[1])
    binary = sys.argv[sys.argv.index("--mewla") + 1] if "--mewla" in sys.argv else "mewla"
    os.makedirs(d)
    for i, v in enumerate(VARIANTS):
        open(os.path.join(d, "f%d.txt" % i), "w").write("x")
    subprocess.run(["open", d])
    time.sleep(1.5)
    m = MCP([binary, "agent-use", "computer-mcp"])
    try:
        m.call("app_state", {"app": "Finder"})
        for i, v in enumerate(VARIANTS):
            old, new = "f%d.txt" % i, "g%d.txt" % i
            steps = []
            fid, r = field(m, old)
            steps.append(["find", head(r)])
            if fid is None:
                print(json.dumps({"variant": v, "steps": steps, "renamed": False, "why": "no field"})); continue
            if v == "confirm":
                steps.append(["set_value", head(m.call("element", {"id": fid, "action": "set_value", "value": new}))])
                steps.append(["confirm", head(m.call("element", {"id": fid, "action": "confirm"}))])
            elif v == "return":
                steps.append(["set_value", head(m.call("element", {"id": fid, "action": "set_value", "value": new}))])
                steps.append(["key Return", head(key(m, "Return"))])
            elif v == "focus-return":
                steps.append(["focus", head(m.call("element", {"id": fid, "action": "focus"}))])
                steps.append(["key Return", head(key(m, "Return"))])
                steps.append(["key cmd+a", head(key(m, "cmd+a"))])
                steps.append(["type", head(m.call("computer", {"action": "type", "text": new, "app": "Finder"}))])
                steps.append(["key Return", head(key(m, "Return"))])
            elif v == "select-return-type":
                steps.append(["press", head(m.call("element", {"id": fid, "action": "press"}))])
                steps.append(["key Return", head(key(m, "Return"))])
                steps.append(["key cmd+a", head(key(m, "cmd+a"))])
                steps.append(["type", head(m.call("computer", {"action": "type", "text": new, "app": "Finder"}))])
                steps.append(["key Return", head(key(m, "Return"))])
            elif v == "context-menu":
                steps.append(["show_menu", head(m.call("element", {"id": fid, "action": "show_menu"}))])
                steps.append(["File menu", head(m.call("menu", {"app": "Finder", "path": "File >"}))])
            time.sleep(1.0)
            print(json.dumps({"variant": v, "steps": steps, "renamed": os.path.exists(os.path.join(d, new))
                              and not os.path.exists(os.path.join(d, old))}), flush=True)
            subprocess.run(["osascript", "-e", 'tell application "Finder" to activate'], capture_output=True)
    finally:
        m.close()


if __name__ == "__main__":
    main()
