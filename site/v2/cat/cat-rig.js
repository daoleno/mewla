// Zen cat: a painted sleeping cat on canvas.
//
// Static parts (body, head, ears, paws) are painted once into offscreen
// sprites: soft shading, form-following stripes, fur strands and grain.
// Per frame they are only transformed. The tail, eyes, nose, mouth and
// whiskers are drawn live so they can bend, blink and yawn.

const NS = 'http://www.w3.org/2000/svg';
const G = 362; // ground line in world units (world is 640 x 420)

const PAL = {
  light: [244, 205, 154],
  mid: [226, 160, 104],
  shadow: [190, 116, 72],
  deep: [140, 82, 56],
  stripe: [184, 102, 58],
  rim: [255, 242, 220],
  hi: [255, 246, 230],
  cream: [249, 238, 222],
  creamShade: [222, 199, 174],
  ink: [56, 40, 33],
  nose: [214, 140, 128],
  noseDark: [170, 98, 90],
  innerEar: [238, 178, 160],
  iris: [222, 186, 92],
  irisDark: [150, 110, 42],
};
const css = (c, a = 1) => `rgba(${c[0] | 0},${c[1] | 0},${c[2] | 0},${a})`;
const mix = (a, b, t) => [a[0] + (b[0] - a[0]) * t, a[1] + (b[1] - a[1]) * t, a[2] + (b[2] - a[2]) * t];
const clamp = (v, a, b) => Math.min(b, Math.max(a, v));
const smooth = (v, a, b) => { const t = clamp((v - a) / (b - a), 0, 1); return t * t * (3 - 2 * t); };
const unit = (x, y) => { const l = Math.hypot(x, y) || 1; return [x / l, y / l]; };
const turn = ([x, y], a) => [x * Math.cos(a) - y * Math.sin(a), x * Math.sin(a) + y * Math.cos(a)];

function rng(seed) {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

// ---- easing + keyframes ---------------------------------------------------
const E = {
  lin: (t) => t,
  io: (t) => (t < 0.5 ? 4 * t * t * t : 1 - (-2 * t + 2) ** 3 / 2),
  out: (t) => 1 - (1 - t) ** 3,
  in: (t) => t * t * t,
};
// keys: [[time, value, ease-into-this-key]]
function track(t, keys) {
  if (t <= keys[0][0]) return keys[0][1];
  for (let i = 1; i < keys.length; i++) {
    const [t1, v1, e = 'io'] = keys[i];
    if (t <= t1) {
      const [t0, v0] = keys[i - 1];
      return v0 + (v1 - v0) * E[e]((t - t0) / (t1 - t0));
    }
  }
  return keys[keys.length - 1][1];
}

// ---- painting helpers -----------------------------------------------------
const probe = document.createElement('canvas').getContext('2d');
let svgHost = null;
const OFF = 4000;

// Blurred draw: the shape is drawn far off-canvas and only its shadow lands.
function soft(ctx, s, blur, color, draw) {
  ctx.save();
  ctx.shadowColor = color;
  ctx.shadowBlur = blur * s;
  ctx.shadowOffsetX = OFF * s;
  ctx.shadowOffsetY = 0;
  ctx.translate(-OFF, 0);
  ctx.fillStyle = ctx.strokeStyle = '#000';
  ctx.lineCap = 'round';
  ctx.lineJoin = 'round';
  draw(ctx);
  ctx.restore();
}
const ellipse = (c, x, y, rx, ry, a = 0) => { c.beginPath(); c.ellipse(x, y, rx, ry, a, 0, Math.PI * 2); c.fill(); };
const strokeD = (c, w, ...ds) => { c.lineWidth = w; for (const d of ds) c.stroke(new Path2D(d)); };
function glow(ctx, x, y, r, col, a) {
  const g = ctx.createRadialGradient(x, y, 0, x, y, r);
  g.addColorStop(0, css(col, a));
  g.addColorStop(1, css(col, 0));
  ctx.fillStyle = g;
  ctx.fillRect(x - r, y - r, r * 2, r * 2);
}

// A single tapered hair.
function strand(ctx, x, y, dx, dy, len, w, bend) {
  const nx = -dy, ny = dx;
  const tx = x + dx * len + nx * bend, ty = y + dy * len + ny * bend;
  const cx = x + dx * len * 0.5 + nx * bend * 0.6, cy = y + dy * len * 0.5 + ny * bend * 0.6;
  ctx.beginPath();
  ctx.moveTo(x + nx * w * 0.5, y + ny * w * 0.5);
  ctx.quadraticCurveTo(cx + nx * w * 0.25, cy + ny * w * 0.25, tx, ty);
  ctx.quadraticCurveTo(cx - nx * w * 0.25, cy - ny * w * 0.25, x - nx * w * 0.5, y - ny * w * 0.5);
  ctx.fill();
}

// Points along a closed outline with outward normals (cached per part).
function contour(part) {
  if (part._contour) return part._contour;
  if (!svgHost) {
    svgHost = document.createElementNS(NS, 'svg');
    svgHost.setAttribute('aria-hidden', 'true');
    svgHost.style.cssText = 'position:absolute;width:0;height:0;visibility:hidden';
    document.body.appendChild(svgHost);
  }
  const el = document.createElementNS(NS, 'path');
  el.setAttribute('d', part.d);
  svgHost.appendChild(el);
  const path = new Path2D(part.d);
  const total = el.getTotalLength();
  const pts = [];
  for (let l = 0; l < total; l += part.step || 1) {
    const a = el.getPointAtLength(l), b = el.getPointAtLength(Math.min(total, l + 0.5));
    let [nx, ny] = unit(b.y - a.y, -(b.x - a.x));
    if (probe.isPointInPath(path, a.x + nx * 1.5, a.y + ny * 1.5)) { nx = -nx; ny = -ny; }
    pts.push({ x: a.x, y: a.y, nx, ny });
  }
  el.remove();
  part._contour = pts;
  return pts;
}

// Paint a part: base shading inside the outline, then strands that pick up
// the painted colour, then edge fur that breaks the silhouette, then grain.
function paintFur(ctx, s, part) {
  const path = new Path2D(part.d);
  const R = rng(part.seed);
  const [x0, y0, x1, y1] = part.box;

  // The shading is all soft, so paint it at half resolution and scale it up;
  // only the outline and the strands need the full resolution.
  const ls = Math.max(0.75, s / 2);
  const low = document.createElement('canvas');
  low.width = Math.ceil((x1 - x0 + 2 * PAD) * ls);
  low.height = Math.ceil((y1 - y0 + 2 * PAD) * ls);
  const lc = low.getContext('2d', { willReadFrequently: true });
  lc.setTransform(ls, 0, 0, ls, -(x0 - PAD) * ls, -(y0 - PAD) * ls);
  part.base(lc, ls);
  ctx.save();
  ctx.clip(path);
  ctx.imageSmoothingQuality = 'high';
  ctx.drawImage(low, x0 - PAD, y0 - PAD, low.width / ls, low.height / ls);
  ctx.restore();

  // strands pick up the painted colour under them
  const data = lc.getImageData(0, 0, low.width, low.height).data;
  const col = (x, y) => {
    const px = clamp(Math.round((x - x0 + PAD) * ls), 0, low.width - 1);
    const py = clamp(Math.round((y - y0 + PAD) * ls), 0, low.height - 1);
    const i = (py * low.width + px) * 4;
    return [data[i], data[i + 1], data[i + 2]];
  };
  const inside = (x, y) => probe.isPointInPath(path, x, y);
  const [l0, l1] = part.len;

  // interior clumps: sparse, lighter on the lit side, darker in the creases
  ctx.save();
  ctx.clip(path);
  const n = Math.round((x1 - x0) * (y1 - y0) * part.density);
  for (let i = 0; i < n; i++) {
    const x = x0 + R() * (x1 - x0), y = y0 + R() * (y1 - y0);
    const len = l0 + R() * (l1 - l0), jit = R() - 0.5, bend = R() - 0.5, rot = R() - 0.5;
    if (!inside(x, y)) continue;
    const [dx, dy] = turn(part.flow(x, y), rot * 0.5);
    const c = col(x, y);
    const t = jit * 0.4;
    ctx.fillStyle = css(t > 0 ? mix(c, PAL.hi, t) : mix(c, PAL.deep, -t), 0.55);
    strand(ctx, x, y, dx, dy, len, part.w, bend * len * 0.35);
  }
  ctx.restore();

  // edge fur
  for (const q of contour(part)) {
    const k = 0.55 + R() * 0.8, rot = R() - 0.5, bend = R() - 0.5, jit = R() - 0.5;
    const L = part.edge(q.x, q.y, q.nx, q.ny) * k;
    if (L < 0.5) continue;
    const f = part.flow(q.x, q.y);
    const [dx, dy] = turn(unit(f[0] * part.lean + q.nx, f[1] * part.lean + q.ny), rot * 0.7);
    const c = col(q.x - q.nx * 2.5, q.y - q.ny * 2.5);
    ctx.fillStyle = css(jit > 0 ? mix(c, PAL.hi, jit * 0.25) : mix(c, PAL.deep, -jit * 0.2));
    strand(ctx, q.x - q.nx * L * 0.5, q.y - q.ny * L * 0.5, dx, dy, L, part.ew, bend * L * 0.45);
  }

  // grain
  ctx.save();
  ctx.clip(path);
  const g = Math.round((x1 - x0) * (y1 - y0) / 7);
  for (let i = 0; i < g; i++) {
    const x = x0 + R() * (x1 - x0), y = y0 + R() * (y1 - y0);
    ctx.fillStyle = R() < 0.5 ? 'rgba(255,248,235,.08)' : 'rgba(80,45,25,.07)';
    ctx.fillRect(x, y, 0.6, 0.6);
  }
  ctx.restore();
  if (part.after) part.after(ctx, s, R);
}

// A tapered stripe: wide at the spine, thinning to nothing down the side.
function taper(c, x, y, len, lean, w, wave) {
  const bx = x + lean, by = y + len;
  const cx = x + lean * 0.3 + wave, cy = y + len * 0.5;
  c.beginPath();
  c.moveTo(x - w / 2, y);
  c.quadraticCurveTo(cx - w * 0.3, cy, bx, by);
  c.quadraticCurveTo(cx + w * 0.3, cy, x + w / 2, y);
  c.fill();
}
// [x at spine, y at spine, length, lean, width, wave]
const STRIPES = [
  [304, 216, 54, -10, 10, 4], [336, 205, 68, -12, 12, -3], [370, 200, 78, -12, 13, 5],
  [404, 199, 82, -8, 12, -4], [438, 203, 78, -2, 12, 4], [468, 213, 66, 8, 11, -3],
  [492, 232, 48, 14, 10, 3], [320, 210, 20, -4, 6, 0], [421, 200, 26, -2, 7, 0],
];

// ---- parts ----------------------------------------------------------------
// Body, in world coordinates. Light comes from the upper left.
const BODY = {
  seed: 11,
  d: 'M 232 360 C 210 330 222 262 276 232 C 314 210 352 198 396 198 C 452 198 500 222 510 272 C 518 320 498 362 448 364 L 262 364 C 248 364 238 363 232 360 Z',
  box: [212, 192, 520, 366],
  density: 0.018, len: [4, 9], w: 0.7, ew: 0.95, lean: 1.1, step: 0.9,
  flow: (x, y) => unit(-(y - 345) + (x - 380) * 0.25, (x - 380) + (y - 345) * 0.25),
  edge: (x, y) => (y > 352 ? 1 : 3.2 + 3 * smooth(y, 200, 310)),
  base(ctx, s) {
    const g = ctx.createLinearGradient(0, 185, 0, 364);
    g.addColorStop(0, css(PAL.light));
    g.addColorStop(0.42, css(PAL.mid));
    g.addColorStop(1, css(PAL.shadow));
    ctx.fillStyle = g;
    ctx.fillRect(190, 130, 370, 240);
    glow(ctx, 346, 222, 140, PAL.light, 0.5);
    ctx.globalCompositeOperation = 'multiply';
    glow(ctx, 470, 342, 150, PAL.shadow, 0.55);
    // a darker band along the spine, then tapered stripes that wrap the barrel
    soft(ctx, s, 14, css(PAL.stripe, 0.45), (c) => strokeD(c, 26, 'M 280 236 C 314 214 352 203 396 203 C 450 203 494 224 504 270'));
    soft(ctx, s, 2.5, css(PAL.stripe, 0.6), (c) => {
      for (const [x, y, len, lean, w, wave] of STRIPES) taper(c, x, y, len, lean, w, wave);
    });
    // thigh: the curled hind leg, a soft crease with light on top of it
    soft(ctx, s, 9, css(PAL.deep, 0.3), (c) => strokeD(c, 9, 'M 422 262 C 404 296 410 334 436 356'));
    // the head throws a shadow onto the shoulder, and the floor darkens the base
    soft(ctx, s, 24, css(PAL.deep, 0.5), (c) => ellipse(c, 302, 304, 80, 66));
    soft(ctx, s, 8, css(PAL.deep, 0.55), (c) => c.fillRect(200, 356, 370, 16));
    ctx.globalCompositeOperation = 'source-over';
    soft(ctx, s, 3, css(PAL.rim, 0.85), (c) => strokeD(c, 5, 'M 276 232 C 314 210 352 198 396 198 C 452 198 500 222 510 272'));
    soft(ctx, s, 12, css(PAL.light, 0.3), (c) => strokeD(c, 18, 'M 452 240 C 476 254 486 282 480 308'));
    // pale chest showing between the front legs
    soft(ctx, s, 6, css(PAL.cream, 0.9), (c) => ellipse(c, 236, 346, 30, 20));
    // hind foot tucked under
    soft(ctx, s, 3, css(PAL.cream, 0.8), (c) => ellipse(c, 432, 358, 20, 7.5, -0.05));
    soft(ctx, s, 3, css(PAL.creamShade, 0.9), (c) => ellipse(c, 436, 362, 18, 3.5));
    soft(ctx, s, 2.5, css(PAL.shadow, 0.35), (c) => strokeD(c, 1.6, 'M 414 352 C 424 349 438 349 450 353'));
  },
};

// Head, local coordinates around its centre. Front-facing, chin tucked.
const HEAD = {
  seed: 23,
  d: 'M -86 14 C -90 -26 -60 -60 0 -62 C 60 -60 90 -26 86 14 C 84 44 50 62 0 62 C -50 62 -84 44 -86 14 Z',
  box: [-90, -64, 90, 64],
  density: 0.012, len: [3, 7], w: 0.6, ew: 0.9, lean: 0.9, step: 0.8,
  flow: (x, y) => unit(x, y - 26),
  edge: (x, y) => 1.8 + 8 * smooth(Math.abs(x), 46, 84) * smooth(y, -18, 30) + 1.2 * smooth(-y, 30, 60),
  base(ctx, s) {
    const g = ctx.createLinearGradient(0, -62, 0, 62);
    g.addColorStop(0, css(mix(PAL.mid, PAL.stripe, 0.15)));
    g.addColorStop(0.5, css(mix(PAL.mid, PAL.light, 0.55)));
    g.addColorStop(1, css(PAL.light));
    ctx.fillStyle = g;
    ctx.fillRect(-92, -66, 184, 132);
    glow(ctx, -38, -30, 74, PAL.light, 0.55);
    ctx.globalCompositeOperation = 'multiply';
    glow(ctx, 58, 36, 70, PAL.shadow, 0.45);
    // forehead "M", cheek bars and the line running back from each eye
    soft(ctx, s, 2.5, css(PAL.stripe, 0.7), (c) => strokeD(c, 6.5, 'M -21 -58 Q -18 -46 -11 -37', 'M 0 -61 L 0 -39', 'M 21 -58 Q 18 -46 11 -37'));
    soft(ctx, s, 2.5, css(PAL.stripe, 0.55), (c) => strokeD(c, 5,
      'M -88 -4 Q -74 -1 -62 6', 'M -86 14 Q -74 15 -64 20',
      'M 88 -4 Q 74 -1 62 6', 'M 86 14 Q 74 15 64 20',
      'M -47 -2 Q -60 -7 -72 -6', 'M 47 -2 Q 60 -7 72 -6'));
    soft(ctx, s, 5, css(PAL.deep, 0.2), (c) => { ellipse(c, -33, 3, 16, 8, 0.12); ellipse(c, 33, 3, 16, 8, -0.12); });
    soft(ctx, s, 6, css(PAL.deep, 0.35), (c) => ellipse(c, 0, 66, 72, 10));
    ctx.globalCompositeOperation = 'source-over';
    // cream muzzle, chin, cheek fluff and a lit bridge of the nose
    soft(ctx, s, 6, css(PAL.cream, 0.6), (c) => { ellipse(c, -46, 38, 30, 15, 0.2); ellipse(c, 46, 38, 30, 15, -0.2); });
    soft(ctx, s, 3, css(PAL.cream, 0.95), (c) => { ellipse(c, -13, 31, 17, 15); ellipse(c, 13, 31, 17, 15); ellipse(c, 0, 47, 22, 12); });
    soft(ctx, s, 3, css(PAL.cream, 0.45), (c) => { ellipse(c, -33, 13, 14, 5, 0.1); ellipse(c, 33, 13, 14, 5, -0.1); });
    soft(ctx, s, 6, css(PAL.hi, 0.3), (c) => strokeD(c, 9, 'M 0 -24 L 0 12'));
    soft(ctx, s, 3, css(PAL.rim, 0.75), (c) => strokeD(c, 4.5, 'M -84 -10 C -82 -40 -55 -62 0 -62 C 40 -61 70 -46 80 -24'));
  },
};

// Left ear, local to its base; the right ear is drawn mirrored.
const EAR = {
  seed: 37,
  d: 'M -30 6 C -33 -22 -28 -52 -18 -64 Q -13 -70 -7 -63 C 7 -48 20 -26 30 2 Z',
  box: [-34, -72, 34, 8],
  density: 0.008, len: [2, 5], w: 0.5, ew: 0.7, lean: 1.2, step: 0.8,
  flow: (x, y) => unit(-0.2, -1),
  edge: (x, y) => 1 + 1.4 * smooth(y, -30, 0),
  base(ctx, s) {
    const g = ctx.createLinearGradient(0, -70, 0, 6);
    g.addColorStop(0, css(mix(PAL.mid, PAL.stripe, 0.5)));
    g.addColorStop(1, css(PAL.mid));
    ctx.fillStyle = g;
    ctx.fillRect(-36, -74, 72, 84);
    const ig = ctx.createLinearGradient(0, -52, 0, 2);
    ig.addColorStop(0, css(mix(PAL.innerEar, PAL.hi, 0.25)));
    ig.addColorStop(1, css(mix(PAL.innerEar, PAL.shadow, 0.35)));
    ctx.fillStyle = ig;
    ctx.fill(new Path2D('M -19 0 C -22 -20 -19 -40 -13 -50 Q -10 -54 -7 -49 C 2 -36 10 -20 17 -2 Z'));
    ctx.globalCompositeOperation = 'multiply';
    soft(ctx, s, 6, css(PAL.deep, 0.35), (c) => ellipse(c, 0, 4, 22, 8));
    ctx.globalCompositeOperation = 'source-over';
    soft(ctx, s, 2, css(PAL.rim, 0.7), (c) => strokeD(c, 3, 'M -30 6 C -33 -22 -28 -52 -18 -64'));
  },
  after(ctx, s, R) {
    // pale tufts growing out of the inner ear
    ctx.fillStyle = 'rgba(255,249,238,.85)';
    for (let i = 0; i < 16; i++) {
      const x = -10 + R() * 22, y = -2 - R() * 12;
      const [dx, dy] = turn(unit(-0.3, -1), (R() - 0.5) * 0.7);
      strand(ctx, x, y, dx, dy, 9 + R() * 12, 0.9, (R() - 0.5) * 4);
    }
  },
};

// Front paw, local to its centre, toes to the left.
const PAW = {
  seed: 51,
  d: 'M -27 1 C -27 -11 -13 -16 2 -15 C 18 -14 27 -8 27 3 C 27 11 15 14 0 14 C -16 14 -27 10 -27 1 Z',
  box: [-29, -17, 29, 16],
  density: 0.02, len: [2, 4], w: 0.5, ew: 0.75, lean: 1.4, step: 0.7,
  flow: () => unit(-1, 0.15),
  edge: (x, y) => (y > 10 ? 0.8 : 2.2),
  base(ctx, s) {
    const g = ctx.createLinearGradient(0, -16, 0, 14);
    g.addColorStop(0, css(PAL.cream));
    g.addColorStop(1, css(PAL.creamShade));
    ctx.fillStyle = g;
    ctx.fillRect(-30, -18, 60, 34);
    ctx.globalCompositeOperation = 'multiply';
    soft(ctx, s, 5, css(PAL.deep, 0.25), (c) => ellipse(c, 2, -17, 34, 7));
    soft(ctx, s, 1.2, css(mix(PAL.creamShade, PAL.deep, 0.35), 0.8), (c) => strokeD(c, 1.4, 'M -16 13 Q -18 7 -15 2', 'M -6 14 Q -7 8 -5 3'));
    ctx.globalCompositeOperation = 'source-over';
  },
};

// ---- sprite cache ---------------------------------------------------------
const PAD = 16;
function bake(part, scale) {
  const [x0, y0, x1, y1] = part.box;
  const big = Math.max(x1 - x0, y1 - y0) + 2 * PAD;
  const s = Math.min(scale, 4000 / big);
  const cv = document.createElement('canvas');
  cv.width = Math.ceil((x1 - x0 + 2 * PAD) * s);
  cv.height = Math.ceil((y1 - y0 + 2 * PAD) * s);
  const ctx = cv.getContext('2d', { willReadFrequently: true });
  ctx.setTransform(s, 0, 0, s, -(x0 - PAD) * s, -(y0 - PAD) * s);
  paintFur(ctx, s, part);
  return { cv, x: x0 - PAD, y: y0 - PAD, w: cv.width / s, h: cv.height / s };
}
const sprites = new Map();
// Bakes are shared in steps of sqrt(2), so instances at similar sizes reuse them.
function spritesAt(scale) {
  const key = Math.min(8, 2 ** (Math.ceil(Math.log2(Math.max(1, scale)) * 2) / 2));
  if (!sprites.has(key)) sprites.set(key, { body: bake(BODY, key), head: bake(HEAD, key), ear: bake(EAR, key), paw: bake(PAW, key) });
  return sprites.get(key);
}
const blit = (ctx, sp) => ctx.drawImage(sp.cv, sp.x, sp.y, sp.w, sp.h);

// ---- live parts -----------------------------------------------------------
const TAIL = [[494, 318], [484, 366], [424, 384], [356, 383], [314, 370], [295, 351]];
const TAIL_STRANDS = (() => {
  const R = rng(71), out = [];
  for (let i = 0; i < 300; i++) out.push({ u: 0.1 + R() * 0.88, side: R() < 0.5 ? 1 : -1, len: 2.5 + R() * 3, rot: R() - 0.5, bend: R() - 0.5, jit: R() - 0.5 });
  for (let i = 0; i < 110; i++) out.push({ u: 0.04 + R() * 0.9, v: (R() - 0.5) * 1.5, len: 5 + R() * 5, rot: R() - 0.5, bend: R() - 0.5, jit: R() - 0.5 });
  return out;
})();
// [position, half-width]
const TAIL_RINGS = [[0.2, 0.018], [0.31, 0.024], [0.43, 0.02], [0.55, 0.026], [0.66, 0.022], [0.76, 0.024], [0.85, 0.02]];

function catmull(pts, n) {
  const out = [];
  const seg = pts.length - 1;
  for (let i = 0; i <= n; i++) {
    const f = (i / n) * seg, k = Math.min(seg - 1, Math.floor(f)), t = f - k;
    const p0 = pts[Math.max(0, k - 1)], p1 = pts[k], p2 = pts[k + 1], p3 = pts[Math.min(seg, k + 2)];
    const q = (a, b, c, d) => 0.5 * (2 * b + (-a + c) * t + (2 * a - 5 * b + 4 * c - d) * t * t + (-a + 3 * b - 3 * c + d) * t * t * t);
    out.push([q(p0[0], p1[0], p2[0], p3[0]), q(p0[1], p1[1], p2[1], p3[1])]);
  }
  return out;
}
function spin(pts, from, at, a) {
  const [cx, cy] = pts[at];
  for (let i = from; i < pts.length; i++) {
    const [x, y] = turn([pts[i][0] - cx, pts[i][1] - cy], a);
    pts[i] = [cx + x, cy + y];
  }
}

function drawTail(ctx, p) {
  const pts = TAIL.map((q) => q.slice());
  spin(pts, 3, 2, 0.16 * p.tlift);
  spin(pts, 4, 3, 0.5 * p.curl);
  const N = 48;
  const c = catmull(pts, N);
  const rad = (u) => 16 - 5 * u * u;
  const frame = c.map((q, i) => {
    const a = c[Math.max(0, i - 1)], b = c[Math.min(N, i + 1)];
    const [tx, ty] = unit(b[0] - a[0], b[1] - a[1]);
    return { x: q[0], y: q[1], tx, ty, nx: -ty, ny: tx, r: rad(i / N) };
  });
  const at = (u) => frame[Math.round(clamp(u, 0, 1) * N)];

  const shape = new Path2D();
  frame.forEach((f, i) => (i ? shape.lineTo : shape.moveTo).call(shape, f.x + f.nx * f.r, f.y + f.ny * f.r));
  const tip = frame[N];
  shape.arc(tip.x, tip.y, tip.r, Math.atan2(tip.ny, tip.nx), Math.atan2(-tip.ny, -tip.nx));
  for (let i = N; i >= 0; i--) shape.lineTo(frame[i].x - frame[i].nx * frame[i].r, frame[i].y - frame[i].ny * frame[i].r);
  shape.closePath();

  ctx.fillStyle = css(PAL.shadow);
  ctx.fill(shape);

  ctx.save();
  ctx.clip(shape);
  // round it: bands along the tail, brighter toward whichever side faces up
  ctx.lineCap = 'round';
  ctx.lineJoin = 'round';
  const band = (v, w, color, a) => {
    ctx.beginPath();
    frame.forEach((f, i) => {
      const up = f.ny < 0 ? 1 : -1;
      (i ? ctx.lineTo : ctx.moveTo).call(ctx, f.x + f.nx * f.r * v * up, f.y + f.ny * f.r * v * up);
    });
    ctx.strokeStyle = css(color, a);
    ctx.lineWidth = w;
    ctx.stroke();
  };
  band(0.05, 27, PAL.mid, 1);
  for (let i = 1; i <= 8; i++) band(0.06 * i, 26 - i * 2.8, mix(PAL.mid, PAL.light, i / 8), 0.28);
  band(0.5, 3, PAL.hi, 0.25);
  for (let i = 0; i < 3; i++) band(-0.95, 4 + i * 4, PAL.deep, 0.12);
  // soft rings, bowed toward the tip so the tail reads round
  for (const [u, hw] of TAIL_RINGS) for (const [wk, al] of [[1.7, 0.1], [1, 0.2]]) {
    ctx.fillStyle = css(PAL.stripe, al);
    const a = at(u - hw * wk), b = at(u + hw * wk), m = at(u);
    const bow = m.r * 0.35;
    ctx.beginPath();
    ctx.moveTo(a.x + a.nx * a.r * 1.2, a.y + a.ny * a.r * 1.2);
    ctx.quadraticCurveTo(a.x + a.tx * bow, a.y + a.ty * bow, a.x - a.nx * a.r * 1.2, a.y - a.ny * a.r * 1.2);
    ctx.lineTo(b.x - b.nx * b.r * 1.2, b.y - b.ny * b.r * 1.2);
    ctx.quadraticCurveTo(b.x + b.tx * bow, b.y + b.ty * bow, b.x + b.nx * b.r * 1.2, b.y + b.ny * b.r * 1.2);
    ctx.fill();
  }
  ctx.fillStyle = css(PAL.stripe, 0.35);
  ellipse(ctx, tip.x, tip.y, tip.r * 1.3, tip.r * 1.3);
  ctx.restore();

  // fur, all pointing toward the tip
  for (const h of TAIL_STRANDS) {
    const f = at(h.u);
    let x, y, dx, dy;
    if (h.side) {
      x = f.x + f.nx * f.r * h.side * 0.85; y = f.y + f.ny * f.r * h.side * 0.85;
      [dx, dy] = turn(unit(f.tx + f.nx * h.side * 0.55, f.ty + f.ny * h.side * 0.55), h.rot * 0.5);
    } else {
      x = f.x + f.nx * f.r * h.v * 0.6; y = f.y + f.ny * f.r * h.v * 0.6;
      [dx, dy] = turn([f.tx, f.ty], h.rot * 0.4);
    }
    const lit = h.side ? (f.ny * h.side < 0 ? 0.35 : -0.25) : -(h.v || 0) * 0.2 * Math.sign(f.ny || 1);
    const base = mix(PAL.mid, lit > 0 ? PAL.hi : PAL.deep, Math.abs(lit) + h.jit * 0.15);
    ctx.fillStyle = css(base, h.side ? 0.95 : 0.4);
    strand(ctx, x, y, dx, dy, h.len, h.side ? 0.75 : 0.55, h.bend * h.len * 0.4);
  }
}

function drawEye(ctx, x, y, rot, open, outer) {
  ctx.save();
  ctx.translate(x, y);
  ctx.rotate(rot);
  const w = 13;
  const squeeze = Math.max(0, -open);
  const o = Math.max(0, open);
  const up = o > 0 ? 2 - 24 * o : 2 + 5 * squeeze;
  const low = 8 + 5 * squeeze;
  if (o > 0.03) {
    const ap = new Path2D(`M ${-w} 0 Q 0 ${up} ${w} 0 Q 0 ${low} ${-w} 0 Z`);
    ctx.save();
    ctx.clip(ap);
    const g = ctx.createRadialGradient(0, 1, 1, 0, 1, 12);
    g.addColorStop(0, css(mix(PAL.iris, PAL.hi, 0.3)));
    g.addColorStop(0.65, css(PAL.iris));
    g.addColorStop(1, css(PAL.irisDark));
    ctx.fillStyle = g;
    ctx.fillRect(-w, -14, w * 2, 20);
    ctx.fillStyle = css(PAL.ink);
    ellipse(ctx, 0, 1.5, 2.1 + 1.2 * (1 - o), 8.5);
    const lid = ctx.createLinearGradient(0, up / 2, 0, up / 2 + 6);
    lid.addColorStop(0, 'rgba(60,35,20,.5)');
    lid.addColorStop(1, 'rgba(60,35,20,0)');
    ctx.fillStyle = lid;
    ctx.fillRect(-w, -14, w * 2, 20);
    ctx.fillStyle = 'rgba(255,255,255,.9)';
    ellipse(ctx, 3.5, up / 2 + 3.2, 1.6, 1.4);
    ctx.restore();
    ctx.strokeStyle = css(PAL.ink, 0.45);
    ctx.lineWidth = 0.9;
    ctx.stroke(new Path2D(`M ${-w} 0 Q 0 ${low} ${w} 0`));
  }
  // the lash line: a crescent along the upper lid, thickest in the middle
  const th = o > 0.03 ? 4.5 : 6;
  ctx.fillStyle = css(PAL.ink);
  ctx.fill(new Path2D(`M ${-w - 0.5} 0 Q 0 ${up} ${w + 0.5} 0 Q 0 ${up + th} ${-w - 0.5} 0 Z`));
  strand(ctx, outer * w, 0, ...unit(outer * 0.9, -0.5), 4.5, 1.8, 0);
  ctx.restore();
}

function drawNose(ctx, n) {
  ctx.save();
  ctx.translate(0, 21 - 0.9 * n);
  ctx.scale(1 + 0.07 * n, 1 - 0.05 * n);
  const g = ctx.createLinearGradient(0, -5, 0, 6);
  g.addColorStop(0, css(mix(PAL.nose, PAL.hi, 0.25)));
  g.addColorStop(1, css(PAL.noseDark));
  ctx.fillStyle = g;
  ctx.fill(new Path2D('M -7.5 -2.5 Q 0 -5 7.5 -2.5 Q 7.5 0.5 2 5 Q 0 6.5 -2 5 Q -7.5 0.5 -7.5 -2.5 Z'));
  ctx.fillStyle = 'rgba(255,255,255,.45)';
  ellipse(ctx, -2.2, -2, 2.2, 1);
  ctx.restore();
}

function drawMouth(ctx, y) {
  ctx.strokeStyle = css(PAL.ink, 0.75);
  ctx.lineWidth = 1.3;
  ctx.lineCap = 'round';
  if (y < 0.03) {
    ctx.stroke(new Path2D('M 0 26 L 0 29.5 M 0 29.5 Q -3 34 -8.5 32 M 0 29.5 Q 3 34 8.5 32'));
    return;
  }
  const w = 8.5 + 8 * y;
  const m = new Path2D(`M ${-w} 29 Q 0 ${29 - 6 * y} ${w} 29 Q ${w * 0.95} ${29 + 26 * y} 0 ${29 + 32 * y} Q ${-w * 0.95} ${29 + 26 * y} ${-w} 29 Z`);
  ctx.fillStyle = '#5e2b2b';
  ctx.fill(m);
  ctx.save();
  ctx.clip(m);
  ctx.fillStyle = '#e48a88';
  ellipse(ctx, 0, 29 + 28 * y, w * 0.7, 11 * y);
  ctx.fillStyle = 'rgba(255,255,255,.3)';
  ellipse(ctx, -2.5, 29 + 22 * y, w * 0.22, 2.4 * y);
  ctx.restore();
  ctx.fillStyle = '#fffaf2';
  for (const sx of [-1, 1]) {
    ctx.beginPath();
    ctx.moveTo(sx * (w - 5), 28.5);
    ctx.lineTo(sx * (w - 1.8), 28.5);
    ctx.lineTo(sx * (w - 3.6), 28.5 + 6.5 * y);
    ctx.fill();
  }
  ctx.stroke(m);
  ctx.stroke(new Path2D('M 0 26 L 0 28'));
}

const WHISKERS = [[-16, 29, -58, 16, -96, 24], [-17, 33, -60, 32, -100, 44], [-15, 37, -54, 46, -88, 62]];
const BROWS = [[-40, -14, -48, -24, -52, -34], [-45, -12, -55, -19, -61, -27]];
function drawWhiskers(ctx, p) {
  const a = 0.15 * p.whisk + 0.12 * p.yawn + 0.02 * p.breath;
  ctx.lineCap = 'round';
  for (const side of [1, -1]) {
    ctx.save();
    ctx.scale(side, 1);
    for (const [set, lift] of [[WHISKERS, a], [BROWS, a * 0.6 + 0.25 * p.earsUp]]) {
      const fine = set === BROWS;
      for (const [rx, ry, cx, cy, tx, ty] of set) {
        ctx.save();
        ctx.translate(rx, ry);
        ctx.rotate(lift);
        const d = new Path2D(`M 0 0 Q ${cx - rx} ${cy - ry} ${tx - rx} ${ty - ry}`);
        ctx.strokeStyle = 'rgba(110,70,45,.16)';
        ctx.lineWidth = 1.3;
        ctx.stroke(d);
        ctx.strokeStyle = fine ? 'rgba(255,252,246,.55)' : 'rgba(255,252,246,.9)';
        ctx.lineWidth = fine ? 0.55 : 0.7;
        ctx.stroke(d);
        ctx.restore();
      }
    }
    ctx.restore();
  }
}

// ---- motion ---------------------------------------------------------------
const REST = { breath: 0, earL: 0, earR: 0, earLSq: 0, earRSq: 0, earsBack: 0, earsUp: 0, eyeL: 0, eyeR: 0, head: 0, yawn: 0, shiver: 0, paw: 0, whisk: 0, nose: 0, curl: 0, tlift: 0, sigh: 0, look: 0 };

export const CLIPS = {
  // a single ear flicks back, sometimes twice
  flick: {
    dur: 1.3,
    init: (R) => ({ side: R() < 0.5 ? 'L' : 'R', twice: R() < 0.4, amp: 0.8 + R() * 0.4 }),
    fn(t, c) {
      const one = (x) => track(x, [[0, 0], [0.06, 1, 'out'], [0.16, -0.35], [0.28, 0.12], [0.44, 0]]);
      const v = one(t) * c.amp + (c.twice ? one(t - 0.5) * c.amp * 0.6 : 0);
      return { ['ear' + c.side]: v, ['ear' + c.side + 'Sq']: Math.max(0, v) * 0.5 };
    },
  },
  // the tip lifts, curls, flicks once and settles
  tail: {
    dur: 3.2,
    init: (R) => ({ amp: 0.75 + R() * 0.45 }),
    fn(t, c) {
      const curl = track(t, [[0, 0], [1.1, 1], [1.9, 0.75], [3.2, 0]]) * c.amp;
      const tlift = track(t, [[0, 0], [0.9, 1], [2.2, 0.25], [3.2, 0]]) * c.amp;
      const f = t > 1.1 && t < 1.9 ? Math.sin((t - 1.1) * 16) * 0.22 * (1 - (t - 1.1) / 0.8) : 0;
      return { curl: curl + f, tlift };
    },
  },
  // dreaming: paw paddles, whiskers and nose twitch, eyes flutter
  dream: {
    dur: 2.6,
    init: () => ({}),
    fn(t) {
      return {
        paw: track(t, [[0, 0], [0.08, 1, 'out'], [0.2, 0], [0.55, 0], [0.62, 0.7, 'out'], [0.75, 0], [1.3, 0], [1.36, 0.5, 'out'], [1.5, 0]]),
        whisk: track(t, [[0.3, 0], [0.36, 1, 'out'], [0.5, -0.3], [0.65, 0], [1.6, 0], [1.66, 0.8, 'out'], [1.8, -0.2], [2.0, 0]]),
        nose: track(t, [[0.2, 0], [0.26, 1, 'out'], [0.34, 0], [0.42, 1, 'out'], [0.5, 0], [1.9, 0], [1.96, 1, 'out'], [2.05, 0]]),
        eyeL: track(t, [[0.9, 0], [1.0, -0.35], [1.1, 0], [1.2, -0.3], [1.35, 0]]),
        eyeR: track(t, [[0.9, 0], [1.0, -0.35], [1.1, 0], [1.2, -0.3], [1.35, 0]]),
        earR: track(t, [[1.1, 0], [1.16, 0.4, 'out'], [1.3, 0]]),
      };
    },
  },
  // one eye opens a slit, checks the room, slow-blinks, closes
  peek: {
    dur: 3.8,
    init: () => ({}),
    fn(t) {
      return {
        eyeR: track(t, [[0, 0], [0.8, 0.42], [1.7, 0.42], [2.0, 0.08], [2.35, 0.42], [2.9, 0.4], [3.6, 0]]),
        eyeL: track(t, [[0.2, 0], [0.9, -0.25], [2.9, -0.25], [3.4, 0]]),
        earRSq: track(t, [[0, 0], [0.4, 0.6], [2.9, 0.6], [3.4, 0]]),
        earR: track(t, [[0, 0], [0.4, -0.3], [2.9, -0.3], [3.4, 0]]),
      };
    },
  },
  // tap: ears up, head lifts, sleepy look, slow blink, a big yawn, resettle
  wake: {
    dur: 7.2,
    init: () => ({}),
    fn(t) {
      const eye = track(t, [[0.9, 0], [1.7, 0.55], [2.1, 0.55], [2.3, 0.05], [2.6, 0.5], [2.9, 0.5], [3.15, -0.6], [4.3, -0.6], [4.6, 0.3], [5.3, 0.3], [6.0, 0]]);
      const yawn = track(t, [[2.95, 0], [3.55, 1, 'out'], [4.1, 0.95], [4.55, 0]]);
      return {
        earsUp: track(t, [[0, 0], [0.35, 1, 'out'], [2.8, 1], [3.0, 0]]),
        head: track(t, [[0.2, 0], [1.3, 1], [4.9, 1], [6.5, 0]]),
        eyeL: eye, eyeR: eye, yawn,
        earsBack: track(t, [[2.9, 0], [3.3, 1], [4.2, 1], [4.6, 0]]),
        whisk: track(t, [[2.9, 0], [3.4, 1], [4.2, 1], [4.6, 0]]),
        shiver: t > 3.5 && t < 4.2 ? Math.sin(t * 60) * 0.006 * Math.sin(((t - 3.5) / 0.7) * Math.PI) : 0,
        sigh: track(t, [[4.7, 0], [5.3, 1], [6.8, 0]]),
        curl: track(t, [[5.2, 0], [6.2, 0.8], [7.2, 0]]),
        tlift: track(t, [[5.0, 0], [6.0, 0.6], [7.2, 0]]),
      };
    },
  },
};
const AUTO = { flick: [3.5, 9], tail: [4.5, 9], dream: [12, 22], peek: [18, 30] };

// Breathing: quicker inhale, slower exhale, a pause; every cycle a bit different.
function breather(R) {
  let start = 0, dur = 3.8, amp = 1;
  return (t) => {
    while (t > start + dur) { start += dur; dur = 3.3 + R() * 1.0; amp = 0.85 + R() * 0.25; }
    const q = (t - start) / dur;
    if (q < 0.36) return E.io(q / 0.36) * amp;
    if (q < 0.84) return (1 - E.io(((q - 0.36) / 0.48) ** 0.8)) * amp;
    return 0;
  };
}

// ---- the instance ---------------------------------------------------------
const HEAD_AT = [238, 276];
const R0 = 0.04;
const NECK = [64, 36];
const NECK_W = (() => { const [x, y] = turn(NECK, R0); return [HEAD_AT[0] + x, HEAD_AT[1] + y]; })();
const PAWS = [[197, 347, 0], [266, 351, 0.06]];

export function createCat(canvas, opts = {}) {
  const o = { view: [118, 110, 456, 300], mode: 'auto', clip: null, gap: 1.6, seed: 5, interactive: false, still: false, ...opts };
  const ctx = canvas.getContext('2d');
  const R = rng(o.seed);
  const breath = breather(rng(o.seed + 1));
  const active = [];
  const next = {};
  for (const [k, [a, b]] of Object.entries(AUTO)) next[k] = a + R() * (b - a);
  let nextOnly = 0.6;
  let sp = null, k = 1, dpr = 1, offX = 0, offY = 0;
  let time = 0, last = 0, speed = 1, raf = 0, visible = false;
  let look = 0, lookTarget = 0;
  let tail = null;

  // The tail only changes while it moves, so keep its last render.
  const TB = [250, 280, 300, 135];
  function tailLayer(p) {
    const S = k * dpr;
    const key = `${p.curl.toFixed(3)} ${p.tlift.toFixed(3)} ${S}`;
    if (tail && tail.key === key) return tail.cv;
    const cv = tail && tail.S === S ? tail.cv : document.createElement('canvas');
    cv.width = Math.ceil(TB[2] * S);
    cv.height = Math.ceil(TB[3] * S);
    const c = cv.getContext('2d');
    c.setTransform(1, 0, 0, 1, 0, 0);
    c.clearRect(0, 0, cv.width, cv.height);
    c.setTransform(S, 0, 0, S, -TB[0] * S, -TB[1] * S);
    drawTail(c, p);
    tail = { key, cv, S };
    return cv;
  }

  function layout() {
    const cw = canvas.clientWidth, ch = canvas.clientHeight;
    if (!cw || !ch) return;
    dpr = Math.min(devicePixelRatio || 1, 2);
    const [vx, vy, vw, vh] = o.view;
    k = Math.min(cw / vw, ch / vh);
    offX = (cw - vw * k) / 2 - vx * k;
    offY = (ch - vh * k) / 2 - vy * k;
    canvas.width = Math.round(cw * dpr);
    canvas.height = Math.round(ch * dpr);
    // bake only once the canvas is on screen
    sp = visible ? spritesAt(k * dpr) : null;
    draw(sample(0));
  }

  function play(name) {
    for (let i = active.length - 1; i >= 0; i--) if (active[i].name === name) active.splice(i, 1);
    active.push({ name, t0: time, c: CLIPS[name].init(R) });
    start();
  }

  function schedule() {
    if (o.mode === 'only' && o.clip && time >= nextOnly) {
      play(o.clip);
      nextOnly = time + CLIPS[o.clip].dur + o.gap;
    }
    if (o.mode !== 'auto') return;
    const busy = active.some((a) => a.name === 'wake');
    for (const name of Object.keys(AUTO)) {
      if (time < next[name]) continue;
      const [a, b] = AUTO[name];
      if (busy || active.some((x) => x.name === name)) { next[name] = time + 2; continue; }
      play(name);
      next[name] = time + a + R() * (b - a);
    }
  }

  function sample(dt) {
    const p = { ...REST };
    p.breath = o.still && !active.length ? 0.3 : breath(time);
    for (let i = active.length - 1; i >= 0; i--) {
      const a = active[i], lt = time - a.t0;
      if (lt > CLIPS[a.name].dur) { active.splice(i, 1); continue; }
      const out = CLIPS[a.name].fn(lt, a.c);
      for (const key in out) p[key] += out[key];
    }
    p.breath += p.sigh * 0.9;
    look += (lookTarget - look) * Math.min(1, dt * 4);
    p.look = look;
    p.eyeL = clamp(p.eyeL, -1, 1);
    p.eyeR = clamp(p.eyeR, -1, 1);
    return p;
  }

  function headXf(p) {
    ctx.translate(NECK_W[0], NECK_W[1] - 3 * p.breath - 12 * p.head - 4 * p.yawn);
    ctx.rotate(R0 + 0.2 * p.head + 0.17 * p.yawn + p.shiver - 0.012 * p.breath);
    ctx.translate(-NECK[0], -NECK[1]);
  }

  function ear(x, mirror, rot, sq, back) {
    ctx.save();
    ctx.translate(x, -46);
    if (mirror) ctx.scale(-1, 1);
    ctx.rotate(rot);
    ctx.scale(1 - 0.22 * clamp(sq, 0, 1), 1 - 0.16 * back);
    blit(ctx, sp.ear);
    ctx.restore();
  }

  function draw(p) {
    if (!sp) return;
    ctx.setTransform(1, 0, 0, 1, 0, 0);
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    ctx.setTransform(k * dpr, 0, 0, k * dpr, offX * dpr, offY * dpr);
    ctx.imageSmoothingQuality = 'high';

    // floor shadow
    ctx.save();
    ctx.translate(352, G + 3);
    ctx.scale(1, 0.09);
    glow(ctx, 0, 0, 220, [96, 62, 40], 0.3 - 0.02 * p.breath);
    ctx.scale(1, 0.5);
    glow(ctx, -6, 0, 170, [80, 50, 32], 0.28);
    ctx.restore();

    // body breathes from the floor up
    ctx.save();
    ctx.translate(372, G);
    ctx.scale(1 + 0.006 * p.breath, 1 + 0.024 * p.breath);
    ctx.translate(-372, -G);
    blit(ctx, sp.body);
    ctx.restore();

    // head, ears and face
    ctx.save();
    headXf(p);
    const swL = Math.max(0, -p.look) * 0.5, swR = Math.max(0, p.look) * 0.5;
    ear(-46, false, -0.26 - 0.42 * p.earL - 0.55 * p.earsBack + 0.16 * p.earsUp + 0.16 * p.look, p.earLSq + swL, p.earsBack);
    ear(46, true, -0.26 - 0.42 * p.earR - 0.55 * p.earsBack + 0.16 * p.earsUp - 0.16 * p.look, p.earRSq + swR, p.earsBack);
    blit(ctx, sp.head);
    drawEye(ctx, -33, 3, 0.12, p.eyeL, -1);
    drawEye(ctx, 33, 3, -0.12, p.eyeR, 1);
    drawMouth(ctx, p.yawn);
    drawNose(ctx, p.nose);
    ctx.restore();

    // front paws, chin resting on them
    PAWS.forEach(([x, y, r], i) => {
      ctx.save();
      ctx.translate(x, y);
      ctx.rotate(r);
      if (i === 0 && p.paw) { ctx.translate(20, -6); ctx.rotate(-0.16 * p.paw); ctx.scale(1 + 0.04 * p.paw, 1); ctx.translate(-20, 6); }
      blit(ctx, sp.paw);
      ctx.restore();
    });

    ctx.save();
    headXf(p);
    drawWhiskers(ctx, p);
    ctx.restore();

    ctx.drawImage(tailLayer(p), TB[0], TB[1], TB[2], TB[3]);
    ctx.save();
    // the rump covers the root of the tail, which comes out from under it
    ctx.beginPath();
    ctx.rect(462, 180, 90, 172);
    ctx.clip();
    ctx.translate(372, G);
    ctx.scale(1 + 0.006 * p.breath, 1 + 0.024 * p.breath);
    ctx.translate(-372, -G);
    blit(ctx, sp.body);
    ctx.restore();
  }

  function running() {
    if (!visible || o.mode === 'static') return false;
    return !o.still || active.length > 0;
  }
  function tick(now) {
    raf = 0;
    const dt = last ? Math.min(0.05, (now - last) / 1000) : 0;
    last = now;
    time += dt * speed;
    if (!o.still) schedule();
    draw(sample(dt * speed));
    if (running()) raf = requestAnimationFrame(tick);
    else last = 0;
  }
  function start() {
    if (!raf && running()) raf = requestAnimationFrame(tick);
  }

  const ro = new ResizeObserver(() => layout());
  ro.observe(canvas);
  const io = new IntersectionObserver(([e]) => {
    visible = e.isIntersecting;
    if (visible && !sp) layout();
    start();
  }, { rootMargin: '80px' });
  io.observe(canvas);

  if (o.interactive) {
    canvas.addEventListener('pointermove', (e) => {
      const r = canvas.getBoundingClientRect();
      const wx = (e.clientX - r.left - offX) / k;
      lookTarget = clamp((wx - HEAD_AT[0]) / 220, -1, 1);
    });
    canvas.addEventListener('pointerleave', () => { lookTarget = 0; });
    canvas.addEventListener('click', () => api.wake());
  }

  layout();
  start();

  const api = {
    replay() {
      if (!o.clip) return;
      play(o.clip);
      nextOnly = time + CLIPS[o.clip].dur + o.gap;
    },
    wake() {
      if (active.some((a) => a.name === 'wake')) return;
      for (let i = active.length - 1; i >= 0; i--) if (active[i].name !== 'flick') active.splice(i, 1);
      play('wake');
    },
    setSpeed(v) { speed = v; },
    // review helper: stop and draw one clip at a fixed moment
    freeze(name, t) {
      cancelAnimationFrame(raf);
      raf = 0;
      o.mode = 'static';
      active.length = 0;
      if (name) active.push({ name, t0: time - t, c: CLIPS[name].init(rng(3)) });
      draw(sample(0));
    },
    destroy() { ro.disconnect(); io.disconnect(); cancelAnimationFrame(raf); },
  };
  return api;
}
