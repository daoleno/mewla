// Brain, the seal cat: asleep inside a vermilion seal; it hops out and follows
// you down the page as a small red cat, acts out each story beat it lands on
// (data-act), naps when left alone and hops home at the top. Visitors can tap,
// pet, pick it up and drop it anywhere, or dangle a yarn ball for it to chase.
// Perches are [data-perch] elements: "seal" for home, or a number for the spot
// along the top edge.

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
// Units: origin at the cat's feet; head on the left, tail wrapped in front.
const CURL = {
  tail: 'M 41 -12 C 46 4 24 7.5 4 5.5 C -6 4.6 -14 4 -20 4.6 C -28 5.2 -34 3 -36 -3',
  body: 'M -14 2 C -26 1 -30 -12 -22 -24 C -14 -38 2 -47 18 -45 C 34 -43 44 -30 43 -16 C 42 -4 34 2 22 2 Z',
  earL: 'M -35.5 -30 C -37.5 -38 -38 -46 -36 -53 C -30 -49 -25.5 -45 -22.5 -40.5 Z',
  earR: 'M -14.5 -41 C -10 -46 -5.5 -50 0 -52 C 1 -46 0.5 -39 -2 -33 Z',
  head: [-19, -25, 18.5, 16, -6],
  paw: 'M -31 -10 C -34 -4 -29 0 -21 0 C -15 0 -12 -3 -14 -7 C -17 -10 -26 -11 -31 -10 Z',
  nose: 'M -21.4 -21.6 L -17.8 -21.6 L -19.6 -19.5 Z',
  // knife lines, by width
  lines: [
    'M -2.2 -38 C 3 -30 3 -18 -3 -10', // head against body
    'M 36 -5 C 33 -16 22 -20 13 -13', // haunch
    'M 38.4 -8 C 40 2 24 3.2 6 1.2', // tail against body
    'M -30 -26.5 Q -27 -23.6 -24 -26.5', 'M -15.5 -27 Q -12.5 -24.1 -9.5 -27', // eyes
  ],
  fine: [
    'M -19.6 -19.6 Q -21.3 -17.2 -23.2 -18.4', 'M -19.6 -19.6 Q -17.9 -17.2 -16 -18.4', // mouth
    'M -33.6 -36 C -34 -41 -34 -45 -33.2 -48', 'M -10.4 -42 C -7.4 -45 -4.8 -47 -2.4 -48.2', // inside the ears
    'M -32.2 -20.4 L -36.2 -21.2', 'M -32.2 -18.2 L -36 -17.4', 'M -7 -21.2 L -3 -22.2', 'M -7 -19 L -3.2 -18.4', // whiskers
    'M -30 -9.4 C -24 -11.2 -18 -10.2 -14.6 -7.6', // paw under the chin
    'M -25 -3.8 L -24.6 -0.8', 'M -20.6 -3.6 L -20.2 -0.6', // toes
  ],
  stripes: [
    'M 9 -43.4 C 11.2 -40 11.8 -37 11.2 -33.8', 'M 18.8 -44 C 20.6 -40.6 20.8 -37.4 19.8 -34.4', 'M 28.6 -41.4 C 29.8 -38.4 29.8 -35.6 28.8 -33', // back
  ],
};

function drawCurl(g, fill, line, { detail = true } = {}) {
  const stroke = (d, w) => el('path', { d, fill: 'none', stroke: line, 'stroke-width': w, 'stroke-linecap': 'round', 'stroke-linejoin': 'round' }, g);
  el('path', { d: CURL.tail, fill: 'none', stroke: fill, 'stroke-width': 9, 'stroke-linecap': 'round' }, g);
  for (const k of ['body', 'earL', 'earR']) el('path', { d: CURL[k], fill }, g);
  const [cx, cy, rx, ry, rot] = CURL.head;
  el('ellipse', { cx, cy, rx, ry, transform: `rotate(${rot} ${cx} ${cy})`, fill }, g);
  el('path', { d: CURL.paw, fill }, g);
  for (const d of CURL.lines) stroke(d, 1.55);
  el('path', { d: CURL.nose, fill: line, stroke: line, 'stroke-width': 0.8, 'stroke-linejoin': 'round' }, g);
  if (!detail) return;
  for (const d of CURL.fine) stroke(d, 0.9);
  for (const d of CURL.stripes) stroke(d, 2);
}

// ---- seal paste ------------------------------------------------------------
// A stamped look for any group: knife-rough edges, paper specks where the
// paste missed, paler clouds where it was thin, and a faint bleed. u is the
// rendered pixels per user unit, so the texture keeps its size on screen.
function inkpad(svg, id, u) {
  const defs = el('defs', {}, svg);
  const f = el('filter', { id, x: '-12%', y: '-12%', width: '124%', height: '124%', 'color-interpolation-filters': 'sRGB' }, defs);
  const n = (v) => v.toFixed(4);
  el('feTurbulence', { type: 'fractalNoise', baseFrequency: n(u / 40), numOctaves: 2, seed: 2, result: 'warp' }, f);
  el('feDisplacementMap', { in: 'SourceGraphic', in2: 'warp', scale: n(1.3 / u), xChannelSelector: 'R', yChannelSelector: 'G', result: 'rough' }, f);
  el('feTurbulence', { type: 'fractalNoise', baseFrequency: n(Math.min(1, u / 3.4)), numOctaves: 2, seed: 5, result: 'grain' }, f);
  el('feColorMatrix', { in: 'grain', type: 'matrix', values: '0 0 0 0 0  0 0 0 0 0  0 0 0 0 0  16 0 0 0 -11.4', result: 'holes' }, f);
  el('feComposite', { in: 'rough', in2: 'holes', operator: 'out', result: 'speck' }, f);
  el('feTurbulence', { type: 'fractalNoise', baseFrequency: n(u / 46), numOctaves: 2, seed: 8, result: 'cloud' }, f);
  el('feColorMatrix', { in: 'cloud', type: 'matrix', values: '0 0 0 0 0.99  0 0 0 0 0.95  0 0 0 0 0.9  0.75 0 0 0 -0.3', result: 'pale' }, f);
  el('feComposite', { in: 'pale', in2: 'speck', operator: 'atop', result: 'mottled' }, f);
  el('feGaussianBlur', { in: 'speck', stdDeviation: n(1.1 / u), result: 'blur' }, f);
  el('feColorMatrix', { in: 'blur', type: 'matrix', values: '1 0 0 0 0  0 1 0 0 0  0 0 1 0 0  0 0 0 0.12 0', result: 'bleed' }, f);
  const m = el('feMerge', {}, f);
  el('feMergeNode', { in: 'bleed' }, m);
  el('feMergeNode', { in: 'mottled' }, m);
  return `url(#${id})`;
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
const SEAL_AT = [44.6, 77, 0.9];
const CAT_IN_SEAL = `translate(${SEAL_AT[0]} ${SEAL_AT[1]}) scale(${SEAL_AT[2]})`;

export function buildSeal(svg, { ghost = false, cat = true, detail = true } = {}) {
  const u = Math.max(0.25, svg.getBoundingClientRect().width / 100);
  const g = el('g', { filter: inkpad(svg, `pad-${svg.dataset.seal}`, u) }, svg);
  // the block: square, corners slightly worn
  const R = rng(9), pts = [];
  for (let k = 0; k < 4; k++) {
    for (let j = 0; j < 14; j++) {
      const t = j / 14, wob = (R() - 0.5) * 0.9;
      const c = Math.min(t, 1 - t) < 0.04 ? 1.2 : 0;
      pts.push([[4 + 92 * t, 4 + c + wob], [96 - c + wob, 4 + 92 * t], [96 - 92 * t, 96 - c + wob], [4 + c + wob, 96 - 92 * t]][k]);
    }
  }
  el('path', { d: 'M ' + pts.map((p) => p.map((v) => v.toFixed(2)).join(' ')).join(' L ') + ' Z', fill: RED }, g);
  if (detail) {
    // a crescent moon over the sleeper, and a few chips knocked off the edge
    el('circle', { cx: 79, cy: 19, r: 7.4, fill: PAPER }, g);
    el('circle', { cx: 82.6, cy: 16.4, r: 6.4, fill: RED }, g);
    for (const d of ['M 4 31 L 6.2 32.4 L 5.4 35 L 4 35.6 Z', 'M 63 96 L 64.6 93.6 L 67.4 94.4 L 68 96 Z', 'M 96 70 L 94.4 71.2 L 94.8 73.4 L 96 73.8 Z', 'M 4 4 L 8.2 4 L 6 5.8 L 4 8 Z']) el('path', { d, fill: 'var(--bg)' }, g);
  }
  if (ghost) {
    // the empty bed the cat leaves behind
    const gh = el('g', { transform: CAT_IN_SEAL, opacity: 0.22, class: 'ghost' }, g);
    drawCurl(gh, PAPER, RED, { detail });
  }
  if (cat) {
    const home = el('g', { class: 'home', opacity: ghost ? 0 : 1 }, el('g', { transform: CAT_IN_SEAL }, g));
    drawCurl(home, PAPER, RED, { detail });
    if (ghost) {
      for (let i = 0; i < 3; i++) el('text', { class: 'zz', x: -40 + i * 3, y: -52 }, home).textContent = 'z';
    }
  }
}

// ---- the live cat ----------------------------------------------------------
// The cat is Brain. It sleeps in the seal, wakes when you scroll, hops to the
// part of the page you are reading, and plays: tap it, pet it (rub it with the
// mouse, or stroke it with a finger), pick it up (drag with the mouse, or
// long-press on touch) and drop it anywhere, or dangle a yarn ball for it.
// Perches may carry data-act to give the cat a job when it lands there.

// One standing rig, many poses. Units: feet on y = 0, facing right. Leg
// angles are radians from straight down, positive is forward. Tail angles are
// degrees: 0 forward, 90 down, 180 back, 270 up.
const BASE = {
  bx: 0, by: -22, brx: 24, bry: 14, brot: 0,
  hx: 23, hy: -36, hrot: 0,
  fn: -0.05, ff: 0.15, rn: 0.12, rf: -0.1, lenF: 14, lenR: 13, ground: 1,
  fold: 0, tuck: 0, paw: 0,
  ta: 215, tc: 40, tl: 34, ears: 0,
  eyes: 'open', mouth: 'none',
};
const POSES = {
  stand: {},
  sit: { bx: -3, by: -25, brx: 20, bry: 15, brot: -48, hx: 9, hy: -52, fn: 0.02, ff: 0.1, fold: 1, ta: 176, tc: 75, tl: 33 },
  loaf: { by: -14, brx: 25, bry: 13, hx: 22, hy: -26, fold: 1, tuck: 1, ta: 178, tc: 55, tl: 33, eyes: 'happy' },
  crouch: { by: -15, bry: 12, hx: 24, hy: -26, ears: 0.3, ta: 190, tc: 20 },
  fly: { by: -24, brot: -6, fn: 1.1, ff: 0.9, rn: -1.0, rf: -1.2, ground: 0, ta: 200, tc: 30 },
  reach: { by: -24, fn: 0.5, ff: 0.4, rn: -0.5, rf: -0.6, ground: 0, lenF: 15, lenR: 14 },
  stretch: { bx: -2, by: -20, brot: 16, hx: 26, hy: -16, fn: 1.05, ff: 0.95, lenF: 16, ta: 260, tc: 30, eyes: 'shut' },
  dangle: { bx: 0, by: -36, brx: 15, bry: 20, brot: -84, hx: 2, hy: -66, hrot: 0, fn: 0.7, ff: 0.5, rn: 0.15, rf: -0.15, lenF: 13, lenR: 15, ground: 0, ta: 100, tc: 10, tl: 30, ears: 0.6, eyes: 'wide' },
  paw: { bx: -3, by: -25, brx: 20, bry: 15, brot: -48, hx: 9, hy: -52, ff: 0.1, fold: 1, paw: 1, ta: 176, tc: 75, tl: 33 },
  groom: { bx: -3, by: -25, brx: 20, bry: 15, brot: -48, hx: 11, hy: -47, hrot: 18, ff: 0.1, fold: 1, paw: 0.75, ta: 176, tc: 75, tl: 33, eyes: 'shut' },
  yawn: { bx: -3, by: -25, brx: 20, bry: 15, brot: -48, hx: 8, hy: -54, hrot: -14, fold: 1, ta: 176, tc: 75, tl: 33, eyes: 'shut', mouth: 'open' },
  alert: { by: -26, bry: 14, hx: 24, hy: -42, lenF: 17, lenR: 16, ears: 0, ta: 268, tc: 6, tl: 36, eyes: 'wide' },
};
const pose = (name, extra) => ({ ...BASE, ...POSES[name], ...extra });

export function startCat(sealSvg) {
  const ghost = sealSvg.querySelector('.ghost');
  const homeCat = sealSvg.querySelector('.home');
  document.documentElement.classList.add('cat-live');
  const svg = el('svg', { class: 'cat', viewBox: '-70 -100 140 110', 'aria-hidden': 'true' }, document.body);
  const root = el('g', { filter: inkpad(svg, 'pad-cat', 1.8) }, svg);
  const curlG = el('g', {}, root);
  const curlInner = el('g', {}, curlG);
  const standG = el('g', { display: 'none' }, root);
  // a generous invisible target, so a small cat is still easy to touch
  const hit = el('rect', { class: 'hit', x: -48, y: -82, width: 98, height: 90, fill: 'transparent' }, svg);
  // hearts, marks and the yarn ball live in page coordinates, outside the paste filter
  const fx = el('svg', { class: 'cat-fx', width: 1, height: 1, 'aria-hidden': 'true' }, document.body);
  svg.style.pointerEvents = 'none';
  Object.assign(hit.style, { pointerEvents: reduce ? 'none' : 'all', touchAction: 'none', cursor: 'grab' });

  // At home the carving itself sleeps; the live cat only shows while away.
  function atHome(yes) {
    homeCat.setAttribute('opacity', yes ? 1 : 0);
    ghost.style.opacity = yes ? 0 : '';
    svg.style.visibility = yes ? 'hidden' : 'visible';
  }

  // ---- curled rig: red cat with paper lines, one eye that can peek
  let peek;
  function paintCurl(home) {
    curlInner.replaceChildren();
    drawCurl(curlInner, home ? PAPER : RED, home ? RED : PAPER);
    peek = el('g', { display: 'none' }, curlInner);
    el('ellipse', { cx: -12.5, cy: -26, rx: 3.8, ry: 2.6, fill: home ? PAPER : RED }, peek);
    el('ellipse', { cx: -12.5, cy: -26, rx: 1.7, ry: 2.1, fill: home ? RED : PAPER }, peek);
    svg.style.setProperty('--zz', home ? PAPER : RED);
  }
  const zs = el('g', {}, curlG);
  for (let i = 0; i < 3; i++) el('text', { class: 'zz', x: -10 + i * 2, y: -60 }, zs).textContent = 'z';

  // ---- standing rig, facing right: red with paper-white carved lines
  const S = {};
  const carve = (d, w, parent) => el('path', { d, fill: 'none', stroke: PAPER, 'stroke-width': w, 'stroke-linecap': 'round' }, parent);
  const leg = (w, c) => el('line', { 'stroke-linecap': 'round', 'stroke-width': w, stroke: c }, standG);
  S.legsFar = [leg(8, RED2), leg(8, RED2)];
  S.tail = el('path', { fill: 'none', 'stroke-linecap': 'round', 'stroke-width': 8, stroke: RED }, standG);
  S.rings = el('path', { fill: 'none', 'stroke-width': 8.4, stroke: PAPER, 'stroke-dasharray': '1.3 6.2', 'stroke-dashoffset': -9 }, standG);
  S.torso = el('g', {}, standG);
  S.haunch = el('ellipse', { cx: -9, cy: 2, rx: 0, ry: 0, fill: RED }, S.torso);
  S.body = el('ellipse', { cx: 0, cy: 0, rx: 24, ry: 14, fill: RED }, S.torso);
  S.marks = el('g', {}, S.torso);
  for (const d of ['M -7 -13.4 q 1.6 4 0.6 8', 'M 1 -14 q 1.6 4 0.6 8', 'M 9 -13.2 q 1.3 3.6 0.3 7']) carve(d, 1.9, S.marks);
  S.haunchLine = carve('M -15 9 C -18 1 -12 -6 -3 -4', 1.6, S.torso);
  S.rearPaws = el('g', {}, standG);
  el('ellipse', { cx: 0, cy: -3, rx: 6.4, ry: 3.4, fill: RED }, S.rearPaws);
  S.legsNear = [leg(8.5, RED), leg(8.5, RED)];
  S.frontPaws = el('g', {}, standG);
  el('ellipse', { cx: 0, cy: -3, rx: 6, ry: 3.4, fill: RED }, S.frontPaws);
  carve('M -1 -4.4 L -1 -1.8 M 2 -4.4 L 2 -1.8', 0.8, S.frontPaws);
  S.head = el('g', {}, standG);
  S.earL = el('path', { d: 'M -13 -5 C -14.4 -12 -13.4 -18 -10.4 -20.4 C -6.4 -18.4 -3.4 -14.6 -2 -11 Z', 'stroke-linejoin': 'round', 'stroke-width': 3, fill: RED, stroke: RED }, S.head);
  S.earR = el('path', { d: 'M 2 -11.6 C 4 -15.6 7.4 -18.6 11.4 -20.4 C 13.8 -17 14 -11 12.8 -5 Z', 'stroke-linejoin': 'round', 'stroke-width': 3, fill: RED, stroke: RED }, S.head);
  S.skull = el('ellipse', { cx: 0, cy: 0, rx: 16.5, ry: 14.5, fill: RED }, S.head);
  S.earLines = el('g', {}, S.head);
  carve('M -9.6 -9.5 L -9.8 -15', 1.1, S.earLines);
  carve('M 9.2 -9.8 L 9.4 -15.2', 1.1, S.earLines);
  for (const d of ['M 13 3 L 18.6 2.2', 'M 13 5.6 L 18.4 6.6', 'M -6 3.4 L -10.6 2.6']) carve(d, 0.9, S.head);
  S.eyes = {};
  S.eyes.open = el('g', { fill: PAPER }, S.head);
  el('ellipse', { cx: -2, cy: -1, rx: 1.9, ry: 2.4 }, S.eyes.open);
  el('ellipse', { cx: 9, cy: -1, rx: 1.9, ry: 2.4 }, S.eyes.open);
  S.eyes.wide = el('g', {}, S.head);
  for (const cx of [-2, 9]) {
    el('ellipse', { cx, cy: -1.2, rx: 2.7, ry: 3.1, fill: PAPER }, S.eyes.wide);
    el('circle', { cx: cx + 0.5, cy: -0.8, r: 1.2, fill: RED2, class: 'pupil' }, S.eyes.wide);
  }
  S.eyes.shut = el('g', { fill: 'none', 'stroke-width': 1.8, 'stroke-linecap': 'round', stroke: PAPER }, S.head);
  el('path', { d: 'M -5 -1 Q -2 1.6 1 -1' }, S.eyes.shut);
  el('path', { d: 'M 6 -1 Q 9 1.6 12 -1' }, S.eyes.shut);
  S.eyes.happy = el('g', { fill: 'none', 'stroke-width': 1.8, 'stroke-linecap': 'round', stroke: PAPER }, S.head);
  el('path', { d: 'M -5 0 Q -2 -2.8 1 0' }, S.eyes.happy);
  el('path', { d: 'M 6 0 Q 9 -2.8 12 0' }, S.eyes.happy);
  S.nose = el('path', { d: 'M 2.6 3.2 L 5.8 3.2 L 4.2 5.2 Z', fill: PAPER, stroke: PAPER, 'stroke-width': 0.7, 'stroke-linejoin': 'round' }, S.head);
  S.smile = el('g', {}, S.head);
  carve('M 4.2 5.2 Q 2.8 7.4 1.2 6.4', 0.9, S.smile);
  carve('M 4.2 5.2 Q 5.6 7.4 7.2 6.4', 0.9, S.smile);
  S.yawn = el('ellipse', { cx: 4.2, cy: 8, rx: 2.4, ry: 3, fill: PAPER }, S.head);

  // ---- state -----------------------------------------------------------------
  const P = pose('stand');
  const cat = {
    x: 0, y: 0, k: 1, dir: 1, curled: true, home: true, perch: null, frac: null,
    sq: 0, rot: 0, breathe: 1, walk: 0, walking: false, blink: false,
    purr: 0, dragging: false, lockUntil: 0, restSince: 0, fidgeted: false,
  };
  const mouse = { x: -1e4, y: -1e4, cx: -1e4, cy: -1e4, t: 0 };
  const toy = { on: false, x: 0, y: 0, vx: 0, vy: 0, ax: 0, ay: 0, auto: true, swatAt: 0 };
  let queue = [], current = null, last = performance.now();

  const act = (ms, step, init) => ({ ms, step, init });
  function run(list) { queue = list; current = null; }
  function blend(from, to, u) {
    for (const key in to) {
      const a = from[key], b = to[key];
      P[key] = typeof b === 'number' ? lerp(a, b, u) : (u < 0.5 ? a : b);
    }
  }
  function toPose(name, ms = 260, extra) {
    let from;
    const to = pose(name, extra);
    return act(ms, (u) => blend(from, to, ease(u)), () => { from = { ...P }; });
  }
  const idle = (ms, step = () => {}) => act(ms, step);
  const call = (fn) => act(1, () => {}, fn);

  // ---- where things are ------------------------------------------------------
  const perches = () => [...document.querySelectorAll('[data-perch]')];
  function spot(node) {
    const r = node.getBoundingClientRect(), sx = scrollX, sy = scrollY;
    if (node === sealSvg) {
      if (cat.porch) {
        // awake at home: sitting on the seal's top edge, under the moon
        return { x: r.left + sx + r.width * (cat.frac ?? 0.6), y: r.top + sy + 1, k: clamp(r.width / 330, 0.72, 1), home: false };
      }
      const k = (r.width / 100) * SEAL_AT[2];
      return { x: r.left + sx + (r.width * SEAL_AT[0]) / 100, y: r.top + sy + (r.height * SEAL_AT[1]) / 100, k, home: true };
    }
    const f = node === cat.perch && cat.frac != null ? cat.frac : +node.dataset.perch;
    return { x: r.left + sx + r.width * f, y: r.top + sy + 1, k: clamp(r.width / 330, 0.72, 1), home: false };
  }
  // the strip of floor the cat may walk on while playing
  function floor() {
    if (!cat.perch) return { a: scrollX + 30, b: scrollX + innerWidth - 30 };
    const r = cat.perch.getBoundingClientRect();
    const pad = 22 * cat.k;
    return { a: r.left + scrollX + pad, b: r.right + scrollX - pad };
  }
  function headAt() {
    const hx = P.hx * cat.k * cat.dir, hy = P.hy * cat.k;
    return { x: cat.x + hx, y: cat.y + hy };
  }

  // ---- drawing ---------------------------------------------------------------
  function rigPoint(lx, ly) {
    const a = P.brot * Math.PI / 180, c = Math.cos(a), s = Math.sin(a);
    return [P.bx + lx * c - ly * s, P.by + lx * s + ly * c];
  }
  function setLeg(line, at, ang, len, show) {
    const [hx, hy] = at;
    let L = len;
    if (P.ground > 0) {
      const down = Math.cos(ang);
      const reach = down > 0.2 ? clamp((-hy - 1) / down, 4, 28) : len;
      L = lerp(len, reach, P.ground);
    }
    L *= show;
    line.setAttribute('display', show < 0.12 ? 'none' : 'inline');
    line.setAttribute('x1', hx.toFixed(2)); line.setAttribute('y1', hy.toFixed(2));
    line.setAttribute('x2', (hx + Math.sin(ang) * L).toFixed(2)); line.setAttribute('y2', (hy + Math.cos(ang) * L).toFixed(2));
  }
  const dirv = (deg, l) => [Math.cos(deg * Math.PI / 180) * l, Math.sin(deg * Math.PI / 180) * l];

  function render(now) {
    const t = now / 1000;
    const w = 140 * cat.k, h = 110 * cat.k;
    const jx = cat.purr ? Math.sin(t * 90) * 0.5 * cat.purr : 0;
    svg.setAttribute('width', w.toFixed(1));
    svg.setAttribute('height', h.toFixed(1));
    svg.style.transform = `translate(${(cat.x - 70 * cat.k + jx).toFixed(1)}px, ${(cat.y - 100 * cat.k).toFixed(1)}px)`;
    const sx = (1 + cat.sq * 0.9) * cat.dir, sy = 1 - cat.sq;
    root.setAttribute('transform', `rotate(${(cat.rot * 57.3).toFixed(2)}) scale(${sx.toFixed(3)} ${sy.toFixed(3)})`);
    curlG.setAttribute('display', cat.curled ? 'inline' : 'none');
    standG.setAttribute('display', cat.curled ? 'none' : 'inline');
    if (cat.curled) {
      const b = 1 + 0.025 * Math.sin(t * TAU / (cat.purr ? 1.6 : 3.6));
      curlInner.setAttribute('transform', `scale(${(1 + (b - 1) * 0.4).toFixed(4)} ${b.toFixed(4)})`);
      const near = Math.hypot(mouse.x - cat.x, mouse.y - (cat.y - 25 * cat.k)) < 110 * cat.k && now - mouse.t < 2500;
      peek.setAttribute('display', (near || cat.purr) && !cat.home ? 'inline' : 'none');
      zs.setAttribute('display', cat.purr ? 'none' : 'inline');
      return;
    }
    // breathing, walking and tail life are layered on top of the pose
    const br = 1 + 0.03 * Math.sin(t * TAU / (cat.purr ? 1.4 : 3));
    S.torso.setAttribute('transform', `translate(${P.bx.toFixed(2)} ${P.by.toFixed(2)}) rotate(${P.brot.toFixed(1)}) scale(1 ${br.toFixed(3)})`);
    S.body.setAttribute('rx', P.brx.toFixed(2));
    S.body.setAttribute('ry', P.bry.toFixed(2));
    S.haunch.setAttribute('rx', (13 * P.fold).toFixed(2));
    S.haunch.setAttribute('ry', (12 * P.fold).toFixed(2));
    S.haunchLine.setAttribute('opacity', (1 - P.fold).toFixed(2));
    const step = cat.walking ? Math.sin(cat.walk) * 0.55 : 0;
    const showF = 1 - P.tuck, showR = 1 - Math.max(P.fold, P.tuck);
    const dang = cat.dragging ? Math.sin(t * 11) * 0.3 : 0;
    setLeg(S.legsFar[0], rigPoint(16, 3), P.ff - step + dang, P.lenF, showF);
    setLeg(S.legsFar[1], rigPoint(-17, 3), P.rf + step - dang, P.lenR, showR);
    setLeg(S.legsNear[1], rigPoint(-14, 5), P.rn - step + dang, P.lenR, showR);
    const pawAng = lerp(P.fn + step - dang, 2.25, P.paw);
    setLeg(S.legsNear[0], rigPoint(13, 5), pawAng, lerp(P.lenF, 11, P.paw), showF);
    // folded hind paws in front of the haunch; tucked fore paws under the chest
    const hp = rigPoint(-6, 10);
    S.rearPaws.setAttribute('transform', `translate(${(hp[0] + 9).toFixed(2)} 0)`);
    S.rearPaws.setAttribute('opacity', P.fold.toFixed(2));
    S.frontPaws.setAttribute('transform', `translate(${(P.bx + 19).toFixed(2)} 0)`);
    S.frontPaws.setAttribute('opacity', P.tuck.toFixed(2));
    // tail
    const swish = Math.sin(t * (toy.on ? 6 : 2.6)) * (toy.on ? 14 : 6) * (1 - P.fold * 0.6);
    const A = rigPoint(-21, -3);
    const th = P.ta + swish * 0.4, cu = P.tc + swish;
    const p1 = dirv(th, P.tl * 0.42), p2 = dirv(th + cu * 0.45, P.tl * 0.78), p3 = dirv(th + cu, P.tl * 0.36);
    const d = `M ${A[0].toFixed(2)} ${A[1].toFixed(2)} C ${(A[0] + p1[0]).toFixed(2)} ${(A[1] + p1[1]).toFixed(2)} ${(A[0] + p2[0]).toFixed(2)} ${(A[1] + p2[1]).toFixed(2)} ${(A[0] + p2[0] + p3[0]).toFixed(2)} ${(A[1] + p2[1] + p3[1]).toFixed(2)}`;
    S.tail.setAttribute('d', d);
    S.rings.setAttribute('d', d);
    // head follows the pointer (or the yarn) a little
    let look = 0, ex = 0, ey = 0;
    const target = toy.on ? { x: toy.x, y: toy.y } : (now - mouse.t < 4000 ? mouse : null);
    if (target && !cat.dragging) {
      const hd = headAt();
      const dx = (target.x - hd.x) * cat.dir, dy = target.y - hd.y;
      look = clamp(Math.atan2(dy, Math.abs(dx) + 40) * 0.6, -0.4, 0.4);
      ex = clamp(dx / 80, -1, 1.2); ey = clamp(dy / 80, -1, 1);
    }
    S.head.setAttribute('transform', `translate(${P.hx.toFixed(2)} ${P.hy.toFixed(2)}) rotate(${(P.hrot + look * 57.3).toFixed(1)})`);
    const flat = P.ears * 38;
    S.earL.setAttribute('transform', `rotate(${(-flat).toFixed(1)} -8 -9)`);
    S.earR.setAttribute('transform', `rotate(${flat.toFixed(1)} 7 -9)`);
    S.earLines.setAttribute('opacity', (1 - P.ears).toFixed(2));
    const eyes = cat.blink && (P.eyes === 'open' || P.eyes === 'wide') ? 'shut' : P.eyes;
    for (const [k, g] of Object.entries(S.eyes)) g.setAttribute('display', k === eyes ? 'inline' : 'none');
    S.eyes.open.setAttribute('transform', `translate(${ex.toFixed(2)} ${ey.toFixed(2)})`);
    for (const p of S.eyes.wide.querySelectorAll('.pupil')) p.setAttribute('transform', `translate(${ex.toFixed(2)} ${ey.toFixed(2)})`);
    S.smile.setAttribute('display', P.mouth === 'open' ? 'none' : 'inline');
    S.yawn.setAttribute('display', P.mouth === 'open' ? 'inline' : 'none');
  }

  // ---- effects ---------------------------------------------------------------
  const sparks = [];
  function spark(kind, x, y) {
    const g = el('g', {}, fx);
    if (kind === 'heart') el('path', { d: 'M 0 4 C -6 -1 -5 -7 0 -3.6 C 5 -7 6 -1 0 4 Z', fill: RED }, g);
    else if (kind === 'bang') el('text', { 'text-anchor': 'middle', fill: RED, 'font-weight': 800, 'font-size': 18 }, g).textContent = '!';
    else el('rect', { x: -7, y: -7, width: 14, height: 14, rx: 2, fill: 'none', stroke: RED, 'stroke-width': 2 }, g);
    sparks.push({ g, kind, x, y, t0: performance.now(), dx: (Math.random() - 0.5) * 18 });
  }
  function drawSparks(now) {
    for (let i = sparks.length - 1; i >= 0; i--) {
      const s = sparks[i], u = (now - s.t0) / (s.kind === 'ring' ? 500 : 1100);
      if (u >= 1) { s.g.remove(); sparks.splice(i, 1); continue; }
      if (s.kind === 'ring') s.g.setAttribute('transform', `translate(${s.x} ${s.y}) scale(${1 + u * 2.4})`);
      else s.g.setAttribute('transform', `translate(${(s.x + s.dx * u).toFixed(1)} ${(s.y - 34 * u).toFixed(1)}) scale(${(0.7 + 0.5 * Math.sin(u * Math.PI)).toFixed(2)})`);
      s.g.setAttribute('opacity', (1 - u * u).toFixed(2));
    }
  }
  const yarn = el('g', { display: 'none' }, fx);
  const string = el('path', { fill: 'none', stroke: RED2, 'stroke-width': 1.2, 'stroke-linecap': 'round' }, yarn);
  const ball = el('g', {}, yarn);
  el('circle', { r: 8, fill: RED }, ball);
  el('path', { d: 'M -6 -3 Q 0 -8 6 -3 M -7 2 Q 0 -3 7 2 M -5 6 Q 0 2 5 6', fill: 'none', stroke: PAPER, 'stroke-width': 1, opacity: 0.8 }, ball);

  // ---- actions ---------------------------------------------------------------
  function pop(toCurl, home, poseName = 'sit') {
    let swapped = false;
    return act(240, (u) => {
      // leaving home: the live cat takes over from the carving straight away
      if (!toCurl && cat.home && svg.style.visibility === 'hidden') {
        svg.style.visibility = 'visible';
        homeCat.setAttribute('opacity', 0);
      }
      cat.sq = Math.sin(u * Math.PI) * 0.28;
      if (!swapped && u > 0.5) {
        swapped = true;
        cat.curled = toCurl;
        if (toCurl) paintCurl(home);
        else Object.assign(P, pose(poseName));
        cat.home = toCurl && home;
        // back in the seal it must lie exactly like the carving
        if (cat.home) { cat.dir = 1; cat.porch = false; cat.frac = null; }
        atHome(cat.home);
      }
    });
  }
  const crouch = () => [toPose('crouch', 150), idle(60, () => { cat.blink = false; })];
  function fly(getTo, { flip = false } = {}) {
    let fx0, fy0, fk, to, hgt, from;
    return act(600, (u) => {
      cat.x = lerp(fx0, to.x, u);
      cat.y = lerp(fy0, to.y, u) - hgt * 4 * u * (1 - u);
      cat.k = lerp(fk, to.k, ease(u));
      cat.sq = -0.14 * Math.sin(u * Math.PI);
      const vy = (to.y - fy0) - hgt * 4 * (1 - 2 * u), vx = Math.abs(to.x - fx0) + 1;
      cat.rot = flip ? -TAU * ease(u) * cat.dir : clamp(Math.atan2(vy, vx) * 0.35, -0.5, 0.5) * cat.dir;
      if (u < 0.7) blend(from, pose('fly'), Math.min(1, u * 5));
      else blend(pose('fly'), pose('reach'), (u - 0.7) / 0.3);
    }, function () {
      from = { ...P };
      fx0 = cat.x; fy0 = cat.y; fk = cat.k; to = getTo();
      const d = Math.hypot(to.x - fx0, to.y - fy0);
      hgt = clamp(50 + d * 0.22, flip ? 70 : 40, 220) * (to.y < fy0 ? 1.25 : 1);
      if (Math.abs(to.x - fx0) > 4) cat.dir = to.x > fx0 ? 1 : -1;
      this.ms = clamp(420 + d * 0.32, flip ? 620 : 420, 1100);
    });
  }
  const land = () => act(260, (u) => {
    cat.sq = (1 - back(u)) * -0.6 + (1 - u) * 0.25; cat.rot *= 0.8;
    blend(pose('reach'), pose('stand'), u);
  }, () => { cat.rot = 0; });
  function walkTo(getX, speed = 0.16) {
    let x0, x1;
    return act(400, (u) => {
      cat.x = lerp(x0, x1, u);
      cat.walking = u < 1;
      cat.walk += 0.32;
    }, function () {
      x0 = cat.x; x1 = getX();
      if (Math.abs(x1 - x0) > 2) cat.dir = x1 > x0 ? 1 : -1;
      this.ms = Math.max(120, Math.abs(x1 - x0) / speed);
      Object.assign(P, pose('stand'));
    });
  }
  const stopWalk = () => call(() => { cat.walking = false; });
  const lookAround = () => act(900, (u) => { P.hrot = Math.sin(u * TAU) * 14; cat.blink = u > 0.45 && u < 0.55; });
  const bang = () => call(() => { const h = headAt(); spark('bang', h.x + 6 * cat.dir, h.y - 22 * cat.k); });
  function stamp(onHit) {
    return [
      toPose('paw', 160),
      idle(90),
      act(110, (u) => { P.paw = 1 - u; cat.sq = u * 0.12; }, () => {}),
      call(() => { cat.sq = 0; spark('ring', cat.x + 14 * cat.k * cat.dir, cat.y - 2); onHit?.(); }),
      idle(160),
    ];
  }
  const sleepHere = () => [toPose('loaf', 380), toPose('yawn', 300), idle(500), pop(true, false)];
  const wakeUp = () => [pop(false, false, 'loaf'), toPose('yawn', 320), idle(420), toPose('stretch', 360), idle(380), toPose('sit', 300), lookAround()];

  // what the cat does when it lands on a perch
  function arrive(node, to) {
    if (to.home) return [pop(true, true)];
    const job = node.dataset.act;
    const targets = node.dataset.stamp ? [...node.querySelectorAll(node.dataset.stamp)] : [];
    const mark = (t) => () => { t.classList.remove('stamped'); void t.offsetWidth; t.classList.add('stamped'); };
    if (job === 'wake') return [toPose('alert', 200), bang(), idle(500), toPose('sit', 300), lookAround()];
    if (job === 'stamp') return [toPose('sit', 200), ...targets.flatMap((t) => stamp(mark(t))), toPose('sit', 200)];
    if (job === 'read') return [toPose('sit', 200), act(1800, (u) => { P.hrot = 16 + Math.sin(u * TAU * 2) * 10; }), toPose('sit', 200)];
    if (job === 'patrol') {
      const edge = (f) => () => { const r = node.getBoundingClientRect(); return r.left + scrollX + r.width * f; };
      return [walkTo(edge(0.12)), walkTo(edge(0.86)), walkTo(edge(0.5)), stopWalk(), toPose('sit', 260)];
    }
    if (job === 'perk') return [toPose('alert', 180), bang(), idle(700), toPose('sit', 300)];
    if (job === 'sleep') return sleepHere();
    return [toPose('sit', 300), lookAround()];
  }

  function travel(node) {
    cat.perch = node;
    cat.frac = null;
    cat.porch = false;
    cat.walking = false;
    const getTo = () => spot(node);
    const out = [];
    if (cat.curled) out.push(pop(false, false, 'stand'), idle(200));
    out.push(...crouch(), fly(getTo), land());
    out.push(act(1, () => {}, () => { queue.unshift(...arrive(node, spot(node))); }));
    return out;
  }

  // ---- input -----------------------------------------------------------------
  const lock = (ms) => { cat.lockUntil = performance.now() + ms; cat.restSince = performance.now(); cat.fidgeted = false; };
  let taps = [];
  function tap() {
    const now = performance.now();
    taps = taps.filter((t) => now - t < 900).concat(now);
    lock(6000);
    if (cat.home) {
      lock(10000);
      // the carving wakes and hops up onto the seal's top edge
      cat.porch = true; cat.frac = 0.62;
      run([pop(false, false, 'stand'), idle(160), ...crouch(), fly(() => spot(sealSvg)), land(), toPose('sit', 260), toPose('yawn', 300), idle(300), toPose('sit', 260), lookAround()]);
      return;
    }
    if (cat.curled) { run(wakeUp()); return; }
    const flip = taps.length >= 3;
    if (flip) taps = [];
    const here = () => ({ x: cat.x, y: cat.y, k: cat.k });
    run([...crouch(), fly(here, { flip }), land(), toPose('sit', 220), ...(flip ? [call(() => { const h = headAt(); spark('heart', h.x, h.y - 18); })] : []), lookAround()]);
  }

  let petT = 0;
  function pet(now) {
    lock(6000);
    if (!cat.purr && !cat.curled) run([toPose('loaf', 320)]);
    cat.purr = 1;
    petT = now;
    if (!pet.last || now - pet.last > 260) {
      pet.last = now;
      const h = cat.curled ? { x: cat.x - 18 * cat.k * cat.dir, y: cat.y - 50 * cat.k } : headAt();
      spark('heart', h.x + (Math.random() - 0.5) * 20, h.y - 16 * cat.k);
    }
  }

  // the x a falling or leaping cat may come down at: inside the content column
  function column(x) {
    const col = document.querySelector('main .wrap')?.getBoundingClientRect() ?? { left: 0, right: innerWidth };
    return clamp(x - scrollX, Math.max(30, col.left + 56), Math.min(innerWidth - 30, col.right - 56)) + scrollX;
  }
  // look straight down from a page point for the first solid thing to stand on
  const SOLID = 'a.btn, button, h1, h2, h3, p, li, pre, figure, aside, [data-perch], .scr, .three, .end-card';
  function surfaceUnder(x, y) {
    for (let vy = Math.max(70, y - scrollY); vy < innerHeight - 4; vy += 6) {
      const n = document.elementsFromPoint(x - scrollX, vy).find((m) => !m.closest('svg.cat, .cat-fx, .toy-btn, .bar'));
      const solid = n?.closest(SOLID);
      if (!solid || solid === sealSvg) continue;
      const r = solid.getBoundingClientRect();
      // never a ledge hidden under the header
      if (r.top > 70 && r.top + scrollY >= y - 30) return { node: solid, y: r.top + scrollY };
    }
    return null;
  }

  let drag = null;
  function pickUp(e) {
    drag = { vx: 0, vy: 0, lx: e.pageX, ly: e.pageY, lt: performance.now() };
    cat.dragging = true;
    cat.purr = 0;
    run([]);
    if (cat.curled) { cat.curled = false; if (cat.home) { cat.home = false; atHome(false); } }
    cat.porch = false;
    cat.perch = null;
    Object.assign(P, pose('dangle'));
    cat.sq = 0;
    document.documentElement.classList.add('cat-carrying');
    hold(e);
  }
  function hold(e) {
    const now = performance.now(), dt = Math.max(8, now - drag.lt);
    drag.vx = lerp(drag.vx, (e.pageX - drag.lx) / dt, 0.4);
    drag.vy = lerp(drag.vy, (e.pageY - drag.ly) / dt, 0.4);
    drag.lx = e.pageX; drag.ly = e.pageY; drag.lt = now;
    // held by the scruff, just behind the head
    cat.x = e.pageX - (P.hx - 4) * cat.k * cat.dir;
    cat.y = e.pageY - (P.hy - 10) * cat.k;
    cat.rot = clamp(-drag.vx * 0.5, -0.6, 0.6);
  }
  function drop() {
    cat.dragging = false;
    document.documentElement.classList.remove('cat-carrying');
    const vx = clamp(drag.vx, -2, 2);
    drag = null;
    // find the surface under the cat: a perch top, the seal, or the bottom of the screen
    const x = cat.x + vx * 120;
    let best = null, by = scrollY + innerHeight - 8;
    const sr = sealSvg.getBoundingClientRect();
    const intoSeal = cat.x > sr.left + scrollX && cat.x < sr.right + scrollX && cat.y > sr.top + scrollY && cat.y < sr.bottom + scrollY + 60 * cat.k;
    if (intoSeal) {
      run([fly(() => spot(sealSvg)), call(() => { cat.perch = sealSvg; cat.porch = false; }), land(), pop(true, true)]);
      lock(4000);
      return;
    }
    // a fling past the content column still comes down beside it, on something
    const cx = column(x);
    const under = surfaceUnder(cx, cat.y - 10);
    if (under) { best = under.node; by = under.y; }
    const fx0 = cx;
    let target;
    if (best) {
      const r = best.getBoundingClientRect();
      target = () => { cat.frac = (fx0 - r.left - scrollX) / r.width; return { ...spot(best), k: cat.k }; };
    } else target = () => ({ x: fx0, y: by, k: cat.k });
    cat.perch = best;
    cat.rot = 0;
    Object.assign(P, pose('fly'));
    run([act(1, () => {}, () => { cat.frac = null; }), fly(target), land(), toPose('alert', 160), act(360, (u) => { P.hrot = Math.sin(u * TAU * 2) * 12; }), toPose('sit', 240), lookAround()]);
    lock(7000);
  }

  let press = null;
  hit.addEventListener('pointerdown', (e) => {
    if (reduce) return;
    e.preventDefault();
    try { hit.setPointerCapture(e.pointerId); } catch { /* synthetic pointers can't be captured */ }
    press = { x: e.clientX, y: e.clientY, lx: e.clientX, ly: e.clientY, t: performance.now(), path: 0, type: e.pointerType, petting: false };
    if (e.pointerType !== 'mouse') press.timer = setTimeout(() => { if (press && press.path < 8) { press.lifted = true; pickUp(e); } }, 380);
  });
  hit.addEventListener('pointermove', (e) => {
    const now = performance.now();
    if (!press) {
      // a mouse rubbing over the cat counts as petting
      if (e.pointerType === 'mouse') {
        const m = pet.lx == null ? 0 : Math.min(60, Math.hypot(e.clientX - pet.lx, e.clientY - pet.ly));
        pet.lx = e.clientX; pet.ly = e.clientY;
        pet.rub = (pet.rub || 0) * Math.exp(-(now - (pet.rubT || now)) / 600) + m;
        pet.rubT = now;
        if (pet.rub > 160) pet(now);
      }
      return;
    }
    const d = Math.hypot(e.clientX - press.x, e.clientY - press.y);
    press.path += Math.hypot(e.clientX - press.lx, e.clientY - press.ly);
    press.lx = e.clientX; press.ly = e.clientY;
    if (cat.dragging) { hold(e); return; }
    if (d > 6) {
      if (press.type === 'mouse') { clearTimeout(press.timer); pickUp(e); }
      else { clearTimeout(press.timer); press.petting = true; pet(now); }
    }
  });
  const release = () => {
    if (!press) return;
    clearTimeout(press.timer);
    const quick = performance.now() - press.t < 350 && press.path < 10;
    if (cat.dragging) drop();
    else if (!press.petting && quick) tap();
    press = null;
  };
  hit.addEventListener('pointerup', release);
  hit.addEventListener('pointerleave', () => { pet.lx = null; });
  hit.addEventListener('pointercancel', release);
  // at home the carving is what you tap
  sealSvg.addEventListener('click', () => { if (cat.home && !reduce) tap(); });
  sealSvg.style.cursor = reduce ? '' : 'pointer';

  addEventListener('pointermove', (e) => {
    mouse.cx = e.clientX; mouse.cy = e.clientY;
    mouse.x = e.pageX; mouse.y = e.pageY; mouse.t = performance.now();
    if (toy.on && e.pointerType === 'mouse') { toy.ax = e.pageX; toy.ay = e.pageY; toy.auto = false; toy.t = mouse.t; }
  }, { passive: true });
  addEventListener('scroll', () => { mouse.x = mouse.cx + scrollX; mouse.y = mouse.cy + scrollY; }, { passive: true });
  // touch: tap anywhere to move the yarn there
  addEventListener('pointerdown', (e) => {
    if (!toy.on || e.pointerType === 'mouse' || e.target.closest('a, button, .cat')) return;
    toy.ax = e.pageX; toy.ay = e.pageY - 60; toy.auto = false; toy.t = performance.now();
  }, { passive: true });

  function setToy(on) {
    toy.on = on;
    yarn.setAttribute('display', on ? 'inline' : 'none');
    if (on) {
      const h = headAt();
      toy.ax = h.x + 70 * cat.dir; toy.ay = h.y - 90; toy.x = toy.ax; toy.y = toy.ay + 50;
      toy.vx = toy.vy = 0; toy.auto = true; toy.t = performance.now();
      lock(1e9);
      if (cat.home) { cat.porch = true; cat.frac = 0.5; run([pop(false, false, 'stand'), ...crouch(), fly(() => spot(sealSvg)), land(), toPose('sit', 200)]); }
      else if (cat.curled) run(wakeUp().slice(0, 2).concat(toPose('sit', 200)));
      else run([toPose('alert', 160), bang(), toPose('sit', 200)]);
    } else {
      lock(3000);
      cat.walking = false;
      if (cat.perch) { const r = cat.perch.getBoundingClientRect(); cat.frac = clamp((cat.x - r.left - scrollX) / r.width, 0, 1); }
      run([toPose('sit', 260), lookAround()]);
    }
  }

  // ---- the loop ----------------------------------------------------------------
  function play(now, dt) {
    // the yarn swings on its string under the anchor
    if (toy.auto || now - toy.t > 2600) {
      // nobody is steering: the yarn wanders just above the cat
      const h = headAt();
      toy.ax = h.x + Math.sin(now / 900) * 90 * cat.k; toy.ay = h.y - 110 * cat.k + Math.sin(now / 430) * 30 * cat.k;
    }
    const L = 56;
    toy.vx = (toy.vx + (toy.ax - toy.x) * 0.012 * dt) * 0.9;
    toy.vy = (toy.vy + (toy.ay + L - toy.y) * 0.012 * dt + 0.02 * dt) * 0.9;
    toy.x += toy.vx; toy.y += toy.vy;
    string.setAttribute('d', `M ${toy.ax.toFixed(1)} ${toy.ay.toFixed(1)} Q ${((toy.ax + toy.x) / 2 + toy.vx * 2).toFixed(1)} ${((toy.ay + toy.y) / 2).toFixed(1)} ${toy.x.toFixed(1)} ${toy.y.toFixed(1)}`);
    ball.setAttribute('transform', `translate(${toy.x.toFixed(1)} ${toy.y.toFixed(1)}) rotate(${(toy.x * 2) % 360})`);
    if (current || queue.length || cat.dragging || cat.curled) return;
    // the cat chases it along its floor, and swats when it comes close
    const f = floor();
    const want = clamp(toy.x, f.a, f.b), dx = want - cat.x;
    const up = cat.y - toy.y;
    // out of reach for a moment: leap down (or up) to whatever is under the yarn
    const steered = !toy.auto && now - toy.t < 2600;
    const away = steered && (want !== toy.x || up < -40 * cat.k || up > 240 * cat.k);
    if (!away) toy.away = 0;
    else if (!toy.away) toy.away = now;
    else if (now - toy.away > 900 && Math.abs(dx) < 30 * cat.k) {
      toy.away = 0;
      const x = column(toy.x);
      const under = surfaceUnder(x, toy.y + 20);
      if (under && under.node !== cat.perch && Math.abs(under.y - cat.y) < innerHeight) {
        const node = under.node;
        cat.walking = false;
        cat.dir = x > cat.x ? 1 : -1;
        run([...crouch(), call(() => {
          if (cat.home) { cat.home = false; atHome(false); }
          cat.porch = false; cat.perch = node;
          const r = node.getBoundingClientRect();
          cat.frac = clamp((x - r.left - scrollX) / r.width, 0, 1);
        }), fly(() => spot(node)), land(), toPose('sit', 160)]);
        return;
      }
    }
    if (Math.abs(dx) > 16 * cat.k) {
      if (!cat.walking) Object.assign(P, pose('stand'));
      cat.walking = true;
      cat.dir = dx > 0 ? 1 : -1;
      cat.x += Math.sign(dx) * Math.min(Math.abs(dx), 0.22 * dt * (Math.abs(dx) > 120 ? 1.6 : 1));
      cat.walk += 0.02 * dt;
      return;
    }
    if (cat.walking) { cat.walking = false; run([toPose('sit', 160)]); return; }
    if (now > toy.swatAt && Math.abs(toy.x - cat.x) < 60 * cat.k) {
      toy.swatAt = now + 900;
      if (up > 15 * cat.k && up < 80 * cat.k) {
        run([...stamp(() => { toy.vx += 6 * cat.dir; toy.vy -= 4; }), toPose('sit', 120)]);
      } else if (up >= 80 * cat.k && up < 190 * cat.k) {
        const here = () => ({ x: cat.x, y: cat.y, k: cat.k });
        run([...crouch(), fly(here), land(), call(() => { toy.vx += 8 * cat.dir; toy.vy -= 6; }), toPose('sit', 160)]);
      }
    }
  }

  function pick() {
    // the perch nearest the upper middle of the screen
    if (scrollY < 40) return sealSvg;
    const mid = innerHeight * 0.42;
    let best = null, bd = Infinity;
    for (const p of perches()) {
      const r = p.getBoundingClientRect();
      if (r.bottom < 40 || r.top > innerHeight - 40) continue;
      const d = Math.abs(r.top - mid);
      if (d < bd) { bd = d; best = p; }
    }
    return best;
  }

  function tick(now) {
    const dt = Math.min(50, now - last);
    last = now;
    if (cat.purr && now - petT > 700) {
      cat.purr = 0;
      if (!cat.curled && !current && !queue.length) run([toPose('sit', 300)]);
    }
    if (!current && queue.length) { current = queue.shift(); current.init?.(); current.t0 = now; }
    if (current) {
      const u = clamp((now - current.t0) / current.ms, 0, 1);
      current.step(u);
      if (u >= 1) current = null;
    }
    if (toy.on) play(now, dt);
    else if (!current && !queue.length && !cat.dragging) {
      // keep resting cats glued to their perch through layout changes
      if (cat.perch && !cat.walking) { const s = spot(cat.perch); cat.x = s.x; cat.y = s.y; cat.k = s.k; }
      if (now > cat.lockUntil && !cat.purr) {
        const p = pick();
        if (p && p !== cat.perch && !(p === sealSvg && cat.porch)) queue = travel(p);
        else if (cat.porch && p === sealSvg) { cat.porch = false; queue = [...crouch(), fly(() => spot(sealSvg)), land(), pop(true, true)]; }
        else if (!cat.curled && !cat.home) {
          // awake with nothing to do: fidget once, then curl up for a nap
          if (!cat.restSince) cat.restSince = now;
          if (!cat.fidgeted && now - cat.restSince > 3500) {
            cat.fidgeted = true;
            queue = Math.random() < 0.5
              ? [toPose('groom', 300), act(1400, (u) => { P.paw = 0.75 + Math.sin(u * TAU * 3) * 0.2; }), toPose('sit', 260)]
              : [toPose('stretch', 380), idle(500), toPose('sit', 300)];
          } else if (now - cat.restSince > 9000) {
            queue = sleepHere();
          }
        }
      }
    }
    if (current || queue.length || cat.curled) { cat.restSince = 0; cat.fidgeted = cat.fidgeted && !cat.curled; }
    // blink now and then
    cat.blink = (now % 4200) < 140;
    render(now);
    drawSparks(now);
    requestAnimationFrame(tick);
  }

  // ---- start -----------------------------------------------------------------
  cat.perch = sealSvg;
  Object.assign(cat, spot(sealSvg));
  paintCurl(true);
  ghost.style.transition = 'opacity .3s';
  atHome(true);
  if (reduce) {
    // no hopping: the cat stays asleep in the seal
    render(performance.now());
    addEventListener('resize', () => { Object.assign(cat, spot(sealSvg)); render(performance.now()); });
  } else requestAnimationFrame(tick);

  return {
    cat, P, render, setToy, toy,
    // for checking poses by hand: zenSeal.strike('sit')
    strike: (name) => { cat.curled = false; Object.assign(P, pose(name)); },
    get playing() { return toy.on; },
    pause: () => { queue = []; current = { ms: 1e9, step() {}, t0: performance.now() }; },
  };
}
