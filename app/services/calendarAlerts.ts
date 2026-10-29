import type { CalendarItem } from "../store/calendar";

/** Mirrors daemon/calendar/alert.go: the lead each kind alerts with when the item sets none. */
export const defaultAlertLeadMinutes = { event: 10, deadline: 30 } as const;
/** An alert later than this past its instant was missed, not owed (AlertWindow in the daemon). */
export const alertWindowMs = 15 * 60_000;
/** An alert the daemon fired stays open this long unanswered; past it the card is stale. */
export const openAlertMaxAgeMs = 24 * 60 * 60_000;

export function alertLeadMs(item: CalendarItem): number {
  if (typeof item.alert_lead_minutes === "number") {
    return item.alert_lead_minutes * 60_000;
  }
  if (item.kind === "event") return defaultAlertLeadMinutes.event * 60_000;
  if (item.kind === "deadline") return defaultAlertLeadMinutes.deadline * 60_000;
  return 0;
}

/** Whether the item alerts at all: Mewla's own reminders and scheduled actions do not. */
export function alerts(item: CalendarItem): boolean {
  return (
    !item.owner &&
    item.kind !== "scheduled_action" &&
    item.status !== "cancelled" &&
    item.status !== "failed" &&
    item.status !== "completed"
  );
}

/** When the item next alerts: its snooze, or its time less the lead. */
export function calendarAlertAt(item: CalendarItem): number | null {
  if (!alerts(item)) return null;
  if (item.snoozed_until) return Date.parse(item.snoozed_until);
  return Date.parse(item.next_at) - alertLeadMs(item);
}

function time(value: string | undefined): number {
  return value ? Date.parse(value) : Number.NEGATIVE_INFINITY;
}

/** The alert the daemon still owes: later than the last one it fired. */
export function pendingAlertAt(item: CalendarItem): number | null {
  const at = calendarAlertAt(item);
  if (at === null || at <= time(item.alerted_at)) return null;
  return at;
}

function eventOver(item: CalendarItem, now: number): boolean {
  return item.kind === "event" && !!item.end_at && Date.parse(item.end_at) <= now;
}

/**
 * The alerts to arm on the phone: the next one of every item, still in the
 * future. A repeating item arms its next occurrence only; the push covers
 * the ones after it until the app syncs again.
 */
export function calendarAlertPlan(serverId: string, items: CalendarItem[], now = Date.now()) {
  const plan: {
    key: string;
    item: CalendarItem;
    at: number;
    trigger: string;
    title: string;
    body: string;
    fingerprint: string;
  }[] = [];
  for (const item of items) {
    const at = pendingAlertAt(item);
    if (at === null || at <= now || eventOver(item, now)) continue;
    const body = calendarAlertBody(item, at);
    plan.push({
      key: `${serverId}:${item.id}`,
      item,
      at,
      trigger: new Date(at).toISOString(),
      title: item.title,
      body,
      fingerprint: JSON.stringify({ at, title: item.title, body, timezone: item.timezone }),
    });
  }
  return plan;
}

export function alertKey(itemId: string, at: number): string {
  return `${itemId}@${at}`;
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

/** The line under the title; the same words the daemon's push uses. */
export function calendarAlertBody(item: CalendarItem, alertAt: number): string {
  const until = Date.parse(item.next_at) - alertAt;
  let when: string;
  if (item.kind === "reminder") when = "Reminder";
  else if (until <= 30_000) when = item.kind === "deadline" ? "Due now" : "Starting now";
  else when = `${item.kind === "deadline" ? "Due in" : "Starts in"} ${span(until)}`;
  const notes = item.notes?.trim();
  return notes ? `${when} · ${notes}` : when;
}

/**
 * The alerts that need an answer now, oldest first: the ones the daemon fired
 * and nobody answered, and the ones whose instant this phone's clock has
 * reached before the daemon recorded them. Nothing here depends on how the
 * alert was delivered, so it reads the same on the web and survives a restart.
 */
export function openCalendarAlerts(items: CalendarItem[], now = Date.now()): CalendarItem[] {
  const open: { item: CalendarItem; at: number }[] = [];
  for (const item of items) {
    if (!alerts(item) || eventOver(item, now)) continue;
    const fired = time(item.alerted_at);
    const acked = time(item.acked_at);
    if (fired > acked && now - fired <= openAlertMaxAgeMs) {
      open.push({ item, at: fired });
      continue;
    }
    const pending = pendingAlertAt(item);
    if (pending !== null && pending > acked && pending <= now && now - pending <= alertWindowMs) {
      open.push({ item, at: pending });
    }
  }
  return open.sort((a, b) => a.at - b.at).map((entry) => entry.item);
}

/** The next instant an unopened alert becomes due, for waking the card without polling. */
export function nextAlertDueAt(items: CalendarItem[], now = Date.now()): number | null {
  let next: number | null = null;
  for (const item of items) {
    const at = pendingAlertAt(item);
    if (at !== null && at > now && !eventOver(item, now) && (next === null || at < next)) next = at;
  }
  return next;
}

/** Minutes left of a snooze taken natively, so another phone is told the true end; 0 or less when it already ran out. */
export function remainingSnoozeMinutes(
  action: { atMs: number; minutes: number },
  now = Date.now(),
): number {
  return Math.ceil((action.atMs + action.minutes * 60_000 - now) / 60_000);
}

/**
 * The wall-clock parts of an instant in a zone, for an iOS calendar trigger.
 * Pinning the zone keeps the alert at the same instant if the phone later
 * changes time zone.
 */
export function calendarTriggerParts(at: number, timezone: string) {
  const parts = Object.fromEntries(
    new Intl.DateTimeFormat("en-US-u-ca-gregory-nu-latn", {
      timeZone: timezone,
      hourCycle: "h23",
      year: "numeric",
      month: "numeric",
      day: "numeric",
      hour: "numeric",
      minute: "numeric",
      second: "numeric",
    })
      .formatToParts(new Date(at))
      .filter((part) => part.type !== "literal")
      .map((part) => [part.type, Number(part.value)]),
  );
  return {
    year: parts.year,
    month: parts.month,
    day: parts.day,
    hour: parts.hour,
    minute: parts.minute,
    second: parts.second,
    timezone,
  };
}

/**
 * The phone's alarms are one set for the one current computer (the native
 * schedule replaces what it held), so a snapshot from another computer must not
 * replace them.
 */
export function mayArmNative(currentServerId: string | null | undefined, serverId: string): boolean {
  return !currentServerId || currentServerId === serverId;
}

/** Removing a computer clears the armed alarms only when they are that computer's. */
export function mayCancelNative(armedServerId: string | null | undefined, serverId: string): boolean {
  return !armedServerId || armedServerId === serverId;
}
