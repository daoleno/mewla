import { describe, expect, test } from "bun:test";
import {
  conversationSubagentsEqual,
  normalizeCodexConversation,
  type CodexConversationEvent,
} from "../conversation/conversation";
import { SCREENSHOT_SUBAGENT_WORK_EVENTS } from "./subagentFixtures";
import { createToolRunFolder } from "./foldToolRuns";
import type { ActivityTimelineItem } from "./activityTypes";
import { splitActivityStatusDetail } from "./activityModel";
import {
  buildTimelineFromSortedEvents,
  formatSubagentDuration,
} from "./model";
import { projectTimeline } from "./projectTimeline";
import { timelineItemsSemanticEqual } from "./timelineItemsSemanticEqual";

const events = SCREENSHOT_SUBAGENT_WORK_EVENTS as CodexConversationEvent[];

function delegates(items: ReturnType<typeof buildTimelineFromSortedEvents>) {
  return items.filter(
    (item): item is ActivityTimelineItem =>
      item.type === "activity" && item.semanticKind === "delegate_agent",
  );
}

describe("a subagent's own steps on its delegating row", () => {
  test("the finished subagent carries its steps, their count and how long it took", () => {
    const [finished] = delegates(buildTimelineFromSortedEvents(events));
    expect(finished.title).toBe("Audit the sync module");
    expect(finished.steps?.map((step) => step.id)).toEqual([
      "sub-1-read",
      "sub-1-read-2",
      "sub-1-grep",
      "sub-1-test",
      "sub-1-read-3",
      "sub-1-read-4",
    ]);
    expect(finished.detail).toBe("6 steps · 2m 14s");
    expect(finished.tone).toBe("success");
    expect(splitActivityStatusDetail(finished.detail, finished.tone)).toEqual({
      mark: null,
      rest: "6 steps · 2m 14s",
    });
  });

  test("a subagent still at work keeps its row running, with no duration yet", () => {
    const [, working] = delegates(buildTimelineFromSortedEvents(events));
    expect(working.tone).toBe("running");
    expect(working.icon).toBe("time");
    expect(working.detail).toBe("2 steps");
    expect(splitActivityStatusDetail(working.detail, working.tone).mark).toBe("running");
    expect(working.steps?.map((step) => step.tone)).toEqual([
      "success",
      "running",
    ]);
  });

  test("a delegating call with no subagent transcript is the row it was", () => {
    const plain = events.map(({ subagent: _drop, ...event }) => event);
    for (const row of delegates(buildTimelineFromSortedEvents(plain))) {
      expect(row.steps).toBeUndefined();
    }
  });

  test("durations read as whole seconds, minutes and hours", () => {
    const at = (seconds: number) =>
      formatSubagentDuration(
        "2026-10-17T10:00:00.000Z",
        new Date(Date.parse("2026-10-17T10:00:00.000Z") + seconds * 1000).toISOString(),
      );
    expect(at(0.4)).toBeUndefined();
    expect(at(45)).toBe("45s");
    expect(at(120)).toBe("2m");
    expect(at(134)).toBe("2m 14s");
    expect(at(3780)).toBe("1h 3m");
    expect(formatSubagentDuration(undefined, "x")).toBeUndefined();
  });
});

describe("folding a subagent's steps", () => {
  const folded = () => createToolRunFolder()(buildTimelineFromSortedEvents(events));

  test("its finished steps fold like a turn's, the failed one inside the run", () => {
    const [finished] = delegates(folded());
    const steps = finished.steps ?? [];
    expect(steps.map((step) => step.id)).toEqual(["sub-1-read"]);
    expect(steps[0].steps?.map((step) => step.id)).toEqual([
      "sub-1-read",
      "sub-1-read-2",
      "sub-1-grep",
      "sub-1-test",
      "sub-1-read-3",
      "sub-1-read-4",
    ]);
    expect(steps[0].detail).toBe("6 steps · 1 failed");
    expect(steps[0].defaultExpanded).toBe(false);
    expect(steps[0].steps?.[3].tone).toBe("failed");
  });

  test("the subagent row is never folded into its neighbours", () => {
    const out = folded();
    expect(delegates(out)).toHaveLength(2);
    expect(delegates(out)[0].semanticKind).toBe("delegate_agent");
  });

  test("an untouched subagent keeps its row object; a rebuilt one folds to an equal row", () => {
    const fold = createToolRunFolder();
    const items = buildTimelineFromSortedEvents(events);
    const first = fold(items);
    expect(delegates(fold(items))[0]).toBe(delegates(first)[0]);

    const again = fold(buildTimelineFromSortedEvents(events));
    const [before] = delegates(first);
    const [after] = delegates(again);
    expect(after).not.toBe(before);
    expect(
      timelineItemsSemanticEqual(before.steps![0], after.steps![0]),
    ).toBe(true);
    expect(timelineItemsSemanticEqual(before, after)).toBe(true);
  });

  test("a new step changes what the row says", () => {
    const grown = events.map((event) =>
      event.id === "subagent-agent" && event.subagent
        ? {
            ...event,
            subagent: {
              ...event.subagent,
              steps: [
                ...event.subagent.steps,
                { ...event.subagent.steps[0], id: "sub-2-more", seq: 3 },
              ],
            },
          }
        : event,
    );
    const [, before] = delegates(createToolRunFolder()(buildTimelineFromSortedEvents(events)));
    const [, after] = delegates(createToolRunFolder()(buildTimelineFromSortedEvents(grown)));
    expect(timelineItemsSemanticEqual(before, after)).toBe(false);
    expect(after.detail).toBe("3 steps");
  });
});

describe("the subagent on the wire", () => {
  test("survives normalisation, nested steps included", () => {
    const conversation = normalizeCodexConversation({
      available: true,
      events: JSON.parse(JSON.stringify(events)),
    });
    const task = conversation.events.find((event) => event.id === "subagent-task");
    expect(task?.subagent?.finished).toBe(true);
    expect(task?.subagent?.steps).toHaveLength(6);
    expect(task?.subagent?.steps[3].status).toBe("failed");
    const again = normalizeCodexConversation({
      available: true,
      events: JSON.parse(JSON.stringify(events)),
    });
    expect(
      conversationSubagentsEqual(
        task?.subagent,
        again.events.find((event) => event.id === "subagent-task")?.subagent,
      ),
    ).toBe(true);
    expect(conversationSubagentsEqual(task?.subagent, undefined)).toBe(false);
  });

  test("steps that change reach the timeline through an incremental projection", () => {
    const first = projectTimeline(events, null);
    const finishedLater = events.map((event) =>
      event.id === "subagent-agent" && event.subagent
        ? {
            ...event,
            status: "completed",
            subagent: {
              ...event.subagent,
              finished: true,
              ended_at: "2026-10-17T10:03:00.000Z",
              steps: event.subagent.steps.map((step) => ({
                ...step,
                status: "completed",
              })),
            },
          }
        : event,
    );
    const second = projectTimeline(finishedLater, first.cache);
    const [, row] = delegates(second.items);
    expect(row.tone).toBe("success");
    expect(row.detail).toBe("2 steps · 2m");
  });
});
