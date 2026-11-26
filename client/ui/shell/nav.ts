import { computed, effect, Keyboard, signal, untracked } from "nsh";
import { createNavStack } from "nsh/base";
import type { TerminalRouteSessionHint } from "../../core/session/sessionRoute";
import type { PageNav } from "../more/routes";
import type { Route } from "./routes";
import { beside } from "./layout";
import { opensBeside, shownRoutes } from "../../core/web/beside";

/**
 * The app's one stack (SPEC §3): home at the bottom, every other page pushed on it. Pages take
 * it as their `nav`; links, notifications and the tray open places through the helpers below,
 * which build the natural stack under the page so Back leads somewhere sensible (§3.8).
 */
export const nav = createNavStack<Route>([{ name: "home" }]);

/** The beside pane's routes (core/web/beside.ts): the Work board at its root, then what was opened beside Brain. */
export type BesideRoute = Route | { name: "board" };
const BOARD: BesideRoute = { name: "board" };
export const besideNav = createNavStack<BesideRoute>([BOARD]);

/** The beside pane shows: the split is up and Brain is the page on the stack. */
export function besideShown(): boolean {
  return beside() && nav.routes().length === 1;
}

/** Whether the beside page at `index` is the one shown. */
export function isBesideTop(index: number): boolean {
  return besideShown() && index === besideNav.routes().length - 1;
}

/** The beside pane back to the Work board (a computer switch: what it held was the old computer's). */
export function closeBeside(): void {
  if (untracked(besideNav.routes).length > 1) besideNav.popToRoot();
}

/** Puts `pages` beside Brain, over the board (the split crossed, a URL restored). */
export function setBesidePages(pages: readonly Route[]): void {
  besideNav.setRoutes([BOARD, ...pages]);
}

/** The pages open beside Brain, without the board. */
export function besidePages(): Route[] {
  return besideNav.routes().slice(1) as Route[];
}

/** What shows: Brain and what stands beside it, or the stack (core/web/beside.ts): the URL, the tab and the left pane's selection. */
export const shown = computed(() => shownRoutes(nav.routes(), besidePages(), beside()));

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
  // With the split up, a Session, a Work or a Work file stands beside Brain in place of what stood there.
  if (untracked(beside) && opensBeside(routes[0])) {
    if (untracked(nav.routes).length > 1) nav.popToRoot();
    besideNav.setRoutes([BOARD, ...routes.filter((route) => route.name !== "home")]);
    return;
  }
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
  push({ name: "session", workerId, hint });
}

/** Pushes a page: on the beside pane while it shows (a link inside it, a new Session, a file a chat names), else on the one stack. */
export function push(route: Route): void {
  if (untracked(besideShown) && opensBeside(route)) besideNav.push(route);
  else nav.push(route);
}

/** Opens what a card in Brain's thread names: in place of what stands beside Brain, or pushed over Brain. */
export function openFromBrain(route: Route): void {
  if (untracked(besideShown) && opensBeside(route)) openPage(route);
  else nav.push(route);
}

/** A Session in place of the Session shown (New session from a Session): in the beside pane when that is where it is. */
export function replaceSession(route: Route): void {
  const top = untracked(besideNav.routes).at(-1);
  if (untracked(besideShown) && top?.name === "session") besideNav.replace(route);
  else nav.replace(route);
}

/** Brain's own Session on its terminal (Brain's menu › Open terminal). */
export function openBrainTerminal(workerId: string): void {
  push({ name: "session", workerId, brain: true });
}

export function openWorkDetail(workId: string): void {
  push({ name: "workDetail", workId });
}

/** Calendar, and an item (and one of its runs) on it, as a link opens them. */
export function openCalendarItem(id: string, runId?: string): void {
  nav.setRoutes([...untracked(nav.routes), { name: "calendar" }, { name: "item", id }, ...(runId ? [{ name: "run" as const, itemId: id, runId }] : [])]);
}

/** A Trigger's page, over the Triggers page of the Calendar it lives in. */
export function openTrigger(id: string): void {
  nav.setRoutes([...untracked(nav.routes), { name: "calendar" }, { name: "triggers" }, { name: "trigger", id }]);
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
