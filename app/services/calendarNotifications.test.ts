import { afterEach, describe, expect, spyOn, test } from "bun:test";
import { resetWarnOnce } from "./warnOnce";
import { reconcileCalendarNotifications } from "./calendarNotificationReconciler";
import type { CalendarItem } from "../store/calendar";

const base: CalendarItem = {
  id: "reminder",
  title: "Item",
  kind: "reminder",
  status: "scheduled",
  notify_at: "2026-07-14T09:00:00Z",
  timezone: "UTC",
  recurrence: "daily",
  next_at: "2026-07-15T09:00:00Z",
  created_at: "2026-07-14T09:00:00Z",
  updated_at: "2026-07-14T09:00:00Z",
  revision: 1,
};

function harness(initial = "{}") {
  let stored = initial;
  const scheduled: any[] = [];
  const cancelled: string[] = [];
  let sequence = 0;
  return {
    scheduled,
    cancelled,
    value: () => JSON.parse(stored),
    deps: {
      getStored: async () => stored,
      setStored: async (value: string) => {
        stored = value;
      },
      schedule: async (request: any) => {
        scheduled.push(request);
        sequence += 1;
        return `notification-${sequence}`;
      },
      cancel: async (id: string) => {
        cancelled.push(id);
      },
      ensureChannel: async () => undefined,
    },
  };
}

afterEach(() => resetWarnOnce());

describe("calendar notification reconciliation", () => {
  test("schedules the canonical next occurrence, not original notify_at", async () => {
    const mock = harness();
    await reconcileCalendarNotifications(
      "server",
      [base],
      mock.deps,
      Date.parse("2026-07-14T10:00:00Z"),
    );
    expect(mock.scheduled).toHaveLength(1);
    expect(mock.scheduled[0].trigger).toBe("2026-07-15T09:00:00.000Z");
    expect(mock.scheduled[0].alertKey).toBe(`reminder@${Date.parse(base.next_at)}`);
    expect(mock.value()["server:reminder"].trigger).toBe("2026-07-15T09:00:00.000Z");
  });

  test("reuses only an exact trigger and content fingerprint", async () => {
    const mock = harness();
    const now = Date.parse("2026-07-14T10:00:00Z");
    await reconcileCalendarNotifications("server", [base], mock.deps, now);
    await reconcileCalendarNotifications("server", [base], mock.deps, now);
    expect(mock.scheduled).toHaveLength(1);
    expect(mock.cancelled).toEqual([]);

    await reconcileCalendarNotifications(
      "server",
      [{ ...base, title: "Changed", revision: 2 }],
      mock.deps,
      now,
    );
    expect(mock.cancelled).toEqual(["notification-1"]);
    expect(mock.scheduled).toHaveLength(2);
  });

  test("reschedules recurrence changes and cancels terminal or removed items", async () => {
    const mock = harness();
    const now = Date.parse("2026-07-14T10:00:00Z");
    await reconcileCalendarNotifications("server", [base], mock.deps, now);
    const next = { ...base, next_at: "2026-07-16T09:00:00Z", revision: 2 };
    await reconcileCalendarNotifications("server", [next], mock.deps, now);
    expect(mock.cancelled).toEqual(["notification-1"]);
    expect(mock.scheduled[1].trigger).toBe("2026-07-16T09:00:00.000Z");

    await reconcileCalendarNotifications(
      "server",
      [{ ...next, status: "completed" }],
      mock.deps,
      now,
    );
    expect(mock.cancelled).toEqual(["notification-1", "notification-2"]);
    expect(mock.value()).toEqual({});
  });

  test("an item Mewla acts on gets no local reminder, and a stale one is cancelled", async () => {
    const mock = harness();
    const now = Date.parse("2026-07-14T10:00:00Z");
    await reconcileCalendarNotifications("server", [base], mock.deps, now);
    await reconcileCalendarNotifications(
      "server",
      [{ ...base, owner: "watch_brief" }],
      mock.deps,
      now,
    );
    expect(mock.scheduled).toHaveLength(1);
    expect(mock.cancelled).toEqual(["notification-1"]);
    expect(mock.value()).toEqual({});
  });

  test("events and deadlines are armed ahead of their time, with the lead in the words", async () => {
    const mock = harness();
    const now = Date.parse("2026-07-14T10:00:00Z");
    const event: CalendarItem = {
      ...base,
      id: "event",
      kind: "event",
      recurrence: "none",
      start_at: "2026-07-14T15:30:00Z",
      end_at: "2026-07-14T16:30:00Z",
      next_at: "2026-07-14T15:30:00Z",
    };
    const deadline: CalendarItem = {
      ...event,
      id: "deadline",
      kind: "deadline",
      due_at: "2026-07-14T15:30:00Z",
      alert_lead_minutes: 60,
    };
    await reconcileCalendarNotifications("server", [event, deadline], mock.deps, now);
    expect(
      mock.scheduled.map((r) => [r.itemId, r.trigger, r.body]),
    ).toEqual([
      ["event", "2026-07-14T15:20:00.000Z", "Starts in 10 min"],
      ["deadline", "2026-07-14T14:30:00.000Z", "Due in 1 h"],
    ]);
  });

  test("an alert the daemon already fired is cancelled, not left to fire a second time", async () => {
    const mock = harness();
    const now = Date.parse("2026-07-14T10:00:00Z");
    await reconcileCalendarNotifications("server", [base], mock.deps, now);
    await reconcileCalendarNotifications(
      "server",
      [{ ...base, alerted_at: base.next_at, revision: 2 }],
      mock.deps,
      now,
    );
    expect(mock.cancelled).toEqual(["notification-1"]);
    expect(mock.value()).toEqual({});
  });

  test("changing the strong-alerts variant schedules again", async () => {
    const mock = harness();
    const now = Date.parse("2026-07-14T10:00:00Z");
    let variant = "strong";
    const deps = { ...mock.deps, variant: () => variant };
    await reconcileCalendarNotifications("server", [base], deps, now);
    variant = "quiet";
    await reconcileCalendarNotifications("server", [base], deps, now);
    expect(mock.scheduled).toHaveLength(2);
    expect(mock.cancelled).toEqual(["notification-1"]);
  });

  test("server removal does not cancel another server's notifications", async () => {
    const mock = harness();
    const now = Date.parse("2026-07-14T10:00:00Z");
    await reconcileCalendarNotifications("a", [base], mock.deps, now);
    await reconcileCalendarNotifications(
      "b",
      [{ ...base, id: "b" }],
      mock.deps,
      now,
    );
    await reconcileCalendarNotifications("a", [], mock.deps, now);
    expect(Object.keys(mock.value())).toEqual(["b:b"]);
    expect(mock.cancelled).toEqual(["notification-1"]);
  });

  test("a reminder that cannot be cancelled stays recorded so the next sync retries it", async () => {
    resetWarnOnce();
    const warn = spyOn(console, "warn").mockImplementation(() => {});
    const mock = harness();
    const now = Date.parse("2026-07-14T10:00:00Z");
    await reconcileCalendarNotifications("server", [base], mock.deps, now);
    let failing = true;
    const deps = {
      ...mock.deps,
      cancel: async (id: string) => {
        if (failing) throw new Error("cancel failed");
        mock.cancelled.push(id);
      },
    };
    await reconcileCalendarNotifications("server", [], deps, now);
    await reconcileCalendarNotifications("server", [], deps, now);
    expect(Object.keys(mock.value())).toHaveLength(1);
    expect(warn).toHaveBeenCalledTimes(1);

    failing = false;
    await reconcileCalendarNotifications("server", [], deps, now);
    expect(mock.cancelled).toEqual(["notification-1"]);
    expect(mock.value()).toEqual({});
    warn.mockRestore();
  });

  test("an unreadable stored record is logged once and reminders are scheduled again", async () => {
    resetWarnOnce();
    const warn = spyOn(console, "warn").mockImplementation(() => {});
    const mock = harness("{not json");
    const now = Date.parse("2026-07-14T10:00:00Z");
    await reconcileCalendarNotifications("server", [base], mock.deps, now);
    expect(mock.scheduled).toHaveLength(1);
    expect(warn).toHaveBeenCalledTimes(1);
    warn.mockRestore();
  });
});
