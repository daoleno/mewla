import { describe, expect, test } from "bun:test";
import type { PhoneStatus } from "../../modules/mewla-phone/src";
import {
  FLOAT_DEFAULT,
  FLOAT_LARGE,
  floatOrigin,
  clampFloatFraction,
  floatSize,
  parseFloatFraction,
  resizedFloatWidth,
  toggledFloatFraction,
  nearestFloatCorner,
  nextPhoneStep,
  parseFloatCorner,
  phoneAgentWorking,
  phoneFloatVisible,
  phoneMode,
  phoneSetupPlan,
  phoneSteps,
} from "./phoneSetup";

const ready: PhoneStatus = {
  supported: true,
  sdk: 35,
  enabled: true,
  link: true,
  developer_options: true,
  wireless_debugging: true,
  wifi: true,
  paired: true,
  helper: true,
  problem: null,
  accessibility: false,
  restricted: false,
  self_healing: true,
  guide: null,
  notifications: true,
  state: "ready",
  w: 720,
  h: 1600,
  agent: null,
  handoff: null,
};

describe("phone setup", () => {
  test("offers the first step that is not done, in the order they can be done", () => {
    expect(nextPhoneStep({ ...ready, developer_options: false, wifi: false, paired: false, helper: false })?.id).toBe("developer");
    expect(nextPhoneStep({ ...ready, wireless_debugging: false, paired: false, helper: false })?.id).toBe("wireless");
    expect(nextPhoneStep({ ...ready, paired: false, helper: false })?.id).toBe("pair");
    expect(nextPhoneStep({ ...ready, helper: false })?.id).toBe("helper");
    expect(nextPhoneStep(ready)).toBeNull();
  });

  test("a hint is only given where the person could not guess the step", () => {
    const hinted = phoneSteps(ready).filter((step) => step.hint);
    expect(hinted.map((step) => step.id)).toEqual(["developer"]);
  });
});

describe("phone mode", () => {
  test("setup until everything is in place, whatever an agent is doing", () => {
    expect(phoneMode({ ...ready, helper: false, agent: "Brain" })).toBe("setup");
  });

  test("idle, watching, driving, handoff and stopped follow the state and the agent", () => {
    expect(phoneMode(ready)).toBe("idle");
    expect(phoneMode({ ...ready, agent: "Brain" })).toBe("watching");
    expect(phoneMode({ ...ready, agent: "Brain", state: "person_in_control" })).toBe("driving");
    expect(phoneMode({ ...ready, agent: "Brain", state: "person_in_control", handoff: "Sign in to Instagram" })).toBe("handoff");
    expect(phoneMode({ ...ready, agent: "Brain", state: "stopped" })).toBe("stopped");
  });
});

describe("phone float", () => {
  const working: PhoneStatus = { ...ready, state: "ready", agent: "Worker" };

  test("floats over every page while an agent works", () => {
    expect(phoneFloatVisible(working, "/")).toBe(true);
    expect(phoneFloatVisible(working, "/list")).toBe(true);
    expect(phoneFloatVisible(working, "/work/abc")).toBe(true);
    expect(phoneFloatVisible(working, "/settings")).toBe(true);
    expect(phoneFloatVisible({ ...working, state: "person_in_control" }, "/")).toBe(true);
    expect(phoneFloatVisible({ ...working, handoff: "Log in" }, "/")).toBe(true);
  });

  test("hides on the Phone page, which is the full view", () => {
    expect(phoneFloatVisible(working, "/phone")).toBe(false);
    expect(phoneFloatVisible(working, "/phone/")).toBe(false);
    expect(phoneFloatVisible(working, "/phones")).toBe(true);
  });

  test("is gone when no agent works, when it is stopped, and before there is a screen", () => {
    expect(phoneFloatVisible(null, "/")).toBe(false);
    expect(phoneFloatVisible({ ...ready }, "/")).toBe(false);
    expect(phoneFloatVisible({ ...working, state: "stopped" }, "/")).toBe(false);
    expect(phoneFloatVisible({ ...working, agent: null }, "/")).toBe(false);
    expect(phoneFloatVisible({ ...working, w: 0, h: 0 }, "/")).toBe(false);
    expect(phoneFloatVisible({ ...working, enabled: false }, "/")).toBe(false);
    expect(phoneFloatVisible({ ...working, helper: false }, "/")).toBe(false);
  });

  test("leaving the App floats the screen under the same working rule", () => {
    expect(phoneAgentWorking(working)).toBe(true);
    expect(phoneAgentWorking({ ...working, state: "stopped" })).toBe(false);
    expect(phoneAgentWorking(ready)).toBe(false);
    expect(phoneAgentWorking(null)).toBe(false);
  });

  const area = { left: 0, top: 100, right: 400, bottom: 700 };
  const size = { width: 80, height: 180 };

  test("sits in the four corners of the free area", () => {
    expect(floatOrigin("top-left", area, size, 12)).toEqual({ x: 12, y: 112 });
    expect(floatOrigin("top-right", area, size, 12)).toEqual({ x: 308, y: 112 });
    expect(floatOrigin("bottom-left", area, size, 12)).toEqual({ x: 12, y: 508 });
    expect(floatOrigin("bottom-right", area, size, 12)).toEqual({ x: 308, y: 508 });
  });

  test("a keyboard that shrinks the area moves the bottom corners up and never off the top", () => {
    const small = { ...area, bottom: 200 };
    expect(floatOrigin("bottom-right", small, size, 12).y).toBe(100);
    expect(floatOrigin("top-right", small, size, 12).y).toBe(112);
  });

  test("snaps to the corner nearest the float's centre", () => {
    expect(nearestFloatCorner({ x: 50, y: 150 }, area)).toBe("top-left");
    expect(nearestFloatCorner({ x: 350, y: 150 }, area)).toBe("top-right");
    expect(nearestFloatCorner({ x: 50, y: 650 }, area)).toBe("bottom-left");
    expect(nearestFloatCorner({ x: 350, y: 650 }, area)).toBe("bottom-right");
  });

  const window = { width: 411, height: 914 };
  const ratio = 720 / 1600;

  test("is the agent display's exact shape, at 55% of the width by default", () => {
    const size = floatSize(ratio, FLOAT_DEFAULT, window);
    expect(size.width).toBeCloseTo(226.05, 2);
    expect(size.width / size.height).toBeCloseTo(ratio, 10);
    // 502dp tall: under 60% of the window's height.
    expect(size.height).toBeLessThan(0.6 * window.height);
  });

  test("the default never covers more than 60% of the height, even on a short wide display", () => {
    const wide = floatSize(0.8, FLOAT_DEFAULT, { width: 411, height: 500 });
    expect(wide.height).toBeLessThanOrEqual(0.6 * 500 + 1e-9);
    expect(wide.width / wide.height).toBeCloseTo(0.8, 10);
  });

  test("a size the person chose may be taller, up to 85% of the height", () => {
    const large = floatSize(ratio, FLOAT_LARGE, window);
    expect(large.width).toBeCloseTo(328.8, 1);
    expect(large.height).toBeCloseTo(730.7, 1);
    expect(large.height).toBeLessThanOrEqual(0.85 * window.height);
    expect(floatSize(ratio, 0.9, { width: 411, height: 700 }).height).toBeLessThanOrEqual(0.85 * 700 + 1e-9);
  });

  test("the least width is 30% of the window, however little room there is", () => {
    const tight = floatSize(ratio, FLOAT_DEFAULT, window, 100);
    expect(tight.width).toBeCloseTo(0.3 * window.width, 5);
    expect(tight.width / tight.height).toBeCloseTo(ratio, 10);
  });

  test("a keyboard gives height first and keeps the shape", () => {
    const size = floatSize(ratio, FLOAT_DEFAULT, window, 400);
    expect(size.height).toBeCloseTo(400, 5);
    expect(size.width / size.height).toBeCloseTo(ratio, 10);
  });

  test("a display with no size yet falls back to a phone's shape", () => {
    expect(floatSize(0, FLOAT_DEFAULT, window).width / floatSize(0, FLOAT_DEFAULT, window).height).toBeCloseTo(9 / 20, 10);
  });

  test("dragging the inner corner outward grows it along the diagonal, for every corner", () => {
    // Anchored top-left: the handle is bottom-right, outward is +x +y.
    expect(resizedFloatWidth(200, ratio, { x: 0, y: 0 }, "top-left")).toBe(200);
    expect(resizedFloatWidth(200, ratio, { x: 30, y: 0 }, "top-left")).toBeGreaterThan(200);
    expect(resizedFloatWidth(200, ratio, { x: 0, y: 40 }, "top-left")).toBeGreaterThan(200);
    expect(resizedFloatWidth(200, ratio, { x: -30, y: -40 }, "top-left")).toBeLessThan(200);
    // Anchored top-right: the handle is bottom-left, outward is -x +y.
    expect(resizedFloatWidth(200, ratio, { x: -30, y: 0 }, "top-right")).toBeGreaterThan(200);
    expect(resizedFloatWidth(200, ratio, { x: 30, y: 0 }, "top-right")).toBeLessThan(200);
    // Anchored bottom-right: the handle is top-left, outward is -x -y.
    expect(resizedFloatWidth(200, ratio, { x: 0, y: -40 }, "bottom-right")).toBeGreaterThan(200);
    // Anchored bottom-left: the handle is top-right, outward is +x -y.
    expect(resizedFloatWidth(200, ratio, { x: 30, y: -40 }, "bottom-left")).toBeGreaterThan(200);
  });

  test("a drag along the diagonal moves the corner exactly as far as the width says", () => {
    const start = 200;
    const height = start / ratio;
    // The handle moves by (dw, dw / ratio) when the width grows by dw.
    expect(resizedFloatWidth(start, ratio, { x: 20, y: 20 / ratio }, "top-left")).toBeCloseTo(start + 20, 8);
    expect(height).toBeGreaterThan(start);
  });

  test("a double tap goes between the default and the large size", () => {
    expect(toggledFloatFraction(FLOAT_DEFAULT)).toBe(FLOAT_LARGE);
    expect(toggledFloatFraction(FLOAT_LARGE)).toBe(FLOAT_DEFAULT);
    expect(toggledFloatFraction(0.4)).toBe(FLOAT_DEFAULT);
  });

  test("remembers a size only in range, and clamps one to 30%-90%", () => {
    expect(parseFloatFraction("0.7")).toBe(0.7);
    expect(parseFloatFraction("0.1")).toBe(FLOAT_DEFAULT);
    expect(parseFloatFraction("2")).toBe(FLOAT_DEFAULT);
    expect(parseFloatFraction("wide")).toBe(FLOAT_DEFAULT);
    expect(parseFloatFraction(null)).toBe(FLOAT_DEFAULT);
    expect(clampFloatFraction(0.1)).toBe(0.3);
    expect(clampFloatFraction(1.4)).toBe(0.9);
    expect(clampFloatFraction(0.5)).toBe(0.5);
  });

  test("remembers only a corner it knows", () => {
    expect(parseFloatCorner("bottom-left")).toBe("bottom-left");
    expect(parseFloatCorner("middle")).toBe("top-right");
    expect(parseFloatCorner(null)).toBe("top-right");
  });
});

describe("phone setup plan", () => {
  const fresh: PhoneStatus = { ...ready, developer_options: false, wireless_debugging: false, paired: false, helper: false };

  test("the first thing asked is to allow the accessibility service, and why it may not open", () => {
    expect(phoneSetupPlan(fresh, false)).toMatchObject({ action: "allow", label: "Allow", note: null });
    expect(phoneSetupPlan({ ...fresh, restricted: true }, false).note).toContain("Allow restricted settings");
  });

  test("with it on, one Set up walks everything and names the step it is on", () => {
    const on = { ...fresh, accessibility: true };
    expect(phoneSetupPlan(on, false)).toMatchObject({ action: "setup", label: "Set up", current: "developer" });
    expect(phoneSetupPlan({ ...on, guide: { step: "developer", needs: null, stalled: false } }, true).note).toBe("Tapping Build number");
    expect(phoneSetupPlan({ ...on, developer_options: true, guide: { step: "wireless", needs: null, stalled: false } }, true).note).toBe(
      "Turning on Wireless debugging",
    );
  });

  test("only what Android reserves for the person is asked of them", () => {
    const on = { ...fresh, accessibility: true };
    expect(phoneSetupPlan({ ...on, guide: { step: "developer", needs: "lock", stalled: false } }, true).note).toBe("Confirm your screen lock");
    expect(phoneSetupPlan({ ...on, developer_options: true, guide: { step: "wireless", needs: "allow", stalled: false } }, true)).toMatchObject({
      current: "wireless",
      note: "Tap Allow on the system prompt",
    });
  });

  test("a step that stops getting anywhere falls back to its manual hint, at that step only", () => {
    const on = { ...fresh, accessibility: true };
    expect(phoneSetupPlan({ ...on, guide: { step: "developer", needs: null, stalled: true } }, true)).toMatchObject({
      current: "developer",
      note: "Tap Build number 7 times",
    });
  });

  test("off Wi-Fi the Wi-Fi page is offered, and a paired phone only needs its helper started", () => {
    expect(phoneSetupPlan({ ...fresh, accessibility: true, wifi: false, developer_options: true }, false).action).toBe("wifi");
    expect(phoneSetupPlan({ ...ready, helper: false }, false)).toMatchObject({ action: "start", current: "helper" });
  });

  test("after a reinstall over a paired phone it is one Start, never the pairing walk again", () => {
    // Wireless debugging went off while the App was away, but the App may turn it back on by itself.
    const reinstalled = { ...ready, helper: false, wireless_debugging: false, developer_options: false, self_healing: true };
    expect(nextPhoneStep(reinstalled)?.id).toBe("helper");
    expect(phoneSetupPlan(reinstalled, false)).toMatchObject({ action: "start", label: "Start", current: "helper" });
  });

  test("a paired phone whose Wireless debugging the App may not turn on offers that one switch, not Allow or Set up", () => {
    const off = { ...ready, helper: false, wireless_debugging: false, self_healing: false, accessibility: false };
    expect(phoneSetupPlan(off, false)).toMatchObject({ action: "wireless", current: "wireless" });
    expect(phoneSetupPlan({ ...off, accessibility: true }, false).action).toBe("wireless");
  });

  test("why the helper is not up is shown under its row", () => {
    expect(phoneSetupPlan({ ...ready, helper: false, problem: "Wireless debugging is off" }, false).note).toBe("Wireless debugging is off");
  });
});
