import { describe, expect, open, test } from "nsh/test";
import { framesOf, paired, phoneHost } from "../testing/fixtures";

// Mewla for Mac (platform macos): the app's computer is this Mac's daemon (`app.pairing`), the menu bar cat's
// perch is the phone's Brain home in a popup, and Pair a phone shows the daemon's QR.
const APP = "../FakeDaemonApp.tsx";
const brainHost = { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: "2026-11-11T07:00:00.000Z" };
const brain = { chat_thread_id: "t-now", host_worker: brainHost };
const qr = { size: 3, rows: ["101", "010", "101"] };
// The stored computer (fixtures `paired`) is this Mac's daemon: same key.
const pairing = {
  expires_at: "2099-01-01T00:00:00Z",
  daemon_id: "a".repeat(64),
  daemon_public_key: "b".repeat(64),
  phone_address: "http://192.168.1.20:9876",
  phone_link: "mewla://settings?p=phone",
  phone_qr: qr,
  wifi_address: "http://192.168.1.30:9876",
  wifi_link: "mewla://settings?p=wifi",
  wifi_qr: { size: 2, rows: ["10", "01"] },
  local_link: "mewla://settings?p=local",
};

function macHost(devScript: Record<string, unknown>) {
  const host = phoneHost({ brain, conversations: { "brain-host": [] } }, { "app.pairing": () => pairing });
  return { ...host, params: { ...host.params, devScript } };
}

describe("Mewla for Mac", () => {
  test("the perch opens as the shortcut opens it, with Brain's composer, and what it sends goes to Brain", async () => {
    const app = await open(APP, { platform: "macos", width: 1280, height: 800, storage: paired, host: macHost({ quickSend: "From the perch." }) });
    await app.waitUntil(() => app.window("perch").state()?.visible === true, { timeout: 15_000 });
    await app.waitUntil(() => framesOf(app.host.posts(), "send_input").some((frame) => JSON.stringify(frame).includes("From the perch.")), { timeout: 10_000 });
    await app.waitFor("id:perch >> id:composer", { timeout: 10_000 });
    const bounds = app.window("perch").state()!.bounds;
    // The 380 × 560 panel and the pet perched 19 pt above it (DESIGN-MAC.md §2.2).
    expect([bounds.size.width, bounds.size.height]).toEqual([380, 579]);
    expect(app.shows("id:perch >> id:perch-cat")).toBe(true);
  }, 30_000);

  test("the perch: ⤢ in place of ⋯ opens the window on Brain and closes the perch", async () => {
    const app = await open(APP, { platform: "macos", width: 1280, height: 800, storage: paired, host: macHost({ perch: true }) });
    await app.waitUntil(() => app.window("perch").state()?.visible === true, { timeout: 15_000 });
    await app.waitFor("id:perch >> id:perch-open-window", { timeout: 10_000 });
    expect(app.shows("id:perch >> id:brain-menu")).toBe(false);
    await app.press("id:perch >> id:perch-open-window");
    await app.waitUntil(() => app.window("perch").state() === null, { timeout: 5_000 });
    expect(app.window("main").state()?.focused).toBe(true);
  }, 30_000);

  test("the perch: Esc closes it", async () => {
    const app = await open(APP, { platform: "macos", width: 1280, height: 800, storage: paired, host: macHost({ perch: true }) });
    await app.waitUntil(() => app.window("perch").state()?.focused === true, { timeout: 15_000 });
    await app.waitFor("id:perch >> id:composer", { timeout: 10_000 });
    await app.keyboard.press("escape");
    await app.waitUntil(() => app.window("perch").state() === null, { timeout: 5_000 });
  }, 30_000);

  test("Pair a phone shows the daemon's QR, the time it has left, and This Wi‑Fi's code", async () => {
    const app = await open(APP, { platform: "macos", width: 1280, height: 800, storage: paired, host: macHost({ pairPhone: true }) });
    await app.waitFor("id:pair-phone-dialog >> id:pair-phone-qr", { timeout: 15_000 });
    expect(app.texts().some((text) => /^Scan with the Mewla app · \d+:\d\d left$/.test(text))).toBe(true);
    await app.press("text:This Wi‑Fi");
    await app.waitFor("id:pair-phone-qr");
    expect(app.shows("text:Copy link")).toBe(true);
    expect(app.shows("text:Send to phone")).toBe(true);
    await app.press("id:pair-phone-close");
    await app.waitUntil(() => !app.shows("id:pair-phone-qr"), { timeout: 5_000 });
  }, 30_000);

  test("the menu bar's Go menu goes to a place by its shortcut: ⌘2 is All Sessions", async () => {
    const app = await open(APP, { platform: "macos", width: 1280, height: 800, storage: paired, host: macHost({}) });
    await app.waitFor("id:composer", { timeout: 15_000 });
    await app.keyboard.press("mod+2");
    await app.waitFor("id:sessions-empty", { timeout: 10_000 });
  }, 30_000);

  test("This Mac (⌘5): no hero cat on wide, Phones and browsers first, and Pair a phone opens the dialog", async () => {
    const app = await open(APP, { platform: "macos", width: 1280, height: 800, storage: paired, host: macHost({}) });
    await app.waitFor("id:composer", { timeout: 15_000 });
    await app.keyboard.press("mod+5");
    await app.waitFor("id:this-mac-devices", { timeout: 10_000 });
    expect(app.shows("id:computer-cat")).toBe(false);
    await app.press("id:pair-phone-row");
    await app.waitFor("id:pair-phone-dialog >> id:pair-phone-qr", { timeout: 10_000 });
  }, 30_000);

  test("Settings (This Mac's row, ⌘,) is a window of its own: General, then Privacy & access by the sidebar", async () => {
    const app = await open(APP, { platform: "macos", width: 1280, height: 800, storage: paired, host: macHost({}) });
    await app.waitFor("id:composer", { timeout: 15_000 });
    await app.keyboard.press("mod+5");
    await app.waitFor("id:computer-settings", { timeout: 10_000 });
    await app.press("id:computer-settings");
    await app.waitUntil(() => app.window("mewla-settings").state()?.visible === true, { timeout: 10_000 });
    const bounds = app.window("mewla-settings").state()!.bounds;
    expect([bounds.size.width, bounds.size.height]).toEqual([720, 560]);
    await app.waitFor("id:mac-settings >> id:mac-settings-login");
    expect(app.shows("id:mac-settings >> id:mac-settings-version")).toBe(true);
    expect(app.shows("id:mac-settings >> text:Appearance")).toBe(true);
    await app.press("id:mac-settings >> id:mac-settings-access");
    await app.waitFor("id:mac-settings >> id:mac-access-screen");
    expect(app.shows("id:mac-settings >> text:Accessibility (click and type)")).toBe(true);
    expect(app.shows("id:mac-settings >> text:Asks you first")).toBe(true);
    // Paired devices live on This Mac, not here.
    expect(app.shows("id:mac-settings >> id:this-mac-devices")).toBe(false);
  }, 30_000);

  // The keystroke itself is core/mac/shortcut.test.ts's: the test keyboard reaches only the main window.
  test("the first time the window closes, the quick chat's shortcut is offered from the cat; one the system refuses shows as taken; Change records", async () => {
    const app = await open(APP, { platform: "macos", width: 1280, height: 800, storage: paired, host: macHost({}) });
    await app.waitFor("id:composer", { timeout: 15_000 });
    await app.window("main").pressClose();
    await app.waitUntil(() => app.window("shortcut-offer").state()?.visible === true, { timeout: 10_000 });
    const bounds = app.window("shortcut-offer").state()!.bounds;
    expect([bounds.size.width, bounds.size.height]).toEqual([280, 134]);
    await app.waitFor("id:shortcut-offer >> id:shortcut-recorder");
    expect(app.shows("id:shortcut-offer >> text:⇧⌘Space")).toBe(true);
    // Headless, the system gives no global shortcut: it is never set silently, and says so.
    expect(app.shows("id:shortcut-offer >> text:⇧⌘Space is held by another app")).toBe(true);
    await app.press("id:shortcut-offer >> id:shortcut-change");
    await app.waitFor("id:shortcut-offer >> text:Type a shortcut");
    await app.press("id:shortcut-offer >> id:shortcut-done");
    await app.waitUntil(() => app.window("shortcut-offer").state() === null, { timeout: 5_000 });
  }, 30_000);

  test("first run, Brain's empty thread: Ready when you are, with this Mac's agents, Brain's host first", async () => {
    const executors = [
      { id: "claude", name: "claude", provider: "claude" },
      { id: "codex", name: "codex", provider: "codex", host: true },
      { id: "grok", name: "grok", provider: "grok" },
    ];
    const states = { claude: { installed: true, signed_in: true }, codex: { installed: true, signed_in: true }, grok: { installed: false, signed_in: false } };
    const ready = { ...brain, host_executor: { id: "codex", name: "codex", provider: "codex" }, executors, executor_states: states };
    const host = phoneHost({ brain: ready, conversations: { "brain-host": [] } }, { "app.pairing": () => pairing });
    const app = await open(APP, { platform: "macos", width: 1280, height: 800, storage: paired, host: { ...host, params: { ...host.params, devScript: {} } } });
    await app.waitFor("id:mac-ready", { timeout: 15_000 });
    expect(app.shows("text:Ready when you are")).toBe(true);
    expect(app.shows("id:mac-ready-codex")).toBe(true);
    expect(app.shows("id:mac-ready-claude")).toBe(true);
    // Not installed: not shown.
    expect(app.shows("id:mac-ready-grok")).toBe(false);
    expect(app.shows("text:What should Brain do?")).toBe(false);
  }, 30_000);

  test("another app in front, or no window shown, holds none of the computer's news: Brain's reply shows at once", async () => {
    const app = await open(APP, { platform: "macos", width: 1280, height: 800, storage: paired, host: macHost({}) });
    await app.waitFor("id:composer", { timeout: 15_000 });
    const reply = (i: number, body: string) => ({ id: `m${i}`, seq: i, kind: "assistant_message", role: "assistant", timestamp: new Date(Date.UTC(2026, 10, 20, 7, 0, i)).toISOString(), body });
    await app.setLifecycle("inactive");
    await app.host.send("app.conversation", { worker: "brain-host", upserts: [reply(1, "Said while another app was in front.")] });
    await app.waitFor("text:Said while another app was in front.", { timeout: 5_000 });
    await app.setLifecycle("background");
    await app.host.send("app.conversation", { worker: "brain-host", upserts: [reply(2, "Said with no window shown.")] });
    await app.waitFor("text:Said with no window shown.", { timeout: 5_000 });
  }, 60_000);

  test("Brain's terminal follows Brain's host: one the computer replaces (a restart, a lost Session resumed) moves the open terminal to the new host", async () => {
    const app = await open(APP, { platform: "macos", width: 1280, height: 800, storage: paired, host: macHost({ terminal: true }) });
    const active = () => framesOf(app.host.posts(), "set_active_worker").map((frame) => frame.worker_id);
    await app.waitUntil(() => active().includes("brain-host"), { timeout: 15_000 });
    await app.host.send("app.daemonPush", { type: "brain_snapshot", brain: { ...brain, host_worker: { ...brainHost, id: "brain-host-2" } } });
    await app.waitUntil(() => active().at(-1) === "brain-host-2", { timeout: 10_000 });
    await app.wait(4_000);
    expect(active().at(-1)).toBe("brain-host-2");
    expect(app.shows("id:session-mode")).toBe(true);
  }, 40_000);

  test("first run: no onboarding; while this Mac's daemon does not answer, why, and Try Again", async () => {
    const host = phoneHost({}, { "app.pairing": () => { throw new Error("This computer's Mewla has no address yet."); } });
    const app = await open(APP, { platform: "macos", width: 1280, height: 800, host });
    await app.waitFor("id:mac-starting", { timeout: 15_000 });
    await app.waitFor("text:This computer's Mewla has no address yet.", { timeout: 10_000 });
    expect(app.shows("id:onboarding")).toBe(false);
    expect(app.shows("text:Try Again")).toBe(true);
  }, 30_000);
});
