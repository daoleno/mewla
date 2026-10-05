// Brush and sword-light strokes for canvas: a path sampled once, then filled
// as a tapered ribbon between two fractions of its length, so any stroke can be
// drawn growing, travelling or at rest from the same data.
import { rng } from "./motion.js";

function measure(pts) {
  let L = 0;
  pts[0][2] = 0;
  for (let i = 1; i < pts.length; i++) {
    L += Math.hypot(pts[i][0] - pts[i - 1][0], pts[i][1] - pts[i - 1][1]);
    pts[i][2] = L;
  }
  pts.L = L;
  return pts;
}

export function cubic(p0, p1, p2, p3, n = 72) {
  const pts = [];
  for (let i = 0; i <= n; i++) {
    const t = i / n, m = 1 - t;
    const a = m * m * m, b = 3 * m * m * t, c = 3 * m * t * t, d = t * t * t;
    pts.push([a * p0[0] + b * p1[0] + c * p2[0] + d * p3[0], a * p0[1] + b * p1[1] + c * p2[1] + d * p3[1]]);
  }
  return measure(pts);
}

export function polyline(points) {
  return measure(points.map((p) => [p[0], p[1]]));
}

export function join(...parts) {
  const pts = [];
  for (const p of parts) for (const q of p) pts.push([q[0], q[1]]);
  return measure(pts);
}

// Point and unit tangent at fraction f of the path.
export function at(pts, f) {
  const s = Math.max(0, Math.min(1, f)) * pts.L;
  let i = 1;
  while (i < pts.length - 1 && pts[i][2] < s) i++;
  const p = pts[i - 1], q = pts[i], seg = q[2] - p[2] || 1, k = (s - p[2]) / seg;
  const dx = (q[0] - p[0]) / seg, dy = (q[1] - p[1]) / seg;
  return [p[0] + (q[0] - p[0]) * k, p[1] + (q[1] - p[1]) * k, dx, dy];
}

// The slice of the path between fractions a and b, ends interpolated.
function slice(pts, a, b) {
  const s0 = a * pts.L, s1 = b * pts.L, out = [];
  const A = at(pts, a);
  out.push([A[0], A[1], s0]);
  for (const p of pts) if (p[2] > s0 && p[2] < s1) out.push(p);
  const B = at(pts, b);
  out.push([B[0], B[1], s1]);
  return out;
}

// Fill a tapered ribbon. `press` and `lift` are the fractions of the visible
// slice over which the brush presses down and lifts off; `dry` cuts feibai
// streaks (paper showing through) into the ribbon along its direction.
export function stroke(ctx, pts, a, b, o) {
  if (b - a <= 1e-4) return;
  const seg = slice(pts, a, b);
  const n = seg.length;
  if (n < 2) return;
  const s0 = seg[0][2], len = seg[n - 1][2] - s0 || 1;
  const w = o.w, press = o.press ?? 0.12, lift = o.lift ?? 0.3, min = o.min ?? 0.25;
  const L = [], R = [];
  for (let i = 0; i < n; i++) {
    const p = seg[i], q = seg[Math.min(n - 1, i + 1)], r = seg[Math.max(0, i - 1)];
    let dx = q[0] - r[0], dy = q[1] - r[1];
    const d = Math.hypot(dx, dy) || 1;
    dx /= d; dy /= d;
    const u = (p[2] - s0) / len;
    const k = Math.min(1, u / press) ** 0.55 * Math.min(1, (1 - u) / lift) ** 0.9;
    const hw = Math.max(min, w * (o.profile ? o.profile(u) : 1) * k) / 2;
    L.push([p[0] - dy * hw, p[1] + dx * hw]);
    R.push([p[0] + dy * hw, p[1] - dx * hw]);
  }
  ctx.beginPath();
  ctx.moveTo(L[0][0], L[0][1]);
  for (let i = 1; i < n; i++) ctx.lineTo(L[i][0], L[i][1]);
  for (let i = n - 1; i >= 0; i--) ctx.lineTo(R[i][0], R[i][1]);
  ctx.closePath();
  ctx.globalAlpha = o.alpha ?? 1;
  ctx.fillStyle = o.color;
  ctx.fill();
  if (o.dry) {
    const rand = rng(o.seed ?? 7);
    ctx.globalCompositeOperation = "destination-out";
    ctx.lineCap = "round";
    for (let j = 0; j < o.dry; j++) {
      const off = rand() - 0.5, u0 = 0.25 + rand() * 0.5, u1 = Math.min(1, u0 + 0.2 + rand() * 0.5);
      ctx.beginPath();
      let started = false;
      for (let i = 0; i < n; i++) {
        const u = (seg[i][2] - s0) / len;
        if (u < u0 || u > u1) continue;
        const x = (L[i][0] + R[i][0]) / 2 + (L[i][0] - R[i][0]) * off * 0.9;
        const y = (L[i][1] + R[i][1]) / 2 + (L[i][1] - R[i][1]) * off * 0.9;
        started ? ctx.lineTo(x, y) : ctx.moveTo(x, y);
        started = true;
      }
      ctx.lineWidth = 0.5 + rand() * w * 0.12;
      ctx.globalAlpha = 0.35 + rand() * 0.5;
      ctx.stroke();
    }
    ctx.globalCompositeOperation = "source-over";
  }
  ctx.globalAlpha = 1;
}

// A sword of light in flight: a sharp head at fraction p, its body trailing
// behind for `len` of the path, plus the faint wake it leaves on the paper.
export function sword(ctx, pts, p, o) {
  const len = o.len ?? 0.2;
  if (o.wake) stroke(ctx, pts, 0, p, { w: o.wake, color: o.color, alpha: o.wakeAlpha ?? 0.16, press: 0.02, lift: 0.02, min: 0.4 });
  if (p <= 0 || o.headAlpha === 0) return;
  stroke(ctx, pts, Math.max(0, p - len), p, { w: o.w, color: o.color, alpha: o.headAlpha ?? 1, press: 0.85, lift: 0.06, min: 0.2 });
}

export function rgba(rgb, a) {
  return `rgba(${rgb},${a})`;
}
