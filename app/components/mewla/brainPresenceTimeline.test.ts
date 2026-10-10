import { describe, expect, test } from "bun:test";
import type { BrainWorkResultEvent } from "../brain/brainWorkEvent";
import type { TimelineItem } from "../terminal/InterfaceTimelineItemView";
import { PROVIDER_ACTIVITY_ITEM_PREFIX } from "../terminal/InterfaceTimelineModel";
import {
  BRAIN_PRESENCE_ITEM_PREFIX,
  BRAIN_STEPS_ITEM_PREFIX,
  foldBrainToolRows,
  mergeBrainPresenceIntoTimeline,
} from "./brainPresenceTimeline";

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

const say = { type: "activity", id: "say", title: "Note", tone: "neutral", defaultExpanded: false } as TimelineItem;
const cats = (items: TimelineItem[]) =>
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
    const working = { type: "activity", id: `${PROVIDER_ACTIVITY_ITEM_PREFIX}t1`, title: "Working", tone: "running", defaultExpanded: false } as TimelineItem;
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

  test("a delivered result, delegated Work and the sleeping cat each hold the tail row", () => {
    expect(mergeBrainPresenceIntoTimeline([say], { state: "delivered", count: 1 }).at(-1)?.id).toBe(`${BRAIN_PRESENCE_ITEM_PREFIX}delivered`);
    // Asleep, the cat stays at the newest edge so a tap can ask it how things are.
    const idle = mergeBrainPresenceIntoTimeline([say], { state: "idle" });
    expect(idle.at(-1)).toMatchObject({ id: `${BRAIN_PRESENCE_ITEM_PREFIX}idle`, title: "All quiet" });
    expect(cats(idle)).toBe(1);
  });

  test("a cat away in the Work column leaves no tail row", () => {
    expect(mergeBrainPresenceIntoTimeline([say], { state: "attention", count: 1, workIds: ["w9"], away: true })).toEqual([say]);
  });

  test("a running turn has no tail row; its Working row says Brain's newest step", () => {
    const tool = { type: "activity", id: "t1", title: "Read brainWorkSurface.ts", tone: "success", defaultExpanded: false } as TimelineItem;
    const working = { type: "activity", id: `${PROVIDER_ACTIVITY_ITEM_PREFIX}a1`, title: "Working", tone: "running", defaultExpanded: false } as TimelineItem;
    const merged = mergeBrainPresenceIntoTimeline([say, tool, working], { state: "idle" });
    expect(merged.map((item) => item.id)).toEqual(["say", "t1", working.id]);
    expect(merged.at(-1)).toMatchObject({ detail: "Read brainWorkSurface.ts" });
    // A step from before the user's last message is not this turn's.
    const reply = { type: "message", id: "m", role: "user", body: "go" } as TimelineItem;
    expect(mergeBrainPresenceIntoTimeline([tool, reply, working], undefined).at(-1)).toBe(working);
  });
});

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

  test("an answered question stays its own line between folded steps", () => {
    const answered = { ...tool("q"), title: "Answered", statusKey: "choice:answered" } as TimelineItem;
    const folded = foldBrainToolRows([tool("a"), tool("b"), answered, tool("c"), tool("d")]);
    expect(folded.map((item) => item.id)).toEqual([`${BRAIN_STEPS_ITEM_PREFIX}a`, "q", `${BRAIN_STEPS_ITEM_PREFIX}c`]);
  });
});
