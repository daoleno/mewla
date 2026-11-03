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
