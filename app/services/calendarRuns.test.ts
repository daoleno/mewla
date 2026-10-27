import { describe, expect, test } from "bun:test";
import {
  calendarActiveRun,
  calendarItemSummary,
  calendarNextLine,
  calendarOriginLabel,
  calendarOriginOfWork,
  calendarRunDate,
  calendarRunPreview,
  calendarRunsNewestFirst,
  calendarScheduleLine,
  firstSentence,
} from "./calendarRuns";
import type { CalendarItem, CalendarRun } from "../store/calendar";

const ZONE = "Asia/Shanghai";

function run(id: string, status: CalendarRun["status"], day: number, extra: Partial<CalendarRun> = {}): CalendarRun {
  return {
    id,
    status,
    scheduled_for: `2026-10-${day}T20:30:00+08:00`,
    started_at: `2026-10-${day}T12:30:00Z`,
    ...extra,
  };
}

// The real 运营 item as `mewla calendar get` shows it: no summary, an English
// prompt, a stale item-level failure while scheduled for tomorrow.
const yunying: CalendarItem = {
  id: "43754e56",
  title: "运营：从今天的工作里挖稿",
  kind: "scheduled_action",
  status: "scheduled",
  due_at: "2026-10-28T20:30:00+08:00",
  next_at: "2026-10-28T20:30:00+08:00",
  timezone: ZONE,
  recurrence: "daily",
  action_instruction:
    "Daily content mining. The user said 「我们每天跟 Brain 聊这么多事情」 Follow worklog/ops/PIPELINE.md exactly.",
  source_thread_id: "brain_1790733088397305726",
  linked_work_id: "b063b219",
  failure_reason: "Linked Work/agent is no longer observable after restart; Mewla did not relaunch it to avoid duplicate execution.",
  created_at: "2026-10-23T13:57:41Z",
  updated_at: "2026-10-27T12:57:14Z",
  revision: 12,
  runs: [
    run("559f", "failed", 24, { failure_reason: "Linked agent completed before producing a valid terminal scheduled Work deliverable." }),
    run("c08f", "completed", 25, { result_preview: "今天的稿子好了，两条 X 和一条小红书，都是猫游戏这一天的事。" }),
    run("b063", "failed", 26, { failure_reason: "Linked Work/agent is no longer observable after restart; Mewla did not relaunch it to avoid duplicate execution." }),
  ],
};

describe("Calendar schedule in words", () => {
  test("says how often and when, without the zone when it is the viewer's", () => {
    expect(calendarScheduleLine(yunying, ZONE)).toBe("Every day at 8:30 PM");
    expect(calendarScheduleLine(yunying, "America/New_York")).toBe("Every day at 8:30 PM GMT+8");
    expect(calendarScheduleLine({ ...yunying, recurrence: "weekdays" }, ZONE)).toBe("Weekdays at 8:30 PM");
    expect(calendarScheduleLine({ ...yunying, recurrence: "weekly" }, ZONE)).toBe("Every Wednesday at 8:30 PM");
    expect(calendarScheduleLine({ ...yunying, recurrence: "none" }, ZONE)).toBe("Oct 28 at 8:30 PM");
  });

  test("names the next time relative to today", () => {
    const now = new Date("2026-10-27T14:00:00Z");
    expect(calendarNextLine(yunying, now, ZONE)).toBe("Next: tomorrow 8:30 PM");
    expect(calendarNextLine(yunying, new Date("2026-10-28T01:00:00Z"), ZONE)).toBe("Next: today 8:30 PM");
    expect(calendarNextLine(yunying, new Date("2026-10-25T01:00:00Z"), ZONE)).toBe("Next: Wed, Oct 28, 8:30 PM");
    expect(calendarNextLine({ ...yunying, status: "running" }, now, ZONE)).toBeNull();
    expect(calendarNextLine({ ...yunying, recurrence: "none" }, now, ZONE)).toBeNull();
  });
});

describe("what an item does", () => {
  test("prefers Brain's summary and falls back to the instruction's first sentence", () => {
    expect(calendarItemSummary({ ...yunying, summary: "每晚从当天的工作里挖出可发的稿子" })).toBe("每晚从当天的工作里挖出可发的稿子");
    expect(calendarItemSummary(yunying)).toBe("Daily content mining.");
    expect(calendarItemSummary({ ...yunying, kind: "reminder", action_instruction: undefined })).toBe("");
  });

  test("cuts a sentence at Chinese or Latin punctuation and at one line", () => {
    expect(firstSentence("先看日志。然后修。")).toBe("先看日志。");
    expect(firstSentence("Check v1.2 builds. Then ship.")).toBe("Check v1.2 builds.");
    expect(firstSentence("a".repeat(200), 20)).toBe(`${"a".repeat(19)}…`);
  });
});

describe("runs", () => {
  test("newest first, each with its date and one line", () => {
    const runs = calendarRunsNewestFirst(yunying);
    expect(runs.map((item) => item.id)).toEqual(["b063", "c08f", "559f"]);
    expect(calendarRunDate(runs[0], yunying)).toBe("Mon, Oct 26");
    expect(calendarRunPreview(runs[1])).toBe("今天的稿子好了，两条 X 和一条小红书，都是猫游戏这一天的事。");
    expect(calendarRunPreview(runs[0])).toBe(
      "Linked Work/agent is no longer observable after restart; Mewla did not relaunch it to avoid duplicate execution.",
    );
    expect(calendarRunPreview(run("x", "completed", 27, { result: "## X1\n\n![cover](/a.png)\n\n- I built a cat." }))).toBe("X1");
  });

  test("finds the run still at work", () => {
    expect(calendarActiveRun(yunying)).toBeUndefined();
    const running = { runs: [...yunying.runs!, run("r", "running", 27, { worker_session: "%3" })] };
    expect(calendarActiveRun(running)?.worker_session).toBe("%3");
  });
});

describe("where Calendar Work came from", () => {
  test("reads the item and run from a calendar_action Work only", () => {
    const work = { frontmatter: { kind: "calendar_action", extra: { calendar_item_id: "43754e56", calendar_run_id: "c08f" } } };
    expect(calendarOriginOfWork(work)).toEqual({ itemId: "43754e56", runId: "c08f" });
    expect(calendarOriginOfWork({ frontmatter: { kind: "task", extra: { calendar_item_id: "x" } } })).toBeNull();
  });

  test("labels it with the schedule's name and the run's date", () => {
    expect(calendarOriginLabel({ itemId: "43754e56", runId: "c08f" }, [yunying])?.label).toBe("运营：从今天的工作里挖稿 · Oct 25");
    // Not in Calendar any more: the Work's own title and start date.
    expect(
      calendarOriginLabel({ itemId: "gone" }, [yunying], { title: "每日论文速读", at: "2026-10-27T01:00:00Z" })?.label,
    ).toBe("每日论文速读 · Oct 27");
    expect(calendarOriginLabel({ itemId: "gone" }, [yunying])).toBeNull();
  });
});
