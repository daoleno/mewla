import { describe, expect, test } from "bun:test";
import { sessionEmptyState } from "./sessionEmptyState";

// From the Expo app's services/mobileEmptyStates.test.ts (the Sessions case).
describe("empty-state actions", () => {
  test("state determines the action, not the existence of other servers", () => {
    expect(sessionEmptyState(false, undefined).action).toBe("pair");
    expect(sessionEmptyState(true, "connecting")).toMatchObject({ action: null, busy: true });
    expect(sessionEmptyState(true, "offline").action).toBe("retry");
    expect(sessionEmptyState(true, "connected").action).toBe("terminal");
    expect(sessionEmptyState(true, "connected", true).action).toBe("clear");
    expect(sessionEmptyState(true, "offline", true).action).toBe("retry");
  });
});
