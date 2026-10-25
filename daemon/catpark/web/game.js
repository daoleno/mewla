(function () {
'use strict';
/* The cat's house, album, shelf and yard. One page for every client: the phone
   app loads it from its cache in a WebView and the web UI frames it. It holds no
   credentials and never touches the network: all state arrives from, and every
   action goes to, the host that embeds it (see the bridge below). */

var $ = function (id) { return document.getElementById(id); };
var LANG = /^zh/i.test(navigator.language || '') ? 'zh' : 'en';
var SV_ORDER = ['palette', 'bottle', 'book', 'wrench', 'lantern', 'pot', 'telescope', 'bell', 'teapot', 'stamp', 'compass', 'pinecone'];
var TREATS = ['fish', 'yarn', 'leaf'];
var SLOTS = [[28, 73], [50, 65], [71, 72]];   // yard treat spots, % of the picture
var SHELF_X = [16.5, 25, 33.5, 42];
var PANTRY_CAP = 9;

var STYLES = ['crayon', 'watercolor', 'gouache', 'papercut', 'pixel', 'clay'];
var STYLE = 'crayon';         // the art the house is drawn in; the daemon remembers it per user
var TAB = false;              // hosted as the App's Cat tab: its header already leads home

var S = null;                 // the latest View from the daemon
var screen = 'house';
var known = null;             // postcard / souvenir ids already shown
var arriving = false;

function el(tag, cls, html) { var e = document.createElement(tag); if (cls) e.className = cls; if (html != null) e.innerHTML = html; return e; }
function img(src, cls) { var i = el('img', cls); i.src = src; i.alt = ''; i.draggable = false; return i; }
function fmt(ts) { var d = new Date(ts); return (d.getMonth() + 1) + '/' + ('0' + d.getDate()).slice(-2); }
function isNight() { var h = new Date().getHours(); return h >= 20 || h < 6; }

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
  }
}
window.__catHost = receive;
window.addEventListener('message', function (e) {
  if (e.source !== window.parent || e.origin !== location.origin) return;
  receive(e.data);
});

/* ---- words ---- */
function lineFor(card) {
  var set = (window.CAT_LINES[LANG] || window.CAT_LINES.en);
  var kind = set[card.kind] || set.trail;
  var lines = kind[card.phase] || kind.start;
  return lines[(card.variant || 0) % lines.length];
}

/* ---- state ---- */
function postcards() { return (S && S.postcards) || []; }
function souvenirs() { return (S && S.souvenirs) || []; }
function unseen() { return postcards().filter(function (c) { return !c.seen; }).sort(function (a, b) { return a.at - b.at; }); }
function overlayOpen() { return !!document.querySelector('.ov.on'); }

function words() { return window.CAT_UI[LANG] || window.CAT_UI.en; }
function applyWords() {
  var w = words();
  Object.keys(w).forEach(function (id) { var n = $(id); if (n && id !== 'home') n.setAttribute('aria-label', w[id]); });
  document.documentElement.lang = LANG;
}
applyWords();

function setState(next, locale) {
  if (locale) { LANG = /^zh/i.test(locale) ? 'zh' : 'en'; applyWords(); renderStyleRow(); }
  if (next.style && next.style !== STYLE && STYLES.indexOf(next.style) >= 0) changeStyle(next.style, known !== null);
  var first = known === null;
  var fresh = [];
  var freshSv = [];
  if (!first) {
    (next.postcards || []).forEach(function (c) { if (!known.p[c.id]) fresh.push(c); });
    (next.souvenirs || []).forEach(function (v) { if (!known.s[v.id]) freshSv.push(v); });
  }
  S = next;
  known = known || { p: {}, s: {} };
  postcards().forEach(function (c) { known.p[c.id] = 1; });
  souvenirs().forEach(function (v) { known.s[v.id] = 1; });
  render(freshSv.length > 0);
  if (first && unseen().length) setTimeout(maybeArrive, 700);
  else if (fresh.length) setTimeout(maybeArrive, 350);
}

/* ---- house ---- */
var plates = { home: $('p-home'), away: $('p-away'), night: $('p-night'), nightaway: $('p-nightaway') };

/* ---- the house's art style ----
   Crayon is the original: an empty room plus the cat as a cut-out that breathes.
   Every other style is whole pictures with the cat painted in, so its cut-outs
   stay hidden; a style has no picture of the empty room at night, so that one
   moment is the empty day room under a dark veil. */
function baked() { return STYLE !== 'crayon'; }
function plateSrc(style, slot) {
  if (style === 'crayon') return { home: 'a/home_nocat.jpg', away: 'a/away.jpg', night: 'a/night.jpg', nightaway: 'a/night_away.jpg' }[slot];
  return 'a/s/' + style + '/' + (slot === 'nightaway' ? 'away' : slot) + '.jpg';
}
function plateKey() {
  var p = (S && S.presence) || {}, night = isNight();
  return night ? (p.away ? 'nightaway' : 'night') : (p.away ? 'away' : 'home');
}
function setStylePlates(style) {
  Object.keys(plates).forEach(function (k) { plates[k].src = plateSrc(style, k); });
  $('frameimg').src = 'a/s/' + style + '/card.jpg';
}
var styleToken = 0;
/* Swap the whole scene: the new picture fades in over the old one, then the plates take it over. */
function changeStyle(next, animate) {
  if (next === STYLE || STYLES.indexOf(next) < 0) return;
  var token = ++styleToken, key = plateKey(), url = plateSrc(next, key);
  var finish = function () {
    if (token !== styleToken) return;
    STYLE = next;
    setStylePlates(next);
    var g = document.querySelector('.ghost'); if (g) g.remove();
    renderHouse(false); renderStyleRow();
  };
  if (!animate) { finish(); return; }
  var pre = new Image();
  pre.onload = pre.onerror = function () {
    if (token !== styleToken) return;
    var ghost = img(url, 'plate ghost');
    var old = document.querySelector('.ghost'); if (old) old.remove();
    $('stage').insertBefore(ghost, $('cat-home'));
    void ghost.offsetWidth; ghost.classList.add('on');
    STYLE = next;                       // cut-outs fade out and the veil follows at once
    renderHouse(false); renderStyleRow();
    $('frameimg').src = 'a/s/' + next + '/card.jpg';
    $('frame').classList.remove('bump'); void $('frame').offsetWidth; $('frame').classList.add('bump');
    setTimeout(function () { if (token === styleToken) { setStylePlates(next); var g = document.querySelector('.ghost'); if (g) g.remove(); } }, 760);
  };
  pre.src = url;
}
function render(popShelf) {
  renderHouse(popShelf);
  if (screen === 'album') renderAlbum();
  if (screen === 'yard') renderYard();
  if ($('bagsheet').classList.contains('on')) renderTray($('bagtray'));
  if ($('sheet').classList.contains('on')) renderSheet();
  renderBack();
}

function renderHouse(popShelf) {
  var p = (S && S.presence) || {};
  var night = isNight();
  var plate = plateKey();
  Object.keys(plates).forEach(function (k) { plates[k].classList.toggle('on', k === plate); });
  $('cat-home').classList.toggle('on', !p.away && !night && !baked());
  $('cat-night').classList.toggle('on', !p.away && night && !baked());
  $('veil').classList.toggle('on', baked() && night && !!p.away);
  $('stage').classList.toggle('night', night);
  var cat = $('hot-cat');
  cat.style.display = p.away ? 'none' : '';
  if (night) { cat.style.left = '46.6%'; cat.style.top = '58.7%'; cat.style.width = '26%'; cat.style.height = '10%'; }
  else { cat.style.left = '51%'; cat.style.top = '51%'; cat.style.width = '36%'; cat.style.height = '21%'; }

  // The waiting sign: by the cat at home, on its empty hook while it is away.
  var sign = $('sign'), hot = $('hot-sign');
  sign.classList.toggle('on', !!p.waiting);
  sign.classList.toggle('hook', !!p.away);
  hot.style.display = p.waiting ? '' : 'none';
  if (p.away) { hot.style.left = '11%'; hot.style.top = '25%'; } else { hot.style.left = '22.5%'; hot.style.top = '33%'; }
  $('hot-bag').style.display = p.waiting && p.away ? 'none' : '';

  var bag = S && S.yard && S.yard.bag;
  var bt = $('bagtreat');
  if (bag && !p.away) { bt.src = 'a/t_' + bag + '.webp'; bt.classList.add('on'); } else { bt.classList.remove('on'); }

  $('badge').classList.toggle('on', unseen().length > 0);
  renderShelf(popShelf);
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
      n = img('a/sv_' + v.item + '.webp', 'si');
      if (pop && known) n.classList.add('pop');
      box.appendChild(n); shelfShown[v.id] = n;
    }
    n.style.left = SHELF_X[i] + '%';
  });
  Object.keys(shelfShown).forEach(function (id) { if (!want[id]) { shelfShown[id].remove(); delete shelfShown[id]; } });
}

$('hot-cat').addEventListener('click', function () {
  var c = baked() ? plates[plateKey()] : isNight() ? $('cat-night') : $('cat-home');
  c.classList.remove('poke'); void c.offsetWidth; c.classList.add('poke');
});
$('frame').addEventListener('click', openStyles);
$('hot-mail').addEventListener('click', function () { if (unseen().length) showArrive(); else openAlbum(); });
$('hot-shelf').addEventListener('click', openSheet);
$('hot-window').addEventListener('click', openYard);
$('hot-bag').addEventListener('click', openBag);
$('hot-sign').addEventListener('click', function () { send({ type: 'open_brain' }); });

/* ---- overlay plumbing ---- */
function on(id) { var o = $(id); o.style.display = 'block'; void o.offsetWidth; o.classList.add('on'); renderBack(); }
function off(id, ms) { var o = $(id); o.classList.remove('on'); setTimeout(function () { if (!o.classList.contains('on')) o.style.display = ''; renderBack(); }, ms || 380); renderBack(); }

/* ---- postcards ---- */
function postcardEl(card, cls) {
  var p = el('div', 'postcard ' + (cls || ''));
  p.appendChild(img('a/pc_' + card.kind + '.jpg', 'pic'));
  var strip = el('div', 'strip'), line = el('span', 'line');
  line.textContent = lineFor(card);
  strip.appendChild(line);
  if (card.treat) strip.appendChild(img('a/t_' + card.treat + '.webp', 'tr'));
  p.appendChild(strip); p.appendChild(el('i', 'stamp'));
  return p;
}

function maybeArrive() { if (screen === 'house' && !overlayOpen() && unseen().length) showArrive(); }

function showArrive() {
  var list = unseen();
  if (!list.length || arriving) return;
  arriving = true;
  var card = list[0];
  var h = $('arriveholder'); h.innerHTML = '';
  var p = postcardEl(card, 'wipe'); p.dataset.id = card.id; h.appendChild(p);
  on('arrive');
  requestAnimationFrame(function () { requestAnimationFrame(function () { p.classList.add('in'); }); });
}

$('arrivetap').addEventListener('click', function () {
  var p = document.querySelector('#arriveholder .postcard');
  if (!p || p.classList.contains('out')) return;
  var pr = p.getBoundingClientRect(), mr = $('hot-mail').getBoundingClientRect();
  var dx = (mr.left + mr.width / 2) - (pr.left + pr.width / 2), dy = (mr.top + mr.height / 2) - (pr.top + pr.height / 2);
  p.style.transform = 'translate(' + dx + 'px,' + dy + 'px) scale(.12) rotate(14deg)';
  p.classList.add('out');
  markSeen('postcard', p.dataset.id);
  off('arrive', 700);
  setTimeout(function () {
    arriving = false;
    $('badge').classList.toggle('on', unseen().length > 0);
    var m = $('hot-mail'); m.classList.remove('bump'); void m.offsetWidth; m.classList.add('bump');
  }, 620);
});

function markSeen(what, id) {
  var list = what === 'postcard' ? postcards() : souvenirs();
  list.forEach(function (x) { if (x.id === id) x.seen = true; });
  send({ type: 'seen', what: what, id: id });
}

/* ---- album ---- */
function renderAlbum() {
  var g = $('grid'); g.innerHTML = '';
  var list = postcards().slice().sort(function (a, b) { return b.at - a.at; });
  if (!list.length) { g.className = 'empty'; for (var i = 0; i < 4; i++) g.appendChild(el('i')); return; }
  g.className = 'grid';
  list.forEach(function (c) {
    var b = el('button', 'mini' + (c.seen ? '' : ' fresh'));
    b.appendChild(img('a/pc_' + c.kind + '.jpg'));
    b.appendChild(el('span', 'd', fmt(c.at))); b.appendChild(el('i', 'stamp'));
    b.addEventListener('click', function () { if (!c.seen) markSeen('postcard', c.id); openZoomCard(c); });
    g.appendChild(b);
  });
}
function openAlbum() { screen = 'album'; renderAlbum(); $('albumscroll').scrollTop = 0; $('album').classList.add('on'); renderBack(); }
function closeAlbum() { screen = 'house'; $('album').classList.remove('on'); renderBack(); renderHouse(); }

function openZoomCard(card) { var h = $('zoomholder'); h.innerHTML = ''; h.appendChild(postcardEl(card)); on('zoom'); }
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
    if (v) s.addEventListener('click', function () { markSeen('souvenir', v.id); openZoomSouvenir(v); });
    r.appendChild(s);
  });
}
function openSheet() { renderSheet(); on('sheet'); }
$('sheettap').addEventListener('click', function () { off('sheet'); });

function openZoomSouvenir(v) {
  var h = $('zoomholder'); h.innerHTML = '';
  h.appendChild(img('a/sv_' + v.item + '.webp', 'big'));
  var p = el('div', 'postcard small');
  p.appendChild(img('a/pc_' + v.kind + '.jpg', 'pic'));
  var strip = el('div', 'strip'), line = el('span', 'line');
  line.textContent = (v.title || '') + (v.title ? '  ' : '') + fmt(v.at);
  strip.appendChild(line); p.appendChild(strip);
  h.appendChild(p);
  on('zoom');
}

/* ---- yard ---- */
function yard() { return (S && S.yard) || { treats: [], pantry: {}, bag: '' }; }
function renderYard() {
  var night = isNight();
  $('y-day').classList.toggle('on', !night); $('y-night').classList.toggle('on', night);
  var box = $('treats'); box.innerHTML = '';
  yard().treats.forEach(function (t) {
    if (t.taken) return;
    var spot = SLOTS[t.slot % SLOTS.length];
    var b = el('button', 'tt'); b.style.left = spot[0] + '%'; b.style.top = spot[1] + '%';
    b.appendChild(img('a/t_' + t.kind + '.webp'));
    b.addEventListener('click', function () { collect(t, b); });
    box.appendChild(b);
  });
  renderTray($('yardtray'));
}
function collect(t, node) {
  var y = yard();
  if ((y.pantry[t.kind] || 0) >= PANTRY_CAP) return;
  t.taken = true; y.pantry[t.kind] = (y.pantry[t.kind] || 0) + 1;
  node.classList.add('take');
  send({ type: 'collect', slot: t.slot });
  setTimeout(function () { renderTray($('yardtray')); }, 250);
}
var SATCHEL = '<svg viewBox="0 0 34 34" fill="none" stroke="#4a3b2e" stroke-width="2.2" stroke-linejoin="round" stroke-linecap="round"><path d="M9 13c0-7 16-7 16 0"/><path d="M5 15h24l-2 15H7z" fill="#e7cfa4"/><path d="M5 15l12 6 12-6" stroke="#c8442f"/></svg>';
function renderTray(box) {
  var y = yard();
  box.innerHTML = '';
  var any = false;
  TREATS.forEach(function (k) {
    var n = y.pantry[k] || 0;
    if (n <= 0) return;
    any = true;
    var b = el('button', 'it'); b.appendChild(img('a/t_' + k + '.webp')); b.appendChild(el('b', '', String(n)));
    b.addEventListener('click', function () { pack(k); });
    box.appendChild(b);
  });
  if (!any && !y.bag) return;
  var bag = el('button', 'it bag', SATCHEL);
  if (y.bag) bag.appendChild(img('a/t_' + y.bag + '.webp'));
  bag.addEventListener('click', function () { if (yard().bag) pack(''); });
  box.appendChild(bag);
}
function pack(kind) {
  var y = yard();
  if (kind === y.bag) return;
  if (kind && (y.pantry[kind] || 0) <= 0) return;
  if (y.bag) y.pantry[y.bag] = (y.pantry[y.bag] || 0) + 1;
  y.bag = '';
  if (kind) { y.pantry[kind]--; y.bag = kind; }
  send({ type: 'pack', treat: kind });
  renderTray($('yardtray')); renderTray($('bagtray')); renderHouse();
}
function openYard() { screen = 'yard'; renderYard(); $('yard').classList.add('on'); renderBack(); }
function closeYard() { screen = 'house'; $('yard').classList.remove('on'); renderBack(); renderHouse(); }
function openBag() { renderTray($('bagtray')); on('bagsheet'); }
$('bagtap').addEventListener('click', function () { off('bagsheet'); });

/* ---- style picker: tap the picture frame ---- */
function renderStyleRow() {
  var row = $('stylerow'), names = (window.CAT_STYLES[LANG] || window.CAT_STYLES.en);
  row.innerHTML = '';
  STYLES.forEach(function (k) {
    var b = el('button', 'stile' + (k === STYLE ? ' on' : ''));
    b.setAttribute('aria-pressed', k === STYLE ? 'true' : 'false');
    b.appendChild(img('a/s/' + k + '/card.jpg'));
    var n = el('span'); n.textContent = names[k]; b.appendChild(n);
    b.addEventListener('click', function () { chooseStyle(k); });
    row.appendChild(b);
  });
}
function chooseStyle(k) {
  if (k === STYLE) return;
  changeStyle(k, true);
  send({ type: 'style', style: k });
}
function openStyles() { renderStyleRow(); on('styles'); var s = document.querySelector('#stylerow .stile.on'); if (s && s.scrollIntoView) s.scrollIntoView({ inline: 'center', block: 'nearest' }); }
$('stylestap').addEventListener('click', function () { off('styles'); });

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
  if ($('zoom').classList.contains('on')) closeZoom();
  else if ($('sheet').classList.contains('on')) off('sheet');
  else if ($('bagsheet').classList.contains('on')) off('bagsheet');
  else if ($('styles').classList.contains('on')) off('styles');
  else if (screen === 'album' && $('albumscroll').scrollTop <= 0) closeAlbum();
}, { passive: true });

/* ---- day turns to night while the page is open ---- */
setInterval(function () { if (S) { renderHouse(false); if (screen === 'yard') renderYard(); } }, 60000);
document.addEventListener('visibilitychange', function () { if (!document.hidden && S) renderHouse(false); });

window.__cat = { setState: setState, openStyles: openStyles, chooseStyle: chooseStyle, get style() { return STYLE; }, get state() { return S; }, openAlbum: openAlbum, openYard: openYard, openSheet: openSheet, openBag: openBag };

renderBack();
renderHouse(false);
send({ type: 'ready' });
})();
