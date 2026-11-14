import { describe, expect, open, test } from "nsh/test";
import { paired, phoneHost, settle } from "../testing/fixtures";

const APP = "../FakeDaemonApp.tsx";

// iPhone Duo (the Mac's simulator, iOS 27.2): the cover screen 466 wide with the 84 pt side bar on its right; the inner
// screen 951 x 669 on its side, the side bar again on its right; 669 x 951 upright, its status bar on top.
const SIDE_BAR = { top: 0, right: 84, bottom: 20, left: 0 };
const sessions = [{ id: "w1", name: "release", status: "done", cwd: "/home/me/mewla", command: "claude", provider: "claude", updated_at: Date.now() }];

describe("an open foldable (the owner's pick A, 11-14)", () => {
  test("iPhone Duo open on its side: the tray is the left pane beside Brain; the side bar is not counted as room", async () => {
    const app = await open(APP, { platform: "ios", width: 951, height: 669, safeArea: SIDE_BAR, storage: paired, host: phoneHost({ brain: {}, worker_sessions: sessions }) });
    await app.waitFor("id:left-pane", { timeout: 10_000 });
    // Brain's header beside the pane: no cat and no state control, the pane holds both.
    expect(app.shows("id:state-control")).toBe(false);
    expect(app.shows("id:pane-header")).toBe(true);
  }, 20_000);

  test("iPhone Duo upright, and its cover screen either way: one stack, as on a phone", async () => {
    for (const [width, height, safeArea] of [[669, 951, { top: 40, right: 0, bottom: 20, left: 0 }], [466, 678, SIDE_BAR], [678, 466, SIDE_BAR]] as const) {
      const app = await open(APP, { platform: "ios", width, height, safeArea, storage: paired, host: phoneHost({ brain: {} }) });
      await app.waitFor("id:state-control", { timeout: 10_000 });
      expect(app.shows("id:left-pane")).toBe(false);
    }
  }, 40_000);

  test("Android: the SDK's 7.6\" Fold-in on its side (841 x 674 dp, a side cutout) has the left pane, upright it has not; a phone on its side never", async () => {
    const wideFold = await open(APP, { platform: "android", width: 841, height: 674, safeArea: { top: 24, right: 32, bottom: 16, left: 0 }, storage: paired, host: phoneHost({ brain: {} }) });
    await wideFold.waitFor("id:left-pane", { timeout: 10_000 });
    // Android 14 resizes the window over the keyboard: 674 → 400 high at the same width keeps the pane.
    await wideFold.setViewport(841, 400);
    await wideFold.wait(300);
    expect(wideFold.shows("id:left-pane")).toBe(true);
    for (const [width, height, safeArea] of [[674, 841, { top: 24, right: 0, bottom: 16, left: 0 }], [915, 412, { top: 0, right: 48, bottom: 0, left: 32 }], [840, 600, { top: 0, right: 48, bottom: 0, left: 48 }]] as const) {
      const app = await open(APP, { platform: "android", width, height, safeArea, storage: paired, host: phoneHost({ brain: {} }) });
      await app.waitFor("id:state-control", { timeout: 10_000 });
      expect(app.shows("id:left-pane")).toBe(false);
    }
  }, 40_000);

  test("unfolding and folding keep the same stack, the same page and what is typed", async () => {
    const app = await open(APP, { platform: "ios", width: 466, height: 678, safeArea: SIDE_BAR, storage: paired, host: phoneHost({ brain: {}, worker_sessions: sessions }) });
    await app.waitFor("id:state-control", { timeout: 10_000 });
    await settle(app);
    await app.type("id:composer", "half a thought");
    await app.waitUntil(() => app.value("id:composer") === "half a thought");
    // Unfold, held like a book.
    await app.setViewport(951, 669);
    await app.waitFor("id:left-pane");
    expect(app.value("id:composer")).toBe("half a thought");
    // A page opened from the pane, and what only that page holds (All Sessions' selection).
    await app.press("id:tray-all-sessions");
    await app.waitFor("id:session-row");
    await app.longPress("id:session-row");
    await app.waitFor("text:1 selected");
    // The pushed page keeps its phone capsule, inside the right pane, never over the tray.
    expect(app.count("id:right-pane >> id:capsule-layer")).toBeGreaterThan(0);
    expect(app.count("id:left-pane >> id:capsule-layer")).toBe(0);
    // Turned upright, then folded shut: the page and its selection stay; Back still leads to Brain and its draft.
    await app.setViewport(669, 951);
    await app.waitFor("id:left-pane", { state: "gone" });
    expect(app.shows("text:1 selected")).toBe(true);
    await app.setViewport(466, 678);
    expect(app.shows("text:1 selected")).toBe(true);
    for (let i = 0; i < 3 && !app.shows("id:home-header"); i += 1) await app.back();
    await app.waitFor("id:home-header");
    expect(app.value("id:composer")).toBe("half a thought");
  }, 40_000);

  test("New session on the open Duo is a dialog with Start beside its scroll", async () => {
    const app = await open(APP, { platform: "android", width: 951, height: 669, safeArea: SIDE_BAR, storage: paired, host: phoneHost({ brain: {} }) });
    await app.waitFor("id:left-pane", { timeout: 10_000 });
    await settle(app);
    await app.press("id:tray-new");
    await app.waitFor("id:new-launch", { timeout: 10_000 });
    expect(app.count("id:new-scroll >> id:new-launch")).toBe(0);
  }, 30_000);
});
