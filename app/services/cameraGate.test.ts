import { describe, expect, test } from "bun:test";
import { cameraGate, cameraMayRun } from "./cameraGate";

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
