// Local fixture site for the browser benchmark. Deterministic, no network.
import http from 'node:http';

const esc = s => s.replace(/[&<>]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' }[c]));
const page = (title, body, script = '') =>
  `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>${esc(title)}</title></head><body>${body}<script>${script}</script></body></html>`;

function busy() {
  let channels = '';
  for (let i = 1; i <= 200; i++) channels += `<li><a href="#c${i}">Channel ${i}</a></li>`;
  let msgs = '';
  for (let i = 1; i <= 800; i++) {
    msgs += `<article><h3><a href="#u${i % 40}">User ${i % 40}</a> <time>10:${String(i % 60).padStart(2, '0')}</time></h3>` +
      `<p>Message ${i}: status update for item ${i}, lorem ipsum dolor sit amet.</p>` +
      `<button id="react${i}">React ${i}</button><button id="reply${i}">Reply ${i}</button></article>`;
  }
  return page('Busy app', `<nav><ul>${channels}</ul></nav><main>${msgs}<form><label>Message <input id="composer" type="text"></label><button type="button" id="send">Send</button></form></main>`);
}
function form() {
  return page('Form', `<h1>Sign up</h1><form onsubmit="return false">
<label>Name <input id="name" type="text"></label>
<label>Email <input id="email" type="email"></label>
<label>Plan <select id="plan"><option value="free">Free</option><option value="pro">Pro</option></select></label>
<label><input id="agree" type="checkbox"> I agree</label>
<button id="submit" type="button" onclick="document.getElementById('result').textContent='Saved '+document.getElementById('name').value">Submit</button></form><div id="result"></div>`);
}
function delay() {
  return page('Delay', `<button id="go" onclick="setTimeout(()=>{document.getElementById('status').textContent='Loaded'},400)">Go</button><div id="status">Idle</div>`);
}
function table() {
  let rows = '';
  for (let i = 1; i <= 60; i++) rows += `<tr><td>${i}</td><td>Item ${i}</td><td>${(i * 7.5).toFixed(2)}</td><td>${i % 3 ? 'open' : 'closed'}</td></tr>`;
  return page('Table', `<table id="data"><thead><tr><th>ID</th><th>Name</th><th>Price</th><th>State</th></tr></thead><tbody>${rows}</tbody></table>`);
}
function xhr() {
  return page('XHR', `<button id="go" onclick="fetch('/api/slow').then(r=>r.text()).then(t=>{document.getElementById('status').textContent=t})">Load</button><div id="status">Idle</div><button id="timer" onclick="setTimeout(()=>{document.getElementById('status').textContent='Timer done'},150)">Timer</button>`);
}
const simple = n => page('Tab ' + n, `<h1>Tab ${n}</h1><a href="/form">form</a>`);

const apiSlow = (res) => setTimeout(() => { res.writeHead(200, { 'content-type': 'text/plain', 'cache-control': 'no-store' }); res.end('XHR done'); }, 120);
const routes = { '/busy': busy, '/form': form, '/delay': delay, '/table': table, '/xhr': xhr, '/tab2': () => simple(2), '/tab3': () => simple(3) };

export function startFixture(port = 8765) {
  const server = http.createServer((req, res) => {
    if (req.url === '/api/slow') return apiSlow(res);
    const route = routes[req.url.split('?')[0]];
    if (!route) { res.writeHead(404); res.end('not found'); return; }
    res.writeHead(200, { 'content-type': 'text/html; charset=utf-8', 'cache-control': 'no-store' });
    res.end(route());
  });
  return new Promise(r => server.listen(port, '127.0.0.1', () => r(server)));
}

if (import.meta.url === `file://${process.argv[1]}`) {
  await startFixture(Number(process.env.PORT || 8765));
  console.log('fixture on', process.env.PORT || 8765);
}
