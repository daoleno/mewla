// The seal cat: asleep inside a vermilion seal; it hops out and follows you
// down the page as a small red cat, curls up to sleep wherever it lands, and
// hops home when you scroll back to the top.

const NS = 'http://www.w3.org/2000/svg';
const RED = '#c8372b', RED2 = '#a92b21', PAPER = '#fbf6ec', PAPER2 = '#efe4d2';
const TAU = Math.PI * 2;
const clamp = (v, a, b) => Math.min(b, Math.max(a, v));
const lerp = (a, b, t) => a + (b - a) * t;
const ease = (t) => t * t * (3 - 2 * t);
const back = (t) => 1 + 2.4 * (t - 1) ** 3 + 1.4 * (t - 1) ** 2;
const reduce = matchMedia('(prefers-reduced-motion: reduce)').matches;

function el(tag, attrs = {}, parent = null) {
  const e = document.createElementNS(NS, tag);
  for (const [k, v] of Object.entries(attrs)) e.setAttribute(k, v);
  if (parent) parent.appendChild(e);
  return e;
}

// ---- the curled cat, shared by the seal, its ghost and the live cat --------
// Units: origin at the cat's feet, about 76 wide and 60 tall.
const CURL = {
  body: 'M -6 -2 C -30 -2 -34 -30 -10 -38 C 8 -44 34 -38 38 -18 C 40 -6 30 -1 18 -1 Z',
  head: 'M -20 -45 C -8 -45 -2 -36 -2 -27 C -2 -16 -10 -10 -20 -10 C -31 -10 -38 -17 -38 -27 C -38 -37 -31 -45 -20 -45 Z',
  earL: 'M -36 -33 C -38 -44 -37 -52 -34 -59 C -28 -53 -24 -49 -21 -45 Z',
  earR: 'M -18 -45 C -12 -51 -7 -55 -1 -58 C 0 -50 -1 -42 -4 -35 Z',
  tail: 'M 36 -14 C 40 4 10 6 -16 0',
  lines: ['M -31 -27 Q -28.5 -24.5 -26 -27', 'M -17 -27 Q -14.5 -24.5 -12 -27', 'M -3 -40 Q 6 -26 -2 -12', 'M 33 -9 C 30 1 8 2 -10 -2'],
  nose: [-21.5, -21, 2, 1.5],
};

function drawCurl(g, fill, line, lw = 2) {
  el('path', { d: CURL.tail, fill: 'none', stroke: fill, 'stroke-width': 9, 'stroke-linecap': 'round' }, g);
  for (const k of ['body', 'earL', 'earR', 'head']) el('path', { d: CURL[k], fill }, g);
  for (const d of CURL.lines) el('path', { d, fill: 'none', stroke: line, 'stroke-width': lw, 'stroke-linecap': 'round' }, g);
  const [cx, cy, rx, ry] = CURL.nose;
  el('ellipse', { cx, cy, rx, ry, fill: line }, g);
}

// ---- the seal --------------------------------------------------------------
function rng(seed) {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}
const CAT_IN_SEAL = 'translate(50 79) scale(0.98)';

function buildSeal(svg, { ghost = false, cat = true } = {}) {
  const R = rng(9), pts = [];
  for (let k = 0; k < 4; k++) {
    for (let j = 0; j < 10; j++) {
      const t = j / 10, j1 = (R() - 0.5) * 1.4, j2 = (R() - 0.5) * 1.4;
      pts.push([[4 + 92 * t, 4], [96, 4 + 92 * t], [96 - 92 * t, 96], [4, 96 - 92 * t]][k].map((v, i) => v + (i ? j2 : j1)));
    }
  }
  el('path', { d: 'M ' + pts.map((p) => p.map((v) => v.toFixed(1)).join(' ')).join(' L ') + ' Z', fill: RED }, svg);
  if (ghost) {
    // the empty bed the cat leaves behind
    const g = el('g', { transform: CAT_IN_SEAL, opacity: 0.22, class: 'ghost' }, svg);
    drawCurl(g, PAPER, RED);
  }
  if (cat) drawCurl(el('g', { transform: CAT_IN_SEAL }, svg), PAPER, RED);
  // weathering
  for (let i = 0; i < 46; i++) {
    const a = (R() * 4) | 0, t = R(), edge = R() < 0.7;
    const p = edge ? [[4 + 92 * t, 5], [95, 4 + 92 * t], [4 + 92 * t, 95], [5, 4 + 92 * t]][a] : [6 + R() * 88, 6 + R() * 88];
    el('circle', { cx: p[0].toFixed(1), cy: p[1].toFixed(1), r: (0.3 + R() * (edge ? 1.1 : 0.5)).toFixed(2), fill: 'var(--bg)', opacity: (0.5 + R() * 0.5).toFixed(2) }, svg);
  }
}
buildSeal(document.querySelector('[data-seal="mini"]'));
const sealSvg = document.querySelector('[data-seal="hero"]');
buildSeal(sealSvg, { ghost: true, cat: false });
const ghost = sealSvg.querySelector('.ghost');

// ---- the live cat ----------------------------------------------------------
const svg = el('svg', { class: 'cat', viewBox: '-70 -100 140 110', 'aria-hidden': 'true' }, document.body);
const root = el('g', {}, svg);
const curlG = el('g', {}, root);
const curlInner = el('g', {}, curlG);
const standG = el('g', { display: 'none' }, root);

function paintCurl(home) {
  curlInner.replaceChildren();
  drawCurl(curlInner, home ? PAPER : RED, home ? RED : PAPER);
  svg.style.setProperty('--zz', home ? PAPER : RED);
}
for (let i = 0; i < 3; i++) {
  const z = el('text', { class: 'zz', x: -6 + i * 2, y: -60 }, curlG);
  z.textContent = 'z';
}

// standing rig, facing right
const S = {};
S.legsFar = [el('line', { 'stroke-linecap': 'round', 'stroke-width': 8 }, standG), el('line', { 'stroke-linecap': 'round', 'stroke-width': 8 }, standG)];
S.tail = el('path', { fill: 'none', 'stroke-linecap': 'round', 'stroke-width': 8 }, standG);
S.body = el('ellipse', { cx: 0, cy: -22, rx: 24, ry: 14 }, standG);
S.legsNear = [el('line', { 'stroke-linecap': 'round', 'stroke-width': 8.5 }, standG), el('line', { 'stroke-linecap': 'round', 'stroke-width': 8.5 }, standG)];
S.head = el('g', {}, standG);
S.earL = el('path', { d: 'M -12.5 -6 L -11 -20 L -2 -12 Z', 'stroke-linejoin': 'round', 'stroke-width': 4 }, S.head);
S.earR = el('path', { d: 'M 2 -13 L 11 -20 L 12.5 -6 Z', 'stroke-linejoin': 'round', 'stroke-width': 4 }, S.head);
S.skull = el('ellipse', { cx: 0, cy: 0, rx: 16.5, ry: 14.5 }, S.head);
S.eyesOpen = el('g', {}, S.head);
el('ellipse', { cx: -2, cy: -1, rx: 1.9, ry: 2.4 }, S.eyesOpen);
el('ellipse', { cx: 9, cy: -1, rx: 1.9, ry: 2.4 }, S.eyesOpen);
S.eyesShut = el('g', { fill: 'none', 'stroke-width': 1.8, 'stroke-linecap': 'round' }, S.head);
el('path', { d: 'M -5 -1 Q -2 1.6 1 -1' }, S.eyesShut);
el('path', { d: 'M 6 -1 Q 9 1.6 12 -1' }, S.eyesShut);
S.nose = el('ellipse', { cx: 4, cy: 4, rx: 1.8, ry: 1.3 }, S.head);
for (const l of S.legsFar) l.setAttribute('stroke', RED2);
for (const l of S.legsNear) l.setAttribute('stroke', RED);
S.tail.setAttribute('stroke', RED);
for (const e of [S.body, S.skull, S.earL, S.earR]) e.setAttribute('fill', RED);
for (const e of [S.earL, S.earR]) e.setAttribute('stroke', RED);
S.eyesOpen.setAttribute('fill', PAPER);
S.eyesShut.setAttribute('stroke', PAPER);
S.nose.setAttribute('fill', PAPER);

// ---- state -----------------------------------------------------------------
const cat = {
  x: 0, y: 0, k: 1, dir: 1, curled: true, home: true, perch: null, pending: null,
  sq: 0, rot: 0, legs: 0, blink: false, look: 0, breathe: 1,
};
let queue = [], current = null;

function perches() {
  return [...document.querySelectorAll('[data-perch]')];
}
function spot(node) {
  const r = node.getBoundingClientRect(), sx = scrollX, sy = scrollY;
  if (node.dataset.perch === 'seal') {
    const k = r.width / 102;
    return { x: r.left + sx + r.width / 2, y: r.top + sy + r.height * 0.79, k, home: true };
  }
  const k = clamp(r.width / 330, 0.72, 1);
  return { x: r.left + sx + r.width * +node.dataset.perch, y: r.top + sy + 1, k, home: false };
}

function render(now) {
  const t = now / 1000;
  const w = 140 * cat.k, h = 110 * cat.k;
  svg.setAttribute('width', w.toFixed(1));
  svg.setAttribute('height', h.toFixed(1));
  svg.style.transform = `translate(${(cat.x - 70 * cat.k).toFixed(1)}px, ${(cat.y - 100 * cat.k).toFixed(1)}px)`;
  const sx = (1 + cat.sq * 0.9) * cat.dir, sy = 1 - cat.sq;
  root.setAttribute('transform', `rotate(${(cat.rot * 57.3).toFixed(2)}) scale(${sx.toFixed(3)} ${sy.toFixed(3)})`);
  curlG.setAttribute('display', cat.curled ? 'inline' : 'none');
  standG.setAttribute('display', cat.curled ? 'none' : 'inline');
  if (cat.curled) {
    const b = 1 + 0.025 * Math.sin(t * TAU / 3.6);
    curlInner.setAttribute('transform', `translate(0 0) scale(${(1 + (b - 1) * 0.4).toFixed(4)} ${b.toFixed(4)})`);
    return;
  }
  // legs: 0 standing, 1 tucked for flight, -1 reaching to land
  const L = cat.legs;
  const leg = (line, hx, hy, ang, len) => {
    line.setAttribute('x1', hx); line.setAttribute('y1', hy);
    line.setAttribute('x2', (hx + Math.sin(ang) * len).toFixed(2)); line.setAttribute('y2', (hy + Math.cos(ang) * len).toFixed(2));
  };
  const walk = Math.sin(t * 9) * 0.08 * (1 - Math.abs(L));
  leg(S.legsFar[0], 15, -16, 0.15 + walk + L * 0.9, 13 - Math.abs(L) * 3);
  leg(S.legsFar[1], -14, -16, -0.1 - walk - L * 0.9, 13 - Math.abs(L) * 3);
  leg(S.legsNear[0], 11, -15, -0.05 - walk + L * 1.1, 14 - Math.abs(L) * 3);
  leg(S.legsNear[1], -18, -15, 0.12 + walk - L * 1.1, 14 - Math.abs(L) * 3);
  const wave = Math.sin(t * 3.2) * 5;
  S.tail.setAttribute('d', `M -20 -26 C -34 -30 ${-40 + wave * 0.3} ${-44 - L * 4} ${-30 + wave} ${-54 - L * 6}`);
  S.body.setAttribute('ry', (14 * cat.breathe).toFixed(2));
  S.head.setAttribute('transform', `translate(23 -36) rotate(${(cat.look * 57.3).toFixed(1)})`);
  S.eyesOpen.setAttribute('display', cat.blink ? 'none' : 'inline');
  S.eyesShut.setAttribute('display', cat.blink ? 'inline' : 'none');
}

// ---- actions ---------------------------------------------------------------
const act = (ms, step, done) => ({ ms, step, done });
function pop(toCurl, home) {
  let swapped = false;
  return act(240, (u) => {
    cat.sq = Math.sin(u * Math.PI) * 0.28;
    if (!swapped && u > 0.5) {
      swapped = true;
      cat.curled = toCurl;
      if (toCurl) { cat.blink = true; paintCurl(home); }
      cat.home = toCurl && home;
      // back in the seal it must lie exactly like the carving
      if (cat.home) cat.dir = 1;
      ghost.style.opacity = cat.home ? 0 : '';
    }
  });
}
const crouch = () => act(150, (u) => { cat.sq = ease(u) * 0.22; cat.legs = 0; cat.blink = false; });
function fly(to) {
  const fx = cat.x, fy = cat.y, fk = cat.k;
  const d = Math.hypot(to.x - fx, to.y - fy);
  const hgt = clamp(50 + d * 0.22, 40, 220) * (to.y < fy ? 1.25 : 1);
  if (Math.abs(to.x - fx) > 4) cat.dir = to.x > fx ? 1 : -1;
  return act(clamp(420 + d * 0.32, 420, 1100), (u) => {
    const e = u;
    cat.x = lerp(fx, to.x, e);
    cat.y = lerp(fy, to.y, e) - hgt * 4 * e * (1 - e);
    cat.k = lerp(fk, to.k, ease(u));
    cat.sq = -0.14 * Math.sin(u * Math.PI);
    const vy = (to.y - fy) - hgt * 4 * (1 - 2 * e), vx = Math.abs(to.x - fx) + 1;
    cat.rot = clamp(Math.atan2(vy, vx) * 0.35, -0.5, 0.5) * cat.dir;
    cat.legs = u < 0.75 ? 1 : lerp(1, -1, (u - 0.75) / 0.25);
  });
}
const land = () => act(260, (u) => { cat.sq = (1 - back(u)) * -0.6 + (1 - u) * 0.25; cat.rot *= 0.8; cat.legs = lerp(-1, 0, u); });
const idle = (ms, step = () => {}) => act(ms, step);
const look = () => act(700, (u) => { cat.look = Math.sin(u * TAU) * 0.18; cat.blink = u > 0.45 && u < 0.55; });

function travel(node) {
  const to = spot(node);
  cat.perch = node;
  const out = [];
  if (cat.curled) out.push(pop(false), idle(260, (u) => { cat.blink = u < 0.5; }));
  out.push(crouch(), fly(to), land(), look(), pop(true, to.home));
  return out;
}

function hopInPlace() {
  if (current) return;
  const here = { x: cat.x, y: cat.y, k: cat.k };
  const wasHome = cat.home;
  queue = [...(cat.curled ? [pop(false)] : []), crouch(), fly(here), land(), look(), pop(true, wasHome)];
}

function pick() {
  // the perch nearest the upper middle of the screen
  const mid = innerHeight * 0.42;
  let best = null, bd = Infinity;
  for (const p of perches()) {
    const r = p.getBoundingClientRect();
    if (r.bottom < 40 || r.top > innerHeight - 40) continue;
    const d = Math.abs(r.top - mid);
    if (d < bd) { bd = d; best = p; }
  }
  if (scrollY < 40) best = sealSvg;
  return best;
}

function tick(now) {
  if (!current && queue.length) { current = queue.shift(); current.t0 = now; }
  if (current) {
    const u = clamp((now - current.t0) / current.ms, 0, 1);
    current.step(u);
    if (u >= 1) { current.done?.(); current = null; }
  }
  if (!current && !queue.length) {
    // keep resting cats glued to their perch through layout changes
    if (cat.perch) { const s = spot(cat.perch); cat.x = s.x; cat.y = s.y; cat.k = s.k; }
    const p = pick();
    if (p && p !== cat.perch) queue = travel(p);
  }
  render(now);
  requestAnimationFrame(tick);
}

// ---- start -----------------------------------------------------------------
cat.perch = sealSvg;
Object.assign(cat, spot(sealSvg));
paintCurl(true);
ghost.style.opacity = 0;
ghost.style.transition = 'opacity .3s';
svg.addEventListener('click', hopInPlace);
if (reduce) {
  // no hopping: the cat stays asleep in the seal
  render(performance.now());
  addEventListener('resize', () => { Object.assign(cat, spot(sealSvg)); render(performance.now()); });
} else requestAnimationFrame(tick);

window.zenSeal = { cat, render, pause: () => { queue = []; current = { ms: 1e9, step() {}, t0: performance.now() }; } };
