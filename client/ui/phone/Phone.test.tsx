import { describe, expect, open, test } from "nsh/test";
import { stepUntil } from "../testing/fixtures";

const HARNESS = "./testing/Harness.tsx";
const DAEMON = "a".repeat(64);

type Status = Record<string, unknown>;

const ready: Status = {
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
  accessibility: true,
  restricted: false,
  self_healing: true,
  guide: null,
  notifications: true,
  state: "ready",
  w: 720,
  h: 1600,
  display_info: null,
  agent: null,
  handoff: null,
  drawn: true,
  opening: null,
  launch_failed: null,
};
const fresh: Status = { ...ready, developer_options: false, wireless_debugging: false, paired: false, helper: false, accessibility: false, link: false, link_state: "offline", w: 0, h: 0 };
const working: Status = { ...ready, agent: "Writer" };

function secureStore() {
  const secure = new Map<string, string>();
  return {
    "secure.get": ({ key }: { key: string }) => secure.get(key) ?? null,
    "secure.set": ({ key, value }: { key: string; value: string }) => void secure.set(key, value),
    "secure.remove": ({ key }: { key: string }) => void secure.delete(key),
  };
}

/** The Android host as the test plays it: its status, and what each call answers. */
function phoneHost(start: Status, answers: Record<string, unknown> = {}) {
  const box = { status: start };
  const handlers: Record<string, unknown> = {
    ...secureStore(),
    "push.requestPermission": true,
    "phone.status": () => box.status,
    "phone.setEnabled": ({ enabled }: { enabled: boolean }) => (box.status = { ...box.status, enabled }),
    "phone.follow": null,
    "phone.pair": true,
    "phone.ensureHelper": true,
    "phone.enableAccessibility": false,
    "phone.open": null,
    "phone.takeOver": null,
    "phone.giveBack": null,
    "phone.stop": null,
    "phone.handoffDone": null,
    "phone.enterPip": true,
    "phone.setPipAuto": null,
    ...answers,
  };
  return { box, handlers };
}

const computer = { "POST http://desk.test:9877/phone": { status: 200, body: { enabled: true, connected: true } } };

function start(status: Status, extra: { answers?: Record<string, unknown>; params?: Record<string, unknown> } = {}) {
  const fake = phoneHost(status, extra.answers);
  return {
    fake,
    options: { platform: "android" as const, network: computer, host: { params: extra.params ?? {}, handlers: fake.handlers } },
  };
}

type App = Awaited<ReturnType<typeof open>>;
const calls = (app: App, name: string) => app.host.calls().filter((c) => c.name === name).map((c) => c.data);

describe("Phone use, off", () => {
  test("the page offers Turn on; turning on asks for notifications, switches it on and the computer's switch too", async () => {
    const { options } = start({ ...fresh, enabled: false });
    const app = await open(HARNESS, options);
    await app.waitFor("id:phone-turn-on");
    await app.press("id:phone-turn-on");
    await app.waitFor("text:Developer options");
    expect(calls(app, "push.requestPermission").length).toBe(1);
    expect(calls(app, "phone.setEnabled")).toEqual([{ enabled: true }]);
    await app.waitUntil(() => app.requests().some((r) => r.method === "POST" && r.url === "http://desk.test:9877/phone"));
    const post = app.requests().find((r) => r.method === "POST" && r.url === "http://desk.test:9877/phone")!;
    expect(post.body).toBe('{"enabled":true}');
    expect(post.headers.authorization ?? post.headers.Authorization).toContain(`:${DAEMON}:`);
  }, 20_000);

  test("the link follows the current computer, on the computer's own address", async () => {
    const { options } = start({ ...fresh, enabled: false });
    const app = await open(HARNESS, options);
    await app.waitUntil(() => calls(app, "phone.follow").length === 1);
    expect(calls(app, "phone.follow")).toEqual([{ serverId: "s1", url: "ws://desk.test:9877/phone/device", daemonId: DAEMON }]);
  }, 20_000);

  test("switching computers moves the link; no computer stops it", async () => {
    const { options } = start(ready);
    const app = await open(HARNESS, options);
    await app.waitUntil(() => calls(app, "phone.follow").length === 1);
    await app.host.send("app.switch", { id: "s2", url: "https://other.test", daemonId: "c".repeat(64) });
    await app.waitUntil(() => calls(app, "phone.follow").length === 2);
    expect(calls(app, "phone.follow")[1]).toEqual({ serverId: "s2", url: "wss://other.test/phone/device", daemonId: "c".repeat(64) });
    await app.host.send("app.switch", null);
    await app.waitUntil(() => calls(app, "phone.follow").length === 3);
    expect(calls(app, "phone.follow")[2]).toBeNull();
  }, 20_000);

  test("the switch in the header turns it off here and on the computer", async () => {
    const { options } = start(ready);
    const app = await open(HARNESS, options);
    await app.waitFor("text:Ready");
    await app.press("id:phone-switch");
    await app.waitFor("id:phone-turn-on");
    expect(calls(app, "phone.setEnabled")).toEqual([{ enabled: false }]);
    await app.waitUntil(() => app.requests().some((r) => r.method === "POST" && r.body === '{"enabled":false}'));
  }, 20_000);

  test("a build without Phone use (the Play build, iOS) shows nothing to switch", async () => {
    const { options } = start(ready, { answers: { "phone.status": () => { throw new Error("no phone"); } } });
    const app = await open(HARNESS, options);
    await app.wait(200);
    expect(app.shows("id:phone-switch")).toBe(false);
    expect(app.shows("id:phone-turn-on")).toBe(false);
    expect(calls(app, "phone.follow")).toEqual([]);
  }, 20_000);
});

describe("Phone use, setup", () => {
  test("the checklist, with the one hint where it is needed; the first thing asked is Allow", async () => {
    const { options } = start(fresh);
    const app = await open(HARNESS, options);
    await app.waitFor("text:Allow");
    for (const title of ["Developer options", "Wi‑Fi", "Wireless debugging", "Pairing", "Helper"]) expect(app.texts()).toContain(title);
    expect(app.texts()).toContain("Tap Build number 7 times");
    await app.press("id:phone-setup-primary");
    // No secure-settings grant yet: the system's page for Mewla's service.
    await app.waitUntil(() => calls(app, "phone.open").length === 1);
    expect(calls(app, "phone.enableAccessibility").length).toBe(1);
    expect(calls(app, "phone.open")).toEqual([{ screen: "accessibility" }]);
  }, 20_000);

  test("a restricted install says how, and opens App info", async () => {
    const { options } = start({ ...fresh, restricted: true });
    const app = await open(HARNESS, options);
    await app.waitFor("text:App info › ⋮ › Allow restricted settings");
    await app.press("id:phone-app-info");
    await app.waitUntil(() => calls(app, "phone.open").length === 1);
    expect(calls(app, "phone.open")).toEqual([{ screen: "app" }]);
  }, 20_000);

  test("with the service on, Set up walks Settings; only what Android reserves is asked of the person", async () => {
    const { options } = start({ ...fresh, accessibility: true }, { answers: { "phone.pair": () => new Promise(() => {}) } });
    const app = await open(HARNESS, options);
    await app.waitFor("text:Set up");
    await app.press("id:phone-setup-primary");
    await app.waitUntil(() => calls(app, "phone.pair").length === 1);
    await app.host.send("phone.status", { ...fresh, accessibility: true, guide: { step: "developer", needs: "lock", stalled: false } });
    await app.waitFor("text:Confirm your screen lock");
    await app.host.send("phone.status", { ...fresh, accessibility: true, developer_options: true, guide: { step: "wireless", needs: "allow", stalled: false } });
    await app.waitFor("text:Tap Allow on the system prompt");
  }, 20_000);

  test("a row that is not done does its step: Developer options opens About phone", async () => {
    const { options } = start({ ...fresh, accessibility: true });
    const app = await open(HARNESS, options);
    await app.press("id:phone-step-developer");
    await app.waitUntil(() => calls(app, "phone.open").length === 1);
    expect(calls(app, "phone.open")).toEqual([{ screen: "developer" }]);
  }, 20_000);

  test("once paired the helper starts by itself, and why it did not is under its row", async () => {
    const { options } = start({ ...ready, helper: false, problem: "Wireless debugging is off" });
    const app = await open(HARNESS, options);
    await app.waitFor("text:Wireless debugging is off");
    await app.waitUntil(() => calls(app, "phone.ensureHelper").length >= 1);
    expect(app.texts()).toContain("Start");
  }, 20_000);
});

describe("Phone use, live", () => {
  test("ready says so", async () => {
    const { options } = start(ready);
    const app = await open(HARNESS, options);
    await app.waitFor("text:Ready");
    expect(app.shows("id:phone-take-over")).toBe(false);
  }, 20_000);

  test("an agent at work: its name, Float, Take over and Stop; taking over offers Give back", async () => {
    const { options } = start(working);
    const app = await open(HARNESS, options);
    await app.waitFor("text:Writer");
    await app.press("label:Float");
    await app.waitUntil(() => calls(app, "phone.enterPip").length === 1);
    expect(calls(app, "phone.enterPip")).toEqual([{ w: 720, h: 1600 }]);
    await app.press("id:phone-take-over");
    await app.waitUntil(() => calls(app, "phone.takeOver").length === 1);
    await app.host.send("phone.status", { ...working, state: "person_in_control" });
    await app.waitFor("id:phone-give-back");
    await app.press("id:phone-give-back");
    await app.waitUntil(() => calls(app, "phone.giveBack").length === 1);
    // While an agent works, leaving the app floats its screen.
    expect(calls(app, "phone.setPipAuto")[0]).toEqual({ on: true, w: 720, h: 1600 });
  }, 20_000);

  test("a handoff shows what the agent asks, with Done and Stop", async () => {
    const { options } = start({ ...working, state: "person_in_control", handoff: "Sign in to Instagram" });
    const app = await open(HARNESS, options);
    await app.waitFor("text:Sign in to Instagram");
    await app.press("id:phone-done");
    await app.waitUntil(() => calls(app, "phone.handoffDone").length === 1);
    await app.press("id:phone-stop");
    await app.waitUntil(() => calls(app, "phone.stop").length === 1);
  }, 20_000);

  test("stopped offers Resume", async () => {
    const { options } = start({ ...working, state: "stopped" });
    const app = await open(HARNESS, options);
    await app.waitFor("text:Stopped");
    await app.press("id:phone-resume");
    await app.waitUntil(() => calls(app, "phone.giveBack").length === 1);
  }, 20_000);
});

describe("Phone use, the floating screen", () => {
  /**
   * The window's menu, answered by one handler set as the app opens; waited for a step at a time, as a slow
   * runner shows the sheet after the press resolved (`stepUntil`).
   */
  function menus(app: App) {
    let choose: string | null = null;
    let offered: string[] | null = null;
    app.onDialog((dialog) => {
      if (!dialog.sheet) return null;
      offered = dialog.buttons;
      return choose ?? "Cancel";
    });
    /** Opens the menu and answers it with `answer`; resolves the options it offered. */
    return async (answer: string | null): Promise<string[]> => {
      choose = answer;
      offered = null;
      await app.press("id:screen-float-options");
      await stepUntil(app, () => offered !== null);
      return offered!;
    };
  }

  test("floats over another page while an agent works, named after it; Stop in its menu stops it", async () => {
    const { options } = start(working, { params: { page: "home" } });
    const app = await open(HARNESS, options);
    const menu = menus(app);
    await app.waitFor("id:screen-float");
    expect(app.texts()).toContain("Writer");
    expect(await menu("Stop")).toEqual(["Take over", "Stop", "Cancel"]);
    await app.waitUntil(() => calls(app, "phone.stop").length === 1);
  }, 20_000);

  test("Take over opens the Phone page, where the float is not", async () => {
    const { options } = start(working, { params: { page: "home" } });
    const app = await open(HARNESS, options);
    const menu = menus(app);
    await app.waitFor("id:screen-float");
    await menu("Take over");
    await app.waitFor("id:phone-page");
    expect(calls(app, "phone.takeOver").length).toBe(1);
    await app.waitFor("id:screen-float", { state: "gone" });
  }, 20_000);

  test("Open opens the Phone page without taking over", async () => {
    const { options } = start(working, { params: { page: "home" } });
    const app = await open(HARNESS, options);
    await app.press("id:screen-float-open");
    await app.waitFor("id:phone-page");
    expect(calls(app, "phone.takeOver")).toHaveLength(0);
  }, 20_000);

  test("driving offers Give back, a handoff Done with the ring; an app that could not open says so", async () => {
    const { options } = start({ ...working, state: "person_in_control" }, { params: { page: "home" } });
    const app = await open(HARNESS, options);
    const menu = menus(app);
    await app.waitFor("id:screen-float");
    expect(await menu(null)).toEqual(["Give back", "Stop", "Cancel"]);
    await app.host.send("phone.status", { ...working, state: "person_in_control", handoff: "Pay" });
    await app.waitFor("id:screen-float-attention");
    expect(await menu("Done")).toEqual(["Done", "Stop", "Cancel"]);
    await app.waitUntil(() => calls(app, "phone.handoffDone").length === 1);
    await app.host.send("phone.status", { ...working, drawn: false, launch_failed: "微信" });
    await app.waitFor("text:Couldn't open 微信");
  }, 20_000);

  test("nothing floats while no agent works", async () => {
    const { options } = start(ready, { params: { page: "home" } });
    const app = await open(HARNESS, options);
    await app.waitFor("text:Home");
    await app.wait(200);
    expect(app.shows("id:screen-float")).toBe(false);
  }, 20_000);
});
