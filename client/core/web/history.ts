import { placeOfPath, type WebPlace } from "./places";

/**
 * The browser's history for the web app, as the app keeps it: one entry per
 * place the person went to, so Back, Forward and reload return to it. The page
 * host does what `visit` says (`web.setLocation`) and reports Back and Forward
 * (`web.popstate`); this mirror of the entries decides push, replace or a step.
 *
 * An entry's state is the app's own (`WebEntryState`): its position, what it
 * showed (`snapshot`, the visible stack's routes) and the computer it was on,
 * so a step back reopens exactly that page, and never another computer's.
 */

export interface WebEntryState<S = unknown> {
  /** Its position among this tab's entries. */
  index: number;
  /** What identifies the page it showed: the same key is the same entry. */
  key: string;
  /** The computer it was on (null before any). */
  serverId: string | null;
  snapshot: S;
}

/** What the browser does: a new entry, the current one rewritten, or a step to a neighbour. */
export type LocationMode = "push" | "replace" | "back" | "forward";

export interface LocationRequest<S = unknown> {
  path: string;
  title: string;
  mode: LocationMode;
  state: WebEntryState<S>;
}

export function isWebEntryState(value: unknown): value is WebEntryState {
  if (!value || typeof value !== "object") return false;
  const state = value as Record<string, unknown>;
  return Number.isInteger(state.index) && (state.index as number) >= 0 && typeof state.key === "string"
    && (state.serverId === null || typeof state.serverId === "string") && "snapshot" in state;
}

export class WebHistory<S = unknown> {
  /** The keys of the entries this tab knows, by position. */
  private keys = new Map<number, string>();
  private index = 0;
  private started = false;

  /** The page loaded on an entry the app made earlier (a reload keeps its state), or on a new one. */
  start(state: unknown): void {
    if (!isWebEntryState(state)) return;
    this.index = state.index;
    this.keys.set(state.index, state.key);
  }

  /** The app now shows the page `key`: what the browser does about it. */
  visit(input: { key: string; path: string; title: string; serverId: string | null; snapshot: S }): LocationRequest<S> {
    let mode: LocationMode;
    if (!this.started) {
      // The entry the page loaded on becomes this page's.
      this.started = true;
      mode = "replace";
    } else if (this.keys.get(this.index) === input.key) {
      mode = "replace";
    } else if (this.index > 0 && this.keys.get(this.index - 1) === input.key) {
      // The app went back itself (a Back button, Escape): the browser steps back, so Forward still works.
      this.index -= 1;
      mode = "back";
    } else if (this.keys.get(this.index + 1) === input.key) {
      this.index += 1;
      mode = "forward";
    } else {
      // Somewhere new: the entries ahead are gone, as the browser drops them.
      for (const position of [...this.keys.keys()]) if (position > this.index) this.keys.delete(position);
      this.index += 1;
      mode = "push";
    }
    this.keys.set(this.index, input.key);
    return {
      path: input.path,
      title: input.title,
      mode,
      state: { index: this.index, key: input.key, serverId: input.serverId, snapshot: input.snapshot },
    };
  }

  /** The browser stepped to an entry (Back, Forward, its history menu): its state, if the app made it. */
  popped(state: unknown): WebEntryState<S> | null {
    if (!isWebEntryState(state)) return null;
    this.started = true;
    this.index = state.index;
    this.keys.set(state.index, state.key);
    return state as WebEntryState<S>;
  }

  /** Where the app thinks the browser is (tests). */
  position(): number {
    return this.index;
  }
}

/**
 * The routes an entry restores, with each one that is already shown at its
 * place kept as the same value: a stack keeps the page of a route that stays,
 * so going back to a list does not make it again (its scroll, its state).
 */
export function reuseRoutes<R>(current: readonly R[], next: readonly R[]): R[] {
  return next.map((route, index) => {
    const shown = current[index];
    return shown !== undefined && JSON.stringify(shown) === JSON.stringify(route) ? shown : route;
  });
}

/** What to show for an entry the browser stepped to, or the page loaded at. */
export type PoppedPage<S> = { kind: "snapshot"; snapshot: S } | { kind: "place"; place: WebPlace };

/**
 * An entry of this computer reopens what it showed. Otherwise the path decides:
 * an entry of another computer opens the root of its place (a Session id belongs
 * to the computer it was on), and an unknown path opens Brain.
 */
export function poppedPage<S>(input: { path: string; state: unknown }, serverId: string | null): PoppedPage<S> {
  const state = isWebEntryState(input.state) ? input.state : null;
  if (state && state.serverId === serverId) return { kind: "snapshot", snapshot: state.snapshot as S };
  const place = placeOfPath(input.path) ?? { key: "brain" as const };
  if (state && place.key === "session") return { kind: "place", place: { key: "sessions" } };
  // A page under a place (a Work file, a post) was that computer's too.
  if (state && "page" in place && place.page) return { kind: "place", place: place.key === "work" ? { key: "brain" } : { key: place.key } };
  return { kind: "place", place };
}
