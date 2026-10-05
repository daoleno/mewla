// The Zen mark (two interlocking rings) rendered as ink.
// Geometry: rings-geo.js, traced from app/assets/icon.png in a 1000-unit box.
// Each ring is two visible crescents plus short "passages" that run under the
// other ring. Passages sit on the bottom layer and crescents on the top layer,
// so the over-under weave is correct at every frame of a draw.
import { RINGS } from "./rings-geo.js";

import { rng } from "./motion.js";

const DEG = Math.PI / 180;

export const ring = {
  cx: RINGS.cx, cy: RINGS.cy,
  ivory: { body: new Path2D(RINGS.ivory.d), pass: new Path2D(RINGS.ivory.pass), gaps: RINGS.ivory.gaps, mid: RINGS.ivory.mid, start: 86 },
  sage: { body: new Path2D(RINGS.sage.d), pass: new Path2D(RINGS.sage.pass), gaps: RINGS.sage.gaps, mid: RINGS.sage.mid, start: 181 },
};
// Point on a ring's centreline at angle a (degrees), in 1000-unit space.
export function ringPoint(r, a, off = 0) {
  const i = ((Math.round(a) % 360) + 360) % 360;
  const rad = r.mid[i] + off;
  return [ring.cx + rad * Math.cos(a * DEG), ring.cy + rad * Math.sin(a * DEG)];
}

function canvas(px) {
  const c = document.createElement("canvas");
  c.width = c.height = px;
  return c;
}

// Paint one ring's shapes as ink: a wet body, darker pooled edges, dry-brush
// streaks that follow the stroke and thicken toward each crescent's tail, and
// a fibre grain where the paper shows through.
function paintInk(px, path, tone, r, seed, { dry = 1, strands = 0 } = {}) {
  const c = canvas(px), x = c.getContext("2d"), k = px / 1000, rand = rng(seed);
  x.scale(k, k);
  x.save();
  x.clip(path);
  x.fillStyle = tone.body;
  x.fill(path);
  // pooled edge: ink settles at the rim of a wet stroke
  x.strokeStyle = tone.edge;
  x.lineJoin = "round";
  for (const [w, a] of [[16, 0.18], [7, 0.32], [2.4, 0.5]]) {
    x.globalAlpha = a; x.lineWidth = w; x.stroke(path);
  }
  x.globalAlpha = 1;
  if (strands) {
    // B: hairline strands that read as sword-light trails
    for (let i = 0; i < strands; i++) {
      const a0 = rand() * 360, span = 40 + rand() * 160, off = (rand() - 0.5) * 250;
      x.beginPath();
      for (let a = a0; a <= a0 + span; a += 3) {
        const [px2, py2] = ringPoint(r, a, off);
        a === a0 ? x.moveTo(px2, py2) : x.lineTo(px2, py2);
      }
      x.lineWidth = 0.6 + rand() * 1.6;
      x.strokeStyle = rand() < 0.5 ? tone.edge : tone.light;
      x.globalAlpha = 0.25 + rand() * 0.45;
      x.stroke();
    }
    x.globalAlpha = 1;
  }
  // dry brush: paper shows through in streaks along the direction of travel
  x.globalCompositeOperation = "destination-out";
  const tails = r.gaps.map(([s]) => s);
  for (let i = 0; i < 420 * dry; i++) {
    const tail = tails[i % tails.length];
    // most streaks gather toward each crescent's tail: feibai, the brush running dry
    const nearTail = rand() < 0.66;
    const depth = Math.pow(rand(), 2.2);
    const a0 = nearTail ? tail + 2 - depth * 58 : rand() * 360;
    const span = nearTail ? 10 + (1 - depth) * 30 + rand() * 14 : 3 + rand() * 14;
    const off = (rand() - 0.5) * 250;
    x.beginPath();
    for (let a = a0 - span; a <= a0; a += 1.5) {
      const [px2, py2] = ringPoint(r, a, off);
      a === a0 - span ? x.moveTo(px2, py2) : x.lineTo(px2, py2);
    }
    x.lineWidth = 0.6 + rand() * (nearTail ? 2 + (1 - depth) * 5 : 1.4);
    x.globalAlpha = nearTail ? 0.45 + (1 - depth) * 0.5 * rand() + 0.05 : 0.1 + rand() * 0.22;
    x.lineCap = "round";
    x.stroke();
  }
  // fibre grain
  for (let i = 0; i < 2600; i++) {
    x.globalAlpha = rand() * 0.22;
    x.fillRect(rand() * 1000, rand() * 1000, 0.6 + rand() * 2.4, 0.5 + rand() * 0.9);
  }
  x.restore();
  return c;
}

// The soft halo of ink spreading into xuan paper. shadowBlur works everywhere,
// unlike ctx.filter.
function paintBleed(px, path, color, blur) {
  const c = canvas(px), x = c.getContext("2d"), k = px / 1000;
  x.shadowColor = color;
  x.shadowBlur = blur * k;
  x.shadowOffsetX = 4000;
  x.translate(-4000, 0);
  x.scale(k, k);
  x.fillStyle = "#000";
  x.fill(path);
  return c;
}

function paintSolid(px, path) {
  const c = canvas(px), x = c.getContext("2d"), k = px / 1000;
  x.scale(k, k); x.fillStyle = "#000"; x.fill(path);
  return c;
}

function clipTo(src, clipPath) {
  const c = canvas(src.width), x = c.getContext("2d"), k = src.width / 1000;
  x.save(); x.scale(k, k); x.clip(clipPath); x.setTransform(1, 0, 0, 1, 0, 0);
  x.drawImage(src, 0, 0); x.restore();
  return c;
}

// Build every layer once for a given pixel size.
export function buildLayers(px, tones, opts = {}) {
  const out = {};
  for (const [name, other] of [["ivory", "sage"], ["sage", "ivory"]]) {
    const r = ring[name], tone = tones[name];
    out[name] = {
      top: paintInk(px, r.body, tone, r, name === "ivory" ? 11 : 23, opts),
      // passages are clipped to the other ring, so they vanish under it once drawn
      pass: clipTo(paintInk(px, r.pass, tone, r, name === "ivory" ? 31 : 41, opts), ring[other].body),
      bleed: paintBleed(px, r.body, tone.bleed, opts.bleedBlur ?? 22),
      solid: paintSolid(px, r.body),
    };
  }
  return out;
}

// Mask a layer by a sweep around the logo centre: [start, start + p*360], with a
// feathered leading edge like the wet front of a brush.
export function sweep(dst, scratch, layer, startDeg, p, feather = 0.03, alpha = 1, op = "source-over") {
  if (p <= 0) return;
  const s = scratch.getContext("2d"), W = scratch.width, k = W / 1000;
  s.globalCompositeOperation = "copy";
  s.drawImage(layer, 0, 0);
  if (p < 1) {
    s.globalCompositeOperation = "destination-in";
    const g = s.createConicGradient(startDeg * DEG, ring.cx * k, ring.cy * k);
    const f = Math.min(feather, p);
    g.addColorStop(0, "rgba(0,0,0,1)");
    g.addColorStop(Math.max(0, p - f), "rgba(0,0,0,1)");
    g.addColorStop(p, "rgba(0,0,0,0)");
    g.addColorStop(1, "rgba(0,0,0,0)");
    s.fillStyle = g;
    s.fillRect(0, 0, W, W);
  }
  s.globalCompositeOperation = "source-over";
  dst.globalAlpha = alpha;
  dst.globalCompositeOperation = op;
  dst.drawImage(scratch, 0, 0);
  dst.globalCompositeOperation = "source-over";
  dst.globalAlpha = 1;
}

// An over-crossing hides what lies beneath it, even where its ink is thin.
export function sweepOver(dst, scratch, L, startDeg, p, feather) {
  sweep(dst, scratch, L.solid, startDeg, p, feather, 1, "destination-out");
  sweep(dst, scratch, L.top, startDeg, p, feather);
}

// Union of several arcs, used when many strokes build one ring (direction B).
export function sweepArcs(dst, scratch, layer, arcs, alpha = 1) {
  if (!arcs.length) return;
  const s = scratch.getContext("2d"), W = scratch.width, k = W / 1000, R = 760 * k;
  s.globalCompositeOperation = "copy";
  s.drawImage(layer, 0, 0);
  s.globalCompositeOperation = "destination-in";
  s.beginPath();
  for (const [a0, a1] of arcs) {
    if (a1 <= a0) continue;
    s.moveTo(ring.cx * k, ring.cy * k);
    s.arc(ring.cx * k, ring.cy * k, R, a0 * DEG, a1 * DEG);
    s.closePath();
  }
  s.fillStyle = "#000";
  s.fill();
  s.globalCompositeOperation = "source-over";
  dst.globalAlpha = alpha;
  dst.drawImage(scratch, 0, 0);
  dst.globalAlpha = 1;
}

export function sizeCanvas(el, cssW, cssH = cssW) {
  const dpr = Math.min(2, window.devicePixelRatio || 1);
  el.width = Math.round(cssW * dpr);
  el.height = Math.round(cssH * dpr);
  return dpr;
}

export { canvas as makeCanvas };
