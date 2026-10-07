// Mewla docs: mobile menu and client-side search over docs/search-index.json,
// which scripts/site-docs/build.py writes as [{t: title, u: url, h: [[heading, id]], x: text}].
(() => {
  const root = document.body.dataset.docsRoot || "./";
  const side = document.getElementById("d-side");
  const menu = document.querySelector(".d-menu");
  const input = document.getElementById("d-q");
  const list = document.getElementById("d-results");

  const setMenu = (open) => {
    side.classList.toggle("open", open);
    menu.setAttribute("aria-expanded", String(open));
    menu.textContent = open ? "Close" : "Menu";
    document.body.classList.toggle("d-locked", open);
  };
  menu.addEventListener("click", () => setMenu(!side.classList.contains("open")));
  side.addEventListener("click", (e) => {
    if (e.target.closest("a") && side.classList.contains("open")) setMenu(false);
  });
  window.addEventListener("hashchange", () => side.classList.contains("open") && setMenu(false));

  let index = null;
  let loading = null;
  const load = () =>
    (loading ||= fetch(root + "search-index.json")
      .then((r) => r.json())
      .then((data) => (index = data))
      .catch(() => (index = [])));

  const escape = (s) =>
    s.replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
  const highlight = (s, terms) => {
    let out = escape(s);
    for (const t of terms) {
      out = out.replace(new RegExp(t.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"), "gi"), (m) => `<mark>${m}</mark>`);
    }
    return out;
  };

  const search = (query) => {
    const terms = query.toLowerCase().split(/\s+/).filter(Boolean);
    const results = [];
    for (const page of index) {
      const title = page.t.toLowerCase();
      const text = page.x.toLowerCase();
      const heads = page.h.map(([label, id]) => [label, id, label.toLowerCase()]);
      const hit = (t) => title.includes(t) || text.includes(t) || heads.some((h) => h[2].includes(t));
      if (!terms.every(hit)) continue;
      let score = terms.filter((t) => title.includes(t)).length * 10;
      let best = null;
      let bestScore = 0;
      for (const h of heads) {
        const n = terms.filter((t) => h[2].includes(t)).length;
        if (n > bestScore) [best, bestScore] = [h, n];
      }
      score += bestScore * 5 + terms.filter((t) => text.includes(t)).length;
      const at = text.indexOf(terms[0]);
      const snippet =
        at < 0 ? page.x.slice(0, 110) : (at > 50 ? "…" : "") + page.x.slice(Math.max(0, at - 50), at + 90) + "…";
      results.push({
        score,
        href: root + page.u + (best ? "#" + best[1] : ""),
        title: page.t,
        sub: best ? best[0] : snippet,
      });
    }
    return results.sort((a, b) => b.score - a.score).slice(0, 8).map((r) => ({ ...r, terms }));
  };

  const render = () => {
    const query = input.value.trim();
    if (!query) {
      list.hidden = true;
      list.innerHTML = "";
      return;
    }
    if (!index) {
      load().then(render);
      return;
    }
    const results = search(query);
    list.hidden = false;
    list.innerHTML = results.length
      ? results
          .map((r) => `<li><a href="${escape(r.href)}"><b>${highlight(r.title, r.terms)}</b><span>${highlight(r.sub, r.terms)}</span></a></li>`)
          .join("")
      : '<li class="none">No results</li>';
  };

  input.addEventListener("focus", load, { once: true });
  input.addEventListener("input", render);
  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter") {
      const first = list.querySelector("a");
      if (first) location.href = first.href;
    } else if (e.key === "Escape") {
      input.value = "";
      render();
    }
  });
  document.addEventListener("keydown", (e) => {
    if (e.key === "/" && !/^(INPUT|TEXTAREA|SELECT)$/.test(document.activeElement.tagName)) {
      e.preventDefault();
      if (getComputedStyle(side).display === "none") setMenu(true);
      input.focus();
    } else if (e.key === "Escape" && side.classList.contains("open")) {
      setMenu(false);
    }
  });
})();
