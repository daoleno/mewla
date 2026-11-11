import { computed, effect, signal } from "nsh";
import { lifecycle } from "../lifecycle";
import { nextAlertDueAt, openAlerts } from "../../core/calendar/alerts";
import type { CalendarItem } from "../../core/calendar/model";
import { calendar, calendarItems } from "./state";

/**
 * A calendar alert while the app is open: the card (AlertCard) with the item that needs an
 * answer, until the person gives one (Done, a snooze, or Open in Calendar), as in v0.3.1.
 * What it shows comes from the synced Calendar and this phone's clock, not from how the alert
 * was delivered; an answer given on the phone's own alert (the notification, the lock-screen
 * alert) counts as soon as the app takes it, before the computer has it.
 */
const now = signal(Date.now());
/** Alerts opened in Calendar instead: the card does not return for them (until the computer changes). */
const opened = signal<ReadonlySet<string>>(new Set());
let timer: ReturnType<typeof setTimeout> | null = null;

export const alertKey = (item: CalendarItem) => `${item.id}@${item.alerted_at ?? ""}@${item.snoozed_until ?? ""}`;

/** The alert the card shows, and how many more wait behind it; null when none needs an answer. */
export const shownAlert = computed<{ item: CalendarItem; more: number } | null>(() => {
  now();
  const items = calendarItems();
  const model = calendar();
  if (!model || !model.alertsSettled()) return null;
  const skip = opened();
  const open = openAlerts(items, Date.now()).filter((item) => !skip.has(alertKey(item)) && !model.answered(item.id));
  return open.length ? { item: open[0], more: open.length - 1 } : null;
});

export function openedInCalendar(item: CalendarItem): void {
  opened.set(new Set([...opened(), alertKey(item)]));
}

/** Wakes the card when the next alert comes due, instead of polling. */
function wake(items: CalendarItem[]): void {
  if (timer) clearTimeout(timer);
  timer = null;
  const due = nextAlertDueAt(items, Date.now());
  if (due !== null) timer = setTimeout(() => now.set(Date.now()), Math.min(due - Date.now() + 500, 2_147_000_000));
}

export function startCalendarAlerts(): void {
  effect(() => {
    calendar();
    opened.set(new Set());
  });
  effect(() => {
    const items = calendarItems();
    now.set(Date.now());
    wake(items);
  });
  // Back in front: what was answered on the phone's own alert meanwhile is taken first.
  effect(() => {
    if (lifecycle() !== "active") return;
    now.set(Date.now());
    void calendar()?.replayNativeActions();
  });
}
