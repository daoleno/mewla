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
