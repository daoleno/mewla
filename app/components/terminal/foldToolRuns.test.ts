import { describe, expect, test } from "bun:test";
import type { CodexConversationEvent } from "../../services/codexConversation";
import { SCREENSHOT_CHAT_ACTIVITY_EVENTS } from "../../services/screenshotDemoFixtures";
import {
  TOOL_RUN_ITEM_PREFIX,
  createToolRunFolder,
  isFoldableToolRow,
  summarizeToolRun,
} from "./foldToolRuns";
import type { ActivityTimelineItem } from "./InterfaceTimelineActivityTypes";
import type { TimelineItem } from "./InterfaceTimelineItemView";
import { buildTimelineFromSortedEvents } from "./InterfaceTimelineModel";
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
      `${TOOL_RUN_ITEM_PREFIX}r1`,
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
      `${TOOL_RUN_ITEM_PREFIX}a1`,
      "between",
      `${TOOL_RUN_ITEM_PREFIX}b1`,
    ]);
  });

  test("a running, failed, blocked, streaming or self-opening row stays visible and splits the run", () => {
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
      `${TOOL_RUN_ITEM_PREFIX}a`,
      "failed",
      `${TOOL_RUN_ITEM_PREFIX}c`,
      "running",
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
      `${TOOL_RUN_ITEM_PREFIX}e1`,
      "m",
      `${TOOL_RUN_ITEM_PREFIX}r1`,
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
  test("folds the finished exploration, edit and test rows; the failure and the live row stay", () => {
    const items = buildTimelineFromSortedEvents(
      SCREENSHOT_CHAT_ACTIVITY_EVENTS as CodexConversationEvent[],
    );
    const out = createToolRunFolder()(items);
    const activityIds = out
      .filter((item) => item.type === "activity")
      .map((item) => item.id);
    expect(activityIds.some((id) => id.startsWith(TOOL_RUN_ITEM_PREFIX))).toBe(
      true,
    );
    const failed = out.find(
      (item) => item.type === "activity" && item.tone === "failed",
    );
    expect(failed).toBeDefined();
    expect(failed?.id.startsWith(TOOL_RUN_ITEM_PREFIX)).toBe(false);
  });
});
