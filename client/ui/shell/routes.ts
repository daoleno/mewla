import type { TerminalRouteSessionHint } from "../../core/session/sessionRoute";
import type { MorePage } from "../more/routes";

export type MoreRoute = { name: "more" } | { name: "pair" } | MorePage;

/** The Sessions tab's stack: the list, and a Session opened from it (with what the list knew, before the Session is on it). */
export type SessionsRoute = { name: "sessions" } | { name: "session"; workerId: string; hint?: TerminalRouteSessionHint };
