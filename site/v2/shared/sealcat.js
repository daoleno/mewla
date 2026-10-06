// The seal cat: asleep inside a vermilion seal; it hops out and follows you
// down the page as a small red cat, curls up to sleep wherever it lands, and
// hops home when you scroll back to the top. Perches are [data-perch]
// elements: "seal" for home, or a number for the spot along the top edge.

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
export function startCat(sealSvg) {
  const ghost = sealSvg.querySelector('.ghost');
  const homeCat = sealSvg.querySelector('.home');
  const svg = el('svg', { class: 'cat', viewBox: '-70 -100 140 110', 'aria-hidden': 'true' }, document.body);
  const root = el('g', { filter: inkpad(svg, 'pad-cat', 1.8) }, svg);
  // only the painted cat takes taps; its box must not cover links below it
  svg.style.pointerEvents = 'none';
  root.style.pointerEvents = 'visiblePainted';
  const curlG = el('g', {}, root);
  const curlInner = el('g', {}, curlG);
  const standG = el('g', { display: 'none' }, root);

  // At home the carving itself sleeps; the live cat only shows while away.
  function atHome(yes) {
    homeCat.setAttribute('opacity', yes ? 1 : 0);
    ghost.style.opacity = yes ? 0 : '';
    svg.style.visibility = yes ? 'hidden' : 'visible';
  }

  function paintCurl(home) {
    curlInner.replaceChildren();
    drawCurl(curlInner, home ? PAPER : RED, home ? RED : PAPER);
    svg.style.setProperty('--zz', home ? PAPER : RED);
  }
  for (let i = 0; i < 3; i++) {
    const z = el('text', { class: 'zz', x: -10 + i * 2, y: -60 }, curlG);
    z.textContent = 'z';
  }

  // standing rig, facing right: red with paper-white carved lines
  const S = {};
  const carve = (d, w, parent) => el('path', { d, fill: 'none', stroke: PAPER, 'stroke-width': w, 'stroke-linecap': 'round' }, parent);
  S.legsFar = [0, 1].map(() => el('line', { 'stroke-linecap': 'round', 'stroke-width': 8, stroke: RED2 }, standG));
  S.tail = el('path', { fill: 'none', 'stroke-linecap': 'round', 'stroke-width': 8, stroke: RED }, standG);
  S.rings = el('path', { fill: 'none', 'stroke-width': 8.4, stroke: PAPER, 'stroke-dasharray': '1.3 6.2', 'stroke-dashoffset': -7 }, standG);
  S.body = el('ellipse', { cx: 0, cy: -22, rx: 24, ry: 14, fill: RED }, standG);
  S.legsNear = [0, 1].map(() => el('line', { 'stroke-linecap': 'round', 'stroke-width': 8.5, stroke: RED }, standG));
  S.marks = el('g', {}, standG);
  for (const d of ['M -7 -35.4 q 1.6 4 0.6 8', 'M 1 -36 q 1.6 4 0.6 8', 'M 9 -35.2 q 1.3 3.6 0.3 7']) carve(d, 1.9, S.marks);
  carve('M -15 -13 C -18 -21 -12 -28 -3 -26', 1.6, S.marks);
  S.head = el('g', {}, standG);
  S.earL = el('path', { d: 'M -12.5 -6 L -11 -20 L -2 -12 Z', 'stroke-linejoin': 'round', 'stroke-width': 4, fill: RED, stroke: RED }, S.head);
  S.earR = el('path', { d: 'M 2 -13 L 11 -20 L 12.5 -6 Z', 'stroke-linejoin': 'round', 'stroke-width': 4, fill: RED, stroke: RED }, S.head);
  S.skull = el('ellipse', { cx: 0, cy: 0, rx: 16.5, ry: 14.5, fill: RED }, S.head);
  carve('M -9.6 -9.5 L -9.6 -15.5', 1.1, S.head);
  carve('M 9.4 -9.8 L 9 -15.6', 1.1, S.head);
  for (const d of ['M 13 3 L 18.6 2.2', 'M 13 5.6 L 18.4 6.6', 'M -6 3.4 L -10.6 2.6']) carve(d, 0.9, S.head);
  S.eyesOpen = el('g', { fill: PAPER }, S.head);
  el('ellipse', { cx: -2, cy: -1, rx: 1.9, ry: 2.4 }, S.eyesOpen);
  el('ellipse', { cx: 9, cy: -1, rx: 1.9, ry: 2.4 }, S.eyesOpen);
  S.eyesShut = el('g', { fill: 'none', 'stroke-width': 1.8, 'stroke-linecap': 'round', stroke: PAPER }, S.head);
  el('path', { d: 'M -5 -1 Q -2 1.6 1 -1' }, S.eyesShut);
  el('path', { d: 'M 6 -1 Q 9 1.6 12 -1' }, S.eyesShut);
  S.nose = el('path', { d: 'M 2.6 3.2 L 5.8 3.2 L 4.2 5.2 Z', fill: PAPER, stroke: PAPER, 'stroke-width': 0.7, 'stroke-linejoin': 'round' }, S.head);
  carve('M 4.2 5.2 Q 2.8 7.4 1.2 6.4', 0.9, S.head);
  carve('M 4.2 5.2 Q 5.6 7.4 7.2 6.4', 0.9, S.head);

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
      const k = (r.width / 100) * SEAL_AT[2];
      return { x: r.left + sx + (r.width * SEAL_AT[0]) / 100, y: r.top + sy + (r.height * SEAL_AT[1]) / 100, k, home: true };
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
    const tail = `M -20 -26 C -34 -30 ${-40 + wave * 0.3} ${-44 - L * 4} ${-30 + wave} ${-54 - L * 6}`;
    S.tail.setAttribute('d', tail);
    S.rings.setAttribute('d', tail);
    S.marks.setAttribute('transform', `translate(0 ${(-22 * (cat.breathe - 1)).toFixed(2)})`);
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
      // leaving home: the live cat takes over from the carving straight away
      if (!toCurl && cat.home && svg.style.visibility === 'hidden') {
        svg.style.visibility = 'visible';
        homeCat.setAttribute('opacity', 0);
      }
      cat.sq = Math.sin(u * Math.PI) * 0.28;
      if (!swapped && u > 0.5) {
        swapped = true;
        cat.curled = toCurl;
        if (toCurl) { cat.blink = true; paintCurl(home); }
        cat.home = toCurl && home;
        // back in the seal it must lie exactly like the carving
        if (cat.home) cat.dir = 1;
        atHome(cat.home);
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
  ghost.style.transition = 'opacity .3s';
  atHome(true);
  root.addEventListener('click', hopInPlace);
  if (reduce) {
    // no hopping: the cat stays asleep in the seal
    render(performance.now());
    addEventListener('resize', () => { Object.assign(cat, spot(sealSvg)); render(performance.now()); });
  } else requestAnimationFrame(tick);

  return { cat, render, pause: () => { queue = []; current = { ms: 1e9, step() {}, t0: performance.now() }; } };
}
