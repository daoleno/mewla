import { lifecycle as nshLifecycle, signal } from "nsh";

export type AppLifecycle = "active" | "inactive" | "background";

/**
 * Whether the app is in front, as NSH's `lifecycle` says. NSH 0.4.0's browser runtime
 * does not export `lifecycle` (every call threw and the web app never started), so in a
 * browser it follows the page as v0.3.1's web app did: shown is active, hidden is in the
 * background. Import it from here, never from "nsh".
 */
export const lifecycle: () => AppLifecycle =
  typeof nshLifecycle === "function" ? nshLifecycle : pageLifecycle();

interface Page {
  document?: { visibilityState?: string; addEventListener(type: string, listener: () => void): void };
  addEventListener?(type: string, listener: () => void): void;
}

function pageLifecycle(): () => AppLifecycle {
  const page = globalThis as unknown as Page;
  const read = (): AppLifecycle => (page.document?.visibilityState === "hidden" ? "background" : "active");
  const state = signal<AppLifecycle>(read());
  const update = () => state.set(read());
  page.document?.addEventListener("visibilitychange", update);
  page.addEventListener?.("pageshow", update);
  page.addEventListener?.("pagehide", () => state.set("background"));
  return () => state();
}
