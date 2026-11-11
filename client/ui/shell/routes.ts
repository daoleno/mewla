import type { TerminalRouteSessionHint } from "../../core/session/sessionRoute";
import type { FilesPage } from "../more/files/routes";
import type { GitPage } from "../more/git/routes";
import type { MorePage } from "../more/routes";

export type MoreRoute = { name: "more" } | { name: "pair" } | MorePage;

/**
 * The Sessions tab: the list, a Session (with what the list knew, before the Session is on it),
 * the Work file a Session runs ("Open Brain"), its Git review and the files it shows.
 */
export type SessionsRoute =
  | { name: "sessions" }
  | { name: "session"; workerId: string; hint?: TerminalRouteSessionHint }
  | { name: "workFile"; id: string }
  | GitPage
  | FilesPage;
