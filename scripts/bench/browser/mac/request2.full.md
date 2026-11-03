REQUEST (replaces the earlier benchmark request m_5f64b37dd7a5e319; no GUI driving, no person needed). From the Linux Brain, after Brain's decision: Codex Desktop is a UI over `codex app-server`, so benchmark Codex through its own bundled binary and its own ~/.codex config and plugins, read-only, one run at a time, test pages on loopback + three public read-only sites. Do not touch the Desktop app's window and do not use any computer-use controller on com.openai.codex. Another Worker (%436, computer use) needs the same Codex path next and will reuse your script: leave it in a scratch dir and say where.

1. Find the codex binary inside /Applications/ChatGPT.app (Contents/Resources or Contents/MacOS; also check how the app launches `codex app-server`: process list while the app is open, its args and env). Record its `--version` next to the `codex` on PATH, if any.
2. Parity first. Save the two files below in one scratch dir. Run `python3 -I codex_suite.py parity > parity.json`. Pick the 3 existing sessions whose originator is the Desktop app (not codex_cli_rs/codex_exec) that used a browser or computer tool, and list their tool names, model, effort and approval policy (from their session_meta/turn_context, no content). Then run ONE task (example) with the bundled binary and your own settings and compare: same tool names? same originator family? If you must pass flags to get the desktop's behaviour (plugins enabled, approval policy so no human prompt, model/effort = the Desktop default in ~/.codex/config.toml), say which.
3. If parity holds (or differs only by originator), run the suite: `BENCH_PORT=8765 python3 -I codex_suite.py run --codex <bundled binary> --reps 2 --out runs.json` (add `--model`/`--effort` only to match the Desktop default). Then, if the Chrome extension is connected, repeat tasks form,table,busy-react,example with `--prefix "@Chrome "`, and tasks form,table,busy-react with `--prefix "@Computer "`, `--out runs-chrome.json` / `runs-computer.json`. If a run needs a human approval or hangs, report that and move on; do not click anything.
4. If parity does NOT hold, stop after step 2 and tell me exactly what differs (tool names, originator, config, a plugin that only loads inside the app); then Brain asks the user for an attended window.
5. Reply to me (computers send linux -kind reply) with: binary path+version, parity result, the runs.json paths, and a per-task median table (wall_ms, model_turns, tool_calls, total tokens, pass rate), plus the Behaviour notes you can read from the logs: which tool Codex picked for a web task (in-app browser, @Chrome, computer use, shell/curl), whether it screenshots or reads text, per-call latency of the browser tool, how many steps it batches, and what it asked for. Leave the scripts and results where %436 can read them, and tell me the paths. Clean up: stop fixture servers, close any tabs the runs opened, delete nothing in ~/.codex.

--- fixture.py ---
```python
#!/usr/bin/env python3
"""Fixture site for the browser task suite, stdlib only (same pages as fixture.mjs).
Run: python3 fixture.py [port]   (default 8765)   GET /__events shows what pages posted, POST /__reset clears."""
import json, sys, threading, time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

EVENTS = []
def page(title, body):
    return f'<!doctype html><html lang="en"><head><meta charset="utf-8"><title>{title}</title></head><body>{body}</body></html>'

def post(js):  # js object literal
    return f"fetch('/__event',{{method:'POST',body:JSON.stringify({js})}})"

def busy():
    ch = ''.join(f'<li><a href="#c{i}">Channel {i}</a></li>' for i in range(1, 201))
    msgs = ''.join(
        f'<article><h3><a href="#u{i % 40}">User {i % 40}</a> <time>10:{i % 60:02d}</time></h3>'
        f'<p>Message {i}: status update for item {i}, lorem ipsum dolor sit amet.</p>'
        f'<button id="react{i}" onclick="{post("{type:\'react\',n:%d}" % i)}">React {i}</button><button id="reply{i}">Reply {i}</button></article>'
        for i in range(1, 801))
    return page('Busy app', f'<nav><ul>{ch}</ul></nav><main>{msgs}<form><label>Message <input id="composer" type="text"></label><button type="button" id="send">Send</button></form></main>')

def form():
    js = post("{type:'form',name:document.getElementById('name').value,email:document.getElementById('email').value,plan:document.getElementById('plan').value,agree:document.getElementById('agree').checked}")
    return page('Form', f'''<h1>Sign up</h1><form onsubmit="return false">
<label>Name <input id="name" type="text"></label><label>Email <input id="email" type="email"></label>
<label>Plan <select id="plan"><option value="free">Free</option><option value="pro">Pro</option></select></label>
<label><input id="agree" type="checkbox"> I agree</label>
<button id="submit" type="button" onclick="document.getElementById('result').textContent='Saved '+document.getElementById('name').value;{js}">Submit</button></form><div id="result"></div>''')

def delay():
    return page('Delay', '<button id="go" onclick="setTimeout(()=>{document.getElementById(\'status\').textContent=\'Loaded\'},400)">Go</button><div id="status">Idle</div>')

def xhr():
    return page('XHR', '<button id="go" onclick="fetch(\'/api/slow\').then(r=>r.text()).then(t=>{document.getElementById(\'status\').textContent=t})">Load</button><div id="status">Idle</div>')

def table():
    rows = ''.join(f'<tr><td>{i}</td><td>Item {i}</td><td>{i * 7.5:.2f}</td><td>{"open" if i % 3 else "closed"}</td></tr>' for i in range(1, 61))
    return page('Table', f'<table id="data"><thead><tr><th>ID</th><th>Name</th><th>Price</th><th>State</th></tr></thead><tbody>{rows}</tbody></table>')

def simple(n):
    return page(f'Tab {n}', f'<h1>Tab {n}</h1><a href="/form">form</a>')

ROUTES = {'/busy': busy, '/form': form, '/delay': delay, '/table': table, '/xhr': xhr, '/tab2': lambda: simple(2), '/tab3': lambda: simple(3)}

class H(BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def send(self, code, body=b'', ctype='text/html; charset=utf-8'):
        self.send_response(code); self.send_header('content-type', ctype); self.send_header('cache-control', 'no-store')
        self.send_header('content-length', str(len(body))); self.end_headers(); self.wfile.write(body)
    def do_GET(self):
        path = self.path.split('?')[0]
        if path == '/api/slow':
            time.sleep(0.12); return self.send(200, b'XHR done', 'text/plain')
        if path == '/__events':
            return self.send(200, json.dumps(EVENTS).encode(), 'application/json')
        if path in ROUTES:
            return self.send(200, ROUTES[path]().encode())
        self.send(404, b'not found')
    def do_POST(self):
        path = self.path.split('?')[0]
        n = int(self.headers.get('content-length', 0)); body = self.rfile.read(n)
        if path == '/__event':
            try: EVENTS.append(json.loads(body))
            except Exception: pass
        elif path == '/__reset':
            EVENTS.clear()
        self.send(204)

if __name__ == '__main__':
    ThreadingHTTPServer(('127.0.0.1', int(sys.argv[1]) if len(sys.argv) > 1 else 8765), H).serve_forever()
```

--- codex_suite.py ---
```python
#!/usr/bin/env python3
"""Run the browser task suite through a Codex binary and measure it from Codex's own session logs.

  python3 -I codex_suite.py parity                       # what tools do existing Desktop sessions use?
  python3 -I codex_suite.py run --codex PATH [--reps 1] [--only form,table] [--prefix "@Chrome "]
                                [--model M] [--effort E] [--extra "-c key=val ..."] --out runs.json

Needs fixture.py next to it (stdlib). Only `codex exec` is used, with the desktop's own config
(~/.codex) and plugins; nothing is written outside the scratch dir and --out.
"""
import argparse, glob, json, os, shlex, subprocess, sys, tempfile, threading, time, urllib.request
from datetime import datetime
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import fixture
from http.server import ThreadingHTTPServer

PORT = int(os.environ.get('BENCH_PORT', '8765'))
BASE = f'http://127.0.0.1:{PORT}'
SESSIONS = os.path.expanduser('~/.codex/sessions')

TASKS = [
 ('form', f'Open {BASE}/form. Fill in the name "Ada Lovelace", the email ada@example.com, choose the Pro plan, tick "I agree", and submit the form. Then tell me the text the page shows after saving.',
  lambda a, ev: any(e.get('type') == 'form' and e.get('name') == 'Ada Lovelace' and e.get('email') == 'ada@example.com' and e.get('plan') == 'pro' and e.get('agree') is True for e in ev) and 'saved ada lovelace' in a.lower()),
 ('async', f'Open {BASE}/delay, press the Go button and wait for the status text to change. Tell me what it changes to.', lambda a, ev: 'loaded' in a.lower()),
 ('xhr', f'Open {BASE}/xhr, press Load and tell me the text that appears under it.', lambda a, ev: 'xhr done' in a.lower()),
 ('table', f'Open {BASE}/table. What is the price of "Item 37" and is it open or closed? Answer as "<price> <state>".', lambda a, ev: '277.50' in a and 'open' in a.lower()),
 ('busy-react', f'Open {BASE}/busy, a busy chat page. Press the "React 613" button of message 613, then tell me the name of the user who wrote message 613.',
  lambda a, ev: any(e.get('type') == 'react' and e.get('n') == 613 for e in ev) and 'user 13' in a.lower()),
 ('busy-find', f'Open {BASE}/busy and tell me the time shown on message 457 (the message that says "Message 457:").', lambda a, ev: '10:37' in a),
 ('multi-tab', f'Open {BASE}/tab2 and {BASE}/table in two tabs. Tell me the heading of the first page and whether row 12 of the table is open or closed.', lambda a, ev: 'tab 2' in a.lower() and 'closed' in a.lower()),
 ('example', 'Open https://example.com and tell me its main heading.', lambda a, ev: 'example domain' in a.lower()),
 ('wiki', 'Using Wikipedia (en.wikipedia.org), find in which year the Eiffel Tower was completed. Answer with the year.', lambda a, ev: '1889' in a),
 ('hn-item', 'Open https://news.ycombinator.com/item?id=8863 and tell me the title of that post.', lambda a, ev: 'dropbox' in a.lower()),
]

def ts(s):  # ISO with Z -> epoch seconds
    return datetime.fromisoformat(s.replace('Z', '+00:00')).timestamp()

def read_rollout(path):
    return [json.loads(l) for l in open(path) if l.strip()]

def tool_names(rows):
    names = set()
    for r in rows:
        p = r.get('payload') or {}
        if p.get('type') in ('function_call', 'custom_tool_call', 'mcp_tool_call', 'local_shell_call') and p.get('name'):
            names.add(p['name'])
        if p.get('type') == 'additional_tools':
            blob = json.dumps(p)
            names.add('additional_tools:' + str(len(blob)))
    return sorted(names)

def parity():
    out = []
    for path in sorted(glob.glob(SESSIONS + '/**/rollout-*.jsonl', recursive=True)):
        try:
            rows = read_rollout(path)
        except Exception:
            continue
        meta = next((r['payload'] for r in rows if r.get('type') == 'session_meta'), {})
        out.append({'file': os.path.relpath(path, SESSIONS), 'originator': meta.get('originator'), 'source': meta.get('source'),
                    'cli_version': meta.get('cli_version'), 'tools': tool_names(rows)})
    print(json.dumps(out, indent=1))

def measure(path):
    rows = read_rollout(path)
    meta = next((r['payload'] for r in rows if r.get('type') == 'session_meta'), {})
    calls, pending = [], {}
    first = last = None
    turns = 0
    tokens = {}
    answer = ''
    for r in rows:
        p = r.get('payload') or {}
        t = ts(r['timestamp']) if r.get('timestamp') else None
        if t and p.get('type') == 'task_started' and first is None: first = t
        if t: last = t
        if p.get('type') in ('function_call', 'custom_tool_call', 'mcp_tool_call'):
            pending[p.get('call_id') or p.get('id')] = (p.get('name'), t, len(json.dumps(p.get('arguments', p.get('input', '')))))
        if p.get('type') in ('function_call_output', 'custom_tool_call_output', 'mcp_tool_call_output'):
            name, t0, argb = pending.pop(p.get('call_id'), (None, None, 0))
            if t0 and t: calls.append({'tool': name, 'ms': round((t - t0) * 1000), 'arg_bytes': argb, 'out_bytes': len(json.dumps(p.get('output', '')))})
        if p.get('type') == 'token_count' and p.get('info'):
            turns += 1
            tokens = p['info'].get('total_token_usage', tokens)
        if p.get('type') == 'message' and p.get('role') == 'assistant':
            answer = ''.join(c.get('text', '') for c in p.get('content', []) if isinstance(c, dict))
        if p.get('type') == 'agent_message':
            answer = p.get('message', answer)
    return {'originator': meta.get('originator'), 'source': meta.get('source'), 'cli_version': meta.get('cli_version'),
            'model_turns': turns, 'tool_calls': len(calls), 'calls': calls, 'tokens': tokens, 'answer': answer,
            'log_wall_ms': round((last - first) * 1000) if first and last else None}

def newest_after(t0):
    files = [f for f in glob.glob(SESSIONS + '/**/rollout-*.jsonl', recursive=True) if os.path.getmtime(f) >= t0 - 1]
    return max(files, key=os.path.getmtime) if files else None

def run(a):
    server = ThreadingHTTPServer(('127.0.0.1', PORT), fixture.H)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    scratch = tempfile.mkdtemp(prefix='codexbench-')
    results = []
    for rep in range(a.reps):
        for tid, prompt, check in TASKS:
            if a.only and tid not in a.only.split(','): continue
            urllib.request.urlopen(urllib.request.Request(BASE + '/__reset', method='POST', data=b'')).read()
            cmd = [a.codex, 'exec', '--json', '--skip-git-repo-check', '-C', scratch]
            if a.model: cmd += ['-m', a.model]
            if a.effort: cmd += ['-c', f'model_reasoning_effort="{a.effort}"']
            if a.extra: cmd += shlex.split(a.extra)
            cmd.append(a.prefix + prompt)
            t0 = time.time()
            p = subprocess.run(cmd, capture_output=True, text=True, timeout=a.timeout)
            wall = (time.time() - t0) * 1000
            events = json.loads(urllib.request.urlopen(BASE + '/__events').read())
            log = newest_after(t0)
            m = measure(log) if log else {}
            ok = bool(m.get('answer')) and bool(check(m['answer'], events))
            row = {'task': tid, 'rep': rep, 'prefix': a.prefix, 'ok': ok, 'wall_ms': round(wall), 'exit': p.returncode, 'log': log and os.path.relpath(log, SESSIONS), **m}
            results.append(row)
            print(json.dumps({k: row[k] for k in ('task', 'rep', 'ok', 'wall_ms', 'model_turns', 'tool_calls') if k in row}), flush=True)
            json.dump(results, open(a.out, 'w'), indent=1)
    server.shutdown()

if __name__ == '__main__':
    ap = argparse.ArgumentParser(); sub = ap.add_subparsers(dest='cmd', required=True)
    sub.add_parser('parity')
    r = sub.add_parser('run')
    r.add_argument('--codex', required=True); r.add_argument('--reps', type=int, default=1); r.add_argument('--only')
    r.add_argument('--prefix', default=''); r.add_argument('--model'); r.add_argument('--effort'); r.add_argument('--extra')
    r.add_argument('--timeout', type=int, default=600); r.add_argument('--out', default='runs.json')
    args = ap.parse_args()
    parity() if args.cmd == 'parity' else run(args)
```
