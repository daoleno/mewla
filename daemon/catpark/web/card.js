(function () {
'use strict';
/* A postcard is composed, not painted: the place's empty picture, the hour as a
   wash of light, the day's weather, the cat doing something there, a friend it
   met and the snack it took. The daemon chose every piece and keeps them, so a
   card looks the same every time it is opened; this file only draws it and
   writes its line. */

var C = window.CAT_CATALOG, K = window.CAT_KIT;

function el(tag, cls) { var e = document.createElement(tag); if (cls) e.className = cls; return e; }
function pic(src, cls) { var i = new Image(); i.src = src; i.alt = ''; i.draggable = false; if (cls) i.className = cls; return i; }
function pct(v) { return (v * 100).toFixed(2) + '%'; }
function words() { return C.words; }

/* the scene: a 3:2 box of layers */
function scene(card, live) {
  var box = el('div', 'scene t-' + (card.time || 'day') + ' w-' + (card.weather || 'clear') + (live ? ' live' : ''));
  if (card.phase === 'welcome' || card.phase === 'letter') return note(box, card);
  var place = C.places[card.place] || C.places.trail;
  box.appendChild(pic('a/pl_' + (C.places[card.place] ? card.place : 'trail') + '.jpg', 'bg'));
  box.appendChild(el('i', 'hour'));
  var spot = place.spot, d = K.cards[card.act] || K.cards.read;
  if (d) {
    // spot[2] is how tall the cat itself stands there; a pose with a big prop is taller than the cat
    // a candid one walking off is caught close up at the right edge, its face already out of the picture
    var h = d.edge ? Math.min(0.5, spot[2] * d.k) : spot[2] * d.k, w = h * d.ar / 1.5;
    var cx = d.edge ? 1 - w * 0.2 : spot[0], foot = d.edge ? 0.98 : spot[1];
    var cat = pic('a/' + d.f + '.webp', 'who');
    cat.style.left = pct(cx - w / 2); cat.style.top = pct(foot - h); cat.style.width = pct(w);
    box.appendChild(cat);
    if (card.friend) {
      var f = pic('a/f_' + card.friend + '.webp', 'friend');
      var fh = (d.edge ? h : spot[2]) * 0.5, fw = fh / 1.5, side = place.friend === 'left' ? -1 : place.friend === 'right' ? 1 : cx < 0.6 ? 1 : -1;  // a place may say where there is ground beside the cat
      f.style.left = pct(cx + side * (w / 2 + 0.01) - (side < 0 ? fw : 0)); f.style.top = pct(foot - fh); f.style.width = pct(fw);
      if (side < 0) f.classList.add('flip');
      box.appendChild(f);
    }
    if (card.time === 'night') { var glow = el('i', 'glow'); glow.style.left = pct(cx); glow.style.top = pct(foot - h / 2); box.appendChild(glow); }
  }
  // indoors the weather is only in the words; rain is not drawn across a room
  if (!place.indoor) box.appendChild(el('i', 'weather'));
  return box;
}

/* written at home: paper, the cat's little drawing, no place */
function note(box, card) {
  box.className = 'scene note';
  var c = pic('a/cat_' + (card.phase === 'welcome' ? 'greet_a' : 'letters_a') + '.webp', 'notecat');
  box.appendChild(c);
  return box;
}

function lineFor(card) {
  var W = words(), seed = card.seed >>> 0;
  if (card.phase === 'welcome') return W.welcome;
  if (card.phase === 'letter') {
    var ok = W.letter.filter(function (l) { return (l.indexOf('{t}') < 0 || card.treat) && (l.indexOf('{h}') < 0 || card.hobby); });
    var l = ok[seed % ok.length] || W.letter[0];
    return l.replace('{t}', W.foods[card.treat] || '').replace('{h}', W.hobbies[card.hobby] || '');
  }
  var acts = W.acts[card.act] || W.acts.read;
  var first = acts[seed % acts.length].replace('{p}', W.places[card.place] || W.places.trail);
  var tails = [];
  if (card.friend && W.friends[card.friend]) tails.push(W.friends[card.friend]);
  if (card.treat && W.foods[card.treat]) tails.push(W.treat.replace('{t}', W.foods[card.treat]));
  if (W.weathers[card.weather]) tails.push(W.weathers[card.weather]);
  if (W.times[card.time]) tails.push(W.times[card.time]);
  if (!tails.length) return first;
  var tail = tails[(seed >>> 8) % tails.length];
  return first + ' ' + tail;
}

window.CatCard = { scene: scene, line: lineFor };
})();
