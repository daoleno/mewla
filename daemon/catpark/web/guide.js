(function () {
'use strict';
/* The first visit teaches itself, without words: the cat walks over to the
   next thing to try and thinks of it, and that thing glows. Each step is done
   once (the daemon remembers it per person) and then never shown again. Later,
   anything new introduces itself the same way: a hobby's prop, the jar's first
   star, a level. */

var Sh = window.CatShell, Life = window.CatLife, K = window.CAT_KIT;
if (!Sh || !Life) return;
var $ = function (id) { return document.getElementById(id); };
function svg(body) { var d = document.createElement('div'); d.className = 'icon'; d.innerHTML = '<svg viewBox="0 0 34 34" fill="none" stroke="#4a3b2e" stroke-width="2.2" stroke-linejoin="round" stroke-linecap="round">' + body + '</svg>'; return d; }
function picIcon(src) { var i = new Image(); i.src = src; i.alt = ''; i.className = 'icon'; return i; }

var ICON = {
  mail: function () { return svg('<rect x="4" y="8" width="26" height="18" rx="2" fill="#fffaf0"/><path d="M5 10l12 9 12-9"/>'); },
  feed: function () { return picIcon('a/t_' + (Sh.cat().favorite || 'fish') + '.webp'); },
  yard: function () { return picIcon('a/t_berry.webp'); },
  pack: function () { return svg('<path d="M10 13c0-7 14-7 14 0"/><path d="M6 15h22l-2 14H8z" fill="#e7cfa4"/><path d="M6 15l11 6 11-6" stroke="#c8442f"/>'); }
};
/* where the cat goes to show each thing, and what glows */
var STEPS = [
  { id: 'pet', glow: function () { return null; } },
  { id: 'mail', spot: [0.6, 0.67], glow: function () { return $('hot-mail'); }, when: function () { return Sh.unseen().length > 0; } },
  { id: 'feed', spot: [0.8, 0.72], glow: function () { return document.querySelector('#life .prop.bowl'); }, home: true },
  { id: 'yard', spot: [0.72, 0.64], glow: function () { return $('hot-window'); } },
  { id: 'pack', spot: [0.2, 0.74], glow: function () { return $('hot-bag'); } }
];
var G = { step: null, glowing: null, ring: null, wait: 0, go: 0, intro: null, introGlow: null };

function learned(id) { var l = Sh.cat().learned || {}; return !!l[id]; }
function current() {
  for (var i = 0; i < STEPS.length; i++) {
    var s = STEPS[i];
    if (learned(s.id)) continue;
    if (s.when && !s.when()) continue;
    if ((s.id === 'pet' || s.home) && Sh.away()) continue;
    return s;
  }
  return null;
}
function inTutorial() { return STEPS.some(function (s) { return !learned(s.id); }); }

function setGlow(node) {
  if (G.glowing === node) return;
  if (G.glowing) G.glowing.classList.remove('glow');
  G.glowing = node;
  if (node) node.classList.add('glow');
}
function ring(on) {
  if (!G.ring) { G.ring = document.createElement('i'); G.ring.className = 'catring'; $('stage').appendChild(G.ring); }
  if (on) { var p = Life.catPoint(); G.ring.style.left = (p[0] * 100) + '%'; G.ring.style.top = ((p[1] + 0.06) * 100) + '%'; }
  G.ring.classList.toggle('on', !!on);
}

/* look again at what to show: called on every change */
function evaluate() {
  clearTimeout(G.wait);
  var screen = Sh.screen();
  document.body.classList.toggle('guide-yard', screen === 'yard' && !learned('yard'));
  document.body.classList.toggle('guide-back', screen === 'yard' && learned('yard') && inTutorial());
  document.body.classList.toggle('guide-pack', !learned('pack'));
  document.body.classList.toggle('guide-feed', !learned('feed'));
  if (screen !== 'house' || Sh.overlayOpen() || Sh.arriving()) { setGlow(null); ring(false); return; }
  if (G.intro) { ring(false); setGlow(G.introGlow); return; }
  var s = current();
  if (!s) {
    ring(false); if (G.step) { G.step = null; Life.release(); }
    // a hobby the cat does out in the yard calls through the window until it is seen
    setGlow(Sh.yardNew() ? $('hot-window') : null);
    return;
  }
  if (Life.busy) { G.wait = setTimeout(evaluate, 700); return; }
  if (s.id === 'pet') { setGlow(null); ring(true); G.step = s; G.wait = setTimeout(evaluate, 1500); return; }
  ring(false);
  setGlow(s.glow());
  if (G.step !== s) {
    G.step = s; G.went = false;
    clearTimeout(G.go);
    // give the greeting a moment before the cat walks over
    G.go = setTimeout(function walk() {
      if (G.step !== s) return;
      if (!Sh.onHouse() || Life.busy || Life.held) { G.go = setTimeout(walk, 800); return; }
      G.went = true; Life.attend(s.spot, ICON[s.id]());
    }, 1400);
  } else if (G.went && !Life.attending && !Life.busy && !Life.held) {
    // it wandered off (a tap, the hour): it comes back to show the thing again
    G.went = false; G.step = null; evaluate();
  }
}

/* a new hobby: once the cat is home, its prop glows and the cat goes over to it */
function introduce(hobbies) {
  var h = hobbies[0];
  var go = function () {
    if (!Sh.onHouse() || Life.busy || Life.away) { setTimeout(go, 900); return; }
    if (K.yard && K.yard[h] && !Life.prop(h)) { introduceYard(h); return; }
    var spot = Life.propSpot(h), p = Life.prop(h);
    if (!spot || !p) return;
    G.intro = h; setGlow(null); ring(false);
    var i = new Image(); i.src = 'a/' + K.props[h] + '.webp'; i.alt = ''; i.className = 'icon';
    Life.attend(spot, i);
    p.classList.add('glow');
    var done = function () { p.classList.remove('glow'); G.intro = null; evaluate(); };
    p.addEventListener('click', done, { once: true });
    setTimeout(function () { if (G.intro === h) { G.intro = null; Life.release(); evaluate(); } }, 20000);
  };
  setTimeout(go, 2600);
}

/* a yard hobby: the cat goes to the window and thinks of itself out there;
   the window glows (evaluate keeps it glowing) and in the yard the cat glows */
function introduceYard(h) {
  if (Sh.yardNew() !== h) return; // fishing waits for day, camping for night: the window glows then
  var i = new Image(); i.src = 'a/' + K.yard[h].a + '.webp'; i.alt = ''; i.className = 'icon';
  G.intro = h; G.introGlow = $('hot-window');
  Life.attend(STEPS[3].spot, i);
  setGlow(G.introGlow);
  var done = function () { if (G.intro !== h) return; G.intro = null; G.introGlow = null; Life.release(); evaluate(); };
  $('hot-window').addEventListener('click', done, { once: true });
  setTimeout(done, 20000);
}

/* the jar introduces itself with its first star */
function firstStar() {
  if (Sh.learnt('jar')) return;
  Sh.markLearnt('jar');
  var jar = document.querySelector('#life .prop.jar');
  if (jar) { jar.classList.add('glow'); setTimeout(function () { jar.classList.remove('glow'); }, 3600); }
}

Sh.on('state', function (e) { setTimeout(evaluate, e.first ? 2200 : 200); });
Sh.on('did', function (what) {
  var c = Sh.cat(); c.learned = c.learned || {};
  if (what === 'pet') c.learned.pet = true;
  if (what === 'mail') c.learned.mail = true;
  if (what === 'collect') c.learned.yard = true;
  if (G.step && (what === G.step.id || (G.step.id === 'yard' && what === 'collect'))) { G.step = null; Life.release(); }
  setTimeout(evaluate, 600);
});
Sh.on('overlay', function () { setTimeout(evaluate, 100); });
Sh.on('screen', function () { setTimeout(evaluate, 300); });
Sh.on('star', firstStar);
Sh.on('learn', introduce);
Sh.on('level', function () { Life.levelUp(); });

window.CatGuide = {
  /* during the first visit the mailbox waits to be tapped, so the person learns where letters go */
  holdMail: function () { return !learned('mail') || (G.step && G.step.id === 'pet'); },
  evaluate: evaluate,
  get step() { return G.step && G.step.id; }
};
})();
