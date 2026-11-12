import { effect, Keyboard, signal, untracked } from "nsh";
import { createNavStack } from "nsh/base";
import type { TerminalRouteSessionHint } from "../../core/session/sessionRoute";
import type { PageNav } from "../more/routes";
import type { Route } from "./routes";

/**
 * The app's one stack (SPEC §3): home at the bottom, every other page pushed on it. Pages take
 * it as their `nav`; links, notifications and the tray open places through the helpers below,
 * which build the natural stack under the page so Back leads somewhere sensible (§3.8).
 */
export const nav = createNavStack<Route>([{ name: "home" }]);

/** Home's tray (SPEC §3.2): home's own overlay. It stays open while a page pushed from it is on the stack. */
export const trayOpen = signal(false);

/** Whether the page at `index` is the one shown (the top of the stack). */
export function isTop(index: number): boolean {
  return index === nav.routes().length - 1;
}

/** Home is shown: nothing is pushed on it. */
export function homeShown(): boolean {
  return nav.routes().length === 1;
}

/** Replaces everything above home with `routes`. */
export function openPage(...routes: Route[]): void {
  // Home stays the page it is (its draft, thread scroll and tray, SPEC §3.8): only what is above it changes.
  nav.setRoutes([untracked(nav.routes)[0] ?? { name: "home" }, ...routes.filter((route) => route.name !== "home")]);
}

/** Back to home, the tray closed (a computer switch, "Ask Brain about this", a link to Brain). */
export function goHome(options: { tray?: boolean } = {}): void {
  trayOpen.set(Boolean(options.tray));
  nav.popToRoot();
}

/** Pushes a Session of the current computer on whatever is shown. */
export function openSession(workerId: string, hint?: TerminalRouteSessionHint): void {
  nav.push({ name: "session", workerId, hint });
}

/** Brain's own Session on its terminal (Brain's menu › Open terminal). */
export function openBrainTerminal(workerId: string): void {
  nav.push({ name: "session", workerId, brain: true });
}

export function openWorkDetail(workId: string): void {
  nav.push({ name: "workDetail", workId });
}

/** Calendar, and an item (and one of its runs) on it, as a link opens them. */
export function openCalendarItem(id: string, runId?: string): void {
  nav.setRoutes([...untracked(nav.routes), { name: "calendar" }, { name: "item", id }, ...(runId ? [{ name: "run" as const, itemId: id, runId }] : [])]);
}

/** How the computer page's children push their next page and leave. */
export const pageNav: PageNav = {
  push: (route) => nav.push(route),
  pop: () => nav.pop(),
  routes: () => nav.routes(),
};

// Leaving a page (Back, a pop) takes its field's keyboard down with it; a page pushed on top
// may focus its own field, so only its page's composer lets go then (Composer's `active`).
let depth = untracked(() => nav.routes().length);
effect(() => {
  const now = nav.routes().length;
  if (now < depth) Keyboard.dismiss();
  depth = now;
});
