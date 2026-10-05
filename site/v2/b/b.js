// Direction B: 御剑. Eight agents take off from the edges as ink sword-light.
// Each flies to its landing angle on one of the two Zen rings, then runs along
// the ring, and the ring is inked behind it: four swords draw the dense ring,
// four the pale one. When every sword has landed, their light settles into the
// rings and only faint wakes remain, the lines of command to the phone.
// Below, each capability row runs a short sword sequence of its own.
import { timeline, ease, span } from "../shared/motion.js";
import { buildLayers, sweepArcs, sweepArcsOver, sizeCanvas, makeCanvas, ring, ringPoint } from "../shared/rings.js";
import { cubic, join, polyline, stroke, sword } from "../shared/ink.js";

const dark = matchMedia("(prefers-color-scheme: dark)").matches;
const INK = dark ? "235,230,218" : "24,23,20";
const RED = dark ? "224,100,74" : "176,58,35";
const DPR = () => Math.min(2, devicePixelRatio || 1);
const rect = (el, o) => { const r = el.getBoundingClientRect(); return { x: r.left - o.left, y: r.top - o.top, w: r.width, h: r.height }; };
const DEG = Math.PI / 180;

// ---------- hero ----------
const field = document.querySelector(".field");
const flight = field.querySelector(".flight");
const wake = field.querySelector(".wake");
const ringsEl = field.querySelector(".rings");
const labels = [...field.querySelectorAll(".sw")];
const tones = {
  ivory: { body: `rgba(${INK},0.9)`, edge: `rgba(${INK},1)`, light: `rgba(${INK},0.35)`, bleed: `rgba(${INK},0.3)` },
  sage: { body: `rgba(${INK},0.26)`, edge: `rgba(${INK},0.62)`, light: `rgba(${INK},0.12)`, bleed: `rgba(${INK},0.2)` },
};
const ARC = 92; // degrees each sword inks; four per ring, a little overlap
const FLY = 1750, STAGGER = 165, T0 = 250;
const HERO_END = T0 + STAGGER * 7 + FLY + 700;

let H = null;
function layoutHero() {
  const o = field.getBoundingClientRect();
  const dpr = DPR();
  flight.width = Math.round(o.width * dpr);
  flight.height = Math.round(o.height * dpr);
  wake.width = flight.width;
  wake.height = flight.height;
  const rr = rect(ringsEl, o);
  sizeCanvas(ringsEl, rr.w);
  const px = ringsEl.width;
  const k = rr.w / 1000;
  const toField = ([x, y]) => [rr.x + x * k, rr.y + y * k];
  const C = toField([ring.cx, ring.cy]);
  // landing points, sorted round the ring; take-off points, sorted the same way,
  // then paired so no two swords cross on the way in
  const entries = [];
  for (const name of ["ivory", "sage"]) for (let i = 0; i < 4; i++) entries.push({ name, a: ring[name].start + i * 90 });
  const ang = (x, y) => (Math.atan2(y - C[1], x - C[0]) / DEG + 360) % 360;
  entries.forEach((e) => { const p = toField(ringPoint(ring[e.name], e.a)); e.p = p; e.ang = ang(...p); });
  const starts = labels.map((el, i) => {
    const r = rect(el.querySelector("i"), o);
    const x = r.x + r.w / 2, y = r.y + r.h / 2;
    return { el, i, x, y, ang: ang(x, y) };
  });
  entries.sort((a, b) => a.ang - b.ang);
  starts.sort((a, b) => a.ang - b.ang);
  let best = 0, bestCost = Infinity;
  for (let s = 0; s < 8; s++) {
    let c = 0;
    for (let i = 0; i < 8; i++) { const d = Math.abs(starts[i].ang - entries[(i + s) % 8].ang) % 360; c += Math.min(d, 360 - d); }
    if (c < bestCost) { bestCost = c; best = s; }
  }
  const swords = starts.map((s, i) => {
    const e = entries[(i + best) % 8];
    const a = e.a * DEG;
    const tan = [-Math.sin(a), Math.cos(a)];
    const reach = Math.hypot(e.p[0] - s.x, e.p[1] - s.y);
    const dir = Math.sign(C[0] - s.x) || 1;
    const run = cubic([s.x, s.y], [s.x + dir * reach * 0.42, s.y - reach * 0.08], [e.p[0] - tan[0] * reach * 0.5, e.p[1] - tan[1] * reach * 0.5], e.p, 64);
    const arcPts = [];
    for (let d = 0; d <= ARC; d += 2) arcPts.push(toField(ringPoint(ring[e.name], e.a + d)));
    const arc = polyline(arcPts);
    const path = join(run, arc);
    return { ...e, el: s.el, order: s.i, path, cut: run.L / path.L, t0: T0 + s.i * STAGGER };
  });
  if (!H || H.px !== px) {
    H = { px, layers: buildLayers(px, tones, { dry: 0.55, strands: 150, bleedBlur: 10 }), scratch: makeCanvas(px) };
  }
  Object.assign(H, { swords, dpr, ctx: flight.getContext("2d"), wctx: wake.getContext("2d"), rctx: ringsEl.getContext("2d") });
}

function heroFrame(t) {
  if (!H) layoutHero();
  const { ctx, wctx, rctx, dpr, layers, scratch } = H;
  for (const c of [ctx, wctx]) {
    c.setTransform(1, 0, 0, 1, 0, 0);
    c.clearRect(0, 0, flight.width, flight.height);
    c.setTransform(dpr, 0, 0, dpr, 0, 0);
  }
  const arcs = { ivory: [], sage: [] };
  const settle = span(t, HERO_END - 900, HERO_END);
  for (const s of H.swords) {
    const p = ease.inOut(span(t, s.t0, s.t0 + FLY));
    const q = Math.max(0, (p - s.cut) / (1 - s.cut));
    if (q > 0) arcs[s.name].push([s.a, s.a + ARC * q]);
    const heavy = s.name === "ivory";
    // the wake: the line from an agent to the ring, kept at rest, under the rings
    stroke(wctx, s.path, 0, Math.min(p, s.cut), {
      w: heavy ? 1.6 : 1.1, color: `rgb(${INK})`, alpha: (heavy ? 0.3 : 0.2) + 0.25 * (1 - settle) * (p > 0 ? 1 : 0),
      press: 0.02, lift: 0.04, min: 0.5,
    });
    // the sword itself, fading into the ring once it has landed
    const glow = p <= 0 ? 0 : 1 - span(t, s.t0 + FLY - 80, s.t0 + FLY + 520);
    if (glow > 0) sword(ctx, s.path, p, { w: heavy ? 5.2 : 4, len: 150 / s.path.L, color: `rgb(${INK})`, headAlpha: glow });
    if (s.el) s.el.style.opacity = t >= HERO_END ? "" : String(0.4 + 0.6 * span(t, s.t0 - 260, s.t0));
  }
  rctx.clearRect(0, 0, ringsEl.width, ringsEl.height);
  sweepArcs(rctx, scratch, layers.sage.pass, arcs.sage);
  sweepArcs(rctx, scratch, layers.ivory.pass, arcs.ivory);
  sweepArcs(rctx, scratch, layers.sage.bleed, arcs.sage, 0.5 * settle);
  sweepArcs(rctx, scratch, layers.ivory.bleed, arcs.ivory, 0.5 * settle);
  sweepArcsOver(rctx, scratch, layers.ivory, arcs.ivory);
  sweepArcsOver(rctx, scratch, layers.sage, arcs.sage);
}

const hero = timeline(document.querySelector(".hero-b"), heroFrame, HERO_END);

// ---------- capability rows ----------
const ROW_END = 2600;
function rowFlow(row) {
  const cv = row.querySelector(".flow");
  const kind = row.dataset.flow;
  let G = null;
  const layout = () => {
    const o = cv.getBoundingClientRect(), dpr = DPR();
    cv.width = Math.round(o.width * dpr);
    cv.height = Math.round(o.height * dpr);
    const front = rect(row.querySelector(".front"), o);
    const tags = [...row.querySelectorAll(".tags span")].map((el) => rect(el, o));
    const paths = [];
    if (kind === "converge") {
      // agents fly in from the left and enter the phone
      tags.forEach((r, i) => {
        const s = [r.x + r.w + 4, r.y + r.h / 2], e = [front.x + 30, front.y + front.h * (0.34 + i * 0.1)];
        paths.push({ pts: cubic(s, [s[0] + 90, s[1] - 26], [e[0] - 110, e[1] + 18], e), t0: 150 + i * 190, red: false });
      });
    } else if (kind === "split") {
      // one goal leaves the phone and divides into three Workers
      const s = [front.x + front.w - 26, front.y + front.h * 0.42];
      tags.forEach((r, i) => {
        const e = [r.x - 6, r.y + r.h / 2], mx = s[0] + (e[0] - s[0]) * 0.36;
        paths.push({ pts: join(cubic(s, [s[0] + 40, s[1]], [mx - 40, s[1]], [mx, s[1]], 24), cubic([mx, s[1]], [mx + 60, s[1]], [e[0] - 90, e[1]], e)), t0: 200, red: false });
      });
    } else {
      // two agents keep working out of the way; the one that needs you comes to the phone
      const W = o.width, Hh = o.height;
      paths.push({ pts: cubic([W * 0.02, Hh * 0.2], [W * 0.14, Hh * 0.06], [W * 0.24, Hh * 0.3], [W * 0.14, Hh * 0.42]), t0: 0, quiet: true });
      paths.push({ pts: cubic([W * 0.98, Hh * 0.9], [W * 0.86, Hh * 0.98], [W * 0.78, Hh * 0.72], [W * 0.88, Hh * 0.62]), t0: 120, quiet: true });
      const e = [front.x + front.w * 0.5, front.y + front.h * 0.3];
      paths.push({ pts: cubic([W * 0.99, Hh * 0.04], [W * 0.84, Hh * 0.02], [e[0] + 180, e[1] - 140], e), t0: 380, red: true });
    }
    G = { ctx: cv.getContext("2d"), dpr, paths };
  };
  const frame = (t) => {
    if (!G) layout();
    const { ctx, dpr } = G;
    ctx.setTransform(1, 0, 0, 1, 0, 0);
    ctx.clearRect(0, 0, cv.width, cv.height);
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    for (const s of G.paths) {
      const dur = s.red ? 900 : 1200;
      const p = ease.inOut(span(t, s.t0, s.t0 + dur));
      const rgb = s.red ? RED : INK;
      stroke(ctx, s.pts, 0, p, { w: s.red ? 2 : s.quiet ? 1 : 1.4, color: `rgb(${rgb})`, alpha: s.red ? 0.55 : s.quiet ? 0.2 : 0.32, press: 0.02, lift: 0.04, min: 0.5 });
      const glow = p <= 0 ? 0 : 1 - span(t, s.t0 + dur - 60, s.t0 + dur + 420);
      if (glow > 0 && !s.quiet) sword(ctx, s.pts, p, { w: s.red ? 5 : 4, len: 110 / s.pts.L, color: `rgb(${rgb})`, headAlpha: glow });
      if (s.quiet && glow > 0) sword(ctx, s.pts, p, { w: 3, len: 80 / s.pts.L, color: `rgb(${rgb})`, headAlpha: glow * 0.5 });
    }
  };
  const tl = timeline(row, frame, ROW_END);
  return () => { G = null; tl.redraw(); };
}
const rows = [...document.querySelectorAll(".row[data-flow]")].map(rowFlow);

let w = innerWidth;
addEventListener("resize", () => {
  if (innerWidth === w) return;
  w = innerWidth;
  H = H && { px: H.px, layers: H.layers, scratch: H.scratch };
  layoutHero();
  hero.redraw();
  rows.forEach((r) => r());
});
// fonts move the labels: re-measure once they settle
document.fonts?.ready.then(() => { layoutHero(); hero.redraw(); rows.forEach((r) => r()); });
