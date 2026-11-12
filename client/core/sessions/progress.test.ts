import { describe, expect, test } from "bun:test";
import { progressSteps } from "./progress";

describe("a Worker's progress bar", () => {
  test("five segments from reading on; starting is an empty bar; no phase, no bar", () => {
    expect(["starting", "reading", "planning", "working", "verifying", "reporting"].map(progressSteps)).toEqual([0, 1, 2, 3, 4, 5]);
    expect(progressSteps(undefined)).toBeNull();
    expect(progressSteps("")).toBeNull();
    expect(progressSteps("thinking hard")).toBeNull();
    expect(progressSteps(" Verifying ")).toBe(4);
  });
});
