import { withoutSessionId } from "../sessions/listRow";
import type { AgentDisplay } from "./screen";

/**
 * Screen's stage: one agent's display at a time, full bleed, with one hard switch (Take over / Give back).
 * While the person holds a browser display from a phone, the agent's window is laid out for the phone
 * (the display viewer's `fit`); this is the model of what the stage shows and when it asks.
 */

/** One agent on the stage's switcher. */
export interface StageAgent {
  id: string;
  name: string;
  /** The person stopped it. */
  stopped: boolean;
  /** Someone holds it: this device (`holding`) or another. */
  held: boolean;
  /** Acting now (it used its display within the last few seconds). */
  acting: boolean;
  /** A browser window is on it: taking it over lays the page out for the phone. */
  browser: boolean;
}

/** An agent's name as the stage shows it: the Worker's, never the daemon's Session id. */
export function stageName(display: Pick<AgentDisplay, "id" | "name">, workerName?: string): string {
  return withoutSessionId(workerName || display.name) || "An agent";
}

/** The switcher's rows, in the computer's order (a stable list: rows never jump under a finger). */
export function stageAgents(displays: readonly AgentDisplay[], names: (id: string) => string | undefined): StageAgent[] {
  return displays.map((display) => ({
    id: display.id,
    name: stageName(display, names(display.id)),
    stopped: !!display.stopped,
    held: display.control === "person",
    acting: display.idle_s === 0,
    browser: (display.browser ?? 0) > 0,
  }));
}

/**
 * Which agent the stage shows: the one asked for (a Work card's Screen, the float's Open) while it has a
 * display, else the one shown now while it still has one, else the first.
 */
export function pickStageAgent(list: readonly { id: string }[], wanted: string | undefined, shown: string | null): string | null {
  if (wanted && list.some((agent) => agent.id === wanted)) return wanted;
  if (shown && list.some((agent) => agent.id === shown)) return shown;
  return list[0]?.id ?? null;
}

/** The viewer's size in CSS points and its pixels per point, as the viewer measured it. */
export interface StageSize {
  width: number;
  height: number;
  dpr: number;
}

/** The smallest view a page is laid out for; smaller (a float, a split) keeps the window as it is. */
export const MIN_FIT = 240;

/**
 * Whether a held display should be laid out (again) for the view: the first time, when the width changes
 * (the phone turned, a window was resized) or the height grows. A height that only shrinks is the
 * keyboard coming up: the page keeps its layout and the view pans to the field instead.
 */
export function shouldFit(last: StageSize | null, next: StageSize): boolean {
  if (next.width < MIN_FIT || next.height < MIN_FIT || !(next.dpr > 0)) return false;
  if (!last) return true;
  if (Math.abs(next.width - last.width) >= 1) return true;
  if (Math.abs(next.dpr - last.dpr) > 0.01) return true;
  return next.height - last.height >= 1;
}

/** The fit message for a view, whole points. */
export function fitMessage(size: StageSize): { type: "fit"; width: number; height: number; dpr: number } {
  return { type: "fit", width: Math.round(size.width), height: Math.round(size.height), dpr: Math.round(size.dpr * 1000) / 1000 };
}

/** How the agent's window is shown: laid out for this phone (with the page's host), or as the agent left it. */
export interface StageReflow {
  on: boolean;
  host: string;
}

export const NO_REFLOW: StageReflow = { on: false, host: "" };

/** The display viewer's reflow message, or null for anything else. */
export function parseReflow(message: { type?: unknown; on?: unknown; host?: unknown }): StageReflow | null {
  if (message.type !== "reflow" || typeof message.on !== "boolean") return null;
  return { on: message.on, host: message.on && typeof message.host === "string" ? message.host.slice(0, 253) : "" };
}

/** The host the domain chip shows: the page's own name, without a leading "www.". */
export function hostLabel(host: string): string {
  return host.replace(/^www\./i, "");
}

/** What the person typed, as the echo above the key bar shows it: each character for a second, then a dot. */
export interface EchoEntry {
  text: string;
  at: number;
}

export const ECHO_SHOW_MS = 1_000;
const ECHO_KEEP = 24;

/** The echo after typing `text` (a backspace takes one character away). */
export function pushEcho(entries: readonly EchoEntry[], edit: { kind: "text"; text: string } | { kind: "backspace"; count: number }, now: number): EchoEntry[] {
  const out = [...entries];
  if (edit.kind === "backspace") {
    for (let i = 0; i < edit.count && out.length; i++) out.pop();
    return out;
  }
  for (const ch of Array.from(edit.text)) out.push({ text: ch, at: now });
  return out.slice(-ECHO_KEEP);
}

/** The echo line: a dot for each character older than a second, the character itself before that. */
export function echoLine(entries: readonly EchoEntry[], now: number): string {
  return entries.map((entry) => (now - entry.at < ECHO_SHOW_MS && entry.text.trim() ? entry.text : entry.text === " " ? " " : "•")).join("");
}

/** When the echo next changes (a character turns into a dot), or null when it never will. */
export function echoNextChange(entries: readonly EchoEntry[], now: number): number | null {
  let next: number | null = null;
  for (const entry of entries) {
    const at = entry.at + ECHO_SHOW_MS;
    if (at > now && (next === null || at < next)) next = at;
  }
  return next;
}
