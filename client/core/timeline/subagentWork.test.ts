import { describe, expect, test } from "bun:test";
import {
  conversationSubagentsEqual,
  normalizeCodexConversation,
  type CodexConversationEvent,
} from "../conversation/conversation";
import { SCREENSHOT_SUBAGENT_WORK_EVENTS } from "./subagentFixtures";
import type { ActivityTimelineItem } from "./activityTypes";
import {
  buildTimelineFromSortedEvents,
  formatSubagentDuration,
} from "./model";
import { projectTimeline } from "./projectTimeline";

const events = SCREENSHOT_SUBAGENT_WORK_EVENTS as CodexConversationEvent[];

function delegates(items: ReturnType<typeof buildTimelineFromSortedEvents>) {
  return items.filter(
    (item): item is ActivityTimelineItem =>
      item.type === "activity" && item.semanticKind === "delegate_agent",
  );
}

describe("a subagent's own steps on its delegating row", () => {
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
