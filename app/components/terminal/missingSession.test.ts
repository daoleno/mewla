import { describe, expect, test } from "bun:test";
import { shouldLeaveMissingSession } from "./screen/missingSession";

const confirmed = { resolved: false, everResolved: false, sessionListFresh: true, brainHostKnown: true };

describe("missing Session links", () => {
  test("a link to a Session the server does not have leaves", () => {
    expect(shouldLeaveMissingSession(confirmed)).toBe(true);
  });

  test("waits until the server confirms its Session list and Brain host", () => {
    expect(shouldLeaveMissingSession({ ...confirmed, sessionListFresh: false })).toBe(false);
    expect(shouldLeaveMissingSession({ ...confirmed, brainHostKnown: false })).toBe(false);
  });

  test("a resolved Session, or one that ended while open, stays", () => {
    expect(shouldLeaveMissingSession({ ...confirmed, resolved: true })).toBe(false);
    expect(shouldLeaveMissingSession({ ...confirmed, everResolved: true })).toBe(false);
  });
});
