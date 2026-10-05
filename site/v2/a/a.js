// Direction A: the two rings drawn as ensō with a loaded brush.
// Dense ink (the ivory ring) goes down first; pale ink (the sage ring) follows
// and passes under it, then both bleed softly into the paper.
import { timeline, ease, span } from "../shared/motion.js";
import { buildLayers, sweep, sweepOver, sizeCanvas, makeCanvas, ring } from "../shared/rings.js";

const el = document.querySelector(".hero-a .rings");
const dark = matchMedia("(prefers-color-scheme: dark)").matches;
const ink = dark ? "235,230,218" : "24,23,20";
const tones = {
  ivory: { body: `rgba(${ink},0.86)`, edge: dark ? "rgba(255,252,244,1)" : "rgba(8,8,6,1)", bleed: `rgba(${ink},0.5)` },
  sage: { body: `rgba(${ink},0.22)`, edge: `rgba(${ink},0.55)`, bleed: `rgba(${ink},0.32)` },
};

const T = {
  ivory: [380, 2380],   // ms: start, end of the dense stroke
  sage: [1500, 3550],   // pale stroke starts while the first is still moving
  bleedLag: 420,
  end: 6300,
};

let layers, scratch, ctx, built = 0;
function build() {
  sizeCanvas(el, el.getBoundingClientRect().width);
  const px = el.width;
  if (built === px) return;
  built = px;
  layers = buildLayers(px, tones, { dry: 1.7 });
  scratch = makeCanvas(px);
  ctx = el.getContext("2d");
}

const prog = (name, t, lag = 0) => ease.brush(span(t - lag, T[name][0], T[name][1]));

function render(t) {
  build();
  ctx.clearRect(0, 0, el.width, el.height);
  const pi = prog("ivory", t), ps = prog("sage", t);
  const bi = prog("ivory", t, T.bleedLag), bs = prog("sage", t, T.bleedLag);
  // bottom: passages (each ring's under-crossings)
  sweep(ctx, scratch, layers.sage.pass, ring.sage.start, ps);
  sweep(ctx, scratch, layers.ivory.pass, ring.ivory.start, pi);
  // the halo of ink spreading into paper, grows in after the brush passes
  sweep(ctx, scratch, layers.sage.bleed, ring.sage.start, bs, 0.08, 0.5 * span(t, T.sage[0], T.sage[0] + 2600));
  sweep(ctx, scratch, layers.ivory.bleed, ring.ivory.start, bi, 0.08, 0.42 * span(t, T.ivory[0], T.ivory[0] + 2600));
  // top: crescents
  sweepOver(ctx, scratch, layers.ivory, ring.ivory.start, pi, 0.028);
  sweepOver(ctx, scratch, layers.sage, ring.sage.start, ps, 0.034);
}

if (el) {
  const tl = timeline(document.querySelector(".hero-a"), render, T.end);
  let w = innerWidth;
  addEventListener("resize", () => { if (innerWidth !== w) { w = innerWidth; built = 0; tl.redraw(); } });
}
