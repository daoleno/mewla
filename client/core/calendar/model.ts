import type { DateFormat } from "./format";
import type { Zones } from "./zones";

/** A Calendar item as the daemon sends it (daemon/calendar/model.go). Times are RFC 3339 instants. */
export type CalendarKind = "event" | "reminder" | "deadline" | "scheduled_action";
export type CalendarStatus = "scheduled" | "waiting" | "running" | "completed" | "failed" | "cancelled";
export type CalendarRecurrence = "none" | "daily" | "weekly" | "weekdays";

export interface CalendarRun {
  id: string;
  scheduled_for: string;
  started_at: string;
  finished_at?: string;
  status: CalendarStatus;
  manual?: boolean;
  work_id?: string;
  worker_session?: string;
  /** Only get_calendar_item carries the full result. */
  result?: string;
  /** Lists and broadcasts carry this one line instead. */
  result_preview?: string;
  failure_reason?: string;
}

export interface CalendarItem {
  id: string;
  title: string;
  kind: CalendarKind;
  status: CalendarStatus;
  start_at?: string;
  end_at?: string;
  notify_at?: string;
  due_at?: string;
  /** IANA zone the item's wall-clock times (and its repeats) follow. */
  timezone: string;
  recurrence: CalendarRecurrence;
  notes?: string;
  /** One line saying what a scheduled action does. */
  summary?: string;
  action_instruction?: string;
  action_cwd?: string;
  source_thread_id?: string;
  source_message_id?: string;
  linked_work_id?: string;
  /** Set by the daemon when Mewla acts on the item itself (Watch's morning brief). */
  owner?: string;
  alert_lead_minutes?: number;
  snoozed_until?: string;
  /** The alert the daemon last fired, and the last one answered: an alert is open while the first is later. */
  alerted_at?: string;
  acked_at?: string;
  failure_reason?: string;
  next_at: string;
  created_at: string;
  updated_at: string;
  cancelled_at?: string;
  revision: number;
  /** Lists carry the newest ten runs, oldest first; get_calendar_item has them all. */
  runs?: CalendarRun[];
}

/** What every time-dependent line needs: the zone table, the formatter and the time now. */
export interface CalendarClock {
  zones: Zones;
  format: DateFormat;
  now: number;
}

export const kindLabel: Record<CalendarKind, string> = {
  event: "Event",
  reminder: "Reminder",
  deadline: "Deadline",
  scheduled_action: "Mewla action",
};

export const statusLabel: Record<CalendarStatus, string> = {
  scheduled: "Scheduled",
  waiting: "Needs you",
  running: "Running",
  completed: "Done",
  failed: "Failed",
  cancelled: "Cancelled",
};

/** The item's instant on the agenda: when it next comes due, else its own time. */
export function itemInstant(item: CalendarItem): number {
  return Date.parse(item.next_at || item.start_at || item.notify_at || item.due_at || "");
}

/** The zones a set of items needs from the host: theirs and the phone's. */
export function zonesOf(items: readonly CalendarItem[], device: string): string[] {
  return [...new Set([device, ...items.map((item) => item.timezone).filter(Boolean)])].sort();
}

function dayOrdinal(key: string): number {
  const [year, month, day] = key.split("-").map(Number);
  return Math.floor(Date.UTC(year, month - 1, day) / 86_400_000);
}

export type AgendaSection = "Today" | "Tomorrow" | "Later This Week" | "Later";

/** Which part of the agenda an item is in, by its day in the phone's zone. Past, overdue and done items stay in Today. */
export function agendaSection(item: CalendarItem, clock: CalendarClock): AgendaSection {
  const zone = clock.zones.device;
  const today = clock.zones.dateKey(clock.now, zone);
  const days = dayOrdinal(clock.zones.dateKey(itemInstant(item), zone)) - dayOrdinal(today);
  if (days <= 0) return "Today";
  if (days === 1) return "Tomorrow";
  const weekday = new Date(dayOrdinal(today) * 86_400_000).getUTCDay();
  if (days <= 7 - weekday) return "Later This Week";
  return "Later";
}

/** The agenda: cancelled items hidden, the rest by section, each in time order. */
export function groupAgenda(items: readonly CalendarItem[], clock: CalendarClock): { title: AgendaSection; items: CalendarItem[] }[] {
  const shown = items.filter((item) => item.status !== "cancelled").sort((a, b) => itemInstant(a) - itemInstant(b));
  return (["Today", "Tomorrow", "Later This Week", "Later"] as const)
    .map((title) => ({ title, items: shown.filter((item) => agendaSection(item, clock) === title) }))
    .filter((section) => section.items.length > 0);
}

/** The items of one day (a date picked on the month), in time order. */
export function itemsOnDay(items: readonly CalendarItem[], dateKey: string, clock: CalendarClock): CalendarItem[] {
  return items
    .filter((item) => item.status !== "cancelled" && clock.zones.dateKey(itemInstant(item), clock.zones.device) === dateKey)
    .sort((a, b) => itemInstant(a) - itemInstant(b));
}

/** The item's wall-clock time, with its zone named only when it is not the phone's. */
function wallTime(at: number, item: CalendarItem, clock: CalendarClock): string {
  return clock.format.time(at, item.timezone, item.timezone !== clock.zones.device);
}

/** The time on an agenda row: in the item's own zone. */
export function rowTime(item: CalendarItem, clock: CalendarClock): string {
  return wallTime(itemInstant(item), item, clock);
}

/** "Every day at 8:30 PM", "Weekdays at 9:00 AM", "Every Monday at 9:00 AM", "Oct 28 at 8:30 PM". */
export function scheduleLine(item: CalendarItem, clock: CalendarClock): string {
  const at = itemInstant(item);
  const time = wallTime(at, item, clock);
  switch (item.recurrence) {
    case "daily":
      return `Every day at ${time}`;
    case "weekdays":
      return `Weekdays at ${time}`;
    case "weekly":
      return `Every ${clock.format.weekday(at, item.timezone)} at ${time}`;
    default:
      return `${clock.format.shortDate(at, item.timezone)} at ${time}`;
  }
}

/** "Next: tomorrow 8:30 PM", for a repeating item waiting on its next time. */
export function nextLine(item: CalendarItem, clock: CalendarClock): string | null {
  if (!item.recurrence || item.recurrence === "none" || item.status !== "scheduled") return null;
  const at = itemInstant(item);
  const zone = clock.zones.device;
  const days = dayOrdinal(clock.zones.dateKey(at, zone)) - dayOrdinal(clock.zones.dateKey(clock.now, zone));
  const time = wallTime(at, item, clock);
  if (days === 0) return `Next: today ${time}`;
  if (days === 1) return `Next: tomorrow ${time}`;
  return `Next: ${clock.format.day(at, item.timezone)}, ${time}`;
}

/** The first sentence of a text, at most one line long. */
export function firstSentence(text: string | undefined, max = 120): string {
  const line = (text ?? "").replace(/\s+/g, " ").trim();
  if (!line) return "";
  const end = line.search(/[。！？]|[.!?](?=\s|$)/);
  const sentence = end >= 0 ? line.slice(0, end + 1) : line;
  return sentence.length > max ? `${sentence.slice(0, max - 1).trimEnd()}…` : sentence;
}

/** What an item does, in one line: Brain's summary, else the first sentence of its instruction; never the whole prompt. */
export function itemSummary(item: CalendarItem): string {
  return firstSentence(item.summary) || (item.kind === "scheduled_action" ? firstSentence(item.action_instruction) : "");
}

export function runsNewestFirst(item: Pick<CalendarItem, "runs">): CalendarRun[] {
  return [...(item.runs ?? [])].sort((a, b) => Date.parse(b.started_at) - Date.parse(a.started_at));
}

export function activeRun(item: Pick<CalendarItem, "runs">): CalendarRun | undefined {
  return runsNewestFirst(item).find((run) => run.status === "running");
}

/** One line of what a run produced, or why it failed. */
export function runPreview(run: CalendarRun): string {
  if (run.status === "failed") return firstSentence(run.failure_reason, 160);
  if (run.result_preview) return run.result_preview;
  const line = (run.result ?? "")
    .split("\n")
    .map((value) => value.replace(/^[#>\s]+/, "").replace(/^[-*+]\s+/, "").trim())
    .find((value) => value && !value.startsWith("![") && !/^[-*_=|:\s]+$/.test(value));
  return firstSentence(line, 160);
}

/** "Tue, Oct 27, 8:30 PM": when a run was due, in the item's zone. */
export function runWhen(run: CalendarRun, item: CalendarItem, clock: CalendarClock): string {
  const at = Date.parse(run.scheduled_for || run.started_at);
  return `${clock.format.day(at, item.timezone)}, ${clock.format.time(at, item.timezone)}`;
}

/** A run's result as the sheet shows it: the full one merged in by run id when it was fetched. */
export function mergeRunResults(listed: CalendarItem, full: CalendarItem | null): CalendarItem {
  if (!full || full.id !== listed.id) return listed;
  const results = new Map((full.runs ?? []).map((run) => [run.id, run.result]));
  return { ...listed, runs: (listed.runs ?? []).map((run) => (results.get(run.id) !== undefined ? { ...run, result: results.get(run.id) } : run)) };
}

/** The Calendar item and run a Work file came from (`kind: calendar_action`). */
export function calendarOriginOfWork(work: { frontmatter?: { kind?: string; extra?: Record<string, unknown> } }): { itemId: string; runId?: string } | null {
  if (work.frontmatter?.kind !== "calendar_action") return null;
  const extra = work.frontmatter.extra ?? {};
  const itemId = typeof extra.calendar_item_id === "string" ? extra.calendar_item_id.trim() : "";
  const runId = typeof extra.calendar_run_id === "string" ? extra.calendar_run_id.trim() : "";
  return itemId ? { itemId, runId: runId || undefined } : null;
}

/** "运营：挖稿 · Oct 27": the schedule's name and the run's date, for a back-link to it. */
export function originLabel(origin: { itemId: string; runId?: string }, items: readonly CalendarItem[], clock: CalendarClock, fallback: { title?: string; at?: string } = {}): string | null {
  const item = items.find((candidate) => candidate.id === origin.itemId);
  const run = item?.runs?.find((candidate) => candidate.id === origin.runId);
  const title = (item?.title || fallback.title || "").trim();
  if (!title) return null;
  const at = run?.scheduled_for ?? fallback.at;
  const date = at ? clock.format.shortDate(Date.parse(at), item?.timezone ?? clock.zones.device) : "";
  return date ? `${title} · ${date}` : title;
}
