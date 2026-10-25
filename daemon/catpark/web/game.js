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
var SV_ORDER = ['palette', 'bottle', 'book', 'wrench', 'lantern', 'pot', 'telescope', 'bell', 'teapot', 'stamp', 'compass', 'pinecone'];
var SLOTS = [[30, 63], [48, 69], [66, 62], [40, 75], [60, 76]];   // yard treat spots on the bed of soil, % of the picture
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
/* the hour of the day; ?hour=6.5 pins it, to look at the room at any time */
var HOUR = parseFloat((location.search.match(/[?&]hour=([0-9.]+)/) || [])[1]);
function hour() { if (HOUR >= 0 && HOUR < 24) return HOUR; var d = new Date(); return d.getHours() + d.getMinutes() / 60; }
function isNight() { var h = hour(); return h >= 20 || h < 6; }
/* the cat's day: each part starts at its hour (life.js lives by it, the diary remembers it) */
var DAY = [[0, 'night'], [5.5, 'dawn'], [8, 'morning'], [12, 'noon'], [13.5, 'afternoon'], [17, 'dusk'], [19.5, 'evening'], [22.5, 'night']];
function slotOf(h) { var s = DAY[0][1]; DAY.forEach(function (d) { if (h >= d[0]) s = d[1]; }); return s; }
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
    setState(msg.state);
  } else if (msg.type === 'focus') {
    // the host's tab is on or off screen: the room sleeps with it, and so do its sounds
    if (window.CatLife) window.CatLife.pause(!msg.on);
    if (window.CatSound) window.CatSound.pause(!msg.on);
    if (msg.on && S) visit();
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

function words() { return window.CAT_UI; }
function cwords() { return C.words; }
function applyWords() {
  var w = words();
  Object.keys(w).forEach(function (id) { var n = $(id); if (n && id !== 'home') n.setAttribute('aria-label', w[id]); });
}
applyWords();

function setState(next) {
  var first = known === null, prev = S;
  var fresh = [], freshSv = [];
  if (!first) {
    (next.postcards || []).forEach(function (c) { if (!known.p[c.id]) fresh.push(c); });
    (next.souvenirs || []).forEach(function (v) { if (!known.s[v.id]) freshSv.push(v); });
  }
  S = next;
  if (next.now) skew = next.now - Date.now();
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
  if (first) { visit(); setTimeout(askName, 6000); }
  maybeDiary();
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

  // the waiting sign hangs on the door
  $('sign').classList.toggle('on', !!p.waiting);
  $('hot-sign').style.display = p.waiting ? '' : 'none';

  // what you packed peeks out of the satchel on its peg
  var bag = yard().bag, bt = $('bagtreat');
  if (bag && !p.away) { bt.src = food(bag); bt.classList.add('on'); } else { bt.classList.remove('on'); }

  renderSouvenirs(popShelf);
  renderFrames();
  renderAway();
}

/* while the cat is out, a note where it sits says where it went and when it is back */
var skew = 0, awayTimer = 0;   // skew: the daemon's clock minus ours, so "back in" never trusts the phone's clock
function awayText() {
  var p = S.presence, W = cwords(), place = W.places[p.kind] || W.places.trail;
  var n = Math.ceil((p.back - (Date.now() + skew)) / 60000);
  return { place: place, n: n, note: W.away.note.replace('{p}', place), when: n > 1 ? W.away['in'].replace('{n}', n) : W.away.soon,
    line: n > 1 ? W.away.line.replace('{p}', place).replace('{n}', n) : W.away.line_soon.replace('{p}', place) };
}
function renderAway() {
  var p = (S && S.presence) || {}, note = $('awaynote');
  clearTimeout(awayTimer);
  // shown once the cat has gone through the window, not while it is still packing
  if (!p.away || !p.back || (window.CatLife && !window.CatLife.away)) { note.classList.remove('on'); return; }
  var t = awayText();
  note.querySelector('img').src = 'a/pl_' + (C.places[p.kind] ? p.kind : 'trail') + '.jpg';
  $('awaywhere').textContent = t.note;
  $('awaywhen').textContent = t.when;
  note.setAttribute('aria-label', t.line);
  note.classList.add('on');
  awayTimer = setTimeout(renderAway, 20000);
}
/* the note opens the outing's postcard once it has come, or the place it went to */
function openAway() {
  var p = (S && S.presence) || {};
  if (!p.away) return;
  if (window.CatSound) window.CatSound.paper();
  var card = p.card && postcards().filter(function (c) { return c.id === p.card; })[0];
  if (card) { if (!card.seen) markSeen('postcard', card.id); openZoomCard(card); return; }
  var t = awayText(), h = $('zoomholder'), pc = el('div', 'postcard away');
  var sc = el('div', 'scene'); sc.appendChild(img('a/pl_' + (C.places[p.kind] ? p.kind : 'trail') + '.jpg', 'bg'));
  var strip = el('div', 'strip'), line = el('span', 'line'); line.textContent = t.line; strip.appendChild(line);
  pc.appendChild(sc); pc.appendChild(strip);
  h.innerHTML = ''; h.appendChild(pc); on('zoom');
}

/* what it brought home is put about the room, each kind of thing in its own place: the newest of each */
var svShown = {};
function renderSouvenirs(pop) {
  var box = $('shelfitems'), newest = {};
  souvenirs().forEach(function (v) { if (K.sv[v.item] && (!newest[v.item] || newest[v.item].at < v.at)) newest[v.item] = v; });
  Object.keys(newest).forEach(function (item) {
    var v = newest[item], n = svShown[item];
    if (n && n.dataset.id === v.id) return;
    if (window.CatLife && window.CatLife.holding()) return;   // the cat is still carrying it home
    if (!n) {
      var at = K.sv[item];
      n = el('button', 'si'); n.appendChild(img('a/sv_' + item + '.webp'));
      n.style.left = (at[0] * 100) + '%'; n.style.bottom = ((1 - at[1]) * 100) + '%'; n.style.width = (at[2] * 100) + '%';
      n.setAttribute('aria-label', words().souvenir);
      n.addEventListener('click', function () { var cur = newest[item]; souvenirs().forEach(function (x) { if (x.id === n.dataset.id) cur = x; }); markSeen('souvenir', cur.id); openZoomSouvenir(cur); });
      box.appendChild(n); svShown[item] = n;
    }
    n.dataset.id = v.id;
    if (pop && known) { n.classList.remove('pop'); void n.offsetWidth; n.classList.add('pop'); }
  });
}

/* the wall fills up as you grow close: a framed postcard at each of the kit's levels, the newest ones */
var framesKey = '';
function renderFrames() {
  var n = K.framelv.filter(function (lv) { return cat().level >= lv; }).length;
  var list = postcards().filter(function (c) { return c.phase !== 'letter'; });
  var pick = list.slice().sort(function (a, b) { return b.at - a.at; }).slice(0, n);
  var key = pick.map(function (c) { return c.id; }).join(',') + '|' + n;
  if (key === framesKey) return;
  var before = framesKey === '' ? -1 : framesKey.split('|')[1] * 1;
  framesKey = key;
  var box = $('frames'); box.innerHTML = '';
  pick.forEach(function (c, i) {
    var f = K.frames[i], b = el('button', 'frame' + (before >= 0 && i >= before ? ' pop' : ''));
    b.dataset.id = c.id;
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
$('awaynote').addEventListener('click', openAway);

/* ---- care ---- */
function petCat() {
  if (away() || !window.CatLife) return;
  count('pet');
  window.CatLife.poke();
  act('pet', '');
  emit('did', 'pet');
  if (!cat().name) setTimeout(askName, 2600);
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
  y.pantry[k]--; count('feed'); cat().learned.feed = true;
  act('feed', k);
  if (window.CatLife) window.CatLife.eat(k);
  emit('did', 'feed');
}
function play() {
  if (away() || !window.CatLife) return;
  count('play');
  act('play', '');
  window.CatLife.doAct('yarn', 14000);
  emit('did', 'play');
}
function hobby(id) {
  if (away() || !window.CatLife || cat().hobbies.indexOf(id) < 0) return;
  count('hobby');
  act('hobby', id);
  if (id === 'letters' && !(cat().today['yield:letters'] > 0)) cat().today['yield:letters'] = 1;
  window.CatLife.doAct(id, 18000);
  emit('did', 'hobby');
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
  line.textContent = window.CatCard.line(card);
  strip.appendChild(line);
  if (card.treat && !home) strip.appendChild(img(food(card.treat), 'tr'));
  p.appendChild(strip);
  if (cat().name) p.appendChild(el('span', 'sig', '')).textContent = cwords().name.sign.replace('{n}', cat().name);
  if (!home) { p.appendChild(el('i', 'stamp')); p.appendChild(el('span', 'pm', fmt(card.at))); }
  return p;
}

function maybeArrive() {
  if (diaryWaits()) { setTimeout(maybeArrive, 700); return; }
  if (screen === 'house' && !overlayOpen() && unseen().length && !(window.CatGuide && window.CatGuide.holdMail())) showArrive();
}

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
  if (earns('read')) count('read');
  markSeen('postcard', p.dataset.id);
  off('arrive', 700);
  setTimeout(function () {
    arriving = false;
    renderHouse(false);
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

/* ---- its name: asked once you have met (after the first pet), until it has one ---- */
var nameAsked = false, nameHold = 0;
function askName() {
  if (nameAsked || cat().name || !learnt('pet') || away()) return;
  // after the letter or the diary, back home, once the cat has said hello
  if (overlayOpen() || screen !== 'house' || document.hidden || diaryWaits() || Date.now() < nameHold) { setTimeout(askName, 2500); return; }
  nameAsked = true;
  var W = cwords().name;
  $('nameask').textContent = W.ask;
  $('namein').placeholder = W.hint; $('namein').value = '';
  $('nameok').textContent = W.ok;
  on('namesheet');
  setTimeout(function () { try { $('namein').focus(); } catch (e) { /* the keyboard comes on the first tap */ } }, 420);
}
function cleanName(v) { return Array.from(v.replace(/\s+/g, ' ').replace(/[^\p{L}\p{N} '\-]/gu, '').trim()).slice(0, 16).join('').trim(); }
$('nameform').addEventListener('submit', function (e) {
  e.preventDefault();
  var name = cleanName($('namein').value);
  if (!name) { bump($('namein')); return; }
  $('namein').blur();
  var c = cat(); c.name = name; c.learned = c.learned || {}; c.learned.name = true;
  act('name', name);
  off('namesheet');
  if (window.CatSound) window.CatSound.chime();
  if (window.CatLife) setTimeout(function () { window.CatLife.react('greet', 2400).then(function () { window.CatLife.hearts(3); }); }, 500);
  emit('did', 'name');
});
$('nametap').addEventListener('click', function () { $('namein').blur(); off('namesheet'); });

/* ---- while you were away: on a new visit the cat's diary opens on the time you were gone,
   from what the daemon remembers and the hours of its day. A reopen within minutes is the
   same visit and shows nothing; the calendar on the wall opens the page again. ---- */
var visits = null, visitAt = 0, diary = null, diaryPending = 0;
function visit() { visitAt = Date.now(); act('visit', ''); }
/* the diary goes first: while a visit is on its way to the daemon (over a slow tunnel it can
   take seconds) and while a page is due, the postcard and the name card wait */
function diaryWaits() { return !!diaryPending || Date.now() - visitAt < 6000; }
function maybeDiary() {
  var c = cat(), was = visits;
  visits = c.visits;
  // a visit counted just now, for this page (another device's visit opens nothing here)
  if (was == null || !(c.visits > was) || Date.now() - visitAt > 15000) return;
  visitAt = 0;
  var d = composeDiary();
  diary = d;
  $('calmark').classList.toggle('on', !!d.lines.length);
  if (!c.since || !learnt('pet') || !(c.gap >= 3600e3 || d.events || d.cards.length)) return;
  var token = diaryPending = c.visits;
  setTimeout(function open() {
    if (diaryPending !== token) return;   // a newer visit has its own page
    if (screen !== 'house') { diaryPending = 0; return; }   // you went elsewhere: the calendar keeps it
    if (overlayOpen() || document.hidden) { setTimeout(open, 500); return; }
    diaryPending = 0;
    openDiary(d, true);
  }, 500);
}
function seeded(seed) { var x = (seed % 2147483647) || 1; return function () { x = x * 48271 % 2147483647; return x / 2147483647; }; }
function clock(ts) {
  var d = new Date(ts), h = d.getHours(), m = d.getMinutes();
  return ((h + 11) % 12 + 1) + (m ? ':' + ('0' + m).slice(-2) : '') + (h < 12 ? ' am' : ' pm');
}
/* the parts of its day between two moments, each with when it began */
function partsOf(from, to) {
  var out = [], t = from;
  while (t < to && out.length < 60) {
    var d = new Date(t), h = d.getHours() + d.getMinutes() / 60, next = null;
    for (var i = 0; i < DAY.length; i++) if (DAY[i][0] > h) { next = DAY[i][0]; break; }
    var end = new Date(d.getFullYear(), d.getMonth(), d.getDate() + (next == null ? 1 : 0), 0, 0, 0, 0);
    end = end.getTime() + (next == null ? 0 : next * 3600e3);
    var slot = slotOf(h), last = out[out.length - 1];
    if (last && last.slot === slot) last.to = Math.min(end, to);   // the night runs over midnight
    else out.push({ slot: slot, at: t, to: Math.min(end, to) });
    t = end;
  }
  return out;
}
function composeDiary() {
  var c = cat(), W = cwords().diary, P = cwords(), since = c.since || 0, now = Date.now() + skew;
  var rand = seeded(Math.floor(since / 1000)), lines = [], events = 0;
  var memory = (c.memory || []).filter(function (m) { return m.at > since; });
  // where it went, and when it was back
  memory.forEach(function (m, i) {
    if (m.what !== 'leave') return;
    var back = memory.slice(i + 1).filter(function (x) { return x.what === 'home' || x.what === 'back'; })[0];
    var place = P.places[m.arg] || P.places.trail, today = back && new Date(back.at).toDateString() === new Date(now).toDateString();
    lines.push({ at: m.at, text: W.out.replace('{p}', place) + (today ? ' ' + W.back.replace('{t}', clock(back.at)) : ''), pic: 'a/pl_' + (C.places[m.arg] ? m.arg : 'trail') + '.jpg', place: true });
    events++;
  });
  // what it brought home, and where it put it
  var brought = {};
  souvenirs().forEach(function (v) {
    if (v.at <= since || brought[v.item] || !W.brought[v.item]) return;
    brought[v.item] = v;
    lines.push({ at: v.at, text: W.brought[v.item], pic: 'a/sv_' + v.item + '.webp', sv: v.item });
    events++;
  });
  // what it made for its home as you grew close, and the friends who came by
  var grew = [];
  memory.forEach(function (m) {
    if (m.what === 'level') {
      // a thing it made is a corner of the room: a little photo of it
      var lv = m.arg * 1, g = K.grow.filter(function (x) { return x.lv === lv; })[0];
      if (g && W.grew[g.id]) { grew.push(g.id); lines.push({ at: m.at, text: W.grew[g.id], pic: 'a/' + (g.t || g.d) + '.webp', place: true }); events++; }
      else if (K.framelv.indexOf(lv) >= 0 && W.grew.frame) { lines.push({ at: m.at, text: W.grew.frame }); events++; }
    }
    if (m.what === 'friend' && W.friend[m.arg]) { lines.push({ at: m.at, text: W.friend[m.arg], pic: 'a/f_' + m.arg + '.webp' }); events++; }
  });
  memory.forEach(function (m) {
    if (m.what !== 'learn' || !P.hobbies[m.arg]) return;
    var pic = K.props[m.arg] || (K.yard[m.arg] && K.yard[m.arg].a);
    lines.push({ at: m.at, text: W.learnt.replace('{h}', P.hobbies[m.arg]), pic: pic ? 'a/' + pic + '.webp' : null });
    events++;
  });
  // its day at home: the parts that went by while you were gone, the last few
  var parts = partsOf(since, now).filter(function (x) { return x.to - x.at >= 3600e3 && x.to < now; }).slice(-3);
  var doing = (c.hobbies || []).filter(function (h) { return h !== 'stargaze' && /ing$/.test(P.hobbies[h] || ''); });
  parts.forEach(function (x) {
    var list = W.slots[x.slot] || [], text = list[Math.floor(rand() * list.length)];
    if (x.slot === 'afternoon' && doing.length && rand() < 0.6) text = W.hobby.replace('{h}', P.hobbies[doing[Math.floor(rand() * doing.length)]]);
    if (text) lines.push({ at: x.at, text: text });
  });
  lines.sort(function (a, b) { return a.at - b.at; });
  // a few lines: what happened comes before what it always does
  while (lines.length > 6) {
    var plain = -1;
    for (var i = 0; i < lines.length; i++) if (!lines[i].pic) { plain = i; break; }
    lines.splice(plain < 0 ? 0 : plain, 1);
  }
  var days = Math.floor((now - since) / 86400e3);
  if (days >= 2) lines.unshift({ at: since, text: W.days.replace('{n}', days) });
  var cards = postcards().filter(function (p) { return p.at > since; }).sort(function (a, b) { return a.at - b.at; }).slice(-3);
  return { since: since, lines: lines, events: events, cards: cards, brought: brought, grew: grew };
}
function openDiary(d, arrival) {
  var W = cwords().diary, page = $('diarypage'), now = new Date(Date.now() + skew);
  page.innerHTML = '';
  var head = el('div', 'dhead');
  head.appendChild(el('b', 'dday', String(now.getDate())));
  var wk = el('span', 'dwk');
  wk.appendChild(el('i', '', now.toLocaleDateString('en-GB', { weekday: 'long' })));
  wk.appendChild(el('i', '', now.toLocaleDateString('en-GB', { month: 'long' })));
  head.appendChild(wk);
  page.appendChild(head);
  page.appendChild(el('h3', 'dtitle')).textContent = W.title;
  var ul = el('ul', 'dlines');
  (d.lines.length ? d.lines : [{ text: W.quiet }]).forEach(function (l) {
    var li = el('li', l.pic ? 'pic' + (l.place ? ' place' : '') : '');
    if (l.pic) li.appendChild(img(l.pic));
    li.appendChild(el('span')).textContent = l.text;
    ul.appendChild(li);
  });
  page.appendChild(ul);
  if (d.cards.length) {
    var row = el('div', 'dcards');
    d.cards.forEach(function (card, i) {
      var b = el('button', 'dcard' + (card.seen ? '' : ' fresh'));
      b.style.setProperty('--r', [-4, 3, -2][i % 3] + 'deg');
      b.appendChild(window.CatCard.scene(card));
      b.setAttribute('aria-label', words().frame);
      b.addEventListener('click', function () {
        if (window.CatSound) window.CatSound.paper();
        if (!card.seen) { markSeen('postcard', card.id); b.classList.remove('fresh'); count('read'); }
        openZoomCard(card);
      });
      row.appendChild(b);
    });
    page.appendChild(row);
  }
  if (cat().name) page.appendChild(el('span', 'sig')).textContent = cwords().name.sign.replace('{n}', cat().name);
  page.dataset.arrival = arrival ? '1' : '';
  on('diary');
  if (window.CatSound) window.CatSound.paper();
}
/* closed: the things it wrote about are there in the room, and each gives one little shine */
function closeDiary() {
  var page = $('diarypage'), d = diary, arrival = page.dataset.arrival === '1';
  off('diary');
  $('calmark').classList.add('on');
  if (!arrival || !d) return;
  page.dataset.arrival = '';
  nameHold = Date.now() + 8000;
  setTimeout(function () {
    var shine = [];
    Object.keys(d.brought).forEach(function (item) { if (svShown[item]) shine.push(svShown[item]); });
    (d.grew || []).forEach(function (id) { var g = window.CatLife && window.CatLife.grown(id); if (g) shine.push(g); });
    document.querySelectorAll('#frames .frame').forEach(function (f) { if (d.cards.some(function (c) { return c.id === f.dataset.id; })) shine.push(f); });
    shine.forEach(function (n, i) { setTimeout(function () { n.classList.remove('pop', 'glint'); void n.offsetWidth; n.classList.add('glint'); }, i * 260); });
  }, 450);
}
$('diarytap').addEventListener('click', closeDiary);
$('hot-cal').addEventListener('click', function () {
  if (window.CatSound) window.CatSound.paper();
  openDiary(diary || composeDiary(), false);
});

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
  count('hobby'); act('hobby', which);
  if (which === 'fish' && !(c.today['yield:fish'] > 0)) {
    c.today['yield:fish'] = 1;
    var y = yard(); y.pantry.fish = Math.min(PANTRY_CAP, (y.pantry.fish || 0) + 1);
    setTimeout(function () { renderYardTray(); bump(document.querySelector('#yardtray .it[data-k="fish"]')); }, 900);
  }
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
  var top = ['zoom', 'sheet', 'bagsheet', 'foodsheet'].filter(function (id) { return $(id).classList.contains('on'); })[0];
  if (top) { if (top === 'zoom') closeZoom(); else off(top); }
  else if (screen === 'album' && $('albumscroll').scrollTop <= 0) closeAlbum();
  else if (screen === 'yard') closeYard();
}, { passive: true });

/* ---- day turns to night while the page is open ---- */
setInterval(function () { if (S) { renderHouse(false); if (screen === 'yard') renderYard(); } }, 60000);
document.addEventListener('visibilitychange', function () { if (!document.hidden && S) { renderHouse(false); visit(); } });

window.CatShell = {
  words: words, cwords: cwords, renderHouse: renderHouse, renderAway: renderAway, cat: cat, yard: yard, away: away, postcards: postcards, unseen: unseen,
  isNight: isNight, screen: function () { return screen; },
  onHouse: function () { return screen === 'house' && !overlayOpen() && !document.hidden; },
  overlayOpen: overlayOpen, arriving: function () { return arriving; },
  on: function (name, fn) { (listeners[name] = listeners[name] || []).push(fn); },
  hotCat: function (x, y, w, h) {
    var c = $('hot-cat');
    if (x == null) { c.style.display = 'none'; return; }
    c.style.display = ''; c.style.left = (x * 100) + '%'; c.style.top = (y * 100) + '%'; c.style.width = (w * 100) + '%'; c.style.height = (h * 100) + '%';
  },
  openFood: openFood, play: play, hobby: hobby, yardNew: yardNew, learnt: learnt, markLearnt: markLearnt, showArrive: showArrive, maybeArrive: maybeArrive, act: act, hour: hour, slotOf: slotOf
};
/* audio starts with the first touch, as the browser demands */
['touchend', 'click'].forEach(function (name) { document.addEventListener(name, function () { if (window.CatSound) window.CatSound.unlock(); }, { passive: true }); });

window.__cat = { setState: setState, get state() { return S; }, openAlbum: openAlbum, openYard: openYard, openSheet: openSheet, openBag: openBag, openFood: openFood };

renderBack();
// the host can miss the first hello (a tab mounted off screen): say it until the state comes
(function hello() { if (S) return; send({ type: 'ready' }); setTimeout(hello, 1000); })();
})();
