import { describe, expect, open, test } from "nsh/test";
import { expoTokenService, paired, pairedTwo, phoneHost } from "../testing/fixtures";

const APP = "../FakeDaemonApp.tsx";

describe("home's header", () => {
  test("one computer: \"Brain\" alone; the composer says \"Tell Brain\", no ellipsis", async () => {
    const app = await open(APP, { platform: "android", storage: paired, network: expoTokenService, host: phoneHost({ brain: {} }) });
    await app.waitFor("id:home-header", { timeout: 10_000 });
    await app.waitFor("text:Starting…", { timeout: 10_000 });
    expect(app.shows("id:home-computer")).toBe(false);
    expect(app.shows("text:Tell Brain")).toBe(true);
    expect(app.shows("text:Tell Brain…")).toBe(false);
  }, 20_000);

  test("two computers: the current one's name under \"Brain\" (SPEC §11 Home header)", async () => {
    const app = await open(APP, { platform: "android", storage: pairedTwo, network: expoTokenService, host: phoneHost({ brain: {} }) });
    await app.waitFor("id:home-computer", { timeout: 10_000 });
    expect(app.texts()).toContain("Desk");
  }, 20_000);
});
