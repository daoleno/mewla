import { describe, expect, test } from "bun:test";
import { alertAt, alertBody, alertPlan, nextAlertDueAt, openAlerts, remainingSnoozeMinutes } from "./alerts";
import type { CalendarItem } from "./model";

const at = "2026-07-14T15:30:00Z";
const ms = (value: string) => Date.parse(value);
const make = (patch: Partial<CalendarItem> = {}): CalendarItem => ({
  id: "item", title: "Design review", kind: "event", status: "scheduled", start_at: at, end_at: "2026-07-14T16:30:00Z",
  timezone: "UTC", recurrence: "none", next_at: at, created_at: at, updated_at: at, revision: 1, ...patch,
});

describe("when an item alerts", () => {
  test("each kind's default lead, and the item's own lead wins", () => {
    expect(alertAt(make())).toBe(ms(at) - 10 * 60_000);
    expect(alertAt(make({ kind: "deadline" }))).toBe(ms(at) - 30 * 60_000);
    expect(alertAt(make({ kind: "reminder" }))).toBe(ms(at));
    expect(alertAt(make({ alert_lead_minutes: 45 }))).toBe(ms(at) - 45 * 60_000);
    expect(alertAt(make({ alert_lead_minutes: 0 }))).toBe(ms(at));
  });
  test("a snooze replaces the lead; owned, finished and action items never alert", () => {
    expect(alertAt(make({ snoozed_until: "2026-07-14T15:00:00Z" }))).toBe(ms("2026-07-14T15:00:00Z"));
    expect(alertAt(make({ owner: "watch_brief" }))).toBeNull();
    expect(alertAt(make({ status: "cancelled" }))).toBeNull();
    expect(alertAt(make({ kind: "scheduled_action" }))).toBeNull();
  });
});

describe("the alerts the phone arms", () => {
  const now = ms("2026-07-14T09:00:00Z");
  test("every item's next alert once, in the future only, under the key the daemon's push carries", () => {
    const plan = alertPlan("s1", [make({ id: "event" }), make({ id: "deadline", kind: "deadline" }), make({ id: "reminder", kind: "reminder", notes: "Bring slides" }), make({ id: "past", next_at: "2026-07-14T08:00:00Z" })], now);
    expect(plan.map((a) => a.itemId)).toEqual(["deadline", "event", "reminder"]);
    expect(plan[1]).toEqual({ key: `event@${ms("2026-07-14T15:20:00Z")}`, itemId: "event", serverId: "s1", title: "Design review", body: "Starts in 10 min", atMs: ms("2026-07-14T15:20:00Z"), timezone: "UTC" });
    expect(plan[0].body).toBe("Due in 30 min");
    expect(plan[2].body).toBe("Reminder · Bring slides");
  });
  test("an alert the daemon already fired is not armed again; its snooze is", () => {
    expect(alertPlan("s1", [make({ alerted_at: "2026-07-14T15:20:00Z" })], now)).toEqual([]);
    expect(alertPlan("s1", [make({ alerted_at: "2026-07-14T15:20:00Z", snoozed_until: "2026-07-14T15:25:00Z" })], now)[0].atMs).toBe(ms("2026-07-14T15:25:00Z"));
  });
  test("the body says how soon, down to now", () => {
    expect(alertBody(make(), ms(at))).toBe("Starting now");
    expect(alertBody(make({ kind: "deadline" }), ms(at))).toBe("Due now");
    expect(alertBody(make(), ms(at) - 90 * 60_000)).toBe("Starts in 1 h 30 min");
  });
  test("the card wakes at the earliest owed alert", () => {
    expect(nextAlertDueAt([make({ id: "a" }), make({ id: "b", alert_lead_minutes: 60 })], now)).toBe(ms(at) - 60 * 60_000);
    expect(nextAlertDueAt([make({ status: "completed" })], now)).toBeNull();
  });
});

describe("the open alerts (the in-app card)", () => {
  const fired = ms("2026-07-14T15:20:00Z");
  test("an alert the daemon fired stays open until it is answered", () => {
    const item = make({ alerted_at: "2026-07-14T15:20:00Z" });
    expect(openAlerts([item], fired + 1000)).toEqual([item]);
    expect(openAlerts([item], fired + 40 * 60_000)).toEqual([item]);
    expect(openAlerts([{ ...item, acked_at: "2026-07-14T15:20:00Z" }], fired + 1000)).toEqual([]);
  });
  test("a snooze closes it until the snooze comes due", () => {
    const snoozed = make({ alerted_at: "2026-07-14T15:20:00Z", acked_at: "2026-07-14T15:20:00Z", snoozed_until: "2026-07-14T15:25:00Z" });
    expect(openAlerts([snoozed], ms("2026-07-14T15:24:59Z"))).toEqual([]);
    expect(openAlerts([snoozed], ms("2026-07-14T15:25:01Z"))).toEqual([snoozed]);
  });
  test("the phone's clock opens an alert the daemon has not recorded yet, but not an old one", () => {
    expect(openAlerts([make()], fired - 1000)).toEqual([]);
    expect(openAlerts([make()], fired + 1000)).toHaveLength(1);
    expect(openAlerts([make()], fired + 16 * 60_000)).toEqual([]);
  });
  test("a repeating reminder the daemon already moved on stays open for the alert it fired", () => {
    const moved = make({ kind: "reminder", recurrence: "daily", next_at: "2026-07-15T15:30:00Z", alerted_at: "2026-07-14T15:30:00Z" });
    expect(openAlerts([moved], ms("2026-07-14T15:31:00Z"))).toEqual([moved]);
  });
  test("no card for an event that is over, cancelled or owned; the oldest alert first", () => {
    const old = make({ id: "old", alerted_at: "2026-07-14T15:20:00Z" });
    const newer = make({ id: "newer", alerted_at: "2026-07-14T15:22:00Z" });
    expect(openAlerts([newer, old], fired + 5 * 60_000).map((i) => i.id)).toEqual(["old", "newer"]);
    expect(openAlerts([old], ms("2026-07-14T16:31:00Z"))).toEqual([]);
    expect(openAlerts([{ ...old, status: "cancelled" }], fired + 1000)).toEqual([]);
    expect(openAlerts([{ ...old, owner: "watch_brief" }], fired + 1000)).toEqual([]);
  });
});

describe("a snooze taken on the notification", () => {
  const action = { minutes: 15, atMs: 1_000_000 };
  test("tells the computer what is left of it when replayed late", () => {
    expect(remainingSnoozeMinutes(action, 1_000_000)).toBe(15);
    expect(remainingSnoozeMinutes(action, 1_000_000 + 4 * 60_000)).toBe(11);
  });
  test("has nothing left to tell once it ran out", () => expect(remainingSnoozeMinutes(action, 1_000_000 + 20 * 60_000)).toBeLessThan(1));
});
