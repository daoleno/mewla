(function () {
'use strict';
/* The house is alive. The room is two pictures of the cat's home (day, night)
   that fade into each other with the real hour. The cat keeps a day like a
   person: tea at dawn, the desk in the morning, lunch, a nap or a hobby, the
   stove at dusk, the lamp in the evening, bed. At each of its places it is
   painted into that corner of the room, mostly with its back to you, as a day
   and a night picture that fade with the room; between places, and for what
   it does on the rug, it is its own picture on top. It notices you after a
   while, goes out by the door with its satchel and comes home the same way.
   Things it has taken up stand in the room as props you can tap; letters wait
   on the doormat; the bowl on the table takes food. All motion is transform
   and opacity only, and every timer sleeps while the page is off screen. */

var K = window.CAT_KIT;
var Sh = window.CatShell;
var noop = function () {};
var Snd = window.CatSound || { unlock: noop, pause: noop, setNight: noop, purr: noop, paper: noop, pop: noop, chime: noop, thump: noop, chirp: noop, clink: noop, hobby: noop, munch: noop, meow: noop };
if (!K || !Sh) return;

var BASE = 'a/';
var stage = document.getElementById('stage');
var M = K.meta;
var L = { built: false, on: false, away: null, cur: null, busy: false, paused: false, walking: false, returning: false, last: [], timers: {}, acts: {}, np: 0, chain: Promise.resolve(), gift: null, visiting: false, pokeUntil: 0, holdUntil: 0, props: {}, greeted: false, attending: null };
var layer, walker, pegEl, mailEl, tint, beam, topLayer, propLayer, bowlEl, bowlFood, yarnEl, think;
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
function knows(h) { return Sh.cat().hobbies.indexOf(h) >= 0; }
/* on a narrow screen the room is wider than the view: it slides gently so the cat at the bed or the table is not cut off */
function follow(x) {
  var W = stage.offsetWidth, V = (stage.parentNode && stage.parentNode.offsetWidth) || window.innerWidth, room = (W - V) / 2;
  if (room < 4) { stage.style.setProperty('--pan', '0px'); return; }
  var pan = parseFloat(stage.style.getPropertyValue('--pan')) || 0, m = V * 0.16;
  var sx = x * W - (W - V) / 2 + pan;   // where x is on screen now
  if (sx < m) pan += m - sx; else if (sx > V - m) pan -= sx - (V - m); else return;
  pan = Math.max(-room, Math.min(room, pan));
  stage.style.setProperty('--pan', pan.toFixed(0) + 'px');
}
/* a corner of the room in its day and night pictures; the night one fades in with the hour */
function pair(box, day, night, cls) {
  var d = el('div', cls); place(d, box);
  d.appendChild(pic(day + '.webp', 'd')); d.appendChild(pic(night + '.webp', 'n'));
  return d;
}

/* ---- the hour ---- */
function hourNow() { return Sh.hour(); }
function nightAmount(h) {
  if (h >= 20.5 || h < 5.5) return 1;
  var t = h >= 17.5 ? (h - 17.5) / 3 : h < 7.5 ? 1 - (h - 5.5) / 2 : 0;
  return t * t * (3 - 2 * t);
}
/* the cat's day, by the clock: what part of it this is */
function slotOf(h) { return Sh.slotOf(h); }
function warmth(h) { return Math.max(0, 1 - Math.abs(h - 18.7) / 1.6) * 0.2 + Math.max(0, 1 - Math.abs(h - 6.4) / 1.0) * 0.13; }

/* ---- build the layer once ---- */
function build() {
  if (L.built) return;
  L.built = true;
  layer = el('div'); layer.id = 'life';
  stage.insertBefore(layer, document.getElementById('frames'));

  // light through the round window, by day
  beam = el('div', 'beam'); layer.appendChild(beam);
  for (var i = 0; i < 7; i++) { var mo = el('i', 'mote'); mo.style.left = (40 + Math.random() * 30) + '%'; mo.style.top = (42 + Math.random() * 30) + '%'; mo.style.animationDelay = (-Math.random() * 14) + 's'; mo.style.animationDuration = (11 + Math.random() * 8) + 's'; beam.appendChild(mo); }

  // the satchel is painted on its peg; while it is out, the peg is empty. Letters wait on the doormat.
  pegEl = pair('h_peg_d', 'h_peg_d', 'h_peg_n', 'corner peg'); layer.appendChild(pegEl);
  mailEl = pair('h_mail_d', 'h_mail_d', 'h_mail_n', 'corner mail'); layer.appendChild(mailEl);

  // things you can tap: the props of its hobbies, the bowl on the table, the yarn in its basket
  propLayer = el('div', 'props'); layer.appendChild(propLayer);
  bowlEl = el('button', 'prop bowl'); place(bowlEl, 'p_bowl');
  bowlFood = el('img', 'food'); bowlEl.appendChild(bowlFood);
  bowlEl.addEventListener('click', function () { Snd.pop(); Sh.openFood(); });
  yarnEl = el('button', 'prop yarn'); place(yarnEl, 't_yarn');
  yarnEl.addEventListener('click', function () { Snd.pop(); Sh.play(); });
  [[bowlEl, 'p_bowl'], [yarnEl, 't_yarn']].forEach(function (b) { b[0].style.height = pct(M[b[1]][3]); b[0].setAttribute('aria-label', Sh.words()[b[0].classList[1]] || ''); propLayer.appendChild(b[0]); });

  walker = el('div', 'walker fx');
  walker.appendChild(pic('cat_' + K.sit + '.webp', 'w sit on')); walker.appendChild(pic('cat_' + K.ready + '.webp', 'w ready'));
  layer.appendChild(walker);
  think = el('div', 'think'); layer.appendChild(think);

  topLayer = el('div', 'top'); layer.appendChild(topLayer);
  tint = el('div', 'tint'); layer.appendChild(tint);
  K.acts.forEach(function (a) { [a.a, a.b, a.n].forEach(function (f) { if (f) { var s = new Image(); s.src = BASE + 'cat_' + f + '.webp'; } }); });
}
function addZz(root, a) {
  if (a.zz) for (var z = 0; z < 2; z++) { var zz = el('i', 'zz'); zz.style.left = pct(a.zz[0]); zz.style.top = pct(a.zz[1]); zz.style.animationDelay = (z * 2.2) + 's'; zz.innerHTML = '<svg viewBox="0 0 20 20"><path d="M4 4h11L4 16h12" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"/></svg>'; root.appendChild(zz); }
}
function bounce(n) { anim(n, [{ transform: 'scale(1)' }, { transform: 'scale(1.12,.9)' }, { transform: 'scale(.95,1.08)' }, { transform: 'scale(1)' }], { duration: 420, easing: 'ease-out' }); }

/* the props of the hobbies it knows; a new one glows until it has been tapped.
   A hobby done at something painted in the room (the stove) is a spot there instead. */
function renderProps() {
  var c = Sh.cat(), night = L.np > 0.55, spots = K.spots || {};
  Object.keys(K.props).concat(Object.keys(spots)).forEach(function (h) {
    var want = c.hobbies.indexOf(h) >= 0 && !L.away, p = L.props[h];
    if (want && !p) {
      if (spots[h]) {
        var s = spots[h];
        p = el('button', 'prop hob spot'); p.dataset.h = h;
        p.style.left = pct(s[0] - s[2] / 2); p.style.top = pct(s[1] - s[3] / 2); p.style.width = pct(s[2]); p.style.height = pct(s[3]);
      } else { p = el('button', 'prop hob'); p.dataset.h = h; place(p, K.props[h]); p.appendChild(pic(K.props[h] + '.webp')); }
      p.setAttribute('aria-label', (Sh.cwords().hobbies || {})[h] || h);
      p.addEventListener('click', function () { if (!spots[h]) bounce(p); p.classList.remove('new'); Sh.markLearnt('prop:' + h); Sh.hobby(h); });
      if (!Sh.learnt('prop:' + h) && !spots[h]) p.classList.add('new');
      propLayer.insertBefore(p, propLayer.firstChild); L.props[h] = p;
      void p.offsetWidth; p.classList.add('in');
    } else if (!want && p) { p.remove(); delete L.props[h]; }
    if (p) p.classList.toggle('busy', !!(L.cur && L.cur.hobby === h));
  });
  Object.keys(L.props).forEach(function (k) { L.props[k].classList.toggle('lit', night); });
  yarnEl.style.display = L.away ? 'none' : '';
  bowlFood.classList.toggle('on', !!c.bowl);
  if (c.bowl) bowlFood.src = BASE + 't_' + c.bowl + '.webp';
  mailEl.classList.toggle('on', Sh.unseen().length > 0);
}

function ensureAct(a) {
  var o = L.acts[a.id];
  if (o) return o;
  if (a.patch) {
    // a corner of the room with the cat painted in it
    var corner = pair(a.a, 'cat_' + a.a, 'cat_' + a.n, 'act patch fx');
    if (a.steam) for (var k = 0; k < 3; k++) { var st = el('i', 'steam'); st.style.left = pct(a.steam[0]); st.style.top = pct(a.steam[1]); st.style.animationDelay = (k * 1.1) + 's'; corner.appendChild(st); }
    addZz(corner, a);
    layer.insertBefore(corner, walker);
    o = L.acts[a.id] = { a: a, root: corner, inner: corner, fb: null };
    return o;
  }
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
  addZz(root, a);
  layer.insertBefore(root, walker);
  o = L.acts[a.id] = { a: a, root: root, inner: inner, fb: fb };
  return o;
}

/* ---- what the cat does on its own: what this part of its day is for, by habit.
   It stays at its place for the hour; a thing on the rug is never done twice running ---- */
function pick() {
  var p = slotOf(hourNow()), c = Sh.cat();
  var list = K.acts.filter(function (a) { return !a.react && a.when[p] && (!a.hobby || knows(a.hobby)); });
  var fresh = list.filter(function (a) { return a.patch || L.last.indexOf(a.id) < 0; });
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
function headOf(a) { if (a.head) return a.head; var m = M[a.a], P = stagePx(); return [a.foot[0], a.foot[1] - Math.min(m[3], a.bw * 0.95 * P.w / P.h)]; }
function fits(a) { return !!a.when[slotOf(hourNow())]; }

function showAct(a) {
  var o = ensureAct(a);
  L.cur = a; remember(a);
  follow(headOf(a)[0]);
  o.root.classList.add('on');
  o.inner.classList.remove('b');
  // the cat's own hotspot follows where it sits
  if (a.patch) Sh.hotCat(a.head[0], a.head[1] + 0.045, 0.17, 0.13);
  else { var P = stagePx(), bh = a.bw * 0.95 * P.w / P.h; Sh.hotCat(a.foot[0], a.foot[1] - bh / 2, Math.max(a.bw * 1.15, 0.2), Math.max(bh, 0.1)); }
  scheduleGlance();
  if (a.hobby) { Snd.hobby(a.hobby); renderProps(); }
}
function hideAct(a) { var o = L.acts[a.id]; if (o) o.root.classList.remove('on'); clearTimeout(L.timers.glance); if (a.hobby) setTimeout(renderProps, 0); }

function showB(ms) {
  var a = L.cur; if (!a) return;
  var o = L.acts[a.id]; if (!o || !o.fb) return;
  o.inner.classList.add('b');
  clearTimeout(o.bt);
  o.bt = setTimeout(function () { o.inner.classList.remove('b'); }, ms);
}
function scheduleGlance() {
  clearTimeout(L.timers.glance);
  var a = L.cur; if (!a || a.patch) return;   // at its places it has its back to you
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
  }, (L.cur && L.cur.patch ? rnd(70, 150) : rnd(30, 60)) * 1000);
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
  return { x: K.rug[0], y: K.rug[1], bw: K.bw };
}

async function changeAct(next) {
  var cur = L.cur;
  if (cur === next || L.away) return;   // a change queued before it went out is dropped
  L.busy = true;
  try {
    var from = whereNow();
    placeWalker(from.x, from.y, from.bw, 'sit');
    walker.classList.add('on'); hideThink();
    if (cur) { hideAct(cur); L.cur = null; Sh.hotCat(null); }
    L.attending = null;
    await wait(220);
    if (Math.hypot(from.x - next.foot[0], from.y - next.foot[1]) > 0.01) {
      follow(next.foot[0]);
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
    if (L.cur === a) return queue(function () { return changeAct(back && fits(back) ? back : pick()); });
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
    if (L.away) return;
    L.busy = true;
    try {
      var from = whereNow();
      placeWalker(from.x, from.y, from.bw, 'sit'); walker.classList.add('on');
      if (L.cur) { hideAct(L.cur); L.cur = null; }
      await wait(200);
      var h = hopsFor(from.x, from.y, spot[0], spot[1]);
      follow(spot[0]);
      await hopTo(from.x, from.y, from.bw, spot[0], spot[1], K.bw, h.ms, h.hops);
      L.attending = { x: spot[0], y: spot[1], bw: K.bw };
      Sh.hotCat(spot[0], spot[1] - 0.04, 0.2, 0.09);
      showThink(icon, spot);
    } finally { L.busy = false; }
  });
}
function showThink(icon, spot) {
  think.innerHTML = '';
  if (!icon) return;
  var P = stagePx();
  icon.classList.add('icon');   // whatever it thinks of fits inside the bubble
  think.appendChild(icon);
  think.style.left = pct(spot[0]); think.style.top = pct(spot[1] - K.bw * 0.95 * P.w / P.h - 0.01);
  think.classList.remove('on'); void think.offsetWidth; think.classList.add('on');
}
function hideThink() { think.classList.remove('on'); }
/* the cat stops attending and goes back to doing something */
function release() {
  hideThink();
  if (!L.attending || L.away || Date.now() < L.holdUntil) return;   // already off to do what was asked
  queue(function () { return changeAct(pick()); });
}

/* ---- the greeting: it is busy with its day and notices you after a while;
   how long you were away decides how soon and how glad ---- */
function greet() {
  if (L.away || L.greeted) return;
  L.greeted = true;
  if (L.cur && L.cur.still) return;   // asleep: it does not stir until you touch it
  // how long you were away is known once the visit is counted, a moment after the page opens
  setTimeout(function () {
    var c = Sh.cat(), gap = c.gap || 0, level = c.level || 1;
    var big = gap > 20 * 3600e3 || c.visits <= 1, warm = gap > 3600e3 || level >= 3;
    if (!big && !warm) {   // you were only just here: it carries on, and sometimes looks round
      if (L.cur && !L.cur.patch) setTimeout(function () { showB(1600); Snd.meow(0.6); }, rnd(2.2, 5.2) * 1000);
      return;
    }
    var notice = function () {
      if (L.away || L.busy) return;
      // the diary or a card is open: it notices you once you are back in the room
      if (!Sh.onHouse()) { seen().then(function () { setTimeout(notice, rnd(1.2, 2) * 1000); }); return; }
      react('greet', big ? 3600 : 2200).then(function () { hearts(big ? 3 : 1); });
      if (big) setTimeout(function () { hearts(2); }, 1200);
      // it remembers what you last did together, and asks for it again
      var lastFeed = (c.memory || []).filter(function (m) { return m.what === 'feed'; }).pop();
      if (!big && lastFeed && (c.today.feed || 0) < 2 && (Sh.yard().pantry[lastFeed.arg] || 0) > 0) setTimeout(function () { wantFood(lastFeed.arg); }, 4600);
    };
    setTimeout(notice, (big ? rnd(1, 1.8) : rnd(3.7, 6.7)) * 1000);
  }, 800);
}
function wantFood(food) {
  react('want', 3800);
  var i = new Image(); i.src = BASE + 't_' + food + '.webp'; i.alt = ''; i.className = 'icon';
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
/* the empty peg shows while the satchel is off it */
function bagOff(off) { pegEl.classList.toggle('on', !!off); }

/* it takes its satchel off the peg, goes to the door and out */
async function doLeave() {
  L.busy = true;
  try {
    clearTimeout(L.timers.glance); hideThink();
    var from = whereNow(), door = K.door, by = [door[0] + 0.1, door[1] + 0.07];   // just in front of the door
    placeWalker(from.x, from.y, from.bw, 'sit'); walker.classList.add('on');
    if (L.cur) { hideAct(L.cur); L.cur = null; Sh.hotCat(null); }
    L.attending = null;
    await wait(300);
    var h = hopsFor(from.x, from.y, by[0], by[1]);
    follow(door[0]);
    await hopTo(from.x, from.y, from.bw, by[0], by[1], K.bw * 0.85, h.ms, h.hops);
    // the satchel comes off its peg and onto the cat
    bagOff(true);
    await flyBag(hookSpot(), [by[0], by[1] - 0.05], 760, 0.8);
    walker.querySelector('.sit').classList.remove('on'); walker.querySelector('.ready').classList.add('on');
    Snd.pop();
    anim(walker, [{ transform: 'translate(-50%,-100%) scale(1.08,.9)' }, { transform: 'translate(-50%,-100%) scale(.96,1.06)' }, { transform: 'translate(-50%,-100%)' }], { duration: 420, easing: 'ease-out' });
    await wait(700);
    await hopTo(by[0], by[1], K.bw * 0.85, door[0], door[1], K.bw * 0.7, 900, 2, 'out');
    puff(door[0], door[1] - 0.03);
    walker.classList.remove('on'); walker.querySelector('.ready').classList.remove('on');
    walker.querySelector('.sit').classList.add('on');
    L.away = true;
    renderProps();
    Sh.renderAway();   // the note it left
  } finally { L.busy = false; }
}

/* it comes in by the door, hangs the satchel back on its peg and goes to its place for the hour */
async function doReturn() {
  L.busy = true; L.returning = true;
  try {
    var door = K.door, by = [door[0] + 0.1, door[1] + 0.07];
    walker.querySelector('.sit').classList.remove('on'); walker.querySelector('.ready').classList.add('on');
    placeWalker(door[0], door[1], K.bw * 0.7, 'ready'); walker.classList.add('on');
    follow(door[0]);
    puff(door[0], door[1] - 0.03);
    await hopTo(door[0], door[1], K.bw * 0.7, by[0], by[1], K.bw * 0.85, 1000, 2, 'in');
    await wait(300);
    await flyBag([by[0], by[1] - 0.05], hookSpot(), 760, 1);
    bagOff(false);
    Snd.pop();
    walker.querySelector('.ready').classList.remove('on'); walker.querySelector('.sit').classList.add('on');
    L.returning = false; L.away = false; Sh.renderHouse(true);        // the souvenir finds its place in the room, the props come back
    renderProps();
    await wait(900);
    var next = Sh.cat().bowl ? ACT.eat : pick(), h = hopsFor(by[0], by[1], next.foot[0], next.foot[1]);
    follow(next.foot[0]);
    await hopTo(by[0], by[1], K.bw * 0.85, next.foot[0], next.foot[1], next.bw, h.ms, h.hops);
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
  if (a.patch) {
    // at its place it has its back to you: a touch makes it turn round and come over, unless it is asleep
    var hp = headOf(a);
    heart(hp[0] + (Math.random() - 0.5) * 0.04, hp[1]);
    Snd.purr();
    if (!a.still && !L.busy && Date.now() > L.pokeUntil) { L.pokeUntil = Date.now() + 8000; react('greet', 2400).then(function () { hearts(2); }); }
    return;
  }
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
    var bird = el('div', 'bird'); bird.style.width = pct(0.055);
    var fly = pic('bird_fly.webp', 'on'), perch = pic('bird_perch.webp');
    bird.appendChild(fly); bird.appendChild(perch);
    var spot = K.perch, from = [1.06, 0.1];
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
    var gx = rnd(0.45, 0.62), gy = rnd(0.64, 0.7);
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
  if ((t.gift || 0) < 2) { Sh.act('gift', kind); y.pantry[kind] = (y.pantry[kind] || 0) + 1; t.gift = (t.gift || 0) + 1; }
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
  beam.style.opacity = Math.max(0, 1 - np * 1.6) * (1 - warmth(h) * 1.5);
  tint.style.opacity = warmth(h) * 1.2;
  walker.classList.toggle('lit', night);
  Snd.setNight(night);
  renderProps();
  // the cat keeps its day: when the hour turns, it goes on to what the next part of the day is for
  if (L.cur && !L.away && !L.busy && !L.cur.react && !fits(L.cur) && Date.now() > L.holdUntil) queue(function () { return changeAct(pick()); });
  return before;
}

/* ---- host side ---- */
function ensureOn() {
  build(); L.on = true; layer.style.display = '';
  window.addEventListener('resize', function () { stage.style.setProperty('--pan', '0px'); if (L.cur) follow(headOf(L.cur)[0]); });
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
    bagOff(L.away);
    if (L.away) Sh.hotCat(null); else showAct(firstPick());
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
function holding() { return L.returning || (L.on && L.away && !Sh.away()); }   // home in the state, not yet through the door
function catPoint() { var a = L.cur; if (a) return headOf(a); var w = whereNow(); return [w.x, w.y - 0.08]; }
/* a new level: the home has grown (a picture on the wall); the cat comes over glad */
function levelUp() {
  Snd.chime();
  if (!L.away) setTimeout(function () { react('greet', 2200).then(function () { hearts(3); }); }, 700);
}

window.CatLife = { sync: sync, poke: poke, pause: pause, holding: holding, doAct: doAct, react: react, eat: eat, nap: nap, attend: attend, release: release, greet: greet,
  catPoint: catPoint, levelUp: levelUp, hearts: hearts,
  bowlFill: function (k) { bowlFood.src = BASE + 't_' + k + '.webp'; bowlFood.classList.add('on'); bounce(bowlEl); },
  bowlWiggle: function () { bounce(bowlEl); },
  bagBounce: function () { var hs = hookSpot(); puff(hs[0], hs[1]); },
  prop: function (h) { return L.props[h]; },
  propSpot: function (h) {
    var sp = K.spots && K.spots[h]; if (sp) return [sp[0] - sp[2] * 0.6, Math.min(0.88, sp[1] + sp[3] / 2 + 0.03)];
    var m = M[K.props[h]]; return m ? [m[0] + m[2] / 2, Math.min(0.88, m[1] + m[3] + 0.02)] : null;
  },
  collect: function () { if (L.gift) L.gift.click(); }, visit: function () { if (!L.visiting) visit(); },
  get away() { return L.away; }, get cur() { return L.cur && L.cur.id; }, get np() { return L.np; }, get busy() { return L.busy; }, get held() { return Date.now() < L.holdUntil; }, get attending() { return !!L.attending; },
  act: function (id) { doAct(id, 15000); } };
})();
