import { describe, expect, test } from "bun:test";
import type { PhoneStatus } from "../../modules/mewla-phone/src";
import {
  floatOrigin,
  floatSize,
  floatWidth,
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

  test("keeps the agent's proportions inside the height it may take", () => {
    expect(floatSize(9 / 20, 80, 400)).toEqual({ width: 80, height: 178 });
    expect(floatSize(9 / 20, 80, 100)).toEqual({ width: 45, height: 100 });
    expect(floatSize(0, 80, 400)).toEqual({ width: 80, height: 178 });
  });

  test("is two fifths of the window wide, and larger on request, never a sliver", () => {
    expect(floatWidth(411, false)).toBe(164);
    expect(floatWidth(411, true)).toBe(288);
    // The agent's 720x1600 display at that width on a 914-tall window leaves room for the whole picture.
    expect(floatSize(720 / 1600, floatWidth(411, false), 700)).toEqual({ width: 164, height: 364 });
    // With the keyboard up the window gives height first, but keeps a readable width.
    expect(floatSize(720 / 1600, floatWidth(411, false), 240).width).toBe(108);
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
});
