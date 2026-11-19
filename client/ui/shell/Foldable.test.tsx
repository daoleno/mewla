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
      // The cover screen's side bar holds the state (the side rail); upright, the inner screen has none.
      await app.waitFor(safeArea.right ? "id:rail-state" : "id:state-control", { timeout: 10_000 });
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
    await app.waitFor("id:rail-state", { timeout: 10_000 });
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
    // The pushed page's Back is in the side rail (its capsule has nothing else at rest), never over the tray.
    expect(app.shows("id:rail-back")).toBe(true);
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

describe("the side rail: Mewla's buttons in the iPhone Duo's side bar (11-19)", () => {
  test("cover upright: Brain's controls and the app's places are in the bar, the capsule keeps the cat alone", async () => {
    const app = await open(APP, { platform: "ios", width: 466, height: 678, safeArea: SIDE_BAR, storage: paired, host: phoneHost({ brain: {}, worker_sessions: sessions }) });
    await app.waitFor("id:rail", { timeout: 10_000 });
    for (const id of ["rail-brain-menu", "rail-brain", "rail-state", "rail-search", "rail-screen", "rail-new"]) expect(app.shows(`id:${id}`)).toBe(true);
    // Nothing to go back to, and no turn running: no Back, no Stop. Not duplicated in the header.
    expect(app.shows("id:rail-back")).toBe(false);
    expect(app.shows("id:rail-stop")).toBe(false);
    expect(app.shows("id:state-control")).toBe(false);
    expect(app.shows("id:brain-menu")).toBe(false);
    expect(app.count("id:rail-page >> id:rail-brain-menu")).toBe(1);
    expect(app.count("id:rail-places >> id:rail-new")).toBe(1);
    expect(app.shows("id:home-header")).toBe(true);
    // Each place does what its page's own control does.
    await app.press("id:rail-new");
    await app.waitFor("id:new-session-sheet");
  }, 30_000);

  test("State opens the tray; a pushed page and a Session have Back and their controls in the bar, not in the capsule", async () => {
    const app = await open(APP, { platform: "ios", width: 466, height: 678, safeArea: SIDE_BAR, storage: paired, host: phoneHost({ brain: {}, worker_sessions: sessions }) });
    await app.waitFor("id:rail-state", { timeout: 10_000 });
    await settle(app);
    await app.press("id:rail-state");
    await app.waitFor("id:tray");
    await app.press("id:tray-all-sessions");
    await app.waitFor("id:session-row");
    await app.waitFor("id:rail-back");
    expect(app.shows("id:page-back")).toBe(false);
    await app.press("id:session-row");
    await app.waitFor("id:rail-session-menu");
    expect(app.shows("id:rail-session-mode") || app.shows("id:rail-session-menu")).toBe(true);
    expect(app.shows("id:session-actions")).toBe(false);
    expect(app.shows("id:page-back")).toBe(false);
    // Back in the bar leads back down the stack; Brain in the bar goes home.
    await app.press("id:rail-back");
    await app.waitFor("id:rail-session-menu", { state: "gone" });
    await app.press("id:rail-brain");
    await app.waitFor("id:rail-back", { state: "gone" });
    expect(app.shows("id:home-header")).toBe(true);
  }, 40_000);

  test("inner screen on its side: the rail beside the pane, State left to the pane; Brain's header is gone", async () => {
    const app = await open(APP, { platform: "ios", width: 951, height: 669, safeArea: SIDE_BAR, storage: paired, host: phoneHost({ brain: {} }) });
    await app.waitFor("id:left-pane", { timeout: 10_000 });
    await app.waitFor("id:rail");
    expect(app.shows("id:rail-state")).toBe(false);
    expect(app.shows("id:rail-search")).toBe(true);
    expect(app.shows("id:home-header")).toBe(false);
  }, 30_000);

  test("the left app in Split View has its bar, and the rail, on the left", async () => {
    const app = await open(APP, { platform: "ios", width: 466, height: 669, safeArea: { top: 0, right: 0, bottom: 20, left: 84 }, storage: paired, host: phoneHost({ brain: {} }) });
    await app.waitFor("id:rail", { timeout: 10_000 });
    expect(app.shows("id:rail-brain")).toBe(true);
  }, 20_000);

  test("never on a normal iPhone (upright or on its side), Android with the same inset, or the inner screen upright", async () => {
    const cases = [
      ["ios", 393, 852, { top: 59, right: 0, bottom: 34, left: 0 }],
      ["ios", 852, 393, { top: 0, right: 59, bottom: 21, left: 59 }],
      ["android", 466, 678, SIDE_BAR],
      ["ios", 669, 951, { top: 40, right: 0, bottom: 20, left: 0 }],
    ] as const;
    for (const [platform, width, height, safeArea] of cases) {
      const app = await open(APP, { platform, width, height, safeArea, storage: paired, host: phoneHost({ brain: {} }) });
      await app.waitFor("id:state-control", { timeout: 10_000 });
      expect(app.shows("id:rail")).toBe(false);
    }
  }, 60_000);
});
