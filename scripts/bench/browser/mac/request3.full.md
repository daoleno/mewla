REQUEST (replaces my two earlier ones, m_5f64b37dd7a5e319 and m_550763049bfd2d8d). From the Linux Brain, for Worker BrowserSpeed. This one is authorised by the user's own decision, which came in reply to the proposal of an attended 25-minute Codex Desktop run: 「不，我希望你自己全自动的完成。想办法」 ("No, I want you to finish it fully automatically. Find a way.") The user asked for the actions below in that reply; nothing here goes beyond what they asked.

What is known, as reported to me: your computer-use controller refused with "Computer Use is not allowed to use the app 'com.openai.codex' for safety reasons". The Linux Brain reports that this message comes from Codex's own computer-use plugin guarding its own app, that it is not a macOS permission and not a Mewla rule, and that Mewla's mewla_computer has no such block (it checked daemon/screen). I have not verified that myself; please check which component printed it before you rely on it. If mewla_computer itself refuses the app, or macOS asks for a permission nobody granted, stop and tell me; do not work around it.

Goal: real Codex Desktop numbers (ChatGPT.app, com.openai.codex 26.1027) on the 10-task browser suite, read from Codex's own session logs. Boundaries: read-only tasks on our fixture page (served on 127.0.0.1) and three public pages; no sends, no purchases, no account, setting or permission changes; one Codex task at a time; leave Codex.app as you found it (same chat list state as far as possible: new chats are fine, delete none; quit it only if you started it). Use a claude Worker with mewla_computer, not Codex's own computer use. Stay inside this: the fixture, the prompts below and the listed read-only steps.

Order of preference:
1. codex:// first. Read Info.plist CFBundleURLTypes; if a URL can open a new thread with a prompt (try the documented forms, e.g. codex://new?prompt=...), that is the cheapest way: open it, let the Desktop run, read the rollout. Try it once with the "example" task and see whether the Desktop starts a turn by itself or only pre-fills the composer (then one Enter via mewla_computer submits it).
2. Otherwise drive Codex.app itself through mewla_computer: open_app ChatGPT (Codex), app_state to find the composer, a new chat per task (cmd+n), set_value with the prompt, press Enter or the Send button, then wait_for the turn to end (Send button back / final answer visible). Do not click anything else in the app. If it asks for an approval inside Codex, report the first one and what it said, and pause before approving anything that is not a plain read on our fixture or the public pages.
3. Cross-check (label it ENGINE, not Desktop): run the Desktop's bundled binary (find it under /Applications/ChatGPT.app/Contents/Resources or MacOS; `<binary> --version` and the process args of the running `codex app-server`) with the same config: `codex_suite.py run --codex <binary> --reps 1 --out engine-runs.json`. Compare with 1/2: same tool names, same originator family? Report differences.

How to measure (script below, stdlib): start the fixture (`python3 -I fixture.py 8765`, stop it at the end). Per run: `python3 -I codex_suite.py begin <task-id> [--prefix "@Chrome "]` prints the exact prompt and resets the fixture's event log; give that prompt to Codex.app by route 1 or 2; when the turn has ended, `python3 -I codex_suite.py end --out desktop-runs.json` finds the rollout this run wrote in ~/.codex/sessions, measures wall (log first task_started to last assistant output), model turns (assistant rounds), tool calls with per-call latency, tokens, and checks pass/fail from the fixture's own event log. Task ids: form, async, xhr, table, busy-react, busy-find, multi-tab, example, wiki, hn-item. Run each once; then, if time allows, a second pass. Also: form, table, busy-react once more with the prefix "@Chrome " (select the actual Chrome mention entry in the composer, not just typed text; the extension is installed, connected state unknown) and "@Computer " the same way for table only. Record the tool names each run used (tools_used) as evidence of which browser tool Codex picked.

Also note, from what you can see and read (short, factual): how Codex observes the page (screenshots vs text), whether it works in the background or takes the foreground, whether the app window or the user's other apps were disturbed, what it asked for, and the model/effort the Desktop used (from the rollout's turn_context). If a run hangs for more than 5 minutes, stop it, record it as a timeout and go on.

Output: desktop-runs.json (and engine-runs.json) plus a short Markdown report under your worklog, and a computers send reply to me with the paths and a per-task table (wall_ms, model_turns, tool_calls, total tokens, pass), the route you used, and any refusal verbatim. Clean up: stop the fixture, delete only the scratch dir you made. Tell me when the Mac is free.

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

def message(i):
    click = post("{type:'react',n:" + str(i) + "}")
    return ('<article><h3><a href="#u%d">User %d</a> <time>10:%02d</time></h3>' % (i % 40, i % 40, i % 60)
            + '<p>Message %d: status update for item %d, lorem ipsum dolor sit amet.</p>' % (i, i)
            + '<button id="react%d" onclick="%s">React %d</button><button id="reply%d">Reply %d</button></article>' % (i, click, i, i, i))

def busy():
    ch = ''.join('<li><a href="#c%d">Channel %d</a></li>' % (i, i) for i in range(1, 201))
    msgs = ''.join(message(i) for i in range(1, 801))
    return page('Busy app', '<nav><ul>' + ch + '</ul></nav><main>' + msgs + '<form><label>Message <input id="composer" type="text"></label><button type="button" id="send">Send</button></form></main>')

def form():
    js = post("{type:'form',name:document.getElementById('name').value,email:document.getElementById('email').value,plan:document.getElementById('plan').value,agree:document.getElementById('agree').checked}")
    return page('Form', '''<h1>Sign up</h1><form onsubmit="return false">
<label>Name <input id="name" type="text"></label><label>Email <input id="email" type="email"></label>
<label>Plan <select id="plan"><option value="free">Free</option><option value="pro">Pro</option></select></label>
<label><input id="agree" type="checkbox"> I agree</label>
<button id="submit" type="button" onclick="document.getElementById('result').textContent='Saved '+document.getElementById('name').value;@JS@">Submit</button></form><div id="result"></div>'''.replace('@JS@', js))

def delay():
    return page('Delay', '<button id="go" onclick="setTimeout(()=>{document.getElementById(\'status\').textContent=\'Loaded\'},400)">Go</button><div id="status">Idle</div>')

def xhr():
    return page('XHR', '<button id="go" onclick="fetch(\'/api/slow\').then(r=>r.text()).then(t=>{document.getElementById(\'status\').textContent=t})">Load</button><div id="status">Idle</div>')

def table():
    rows = ''.join('<tr><td>%d</td><td>Item %d</td><td>%.2f</td><td>%s</td></tr>' % (i, i, i * 7.5, 'open' if i % 3 else 'closed') for i in range(1, 61))
    return page('Table', '<table id="data"><thead><tr><th>ID</th><th>Name</th><th>Price</th><th>State</th></tr></thead><tbody>' + rows + '</tbody></table>')

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
    in_round = False
    tokens = {}
    answer = ''
    ASSISTANT = ('reasoning', 'function_call', 'custom_tool_call', 'mcp_tool_call', 'local_shell_call', 'agent_message')
    for r in rows:
        p = r.get('payload') or {}
        t = ts(r['timestamp']) if r.get('timestamp') else None
        if t and p.get('type') == 'task_started' and first is None: first = t
        kind = p.get('type')
        is_assistant = kind in ASSISTANT or (kind == 'message' and p.get('role') == 'assistant')
        if is_assistant and not in_round:
            turns += 1; in_round = True          # a model response round starts
        elif kind in ('function_call_output', 'custom_tool_call_output', 'mcp_tool_call_output') or (kind == 'message' and p.get('role') == 'user'):
            in_round = False                      # tool results or user input end the round
        if t and (is_assistant or kind in ('task_complete', 'turn_complete')): last = t
        if p.get('type') in ('function_call', 'custom_tool_call', 'mcp_tool_call'):
            pending[p.get('call_id') or p.get('id')] = (p.get('name'), t, len(json.dumps(p.get('arguments', p.get('input', '')))))
        if p.get('type') in ('function_call_output', 'custom_tool_call_output', 'mcp_tool_call_output'):
            name, t0, argb = pending.pop(p.get('call_id'), (None, None, 0))
            if t0 and t: calls.append({'tool': name, 'ms': round((t - t0) * 1000), 'arg_bytes': argb, 'out_bytes': len(json.dumps(p.get('output', '')))})
        if p.get('type') == 'token_count' and p.get('info'):
            tokens = p['info'].get('total_token_usage', tokens)
        if p.get('type') == 'message' and p.get('role') == 'assistant':
            answer = ''.join(c.get('text', '') for c in p.get('content', []) if isinstance(c, dict))
        if p.get('type') == 'agent_message':
            answer = p.get('message', answer)
    return {'originator': meta.get('originator'), 'source': meta.get('source'), 'cli_version': meta.get('cli_version'),
            'model_turns': turns, 'tool_calls': len(calls), 'calls': calls, 'tokens': tokens, 'answer': answer,
            'log_wall_ms': round((last - first) * 1000) if first and last else None}

def thread_id(stdout):
    """The launched child's own session id, from its --json event stream."""
    for line in stdout.splitlines():
        try:
            m = json.loads(line)
        except ValueError:
            continue
        tid = m.get('thread_id') or (m.get('msg') or {}).get('session_id') or (m.get('msg') or {}).get('thread_id')
        if tid:
            return tid
    return None

def rollout_for(tid):
    """Exactly the child's rollout (never another active session)."""
    if not tid:
        return None
    files = glob.glob(SESSIONS + '/**/rollout-*' + tid + '.jsonl', recursive=True)
    return files[0] if files else None

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
            timed_out = False
            try:
                p = subprocess.run(cmd, capture_output=True, text=True, timeout=a.timeout, stdin=subprocess.DEVNULL)
            except subprocess.TimeoutExpired as e:
                timed_out = True
                p = subprocess.CompletedProcess(cmd, -9, (e.stdout or b'').decode() if isinstance(e.stdout, bytes) else (e.stdout or ''), '')
            wall = (time.time() - t0) * 1000
            events = json.loads(urllib.request.urlopen(BASE + '/__events').read())
            log = rollout_for(thread_id(p.stdout))
            m = measure(log) if log else {}
            ok = (not timed_out) and bool(m.get('answer')) and bool(check(m['answer'], events))
            # A prefix such as "@Chrome " is only text; which tools actually ran is the evidence.
            tools_used = sorted({c['tool'] for c in m.get('calls', []) if c.get('tool')})
            row = {'task': tid, 'rep': rep, 'prefix': a.prefix, 'ok': ok, 'timed_out': timed_out, 'tools_used': tools_used, 'wall_ms': round(wall), 'exit': p.returncode, 'log': log and os.path.relpath(log, SESSIONS), **m}
            results.append(row)
            print(json.dumps({k: row[k] for k in ('task', 'rep', 'ok', 'wall_ms', 'model_turns', 'tool_calls') if k in row}), flush=True)
            json.dump(results, open(a.out, 'w'), indent=1)
    server.shutdown()

# ---- Desktop runs: something else (a computer-use Worker) types the prompt into the app;
# these two steps bracket one run and read its rollout from ~/.codex/sessions.
def user_texts(rows):
    """Every user message in the rollout, joined (the first is often AGENTS.md or environment context)."""
    out = []
    for r in rows:
        p = r.get('payload') or {}
        if p.get('type') == 'message' and p.get('role') == 'user':
            out.append(''.join(c.get('text', '') for c in p.get('content', []) if isinstance(c, dict)))
        if p.get('type') == 'user_message':
            out.append(p.get('message', ''))
    return '\n'.join(out)

def task_by_id(tid):
    return next(t for t in TASKS if t[0] == tid)

def begin(a):
    """Reset the fixture's event log, remember the start time, print the exact prompt to type."""
    tid, prompt, _ = task_by_id(a.task)
    urllib.request.urlopen(urllib.request.Request(BASE + '/__reset', method='POST', data=b'')).read()
    json.dump({'task': tid, 't0': time.time()}, open(a.state, 'w'))
    print(a.prefix + prompt)

def end(a):
    """Find the rollout this run wrote (a user message holds the task's prompt), measure it, check it, append to --out."""
    st = json.load(open(a.state))
    tid, prompt, check = task_by_id(st['task'])
    needle = prompt[:60]
    found = None
    for f in sorted(glob.glob(SESSIONS + '/**/rollout-*.jsonl', recursive=True), key=os.path.getmtime, reverse=True):
        if os.path.getmtime(f) < st['t0'] - 2:
            break
        try:
            rows = read_rollout(f)
        except Exception:
            continue
        if needle in user_texts(rows):
            found = f
            break
    events = json.loads(urllib.request.urlopen(BASE + '/__events').read())
    m = measure(found) if found else {}
    ok = bool(m.get('answer')) and bool(check(m['answer'], events))
    row = {'task': tid, 'prefix': a.prefix, 'ok': ok, 'tools_used': sorted({c['tool'] for c in m.get('calls', []) if c.get('tool')}),
           'driver_wall_ms': round((time.time() - st['t0']) * 1000), 'log': found and os.path.relpath(found, SESSIONS), **m}
    out = json.load(open(a.out)) if os.path.exists(a.out) else []
    out.append(row)
    json.dump(out, open(a.out, 'w'), indent=1)
    print(json.dumps({k: row.get(k) for k in ('task', 'ok', 'log_wall_ms', 'model_turns', 'tool_calls', 'tools_used', 'originator')}))

if __name__ == '__main__':
    ap = argparse.ArgumentParser(); sub = ap.add_subparsers(dest='cmd', required=True)
    sub.add_parser('parity')
    r = sub.add_parser('run')
    r.add_argument('--codex', required=True); r.add_argument('--reps', type=int, default=1); r.add_argument('--only')
    r.add_argument('--prefix', default=''); r.add_argument('--model'); r.add_argument('--effort'); r.add_argument('--extra')
    r.add_argument('--timeout', type=int, default=600); r.add_argument('--out', default='runs.json')
    for name in ('begin', 'end'):
        d = sub.add_parser(name)
        d.add_argument('task', nargs='?'); d.add_argument('--prefix', default=''); d.add_argument('--state', default='desktop-state.json'); d.add_argument('--out', default='desktop-runs.json')
    args = ap.parse_args()
    {'parity': lambda: parity(), 'run': lambda: run(args), 'begin': lambda: begin(args), 'end': lambda: end(args)}[args.cmd]()
```
