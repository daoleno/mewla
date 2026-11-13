import { describe, expect, open, test } from "nsh/test";
import { expoTokenService, paired, phoneHost } from "../testing/fixtures";

// No computer answers: the app goes offline (as in App.test.tsx).
const offlineHost = () => phoneHost({});

describe("the connection mark follows you (SPEC §1.3)", () => {
  for (const platform of ["android", "ios", "web"] as const) {
    test(`${platform}: offline, a pushed page's Back carries the dot and says "Back · Offline"; home has none`, async () => {
      const app = await open("../App.tsx", { host: offlineHost(), storage: paired, platform, network: expoTokenService });
      await app.waitFor("text:Offline", { timeout: 15_000 });
      expect(app.shows("id:back-mark")).toBe(false);
      await app.host.send("links.open", { url: "mewla://calendar" });
      await app.waitFor("id:back-mark", { timeout: 10_000 });
      expect(app.shows("label:Back · Offline")).toBe(true);
      await app.press("label:Back · Offline");
      await app.waitUntil(() => !app.shows("id:back-mark"));
    }, 30_000);
  }

  test("connected, Back is the system's own: no dot", async () => {
    const app = await open("../FakeDaemonApp.tsx", { host: phoneHost({ brain: {} }), storage: paired, platform: "android", network: expoTokenService });
    await app.waitFor("id:composer");
    await app.host.send("links.open", { url: "mewla://calendar" });
    await app.waitFor("id:calendar-empty", { timeout: 10_000 });
    expect(app.shows("id:back-mark")).toBe(false);
    // The header reads Back · the title · Today | Month · + (the mock's order): no leading item for Android to
    // put after the title, the control and + at the header's end.
    expect(app.shows("id:calendar-view")).toBe(true);
    expect(app.shows("id:add-item")).toBe(true);
  }, 30_000);
});

describe("a Session's capsule on Android", () => {
  const brain = { chat_thread_id: "t-now", host_worker: { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: "2026-11-11T07:00:00.000Z" } };
  const proj = { id: "s-proj", name: "s-proj", cwd: "/home/me/proj", command: "claude", status: "unknown", started_at: "2026-11-11T07:00:00.000Z" };

  test("Back is the capsule's own, right before the logo", async () => {
    const app = await open("../FakeDaemonApp.tsx", { host: phoneHost({ brain, worker_sessions: [proj] }), storage: paired, platform: "android", network: expoTokenService });
    await app.waitFor("id:composer");
    await app.host.send("links.open", { url: "mewla://sessions/s-proj" });
    await app.waitFor("id:session-status", { timeout: 10_000 });
    // Its terminal (no chat yet) has the capsule collapsed: Back, the mark and the name.
    expect(app.shows("id:capsule-name")).toBe(true);
    expect(app.shows("id:page-back")).toBe(true);
    await app.wait(500);
    await app.press("id:page-back");
    await app.waitUntil(() => !app.shows("id:session-status"));
  }, 30_000);
});
