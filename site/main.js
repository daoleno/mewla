// mewla.ai: the app's screens, live. Each demo plays once when it comes into
// view and then waits for a tap. With reduced motion nothing plays by itself:
// the screens stay as drawn, and taps still work.

const reduce = matchMedia('(prefers-reduced-motion: reduce)').matches;
const $ = (sel, root = document) => root.querySelector(sel);
const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel));
const wait = (ms) => new Promise((r) => setTimeout(r, ms));

// ---- the bar, copy buttons, the install tabs --------------------------------
const bar = $('.bar');
const onScroll = () => bar.classList.toggle('scrolled', scrollY > 8);
addEventListener('scroll', onScroll, { passive: true });
onScroll();

for (const btn of $$('[data-copy]')) {
  btn.addEventListener('click', async () => {
    const text = btn.previousElementSibling.textContent.trim();
    try { await navigator.clipboard.writeText(text); } catch { return; }
    btn.classList.add('ok');
    setTimeout(() => btn.classList.remove('ok'), 1400);
  });
}

function tabs(buttons, key, pick) {
  for (const b of buttons) {
    b.addEventListener('click', () => {
      for (const o of buttons) {
        const on = o === b;
        o.classList.toggle('on', on);
        o.setAttribute(o.hasAttribute('aria-selected') ? 'aria-selected' : 'aria-pressed', String(on));
      }
      pick(b.dataset[key]);
    });
  }
}
tabs($$('[data-os]'), 'os', (os) => { for (const c of $$('[data-os-cmd]')) c.hidden = c.dataset.osCmd !== os; });
if (/Windows/.test(navigator.userAgent)) $('[data-os="win"]')?.click();

// ---- the cat: Brain's state, drawn by Dango's clips (SPEC 5) -----------------
// A state change plays its clip once, then the still stays. Clips on a bleed
// canvas are enlarged so the cat keeps its size.
const PET = 'pets/dango/';
const CLIP = {
  idle: { file: 'going_back', bleed: true, ms: 1370 },
  working: { file: 'working', ms: 2400 },
  delegating: { file: 'delegating', ms: 2200 },
  attention: { file: 'attention', bleed: true, ms: 2020 },
  delivered: { file: 'delivered', bleed: true, ms: 840 },
  waking: { file: 'waking', bleed: true, ms: 1440 },
};
const blobs = {};
const blobOf = (file) => blobs[file] || (blobs[file] = fetch(PET + file + '.webp').then((r) => r.blob()).catch(() => null));

function makeCat(box) {
  const img = $('img', box);
  let state = /pets\/dango\/([a-z]+)-still/.exec(img.getAttribute('src'))?.[1] ?? 'idle';
  let token = 0, url = '';
  const still = (s) => { img.classList.remove('bleed'); img.src = `${PET}${s}-still.webp`; };
  async function clip(name) {
    const c = CLIP[name], my = token;
    const blob = await blobOf(c.file);
    if (!blob || my !== token) return;
    if (url) URL.revokeObjectURL(url);
    url = URL.createObjectURL(blob);
    img.classList.toggle('bleed', !!c.bleed);
    img.src = url;
    await wait(c.ms);
  }
  return {
    get state() { return state; },
    async set(next, { once } = {}) {
      if (next === state && !once) return;
      const from = state;
      if (!once) state = next;
      const my = ++token;
      if (reduce) { still(state); return; }
      if (from === 'idle' && next !== 'idle' && next !== 'delivered') await clip('waking');
      if (my !== token) return;
      await clip(next);
      if (my === token) still(state);
    },
    preload() { for (const k of ['working', 'delegating', 'attention', 'going_back', 'waking', 'delivered']) blobOf(k); },
  };
}

// ---- small helpers for the demos ---------------------------------------------
function tapAt(app, el) {
  if (reduce || !el) return;
  const a = app.getBoundingClientRect(), r = el.getBoundingClientRect();
  const t = document.createElement('span');
  t.className = 'tap';
  t.style.left = `${r.left - a.left + r.width / 2}px`;
  t.style.top = `${r.top - a.top + r.height / 2}px`;
  app.appendChild(t);
  setTimeout(() => t.remove(), 800);
}
function setStateCtl(ctl, kind, text) {
  const dot = kind === 'needs' ? '<i class="seal-dot"></i>' : kind === 'running' ? '<i class="live"></i>' : '';
  ctl.classList.toggle('quiet', kind === 'quiet');
  ctl.innerHTML = `${dot}<span>${text}</span><svg class="ic chev"><use href="#i-chevron-down"/></svg>`;
}
const sealed = (answer) => `<div class="answered"><svg class="ic"><use href="#i-check-circle-fill"/></svg><div class="a"><small>You chose</small><b>${answer}</b></div></div>`;
const show = (el) => { el.hidden = false; el.classList.remove('in'); void el.offsetWidth; el.classList.add('in'); };
function whenSeen(el, fn, margin = '-20% 0px') {
  if (!('IntersectionObserver' in window)) return fn();
  const io = new IntersectionObserver((es) => { if (es.some((e) => e.isIntersecting)) { io.disconnect(); fn(); } }, { rootMargin: margin });
  io.observe(el);
}

// ---- 1. the hero: one goal, from the ask to the nap --------------------------
(function hero() {
  const root = $('[data-demo="home"]');
  if (!root) return;
  const app = $('.app', root), cat = makeCat($('[data-cat]', root));
  const ctl = $('[data-statectl]', root), input = $('[data-input]', root), send = $('[data-send]', root);
  const steps = (name) => $$(`[data-step="${name}"]`, root);
  const slot = $('.req-slot', root), cardHTML = slot.innerHTML;
  const pricingLine = $('[data-pricing-line]', root), pricingEnd = $('[data-pricing-end]', root), workEnd = $('[data-work-end]', root);
  const replay = $('[data-replay]', root);
  const ASK = 'Ship the pricing page by Friday, and get the API tests green.';
  let run = 0, answered = null;

  function wire() {
    for (const b of $$('[data-answer]', slot)) b.addEventListener('click', () => answer(b.dataset.answer, b));
  }
  function answer(text, btn) {
    if (!answered) return;
    btn?.classList.add('picked');
    const done = answered; answered = null;
    setTimeout(() => done(text), 160);
  }
  function reset() {
    run++;
    for (const n of ['ask', 'tool', 'say', 'work', 'req', 'result']) for (const el of steps(n)) el.hidden = true;
    slot.innerHTML = cardHTML; wire();
    pricingLine.textContent = 'Building the comparison table';
    pricingEnd.innerHTML = '<i class="live"></i>4m';
    workEnd.innerHTML = '<i class="live"></i>2 running<svg class="ic s"><use href="#i-chevron-right"/></svg>';
    setStateCtl(ctl, 'quiet', 'All quiet');
    input.innerHTML = '<span class="ph">Tell Brain</span>';
    send.classList.remove('ready');
    replay.hidden = true;
    cat.set('idle');
  }
  async function play() {
    reset();
    const my = run, live = () => my === run;
    cat.preload();
    await wait(700); if (!live()) return;
    for (let i = 1; i <= ASK.length; i++) {
      input.innerHTML = `${ASK.slice(0, i)}<i class="caret"></i>`;
      send.classList.toggle('ready', true);
      await wait(22); if (!live()) return;
    }
    await wait(350); tapAt(app, send); await wait(200); if (!live()) return;
    input.innerHTML = '<span class="ph">Tell Brain</span>'; send.classList.remove('ready');
    steps('ask').forEach(show); cat.set('working');
    await wait(900); if (!live()) return;
    steps('tool').forEach(show);
    await wait(800); if (!live()) return;
    steps('say').forEach(show);
    await wait(900); if (!live()) return;
    steps('work').forEach(show); setStateCtl(ctl, 'running', '2 running'); cat.set('delegating');
    await wait(1700); if (!live()) return;
    steps('req').forEach(show); setStateCtl(ctl, 'needs', '1 needs you'); cat.set('attention');
    const text = await new Promise((resolve) => {
      answered = resolve;
      setTimeout(() => { if (answered === resolve && live()) { const b = $('[data-answer]', slot); tapAt(app, b); answer(b.dataset.answer, b); } }, 5200);
    });
    if (!live()) return;
    slot.innerHTML = sealed(text); setStateCtl(ctl, 'running', '2 running'); cat.set('delegating');
    await wait(1500); if (!live()) return;
    workEnd.innerHTML = 'Done<svg class="ic s"><use href="#i-check"/></svg>';
    steps('result').forEach(show); setStateCtl(ctl, 'running', '1 running'); cat.set('delivered', { once: true });
    await wait(2000); if (!live()) return;
    pricingLine.textContent = 'Shipped with the 14-day trial';
    pricingEnd.innerHTML = '<svg class="ic s"><use href="#i-check"/></svg>';
    $('.prog', pricingLine.parentNode).hidden = true;
    setStateCtl(ctl, 'quiet', 'All quiet'); cat.set('idle');
    await wait(1200); if (!live()) return;
    replay.hidden = false;
  }
  replay.addEventListener('click', () => { $('.prog', pricingLine.parentNode).hidden = false; play(); });

  if (reduce) {
    // the screen stays at "needs you", as drawn; answering it still seals it
    answered = (text) => { slot.innerHTML = sealed(text); setStateCtl(ctl, 'running', '2 running'); cat.set('delegating'); };
    wire();
    return;
  }
  reset();
  whenSeen(root, play, '0px');
})();

// ---- 2. one question in three places -----------------------------------------
(function trio() {
  const root = $('[data-demo="trio"]');
  if (!root) return;
  const cat = makeCat($('[data-cat]', root)), ctl = $('[data-statectl]', root);
  const slot = $('.req-slot', root), cardHTML = slot.innerHTML;
  const needs = $('[data-tray-needs]', root), needsLabel = needs.previousElementSibling, rowHTML = needs.innerHTML;
  const reset = $('[data-trio-reset]');
  function bind() {
    for (const b of $$('[data-answer]', root)) b.onclick = () => answer(b.dataset.answer, b);
    const go = $('[data-goto-card]', root);
    if (go) go.onclick = () => {
      const card = $('.card', slot);
      if (!card) return;
      card.scrollIntoView({ behavior: reduce ? 'auto' : 'smooth', block: 'center' });
      card.classList.remove('flash'); void card.offsetWidth; card.classList.add('flash');
    };
  }
  function answer(text, btn) {
    if (root.dataset.state === 'done') return;
    btn.classList.add('picked');
    root.dataset.state = 'done';
    $('[data-answer-text]', root).textContent = `You chose: ${text}`;
    slot.innerHTML = sealed(text);
    needs.innerHTML = ''; needs.hidden = true; needsLabel.hidden = true;
    setStateCtl(ctl, 'running', '1 running');
    cat.set('delegating');
    reset.hidden = false;
  }
  reset.addEventListener('click', () => {
    root.dataset.state = 'open';
    slot.innerHTML = cardHTML;
    needs.innerHTML = rowHTML; needs.hidden = false; needsLabel.hidden = false;
    setStateCtl(ctl, 'needs', '1 needs you');
    cat.set('attention');
    reset.hidden = true;
    bind();
  });
  bind();
})();

// ---- 3. the tray, then a Session ----------------------------------------------
(function tray() {
  const root = $('[data-demo="tray"]');
  if (!root) return;
  const app = $('.app', root), ses = $('.layer-session', root), stateBtn = $('[data-open-tray]', root);
  let auto = true;
  const user = () => { auto = false; };
  const openTray = (on) => { app.classList.toggle('trayed', on); stateBtn.setAttribute('aria-expanded', String(on)); };
  const view = (v) => { app.dataset.view = v; ses.setAttribute('aria-hidden', String(v !== 'session')); };
  const mode = (m) => {
    ses.dataset.mode = m;
    for (const b of $$('[data-mode-btn]', root)) { const on = b.dataset.modeBtn === m; b.classList.toggle('on', on); b.setAttribute('aria-pressed', String(on)); }
  };
  stateBtn.addEventListener('click', (e) => { if (e.isTrusted) user(); openTray(!app.classList.contains('trayed')); });
  $('[data-close-tray]', root).addEventListener('click', () => { user(); openTray(false); });
  $('[data-open-session]', root).addEventListener('click', (e) => { if (e.isTrusted) user(); view('session'); });
  $('[data-back]', root).addEventListener('click', () => { user(); view('home'); });
  for (const b of $$('[data-mode-btn]', root)) b.addEventListener('click', (e) => { if (e.isTrusted) user(); mode(b.dataset.modeBtn); });
  if (reduce) return;
  whenSeen(root, async () => {
    const step = async (ms, el, act) => { await wait(ms); if (!auto) return false; tapAt(app, el); await wait(180); if (!auto) return false; act(); return true; };
    if (!await step(900, stateBtn, () => openTray(true))) return;
    if (!await step(1500, $('[data-open-session]', root), () => view('session'))) return;
    await step(1800, $('[data-mode-btn="term"]', root), () => mode('term'));
  });
})();

// ---- 4. hands on your computers -----------------------------------------------
(function hands() {
  const root = $('[data-demo="hands"]');
  if (!root) return;
  const remote = $('.remote', root), cursor = $('.agent-cursor', root), take = $('[data-take]', root);
  const rstate = $('[data-rstate]', root), line = $('[data-hands-line]');
  const LINES = {
    screen: 'Agents use the apps on your Mac, Windows PC or Linux box. Watch, or take over.',
    chrome: 'Agents use your own Chrome and its logins, in a tab group of their own.',
    phone: 'Agents use your Android phone on a hidden screen, while you keep using it. In Labs.',
  };
  const PATHS = {
    screen: [[30, 70], [62, 72], [78, 72], [46, 50]],
    chrome: [[56, 16], [40, 58], [30, 80], [70, 40]],
    phone: [[57, 82], [50, 38], [57, 82], [46, 56]],
  };
  let tab = 'screen', i = 0, timer = 0;
  tabs($$('[data-tab-btn]', root), 'tabBtn', (t) => { tab = t; root.dataset.tab = t; line.textContent = LINES[t]; i = 0; move(); });
  take.addEventListener('click', () => {
    const you = remote.dataset.control !== 'you';
    remote.dataset.control = you ? 'you' : 'agent';
    take.textContent = you ? 'Give back' : 'Take over';
    rstate.textContent = you ? 'You have the controls' : 'Watching';
  });
  function move() {
    const [x, y] = PATHS[tab][i % PATHS[tab].length];
    cursor.style.left = `${x}%`; cursor.style.top = `${y}%`;
  }
  move();
  if (reduce) return;
  whenSeen(root, () => {
    timer = setInterval(() => { if (remote.dataset.control === 'agent' && !document.hidden) { i++; move(); } }, 1300);
  }, '0px');
})();

// ---- 6. extension switches ------------------------------------------------------
for (const b of $$('[data-switch]')) {
  b.addEventListener('click', () => {
    const on = b.getAttribute('aria-pressed') !== 'true';
    b.setAttribute('aria-pressed', String(on));
    $('.sw', b).classList.toggle('on', on);
  });
}

// ---- 7. the wide layout, scaled to fit -----------------------------------------
(function wide() {
  const frame = $('[data-demo="wide"]');
  if (!frame) return;
  const inner = $('.wide', frame);
  const fit = () => {
    const s = frame.clientWidth / 1180;
    inner.style.setProperty('--s', String(s));
    frame.style.height = `${Math.round(720 * s)}px`;
  };
  fit();
  if ('ResizeObserver' in window) new ResizeObserver(fit).observe(frame);
  else addEventListener('resize', fit);
})();
