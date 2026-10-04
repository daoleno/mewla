(() => {
  const reduced = matchMedia("(prefers-reduced-motion: reduce)");
  const root = document.documentElement;
  let motionPaused = false;
  const players = [];

  // Header hairline once the page scrolls.
  const top = document.querySelector(".top");
  const onScroll = () => top.classList.toggle("scrolled", scrollY > 8);
  addEventListener("scroll", onScroll, { passive: true });
  onScroll();

  // One switch stops every animation and autoplay on the page.
  const motionButton = document.querySelector("[data-motion]");
  motionButton.addEventListener("click", () => {
    motionPaused = !motionPaused;
    root.classList.toggle("paused", motionPaused);
    motionButton.setAttribute("aria-pressed", String(motionPaused));
    motionButton.textContent = motionPaused ? "Play motion" : "Pause motion";
    players.forEach((p) => p.sync());
  });

  // The SVG files under assets/ are shared with the README, where they play
  // their own CSS loop. Here they are inlined so the page can drive them.
  async function inline(img) {
    const res = await fetch(img.currentSrc || img.src);
    if (!res.ok) throw new Error(`${res.status} ${img.src}`);
    const doc = new DOMParser().parseFromString(await res.text(), "image/svg+xml");
    const svg = document.importNode(doc.documentElement, true);
    if (svg.nodeName !== "svg") throw new Error(`not an SVG: ${img.src}`);
    svg.classList.add("is-live");
    const live = svg.getAttribute("data-live-viewbox");
    if (live) svg.setAttribute("viewBox", live);
    svg.setAttribute("width", img.getAttribute("width"));
    svg.setAttribute("height", img.getAttribute("height"));
    img.replaceWith(svg);
    return svg;
  }

  // Narrow layout: attributes named data-n-<attr> replace <attr> when the
  // figure is too small for the wide drawing.
  function responsive(svg, figure) {
    const swaps = [];
    [svg, ...svg.querySelectorAll("*")].forEach((el) => {
      for (const { name, value } of [...el.attributes]) {
        if (!name.startsWith("data-n-")) continue;
        let attr = name.slice(7);
        if (attr === "viewbox") attr = "viewBox";
        swaps.push({ el, attr, narrow: value, wide: el.getAttribute(attr) });
      }
    });
    if (!swaps.length) return;
    let current = null;
    const apply = () => {
      const narrow = figure.clientWidth < Number(figure.dataset.narrowBelow || 600);
      if (narrow === current) return;
      current = narrow;
      swaps.forEach(({ el, attr, narrow: n, wide: w }) => {
        const v = narrow ? n : w;
        if (v === null) el.removeAttribute(attr);
        else el.setAttribute(attr, v);
      });
      const [, , vw, vh] = svg.getAttribute("viewBox").split(/\s+/).map(Number);
      svg.setAttribute("width", vw);
      svg.setAttribute("height", vh);
    };
    new ResizeObserver(apply).observe(figure);
    apply();
  }

  // Steps through numbered states while visible, until the reader takes over.
  function player(figure, svg, count, interval, onState) {
    const p = { state: 1, auto: true, visible: false, timer: null };
    const set = (n) => {
      p.state = n;
      svg.setAttribute("data-state", String(n));
      onState(n);
    };
    p.sync = () => {
      clearInterval(p.timer);
      if (p.auto && p.visible && !motionPaused && !reduced.matches) {
        p.timer = setInterval(() => set((p.state % count) + 1), interval);
      }
    };
    p.take = (n) => {
      p.auto = false;
      set(n);
      p.sync();
    };
    new IntersectionObserver(([e]) => {
      p.visible = e.isIntersecting;
      p.sync();
    }, { threshold: 0.35 }).observe(figure);
    reduced.addEventListener("change", p.sync);
    players.push(p);
    set(1);
    return p;
  }

  function stepped(figure, svg) {
    const buttons = [...figure.querySelectorAll(".stepper button")];
    const captions = [...figure.querySelectorAll("[data-caption]")];
    const p = player(figure, svg, Number(figure.dataset.states), Number(figure.dataset.interval), (n) => {
      buttons.forEach((b) => b.setAttribute("aria-pressed", String(b.dataset.state === String(n))));
      captions.forEach((c) => (c.hidden = c.dataset.caption !== String(n)));
    });
    buttons.forEach((b) => b.addEventListener("click", () => p.take(Number(b.dataset.state))));
  }

  // switch.svg: the client seats inside the drawing are the controls.
  function switcher(figure, svg) {
    const order = ["codex", "claude", "pi", "grok", "agent", "opencode"];
    // Its seats are controls, so the drawing cannot be a single image.
    svg.setAttribute("role", "group");
    const picks = [...svg.querySelectorAll("[data-pick]")];
    const label = (el) => el.getAttribute("aria-label");
    const mark = () =>
      picks.forEach((el) => {
        const key = el.dataset.pick === "host" ? "data-host" : "data-exec";
        el.setAttribute("aria-pressed", String(svg.getAttribute(key) === el.dataset.value));
      });
    picks.forEach((el) => {
      el.setAttribute("role", "button");
      el.setAttribute("tabindex", "0");
      el.setAttribute("aria-label", label(el));
      const choose = () => {
        p.auto = false;
        p.sync();
        svg.setAttribute(el.dataset.pick === "host" ? "data-host" : "data-exec", el.dataset.value);
        mark();
      };
      el.addEventListener("click", choose);
      el.addEventListener("keydown", (ev) => {
        if (ev.key === "Enter" || ev.key === " ") {
          ev.preventDefault();
          choose();
        }
      });
    });
    // Autoplay alternates: move Brain, then pick a Worker client.
    const p = player(figure, svg, order.length * 2, 2600, (n) => {
      const i = Math.floor((n - 1) / 2);
      if (n % 2) svg.setAttribute("data-host", order[i]);
      else svg.setAttribute("data-exec", order[(i + 1) % order.length]);
      mark();
    });
    svg.removeAttribute("data-state");
  }

  document.querySelectorAll("img[data-inline]").forEach(async (img) => {
    const figure = img.closest("figure");
    try {
      const svg = await inline(img);
      responsive(svg, figure);
      const kind = figure.dataset.figure;
      if (kind === "dispatch" || kind === "routing") stepped(figure, svg);
      if (kind === "switch") switcher(figure, svg);
    } catch (err) {
      // The <img> stays and plays the SVG's own loop.
      console.warn("inline SVG skipped:", err.message);
    }
  });

  // Durable Work: replay a restart in the Event trail.
  const ledger = document.querySelector("[data-ledger]");
  const restart = document.querySelector("[data-restart]");
  restart.addEventListener("click", () => {
    const marker = ledger.querySelector(".restart");
    const later = [...ledger.querySelectorAll(".later")];
    if (!marker.hidden) {
      marker.hidden = true;
      later.forEach((li) => (li.hidden = true));
      restart.textContent = "Restart the daemon";
      return;
    }
    ledger.classList.remove("rebooting");
    void ledger.offsetWidth;
    ledger.classList.add("rebooting");
    marker.hidden = false;
    marker.classList.add("enter");
    const quick = reduced.matches || motionPaused;
    later.forEach((li, i) => {
      const show = () => {
        li.hidden = false;
        li.classList.add("enter");
      };
      quick ? show() : setTimeout(show, 500 + i * 420);
    });
    restart.textContent = "Reset the trail";
  });

  // Phone screens.
  const tabs = [...document.querySelectorAll('[role="tab"]')];
  const selectTab = (tab, focus) => {
    tabs.forEach((t) => {
      const on = t === tab;
      t.setAttribute("aria-selected", String(on));
      t.tabIndex = on ? 0 : -1;
      document.getElementById(t.getAttribute("aria-controls")).hidden = !on;
      const note = document.querySelector(`[data-note="${t.id.slice(4)}"]`);
      if (note) note.hidden = !on;
    });
    if (focus) tab.focus();
  };
  tabs.forEach((tab, i) => {
    tab.addEventListener("click", () => selectTab(tab));
    tab.addEventListener("keydown", (ev) => {
      const step = { ArrowRight: 1, ArrowLeft: -1 }[ev.key];
      if (step) selectTab(tabs[(i + step + tabs.length) % tabs.length], true);
      if (ev.key === "Home") selectTab(tabs[0], true);
      if (ev.key === "End") selectTab(tabs[tabs.length - 1], true);
    });
  });

  // Copy buttons.
  document.querySelectorAll("[data-copy]").forEach((button) => {
    button.addEventListener("click", async () => {
      const text = document.getElementById(button.dataset.copy).textContent;
      try {
        await navigator.clipboard.writeText(text);
        button.textContent = "Copied";
      } catch {
        button.textContent = "Select";
      }
      setTimeout(() => (button.textContent = "Copy"), 1600);
    });
  });
})();
