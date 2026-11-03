#!/usr/bin/env python3
"""A fake `mewla agent-use computer-mcp`: answers every tools/call with text."""
import json, sys, time
for line in sys.stdin:
    m = json.loads(line)
    if "id" not in m:
        continue
    if m["method"] == "initialize":
        r = {"protocolVersion": "2025-06-18", "capabilities": {}, "serverInfo": {"name": "fake"}}
    else:
        name = m["params"]["name"]
        time.sleep(0.002)
        text = "Apps: Finder, System Settings, Notes, TextEdit, Calculator" if name == "apps" else \
            "App: X, pid 1\n[1] button \"1\"\n  [2] textarea \"\"\n(the app has more than was read)\n"
        r = {"content": [{"type": "text", "text": text}]}
    sys.stdout.write(json.dumps({"jsonrpc": "2.0", "id": m["id"], "result": r}) + "\n")
    sys.stdout.flush()
