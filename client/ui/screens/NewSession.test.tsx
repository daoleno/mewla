import { describe, expect, open, test } from "nsh/test";
import { paired, phoneHost } from "../testing/fixtures";

const APP = "../FakeDaemonApp.tsx";

/** New session from the tray's +, on a window `height` high. */
async function newSession(height: number) {
  const app = await open(APP, { host: phoneHost({ brain: {} }), storage: paired, platform: "ios", width: 390, height });
  await app.host.send("links.open", { url: "mewla://sessions" });
  await app.waitFor("id:tray-new", { timeout: 10_000 });
  await app.press("id:tray-new");
  await app.waitFor("id:new-launch", { timeout: 10_000 });
  return app;
}

describe("New session fits the window (iPhone Duo, 11-14)", () => {
  for (const height of [844, 466, 300]) {
    test(`on a window ${height} high the Task field and Start are under the scroll, never in it`, async () => {
      // In no scroll, NSH keeps the focused field clear of the keyboard once the sheet rises; Start in the scroll sat
      // behind the keyboard on the iPhone Duo's cover screen held sideways (Mac re-run 11-14, 1c).
      const app = await newSession(height);
      expect(app.count("id:new-pinned >> id:new-launch")).toBe(1);
      expect(app.count("id:new-pinned >> id:new-task")).toBe(1);
      expect(app.count("id:new-scroll >> id:new-launch")).toBe(0);
      expect(app.count("id:new-scroll >> id:new-task")).toBe(0);
      // The agents still show on every window, down to 300 (the Task caption gives way below 500); when room runs
      // out, the scroll gives way first, never Task or Start.
      expect(app.count("id:new-scroll >> id:preset-claude")).toBe(1);
      // A short window gives the Task caption's room to the agents; the placeholder says it.
      expect(app.texts().includes("Task")).toBe(height >= 500);
    }, 30_000);
  }

  test("a custom command's two fields are under the scroll too, and Start starts it", async () => {
    const app = await newSession(844);
    await app.press("id:new-advanced");
    await app.waitFor("id:new-command");
    expect(app.count("id:new-pinned >> id:new-command")).toBe(1);
    expect(app.count("id:new-pinned >> id:new-name")).toBe(1);
    expect(app.count("id:new-scroll >> id:new-command")).toBe(0);
  }, 30_000);
});
