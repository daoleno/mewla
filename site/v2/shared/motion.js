// Shared motion rules for both directions.
// - The page is complete at rest: every canvas paints its final frame on load
//   and every element's base style is its finished state.
// - Motion only replays. An element that starts below the fold is "armed"
//   (reset to its first frame) the moment its edge enters the viewport, and
//   plays once enough of it is visible. Nothing waits on an observer to appear.
// - prefers-reduced-motion: final frames only, no replay.
// - ?t=1234 or window.__zenT freezes every timeline for deterministic captures;
//   window.__zenRender() repaints after changing __zenT.
export const reduceMotion = matchMedia("(prefers-reduced-motion: reduce)").matches;
const fixedT = new URLSearchParams(location.search).get("t");
export const frozen = () => window.__zenT != null || fixedT != null;
const frozenT = () => (window.__zenT != null ? +window.__zenT : +fixedT);

const renderers = new Set();
window.__zenRender = () => renderers.forEach((r) => r());

export function inView(el) {
  const r = el.getBoundingClientRect();
  return r.bottom > 0 && r.top < innerHeight;
}

// Calls arm() when el's edge enters the viewport and play() once it is well in
// view. If el is already on screen, plays at once. Returns false when motion is
// off, so the caller keeps the finished state.
export function reveal(el, { arm, play, ratio = 0.15 }) {
  if (reduceMotion || frozen() || !("IntersectionObserver" in window)) return false;
  if (inView(el)) { arm?.(); play(); return true; }
  let armed = false, done = false, timer = 0;
  const go = () => { if (done) return; done = true; clearTimeout(timer); io.disconnect(); play(); };
  const io = new IntersectionObserver((entries) => {
    for (const e of entries) {
      if (!e.isIntersecting) continue;
      // armed means hidden: never leave it that way, even if scrolling stops here
      if (!armed) { armed = true; arm?.(); timer = setTimeout(go, 450); }
      const seen = e.intersectionRect.height / Math.min(innerHeight, e.boundingClientRect.height || 1);
      if (seen >= ratio) go();
    }
  }, { threshold: [0, 0.05, 0.1, 0.15, 0.2, 0.3] });
  io.observe(el);
  return true;
}

// A timeline of `duration` ms drawn by render(t). Paints the final frame now,
// replays from 0 when revealed.
export function timeline(root, render, duration) {
  let last = duration;
  const draw = (t) => { last = t; render(t); };
  renderers.add(() => draw(frozen() ? frozenT() : last));
  if (frozen()) { draw(frozenT()); return { redraw: () => draw(last) }; }
  draw(duration);
  let t0 = 0;
  const tick = (now) => {
    const t = Math.min(now - t0, duration);
    draw(t);
    if (t < duration) requestAnimationFrame(tick);
  };
  reveal(root, {
    arm: () => draw(0),
    play: () => requestAnimationFrame((now) => { t0 = now; tick(now); }),
  });
  return { redraw: () => draw(last) };
}

export const ease = {
  inOut: (x) => (x <= 0 ? 0 : x >= 1 ? 1 : 0.5 - Math.cos(Math.PI * x) / 2),
  // a brush: slow press, quick middle, slow lift
  brush: (x) => (x <= 0 ? 0 : x >= 1 ? 1 : x < 0.5 ? 4 * x * x * x : 1 - Math.pow(-2 * x + 2, 3) / 2),
  out: (x) => (x <= 0 ? 0 : x >= 1 ? 1 : 1 - Math.pow(1 - x, 3)),
  in: (x) => (x <= 0 ? 0 : x >= 1 ? 1 : x * x * x),
};
export const clamp01 = (x) => Math.max(0, Math.min(1, x));
export const span = (t, a, b) => clamp01((t - a) / (b - a));

// Deterministic noise so captures are repeatable.
export function rng(seed) {
  let s = seed >>> 0;
  return () => ((s = (s * 1664525 + 1013904223) >>> 0) / 4294967296);
}
