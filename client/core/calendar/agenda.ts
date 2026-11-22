import { firstSentence, itemInstant, plainLine, runPreview, type CalendarClock, type CalendarItem, type CalendarRun } from "./model";

/**
 * The agenda as every client shows it (w-cal, owner 2026-11-22): what a day holds, each repeat at its time on that day
 * with the run it made, what is still to come, and what earlier days left. A repeat's time on a day is shown only from
 * its next time on (`next_at`) or where a run shows it ran: a repeat is never drawn into the past as if it ran.
 */

/** How a row stands: still to come, running, how its run went, or a time that passed with nothing to run. */
export type RowState = "coming" | "running" | "done" | "failed" | "needs_you" | "past";

export interface AgendaRow {
  /** Unique within a day: the item, and its run or its time. */
  key: string;
  item: CalendarItem;
  at: number;
  run?: CalendarRun;
  state: RowState;
  /** From an earlier day, still open (running or waiting on the person): its date leads the line. */
  carried?: boolean;
}

const DAY = 86_400_000;
/** A run belongs to the time it was scheduled for; a minute either way is the same time. */
const SAME_TIME_MS = 60_000;

function ordinal(key: string): number {
  const [year, month, day] = key.split("-").map(Number);
  return Math.floor(Date.UTC(year, month - 1, day) / DAY);
}

function keyOf(ordinalDay: number): string {
  return new Date(ordinalDay * DAY).toISOString().slice(0, 10);
}

/** The day key `offset` days from today, in the device's zone. */
export function dayKey(clock: CalendarClock, offset = 0): string {
  return keyOf(ordinal(clock.zones.dateKey(clock.now, clock.zones.device)) + offset);
}

export function repeats(item: CalendarItem): boolean {
  return Boolean(item.recurrence) && item.recurrence !== "none";
}

/** The repeat's times that fall on a device day, from its next time on (never before it). */
function slotsOn(item: CalendarItem, key: string, clock: CalendarClock): number[] {
  const next = itemInstant(item);
  if (!Number.isFinite(next) || !clock.zones.knows(item.timezone)) return [];
  const { time } = clock.zones.localFields(next, item.timezone);
  const weekday = clock.zones.wallClock(next, item.timezone).weekday;
  const slots: number[] = [];
  // The item's zone may be a day off the device's: its dates around the day, kept where they land on it.
  for (const offset of [-1, 0, 1]) {
    const date = keyOf(ordinal(key) + offset);
    const resolved = clock.zones.resolveLocal(date, time, item.timezone);
    const at = resolved.status === "resolved" ? resolved.instant : resolved.status === "ambiguous" ? resolved.instants[0] : NaN;
    if (!Number.isFinite(at) || at < next - SAME_TIME_MS / 2) continue;
    if (clock.zones.dateKey(at, clock.zones.device) !== key) continue;
    const day = clock.zones.wallClock(at, item.timezone).weekday;
    if (item.recurrence === "weekdays" && (day === 0 || day === 6)) continue;
    if (item.recurrence === "weekly" && day !== weekday) continue;
    slots.push(at);
  }
  return slots;
}

/** How a run went: what its agent said (`outcome`), else its status. */
export function runState(run: CalendarRun): RowState {
  if (run.status === "running") return "running";
  if (run.status === "failed" || run.outcome === "failed") return "failed";
  if (run.outcome === "needs_you") return "needs_you";
  return "done";
}

/** How a run went, in a word, for its row on an item's page and its own page. */
export function runWord(run: CalendarRun): string {
  switch (runState(run)) {
    case "running": return "Running";
    case "failed": return "Failed";
    case "needs_you": return "Needs you";
    default: return "Done";
  }
}

/** A one-off item with no run of its own (a reminder, an event): its status, or past once its time has gone. */
function itemState(item: CalendarItem, at: number, now: number): RowState {
  switch (item.status) {
    case "running": return "running";
    case "completed": return "done";
    case "failed": return "failed";
    case "waiting": return "needs_you";
  }
  return at <= now ? "past" : "coming";
}

function runAt(run: CalendarRun): number {
  return Date.parse(run.scheduled_for || run.started_at);
}

/**
 * A day's rows in time order. Repeats show at each time on the day from their next time on, and wherever a run of
 * theirs was scheduled that day; a reminder or event that repeats (it makes no runs) also shows today at a time
 * already gone, dimmed, when it existed then. One-off items show at their time with how they went. Cancelled never.
 * `known` keeps a past day to what is known: one-off items and the runs the list carries, nothing projected.
 */
export function dayRows(items: readonly CalendarItem[], key: string, clock: CalendarClock): AgendaRow[] {
  const zone = clock.zones.device;
  const today = dayKey(clock);
  const past = key < today;
  const rows: AgendaRow[] = [];
  for (const item of items) {
    if (item.status === "cancelled") continue;
    const runs = (item.runs ?? []).filter((run) => clock.zones.dateKey(runAt(run), zone) === key);
    if (!repeats(item)) {
      const at = itemInstant(item);
      if (clock.zones.dateKey(at, zone) !== key) {
        // A run now of a one-off on another day than its own (Run now) shows on the day it ran.
        for (const run of runs) rows.push({ key: `${item.id}:${run.id}`, item, at: runAt(run), run, state: runState(run) });
        continue;
      }
      const run = runs.at(-1) ?? (item.runs ?? []).at(-1);
      rows.push({ key: item.id, item, at, run, state: run ? runState(run) : itemState(item, at, clock.now) });
      continue;
    }
    const used = new Set<string>();
    const slots = past ? [] : slotsOn(item, key, clock);
    // Today, a reminder's or an event's time already gone (it makes no runs, so none says it happened).
    if (key === today && item.kind !== "scheduled_action") {
      const created = Date.parse(item.created_at);
      for (const at of slotsBefore(item, key, clock)) if (!(created > at)) slots.push(at);
    }
    for (const at of slots.sort((a, b) => a - b)) {
      const run = runs.find((candidate) => !used.has(candidate.id) && Math.abs(runAt(candidate) - at) <= SAME_TIME_MS);
      if (run) used.add(run.id);
      const state = run ? runState(run) : at <= clock.now ? (item.kind === "scheduled_action" ? "coming" : "past") : "coming";
      rows.push({ key: run ? `${item.id}:${run.id}` : `${item.id}@${at}`, item, at, run, state });
    }
    for (const run of runs) if (!used.has(run.id)) rows.push({ key: `${item.id}:${run.id}`, item, at: runAt(run), run, state: runState(run) });
  }
  return rows.sort((a, b) => a.at - b.at || a.item.title.localeCompare(b.item.title));
}

/** A repeat's time on a day before its next time: only to find today's reminder or event already gone. */
function slotsBefore(item: CalendarItem, key: string, clock: CalendarClock): number[] {
  const next = itemInstant(item);
  if (!Number.isFinite(next)) return [];
  const step = item.recurrence === "weekly" ? 7 : 1;
  const shifted = { ...item, next_at: new Date(next - step * DAY).toISOString(), start_at: undefined, notify_at: undefined, due_at: undefined };
  return slotsOn(shifted, key, clock).filter((at) => at < next - SAME_TIME_MS / 2);
}

export interface Agenda {
  /** Today's rows, what earlier days left open first (running, waiting on the person). */
  today: AgendaRow[];
  /** How many of today's rows are today's own. */
  todayCount: number;
  tomorrow: AgendaRow[];
  /** One-off items after tomorrow, and repeats whose next time is after tomorrow, at that time. */
  later: AgendaRow[];
  /** Earlier days' one-off items, finished: newest day first. */
  earlier: { key: string; rows: AgendaRow[] }[];
  done: number;
  failed: number;
}

export function agenda(items: readonly CalendarItem[], clock: CalendarClock): Agenda {
  const zone = clock.zones.device;
  const today = dayKey(clock);
  const tomorrowKey = dayKey(clock, 1);
  const todayRows = dayRows(items, today, clock);
  const tomorrow = dayRows(items, tomorrowKey, clock);
  const shown = new Set([...todayRows, ...tomorrow].map((row) => row.item.id));
  const carried: AgendaRow[] = [];
  const later: AgendaRow[] = [];
  const byDay = new Map<string, AgendaRow[]>();
  let done = 0;
  let failed = 0;
  for (const item of items) {
    if (item.status === "cancelled") continue;
    const at = itemInstant(item);
    if (!Number.isFinite(at)) continue;
    const key = clock.zones.dateKey(at, zone);
    if (key > tomorrowKey) {
      if (!shown.has(item.id)) later.push({ key: item.id, item, at, state: "coming" });
      continue;
    }
    if (key >= today || repeats(item)) continue;
    // An earlier day's item: still open, it stays in Today; finished, it goes to Earlier.
    const run = (item.runs ?? []).at(-1);
    const state = run ? runState(run) : itemState(item, at, clock.now);
    if (item.status === "running" || item.status === "waiting" || (item.status === "scheduled" && item.kind !== "event")) {
      carried.push({ key: item.id, item, at, run, state: item.status === "scheduled" ? "coming" : state, carried: true });
      continue;
    }
    if (state === "failed") failed++;
    else done++;
    const list = byDay.get(key) ?? [];
    list.push({ key: item.id, item, at, run, state });
    byDay.set(key, list);
  }
  const earlier = [...byDay.entries()].sort((a, b) => b[0].localeCompare(a[0])).map(([key, rows]) => ({ key, rows: rows.sort((a, b) => a.at - b.at) }));
  return {
    today: [...carried.sort((a, b) => a.at - b.at), ...todayRows],
    todayCount: todayRows.length,
    tomorrow,
    later: later.sort((a, b) => a.at - b.at),
    earlier,
    done,
    failed,
  };
}

/** The month's dots: days with one-off items, `failed` where one of them failed. Repeats get none (every day would). */
export function monthMarks(items: readonly CalendarItem[], clock: CalendarClock): Map<string, "item" | "failed"> {
  const marks = new Map<string, "item" | "failed">();
  for (const item of items) {
    if (item.status === "cancelled" || repeats(item)) continue;
    const at = itemInstant(item);
    if (!Number.isFinite(at)) continue;
    const key = clock.zones.dateKey(at, clock.zones.device);
    const failed = item.status === "failed" || (item.runs ?? []).some((run) => runState(run) === "failed");
    if (failed) marks.set(key, "failed");
    else if (!marks.has(key)) marks.set(key, "item");
  }
  return marks;
}

/** Who acts on an item that is still to come: Brain for its scheduled actions, Mewla for its own, else what it is. */
function actor(item: CalendarItem): string | undefined {
  if (item.owner) return "Mewla";
  if (item.kind === "scheduled_action") return "Brain";
  if (item.kind === "reminder") return "Reminder";
  if (item.kind === "deadline") return "Deadline";
  return undefined;
}

function repeatWords(item: CalendarItem, clock: CalendarClock): string | undefined {
  switch (item.recurrence) {
    case "daily": return "Every day";
    case "weekdays": return "Weekdays";
    case "weekly": return `Every ${clock.format.weekday(itemInstant(item), item.timezone)}`;
    default: return undefined;
  }
}

function runLine(run: CalendarRun): string {
  if (run.status === "failed") return firstSentence(plainLine(run.failure_reason), 80) || runPreview(run);
  return runPreview(run);
}

/**
 * A row's one quiet line: how its run went ("Done · 412 matched", "Failed · why", "Needs you · what"), running and
 * for how long, else how it repeats and who acts ("Every day · Brain"); a later or carried row leads with its date.
 */
export function rowLine(row: AgendaRow, clock: CalendarClock, options: { date?: boolean } = {}): string {
  const join = (...parts: (string | undefined)[]) => parts.filter(Boolean).join(" · ");
  const date = options.date || row.carried ? clock.format.day(row.at, clock.zones.device) : undefined;
  const { item, run } = row;
  switch (row.state) {
    case "done": return join(date, "Done", run ? runLine(run) : undefined);
    case "failed": return join(date, "Failed", run ? runLine(run) : firstSentence(plainLine(item.failure_reason), 80));
    case "needs_you": return join(date, "Needs you", run ? runLine(run) : undefined);
    case "running": {
      const started = run ? Date.parse(run.started_at) : NaN;
      const minutes = Number.isFinite(started) ? Math.max(1, Math.floor((clock.now - started) / 60_000)) : 0;
      return join(date, "Running", minutes ? (minutes < 60 ? `${minutes} min` : `${Math.floor(minutes / 60)} h ${minutes % 60} min`) : undefined);
    }
  }
  return join(date, repeatWords(item, clock), actor(item));
}

/** Rows of a finished or passed state are dimmed. */
export function rowDim(row: AgendaRow): boolean {
  return row.state === "done" || row.state === "past";
}
