import { describe, expect, test } from "bun:test";
import { agenda, dayKey, dayRows, monthMarks, rowDim, rowLine, runState } from "./agenda";
import type { CalendarItem, CalendarRun } from "./model";
import { testClock } from "./testClock";

const ZONE = "Asia/Shanghai";
// Sunday Nov 22 2026, 10:44 in Shanghai.
const NOW = "2026-11-22T02:44:00Z";
const clock = testClock(NOW, ZONE, [], false);

const local = (date: string, time: string) => `${date}T${time}:00+08:00`;

function make(id: string, at: string, extra: Partial<CalendarItem> = {}): CalendarItem {
  return {
    id, title: id, kind: "scheduled_action", status: "scheduled", due_at: at, timezone: ZONE, recurrence: "none", next_at: at,
    created_at: "2026-10-01T00:00:00Z", updated_at: at, revision: 1, ...extra,
  };
}

function run(id: string, scheduled: string, status: CalendarRun["status"], extra: Partial<CalendarRun> = {}): CalendarRun {
  return { id, scheduled_for: scheduled, started_at: scheduled, status, ...extra };
}

describe("a day's rows", () => {
  test("a repeat that already ran today shows at its time with its run, and again tomorrow", () => {
    const digest = make("digest", local("2026-11-23", "07:30"), {
      recurrence: "daily",
      runs: [run("r1", local("2026-11-21", "07:30"), "completed", { result_preview: "Clear" }), run("r2", local("2026-11-22", "07:30"), "completed", { result_preview: "Rain after 17:00" })],
    });
    const today = dayRows([digest], dayKey(clock), clock);
    expect(today.map((row) => [row.run?.id, row.state])).toEqual([["r2", "done"]]);
    expect(rowLine(today[0]!, clock)).toBe("Done · Rain after 17:00");
    expect(rowDim(today[0]!)).toBe(true);
    const tomorrow = dayRows([digest], dayKey(clock, 1), clock);
    expect(tomorrow.map((row) => [row.state, rowLine(row, clock)])).toEqual([["coming", "Every day · Brain"]]);
  });

  test("a repeat still to come today is its next time; a running one has its run", () => {
    const crawl = make("crawl", local("2026-11-22", "09:45"), { recurrence: "daily", status: "running", runs: [run("r", local("2026-11-22", "09:45"), "running", { started_at: "2026-11-22T01:45:00Z" })] });
    const later = make("later", local("2026-11-22", "15:45"), { recurrence: "daily" });
    const rows = dayRows([crawl, later], dayKey(clock), clock);
    expect(rows.map((row) => [row.item.id, row.state])).toEqual([["crawl", "running"], ["later", "coming"]]);
    expect(rowLine(rows[0]!, clock)).toBe("Running · 59 min");
  });

  test("a past day shows what is known and never draws a repeat into it", () => {
    const daily = make("daily", local("2026-11-23", "07:30"), { recurrence: "daily", runs: [run("old", local("2026-11-16", "07:30"), "completed")] });
    const gap = dayRows([daily], "2026-11-15", clock);
    expect(gap).toEqual([]);
    expect(dayRows([daily], "2026-11-16", clock).map((row) => row.run?.id)).toEqual(["old"]);
  });

  test("a future day lists every repeat that falls on it: weekdays skip the weekend, weekly keeps its day", () => {
    const items = [
      make("daily", local("2026-11-22", "15:45"), { recurrence: "daily" }),
      make("weekdays", local("2026-11-23", "09:00"), { recurrence: "weekdays" }),
      make("weekly", local("2026-11-22", "21:00"), { recurrence: "weekly" }),
    ];
    expect(dayRows(items, "2026-11-28", clock).map((row) => row.item.id)).toEqual(["daily"]);
    expect(dayRows(items, "2026-11-24", clock).map((row) => row.item.id)).toEqual(["weekdays", "daily"]);
    expect(dayRows(items, "2026-11-29", clock).map((row) => row.item.id)).toEqual(["daily", "weekly"]);
  });

  test("a repeating reminder's time already gone today shows dimmed, with no check: nothing says it happened", () => {
    const brief = make("brief", local("2026-11-23", "08:15"), { kind: "reminder", notify_at: local("2026-11-23", "08:15"), due_at: undefined, recurrence: "daily", owner: "watch_brief" });
    const rows = dayRows([brief], dayKey(clock), clock);
    expect(rows.map((row) => row.state)).toEqual(["past"]);
    expect(rowLine(rows[0]!, clock)).toBe("Every day · Mewla");
  });

  test("an item in another zone lands on the device's day", () => {
    const ny = make("ny", "2026-11-22T14:00:00Z", { timezone: "America/New_York", recurrence: "daily" });
    // 09:00 in New York is 22:00 in Shanghai.
    expect(dayRows([ny], "2026-11-23", clock).map((row) => clock.format.time(row.at, ZONE))).toEqual(["22:00"]);
  });
});

describe("how a run went", () => {
  test("its agent's outcome wins over the status: failed reads Failed, needs you reads Needs you", () => {
    expect(runState(run("a", NOW, "completed", { outcome: "failed" }))).toBe("failed");
    expect(runState(run("a", NOW, "completed", { outcome: "needs_you" }))).toBe("needs_you");
    expect(runState(run("a", NOW, "completed"))).toBe("done");
    expect(runState(run("a", NOW, "failed"))).toBe("failed");
  });
  test("a run without an outcome reads Done and its first line", () => {
    const item = make("x", local("2026-11-22", "09:45"), { runs: [run("r", local("2026-11-22", "09:45"), "completed", { result_preview: "Exit code 1: sign-in expired" })] });
    expect(rowLine(dayRows([item], dayKey(clock), clock)[0]!, clock)).toBe("Done · Exit code 1: sign-in expired");
  });
  test("a failed run says why", () => {
    const item = make("x", local("2026-11-22", "09:45"), { status: "failed", runs: [run("r", local("2026-11-22", "09:45"), "failed", { failure_reason: "The key was refused. More." })] });
    expect(rowLine(dayRows([item], dayKey(clock), clock)[0]!, clock)).toBe("Failed · The key was refused.");
  });
});

describe("the agenda", () => {
  const items = [
    make("old-done", local("2026-11-16", "15:00"), { status: "completed", runs: [run("a", local("2026-11-16", "15:00"), "completed")] }),
    make("old-failed", local("2026-11-11", "20:00"), { status: "failed", runs: [run("b", local("2026-11-11", "20:00"), "failed")] }),
    make("old-event", local("2026-11-10", "14:00"), { kind: "event", status: "completed" }),
    make("waiting", local("2026-11-20", "10:00"), { kind: "reminder", status: "waiting" }),
    make("today", local("2026-11-22", "09:15"), { status: "completed", runs: [run("c", local("2026-11-22", "09:15"), "completed")] }),
    make("daily", local("2026-11-22", "15:45"), { recurrence: "daily" }),
    make("weekly", local("2026-11-27", "21:00"), { recurrence: "weekly" }),
    make("later", local("2026-12-09", "09:00")),
    make("gone", local("2026-11-22", "11:00"), { status: "cancelled" }),
  ];
  const a = agenda(items, clock);

  test("Today: what an earlier day left open first, then today's own; the count is today's own", () => {
    expect(a.today.map((row) => row.item.id)).toEqual(["waiting", "today", "daily"]);
    expect(a.today[0]!.carried).toBe(true);
    expect(rowLine(a.today[0]!, clock)).toBe("Fri, Nov 20 · Needs you");
    expect(a.todayCount).toBe(2);
  });
  test("Tomorrow projects the repeats; Later holds one-offs and repeats whose next time is after tomorrow", () => {
    expect(a.tomorrow.map((row) => row.item.id)).toEqual(["daily"]);
    expect(a.later.map((row) => [row.item.id, rowLine(row, clock, { date: true })])).toEqual([["weekly", "Fri, Nov 27 · Every Friday · Brain"], ["later", "Wed, Dec 9 · Brain"]]);
  });
  test("Earlier: finished items of earlier days, newest day first, counted done and failed; cancelled never", () => {
    expect(a.earlier.map((day) => day.key)).toEqual(["2026-11-16", "2026-11-11", "2026-11-10"]);
    expect([a.done, a.failed]).toEqual([2, 1]);
  });
  test("Month marks days with one-off items, amber where one failed; repeats get none", () => {
    const marks = monthMarks(items, clock);
    expect(marks.get("2026-11-11")).toBe("failed");
    expect(marks.get("2026-11-16")).toBe("item");
    expect(marks.has("2026-11-27")).toBe(false);
    expect(marks.has("2026-11-23")).toBe(false);
  });
});
