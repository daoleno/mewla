import { describe, expect, open, test, type StubResponse } from "nsh/test";

const HARNESS = "../testing/Harness.tsx";
const SCREEN = "http://desk.test:9877/screen";
const IPHONE = "http://desk.test:9877/iphone";

function secureStore() {
  const secure = new Map<string, string>();
  return {
    "secure.get": ({ key }: { key: string }) => secure.get(key) ?? null,
    "secure.set": ({ key, value }: { key: string; value: string }) => void secure.set(key, value),
    "secure.remove": ({ key }: { key: string }) => void secure.delete(key),
  };
}

const json = (body: unknown) => ({ status: 200, headers: { "content-type": "application/json" }, body: JSON.stringify(body) });

const agents = [{ worker: "w1", name: "Codex", since: "2026-11-11T08:00:00Z" }];
const screen = { supported: true, platform: "darwin", name: "Studio", disabled: false, approved: true, ready: true, keep_awake: false, permissions: [], agent: agents[0], agents };
const iphoneFree = { enabled: true, state: "ready", summary: "Ready", running: true };
const iphoneBusy = { ...iphoneFree, active: { worker: "w2", name: "Writer", since: "2026-11-11T08:05:00Z" } };

function options(network: Record<string, StubResponse>, handlers: Record<string, unknown> = {}, platform?: "android") {
  return { host: { params: { page: { name: "screen" } }, handlers: { ...secureStore(), "app.daemon": () => ({ error: "not in this test" }), ...handlers } }, network, ...(platform ? { platform } : {}) };
}

describe("Screen and the person's phones", () => {
  test("an agent on the iPhone shows on Screen; Done tells the computer the person has finished", async () => {
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json(screen), [`GET ${IPHONE}`]: json(iphoneBusy), [`POST ${IPHONE}`]: json(iphoneFree) }));
    await app.waitFor("id:screen-iphone");
    expect(app.texts()).toContain("Writer is using your iPhone");
    await app.press("id:screen-iphone-done");
    await app.waitUntil(() => app.requests().some((r) => r.method === "POST" && r.url === IPHONE));
    expect(JSON.parse(String(app.requests().find((r) => r.method === "POST" && r.url === IPHONE)!.body))).toEqual({ handoff_done: true });
    await app.waitUntil(() => !app.shows("id:screen-iphone"));
  });

  test("Stop stops the agent on the iPhone, and Watch opens iPhone use", async () => {
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json(screen), [`GET ${IPHONE}`]: json(iphoneBusy), [`POST ${IPHONE}`]: json(iphoneBusy) }));
    await app.waitFor("id:screen-iphone-stop");
    await app.press("id:screen-iphone-stop");
    await app.waitUntil(() => app.requests().some((r) => r.method === "POST" && r.url === IPHONE));
    expect(JSON.parse(String(app.requests().find((r) => r.method === "POST" && r.url === IPHONE)!.body))).toEqual({ stop_agent: true });
    await app.press("id:screen-iphone-watch");
    await app.waitFor("id:iphone-page");
  });

  test("no agent on the iPhone, or a computer without iPhone use: no row", async () => {
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json(screen), [`GET ${IPHONE}`]: json(iphoneFree) }));
    await app.waitFor("id:screen-agents");
    await app.waitUntil(() => app.requests().some((r) => r.url.startsWith(IPHONE)));
    await app.wait(200);
    expect(app.shows("id:screen-iphone")).toBe(false);
  });

  test("while Phone use is on, Screen's menu lists the Phone with who uses it, and opens its page", async () => {
    const phone = {
      supported: true, sdk: 35, enabled: true, link: true, link_state: "online", developer_options: true, wireless_debugging: true, wifi: true, paired: true, helper: true,
      problem: null, accessibility: true, restricted: false, self_healing: true, guide: null, notifications: true, state: "ready", w: 720, h: 1600, display_info: null,
      agent: "Writer", handoff: null, drawn: true, opening: null, launch_failed: null,
    };
    const app = await open(HARNESS, options(
      { [`GET ${SCREEN}`]: json(screen), [`GET ${IPHONE}`]: json(iphoneFree), "POST http://desk.test:9877/phone": json({ enabled: true, connected: true }) },
      { "phone.status": () => phone, "phone.follow": null, "phone.setPipAuto": null },
      "android",
    ));
    // The menu is made from what the computer said: open it once that has come (the button is there before).
    await app.waitFor("text:Codex is using the screen");
    await app.press("id:screen-options");
    await app.waitFor("text:Phone");
    expect(app.texts()).toContain("Writer");
    await app.press("text:Phone");
    await app.waitFor("id:phone-page");
  });
});
