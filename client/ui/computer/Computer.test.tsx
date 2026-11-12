import { describe, expect, open, test } from "nsh/test";
import { answering } from "../testing/fixtures";

const HARNESS = "./testing/Harness.tsx";
const json = (body: unknown) => ({ status: 200, headers: { "content-type": "application/json" }, body: JSON.stringify(body) });

function secureStore() {
  const secure = new Map<string, string>();
  return {
    "secure.get": ({ key }: { key: string }) => secure.get(key) ?? null,
    "secure.set": ({ key, value }: { key: string; value: string }) => void secure.set(key, value),
    "secure.remove": ({ key }: { key: string }) => void secure.delete(key),
  };
}

const desk = { id: "s1", name: "Desk", url: "ws://desk.test:9877/ws", daemonId: "a".repeat(64), daemonPublicKey: "b".repeat(64), transportKind: "manual" };
const studio = { id: "s2", name: "Studio", url: "ws://studio.test:9877/ws", daemonId: "c".repeat(64), daemonPublicKey: "d".repeat(64), transportKind: "manual" };
const lab = { id: "s3", name: "Lab", url: "ws://lab.test:9877/ws", daemonId: "e".repeat(64), daemonPublicKey: "f".repeat(64), transportKind: "manual" };

function setup(params: Record<string, unknown>, extra: Record<string, unknown> = {}) {
  return {
    ...extra,
    host: { params, handlers: { ...secureStore(), "app.daemon": () => ({ error: "unexpected" }) } },
  };
}

describe("The computer page", () => {
  test("shows this computer, the others with their state, Pair a computer, the rare places and Settings", async () => {
    const app = await open(HARNESS, setup({ servers: [studio, lab] }, {
      storage: { "mewla:v3:servers": JSON.stringify([desk, studio, lab]) },
      network: {
        "GET http://desk.test:9877/auth-check": { status: 503 },
        "GET http://studio.test:9877/auth-check": { status: 401 },
        "GET http://lab.test:9877/auth-check": { status: 503 },
      },
    }));
    await app.waitFor("id:computer-page");
    await app.waitFor("text:Pair again");
    for (const word of ["Desk", "Connected", "Studio", "Lab", "Offline", "Pair a computer", "Screen", "Extensions", "Stats", "Resources", "Settings"]) expect(app.texts()).toContain(word);
    // Studio forgot this phone: its row carries the red dot; Lab is only offline.
    expect(app.shows("id:computer-s2-dot")).toBe(true);
    expect(app.shows("id:computer-s3-dot")).toBe(false);
    // Nothing needs fixing on Desk.
    expect(app.shows("id:needs-fixing")).toBe(false);
  });

  test("a tap on another computer switches to it in one tap, and the stack returns to home", async () => {
    const app = await open(HARNESS, setup({ servers: [studio] }, {
      storage: { "mewla:v3:servers": JSON.stringify([desk, studio]) },
      network: { "GET http://desk.test:9877/auth-check": { status: 503 }, "GET http://studio.test:9877/auth-check": { status: 503 } },
    }));
    await app.waitFor("id:computer-s2");
    await app.press("id:computer-s2");
    await app.waitFor("id:home-stub");
    expect(app.shows("id:computer-page")).toBe(false);
  });

  test("ⓘ opens another computer's details; this computer's name opens its own", async () => {
    const app = await open(HARNESS, setup({ servers: [studio] }, {
      storage: { "mewla:v3:servers": JSON.stringify([desk, studio]) },
      network: { "GET http://desk.test:9877/auth-check": { status: 503 }, "GET http://studio.test:9877/auth-check": { status: 503 } },
    }));
    await app.press("id:computer-s2-info");
    await app.waitFor("id:computer-remove");
    expect(app.value("id:computer-name")).toBe("Studio");
    await app.back();
    await app.waitFor("id:computer-page");
    await app.press("id:computer-details");
    await app.waitFor("id:computer-remove");
    expect(app.value("id:computer-name")).toBe("Desk");
  });

  test("Pair a computer opens pairing", async () => {
    const app = await open(HARNESS, setup({}, { network: { "GET http://desk.test:9877/auth-check": { status: 503 } } }));
    await app.press("id:pair-another");
    await app.waitFor("id:pair-stub");
  });

  test("a computer that forgot this phone says Device not paired, and Pair pairs", async () => {
    const app = await open(HARNESS, setup({ state: "waiting" }, { network: { "GET http://desk.test:9877/auth-check": { status: 401 }, "GET http://desk.test:9877/health": { status: 401 } } }));
    await app.waitFor("id:fix-pair");
    expect(app.texts()).toContain("Device not paired");
    await app.press("id:fix-pair-button");
    await app.waitFor("id:pair-stub");
  });

  test("Claude not signed in says so, and Switch opens Brain's host sheet", async () => {
    const app = await open(HARNESS, setup({}, { network: { "GET http://desk.test:9877/auth-check": { status: 503 } } }));
    await app.waitFor("id:computer-page");
    await app.host.send("app.brain", { host_problem: { code: "not_signed_in", executor: "claude", detail: "Sign in to Claude Code.", installed_executors: ["claude", "codex"] } });
    await app.waitFor("text:Claude is not signed in");
    await app.press("id:fix-host-button");
    await app.waitFor("id:opened-brain-host");
  });

  test("an update the app requires is red on the page and installs with Update", async () => {
    const app = await open(HARNESS, setup({}, {
      network: {
        "GET http://desk.test:9877/auth-check": { status: 503 },
        "GET http://desk.test:9877/daemon-update": json({ version: "0.2.10", supported: true, latest: "0.3.2" }),
        "POST http://desk.test:9877/daemon-update": json({ version: "0.2.10", supported: true, latest: "0.3.2", updating: true }),
      },
    }));
    await app.waitFor("text:Update required");
    await app.press("id:fix-update-button");
    await app.waitFor("text:Updating…");
    expect(app.requests().some((r) => r.method === "POST" && r.url === "http://desk.test:9877/daemon-update")).toBe(true);
  });

  test("an optional update is a quiet row, not a fix", async () => {
    const app = await open(HARNESS, setup({}, {
      network: {
        "GET http://desk.test:9877/auth-check": { status: 503 },
        "GET http://desk.test:9877/daemon-update": json({ version: "0.3.1", supported: true, latest: "0.3.2" }),
      },
    }));
    await app.waitFor("text:Update available · 0.3.2");
    expect(app.shows("id:fix-update")).toBe(false);
  });

  test("removing a computer from its details comes back here and names the computers that did not answer", async () => {
    const app = await open(HARNESS, setup({ servers: [studio] }, {
      storage: { "mewla:v3:servers": JSON.stringify([desk, studio]) },
      network: {
        "GET http://desk.test:9877/auth-check": { status: 503 },
        "GET http://studio.test:9877/auth-check": { status: 503 },
        "DELETE http://desk.test:9877/computers": { status: 503, body: "busy" },
        "DELETE http://studio.test:9877/devices": { status: 200, body: "{}" },
      },
    }));
    app.onDialog((dialog) => (dialog.title === "Remove Studio?" ? "Remove" : null));
    await app.press("id:computer-s2-info");
    await app.waitFor("id:computer-remove");
    await answering(app, () => app.press("id:computer-remove"));
    await app.waitFor("id:removed-notice");
    expect(app.texts()).toContain("Desk didn't answer");
    expect(app.shows("id:computer-s2")).toBe(false);
  });

  test("each return to the page asks the computers again", async () => {
    const app = await open(HARNESS, setup({}, { network: { "GET http://desk.test:9877/auth-check": { status: 503 } } }));
    const probes = () => app.requests().filter((r) => r.url === "http://desk.test:9877/auth-check").length;
    await app.waitUntil(() => probes() === 1);
    await app.press("id:computer-settings");
    await app.waitFor("id:settings");
    await app.back();
    await app.waitUntil(() => probes() === 2);
    await app.setLifecycle("background");
    await app.setLifecycle("active");
    await app.waitUntil(() => probes() === 3);
  });

  test("the computer in use shows how the phone reaches it; a long press says more", async () => {
    const app = await open(HARNESS, setup({ server: { url: "ws://192.168.1.20:9877/ws" } }, { network: { "GET http://192.168.1.20:9877/auth-check": { status: 503 } } }));
    await app.waitFor("id:computer-path");
    await app.longPress("id:computer-connection");
    await app.waitFor("text:LAN · 192.168.1.20");
    await app.host.send("app.daemonState", "waiting");
    await app.waitUntil(() => !app.shows("id:computer-path"));
  });

  test("when every other computer answers a removal, nothing is left to say", async () => {
    const app = await open(HARNESS, setup({ servers: [studio] }, {
      storage: { "mewla:v3:servers": JSON.stringify([desk, studio]) },
      network: {
        "GET http://desk.test:9877/auth-check": { status: 503 },
        "GET http://studio.test:9877/auth-check": { status: 503 },
        "DELETE http://desk.test:9877/computers": { status: 200, body: "{}" },
        "DELETE http://studio.test:9877/computers": { status: 200, body: "{}" },
        "DELETE http://studio.test:9877/devices": { status: 200, body: "{}" },
      },
    }));
    app.onDialog(() => "Remove");
    await app.press("id:computer-s2-info");
    await app.waitFor("id:computer-remove");
    await answering(app, () => app.press("id:computer-remove"));
    await app.waitUntil(() => !app.shows("id:computer-s2") && app.shows("id:pair-another"));
    // Studio forgets Desk too, both ways.
    expect(app.requests().some((r) => r.method === "DELETE" && r.url === "http://studio.test:9877/computers")).toBe(true);
    expect(app.shows("id:removed-notice")).toBe(false);
  });

  test("in a browser, the computer serving the page has no connection-path mark", async () => {
    const app = await open(HARNESS, setup({ server: { url: "ws://192.168.1.20:9877/ws" } }, { platform: "web", network: { "GET http://192.168.1.20:9877/auth-check": { status: 503 } } }));
    await app.waitFor("id:computer-page");
    await app.wait(100);
    expect(app.shows("id:computer-path")).toBe(false);
    // Friends is a phone Lab.
    expect(app.shows("id:computer-friends")).toBe(false);
  });
});
