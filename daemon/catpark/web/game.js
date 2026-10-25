(function () {
'use strict';
/* The cat's house, album, shelf and yard. One page for every client: the phone
   app loads it from its cache in a WebView and the web UI frames it. It holds no
   credentials and never touches the network: all state arrives from, and every
   action goes to, the host that embeds it (see the bridge below).
   The room's life is life.js, the first-visit guidance is guide.js, and a
   postcard's picture and line are card.js. */

var $ = function (id) { return document.getElementById(id); };
var C = window.CAT_CATALOG, K = window.CAT_KIT;
var LANG = /^zh/i.test(navigator.language || '') ? 'zh' : 'en';
var SV_ORDER = ['palette', 'bottle', 'book', 'wrench', 'lantern', 'pot', 'telescope', 'bell', 'teapot', 'stamp', 'compass', 'pinecone'];
var SLOTS = [[30, 63], [48, 69], [66, 62], [40, 75], [60, 76]];   // yard treat spots on the bed of soil, % of the picture
var SHELF_X = [16.5, 25, 33.5, 42];
var PANTRY_CAP = 9;
/* how many times a day each kind of care drops a star (the daemon keeps the same caps) */
var CAPS = { pet: 3, feed: 2, play: 2, hobby: 3, read: 4, gift: 2 };
var TAB = false;              // hosted as the App's Cat tab: its header already leads home

var S = null;                 // the latest View from the daemon
var screen = 'house';
var known = null;             // postcard / souvenir ids already shown
var arriving = false;
var listeners = {};

function el(tag, cls, html) { var e = document.createElement(tag); if (cls) e.className = cls; if (html != null) e.innerHTML = html; return e; }
function img(src, cls) { var i = el('img', cls); i.src = src; i.alt = ''; i.draggable = false; return i; }
function fmt(ts) { var d = new Date(ts); return (d.getMonth() + 1) + '/' + ('0' + d.getDate()).slice(-2); }
function isNight() { var h = new Date().getHours(); return h >= 20 || h < 6; }
function emit(name, data) { (listeners[name] || []).forEach(function (fn) { try { fn(data); } catch (e) { /* one listener never stops the rest */ } }); }
function food(k) { return 'a/t_' + k + '.webp'; }
function bump(n) { if (!n) return; n.classList.remove('bump'); void n.offsetWidth; n.classList.add('bump'); }

/* ---- bridge: the host owns the connection ---- */
function send(msg) {
  msg.cat = 1;
  try {
    if (window.ReactNativeWebView) window.ReactNativeWebView.postMessage(JSON.stringify(msg));
    else if (window.parent !== window) window.parent.postMessage(msg, location.origin);
  } catch (e) { /* no host: the page stays as it is */ }
}
function receive(msg) {
  if (!msg || msg.cat !== 1) return;
  if (msg.type === 'state' && msg.state) {
    if (msg.tab && !TAB) { TAB = true; renderBack(); }
    setState(msg.state, msg.locale);
  } else if (msg.type === 'focus') {
    // the host's tab is on or off screen: the room sleeps with it, and so do its sounds
    if (window.CatLife) window.CatLife.pause(!msg.on);
    if (window.CatSound) window.CatSound.pause(!msg.on);
    if (msg.on && S) act('visit', '');
  }
}
window.__catHost = receive;
window.addEventListener('message', function (e) {
  if (e.source !== window.parent || e.origin !== location.origin) return;
  receive(e.data);
});

/* one thing the person did: answered at once here, remembered by the daemon */
function act(name, arg) { send({ type: 'act', act: name, arg: arg == null ? '' : String(arg) }); }

/* ---- state ---- */
function cat() { return (S && S.cat) || { level: 1, stars: 0, need: 6, hobbies: [], pending: [], today: {}, learned: {}, memory: [], gap: 0 }; }
function postcards() { return (S && S.postcards) || []; }
function souvenirs() { return (S && S.souvenirs) || []; }
function yard() { return (S && S.yard) || { treats: [], pantry: {}, bag: '' }; }
function away() { return !!(S && S.presence && S.presence.away); }
/* what the person has already met: kept by the daemon, so it follows the server, not the phone */
function learnt(k) { var l = cat().learned || {}; return !!l[k]; }
function markLearnt(k) { var c = cat(); c.learned = c.learned || {}; if (c.learned[k]) return; c.learned[k] = true; act('learn', k); }
function unseen() { return postcards().filter(function (c) { return !c.seen; }).sort(function (a, b) { return a.at - b.at; }); }
function overlayOpen() { return !!document.querySelector('.ov.on'); }
function earns(what) { var t = cat().today || {}; return (t[what] || 0) < (CAPS[what] || 0); }
function count(what) { var c = cat(); c.today = c.today || {}; c.today[what] = (c.today[what] || 0) + 1; }

function words() { return window.CAT_UI[LANG] || window.CAT_UI.en; }
function cwords() { return C.words[LANG] || C.words.en; }
function applyWords() {
  var w = words();
  Object.keys(w).forEach(function (id) { var n = $(id); if (n && id !== 'home') n.setAttribute('aria-label', w[id]); });
  document.documentElement.lang = LANG;
}
applyWords();

function setState(next, locale) {
  if (locale) { LANG = /^zh/i.test(locale) ? 'zh' : 'en'; applyWords(); }
  var first = known === null, prev = S;
  var fresh = [], freshSv = [];
  if (!first) {
    (next.postcards || []).forEach(function (c) { if (!known.p[c.id]) fresh.push(c); });
    (next.souvenirs || []).forEach(function (v) { if (!known.s[v.id]) freshSv.push(v); });
  }
  S = next;
  known = known || { p: {}, s: {} };
  postcards().forEach(function (c) { known.p[c.id] = 1; });
  souvenirs().forEach(function (v) { known.s[v.id] = 1; });
  render(freshSv.length > 0);
  if (prev && prev.cat && next.cat) {
    if (next.cat.level > prev.cat.level) emit('level', next.cat.level);
    var met = (next.cat.hobbies || []).filter(function (h) { return (prev.cat.hobbies || []).indexOf(h) < 0; });
    if (met.length) emit('learn', met);
  }
  emit('state', { first: first, prev: prev });
  if (first) act('visit', '');
  if (first && unseen().length) setTimeout(maybeArrive, 2600);
  else if (fresh.length) setTimeout(maybeArrive, 350);
}

/* ---- house ---- */
function render(popShelf) {
  renderHouse(popShelf);
  if (screen === 'album') renderAlbum();
  if (screen === 'yard') renderYard();
  if ($('bagsheet').classList.contains('on')) renderBagTray();
  if ($('foodsheet').classList.contains('on')) renderFoodTray();
  if ($('sheet').classList.contains('on')) renderSheet();
  renderBack();
}

function renderHouse(popShelf) {
  var p = (S && S.presence) || {};
  var night = isNight();
  $('stage').classList.toggle('night', night);
  if (window.CatLife) window.CatLife.sync(S);

  // the waiting sign: by the cat at home, on its empty hook while it is away
  var sign = $('sign'), hot = $('hot-sign');
  sign.classList.toggle('on', !!p.waiting);
  sign.classList.toggle('hook', !!p.away);
  hot.style.display = p.waiting ? '' : 'none';
  if (p.away) { hot.style.left = '11%'; hot.style.top = '25%'; } else { hot.style.left = '22.5%'; hot.style.top = '33%'; }
  $('hot-bag').style.display = p.waiting && p.away ? 'none' : '';

  var bag = yard().bag, bt = $('bagtreat');
  if (bag && !p.away) { bt.src = food(bag); bt.classList.add('on'); } else { bt.classList.remove('on'); }

  $('badge').classList.toggle('on', unseen().length > 0);
  renderShelf(popShelf);
  renderFrames();
}

var shelfShown = {};
function renderShelf(pop) {
  var box = $('shelfitems');
  var latest = souvenirs().slice().sort(function (a, b) { return b.at - a.at; }).slice(0, SHELF_X.length).reverse();
  var want = {};
  latest.forEach(function (v, i) {
    want[v.id] = 1;
    var n = shelfShown[v.id];
    if (!n) {
      if (window.CatLife && window.CatLife.holding()) return;   // the cat is still carrying it home
      n = img('a/sv_' + v.item + '.webp', 'si');
      if (pop && known) n.classList.add('pop');
      box.appendChild(n); shelfShown[v.id] = n;
    }
    n.style.left = SHELF_X[i] + '%';
  });
  Object.keys(shelfShown).forEach(function (id) { if (!want[id]) { shelfShown[id].remove(); delete shelfShown[id]; } });
}

/* the wall fills up as you grow close: one framed postcard per level from 2, the newest ones */
var framesKey = '';
function renderFrames() {
  var n = Math.min(Math.max(cat().level - 1, 0), K.frames.length);
  var list = postcards().filter(function (c) { return c.phase !== 'letter'; });
  var pick = list.slice().sort(function (a, b) { return b.at - a.at; }).slice(0, n);
  var key = pick.map(function (c) { return c.id; }).join(',') + '|' + n;
  if (key === framesKey) return;
  var before = framesKey === '' ? -1 : framesKey.split('|')[1] * 1;
  framesKey = key;
  var box = $('frames'); box.innerHTML = '';
  pick.forEach(function (c, i) {
    var f = K.frames[i], b = el('button', 'frame' + (before >= 0 && i >= before ? ' pop' : ''));
    b.style.left = (f[0] * 100) + '%'; b.style.top = (f[1] * 100) + '%'; b.style.width = (f[2] * 100) + '%';
    b.style.setProperty('--r', f[3] + 'deg');
    b.setAttribute('aria-label', words().frame);
    b.appendChild(window.CatCard.scene(c));
    b.addEventListener('click', function () { if (window.CatSound) window.CatSound.paper(); openZoomCard(c); });
    box.appendChild(b);
  });
  if (before >= 0 && n > before) emit('frame');
}

$('hot-cat').addEventListener('click', function () { petCat(); });
$('hot-mail').addEventListener('click', function () { if (unseen().length) showArrive(); else openAlbum(); });
$('hot-shelf').addEventListener('click', function () { act('learn', 'shelf'); openSheet(); });
$('hot-window').addEventListener('click', openYard);
$('hot-bag').addEventListener('click', openBag);
$('hot-bed').addEventListener('click', function () { if (window.CatLife) window.CatLife.nap(); });
$('hot-sign').addEventListener('click', function () { send({ type: 'open_brain' }); });

/* ---- care ---- */
function petCat() {
  if (away() || !window.CatLife) return;
  var star = earns('pet');
  count('pet');
  window.CatLife.poke();
  act('pet', '');
  if (star) setTimeout(function () { flyStar(window.CatLife.catPoint()); }, 250);
  emit('did', 'pet');
}
/* feed from the bowl: away, it waits in the bowl; full, the cat says no */
function feed(k) {
  var y = yard();
  if ((y.pantry[k] || 0) <= 0) return;
  off('foodsheet');
  if (away()) {
    if (cat().bowl) return;
    y.pantry[k]--; cat().bowl = k;
    act('feed', k);
    if (window.CatLife) window.CatLife.bowlFill(k);
    if (window.CatSound) window.CatSound.pop();
    return;
  }
  if ((cat().today.feed || 0) >= CAPS.feed) { if (window.CatLife) window.CatLife.react('full'); return; }
  var star = earns('feed');
  y.pantry[k]--; count('feed'); cat().learned.feed = true;
  act('feed', k);
  if (window.CatLife) window.CatLife.eat(k).then(function () { if (star) flyStar(window.CatLife.catPoint()); });
  emit('did', 'feed');
}
function play() {
  if (away() || !window.CatLife) return;
  var star = earns('play');
  count('play');
  act('play', '');
  window.CatLife.doAct('yarn', 14000).then(function () { if (star) flyStar(window.CatLife.catPoint()); });
  emit('did', 'play');
}
function hobby(id) {
  if (away() || !window.CatLife || cat().hobbies.indexOf(id) < 0) return;
  var star = earns('hobby');
  count('hobby');
  act('hobby', id);
  if (id === 'letters' && !(cat().today['yield:letters'] > 0)) cat().today['yield:letters'] = 1;
  window.CatLife.doAct(id, 18000).then(function () { if (star) flyStar(window.CatLife.catPoint()); });
  emit('did', 'hobby');
}

/* a star flies from where something happened into the jar */
function flyStar(from) {
  var jar = window.CatLife && window.CatLife.jarPoint();
  if (!from || !jar) return;
  var stage = $('stage'), r = stage.getBoundingClientRect();
  var s = el('i', 'flystar', '<svg viewBox="0 0 24 24"><path d="M12 1.5l3 6.9 7.5.7-5.7 5 1.7 7.4L12 17.6l-6.5 3.9 1.7-7.4-5.7-5 7.5-.7z" fill="#ffd25e" stroke="#c98a1c" stroke-width="1.2" stroke-linejoin="round"/></svg>');
  s.style.left = (from[0] * 100) + '%'; s.style.top = (from[1] * 100) + '%';
  stage.appendChild(s);
  var dx = (jar[0] - from[0]) * r.width, dy = (jar[1] - from[1]) * r.height, kf = [];
  for (var i = 0; i <= 16; i++) { var u = i / 16, e = u * u * (3 - 2 * u); kf.push({ offset: u, transform: 'translate(-50%,-50%) translate(' + (dx * e).toFixed(1) + 'px,' + (dy * e - Math.sin(Math.PI * u) * r.width * 0.16).toFixed(1) + 'px) rotate(' + (u * 300).toFixed(0) + 'deg) scale(' + (1 - u * 0.4).toFixed(2) + ')' }); }
  var an; try { an = s.animate(kf, { duration: 900, easing: 'linear', fill: 'forwards' }); } catch (e) { an = null; }
  var done = function () { s.remove(); if (window.CatLife) window.CatLife.jarClink(); if (window.CatSound) window.CatSound.clink(); emit('star'); };
  if (an) an.finished.then(done, done); else setTimeout(done, 900);
}

/* ---- overlay plumbing ---- */
function on(id) { var o = $(id); o.style.display = 'block'; void o.offsetWidth; o.classList.add('on'); renderBack(); emit('overlay', id); }
function off(id, ms) { var o = $(id); if (!o.classList.contains('on')) return; o.classList.remove('on'); setTimeout(function () { if (!o.classList.contains('on')) o.style.display = ''; renderBack(); emit('overlay', null); }, ms || 380); renderBack(); }

/* ---- postcards ---- */
function postcardEl(card, cls, live) {
  var home = card.phase === 'welcome' || card.phase === 'letter';
  var p = el('div', 'postcard ' + (cls || '') + (home ? ' letter' : ''));
  p.appendChild(window.CatCard.scene(card, live));
  var strip = el('div', 'strip'), line = el('span', 'line');
  line.textContent = window.CatCard.line(card, LANG);
  strip.appendChild(line);
  if (card.treat && !home) strip.appendChild(img(food(card.treat), 'tr'));
  p.appendChild(strip);
  if (!home) { p.appendChild(el('i', 'stamp')); p.appendChild(el('span', 'pm', fmt(card.at))); }
  return p;
}

function maybeArrive() { if (screen === 'house' && !overlayOpen() && unseen().length && !(window.CatGuide && window.CatGuide.holdMail())) showArrive(); }

function showArrive() {
  var list = unseen();
  if (!list.length || arriving) return;
  arriving = true;
  var card = list[0];
  var h = $('arriveholder'); h.innerHTML = '';
  var p = postcardEl(card, 'wipe', true); p.dataset.id = card.id; h.appendChild(p);
  on('arrive');
  if (window.CatSound) window.CatSound.paper();
  requestAnimationFrame(function () { requestAnimationFrame(function () { p.classList.add('in'); }); });
}

$('arrivetap').addEventListener('click', function () {
  var p = document.querySelector('#arriveholder .postcard');
  if (!p || p.classList.contains('out')) return;
  var pr = p.getBoundingClientRect(), mr = $('hot-mail').getBoundingClientRect();
  var dx = (mr.left + mr.width / 2) - (pr.left + pr.width / 2), dy = (mr.top + mr.height / 2) - (pr.top + pr.height / 2);
  p.style.transform = 'translate(' + dx + 'px,' + dy + 'px) scale(.12) rotate(14deg)';
  p.classList.add('out');
  var star = earns('read');
  markSeen('postcard', p.dataset.id);
  if (star) { count('read'); setTimeout(function () { var s = $('stage').getBoundingClientRect(); flyStar([(mr.left + mr.width / 2 - s.left) / s.width, (mr.top + mr.height / 2 - s.top) / s.height]); }, 650); }
  off('arrive', 700);
  setTimeout(function () {
    arriving = false;
    $('badge').classList.toggle('on', unseen().length > 0);
    bump($('hot-mail'));
    emit('did', 'mail');
    if (unseen().length) setTimeout(maybeArrive, 900);
  }, 620);
});

function markSeen(what, id) {
  var list = what === 'postcard' ? postcards() : souvenirs();
  list.forEach(function (x) { if (x.id === id) x.seen = true; });
  if (what === 'postcard') cat().learned.mail = true;
  act('seen', what + ':' + id);
}

/* ---- album ---- */
function renderAlbum() {
  var g = $('grid'); g.innerHTML = '';
  var list = postcards().slice().sort(function (a, b) { return b.at - a.at; });
  if (!list.length) { g.className = 'empty'; for (var i = 0; i < 4; i++) g.appendChild(el('i')); return; }
  g.className = 'grid';
  list.forEach(function (c) {
    var b = el('button', 'mini' + (c.seen ? '' : ' fresh'));
    b.appendChild(window.CatCard.scene(c));
    b.appendChild(el('span', 'd', fmt(c.at))); b.appendChild(el('i', 'stamp'));
    b.addEventListener('click', function () { if (!c.seen) markSeen('postcard', c.id); openZoomCard(c); });
    g.appendChild(b);
  });
}
function openAlbum() { screen = 'album'; act('learn', 'album'); renderAlbum(); $('albumscroll').scrollTop = 0; $('album').classList.add('on'); renderBack(); if (window.CatSound) window.CatSound.paper(); emit('screen', 'album'); }
function closeAlbum() { screen = 'house'; $('album').classList.remove('on'); renderBack(); renderHouse(); emit('screen', 'house'); }

function openZoomCard(card) { var h = $('zoomholder'); h.innerHTML = ''; h.appendChild(postcardEl(card, '', true)); on('zoom'); }
function closeZoom() { off('zoom'); }
$('zoomtap').addEventListener('click', closeZoom);

/* ---- shelf ---- */
function renderSheet() {
  var have = {};
  souvenirs().forEach(function (v) { if (!have[v.item] || have[v.item].at < v.at) have[v.item] = v; });
  var r = $('sheetrow'); r.innerHTML = '';
  SV_ORDER.forEach(function (k) {
    var v = have[k], s = el('button', 'slot' + (v ? '' : ' locked') + (v && !v.seen ? ' fresh' : ''));
    s.appendChild(img('a/sv_' + k + '.webp'));
    s.addEventListener('click', function () { if (v) { markSeen('souvenir', v.id); openZoomSouvenir(v); } else bump(s); });
    r.appendChild(s);
  });
}
function openSheet() { renderSheet(); on('sheet'); if (window.CatSound) window.CatSound.wood(); }
$('sheettap').addEventListener('click', function () { off('sheet'); });

function openZoomSouvenir(v) {
  var h = $('zoomholder'); h.innerHTML = '';
  h.appendChild(img('a/sv_' + v.item + '.webp', 'big'));
  var tag = el('div', 'tag');
  var line = el('span', 'line'); line.textContent = (v.title || '') + (v.title ? '  ' : '') + fmt(v.at);
  tag.appendChild(line);
  if (v.hobby && K.props[v.hobby]) tag.appendChild(img('a/' + K.props[v.hobby] + '.webp', 'hob'));
  h.appendChild(tag);
  on('zoom');
}

/* ---- the food tray (from the bowl) and the bag ---- */
function trayItem(k, n, fn) {
  var b = el('button', 'it'); b.appendChild(img(food(k))); b.appendChild(el('b', '', String(n)));
  if (k === cat().favorite) b.appendChild(el('i', 'fav'));
  b.addEventListener('click', function () { fn(k, b); });
  return b;
}
var PLUS = '<svg viewBox="0 0 34 34" fill="none" stroke="#4a3b2e" stroke-width="2.2" stroke-linecap="round"><circle cx="17" cy="17" r="12" stroke-dasharray="4 4"/><path d="M17 11v12M11 17h12"/></svg>';
function renderFoodTray() {
  var box = $('foodtray'), y = yard(); box.innerHTML = '';
  var any = false;
  C.foods.forEach(function (k) { var n = y.pantry[k] || 0; if (n > 0) { any = true; box.appendChild(trayItem(k, n, feed)); } });
  if (!any) {
    // nothing in the pantry: the window is where food grows
    var w = el('button', 'it empty glow', PLUS);
    w.addEventListener('click', function () { off('foodsheet'); setTimeout(openYard, 300); });
    box.appendChild(w);
  }
}
function openFood() {
  if (!away() && (cat().today.feed || 0) >= CAPS.feed) { if (window.CatLife) window.CatLife.react('full'); return; }
  if (away() && cat().bowl) { if (window.CatLife) window.CatLife.bowlWiggle(); return; }
  renderFoodTray(); on('foodsheet');
}
var SATCHEL = '<svg viewBox="0 0 34 34" fill="none" stroke="#4a3b2e" stroke-width="2.2" stroke-linejoin="round" stroke-linecap="round"><path d="M9 13c0-7 16-7 16 0"/><path d="M5 15h24l-2 15H7z" fill="#e7cfa4"/><path d="M5 15l12 6 12-6" stroke="#c8442f"/></svg>';
function renderBagTray() {
  var box = $('bagtray'), y = yard(); box.innerHTML = '';
  var bag = el('button', 'it bag' + (y.bag ? ' full' : ''), SATCHEL);
  if (y.bag) bag.appendChild(img(food(y.bag)));
  bag.addEventListener('click', function () { if (yard().bag) pack(''); });
  var any = false;
  C.foods.forEach(function (k) { var n = y.pantry[k] || 0; if (n > 0) { any = true; box.appendChild(trayItem(k, n, function (kk) { pack(kk); })); } });
  if (!any && !y.bag) {
    var w = el('button', 'it empty glow', PLUS);
    w.addEventListener('click', function () { off('bagsheet'); setTimeout(openYard, 300); });
    box.appendChild(w);
  }
  box.appendChild(bag);
}
function pack(kind) {
  var y = yard();
  if (kind === y.bag) return;
  if (kind && (y.pantry[kind] || 0) <= 0) return;
  if (y.bag) y.pantry[y.bag] = (y.pantry[y.bag] || 0) + 1;
  y.bag = '';
  if (kind) { y.pantry[kind]--; y.bag = kind; cat().learned.pack = true; }
  act('pack', kind);
  if (window.CatSound) window.CatSound.pop();
  renderBagTray(); renderHouse();
  if (window.CatLife) window.CatLife.bagBounce();
  if (kind) { emit('did', 'pack'); setTimeout(function () { off('bagsheet'); }, 650); }
}
function openBag() { act('learn', 'bag'); renderBagTray(); on('bagsheet'); if (window.CatSound) window.CatSound.cloth(); }
$('bagtap').addEventListener('click', function () { off('bagsheet'); });
$('foodtap').addEventListener('click', function () { off('foodsheet'); });

/* ---- the jar: stars toward the next level, and what that level brings ---- */
var UNLOCKS = { 2: 'frame', 3: 'run', 4: 'friend', 5: 'gift', 6: 'frame', 7: 'more', 8: 'rare', 9: 'frame', 10: 'rare' };
function openJar() {
  act('learn', 'jar');
  var c = cat(), h = $('jarholder'); h.innerHTML = '';
  var card = el('div', 'jarcard');
  var lv = el('div', 'lv'); lv.appendChild(img('a/p_jar.webp')); lv.appendChild(el('b', '', String(c.level))); card.appendChild(lv);
  var row = el('div', 'stars');
  for (var i = 0; i < (c.need || 0); i++) row.appendChild(el('i', i < c.stars ? 'on' : ''));
  card.appendChild(row);
  if (c.need) {
    var next = el('div', 'next');
    next.appendChild(el('span', 'arrow', '<svg viewBox="0 0 24 24"><path d="M4 12h14M13 6l6 6-6 6" fill="none" stroke="#8a7867" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"/></svg>'));
    next.appendChild(unlockIcon(UNLOCKS[c.level + 1]));
    (c.pending || []).forEach(function (p) { if (K.props[p]) next.appendChild(img('a/' + K.props[p] + '.webp', 'ghost')); });
    card.appendChild(next);
  }
  h.appendChild(card);
  on('jarsheet');
  if (window.CatSound) window.CatSound.clink();
}
function unlockIcon(u) {
  switch (u) {
    case 'frame': return el('i', 'u-frame');
    case 'friend': return img('a/f_frog.webp', 'ghost');
    case 'gift': return img('a/gift_acorn.webp', 'ghost');
    case 'run': return img('a/cat_greet_a.webp', 'ghost');
    case 'more': return img('a/t_berry.webp', 'ghost');
    default: return img('a/pl_' + (C.rare[0] || 'trail') + '.jpg', 'ghost card');
  }
}
$('jartap').addEventListener('click', function () { off('jarsheet'); });

/* ---- yard ---- */
function renderYard() {
  var night = isNight();
  $('y-day').classList.toggle('on', !night); $('y-night').classList.toggle('on', night);
  var box = $('treats'); box.innerHTML = '';
  yard().treats.forEach(function (t) {
    if (t.taken) return;
    var spot = SLOTS[t.slot % SLOTS.length];
    var b = el('button', 'tt'); b.style.left = spot[0] + '%'; b.style.top = spot[1] + '%';
    b.appendChild(img(food(t.kind)));
    b.addEventListener('click', function () { collect(t, b); });
    box.appendChild(b);
  });
  renderYardTray();
  renderYardCat();
}
function renderYardTray() {
  var box = $('yardtray'), y = yard(); box.innerHTML = '';
  C.foods.forEach(function (k) { var n = y.pantry[k] || 0; if (n > 0) { var b = el('div', 'it'); b.dataset.k = k; b.appendChild(img(food(k))); b.appendChild(el('b', '', String(n))); box.appendChild(b); } });
}
function collect(t, node) {
  var y = yard();
  if ((y.pantry[t.kind] || 0) >= PANTRY_CAP) { bump(node); return; }
  t.taken = true; y.pantry[t.kind] = (y.pantry[t.kind] || 0) + 1; cat().learned.yard = true;
  node.classList.add('take');
  if (window.CatSound) window.CatSound.pop();
  act('collect', t.slot);
  setTimeout(function () { renderYardTray(); bump(document.querySelector('#yardtray .it[data-k="' + t.kind + '"]')); emit('did', 'collect'); }, 300);
}
/* the cat in the yard: fishing by day once it has learnt to, camping at night.
   A hobby first met there glows until it is tapped, like a new prop indoors. */
var yardCatEl = null;
function yardWhich() {
  var c = cat();
  if (away()) return '';
  if (isNight()) return c.hobbies.indexOf('camp') >= 0 ? 'camp' : '';
  return c.hobbies.indexOf('fish') >= 0 ? 'fish' : '';
}
function freshProp(h) { return !learnt('prop:' + h); }
function seeProp(h) { markLearnt('prop:' + h); }
function yardNew() { var w = yardWhich(); return w && K.yard && K.yard[w] && freshProp(w) ? w : ''; }
function renderYardCat() {
  var box = $('yardlife'), which = yardWhich();
  var y = K.yard && K.yard[which];
  if (!y) { box.innerHTML = ''; yardCatEl = null; return; }
  if (yardCatEl && yardCatEl.dataset.w === which) return;
  box.innerHTML = '';
  var d = el('button', 'ycat'); d.dataset.w = which;
  d.setAttribute('aria-label', (cwords().hobbies || {})[which] || which);
  d.style.left = (y.box[0] * 100) + '%'; d.style.top = (y.box[1] * 100) + '%'; d.style.width = (y.box[2] * 100) + '%';
  d.appendChild(img('a/' + y.a + '.webp', 'fa'));
  if (y.b) { var b = img('a/' + y.b + '.webp', 'fb'); var bb = y.bbox || y.box; b.style.left = ((bb[0] - y.box[0]) / y.box[2] * 100) + '%'; b.style.top = ((bb[1] - y.box[1]) / y.box[3] * 100) + '%'; b.style.width = (bb[2] / y.box[2] * 100) + '%'; d.appendChild(b); }
  if (freshProp(which)) d.classList.add('glow');
  d.addEventListener('click', function () { yardHobby(which); });
  box.appendChild(d); yardCatEl = d;
}
/* doing the yard hobby: fishing by the pond pays a fish once a day */
function yardHobby(which) {
  var c = cat();
  if (!yardCatEl || yardCatEl.dataset.w !== which) return;
  if (yardCatEl.classList.contains('glow')) { yardCatEl.classList.remove('glow'); seeProp(which); emit('did', 'yardnew'); }
  yardCatEl.classList.add('b'); setTimeout(function () { if (yardCatEl) yardCatEl.classList.remove('b'); }, 2200);
  if (window.CatSound) window.CatSound.hobby(which);
  var star = earns('hobby');
  count('hobby'); act('hobby', which);
  if (which === 'fish' && !(c.today['yield:fish'] > 0)) {
    c.today['yield:fish'] = 1;
    var y = yard(); y.pantry.fish = Math.min(PANTRY_CAP, (y.pantry.fish || 0) + 1);
    setTimeout(function () { renderYardTray(); bump(document.querySelector('#yardtray .it[data-k="fish"]')); }, 900);
  }
  if (star && window.CatSound) setTimeout(function () { window.CatSound.clink(); }, 900);
}
$('hot-pond').addEventListener('click', function () {
  if (window.CatSound) window.CatSound.plop();
  ripple();
  if (yardCatEl && yardCatEl.dataset.w === 'fish') yardHobby('fish');
});
function ripple() {
  var r = el('i', 'ripple'); r.style.left = '18%'; r.style.top = '83.5%';
  $('yardstage').appendChild(r); setTimeout(function () { r.remove(); }, 1200);
}
$('hot-dog').addEventListener('click', function () {
  var n = $('hot-dog'); n.classList.remove('wig'); void n.offsetWidth; n.classList.add('wig');
  if (window.CatSound) window.CatSound.wood();
});
function openYard() { screen = 'yard'; act('learn', 'window'); renderYard(); $('yard').classList.add('on'); renderBack(); if (window.CatSound) window.CatSound.chirp(); emit('screen', 'yard'); }
function closeYard() { screen = 'house'; $('yard').classList.remove('on'); renderBack(); renderHouse(); emit('screen', 'house'); }

/* ---- the one back button ---- */
var CHEVRON = '<svg viewBox="0 0 26 26" fill="none" stroke="#4a3b2e" stroke-width="2.4" stroke-linejoin="round" stroke-linecap="round"><path d="M16 4L7 13l9 9"/></svg>';
var HOME = '<svg viewBox="0 0 26 26" fill="none" stroke="#4a3b2e" stroke-width="2.2" stroke-linejoin="round" stroke-linecap="round"><path d="M3 12.5L13 4l10 8.5"/><path d="M6 11v11h14V11"/><path d="M11 22v-6h4v6" stroke="#c8442f"/></svg>';
function renderBack() {
  var b = $('back');
  b.style.display = overlayOpen() || (TAB && screen === 'house') ? 'none' : '';
  b.innerHTML = screen === 'house' ? CHEVRON : HOME;
  b.setAttribute('aria-label', screen === 'house' ? words().back : words().home);
}
$('back').addEventListener('click', function () {
  if (screen === 'album') closeAlbum();
  else if (screen === 'yard') closeYard();
  else send({ type: 'back' });
});

/* ---- swipe down closes whatever is on top ---- */
var t0 = null;
document.addEventListener('touchstart', function (e) { var t = e.touches[0]; t0 = { x: t.clientX, y: t.clientY }; }, { passive: true });
document.addEventListener('touchend', function (e) {
  if (!t0) return;
  var t = e.changedTouches[0], dx = t.clientX - t0.x, dy = t.clientY - t0.y; t0 = null;
  if (dy < 80 || Math.abs(dx) > 70) return;
  var top = ['zoom', 'jarsheet', 'sheet', 'bagsheet', 'foodsheet'].filter(function (id) { return $(id).classList.contains('on'); })[0];
  if (top) { if (top === 'zoom') closeZoom(); else off(top); }
  else if (screen === 'album' && $('albumscroll').scrollTop <= 0) closeAlbum();
  else if (screen === 'yard') closeYard();
}, { passive: true });

/* ---- day turns to night while the page is open ---- */
setInterval(function () { if (S) { renderHouse(false); if (screen === 'yard') renderYard(); } }, 60000);
document.addEventListener('visibilitychange', function () { if (!document.hidden && S) { renderHouse(false); act('visit', ''); } });

window.CatShell = {
  words: words, cwords: cwords, renderHouse: renderHouse, cat: cat, yard: yard, away: away, postcards: postcards, unseen: unseen,
  lang: function () { return LANG; }, isNight: isNight, screen: function () { return screen; },
  onHouse: function () { return screen === 'house' && !overlayOpen() && !document.hidden; },
  overlayOpen: overlayOpen, arriving: function () { return arriving; },
  on: function (name, fn) { (listeners[name] = listeners[name] || []).push(fn); },
  hotCat: function (x, y, w, h) {
    var c = $('hot-cat');
    if (x == null) { c.style.display = 'none'; return; }
    c.style.display = ''; c.style.left = (x * 100) + '%'; c.style.top = (y * 100) + '%'; c.style.width = (w * 100) + '%'; c.style.height = (h * 100) + '%';
  },
  openFood: openFood, openJar: openJar, play: play, hobby: hobby, yardNew: yardNew, learnt: learnt, markLearnt: markLearnt, flyStar: flyStar, showArrive: showArrive, maybeArrive: maybeArrive, act: act
};
/* audio starts with the first touch, as the browser demands */
['touchend', 'click'].forEach(function (name) { document.addEventListener(name, function () { if (window.CatSound) window.CatSound.unlock(); }, { passive: true }); });

window.__cat = { setState: setState, get state() { return S; }, openAlbum: openAlbum, openYard: openYard, openSheet: openSheet, openBag: openBag, openJar: openJar, openFood: openFood };

renderBack();
// the host can miss the first hello (a tab mounted off screen): say it until the state comes
(function hello() { if (S) return; send({ type: 'ready' }); setTimeout(hello, 1000); })();
})();
