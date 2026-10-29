import { describe, expect, test } from "bun:test";
import { remainingSnoozeMinutes } from "./calendarAlerts";

describe("native calendar action replay", () => {
  const action = { type: "snooze" as const, itemId: "i", serverId: "s", minutes: 15, atMs: 1_000_000 };

  test("a snooze replayed late tells the daemon what is left of it", () => {
    expect(remainingSnoozeMinutes(action, 1_000_000)).toBe(15);
    expect(remainingSnoozeMinutes(action, 1_000_000 + 4 * 60_000)).toBe(11);
  });

  test("a snooze that already ran out has nothing left to tell the daemon", () => {
    expect(remainingSnoozeMinutes(action, 1_000_000 + 20 * 60_000)).toBeLessThan(1);
  });
});
