import { describe, expect, test } from "bun:test";
import {
  floatPlaceholder,
  nextPhoneStep,
  phoneAgentWorking,
  phoneFloat,
  phoneLabsValue,
  phoneMode,
  phoneOwner,
  phoneReady,
  phoneSetupPlan,
  phoneStateWord,
  phoneSteps,
} from "./setup";
import { parsePhoneStatus, type PhoneStatus } from "./status";

// The Expo app's tests (app/components/phone/phoneSetup.test.ts and the phone parts of
// screenFloatModel.test.ts, PhoneSettings), on the ported logic.

const ready: PhoneStatus = {
  supported: true,
  sdk: 35,
  enabled: true,
  link: true,
  link_state: "online",
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
  drawn: true,
  opening: null,
  launch_failed: null,
  display_info: null,
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

  test("leaving the App floats the screen under the same working rule", () => {
    expect(phoneAgentWorking(working)).toBe(true);
    expect(phoneAgentWorking({ ...working, state: "stopped" })).toBe(false);
    expect(phoneAgentWorking(ready)).toBe(false);
    expect(phoneAgentWorking(null)).toBe(false);
    expect(phoneAgentWorking({ ...working, enabled: false })).toBe(false);
  });

  test("shows what is going on while the agent's screen has nothing on it, in as few words as it can", () => {
    expect(floatPlaceholder({ drawn: true, opening: null, launch_failed: null })).toBeNull();
    expect(floatPlaceholder({ drawn: true, opening: "微信", launch_failed: null })).toBeNull();
    expect(floatPlaceholder({ drawn: false, opening: "微信 (分身 999)", launch_failed: null })).toEqual({ text: "微信 (分身 999)", failed: false });
    expect(floatPlaceholder({ drawn: false, opening: null, launch_failed: "微信 (分身 999)" })).toEqual({ text: "Couldn't open 微信 (分身 999)", failed: true });
    expect(floatPlaceholder({ drawn: false, opening: null, launch_failed: null })).toEqual({ text: null, failed: false });
  });

  test("exists only while an agent works the phone and its screen has a size", () => {
    expect(phoneFloat({ ...working, agent: "Writer" })).toEqual({ title: "Writer", width: 720, height: 1600 });
    expect(phoneFloat({ ...working, agent: null })).toBeNull();
    expect(phoneFloat({ ...working, state: "stopped" })).toBeNull();
    expect(phoneFloat({ ...working, w: 0, h: 0 })).toBeNull();
    expect(phoneFloat(null)).toBeNull();
  });

  test("the agent is matched to a Worker by id, else by a name only one has", () => {
    const workers = [
      { id: "%1", name: "Writer" },
      { id: "%2", name: "Coder" },
      { id: "%3", name: "Coder" },
    ];
    expect(phoneOwner("%2", workers)).toBe("%2");
    expect(phoneOwner("Writer", workers)).toBe("%1");
    expect(phoneOwner("Coder", workers)).toBeNull();
    expect(phoneOwner("An agent", workers)).toBeNull();
  });
});

describe("phone setup plan", () => {
  const fresh: PhoneStatus = { ...ready, developer_options: false, wireless_debugging: false, paired: false, helper: false };

  test("the first thing asked is to allow the accessibility service, and why it may not open", () => {
    expect(phoneSetupPlan(fresh, false)).toMatchObject({ action: "allow", label: "Allow", note: null });
    expect(phoneSetupPlan({ ...fresh, restricted: true }, false).note).toContain("Allow restricted settings");
  });

  test("with it on, one Set up walks everything; the row's spinner is the progress, not a sentence", () => {
    const on = { ...fresh, accessibility: true };
    expect(phoneSetupPlan(on, false)).toMatchObject({ action: "setup", label: "Set up", current: "developer" });
    expect(phoneSetupPlan({ ...on, guide: { step: "developer", needs: null, stalled: false } }, true)).toMatchObject({ current: "developer", note: null });
    expect(phoneSetupPlan({ ...on, developer_options: true, guide: { step: "wireless", needs: null, stalled: false } }, true)).toMatchObject({
      current: "wireless",
      note: null,
    });
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

describe("Settings → Labs → Phone use", () => {
  test("Off, Set up, Ready or You are in control", () => {
    expect(phoneLabsValue(null)).toBe("Off");
    expect(phoneLabsValue({ ...ready, enabled: false })).toBe("Off");
    expect(phoneLabsValue(ready)).toBe("Ready");
    expect(phoneLabsValue({ ...ready, state: "person_in_control" })).toBe("You are in control");
    expect(phoneLabsValue({ ...ready, helper: false })).toBe("Set up");
  });

  test("ready needs the pairing, the helper and the link to the computer", () => {
    expect(phoneReady(ready)).toBe(true);
    expect(phoneReady({ ...ready, paired: false })).toBe(false);
    expect(phoneReady({ ...ready, helper: false })).toBe(false);
    expect(phoneReady({ ...ready, link: false, link_state: "offline" })).toBe(false);
    expect(phoneReady({ ...ready, link_state: "replaced" })).toBe(false);
    expect(phoneStateWord(false, "ready")).toBe("Set up");
  });
});

describe("the host's status", () => {
  test("is read field by field, JSON nulls as null", () => {
    const parsed = parsePhoneStatus({ ...ready, guide: { step: "pair", needs: null, stalled: true }, problem: null, agent: "Writer" });
    expect(parsed).toEqual({ ...ready, guide: { step: "pair", needs: null, stalled: true }, agent: "Writer" });
  });

  test("an answer that is not a status is none", () => {
    expect(parsePhoneStatus(null)).toBeNull();
    expect(parsePhoneStatus("ready")).toBeNull();
    expect(parsePhoneStatus({ enabled: true })).toBeNull();
  });

  test("an unknown state reads as setup, and an older host without link_state as its link flag", () => {
    const old = { ...ready, state: "flying", link_state: undefined };
    expect(parsePhoneStatus(old)?.state).toBe("setup");
    expect(parsePhoneStatus(old)?.link_state).toBe("online");
    expect(parsePhoneStatus({ ...old, link: false })?.link_state).toBe("off");
  });
});
