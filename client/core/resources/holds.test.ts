import { describe, expect, test } from "bun:test";
import { baseName, growthText, holdKindLabel, holdOwnerTitle, normalizeHolds } from "./holds";
import { formatBytes, normalizeResourceTelemetry } from "./resourceTelemetry";

const wire = {
  day: "2026-11-24",
  kinds: [
    { kind: "scratch", count: 3, bytes: 3_000_000, oldest: "2026-11-20T10:00:00Z", growth_7d: 1_000_000 },
    { kind: "trash", count: 1, bytes: 300 },
    { kind: "", count: 9 },
  ],
  owners: [{ unit: "mewla-worker-o-t.scope", session_id: "mh-1", work_id: "w-1", work_title: "Release", bytes: 2_000_000 }, { bytes: 5 }],
  kept: [{ id: "k1", path: "/s/t/x/out", session_id: "mh-1", until: "2026-11-26T15:59:59Z", why: "the gate reads it" }],
  collected_today: { items: 2, bytes: 400, restorable: [{ id: "20261124-1-old", kind: "unmarked", original: "/s/t/old", bytes: 300, reason: "unmarked", at: "2026-11-24T01:00:00Z" }] },
};

describe("holds", () => {
  test("reads the daemon's summary and drops what it cannot name", () => {
    const holds = normalizeHolds(wire)!;
    expect(holds.day).toBe("2026-11-24");
    expect(holds.kinds.map((k) => k.kind)).toEqual(["scratch", "trash"]);
    expect(holds.kinds[0].growth7d).toBe(1_000_000);
    expect(holds.kinds[1].growth7d).toBeUndefined();
    expect(holds.kinds[0].oldest).toBe(Date.parse("2026-11-20T10:00:00Z"));
    expect(holds.owners).toHaveLength(1);
    expect(holds.kept[0].why).toBe("the gate reads it");
    expect(holds.today.restorable[0].id).toBe("20261124-1-old");
  });

  test("is part of telemetry, and absent from a daemon before it", () => {
    const base = { sampled_at: "2026-11-24T01:00:00Z" };
    expect(normalizeResourceTelemetry({ ...base, holds: wire })?.holds?.today.items).toBe(2);
    expect(normalizeResourceTelemetry(base)?.holds).toBeUndefined();
  });

  test("words", () => {
    expect(holdKindLabel("scratch")).toBe("Session scratch");
    expect(growthText(1_000_000, formatBytes)).toBe(`+${formatBytes(1_000_000)} in 7 days`);
    expect(growthText(-300, formatBytes)).toBe(`−${formatBytes(300)} in 7 days`);
    expect(growthText(0, formatBytes)).toBe("Same as 7 days ago");
    expect(growthText(undefined, formatBytes)).toBeUndefined();
    expect(holdOwnerTitle({ unit: "u", sessionId: "mh-1", bytes: 1 })).toBe("mh-1");
    expect(holdOwnerTitle({ unit: "u", sessionId: "mh-1", workTitle: "Release", bytes: 1 })).toBe("Release");
    expect(holdOwnerTitle({ unit: "u", bytes: 1 })).toBe("Ended Session");
    expect(baseName("/s/t/x/out/")).toBe("out");
  });
});
