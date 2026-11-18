import { describe, expect, open, test, type StubResponse } from "nsh/test";
import { KEYBOARD_SENTINEL } from "../../../core/screen/screenPageModel";
import { SCREEN_TOO_OLD } from "../../../core/screen/screen";
import { pressWhenEnabled } from "../../testing/fixtures";

const HARNESS = "./testing/ScreenHarness.tsx";
const SCREEN = "http://desk.test:9877/screen?video=webrtc";
const DISPLAY = "http://desk.test:9877/display";
const IPHONE = "http://desk.test:9877/iphone";
const VIEWER = "ws://desk.test:9877/screen/viewer";

function secureStore() {
  const secure = new Map<string, string>();
  return {
    "secure.get": ({ key }: { key: string }) => secure.get(key) ?? null,
    "secure.set": ({ key, value }: { key: string; value: string }) => void secure.set(key, value),
    "secure.remove": ({ key }: { key: string }) => void secure.delete(key),
  };
}

const json = (body: unknown) => ({ status: 200, headers: { "content-type": "application/json" }, body: JSON.stringify(body) });

const base = { supported: true, platform: "darwin", name: "Studio", disabled: false, approved: true, ready: true, keep_awake: true, permissions: [], video: "webrtc" };
const codex = { worker: "w1", name: "Codex", since: "2026-11-11T08:00:00Z" };
const claude = { worker: "w2", name: "Claude", since: "2026-11-11T08:01:00Z" };

type Network = Record<string, StubResponse | ((request: { method: string; url: string; body?: string | null }) => StubResponse)>;

function options(network: Network, extra: { below?: unknown; worker?: string; rttMs?: number; platform?: "ios" | "android" | "web"; width?: number; height?: number; phone?: Record<string, unknown> } = {}) {
  return {
    platform: extra.platform,
    width: extra.width,
    height: extra.height,
    host: {
      params: { page: { name: "screen", worker: extra.worker }, below: extra.below, rttMs: extra.rttMs },
      handlers: {
        ...secureStore(),
        "app.daemon": () => ({ error: "not in this test" }),
        "app.socketOpened": null,
        "app.socketClosed": null,
        "app.socketSent": null,
        "phone.status": () => extra.phone ?? { supported: false, enabled: false },
      },
    },
    network,
  };
}

type App = Awaited<ReturnType<typeof open>>;
const opened = (app: App, url = VIEWER) => app.host.posts().filter((p) => p.name === "app.socketOpened" && (p.data as { url: string }).url.startsWith(url));
const closed = (app: App, url = VIEWER) => app.host.posts().filter((p) => p.name === "app.socketClosed" && (p.data as { url: string }).url.startsWith(url));
const posted = (app: App, url: string) => app.requests().filter((r) => r.method === "POST" && r.url === url).map((r) => JSON.parse(String(r.body)));

const sent = (app: App) => app.host.posts().filter((p) => p.name === "app.socketSent").map((p) => (p.data as { message: unknown }).message);

async function goLive(app: App) {
  await app.waitUntil(() => opened(app).length > 0);
  await app.host.send("app.viewer", { type: "connected", width: 1280, height: 800 });
  await app.waitFor("id:screen-indicator");
}

describe("Screen: setup", () => {
  test("a computer that has not allowed this phone asks, and Turn on Screen sends enable", async () => {
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json({ ...base, approved: false, ready: false }), [`POST ${SCREEN}`]: json({ ...base, approved: false, ready: false }) }));
    await app.waitFor("id:screen-turn-on");
    expect(app.texts()).toContain("Turn on Screen");
    await app.press("id:screen-turn-on");
    await app.waitUntil(() => posted(app, SCREEN).length > 0);
    expect(posted(app, SCREEN)[0]).toEqual({ action: "enable" });
  });

  test("a screen turned off at the computer says how to turn it on", async () => {
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json({ ...base, disabled: true, ready: false }) }));
    await app.waitFor("text:Screen is off");
    expect(app.texts()).toContain("Run mewla screen enable on Studio.");
  });

  test("waiting for permissions follows the computer: a grant that needs a restart brings Restart Mewla", async () => {
    let status = { ...base, ready: false, can_restart: true, permissions: [{ id: "rec", name: "Screen Recording", granted: false, can_open: true }] };
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: () => json(status) }));
    await app.waitFor("text:Screen Recording");
    expect(app.texts()).toContain("Open settings");
    expect(app.texts()).toContain("Ask on Studio");
    status = { ...status, permissions: [{ id: "rec", name: "Screen Recording", granted: false, can_open: true, restart_required: true } as never] };
    await app.waitFor("id:screen-restart", { timeout: 8_000 });
    expect(app.texts()).not.toContain("Ask on Studio");
  });

  test("Restart Mewla asks the computer, keeps looking until it is back, then opens the screen", async () => {
    let status: Record<string, unknown> = { ...base, ready: false, can_restart: true, permissions: [{ id: "rec", name: "Screen Recording", granted: false, restart_required: true }] };
    const app = await open(HARNESS, options({
      [`GET ${SCREEN}`]: () => json(status),
      [`POST ${SCREEN}`]: () => {
        status = { ...base };
        return { status: 202, headers: { "content-type": "application/json" }, body: JSON.stringify(status) };
      },
    }));
    await app.waitFor("id:screen-restart");
    await app.press("id:screen-restart");
    await app.waitFor("text:Restarting Mewla…");
    // The page says so as it asks: the request is on its way, not yet made.
    await app.waitUntil(() => posted(app, SCREEN).length > 0);
    expect(posted(app, SCREEN)).toEqual([{ action: "restart" }]);
    await app.waitUntil(() => opened(app).length > 0, { timeout: 8_000 });
  });

  test("a Linux computer without a screen offers the virtual screen, which it keeps", async () => {
    const app = await open(HARNESS, options({
      [`GET ${SCREEN}`]: json({ ...base, supported: false, platform: "linux", ready: false }),
      "POST http://desk.test:9877/browser": json({ virtual_screen: false }),
    }));
    await app.waitFor("id:virtual-screen");
    await pressWhenEnabled(app, "id:virtual-screen");
    await app.waitUntil(() => app.requests().filter((r) => r.url === "http://desk.test:9877/browser").length === 2);
    const last = app.requests().filter((r) => r.url === "http://desk.test:9877/browser").at(-1)!;
    expect(JSON.parse(String(last.body))).toEqual({ action: "settings", enabled: true });
  });

  test("turning the virtual screen on shows the agents' displays at once", async () => {
    const app = await open(HARNESS, options({
      [`GET ${SCREEN}`]: json({ ...base, supported: false, platform: "linux", ready: false }),
      "POST http://desk.test:9877/browser": (request) => json({ virtual_screen: JSON.parse(String(request.body)).enabled === true }),
      [`GET ${DISPLAY}`]: json({ displays: [], available: true, video: "webrtc" }),
    }));
    // `enabled` throws while no view matches: the switch first, then that it takes a press.
    await app.waitFor("id:virtual-screen");
    await pressWhenEnabled(app, "id:virtual-screen");
    await app.waitFor("text:Nothing on screen");
  });
});

describe("Screen: agents", () => {
  test("while an agent uses the screen, Watch, Stop and Take over are offered, and Stop tells the computer", async () => {
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json({ ...base, agent: codex, agents: [codex] }), [`POST ${SCREEN}`]: json({ ...base, agent: codex, agents: [codex] }) }));
    await app.waitFor("id:screen-agents");
    expect(app.texts()).toContain("Codex is using the screen");
    expect(app.texts()).toContain("Take over");
    expect(opened(app)).toHaveLength(0);
    await app.press("label:Stop Codex");
    await app.waitUntil(() => posted(app, SCREEN).length > 0);
    expect(posted(app, SCREEN)[0]).toEqual({ action: "stop_agent", worker: "w1" });
  });

  test("the agent card follows the computer: a second agent shows up with its own Watch and Stop", async () => {
    let status: Record<string, unknown> = { ...base, agent: codex, agents: [codex] };
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: () => json(status) }));
    await app.waitFor("text:Codex is using the screen");
    status = { ...base, agent: codex, agents: [codex, claude] };
    await app.waitFor("text:2 agents are using the screen", { timeout: 8_000 });
    expect(app.shows("label:Watch Claude")).toBe(true);
    expect(app.texts()).toContain("Take over (stops all)");
  });

  test("Watch shows the agent's screen without taking over, names the watched agent, and hands the screen back when it finishes", async () => {
    let status: Record<string, unknown> = { ...base, agent: codex, agents: [codex, claude] };
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: () => json(status) }));
    await app.waitFor("label:Watch Claude");
    await app.press("label:Watch Claude");
    await app.waitUntil(() => opened(app).length === 1);
    expect((opened(app)[0].data as { url: string }).url).toContain("watch=1");
    expect((opened(app)[0].data as { url: string }).url).toContain("worker=w2");
    await app.host.send("app.viewer", { type: "connected", width: 1280, height: 800 });
    await app.waitFor("id:screen-watch-bar");
    expect(app.shows("label:Watching Claude")).toBe(true);
    expect(app.shows("label:Show keyboard")).toBe(false);
    // The agent finishes: the computer ends the watch, the page looks again and opens the screen.
    status = { ...base };
    await app.host.send("app.socketEnd", { reason: "agent finished" });
    await app.waitUntil(() => opened(app).length === 2);
    expect((opened(app)[1].data as { url: string }).url).not.toContain("watch=1");
    expect(app.shows("id:screen-lost")).toBe(false);
  });

  test("an older computer (no video in its status) is told to update before any viewer opens", async () => {
    const { video: _, ...old } = base;
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json(old) }));
    await app.waitFor(`text:${SCREEN_TOO_OLD}`);
    await new Promise((resolve) => setTimeout(resolve, 300));
    expect(opened(app).length).toBe(0);
  });

  test("an older computer (its RFB stream's first bytes) is told to update, once, also while watching", async () => {
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json({ ...base, agent: codex, agents: [codex] }) }));
    await app.waitFor("label:Watch Codex");
    await app.press("label:Watch Codex");
    await app.waitUntil(() => opened(app).length === 1);
    await app.host.send("app.socketBytes", { text: "RFB 003.008\n" });
    await app.waitFor(`text:${SCREEN_TOO_OLD}`);
    await new Promise((resolve) => setTimeout(resolve, 300));
    expect(opened(app).length).toBe(1);
  });

  test("Take over opens the screen", async () => {
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json({ ...base, agent: codex, agents: [codex] }) }));
    await app.waitFor("id:take-over");
    await app.press("id:take-over");
    await app.waitUntil(() => opened(app).length === 1);
    expect((opened(app)[0].data as { url: string }).url).not.toContain("watch=1");
  });

  test("an agent using the iPhone has its own row: Done tells it, Watch opens iPhone use", async () => {
    const iphone = { enabled: true, state: "ready", summary: "", active: { worker: "w9", name: "Gemini", since: "2026-11-11T08:00:00Z" } };
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json({ ...base, agent: codex, agents: [codex] }), [`GET ${IPHONE}`]: json(iphone), [`POST ${IPHONE}`]: json(iphone) }));
    await app.waitFor("text:Gemini is using your iPhone");
    await app.press("label:Done: tell the agent you finished on your iPhone");
    await app.waitUntil(() => posted(app, IPHONE).length > 0);
    expect(posted(app, IPHONE)[0]).toEqual({ handoff_done: true });
    await app.press("label:Stop Gemini on your iPhone");
    await app.waitUntil(() => posted(app, IPHONE).length > 1);
    expect(posted(app, IPHONE)[1]).toEqual({ stop_agent: true });
    await app.press("label:Watch Gemini on your iPhone");
    await app.waitFor("text:iPhone use");
  });

  test("on the computer itself: the agents with Stop, the iPhone row, and keep awake in the menu", async () => {
    const iphone = { enabled: true, state: "ready", summary: "", active: { worker: "w9", name: "Gemini", since: "2026-11-11T08:00:00Z" } };
    const local = { ...base, local: true, keep_awake: false, agent: codex, agents: [codex] };
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json(local), [`POST ${SCREEN}`]: json({ ...local, keep_awake: true }), [`GET ${IPHONE}`]: json(iphone) }));
    await app.waitFor("id:screen-local");
    expect(app.shows("label:Stop Codex")).toBe(true);
    await app.waitFor("text:Gemini is using your iPhone");
    expect(opened(app)).toHaveLength(0);
    await app.press("id:screen-options");
    await app.press("text:Keep this computer awake");
    await app.waitUntil(() => posted(app, SCREEN).length > 0);
    expect(posted(app, SCREEN)[0]).toEqual({ keep_awake: true });
  });

  test("an open menu and the agent card's buttons survive the computer being asked again", async () => {
    const app = await open(HARNESS, { ...options({ [`GET ${SCREEN}`]: () => json({ ...base, keep_awake: false, agent: codex, agents: [codex] }), [`POST ${SCREEN}`]: json({ ...base, agent: codex, agents: [codex] }) }), clock: {} });
    await app.waitFor("id:screen-agents");
    await app.press("id:screen-options");
    await app.wait(3_500);
    await app.press("text:Keep this computer awake");
    await app.waitUntil(() => posted(app, SCREEN).length > 0);
    expect(posted(app, SCREEN)[0]).toEqual({ keep_awake: true });
  });

  test("where phone use is on, the menu has Phone with who is using it", async () => {
    const phone = { supported: true, enabled: true, agent: "Claude" };
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json({ ...base, agent: codex, agents: [codex] }) }, { platform: "android", phone }));
    await app.waitFor("id:screen-agents");
    await app.press("id:screen-options");
    await app.waitFor("id:screen-phone");
    expect(app.texts()).toContain("Claude");
  });

  test("where phone use is off, the menu has no Phone", async () => {
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json({ ...base, agent: codex, agents: [codex] }) }, { platform: "android", phone: { supported: true, enabled: false } }));
    await app.waitFor("id:screen-agents");
    await app.press("id:screen-options");
    await app.waitFor("text:Let this computer sleep");
    expect(app.shows("id:screen-phone")).toBe(false);
  });
});

describe("Screen: the live viewer", () => {
  test("leaving the page closes the viewer", async () => {
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json(base) }, { below: { name: "settings-phone" } }));
    await goLive(app);
    expect(closed(app)).toHaveLength(0);
    await app.back();
    await app.waitUntil(() => closed(app).length === 1);
  });

  test("the soft keyboard's field goes back to empty after each key, so a key is sent once (screen [67])", async () => {
    const app = await open(HARNESS, { ...options({ [`GET ${SCREEN}`]: json(base) }), platform: "android" as const });
    await goLive(app);
    await app.press("label:Show keyboard");
    await app.waitFor("id:remote-key-bar");
    await app.type("id:remote-keyboard", "a");
    await app.waitUntil(() => app.value("id:remote-keyboard") === KEYBOARD_SENTINEL);
    await app.type("id:remote-keyboard", "b");
    await app.waitUntil(() => app.value("id:remote-keyboard") === KEYBOARD_SENTINEL);
  });

  test("a switch of computer closes the viewer", async () => {
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json(base), "GET http://laptop.test:9877/screen?video=webrtc": json({ ...base, name: "Laptop" }) }));
    await goLive(app);
    await app.host.send("app.daemonSwitch", { id: "s2", name: "Laptop", url: "ws://laptop.test:9877/ws" });
    await app.waitUntil(() => closed(app).length === 1);
    await app.waitUntil(() => opened(app, "ws://laptop.test:9877/screen/viewer").length === 1);
  });

  test("away from the app the viewer closes; back, it looks first and waits for an agent instead of taking over", async () => {
    let status: Record<string, unknown> = { ...base };
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: () => json(status) }));
    await goLive(app);
    await app.setLifecycle("background");
    await app.waitUntil(() => closed(app).length === 1);
    status = { ...base, agent: codex, agents: [codex] };
    await app.setLifecycle("active");
    await app.waitFor("text:Codex is using the screen");
    expect(opened(app)).toHaveLength(1);
  });

  test("Reconnect looks first: with an agent at work it shows the agent card", async () => {
    let status: Record<string, unknown> = { ...base };
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: () => json(status) }));
    await goLive(app);
    await app.host.send("app.socketEnd", { reason: "screen closed" });
    await app.waitFor("id:screen-lost");
    expect(app.texts()).toContain("screen closed");
    status = { ...base, agent: codex, agents: [codex] };
    await app.press("id:screen-reconnect");
    await app.waitFor("text:Codex is using the screen");
    expect(opened(app)).toHaveLength(1);
  });

  test("the connection dot opens the diagnostics; a computer that may sleep offers Keep awake", async () => {
    const status = { ...base, keep_awake: false, power: { source: "ac", percent: 80, keeping_awake: false } };
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json(status), [`POST ${SCREEN}`]: json({ ...status, keep_awake: true }) }));
    await goLive(app);
    expect(app.shows("label:Connection needs attention")).toBe(true);
    await app.host.send("app.viewer", { type: "stats", fps: 24, kbps: 1800, inputMs: 45, width: 1280, height: 800 });
    await app.host.send("app.socketPush", { message: { type: "stats", path: "relay", route: "lan", transport: "webrtc", updates: 24, bytes: 200000, encode_ms: 6, encode_max_ms: 9, kbps: 3000 } });
    await app.press("id:screen-indicator");
    await app.waitFor("text:Connection");
    const texts = app.texts();
    for (const text of ["Route", "LAN", "Network", "To screen", "45 ms", "Frame rate", "24 fps", "Data", "1.8 Mb/s", "Computer", "This computer may sleep"]) expect(texts).toContain(text);
    expect(texts.some((t) => t.startsWith("Charger · 80%"))).toBe(true);
    await app.press("id:screen-keep-awake");
    await app.waitUntil(() => posted(app, SCREEN).length > 0);
    expect(posted(app, SCREEN)[0]).toEqual({ keep_awake: true });
  });

  test("a slow connection that lasts lights the dot and is said once in the header", async () => {
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json(base) }, { rttMs: 400 }));
    await goLive(app);
    await app.waitFor("id:screen-slow-note", { timeout: 9_000 });
    expect(app.texts()).toContain("Slow connection");
    expect(app.shows("label:Connection needs attention")).toBe(true);
    await app.press("id:screen-indicator");
    await app.waitFor("text:Connection");
    expect(app.texts().filter((t) => t === "Slow connection").length).toBeGreaterThanOrEqual(1);
  });

  test("a locked computer shows a lock with its own label", async () => {
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json({ ...base, locked: true }) }));
    await app.waitUntil(() => opened(app).length > 0);
    await app.host.send("app.viewer", { type: "connected", width: 1280, height: 800 });
    await app.waitFor("label:Computer is locked, connection details");
  });

  test("held sideways the picture owns the display: the controls stand in a rail beside it", async () => {
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json(base) }));
    await goLive(app);
    expect(app.shows("id:screen-rail")).toBe(false);
    await app.setViewport(844, 390);
    await app.waitFor("id:screen-rail");
    expect(app.shows("id:screen-rail-options")).toBe(true);
    await app.setViewport(390, 844);
    await app.waitUntil(() => !app.shows("id:screen-rail"));
    expect(opened(app)).toHaveLength(1);
  });

  test("the menu disconnects while live and reconnects after", async () => {
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json(base) }));
    await goLive(app);
    await app.press("id:screen-options");
    await app.press("text:Disconnect");
    await app.waitFor("id:screen-lost");
    await app.press("id:screen-options");
    // The menu names the computer it is about.
    expect(app.texts()).toContain("Studio");
    await app.press("id:screen-menu-reconnect");
    await app.waitUntil(() => opened(app).length === 2);
  });
});

describe("Screen: Linux agent displays", () => {
  const linux = { ...base, platform: "linux", supported: false, ready: false, agent_displays: true };
  const display = (id: string, extra: Record<string, unknown> = {}) => ({ id, width: 1280, height: 800, control: "agent", apps: 1, ...extra });
  const DISPLAY_VIEWER = "ws://desk.test:9877/display/viewer";

  test("a Linux computer lists each agent's display, or says nothing is on screen", async () => {
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json(linux), [`GET ${DISPLAY}`]: json({ displays: [], available: true, video: "webrtc" }) }));
    await app.waitFor("text:Nothing on screen");
  });

  test("on a phone, the Linux page's options show only when they hold the Phone row (screen [72])", async () => {
    const network = { [`GET ${SCREEN}`]: json(linux), [`GET ${DISPLAY}`]: json({ displays: [], available: true, video: "webrtc" }) };
    const off = await open(HARNESS, options(network, { platform: "android", phone: { supported: true, enabled: false } }));
    await off.waitFor("text:Nothing on screen");
    expect(off.shows("id:screen-options")).toBe(false);
  });

  test("on a phone with phone use on, the Linux page's options hold the Phone row", async () => {
    const network = { [`GET ${SCREEN}`]: json(linux), [`GET ${DISPLAY}`]: json({ displays: [], available: true, video: "webrtc" }) };
    const on = await open(HARNESS, options(network, { platform: "android", phone: { supported: true, enabled: true } }));
    await on.waitFor("id:screen-options");
    // The button shows once Phone use is read; a press before its first layout lands nowhere.
    for (let tries = 0; !on.shows("id:screen-phone") && tries < 5; tries += 1) {
      await on.press("id:screen-options");
      await on.waitFor("id:screen-phone", { timeout: 1_000 }).catch(() => {});
    }
    expect(on.shows("id:screen-phone")).toBe(true);
  });

  test("a first read that fails ends the wait and explains it", async () => {
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json(linux), [`GET ${DISPLAY}`]: { status: 500, headers: { "content-type": "application/json" }, body: JSON.stringify({ error: "boom" }) } }));
    await app.waitFor("text:That didn't finish");
    expect(app.texts()).toContain("Nothing on screen");
  });

  test("the stage has one switch: Take over holds the agent (a red frame), Give back lets it go; a Stop that fails says so", async () => {
    const app = await open(HARNESS, options({
      [`GET ${SCREEN}`]: json(linux),
      [`GET ${DISPLAY}`]: json({ displays: [display("w1", { name: "Sign in to Acme (mh-17b3337d43f38037916f0e5b)" })], available: true, video: "webrtc" }),
      [`POST ${DISPLAY}`]: { status: 500, headers: { "content-type": "application/json" }, body: JSON.stringify({ error: "halt failed" }) },
    }, { platform: "android" }));
    await app.waitFor("id:display-w1");
    // The Worker's name, never the daemon's Session id.
    await app.waitFor("text:Sign in to Acme");
    expect(app.texts().some((t) => t.includes("mh-"))).toBe(false);
    await app.waitUntil(() => opened(app, DISPLAY_VIEWER).length === 1);
    await app.host.send("app.viewer", { type: "connected", width: 1280, height: 800 });
    await pressWhenEnabled(app, "id:stage-take-over");
    await app.waitFor("id:stage-held-frame");
    expect(sent(app)).toContainEqual({ type: "hold", on: true });
    expect(app.shows("label:Give back")).toBe(true);
    await app.press("label:Give back");
    await app.waitUntil(() => !app.shows("id:stage-held-frame"));
    expect(sent(app)).toContainEqual({ type: "hold", on: false });
    await app.press("label:Stop Sign in to Acme");
    await app.waitFor("id:displays-issue");
    expect(app.texts()).toContain("That didn't finish");
    expect(posted(app, DISPLAY)[0]).toEqual({ action: "halt", worker: "w1" });
  });

  test("taken over from a phone, the agent's page is laid out for the phone: the view's size goes once, the keyboard never re-lays it, and the real host shows", async () => {
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json(linux), [`GET ${DISPLAY}`]: json({ displays: [display("w1", { browser: 1, apps: 0 })], available: true, video: "webrtc" }) }, { platform: "android" }));
    await app.waitUntil(() => opened(app, DISPLAY_VIEWER).length === 1);
    await app.host.send("app.viewer", { type: "connected", width: 1280, height: 800 });
    await app.host.send("app.viewer", { type: "size", width: 412, height: 760, dpr: 2.625 });
    // Watching never lays it out.
    expect(sent(app).filter((m) => (m as { type?: string }).type === "fit")).toHaveLength(0);
    await pressWhenEnabled(app, "id:stage-take-over");
    await app.waitUntil(() => sent(app).some((m) => (m as { type?: string }).type === "fit"));
    expect(sent(app)).toContainEqual({ type: "fit", width: 412, height: 760, dpr: 2.625 });
    await app.host.send("app.socketPush", { message: { type: "reflow", on: true, host: "www.github.com" } });
    await app.waitFor("id:stage-domain");
    expect(app.texts()).toContain("github.com");
    // The keyboard comes up: the view gets shorter, and nothing is laid out again.
    await app.host.send("app.viewer", { type: "size", width: 412, height: 430, dpr: 2.625 });
    await app.wait(100);
    expect(sent(app).filter((m) => (m as { type?: string }).type === "fit")).toHaveLength(1);
    // Turned sideways: laid out again.
    await app.host.send("app.viewer", { type: "size", width: 760, height: 412, dpr: 2.625 });
    await app.waitUntil(() => sent(app).filter((m) => (m as { type?: string }).type === "fit").length === 2);
    await app.press("label:Give back");
    await app.waitUntil(() => !app.shows("id:stage-domain"));
  });

  test("leaving the page closes the display, which lets a held agent go on; only the agent shown streams", async () => {
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json(linux), [`GET ${DISPLAY}`]: json({ displays: [display("w1"), display("w2")], available: true, video: "webrtc" }) }, { below: { name: "settings-phone" } }));
    await app.waitUntil(() => opened(app, DISPLAY_VIEWER).length === 1);
    await app.wait(200);
    expect(opened(app, DISPLAY_VIEWER)).toHaveLength(1);
    await app.back();
    await app.waitUntil(() => closed(app, DISPLAY_VIEWER).length === 1);
  });

  test("a switch of computer closes every display", async () => {
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json(linux), [`GET ${DISPLAY}`]: json({ displays: [display("w1")], available: true, video: "webrtc" }), "GET http://laptop.test:9877/screen?video=webrtc": json(base) }));
    await app.waitUntil(() => opened(app, DISPLAY_VIEWER).length === 1);
    await app.host.send("app.daemonSwitch", { id: "s2", name: "Laptop", url: "ws://laptop.test:9877/ws" });
    await app.waitUntil(() => closed(app, DISPLAY_VIEWER).length === 1);
  });

  test("away from the app every display closes; back, they reconnect", async () => {
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json(linux), [`GET ${DISPLAY}`]: json({ displays: [display("w1")], available: true, video: "webrtc" }) }));
    await app.waitUntil(() => opened(app, DISPLAY_VIEWER).length === 1);
    await app.setLifecycle("background");
    await app.waitUntil(() => closed(app, DISPLAY_VIEWER).length === 1);
    await app.setLifecycle("active");
    await app.waitUntil(() => opened(app, DISPLAY_VIEWER).length === 2);
  });

  test("the route's worker opens Screen on that agent's display; the chip switches to another", async () => {
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json(linux), [`GET ${DISPLAY}`]: json({ displays: [display("w1", { name: "Research" }), display("w2", { name: "SignIn" })], available: true, video: "webrtc" }) }, { worker: "w2", platform: "android" }));
    await app.waitFor("id:display-w2");
    expect(app.shows("id:display-w1")).toBe(false);
    await app.press("id:stage-chip");
    await app.waitFor("id:stage-agent-w1");
    await app.press("id:stage-agent-w1");
    await app.waitFor("id:display-w1");
    await app.waitUntil(() => opened(app, DISPLAY_VIEWER).length === 2 && closed(app, DISPLAY_VIEWER).length === 1);
  });
});

describe("Screen: a Mac's agent browser windows", () => {
  const window = (id: string, name: string, extra: Record<string, unknown> = {}) => ({ id, name, width: 800, height: 500, control: "agent", apps: 0, browser: 1, idle_s: 3, ...extra });
  const DISPLAY_VIEWER = "ws://desk.test:9877/display/viewer";

  test("a Chrome-only Worker's window is listed first, by name, live; the Mac's own screen is one tap away and back", async () => {
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json(base), [`GET ${DISPLAY}`]: json({ displays: [window("mh-signin", "SignIn")], available: true, video: "webrtc" }) }));
    await app.waitFor("id:display-mh-signin");
    expect(app.texts()).toContain("SignIn");
    await app.waitUntil(() => opened(app, DISPLAY_VIEWER).length === 1);
    expect((opened(app, DISPLAY_VIEWER)[0].data as { url: string }).url).toContain("worker=mh-signin");
    // Nothing of the Mac's own screen opens under the agents' windows.
    expect(opened(app)).toHaveLength(0);
    await app.press("id:screen-options");
    await app.press("id:displays-own-screen");
    await app.waitUntil(() => opened(app).length === 1);
    await app.waitUntil(() => closed(app, DISPLAY_VIEWER).length === 1);
    await app.host.send("app.viewer", { type: "connected", width: 1280, height: 800 });
    await app.waitFor("id:screen-indicator");
    await app.press("id:screen-options");
    await app.waitFor("id:screen-agent-windows");
    expect(app.texts()).toContain("Agent's browser window");
    await app.press("id:screen-agent-windows");
    await app.waitFor("id:display-mh-signin");
    await app.waitUntil(() => closed(app).length === 1);
    await app.waitUntil(() => opened(app, DISPLAY_VIEWER).length === 2);
  });

  test("opened for a Worker (its Work card's Screen), it is at that Worker's window", async () => {
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json(base), [`GET ${DISPLAY}`]: json({ displays: [window("mh-a", "Research"), window("mh-signin", "SignIn")], available: true, video: "webrtc" }) }, { worker: "mh-signin" }));
    await app.waitFor("id:display-mh-signin");
    expect(app.shows("id:display-mh-a")).toBe(false);
    expect(opened(app)).toHaveLength(0);
  });

  test("Take over holds the agent; its keyboard has the Mac's keys, Paste and the editing keys", async () => {
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json(base), [`GET ${DISPLAY}`]: json({ displays: [window("mh-signin", "SignIn")], available: true, video: "webrtc" }) }, { platform: "android" }));
    await app.waitFor("id:display-mh-signin");
    await app.waitUntil(() => opened(app, DISPLAY_VIEWER).length === 1);
    await app.host.send("app.viewer", { type: "connected", width: 800, height: 500 });
    await pressWhenEnabled(app, "id:stage-take-over");
    await app.waitFor("id:stage-held-frame");
    expect(sent(app)).toContainEqual({ type: "hold", on: true });
    await app.press("label:Show keyboard");
    await app.waitFor("text:⌘");
    expect(app.shows("id:key-paste")).toBe(true);
    expect(app.shows("label:Backspace")).toBe(true);
    // Typing docks the key bar: the switch steps aside.
    expect(app.shows("id:stage-bar")).toBe(false);
  });

  test("the last window closing hands the page to the Mac's own screen", async () => {
    let list = [window("mh-signin", "SignIn")];
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json(base), [`GET ${DISPLAY}`]: () => json({ displays: list, available: true, video: "webrtc" }) }));
    await app.waitFor("id:display-mh-signin");
    list = [];
    await app.waitUntil(() => opened(app).length === 1, { timeout: 10_000 });
    await app.waitUntil(() => closed(app, DISPLAY_VIEWER).length === 1);
  });

  test("a window that opens while Screen is up shows at once, also before this device may view the Mac", async () => {
    let list: unknown[] = [];
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json({ ...base, approved: false, ready: false }), [`GET ${DISPLAY}`]: () => json({ displays: list, available: true, video: "webrtc" }) }));
    await app.waitFor("id:screen-enable");
    list = [window("mh-signin", "SignIn")];
    await app.waitFor("id:display-mh-signin", { timeout: 6_000 });
    expect(app.texts()).toContain("SignIn");
  });

  test("a Mac without agent windows (or an older Mewla) opens its own screen as before", async () => {
    const none = await open(HARNESS, options({ [`GET ${SCREEN}`]: json(base), [`GET ${DISPLAY}`]: json({ displays: [], available: true, video: "webrtc" }) }));
    await none.waitUntil(() => opened(none).length === 1);
    const older = await open(HARNESS, options({ [`GET ${SCREEN}`]: json(base), [`GET ${DISPLAY}`]: { status: 404, headers: { "content-type": "text/plain" }, body: "404 page not found" } }));
    await older.waitUntil(() => opened(older).length === 1);
  });
});

describe("Screen in a browser", () => {
  const web = { platform: "web" as const, width: 1280, height: 800 };
  const linux = { ...base, platform: "linux", supported: false, ready: false, agent_displays: true };
  const DISPLAY_VIEWER = "ws://desk.test:9877/display/viewer";

  test("the browser's own keyboard types into the picture: no keyboard button, no hidden field", async () => {
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json(base) }, web));
    await goLive(app);
    expect(app.shows("id:remote-view")).toBe(true);
    expect(app.shows("id:screen-keyboard")).toBe(false);
    expect(app.shows("id:remote-keyboard")).toBe(false);
    expect(app.shows("id:remote-key-bar")).toBe(false);
  });

  test("Full size asks the page to show the picture full size, with the keyboard locked (parity #29)", async () => {
    const opts = options({ [`GET ${SCREEN}`]: json(base) }, web);
    const app = await open(HARNESS, { ...opts, host: { ...opts.host, handlers: { ...opts.host.handlers, "web.fullscreen": () => ({ keyboardLocked: true }) } } });
    await goLive(app);
    await app.press("id:screen-options");
    await app.press("text:Full size");
    await app.waitUntil(() => app.host.calls().some((call) => call.name === "web.fullscreen"));
    expect(app.host.calls().find((call) => call.name === "web.fullscreen")?.data).toEqual({ testID: "remote-view", label: "Screen of Studio" });
  });

  test("a wide window keeps its header and the picture at the top", async () => {
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json(base) }, { ...web, height: 420 }));
    await goLive(app);
    expect(app.shows("id:screen-rail")).toBe(false);
    expect(app.shows("id:screen-options")).toBe(true);
  });

  test("another window taking the focus keeps the viewer; another tab closes it, and coming back looks again", async () => {
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json(base) }, web));
    await goLive(app);
    await app.setLifecycle("inactive");
    await app.wait(200);
    expect(closed(app)).toHaveLength(0);
    await app.setLifecycle("background");
    await app.waitUntil(() => closed(app).length === 1);
    await app.setLifecycle("active");
    await app.waitUntil(() => opened(app).length === 2);
  });

  test("the connection sheet opens from the dot, and Keep awake works there", async () => {
    const status = { ...base, keep_awake: false, power: { source: "ac", percent: 80, keeping_awake: false } };
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json(status), [`POST ${SCREEN}`]: json({ ...status, keep_awake: true }) }, web));
    await goLive(app);
    await app.host.send("app.viewer", { type: "stats", fps: 24, kbps: 1800, inputMs: 45, width: 1280, height: 800 });
    await app.press("id:screen-indicator");
    await app.waitFor("text:Connection");
    for (const text of ["Frame rate", "24 fps", "This computer may sleep"]) expect(app.texts()).toContain(text);
    await app.press("id:screen-keep-awake");
    await app.waitUntil(() => posted(app, SCREEN).length > 0);
    expect(posted(app, SCREEN)[0]).toEqual({ keep_awake: true });
  });

  test("a computer that can't share its screen doesn't send the browser to another one", async () => {
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json({ ...base, supported: false, platform: "freebsd", ready: false }) }, web));
    await app.waitFor("text:Screen needs a Mac, Windows or Linux computer");
    expect(app.texts()).toContain("Open Mewla from a Mac, Windows or Linux computer.");
    expect(app.texts()).not.toContain("Choose a computer");
  });

  test("an agent at work: no iPhone row, as a browser has no iPhone use", async () => {
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json({ ...base, agent: codex, agents: [codex] }), [`GET ${IPHONE}`]: json({ enabled: true, active: { worker: "w9", name: "Gemini" } }) }, web));
    await app.waitFor("text:Codex is using the screen");
    await app.wait(200);
    expect(app.requests().some((r) => r.url === IPHONE)).toBe(false);
    expect(app.texts()).not.toContain("Gemini is using your iPhone");
  });

  test("agent displays: no keyboard button on a card; another window's focus keeps them", async () => {
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json(linux), [`GET ${DISPLAY}`]: json({ displays: [{ id: "w1", width: 1280, height: 800, control: "agent", apps: 1 }], available: true, video: "webrtc" }) }, web));
    await app.waitUntil(() => opened(app, DISPLAY_VIEWER).length === 1);
    await app.host.send("app.viewer", { type: "connected", width: 1280, height: 800 });
    await app.waitFor("id:stage-take-over-bar");
    expect(app.shows("label:Show keyboard")).toBe(false);
    await app.setLifecycle("inactive");
    await app.wait(200);
    expect(closed(app, DISPLAY_VIEWER)).toHaveLength(0);
  });
});
