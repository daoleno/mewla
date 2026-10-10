const PAIR_FRAGMENT_KEY = "pair";

/**
 * `mewla pair` prints `<origin>/#pair=<mewla pairing link>` for browsers. The fragment never
 * reaches the daemon or proxies; it is read once and removed from history.
 */
export async function readInitialConnectLink(): Promise<string | null> {
  if (typeof window === "undefined") return null;
  const fragment = new URLSearchParams(window.location.hash.replace(/^#/, ""));
  if (!fragment.has(PAIR_FRAGMENT_KEY)) return null;
  const link = fragment.get(PAIR_FRAGMENT_KEY)?.trim() || "";
  window.history.replaceState(
    window.history.state,
    "",
    window.location.pathname + window.location.search,
  );
  return link || null;
}
