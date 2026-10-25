(function () {
'use strict';
/* The gouache house is alive. The room is two empty pictures (day, night) that
   fade into each other with the real hour; the cat is its own picture on top,
   one pose per thing it does, so it can go from one thing to another, hop
   across the room, leave through the window with its bag, and come home. All
   motion is transform and opacity only, and every timer sleeps while the page
   is off screen. */

var K = window.CAT_KIT && window.CAT_KIT.gouache;
var Sh = window.CatShell;
var Snd = window.CatSound || { unlock: function () {}, pause: function () {}, setNight: function () {}, purr: function () {}, paper: function () {}, pop: function () {}, chime: function () {}, thump: function () {}, chirp: function () {} };
if (!K || !Sh) return;

var BASE = 'a/s/gouache/';
var stage = document.getElementById('stage');
var M = K.meta;
var L = { built: false, on: false, away: null, cur: null, busy: false, paused: false, walking: false, returning: false, last: [], timers: {}, acts: {}, np: 0, chain: Promise.resolve(), gift: null, visiting: false, pokeUntil: 0 };
var layer, sky, walker, bagEl, tint, lampEl, beam, mullDay, mullNight, topLayer;
var reduce = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;

function el(tag, cls) { var e = document.createElement(tag); if (cls) e.className = cls; return e; }
function pic(file, cls) { var i = new Image(); i.src = BASE + file; i.alt = ''; i.draggable = false; if (cls) i.className = cls; return i; }
function pct(v) { return (v * 100) + '%'; }
function wait(ms) { return new Promise(function (r) { setTimeout(r, ms); }); }
function rnd(a, b) { return a + Math.random() * (b - a); }
function stagePx() { return { w: stage.offsetWidth || 400, h: stage.offsetHeight || 711 }; }
function place(e, name) { var m = M[name]; e.style.left = pct(m[0]); e.style.top = pct(m[1]); e.style.width = pct(m[2]); }
function anim(e, kf, o) { try { return e.animate(kf, o); } catch (err) { return { finished: Promise.resolve(), cancel: function () {} }; } }

/* ---- the hour ---- */
function hourNow() { var d = new Date(); return d.getHours() + d.getMinutes() / 60; }
function nightAmount(h) {
  if (h >= 20.5 || h < 5.5) return 1;
  var t = h >= 17.5 ? (h - 17.5) / 3 : h < 7.5 ? 1 - (h - 5.5) / 2 : 0;
  return t * t * (3 - 2 * t);
}
function periodOf(h, np) { return np > 0.55 ? 'night' : h < 10 ? 'morning' : h < 17 ? 'day' : 'evening'; }
function warmth(h) { return Math.max(0, 1 - Math.abs(h - 18.7) / 1.6) * 0.2 + Math.max(0, 1 - Math.abs(h - 6.4) / 1.0) * 0.13; }

/* ---- build the layer once ---- */
function build() {
  if (L.built) return;
  L.built = true;
  layer = el('div'); layer.id = 'life';
  stage.insertBefore(layer, document.getElementById('cat-home'));
  var g = K.glass, b = K.box;

  // the window: sky things live inside the glass, the painted bars sit over them
  sky = el('div', 'sky');
  sky.style.left = pct(g[0] - g[2]); sky.style.top = pct(g[1] - g[3]); sky.style.width = pct(2 * g[2]); sky.style.height = pct(2 * g[3]);
  [['a', 8, 36], ['b', 34, 26]].forEach(function (c) {
    var cl = pic('cloud.webp', 'cloud ' + c[0]); cl.style.top = c[1] + '%'; cl.style.width = c[2] + '%'; sky.appendChild(cl);
  });
  K.stars.slice(0, 6).forEach(function (s, i) {
    var px = b[0] + s[0] * b[2], py = b[1] + s[1] * b[3];
    var d = el('i', 'star'); d.style.left = pct((px - (g[0] - g[2])) / (2 * g[2])); d.style.top = pct((py - (g[1] - g[3])) / (2 * g[3])); d.style.animationDelay = (i * 0.7) + 's'; sky.appendChild(d);
  });
  layer.appendChild(sky);
  mullDay = pic('mullion_day.webp', 'mull'); mullNight = pic('mullion_night.webp', 'mull');
  [mullDay, mullNight].forEach(function (m) { m.style.left = pct(b[0]); m.style.top = pct(b[1]); m.style.width = pct(b[2]); layer.appendChild(m); });

  // light that follows the hour
  beam = el('div', 'beam'); layer.appendChild(beam);
  for (var i = 0; i < 7; i++) { var mo = el('i', 'mote'); mo.style.left = (20 + Math.random() * 34) + '%'; mo.style.top = (52 + Math.random() * 34) + '%'; mo.style.animationDelay = (-Math.random() * 14) + 's'; mo.style.animationDuration = (11 + Math.random() * 8) + 's'; beam.appendChild(mo); }
  lampEl = el('div', 'lamp'); lampEl.style.left = pct(K.lamp[0]); lampEl.style.top = pct(K.lamp[1]); lampEl.appendChild(el('i')); layer.appendChild(lampEl);

  bagEl = pic('bag.webp', 'bag'); place(bagEl, 'bag'); layer.appendChild(bagEl);

  walker = el('div', 'walker fx');
  walker.appendChild(pic('cat_' + K.sit + '.webp', 'w sit on')); walker.appendChild(pic('cat_' + K.ready + '.webp', 'w ready'));
  layer.appendChild(walker);

  topLayer = el('div', 'top'); layer.appendChild(topLayer);
  tint = el('div', 'tint'); layer.appendChild(tint);
  K.acts.forEach(function (a) { var s = new Image(); s.src = BASE + 'cat_' + a.a + '.webp'; var t = new Image(); t.src = BASE + 'cat_' + a.b + '.webp'; });
}

function ensureAct(a) {
  var o = L.acts[a.id];
  if (o) return o;
  var root = el('div', 'act fx' + (a.still ? ' still' : '')), inner = el('div', 'in');
  place(root, a.a);
  var m = M[a.a], mb = M[a.b];
  if (!a.still) root.appendChild(el('i', 'sh'));
  var fa = pic('cat_' + a.a + '.webp', 'fa'), fb = pic('cat_' + a.b + '.webp', 'fb');
  fb.style.left = pct((mb[0] - m[0]) / m[2]); fb.style.top = pct((mb[1] - m[1]) / m[3]); fb.style.width = pct(mb[2] / m[2]);
  inner.appendChild(fa); inner.appendChild(fb);
  root.appendChild(inner);
  if (a.steam) for (var i = 0; i < 3; i++) { var s = el('i', 'steam'); s.style.left = pct(a.steam[0]); s.style.top = pct(a.steam[1]); s.style.animationDelay = (i * 1.1) + 's'; root.appendChild(s); }
  if (a.zz) for (var z = 0; z < 2; z++) { var zz = el('i', 'zz'); zz.style.left = pct(a.zz[0]); zz.style.top = pct(a.zz[1]); zz.style.animationDelay = (z * 2.2) + 's'; zz.innerHTML = '<svg viewBox="0 0 20 20"><path d="M4 4h11L4 16h12" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"/></svg>'; root.appendChild(zz); }
  layer.insertBefore(root, walker);
  o = L.acts[a.id] = { a: a, root: root, inner: inner, fb: fb };
  return o;
}

/* ---- what the cat does ---- */
function pick() {
  var h = hourNow(), p = periodOf(h, L.np), list = K.acts.filter(function (a) { return a.when[p]; });
  var fresh = list.filter(function (a) { return L.last.indexOf(a.id) < 0; });
  if (fresh.length) list = fresh;
  var sum = 0; list.forEach(function (a) { sum += a.when[p]; });
  var r = Math.random() * sum, got = list[0];
  for (var i = 0; i < list.length; i++) { r -= list[i].when[p]; if (r <= 0) { got = list[i]; break; } }
  return got;
}
function remember(a) {
  L.last.unshift(a.id); L.last.length = Math.min(L.last.length, 2);
  try { localStorage.setItem('cat.last', a.id); } catch (e) { /* no storage: the room still varies */ }
}
function firstPick() {
  var before = ''; try { before = localStorage.getItem('cat.last') || ''; } catch (e) { before = ''; }
  if (before) L.last = [before];
  return pick();
}
function footBox(a) { return { x: a.foot[0], y: a.foot[1] }; }

function showAct(a, instant) {
  var o = ensureAct(a);
  L.cur = a; remember(a);
  o.root.classList.toggle('instant', !!instant);
  void o.root.offsetWidth;
  o.root.classList.add('on');
  o.fb.classList.remove('on');
  // the cat's own hotspot follows where it sits
  var P = stagePx(), bh = a.bw * 0.95 * P.w / P.h;
  Sh.hotCat(a.foot[0], a.foot[1] - bh / 2, Math.max(a.bw * 1.15, 0.2), Math.max(bh, 0.1));
  scheduleGlance();
}
function hideAct(a) { var o = L.acts[a.id]; if (o) { o.root.classList.remove('on'); o.root.classList.remove('instant'); } clearTimeout(L.timers.glance); }

function showB(ms) {
  var a = L.cur; if (!a) return;
  var o = L.acts[a.id]; if (!o) return;
  o.fb.classList.add('on');
  clearTimeout(o.bt);
  o.bt = setTimeout(function () { o.fb.classList.remove('on'); }, ms);
}
function scheduleGlance() {
  clearTimeout(L.timers.glance);
  var a = L.cur; if (!a) return;
  L.timers.glance = setTimeout(function () {
    if (!L.paused && !L.busy && L.cur === a && Date.now() > L.pokeUntil) showB(a.night ? 2200 : 1300);
    scheduleGlance();
  }, (a.still ? rnd(22, 36) : rnd(8, 16)) * 1000);
}
function scheduleNext() {
  clearTimeout(L.timers.next);
  L.timers.next = setTimeout(function () {
    if (L.away || L.paused || L.busy || !Sh.onHouse()) { scheduleNext(); return; }
    queue(function () { return changeAct(pick()); });
    scheduleNext();
  }, rnd(24, 55) * 1000);
}

/* ---- movement ---- */
function placeWalker(x, y, bw, which) {
  walker.style.left = pct(x); walker.style.top = pct(y); walker.style.width = pct(bw);
  walker.querySelector('.sit').classList.toggle('on', which !== 'ready');
  walker.querySelector('.ready').classList.toggle('on', which === 'ready');
  walker.classList.toggle('lit', L.np > 0.55);
}
/* hop from (x1,y1) to (x2,y2), growing or shrinking from body width bw1 to bw2 */
function hopTo(x1, y1, bw1, x2, y2, bw2, ms, hops, fade) {
  var P = stagePx(), dx = (x2 - x1) * P.w, dy = (y2 - y1) * P.h, s2 = bw2 / bw1, hh = Math.max(8, P.w * 0.032), N = 28, kf = [];
  for (var i = 0; i <= N; i++) {
    var u = i / N, e = u * u * (3 - 2 * u), ph = e * hops, s = Math.abs(Math.sin(Math.PI * ph)), sq = Math.min(1, Math.min(i, N - i) / 2), sc = 1 + (s2 - 1) * e;
    var sy = sc * (1 + sq * (-0.12 + 0.2 * Math.pow(s, 0.6))), sx = sc * (1 + sq * (0.08 - 0.13 * s));
    var f = { offset: u, transform: 'translate(-50%,-100%) translate(' + (dx * e).toFixed(1) + 'px,' + (dy * e - s * hh).toFixed(1) + 'px) scale(' + sx.toFixed(3) + ',' + sy.toFixed(3) + ')' };
    if (fade === 'out') f.opacity = u < 0.7 ? 1 : 1 - (u - 0.7) / 0.3;
    if (fade === 'in') f.opacity = u > 0.25 ? 1 : u / 0.25;
    kf.push(f);
  }
  L.walking = true;
  var an = anim(walker, kf, { duration: ms, easing: 'linear', fill: 'forwards' });
  return an.finished.then(function () {
    placeWalker(x2, y2, bw2, walker.querySelector('.ready').classList.contains('on') ? 'ready' : 'sit');
    if (an.cancel) an.cancel();
    L.walking = false;
    Snd.thump();
  }, function () { L.walking = false; });
}
function hopsFor(x1, y1, x2, y2) {
  var P = stagePx(), d = Math.hypot((x2 - x1) * P.w, (y2 - y1) * P.h) / P.w;
  return { hops: Math.max(1, Math.round(d * 7)), ms: 520 + d * 1100 };
}

async function changeAct(next) {
  var cur = L.cur;
  if (cur === next) return;
  L.busy = true;
  try {
    if (cur) {
      placeWalker(cur.foot[0], cur.foot[1], cur.bw, 'sit');
      walker.classList.add('on');
      hideAct(cur); L.cur = null; Sh.hotCat(null);
      await wait(260);
      var h = hopsFor(cur.foot[0], cur.foot[1], next.foot[0], next.foot[1]);
      await hopTo(cur.foot[0], cur.foot[1], cur.bw, next.foot[0], next.foot[1], next.bw, h.ms, h.hops);
    }
    showAct(next);
    await wait(160);
    walker.classList.remove('on');
    await wait(300);
  } finally { L.busy = false; }
}

/* ---- the bag, the leaving and the coming home ---- */
function flyBag(from, to, ms, toScale) {
  var P = stagePx(), b = el('div', 'flybag'), im = pic('bag.webp');
  b.style.left = pct(from[0]); b.style.top = pct(from[1]); b.style.width = pct(M.bag[2]);
  b.appendChild(im); layer.insertBefore(b, topLayer);
  var dx = (to[0] - from[0]) * P.w, dy = (to[1] - from[1]) * P.h, kf = [];
  for (var i = 0; i <= 14; i++) {
    var u = i / 14, e = u * u * (3 - 2 * u), arc = -Math.sin(Math.PI * u) * P.w * 0.1, sc = 1 + (toScale - 1) * e;
    kf.push({ offset: u, transform: 'translate(-50%,-50%) translate(' + (dx * e).toFixed(1) + 'px,' + (dy * e + arc).toFixed(1) + 'px) rotate(' + (Math.sin(u * Math.PI * 2) * 14).toFixed(1) + 'deg) scale(' + sc.toFixed(3) + ')' });
  }
  return anim(b, kf, { duration: ms, easing: 'linear', fill: 'forwards' }).finished.then(function () { b.remove(); }, function () { b.remove(); });
}
function hookSpot() { return [M.bag[0] + M.bag[2] * 0.5, M.bag[1] + M.bag[3] * 0.55]; }
function puff(x, y) {
  var p = el('i', 'puff'); p.style.left = pct(x); p.style.top = pct(y); topLayer.appendChild(p);
  anim(p, [{ opacity: 0.7, transform: 'translate(-50%,-50%) scale(.3)' }, { opacity: 0, transform: 'translate(-50%,-70%) scale(1.6)' }], { duration: 700, easing: 'ease-out' }).finished.then(function () { p.remove(); }, function () { p.remove(); });
}

async function doLeave() {
  L.busy = true;
  try {
    clearTimeout(L.timers.glance);
    var cur = L.cur, from = cur ? cur.foot : K.rug, bw = cur ? cur.bw : 0.3, rug = K.rug;
    placeWalker(from[0], from[1], bw, 'sit'); walker.classList.add('on');
    if (cur) { hideAct(cur); L.cur = null; Sh.hotCat(null); }
    await wait(300);
    if (Math.hypot(from[0] - rug[0], from[1] - rug[1]) > 0.05) { var h = hopsFor(from[0], from[1], rug[0], rug[1]); await hopTo(from[0], from[1], bw, rug[0], rug[1], 0.3, h.ms, h.hops); }
    else placeWalker(rug[0], rug[1], 0.3, 'sit');
    // the bag comes off its hook and onto the cat
    bagEl.classList.add('off');
    await flyBag(hookSpot(), [rug[0], rug[1] - 0.06], 760, 0.9);
    walker.querySelector('.sit').classList.remove('on'); walker.querySelector('.ready').classList.add('on');
    Snd.pop();
    anim(walker, [{ transform: 'translate(-50%,-100%) scale(1.08,.9)' }, { transform: 'translate(-50%,-100%) scale(.96,1.06)' }, { transform: 'translate(-50%,-100%)' }], { duration: 420, easing: 'ease-out' });
    await wait(700);
    var w = K.windowspot, hh = hopsFor(rug[0], rug[1], w[0], w[1]);
    await hopTo(rug[0], rug[1], 0.3, w[0], w[1], 0.1, 1300, 3, 'out');
    puff(w[0], w[1] - 0.01);
    walker.classList.remove('on'); walker.querySelector('.ready').classList.remove('on');
    walker.querySelector('.sit').classList.add('on');
    L.away = true;
  } finally { L.busy = false; }
}

async function doReturn() {
  L.busy = true; L.returning = true;
  try {
    var w = K.windowspot, rug = K.rug;
    walker.querySelector('.sit').classList.remove('on'); walker.querySelector('.ready').classList.add('on');
    placeWalker(w[0], w[1], 0.1, 'ready'); walker.classList.add('on');
    await hopTo(w[0], w[1], 0.1, rug[0], rug[1], 0.3, 1400, 3, 'in');
    await wait(300);
    await flyBag([rug[0], rug[1] - 0.06], hookSpot(), 760, 1 / 0.9 * 0.9);
    bagEl.classList.remove('off'); bagEl.classList.add('swing'); setTimeout(function () { bagEl.classList.remove('swing'); }, 1600);
    Snd.pop();
    walker.querySelector('.ready').classList.remove('on'); walker.querySelector('.sit').classList.add('on');
    L.returning = false; Sh.renderHouse(true);        // the souvenir comes onto the shelf
    L.away = false;
    await wait(900);
    var next = pick(), h = hopsFor(rug[0], rug[1], next.foot[0], next.foot[1]);
    await hopTo(rug[0], rug[1], 0.3, next.foot[0], next.foot[1], next.bw, h.ms, h.hops);
    showAct(next);
    await wait(160); walker.classList.remove('on');
  } finally { L.busy = false; L.returning = false; }
}

function queue(fn) { L.chain = L.chain.then(fn).catch(function () { L.busy = false; }); return L.chain; }

/* ---- touch ---- */
function heart(x, y) {
  var h = el('i', 'heart'); h.style.left = pct(x); h.style.top = pct(y);
  h.innerHTML = '<svg viewBox="0 0 24 22"><path d="M12 21C4 14 1.5 10.5 1.5 6.8 1.5 3.8 3.8 1.8 6.6 1.8c2.2 0 4 1.3 5.4 3.6 1.4-2.3 3.2-3.6 5.4-3.6 2.8 0 5.1 2 5.1 5C22.5 10.5 20 14 12 21z" fill="#c8442f" stroke="#fffaf0" stroke-width="1.6"/></svg>';
  topLayer.appendChild(h);
  anim(h, [{ opacity: 0, transform: 'translate(-50%,0) scale(.4)' }, { opacity: 1, transform: 'translate(-50%,-14px) scale(1)', offset: 0.25 }, { opacity: 0, transform: 'translate(-50%,-52px) scale(.9)' }], { duration: 1300, easing: 'ease-out' }).finished.then(function () { h.remove(); }, function () { h.remove(); });
}
function poke() {
  if (L.away || !L.on) return;
  var P = stagePx();
  if (L.walking || !L.cur) {
    anim(walker, [{ transform: 'translate(-50%,-100%)' }, { transform: 'translate(-50%,-100%) translateY(-9px) scale(.96,1.06)' }, { transform: 'translate(-50%,-100%)' }], { duration: 380 });
    Snd.pop(); return;
  }
  var a = L.cur, o = L.acts[a.id];
  L.pokeUntil = Date.now() + 2600;
  showB(1900);
  if (!a.still) anim(o.inner, [{ transform: 'none' }, { transform: 'scale(1.08,.9)', offset: 0.22 }, { transform: 'translateY(-' + (P.w * 0.03).toFixed(1) + 'px) scale(.96,1.07)', offset: 0.55 }, { transform: 'scale(1.04,.96)', offset: 0.8 }, { transform: 'none' }], { duration: 640, easing: 'ease-out' });
  heart(a.foot[0] + (Math.random() - 0.5) * 0.04, a.foot[1] - a.bw * 0.95 * P.w / P.h);
  Snd.purr();
}

/* ---- little visitors ---- */
function scheduleVisit() {
  clearTimeout(L.timers.visit);
  L.timers.visit = setTimeout(function () {
    var ok = !L.away && !L.paused && !L.busy && !L.visiting && L.np < 0.5 && Sh.onHouse();
    if (ok) visit(); else scheduleVisit();
  }, rnd(80, 170) * 1000);
}
function flightPath(from, to, ms, w, arc, flip) {
  var P = stagePx(), dx = (to[0] - from[0]) * P.w, dy = (to[1] - from[1]) * P.h, kf = [];
  for (var i = 0; i <= 16; i++) { var u = i / 16, e = u * u * (3 - 2 * u); kf.push({ offset: u, transform: 'translate(-50%,-100%) translate(' + (dx * e).toFixed(1) + 'px,' + (dy * e - Math.sin(Math.PI * u) * arc * P.w).toFixed(1) + 'px) scaleX(' + (flip ? -1 : 1) + ')' }); }
  return kf;
}
async function visit() {
  L.visiting = true;
  try {
    var bird = el('div', 'bird'); bird.style.width = pct(0.07);
    var fly = pic('bird_fly.webp', 'on'), perch = pic('bird_perch.webp');
    bird.appendChild(fly); bird.appendChild(perch);
    var spot = [K.glass[0], K.glass[1] - K.glass[3] - 0.012], from = [1.06, 0.1];
    bird.style.left = pct(from[0]); bird.style.top = pct(from[1]);
    topLayer.appendChild(bird);
    Snd.chirp();
    await anim(bird, flightPath(from, spot, 1500, 0, 0.06, true), { duration: 1500, easing: 'linear', fill: 'forwards' }).finished;
    bird.style.left = pct(spot[0]); bird.style.top = pct(spot[1]);
    fly.classList.remove('on'); perch.classList.add('on');
    bird.getAnimations().forEach(function (a) { a.cancel(); });
    bird.style.transform = 'translate(-50%,-100%) scaleX(-1)';
    if (L.cur && !L.busy) showB(1500);
    await wait(900); Snd.chirp();
    await anim(bird, [{ transform: 'translate(-50%,-100%) scaleX(-1) rotate(0)' }, { transform: 'translate(-50%,-100%) scaleX(-1) rotate(-10deg)' }, { transform: 'translate(-50%,-100%) scaleX(-1) rotate(8deg)' }, { transform: 'translate(-50%,-100%) scaleX(-1) rotate(0)' }], { duration: 1500 }).finished;
    await wait(500);
    // a tiny gift, left on the floor under the window
    var kind = ['feather', 'berry', 'acorn'][Math.floor(Math.random() * 3)], g = el('button', 'hot gift');
    var gi = pic('gift_' + kind + '.webp'); g.appendChild(gi); g.setAttribute('aria-label', Sh.words().gift);
    var gx = rnd(0.56, 0.68), gy = rnd(0.62, 0.7);
    g.style.left = pct(spot[0]); g.style.top = pct(spot[1] + 0.02);
    stage.appendChild(g);
    var P = stagePx();
    anim(g, [{ transform: 'translate(-50%,-50%) scale(.6)', opacity: 1 }, { transform: 'translate(-50%,-50%) translate(' + ((gx - spot[0]) * P.w * 0.5).toFixed(0) + 'px,' + ((gy - spot[1]) * P.h * 0.4).toFixed(0) + 'px) rotate(25deg) scale(1)', offset: 0.45 }, { transform: 'translate(-50%,-50%) translate(' + ((gx - spot[0]) * P.w).toFixed(0) + 'px,' + ((gy - spot[1] - 0.02) * P.h).toFixed(0) + 'px) rotate(-8deg) scale(1)' }], { duration: 1100, easing: 'ease-in', fill: 'forwards' }).finished.then(function () {
      g.style.left = pct(gx); g.style.top = pct(gy); g.getAnimations().forEach(function (a) { a.cancel(); });
      g.classList.add('landed');
    });
    g.addEventListener('click', function () { collectGift(g); });
    if (L.gift) L.gift.remove();
    L.gift = g;
    fly.classList.add('on'); perch.classList.remove('on');
    Snd.chirp();
    await anim(bird, flightPath(spot, [-0.08, 0.06], 1500, 0, 0.05, false), { duration: 1500, easing: 'ease-in', fill: 'forwards' }).finished;
    bird.remove();
  } finally { L.visiting = false; scheduleVisit(); }
}
function collectGift(g) {
  if (g.classList.contains('gone')) return;
  g.classList.add('gone'); Snd.chime();
  var r = g.getBoundingClientRect(), s = stage.getBoundingClientRect(), P = stagePx();
  heart((r.left + r.width / 2 - s.left) / P.w, (r.top - s.top) / P.h);
  setTimeout(function () { g.remove(); if (L.gift === g) L.gift = null; }, 420);
}
function scheduleButterfly() {
  clearTimeout(L.timers.fly);
  L.timers.fly = setTimeout(function () {
    if (!L.paused && L.np < 0.4 && Sh.onHouse() && !reduce) flutter();
    scheduleButterfly();
  }, rnd(120, 260) * 1000);
}
function flutter() {
  var P = stagePx(), b = el('div', 'bfly'), kf = [], y0 = rnd(0.34, 0.5), y1 = rnd(0.3, 0.46);
  b.appendChild(pic('butterfly.webp')); b.style.width = pct(0.05); b.style.left = '0'; b.style.top = '0';
  topLayer.appendChild(b);
  for (var i = 0; i <= 40; i++) { var u = i / 40; kf.push({ offset: u, transform: 'translate(' + ((-0.06 + 1.12 * u) * P.w).toFixed(0) + 'px,' + (((y0 + (y1 - y0) * u) + Math.sin(u * 17) * 0.025 + Math.sin(u * 5) * 0.03) * P.h).toFixed(0) + 'px)' }); }
  anim(b, kf, { duration: 11000, easing: 'linear' }).finished.then(function () { b.remove(); }, function () { b.remove(); });
}

/* ---- the hour, once a minute ---- */
function tick() {
  var h = hourNow(), np = nightAmount(h), before = L.np;
  L.np = np;
  var night = np > 0.55;
  document.getElementById('p-night').style.opacity = np;
  layer.classList.toggle('is-night', night);
  layer.style.setProperty('--np', np);
  mullNight.style.opacity = np; mullDay.style.opacity = 1 - np;
  lampEl.style.opacity = Math.max(0, (np - 0.35) / 0.65);
  beam.style.opacity = Math.max(0, 1 - np * 1.6) * (1 - warmth(h) * 1.5);
  tint.style.opacity = warmth(h) * 1.2;
  bagEl.classList.toggle('lit', night);
  Snd.setNight(night);
  // the cat does what the hour suits: at dusk a day thing gives way to a night thing, and back
  if (L.cur && !L.away && !L.busy && L.cur.night !== night) queue(function () { return changeAct(pick()); });
  return before;
}

/* ---- host side ---- */
function ensureOn() {
  build(); L.on = true; layer.style.display = '';
  ['p-away', 'p-nightaway'].forEach(function (id) { document.getElementById(id).classList.remove('on'); });
  document.getElementById('p-home').classList.add('on');
  document.getElementById('p-night').classList.remove('on');
  tick();
  clearInterval(L.timers.tick); L.timers.tick = setInterval(tick, 30000);
}
function stop() {
  if (!L.built || !L.on) return;
  L.on = false; layer.style.display = 'none';
  Object.keys(L.timers).forEach(function (k) { clearTimeout(L.timers[k]); clearInterval(L.timers[k]); });
  document.getElementById('p-night').style.opacity = '';
  Sh.hotCat(null);
  if (L.gift) { L.gift.remove(); L.gift = null; }
  L.cur = null; L.away = null; L.busy = false;
  Object.keys(L.acts).forEach(function (k) { L.acts[k].root.classList.remove('on'); });
  walker.classList.remove('on');
}
/* the page's state has changed: bring the room to match, with a leave or a return when it is the cat that moved */
function sync(view) {
  var p = (view && view.presence) || {};
  if (!L.on) {
    ensureOn();
    L.away = !!p.away;
    if (L.away) { bagEl.classList.add('off'); Sh.hotCat(null); }
    else { bagEl.classList.remove('off'); showAct(firstPick(), false); }
    scheduleNext(); scheduleVisit(); scheduleButterfly();
    return;
  }
  var away = !!p.away;
  if (away === !!L.away) return;
  if (away) queue(function () { return L.away ? Promise.resolve() : doLeave(); });
  else queue(function () { return L.away ? doReturn() : Promise.resolve(); });
}
function pause(on) {
  on = !!on;
  if (L.paused === on) return;
  L.paused = on;
  if (!L.built) return;
  layer.classList.toggle('paused', on);
  try { stage.getAnimations({ subtree: true }).forEach(function (a) { if (on) a.pause(); else a.play(); }); } catch (e) { /* older engine: CSS pause still holds */ }
  if (!on) { tick(); }
}
function holding() { return L.returning; }

window.CatLife = { sync: sync, stop: stop, poke: poke, pause: pause, holding: holding, collect: function () { if (L.gift) collectGift(L.gift); },
  get away() { return L.away; }, get cur() { return L.cur && L.cur.id; }, get np() { return L.np; }, get busy() { return L.busy; }, act: function (id) { var a = K.acts.filter(function (x) { return x.id === id; })[0]; if (a) queue(function () { return changeAct(a); }); }, visit: function () { if (!L.visiting) visit(); } };
})();
