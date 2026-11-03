#!/usr/bin/env python3
"""The task suite: identical prompts for every arm, and checks that judge success.

  tasks.py setup DIR             create the scratch files; writes DIR/expected.json
  tasks.py prompt ID DIR         the prompt for a task, with DIR filled in
  tasks.py check ID DIR ANSWER   exit 0 and print {"ok": true} when the task's outcome holds
  tasks.py reset DIR             put the scratch files back (between arms)
  tasks.py cleanup DIR           delete the scratch directory (scratch only)
Fill DIR/expected.json's ELECTRON_* and CALENDAR_EXPECTED by hand where setup says so.
Standard library only; prints no file content other than what a check compares.
"""
import json, os, platform, subprocess, sys

HERE = os.path.dirname(os.path.abspath(__file__))
TASKS = {t["id"]: t for t in json.load(open(os.path.join(HERE, "tasks.json")))["tasks"]}


def expected(d):
    p = os.path.join(d, "expected.json")
    return json.load(open(p)) if os.path.exists(p) else {}


def fill(s, d):
    e = dict(expected(d), S=d)
    for k, v in e.items():
        s = s.replace("{%s}" % k, str(v))
    return s


def reset(d):
    os.makedirs(os.path.join(d, "many"), exist_ok=True)
    open(os.path.join(d, "scratch.txt"), "w").write("hello")
    for f in ("renamed.txt", "rename-me.txt"):
        if os.path.exists(os.path.join(d, f)):
            os.remove(os.path.join(d, f))
    open(os.path.join(d, "rename-me.txt"), "w").write("x")


def setup(d):
    reset(d)
    many = os.path.join(d, "many")
    names = ["a%d.txt" % i for i in range(11)] + ["largest-file.txt"]
    for n in names:
        open(os.path.join(many, n), "w").write("y" * (5000 if n.startswith("largest") else 3))
    version = ""
    if platform.system() == "Darwin":
        version = subprocess.run(["sw_vers", "-productVersion"], capture_output=True, text=True).stdout.strip()
    e = {"MANY_COUNT": str(len(names)), "MANY_LARGEST": "largest-file", "MACOS_VERSION": version,
         "ELECTRON_APP": "TODO", "ELECTRON_QUESTION": "TODO", "ELECTRON_EXPECTED": "TODO",
         "CALENDAR_EXPECTED": "TODO"}
    json.dump(e, open(os.path.join(d, "expected.json"), "w"), indent=1)
    print("fill the TODO entries in %s/expected.json" % d)


def check(tid, d, answer):
    c = TASKS[tid]["check"]
    text = open(answer).read().lower() if answer and os.path.exists(answer) else ""
    ok, why = True, []
    if "file" in c:
        try:
            got = open(fill(c["file"], d)).read().strip()
        except OSError:
            got = None
        if got != c["equals"]:
            ok = False; why.append("file differs")
    if "exists" in c and not os.path.exists(fill(c["exists"], d)):
        ok = False; why.append("missing " + os.path.basename(c["exists"]))
    if "absent" in c and os.path.exists(fill(c["absent"], d)):
        ok = False; why.append("still there " + os.path.basename(c["absent"]))
    for want in c.get("answer_contains", []):
        w = fill(want, d).lower()
        if "todo" in w or w not in text:
            ok = False; why.append("answer lacks %r" % w[:20])
    if "manual" in c:
        ok, why = None, ["manual: " + c["manual"]]
    return {"ok": ok, "why": why}


if __name__ == "__main__":
    a = sys.argv[1:]
    if len(a) >= 2 and a[0] == "setup":
        setup(a[1])
    elif len(a) == 3 and a[0] == "prompt":
        print(fill(TASKS[a[1]]["prompt"], a[2]))
    elif len(a) >= 3 and a[0] == "check":
        r = check(a[1], a[2], a[3] if len(a) > 3 else "")
        print(json.dumps(r)); sys.exit(0 if r["ok"] else 1)
    elif len(a) == 2 and a[0] == "reset":
        reset(a[1])
    elif len(a) == 2 and a[0] == "cleanup":
        import shutil; shutil.rmtree(a[1], ignore_errors=True)
    else:
        sys.exit(__doc__)
