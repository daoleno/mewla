import type { StateTone } from "../more/colour";
import type { CalendarItem, CalendarStatus } from "./model";

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

/**
 * An item's or a run's state: done is ok, a failure warn (Brain's job failed, nothing of yours stops), waiting for
 * you is red (your move), the rest off.
 */
export function calendarStateTone(status: CalendarStatus): StateTone {
  if (status === "completed") return "ok";
  if (status === "failed") return "warn";
  if (status === "waiting") return "error";
  return "off";
}
