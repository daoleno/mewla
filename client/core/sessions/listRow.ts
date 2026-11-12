import { DateStyle, formatDate } from "../timeline/dates";
import { sessionActivity } from "./activity";
import { buildWorkerSessionAccessibilityLabel, workerStatusLabel } from "./workerStatusPresentation";
import { presentWorker, type AgentKind, type TerminalFlavor } from "./workerPresentation";
import type { Worker, WorkerStatus } from "./workers";

/** From the Expo app's constants/telegramPresentation.ts: "8:05 PM" today, "Yesterday", "Tue" this week, else "Oct 27". */
export function listTime(timestamp?: number, now = new Date()): string {
  if (!timestamp) return "";
  const date = new Date(timestamp);
  if (!Number.isFinite(date.getTime())) return "";
  if (date.toDateString() === now.toDateString()) return formatDate(date, DateStyle.time);
  const yesterday = new Date(now);
  yesterday.setDate(yesterday.getDate() - 1);
  if (date.toDateString() === yesterday.toDateString()) return "Yesterday";
  if (now.getTime() - date.getTime() < 7 * 86_400_000) return formatDate(date, DateStyle.weekday);
  return formatDate(date, DateStyle.shortDate, date.getFullYear() === now.getFullYear() ? undefined : { year: "numeric" });
}

/** "claude (1234)" → "claude". */
export function shortWorkerLabel(value?: string): string {
  const trimmed = value?.trim() || "";
  return trimmed.replace(/\s+\([^)]+\)\s*$/, "") || trimmed;
}

export type SessionRowModel = {
  title: string;
  kind: AgentKind;
  flavor: TerminalFlavor;
  /** What it is doing, in words (core/sessions/activity `sessionActivity`). */
  line: string;
  status: WorkerStatus;
  needsYou: boolean;
  delegated: boolean;
  fromCalendar: boolean;
  /** What a screen reader says for the row (the Expo app's WorkerSessionRow label): title, origin, status, line, time. */
  accessibilityLabel: string;
};

/**
 * The daemon names an unnamed Session after its id ("mh-…", or "mh-… (target)"):
 * that is no name to show.
 */
function hasGeneratedName(worker: Worker): boolean {
  const name = shortWorkerLabel(worker.name);
  return !name || name === worker.id || /^mh-[0-9a-f]{24}$/.test(name);
}

/**
 * A row of the Sessions list (WorkerListRowContainer): the person's nickname,
 * else the Session's own name, else the Calendar or Work it runs for, else a
 * short title from its command and directory.
 */
export function sessionRowModel(worker: Worker, options: { alias?: string; calendarLabel?: string; workTitle?: string } = {}): SessionRowModel {
  const generated = hasGeneratedName(worker);
  const presented = presentWorker(generated ? { ...worker, name: "" } : worker, options.alias);
  const workTitle = options.calendarLabel?.trim() || options.workTitle?.trim();
  const title = presented.titleSource !== "default"
    ? presented.title
    : workTitle || presented.shortTitle || (generated ? "" : shortWorkerLabel(worker.name)) || presented.title;
  const line = sessionActivity(worker);
  return {
    title,
    kind: presented.kind,
    flavor: presented.terminalFlavor,
    line,
    status: worker.status,
    needsYou: worker.needs_attention === true,
    delegated: Boolean(worker.delegated),
    fromCalendar: Boolean(options.calendarLabel),
    accessibilityLabel: buildWorkerSessionAccessibilityLabel({
      title,
      status: worker.status,
      // A line that is only the state's word ("Done", "Idle") is said once.
      preview: line === workerStatusLabel(worker.status) ? "" : line,
      timeLabel: worker.status === "running" ? "" : listTime(worker.updated_at),
      brainDelegated: Boolean(worker.delegated),
      fromCalendar: Boolean(options.calendarLabel),
    }),
  };
}
