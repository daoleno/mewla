// Pages for the longer tasks (ten or more actions each).
const esc = s => s.replace(/[&<>]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' }[c]));
const page = (title, body, script = '') =>
  `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>${esc(title)}</title></head><body>${body}<script>${script}</script></body></html>`;

const FIELDS = ['first', 'last', 'email', 'phone', 'street', 'city', 'plan'];

export function wizard() {
  const steps = [
    ['Your name', '<label>First name <input id="first"></label><label>Last name <input id="last"></label>'],
    ['Contact', '<label>Email <input id="email" type="email"></label><label>Phone <input id="phone"></label>'],
    ['Address', '<label>Street <input id="street"></label><label>City <input id="city"></label>'],
    ['Plan', '<label>Plan <select id="plan"><option value="basic">Basic</option><option value="team">Team</option><option value="enterprise">Enterprise</option></select></label><label><input id="news" type="checkbox"> Send me the newsletter</label>'],
  ];
  const html = steps.map((st, i) =>
    `<section id="step${i + 1}" ${i ? 'hidden' : ''}><h2>Step ${i + 1} of 5: ${st[0]}</h2>${st[1]}<button type="button" onclick="go(${i + 2})">Next</button></section>`).join('') +
    '<section id="step5" hidden><h2>Step 5 of 5: Review</h2><pre id="review"></pre><button type="button" id="finish" onclick="finish()">Finish</button></section><div id="done"></div>';
  const script = `
const F = ${JSON.stringify(FIELDS)};
function go(n) {
  document.querySelectorAll('section').forEach(s => s.hidden = true);
  document.getElementById('step' + n).hidden = false;
  if (n == 5) document.getElementById('review').textContent = F.map(k => k + ': ' + document.getElementById(k).value).join(String.fromCharCode(10)) + String.fromCharCode(10) + 'newsletter: ' + document.getElementById('news').checked;
}
function finish() {
  const v = { type: 'wizard' };
  F.forEach(k => v[k] = document.getElementById(k).value);
  v.news = document.getElementById('news').checked;
  fetch('/__event', { method: 'POST', body: JSON.stringify(v) });
  document.getElementById('done').textContent = 'Registered ' + v.first + ' ' + v.last;
}`;
  return page('Wizard', html, script);
}

export function pages(url) {
  const p = Number(new URL(url, 'http://x').searchParams.get('p') || 1);
  let rows = '';
  for (let i = (p - 1) * 100 + 1; i <= p * 100; i++) {
    rows += `<tr><td>SKU-${String(i).padStart(4, '0')}</td><td>Part ${i}</td><td>$${(i * 3.25 + 10).toFixed(2)}</td></tr>`;
  }
  return page('Catalog p' + p,
    `<h1>Catalog, page ${p} of 12</h1><table><thead><tr><th>SKU</th><th>Name</th><th>Price</th></tr></thead><tbody>${rows}</tbody></table>` +
    (p > 1 ? `<a href="/pages?p=${p - 1}">Previous</a> ` : '') + (p < 12 ? `<a href="/pages?p=${p + 1}">Next</a>` : ''));
}

const PRODUCTS = [['Alpha lamp', 42.5], ['Beta lamp', 38.9], ['Gamma lamp', 45.0], ['Delta lamp', 39.4]];
export function product(n) {
  const [name, price] = PRODUCTS[n - 1] ?? PRODUCTS[0];
  return page('Product ' + n, `<h1>${name}</h1><p>Price: $${price}</p><a href="/product/${n % 4 + 1}">Next product</a>`);
}

export function todo() {
  const script = `
const list = document.getElementById('list');
document.getElementById('new').addEventListener('keydown', e => { if (e.key === 'Enter') add(); });
function add() {
  const i = document.getElementById('new');
  if (!i.value.trim()) return;
  const li = document.createElement('li');
  li.innerHTML = '<label><input type="checkbox"> <span></span></label>';
  li.querySelector('span').textContent = i.value;
  li.querySelector('input').addEventListener('change', count);
  list.appendChild(li); i.value = ''; count();
}
function count() {
  const left = [...list.querySelectorAll('input')].filter(c => !c.checked).length;
  document.getElementById('count').textContent = left + ' left';
  fetch('/__event', { method: 'POST', body: JSON.stringify({ type: 'todo', left, total: list.children.length }) });
}`;
  return page('Todo', '<h1>Todo</h1><input id="new" placeholder="What needs doing?"><button type="button" onclick="add()">Add</button><ul id="list"></ul><p id="count">0 left</p>', script);
}
