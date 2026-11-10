import { describe, expect, test } from "bun:test";
import type { CodexConversationEvent } from "../conversation/conversation";
import { SCREENSHOT_CHAT_ACTIVITY_EVENTS } from "./chatActivityFixtures";
import {
  createToolRunFolder,
  isFoldableToolRow,
  summarizeToolRun,
} from "./foldToolRuns";
import { shouldAutoExpandActivity } from "./activityModel";
import type { ActivityTimelineItem } from "./activityTypes";
import type { TimelineItem } from "./types";
import { buildTimelineFromSortedEvents } from "./model";
import { timelineItemsSemanticEqual } from "./timelineItemsSemanticEqual";

function row(
  id: string,
  semanticKind: ActivityTimelineItem["semanticKind"],
  extra: Partial<ActivityTimelineItem> = {},
): ActivityTimelineItem {
  return {
    type: "activity",
    id,
    title: id,
    tone: "success",
    icon: "terminal",
    statusKey: "done",
    semanticKind,
    ...extra,
  };
}

const say = (id: string): TimelineItem => ({
  type: "message",
  id,
  role: "assistant",
  body: id,
  attachments: [],
});

describe("folding a turn's finished tool rows", () => {
  test("folds two or more finished rows, leaves a single row alone", () => {
    const fold = createToolRunFolder();
    const one = [say("a"), row("r1", "read_files"), say("b")];
    expect(fold(one)).toBe(one);

    const items = [
      say("a"),
      row("r1", "read_files", { files: ["a.ts"] }),
      row("r2", "run_command"),
      say("b"),
    ];
    const out = fold(items);
    expect(out.map((item) => item.id)).toEqual([
      "a",
      "r1",
      "b",
    ]);
    const group = out[1] as ActivityTimelineItem;
    expect(group.steps?.map((step) => step.id)).toEqual(["r1", "r2"]);
    expect(group.title).toBe("Ran 1 command and read 1 file");
    expect(group.detail).toBe("2 steps");
    expect(group.defaultExpanded).toBe(false);
  });

  test("never crosses a turn: words, plans and notices end a run", () => {
    const fold = createToolRunFolder();
    const out = fold([
      row("a1", "read_files"),
      row("a2", "read_files"),
      say("between"),
      row("b1", "run_command"),
      row("b2", "run_command"),
    ]);
    expect(out.map((item) => item.id)).toEqual([
      "a1",
      "between",
      "b1",
    ]);
  });

  test("a failed row is an ordinary finished row: collapsed and folded into the run", () => {
    const failed = row("failed", "run_command", {
      tone: "failed",
      detail: "Failed · exit 1",
    });
    expect(isFoldableToolRow(failed)).toBe(true);
    expect(shouldAutoExpandActivity(failed)).toBe(false);
    const out = createToolRunFolder()([
      row("a", "read_files"),
      failed,
      row("c", "read_files"),
    ]);
    expect(out.map((item) => item.id)).toEqual(["a"]);
    const group = out[0] as ActivityTimelineItem;
    expect(group.steps?.map((step) => step.id)).toEqual(["a", "failed", "c"]);
    expect(group.detail).toBe("3 steps · 1 failed");
    // Quiet like any fold: the count says it, the title is not painted.
    expect(group.tone).toBe("neutral");
    expect(group.defaultExpanded).toBe(false);
    expect(shouldAutoExpandActivity(group)).toBe(false);
    // The failed step inside the opened run starts collapsed too.
    expect(shouldAutoExpandActivity(group.steps![1])).toBe(false);
  });

  test("a run with no failure keeps the plain step count", () => {
    const out = createToolRunFolder()([
      row("a", "read_files"),
      row("b", "read_files"),
    ]);
    const group = out[0] as ActivityTimelineItem;
    expect(group.detail).toBe("2 steps");
    expect(group.tone).toBe("neutral");
  });

  test("a running, blocked, streaming or self-opening row stays visible and splits the run", () => {
    const kept = [
      row("running", "run_command", { tone: "running" }),
      row("failed", "run_command", { tone: "failed", defaultExpanded: true }),
      row("blocked", "run_command", { detail: "Blocked · needs approval" }),
      row("streaming", "run_command", { streaming: true }),
      row("open", "run_command", { defaultExpanded: true }),
    ];
    expect(
      isFoldableToolRow(row("passed", "test_app", { detail: "Passed · 2s" })),
    ).toBe(true);
    for (const item of kept) {
      expect(isFoldableToolRow(item)).toBe(false);
    }
    const out = createToolRunFolder()([
      row("a", "read_files"),
      row("b", "read_files"),
      kept[1],
      row("c", "read_files"),
      row("d", "read_files"),
      kept[0],
    ]);
    expect(out.map((item) => item.id)).toEqual([
      "a",
      "failed",
      "c",
      "running",
    ]);
  });

  test("a subagent row stays on its own and splits the run", () => {
    const out = createToolRunFolder()([
      row("a", "read_files"),
      row("b", "read_files"),
      row("agent", "delegate_agent"),
      row("c", "run_command"),
      row("d", "run_command"),
    ]);
    expect(out.map((item) => item.id)).toEqual([
      "a",
      "agent",
      "c",
    ]);
  });

  test("rows without a kind, the agent's words and the choice answer are not folded", () => {
    expect(isFoldableToolRow(row("x", undefined))).toBe(false);
    expect(
      isFoldableToolRow(row("x", "use_tool", { activityKind: "update" })),
    ).toBe(false);
    expect(
      isFoldableToolRow(row("x", "use_tool", { statusKey: "choice:abc" })),
    ).toBe(false);
  });

  test("a lone row that becomes a run keeps its row id", () => {
    const fold = createToolRunFolder();
    const first = row("r1", "read_files");
    const alone = fold([say("m"), first]);
    expect(alone.map((item) => item.id)).toEqual(["m", "r1"]);
    const grown = fold([say("m"), first, row("r2", "run_command")]);
    expect(grown.map((item) => item.id)).toEqual(["m", "r1"]);
    expect((grown[1] as ActivityTimelineItem).steps).toHaveLength(2);
  });

  test("a run that grows keeps its row key; untouched runs keep their object", () => {
    const fold = createToolRunFolder();
    const first = row("r1", "read_files");
    const second = row("r2", "read_files");
    const earlier = [row("e1", "run_command"), row("e2", "run_command"), say("m")];
    const before = fold([...earlier, first, second]);
    const earlierGroup = before[0];
    const after = fold([...earlier, first, second, row("r3", "search_code")]);
    expect(after[0]).toBe(earlierGroup);
    expect(after.map((item) => item.id)).toEqual([
      "e1",
      "m",
      "r1",
    ]);
    expect((after[2] as ActivityTimelineItem).steps).toHaveLength(3);
    expect(timelineItemsSemanticEqual(before[2], after[2])).toBe(false);
    expect(timelineItemsSemanticEqual(after[2], after[2])).toBe(true);
  });
});

describe("summarising a run", () => {
  test("counts unique files for reads and edits, rows for the rest", () => {
    const title = summarizeToolRun([
      row("a", "read_files", { files: ["x.ts", "y.ts"] }),
      row("b", "read_files", { files: ["x.ts"] }),
      row("c", "update_files", { files: ["x.ts"] }),
      row("d", "run_command"),
      row("e", "run_command"),
      row("f", "run_command"),
    ]);
    expect(title).toBe("Edited 1 file and ran 3 commands");
  });

  test("names the two most consequential kinds (tests count as commands)", () => {
    expect(
      summarizeToolRun([
        row("a", "update_files"),
        row("b", "run_command"),
        row("c", "test_app"),
        row("d", "read_files"),
        row("e", "search_code"),
        row("f", "use_tool"),
      ]),
    ).toBe("Edited 1 file and ran 2 commands");
  });

  test("an explore row counts every lookup it holds", () => {
    expect(
      summarizeToolRun([
        row("explore:1", "search_code", {
          children: [
            { id: "c1", title: "Search a" },
            { id: "c2", title: "Search b" },
          ],
        }),
        row("b", "search_code"),
      ]),
    ).toBe("Searched code 3 times");
  });
});

describe("the activity demo fixture", () => {
  test("folds the finished exploration, edit, test and failed rows; the live row stays", () => {
    const items = buildTimelineFromSortedEvents(
      SCREENSHOT_CHAT_ACTIVITY_EVENTS as CodexConversationEvent[],
    );
    const out = createToolRunFolder()(items);
    const folded = out.filter(
      (item) => item.type === "activity" && item.steps?.length,
    ) as ActivityTimelineItem[];
    expect(folded).toHaveLength(1);
    expect(folded[0].steps?.map((step) => step.id)).toContain("activity-failed");
    expect(folded[0].detail).toBe("5 steps · 1 failed");
    expect(
      out.some((item) => item.type === "activity" && item.id === "activity-failed"),
    ).toBe(false);
    const failed = folded[0].steps?.find((step) => step.id === "activity-failed");
    expect(failed && shouldAutoExpandActivity(failed)).toBe(false);
    const running = out.find(
      (item) => item.type === "activity" && item.tone === "running",
    );
    expect(running).toBeDefined();
  });
});
