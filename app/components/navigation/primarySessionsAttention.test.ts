import { describe, expect, test } from "bun:test";
import { sessionsNeedYou } from "./primarySessionsAttention";

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
