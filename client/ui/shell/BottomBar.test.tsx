import { describe, expect, open, test } from "nsh/test";
import { capsulePageInset } from "../../core/shell/capsule";

const APP = "./testing/BottomBar.tsx";

/** Where the pushed page's content lies, as it last reported it. */
function foot(app: { host: { posts(): { name: string; data: unknown }[] } }) {
  return app.host.posts().filter((p) => p.name === "app.footLayout").map((p) => p.data as { top: number; bottom: number }).at(-1);
}

describe("a pushed page and the system's bottom bar (Brain 11-15: a PDF's last page hid under Android's navigation bar)", () => {
  test("on a phone its content ends above the bar", async () => {
    const app = await open(APP, { platform: "android", width: 390, height: 844, safeArea: { top: 24, right: 0, bottom: 48, left: 0 }, host: { handlers: { "app.footLayout": () => undefined } } });
    await app.waitFor("text:pushed");
    await app.waitUntil(() => foot(app) !== undefined);
    expect(foot(app)!.bottom).toBeLessThanOrEqual(844 - 48);
  });

  // Brain 11-16: a file preview's title looked to sit under Android's status bar (a scratch page's, not this one).
  test("on a phone its content starts below the status bar and the capsule, never under them", async () => {
    const app = await open(APP, { platform: "android", width: 390, height: 844, safeArea: { top: 48, right: 0, bottom: 0, left: 0 }, host: { handlers: { "app.footLayout": () => undefined } } });
    await app.waitFor("text:pushed");
    await app.waitUntil(() => foot(app) !== undefined);
    expect(foot(app)!.top).toBe(capsulePageInset(48));
    expect(foot(app)!.top).toBeGreaterThan(48);
  });

  test("with no bar, it reaches the screen's bottom", async () => {
    const app = await open(APP, { platform: "android", width: 390, height: 844, safeArea: { top: 24, right: 0, bottom: 0, left: 0 }, host: { handlers: { "app.footLayout": () => undefined } } });
    await app.waitFor("text:pushed");
    await app.waitUntil(() => foot(app) !== undefined);
    expect(foot(app)!.bottom).toBeGreaterThan(844 - 8);
  });
});
