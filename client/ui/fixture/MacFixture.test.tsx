import { describe, expect, open, test } from "nsh/test";
import { phoneHost } from "../testing/fixtures";
import { FIXTURE_NOW, storage } from "./scenario";

// The busy desk the Mac's fidelity captures are taken on (MacFixtureApp.tsx, macos-fidelity.yml): the app joins the
// scripted workbench as this Mac's own daemon and shows the mock's Sessions in the left pane.
describe("the Mac fixture", () => {
  test("joins workbench as this Mac and shows the busy desk in two panes", async () => {
    const app = await open("./MacFixtureApp.tsx", { platform: "macos", width: 1280, height: 800, storage, host: phoneHost({}), clock: { time: FIXTURE_NOW } });
    await app.waitFor("id:left-pane", { timeout: 15_000 });
    await app.waitFor("text:3 need you", { timeout: 10_000 });
    expect(app.texts()).toContain("workbench");
    expect(app.texts().join("\n")).toContain("Cache layer for the catalogue API");
    expect(app.shows("id:composer")).toBe(true);
  }, 30_000);
});
