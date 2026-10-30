import { describe, expect, test } from "bun:test";
import type { PhoneStatus } from "../../modules/mewla-phone/src";
import { nextPhoneStep, phoneMode, phoneSteps } from "./phoneSetup";

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
