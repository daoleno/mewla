import { describe, expect, test } from "bun:test";
import type { CodexConversationEvent } from "../conversation/conversation";
import {
  buildTimelineRenderItems,
  stabilizeTimelineRenderItems,
  type TimelineRenderItem,
} from "./grouping";
import type { TimelineItem } from "./types";
import { projectTimeline } from "./projectTimeline";
import { makeMixedTimelineEvents } from "./fixtures";

/**
 * Distinguishes four invalidation layers on a long mixed timeline when a new
 * tool/activity row arrives:
 * 1. raw TimelineItem identity from projectTimeline
 * 2. grouped TimelineRenderItem identity after stabilize
 * 3. row memo props (item + presentation references)
 * 4. FlatList data-array identity (new array allowed on length change)
 *
 * Historical rows must keep (1)-(3). Only the new/active tool row and
 * legitimate adjacent message-grouping boundaries may change.
 */

const NEW_TOOL_ID = "tool-append-live";

function appendRunningTool(
  events: CodexConversationEvent[],
): CodexConversationEvent[] {
  const last = events[events.length - 1];
  const lastMs = last?.timestamp
    ? Date.parse(last.timestamp)
    : Date.parse("2026-08-06T00:00:00.000Z");
  return [
    ...events,
    {
      id: NEW_TOOL_ID,
      seq: (last?.seq ?? events.length) + 1,
      kind: "tool",
      tool_name: "Grep",
      input: `{"pattern":"live-append","path":"app"}`,
      body: "live match",
      status: "running",
      partial: true,
      timestamp: new Date(lastMs + 60_000).toISOString(),
    },
  ];
}

function upsertToolBody(
  events: CodexConversationEvent[],
  toolId: string,
  body: string,
): CodexConversationEvent[] {
  return events.map((event) =>
    event.id === toolId
      ? { ...event, body, status: "running", partial: true }
      : event,
  );
}

function historicalIds(items: { id: string }[], activeId: string) {
  return items.map((item) => item.id).filter((id) => id !== activeId);
}

function indexById<T extends { id: string }>(items: T[]) {
  return new Map(items.map((item) => [item.id, item]));
}

function memoPropsOf(item: TimelineRenderItem) {
  if (item.type === "date-divider") {
    return { item, presentation: undefined };
  }
  return {
    item: item.type === "message" ? (item.sourceItem ?? item) : item,
    presentation: item.type === "message" ? item.presentation : undefined,
  };
}

describe("tool/activity append timeline stability layers", () => {
  test("same-id tool activity upsert preserves unrelated historical raw identities", () => {
    const baseEvents = makeMixedTimelineEvents(120);
    const toolEvent = baseEvents.find((event) => event.kind === "tool");
    if (!toolEvent) {
      throw new Error("fixture requires a tool event");
    }
    const initial = projectTimeline(baseEvents, null);
    const nextEvents = upsertToolBody(
      baseEvents,
      toolEvent.id,
      `${toolEvent.body}\nsecond match`,
    );
    const projected = projectTimeline(nextEvents, initial.cache);

    expect(projected.mode).toBe("incremental");
    expect(projected.fallbackReason).toBeUndefined();
    expect(projected.stableRowChurn).toBe(1);

    const previousRaw = indexById(initial.items);
    for (const item of projected.items) {
      if (item.id === toolEvent.id) {
        expect(item).not.toBe(previousRaw.get(item.id));
        continue;
      }
      const previous = previousRaw.get(item.id);
      if (!previous) {
        throw new Error(`missing previous raw row ${item.id}`);
      }
      expect(item).toBe(previous);
    }
  });
});
