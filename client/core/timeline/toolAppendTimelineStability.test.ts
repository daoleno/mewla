import { describe, expect, test } from "bun:test";
import type { CodexConversationEvent } from "../conversation/conversation";
import type { TimelineItem } from "./types";
import { projectTimeline } from "./projectTimeline";
import { makeMixedTimelineEvents } from "./fixtures";
import { timelineItemsSemanticEqual } from "./timelineItemsSemanticEqual";

/**
 * When a new tool/activity row arrives on a long mixed timeline, the
 * TimelineItems projectTimeline hands back for historical rows keep their
 * identity; only the new/active tool row changes.
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

describe("tool/activity append timeline stability layers", () => {
  test("long mixed timeline: new tool preserves historical raw identities", () => {
    const baseEvents = makeMixedTimelineEvents(120);
    const initial = projectTimeline(baseEvents, null);
    expect(initial.mode).toBe("full");
    expect(initial.items.length).toBeGreaterThanOrEqual(110);

    const nextEvents = appendRunningTool(baseEvents);
    const projected = projectTimeline(nextEvents, initial.cache);

    // The authoritative projection keeps historical raw item refs.
    expect(projected.mode).toBe("incremental");
    expect(projected.fallbackReason).toBeUndefined();
    const previousRaw = indexById(initial.items);
    const historicalRawIds = historicalIds(projected.items, NEW_TOOL_ID);
    expect(historicalRawIds.length).toBe(initial.items.length);
    for (const id of historicalRawIds) {
      expect(projected.items.find((item) => item.id === id)).toBe(
        previousRaw.get(id),
      );
    }
    expect(projected.items.some((item) => item.id === NEW_TOOL_ID)).toBe(true);
    expect(projected.stableRowChurn).toBeLessThanOrEqual(2);
    expect(projected.stableRowReuse).toBeGreaterThanOrEqual(
      initial.items.length - 1,
    );

    // Salvage mirror of useInterfaceTimelineItems (should be identity no-op when
    // projection already preserved refs).
    const salvaged: TimelineItem[] = projected.items.map((item) => {
      const prior = previousRaw.get(item.id);
      return prior && timelineItemsSemanticEqual(prior, item) ? prior : item;
    });
    for (const id of historicalRawIds) {
      expect(salvaged.find((item) => item.id === id)).toBe(previousRaw.get(id));
    }
  });

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
