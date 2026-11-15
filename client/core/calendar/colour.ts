import type { CalendarItem } from "./model";

/**
 * Calendar's lead (soft-final SPEC §10.4): the glyph on the neutral tile says who acts. Something for you is a
 * `bell` (a reminder), `calendar` (an event) or `flag` (a deadline); a job Brain runs once is `flash`; one it runs
 * again and again, a recurring check, is `sync`.
 */
export type CalendarGlyph = "bell" | "calendar" | "flag" | "flash" | "sync";

export function calendarLead(item: Pick<CalendarItem, "kind" | "recurrence">): { kind: "tile"; glyph: CalendarGlyph } {
  if (item.kind === "scheduled_action") {
    return item.recurrence && item.recurrence !== "none" ? { kind: "tile", glyph: "sync" } : { kind: "tile", glyph: "flash" };
  }
  return { kind: "tile", glyph: item.kind === "event" ? "calendar" : item.kind === "deadline" ? "flag" : "bell" };
}
