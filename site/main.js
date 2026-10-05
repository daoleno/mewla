(() => {
  const reduced = matchMedia("(prefers-reduced-motion: reduce)").matches;
  const dark = matchMedia("(prefers-color-scheme: dark)");

  // Parts of the page arrive slowly as they come into view.
  const parts = document.querySelectorAll(".rv");
  if (!reduced && "IntersectionObserver" in window) {
    const io = new IntersectionObserver((entries) => {
      for (const e of entries) {
        if (e.isIntersecting) {
          e.target.classList.add("in");
          io.unobserve(e.target);
        }
      }
    }, { rootMargin: "0px 0px -12% 0px" });
    parts.forEach((el) => io.observe(el));
  } else {
    parts.forEach((el) => el.classList.add("in"));
  }

  // Copy the install line.
  document.querySelectorAll("[data-copy]").forEach((btn) => {
    btn.addEventListener("click", async () => {
      const text = document.getElementById(btn.dataset.copy).textContent;
      try {
        await navigator.clipboard.writeText(text);
        btn.textContent = "Copied";
      } catch {
        btn.textContent = "Select it";
      }
      setTimeout(() => { btn.textContent = "Copy"; }, 1800);
    });
  });

  garden(document.querySelector(".garden canvas"));

  // Karesansui. Each raked line is an agent at work; they flow as streamlines
  // of still water around one stone, the Session that needs you. Line shapes
  // are solved once per size; a frame only adds a slow drift and the ripple
  // left by the pointer or by scrolling.
  function garden(canvas) {
    if (!canvas) return;
    const ctx = canvas.getContext("2d");
    const KX = 1.4; // stretch the stone's field sideways, as a raked garden does
    const LABELS = ["claude · onboarding", "codex · tests", "brain", "pi · changelog", "opencode · docs", "shell · ~/zen"];
    let W = 0, H = 0, dpr = 1;
    let colors = {};
    let g = null; // geometry for the current size
    let arrive = reduced ? 1 : 0; // the stone settles in, 0..1
    let arrivedAt = 0;
    let running = false, visible = false, seen = false, raf = 0;
    let t = 0, last = 0;
    const ptr = { x: -1e4, y: -1e4, ex: -1e4, ey: -1e4, e: 0, inside: false };
    let phase = 0, stir = 0, lastScroll = scrollY;

    function readColors() {
      const s = getComputedStyle(document.documentElement);
      const v = (n) => s.getPropertyValue(n).trim();
      colors = { ink: v("--ink"), ink2: v("--ink2"), faint: v("--faint"), rake: v("--rake"), seal: v("--seal") };
    }

    function layout() {
      const r = canvas.getBoundingClientRect();
      W = r.width;
      H = r.height;
      dpr = Math.min(2, devicePixelRatio || 1);
      canvas.width = Math.round(W * dpr);
      canvas.height = Math.round(H * dpr);
      const narrow = W < 640;
      const gap = narrow ? 13 : 15;
      const pad = gap * 1.2;
      const n = Math.floor((H - 2 * pad) / gap);
      const y0 = (H - (n - 1) * gap) / 2;
      const j = Math.round(n * 0.52);
      const stone = {
        x: W * (narrow ? 0.64 : 0.66),
        y: y0 + (j - 0.5) * gap,
        r: Math.max(22, Math.min(54, Math.min(W, H) * 0.075)),
      };
      const rings = 3;
      const ringGap = gap;
      const A = stone.r + ringGap * (rings + 0.9);
      const step = narrow ? 5 : 6;
      const xs = [];
      for (let x = -step; x <= W + step; x += step) xs.push(x);
      g = { gap, n, y0, stone, rings, ringGap, A, xs, narrow, shape: stoneShape(stone.r), lines: [], solvedFor: -1 };
      solve(arrive);
    }

    // A pebble-like outline: an ellipse with a little irregularity, fixed per size.
    function stoneShape(r) {
      const pts = [];
      let seed = 11;
      const rnd = () => ((seed = (seed * 16807) % 2147483647) / 2147483647);
      const k = [rnd(), rnd(), rnd()];
      for (let i = 0; i < 48; i++) {
        const a = (i / 48) * Math.PI * 2;
        const w = 1 + 0.06 * Math.sin(a * 2 + k[0] * 6) + 0.04 * Math.sin(a * 3 + k[1] * 6) + 0.02 * Math.sin(a * 5 + k[2] * 6);
        pts.push([Math.cos(a) * r * 1.32 * w, Math.sin(a) * r * 0.82 * w - (Math.sin(a) < 0 ? r * 0.08 : 0)]);
      }
      return pts;
    }

    // Streamline of potential flow around a cylinder of radius A:
    // psi = Y (1 - A^2 / (X^2 + Y^2)). Solve Y for a given X and psi.
    function streamY(X, psi, A) {
      if (A <= 0 || psi === 0) return psi;
      const s = Math.sign(psi), p = Math.abs(psi);
      const A2 = A * A, X2 = X * X;
      let lo = Math.max(p, Math.sqrt(Math.max(0, A2 - X2))), hi = p + A;
      for (let i = 0; i < 18; i++) {
        const m = (lo + hi) / 2;
        if (m * (1 - A2 / (X2 + m * m)) < p) lo = m; else hi = m;
      }
      return s * (lo + hi) / 2;
    }

    function solve(k) {
      const { n, y0, gap, stone, xs } = g;
      const A = g.A * ease(k);
      g.lines = [];
      for (let i = 0; i < n; i++) {
        const psi = y0 + i * gap - stone.y;
        const ys = new Float32Array(xs.length);
        for (let q = 0; q < xs.length; q++) ys[q] = stone.y + streamY((xs[q] - stone.x) / KX, psi, A);
        g.lines.push(ys);
      }
      g.solvedFor = k;
    }

    function ease(x) { return x < 0.5 ? 4 * x * x * x : 1 - Math.pow(-2 * x + 2, 3) / 2; }

    function draw() {
      const { xs, stone, narrow } = g;
      const k = ease(arrive);
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.clearRect(0, 0, W, H);

      // Ripple from the eased pointer, and a drift that never quite stops.
      const e = ptr.e, sig = 92, sig2 = 2 * sig * sig;
      const drift = reduced ? 0 : 0.9 + e * 1.4 + stir * 2.2;
      ctx.beginPath();
      for (let i = 0; i < g.lines.length; i++) {
        const ys = g.lines[i];
        for (let q = 0; q < xs.length; q++) {
          const x = xs[q];
          let y = ys[q];
          if (!reduced) {
            y += drift * Math.sin(x * 0.0085 + t * 0.32 + i * 0.6 + phase);
            if (e > 0.002) {
              const dx = x - ptr.ex, dy = y - ptr.ey, d2 = dx * dx + dy * dy;
              if (d2 < sig2 * 4) {
                const f = Math.exp(-d2 / sig2);
                y += e * f * (12 * Math.tanh(dy / 26) + 2.4 * Math.sin(Math.sqrt(d2) * 0.07 - t * 1.4));
              }
            }
          }
          if (q === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
        }
      }

      // Rings raked around the stone.
      for (let r = 1; r <= g.rings; r++) {
        const rad = (stone.r + g.ringGap * (r - 0.1)) * k;
        const wob = reduced ? 0 : 0.5 * Math.sin(t * 0.4 + r) * k;
        if (rad + wob <= 0) continue;
        ctx.moveTo(stone.x + (rad + wob) * KX, stone.y);
        ctx.ellipse(stone.x, stone.y, (rad + wob) * KX, rad + wob, 0, 0, Math.PI * 2);
      }
      ctx.lineWidth = 1;
      ctx.strokeStyle = colors.rake;
      ctx.stroke();

      // The stone.
      if (k > 0) {
        ctx.save();
        ctx.translate(stone.x, stone.y);
        ctx.scale(k, k);
        ctx.beginPath();
        g.shape.forEach(([x, y], i) => (i ? ctx.lineTo(x, y) : ctx.moveTo(x, y)));
        ctx.closePath();
        ctx.globalAlpha = 0.92;
        ctx.fillStyle = colors.ink2;
        ctx.fill();
        ctx.restore();
      }

      // Labels sit on the lines; each clears a little space in the gravel.
      ctx.font = `italic 300 ${narrow ? 12 : 13}px Newsreader, Georgia, serif`;
      ctx.textBaseline = "middle";
      const lx = narrow ? 16 : Math.max(24, W * 0.05);
      const names = narrow ? [LABELS[0], LABELS[2], LABELS[5]] : LABELS;
      const step = Math.floor(g.lines.length / (names.length + 1));
      const q0 = Math.max(0, Math.round(lx / (xs[1] - xs[0])) + 1);
      names.forEach((name, i) => {
        const line = g.lines[Math.min(g.lines.length - 1, step * (i + 1) - (i % 2))];
        label(name, lx, line[q0], colors.faint);
      });
      if (k > 0.6) {
        ctx.globalAlpha = Math.min(1, (k - 0.6) / 0.4);
        const sx = narrow ? stone.x - stone.r * 1.2 : stone.x + g.A * KX + 18;
        const sy = narrow ? stone.y + g.A + 20 : stone.y;
        label("session-auth", sx + 14, sy - 9, colors.ink, 8);
        label("needs you", sx + 14, sy + 10, colors.seal);
        ctx.fillStyle = colors.seal;
        ctx.save();
        ctx.translate(sx + 3.5, sy - 9);
        ctx.rotate(0.07);
        ctx.fillRect(-3.5, -3.5, 7, 7);
        ctx.restore();
        ctx.globalAlpha = 1;
      }
    }

    function label(text, x, y, color, padLeft = 0) {
      const w = ctx.measureText(text).width;
      ctx.save();
      ctx.globalCompositeOperation = "destination-out";
      ctx.fillStyle = "#000";
      ctx.fillRect(x - 8 - padLeft, y - 9, w + 16 + padLeft, 18);
      ctx.restore();
      ctx.fillStyle = color;
      ctx.fillText(text, x, y);
    }

    function frame(now) {
      raf = 0;
      if (!running) return;
      const dt = Math.min(0.05, last ? (now - last) / 1000 : 0.016);
      last = now;
      t += dt;
      if (arrive < 1 && seen) {
        if (!arrivedAt) arrivedAt = now + 700;
        arrive = Math.max(0, Math.min(1, (now - arrivedAt) / 2600));
        solve(arrive);
      } else if (arrive >= 1 && g.solvedFor !== 1) {
        solve(1);
      }
      const follow = 1 - Math.pow(0.04, dt); // reaches the pointer in about a second
      ptr.ex += (ptr.x - ptr.ex) * follow;
      ptr.ey += (ptr.y - ptr.ey) * follow;
      ptr.e *= Math.pow(0.35, dt);
      stir *= Math.pow(0.3, dt);
      draw();
      raf = requestAnimationFrame(frame);
    }

    function sync() {
      const go = visible && !document.hidden && !reduced;
      if (go && !running) {
        running = true;
        last = 0;
        raf = requestAnimationFrame(frame);
      } else if (!go && running) {
        running = false;
        cancelAnimationFrame(raf);
        raf = 0;
      }
    }

    readColors();
    layout();
    draw();
    new ResizeObserver(() => { layout(); draw(); }).observe(canvas);
    dark.addEventListener("change", () => { readColors(); draw(); });
    if (document.fonts) document.fonts.ready.then(draw);
    if (reduced) return;

    // The stone settles in once the garden is well in view.
    new IntersectionObserver(([entry]) => {
      visible = entry.isIntersecting;
      if (entry.intersectionRatio >= 0.35) seen = true;
      sync();
    }, { threshold: [0, 0.35] }).observe(canvas);
    document.addEventListener("visibilitychange", sync);
    canvas.addEventListener("pointermove", (ev) => {
      const r = canvas.getBoundingClientRect();
      const x = ev.clientX - r.left, y = ev.clientY - r.top;
      if (!ptr.inside) { ptr.ex = x; ptr.ey = y; ptr.inside = true; }
      ptr.e = Math.min(1, ptr.e + Math.hypot(x - ptr.x, y - ptr.y) * 0.004);
      ptr.x = x;
      ptr.y = y;
    }, { passive: true });
    canvas.addEventListener("pointerleave", () => { ptr.inside = false; }, { passive: true });
    addEventListener("scroll", () => {
      if (!visible) return;
      const d = scrollY - lastScroll;
      lastScroll = scrollY;
      phase += d * 0.004;
      stir = Math.min(1, stir + Math.abs(d) * 0.006);
    }, { passive: true });
  }
})();
