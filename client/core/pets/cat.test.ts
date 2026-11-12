import { describe, expect, test } from "bun:test";
import { catState, deliveredSince } from "./cat";

const base = { kind: "quiet", starting: false, needs: 0, brainTurn: false, running: 0 };

describe("the header cat", () => {
  test("no computer, offline, connecting, needs you, Brain's turn, Workers, quiet, highest first", () => {
    expect(catState({ ...base, kind: "none", needs: 2 })).toBe("homeless");
    for (const kind of ["offline", "pair", "cant_start"]) expect(catState({ ...base, kind, needs: 1, brainTurn: true })).toBe("offline");
    expect(catState({ ...base, kind: "connecting", needs: 1 })).toBe("waking");
    // The tray says "1 needs you" while Brain starts; the cat wakes first.
    expect(catState({ ...base, kind: "needs", starting: true, needs: 1 })).toBe("waking");
    expect(catState({ ...base, kind: "needs", needs: 1, brainTurn: true, running: 2 })).toBe("attention");
    expect(catState({ ...base, kind: "running", brainTurn: true, running: 2 })).toBe("working");
    expect(catState({ ...base, kind: "running", running: 2 })).toBe("delegating");
    expect(catState(base)).toBe("idle");
  });

  test("delivered: a result unread now that was not before; the first look only takes stock", () => {
    expect(deliveredSince(null, new Set(["w1"]))).toBe(false);
    expect(deliveredSince(new Set(), new Set(["w1"]))).toBe(true);
    expect(deliveredSince(new Set(["w1"]), new Set(["w1"]))).toBe(false);
    expect(deliveredSince(new Set(["w1"]), new Set())).toBe(false);
  });
});
