import { DateStyle, formatDate } from "../timeline/dates";
import { formatWorkerSessionPreview, type SessionPreviewTone } from "./sessionPreview";
import { isWorkerActivelyRunning } from "./workerStatusPresentation";
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
  preview: string;
  previewPrefix?: string;
  tone: SessionPreviewTone;
  /** "live" while a turn runs, else when it last changed. */
  time: string;
  status: WorkerStatus;
  needsYou: boolean;
  delegated: boolean;
  fromCalendar: boolean;
};

/**
 * A row of the Sessions list (WorkerListRowContainer): the person's nickname,
 * else the Session's own name, else the Calendar or Work it runs for, else a
 * short title from its command and directory.
 */
export function sessionRowModel(worker: Worker, options: { alias?: string; calendarLabel?: string; workTitle?: string } = {}): SessionRowModel {
  // The daemon names an unnamed Session after its id ("mh-…"): that is no name to show.
  const presented = presentWorker(worker.name === worker.id ? { ...worker, name: "" } : worker, options.alias);
  const workTitle = options.calendarLabel?.trim() || options.workTitle?.trim();
  const title = presented.titleSource !== "default"
    ? presented.title
    : workTitle || presented.shortTitle || (worker.name === worker.id ? "" : shortWorkerLabel(worker.name)) || presented.title;
  const preview = formatWorkerSessionPreview(worker);
  return {
    title,
    kind: presented.kind,
    flavor: presented.terminalFlavor,
    preview: preview.text,
    previewPrefix: preview.prefix,
    tone: preview.tone,
    time: isWorkerActivelyRunning(worker.status) ? "live" : listTime(worker.updated_at),
    status: worker.status,
    needsYou: worker.needs_attention === true,
    delegated: Boolean(worker.delegated),
    fromCalendar: Boolean(options.calendarLabel),
  };
}
