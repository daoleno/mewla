import { describe, expect, open, test } from "nsh/test";
import { expoTokenService, framesOf, pairedTwo, phoneHost } from "../testing/fixtures";

const APP = "../FakeDaemonApp.tsx";
const LAB = "c".repeat(64);

describe("the cat's dot for another computer", () => {
  test("another computer with a request open lights the cat; answered there, the dot goes; offline is unknown, no dot", async () => {
    const app = await open(APP, {
      platform: "android", storage: pairedTwo, network: expoTokenService,
      host: phoneHost({ brain: {}, computers: { computers: [{ daemon_id: LAB, name: "lab", online: true, open_requests: 2 }], open_requests: 0 } }),
    });
    await app.waitFor("id:home-cat-dot", { timeout: 10_000 });
    expect(framesOf(app.host.posts(), "computers").length).toBeGreaterThan(0);
    // Lab's requests were answered there: Desk says so unasked.
    await app.host.send("app.daemonPush", { type: "computers", computers: [{ daemon_id: LAB, name: "lab", online: true, open_requests: 0 }], open_requests: 0 });
    await app.waitUntil(() => !app.shows("id:home-cat-dot"));
    // Lab went away with requests it had: unknown now, so still no dot.
    await app.host.send("app.daemonPush", { type: "computers", computers: [{ daemon_id: LAB, name: "lab", online: false, open_requests: 2, last_seen: new Date().toISOString() }], open_requests: 0 });
    await app.wait(300);
    expect(app.shows("id:home-cat-dot")).toBe(false);
  }, 20_000);
});
