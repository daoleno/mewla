import type { TerminalRouteSessionHint } from "../../core/session/sessionRoute";
import type { MorePage } from "../more/routes";

export type MoreRoute = { name: "more" } | { name: "pair" } | MorePage;

/** The Sessions tab's stack: the list, and a Session opened from it (with what the list knew, before the Session is on it). */
/** The Sessions tab: the list, a Session, and the Work file a Session runs ("Open Brain"). */
export type SessionsRoute = { name: "sessions" } | { name: "session"; workerId: string; hint?: TerminalRouteSessionHint } | { name: "workFile"; id: string };
