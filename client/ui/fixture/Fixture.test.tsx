import { describe, expect, open, test } from "nsh/test";
import { phoneHost } from "../testing/fixtures";
import { IDS, storage } from "./scenario";

const APP = "./FixtureApp.tsx";

async function fixture() {
  const app = await open(APP, { platform: "android", storage, host: phoneHost({}) });
  await app.waitFor("id:composer", { timeout: 10_000 });
  return app;
}

describe("the mock's scenario on the app (the fidelity sheet's fixture)", () => {
  test("home is Brain's thread: the Work cards, the folded read and the open requests", async () => {
    const app = await fixture();
    await app.waitFor("id:work-card", { timeout: 10_000 });
    expect(app.texts()).toContain("Docs refresh");
    expect(app.shows("id:activity")).toBe(true);
    expect(app.shows("id:request-card")).toBe(true);
    expect(app.texts().join(" ")).toContain("When does tonight's deploy go out?");
  }, 30_000);

  test("the tray lists the mock's Sessions and what needs you", async () => {
    const app = await fixture();
    await app.waitFor("text:3 need you", { timeout: 10_000 });
    await app.press("id:state-control");
    await app.waitFor("text:Cache layer for the catalogue API", { timeout: 10_000 });
    expect(app.shows("id:home-page-running")).toBe(true);
    expect(app.shows("id:composer")).toBe(false);
    for (const name of ["Flaky checkout test", "German translation", "Docs link sweep"]) expect(app.texts().join("\n")).toContain(name);
  }, 30_000);

  test("the Rate-limit Session opens on its push to approve, with its edited files", async () => {
    const app = await fixture();
    await app.host.send("links.open", { url: `mewla://sessions/${IDS.ratelimit}` });
    await app.waitFor("id:request-card", { timeout: 10_000 });
    expect(app.texts()).toContain("git push origin fix/rate-limit-window");
    expect(app.texts().join(" ")).toContain("Ready to push.");
  }, 30_000);

  test("Docs refresh's Work detail lists its three Workers", async () => {
    const app = await fixture();
    await app.host.send("links.open", { url: `mewla://brain/work/${IDS.docsWork}` });
    await app.waitFor("text:Workers", { timeout: 10_000 });
    for (const name of ["Quickstart rewrite", "Screenshot refresh", "API reference"]) expect(app.texts().join("\n")).toContain(name);
  }, 30_000);

  test("the computer page lists the other computers, and Resources shows its tiles", async () => {
    const app = await fixture();
    await app.wait(500);
    await app.press("id:home-cat");
    await app.waitFor("text:Resources", { timeout: 10_000 });
    for (const name of ["studio", "laptop"]) expect(app.texts().join("\n")).toContain(name);
    await app.press("text:Resources");
    await app.waitFor("id:tile-cpu", { timeout: 10_000 });
    expect(app.shows("id:consumers")).toBe(true);
  }, 30_000);

  test("the Pricing Session: Brain's brief in two lines, an Edit with its +/−, the screenshots under their row", async () => {
    const app = await fixture();
    await app.host.send("links.open", { url: `mewla://sessions/${IDS.pricing}` });
    await app.waitFor("id:delegated-brief", { timeout: 10_000 });
    await app.waitFor("id:tool-pictures >> id:chat-image-open", { timeout: 10_000 });
    expect(app.texts().some((text) => text.includes("+38 −12"))).toBe(true);
  }, 30_000);
});
