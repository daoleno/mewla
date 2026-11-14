import type { TerminalRouteSessionHint } from "../../core/session/sessionRoute";
import type { CalendarRoute } from "../calendar/routes";
import type { MorePage } from "../more/routes";

/**
 * Every page of the one stack (soft-final SPEC §3). Home is Brain's thread and always the first
 * page; everything else is pushed on it, so Back always has somewhere to go and the page under
 * keeps its scroll, its open tray and its draft.
 */
export type Route =
  | { name: "home" }
  /** A Session (with what the list knew, before the Session is on it); `brain`: Brain's own Session, on its terminal. */
  | { name: "session"; workerId: string; hint?: TerminalRouteSessionHint; brain?: boolean }
  /** Every Session of the computer, by folder (the tray's "All Sessions"). */
  | { name: "allSessions" }
  /** The cat: this computer, the other computers and everything rare. */
  | { name: "computer" }
  /** Pair a computer (the computer page's "Pair a computer"). */
  | { name: "pair" }
  | { name: "workDetail"; workId: string }
  | CalendarRoute
  | MorePage;

export type RouteName = Route["name"];

