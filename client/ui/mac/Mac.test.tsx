import { describe, expect, open, test } from "nsh/test";
import { framesOf, item, paired, phoneHost } from "../testing/fixtures";
import { testComputer } from "../testing/pairing";

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

function macHost(devScript: Record<string, unknown>, daemon: Record<string, unknown> = {}) {
  const host = phoneHost({ brain, conversations: { "brain-host": [] }, ...daemon }, { "app.pairing": () => pairing });
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

  test("a fresh Mac enables trusted LAN only after consent", async () => {
    let enabled = false;
    const host = macHost({ pairPhone: true });
    const handlers = {
      ...host.handlers,
      "app.pairing": () => enabled
        ? { ...pairing, mac_lan: true }
        : { daemon_id: pairing.daemon_id, daemon_public_key: pairing.daemon_public_key, expires_at: pairing.expires_at, local_link: pairing.local_link, mac_lan: false },
      "app.setLAN": ({ enabled: choice }: { enabled: boolean }) => { enabled = choice; },
    };
    const app = await open(APP, { platform: "macos", width: 720, height: 560, storage: paired, host: { ...host, handlers } });
    await app.waitFor("id:pair-lan-cancel");
    await app.press("id:pair-lan-cancel");
    expect(enabled).toBe(false);
    await app.keyboard.press("mod+5");
    await app.press("id:pair-phone-row");
    await app.press("id:pair-lan-allow");
    await app.waitFor("id:pair-phone-qr");
    expect(enabled).toBe(true);
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

  // What Privacy & access says of Chrome (core/settings/computerModel chromeAccess): never a blank row.
  const chromeSettings = async (browser: Record<string, unknown>, post?: (request: { body?: string | null }) => { status: number; headers: Record<string, string>; body: string }) => {
    const base = macHost({ settings: "access" });
    const status = { enabled: true, browser: { summary: "x", ...browser } };
    const json = (body: unknown) => ({ status: 200, headers: { "content-type": "application/json" }, body: JSON.stringify(body) });
    const app = await open(APP, {
      platform: "macos", width: 1280, height: 800, storage: paired, host: base,
      network: { "GET http://127.0.0.1:1/agent-use": json(status), ...(post ? { "POST http://127.0.0.1:1/agent-use": post } : {}) },
    });
    await app.waitUntil(() => app.window("mewla-settings").state()?.visible === true, { timeout: 15_000 });
    await app.waitFor("id:mac-settings >> id:mac-access-chrome");
    return app;
  };

  test("Settings › Privacy & access: Chrome says it is not installed instead of staying blank", async () => {
    const app = await chromeSettings({ state: "no_chrome" });
    await app.waitFor("id:mac-access-chrome >> text:Chrome isn't installed", { timeout: 10_000 });
    expect(app.shows("id:mac-access-chrome >> id:mac-access-chrome-fix")).toBe(false);
  }, 30_000);

  test("Settings › Privacy & access: Chrome without the extension carries the one button that adds it, and it asks the computer", async () => {
    let asked = "";
    const app = await chromeSettings({ state: "no_extension", fix: "install_extension" }, (request) => {
      asked = request.body ?? "";
      return { status: 200, headers: { "content-type": "application/json" }, body: JSON.stringify({ enabled: true, browser: { state: "preparing", summary: "x" } }) };
    });
    await app.waitFor("id:mac-access-chrome >> id:mac-access-chrome-fix", { timeout: 10_000 });
    expect(app.shows("id:mac-access-chrome >> text:Add extension")).toBe(true);
    await app.press("id:mac-access-chrome >> id:mac-access-chrome-fix");
    await app.waitUntil(() => asked.includes("install_extension"), { timeout: 5_000 });
    await app.waitFor("id:mac-access-chrome >> text:Getting ready…", { timeout: 10_000 });
  }, 30_000);

  test("Settings › Privacy & access: Chrome with the extension on says so", async () => {
    const app = await chromeSettings({ state: "ready" });
    await app.waitFor("id:mac-access-chrome >> text:Extension on", { timeout: 10_000 });
  }, 30_000);

  // Owner-found 11-23: Mewla removed from Accessibility with −, then nothing in Mewla brought it back, and the guide's
  // "Allow" did nothing. Each row reads what the daemon reads afresh on /agent-use (no Screen approval of the app
  // involved); Allow opens the list with the panel to drag Mewla in and asks the daemon to follow the grant.
  test("Settings › Privacy & access: each grant's real state, and Allow brings a removed Mewla back through the panel", async () => {
    const guides: unknown[] = [];
    const posts: string[] = [];
    let accessibility = "missing";
    const json = (body: unknown) => ({ status: 200, headers: { "content-type": "application/json" }, body: JSON.stringify(body) });
    const status = () => ({
      enabled: true, browser: { state: "ready", summary: "x" },
      computer: { state: "permission", summary: "x", permissions: [
        { id: "screen_recording", name: "Screen Recording", state: "granted" },
        { id: "accessibility", name: "Accessibility", state: accessibility },
      ] },
    });
    const base = macHost({ settings: "access" });
    const app = await open(APP, {
      platform: "macos", width: 1280, height: 800, storage: paired,
      host: { ...base, handlers: { ...base.handlers, "app.permissionGuide": (data: unknown) => void guides.push(data) } },
      network: {
        "GET http://127.0.0.1:1/agent-use": () => json(status()),
        "POST http://127.0.0.1:1/agent-use": (request: { body?: string | null }) => { posts.push(request.body ?? ""); return json(status()); },
      },
    });
    await app.waitUntil(() => app.window("mewla-settings").state()?.visible === true, { timeout: 15_000 });
    await app.waitFor("id:mac-access-screen >> id:mac-access-screen-state", { timeout: 10_000 });
    expect(app.shows("id:mac-access-screen >> text:Allowed")).toBe(true);
    // Removed from the list: Allow, which opens the panel for Accessibility and has the daemon follow it.
    await app.waitFor("id:mac-access-accessibility >> id:mac-access-accessibility-allow", { timeout: 10_000 });
    await app.press("id:mac-access-accessibility >> id:mac-access-accessibility-allow");
    await app.waitUntil(() => guides.length === 1 && posts.some((body) => body.includes('"follow":"accessibility"')), { timeout: 5_000 });
    // With the daemon's reading, so the host clears Mewla's own rows that do not count first.
    expect(guides[0]).toEqual({ permission: "accessibility", state: "missing" });
    // Switched off in the list: the same Allow.
    accessibility = "off";
    await app.wait(3_500);
    expect(app.shows("id:mac-access-accessibility >> id:mac-access-accessibility-allow")).toBe(true);
    await app.press("id:mac-access-accessibility >> id:mac-access-accessibility-allow");
    await app.waitUntil(() => guides.length === 2, { timeout: 5_000 });
    expect(guides[1]).toEqual({ permission: "accessibility", state: "off" });
    // Given, in effect once Mewla starts again: one button, which restarts it.
    accessibility = "restart";
    await app.waitFor("id:mac-access-accessibility >> id:mac-access-accessibility-restart", { timeout: 6_000 });
    await app.press("id:mac-access-accessibility >> id:mac-access-accessibility-restart");
    await app.waitUntil(() => posts.some((body) => body.includes('"restart":true')), { timeout: 5_000 });
    // And once it counts, the row says so by itself.
    accessibility = "granted";
    await app.waitFor("id:mac-access-accessibility >> id:mac-access-accessibility-state", { timeout: 6_000 });
  }, 40_000);

  test("Settings › Privacy & access: Watch's switch reads the computer's and sets it (m-access)", async () => {
    let posted = "";
    const json = (body: unknown) => ({ status: 200, headers: { "content-type": "application/json" }, body: JSON.stringify(body) });
    const app = await open(APP, {
      platform: "macos", width: 1280, height: 800, storage: paired, host: macHost({ settings: "access" }),
      network: {
        "GET http://127.0.0.1:1/watch": json({ enabled: false }),
        "POST http://127.0.0.1:1/watch": (request: { body?: string | null }) => { posted = request.body ?? ""; return json({ enabled: true }); },
      },
    });
    await app.waitUntil(() => app.window("mewla-settings").state()?.visible === true, { timeout: 15_000 });
    await app.waitFor("id:mac-settings >> id:mac-access-watch", { timeout: 10_000 });
    expect(app.shows("id:mac-settings >> text:Notice what changes in your projects")).toBe(true);
    await app.press("id:mac-settings >> id:mac-access-watch");
    await app.waitUntil(() => posted.includes('"enabled":true'), { timeout: 5_000 });
  }, 30_000);

  test("Settings › Privacy & access: a daemon without Watch's switch shows no Watch group, not a dead row", async () => {
    const app = await open(APP, {
      platform: "macos", width: 1280, height: 800, storage: paired, host: macHost({ settings: "access" }),
      network: { "GET http://127.0.0.1:1/watch": { status: 404, headers: {}, body: "404 page not found" } },
    });
    await app.waitUntil(() => app.window("mewla-settings").state()?.visible === true, { timeout: 15_000 });
    await app.waitFor("id:mac-settings >> id:mac-access-devices", { timeout: 10_000 });
    await app.wait(1_000);
    expect(app.shows("id:mac-settings >> id:mac-access-watch")).toBe(false);
  }, 30_000);

  test("Settings › General carries this app's update beside its version: Update to X, and the tap goes to the update", async () => {
    const update = { kind: "available", update: { version: "0.4.18", versionCode: 0, fileName: "", url: "", sha256: "", size: 0 } };
    const base = phoneHost({ brain, conversations: { "brain-host": [] } }, { "app.pairing": () => pairing, "app.appUpdatePressed": () => undefined });
    const app = await open(APP, { platform: "macos", width: 1280, height: 800, storage: paired, host: { ...base, params: { ...base.params, devScript: { settings: "general" }, appUpdate: update } } });
    await app.waitUntil(() => app.window("mewla-settings").state()?.visible === true, { timeout: 15_000 });
    await app.waitFor("id:mac-settings >> id:mac-settings-update");
    expect(app.shows("id:mac-settings >> text:Update to 0.4.18")).toBe(true);
    await app.press("id:mac-settings >> id:mac-settings-update");
    await app.waitUntil(() => app.host.posts().some((post) => post.name === "app.appUpdatePressed"), { timeout: 5_000 });
  }, 30_000);

  test("Settings › General shows no update row while no update waits", async () => {
    const app = await open(APP, { platform: "macos", width: 1280, height: 800, storage: paired, host: macHost({ settings: "general" }) });
    await app.waitUntil(() => app.window("mewla-settings").state()?.visible === true, { timeout: 15_000 });
    await app.waitFor("id:mac-settings >> id:mac-settings-version");
    expect(app.shows("id:mac-settings >> id:mac-settings-update")).toBe(false);
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

  test("⌘W on the main window is a close too: the quick chat's shortcut is offered from the cat", async () => {
    const app = await open(APP, { platform: "macos", width: 1280, height: 800, storage: paired, host: macHost({}) });
    await app.waitFor("id:composer", { timeout: 15_000 });
    await app.keyboard.press("mod+w");
    await app.waitUntil(() => app.window("main").state()?.visible === false, { timeout: 5_000 });
    await app.waitUntil(() => app.window("shortcut-offer").state()?.visible === true, { timeout: 10_000 });
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
    // The pane selects Brain's row while its terminal is in front (w-session selects the Session's row).
    expect(app.shows("id:pane-brain >> id:selected-well")).toBe(true);
  }, 40_000);

  test("no agent on this Mac: Brain can't start, the thread offers the installs, and one runs this Mac's own command in a terminal Session", async () => {
    const installs = [
      { id: "claude", name: "Claude Code", command: "'/Applications/Mewla.app/Contents/Helpers/mewla' deps -agent claude -state-dir '/Users/me/.mewla'" },
      { id: "codex", name: "Codex", command: "'/Applications/Mewla.app/Contents/Helpers/mewla' deps -agent codex -state-dir '/Users/me/.mewla'" },
    ];
    const noAgent = { host_problem: { code: "not_installed", executor: "claude", detail: "Brain is set to run on claude, but claude is not installed on this computer.", installed_executors: [] }, agent_installs: installs };
    const host = phoneHost({ brain: noAgent }, { "app.pairing": () => pairing });
    const app = await open(APP, { platform: "macos", width: 1280, height: 800, storage: paired, host });
    await app.waitFor("id:mac-no-agent", { timeout: 15_000 });
    expect(app.shows("id:mac-no-agent >> text:No agent on this Mac")).toBe(true);
    expect(app.shows("id:mac-install-claude >> text:Install Claude Code")).toBe(true);
    // The composer reads "Brain can't start"; the notice with the computer's detail is this page's, not shown.
    expect(app.shows("text:Brain is set to run on claude, but claude is not installed on this computer.")).toBe(false);
    await app.press("id:mac-install-codex");
    await app.waitUntil(() => framesOf(app.host.posts(), "create_session").length === 1, { timeout: 10_000 });
    const created = framesOf(app.host.posts(), "create_session")[0];
    expect([created.command, created.name]).toEqual([installs[1].command, "Install Codex"]);
    // Its terminal Session opens, where the installer and the sign-in ask.
    await app.waitFor("id:session-state", { timeout: 10_000 });
  }, 40_000);

  test("under 1024 the window is one column (w-small), at 1024 two panes: a Mac window is never a foldable held on its side", async () => {
    const narrow = await open(APP, { platform: "macos", width: 760, height: 620, storage: paired, host: macHost({}) });
    await narrow.waitFor("id:composer", { timeout: 15_000 });
    expect(narrow.shows("id:left-pane")).toBe(false);
    const first = await open(APP, { platform: "macos", width: 720, height: 560, storage: paired, host: macHost({}) });
    await first.waitFor("id:composer", { timeout: 15_000 });
    expect(first.shows("id:left-pane")).toBe(false);
    const wide = await open(APP, { platform: "macos", width: 1024, height: 700, storage: paired, host: macHost({}) });
    await wide.waitFor("id:left-pane", { timeout: 15_000 });
  }, 60_000);

  test("a page the left pane opens (Calendar) has no ‹ Back: the left pane is the way back", async () => {
    const app = await open(APP, { platform: "macos", width: 1280, height: 800, storage: paired, host: macHost({ open: "calendar" }) });
    await app.waitFor("id:left-pane", { timeout: 15_000 });
    await app.waitFor("id:calendar-page", { timeout: 10_000 });
    expect(app.count("id:page-back")).toBe(0);
  }, 30_000);

  test("Calendar is w-cal, the same as every client's: \"N today\" and the date, the day's card with the now line, one quiet line a row, local times", async () => {
    const done = item("Crawler check", -0.01, { kind: "scheduled_action", status: "completed", runs: [{ id: "r1", scheduled_for: new Date().toISOString(), started_at: new Date().toISOString(), status: "completed", result_preview: "2 stale feeds" }] });
    const later = item("Write the digest", 48, { kind: "scheduled_action", recurrence: "daily" });
    // Overdue from an earlier day: in Today's card with its date, not counted as today's.
    const overdue = item("Renew the domain", -30);
    const app = await open(APP, { platform: "macos", width: 1280, height: 800, storage: paired, host: macHost({ open: "calendar" }, { calendar_items: [done, later, overdue] }) });
    await app.waitFor("id:calendar-headline", { timeout: 15_000 });
    await app.waitFor("id:calendar-item-Write the digest", { timeout: 10_000 });
    const texts = app.texts();
    expect(texts).toContain("1 today");
    expect(texts).toContain("Done · 2 stale feeds");
    // A repeat whose next time is after tomorrow is under Later, with its date.
    expect(app.shows("id:calendar-day-later >> id:calendar-item-Write the digest")).toBe(true);
    expect(texts.some((text) => /^[A-Z][a-z]{2}, [A-Z][a-z]{2} \d+ · Every day · Brain$/.test(text))).toBe(true);
    expect(app.shows("id:calendar-day-today >> id:calendar-now")).toBe(true);
    expect(app.shows("id:calendar-day-today >> id:calendar-item-Crawler check")).toBe(true);
    // The phone's lines are gone: no zone names, no status words, no kind tiles.
    expect(texts.some((text) => text.includes("GMT") || text.includes("UTC") || text.startsWith("Scheduled"))).toBe(false);
    expect(app.count("id:calendar-item-lead")).toBe(0);
    expect(app.shows("id:calendar-day-today >> id:calendar-item-Renew the domain")).toBe(true);
    expect(texts.some((text) => /^[A-Z][a-z]{2}, [A-Z][a-z]{2} \d+ · Reminder$/.test(text))).toBe(true);
    // Month: today's card under the month.
    await app.press("id:calendar-view-month");
    await app.waitFor("id:calendar-month", { timeout: 10_000 });
    await app.waitFor("id:agenda-month >> id:calendar-item-Crawler check", { timeout: 10_000 });
    expect(app.count("id:calendar-item-lead")).toBe(0);
  }, 30_000);

  test("Brain's thread in the window sits in the mock's column (700 at most), its header the window's width", async () => {
    const app = await open(APP, { platform: "macos", width: 760, height: 620, storage: paired, host: macHost({}) });
    await app.waitFor("id:mac-thread-column", { timeout: 15_000 });
    expect(app.shows("id:home-header")).toBe(true);
  }, 30_000);

  // Owner 11-23: a Mac app always has its own computer, at its own loopback address, and nothing loses it.
  test("an old pairing of this Mac by its hosted name is moved to loopback in place (same id), and another computer stays", async () => {
    const local = `mewla://pair?u=${encodeURIComponent("ws://127.0.0.1:1/ws")}&n=Desk&d=${"a".repeat(64)}&k=${"b".repeat(64)}&t=${"7".repeat(64)}`;
    const storage = {
      ...paired,
      "mewla:v3:servers": JSON.stringify([
        { id: "s1", name: "MacBook Pro", url: "wss://quiet-otter.m.mewla.ai/ws", daemonId: "a".repeat(64), daemonPublicKey: "b".repeat(64) },
        { id: "s2", name: "manjaro", url: "ws://127.0.0.1:2/ws", daemonId: "c".repeat(64), daemonPublicKey: "d".repeat(64) },
      ]),
      "mewla:v1:current_server_id": "s2",
    };
    const host = phoneHost({ brain, conversations: { "brain-host": [] } }, { "app.pairing": () => ({ ...pairing, local_link: local }) });
    const app = await open(APP, { platform: "macos", width: 1280, height: 800, storage, host });
    const stored = () => JSON.parse(app.storage()["mewla:v3:servers"] ?? "[]") as Array<{ id: string; name: string; url: string }>;
    await app.waitUntil(() => stored().find((server) => server.id === "s1")?.url === "ws://127.0.0.1:1/ws", { timeout: 15_000 });
    expect(stored().map((server) => `${server.id} ${server.name} ${server.url}`)).toEqual(["s1 MacBook Pro ws://127.0.0.1:1/ws", "s2 manjaro ws://127.0.0.1:2/ws"]);
    await app.waitFor("text:This Mac · Online", { timeout: 10_000 });
  }, 30_000);

  test("this Mac's daemon killed under the app (its pairing still answers, as `mewla pair` does with no daemon): the pane says Mewla is not running, not just offline", async () => {
    const local = `mewla://pair?u=${encodeURIComponent("ws://127.0.0.1:1/ws")}&n=Desk&d=${"a".repeat(64)}&k=${"b".repeat(64)}&t=${"7".repeat(64)}`;
    const host = phoneHost({ brain }, { "app.pairing": () => ({ ...pairing, local_link: local }) });
    const app = await open(APP, { platform: "macos", width: 1280, height: 800, host, storage: { ...paired, "mewla:v1:mac_local_daemon_key": "b".repeat(64) } });
    await app.waitFor("text:This Mac · Online", { timeout: 15_000 });
    await app.host.send("app.daemonDown");
    await app.waitFor("text:This Mac · Mewla is not running", { timeout: 30_000 });
    expect(app.shows("text:This Mac · Offline · Server unreachable")).toBe(false);
  }, 60_000);

  // Owner 11-23, his exact state: this Mac removed on 0.4.26, manjaro current, this Mac's Mewla not answering.
  test("no entry for this Mac, another computer current, its Mewla not answering: This Mac is a row saying so; chosen, the reason with Try Again; joined, it is the computer", async () => {
    // Down: nothing answers at its address (a request the test does not list fails, as a refused connection does).
    const down = testComputer("http://127.0.0.1:19877", 13);
    const mac = testComputer("http://127.0.0.1:19878", 13);
    let up = false;
    const host = phoneHost({ brain }, {
      "app.pairing": () => ({ ...pairing, daemon_public_key: mac.publicKey, local_link: (up ? mac : down).link("MacBook Pro") }),
    });
    (host.params as Record<string, unknown>).computerName = "MacBook Pro";
    const storage = {
      ...paired,
      "mewla:v3:servers": JSON.stringify([{ id: "s2", name: "manjaro", url: "ws://127.0.0.1:2/ws", daemonId: "c".repeat(64), daemonPublicKey: "d".repeat(64) }]),
      "mewla:v1:current_server_id": "s2",
    };
    const app = await open(APP, { platform: "macos", width: 1280, height: 800, host, storage,
      network: { [mac.pairURL]: () => mac.answer() } });
    await app.waitFor("text:manjaro", { timeout: 15_000 });
    await app.press("id:home-cat");
    await app.waitFor("id:computer-this-mac", { timeout: 10_000 });
    await app.waitFor("id:computer-this-mac >> text:Mewla is not running", { timeout: 15_000 });
    expect(app.shows("id:computer-this-mac >> text:MacBook Pro")).toBe(true);

    await app.press("id:computer-this-mac");
    await app.waitFor("id:mac-starting", { timeout: 10_000 });
    await app.waitFor("id:mac-starting >> text:Mewla is not running", { timeout: 15_000 });
    expect(app.shows("text:Try Again")).toBe(true);
    await app.press("id:mac-starting-back");
    await app.waitUntil(() => !app.shows("id:mac-starting"), { timeout: 10_000 });
    expect(app.storage()["mewla:v1:current_server_id"]).toBe("s2");

    // Chosen again while it is still down; its Mewla starts, and Try Again makes it the computer, manjaro kept.
    await app.press("id:computer-this-mac");
    await app.waitFor("id:mac-starting >> text:Try Again", { timeout: 15_000 });
    up = true;
    await app.press("text:Try Again");
    await app.waitUntil(() => !app.shows("id:mac-starting"), { timeout: 15_000 });
    await app.waitFor("text:This Mac · Online", { timeout: 15_000 });
    const stored = JSON.parse(app.storage()["mewla:v3:servers"] ?? "[]") as Array<{ id: string; name: string; url: string }>;
    expect(stored.map((server) => `${server.name} ${server.url}`).sort()).toEqual(["MacBook Pro ws://127.0.0.1:19878/ws", "manjaro ws://127.0.0.1:2/ws"]);
  }, 90_000);

  test("first run: no onboarding; while this Mac's daemon does not answer, why, and Try Again", async () => {
    const host = phoneHost({}, { "app.pairing": () => { throw new Error("This computer's Mewla has no address yet."); } });
    const app = await open(APP, { platform: "macos", width: 1280, height: 800, host });
    await app.waitFor("id:mac-starting", { timeout: 15_000 });
    await app.waitFor("text:This computer's Mewla has no address yet.", { timeout: 10_000 });
    expect(app.shows("id:onboarding")).toBe(false);
    expect(app.shows("text:Try Again")).toBe(true);
  }, 30_000);
});
