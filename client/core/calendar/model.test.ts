import { describe, expect, test } from "bun:test";
import {
  activeRun, agendaSection, calendarOriginOfWork, firstSentence, groupAgenda, itemSummary, itemsOnDay, mergeRunResults, nextLine,
  originLabel, runPreview, runsNewestFirst, runWhen, scheduleLine, statusLabel, type CalendarItem, type CalendarRun,
} from "./model";
import { testClock } from "./testClock";

const make = (id: string, at: string, kind: CalendarItem["kind"] = "reminder"): CalendarItem => ({
  id, title: id, kind, status: "scheduled", notify_at: kind === "reminder" ? at : undefined, due_at: kind !== "reminder" ? at : undefined,
  timezone: "UTC", recurrence: "none", next_at: at, created_at: at, updated_at: at, revision: 1,
});

describe("the agenda", () => {
  test("groups by day in the phone's zone", () => {
    const clock = testClock("2026-07-14T10:00:00Z");
    expect(agendaSection(make("today", "2026-07-14T18:00:00Z"), clock)).toBe("Today");
    expect(groupAgenda([make("tomorrow", "2026-07-15T10:00:00Z")], clock)[0].title).toBe("Tomorrow");
  });
  test("an item after midnight is tomorrow where the phone is, whatever UTC says", () => {
    const afterMidnight = make("tomorrow", "2026-03-08T05:30:00Z");
    expect(agendaSection(afterMidnight, testClock("2026-03-08T04:30:00Z", "America/New_York"))).toBe("Tomorrow");
    expect(agendaSection(afterMidnight, testClock("2026-03-08T04:30:00Z", "UTC"))).toBe("Today");
  });
  test("keeps the week's boundary across the spring DST change", () => {
    expect(agendaSection(make("monday", "2026-03-09T16:00:00Z"), testClock("2026-03-07T17:00:00Z", "America/New_York"))).toBe("Later");
  });
  test("hides cancelled items, keeps overdue and done ones in Today, in time order", () => {
    const clock = testClock("2026-07-14T10:00:00Z");
    const sections = groupAgenda([
      { ...make("b", "2026-07-14T12:00:00Z"), status: "completed" },
      make("a", "2026-07-13T08:00:00Z"),
      { ...make("gone", "2026-07-14T11:00:00Z"), status: "cancelled" },
    ], clock);
    expect(sections).toEqual([{ title: "Today", items: [expect.objectContaining({ id: "a" }), expect.objectContaining({ id: "b" })] }]);
  });
  test("a picked day lists that day's items only", () => {
    const clock = testClock("2026-07-14T10:00:00Z", "Asia/Shanghai");
    expect(itemsOnDay([make("late", "2026-07-14T17:00:00Z"), make("early", "2026-07-14T03:00:00Z")], "2026-07-15", clock).map((i) => i.id)).toEqual(["late"]);
  });
  test("a due reminder reads as needing you", () => expect(statusLabel.waiting).toBe("Needs you"));
});

const ZONE = "Asia/Shanghai";
function run(id: string, status: CalendarRun["status"], day: number, extra: Partial<CalendarRun> = {}): CalendarRun {
  return { id, status, scheduled_for: `2026-10-${day}T20:30:00+08:00`, started_at: `2026-10-${day}T12:30:00Z`, ...extra };
}
// The real 运营 item: no summary, an English prompt, a stale item-level failure while scheduled for tomorrow.
const yunying: CalendarItem = {
  id: "43754e56", title: "运营：从今天的工作里挖稿", kind: "scheduled_action", status: "scheduled",
  due_at: "2026-10-28T20:30:00+08:00", next_at: "2026-10-28T20:30:00+08:00", timezone: ZONE, recurrence: "daily",
  action_instruction: "Daily content mining. The user said 「我们每天跟 Brain 聊这么多事情」 Follow worklog/ops/PIPELINE.md exactly.",
  source_thread_id: "brain_1790733088397305726", linked_work_id: "b063b219",
  failure_reason: "Linked Work/agent is no longer observable after restart; Mewla did not relaunch it to avoid duplicate execution.",
  created_at: "2026-10-23T13:57:41Z", updated_at: "2026-10-27T12:57:14Z", revision: 12,
  runs: [
    run("559f", "failed", 24, { failure_reason: "Linked agent completed before producing a valid terminal scheduled Work deliverable." }),
    run("c08f", "completed", 25, { result_preview: "今天的稿子好了，两条 X 和一条小红书，都是猫游戏这一天的事。" }),
    run("b063", "failed", 26, { failure_reason: "Linked Work/agent is no longer observable after restart; Mewla did not relaunch it to avoid duplicate execution." }),
  ],
};

describe("a schedule in words", () => {
  const here = testClock("2026-10-27T14:00:00Z", ZONE);
  test("says how often and when, naming the zone only when it is not the phone's", () => {
    expect(scheduleLine(yunying, here)).toBe("Every day at 8:30 PM");
    expect(scheduleLine(yunying, testClock("2026-10-27T14:00:00Z", "America/New_York"))).toBe("Every day at 8:30 PM GMT+8");
    expect(scheduleLine({ ...yunying, recurrence: "weekdays" }, here)).toBe("Weekdays at 8:30 PM");
    expect(scheduleLine({ ...yunying, recurrence: "weekly" }, here)).toBe("Every Wednesday at 8:30 PM");
    expect(scheduleLine({ ...yunying, recurrence: "none" }, here)).toBe("Oct 28 at 8:30 PM");
  });
  test("names the next time relative to today", () => {
    expect(nextLine(yunying, here)).toBe("Next: tomorrow 8:30 PM");
    expect(nextLine(yunying, testClock("2026-10-28T01:00:00Z", ZONE))).toBe("Next: today 8:30 PM");
    expect(nextLine(yunying, testClock("2026-10-25T01:00:00Z", ZONE))).toBe("Next: Wed, Oct 28, 8:30 PM");
    expect(nextLine({ ...yunying, status: "running" }, here)).toBeNull();
    expect(nextLine({ ...yunying, recurrence: "none" }, here)).toBeNull();
  });
});

describe("what an item does", () => {
  test("Brain's summary first, else the instruction's first sentence, never for a plain reminder", () => {
    expect(itemSummary({ ...yunying, summary: "每晚从当天的工作里挖出可发的稿子" })).toBe("每晚从当天的工作里挖出可发的稿子");
    expect(itemSummary(yunying)).toBe("Daily content mining.");
    expect(itemSummary({ ...yunying, kind: "reminder", action_instruction: undefined })).toBe("");
  });
  test("a sentence ends at Chinese or Latin punctuation and at one line", () => {
    expect(firstSentence("先看日志。然后修。")).toBe("先看日志。");
    expect(firstSentence("Check v1.2 builds. Then ship.")).toBe("Check v1.2 builds.");
    expect(firstSentence("a".repeat(200), 20)).toBe(`${"a".repeat(19)}…`);
  });
});

describe("runs", () => {
  const clock = testClock("2026-10-27T14:00:00Z", ZONE);
  test("newest first, each with when it was due and one line", () => {
    const runs = runsNewestFirst(yunying);
    expect(runs.map((r) => r.id)).toEqual(["b063", "c08f", "559f"]);
    expect(runWhen(runs[0], yunying, clock)).toBe("Mon, Oct 26, 8:30 PM");
    expect(runPreview(runs[1])).toBe("今天的稿子好了，两条 X 和一条小红书，都是猫游戏这一天的事。");
    expect(runPreview(runs[0])).toBe("Linked Work/agent is no longer observable after restart; Mewla did not relaunch it to avoid duplicate execution.");
    expect(runPreview(run("x", "completed", 27, { result: "## X1\n\n![cover](/a.png)\n\n- I built a cat." }))).toBe("X1");
  });
  test("finds the run still at work", () => {
    expect(activeRun(yunying)).toBeUndefined();
    expect(activeRun({ runs: [...yunying.runs!, run("r", "running", 27, { worker_session: "%3" })] })?.worker_session).toBe("%3");
  });
  test("a fetched item lends its full results to the listed runs, by run id", () => {
    const full = { ...yunying, runs: [run("c08f", "completed", 25, { result: "# Full\n\nbody" })] };
    const merged = mergeRunResults(yunying, full);
    expect(merged.runs?.find((r) => r.id === "c08f")?.result).toBe("# Full\n\nbody");
    expect(merged.runs?.find((r) => r.id === "b063")?.result).toBeUndefined();
    expect(mergeRunResults(yunying, { ...full, id: "other" })).toBe(yunying);
  });
});

describe("where Calendar Work came from", () => {
  const clock = testClock("2026-10-27T14:00:00Z", ZONE);
  test("reads the item and run from a calendar_action Work only", () => {
    expect(calendarOriginOfWork({ frontmatter: { kind: "calendar_action", extra: { calendar_item_id: "43754e56", calendar_run_id: "c08f" } } })).toEqual({ itemId: "43754e56", runId: "c08f" });
    expect(calendarOriginOfWork({ frontmatter: { kind: "task", extra: { calendar_item_id: "x" } } })).toBeNull();
  });
  test("labels it with the schedule's name and the run's date", () => {
    expect(originLabel({ itemId: "43754e56", runId: "c08f" }, [yunying], clock)).toBe("运营：从今天的工作里挖稿 · Oct 25");
    expect(originLabel({ itemId: "gone" }, [yunying], clock, { title: "每日论文速读", at: "2026-10-27T01:00:00Z" })).toBe("每日论文速读 · Oct 27");
    expect(originLabel({ itemId: "gone" }, [yunying], clock)).toBeNull();
  });
});
