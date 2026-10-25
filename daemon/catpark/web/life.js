(function () {
'use strict';
/* The house is alive. The room is two empty pictures (day, night) that fade
   into each other with the real hour; the cat is its own picture on top, one
   pose per thing it does, so it can go from one thing to another, hop across
   the room, leave through the window with its bag, and come home. Things it
   has taken up stand in the room as props you can tap; the jar fills with
   stars; the bowl takes food. All motion is transform and opacity only, and
   every timer sleeps while the page is off screen. */

var K = window.CAT_KIT;
var Sh = window.CatShell;
var noop = function () {};
var Snd = window.CatSound || { unlock: noop, pause: noop, setNight: noop, purr: noop, paper: noop, pop: noop, chime: noop, thump: noop, chirp: noop, clink: noop, hobby: noop, munch: noop, meow: noop };
if (!K || !Sh) return;

var BASE = 'a/';
var stage = document.getElementById('stage');
var M = K.meta;
var L = { built: false, on: false, away: null, cur: null, busy: false, paused: false, walking: false, returning: false, last: [], timers: {}, acts: {}, np: 0, chain: Promise.resolve(), gift: null, visiting: false, pokeUntil: 0, holdUntil: 0, props: {}, greeted: false, attending: null };
var layer, sky, walker, bagEl, tint, lampEl, beam, mullDay, mullNight, topLayer, propLayer, jarEl, jarStars, bowlEl, bowlFood, yarnEl, think;
var reduce = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
var ACT = {}; K.acts.forEach(function (a) { ACT[a.id] = a; });

function el(tag, cls) { var e = document.createElement(tag); if (cls) e.className = cls; return e; }
function pic(file, cls) { var i = new Image(); i.src = BASE + file; i.alt = ''; i.draggable = false; if (cls) i.className = cls; return i; }
function pct(v) { return (v * 100) + '%'; }
function wait(ms) { return new Promise(function (r) { setTimeout(r, ms); }); }
function rnd(a, b) { return a + Math.random() * (b - a); }
function stagePx() { return { w: stage.offsetWidth || 400, h: stage.offsetHeight || 711 }; }
function place(e, name) { var m = M[name]; e.style.left = pct(m[0]); e.style.top = pct(m[1]); e.style.width = pct(m[2]); }
function anim(e, kf, o) { try { return e.animate(kf, o); } catch (err) { return { finished: Promise.resolve(), cancel: function () {} }; } }
function center(name) { var m = M[name]; return [m[0] + m[2] / 2, m[1] + m[3] / 2]; }
function knows(h) { return Sh.cat().hobbies.indexOf(h) >= 0; }

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
  stage.insertBefore(layer, document.getElementById('frames'));
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

  // things you can tap: the props of its hobbies, the jar, the bowl, the yarn
  propLayer = el('div', 'props'); layer.appendChild(propLayer);
  jarEl = el('button', 'prop jar'); place(jarEl, 'p_jar'); jarEl.appendChild(pic('p_jar.webp'));
  jarStars = el('div', 'jarstars'); jarEl.appendChild(jarStars);
  jarEl.addEventListener('click', function () { bounce(jarEl); Sh.openJar(); });
  bowlEl = el('button', 'prop bowl'); place(bowlEl, 'p_bowl'); bowlEl.appendChild(pic('p_bowl.webp'));
  bowlFood = el('img', 'food'); bowlEl.appendChild(bowlFood);
  bowlEl.addEventListener('click', function () { bounce(bowlEl); Snd.pop(); Sh.openFood(); });
  yarnEl = el('button', 'prop yarn'); place(yarnEl, 't_yarn');
  yarnEl.appendChild(pic('t_yarn.webp'));
  yarnEl.addEventListener('click', function () { bounce(yarnEl); Snd.pop(); Sh.play(); });
  [jarEl, bowlEl, yarnEl].forEach(function (p) { p.setAttribute('aria-label', Sh.words()[p.classList[1]] || ''); propLayer.appendChild(p); });

  walker = el('div', 'walker fx');
  walker.appendChild(pic('cat_' + K.sit + '.webp', 'w sit on')); walker.appendChild(pic('cat_' + K.ready + '.webp', 'w ready'));
  layer.appendChild(walker);
  think = el('div', 'think'); layer.appendChild(think);

  topLayer = el('div', 'top'); layer.appendChild(topLayer);
  tint = el('div', 'tint'); layer.appendChild(tint);
  K.acts.forEach(function (a) { var s = new Image(); s.src = BASE + 'cat_' + a.a + '.webp'; if (a.b) { var t = new Image(); t.src = BASE + 'cat_' + a.b + '.webp'; } });
}
function bounce(n) { anim(n, [{ transform: 'scale(1)' }, { transform: 'scale(1.12,.9)' }, { transform: 'scale(.95,1.08)' }, { transform: 'scale(1)' }], { duration: 420, easing: 'ease-out' }); }

/* the props of the hobbies it knows; a new one glows until it has been tapped */
function renderProps() {
  var c = Sh.cat(), night = L.np > 0.55;
  Object.keys(K.props).forEach(function (h) {
    var want = c.hobbies.indexOf(h) >= 0 && !L.away, p = L.props[h];
    if (want && !p) {
      p = el('button', 'prop hob'); p.dataset.h = h; place(p, K.props[h]); p.appendChild(pic(K.props[h] + '.webp'));
      p.setAttribute('aria-label', (Sh.cwords().hobbies || {})[h] || h);
      p.addEventListener('click', function () { bounce(p); p.classList.remove('new'); Sh.markLearnt('prop:' + h); Sh.hobby(h); });
      if (!Sh.learnt('prop:' + h)) p.classList.add('new');
      propLayer.insertBefore(p, propLayer.firstChild); L.props[h] = p;
      void p.offsetWidth; p.classList.add('in');
    } else if (!want && p) { p.remove(); delete L.props[h]; }
    if (p) p.classList.toggle('busy', !!(L.cur && L.cur.hobby === h));
  });
  [jarEl, bowlEl, yarnEl].concat(Object.keys(L.props).map(function (k) { return L.props[k]; })).forEach(function (p) { p.classList.toggle('lit', night); });
  yarnEl.style.display = L.away ? 'none' : '';
  bowlFood.classList.toggle('on', !!c.bowl);
  if (c.bowl) bowlFood.src = BASE + 't_' + c.bowl + '.webp';
  renderJar();
}

/* the jar shows the stars toward the next level as little lights inside */
function renderJar() {
  var c = Sh.cat(), n = c.need ? Math.round(Math.min(1, c.stars / c.need) * 9) : 9;
  if (jarStars.childElementCount !== n) {
    jarStars.innerHTML = '';
    for (var i = 0; i < n; i++) { var s = el('i'); s.style.left = (18 + ((i * 37) % 64)) + '%'; s.style.bottom = (8 + Math.floor(i / 3) * 18 + (i % 2) * 5) + '%'; s.style.animationDelay = (i * 0.4) + 's'; jarStars.appendChild(s); }
  }
}

function ensureAct(a) {
  var o = L.acts[a.id];
  if (o) return o;
  var root = el('div', 'act fx' + (a.still ? ' still' : '')), inner = el('div', 'in');
  place(root, a.a);
  var m = M[a.a];
  if (!a.still) root.appendChild(el('i', 'sh'));
  var fa = pic('cat_' + a.a + '.webp', 'fa'), fb = null;
  inner.appendChild(fa);
  if (a.b) {
    var mb = M[a.b];
    fb = pic('cat_' + a.b + '.webp', 'fb');
    fb.style.left = pct((mb[0] - m[0]) / m[2]); fb.style.top = pct((mb[1] - m[1]) / m[3]); fb.style.width = pct(mb[2] / m[2]);
    inner.appendChild(fb);
  }
  root.appendChild(inner);
  if (a.steam) for (var i = 0; i < 3; i++) { var s = el('i', 'steam'); s.style.left = pct(a.steam[0]); s.style.top = pct(a.steam[1]); s.style.animationDelay = (i * 1.1) + 's'; root.appendChild(s); }
  if (a.zz) for (var z = 0; z < 2; z++) { var zz = el('i', 'zz'); zz.style.left = pct(a.zz[0]); zz.style.top = pct(a.zz[1]); zz.style.animationDelay = (z * 2.2) + 's'; zz.innerHTML = '<svg viewBox="0 0 20 20"><path d="M4 4h11L4 16h12" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"/></svg>'; root.appendChild(zz); }
  layer.insertBefore(root, walker);
  o = L.acts[a.id] = { a: a, root: root, inner: inner, fb: fb };
  return o;
}

/* ---- what the cat does on its own: by the hour, by habit, never twice running ---- */
function pick() {
  var h = hourNow(), p = periodOf(h, L.np), c = Sh.cat();
  var list = K.acts.filter(function (a) { return !a.react && a.when[p] && (!a.hobby || knows(a.hobby)); });
  var fresh = list.filter(function (a) { return L.last.indexOf(a.id) < 0; });
  if (fresh.length) list = fresh;
  var weight = function (a) { return a.when[p] * (a.hobby ? (a.hobby === c.fav_hobby ? 3 : 1.6) : 1); };
  var sum = 0; list.forEach(function (a) { sum += weight(a); });
  var r = Math.random() * sum, got = list[0];
  for (var i = 0; i < list.length; i++) { r -= weight(list[i]); if (r <= 0) { got = list[i]; break; } }
  return got;
}
function remember(a) {
  if (a.react) return;
  L.last.unshift(a.id); L.last.length = Math.min(L.last.length, 2);
  try { localStorage.setItem('cat.last', a.id); } catch (e) { /* no storage: the room still varies */ }
}
function firstPick() {
  var before = ''; try { before = localStorage.getItem('cat.last') || ''; } catch (e) { before = ''; }
  if (before) L.last = [before];
  return pick();
}
function headOf(a) { var m = M[a.a], P = stagePx(); return [a.foot[0], a.foot[1] - Math.min(m[3], a.bw * 0.95 * P.w / P.h)]; }

function showAct(a) {
  var o = ensureAct(a);
  L.cur = a; remember(a);
  o.root.classList.add('on');
  o.inner.classList.remove('b');
  yarnEl.style.visibility = a.id === 'yarn' ? 'hidden' : '';  // the yarn is in its paws
  // the cat's own hotspot follows where it sits
  var P = stagePx(), bh = a.bw * 0.95 * P.w / P.h;
  Sh.hotCat(a.foot[0], a.foot[1] - bh / 2, Math.max(a.bw * 1.15, 0.2), Math.max(bh, 0.1));
  scheduleGlance();
  if (a.hobby) { Snd.hobby(a.hobby); renderProps(); }
}
function hideAct(a) { var o = L.acts[a.id]; if (o) o.root.classList.remove('on'); if (a.id === 'yarn') yarnEl.style.visibility = ''; clearTimeout(L.timers.glance); if (a.hobby) setTimeout(renderProps, 0); }

function showB(ms) {
  var a = L.cur; if (!a) return;
  var o = L.acts[a.id]; if (!o || !o.fb) return;
  o.inner.classList.add('b');
  clearTimeout(o.bt);
  o.bt = setTimeout(function () { o.inner.classList.remove('b'); }, ms);
}
function scheduleGlance() {
  clearTimeout(L.timers.glance);
  var a = L.cur; if (!a) return;
  var held = Date.now() < L.holdUntil;
  L.timers.glance = setTimeout(function () {
    if (!L.paused && !L.busy && L.cur === a && Date.now() > L.pokeUntil) showB(a.night ? 2200 : 1300);
    scheduleGlance();
  }, (held ? rnd(2.5, 4.5) : a.still ? rnd(22, 36) : rnd(8, 16)) * 1000);
}
function scheduleNext() {
  clearTimeout(L.timers.next);
  L.timers.next = setTimeout(function () {
    var waiting = window.CatGuide && window.CatGuide.step === 'pet';   // it stays put while you learn to pet it
    if (L.away || L.paused || L.busy || L.attending || waiting || Date.now() < L.holdUntil || !Sh.onHouse()) { scheduleNext(); return; }
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
/* where the cat is now, as a foot point and body width (an act, the walker, or the rug) */
function whereNow() {
  if (L.cur) return { x: L.cur.foot[0], y: L.cur.foot[1], bw: L.cur.bw };
  if (L.attending) return L.attending;
  return { x: K.rug[0], y: K.rug[1], bw: 0.3 };
}

async function changeAct(next) {
  var cur = L.cur;
  if (cur === next) return;
  L.busy = true;
  try {
    var from = whereNow();
    placeWalker(from.x, from.y, from.bw, 'sit');
    walker.classList.add('on'); hideThink();
    if (cur) { hideAct(cur); L.cur = null; Sh.hotCat(null); }
    L.attending = null;
    await wait(220);
    if (Math.hypot(from.x - next.foot[0], from.y - next.foot[1]) > 0.01) {
      var h = hopsFor(from.x, from.y, next.foot[0], next.foot[1]);
      await hopTo(from.x, from.y, from.bw, next.foot[0], next.foot[1], next.bw, h.ms, h.hops);
    }
    showAct(next); walker.classList.remove('on');
    await wait(460);
  } finally { L.busy = false; }
}

/* the person asked for this: do it for a while, looking up now and then */
function doAct(id, ms) {
  var a = ACT[id]; if (!a || L.away) return Promise.resolve();
  if (a.night && L.np < 0.55 && ACT[id + '_day']) a = ACT[id + '_day'];
  L.holdUntil = Date.now() + (ms || 15000);
  return queue(function () { return changeAct(a); }).then(function () { scheduleGlance(); });
}
/* a short reaction, then back to what it was doing */
function react(id, ms) {
  var a = ACT[id]; if (!a || L.away) return Promise.resolve();
  var back = L.cur && !L.cur.react ? L.cur : null;
  L.holdUntil = Date.now() + (ms || 2600) + 1500;
  if (id === 'full') Snd.meow(0.7); else if (id === 'want' || id === 'greet') Snd.meow(1);
  return queue(function () { return changeAct(a); }).then(function () {
    showB(900);
    return wait(ms || 2600);
  }).then(function () {
    if (L.cur === a) return queue(function () { return changeAct(back && (!back.night === !(L.np > 0.55)) ? back : pick()); });
  });
}
function eat(food) {
  bowlFood.src = BASE + 't_' + food + '.webp'; bowlFood.classList.add('on');
  Snd.pop();
  var then = food === 'catnip' ? react('catnip', 4200) : doAct('eat', 9000).then(function () { Snd.munch(); });
  return then.then(function () { bowlFood.classList.remove('on'); hearts(2); });
}
function nap() {
  if (L.away) return;
  doAct(L.np > 0.55 ? 'nsleep' : 'bed', 24000);
  Snd.purr();
}

/* ---- guidance: the cat walks over to a thing and thinks of it ---- */
function attend(spot, icon) {
  if (L.away) return Promise.resolve();
  return queue(async function () {
    L.busy = true;
    try {
      var from = whereNow();
      placeWalker(from.x, from.y, from.bw, 'sit'); walker.classList.add('on');
      if (L.cur) { hideAct(L.cur); L.cur = null; }
      await wait(200);
      var h = hopsFor(from.x, from.y, spot[0], spot[1]);
      await hopTo(from.x, from.y, from.bw, spot[0], spot[1], 0.24, h.ms, h.hops);
      L.attending = { x: spot[0], y: spot[1], bw: 0.24 };
      Sh.hotCat(spot[0], spot[1] - 0.05, 0.26, 0.1);
      showThink(icon, spot);
    } finally { L.busy = false; }
  });
}
function showThink(icon, spot) {
  think.innerHTML = '';
  if (!icon) return;
  var P = stagePx();
  think.appendChild(icon);
  think.style.left = pct(spot[0]); think.style.top = pct(spot[1] - 0.24 * 0.95 * P.w / P.h - 0.01);
  think.classList.remove('on'); void think.offsetWidth; think.classList.add('on');
}
function hideThink() { think.classList.remove('on'); }
/* the cat stops attending and goes back to doing something */
function release() {
  hideThink();
  if (!L.attending || L.away || Date.now() < L.holdUntil) return;   // already off to do what was asked
  queue(function () { return changeAct(pick()); });
}

/* ---- the greeting: how long you were away decides how glad it is ---- */
function greet() {
  if (L.away || L.greeted) return;
  L.greeted = true;
  var c = Sh.cat(), gap = c.gap || 0, level = c.level || 1;
  if (L.np > 0.55 && L.cur && L.cur.still) { showB(2400); return; }   // asleep at night: one sleepy look
  var big = gap > 20 * 3600e3 || c.visits <= 1, warm = gap > 3600e3 || level >= 3;
  if (!big && !warm) { setTimeout(function () { showB(1600); Snd.meow(0.6); }, 900); return; }
  setTimeout(function () {
    react('greet', big ? 3600 : 2200).then(function () { hearts(big ? 3 : 1); });
    if (big) setTimeout(function () { hearts(2); }, 1200);
    // it remembers what you last did together, and asks for it again
    var lastFeed = (c.memory || []).filter(function (m) { return m.what === 'feed'; }).pop();
    if (!big && lastFeed && (c.today.feed || 0) < 2 && (Sh.yard().pantry[lastFeed.arg] || 0) > 0) setTimeout(function () { wantFood(lastFeed.arg); }, 4600);
  }, 700);
}
function wantFood(food) {
  react('want', 3800);
  var i = new Image(); i.src = BASE + 't_' + food + '.webp'; i.alt = '';
  setTimeout(function () { var a = L.cur; if (a) showThink(i, a.foot); setTimeout(hideThink, 3200); }, 900);
}
function hearts(n) {
  var a = L.cur; if (!a) return;
  var hd = headOf(a);
  for (var i = 0; i < n; i++) (function (k) { setTimeout(function () { heart(hd[0] + (Math.random() - 0.5) * 0.06, hd[1]); }, k * 260); })(i);
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
    clearTimeout(L.timers.glance); hideThink();
    var from = whereNow(), rug = K.rug;
    placeWalker(from.x, from.y, from.bw, 'sit'); walker.classList.add('on');
    if (L.cur) { hideAct(L.cur); L.cur = null; Sh.hotCat(null); }
    L.attending = null;
    await wait(300);
    if (Math.hypot(from.x - rug[0], from.y - rug[1]) > 0.05) { var h = hopsFor(from.x, from.y, rug[0], rug[1]); await hopTo(from.x, from.y, from.bw, rug[0], rug[1], 0.3, h.ms, h.hops); }
    else placeWalker(rug[0], rug[1], 0.3, 'sit');
    // the bag comes off its hook and onto the cat
    bagEl.classList.add('off');
    await flyBag(hookSpot(), [rug[0], rug[1] - 0.06], 760, 0.9);
    walker.querySelector('.sit').classList.remove('on'); walker.querySelector('.ready').classList.add('on');
    Snd.pop();
    anim(walker, [{ transform: 'translate(-50%,-100%) scale(1.08,.9)' }, { transform: 'translate(-50%,-100%) scale(.96,1.06)' }, { transform: 'translate(-50%,-100%)' }], { duration: 420, easing: 'ease-out' });
    await wait(700);
    var w = K.windowspot;
    await hopTo(rug[0], rug[1], 0.3, w[0], w[1], 0.1, 1300, 3, 'out');
    puff(w[0], w[1] - 0.01);
    walker.classList.remove('on'); walker.querySelector('.ready').classList.remove('on');
    walker.querySelector('.sit').classList.add('on');
    L.away = true;
    renderProps();
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
    await flyBag([rug[0], rug[1] - 0.06], hookSpot(), 760, 1);
    bagEl.classList.remove('off'); bagEl.classList.add('swing'); setTimeout(function () { bagEl.classList.remove('swing'); }, 1600);
    Snd.pop();
    walker.querySelector('.ready').classList.remove('on'); walker.querySelector('.sit').classList.add('on');
    L.returning = false; L.away = false; Sh.renderHouse(true);        // the souvenir comes onto the shelf, the props come back
    renderProps();
    await wait(900);
    var next = Sh.cat().bowl ? ACT.eat : pick(), h = hopsFor(rug[0], rug[1], next.foot[0], next.foot[1]);
    await hopTo(rug[0], rug[1], 0.3, next.foot[0], next.foot[1], next.bw, h.ms, h.hops);
    showAct(next); walker.classList.remove('on');
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
  if (L.attending) { release(); }
  if (L.walking || !L.cur) {
    anim(walker, [{ transform: 'translate(-50%,-100%)' }, { transform: 'translate(-50%,-100%) translateY(-9px) scale(.96,1.06)' }, { transform: 'translate(-50%,-100%)' }], { duration: 380 });
    Snd.purr(); if (L.attending || !L.cur) heart(whereNow().x, whereNow().y - 0.08);
    return;
  }
  var a = L.cur, o = L.acts[a.id];
  L.pokeUntil = Date.now() + 2600;
  showB(1900);
  if (!a.still) anim(o.inner, [{ transform: 'none' }, { transform: 'scale(1.08,.9)', offset: 0.22 }, { transform: 'translateY(-' + (P.w * 0.03).toFixed(1) + 'px) scale(.96,1.07)', offset: 0.55 }, { transform: 'scale(1.04,.96)', offset: 0.8 }, { transform: 'none' }], { duration: 640, easing: 'ease-out' });
  var hd = headOf(a);
  heart(hd[0] + (Math.random() - 0.5) * 0.04, hd[1]);
  Snd.purr();
}

/* ---- little visitors ---- */
function scheduleVisit() {
  clearTimeout(L.timers.visit);
  L.timers.visit = setTimeout(function () {
    var ok = !L.away && !L.paused && !L.busy && !L.visiting && !L.attending && L.np < 0.5 && Sh.onHouse();
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
    // a little gift, left on the floor under the window: a berry or a fish, for the pantry
    var kind = Math.random() < 0.6 ? 'berry' : 'fish', g = el('button', 'hot gift');
    g.appendChild(pic('t_' + kind + '.webp')); g.setAttribute('aria-label', Sh.words().gift);
    var gx = rnd(0.56, 0.68), gy = rnd(0.62, 0.68);
    g.style.left = pct(spot[0]); g.style.top = pct(spot[1] + 0.02);
    stage.appendChild(g);
    var P = stagePx();
    anim(g, [{ transform: 'translate(-50%,-50%) scale(.6)', opacity: 1 }, { transform: 'translate(-50%,-50%) translate(' + ((gx - spot[0]) * P.w * 0.5).toFixed(0) + 'px,' + ((gy - spot[1]) * P.h * 0.4).toFixed(0) + 'px) rotate(25deg) scale(1)', offset: 0.45 }, { transform: 'translate(-50%,-50%) translate(' + ((gx - spot[0]) * P.w).toFixed(0) + 'px,' + ((gy - spot[1] - 0.02) * P.h).toFixed(0) + 'px) rotate(-8deg) scale(1)' }], { duration: 1100, easing: 'ease-in', fill: 'forwards' }).finished.then(function () {
      g.style.left = pct(gx); g.style.top = pct(gy); g.getAnimations().forEach(function (a) { a.cancel(); });
      g.classList.add('landed');
    });
    g.addEventListener('click', function () { collectGift(g, kind); });
    if (L.gift) L.gift.remove();
    L.gift = g;
    fly.classList.add('on'); perch.classList.remove('on');
    Snd.chirp();
    await anim(bird, flightPath(spot, [-0.08, 0.06], 1500, 0, 0.05, false), { duration: 1500, easing: 'ease-in', fill: 'forwards' }).finished;
    bird.remove();
  } finally { L.visiting = false; scheduleVisit(); }
}
function collectGift(g, kind) {
  if (g.classList.contains('gone')) return;
  g.classList.add('gone'); Snd.chime();
  var r = g.getBoundingClientRect(), s = stage.getBoundingClientRect(), P = stagePx();
  var at = [(r.left + r.width / 2 - s.left) / P.w, (r.top - s.top) / P.h];
  heart(at[0], at[1]);
  var y = Sh.yard(), t = Sh.cat().today || {};
  if ((t.gift || 0) < 2) { Sh.act('gift', kind); y.pantry[kind] = (y.pantry[kind] || 0) + 1; t.gift = (t.gift || 0) + 1; Sh.flyStar(at); }
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

/* ---- the hour, twice a minute ---- */
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
  renderProps();
  // the cat does what the hour suits: at dusk a day thing gives way to a night thing, and back
  if (L.cur && !L.away && !L.busy && !L.cur.react && !!L.cur.night !== night && Date.now() > L.holdUntil) queue(function () { return changeAct(pick()); });
  return before;
}

/* ---- host side ---- */
function ensureOn() {
  build(); L.on = true; layer.style.display = '';
  tick();
  clearInterval(L.timers.tick); L.timers.tick = setInterval(tick, 30000);
}
/* the page's state has changed: bring the room to match, with a leave or a return when it is the cat that moved */
function sync(view) {
  if (!view) return;
  var p = view.presence || {};
  if (!L.on) {
    ensureOn();
    L.away = !!p.away;
    if (L.away) { bagEl.classList.add('off'); Sh.hotCat(null); }
    else { bagEl.classList.remove('off'); showAct(firstPick()); }
    renderProps();
    scheduleNext(); scheduleVisit(); scheduleButterfly();
    setTimeout(greet, 400);
    return;
  }
  renderProps();
  var away = !!p.away;
  if (away === !!L.away) return;
  // the cat leaves and comes home in front of you: off screen it waits for you to look
  if (away) queue(function () { return seen().then(function () { return L.away || !Sh.away() ? null : doLeave(); }); });
  else queue(function () { return seen().then(function () { return L.away && !Sh.away() ? doReturn() : null; }); });
}
async function seen() { while (L.paused || !Sh.onHouse()) await wait(400); }
function pause(on) {
  on = !!on;
  if (L.paused === on) return;
  L.paused = on;
  if (!L.built) return;
  layer.classList.toggle('paused', on);
  try { stage.getAnimations({ subtree: true }).forEach(function (a) { if (on) a.pause(); else a.play(); }); } catch (e) { /* older engine: CSS pause still holds */ }
  if (!on) { tick(); L.greeted = false; setTimeout(greet, 600); }
}
function holding() { return L.returning || (L.on && L.away && !Sh.away()); }   // home in the state, not yet through the window
function catPoint() { var a = L.cur; if (a) return headOf(a); var w = whereNow(); return [w.x, w.y - 0.08]; }
function jarPoint() { return center('p_jar'); }
function jarClink() {
  if (!jarEl) return;
  jarEl.classList.remove('clink'); void jarEl.offsetWidth; jarEl.classList.add('clink');
  setTimeout(renderJar, 200);
}
function levelUp() {
  if (!jarEl) return;
  jarEl.classList.remove('burst'); void jarEl.offsetWidth; jarEl.classList.add('burst');
  var c = center('p_jar');
  for (var i = 0; i < 8; i++) (function (k) {
    var s = el('i', 'spark'); s.style.left = pct(c[0]); s.style.top = pct(c[1]); topLayer.appendChild(s);
    var ang = k / 8 * Math.PI * 2, P = stagePx(), d = P.w * 0.12;
    anim(s, [{ transform: 'translate(-50%,-50%) scale(.4)', opacity: 1 }, { transform: 'translate(-50%,-50%) translate(' + (Math.cos(ang) * d).toFixed(0) + 'px,' + (Math.sin(ang) * d).toFixed(0) + 'px) scale(1)', opacity: 0 }], { duration: 900, easing: 'ease-out' }).finished.then(function () { s.remove(); }, function () { s.remove(); });
  })(i);
  Snd.chime();
  if (!L.away) setTimeout(function () { react('greet', 2200).then(function () { hearts(3); }); }, 700);
}

window.CatLife = { sync: sync, poke: poke, pause: pause, holding: holding, doAct: doAct, react: react, eat: eat, nap: nap, attend: attend, release: release, greet: greet,
  catPoint: catPoint, jarPoint: jarPoint, jarClink: jarClink, levelUp: levelUp, hearts: hearts,
  bowlFill: function (k) { bowlFood.src = BASE + 't_' + k + '.webp'; bowlFood.classList.add('on'); bounce(bowlEl); },
  bowlWiggle: function () { bounce(bowlEl); },
  bagBounce: function () { bagEl.classList.remove('swing'); void bagEl.offsetWidth; bagEl.classList.add('swing'); },
  prop: function (h) { return L.props[h]; }, propSpot: function (h) { var m = M[K.props[h]]; return m ? [m[0] + m[2] / 2, Math.min(0.88, m[1] + m[3] + 0.02)] : null; },
  collect: function () { if (L.gift) L.gift.click(); }, visit: function () { if (!L.visiting) visit(); },
  get away() { return L.away; }, get cur() { return L.cur && L.cur.id; }, get np() { return L.np; }, get busy() { return L.busy; }, get held() { return Date.now() < L.holdUntil; }, get attending() { return !!L.attending; },
  act: function (id) { doAct(id, 15000); } };
})();
