import { describe, expect, test } from "bun:test";
import { grantOf, grantRow, type ComputerGrant } from "./macGrants";

describe("a Mac's grants", () => {
  test("each state the daemon reads has one row", () => {
    expect(grantRow("granted")).toBe("allowed");
    // In the list switched off, or not in the list at all (never asked, removed with −): the same Allow… opens System
    // Settings with the panel, which says which of the two to do.
    expect(grantRow("off")).toBe("allow");
    expect(grantRow("missing")).toBe("allow");
    expect(grantRow("restart")).toBe("restart");
    expect(grantRow(undefined)).toBeNull();
  });

  test("a grant is found by id, and none is reported by a computer without them", () => {
    const grants: ComputerGrant[] = [
      { id: "screen_recording", name: "Screen Recording", state: "granted" },
      { id: "accessibility", name: "Accessibility", state: "missing" },
    ];
    expect(grantOf(grants, "accessibility")?.state).toBe("missing");
    expect(grantOf(undefined, "accessibility")).toBeUndefined();
  });
});
