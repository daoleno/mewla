import { describe, expect, open, test } from "nsh/test";

const HARNESS = "../testing/Harness.tsx";
const json = (body: unknown) => ({ status: 200, headers: { "content-type": "application/json" }, body: JSON.stringify(body) });

function secureStore() {
  const secure = new Map<string, string>();
  return {
    "secure.get": ({ key }: { key: string }) => secure.get(key) ?? null,
    "secure.set": ({ key, value }: { key: string; value: string }) => void secure.set(key, value),
    "secure.remove": ({ key }: { key: string }) => void secure.delete(key),
  };
}

type Answer = (type: string, body: Record<string, unknown>) => unknown;

function setup(params: Record<string, unknown>, answer: Answer, extra: Record<string, unknown> = {}) {
  const asked: Array<{ type: string; body: Record<string, unknown> }> = [];
  return {
    asked,
    options: {
      ...extra,
      host: {
        params,
        handlers: {
          ...secureStore(),
          "app.daemon": ({ type, body }: { type: string; body: Record<string, unknown> }) => {
            asked.push({ type, body });
            return answer(type, body) ?? { error: `unexpected ${type}` };
          },
        },
      },
    },
  };
}

const desk = { id: "s1", name: "Desk", url: "ws://desk.test:9877/ws", daemonId: "a".repeat(64), daemonPublicKey: "b".repeat(64), transportKind: "manual" };
const studio = { id: "s2", name: "Studio", url: "ws://studio.test:9877/ws", daemonId: "c".repeat(64), daemonPublicKey: "d".repeat(64), transportKind: "manual" };
const twoComputers = { "mewla:v3:servers": JSON.stringify([desk, studio]) };
const none = () => null;

describe("Computers", () => {
  test("removing a computer tells the others to forget it, and names the ones that did not answer", async () => {
    const s = setup({ page: { name: "settings" }, servers: [studio] }, none, {
      storage: twoComputers,
      network: {
        "GET http://desk.test:9877/auth-check": { status: 503 },
        "GET http://studio.test:9877/auth-check": { status: 503 },
        "DELETE http://desk.test:9877/computers": { status: 503, body: "busy" },
        "DELETE http://studio.test:9877/devices": { status: 200, body: "{}" },
      },
    });
    const app = await open(HARNESS, s.options);
    app.onDialog((dialog) => (dialog.title === "Remove Studio?" ? "Remove" : null));
    await app.press("id:computer-s2");
    await app.waitFor("id:computer-remove");
    await app.press("id:computer-remove");
    await app.waitFor("id:removed-notice");
    expect(app.texts()).toContain("Desk didn't answer");
    expect(app.texts()).toContain('It still works with Studio. On it, run mewla computers remove "Studio".');
    const told = app.requests().find((r) => r.method === "DELETE" && r.url === "http://desk.test:9877/computers");
    expect(JSON.parse(String(told?.body))).toEqual({ expected_daemon_id: studio.daemonId });
    expect(app.requests().some((r) => r.method === "DELETE" && r.url === "http://studio.test:9877/devices")).toBe(true);
    expect(app.shows("id:computer-s2")).toBe(false);
    expect(app.storage()["mewla:v3:servers"]).not.toContain("studio.test");
  });

  test("when every other computer answers, nothing is left to say", async () => {
    const s = setup({ page: { name: "settings" }, servers: [studio] }, none, {
      storage: twoComputers,
      network: {
        "GET http://desk.test:9877/auth-check": { status: 503 },
        "GET http://studio.test:9877/auth-check": { status: 503 },
        "DELETE http://desk.test:9877/computers": { status: 200, body: "{}" },
        "DELETE http://studio.test:9877/computers": { status: 200, body: "{}" },
        "DELETE http://studio.test:9877/devices": { status: 200, body: "{}" },
      },
    });
    const app = await open(HARNESS, s.options);
    app.onDialog(() => "Remove");
    await app.press("id:computer-s2");
    await app.press("id:computer-remove");
    await app.waitUntil(() => !app.shows("id:computer-s2") && app.shows("id:pair-another"));
    // Studio forgets Desk too, both ways.
    expect(app.requests().some((r) => r.method === "DELETE" && r.url === "http://studio.test:9877/computers")).toBe(true);
    expect(app.shows("id:removed-notice")).toBe(false);
  });

  test("each return to Settings asks the computers again", async () => {
    const s = setup({ page: { name: "settings" } }, none, { network: { "GET http://desk.test:9877/auth-check": { status: 503 } } });
    const app = await open(HARNESS, s.options);
    const probes = () => app.requests().filter((r) => r.url === "http://desk.test:9877/auth-check").length;
    await app.waitUntil(() => probes() === 1);
    await app.press("id:settings-pet");
    await app.waitFor("id:pet-page");
    await app.back();
    await app.waitUntil(() => probes() === 2);
    await app.setLifecycle("background");
    await app.setLifecycle("active");
    await app.waitUntil(() => probes() === 3);
  });

  test("the computer in use shows how the phone reaches it; a long press says more", async () => {
    const s = setup({ page: { name: "settings" }, server: { url: "ws://192.168.1.20:9877/ws" } }, none, {
      network: { "GET http://192.168.1.20:9877/auth-check": { status: 503 } },
    });
    const app = await open(HARNESS, s.options);
    await app.waitFor("id:computer-path");
    await app.longPress("id:computer-s1");
    await app.waitFor("text:LAN · 192.168.1.20");
    await app.host.send("app.daemonState", "waiting");
    await app.waitUntil(() => !app.shows("id:computer-path"));
  });
});

describe("Computer page", () => {
  test("the address shows as typed, and a failed look at agent use says why", async () => {
    const s = setup({ page: { name: "settings-computer", id: "s1" } }, none, {
      network: {
        "GET http://desk.test:9877/auth-check": { status: 503 },
        "GET http://desk.test:9877/agent-use": { status: 500, body: "Agent use is not ready here" },
        "GET http://desk.test:9877/daemon-update": { status: 404 },
      },
    });
    const app = await open(HARNESS, s.options);
    await app.waitFor("id:computer-address");
    expect(app.value("id:computer-address")).toBe("http://desk.test:9877");
    await app.waitFor("text:Agent use is not ready here");
    expect(app.enabled("id:agent-use")).toBe(false);
  });

  test("a connection that fails says what is wrong and how to fix it", async () => {
    const s = setup({ page: { name: "settings-computer", id: "s1" }, state: "waiting" }, none, {
      network: { "GET http://desk.test:9877/auth-check": { status: 503 }, "GET http://desk.test:9877/health": { status: 502 } },
    });
    const app = await open(HARNESS, s.options);
    await app.host.send("app.sessionProblem", "closed (1006)");
    await app.waitFor("text:Proxy could not reach daemon");
    expect(app.texts()).toContain("Check that mewla is running on 127.0.0.1:9876 and that your tunnel points at the same local port.");
    expect(app.texts()).toContain("Try again");
  });

  test("a computer that forgot this phone asks to pair again", async () => {
    const s = setup({ page: { name: "settings-computer", id: "s1" }, state: "waiting" }, none, {
      network: { "GET http://desk.test:9877/auth-check": { status: 401 } },
    });
    const app = await open(HARNESS, s.options);
    await app.waitFor("id:computer-pair-again");
    expect(app.texts()).toContain("This computer no longer knows this phone.");
  });

  test("an address that is not one says so and keeps what was typed", async () => {
    const s = setup({ page: { name: "settings-computer", id: "s1" } }, none, { network: { "GET http://desk.test:9877/auth-check": { status: 503 } } });
    const app = await open(HARNESS, s.options);
    await app.waitFor("id:computer-address");
    await app.clear("id:computer-address");
    await app.type("id:computer-address", "desk");
    await app.submit("id:computer-address");
    await app.waitFor("text:Check the address");
    expect(app.value("id:computer-address")).toBe("desk");
  });
});

const bound = { state: "connected", enabled: true, bot_username: "mewla_bot", owner_hint: "@me", binding_pending: false };

describe("Telegram", () => {
  test("a status that did not load offers Retry, never the token form", async () => {
    let fail = true;
    const s = setup({ page: { name: "settings-telegram" } }, (type) => {
      if (type !== "telegram_connection_status") return null;
      return fail ? { error: "Telegram is not configured on this daemon build" } : { connection: bound };
    });
    const app = await open(HARNESS, s.options);
    await app.waitFor("id:telegram-retry");
    expect(app.shows("id:telegram-token")).toBe(false);
    fail = false;
    await app.press("id:telegram-retry");
    await app.waitFor("id:telegram-open");
    expect(app.shows("id:telegram-retry")).toBe(false);
  });

  test("away from the computer nothing can be changed, but the chat still opens", async () => {
    const s = setup({ page: { name: "settings-telegram" } }, (type) => (type === "telegram_connection_status" ? { connection: bound } : null));
    const app = await open(HARNESS, s.options);
    await app.waitFor("id:telegram-disconnect");
    expect(app.enabled("id:telegram-disconnect")).toBe(true);
    await app.host.send("app.daemonState", "waiting");
    await app.waitFor("id:telegram-offline");
    expect(app.enabled("id:telegram-disconnect")).toBe(false);
    expect(app.enabled("id:telegram-open")).toBe(true);
  });

  test("a token the computer refused says why", async () => {
    const s = setup({ page: { name: "settings-telegram" } }, (type) => {
      if (type === "telegram_connection_status") return { connection: { state: "disabled", enabled: false, binding_pending: false } };
      if (type === "telegram_connection_configure") return { error: "Telegram rejected the token: Unauthorized" };
      return null;
    });
    const app = await open(HARNESS, s.options);
    await app.waitFor("id:telegram-token");
    await app.type("id:telegram-token", "123:bad");
    await app.press("id:telegram-verify");
    await app.waitFor("text:Telegram setup failed");
    expect(app.texts()).toContain("Telegram rejected the token: Unauthorized");
  });

  test("a disconnected bot can still be opened from its row", async () => {
    const s = setup({ page: { name: "settings-telegram" } }, (type) => (type === "telegram_connection_status" ? { connection: { ...bound, enabled: false } } : null));
    const app = await open(HARNESS, s.options);
    await app.waitFor("id:telegram-reconnect");
    await app.press("id:telegram-open-row");
    await app.waitUntil(() => app.openedUrls().includes("https://t.me/mewla_bot"));
  });
});

const connection = (id: string, name: string, extra: Record<string, unknown> = {}) => ({
  id,
  name,
  clients: ["codex"],
  credential_ready: true,
  advanced: true,
  base_url: `https://${id}.example.com/v1`,
  ...extra,
});
const catalog = (connections: unknown[], extra: Record<string, unknown> = {}) => ({ revision: 3, connections, defaults: { codex: { connection_id: "" } }, presets: [], models: {}, ...extra });
const applied = { persistence_outcome: "applied", persistence_durable: true };

describe("Model Providers", () => {
  test("a long list can be searched", async () => {
    const names = ["Alpha", "Bravo", "Charlie", "Delta", "Echo", "Zeta"];
    const s = setup({ page: { name: "settings-providers" } }, (type) => (type === "list_providers" ? catalog(names.map((n) => connection(n.toLowerCase(), n))) : null));
    const app = await open(HARNESS, s.options);
    await app.waitFor("id:provider-search");
    await app.type("id:provider-search", "zet");
    await app.waitUntil(() => !app.shows("id:provider-alpha"));
    expect(app.shows("id:provider-zeta")).toBe(true);
    await app.type("id:provider-search", "x");
    await app.waitFor("text:No Providers match this search.");
  });

  test("a short list has no search", async () => {
    const s = setup({ page: { name: "settings-providers" } }, (type) => (type === "list_providers" ? catalog([connection("a", "Alpha")]) : null));
    const app = await open(HARNESS, s.options);
    await app.waitFor("text:Alpha");
    expect(app.shows("id:provider-search")).toBe(false);
  });

  test("a new Provider with its key goes straight to its models", async () => {
    let current = catalog([]);
    const s = setup({ page: { name: "settings-providers" } }, (type) => {
      if (type === "list_providers") return current;
      if (type === "upsert_provider_connection") {
        current = catalog([connection("c9", "Home", { preset_id: "custom" })], { revision: 4 });
        return { ...current, ...applied };
      }
      if (type === "discover_provider_models") return { reply: "provider_models", connection_id: "c9", models: [{ id: "m1", display_name: "Model One", available: true, source: "upstream" }] };
      return null;
    });
    const app = await open(HARNESS, s.options);
    await app.waitFor("id:provider-add");
    await app.press("id:provider-add");
    await app.type("id:provider-name", "Home");
    await app.type("id:provider-url", "https://c9.example.com/v1");
    await app.type("id:provider-key", "sk-home");
    await app.press("id:provider-save");
    await app.waitFor("id:provider-models");
    await app.waitFor("text:Model One");
    expect(s.asked.filter((a) => a.type === "list_providers").length).toBeGreaterThan(1);
  });

  test("a new key that did not take opens the same form again to retry", async () => {
    const s = setup({ page: { name: "settings-providers" } }, (type) => {
      if (type === "list_providers") return catalog([connection("c2", "Spare", { credential_ready: false })]);
      if (type === "upsert_provider_connection") return { ...catalog([connection("c2", "Spare", { credential_ready: false })], { revision: 4 }), ...applied };
      return null;
    });
    const app = await open(HARNESS, s.options);
    await app.waitFor("id:provider-c2");
    await app.press("id:provider-c2");
    await app.waitFor("id:provider-key");
    await app.type("id:provider-key", "sk-wrong");
    await app.press("id:provider-save");
    await app.waitFor("id:provider-retry");
    expect(app.texts()).toContain("The API key was not saved. Check it and try again.");
  });

  test("a change whose outcome is unknown reads the list again by itself", async () => {
    const s = setup({ page: { name: "settings-providers" } }, (type) => {
      if (type === "list_providers") return catalog([connection("a", "Alpha")]);
      if (type === "set_provider_connection") return catalog([connection("a", "Alpha")], { revision: 4 });
      return null;
    });
    const app = await open(HARNESS, s.options);
    await app.waitFor("id:provider-a");
    await app.press("id:provider-a");
    await app.waitUntil(() => s.asked.filter((a) => a.type === "list_providers").length === 2);
    await app.waitUntil(() => !app.shows("id:providers-locked"));
  });

  test("an empty name says it is required", async () => {
    const s = setup({ page: { name: "settings-providers" } }, (type) => (type === "list_providers" ? catalog([]) : null));
    const app = await open(HARNESS, s.options);
    await app.waitFor("id:provider-add");
    await app.press("id:provider-add");
    await app.waitFor("text:Provider name is required.");
    expect(app.enabled("id:provider-save")).toBe(false);
  });
});

describe("Settings rows", () => {
  test("Telegram says it is unavailable while the computer is away", async () => {
    const s = setup({ page: { name: "settings" }, state: "waiting" }, none, { network: { "GET http://desk.test:9877/auth-check": { status: 503 } } });
    const app = await open(HARNESS, s.options);
    await app.waitFor("text:Unavailable");
  });

  test("license text can be selected and copied", async () => {
    const s = setup({ page: { name: "settings-licenses" } }, none);
    const app = await open(HARNESS, s.options);
    await app.press("text:noVNC");
    await app.waitFor("id:license-text");
  });
});

describe("Pet", () => {
  test("the preview acts out Brain's states, hopping out of the seal and back", async () => {
    const s = setup({ page: { name: "settings-pet" } }, none, { clock: {} });
    const app = await open(HARNESS, s.options);
    const clock = app.clock!;
    await app.waitFor("id:pet-clip-delegating");
    await clock.runFor(3_200);
    await app.waitFor("id:pet-clip-working");
    await clock.runFor(3_200);
    await app.waitFor("id:pet-clip-delivered");
    await clock.runFor(3_200);
    await app.waitFor("id:pet-clip-going_back");
    await clock.runFor(1_400);
    await app.waitFor("id:pet-clip-idle");
  });

  test("with less motion asked for, the pet stands still", async () => {
    const s = setup({ page: { name: "settings-pet" } }, none, { reducedMotion: true });
    const app = await open(HARNESS, s.options);
    await app.waitFor("id:pet-still");
  });
});

void json;
