import { effect, host, platform, signal, untracked } from "nsh";
import { poppedPage, WebHistory, type PoppedPage } from "../../core/web/history";
import { documentTitle, placePath, type WebPlace } from "../../core/web/places";

/** A browser page served by the computer: the page host (client/ui/web/host.js) answers `web.*`. */
export const webHosted = platform.os === "web" && host.available();

/** What a history entry reopens: a section and its stack's routes. */
export interface PageSnapshot {
  tab: number;
  routes: readonly unknown[];
}

/**
 * Every place gets a URL and the tab a title, and Back, Forward and reload
 * return to it. The page host does what `WebHistory` decides
 * (`web.setLocation {path, title, mode, state}`), says where the page loaded
 * (`web.initialPath` → `{path, state}`) and reports Back and Forward
 * (`web.popstate {path, state}`). Without a page host (`nsh test` without
 * those handlers, the desktops) nothing of this runs. Run inside the shell.
 */
export function syncWebLocation(options: {
  place: () => WebPlace;
  snapshot: () => PageSnapshot;
  /** The page's own title (a Session's name), else the place's. */
  pageTitle: () => string | null;
  serverId: () => string | null;
  restore: (page: PoppedPage<PageSnapshot>) => void;
}): void {
  if (!webHosted) return;
  const history = new WebHistory<PageSnapshot>();
  const started = signal(false);
  void host.call<{ path?: string; state?: unknown } | null>("web.initialPath").then((initial) => {
    const path = typeof initial?.path === "string" ? initial.path : "/";
    history.start(initial?.state);
    options.restore(poppedPage({ path, state: initial?.state }, untracked(options.serverId)));
    started.set(true);
  }).catch(() => {
    // An older page host, or a test without one: no URLs.
  });
  // Browsers cap history writes (Safari throws past ~100 in 30 s): an entry rewritten with what it
  // already holds is not sent again.
  let lastSent = "";
  effect(() => {
    if (!started()) return;
    const place = options.place();
    const snapshot = options.snapshot();
    const request = history.visit({
      key: JSON.stringify(snapshot),
      path: placePath(place),
      title: documentTitle(place, options.pageTitle()),
      serverId: options.serverId(),
      snapshot,
    });
    // What the entry holds, whichever way it got there (a push, then the same page rewritten).
    const sent = JSON.stringify([request.path, request.title, request.state]);
    if (request.mode === "replace" && sent === lastSent) return;
    lastSent = sent;
    void host.call("web.setLocation", request).catch(() => {});
  });
  host.on<{ path?: string; state?: unknown } | null>("web.popstate", (event) => {
    if (!untracked(started)) return;
    history.popped(event?.state);
    options.restore(poppedPage({ path: event?.path ?? "/", state: event?.state }, untracked(options.serverId)));
  });
}
