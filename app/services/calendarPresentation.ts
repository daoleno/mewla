import { DateStyle, formatDate } from "./dateFormat";
import type { CalendarItem, CalendarKind } from "../store/calendar";
import type { WorkStatus } from "../components/ui/workStatus";
export const kindLabel: Record<CalendarKind, string> = {
  event: "Event",
  reminder: "Reminder",
  deadline: "Deadline",
  scheduled_action: "Mewla action",
};
export const calendarStatusLabel: Record<CalendarItem["status"], string> = {
  scheduled: "Scheduled",
  waiting: "Needs you",
  running: "Running",
  completed: "Done",
  failed: "Failed",
  cancelled: "Cancelled",
};
/**
 * The Seal & Slip mark for a calendar status. A due reminder or deadline
 * waits for you, so it takes the seal; Scheduled is the quiet default and has
 * no mark, like an idle Session.
 */
export const calendarStatusMark: Record<CalendarItem["status"], WorkStatus | null> = {
  scheduled: null,
  waiting: "needs",
  running: "running",
  completed: "ready",
  failed: "failed",
  cancelled: "blocked",
};
export function itemInstant(item: CalendarItem): string {
  return item.next_at ?? item.start_at ?? item.notify_at ?? item.due_at!;
}
export function viewerTimezone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
}
export function calendarDateKey(instant: string | Date, timeZone: string) {
  const parts = new Intl.DateTimeFormat("en-CA-u-ca-gregory-nu-latn", {
    timeZone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).formatToParts(new Date(instant));
  const value = Object.fromEntries(
    parts
      .filter((part) => part.type !== "literal")
      .map((part) => [part.type, part.value]),
  );
  return `${value.year}-${value.month}-${value.day}`;
}
function dateOrdinal(key: string) {
  const [year, month, day] = key.split("-").map(Number);
  return Math.floor(Date.UTC(year, month - 1, day) / 86_400_000);
}
export function agendaSection(
  item: CalendarItem,
  now = new Date(),
  timeZone = viewerTimezone(),
): "Today" | "Tomorrow" | "Later This Week" | "Later" {
  const todayKey = calendarDateKey(now, timeZone);
  const days =
    dateOrdinal(calendarDateKey(itemInstant(item), timeZone)) -
    dateOrdinal(todayKey);
  if (days <= 0) return "Today";
  if (days === 1) return "Tomorrow";
  const [year, month, day] = todayKey.split("-").map(Number);
  const weekday = new Date(Date.UTC(year, month - 1, day)).getUTCDay();
  if (days <= 7 - weekday) return "Later This Week";
  return "Later";
}
export function groupAgenda(
  items: CalendarItem[],
  now = new Date(),
  timeZone = viewerTimezone(),
) {
  const labels = ["Today", "Tomorrow", "Later This Week", "Later"] as const;
  return labels
    .map((title) => ({
      title,
      data: items.filter(
        (item) => agendaSection(item, now, timeZone) === title,
      ),
    }))
    .filter((section) => section.data.length > 0);
}
export function formatCalendarTime(item: CalendarItem) {
  return formatDate(itemInstant(item), DateStyle.dateTime, {
    timeZone: item.timezone,
    timeZoneName: "short",
  });
}
export function executes(item: CalendarItem) {
  return item.kind === "scheduled_action";
}
