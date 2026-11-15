import { describe, expect, open, test } from "nsh/test";
import { expoTokenService, paired, pairedTwo, phoneHost } from "../testing/fixtures";

const APP = "../FakeDaemonApp.tsx";

describe("home's header", () => {
  test("one computer: the capsule says \"Brain\" alone, the cat its own button to the computer page; the composer says \"Tell Brain\", no ellipsis", async () => {
    const app = await open(APP, { platform: "android", storage: paired, network: expoTokenService, host: phoneHost({ brain: {} }) });
    await app.waitFor("id:capsule", { timeout: 10_000 });
    await app.waitFor("text:Starting…", { timeout: 10_000 });
    // SPEC §11: the computer's name only with two or more computers.
    expect(app.shows("id:home-title")).toBe(true);
    expect(app.shows("id:home-computer")).toBe(false);
    expect(app.count("id:home-cat")).toBe(1);
    expect(app.shows("id:home-cat-sprite")).toBe(true);
    expect(app.shows("text:Tell Brain")).toBe(true);
    expect(app.shows("text:Tell Brain…")).toBe(false);
    // The cat is the door to the computer page, whose capsule is Back alone while its own title shows.
    await app.press("id:home-cat");
    await app.waitFor("id:computer-page");
    await app.waitFor("id:capsule-collapsed");
    expect(app.shows("id:page-back")).toBe(true);
    expect(app.shows("id:page-title")).toBe(false);
  }, 20_000);

  test("two computers: the current one's name under \"Brain\" (SPEC §11 Home header)", async () => {
    const app = await open(APP, { platform: "android", storage: pairedTwo, network: expoTokenService, host: phoneHost({ brain: {} }) });
    await app.waitFor("id:home-computer", { timeout: 10_000 });
    expect(app.texts()).toContain("Desk");
  }, 20_000);
});

describe("Brain can't start", () => {
  // A Windows user's Brain ended at start and Chat said only "Brain can't start": the
  // computer's reason (how the agent ended, what it printed) is in Chat itself.
  test("Chat shows the computer's reason under the title", async () => {
    const detail = "Brain could not start on claude: claude exited less than a second after it started (exit code 127). It printed: mewla sh: claude: argument 4 holds a character.";
    const brain = { host_worker: null, host_problem: { code: "start_failed", executor: "claude", detail } };
    const app = await open(APP, { platform: "android", storage: paired, network: expoTokenService, host: phoneHost({ brain }) });
    await app.waitFor("text:Brain can't start", { timeout: 10_000 });
    expect(app.texts()).toContain(detail);
  }, 20_000);
});
