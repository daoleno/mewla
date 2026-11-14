import { describe, expect, open, test, type App } from "nsh/test";
import { paired, phoneHost } from "./testing/fixtures";

const APP = "./FakeDaemonApp.tsx";
const brain = {
  chat_thread_id: "t-now",
  host_worker: { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: "2026-11-11T07:00:00.000Z" },
};

const sockets = (app: App) => Number(app.storage()["fake:sockets"] ?? 0);
/** The host is told the current computer once per user of a new connection (notifications, live surfaces). */
const transportsSeen = (app: App) => app.host.calls().filter((call) => call.name === "live.setCurrentServer").length;
/** Lets whatever a step started run its course: the app's timers, its connection, its renders. */
const settle = (app: App, ms = 1_500) => app.waitUntil(() => false, { timeout: ms }).catch(() => undefined);

describe("the app starts once per process", () => {
  test("the entry's top level run again (a hot patch) keeps the one connection, its registrations and Brain's draft", async () => {
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain }) });
    await app.waitFor("id:composer", { timeout: 10_000 });
    await app.waitUntil(() => sockets(app) === 1 && transportsSeen(app) > 0, { timeout: 10_000 });
    await app.type("id:composer", "half a thought");
    await settle(app);
    // What one connection's start tells the host (its computer, once per user of the connection).
    const once = transportsSeen(app);

    await app.host.send("app.entryAgain");
    await settle(app);
    // One connection, never closed and opened again; notifications registered for it once.
    expect(sockets(app)).toBe(1);
    expect(transportsSeen(app)).toBe(once);
    expect(app.value("id:composer")).toBe("half a thought");

    // The connection still answers: a drop reconnects it, still one connection's registrations.
    await app.host.send("app.daemonDrop");
    await app.waitUntil(() => sockets(app) === 2, { timeout: 10_000 });
    await settle(app);
    expect(transportsSeen(app)).toBe(once);
    expect(app.value("id:composer")).toBe("half a thought");
  }, 30_000);
});
