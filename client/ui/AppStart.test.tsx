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

  test("the new app of a move takes the pairing the old one staged, once, and the old one is told", async () => {
    const staged = JSON.stringify({
      version: 1,
      secure: { "mewla.device.v3.id": "phone-1", "mewla.device.v3.name": "Pixel", "mewla.device.v3.seed": "a".repeat(64), "mewla.device.v3.public-key": "b".repeat(64) },
      storage: { "mewla:v3:servers": paired["mewla:v3:servers"], "mewla:v1:current_server_id": "s1", "mewla:onboarded": "true" },
    });
    const host = phoneHost({ brain }, { "handover.take": () => staged, "handover.taken": () => null });
    const app = await open(APP, { platform: "android", storage: {}, host });
    // Paired as it was: the computer's home, not a scan.
    await app.waitFor("id:composer", { timeout: 10_000 });
    expect(app.host.calls().filter((call) => call.name === "handover.taken")).toHaveLength(1);
    expect(JSON.parse(app.storage()["mewla:v3:servers"])[0]).toMatchObject({ id: "s1", name: "Desk" });
    expect(app.host.calls().filter((call) => call.name === "secure.set").map((call) => call.data)).toContainEqual({ key: "mewla.device.v3.seed", value: "a".repeat(64) });
  }, 30_000);
});
