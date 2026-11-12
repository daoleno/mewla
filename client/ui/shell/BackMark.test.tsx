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
