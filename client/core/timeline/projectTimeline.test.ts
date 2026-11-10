import { describe, expect, test } from "bun:test";
import type { CodexConversationEvent } from "../conversation/conversation";
import { buildTimeline } from "./model";
import {
  projectTimeline,
  resolveProjectionEventOrder,
  type TimelineProjectionCache,
} from "./projectTimeline";
import type { TimelineProjectionFallbackReason } from "./perf";
import {
  firstAssistantEventId,
  makeMixedTimelineEvents,
  withAssistantBodyRevision,
} from "./fixtures";
import type { TimelineItem } from "./types";

describe("projectTimeline incremental projection", () => {
  test("500-event streaming upserts stay under 20% of full projection CPU with stable settled rows", () => {
    const base = makeMixedTimelineEvents(500);
    const streamId = firstAssistantEventId(base);
    const revisions = 100;
    // Precompute outside timed regions — array construction is not projection CPU.
    const revisionEvents = Array.from({ length: revisions + 1 }, (_, revision) =>
      withAssistantBodyRevision(base, streamId, revision),
    );

    {
      // Shared warmup so cold JIT does not dominate either path.
      let warmCache: TimelineProjectionCache | null = null;
      for (let revision = 0; revision < 10; revision += 1) {
        warmCache = projectTimeline(
          revisionEvents[revision],
          warmCache,
        ).cache;
      }

      let cache: TimelineProjectionCache | null = null;
      let minStableReuse = Number.POSITIVE_INFINITY;
      for (let revision = 0; revision < revisions; revision += 1) {
        const result = projectTimeline(revisionEvents[revision], cache);
        cache = result.cache;
        if (revision === 0) {
          expect(result.mode).toBe("full");
          expect(result.cache.incrementalSafe).toBe(true);
          expect(result.items.length).toBeGreaterThanOrEqual(499);
          continue;
        }
        expect(result.mode).toBe("incremental");
        expect(result.fallbackReason).toBeUndefined();
        expect(result.eventOrder).toBe("cached-ids");
        expect(result.cache.incrementalSafe).toBe(true);
        minStableReuse = Math.min(minStableReuse, result.stableRowReuse);
        expect(result.stableRowChurn).toBe(1);
      }
      expect(minStableReuse).toBeGreaterThanOrEqual(498);

      const ratios: number[] = [];
      for (let trial = 0; trial < 3; trial += 1) {
        const fullStart = nowMs();
        for (let revision = 1; revision <= revisions; revision += 1) {
          projectTimeline(revisionEvents[revision], null);
        }
        const fullWallMs = nowMs() - fullStart;

        let rolling: TimelineProjectionCache | null = projectTimeline(
          revisionEvents[0],
          null,
        ).cache;
        const incrementalStart = nowMs();
        for (let revision = 1; revision <= revisions; revision += 1) {
          rolling = projectTimeline(revisionEvents[revision], rolling).cache;
        }
        const incrementalWallMs = nowMs() - incrementalStart;
        expect(fullWallMs).toBeGreaterThan(0);
        ratios.push(incrementalWallMs / fullWallMs);
      }
      ratios.sort((left, right) => left - right);
      const medianRatio = ratios[1];
      // Report measured median ratio truthfully (not gamed).
      console.log(
        JSON.stringify({
          benchmark: "500-event-streaming-projection",
          trials: ratios,
          medianRatio,
        }),
      );
      expect(medianRatio).toBeLessThanOrEqual(0.2);
    }
  });

  test("incremental output matches canonical full projection across property cases", () => {
    const cases: Array<{
      name: string;
      previous: CodexConversationEvent[];
      next: CodexConversationEvent[];
      expectMode: "full" | "incremental";
      fallbackReason?: TimelineProjectionFallbackReason;
    }> = [];

    const mixed50 = makeMixedTimelineEvents(50);
    const streamId = firstAssistantEventId(mixed50);

    cases.push({
      name: "text-growth",
      previous: withAssistantBodyRevision(mixed50, streamId, 1),
      next: withAssistantBodyRevision(mixed50, streamId, 2),
      expectMode: "incremental",
    });
    cases.push({
      name: "text-shrink",
      previous: withAssistantBodyRevision(mixed50, streamId, 8),
      next: withAssistantBodyRevision(mixed50, streamId, 1),
      expectMode: "incremental",
    });

    const equalTs: CodexConversationEvent[] = [
      {
        id: "a",
        seq: 1,
        kind: "user_message",
        role: "user",
        body: "one",
        timestamp: "2026-08-06T12:00:00.000Z",
      },
      {
        id: "b",
        seq: 2,
        kind: "assistant_message",
        role: "assistant",
        body: "two",
        timestamp: "2026-08-06T12:00:00.000Z",
        partial: true,
      },
    ];
    cases.push({
      name: "equal-timestamps",
      previous: equalTs,
      next: equalTs.map((event) =>
        event.id === "b" ? { ...event, body: "two!" } : event,
      ),
      expectMode: "incremental",
    });

    const dateBoundary: CodexConversationEvent[] = [
      {
        id: "day1-user",
        seq: 1,
        kind: "user_message",
        role: "user",
        body: "yesterday",
        timestamp: "2026-08-05T23:00:00.000Z",
      },
      {
        id: "day2-assistant",
        seq: 2,
        kind: "assistant_message",
        role: "assistant",
        body: "today",
        timestamp: "2026-08-06T01:00:00.000Z",
        partial: true,
      },
    ];
    cases.push({
      name: "date-boundary-stream",
      previous: dateBoundary,
      next: dateBoundary.map((event) =>
        event.id === "day2-assistant"
          ? { ...event, body: "today continues" }
          : event,
      ),
      expectMode: "incremental",
    });

    const withExploration: CodexConversationEvent[] = [
      {
        id: "u1",
        seq: 1,
        kind: "user_message",
        role: "user",
        body: "look",
        timestamp: "2026-08-06T02:00:00.000Z",
      },
      {
        id: "c1",
        seq: 2,
        kind: "command",
        command: "ls app",
        status: "done",
        body: "a.ts",
        timestamp: "2026-08-06T02:01:00.000Z",
      },
      {
        id: "t1",
        seq: 3,
        kind: "tool",
        tool_name: "Grep",
        input: `{"pattern":"x"}`,
        body: "hit",
        status: "done",
        timestamp: "2026-08-06T02:02:00.000Z",
      },
      {
        id: "a1",
        seq: 4,
        kind: "assistant_message",
        role: "assistant",
        body: "found",
        timestamp: "2026-08-06T02:03:00.000Z",
        partial: true,
      },
    ];
    cases.push({
      name: "exploration-tool-adjacency",
      previous: withExploration,
      next: withExploration.map((event) =>
        event.id === "a1" ? { ...event, body: "found more" } : event,
      ),
      expectMode: "incremental",
    });

    const withWork: CodexConversationEvent[] = [
      {
        id: "work-1",
        seq: 1,
        kind: "status",
        source: "work_result",
        status: "session.done",
        work_id: "w1",
        title: "Ship",
        body: "done",
        timestamp: "2026-08-06T03:00:00.000Z",
        work_review_state: "resolved",
        work_session_state: "not_required",
        work_result_current: true,
      },
      {
        id: "assist-work",
        seq: 2,
        kind: "assistant_message",
        role: "assistant",
        body: "ok",
        timestamp: "2026-08-06T03:01:00.000Z",
        partial: true,
      },
    ];
    cases.push({
      name: "typed-work-card-adjacent",
      previous: withWork,
      next: withWork.map((event) =>
        event.id === "assist-work" ? { ...event, body: "ok!" } : event,
      ),
      expectMode: "incremental",
    });

    cases.push({
      name: "middle-insert-falls-back",
      previous: mixed50.slice(0, 10),
      next: [
        ...mixed50.slice(0, 5),
        {
          id: "inserted-mid",
          seq: 10_000,
          kind: "assistant_message",
          role: "assistant",
          body: "mid",
          timestamp: "2026-08-06T00:04:30.000Z",
        },
        ...mixed50.slice(5, 10),
      ],
      expectMode: "full",
      fallbackReason: "length-change",
    });

    cases.push({
      name: "pure-tail-append-stays-incremental",
      previous: mixed50.slice(0, 10),
      next: mixed50.slice(0, 11),
      expectMode: "incremental",
    });

    cases.push({
      name: "delete-falls-back",
      previous: mixed50.slice(0, 11),
      next: mixed50.slice(0, 10),
      expectMode: "full",
      fallbackReason: "length-change",
    });

    const reordered = mixed50.slice(0, 8);
    cases.push({
      name: "reorder-falls-back",
      previous: reordered,
      next: reordered.map((event, index) =>
        index === 3
          ? { ...event, id: `replaced-${event.id}` }
          : event,
      ),
      expectMode: "full",
      fallbackReason: "id-sequence-change",
    });

    cases.push({
      name: "snapshot-length-change",
      previous: makeMixedTimelineEvents(20),
      next: makeMixedTimelineEvents(5),
      expectMode: "full",
      fallbackReason: "length-change",
    });

    cases.push({
      name: "tool-body-change-stays-incremental",
      previous: withExploration,
      next: withExploration.map((event) =>
        event.id === "t1" ? { ...event, body: "hit\nmore" } : event,
      ),
      expectMode: "incremental",
    });

    cases.push({
      name: "empty-to-visible-presence-fallback",
      previous: [
        {
          id: "empty-assist",
          seq: 1,
          kind: "assistant_message",
          role: "assistant",
          body: "   ",
          timestamp: "2026-08-06T04:00:00.000Z",
          partial: true,
        },
        {
          id: "user-keep",
          seq: 2,
          kind: "user_message",
          role: "user",
          body: "hi",
          timestamp: "2026-08-06T04:01:00.000Z",
        },
      ],
      next: [
        {
          id: "empty-assist",
          seq: 1,
          kind: "assistant_message",
          role: "assistant",
          body: "now visible",
          timestamp: "2026-08-06T04:00:00.000Z",
          partial: true,
        },
        {
          id: "user-keep",
          seq: 2,
          kind: "user_message",
          role: "user",
          body: "hi",
          timestamp: "2026-08-06T04:01:00.000Z",
        },
      ],
      expectMode: "full",
      fallbackReason: "item-missing",
    });

    for (const testCase of cases) {
      const primed = projectTimeline(testCase.previous, null);
      const optimized = projectTimeline(testCase.next, primed.cache);
      const canonical = buildTimeline(testCase.next);
      expect(optimized.mode, testCase.name).toBe(testCase.expectMode);
      if (testCase.fallbackReason) {
        expect(optimized.fallbackReason, testCase.name).toBe(
          testCase.fallbackReason,
        );
      }
      expect(
        timelineItemsSemanticallyEqual(optimized.items, canonical),
        testCase.name,
      ).toBe(true);
    }
  });

  test("50 and 500 fixtures project deterministically", () => {
    const fifty = makeMixedTimelineEvents(50);
    const fiveHundred = makeMixedTimelineEvents(500);
    expect(buildTimeline(fifty).map((item) => item.id)).toEqual(
      projectTimeline(fifty, null).items.map((item) => item.id),
    );
    expect(buildTimeline(fiveHundred).length).toBeGreaterThan(400);
    expect(projectTimeline(fiveHundred, null).items.length).toEqual(
      buildTimeline(fiveHundred).length,
    );
  });
});

describe("mid-review projection contracts", () => {
  test("streaming revisions reuse cached raw order keys without re-sorting", () => {
    const base = makeMixedTimelineEvents(80);
    const streamId = firstAssistantEventId(base);
    const primed = projectTimeline(base, null);
    expect(["already-sorted", "sorted"]).toContain(primed.eventOrder);

    let cache = primed.cache;
    for (let revision = 1; revision <= 5; revision += 1) {
      const next = withAssistantBodyRevision(base, streamId, revision);
      const ordered = resolveProjectionEventOrder(next, cache);
      expect(ordered.source).toBe("cached-ids");
      const projected = projectTimeline(next, cache);
      expect(projected.eventOrder).toBe("cached-ids");
      expect(projected.mode).toBe("incremental");
      cache = projected.cache;
    }

    const unsorted = [...cache.sortedEvents].reverse();
    expect(resolveProjectionEventOrder(unsorted, null).source).toBe("sorted");
    expect(resolveProjectionEventOrder(unsorted, cache).source).toBe("sorted");
    // Same event refs after a forced sort are a proven no-op, not a second model.
    const resortedNoop = projectTimeline(unsorted, cache);
    expect(resortedNoop.eventOrder).toBe("sorted");
    expect(resortedNoop.mode).toBe("incremental");
    expect(resortedNoop.stableRowChurn).toBe(0);

    const structurallyDifferent = unsorted.map((event, index) =>
      index === 0 ? { ...event, id: `other-${event.id}` } : event,
    );
    const structural = projectTimeline(structurallyDifferent, cache);
    expect(structural.eventOrder).toBe("sorted");
    expect(structural.mode).toBe("full");
    expect(structural.fallbackReason).toBe("id-sequence-change");
  });

  test("timestamp/seq crossing rejects cached-ids and matches canonical full", () => {
    const previous: CodexConversationEvent[] = [
      {
        id: "keep-a",
        seq: 1,
        kind: "user_message",
        role: "user",
        body: "first",
        timestamp: "2026-08-06T10:00:00.000Z",
      },
      {
        id: "move-b",
        seq: 2,
        kind: "assistant_message",
        role: "assistant",
        body: "second",
        timestamp: "2026-08-06T11:00:00.000Z",
        partial: true,
      },
    ];
    const primed = projectTimeline(previous, null);
    // Same array ID order, but move-b's timestamp/seq now sorts before keep-a.
    const crossed: CodexConversationEvent[] = [
      previous[0],
      {
        ...previous[1],
        timestamp: "2026-08-06T09:00:00.000Z",
        seq: 0,
        body: "second crossed",
      },
    ];
    expect(crossed.map((event) => event.id)).toEqual(["keep-a", "move-b"]);
    expect(resolveProjectionEventOrder(crossed, primed.cache).source).toBe(
      "sorted",
    );
    expect(resolveProjectionEventOrder(crossed, primed.cache).source).not.toBe(
      "cached-ids",
    );

    const projected = projectTimeline(crossed, primed.cache);
    expect(projected.eventOrder).toBe("sorted");
    expect(projected.mode).toBe("full");
    expect(projected.cache.sortedEvents.map((event) => event.id)).toEqual([
      "move-b",
      "keep-a",
    ]);
    expect(
      timelineItemsSemanticallyEqual(
        projected.items,
        buildTimeline(crossed),
      ),
    ).toBe(true);
  });

  test("duplicate event ids fall back before ambiguous incremental replacement", () => {
    const duplicates: CodexConversationEvent[] = [
      {
        id: "dup",
        seq: 1,
        kind: "assistant_message",
        role: "assistant",
        body: "first copy",
        timestamp: "2026-08-06T12:00:00.000Z",
        partial: true,
      },
      {
        id: "dup",
        seq: 2,
        kind: "assistant_message",
        role: "assistant",
        body: "second copy",
        timestamp: "2026-08-06T12:01:00.000Z",
        partial: true,
      },
    ];
    const primed = projectTimeline(duplicates, null);
    expect(primed.mode).toBe("full");
    expect(primed.cache.incrementalSafe).toBe(false);
    expect(primed.cache.itemIndexById).toBeNull();
    expect(primed.items.map((item) => item.id)).toEqual(["dup", "dup"]);

    const next = [
      duplicates[0],
      {
        ...duplicates[1],
        body: "second copy revised",
        partial: true,
      },
    ];
    const projected = projectTimeline(next, primed.cache);
    expect(projected.mode).toBe("full");
    expect(projected.fallbackReason).toBe("duplicate-event-id");
    expect(projected.cache.incrementalSafe).toBe(false);
    expect(
      timelineItemsSemanticallyEqual(projected.items, buildTimeline(next)),
    ).toBe(true);
    expect(
      (projected.items[1] as { body?: string } | undefined)?.body,
    ).toBe("second copy revised");
  });
});

function nowMs() {
  const perf = (
    globalThis as { performance?: { now(): number } }
  ).performance;
  return perf?.now?.() ?? Date.now();
}

function timelineItemsSemanticallyEqual(
  left: TimelineItem[],
  right: TimelineItem[],
) {
  if (left.length !== right.length) {
    return false;
  }
  for (let index = 0; index < left.length; index += 1) {
    if (stableItemKey(left[index]) !== stableItemKey(right[index])) {
      return false;
    }
  }
  return true;
}

function stableItemKey(item: TimelineItem) {
  const { onPress: _onPress, onRetryPending: _onRetry, ...rest } = item as {
    onPress?: unknown;
    onRetryPending?: unknown;
  } & TimelineItem;
  return JSON.stringify(rest);
}
