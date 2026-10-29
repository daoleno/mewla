import { describe, expect, test } from "bun:test";
import {
  calendarAlertAt,
  calendarAlertBody,
  calendarAlertPlan,
  nextAlertDueAt,
  openCalendarAlerts,
} from "./calendarAlerts";
import type { CalendarItem } from "../store/calendar";

const at = "2026-07-14T15:30:00Z";
const ms = (value: string) => Date.parse(value);
const make = (patch: Partial<CalendarItem> = {}): CalendarItem => ({
  id: "item",
  title: "Design review",
  kind: "event",
  status: "scheduled",
  start_at: at,
  end_at: "2026-07-14T16:30:00Z",
  timezone: "UTC",
  recurrence: "none",
  next_at: at,
  created_at: at,
  updated_at: at,
  revision: 1,
  ...patch,
});

describe("calendar alert instants", () => {
  test("default leads per kind, and an item's own lead wins", () => {
    expect(calendarAlertAt(make())).toBe(ms(at) - 10 * 60_000);
    expect(calendarAlertAt(make({ kind: "deadline" }))).toBe(ms(at) - 30 * 60_000);
    expect(calendarAlertAt(make({ kind: "reminder" }))).toBe(ms(at));
    expect(calendarAlertAt(make({ alert_lead_minutes: 45 }))).toBe(ms(at) - 45 * 60_000);
    expect(calendarAlertAt(make({ alert_lead_minutes: 0 }))).toBe(ms(at));
  });

  test("a snooze replaces the lead; owned, finished and action items never alert", () => {
    expect(calendarAlertAt(make({ snoozed_until: "2026-07-14T15:00:00Z" }))).toBe(ms("2026-07-14T15:00:00Z"));
    expect(calendarAlertAt(make({ owner: "watch_brief" }))).toBeNull();
    expect(calendarAlertAt(make({ status: "cancelled" }))).toBeNull();
    expect(calendarAlertAt(make({ kind: "scheduled_action" }))).toBeNull();
  });
});

describe("calendar alert plan", () => {
  const now = ms("2026-07-14T09:00:00Z");

  test("arms every kind's next alert once, in the future only", () => {
    const plan = calendarAlertPlan(
      "server",
      [
        make({ id: "event" }),
        make({ id: "deadline", kind: "deadline" }),
        make({ id: "reminder", kind: "reminder", notes: "Bring slides" }),
        make({ id: "past", next_at: "2026-07-14T08:00:00Z" }),
      ],
      now,
    );
    expect(plan.map((entry) => entry.item.id)).toEqual(["event", "deadline", "reminder"]);
    expect(plan[0].trigger).toBe("2026-07-14T15:20:00.000Z");
    expect(plan[0].body).toBe("Starts in 10 min");
    expect(plan[1].body).toBe("Due in 30 min");
    expect(plan[2].body).toBe("Reminder · Bring slides");
  });

  test("an alert the daemon already fired is not armed again", () => {
    const fired = make({ alerted_at: "2026-07-14T15:20:00Z" });
    expect(calendarAlertPlan("server", [fired], now)).toEqual([]);
    const snoozed = make({ alerted_at: "2026-07-14T15:20:00Z", snoozed_until: "2026-07-14T15:25:00Z" });
    expect(calendarAlertPlan("server", [snoozed], now)[0].trigger).toBe("2026-07-14T15:25:00.000Z");
  });

  test("body says how soon, down to now", () => {
    expect(calendarAlertBody(make(), ms(at))).toBe("Starting now");
    expect(calendarAlertBody(make({ kind: "deadline" }), ms(at))).toBe("Due now");
    expect(calendarAlertBody(make(), ms(at) - 90 * 60_000)).toBe("Starts in 1 h 30 min");
  });

  test("wakes at the earliest owed alert", () => {
    expect(nextAlertDueAt([make({ id: "a" }), make({ id: "b", alert_lead_minutes: 60 })], now)).toBe(ms(at) - 60 * 60_000);
    expect(nextAlertDueAt([make({ status: "completed" })], now)).toBeNull();
  });
});

describe("open calendar alerts (the card)", () => {
  const fired = ms("2026-07-14T15:20:00Z");

  test("an alert the daemon fired stays open until it is answered", () => {
    const item = make({ alerted_at: "2026-07-14T15:20:00Z" });
    expect(openCalendarAlerts([item], fired + 1000)).toEqual([item]);
    expect(openCalendarAlerts([item], fired + 40 * 60_000)).toEqual([item]);
    expect(openCalendarAlerts([{ ...item, acked_at: "2026-07-14T15:20:00Z" }], fired + 1000)).toEqual([]);
  });

  test("a snooze closes it until the snooze alert fires", () => {
    const snoozed = make({
      alerted_at: "2026-07-14T15:20:00Z",
      acked_at: "2026-07-14T15:20:00Z",
      snoozed_until: "2026-07-14T15:25:00Z",
    });
    expect(openCalendarAlerts([snoozed], ms("2026-07-14T15:24:59Z"))).toEqual([]);
    // This phone's clock reached the snooze before the daemon recorded it.
    expect(openCalendarAlerts([snoozed], ms("2026-07-14T15:25:01Z"))).toEqual([snoozed]);
  });

  test("the phone's clock opens an alert the daemon has not recorded yet, but not an old one", () => {
    const item = make();
    expect(openCalendarAlerts([item], fired - 1000)).toEqual([]);
    expect(openCalendarAlerts([item], fired + 1000)).toEqual([item]);
    expect(openCalendarAlerts([item], fired + 16 * 60_000)).toEqual([]);
  });

  test("a repeating reminder the daemon already moved on stays open for the alert it fired", () => {
    const moved = make({
      kind: "reminder",
      recurrence: "daily",
      next_at: "2026-07-15T15:30:00Z",
      alerted_at: "2026-07-14T15:30:00Z",
    });
    expect(openCalendarAlerts([moved], ms("2026-07-14T15:31:00Z"))).toEqual([moved]);
  });

  test("an event that is over, cancelled or owned shows no card; the oldest alert comes first", () => {
    const old = make({ id: "old", alerted_at: "2026-07-14T15:20:00Z" });
    const newer = make({ id: "newer", alerted_at: "2026-07-14T15:22:00Z" });
    expect(openCalendarAlerts([newer, old], fired + 5 * 60_000).map((item) => item.id)).toEqual(["old", "newer"]);
    expect(openCalendarAlerts([old], ms("2026-07-14T16:31:00Z"))).toEqual([]);
    expect(openCalendarAlerts([{ ...old, status: "cancelled" }], fired + 1000)).toEqual([]);
    expect(openCalendarAlerts([{ ...old, owner: "watch_brief" }], fired + 1000)).toEqual([]);
  });
});
