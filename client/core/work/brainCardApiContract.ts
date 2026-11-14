// Run by daemon/brain/card_presentation_bdd_test.go on real emitted, persisted and API-projected
// data: what the client's Brain thread makes of one Work result, live, once accepted, and after a
// reconnect (no cache). Reads the payload as JSON on stdin; fails with an assertion.
import assert from "node:assert/strict";
import { attachBrainWorkEventActions } from "../timeline/model";
import { projectTimeline } from "../timeline/projectTimeline";
import { brainCurrentWorkLifecycle } from "./surface";

const payload = JSON.parse(await Bun.stdin.text());
let cache: ReturnType<typeof projectTimeline>["cache"] | null = null;
for (const phase of ["live", "accepted", "reconnect"] as const) {
  const wire = payload[phase];
  const projection = projectTimeline(wire.events, phase === "reconnect" ? null : cache);
  cache = projection.cache;
  const items = attachBrainWorkEventActions(projection.items, undefined, undefined, wire.current_work);
  const cards = items.filter((item) => item.type === "brain-work-event");
  assert.equal(cards.length, 1, `${phase}: exactly one lineage card`);
  const card = cards[0];
  if (card.type !== "brain-work-event") throw new Error("unreachable");
  assert.equal(card.event.work_id, payload.work_id);
  // What the card shows (WorkEventCard in ui/chat/TimelineRows.tsx): the Work's title and summary.
  const shown = `${card.event.work_title}\n${card.event.summary ?? ""}`;
  assert.ok(!shown.includes("�"), `${phase}: no broken characters on the card`);
  assert.equal(/Offline tests|Replay rows/.test(shown), false, `${phase}: no internal facts on the card`);
  // Where the Work stands: the daemon's current Work when it still lists it, else the result's review state.
  const lifecycle = card.currentWork
    ? brainCurrentWorkLifecycle(card.currentWork).lifecycle
    : card.event.review_state === "reviewing" ? "reviewing" : card.event.review_state === "resolved" ? "done" : "working";
  assert.equal(lifecycle, phase === "live" ? "reviewing" : "done", `${phase}: lifecycle`);
  assert.equal(wire.events.find((event: { id: string }) => event.id === "quoted").body, payload.quoted);
  assert.equal(wire.events.find((event: { id: string }) => event.id === "assistant").body, "整理完成，原始证据保持不变。");
  assert.equal(items.filter((item) => item.type === "message").length, 3);
  assert.equal(wire.events.some((event: { id: string }) => ["internal", "internal-duplicate", "legacy-on-disk"].includes(event.id)), false);
}
console.log("Brain card API/client live, accepted and reconnect contract passed");
