// Zen landing. The page is complete without this file: it only adds the
// header rule, the painting's parallax, the Brain steps' current mark, a
// one-time reveal for content below the fold, and the copy buttons.
const reduce = matchMedia("(prefers-reduced-motion: reduce)").matches;

const bar = document.querySelector(".bar");
const onScrollHeader = () => bar.classList.toggle("scrolled", scrollY > 8);
addEventListener("scroll", onScrollHeader, { passive: true });
onScrollHeader();

// Parallax: each ink layer drifts at its own depth while the hero is on screen.
const hero = document.querySelector(".hero");
const layers = [...document.querySelectorAll(".layer")].map((el) => ({ el, d: parseFloat(el.dataset.depth) || 0 }));
if (!reduce && layers.length) {
  let heroVisible = true;
  let queued = false;
  new IntersectionObserver(([e]) => { heroVisible = e.isIntersecting; }).observe(hero);
  const paint = () => {
    queued = false;
    if (!heroVisible) return;
    const y = scrollY;
    for (const { el, d } of layers) el.style.transform = `translate3d(0, ${(y * d).toFixed(1)}px, 0)`;
  };
  addEventListener("scroll", () => { if (!queued) { queued = true; requestAnimationFrame(paint); } }, { passive: true });
  paint();
}

// Brain steps: mark the one in the middle of the screen.
const steps = [...document.querySelectorAll(".step")];
if (steps.length) {
  const io = new IntersectionObserver((entries) => {
    for (const e of entries) if (e.isIntersecting) {
      steps.forEach((s) => s.classList.toggle("on", s === e.target));
    }
  }, { rootMargin: "-42% 0px -42% 0px" });
  steps.forEach((s) => io.observe(s));
}

// Reveal: only things still below the fold are hidden, and anything hidden
// plays as soon as any part of it is on screen (or after 450 ms, if it is
// already in view when armed). Nothing stays blank.
if (!reduce && "IntersectionObserver" in window) {
  const targets = document.querySelectorAll(
    ".brain h2, .brain .intro, .reach h2, .sessions h2, .tools h2, .install h2, .yours h2, .end h2, .cards li, .three li, .pair .phone, .side-phone, .warn, .step[data-step='5']"
  );
  const play = (el) => {
    if (!el.classList.contains("arm")) return;
    el.classList.remove("arm");
    el.classList.add("play");
  };
  const io = new IntersectionObserver((entries) => {
    for (const e of entries) if (e.isIntersecting) { play(e.target); io.unobserve(e.target); }
  }, { rootMargin: "0px 0px -8% 0px" });
  const fold = innerHeight;
  targets.forEach((el, i) => {
    if (el.getBoundingClientRect().top < fold) return;
    el.classList.add("arm");
    if (!el.classList.contains("step")) el.classList.add("rv");
    el.style.animationDelay = el.matches(".cards li, .three li, .pair .phone") ? `${(i % 3) * 90}ms` : "";
    io.observe(el);
  });
  // a jump (anchor link, restored scroll) can land with armed things in view
  const sweep = () => document.querySelectorAll(".arm").forEach((el) => {
    const r = el.getBoundingClientRect();
    if (r.top < innerHeight && r.bottom > 0) play(el);
  });
  addEventListener("hashchange", () => setTimeout(sweep, 450));
  addEventListener("scrollend", sweep);
  setTimeout(sweep, 450);
}

// Copy buttons
for (const b of document.querySelectorAll("[data-copy]")) {
  b.addEventListener("click", async () => {
    const text = b.previousElementSibling.textContent;
    try {
      await navigator.clipboard.writeText(text);
      b.textContent = "Copied";
    } catch {
      b.textContent = "Select";
    }
    setTimeout(() => { b.textContent = "Copy"; }, 1600);
  });
}
