import { describe, expect, open, test } from "nsh/test";
import { expoTokenService, paired, phoneHost } from "../../testing/fixtures";

/**
 * Owner 11-13: the capsule header that the Session has, on Extensions too. Every pushed page on a phone takes it
 * from the kit's Page (b9fecf03); this keeps Extensions on it: Back in the capsule, no system header.
 */
describe("Extensions on a phone wear the capsule", () => {
  for (const platform of ["android", "ios"] as const) {
    test(`${platform}: Back sits in the capsule over Extensions`, async () => {
      const app = await open("../../FakeDaemonApp.tsx", { host: phoneHost({ brain: {} }), storage: paired, platform, network: expoTokenService });
      await app.waitFor("id:composer");
      // A sign-in's return opens Extensions (the computer page, then Extensions).
      await app.host.send("links.open", { url: "mewla://plugins" });
      await app.waitFor("id:extensions-page", { timeout: 10_000 });
      expect(app.shows("id:capsule-layer")).toBe(true);
      expect(app.shows("id:page-back")).toBe(true);
    }, 30_000);
  }
});
