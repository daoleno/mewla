import { describe, expect, open, test } from "nsh/test";
import { paired, phoneHost } from "../testing/fixtures";

/**
 * A Worker needs a grant on this Mac that only the person gives (DESIGN-MAC.md §2.4, `m-perm`): Brain's thread carries one
 * card for it, the reason and two buttons, sealed once the grant counts or the person says Not now; a grant that is given
 * but not yet in effect ends in one button, Restart Mewla. The buttons' signed request is core/screen/grantRequest.test.ts's.
 */
const APP = "../FakeDaemonApp.tsx";
const host = { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: "2026-11-11T07:00:00.000Z" };
const brain = { chat_thread_id: "t-now", host_worker: host };
const at = (i: number) => new Date(Date.UTC(2026, 10, 21, 2, 0, i)).toISOString();
const reason = "A Worker needs to see this Mac's screen to finish the release check.";

function thread(status: string, permission = "screen_recording") {
  return [
    { id: "m1", seq: 1, kind: "user_message", timestamp: at(1), body: "Check the release" },
    { id: `grant:${permission}:1`, seq: 2, kind: "status", role: "assistant", timestamp: at(2), body: reason, status, source: "grant_request", grant_permission: permission },
  ];
}

async function card(status: string, permission?: string) {
  const app = await open(APP, { platform: "macos", width: 1280, height: 800, storage: paired, host: phoneHost({ brain, conversations: { "brain-host": thread(status, permission) } }, {}) });
  await app.waitFor("id:grant-request", { timeout: 15_000 });
  return app;
}

describe("a grant request in Brain's thread", () => {
  test("asked: the reason, Not now and Open System Settings, and nothing else", async () => {
    const app = await card("asked");
    expect(app.texts()).toContain(reason);
    expect(app.shows("id:grant-request >> id:grant-request-not_now")).toBe(true);
    expect(app.shows("id:grant-request >> id:grant-request-open_settings")).toBe(true);
    expect(app.shows("id:grant-request >> text:Open System Settings")).toBe(true);
    expect(app.shows("id:grant-request >> id:grant-request-restart")).toBe(false);
    expect(app.shows("id:grant-request >> id:grant-request-sealed")).toBe(false);
  }, 30_000);

  test("asked, on the Mac: m-perm's card (its title, the reason, Not now and Open System Settings) and it needs you", async () => {
    const app = await card("asked");
    expect(app.shows("id:grant-request >> text:Allow screen recording")).toBe(true);
    // The window's header counts it, as the menu bar cat does: one thing waits for the person.
    await app.waitFor("text:1 needs you", { timeout: 10_000 });
    expect(app.shows("text:All quiet")).toBe(false);
  }, 30_000);

  test("opened (System Settings shown): the same two buttons, the grant is still the person's to give", async () => {
    const app = await card("opened", "accessibility");
    expect(app.shows("id:grant-request >> id:grant-request-open_settings")).toBe(true);
    expect(app.shows("id:grant-request >> id:grant-request-not_now")).toBe(true);
  }, 30_000);

  test("granted: sealed, Allowed, no buttons", async () => {
    const app = await card("granted");
    expect(app.shows("id:grant-request >> id:grant-request-sealed")).toBe(true);
    expect(app.shows("id:grant-request >> text:Allowed")).toBe(true);
    expect(app.shows("id:grant-request >> id:grant-request-open_settings")).toBe(false);
    expect(app.shows("id:grant-request >> id:grant-request-not_now")).toBe(false);
  }, 30_000);

  test("declined (Not now): sealed, with that word, no buttons", async () => {
    const app = await card("declined");
    expect(app.shows("id:grant-request >> text:Not now")).toBe(true);
    expect(app.shows("id:grant-request >> id:grant-request-open_settings")).toBe(false);
  }, 30_000);

  test("restart: one button, Restart Mewla", async () => {
    const app = await card("restart", "accessibility");
    expect(app.shows("id:grant-request >> id:grant-request-restart")).toBe(true);
    expect(app.shows("id:grant-request >> text:Restart Mewla")).toBe(true);
    expect(app.shows("id:grant-request >> id:grant-request-open_settings")).toBe(false);
    expect(app.shows("id:grant-request >> id:grant-request-not_now")).toBe(false);
  }, 30_000);

  test("the computer's news of the card's new state replaces it: asked, then granted seals it in place", async () => {
    const app = await card("asked");
    expect(app.shows("id:grant-request >> id:grant-request-open_settings")).toBe(true);
    app.host.send("app.conversation", { worker: "brain-host", upserts: [thread("granted")[1]] });
    await app.waitFor("id:grant-request >> id:grant-request-sealed", { timeout: 10_000 });
    expect(app.shows("id:grant-request >> id:grant-request-open_settings")).toBe(false);
    expect(app.shows("id:grant-request >> text:Allowed")).toBe(true);
    // One card, not two.
    expect(app.texts().filter((text) => text === reason).length).toBe(1);
  }, 30_000);
});
