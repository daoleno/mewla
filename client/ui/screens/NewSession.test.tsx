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
  test("a tall phone keeps Start under the agents and folders", async () => {
    const app = await newSession(844);
    expect(app.count("id:new-scroll >> id:new-launch")).toBe(0);
  }, 30_000);

  test("a short window (a phone held sideways) puts Start at the scroll's end, where scrolling reaches it", async () => {
    // The sheet's room reaches its body: without it, the scroll kept its 520 pt and Start fell below the screen.
    const app = await newSession(300);
    expect(app.count("id:new-scroll >> id:new-launch")).toBe(1);
  }, 30_000);
});
