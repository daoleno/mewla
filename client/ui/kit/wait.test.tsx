import { describe, expect, open, test } from "nsh/test";

const HARNESS = "./testing/WaitHarness.tsx";
const start = (reducedMotion = false) => open(HARNESS, { clock: {}, reducedMotion });

describe("a place waiting, drawn (SPEC §16.2)", () => {
  test("nothing before 150 ms, then final-size placeholder rows; the slow line at 3 s; Try again at 15 s", async () => {
    const app = await start();
    await app.wait(140);
    expect(app.shows("id:placeholder-rows")).toBe(false);
    expect(app.shows("id:content")).toBe(false);
    await app.wait(20);
    expect(app.shows("id:placeholder-rows")).toBe(true);
    expect(app.texts().some((t) => /Loading/.test(t))).toBe(false);
    expect(app.shows("id:slow-line-on")).toBe(false);
    await app.wait(2900);
    expect(app.shows("id:slow-line-on")).toBe(true);
    await app.wait(11_900);
    expect(app.shows("text:Couldn't read sessions")).toBe(false);
    await app.wait(200);
    await app.waitFor("text:Couldn't read sessions");
    expect(app.shows("text:Desk didn't answer")).toBe(true);
    expect(app.shows("id:slow-line-on")).toBe(false);
    await app.press("id:wait-retry");
    await app.waitFor("text:reads 1");
  });

  test("the answer crossfades in; the content is made once and kept under a forced cold read", async () => {
    const app = await start();
    await app.wait(500);
    await app.press("id:answer");
    await app.waitFor("text:Pricing page rework · made 1");
    await app.waitUntil(() => !app.shows("id:placeholder-rows"));
    await app.press("id:force-slow");
    await app.wait(200);
    expect(app.shows("id:placeholder-rows")).toBe(true);
    await app.wait(3000);
    expect(app.shows("id:slow-line-on")).toBe(true);
    await app.press("id:answer");
    await app.wait(2000);
    await app.waitUntil(() => !app.shows("id:placeholder-rows"));
    // Faded under the placeholder, never re-made: what it holds (a scroll position, a terminal) stays.
    expect(app.shows("text:Pricing page rework · made 1")).toBe(true);
    await app.press("id:force-off");
    await app.wait(1000);
    expect(app.shows("text:Pricing page rework · made 1")).toBe(true);
  });

  test("empty only after a successful read", async () => {
    const app = await start();
    await app.wait(16_000);
    expect(app.shows("id:wait-empty")).toBe(false);
    await app.press("id:wait-retry");
    await app.wait(400);
    await app.press("id:answer-empty");
    await app.waitFor("id:wait-empty");
  });

  test("offline is not a failure: it waits, says away, and reads again when back", async () => {
    const app = await start();
    await app.press("id:link-connecting");
    await app.wait(30_000);
    expect(app.shows("text:Couldn't read sessions")).toBe(false);
    expect(app.shows("id:placeholder-rows")).toBe(true);
    await app.press("id:link-away");
    await app.waitFor("text:Desk is offline");
    await app.press("id:link-up");
    await app.waitFor("text:reads 1");
  });

  test("a waiting button: the press alone under 150 ms, then the ring over the label", async () => {
    const app = await start();
    await app.press("id:approve");
    expect(app.shows("id:approve-ring")).toBe(false);
    expect(app.enabled("id:approve")).toBe(false);
    await app.wait(200);
    expect(app.shows("id:approve-ring")).toBe(true);
    await app.press("id:approve-done");
    expect(app.shows("id:approve-ring")).toBe(false);
  });

  test("reduced motion: the slow line stands still; the answer still arrives", async () => {
    const app = await start(true);
    await app.wait(3200);
    expect(app.shows("id:slow-line-on")).toBe(true);
    await app.press("id:answer");
    await app.waitFor("id:content");
  });
});
