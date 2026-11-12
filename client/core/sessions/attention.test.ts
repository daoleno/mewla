import { describe, expect, test } from "bun:test";
import type { BrainCurrentWork } from "../work/types";
import { brainNeedsYou, sessionsNeedYou } from "./attention";

describe("the needs-you dots", () => {
  test("Sessions: one of the computer's Sessions waits on you", () => {
    expect(sessionsNeedYou([{ needs_attention: false }, {}])).toBe(false);
    expect(sessionsNeedYou([{ needs_attention: false }, { needs_attention: true }])).toBe(true);
  });

  test("Brain: Work that needs you, until it is answered or snoozed", () => {
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
    expect(brainNeedsYou(undefined, now)).toBe(false);
  });

});
