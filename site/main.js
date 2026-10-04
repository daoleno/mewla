(() => {
  const reduced = matchMedia("(prefers-reduced-motion: reduce)");

  // Header hairline once the page scrolls.
  const top = document.querySelector(".top");
  const onScroll = () => top.classList.toggle("scrolled", scrollY > 8);
  addEventListener("scroll", onScroll, { passive: true });
  onScroll();

  // The SVG files under assets/ are shared with the README, where they show
  // their default state. Here they are inlined so the page can drive them.
  async function inline(img) {
    const res = await fetch(img.currentSrc || img.src);
    if (!res.ok) throw new Error(`${res.status} ${img.src}`);
    const doc = new DOMParser().parseFromString(await res.text(), "image/svg+xml");
    const svg = document.importNode(doc.documentElement, true);
    if (svg.nodeName !== "svg") throw new Error(`not an SVG: ${img.src}`);
    svg.removeAttribute("width");
    svg.removeAttribute("height");
    const picture = img.parentElement.nodeName === "PICTURE" ? img.parentElement : null;
    (picture || img).replaceWith(svg);
    return svg;
  }

  // A figure with [data-fig] holds buttons that set data-state on its SVG.
  // [data-state="n"] selects a state; [data-toggle] flips between 1 and 2.
  function bind(fig, svg) {
    const choices = [...fig.querySelectorAll("button[data-state]")];
    const toggles = [...fig.querySelectorAll("button[data-toggle]")];
    let state = 1;
    let timer = null;
    let visible = false;
    let auto = fig.hasAttribute("data-autoplay");

    const set = (n) => {
      state = n;
      svg.setAttribute("data-state", String(n));
      choices.forEach((b) => b.setAttribute("aria-pressed", String(Number(b.dataset.state) === n)));
      toggles.forEach((b) => b.setAttribute("aria-pressed", String(n === 2)));
      const cap = fig.querySelector("[data-caption]");
      const active = choices.find((b) => Number(b.dataset.state) === n);
      if (cap && active) cap.textContent = [...active.querySelectorAll("b, span:last-child")].map((el) => el.textContent).join(". ");
    };
    const takeOver = () => {
      auto = false;
      sync();
    };
    choices.forEach((b) => b.addEventListener("click", () => { takeOver(); set(Number(b.dataset.state)); }));
    toggles.forEach((b) => b.addEventListener("click", () => { takeOver(); set(state === 2 ? 1 : 2); }));

    // Autoplay steps through the states while the figure is on screen, until
    // the reader picks one. Reduced motion never autoplays.
    const count = choices.length;
    function sync() {
      clearInterval(timer);
      timer = null;
      if (auto && visible && !reduced.matches && count > 1) {
        timer = setInterval(() => set((state % count) + 1), Number(fig.dataset.autoplay));
      }
      fig.classList.toggle("playing", timer !== null);
    }
    fig.style.setProperty("--dur", `${Number(fig.dataset.autoplay) || 4000}ms`);
    if (auto) {
      new IntersectionObserver(([entry]) => {
        visible = entry.isIntersecting;
        sync();
      }, { threshold: 0.35 }).observe(fig);
      reduced.addEventListener("change", sync);
    }
    set(1);
  }

  document.querySelectorAll("img[data-inline]").forEach((img) => {
    const fig = img.closest("[data-fig]");
    inline(img)
      .then((svg) => { if (fig) bind(fig, svg); })
      .catch((err) => console.warn("drawing stays static:", err.message));
  });

  // Copy buttons in Install.
  document.querySelectorAll("[data-copy]").forEach((btn) => {
    btn.addEventListener("click", async () => {
      const text = document.getElementById(btn.dataset.copy).textContent;
      try {
        await navigator.clipboard.writeText(text);
        btn.textContent = "Copied";
      } catch {
        btn.textContent = "Select";
      }
      setTimeout(() => { btn.textContent = "Copy"; }, 1600);
    });
  });
})();
