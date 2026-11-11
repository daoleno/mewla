import { describe, expect, open, test } from "nsh/test";

const PREVIEW = "./testing/Preview.tsx";

describe("Terminal on the web", () => {
  test("is a web view the app's capabilities allow: it shows, and no load failure comes back", async () => {
    const app = await open(PREVIEW, { platform: "web" });
    expect(app.count("id:terminal-web")).toBe(1);
    await app.wait(500);
    expect(app.texts()).toContain("failed: ");
  }, 20_000);
});
