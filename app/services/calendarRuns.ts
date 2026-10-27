import { DateStyle, formatDate } from "./dateFormat";
import { calendarDateKey, itemInstant } from "./calendarPresentation";
import type { CalendarItem, CalendarRun } from "../store/calendar";

/**
 * What a schedule is and what each of its runs did, in words a person reads:
 * "Every day at 8:30 PM", "Next: tomorrow 8:30 PM", a one-line summary and a
 * line per run. Pure, so the sheet, the agenda and the back-links agree.
 */

const SUMMARY_MAX = 120;

function dayOrdinal(key: string) {
  const [year, month, day] = key.split("-").map(Number);
  return Math.floor(Date.UTC(year, month - 1, day) / 86_400_000);
}

/** The item's wall-clock time, with its zone only when it is not the viewer's. */
function wallTime(instant: string, item: CalendarItem, viewerZone: string) {
  return formatDate(instant, DateStyle.time, {
    timeZone: item.timezone,
    ...(item.timezone === viewerZone ? {} : { timeZoneName: "short" as const }),
  });
}

/** "Every day at 8:30 PM", "Weekdays at 9:00 AM", "Every Monday at 9:00 AM", "Oct 28 at 8:30 PM". */
export function calendarScheduleLine(item: CalendarItem, viewerZone: string): string {
  const instant = itemInstant(item);
  const time = wallTime(instant, item, viewerZone);
  switch (item.recurrence) {
    case "daily":
      return `Every day at ${time}`;
    case "weekdays":
      return `Weekdays at ${time}`;
    case "weekly":
      return `Every ${formatDate(instant, { weekday: "long" }, { timeZone: item.timezone })} at ${time}`;
    default:
      return `${formatDate(instant, DateStyle.shortDate, { timeZone: item.timezone })} at ${time}`;
  }
}

/** "Next: tomorrow 8:30 PM", for a repeating item waiting on its next time. */
export function calendarNextLine(
  item: CalendarItem,
  now: Date,
  viewerZone: string,
): string | null {
  if (item.recurrence === "none" || item.status !== "scheduled") return null;
  const instant = itemInstant(item);
  const days =
    dayOrdinal(calendarDateKey(instant, viewerZone)) -
    dayOrdinal(calendarDateKey(now, viewerZone));
  const time = wallTime(instant, item, viewerZone);
  if (days === 0) return `Next: today ${time}`;
  if (days === 1) return `Next: tomorrow ${time}`;
  return `Next: ${formatDate(instant, DateStyle.day, { timeZone: item.timezone })}, ${time}`;
}

/** The first sentence of a text, at most one line long. */
export function firstSentence(text: string | undefined, max = SUMMARY_MAX): string {
  const line = (text ?? "").replace(/\s+/g, " ").trim();
  if (!line) return "";
  const end = line.search(/[。！？]|[.!?](?=\s|$)/);
  const sentence = end >= 0 ? line.slice(0, end + 1) : line;
  return sentence.length > max ? `${sentence.slice(0, max - 1).trimEnd()}…` : sentence;
}

/**
 * What an item does, in one line: Brain's summary, else the first sentence
 * of its instruction (items from before summaries), never the whole prompt.
 */
export function calendarItemSummary(item: CalendarItem): string {
  return (
    firstSentence(item.summary) ||
    (item.kind === "scheduled_action" ? firstSentence(item.action_instruction) : "")
  );
}

export function calendarRunsNewestFirst(item: Pick<CalendarItem, "runs">): CalendarRun[] {
  return [...(item.runs ?? [])].sort(
    (left, right) => Date.parse(right.started_at) - Date.parse(left.started_at),
  );
}

/** The run still at work, if any. */
export function calendarActiveRun(item: Pick<CalendarItem, "runs">): CalendarRun | undefined {
  return calendarRunsNewestFirst(item).find((run) => run.status === "running");
}

/** One line of what a run produced, or why it failed. */
export function calendarRunPreview(run: CalendarRun): string {
  if (run.status === "failed") return firstSentence(run.failure_reason, 160);
  if (run.result_preview) return run.result_preview;
  const line = (run.result ?? "")
    .split("\n")
    .map((value) => value.replace(/^[#>\s]+/, "").replace(/^[-*+]\s+/, "").trim())
    .find((value) => value && !value.startsWith("![") && !/^[-*_=|:\s]+$/.test(value));
  return firstSentence(line, 160);
}

/** "Tue, Oct 27", when the run was due, in the item's own zone. */
export function calendarRunDate(run: CalendarRun, item: Pick<CalendarItem, "timezone">): string {
  return formatDate(run.scheduled_for || run.started_at, DateStyle.day, { timeZone: item.timezone });
}

/** The Calendar item and run a Work or Session came from. */
export type CalendarOrigin = { itemId: string; runId?: string };

/** A Work file the daemon wrote for a Calendar run names its item and run. */
export function calendarOriginOfWork(work: {
  frontmatter?: { kind?: string; extra?: Record<string, unknown> };
}): CalendarOrigin | null {
  if (work.frontmatter?.kind !== "calendar_action") return null;
  const extra = work.frontmatter.extra ?? {};
  const itemId = typeof extra.calendar_item_id === "string" ? extra.calendar_item_id.trim() : "";
  const runId = typeof extra.calendar_run_id === "string" ? extra.calendar_run_id.trim() : "";
  return itemId ? { itemId, runId: runId || undefined } : null;
}

/**
 * "运营：挖稿 · Oct 27": the schedule's name and the run's date. The title
 * falls back to the Work's own (the run keeps the title it ran under) and the
 * date to when the Work started.
 */
export function calendarOriginLabel(
  origin: CalendarOrigin,
  items: readonly CalendarItem[],
  fallback: { title?: string; at?: string } = {},
): { title: string; date: string; label: string } | null {
  const item = items.find((candidate) => candidate.id === origin.itemId);
  const run = item?.runs?.find((candidate) => candidate.id === origin.runId);
  const title = (item?.title || fallback.title || "").trim();
  if (!title) return null;
  const at = run?.scheduled_for ?? fallback.at;
  const date = at
    ? formatDate(at, DateStyle.shortDate, item ? { timeZone: item.timezone } : undefined)
    : "";
  return { title, date, label: date ? `${title} · ${date}` : title };
}

/** A Calendar run's Session starts right after its Work is written. */
const SESSION_START_WINDOW_MS = 10 * 60 * 1000;

/**
 * Whether a Session is the one a Calendar run's Work started. Pane ids
 * restart when tmux does, so a later, unrelated Session can carry a finished
 * run's id; it started well after that Work was written.
 */
export function calendarWorkOwnsSession(
  work: { frontmatter?: { created?: string } },
  sessionStartedAt: number | undefined,
): boolean {
  const created = Date.parse(work.frontmatter?.created ?? "");
  if (!Number.isFinite(created) || !sessionStartedAt) return true;
  return sessionStartedAt <= created + SESSION_START_WINDOW_MS;
}
