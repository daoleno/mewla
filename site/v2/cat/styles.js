// One sleeping cat, drawn in many styles. Each style is a small canvas renderer
// over the same silhouette, so only the way of drawing changes between cards.

const W = 400, H = 280;
const TAU = Math.PI * 2;
const clamp = (v, a, b) => Math.min(b, Math.max(a, v));

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

// ---- the cat ---------------------------------------------------------------
// Cat coordinates; toCat() moves them into the middle of the 400x280 frame.
const ell = (cx, cy, rx, ry) => `M ${cx - rx} ${cy} A ${rx} ${ry} 0 1 0 ${cx + rx} ${cy} A ${rx} ${ry} 0 1 0 ${cx - rx} ${cy} Z`;

function spline(pts, n = 10) {
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

function normals(c) {
  return c.map((p, i) => {
    const a = c[Math.max(0, i - 1)], b = c[Math.min(c.length - 1, i + 1)];
    const dx = b[0] - a[0], dy = b[1] - a[1], l = Math.hypot(dx, dy) || 1;
    return [-dy / l, dx / l];
  });
}

const TAIL = [[382, 222], [388, 246], [354, 263], [272, 268], [204, 264], [162, 255]];
const tailR = (u) => 14 - 5 * u;
const TAIL_C = spline(TAIL, 10);

function tailPath() {
  const c = TAIL_C, nm = normals(c), L = [], R = [];
  c.forEach((p, i) => {
    const r = tailR(i / (c.length - 1));
    L.push([p[0] + nm[i][0] * r, p[1] + nm[i][1] * r]);
    R.push([p[0] - nm[i][0] * r, p[1] - nm[i][1] * r]);
  });
  const e = c[c.length - 1], n = nm[nm.length - 1], f = [n[1], -n[0]], r = tailR(1);
  const tip = [];
  for (let k = 1; k < 8; k++) {
    const t = (k / 8) * Math.PI;
    tip.push([e[0] + r * (n[0] * Math.cos(t) + f[0] * Math.sin(t)), e[1] + r * (n[1] * Math.cos(t) + f[1] * Math.sin(t))]);
  }
  const pts = [...L, ...tip, ...R.reverse()];
  return 'M ' + pts.map((p) => `${p[0].toFixed(1)} ${p[1].toFixed(1)}`).join(' L ') + ' Z';
}

const PART = {
  tail: tailPath(),
  body: 'M 150 244 C 136 206 160 150 222 128 C 276 110 338 116 368 148 C 394 176 396 226 380 244 C 360 256 300 257 250 256 C 210 256 170 253 150 244 Z',
  earL: 'M 72 180 C 70 160 74 138 82 124 C 94 132 106 142 114 154 Z',
  earR: 'M 130 154 C 140 142 152 132 166 124 C 172 138 174 160 172 182 Z',
  head: 'M 66 196 C 64 164 90 146 122 146 C 154 146 180 164 178 196 C 176 222 152 236 122 236 C 92 236 68 222 66 196 Z',
  pawL: ell(98, 241, 20, 9),
  pawR: ell(146, 243, 20, 9),
};
const ORDER = ['tail', 'body', 'earL', 'earR', 'head', 'pawL', 'pawR'];
const INNER = {
  earL: 'M 80 172 C 79 158 81 144 85 134 C 93 141 100 148 106 156 Z',
  earR: 'M 138 156 C 145 148 153 140 162 134 C 165 146 166 160 165 174 Z',
};
const FACE = {
  eyeL: 'M 90 193 Q 100 201 110 193',
  eyeR: 'M 134 193 Q 144 201 154 193',
  nose: 'M 117.5 205 Q 122 203.5 126.5 205 L 122 210 Z',
  mouth: 'M 122 210 L 122 213 M 115 213 Q 118.5 217 122 213 Q 125.5 217 129 213',
};
const WHISK = ['M 104 210 Q 86 205 64 206', 'M 104 214 Q 86 214 66 220', 'M 140 210 Q 158 205 180 206', 'M 140 214 Q 158 214 178 220'];
const STRIPES = [
  [[200, 150], [198, 166], [204, 184]], [[226, 132], [224, 154], [230, 176]], [[256, 120], [256, 144], [262, 166]],
  [[288, 116], [290, 140], [296, 160]], [[318, 120], [322, 142], [330, 160]], [[346, 134], [352, 150], [360, 166]],
  [[370, 160], [376, 174], [382, 188]],
];
const HEAD_STRIPES = [[[110, 152], [112, 160], [113, 168]], [[122, 149], [122, 158], [122, 167]], [[134, 152], [132, 160], [131, 168]]];
const CHEEK = [[[70, 200], [80, 202], [88, 206]], [[72, 210], [80, 211], [87, 214]], [[174, 200], [164, 202], [156, 206]], [[172, 210], [164, 211], [157, 214]]];
const CAT = [-32, -45];
const toCat = (c) => c.translate(CAT[0], CAT[1]);
const canvasPt = (x, y) => [x + CAT[0], y + CAT[1]];

const P = {};
const path = (d) => P[d] || (P[d] = new Path2D(d));
const hit = document.createElement('canvas').getContext('2d');
const inPart = (n, x, y) => hit.isPointInPath(path(PART[n]), x, y);
const inCatXY = (x, y) => ORDER.some((n) => inPart(n, x, y));

let svgPath = null;
function polyOf(d, n, closed = true) {
  if (!svgPath) {
    const NS = 'http://www.w3.org/2000/svg';
    const svg = document.createElementNS(NS, 'svg');
    svg.setAttribute('width', '0');
    svg.setAttribute('height', '0');
    svg.style.position = 'absolute';
    svgPath = document.createElementNS(NS, 'path');
    svg.appendChild(svgPath);
    document.body.appendChild(svg);
  }
  svgPath.setAttribute('d', d);
  const L = svgPath.getTotalLength(), out = [];
  for (let i = 0; i < n; i++) {
    const p = svgPath.getPointAtLength((i / (closed ? n : n - 1)) * L);
    out.push([p.x, p.y]);
  }
  return out;
}

// ---- drawing helpers -------------------------------------------------------
// Soft shapes via an offset shadow, which works where ctx.filter does not.
function soft(c, blur, color, fn) {
  const m = c.getTransform();
  c.save();
  c.shadowColor = color;
  c.shadowBlur = blur * m.a;
  c.shadowOffsetX = 5000 * m.a;
  c.translate(-5000, 0);
  c.fillStyle = c.strokeStyle = '#000';
  fn(c);
  c.restore();
}
const blob = (x, y, rx, ry, rot = 0) => (s) => { s.beginPath(); s.ellipse(x, y, rx, ry, rot, 0, TAU); s.fill(); };

let noiseCv = null;
function noise() {
  if (noiseCv) return noiseCv;
  noiseCv = document.createElement('canvas');
  noiseCv.width = noiseCv.height = 256;
  const c = noiseCv.getContext('2d');
  const img = c.createImageData(256, 256);
  const R = rng(7);
  for (let i = 0; i < img.data.length; i += 4) {
    const v = 128 + (R() - 0.5) * 230;
    img.data[i] = img.data[i + 1] = img.data[i + 2] = v;
    img.data[i + 3] = 255;
  }
  c.putImageData(img, 0, 0);
  return noiseCv;
}
function grain(c, alpha, mode = 'overlay') {
  c.save();
  c.setTransform(1, 0, 0, 1, 0, 0);
  c.globalAlpha = alpha;
  c.globalCompositeOperation = mode;
  c.fillStyle = c.createPattern(noise(), 'repeat');
  c.fillRect(0, 0, c.canvas.width, c.canvas.height);
  c.restore();
}
function layer(c) {
  const cv = document.createElement('canvas');
  cv.width = c.canvas.width;
  cv.height = c.canvas.height;
  const l = cv.getContext('2d');
  l.setTransform(c.getTransform());
  return l;
}
function put(c, l, alpha = 1, mode = 'source-over') {
  c.save();
  c.setTransform(1, 0, 0, 1, 0, 0);
  c.globalAlpha = alpha;
  c.globalCompositeOperation = mode;
  c.drawImage(l.canvas, 0, 0);
  c.restore();
}
function paper(c, R, color, fibers = 160, fiber = 'rgba(120,100,70,.12)') {
  c.fillStyle = color;
  c.fillRect(0, 0, W, H);
  c.save();
  c.strokeStyle = fiber;
  c.lineWidth = 0.35;
  for (let i = 0; i < fibers; i++) {
    const x = R() * W, y = R() * H, a = R() * TAU, l = 4 + R() * 16;
    c.beginPath();
    c.moveTo(x, y);
    c.quadraticCurveTo(x + Math.cos(a + 0.6) * l * 0.5, y + Math.sin(a + 0.6) * l * 0.5, x + Math.cos(a) * l, y + Math.sin(a) * l);
    c.stroke();
  }
  c.restore();
}
function vignette(c, alpha, color = '0,0,0') {
  const g = c.createRadialGradient(W / 2, H / 2, H * 0.35, W / 2, H / 2, W * 0.62);
  g.addColorStop(0, `rgba(${color},0)`);
  g.addColorStop(1, `rgba(${color},${alpha})`);
  c.fillStyle = g;
  c.fillRect(0, 0, W, H);
}
const lin = (c, x0, y0, x1, y1, stops) => {
  const g = c.createLinearGradient(x0, y0, x1, y1);
  for (const [o, col] of stops) g.addColorStop(o, col);
  return g;
};
function smooth(pts) {
  const loop = [pts[pts.length - 1], ...pts, pts[0], pts[1]];
  const cc = spline(loop, 8).slice(8, 8 * (pts.length + 1) + 1);
  const p = new Path2D();
  cc.forEach(([x, y], i) => (i ? p.lineTo(x, y) : p.moveTo(x, y)));
  p.closePath();
  return p;
}
const quad = (c, s) => { c.beginPath(); c.moveTo(...s[0]); c.quadraticCurveTo(...s[1], ...s[2]); };
const fillAll = (c, color, names = ORDER) => { c.fillStyle = color; for (const n of names) c.fill(path(PART[n])); };
const strokeAll = (c, color, w, names = ORDER) => {
  c.save();
  c.strokeStyle = color;
  c.lineWidth = w;
  c.lineJoin = 'round';
  for (const n of names) c.stroke(path(PART[n]));
  c.restore();
};

// A dry brush: bristles along a spline, thinning and breaking up at the ends.
function brush(c, R, pts, w, color, { n = 14, dry = 0.2, alpha = 0.8, press = true } = {}) {
  const cc = spline(pts, 18), nm = normals(cc), N = cc.length;
  c.save();
  c.strokeStyle = color;
  c.lineCap = 'round';
  c.lineJoin = 'round';
  for (let k = 0; k < n; k++) {
    const o = n === 1 ? 0 : k / (n - 1) - 0.5;
    const edge = Math.abs(o) * 2;
    c.lineWidth = Math.max(0.35, (w / n) * (1.7 + 1.4 * R()));
    c.globalAlpha = alpha * (0.55 + 0.45 * R());
    c.beginPath();
    let pen = false, gap = 0;
    const start = R() * 0.08 * edge;
    const end = 1 - R() * dry * (0.3 + edge) * 0.9;
    for (let i = 0; i < N; i++) {
      const u = i / (N - 1);
      if (u < start || u > end) { pen = false; continue; }
      const p = press ? 0.35 + 0.65 * Math.sin(Math.PI * clamp(u * 0.9 + 0.08, 0, 1)) : 1;
      const x = cc[i][0] + nm[i][0] * o * w * p, y = cc[i][1] + nm[i][1] * o * w * p;
      if (gap > 0) { gap--; pen = false; continue; }
      if (R() < dry * 0.05 * (0.3 + edge) * (0.4 + u)) { gap = 3 + ((R() * 8) | 0); pen = false; continue; }
      if (!pen) { c.moveTo(x, y); pen = true; } else c.lineTo(x, y);
    }
    c.stroke();
  }
  c.restore();
}

function face(c, col, w = 1.6, o = {}) {
  c.save();
  c.strokeStyle = col;
  c.lineCap = 'round';
  c.lineJoin = 'round';
  c.lineWidth = w;
  c.stroke(path(FACE.eyeL));
  c.stroke(path(FACE.eyeR));
  c.lineWidth = w * 0.65;
  c.stroke(path(FACE.mouth));
  c.fillStyle = o.nose || col;
  c.fill(path(FACE.nose));
  if (o.whisk) {
    c.strokeStyle = o.whisk;
    c.lineWidth = o.ww || 0.5;
    for (const d of WHISK) c.stroke(path(d));
  }
  c.restore();
}

// A shaded colour map of the scene, used by the painterly styles to pick colours.
function shadeCat(c, pal) {
  const fillP = (n, style) => { c.fillStyle = style; c.fill(path(PART[n])); };
  soft(c, 10, pal.ground, blob(244, 254, 172, 13));
  fillP('tail', lin(c, 200, 246, 214, 282, [[0, pal.mid], [1, pal.dark]]));
  c.save();
  c.clip(path(PART.tail));
  c.strokeStyle = pal.stripe;
  c.lineWidth = 7;
  for (const x of [218, 258, 298, 338]) { c.beginPath(); c.moveTo(x, 246); c.lineTo(x - 8, 284); c.stroke(); }
  c.restore();
  fillP('body', lin(c, 236, 112, 320, 252, [[0, pal.light], [0.45, pal.mid], [1, pal.dark]]));
  c.save();
  c.clip(path(PART.body));
  c.strokeStyle = pal.stripe;
  c.lineCap = 'round';
  c.lineWidth = 9;
  for (const s of STRIPES) { quad(c, s); c.stroke(); }
  soft(c, 18, pal.shade, blob(174, 210, 44, 52));
  soft(c, 14, pal.shade, blob(262, 248, 120, 14));
  c.restore();
  for (const n of ['earL', 'earR']) fillP(n, lin(c, 80, 124, 160, 180, [[0, pal.light], [1, pal.mid]]));
  c.fillStyle = pal.inner;
  for (const n of ['earL', 'earR']) c.fill(path(INNER[n]));
  fillP('head', lin(c, 90, 150, 150, 238, [[0, pal.light], [0.55, pal.mid], [1, pal.dark]]));
  c.save();
  c.clip(path(PART.head));
  c.strokeStyle = pal.stripe;
  c.lineCap = 'round';
  c.lineWidth = 4;
  for (const s of [...HEAD_STRIPES, ...CHEEK]) { quad(c, s); c.stroke(); }
  c.fillStyle = pal.cream;
  for (const [x, y, rx, ry] of [[113, 214, 11, 8], [131, 214, 11, 8], [122, 226, 18, 9]]) { c.beginPath(); c.ellipse(x, y, rx, ry, 0, 0, TAU); c.fill(); }
  c.restore();
  for (const n of ['pawL', 'pawR']) fillP(n, lin(c, 0, 233, 0, 252, [[0, pal.cream], [1, pal.creamDark]]));
}

function baseMap(pal, R) {
  const k = 2;
  const cv = document.createElement('canvas');
  cv.width = W * k;
  cv.height = H * k;
  const c = cv.getContext('2d', { willReadFrequently: true });
  c.scale(k, k);
  pal.bg(c, R);
  c.save();
  toCat(c);
  shadeCat(c, pal);
  c.restore();
  const data = c.getImageData(0, 0, cv.width, cv.height).data;
  const mc = document.createElement('canvas');
  mc.width = W;
  mc.height = H;
  const m = mc.getContext('2d', { willReadFrequently: true });
  toCat(m);
  fillAll(m, '#000');
  const md = m.getImageData(0, 0, W, H).data;
  return {
    cv,
    at(x, y) {
      const px = clamp((x * k) | 0, 0, cv.width - 1), py = clamp((y * k) | 0, 0, cv.height - 1);
      const i = (py * cv.width + px) * 4;
      return [data[i], data[i + 1], data[i + 2]];
    },
    inCat(x, y) {
      const px = clamp(x | 0, 0, W - 1), py = clamp(y | 0, 0, H - 1);
      return md[(py * W + px) * 4 + 3] > 128;
    },
  };
}

// Fur direction in canvas coordinates: radial on the face, around the body elsewhere.
function furDir(x, y) {
  if (Math.hypot(x - 90, y - 148) < 50) return Math.atan2(y - 165, x - 90);
  return Math.atan2(y - 165, x - 236) + Math.PI / 2;
}

function paint(c, R, B, { n, len, w, alpha = 0.9, jit = 14, accent = null, accentP = 0, dir, where }) {
  c.save();
  c.lineCap = 'round';
  for (let i = 0; i < n; i++) {
    const x = R() * W, y = R() * H;
    if (where && !where(x, y)) continue;
    let [r, g, b] = B.at(x, y);
    if (accent && R() < accentP) [r, g, b] = accent[(R() * accent.length) | 0];
    else {
      const j = (R() - 0.5) * jit;
      r += j + (R() - 0.5) * jit; g += j + (R() - 0.5) * jit; b += j + (R() - 0.5) * jit;
    }
    const a = dir(x, y) + (R() - 0.5) * 0.5;
    const l = len[0] + R() * (len[1] - len[0]);
    const ca = Math.cos(a) * l / 2, sa = Math.sin(a) * l / 2;
    c.strokeStyle = `rgba(${r | 0},${g | 0},${b | 0},${alpha})`;
    c.lineWidth = w[0] + R() * (w[1] - w[0]);
    c.beginPath();
    c.moveTo(x - ca, y - sa);
    c.quadraticCurveTo(x - sa * 0.25, y + ca * 0.25, x + ca, y + sa);
    c.stroke();
  }
  c.restore();
}

const rgb = (h) => [parseInt(h.slice(1, 3), 16), parseInt(h.slice(3, 5), 16), parseInt(h.slice(5, 7), 16)];

// ---- the styles ------------------------------------------------------------
const STYLES = [
  {
    id: 'cave', name: '史前岩画', era: '约公元前 15000 年',
    note: '赭石和炭条画在粗糙的岩壁上，线条抖动，颜料吃进石头里。',
    draw(c, R) {
      c.fillStyle = '#c4a179';
      c.fillRect(0, 0, W, H);
      for (let i = 0; i < 46; i++) {
        const d = R() < 0.5;
        soft(c, 18 + R() * 40, d ? `rgba(110,74,44,${0.12 + R() * 0.16})` : `rgba(236,214,180,${0.18 + R() * 0.2})`, blob(R() * W, R() * H, 20 + R() * 60, 12 + R() * 40, R() * 3));
      }
      c.strokeStyle = 'rgba(70,46,28,.35)';
      for (let i = 0; i < 7; i++) {
        let x = R() * W, y = R() * H;
        c.lineWidth = 0.5 + R() * 0.7;
        c.beginPath();
        c.moveTo(x, y);
        for (let k = 0; k < 9; k++) { x += (R() - 0.3) * 18; y += (R() - 0.5) * 14; c.lineTo(x, y); }
        c.stroke();
      }
      // pigment, rubbed into the rock
      const l = layer(c);
      l.save();
      toCat(l);
      fillAll(l, '#a2432a');
      l.fillStyle = '#7d2f1e';
      l.globalAlpha = 0.5;
      for (const s of STRIPES) { l.lineWidth = 6; l.strokeStyle = '#6e2818'; quad(l, s); l.stroke(); }
      l.restore();
      l.globalCompositeOperation = 'destination-out';
      for (let i = 0; i < 2600; i++) {
        l.globalAlpha = R() * 0.7;
        l.fillRect(R() * W, R() * H, 0.5 + R() * 1.8, 0.5 + R() * 1.4);
      }
      put(c, l, 0.88, 'multiply');
      // charcoal contour, a little shaky
      c.save();
      toCat(c);
      c.strokeStyle = '#2b1b13';
      c.lineCap = c.lineJoin = 'round';
      for (const n of ['tail', 'body', 'earL', 'earR', 'head', 'pawL', 'pawR']) {
        for (let pass = 0; pass < 2; pass++) {
          const pts = polyOf(PART[n], n.startsWith('paw') ? 24 : 70);
          c.globalAlpha = pass ? 0.35 : 0.85;
          c.lineWidth = pass ? 1.2 : 2.6;
          c.beginPath();
          pts.forEach(([x, y], i) => { const jx = x + (R() - 0.5) * 1.6, jy = y + (R() - 0.5) * 1.6; i ? c.lineTo(jx, jy) : c.moveTo(jx, jy); });
          c.closePath();
          c.stroke();
        }
      }
      c.globalAlpha = 0.9;
      c.lineWidth = 2.4;
      for (const d of [FACE.eyeL, FACE.eyeR]) c.stroke(path(d));
      c.lineWidth = 1.4;
      for (const d of WHISK) c.stroke(path(d));
      c.fillStyle = '#2b1b13';
      c.fill(path(FACE.nose));
      c.restore();
      // a row of ochre dots, like a tally
      c.fillStyle = 'rgba(150,58,36,.75)';
      for (let i = 0; i < 6; i++) { c.beginPath(); c.arc(300 + i * 11 + (R() - 0.5) * 2, 44 + (R() - 0.5) * 3, 2.6 + R(), 0, TAU); c.fill(); }
      grain(c, 0.32);
    },
  },
  {
    id: 'greek', name: '古希腊黑绘陶', era: '公元前 6 世纪',
    note: '陶土底上的黑色剪影，细节用针刻出陶土色的细线，耳朵点一点紫红。',
    draw(c, R) {
      const g = c.createRadialGradient(200, 130, 30, 200, 140, 260);
      g.addColorStop(0, '#d47a44');
      g.addColorStop(1, '#a9522a');
      c.fillStyle = g;
      c.fillRect(0, 0, W, H);
      const BLACK = '#1a120e';
      // bands: tongues on top, a running key below
      c.fillStyle = BLACK;
      c.fillRect(0, 16, W, 3);
      c.fillRect(0, 40, W, 2);
      for (let x = 6; x < W; x += 14) { c.beginPath(); c.ellipse(x, 28, 4.2, 9, 0, 0, TAU); c.fill(); }
      c.fillRect(0, 222, W, 2.5);
      c.fillRect(0, 254, W, 2.5);
      c.strokeStyle = BLACK;
      c.lineWidth = 2.2;
      c.lineJoin = 'miter';
      for (let x = 4; x < W; x += 24) {
        c.beginPath();
        c.moveTo(x, 250); c.lineTo(x, 228); c.lineTo(x + 18, 228); c.lineTo(x + 18, 245);
        c.lineTo(x + 7, 245); c.lineTo(x + 7, 236); c.lineTo(x + 12, 236);
        c.stroke();
        c.beginPath(); c.moveTo(x, 250); c.lineTo(x + 24, 250); c.stroke();
      }
      c.save();
      toCat(c);
      fillAll(c, BLACK);
      soft(c, 12, 'rgba(255,230,200,.07)', blob(270, 136, 70, 14, -0.15));
      c.fillStyle = '#6a2119';
      for (const n of ['earL', 'earR']) c.fill(path(INNER[n]));
      // incisions
      const INC = '#cf7a45';
      c.strokeStyle = INC;
      c.lineCap = c.lineJoin = 'round';
      c.lineWidth = 0.9;
      for (const n of ['head', 'pawL', 'pawR']) c.stroke(path(PART[n]));
      c.stroke(path('M 366 244 C 392 230 394 176 368 148'));
      c.save();
      c.clip(path(PART.tail));
      c.stroke(path(PART.body));
      c.restore();
      for (const s of STRIPES) {
        for (const o of [-2.2, 2.2]) { quad(c, s.map(([x, y]) => [x + o, y])); c.stroke(); }
      }
      for (const s of HEAD_STRIPES) { quad(c, s); c.stroke(); }
      c.lineWidth = 1.2;
      for (const d of [FACE.eyeL, FACE.eyeR, FACE.mouth]) c.stroke(path(d));
      c.lineWidth = 0.7;
      for (const d of WHISK) c.stroke(path(d));
      c.stroke(path(FACE.nose));
      for (const [x, y] of [[92, 238], [100, 238], [140, 240], [148, 240]]) { c.beginPath(); c.moveTo(x, y); c.lineTo(x, y + 6); c.stroke(); }
      // fur ticks along the back
      c.lineWidth = 0.6;
      const back = polyOf('M 168 190 C 170 160 200 136 222 128 C 276 110 338 116 368 148', 40, false);
      back.forEach(([x, y], i) => { if (i % 2) return; c.beginPath(); c.moveTo(x + 1, y + 5); c.lineTo(x + 3, y + 10); c.stroke(); });
      c.restore();
      grain(c, 0.12);
    },
  },
  {
    id: 'han', name: '汉画像石拓片', era: '公元 2 世纪',
    note: '石面凿出浮雕再用纸拓：猫是墨黑的平面，刻线是纸白，背景是凿痕和云气纹。',
    draw(c, R) {
      paper(c, R, '#efe9dc', 80);
      const X = 22, Y = 20, PW = 356, PH = 240;
      const l = layer(c);
      l.fillStyle = '#4a4541';
      l.fillRect(X, Y, PW, PH);
      // chisel striations in the background
      l.strokeStyle = 'rgba(235,228,214,.16)';
      for (let x = X + 2; x < X + PW; x += 2.2 + R() * 1.2) {
        l.lineWidth = 0.4 + R() * 0.5;
        l.beginPath(); l.moveTo(x, Y); l.lineTo(x + (R() - 0.5) * 3, Y + PH); l.stroke();
      }
      // cloud scrolls
      l.strokeStyle = 'rgba(235,228,214,.55)';
      l.lineWidth = 1.6;
      for (const [cx, cy, r, dir] of [[70, 62, 14, 1], [112, 50, 9, -1], [330, 60, 13, -1], [292, 46, 8, 1], [350, 200, 10, 1]]) {
        l.beginPath();
        for (let t = 0; t < 2.6 * TAU; t += 0.1) {
          const rr = r * (1 - t / (2.9 * TAU));
          const x = cx + Math.cos(t * dir) * rr, y = cy + Math.sin(t * dir) * rr * 0.8;
          t ? l.lineTo(x, y) : l.moveTo(x, y);
        }
        l.stroke();
        l.beginPath(); l.moveTo(cx + r * dir, cy); l.quadraticCurveTo(cx + r * 2.2 * dir, cy + r * 0.2, cx + r * 3 * dir, cy - r * 0.6); l.stroke();
      }
      // frame: double line and a band of triangles
      l.fillStyle = '#1f1c1a';
      l.fillRect(X, Y, PW, 12); l.fillRect(X, Y + PH - 12, PW, 12); l.fillRect(X, Y, 12, PH); l.fillRect(X + PW - 12, Y, 12, PH);
      l.fillStyle = 'rgba(235,228,214,.8)';
      for (let x = X + 12; x < X + PW - 12; x += 8) {
        for (const [y, s] of [[Y + 12, 1], [Y + PH - 12, -1]]) { l.beginPath(); l.moveTo(x, y); l.lineTo(x + 4, y + 5 * s); l.lineTo(x + 8, y); l.fill(); }
      }
      l.strokeStyle = 'rgba(235,228,214,.8)';
      l.lineWidth = 1;
      l.strokeRect(X + 4, Y + 4, PW - 8, PH - 8);
      // the raised cat takes the most ink
      l.save();
      toCat(l);
      fillAll(l, '#1d1a18');
      l.strokeStyle = 'rgba(240,234,222,.92)';
      l.lineCap = l.lineJoin = 'round';
      l.lineWidth = 1.5;
      for (const n of ['head', 'pawL', 'pawR', 'earL', 'earR']) l.stroke(path(n.startsWith('ear') ? INNER[n] : PART[n]));
      l.save(); l.clip(path(PART.tail)); l.stroke(path(PART.body)); l.restore();
      for (const s of [...STRIPES, ...HEAD_STRIPES]) { quad(l, s); l.stroke(); }
      l.lineWidth = 1.8;
      for (const d of [FACE.eyeL, FACE.eyeR]) l.stroke(path(d));
      l.lineWidth = 1.1;
      for (const d of [FACE.mouth, FACE.nose, ...WHISK]) l.stroke(path(d));
      l.restore();
      // uneven rubbing: specks where the paper did not take ink
      l.globalCompositeOperation = 'destination-out';
      for (let i = 0; i < 5200; i++) {
        l.globalAlpha = R() * 0.55;
        const s = 0.4 + R() * 1.3;
        l.fillRect(X + R() * PW, Y + R() * PH, s, s * (0.6 + R()));
      }
      for (let i = 0; i < 14; i++) soft(l, 26, `rgba(0,0,0,${0.08 + R() * 0.12})`, blob(X + R() * PW, Y + R() * PH, 30 + R() * 40, 20 + R() * 30));
      put(c, l);
      grain(c, 0.14);
    },
  },
  {
    id: 'ink', name: '水墨写意', era: '宋元以来',
    note: '宣纸上几笔湿墨和飞白：背是一笔，尾巴是一笔，脸只点到为止，加一枚朱印。',
    draw(c, R) {
      paper(c, R, '#f3eee2', 220, 'rgba(130,115,90,.10)');
      c.save();
      toCat(c);
      soft(c, 12, 'rgba(70,64,58,.14)', blob(250, 252, 160, 8));
      // wet washes: pale overall, pooling darker on the back and the rump
      soft(c, 6, 'rgba(60,56,52,.10)', (s) => { for (const n of ['body', 'tail']) s.fill(path(PART[n])); });
      c.save();
      c.clip(path(PART.body));
      for (const [x, y, rx, ry, a, r] of [[270, 140, 90, 30, 0.22, -0.2], [350, 190, 40, 50, 0.2, 0], [230, 170, 50, 30, 0.1, 0], [300, 230, 80, 18, 0.08, 0], [200, 210, 30, 30, 0.06, 0]]) soft(c, 14, `rgba(36,32,28,${a})`, blob(x, y, rx, ry, r));
      c.restore();
      soft(c, 4, 'rgba(60,56,52,.06)', (s) => s.fill(path(PART.head)));
      soft(c, 8, 'rgba(36,32,28,.12)', blob(122, 160, 40, 10));
      const INK = '#1c1a18';
      brush(c, R, [[156, 214], [168, 168], [212, 136], [262, 118], [318, 118], [362, 144], [384, 186], [380, 222]], 15, INK, { n: 22, dry: 0.4, alpha: 0.85 });
      brush(c, R, [[154, 240], [150, 226]], 5, INK, { n: 6, alpha: 0.5 });
      for (const s of STRIPES.slice(0, 6)) brush(c, R, s, 7, INK, { n: 9, dry: 0.5, alpha: 0.6 });
      brush(c, R, TAIL.map(([x, y]) => [x, y]), 20, '#2a2724', { n: 24, dry: 0.35, alpha: 0.78 });
      brush(c, R, [[72, 182], [76, 150], [83, 125], [98, 138], [113, 153]], 5, INK, { n: 8, dry: 0.15, alpha: 0.9 });
      brush(c, R, [[131, 153], [148, 136], [165, 125], [171, 150], [172, 182]], 5, INK, { n: 8, dry: 0.15, alpha: 0.9 });
      brush(c, R, [[68, 194], [70, 170], [92, 152], [122, 148]], 3, INK, { n: 5, alpha: 0.65 });
      brush(c, R, [[122, 148], [152, 150], [174, 170], [177, 196]], 3, INK, { n: 5, alpha: 0.65 });
      brush(c, R, [[67, 200], [76, 224], [104, 236], [128, 236]], 2.6, INK, { n: 5, alpha: 0.55, dry: 0.3 });
      for (const s of HEAD_STRIPES) brush(c, R, s, 3, INK, { n: 4, alpha: 0.5 });
      brush(c, R, [[79, 238], [96, 233], [117, 240]], 2, INK, { n: 4, alpha: 0.5 });
      brush(c, R, [[127, 241], [144, 235], [165, 242]], 2, INK, { n: 4, alpha: 0.5 });
      c.globalAlpha = 0.9;
      face(c, INK, 2, { whisk: 'rgba(30,28,26,.5)', ww: 0.5 });
      c.restore();
      // seal
      c.save();
      c.translate(356, 236);
      c.rotate(-0.02);
      c.fillStyle = '#b3302a';
      c.fillRect(0, 0, 24, 24);
      c.fillStyle = '#f3eee2';
      c.font = '700 17px "Songti SC", "Noto Serif SC", "STSong", serif';
      c.textAlign = 'center';
      c.textBaseline = 'middle';
      c.fillText('眠', 12, 13);
      c.globalCompositeOperation = 'destination-out';
      for (let i = 0; i < 90; i++) { c.globalAlpha = R(); c.fillRect(R() * 24, R() * 24, R() * 1.4, R() * 1.4); }
      c.restore();
      grain(c, 0.1);
    },
  },
  {
    id: 'papercut', name: '民间剪纸', era: '明清至今',
    note: '一张红纸剪出来：月牙纹、锯齿纹、梅花，剪下的地方透出白纸。',
    draw(c, R) {
      paper(c, R, '#f6efe2', 120);
      const l = layer(c);
      l.save();
      toCat(l);
      fillAll(l, '#c41e2d');
      l.globalCompositeOperation = 'destination-out';
      l.lineCap = l.lineJoin = 'round';
      const cut = (d, w) => { l.lineWidth = w; l.stroke(path(d)); };
      for (const n of ['earL', 'earR']) l.fill(path(INNER[n]));
      l.globalCompositeOperation = 'source-over';
      l.fillStyle = '#c41e2d';
      l.save(); l.translate(3, 6); l.scale(0.96, 0.96);
      for (const n of ['earL', 'earR']) { l.save(); const s = 0.55; l.translate(n === 'earL' ? 40 : 66, 70); l.scale(s, s); l.fill(path(INNER[n])); l.restore(); }
      l.restore();
      l.globalCompositeOperation = 'destination-out';
      cut(PART.head, 2);
      cut(PART.pawL, 1.8);
      cut(PART.pawR, 1.8);
      l.save(); l.clip(path(PART.tail)); cut(PART.body, 2); l.restore();
      cut(FACE.eyeL, 3);
      cut(FACE.eyeR, 3);
      cut(FACE.mouth, 1.4);
      for (const d of WHISK) cut(d, 1.1);
      const crescent = (x, y, r, a) => {
        l.save(); l.translate(x, y); l.rotate(a);
        l.beginPath(); l.arc(0, 0, r, -Math.PI / 2, Math.PI / 2); l.ellipse(0, 0, r * 0.4, r, 0, Math.PI / 2, -Math.PI / 2, true); l.closePath(); l.fill();
        l.restore();
      };
      // rows of moon-teeth along the stripes
      for (const s of STRIPES.slice(0, 6)) {
        const pts = polyOf(`M ${s[0].join(' ')} Q ${s[1].join(' ')} ${s[2].join(' ')}`, 5, false);
        pts.forEach(([x, y], i) => crescent(x, y, 3.6 - i * 0.3, Math.PI / 2));
      }
      for (const [x, y, a] of [[100, 170, -0.4], [122, 165, 0], [144, 170, 0.4]]) crescent(x, y, 3.6, a - Math.PI / 2);
      for (const [x, y] of [[84, 214], [88, 222], [160, 214], [156, 222]]) { l.beginPath(); l.arc(x, y, 1.8, 0, TAU); l.fill(); }
      // sawtooth inside the back
      const back = polyOf('M 172 176 C 182 150 214 134 236 128 C 282 116 334 122 358 150 C 376 170 380 200 374 226', 34, false);
      const nm = normals(back);
      back.forEach(([x, y], i) => {
        const [nx, ny] = nm[i];
        l.beginPath();
        l.moveTo(x - ny * 2.6, y + nx * 2.6);
        l.lineTo(x + ny * 2.6, y - nx * 2.6);
        l.lineTo(x - nx * 4.5, y - ny * 4.5);
        l.fill();
      });
      // a plum blossom on the hip
      for (let k = 0; k < 5; k++) { const a = (k / 5) * TAU - Math.PI / 2; l.beginPath(); l.arc(306 + Math.cos(a) * 9, 196 + Math.sin(a) * 9, 6, 0, TAU); l.fill(); }
      l.globalCompositeOperation = 'source-over';
      l.beginPath(); l.arc(306, 196, 4, 0, TAU); l.fill();
      l.globalCompositeOperation = 'destination-out';
      for (let k = 0; k < 5; k++) { const a = (k / 5) * TAU - Math.PI / 2; l.beginPath(); l.arc(306 + Math.cos(a) * 2.2, 196 + Math.sin(a) * 2.2, 0.8, 0, TAU); l.fill(); }
      // notches along the tail
      TAIL_C.forEach(([x, y], i) => { if (i % 4 || i < 6) return; crescent(x, y, 3.2, Math.PI / 2); });
      l.restore();
      c.save();
      c.shadowColor = 'rgba(90,30,20,.28)';
      c.shadowBlur = 3 * c.getTransform().a;
      c.shadowOffsetX = 1.4 * c.getTransform().a;
      c.shadowOffsetY = 2.2 * c.getTransform().a;
      put(c, l);
      c.restore();
      grain(c, 0.14);
    },
  },
  {
    id: 'ukiyoe', name: '浮世绘', era: '江户时代',
    note: '木版套色：平涂、墨线、天空的渐层，三花猫睡在鹿子纹的坐垫上，套版略微错位。',
    draw(c, R) {
      paper(c, R, '#eee0c0', 160);
      c.fillStyle = lin(c, 0, 0, 0, 130, [[0, 'rgba(38,66,104,.9)'], [1, 'rgba(38,66,104,0)']]);
      c.fillRect(0, 0, W, 130);
      // cushion with dotted tie-dye
      const cushion = new Path2D('M 44 206 Q 60 190 200 188 Q 352 190 368 206 L 378 236 Q 200 252 32 238 Z');
      c.fillStyle = '#9c2f26';
      c.fill(cushion);
      c.save();
      c.clip(cushion);
      c.fillStyle = 'rgba(248,232,205,.85)';
      for (let y = 190; y < 252; y += 7) for (let x = 30 + ((y / 7) % 2) * 3.5; x < 380; x += 7) { c.fillRect(x - 1.6, y - 1.6, 3.2, 3.2); }
      c.fillStyle = '#9c2f26';
      for (let y = 190; y < 252; y += 7) for (let x = 30 + ((y / 7) % 2) * 3.5; x < 380; x += 7) { c.fillRect(x - 0.6, y - 0.6, 1.2, 1.2); }
      c.restore();
      c.strokeStyle = '#2b2420';
      c.lineWidth = 1.3;
      c.stroke(cushion);
      // the colour blocks sit a hair off the key block
      c.save();
      toCat(c);
      const OR = '#d9782a', BK = '#2a2522';
      const PATCH = {
        body: [[[226, 128], [270, 108], [330, 112], [366, 140], [360, 172], [322, 170], [292, 184], [258, 170], [232, 160]], OR, [[318, 206], [346, 186], [382, 196], [392, 236], [350, 250], [316, 236]], BK],
        head: [[[64, 150], [108, 142], [116, 160], [104, 178], [80, 186], [62, 184]], OR, [[150, 146], [182, 150], [184, 184], [164, 180], [154, 164]], BK],
        earL: [[[60, 110], [130, 110], [130, 200], [60, 200]], OR],
        earR: [[[120, 110], [190, 110], [190, 200], [120, 200]], BK],
        tail: [[[150, 240], [236, 240], [240, 290], [150, 290]], BK, [[306, 236], [400, 230], [400, 290], [300, 290]], OR],
      };
      for (const n of ORDER) {
        const p = path(PART[n]);
        c.save();
        c.translate(0.9, 0.6);
        c.fillStyle = '#f8f2e4';
        c.fill(p);
        c.clip(p);
        const pl = PATCH[n] || [];
        for (let i = 0; i < pl.length; i += 2) { c.fillStyle = pl[i + 1]; c.fill(pl[i].length > 4 ? smooth(pl[i]) : new Path2D('M ' + pl[i].map((q) => q.join(' ')).join(' L ') + ' Z')); }
        if (n === 'body') { c.fillStyle = 'rgba(120,110,100,.12)'; c.fillRect(140, 226, 260, 34); }
        if (n === 'earL') { c.fillStyle = '#e8a59b'; c.fill(path(INNER.earL)); }
        c.restore();
        c.strokeStyle = '#2b2420';
        c.lineWidth = 1.5;
        c.lineJoin = 'round';
        c.stroke(p);
      }
      face(c, '#2b2420', 1.8, { nose: '#d9837e', whisk: 'rgba(43,36,32,.8)', ww: 0.6 });
      c.restore();
      // title cartouche
      c.fillStyle = '#e9d58e';
      c.fillRect(340, 18, 26, 64);
      c.strokeStyle = '#2b2420';
      c.lineWidth = 1;
      c.strokeRect(340, 18, 26, 64);
      c.fillStyle = '#2b2420';
      c.font = '600 15px "Hiragino Mincho ProN", "Songti SC", "Noto Serif SC", serif';
      c.textAlign = 'center';
      c.fillText('睡', 353, 42);
      c.fillText('猫', 353, 66);
      grain(c, 0.16);
    },
  },
  {
    id: 'oil', name: '古典油画', era: '17 世纪 · 明暗对照',
    note: '暗底上一束暖光，笔触顺着毛走，亮部厚涂，最后一层泛黄的光油。',
    draw(c, R) {
      const B = baseMap({
        bg(b) {
          b.fillStyle = '#1d140d';
          b.fillRect(0, 0, W, H);
          soft(b, 120, 'rgba(120,80,40,.75)', blob(150, 90, 120, 80));
          const cush = new Path2D('M 20 196 Q 200 176 384 200 L 396 280 L 4 280 Z');
          b.fillStyle = lin(b, 0, 180, 0, 280, [[0, '#7a1f1c'], [0.5, '#4a1210'], [1, '#1c0806']]);
          b.fill(cush);
          for (const [x, y] of [[90, 230], [250, 244], [330, 222]]) soft(b, 14, 'rgba(190,80,60,.45)', blob(x, y, 40, 5, -0.1));
          soft(b, 30, 'rgba(0,0,0,.5)', blob(240, 212, 170, 14));
        },
        light: '#f4c890', mid: '#b5652c', dark: '#3a1d0e', stripe: '#6a3417', shade: 'rgba(30,14,6,.75)', ground: 'rgba(0,0,0,.6)',
        inner: '#c98a76', cream: '#f2dcbc', creamDark: '#8a6a50',
      }, R);
      c.drawImage(B.cv, 0, 0, W, H);
      paint(c, R, B, { n: 2600, len: [16, 34], w: [5, 10], alpha: 0.6, jit: 10, dir: (x, y) => (y > 190 ? 0.05 : -0.6) });
      paint(c, R, B, { n: 1800, len: [10, 20], w: [3, 6], alpha: 0.85, jit: 10, dir: (x, y) => (y > 190 ? 0.05 : -0.6), where: (x, y) => !B.inCat(x, y) });
      paint(c, R, B, { n: 9000, len: [5, 12], w: [1.4, 3.2], alpha: 0.9, jit: 12, dir: furDir, where: (x, y) => B.inCat(x, y) });
      // impasto in the light
      c.save();
      c.lineCap = 'round';
      for (let i = 0; i < 1300; i++) {
        const x = R() * W, y = R() * H;
        if (!B.inCat(x, y)) continue;
        const [r, g, b] = B.at(x, y);
        if (r + g + b < 520) continue;
        const a = furDir(x, y) + (R() - 0.5) * 0.4, l = 4 + R() * 8;
        const ca = Math.cos(a) * l / 2, sa = Math.sin(a) * l / 2;
        c.strokeStyle = `rgba(${Math.min(255, r + 26)},${Math.min(255, g + 22)},${Math.min(255, b + 14)},.9)`;
        c.lineWidth = 2 + R() * 1.6;
        c.beginPath(); c.moveTo(x - ca, y - sa); c.lineTo(x + ca, y + sa); c.stroke();
        c.strokeStyle = 'rgba(255,246,226,.55)';
        c.lineWidth = 0.6;
        c.beginPath(); c.moveTo(x - ca - 0.5, y - sa - 0.6); c.lineTo(x + ca - 0.5, y + sa - 0.6); c.stroke();
      }
      c.restore();
      c.save();
      toCat(c);
      c.globalAlpha = 0.85;
      face(c, '#2a150a', 2, { nose: '#a85a4a', whisk: 'rgba(250,230,200,.55)', ww: 0.6 });
      c.restore();
      c.fillStyle = 'rgba(150,100,30,.14)';
      c.globalCompositeOperation = 'multiply';
      c.fillRect(0, 0, W, H);
      c.globalCompositeOperation = 'source-over';
      vignette(c, 0.6, '10,6,2');
      // canvas weave
      c.save();
      c.strokeStyle = 'rgba(255,240,210,.035)';
      c.lineWidth = 0.6;
      for (let x = 0; x < W; x += 1.6) { c.beginPath(); c.moveTo(x, 0); c.lineTo(x, H); c.stroke(); }
      for (let y = 0; y < H; y += 1.6) { c.beginPath(); c.moveTo(0, y); c.lineTo(W, y); c.stroke(); }
      c.restore();
      grain(c, 0.12);
    },
  },
  {
    id: 'impression', name: '印象派', era: '19 世纪末',
    note: '午后草地上的光：短促的色块直接并置，阴影是紫蓝色的，不画轮廓。',
    draw(c, R) {
      const B = baseMap({
        bg(b) {
          b.fillStyle = lin(b, 0, 0, 0, H, [[0, '#b9d3c0'], [0.35, '#86b07a'], [0.6, '#a6c46e'], [1, '#7ea65c']]);
          b.fillRect(0, 0, W, H);
          for (let i = 0; i < 26; i++) soft(b, 30, `rgba(250,236,170,${0.25 + R() * 0.35})`, blob(R() * W, R() * H, 20 + R() * 40, 10 + R() * 20));
          for (let i = 0; i < 14; i++) soft(b, 30, `rgba(70,110,110,${0.2 + R() * 0.25})`, blob(R() * W, R() * 120, 30 + R() * 40, 20 + R() * 30));
        },
        light: '#ffd79a', mid: '#f0a058', dark: '#8b76b0', stripe: '#d0703a', shade: 'rgba(120,110,190,.75)', ground: 'rgba(90,100,180,.7)',
        inner: '#f2a3a0', cream: '#fff3dc', creamDark: '#b9b2dc',
      }, R);
      const ACC = ['#f7d25a', '#e9805a', '#7f8fd0', '#9bd07a', '#f4b6c0'].map(rgb);
      c.drawImage(B.cv, 0, 0, W, H);
      paint(c, R, B, { n: 7000, len: [5, 10], w: [3, 5], alpha: 0.9, jit: 34, accent: ACC, accentP: 0.08, dir: (x, y) => (y > 170 ? -1.35 + (R() - 0.5) * 0.6 : 0.2) });
      paint(c, R, B, { n: 9000, len: [4, 8], w: [2.2, 3.6], alpha: 0.92, jit: 30, accent: ACC, accentP: 0.06, dir: furDir, where: (x, y) => B.inCat(x, y) });
      c.save();
      toCat(c);
      c.globalAlpha = 0.75;
      face(c, '#4b3b66', 2.2, { nose: '#e0807a' });
      c.restore();
      grain(c, 0.08);
    },
  },
  {
    id: 'pointil', name: '点彩', era: '1880 年代',
    note: '整张画由纯色小点组成，橙和紫、黄和蓝并排，远看在眼睛里混色。',
    draw(c, R) {
      const B = baseMap({
        bg(b) {
          b.fillStyle = lin(b, 0, 0, 0, H, [[0, '#d9e6ee'], [0.42, '#a7c2dc'], [0.5, '#8fbf72'], [1, '#6f9f52']]);
          b.fillRect(0, 0, W, H);
          for (let i = 0; i < 10; i++) soft(b, 30, 'rgba(255,240,190,.4)', blob(R() * W, 40 + R() * 60, 40, 14));
        },
        light: '#ffd890', mid: '#f29a52', dark: '#7e6cb4', stripe: '#d76a36', shade: 'rgba(110,100,190,.8)', ground: 'rgba(80,90,190,.8)',
        inner: '#f39aa0', cream: '#fff6e4', creamDark: '#bdb6e4',
      }, R);
      c.fillStyle = '#f1ece0';
      c.fillRect(0, 0, W, H);
      const CAT_ACC = ['#ffd84a', '#ff7a3c', '#e8506a', '#6a78d8'].map(rgb);
      const BG_ACC = ['#ffe36a', '#4fa3e0', '#7cc860', '#c78ad8'].map(rgb);
      for (let pass = 0; pass < 3; pass++) {
        const step = 2.6;
        for (let y = 0; y < H; y += step) {
          for (let x = 0; x < W; x += step) {
            const px = x + (R() - 0.5) * step, py = y + (R() - 0.5) * step;
            let [r, g, b] = B.at(px, py);
            const cat = B.inCat(px, py);
            if (R() < 0.18) [r, g, b] = (cat ? CAT_ACC : BG_ACC)[(R() * 4) | 0];
            else { r += (R() - 0.5) * 50; g += (R() - 0.5) * 50; b += (R() - 0.5) * 50; }
            c.fillStyle = `rgb(${r | 0},${g | 0},${b | 0})`;
            c.beginPath();
            c.arc(px, py, cat ? 1.1 + R() * 0.4 : 1.2 + R() * 0.5, 0, TAU);
            c.fill();
          }
        }
      }
      c.save();
      toCat(c);
      for (const d of [FACE.eyeL, FACE.eyeR, FACE.mouth]) {
        for (const [x, y] of polyOf(d, 9, false)) { c.fillStyle = R() < 0.5 ? '#3a3480' : '#5a2a60'; c.beginPath(); c.arc(x, y, 1.15, 0, TAU); c.fill(); }
      }
      for (const [x, y] of polyOf(FACE.nose, 6)) { c.fillStyle = '#e0607a'; c.beginPath(); c.arc(x, y, 1.2, 0, TAU); c.fill(); }
      c.restore();
    },
  },
  {
    id: 'cubism', name: '立体主义', era: '1910 年代',
    note: '猫被切成棱角分明的块面，同时从正面和侧面看它，土色调、颤抖的轮廓。',
    draw(c, R) {
      const PAL = ['#c8b48a', '#a8916a', '#7d7360', '#5b584b', '#d9cca6', '#7f8e84', '#a7623d'];
      c.fillStyle = '#cdbf9a';
      c.fillRect(0, 0, W, H);
      for (let i = 0; i < 18; i++) {
        c.fillStyle = PAL[(R() * PAL.length) | 0];
        c.globalAlpha = 0.45 + R() * 0.3;
        const x = R() * W, y = R() * H;
        c.beginPath();
        c.moveTo(x, y);
        for (let k = 0; k < 3 + ((R() * 2) | 0); k++) c.lineTo(x + (R() - 0.5) * 220, y + (R() - 0.5) * 160);
        c.closePath();
        c.fill();
      }
      c.globalAlpha = 1;
      c.save();
      toCat(c);
      const poly = { tail: 12, body: 11, earL: 3, earR: 3, head: 9, pawL: 5, pawR: 5 };
      const ang = {};
      for (const n of ORDER) ang[n] = new Path2D('M ' + polyOf(PART[n], poly[n]).map((p) => p.map((v) => v.toFixed(1)).join(' ')).join(' L ') + ' Z');
      const fillA = { tail: '#7c6a4e', body: '#b8773f', earL: '#d6b47a', earR: '#6f6450', head: '#d39a5c', pawL: '#e6dcbc', pawR: '#bfb08a' };
      for (const n of ORDER) { c.fillStyle = fillA[n]; c.fill(ang[n]); }
      // facets: half-planes cut through the cat, lighter or darker
      const clipCat = () => { const all = new Path2D(); for (const n of ORDER) all.addPath(ang[n]); c.clip(all); };
      for (let i = 0; i < 22; i++) {
        const x = 60 + R() * 330, y = 120 + R() * 140, a = R() * TAU;
        const dx = Math.cos(a), dy = Math.sin(a);
        c.save();
        clipCat();
        c.beginPath();
        c.moveTo(x - dx * 400, y - dy * 400);
        c.lineTo(x + dx * 400, y + dy * 400);
        c.lineTo(x + dx * 400 - dy * 60, y + dy * 400 + dx * 60);
        c.lineTo(x - dx * 400 - dy * 60, y - dy * 400 + dx * 60);
        c.closePath();
        c.fillStyle = [`rgba(250,236,200,${0.22 + R() * 0.2})`, `rgba(40,34,26,${0.18 + R() * 0.18})`, `rgba(120,140,128,${0.25 + R() * 0.2})`, `rgba(170,90,50,${0.2 + R() * 0.2})`][(R() * 4) | 0];
        c.fill();
        c.strokeStyle = 'rgba(40,30,22,.35)';
        c.lineWidth = 0.8;
        c.beginPath(); c.moveTo(x - dx * 400, y - dy * 400); c.lineTo(x + dx * 400, y + dy * 400); c.stroke();
        c.restore();
      }
      c.strokeStyle = '#2c261f';
      c.lineJoin = 'miter';
      for (const n of ORDER) { c.lineWidth = 2.2; c.stroke(ang[n]); }
      c.save(); c.translate(7, -3); c.lineWidth = 1; c.globalAlpha = 0.5; c.stroke(ang.head); c.restore();
      // one eye from the front, one in profile, a nose seen from the side
      c.lineWidth = 2.2;
      c.stroke(path(FACE.eyeL));
      c.beginPath(); c.moveTo(134, 194); c.lineTo(144, 189); c.lineTo(156, 194); c.lineTo(144, 197); c.closePath(); c.stroke();
      c.beginPath(); c.moveTo(144, 189); c.lineTo(144, 197); c.stroke();
      c.fillStyle = '#5b3a2a';
      c.beginPath(); c.moveTo(120, 198); c.lineTo(132, 210); c.lineTo(120, 211); c.closePath(); c.fill();
      c.lineWidth = 1.2;
      c.stroke(path(FACE.mouth));
      for (const d of WHISK) c.stroke(path(d.replace(/Q [\d.]+ [\d.]+ /, 'L ')));
      c.restore();
      grain(c, 0.16);
    },
  },
  {
    id: 'bauhaus', name: '包豪斯几何', era: '1920 年代',
    note: '只用圆、半圆、三角和直线，红黄蓝黑四色，把猫拼出来。',
    draw(c, R) {
      c.fillStyle = '#efe8d8';
      c.fillRect(0, 0, W, H);
      const RED = '#d33a2c', YEL = '#f2b134', BLUE = '#2353a5', BLK = '#1d1b1a';
      c.fillStyle = BLUE;
      c.beginPath(); c.arc(330, 66, 22, 0, TAU); c.fill();
      c.fillStyle = BLK;
      c.fillRect(30, 214, 340, 2.2);
      c.fillRect(352, 40, 2.2, 52);
      // body: a half disc
      c.fillStyle = RED;
      c.beginPath(); c.arc(262, 214, 94, Math.PI, 0); c.closePath(); c.fill();
      c.strokeStyle = '#efe8d8';
      c.lineWidth = 3;
      for (const a of [-2.35, -1.95, -1.55, -1.15]) { c.beginPath(); c.moveTo(262 + Math.cos(a) * 52, 214 + Math.sin(a) * 52); c.lineTo(262 + Math.cos(a) * 94, 214 + Math.sin(a) * 94); c.stroke(); }
      // tail: a blue bar that turns up into the body
      c.fillStyle = BLUE;
      c.beginPath();
      c.moveTo(356, 214);
      c.arc(340, 216, 16, 0, Math.PI / 2);
      c.lineTo(178, 232);
      c.arc(178, 223, 9, Math.PI / 2, -Math.PI / 2);
      c.lineTo(340, 214);
      c.closePath();
      c.fill();
      // head: a yellow disc with two black triangles
      c.fillStyle = BLK;
      c.beginPath(); c.moveTo(108, 156); c.lineTo(116, 112); c.lineTo(140, 140); c.closePath(); c.fill();
      c.beginPath(); c.moveTo(160, 140); c.lineTo(184, 112); c.lineTo(192, 156); c.closePath(); c.fill();
      c.fillStyle = YEL;
      c.beginPath(); c.arc(150, 170, 46, 0, TAU); c.fill();
      c.strokeStyle = BLK;
      c.lineWidth = 3.4;
      c.lineCap = 'round';
      for (const x of [132, 168]) { c.beginPath(); c.arc(x, 168, 8, 0.15 * Math.PI, 0.85 * Math.PI); c.stroke(); }
      c.fillStyle = RED;
      c.beginPath(); c.moveTo(144, 182); c.lineTo(156, 182); c.lineTo(150, 190); c.closePath(); c.fill();
      c.strokeStyle = BLK;
      c.lineWidth = 1.4;
      for (const [x0, x1, y] of [[118, 82, 186], [118, 84, 194], [182, 218, 186], [182, 216, 194]]) { c.beginPath(); c.moveTo(x0, y); c.lineTo(x1, y + (y - 190) * 0.8); c.stroke(); }
      // paws: two black half discs
      c.fillStyle = BLK;
      for (const x of [128, 172]) { c.beginPath(); c.arc(x, 216, 12, Math.PI, 0); c.closePath(); c.fill(); }
      // sleep: three squares stepping up
      c.fillStyle = BLK;
      for (let i = 0; i < 3; i++) c.fillRect(206 + i * 14, 108 - i * 14, 6 + i * 2, 6 + i * 2);
      grain(c, 0.1);
    },
  },
  {
    id: 'pop', name: '波普', era: '1960 年代',
    note: '粗黑线、平涂高饱和、网点印刷的阴影，再配一个漫画对白框。',
    draw(c, R) {
      c.fillStyle = '#3fc1d6';
      c.fillRect(0, 0, W, H);
      c.fillStyle = '#2aa0bd';
      for (let y = 0; y < H + 6; y += 7) for (let x = ((y / 7) % 2) * 3.5; x < W + 6; x += 7) { c.beginPath(); c.arc(x, y, 1.9, 0, TAU); c.fill(); }
      const B = baseMap({
        bg(b) { b.fillStyle = '#fff'; b.fillRect(0, 0, W, H); },
        light: '#ffffff', mid: '#cfcfcf', dark: '#5a5a5a', stripe: '#b0b0b0', shade: 'rgba(0,0,0,.5)', ground: 'rgba(0,0,0,0)',
        inner: '#ffffff', cream: '#ffffff', creamDark: '#bdbdbd',
      }, R);
      c.save();
      toCat(c);
      c.fillStyle = 'rgba(0,0,0,.9)';
      c.beginPath(); c.ellipse(250, 262, 166, 10, 0, 0, TAU); c.fill();
      // Ben-Day dots for the shading, in canvas coordinates
      const dots = () => {
        c.save();
        c.setTransform(c.getTransform().translate(-CAT[0], -CAT[1]));
        c.fillStyle = '#e8402a';
        for (let y = 0; y < H; y += 3.6) {
          for (let x = ((y / 3.6) % 2) * 1.8; x < W; x += 3.6) {
            const r = (1 - B.at(x, y)[0] / 255) * 2.1;
            if (r < 0.35) continue;
            c.beginPath(); c.arc(x, y, r, 0, TAU); c.fill();
          }
        }
        c.restore();
      };
      const HI = { body: [262, 132, 26, 6, -0.25], head: [112, 160, 12, 4, -0.4], tail: [326, 262, 18, 3, -0.05] };
      for (const n of ORDER) {
        const p = path(PART[n]);
        c.fillStyle = n.startsWith('paw') ? '#ffffff' : '#ffb23e';
        c.fill(p);
        c.save();
        c.clip(p);
        if (!n.startsWith('paw')) dots();
        if (n === 'body') {
          c.strokeStyle = '#e8402a';
          c.lineWidth = 7;
          c.lineCap = 'round';
          for (const s of STRIPES.slice(0, 6)) { quad(c, s); c.stroke(); }
        }
        if (INNER[n]) { c.fillStyle = '#ff7aa8'; c.fill(path(INNER[n])); }
        if (HI[n]) { const [x, y, rx, ry, r] = HI[n]; c.fillStyle = '#fff'; c.beginPath(); c.ellipse(x, y, rx, ry, r, 0, TAU); c.fill(); }
        c.restore();
        c.strokeStyle = '#111';
        c.lineWidth = 3.4;
        c.lineJoin = 'round';
        c.stroke(p);
      }
      face(c, '#111', 3, { nose: '#ff5a8a', whisk: '#111', ww: 1.4 });
      c.restore();
      // speech balloon
      c.fillStyle = '#fff';
      c.strokeStyle = '#111';
      c.lineWidth = 3;
      c.beginPath();
      c.ellipse(108, 46, 70, 28, -0.04, 0, TAU);
      c.moveTo(110, 72); c.lineTo(104, 92); c.lineTo(128, 70);
      c.fill();
      c.stroke();
      c.fillStyle = '#fff';
      c.beginPath(); c.ellipse(108, 46, 67, 25, -0.04, 0, TAU); c.fill();
      c.fillStyle = '#111';
      c.font = '900 26px "Arial Black", "Helvetica Neue", Impact, sans-serif';
      c.textAlign = 'center';
      c.textBaseline = 'middle';
      c.fillText('ZZZ…', 108, 48);
    },
  },
  {
    id: 'line', name: '极简线描', era: '当代',
    note: '一根连续的细线勾出整只猫，背后一团淡淡的水彩。',
    draw(c, R) {
      c.fillStyle = '#fbf9f4';
      c.fillRect(0, 0, W, H);
      for (let i = 0; i < 9; i++) soft(c, 26, `rgba(246,${170 + R() * 30},${120 + R() * 30},${0.16 + R() * 0.12})`, blob(220 + (R() - 0.5) * 140, 150 + (R() - 0.5) * 60, 40 + R() * 50, 30 + R() * 30));
      c.save();
      toCat(c);
      c.strokeStyle = '#1f1d1b';
      c.lineWidth = 1.7;
      c.lineCap = c.lineJoin = 'round';
      c.stroke(path(
        'M 170 254 C 230 270 336 272 376 254 C 400 240 398 196 382 168 C 360 128 300 108 248 116 '
        + 'C 210 122 186 136 172 152 C 171 140 169 130 166 124 C 154 130 142 140 134 150 '
        + 'C 122 146 110 146 104 151 C 96 140 90 132 82 124 C 74 140 70 162 72 180 '
        + 'C 62 204 72 230 104 235 C 90 235 78 240 78 245 C 78 252 112 252 120 245 '
        + 'C 124 252 162 254 166 246 C 168 240 160 236 148 235 '
        + 'C 166 230 178 216 178 196 C 178 178 170 166 160 158',
      ));
      c.lineWidth = 1.5;
      for (const d of [FACE.eyeL, FACE.eyeR]) c.stroke(path(d));
      c.lineWidth = 1.1;
      c.stroke(path(FACE.mouth));
      c.fillStyle = '#e07a5a';
      c.fill(path(FACE.nose));
      c.lineWidth = 0.6;
      c.globalAlpha = 0.7;
      for (const d of WHISK) c.stroke(path(d));
      c.restore();
    },
  },
  {
    id: 'pixel', name: '像素', era: '1980 年代 · 8 位机',
    note: '100×70 的格子，限定色板，硬边描线，抖动的地面。',
    draw(c) {
      const GW = 100, GH = 70, k = GW / W;
      const lc = document.createElement('canvas');
      lc.width = GW;
      lc.height = GH;
      const l = lc.getContext('2d', { willReadFrequently: true });
      const PAL = {
        bg: '#f4ecd8', bg2: '#e9dcc0', floor: '#d6c5a2', line: '#3b2626', l1: '#f6b56a', l2: '#e2873a', l3: '#b05f2a',
        st: '#8f4520', cr: '#fbeacc', cr2: '#dcc09a', pink: '#ec9a98', sh: '#c9b590',
      };
      l.scale(k, k);
      l.fillStyle = PAL.bg;
      l.fillRect(0, 0, W, H);
      l.save();
      toCat(l);
      l.fillStyle = PAL.sh;
      l.beginPath(); l.ellipse(244, 256, 170, 12, 0, 0, TAU); l.fill();
      const fill = (n, col) => { l.fillStyle = col; l.fill(path(PART[n])); };
      fill('tail', PAL.l2);
      fill('body', PAL.l2);
      l.save(); l.clip(path(PART.body)); l.fillStyle = PAL.l1; l.beginPath(); l.ellipse(250, 120, 110, 44, -0.2, 0, TAU); l.fill(); l.fillStyle = PAL.l3; l.beginPath(); l.ellipse(330, 260, 130, 50, 0, 0, TAU); l.fill(); l.fillRect(150, 236, 250, 20); l.restore();
      fill('earL', PAL.l2);
      fill('earR', PAL.l2);
      l.fillStyle = PAL.pink;
      for (const n of ['earL', 'earR']) l.fill(path(INNER[n]));
      fill('head', PAL.l2);
      l.save(); l.clip(path(PART.head)); l.fillStyle = PAL.l1; l.beginPath(); l.ellipse(108, 160, 46, 22, -0.2, 0, TAU); l.fill(); l.fillStyle = PAL.cr; l.beginPath(); l.ellipse(122, 222, 26, 14, 0, 0, TAU); l.fill(); l.restore();
      l.lineWidth = 6;
      l.strokeStyle = PAL.st;
      l.save(); l.clip(path(PART.body)); for (const s of STRIPES) { quad(l, s); l.stroke(); } l.restore();
      l.save(); l.clip(path(PART.tail)); for (const x of [222, 262, 302, 342]) { l.beginPath(); l.moveTo(x, 246); l.lineTo(x - 6, 284); l.stroke(); } l.restore();
      l.lineWidth = 4;
      l.save(); l.clip(path(PART.head)); for (const s of HEAD_STRIPES) { quad(l, s); l.stroke(); } l.restore();
      fill('pawL', PAL.cr);
      fill('pawR', PAL.cr);
      l.restore();
      // snap every pixel to the palette, then outline the cat
      const img = l.getImageData(0, 0, GW, GH), d = img.data;
      const cols = Object.values(PAL).map(rgb);
      for (let i = 0; i < d.length; i += 4) {
        let best = 0, bd = Infinity;
        cols.forEach((q, j) => { const e = (d[i] - q[0]) ** 2 + (d[i + 1] - q[1]) ** 2 + (d[i + 2] - q[2]) ** 2; if (e < bd) { bd = e; best = j; } });
        [d[i], d[i + 1], d[i + 2]] = cols[best];
        d[i + 3] = 255;
      }
      const bgSet = new Set([PAL.bg, PAL.sh].map((h) => rgb(h).join()));
      const isBg = (x, y) => x < 0 || y < 0 || x >= GW || y >= GH || bgSet.has([d[(y * GW + x) * 4], d[(y * GW + x) * 4 + 1], d[(y * GW + x) * 4 + 2]].join());
      const out = [];
      for (let y = 0; y < GH; y++) for (let x = 0; x < GW; x++) if (!isBg(x, y) && (isBg(x - 1, y) || isBg(x + 1, y) || isBg(x, y - 1) || isBg(x, y + 1))) out.push([x, y]);
      l.setTransform(1, 0, 0, 1, 0, 0);
      l.putImageData(img, 0, 0);
      l.fillStyle = PAL.line;
      for (const [x, y] of out) l.fillRect(x, y, 1, 1);
      // seams between head, paws and body, then the face, pixel by pixel
      const px = (x, y, col = PAL.line) => { l.fillStyle = col; l.fillRect(x, y, 1, 1); };
      const g = (wx, wy) => [Math.round((wx + CAT[0]) * k), Math.round((wy + CAT[1]) * k)];
      for (const [x, y] of polyOf(PART.head, 90).map(([x, y]) => g(x, y))) if (!isBg(x, y)) px(x, y);
      for (const n of ['pawL', 'pawR']) for (const [x, y] of polyOf(PART[n], 40).map(([x, y]) => g(x, y))) if (!isBg(x, y)) px(x, y);
      for (const [ex, ey] of [g(100, 196), g(144, 196)]) { px(ex - 2, ey - 1); px(ex - 1, ey); px(ex, ey); px(ex + 1, ey); px(ex + 2, ey - 1); }
      const [nx, ny] = g(122, 206);
      px(nx, ny, PAL.pink); px(nx - 1, ny - 1, PAL.pink); px(nx + 1, ny - 1, PAL.pink); px(nx, ny + 1); px(nx - 1, ny + 2); px(nx + 1, ny + 2);
      // dithered floor and a few Z's
      for (let y = 57; y < GH; y++) for (let x = 0; x < GW; x++) if ((x + y) % 2 === 0 && isBg(x, y) && y > 58 + (x % 3 === 0)) px(x, y, PAL.bg2);
      const Z = (x, y, s) => { for (let i = 0; i < s; i++) { px(x + i, y); px(x + i, y + s - 1); px(x + s - 1 - i, y + i); } };
      Z(17, 16, 3); Z(22, 10, 4); Z(28, 3, 5);
      c.save();
      c.imageSmoothingEnabled = false;
      c.drawImage(lc, 0, 0, W, H);
      c.restore();
    },
  },
  {
    id: 'mech', name: '机械', era: '未来',
    note: '金属板拼成的机器猫，在充电垫上待机：护目镜里亮着闭眼的灯，尾巴是一节节的关节。',
    draw(c, R) {
      c.fillStyle = lin(c, 0, 0, 0, H, [[0, '#1b222d'], [1, '#2b3442']]);
      c.fillRect(0, 0, W, H);
      c.strokeStyle = 'rgba(120,170,220,.07)';
      c.lineWidth = 0.6;
      for (let x = 0; x < W; x += 12) { c.beginPath(); c.moveTo(x, 0); c.lineTo(x, H); c.stroke(); }
      for (let y = 0; y < H; y += 12) { c.beginPath(); c.moveTo(0, y); c.lineTo(W, y); c.stroke(); }
      // charging pad
      c.fillStyle = lin(c, 0, 200, 0, 232, [[0, '#3a4452'], [1, '#141920']]);
      c.beginPath(); c.ellipse(212, 214, 186, 20, 0, 0, TAU); c.fill();
      soft(c, 8, 'rgba(70,220,255,.7)', (s) => { s.lineWidth = 1.6; s.beginPath(); s.ellipse(212, 211, 180, 15, 0, 0, TAU); s.stroke(); });
      c.strokeStyle = 'rgba(120,230,255,.8)';
      c.lineWidth = 0.8;
      c.beginPath(); c.ellipse(212, 211, 180, 15, 0, 0, TAU); c.stroke();
      c.save();
      toCat(c);
      const metal = (x0, y0, x1, y1) => lin(c, x0, y0, x1, y1, [[0, '#eef2f6'], [0.45, '#a9b3bf'], [1, '#4c5664']]);
      const plate = (n, g) => {
        const p = path(PART[n]);
        c.fillStyle = g; c.fill(p);
        c.save(); c.clip(p);
        c.strokeStyle = 'rgba(10,14,20,.55)'; c.lineWidth = 4; c.stroke(p);
        c.strokeStyle = lin(c, 60, 110, 400, 270, [[0, 'rgba(140,230,255,.8)'], [0.5, 'rgba(140,230,255,0)']]); c.lineWidth = 2.4; c.stroke(p);
        c.restore();
        c.strokeStyle = '#0d1117'; c.lineWidth = 1.1; c.stroke(p);
      };
      // tail: jointed segments, root to tip
      const segs = spline(TAIL, 3);
      segs.forEach(([x, y], i) => {
        const r = tailR(i / (segs.length - 1));
        const g = c.createRadialGradient(x - r * 0.4, y - r * 0.5, 1, x, y, r);
        g.addColorStop(0, '#f2f5f8'); g.addColorStop(0.6, '#8d97a4'); g.addColorStop(1, '#2d3540');
        c.fillStyle = g;
        c.beginPath(); c.arc(x, y, r, 0, TAU); c.fill();
        c.strokeStyle = '#0d1117'; c.lineWidth = 0.9; c.stroke();
      });
      const tip = TAIL[TAIL.length - 1];
      soft(c, 6, 'rgba(70,220,255,.9)', blob(tip[0] - 4, tip[1], 4, 4));
      plate('body', metal(236, 112, 320, 252));
      c.save();
      c.clip(path(PART.body));
      for (const s of STRIPES) {
        quad(c, s); c.strokeStyle = 'rgba(10,14,20,.6)'; c.lineWidth = 1.6; c.stroke();
        quad(c, s.map(([x, y]) => [x - 1.2, y])); c.strokeStyle = 'rgba(255,255,255,.4)'; c.lineWidth = 0.7; c.stroke();
      }
      for (const s of STRIPES) for (const [x, y] of [s[0], s[2]]) {
        const g = c.createRadialGradient(x - 0.6, y - 0.6, 0.2, x, y, 2.2);
        g.addColorStop(0, '#fff'); g.addColorStop(1, '#4c5664');
        c.fillStyle = g; c.beginPath(); c.arc(x + 6, y + 4, 1.8, 0, TAU); c.fill();
      }
      c.restore();
      // status light on the hip
      soft(c, 10, 'rgba(70,220,255,.9)', blob(330, 196, 7, 7));
      c.fillStyle = '#c9f6ff'; c.beginPath(); c.arc(330, 196, 3.4, 0, TAU); c.fill();
      for (const n of ['earL', 'earR']) {
        plate(n, metal(70, 124, 170, 182));
        c.fillStyle = '#1a212b'; c.fill(path(INNER[n]));
      }
      c.strokeStyle = 'rgba(160,190,215,.6)';
      c.lineWidth = 1.2;
      for (const [x0, y0, x1, y1] of [[84, 150, 98, 148], [84, 158, 102, 156], [148, 148, 160, 150], [146, 156, 162, 158]]) { c.beginPath(); c.moveTo(x0, y0); c.lineTo(x1, y1); c.stroke(); }
      plate('head', metal(90, 150, 150, 238));
      // visor with closed-eye lights
      const visor = new Path2D('M 78 186 Q 122 178 166 186 L 164 204 Q 122 210 80 204 Z');
      c.fillStyle = lin(c, 0, 182, 0, 206, [[0, '#0b1018'], [1, '#1f2a38']]);
      c.fill(visor);
      c.strokeStyle = 'rgba(255,255,255,.25)'; c.lineWidth = 0.8; c.stroke(visor);
      for (const d of [FACE.eyeL, FACE.eyeR]) {
        soft(c, 6, 'rgba(70,220,255,1)', (s) => { s.lineWidth = 2.6; s.lineCap = 'round'; s.stroke(path(d)); });
        c.strokeStyle = '#d6fbff'; c.lineWidth = 1.4; c.lineCap = 'round'; c.stroke(path(d));
      }
      c.translate(0, 0);
      soft(c, 4, 'rgba(255,170,60,.9)', blob(122, 214, 3, 2.4));
      c.fillStyle = '#ffd28a'; c.beginPath(); c.ellipse(122, 214, 1.8, 1.4, 0, 0, TAU); c.fill();
      c.strokeStyle = 'rgba(10,14,20,.6)'; c.lineWidth = 1.1;
      c.beginPath(); c.moveTo(110, 222); c.lineTo(134, 222); c.stroke();
      // antenna whiskers
      c.strokeStyle = '#9aa6b4'; c.lineWidth = 0.8;
      for (const d of WHISK) c.stroke(path(d));
      c.fillStyle = '#c9f6ff';
      for (const [x, y] of [[64, 206], [66, 220], [180, 206], [178, 220]]) { c.beginPath(); c.arc(x, y, 1.4, 0, TAU); c.fill(); }
      for (const n of ['pawL', 'pawR']) {
        plate(n, metal(0, 232, 0, 252));
        const [cx] = n === 'pawL' ? [98] : [146];
        c.strokeStyle = 'rgba(10,14,20,.6)'; c.lineWidth = 1;
        for (const o of [-6, 0, 6]) { c.beginPath(); c.moveTo(cx + o, 245); c.lineTo(cx + o, 250); c.stroke(); }
      }
      c.restore();
      // charge readout
      c.fillStyle = 'rgba(120,230,255,.85)';
      c.font = '600 9px ui-monospace, "SF Mono", Menlo, monospace';
      c.fillText('CHARGING 82%', 304, 252);
      for (let i = 0; i < 10; i++) { c.fillStyle = i < 8 ? 'rgba(120,230,255,.85)' : 'rgba(120,230,255,.2)'; c.fillRect(304 + i * 7, 256, 5, 3); }
      grain(c, 0.08);
    },
  },
];

// ---- page ------------------------------------------------------------------
function render(canvas, style) {
  const w = canvas.clientWidth;
  if (!w) return;
  const dpr = Math.min(2, devicePixelRatio || 1);
  canvas.width = Math.round(w * dpr);
  canvas.height = Math.round((w * H / W) * dpr);
  const c = canvas.getContext('2d');
  const S = canvas.width / W;
  c.setTransform(S, 0, 0, S, 0, 0);
  style.draw(c, rng(11 + style.id.length * 97));
  canvas.dataset.w = String(w);
}

const grid = document.getElementById('grid');
const io = new IntersectionObserver((entries) => {
  for (const e of entries) {
    if (!e.isIntersecting) continue;
    const cv = e.target;
    if (cv.dataset.w !== String(cv.clientWidth)) render(cv, STYLES[cv.dataset.i]);
  }
}, { rootMargin: '200px' });

STYLES.forEach((s, i) => {
  const card = document.createElement('button');
  card.type = 'button';
  card.className = 'card';
  card.innerHTML = `<canvas data-i="${i}" role="img" aria-label="${s.name}风格的睡猫"></canvas>
    <span class="meta"><span class="num">${String(i + 1).padStart(2, '0')}</span><span class="name">${s.name}</span><span class="era">${s.era}</span></span>
    <span class="note">${s.note}</span>`;
  grid.appendChild(card);
  io.observe(card.querySelector('canvas'));
  card.addEventListener('click', () => open(i));
});

let resizeT = 0;
addEventListener('resize', () => {
  clearTimeout(resizeT);
  resizeT = setTimeout(() => {
    for (const cv of grid.querySelectorAll('canvas')) {
      const r = cv.getBoundingClientRect();
      if (r.bottom > -200 && r.top < innerHeight + 200 && cv.dataset.w !== String(cv.clientWidth)) render(cv, STYLES[cv.dataset.i]);
    }
  }, 150);
});

const dlg = document.getElementById('big');
const bigCv = dlg.querySelector('canvas');
const bigTitle = dlg.querySelector('h2');
const bigNote = dlg.querySelector('p');
let cur = 0;
function open(i) {
  cur = (i + STYLES.length) % STYLES.length;
  const s = STYLES[cur];
  bigTitle.textContent = `${String(cur + 1).padStart(2, '0')} · ${s.name} · ${s.era}`;
  bigNote.textContent = s.note;
  if (!dlg.open) dlg.showModal();
  bigCv.dataset.w = '';
  requestAnimationFrame(() => render(bigCv, s));
}
dlg.querySelector('[data-close]').addEventListener('click', () => dlg.close());
dlg.querySelector('[data-prev]').addEventListener('click', () => open(cur - 1));
dlg.querySelector('[data-next]').addEventListener('click', () => open(cur + 1));
dlg.addEventListener('click', (e) => { if (e.target === dlg) dlg.close(); });
dlg.addEventListener('keydown', (e) => {
  if (e.key === 'ArrowLeft') open(cur - 1);
  if (e.key === 'ArrowRight') open(cur + 1);
});

window.zenStyles = { STYLES, open };
