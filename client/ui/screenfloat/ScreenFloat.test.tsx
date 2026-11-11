import { describe, expect, open, test, type StubResponse } from "nsh/test";
import { expoTokenService, paired, phoneHost, stepUntil } from "../testing/fixtures";

const APP = "../FakeDaemonApp.tsx";
const BASE = "http://127.0.0.1:1";

const json = (body: unknown): StubResponse => ({ status: 200, headers: { "content-type": "application/json" }, body: JSON.stringify(body) });
const session = (id: string, patch: Record<string, unknown> = {}) => ({ id, name: "", status: "running", cwd: "/home/me/mewla", command: "claude", provider: "claude", updated_at: Date.now(), ...patch });
const display = (id: string, patch: Record<string, unknown> = {}) => ({ id, width: 1280, height: 800, control: "agent", apps: 1, idle_s: 2, ...patch });
const linux = { supported: false, platform: "linux", name: "Desk", ready: false };

/** The whole app, its computer answering Screen's and the displays' requests. */
async function app(daemon: Record<string, unknown>, network: Record<string, StubResponse>, platform: "android" | "ios" = "android", extra: Parameters<typeof open>[1] = {}) {
  const opened = await open(APP, { host: phoneHost({ brain: {}, ...daemon }), storage: paired, platform, network: { ...expoTokenService, ...network }, ...extra });
  await opened.waitFor("id:composer");
  return opened;
}

describe("the floating screen: agents' displays and the Mac", () => {
  test("an agent's display floats while its Worker runs, named after it; Stop in its menu stops that display", async () => {
    const shown = await app({ worker_sessions: [session("%1", { name: "Writer" })] }, {
      [`GET ${BASE}/display`]: json({ displays: [display("%1")], available: true }),
      [`GET ${BASE}/screen`]: json(linux),
      [`POST ${BASE}/display`]: json({ ok: true }),
    });
    let offered: string[] = [];
    shown.onDialog((dialog) => {
      if (!dialog.sheet) return null;
      offered = dialog.buttons;
      return "Stop";
    });
    await shown.waitFor("id:screen-float", { timeout: 10_000 });
    expect(shown.texts()).toContain("Writer");
    await shown.press("id:screen-float-options");
    await stepUntil(shown, () => offered.length > 0);
    expect(offered).toContain("Stop");
    await shown.waitUntil(() => shown.requests().some((r) => r.method === "POST" && r.url === `${BASE}/display`));
    const halt = shown.requests().find((r) => r.method === "POST" && r.url === `${BASE}/display`)!;
    expect(JSON.parse(String(halt.body))).toMatchObject({ worker: "%1" });
  }, 25_000);

  test("Open goes to Screen, where the float is not; a display its agent has stopped, or one long idle, does not float", async () => {
    const shown = await app({ worker_sessions: [session("%1", { name: "Writer" }), session("%2", { name: "Coder" }), session("%3", { name: "Idle" })] }, {
      [`GET ${BASE}/display`]: json({ displays: [display("%1"), display("%2", { stopped: true }), display("%3", { idle_s: 600 })], available: true }),
      [`GET ${BASE}/screen`]: json(linux),
    });
    await shown.waitFor("id:screen-float", { timeout: 10_000 });
    expect(shown.texts()).not.toContain("Coder");
    expect(shown.texts()).not.toContain("Idle");
    expect(shown.texts().some((text) => text.includes("1/"))).toBe(false);
    await shown.press("id:screen-float-open");
    await shown.waitFor("id:screen-page", { timeout: 10_000 });
    await shown.waitUntil(() => !shown.shows("id:screen-float"));
  }, 25_000);

  test("nothing floats, and nothing is asked, while no Worker runs", async () => {
    const shown = await app({ worker_sessions: [session("%1", { name: "Writer", status: "done" })] }, {
      [`GET ${BASE}/display`]: json({ displays: [display("%1")], available: true }),
      [`GET ${BASE}/screen`]: json(linux),
    });
    await shown.wait(1_500);
    expect(shown.shows("id:screen-float")).toBe(false);
    expect(shown.requests().some((r) => r.url === `${BASE}/display`)).toBe(false);
  }, 25_000);

  test("a Session's page shows its own Worker's screen only", async () => {
    const shown = await app({ worker_sessions: [session("%1", { name: "Writer" }), session("%2", { name: "Coder" })] }, {
      [`GET ${BASE}/display`]: json({ displays: [display("%2")], available: true }),
      [`GET ${BASE}/screen`]: json(linux),
    });
    await shown.waitFor("id:screen-float", { timeout: 10_000 });
    await shown.host.send("links.open", { url: "mewla://sessions/%251" });
    await shown.waitUntil(() => !shown.shows("id:screen-float"), { timeout: 10_000 });
  }, 25_000);

  // That the Mac's is a watch, never a take-over, is core/screen/float.test.ts's "what opens a screen".
  test("an agent on the Mac's screen floats on an iPhone too; two screens take turns on the title", async () => {
    const mac = { supported: true, platform: "darwin", name: "Studio", disabled: false, approved: true, ready: true, keep_awake: false, permissions: [], agent: { worker: "%2", name: "Coder", since: "" }, agents: [{ worker: "%2", name: "Coder", since: "" }] };
    const shown = await app({ worker_sessions: [session("%2", { name: "Coder" }), session("%1", { name: "Writer" })] }, {
      [`GET ${BASE}/display`]: json({ displays: [display("%1")], available: true }),
      [`GET ${BASE}/screen`]: json(mac),
    }, "ios");
    await shown.waitFor("id:screen-float", { timeout: 10_000 });
    await shown.waitUntil(() => shown.texts().some((text) => /1\/2$/.test(text)), { timeout: 10_000 });
    const before = shown.texts().find((text) => /\/2$/.test(text));
    await shown.press("id:screen-float-title");
    await shown.waitUntil(() => shown.texts().some((text) => /2\/2$/.test(text)));
    expect(shown.texts().find((text) => /\/2$/.test(text))).not.toBe(before);
  }, 25_000);

  /** Two agents' displays on a manual clock: taps and drags land at the times the test says. */
  const two = () => app({ worker_sessions: [session("%2", { name: "Coder" }), session("%1", { name: "Writer" })] }, {
    [`GET ${BASE}/display`]: json({ displays: [display("%1"), display("%2")], available: true }),
    [`GET ${BASE}/screen`]: json(linux),
  }, "android", { clock: {} });
  const on = (shown: Awaited<ReturnType<typeof app>>, n: number) => shown.texts().some((text) => new RegExp(`${n}/2$`).test(text));

  test("a double tap on the title only changes the size: the first tap does not go to the next screen", async () => {
    const shown = await two();
    await shown.waitUntil(() => on(shown, 1), { timeout: 10_000 });
    await shown.press("id:screen-float-title");
    await shown.press("id:screen-float-title");
    await shown.clock!.runFor(1_000);
    expect(on(shown, 1)).toBe(true);
    expect(shown.storage()["mewla:phone_float_size"]).toBe("0.8");
    // One tap goes on once no second one follows.
    await shown.press("id:screen-float-title");
    expect(on(shown, 1)).toBe(true);
    await shown.clock!.runFor(1_000);
    expect(on(shown, 2)).toBe(true);
  }, 25_000);

  test("dragged by its bar and let go, the window goes to the nearest corner and stays there", async () => {
    const shown = await two();
    await shown.waitUntil(() => on(shown, 1), { timeout: 10_000 });
    await shown.drag("id:screen-float-title", { x: -300, y: -500 });
    await shown.clock!.runFor(1_000);
    expect(shown.storage()["mewla:phone_float_corner"]).toBe("top-left");
    // A drag is not a tap: still the same screen.
    expect(on(shown, 1)).toBe(true);
  }, 25_000);
});
