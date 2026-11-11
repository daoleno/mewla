import { describe, expect, open, test } from "nsh/test";
import { expoTokenService, paired, phoneHost } from "../testing/fixtures";

const APP = "../FakeDaemonApp.tsx";

const ready = {
  supported: true, sdk: 35, enabled: true, link: true, link_state: "online", developer_options: true, wireless_debugging: true, wifi: true, paired: true, helper: true,
  problem: null, accessibility: true, restricted: false, self_healing: true, guide: null, notifications: true, state: "ready", w: 720, h: 1600, display_info: null,
  agent: null, handoff: null, drawn: true, opening: null, launch_failed: null,
};

/** The whole app on the Android GitHub APK, with the host's Phone use answering `status`. */
async function phoneApp(status: Record<string, unknown>) {
  const calls: string[] = [];
  const handlers: Record<string, (data: any) => unknown> = {
    "phone.status": () => status,
    "phone.follow": () => null,
    "phone.setPipAuto": () => null,
    "phone.takeOver": () => void calls.push("takeOver"),
    "phone.stop": () => void calls.push("stop"),
  };
  const app = await open(APP, { host: phoneHost({}, handlers), storage: paired, platform: "android", network: expoTokenService });
  await app.waitFor("id:composer");
  return { app, calls };
}

describe("Phone use in the app", () => {
  test("a Phone use notification's link opens the Phone page", async () => {
    const { app } = await phoneApp(ready);
    await app.host.send("links.open", { url: "mewla://phone" });
    await app.waitFor("id:phone-page", { timeout: 10_000 });
    await app.waitFor("id:phone-live-title");
    expect(app.texts()).toContain("Ready");
  }, 20_000);

  test("while an agent works the phone, its floating control is over the app; Take over takes it and opens the Phone page, where the float hides", async () => {
    const { app, calls } = await phoneApp({ ...ready, agent: "Writer" });
    await app.waitFor("id:phone-float", { timeout: 10_000 });
    expect(app.texts()).toContain("Writer");
    await app.press("id:phone-float-take-over");
    await app.waitUntil(() => calls.includes("takeOver"));
    await app.waitFor("id:phone-page");
    await app.waitUntil(() => !app.shows("id:phone-float"));
  }, 20_000);

  test("nothing floats while no agent works the phone", async () => {
    const { app } = await phoneApp(ready);
    await app.wait(500);
    expect(app.shows("id:phone-float")).toBe(false);
  }, 20_000);
});
