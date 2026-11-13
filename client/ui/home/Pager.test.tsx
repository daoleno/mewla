import { describe, expect, open, test } from "nsh/test";
import { phoneHost } from "../testing/fixtures";
import { storage } from "../fixture/scenario";

// Trial: three pages (SPEC 1, under trial since 11-13). Brain is the middle page; the computer page is to
// its left, the running things to its right.
async function home() {
  const app = await open("../fixture/FixtureApp.tsx", { platform: "android", storage, host: phoneHost({}) });
  await app.waitFor("id:composer", { timeout: 10_000 });
  return app;
}

describe("Trial: three pages", () => {
  test("the cat block shows the computer page; Back returns to Brain with the draft as it was", async () => {
    const app = await home();
    await app.type("id:composer", "half a thought");
    await app.press("id:home-cat");
    await app.waitFor("id:computer-settings");
    expect(app.shows("id:composer")).toBe(false);
    expect(app.count("id:back")).toBe(0);
    await app.back();
    await app.waitFor("id:composer");
    expect(app.value("id:composer")).toBe("half a thought");
  }, 30_000);

  test("the state control shows the running things as a page; Back returns to Brain", async () => {
    const app = await home();
    await app.waitFor("text:3 need you", { timeout: 10_000 });
    await app.press("id:state-control");
    await app.waitFor("text:Cache layer for the catalogue API", { timeout: 10_000 });
    expect(app.shows("id:home-page-running")).toBe(true);
    expect(app.shows("id:composer")).toBe(false);
    await app.back();
    await app.waitFor("id:composer");
    expect(app.shows("text:Cache layer for the catalogue API")).toBe(false);
  }, 30_000);

  test("a Session opened from the running page pushes, and Back returns to that page", async () => {
    const app = await home();
    await app.waitFor("text:3 need you", { timeout: 10_000 });
    await app.press("id:state-control");
    await app.waitFor("text:Flaky checkout test", { timeout: 10_000 });
    await app.press("text:Flaky checkout test");
    await app.waitFor("id:session-menu", { timeout: 10_000 });
    await app.back();
    await app.waitFor("text:Cache layer for the catalogue API", { timeout: 10_000 });
    expect(app.shows("id:home-page-running")).toBe(true);
    expect(app.shows("id:composer")).toBe(false);
  }, 30_000);

  test("on the computer page, a link to Brain shows Brain", async () => {
    const app = await home();
    await app.press("id:home-cat");
    await app.waitFor("id:computer-settings");
    await app.host.send("links.open", { url: "mewla://brain" });
    await app.waitFor("id:composer");
    expect(app.shows("id:computer-settings")).toBe(false);
  }, 30_000);

  test("a swipe left past half a page shows the running things; a swipe right from there returns to Brain", async () => {
    const app = await home();
    await app.drag("id:home-page-brain", { x: -260 });
    await app.waitFor("text:Cache layer for the catalogue API", { timeout: 10_000 });
    await app.drag("id:home-page-running", { x: 260 });
    await app.waitFor("id:composer");
    await app.drag("id:home-page-brain", { x: 260 });
    await app.waitFor("id:computer-settings");
  }, 30_000);
});
