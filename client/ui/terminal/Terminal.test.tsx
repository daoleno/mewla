import { describe, expect, open, test } from "nsh/test";

const HARNESS = "./testing/Harness.tsx";

const text = ["$ make", "cc main.c", "error: x", "$ make test", "ok", "error: y", "$ ls", "a b"];
const out = (texts: string[]) => texts.find((t) => t.startsWith("scrollTo") || t.startsWith("mark") || t.startsWith("retry")) ?? "";

describe("Terminal, scrolled up", () => {
  test("the four controls say what they do; the prompt jumps and the bottom move the view", async () => {
    const app = await open(HARNESS, { host: { params: { text, top: 6 } } });
    for (const label of ["Search history", "Previous prompt", "Next prompt", "Scroll to bottom"]) expect(app.count(`label:${label}`)).toBe(1);
    await app.press("id:terminal-jump-previous");
    await app.waitUntil(() => out(app.texts()).includes("scrollTo:3"));
    await app.press("id:terminal-jump-bottom");
    await app.waitUntil(() => out(app.texts()).endsWith("scrollToBottom"));
    expect(out(app.texts()).split("\n")).toEqual(["scrollTo:3", "scrollToBottom"]);
  }, 20_000);

  test("Search history opens the bar; a query lands on the newest match and the arrows step through the matches", async () => {
    const app = await open(HARNESS, { host: { params: { text, top: 4 } } });
    await app.press("id:terminal-jump-search");
    await app.waitFor("id:terminal-search-input");
    expect(app.enabled("id:terminal-search-older")).toBe(false);
    await app.type("id:terminal-search-input", "error");
    await app.waitFor("text:2/2");
    expect(out(app.texts()).split("\n")).toEqual(["mark:5", "scrollTo:2"]);
    await app.press("id:terminal-search-older");
    await app.waitFor("text:1/2");
    await app.press("id:terminal-search-newer");
    await app.waitFor("text:2/2");
    await app.press("id:terminal-search-close");
    await app.waitFor("id:terminal-search-input", { state: "gone" });
    expect(out(app.texts()).split("\n").at(-1)).toBe("mark:-1");
  }, 20_000);

  test("a query with no match counts 0", async () => {
    const app = await open(HARNESS, { host: { params: { text, top: 4 } } });
    await app.press("id:terminal-jump-search");
    await app.type("id:terminal-search-input", "nothing here");
    await app.waitFor("text:0");
    expect(app.enabled("id:terminal-search-newer")).toBe(false);
  }, 20_000);
});

describe("Terminal renderer", () => {
  test("a VT that could not start says so and offers Retry", async () => {
    const app = await open(HARNESS, { host: { params: { text, failure: "libghostty-vt did not load." } } });
    expect(app.texts()).toContain("Terminal renderer could not start");
    expect(app.texts()).toContain("libghostty-vt did not load.");
    await app.press("label:Retry Terminal renderer");
    await app.waitFor("id:terminal-renderer-failure", { state: "gone" });
    expect(out(app.texts())).toBe("retry");
  }, 20_000);
});
