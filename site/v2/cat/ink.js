// Sleeping cats in light freehand ink. Each card pushes one guess about why the
// cat felt wrong (body too heavy, not abstract, not cute, not a logo), and shows
// the same art as a small icon and a header lockup.

const W = 400, H = 280;
const TAU = Math.PI * 2;
const clamp = (v, a, b) => Math.min(b, Math.max(a, v));
const ss = (a, b, x) => { const t = clamp((x - a) / (b - a), 0, 1); return t * t * (3 - 2 * t); };

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

function spline(pts, n = 12) {
  if (pts.length < 2) return pts.map((p) => p.slice());
  const out = [];
  for (let i = 0; i < pts.length - 1; i++) {
    const p0 = pts[Math.max(0, i - 1)], p1 = pts[i], p2 = pts[i + 1], p3 = pts[Math.min(pts.length - 1, i + 2)];
    for (let j = 0; j < n; j++) {
      const t = j / n, t2 = t * t, t3 = t2 * t;
      out.push([0, 1].map((k) => 0.5 * (2 * p1[k] + (p2[k] - p0[k]) * t + (2 * p0[k] - 5 * p1[k] + 4 * p2[k] - p3[k]) * t2 + (3 * p1[k] - p0[k] - 3 * p2[k] + p3[k]) * t3)));
    }
  }
  out.push(pts[pts.length - 1].slice());
  return out;
}

function resample(pts, step) {
  const out = [pts[0].slice()];
  let prev = pts[0], need = step;
  for (let i = 1; i < pts.length; i++) {
    const p = pts[i];
    let d = Math.hypot(p[0] - prev[0], p[1] - prev[1]);
    while (d >= need) {
      const t = need / d;
      prev = [prev[0] + (p[0] - prev[0]) * t, prev[1] + (p[1] - prev[1]) * t];
      out.push(prev);
      d -= need;
      need = step;
    }
    need -= d;
    prev = p;
  }
  if (out.length < 2) out.push(pts[pts.length - 1].slice());
  return out;
}

function normals(c) {
  return c.map((p, i) => {
    const a = c[Math.max(0, i - 1)], b = c[Math.min(c.length - 1, i + 1)];
    const dx = b[0] - a[0], dy = b[1] - a[1], l = Math.hypot(dx, dy) || 1;
    return [-dy / l, dx / l];
  });
}

function smoothNoise(R, n, amp, k = 8) {
  let a = Array.from({ length: n }, () => (R() - 0.5) * 2);
  for (let pass = 0; pass < 2; pass++) {
    a = a.map((_, i) => {
      let s = 0, m = 0;
      for (let j = -k; j <= k; j++) { const v = a[i + j]; if (v !== undefined) { s += v; m++; } }
      return s / m;
    });
  }
  const peak = Math.max(1e-6, ...a.map(Math.abs));
  return a.map((v) => (v / peak) * amp);
}

// ---- ink -------------------------------------------------------------------
const INK = [30, 28, 26];
const ZHU = [222, 92, 70]; // vermilion
const ZHE = [190, 128, 74]; // ochre
const rgba = ([r, g, b], a) => `rgba(${r},${g},${b},${a})`;

// Soft shapes via an offset shadow, which works where ctx.filter does not.
function soft(c, blur, color, fn) {
  const m = c.getTransform();
  c.save();
  c.shadowColor = color;
  c.shadowBlur = blur * Math.hypot(m.a, m.b);
  c.shadowOffsetX = 5000 * m.a;
  c.shadowOffsetY = 5000 * m.b;
  c.translate(-5000, 0);
  c.fillStyle = c.strokeStyle = '#000';
  fn(c);
  c.restore();
}

let speckleCv = null;
function speckle() {
  if (speckleCv) return speckleCv;
  speckleCv = document.createElement('canvas');
  speckleCv.width = speckleCv.height = 192;
  const c = speckleCv.getContext('2d');
  const img = c.createImageData(192, 192);
  const R = rng(11);
  for (let i = 0; i < img.data.length; i += 4) img.data[i + 3] = 255 * R() ** 2.2;
  c.putImageData(img, 0, 0);
  return speckleCv;
}

let scratch = null;
function scratchFor(c) {
  const { width, height } = c.canvas;
  if (!scratch || scratch.width !== width || scratch.height !== height) {
    scratch = document.createElement('canvas');
    scratch.width = width;
    scratch.height = height;
  }
  const l = scratch.getContext('2d');
  l.setTransform(1, 0, 0, 1, 0, 0);
  l.globalAlpha = 1;
  l.globalCompositeOperation = 'source-over';
  l.clearRect(0, 0, width, height);
  l.setTransform(c.getTransform());
  return l;
}
function put(c, l) {
  c.save();
  c.setTransform(1, 0, 0, 1, 0, 0);
  c.drawImage(l.canvas, 0, 0);
  c.restore();
}

// Pressure along a stroke: a blunt, rounded start and a tapering finish.
const press = ({ head = 0.12, tail = 0.4, h0 = 0.5, t1 = 0.05, belly = 0 } = {}) =>
  (u) => (h0 + (1 - h0) * ss(0, head, u)) * (1 - (1 - t1) * ss(1 - tail, 1, u)) * (1 + belly * Math.sin(Math.PI * u));
const flat = () => 1;

function shapeOf(R, pts, w, prof, wob) {
  const c = resample(spline(pts, 16), 1.1), nm = normals(c), N = c.length;
  const half = c.map((_, i) => (w / 2) * prof(i / (N - 1)));
  const eL = smoothNoise(R, N, wob), eR = smoothNoise(R, N, wob);
  const L = c.map((p, i) => [p[0] + nm[i][0] * half[i] * (1 + eL[i]), p[1] + nm[i][1] * half[i] * (1 + eL[i])]);
  const Rt = c.map((p, i) => [p[0] - nm[i][0] * half[i] * (1 + eR[i]), p[1] - nm[i][1] * half[i] * (1 + eR[i])]);
  return { c, nm, half, L, Rt, N };
}

function outline(S, [capA, capB]) {
  const { c, nm, half, L, Rt, N } = S, p = new Path2D();
  const arc = (e, n, f, r, from) => {
    for (let k = 1; k < 10; k++) {
      const t = (k / 10) * Math.PI, s = from * Math.cos(t);
      p.lineTo(e[0] + r * (n[0] * s + f[0] * Math.sin(t)), e[1] + r * (n[1] * s + f[1] * Math.sin(t)));
    }
  };
  p.moveTo(...L[0]);
  for (let i = 1; i < N; i++) p.lineTo(...L[i]);
  const ne = nm[N - 1];
  if (capB) arc(c[N - 1], ne, [ne[1], -ne[0]], half[N - 1], 1);
  for (let i = N - 1; i >= 0; i--) p.lineTo(...Rt[i]);
  const n0 = nm[0];
  if (capA) arc(c[0], n0, [-n0[1], n0[0]], half[0], -1);
  p.closePath();
  return p;
}

// One brush stroke: a wet body that bleeds a little, a darker rim where the
// water dried, paper grain, bristle streaks that break into dry-brush white,
// and ink that pales from tone to tone * fade along the stroke.
function ink(c, R, pts, o = {}) {
  const {
    w = 8, tone = 0.9, fade = 0.55, col = INK, wet = 0.4, dry = 0.3, prof = press(), cap = [1, 0],
    wob = 0.07, grain = 0.22, rim = wet, conic = null, lin = null, streak = dry, side = 0,
  } = o;
  const S = shapeOf(R, pts, w, prof, wob);
  const P = outline(S, cap);
  const l = scratchFor(c);
  if (wet > 0) soft(l, 2 + wet * 5, rgba(col, 0.22 * wet), (s) => s.fill(P));
  soft(l, 0.5 + wet * 1.2, rgba(col, 1), (s) => s.fill(P));
  l.globalCompositeOperation = 'destination-out';
  // the middle dries paler than the edges
  if (rim > 0) soft(l, Math.max(2, w * 0.22), `rgba(0,0,0,${0.32 * rim})`, (s) => s.fill(P));
  const lane = (off, from, alpha, lw, gaps) => {
    l.lineWidth = lw;
    l.strokeStyle = `rgba(0,0,0,${alpha})`;
    l.beginPath();
    let on = !gaps;
    for (let i = from; i < S.N; i++) {
      const u = i / (S.N - 1);
      if (gaps) {
        const q = gaps(u);
        if (on ? R() < 0.05 + 0.1 * (1 - q) : R() < 0.08 * q) on = !on;
      }
      const x = S.c[i][0] + S.nm[i][0] * off * 2 * S.half[i], y = S.c[i][1] + S.nm[i][1] * off * 2 * S.half[i];
      if (on) l.lineTo(x, y); else l.moveTo(x, y);
    }
    l.stroke();
  };
  const n = clamp(Math.round(w / 1.3), 4, 50);
  l.lineCap = 'round';
  // bristle striations along the whole stroke; side > 0 pales the right edge
  if (streak > 0 || side) {
    for (let k = 0; k < n; k++) {
      const off = (k + 0.5) / n - 0.5;
      const a = streak * (0.04 + 0.26 * R() ** 2) + Math.abs(side) * ss(-0.5, 0.5, off * Math.sign(side || 1)) * 0.5;
      lane(off, 0, clamp(a, 0, 0.9), Math.max(0.3, (w / n) * (0.6 + 0.8 * R())));
    }
  }
  // dry-brush white: bristles run out of ink toward the end, edges first
  if (dry > 0) {
    for (let k = 0; k < n; k++) {
      const off = (k + 0.5) / n - 0.5, edge = Math.abs(off) * 2;
      const u0 = clamp(1 - dry * (0.25 + 0.75 * R()) * (0.55 + 0.6 * edge), 0.05, 0.97);
      const from = Math.floor(clamp(u0 - 0.12, 0, 1) * (S.N - 1));
      lane(off, from, 0.8 + 0.2 * R(), Math.max(0.35, (w / n) * (0.5 + 0.7 * R())), (u) => ss(u0 - 0.06, Math.min(1, u0 + 0.3), u));
    }
  }
  // paper grain
  if (grain > 0) {
    l.save();
    l.setTransform(1, 0, 0, 1, 0, 0);
    l.globalAlpha = grain;
    l.fillStyle = l.createPattern(speckle(), 'repeat');
    l.fillRect(0, 0, l.canvas.width, l.canvas.height);
    l.restore();
  }
  // tone along the stroke, applied in device space
  const m = l.getTransform();
  const [p0, p1] = lin ? [lin.slice(0, 2), lin.slice(2)] : [S.c[0], S.c[S.N - 1]];
  const a = m.transformPoint({ x: p0[0], y: p0[1] }), b = m.transformPoint({ x: p1[0], y: p1[1] });
  l.setTransform(1, 0, 0, 1, 0, 0);
  l.globalCompositeOperation = 'destination-in';
  let g;
  if (conic) {
    const ctr = m.transformPoint({ x: conic[0], y: conic[1] });
    const off = (conic[2] ?? 0.4) * (conic[3] === -1 ? -1 : 1);
    g = l.createConicGradient(Math.atan2(a.y - ctr.y, a.x - ctr.x) - off, ctr.x, ctr.y);
  } else if (Math.hypot(b.x - a.x, b.y - a.y) < 1) {
    g = rgba([0, 0, 0], tone);
  } else g = l.createLinearGradient(a.x, a.y, b.x, b.y);
  if (typeof g !== 'string') {
    // a counter-clockwise curl runs the conic gradient backwards
    const back = conic && conic[3] === -1;
    g.addColorStop(0, `rgba(0,0,0,${back ? tone * fade : tone})`);
    g.addColorStop(1, `rgba(0,0,0,${back ? tone : tone * fade})`);
  }
  l.fillStyle = g;
  l.fillRect(0, 0, l.canvas.width, l.canvas.height);
  put(c, l);
}

// A boneless wash: any closed shape, soft-edged with a dried rim.
function wash(c, R, shapeFn, { tone = 0.2, col = INK, wet = 1, grain = 0.25 } = {}) {
  const l = scratchFor(c);
  soft(l, 4 + wet * 5, rgba(col, 0.25), shapeFn);
  soft(l, 1 + wet * 1.5, rgba(col, 1), shapeFn);
  l.globalCompositeOperation = 'destination-out';
  soft(l, 10, `rgba(0,0,0,${0.35 * wet})`, shapeFn);
  l.setTransform(1, 0, 0, 1, 0, 0);
  l.globalAlpha = grain;
  l.fillStyle = l.createPattern(speckle(), 'repeat');
  l.fillRect(0, 0, l.canvas.width, l.canvas.height);
  l.globalAlpha = 1;
  l.globalCompositeOperation = 'destination-in';
  l.fillStyle = `rgba(0,0,0,${tone})`;
  l.fillRect(0, 0, l.canvas.width, l.canvas.height);
  put(c, l);
}
const oval = (x, y, rx, ry, rot = 0) => (s) => { s.beginPath(); s.ellipse(x, y, rx, ry, rot, 0, TAU); s.fill(); };
// Clears a soft hole so a wash passes behind the head instead of pooling on it.
function hole(c, x, y, rx, ry) {
  c.save();
  c.globalCompositeOperation = 'destination-out';
  soft(c, 4, 'rgba(0,0,0,1)', oval(x, y, rx, ry));
  c.restore();
}

// ---- shared bits -----------------------------------------------------------
// A sleeping face centred on the nose bridge; s scales it.
function face(c, R, x, y, { s = 1, rot = 0, blush = 0.28, mouth = true, whisk = 0.5, eye = 1 } = {}) {
  c.save();
  c.translate(x, y);
  c.rotate(rot);
  c.scale(s, s);
  if (blush) for (const sx of [-1, 1]) wash(c, R, oval(sx * 21, 9, 7, 4.2), { tone: blush, col: ZHU, wet: 0.6, grain: 0.15 });
  const e = { w: 2.8 * eye, tone: 0.92, fade: 0.8, wet: 0.2, dry: 0, prof: press({ head: 0.3, tail: 0.45, h0: 0.55, t1: 0.25 }), streak: 0 };
  ink(c, R, [[-17, -1], [-12.5, 3.2], [-7, 1.4]], e);
  ink(c, R, [[17, -1], [12.5, 3.2], [7, 1.4]], e);
  ink(c, R, [[-1.6, 7], [1.6, 7]], { w: 3.6, tone: 0.9, fade: 1, wet: 0.2, dry: 0, prof: flat, cap: [1, 1], streak: 0 });
  if (mouth) {
    const m = { w: 1.3, tone: 0.7, fade: 0.9, wet: 0, dry: 0, prof: press({ head: 0.3, tail: 0.3, h0: 0.6, t1: 0.4 }), streak: 0, grain: 0 };
    ink(c, R, [[0, 8.5], [-2.6, 11.6], [-5.4, 10.4]], m);
    ink(c, R, [[0, 8.5], [2.6, 11.6], [5.4, 10.4]], m);
  }
  if (whisk) {
    const k = { w: 1, tone: whisk, fade: 0.3, wet: 0, dry: 0.4, prof: press({ head: 0.1, tail: 0.7, h0: 0.7 }), streak: 0, grain: 0 };
    ink(c, R, [[-24, 4], [-36, 1], [-46, 2]], k);
    ink(c, R, [[-24, 8], [-36, 9], [-44, 13]], k);
    ink(c, R, [[24, 4], [36, 1], [46, 2]], k);
    ink(c, R, [[24, 8], [36, 9], [44, 13]], k);
  }
  c.restore();
}

// One ear: a stroke pushed from the base up into a point.
function ear(c, R, base, tip, w = 16, tone = 0.85) {
  ink(c, R, [base, [(base[0] * 3 + tip[0] * 2) / 5, (base[1] * 3 + tip[1] * 2) / 5], tip], {
    w, tone, fade: 0.8, wet: 0.45, dry: 0.12, streak: 0.15, prof: press({ head: 0.04, tail: 0.85, h0: 1, t1: 0.03 }), cap: [1, 0],
  });
}

// A plum blossom: five pale vermilion petals and a few dark stamens.
function plum(c, R, x, y, r = 6) {
  const a0 = R() * TAU;
  for (let k = 0; k < 5; k++) {
    const a = a0 + (k * TAU) / 5;
    wash(c, R, oval(x + Math.cos(a) * r * 0.72, y + Math.sin(a) * r * 0.72, r * 0.62, r * 0.52, a), { tone: 0.34, col: ZHU, wet: 0.5, grain: 0.12 });
  }
  for (let k = 0; k < 4; k++) {
    const a = R() * TAU, d = r * (0.2 + 0.3 * R());
    ink(c, R, [[x + Math.cos(a) * d, y + Math.sin(a) * d], [x + Math.cos(a) * d + 0.4, y + Math.sin(a) * d]], { w: 1.6, tone: 0.75, prof: flat, cap: [1, 1], wet: 0, dry: 0, streak: 0, grain: 0 });
  }
}

// ---- the cats --------------------------------------------------------------
// parts: drawn once into their own layer. move(t) returns a transform about the
// part's pivot. Only one or two parts move per cat.
const breathe = (amp = 0.018) => (t) => ({ sy: 1 + amp * Math.sin(t * TAU / 3.8), sx: 1 + amp * 0.3 * Math.sin(t * TAU / 3.8) });
const sway = (amp = 0.04, period = 5.2) => (t) => ({ rot: amp * Math.sin(t * TAU / period) });
const nod = (amp = 0.012) => (t) => ({ rot: amp * Math.sin(t * TAU / 3.8 + 0.9), dy: 0.8 * Math.sin(t * TAU / 3.8) });

const CATS = [
  {
    id: 'ring', name: '一笔成团', test: '更抽象 · 身体是一笔',
    note: '身体只是一笔侧锋，从头后绕一圈收成尾巴；头是一团淡墨。',
    logo: [218, 160, 236], zz: [92, 100],
    parts: [
      {
        pivot: [220, 220], move: breathe(0.014),
        draw(c, R) {
          ink(c, R, [[184, 124], [226, 100], [282, 104], [318, 140], [314, 190], [272, 215], [212, 221], [166, 214], [134, 198], [124, 182]], {
            w: 34, tone: 0.86, fade: 0.22, wet: 0.6, dry: 0.6, side: 0.35, prof: press({ head: 0.14, tail: 0.35, h0: 0.35, t1: 0.05 }), conic: [218, 162],
          });
        },
      },
      {
        pivot: [150, 186], move: nod(),
        draw(c, R) {
          ink(c, R, [[144, 152], [156, 152]], { w: 70, tone: 0.16, fade: 1, wet: 1, dry: 0, prof: flat, cap: [1, 1], streak: 0 });
          ear(c, R, [128, 132], [119, 108]);
          ear(c, R, [170, 130], [181, 107]);
          face(c, R, 150, 156, { s: 0.95 });
        },
      },
    ],
  },
  {
    id: 'mochi', name: '墨团子', test: '更可爱 · 身体淡成一团',
    note: '整只猫是一团湿淡墨，像糯米团；只有耳朵、脸和尾巴是浓墨。',
    logo: [210, 168, 236], zz: [98, 104],
    parts: [
      {
        pivot: [210, 226], move: breathe(),
        draw(c, R) {
          ink(c, R, [[168, 170], [252, 172]], { w: 112, tone: 0.2, fade: 0.8, wet: 1, dry: 0, prof: flat, cap: [1, 1], streak: 0, wob: 0.03 });
          ink(c, R, [[196, 118], [246, 114], [290, 130], [306, 160]], { w: 26, tone: 0.3, fade: 0.4, wet: 0.8, dry: 0.5, prof: press({ head: 0.1, tail: 0.5, h0: 0.6 }) });
          ear(c, R, [136, 132], [127, 109]);
          ear(c, R, [176, 126], [188, 104]);
          face(c, R, 156, 172, { s: 1 });
        },
      },
      {
        pivot: [300, 196], move: sway(0.035),
        draw(c, R) {
          ink(c, R, [[302, 192], [292, 214], [256, 226], [206, 228], [170, 220]], { w: 13, tone: 0.9, fade: 0.55, wet: 0.3, dry: 0.45, prof: press({ head: 0.1, tail: 0.45, h0: 0.7 }) });
        },
      },
    ],
  },
  {
    id: 'blank', name: '留白', test: '身体几乎全留白',
    note: '只画头、耳朵和一条尾巴；身体就一根淡淡的背线，剩下交给白纸。',
    logo: [218, 170, 236], zz: [92, 106],
    parts: [
      {
        pivot: [240, 222], move: breathe(0.014),
        draw(c, R) {
          ink(c, R, [[166, 140], [214, 118], [280, 120], [326, 150], [334, 192]], { w: 5, tone: 0.55, fade: 0.4, dry: 0.55, wet: 0.1, prof: press({ head: 0.1, tail: 0.5, h0: 0.5 }) });
          ink(c, R, [[180, 214], [238, 222], [300, 216]], { w: 3, tone: 0.28, fade: 0.5, dry: 0.6, wet: 0, prof: press({ head: 0.2, tail: 0.5, h0: 0.4 }) });
        },
      },
      {
        pivot: [330, 196], move: sway(0.05),
        draw(c, R) {
          ink(c, R, [[332, 194], [318, 218], [280, 230], [220, 232], [172, 224], [150, 208], [153, 194]], { w: 15, tone: 0.9, fade: 0.45, dry: 0.5, side: 0.2, wet: 0.35, prof: press({ head: 0.12, tail: 0.4, h0: 0.55 }) });
        },
      },
      {
        pivot: [140, 200], move: nod(),
        draw(c, R) {
          ink(c, R, [[134, 162], [146, 162]], { w: 62, tone: 0.1, fade: 1, wet: 1, dry: 0, prof: flat, cap: [1, 1], streak: 0 });
          ear(c, R, [120, 142], [111, 118]);
          ear(c, R, [160, 140], [170, 116]);
          face(c, R, 140, 166, { s: 0.95 });
        },
      },
    ],
  },
  {
    id: 'loaf', name: '揣手小面包', test: '大头小身 · 一点赭石',
    note: '团成一只小面包，头大身子短，淡淡一层赭石，背上三笔虎斑。',
    logo: [222, 172, 236], zz: [98, 104],
    parts: [
      {
        pivot: [230, 228], move: breathe(),
        draw(c, R) {
          ink(c, R, [[184, 190], [268, 190]], { w: 76, tone: 0.32, fade: 0.75, col: ZHE, wet: 1, dry: 0, prof: flat, cap: [1, 1], streak: 0, wob: 0.03 });
          hole(c, 150, 160, 40, 35);
          ink(c, R, [[174, 154], [224, 142], [276, 147], [306, 172], [309, 204]], { w: 6, tone: 0.75, fade: 0.5, dry: 0.45, wet: 0.2, prof: press({ head: 0.1, tail: 0.4, h0: 0.5 }) });
          for (const [a, b] of [[[232, 144], [228, 164]], [[258, 148], [252, 170]], [[282, 158], [274, 178]]]) {
            ink(c, R, [a, b], { w: 8, tone: 0.65, fade: 0.6, dry: 0.3, wet: 0.3, prof: press({ head: 0.1, tail: 0.6, h0: 0.9, t1: 0.1 }) });
          }
          for (const x of [150, 178]) {
            ink(c, R, [[x, 223], [x + 14, 224]], { w: 13, tone: 0.24, fade: 1, col: ZHE, wet: 0.8, dry: 0, prof: flat, cap: [1, 1], streak: 0 });
            ink(c, R, [[x - 4, 226], [x + 6, 230], [x + 18, 227]], { w: 2.2, tone: 0.55, fade: 0.6, dry: 0.3, wet: 0, prof: press({ head: 0.2, tail: 0.4, h0: 0.5, t1: 0.3 }) });
          }
        },
      },
      {
        pivot: [306, 214], move: sway(0.03),
        draw(c, R) {
          ink(c, R, [[310, 204], [302, 224], [264, 232], [222, 230]], { w: 11, tone: 0.85, fade: 0.55, dry: 0.4, wet: 0.3, prof: press({ head: 0.1, tail: 0.45, h0: 0.7 }) });
        },
      },
      {
        pivot: [150, 200], move: nod(),
        draw(c, R) {
          ink(c, R, [[142, 160], [158, 160]], { w: 72, tone: 0.24, fade: 1, col: ZHE, wet: 1, dry: 0, prof: flat, cap: [1, 1], streak: 0, wob: 0.03 });
          ear(c, R, [126, 138], [117, 112]);
          ear(c, R, [172, 136], [184, 111]);
          for (const [a, b] of [[[142, 128], [143, 140]], [[152, 127], [152, 139]], [[162, 128], [161, 140]]]) {
            ink(c, R, [a, b], { w: 3.4, tone: 0.55, fade: 0.5, dry: 0.2, wet: 0.1, prof: press({ head: 0.1, tail: 0.6, h0: 0.9, t1: 0.1 }) });
          }
          face(c, R, 150, 166);
        },
      },
    ],
  },
  {
    id: 'patch', name: '花猫', test: '白猫加几块墨斑',
    note: '一只白猫，轮廓只勾一半；背上一大块墨斑，头顶和一只耳朵也是黑的。',
    logo: [218, 172, 236], zz: [92, 106],
    parts: [
      {
        pivot: [230, 228], move: breathe(),
        draw(c, R) {
          ink(c, R, [[178, 134], [224, 114], [282, 116], [322, 146], [328, 190], [306, 216]], { w: 4.5, tone: 0.5, fade: 0.5, dry: 0.4, wet: 0.1, prof: press({ head: 0.1, tail: 0.3, h0: 0.6, t1: 0.3 }) });
          ink(c, R, [[300, 222], [240, 229], [182, 223]], { w: 3, tone: 0.26, fade: 0.6, dry: 0.5, wet: 0, prof: press({ head: 0.2, tail: 0.4, h0: 0.5 }) });
          ink(c, R, [[226, 124], [262, 120], [294, 136], [304, 160]], { w: 36, tone: 0.85, fade: 0.65, wet: 1, dry: 0.25, side: 0.2, wob: 0.15, prof: press({ head: 0.25, tail: 0.45, h0: 0.55, t1: 0.2 }) });
        },
      },
      {
        pivot: [318, 204], move: sway(0.035),
        draw(c, R) {
          ink(c, R, [[320, 200], [302, 222], [258, 232], [204, 232], [168, 222]], { w: 13, tone: 0.92, fade: 0.6, dry: 0.4, wet: 0.3, prof: press({ head: 0.1, tail: 0.4, h0: 0.7 }) });
        },
      },
      {
        pivot: [150, 204], move: nod(),
        draw(c, R) {
          ink(c, R, [[120, 150], [113, 172], [124, 192], [150, 200], [176, 194]], { w: 3, tone: 0.35, fade: 0.5, dry: 0.4, wet: 0, prof: press({ head: 0.2, tail: 0.4, h0: 0.5 }) });
          ink(c, R, [[142, 137], [166, 133], [184, 146]], { w: 26, tone: 0.82, fade: 0.8, wet: 1, dry: 0.15, wob: 0.15, prof: press({ head: 0.3, tail: 0.5, h0: 0.6, t1: 0.25 }) });
          ear(c, R, [174, 136], [186, 110]);
          ink(c, R, [[124, 146], [115, 116], [140, 134]], { w: 3, tone: 0.7, fade: 0.8, dry: 0.2, wet: 0.1, prof: press({ head: 0.1, tail: 0.3, h0: 0.7, t1: 0.3 }) });
          face(c, R, 148, 170);
        },
      },
    ],
  },
  {
    id: 'branch', name: '枝头', test: '放进网页首屏 · 尾巴会晃',
    note: '趴在一根梅枝上睡，一只前爪和尾巴垂下来，尾巴跟着呼吸慢慢晃。',
    logo: [190, 166, 190], zz: [80, 96],
    parts: [
      {
        draw(c, R) {
          ink(c, R, [[16, 196], [90, 186], [170, 180], [250, 176], [330, 162], [392, 146]], { w: 11, tone: 0.8, fade: 0.5, dry: 0.6, side: 0.3, wet: 0.2, prof: press({ head: 0.05, tail: 0.4, h0: 0.9, t1: 0.2 }) });
          ink(c, R, [[318, 164], [340, 136], [352, 112]], { w: 4.5, tone: 0.75, fade: 0.5, dry: 0.5, wet: 0.1 });
          ink(c, R, [[64, 190], [52, 212], [46, 230]], { w: 4, tone: 0.7, fade: 0.5, dry: 0.5, wet: 0.1 });
          for (const [x, y, r] of [[350, 106, 7], [332, 140, 6], [374, 150, 5.5], [50, 230, 6], [92, 182, 5]]) plum(c, R, x, y, r);
          for (const [x, y] of [[358, 98], [304, 160], [40, 240]]) ink(c, R, [[x, y], [x + 1, y]], { w: 4, tone: 0.5, col: ZHU, prof: flat, cap: [1, 1], wet: 0.4, dry: 0, streak: 0 });
        },
      },
      {
        pivot: [262, 168], move: sway(0.09, 4.6),
        draw(c, R) {
          ink(c, R, [[262, 164], [278, 186], [281, 214], [273, 240], [280, 262]], { w: 10, tone: 0.85, fade: 0.5, dry: 0.45, wet: 0.3, prof: press({ head: 0.1, tail: 0.4, h0: 0.7 }) });
        },
      },
      {
        pivot: [200, 180], move: breathe(),
        draw(c, R) {
          ink(c, R, [[166, 158], [236, 156]], { w: 44, tone: 0.17, fade: 1, wet: 1, dry: 0, prof: flat, cap: [1, 1], streak: 0, wob: 0.04 });
          hole(c, 128, 154, 31, 28);
          ink(c, R, [[162, 140], [206, 129], [250, 134], [268, 154]], { w: 5, tone: 0.7, fade: 0.5, dry: 0.45, wet: 0.15, prof: press({ head: 0.1, tail: 0.4, h0: 0.5 }) });
          ink(c, R, [[166, 178], [168, 198], [169, 210]], { w: 12, tone: 0.2, fade: 1, wet: 1, dry: 0, prof: flat, cap: [1, 1], streak: 0 });
          ink(c, R, [[163, 210], [168, 213], [174, 210]], { w: 2.4, tone: 0.6, fade: 0.8, dry: 0.2, wet: 0, prof: press({ head: 0.2, tail: 0.4, h0: 0.5, t1: 0.3 }) });
        },
      },
      {
        pivot: [130, 178], move: nod(),
        draw(c, R) {
          ink(c, R, [[122, 154], [134, 154]], { w: 56, tone: 0.13, fade: 1, wet: 1, dry: 0, prof: flat, cap: [1, 1], streak: 0 });
          ear(c, R, [111, 136], [103, 113], 14);
          ear(c, R, [146, 133], [156, 110], 14);
          face(c, R, 128, 158, { s: 0.85 });
        },
      },
    ],
  },
  {
    id: 'swirl', name: '卷云尾', test: '更抽象 · 尾巴是主角',
    note: '小小一团淡墨蜷在中间，大尾巴一笔绕一圈，最后卷成一朵云。',
    logo: [214, 168, 236], zz: [88, 92],
    parts: [
      {
        pivot: [214, 214], move: breathe(),
        draw(c, R) {
          ink(c, R, [[198, 182], [238, 184]], { w: 62, tone: 0.16, fade: 1, wet: 1, dry: 0, prof: flat, cap: [1, 1], streak: 0, wob: 0.03 });
          hole(c, 166, 172, 28, 26);
        },
      },
      {
        pivot: [252, 196], move: sway(0.012),
        draw(c, R) {
          // one stroke round the cat; the tip curls up like a cloud
          ink(c, R, [[252, 198], [270, 160], [262, 118], [224, 94], [170, 94], [128, 122], [112, 170], [128, 214], [176, 240], [240, 245], [284, 232], [306, 208], [302, 188], [286, 184], [279, 196]], {
            w: 20, tone: 0.85, fade: 0.32, dry: 0.4, side: 0.25, wet: 0.4, lin: [150, 90, 290, 240], prof: press({ head: 0.08, tail: 0.25, h0: 0.45, t1: 0.15 }),
          });
        },
      },
      {
        pivot: [168, 204], move: nod(),
        draw(c, R) {
          ink(c, R, [[160, 172], [172, 172]], { w: 52, tone: 0.12, fade: 1, wet: 1, dry: 0, prof: flat, cap: [1, 1], streak: 0 });
          ear(c, R, [150, 154], [143, 134], 12);
          ear(c, R, [182, 152], [191, 132], 12);
          face(c, R, 166, 176, { s: 0.75, whisk: 0.4 });
        },
      },
    ],
  },
  {
    id: 'three', name: '三笔', test: '最少笔数 · 三笔',
    note: '头一笔，身子一笔侧锋带飞白，尾巴一笔。只剩最必要的。',
    logo: [222, 172, 236], zz: [96, 104],
    parts: [
      {
        pivot: [240, 220], move: breathe(),
        draw(c, R) {
          ink(c, R, [[180, 150], [236, 134], [292, 150], [314, 190]], { w: 46, tone: 0.45, fade: 0.25, dry: 0.8, side: 0.3, wet: 0.4, prof: press({ head: 0.08, tail: 0.45, h0: 0.85, t1: 0.2 }) });
          hole(c, 150, 164, 33, 31);
        },
      },
      {
        pivot: [312, 198], move: sway(0.035),
        draw(c, R) {
          ink(c, R, [[312, 196], [292, 220], [238, 230], [186, 222]], { w: 12, tone: 0.92, fade: 0.6, dry: 0.4, wet: 0.3, prof: press({ head: 0.1, tail: 0.45, h0: 0.7 }) });
        },
      },
      {
        pivot: [150, 198], move: nod(),
        draw(c, R) {
          ink(c, R, [[146, 164], [154, 164]], { w: 62, tone: 0.34, fade: 1, wet: 1, dry: 0, prof: flat, cap: [1, 1], streak: 0.25, wob: 0.05 });
          ink(c, R, [[130, 142], [119, 115]], { w: 15, tone: 0.9, fade: 0.8, wet: 0.4, dry: 0.15, prof: press({ head: 0.04, tail: 0.9, h0: 1, t1: 0.02 }) });
          ink(c, R, [[168, 139], [180, 113]], { w: 15, tone: 0.9, fade: 0.8, wet: 0.4, dry: 0.15, prof: press({ head: 0.04, tail: 0.9, h0: 1, t1: 0.02 }) });
          face(c, R, 150, 170, { whisk: 0.35 });
        },
      },
    ],
  },
  {
    id: 'seal', name: '朱印', test: '为图标而生 · 16px 也认得出',
    note: '把团着的猫刻成一方白文朱印：红底，猫是留出来的白，脸用红线刻。',
    logo: [200, 140, 156],
    parts: [{ draw: seal }],
  },
];

// A white-on-red seal: the cat is carved out of the red block.
function seal(c, R) {
  const x0 = 130, y0 = 70, S = 140;
  const block = new Path2D();
  const pts = [];
  for (let k = 0; k < 4; k++) {
    for (let j = 0; j < 12; j++) {
      const t = j / 12, jit = () => (R() - 0.5) * 1.6;
      const sides = [[x0 + S * t, y0], [x0 + S, y0 + S * t], [x0 + S * (1 - t), y0 + S], [x0, y0 + S * (1 - t)]];
      pts.push([sides[k][0] + jit(), sides[k][1] + jit()]);
    }
  }
  pts.forEach((p, i) => (i ? block.lineTo(...p) : block.moveTo(...p)));
  block.closePath();
  c.save();
  c.fillStyle = 'rgb(198,52,40)';
  c.fill(block);
  c.globalCompositeOperation = 'destination-out';
  c.fillStyle = '#000';
  c.lineCap = c.lineJoin = 'round';
  // body, head, ears, tail
  c.beginPath(); c.ellipse(216, 160, 46, 34, -0.05, 0, TAU); c.fill();
  c.beginPath(); c.ellipse(166, 136, 31, 28, 0, 0, TAU); c.fill();
  for (const [a, b, d] of [[[142, 124], [143, 92], [164, 112]], [[168, 112], [190, 93], [192, 126]]]) {
    c.beginPath(); c.moveTo(...a); c.quadraticCurveTo(a[0] - 2, (a[1] + b[1]) / 2, ...b); c.quadraticCurveTo((b[0] + d[0]) / 2 + 2, (b[1] + d[1]) / 2, ...d); c.closePath(); c.fill();
  }
  c.lineWidth = 15;
  c.beginPath(); c.moveTo(258, 166); c.bezierCurveTo(262, 196, 222, 200, 176, 192); c.stroke();
  // weathering
  for (let i = 0; i < 70; i++) {
    const edge = R() < 0.6, a = R() * 4 | 0, t = R();
    const p = edge ? [[x0 + S * t, y0 + 2], [x0 + S - 2, y0 + S * t], [x0 + S * t, y0 + S - 2], [x0 + 2, y0 + S * t]][a] : [x0 + R() * S, y0 + R() * S];
    c.globalAlpha = 0.3 + 0.6 * R();
    c.beginPath(); c.arc(p[0], p[1], 0.4 + R() * (edge ? 1.6 : 0.9), 0, TAU); c.fill();
  }
  c.globalAlpha = 1;
  c.globalCompositeOperation = 'source-over';
  c.strokeStyle = 'rgb(198,52,40)';
  c.lineWidth = 2.6;
  // face, and the lines that separate head, body and tail
  for (const d of ['M 150 140 Q 155 145 160 141', 'M 172 141 Q 177 145 182 140', 'M 186 128 Q 196 150 186 166', 'M 254 170 Q 244 188 200 186']) c.stroke(new Path2D(d));
  c.fillStyle = 'rgb(198,52,40)';
  c.beginPath(); c.ellipse(166, 149, 2.6, 2, 0, 0, TAU); c.fill();
  c.restore();
}

// ---- rendering -------------------------------------------------------------
let paperUrl = null;
function paperTile() {
  if (paperUrl) return paperUrl;
  const cv = document.createElement('canvas');
  cv.width = cv.height = 320;
  const c = cv.getContext('2d'), R = rng(3);
  c.fillStyle = '#faf7f0';
  c.fillRect(0, 0, 320, 320);
  c.strokeStyle = 'rgba(150,130,100,.06)';
  c.lineWidth = 0.6;
  for (let i = 0; i < 140; i++) {
    const x = R() * 320, y = R() * 320, a = R() * TAU, l = 6 + R() * 22;
    c.beginPath();
    c.moveTo(x, y);
    c.quadraticCurveTo(x + Math.cos(a + 0.7) * l * 0.5, y + Math.sin(a + 0.7) * l * 0.5, x + Math.cos(a) * l, y + Math.sin(a) * l);
    c.stroke();
  }
  paperUrl = cv.toDataURL();
  return paperUrl;
}

let zStamp = null;
function zLayer(scale) {
  if (zStamp && zStamp.scale === scale) return zStamp.cv;
  const cv = document.createElement('canvas');
  cv.width = cv.height = Math.ceil(24 * scale);
  const c = cv.getContext('2d');
  c.scale(scale, scale);
  ink(c, rng(5), [[5, 6], [17, 5], [6, 18], [19, 17]], { w: 2.6, tone: 0.6, fade: 0.7, wet: 0.2, dry: 0.2, prof: press({ head: 0.1, tail: 0.3, h0: 0.7, t1: 0.3 }), streak: 0 });
  zStamp = { scale, cv };
  return cv;
}

// Draws every part into its own layer, cropped to what it painted.
function build(cat, scale) {
  return cat.parts.map((part, i) => {
    const cv = document.createElement('canvas');
    cv.width = Math.ceil(W * scale);
    cv.height = Math.ceil(H * scale);
    const c = cv.getContext('2d');
    c.scale(scale, scale);
    part.draw(c, rng(101 + i * 17 + cat.id.length));
    const box = bounds(c, cv);
    const out = document.createElement('canvas');
    out.width = Math.max(1, box.w);
    out.height = Math.max(1, box.h);
    out.getContext('2d').drawImage(cv, box.x, box.y, box.w, box.h, 0, 0, box.w, box.h);
    return { part, cv: out, x: box.x / scale, y: box.y / scale, w: box.w / scale, h: box.h / scale };
  });
}
function bounds(c, cv) {
  const { data, width, height } = c.getImageData(0, 0, cv.width, cv.height);
  let x0 = width, y0 = height, x1 = -1, y1 = -1;
  for (let y = 0; y < height; y += 2) {
    for (let x = 0; x < width; x += 2) {
      if (data[(y * width + x) * 4 + 3] > 2) {
        if (x < x0) x0 = x;
        if (x > x1) x1 = x;
        if (y < y0) y0 = y;
        if (y > y1) y1 = y;
      }
    }
  }
  if (x1 < 0) return { x: 0, y: 0, w: 1, h: 1 };
  x0 = Math.max(0, x0 - 4); y0 = Math.max(0, y0 - 4);
  return { x: x0, y: y0, w: Math.min(width, x1 + 6) - x0, h: Math.min(height, y1 + 6) - y0 };
}

function compose(c, layers, t, cat, zz = true) {
  for (const L of layers) {
    const m = L.part.move ? L.part.move(t) : {};
    const [px, py] = L.part.pivot || [0, 0];
    c.save();
    c.translate(px + (m.dx || 0), py + (m.dy || 0));
    c.rotate(m.rot || 0);
    c.scale(m.sx || 1, m.sy || 1);
    c.translate(-px, -py);
    c.drawImage(L.cv, L.x, L.y, L.w, L.h);
    c.restore();
  }
  if (zz && cat.zz) {
    const z = zStamp && zStamp.cv;
    if (!z) return;
    for (let k = 0; k < 3; k++) {
      const p = ((t / 4.5 + k / 3) % 1);
      c.save();
      c.globalAlpha = Math.sin(p * Math.PI) * 0.85;
      const s = 0.45 + p * 0.55;
      c.translate(cat.zz[0] + p * 26 + Math.sin(p * 5 + k) * 3, cat.zz[1] - p * 44);
      c.scale(s, s);
      c.drawImage(z, 0, 0, 24, 24);
      c.restore();
    }
  }
}

const reduce = matchMedia('(prefers-reduced-motion: reduce)').matches;
const live = new Set();

function mount(card, cat) {
  const cv = card.querySelector('canvas.art');
  const dpr = +new URLSearchParams(location.search).get('dpr') || Math.min(2, window.devicePixelRatio || 1);
  const scale = (cv.clientWidth / W) * dpr;
  cv.width = Math.round(W * scale);
  cv.height = Math.round(H * scale);
  zLayer(scale);
  const layers = build(cat, scale);
  const c = cv.getContext('2d');
  const entry = {
    draw(t) {
      c.setTransform(1, 0, 0, 1, 0, 0);
      c.clearRect(0, 0, cv.width, cv.height);
      c.setTransform(scale, 0, 0, scale, 0, 0);
      compose(c, layers, t, cat);
    },
  };
  entry.draw(0);
  // icons and the header lockup reuse the still frame without paper or zz
  const still = document.createElement('canvas');
  still.width = cv.width;
  still.height = cv.height;
  const sc = still.getContext('2d');
  sc.setTransform(scale, 0, 0, scale, 0, 0);
  compose(sc, layers, 0, cat, false);
  const [lx, ly, ls] = cat.logo;
  for (const ic of card.querySelectorAll('canvas.icon')) {
    const px = +ic.dataset.px, d = Math.round(px * (window.devicePixelRatio || 1));
    ic.width = ic.height = d;
    const ictx = ic.getContext('2d');
    ictx.imageSmoothingQuality = 'high';
    ictx.drawImage(still, (lx - ls / 2) * scale, (ly - ls / 2) * scale, ls * scale, ls * scale, 0, 0, d, d);
  }
  return entry;
}

const grid = document.getElementById('grid');
const entries = new Map();
const io = new IntersectionObserver((list) => {
  for (const e of list) {
    const i = +e.target.dataset.i;
    if (e.isIntersecting) {
      if (!entries.has(i)) entries.set(i, mount(e.target, CATS[i]));
      live.add(i);
    } else live.delete(i);
  }
}, { rootMargin: '200px' });

CATS.forEach((cat, i) => {
  const card = document.createElement('article');
  card.className = 'card';
  card.dataset.i = i;
  const L = String.fromCharCode(65 + i);
  card.innerHTML = `
    <canvas class="art" role="img" aria-label="${cat.name}"></canvas>
    <div class="meta"><span class="num">${L}</span><span class="name">${cat.name}</span><span class="test">${cat.test}</span></div>
    <p class="note">${cat.note}</p>
    <div class="logos" aria-label="当作图标">
      <canvas class="icon" data-px="64" style="width:64px;height:64px"></canvas>
      <canvas class="icon" data-px="32" style="width:32px;height:32px"></canvas>
      <canvas class="icon" data-px="16" style="width:16px;height:16px"></canvas>
      <span class="lockup"><canvas class="icon" data-px="30" style="width:30px;height:30px"></canvas><b>Zen</b></span>
    </div>`;
  card.querySelector('canvas.art').style.backgroundImage = `url(${paperTile()})`;
  grid.appendChild(card);
  io.observe(card);
});

if (!reduce) {
  const t0 = performance.now();
  const tick = (now) => {
    const t = (now - t0) / 1000;
    for (const i of live) entries.get(i)?.draw(t);
    requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
}

window.zenInk = { CATS };
