import { describe, expect, open, test, type StubResponse } from "nsh/test";

const HARNESS = "../testing/Harness.tsx";
const SCREEN = "http://desk.test:9877/screen";

function secureStore() {
  const secure = new Map<string, string>();
  return {
    "secure.get": ({ key }: { key: string }) => secure.get(key) ?? null,
    "secure.set": ({ key, value }: { key: string; value: string }) => void secure.set(key, value),
    "secure.remove": ({ key }: { key: string }) => void secure.delete(key),
  };
}

const json = (body: unknown) => ({ status: 200, headers: { "content-type": "application/json" }, body: JSON.stringify(body) });

const base = { supported: true, platform: "darwin", name: "Studio", disabled: false, approved: true, ready: true, keep_awake: false, permissions: [] };

function options(network: Record<string, StubResponse>) {
  return { host: { params: { page: { name: "screen" } }, handlers: { ...secureStore(), "app.daemon": () => ({ error: "not in this test" }) } }, network };
}

describe("Screen", () => {
  test("a computer that has not allowed this phone asks, and Turn on Screen sends enable", async () => {
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json({ ...base, approved: false, ready: false }), [`POST ${SCREEN}`]: json({ ...base, approved: false, ready: false }) }));
    await app.waitFor("id:screen-turn-on");
    expect(app.texts()).toContain("Turn on Screen");
    await app.press("id:screen-turn-on");
    await app.waitUntil(() => app.requests().some((r) => r.method === "POST" && r.url === SCREEN));
    const sent = app.requests().find((r) => r.method === "POST" && r.url === SCREEN)!;
    expect(JSON.parse(String(sent.body))).toEqual({ action: "enable" });
  });

  test("a screen turned off at the computer says how to turn it on", async () => {
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json({ ...base, disabled: true, ready: false }) }));
    await app.waitFor("text:Screen is off");
    expect(app.texts()).toContain("Run mewla screen enable on Studio.");
  });

  test("while an agent uses the screen, Watch, Stop and Take over are offered, and Stop tells the computer", async () => {
    const agents = [{ worker: "w1", name: "Codex", since: "2026-11-11T08:00:00Z" }];
    const app = await open(HARNESS, options({ [`GET ${SCREEN}`]: json({ ...base, agent: agents[0], agents }), [`POST ${SCREEN}`]: json({ ...base, agent: agents[0], agents }) }));
    await app.waitFor("id:screen-agents");
    expect(app.texts()).toContain("Codex is using the screen");
    expect(app.texts()).toContain("Take over");
    await app.press("text:Stop");
    await app.waitUntil(() => app.requests().some((r) => r.method === "POST" && r.url === SCREEN));
    expect(JSON.parse(String(app.requests().find((r) => r.method === "POST" && r.url === SCREEN)!.body))).toEqual({ action: "stop_agent", worker: "w1" });
  });

  test("a Linux computer without a screen offers the virtual screen, which it keeps", async () => {
    const app = await open(HARNESS, options({
      [`GET ${SCREEN}`]: json({ ...base, supported: false, platform: "linux", ready: false }),
      "POST http://desk.test:9877/browser": json({ virtual_screen: false }),
    }));
    await app.waitFor("id:virtual-screen");
    await app.waitUntil(() => app.enabled("id:virtual-screen"));
    await app.press("id:virtual-screen");
    await app.waitUntil(() => app.requests().filter((r) => r.url === "http://desk.test:9877/browser").length === 2);
    const last = app.requests().filter((r) => r.url === "http://desk.test:9877/browser").at(-1)!;
    expect(JSON.parse(String(last.body))).toEqual({ action: "settings", enabled: true });
  });

  test("a Linux computer lists each agent's display, or says nothing is on screen", async () => {
    const app = await open(HARNESS, options({
      [`GET ${SCREEN}`]: json({ ...base, platform: "linux", agent_displays: true }),
      "GET http://desk.test:9877/display": json({ displays: [], available: true }),
    }));
    await app.waitFor("text:Nothing on screen");
    expect(app.texts()).toContain("An agent's screen shows up here while it works.");
  });
});
