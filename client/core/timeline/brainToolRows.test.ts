import { describe, expect, test } from "bun:test";
import type { BrainWorkResultEvent } from "./brainWorkEvent";
import type { TimelineItem } from "./types";
import { PROVIDER_ACTIVITY_ITEM_PREFIX } from "./model";
import {
  BRAIN_STEPS_ITEM_PREFIX,
  foldBrainToolRows,
} from "./brainToolRows";

function slip(id: string, workId: string): TimelineItem {
  const event: BrainWorkResultEvent = {
    event_id: id,
    kind: "session.needs_input",
    work_id: workId,
    work_title: "Sync fix",
    summary: "Keep both copies, or the newest edit?",
    occurred_at: "2026-10-07T09:00:00Z",
    unread: true,
    review_state: "queued",
    session_state: "open",
    current_result: true,
    attention: "user_input",
  };
  return { type: "brain-work-event", id, timestamp: event.occurred_at, event, events: [event] };
}

describe("Brain's tool rows", () => {
  const tool = (id: string, tone: "success" | "failed" | "running" = "success") =>
    ({ type: "activity", id, title: `Search ${id}`, tone, icon: "search", defaultExpanded: false }) as TimelineItem;
  const reply = { type: "message", id: "reply", role: "assistant", body: "Done." } as TimelineItem;

  test("a turn's run folds into one Worked row that keeps every step", () => {
    const folded = foldBrainToolRows([tool("a"), tool("b"), tool("c", "failed"), reply, slip("s", "w1")]);
    expect(folded.map((item) => item.id)).toEqual([`${BRAIN_STEPS_ITEM_PREFIX}a`, "reply", "s"]);
    const steps = folded[0];
    expect(steps).toMatchObject({ type: "activity", title: "Worked · 3 steps · 1 failed", tone: "failed", defaultExpanded: false });
    expect(steps.type === "activity" && steps.children?.map((child) => child.id)).toEqual(["a", "b", "c"]);
  });

  test("a single row, a running row and the cat's rows stay as they are", () => {
    const working = { type: "activity", id: `${PROVIDER_ACTIVITY_ITEM_PREFIX}t1`, title: "Working", tone: "running", defaultExpanded: false } as TimelineItem;
    const items = [tool("a"), reply, tool("b"), tool("c", "running"), working];
    expect(foldBrainToolRows(items)).toBe(items);
  });

  test("Brain's update line stays visible between folded steps", () => {
    const update = { type: "activity", id: "u", title: "Quota is now 256.", body: "Quota is now 256.", tone: "neutral", icon: "bulb", activityKind: "update" } as TimelineItem;
    const folded = foldBrainToolRows([tool("a"), tool("b"), update, tool("c"), tool("d"), reply]);
    expect(folded.map((item) => item.id)).toEqual([`${BRAIN_STEPS_ITEM_PREFIX}a`, "u", `${BRAIN_STEPS_ITEM_PREFIX}c`, "reply"]);
  });

  test("an answered question stays its own line between folded steps", () => {
    const answered = { ...tool("q"), title: "Answered", statusKey: "choice:answered" } as TimelineItem;
    const folded = foldBrainToolRows([tool("a"), tool("b"), answered, tool("c"), tool("d")]);
    expect(folded.map((item) => item.id)).toEqual([`${BRAIN_STEPS_ITEM_PREFIX}a`, "q", `${BRAIN_STEPS_ITEM_PREFIX}c`]);
  });
});
