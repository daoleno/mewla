import { describe, expect, test } from "bun:test";
import type { BrainCurrentWork } from "../../store/brain";
import { brainNeedsYou, sessionsNeedYou } from "./primarySessionsAttention";

describe("Sessions needs-you dot", () => {
  test("only the current server's Sessions count", () => {
    const workers = [
      { serverId: "a", needs_attention: false },
      { serverId: "b", needs_attention: true },
    ];
    expect(sessionsNeedYou(workers, "a")).toBe(false);
    expect(sessionsNeedYou(workers, "b")).toBe(true);
    expect(sessionsNeedYou(workers, null)).toBe(false);
  });
});

describe("Brain needs-you dot", () => {
  test("lights for Work that needs you and clears once it is answered or snoozed", () => {
    const question = {
      work_id: "w1",
      title: "Sync fix needs your call",
      status: "waiting",
      progress_mode: "waiting",
      unread_result: true,
      wait_for: "Keep both copies?",
      wake: { kind: "user_input", ref: "Keep both copies?" },
      updated_at: "2026-10-09T00:00:00Z",
    } as unknown as BrainCurrentWork;
    const now = Date.parse("2026-10-09T00:10:00Z");
    expect(brainNeedsYou([question], now)).toBe(true);
    expect(brainNeedsYou([{ ...question, status: "cancelled" }], now)).toBe(false);
    expect(brainNeedsYou([{ ...question, snoozed_until: "2026-10-10T09:00:00Z" }], now)).toBe(false);
    expect(brainNeedsYou([], now)).toBe(false);
    expect(brainNeedsYou(undefined, now)).toBe(false);
  });
});
