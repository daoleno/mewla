import { describe, expect, test } from "bun:test";
import type { BrainWorkResultEvent } from "../brain/brainWorkEvent";
import type { ZenTimelineItem } from "../terminal/InterfaceTimelineItemView";
import { PROVIDER_ACTIVITY_ITEM_PREFIX } from "../terminal/InterfaceTimelineModel";
import {
  BRAIN_PRESENCE_ITEM_PREFIX,
  BRAIN_STEPS_ITEM_PREFIX,
  foldBrainToolRows,
  mergeBrainPresenceIntoTimeline,
} from "./brainPresenceTimeline";

function slip(id: string, workId: string): ZenTimelineItem {
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

const say = { type: "activity", id: "say", title: "Note", tone: "neutral", defaultExpanded: false } as ZenTimelineItem;
const cats = (items: ZenTimelineItem[]) =>
  items.filter((item) => item.id.startsWith(BRAIN_PRESENCE_ITEM_PREFIX) || (item.type === "brain-work-event" && item.catPerched)).length;

describe("the one cat between turns", () => {
  test("hops onto the newest slip of the Work that needs you, with no tail row", () => {
    const items = [slip("old", "w1"), say, slip("new", "w1"), slip("other", "w2")];
    const merged = mergeBrainPresenceIntoTimeline(items, { state: "attention", count: 1, workIds: ["w1"] });
    expect(merged.map((item) => item.type === "brain-work-event" && item.catPerched ? item.id : null).filter(Boolean)).toEqual(["new"]);
    expect(cats(merged)).toBe(1);
  });

  test("falls back to the tail row when that slip is not in this conversation", () => {
    const merged = mergeBrainPresenceIntoTimeline([say, slip("other", "w2")], { state: "attention", count: 1, workIds: ["w1"] });
    expect(merged.at(-1)?.id).toBe(`${BRAIN_PRESENCE_ITEM_PREFIX}attention`);
    expect(cats(merged)).toBe(1);
  });

  test("a running turn keeps the cat in the Working row", () => {
    const working = { type: "activity", id: `${PROVIDER_ACTIVITY_ITEM_PREFIX}t1`, title: "Working", tone: "running", defaultExpanded: false } as ZenTimelineItem;
    const items = [slip("new", "w1"), working];
    expect(mergeBrainPresenceIntoTimeline(items, { state: "attention", count: 1, workIds: ["w1"] })).toBe(items);
  });

  test("perches on the newest slip among several Works that need you", () => {
    const items = [slip("a", "w1"), say, slip("b", "w2"), slip("c", "w3")];
    const merged = mergeBrainPresenceIntoTimeline(items, { state: "attention", count: 2, workIds: ["w1", "w2"] });
    expect(merged.filter((item) => item.type === "brain-work-event" && item.catPerched).map((item) => item.id)).toEqual(["b"]);
  });

  test("the tail row is presence only: a count, never one Work's title", () => {
    const tail = mergeBrainPresenceIntoTimeline([say], { state: "attention", count: 6, workIds: ["w9"] }).at(-1);
    expect(tail).toMatchObject({ title: "6 need you" });
    expect(tail && "detail" in tail ? tail.detail : undefined).toBeUndefined();
  });

  test("a delivered result and delegated Work keep the tail row; asleep adds nothing", () => {
    expect(mergeBrainPresenceIntoTimeline([say], { state: "delivered", count: 1 }).at(-1)?.id).toBe(`${BRAIN_PRESENCE_ITEM_PREFIX}delivered`);
    expect(mergeBrainPresenceIntoTimeline([say], { state: "idle" })).toEqual([say]);
  });
});

describe("Brain's tool rows", () => {
  const tool = (id: string, tone: "success" | "failed" | "running" = "success") =>
    ({ type: "activity", id, title: `Search ${id}`, tone, icon: "search-outline", defaultExpanded: false }) as ZenTimelineItem;
  const reply = { type: "message", id: "reply", role: "assistant", body: "Done." } as ZenTimelineItem;

  test("a turn's run folds into one Worked row that keeps every step", () => {
    const folded = foldBrainToolRows([tool("a"), tool("b"), tool("c", "failed"), reply, slip("s", "w1")]);
    expect(folded.map((item) => item.id)).toEqual([`${BRAIN_STEPS_ITEM_PREFIX}a`, "reply", "s"]);
    const steps = folded[0];
    expect(steps).toMatchObject({ type: "activity", title: "Worked · 3 steps · 1 failed", tone: "failed", defaultExpanded: false });
    expect(steps.type === "activity" && steps.children?.map((child) => child.id)).toEqual(["a", "b", "c"]);
  });

  test("a single row, a running row and the cat's rows stay as they are", () => {
    const working = { type: "activity", id: `${PROVIDER_ACTIVITY_ITEM_PREFIX}t1`, title: "Working", tone: "running", defaultExpanded: false } as ZenTimelineItem;
    const items = [tool("a"), reply, tool("b"), tool("c", "running"), working];
    expect(foldBrainToolRows(items)).toBe(items);
  });
});
