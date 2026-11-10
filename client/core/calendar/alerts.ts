import type { CalendarItem } from "./model";

/**
 * Which items alert the phone, when, and with what words (the daemon's push says the
 * same: daemon/calendar/alert.go, daemon/push/calendar.go). The host arms the plan with
 * the system's own alarms (Android) or local notifications (iOS); the in-app card shows
 * the open ones while the app is in front.
 */
export const defaultLeadMinutes = { event: 10, deadline: 30 } as const;
/** An alert later than this past its instant was missed, not owed. */
export const ALERT_WINDOW_MS = 15 * 60_000;
/** An alert the daemon fired stays open this long unanswered. */
export const OPEN_ALERT_MAX_AGE_MS = 24 * 60 * 60_000;

export function leadMs(item: CalendarItem): number {
  if (typeof item.alert_lead_minutes === "number") return item.alert_lead_minutes * 60_000;
  if (item.kind === "event") return defaultLeadMinutes.event * 60_000;
  if (item.kind === "deadline") return defaultLeadMinutes.deadline * 60_000;
  return 0;
}

/** Mewla's own reminders and scheduled actions never alert; finished and cancelled items neither. */
export function alerts(item: CalendarItem): boolean {
  return !item.owner && item.kind !== "scheduled_action" && item.status !== "cancelled" && item.status !== "failed" && item.status !== "completed";
}

/** When the item next alerts: its snooze, or its time less the lead. */
export function alertAt(item: CalendarItem): number | null {
  if (!alerts(item)) return null;
  if (item.snoozed_until) return Date.parse(item.snoozed_until);
  return Date.parse(item.next_at) - leadMs(item);
}

const time = (value: string | undefined) => (value ? Date.parse(value) : Number.NEGATIVE_INFINITY);

/** The alert the daemon still owes: later than the last one it fired. */
export function pendingAlertAt(item: CalendarItem): number | null {
  const at = alertAt(item);
  return at === null || at <= time(item.alerted_at) ? null : at;
}

function eventOver(item: CalendarItem, now: number): boolean {
  return item.kind === "event" && !!item.end_at && Date.parse(item.end_at) <= now;
}

export function alertKey(itemId: string, at: number): string {
  return `${itemId}@${at}`;
}

/** One alert the host arms: `key` is the same one the daemon's push carries, so the phone shows it once. */
export interface ArmedAlert {
  key: string;
  itemId: string;
  serverId: string;
  title: string;
  body: string;
  atMs: number;
  /** The item's zone: an iOS calendar trigger pinned to it keeps the instant if the phone changes zone. */
  timezone: string;
}

/** The next alert of every item, still in the future: a repeating item arms its next one; the push covers later ones. */
export function alertPlan(serverId: string, items: readonly CalendarItem[], now = Date.now()): ArmedAlert[] {
  const plan: ArmedAlert[] = [];
  for (const item of items) {
    const at = pendingAlertAt(item);
    if (at === null || at <= now || eventOver(item, now)) continue;
    plan.push({ key: alertKey(item.id, at), itemId: item.id, serverId, title: item.title, body: alertBody(item, at), atMs: at, timezone: item.timezone });
  }
  return plan.sort((a, b) => a.atMs - b.atMs);
}

function span(ms: number): string {
  const minutes = Math.max(1, Math.round(ms / 60_000));
  if (minutes < 60) return `${minutes} min`;
  if (minutes < 24 * 60) {
    const hours = Math.floor(minutes / 60);
    const rest = minutes % 60;
    return rest ? `${hours} h ${rest} min` : `${hours} h`;
  }
  return `${Math.round(minutes / (24 * 60))} d`;
}

/** The line under the title: "Starts in 10 min · notes", the daemon's push's words. */
export function alertBody(item: CalendarItem, at: number): string {
  const until = Date.parse(item.next_at) - at;
  let when: string;
  if (item.kind === "reminder") when = "Reminder";
  else if (until <= 30_000) when = item.kind === "deadline" ? "Due now" : "Starting now";
  else when = `${item.kind === "deadline" ? "Due in" : "Starts in"} ${span(until)}`;
  const notes = item.notes?.trim();
  return notes ? `${when} · ${notes}` : when;
}

/**
 * The alerts that need an answer now, oldest first: the ones the daemon fired and
 * nobody answered, and the ones this phone's clock reached before the daemon recorded
 * them. None of it depends on how the alert was delivered.
 */
export function openAlerts(items: readonly CalendarItem[], now = Date.now()): CalendarItem[] {
  const open: { item: CalendarItem; at: number }[] = [];
  for (const item of items) {
    if (!alerts(item) || eventOver(item, now)) continue;
    const fired = time(item.alerted_at);
    const acked = time(item.acked_at);
    if (fired > acked && now - fired <= OPEN_ALERT_MAX_AGE_MS) {
      open.push({ item, at: fired });
      continue;
    }
    const pending = pendingAlertAt(item);
    if (pending !== null && pending > acked && pending <= now && now - pending <= ALERT_WINDOW_MS) open.push({ item, at: pending });
  }
  return open.sort((a, b) => a.at - b.at).map((entry) => entry.item);
}

/** The next instant an unopened alert becomes due, to wake the card then instead of polling. */
export function nextAlertDueAt(items: readonly CalendarItem[], now = Date.now()): number | null {
  let next: number | null = null;
  for (const item of items) {
    const at = pendingAlertAt(item);
    if (at !== null && at > now && !eventOver(item, now) && (next === null || at < next)) next = at;
  }
  return next;
}

/** Done or Snooze taken on a notification or the lock-screen alert while no app ran. */
export interface NativeAlertAction {
  type: "done" | "snooze";
  itemId: string;
  serverId: string;
  minutes: number;
  atMs: number;
}

/** What is left of a snooze taken natively, so the computer is told its true end; under 1 when it already ran out. */
export function remainingSnoozeMinutes(action: Pick<NativeAlertAction, "atMs" | "minutes">, now = Date.now()): number {
  return Math.ceil((action.atMs + action.minutes * 60_000 - now) / 60_000);
}
