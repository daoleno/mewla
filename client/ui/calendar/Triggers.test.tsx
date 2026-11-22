import { describe, expect, open, test } from "nsh/test";
import { expoTokenService, framesOf, paired, phoneHost } from "../testing/fixtures";

const APP = "../FakeDaemonApp.tsx";
const SLOW = { timeout: 10_000 };
const MIN = 60_000;
const HOUR = 3_600_000;
const ago = (ms: number) => new Date(Date.now() - ms).toISOString();

function trigger(id: string, title: string, patch: Record<string, unknown> = {}) {
  return {
    id, title, check: "~/pr-loop/check.sh", interval_seconds: 600, timeout_seconds: 60, work_id: `w-${id}`,
    instruction: "Review each new PR head.", created_at: ago(10 * HOUR), updated_at: ago(MIN), revision: 1, state: "watching",
    last_checked_at: ago(2 * MIN), ...patch,
  };
}

const watching = () => trigger("t-pr", "PR heads", {
  firings: [{ seq: 1, at: ago(5 * HOUR), outputs: 1, payload: "/p/1.txt", event_id: "e1", handled_at: ago(4 * HOUR) }, { seq: 2, at: ago(5 * MIN), outputs: 2, payload: "/p/2.txt", event_id: "e2" }],
  pending_changes: 1,
});
const failing = () => trigger("t-ci", "CI on main", { created_at: ago(HOUR), failing: true, failures: 4, last_failure: "exit status 1\ngh: HTTP 502", state: "failing" });

async function calendarApp(daemon: Record<string, unknown>) {
  const app = await open(APP, { host: phoneHost(daemon), storage: paired, platform: "android", network: expoTokenService });
  await app.host.send("links.open", { url: "mewla://calendar" });
  await app.waitFor("id:calendar-page");
  return app;
}

describe("Triggers in Calendar", () => {
  test("Calendar's one Triggers row says how many watch, with the seal only when one needs you; it opens the Triggers page; none, no row", async () => {
    const app = await calendarApp({ calendar_items: [], triggers: [watching(), failing()] });
    await app.waitFor("id:triggers-row", SLOW);
    expect(app.texts()).toContain("2 watching");
    expect(app.texts()).toContain(" · 1 needs you");
    expect(app.shows("id:triggers-open-seal")).toBe(true);
    // The Triggers are never a list on the agenda.
    expect(app.shows("id:trigger-row-t-pr")).toBe(false);
    await app.press("id:triggers-open");
    await app.waitFor("id:triggers-page", SLOW);
    expect(app.texts()).toContain("2 watching");
    // What needs you first, with the seal; what noticed something today, when it last did.
    expect(app.shows("id:triggers-needs >> id:trigger-row-t-ci")).toBe(true);
    expect(app.shows("id:trigger-row-t-ci-seal")).toBe(true);
    expect(app.texts()).toContain("Its check keeps failing");
    expect(app.shows("id:triggers-today >> id:trigger-row-t-pr")).toBe(true);
    expect(app.shows("id:trigger-row-t-pr-seal")).toBe(false);

    const healthy = await calendarApp({ calendar_items: [], triggers: [watching()] });
    await healthy.waitFor("id:triggers-row", SLOW);
    expect(healthy.texts()).toContain("1 watching");
    expect(healthy.shows("id:triggers-open-seal")).toBe(false);

    const none = await calendarApp({ calendar_items: [] });
    await none.waitFor("id:calendar-headline", SLOW);
    expect(none.shows("id:triggers-row")).toBe(false);
  }, 40_000);

  test("a Trigger's page: how often, what it noticed newest first, what Brain is told, how it checks one level down; Pause, Resume and Check now reach the computer", async () => {
    const app = await calendarApp({ calendar_items: [], triggers: [watching()] });
    await app.press("id:triggers-open");
    await app.press("id:trigger-row-t-pr");
    await app.waitFor("id:trigger-page", SLOW);
    expect(app.texts()).toContain("Checks every 10 min");
    expect(app.texts()).toContain("Review each new PR head.");
    expect(app.texts()).toContain("1 more change waits for Brain to handle the last one.");
    const firings = app.texts().filter((text) => / changes? · /.test(text));
    expect(firings).toEqual(["2 changes · With Brain", "1 change · Brain handled it"]);
    // The command is under How it checks, not on the page.
    expect(app.texts()).not.toContain("~/pr-loop/check.sh");
    await app.press("id:trigger-how");
    await app.waitFor("id:trigger-check-page", SLOW);
    expect(app.texts()).toContain("~/pr-loop/check.sh");
    expect(app.texts()).toContain("Every 10 min · stops after 1 min");
    await app.back();
    await app.waitFor("id:trigger-pause", SLOW);

    await app.press("id:trigger-pause");
    await app.waitFor("id:trigger-resume", SLOW);
    expect(app.texts()).toContain("Paused");
    await app.press("id:trigger-resume");
    await app.waitFor("id:trigger-pause", SLOW);
    await app.press("id:trigger-check-now");
    await app.waitUntil(() => framesOf(app.host.posts(), "check_trigger").length === 1, SLOW);
    expect(framesOf(app.host.posts(), "pause_trigger").map((f) => f.id)).toEqual(["t-pr"]);
    expect(framesOf(app.host.posts(), "resume_trigger").map((f) => f.id)).toEqual(["t-pr"]);
  }, 30_000);

  test("a Trigger that needs you says why and how long, with Check now and Pause on its card; paused, it no longer asks", async () => {
    const app = await calendarApp({ calendar_items: [], triggers: [failing()] });
    await app.press("id:triggers-open");
    await app.press("id:trigger-row-t-ci");
    await app.waitFor("id:trigger-ask", SLOW);
    expect(app.texts()).toContain("Its check keeps failing");
    expect(app.texts().join(" ")).toMatch(/4 times in a row: gh: HTTP 502/);
    expect(app.texts().join(" ")).not.toMatch(/exit status/);
    expect(app.shows("id:trigger-check-now")).toBe(false);
    expect(app.shows("id:trigger-ask-check")).toBe(true);
    // Paused by the person, a failing check no longer asks for them: the card goes.
    await app.press("id:trigger-ask-pause");
    await app.waitUntil(() => framesOf(app.host.posts(), "pause_trigger").length === 1, SLOW);
    await app.waitUntil(() => !app.shows("id:trigger-ask"), SLOW);
  }, 30_000);

  test("a refused check says the computer's reason; Delete asks first, then the Trigger is gone", async () => {
    const app = await calendarApp({ calendar_items: [], triggers: [watching()], replies: { check_trigger: { type: "error", message: "the check is already running" } } });
    await app.press("id:triggers-open");
    await app.press("id:trigger-row-t-pr");
    await app.waitFor("id:trigger-page", SLOW);
    await app.press("id:trigger-check-now");
    await app.waitFor("id:trigger-failure", SLOW);
    expect(app.texts().join(" ")).toContain("the check is already running");

    let asked = "";
    app.onDialog((dialog) => {
      asked = dialog.title ?? "";
      return dialog.title === "Delete Trigger?" ? "Delete" : null;
    });
    await app.press("id:trigger-delete");
    await app.waitUntil(() => framesOf(app.host.posts(), "delete_trigger").length === 1, SLOW);
    await app.waitUntil(() => !app.shows("id:trigger-page"), SLOW);
    expect(asked).toBe("Delete Trigger?");
    await app.waitFor("id:triggers-page");
    expect(app.texts()).toContain("Nothing is watching on this computer.");
  }, 30_000);

  test("a failing Trigger is under Needs you and its row opens the Trigger; a firing Brain has not handled is Brain's, not yours (owner 2026-11-19)", async () => {
    const late = trigger("t-late", "Release notes", { firings: [{ seq: 1, at: ago(3 * HOUR + MIN), outputs: 1, payload: "/p/1.txt", event_id: "e1" }] });
    const app = await open(APP, { host: phoneHost({ triggers: [failing(), late, watching()] }), storage: paired, platform: "android", network: expoTokenService });
    await app.waitFor("id:composer");
    await app.waitUntil(() => app.texts().some((text) => /needs? you/.test(text)), SLOW);
    await app.press("id:state-control");
    await app.waitFor("id:tray-needs");
    expect(app.count("id:tray-needs >> id:tray-request")).toBe(1);
    expect(app.texts()).toContain("Check keeps failing");
    expect(app.texts()).not.toContain("Not handled");
    expect(app.count("id:tray-needs >> id:tray-open")).toBe(1);
    await app.press("id:tray-needs >> id:tray-open");
    await app.waitFor("id:trigger-page", SLOW);
  }, 30_000);
});
