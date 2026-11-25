// Counts a page view of mewla.ai for Mewla's own numbers: which kind of page, and where the
// visitor came from, both as one word (privacy/#website). No cookie, no id, nothing stored in
// the browser, nothing sent when the browser asks sites not to track. A download or copy of the
// install command counts as a "download" view.
(() => {
  const ENDPOINT = "https://connect.mewla.ai/v1/visit";
  const REFERRERS = {
    "github.com": "github", "google.": "google", "bing.com": "bing", "duckduckgo.com": "duckduckgo", "baidu.com": "baidu",
    "x.com": "x", "t.co": "x", "twitter.com": "x", "news.ycombinator.com": "hackernews", "reddit.com": "reddit",
    "v2ex.com": "v2ex", "producthunt.com": "producthunt", "linkedin.com": "linkedin", "youtube.com": "youtube",
  };

  const off = () => navigator.doNotTrack === "1" || window.doNotTrack === "1" || navigator.globalPrivacyControl === true || location.hostname !== "mewla.ai";

  const page = (path) => {
    if (path === "/" || path === "/index.html") return "home";
    for (const kind of ["docs", "privacy", "support"]) if (path.startsWith(`/${kind}/`) || path === `/${kind}`) return kind;
    return "other";
  };

  const referrer = () => {
    let host = "";
    try {
      host = document.referrer ? new URL(document.referrer).hostname.toLowerCase() : "";
    } catch {
      return "other";
    }
    if (!host) return "direct";
    if (host === "mewla.ai" || host.endsWith(".mewla.ai")) return "mewla";
    for (const [domain, word] of Object.entries(REFERRERS)) {
      if (domain.endsWith(".") ? host.includes(domain) : host === domain || host.endsWith(`.${domain}`)) return word;
    }
    return "other";
  };

  const count = (kind) => {
    if (off()) return;
    try {
      // A string body goes as text/plain, so no preflight; connect.mewla.ai never sets a cookie, so none goes along.
      navigator.sendBeacon(ENDPOINT, JSON.stringify({ page: kind, ref: referrer() }));
    } catch {
      // Counting never gets in the way of the page.
    }
  };

  count(page(location.pathname));
  document.addEventListener("click", (event) => {
    const target = event.target instanceof Element ? event.target.closest(".mac-get a, .cmd-card [data-copy]") : null;
    if (target) count("download");
  });
})();
