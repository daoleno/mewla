/**
 * The wide layout's left pane (soft-final SPEC §14.1): its header's state line, the tray's title
 * with its counts, and which row's page is in the right pane. Pure, from the tray model and the
 * stack, so the pane, the phone's tray and the tests agree.
 */
import type { StateDot, StateKind, TrayModel, TrayRequest, TrayRow, TrayTarget } from "./model";

/** The line under the computer's name: the connection, or Brain's own trouble. */
export interface PaneStateLine {
  words: string;
  /** The dot before the words: filled ink, a soft ring, red, or none. */
  dot: StateDot;
  /** Ink and 600 (Pair again), else soft. */
  ink: boolean;
}

/**
 * "● Online", "○ Connecting…", "○ Offline · Relay unreachable", "● Pair again", "Brain is
 * starting", "Brain can't start". What waits on you is the tray's, not the connection's: needs and
 * running read Online here.
 */
export function paneStateLine(input: { kind: StateKind; diagnosis?: string }): PaneStateLine {
  switch (input.kind) {
    case "none": return { words: "", dot: null, ink: false };
    case "pair": return { words: "Pair again", dot: "seal", ink: true };
    case "connecting": return { words: "Connecting…", dot: "ring", ink: false };
    case "offline": return { words: input.diagnosis ? `Offline · ${input.diagnosis}` : "Offline", dot: "ring", ink: false };
    case "starting": return { words: "Brain is starting", dot: null, ink: false };
    case "cant_start": return { words: "Brain can't start", dot: "seal", ink: false };
    default: return { words: "Online", dot: "ink", ink: false };
  }
}

/** The tray's title in the pane: what runs and what waits on you together ("12 running, 3 need you"). */
export function paneTrayTitle(model: Pick<TrayModel, "needs" | "running">): string {
  const running = model.running.filter((row) => !row.quiet).length;
  const needs = model.needs.length;
  const parts = [
    ...(running ? [`${running} running`] : []),
    ...(needs ? [needs === 1 ? "1 needs you" : `${needs} need you`] : []),
  ];
  return parts.length ? parts.join(", ") : "All quiet";
}

/** A row's page, as the stack names it: `session:<id>`, `work:<id>`, `item:<id>`. */
export function targetKey(target: TrayTarget): string {
  switch (target.open) {
    case "session": return `session:${target.workerId}`;
    case "work": return `work:${target.workId}`;
    case "item": return `item:${target.itemId}`;
  }
}

/** The key of a tray row, matched against `paneSelection`. */
export function rowKey(row: Pick<TrayRow, "target">): string {
  return targetKey(row.target);
}

/**
 * The page a Needs you row opens in the right pane, as paneSelection names it, so the row is selected while that
 * page shows; null for a request answered in Brain's thread (its card) or a new device (a dialog).
 */
export function requestPaneKey(request: Pick<TrayRequest, "where" | "workerId" | "workId" | "itemId">): string | null {
  if (request.where === "session" && request.workerId) return `session:${request.workerId}`;
  if (request.where === "work" && request.workId) return `work:${request.workId}`;
  if (request.where !== "brain" && request.itemId) return `item:${request.itemId}`;
  return null;
}

/**
 * Which left-pane row owns the right pane's page: the page right above Brain (a row replaces it;
 * what that page pushed keeps its row selected). `brain` with nothing above Brain; `computer`
 * for the cat's page; `calendar` and `all` for the tray's links; null for a page no row opens.
 */
export function paneSelection(routes: readonly { name: string; [key: string]: unknown }[]): string | null {
  const page = routes[1];
  if (!page) return "brain";
  switch (page.name) {
    case "session": return `session:${String(page.workerId)}`;
    case "workDetail": return `work:${String(page.workId)}`;
    case "item": return `item:${String(page.id)}`;
    case "calendar": return "calendar";
    case "allSessions": return "all";
    case "computer": return "computer";
    default: return null;
  }
}

/** The Brain row's line: the last thing Brain said, on one line, without its markdown marks. */
export function brainRowLine(events: readonly { kind: string; seq: number; body?: string }[] | null | undefined): string {
  let last: { seq: number; body: string } | null = null;
  for (const event of events ?? []) {
    if (event.kind !== "assistant_message" || !event.body?.trim()) continue;
    if (!last || event.seq > last.seq) last = { seq: event.seq, body: event.body };
  }
  if (!last) return "";
  return last.body
    .split("\n")
    .map((line) => line.replace(/^\s*(#{1,6}\s+|[-*+>]\s+|\d+\.\s+)/, "").trim())
    .filter(Boolean)
    .join(" ")
    .replace(/[*_`~]+/g, "")
    .replace(/\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/\s+/g, " ")
    .trim();
}
