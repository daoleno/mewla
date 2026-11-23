import { describe, expect, open, test } from "nsh/test";
import { phoneHost } from "../testing/fixtures";
import { FIXTURE_NOW, storage } from "../fixture/scenario";

// The owner's capture (11-23): on the Mac's narrowest column "1 needs you · 1 new" sat on one line and "3 running"
// wrapped onto the project chips. The counts are one line in every column: the words while they fit, else dots and numbers.
type Screen = { shows(selector: string): boolean };
/** What each count of the owner's set says in the tray's title, the longest form first. */
const owner = (app: Screen) => [["needs", ["1 needs you", "1"]], ["fresh", ["1 new"]], ["running", ["3 running", "3"]]].map(([kind, forms]) =>
  (forms as string[]).find((text) => app.shows(`id:tray-count-${kind} >> text:${text}`)) ?? "");
const busy = (app: Screen) => [["needs", ["12 need you", "12"]], ["fresh", ["14 new"]], ["running", ["17 running", "17"]]].map(([kind, forms]) =>
  (forms as string[]).find((text) => app.shows(`id:tray-count-${kind} >> text:${text}`)) ?? "");

describe("the tray's counts on a narrow column", () => {
  test("the Mac's pane at its least (240) holds the owner's three counts as dots, numbers and New", async () => {
    const base = phoneHost({});
    const host = { ...base, params: { ...(base.params ?? {}), devScript: { traySqueeze: "owner" } } };
    const app = await open("../fixture/MacFixtureApp.tsx", { platform: "macos", width: 1280, height: 800, storage, host, clock: { time: FIXTURE_NOW } });
    await app.waitFor("text:1 new", { timeout: 15_000 });
    await app.waitFor("id:tray-projects");
    // Not the words that do not fit, nor a count's label cut.
    expect(owner(app)).toEqual(["1", "1 new", "3"]);
    expect(app.shows("label:1 needs you")).toBe(true);
    expect(app.shows("label:3 running")).toBe(true);
    expect(app.count("id:tray-project")).toBe(3);
    // A count still filters, in whatever form it shows.
    await app.press("id:tray-count-running");
    await app.waitUntil(() => app.count("id:tray-running >> id:tray-row") === 3, { timeout: 5_000 });
    expect(app.shows("id:tray-needs")).toBe(false);
  }, 40_000);

  test("every count at once with two digits stays whole at 240", async () => {
    const base = phoneHost({});
    const host = { ...base, params: { ...(base.params ?? {}), devScript: { traySqueeze: "busy" } } };
    const app = await open("../fixture/MacFixtureApp.tsx", { platform: "macos", width: 1280, height: 800, storage, host, clock: { time: FIXTURE_NOW } });
    await app.waitFor("text:14 new", { timeout: 15_000 });
    expect(busy(app)).toEqual(["12", "14 new", "17"]);
  }, 40_000);

  const SQUEEZE = "../fixture/TraySqueezeApp.tsx";
  const OWNER = { needs: 1, fresh: 1, running: 3 };

  test("the web's wide pane at its least (280) and the phone's 320 hold the three as dots and numbers; 390 keeps running's word; a wider window all words", async () => {
    const wide = await open(SQUEEZE, { platform: "web", width: 1280, height: 800, storage, host: phoneHost({}), clock: { time: FIXTURE_NOW } });
    await wide.waitFor("text:11 running", { timeout: 15_000 });
    await wide.host.send("squeeze.set", OWNER);
    await wide.waitFor("text:1 new", { timeout: 10_000 });
    expect(owner(wide)).toEqual(["1", "1 new", "3"]);
    for (const [width, expected] of [[320, ["1", "1 new", "3"]], [390, ["1", "1 new", "3 running"]], [600, ["1 needs you", "1 new", "3 running"]]] as const) {
      const phone = await open(SQUEEZE, { platform: "android", width, height: 800, storage, host: phoneHost({}), clock: { time: FIXTURE_NOW } });
      await phone.waitFor("id:state-control", { timeout: 15_000 });
      await phone.waitFor("text:3 running", { timeout: 15_000 });
      await phone.host.send("squeeze.set", OWNER);
      await phone.press("id:state-control");
      await phone.waitFor("text:1 new", { timeout: 10_000 });
      await phone.waitFor("id:tray-count-running", { timeout: 10_000 });
      await phone.waitUntil(() => owner(phone).every(Boolean), { timeout: 5_000 });
      expect(owner(phone)).toEqual(expected as unknown as string[]);
    }
  }, 90_000);
});
