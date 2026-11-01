import { describe, expect, test } from "bun:test";
import { cameraGate, cameraIsSearching, cameraMayRun } from "./cameraGate";

describe("camera gate", () => {
  test("every permission state has something to show", () => {
    expect(cameraGate(null)).toBe("loading");
    expect(cameraGate(undefined)).toBe("loading");
    expect(cameraGate({ granted: false, canAskAgain: true })).toBe("request");
    expect(cameraGate({ granted: false, canAskAgain: false })).toBe("denied");
    expect(cameraGate({ granted: true, canAskAgain: false })).toBe("camera");
    expect(cameraGate({ granted: true, canAskAgain: true })).toBe("camera");
  });

  test("the system permission dialog does not stop the camera", () => {
    expect(cameraMayRun("active")).toBe(true);
    expect(cameraMayRun("inactive")).toBe(true);
    expect(cameraMayRun("unknown")).toBe(true);
    expect(cameraMayRun("background")).toBe(false);
  });
});

describe("camera searching", () => {
  test("only a running camera is looking for a code", () => {
    expect(cameraIsSearching("camera", null, true)).toBe(true);
    expect(cameraIsSearching("denied", null, true)).toBe(false);
    expect(cameraIsSearching("request", null, true)).toBe(false);
    expect(cameraIsSearching("loading", null, true)).toBe(false);
    expect(cameraIsSearching("camera", "could not start", true)).toBe(false);
    expect(cameraIsSearching("camera", null, false)).toBe(false);
  });
});
